# Prepared Go Bundles

Prepared bundles are an optional build-time acceleration path. Ember compiles
the exact loaded Program into deterministic Go source; the application builds
that source normally and supplies its exported `Bundle` explicitly when it
creates a runtime. A missing bundle keeps the ordinary dynamic Machine path.

Generated code is a trusted application build artifact, not a sandbox for
untrusted Go. Ember does not compile Go at runtime, load plugins, register
bundles through `init`, or expose Machine registers and arenas.

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
