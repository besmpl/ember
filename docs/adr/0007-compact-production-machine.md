# ADR 0007: Compact Production Machine

Status: Accepted; final-backend and performance-target clauses are superseded
by ADR 0008. The CodeImage, Machine, ownership, effect, and no-CGO decisions
remain active.

## Context

Ember's canonical interpreter executes wordcode through pointer-bearing
`Value` registers, pointer-rich VM frames, Go-object tables, and repeated
call/effect boundary setup. Narrow alternate engines previously won isolated
microbenchmarks but did not cover production programs, so ADR 0005 retained
the direct VM. That decision simplified the runtime, but it did not make its
state representation competitive with the pinned Luau interpreter across the
complete 37-workload product corpus.

The production migration therefore needs one complete state model, not another
selective execution tier. It must remain pure Go, preserve Luau-visible
semantics, handle every public/runtime effect, and keep mutable state owned by
one runtime from entry to exit.

## Decision

Replace the direct `Value`/`vmFrame` execution core with two private modules in
the root package:

1. `CodeImage` is immutable, owner-neutral, prepared once from a verified
   `Program` or `Proto`, and shareable across runtime owners. It contains only
   scalar/string constants, compact operations, descriptors, block and
   liveness metadata, source maps, effect classes, and exact guest-instruction
   accounting. It contains no owner slots, host payloads, mutable caches, or
   lifecycle state.
2. `Machine` is one owner-bound mutable world. Its hot registers,
   continuations, closures, upvalues, strings, globals, modules, tables,
   iterators, caches, and coroutine snapshots use compact scalar records and
   64-bit slots. Pointer-bearing host functions, userdata, detached values,
   errors, and leases stay in a Go-owned root/payload sidecar.

A generated pure-Go `RunBurst` kernel is the canonical execution backend. It
retires bounded work and returns a compact, replay-safe record for terminal
state, semantic effects, policy safepoints, or backend continuations. One Go
effect executor owns host calls, modules, cancellation, limits, collection,
growth, errors, and coroutine transitions. Direct script calls and stable
scalar operations remain inside the Machine.

The final target is that every public entrypoint reaches one Machine facade,
the old VM is deleted, and every one of the 37 qualified workloads passes both
independent acceptance captures at median Ember/Luau ratio <= 1.85 and p90 <=
2.00. Intermediate cutovers may not regress any already-supported baseline row
by more than 5% or exceed its frozen warmed allocation ceilings.

## Hard constraints

- Production remains `CGO_ENABLED=0`: no `import "C"`, foreign objects,
  dynamic loading, helper process, embedded Luau backend, runtime-generated
  code, executable-memory mapping, private Go ABI, or new dependency.
- Routing during migration is whole-Program and feature-based. One invocation
  uses either the old VM or the Machine from entry to exit; mutable objects are
  never mirrored and no opcode performs an old/new fallback.
- General specialization may use only semantic/runtime-derived guards. Source,
  fixture, benchmark, corpus, or Proto identity is never an execution key.
- Public imports and exports are batched at effects and ownership boundaries.
  No `Value`/slot adapter recurs per opcode, script call, loop iteration, or
  warmed stable lookup.
- Owner and generation checks happen at public, callback, coroutine, import,
  export, or effect seams. Hot internal references are owner-local dense
  indices and are reclaimed only at quiescent Machine points.
- Exact Luau-visible results, errors/frames, effects, limits, cancellation,
  iteration order, modules, callbacks, coroutines, and owner lifetime remain
  compatibility gates.

## Migration invariant

Each slice extends one production Machine vertically, proves its declared
semantic family against a separate old-VM owner, and then routes whole eligible
Programs to it. Generated decode, dataflow, accounting, effect, and quickening
metadata have one specification. Temporary adapters and losing accelerators
are deleted in the phase that rejects them. The old VM remains an oracle only
until the complete expected corpus artifact is frozen, then is deleted.

## Supersession map

- ADR 0001 still governs host/public ownership: ordinary Go values and Go GC
  own detached data and opaque host payloads. It is superseded for internal
  execution state; Go objects and Go-GC-visible references are no longer the
  default representation for hot script objects.
- ADR 0004's public source-compatibility requirement, mandatory stateless
  result promotion, typed pointer slabs, and generation check on each hot
  handle dereference are superseded. Owner-bound views/leases may deliberately
  change the public boundary; explicit detach/copy makes durable values; scalar
  arenas own hot objects; generation validation is a boundary rule. Its durable
  ownership lessons remain: owners are explicit, roots/pins have lifetimes,
  stale boundary handles fail closed, opaque Go payloads remain traced by Go,
  and collection occurs only at safe owner points.
- ADR 0005 is superseded as the final architecture. The direct
  `Value`/`vmFrame` VM remains the temporary semantic oracle, not the permanent
  canonical engine. Its evidence against partial alternate engines still
  requires broad coverage and deletion of losing paths.
- ADR 0006 remains valid evidence against an allocation-only table campaign.
  Phase 4 replaces tables because scalar tables are part of the complete
  canonical execution architecture and speed model, not because a standalone
  table allocator won a persistent allocation profile.

## Frozen diagnostic vocabulary

Untimed or separately timed diagnostics use these stable counter families:

- `guest_instructions` and `image_blocks`, partitioned by semantic family;
- `script_calls`, `script_returns`, `tail_calls`, and `result_modes`;
- `global_hits`, `global_misses`, `property_hits`, `property_misses`,
  `table_hits`, `table_misses`, `table_probe_lengths`, `table_growth`,
  `iterator_hits`, and `iterator_misses`;
- `effects` and `side_exits`, partitioned by reason;
- `quickening_installs`, `quickening_guard_hits`,
  `quickening_guard_misses`, and `quickening_reversions`;
- `imports`, `exports`, `imported_values`, and `exported_values`;
- `coroutine_transitions`, `host_transitions`, `module_transitions`, and
  `safepoint_returns`, partitioned by transition/return reason;
- `arena_capacity`, `arena_live`, `arena_retired`, `arena_reused`,
  `root_store_entries`, `pins`, and `leases`;
- `collections`, `collection_live`, `collection_reclaimed`,
  `collection_pause_ns`, and `stale_boundary_handles`.

Renaming or changing the meaning of a frozen counter requires an ADR update so
before/after profile evidence stays comparable.

## Starting evidence

The clean migration baseline is commit
`f94a61d7668c628924224164fc8275048ff6f885`. Captures ran on Darwin 24.6.0,
Apple M1 arm64, Go 1.26.4, `CGO_ENABLED=0`, and `GOMAXPROCS=1`. The shared
non-Luau environment artifact SHA-256 is
`9b83720ee86181a5ed57d66153b5c8d14c8017e2aecc15f83aab9b530be6b8e0`.

The two five-sample, 500 ms performance captures are independently bound by
fingerprints
`d7735be182aa284748e09634bba9a85fd3d26aa31e4b0e44cfe6328424690fe0`
and
`2b8650f683e863cfde3987a972f1243a9645856f9fbb6dab1b9e12b572489590`.
Their derived version-2 manifest SHA-256 is
`d7bec9389c99d2519a5a017eba0a4fc5a52594dfcf52ece61d61a3cd5648427d`.
Each row stores an A and B timing ceiling derived from the same absolute noise
envelope and normalized by that role's own median. Either independently
selected baseline role therefore preserves the same absolute-noise policy.

The two 56-row allocation/lifecycle captures are bound by directory hashes
`abb48b3155f10b0aa3b8191d8e1984f7a895c6586bfc34188c1136f17c13e0d1`
and
`2cf801eece63bf6793aab562570b5595652d9a4c74c36c6c230b338bec62478b`.
Their frozen ceiling manifest SHA-256 is
`6d275313172967f724453906763f0bfdf47f3e0837e1a273b981aa7274883207`.

The speed evidence uses pinned Luau 0.728 at SHA-256
`c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd`.
The two speed capture hashes and derived manifest hash are appended here only
after both independent quiet-machine acquisitions complete without
contamination; a failed quietness probe is not acceptance evidence.

## Consequences

This migration accepts a temporary second engine and public-boundary changes
in exchange for one coherent final runtime. Compact state, explicit effects,
and one generated semantic model create a narrow optimization seam for pure
Go, optional static Go assembly, or prepared-bundle Go AOT without duplicating
ownership or semantics.

The implementation is substantially larger than an incremental tuning slice.
Mixed-stage adapters can understate final speed, so individual slices are
judged by semantic completeness, architecture progress, allocation ceilings,
and no-regression gates rather than an arbitrary early total speedup. The final
all-37 gate remains empirical and cannot be waived by geomean.

## Alternatives considered

- Continue tuning the direct pointer-rich VM. Rejected as the final strategy
  because its costs span registers, frames, calls, accounting, objects, tables,
  and effects; isolated changes do not establish one compact ownership model.
- Revive a narrow scalar, compact-call, or loop-kernel tier. Rejected by ADR
  0005's coverage evidence and the whole-Program migration invariant.
- Port or invoke upstream Luau. Rejected by the no-CGO/no-foreign-backend
  product boundary; Luau remains an oracle only.
- Begin with assembly or generated Go AOT. Deferred until the complete pure-Go
  Machine is measured. Both must implement the same state/effect contract and
  earn retention on the full corpus.
