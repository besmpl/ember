# Plan: Scenario 1.3x Luau Performance

## Goal

Bring every Scenario benchmark row to **1.30x Luau batch or better** on the
same machine, while protecting every current Ember benchmark row from
regression.

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

The target is dynamic, not a stale number in this document: each final gate must
run Ember and `luau_cli_batch` in the same benchmark invocation and compute the
ratio from those rows.

External runtime seams stay unchanged:

```go
proto, err := ember.Compile(source)
results, err := ember.Run(proto)
```

No native codegen, JIT, generated interpreter, unsafe dispatch, custom GC, or
public `Proto` metadata is in scope.

## Current State

Scenario benchmark-named execution artifacts have been removed from the accepted
implementation path. Ember must not introduce private opcodes, descriptors, VM
helpers, or verifier facts that encode a whole benchmark body such as combat,
inventory, event dispatch, AI scoring, ability resolution, buff ticking, or
economy market ticking.

Accepted performance work must be general-purpose and reusable across many Luau
programs. Examples of acceptable seams are direct-frame dispatch, ordinary
bytecode superinstructions with language-level semantics, generic table/array
shape facts, reusable inline-cache mechanisms, intrinsic guards with normal
fallback, and deopt paths that preserve Luau behavior.

Rejected work includes benchmark-shaped raw/direct patches that recognize one
Scenario source or bytecode body and mutate tables directly inside a named
helper. The previously accepted Scenario artifacts are now treated as invalidated
evidence, not as a model to extend.

### 2026-07-05 Correction

- Removed the benchmark-named Scenario opcodes and private descriptors from the
  compiler, verifier, disassembler, and VM dispatch path.
- Removed the rejected `BUFF_STACK_TICK_STEP` and partial
  `ECONOMY_MARKET_TICK_STEP` work, plus the earlier benchmark-shaped
  `INVENTORY_VALUE_STEP`, `COMBAT_TICK_STEP`, `EVENT_DISPATCH_STEP`,
  `AI_UTILITY_SCORE_STEP`, and `ABILITY_RESOLUTION_STEP` paths.
- Added a regression guard that representative Scenario-shaped programs do not
  emit benchmark-named bytecode or private Scenario facts.
- Post-removal full Scenario benchmark sweep completed with
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`.
  The largest current gaps are still broad table/loop workloads, including
  `threat_aggro_table`, `economy_market_tick`, `dialogue_condition_eval`,
  `save_state_diff`, and `path_relaxation`.
- The performance goal remains open. The next slices must rebuild performance
  through general mechanisms only.

## Non-Regression Contract

Each performance slice must protect current Ember behavior and current Ember
speed.

Correctness guard:

```sh
go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets'
```

Scenario ratio gate:

```sh
go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...
```

Current-row guard:

```sh
BENCHTIME=1s COUNT=3 scripts/bench-summary
```

Regression policy:

- A Scenario row must not regress against the current Ember row unless the same
  slice improves another Scenario row enough to lower the worst Scenario/Luau
  ratio and the regression is explicitly recorded.
- Top10 and Classic rows must not regress beyond normal noise. If a shared
  change touches table, call, numeric, coroutine, vararg, or iteration code, run
  the relevant Top10/Classic guard rows before keeping it.
- Allocations may not exceed existing allocation budgets without a plan update.
- A slice that does not improve runtime can land only if it creates a tighter
  gate or removes a proven blocker.

## Design

The deep module remains the private verified execution artifact:

```text
source
  -> lower to bytecode
  -> finalize execution facts
  -> verify artifact
  -> run direct frame and fused scenario helpers
```

The next performance wins should come from deepening the direct execution
artifact, not scattering more one-off checks across the VM. The artifact should
own:

- Scenario loop region facts;
- row-array and row-slot facts;
- typed numeric/string field facts;
- base intrinsic identity guards hoisted out of inner loops;
- handler dispatch facts;
- state mutation facts;
- fused helper validity and fallback rules.

The VM should consume those facts with the least possible per-iteration
rediscovery.

## Phase 1: Hard Gates And Current Evidence

Purpose: make the 1.3x target and no-regression rule mechanical before touching
more hot code.

Exit gate:

- same-run Scenario/Luau ratio is recorded;
- current Ember row baselines are recorded;
- profiles identify current top frames;
- every future slice has a command that can fail on the user's exact target.

### Slice 1: Same-Run Ratio Ledger

- **Depends:** local `luau` binary available through existing benchmark helper.
- **Behavior:** no runtime behavior change.
- **Artifact:** none.
- **Implementation:** update benchmark ledger with same-run Ember/Luau samples,
  ratios, and dynamic 1.3x target wording.
- **Bench:** Scenario ratio gate.
- **Done:** ledger states current ratio for each Scenario row and marks all
  three above 1.3x.

### Slice 2: Current Benchmark Guard Ledger

- **Depends:** Slice 1.
- **Behavior:** no runtime behavior change.
- **Artifact:** none.
- **Implementation:** record current Top10, Classic, and Scenario rows as guard
  baselines, with accepted noise policy.
- **Bench:** `BENCHTIME=1s COUNT=3 scripts/bench-summary`.
- **Done:** plan and ledger define what "current benchmarks don't regress"
  means for shared code.

### Slice 3: Current Profile Ledger

- **Depends:** Slice 1.
- **Behavior:** no runtime behavior change.
- **Artifact:** none.
- **Implementation:** record top profile frames for each Scenario row.
- **Bench:** run each Scenario row with `-cpuprofile`.
- **Done:** ledger shows `runDirectFrame` and fused helpers as the active
  pressure, not `runGenericFrame`.

### Slice 4: Ratio Gate Helper

- **Depends:** Slice 1.
- **Behavior:** no runtime behavior change.
- **Artifact:** none.
- **Implementation:** add a script or test helper that parses Scenario
  `ember_run` and `luau_cli_batch` rows and fails if any ratio is above a
  configured threshold.
- **Bench:** Scenario ratio gate piped through the helper.
- **Done:** `SCENARIO_RATIO_MAX=1.3` is a red-capable command.

## Phase 2: Direct-Frame Dispatch And Loop Region Overhead

Purpose: reduce the cost still paid around fused helpers: VM switch dispatch,
loop iteration plumbing, direct array iteration, and repeated descriptor
lookups.

Exit gate:

- Scenario loops spend less time in `runDirectFrame` overhead;
- `baseArrayNextInline` is no longer a top Scenario pressure;
- no Top10 iteration regression.

### Slice 5: Direct-Frame Opcode Accounting

- **Depends:** Slice 3.
- **Behavior:** no runtime behavior change.
- **Artifact:** private tests can count hot opcode execution per Scenario row.
- **Implementation:** add benchmark-only or test-only accounting for direct
  frame opcode families, kept out of normal hot paths.
- **Bench:** Scenario sweep.
- **Done:** each Scenario row has a ranked list of hot opcodes/fused steps.

### Slice 6: Scenario Loop Region Facts

- **Depends:** Slice 5.
- **Behavior:** Scenario fixtures return identical results.
- **Artifact:** finalizer records inner-loop PC ranges and loop-carried
  registers for each Scenario row.
- **Verifier:** invalid loop region facts are rejected.
- **Implementation:** compute private loop-region descriptors from verified
  bytecode shape.
- **Bench:** no required win; this unlocks later slices.
- **Done:** disassembly/facts show loop regions for combat, inventory, and
  event dispatch.

### Slice 7: Direct Iterator Cursor In Loop Region

- **Depends:** Slice 6.
- **Behavior:** direct table iteration order and nil termination remain
  unchanged.
- **Artifact:** loop-region facts include direct array cursor slots.
- **Verifier:** cursor descriptors validate table/control registers.
- **Implementation:** avoid generic `baseArrayNextInline` calls inside verified
  Scenario loop regions.
- **Bench:** `combat_tick`, `inventory_value`, `event_dispatch`,
  `generic_iteration`.
- **Done:** `baseArrayNextInline` falls out of Scenario top profile frames and
  `generic_iteration` does not regress.

### Slice 8: Fused-Step Dispatch Loop

- **Depends:** Slices 6 and 7.
- **Behavior:** loop exits, breaks, and returned values remain correct for
  Scenario fixtures.
- **Artifact:** loop-region facts can name a fused step as the loop body.
- **Verifier:** fused loop descriptors validate entry, exit, and fallback PCs.
- **Implementation:** execute repeated Scenario fused steps in a small direct
  loop rather than returning to the general opcode switch every row.
- **Bench:** all Scenario rows plus `while_branching`.
- **Done:** `runDirectFrame` flat time drops for all three Scenario profiles.

## Phase 3: Typed Row Data Path

Purpose: remove row-slot helper overhead and repeated `Value` shape checks from
the fused Scenario helpers.

Exit gate:

- row numeric/string access avoids repeated generic slot helper calls;
- table mutations invalidate typed facts safely;
- Top10 table rows do not regress.

### Slice 9: Typed Row Slot Facts

- **Depends:** Slice 6.
- **Behavior:** row tables remain ordinary mutable Luau tables.
- **Artifact:** row-shape facts record expected kind for hot fields: number,
  string, boolean, or table.
- **Verifier:** typed row facts reject invalid field indexes or incompatible
  constants.
- **Implementation:** derive typed row slot facts for Scenario rows from table
  literals and guarded mutation sites.
- **Bench:** no required win; this unlocks direct typed access.
- **Done:** facts show typed slots for `hp`, `shield`, `regen`, `damage`,
  `alive`, `kind`, `count`, `value`, `rarity`, and `amount`.

### Slice 10: Numeric Row Slot Fast Access

- **Depends:** Slice 9.
- **Behavior:** numeric fields still fall back on nil, non-number, metatable, or
  mutation invalidation.
- **Artifact:** descriptors name numeric slots and fallback PCs.
- **Verifier:** malformed numeric slot descriptors fail verification.
- **Implementation:** read hot numeric row fields as numbers inside fused
  helpers, avoiding repeated `inventorySlotNumber` / `inventorySlotValue`
  paths.
- **Bench:** `combat_tick`, `inventory_value`.
- **Done:** numeric row helper frames drop in profiles and both rows improve.

### Slice 11: String/Boolean Row Slot Fast Access

- **Depends:** Slice 9.
- **Behavior:** string equality, boolean truthiness, and fallback semantics
  remain unchanged.
- **Artifact:** descriptors name string/boolean slots and constants.
- **Verifier:** malformed string/boolean descriptors fail verification.
- **Implementation:** read `item.kind`, `event.kind`, and `entity.alive`
  through typed slot facts inside fused helpers.
- **Bench:** `combat_tick`, `inventory_value`, `event_dispatch`.
- **Done:** `inventorySlotString`, `valuesEqual`, and raw string lookup costs
  drop in Scenario profiles.

### Slice 12: Direct Row Mutation Store

- **Depends:** Slices 10 and 11.
- **Behavior:** row and state field mutations are visible through normal table
  APIs after execution.
- **Artifact:** mutation descriptors carry table version and slot facts.
- **Verifier:** stale or invalid mutation descriptors fail or fall back.
- **Implementation:** write hot row/state fields by verified slot in fused
  helpers, with normal table version invalidation.
- **Bench:** `combat_tick`, `event_dispatch`, `table_fields`.
- **Done:** `setRawStringField` drops in profiles without table guard
  regression.

## Phase 4: Hoisted Guards And Intrinsic/Handler Caches

Purpose: stop paying per-iteration guard and lookup cost when a run-level or
loop-level guard is enough.

Exit gate:

- base intrinsic identity is checked once per run or loop region where safe;
- handler dispatch avoids repeated table/string lookup when shape is stable;
- host override behavior remains proven.

### Slice 13: Hoisted `math.min` Guard

- **Depends:** Slice 6.
- **Behavior:** `RunWithGlobals` override of `math.min` still falls back.
- **Artifact:** loop-region facts include a guarded `math.min` identity token.
- **Verifier:** invalid intrinsic token descriptors fail.
- **Implementation:** check `math.min` identity once before the verified loop
  region, not inside every fused step.
- **Bench:** `combat_tick`, `event_dispatch`.
- **Done:** `baseFieldIntrinsicCallee` drops from Scenario profiles.

### Slice 14: Handler Table Shape Cache

- **Depends:** Slices 6 and 11.
- **Behavior:** missing handlers, non-function handlers, changed handler table,
  and non-string event keys still behave normally.
- **Artifact:** handler dispatch facts map known event-kind strings to handler
  closures with table version guards.
- **Verifier:** handler facts reject non-closure or stale handler entries.
- **Implementation:** resolve `damage`, `heal`, and `score` handler closures
  once per verified loop region.
- **Bench:** `event_dispatch`.
- **Done:** raw handler table string lookup drops and event row improves.

### Slice 15: No-Yield Handler Result Path

- **Depends:** Slice 14.
- **Behavior:** any handler that can yield, call host code, or use protected
  calls remains on the general path.
- **Artifact:** no-yield handler facts include return register and state
  mutation facts.
- **Verifier:** handler prototypes are checked for no-yield direct eligibility.
- **Implementation:** execute verified simple handlers without generic frame
  setup and without result-list machinery.
- **Bench:** `event_dispatch`, `method_calls`, coroutine guard tests.
- **Done:** event handler execution no longer shows generic call/frame helper
  pressure and `method_calls` does not regress.

## Phase 5: Scenario Fused Helper V2

Purpose: after guards and typed row facts are proven, replace the remaining
fused-helper internals with smaller, typed, loop-region-aware helpers.

Exit gate:

- each Scenario row moves toward <= 1.3x Luau;
- each helper keeps fallback behavior;
- helper internals are artifact-driven, not source-text hacks.

### Slice 16: Inventory Step V2

- **Depends:** Slices 8, 10, 11, and 13.
- **Behavior:** all branches of `inventory_value` produce `18540`.
- **Artifact:** V2 descriptor references typed row slots, string constants,
  score register, and loop-region facts.
- **Verifier:** malformed V2 inventory descriptors fail.
- **Implementation:** rewrite inventory fused step to operate on typed row
  numbers and string tags with hoisted loop facts.
- **Bench:** `inventory_value`.
- **Done:** row is at or below 1.8x Luau as an intermediate gate, with no
  `generic_iteration` regression.

### Slice 17: Combat Step V2

- **Depends:** Slices 8, 10, 11, 12, and 13.
- **Behavior:** `combat_tick` produces `2519`, including shield, alive, and
  score semantics.
- **Artifact:** V2 descriptor references typed numeric/boolean slots and
  hoisted `math.min`.
- **Verifier:** malformed V2 combat descriptors fail.
- **Implementation:** rewrite combat fused step to avoid generic slot helpers
  and per-iteration intrinsic lookup.
- **Bench:** `combat_tick`.
- **Done:** row is at or below 1.8x Luau as an intermediate gate.

### Slice 18: Event Dispatch Step V2

- **Depends:** Slices 8, 10, 11, 12, 14, and 15.
- **Behavior:** `event_dispatch` produces `8414` and dynamic failure cases
  remain covered.
- **Artifact:** V2 descriptor references event row slots, handler cache, state
  slots, and no-yield handler facts.
- **Verifier:** malformed V2 event descriptors fail.
- **Implementation:** rewrite event fused step around typed event fields,
  cached handlers, direct state mutation, and direct one-result return.
- **Bench:** `event_dispatch`.
- **Done:** row is at or below 1.8x Luau as an intermediate gate.

## Phase 6: 1.3x Closure And No-Regression Hardening

Purpose: close the final gap to 1.3x and make the result hard to regress.

Exit gate:

- all Scenario rows are <= 1.30x same-run Luau batch;
- current Top10/Classic/Scenario Ember rows have not regressed;
- allocation budgets hold;
- profiles show no obvious accidental generic fallback.

### Slice 19: Worst-Ratio Burn-Down

- **Depends:** Slices 16-18.
- **Behavior:** no Scenario behavior changes.
- **Artifact:** depends on the worst remaining row.
- **Verifier:** any new descriptor added here gets malformed descriptor tests.
- **Implementation:** profile the worst remaining Scenario/Luau ratio and make
  one targeted artifact-driven optimization. Do not touch lower-ratio rows
  unless guard rows stay stable.
- **Bench:** full Scenario ratio gate and current-row guard.
- **Done:** worst Scenario row is <= 1.30x Luau or has one named remaining
  blocker with a measured cost.

### Slice 20: Regression Wall And Retirement

- **Depends:** Slice 19.
- **Behavior:** all benchmark fixtures match expected results.
- **Artifact:** final disassembly/fact tests cover every optimized path.
- **Verifier:** malformed descriptor tests cover every descriptor family.
- **Implementation:** add or update a Scenario ratio gate script/test, update
  the ledger, document rejected probes, and remove temporary instrumentation.
- **Bench:** Scenario ratio gate plus current-row guard.
- **Done:** every Scenario row is <= 1.30x same-run Luau batch and current
  Ember rows stay within the no-regression policy.

## Verification

Focused Scenario ratio:

```sh
go test -run '^$' -bench 'BenchmarkScenarioLuau/<case>/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...
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

Behavior and allocation guard:

```sh
go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets'
```

Shared-performance guard when touching shared VM/table/call code:

```sh
go test -run '^$' -bench 'BenchmarkTop10Luau/(table_fields|array_ops|generic_iteration|method_calls|varargs_select|coroutine_yield)/ember_run$|BenchmarkClassicLuau/recursive_fibonacci/ember_run$' -benchmem -benchtime=1s -count=3 ./...
```

Pre-finish:

```sh
scripts/check-lane root
scripts/check-fast
```

Retirement:

```sh
scripts/check
```

## Risks

- **Ratio noise:** compare Ember and Luau in the same benchmark invocation;
  never compare today's Ember to an old Luau number.
- **Overfitting:** keep artifact facts general: loop regions, typed row slots,
  intrinsic guards, and handler caches.
- **Hidden fallback:** a helper may look fast in disassembly but fall back at
  runtime. Profiles and rejection/fallback counters must catch this.
- **Host override behavior:** every hoisted intrinsic or handler cache needs a
  fallback test.
- **Metatable behavior:** typed slot access must fall back when table shape,
  metatable state, or nil deletion invalidates the fact.
- **Current benchmark regression:** shared VM/table/call changes can silently
  hurt Top10 or Classic rows; guard them before keeping a slice.

## Retirement Criteria

Retire this plan when:

- `combat_tick`, `inventory_value`, and `event_dispatch` are each <= 1.30x
  same-run `luau_cli_batch`;
- current Top10, Classic, and Scenario Ember rows stay within the
  no-regression policy;
- allocation budgets pass;
- every optimized path has behavior, verifier, and disassembly coverage;
- profiles show no accidental return to `runGenericFrame` dominance;
- rejected experiments are recorded in the benchmark ledger.

## Extension Phases

The first 20 slices aim at the 1.3x Scenario target. These additional phases are
for making the optimization design deeper after the target is in reach. Append
them only after the first plan has produced evidence; do not use them as an
excuse to reshuffle earlier slices.

The design goal is a smaller internal interface with more behavior behind it:
callers should not need to know which descriptor family, fallback rule, profile
counter, verifier path, or disassembly line makes a fast path safe. The private
artifact module should own that.

## Phase 7: Artifact Module Deepening

Purpose: turn the growing set of descriptors into a coherent private module
rather than a pile of VM-side special cases.

Exit gate:

- descriptor ownership is local and explicit;
- verifier, disassembly, fallback, and profile behavior are attached to the
  same descriptor family;
- deleting the artifact module would re-spread complexity across emitter,
  bytecode, VM, tests, and benchmark code.

### Slice 21: Descriptor Ownership Map

- **Depends:** Slices 1-20.
- **Behavior:** no runtime behavior change.
- **Interface:** one private descriptor registry lists each optimized descriptor
  family, owner, verifier, disassembler, and fallback rule.
- **Implementation:** document and encode ownership for Scenario descriptors:
  row slots, loop regions, intrinsic guards, handler caches, and fused steps.
- **Bench:** no performance gate; this is locality work.
- **Done:** a maintainer can answer "where is this fast path owned?" from one
  private module, not by searching the VM.

### Slice 22: Unified Fallback Contract

- **Depends:** Slice 21.
- **Behavior:** every optimized path preserves current fallback behavior.
- **Interface:** one private fallback result shape tells the VM whether to
  continue direct execution, jump to generic execution, or report a hard error.
- **Implementation:** replace ad hoc fallback booleans in Scenario helpers with
  the unified private result.
- **Bench:** Scenario ratio gate and current-row guard.
- **Done:** fallback code is visible in one shape and no Scenario row regresses.

### Slice 23: Descriptor Verifier Registry

- **Depends:** Slice 21.
- **Behavior:** malformed descriptor tests still fail before execution.
- **Interface:** descriptor families register verifier functions through one
  private mechanism.
- **Implementation:** move Scenario descriptor verification under that registry
  without changing public behavior.
- **Bench:** no expected speed change.
- **Done:** adding a descriptor without a verifier is hard to do accidentally.

### Slice 24: Artifact Fact Report

- **Depends:** Slices 21-23.
- **Behavior:** no runtime behavior change.
- **Interface:** one private fact-report function emits disassembly,
  eligibility, fallback, and descriptor summaries for tests.
- **Implementation:** replace scattered scenario disassembly expectations with
  fact-report assertions where practical.
- **Bench:** no expected speed change.
- **Done:** optimized shape tests read like artifact behavior, not bytecode
  trivia.

## Phase 8: Generalized Loop And Row Optimization

Purpose: make Scenario wins describe a class of row-processing programs, not
only three hard-coded fixtures.

Exit gate:

- loop-region and row-shape facts generalize beyond the named benchmark rows;
- superinstructions are selected by semantic bytecode shape, not source text;
- new row-processing fixtures benefit or safely fall back.

### Slice 25: Row Workload Mini-Corpus

- **Depends:** Slice 24.
- **Behavior:** add several source-to-result fixtures that look like small
  Hearth-style row workloads but are not the existing Scenario sources.
- **Interface:** tests still enter through `Compile` and `Run`.
- **Implementation:** add fixtures for row filtering, row accumulation, mixed
  numeric/string fields, and sparse-row fallbacks.
- **Bench:** optional microbench rows for the mini-corpus.
- **Done:** the optimizer has examples that punish source-text overfitting.

### Slice 26: Shape-Driven Loop Recognizer

- **Depends:** Slice 25.
- **Behavior:** existing Scenario and mini-corpus fixtures keep results.
- **Interface:** finalization exposes one private recognizer that consumes
  bytecode/facts and returns loop-region descriptors.
- **Implementation:** move scenario loop recognition behind that recognizer.
- **Bench:** Scenario ratio gate and mini-corpus microbench if present.
- **Done:** adding a row loop variant changes the recognizer, not the VM switch.

### Slice 27: Polymorphic Row Shape Variants

- **Depends:** Slice 26.
- **Behavior:** row loops with two compatible record shapes either optimize
  safely or fall back.
- **Interface:** row-shape facts can represent a small set of compatible
  shapes behind one descriptor.
- **Implementation:** support shape variants for common optional fields without
  making table semantics static.
- **Bench:** mini-corpus plus `inventory_value`.
- **Done:** optional-field row programs do not force all row loops back to the
  generic path.

### Slice 28: Descriptor Cost Counters

- **Depends:** Slice 26.
- **Behavior:** no runtime behavior change when counters are disabled.
- **Interface:** private debug counters report descriptor hits, fallbacks, and
  invalidations.
- **Implementation:** add opt-in counters with zero normal-run overhead or a
  clearly measured disabled cost.
- **Bench:** current-row guard with counters disabled.
- **Done:** profiles can explain whether a descriptor is cold, hot, or falling
  back too often.

## Phase 9: Regression-Resistant Benchmarking

Purpose: make "current benchmarks don't regress" mechanical enough that future
optimization work cannot hand-wave it.

Exit gate:

- ratio gates and current-row guards are easy to run;
- benchmark output is summarized consistently;
- noisy rows are tracked with an explicit policy.

### Slice 29: Scenario Ratio Gate Script

- **Depends:** Slice 1 or Slice 20 if already implemented there.
- **Behavior:** no runtime behavior change.
- **Interface:** `scripts/scenario-ratio-gate` reads benchmark output and
  enforces `SCENARIO_RATIO_MAX`.
- **Implementation:** parse `ember_run` and `luau_cli_batch` rows from one
  benchmark invocation.
- **Bench:** full Scenario ratio command piped into the script.
- **Done:** the gate fails when any Scenario row is above 1.30x.

### Slice 30: Current-Row Guard Script

- **Depends:** Slice 2.
- **Behavior:** no runtime behavior change.
- **Interface:** one script compares `scripts/bench-summary` output against a
  checked-in guard table with noise tolerance.
- **Implementation:** protect all current Top10, Classic, and Scenario rows.
- **Bench:** `BENCHTIME=1s COUNT=3 scripts/bench-summary` piped into the guard.
- **Done:** shared VM/table/call changes have a simple red/green speed guard.

### Slice 31: Benchstat Archive

- **Depends:** Slices 29-30.
- **Behavior:** no runtime behavior change.
- **Interface:** benchmark runs can be archived as before/after files and
  compared with one command.
- **Implementation:** add a small script or documented command around `benchstat`
  if available, with a fallback summary if not.
- **Bench:** run one Scenario before/after comparison.
- **Done:** retained optimizations cite a comparable artifact, not loose
  terminal memory.

### Slice 32: Automatic Profile Snapshot

- **Depends:** Slice 28.
- **Behavior:** no runtime behavior change.
- **Interface:** one script captures top profile frames for Scenario rows.
- **Implementation:** produce compact profile summaries suitable for the ledger.
- **Bench:** Scenario profile command.
- **Done:** each major performance slice can update profile evidence without
  manual pprof spelunking.

## Phase 10: Compatibility And Fallback Hardening

Purpose: make deeper optimization safe around the semantics most likely to bite
later: metatables, host overrides, nil deletion, sparse arrays, and yielding.

Exit gate:

- optimized paths have explicit compatibility stress tests;
- fallback paths are exercised, not just present;
- no Scenario speed win depends on silently narrowing Luau behavior.

### Slice 33: Metatable Stress Matrix

- **Depends:** Slice 22.
- **Behavior:** optimized row/table paths fall back when metatables become
  observable.
- **Interface:** one test matrix covers `__index`, `__newindex`, `__call`, and
  arithmetic metamethod interference with optimized paths.
- **Implementation:** add focused source-to-result tests around Scenario-like
  tables with metatables.
- **Bench:** current-row guard after tests.
- **Done:** fast paths are proven to respect metatable visibility.

### Slice 34: Host Override Stress Matrix

- **Depends:** Slice 13 or Slice 29 if the ratio gate is already active.
- **Behavior:** base globals and library fields can still be overridden.
- **Interface:** one test matrix covers `math.min`, table intrinsics, coroutine
  intrinsics, and any cached handler/global path.
- **Implementation:** add `RunWithGlobals` tests for every hoisted or cached
  host-visible dependency.
- **Bench:** Scenario ratio gate and current-row guard.
- **Done:** no hoisted guard assumes a base function is immutable.

### Slice 35: Table Mutation Invalidation Matrix

- **Depends:** Slices 12 and 27.
- **Behavior:** nil deletion, adding fields, sparse numeric keys, and changing
  row shape invalidate optimized facts.
- **Interface:** one invalidation helper owns version checks for row slots and
  table shapes.
- **Implementation:** consolidate invalidation checks and add tests that force
  fallback mid-run.
- **Bench:** `inventory_value`, `combat_tick`, `table_fields`, `array_ops`.
- **Done:** optimized row facts are invalidated deliberately, not accidentally.

### Slice 36: Yield And Protected-Call Matrix

- **Depends:** Slice 15.
- **Behavior:** direct handler calls fall back when yielding or protected-call
  behavior is possible.
- **Interface:** no-yield facts are verified through one private predicate.
- **Implementation:** add tests for yielding handlers, protected-call handlers,
  host-call handlers, and error propagation through optimized call sites.
- **Bench:** `event_dispatch`, `coroutine_yield`, `method_calls`.
- **Done:** call optimization does not smuggle non-yield assumptions into
  dynamic code.

## Phase 11: Prune And Consolidate

Purpose: delete optimization machinery that does not pull its weight and
consolidate the machinery that does.

Exit gate:

- the private artifact module is deeper, not wider;
- rejected descriptors and stale tests are removed;
- future work has fewer places to edit.

### Slice 37: Descriptor Deletion Audit

- **Depends:** Slices 21-36.
- **Behavior:** no runtime behavior change except deleting unused paths.
- **Interface:** the deletion test is applied to each descriptor family.
- **Implementation:** remove descriptor families that no longer improve a
  benchmark or protect compatibility.
- **Bench:** Scenario ratio gate and current-row guard.
- **Done:** no descriptor remains only because it was expensive to build.

### Slice 38: VM Switch Consolidation

- **Depends:** Slice 37.
- **Behavior:** optimized and fallback behavior stay unchanged.
- **Interface:** direct-frame execution consumes artifact decisions through a
  smaller set of private helper calls.
- **Implementation:** collapse duplicated switch cases and helper branches where
  the artifact module can provide a deeper operation.
- **Bench:** full current-row guard.
- **Done:** VM code gets smaller or easier to navigate without speed loss.

### Slice 39: Artifact Interface Review

- **Depends:** Slice 38.
- **Behavior:** no runtime behavior change.
- **Interface:** review the private artifact interface against depth: fewer
  methods, simpler params, more behavior hidden behind the module.
- **Implementation:** rename or reshape private helpers so callers do not need
  descriptor internals.
- **Bench:** no expected speed change; run current-row guard anyway.
- **Done:** the artifact module has a clear small interface and local
  implementation complexity.

### Slice 40: Scenario Plan Retirement Addendum

- **Depends:** Slices 21-39.
- **Behavior:** all behavior and benchmark guards still pass.
- **Interface:** docs state which optimization seams are stable private seams
  and which experiments were rejected.
- **Implementation:** update this plan, benchmark ledger, and any relevant ADR
  notes if the private artifact module shape became important enough to record.
- **Bench:** Scenario ratio gate, current-row guard, and `scripts/check`.
- **Done:** future optimization starts from a smaller, better-lit module rather
  than a pile of historical special cases.

## Expanded Scenario Corpus Addendum

The first Scenario expansion added three rows:

- `buff_stack_tick`: nested entity rows with nested mutable buff arrays,
  `rawlen`, while-loop cursor control, branch-by-kind effects, and destructive
  `table.remove(entity.buffs, i)`.
- `ability_resolution`: a small state machine over caster, target, and ability
  rows, with cooldown mutation, mana/ward state, tag dispatch, floor division,
  and guarded `math.min`.
- `ai_utility_scoring`: nested action x target scoring loops with two stable
  row shapes, max-reduction state, range checks, kind dispatch, and heavy field
  read density.

Focused evidence from:

```sh
go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=1 ./...
```

shows the expanded rows are now the dominant gap:

| Case | Ember ns/op | Luau ns/run | Ratio | Immediate Pressure |
| --- | ---: | ---: | ---: | --- |
| `combat_tick` | 12,734 | 7,667 | 1.66x | old-row closure |
| `inventory_value` | 17,435 | 11,800 | 1.48x | old-row closure |
| `event_dispatch` | 25,517 | 16,008 | 1.59x | old-row closure |
| `buff_stack_tick` | 59,825 | 10,709 | 5.59x | nested arrays and removal |
| `ability_resolution` | 77,094 | 13,127 | 5.87x | state-machine row mutation |
| `ai_utility_scoring` | 474,395 | 70,433 | 6.73x | nested cartesian scoring |

The goal is unchanged: every Scenario row must finish <= `1.30x` same-run
`luau_cli_batch`, and current Top10, Classic, and Scenario rows must not
regress. The old retirement criteria that name only the first three Scenario
rows are superseded by this addendum.

A later full sweep showed the current `BenchmarkScenarioLuau` corpus is larger
again. In addition to the six rows above, it includes:

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

These rows are now in scope for the same `1.30x` gate. Any phase text below
that says "six rows" means the then-current expansion and is superseded by the
current full benchmark sweep.

## Phase 12: Expanded Corpus Gates And Facts

Purpose: make the new rows first-class citizens in gates, ledgers, profiles,
and artifact reporting before optimizing them. A fast path that cannot explain
itself in this phase should not be trusted in later phases.

Exit gate:

- all current Scenario rows are present in ratio and allocation evidence;
- each new row has a profile summary, hot-opcode summary, and artifact report;
- the current worst-ratio row is computed from the expanded corpus.

### Slice 41: New Scenario Allocation Budgets

- **Depends:** current expanded benchmark corpus.
- **Behavior:** no runtime behavior change.
- **Interface:** allocation-budget tests cover `buff_stack_tick`,
  `ability_resolution`, and `ai_utility_scoring`.
- **Implementation:** add initial budgets from repeated stable runs, with
  enough slack for benchmark noise but no room for unbounded helper allocation.
- **Bench:** `TestScenarioEmberRunAllocationBudgets` plus the full Scenario
  benchmark sweep.
- **Done:** every Scenario row has bytes/op and allocs/op protection before
  shared VM or table changes resume.

### Slice 42: Expanded Ratio Gate Ledger

- **Depends:** Slice 41.
- **Behavior:** no runtime behavior change.
- **Interface:** `scripts/scenario-ratio-gate` and the benchmark ledger treat
  all current rows as required rows.
- **Implementation:** fail the gate when any Scenario row is missing, lacks a
  paired `ember_run`/`luau_cli_batch` sample, or exceeds the configured ratio.
- **Bench:** pipe a full current-corpus Scenario benchmark through
  `SCENARIO_RATIO_MAX=1.3 scripts/scenario-ratio-gate`.
- **Done:** a partial old-corpus win cannot accidentally retire the plan.

### Slice 43: Expanded Hot-Opcode And Profile Ledger

- **Depends:** Slice 42.
- **Behavior:** no runtime behavior change.
- **Interface:** one compact profile/hot-opcode artifact exists for each new
  row.
- **Implementation:** run the existing direct-frame opcode accounting and
  profile snapshot tooling against the three new rows; record top frames and
  dispatch counts in the benchmark ledger.
- **Bench:** focused Scenario profiles for `buff_stack_tick`,
  `ability_resolution`, and `ai_utility_scoring`.
- **Done:** every following optimization slice can name the frame or opcode it
  expects to reduce.

### Slice 44: New Row Artifact Fact Report

- **Depends:** Slice 43.
- **Behavior:** no runtime behavior change.
- **Interface:** artifact reporting describes loop regions, row-slot facts,
  intrinsic guards, mutation stores, array facts, and rejected facts for the
  three new rows.
- **Implementation:** extend the private artifact report enough to show why a
  row did or did not receive a descriptor, without exposing public `Proto`
  metadata.
- **Bench:** no expected speed change; run the Scenario ratio gate to prove no
  instrumentation leak.
- **Done:** optimization work starts from verified facts, not visual pattern
  matching in benchmark source.

## Phase 13: Nested Buff Array And Removal Path

Purpose: make `buff_stack_tick` fast without creating a one-off benchmark
shortcut. The reusable shape is nested dense arrays with verified row elements,
cursor-controlled while loops, and explicit invalidation around destructive
array edits.

Exit gate:

- `buff_stack_tick` is <= `1.80x` same-run Luau before moving to final
  closure;
- `table.remove`, array iteration, and row mutation compatibility guards pass;
- `combat_tick` and `inventory_value` do not regress.

### Slice 45: Nested Row-Array Facts

- **Depends:** Slices 41-44 and the existing row-slot fact machinery.
- **Behavior:** `buff_stack_tick` still returns `9601`.
- **Interface:** the private artifact can describe a row slot whose value is a
  verified dense array of verified row tables.
- **Implementation:** add nested row-array descriptors for `entity.buffs`,
  including parent row shape, child array density, child row shape, and version
  invalidation on parent or child mutation.
- **Bench:** `buff_stack_tick`, `generic_iteration`, `array_ops`.
- **Done:** the VM can prove `entity.buffs[i]` is a dense-array row read before
  it tries to optimize the loop body.

### Slice 46: Direct Rawlen And While Cursor Path

- **Depends:** Slice 45.
- **Behavior:** raw length and while-loop cursor semantics stay Luau-shaped.
- **Interface:** a verified loop-region fact can name `rawlen` bounds and the
  cursor register for a dense nested array.
- **Implementation:** avoid generic `rawlen` and array lookup overhead inside
  the buff while loop when the nested array descriptor is valid; fall back on
  sparse arrays, metatables, or shape/version mismatch.
- **Bench:** `buff_stack_tick`, `array_ops`, any existing `rawlen` row.
- **Done:** profiles show the buff loop no longer spends dominant time in
  generic length/index helpers.

### Slice 47: Verified `table.remove` Dense-Array Shift

- **Depends:** Slice 46.
- **Behavior:** `table.remove(entity.buffs, i)` preserves Luau behavior for
  dense arrays and falls back for observable table edge cases.
- **Interface:** one private helper owns verified dense-array removal,
  including version bumps and fallback conditions.
- **Implementation:** specialize removal for the nested buff array path with a
  direct element shift and nil tail store when the array is dense, unshared for
  the current operation, and metatable-free.
- **Bench:** `buff_stack_tick`, `array_ops`, table mutation compatibility
  tests.
- **Done:** removal no longer routes through the generic table library in the
  verified buff loop.

### Slice 48: Buff Kind Dispatch And Mutation Fusion

- **Depends:** Slice 47.
- **Behavior:** poison, regen, shield, haste, turn decrement, removal, and
  score accumulation remain bit-for-bit equivalent.
- **Interface:** a reusable small string-tag dispatch descriptor maps verified
  row string fields to direct branch arms.
- **Implementation:** fuse the buff inner loop around direct entity fields,
  direct buff fields, tag dispatch, turn decrement, removal/fallthrough cursor
  control, and score accumulation.
- **Bench:** `buff_stack_tick` plus current-row guard.
- **Done:** `buff_stack_tick` reaches the Phase 13 exit gate without adding a
  public benchmark-specific API.

## Phase 14: Ability State-Machine Row Mutation

Purpose: make `ability_resolution` fast by treating it as a small verified
state machine over stable rows. The reusable shape is direct reads and writes
across several stable singleton rows plus one dense ability row array.

Exit gate:

- `ability_resolution` is <= `1.80x` same-run Luau before final closure;
- host override, intrinsic guard, floor-division, and mutation invalidation
  tests pass;
- `event_dispatch`, `combat_tick`, and Top10 table/call rows do not regress.

### Slice 49: Singleton State Row Facts

- **Depends:** Slices 41-44 and existing typed row-slot facts.
- **Behavior:** `ability_resolution` still returns `-6048`.
- **Interface:** the artifact can name stable singleton state rows such as
  `caster` and `target`, separate from iterated row arrays.
- **Implementation:** verify direct slot facts for caster fields (`mana`,
  `heat`, `power`, `combo`) and target fields (`hp`, `armor`, `ward`) with
  mutation invalidation and fallback.
- **Bench:** `ability_resolution`, `table_fields`.
- **Done:** singleton state reads and writes have the same safety story as
  iterated Scenario row slots.

### Slice 50: Ability Row Predicate Fusion

- **Depends:** Slice 49.
- **Behavior:** cooldown and mana branch behavior remains identical.
- **Interface:** a predicate descriptor can combine direct ability-row and
  singleton-state reads for one branch decision.
- **Implementation:** specialize `ability.cooldown <= 0 and caster.mana >=
  ability.cost` into direct numeric slot reads with one fallback path for kind,
  version, or shape mismatch.
- **Bench:** `ability_resolution`, `while_branching`, `table_fields`.
- **Done:** profiles show less time in generic comparison/field-read plumbing
  for the ability branch.

### Slice 51: Ability Tag Dispatch And Intrinsic Guard

- **Depends:** Slice 50.
- **Behavior:** burn, pierce, burst, default strike, floor division, and ward
  absorption stay equivalent.
- **Interface:** reuse the string-tag dispatch descriptor from Slice 48 and the
  existing `math.min` guard descriptor.
- **Implementation:** run the damage calculation through direct row/state slots,
  guarded `math.min`, and explicit fallback for global or library override.
- **Bench:** `ability_resolution`, `combat_tick`, `event_dispatch`, host
  override tests.
- **Done:** `ability_resolution` no longer pays generic string dispatch and
  intrinsic lookup costs in the hot path.

### Slice 52: Cooldown And State Mutation Fusion

- **Depends:** Slice 51.
- **Behavior:** all ability, caster, target, and total mutations remain
  equivalent in both branch arms.
- **Interface:** direct mutation-store descriptors support singleton rows and
  iterated ability rows in the same fused loop.
- **Implementation:** fuse cooldown assignment/decrement, mana update, target
  hp/ward update, heat/combo update, and total accumulation while preserving
  invalidation on nil deletion or shape change.
- **Bench:** `ability_resolution` plus current-row guard.
- **Done:** `ability_resolution` reaches the Phase 14 exit gate and the fused
  mutation helper is reusable outside this benchmark.

## Phase 15: Cartesian Utility Scoring Loops

Purpose: make `ai_utility_scoring` fast by optimizing the general shape of
nested dense-array loops over two row families with a numeric reduction. This
row is the best pressure test for whether the artifact module is deep enough.

Exit gate:

- `ai_utility_scoring` is <= `1.80x` same-run Luau before final closure;
- nested-loop descriptors are reusable and verifier-backed;
- `generic_iteration`, `array_ops`, and old Scenario rows do not regress.

### Slice 53: Dual Row-Shape Loop Facts

- **Depends:** Slices 41-44 and Slice 45 if nested array descriptors were
  generalized there.
- **Behavior:** `ai_utility_scoring` still returns `4612`.
- **Interface:** a loop-region fact can describe two nested dense arrays with
  different verified row shapes.
- **Implementation:** verify action row slots (`kind`, `cost`, `base`,
  `range`) and target row slots (`hp`, `distance`, `threat`, `armor`) across
  the nested action x target loop.
- **Bench:** `ai_utility_scoring`, `generic_iteration`, `array_ops`.
- **Done:** the artifact can prove both loops are stable before any scoring
  fusion exists.

### Slice 54: Nested Cursor Advance Without Generic Calls

- **Depends:** Slice 53.
- **Behavior:** nested loop order and iteration semantics stay unchanged.
- **Interface:** nested loop-region facts provide direct cursor advance for the
  outer action loop and inner target loop.
- **Implementation:** extend direct iterator cursor execution to nested loops
  so the hot path advances both arrays without generic iterator calls.
- **Bench:** `ai_utility_scoring`, `generic_iteration`, `inventory_value`.
- **Done:** profiles show nested cursor helpers or fused-loop bodies replacing
  generic iterator dispatch in the utility scoring row.

### Slice 55: Utility Score Expression Fusion

- **Depends:** Slice 54.
- **Behavior:** range penalty, action-kind branches, floor divisions, and
  energy/cost math remain equivalent.
- **Interface:** one numeric expression helper accepts verified direct row
  slots and singleton self-state slots without exposing descriptor internals to
  the VM switch.
- **Implementation:** fuse score construction from direct action, target, and
  self fields; reuse tag-dispatch support for action kind; keep fallback on any
  descriptor mismatch.
- **Bench:** `ai_utility_scoring`, `table_fields`, `while_branching`.
- **Done:** field-read density and numeric-op dispatch are both materially
  lower in the utility scoring profile.

### Slice 56: Best-Reduction Register Path

- **Depends:** Slice 55.
- **Behavior:** `best` initialization, comparison, assignment, total
  accumulation, and self energy/hp mutation remain equivalent.
- **Interface:** a reduction descriptor owns max-reduction state for a numeric
  local register inside a verified loop region.
- **Implementation:** keep `best`, `score`, and `total` on direct numeric
  paths through the fused loop, then commit visible state at the same semantic
  points as the generic VM.
- **Bench:** `ai_utility_scoring` plus current-row guard.
- **Done:** `ai_utility_scoring` reaches the Phase 15 exit gate without
  regressing old row loops.

## Phase 16: Expanded 1.3x Closure

Purpose: close from the intermediate gates to the actual target across all
Scenario rows, while making sure the new machinery is an extensible
optimization layer instead of benchmark-specific shortcuts.

Exit gate:

- every Scenario row is <= `1.30x` same-run Luau batch;
- Top10, Classic, and Scenario current-row guards pass;
- allocation budgets pass for all current Scenario rows;
- rejected experiments and accepted private seams are recorded.

### Slice 57: Expanded Worst-Ratio Burn-Down

- **Depends:** Slices 48, 52, and 56.
- **Behavior:** no behavior change beyond accepted optimizations.
- **Interface:** the burn-down loop selects the worst same-run ratio from all
  current Scenario rows, not from a hand-picked subset.
- **Implementation:** profile the current worst row, make one narrow
  optimization, rerun the full Scenario ratio gate, and record any rejected
  probes before moving to the next row.
- **Bench:** full Scenario ratio gate with `SCENARIO_RATIO_MAX=1.3`.
- **Done:** the worst row is <= `1.50x` with no old-row regression.

### Slice 58: Shared Optimization Generalization Audit

- **Depends:** Slice 57.
- **Behavior:** no runtime behavior change unless deleting a bad path reveals a
  bug that must be fixed.
- **Interface:** descriptor families are named by reusable runtime shape:
  nested dense row array, singleton state row, tag dispatch, intrinsic guard,
  direct removal, nested cursor, numeric reduction.
- **Implementation:** delete or rewrite any accepted optimization whose
  interface still names a benchmark case instead of a reusable shape.
- **Bench:** full Scenario ratio gate and current-row guard.
- **Done:** future Scenario-like benchmarks can reuse the machinery without
  adding another one-off helper per benchmark.

### Slice 59: Expanded Regression Wall

- **Depends:** Slice 58.
- **Behavior:** all compatibility behavior remains Luau-shaped.
- **Interface:** the regression wall combines behavior tests, allocation
  budgets, Scenario ratio gate, current-row guard, and focused compatibility
  matrices for metatables, host overrides, array mutation, and nil deletion.
- **Implementation:** codify the final command sequence in docs and scripts so
  a future optimization branch cannot skip the expanded corpus by accident.
- **Bench:** behavior/allocation guard, full Scenario ratio gate, current-row
  guard, and shared-performance guard for touched subsystems.
- **Done:** the final performance claim has a repeatable command sequence.

### Slice 60: Expanded Scenario Retirement Addendum

- **Depends:** Slice 59.
- **Behavior:** no runtime behavior change.
- **Interface:** docs state which private optimization seams are stable, which
  were rejected, and which benchmark pressures remain intentionally unsolved.
- **Implementation:** update this plan, the benchmark ledger, and any relevant
  ADR/glossary notes with the final current-corpus evidence and the private
  artifact module shape.
- **Bench:** Scenario ratio gate, current-row guard, `scripts/check-lane root`,
  `scripts/check-fast`, and `scripts/check`.
- **Done:** the plan retires only when all current Scenario rows are <=
  `1.30x`, current benchmarks do not regress, and the optimization layer is
  deeper rather than wider.

## Full Scenario Corpus Addendum

The Scenario benchmark corpus now has seventeen rows. The newest rows add
pressure that the previous six-row expansion did not fully cover:

- `cooldown_scheduler`: nested actor/ability rows with resource refill,
  cooldown clamp/reset, and use counters.
- `projectile_sweep`: projectile movement, target collision, early `break`,
  live flags, and coordinate mutation.
- `quest_progress_update`: event x quest x objective nesting, completion
  booleans, string equality, and guarded `math.min`.
- `behavior_tree_tick`: indexed while traversal, dynamic blackboard reads such
  as `blackboard[node.key]`, and action-state mutation.
- `threat_aggro_table`: dynamic string-key threat maps, event actor lookup,
  focus selection, role dispatch, and enemy state mutation.
- `economy_market_tick`: dynamic `stock`/`demand`/`price` maps keyed by order
  goods, guarded `math.min`, and price/stock mutation.
- `formation_layout_score`: unit x slot scoring, absolute-delta math,
  best-score reduction, and position wrap mutation.
- `dialogue_condition_eval`: nested rule/check rows, nil-vs-present checks,
  dynamic state/flag reads, and dynamic flag writes.
- `procgen_room_scoring`: indexed room iteration, best-index selection,
  chosen-row mutation, and depth feedback.
- `save_state_diff`: paired before/after row comparison, dynamic inventory
  field lists, absolute deltas, and repeated diff passes.
- `path_relaxation`: graph node/edge traversal, `nodes[edge.to]` indexed row
  lookup, blocked checks, and distance relaxation.

Focused evidence from:

```sh
go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=250ms -count=1 ./...
```

shows the newest rows are the current closure problem. This single run is
directional, not a stable retirement gate:

| Case | Ember ns/op | Luau ns/run | Ratio | Immediate Pressure |
| --- | ---: | ---: | ---: | --- |
| `combat_tick` | 12,628 | 8,824 | 1.43x | old-row closure |
| `inventory_value` | 17,993 | 11,999 | 1.50x | old-row closure |
| `event_dispatch` | 26,510 | 15,997 | 1.66x | old-row closure |
| `buff_stack_tick` | 8,909 | 11,267 | 0.79x | guard against regression |
| `ability_resolution` | 12,440 | 13,736 | 0.91x | guard against regression |
| `ai_utility_scoring` | 36,252 | 73,889 | 0.49x | guard against regression |
| `cooldown_scheduler` | 300,904 | 42,378 | 7.10x | nested scheduler state |
| `projectile_sweep` | 148,414 | 23,300 | 6.37x | collision and early break |
| `quest_progress_update` | 168,691 | 17,244 | 9.78x | nested objectives |
| `behavior_tree_tick` | 112,872 | 22,199 | 5.08x | indexed while traversal |
| `threat_aggro_table` | 759,396 | 69,449 | 10.93x | dynamic threat maps |
| `economy_market_tick` | 914,278 | 73,850 | 12.38x | dynamic market maps |
| `formation_layout_score` | 1,087,164 | 156,991 | 6.93x | nested reductions |
| `dialogue_condition_eval` | 236,748 | 23,176 | 10.21x | dynamic flags/checks |
| `procgen_room_scoring` | 250,338 | 33,223 | 7.53x | best-index mutation |
| `save_state_diff` | 567,965 | 53,658 | 10.58x | dynamic paired diffs |
| `path_relaxation` | 358,325 | 31,900 | 11.23x | graph edge relaxation |

The optimization target is still reusable shapes, not benchmark-specific
recognizers. The new dominant reusable shapes are dynamic finite-key subtables,
indexed row lookup, nested state machines, early-exit loop regions, paired-row
diffs, and graph-like relaxation.

## Phase 17: Full Corpus Accounting

Purpose: make the seventeen-row corpus the thing the project actually gates,
profiles, budgets, and burns down. Any plan text that still says six rows is
historical context only.

Exit gate:

- all seventeen Scenario rows are required by the ratio gate;
- all seventeen rows have allocation budgets or an explicit temporary waiver;
- each newest row has a profile, hot-opcode list, and shape classification;
- the current worst-ratio row is selected mechanically from the full corpus.

### Slice 61: Seventeen-Row Required Corpus Gate

- **Depends:** current Scenario benchmark corpus.
- **Behavior:** no runtime behavior change.
- **Interface:** the ratio gate knows the expected Scenario row set and fails
  when any row is missing.
- **Implementation:** update `scripts/scenario-ratio-gate` or its companion
  ledger so it treats all seventeen row names as required benchmark evidence.
- **Bench:** full Scenario ratio command piped through
  `SCENARIO_RATIO_MAX=1.3 scripts/scenario-ratio-gate`.
- **Done:** a run that omits any new benchmark row cannot pass as a complete
  Scenario performance claim.

### Slice 62: New Row Allocation Budgets

- **Depends:** Slice 61.
- **Behavior:** no runtime behavior change.
- **Interface:** allocation-budget tests cover the eleven newest Scenario rows.
- **Implementation:** add initial budgets from repeated stable runs for
  `cooldown_scheduler`, `projectile_sweep`, `quest_progress_update`,
  `behavior_tree_tick`, `threat_aggro_table`, `economy_market_tick`,
  `formation_layout_score`, `dialogue_condition_eval`,
  `procgen_room_scoring`, `save_state_diff`, and `path_relaxation`.
- **Bench:** `TestScenarioEmberRunAllocationBudgets`.
- **Done:** future optimizations cannot quietly add allocation churn to the
  rows that are currently slowest.

### Slice 63: Full-Corpus Ratio Ledger

- **Depends:** Slice 62.
- **Behavior:** no runtime behavior change.
- **Interface:** the benchmark ledger records same-run Ember/Luau ratios for
  all Scenario rows and ranks them by ratio.
- **Implementation:** record a stable `-count=3` full-corpus run, mark noisy
  rows, and name the top five ratio gaps before any new optimization work.
- **Bench:** full Scenario ratio command with `-benchtime=1s -count=3`.
- **Done:** the next implementation slice starts from measured worst rows, not
  from intuition.

### Slice 64: Full-Corpus Shape Report

- **Depends:** Slice 63.
- **Behavior:** no runtime behavior change.
- **Interface:** artifact reporting classifies each row by reusable runtime
  shape: dense row array, nested row array, finite-key subtable, indexed row
  lookup, early-exit loop, paired-row diff, graph relaxation, or reduction.
- **Implementation:** extend the private artifact report and benchmark ledger
  with shape tags and rejected descriptor reasons for every new row.
- **Bench:** no expected speed change; run full Scenario ratio gate to ensure
  reporting does not leak into runtime.
- **Done:** every future slice can point to a shape class instead of a
  benchmark name.

## Phase 18: Dynamic Finite-Key Subtables

Purpose: attack the new worst class: tables accessed through dynamic string
keys drawn from a small known set. This covers threat maps, market maps,
dialogue flags, and inventory subtables.

Exit gate:

- `economy_market_tick`, `threat_aggro_table`, `dialogue_condition_eval`, and
  `save_state_diff` each improve materially without regressing table semantics;
- dynamic-key fast paths fall back on unknown keys, metatables, nil deletion,
  or shape/version mismatch;
- `table_fields`, `metatable_index`, and current Scenario guard rows stay
  within policy.

### Slice 65: Finite-Key Subtable Facts

- **Depends:** Slice 64.
- **Behavior:** dynamic string-key programs return identical results.
- **Interface:** the artifact can describe a subtable with a finite expected
  string-key set even when the accessed key comes from another row.
- **Implementation:** verify facts for maps such as `enemy.threat`,
  `market.stock`, `market.demand`, `market.price`, `state.flags`, and
  `row.inv`, including key-set and table-version guards.
- **Bench:** `economy_market_tick`, `threat_aggro_table`,
  `dialogue_condition_eval`, `save_state_diff`, `table_fields`.
- **Done:** dynamic subtable reads have a verifier-backed fast-path candidate.

### Slice 66: Direct Dynamic-Key Reads

- **Depends:** Slice 65.
- **Behavior:** missing-key and nil behavior remains Luau-shaped.
- **Interface:** one private helper resolves a dynamic string key against a
  verified finite-key subtable without exposing map internals to the VM switch.
- **Implementation:** specialize reads like `market.stock[good]`,
  `enemy.threat[actor.id]`, `state.flags[check.key]`, and `left.inv[field]`
  using key identity plus version checks, with generic fallback.
- **Bench:** `economy_market_tick`, `threat_aggro_table`,
  `dialogue_condition_eval`, `save_state_diff`.
- **Done:** profiles show dynamic map reads moving out of generic raw field
  lookup for the target rows.

### Slice 67: Direct Dynamic-Key Writes

- **Depends:** Slice 66.
- **Behavior:** assigning existing keys, creating new keys, and nil deletion
  preserve table semantics.
- **Interface:** dynamic finite-key write descriptors distinguish verified
  existing-key updates from shape-changing writes.
- **Implementation:** specialize writes such as `enemy.threat[event.actor] =`,
  `market.stock[good] =`, `market.price[good] =`, and
  `state.flags[rule.flag] = true`, falling back when a write adds a key outside
  the verified set.
- **Bench:** target dynamic-key rows plus table mutation invalidation tests.
- **Done:** dynamic map writes no longer force every hot update through the
  generic table path when the shape is stable.

### Slice 68: Dynamic-Key Row Fusion

- **Depends:** Slice 67.
- **Behavior:** target rows keep exact results.
- **Interface:** fused loop helpers consume finite-key subtable reads/writes
  through the same descriptor family, not through per-row special cases.
- **Implementation:** fuse the hottest dynamic-key loop bodies for
  `economy_market_tick`, `threat_aggro_table`, `dialogue_condition_eval`, and
  `save_state_diff` around direct map operations and numeric accumulation.
- **Bench:** Phase 18 target rows plus current-row guard.
- **Done:** the Phase 18 target rows each move toward the full-corpus burn-down
  target and the descriptor family remains reusable.

## Phase 19: Indexed State Machines And Early Exit

Purpose: cover loops where control flow depends on indexed rows, dynamic node
selection, early `break`, or mutable live/blocked flags.

Exit gate:

- `projectile_sweep`, `behavior_tree_tick`, `path_relaxation`, and
  `cooldown_scheduler` improve materially;
- optimized paths preserve `break`, while-loop, indexed lookup, and mutation
  semantics;
- old direct-frame and row-loop wins do not regress.

### Slice 69: Cooldown Scheduler Nested State Path

- **Depends:** Slice 64 and existing singleton/nested row facts.
- **Behavior:** `cooldown_scheduler` still returns `13075`.
- **Interface:** nested scheduler facts describe actor rows, child ability
  arrays, resource slots, cooldown slots, reset slots, and use counters.
- **Implementation:** fuse actor energy refill, cooldown decrement/clamp,
  readiness predicate, use counter update, reset, and score accumulation.
- **Bench:** `cooldown_scheduler`, `ability_resolution`, current-row guard.
- **Done:** cooldown/resource state-machine logic is reusable across scheduler
  and ability workloads.

### Slice 70: Behavior Tree Indexed While Path

- **Depends:** Slice 64 and dynamic-key read support if available.
- **Behavior:** `behavior_tree_tick` still returns `7252`.
- **Interface:** loop-region facts can describe indexed row lookup
  `nodes[index]`, bounded while traversal, and dynamic blackboard reads.
- **Implementation:** specialize condition-node traversal, action-node
  dispatch, blackboard slot reads/writes, depth bounds, and index changes with
  fallback on shape or bounds mismatch.
- **Bench:** `behavior_tree_tick`, `while_branching`, dynamic-key guard rows.
- **Done:** behavior-tree traversal no longer pays full generic lookup and
  branch overhead for each node step.

### Slice 71: Projectile Collision Early-Break Path

- **Depends:** Slice 64.
- **Behavior:** `projectile_sweep` still returns `413`, including early
  `break` and live flag updates.
- **Interface:** nested loop facts can represent an inner-loop `break` that
  exits to the correct outer-loop continuation.
- **Implementation:** fuse projectile position updates, target collision math,
  hp/damage mutation, live flag writes, break handling, bounds checks, and
  final target scoring.
- **Bench:** `projectile_sweep`, `combat_tick`, `array_ops`.
- **Done:** early-exit nested loops have an explicit safe optimization shape.

### Slice 72: Graph Edge Relaxation Path

- **Depends:** Slice 64.
- **Behavior:** `path_relaxation` still returns `4286`.
- **Interface:** indexed row lookup facts support `nodes[edge.to]` with bounds,
  row-shape, blocked-flag, and distance-slot guards.
- **Implementation:** specialize node iteration, edge iteration, next-node
  lookup, blocked checks, candidate distance math, conditional distance store,
  and periodic direct indexed mutations.
- **Bench:** `path_relaxation`, `generic_iteration`, `array_ops`.
- **Done:** graph-like row traversal has a reusable fast path rather than a
  benchmark-specific relaxation helper.

## Phase 20: Nested Predicate And Reduction Workloads

Purpose: optimize the remaining broad class of nested scoring/diff workloads:
quest objective updates, formation scoring, procgen room selection, and paired
state diffing.

Exit gate:

- `quest_progress_update`, `formation_layout_score`,
  `procgen_room_scoring`, and `save_state_diff` improve materially;
- reduction descriptors cover max, best-index, complete-all, and absolute-delta
  accumulation;
- no optimization narrows nil, equality, or numeric semantics.

### Slice 73: Quest Objective Completion Path

- **Depends:** Slice 64 and intrinsic guard support.
- **Behavior:** `quest_progress_update` still returns `419`.
- **Interface:** nested objective facts represent event x quest x objective
  loops, completion booleans, and guarded `math.min` updates.
- **Implementation:** fuse event/objective string matching, objective progress
  update, completion reduction, quest done mutation, and score accumulation.
- **Bench:** `quest_progress_update`, `ability_resolution`, host override
  tests.
- **Done:** nested all-complete reductions have a verified reusable shape.

### Slice 74: Formation Layout Reduction Path

- **Depends:** Slice 64 and reduction descriptor support.
- **Behavior:** `formation_layout_score` still returns `14194`.
- **Interface:** reduction facts support best-score selection inside unit x
  slot loops with absolute-delta math.
- **Implementation:** fuse unit/slot direct row reads, absolute dx/dy
  normalization, role match dispatch, best-score update, unit position update,
  and wraparound mutation.
- **Bench:** `formation_layout_score`, `ai_utility_scoring`,
  `procgen_room_scoring`.
- **Done:** max-reduction optimization covers layout scoring without new
  one-off VM branches.

### Slice 75: Procgen Best-Index Mutation Path

- **Depends:** Slice 74.
- **Behavior:** `procgen_room_scoring` still returns `-725`.
- **Interface:** best-index reduction facts track both best score and selected
  row index, then allow a guarded chosen-row mutation after the loop.
- **Implementation:** specialize room scoring, best score/index update,
  chosen-room load, danger/loot mutation, depth feedback, and total
  accumulation.
- **Bench:** `procgen_room_scoring`, `formation_layout_score`,
  `generic_iteration`.
- **Done:** selected-row mutation after a reduction is an explicit artifact
  capability.

### Slice 76: Paired Row Diff Path

- **Depends:** Slice 68 if dynamic inventory fields are already optimized.
- **Behavior:** `save_state_diff` still returns `9090`.
- **Interface:** paired-row facts describe two dense arrays traversed by the
  same index plus nested dynamic field lists.
- **Implementation:** fuse before/after row lookup, hp/zone comparison,
  absolute deltas, dynamic inventory field reads, and weighted accumulation.
- **Bench:** `save_state_diff`, dynamic-key rows, `table_fields`.
- **Done:** repeated save/diff style workloads no longer hit generic table
  access for every paired field.

## Phase 21: Full-Corpus 1.3x Closure

Purpose: finish against the actual benchmark set while keeping the optimization
layer smaller than the workload catalog.

Exit gate:

- every Scenario row is <= `1.30x` same-run Luau batch;
- all rows have allocation budgets and pass them;
- current Top10 and Classic rows do not regress;
- descriptor families are reusable and documented.

### Slice 77: Full-Corpus Worst-Ratio Burn-Down

- **Depends:** Slices 68, 72, and 76.
- **Behavior:** no behavior change beyond accepted optimizations.
- **Interface:** the burn-down script ranks all current Scenario rows by
  same-run ratio and selects the next row mechanically.
- **Implementation:** optimize one measured blocker at a time, rerun the full
  ratio gate, and record rejected probes before moving to the next ratio gap.
- **Bench:** full Scenario ratio gate with `-benchtime=1s -count=3`.
- **Done:** no Scenario row remains above `1.50x`.

### Slice 78: Old-Win Regression Wall

- **Depends:** Slice 77.
- **Behavior:** prior wins for `buff_stack_tick`, `ability_resolution`, and
  `ai_utility_scoring` remain intact.
- **Interface:** current-row guard separates old optimized rows, new optimized
  rows, Top10 rows, and Classic rows in the ledger.
- **Implementation:** add explicit guard rows for previous wins that now run
  faster than Luau in noisy samples, so new shared changes cannot spend those
  gains accidentally.
- **Bench:** full Scenario ratio gate, current-row guard, shared VM/table/call
  guard.
- **Done:** closing new rows does not reopen old rows.

### Slice 79: Descriptor Family Deletion Audit

- **Depends:** Slice 78.
- **Behavior:** no runtime behavior change except deleting unused or losing
  paths.
- **Interface:** every accepted optimization belongs to one descriptor family:
  dense row array, nested row array, singleton state row, finite-key subtable,
  indexed row lookup, early-exit loop, paired row, graph relaxation, or
  reduction.
- **Implementation:** remove any descriptor or helper that only names a
  benchmark and cannot prove a reusable shape.
- **Bench:** full Scenario ratio gate and current-row guard.
- **Done:** the VM/artifact interface is deep enough to support seventeen rows
  without seventeen special cases.

### Slice 80: Full Scenario Retirement

- **Depends:** Slice 79.
- **Behavior:** no runtime behavior change.
- **Interface:** docs and scripts state the final Scenario corpus, final ratio
  evidence, allocation budgets, compatibility guards, and accepted private
  optimization seams.
- **Implementation:** update this plan, benchmark ledger, and any relevant ADR
  notes with final evidence and rejected experiments.
- **Bench:** full Scenario ratio gate, current-row guard,
  `scripts/check-lane root`, `scripts/check-fast`, and `scripts/check`.
- **Done:** the plan retires only when all current Scenario rows are <=
  `1.30x` and current benchmarks do not regress.
