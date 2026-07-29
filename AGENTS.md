# Ember Agent Guide

This is a hybrid navigation index, ownership map, and operating playbook.
Durable semantics and architecture belong in the docs linked below; this guide
should tell an agent where to look, where to change code, and what proof is
required.

Ember is a Go 1.26 module (`github.com/besmpl/ember`) implementing a Go-native,
Luau-compatible scripting runtime. Most production code intentionally remains
in the root `ember` package. `preparedworker` and `preparedworkerbuild` are the
proved public transaction/reload modules. `preparedsource` is the provisional
pure immutable generated-source value seam from ADR 0012; `cmd`, `internal`,
`scripts`, and `testdata` are supporting boundaries.

## Non-Negotiable Direction

- Do not consider backward compatibility. Ignore legacy code and libraries
  when choosing or evaluating the design. Old aliases, old engine paths,
  historical plans, and superseded ADR clauses are not architectural
  requirements; remove or update obsolete paths within task scope instead of
  extending them.
- Luau compatibility is different from Ember backward compatibility. Preserve
  only the Luau behavior currently claimed by `docs/compatibility.md` and its
  cited behavior tests, unless the task deliberately changes that claim.
- Treat the current intended architecture in `README.md`, maintained docs, and
  accepted non-superseded ADRs as authoritative. Use current code and tests to
  establish actual behavior. Do not infer direction from Git history, retired
  plans, `docs/exec-plans`, or stray root plan files.
- Keep the root package independent of Hearth and other hosts. Hosts own files,
  clocks, randomness, networking, logging, application state, orchestration,
  and process lifecycle. Ember receives explicit values and capabilities.
- Preserve deterministic ordering, explicit limits and cancellation, and
  fail-closed validation at source, bytecode, module, prepared-bundle,
  prepared-worker artifact, protocol, journal, and process boundaries.
- Preserve user work. Inspect `git status --short` before editing, do not clean
  or rewrite unrelated files, and do not create a branch, commit, push, or PR
  unless the user explicitly requests that exact Git operation.

## Read by Change, Not by Habit

Always start with `README.md`, `docs/README.md`, `docs/golang-rules.md`, and the
nearby code and tests. Then choose the smallest relevant set:

| Change | Read first |
| --- | --- |
| Values, bytecode, execution, errors | `docs/principles.md`, `docs/design.md`, `docs/public-surface.md` |
| Parser, compiler, or allocation shape | `docs/compiler.md`, `docs/compatibility.md` |
| Typed analysis or module facts | `CONTEXT.md`, `docs/public-surface.md`, analysis tests near the change |
| Program loading or host calls | `docs/embedding.md`, `docs/public-surface.md` |
| Prepared Go, worker reload, or generation lifetime | `docs/prepared.md`, ADRs 0008, 0010, and 0011 |
| Test or performance evidence | `docs/checks.md`, `performance-audit.md` when performance is in scope |

Read an ADR only when its decision owns the seam. Check its status and
supersession text before relying on it. Do not create durable plan documents by
default; follow `docs/README.md` for documentation placement.

## Ownership Map

### Source, compiler, and bytecode

- `source_pipeline.go` owns the shared parse/compile/check artifact cache and
  source-limit enforcement.
- `lexer.go`, `parser.go`, `syntax_*.go`, and `expression_shape.go` turn source
  into compact arena-owned syntax. Keep spans and source identity intact.
- `binder.go` and `compiler_plans.go` resolve lexical ownership and prepare
  compilation decisions. `compiler.go`, `emitter.go`, `register_*.go`, and
  `optimizer.go` lower those decisions into sealed `Proto` bytecode.
- `bytecode.go`, `opcode_info.go`, and `vm_dispatch_spec.go` are the semantic
  instruction inventory. When opcode behavior changes, update verifier,
  effects, execution engines, generation inputs, and differential tests as one
  coherent contract.

Do not make the parser perform analysis policy or runtime effects. Do not make
the emitter rediscover binding decisions that belong in the binder/plans.
`Proto` is immutable after sealing; runtime-local caches must not mutate it.

`internal/sprigproof` is ADR 0012's bounded architecture proof, not the Luau
compiler and not a public Sprig package. Keep its syntax, nominal checked
project facts, canonical evaluator, requested-package planner, and direct-Go
emitter language-owned. `PrepareStandalone` emits location-neutral entry
closures; `PrepareLinked` accepts exact application Go import bindings and
emits only requested reachable packages. These are explicit products, not an
adaptive mode. Only the application chooses `Layout` mounts. Sprig's only
shared producer dependency is `preparedsource`; do not extract a shared
compiler IR, package graph, backend, registry, runtime, or producer-owned
placement from conceptual similarity with Luau.

### Typed analysis

- `analysis.go` is the public analyzer/check-result boundary.
- `checker.go`, `type_*.go`, `function_analysis.go`,
  `analysis_flow_view.go`, and `analysis_refinements.go` own type facts,
  constraints, flow refinement, and diagnostics.
- `module_summary_*.go`, `module_require_facts.go`, and `type_summary_*.go` own
  copy-safe cross-module facts. `program.go` applies them dependency-first
  during `LoadProgram`.

Typed diagnostics are source-attributed results; parser failures and
cancellation are Go errors. Analysis must not load or execute runtime modules,
and analyzer globals must not become runtime globals. Keep cached syntax
environment-neutral and rebuild environment-dependent facts.

### Runtime, modules, and embedding

- `program.go`, `module_*.go`, and `invocation.go` own module graph loading,
  entrypoints, `Runtime`, host invocation, and dispatch.
- `runtime_owner.go`, `runtime_heap.go`, `slot.go`, and `value.go` own mutable
  runtime state, compact handles, roots/pins, and lifetime admission.
- `runtime_machine*.go` plus `runtime_program_image.go` own the owner-bound
  Machine and the immutable owner-neutral code/program images.
- `vm*.go` is still the default engine for an unprepared `Runtime` and a
  differential behavior path. Prepared runtimes bind the Machine; exact replay
  returns to the Machine. Do not use the older VM shape as justification for
  new architecture.
- `runtime_resumable*.go`, `suspension.go`, and `callback.go` own cooperative
  suspension and retained callback lifetimes.
- `base_*.go` and `baselib.go` own pure standard-library globals. Host services
  do not belong there.

One `Runtime` owns mutable execution state and admits one active operation.
Callbacks, suspensions, roots, pins, coroutines, and prepared generations must
have explicit owners, busy behavior, and idempotent cleanup. Host-provided
tables and globals remain caller-owned and require caller synchronization when
shared.

### Prepared execution and worker generations

- `preparedsource` owns effect-free immutable `Set` and `Layout` values,
  lowercase-ASCII portable names, bounded generated-Go/embed policy,
  canonical ordering, and framed delivery identities. It does not own
  filesystem materialization, Go type/build validity, a language ABI, or a
  compiler/runtime interface. The working-tree S3a Luau producer/materializer,
  S5a out-of-module static producer, internal Sprig compiler, and external
  checked Seed compiler consume it; the mixed fixture composes the latter two
  without another seam. The static shape fixture proves no Ruby or Sprig
  semantics; the Seed fixture proves only the extension mechanism for its own
  bounded language. Retention remains gated on the S4 worker consumer in ADR
  0012.
- `runtime_backend_program.go` and `runtime_backend_ir.go` lower immutable
  Program/CodeImage data into verified backend IR.
- `runtime_backend_go*.go` emits deterministic static prepared Go with guarded
  fast paths and exact pre-effect replay.
- `runtime_prepared_*.go` and `runtime_prepared_bundle.go` own the concrete
  Luau artifact, its immutable prepared-source Set, generated-bundle creation,
  and exact Program binding. `cmd/emberc` owns the one-leaf Layout plan and
  fail-closed embedded filesystem materialization.
- `preparedsource/testdata/externalproducer` is an independently versioned
  static-delivery proof. Its producer owns Sets, its application tooling owns
  Layout and a clean temporary root, and its generated application must retain
  no Ember or producer dependency. Do not grow it into a shared guest runtime
  or cite its Ruby/Sprig-shaped APIs as language compatibility.
- `preparedsource/testdata/externalcompiler` is an independently versioned real
  compiler proof. Keep its Seed lexer, parser, checker, evaluator, diagnostics,
  identities, and emitter external and concrete. It may depend only on
  `preparedsource`; its generated application must retain neither compiler nor
  delivery code. Do not extract a compiler kit from its shallow overlap with
  Sprig or turn the fixture into a public language claim.
- `preparedsource/testdata/mixedcompiler` is application-owned proof tooling
  that invokes the real internal Sprig and external Seed compilers, then
  combines only their Sets through one Layout and concrete typed handler. Keep
  call order, domain policy, context, distinct limits, and the closed result
  record explicit in the application. Its generated module must retain neither
  compiler nor delivery code. Do not extract a cross-language package graph,
  ABI, `Compose` facade, registry, or generic value from this fixture.
- `preparedworker` owns the typed transaction ledger, durable resolution,
  embedded/process adapters, quiescent candidate activation, process
  supervision, retirement, and bounded cleanup behind `Runner` and `Reload`.
- `preparedworkerbuild` owns the explicit content-addressed Go build and
  publication effect for immutable `PreparedGoArtifact` values.
- `internal/preparedworkerartifact` and `internal/preparedworkercontract` own
  private build/protocol identity derivation. Application fixtures remain
  internal and must consume the public worker interface.

Static generated Go is trusted application build output, not a sandbox.
Worker construction is an explicit expensive host effect. Never add hidden
source loading, compilation, file watching, registration, process launch, or
cache mutation to imports, constructors, invocation, or activation.

Workers accept immutable builds and closed application records, never a remote
`Runtime` or fine-grained host RPC. Hosts own schemas, quiescence, build/watch
policy, cache policy, and application effects. Release callers retain only
`Runner`; development receives separate `Reload` authority.

## Engineering Boundaries

- Prefer a deep private implementation behind the existing small public
  surface. Add a package or exported name only when concrete caller behavior
  proves the split or API reduces complexity.
- Convert messy input to compact, validated internal data early. Keep policy
  decisions separate from effects, and pass dependencies explicitly.
- Do not add a dependency unless the task requires it and it clearly removes
  more complexity than it introduces. `x/sys` supports reviewed worker process
  ownership. It is not a license for general dynamic loading or platform
  coupling.
- The portable runtime is no-cgo. Do not add `import "C"`, foreign source or
  object files, plugin loading, alternate dynamic loaders, executable-memory
  work, or unreviewed linkname use. The explicit
  `preparedworkerbuild` toolchain build and `preparedworker` launch owners are
  the only reviewed process-execution classes. `scripts/check-purego` enforces
  this boundary.
- `unsafe` is limited to proved representation seams such as compact
  `Value`/heap storage. Any expansion requires a narrower safe interface,
  layout and lifetime tests, checkptr coverage, and platform-specific proof.
  Never let Go pointers escape their valid lifetime through integer or foreign
  representations.
- Keep tables, iteration, metatables, calls, value-list adjustment, errors,
  limits, and cancellation identical across applicable VM, Machine, and
  prepared paths. A fast path must side-exit before effects when its guard
  cannot prove exact behavior.
- Performance changes require retained benchmark evidence from the repository
  runners; intuition and one local timing are not acceptance evidence. Do not
  run long parity, fuzz, or performance captures unless the task needs them.

## Generated Code and Test Fixtures

Never hand-edit a file headed `Code generated ... DO NOT EDIT`.

- `vm_dispatch_spec.go`, `bytecode.go`, `vm_dispatch_template.go.tmpl`,
  `runtime_machine_template.go.tmpl`, and `cmd/ember-vmgen` generate
  `vm_dispatch_generated.go` and `runtime_machine_generated.go`:

  ```sh
  go generate ./...
  go run ./cmd/ember-vmgen -check
  ```

- `internal/preparedfixture/prepared_generated.go` is owned by
  `cmd/emberc`'s `TestExternalPreparedFixtureIsFresh`. Its opt-in update path
  is:

  ```sh
  EMBER_UPDATE_EXTERNAL_PREPARED_FIXTURE=1 \
    go test -run '^TestExternalPreparedFixtureIsFresh$' ./cmd/emberc
  ```

  Root `runtime_backend_*_generated_test.go` files have their own checked
  update mechanisms. Regenerate only through the owning test, then review both
  source and generated diffs. Do not invent a blanket update command.
- `go run ./cmd/emberc -check <manifest.json>` is the application-manifest
  freshness path described in `docs/prepared.md`; it is not a repository-wide
  generator.

## Verification

Use the smallest proof first and broaden according to the changed boundary.
Most focused tests target the root package:

```sh
go test -run '^TestExactName$' .
go test -count=1 ./preparedsource
go test -count=1 ./preparedworker
go test -count=1 ./preparedworkerbuild
```

For cross-engine semantics, add or run public source-to-result tests plus the
nearest VM/Machine/prepared differential tests. For public behavior, prefer
`package ember_test`; use same-package tests for private representations and
invariants. For compatibility claims, update `docs/compatibility.md` and ensure
its cited test name exists. Control context, limits, time, randomness,
environment, filesystem, and host callbacks.

Repository checks are exact:

```sh
scripts/check-lane root       # currently: go test -count=1 ./...
scripts/check                 # format check, script tests, Go tests, pure-Go policy, diff check
go build ./...                # separate CI build proof
```

`scripts/check-fast` currently delegates to `scripts/check`; do not run both
expecting different coverage. `scripts/check-full` adds vet, race, and checkptr
and is run only when explicitly requested. Otherwise add focused strong checks
when the risk demands them:

```sh
go vet ./...
go test -race -count=1 ./...
go test -gcflags=all=-d=checkptr=2 -count=1 ./...
CGO_ENABLED=0 go test ./...
```

Run `go build ./...` for public interface, generator, package, platform, or
build-tag changes. Worker process/platform changes require the applicable CI
target and the tests named in `docs/checks.md`; do not claim cross-platform
proof from a local host. Performance acceptance uses the exact pinned capture
commands and clean environment rules in `docs/checks.md`.

For documentation-only changes, run:

```sh
git diff --check
perl -ne 'print "$ARGV:$.:$_" if /[^\x00-\x7F]/' AGENTS.md
awk '/[ \t]$/ { print FILENAME ":" FNR ": trailing whitespace" }' AGENTS.md
```

Before reporting any change, inspect `git status --short`, `git diff --check`,
and the file-scoped diff. Confirm only intended files changed, generated output
is fresh when relevant, and docs, tests, public comments, and compatibility
claims agree. Report changed files, exact checks and results, remaining risks,
and anything deliberately left out of scope.
