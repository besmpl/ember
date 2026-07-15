<!-- simple-loop-plan -->

# Complete no-CGO production Machine migration implementation plan

## Outcome

- **Source:** [runtime-speed-2x-no-cgo-production-migration-implementation-plan.md](runtime-speed-2x-no-cgo-production-migration-implementation-plan.md), continuing the completed [scalar/control revision](runtime-speed-2x-no-cgo-production-migration-implementation-plan-2.md).
- **Eventual goal:** Replace Ember's pointer-rich VM with one production `CodeImage`/`Machine` runtime whose dynamic path remains pure Go and is within 2x of pinned Luau on every qualified workload.
- **Current run:** Complete that goal: migrate every supported semantic family and public entrypoint, make the Machine canonical, delete the old VM, and certify the exact post-deletion runtime.

### Done when

- D1. `Run`, `RunWithGlobals`, `Program.NewRuntime`, `Runtime.RunHook`, modules, callbacks, coroutines, and scripted re-entry all execute through one Machine facade with no feature fallback or mixed mutable representation.
- D2. Calls/returns/varargs, closures/upvalues, strings/globals/modules, tables/metatables/iteration, host effects, callbacks, and coroutines use compact owner-local state; hot records are pointer-free, boundary conversion is batched, and warmed stable operations allocate nothing per instruction, call, loop, lookup, insertion, or resume.
- D3. Independent old/Machine owners match values, errors/frames, state/effects, deterministic iteration, exact limits, cancellation, owner isolation, collection, close, retained results, and stale/cross-owner rejection across all supported features.
- D4. A reviewed expected-result corpus replaces the old VM as semantic oracle; old `vmThread`/`vmFrame`, old dispatch, `runtimeHeap` execution state, eligibility routing, and migration adapters are deleted and cannot be regenerated or reached.
- D5. On the clean post-deletion commit, two independent all-37 dynamic captures each pass every row at median Ember/Luau ratio <=1.85 and nearest-rank p90 <=2.00; both candidate/current comparisons are <=1.05; frozen warmed allocation ceilings and all no-CGO/platform gates pass.
- D6. ADR 0007 and runtime documentation record ownership, retained or rejected accelerators, evidence hashes, reproduction commands, compatibility changes, and platform limits.

### Constraints

- Preserve Luau-visible behavior. Go APIs and private bytecode may change only through test-backed ownership decisions and migration documentation.
- Keep one owner-local representation per invocation. No opcode, script call, object, or effect may fall back to or mirror old state after Machine selection.
- Keep production `CGO_ENABLED=0`: no foreign backend, helper process, runtime code generation, executable memory, private Go ABI, or new dependency without approval.
- Specialize only from verified semantic/runtime state, never source, fixture, corpus, benchmark, constant, or Proto identity. Keep the root package flat and preserve unrelated work.

### Non-goals

New language features, Hearth integration, publication, and prepared-bundle AOT are not required for the dynamic-runtime goal.

## Repository evidence

- Commit `5bda6072` completed the lazy scalar `codeImage`, generated policy, pointer-free ephemeral `scalarMachine`, public `Run` routing, differential/lifecycle tests, and <=1.05 retention gate. Treat it as evidence.
- Persistent execution still uses `runtimeOwner`/`runtimeHeap`, `vmThread`/`vmFrame`, `Value` registers, old generated dispatch, and independent seams in `program.go`, `module_runtime.go`, `callback.go`, and `base_coroutine.go`; only scalar leaf `Run` reaches `executeCodeImage`.
- ADR 0007 accepts one immutable image, one owner-bound Machine, batched boundaries, explicit effects, full cutover, and old-VM deletion. Existing parity, allocation, performance, pure-Go, race, and checkptr tooling supplies the gates.
- The target Apple M1 host and pinned `/opt/homebrew/bin/luau` are available. Performance/allocation baselines are hash-bound in ADR 0007; the two final speed baseline captures must be freshly restored or reacquired from the recorded baseline commit before certification.
- `EMBER_IMPLEMENTATION_PLAN.md`, `performance-optimization-plan.md`, and the focused revision are untracked user files and must remain untouched.

## Execution path

### P1. Establish the owner-bound Machine and public lifetime [D1, D2, D3]
- **Touch:** `runtime_machine*.go`, `runtime_owner.go`, `runtime_heap.go`, `program.go`, `callback.go`, `value.go`, and focused layout/owner/public tests.
- **Change:** Evolve the ephemeral executor into one Runtime-owned Machine with scalar stacks/continuations and arenas, typed root/payload sidecar, batch import/export, explicit result/callback/coroutine leases, quiescent collection/reuse, and stale/cross-owner validation. Freeze convenience-wrapper, close, busy, detach, and re-entry behavior before routing objects.
- **Proof:** Pointer-closure layout tests; forced GC across bind/grow/effect/export/close; pin versus unrelated-reclamation tests; race/checkptr lifetime tests; cold and warmed boundary allocation lanes.

### P2. Move calls and the non-table object world [D1, D2, D3]
- **Touch:** `runtime_image.go`, generated Machine dispatch/spec, new stack/object/string/global/module components, `program.go`, `module_runtime.go`, and `runtime_call.go`.
- **Change:** Add in-Machine direct/tail calls, all argument/result/vararg modes, recursion, compact closures/open and closed upvalues, owner string interning, dense globals/descriptors, and dense module load/export state. Reserve growth at stopped effects; never recurse through Go or box stable accesses into `Value`.
- **Proof:** Generated arity/result/capture corpus; call error/limit differentials; closure alias/close/GC; string/global mutation; module cycles/errors/repeated hooks; five-sample benchmarks and flat warmed allocations.
- **Depends on:** P1

### P3. Replace tables and dynamic object semantics [D1, D2, D3]
- **Touch:** New Machine table/iterator/shape/cache components, generated dispatch/effects, `table_ops.go`, `table_shape.go`, and table/metatable/property tests.
- **Change:** Implement scalar table arenas with array/hash storage, bounded probes, deterministic order, growth/rehash effects, shape transitions and bounded descriptor caches, scripted metatable/method calls, raw operations, and generic iteration. Preserve the documented Ember order contract and Luau-visible key/metatable behavior.
- **Proof:** Old/Machine differential fuzzing for long mutation sequences; NaN/signed-zero/object keys, deletion/reinsertion, iteration mutation, chain invalidation and errors; forced GC/race/checkptr; five-sample read/write/growth/iteration benchmarks.
- **Depends on:** P2

### P4. Complete effects, callbacks, coroutines, and policy [D1, D2, D3]
- **Touch:** Machine effect/root/GC/callback/coroutine components, `execution_control.go`, `runtime_error.go`, public/runtime standard-library seams, and lifecycle tests.
- **Change:** Centralize pointer-bearing and unbounded work in one replay-safe stopped-Go effect protocol; port host/native calls, callbacks, yields/resumes, exact instruction policies, cancellation, errors, result leases, and complete root tracing. Require every verified opcode and supported runtime behavior to be Machine-capable.
- **Proof:** Exhaustive effect-return/resume vectors; nested host/script re-entry; callback/coroutine close races; every limit around calls/effects/yields; cancellation latency; adversarial collection; full public differential and parity suite.
- **Depends on:** P3

### P5. Make the complete pure-Go Machine canonical and tune it [D1, D4, D5]
- **Touch:** `vm.go`, `program.go`, module/callback/coroutine entry seams, Machine generator/kernel, routing tests, and performance diagnostics.
- **Change:** Route every public entry through one facade, remove eligibility fallback, and isolate the old VM as a test oracle. Profile the public path; retain only general pure-Go changes keeping every exercised row within its 1.05 current-baseline and allocation ceilings.
- **Proof:** Static entry/call-path inventory; no unsupported reasons; full/race/checkptr/pure-Go checks; two candidate/current comparisons plus CPU/allocation/effect profiles.
- **Depends on:** P4

### P6. Freeze the semantic oracle and delete the old runtime [D3, D4]
- **Touch:** Generated differential corpus, `testdata/runtime-machine/`, ADR 0007, old VM/dispatch/heap files, generator branches, migration routes, and superseded tests.
- **Change:** Generate and review a hash-bound expected JSONL corpus from separate old/Machine owners on a clean commit, make every retained backend pass it, then delete old execution, pointer-rich owner state, feature routing, duplicate metadata, and obsolete generation. Port every useful owner/callback/coroutine/GC obligation before its implementation disappears.
- **Proof:** Default expected-corpus comparison; `go generate ./...` and generator freshness; deletion `rg` inventory with every surviving hit classified; focused checks after each deletion group, then race and checkptr.
- **Depends on:** P5

### P7. Certify and document the delivered runtime [D5, D6]
- **Touch:** Runtime evidence scripts/artifacts, `README.md`, runtime/public/compatibility/check docs, ADR 0007, and final architecture/performance summary.
- **Change:** On the exact post-deletion commit, acquire two clean full captures, two independent dynamic `speed2x` captures, allocation evidence, and paired performance audits. Reacquire or restore hash-verified baseline captures from the recorded baseline commit; record all hashes and only claims supported by both candidate captures.
- **Proof:** `scripts/check`, vet, race, checkptr, pure-Go/platform builds, expected corpus, both `full` captures, `runtime-ratio-gate` at 1.85/2.00/1.05, `runtime-allocation-gate`, and both `performance-audit-compare` roles.
- **Depends on:** P6

## Conditional work

### C1. Add only profile-proven general acceleration
- **Trigger:** P5's Machine profiles show the D5 target still fails and identify sufficient repeated decode/tag/dispatch work to close the measured gaps.
- **Then:** First test generated guarded quickening/block operations with exact accounting and deoptimization. If the target still fails and Darwin/arm64 kernel work remains sufficient, test one bounded no-call static `RunBurst` assembly backend with portable-Go fallback, cross/native binary inspection, <=256 KiB text/rodata, GC/latency canaries, and complete differential coverage. Retain a mechanism only under the source plan's broad-row, <=5% per-row regression, allocation, safety, and code-size gates; otherwise delete it. If retained mechanisms cannot make D5 pass, stop before P6 and report the run incomplete.
- **Else:** no work

## Deferred / external

- Prepared-bundle Go AOT requires a separate product decision and cannot satisfy the dynamic D5 claim. Hearth build integration and release/publication follow only after this repository's dynamic contract passes.

## Final verification

- After all active and triggered work, run the P7 matrix on a clean post-deletion commit and verify `git diff --check`; no pre-deletion, contaminated, best-attempt, or merged capture can prove completion.

## Risks and assumptions

- Calls/tables/effects are too coupled for per-op fallback; a failed vertical remains incomplete rather than narrowing the supported set.
- Table order, upvalue aliasing, replay-safe effects, suspended roots, and boundary leases are the highest semantic/retention risks and require independent-owner plus forced-GC proof.
- Final speed is empirical. Missing target evidence triggers only bounded general accelerators; it never weakens D5 or turns prepared AOT into a dynamic-runtime pass.

## Source disposition

- Source Phase 0 and image/scalar portions of Phases 1-2 are repository evidence through `5bda6072`; P1-P5 activate the remaining ownership, call/object, table, effect, lifecycle, cutover, and tuning work from Source Phases 1-6.
- C1 contains the evidence-dependent Source Phase 7 path. P6-P7 activate Source Phase 9 oracle deletion, certification, and documentation. Source Phase 8 remains deferred because its prepared mode neither satisfies the dynamic goal nor has the required product authorization.
