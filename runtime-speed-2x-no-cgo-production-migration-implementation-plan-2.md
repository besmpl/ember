<!-- simple-loop-plan -->

# Compact Machine scalar/control vertical implementation plan

## Outcome

- **Source:** [runtime-speed-2x-no-cgo-production-migration-implementation-plan.md](runtime-speed-2x-no-cgo-production-migration-implementation-plan.md)
- **Eventual goal:** Replace Ember's pointer-rich VM with one no-CGO `CodeImage`/`Machine` runtime whose dynamic path is within 2x of pinned Luau on every qualified workload.
- **Current run:** Deliver the first production Machine vertical through public `Compile` then `Run`: a finalized leaf `Proto` in the frozen scalar/control set uses one ephemeral pure-Go Machine from selection through detached results and close; every other `Proto` remains wholly on the old VM.

### Done when

- D1. The eligible set is a compiler-produced leaf `Proto` with no parameters, varargs, children, or upvalues; only nil/boolean/number constants; and only constant loads, moves, numeric arithmetic, scalar equality/ordering, conditional/unconditional jumps, numeric `for`, and fixed-count returns. Every eligible public `Run` uses the Machine without old-VM fallback; every ineligible `Run` still uses only the old VM.
- D2. Old-VM/Machine differential tests match detached results and number behavior (including NaN and signed zero), deterministic error identity/message/script frames, and guest PC accounting across generated operand, branch, loop, return-count, and limit-boundary cases. Cancellation and instruction limits are exercised through the existing private controller seam because public `Run` has no context or limits input.
- D3. One checked opcode metadata source drives image decoding, register effects, control flow, guest charge, error PC, and Machine eligibility. The cached `CodeImage` is immutable and owner-neutral; Machine registers/results are pointer-free 64-bit slots; each run closes on success or failure; and warmed loops allocate nothing per guest instruction or iteration.
- D4. Generated artifacts, focused behavior/routing/lifecycle tests, root and pure-Go checks pass. Five-sample same-tree old/Machine subbenchmarks for `BenchmarkRunArithmetic` and `BenchmarkRunWhileLoop` show Machine median ns/op at most 1.05x old VM, no higher warmed B/op or allocs/op, and allocation counts that stay flat as loop trip count grows.

### Constraints

- Preserve the public API and Luau-visible behavior. Route only `Run`; do not route `RunWithGlobals`, `Program.NewRuntime`, `Runtime.RunHook`, modules, callbacks, coroutines, calls, closures/upvalues, strings, tables, host values, or owner-bound references.
- Use no CGO, foreign backend, runtime-generated code, executable memory, helper-process runtime path, new dependency, or source/fixture/benchmark identity routing.
- Decide the backend before binding the ephemeral Machine and retain it through close. Do not add per-op `Value` conversion or fallback.
- Keep the root package flat and preserve unrelated work.

### Non-goals

Script calls, persistent ownership, object semantics, the canonical cutover, the final 2x claim, and publication are outside this run.

## Repository evidence

- The planning worktree is at `b534bdfd81d1`; `EMBER_IMPLEMENTATION_PLAN.md`, `performance-optimization-plan.md`, and the existing focused revision are untracked user files and must be preserved. Phase 0 of the source plan is already represented by commits `f5e336d7` through `b534bdfd`: invocation reconciliation, the permanent pure-Go check, all-37 correctness/performance schemas, bound A/B performance and allocation baselines, and accepted ADR 0007.
- `bytecode.go` already centralizes opcode names, operands, control flow, register effects, semantic effects, and wordcode metadata. `cmd/ember-vmgen` reads that inventory and generates the old direct loop from `vm_dispatch_template.go.tmpl`; `vm_dispatch_spec.go` is only its generation entrypoint.
- `vm.go` still runs `vmThread`/`vmFrame` over `[]Value`; the generated loop calls `executionWindow.stepInstruction` for each guest instruction. `slot.go` supplies the 64-bit scalar encoding, including a boxed-number tag that must not reuse the pointer-slab `runtimeHeap` in the new hot path.
- Public `Run` always uses `context.Background()` and no controller. Private `executeProto` accepts the controller used by existing limit/cancellation differential tests. The all-37 parity timer uses `Program`/persistent Runtime, so it cannot prove this stateless slice and is not an active gate.

## Execution path

### P1. Prepare one immutable scalar image [D1, D3]
- **Touch:** `bytecode.go`, `wordcode.go`, `cmd/ember-vmgen/main.go`, generation templates/output, and new focused `runtime_image*.go` files/tests.
- **Change:** Extend the existing opcode metadata with explicit guest charge, error/safepoint class, and Machine eligibility, requiring every opcode to be classified. Deterministically lower and once-cache finalized Proto graphs into owner-neutral images with compact operations, blocks, register/PC/source maps, and scalar constant descriptors. Freeze D1 as a fail-closed image predicate; do not bind owner state or alter old-loop semantics.
- **Proof:** `go generate ./...`; `go run ./cmd/ember-vmgen -check`; focused metadata completeness, generated-output, image determinism, malformed-Proto, eligibility rejection, concurrent sharing, and source-map tests.

### P2. Execute scalar/control semantics in one Machine [D2, D3]
- **Touch:** `slot.go`, `execution_control.go`, and new focused `runtime_machine*.go` executor/layout/differential tests.
- **Change:** Bind an eligible image to ephemeral pointer-free slot registers and fixed results; execute blocks with exact guest charging and bounded cancellation polls; preserve error PCs/frames; batch-export detached scalars; close on every terminal path. Handle colliding NaNs in Machine-local scalar number storage without the old pointer slabs, and reserve all timed-path capacity before execution.
- **Proof:** Generated scalar/branch/loop/error/limit/cancel differentials; NaN/signed-zero cases; forced GC between prepare/bind/run/export/close; close-on-error tests; checkptr layout checks; warmed trip-count allocation benchmarks.
- **Depends on:** P1

### P3. Route the public stateless vertical [D1, D4]
- **Touch:** `vm.go`, focused route/entry-inventory tests, and `runtime_benchmark_test.go`.
- **Change:** Select from the cached image before ephemeral binding, route only public `Run` for the exact D1 predicate, and retain the existing VM for every other public surface and semantic feature. Add a structural guard against Machine-to-old-VM calls and paired old/Machine subbenchmarks independent of production routing keys.
- **Proof:** Public source-to-result eligible and fallback tests; concurrent `Run` image-sharing tests; five-sample D4 benchmark comparison; `scripts/check-lane root`; `scripts/check-fast`; `scripts/check-purego`.
- **Depends on:** P2

## Conditional work

### C1. Repair a failed semantic or retention gate
- **Trigger:** P2 or P3 produces a differential/lifecycle mismatch, recurring loop allocation, or a D4 benchmark ratio above 1.05.
- **Then:** Profile only the failing path; repair bounded image, slot, dispatch, accounting, export, or cleanup mechanics and rerun that proof. If no bounded fix passes, remove the production route and report the current run incomplete; do not narrow D1 or weaken D4.
- **Else:** no work

## Deferred / external

- Direct calls, varargs, multiple results, recursion, closures, and upvalues form the next call/object vertical; recursion is not evidence for a noncapturing-call slice.
- Strings/globals/modules, tables/metatables/iteration, host effects/callbacks/coroutines, persistent ownership/GC, canonical cutover, old-VM deletion, and profile-proven accelerators remain later deliveries.
- Two clean quiet-machine all-37 Ember/Luau captures and final certification remain external until persistent Runtime uses the complete Machine.

## Final verification

- After all active and triggered work is stable, run `scripts/check` on the integrated tree.

## Risks and assumptions

- The eligibility predicate must be derived from decoded verified metadata and fail closed; opcode-only routing without constant/Proto-shape checks could admit reference state.
- NaN-tag collisions need a scalar Machine-local representation that neither changes visible number behavior nor allocates per operation.
- Same-tree wall-time evidence is noisy; use five samples and the frozen 1.05 median rule. This is a slice-retention check, not the final Luau ratio claim.

## Source disposition

- Source Phase 0 is satisfied by repository evidence. P1 takes only the image/metadata portion of Slices 1.1-1.2; P2-P3 take Slice 2.2 and its stateless routing proof.
- Stateful Machine ownership and public lifetime decisions from Slices 1.3-1.4 move to the first object/persistent vertical. Slice 2.3 is deferred intact because general recursion requires closure/upvalue state excluded here.
- Source Phases 3-6 remain later semantic/cutover deliveries; Phases 7-8 remain profile-conditional; Phase 9 remains final oracle, deletion, and certification work. Architecture essays, agent assignments, commit ceremony, duplicate command ladders, and file-ownership forecasts are context rather than active work.
