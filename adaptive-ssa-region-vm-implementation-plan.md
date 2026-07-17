<!-- simple-loop-plan -->

# Adaptive SSA region VM implementation plan

## Lead architecture audit

### Evidence and cutoff

At exact HEAD `2cf1e08d`, one clean Apple-M1 schema-v2 `speed2x` all-37 VM capture (`CGO_ENABLED=0`, `GOMAXPROCS=1`, pinned Luau 0.728) has a 5.705x ratio geomean: 2.439x best, 11.567x worst, arithmetic 5.359x, recursion 5.260x, arrays 4.604x, and event dispatch 8.066x. Its `slopes.tsv` SHA-256 is `e50b9a1362f63aa3f5cedd685217a3c9cbfc50f513bfc4d872e11f818f7c0846`; P1 must reproduce it twice before implementation.

The corrected Machine pair at `a4240224` is 12.221x/12.270x geomean. Fresh HEAD profiles confirm the rank reversal: Machine is slightly faster for recursion (2.86 ms versus VM 3.17 ms) but much slower for arrays (64.9 us versus 18.8 us) and event dispatch (190.6 us versus 145.7 us). Dispatch is only 37%-53% flat on arithmetic and less dominant on table/call rows, so deleting dispatch alone cannot close the gap.

| Candidate | What evidence says | Decision |
| --- | --- | --- |
| Tune or quicken the current VM | Compact wordcode, 16-byte `Value`, direct `*Table`, PICs, and compact calls are proven good; per-op tags, frames, and dispatch remain. | Keep as fallback; insufficient final architecture. |
| Cut over to compact Machine | Scalar calls help recursion; expanded operations, helper traffic, and table arenas lose broadly. | Reject as dynamic substrate. |
| Rebuild a scalar-wordcode Machine | Recombines isolated wins, but ADRs 0005/0007 show no broad coverage and current VM already has its strongest mechanisms. | Reject another state migration before specialization proof. |
| Static superinstructions/threaded handlers | Useful only for recurring shapes; Go function threading adds calls and quickening cannot scalar-replace objects or calls. | Permit as a lowering tactic, not the architecture. |
| Free-form traces | Can span calls, but path/version explosion, unstable feedback, and arbitrary side exits fight determinism and ownership. | Reject unbounded recording. |
| Runtime Go/assembly/JIT/foreign backend | Could compile straight-line code, but violates the product boundary and Go ABI/stack-map safety. | Inadmissible. |
| Bounded adaptive SSA regions over the VM | Reuses proven table/PIC fallback and existing SSA/loop/type/escape facts; removes repeated work across loops and call clusters. | **Select.** |

### Selected architecture

Build one private, data-only partial evaluator for scripts unknown at Go build time. Immutable, owner-neutral `regionTemplate`s come from a single verified optimization IR shared with prepared Go. A template covers a natural loop or bounded direct-call cluster and contains guards, typed SSA/macro-ops, snapshots, effect boundaries, and exact guest charges. `vmFunctionInstance` owns hot counters and at most two guarded versions per header; shared `Proto`/images never receive mutable feedback.

Entry occurs only at function entry or a hot backedge, never through a probe on every opcode. The generated region executor uses unboxed typed banks, compact continuations, scalar-replaced nonescaping tables/closures/tuples, and guarded direct `*Table` shape slots. Guards and potentially effectful/erroring operations exit before mutation; exact snapshots rebuild VM frames, live `Value`s, PC, limits, and error ancestry. Host calls, yields, protected calls, modules, debugging, and unproved multi-step mutations remain VM semantic islands. This keeps one complete dynamic semantic state. Prepared bundles continue using Machine independently.

The unproved premise is that a finite typed region ISA can reach parity without executable code or shape explosion. P3-P4 are an explicit architecture kill gate, not permission to accumulate a second interpreter.

## Outcome

- **Eventual goal:** Dynamic Ember scripts unavailable at Go build time match or beat pinned Luau across the complete all-37 contract while preserving production semantics and boundaries.
- **Current run:** Prove and deliver the fastest admissible architecture as an adaptive SSA region tier with exact VM fallback, or delete it after the bounded proof disproves its premise.

### Done when

- D1. Two clean exact-commit VM baselines bind all 37 results, profiles, allocations, and the architecture observer; the audit records proven wins, losses, assumptions, and rank reversals.
- D2. One immutable optimization IR owns CFG, SSA, effects, reads/writes, representation, escape, liveness, and snapshot facts for both region and prepared lowering; no public API or duplicate semantic metadata is added.
- D3. Numeric loops, recursion/direct calls, stable fields/arrays, and event dispatch each reach median `<=2.00x` Luau in two exploratory captures, with no exercised result mismatch, warmed allocation increase, or untargeted row regression over 5%.
- D4. Final dynamic evidence is two clean all-37 captures with every row median `<=1.00x` and nearest-rank p90 `<=1.05x`, exact result hashes, and no warmed B/op or allocs/op above frozen ceilings.
- D5. VM/region/prepared differentials prove values, errors/frames, metamethod order, iteration, aliases, limits, cancellation, effects, coroutines, owner isolation, and exact deoptimization at every region operation boundary.
- D6. Region execution is allocation-free after warmup; two versions/header, 64 templates/Proto, and `4x wordcode+IR + 64 KiB` per owner-Program binding are hard caps; a 100k-call churn test plateaus.
- D7. Compile/load/cold-call medians regress at most 10%; prepared all-37 parity and artifact freshness remain green; the root public surface and pure-Go boundary do not change.

### Constraints

- No CGO, foreign runtime, subprocess, plugin, executable memory/JIT, private Go ABI, `go:linkname`, assembly foundation, dependency, benchmark identity, or source-shaped selector.
- Keep the root package flat. Separate planning policy, execution mechanism, and VM materialization behind narrow private types. One opcode/effect specification remains authoritative.
- Start every behavior slice red through `Compile`/`Run`, then add private three-way differentials. Preserve all user work and raw captures outside the repository.

## Execution path

### P1. Freeze the dynamic architecture contract [D1]

- **Touch:** test-only VM observer/generator support, performance audit, and ADR 0009.
- **Change:** Reproduce two all-37 baselines; count normalized loop/call clusters, tags, shapes, exits, and cost without source identity. Seal an independent seed `0x454d42455204` holdout without running or inspecting it before P4.
- **Proof:** Deterministic observer output, zero production hook, hash-bound captures, Amdahl and candidate table reproduced.

### P2. Extract the deep IR and exact snapshot seam [D2, D5]

- **Touch:** `runtime_backend_*`, VM opcode metadata, new private region planner/snapshot files, prepared generator tests.
- **Change:** Make current backend IR state-neutral; define region boundaries, commit frontiers, live/inlined-frame snapshots, and pre-effect exits. Prepared lowering consumes the same facts unchanged.
- **Proof:** Malformed-IR rejection, exhaustive per-op snapshot round trips, prepared freshness/parity, race/checkptr, and no semantic duplicate table.
- **Depends on:** P1

### P3. Prove typed loops and compact call clusters [D3, D5]

- **Touch:** generated region executor, owner cache, VM entry/exit seam, focused public/differential tests.
- **Change:** Add fixed-cap numeric SSA banks, loop backedge entry, direct-call continuations, recursion materialization, exact guards/charges, and generic fallback.
- **Proof:** Arithmetic and recursion meet D3 across alias/tag/NaN, depth/error, controller, cancellation, cold, polymorphic, and forced-exit cases; executor stays under 4,000 non-test LOC before P4.
- **Depends on:** P2

### P4. Prove object specialization and decide retention [D3, D5, D6]

- **Touch:** region shape/access/call lowering, scalar scratch, `vmFunctionInstance` lifecycle, table/closure/vararg tests.
- **Change:** Scalar-replace nonescaping objects; guard escaping table layout/metatable/value versions; inline stable field/array/callee paths; keep effects and megamorphism in VM.
- **Proof:** Arrays and event dispatch meet D3; holdout agrees; version/cache churn plateaus; generated source grows at most 20%. If any gate fails, C1 runs before broadening.
- **Depends on:** P3

### P5. Complete bounded coverage without semantic spread [D4-D6]

- **Touch:** only profile-proven region families and canonical VM materialization.
- **Change:** Add remaining all-37 shapes in measured order, independently retaining each family only when geomean improves at least 2%, no row regresses over 5%, and complexity/caps hold.
- **Proof:** Boundary-exit matrix, fuzzed three-way differential, all semantic/effect suites, allocation/lifetime ceilings, race/checkptr, and residual profiles after every retained family.
- **Depends on:** P4

### P6. Make adaptive VM the dynamic default and certify [D4, D7]

- **Touch:** private runtime selection, docs/audit/ADR; no public API.
- **Change:** Enable bounded regions for non-prepared execution; retain VM fallback and prepared Machine; remove losing experiments and production observers.
- **Proof:** Two strict final captures, cold/compile gates, prepared pair, `go generate ./...`, `scripts/check-purego`, `scripts/check-fast`, `scripts/check`, and `git diff --check`.
- **Depends on:** P5

## Conditional work

### C1. Delete or reopen after rank reversal

- **Trigger:** P3/P4 misses any D3 gate, needs identity tuning, exceeds caps, or loses to the faster current VM/Machine path for its family.
- **Then:** Delete production regions/caches, retain only zero-cost audit fixtures, record the failed premise in ADR 0009, and reopen the bounded candidate cutoff. Do not relax gates or rescue it with JIT, assembly, or another simultaneous state rewrite.
- **Proof:** Generic VM/prepared checks and baseline captures pass; production search finds no region executor/cache remnants; ADR 0009 binds the failed evidence.
- **Else:** no work

## Deferred / external

- Build-time prepared Go remains the preferred path for known scripts. Native code, runtime compilation, foreign backends, and public tier controls remain excluded.

## Risks and assumptions

- Typed macro-op dispatch may still be too expensive; D3 kills the architecture early.
- Deoptimizing inlined calls can corrupt visible frames; pre-mutation snapshots and boundary-forced tests are mandatory.
- Corpus success can overfit; semantic keys, caps, cross-case coverage, and the frozen holdout carry the generality boundary.
