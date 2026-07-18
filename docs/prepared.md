# Prepared execution

Prepared execution accelerates an exact loaded Program while preserving the
canonical Machine as its semantic fallback. Ember has two main preparation
times:

- **Static prepared Go:** generate deterministic Go, compile it with the
  application, and pass its bundle explicitly. This is the portable release
  path and the broadest prepared tier.
- **Reload-time native:** give changed source to `PreparedRuntimeSlot.Prepare`
  before activation. On supported processes Ember lowers qualified numeric
  functions directly to ARM64 or x86-64 code without the Go toolchain or cgo.

Prepared artifacts are trusted application code, not a sandbox. Neither path
runs hidden file watchers or source loaders. Static preparation keeps all build
effects in the host toolchain; reload-time preparation is an explicit expensive
slot operation.

## Static manifest

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

Malformed manifests, undeclared modules, bounded-input violations, and
oversized generated output fail before the destination changes. Unsupported
individual Protos produce exact nil bundle entries and use canonical replay;
they do not prevent supported siblings from being generated. `-check` reports
missing or stale output and never rewrites it.

## Static runtime binding

Import the generated application package and pass its bundle explicitly:

```go
runtime, err := program.NewRuntime(ember.RuntimeOptions{
    Prepared: preparedgame.Bundle,
})
```

The bundle is bound to the exact prepared ABI, semantic version, Program hash,
and module/Proto inventory. A mismatch returns `*ember.PreparedBundleError`
before runtime-owner mutation and never silently falls back. Passing no bundle
to `Program.NewRuntime` preserves the ordinary runtime selection path.

## Prepared generations and hot reload

`PreparedRuntimeSlot` keeps compilation and reload transactionality outside
guest execution. Load changed source into a new Program, prepare it while the
current generation remains active, and publish only at the host's safe point:

```go
candidate, err := scripts.Prepare(nextProgram, ember.RuntimeOptions{
    Host:   gameHost,
    Limits: limits,
    // Prepared may be a static/plugin bundle. When nil, Ember attempts
    // reload-time native preparation and builds an exact replay bundle for
    // everything that cannot be native.
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

`Prepare` builds backend IR, emits and validates code, installs executable
images, exact-binds the bundle, and creates an inert Runtime. It executes no
guest code. `Activate` performs no compilation, mapping, I/O, source loading,
or guest work.

Call active behavior through `scripts.Use`. One `Use` should cover a complete
frame or host operation; the supplied `*Runtime` must not escape it. Activation
while `Use` is running returns `ErrRuntimeBusy`. A candidate becomes stale if
another candidate wins first.

Successful activation closes the old Runtime and native images. Old callbacks
and suspensions therefore remain generation-bound and fail closed or stale;
Ember does not transplant closures, stacks, tables, or module state. Keep
game/world state in the Host Adapter. Transfer surviving state only as
application-owned detached data through an explicit schema.

## Reload-time native tier

The native tier currently qualifies pure numeric functions containing numeric
arithmetic, comparisons, branches, loops, direct static calls, bounded
self-recursion, and immutable module-private numeric captures. It supports at
most eight explicit plus hidden numeric arguments and one numeric result.

The following remain canonical Machine execution:

- tables and host effects;
- mutable or escaping closures;
- varargs and coroutines;
- unsupported operations or static-call dependencies;
- qualified functions called with nonnumeric values.

Replay begins at the function entry before any effect. Qualification is based
only on Program semantics, never source names, benchmark names, or workload
hashes.

| Host process | Reload-time result |
| --- | --- |
| Darwin ARM64 | Native qualified subset plus exact Machine replay |
| Darwin x86-64 with SSE4.1 | Native qualified subset plus exact Machine replay |
| Linux ARM64 | Native qualified subset plus exact Machine replay |
| Linux x86-64 with SSE4.1 | Native qualified subset plus exact Machine replay |
| Other OS/ISA or denied executable-memory policy | Exact all-Machine bundle |

ARM64 and x86-64 use separate emitters behind one target-independent semantic
candidate. Native-to-native calls use a private register ABI; only the outer
adapter accepts argument/result pointers from Go.

The Darwin installer uses generation-owned `MAP_JIT` memory, serialized JIT
write protection, and explicit instruction-cache invalidation. The Linux
installer maps writable pages, copies the complete image, and seals them
read-execute before publication. Both use architecture-specific system-stack
trampolines. Calls hold a lease so retirement cannot unmap active code. The
implementation passes with `CGO_ENABLED=0`, but uses `purego` and one audited
private `runtime.cgocall` boundary. Hosts must treat Go toolchain and OS
security-policy upgrades as compatibility events and rerun race/checkptr plus
the focused native tests.

Native installation on Windows is not implemented yet. Its emitters
cross-build, but the runtime deliberately selects exact Machine fallback rather
than claiming native speed.

## Choosing a deployment path

| Need | Recommended path |
| --- | --- |
| Shipping release; source known at build | Static generated Go |
| Same-process editor reload on supported Darwin/Linux | Nil-bundle `PreparedRuntimeSlot.Prepare` |
| Complete behavior outside the native subset | Automatic canonical Machine replay |
| Same-process generated Go under accepted cgo/plugin limits | Optional `preparedplugin` adapter |
| Compiled reload where no native installer exists | Rebuild/re-exec or an application-owned worker |

The optional plugin adapter still loads a generated `package main` bundle from
an absolute path. Go plugins require cgo on their supported platforms, bind to
the exact Go toolchain/module graph, and cannot unload. Use unique
content-derived package and artifact paths, deduplicate identical identities,
cap generations, and restart periodically. `preparedplugin.Open` returns
`preparedplugin.ErrUnsupported` rather than selecting another mode.

Rebuild/re-exec preserves static prepared speed and reclaims old code, but
process-local state survives only through an explicit application handoff.
