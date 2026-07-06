# Plan: Scenario 1.3x Luau Performance

## Status

Superseded by `scenario-general-performance.md`.

This file is historical context for benchmark-shaped artifact removal, previous
Scenario ratio evidence, and earlier guardrail work. New Scenario performance
work should follow the general-mechanism plan instead.

## Goal

Bring every current Scenario benchmark row to **1.30x same-run
`luau_cli_batch` or better** on the same machine, while protecting current
Ember behavior, allocations, and non-Scenario benchmark rows from regression.

Scenario rows:

- `combat_tick`;
- `inventory_value`;
- `event_dispatch`;
- `buff_stack_tick`;
- `ability_resolution`;
- `ai_utility_scoring`;
- `cooldown_scheduler`;
- `projectile_sweep`;
- `quest_progress_update`;
- `behavior_tree_tick`;
- `threat_aggro_table`;
- `economy_market_tick`;
- `formation_layout_score`;
- `dialogue_condition_eval`;
- `procgen_room_scoring`;
- `save_state_diff`;
- `path_relaxation`.

The target is dynamic: each final gate must run Ember and `luau_cli_batch` in
the same benchmark invocation and compute ratios from those rows.

External runtime seams stay unchanged:

```go
proto, err := ember.Compile(source)
results, err := ember.Run(proto)
```

No native codegen, JIT, generated interpreter, unsafe dispatch, custom GC, or
public `Proto` metadata is in scope.

## Current State

Benchmark-named Scenario execution artifacts are rejected. Ember must not add
private opcodes, descriptors, VM helpers, verifier facts, or source/body
recognizers that encode a whole benchmark such as combat, inventory, event
dispatch, AI scoring, ability resolution, buff ticking, economy ticking, or any
other Scenario row.

Accepted performance work must be general-purpose and reusable across many Luau
programs. Valid seams include:

- direct-frame dispatch for ordinary bytecode;
- language-level bytecode superinstructions;
- generic table, array, row, and field slot hints with normal fallback;
- reusable inline caches guarded by table/global versions;
- intrinsic guards that preserve host override behavior;
- deopt or fallback paths that execute ordinary Luau semantics.

Rejected work includes raw/direct patches that recognize one Scenario source or
bytecode body and mutate tables directly inside a benchmark-named helper. The
old Scenario fused-helper evidence is historical only and must not be used as a
model for future slices.

## Completed Corrections

### 2026-07-05 Benchmark-Shaped Artifacts Removed

- Removed benchmark-named Scenario opcodes, descriptors, verifier facts,
  disassembly facts, and VM helpers.
- Removed the rejected `BUFF_STACK_TICK_STEP` and partial
  `ECONOMY_MARKET_TICK_STEP` work, plus earlier
  `INVENTORY_VALUE_STEP`, `COMBAT_TICK_STEP`, `EVENT_DISPATCH_STEP`,
  `AI_UTILITY_SCORE_STEP`, and `ABILITY_RESOLUTION_STEP` paths.
- Added a regression guard proving representative Scenario-shaped programs do
  not emit benchmark-named bytecode or private Scenario facts.
- Recorded the post-removal full Scenario ratio sweep in
  `docs/benchmarks/fast-execution-ledger.md`.

### 2026-07-05 General Row Store Slice

- Added `SET_ROW_STRING_FIELD`, a general slot-aware string-field store for
  table rows with known named-field layout.
- The compiler emits it only when existing row/table slot evidence is already
  available, including row shapes propagated through generic iteration.
- The VM uses the slot hint only when the table has no metatable; nil deletes,
  stale slots, missing fields, map-backed fields, and metatable cases fall back
  to the existing table semantics.
- Added verifier and behavior coverage for invalid slot hints, row mutation,
  and `__newindex` fallback after deleting a slotted field.

### 2026-07-05 Full-Corpus Ratio Gate

- `scripts/scenario-ratio-gate` now treats all seventeen Scenario rows as
  required evidence.
- The default ratio target is `1.3`.
- Missing Ember or Luau samples fail the gate.

## Non-Regression Contract

Correctness and allocation guard:

```sh
go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets'
```

Full Scenario ratio:

```sh
go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...
```

Scenario ratio gate:

```sh
go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./... | SCENARIO_RATIO_MAX=1.3 scripts/scenario-ratio-gate
```

Current-row guard:

```sh
BENCHTIME=1s COUNT=3 scripts/bench-summary
```

Shared VM/table/call guard when touching shared paths:

```sh
go test -run '^$' -bench 'BenchmarkTop10Luau/(table_fields|array_ops|generic_iteration|method_calls|varargs_select|coroutine_yield)/ember_run$|BenchmarkClassicLuau/recursive_fibonacci/ember_run$' -benchmem -benchtime=1s -count=3 ./...
```

Pre-finish:

```sh
scripts/check-lane root
scripts/check-fast
scripts/check
```

Regression policy:

- A Scenario row must not regress against the current Ember row unless the same
  slice improves another Scenario row enough to lower the worst Scenario/Luau
  ratio and the regression is explicitly recorded.
- Top10 and Classic rows must not regress beyond normal noise.
- Allocations may not exceed existing allocation budgets without a plan update.
- A slice that does not improve runtime can land only if it creates a tighter
  gate, removes invalid work, or removes a proven blocker.

## Design

The deep module is the private bytecode artifact consumed by the compiler,
verifier, disassembler, and VM:

```text
source
  -> lower to ordinary bytecode
  -> attach general execution hints
  -> verify bytecode and hints
  -> run direct frame with guarded fallbacks
```

The artifact may describe general facts such as row string-field slots, dense
array iteration state, dynamic string-key caches, intrinsic identity guards, and
call-site shape guards. It must not describe a whole Scenario benchmark body.

The VM should consume these facts through small private helpers so fallback
behavior stays local and tests can exercise the public `Compile` and `Run`
seams.

## Phase 1: Gates And Evidence

Purpose: make the 1.3x target and no-regression rule mechanical.

Exit gate:

- same-run Scenario/Luau ratios are recorded for all seventeen rows;
- current Ember guard rows are recorded;
- ratio gate fails on missing rows and rows above the configured target;
- allocation budgets cover every Scenario row or document a temporary waiver.

### Slice 1: Same-Run Ratio Ledger

- **Status:** done for the post-removal sweep.
- **Behavior:** no runtime behavior change.
- **Bench:** full Scenario ratio.
- **Done:** ledger records all seventeen rows.

### Slice 2: Current Benchmark Guard Ledger

- **Status:** partially done.
- **Behavior:** no runtime behavior change.
- **Bench:** `BENCHTIME=1s COUNT=3 scripts/bench-summary`.
- **Done:** ledger records Top10, Classic, and all Scenario guard rows.

### Slice 3: Full-Corpus Ratio Gate

- **Status:** done.
- **Behavior:** no runtime behavior change.
- **Artifact:** `scripts/scenario-ratio-gate`.
- **Bench:** full Scenario ratio piped into the gate.
- **Done:** gate requires all seventeen Scenario rows and fails on missing rows.

### Slice 4: Scenario Allocation Budgets

- **Status:** done.
- **Behavior:** no runtime behavior change.
- **Implementation:** extend `TestScenarioEmberRunAllocationBudgets` beyond the
  original three rows.
- **Bench:** `go test -count=1 ./... -run TestScenarioEmberRunAllocationBudgets`.
- **Done:** every current Scenario row has bytes/op and allocs/op protection.

## Phase 2: General Row/Table Field Path

Purpose: reduce repeated string-field lookup, shape checks, and table mutation
overhead for row-processing programs.

Exit gate:

- slot-aware reads and writes are available through ordinary bytecode;
- stale slots, nil deletes, metatables, and map-backed tables fall back safely;
- Top10 table rows do not regress.

### Slice 5: Slot-Aware Row Reads

- **Status:** already present as `GET_ROW_STRING_FIELD`.
- **Behavior:** ordinary field reads preserve missing-field and metatable
  semantics.
- **Bench:** `inventory_value`, `dialogue_condition_eval`, `table_fields`.
- **Done:** compiler emits row read slot hints from named table shapes.

### Slice 6: Slot-Aware Row Stores

- **Status:** done.
- **Behavior:** ordinary field writes preserve nil delete and `__newindex`
  behavior.
- **Artifact:** `SET_ROW_STRING_FIELD`.
- **Bench:** row-heavy Scenario subset plus Top10 table/iteration guard.
- **Done:** compiler emits row store slot hints from named table shapes and
  generic-for row propagation.

### Slice 7: Dynamic String-Key Inline Cache

- **Status:** open.
- **Behavior:** `table[key]` with string keys preserves nil, metatable, and
  non-string-key behavior.
- **Artifact:** generic cache for dynamic string-key get/set, guarded by table
  version and key kind.
- **Bench:** `threat_aggro_table`, `economy_market_tick`,
  `save_state_diff`, `table_fields`.
- **Done:** finite dynamic-key table workloads improve or safely fall back.

### Slice 8: Nested Field Path Store

- **Status:** open.
- **Behavior:** nested paths such as `a.b.c = v` preserve intermediate lookup
  and metatable semantics.
- **Artifact:** general two-step or path-shaped store helper, not a Scenario
  descriptor.
- **Bench:** `path_relaxation`, `save_state_diff`, `table_fields`.
- **Done:** nested row mutations reduce repeated lookup without behavior
  regressions.

## Phase 3: General Iteration Path

Purpose: reduce generic-for overhead for dense arrays and row-processing loops
without recognizing Scenario bodies.

Exit gate:

- dense-array iteration has a verified reusable fast path;
- mixed or mutating iteration falls back safely;
- `generic_iteration` and `array_ops` do not regress.

### Slice 9: Dense Array Iterator Cursor

- **Status:** open.
- **Behavior:** generic-for order and nil termination remain unchanged.
- **Artifact:** general dense-array cursor hint for ordinary `for _, row in t`.
- **Bench:** `generic_iteration`, `inventory_value`,
  `quest_progress_update`, `ai_utility_scoring`.
- **Done:** iterator helper cost falls in profiles with no iteration regression.

### Slice 10: Reusable Loop-Local Field Hoisting

- **Status:** open.
- **Behavior:** loop-carried locals and field reads preserve mutation
  visibility.
- **Artifact:** general loop-local cache invalidated by writes to the owner
  table.
- **Bench:** `combat_tick`, `inventory_value`, `projectile_sweep`.
- **Done:** repeated same-row field reads within a loop reuse guarded facts.

## Phase 4: General Call, Global, And Intrinsic Guards

Purpose: stop paying repeated lookup and call setup costs when identity is
stable, while preserving host overrides and yielding behavior.

Exit gate:

- intrinsic/global identity guards are checked once per safe scope;
- dynamic function calls fall back on changed globals, changed tables, yielding
  functions, host calls, or protected calls;
- Top10 call/coroutine rows do not regress.

### Slice 11: Hoisted Base Intrinsic Guards

- **Status:** open.
- **Behavior:** `RunWithGlobals` overrides still take effect.
- **Artifact:** generic intrinsic identity guard with normal fallback.
- **Bench:** `combat_tick`, `ability_resolution`, `event_dispatch`,
  `method_calls`.
- **Done:** repeated `math.min`, `rawlen`, and table intrinsic lookups move out
  of hot loops where legal.

### Slice 12: Stable Table Function Call Cache

- **Status:** open.
- **Behavior:** missing handlers, non-functions, changed handler tables,
  yielding handlers, host handlers, and error propagation remain correct.
- **Artifact:** general guarded table-field call cache.
- **Bench:** `event_dispatch`, `behavior_tree_tick`, `method_calls`,
  `coroutine_yield`.
- **Done:** dynamic handler dispatch improves without a benchmark-named helper.

## Phase 5: Worst-Ratio Burn-Down

Purpose: repeatedly profile the current worst same-run Scenario/Luau ratio and
choose the next general mechanism.

Exit gate:

- every Scenario row is <= `1.30x` same-run Luau batch;
- Top10, Classic, and Scenario current-row guards pass;
- allocation budgets pass;
- ledger records the final evidence and rejected probes.

### Slice 13: Profile Worst Current Row

- **Status:** open.
- **Behavior:** no runtime behavior change.
- **Bench:** focused Scenario profile for the worst row from the latest full
  ratio sweep.
- **Done:** ledger names the top frames and the general mechanism they imply.

### Slice 14: Implement One General Mechanism

- **Status:** repeating slice.
- **Behavior:** no public behavior regression.
- **Bench:** focused row ratio, shared guard rows, full Scenario ratio.
- **Done:** worst ratio decreases, or the ledger records why the attempted
  mechanism was rejected.

### Slice 15: Retirement

- **Status:** open.
- **Behavior:** all benchmark fixtures match expected results.
- **Bench:** behavior/allocation guard, full Scenario ratio gate,
  current-row guard, `scripts/check-lane root`, `scripts/check-fast`, and
  `scripts/check`.
- **Done:** every current Scenario row is <= `1.30x`, no current rows regress,
  and docs/ledger describe the final accepted mechanisms.

## Risks

- **Ratio noise:** compare Ember and Luau in the same benchmark invocation.
- **Overfitting:** reject benchmark-named helpers and source/body recognition.
- **Hidden fallback:** profiles must confirm optimized bytecode is actually
  running.
- **Host override behavior:** hoisted globals or intrinsics need fallback tests.
- **Metatable behavior:** slot and cache paths must fall back around
  `__index`, `__newindex`, nil deletes, and table shape changes.
- **Current benchmark regression:** shared VM/table/call changes need Top10 and
  Classic guard rows.
