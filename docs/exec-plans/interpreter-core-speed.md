# Interpreter Core Speed Execution Plan

Temporary execution plan. Retire this file when the work lands, is replaced,
or is abandoned.

This plan supersedes the rebuild phases of `general-optimization.md`. The
reset that plan ordered has landed and held: benchmark-shaped opcodes, region
executors, and plan tables are gone; the engine is down to 78 opcodes, 8
`Proto` side tables, and 15,451 engine lines (from 36,020). Several rebuild
slices landed too (packed instructions, boxed strings, table cold sidecar,
proto-owned index caches, `opFastCall`, global slots, partial stack/window
transport).

The result is honest and uniform: every Scenario row now runs 3-10x behind
upstream Luau, with no outliers hidden by special cases. That uniformity is
the signal that the remaining losses are general, structural, and fixable in
five specific places measured below. The simplicity stance is unchanged: no
benchmark-shaped mechanism may return, and the opcode and side-table budgets
only ratchet down.

## Current State (2026-07-09, arm64 darwin, Go 1.26.4)

Capture commands:

```sh
go test -run '^$' -bench 'Luau/.*/ember_run$' -benchmem \
  -cpuprofile /tmp/ember-cpu2.prof -memprofile /tmp/ember-mem2.prof -count=1 .
go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/luau_cli_batch' -count=1 .
go test -run '^$' -bench 'BenchmarkCompileArithmetic' -benchmem -count=1 .
```

Ratio table, `ember_run` ns/op over Luau CLI batch ns/run:

| Row | Ember | Luau | Ratio |
| --- | ---: | ---: | ---: |
| combat_tick | 23,340 | 7,718 | 3.0x |
| inventory_value | 59,881 | 11,698 | 5.1x |
| event_dispatch | 140,693 | 15,702 | 9.0x |
| buff_stack_tick | 51,756 | 11,322 | 4.6x |
| ability_resolution | 54,211 | 13,501 | 4.0x |
| ai_utility_scoring | 376,148 | 73,151 | 5.1x |
| cooldown_scheduler | 242,841 | 41,968 | 5.8x |
| projectile_sweep | 112,819 | 22,554 | 5.0x |
| quest_progress_update | 75,744 | 16,696 | 4.5x |
| behavior_tree_tick | 90,834 | 21,580 | 4.2x |
| threat_aggro_table | 407,660 | 68,115 | 6.0x |
| economy_market_tick | 549,466 | 72,647 | 7.6x |
| formation_layout_score | 842,143 | 153,544 | 5.5x |
| dialogue_condition_eval | 114,804 | 23,404 | 4.9x |
| procgen_room_scoring | 158,982 | 32,957 | 4.8x |
| save_state_diff | 325,762 | 53,180 | 6.1x |
| path_relaxation | 172,062 | 32,803 | 5.2x |
| component_churn | 304,885 | 43,733 | 7.0x |
| prototype_fallback | 249,790 | 27,088 | 9.2x |
| signal_bus_callbacks | 248,578 | 29,181 | 8.5x |
| state_machine_transitions | 86,972 | 14,502 | 6.0x |
| sparse_grid_neighbors | 4,032,541 | 540,713 | 7.5x |
| dirty_metatable_writes | 184,347 | 24,747 | 7.4x |
| array_hole_compaction | 199,422 | 28,047 | 7.1x |
| command_vararg_router | 222,703 | 22,161 | 10.0x |

Geometric mean is roughly 5.7x. Top10 markers: `arithmetic_for` 2.0x,
`table_fields` 3.0x, `method_calls` 3.6x, `closures_upvalues` 3.8x,
`varargs_select` 3.6x, `recursive_fibonacci` 10.2x (5.66ms vs 555us).
Compile marker: `BenchmarkCompileArithmetic` 40,086 ns/op, 470 allocs/op.

## Where We Lose, Exactly

Five loss centers, from the fresh CPU and heap profiles. Percentages are of
total benchmark samples unless stated.

L1. Dispatch loop mechanics, roughly half of all CPU.
`runDirectFrameCore` is 43.3% flat, and much of that flat time is loop
scaffolding rather than opcode work:

- `packedInstruction.unpack` is 9.9% flat on its own: the loop converts
  each 16-byte packed instruction into the old 40-byte `instruction`
  struct on every dispatch (`ins := code[frame.pc].unpack()`); the packing
  slice bought dense storage and then paid a per-instruction decode tax.
- The trace seam is generic (`runDirectFrameCore[T directFrameTrace]`),
  and Go's gcshape lowering does not devirtualize the no-op methods: the
  literal do-nothing `directFrameNoTrace.countInstruction` shows up as
  1.2% flat, and PIC-counter calls (`addGlobalSlotHit`, `addSideExit`)
  still run inside the production loop.
- `frame.pc` lives in the heap frame object and is loaded and stored per
  instruction; budget and debug-hook booleans are re-tested per
  instruction even when off.
- Evidence that everything pays this: pure-arithmetic `arithmetic_for` is
  2.0x and `iterative_fibonacci` regressed from 1.67us to 2.90us with no
  allocation involved at all.

L2. Call machinery, the dominant cost on call-heavy rows.
`recursive_fibonacci` is 10.2x, and its profile shows only about half the
time in the dispatch core; the rest is per-call plumbing: a Go stack frame
per script call (`runInlineScriptCallFixedOneNoHook` recursion),
`vmFrame` pool objects with `resetFrame`/`resetFrameIntoRegisters`/
`resetForReuse` at ~16%, `newClosureCallFrameFixed` 15.8% cumulative,
plus `pushFrame`, `frameSlot`, and result plumbing (`vmReturnedValue`,
`typedslicecopy`). The contiguous stack exists (`thread.stack`), but calls
still materialize frame objects and recurse through Go functions instead of
staying inside one dispatch loop. This is why `method_calls` 3.6x,
`closures_upvalues` 3.8x, `signal_bus_callbacks` 8.5x,
`prototype_fallback` 9.2x (function `__index` per miss), and
`command_vararg_router` 10.0x cluster at the top.

L3. `opFastCall` result transport allocates per call.
On builtin-heavy rows (`state_machine_transitions`, `component_churn`,
`buff_stack_tick`), `runDirectFastCall` is 85.6% of allocated objects: the
general builtin path wraps results in fresh `[]Value{...}` slices
(`directFrameApplyCallIslandResults(..., []Value{value})`) and builds arg
slices on fallback. The deleted per-builtin opcodes wrote results in place;
their general replacement must too. This is the exact source of the
allocation regressions: `state_machine_transitions` 22 to 138 allocs/op,
`buff_stack_tick` 34 to 209, `component_churn` 50 to 286,
`array_hole_compaction` 57 to 656.

L4. Allocation and run-entry churn keep the GC hot.
GC background work (`madvise`, `pthread_cond_signal`, `kevent`) is ~19% of
samples. Heap attribution: table construction 48% of bytes
(`newTableStorage` + `newTableWithCapacity`), `runDirectFastCall` 21.6%
(L3), `growFastArray` 8.1%, `growStack` 7.1% (the value stack is rebuilt
from zero every `Run`; nothing is pooled across runs), `globalEnv.get`
5.7% cumulative (per-run global slot cache refill), coroutine machinery
~12% cumulative on its row (`coroutine_yield` regressed from 37 to 64
allocs/op).

L5. String-keyed field access still compares bytes.
`rawStringField` is 3.7% flat plus `memequal` 1.3%: `tableStringField`
stores a plain Go `string`, so every inline-field probe is a linear scan
with byte comparison, and the per-pc index caches compare strings on hit
verification. String boxes with cached hashes landed in `Value`, but field
slots and caches do not use them, so the box investment is not paying off
yet.

The old-17 rows regressed at the reset (for example `event_dispatch` 9.0x,
`formation_layout_score` 5.5x) for these same five reasons; they contain
ordinary loops, field traffic, and calls, and are won back by the same
fixes, not by re-specialization.

## How We Win

Luau's interpreter gets its speed from exactly the things Ember still lacks
in the loop: a one-word instruction fetch with operands read in place, pc
and base kept in locals, calls that stay inside the dispatch loop, builtins
that write results into registers, and interned strings compared by
pointer. Each phase below closes one measured gap, is general for all
programs, and deletes the mechanism it replaces.

## Scope

In scope: private VM, bytecode, table, string, and compiler-output changes
behind the existing public surface; deletion of transport and frame
machinery the new ABI replaces; benchmark, allocation, and budget gates.

Out of scope: CGo, new dependencies, native codegen, public API changes,
any workload-shaped mechanism (opcode-count and side-table budgets stay
ratchet-down), unsafe code outside the one optional slice below.

## Design Rules

Carried from the previous plan: red tracer first, phrased against
`Compile`/`Run` behavior or size/allocation/complexity budgets; behavior
tests green through every slice; one mechanism per job with the replaced
path deleted in the same phase; determinism documented when it is
host-visible. The ratio gate runs in hard mode from Phase 1 onward at the
current milestone bound (start at 4.0x after Phase 3, per milestones
below).

## Phase 1: Zero-Overhead Dispatch

Goal: remove the per-instruction taxes so the switch loop costs fetch,
decode-in-place, and the opcode body, nothing else. Attacks L1 (~50% of
CPU); every row benefits.

Slices:

1. `1.1 Operands read in place`
   - Dispatch on `code[pc].op` and read `a/b/c/d` directly from the packed
     element (pointer or value copy of the 16-byte element, no `unpack`,
     no 40-byte `instruction` materialization anywhere in the run path).
   - Delete `packedInstruction.unpack` from the hot path; keep it for the
     disassembler and tests only.
   - Red tracers: `TestRunPathDoesNotMaterializeUnpackedInstructions`
     (asserts the packed accessors are the only decode used by execution,
     via a build-time seam or coverage of the deleted call), plus the
     existing packed round-trip tests staying green.

2. `1.2 Concrete production loop, instrumentation fully outside`
   - Make the production loop a plain non-generic function with zero trace
     or PIC-counter calls; the instrumented loop is a separate function
     selected by tests (the generic seam may remain there or be deleted).
   - Move remaining production-path counter calls (`addGlobalSlotHit`,
     `addSideExit`, `getCounted`/`getSymbolCounted` variants) into the
     instrumented loop only.
   - Red tracers: `TestRunProductionLoopHasNoInstrumentationSideEffects`
     (existing, extended to assert PIC counters stay untouched by a plain
     run), and a benchmark note in this file showing `arithmetic_for`
     movement.

3. `1.3 Locals for pc and registers, write-back at edges`
   - Keep `pc`, `code`, `registers`, and `constants` in loop locals; write
     `frame.pc` only at calls, side exits, yields, and returns. Select the
     budget/hook-checking loop variant once at frame entry instead of
     testing three booleans per instruction.
   - Audit bounds-check elimination with `-gcflags='-d=ssa/check_bce'`
     and shape slices (`code`, `registers`) so the checks hoist.
   - Red tracers: `TestDebugHooksAndBudgetsStillFireAtDocumentedPoints`
     (behavior), plus recorded before/after per-instruction cost on
     `arithmetic_for` and `iterative_fibonacci` in this file.

Acceptance for the phase: `arithmetic_for` at or under 1.3x,
`iterative_fibonacci` at or under 2.0us, geomean improvement recorded on
all 25 rows, no allocation movement.

Checks:

```sh
go test -run 'Test(Run|Packed|Instruction|Debug)' ./...
go test -run '^TestScenario|^TestTop10|^TestClassic' .
go test -run '^$' -bench '^Benchmark(ScenarioLuau|Top10Luau|ClassicLuau)/.*/ember_run$' -benchmem -count=3 .
scripts/check-fast && scripts/check
```

Risks:

- Manual write-back of `frame.pc` is easy to miss on one exit path; the
  yield, error, pcall, and coroutine tests are the guard and must stay
  green.
- Two loop variants (plain, hooked/instrumented) is the accepted ceiling;
  do not fork further copies.

## Phase 2: Calls Stay In The Loop

Goal: a script-to-script call becomes push-frame-record-and-continue in the
same dispatch loop; return becomes pop-and-continue. No Go recursion, no
frame heap objects, no register copying beyond argument placement. Attacks
L2; targets `recursive_fibonacci` 10.2x, `method_calls` 3.6x,
`closures_upvalues` 3.8x, `signal_bus_callbacks` 8.5x,
`command_vararg_router` 10.0x.

Design: the thread owns one value stack (exists) plus a flat frame-record
slice (proto, return pc, base, result register and count, vararg window,
flags). The dispatch loop carries the current record in locals. Host calls,
metamethod calls into script code, pcall, and coroutine boundaries may
still use Go-level calls; the plain script call path may not. `vmFrame`
objects survive only where suspension needs them (coroutines) until 2.4
removes that too.

Slices:

1. `2.1 In-loop fixed-arity calls and returns`
   - `opCall`/`opCallOne` with a script callee and fixed arity push a
     frame record and continue the loop; `opReturn`/`opReturnOne` pop and
     continue in the caller. Arguments are already contiguous at the top
     of the caller window; entering copies nothing beyond nil-fill.
   - Red tracers: `TestScriptCallFixedArityDoesNotAllocatePerCall`
     (tightened to zero allocs), and
     `TestDeepRecursionGrowsOneStackWithoutFramePerCall` (recursion depth
     scales with the frame-record slice only).

2. `2.2 Multi-return, open calls, and varargs on the frame records`
   - Open-arity calls and returns adjust counts through the shared stack;
     `vmResultWindow.ownedValues`, `copiedCallArgs`, and the remaining
     window-to-slice materialization leave the internal path.
   - Vararg functions record their extra-argument window in the frame
     record; `select`, vararg forwarding, and final-call expansion read
     it.
   - Red tracers: `TestOpenCallResultsDoNotAllocatePerCall` and
     `TestVarargRouterShapeRunsWithoutPerCallAllocation` (generic vararg
     dispatch shape through `Compile`/`Run`, no row name).

3. `2.3 Metamethod and cross-boundary calls reuse the same records`
   - Function-valued `__index`/`__newindex`, `__call`, comparison and
     arithmetic metamethods enter script callees through the same
     push-record path (a scratch window for arguments), so
     `prototype_fallback`-shaped code stops paying Go-call overhead per
     miss.
   - Red tracer: `TestFunctionIndexMetamethodCallStaysInLoop` (alloc and
     depth budget through `Run`).

4. `2.4 Coroutines suspend the record stack`
   - A coroutine owns its (value stack, frame records) pair; suspend and
     resume move values between stacks through windows instead of owned
     slices; `vmFrame` objects and their pool are deleted.
   - Red tracers: existing coroutine behavior tests plus
     `TestCoroutineResumeTransportDoesNotAllocatePerHop` (budget), and
     `rg 'vmFramePool|resetForReuse'` finding no live code.

Acceptance for the phase: `recursive_fibonacci` at or under 2.5x,
`method_calls` at or under 1.8x, `signal_bus_callbacks` and
`command_vararg_router` at or under 4.0x, `coroutine_yield` allocation
back at or under 40 allocs/op.

Checks:

```sh
go test -run 'Test.*(Call|Return|Vararg|Recursion|Coroutine|Metamethod)' ./...
go test -run '^TestScenario|^TestTop10|^TestClassic' .
go test -run '^$' -bench '^Benchmark(ScenarioLuau|ClassicLuau|Top10Luau)/.*/ember_run$' -benchmem -count=3 .
scripts/check-fast && scripts/check
```

Risks:

- This is the largest slice cluster; land strictly in order, keeping the
  recursive path as the fallback until 2.2, then delete it (no dual
  maintenance).
- pcall unwinding across in-loop frames needs explicit tests: protected
  frames record their depth, and error recovery truncates to it.
- Debug hooks must observe the same call/return events; the instrumented
  loop carries the hook calls.

## Phase 3: Builtins Write Results In Place

Goal: `opFastCall` becomes allocation-free in steady state, restoring the
alloc floors the reset lost. Attacks L3.

Slices:

1. `3.1 In-place builtin ABI`
   - Builtin implementations receive the register window and the result
     window and write results directly; the `[]Value{...}` result wrapping
     and `directFrameApplyCallIslandResults` slice path are deleted. The
     `select('#', ...)` and vararg-consuming builtins read the frame
     record's vararg window.
   - Red tracers: `TestFastCallBuiltinsDoNotAllocatePerCall` (covering the
     builtin table generically) and the tightened row budgets below.

2. `3.2 Alloc budgets ratchet back`
   - Tighten `TestScenarioEmberRunAllocationBudgets` for the regressed
     rows to at or under their pre-reset floors:
     `state_machine_transitions` 22, `buff_stack_tick` 34,
     `component_churn` 50, `array_hole_compaction` 57 allocs/op (or
     better).
   - Red tracer: the budget test itself.

Checks:

```sh
go test -run 'Test.*(FastCall|AllocationBudget)' ./...
go test -run '^$' -bench '^BenchmarkScenarioLuau/' -benchmem -count=3 .
scripts/check-fast && scripts/check
```

Risks:

- Builtins that can call back into script (`table.sort` comparators,
  `tostring` via `__tostring`) must keep re-entrancy through the Phase 2
  record path; give them explicit tests.

## Phase 4: Allocation And Run-Entry Churn

Goal: cut the remaining heap traffic that keeps the GC background at ~19%
of samples. Attacks L4.

Slices:

1. `4.1 Pooled run entry`
   - Pool threads, value stacks, and frame-record slices across `Run`
     calls (`sync.Pool`); `growStack` stops appearing in per-run profiles.
   - Share one immutable base global env for `Run(proto)` without host
     globals; global slot arrays persist per program so `globalEnv.get`
     refill disappears from steady-state profiles.
   - Red tracers: `TestRunMinimalScriptAllocationBudget` tightened to the
     measured floor, and `TestRepeatedRunsDoNotGrowTheValueStack`.

2. `4.2 Table literal shape templates`
   - Compile table literals to a shape template (array size, field names,
     layout) and instantiate by one-block clone; combined with the
     existing storage objects this makes a literal one allocation plus
     content.
   - Red tracer: `TestLoopTableLiteralAllocationBudget` tightened to one
     allocation per literal.

3. `4.3 Array growth policy`
   - Doubling growth with shape-informed initial capacity for append
     loops (`table.insert`, `values[#values+1]`); `growFastArray` falls
     out of the top allocation sites.
   - Red tracer: `TestAppendLoopAmortizesArrayGrowth` (allocation count
     scales logarithmically with elements).

Checks:

```sh
go test -run 'Test.*(Run|Literal|Growth|Global)' ./...
go test -run '^$' -bench '^BenchmarkScenarioLuau/' -benchmem -count=3 .
scripts/check-fast && scripts/check
```

Risks:

- Pooled state must be fully reset between runs or fully re-initialized
  by construction; leaking values across runs is a correctness bug, so the
  pool reset gets its own test including coroutine leftovers.

## Phase 5: String Symbols In Field Slots

Goal: make string-keyed field access compare pointers, not bytes. Attacks
L5 and the remaining `rawStringField`/`memequal` flat cost.

Slices:

1. `5.1 Boxed keys in field storage`
   - `tableStringField` and the hash overflow store the string box pointer
     (with its cached hash) alongside or instead of the raw string; probes
     compare box pointer first, then hash, then bytes; compile-time
     constants and interned runtime keys hit the pointer path.
   - Red tracers: `TestFieldLookupComparesInternedKeysByPointer`
     (mechanism observable via allocation/step budget) and existing
     iteration-order tests staying green.

2. `5.2 Caches keyed by symbol`
   - Per-pc index caches verify hits by box pointer and layout version
     only; the string re-compare on the hit path is deleted.
   - Red tracer: `TestWarmFieldCacheHitDoesNotTouchStringBytes` (budget
     through a step-count or alloc proxy).

Checks:

```sh
go test -run 'Test.*(Field|Intern|Cache|Iteration)' ./...
go test -run '^$' -bench '^BenchmarkScenarioLuau/' -benchmem -count=3 .
scripts/check-fast && scripts/check
```

Risks:

- Host strings and dynamic keys are not interned; the byte-compare
  fallback must stay correct and tested for equal-content distinct boxes.

## Phase 6: Compiler Output Quality And Guards

Goal: emit less work per program and hold compile cost, unchanged in
spirit from the previous plan and kept last because the runtime phases
above dominate.

Slices:

1. `6.1 Jump threading and branch simplification`
   (`TestOptimizerThreadsJumpChains`,
   `TestOptimizerRemovesConstantBranches`).
2. `6.2 Liveness-driven frame shrink`
   (`TestCompilerShrinksFrameUsingLiveness`,
   `TestFrameShrinkPreservesCapturedAndVarargRegisters`); smaller windows
   compound with Phase 2 by shrinking stack traffic per call.
3. `6.3 General constant folding`
   (`TestCompilerFoldsConstantExpressionsWithoutChangingErrors`).
4. `6.4 Compile-cost guard` at the current floor
   (`BenchmarkCompileArithmetic` 40,086 ns/op, 470 allocs/op; budget test
   with explicit numbers, worklist passes if the optimizer grows).

## Optional: 16-Byte NaN-Boxed Value

Unchanged from the previous plan: only if post-Phase-2 profiles still show
register copy pressure; one unsafe file with total accessor coverage and a
differential build tag; reject on any accessor leak. Expected value is
lower now that dispatch and calls dominate; decide by profile, not by
appetite.

## Milestones

- `M1` (Phases 1-2): dispatch and calls fixed. `arithmetic_for` at or
  under 1.3x, `recursive_fibonacci` at or under 2.5x, geomean at or under
  3.0x, no allocation regressions.
- `M2` (Phase 3): allocation floors restored to pre-reset values on the
  regressed rows; GC background under 10% of profile samples.
- `M3` (Phases 4-5): all 25 rows at or under 2.5x, geomean at or under
  2.0x; ratio gate hard at 2.5x.
- `M4` (Phase 6 and polish): all 25 rows at or under 2.0x with count=3;
  gate hard at 2.0x; stretch geomean 1.5x.

## Completion Criteria

- The ratio gate passes at 2.0x for all 25 rows with count=3.
- The five loss centers are gone from profiles as described: no `unpack`
  or trace calls in the production loop, no per-script-call Go recursion
  or frame objects, no per-builtin-call result slices, `growStack` and
  literal churn out of the top allocation sites, no byte comparison on
  warm field-cache hits.
- Opcode and side-table budgets unchanged or lower (78 and 8 today); no
  benchmark-named mechanism anywhere; replaced transport machinery
  (`vmFrame` pool, result-window materialization, unpack path) deleted,
  not flagged off.
- `scripts/check-fast` and `scripts/check` pass; no CGo, no new
  dependencies; unsafe confined as before plus the optional NaN-box file
  if taken.
- This file records per-phase before/after numbers, then gets retired
  together with `general-optimization.md`.
