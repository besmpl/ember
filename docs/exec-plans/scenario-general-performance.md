# Plan: General Scenario Performance

## Goal

Bring every current Scenario benchmark row to **2.0x same-run
`luau_cli_batch` or better** without benchmark-named helpers or source-specific
recognizers.

The work should improve ordinary Luau execution patterns:

- dynamic table indexing;
- dense array iteration;
- table-of-records field access and mutation;
- nested table paths;
- base-library intrinsic calls;
- fixed-result script calls;
- numeric loop and branch bodies;
- finite-key subtables;
- indexed row traversal;
- reductions such as best-score, all-complete, and paired diffs;
- frame and allocation lifetime;
- control-flow-aware bytecode cleanup;
- stable global/import lookup;
- table storage shape IDs;
- direct-frame superblocks;
- value-list and temporary allocation lifetime;
- finite string-tag, nil, and boolean predicate facts;
- loop-local table path reuse under guarded table shape facts;
- polymorphic dynamic-key caches for finite keysets;
- value-kind facts for numbers, strings, booleans, and tables;
- direct-frame slow-path islands with precise side exits;
- composition rules for overlapping execution facts.

The stretch target after the 2.0x gate is 1.5x, then 1.3x, but slices should
optimize for shared runtime leverage first.

## Baseline

Fresh single-count same-run snapshot from 2026-07-05:

```sh
go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...
SCENARIO_RATIO_MAX=99 scripts/scenario-ratio-gate < /tmp/ember-scenario-blankpage.out
```

| Case | Ratio |
| --- | ---: |
| combat_tick | 3.81x |
| inventory_value | 5.74x |
| event_dispatch | 6.53x |
| buff_stack_tick | 6.25x |
| ability_resolution | 4.77x |
| ai_utility_scoring | 6.67x |
| cooldown_scheduler | 6.99x |
| projectile_sweep | 6.49x |
| quest_progress_update | 9.29x |
| behavior_tree_tick | 4.66x |
| threat_aggro_table | 10.93x |
| economy_market_tick | 12.47x |
| formation_layout_score | 6.98x |
| dialogue_condition_eval | 9.87x |
| procgen_room_scoring | 7.32x |
| save_state_diff | 10.54x |
| path_relaxation | 11.72x |

An economy profile showed `runGenericFrame` dominating execution. The first
blank-page finding is that ordinary `table[key]` uses `opGetIndex`, which is
not direct-frame eligible today. This is a general interpreter coverage gap,
not a Scenario-specific opportunity.

## Scenario Inspection

The Scenario sources point at repeated language shapes, not one-off benchmark
bodies. Each row below names the weakest runtime pressure and the general
module deepening that should move it.

| Case | Weakest runtime pressure in the source | General optimization to pursue |
| --- | --- | --- |
| combat_tick | dense row iteration with repeated entity field reads/writes, `alive` boolean gates, `math.min`, and numeric branch bodies | row shape propagation, hoisted `math.min` import guards, boolean predicate branches, and branch-ending superblocks |
| inventory_value | record rows with repeated numeric fields plus finite string tag checks on `item.kind` | row slot facts, numeric field-to-register facts, and finite string-tag predicate branches |
| event_dispatch | dynamic `handlers[event.kind]` script calls, fixed arguments, and state table mutation | stable table function call cache, borrowed fixed argument windows, and dynamic string-key call guards |
| buff_stack_tick | nested `entity.buffs` arrays, `rawlen`, `table.remove`, repeated `entity.buffs` loads, and `buff.kind` tag chains | loop-local table path reuse, guarded sequence intrinsics, dense mutation paths, and finite tag predicates |
| ability_resolution | cooldown/cost gates, caster/target field mutation, `math.min`, and ability tag dispatch | numeric branch coverage, row store-back facts, hoisted intrinsic guards, and finite tag predicates |
| ai_utility_scoring | nested action-target rows, best-score reduction, many numeric fields, and action kind dispatch | max reduction facts, field-to-register numeric facts, row shape propagation, and numeric/table superblocks |
| cooldown_scheduler | nested actor ability rows, repeated `actor.abilities`, cooldown store-back, and numeric clamps | loop-local table path reuse, numeric store-back facts, row mutation facts, and branch-ending superblocks |
| projectile_sweep | nested projectile-target loops, boolean `live`/hp gates, squared-distance arithmetic, and `break` | boolean predicate branches, numeric superblocks, early-exit loop facts, and row slot reads/writes |
| quest_progress_update | event-quest-objective nesting, string target/kind matches, `math.min`, and all-complete reduction | all-complete reduction facts, finite string predicates, hoisted intrinsic guards, and loop-local objective path reuse |
| behavior_tree_tick | bounded indexed traversal through `nodes[index]`, dynamic `blackboard[node.key]`, and kind dispatch | indexed row lookup facts, finite-key blackboard reads, bounded while traversal, and predicate branch facts |
| threat_aggro_table | finite threat maps indexed by event/actor ids, dynamic writes, and role tag selection | finite-key dynamic reads/writes, dynamic string-key caches, and finite string-tag predicate branches |
| economy_market_tick | nested finite maps `stock`/`demand`/`price` indexed by `order.good`, dynamic writes, and order tag branches | finite-key subtable reads/writes, dynamic-first nested stores, loop-local map path reuse, and tag predicate facts |
| formation_layout_score | unit-slot nested reduction, absolute distance normalization, role equality, and wraparound branches | max/best reduction facts, absolute-delta facts, string equality predicates, and numeric branch coverage |
| dialogue_condition_eval | nil tests on check shape, dynamic `state.flags[check.key]`, dynamic `state[check.stat]`, and boolean flag score | nil predicate branches, finite-key dynamic maps, loop-local path reuse, and boolean field predicate facts |
| procgen_room_scoring | best-score plus best-index reduction, room kind dispatch, indexed chosen-row mutation, and clamps | best-index reduction facts, indexed row lookup facts, finite tag predicates, and numeric store-back facts |
| save_state_diff | paired row traversal, dynamic inventory fields, string inequality, and absolute deltas | paired row facts, finite-key dynamic reads, absolute-delta reduction facts, and string comparison branch facts |
| path_relaxation | graph-like `node.edges`, `nodes[edge.to]`, boolean blocked gates, distance store-back, and periodic indexed writes | graph edge traversal facts, indexed row lookup facts, boolean predicate branches, and numeric store-back facts |

Cross-scenario priority:

1. Deepen `Table` storage and execution facts so row slots, finite maps, and
   nested dynamic paths share one private shape interface.
2. Add predicate facts for finite string tags, nil checks, and boolean fields;
   these appear in nearly every Scenario row and are currently too generic.
3. Add loop-local table path reuse for stable subtable fields such as
   `entity.buffs`, `actor.abilities`, `quest.objectives`, `market.stock`, and
   `node.edges`.
4. Finish numeric/reduction/superblock work after table and predicate facts make
   operand kinds stable enough for cheap guards.

## Runtime Weak Point Inspection

Code inspection adds another layer of pressure beyond the scenario scripts.
These are the weakest seams to deepen before the final burn-down:

| Area | Current weak point | General optimization direction |
| --- | --- | --- |
| Bytecode optimizer | `optimizeBytecodeIR` and `peepholeBytecode` return unchanged bytecode when any jump exists, so loop bodies keep avoidable moves and loads | finish CFG-local optimization and liveness so control flow stops disabling cleanup |
| Execution fact ownership | `Proto` carries many parallel descriptor slices and fast-path booleans, making fact interactions shallow and spread across compiler, verifier, disassembly, and VM | deepen a private execution-artifact finalizer that owns facts, conflicts, invalidation, and test disassembly |
| Dynamic string indexes | direct-frame dynamic string caches are single-entry per instruction, but Scenario rows often rotate through small keysets such as `wood`/`ore`/`food` or actor ids | add small polymorphic inline caches and keyset slot vectors guarded by table shape |
| Table storage | table layout is split across array state, inline string fields, string maps, generic fields, versions, and metatable caches | deepen `Table` behind one private shape interface with coherent tokens for array, string, metatable, and mixed storage |
| Table access slow paths | direct-frame table operations often duplicate fast raw access and fall back to the generic frame for metatable-capable paths | centralize raw/guarded access helpers and add direct-frame slow-path islands that return to the direct loop after one fallback operation |
| Global/import stability | `globalEnv` has no version token, so import and intrinsic guards are hard to hoist or invalidate cheaply | add environment version facts and import guard hoisting for stable base tables and globals |
| Value-kind checks | arithmetic, comparison, branch, and table operations repeatedly check `Value.kind` after nearby facts already imply number, string, boolean, or table | propagate value-kind facts through registers, row slots, constants, branches, and stores |
| Call/result lifetime | call paths still build slices for copied args, open-call results, metamethod args, and `vmFrameResult.values()` | unify inline/borrowed value-list carriers and make ownership explicit at the frame seam |
| Direct-frame fallback granularity | one unsupported operation or guard miss can abandon the direct runner, even when the next bytecode is direct-safe again | add side-exit accounting and resumable slow-path islands before larger superblocks |
| Instrumentation | opcode counts exist, but hot pressure is not yet grouped by fact family, guard miss, cache miss, or side-exit reason | add private counters that classify misses by reusable mechanism, not scenario name |

The deletion test for these additions is strong: if the execution-artifact,
table-shape, value-kind, or direct-frame side-exit modules disappear, their
complexity reappears across compiler lowering, verifier checks, VM switch cases,
and tests. That makes them deep-module candidates rather than speculative seams.

## Scope

- In: private bytecode, verifier, disassembly, compiler lowering, table
  storage, VM direct-frame execution, guarded caches, and benchmark gates.
- In: source-to-result behavior tests through `Compile`, `Run`, and
  `RunWithGlobals`.
- In: private debug/disassembly facts when needed to prove optimized shapes.
- Out: native codegen, JIT, generated interpreter backends, unsafe dispatch,
  custom GC, public `Proto` metadata, Hearth-specific shortcuts, and
  benchmark-named descriptors or helpers.

Rejected artifact names include any opcode, descriptor, helper, or verifier
fact named after Scenario rows such as combat, inventory, event, buff, ability,
AI, cooldown, projectile, quest, behavior tree, threat, economy, formation,
dialogue, procgen, save state, or path relaxation.

## Design

The deep module is the private verified execution artifact:

```text
source
  -> ordinary bytecode
  -> general execution facts
  -> verifier
  -> direct-frame VM with guarded fallbacks
```

The interface stays small:

```go
proto, err := ember.Compile(source)
results, err := ember.Run(proto)
```

Optimization facts must describe reusable language shapes, not whole programs:

- `GET_INDEX` direct-frame eligibility;
- dynamic string-key slot caches;
- table literal shape facts;
- dense-array iterator facts;
- intrinsic identity guards;
- stable table-call guards;
- numeric register facts;
- finite-key subtable facts;
- indexed row lookup facts;
- reduction facts;
- frame lifetime facts;
- control-flow liveness facts;
- global environment version facts;
- table shape IDs;
- direct-frame superblock facts;
- value-list lifetime facts;
- finite predicate facts for string equality, nil checks, and boolean fields;
- loop-local table path reuse facts;
- polymorphic dynamic-key cache facts;
- value-kind register and slot facts;
- direct-frame side-exit facts;
- composed-fact conflict and invalidation facts.

Each fast path must either be verified as safe or guarded at runtime with a
normal Luau fallback.

## Phase 0: Measurement And Guardrails

Purpose: make the target mechanical and keep optimization honest.

Exit gate:

- all Scenario rows are present in same-run ratio output;
- the 2.0x ratio gate fails red today and passes only when every row is below
  target;
- allocation budgets protect every Scenario row;
- current Top10 and Classic guard rows are recorded.

### Slice 0.1: Ratio And Ledger Refresh

- Behavior: no runtime change.
- Work: record a count-3 same-run Scenario baseline and current Ember guard
  rows.
- Bench:
  `go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...`
- Done: ledger names the current worst rows and top profile categories.

### Slice 0.2: General-Only Artifact Guard

- Behavior: no runtime change.
- Work: extend artifact guard tests so Scenario programs must not emit
  benchmark-named opcodes, descriptors, or disassembly facts.
- Done: any Scenario-named optimized artifact fails tests.

## Phase 1: Direct-Frame Completeness

Purpose: keep ordinary non-yielding code on the direct runner.

Exit gate:

- dynamic table gets and sets no longer force direct-frame rejection;
- direct paths preserve metatable fallback, missing-key nil behavior, and
  invalid-key errors;
- worst dynamic-table rows move for a general reason.

### Slice 1.1: Direct `GET_INDEX`

- Status: done, 2026-07-05.
- Behavior: `table[key]` preserves string, numeric, missing, nil, metatable,
  and invalid-key semantics.
- Work: make `opGetIndex` direct-frame eligible with fast paths for no-metatable
  string fields and dense array indexes.
- Bench: `economy_market_tick`, `threat_aggro_table`, `save_state_diff`,
  `path_relaxation`, plus `table_fields`.
- Done: dynamic-index programs remain direct-frame eligible and fallback tests
  pass.

### Slice 1.2: Direct `SET_INDEX`

- Status: done, 2026-07-05.
- Behavior: `table[key] = value` preserves nil deletes, `__newindex`, invalid
  keys, array extension, and map-backed fields.
- Work: add direct-frame string and numeric set paths with generic fallback.
- Bench: `economy_market_tick`, `threat_aggro_table`,
  `dialogue_condition_eval`, plus `array_ops`.
- Done: hot dynamic-key mutation does not leave direct-frame execution.

### Slice 1.3: Direct Nested Index-And-Field Paths

- Status: partially done, 2026-07-05. Named two-step paths and
  field-then-index paths are done; dynamic-first paths remain future pressure.
- Behavior: paths such as `a[b].c`, `a.b[c]`, and `a[b][c]` preserve each
  intermediate lookup and metatable fallback.
- Work: add general two-step path helpers only where bytecode already carries
  the normal operations.
- Bench: `path_relaxation`, `save_state_diff`, `behavior_tree_tick`.
- Done: named two-step field paths and `a.b[c]` field-then-index paths are
  direct-frame eligible with metatable fallback; `a[b].c` and `a[b][c]`
  dynamic-first nested helpers remain future pressure.

## Phase 2: Table Shape And Dynamic-Key Caches

Purpose: stop repeating stable table layout discovery in hot loops.

Exit gate:

- string field slot caches are layout-version guarded;
- value updates do not invalidate slot layout;
- map-backed, deleted, nil, metatable, and shape-changed tables fall back.

### Slice 2.1: Split String Field Layout Version

- Status: done, 2026-07-05.
- Behavior: stale slot guards still reject deleted or moved fields.
- Work: separate table string-field layout version from field-value mutation
  version.
- Bench: row-field reads/writes, `table_fields`, and dynamic-key rows.
- Done: repeated value writes no longer invalidate stable slot positions.

### Slice 2.2: Dynamic String-Key Inline Cache

- Status: done, 2026-07-05.
- Behavior: `table[key]` with string keys preserves nil, metatable, and
  non-string-key behavior.
- Work: attach a small private per-instruction cache keyed by table identity,
  layout version, and key string.
- Bench: `economy_market_tick`, `threat_aggro_table`,
  `dialogue_condition_eval`, `save_state_diff`.
- Done: finite dynamic-key workloads improve and cache misses safely fallback.

### Slice 2.3: Table Literal Shape Artifact

- Behavior: table literals still create ordinary mutable tables.
- Work: record private literal shapes for array capacity, inline string-field
  order, and nested literal shapes.
- Bench: all Scenario setup-heavy rows plus `array_ops`.
- Done: setup cost falls and compiled row-slot facts become more reliable.

### Slice 2.4: Shape-Aware Row Propagation

- Status: partially done, 2026-07-05. Generic-for row bindings and locals
  assigned from indexed row arrays carry row slot facts.
- Behavior: aliases, reassignment, nil deletes, and metatables remain visible.
- Work: propagate table literal shape facts through locals and generic-for row
  bindings when ownership is clear.
- Bench: `inventory_value`, `cooldown_scheduler`, `formation_layout_score`,
  `procgen_room_scoring`.
- Done: more ordinary row loops use general slot-aware reads and writes.

## Phase 3: Iteration And Sequence Operations

Purpose: make dense array loops and sequence helpers cheap.

Exit gate:

- clean-array generic-for loops avoid host-call-shaped iterator overhead;
- holes, mixed tables, metatables, and mutation fallback correctly;
- sequence operations stay allocation-neutral or improve.

### Slice 3.1: Dense Array Iterator Opcode

- Status: deepened, 2026-07-06. Two-result array iteration now fuses the
  nil-termination branch into the iterator opcode.
- Behavior: generic-for over a dense array preserves index/value order and nil
  termination.
- Work: lower verified clean-array iteration to a private direct iterator
  operation or equivalent artifact fact.
- Bench: `generic_iteration`, `quest_progress_update`,
  `ai_utility_scoring`, `path_relaxation`.
- Done: array iteration no longer appears as generic call plumbing in profiles;
  hot two-result loops avoid the separate iterator result move, nil compare,
  and branch dispatch.

### Slice 3.2: Fast `rawlen` Intrinsic In Direct Frame

- Status: done, 2026-07-05.
- Behavior: `RunWithGlobals` overrides and `__len`-using `#` behavior remain
  distinct from `rawlen`.
- Work: direct-frame intrinsic guard for default `rawlen`.
- Bench: `buff_stack_tick`, `array_ops`.
- Done: repeated raw length checks do not call through generic host machinery.

### Slice 3.3: Fast `table.remove` And `table.insert`

- Status: done, 2026-07-05.
- Behavior: argument validation, out-of-range positions, nil returns, and host
  overrides stay correct.
- Work: direct-frame intrinsic guards for default dense-array insert/remove.
- Bench: `buff_stack_tick`, `array_ops`.
- Done: dense sequence mutation uses table storage directly under guard.

## Phase 4: Calls And Intrinsics

Purpose: avoid repeated dynamic lookup and generic call setup when identities
are stable.

Exit gate:

- host overrides, handler mutation, yielding calls, and protected calls still
  fallback;
- method and handler rows improve without Scenario-specific helpers.

### Slice 4.1: Hoisted Base Intrinsic Guards

- Behavior: replacing `math`, `table`, `rawlen`, or specific function fields
  through `RunWithGlobals` changes behavior.
- Work: verify default intrinsic identity once per safe scope and pass the
  guard to direct-frame instructions.
- Bench: `combat_tick`, `ability_resolution`, `event_dispatch`,
  `buff_stack_tick`.
- Done: repeated `math.min`, `rawlen`, `table.remove`, and `table.insert`
  lookup disappears from hot profiles.

### Slice 4.2: Stable Table Function Call Cache

- Status: done, 2026-07-05.
- Behavior: missing handlers, non-functions, table mutation, metatable calls,
  host functions, yielding functions, and errors remain correct.
- Work: cache table-field script call targets behind table layout/version and
  callee identity guards.
- Bench: `event_dispatch`, `behavior_tree_tick`, `method_calls`,
  `coroutine_yield`.
- Done: dynamic handler dispatch improves while coroutine and method guards
  stay stable.

### Slice 4.3: Fixed One-Result Script Call Return Path

- Status: partially done, 2026-07-05.
- Behavior: zero, one, many, and open returns preserve Luau value-list
  adjustment.
- Work: keep one-result script calls on an inline result path when verifier
  facts allow it.
- Bench: `method_calls`, `event_dispatch`, `recursive_fibonacci`.
- Done: local one-result script calls are direct-frame eligible; broader method
  and upvalue one-result call direct-frame paths remain future pressure.

## Phase 5: Numeric Loop And Branch Facts

Purpose: make arithmetic-heavy rows pay for numeric checks once where safe.

Exit gate:

- numeric facts are invalidated by writes, calls, table loads, and fallback
  edges that can change value kind;
- arithmetic metamethod behavior remains covered.

### Slice 5.1: Loop-Local Numeric Register Facts

- Behavior: numeric strings and metamethod-capable operands still fallback
  where Luau requires them.
- Work: infer guarded numeric facts inside loops for locals assigned from
  numeric literals, numeric fields, and numeric arithmetic.
- Bench: `formation_layout_score`, `ai_utility_scoring`,
  `procgen_room_scoring`, `cooldown_scheduler`.
- Done: arithmetic op variants are selected by general facts, not source names.

### Slice 5.2: Numeric Compare-And-Branch Forms

- Status: partially done, 2026-07-06.
- Behavior: NaN and non-number behavior remain compatible.
- Work: add general branch forms for common numeric comparisons against
  constants and numeric registers.
- Bench: branch-heavy Scenario rows plus `while_branching`.
- Done: row-slot numeric field comparisons against constants, register
  `left < right` / `left > right` comparisons, and register-to-row-field
  `left < row.field` comparisons use private guarded branch forms. Same-row
  field-to-field `row.left < row.right` comparisons also use a guarded branch
  form. `<=`, `>=`, cross-row field comparisons, and constant greater-than
  forms remain future pressure.

### Slice 5.3: Field Presence Branch Forms

- Status: done, 2026-07-05. `field ~= nil`, `field == nil`, and `not field`
  lower to guarded string-field predicate branches.
- Behavior: false field values remain distinct from nil; metatable fallback
  remains visible.
- Work: add branch forms for common field presence checks without materializing
  temporary comparison registers.
- Bench: `dialogue_condition_eval`, `quest_progress_update`, field-heavy rows.
- Done: field presence checks and negated boolean field predicates use private
  guarded branch forms.

## Phase 6: Execution Artifact Ownership

Purpose: make optimized facts one coherent private module instead of scattered
compiler and VM conventions.

Exit gate:

- optimized facts have one finalization path;
- verifier and disassembly cover every fact family;
- tests can inspect artifact shape without widening the public runtime
  interface.

### Slice 6.1: Execution Fact Inventory

- Behavior: no runtime change.
- Work: list every private optimization fact currently carried by `Proto`,
  bytecode descriptors, table metadata, or VM side tables.
- Done: each fact has an owner, verifier check, disassembly line, and fallback
  rule.

### Slice 6.2: Fact Finalizer Module

- Behavior: no runtime behavior change.
- Work: gather shape inference, descriptor construction, and direct-frame
  eligibility decisions behind one private finalization seam.
- Bench: compile benchmark guard plus Scenario behavior tests.
- Done: future facts are added in one place rather than by teaching multiple
  callers the same invariants.

### Slice 6.3: Fact Disassembly Contract

- Behavior: no runtime behavior change.
- Work: make private disassembly report enough fact detail to test optimized
  shape without exposing internals publicly.
- Done: tests can assert general shapes such as dynamic-key cache,
  finite-key subtable, dense iterator, and reduction facts.

### Slice 6.4: Fact Rejection Matrix

- Behavior: invalid optimized artifacts fail before execution or fallback
  safely at the guard point.
- Work: add verifier coverage for stale slots, malformed keysets, invalid
  register ranges, wrong value kinds, and unsupported control-flow edges.
- Done: every optimized fact family has at least one rejection test.

## Phase 7: Dynamic-First Nested Paths

Purpose: cover nested table paths where the first hop is dynamic, not just
`a.b[c]` or `a.b.c`.

Exit gate:

- `a[b].c`, `a[b][c]`, and paired table-array paths preserve intermediate
  metatable behavior;
- dynamic-first helpers improve path-heavy rows without source-name
  recognition;
- table guard rows do not regress.

### Slice 7.1: Dynamic-First Field Read

- Behavior: `a[b].c` preserves missing intermediate values, metatables, and
  invalid dynamic keys.
- Work: add a general path helper or descriptor for dynamic first hop followed
  by constant string field.
- Bench: `path_relaxation`, `behavior_tree_tick`, `table_fields`.
- Done: direct-frame execution handles dynamic-first field reads with fallback.

### Slice 7.2: Dynamic-First Dynamic Read

- Behavior: `a[b][c]` preserves both lookup steps and fallback at either step.
- Work: add a guarded helper for two dynamic keys, optimized only when both
  tables and key kinds remain stable.
- Bench: `save_state_diff`, `economy_market_tick`,
  `dialogue_condition_eval`.
- Done: paired dynamic map reads avoid repeated generic table access.

### Slice 7.3: Dynamic-First Store

- Behavior: `a[b].c = v` and `a[b][c] = v` preserve nil deletes and
  `__newindex`.
- Work: add direct-frame store support for dynamic-first paths with normal
  fallback on metatable or shape changes.
- Bench: `threat_aggro_table`, `economy_market_tick`,
  `dialogue_condition_eval`.
- Done: hot nested map mutations stay on the direct runner when guarded.

## Phase 8: Finite-Key Subtables

Purpose: optimize dynamic string-key maps whose key set is small and stable,
such as inventory maps, threat maps, blackboards, flags, prices, and stock.

Exit gate:

- keyset facts are derived from ordinary table literals and guarded writes;
- unknown keys, nil deletes, metatables, and shape changes fallback;
- finite-key facts are reusable across many rows.

### Slice 8.1: Keyset Shape Facts

- Behavior: table literals remain ordinary mutable tables.
- Work: record private facts for finite string-key tables, including known keys,
  inline slots, map-backed storage, and layout version.
- Bench: compile/run behavior only.
- Done: finite-key facts appear for table literals without naming Scenario
  rows.

### Slice 8.2: Finite-Key Dynamic Reads

- Behavior: `map[key]` returns nil for missing keys and respects metatables.
- Work: use keyset facts plus dynamic string-key caches to read finite maps by
  slot or map entry under guard.
- Bench: `economy_market_tick`, `threat_aggro_table`,
  `dialogue_condition_eval`, `save_state_diff`.
- Done: dynamic map reads drop from top profile frames.

### Slice 8.3: Finite-Key Dynamic Writes

- Behavior: updates to known keys keep the shape; writes to unknown keys or nil
  deletes update shape and invalidate caches.
- Work: add shape-aware direct writes for finite string-key maps.
- Bench: `economy_market_tick`, `threat_aggro_table`,
  `dialogue_condition_eval`.
- Done: stable finite-map writes improve without hiding table mutation.

### Slice 8.4: Finite-Key Join Across Aliases

- Behavior: aliases, branch assignments, and loop-carried map references
  preserve visible mutation.
- Work: propagate finite-key facts through locals and row fields when the owner
  table is still explicit.
- Bench: `save_state_diff`, `behavior_tree_tick`, `quest_progress_update`.
- Done: dynamic-key facts survive the common aliasing patterns in Scenario
  programs.

## Phase 9: Indexed Row Traversal

Purpose: make `rows[index]` and graph-like row traversal cheap without
recognizing any specific graph or behavior-tree source.

Exit gate:

- indexed row facts prove bounds, row shape, and mutation visibility;
- out-of-range, nil, holes, and metatables fallback correctly;
- indexed traversal rows move for the same general mechanism.

### Slice 9.1: Indexed Row Lookup Facts

- Behavior: `rows[index]` still returns nil or errors exactly as current Luau
  behavior requires.
- Work: record facts for dense row arrays and numeric index registers with
  guarded bounds.
- Bench: `behavior_tree_tick`, `path_relaxation`,
  `procgen_room_scoring`.
- Done: indexed row lookups can use dense-array direct access under guard.

### Slice 9.2: Bounded While Traversal

- Behavior: while-loop conditions, depth caps, and index updates remain
  observable.
- Work: optimize bounded indexed traversal loops without assuming a particular
  node schema.
- Bench: `behavior_tree_tick`, `while_branching`.
- Done: indexed while traversal avoids generic dispatch and repeated bounds
  rediscovery.

### Slice 9.3: Graph Edge Traversal

- Behavior: nested edge arrays, blocked flags, and conditional stores preserve
  current results.
- Work: generalize indexed row facts to nested edge rows that point back into a
  dense row array.
- Bench: `path_relaxation`, `projectile_sweep`, `generic_iteration`.
- Done: graph-like relaxation uses row/index facts instead of generic table
  access at every edge.

### Slice 9.4: Early-Exit Nested Loops

- Behavior: `break` exits the correct inner loop and leaves all mutations
  visible.
- Work: record verified exit targets for nested direct-frame loop regions.
- Bench: `projectile_sweep`, `procgen_room_scoring`.
- Done: early-exit loops can stay optimized through the break edge.

## Phase 10: Reduction Patterns

Purpose: optimize common nested-loop reductions as general control/data-flow
facts, not as per-row fused bodies.

Exit gate:

- reductions are described by accumulator, candidate, predicate, and mutation
  facts;
- reduction facts preserve NaN, nil, branch, and mutation behavior;
- reductions improve multiple Scenario rows with one mechanism family.

### Slice 10.1: Max/Best Reduction Facts

- Behavior: best-score updates preserve strict comparison behavior.
- Work: identify loops that maintain a best numeric candidate and optional
  best index.
- Bench: `formation_layout_score`, `ai_utility_scoring`,
  `procgen_room_scoring`.
- Done: best-score loops use numeric and row facts under one reduction shape.

### Slice 10.2: All-Complete Boolean Reduction

- Behavior: boolean reductions still observe every mutation and predicate.
- Work: support loops that start true and clear on failed child predicates.
- Bench: `quest_progress_update`, `dialogue_condition_eval`.
- Done: all-complete style loops become a reusable reduction shape.

### Slice 10.3: Absolute Delta Reduction

- Behavior: sign normalization and weighted accumulation preserve numeric
  semantics.
- Work: recognize general `delta < 0 then delta = -delta` reductions.
- Bench: `formation_layout_score`, `save_state_diff`,
  `projectile_sweep`.
- Done: absolute-delta loops avoid generic branch and arithmetic overhead.

### Slice 10.4: Paired Row Diff Facts

- Behavior: two arrays traversed by the same index preserve independent table
  mutation and dynamic nested fields.
- Work: record paired row facts for `left = before[i]`, `right = after[i]`,
  and similar patterns.
- Bench: `save_state_diff`.
- Done: paired-row diff workloads use dense row, dynamic-key, and reduction
  facts together.

## Phase 11: Call And Frame Lifetime

Purpose: reduce call/frame/allocation overhead after table and loop mechanisms
have moved the hot rows closer to the VM core.

Exit gate:

- ordinary fixed calls avoid unnecessary slices and result allocations;
- coroutine, protected-call, vararg, host-call, and yielding behavior still
  fallback;
- allocation budgets do not regress.

### Slice 11.1: Fixed Argument Windows

- Behavior: fixed script and host calls receive the same arguments as before.
- Work: pass borrowed register windows through direct-frame fixed calls where
  lifetimes are obvious.
- Bench: `method_calls`, `event_dispatch`, `recursive_fibonacci`.
- Done: call-heavy rows allocate less or run faster without changing returns.

### Slice 11.2: One-Result Method Calls

- Behavior: method calls still pass `self`, handle missing methods, and respect
  metatables.
- Work: extend one-result direct-frame paths to common method-call bytecode
  shapes under guard.
- Bench: `method_calls`, `behavior_tree_tick`.
- Done: method-call overhead drops without coroutine regression.

### Slice 11.3: Host Intrinsic Result Application

- Behavior: fixed-result intrinsics preserve error messages and override
  behavior.
- Work: write one-result and two-result intrinsic outputs directly to
  registers when default identity is guarded.
- Bench: `buff_stack_tick`, `array_ops`, `varargs_select`.
- Done: intrinsic calls avoid generic result-list adjustment when safe.

### Slice 11.4: Frame Reuse Audit

- Behavior: reused frames never leak stale register, vararg, upvalue, or open
  call state.
- Work: tighten frame reset facts and allocation budgets for direct-register
  no-capture calls.
- Bench: `recursive_fibonacci`, `event_dispatch`, `method_calls`.
- Done: frame pooling is deliberate and covered by behavior tests.

## Phase 12: Numeric Fact Deepening

Purpose: finish numeric specialization after table/loop facts make operand
shapes more stable.

Exit gate:

- numeric facts are explicit, invalidated, and inspected in tests;
- numeric branch and arithmetic variants cover the remaining hot cases;
- metamethod-capable paths still fallback.

### Slice 12.1: Field-To-Register Numeric Facts

- Behavior: numeric field reads that later become non-numeric fallback safely.
- Work: propagate guarded numeric facts from slot reads into loop-local
  registers.
- Bench: `formation_layout_score`, `ai_utility_scoring`,
  `cooldown_scheduler`.
- Done: later arithmetic in the same loop can trust guarded numeric facts.

### Slice 12.2: Numeric Branch Coverage

- Status: partially done, 2026-07-06. Register-to-row-field less-than
  branches now avoid materializing the right-hand row field; same-row
  field-to-field less-than branches avoid materializing both branch operands.
- Behavior: `<=`, `>=`, and constant greater-than forms preserve NaN and
  non-number behavior.
- Work: add the branch forms left open by Phase 5.
- Bench: branch-heavy Scenario rows plus `while_branching`.
- Done: numeric comparison helpers leave the top profile frames.

### Slice 12.3: Numeric Store-Back Facts

- Behavior: storing numeric results to tables updates versioning and invalidates
  dependent caches correctly.
- Work: connect numeric register facts to row/finite-key store paths.
- Bench: `cooldown_scheduler`, `economy_market_tick`,
  `path_relaxation`.
- Done: numeric mutation loops avoid rechecking freshly stored numeric values.

## Phase 13: Control-Flow-Aware Bytecode Optimization

Purpose: stop treating loops and branches as a reason to skip bytecode cleanup.

Exit gate:

- optimizer works over basic blocks and liveness, not whole straight-line
  programs only;
- optimized bytecode preserves jump targets, line tables, and source behavior;
- direct-frame dispatch sees fewer moves, loads, and temporary registers.

### Slice 13.1: Basic-Block Peephole Pass

- Behavior: branch targets and line metadata remain stable.
- Work: run local peepholes inside each basic block even when the prototype has
  control flow.
- Bench: `while_branching`, `formation_layout_score`,
  `procgen_room_scoring`.
- Done: self-moves, round trips, and dead local moves disappear inside loop
  bodies without changing jumps.

### Slice 13.2: CFG Liveness Move Elimination

- Behavior: variables still have Luau-visible nil and value-list behavior.
- Work: use existing bytecode IR liveness to remove moves whose destinations
  are dead across control-flow joins.
- Bench: all Scenario rows plus compile benchmark guard.
- Done: register move pressure drops in direct-frame opcode counts.

### Slice 13.3: Loop-Invariant Constant Loads

- Behavior: constants stay ordinary immutable `Value`s.
- Work: hoist repeated constant loads out of loop bodies when register liveness
  proves the target can be reused.
- Bench: `combat_tick`, `inventory_value`, `ability_resolution`,
  `while_branching`.
- Done: repeated `LOAD_CONST` pressure falls without widening public bytecode.

### Slice 13.4: Temporary Register Compaction

- Behavior: register values remain visible to closures and value-list
  adjustment exactly as before.
- Work: shrink temporary register spans after liveness, then recompute entry
  nil plans and direct-register facts.
- Bench: allocation budgets and frame/register pressure rows.
- Done: register count falls for hot prototypes or the rejected cases are
  recorded.

## Phase 14: Global And Import Stability

Purpose: avoid repeated global and base-library table lookup when the run-time
environment proves it is stable.

Exit gate:

- `RunWithGlobals` overrides remain visible;
- global writes invalidate dependent facts;
- base-library identity checks happen once per safe scope.

### Slice 14.1: Global Environment Version Facts

- Behavior: assigning globals and passing host globals remain observable.
- Work: version the runtime global environment and expose private guards to
  optimized bytecode.
- Bench: `event_dispatch`, `ability_resolution`, global access guard rows.
- Done: optimized global loads can prove their assumptions or fallback.

### Slice 14.2: Stable Base Table Imports

- Behavior: replacing `math`, `table`, `coroutine`, or their fields through
  globals changes behavior.
- Work: lower stable base-table field reads to guarded import facts, not
  repeated table lookups.
- Bench: `combat_tick`, `buff_stack_tick`, `ability_resolution`,
  `array_ops`.
- Done: intrinsic lookup pressure falls while override tests pass.

### Slice 14.3: Global Load Direct-Frame Cache

- Behavior: undefined globals, assigned globals, and host globals produce the
  same results and errors.
- Work: add a direct-frame global load cache guarded by environment version and
  name.
- Bench: `BenchmarkRunGlobalAccess`, `method_calls`, Scenario call rows.
- Done: ordinary global reads stop paying map lookup each iteration when safe.

### Slice 14.4: Import Guard Hoisting

- Behavior: any write that can change an imported global invalidates the guard.
- Work: hoist import guards to loop preheaders or function entry when liveness
  and global-write analysis prove safety.
- Bench: intrinsic-heavy Scenario rows and Top10 guard rows.
- Done: base intrinsic checks leave inner-loop profile frames.

## Phase 15: Table Storage Deepening

Purpose: make `Table` a deeper module: simple Luau table behavior outside,
shape-aware storage and invalidation inside.

Exit gate:

- public `Table` behavior and metatable semantics stay unchanged;
- shape IDs and storage modes are private;
- inline, map-backed, array, and mixed tables have explicit invalidation rules.

### Slice 15.1: Table Shape ID

- Behavior: all table reads, writes, deletes, and metatable operations behave
  as before.
- Work: replace ad hoc string layout version checks with a private shape ID
  that changes on layout and metatable-affecting mutations.
- Bench: table guard rows and dynamic-key Scenario rows.
- Done: table caches guard on one coherent shape token.

### Slice 15.2: Mixed Array/String Fast Storage

- Behavior: mixed tables still iterate and raw-access correctly.
- Work: keep dense array operations fast even when the same table also has
  stable inline string fields.
- Bench: `buff_stack_tick`, `quest_progress_update`,
  `dialogue_condition_eval`, `array_ops`.
- Done: adding record fields does not disqualify array storage unnecessarily.

### Slice 15.3: Shape-Aware Raw Access Helpers

- Status: partially done, 2026-07-05. Direct row string-field reads now keep
  slot fast path, raw fallback, and metatable fallback in one private helper.
- Behavior: `rawget`, `rawset`, `rawlen`, and ordinary access retain distinct
  semantics.
- Work: centralize raw string, dynamic string, numeric array, and map access
  behind private shape-aware helpers.
- Bench: dynamic table rows and Top10 table rows.
- Done: VM fast paths call one deep table-storage interface instead of
  duplicating storage checks.

### Slice 15.4: Metatable Cache Tokens

- Behavior: `__index`, `__newindex`, `__len`, `__call`, and equality/ordering
  metamethod changes remain visible.
- Work: give metatable-dependent caches a token that invalidates on relevant
  metatable writes.
- Bench: `metatable_index`, table guard rows, and cache-heavy Scenario rows.
- Done: cache paths are allowed near metatable-capable code without becoming
  unsound.

## Phase 16: Direct-Frame Superblocks

Purpose: reduce interpreter switch overhead for verified straight-line regions
without generating code or recognizing benchmark bodies.

Exit gate:

- superblocks are formed from ordinary bytecode regions with explicit side
  exits;
- any unsupported value kind, metatable, host override, yield, or budget/debug
  mode falls back to normal execution;
- direct-frame opcode counts show fewer switch trips in hot loops.

### Slice 16.1: Superblock Candidate Facts

- Behavior: no runtime behavior change.
- Work: identify straight-line direct-frame regions between branches, calls,
  and side-effectful fallback points.
- Bench: no expected win; disassembly/facts only.
- Done: tests can see candidate regions without any benchmark names.

### Slice 16.2: Numeric/Table Superblock Executor

- Behavior: every guard failure resumes at the correct bytecode PC.
- Work: execute small verified blocks of numeric ops, field reads, and stores
  in one helper.
- Bench: `formation_layout_score`, `ai_utility_scoring`,
  `economy_market_tick`.
- Done: switch dispatch falls for rows with repeated straight-line loop bodies.

### Slice 16.3: Branch-Ending Superblocks

- Behavior: branch decisions and NaN behavior remain compatible.
- Work: allow superblocks to end in a guarded numeric/string/table branch and
  jump to the verified target.
- Bench: `while_branching`, `combat_tick`, `cooldown_scheduler`.
- Done: hot branch bodies dispatch as one region plus one branch decision.

### Slice 16.4: Superblock Side-Exit Accounting

- Behavior: no runtime behavior change when accounting is disabled.
- Work: add test-only counters for superblock entries, exits, and fallback
  reasons.
- Done: rejected or low-value superblocks can be removed with evidence.

## Phase 17: Value-List And Temporary Lifetime

Purpose: reduce allocation and copying around Luau value lists, calls, returns,
varargs, and metamethods.

Exit gate:

- multiple returns, varargs, final-call expansion, protected calls, and yields
  retain exact behavior;
- common one/two-result paths avoid slice allocation;
- allocation budgets stay flat or improve.

### Slice 17.1: Inline Multi-Result Carrier

- Behavior: open returns and multiple assignment keep Luau adjustment rules.
- Work: extend the existing inline result shape beyond one value for common
  two-result paths.
- Bench: `coroutine_yield`, `varargs_select`, call-heavy Scenario rows.
- Done: two-result iterator/call paths allocate less or copy less.

### Slice 17.2: Borrowed Argument Windows

- Behavior: callees cannot observe caller register mutation incorrectly.
- Work: borrow register windows for fixed script/native calls when lifetime
  analysis proves safety.
- Bench: `event_dispatch`, `method_calls`, `recursive_fibonacci`.
- Done: call argument slices disappear from hot paths where safe.

### Slice 17.3: Metamethod Argument Scratch

- Behavior: metamethod calls still receive correct arguments and errors.
- Work: use fixed scratch storage for one/two-argument metamethod calls instead
  of allocating new slices.
- Bench: metatable guard rows and arithmetic fallback tests.
- Done: making fallback paths correct does not make them allocation-heavy.

### Slice 17.4: Vararg Window Facts

- Behavior: varargs preserve nil padding, select behavior, and open expansion.
- Work: track when varargs can be borrowed as a stable frame window.
- Bench: `varargs_select`, recursive/call guard rows.
- Done: common vararg reads avoid copying.

## Phase 18: Allocation And GC Pressure

Purpose: attack remaining gaps where runtime work is cheap but allocation or
GC churn keeps ratios high.

Exit gate:

- no object pooling leaks mutable table state across runs;
- frame, register, scratch, and temporary lifetimes are explicit;
- allocation budget tests protect each accepted reduction.

### Slice 18.1: Run Allocation Ledger

- Behavior: no runtime behavior change.
- Work: record B/op and allocs/op for every Scenario, Top10, and Classic row
  after the general fast paths already landed.
- Done: the next allocation target is selected from measured rows.

### Slice 18.2: Frame And Register Storage Reuse

- Behavior: stale register contents, open calls, varargs, and upvalues never
  leak between calls.
- Work: deepen frame reuse around direct-register no-capture prototypes.
- Bench: `recursive_fibonacci`, `method_calls`, `event_dispatch`.
- Done: frame-heavy rows improve or the rejected reuse path is recorded.

### Slice 18.3: Temporary Slice Elimination

- Behavior: public results remain independent slices where callers can mutate
  them safely.
- Work: remove internal temporary slices for adjusted results, iterator
  returns, and host intrinsic outputs where public ownership is not involved.
- Bench: `generic_iteration`, `varargs_select`, call-heavy rows.
- Done: internal slice allocation falls without changing public result
  ownership.

### Slice 18.4: Table Setup Allocation Audit

- Behavior: each script run still receives fresh mutable tables.
- Work: reduce table literal setup allocations through capacity plans and
  storage layout, not table object reuse.
- Bench: setup-heavy Scenario rows and `array_ops`.
- Done: table literal setup gets cheaper while preserving fresh identity.

## Phase 19: Predicate And Tag Dispatch Facts

Purpose: make common Luau predicates cheap after table shape facts prove where
the values come from.

Exit gate:

- string equality, nil checks, and boolean field checks are optimized as
  general predicate facts;
- tag dispatch chains improve without recognizing specific tag values as
  benchmark artifacts;
- nil, false, missing fields, metatables, and changed table shapes still
  fallback correctly.

### Slice 19.1: String Equality Branch Facts

- Status: partially done, 2026-07-06. Constant string field branches and
  cross-row slot-known field equality/inequality branches are implemented.
- Behavior: string equality and inequality preserve Luau semantics for strings,
  numbers, booleans, nil, tables, and metamethod-capable values.
- Work: add guarded branch forms for value-known-string or slot-known-string
  comparisons against constants and registers.
- Bench: `inventory_value`, `ability_resolution`, `formation_layout_score`,
  `save_state_diff`.
- Done: string comparison branches leave generic equality frames in tag-heavy
  rows. Remaining pressure includes finite tag chains and dynamic-key string
  comparisons.

### Slice 19.2: Finite Tag Chain Dispatch

- Status: partially done, 2026-07-06. Guarded raw-table `if`/`elseif`
  string-tag chains can load a repeated tag field once and branch against
  constants, including arms with additional `and` predicates, with a metatable
  fallback to the original source-order chain.
- Behavior: `if`/`elseif` chains still evaluate in source order and preserve
  side effects in conditions.
- Work: use finite string-tag facts to lower common tag chains to guarded
  predicate dispatch, not benchmark-specific switches.
- Bench: `buff_stack_tick`, `ai_utility_scoring`,
  `procgen_room_scoring`, `threat_aggro_table`.
- Done: finite tag chains improve across unrelated row schemas with one fact
  family. Remaining pressure includes broader direct-frame/global lookup costs
  around tag-heavy loops and other predicate families such as nil/boolean row
  facts.

### Slice 19.3: Nil-Shape Predicate Facts

- Status: partially done, 2026-07-06. Field nil predicates can now be stitched
  into optimized `and` chains with local-register numeric predicates, so shapes
  such as `check.key ~= nil and value > top` avoid materializing the variant
  field before branching.
- Behavior: explicit `nil` checks distinguish missing, nil-valued, false, and
  metatable-backed fields.
- Work: record when a table row belongs to a finite shape where one of several
  fields distinguishes the row variant, such as `check.key ~= nil` versus
  `check.stat`.
- Bench: `dialogue_condition_eval`, `quest_progress_update`, table guard rows.
- Done: nil-shape predicates avoid generic comparison when guarded by row
  shape. Remaining pressure includes deeper finite-shape slot facts for rows
  where some variants omit the discriminator field.

### Slice 19.4: Boolean Field Branch Facts

- Status: partially done, 2026-07-06. Row/string-field truthiness can now be
  stitched into optimized `and` chains with local-register numeric predicates,
  so shapes such as `actor.alive and value > top` avoid materializing the
  boolean field.
- Behavior: `false`, `nil`, truthy non-booleans, missing fields, and metatable
  results preserve Luau truthiness.
- Work: propagate guarded boolean facts from row fields into direct branch
  forms.
- Bench: `combat_tick`, `projectile_sweep`, `path_relaxation`,
  `behavior_tree_tick`.
- Done: boolean guard branches move multiple rows without source-name checks.
  Remaining pressure includes broader boolean/nil shape predicates and table
  path/cache facts around boolean-heavy loops.

## Phase 20: Opcode Metadata And Artifact Trust

Purpose: make private bytecode and execution metadata impossible to drift
before more facts depend on it.

The current optimization pressure is not only runtime speed. Newer branch and
predicate opcodes need one authoritative metadata table so CFG, liveness,
peepholes, disassembly, verifier checks, direct-frame coverage, and future fact
invalidation all agree about control flow and effects.

Exit gate:

- every opcode has one private metadata entry;
- branch operands, control-flow kind, jump target slots, effects, and
  direct-frame support are derived from that metadata;
- malformed metadata fails verifier or coverage tests before execution;
- `Proto` stops accumulating unrelated sibling fact slices as the long-term
  extension point.

### Slice 20.1: Single Opcode Metadata Table

- Behavior: no runtime behavior change.
- Work: create one private metadata table that describes control flow, jump
  target slot, operand shape, direct-frame support, call/yield risk, table
  reads/writes, global reads/writes, and allocation risk.
- Done: `classifyInstructionOperands`, `opcodeControlFlow`,
  `opcodeJumpTarget`, direct-frame coverage checks, and verifier helpers can
  read the same source of truth.
- Progress 2026-07-06: opcode names, direct-frame eligibility, direct-frame
  unsupported reasons, control-flow kind, jump-target slot, operand shapes,
  call/yield risk, table/global effects, and allocation risk now live in one
  private metadata table. `classifyInstructionOperands`,
  `opcodeControlFlow`, `opcodeJumpTarget`, direct-frame rejection, and opcode
  effect helpers read that table for known opcodes.

### Slice 20.2: Metadata Coverage Tests

- Behavior: no runtime behavior change.
- Work: add table-driven coverage over all opcodes, including specialized
  branch opcodes, predicate branches, `opArrayNextJump2`, and metatable tests.
- Done: any opcode with a jump-target operand but no branch metadata fails, and
  every branch has a valid target slot.
- Progress 2026-07-06: `TestOpcodeMetadataCoversEveryOpcode` now covers every
  opcode entry, direct-frame eligibility, explicit control-flow expectations,
  jump-target slot coverage, non-empty operand shapes, and branch/jump/return
  consistency. `TestOpcodeMetadataValidationRejectsMalformedEntries` mutates
  copied metadata entries to prove malformed names, operands, branch targets,
  jump slots, and yield/call facts fail validation before execution.

### Slice 20.3: Direct-Frame Metadata Contract

- Behavior: direct-frame execution remains unchanged.
- Work: verify that every direct-frame-supported opcode has direct runner
  handling and every unsupported opcode is rejected for an explicit reason.
- Done: direct-frame eligibility no longer drifts from the VM switch.
- Progress 2026-07-06: unsupported opcodes now carry metadata reasons, and
  `protoDirectFrameRejection`/`verifyProto` surface those reasons with opcode
  names and program counters.

### Slice 20.4: Execution Artifact Finalizer

- Behavior: no public interface change.
- Work: move fact construction, descriptor construction, conflict checks,
  verifier checks, and private disassembly toward one `executionArtifact`
  finalization seam owned by `Proto`.
- Done: adding a new fact family touches the finalizer instead of scattering
  invariants across compiler, verifier, disassembly, and VM code.
- Progress 2026-07-06: added a private `executionArtifact` builder/apply seam
  for existing derived proto facts: constant key/number facts, numeric-loop
  and intrinsic descriptors, captured-local/direct-frame facts, entry-nil
  registers, and current fast-call descriptors. `newProtoWithDescriptors` and
  `bytecodeBuilder.proto` both route through `finalizeProtoExecutionArtifact`.

## Phase 21: CFG-Aware Bytecode Optimization

Purpose: stop treating loops and branches as a reason to skip bytecode cleanup.

Exit gate:

- peepholes can run inside basic blocks even when a prototype has jumps;
- instruction deletion can remap jump targets through opcode metadata;
- liveness-backed cleanup preserves Luau value-list, table, call, metamethod,
  debug, and line behavior;
- direct-frame dispatch sees fewer moves, loads, and temporary registers.

### Slice 21.1: Basic-Block Peepholes

- Behavior: branch targets and line metadata remain stable.
- Work: remove self-moves and block-local move round trips where the temp is
  dead inside the block. Fold immediate constant use only where existing
  constant-form opcodes already preserve semantics.
- Bench: `while_branching`, `combat_tick`, `formation_layout_score`,
  `procgen_room_scoring`.
- Done: local loop bodies lose avoidable dispatch without rewriting targets.
- Progress 2026-07-06: `optimizeBytecodeIR` no longer bails out just because
  bytecode has jumps. It removes self-moves inside basic blocks and remaps
  `B`/`D` jump targets through opcode metadata after deletion. The first tracer
  test covers then/else self-move deletion with both fallthrough and jump-end
  target repair.
- Progress 2026-07-06: block-local move round trips now consult CFG liveness
  before deletion, so temps live out through a join are preserved while temps
  overwritten inside the same block can be removed.

### Slice 21.2: CFG Target Remapping

- Behavior: forward jumps, backward jumps, nested loops, `break`, `continue`,
  and specialized branches land at the same semantic instruction.
- Work: build old-PC to new-PC maps and rewrite all jump targets through the
  opcode metadata table.
- Done: instruction deletion is allowed across blocks and every branch target
  rewrite has tests.
- Progress 2026-07-06: target-remap tests now cover `B` jumps, specialized
  `D`-slot predicate branches, and backward loop jumps after instruction
  deletion.

### Slice 21.3: Liveness Dead-Code Cleanup

- Behavior: calls, table writes, global writes, iterator prep, open-call state,
  and metamethod-capable operations are never removed as if they were pure.
- Work: remove dead `LOAD_CONST`, `MOVE`, and pure numeric temporary results
  when CFG liveness proves the result is unused.
- Done: compile-on/compile-off equivalence tests pass through public `Run`.
- Progress 2026-07-06: added a conservative liveness DCE tracer for simple
  blocks containing only `LOAD_CONST`, `MOVE`, simple branches, jumps, and
  returns. Dead constant/move cascades are removed, while effectful-looking
  blocks are skipped until value-list, intrinsic, table, call, and metamethod
  read/write models are covered.
- Progress 2026-07-06: the read model now covers intrinsic argument windows for
  `TABLE_INSERT`, `TABLE_REMOVE`, `COROUTINE_RESUME`, and `MATH_MIN`.
  Conservative DCE can run through blocks containing those proven intrinsics,
  preserving argument loads and effectful intrinsic instructions while removing
  unrelated dead loads.
- Progress 2026-07-06: fixed-argument `CALL` and `CALL_ONE` read windows are
  covered by tests, and conservative DCE can run through those blocks while
  preserving call arguments and effectful call instructions. Open-call blocks
  remain skipped until value-list conventions are modeled.
- Progress 2026-07-06: table field/index read and write operand conventions
  are covered for `GET_FIELD`, `SET_FIELD`, `GET_INDEX`, `SET_INDEX`,
  `GET_STRING_FIELD`, and `SET_STRING_FIELD`. Conservative DCE can run through
  blocks containing those proven table ops, preserving table effects while
  removing unrelated dead loads.
- Progress 2026-07-06: row string field operand conventions are covered for
  `GET_ROW_STRING_FIELD` and `SET_ROW_STRING_FIELD`. Conservative DCE can run
  through blocks containing those proven row field ops, and public compile/run
  coverage verifies row-field programs still preserve mutation/read behavior.
- Progress 2026-07-06: nested string-field pair/index variants are covered for
  `GET_STRING_FIELD2`, `SET_STRING_FIELD2`, `GET_STRING_FIELD_INDEX`, and
  `SET_STRING_FIELD_INDEX`. Conservative DCE can run through blocks containing
  those proven nested field ops, with public compile/run coverage for nested
  table field mutation and read behavior.
- Progress 2026-07-06: iterator operand conventions are covered for
  `PREPARE_ITER`, `ARRAY_NEXT`, and `ARRAY_NEXT_JUMP2`, including iterator
  result-register ranges. Conservative DCE can run through blocks containing
  those iterator ops, preserving iterator effects and jump-target remapping
  while removing unrelated dead loads, with public compile/run coverage for
  array iteration behavior.
- Progress 2026-07-06: simple comparison and numeric branch operand
  conventions are covered for `NUMERIC_FOR_CHECK`, `JUMP_IF_NOT_EQUAL_K`,
  `JUMP_IF_NOT_LESS_K`, `JUMP_IF_NOT_LESS`, `JUMP_IF_NOT_GREATER`, and
  `JUMP_IF_MOD_K_NOT_EQUAL_K`. Conservative DCE can run through blocks
  containing those branch predicates, preserving register reads and remapping
  branch targets while removing unrelated dead loads.
- Progress 2026-07-06: table-backed predicate branch operand conventions are
  covered for metatable tests, string-field comparisons, row string-field
  comparisons, and string-field truthy/nil predicates. Conservative DCE can run
  through blocks containing those predicate branches while preserving table
  reads, predicate effects, and jump-target remapping, with public compile/run
  coverage for field-predicate branch behavior.
- Progress 2026-07-06: fixed-count `VARARG` result windows are covered by the
  liveness write model. Conservative DCE can run through fixed-count vararg
  blocks, preserving vararg effects while removing unrelated dead loads, with
  public compile/run coverage for variadic function behavior. Open vararg
  blocks remain skipped until open value-list state is modeled.
- Progress 2026-07-06: open `RETURN` fixed-prefix registers are covered by
  liveness candidate discovery, so conservative DCE preserves prefix values
  before an expanded final call while still leaving open producer state for a
  future value-list model. Public compile/run coverage verifies prefix plus
  final-call return behavior.
- Progress 2026-07-06: open `VARARG` producers can now participate in
  conservative DCE while remaining non-removable. Blocks containing `return ...`
  can lose unrelated dead loads without losing the open value-list state, with
  public compile/run coverage for open variadic return behavior. Open `CALL`
  producers remain skipped until call result state is modeled.
- Progress 2026-07-06: fixed-argument open-result `CALL` producers can now
  participate in conservative DCE while remaining non-removable. Blocks
  containing final-call expansion can lose unrelated dead loads without losing
  open call result state, with public compile/run coverage for final-call
  return behavior.
- Progress 2026-07-06: open-argument `CALL` consumers now model their callee
  and fixed prefix argument registers, so conservative DCE can run through
  blocks containing calls such as `take(prefix, rest())`. The open value-list
  producer remains non-removable, and public compile/run coverage verifies
  expanded call arguments still reach the callee.
- Progress 2026-07-06: pure numeric temporary cleanup now has block-local
  number facts sourced from numeric constants, moves, and proven numeric
  arithmetic. Dead `ADD`/`SUB`/`MUL`/`DIV`/`MOD`/`IDIV`/`POW`, `*_K`, and
  unary-negate results can be removed only when their operands are proven
  numeric, while public compile/run coverage verifies dead arithmetic that can
  invoke a metamethod still runs.
- Progress 2026-07-06: descriptor-backed `ADD_NUMERIC_MOD_K` now participates
  in the same numeric DCE proof. The optimizer carries private numeric
  add-mod descriptors alongside constants, so the opcode is removable only
  when its old target, source register, and all descriptor constants are
  proven numeric.

### Slice 21.4: Loop Constants And Register Compaction

- Behavior: parameter registers, varargs, open-call conventions, upvalue
  captures, debug line mapping, and return ranges remain correct.
- Work: clean repeated constant loads and compact temporary registers only
  after liveness proves shorter live ranges.
- Done: hot prototypes show lower register or dispatch pressure, or rejected
  cases are recorded in the ledger.
- Progress 2026-07-06: numeric `for` coercions now use constant-form `ADD_K`
  with a shared zero constant instead of materializing a dedicated zero
  register. This preserves numeric-string loop conversion while reducing simple
  numeric-loop register pressure.
- Progress 2026-07-06: compiled prototypes now choose their final register
  count from optimized bytecode plus child local-upvalue captures, instead of
  blindly keeping the compiler allocation high-water mark. This trims frame
  size after liveness DCE removes dead high-register locals while preserving
  parameter, open value-list, and captured-local anchors.

## Phase 22: Table Shape Tokens And Guarded Access

Purpose: deepen `Table` so row slots, finite maps, array access, and metatable
guards share one private shape interface.

Exit gate:

- caches guard on coherent shape tokens instead of scattered version checks;
- layout mutation, value mutation, array shape, generic-map shape, and metatable
  mutation have explicit invalidation rules;
- direct-frame table operations call shared raw/guarded helpers instead of
  duplicating storage checks;
- `tableAccess` remains the semantic owner of `__index` and `__newindex`.

### Slice 22.1: Table Shape Token

- Behavior: all table reads, writes, deletes, and metatable operations behave
  as before.
- Work: introduce a private token that can cover string layout, string values,
  array shape, array values, generic shape, metatable state, and storage mode.
- Bench: table guard rows and dynamic-key Scenario rows.
- Done: existing slot and cache guards can migrate to one token interface.
- Progress 2026-07-06: introduced a private `tableShapeToken` carrying current
  string layout, string value, and metatable state, then migrated inline string
  slots, dynamic string index caches, and table-field call caches from raw
  version fields to token comparisons. The public table behavior is unchanged;
  this creates the guard surface needed for the Phase 23 PIC work.

### Slice 22.2: Mutation Version Rules

- Behavior: nil deletes, inline string field deletion, string-map promotion,
  array holes, generic-key mutation, and metatable assignment stay visible.
- Work: define which token fields change for inline string add/delete/reorder,
  string value updates, dense array length or hole changes, generic map
  add/delete, and metatable set/clear.
- Done: stale slot, stale array, and stale metatable guards are rejected by
  tests.
- Progress 2026-07-06: `tableShapeToken` now exposes string storage, array
  layout/value, generic layout/value, and metatable guard categories. Raw table
  mutation routes generic fields and metatable assignment through private table
  helpers, and array append/update/delete plus sparse numeric promotion update
  the shared compact layout/value generations. The guard test covers string-map
  promotion, array value updates and holes, generic add/update/delete, and
  metatable set/clear while preserving the existing allocation budgets.

### Slice 22.3: Shape-Aware Raw And Guarded Helpers

- Behavior: `rawget`, `rawset`, ordinary access, metatable fallback, and
  invalid-key errors remain distinct.
- Work: centralize raw string, guarded string, array, dynamic string, and
  map-backed get/set helpers behind the table module.
- Bench: `economy_market_tick`, `threat_aggro_table`,
  `dialogue_condition_eval`, `save_state_diff`, table guard rows.
- Done: VM fast paths call a deep table-storage interface rather than
  open-coding storage checks.
- Progress 2026-07-06: added private table-owned helpers for raw dense-array
  value reads, map-backed generic field reads, and current-shape string slot
  reads. `rawget`/`rawGetKey`, direct-frame row string reads, direct constant
  field stores, predicate string-field branches, and direct generic-map
  `__index` probes now call those helpers instead of reaching into table
  storage from the VM. Dense-array iterator mechanics still read the array
  directly because that path owns raw sequence traversal.

### Slice 22.4: Row Slot Guard Migration

- Behavior: row-slot reads and writes fallback when shape, slot, or metatable
  assumptions change.
- Work: migrate row-field opcodes and descriptor facts from slot-index-only
  checks toward table shape tokens.
- Done: row-slot fast paths compose with PIC, path, kind, and predicate facts.
- Progress 2026-07-06: introduced a private `rowStringFieldSlotRef` execution
  helper and moved row string reads, writes, row predicate descriptors, row
  sub/add descriptors, and slot-backed string predicate branches through
  table-owned row slot helpers. Bytecode operands and descriptors still store
  compact slot indexes, but VM execution now resolves those indexes through
  shape-tokened table helpers that fall back by key or metatable semantics when
  shape assumptions no longer hold.

## Phase 23: Polymorphic Dynamic-Key And Handler Caches

Purpose: make finite dynamic-key workloads fast when one bytecode instruction
sees several stable keys.

Exit gate:

- dynamic index caches support a small finite keyset without source-name
  recognition;
- cache entries are guarded by table shape, key, slot, and metatable token;
- nil writes and metatable-backed access fallback through normal Luau paths;
- handler dispatch uses the same cache family instead of a separate special
  case.

### Slice 23.1: Four-Entry Dynamic String PIC

- Behavior: `table[key]` preserves missing-key nil, invalid keys, metatables,
  and host-visible mutation.
- Work: replace single-entry dynamic string index caches with a tiny private
  polymorphic inline cache for direct-frame string-key reads.
- Bench: `economy_market_tick`, `threat_aggro_table`,
  `save_state_diff`, `dialogue_condition_eval`.
- Done: rotating finite keysets stop thrashing one cache entry.
- Progress 2026-07-06: replaced the direct-frame dynamic string index cache's
  single entry with a four-entry private PIC while keeping the existing
  `get`/`store`/`set` cache seam. Read entries are guarded by table identity,
  key, shape token, and slot, retain four stable string keys for one bytecode
  instruction, evict on a fifth key, and reject stale shape tokens after table
  layout mutation. Existing nil, invalid-key, and metatable fallback behavior
  remains on the ordinary raw/runtime paths.

### Slice 23.2: Dynamic Write PIC

- Behavior: stable non-nil writes update the right slot; nil writes delete and
  invalidate through the ordinary raw delete path.
- Work: extend the PIC to writes guarded by table shape, key, slot, and
  metatable token.
- Bench: `economy_market_tick`, `threat_aggro_table`,
  `dialogue_condition_eval`.
- Done: finite-key updates stay direct while mutation remains visible.
- Progress 2026-07-06: made the dynamic string PIC write path explicit through
  a private `write` cache operation guarded by table identity, key, shape token,
  and slot. Four stable non-nil string-key writes update cached slots directly,
  while nil writes and stale-shape writes miss the PIC and continue through the
  ordinary raw set/delete path so mutation remains visible.

### Slice 23.3: Handler Table-Field Call PIC

- Behavior: missing handlers, non-functions, table mutation, metatable calls,
  yielding functions, and errors remain correct.
- Work: extend table-field call caching to a small PIC so
  `handlers[event.kind](...)` style dispatch can rotate through several stable
  keys.
- Bench: `event_dispatch`, `behavior_tree_tick`, method/call guard rows.
- Done: dynamic handler dispatch improves for the same reason finite maps do.
- Progress 2026-07-06: replaced the direct-frame table-field call cache's
  single entry with a four-entry handler PIC guarded by handler table identity,
  string key, table value token, and closure. The cache is lazy on `vmFrame` so
  non-handler programs do not pay the larger frame allocation cost. Tests cover
  four rotating handler keys, fifth-key eviction, stale handler mutation
  rejection, and existing dynamic handler mutation behavior.

### Slice 23.4: PIC Miss Accounting

- Behavior: no runtime behavior change when counters are disabled.
- Work: count monomorphic hits, polymorphic hits, key misses, shape misses,
  metatable misses, missing-key fallback, nil-write fallback, and invalid-key
  fallback.
- Done: cache size and rejected paths are based on measured miss classes.
- Progress 2026-07-06: added opt-in private direct-frame PIC counters on
  `vmThread`. Dynamic string PIC reads/writes and handler table-field calls now
  classify monomorphic hits, polymorphic hits, key misses, and stale shape/value
  misses through counted helper variants while the existing cache seam remains
  available for ordinary callers. Direct-frame dynamic index and handler paths
  also count metatable exits, missing string-key fallbacks, nil-write
  fallbacks, and non-string-key fallbacks. Counters are nil by default, so
  normal `Run` behavior and allocation shape are unchanged when measurement is
  disabled.

## Phase 24: Stable Global And Intrinsic Guards

Purpose: avoid repeated global and base-library table lookup when the runtime
environment proves stable.

Exit gate:

- `RunWithGlobals` overrides remain visible;
- global writes invalidate dependent facts;
- base table and intrinsic identity checks happen once per safe scope;
- guard misses resolve through normal call semantics before refreshing or
  falling back.

### Slice 24.1: Global Environment Version

- Behavior: assigning globals and passing host globals remain observable.
- Work: add a private `globalEnv` version that increments on writes and is
  initialized correctly for `RunWithGlobals` override state.
- Bench: global access guard rows and intrinsic-heavy Scenario rows.
- Done: optimized global loads can guard on environment version and name.
- Progress 2026-07-06: added a private `globalEnv.version` token. Base-only
  environments start at version zero, `RunWithGlobals` environments with host
  overrides start in a distinct nonzero state, and every observable global
  assignment increments the token while still writing through to the host
  globals map. Internal caching of base globals does not change the version, so
  future guards can depend on script-visible binding changes rather than
  runtime bookkeeping.

### Slice 24.2: Intrinsic Guard Descriptors

- Behavior: replacing `math`, `table`, `coroutine`, `rawlen`, `select`, or
  specific function fields changes behavior.
- Work: describe stable intrinsic calls with global name, field name, native
  identity, global version, and library table value version.
- Bench: `combat_tick`, `ability_resolution`, `buff_stack_tick`,
  `quest_progress_update`, `array_ops`.
- Done: repeated `math.min`, `table.insert`, `table.remove`, `rawlen`, and
  selected coroutine/base calls avoid repeated lookup when guarded.
- Progress 2026-07-06: extended finalized intrinsic descriptors with stable
  guard identity from the base intrinsic registry: global name, field name, and
  native function identity. Descriptor facts now render that identity in
  disassembly, and verifier equality covers it as part of stale artifact
  rejection. Runtime behavior is unchanged; later guard-miss work can combine
  these artifact facts with `globalEnv.version` and table value tokens at the
  execution seam.

### Slice 24.3: Intrinsic Guard Miss Path

- Behavior: metatables on base tables, script mutations, protected calls, and
  yielding calls remain visible.
- Work: resolve a guard miss through normal `runtimeTableAccess` and ordinary
  call semantics; refresh only if the same intrinsic identity is still present.
- Done: override tests prove fast paths do not hide global or library mutation.
- Progress 2026-07-06: deepened `baseFieldIntrinsicCallee` into the shared
  runtime seam for intrinsic guard lookup and miss handling. Explicit base
  library table fields are checked directly, so restored native identities
  become fast again, while missing fields and metatable-backed fields still
  resolve through `runtimeTableAccess` and ordinary call semantics. Tests cover
  host overrides, script mutation before a compiled intrinsic call, native
  restore, and repeated metatable lookup visibility. A persistent per-env guard
  cache was rejected after allocation-budget failures; future hoisting should
  use compile/runtime guard facts without enlarging every `globalEnv`.

### Slice 24.4: Import Guard Hoisting

- Behavior: any write that can change an imported global invalidates the guard.
- Work: hoist import guards to loop preheaders or function entry when CFG and
  global-write facts prove safety.
- Done: intrinsic lookup pressure leaves hot inner-loop profiles.
- Progress 2026-07-06: added a lazy active-thread intrinsic guard cache for
  host-global environments. Repeated intrinsic calls with unrelated host
  globals now resolve the absent imported base global once and reuse the guard
  while `globalEnv.version` and library table value tokens remain stable.
  Global writes and library table mutation invalidate the guard and fall back
  through the shared miss path. The cache is allocated only when a guarded
  host-global intrinsic path is used, and it reuses an existing thread pointer
  slot so default `Run` allocation budgets remain unchanged.

## Phase 25: Loop-Local Table Path Reuse

Purpose: stop rediscovering stable subtable paths inside loop bodies while
keeping Luau table mutation visible.

Exit gate:

- repeated paths such as `row.children`, `state.flags`, and `market.stock`
  reuse guarded table identities inside a loop-local region;
- writes, unknown aliasing, calls, metatable fallback, and nil intermediate
  values invalidate or reject the path fact;
- accepted path facts are visible through private disassembly.

### Slice 25.1: One-Segment Path Facts

- Behavior: repeated `row.field` loads still observe intervening writes,
  metatables, and nil values.
- Work: record guarded loop-local facts for stable one-segment constant string
  paths when owner shape and aliasing are safe.
- Bench: `buff_stack_tick`, `cooldown_scheduler`,
  `quest_progress_update`, `path_relaxation`.
- Done: repeated subtable loads become one guarded path fact plus fallback.
- Progress 2026-07-06: added finalized private one-segment path facts for
  repeated string field reads inside backward loops. Detection groups duplicate
  string constants by field text, includes row-slot-backed string reads, and
  rejects loops with table/global writes or calls. Accepted facts are visible in
  disassembly as `path_fact` lines and verifier coverage rejects stale fact
  drift. Runtime behavior is unchanged while the fact seam is made visible for
  later cache fallback work.

### Slice 25.2: Two-Segment Path Facts

- Behavior: `row.child[key]`, `row.child.field`, and nil intermediate errors
  preserve current behavior.
- Work: extend path facts to two segments with table shape, dynamic-key PIC,
  and metatable guards.
- Bench: `economy_market_tick`, `dialogue_condition_eval`,
  `behavior_tree_tick`, `path_relaxation`.
- Done: nested maps and arrays reuse stable parent paths without field-name
  recognition.
- Progress 2026-07-06: extended finalized private path facts to repeated
  two-segment paths. Static `row.child.field` reads record both string
  segments, and dynamic `row.child[key]` reads record the stable parent segment
  plus a `dynamic_key` marker. Detection groups duplicate string constants by
  text, handles compiler-emitted `GET_STRING_FIELD2` and
  `GET_STRING_FIELD_INDEX`, preserves existing runtime behavior, and keeps
  verifier/disassembly coverage on the finalized artifact.

### Slice 25.3: Alias And Mutation Invalidation

- Behavior: any store or call that can alter the owner table, subtable field,
  metatable, or path value invalidates dependent facts.
- Work: teach the finalizer a conservative alias model for loop-local paths.
  Calls kill path facts unless proven non-mutating.
- Done: rejected path reuse is explicit and accepted facts have verifier
  coverage.
- Progress 2026-07-06: added finalized private path-fact rejection descriptors.
  Loop-local candidates with repeated paths now record explicit rejection
  reasons when a table write, global write, or call can invalidate the owner,
  subtable, metatable, or path value. Rejections are visible in disassembly as
  `path_fact_rejection` lines and verifier coverage rejects stale accepted and
  rejected path artifacts.

### Slice 25.4: Runtime Path Cache Fallback

- Behavior: bytecode stays unchanged when liveness cannot safely reuse a
  register.
- Work: attach a runtime path cache to the load PC for cases where a compile
  rewrite is not safe but the same guarded path repeats.
- Done: the mechanism helps path-heavy loops without forcing unsound register
  reuse.
- Progress 2026-07-06: attached a lazy active-thread runtime path cache to
  accepted static two-segment direct-frame loads. `GET_STRING_FIELD2` now uses
  finalized path facts to cache the base table identity plus guarded first and
  second string slots by load PC, falling back to the existing raw/metatable
  path when any guard misses. Bytecode and register liveness remain unchanged,
  and allocation guards cover that normal runs do not pay for the lazy cache.

## Phase 26: Value-Kind And Predicate Fact System

Purpose: stop rechecking `Value.kind` and predicate sources after compiler and
runtime facts already prove the common kind.

Exit gate:

- register, slot, constant, branch, and path facts can carry number, string,
  boolean, table, and nil knowledge;
- facts are invalidated by calls, stores, metatable fallback, joins, varargs,
  open-call results, and unknown alias writes;
- new branch work prefers descriptor-driven predicates over more opcode
  families.

### Slice 26.1: Register And Constant Kind Facts

- Behavior: values can still change kind through assignment, calls, varargs,
  table loads, and fallback edges.
- Work: propagate kind facts for literals, numeric ops, string constants,
  boolean predicates, table literals, `rawlen`, and known intrinsic returns.
- Bench: arithmetic-heavy and predicate-heavy Scenario rows.
- Done: obvious number/string/boolean registers use direct branch and
  arithmetic paths without repeated generic checks.
- Progress 2026-07-06: added finalized private constant and register kind
  facts to the execution artifact seam. Constants now emit
  `constant_kind` facts for nil, boolean, number, string, and table-shaped
  values; straight-line register writes emit `register_kind` facts for
  literal loads, propagated moves, table literals, length results, proven
  numeric operations, simple comparisons, and guarded intrinsic result
  candidates. Propagation resets at basic-block starts and control-flow edges,
  and calls, varargs, table loads, stores, and other unknown writes clear
  affected register knowledge through the existing instruction write model.
  Verifier coverage rejects stale constant and register kind facts. Runtime
  behavior is unchanged until later predicate and numeric fast paths consume
  these facts.

### Slice 26.2: Slot And Path Kind Facts

- Behavior: table slots remain mutable and host-visible.
- Work: attach guarded kind facts to row slots, finite-key slots, and reused
  paths when table shape and store history prove the value kind.
- Bench: `combat_tick`, `inventory_value`, `cooldown_scheduler`,
  `formation_layout_score`.
- Done: field reads feed numeric, string, boolean, table, and nil facts into
  later operations.
- Progress 2026-07-06: added finalized private `slot_kind` and `path_kind`
  facts. Slot facts are derived from string-field table literal writes and
  row-slot stores when the stored value kind is already known; they carry the
  guarded field constant, slot index, value kind, and source. Path facts now
  attach guarded table-kind evidence to accepted nested path parents such as
  `row.child` in repeated `row.child.value` loads. The verifier rejects stale
  slot/path kind facts, and disassembly exposes both fact families. Runtime
  behavior is unchanged; later predicate and arithmetic consumers can use these
  guarded facts instead of rediscovering the same table/field kinds.

### Slice 26.3: Predicate Branch Descriptors

- Behavior: Luau truthiness, `nil`, `false`, non-boolean truthy values, NaN,
  table equality, and metamethod-capable comparisons stay correct.
- Work: generalize predicate branches with descriptors for register, row-field,
  and path-field sources; operations include truthy, nil, equality to constant,
  inequality to constant, and numeric comparison.
- Done: existing specialized opcodes can remain, but new predicate work flows
  through the descriptor layer.
- Progress 2026-07-06: added finalized private `predicate_branch`
  descriptors. The descriptor layer summarizes existing register, string-field,
  row-field, row-field-pair, and path-field branch sources with operations such
  as truthy, nil/not-nil, equality-to-constant, field equality, and numeric
  comparison. Path-field descriptors can also recognize a generic comparison
  register fed by a guarded `GET_STRING_FIELD2` path fact, so nested predicates
  such as `row.child.value > 0` no longer require a new opcode family before
  later consumers can see the predicate source. Verifier coverage rejects stale
  predicate descriptors, and disassembly exposes the normalized predicate
  source/op vocabulary. Runtime behavior is unchanged.

### Slice 26.4: Finite Tag, Nil, And Boolean Refinement

- Behavior: `if`/`elseif` chains still evaluate in source order and preserve
  side effects.
- Work: refine facts along finite string tag chains, `value ~= nil`, boolean
  field branches, and negative equality edges.
- Bench: `inventory_value`, `quest_progress_update`,
  `dialogue_condition_eval`, `procgen_room_scoring`, `path_relaxation`.
- Done: branch bodies inherit useful facts without source-specific switches.
- Progress 2026-07-06: added finalized private `branch_refinement` and
  `finite_tag_refinement` facts derived from predicate descriptors. Branch
  refinements record the fallthrough and target-edge facts for truthy/falsey,
  nil/not-nil, equality/negative-equality, field equality, and numeric
  comparison branches. Finite tag refinements group repeated string
  equality-to-constant predicates for the same register or field source while
  preserving source-order evaluation. Verifier coverage rejects stale branch
  and finite-tag refinement artifacts, and disassembly exposes the edge fact
  vocabulary for later branch consumers. Runtime behavior is unchanged.

## Phase 27: Direct-Frame Slow-Path Islands

Purpose: avoid abandoning direct-frame execution when one operation needs a
generic Luau fallback.

Exit gate:

- a guard miss can execute one bounded slow operation and resume direct-frame
  execution at the next bytecode;
- yielding, protected calls, debug hooks, instruction budgets, and unbounded
  metamethod recursion still leave or block direct-frame execution;
- side exits are counted by reason.

### Slice 27.1: Side-Exit Contract

- Status: done, 2026-07-06. Direct-frame execution now returns one private
  side-exit result that represents resume, return, call, yield, generic-frame
  entry, and failure.
- Behavior: fallback operations preserve errors, PC, register writes, open-call
  state, pending-call state, and result adjustment.
- Work: define a private direct-frame side-exit result that can resume, return,
  call, yield, enter generic frame, or fail.
- Done: one VM seam owns direct-frame fallback behavior instead of each opcode
  improvising.
  `runFrame` is the adapter from the direct-frame side-exit contract back to
  the existing frame-result loop, while direct-frame opcode cases choose named
  exits instead of returning raw tuple states.

### Slice 27.2: Table Access Islands

- Status: done, 2026-07-06. Direct-frame table get/set misses now execute a
  bounded table-valued metatable island and resume direct execution when no
  function-valued metamethod call is needed.
- Behavior: `__index`, `__newindex`, missing keys, invalid keys, and metatable
  cycles preserve current results.
- Work: let direct-frame table gets/sets call a bounded slow table-access
  island and resume when no yield or call escape occurs.
- Bench: table guard rows plus dynamic-key Scenario rows.
- Done: mostly raw table paths no longer lose the whole direct runner because
  one operation needed semantic fallback.
  `GET_FIELD`, `GET_STRING_FIELD`, `GET_ROW_STRING_FIELD`, `GET_INDEX`,
  `SET_FIELD`, `SET_STRING_FIELD`, `SET_ROW_STRING_FIELD`, and `SET_INDEX`
  share the private table-island helpers. Function-valued `__index` and
  `__newindex` still enter the generic frame so calls and yields remain owned
  by the full VM path.

### Slice 27.3: Intrinsic And Fixed-Call Islands

- Status: done, 2026-07-06. Overridden fixed-result non-yielding intrinsic
  calls can run as direct-frame islands and resume direct execution; open-result
  and call-capable cases still enter the generic frame.
- Behavior: overridden globals, base-library fields, protected calls, yielding
  calls, and host errors remain visible.
- Work: allow intrinsic guard misses and fixed-result non-yielding calls to
  execute one slow island and resume when PC/register effects are complete.
- Bench: intrinsic-heavy and dispatch-heavy Scenario rows.
- Done: guard misses stay correct without permanently leaving direct-frame
  execution.
  `MATH_MIN` uses the fixed-call island for one-result non-yielding overrides,
  and table intrinsic overrides only use the island when their result count is
  fixed and positive. Script, yieldable, protected, and open-result calls remain
  on the generic VM path.

### Slice 27.4: Side-Exit Counters

- Status: done, 2026-07-06. Optional direct-frame counters now record table,
  intrinsic, call, metatable, debug, budget, yield, error, and generic-frame
  side-exit reasons.
- Behavior: no runtime behavior change when counters are disabled.
- Work: count side exits by table, intrinsic, call, metatable, debug, budget,
  yield, error, and generic-frame reasons.
- Done: future superblock or island work targets real escape reasons.
  The counts live on the existing opt-in direct-frame counter storage rather
  than adding another hot `vmThread` field. Resumable table/intrinsic islands
  count at the island point, result exits count at the `runFrame` adapter, and
  debug or instruction-budget blocks count before direct-frame execution is
  skipped.

## Phase 28: Call And Value-List Lifetime

Purpose: reduce copied slices and result-list churn around Luau calls,
metamethods, returns, varargs, and protected calls.

Exit gate:

- multiple returns, varargs, final-call expansion, protected calls, host calls,
  coroutine yield/resume, and public result ownership remain exact;
- common fixed-argument and fixed-result paths avoid internal allocation;
- host callbacks cannot retain borrowed mutable windows unsafely.

### Slice 28.1: Private Value-List Carrier

- Status: done, 2026-07-06. Frame results and open-call result retention now
  use one private value-list carrier with inline, slice, count, and borrowed
  ownership state.
- Behavior: visible return and assignment adjustment remains unchanged.
- Work: introduce an internal carrier with inline storage, optional slice,
  count, and borrowed ownership flag.
- Done: script calls, host/native calls, metamethod calls, protected-call
  wrappers, frame results, and open-call results can share one lifetime model.
  `vmFrameResult` now carries `vmValueList` instead of its own parallel
  inline/slice representation. Raw call-result slices are treated as borrowed
  at the frame destination seam, so open-call retention copies through the
  carrier while fixed destinations still read directly.

### Slice 28.2: Borrowed Fixed Argument Windows

- Status: done, 2026-07-06. Fixed internal script/native call arguments now
  flow through an explicit private argument-window module that marks borrowed
  versus retained slices.
- Behavior: callees receive the same arguments and cannot observe caller
  register mutation incorrectly.
- Work: pass borrowed register windows through fixed script/native calls when
  lifetime analysis proves safety; copy at public or retaining boundaries.
- Bench: `event_dispatch`, `method_calls`, `recursive_fibonacci`.
- Done: fixed call argument slices disappear from hot paths where safe.
  `scriptCallArgs` delegates to `borrowedFixedCallArgs`, which borrows ordinary
  register windows and copies when captured cells are in range. Host, unknown,
  and retaining call boundaries use `retainedFixedCallArgs`, preserving the
  existing host argument isolation behavior.

### Slice 28.3: Inline Fixed Results

- Status: done, 2026-07-06. Inline fixed-result application now routes through
  the private value-list carrier instead of carrying a second adjustment
  implementation.
- Behavior: zero, one, many, open returns, varargs, `pcall`, `xpcall`, and
  coroutine pending calls keep Luau adjustment rules.
- Work: apply one-result and two-result outputs directly to registers when the
  call shape is fixed and non-yielding.
- Bench: `event_dispatch`, `varargs_select`, `coroutine_yield`,
  method/call guard rows.
- Done: internal result slice allocation falls without changing public result
  ownership.
  `applyInlineResultDestination` now builds an inline-array `vmValueList` and
  delegates to the same destination helper as frame results and borrowed call
  results, preserving fixed and open-result adjustment through one module.

### Slice 28.4: Metamethod Argument Scratch

- Status: done, 2026-07-06. Common runtime metamethod calls now use fixed
  one/two/three-argument scratch helpers instead of short slice literals.
- Behavior: metamethod argument order, errors, and fallback semantics remain
  exact.
- Work: use fixed scratch storage for common one/two-argument metamethod calls
  instead of allocating short slices.
- Done: correctness fallback paths stay cheap enough for slow-path islands.
  The helpers preserve host-retained argument windows by using per-call fixed
  storage, while table `__index`/`__newindex`, unary, binary, length, concat,
  and conversion metamethod paths share the same argument construction helpers.

## Phase 29: Numeric And Reduction Block Plans

Purpose: finish numeric and reduction cleanup after table, path, PIC, global,
and kind facts make operands stable enough to optimize safely.

Exit gate:

- numeric facts are explicit, invalidated, and inspected in tests;
- NaN, negative zero where relevant, floor division/mod, tie behavior, branch
  side effects, and metamethod-capable paths remain correct;
- reductions are described by accumulator, candidate, predicate, and mutation
  facts, not benchmark bodies.

### Slice 29.1: Kind-Proven Numeric Fast Paths

- Status: done, 2026-07-06. Direct-frame arithmetic and ordered comparison
  opcodes now consume explicit numeric operand facts derived from the
  value-kind propagation pass, with a private per-PC fact map for the hot VM
  check.
- Behavior: numeric strings, non-numbers, and metamethod-capable operands still
  fallback where Luau requires them.
- Work: use value-kind facts inside direct-frame arithmetic and comparison to
  avoid repeated `Value.kind` checks.
- Bench: `ai_utility_scoring`, `formation_layout_score`,
  `procgen_room_scoring`, `path_relaxation`.
- Done: numeric helpers leave hot profiles where operands are already proven.
  Numeric operand facts are disassembled and verifier-checked, and the direct
  comparison path keeps the NaN side exit so normal error behavior is
  preserved.

### Slice 29.2: General Reduction Descriptors

- Status: done, 2026-07-06. Max/best reductions of the form
  `if candidate > accumulator then accumulator = candidate ... end` and
  all-complete boolean reductions of the form `if predicate then complete =
  false end` now emit explicit reduction facts. Absolute-delta normalization
  of the form `if delta < 0 then delta = -delta end` also emits a reduction
  fact, and paired-row diff loops that fetch `right = after[i]` from the
  current left-row iterator emit paired-row diff facts. Best-index style branch
  bodies can include additional move-only mutations; all-complete and
  paired-row descriptors reject calls, stores, and aliasing shapes that would
  make later block plans unsafe.
- Behavior: loops still observe every relevant predicate, call, table store,
  and branch side effect.
- Work: describe max/best, all-complete, absolute-delta, and paired-row diff
  reductions as general facts.
- Bench: `ai_utility_scoring`, `quest_progress_update`,
  `formation_layout_score`, `procgen_room_scoring`, `save_state_diff`.
- Done: reduction rows improve through one reusable mechanism family.
  This slice completes the descriptor surface; runtime improvement from these
  facts is intentionally left to Slice 29.3 direct block plans.

### Slice 29.3: Direct Block Plans

- Status: partially done, 2026-07-06. Absolute-delta, max/best branch,
  paired-row diff field-read regions, row-field add-store regions, and simple
  row-field branch-ending set/arithmetic/sub-add store regions now emit
  verified direct block plans, and the direct-frame runner consumes those plans
  to execute the planned bytecode region and resume at the next bytecode.
- Behavior: every guard failure resumes at the correct bytecode PC.
- Work: build small verified block plans for numeric ops, field reads, stores,
  and branch-ending regions only when facts prove reusable shapes.
- Bench: branch-heavy and reduction-heavy Scenario rows.
- Done: switch dispatch and row-slot lookup work fall without adding
  benchmark-shaped fused bodies. Remaining work includes wider branch-ending
  table regions beyond single row-field set/add/sub/sub-add store-backs.

### Slice 29.4: Rejection Tests

- Behavior: unsafe reductions execute ordinary bytecode.
- Work: reject reductions with calls, stores that invalidate inputs,
  unsupported joins, unknown alias writes, or side-effectful branch conditions.
- Done: rejected cleverness is recorded rather than kept as dead complexity.

## Phase 30: Mechanism Instrumentation And Burn-Down

Purpose: make the final optimization choices evidence-based without measuring
or optimizing benchmark bodies directly.

Exit gate:

- counters group pressure by reusable mechanism and fact family;
- no counter names Scenario rows or field/tag names;
- every post-instrumentation slice is kept only if it helps multiple language
  shapes or has clear runtime-wide value;
- every Scenario row is <= 2.0x same-run Luau batch.

### Slice 30.1: Private Mechanism Counters

- Behavior: no runtime behavior change when counters are disabled.
- Work: count direct-frame entries/completions, side exits, row-slot hits,
  mono/PIC hits, PIC miss reasons, path reuse hits and invalidations,
  intrinsic guard hits/misses, value-list copies, result slice allocations, and
  metamethod scratch allocations.
- Done: profiles can distinguish dispatch, dynamic table, path reload,
  intrinsic, call/value-list, predicate/kind, numeric/reduction, and
  allocation pressure.

### Slice 30.2: Fact And Counter Snapshots

- Behavior: no runtime behavior change.
- Work: add private disassembly or snapshot lines for facts and counters, such
  as PIC slot count, path guard shape, intrinsic guard shape, and predicate
  source.
- Done: tests and ledger entries can name the general mechanism that moved.

### Slice 30.3: Full-Corpus Burn-Down Loop

- Behavior: no public behavior regression.
- Work: run full same-run Scenario ratios, pick the current worst row,
  classify pressure by mechanism counters, implement one general mechanism
  slice, and rerun related rows plus the full gate.
- Done: the worst ratio falls or the attempt is removed and recorded.

### Slice 30.4: Mechanism Deletion Audit

- Behavior: no runtime behavior change except removing rejected paths.
- Work: delete facts, opcodes, helpers, caches, or block plans that cannot
  prove reuse across a language pattern.
- Done: no accepted optimization is only justified by one Scenario row.

## Phase 31: Retirement And Stretch Targets

Purpose: close the 2.0x plan cleanly and decide whether to continue toward
1.5x or 1.3x.

Exit gate:

- full 2.0x gate passes;
- final evidence is in the benchmark ledger;
- remaining work is split into a smaller follow-up plan.

### Slice 31.1: 2.0x Retirement Gate

- Behavior: all benchmark fixtures still match expected results.
- Work: record final ratio table, allocation table, mechanism counters, and
  remaining stretch target opportunities.
- Done: the 2.0x gate passes and this plan is replaced by a smaller 1.5x or
  1.3x plan if needed.

### Slice 31.2: Guard Row Freeze

- Behavior: no runtime change.
- Work: record the Top10, Classic, and Scenario current-row guard bands that
  future work must preserve.
- Done: future optimization plans cannot spend the 2.0x gains silently.

### Slice 31.3: Stretch Plan Decision

- Behavior: no runtime change.
- Work: choose between a 1.5x plan, a 1.3x plan, or a maintenance-only plan
  based on the final profile evidence.
- Done: this document is retired instead of continuing as an ever-growing
  backlog.

## Checks

Behavior and allocation guard:

```sh
go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets'
```

Focused ratio:

```sh
go test -run '^$' -bench 'BenchmarkScenarioLuau/<case>/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./...
```

Full 2.0x gate:

```sh
go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=1s -count=3 ./... | SCENARIO_RATIO_MAX=2.0 scripts/scenario-ratio-gate
```

Shared guard rows:

```sh
go test -run '^$' -bench 'BenchmarkTop10Luau/(table_fields|array_ops|generic_iteration|method_calls|coroutine_yield)/ember_run$|BenchmarkClassicLuau/(recursive_fibonacci|iterative_fibonacci)/ember_run$' -benchmem -benchtime=1s -count=3 ./...
```

Pre-finish:

```sh
scripts/check-lane root
scripts/check-fast
scripts/check
```

## Risks

- Compatibility: dynamic table and intrinsic fast paths can accidentally skip
  metatable or host override semantics.
- Performance: cache guards can cost more than they save on small tables.
- Public interface: keep all optimizer facts private until a real embedder
  needs observability.
- Maintenance: avoid one opcode per benchmark shape; prefer one mechanism per
  language pattern.
