# ADR 0011: Use Supervised Static-AOT Worker Generations

Status: Accepted. Supersedes ADR 0010's reload, prepared-slot, native-code,
executable-memory, and plugin clauses.

## Context

Static prepared Go is Ember's only broad execution path with retained all-37
evidence at or better than Luau. It is also ordinary no-cgo Go code and keeps
exact Machine replay for behavior that a generated fast path cannot prove.
Its original limitation was build time: source unknown to the parent
application's Go build could not become compiled code in that process.

The removed same-process native tier solved loading for a small numeric subset,
not general performance. Completing it would have duplicated Ember's values,
effects, stacks, continuations, cancellation, debugging, and two ISA backends.
Go plugins require cgo on their supported systems, cannot be unloaded, exclude
Windows, and bind to exact Go build identity. Rebuilding the complete host is
fast but also replaces the editor, renderer, sockets, and every other
process-local owner.

A process seam is viable only when it surrounds one complete application
transaction. Per-value or per-host-function RPC would erase the prepared speed
advantage. Two materially different host shapes - a rich game turn and a job
request - proved the same typed transaction interface before it was promoted.

## Decision

Ember uses a **supervised static-AOT worker** for development hot reload. A
stable host builds a content-addressed generated-Go worker executable while the
active generation continues serving, restores detached state into an inert
candidate, follows committed work, and changes one route at a quiescent safe
point.

The public seam is the `preparedworker` package. Its complete steady-state
capability is:

```go
type Runner[Q, R any] interface {
    Apply(context.Context, Operation[Q]) (Completion[R], error)
    Resolve(context.Context, Operation[Q]) (Resolution[R], error)
    Close(context.Context) error
}
```

Development receives a separate capability:

```go
type Reload interface {
    Prepare(context.Context, Build) (*Candidate, error)
}
```

`OpenEmbedded` returns only `Runner`. `OpenDevelopment` returns distinct
`Runner` and `Reload` values backed by one owner; `Runner` does not implement
`Reload`, so release or steady-state callers cannot recover reload authority by
type assertion. `Build` and `Candidate` are opaque owner-qualified values.

The implementation is a deep module. Journals, wire identities, protocol
frames, process backends, candidate routing, retirement, and artifact
descriptors remain private. The separate `preparedworkerbuild` package owns the
explicit Go-toolchain effect.

### Closed transaction contract

An application supplies four closed, versioned, bounded records: request,
result, checkpoint, and effect. Codecs must be canonical: decoding and
re-encoding accepted bytes reproduces the same bytes. The contract identity
binds schema names, codec version, every byte bound, and the maximum effect
count.

One admitted operation commits a single immutable record containing:

- the exact request identity and sequence/base revision;
- the canonical result;
- an optional detached checkpoint, which is the sole quiescence evidence; and
- the ordered effect outbox.

Exact duplicates replay the stored completion without guest entry. A payload
conflict, base-revision conflict, or sequence gap fails before guest entry.
Effects publish only after commit with stable ordered `DeliveryID` values.
External sinks must be transactional or idempotent under those IDs; Ember
cannot make an arbitrary sink atomic with its journal.

Any backend error after admission is terminal for that generation. A domain
rejection is a committed typed result, not an ambiguous Go error. Durable
lookup classifies uncertainty as committed, not committed, or unresolved.
Unresolved work quarantines and is never blindly re-entered.

### Process seam

The worker owns one complete prepared execution island: Program, Runtime,
Machine fallback, generated bundle, module cache, globals, mutable script
values, callbacks, and suspensions. The host sends one request projection and
prior effect completions; the worker returns one decision with commands,
effects, and checkpoint data.

`ember.Value`, tables, userdata, host functions, callbacks, suspensions,
closures, frames, and module caches never cross the seam. Frequently queried
world data belongs in the request projection or worker-local state. External
I/O remains host-owned and returns as ordered effects. Fine-grained synchronous
RPC is prohibited; hosts needing it use embedded execution.

EPW2 uses bounded deterministic binary frames over inherited pipes. Normal
`Apply` consists of one request/decision exchange. Shared memory or a bulk
transport is not part of the architecture unless retained profiles show byte
copying, rather than synchronization count, dominates a real host budget.

### Artifact construction and identity

`Program.GeneratePreparedGo` creates an immutable generated artifact for source
first observed after the parent build. `preparedworkerbuild.Build` overlays it
at the worker package's generated source path without mutating the application
module tree.

The published build identity binds at least:

- the reconstructible Program recipe, Program hash, and Proto inventory;
- generated Go and the complete worker-wrapper source closure;
- prepared ABI and Ember semantic version;
- protocol and typed transaction contract;
- Go module graph, exact selected Go installation, target, tags, and the
  versioned fixed build policy; and
- the executable digest in the publication manifest, with the configured
  executable bound enforced as admission policy on publication and every cache
  selection rather than used to fragment the pre-link build identity.

The host names one explicit Go command. The builder resolves it to one real
executable, disables toolchain auto-selection, cgo, workspace mode, implicit
PGO, host-native ISA selection, and inherited build flags, then derives both
the commands it executes and the descriptor fields from the same immutable
policy. The portable policy uses the baseline target ISA and `-pgo=off`.

Toolchain identity is content based, not a short `go version` label. It frames
the exact resolved Go executable, the bounded complete `GOTOOLDIR`, the
selected standard-library source closure, and `GOROOT/pkg/include`. This is
preferred to a hard-coded list of tool names, which is vulnerable to toolchain
layout changes, and to hashing all of `GOROOT`, which adds unrelated churn and
work. The builder deliberately keeps no persistent metadata fingerprint cache;
source and toolchain installations are immutable host inputs for one `Build`,
and it recaptures output-changing inputs after compilation to reject observable
drift.

Publication is content-addressed and atomic. The host hashes and verifies the
executable before launch. The worker reconstructs its embedded Program/bundle
identity and reports the compiled build identity in `READY`; any mismatch kills
only the candidate and leaves active work unchanged.

Build, watch, cache eviction, and source selection are explicit host actions.
No import, constructor, invocation, or activation invokes the Go toolchain.

### Restore, follow, and activation

1. Build, verify, and launch a candidate while the active worker serves.
2. Restore the latest durable quiescent checkpoint. Migrate its versioned
   envelope deterministically inside the candidate.
3. Replay/follow later committed records without republishing effects.
4. Warm and verify the candidate, then wait until it is caught up.
5. At an idle quiescent head, `Activate` performs a bounded pointer swap.
6. Admit no new work to the retired worker; one owner closes and reaps it.

Preparation may perform I/O, process launch, codecs, replay, migration, and
guest warmup. Activation performs none of those things. A candidate behind the
head waits/follows; a caught-up candidate at a nonquiescent head cannot
activate. Failed preparation or migration leaves the active generation
untouched.

Callbacks and suspensions remain generation-local. A durable subscription must
be represented in detached application state and recreated. No Ember object is
transplanted between generations.

### Ownership and failure

The parent owns bounded inherited pipes, one process tree, and exactly one
`Wait`. Cancellation, protocol failure, guest failure, or parent-lifetime loss
terminates and reaps the complete owned worker. Candidate retirement has one
bounded reaper; a timed-out or failed close remains attributable and retryable.
Runner close keeps exclusive journal ownership until module/process cleanup
actually succeeds.

The worker is trusted application code, not a hostile-code sandbox. Process
separation contains ordinary worker crashes and reclaims code/state, but it is
not a security boundary against malicious generated code.

### Portability and admission

The worker path uses no cgo, Go plugin, custom dynamic loader, executable
memory, assembly call bridge, or private Go runtime call. It targets Darwin,
Linux, and Windows on ARM64 and x86-64.

Cross-compilation proves construction only. Target-native CI must exercise
launch, identity handshake, apply, reload, cancellation, retirement, and close.
The retained production admission contract additionally requires:

- exact all-37 semantics and generated-Go median `<=1.00x`/p90 `<=1.05x`
  pinned Luau;
- worker/embedded slope `<=1.50x` for representative host work;
- one normal EPW2 exchange and directly observed steady-state codec/IPC p99
  within 10% of the host budget; independent durable-turn timings must not be
  subtracted to estimate transport cost;
- exactly 1,024 alternating independent-generation swaps with bounded process
  and resource lifetime; and
- physical ARM64 and x86-64 receipts in addition to all six target builds.

`docs/checks.md` owns the exact commands, environment fingerprints, and
evidence schema. Architecture acceptance does not turn cross-build output or a
single local timing into a platform performance receipt.

## Consequences

- Changed source receives the same broad static prepared-Go optimization
  opportunity as an embedded release.
- Build, launch, restore, and warmup move off the active transaction path;
  activation stays small and deterministic.
- Old generated code and runtime state are reclaimed by process termination,
  without cgo or executable-memory policy.
- Hot reload requires explicit serializable state and coarse data flow. That is
  a game/application architecture constraint, not a transparent Runtime swap.
- Source-to-ready includes the Go toolchain and may take seconds. Content
  addressing and build caching reduce repeated work but do not hide this cost.
- Embedded prepared execution remains marginally faster when IPC is unwanted;
  development worker overhead does not affect a shipped embedded game.

## Alternatives considered

- **Whole-process rebuild/re-exec:** fastest when the complete host may restart;
  retained as a host choice, not transparent editor reload.
- **Complete relocatable native AOT:** rejected because it duplicates a full
  runtime, two ISA backends, executable-memory policy, stack/unwind/debug data,
  and reclamation without a measured general advantage over generated Go.
- **Partial numeric native tier:** removed because general workloads replayed
  through the slower Machine and could not satisfy the target.
- **Go plugin or custom shared-library loader:** rejected for cgo, platform,
  identity, unload, and stale-pointer constraints.
- **Chatty remote Runtime:** rejected because fine-grained crossings erase the
  prepared performance advantage and expose Ember internals on the wire.
- **Wasm/foreign runtime:** rejected by retained performance experiments and the
  no-cgo Go-native runtime direction.
- **One generic loader interface:** rejected. Static bundles, supervised
  processes, and Machine replay retain distinct trust, state, failure, and
  close semantics.
