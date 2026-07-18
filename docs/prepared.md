# Prepared Go Bundles

Prepared bundles are an optional AOT acceleration path. Ember compiles the
exact loaded Program into deterministic Go source; the application builds that
source and supplies its exported `Bundle` explicitly. A missing bundle keeps
the ordinary dynamic Machine path.

Generated code is a trusted application build artifact, not a sandbox for
untrusted Go. Ember's root runtime does not invoke the Go toolchain, load native
code, register bundles through `init`, or expose Machine registers and arenas.
Static linking is the portable release path. The optional `preparedplugin`
subpackage is an explicit editor adapter for hosts that accept Go's plugin
constraints.

## Manifest

`emberc` is the standard-library-only file/manifest wrapper around
`Program.WritePreparedGo`. Paths are relative to the manifest file, not the
process working directory.

```json
{
  "package": "preparedgame",
  "output": "internal/preparedgame/prepared_generated.go",
  "max_bytes": 33554432,
  "parallelism": 1,
  "entrypoints": [
    {"name": "server", "module": "logical:game/server"}
  ],
  "modules": [
    {"id": "logical:game/server", "source": "scripts/server.luau"},
    {"id": "logical:game/shared", "source": "scripts/shared.luau"}
  ]
}
```

Every source module that the Program can load, including `require`
dependencies, must appear once in `modules`. Module IDs use the public
`logical:` or `host:` namespace spelling. Entrypoint order is significant and
is preserved. `max_bytes` is optional; zero uses Ember's 32 MiB default.
`parallelism` is optional and defaults to one.

The output directory must already exist. Generate or atomically replace the
file with:

```sh
go run github.com/besmpl/ember/cmd/emberc ./ember-prepared.json
```

CI can verify byte-for-byte freshness without changing the file:

```sh
go run github.com/besmpl/ember/cmd/emberc -check ./ember-prepared.json
```

Malformed manifests, undeclared modules, bounded-input violations, unsupported
Programs, and oversized generated output fail before the destination changes.
`-check` reports missing or stale output and never rewrites it.

## Runtime Binding

Import the generated application package and pass its bundle explicitly:

```go
runtime, err := program.NewRuntime(ember.RuntimeOptions{
    Prepared: preparedgame.Bundle,
})
```

The bundle is bound to the exact prepared ABI, semantic version, Program hash,
and module/Proto inventory. A mismatch returns `*ember.PreparedBundleError`
before runtime-owner mutation and never silently falls back. Passing no bundle
preserves the ordinary runtime selection path.

## Prepared Generations

`PreparedRuntimeSlot` keeps reload transactionality outside guest execution. A
host builds and loads the next bundle while the current generation keeps
running, then binds an inert candidate before reaching its safe point:

```go
candidate, err := scripts.Prepare(nextProgram, ember.RuntimeOptions{
    Host:     gameHost,
    Limits:   limits,
    Prepared: nextBundle,
})
if err != nil {
    return err // the active generation is unchanged
}

// Run this only after the frame's script calls, callbacks, and resumes finish.
if err := scripts.Activate(candidate); err != nil {
    candidate.Close()
    return err
}
```

Call active script behavior through `scripts.Use`. One `Use` should cover a
whole frame or host operation; the supplied `*Runtime` must not escape it.
Activation while `Use` is running returns `ErrRuntimeBusy`. A candidate becomes
stale if another candidate wins first. Bundle mismatch is still reported as a
wrapped `*PreparedBundleError` before the active generation changes.

Successful activation closes the old Runtime. Old callbacks and suspensions
therefore remain generation-bound and fail closed/stale; Ember does not
transplant closures, stacks, tables, or module state. Keep game/world state in
the Host Adapter. If script state must survive, transfer only application-owned
detached data through an explicit schema before activation.

`PreparedRuntimeSlot.Use` adds one host-boundary admission per batch, not per
guest operation. Generated functions, side exits, and the Machine execution
path are unchanged.

## Same-Process Editor Reload

For source not known at the host's original `go build`, generate a `package
main`, put it under a unique content-derived package path, build an immutable
plugin, and open its exported bundle. An editor builder follows this shape:

```sh
# digest covers generated Go plus Go version, target, tags, and module identity.
generation="internal/emberprepared/${digest}"
# Write Program.WritePreparedGo(..., Package: "main") to:
#   ${generation}/prepared_generated.go
go build -buildmode=plugin \
  -o ".ember/prepared/${digest}.so" "./${generation}"
```

With `emberc`, set the manifest's `package` to `main` and generate a staging
file first. Compute the complete build identity, then copy those unchanged bytes
into the identity-named generation directory before building. The output and
cache directories are application owned. Both the Go package import path and
artifact path must change when the build identity changes; Go rejects a second
body for an already loaded plugin package path. Identical identities reuse the
original artifact path. Build from the same module graph, Go toolchain, build
tags, and shared dependency sources as the running host. Then use an absolute
path:

```go
bundle, err := preparedplugin.Open(artifactPath)
if err != nil {
    return err // the active slot is untouched
}
candidate, err := scripts.Prepare(nextProgram, ember.RuntimeOptions{
    Host: gameHost, Limits: limits, Prepared: bundle,
})
```

Go plugins require cgo and are supported only where the standard library's
plugin loader is available (currently Darwin, FreeBSD, and Linux). They cannot
be unloaded, so every distinct loaded generation consumes process-lifetime
code memory. Editor hosts must use unique content-derived package and artifact
paths, deduplicate identical build identities, cap reload generations, and
periodically restart. A plugin is trusted native code, not a script sandbox. On
unsupported builds, `preparedplugin.Open` returns
`preparedplugin.ErrUnsupported`; Ember never silently falls back to the dynamic
VM.

Use rebuild/re-exec when same-process plugins are unavailable. That keeps AOT
speed and reclaims old code, but process-local state survives only through an
explicit application handoff.
