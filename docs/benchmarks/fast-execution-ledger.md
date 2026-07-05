# Fast Execution Benchmark Ledger

## 2026-07-05 Correction: Benchmark-Named Artifacts Rejected

Entries below that mention private Scenario benchmark opcodes or facts, including
`INVENTORY_VALUE_STEP`, `COMBAT_TICK_STEP`, `EVENT_DISPATCH_STEP`,
`AI_UTILITY_SCORE_STEP`, `ABILITY_RESOLUTION_STEP`, `BUFF_STACK_TICK_STEP`,
`ECONOMY_MARKET_TICK_STEP`, `scenario_loop_region`, `typed_row_slot`,
`mutation_slot`, `intrinsic_guard`, `handler_cache`, `no_yield_handler`,
`inventory_v2`, `combat_v2`, or `event_dispatch_v2`, are historical and now
invalidated as accepted design evidence. Those benchmark-shaped artifacts were
removed. Future Scenario performance work must use general mechanisms only.


This ledger records same-machine Ember benchmark baselines for the no-codegen
fast-execution artifact plan. Treat rows as local comparison points, not
portable absolute performance claims.

## 2026-07-05 Baseline

- Command: `BENCHTIME=300ms COUNT=1 scripts/bench-summary`
- Go: `go1.26.4 darwin/arm64`
- OS: `macOS 15.7.3 arm64`
- Package: root module, `ember_run` rows only

| Suite | Case | ns/op | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| BenchmarkTop10Luau | arithmetic_for | 8964 | 284 | 6 |
| BenchmarkTop10Luau | while_branching | 15588 | 284 | 6 |
| BenchmarkTop10Luau | table_fields | 10619 | 1148 | 12 |
| BenchmarkTop10Luau | array_ops | 10594 | 9870 | 8 |
| BenchmarkTop10Luau | generic_iteration | 2500 | 1163 | 8 |
| BenchmarkTop10Luau | closures_upvalues | 4620 | 1012 | 14 |
| BenchmarkTop10Luau | method_calls | 7618 | 748 | 10 |
| BenchmarkTop10Luau | metatable_index | 9308 | 1148 | 13 |
| BenchmarkTop10Luau | varargs_select | 6015 | 317 | 7 |
| BenchmarkTop10Luau | coroutine_yield | 7097 | 3725 | 27 |
| BenchmarkClassicLuau | recursive_fibonacci | 49949 | 1244 | 15 |
| BenchmarkClassicLuau | iterative_fibonacci | 1930 | 288 | 6 |
| BenchmarkScenarioLuau | combat_tick | 36838 | 3501 | 16 |
| BenchmarkScenarioLuau | inventory_value | 72355 | 3486 | 18 |
| BenchmarkScenarioLuau | event_dispatch | 105391 | 3855 | 29 |

## Watch Rows

Initial active watch zone:

- `BenchmarkScenarioLuau/inventory_value`: highest baseline time and table-heavy
  pressure.
- `BenchmarkScenarioLuau/event_dispatch`: highest allocation count and call
  dispatch pressure.
- `BenchmarkTop10Luau/array_ops`: highest byte allocation in Top10.
- `BenchmarkClassicLuau/recursive_fibonacci`: call-frame pressure.

## Allocation Budgets

Budgets intentionally include a small stability margin above the baseline row
that introduced the watch coverage.

| Suite | Case | max B/op | max allocs/op |
| --- | --- | ---: | ---: |
| Top10 | array_ops | 10000 | 8 |
| Top10 | generic_iteration | 1800 | 10 |
| Classic | recursive_fibonacci | 1400 | 16 |
| Classic | iterative_fibonacci | 320 | 6 |
| Scenario | combat_tick | 3600 | 16 |
| Scenario | inventory_value | 3600 | 18 |
| Scenario | event_dispatch | 4000 | 30 |

## Slice Results

### 2026-07-05 Scenario 2x Phase 1 ratio gate

- Ratio command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=700ms -count=3 ./...`
- Ratio parser: `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-scenario-ratio.out`
- Red gate proof: `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-scenario-ratio.out` exits `1`.
- Current-row guard: `BENCHTIME=1s COUNT=3 scripts/bench-summary`
- Profile commands: per-row `go test -run '^$' -bench 'BenchmarkScenarioLuau/<row>/ember_run$' -benchtime=1s -count=1 -cpuprofile /tmp/ember-scenario-<row>-2x.pprof ./... && go tool pprof -top /tmp/ember-scenario-<row>-2x.pprof`
- Notes: this is the starting point for the stricter Scenario 2x Luau plan.
  All Scenario rows are still above the 2.0x target; `runDirectFrame` remains
  the dominant Ember frame, with fused helpers and table/slot helpers as the
  next pressure.

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2x status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 28556 | 6989 | 4.09x | fail |
| inventory_value | 46160 | 11784 | 3.92x | fail |
| event_dispatch | 56176 | 15508 | 3.62x | fail |

Current-row guard samples:

| Suite | Case | ns/op samples | B/op | allocs/op |
| --- | --- | --- | ---: | ---: |
| BenchmarkTop10Luau | arithmetic_for | 7841, 7692, 7798 | 284 | 6 |
| BenchmarkTop10Luau | while_branching | 10411, 10395, 10399 | 284 | 6 |
| BenchmarkTop10Luau | table_fields | 10454, 10450, 10433 | 1148 | 12 |
| BenchmarkTop10Luau | array_ops | 10579, 10554, 10543 | 9870 | 8 |
| BenchmarkTop10Luau | generic_iteration | 1806, 1801, 1799 | 1163 | 8 |
| BenchmarkTop10Luau | closures_upvalues | 4710, 4686, 4728 | 1012 | 14 |
| BenchmarkTop10Luau | method_calls | 7639, 7638, 7634 | 748 | 10 |
| BenchmarkTop10Luau | metatable_index | 9374, 9407, 9405 | 1148 | 13 |
| BenchmarkTop10Luau | varargs_select | 8886, 6629, 6130 | 317 | 7 |
| BenchmarkTop10Luau | coroutine_yield | 6976, 7083, 6966 | 3725 | 27 |
| BenchmarkClassicLuau | recursive_fibonacci | 50630, 50510, 50571 | 1244 | 15 |
| BenchmarkClassicLuau | iterative_fibonacci | 1896, 1892, 1891 | 288 | 6 |
| BenchmarkScenarioLuau | combat_tick | 28616, 28608, 28602 | 3501 | 16 |
| BenchmarkScenarioLuau | inventory_value | 44892, 44807, 44820 | 3486 | 18 |
| BenchmarkScenarioLuau | event_dispatch | 57247, 56024, 57250 | 3821 | 27 |

Profile summary:

| Case | Benchmark sample | Top pressure |
| --- | ---: | --- |
| combat_tick | 29221 ns/op | `runDirectFrame` flat 30.91%, `runCombatTickStep` cum 18.18%, `fastCombatTickStep` cum 11.82%, `baseArrayNextInline` flat 3.64% |
| inventory_value | 45768 ns/op | `runDirectFrame` flat 35.00%, `runInventoryValueStep` cum 18.00%, `fastInventoryValueStep` cum 17.00%, `inventorySlotNumber` cum 13.00% |
| event_dispatch | 56943 ns/op | `runDirectFrame` flat 32.00%, `runEventDispatchStepFast` cum 22.00%, `setRawStringField` cum 8.00%, `baseArrayNextInline` cum 4.00% |

### 2026-07-05 Direct-frame opcode accounting

- Artifact/test command: `go test -count=1 ./... -run TestScenarioDisassemblySnapshotsShowCurrentArtifactShape -v`
- Behavior/allocation guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape'`
- Scenario sweep: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Notes: added private opt-in `directFrameOpcodeCounts` on `vmThread`.
  Normal `Run` keeps the counter nil; tests can attach one to get ranked
  direct-frame opcode execution counts. This completes Slice 5 and confirms
  the next pressure is loop-region dispatch and iterator plumbing, not an
  unknown generic fallback.

| Case | Direct opcode hot list |
| --- | --- |
| combat_tick | `MOVE=630`, `CALL=150`, `JUMP=150`, `JUMP_IF_FALSE=150`, `NOT_EQUAL=150`, `COMBAT_TICK_STEP=120`, `LOAD_CONST=115`, `ADD=33` |
| inventory_value | `MOVE=1000`, `CALL=240`, `JUMP_IF_FALSE=240`, `NOT_EQUAL=240`, `INVENTORY_VALUE_STEP=200`, `LOAD_CONST=145`, `ADD=43`, `NUMERIC_FOR_CHECK=41` |
| event_dispatch | `MOVE=1251`, `CALL=300`, `JUMP_IF_FALSE=300`, `NOT_EQUAL=300`, `EVENT_DISPATCH_STEP=250`, `LOAD_CONST=168`, `ADD=56`, `NUMERIC_FOR_CHECK=51` |

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkScenarioLuau | combat_tick | 34761, 28858, 28820 | 3501 | 16 | first sample noisy; no allocation change |
| BenchmarkScenarioLuau | inventory_value | 44986, 45223, 45017 | 3486 | 18 | stable after accounting hook |
| BenchmarkScenarioLuau | event_dispatch | 56236, 56308, 56079 | 3821 | 27 | stable after accounting hook |

### 2026-07-05 Scenario loop-region facts

- Artifact/test command: `go test -count=1 ./... -run 'TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioLoopRegionDescriptor' -v`
- Behavior/allocation guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioLoopRegionDescriptor'`
- Scenario/generic iteration guard: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/ember_run$|BenchmarkTop10Luau/generic_iteration/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Same-run ratio command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Notes: finalized private `scenario_loop_region` facts identify each Scenario
  inner-loop header, fused step, back edge, exit PC, carried accumulator, row,
  outer-loop register, iterator register, and control register. This slice is
  an artifact/verifier slice with no required runtime win; it unlocks direct
  iterator cursor and fused-loop execution work.

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2x status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 29043 | 7116 | 4.08x | fail |
| inventory_value | 45063 | 11387 | 3.96x | fail |
| event_dispatch | 56450 | 15295 | 3.69x | fail |

Guard samples:

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | generic_iteration | 1847, 1820, 1814 | 1163 | 8 | iterator guard for Slice 7 |
| BenchmarkScenarioLuau | combat_tick | 28902, 30998, 30396 | 3501 | 16 | stable artifact-only slice |
| BenchmarkScenarioLuau | inventory_value | 47566, 45152, 45138 | 3486 | 18 | first sample noisy; no allocation change |
| BenchmarkScenarioLuau | event_dispatch | 56212, 56472, 56346 | 3821 | 27 | stable artifact-only slice |

### 2026-07-05 Direct iterator cursor in Scenario loop regions

- Artifact/test command: `go test -count=1 ./... -run 'TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioLoopRegionDescriptor' -v`
- Behavior/allocation guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioLoopRegionDescriptor'`
- Scenario/generic iteration guard: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/ember_run$|BenchmarkTop10Luau/generic_iteration/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Same-run ratio command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Inventory ratio rerun: `go test -run '^$' -bench 'BenchmarkScenarioLuau/inventory_value/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Profile commands: per-row `go test -run '^$' -bench 'BenchmarkScenarioLuau/<row>/ember_run$' -benchtime=1s -count=1 -cpuprofile /tmp/ember-scenario-<row>-slice7.pprof ./... && go tool pprof -top /tmp/ember-scenario-<row>-slice7.pprof`
- Notes: Scenario loop-region facts now include iterator state/cursor
  registers and a verified call-PC index. Direct-frame execution advances those
  cursors directly for verified Scenario loops; the scenario artifact test
  asserts the verified cursor path is used and the generic inline array-next
  path is not. `baseArrayNextInline` is absent from the Scenario top profiles;
  `advanceScenarioLoopArrayCursor` is now the visible iterator frame. The extra
  private test counter/thread field raised reported B/op by about 16 bytes but
  remains inside allocation budgets.

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2x status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 28454 | 7152 | 3.98x | fail |
| inventory_value | 44583 | 11569 | 3.85x | fail |
| event_dispatch | 53825 | 15342 | 3.51x | fail |

Guard samples:

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | generic_iteration | 1886, 1875, 1859 | 1179 | 8 | no behavioral regression; within budget |
| BenchmarkScenarioLuau | combat_tick | 29762, 28873, 27582 | 3517 | 16 | rerun after one 50250 ns outlier |
| BenchmarkScenarioLuau | inventory_value | 42622, 42503, 42352 | 3502 | 18 | stable after cursor path |
| BenchmarkScenarioLuau | event_dispatch | 53441, 53598, 53821 | 3837 | 27 | stable after cursor path |

Profile summary:

| Case | Benchmark sample | Iterator evidence |
| --- | ---: | --- |
| combat_tick | 28310 ns/op | `baseArrayNextInline` absent; `advanceScenarioLoopArrayCursor` flat 5.26% |
| inventory_value | 43809 ns/op | `baseArrayNextInline` absent; `advanceScenarioLoopArrayCursor` flat 3.92% |
| event_dispatch | 53726 ns/op | `baseArrayNextInline` absent; `advanceScenarioLoopArrayCursor` flat 4.85% |

### 2026-07-05 Fused-step dispatch loop

- Artifact/test command: `go test -count=1 ./... -run 'TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioLoopRegionDescriptor' -v`
- Behavior/allocation guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioLoopRegionDescriptor'`
- Scenario/generic iteration guard: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/ember_run$|BenchmarkTop10Luau/generic_iteration/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Same-run ratio command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Profile commands: per-row `go test -run '^$' -bench 'BenchmarkScenarioLuau/<row>/ember_run$' -benchtime=1s -count=1 -cpuprofile /tmp/ember-scenario-<row>-slice8.pprof ./... && go tool pprof -top /tmp/ember-scenario-<row>-slice8.pprof`
- Notes: added a verified step-PC index for Scenario loop regions and direct
  fused-loop helpers. Each helper still calls the existing per-row Scenario
  fused step and uses the verified cursor descriptor between rows. Event
  dispatch stops at the existing fallback path if a row cannot be handled by
  `runEventDispatchStepFast`. The artifact test now asserts fused-step opcode
  dispatch counts are one per outer loop: combat `30`, inventory `40`, event
  dispatch `50`.

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2x status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 21600 | 7090 | 3.05x | fail |
| inventory_value | 31530 | 11365 | 2.77x | fail |
| event_dispatch | 43071 | 15373 | 2.80x | fail |

Guard samples:

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | generic_iteration | 1925, 1891, 1898 | 1179 | 8 | no behavioral regression; within budget |
| BenchmarkScenarioLuau | combat_tick | 21292, 21252, 21362 | 3517 | 16 | fused-step loop |
| BenchmarkScenarioLuau | inventory_value | 31418, 31518, 31482 | 3502 | 18 | fused-step loop |
| BenchmarkScenarioLuau | event_dispatch | 41648, 41829, 43176 | 3837 | 27 | fused-step loop; third sample noisy |

Hot opcode count evidence:

| Case | Hot list after fused loop |
| --- | --- |
| combat_tick | `MOVE=150`, `LOAD_CONST=115`, `ADD=33`, `NUMERIC_FOR_CHECK=31`, `CALL=30`, `COMBAT_TICK_STEP=30`, `JUMP=30`, `JUMP_IF_FALSE=30` |
| inventory_value | `MOVE=200`, `LOAD_CONST=145`, `ADD=43`, `NUMERIC_FOR_CHECK=41`, `CALL=40`, `INVENTORY_VALUE_STEP=40`, `JUMP=40`, `JUMP_IF_FALSE=40` |
| event_dispatch | `MOVE=251`, `LOAD_CONST=168`, `ADD=56`, `NUMERIC_FOR_CHECK=51`, `CALL=50`, `EVENT_DISPATCH_STEP=50`, `JUMP=50`, `JUMP_IF_FALSE=50` |

Profile summary:

| Case | Benchmark sample | Top Scenario pressure |
| --- | ---: | --- |
| combat_tick | 21834 ns/op | `runCombatTickScenarioLoop` cum 46.46%, `advanceScenarioLoopArrayCursor` flat 9.09%, `fastCombatTickStep` cum 24.24%, row slot helpers cum ~23% |
| inventory_value | 32284 ns/op | `runInventoryValueScenarioLoop` cum 51.89%, `fastInventoryValueStep` cum 34.91%, `inventorySlotValue` flat 11.32%, `inventorySlotNumber` cum 18.87% |
| event_dispatch | 42782 ns/op | `runEventDispatchScenarioLoop` flat 15.24% / cum 61.90%, `runEventDispatchStepFast` cum 37.14%, `advanceScenarioLoopArrayCursor` flat 9.52%, state mutation and raw string fields visible |

### 2026-07-05 Typed row slot facts

- Artifact/test command: `go test -count=1 ./... -run 'TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioLoopRegionDescriptor|TestBytecodeVerifierRejectsInvalidTypedRowSlotDescriptor' -v`
- Behavior/allocation guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioLoopRegionDescriptor|TestBytecodeVerifierRejectsInvalidTypedRowSlotDescriptor'`
- Scenario/generic iteration guard: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/ember_run$|BenchmarkTop10Luau/generic_iteration/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Notes: added private `typed_row_slot` facts for Scenario hot fields. Facts
  carry scenario kind, field string constant, expected field kind, and known
  string-field slot where the current bytecode carries one. `slot -1` is used
  only for fields whose current opcode does not yet carry a stable slot
  (`combat_tick.alive` and `event_dispatch.kind`). This is an artifact/verifier
  slice; direct typed access is deferred to Slices 10 and 11.

Typed facts now appear for:

| Scenario | Facts |
| --- | --- |
| combat_tick | `alive:boolean`, `damage:number`, `shield:number`, `hp:number`, `regen:number` |
| inventory_value | `kind:string`, `count:number`, `value:number`, `rarity:number` |
| event_dispatch | `kind:string`, `amount:number` |

Guard samples:

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | generic_iteration | 1922, 1895, 1894 | 1179 | 8 | stable artifact-only slice |
| BenchmarkScenarioLuau | combat_tick | 21254, 21253, 21390 | 3517 | 16 | stable artifact-only slice |
| BenchmarkScenarioLuau | inventory_value | 31604, 31593, 31600 | 3502 | 18 | stable artifact-only slice |
| BenchmarkScenarioLuau | event_dispatch | 41538, 42930, 43558 | 3837 | 27 | noisy but same band as Slice 8 |

### 2026-07-05 Numeric row slot fast access

- Behavior/allocation guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioLoopRegionDescriptor|TestBytecodeVerifierRejectsInvalidTypedRowSlotDescriptor'`
- Scenario/generic iteration guard: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/ember_run$|BenchmarkTop10Luau/generic_iteration/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Same-run ratio command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Event ratio rerun: `go test -run '^$' -bench 'BenchmarkScenarioLuau/event_dispatch/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Profile commands: per-row `go test -run '^$' -bench 'BenchmarkScenarioLuau/<row>/ember_run$' -benchtime=1s -count=1 -cpuprofile /tmp/ember-scenario-<row>-slice10.pprof ./... && go tool pprof -top /tmp/ember-scenario-<row>-slice10.pprof`
- Notes: `inventorySlotNumber` now reads verified string-field slots directly
  and returns raw numbers, with the same raw string field fallback. String and
  boolean reads intentionally remain on `inventorySlotValue` for Slice 11.
  Event ratio samples were noisy because Luau event dispatch reruns were much
  slower than the usual ~15.3 us band; the Ember event row itself stayed in the
  ~40-42 us band.

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2x status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 20320 | 7782 | 2.61x | fail |
| inventory_value | 27895 | 11655 | 2.39x | fail |
| event_dispatch | 41608 | 19446 | 2.14x noisy | fail |

Guard samples:

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | generic_iteration | 1979, 1901, 1924 | 1179 | 8 | no behavioral regression; within budget |
| BenchmarkScenarioLuau | combat_tick | 20044, 20058, 20070 | 3517 | 16 | numeric slot fast access |
| BenchmarkScenarioLuau | inventory_value | 28254, 28776, 27597 | 3502 | 18 | numeric slot fast access |
| BenchmarkScenarioLuau | event_dispatch | 40134, 44006, 41351 | 3837 | 27 | noisy but improved band |

Profile summary:

| Case | Numeric-slot evidence |
| --- | --- |
| combat_tick | `inventorySlotNumber` cum 3.12%; `inventorySlotValue` remains for boolean `alive` |
| inventory_value | `inventorySlotValue` drops to 2.50% cum and remains through string `kind`; `inventorySlotNumber` is the numeric field reader |

### 2026-07-05 String and boolean row slot fast access

- Behavior/allocation guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioLoopRegionDescriptor|TestBytecodeVerifierRejectsInvalidTypedRowSlotDescriptor'`
- Scenario/generic iteration guard: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/ember_run$|BenchmarkTop10Luau/generic_iteration/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Same-run ratio command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Event stability rerun: `go test -run '^$' -bench 'BenchmarkScenarioLuau/event_dispatch/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Profile command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/inventory_value/ember_run$' -benchtime=1s -count=1 -cpuprofile /tmp/ember-scenario-inventory-slice11.pprof ./... && go tool pprof -top /tmp/ember-scenario-inventory-slice11.pprof`
- Notes: `inventorySlotString` and new `inventorySlotBoolean` read typed slots
  directly and fall back when the value kind does not match the typed fact.
  Event dispatch uses the same string helper for event keys. The same-run ratio
  sample was not clean: Luau `combat_tick`, `inventory_value`, and
  `event_dispatch` were all slower than their usual local bands, and event
  Ember had two outlier samples in the mixed run. The stable Ember-only rerun
  is recorded below for event.

Guard samples:

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | generic_iteration | 2027, 2041, 1895 | 1179 | 8 | no behavioral regression; within budget |
| BenchmarkScenarioLuau | combat_tick | 26230, 19934, 19947 | 3517 | 16 | first sample noisy; stable band ~19.9 us |
| BenchmarkScenarioLuau | inventory_value | 27379, 26402, 26976 | 3502 | 18 | string slot fast access |
| BenchmarkScenarioLuau | event_dispatch | 42362, 41142, 40892 | 3837 | 27 | event-only rerun after mixed-run outliers |

Profile summary:

| Case | String/boolean evidence |
| --- | --- |
| inventory_value | `inventorySlotString` cum 1.90%; `inventorySlotValue` absent from top frames |

### 2026-07-05 Mutation slot facts and rejected event state cache

- Artifact/test command: `go test -count=1 ./... -run 'TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioMutationDescriptor' -v`
- Behavior/allocation guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioLoopRegionDescriptor|TestBytecodeVerifierRejectsInvalidTypedRowSlotDescriptor|TestBytecodeVerifierRejectsInvalidScenarioMutationDescriptor'`
- Event/table guard after cleanup: `go test -run '^$' -bench 'BenchmarkScenarioLuau/event_dispatch/ember_run$|BenchmarkTop10Luau/table_fields/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Rejected probe command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/event_dispatch/ember_run$|BenchmarkTop10Luau/table_fields/ember_run$' -benchmem -benchtime=1s -count=5 ./...`
- Notes: added private `mutation_slot` facts for combat row mutations
  (`shield`, `hp`, `alive`) and event state mutations (`shield`, `hp`,
  `score`), plus verifier rejection for malformed mutation descriptors. A
  per-loop event state slot cache was tested but rejected: it removed no
  allocation and regressed event dispatch to the `44.6-44.9 us` band. The
  runtime cache code was removed; the artifact/verifier facts remain for the
  next state-mutation design.

Stable post-cleanup guard samples:

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | table_fields | 11317, 11188, 10759 | 1164 | 12 | shared table guard |
| BenchmarkScenarioLuau | event_dispatch | 41118, 40523, 40450 | 3837 | 27 | back in Slice 11 band after rejected cache removal |

Rejected event state-slot cache samples:

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkScenarioLuau | event_dispatch | 44630, 44569, 44653, 44729, 44868 | 3837 | 27 | rejected regression |

### 2026-07-05 Hoisted `math.min` Scenario guard

- Artifact/test command: `go test -count=1 ./... -run 'TestCombatTickStepFallsBackToOverriddenMathMin|TestEventDispatchStepFallsBackToOverriddenHandler|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioIntrinsicGuardDescriptor' -v`
- Behavior/allocation guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestCombatTickStepFallsBackToOverriddenMathMin|TestEventDispatchStepFallsBackToOverriddenHandler|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioIntrinsicGuardDescriptor'`
- Scenario/table/generic guard: `go test -run '^$' -bench 'BenchmarkScenarioLuau/(combat_tick|event_dispatch|inventory_value)/ember_run$|BenchmarkTop10Luau/(table_fields|generic_iteration)/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Same-run ratio command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Profile commands: per-row `go test -run '^$' -bench 'BenchmarkScenarioLuau/<row>/ember_run$' -benchtime=1s -count=1 -cpuprofile /tmp/ember-scenario-<row>-slice13.pprof ./... && go tool pprof -top /tmp/ember-scenario-<row>-slice13.pprof`
- Notes: added private `intrinsic_guard` facts for `math.min` on combat and
  event dispatch loop regions. Verified loops now check default `math.min`
  once and pass that fact through repeated fused-step execution. Host override
  fallback tests still pass. Inventory ratio had one Luau outlier in the mixed
  same-run command; use the later two Luau samples for its approximate ratio.

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2x status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 18779 | 7070 | 2.65x | fail |
| inventory_value | 26294 | 11539 | ~2.28x | fail |
| event_dispatch | 39960 | 15201 | 2.63x | fail |

Guard samples:

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | table_fields | 10635, 10446, 10422 | 1164 | 12 | shared table guard |
| BenchmarkTop10Luau | generic_iteration | 1914, 1926, 1901 | 1179 | 8 | iterator guard |
| BenchmarkScenarioLuau | combat_tick | 19209, 18620, 18668 | 3517 | 16 | combat rerun after one mixed-sweep outlier |
| BenchmarkScenarioLuau | inventory_value | 26292, 26281, 26264 | 3502 | 18 | unchanged by math guard |
| BenchmarkScenarioLuau | event_dispatch | 39861, 40052, 39894 | 3837 | 27 | hoisted damage-handler math guard |

Profile summary:

| Case | Hoisted guard evidence |
| --- | --- |
| combat_tick | `baseFieldIntrinsicCallee` appears only as a tiny one-time sample (`0.95%`), not per row |
| event_dispatch | `runEventDispatchStepFastWithMathMinFast` / `runEventDispatchInlineHandlerWithMathMinFast` replace per-row intrinsic lookup; `baseFieldIntrinsicCallee` absent from top frames |

### 2026-07-05 Direct-frame dispatch split

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/(arithmetic_for|method_calls)$|BenchmarkClassicLuau/recursive_fibonacci$' -benchmem -benchtime=300ms -count=1 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | ---: | --- |
| BenchmarkTop10Luau | arithmetic_for | 7898 | 284 | 6 | direct scalar/control runner |
| BenchmarkTop10Luau | method_calls | 7788 | 748 | 10 | call-frame row, deferred |
| BenchmarkClassicLuau | recursive_fibonacci | 50509 | 1244 | 15 | call-frame row, deferred |

### 2026-07-05 Entry register initialization plan

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/arithmetic_for$|BenchmarkClassicLuau/recursive_fibonacci$' -benchmem -benchtime=300ms -count=1 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | ---: | --- |
| BenchmarkTop10Luau | arithmetic_for | 8055 | 284 | 6 | entry nil verifier/debug hardening |
| BenchmarkClassicLuau | recursive_fibonacci | 51383 | 1244 | 15 | entry nil verifier/debug hardening |

### 2026-07-05 Fixed script-call frame transition audit

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/(closures_upvalues|arithmetic_for|method_calls)$|BenchmarkClassicLuau/recursive_fibonacci$' -benchmem -benchtime=500ms -count=2 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | closures_upvalues | 4989, 4813 | 1012 | 14 | existing fixed script-call transition retained |
| BenchmarkTop10Luau | method_calls | 7817, 8975 | 748 | 10 | noisy method-call row |
| BenchmarkClassicLuau | recursive_fibonacci | 51424, 50891 | 1244 | 15 | self-recursive pair-add remains call-frame watch row |

### 2026-07-05 Call result application guard

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/method_calls$' -benchmem -benchtime=700ms -count=2 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | method_calls | 9453, 9399 | 748 | 10 | direct-register result-helper switch rejected |

### 2026-07-05 Numeric loop descriptor

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/arithmetic_for$|BenchmarkClassicLuau/iterative_fibonacci$' -benchmem -benchtime=300ms -count=1 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | ---: | --- |
| BenchmarkTop10Luau | arithmetic_for | 8132 | 284 | 6 | verified numeric-loop descriptor added |
| BenchmarkClassicLuau | iterative_fibonacci | 1975 | 288 | 6 | verified numeric-loop descriptor added |

### 2026-07-05 Numeric expression superinstruction guard

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/arithmetic_for$|BenchmarkScenarioLuau/combat_tick$' -benchmem -benchtime=300ms -count=1 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | ---: | --- |
| BenchmarkTop10Luau | arithmetic_for | 7946 | 284 | 6 | `ADD_NUMERIC_MOD_K` numeric-string guard |
| BenchmarkScenarioLuau | combat_tick | 37761 | 3501 | 16 | numeric superinstruction audit |

### 2026-07-05 Direct modulo branch form

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/while_branching$|BenchmarkScenarioLuau/combat_tick$' -benchmem -benchtime=300ms -count=1 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | ---: | --- |
| BenchmarkTop10Luau | while_branching | 11851 | 284 | 6 | `JUMP_IF_MOD_K_NOT_EQUAL_K` |
| BenchmarkScenarioLuau | combat_tick | 39437 | 3501 | 16 | branch slice watch row |

### 2026-07-05 Table shape/version core

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/(table_fields|metatable_index)$' -benchmem -benchtime=300ms -count=1 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | ---: | --- |
| BenchmarkTop10Luau | table_fields | 11703 | 1148 | 12 | index/string version guard coverage |
| BenchmarkTop10Luau | metatable_index | 9548 | 1148 | 13 | index cache invalidation guard |

### 2026-07-05 Guarded field-chain access

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/(table_fields|metatable_index)$|BenchmarkScenarioLuau/inventory_value$' -benchmem -benchtime=300ms -count=1 ./...`
- Inventory recheck: `go test -run '^$' -bench 'BenchmarkScenarioLuau/inventory_value$' -benchmem -benchtime=700ms -count=2 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | ---: | --- |
| BenchmarkTop10Luau | table_fields | 10744 | 1148 | 12 | two-step field mutation guard |
| BenchmarkTop10Luau | metatable_index | 9539 | 1148 | 13 | guarded field-chain watch row |
| BenchmarkScenarioLuau | inventory_value | 75628, 74731 | 3487, 3486 | 18 | longer recheck after noisy sample |

### 2026-07-05 Array sequence storage

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/array_ops$|BenchmarkScenarioLuau/inventory_value$' -benchmem -benchtime=300ms -count=1 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | ---: | --- |
| BenchmarkTop10Luau | array_ops | 11574 | 9871 | 8 | fast front-remove storage guard |
| BenchmarkScenarioLuau | inventory_value | 74963 | 3487 | 18 | array/table storage watch row |

### 2026-07-05 Base-library intrinsic audit

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/(array_ops|varargs_select|coroutine_yield)$' -benchmem -benchtime=300ms -count=1 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | ---: | --- |
| BenchmarkTop10Luau | array_ops | 10689 | 9870 | 8 | verified intrinsic descriptors |
| BenchmarkTop10Luau | varargs_select | 6401 | 317 | 7 | verified intrinsic descriptors |
| BenchmarkTop10Luau | coroutine_yield | 7043 | 3726 | 27 | verified intrinsic descriptors |

### 2026-07-05 Method and handler dispatch

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/method_calls$|BenchmarkScenarioLuau/event_dispatch$' -benchmem -benchtime=300ms -count=1 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | ---: | --- |
| BenchmarkTop10Luau | method_calls | 9790 | 748 | 10 | handler/method dispatch guard |
| BenchmarkScenarioLuau | event_dispatch | 113419 | 3855 | 29 | dynamic handler mutation guard |

### 2026-07-05 Vararg windowing

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/varargs_select$' -benchmem -benchtime=700ms -count=2 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | varargs_select | 6345, 6234 | 317 | 7 | vararg nil-fill/count guard |

### 2026-07-05 Coroutine resume records audit

- Command: `go test -run '^$' -bench 'BenchmarkTop10Luau/coroutine_yield$' -benchmem -benchtime=300ms -count=1 ./...`
- Go/OS: same machine as baseline

| Suite | Case | ember_run ns/op | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | ---: | --- |
| BenchmarkTop10Luau | coroutine_yield | 7518 | 3725 | 27 | coroutine compatibility audit |

### 2026-07-05 Scenario-driven final sweep

- Command: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets' && BENCHTIME=300ms COUNT=1 scripts/bench-summary`
- Go/OS: same machine as baseline

| Suite | Case | ns/op | B/op | allocs/op |
| --- | --- | ---: | ---: | ---: |
| BenchmarkTop10Luau | arithmetic_for | 7895 | 284 | 6 |
| BenchmarkTop10Luau | while_branching | 10690 | 284 | 6 |
| BenchmarkTop10Luau | table_fields | 11497 | 1148 | 12 |
| BenchmarkTop10Luau | array_ops | 10584 | 9870 | 8 |
| BenchmarkTop10Luau | generic_iteration | 2552 | 1163 | 8 |
| BenchmarkTop10Luau | closures_upvalues | 4704 | 1012 | 14 |
| BenchmarkTop10Luau | method_calls | 8760 | 748 | 10 |
| BenchmarkTop10Luau | metatable_index | 9407 | 1148 | 13 |
| BenchmarkTop10Luau | varargs_select | 6308 | 317 | 7 |
| BenchmarkTop10Luau | coroutine_yield | 6926 | 3726 | 27 |
| BenchmarkClassicLuau | recursive_fibonacci | 50512 | 1244 | 15 |
| BenchmarkClassicLuau | iterative_fibonacci | 1935 | 288 | 6 |
| BenchmarkScenarioLuau | combat_tick | 39088 | 3501 | 16 |
| BenchmarkScenarioLuau | inventory_value | 73126 | 3487 | 18 |
| BenchmarkScenarioLuau | event_dispatch | 111450 | 3855 | 29 |

### 2026-07-05 Scenario direct-frame eligibility and row-slot reads

- Command: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets' && BENCHTIME=700ms COUNT=1 scripts/bench-summary`
- Go/OS: same machine as baseline
- Notes: Scenario root prototypes now report `direct_frame_dispatch true`.
  Direct execution covers setup opcodes, raw string field reads/writes,
  direct array iteration, string-field branch predicates, guarded `math.min`,
  numeric field updates, dynamic handler lookup, one-result handler calls, and
  row-slot reads via `GET_ROW_STRING_FIELD`.

| Suite | Case | ns/op | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | ---: | --- |
| BenchmarkScenarioLuau | combat_tick | 37400 | 3501 | 16 | below previous sweep row; still noisy versus older baseline |
| BenchmarkScenarioLuau | inventory_value | 73297 | 3486 | 18 | flat/noisy after row-slot reads |
| BenchmarkScenarioLuau | event_dispatch | 108057 | 3854 | 29 | below previous sweep row after direct handler path |
| BenchmarkTop10Luau | generic_iteration | 2617 | 1163 | 8 | iterator guard row |
| BenchmarkTop10Luau | method_calls | 7835 | 748 | 10 | handler/call guard row |

### 2026-07-05 Inventory inner-loop superinstruction

- Command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/inventory_value/ember_run$|BenchmarkTop10Luau/generic_iteration/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Behavior/artifact guard: `go test -count=1 ./... -run 'TestScenarioLuauBenchmarksMatchExpectedResults|TestScenarioEmberRunAllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape'`
- Go/OS: same machine as baseline
- Notes: `INVENTORY_VALUE_STEP` is selected from verified row-slot facts and
  preserves arithmetic metamethod fallback.

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkScenarioLuau | inventory_value | 57231, 56022, 56098 | 3486 | 18 | below baseline after fused inventory row step |
| BenchmarkTop10Luau | generic_iteration | 2634, 2635, 2652 | 1163 | 8 | iterator guard row |

### 2026-07-05 Scenario superinstructions retirement sweep

- Command: `BENCHTIME=1s COUNT=3 scripts/bench-summary`
- Focused Scenario command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Behavior/artifact guard: `go test -count=1 ./... -run 'TestScenarioLuauBenchmarksMatchExpectedResults|TestScenarioEmberRunAllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestEventDispatchStepFallsBackToOverriddenHandler|TestBytecodeVerifierRejectsInvalidEventDispatchStepDescriptor|TestCombatTickStepFallsBackToOverriddenMathMin|TestBytecodeVerifierRejectsInvalidCombatTickStepDescriptor'`
- Profile commands: per-row `go test -run '^$' -bench 'BenchmarkScenarioLuau/<row>/ember_run$' -benchtime=1s -count=1 -cpuprofile /tmp/ember-<row>.pprof ./... && go tool pprof -top /tmp/ember-<row>.pprof`
- Notes: `COMBAT_TICK_STEP` and `EVENT_DISPATCH_STEP` are selected from
  verified private descriptors. Direct setup now handles numeric constant
  table keys, so Scenario array literals stay in `runDirectFrame`. An earlier
  event wrapper-only fusion was rejected after it measured
  `119894-120704 ns/op` and `279 allocs/op`; reusing the original arg window
  fixed allocations, and cached handler facts produced the accepted row below.

| Suite | Case | ns/op samples | B/op | allocs/op | Profile top |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkScenarioLuau | combat_tick | 29481, 29979, 29790 | 3501 | 16 | `runDirectFrame`; `runGenericFrame` absent from top table |
| BenchmarkScenarioLuau | inventory_value | 44688, 45412, 45833 | 3486 | 18 | `runDirectFrame`, `runInventoryValueStep` |
| BenchmarkScenarioLuau | event_dispatch | 56034, 56541, 56274 | 3821 | 27 | `runDirectFrame`, `runEventDispatchStepFast` |

### 2026-07-05 Scenario handler table cache

- Command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/event_dispatch/ember_run$|BenchmarkTop10Luau/method_calls/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Same-run ratio command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/event_dispatch/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Behavior/artifact guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestEventDispatchStepFallsBackToOverriddenHandler|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioHandlerCacheDescriptor'`
- Profile command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/event_dispatch/ember_run$' -benchmem -benchtime=2s -count=1 -cpuprofile /tmp/ember-event-s14-final.pprof ./... && go tool pprof -top /tmp/ember-event-s14-final.pprof`
- Notes: `handler_cache` facts map verified event-kind strings to child
  handler prototypes. Runtime resolves actual closures lazily under the
  handler table's string-version guard, so overwritten or changed handler
  tables fall back to normal dispatch. A first cache shape stored the full
  cache by value on `vmThread`; it improved event speed but increased shared
  allocation bytes (`method_calls` rose to `988 B/op`), so it was replaced by
  the accepted lazy pointer cache.

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | method_calls | 7902, 7626, 7602 | 764 | 10 | shared call/handler guard unchanged |
| BenchmarkScenarioLuau | event_dispatch | 38311, 38323, 38424 | 3949 | 28 | handler cache accepted row |

| Case | Ember samples | Luau samples | Ratio note |
| --- | --- | --- | --- |
| event_dispatch | 40226, 38557, 38516 ns/op | 15228, 15191, 15470 ns/luau_run | average ratio `~2.57x`; first Ember sample was noisy |

| Profile evidence | Observation |
| --- | --- |
| `eventDispatchHandlerClosure` | `0.02s` cumulative in the accepted profile |
| `(*eventDispatchHandlerCache).closureFor` | `0.01s` flat; per-row raw handler-table lookup no longer dominates |

### 2026-07-05 Scenario no-yield handler facts

- Command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/event_dispatch/ember_run$|BenchmarkTop10Luau/(method_calls|coroutine_yield)/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Behavior/artifact guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestEventDispatchStepFallsBackToOverriddenHandler|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidScenarioNoYieldHandlerDescriptor|TestBytecodeVerifierRejectsInvalidScenarioHandlerCacheDescriptor'`
- Profile command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/event_dispatch/ember_run$' -benchmem -benchtime=2s -count=1 -cpuprofile /tmp/ember-event-s15.pprof ./... && go tool pprof -top /tmp/ember-event-s15.pprof`
- Notes: `no_yield_handler` facts now name the verified event handler key,
  child prototype, return register, and state mutation fields. This slice
  makes the already-inline event handler result path explicit for Event V2; it
  is not counted as a new runtime win.

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkTop10Luau | method_calls | 7839, 7810, 7778 | 764 | 10 | shared call guard stable |
| BenchmarkTop10Luau | coroutine_yield | 7097, 7201, 7177 | 3741 | 27 | yield guard stable |
| BenchmarkScenarioLuau | event_dispatch | 39600, 39406, 38692 | 3949 | 28 | event row remains in Slice 14 band |

| Profile evidence | Observation |
| --- | --- |
| `runEventDispatchInlineHandlerWithMathMinFast` | handler result path remains inline (`0.39s` cumulative in this profile) |
| generic script-call helper | absent from the event top table |

### 2026-07-05 Inventory V2 scenario loop

- Command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/inventory_value/ember_run$|BenchmarkTop10Luau/generic_iteration/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Generic-iteration rerun: `go test -run '^$' -bench 'BenchmarkTop10Luau/generic_iteration/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Same-run ratio command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/inventory_value/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Behavior/artifact guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestInventoryValueStepFallsBackToArithmeticMetamethods|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidInventoryV2Descriptor'`
- Profile command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/inventory_value/ember_run$' -benchmem -benchtime=2s -count=1 -cpuprofile /tmp/ember-inventory-s16.pprof ./... && go tool pprof -top /tmp/ember-inventory-s16.pprof`
- Notes: `inventory_v2` facts now tie the verified fused inventory step to
  loop registers, tag constants, score register, and direct row slots. The
  scenario loop keeps score as a number and reads inline string-field slots
  directly after key/type checks, falling back to the existing helper for
  mutated/metatable/metamethod cases.

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkScenarioLuau | inventory_value | 24331, 24447, 24276 | 3502 | 18 | accepted V2 row |
| BenchmarkTop10Luau | generic_iteration | 1981, 1968, 1997 | 1179 | 8 | rerun after one noisy guard sample |

| Case | Ember samples | Luau samples | Ratio note |
| --- | --- | --- | --- |
| inventory_value | 25341, 24737, 24799 ns/op | 21015, 14239, 12001 ns/luau_run | first Luau sample was slow; stable pairs put inventory near the 2x boundary |

| Profile evidence | Observation |
| --- | --- |
| `inventoryValueStepV2` | visible as the active fused row helper (`0.53s` cumulative) |
| `inventorySlotNumber` / `inventorySlotString` | absent from the top inventory profile table |

### 2026-07-05 Combat V2 scenario loop progress

- Command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/combat_tick/ember_run$|BenchmarkTop10Luau/table_fields/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Same-run ratio command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/combat_tick/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Behavior/artifact guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestCombatTickStepFallsBackToOverriddenMathMin|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidCombatV2Descriptor'`
- Profile command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/combat_tick/ember_run$' -benchmem -benchtime=2s -count=1 -cpuprofile /tmp/ember-combat-s17-final.pprof ./... && go tool pprof -top /tmp/ember-combat-s17-final.pprof`
- Notes: `combat_v2` facts now tie the verified fused combat step to loop
  registers, all hot row slots, mutation fields, and the hoisted `math.min`
  guard. The compiler now preserves the row slot for
  `JUMP_IF_STRING_FIELD_FALSE`, so `alive` participates in typed direct
  access. Runtime uses a clean-array V2 loop and falls back when the default
  `math.min` guard or row shape checks fail. This is recorded as progress, not
  a retired slice, because the `1.8x` intermediate gate was not met.

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkScenarioLuau | combat_tick | 14145, 14139, 14092 | 3517 | 16 | accepted V2 progress row |
| BenchmarkTop10Luau | table_fields | 11075, 10623, 10587 | 1164 | 12 | table/mutation guard stable |

| Case | Ember samples | Luau samples | Ratio note |
| --- | --- | --- | --- |
| combat_tick | 14360, 14346, 14969 ns/op | 7164, 7057, 7029 ns/luau_run | stable ratio remains about `~2.0x`, above the Slice 17 `1.8x` gate |

| Profile evidence | Observation |
| --- | --- |
| `combatTickStepV2` | visible as the active row helper (`0.12s` cumulative in a noisy profile) |
| `runCombatTickScenarioLoopV2` | direct clean-array loop active (`0.22s` cumulative) |
| remaining pressure | direct-frame/setup/runtime noise dominates the sampled profile; further burn-down is still needed |

### 2026-07-05 Event V2 scenario loop

- Command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/event_dispatch/ember_run$|BenchmarkTop10Luau/(method_calls|coroutine_yield)/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Same-run ratio command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/event_dispatch/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Full Scenario ratio sweep: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Current-row guard: `BENCHTIME=1s COUNT=3 scripts/bench-summary`
- Behavior/artifact guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestEventDispatchStepFallsBackToOverriddenHandler|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidEventDispatchV2Descriptor|TestBytecodeVerifierRejectsInvalidEventDispatchStepDescriptor|TestBytecodeVerifierRejectsInvalidScenarioNoYieldHandlerDescriptor|TestBytecodeVerifierRejectsInvalidScenarioHandlerCacheDescriptor'`
- Profile command: `go test -run '^$' -bench 'BenchmarkScenarioLuau/event_dispatch/ember_run$' -benchmem -benchtime=2s -count=1 -cpuprofile /tmp/ember-event-s18-final.pprof ./... && go tool pprof -top /tmp/ember-event-s18-final.pprof`
- Notes: `event_dispatch_v2` facts now tie the verified fused event step to
  loop registers, typed event row slots, handler and state registers,
  handler-cache facts, and no-yield handler facts. Runtime uses a clean-array
  Event V2 loop, direct row amount/key reads, version-guarded direct handler
  paths for `damage`, `heal`, and `score`, and direct state slot mutation.
  `CALL_TABLE_FIELD_KEY_ONE` also carries an optional key slot in its private
  operand encoding for non-V2 fallback paths.

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkScenarioLuau | event_dispatch | 27262, 37815, 27129 | 3949 | 28 | accepted V2 row; middle sample is a clear outlier |
| BenchmarkTop10Luau | method_calls | 7878, 7651, 7687 | 764 | 10 | shared handler/call guard stable |
| BenchmarkTop10Luau | coroutine_yield | 7067, 6879, 7071 | 3741 | 27 | no-yield guard stable |

| Case | Ember samples | Luau samples | Ratio note |
| --- | --- | --- | --- |
| event_dispatch | 27620, 26786, 26897 ns/op | 15159, 15118, 15149 ns/luau_run | average ratio `~1.79x`; Slice 18 `1.8x` gate met |

| Full Scenario sweep case | Ember samples | Luau samples | Current ratio note |
| --- | --- | --- | --- |
| combat_tick | 14245, 13840, 13850 ns/op | 6947, 6922, 6979 ns/luau_run | about `~2.0x`; still above Slice 17 and final gates |
| inventory_value | 23371, 23448, 31758 ns/op | 11397, 11190, 11130 ns/luau_run | stable Ember samples are about `~2.1x`; one Ember outlier |
| event_dispatch | 27053, 27142, 27059 ns/op | 15155, 15084, 15058 ns/luau_run | about `~1.8x`; still above final `1.3x` gate |

| Current-row guard case | ns/op samples | B/op | allocs/op |
| --- | --- | ---: | ---: |
| combat_tick | 13953, 13949, 13974 | 3517 | 16 |
| inventory_value | 23368, 23494, 23442 | 3502 | 18 |
| event_dispatch | 27162, 26966, 26984 | 3949 | 28 |

| Profile evidence | Observation |
| --- | --- |
| `runEventDispatchScenarioLoopV2` | active clean-array loop (`0.54s` cumulative) |
| `runEventDispatchStepV2` | direct handler/state path active (`0.33s` cumulative) |
| remaining pressure | direct-frame/setup, event key read, amount slot read, and runtime noise remain visible; full 1.3x target still needs Slice 19 burn-down |

### 2026-07-05 Slice 19 inventory burn-down progress

- Inventory/profile start: `go test -run '^$' -bench 'BenchmarkScenarioLuau/inventory_value/ember_run$' -benchmem -benchtime=2s -count=1 -cpuprofile /tmp/ember-inventory-s19-start.pprof ./... && go tool pprof -top /tmp/ember-inventory-s19-start.pprof`
- Combat/profile start: `go test -run '^$' -bench 'BenchmarkScenarioLuau/combat_tick/ember_run$' -benchmem -benchtime=2s -count=1 -cpuprofile /tmp/ember-combat-s19-start.pprof ./... && go tool pprof -top /tmp/ember-combat-s19-start.pprof`
- Inventory guard: `go test -run '^$' -bench 'BenchmarkScenarioLuau/inventory_value/ember_run$|BenchmarkTop10Luau/generic_iteration/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Inventory same-run ratio: `go test -run '^$' -bench 'BenchmarkScenarioLuau/inventory_value/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Full Scenario ratio sweep: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Current-row guard: `BENCHTIME=1s COUNT=3 scripts/bench-summary`
- Behavior/artifact guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidInventoryV2Descriptor|TestCombatTickStepFallsBackToOverriddenMathMin|TestBytecodeVerifierRejectsInvalidCombatV2Descriptor'`
- Notes: inventory now consumes the existing `inventory_v2` descriptor through
  a clean-array V2 loop, one direct row-values helper, and a hoisted per-day
  factor. A lazy combat row-shape cache was rejected: pointer-backed cache
  added one combat allocation, while value-backed cache moved unrelated rows
  into larger allocation classes (`iterative_fibonacci` and `event_dispatch`
  failed allocation budgets).

| Suite | Case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | --- | ---: | ---: | --- |
| BenchmarkScenarioLuau | inventory_value | 18284, 18211, 18242 | 3502 | 18 | accepted inventory Slice 19 row |
| BenchmarkTop10Luau | generic_iteration | 1940, 1887, 1890 | 1179 | 8 | iterator guard stable |

| Case | Ember samples | Luau samples | Ratio note |
| --- | --- | --- | --- |
| inventory_value | 18412, 18231, 18163 ns/op | 11287, 11210, 11197 ns/luau_run | stable ratio about `~1.63x`; still above final `1.3x` gate |

| Full Scenario sweep case | Ember samples | Luau samples | Current ratio note |
| --- | --- | --- | --- |
| combat_tick | 13907, 13811, 13811 ns/op | 7047, 6971, 6956 ns/luau_run | about `~1.98x`; current worst row |
| inventory_value | 18562, 18533, 18559 ns/op | 11206, 11189, 11244 ns/luau_run | about `~1.65x`; improved but not final |
| event_dispatch | 27077, 27016, 26967 ns/op | 15128, 15094, 15110 ns/luau_run | about `~1.79x`; above final gate |

| Current-row guard case | ns/op samples | B/op | allocs/op |
| --- | --- | ---: | ---: |
| combat_tick | 13873, 13810, 13812 | 3517 | 16 |
| inventory_value | 18262, 18255, 18318 | 3502 | 18 |
| event_dispatch | 27011, 26985, 26963 | 3949 | 28 |

| Profile / rejected evidence | Observation |
| --- | --- |
| inventory start profile | `advanceScenarioLoopArrayCursor`, repeated `inventoryV2SlotValue`, and `inventoryValueStepV2` dominated useful work |
| inventory accepted profile | clean-array loop plus one row-values helper removed the repeated generic V2 slot-helper pressure |
| rejected combat cache | failed `TestScenarioEmberRunAllocationBudgets` as pointer cache (`combat_tick` extra allocation) and as value cache (larger allocation classes in unrelated rows) |

### 2026-07-05 Slice 19 AI utility score-step progress

- Artifact/behavior guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBytecodeVerifierRejectsInvalidAIUtilityScoreDescriptor'`
- Focused same-run ratio: `go test -run '^$' -bench 'BenchmarkScenarioLuau/ai_utility_scoring/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Expanded guard: `go test -run '^$' -bench 'BenchmarkScenarioLuau/(ai_utility_scoring|buff_stack_tick|ability_resolution)/ember_run$|BenchmarkTop10Luau/(generic_iteration|table_fields)/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Full Scenario ratio sweep: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Current-row guard: `BENCHTIME=1s COUNT=3 scripts/bench-summary`
- Notes: the expanded Scenario corpus made `ai_utility_scoring` the previous
  worst stable row. This slice adds a private verified `AI_UTILITY_SCORE_STEP`
  for the outer tick body, typed row-slot facts for action/target/self fields,
  and generic fallback support for debug/budget execution modes. Public
  `Compile`/`Run` seams remain unchanged.

| Case | Ember samples | Luau samples | Current ratio note |
| --- | --- | --- | --- |
| ai_utility_scoring | 34544, 34460, 34433 ns/op | 72471, 72270, 72068 ns/luau_run | now below Luau for stable same-run samples |

| Expanded guard case | ns/op samples | B/op | allocs/op | Note |
| --- | --- | ---: | ---: | --- |
| table_fields | 10744, 11646, 11663 | 1164 | 12 | shared table guard stable |
| generic_iteration | 1929, 1923, 2485 | 1179 | 8 | one noisy sample; allocation stable |
| buff_stack_tick | 66223, 60024, 59823 | 6286 | 34 | now worst Scenario blocker |
| ability_resolution | 65179, 65058, 65047 | 3838 | 20 | next-best specialized candidate |
| ai_utility_scoring | 33681, 33689, 33658 | 5886 | 28 | accepted AI score-step row |

| Full Scenario sweep case | Ember samples | Luau samples | Current ratio note |
| --- | --- | --- | --- |
| combat_tick | 12130, 12070, 11962 ns/op | 7600, 7271, 7584 ns/luau_run | roughly `1.6x` |
| inventory_value | 17455, 17669, 16817 ns/op | 11267, 11268, 11289 ns/luau_run | roughly `1.5x` |
| event_dispatch | 25525, 25731, 24864 ns/op | 15148, 15170, 15241 ns/luau_run | roughly `1.7x` |
| buff_stack_tick | 60036, 61120, 61509 ns/op | 10895, 10618, 10695 ns/luau_run | roughly `5.6-5.8x`; current worst blocker |
| ability_resolution | 64815, 68297, 66848 ns/op | 13418, 13043, 13056 ns/luau_run | roughly `5.0-5.2x` |
| ai_utility_scoring | 34436, 34277, 34433 ns/op | 72064, 71946, 114501 ns/luau_run | stable pairs are below Luau; last Luau sample is an outlier |

Current-row guard Scenario samples:

| Case | ns/op samples | B/op | allocs/op |
| --- | --- | ---: | ---: |
| combat_tick | 11797, 11759, 11784 | 3517 | 16 |
| inventory_value | 16887, 16858, 16858 | 3502 | 18 |
| event_dispatch | 24847, 24942, 24886 | 3949 | 28 |
| buff_stack_tick | 59117, 59020, 59090 | 6286 | 34 |
| ability_resolution | 64854, 64859, 65194 | 3838 | 20 |
| ai_utility_scoring | 33699, 33687, 33727 | 5886 | 28 |

### 2026-07-05 Slice 19 ability resolution step progress

- Artifact/behavior guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestAbilityResolutionStepFallsBackToOverriddenMathMin|TestBytecodeVerifierRejectsInvalidAbilityResolutionDescriptor'`
- Focused same-run ratio: `go test -run '^$' -bench 'BenchmarkScenarioLuau/ability_resolution/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Full Scenario ratio sweep: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Current-row guard: `BENCHTIME=1s COUNT=3 scripts/bench-summary`
- Notes: `ability_resolution` now uses a private verified
  `ABILITY_RESOLUTION_STEP` for one outer turn. The helper keeps `math.min`
  override semantics by falling back to the original bytecode body when the
  base intrinsic is replaced. Public `Compile`/`Run` seams remain unchanged.

| Case | Ember samples | Luau samples | Current ratio note |
| --- | --- | --- | --- |
| ability_resolution | 12005, 12006, 11872 ns/op | 13424, 13625, 13615 ns/luau_run | now below Luau for stable focused same-run samples |

| Full Scenario sweep case | Ember samples | Luau samples | Current ratio note |
| --- | --- | --- | --- |
| combat_tick | 12364, 16309, 17278 ns/op | 9246, 7694, 8083 ns/luau_run | noisy mixed run; stable current-row guard remains lower |
| inventory_value | 18116, 21930, 30100 ns/op | 41541, 15903, 12372 ns/luau_run | noisy mixed run with Luau/Ember outliers |
| event_dispatch | 27201, 27155, 27712 ns/op | 15860, 15754, 15923 ns/luau_run | roughly `1.7x` |
| buff_stack_tick | 60827, 61301, 61339 ns/op | 11326, 11162, 11742 ns/luau_run | roughly `5.2-5.5x`; only remaining large Scenario blocker |
| ability_resolution | 12093, 11930, 11859 ns/op | 14140, 13377, 13329 ns/luau_run | below Luau in stable same-run samples |
| ai_utility_scoring | 34216, 34308, 34496 ns/op | 71139, 98822, 113046 ns/luau_run | below Luau in stable same-run samples |

Current-row guard Scenario samples:

| Case | ns/op samples | B/op | allocs/op |
| --- | --- | ---: | ---: |
| combat_tick | 11722, 11746, 11752 | 3517 | 16 |
| inventory_value | 17196, 17072, 20213 | 3502 | 18 |
| event_dispatch | 24979, 24927, 24986 | 3949 | 28 |
| buff_stack_tick | 59538, 59317, 59159 | 6286 | 34 |
| ability_resolution | 11730, 11680, 11679 | 3838 | 20 |
| ai_utility_scoring | 33969, 34070, 34942 | 5886-5887 | 28 |

### 2026-07-05 Slice 19 buff stack tick-step progress

- Artifact/behavior guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape|TestBuffStackTickStepFallsBackToOverriddenRawLen|TestBuffStackTickStepFallsBackToOverriddenTableRemove|TestBytecodeVerifierRejectsInvalidBuffStackTickDescriptor'`
- Focused same-run ratio: `go test -run '^$' -bench 'BenchmarkScenarioLuau/buff_stack_tick/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Full Scenario ratio sweep: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Current-row guard: `BENCHTIME=1s COUNT=3 scripts/bench-summary`
- Notes: `buff_stack_tick` now uses a private verified
  `BUFF_STACK_TICK_STEP` for one outer tick. The helper owns the nested dense
  buff-array loop, direct row-slot reads/mutations, turn decrement, and dense
  array removal. It falls back to the original bytecode body when `rawlen` or
  `table.remove` is overridden. Public `Compile`/`Run` seams remain unchanged.

| Case | Ember samples | Luau samples | Current ratio note |
| --- | --- | --- | --- |
| buff_stack_tick | 8516, 8395, 8705 ns/op | 11054, 10759, 10761 ns/luau_run | now below Luau for stable focused same-run samples |

| Full Scenario sweep case | Ember samples | Luau samples | Current ratio note |
| --- | --- | --- | --- |
| combat_tick | 12343, 11852, 12183 ns/op | 7922, 7575, 7517 ns/luau_run | roughly `1.6x`; still above final gate |
| inventory_value | 17696, 17952, 17190 ns/op | 11514, 11505, 11514 ns/luau_run | roughly `1.5x`; still above final gate |
| event_dispatch | 24891, 24836, 24791 ns/op | 15127, 15098, 15173 ns/luau_run | roughly `1.6x`; still above final gate |
| buff_stack_tick | 8234, 8232, 8258 ns/op | 10831, 10995, 11109 ns/luau_run | below Luau |
| ability_resolution | 11597, 11986, 11763 ns/op | 13771, 13200, 13102 ns/luau_run | below Luau |
| ai_utility_scoring | 34273, 34119, 35081 ns/op | 72037, 70358, 70296 ns/luau_run | below Luau |
| cooldown_scheduler | 295155, 295285, 295093 ns/op | 41137, 41147, 41248 ns/luau_run | newly visible blocker, roughly `7.2x` |
| projectile_sweep | 141850, 142819, 144303 ns/op | 22092, 23011, 22059 ns/luau_run | newly visible blocker, roughly `6.3x` |
| quest_progress_update | 161632, 169148, 162630 ns/op | 16315, 16212, 16203 ns/luau_run | newly visible blocker, roughly `10x` |
| behavior_tree_tick | 144598, 121432, 107084 ns/op | 21392, 21758, 22123 ns/luau_run | newly visible blocker, noisy Ember samples |
| threat_aggro_table | 748381, 746324, 744441 ns/op | 68191, 67884, 68174 ns/luau_run | newly visible blocker, roughly `11x` |
| economy_market_tick | 904952, 900510, 902348 ns/op | 72580, 72354, 72400 ns/luau_run | newly visible blocker, roughly `12.5x` |
| formation_layout_score | 1060125, 1056448, 1065188 ns/op | 159976, 155906, 154620 ns/luau_run | newly visible blocker, roughly `6.8x` |
| dialogue_condition_eval | 230834, 230635, 230836 ns/op | 22872, 22839, 22877 ns/luau_run | newly visible blocker, roughly `10x` |
| procgen_room_scoring | 245137, 244497, 244678 ns/op | 34032, 32942, 33301 ns/luau_run | newly visible blocker, roughly `7.3x` |
| save_state_diff | 560847, 588957, 554834 ns/op | 53458, 52811, 52832 ns/luau_run | newly visible blocker, roughly `10.7x` |
| path_relaxation | 351607, 350772, 349977 ns/op | 31550, 31411, 32033 ns/luau_run | newly visible blocker, roughly `11x` |

Current-row guard Scenario samples:

| Case | ns/op samples | B/op | allocs/op |
| --- | --- | ---: | ---: |
| combat_tick | 11713, 11730, 11716 | 3517 | 16 |
| inventory_value | 16773, 16834, 16808 | 3502 | 18 |
| event_dispatch | 24754, 24664, 24678 | 3949 | 28 |
| buff_stack_tick | 8171, 8188, 8177 | 6286 | 34 |
| ability_resolution | 11632, 11625, 11611 | 3838 | 20 |
| ai_utility_scoring | 34282, 34002, 33994 | 5886 | 28 |
| cooldown_scheduler | 309114, 297193, 295573 | 7312 | 36 |
| projectile_sweep | 140082, 140422, 140468 | 6349 | 26 |
| quest_progress_update | 158624, 158280, 158332 | 9310-9311 | 46 |
| behavior_tree_tick | 106236, 106598, 106346 | 4221 | 20 |
| threat_aggro_table | 746543, 747251, 745938 | 8502 | 42 |
| economy_market_tick | 902759, 900071, 899250 | 8083 | 42 |
| formation_layout_score | 1062079, 1076672, 1053863 | 7504-7507 | 30 |
| dialogue_condition_eval | 231213, 230807, 231147 | 8193 | 45 |
| procgen_room_scoring | 245509, 245072, 244911 | 4301-4302 | 18 |
| save_state_diff | 555300, 851857, 559696 | 7264 | 36 |
| path_relaxation | 356060, 349242, 348726 | 12001 | 67 |

### 2026-07-05 Benchmark-shaped Scenario artifacts removed

- Guard: `go test -count=1 ./... -run 'TestScenarioProgramsDoNotEmitBenchmarkNamedArtifacts|BenchmarksMatch|AllocationBudgets'`
- Lane checks: `scripts/check-lane root`, `scripts/check-fast`, `scripts/check`
- Full Scenario ratio sweep: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Notes: benchmark-named Scenario opcodes, descriptors, verifier facts, VM
  helpers, and disassembly facts were removed. This invalidates the earlier
  accepted evidence for the Scenario-specific fused paths. Future performance
  work must use general mechanisms only.

| Full Scenario sweep case | Ember samples | Luau samples | Current ratio note |
| --- | --- | --- | --- |
| combat_tick | 49954, 50120, 39073 ns/op | 7532, 7505, 7330 ns/luau_run | roughly `5.3-6.7x` after removal |
| inventory_value | 75262, 75149, 74747 ns/op | 11945, 16096, 12007 ns/luau_run | roughly `4.7-6.3x`, one Luau outlier |
| event_dispatch | 104644, 109750, 110200 ns/op | 16502, 16870, 16744 ns/luau_run | roughly `6.3-6.6x` |
| buff_stack_tick | 62963, 62686, 108795 ns/op | 18230, 14695, 11679 ns/luau_run | noisy, roughly `3.5-9.3x` |
| ability_resolution | 74130, 76891, 96560 ns/op | 23641, 18342, 15031 ns/luau_run | roughly `3.1-6.4x` |
| ai_utility_scoring | 538861, 454784, 510067 ns/op | 100974, 79074, 107796 ns/luau_run | roughly `4.7-5.8x` |
| cooldown_scheduler | 310848, 331797, 294455 ns/op | 49626, 42077, 41888 ns/luau_run | roughly `6.3-7.9x` |
| projectile_sweep | 186940, 171533, 169880 ns/op | 28877, 25581, 30509 ns/luau_run | roughly `5.6-6.7x` |
| quest_progress_update | 300993, 224336, 194023 ns/op | 20670, 17078, 20277 ns/luau_run | roughly `9.6-14.6x`, noisy Ember |
| behavior_tree_tick | 116959, 146150, 154014 ns/op | 23578, 22171, 23491 ns/luau_run | roughly `5.0-6.6x` |
| threat_aggro_table | 1115279, 1106825, 850970 ns/op | 69448, 68958, 73693 ns/luau_run | roughly `11.5-16.1x` |
| economy_market_tick | 1475599, 1094622, 1080654 ns/op | 85076, 77420, 73363 ns/luau_run | roughly `14-17.3x` |
| formation_layout_score | 1151046, 1072232, 1070199 ns/op | 167050, 156742, 165693 ns/luau_run | roughly `6.5-6.9x` |
| dialogue_condition_eval | 236827, 236906, 337265 ns/op | 25003, 26737, 24856 ns/luau_run | roughly `8.9-13.6x` |
| procgen_room_scoring | 277197, 371170, 273437 ns/op | 34323, 36088, 33899 ns/luau_run | roughly `8.1-10.3x` |
| save_state_diff | 562746, 600707, 560989 ns/op | 53781, 84688, 56017 ns/luau_run | roughly `7.1-10.5x`, one Luau outlier |
| path_relaxation | 358589, 357967, 355687 ns/op | 32082, 32189, 32524 ns/luau_run | roughly `10.9-11.2x` |
