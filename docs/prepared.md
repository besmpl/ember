# Prepared execution

Prepared execution accelerates an exact loaded `Program` while preserving the
canonical Machine as its semantic fallback. Ember has two deployment adapters
for the same generated-Go lowering:

- **Embedded release:** generate deterministic Go, compile it with the
  application, and bind its bundle explicitly to a `Runtime`.
- **Supervised development worker:** generate the changed Program, build a
  content-addressed no-cgo worker executable while the active worker continues,
  restore detached application state, and activate it at a quiescent point.

Prepared artifacts and worker executables are trusted application code, not a
sandbox. Generation, Go compilation, file watching, cache policy, and
activation are explicit host actions. Imports, constructors, and invocation do
not hide these effects.

## Static manifest

`emberc` is the standard-library-only file/manifest wrapper around
`Program.GeneratePreparedGo`. The manifest must be a regular non-symlink file
directly inside the module root beside a regular non-symlink `go.mod`. Paths
are relative to that manifest/module root, not the process working directory.

```json
{
  "package": "preparedgame",
  "output_dir": "internal/preparedgame",
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

`output_dir` is the canonical lowercase-ASCII `preparedsource.Layout` mount.
Every component must already exist with exact spelling and be a real
non-symlink directory. The producer owns the fixed
`prepared_generated.go` leaf; callers cannot rename it.

Generate or replace that leaf with:

```sh
go run github.com/besmpl/ember/cmd/emberc ./ember-prepared.json
```

CI can verify byte-for-byte freshness without changing the file:

```sh
go run github.com/besmpl/ember/cmd/emberc -check ./ember-prepared.json
```

Malformed manifests, undeclared modules, bounded-input violations, unsafe or
case-aliased paths, protected-input aliases, unmanaged existing targets, and
oversized output fail before the destination changes. A fresh owned file is a
true no-op. A stale or missing file is staged, synced, closed, revalidated, and
renamed in the destination directory; unrelated siblings are never scanned or
deleted. Same-directory rename provides atomic namespace replacement on the
supported POSIX path. Ember makes no universal Windows replacement-atomicity
or power-loss-durability claim. Unsupported individual Protos produce exact
nil bundle entries and use canonical Machine replay; they do not prevent
supported siblings from being generated. `-check` uses the same admission
path, reports missing or stale output, and never rewrites it.

## Static runtime binding

Import the generated application package and pass its bundle explicitly:

```go
runtime, err := program.NewRuntime(ember.RuntimeOptions{
    Prepared: preparedgame.Bundle,
})
```

The bundle is bound to the exact prepared ABI, semantic version, Program hash,
and module/Proto inventory. A mismatch returns `*ember.PreparedBundleError`
before runtime-owner mutation and never silently selects a different artifact.
Passing no bundle to `Program.NewRuntime` preserves the ordinary runtime path.

Public generated helpers share one opaque `PreparedContext`. The compiler emits
`Continue` checks at prepared-function entries and control-flow backedges, so a
live cancelable invocation remains on the static-AOT path without making an
unbounded loop ignore its context. Observed cancellation side-exits before an
effect and the canonical Machine reports the context error. A runtime with any
configured `ExecutionLimits` remains on the Machine path so instruction and
resource accounting stay exact. The prepared ABI rejects older generated
bundles that do not carry this safe-point contract.

For source first observed after the parent application was built, use the
in-memory artifact interface:

```go
artifact, err := program.GeneratePreparedGo(ember.PreparedGoOptions{
    Package:            "preparedgame",
    EmbedProgramRecipe: true,
})
```

`PreparedGoArtifact` owns one immutable `preparedsource.Set` with the fixed
`prepared_generated.go` leaf. `Sources()` exposes that value; application
placement therefore cannot change producer identity. Its Luau identity binds
the raw generated source and exact reconstructible Program recipe. The legacy
raw digest and `WriteTo` view read the same Set leaf only while worker builder
V1 still needs the singular source contract.

### Provisional multi-language source values

The pure `preparedsource` package now has a working-tree S2 candidate. A `Set`
owns one canonical generated Go package, and a `Layout` owns application mount
placement separately. Construction is immutable, bounded, deterministic,
stdlib-only, and effect-free; it performs source-policy validation but not Go
type checking, import resolution, filesystem materialization, or compilation.

The working-tree S3a candidate now proves one real Luau producer and one
one-leaf embedded materializer through the same Set/Layout values. This is not
the active language-neutral worker artifact path yet:
`preparedworkerbuild.Program` and descriptor V1 retain their Luau-specific
singular-artifact contract until S0 permits the atomic S4 builder cutover. No
caller should infer language-neutral worker support merely from the value
package or embedded consumer.

The external S5a fixture under `preparedsource/testdata/externalproducer`
separately proves the static extension boundary. Its producer module imports
only `preparedsource`, returns independent Ruby-shaped and Sprig-shaped Sets,
and never selects mount paths. Application-owned tooling constructs a
two-mount Layout and materializes its canonical Mounts/Files into an exclusively
claimed temporary root. The proof deletes the complete producer module before
using a fresh offline Go cache to list, test, build, inspect, and execute the
resulting zero-dependency application. Its direct package graph contains only
the application and two concrete generated packages, and its build metadata
records `CGO_ENABLED=0`.

That fixture demonstrates delivery, placement, owner/detachment shape, typed
functional-composition shape, and absence of a shared steady runtime. It does
not implement or claim Ruby/Sprig parsing, semantics, compatibility,
performance, worker overlays, or hot reload. The real worker consumer remains
S4/S5b-gated; the external proof adds no public materializer, Backend, language
interface, or registry.

A later disposable S4 preflight confirmed that a real package-directory
`doc.go` anchor does not make Go 1.26 discover nonexistent overlay-only Go or
embed files: `go list -deps -json -overlay` enumerated only the physical
anchor, so every supplied Layout leaf was selected zero times. The proposed
language-neutral builder therefore will not generalize V1's replacement
overlay. After the retained S0 gate, the revised S4 direction is an
application-owned minimal standalone worker module in a fresh physical root:
the application/host owns its fixed wrapper and handler sources, Layout
materialization, immutable lifetime, and cleanup; the builder verifies but
never writes the root and owns only Go discovery/build identity plus executable
publication. This is proposed architecture, not current worker support or a
public filesystem materializer.

A second disposable observer then passed that revised mechanism. Two
byte-identical fresh module roots produced the same relevant package BuildIDs
and `ruby-v1|12|15|27`; a separate v2 root removed the v1 implementation leaf,
kept unrelated Sprig/support BuildIDs stable, changed Ruby/application
BuildIDs, and produced `ruby-v2|17|15|32`. Ordinary offline cgo-free Go selected
every reachable Layout Go/embed leaf exactly once, while an unreachable mount
was rejected and altered or missing physical bytes failed before Go ran. The
proof also showed that accounting must use Go import path, source kind, and
basename rather than an absolute `Dir`, because macOS may report equivalent
temporary roots through `/var` and `/private/var`. The proof-only harness was
removed after its focused checks; production S4, worker reload, retained
build-to-READY performance, and S0 remain unproved.

A third disposable observer narrowed future S4 discovery on Go 1.26.4. Package
listing selected only `Dir`, `ImportPath`, `Standard`, `Incomplete`, the Go,
foreign, assembly, prebuilt, header, and embed inventories, plus `Error`;
module listing selected only `Path`, `Version`, `Main`, `GoVersion`, `Sum`,
`GoMod`, and `Replace`. Across controlled chains of 64, 128, and 256 generated
packages, the selected package stream preserved exact decoded values, source
inventories, and production wrapper/standard/module digests while occupying
11.8504%, 7.2454%, and 3.7786% of unrestricted JSON. Unrestricted output
contained 2,016, 8,128, and 32,640 unused recursive `Deps` references.

Field selection is only top-level: nested `Error` and `Replace` values still
carry fields Ember does not consume. Revised S4 must therefore hard-cap raw
stdout below the JSON decoder, bound and continue draining stderr, and enforce
package/module/source/metadata counts separately. Its process owner must own
the pipes, wait for the direct child concurrently with both drains, bound
descriptor retention after that child exits, and join decoding, limit,
context, stderr, close, and exit failures. This is direct-child settlement, not
process-tree termination. The proof exercised raw/stderr overflow,
cancellation, malformed output with a simultaneous exit failure, and inherited
descriptors; six target environments were projection-only, not cross-build or
native-execution evidence.

The 1,227-line harness was deleted after independent semantic and lifecycle
review. The current V1 builder is unchanged. A subsequent three-vector review
selected the simpler S4 composition rule: general Layout validity is not a
promise of worker admission. Actual repeated Layout bytes consume the private
complete non-standard source ceiling first; every selected fixed application
or versioned dependency source consumes the checked residual. Repeated mounts
count repeatedly, every expected Layout leaf must be selected exactly once, and
standard-library sources retain their separate toolchain closure ceiling.

Raw package/module JSON, stderr, retained semantic and derived-key bytes,
package/module/source counts, replacement nodes, and module-file work remain
independent private ceilings. An input must satisfy their intersection; Ember
does not derive one brittle worst-case JSON formula or promise that all maxima
are simultaneously attainable. S4 must add aggregate replacement-node and
unique `go.mod`-byte work bounds because V1's top-level module count,
replacement depth, and per-file cap otherwise permit enormous repeated hashing.
Classification, byte equality, residual accounting, and hashing belong in each
required pre/post capture, not another source pass or public limits API.

Initial S4 also keeps Go replacement policy deliberately narrow. It parses the
stable main `go.mod` once with the selected pinned Go tool before discovery,
accepts ordinary versioned modules and module-to-module replacements with a
nonempty replacement version, and rejects every versionless filesystem RHS.
That includes both absolute paths and apparently contained paths such as
`./deps/local`. Application-local packages stay in the one standalone main
module; separate dependencies must be provisioned as immutable versioned
module inputs. This is proposed S4/V2 policy, not behavior newly claimed for
the unchanged V1 builder, and it creates no language or public replacement
API.

Versioned-only does not make an ambient module cache safe or offline. S4 still
needs an explicit immutable module-cache owner, fixed no-network environment,
exact module attribution, selected-file hashing, and complete pre/post
recapture. It was selected at **9.6/10 fail-closed safety** and **7.2/10 local
development usefulness** because it removes a second path, hardlink, and ABA
admission problem from the initial cutover.

The proposed S4 dependency owner is a **logical application-scoped sealed
module-supply view**, rated **9.6/10**. Its target-time representation is an
explicitly leased immutable `GOMODCACHE`, but its identity contains only the
admitted module graph and selected package/source closure. Moving the physical
cache or adding an unrelated immutable module must not change module, package,
source, Go BuildID, executable, or application identity. Production must not
walk, hash, or copy a whole module cache to enforce this rule.

Provisioning is a separate host effect. It may use an authorized proxy or
network, a mutable staging cache, and the pinned Go tool, then atomically
publishes a complete immutable cache epoch. Target discovery/build borrows the
application-root and supply-view read leases and closes ambient Go state:

```text
GOMODCACHE=<leased immutable root>
GOPROXY=off
GONOPROXY=none
GOPRIVATE=
GOSUMDB=off
GONOSUMDB=none
GOVCS=*:off
GOAUTH=off
GOINSECURE=
GOENV=off
GOWORK=off
GOTOOLCHAIN=local
CGO_ENABLED=0
```

`HOME`, `GOPATH`, temporary storage, and `GOCACHE` are also explicit private
paths. `GOCACHE` may remain writable and reusable as derived acceleration; it
never admits source. Target commands use `-mod=readonly`. Adding dependencies
creates another unpublished cache epoch instead of mutating an active one, and
concurrent targets/retries may share read leases.

The S4 identity binds exact main `go.mod`/`go.sum` bytes; original and
replacement path/version plus applicable `Sum` and `GoModSum` values; selected
`GoMod` bytes; package `Module` attribution; and selected Go/embed/other
compiler inputs under logical module-relative names. Absolute application,
cache, proxy, and toolchain roots are containment evidence only. Hold both
input leases through post-build recapture and artifact publication, then
release them before `Reload.Prepare`. A worker generation retains only its
opaque executable artifact, never a source or module-cache lease.

This is private Go delivery policy, not a language seam. Do not add it to
`preparedsource.Set`/`Layout`, Ruby/Luau/Sprig compiler or runtime APIs,
application records, `preparedworker.Build`, `Reload`, or a generic Backend or
dependency-supply interface. Go-emitting languages share the mechanism after
lowering without knowing it. A future non-Go artifact builder owns a sibling
supply mechanism behind the same completed-build/reload transaction. If S4
needs caller input, prefer one narrow explicit Go-builder cache root/capability
over a supply facade.

A disposable Go 1.26.4 proof used an ordinary module, a versioned replacement,
`internal`, a build tag, and a nested embed. A complete write-denied cache
succeeded with proxy off and retained sums plus module roots. Two unrelated
application roots and physical caches, independent fresh build caches, and an
extra valid but unselected module produced identical selected identities,
BuildIDs, executables, and behavior while whole-cache receipts differed.
Vendor mode remained offline and compact, but could not natively list the full
vendored `all` graph and omitted dependency/replacement sums, `Dir`, and
`GoMod`; it remains a **7.2/10** fallback. Full-tree hashing and chmod were
proof-only sentinels. Cross-platform write exclusion, ABA/hardlink ownership,
multi-target behavior, warm/retry timing, RSS, cache-format portability, and
production S4 integration remain unproved. The unchanged V1 builder still does
not enforce this policy.

A reviewed Darwin/arm64 proof showed that a strict contained-relative route can
use one root and one main parser with no subtree copy, child parser, or extra Go
subprocess. That design remains an **8.9/10** revisit route, but its exact
1,757-line evidence rated **6.8/10**: it rejected valid nested Go embed paths
such as `assets/message.txt`, charged aggregate selected-source work only after
reading the next file, and lacked an aggregate proof-Layout byte ledger. The
disposable was deleted rather than turned into an Ember-specific flat-embed
dialect. Revisit only for a concrete application that cannot use one main
module or provisioned versioned inputs, and require nested-embed,
charge-before-work, portable no-follow/reparse and hardlink/ABA ownership, plus
retained fresh-cache multi-target proof.

Numeric near-limit performance, retained production behavior, and S0 remain
unproved. These results are structural architecture evidence, not current
worker support. See
[ADR 0012](adr/0012-language-owned-runtimes-and-staged-prepared-sharing.md) for
the exact receipt and exclusions.

## Typed transaction module

The public `preparedworker` package is a deep module around one serialized
application transaction stream. Release code retains only:

```go
type Runner[Q, R any] interface {
    Apply(context.Context, Operation[Q]) (Completion[R], error)
    Resolve(context.Context, Operation[Q]) (Resolution[R], error)
    Close(context.Context) error
}
```

`Apply` atomically commits one canonical result, optional quiescent checkpoint,
and ordered effect outbox. An exact duplicate replays the stored completion
without entering guest code. A conflicting request or sequence gap fails before
guest entry. `Resolve` performs durable uncertainty lookup and never retries the
guest operation.

Applications define four closed, versioned records: request, result,
checkpoint, and effect. Each has a deterministic codec and hard byte bound.
Ember values, tables, userdata, callbacks, suspensions, closures, Runtime
owners, and module caches never cross the process seam.

The embedded adapter receives an application handler factory:

```go
runner, err := preparedworker.OpenEmbedded(ctx, options, restoreHandler)
```

The factory owns the exact generated bundle and recreates one complete handler
from a detached checkpoint. `OpenEmbedded` returns only `Runner`; release code
cannot recover reload authority through a type assertion.

## Building a changed worker

`preparedworkerbuild` owns the explicit Go-toolchain effect. A host supplies:

- one explicit Go command, resolved to the exact real executable used for
  discovery, listing, and compilation;
- the immutable `PreparedGoArtifact` and its virtual generated source path;
- the worker wrapper package that calls `preparedworker.ServeWorker`;
- the typed transaction contract;
- module directory, target, tags, output directory, name, and executable bound.

```go
buildOptions := preparedworkerbuild.NewOptions(contract)
buildOptions.GoCommand = goCommand
buildOptions.ModuleDir = moduleDir
buildOptions.Program = preparedworkerbuild.Program{
    Artifact:    artifact,
    GeneratedGo: generatedTarget,
}
buildOptions.WorkerPackage = "./internal/gameworker/cmd/worker"
buildOptions.Target = preparedworkerbuild.Target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
buildOptions.OutputDir = cacheDir
buildOptions.Name = "game-worker"
buildOptions.MaxExecutableBytes = 128 << 20

result, err := preparedworkerbuild.Build(ctx, buildOptions)
build, err := preparedworker.OpenBuild(result.Manifest, 128<<20)
```

The builder overlays generated source into an owned temporary directory at the
caller's one already existing `GeneratedGo` path; this is file replacement, not
a general mechanism for injecting new package leaves. It does not require the
replacement bytes to exist at the parent application's `go build`. It resolves
`GoCommand` once, uses only that executable, sets
`GOTOOLCHAIN=local`, and derives executed arguments, pinned environment,
baseline target ISA, and descriptor fields from one versioned build policy.
The portable policy is no-cgo and uses `-pgo=off`; there is no implicit host
profile or native-ISA tier.

The pre-link content identity binds the Program recipe, generated source,
complete selected wrapper and standard-library source closures, module graph,
the exact Go executable, bounded complete `GOTOOLDIR`, Go include tree, build
tags and policy, target, transaction contract, and protocol version. The
publication manifest separately binds the executable digest and enforces the
host's maximum executable size on every cache selection. Cache publication is
atomic and a corrupt entry is rejected rather than replaced or selected.

The module tree and selected Go installation are immutable inputs for the
duration of `Build`. The builder recaptures the output-changing source, module,
and toolchain inputs after compilation and fails without replacing the prior
selection when it observes drift. It intentionally has no persistent
path/mtime toolchain-fingerprint cache; add one only if retained build-to-READY
profiles prove hashing material and an immutable CAS can own invalidation.

## Development reload

Open the initial verified build with separate steady and reload capabilities:

```go
runner, reload, err := preparedworker.OpenDevelopment(ctx, options, initialBuild)

candidate, err := reload.Prepare(ctx, nextBuild)
if err != nil {
    return err // active work is unchanged
}
defer candidate.Close(context.Background())

// Only at an application-owned idle, quiescent point.
if err := candidate.Activate(); err != nil {
    return err
}
```

`Prepare` launches and verifies an inert worker, restores the latest durable
quiescent checkpoint, runs schema migration and warmup inside the candidate,
and follows commits made while active work continues. `Activate` succeeds only
when the candidate is caught up and the current head is quiescent. It performs
no compilation, file I/O, protocol exchange, codec work, journal write, or
guest execution; publication is a bounded route swap.

The old process receives no new operations after activation and is reaped by
the module's single bounded owner. Exactly one process owns `Wait`; cancellation
or a terminal backend error kills and reaps the complete worker ownership tree.
Candidate and runner close operations are context-bounded, idempotent where
complete, and retryable after an incomplete cleanup.

## Commit uncertainty and effects

Any backend error after admission quarantines that generation. A domain
rejection belongs in a successfully committed typed result; it must not be an
ambiguous Go error.

When commit visibility is uncertain, `Resolve` reports one of:

- `ResolveCommitted`: replay the exact stored completion;
- `ResolveNotCommitted`: durable lookup proved absence;
- `ResolveUnresolved`: the module still cannot prove either outcome.

Unresolved work is never re-entered automatically. Effects become deliverable
only after the transaction commits. Every ordered effect has a stable
`DeliveryID`; external sinks must use it as an idempotency key or participate
in their own transaction. Ember guarantees logical identity and ordering, not
atomic visibility in an arbitrary external system.

## Process shape and portability

Normal work is one EPW2 request/decision exchange. Repeated script queries use
worker-local projected state; external I/O returns as ordered effects. A host
that requires fine-grained synchronous calls should use embedded prepared
execution instead of turning every host function into IPC.

The portable worker path uses no cgo, plugins, custom dynamic loader, executable
memory, or private runtime call bridge. Worker construction cross-builds for:

| OS | Architectures |
| --- | --- |
| Darwin | ARM64, x86-64 |
| Linux | ARM64, x86-64 |
| Windows | ARM64, x86-64 |

A worker build is runnable only on its exact target. Cross-compilation proves
construction; native CI owns launch, handshake, apply, reload, cancellation,
retirement, and close behavior for each declared pair. See `checks.md` for the
paired Luau, one-exchange, transport, and resource-bounded 1,024-swap evidence
contracts.

## Choosing a deployment path

| Need | Recommended path |
| --- | --- |
| Shipping release with source known at build | Static generated Go bound directly to `Runtime` |
| Embedded engine with an application transaction schema | `preparedworker.OpenEmbedded` |
| Editor/game hot reload without cgo | Build a worker, then `OpenDevelopment` and `Reload.Prepare` |
| Source first seen after the parent build | `GeneratePreparedGo` plus `preparedworkerbuild.Build` |
| Complete behavior outside a generated fast path | Exact canonical Machine replay inside the generated bundle |
| Fine-grained synchronous host callbacks | Embedded Runtime/prepared execution, not a remote worker |
| Restarting the whole host is acceptable | Rebuild and re-exec the host process |

Whole-process rebuild/re-exec removes even the worker IPC intercept, but all
process-local application state then needs an explicit handoff. The supervised
worker keeps the editor/engine process stable and transfers only application-
owned detached schema data.
