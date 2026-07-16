<!-- simple-loop-plan -->

# Natural-loop Darwin/arm64 RunBurst implementation plan

## Outcome

- **Source:** [first RunBurst viability plan](machine-runburst-arm64-implementation-plan.md), refining Source Slice 7.2 in the [production migration plan](runtime-speed-2x-no-cgo-production-migration-implementation-plan.md).
- **Eventual goal:** Deliver one complete no-CGO Machine runtime whose qualified all-37 dynamic path is within 2x of pinned Luau.
- **Current run:** Implement and judge one static Darwin/arm64 assembly leaf that executes a verified straight-line numeric-for natural loop in bounded bursts. Retain it only if the frozen dispatch case meets its speed target and the unchanged broad Machine surface remains safe and neutral.

### Done when

- D1. Image preparation lowers only single-entry numeric-for regions with one check, straight-line supported body, one latch backedge, and one exit into immutable pointer-free descriptors; source, benchmark, constants, fixture, corpus, and Proto identity never participate in eligibility.
- D2. A supported Go-prototyped, no-call assembly leaf matches generic Machine bit-for-bit for `MOVE`, `MUL_K`, `IDIV_K`, `SUB`, `MOD_K`, `ADD`, numeric-for check/latch, boxed numbers, exceptional floats, and bounded exits.
- D3. Assembly runs only when `controller == nil`, `skipCharge == 0`, and the PC is an eligible loop header. Complete and quantum exits resume at the exact PC; failed guards resume generic Machine before the failing operation mutates state. No path enters the old VM, and portable targets retain generic execution.
- D4. Cross/native inspection proves the leaf has no `BL`/`BLR` or other calls, forbidden relocations/symbols, private ABI, writable-executable memory, build-tag leakage, or retained pointers; backend text/rodata is <=256 KiB.
- D5. Two candidate dispatch captures each have median Ember/Luau <=1.85 and nearest-rank p90 <=2.00; all four candidate/frozen-current capture pairings have 3x3 median <=1.05; hashes match and warmed allocations do not rise. Across all 37 warmed Machine rows, at least 80% improve or remain inside baseline noise and none regress over 5%.

### Constraints

- Keep `CGO_ENABLED=0`, Go 1.26 declarations, and Plan 9 assembly only. No C, foreign backend, runtime code generation, executable memory, helper process, new dependency, `ABIInternal`, or `go:linkname`.
- Pass no `Machine`, interface, slice header, pointer-rich struct, or stack array. The leaf receives a pointer-free scalar control record and separate typed descriptor/register/number bases with lengths; it retains nothing and mutates only registers, `numberBits`, and control output.
- Keep limits, cancellation, calls, effects, tables, strings, allocation, returns, malformed descriptors, and unsupported operations in Go. One leaf entry retires at most 4096 primitive Machine operations.
- Preserve unrelated work. Performance captures come from clean detached worktrees bound to exact commits and the pinned acceptance environment.

### Non-goals

Inner branches, nested loops, `POW`, effects, calls, table/string operations, other architectures, AOT, broad Machine cutover, and old-VM deletion are outside this run.

## Repository evidence

- The frozen source hash `bc42d7b066b110385e6ca8e0035511dfc84012f4f79f6c336dd01bd9756711f1` compiles to preheader PCs 0-3, `NUMERIC_FOR_CHECK` at PC 4, body/latch `MOVE,MUL_K,MOVE,IDIV_K,SUB,MOD_K,ADD,NUMERIC_FOR_LOOP` at PCs 5-12, and `RETURN_ONE` at PC 13. Existing blocks are `[0,4)`, `[4,5)`, `[5,13)`, and `[13,14)` but are not consumed by execution.
- The clean dispatch result at `7a9a7348` was median `6.057109` and p90 `6.704961` versus Luau. The CPU profile places 78.93% cumulatively in the generated loop and numeric helpers, so assembly entry must span the whole natural loop rather than translate one basic block or operation at a time.
- `machineOperation`, slots, and `numberBits` are pointer-free. `setNumber` stores ordinary IEEE bits directly but boxes bit patterns matching `0x7ff8...` into generation-1, cell-relative `numberBits`; `MOVE` re-encodes numbers instead of copying boxed handles.
- The warmed callback path reaches persistent `scalarMachine` with `context.Background()` and zero limits, so `newExecutionPolicy` supplies the nil controller required by the focused leaf. Existing controller/coroutine/effect paths remain an equivalent generic class.
- `purego_boundary_test.go` already permits Go assembly while rejecting foreign targets, and `scripts/check-purego` builds/tests with CGO disabled and inspects linked binaries; it needs a stricter leaf-specific no-call observer, not a replacement.

## Execution path

### P1. Lower strict numeric-for regions and freeze the transition contract [D1, D2]

- **Touch:** `runtime_image.go`, new `runtime_machine_burst.go`, and new `runtime_machine_burst_test.go`.
- **Change:** Detect `numericForLoopDesc` regions whose check fallthrough, contiguous body, latch, backedge, and exit match the verified CFG. Accept only the exact supported operation set, numeric constants, bounded register accesses, and body reads that are guarded live-ins or earlier numeric definitions. Emit compact relative-register operations plus check/body/latch PCs, exit PC, per-iteration cost, and guard set. Define statuses `complete`, `quantum`, and `fallback`, with `nextPC`, failing PC, and retired count. Add a pure-Go reference executor over cloned scalar arrays.
- **Proof:** The frozen source lowers to one region covering PCs 4-12 and 1801 dynamic primitive operations for 200 iterations; nearby loops with inner branches, effects, unsupported ops, alternate entry/backedge, invalid constants, or unproved reads reject. Layout tests prove descriptor/control pointer freedom and fixed offsets. Table-driven reference-versus-generic transitions prove empty, one-iteration, positive/negative/zero-step, quantum-boundary, alias, nonzero-base, and exact fallback-PC behavior.

### P2. Implement the arm64 leaf and integrate one header hook [D2, D3]

- **Touch:** new `runtime_machine_burst_arm64.go`, `runtime_machine_burst_arm64.s`, `runtime_machine_burst_portable.go`, and arm64 tests; `runtime_machine_template.go.tmpl`, generated output, and `cmd/ember-vmgen` only as required by the canonical hook.
- **Change:** Declare one Go ABI0 prototype, use `go_asm.h` for constants/field offsets, and justify `//go:noescape` only for the verified no-call/non-retaining leaf. Validate lengths/bases in Go, then keep image and arenas alive with `runtime.KeepAlive`. Implement numeric decode and cell-relative generation-1 boxing; reject other tags/generations before the current operation. Use separate ARM64 `FDIVD`, `FRINTMD`, `FMULD`, and `FSUBD` steps for floor division/modulo so fused rounding cannot change Go semantics. At `opNumericForCheck`, enter only the unrestricted eligible region; retry quantum exits, but suppress repeated guard attempts until PC leaves that region. Portable selection returns unsupported without assembly linkage.
- **Proof:** Three-way assembly/reference/generic comparisons cover all supported op forms, destination/source aliasing, +/-0, subnormals, extremes, infinities, tagged and untagged NaNs, divide/modulo by +/-0, valid/malformed boxed handles, and exact register/`numberBits` bytes. Add forced-GC, repeated rebind/growth, independent/concurrent owners, nested nonzero-base calls, guard no-mutation canaries, quantum progress, controller/cancellation fallback, frozen coverage, generated freshness, native checkptr, and portable cross-build tests.
- **Depends on:** P1

### P3. Make binary and focused-ratio proof fail closed [D4, D5]

- **Touch:** new `cmd/ember-machocheck/main.go`, new `scripts/check-machine-backend`, `scripts/check-purego`, pure-Go fixtures/tests, and `scripts/runtime-ratio-gate` plus its self-test.
- **Change:** Add `runtime-ratio-gate --phase dispatch` without changing the all-37 default; bind exactly `top10/arithmetic_for`, validate one-case row counts, and report all four candidate/current pairings. Add cross/native Mach-O inspection for the exact leaf symbol, calls, relocations, ABI/symbol provenance, W+X segments, selected/absent build-tag symbols, and the 256 KiB ceiling. Native mode additionally corroborates Mach-O state with `otool`.
- **Proof:** Gate fixtures reject missing/extra/duplicate/contaminated rows and each threshold violation while the existing full self-test remains unchanged. Binary fixtures reject every forbidden class. Cross-build Darwin/arm64 and Linux/arm64 test binaries; run cross inspection on both, native inspection on Darwin/arm64, `go vet`, `scripts/check-purego`, `scripts/check-lane root`, and `scripts/check-fast`.
- **Depends on:** P2

### P4. Earn the retain-or-delete decision [D5]

- **Touch:** Final backend selection/integration only; evidence stays in caller-owned temporary directories.
- **Change:** From clean worktrees acquire two frozen-current and two candidate `dispatch` captures and gate them with P3. Separately acquire two pure-Go and two assembly all-37 `BenchmarkRuntimeSpeed2x` runs with `GOMAXPROCS=1`, `CGO_ENABLED=0`, `-benchmem`, `-benchtime=300ms`, and `-count=5`; parse raw files through `bench-summary`. Per row, use ten-sample baseline/candidate medians and neutral ceiling `max(1, median(A/B), median(B/A))` over the two 5x5 pure-Go Cartesian sets; require candidate/baseline within it, <=1.05, and unchanged median B/op and allocs/op. Retain default Darwin/arm64 selection only if every D5 condition passes.
- **Proof:** Immutable capture metadata/hashes, phase-gate output, raw benchmark files and TSV, per-row baseline-noise/ratio/allocation table, correct-result checks, and a final profile showing the leaf executes the intended region rather than a Go fallback.
- **Depends on:** P3

## Conditional work

### C1. Make one bounded performance correction or reject assembly

- **Trigger:** P4 is semantically correct and safe but misses any D5 timing/allocation condition.
- **Then:** Use the failed row/profile to make at most one local change to leaf dispatch, descriptor packing, quantum, or wrapper entry, rerun all P2-P4 proof, and retain only on a complete pass. Otherwise remove every run-created backend/integration/checker change, restore generic Machine generation, run `scripts/check-purego` and `scripts/check`, preserve external evidence, and report the run incomplete.
- **Else:** no work

## Deferred / external

- Broader assembly families and the migration plan's hardening, all-37 target, cutover, deletion, and certification require a new plan after this dispatch viability result. AOT and foreign/CGO backends remain separate product decisions.

## Final verification

- After P4 or C1 establishes the final tree, run `scripts/check`, `git diff --check`, and generated-file freshness because the retain/delete decision occurs after P3's structural sweep.

## Risks and assumptions

- A data-driven loop leaf may still be too slow for the required gain; failure deletes it rather than adding benchmark-shaped superinstructions.
- ARM64 rounding, NaN payloads, boxed-cell indices, and nonzero frame bases are corruption risks invisible to `-race`; byte-level differentials and no-call inspection are required together.
- The phase-specific ratio observer must not weaken the existing all-37 gate; its default behavior and self-test stay byte-for-byte compatible.

## Source disposition

- P1-P4 replace the first plan's broad P1-P3 wording with the exact natural-loop implementation, safety observer, and retention boundary for Source Slice 7.2. C1 preserves its mandatory delete-on-failure rule; all later source phases remain deferred.
