<!-- simple-loop-plan -->

# Performance-first complete Machine migration implementation plan

## Outcome

- **Source:** [complete migration revision](runtime-speed-2x-no-cgo-production-migration-implementation-plan-3.md), grounded in the [production migration source](runtime-speed-2x-no-cgo-production-migration-implementation-plan.md).
- **Eventual goal:** Replace Ember's pointer-rich VM with one high-quality no-CGO `CodeImage`/`Machine` runtime whose dynamic path is within 2x of pinned Luau on every qualified workload.
- **Current run:** Build a complete production-shaped Machine candidate, prove it is broadly faster and meets the final all-37 target, and only after that proof spend the remaining work on exhaustive hardening, canonical cutover, old-runtime deletion, documentation, and post-polish certification.

### Done when

- D1. A Machine candidate executes all 37 qualified workloads through the real public invocation shape, with every supported semantic family owner-local and no per-op, per-call, object, or effect fallback to old mutable state.
- D2. Before quality-polish packages begin, two clean candidate captures each have correct results, every row at median Ember/Luau <=1.85 and nearest-rank p90 <=2.00, every candidate/current row <=1.05, the median of the 37 candidate/current row medians <1.00, and warmed allocations within frozen ceilings.
- D3. After D2 passes, exhaustive independent-owner tests prove values, errors/frames, state/effects, deterministic iteration, limits, cancellation, collection, close, leases, callbacks, coroutines, stale/cross-owner rejection, and public lifecycle behavior across all supported features.
- D4. Every production entrypoint uses one Machine facade; a reviewed expected corpus replaces the old oracle; old `vmThread`/`vmFrame`, old dispatch, `runtimeHeap` execution state, eligibility routing, and migration adapters are deleted and cannot be reached or regenerated.
- D5. The exact clean post-polish/deletion commit repeats D2's two-capture speed, current-baseline, and allocation gates and passes pure-Go, platform, race, checkptr, semantic-corpus, and lifecycle checks.
- D6. ADR 0007 and runtime/public/check documentation accurately record the delivered ownership model, evidence hashes, compatibility changes, reproduction commands, retained or rejected accelerators, and platform limits.

### Constraints

- Benchmark only a semantically representative, result-correct, whole-owner candidate. "Performance first" never permits benchmark-specific routing, skipped effects, weakened accounting, unsafe ownership, or timing a partial runtime as the final architecture.
- Preserve Luau-visible behavior. Go APIs and private bytecode may change only through test-backed ownership decisions and migration documentation.
- Keep production `CGO_ENABLED=0`: no foreign backend, helper process, runtime code generation, executable memory, private Go ABI, or new dependency without approval.
- Specialize only from verified semantic/runtime state, never source, fixture, corpus, benchmark, constant, or Proto identity. Keep the root package flat and preserve unrelated work.

### Non-goals

New language features, Hearth integration, publication, and prepared-bundle AOT are outside the dynamic-runtime result.

## Repository evidence

- Commit `5bda6072` supplies the lazy scalar `codeImage`, generated policy, pointer-free ephemeral `scalarMachine`, public scalar `Run` route, differential/lifecycle tests, and a passing slice-retention benchmark. It is evidence, not active work.
- Persistent execution still uses `runtimeOwner`/`runtimeHeap`, `vmThread`/`vmFrame`, `Value` registers, old generated dispatch, and separate program/module/callback/coroutine seams; the full candidate must replace those costs before broad timing is meaningful.
- `runtime_parity_test.go`, `scripts/check-runtime-parity`, `scripts/runtime-ratio-gate`, `scripts/runtime-allocation-gate`, and `scripts/performance-audit` already provide result validation, matched warmed timing, two-capture comparison, allocation, and profile evidence. The target M1 host and pinned Luau are available.
- ADR 0007 authorizes a temporary separate old/Machine oracle mode and whole-Program selection before mutable state exists. This permits truthful candidate measurement without making an insufficiently hardened backend the production default.
- `EMBER_IMPLEMENTATION_PLAN.md`, `performance-optimization-plan.md`, and the focused revision are untracked user files and must remain untouched.

## Execution path

### P1. Build the compact owner, call, and object core [D1]
- **Touch:** `runtime_image.go`, `runtime_machine*.go`, generated Machine dispatch/spec, `runtime_owner.go`, `runtime_heap.go`, `program.go`, `module_runtime.go`, and `runtime_call.go`.
- **Change:** Extend images to complete Program graphs; add owner-bound scalar stacks, continuations, arenas, root sidecar, batch boundaries, direct/tail calls, all arity/result/vararg modes, closures/upvalues, interned strings, dense globals, and module state. Keep growth and pointer work at stopped effects; do not polish obsolete old-runtime code.
- **Proof:** Representative generated call/capture/result vectors, module/global cases, forced-GC bind/grow/export checks, pointer-closure layout checks, and flat warmed call/global/module allocations.

### P2. Complete the benchmark-valid Machine candidate [D1]
- **Touch:** Machine table/shape/cache/effect/callback/coroutine components, `table_ops.go`, `table_shape.go`, `execution_control.go`, `runtime_error.go`, public/runtime seams, and private candidate selection in the acceptance subprocess.
- **Change:** Add scalar tables, properties, metatables, iteration, host/native effects, callbacks, coroutines, exact limit/cancellation policy, errors, roots, and results. Exercise the Machine through the same public facade and owner lifetime as production, selected generically before binding in a separate old-or-candidate process; no benchmark or workload identity may select it.
- **Proof:** One representative differential per semantic/effect/lifecycle family, all-37 result parity, entry inventory, no Machine-to-old-runtime call path, focused race/checkptr at pointer and suspension boundaries, and allocation counters sufficient to trust timing.
- **Depends on:** P1

### P3. Earn the performance decision before polishing [D2]
- **Touch:** Acceptance/capture plumbing only where needed for generic old-versus-candidate selection, benchmark diagnostics, and reproducible evidence outputs.
- **Change:** Freeze the candidate, restore or reacquire hash-bound current baselines, then acquire two clean candidate `full` and `speed2x` captures, allocation evidence, and paired performance audits. Derive the suite median from the 37 per-row candidate/current medians without dropping or weighting rows. Do not start P4-P6 until every D2 threshold passes.
- **Proof:** Both `full` captures; `runtime-ratio-gate` at 1.85/2.00/1.05; independently checked suite medians <1.00; `runtime-allocation-gate`; both `performance-audit-compare` roles; CPU/allocation/effect profiles and artifact hashes.
- **Depends on:** P2

### P4. Harden semantics, ownership, and maintainability [D3]
- **Touch:** Complete Machine tests and generated corpus, public ownership APIs/docs, root/GC/lifecycle code, generator structure, and nearby runtime code exposed by review.
- **Change:** Only after P3 passes, add exhaustive arity/capture/table/metatable/effect/limit matrices, mutation differential fuzzing, adversarial GC/growth, stale-handle and lease tests, callback/coroutine close races, cancellation latency bounds, defensive validation, and simplification of the retained Machine. Freeze explicit convenience-wrapper, detach, busy, close, and re-entry contracts; remove candidate-only instrumentation from timed paths.
- **Proof:** Full generated old/Machine differential corpus; focused fuzz regressions; `scripts/check`; vet, race, checkptr, pure-Go/platform builds; forced-GC and repeated-growth tests; public API/lifecycle tests.
- **Depends on:** P3

### P5. Cut over, freeze the oracle, and delete migration state [D4]
- **Touch:** `vm.go`, all public entry seams, generated expected corpus under `testdata/runtime-machine/`, ADR 0007, old VM/dispatch/heap files, generator branches, routes, and superseded tests.
- **Change:** Make the hardened Machine the only production facade. Generate and review a hash-bound expected JSONL corpus from clean separate old/Machine owners, make the generic backend match it, then delete old execution, pointer-rich owner state, feature routing, duplicate metadata, and obsolete generation. Port every useful old owner/callback/coroutine/GC obligation before deletion.
- **Proof:** Default expected-corpus comparison; static production entry/call-path inventory; `go generate ./...` freshness; deletion `rg` inventory with surviving hits classified; focused checks after each deletion group, then full race/checkptr.
- **Depends on:** P4

### P6. Reconfirm speed and document the delivered runtime [D5, D6]
- **Touch:** Final runtime evidence, `README.md`, runtime/public/compatibility/check docs, ADR 0007, and architecture/performance summary.
- **Change:** On the exact post-polish/deletion commit, repeat both clean full/speed captures, allocation captures, performance audits, and platform checks. Compare against P3 and frozen current baselines; record only claims supported by both final captures and publish reproduction commands and evidence hashes in repository docs.
- **Proof:** P5 semantic corpus; `scripts/check`, vet, race, checkptr, pure-Go/platform matrix; both D5 ratio/suite-median/allocation gates; both performance-audit roles; documentation claim-to-artifact review.
- **Depends on:** P5

## Conditional work

### C1. Repair a failed pre-polish performance gate
- **Trigger:** P3 produces a correct complete candidate but any D2 speed or allocation threshold fails.
- **Then:** Profile only the failing broad paths and retain general pure-Go layout, batching, reservation, accounting, dispatch, or guarded quickening changes that preserve the frozen candidate set and correctness. Test bounded static Darwin/arm64 `RunBurst` assembly only when profiles show enough addressable kernel mass and apply the source plan's safety/code-size/broad-row retention gates. Rerun all P3 proof after each retained mechanism. If D2 still fails, stop the run and do not execute P4-P6.
- **Else:** no work

### C2. Repair a post-polish regression
- **Trigger:** P6 fails a D5 threshold that the P3 candidate passed.
- **Then:** Attribute the regression to hardening, cutover, deletion, or final layout; repair it without removing D3/D4 guarantees or weakening D2/D5, rerun affected P4/P5 proof, then repeat all P6 evidence. If no bounded repair passes, report the run incomplete.
- **Else:** no work

## Deferred / external

- Prepared-bundle Go AOT requires a separate product decision and cannot satisfy the dynamic target. Hearth integration and publication follow only after D5 passes.

## Final verification

- Completion requires the clean P6 matrix and `git diff --check`; pre-polish evidence authorizes hardening but cannot certify the delivered binary.

## Risks and assumptions

- The benchmark-valid candidate still needs representative correctness and safe boundaries; otherwise its timing is not evidence.
- Exhaustive hardening can perturb layout and speed, so P6 repeats rather than reuses P3 evidence.
- Calls, tables, and effects cannot be narrowed after a miss. A failed D2 gate stops polish instead of redefining the supported set or target.

## Source disposition

- P1-P2 compress the remaining Source Phases 1-5 into the minimum complete benchmark candidate; P3 moves the complete dynamic performance decision ahead of Source Phase 6 cutover polish.
- P4-P6 retain Source Phases 6 and 9 quality, deletion, certification, and documentation obligations. C1 contains only evidence-triggered Source Phase 7 acceleration; Source Phase 8 AOT remains deferred.
