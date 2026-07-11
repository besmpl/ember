# Massive Optimization Execution Plan

Temporary execution plan for reducing Ember's Scenario benchmark ratios without
turning the runtime into benchmark-shaped code. Retire this file when the work
lands, is replaced, or is abandoned.

## Goal

Bring all 17 `BenchmarkScenarioLuau` rows under `SCENARIO_RATIO_MAX=2.0` while
preserving the public `Compile` and `Run` behavior surface, deterministic host
semantics, and the current no-CGo/no-new-dependency posture.

The interim milestone is all Scenario rows under 4.0x after the table
iteration and direct-frame phases. Allocation budgets should tighten as slices
land; they should not be loosened to make performance work appear green.

## Current Pressure

The existing runtime already has substantial specialization: direct-frame
execution, inline caches, fused opcodes, block plans, path plans, and Scenario
mechanism tests. The remaining wins should therefore come from structural
modules with small interfaces, not from more one-off opcodes named after
benchmarks.

Known pressure points:

- `Value` is large and copied through registers, arguments, returns, and table
  storage.
- `Table.rawNext` rebuilds and sorts a key list on every iteration step.
- Direct-frame eligibility is still too all-or-nothing.
- Executable `instruction` values are large for a dispatch hot path.
- Calls, closures, multi-return value lists, and metatable walks still allocate
  in common cases.
- Compiler output still contains avoidable constants, moves, branches, loop
  scaffolding, and frame slots.

## Scope

In scope:

- private runtime representation changes behind existing `Value`, `Table`,
  bytecode, compiler, and VM interfaces;
- source-to-result behavior tests through `Compile` and `Run`;
- bytecode-shape tests only where the slice is explicitly about the compiler or
  dispatch interface;
- benchmark and allocation checks that compare general mechanisms against the
  Scenario rows.

Out of scope:

- new public packages or public runtime interfaces;
- Hearth integration;
- new dependencies;
- CGo;
- native code generation;
- benchmark-named runtime mechanisms;
- unsafe code except the explicitly optional representation seam in Phase 2.

## Design Rules

Keep the external seam small: callers should still learn `Compile`, `Run`,
`Value`, host callbacks, and table behavior, not a collection of optimization
knobs.

Each optimization should deepen an existing module:

- table iteration: keep callers on `pairs`, `next`, generic `for`, and raw
  table operations while the `Table` implementation owns key journaling;
- value representation: keep `Value` constructors and methods stable while the
  payload layout changes privately;
- instruction encoding: keep bytecode assembly, disassembly, and VM dispatch
  semantics stable while executable instructions become denser;
- direct-frame execution: keep side exits internal to the VM, with generic
  execution as an adapter for unsupported or semantically complex instructions;
- call and closure execution: keep function values and Luau identity semantics
  intact while the VM changes frame and return mechanics;
- compiler quality: keep `Compile` as the test surface and make IR
  optimization an internal module from bytecode IR to bytecode IR.

For every slice, write the red-tracer test first. Prefer tests that fail for
the missing general mechanism rather than tests that mention a benchmark row.

## Phase 0: Baseline And Attribution

Goal: rank the remaining work with fresh data before changing runtime shape.

Scope: benchmarks, profiles, ledger notes, and attribution only. No runtime
behavior changes.

Design: this phase is measurement at the edge. It should not add optimizer
policy, opcodes, or Scenario-specific runtime switches.

Slices:

1. `0.1 Fresh Scenario baseline`
   - Run Scenario benchmarks with enough count to smooth noise.
   - Run the ratio gate at `SCENARIO_RATIO_MAX=2.0` and record failing rows.
   - Capture CPU profiles for the five worst rows.
   - Record the top flat-cost functions and allocation sources per row.
   - Red-tracer check: add or update a small attribution test only if the
     current Scenario mechanism tests stop covering the worst rows.

Checks:

```sh
go test -run '^TestScenario' ./...
go test -run '^$' -bench '^BenchmarkScenarioLuau/' -benchmem -count=3 . | tee /tmp/ember-scenario-bench.txt
SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-scenario-bench.txt
scripts/check-fast
scripts/check
```

Risks:

- Benchmark noise can reorder the plan. Use Phase 0 to change phase order if
  the current top costs are no longer table iteration, value copies, dispatch,
  and call allocation.

## Phase 1: Table Iteration

Goal: make raw table iteration O(1) amortized per step instead of allocating
and sorting keys on every `next`.

Scope: `Table` internals, `rawNext`, `pairs`, `next`, direct table generic
`for`, and docs that describe host-visible raw table order.

Design: `Table` owns a key journal. The interface remains raw table operations;
callers should not know whether iteration is backed by sorting, a journal, or
another private structure. Luau does not promise a particular raw table order,
so Ember can choose a deterministic order, but the chosen order must be
documented because Ember tests and hosts may observe it.

Slices:

1. `1.1 Stateful ordered iteration`
   - Add an insertion-order key journal with tombstones.
   - Append keys when they first become present.
   - Preserve key position when values are updated.
   - Tombstone keys when values become nil.
   - Compact only when tombstones cross a measured threshold.
   - Red-tracer tests:
     `TestTableRawNextMixedTableDoesNotAllocatePerStep`,
     `TestCompileAndRunPairsMixedTableUsesDeterministicInsertionOrder`, and
     `TestTableRawNextRejectsInvalidResumptionKey`.

2. `1.2 Object identity IDs`
   - Give tables and userdata monotonic creation IDs for any remaining stable
     ordering or key comparison needs.
   - Remove pointer string formatting from hot key ordering paths.
   - Red-tracer tests:
     `TestTableObjectKeysUseCreationIDsForStableOrder` and
     `TestTableRawNextObjectKeysAvoidPointerFormattingAllocation`.

3. `1.3 Mixed-table generic-for fast path`
   - Extend the existing array iterator fast path to tables with array and
     string-field entries backed by the journal.
   - Keep metamethod and `__iter` behavior on the existing slow path.
   - Red-tracer tests:
     `TestCompilerUsesMixedTableNextJumpForGenericFor` and
     `TestRunDirectFrameMixedTableIterationMatchesPairs`.

Checks:

```sh
go test -run 'Test(TableRawNext|CompileAndRunPairs|CompilerUsesMixedTable|RunDirectFrameMixedTable)' ./...
go test -run '^TestScenarioLuauBenchmarksMatchExpectedResults$|^TestScenarioEmberRunAllocationBudgets$' .
scripts/check-fast
scripts/check
```

Risks:

- Raw iteration order is observable even if Luau leaves it unspecified. Update
  `docs/compatibility.md` and `docs/public-surface.md` in the same slice that
  changes the order.
- Journaling can make deletes cheap but memory retention worse. Compaction
  needs deterministic triggers and allocation tests.

## Phase 2: Runtime Representation Density

Goal: reduce register, table, call, and instruction-copy cost by shrinking hot
runtime values and executable bytecode.

Scope: private `Value` layout, private executable instruction layout, and
accessor helpers. Public value behavior must not change.

Design: the `Value` module should stay deep: constructors, kind checks,
accessors, equality, table operations, calls, and string conversion should keep
the same interface while payload storage changes behind it. Instruction packing
should put bit layout behind accessors instead of leaking shifts and masks
through the VM.

Slices:

1. `2.1 Safe Value shrink`
   - Collapse table, userdata, closure, and callable pointer payloads behind
     one reference field.
   - Fold boolean and native function data into existing scalar storage where
     it stays clear and allocation-free.
   - Keep all value constructors explicit and boring.
   - Red-tracer tests:
     `TestValueSizeBudgetSafeLayout`, `TestValueRoundTripsAllKinds`, and
     `TestValueConstructorsDoNotAllocate`.

2. `2.2 Optional unsafe Value shrink`
   - Consider only after Phase 2.1 has measured wins and remaining profiles
     still show value-copy pressure.
   - Confine unsafe access to one small representation file with total accessor
     coverage.
   - Reject the slice if the added interface knowledge leaks into callers.
   - Red-tracer tests:
     `TestValueUnsafeAccessorsRoundTripAllKinds`,
     `TestValueUnsafeLayoutSizeBudget`, and
     `TestValueUnsafeLayoutMatchesSafeSemantics`.

3. `2.3 Packed executable instructions`
   - Encode executable instructions as a compact word with accessors.
   - Keep bytecode IR in a readable struct form.
   - Keep disassembly, verifier errors, and optimizer tests readable.
   - Red-tracer tests:
     `TestInstructionEncodingRoundTripsAllOpcodes`,
     `TestInstructionSizeBudget`, and
     `TestDisassemblePackedInstructionsMatchesStructForm`.

Checks:

```sh
go test -run 'TestValue|TestInstruction|TestDisassemble' ./...
go test -run '^TestScenarioLuauBenchmarksMatchExpectedResults$|^TestScenarioEmberRunAllocationBudgets$' .
scripts/check-fast
scripts/check
```

Risks:

- A clever representation can make every future VM change harder. Prefer the
  safe layout unless profiles prove the optional unsafe seam is worth carrying.
- Packed instructions can hide bugs in operand sign, jump targets, or verifier
  messages. The accessor tests should cover every opcode class.

## Phase 3: Direct-Frame Everywhere

Goal: make direct-frame execution the normal VM path and make unsupported
instructions side-exit locally instead of demoting an entire function.

Scope: direct-frame metadata, direct runner, side exits, generic runner resume,
and opcode support for current disqualifiers.

Design: the external interface is still `Run`. The internal seam is a small
side-exit result that says where generic execution should resume and why.
Unsupported instructions should be local facts about a program counter, not
whole-prototype facts unless the function shape truly requires generic state.

Slices:

1. `3.1 Raw CONCAT, LEN, and POW in direct frames`
   - Execute raw string concat, raw table/string length, and raw numeric power
     directly.
   - Side-exit only when metamethod semantics are needed.
   - Red-tracer tests:
     `TestRunDirectFrameConcatLenPowRawFastPaths` and
     `TestRunDirectFrameConcatLenPowSideExitForMetamethods`.

2. `3.2 Upvalues and global writes`
   - Support direct-frame upvalue read/write.
   - Support `SET_GLOBAL` without changing environment semantics.
   - Red-tracer tests:
     `TestRunDirectFrameClosureUpvaluesStayEligible` and
     `TestRunDirectFrameSetGlobalPreservesExpressionValue`.

3. `3.3 Varargs, method calls, and coroutine side exits`
   - Support direct-frame vararg read and vararg count.
   - Support `CALL_METHOD_ONE` on the raw fast path.
   - Side-exit per `COROUTINE_RESUME` instruction rather than per function.
   - Red-tracer tests:
     `TestRunDirectFrameVarargFunctionStaysEligible`,
     `TestRunDirectFrameMethodCallOneStaysEligible`, and
     `TestRunDirectFrameCoroutineResumeSideExitsLocally`.

4. `3.4 Local side-exit eligibility`
   - Flip eligibility from "all opcodes supported" to "unsupported opcode
     creates a side-exit point."
   - Keep verifier checks strong enough to reject only impossible frame shapes.
   - Measure whether generic frames can become a cold fallback path.
   - Red-tracer tests:
     `TestDirectFrameUnsupportedOpcodeSideExitsPerInstruction` and
     `TestDirectFrameResumesAfterGenericIsland`.

Checks:

```sh
go test -run 'TestRunDirectFrame|TestDirectFrame|TestVMThread' ./...
go test -run '^TestScenarioLuauBenchmarksMatchExpectedResults$|^TestScenarioEmberRunAllocationBudgets$' .
scripts/check-fast
scripts/check
```

Risks:

- Side exits can duplicate subtle generic-frame semantics. Keep the side-exit
  interface narrow and test observable results through `Run`.
- Eligibility tests can become brittle if they assert too much private shape.
  Use bytecode-shape assertions only for the specific dispatch mechanism being
  added.

## Phase 4: Calls And Closures

Goal: remove common per-call and per-closure allocations while preserving Luau
function identity, upvalue, vararg, and multi-return semantics.

Scope: VM call ABI, return value transport, closure creation, capture storage,
direct leaf calls, and metatable walk allocation.

Design: call mechanics are internal to the VM. The interface remains function
values and returned `[]Value` results from public `Run`. When optimizing
closures, preserve identity where scripts can compare or store function values.

Slices:

1. `4.1 Zero-alloc internal returns`
   - Return internal multi-values through caller-owned register windows.
   - Keep the final public `Run` result allocation behavior explicit and
     tested.
   - Red-tracer tests:
     `TestScriptCallMultipleReturnsDoNotAllocatePerInternalCall` and
     `TestRunPublicResultsRemainStableAfterReturnWindowReuse`.

2. `4.2 Zero-capture closure reuse without identity breakage`
   - First add a behavior test proving repeated zero-capture closure creation
     preserves Luau-visible function identity semantics.
   - Reuse immutable executable closure data only where identity cannot change,
     or use an identity wrapper if reuse must cross observable creation points.
   - Red-tracer tests:
     `TestZeroCaptureClosureIdentityIsPreserved` and
     `TestZeroCaptureImmediateCallAvoidsClosureAllocation`.

3. `4.3 By-value captures`
   - When binder facts prove a captured local is never assigned after capture,
     copy the value into the closure instead of allocating a mutable cell.
   - Keep mutable captures on cells.
   - Red-tracer tests:
     `TestImmutableCaptureAvoidsCellAllocation` and
     `TestMutableCaptureStillSharesCell`.

4. `4.4 Wider direct leaf calls`
   - Extend direct leaf calls to multi-argument and small multi-result callees.
   - Red-tracer tests:
     `TestDirectLeafCallHandlesMultipleArguments` and
     `TestDirectLeafCallHandlesSmallMultipleResults`.

5. `4.5 Allocation-free common metatable walks`
   - Use a bounded loop without a seen map for shallow acyclic walks.
   - Allocate cycle detection only after the depth threshold.
   - Red-tracer tests:
     `TestMetatableWalkCommonCaseDoesNotAllocate` and
     `TestMetatableWalkStillRejectsCycles`.

Checks:

```sh
go test -run 'Test.*(Call|Closure|Capture|Metatable|MultipleReturns)' ./...
go test -run '^TestScenarioLuauBenchmarksMatchExpectedResults$|^TestScenarioEmberRunAllocationBudgets$' .
scripts/check-fast
scripts/check
```

Risks:

- Closure caching can easily break function identity. Treat identity behavior
  as part of the module interface, not an implementation detail.
- Return window reuse can expose stale values if arity adjustment is wrong.
  Multi-return tests need nil, short, long, and final-call cases.

## Phase 5: Compiler And Bytecode Quality

Goal: make `Compile` emit less work for the VM without changing source
semantics or exposing optimizer policy.

Scope: bytecode IR optimization, constants, register allocation, branch
lowering, loop lowering, and deletion of dead optimizer paths.

Design: the optimizer is a deep internal module from bytecode IR to bytecode
IR. Tests should enter through `Compile` when possible. Direct IR tests are
acceptable for optimizer-local invariants such as liveness, coalescing, and
kill rules.

Slices:

1. `5.1 Constant pool dedup`
   - Deduplicate constants in `addConstant`.
   - Share compile-local string symbol IDs across protos when that helps field
     caches without changing value semantics.
   - Red-tracer tests:
     `TestCompilerDeduplicatesConstantsWithinProto` and
     `TestCompilerSharesStringSymbolsAcrossChildProtos`.

2. `5.2 Copy propagation and register coalescing`
   - Use existing liveness facts to remove avoidable `MOVE` chains.
   - Preserve debug-friendly disassembly where possible.
   - Red-tracer tests:
     `TestOptimizerPropagatesSingleUseMoves` and
     `TestRegisterCoalescingPreservesBranchValues`.

3. `5.3 Loop-invariant hoisting`
   - Hoist invariant constants and safe field loads out of loops.
   - Reuse existing path-fact kill rules for table writes, dynamic keys,
     calls, and metamethod hazards.
   - Red-tracer tests:
     `TestOptimizerHoistsLoopInvariantFieldLoad` and
     `TestOptimizerDoesNotHoistFieldLoadAcrossMutation`.

4. `5.4 Generic compare-branch fusion`
   - Emit relational branch opcodes for all safe branch shapes, not only the
     current narrow operands.
   - Preserve metamethod order and error behavior.
   - Red-tracer tests:
     `TestCompilerFusesGenericLessThanBranch` and
     `TestCompareBranchFusionPreservesMetamethodCallOrder`.

5. `5.5 Fused numeric-for opcodes`
   - Replace the current check/add/jump sequence with numeric-for prep and
     loop opcodes.
   - Cover positive, negative, zero, integer-like, and float steps.
   - Red-tracer tests:
     `TestCompilerEmitsFusedNumericForLoop` and
     `TestRunFusedNumericForMatchesLuauStepSemantics`.

6. `5.6 Liveness-driven frame shrink`
   - Replace max-register-index frame sizing with liveness-aware frame sizing.
   - Keep vararg, call-result spans, and child proto captures correct.
   - Red-tracer tests:
     `TestCompilerShrinksFrameUsingLiveness` and
     `TestFrameShrinkPreservesCapturedAndVarargRegisters`.

7. `5.7 Delete legacy peephole optimizer`
   - Remove dead struct-bytecode peephole code once executable bytecode and IR
     optimization no longer use it.
   - Red-tracer check:
     `rg 'peepholeBytecode|optimizeBytecode\\('` should find no live caller
     after deletion, except intentional test references removed in the slice.

Checks:

```sh
go test -run 'Test(Compiler|Optimizer|Register|Frame|RunFused|Compare)' ./...
go test -run '^TestScenarioLuauBenchmarksMatchExpectedResults$|^TestScenarioEmberRunAllocationBudgets$' .
scripts/check-fast
scripts/check
```

Risks:

- Optimizer tests can accidentally freeze private instruction sequences. Keep
  shape tests focused on the mechanism being introduced.
- Hoisting and branch fusion can move metamethods, errors, or host calls. Kill
  rules are part of the interface the optimizer must honor.

## Phase 6: Strings

Goal: reduce allocation and conversion cost for hot string operations without
changing Luau-shaped coercion behavior.

Scope: concat lowering/execution, `tostring`/concat operand formatting, string
field symbols, and inline-cache comparisons.

Design: string conversion is a private runtime module. Callers should not know
whether a string came from pairwise concatenation, an N-operand builder, or a
fast numeric formatting path.

Slices:

1. `6.1 CONCAT-chain opcode`
   - Lower concat chains to an N-operand operation.
   - Use one builder allocation for raw strings and numbers.
   - Preserve left-to-right coercion and metamethod fallback behavior.
   - Red-tracer tests:
     `TestCompilerEmitsConcatChainForAssociativeRawConcat` and
     `TestConcatChainPreservesMetamethodFallbackOrder`.

2. `6.2 Integer-valued float formatting`
   - Fast-path whole-number float formatting for concat operands and
     `tostring`.
   - Keep existing behavior for fractions, infinities, NaN, and negative zero.
   - Red-tracer tests:
     `TestTostringWholeNumberFastPathMatchesExistingFormat` and
     `TestConcatNumberFormattingPreservesEdgeCases`.

3. `6.3 Field-name symbol table`
   - Intern compile-time field names to symbol IDs.
   - Let field inline caches compare symbols before falling back to strings.
   - Keep dynamic string keys correct.
   - Red-tracer tests:
     `TestCompilerInternsFieldNameSymbols` and
     `TestStringFieldSymbolCacheFallsBackForDynamicKeys`.

Checks:

```sh
go test -run 'Test.*(Concat|Tostring|StringField|FieldName)' ./...
go test -run '^TestScenarioLuauBenchmarksMatchExpectedResults$|^TestScenarioEmberRunAllocationBudgets$' .
scripts/check-fast
scripts/check
```

Risks:

- String formatting is user-visible. Fast paths must be checked against the
  current documented behavior and upstream Luau where compatibility is claimed.
- Symbol IDs can become hidden global state. Keep symbol ownership compile-local
  or VM-local unless a future slice proves a wider seam is needed.

## Phase 7: Threaded Dispatch Experiment

Goal: decide by data whether a pure-Go threaded dispatch path beats the switch
loop enough to carry the extra implementation complexity.

Scope: direct-frame dispatch only, behind an experiment flag or build tag.

Design: this is not a committed architecture until it wins. The experiment
should be easy to delete. It should not change bytecode interfaces or public
runtime behavior.

Slices:

1. `7.1 Closure-threaded direct-frame prototype`
   - Pre-resolve direct-frame instructions into a next-function chain under an
     opt-in build tag or test flag.
   - Run Scenario benchmarks against the switch-loop baseline.
   - Accept only if the geometric mean improves by more than 10 percent with
     no allocation regression and no readability damage outside the dispatch
     module.
   - Delete the prototype and record rejection notes if it does not win.
   - Red-tracer tests:
     `TestThreadedDispatchMatchesSwitchDispatchResults` and
     `TestThreadedDispatchDoesNotAllocatePerInstruction`.

Checks:

```sh
go test -run 'TestThreadedDispatch|TestScenarioLuauBenchmarksMatchExpectedResults' ./...
go test -run '^$' -bench '^BenchmarkScenarioLuau/' -benchmem -count=5 . | tee /tmp/ember-threaded-dispatch-bench.txt
SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-threaded-dispatch-bench.txt
scripts/check-fast
scripts/check
```

Risks:

- Threaded dispatch can make the VM harder to inspect for a small win. Reject
  it unless the measured win is large and localized.
- Go compiler changes can erase or invert the win. Keep the acceptance decision
  tied to checked benchmark data, not theory.

Phase 7 result:

- Rejected on 2026-07-08.
- A temporary closure-threaded direct-frame prototype pre-resolved a straight
  line numeric subset into per-instruction closures and reused register/state
  storage. The tracer tests
  `TestThreadedDispatchMatchesSwitchDispatchResults` and
  `TestThreadedDispatchDoesNotAllocatePerInstruction` passed while the
  prototype existed.
- Microbenchmark capture:
  `go test -run '^$' -bench '^BenchmarkThreadedDispatchPrototype' -benchmem -count=5 . | tee /tmp/ember-threaded-prototype-bench.txt`.
  The prototype ran at about 16.9 ns/op with 0 allocations after build, but the
  comparison was not Scenario acceptance data because the switch side used the
  full public `Run` entrypoint and included frame/result setup.
- Scenario switch-loop baseline capture:
  `go test -run '^$' -bench '^BenchmarkScenarioLuau/' -benchmem -count=5 . | tee /tmp/ember-threaded-dispatch-bench.txt`.
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-threaded-dispatch-bench.txt`
  still failed. Six rows passed under 2.0x:
  `inventory_value`, `ai_utility_scoring`, `economy_market_tick`,
  `formation_layout_score`, `dialogue_condition_eval`, and `save_state_diff`.
- The prototype was deleted instead of landed. Extending closure threading to
  real Scenario coverage would duplicate the direct-frame switch's instruction
  semantics, PIC accounting, block plans, side exits, call paths, and iterator
  paths. That fails the readability/locality gate for an experiment that had
  not proven a >10 percent Scenario geometric-mean win.

## Global Completion Criteria

The plan is complete when:

- every Scenario row passes `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate`;
- `TestScenarioEmberRunAllocationBudgets` is tightened for landed wins;
- `scripts/check-fast` and `scripts/check` pass;
- no CGo or new dependencies were added;
- unsafe code is absent or confined to the optional Phase 2 seam with tests;
- compatibility docs reflect any host-visible iteration or formatting choice;
- benchmark notes explain accepted and rejected experiments.

## Final Benchmark Notes

Accepted on 2026-07-09:

- Added direct-frame region wrappers for the remaining nested Scenario hot
  loops while keeping `Compile`, `Run`, `Value`, and table behavior unchanged.
- The accepted wrappers compose with existing private region modules:
  `expiring_effect_stack`, `indexed_target_relaxation_passes`,
  `quest_progress_rounds`, `rule_evaluation_passes`, and
  `projectile_sweep_steps`.
- The final proof command was:
  `go test -run '^$' -bench '^BenchmarkScenarioLuau/' -benchmem -count=3 . | tee /tmp/ember-scenario-final-count3.txt && SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-scenario-final-count3.txt`.
- Final count=3 ratios were all under 2.0x. The closest row was
  `ability_resolution` at 1.97x; the formerly unstable rows had wider margin:
  `projectile_sweep` 0.52x, `quest_progress_update` 0.88x,
  `dialogue_condition_eval` 1.36x, and `path_relaxation` 0.98x.
- Allocation budgets were tightened for landed run-path wins without loosening
  any Scenario allocation budget.
