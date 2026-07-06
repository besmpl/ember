# Plan: Scenario Region Execution Pivot

## Goal

Move Ember from the current 2x to 7x Scenario benchmark gap toward the target
of every row at or below 2.0x slower than same-run `luau_cli_batch`.

This plan starts from the Phase 5.2 lesson in
`scenario-under-2x-speed-plan.md`: table facts, path plans, and small typed
direct-block families are real progress, but they have not changed the cost
model enough. The next speed layer needs deeper modules that execute verified
regions with local state, cheap table slots, and cheaper frame/result movement.

## Current Diagnosis

The latest external diagnosis recorded these worst same-run ratios:

| Scenario row | Ratio |
| --- | ---: |
| `path_relaxation` | 6.93x |
| `quest_progress_update` | 6.57x |
| `cooldown_scheduler` | 6.28x |
| `event_dispatch` | 6.08x |
| `dialogue_condition_eval` | 5.98x |
| `economy_market_tick` | 5.97x |
| `threat_aggro_table` | 5.61x |
| `save_state_diff` | 5.52x |
| `projectile_sweep` | 5.36x |
| `ai_utility_scoring` | 5.11x |

The important pattern is not the exact ratio of one row. The rows cluster
around ordinary runtime shape:

- direct-frame dispatch still dominates profiles;
- current block families are too small and shallow;
- table/path facts exist but are not cheap enough to consume;
- dynamic string fields still pay Go string/map costs;
- stable calls still copy arguments/results and manage frames too much;
- wide `Value` movement, register clearing, and allocation remain visible;
- intrinsic/global guards are not being amortized inside verified regions.

The weakest old assumption is that enough small guarded superblock families
would amortize interpreter overhead. The new thesis is that Ember needs a
deeper verified region execution layer, not another helper around a few
bytecodes.

## Scope

In:

- Private execution artifact, verifier, direct-frame, table, call/frame, and
  intrinsic/global guard mechanisms.
- Pure-Go plan-specific region executors.
- Shape/symbol table storage for hot finite string-key tables.
- Frame/register/result-window lifetime work.
- Measurement gates that prove coverage and profitability before broad rollout.

Out:

- Benchmark-named helpers.
- Scenario-row-specific opcodes.
- Whole-source or whole-benchmark recognizers.
- A generic tracelet mini-VM that recreates `runDirectFrame` under another name.
- Native codegen or unsafe `Value` layout as the first move.
- Public `Compile`, `Run`, or `RunWithGlobals` interface expansion for private
  performance work.

## Design Thesis

The next deep module should be a private **region execution module**.

Its interface should stay small:

```text
regionPlanAt(pc) -> nil or verified region plan
executeRegion(thread, frame, plan) -> next pc or side exit
```

Its implementation should hide a lot:

- loop and region selection;
- guard prelude construction;
- table slot resolution;
- register read/write sets;
- local numeric lanes;
- side-exit register repair;
- instruction budget accounting;
- debug/yield/call rejection;
- fallback to normal direct-frame dispatch.

The key design rule: do **not** implement a generic region interpreter. Region
plans should route to plan-specific executors, such as row loops, dynamic
finite-map updates, stable script-call regions, and intrinsic-hoisted numeric
regions. That is where the depth and leverage live.

The supporting deep modules are:

- **Table shape/symbol module**: turns stable string fields into shape and slot
  loads instead of repeated Go string/map lookup.
- **Frame window module**: makes stable calls borrow arg/result windows and
  materialize only on side exit.
- **Register/value lifetime module**: reduces broad `Value` copies, result
  movement, and register clearing.
- **Intrinsic/global binding module**: proves globals once per verified region,
  not once per bytecode.

## Phase 0: Decision-Grade Evidence

### Slice 0.1: Authoritative Ratio Refresh

Module: benchmark harness and performance ledger.

Seam and interface: same-run Scenario benchmark output consumed by
`scripts/scenario-ratio-gate`.

Implementation complexity hidden: benchmark setup, same-run Luau comparison,
ratio parsing, and noise management.

Leverage: prevents this pivot from optimizing stale rows.

Checks:

```sh
go test -run '^$' \
  -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' \
  -benchmem \
  -benchtime=1s \
  -count=3 ./... \
  | tee /tmp/ember-scenario-current.out

SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-scenario-current.out
```

Done when the current worst-row cluster is known from fresh same-run data.

Abandon or reorder if the ratios have already moved enough that a narrower
Phase 6/7 from the old plan is more appropriate.

### Slice 0.2: Full Mechanism Attribution

Module: private mechanism counter module.

Seam and interface: opt-in counters attached to direct-frame execution and
execution artifacts, with output grouped by mechanism rather than Scenario row.

Implementation complexity hidden:

- retired bytecodes by opcode family;
- bytecodes covered by direct blocks and candidate regions;
- guard checks by guard kind and plan family;
- side exits by region family, pc, and reason;
- table raw string access split by inline slot versus map-backed storage;
- string compare/hash proxy counters;
- fixed-call frame allocations, clears, arg copies, and result copies;
- allocation attribution inside timed `Run`.

Leverage: this is the deletion test for the old plan. If these counters vanish,
complexity reappears as guesses across every future slice.

Checks:

```sh
go test -count=1 ./... -run 'Mechanism|Attribution|BenchmarksMatch'
```

Done when the worst rows can be explained by mechanism, not just by ratio.

Abandon or reorder if disabled counters perturb normal runs or enabled counters
cannot explain most hot work.

### Slice 0.3: CPU And Allocation Profiles For Representative Rows

Module: profiling workflow, not production runtime.

Seam and interface: pprof and benchmem output for individual Scenario rows.

Rows:

- `event_dispatch`;
- `cooldown_scheduler`;
- `economy_market_tick`;
- `formation_layout_score`;
- `path_relaxation`;
- `save_state_diff`.

Checks:

```sh
go test -run '^$' \
  -bench 'BenchmarkScenarioLuau/(event_dispatch|cooldown_scheduler|economy_market_tick|formation_layout_score|path_relaxation|save_state_diff)/ember_run$' \
  -benchmem \
  -benchtime=1s \
  -count=1 \
  -cpuprofile /tmp/ember-scenario.cpu.pprof .

go tool pprof -top -nodecount=40 /tmp/ember-scenario.cpu.pprof
```

Done when `runDirectFrame`, table lookup, call/frame work, copy/clear, and
allocation shares are known well enough to order the next slices.

## Phase 1: Region Coverage And Profitability Model

### Slice 1.1: Region Coverage Dry Run

Module: region planner.

Seam and interface:

```text
candidateRegions(proto, counters) -> coverage report
```

The coverage report should include:

- candidate region kind;
- entry pc and exit pc;
- retired bytecodes covered;
- required guards;
- side-exit points;
- register repair size;
- table slots required;
- calls or intrinsics inside the region.

Implementation complexity hidden:

- loop header discovery;
- bytecode range grouping;
- register liveness;
- table/path descriptor dependencies;
- rejection around calls, yields, hooks, open returns, and uncertain effects.

Leverage: tells whether specialized region execution can cover enough of the
worst rows before building executors.

Correctness hazards: over-reporting eligibility. A dry-run planner that claims
unsafe regions is worse than no planner.

Tests and gates:

```sh
go test -count=1 ./... -run 'Region|Coverage|BenchmarksMatch'
```

Proceed if candidate regions cover at least 50% of retired bytecodes on most
worst rows, with a strong target of 65% to 75% for loop-heavy rows.

Abandon or reorder if coverage is below 40% on most worst rows. In that case,
table/frame/value layout work should move first.

### Slice 1.2: Guard And Repair Cost Model

Module: same region planner.

Seam and interface:

```text
estimateRegionCost(candidate) -> guard cost, repair cost, expected saved work
```

Implementation complexity hidden:

- guard count by kind;
- guard placement;
- register writeback map size;
- side-exit repair size;
- expected dispatch removed;
- expected table/string lookup removed.

Leverage: prevents a repeat of Phase 5.2, where facts existed but plan
execution did not pay for itself.

Correctness hazards: profitability estimates must be conservative. They should
reject uncertain regions rather than bless slow ones.

Done when every candidate region has a reason to exist beyond "it matches a
pattern".

## Phase 2: Plan-Specific Region Execution Module

### Slice 2.1: Region Executor Interface

Module: private region execution module.

Seam and interface:

```text
executeRegion(thread, frame, plan) -> next pc or side exit
```

The interface includes the observable invariants:

- exact PC on continuation or side exit;
- exact register state on side exit;
- exact instruction budget accounting;
- no execution across debug hooks, yields, open returns, or unsafe calls;
- fallback to ordinary direct-frame dispatch on guard failure.

Implementation complexity hidden:

- typed plan payload dispatch;
- guard prelude execution;
- local state setup;
- side-exit repair;
- budget accounting;
- mechanism counters.

Leverage: this gives all later region families one seam for executing verified
work without teaching `runDirectFrame` every detail.

Correctness hazards:

- generic mini-VM creep;
- side-exit state drift;
- accidentally executing unsafe regions;
- exposing table/path internals through the interface.

Checks:

```sh
go test -count=1 ./... -run 'Region|SideExit|Direct|BenchmarksMatch'
```

Done when a no-op or tiny region can enter, exit, side-exit, and count without
changing semantics.

### Slice 2.2: Array-Of-Records Loop Executor

Module: region execution module.

Seam and interface: a typed row-loop plan consumed by a row-loop executor.

The plan should describe:

- array/table iteration source;
- row register;
- stable field slots;
- local numeric lanes;
- branch exits;
- writeback registers;
- fallback pc;
- guard prelude.

Implementation complexity hidden:

- array iteration;
- row shape guards;
- slot loads by small integer;
- local numeric accumulator handling;
- loop-carried register updates;
- side-exit repair.

Leverage rows:

- `formation_layout_score`;
- `ai_utility_scoring`;
- `projectile_sweep`;
- `procgen_room_scoring`;
- `path_relaxation`;
- `cooldown_scheduler`;
- `economy_market_tick`;
- `threat_aggro_table`.

Correctness hazards:

- array holes;
- row table mutation;
- nil versus missing fields;
- metatable invalidation;
- integer/float behavior;
- debug hook and yield barriers.

Tests and gates:

```sh
go test -count=1 ./... -run 'Region|Array|Row|SideExit|BenchmarksMatch'
go test -run '^$' \
  -bench 'BenchmarkScenarioLuau/(formation_layout_score|ai_utility_scoring|projectile_sweep|procgen_room_scoring|path_relaxation|cooldown_scheduler)/(ember_run|luau_cli_batch)$' \
  -benchmem \
  -benchtime=500ms \
  -count=3 ./...
```

Proceed if target rows improve at least 20% Ember-side and
`runDirectFrame` flat CPU drops materially.

Abandon or narrow if side exits exceed 1% to 5% on stable rows or if guard
prelude cost dominates saved dispatch.

### Slice 2.3: Dynamic Finite-Map Update Executor

Module: region execution module plus table shape/symbol module.

Seam and interface: a typed dynamic update plan:

```text
parent slot guard
dynamic key source
child table guard
operation: read, add/sub, writeback
fallback pc
```

Implementation complexity hidden:

- parent path guard;
- dynamic key symbol resolution;
- child slot lookup;
- local numeric operation;
- writeback;
- mutation invalidation;
- non-string key fallback.

Leverage rows:

- `economy_market_tick`;
- `threat_aggro_table`;
- `save_state_diff`;
- `dialogue_condition_eval`;
- `path_relaxation`;
- `cooldown_scheduler`.

Correctness hazards:

- dynamic key mutation;
- nil parent;
- metatable parent or child;
- aliasing between paths;
- non-number operands;
- side exit before mutation.

Gate: this executor must improve more than the Phase 5.2 dynamic path block.
If it only removes a few dispatches and remains flat, it is still too shallow.

### Slice 2.4: Predicate-Chain And Numeric-Reduction Executors

Module: region execution module.

Seam and interface: typed plans for branch-heavy predicate chains and
numeric reductions with local lanes and branch exits.

Implementation complexity hidden:

- truthiness;
- nil and false distinction;
- numeric corner cases;
- local accumulator storage;
- branch repair;
- table slot reads.

Leverage rows:

- `dialogue_condition_eval`;
- `quest_progress_update`;
- `inventory_value`;
- `combat_tick`;
- `ability_resolution`;
- `formation_layout_score`;
- `procgen_room_scoring`.

Abandon if the coverage model shows these regions are too fragmented or guard
density is too high.

## Phase 3: Shape/Symbol Table Storage

### Slice 3.1: Symbol IDs For Hot String Keys

Module: table shape/symbol module.

Seam and interface:

```text
internSymbol(string) -> SymbolID
```

The compiler/runtime may intern constants and hot dynamic keys, but callers
still use ordinary Luau strings semantically.

Implementation complexity hidden:

- symbol table lifetime;
- constant-string interning;
- dynamic-key lazy interning;
- equality between interned and non-interned strings;
- memory growth controls.

Leverage: regions need cheap key identity. Repeated Go string comparison and
hashing should not sit in the hottest loop bodies.

Correctness hazards:

- string equality semantics;
- memory retention;
- host-created strings;
- dynamic keys that are not strings.

Checks:

```sh
go test -count=1 ./... -run 'String|Symbol|Table|BenchmarksMatch'
```

### Slice 3.2: Shape And Slot Vectors For Finite String Tables

Module: private `Table` module.

Seam and interface:

```text
tableShape(table) -> ShapeID
lookupSlot(shape, symbol) -> SlotID or miss
loadSlot(table, slot) -> Value
storeSlot(table, slot, value) -> ok or miss
```

Implementation complexity hidden:

- shape transitions;
- nil deletion;
- dynamic field insertion;
- inline versus vector storage;
- fallback to current general table representation;
- next/pairs interaction;
- metatable invalidation.

Leverage rows:

- `economy_market_tick`;
- `threat_aggro_table`;
- `dialogue_condition_eval`;
- `save_state_diff`;
- `quest_progress_update`;
- `cooldown_scheduler`;
- `path_relaxation`;
- `inventory_value`.

Correctness hazards:

- nil deleting fields;
- iteration order where observable;
- dynamic mutation during iteration;
- metatables;
- mixed array/hash tables;
- shape explosion.

Checks and gates:

```sh
go test -count=1 ./... -run 'Table|Shape|Slot|rawget|rawset|metatable|BenchmarksMatch'
go test -run '^$' \
  -bench 'BenchmarkScenarioLuau/(economy_market_tick|threat_aggro_table|dialogue_condition_eval|save_state_diff|quest_progress_update|cooldown_scheduler|path_relaxation)/(ember_run|luau_cli_batch)$' \
  -benchmem \
  -benchtime=500ms \
  -count=3 ./...
```

Proceed if string/map samples and dynamic table rows move materially.

Abandon or delay if pprof after Phase 2 shows string/map lookup below 5% on all
worst rows.

### Slice 3.3: Region-Owned Slot Binding

Module: table shape/symbol module plus region planner.

Seam and interface: region plans bind shape/slot descriptors at region entry,
then use slot IDs inside the executor.

Implementation complexity hidden:

- descriptor staleness;
- shape guard failure;
- fallback to general table access;
- mutation invalidation.

Leverage: this converts table metadata into actual region speed. Descriptor
plumbing that is not consumed by a region executor does not count.

## Phase 4: Stable Calls And Frame Windows

### Slice 4.1: No-Copy Stable Script Calls

Module: call/frame window module.

Seam and interface:

```text
fixedCallPlan(pc) -> callee guard, arg window, result window, clear mask
executeFixedCall(thread, callerFrame, plan) -> next pc or materialized fallback
```

Implementation complexity hidden:

- borrowed arg windows;
- borrowed result windows;
- frame reuse;
- recursion and reentrancy;
- fallback materialization;
- hooks, yields, protected calls, and errors.

Leverage rows:

- `event_dispatch`;
- `behavior_tree_tick`;
- method-call benchmarks;
- recursive/fixed-call benchmarks.

Correctness hazards:

- recursive frame aliasing;
- yielded calls;
- debug hooks;
- protected-call behavior;
- varargs;
- multiple returns;
- upvalues and captured locals;
- error tracebacks.

Checks and gates:

```sh
go test -count=1 ./... -run 'Call|Frame|Yield|Hook|Protected|Vararg|BenchmarksMatch'
go test -run '^$' \
  -bench 'BenchmarkScenarioLuau/(event_dispatch|behavior_tree_tick)/(ember_run|luau_cli_batch)$' \
  -benchmem \
  -benchtime=500ms \
  -count=3 ./...
```

Proceed if `event_dispatch` improves at least 25% and stable-call arg/result
copy counters approach zero.

Abandon or reorder if removing copies moves `event_dispatch` less than 10%.

### Slice 4.2: Minimal Frame Clear Masks

Module: frame/register lifetime module.

Seam and interface: a private clear mask produced by the verifier or call plan,
consumed by frame setup and region exits.

Implementation complexity hidden:

- GC reachability;
- stale reference clearing;
- dead register detection;
- error-path materialization;
- side-exit repair.

Leverage: every hot call and many region exits currently pay broad clear/copy
costs.

Correctness hazards:

- retaining references too long;
- failing to clear dead references;
- changing debug/error-visible state;
- aliasing caller and callee windows incorrectly.

Gate: CPU samples for `memclr`, `duffzero`, and frame setup should drop without
increasing B/op or allocs/op.

## Phase 5: Register And Value Movement Reduction

### Slice 5.1: Local Numeric Lanes Inside Regions

Module: region execution module.

Seam and interface: region plans declare numeric lanes and writeback registers.

Implementation complexity hidden:

- Value to numeric conversion;
- NaN and infinity behavior;
- integer/float compatibility;
- guard failure and side exit;
- final writeback.

Leverage rows:

- `formation_layout_score`;
- `ai_utility_scoring`;
- `procgen_room_scoring`;
- `combat_tick`;
- `ability_resolution`;
- `path_relaxation`;
- `economy_market_tick`.

Gate: `Value` copy volume and `runDirectFrame` flat CPU should drop on numeric
rows. This should happen before considering unsafe or compact `Value` work.

### Slice 5.2: Register Copy And Result Movement Audit

Module: frame/register lifetime module.

Seam and interface: counters for register copies, result copies, frame clears,
and materializations by call site and region family.

Leverage: decides whether safe lifetime changes are enough or whether Ember
needs a deeper `Value` layout pivot.

Proceed to a compact/unsafe `Value` investigation only if, after Phases 2 to 4:

- `duffcopy`, `duffzero`, or `memclr` remains above 15% to 20% in representative
  profiles;
- worst rows remain above 2.5x;
- region coverage is high but executor overhead remains high.

## Phase 6: Intrinsic And Global Binding In Regions

### Slice 6.1: Region-Entry Intrinsic Guards

Module: intrinsic/global binding module.

Seam and interface:

```text
bindIntrinsic(region, symbol) -> guarded intrinsic binding or reject
```

Implementation complexity hidden:

- `_ENV` mutation;
- library table replacement;
- function identity;
- result arity;
- fallback call behavior;
- error behavior.

Leverage rows:

- `combat_tick`;
- `ability_resolution`;
- `quest_progress_update`;
- `buff_stack_tick`;
- `economy_market_tick`.

Gate: intrinsic guard checks should become region-entry checks, and stable
regions should not show repeated intrinsic misses.

Abandon or keep secondary if intrinsic/global samples are tiny after region and
table work.

## Phase 7: Generated Pure-Go Executor Pivot Gate

This is not the first implementation step. It is the next pivot if hand-written
plan-specific executors prove coverage but still cannot reach the target.

### Slice 7.1: Generated Executor Feasibility Memo

Module: region execution module with a future generated adapter.

Seam and interface: keep the same region plan and side-exit interface, but add
an adapter that can execute a plan through generated pure-Go code.

Implementation complexity hidden:

- code generation;
- build/cache invalidation;
- source maps or debug names;
- side-exit repair;
- fallback to interpreted direct-frame execution.

Proceed only if:

- region coverage is high;
- hand-written executors improve rows but leave worst rows above 2.5x;
- profiles show generic executor overhead, `Value` movement, or dispatch inside
  the region executor remains high.

Rejected before this gate:

- native/JIT first;
- unsafe `Value` first;
- generic tracelet interpreter;
- benchmark-specific code generation.

## Execution Order

0. Refresh ratios, attribution, CPU profiles, and allocation profiles.
1. Build region coverage and profitability dry-run counters.
2. Add the region executor seam without a generic mini-VM.
3. Implement the array-of-records loop executor.
4. Implement the dynamic finite-map update executor.
5. Add shape/symbol table slots and bind them into region plans.
6. Pull stable-call frame windows earlier if `event_dispatch` still dominates.
7. Add local numeric lanes and frame clear masks.
8. Hoist intrinsics/globals at region entry.
9. Consider generated pure-Go executors only after the above evidence.

## Acceptance Gates

Every implementation slice should pass focused tests first, then:

```sh
scripts/check-lane root
scripts/check-fast
scripts/check
```

Performance acceptance:

```sh
go test -run '^$' \
  -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' \
  -benchmem \
  -benchtime=1s \
  -count=3 ./... \
  | tee /tmp/ember-scenario-current.out

scripts/scenario-ratio-gate < /tmp/ember-scenario-current.out
```

Mechanism acceptance:

- at least 60% to 70% of retired hot-loop bytecodes execute inside profitable
  region executors on former worst rows;
- `runDirectFrame` flat CPU drops below roughly 25% to 30% on representative
  former worst rows;
- region side exits stay below 1% on stable Scenario rows;
- guard checks are mostly region-entry checks, not per-bytecode checks;
- stable fixed-call arg/result copies approach zero;
- Go string/map samples drop materially for finite-map rows;
- allocs/op and B/op do not regress.

## Rejection Rules

Reject any change that:

- names a Scenario row in implementation;
- recognizes a whole benchmark body;
- adds another shallow block family as the main strategy;
- creates a generic region mini-VM;
- adds descriptor/fact plumbing that no executor consumes cheaply;
- exposes table storage truth through the VM switch;
- duplicates metamethod semantics in a fast path;
- expands the public runtime interface for private speed work;
- moves to native, unsafe, or generated code before pure-Go region evidence
  proves the need.

## Definition Of Success

The pivot is working when:

- all Scenario rows are at or below 2.0x same-run `luau_cli_batch`;
- no row depends on benchmark-named helpers or source recognizers;
- representative profiles show region executors, not `runDirectFrame`, as the
  useful hot work;
- table-heavy rows no longer spend visible time in repeated Go string/map
  lookup;
- `event_dispatch` no longer spends a dominant share in frame/call/result
  movement;
- allocation and copy/clear samples are lower or at least no worse.

The pivot is failing if, after region executors plus shape/symbol slots plus
frame windows, worst rows remain above 2.5x and profiles still show
`runDirectFrame`, wide `Value` movement, Go map/string cost, or frame clearing
as dominant. At that point the next serious plan is generated pure-Go executors
and, if needed, a compact or unsafe-backed `Value`/register layout.
