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

### 2026-07-05 General array iterator and call-cache mechanisms

- Behavior guard: `go test -count=1 ./...`
- Iterator behavior tests: `go test -count=1 ./... -run 'TestRunDirectFrameArrayIterationPreservesRowOrderAndNilTermination|TestArrayNextIteratorOpcodePreservesMetatableIteratorFallback'`
- Dynamic call behavior tests: `go test -count=1 ./... -run 'TestDynamicFieldCallSeesHandlerMutation|TestCompilerUsesDynamicFieldCallOpcode|TestCompilerUsesLocalOneResultCallOpcode|TestCompileAndRunCoroutineYieldThroughFixedScriptCall'`
- Focused iterator bench: `go test -run '^$' -bench 'BenchmarkTop10Luau/generic_iteration/ember_run$|BenchmarkScenarioLuau/(quest_progress_update|ai_utility_scoring|path_relaxation|inventory_value)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=1 ./...`
- Focused call bench: `go test -run '^$' -bench 'BenchmarkScenarioLuau/(event_dispatch|behavior_tree_tick)/(ember_run|luau_cli_batch)$|BenchmarkTop10Luau/method_calls/ember_run$|BenchmarkTop10Luau/coroutine_yield/ember_run$' -benchmem -benchtime=500ms -count=1 ./...`
- Full short Scenario ratio: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof: `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-scenario-after-arraynext-callcache.out` passed with all seventeen rows.
- Red gate proof: `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-scenario-after-arraynext-callcache.out` exited `1`.
- Notes: added a general private `ARRAY_NEXT` instruction for generic-for
  iterator steps. Clean array iteration uses the existing inline array-next
  implementation without materializing the iterator call frame; custom
  `__iter` and non-array iterators still fall back through normal call
  semantics. Added a frame-local stable table-field script-call cache for
  `handlers[key](...)` shapes, guarded by table identity, key, and string value
  version. Added direct-frame execution for local one-result script calls.

Focused evidence:

| Case | Sample |
| --- | ---: |
| generic_iteration | `1851 ns/op`, `1195 B/op`, `8 allocs/op` |
| inventory_value | `63828 ns/op` Ember, `11934 ns/luau_run` Luau |
| quest_progress_update | `142143 ns/op` Ember, `16576 ns/luau_run` Luau |
| path_relaxation | `259051 ns/op` Ember, `31997 ns/luau_run` Luau |
| event_dispatch | `97071 ns/op` Ember, `15750 ns/luau_run` Luau |
| behavior_tree_tick | `82345 ns/op` Ember, `21626 ns/luau_run` Luau |

Full short Scenario ratio gate output:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2x status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 31120 | 9171 | 3.39x | fail |
| inventory_value | 66979 | 13271 | 5.05x | fail |
| event_dispatch | 112390 | 17042 | 6.59x | fail |
| buff_stack_tick | 63184 | 11877 | 5.32x | fail |
| ability_resolution | 70264 | 14940 | 4.70x | fail |
| ai_utility_scoring | 478383 | 77208 | 6.20x | fail |
| cooldown_scheduler | 310096 | 46707 | 6.64x | fail |
| projectile_sweep | 135922 | 23304 | 5.83x | fail |
| quest_progress_update | 148522 | 16919 | 8.78x | fail |
| behavior_tree_tick | 88728 | 22914 | 3.87x | fail |
| threat_aggro_table | 1079377 | 100220 | 10.77x | fail |
| economy_market_tick | 568302 | 74357 | 7.64x | fail |
| formation_layout_score | 1443449 | 178100 | 8.10x | fail |
| dialogue_condition_eval | 174586 | 24615 | 7.09x | fail |
| procgen_room_scoring | 202926 | 33866 | 5.99x | fail |
| save_state_diff | 374572 | 54041 | 6.93x | fail |
| path_relaxation | 279551 | 32518 | 8.60x | fail |

### 2026-07-05 General direct-frame and row-branch mechanisms

- Behavior guard: `go test -count=1 ./...`
- Focused profile: `go test -run '^$' -bench 'BenchmarkScenarioLuau/save_state_diff/ember_run$' -benchmem -benchtime=1s -count=1 -cpuprofile /tmp/ember-save-state.pprof ./... && go tool pprof -top /tmp/ember-save-state.pprof`
- Focused row/intrinsic guard: `go test -run '^$' -bench 'BenchmarkScenarioLuau/(quest_progress_update|cooldown_scheduler|projectile_sweep|combat_tick|buff_stack_tick|save_state_diff)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=1 ./...`
- Full short Scenario ratio: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof: `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-scenario-general-after-direct.out` passed with all seventeen rows.
- Red gate proof: `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-scenario-general-after-direct.out` exited `1`.
- Notes: added general direct-frame support for `GET_INDEX`, `LOAD_GLOBAL`,
  `CALL_ONE` rawlen, `TABLE_INSERT`, `TABLE_REMOVE`, `GET_STRING_FIELD2`,
  `SET_STRING_FIELD2`, `ADD_SUB_STRING_FIELD2`, and numeric `NEG`; added
  frame-local dynamic string-key inline caches; split string-field layout and
  value versions; and added row-slot numeric field branch forms. These are
  reusable language mechanisms, not Scenario-named artifacts.

Focused evidence:

| Case | Before/after note |
| --- | --- |
| save_state_diff | focused profile moved from about `594542 ns/op` generic-frame dominated to about `364962 ns/op` direct-frame dominated after `GET_INDEX` and `NEG` direct-frame support |
| quest_progress_update | focused sample moved from about `203692 ns/op` to `152827 ns/op` after row-slot numeric branch forms |
| buff_stack_tick | short focused sample after direct rawlen/table intrinsics: about `58309 ns/op` |

Full short Scenario ratio gate output:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2x status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 31139 | 8272 | 3.76x | fail |
| inventory_value | 67928 | 12121 | 5.60x | fail |
| event_dispatch | 105739 | 16062 | 6.58x | fail |
| buff_stack_tick | 58752 | 11133 | 5.28x | fail |
| ability_resolution | 65220 | 13806 | 4.72x | fail |
| ai_utility_scoring | 448250 | 71919 | 6.23x | fail |
| cooldown_scheduler | 291522 | 42388 | 6.88x | fail |
| projectile_sweep | 139572 | 22579 | 6.18x | fail |
| quest_progress_update | 155443 | 16754 | 9.28x | fail |
| behavior_tree_tick | 86158 | 22003 | 3.92x | fail |
| threat_aggro_table | 537556 | 69278 | 7.76x | fail |
| economy_market_tick | 524925 | 73267 | 7.16x | fail |
| formation_layout_score | 907476 | 157410 | 5.77x | fail |
| dialogue_condition_eval | 162461 | 23539 | 6.90x | fail |
| procgen_room_scoring | 199024 | 33211 | 5.99x | fail |
| save_state_diff | 364421 | 53939 | 6.76x | fail |
| path_relaxation | 270819 | 32198 | 8.41x | fail |

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

### 2026-07-05 General row string-field store and full-corpus gates

- Behavior/allocation guard: `go test -count=1 ./... -run 'TestScenarioEmberRunAllocationBudgets|TestBytecodeFinalizerRejectsInvalidRowStringFieldWriteSlot|TestCompilerUsesRowStringFieldStoreOpcode|TestRunRowStringFieldStoreFallsBackToNewIndexAfterDelete'`
- Focused behavior guard: `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestBytecodeFinalizerRejectsInvalidRowStringFieldWriteSlot|TestCompilerUsesRowStringFieldStoreOpcode|TestRunRowStringFieldStoreFallsBackToNewIndexAfterDelete|TestScenarioProgramsDoNotEmitBenchmarkNamedArtifacts'`
- Focused row/table bench: `go test -run '^$' -bench 'BenchmarkScenarioLuau/(combat_tick|event_dispatch|projectile_sweep|quest_progress_update|dialogue_condition_eval)/(ember_run|luau_cli_batch)$|BenchmarkTop10Luau/(table_fields|generic_iteration|array_ops)/ember_run$' -benchmem -benchtime=1s -count=3 ./...`
- Full Scenario ratio sweep: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Gate proof: `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-scenario-full-after-row-store.out` passed with all seventeen rows present.
- Red gate proof: `SCENARIO_RATIO_MAX=1.3 scripts/scenario-ratio-gate < /tmp/ember-scenario-full-after-row-store.out` exited `1`.
- Notes: added the general `SET_ROW_STRING_FIELD` bytecode for slot-aware
  string-field stores on row-shaped tables. This is not benchmark-named and
  falls back through ordinary table semantics for metatables, stale slots, nil
  deletes, missing fields, and map-backed tables. The Scenario ratio gate now
  requires all seventeen current rows, and `TestScenarioEmberRunAllocationBudgets`
  covers the full Scenario corpus.

Focused guard samples:

| Suite | Case | ns/op samples | B/op | allocs/op |
| --- | --- | --- | ---: | ---: |
| BenchmarkTop10Luau | table_fields | 11765, 10923, 10837 | 1164 | 12 |
| BenchmarkTop10Luau | array_ops | 12064, 11297, 12493 | 9886 | 8 |
| BenchmarkTop10Luau | generic_iteration | 2033, 2061, 2016 | 1179 | 8 |
| BenchmarkScenarioLuau | combat_tick | 36785, 32712, 34437 | 3517 | 16 |
| BenchmarkScenarioLuau | event_dispatch | 105029, 101651, 111770 | 3870 | 29 |
| BenchmarkScenarioLuau | projectile_sweep | 167515, 393484, 236389 | 6350 | 26 |
| BenchmarkScenarioLuau | quest_progress_update | 325302, 181187, 160464 | 9310-9311 | 46 |
| BenchmarkScenarioLuau | dialogue_condition_eval | 241194, 251021, 279139 | 8192-8193 | 45 |

Full Scenario ratio gate output:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 1.3 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 32586 | 8217 | 3.97x | fail |
| inventory_value | 72689 | 15415 | 4.72x | fail |
| event_dispatch | 120463 | 17716 | 6.80x | fail |
| buff_stack_tick | 79677 | 18360 | 4.34x | fail |
| ability_resolution | 72620 | 14014 | 5.18x | fail |
| ai_utility_scoring | 499686 | 74712 | 6.69x | fail |
| cooldown_scheduler | 308055 | 44198 | 6.97x | fail |
| projectile_sweep | 162333 | 23615 | 6.87x | fail |
| quest_progress_update | 158245 | 17867 | 8.86x | fail |
| behavior_tree_tick | 116486 | 23808 | 4.89x | fail |
| threat_aggro_table | 846687 | 80576 | 10.51x | fail |
| economy_market_tick | 926773 | 73759 | 12.56x | fail |
| formation_layout_score | 1381439 | 233341 | 5.92x | fail |
| dialogue_condition_eval | 239517 | 24550 | 9.76x | fail |
| procgen_room_scoring | 259377 | 34377 | 7.54x | fail |
| save_state_diff | 609196 | 54079 | 11.26x | fail |
| path_relaxation | 367929 | 32902 | 11.18x | fail |

### 2026-07-05 General nested field-index paths and row shape propagation

- Behavior guard: `go test -count=1 ./... -run 'TestRunDirectFrameNestedStringFieldIndexPathsPreserveValues|TestStringFieldIndexPathsUseMetatableSemantics|TestCompilerPropagatesRowSlotsThroughLocalArrayIndex'`
- Full behavior guard: `go test -count=1 ./...`
- Focused field-index bench: `go test -run '^$' -bench 'BenchmarkScenarioLuau/(threat_aggro_table|economy_market_tick|dialogue_condition_eval|save_state_diff|path_relaxation)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Full short Scenario ratio: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof: `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-scenario-after-field-index.out` passed with all seventeen rows present.
- Red gate proof: `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-scenario-after-field-index.out` exited `1`.
- Row-index rerun: `go test -run '^$' -bench 'BenchmarkScenarioLuau/(save_state_diff|path_relaxation)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=700ms -count=3 ./...`
- Notes: added general `GET_STRING_FIELD_INDEX` and
  `SET_STRING_FIELD_INDEX` bytecode for `a.b[c]` paths. The direct path uses
  raw table access only when both tables have no metatables; otherwise it falls
  back through normal table access. Also propagated existing row-shape facts to
  locals assigned from `rows[index]`, so subsequent field branches and reads can
  use slot-aware row operations. Hoisted optional direct-frame opcode accounting
  out of the hot method-call path; the benchmark signal was mostly noise, but
  the runtime path no longer pays a method call when accounting is disabled.

Focused field-index samples:

| Case | Ember ns/op | Luau ns/run | Ratio note |
| --- | ---: | ---: | --- |
| threat_aggro_table | 507574 | 70109 | `7.24x`; improved from the prior short full-sweep `10.77x` band |
| economy_market_tick | 477231 | 95081 | `5.02x`; improved from the prior short full-sweep `7.64x` band |
| dialogue_condition_eval | 185886 | 23746 | `7.83x`; same broad band |
| save_state_diff | 361020 | 54127 | `6.67x`; same broad band |
| path_relaxation | 272396 | 32335 | `8.42x`; same broad band |

Full short Scenario ratio gate output after field-index paths:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 30399 | 8080 | 3.76x | fail |
| inventory_value | 63866 | 11995 | 5.32x | fail |
| event_dispatch | 105301 | 16341 | 6.44x | fail |
| buff_stack_tick | 58264 | 11388 | 5.12x | fail |
| ability_resolution | 65361 | 13754 | 4.75x | fail |
| ai_utility_scoring | 438116 | 72319 | 6.06x | fail |
| cooldown_scheduler | 293074 | 43008 | 6.81x | fail |
| projectile_sweep | 135240 | 24677 | 5.48x | fail |
| quest_progress_update | 146536 | 16858 | 8.69x | fail |
| behavior_tree_tick | 86521 | 22411 | 3.86x | fail |
| threat_aggro_table | 506117 | 69995 | 7.23x | fail |
| economy_market_tick | 482786 | 75799 | 6.37x | fail |
| formation_layout_score | 917974 | 157989 | 5.81x | fail |
| dialogue_condition_eval | 163686 | 23565 | 6.95x | fail |
| procgen_room_scoring | 197778 | 33596 | 5.89x | fail |
| save_state_diff | 350346 | 53819 | 6.51x | fail |
| path_relaxation | 271782 | 32226 | 8.43x | fail |

Row-index propagation rerun:

| Case | Ember samples | Luau samples | Ratio note |
| --- | --- | --- | --- |
| save_state_diff | 368560, 359545, 355767 ns/op | 53797, 53115, 56398 ns/luau_run | roughly `6.3-6.9x`; no allocation change |
| path_relaxation | 282115, 286850, 289991 ns/op | 32369, 32027, 31922 ns/luau_run | roughly `8.7-9.1x`; no allocation change |

Current-tree short Scenario ratio after row-index propagation and accounting
cleanup:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 31942 | 7289 | 4.38x | fail |
| inventory_value | 64474 | 11791 | 5.47x | fail |
| event_dispatch | 105343 | 15379 | 6.85x | fail |
| buff_stack_tick | 57908 | 10718 | 5.40x | fail |
| ability_resolution | 64891 | 13183 | 4.92x | fail |
| ai_utility_scoring | 432987 | 71084 | 6.09x | fail |
| cooldown_scheduler | 292011 | 42320 | 6.90x | fail |
| projectile_sweep | 133864 | 22156 | 6.04x | fail |
| quest_progress_update | 144087 | 16603 | 8.68x | fail |
| behavior_tree_tick | 86399 | 21370 | 4.04x | fail |
| threat_aggro_table | 504716 | 68277 | 7.39x | fail |
| economy_market_tick | 472609 | 73050 | 6.47x | fail |
| formation_layout_score | 885493 | 155228 | 5.70x | fail |
| dialogue_condition_eval | 166193 | 22921 | 7.25x | fail |
| procgen_room_scoring | 192373 | 32877 | 5.85x | fail |
| save_state_diff | 351527 | 53187 | 6.61x | fail |
| path_relaxation | 279666 | 31399 | 8.91x | fail |

### 2026-07-05 General register numeric branch forms

- Behavior guard: `go test -count=1 ./... -run 'TestCompilerUsesRegisterNumericLessBranch|TestRegisterNumericLessBranchFallsBackToStringComparison|TestCompilerUsesRegisterNumericGreaterBranch'`
- Full behavior guard: `go test -count=1 ./...`
- Focused branch bench: `go test -run '^$' -bench 'BenchmarkScenarioLuau/(formation_layout_score|procgen_room_scoring|behavior_tree_tick|quest_progress_update|path_relaxation)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=1 ./...`
- Full short Scenario ratio: `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof: `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-scenario-after-register-branches.out` passed with all seventeen rows present.
- Red gate proof: `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-scenario-after-register-branches.out` exited `1`.
- Notes: added general `JUMP_IF_NOT_LESS` and `JUMP_IF_NOT_GREATER`
  register/register branch opcodes for simple numeric comparisons in
  conditions. Direct frame handles non-NaN numbers and falls back to generic
  comparison semantics for strings, tables, metamethods, and unsupported
  operands. This removes the temporary boolean compare result for ordinary
  `if left < right` and `if left > right` shapes.

Focused branch samples:

| Case | Ember ns/op | Luau ns/run | Ratio note |
| --- | ---: | ---: | --- |
| quest_progress_update | 290635 | 33202 | `8.75x`; noisy same-run pair, no allocation change |
| behavior_tree_tick | 88392 | 21752 | `4.06x`; same broad band |
| formation_layout_score | 957488 | 156382 | `6.12x`; same broad band |
| procgen_room_scoring | 187254 | 32812 | `5.71x`; slightly better Ember band |
| path_relaxation | 277655 | 32029 | `8.67x`; same broad band |

Current-tree short Scenario ratio after register branch forms:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 30639 | 7137 | 4.29x | fail |
| inventory_value | 64037 | 11304 | 5.66x | fail |
| event_dispatch | 104340 | 15634 | 6.67x | fail |
| buff_stack_tick | 58921 | 11258 | 5.23x | fail |
| ability_resolution | 69055 | 13219 | 5.22x | fail |
| ai_utility_scoring | 421171 | 83256 | 5.06x | fail |
| cooldown_scheduler | 293413 | 41533 | 7.06x | fail |
| projectile_sweep | 136401 | 22229 | 6.14x | fail |
| quest_progress_update | 143462 | 16409 | 8.74x | fail |
| behavior_tree_tick | 85916 | 21437 | 4.01x | fail |
| threat_aggro_table | 514682 | 69781 | 7.38x | fail |
| economy_market_tick | 482190 | 75277 | 6.41x | fail |
| formation_layout_score | 876167 | 158262 | 5.54x | fail |
| dialogue_condition_eval | 166731 | 23162 | 7.20x | fail |
| procgen_room_scoring | 187343 | 32992 | 5.68x | fail |
| save_state_diff | 363430 | 53175 | 6.83x | fail |
| path_relaxation | 278173 | 31697 | 8.78x | fail |

### 2026-07-05 Row string-field helper tightening

- Behavior guard before helper change:
  `go test -count=1 ./... -run 'TestRunRowStringFieldReadFallsBackAfterShapeChange'`
- Focused behavior guard after helper change:
  `go test -count=1 ./... -run 'TestRunRowStringFieldReadFallsBackAfterShapeChange|TestRunDirectFrameRowStringFieldBranchPreservesSlotSemantics|TestCompilerPropagatesRowSlotsThroughLocalArrayIndex|TestCompilerUsesRowStringFieldEqualityBranchOpcode|TestCompilerUsesRowStringFieldNumericBranchOpcodes|TestStringFieldIndexPathsUseMetatableSemantics'`
- Full behavior guard: `go test -count=1 ./...`
- Focused row helper bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(path_relaxation|quest_progress_update|threat_aggro_table)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=1 ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-scenario-row-helper-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-scenario-row-helper-ratio.out`
  exited `1`.
- Notes: tightened the private direct-frame row string-field helper so row-slot
  fast path, raw string-field fallback, and metatable fallback share the table
  validation already performed by the helper. Added a public `Compile`/`Run`
  behavior guard proving stale row-slot reads fall back after inline table
  shape changes.

Focused row-helper samples:

| Case | Ember ns/op | Luau ns/run | Ratio note |
| --- | ---: | ---: | --- |
| quest_progress_update | 149459 | 18820 | `7.94x`; no allocation change |
| threat_aggro_table | 521328 | 71028 | `7.34x`; no allocation change |
| path_relaxation | 378907 | 79665 | `4.76x`; noisy Luau sample, no allocation change |

Current-tree short Scenario ratio after row-helper tightening:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 37491 | 7999 | 4.69x | fail |
| inventory_value | 67584 | 12099 | 5.59x | fail |
| event_dispatch | 106116 | 15765 | 6.73x | fail |
| buff_stack_tick | 60518 | 12496 | 4.84x | fail |
| ability_resolution | 71871 | 14294 | 5.03x | fail |
| ai_utility_scoring | 453502 | 74078 | 6.12x | fail |
| cooldown_scheduler | 330945 | 46632 | 7.10x | fail |
| projectile_sweep | 138704 | 23024 | 6.02x | fail |
| quest_progress_update | 146777 | 17302 | 8.48x | fail |
| behavior_tree_tick | 87476 | 22261 | 3.93x | fail |
| threat_aggro_table | 520712 | 70716 | 7.36x | fail |
| economy_market_tick | 486202 | 75689 | 6.42x | fail |
| formation_layout_score | 879950 | 159494 | 5.52x | fail |
| dialogue_condition_eval | 174657 | 24627 | 7.09x | fail |
| procgen_room_scoring | 195584 | 34694 | 5.64x | fail |
| save_state_diff | 372594 | 61770 | 6.03x | fail |
| path_relaxation | 293981 | 33817 | 8.69x | fail |

### 2026-07-05 String-field nil branch form

- Behavior guard: `go test -count=1 ./... -run 'TestCompilerUsesStringFieldNilBranchOpcode'`
- Focused field-branch guard:
  `go test -count=1 ./... -run 'TestCompilerUsesStringFieldNilBranchOpcode|TestRunStringFieldEqualityBranchOpcode|TestCompilerUsesStringFieldEqualityBranchOpcode|TestCompilerUsesRowStringFieldEqualityBranchOpcode|TestCompilerUsesStringFieldNumericBranchOpcodes|TestCompilerUsesRowStringFieldNumericBranchOpcodes|TestStringFieldIndexPathsUseMetatableSemantics|TestRunRowStringFieldReadFallsBackAfterShapeChange'`
- Full behavior guard: `go test -count=1 ./...`
- Focused field-nil bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(dialogue_condition_eval|quest_progress_update|threat_aggro_table|path_relaxation)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=1 ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-field-nil-branch-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-field-nil-branch-ratio.out`
  exited `1`.
- Notes: added general `JUMP_IF_STRING_FIELD_NIL` for `field ~= nil`
  conditions. The branch preserves false-vs-nil behavior and falls back for
  metatable-dependent reads.

Focused field-nil samples:

| Case | Ember ns/op | Luau ns/run | Ratio note |
| --- | ---: | ---: | --- |
| quest_progress_update | 142332 | 17077 | `8.33x`; no allocation change |
| threat_aggro_table | 515371 | 72586 | `7.10x`; no allocation change |
| dialogue_condition_eval | 163349 | 23849 | `6.85x`; improved focused field-heavy row |
| path_relaxation | 282592 | 32768 | `8.62x`; no allocation change |

Current-tree short Scenario ratio after field-nil branch:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 31180 | 7784 | 4.01x | fail |
| inventory_value | 76121 | 12487 | 6.10x | fail |
| event_dispatch | 103717 | 15842 | 6.55x | fail |
| buff_stack_tick | 59052 | 11312 | 5.22x | fail |
| ability_resolution | 67002 | 13781 | 4.86x | fail |
| ai_utility_scoring | 424046 | 72754 | 5.83x | fail |
| cooldown_scheduler | 299305 | 46737 | 6.40x | fail |
| projectile_sweep | 149731 | 25775 | 5.81x | fail |
| quest_progress_update | 163570 | 19469 | 8.40x | fail |
| behavior_tree_tick | 96468 | 22933 | 4.21x | fail |
| threat_aggro_table | 610387 | 81641 | 7.48x | fail |
| economy_market_tick | 589199 | 93126 | 6.33x | fail |
| formation_layout_score | 1008041 | 169707 | 5.94x | fail |
| dialogue_condition_eval | 208398 | 24471 | 8.52x | fail |
| procgen_room_scoring | 195545 | 34922 | 5.60x | fail |
| save_state_diff | 369099 | 54544 | 6.77x | fail |
| path_relaxation | 273727 | 31996 | 8.56x | fail |

### 2026-07-05 String-field negated boolean branch form

- Red tracer before implementation:
  `go test -count=1 ./... -run 'TestCompilerUsesStringFieldNotBranchOpcode'`
  failed because `not field` materialized `GET_STRING_FIELD`, boolean
  constants, and a second `JUMP_IF_FALSE`.
- Behavior guard:
  `go test -count=1 ./... -run 'TestCompilerUsesStringFieldNotBranchOpcode'`
- Focused predicate guard:
  `go test -count=1 ./... -run 'TestCompilerUsesStringFieldNotBranchOpcode|TestCompilerUsesStringFieldNilBranchOpcode|TestRunStringFieldEqualityBranchOpcode|TestCompilerUsesStringFieldEqualityBranchOpcode|TestCompilerUsesRowStringFieldEqualityBranchOpcode|TestCompilerUsesStringFieldNumericBranchOpcodes|TestCompilerUsesRowStringFieldNumericBranchOpcodes|TestRunDirectFrameStringFieldBranchPredicatesPreserveSemantics|TestRunDirectFrameRowStringFieldBranchPreservesSlotSemantics|TestRunRowStringFieldReadFallsBackAfterShapeChange'`
- Full behavior guard: `go test -count=1 ./...`
- Focused before bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(path_relaxation|projectile_sweep|combat_tick|behavior_tree_tick)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Focused repeat bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(path_relaxation|projectile_sweep|combat_tick|behavior_tree_tick)/ember_run$' -benchmem -benchtime=700ms -count=3 ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Outlier check:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(buff_stack_tick|ability_resolution)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=700ms -count=3 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-boolean-predicate-branch-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-boolean-predicate-branch-ratio.out`
  exited `1`.
- Notes: added general `JUMP_IF_STRING_FIELD_TRUE` for `not field`
  conditions. The branch preserves false/nil truthiness and falls back for
  metatable-dependent reads.

Focused boolean predicate repeat samples:

| Case | Ember ns/op samples | Allocation note |
| --- | --- | --- |
| combat_tick | 38641, 31774, 36641 | no allocation change |
| projectile_sweep | 156934, 333228, 150362 | one noisy outlier; no allocation change |
| behavior_tree_tick | 97599, 90110, 87867 | no allocation change |
| path_relaxation | 283558, 260173, 257397 | improved from the focused before sample |

Outlier recheck:

| Case | Ember ns/op samples | Luau ns/run samples | Allocation note |
| --- | --- | --- | --- |
| buff_stack_tick | 57655, 65148, 57673 | 11738, 10973, 10912 | full-sweep `253514 ns/op` was noise |
| ability_resolution | 65350, 65434, 65291 | 13426, 14199, 13886 | full-sweep `98602 ns/op` was noise |

Current-tree short Scenario ratio after boolean predicate branch:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 31317 | 8505 | 3.68x | fail |
| inventory_value | 71455 | 11808 | 6.05x | fail |
| event_dispatch | 107363 | 17532 | 6.12x | fail |
| buff_stack_tick | 253514 | 17988 | 14.09x | fail/noisy |
| ability_resolution | 98602 | 13859 | 7.11x | fail/noisy |
| ai_utility_scoring | 509284 | 75626 | 6.73x | fail |
| cooldown_scheduler | 298666 | 42028 | 7.11x | fail |
| projectile_sweep | 154568 | 22505 | 6.87x | fail |
| quest_progress_update | 137672 | 17051 | 8.07x | fail |
| behavior_tree_tick | 92607 | 21684 | 4.27x | fail |
| threat_aggro_table | 526600 | 68566 | 7.68x | fail |
| economy_market_tick | 497630 | 73947 | 6.73x | fail |
| formation_layout_score | 859330 | 157753 | 5.45x | fail |
| dialogue_condition_eval | 171065 | 23517 | 7.27x | fail |
| procgen_room_scoring | 188522 | 34654 | 5.44x | fail |
| save_state_diff | 360904 | 54677 | 6.60x | fail |
| path_relaxation | 266330 | 31805 | 8.37x | fail |

### 2026-07-05 String-field equal-nil branch form

- Red tracer before implementation:
  `go test -count=1 ./... -run 'TestCompilerUsesStringFieldEqualNilBranchOpcode'`
  failed because `field == nil` materialized `GET_STRING_FIELD`, `LOAD_CONST
  nil`, `EQUAL`, and `JUMP_IF_FALSE`.
- Behavior guard:
  `go test -count=1 ./... -run 'TestCompilerUsesStringFieldEqualNilBranchOpcode'`
- Focused predicate guard:
  `go test -count=1 ./... -run 'TestCompilerUsesStringFieldEqualNilBranchOpcode|TestCompilerUsesStringFieldNotBranchOpcode|TestCompilerUsesStringFieldNilBranchOpcode|TestRunStringFieldEqualityBranchOpcode|TestCompilerUsesStringFieldEqualityBranchOpcode|TestCompilerUsesRowStringFieldEqualityBranchOpcode|TestCompilerUsesStringFieldNumericBranchOpcodes|TestCompilerUsesRowStringFieldNumericBranchOpcodes|TestRunDirectFrameStringFieldBranchPredicatesPreserveSemantics|TestRunDirectFrameRowStringFieldBranchPreservesSlotSemantics|TestRunRowStringFieldReadFallsBackAfterShapeChange'`
- Full behavior guard: `go test -count=1 ./...`
- Focused field-heavy bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(dialogue_condition_eval|quest_progress_update|path_relaxation)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=1 ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-field-equal-nil-branch-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-field-equal-nil-branch-ratio.out`
  exited `1`.
- Notes: added general `JUMP_IF_STRING_FIELD_NOT_NIL` for `field == nil`
  conditions. This completes the first field-presence branch family with
  false-vs-nil behavior preserved.

Focused equal-nil samples:

| Case | Ember ns/op | Luau ns/run | Ratio note |
| --- | ---: | ---: | --- |
| quest_progress_update | 136140 | 17458 | `7.80x`; no allocation change |
| dialogue_condition_eval | 162006 | 23476 | `6.90x`; no allocation change |
| path_relaxation | 259742 | 35034 | `7.41x`; no allocation change |

Current-tree short Scenario ratio after equal-nil branch:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 29624 | 7952 | 3.73x | fail |
| inventory_value | 64282 | 12108 | 5.31x | fail |
| event_dispatch | 102172 | 16172 | 6.32x | fail |
| buff_stack_tick | 60690 | 11457 | 5.30x | fail |
| ability_resolution | 66411 | 14034 | 4.73x | fail |
| ai_utility_scoring | 419928 | 72869 | 5.76x | fail |
| cooldown_scheduler | 296533 | 43356 | 6.84x | fail |
| projectile_sweep | 132196 | 23406 | 5.65x | fail |
| quest_progress_update | 143515 | 17251 | 8.32x | fail |
| behavior_tree_tick | 85857 | 44775 | 1.92x | pass/noisy Luau |
| threat_aggro_table | 570452 | 90582 | 6.30x | fail |
| economy_market_tick | 483188 | 72932 | 6.63x | fail |
| formation_layout_score | 850221 | 155579 | 5.46x | fail |
| dialogue_condition_eval | 157460 | 23093 | 6.82x | fail |
| procgen_room_scoring | 184773 | 33336 | 5.54x | fail |
| save_state_diff | 343703 | 53302 | 6.45x | fail |
| path_relaxation | 265133 | 31664 | 8.37x | fail |

### 2026-07-06 Row-field/register numeric branch form

- Red tracer before implementation:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldRegisterNumericBranchOpcode'`
  failed because `candidate < row.dist` materialized `GET_ROW_STRING_FIELD`
  and then used `JUMP_IF_NOT_LESS`.
- Behavior guard:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldRegisterNumericBranchOpcode'`
- Focused branch guard:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldRegisterNumericBranchOpcode|TestCompilerUsesStringFieldNumericBranchOpcodes|TestCompilerUsesRowStringFieldNumericBranchOpcodes|TestRunStringFieldNumericBranchOpcodes|TestCompilerUsesStringFieldNotBranchOpcode|TestCompilerUsesStringFieldEqualNilBranchOpcode|TestRunDirectFrameStringFieldBranchPredicatesPreserveSemantics|TestCompilerPropagatesRowSlotsThroughLocalArrayIndex|TestRunRowStringFieldReadFallsBackAfterShapeChange'`
- Full behavior guard: `go test -count=1 ./...`
- Focused scenario bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(path_relaxation|quest_progress_update)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=700ms -count=3 ./...`
- Profile check:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/path_relaxation/ember_run$' -benchmem -benchtime=1s -count=1 -cpuprofile /tmp/ember-path-row-field-register-branch.pprof ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-row-field-register-branch-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-row-field-register-branch-ratio.out`
  exited `1`.
- Notes: added general `JUMP_IF_STRING_FIELD_NOT_GREATER_R` and
  row-slot `JUMP_IF_ROW_STRING_FIELD_NOT_GREATER_R`. This covers the
  Luau-shaped branch `left < table.field` by reading the right-hand field
  inside the branch opcode after the left expression is evaluated. NaN,
  non-number, metatable fallback, and stale row slots still fall back through
  the existing comparison path.

Focused row-field/register samples:

| Case | Ember ns/op samples | Luau ns/run samples | Allocation note |
| --- | --- | --- | --- |
| quest_progress_update | 136160, 139442, 135191 | 16678, 16372, 16535 | no allocation change |
| path_relaxation | 246812, 247354, 248413 | 31560, 31557, 31585 | improved from the preceding `268531 ns/op` profile sample |

Path profile after the branch form:

| Function | Flat sample share | Note |
| --- | ---: | --- |
| `(*vmThread).runDirectFrame` | 62.26% | dispatch remains dominant |
| `directFrameRowStringField` | 4.72% | row field compare no longer requires a separate pre-branch materialization |
| `(*Table).rawStringField` | 2.83% | still visible; table path reuse and storage deepening remain pressure |

Current-tree short Scenario ratio after row-field/register branch:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 43037 | 8991 | 4.79x | fail/noisy |
| inventory_value | 64256 | 11564 | 5.56x | fail |
| event_dispatch | 102233 | 15331 | 6.67x | fail |
| buff_stack_tick | 58227 | 10769 | 5.41x | fail |
| ability_resolution | 64793 | 13295 | 4.87x | fail |
| ai_utility_scoring | 408419 | 71254 | 5.73x | fail |
| cooldown_scheduler | 300729 | 41574 | 7.23x | fail |
| projectile_sweep | 129990 | 22290 | 5.83x | fail |
| quest_progress_update | 132926 | 16395 | 8.11x | fail |
| behavior_tree_tick | 89960 | 21655 | 4.15x | fail |
| threat_aggro_table | 491863 | 68281 | 7.20x | fail |
| economy_market_tick | 468602 | 72685 | 6.45x | fail |
| formation_layout_score | 845380 | 154992 | 5.45x | fail |
| dialogue_condition_eval | 163215 | 23079 | 7.07x | fail |
| procgen_room_scoring | 185488 | 33110 | 5.60x | fail |
| save_state_diff | 342027 | 53382 | 6.41x | fail |
| path_relaxation | 262354 | 31576 | 8.31x | fail/noisy |

### 2026-07-06 Row-field pair numeric branch form

- Red tracer before implementation:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldPairNumericBranchOpcode'`
  failed because `row.have < row.need` materialized `GET_ROW_STRING_FIELD`
  for the left side and then used the previous row-field/register branch for
  the right side.
- Behavior guard:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldPairNumericBranchOpcode'`
- Focused branch guard:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldPairNumericBranchOpcode|TestCompilerUsesRowStringFieldRegisterNumericBranchOpcode|TestCompilerUsesStringFieldNumericBranchOpcodes|TestCompilerUsesRowStringFieldNumericBranchOpcodes|TestRunStringFieldNumericBranchOpcodes|TestCompilerPropagatesRowSlotsThroughLocalArrayIndex|TestRunRowStringFieldReadFallsBackAfterShapeChange'`
- Full behavior guard: `go test -count=1 ./...`
- Focused scenario bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(path_relaxation|quest_progress_update)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=700ms -count=3 ./...`
- Profile check:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/quest_progress_update/ember_run$' -benchmem -benchtime=1s -count=1 -cpuprofile /tmp/ember-quest-row-field-pair-branch.pprof ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-row-field-pair-branch-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-row-field-pair-branch-ratio.out`
  exited `1`.
- Notes: added `JUMP_IF_ROW_STRING_FIELD_NOT_LESS_FIELD` for same-row
  `row.left < row.right` comparisons when row slots are known. This removes
  the branch operand materialization shape used by `objective.have <
  objective.need`, but focused benchmarks were roughly flat, so larger wins
  likely need iteration/dispatch or table-path reuse rather than more local
  branch cleanup.

Focused row-field pair samples:

| Case | Ember ns/op samples | Luau ns/run samples | Allocation note |
| --- | --- | --- | --- |
| quest_progress_update | 136725, 138812, 136011 | 16578, 16386, 16477 | roughly flat; no allocation change |
| path_relaxation | 257021, 250241, 249122 | 31590, 31535, 31678 | roughly flat/noisy; no allocation change |

Quest profile after the branch form:

| Function | Flat sample share | Note |
| --- | ---: | --- |
| `(*vmThread).runDirectFrame` | 55.88% | dispatch remains dominant |
| `baseArrayNextInline` | 7.84% | iteration now shows more clearly |
| `directFrameRowStringField` | 0.00% flat, 1.96% cumulative | row-field materialization is no longer the primary quest pressure |

Current-tree short Scenario ratio after row-field pair branch:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 29443 | 7248 | 4.06x | fail/noisy Luau |
| inventory_value | 63692 | 11443 | 5.57x | fail |
| event_dispatch | 99948 | 15350 | 6.51x | fail |
| buff_stack_tick | 57303 | 10943 | 5.24x | fail |
| ability_resolution | 65149 | 13314 | 4.89x | fail |
| ai_utility_scoring | 410955 | 71262 | 5.77x | fail |
| cooldown_scheduler | 311160 | 41766 | 7.45x | fail |
| projectile_sweep | 131917 | 22279 | 5.92x | fail |
| quest_progress_update | 143360 | 16434 | 8.72x | fail/noisy |
| behavior_tree_tick | 85675 | 21495 | 3.99x | fail |
| threat_aggro_table | 496430 | 68327 | 7.27x | fail |
| economy_market_tick | 473687 | 72925 | 6.50x | fail |
| formation_layout_score | 849895 | 155570 | 5.46x | fail |
| dialogue_condition_eval | 161472 | 23129 | 6.98x | fail |
| procgen_room_scoring | 186374 | 32946 | 5.66x | fail |
| save_state_diff | 347311 | 53517 | 6.49x | fail |
| path_relaxation | 251782 | 31834 | 7.91x | fail |

### 2026-07-06 Fused two-result array iterator branch

- Red tracer before implementation:
  `go test -count=1 ./... -run 'TestCompilerUsesArrayNextJumpForTwoResultArrayIteration'`
  failed because two-result array loops emitted `ARRAY_NEXT`, `MOVE`,
  `NOT_EQUAL`, and `JUMP_IF_FALSE`.
- Behavior and fallback guard:
  `go test -count=1 ./... -run 'TestCompilerUsesArrayNextJumpForTwoResultArrayIteration|TestRunDirectFrameArrayIterationPreservesRowOrderAndNilTermination|TestArrayNextIteratorOpcodePreservesMetatableIteratorFallback|TestCompileAndRunGenericForPairsLoop|TestCompileAndRunGenericForIPairsStopsAtFirstNil|TestCompileAndRunBreakExitsGenericForLoop|TestCompileAndRunContinueSkipsGenericForLoopBody|TestCompileAndRunGenericForSkipsEmptyTable'`
- Full behavior guard: `go test -count=1 ./...`
- Focused scenario bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(quest_progress_update|path_relaxation|combat_tick|inventory_value|projectile_sweep)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=700ms -count=3 ./...`
- Profile check:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/quest_progress_update/ember_run$' -benchmem -benchtime=1s -count=1 -cpuprofile /tmp/ember-quest-array-next-jump2.pprof ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-array-next-jump2-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-array-next-jump2-ratio.out`
  exited `1`.
- Notes: added `ARRAY_NEXT_JUMP2`, a private two-result generic-for opcode
  that advances array iteration and branches on nil termination in one
  dispatch. The generic runner still supports custom iterator calls; the
  direct runner side-exits unless `PREPARE_ITER` produced the native array
  iterator.

Focused fused-iterator samples:

| Case | Ember ns/op samples | Luau ns/run samples | Allocation note |
| --- | --- | --- | --- |
| combat_tick | 26673, 27203, 26363 | 7280, 7164, 7119 | improved; no allocation change |
| inventory_value | 58307, 58347, 58463 | 11513, 11465, 11478 | improved; no allocation change |
| projectile_sweep | 120524, 125101, 120311 | 22358, 22300, 22219 | improved; no allocation change |
| quest_progress_update | 110410, 111026, 110525 | 16561, 17091, 16889 | improved sharply from the prior focused `136-139us` band |
| path_relaxation | 224333, 223734, 224284 | 31623, 31711, 51125 | improved; third Luau sample noisy |

Quest profile after fused iterator branch:

| Function | Flat sample share | Note |
| --- | ---: | --- |
| `(*vmThread).runDirectFrame` | 55.34% | dispatch still dominates, but fewer loop-control instructions execute |
| `(*Table).rawStringField` | 9.71% | table field lookup is the next visible pressure |
| `baseArrayNextInline` | 6.80% | remaining iterator work is the inline cursor itself |
| `directFrameRowStringField` | 2.91% flat / 4.85% cumulative | row slot reads remain visible but below table lookup |

Current-tree short Scenario ratio after fused iterator branch:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 43370 | 8435 | 5.14x | fail/noisy Ember |
| inventory_value | 58853 | 11980 | 4.91x | fail |
| event_dispatch | 95376 | 15601 | 6.11x | fail |
| buff_stack_tick | 58085 | 11082 | 5.24x | fail |
| ability_resolution | 62584 | 13538 | 4.62x | fail |
| ai_utility_scoring | 387932 | 73165 | 5.30x | fail |
| cooldown_scheduler | 267173 | 41892 | 6.38x | fail |
| projectile_sweep | 121353 | 22387 | 5.42x | fail |
| quest_progress_update | 111828 | 20004 | 5.59x | fail |
| behavior_tree_tick | 104990 | 22263 | 4.72x | fail/noisy Ember |
| threat_aggro_table | 458535 | 69235 | 6.62x | fail |
| economy_market_tick | 451628 | 73464 | 6.15x | fail |
| formation_layout_score | 804920 | 156146 | 5.15x | fail |
| dialogue_condition_eval | 141634 | 24015 | 5.90x | fail |
| procgen_room_scoring | 182815 | 33334 | 5.48x | fail |
| save_state_diff | 315481 | 53555 | 5.89x | fail |
| path_relaxation | 225997 | 33176 | 6.81x | fail |

### 2026-07-06 Row-field pair string equality branch form

- Current plan phase: Phase 19, Predicate And Tag Dispatch Facts; specifically
  Slice 19.1, String Equality Branch Facts. The plan has phases 0 through 22
  (23 total); numerically phases 20, 21, and 22 remain after the current phase,
  but earlier partial slices still need burn-down before completion.
- Red tracer before implementation:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldPairEqualityBranchOpcode'`
  failed because `objective.kind == event.kind and objective.target ==
  event.target` materialized two `GET_ROW_STRING_FIELD` instructions per
  comparison, then used generic `EQUAL` and `JUMP_IF_FALSE`.
- Behavior guard:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldPairEqualityBranchOpcode'`
- Focused predicate guard:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldPairEqualityBranchOpcode|TestCompilerUsesStringFieldEqualityBranchOpcode|TestCompilerUsesRowStringFieldEqualityBranchOpcode|TestRunStringFieldEqualityBranchOpcode|TestCompilerUsesStringFieldNotBranchOpcode|TestCompilerUsesStringFieldEqualNilBranchOpcode|TestRunDirectFrameStringFieldBranchPredicatesPreserveSemantics|TestRunRowStringFieldReadFallsBackAfterShapeChange'`
- Full behavior guard: `go test -count=1 ./...`
- Focused scenario bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(quest_progress_update|formation_layout_score|inventory_value|dialogue_condition_eval|economy_market_tick)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=700ms -count=3 ./...`
- Profile check:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/quest_progress_update/ember_run$' -benchmem -benchtime=1s -count=1 -cpuprofile /tmp/ember-quest-row-field-equality.pprof ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-row-field-equality-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-row-field-equality-ratio.out`
  exited `1`.
- Notes: added `JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_FIELD`, which reads two
  slot-known row string fields inside one branch and supports `and` chains by
  funneling all false branches into one patchable exit jump. The direct runner
  side-exits for metatable/table/userdata cases; the generic runner preserves
  normal equality fallback.

Focused row-field equality samples:

| Case | Ember ns/op samples | Luau ns/run samples | Allocation note |
| --- | --- | --- | --- |
| inventory_value | 60936, 63079, 59793 | 11628, 11969, 11583 | roughly flat/noisy; no allocation change |
| quest_progress_update | 114054, 113262, 114161 | 16446, 16373, 16419 | flat versus fused iterator baseline; no allocation change |
| economy_market_tick | 454050, 454772, 451415 | 72993, 72542, 73433 | roughly flat; no allocation change |
| formation_layout_score | 764105, 761199, 765434 | 156101, 155991, 155668 | improved versus prior short sweep |
| dialogue_condition_eval | 142686, 143284, 142700 | 23185, 23045, 22975 | roughly flat; no allocation change |

Quest profile after row-field equality branch:

| Function | Flat sample share | Note |
| --- | ---: | --- |
| `(*vmThread).runDirectFrame` | 52.04% | dispatch remains dominant |
| `directFrameRowStringField` | 8.16% flat / 10.20% cumulative | row-slot lookup remains visible inside fused predicates |
| `baseArrayNextInline` | 5.10% | fused iterator reduced loop-control dispatch but cursor work remains |
| `(*Table).rawStringField` | 4.08% | lower than the fused-iterator profile, but not eliminated |

Current-tree short Scenario ratio after row-field equality branch:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 31027 | 7917 | 3.92x | fail |
| inventory_value | 59256 | 11575 | 5.12x | fail |
| event_dispatch | 96002 | 15377 | 6.24x | fail |
| buff_stack_tick | 57154 | 10886 | 5.25x | fail |
| ability_resolution | 62008 | 13420 | 4.62x | fail |
| ai_utility_scoring | 390656 | 72536 | 5.39x | fail |
| cooldown_scheduler | 270763 | 44903 | 6.03x | fail |
| projectile_sweep | 129917 | 23403 | 5.55x | fail |
| quest_progress_update | 114191 | 16772 | 6.81x | fail |
| behavior_tree_tick | 85710 | 21591 | 3.97x | fail |
| threat_aggro_table | 466948 | 68638 | 6.80x | fail |
| economy_market_tick | 456191 | 72868 | 6.26x | fail |
| formation_layout_score | 786150 | 155287 | 5.06x | fail |
| dialogue_condition_eval | 145073 | 23196 | 6.25x | fail |
| procgen_room_scoring | 187729 | 33187 | 5.66x | fail |
| save_state_diff | 319452 | 53958 | 5.92x | fail |
| path_relaxation | 228452 | 31759 | 7.19x | fail |

### 2026-07-06 Row-field pair string inequality branch form

- Current plan phase: Phase 19, Predicate And Tag Dispatch Facts; specifically
  Slice 19.1, String Equality Branch Facts.
- Red tracer before implementation:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldPairInequalityBranchOpcode'`
  failed because `left.zone ~= right.zone` materialized two
  `GET_ROW_STRING_FIELD` instructions, then used generic `NOT_EQUAL` and
  `JUMP_IF_FALSE`.
- Behavior guard:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldPairInequalityBranchOpcode|TestCompilerUsesRowStringFieldPairEqualityBranchOpcode'`
- Focused predicate guard:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldPairInequalityBranchOpcode|TestCompilerUsesRowStringFieldPairEqualityBranchOpcode|TestCompilerUsesStringFieldEqualityBranchOpcode|TestCompilerUsesRowStringFieldEqualityBranchOpcode|TestRunStringFieldEqualityBranchOpcode|TestCompilerUsesStringFieldNotBranchOpcode|TestCompilerUsesStringFieldEqualNilBranchOpcode|TestRunDirectFrameStringFieldBranchPredicatesPreserveSemantics|TestRunRowStringFieldReadFallsBackAfterShapeChange'`
- Full behavior guard: `go test -count=1 ./...`
- Focused scenario bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(save_state_diff|quest_progress_update|formation_layout_score|dialogue_condition_eval)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=700ms -count=3 ./...`
- Profile check:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/save_state_diff/ember_run$' -benchmem -benchtime=1s -count=1 -cpuprofile /tmp/ember-save-row-field-inequality.pprof ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-row-field-inequality-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-row-field-inequality-ratio.out`
  exited `1`.
- Notes: added `JUMP_IF_ROW_STRING_FIELD_EQUAL_FIELD`, the false-branch form
  for slot-known row-field string inequality. The direct runner side-exits for
  metatable/table/userdata cases; the generic runner preserves normal equality
  fallback for non-fast string values.

Focused row-field inequality samples:

| Case | Ember ns/op samples | Luau ns/run samples | Allocation note |
| --- | --- | --- | --- |
| quest_progress_update | 112600, 113128, 113798 | 16679, 16439, 16457 | roughly flat versus equality branch; no allocation change |
| formation_layout_score | 740099, 739860, 739022 | 156174, 154861, 155090 | improved versus the prior short sweep |
| dialogue_condition_eval | 138558, 138095, 137887 | 23084, 23102, 23003 | improved versus the prior short sweep |
| save_state_diff | 297021, 293933, 295417 | 53038, 53329, 52765 | improved from the prior short-sweep `319452 ns/op` band |

Save-state profile after row-field inequality branch:

| Function | Flat sample share | Note |
| --- | ---: | --- |
| `(*vmThread).runDirectFrame` | 47.00% | dispatch remains dominant |
| `runtime.madvise` | 14.00% | runtime noise in this sample |
| `(*Table).rawStringField` | 8.00% | table lookup remains a shared pressure |
| `directFrameRowStringField` | 5.00% | row-slot lookup remains visible but below dispatch |
| `(*dynamicStringIndexCache).store` | 3.00% | dynamic-key paths remain future pressure |

Current-tree short Scenario ratio after row-field inequality branch:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 26719 | 7695 | 3.47x | fail |
| inventory_value | 57460 | 11887 | 4.83x | fail |
| event_dispatch | 97424 | 15910 | 6.12x | fail |
| buff_stack_tick | 57140 | 11233 | 5.09x | fail |
| ability_resolution | 62137 | 13894 | 4.47x | fail |
| ai_utility_scoring | 386018 | 73062 | 5.28x | fail |
| cooldown_scheduler | 269047 | 42781 | 6.29x | fail |
| projectile_sweep | 123078 | 23114 | 5.32x | fail |
| quest_progress_update | 113048 | 17064 | 6.62x | fail |
| behavior_tree_tick | 86427 | 22101 | 3.91x | fail |
| threat_aggro_table | 461758 | 70090 | 6.59x | fail |
| economy_market_tick | 457478 | 75116 | 6.09x | fail |
| formation_layout_score | 759062 | 159299 | 4.77x | fail |
| dialogue_condition_eval | 142858 | 24110 | 5.93x | fail |
| procgen_room_scoring | 183932 | 34075 | 5.40x | fail |
| save_state_diff | 302703 | 54213 | 5.58x | fail |
| path_relaxation | 226224 | 32797 | 6.90x | fail |

### 2026-07-06 Guarded finite string-tag elseif chains

- Current plan phase: Phase 19, Predicate And Tag Dispatch Facts; specifically
  Slice 19.2, Finite Tag Chain Dispatch.
- Red tracer before implementation:
  `go test -count=1 ./... -run 'TestCompilerLoadsRowStringTagOnceForElseIfChain'`
  failed because a repeated `buff.kind` `if`/`elseif` chain emitted one
  `JUMP_IF_ROW_STRING_FIELD_NOT_EQUAL_K` per arm.
- Behavior guard:
  `go test -count=1 ./... -run 'TestCompilerLoadsRowStringTagOnceForElseIfChain|TestRunStringTagElseIfChainMetatableFallbackPreservesRepeatedReads|TestCompilerUsesRowStringFieldPairInequalityBranchOpcode|TestCompilerUsesRowStringFieldPairEqualityBranchOpcode|TestRunDirectFrameStringFieldBranchPredicatesPreserveSemantics|TestRunRowStringFieldReadFallsBackAfterShapeChange'`
- Full behavior guard: `go test -count=1 ./...`
- Focused scenario bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(buff_stack_tick|ability_resolution|ai_utility_scoring|threat_aggro_table|procgen_room_scoring|inventory_value)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=700ms -count=3 ./...`
- Profile check:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/buff_stack_tick/ember_run$' -benchmem -benchtime=1s -count=1 -cpuprofile /tmp/ember-buff-tag-chain.pprof ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-tag-chain-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-tag-chain-ratio.out`
  exited `1`.
- Notes: added `JUMP_IF_TABLE_HAS_METATABLE` and a private compiler lowering
  for repeated string-tag `if`/`elseif` chains. Raw-table chains load the tag
  field once, then use `JUMP_IF_NOT_EQUAL_K` string branches; tables with
  metatables jump to the original source-order branch chain so repeated
  `__index` reads and side effects are preserved.

Focused finite-tag samples:

| Case | Ember ns/op samples | Luau ns/run samples | Allocation note |
| --- | --- | --- | --- |
| inventory_value | 57427, 57144, 104675 | 19236, 11805, 11589 | first Luau and third Ember samples noisy; no allocation change |
| buff_stack_tick | 53944, 54234, 64184 | 11899, 13424, 11899 | stable samples improved versus prior short-sweep `57140 ns/op`; no allocation change |
| ability_resolution | 60431, 59197, 58977 | 13995, 14791, 14026 | improved versus prior short-sweep `62137 ns/op`; no allocation change |
| ai_utility_scoring | 366301, 426975, 369387 | 72780, 72690, 72013 | two stable samples improved; one Ember sample noisy; no allocation change |
| threat_aggro_table | 453858, 468603, 452782 | 69974, 69864, 72151 | roughly flat; no allocation change |
| procgen_room_scoring | 207648, 179593, 184171 | 33235, 38103, 70783 | noisy Luau tail; no allocation change |

Buff profile after guarded tag-chain lowering:

| Function | Flat sample share | Note |
| --- | ---: | --- |
| `(*vmThread).runDirectFrame` | 39.45% | dispatch remains dominant |
| `baseGlobalDefinitionFor` | 4.59% | global/base-library lookup is now visible around `rawlen` and table helpers |
| `(*globalEnv).get` | 2.75% flat / 11.93% cumulative | stable global/import facts remain future pressure |
| `baseArrayNextInline` | 2.75% | iterator cursor work remains visible |
| `directFrameRowStringField` | 1.83% | repeated tag-field materialization is no longer the main buff pressure |

Current-tree short Scenario ratio after guarded tag-chain lowering:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 25981 | 7593 | 3.42x | fail |
| inventory_value | 57505 | 11659 | 4.93x | fail |
| event_dispatch | 100893 | 16245 | 6.21x | fail |
| buff_stack_tick | 94805 | 12782 | 7.42x | fail/noisy Ember |
| ability_resolution | 64495 | 14191 | 4.54x | fail |
| ai_utility_scoring | 393758 | 71911 | 5.48x | fail |
| cooldown_scheduler | 287606 | 103055 | 2.79x | fail/noisy Luau |
| projectile_sweep | 161326 | 25262 | 6.39x | fail/noisy Ember |
| quest_progress_update | 119848 | 18105 | 6.62x | fail |
| behavior_tree_tick | 117942 | 39643 | 2.98x | fail/noisy Luau |
| threat_aggro_table | 1323425 | 152085 | 8.70x | fail/noisy both |
| economy_market_tick | 561372 | 81408 | 6.90x | fail |
| formation_layout_score | 831214 | 159918 | 5.20x | fail |
| dialogue_condition_eval | 143872 | 26184 | 5.49x | fail |
| procgen_room_scoring | 267560 | 51909 | 5.15x | fail/noisy |
| save_state_diff | 459722 | 76562 | 6.00x | fail/noisy |
| path_relaxation | 322688 | 56940 | 5.67x | fail/noisy |

### 2026-07-06 Guarded finite string-tag elseif chains with `and` predicates

- Red tracer before implementation:
  `go test -count=1 ./... -run 'TestCompilerLoadsRowStringTagOnceForElseIfChainWithAndGuards'`
  failed because `room.kind == "combat" and step % 3 == 0` chains
  materialized `GET_ROW_STRING_FIELD` for the tag in each arm.
- Focused predicate guard:
  `go test -count=1 ./... -run 'TestCompilerLoadsRowStringTagOnceForElseIfChain|TestCompilerLoadsRowStringTagOnceForElseIfChainWithAndGuards|TestRunStringTagElseIfChainMetatableFallbackPreservesRepeatedReads|TestCompilerUsesRowStringFieldPairInequalityBranchOpcode|TestCompilerUsesRowStringFieldPairEqualityBranchOpcode|TestCompilerUsesStringFieldEqualityBranchOpcode|TestCompilerUsesRowStringFieldEqualityBranchOpcode|TestRunStringFieldEqualityBranchOpcode|TestRunDirectFrameStringFieldBranchPredicatesPreserveSemantics|TestRunRowStringFieldReadFallsBackAfterShapeChange'`
- Full behavior guard: `go test -count=1 ./...`
- Focused scenario bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(procgen_room_scoring|buff_stack_tick|ability_resolution|ai_utility_scoring)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=700ms -count=3 ./...`
- Profile check:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/procgen_room_scoring/ember_run$' -benchmem -benchtime=1s -count=1 -cpuprofile /tmp/ember-procgen-tag-chain-guards.pprof ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-tag-chain-guards-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-tag-chain-guards-ratio.out`
  exited `1`.
- Notes: extended the guarded tag-chain lowering so each arm can carry
  remaining `and` comparisons. The compiler tests the loaded tag first, then
  evaluates the arm-local guard only if the tag matched. The metatable fallback
  remains the original source-order chain.

Focused guarded-tag samples:

| Case | Ember ns/op samples | Luau ns/run samples | Allocation note |
| --- | --- | --- | --- |
| buff_stack_tick | 54650, 55632, 56460 | 11436, 10856, 10906 | stable versus previous tag-chain slice; no allocation change |
| ability_resolution | 60034, 60249, 63421 | 14535, 13579, 13431 | stable; no allocation change |
| ai_utility_scoring | 366332, 380998, 379330 | 76766, 74299, 71476 | stable; no allocation change |
| procgen_room_scoring | 141257, 141463, 143962 | 33379, 33144, 34016 | improved sharply from the noisy prior `179-267us` band; no allocation change |

Procgen profile after guarded `and` tag-chain lowering:

| Function | Flat sample share | Note |
| --- | ---: | --- |
| `(*vmThread).runDirectFrame` | 55.05% | dispatch dominates after branch materialization drops |
| `directFrameRowStringField` | 10.09% flat / 15.60% cumulative | row field reads remain the local pressure |
| `baseArrayNextInline` | 2.75% | iterator cursor work remains visible |
| `(*Table).rawStringField` | 0.92% | raw table lookup is present but below row helper cost |

Current-tree short Scenario ratio after guarded `and` tag-chain lowering:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 27736 | 7561 | 3.67x | fail |
| inventory_value | 57355 | 11934 | 4.81x | fail |
| event_dispatch | 99437 | 15937 | 6.24x | fail |
| buff_stack_tick | 54105 | 10937 | 4.95x | fail |
| ability_resolution | 59240 | 13458 | 4.40x | fail |
| ai_utility_scoring | 363358 | 71961 | 5.05x | fail |
| cooldown_scheduler | 265262 | 41950 | 6.32x | fail |
| projectile_sweep | 121322 | 22414 | 5.41x | fail |
| quest_progress_update | 110445 | 16604 | 6.65x | fail |
| behavior_tree_tick | 85019 | 22292 | 3.81x | fail |
| threat_aggro_table | 452539 | 68803 | 6.58x | fail |
| economy_market_tick | 444501 | 73553 | 6.04x | fail |
| formation_layout_score | 740226 | 168184 | 4.40x | fail |
| dialogue_condition_eval | 138822 | 23308 | 5.96x | fail |
| procgen_room_scoring | 140240 | 33255 | 4.22x | fail |
| save_state_diff | 294963 | 53758 | 5.49x | fail |
| path_relaxation | 221843 | 32949 | 6.73x | fail |

### 2026-07-06 Boolean field predicates inside `and` chains

- Current plan phase: Phase 19, Predicate And Tag Dispatch Facts; specifically
  Slice 19.4, Boolean Field Branch Facts.
- Red tracer before implementation:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldTruthyInAndBranch'`
  failed because `actor.alive and value > top` materialized `actor.alive`
  with `GET_ROW_STRING_FIELD`, then used generic boolean materialization and
  `JUMP_IF_FALSE`.
- Focused predicate guard:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldTruthyInAndBranch|TestCompilerUsesStringFieldTruthyBranchOpcode|TestRunStringFieldTruthyBranchOpcode|TestCompilerUsesStringFieldNotBranchOpcode|TestCompilerUsesStringFieldNilBranchOpcode|TestCompilerUsesStringFieldEqualNilBranchOpcode|TestRunDirectFrameStringFieldBranchPredicatesPreserveSemantics|TestRunRowStringFieldReadFallsBackAfterShapeChange|TestCompilerUsesRowStringFieldPairInequalityBranchOpcode|TestCompilerLoadsRowStringTagOnceForElseIfChainWithAndGuards'`
- Full behavior guard: `go test -count=1 ./...`
- Focused scenario bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(combat_tick|projectile_sweep|path_relaxation|behavior_tree_tick|threat_aggro_table)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=700ms -count=3 ./...`
- Profile check:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/threat_aggro_table/ember_run$' -benchmem -benchtime=1s -count=1 -cpuprofile /tmp/ember-threat-and-boolean.pprof ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-and-boolean-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-and-boolean-ratio.out`
  exited `1`.
- Notes: added a private AND-chain branch planner that stitches field
  truthiness and local-register numeric false-branches together after proving
  every term in the chain is supported. The public seam remains `Compile` and
  `Run`; no new VM opcode was needed because `JUMP_IF_STRING_FIELD_FALSE`
  already carries row-slot facts.

Focused boolean-AND samples:

| Case | Ember ns/op samples | Luau ns/run samples | Allocation note |
| --- | --- | --- | --- |
| combat_tick | 26325, 27873, 27783 | 17319, 12966, 7942 | Ember steady; Luau noisy; no allocation change |
| projectile_sweep | 122764, 122086, 122332 | 22972, 25446, 24052 | stable; no allocation change |
| behavior_tree_tick | 85558, 85570, 85872 | 22057, 21631, 22209 | stable; no allocation change |
| threat_aggro_table | 440012, 440691, 439756 | 70190, 68530, 69018 | improved from the prior short-sweep `452539 ns/op`; no allocation change |
| path_relaxation | 222532, 250727, 224706 | 32031, 31946, 38734 | mostly stable; one Ember and one Luau noisy sample |

Threat profile after boolean-AND branch planning:

| Function | Flat sample share | Note |
| --- | ---: | --- |
| `(*vmThread).runDirectFrame` | 44.25% | dispatch remains dominant |
| `directFrameRowStringField` | 7.08% flat / 8.85% cumulative | row field reads still matter after boolean materialization drops |
| `(*Table).rawStringField` | 5.31% | table storage remains visible |
| `(*dynamicStringIndexCache).get` | 4.42% | finite dynamic-key map caches remain future pressure |
| `baseArrayNextInline` | 4.42% | iterator cursor work remains visible |

Current-tree short Scenario ratio after boolean-AND branch planning:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 26165 | 7894 | 3.31x | fail |
| inventory_value | 58381 | 11706 | 4.99x | fail |
| event_dispatch | 96652 | 15779 | 6.13x | fail |
| buff_stack_tick | 55940 | 11509 | 4.86x | fail |
| ability_resolution | 60333 | 13984 | 4.31x | fail |
| ai_utility_scoring | 374680 | 73328 | 5.11x | fail |
| cooldown_scheduler | 267773 | 43730 | 6.12x | fail |
| projectile_sweep | 123884 | 22602 | 5.48x | fail |
| quest_progress_update | 114097 | 17316 | 6.59x | fail |
| behavior_tree_tick | 84758 | 22688 | 3.74x | fail |
| threat_aggro_table | 455305 | 68900 | 6.61x | fail |
| economy_market_tick | 462836 | 77044 | 6.01x | fail |
| formation_layout_score | 746582 | 170083 | 4.39x | fail |
| dialogue_condition_eval | 144712 | 23449 | 6.17x | fail |
| procgen_room_scoring | 144328 | 34641 | 4.17x | fail |
| save_state_diff | 296310 | 64084 | 4.62x | fail |
| path_relaxation | 238416 | 33442 | 7.13x | fail |

### 2026-07-06 Nil field predicates inside `and` chains

- Current plan phase: Phase 19, Predicate And Tag Dispatch Facts; specifically
  Slice 19.3, Nil-Shape Predicate Facts.
- Red tracer before implementation:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldNilInAndBranch'`
  failed because `check.key ~= nil and value > top` materialized `check.key`
  with `GET_STRING_FIELD`, then used generic `NOT_EQUAL` and `JUMP_IF_FALSE`.
- Focused predicate guard:
  `go test -count=1 ./... -run 'TestCompilerUsesRowStringFieldNilInAndBranch|TestCompilerUsesRowStringFieldTruthyInAndBranch|TestCompilerUsesStringFieldNilBranchOpcode|TestCompilerUsesStringFieldEqualNilBranchOpcode|TestCompilerUsesStringFieldNotBranchOpcode|TestCompilerUsesStringFieldTruthyBranchOpcode|TestRunStringFieldTruthyBranchOpcode|TestRunDirectFrameStringFieldBranchPredicatesPreserveSemantics|TestRunRowStringFieldReadFallsBackAfterShapeChange|TestCompilerLoadsRowStringTagOnceForElseIfChainWithAndGuards'`
- Full behavior guard: `go test -count=1 ./...`
- Focused scenario bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(dialogue_condition_eval|quest_progress_update|path_relaxation)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=700ms -count=3 ./...`
- Profile check:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/dialogue_condition_eval/ember_run$' -benchmem -benchtime=1s -count=1 -cpuprofile /tmp/ember-dialogue-nil-and.pprof ./...`
- Full short Scenario ratio:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-nil-and-ratio.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-nil-and-ratio.out`
  exited `1`.
- Notes: extended the private AND-chain branch planner to include
  `JUMP_IF_STRING_FIELD_NIL` and `JUMP_IF_STRING_FIELD_NOT_NIL`. No new VM
  opcode was needed. Variant rows that omit the discriminator field still lack
  propagated slot facts, so deeper finite-shape slot work remains future
  pressure.

Focused nil-AND samples:

| Case | Ember ns/op samples | Luau ns/run samples | Allocation note |
| --- | --- | --- | --- |
| quest_progress_update | 114849, 110352, 110074 | 18212, 17245, 16987 | stable; no allocation change |
| dialogue_condition_eval | 139398, 138645, 138490 | 23189, 23083, 23194 | improved versus the pre-slice profile sample `184580 ns/op`; no allocation change |
| path_relaxation | 221011, 221073, 220674 | 31795, 31745, 31887 | stable; no allocation change |

Dialogue profile after nil-AND branch planning:

| Function | Flat sample share | Note |
| --- | ---: | --- |
| `(*vmThread).runDirectFrame` | 43.81% | dispatch remains dominant |
| `(*Table).rawStringField` | 7.62% flat / 10.48% cumulative | table field lookup remains shared pressure |
| `directFrameRowStringField` | 6.67% flat / 9.52% cumulative | row field reads remain visible |
| `Value.IsNil` | 3.81% | nil checks are still visible but no longer require field materialization in the traced AND shape |
| `baseArrayNextInline` | 2.86% flat / 5.71% cumulative | iterator cursor work remains visible |

Current-tree short Scenario ratio after nil-AND branch planning:

| Case | Ember ns/op avg | Luau ns/run avg | Ratio | 2.0 status |
| --- | ---: | ---: | ---: | --- |
| combat_tick | 27987 | 7689 | 3.64x | fail |
| inventory_value | 55946 | 12309 | 4.55x | fail |
| event_dispatch | 96411 | 15870 | 6.08x | fail |
| buff_stack_tick | 54139 | 11337 | 4.78x | fail |
| ability_resolution | 59046 | 13699 | 4.31x | fail |
| ai_utility_scoring | 364429 | 71343 | 5.11x | fail |
| cooldown_scheduler | 262961 | 41865 | 6.28x | fail |
| projectile_sweep | 120578 | 22485 | 5.36x | fail |
| quest_progress_update | 109951 | 16743 | 6.57x | fail |
| behavior_tree_tick | 84384 | 21665 | 3.89x | fail |
| threat_aggro_table | 438663 | 78258 | 5.61x | fail |
| economy_market_tick | 444881 | 74559 | 5.97x | fail |
| formation_layout_score | 757556 | 155758 | 4.86x | fail |
| dialogue_condition_eval | 139693 | 23343 | 5.98x | fail |
| procgen_room_scoring | 139644 | 33173 | 4.21x | fail |
| save_state_diff | 295221 | 53530 | 5.52x | fail |
| path_relaxation | 220768 | 31845 | 6.93x | fail |

### 2026-07-06 Opcode metadata table checkpoint

- Current plan phase: Phase 20, Opcode Metadata And Artifact Trust; first
  checkpoint across Slice 20.1 and Slice 20.2.
- Behavior: no public runtime behavior change intended.
- Work: added one private opcode metadata table for opcode names,
  direct-frame eligibility, direct-frame unsupported reasons, control-flow
  kind, jump-target slot, operand shapes, call/yield risk, table/global
  effects, and allocation risk. `classifyInstructionOperands`,
  `opcodeControlFlow`, `opcodeJumpTarget`, direct-frame rejection, and opcode
  effect helpers now read that table for known opcodes.
- Artifact finalizer: added a private `executionArtifact` builder/apply seam
  for existing derived proto facts and routed `newProtoWithDescriptors` plus
  `bytecodeBuilder.proto` through `finalizeProtoExecutionArtifact`.
- Coverage guard:
  `go test -count=1 ./... -run 'TestOpcodeMetadataCoversEveryOpcode|TestOpcodeMetadataValidationRejectsMalformedEntries|TestBytecodeVerifierRejectsDirectFrameDispatchForUnsupportedOpcode|TestProtoDirectFrameRejectionReportsFirstUnsupportedOpcode|TestExecutionArtifactFinalizerRebuildsDerivedProtoFacts|TestBytecodeFinalizerReturnsVerifiedProto|TestBytecodeFinalizerRejectsInvalidCompilerProto'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: this is a trust/artifact slice, so no scenario ratio movement is
  expected. Remaining pressure after this checkpoint is applying the artifact
  seam to future fact families and using metadata effect flags in CFG-aware
  optimization.

### 2026-07-06 Basic-block bytecode peephole tracer

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; first
  checkpoint in Slice 21.1, Basic-Block Peepholes.
- Behavior: no public runtime behavior change intended.
- Work: changed `optimizeBytecodeIR` so bytecode with jumps can still remove
  self-moves inside basic blocks. Deleted-instruction remapping now rewrites
  `B` and `D` jump targets through opcode metadata.
- Focused guard:
  `go test -count=1 ./... -run 'TestOptimizeBytecodeIRRemovesSelfMovesWithBranches|TestBytecodeIRBlockOrderSplitsJumpTargetsAndFallthrough|TestBytecodeIRLivenessTracksBranchJoinRegisters|TestBytecodeBuilderPatchesIRJumpTargets'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: this is a correctness/artifact tracer before benchmarking. Remaining
  Slice 21.1 pressure is block-local move round-trip cleanup and line metadata
  coverage before Scenario timing.

### 2026-07-06 CFG peephole and conservative DCE checkpoint

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; continued
  work across Slices 21.1, 21.2, and a conservative Slice 21.3 tracer.
- Behavior: no public runtime behavior change intended.
- Work: block-local move round trips now consult CFG liveness so temps live
  out through a join are preserved. Jump remapping is covered for `B` jumps,
  specialized `D`-slot predicate branches, and backward loop jumps. A
  conservative liveness DCE pass removes dead `LOAD_CONST`/`MOVE` cascades only
  inside simple blocks whose instruction shapes have proven read/write models.
- Focused guard:
  `go test -count=1 ./... -run 'TestOptimizeBytecodeIRRemovesSelfMovesWithBranches|TestOptimizeBytecodeIRKeepsMoveRoundTripWhenTempLiveOut|TestOptimizeBytecodeIRRemovesBlockLocalMoveRoundTripWithBranches|TestOptimizeBytecodeIRRemapsSpecializedBranchDTarget|TestOptimizeBytecodeIRRemapsBackwardJumpTarget|TestOptimizeBytecodeIRRemovesDeadPureTemporaries|TestOptimizeBytecodeIRKeepsDeadEffectfulInstructions|TestBytecodeIRBlockOrderSplitsJumpTargetsAndFallthrough|TestBytecodeIRLivenessTracksBranchJoinRegisters|TestBytecodeBuilderPatchesIRJumpTargets'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: an initially broad DCE pass was too aggressive because call,
  intrinsic, table, coroutine, and value-list argument reads need stronger
  coverage before they can participate. The current pass is intentionally
  gated to simple blocks until those models are proven.

### 2026-07-06 Intrinsic read-window DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: no public runtime behavior change intended.
- Work: extended the register read model and register candidate discovery for
  intrinsic argument windows used by `TABLE_INSERT`, `TABLE_REMOVE`,
  `COROUTINE_RESUME`, and `MATH_MIN`. Conservative DCE now admits blocks with
  those proven intrinsic opcodes, preserving their argument loads and effectful
  operations while removing unrelated dead loads.
- Focused guard:
  `go test -count=1 ./... -run 'TestInstructionReadModelCoversIntrinsicArgumentWindows|TestOptimizeBytecodeIRKeepsIntrinsicArgumentLoads|TestOptimizeBytecodeIRRemovesDeadLoadAroundProvenIntrinsicReads|TestOptimizeBytecodeIRKeepsTableInsertArgumentLoads|TestOptimizeBytecodeIRRemovesDeadPureTemporaries|TestOptimizeBytecodeIRKeepsDeadEffectfulInstructions|TestOptimizeBytecodeIRRemovesSelfMovesWithBranches|TestOptimizeBytecodeIRKeepsMoveRoundTripWhenTempLiveOut|TestOptimizeBytecodeIRRemovesBlockLocalMoveRoundTripWithBranches|TestOptimizeBytecodeIRRemapsSpecializedBranchDTarget|TestOptimizeBytecodeIRRemapsBackwardJumpTarget'`
- Full behavior guard: `go test -count=1 ./...`
- Standard checks: `scripts/check-lane root`, `scripts/check-fast`,
  `scripts/check`.
- Notes: table/coroutine/math intrinsic blocks are no longer blanket-skipped
  by the conservative DCE pass. General calls, open-call result windows,
  varargs, table reads/writes beyond these intrinsics, and metamethod-capable
  operations still need their own focused read/write coverage before widening.

### 2026-07-06 Fixed-call read-window DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: no public runtime behavior change intended.
- Work: added explicit read-window coverage for fixed-argument `CALL` and
  `CALL_ONE`. Conservative DCE now admits blocks containing those fixed calls,
  preserving callee/argument loads and the effectful call instruction while
  removing unrelated dead loads.
- Focused guard:
  `go test -count=1 ./... -run 'TestInstructionReadModelCoversFixedCallArgumentWindows|TestOptimizeBytecodeIRRemovesDeadLoadAroundFixedCallReads|TestOptimizeBytecodeIRRemovesDeadLoadAroundOpenCallWithoutPrefix'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: open-call blocks are intentionally still skipped until value-list
  argument/result conventions have their own focused read/write tests.

### 2026-07-06 Table field/index read-model DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: table reads and writes remain effectful; unrelated dead loads can
  now be removed in blocks whose table operand reads are proven.
- Work: added read-model coverage for `GET_FIELD`, `SET_FIELD`, `GET_INDEX`,
  `SET_INDEX`, `GET_STRING_FIELD`, and `SET_STRING_FIELD`. Conservative DCE now
  admits blocks containing those table ops while keeping the table operations
  themselves non-removable through opcode effect metadata.
- Focused guard:
  `go test -count=1 ./... -run 'TestCompileRunTableFieldDCEPreservesEffects|TestInstructionReadModelCoversTableFieldAndIndexOperands|TestOptimizeBytecodeIRRemovesDeadLoadAroundTableFieldRead|TestOptimizeBytecodeIRRemovesDeadLoadAroundTableFieldWrite|TestOptimizeBytecodeIRRemovesDeadLoadAroundFixedCallReads|TestOptimizeBytecodeIRRemovesDeadLoadAroundProvenIntrinsicReads'`
- Full behavior guard: `go test -count=1 ./...`
- Standard checks: `scripts/check-lane root`, `scripts/check-fast`,
  `scripts/check`.
- Notes: deeper table variants, iterator prep, open calls, varargs, and
  metamethod-capable arithmetic still need focused read/write coverage before
  participating in broader DCE.

### 2026-07-06 Row string field read-model DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: row string field reads/writes remain effectful; unrelated dead
  loads can now be removed in blocks whose row field operand reads are proven.
- Work: added read-model coverage for `GET_ROW_STRING_FIELD` and
  `SET_ROW_STRING_FIELD`, admitted those opcodes into the conservative DCE
  block gate, and added public compile/run coverage for a row-field mutation
  and read loop.
- Focused guard:
  `go test -count=1 ./... -run 'TestCompileRunRowStringFieldDCEPreservesEffects|TestCompileRunTableFieldDCEPreservesEffects|TestInstructionReadModelCoversTableFieldAndIndexOperands|TestOptimizeBytecodeIRRemovesDeadLoadAroundRowStringFieldOps'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: deeper string-field pairs/index variants, iterator prep, open calls,
  varargs, and value-list conventions remain future Phase 21.3 pressure.

### 2026-07-06 Nested string-field read-model DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: nested string-field reads/writes remain effectful; unrelated dead
  loads can now be removed in blocks whose nested field operand reads are
  proven.
- Work: added read-model coverage and DCE admission for `GET_STRING_FIELD2`,
  `SET_STRING_FIELD2`, `GET_STRING_FIELD_INDEX`, and
  `SET_STRING_FIELD_INDEX`, plus public compile/run coverage for nested table
  field mutation and read behavior.
- Focused guard:
  `go test -count=1 ./... -run 'TestCompileRunNestedStringFieldDCEPreservesEffects|TestInstructionReadModelCoversTableFieldAndIndexOperands|TestOptimizeBytecodeIRRemovesDeadLoadAroundStringFieldPairOps|TestOptimizeBytecodeIRRemovesDeadLoadAroundStringFieldIndexOps'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: iterator prep, open calls, varargs, value-list conventions, and
  metamethod-capable arithmetic remain future Phase 21.3 pressure.

### 2026-07-06 Iterator read/write-model DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: iterator setup and array-next operations remain effectful;
  unrelated dead loads can now be removed in blocks whose iterator operand
  reads and writes are proven.
- Work: added read/write-model coverage for `PREPARE_ITER`, `ARRAY_NEXT`, and
  `ARRAY_NEXT_JUMP2`, including iterator result register ranges used by
  liveness. Conservative DCE now admits blocks containing those iterator ops
  while keeping iterator operations non-removable through opcode effect and
  control-flow metadata, and remaps iterator jump targets after deletion.
- Focused guard:
  `go test -count=1 ./... -run 'TestInstructionReadWriteModelCoversIteratorOpcodes|TestOptimizeBytecodeIRRemovesDeadLoadAroundIteratorOps|TestCompileRunIteratorDCEPreservesEffects'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: open calls, varargs, value-list conventions, pure numeric temporary
  removal, and metamethod-capable arithmetic remain future Phase 21.3 pressure.

### 2026-07-06 Comparison-branch read-model DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: comparison and numeric predicate branches keep their observable
  control-flow behavior; unrelated dead loads can now be removed in blocks
  whose branch operand reads are proven.
- Work: added read-model coverage for `NUMERIC_FOR_CHECK`,
  `JUMP_IF_NOT_EQUAL_K`, `JUMP_IF_NOT_LESS_K`, `JUMP_IF_NOT_LESS`,
  `JUMP_IF_NOT_GREATER`, and `JUMP_IF_MOD_K_NOT_EQUAL_K`. Conservative DCE now
  admits those branch predicates and remaps `D` jump targets after deletion.
- Focused guard:
  `go test -count=1 ./... -run 'TestInstructionReadModelCoversComparisonBranchOperands|TestOptimizeBytecodeIRRemovesDeadLoadAroundComparisonBranch'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: table-field predicate branches, open calls, varargs, value-list
  conventions, pure numeric temporary removal, and metamethod-capable
  arithmetic remain future Phase 21.3 pressure.

### 2026-07-06 Table-predicate branch read-model DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: table-backed predicate branches remain effectful table reads;
  unrelated dead loads can now be removed in blocks whose predicate operand
  reads are proven.
- Work: added read-model coverage for metatable tests, string-field
  comparisons, row string-field comparisons, register-vs-field predicates, and
  string-field truthy/nil predicates. Conservative DCE now admits those
  branches while keeping them non-removable through table-read/control-flow
  metadata, and remaps `D` jump targets after deletion.
- Focused guard:
  `go test -count=1 ./... -run 'TestInstructionReadModelCoversTablePredicateBranchOperands|TestOptimizeBytecodeIRRemovesDeadLoadAroundTablePredicateBranch|TestCompileRunTablePredicateDCEPreservesEffects'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: open calls, varargs, value-list conventions, pure numeric temporary
  removal, and metamethod-capable arithmetic remain future Phase 21.3 pressure.

### 2026-07-06 Fixed-count vararg read/write-model DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: fixed-count `VARARG` instructions preserve Luau nil-fill and
  vararg result behavior; unrelated dead loads can now be removed in blocks
  whose fixed vararg result windows are proven.
- Work: extended register candidate discovery and write modeling for
  fixed-count `VARARG` result spans. Conservative DCE now admits non-open
  vararg blocks while keeping `VARARG` non-removable through allocation/effect
  metadata. Open vararg blocks remain skipped.
- Focused guard:
  `go test -count=1 ./... -run 'TestInstructionWriteModelCoversFixedVarargResultWindow|TestOptimizeBytecodeIRRemovesDeadLoadAroundFixedVararg|TestOptimizeBytecodeIRSkipsOpenVarargBlocks|TestCompileRunFixedVarargDCEPreservesEffects'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: open calls, open varargs, open returns, pure numeric temporary
  removal, and metamethod-capable arithmetic remain future Phase 21.3 pressure.

### 2026-07-06 Open-return prefix read-model DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: `return prefix, finalCall()` preserves every fixed prefix value and
  final-call expansion behavior; unrelated dead loads can still be removed
  around the return.
- Work: extended register candidate discovery for negative-count `RETURN`
  instructions so liveness sees the fixed prefix register window. This prevents
  DCE from deleting returned prefix values while leaving open producer state
  modeling for a later value-list slice.
- Focused guard:
  `go test -count=1 ./... -run 'TestInstructionReadModelCoversOpenReturnPrefixWindow|TestOptimizeBytecodeIRKeepsOpenReturnPrefixRegisters|TestCompileRunOpenReturnPrefixDCEPreservesEffects'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: open calls, open varargs, full open-return producer validation, pure
  numeric temporary removal, and metamethod-capable arithmetic remain future
  Phase 21.3 pressure.

### 2026-07-06 Open-vararg producer DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: `return ...` preserves open variadic return behavior; unrelated
  dead loads can now be removed in blocks containing the open `VARARG`
  producer.
- Work: admitted open `VARARG` into the conservative DCE block gate while
  keeping `VARARG` non-removable through opcode allocation/effect metadata.
  Open `CALL` producers remain skipped until call-result open state has its own
  model.
- Focused guard:
  `go test -count=1 ./... -run 'TestOptimizeBytecodeIRRemovesDeadLoadAroundOpenVarargReturn|TestCompileRunOpenVarargReturnDCEPreservesEffects|TestOptimizeBytecodeIRRemovesDeadLoadAroundOpenCallWithoutPrefix'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: open calls, full open-return producer validation for calls, pure
  numeric temporary removal, and metamethod-capable arithmetic remain future
  Phase 21.3 pressure.

### 2026-07-06 Fixed-argument open-result call DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: final-call expansion such as `return f()` preserves all returned
  values; unrelated dead loads can now be removed in blocks containing the
  fixed-argument open-result `CALL` producer.
- Work: admitted `CALL` instructions with fixed arguments into the conservative
  DCE block gate even when their result count is open. Calls remain
  non-removable through call/yield/allocation metadata. Open-argument calls are
  covered by the following value-list slice.
- Focused guard:
  `go test -count=1 ./... -run 'TestOptimizeBytecodeIRRemovesDeadLoadAroundOpenResultCall|TestCompileRunOpenResultCallDCEPreservesEffects|TestOptimizeBytecodeIRRemovesDeadLoadAroundOpenCallWithoutPrefix'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: pure numeric temporary removal and metamethod-capable arithmetic
  remain future Phase 21.3 pressure.

### 2026-07-06 Open-argument call read-model DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  read/write model expansion.
- Behavior: expanded call arguments such as `take(prefix, rest())` preserve the
  fixed prefix arguments and the open value-list tail; unrelated dead loads can
  now be removed in those call blocks.
- Work: tightened `CALL` read modeling for open arguments so liveness sees the
  callee plus fixed prefix registers rather than every higher-numbered
  register. Register candidate discovery now enumerates the same prefix window,
  and conservative DCE admits open-argument calls while keeping call producers
  and consumers non-removable through call/yield/allocation metadata.
- Focused guard:
  `go test -count=1 ./... -run 'TestInstructionReadModelCoversOpenCallArgumentPrefixWindow|TestOptimizeBytecodeIRRemovesDeadLoadAroundOpenArgumentCall|TestCompileRunOpenArgumentCallDCEPreservesEffects|TestOptimizeBytecodeIRRemovesDeadLoadAroundOpenResultCall|TestOptimizeBytecodeIRRemovesDeadLoadAroundOpenVarargReturn'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: pure numeric temporary removal and metamethod-capable arithmetic remain
  future Phase 21.3 pressure.

### 2026-07-06 Proven numeric arithmetic DCE expansion

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.3
  liveness dead-code cleanup.
- Behavior: dead arithmetic temporaries are removed only after block-local
  number facts prove their operands cannot invoke arithmetic metamethods.
- Work: added a private bytecode IR optimization-facts path for the compiler
  builder, plus block-local number facts for numeric constants, moves,
  register arithmetic, constant arithmetic, unary negation, and
  descriptor-backed `ADD_NUMERIC_MOD_K`. The old optimizer helper remains
  available for tests that do not need constant or descriptor facts.
- Focused guard:
  `go test -count=1 ./... -run 'Test(OptimizeBytecodeIR(RemovesDeadProvenNumericArithmetic|RemovesDeadProvenInPlaceNumericArithmetic|KeepsDeadUnprovenArithmetic)|CompileRunDeadArithmeticDCEPreservesMetamethodEffects)'`
- Descriptor guard:
  `go test -count=1 ./... -run 'TestOptimizeBytecodeIR(RemovesDeadProvenNumericAddModArithmetic|KeepsDeadUnprovenNumericAddModArithmetic)'`
- DCE cluster guard:
  `go test -count=1 ./... -run 'Test(OptimizeBytecodeIR|InstructionRead|InstructionWrite|InstructionReadWrite|BytecodeIRLiveness|CompileRun.*DCE)'`
- Notes: broader loop constant cleanup and register compaction remain Slice
  21.4 pressure.

### 2026-07-06 Numeric-for zero coercion register cleanup

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.4
  loop constants and register compaction.
- Behavior: numeric `for` start, limit, and step coercions preserve Luau-shaped
  numeric-string conversion while avoiding a dedicated zero register in the
  loop prologue.
- Work: numeric `for` lowering now emits three `ADD_K` coercions against a
  shared zero constant instead of loading `0` into a fourth loop-control
  register and using register-form `ADD`.
- Focused guard:
  `go test -count=1 ./... -run 'Test(CompilerReusesConstantZeroForNumericForCoercions|NumericSuperinstructionPreservesNumericStringLoopConversion|CompilerLowersNumericForToCombinedLoopCheck)'`
- Compiler/numeric cluster guard:
  `go test -count=1 ./... -run 'Test(Compiler.*Numeric|NumericSuperinstruction|RunDirectFrameScalarLoop|CompilerReusesConstantZero|CompilerLowersNumericFor|FinalizedProtoCachesNumberConstants)'`
- Notes: broader register compaction remains open; this is the first narrow
  21.4 cleanup because it removes proven loop-register pressure without
  changing parameter, vararg, upvalue, debug-line, or open-call conventions.

### 2026-07-06 Final register count compaction after DCE

- Current plan phase: Phase 21, CFG-Aware Bytecode Optimization; Slice 21.4
  loop constants and register compaction.
- Behavior: optimized prototypes allocate frames sized to registers still
  referenced by optimized bytecode, while preserving parameters, open value-list
  anchors, and child local-upvalue captures.
- Work: compiler finalization now computes the final register count from the
  optimized instruction read/write model and child upvalue descriptors instead
  of using the compiler allocation high-water mark directly. The lower-level
  bytecode builder finalizer remains strict for verifier tests and hand-built
  prototypes.
- Focused guard:
  `go test -count=1 ./... -run 'Test(RegisterCompactionShrinksAfterDeadCodeCleanup|RegisterAllocation|CompileRunOpen|DirectFrameFactsReflectCapturedLocals|VMFrameAllocatesCapturedCells)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: this is frame-size compaction, not register renumbering. It avoids
  changing operand conventions for varargs, open calls, upvalues, or debug
  line maps.

### 2026-07-06 Table shape token guard seam

- Current plan phase: Phase 22, Table Shape Tokens And Guarded Access; Slice
  22.1 table shape token.
- Behavior: table reads, writes, deletes, metatable fallback, dynamic string
  index caches, and table-field call caches preserve existing behavior.
- Work: introduced a private `tableShapeToken` for string layout, string value,
  and metatable state, then migrated inline string slot guards, direct-frame
  dynamic string index caches, and table-field call caches from raw version
  fields to token comparisons.
- Focused guard:
  `go test -count=1 ./... -run 'Test(TableShapeTokenSplitsStringLayoutAndValueChanges|TableInlineStringFieldSlotsAreLayoutVersionGuarded|TableIndexCacheInvalidatesWhenMetatableIndexChanges|RunDirectFrameDynamicIndex|StringFieldIndexPathsUseMetatableSemantics|CompilerUsesDynamicTableFieldCallOpcode|RunDirectFrameDynamicTableFieldCall)'`
- Table/direct-frame guard:
  `go test -count=1 ./... -run 'Test(Table|RunDirectFrame.*(Table|Dynamic|String|Row)|StringField|RowString|Compiler.*(Table|Dynamic|String|Row)|.*Metatable.*)'`
- Notes: the first token covers the existing string/meta guard surface. Array,
  generic-map, storage-mode, and explicit mutation-rule fields remain later
  Phase 22 pressure before Phase 23 PIC guards can use the full shape token.

### 2026-07-06 Table mutation version rules

- Current plan phase: Phase 22, Table Shape Tokens And Guarded Access; Slice
  22.2 mutation version rules.
- Behavior: nil deletes, inline string deletes, string-map promotion, array
  holes, generic-key mutation, and metatable assignment remain visible to stale
  table guards without changing public table semantics.
- Work: extended `tableShapeToken` with storage-mode, array, generic, and
  metatable guard categories; routed generic field mutation and metatable
  assignment through private `Table` helpers; and made array append/update,
  delete, hole filling, and sparse numeric promotion update the compact table
  layout/value generations.
- Focused guard:
  `go test -count=1 ./... -run 'Test(TableShapeToken|TableInlineStringFieldSlotsAreLayoutVersionGuarded|TableIndexCacheInvalidatesWhenMetatableIndexChanges|ScenarioEmberRunAllocationBudgets/event_dispatch)'`
- Table/metatable guard:
  `go test -count=1 ./... -run 'Test(TableShapeToken|TableInlineStringFieldSlotsAreLayoutVersionGuarded|TableIndexCacheInvalidatesWhenMetatableIndexChanges|CompileAndRunSetMetatable|Metatable|RawSet|RawGet|TableFastArray)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: array and generic token categories currently share the compact table
  layout/value generations rather than adding per-category counters to every
  table. A first attempt with dedicated counters preserved behavior but raised
  table-heavy Scenario allocation rows; finer counters should wait for a later
  PIC slice that proves the extra locality is worth the storage cost.

### 2026-07-06 Shape-aware raw and guarded table helpers

- Current plan phase: Phase 22, Table Shape Tokens And Guarded Access; Slice
  22.3 shape-aware raw and guarded helpers.
- Behavior: raw table reads/writes, ordinary metatable fallback, invalid-key
  checks, nil holes, string slots, and generic map fields preserve existing
  semantics while storage details stay behind `Table`.
- Work: added private helpers for raw dense-array value reads, raw generic-map
  field reads, and current-shape string slot reads. Migrated `rawget`,
  `rawGetKey`, direct-frame row string reads, direct constant-key stores,
  string-field predicate branches, and direct generic-map `__index` probes to
  call the table helper surface instead of peeking at `stringFields`,
  `stringFieldMap`, or `fields` from the VM.
- Focused guard:
  `go test -count=1 ./... -run 'Test(TableRawHelpersPreserveStorageSemantics|TableShapeToken|TableInlineStringFieldSlotsAreLayoutVersionGuarded|TableIndexCacheInvalidatesWhenMetatableIndexChanges|RunDirectFrame.*(Table|Dynamic|String|Row)|StringField|RowString|Compiler.*(Table|Dynamic|String|Row)|.*Metatable.*)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: the dense-array iterator still directly owns sequence traversal over
  table array storage. That path is mechanism-level iteration, not a VM
  field/index fast path, so it stays out of this helper migration until the
  iteration slices provide a better sequence interface.

### 2026-07-06 Row slot guard migration

- Current plan phase: Phase 22, Table Shape Tokens And Guarded Access; Slice
  22.4 row slot guard migration.
- Behavior: row-slot reads and writes still fall back when layout, slot, or
  metatable assumptions change, while static row slot facts can compose with
  later PIC/path/kind/predicate guards through one table-owned helper surface.
- Work: introduced a private `rowStringFieldSlotRef`, added table-owned row
  read/write helpers that resolve slot references through shape-tokened string
  storage and fall back by key, and migrated row string opcodes, row predicate
  descriptors, row sub/add descriptors, and slot-backed string predicate
  branches to those helpers. Bytecode operands and descriptors continue to
  carry compact slot indexes; the VM converts them to row slot references at
  execution.
- Focused guard:
  `go test -count=1 ./... -run 'Test(TableRowStringSlotReferenceFallsBackThroughShapeChanges|RunRowStringField|RunDirectFrameRowString|CompilerUsesRowString|CompileRunRowString|StringFieldBranch|RowStringField.*Branch|TruthyInAnd|NilInAnd|OptimizeBytecodeIRRemovesDeadLoadAroundRowString|InstructionRead.*RowString|InstructionWrite.*RowString)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: dynamic string caches and nested field slots still use explicit
  `tableStringFieldSlot` values because those caches own a concrete runtime
  token. Row facts are static compile-time slot hints, so they now cross the VM
  seam as row slot references rather than pretending to own a runtime token.

### 2026-07-06 Four-entry dynamic string PIC

- Current plan phase: Phase 23, Polymorphic Dynamic-Key And Handler Caches;
  Slice 23.1 four-entry dynamic string PIC.
- Behavior: direct-frame `table[key]` string-key reads preserve missing-key nil,
  invalid keys, metatable fallback, and host-visible mutation while avoiding
  single-entry cache thrash for small rotating keysets.
- Work: replaced the direct-frame dynamic string index cache's single entry
  with a four-entry private PIC. The cache seam exposes `get`, `store`, and
  `write`, and internally each read entry is guarded by table identity, key,
  shape token, and slot. Tests cover retaining four keys, evicting on a fifth,
  and rejecting stale entries after shape mutation.
- Focused guard:
  `go test -count=1 ./... -run 'Test(DynamicStringIndexCache|RunDirectFrameDynamicIndex|RunDirectFrameNestedStringFieldIndexPathsPreserveValues|StringFieldIndexPathsUseMetatableSemantics|Compiler.*Dynamic|.*Metatable.*)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: Phase 23.2 is responsible for explicitly covering and naming the
  write-PIC behavior around stable non-nil updates and nil deletes.

### 2026-07-06 Dynamic write PIC

- Current plan phase: Phase 23, Polymorphic Dynamic-Key And Handler Caches;
  Slice 23.2 dynamic write PIC.
- Behavior: direct-frame string-key writes preserve nil delete semantics,
  stale-shape invalidation, metatable fallback, and host-visible table mutation
  while keeping finite-key non-nil updates direct.
- Work: made the PIC write path explicit by renaming the cache write operation
  to `write` and covering four-key stable writes. PIC writes are guarded by
  table identity, key, shape token, and slot; nil writes and stale-shape writes
  miss the PIC and continue through the ordinary raw set/delete path.
- Focused guard:
  `go test -count=1 ./... -run 'TestDynamicStringIndexCache|TestRunDirectFrameDynamicIndexStorePreservesStringNumberAndNilKeys|TestRunDirectFrameDynamicIndexPreservesStringNumberAndMissingKeys'`
- Dynamic/table guard:
  `go test -count=1 ./... -run 'Test(DynamicStringIndexCache|RunDirectFrameDynamicIndex|RunDirectFrameNestedStringFieldIndexPathsPreserveValues|StringFieldIndexPathsUseMetatableSemantics|Compiler.*Dynamic|.*Metatable.*|TableShapeToken)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: this keeps the same compact four-entry storage introduced in 23.1.
  Handler call dispatch remains a separate single-entry cache until Slice 23.3.

### 2026-07-06 Handler table-field call PIC

- Current plan phase: Phase 23, Polymorphic Dynamic-Key And Handler Caches;
  Slice 23.3 handler table-field call PIC.
- Behavior: dynamic handler dispatch preserves missing handlers, non-functions,
  table mutation, metatable fallback, yielding calls, and errors while allowing
  one bytecode instruction to rotate through several stable handler keys.
- Work: replaced the direct-frame table-field call cache's single entry with a
  four-entry private handler PIC guarded by handler table identity, string key,
  value shape token, and closure. The PIC is held behind a lazy `vmFrame`
  pointer so programs without handler dispatch do not pay the larger frame
  allocation cost.
- Focused guard:
  `go test -count=1 ./... -run 'Test(TableFieldCallCache|CompilerUsesDynamicFieldCallOpcode|DynamicFieldCallSeesHandlerMutation|RunDirectFrameDynamicTableFieldCall|CompilerUsesDynamicTableFieldCallOpcode)'`
- Call/dynamic guard:
  `go test -count=1 ./... -run 'Test(.*Dynamic.*Call|.*Handler.*|TableFieldCallCache|CallTableField|Compiler.*Call|RunDirectFrame.*Call|.*Metatable.*)'`
- Allocation guard:
  `go test -count=1 ./... -run 'TestClassicEmberRunAllocationBudgets/recursive_fibonacci|Test(TableFieldCallCache|CompilerUsesDynamicFieldCallOpcode|DynamicFieldCallSeesHandlerMutation|RunDirectFrameDynamicTableFieldCall|CompilerUsesDynamicTableFieldCallOpcode)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: the first inline cache attempt stored all four handler entries inline
  on every `vmFrame` and tripped `recursive_fibonacci` allocation budgets. The
  final lazy pointer keeps the handler PIC deep without charging unrelated
  call-heavy programs.

### 2026-07-06 PIC miss accounting

- Current plan phase: Phase 23, Polymorphic Dynamic-Key And Handler Caches;
  Slice 23.4 PIC miss accounting.
- Behavior: normal execution is unchanged when counters are disabled. When a
  test or profiler opts in through private `vmThread` state, direct-frame PIC
  paths classify hits and fallback reasons without exposing public runtime API.
- Work: added private `directFramePICCounts` and counted cache helper variants
  for dynamic string reads/writes and handler table-field calls. The helpers
  record monomorphic hits, polymorphic hits, key misses, and stale shape/value
  misses; direct-frame dynamic index and handler paths record metatable exits,
  missing-key fallback, nil-write fallback, and non-string-key fallback.
- Focused guard:
  `go test -count=1 ./... -run 'Test(DynamicStringIndexCacheCountsHitsAndMisses|TableFieldCallCacheCountsHitsAndMisses|RunDirectFrameDynamicIndexPICCountsFallbackClasses)'`
- Dynamic/cache guard:
  `go test -count=1 ./... -run 'Test(.*Dynamic.*Index|.*StringFieldIndex|DynamicStringIndexCache|TableFieldCallCache|.*PICCounts.*|.*Handler.*|CallTableField|RunDirectFrame.*Call|.*Metatable.*)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: the counter seam is intentionally private and nil by default, matching
  the existing direct-frame opcode counters. This gives future cache-size and
  rejected-path decisions measured miss classes without making profiling part of
  Ember's external interface.

### 2026-07-06 Global environment version

- Current plan phase: Phase 24, Stable Global And Intrinsic Guards; Slice 24.1
  global environment version.
- Behavior: host globals passed through `RunWithGlobals` remain observable,
  assignments still update the host map, and internal base-global caching does
  not look like a script-visible write.
- Work: added a private `globalEnv.version` invalidation token. Base-only
  environments start at zero, host-override environments start in a distinct
  nonzero state, and every `globalEnv.set` increments the token after updating
  the local binding map.
- Focused guard:
  `go test -count=1 ./... -run 'TestGlobalEnvVersion'`
- Global/intrinsic guard:
  `go test -count=1 ./... -run 'Test(GlobalEnvVersion|RunWithGlobals.*Global|CompileAndRunAssignsGlobal|RunWithGlobalsCanOverride.*FastPath|CompilerUses.*Intrinsic|RunDirectFrame.*Intrinsic|RunDirectFrameRawLenGlobalPreservesValues)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: the token is not public API. It is the guard surface needed by later
  stable global and intrinsic descriptors.

### 2026-07-06 Intrinsic guard descriptors

- Current plan phase: Phase 24, Stable Global And Intrinsic Guards; Slice 24.2
  intrinsic guard descriptors.
- Behavior: existing intrinsic execution and override behavior is unchanged,
  while finalized artifacts now carry the stable identity needed for guarded
  lookup.
- Work: extended `intrinsicOpDesc` with base global name, field name, and native
  function identity, sourced from the base intrinsic registry. Disassembly facts
  now show that identity and stale descriptor verification compares it as part
  of the finalized artifact contract.
- Focused guard:
  `go test -count=1 ./... -run 'TestIntrinsicDescriptorsCarryGuardIdentity|TestCompilerUsesMathMinIntrinsicOpcode|TestBytecodeVerifierRejectsStaleIntrinsicDescriptors|TestCompilerUsesTableIntrinsicOpcodes'`
- Intrinsic/global guard:
  `go test -count=1 ./... -run 'Test(.*Intrinsic.*|.*FastPath|GlobalEnvVersion|RunWithGlobals.*Global|CompileAndRunAssignsGlobal|RunDirectFrameRawLenGlobalPreservesValues)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: runtime guard versions are intentionally not stored in the compile-time
  artifact. Later miss-path work should combine these descriptor facts with the
  current `globalEnv.version` and base table value token at execution time.

### 2026-07-06 Intrinsic guard miss path

- Current plan phase: Phase 24, Stable Global And Intrinsic Guards; Slice 24.3
  intrinsic guard miss path.
- Behavior: host overrides, script mutation of base library fields, restored
  native fields, and metatable-backed lookups remain visible to compiled
  intrinsic opcodes.
- Work: made `baseFieldIntrinsicCallee` the shared intrinsic guard/miss seam.
  It checks explicit own fields on overridden or cached base library tables
  directly, returns fast only when the native identity matches, and sends
  missing/metatable-backed fields through `runtimeTableAccess` so ordinary call
  semantics decide the result.
- Focused guard:
  `go test -count=1 ./... -run 'Test(BaseFieldIntrinsicCallee|CompileAndRunScriptMutationOverridesMathMinFastPath|RunWithGlobalsCanOverride.*FastPath|.*Intrinsic.*|RunDirectFrame.*Intrinsic|RunDirectFrameRawLenGlobalPreservesValues|GlobalEnvVersion)'`
- Allocation guard:
  `go test -count=1 ./... -run 'Test(BaseFieldIntrinsicCallee|CompileAndRunScriptMutationOverridesMathMinFastPath|Top10EmberRunAllocationBudgets/(array_ops|generic_iteration)|ClassicEmberRunAllocationBudgets/(iterative_fibonacci|recursive_fibonacci)|ScenarioEmberRunAllocationBudgets/(combat_tick|inventory_value|event_dispatch|buff_stack_tick|ability_resolution|ai_utility_scoring|cooldown_scheduler|projectile_sweep|quest_progress_update|behavior_tree_tick|threat_aggro_table|economy_market_tick|formation_layout_score|dialogue_condition_eval|procgen_room_scoring|save_state_diff|path_relaxation))'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: a persistent per-`globalEnv` intrinsic guard cache was rejected. The
  map-backed version allocated on hot intrinsic paths, and the inline fixed
  cache enlarged every environment enough to trip allocation budgets. Phase 24.4
  should hoist or reuse guards without adding per-run environment storage.

### 2026-07-06 Import guard hoisting

- Current plan phase: Phase 24, Stable Global And Intrinsic Guards; Slice 24.4
  import guard hoisting.
- Behavior: repeated intrinsic calls with unrelated host globals reuse a guarded
  base-import decision, while global writes and base library table mutation still
  change behavior immediately.
- Work: added a lazy active-thread intrinsic guard cache keyed by global name and
  field. The cache stores the current `globalEnv.version`, optional library table
  identity, table string-value token, and native callee. Absent host-global
  imports and native own-fields can hit the guard; metatable-backed and
  non-native fields still use the ordinary miss path.
- Focused guard:
  `go test -count=1 ./... -run 'TestBaseFieldIntrinsicCallee'`
- Intrinsic/allocation guard:
  `go test -count=1 ./... -run 'Test(BaseFieldIntrinsicCallee|CompileAndRunScriptMutationOverridesMathMinFastPath|RunWithGlobalsCanOverride.*FastPath|.*Intrinsic.*|RunDirectFrame.*Intrinsic|RunDirectFrameRawLenGlobalPreservesValues|GlobalEnvVersion|Top10EmberRunAllocationBudgets/(array_ops|generic_iteration)|ClassicEmberRunAllocationBudgets/(iterative_fibonacci|recursive_fibonacci)|ScenarioEmberRunAllocationBudgets/(combat_tick|inventory_value|event_dispatch|buff_stack_tick|ability_resolution|ai_utility_scoring|cooldown_scheduler|projectile_sweep|quest_progress_update|behavior_tree_tick|threat_aggro_table|economy_market_tick|formation_layout_score|dialogue_condition_eval|procgen_room_scoring|save_state_diff|path_relaxation))'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: adding another pointer to `vmThread` tripped the
  `iterative_fibonacci` allocation budget by 8 B/op. The final version reuses
  the optional direct-frame array-next counter slot, which had no current
  consumer, so the thread allocation shape remains flat for normal runs.

### 2026-07-06 One-segment path facts

- Current plan phase: Phase 25, Loop-Local Table Path Reuse; Slice 25.1
  one-segment path facts.
- Behavior: repeated `row.field` reads inside loops keep existing runtime
  behavior; this slice records the safe fact and verifier contract before adding
  runtime path-cache fallback.
- Work: added private finalized `pathFactDesc` artifacts for repeated
  one-segment string field reads in backward loops. Detection handles duplicate
  string constants and row-slot-backed field reads, and rejects loops containing
  table/global writes or calls. Disassembly now emits `path_fact` lines.
- Focused guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsLoopLocalOneSegmentPathFact|BytecodeVerifierRejectsStalePathFacts)'`
- Bytecode/finalizer guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsLoopLocalOneSegmentPathFact|BytecodeVerifierRejectsStalePathFacts|BytecodeFinalizer|BytecodeVerifier|OpcodeMetadata|InstructionRead|InstructionWrite|RunDirectFrameNestedStringField|RunDirectFrameRowString|CompilerUsesRowString|StringFieldBranch)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: runtime behavior is intentionally unchanged here. Slice 25.4 should
  use these facts at the fallback seam once alias invalidation has been made
  explicit.

### 2026-07-06 Two-segment path facts

- Current plan phase: Phase 25, Loop-Local Table Path Reuse; Slice 25.2
  two-segment path facts.
- Behavior: repeated `row.child.field` and `row.child[key]` reads keep current
  nil, metatable, and dynamic-key runtime behavior while the finalized artifact
  records reusable path candidates.
- Work: extended private `pathFactDesc` with optional second segment and dynamic
  key marker. Detection now recognizes compiler-emitted `GET_STRING_FIELD2` and
  `GET_STRING_FIELD_INDEX`, groups duplicate string constants by text, and emits
  disassembly facts such as `field child.value` and `field child dynamic_key`.
- Focused guard:
  `go test -count=1 ./... -run 'TestCompilerRecordsLoopLocal.*PathFact|TestBytecodeVerifierRejectsStalePathFacts'`
- Bytecode/finalizer guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsLoopLocal.*PathFact|BytecodeVerifierRejectsStalePathFacts|BytecodeFinalizer|BytecodeVerifier|OpcodeMetadata|InstructionRead|InstructionWrite|RunDirectFrameNestedStringField|RunDirectFrameDynamicIndex|RunDirectFrameRowString|CompilerUsesRowString|StringFieldBranch|StringFieldIndexPathsUseMetatableSemantics)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: this remains an artifact slice; Slice 25.3 will make alias/mutation
  rejection explicit before runtime path-cache fallback uses these facts.

### 2026-07-06 Path alias and mutation invalidation

- Current plan phase: Phase 25, Loop-Local Table Path Reuse; Slice 25.3 alias
  and mutation invalidation.
- Behavior: loops with calls or writes that can alter the path owner, subtable,
  metatable, or path value do not accept path facts silently.
- Work: added finalized private `pathFactRejectionDesc` artifacts. Path
  detection still records accepted one- and two-segment path facts for safe
  loops, but repeated candidates in unsafe loops now emit
  `path_fact_rejection` lines with conservative reasons such as `table write`,
  `global write`, or `call`.
- Focused guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsLoopLocal.*PathFact|BytecodeVerifierRejectsStalePathFact)'`
- Bytecode/finalizer guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsLoopLocal.*PathFact|BytecodeVerifierRejectsStalePathFact|BytecodeFinalizer|BytecodeVerifier|OpcodeMetadata|InstructionRead|InstructionWrite|RunDirectFrameNestedStringField|RunDirectFrameDynamicIndex|RunDirectFrameRowString|CompilerUsesRowString|StringFieldBranch|StringFieldIndexPathsUseMetatableSemantics)'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: rejection descriptors make unsafe path reuse auditable before Slice
  25.4 attaches runtime path-cache fallback to accepted load PCs.

### 2026-07-06 Runtime path cache fallback

- Current plan phase: Phase 25, Loop-Local Table Path Reuse; Slice 25.4 runtime
  path cache fallback.
- Behavior: bytecode and register liveness stay unchanged while repeated
  accepted static two-segment paths can reuse guarded runtime slots by load PC.
- Work: added a lazy active-thread runtime path cache for direct-frame
  `GET_STRING_FIELD2`. Cache entries guard the base table identity, first string
  slot, child table identity, and second string slot; any guard miss falls back
  to the existing raw/metatable path. Eligibility comes from finalized
  `path_fact` descriptors, including duplicate string-constant matching by text.
- Focused guard:
  `go test -count=1 ./... -run 'TestRunDirectFrameUsesRuntimePathCacheForTwoSegmentFieldPath'`
- Path/allocation guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsLoopLocal.*PathFact|RunDirectFrameUsesRuntimePathCacheForTwoSegmentFieldPath|BytecodeVerifierRejectsStalePathFact|RunDirectFrameNestedStringField|RunDirectFrameDynamicIndex|StringFieldIndexPathsUseMetatableSemantics|Top10EmberRunAllocationBudgets/(array_ops|generic_iteration)|ClassicEmberRunAllocationBudgets/(iterative_fibonacci|recursive_fibonacci)|ScenarioEmberRunAllocationBudgets/(combat_tick|inventory_value|event_dispatch|buff_stack_tick|ability_resolution|ai_utility_scoring|cooldown_scheduler|projectile_sweep|quest_progress_update|behavior_tree_tick|threat_aggro_table|economy_market_tick|formation_layout_score|dialogue_condition_eval|procgen_room_scoring|save_state_diff|path_relaxation))'`
- Full behavior guard: `go test -count=1 ./...`
- Notes: the cache reuses the existing lazy thread runtime guard storage rather
  than widening `vmThread` or `vmFrame`.

### 2026-07-06 Register and constant kind facts

- Current plan phase: Phase 26, Value-Kind And Predicate Fact System; Slice
  26.1 register and constant kind facts.
- Behavior: runtime behavior is unchanged while the finalized artifact records
  safe kind evidence for later branch and arithmetic consumers.
- Work: added private `constant_kind` and `register_kind` facts to the
  execution artifact finalizer. Register facts cover literal loads, moves,
  table literals, length results, proven numeric operations, simple comparison
  predicates, and guarded intrinsic result candidates. Propagation resets at
  control-flow boundaries, and unknown writes clear affected register facts via
  the existing instruction write model.
- Focused guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsRegisterAndConstantKindFacts|BytecodeVerifierRejectsStale(Constant|Register)KindFacts)' -v`
- Bytecode/finalizer guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsRegisterAndConstantKindFacts|BytecodeVerifierRejectsStale(Constant|Register)KindFacts|BytecodeFinalizer|BytecodeVerifierRejectsStale(Path|Intrinsic|Numeric|Entry))'`

### 2026-07-06 Slot and path kind facts

- Current plan phase: Phase 26, Value-Kind And Predicate Fact System; Slice
  26.2 slot and path kind facts.
- Behavior: runtime behavior is unchanged while guarded field-slot and
  reused-path kind evidence becomes visible in the finalized artifact.
- Work: added private `slot_kind` and `path_kind` facts. Slot facts come from
  table literal string-field writes and row-slot stores whose source register
  has a known kind. Nested accepted path facts now carry guarded table-kind
  evidence for their parent path, such as `row.child` in repeated
  `row.child.value` reads.
- Focused guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsSlotAndPathKindFacts|BytecodeVerifierRejectsStale(Slot|Path)KindFacts)'`
- Bytecode/finalizer guard:
  `go test -count=1 ./... -run 'Test(CompilerRecords(RegisterAndConstantKindFacts|SlotAndPathKindFacts|LoopLocal.*PathFact)|BytecodeVerifierRejectsStale(Constant|Register|Slot|Path|PathFact|PathFactRejections|Intrinsic|Numeric|Entry)|DisassembleProtoFactsShowsOptimizedArtifactShape)'`

### 2026-07-06 Predicate branch descriptors

- Current plan phase: Phase 26, Value-Kind And Predicate Fact System; Slice
  26.3 predicate branch descriptors.
- Behavior: runtime behavior is unchanged while branch sources are normalized
  into a descriptor vocabulary for later predicate refinement and branch
  consumers.
- Work: added private `predicate_branch` descriptors for register,
  string-field, row-field, row-field-pair, and path-field sources. Descriptors
  cover truthy, nil/not-nil, equality-to-constant, field equality, and numeric
  comparison operations. Path-field descriptors can recognize generic compare
  registers fed by accepted `GET_STRING_FIELD2` path facts.
- Focused guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsPredicateBranchDescriptors|BytecodeVerifierRejectsStalePredicateBranchDescriptors)'`
- Bytecode/finalizer guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsPredicateBranchDescriptors|CompilerRecords(RegisterAndConstantKindFacts|SlotAndPathKindFacts|LoopLocal.*PathFact)|BytecodeVerifierRejectsStale(Predicate|Constant|Register|Slot|Path|PathFact|PathFactRejections|Intrinsic|Numeric|Entry)|DisassembleProtoFactsShowsOptimizedArtifactShape)'`

### 2026-07-06 Branch and finite tag refinements

- Current plan phase: Phase 26, Value-Kind And Predicate Fact System; Slice
  26.4 finite tag, nil, and boolean refinement.
- Behavior: runtime behavior is unchanged while predicate descriptors now
  produce explicit branch-edge facts for later consumers.
- Work: added private `branch_refinement` and `finite_tag_refinement`
  artifacts. Branch refinements record fallthrough and target-edge facts for
  truthy/falsey, nil/not-nil, equality/negative-equality, field equality, and
  numeric comparison predicates. Finite tag refinements group repeated string
  equality checks for the same source in source order.
- Focused guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsBranchAndFiniteTagRefinements|BytecodeVerifierRejectsStale(Branch|FiniteTag)Refinements)'`
- Bytecode/finalizer guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsBranchAndFiniteTagRefinements|CompilerRecordsPredicateBranchDescriptors|CompilerRecords(RegisterAndConstantKindFacts|SlotAndPathKindFacts|LoopLocal.*PathFact)|BytecodeVerifierRejectsStale(Branch|FiniteTag|Predicate|Constant|Register|Slot|Path|PathFact|PathFactRejections|Intrinsic|Numeric|Entry)|DisassembleProtoFactsShowsOptimizedArtifactShape)'`

### 2026-07-06 Direct-frame side-exit contract

- Current plan phase: Phase 27, Direct-Frame Slow-Path Islands; Slice 27.1
  side-exit contract.
- Behavior: direct-frame fallback behavior is still routed through the existing
  generic frame when an opcode cannot complete directly, while direct-frame
  returns, yields, calls, and failures now share one private result contract.
- Work: added `directFrameSideExit` with resume, return, call, yield,
  generic-frame entry, and failure cases. `runDirectFrame` now returns that
  private result, and `runFrame` owns the adapter back to the existing
  frame-result loop.
- Focused contract guard:
  `go test -count=1 ./... -run TestDirectFrameSideExitContractMapsFrameResults`
- Direct-frame fallback guard:
  `go test -count=1 ./... -run 'Test(DirectFrameSideExitContractMapsFrameResults|StringFieldIndexPathsUseMetatableSemantics|RunRowStringFieldReadFallsBackAfterShapeChange|RunRowStringFieldStoreFallsBackToNewIndexAfterDelete|RunSelfUpvalueCallFallsBackAfterReassignment|RunDirectFrameDynamicIndexPICCountsFallbackClasses)'`

### 2026-07-06 Direct-frame table access islands

- Current plan phase: Phase 27, Direct-Frame Slow-Path Islands; Slice 27.2
  table access islands.
- Behavior: table-valued `__index` and `__newindex` misses can execute one
  bounded slow table operation and resume direct-frame execution at the next
  bytecode. Function-valued metamethods still enter the generic frame so calls
  and yields remain on the full VM path.
- Work: added private direct-frame table get/set island helpers for raw misses,
  missing keys, invalid keys, table-valued metatable chains, and cycle errors.
  `GET_FIELD`, `GET_STRING_FIELD`, `GET_ROW_STRING_FIELD`, `GET_INDEX`,
  `SET_FIELD`, `SET_STRING_FIELD`, `SET_ROW_STRING_FIELD`, and `SET_INDEX`
  now use the helpers when a bounded table fallback can complete locally.
- Focused island guard:
  `go test -count=1 ./... -run 'TestRunDirectFrameTableAccessIslandResumesAfter(Index|NewIndex|DynamicIndex|DynamicNewIndex)Metatable'`
- Semantic fallback guard:
  `go test -count=1 ./... -run 'Test(RunDirectFrameTableAccessIslandResumesAfter(Index|NewIndex|DynamicIndex|DynamicNewIndex)Metatable|StringFieldIndexPathsUseMetatableSemantics|RunRowStringFieldReadFallsBackAfterShapeChange|RunRowStringFieldStoreFallsBackToNewIndexAfterDelete|CompileAndRun(AddSubStringField2|AddStringField|SubStringField|SubAddStringField)OpcodeUsesMetatableSemantics|CompileAndRunMetatable(IndexFunctionRead|NewIndexTableFieldAssignment|NewIndexTableBracketAssignment)|CompileAndRunRuntimeTableAccessCallsFunctionValuedMetamethods|CompileAndRunMetatableIndexCycleReturnsError)'`
- Full behavior guard: `go test -count=1 ./...`

### 2026-07-06 Direct-frame intrinsic and fixed-call islands

- Current plan phase: Phase 27, Direct-Frame Slow-Path Islands; Slice 27.3
  intrinsic and fixed-call islands.
- Behavior: overridden fixed-result non-yielding intrinsic calls can execute one
  slow call island and resume direct-frame execution. Script, yieldable,
  protected, and open-result calls still enter the generic frame.
- Work: added a private non-yielding call island for native and ordinary host
  functions plus one fixed-result adjustment helper. `MATH_MIN` now uses it for
  one-result override misses. Table intrinsic override islands are limited to
  fixed positive result counts after regression tests proved open-result
  `table.insert` and `table.remove` calls must stay generic.
- Focused intrinsic guard:
  `go test -count=1 ./... -run TestRunDirectFrameIntrinsicIslandResumesAfterOverriddenMathMin`
- Override regression guard:
  `go test -count=1 ./... -run 'Test(RunDirectFrameIntrinsicIslandResumesAfterOverriddenMathMin|RunWithGlobalsCanOverrideTable(Remove|Insert)FastPath|RunWithGlobalsCanOverrideMathMinFastPath)'`
- Full behavior guard: `go test -count=1 ./...`

### 2026-07-06 Direct-frame side-exit counters

- Current plan phase: Phase 27, Direct-Frame Slow-Path Islands; Slice 27.4
  side-exit counters.
- Behavior: runtime behavior is unchanged when counters are disabled. When the
  existing direct-frame counter storage is enabled, side exits are counted by
  table, intrinsic, call, metatable, debug, budget, yield, error, and generic
  frame reasons.
- Work: added private side-exit reason vocabulary and side-exit slots to the
  existing opt-in direct-frame counter struct instead of widening `vmThread`.
  Resumable table and intrinsic islands count at the island point; generic
  frame, call, yield, and error exits count through the direct-frame side-exit
  result; debug and instruction-budget blocks count before the direct runner is
  skipped.
- Focused counter guard:
  `go test -count=1 ./... -run 'TestRunDirectFrameSideExitCountersRecord'`
- Island/counter guard:
  `go test -count=1 ./... -run 'Test(RunDirectFrameSideExitCountersRecord|RunDirectFrameTableAccessIslandResumesAfter(Index|NewIndex|DynamicIndex|DynamicNewIndex)Metatable|RunDirectFrameIntrinsicIslandResumesAfterOverriddenMathMin)'`
- Full behavior guard: `go test -count=1 ./...`

### 2026-07-06 Private value-list carrier

- Current plan phase: Phase 28, Call And Value-List Lifetime; Slice 28.1
  private value-list carrier.
- Behavior: visible return and assignment adjustment is unchanged for multiple
  returns, varargs, open-call expansion, protected calls, coroutine yield/resume,
  and host callbacks.
- Work: added private `vmValueList` with inline storage, optional slice, count,
  and borrowed ownership state. `vmFrameResult` now uses the carrier instead of
  its own parallel inline/slice fields, and raw call-result slices enter frame
  destinations as borrowed lists so open-call retention copies through one
  ownership seam.
- Focused carrier guard:
  `go test -count=1 ./... -run 'Test(VMValueListOwnsInlineBorrowedAndAdjustedValues|DirectFrameSideExitContractMapsFrameResults)'`
- Result lifetime guard:
  `go test -count=1 ./... -run 'Test(.*AllocationBudgets|VMValueListOwnsInlineBorrowedAndAdjustedValues|RunVarargWindowPreservesNilFillAndCount|CompileAndRunVararg|CompileAndRunMultipleReturnValues|CompileAndRunFinalVarargArgumentExpandsResults|RunWithGlobalsHostFunctionArgsAreIsolatedFromRegisterReuse)'`
- Full behavior guard: `go test -count=1 ./...`

### 2026-07-06 Borrowed fixed argument windows

- Current plan phase: Phase 28, Call And Value-List Lifetime; Slice 28.2
  borrowed fixed argument windows.
- Behavior: fixed script/native calls can borrow ordinary register windows,
  while captured-cell windows and host/unknown call boundaries still copy so
  retained host arguments cannot observe caller register reuse.
- Work: added private `vmFixedArgWindow` plus `borrowedFixedCallArgs` and
  `retainedFixedCallArgs`. Existing script/native fixed-call paths now name the
  borrowed-window policy explicitly, and host/unknown call paths use the
  retained-window helper.
- Focused guard:
  `go test -count=1 ./... -run 'Test(VMFrameFixedArgWindowsBorrowOnlySafeRegisters|RunWithGlobalsHostFunctionArgsAreIsolatedFromRegisterReuse|RunVarargWindowPreservesNilFillAndCount|CompileAndRunVararg|CompileAndRunFinalVarargArgumentExpandsResults|VMValueListOwnsInlineBorrowedAndAdjustedValues)'`
- Allocation guard:
  `go test -count=1 ./... -run 'Test(.*AllocationBudgets|VMFrameFixedArgWindowsBorrowOnlySafeRegisters|RunWithGlobalsHostFunctionArgsAreIsolatedFromRegisterReuse|VMFrameBorrowsVarargArgumentWindow|RunDirectFrameIntrinsicIslandResumesAfterOverriddenMathMin)'`
- Full behavior guard: `go test -count=1 ./...`

### 2026-07-06 Inline fixed results

- Current plan phase: Phase 28, Call And Value-List Lifetime; Slice 28.3
  inline fixed results.
- Behavior: fixed zero/one/two-result adjustment remains unchanged for direct
  returns, varargs, open-call destinations, protected calls, and coroutine
  pending calls.
- Work: added an inline-array `vmValueList` constructor and routed
  `applyInlineResultDestination` through the shared value-list destination
  helper, removing a second implementation of fixed/open result adjustment.
- Focused guard:
  `go test -count=1 ./... -run 'Test(VMInlineArrayValueListPreservesFixedResultCount|VMFrameAppliesDirectFixedResultDestinations|VMValueListOwnsInlineBorrowedAndAdjustedValues|CompileAndRunMultipleReturnValues|CompileAndRunVararg|CompileAndRunFinalVarargArgumentExpandsResults|CompileAndRunTablePackStoresVarargsAndCount|CompileAndRunSelect)'`
- Allocation guard:
  `go test -count=1 ./... -run 'Test(.*AllocationBudgets|VMInlineArrayValueListPreservesFixedResultCount|VMFrameAppliesDirectFixedResultDestinations|VMValueListOwnsInlineBorrowedAndAdjustedValues|RunVarargWindowPreservesNilFillAndCount|CompileAndRunMultipleReturnValues|CompileAndRunFinalVarargArgumentExpandsResults)'`
- Full behavior guard: `go test -count=1 ./...`

### 2026-07-06 Metamethod argument scratch

- Current plan phase: Phase 28, Call And Value-List Lifetime; Slice 28.4
  metamethod argument scratch.
- Behavior: metamethod argument order, errors, and fallback semantics remain
  unchanged, including host metamethods that retain their argument slices.
- Work: added fixed one/two/three-argument runtime metamethod helpers and
  replaced common short slice literals in table `__index`/`__newindex`, unary,
  binary, length, concat, and conversion metamethod paths.
- Focused guard:
  `go test -count=1 ./... -run 'Test(RuntimeMetamethodScratchPreservesRetainedHostArguments|CompileAndRunMetatable|RuntimeTableAccessCallsFunctionValuedMetamethods|Metamethod|Concat|Length|Arithmetic)'`
- Allocation guard:
  `go test -count=1 ./... -run 'Test(.*AllocationBudgets|RuntimeMetamethodScratchPreservesRetainedHostArguments|CompileAndRunMetatable|RuntimeTableAccessCallsFunctionValuedMetamethods)'`
- Full behavior guard: `go test -count=1 ./...`

### 2026-07-06 Kind-proven numeric fast paths

- Current plan phase: Phase 29, Numeric And Reduction Block Plans; Slice 29.1
  kind-proven numeric fast paths.
- Behavior: public arithmetic and comparison behavior is unchanged. Numeric
  operand proofs skip repeated direct-frame `Value.kind` checks only where the
  existing value-kind propagation proves unguarded number operands; NaN
  ordered comparisons still side-exit to the ordinary error path.
- Work: added explicit `numeric_operand` facts, verifier/finalizer checks for
  stale numeric operand descriptors, and a private per-PC fact map consumed by
  direct-frame arithmetic, constant arithmetic, unary negation, and ordered
  comparisons.
- Focused numeric guard:
  `go test -count=1 ./... -run 'TestCompilerRecordsNumericOperandFactsForProvenNumbers|TestKindProvenNumericComparisonStillFallsBackForNaN|TestBytecodeVerifierRejectsStaleNumericOperandFacts'`
- Behavior/allocation guard:
  `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestCompilerRecordsNumericOperandFactsForProvenNumbers|TestKindProvenNumericComparisonStillFallsBackForNaN|TestBytecodeVerifierRejectsStaleNumericOperandFacts'`
- Full behavior guard: `go test -count=1 ./...`
- Lane guards: `scripts/check-lane root`; `scripts/check-fast`.

### 2026-07-06 Reduction descriptor checkpoints

- Current plan phase: Phase 29, Numeric And Reduction Block Plans; Slice 29.2
  general reduction descriptors, completed descriptor surface.
- Behavior: no runtime behavior change intended. The accepted max/best
  descriptor records move-only branch bodies after a proven `candidate >
  accumulator` register predicate. The accepted all-complete descriptor records
  a predicate-controlled `complete = false` mutation only when the branch body
  has no calls, table stores, nested branches, or other side effects. The
  accepted absolute-delta descriptor records `delta = -delta` after a
  `delta < 0` predicate. The accepted paired-row diff descriptor records an
  iterator-produced left row and index, paired `right = after[i]` lookup,
  same-field row reads, and subtraction; mutation and aliasing variants remain
  ordinary bytecode.
- Work: added private `reduction` facts for max/best assignments, best-index
  style extra move mutations, all-complete false assignments, and absolute
  delta normalization, plus paired-row diffs. Finalizer/verifier/disassembly
  coverage rejects stale descriptor state, all-complete call bodies, paired-row
  mutation before diff, and obvious paired-row aliasing.
- Focused descriptor guard:
  `go test -count=1 ./... -run 'TestCompilerRecords(Max|AllComplete|AbsoluteDelta|PairedRowDiff)ReductionFacts|TestCompilerRejects(AllCompleteReductionWithCallInMutationBody|PairedRowDiffReductionAfterPairMutation|PairedRowDiffReductionWhenRowsMayAlias)|TestBytecodeVerifierRejectsStaleReductionFacts|TestCompilerUsesRegisterNumericGreaterBranch|TestScenarioDisassemblySnapshotsShowCurrentArtifactShape'`
- Behavior/allocation guard:
  `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestCompilerRecords(Max|AllComplete|AbsoluteDelta|PairedRowDiff)ReductionFacts|TestCompilerRejects(AllCompleteReductionWithCallInMutationBody|PairedRowDiffReductionAfterPairMutation|PairedRowDiffReductionWhenRowsMayAlias)|TestBytecodeVerifierRejectsStaleReductionFacts'`
- Full behavior guard: `go test -count=1 ./...`
- Lane guard: `scripts/check-lane root`.
- Notes: Slice 29.2 completes the reduction descriptor surface. Applying these
  descriptors to reduce dispatch or repeated table work belongs to Slice 29.3
  direct block plans.

### 2026-07-06 Direct numeric/reduction block plans

- Current plan phase: Phase 29, Numeric And Reduction Block Plans; Slice 29.3
  direct block plans, numeric/reduction, paired field-read, row store, and
  branch-ending row set/arithmetic/sub-add store checkpoints.
- Behavior: direct-frame absolute-delta regions preserve the existing branch
  behavior and resume at the original bytecode target. Negative deltas execute
  the planned negation without dispatching `NEG` or the trailing `JUMP`;
  non-negative deltas resume after the skipped mutation. Max/best reduction
  branches preserve the existing numeric/NaN fallback behavior while executing
  move-only mutation bodies and resuming at the branch target without
  dispatching the body `MOVE`s or trailing `JUMP`. Paired-row diff regions
  execute the paired `GET_INDEX`, two guarded row-field reads, and subtraction
  as one direct block plan, resuming at the next bytecode. Row-field add-store
  regions keep the existing `ADD_STRING_FIELD` bytecode shape while consuming a
  verified row-slot direct block plan for raw row reads and writes. Simple
  branch-ending row stores such as numeric row-field clamps execute the branch
  predicate, constant load, and row store inside one guarded direct block plan;
  arithmetic store-back bodies such as `row.field = row.field - constant`
  reuse the same plan family when row-slot evidence is available. Same-row
  sub-add bodies such as `row.hp = row.hp - incoming + row.regen` now use the
  same branch-store plan family when their descriptor slots are verified.
- Work: added private `direct_block_plan` descriptors and a per-PC direct
  block plan map, with finalizer/verifier/disassembly coverage. The direct
  runner consumes the `absolute_delta` plan at `JUMP_IF_NOT_LESS_K` and the
  `max` plan at `JUMP_IF_NOT_GREATER`, and consumes `paired_row_diff` at
  `GET_INDEX`. The compiler now preserves row-slot evidence on
  `ADD_STRING_FIELD`, the finalizer records `row_field_add_store` plans, and
  the direct runner consumes them at the existing store-back opcode. The
  finalizer also records `row_field_branch_store` plans for single
  branch-ending row-field set/add/sub/sub-add store-backs, and the direct
  runner consumes them at row numeric branch opcodes.
- Focused block-plan guard:
  `go test -count=1 ./... -run 'TestRunDirectFrame(UsesRowFieldBranchSubAddStoreBlockPlan|UsesRowFieldBranchArithmeticStoreBlockPlan|UsesRowFieldBranchStoreBlockPlan|UsesRowFieldAddStoreBlockPlan|UsesPairedRowDiffBlockPlan|UsesMaxReductionBlockPlan|MaxReductionBlockPlanResumesAfterSkippedMutation|UsesAbsoluteDeltaBlockPlan|AbsoluteDeltaBlockPlanResumesAfterSkippedMutation)|TestBytecodeVerifierRejectsStaleDirectBlockPlans'`
- Behavior/allocation guard:
  `go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets|TestRunDirectFrame(UsesRowFieldBranchSubAddStoreBlockPlan|UsesRowFieldBranchArithmeticStoreBlockPlan|UsesRowFieldBranchStoreBlockPlan|UsesRowFieldAddStoreBlockPlan|UsesPairedRowDiffBlockPlan|UsesMaxReductionBlockPlan|MaxReductionBlockPlanResumesAfterSkippedMutation|UsesAbsoluteDeltaBlockPlan|AbsoluteDeltaBlockPlanResumesAfterSkippedMutation)|TestBytecodeVerifierRejectsStaleDirectBlockPlans'`
- Full behavior guard: `go test -count=1 ./...`
- Lane guard: `scripts/check-lane root`.
- Remaining Slice 29.3 work: wider branch-ending table regions beyond single
  row-field set/add/sub/sub-add store-backs.

### 2026-07-06 Under-2x mechanism counter checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 0, Measurement Before
  Motion; Slice 0.1 mechanism counters.
- Behavior: normal execution is unchanged when counters are disabled. With the
  existing private direct-frame counter storage enabled, direct block plans now
  count entries, direct resumes, fallbacks, and fallback reasons without naming
  Scenario rows or field names.
- Work: extended `directFramePICCounts` with nil-safe direct block mechanism
  counters and wired the current direct block plan families at their direct VM
  entry points. Added tracers for row add-store block resumes and numeric-string
  fallback through ordinary Luau arithmetic semantics.
- Focused counter guard:
  `go test -count=1 ./... -run 'TestRunDirectFrameDirectBlockPlanCounters|TestRunDirectFrameDirectBlockPlanCountersRecordFallbackReason'`
- Next Slice 0.1 pressure: broader mechanism counters still need path cache,
  fixed-call frame lifetime, and intrinsic/global guard attribution before the
  under-2x plan's measurement phase is complete.

### 2026-07-06 Under-2x runtime path counter checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 0, Measurement Before
  Motion; Slice 0.1 mechanism counters.
- Behavior: normal execution is unchanged when counters are disabled. With
  private direct-frame counters enabled, runtime path caches now attribute
  stores, first misses, hits, and stale guard misses for static and dynamic
  two-segment paths.
- Work: added nil-safe path cache mechanism counters to `directFramePICCounts`
  and wired the existing static and dynamic runtime path cache helpers. Existing
  cache-local stats remain in place, but measurement no longer has to inspect
  the intrinsic guard cache internals.
- Focused counter guard:
  `go test -count=1 ./... -run 'TestRunDirectFrameUsesRuntimePathCacheForTwoSegment(Field|Dynamic)Path|TestRuntimePathCacheCountersRecordStaleGuard'`
- Next Slice 0.1 pressure: fixed-call frame lifetime counters and
  intrinsic/global guard attribution.

### 2026-07-06 Under-2x intrinsic/global guard counter checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 0, Measurement Before
  Motion; Slice 0.1 mechanism counters.
- Behavior: normal execution is unchanged when counters are disabled. With
  private direct-frame counters enabled, base-field intrinsic/global guards now
  attribute checks, hits, and misses through the same mechanism counter storage
  as direct blocks and path caches.
- Work: added nil-safe intrinsic guard counters to `directFramePICCounts` and
  wired the existing base-field intrinsic guard lookup. The guard cache still
  owns its local entries, version checks, shape-token checks, and resolution
  count; the mechanism counter only observes check outcomes.
- Focused counter guard:
  `go test -count=1 ./... -run TestBaseFieldIntrinsicCalleeHoistsAbsentHostGlobalGuard`
- Next Slice 0.1 pressure: fixed-call frame lifetime counters remain before
  the measurement phase has coverage for all planned mechanism classes.

### 2026-07-06 Under-2x fixed-call frame counter checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 0, Measurement Before
  Motion; Slice 0.1 mechanism counters.
- Behavior: normal execution is unchanged when counters are disabled. With
  private direct-frame counters enabled, fixed script-call frame lifetime now
  attributes direct-leaf reusable frame entries, full-frame materializations,
  argument copies, and register copies during direct-leaf side-exit
  materialization.
- Work: added nil-safe fixed-call counters to `directFramePICCounts`, counted
  the direct-leaf reusable register frame path, routed ordinary call-created
  frames through a private `newCallFrame` helper, and counted direct-leaf
  fallback materialization separately from top-level script startup.
- Focused counter guard:
  `go test -count=1 ./... -run 'TestRunDirectLeafCallOnePreservesSemantics|TestRunDirectLeafCallOneCountersRecordReusableFrame|TestRunDirectLeafCallOneCountersRecordFallbackMaterialization'`
- Slice 0.1 pressure after this checkpoint: planned mechanism counter classes
  now have private opt-in coverage; benchmark attribution still needs a fresh
  measurement sweep before choosing the next under-2x implementation slice.

### 2026-07-06 Under-2x attribution sweep

- Current plan phase: Scenario Under 2x Speed Work; Phase 0, Measurement Before
  Motion; Slice 0.1 mechanism counters.
- Behavior: no public runtime interface changes. Added a private
  `runWithDirectFrameMechanismCounters` runner that enables direct-frame opcode
  and mechanism counters for tests and ledger work, then returns an internal
  snapshot grouped by mechanism class.
- Ratio sweep:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./... | tee /tmp/ember-under2x-phase0-scenario.out`
- Gate proof:
  `SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-under2x-phase0-scenario.out`
  passed with all seventeen rows present.
- Red gate proof:
  `SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate < /tmp/ember-under2x-phase0-scenario.out`
  exited `1`.
- Focused attribution guard:
  `go test -count=1 ./... -run TestScenarioMechanismAttributionCoversCurrentWorstRows -v`
- Current worst rows by this short sweep:

| Case | Ember ns/op | Luau ns/run | Ratio | Mechanism attribution |
| --- | ---: | ---: | ---: | --- |
| event_dispatch | 70757 | 14878 | 4.76x | `CALL_TABLE_FIELD_KEY_ONE=250`, fixed-call reuse `250`, fixed arg copies `500`, table-call PIC hits/misses, direct blocks `150/150/0` |
| economy_market_tick | 327576 | 73563 | 4.45x | dynamic string field index ops dominate, PIC hits/misses, intrinsic guard checks all miss `324/0/324` |
| cooldown_scheduler | 166349 | 40481 | 4.11x | field dispatch dominates, direct blocks `107/107/0`, no fixed-call or path-cache activity |
| path_relaxation | 121292 | 30902 | 3.93x | row-field dispatch and array iteration dominate; invalid-key fallback count `456` |

- Next pressure: Phase 1 verified plan shell is still the right next seam,
  because later table/path/call/intrinsic work needs a common side-exit and
  attribution contract instead of more one-off direct-frame plan plumbing.

### 2026-07-06 Under-2x verified-plan shell checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 1, Verifier And
  Side-Exit Skeleton; Slice 1.1 verified plan shell.
- Behavior: existing direct block plan behavior is unchanged. The VM still
  resumes direct execution or falls back to ordinary dispatch with the same
  counters and Luau-visible results.
- Work: added private `verifiedPlanDesc`, `verifiedPlanCandidate`, and
  verified-plan rejection descriptors. The execution artifact now finalizes
  verified plans and a per-PC verified plan map from existing direct block
  plans; `verifyProto` rejects stale verified-plan state. The direct-frame VM
  consumes existing direct block families through `verifiedDirectBlockPlanAt`
  instead of reading `directBlockPlanAt` directly.
- Red tracers:
  `go test -count=1 ./... -run 'TestRunDirectFrameDirectBlockPlanCounters|TestBytecodeVerifierRejectsStaleVerifiedPlans'`
  initially failed because `Proto` had no verified-plan shell.
- Focused shell guard:
  `go test -count=1 ./... -run 'TestRunDirectFrameDirectBlockPlanCounters|TestBytecodeVerifierRejectsStaleVerifiedPlans|TestBytecodeVerifierRejectsStaleDirectBlockPlans'`
- Phase guard:
  `go test -count=1 ./... -run 'Direct|Block|SideExit|BenchmarksMatch'`
- Next Slice 1.1 pressure: the shell exists and is consumed for existing block
  families; Slice 1.2 still needs explicit side-exit contract tests for PC and
  register preservation around rejected plans, budget/debug behavior, and
  unsafe call/yield regions.

### 2026-07-06 Under-2x verified-plan side-exit contract checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 1, Verifier And
  Side-Exit Skeleton; Slice 1.2 side-exit contract tests.
- Behavior: existing direct block results and counters are unchanged. Verified
  plan fallback now has one executor seam that restores the original PC and
  leaves registers/row storage resumable for ordinary dispatch.
- Work: added private `executeVerifiedPlan` and `executeVerifiedDirectBlockPlan`
  helpers. `runDirectFrame` now checks `verifiedPlanAt` before opcode dispatch,
  and all existing direct block families route through the common executor.
  Removed per-op verified block plumbing from the switch. `verifyRegion` now
  rejects unknown direct block kinds plus call/yield-risk and return opcodes
  before a candidate can become a verified plan.
- Red tracers:
  `go test -count=1 ./... -run TestExecuteVerifiedPlanFallbackPreservesPCAndRegisters`
  initially failed because `vmThread` had no `executeVerifiedPlan` seam.
  `go test -count=1 ./... -run TestVerifyRegionRejectsCallRisk` initially
  failed because `verifyRegion` accepted a call-risk region.
- Focused contract guard:
  `go test -count=1 ./... -run 'TestRunDirectFrame(UsesRowFieldBranchSubAddStoreBlockPlan|UsesRowFieldBranchArithmeticStoreBlockPlan|UsesRowFieldBranchStoreBlockPlan|UsesRowFieldAddStoreBlockPlan|UsesPairedRowDiffBlockPlan|UsesMaxReductionBlockPlan|MaxReductionBlockPlanResumesAfterSkippedMutation|UsesAbsoluteDeltaBlockPlan|AbsoluteDeltaBlockPlanResumesAfterSkippedMutation|DirectBlockPlanCounters|DirectBlockPlanCountersRecordFallbackReason)|TestBytecodeVerifierRejectsStale(VerifiedPlans|DirectBlockPlans)|TestVerifyRegionRejectsCallRisk|TestExecuteVerifiedPlanFallbackPreservesPCAndRegisters'`
- Phase guard:
  `go test -count=1 ./... -run 'Direct|Block|SideExit|BenchmarksMatch'`
- Next pressure: Phase 2 can start shaping table epochs against the verified
  plan shell; future side-exit tests should expand as each new plan family
  introduces new guard or register repair behavior.

### 2026-07-06 Under-2x table epoch split checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 2, Table Shape And
  Storage Locality; Slice 2.1 split table epochs.
- Behavior: no public runtime interface changes. The VM still asks tables for a
  guarded shape token, but the token now carries independent string, array, and
  generic layout/value epochs behind the private `Table` seam.
- Work: split array and generic epoch counters away from string field versions.
  Array writes, holes, appends, and contiguous promotion now invalidate array
  descriptors; generic add/update/delete invalidates generic descriptors;
  string field updates, deletes, and promotion remain string-only.
- Red tracer:
  `go test -count=1 ./... -run TestTableShapeTokenKeepsUnrelatedEpochsIndependent`
  initially failed because an array value update changed the string and generic
  value token.
- Focused guard:
  `go test -count=1 ./... -run 'TestTableShapeToken(KeepsUnrelatedEpochsIndependent|TracksMutationVersionRules|SplitsStringLayoutAndValueChanges)|TestTableInlineStringFieldSlotsAreLayoutVersionGuarded'`
- Phase guard:
  `go test -count=1 ./... -run 'Table|rawget|rawset|metatable|BenchmarksMatch'`
- Next pressure: Slice 2.2 can add finite-key guarded slots on top of the same
  private descriptor interface without making the VM depend on table storage
  internals.

### 2026-07-06 Under-2x verified-plan regression correction

- Trigger: a full relaunch comparison reported broad normal-run slowdown after
  the verified-plan shell and table epoch split work. The slow sample included
  scalar rows, table-heavy rows, and Scenario rows such as
  `formation_layout_score`, `save_state_diff`, and `ability_resolution`.
- Diagnosis: the scalar regression came from checking `verifiedPlanAt` before
  every direct-frame opcode. A no-plan gate and inlined lookup recovered those
  rows. Profiles and falsification then showed the remaining Scenario slowdown
  was not `shapeToken()`; `executeVerifiedPlan` dominated
  `formation_layout_score` and `save_state_diff`, and disabling verified-plan
  execution recovered the rows.
- Correction: normal direct-frame runs no longer execute verified direct-block
  plans. The verified-plan metadata and executor remain opt-in through the
  private PIC/mechanism counter instrumentation path, so attribution and
  contract tests can still exercise the shell without taxing ordinary `Run`.
- Regression guard:
  `go test -count=1 ./... -run 'TestRunDirectFrameVerifiedPlansArePICOptIn|TestRunDirectFrame(UsesRowFieldBranchSubAddStoreBlockPlan|UsesRowFieldBranchArithmeticStoreBlockPlan|UsesRowFieldBranchStoreBlockPlan|UsesRowFieldAddStoreBlockPlan|UsesPairedRowDiffBlockPlan|UsesMaxReductionBlockPlan|MaxReductionBlockPlanResumesAfterSkippedMutation|UsesAbsoluteDeltaBlockPlan|AbsoluteDeltaBlockPlanResumesAfterSkippedMutation|DirectBlockPlanCounters|DirectBlockPlanCountersRecordFallbackReason)|TestExecuteVerifiedPlanFallbackPreservesPCAndRegisters|TestVerifyRegionRejectsCallRisk'`
- Focused benchmark proof:
  `go test -run '^$' -bench 'BenchmarkTop10Luau/(arithmetic_for|array_ops|generic_iteration)/ember_run$|BenchmarkClassicLuau/(iterative_fibonacci|recursive_fibonacci)/ember_run$|BenchmarkScenarioLuau/(formation_layout_score|save_state_diff|ability_resolution|economy_market_tick)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Healthy post-correction samples:
  `ability_resolution` `36240-36284 ns/op`, `formation_layout_score`
  `463523-468137 ns/op`, `save_state_diff` `194604-195052 ns/op`,
  `arithmetic_for` `6431-6446 ns/op`, with previous allocation budgets restored
  (`arithmetic_for` back to `308 B/op`).

### 2026-07-06 Under-2x finite-key map slot checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 2, Table Shape And
  Storage Locality; Slice 2.2 finite-key guarded slots.
- Behavior: no public runtime interface changes. Map-backed string fields now
  expose the same private guarded descriptor interface as inline string fields.
- Work: `Table.rawStringFieldSlot`, `rawStringFieldAtSlot`, and
  `setRawStringFieldAtSlot` now handle promoted string maps. The direct-frame
  dynamic string index cache stores the descriptor as a `tableStringFieldSlot`
  and performs reads/writes through the `Table` seam instead of inspecting
  inline storage directly.
- Red tracers:
  `go test -count=1 ./... -run TestTableMapStringFieldSlotsAreLayoutVersionGuarded`
  initially failed because map-backed string fields had no slot descriptor.
  `go test -count=1 ./... -run TestDynamicStringIndexCacheUsesMapBackedSlots`
  initially failed because the cache rejected map-backed descriptors.
- Focused guard:
  `go test -count=1 ./... -run 'Table|rawget|rawset|metatable|BenchmarksMatch|DynamicStringIndexCache'`
- Benchmark guard:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(economy_market_tick|dialogue_condition_eval|threat_aggro_table|save_state_diff|behavior_tree_tick|path_relaxation)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=3 ./...`
- Post-slice note: this slice mainly removes storage-specific cache blind spots.
  The 500ms row samples passed but remain noisy; later path/fact slices still
  need to use these descriptors to move the broader ratios materially.

### 2026-07-06 Under-2x fact-kill descriptor checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 3, Fact Lifetime And
  Loop-Local Reuse; Slice 3.1 fact-kill rules.
- Behavior: no runtime semantic changes. Existing conservative path fact
  rejection remains in place for table writes, global writes, and calls.
- Work: `pathFactRejectionDesc` now records first repeated fact birth PC, kill
  class, kill PC, fallback PC, and reason. Disassembly exposes these private
  descriptors so future table/path/intrinsic plan consumers can explain why a
  fact was killed without knowing the underlying table or path implementation.
- Red tracer:
  `go test -count=1 ./... -run 'TestCompilerRecordsLoopLocalPathFactRejectionFor(TableWrite|Call)'`
  initially failed because rejection lines only included the loop range and
  coarse reason.
- Focused guard:
  `go test -count=1 ./... -run 'PathFact|FactRejection|BranchRefinement|Predicate|Verified|BenchmarksMatch'`
- Next pressure: Slice 3.2 should turn these explanations into reusable
  loop-local descriptors rather than rediscovering the same fact inside each
  path cache or future plan family.

### 2026-07-06 Under-2x loop-local reuse descriptor checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 3, Fact Lifetime And
  Loop-Local Reuse; Slice 3.2 loop-local reuse rules.
- Behavior: no runtime semantic changes. Existing runtime path cache behavior
  and conservative rejection rules remain unchanged.
- Work: accepted `pathFactDesc` entries now record birth PC, backedge PC,
  fallback PC, and `kill none`, matching the kill metadata added to rejected
  facts. This gives future path plans a single verifier-owned lifetime
  descriptor instead of per-opcode lifetime inference.
- Red tracer:
  `go test -count=1 ./... -run TestCompilerRecordsLoopLocalTwoSegmentFieldPathFact`
  initially failed because accepted path facts only exposed loop range, base,
  field, and hit count.
- Focused guard:
  `go test -count=1 ./... -run 'Fact|Loop|Path|Direct|BenchmarksMatch'`
- Next pressure: Phase 4 can consume these descriptors in a path plan module
  for shared read/write/read-modify-write path access.

### 2026-07-06 Under-2x path plan artifact checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 4, Composed Path
  Access; Slice 4.1 path plan module.
- Behavior: no runtime semantic changes yet. Existing path cache execution is
  unchanged.
- Work: added private `pathPlanDesc` entries derived from loop-local path facts.
  Read plans record PC, access kind, loop range, base register, static
  parent/child fields or dynamic key source, and fallback PC. `verifyProto`
  rejects stale path-plan state and disassembly exposes `path_plan` lines for
  future direct-frame consumers.
- Red tracers:
  `go test -count=1 ./... -run TestCompilerRecordsReadPathPlanForLoopLocalTwoSegmentFieldPath`
  initially failed because no path-plan artifact existed.
  `go test -count=1 ./... -run TestBytecodeVerifierRejectsStalePathPlans`
  protects the finalized artifact contract.
- Focused guard:
  `go test -count=1 ./... -run 'Path|Plan|Direct|BenchmarksMatch'`
- Next pressure: Slice 4.2 should route reads, writes, and read-modify-write
  path operations through this plan module instead of letting opcodes rediscover
  parent/child path pieces independently.

### 2026-07-06 Under-2x shared path-plan checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 4, Composed Path
  Access; Slice 4.2 shared read, write, and read-modify-write paths.
- Behavior: no public runtime interface changes. Runtime execution can consume
  the new write and read-modify-write path plans through opt-in mechanism
  counters or an already-allocated bounded path cache. Ordinary runs do not
  allocate a path cache solely because a path plan exists.
- Work: widened private `pathPlanDesc` generation from loop-local read facts to
  static two-segment reads/writes, dynamic-key two-segment reads/writes, and
  the existing nested add/sub read-modify-write opcode. Path plans now carry a
  value source for writes and record the target RMW path plus operand read paths.
  The runtime path cache is now keyed by PC plus path instead of only PC, so a
  single RMW opcode can hold target/add/sub entries at the same bytecode PC.
- Red tracers:
  `go test -count=1 ./... -run TestCompilerRecordsWritePathPlanForTwoSegmentFieldPath`
  initially failed because standalone writes were still tied to loop path facts.
  `go test -count=1 ./... -run TestCompilerRecordsReadModifyWritePathPlanForTwoSegmentFieldPath`
  initially failed because `ADD_SUB_STRING_FIELD2` had no path-plan descriptor.
  `go test -count=1 ./... -run 'TestRunDirectFrameUsesRuntimePathCacheForTwoSegment(FieldPathWrite|DynamicPathWrite|ReadModifyWrite)'`
  initially failed because write and RMW opcodes did not consume the path cache.
- Focused guard:
  `go test -count=1 ./... -run 'Path|Dynamic|metatable|BenchmarksMatch|VerifiedPlansArePICOptIn'`
- Benchmark guard:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(economy_market_tick|threat_aggro_table|save_state_diff|dialogue_condition_eval|path_relaxation|behavior_tree_tick|cooldown_scheduler)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=3 ./...`
- Allocation probe: eager normal-run path-plan cache allocation regressed
  scenario rows (`economy_market_tick` jumped to about `710000 ns/op` and added
  one allocation). The consumer was narrowed back to opt-in/already-allocated
  cache use; follow-up samples restored `economy_market_tick` to about
  `361000-367000 ns/op`, `save_state_diff` to about `210000-211000 ns/op`, and
  the previous allocation counts.

### 2026-07-06 Under-2x typed block-plan IR checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 5, Typed
  Direct-Frame Superblock Families; Slice 5.1 private block plan IR.
- Behavior: normal `Run` remains unchanged. Verified direct-block execution is
  still opt-in through the private instrumentation/PIC seam.
- Work: added private `blockPlanKind` and `blockPlanDesc` artifacts derived
  from existing direct-block plans. Finalized prototypes now expose a
  `blockPlanAt(pc)` PC map, `verifyProto` rejects stale block-plan state, and
  disassembly emits typed `block_plan` lines with family, start/resume, and
  fallback PCs. The VM's verified direct-block executor now routes through a
  typed block-plan switch instead of dispatching on string plan kinds.
- Red tracers:
  `go test -count=1 ./... -run TestCompilerRecordsTypedBlockPlanForAbsoluteDelta`
  initially failed because no typed block-plan artifact existed.
  `go test -count=1 ./... -run TestBytecodeVerifierRejectsStaleBlockPlans`
  protects the finalized artifact contract.
- Focused guard:
  `go test -count=1 ./... -run 'Direct|Block|SideExit|BenchmarksMatch'`
- Benchmark guard:
  `go test -run '^$' -bench 'BenchmarkTop10Luau/(arithmetic_for|array_ops|generic_iteration)/ember_run$|BenchmarkClassicLuau/(iterative_fibonacci|recursive_fibonacci)/ember_run$|BenchmarkScenarioLuau/(formation_layout_score|save_state_diff|ability_resolution|economy_market_tick)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=2 ./...`
- Post-checkpoint note: the sample was noisy but did not show the allocation
  regression pattern from the earlier path-cache probe; scalar and scenario
  allocation counts stayed at their prior levels.

### 2026-07-06 Under-2x dynamic path add-store block checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 5, Typed
  Direct-Frame Superblock Families; Slice 5.2 first global block families.
- Behavior: normal direct-frame execution now consumes the first typed
  non-legacy block family, but only from the `GET_STRING_FIELD_INDEX` opcode
  case. There is no global per-opcode block-plan probe.
- Work: added `dynamic_path_add_store` block plans for repeated dynamic
  two-segment read plus numeric add/sub plus write-back sequences. The detector
  uses path-plan evidence and matches duplicated string constants by value.
  The executor reads the parent/child path, checks string keys and numeric
  operands, writes back before resuming, and side-exits before mutation for
  metatables, nil/missing paths, non-table children, non-string keys, or
  non-number arithmetic.
- Red tracers:
  `go test -count=1 ./... -run TestCompilerRecordsDynamicPathAddStoreBlockPlan`
  initially failed because no block family described the dynamic path update.
  `go test -count=1 ./... -run TestRunDirectFrameUsesDynamicPathAddStoreBlockPlan`
  initially failed because `SET_STRING_FIELD_INDEX` still dispatched in the
  loop.
- Correctness guard:
  `go test -count=1 ./... -run 'Test(CompilerRecordsDynamicPathAddStoreBlockPlan|RunDirectFrameUsesDynamicPathAddStoreBlockPlan|DynamicPathAddStoreBlockPlanFallsBackForMetatable)'`
- Phase guard:
  `go test -count=1 ./... -run 'Direct|Block|SideExit|BenchmarksMatch'`
- Attribution proof:
  `go test -count=1 ./... -run TestScenarioMechanismAttributionCoversCurrentWorstRows -v`
  showed `economy_market_tick` `SET_STRING_FIELD_INDEX` leaving the top opcode
  list and `ADD`/`SUB` counts dropping (`ADD 2656 -> 2332`,
  `SUB 1296 -> 972` in the logged sample).
- Benchmark guard:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(economy_market_tick|threat_aggro_table|save_state_diff|dialogue_condition_eval|path_relaxation|cooldown_scheduler)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=3 ./...`
  showed roughly flat/noisy timing for `economy_market_tick`
  (`364396-366126 ns/op`) with allocations stable at `42 allocs/op`.

### 2026-07-06 Under-2x normal-run hot-path regression checkpoint

- Current plan phase: Scenario Under 2x Speed Work; Phase 5, Typed
  Direct-Frame Superblock Families; Slice 5.2 first global block families.
- Finding: the large regression report was not intended. In the current tree,
  normal `Run` no longer pays the verified-plan pre-dispatch probe because
  verified plans are gated by private PIC/instrumentation counters. The
  remaining confirmed normal-run hot-path costs were the `GET_STRING_FIELD_INDEX`
  `blockPlanAt` accessor for the new dynamic block family and full
  `tableShapeToken` materialization in string-only caches after the table epoch
  split.
- Work: flattened the dynamic-path block lookup to direct local PC-map arrays
  inside `runDirectFrame` and added a smaller private `tableStringShapeToken`
  for string-only slots, dynamic index caches, table-field call caches, runtime
  path caches, and intrinsic guards. The full `tableShapeToken` remains for
  table epoch tests and broad shape comparisons, but normal runtime files no
  longer call it from cache hot paths.
- Correctness guard:
  `go test -count=1 ./... -run 'Test(Table.*Slot|TableShapeToken|RunDirectFrameVerifiedPlansArePICOptIn|RunDirectFrameUsesDynamicPathAddStoreBlockPlan|DynamicPathAddStoreBlockPlanFallsBackForMetatable)'`
- Attribution guard:
  `go test -count=1 ./... -run TestScenarioMechanismAttributionCoversCurrentWorstRows -v`
- Benchmark/profile guards:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(economy_market_tick|threat_aggro_table|save_state_diff|dialogue_condition_eval|path_relaxation|cooldown_scheduler)/ember_run$|BenchmarkTop10Luau/(arithmetic_for|array_ops|table_fields|generic_iteration)/ember_run$|BenchmarkClassicLuau/iterative_fibonacci/ember_run$' -benchmem -benchtime=300ms -count=1 ./...`
  before the patch showed `economy_market_tick 366891 ns/op`,
  `threat_aggro_table 283971 ns/op`, `save_state_diff 227078 ns/op`,
  `dialogue_condition_eval 83407 ns/op`, and `path_relaxation 123581 ns/op`.
  The same guard after the patch showed `economy_market_tick 324541 ns/op`,
  `threat_aggro_table 261832 ns/op`, `save_state_diff 203850 ns/op`,
  `dialogue_condition_eval 80888 ns/op`, and `path_relaxation 120686 ns/op`;
  allocations stayed in the same band.
  A focused profile for `economy_market_tick` dropped `(*Proto).blockPlanAt`
  out of the top profile after the direct PC-map lookup.
- Longer guard:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/(economy_market_tick|threat_aggro_table|save_state_diff|dialogue_condition_eval|path_relaxation|cooldown_scheduler)/(ember_run|luau_cli_batch)$|BenchmarkTop10Luau/(arithmetic_for|array_ops|table_fields|generic_iteration)/ember_run$|BenchmarkClassicLuau/iterative_fibonacci/ember_run$' -benchmem -benchtime=500ms -count=3 ./...`
  kept `economy_market_tick` stable around `324000-327000 ns/op`,
  `save_state_diff` around `202000-206000 ns/op`, and
  `dialogue_condition_eval` around `81000 ns/op`. `threat_aggro_table` and
  `path_relaxation` still showed outlier samples, so future broad sweeps should
  continue to treat 200-300ms single runs as noisy.
