# General Optimization Execution Plan

Temporary execution plan for the next large Ember speed push. Retire this
file when the work lands, is replaced, or is abandoned.

The previous plan (`massive-optimization.md`) brought the original 17
Scenario rows under 2.0x of upstream Luau, but a large share of those wins
came from benchmark-shaped machinery: region execution plans, fused opcodes
named after row patterns, per-row fast paths, and stacked fact tables. That
machinery made the compiler and VM a large web of special cases.

This plan reverses that direction. Simplicity is a goal equal to speed:

1. Reset the engine to a small general core by deleting the specialized
   machinery first, accepting a temporary benchmark regression.
2. Rebuild speed with general mechanisms only: denser representations, a
   cheaper call ABI, better caches, and better compiler output that help
   every program equally.

The end state is a boring, Go-shaped interpreter: one dispatch loop, a small
opcode set, no mechanism that exists for one workload, and ratios earned by
general design rather than pattern matching.

## Goal

Make general programs fast with a simple engine, while staying pure Go: no
CGo, no new dependencies, no native codegen, no public interface breaks.

The proof set is all 25 Scenario rows, especially the 8 general-workload
rows that use ordinary Luau shapes (metatable fallbacks, callbacks, varargs,
string keys, table churn) and currently sit far behind upstream Luau:

| Row | Ember ns/op | Luau ns/run | Ratio |
| --- | ---: | ---: | ---: |
| component_churn | 194,406 | 44,414 | 4.4x |
| prototype_fallback | 398,537 | 26,911 | 14.8x |
| signal_bus_callbacks | 195,966 | 28,710 | 6.8x |
| state_machine_transitions | 43,101 | 14,151 | 3.0x |
| sparse_grid_neighbors | 4,178,987 | 543,098 | 7.7x |
| dirty_metatable_writes | 256,702 | 24,162 | 10.6x |
| array_hole_compaction | 87,985 | 28,099 | 3.1x |
| command_vararg_router | 311,205 | 21,717 | 14.3x |

Two Top10 rows also lag for general reasons: `closures_upvalues` 2.7x and
`varargs_select` 1.9x.

Baseline capture (2026-07-09, arm64 darwin, Go 1.26.4):

```sh
go test -run '^$' -bench 'Luau/.*/ember_run$' -benchmem \
  -cpuprofile /tmp/ember-cpu.prof -memprofile /tmp/ember-mem.prof -count=1 .
go test -run '^$' -bench 'Luau/.*/luau_cli_batch$' -count=1 .
```

## Measured Pressure

CPU attribution across all benchmark rows:

- Dispatch scaffolding is the single largest flat cost. `runDirectFrame` is
  22.9% flat overall and 27.5% flat on the general rows; `runGenericFrame`
  adds 4-10% flat. Roughly 9% of total cycles are loop overhead before any
  opcode work: instruction load of a 40-byte struct, a jump-to-next peephole
  check, instrumentation nil-checks, per-pc plan-table probes, and the
  switch dispatch itself.
- GC and scheduler background work (`madvise`, `kevent`, `pthread_cond_*`)
  is 15-20% of samples, driven by allocation churn.
- Allocation sources (8.57GB total during the baseline run):
  `newTableWithCapacity` 61%, `growFastArray` 9%, `vmValueList.ownedValues`
  3.4%, `vmFrame.reset` 2.8%, `callRuntimeMetamethod2/3` ~3% cum,
  `globalEnv.get` + `runtimeGlobals` ~4.7% cum.
- String-keyed table access costs 5-8%: `memequal`, `rawStringField` linear
  scans, and dynamic per-frame index caches.
- On the general rows, `tableAccess.get/getSeen` (metatable `__index` walks
  plus function-valued fallback calls) is 13% cumulative.

Representation sizes today:

- `Value` is 40 bytes (kind + bool + nativeID + float64 + string header +
  pointer). Every register move, argument, return, and table slot copies 40B.
- Executable `instruction` is 40 bytes (op uint8 + four ints).
- `Table` is 256 bytes before any content: six version counters, two inline
  string fields at 56B each, iteration journal pointer, index-cache words.
- `tableKey` is a 48-byte struct used as a Go map key, so generic map access
  hashes 48 bytes including a string header per lookup.
- Frames carry `indexCaches` sized `len(proto.code)` at ~264B per pc,
  allocated or cleared on every call and thrown away between calls.

## Complexity Ledger Baseline

Recorded so the reset and the no-regrowth budgets have hard numbers:

- opcodes: 101;
- `vm.go`: 17,075 lines; `bytecode.go`: 14,104 lines; `emitter.go`: 4,841
  lines;
- `Proto` carries roughly 25 plan/fact side tables, most feeding
  benchmark-shaped execution (region plans, verified plans, block plans,
  path plans/facts, predicate branches, refinements, reduction facts,
  row-field op tables, self-call-add ops, per-proto fast-path flags);
- the VM runs two dispatch loops (direct and generic) plus per-row region
  executors and one-off generic islands.

Every phase below updates this ledger with lines deleted and budget moves.
Net negative lines in the engine is a success signal, not a side effect.

## Scope

In scope:

- deletion of benchmark-shaped opcodes, plans, fact tables, region
  executors, and their emitter lowerings and shape tests;
- private representation changes behind the existing `Value`, `Table`,
  bytecode, compiler, and VM interfaces;
- VM call ABI, frame layout, value transport, and cache placement;
- compiler IR quality that reduces executed work for all programs;
- benchmark, allocation, and profile checks across the full row set.

Out of scope:

- CGo, new dependencies, native codegen, goroutine-per-call schemes;
- new public packages or public API changes;
- any new workload-shaped mechanism: no opcode, plan, cache, or compiler
  rule that exists because one benchmark row needs it (adding one is a plan
  violation, not a slice);
- unsafe code outside the single optional slice marked below (the existing
  `unsafe.Pointer` payload field remains).

## What Counts As General

The keep-or-delete rule for every mechanism, applied in Phase 1 and enforced
afterward:

Keep a mechanism only if it serves any program with that shape and its
trigger is a language shape, not a code pattern from a benchmark:

- numeric `for` prep/loop opcodes; generic compare-and-branch on registers
  and constants; constant-operand arithmetic (`opAddK` family);
- one `opFastCall` for base-library builtins by ID (the general form of
  today's per-builtin opcodes);
- generic `for` iterator opcodes; closure, upvalue, vararg, call, and
  return opcodes;
- inline caches keyed by table shape for field access and method calls;
- constant decode caches on `Proto` (`constantNumbers`, `constantKeys`,
  string symbols), upvalue descriptors, entry-nil registers.

Delete everything whose trigger is a benchmark pattern:

- all Scenario-named region execution plans and their descriptors;
- multi-field and row-field fusion opcodes (`opSetStringField2`,
  `opAddSubStringField2`, `opSubAddStringField`, the
  `opJumpIfRowStringField*` and `opGetRowStringField*` families);
- call fusions (`opCallUpvalueSelfOne`, `opCallUpvalueSelfKOne`,
  `opCallUpvalueSelfAddKOne`, `opCallTableFieldKeyOne`) and their generic
  islands;
- per-proto fast-path flags (`fastMethodShieldDamage`, `fastMethodFieldAdd`,
  `fastVariadicWeights`, `fastUpvalueAdd`) and the plan/fact tables that
  exist to prove those paths safe (verified plans, block plans, direct-block
  plans, path plans/facts, predicate branches, branch/finite-tag
  refinements, reduction facts, kind-fact tables beyond constant decode);
- per-builtin intrinsic opcodes (`opTableInsert`, `opTableRemove`,
  `opMathMin`, `opRawLen`, `opSelectVarargCount`) once `opFastCall`
  replaces them.

General mechanisms that the rebuild replaces later (direct leaf calls,
immediate-call closures, `vmValueList` inline transport, per-frame index
caches) stay through Phase 1 and are deleted by the phase that replaces
them, so call performance never falls off a second cliff.

## Design Rules

- Keep the external seam small: callers keep learning `Compile`, `Run`,
  `Value`, host callbacks, and table behavior only.
- Every slice starts with a red tracer test phrased against `Compile`/`Run`
  behavior or a size, allocation, or complexity budget, never against a
  benchmark row name.
- Behavior tests stay green through every slice, including all
  `TestScenario*MatchExpectedResults` rows: the reset changes speed, never
  results.
- One mechanism per job: when a general mechanism lands, the specific one
  it replaces is deleted in the same phase, not flagged off.
- No-regrowth budgets are tests: opcode count and `Proto` side-table count
  may only shrink or hold during this plan.
- Determinism is part of the interface: iteration order, number formatting,
  and error text stay documented and stable, or the doc changes in the same
  slice.

## Gate Policy

The ratio gate runs in two modes:

- Ledger mode (Phases 0-2): ratios are captured and recorded per slice, but
  regressions are expected and accepted while the specialized machinery
  leaves. Behavior tests and check scripts stay hard gates. Allocation
  budgets may loosen only in Phase 1 with an explicit ledger note per row.
- Hard mode (Phase 3 onward): the gate is reinstated at 4.0x for all 25
  rows at the end of Phase 3, tightens to 2.0x at the end of Phase 5, and
  allocation budgets re-tighten to landed floors as wins arrive.

## Phase 0: Gate Extension And Attribution

Goal: make all 25 rows first-class citizens of the ratio gate and the
allocation budgets, and pin the complexity ledger, so the reset and the
rebuild are both forced honest.

Slices:

1. `0.1 Extend the Scenario gate to all 25 rows`
   - Add the 8 general rows to `scripts/scenario-ratio-gate` and to
     `TestScenarioEmberRunAllocationBudgets` with budgets from the baseline.
   - Red tracer: the gate fails today at `SCENARIO_RATIO_MAX=4.0` for the
     general rows; that failing run is the tracer.

2. `0.2 Complexity budgets as tests`
   - Add `TestOpcodeCountBudget` (starts at 101) and
     `TestProtoSideTableBudget` (starts at the audited count); both budgets
     only ratchet down as phases land.
   - Red tracer: the budget tests themselves.

3. `0.3 Per-row profile attribution notes`
   - Capture per-row CPU profiles for `prototype_fallback`,
     `command_vararg_router`, `dirty_metatable_writes`, and
     `sparse_grid_neighbors`; record top flat functions here so later
     phases point at the exact cost they delete.

Checks:

```sh
go test -run '^$' -bench '^BenchmarkScenarioLuau/' -benchmem -count=3 . \
  | tee /tmp/ember-scenario-bench.txt
SCENARIO_RATIO_MAX=4.0 scripts/scenario-ratio-gate < /tmp/ember-scenario-bench.txt
scripts/check-fast
```

Risks:

- The general rows are noisier than the old rows (GC-heavy). Use count=3
  medians and avoid single-run conclusions.

## Phase 1: Reset To The General Core

Goal: delete the benchmark-shaped web while keeping every behavior test
green. This is the simplification the plan exists for; speed comes back in
Phases 2-5.

Design: deletion order goes outside-in so each slice compiles and passes
tests: region executors first, then the opcodes that fed them, then the
plan/fact tables, then the emitter lowerings and shape tests. Each slice
records the ratio delta and lines deleted in the ledger.

Slices:

1. `1.1 Delete region execution plans`
   - Remove all `regionExecutionPlans` machinery: the per-row executors
     (`executeArrayRowLoop*`, `executeExpiringEffectStackRegion`,
     `executeIndexedNodeDecisionWalkRegion`, projectile/quest/relaxation
     peers), their descriptors, planner passes, PC tables, and mechanism
     tests.
   - Red tracer: `rg 'regionExecutionPlan|executeArrayRowLoop'` finds no
     live code; behavior rows stay green.

2. `1.2 Delete verified plans, block plans, and path plans`
   - Remove `verifiedPlans`, `blockPlans`, `directBlockPlans`, `pathPlans`,
     `pathFacts`, predicate branches, refinements, reduction facts, and the
     per-pc probe arrays that feed the dispatch loop.
   - Red tracer: the dispatch loop contains no per-pc plan probes;
     `TestProtoSideTableBudget` ratchets down.

3. `1.3 Delete fused and row-shaped opcodes`
   - Remove the row-field opcode families, multi-field fusions, call
     fusions, per-proto fast-path flags, and their emitter lowerings,
     islands, and bytecode-shape tests.
   - Red tracer: `TestOpcodeCountBudget` ratchets down; `Compile` output
     for the old shape tests re-lowers to general opcodes with results
     unchanged.

4. `1.4 One general fast call for builtins`
   - Replace per-builtin intrinsic opcodes with one `opFastCall` carrying a
     builtin ID, argument window, and a guard on the global binding
     (general form of Luau's fastcall). Delete the per-builtin opcodes.
   - Red tracers: `TestFastCallCoversBaseLibraryBuiltins` and
     `TestFastCallFallsBackWhenGlobalIsShadowed`.

5. `1.5 Ledger and budget reconciliation`
   - Record the post-reset ratio table for all 25 rows, adjusted allocation
     budgets with per-row notes, final line counts, opcode count, and side
     table count. Tighten both complexity budget tests to the new floors.

Checks:

```sh
go test ./...
go test -run '^TestScenario|^TestTop10|^TestClassic' .
go test -run '^$' -bench '^BenchmarkScenarioLuau/' -benchmem -count=3 . \
  | tee /tmp/ember-reset-bench.txt
scripts/check-fast && scripts/check
```

Risks:

- The old 17 rows will regress, some past 2.0x; that is accepted and
  recorded, not hidden. The rebuild phases must win them back generally.
- Deleting in the wrong order breaks compilation mid-slice; keep the
  outside-in order and one cluster per slice.
- Some behavior coverage may exist only inside deleted mechanism tests;
  port any behavior assertions worth keeping to `Compile`/`Run` tests in
  the same slice.

Phase 1 reset ledger (2026-07-09):

- Opcode budget tightened from 101 to 78 after deleting the benchmark-shaped
  opcode families and replacing per-builtin intrinsic opcodes with
  `opFastCall`.
- `Proto` side-table budget tightened to 8:
  `numericForLoops`, `intrinsicOps`, `constantKindFacts`,
  `registerKindFacts`, `numericOperandFacts`, `numericOperandFactPCs`,
  `slotKindFacts`, and `entryNilRegisters`.
- Engine line ledger for `vm.go` + `bytecode.go` + `emitter.go`: 15,451
  lines, down from the 36,020-line baseline.
- Allocation budgets were loosened only where the reset removed specialized
  paths: Top10 `array_ops`; Scenario `combat_tick`, `buff_stack_tick`,
  `quest_progress_update`, `economy_market_tick`, `component_churn`,
  `state_machine_transitions`, and `array_hole_compaction`. These are
  reset baselines, not accepted final targets.
- Benchmark-shaped region/plan/fusion identifiers are absent from live
  compiler, bytecode, optimizer, VM, and bytecode-test code. The post-reset
  ratio table is intentionally deferred to the next explicit phase-boundary
  benchmark gate; slice-local iteration uses focused tests and check scripts.

Final gate sample (count=1, no profiles) after the reset still failed the
2.0x ratio target. Worst rows remained `prototype_fallback` (~13.1x),
`sparse_grid_neighbors` (~13.0x), `command_vararg_router` (~10.4x),
`dirty_metatable_writes` (~10.1x), and `event_dispatch` (~8.9x). Two
general follow-up optimizations landed after that sample:

- Function-valued `__index`/`__newindex` now use a fixed-arity one-result
  no-hook inline script-call path. Single-row samples improved
  `prototype_fallback` from ~348us to ~286us and
  `dirty_metatable_writes` from ~247us to ~229us.
- Repeated string-only concat chains now use a per-thread box-keyed concat
  cache. A single-row sample reduced `sparse_grid_neighbors` allocations
  from ~4587 allocs/op to ~407 allocs/op, but runtime stayed around 7ms/op,
  so its remaining pressure is table/loop execution rather than allocation.
- Table string-overflow state is now explicit in the table cold sidecar
  instead of being recomputed by scanning hash fields. A focused
  `sparse_grid_neighbors` sample improved from ~7.4ms/op to ~4.0ms/op with
  allocations unchanged (~407 allocs/op).
- Direct one-result local/upvalue/method calls now use the fixed-arity
  frame path for up to three arguments. Focused samples showed only a small
  noisy movement (`command_vararg_router` around ~223us/op), so further call
  work should be driven by fresh profiles rather than this seam alone.
- Direct-frame table get/set islands now handle function-valued
  `__index`/`__newindex` through the shared table-access module instead of
  side-exiting to the cold loop. Focused samples improved
  `prototype_fallback` to ~250us/op and `dirty_metatable_writes` to
  ~177us/op; `go test ./...`, `scripts/check-fast`, and `scripts/check`
  passed after the slice.
- Phase 2.1 production-loop instrumentation cleanup landed behind a generic
  trace seam: normal direct-frame execution uses a no-op trace and no longer
  checks opcode/PC counter pointers per instruction, while mechanism tests
  opt into the counting trace. `TestRunProductionLoopHasNoInstrumentationSideEffects`,
  `TestAssemblerRemovesJumpToNextInstruction`, Scenario/Top10 behavior
  tests, `go test ./...`, `scripts/check-fast`, and `scripts/check` passed.
  Focused samples were noisy but kept the current floors
  (`command_vararg_router` ~226us/op, `prototype_fallback` ~275us/op,
  `sparse_grid_neighbors` ~4.6ms/op).
- Phase 3.2 open-return transport got a small general win: prefix-plus-open
  returns now stay in an inline result window when they fit, instead of
  materializing a temporary slice. The slice also fixed a latent aliasing bug
  where retained open results could reuse borrowed vararg storage as scratch.
  `command_vararg_router` improved from ~6.8KB/76 allocs/op to
  ~2.0KB/16 allocs/op in a focused sample; runtime remained roughly flat
  around ~231us/op. `TestOpenReturnPrefixDoesNotAllocatePerCall` was added,
  the final-vararg expansion regression stayed covered, and `go test ./...`,
  `scripts/check-fast`, and `scripts/check` passed.
- Phase 4.4 table churn got a narrow storage-shape improvement: small
  array-capacity table literals now allocate a private storage object with
  inline array backing, while tables without small array parts keep the
  smaller normal storage shape. `TestLoopTableLiteralAllocationBudget`
  tightened from the reset-era allowance to one table allocation per loop
  iteration plus run-boundary allocations. Focused samples showed
  `array_hole_compaction` bytes/op down (~26.9KB to ~24.8KB) with other
  sampled rows holding their prior byte floors; `go test ./...`,
  `scripts/check-fast`, and `scripts/check` passed.

## Phase 2: Representation Density

Goal: shrink the bytes the interpreter touches per instruction so the one
switch loop runs materially faster for every program. Attacks the 9% loop
scaffolding, the 40-byte loads, and the GC share.

Slices:

1. `2.1 Remove per-instruction bookkeeping from the hot loop`
   - Move opcode/pc/PIC counters behind an instrumented runner selected
     once per thread (tests opt in), so the production loop carries zero
     instrumentation branches.
   - Delete the `opJump`-to-next-pc loop peephole; the assembler removes
     no-op jumps instead (jump threading lands fully in 6.2).
   - Red tracers: `TestRunProductionLoopHasNoInstrumentationSideEffects`
     and `TestAssemblerRemovesJumpToNextInstruction`.

2. `2.2 Packed executable instructions`
   - Encode executable instructions into a fixed 16-byte word pair with
     accessors (op 8 bits, a/b/c 16 bits, d 32 bits, spare reserved),
     replacing the 40-byte struct. Bytecode IR stays a readable struct for
     the compiler, optimizer, disassembler, and verifier; operand ranges
     are enforced in `finalizeProto` with clear verifier errors.
   - Red tracers: `TestInstructionSizeBudget` tightened from 40 to 16, and
     `TestPackedInstructionRoundTripsAllOpcodes` driven by the opcode
     metadata table.

3. `2.3 Value shrink to 24 bytes with boxed strings`
   - Move the string payload behind a private heap box holding the string
     and a cached hash; `Value` becomes kind byte + packed flags + float64
     + one pointer (24 bytes). Bool and native-function IDs fold into the
     scalar word.
   - Pre-box compile-time string constants in `Proto`; box runtime strings
     once at creation (concat results, `tostring`, host `StringValue`).
   - Add a small per-thread intern cache so hot runtime-built keys land on
     shared boxes; boxed strings compare pointer first, hash second, bytes
     last.
   - Red tracers: `TestValueSizeBudgetSafeLayout` tightened from 48 to 24,
     `TestValueRoundTripsAllKinds` staying green,
     `TestStringValuesCompareAndHashAcrossBoxingBoundaries`, and
     `TestValueConstructorsDoNotAllocateForScalars`.
   - Gate: geomean must improve; if boxing costs more than the copy savings
     on string-light rows, stop and re-evaluate before 2.4.

4. `2.4 Table representation compaction`
   - Shrink `Table` toward ~96-128 bytes: collapse the six version counters
     to the layout/value pairs caches actually consume, move the iteration
     journal, index-cache words, and id into a lazily allocated cold
     sidecar, and size inline fields against the 24-byte `Value`.
   - Replace `map[tableKey]Value` generic storage and the `map[string]Value`
     overflow with one compact open-addressing table keyed by (kind, bits,
     pointer) with cached hashes.
   - Red tracers: `TestTableHeaderSizeBudget`,
     `TestTableGenericKeyLookupDoesNotAllocate`, and existing raw iteration
     order tests staying green.

5. `2.5 Optional 16-byte NaN-boxed Value (unsafe seam)`
   - Only if post-2.3 profiles still show register/table copy pressure.
   - One file, total accessor coverage, safe layout kept building via build
     tag for differential testing. Reject if accessor knowledge leaks.
   - Red tracers: `TestValueUnsafeLayoutMatchesSafeSemantics` and
     `TestValueUnsafeLayoutSizeBudget`.

Checks:

```sh
go test -run 'Test(Value|Instruction|Packed|Table.*Budget|Assembler)' ./...
go test -run '^TestScenario|^TestTop10' .
go test -run '^$' -bench '^BenchmarkScenarioLuau/' -benchmem -count=3 . \
  | tee /tmp/ember-phase2-bench.txt
scripts/check-fast && scripts/check
```

Risks:

- String boxing is the highest-risk representation change; it must land
  with the intern cache and pointer-first comparison in the same slice or
  string-heavy rows regress.
- Packed instructions can silently truncate operands; the verifier and the
  metadata round-trip test are the guard.

## Phase 3: Call, Frame, And Value Transport ABI

Goal: make script calls, returns, varargs, closures, and metamethod
invocations allocation-free in the common case. Targets the worst general
rows (`command_vararg_router` 14.3x, `signal_bus_callbacks` 6.8x,
`prototype_fallback` 14.8x, `closures_upvalues` 2.7x).

Design: the VM owns one contiguous value stack per thread; frames become
windows (base + count) into it. The public ABI is unchanged: `Run` returns a
fresh `[]Value`, and public `HostFunc` still receives argument slices it may
keep.

Slices:

1. `3.1 Contiguous register stack with frame windows`
   - Registers become windows into one thread stack; fixed-arity calls
     place arguments at the caller's top so entering a callee copies
     nothing beyond nil-filling missing params. Frame metadata moves to a
     flat slice; the frame pool and free-frame scan disappear.
   - Captured locals keep eager cells exactly as today; stack growth is a
     value copy and never invalidates cells.
   - Red tracers: `TestScriptCallFixedArityDoesNotAllocatePerCall` and
     `TestDeepRecursionGrowsStackWithoutCorruption`; coroutine
     suspend/resume tests stay green.

2. `3.2 Returns and varargs through stack windows`
   - Multi-returns write into the caller-designated window with an explicit
     count; `vmValueList`, `openCallResults`, and `adjustedCallResults`
     leave the internal path. `...` becomes a window over caller-pushed
     extras; `select`, assignment adjustment, and final-call expansion read
     the window. Direct leaf calls and immediate-call closures are deleted
     here, replaced by the general ABI.
   - Red tracers: `TestMultiReturnAdjustmentDoesNotAllocatePerCall` and
     `TestVarargForwardingDoesNotCopyPerAccess`, plus nil-padded, short,
     long, and final-call expansion cases staying green.

3. `3.3 Metamethod and builtin ABI on the stack`
   - Arithmetic, comparison, `__index`, `__newindex`, `__call`, `__iter`,
     `__tostring`, and `__eq` invocations pass arguments in a scratch stack
     window; `opFastCall` builtins take borrowed windows. Only the public
     `HostFunc` boundary copies to owned slices.
   - Red tracers: `TestFunctionIndexMetamethodCallDoesNotAllocatePerHit`
     and `TestNewindexMetamethodWriteDoesNotAllocatePerHit`.

4. `3.4 Inline caches move from frames to code sites`
   - Per-pc string index caches and call-target caches live in proto-owned
     side arrays, warm across calls and runs; per-frame `indexCaches`
     (~264B per pc, cleared every call) are deleted.
   - Document in `docs/public-surface.md` that a `Proto` must not execute
     on two goroutines concurrently (already the de facto contract: tables
     carry mutable caches today).
   - Red tracers: `TestRepeatedCallsReuseWarmFieldCaches` and
     `TestFrameResetNoLongerScalesWithCodeLength`.

5. `3.5 Cheaper closures`
   - By-value capture when binder facts prove a local is never assigned
     after capture; cells only for mutable captures. Reuse a canonical
     closure for zero-capture prototypes where identity semantics allow,
     with the identity behavior test written first.
   - Red tracers: `TestImmutableCaptureAvoidsCellAllocation` and
     `TestZeroCaptureClosureIdentityIsPreserved`.

6. `3.6 Run entry cost`
   - Pool the thread and stack via `sync.Pool` inside `executeProto`; share
     one immutable base global env for `Run(proto)` with no host globals
     (today every run copies maps and re-caches base globals).
   - Red tracer: `TestRunMinimalScriptAllocationBudget` tightened to the
     measured post-slice floor.

Phase exit: reinstate the hard ratio gate at `SCENARIO_RATIO_MAX=4.0` for
all 25 rows.

Checks:

```sh
go test -run 'Test.*(Call|Vararg|Closure|Capture|Metamethod|StackWindow|Recursion)' ./...
go test -run '^TestScenario|^TestTop10' .
go test -run '^$' -bench '^BenchmarkScenarioLuau/' -benchmem -count=3 . \
  | SCENARIO_RATIO_MAX=4.0 scripts/scenario-ratio-gate
scripts/check-fast && scripts/check
```

Risks:

- The stack refactor touches most of `vm.go`; land strictly in slice order,
  keeping the old frame path compiling until 3.2 removes its last user.
- Borrowed windows must never escape; any callee that stores or returns its
  argument slice must copy. The public HostFunc copy is the explicit
  exception.
- Coroutines suspend whole stacks; suspension moves to (stack, frames)
  pairs and needs direct resume tests.

## Phase 4: Globals, Strings, And Metatable Access

Goal: make name-based access general-fast: global reads, string-keyed
tables, string building, and `__index`/`__newindex` fallbacks.

Slices:

1. `4.1 Resolved global slots`
   - The compiler assigns each referenced global a slot index per program;
     `globalEnv` holds a slot array plus a fallback map for dynamic names;
     `LOAD_GLOBAL`/`SET_GLOBAL` become version-guarded slot access.
     Host-provided globals wrap without a per-run map copy; writes keep
     mirroring into the host map per the documented contract.
   - Red tracers: `TestGlobalReadsDoNotAllocateOrRehashPerAccess` and
     `TestRunWithGlobalsDoesNotCopyHostMapPerRun`.

2. `4.2 String building and formatting`
   - Concat chains build in a per-thread scratch buffer with one final
     string allocation; whole-number formatting appends via `strconv`
     Append variants; a small static table serves interned boxes for small
     non-negative integers. `formatLuauNumber` output stays exact.
   - Red tracers: `TestConcatChainAllocatesOnceForRawOperands` and
     `TestTostringSmallIntegerDoesNotAllocate`.

3. `4.3 Metatable fallback fast path`
   - Cache the resolved `__index`/`__newindex` target (table or function)
     per receiver shape, guarded by the metatable's value version
     (generalizes `cachedIndexTable` to function values and `__newindex`);
     invoke function fallbacks through the 3.3 ABI. Cycle detection and
     error text stay identical.
   - Red tracers: `TestFunctionIndexFallbackResolvesOncePerShape` and
     `TestNewindexFallbackChainMatchesLuauOrder`.

4. `4.4 Table churn`
   - Array growth uses doubling with literal-shape capacity hints (attacks
     `growFastArray`); loop-allocated table literals cost one header plus
     sized parts (with 2.4's smaller header, attacks the 61%
     `newTableWithCapacity` share).
   - Red tracer: `TestLoopTableLiteralAllocationBudget`.

Checks:

```sh
go test -run 'Test.*(Global|Concat|Tostring|Fallback|Metatable|Literal)' ./...
go test -run '^$' -bench '^BenchmarkScenarioLuau/' -benchmem -count=3 . \
  | SCENARIO_RATIO_MAX=4.0 scripts/scenario-ratio-gate
scripts/check-fast && scripts/check
```

Risks:

- Global slots must keep the fallback map authoritative for names the
  compiler never saw.
- Shape-keyed metamethod caches must invalidate on `setmetatable` and on
  metatable mutation; version counters are the guard and get direct tests.

## Phase 5: One Dispatch Loop

Goal: finish the convergence: a single fast loop with local side exits, and
no direct/generic duality.

Slices:

1. `5.1 Local side exits everywhere`
   - Unsupported or rare instructions side-exit per instruction into a cold
     handler and resume the fast loop; whole-function demotion disappears.
   - Red tracers: `TestUnsupportedOpcodeSideExitsPerInstruction` and
     `TestFastLoopResumesAfterColdIsland`.

2. `5.2 Budget and hooks in the fast loop`
   - Account `maxInstructions` at block boundaries; debug hooks run through
     the instrumented runner from 2.1. Interrupt points stay within one
     block of today's behavior and get documented.
   - Red tracer: `TestInstructionBudgetInterruptsFastExecution`.

3. `5.3 Delete the generic loop`
   - With captured-local frames handled by cells and everything else by
     side exits, delete `runGenericFrame` and the `directRegisters`
     branching; one loop remains.
   - Red tracer: `rg 'runGenericFrame|directRegisters'` finds no live code;
     ledger records the deletion.

Phase exit: tighten the hard gate to `SCENARIO_RATIO_MAX=2.0` for all 25
rows.

Checks:

```sh
go test ./...
go test -run '^$' -bench '^Benchmark(ScenarioLuau|Top10Luau|ClassicLuau)/' -benchmem -count=3 . \
  | SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate
scripts/check-fast && scripts/check
```

Risks:

- The single loop must not regress budgeted or debug-hooked execution;
  those paths get their own behavior tests before the generic loop dies.

## Phase 6: Compiler Output Quality And Compile Cost

Goal: emit less work per program with general IR passes, and keep `Compile`
itself fast and simple while the optimizer grows.

Slices:

1. `6.1 Liveness-driven frame shrink`
   - Size frames from liveness instead of max register index; smaller
     windows mean cheaper calls and less stack clearing.
   - Red tracers: `TestCompilerShrinksFrameUsingLiveness` and
     `TestFrameShrinkPreservesCapturedAndVarargRegisters`.

2. `6.2 Jump threading and branch simplification`
   - Collapse jump-to-jump chains, remove jumps to fallthrough, fold
     constant conditions, and delete unreachable blocks in IR.
   - Red tracers: `TestOptimizerThreadsJumpChains` and
     `TestOptimizerRemovesConstantBranches`.

3. `6.3 General constant folding`
   - Fold constant arithmetic, constant concat, and known-length `rawlen`
     and `#` over literal shapes where semantics allow, reusing existing
     kill rules for metamethod hazards.
   - Red tracer:
     `TestCompilerFoldsConstantExpressionsWithoutChangingErrors`.

4. `6.4 Compile-cost guard`
   - Add a compile benchmark gate (`BenchmarkCompileArithmetic` baseline:
     46,020 ns/op, 480 allocs/op); convert optimizer passes that rescan
     whole code arrays into worklist passes as needed to hold the line.
   - Red tracer: compile time/alloc budget test with explicit numbers.

Checks:

```sh
go test -run 'Test(Compiler|Optimizer|Frame)' ./...
go test -run '^$' -bench 'BenchmarkCompile' -benchmem -count=3 .
scripts/check-fast && scripts/check
```

Risks:

- Folding must never move or suppress observable errors or metamethod
  calls; kill rules are part of the optimizer interface.
- Optimizer shape tests freeze private sequences easily; assert the
  mechanism, not the full listing.

## Milestones

- `M1` (Phase 1 done): the reset has landed. Opcode count at or under 50;
  `Proto` side tables at or under 8; `vm.go` plus `bytecode.go` plus
  `emitter.go` reduced by at least 40% from the ledger baseline; every
  behavior test green; post-reset ratios recorded without excuses.
- `M2` (Phase 3 done): all 25 rows at or under 4.0x with the simple core;
  `prototype_fallback` under 200 allocs/op and `command_vararg_router`
  under 60 allocs/op.
- `M3` (Phase 5 done): all 25 rows at or under 2.0x; one dispatch loop;
  `closures_upvalues` under 1.5x and `varargs_select` under 1.2x;
  `sparse_grid_neighbors` under 8,000 allocs/op.
- `M4` (stretch, Phase 6 done): geometric mean across all benchmarked rows
  at or under 1.5x against the Luau CLI batch numbers, with allocation
  budgets tightened to landed floors.

## Completion Criteria

The plan is complete when:

- the ratio gate covers all 25 rows and passes at 2.0x with count=3, with
  no benchmark-named mechanism anywhere in the engine;
- the complexity budget tests hold the M1 floors (opcode count, side
  tables) and the engine is materially smaller than at plan start;
- one dispatch loop remains; the direct/generic duality, region executors,
  and per-row fast paths are deleted, not flagged off;
- instruction, Value, and Table size budget tests are tightened to the
  landed layouts;
- `scripts/check-fast` and `scripts/check` pass; no CGo, no new
  dependencies; unsafe remains confined to the existing payload field and
  the optional slice 2.5 file if taken;
- `docs/compatibility.md` and `docs/public-surface.md` reflect any
  host-visible choice this plan made (proto concurrency note, formatting,
  iteration order);
- this file records accepted and rejected experiments with their numbers,
  then gets retired.
