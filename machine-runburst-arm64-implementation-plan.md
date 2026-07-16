<!-- simple-loop-plan -->

# Darwin/arm64 no-CGO Machine RunBurst implementation plan

## Outcome

- **Source:** [current migration run](runtime-speed-2x-no-cgo-production-migration-implementation-plan-4.md), focused from the static `RunBurst` option in the [production migration source](runtime-speed-2x-no-cgo-production-migration-implementation-plan.md).
- **Eventual goal:** Deliver one complete no-CGO Machine runtime whose qualified all-37 dynamic path is within 2x of pinned Luau.
- **Current run:** Build and judge one real static Darwin/arm64 assembly accelerator for verified numeric/control blocks. Retain and enable it only if it closes the frozen `top10/arithmetic_for` dispatch viability gate without semantic, allocation, portability, or broad warmed-runtime regressions; stop before wider acceleration, hardening, or cutover.

### Done when

- D1. One canonical bounded-burst contract lets the generic Machine and assembly backend start from the same owner-local state and return an exact next PC/reason. Unsupported operations, guards, policies, and quantum exits resume in the generic Machine, never the old VM, without partial operation mutation or charge.
- D2. The Darwin/arm64 leaf retires multiple verified numeric/control Machine operations per entry, covers every dynamic hot-loop operation needed by the frozen dispatch case, contains no Go/foreign calls or retained pointers, and matches a pure-Go reference oracle for results, state, errors, limits, GC, and independent owners.
- D3. Two clean candidate dispatch captures each satisfy median Ember/Luau <=1.85 and nearest-rank p90 <=2.00; every Cartesian candidate/current comparison is <=1.05; result hashes match; warmed allocations do not increase. A fixed broad before/after Machine sample has at least 80% of rows neutral or improved and no row regressed over 5%.
- D4. Pure-Go, cross-platform, disassembly, relocation, symbol, memory-permission, code-size, race/checkptr, generation, focused, and repository checks pass. The backend text/rodata contribution is <=256 KiB and non-Darwin/arm64 targets retain the portable path.

### Constraints

- Keep `CGO_ENABLED=0`. Use supported Go declarations and Plan 9 assembly only: no C, foreign symbols, helper process, runtime code generation, executable memory, private ABI, `go:linkname`, or new dependency.
- Select bursts only from verified operation/block semantics and runtime guards, never source, benchmark, corpus, constant, fixture, or Proto identity.
- Pass a pointer-free scalar control record plus separate typed arena bases and lengths. Never pass `Machine`, a slice header, or a stack array; mutate only pointer-free storage; keep owners and backing stores alive across the call and refresh bases after Go growth/effects.
- Keep cancellation, limits, host effects, calls, tables, strings, allocation, and unsupported operations in Go for this run. Bound every assembly entry by a primitive quantum.
- Preserve unrelated and untracked user work. Acquire performance evidence from clean detached worktrees bound to exact commits.

### Non-goals

Assembly coverage for all semantic families, AOT, CGO/foreign backends, production cutover, old-VM deletion, Hearth integration, and publication are outside this run.

## Repository evidence

- The corrected clean dispatch capture at `7a9a7348` measured candidate/Luau median `6.057109` and p90 `6.704961`; candidate/current median was `1.174128`. The frozen target therefore remains decisively unmet.
- The arithmetic profile attributes about 78.93% cumulatively to the generated Machine loop, with repeated costs in numeric helpers, register moves, numeric-for control, per-op charging/policy tests, and switch dispatch. With the measured ratio, Amdahl's law requires roughly an 8.3x kernel speedup even if non-kernel cost is fixed, so a literal one-operation-at-a-time assembly port is not a viable design.
- `codeImage` already records `machineBlock` boundaries, and `machineOperation` is scalar/pointer-free, but execution ignores the blocks. Existing compile-time numeric facts can seed a compact burst lowering; loop-carried values still require runtime tag/shape guards.
- `executionWindow` has a nil controller for the warmed background path. Controlled, cancelable, effectful, or unsupported paths can remain on the generic Machine without workload routing.
- `scripts/check-purego`, arm64 CI coverage, parity capture tooling, and the frozen `BenchmarkRuntimeSpeed2x` corpus provide the portability, binary, correctness, allocation, and broad-retention evidence needed to reject an unsafe or ineffective backend.

## Execution path

### P1. Define the bounded burst contract and reference oracle [D1, D2]

- **Touch:** `runtime_image.go`, `runtime_machine.go`, new `runtime_machine_burst.go`, and focused burst/image tests.
- **Change:** Lower eligible `machineBlock` regions into owner-neutral pointer-free descriptors that group straight-line numeric dataflow and numeric-for control rather than mirror the Go switch one operation at a time. Define entry guards, maximum primitive work, charge accounting, exact exit PC/reason, and a pure-Go reference executor. An unsupported descriptor or failed precondition must exit before that operation changes state.
- **Proof:** Deterministic image/layout tests; pointer-free layout inspection; table-driven transitions for arithmetic, constants, moves, numeric-for latch/backedge, return, guard failure, unsupported exit, and quantum exhaustion; reference-versus-generic Machine differentials including NaN, infinities, signed zero, integer/float coercion, boxed values, exact error PC, and charge state.

### P2. Implement and integrate the real assembly leaf [D1, D2]

- **Touch:** new `runtime_machine_burst_arm64.go`, `runtime_machine_burst_arm64.s`, and `runtime_machine_burst_portable.go`; `runtime_machine_template.go.tmpl`, generated Machine output, `cmd/ember-vmgen`, and arm64/portable tests.
- **Change:** Add a supported Go prototype and mutually exclusive build tags. The no-call Darwin/arm64 leaf consumes the compact descriptors and typed arena bases, executes multiple operations per entry, and returns through the canonical exit record. Enter only at eligible unrestricted block boundaries; use `runtime.KeepAlive` after the call. Policy/controller paths, failed guards, effects, and unsupported descriptors continue at the exact PC in generic Machine code. Tests may force reference or assembly per owner, never through a production process-global selector.
- **Proof:** Assembly/reference/generic differentials over generated block sequences and numeric edges; frozen dispatch operation-coverage assertion; forced GC before/after calls; repeated growth/base refresh; two independent and concurrent owners; bounded-exit and fallback tests; controller/cancellation generic-path differentials; `go run ./cmd/ember-vmgen -check`; native arm64 checkptr and non-arm64 portable builds.
- **Depends on:** P1

### P3. Prove binary safety and earn retention [D3, D4]

- **Touch:** new `scripts/check-machine-backend`, new `cmd/ember-machocheck/main.go`, `scripts/check-purego`, pure-Go boundary tests, and only the minimum capture/check documentation needed for reproducibility.
- **Change:** Add cross and native inspection for forbidden calls/branches-with-link, relocations, foreign/private symbols, executable writable sections, build-tag leakage, and the 256 KiB ceiling. From clean detached worktrees, capture the exact current baseline and two VM/two assembly dispatch runs, compute the same 3x3 Cartesian statistics, and compare fixed five-sample warmed Machine rows across all 37 cases. Enable assembly by default on Darwin/arm64 only after all D3/D4 gates pass.
- **Proof:** Cross-built and native checker fixtures; focused race/checkptr/differential tests; capture manifests, clean-state attestations, hashes, result parity, ratio statistics, allocation evidence, and broad before/after TSV; `scripts/check-purego`; `scripts/check-lane root`; `scripts/check-fast`; `scripts/check`.
- **Depends on:** P2

## Conditional work

### C1. Reject an unsafe or ineffective backend

- **Trigger:** Any P2 semantic/safety proof or P3 retention threshold fails after one profile-directed bounded correction.
- **Then:** Remove the assembly integration and run-created backend-specific artifacts, restore the portable generic Machine as the only path, rerun generation, focused tests, `scripts/check-purego`, and `scripts/check`. Record the measurements and report this run incomplete; do not weaken a threshold or proceed to wider migration phases.
- **Else:** no work

## Deferred / external

- If this viability run passes, a new plan may extend semantic coverage and execute the source plan's all-37 hardening, cutover, deletion, and final certification phases. Prepared-bundle AOT and any foreign/CGO backend remain separate product decisions.

## Final verification

- Completion requires P3's exact clean evidence, `go test ./...` through `scripts/check`, and `git diff --check`; passing unit tests without the frozen speed and binary-safety gates is not completion.

## Risks and assumptions

- The required kernel gain is extreme. Block fusion may still miss it; that is a valid rejection result, not permission to specialize for the benchmark.
- Assembly is invisible to the race detector and can corrupt pointer-adjacent state. Narrow pointer-free mutation, differential canaries, GC stress, checkptr, and binary inspection are mandatory together.
- Entry wrappers, guards, quantum exits, or code growth can erase local wins or regress unrelated rows, so broad retention is measured before default enablement.

## Source disposition

- P1-P3 extract only the evidence-triggered static `RunBurst` experiment from Source Slice 7.2. Source hardening/cutover/deletion phases and broader accelerators stay deferred unless this run passes every gate.
