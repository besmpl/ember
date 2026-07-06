# Plan: Scenario Under 2x Speed Work

## Goal

Improve Ember's Scenario benchmark ratios until every row is at most 2.0x
slower than same-run `luau_cli_batch`, without benchmark-named helpers,
source-specific recognizers, or one-off opcode crumbs.

The performance work should deepen a few private modules that compose existing
facts into larger guarded execution plans. The current evidence says the hot
path is mostly ordinary `runDirectFrame` work: table access, path lookup,
dispatch, fixed script calls, and repeated guards.

## Scope

In:

- Private VM, compiler-finalization, execution-artifact, table, path, call, and
  intrinsic/global guard mechanisms.
- Global Luau-shaped patterns such as table slots, nested dynamic paths,
  predicate chains, numeric reductions, fixed calls, and intrinsic calls.
- Read-only profiling and benchmark-led prioritization before each slice.

Out:

- Benchmark-named helpers or descriptors.
- Whole-benchmark body recognizers.
- Scenario-row-specific opcodes.
- Public `Compile`, `Run`, or `RunWithGlobals` interface expansion unless a
  later embedding need independently proves it.

## Design Thesis

Ember should stop optimizing isolated opcodes and deepen the private execution
artifact. The VM already has useful local facts, but it still consumes them one
instruction at a time. The next speed layer should move complexity behind small
private interfaces:

- The table module owns storage truth and precise invalidation.
- The path module owns composed nested table access.
- The execution artifact owns fact composition and block planning.
- The verifier and side-exit module owns plan safety before large plan families
  exist.
- The direct-frame VM owns execution of verified plans, not discovery of source
  shapes.
- The call adapter owns fixed script-call frame lifetime.
- The intrinsic/global guard adapter owns replacement safety.

These modules should increase leverage for all callers and locality for future
performance fixes.

## Phase 0: Measurement Before Motion

### Slice 0.1: Mechanism Counters

Seam: VM execution artifact and direct-frame execution.

Interface: opt-in counters grouped by mechanism, not benchmark name:

- direct-frame dispatch by opcode class;
- dynamic string PIC hit, miss, stale, and rejection reason;
- runtime path cache hit, miss, stale, and rejection reason;
- table guard miss reason;
- direct block entry, resume, side exit, and fallback reason;
- fixed-call frame reuse, materialization, and copy counts;
- intrinsic/global guard check, hit, and miss counts.

Progress 2026-07-06: direct block plans now count opt-in private entries,
direct resumes, fallbacks, and fallback reasons on the existing nil-by-default
direct-frame counter storage. The first tracer covers row add-store block
resumes and numeric-string operand fallback to ordinary Luau semantics. Runtime
path caches now count opt-in private stores, hits, first misses, and stale guard
misses for static and dynamic two-segment paths. Base-field intrinsic/global
guards now count opt-in private checks, hits, and misses through the same
counter storage. Fixed script-call frame lifetime now counts opt-in private
direct-leaf reusable frame entries, full-frame materializations, argument
copies, and register copies when a direct-leaf side exit materializes a normal
VM frame. A private attribution runner now combines opcode dispatch counts and
mechanism counters for benchmark rows without expanding the public runtime
interface. The first fresh sweep showed current worst-row pressure in fixed
script/table-field calls, dynamic index paths, intrinsic guard misses, direct
blocks, row-field dispatch, and array iteration.

Implementation hidden: attribution logic for where hot time is spent inside
`runDirectFrame`.

Leverage: prevents optimization order from becoming Scenario folklore.

Correctness and performance hazards:

- disabled counters must be allocation-free and effectively zero cost;
- enabled counters must not change behavior;
- output must be by mechanism class, not benchmark row.

Checks:

```sh
go test -count=1 ./... -run 'BenchmarksMatch|AllocationBudgets'
go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=300ms -count=1 ./...
scripts/scenario-ratio-gate < /tmp/scenario.out
```

Done when disabled counters do not perturb benchmarks and enabled counters
explain the main hot mechanisms for the worst rows.

Abandon or reorder if disabled counters add measurable overhead or attribution
is still too vague to choose between table/path, superblock, and call work.

## Phase 1: Verifier And Side-Exit Skeleton

This phase moves the superblock seam early without committing to large block
families yet. Table and path work should be designed against the real verifier
and side-exit contract, not against an imagined future consumer.

### Slice 1.1: Verified Plan Shell

Seam: compiler/finalizer execution artifact to direct-frame VM.

Interface: a private verified-plan shell conceptually exposes:

```text
verifyRegion(proto, pc, candidate) -> verified plan or reject reason
planAt(pc) -> nil or verified plan
executeVerifiedPlan(frame, thread, plan) -> next pc or side exit
```

The interface should be typed and boring. It should support rejection reasons,
counter hooks, and a small side-exit result, but it should not expose table
storage internals, path internals, or Scenario-row names.

Implementation hidden:

- region acceptance and rejection;
- guard ordering metadata;
- PC recovery;
- register-state repair;
- instruction budget accounting;
- counter attribution;
- fallback to ordinary direct-frame dispatch.

Leverage: every later table, path, call, and intrinsic optimization gets one
place to ask "is this safe to execute as a verified region?"

Correctness hazards:

- side exits must resume at the exact bytecode PC;
- live registers must match ordinary direct-frame execution;
- rejected regions must behave as if no plan existed;
- instruction limits and error state must remain observable;
- hooks, yields, open returns, and uncertain calls must reject early.

Checks:

```sh
go test -count=1 ./... -run 'Direct|Block|SideExit|BenchmarksMatch'
```

Progress 2026-07-06: added a private verified-plan shell around existing
direct block plans. The execution artifact now finalizes verified plans, a
per-PC verified plan map, and rejection descriptors. The verifier rejects stale
verified-plan state, and the direct-frame VM now consumes existing direct block
families through `verifiedDirectBlockPlanAt` while preserving direct block
entry, resume, and fallback counters.

Done when the VM can consume a verified plan shell, side-exit back to ordinary
dispatch, and count entries/fallbacks without changing semantics.

Abandon or reorder if the side-exit interface starts needing table/path-specific
details. That means the seam is too shallow.

### Slice 1.2: Side-Exit Contract Tests

Seam: same verifier and direct-frame VM seam.

Interface: tests cross the verified-plan interface, not private plan internals.

Implementation hidden:

- restoring PCs for fallthrough and branch exits;
- preserving registers after guard failure;
- maintaining exact error and budget behavior;
- rejecting plans around calls, yields, hooks, and unknown effects.

Progress 2026-07-06: added the private `executeVerifiedPlan` seam and routed
direct-frame execution through it before opcode dispatch. The executor now owns
verified-plan entries, resumes, fallbacks, and PC recovery for all existing
direct block families. Contract tests cover fallback preserving the original
PC and row/register state, and `verifyRegion` now rejects call/yield-risk
regions before execution.

Correction 2026-07-06: normal direct-frame runs no longer execute verified
direct-block plans by default. Focused benchmarks showed the current direct
block families were net-negative when routed through the verified-plan shell,
especially in `formation_layout_score`, `save_state_diff`, and
`ability_resolution`. Verified-plan metadata and the executor remain available
behind the private mechanism-counter/PIC instrumentation seam for attribution
and future profitable families, but ordinary `Run` stays on direct opcode
dispatch until a later slice proves a plan family pays for itself.

Done when table/path phases can add facts to the verifier without inventing a
new exit mechanism.

## Phase 2: Table Shape And Storage Locality, Metadata First

### Slice 2.1: Split Table Epochs

Seam: private `Table` module.

Interface: the VM asks for guarded table descriptors. It should not know whether
the table uses inline fields, string maps, generic maps, array storage, promoted
storage, or finite-key metadata.

Implementation hidden:

- independent string layout and string value epochs;
- independent array layout and array value epochs;
- independent generic layout and generic value epochs;
- metatable epoch;
- nil deletion;
- inline-to-map promotion;
- map-backed string slot descriptors;
- raw versus semantic access fallback.

Progress 2026-07-06: split the private table shape token into independent
string, array, and generic layout/value epochs. String slot guards still use the
same VM-facing descriptor interface, but array and generic writes no longer
invalidate unrelated string metadata. Array append, hole fill, deletion,
contiguous promotion, sparse numeric fields, generic add/update/delete, string
map promotion, and nil deletes now advance only the semantically relevant
storage-family epochs.

Leverage: reduces stale guards and repeated `rawStringField` cost across
economy, dialogue, threat, save-state, cooldown, path, and behavior-tree style
rows.

Correctness hazards:

- metatable changes;
- `__index` and `__newindex`;
- raw access versus semantic access;
- nil writes deleting fields;
- table identity replacement;
- string map promotion;
- generic key collisions;
- NaN keys;
- array holes;
- host-created globals and `RunWithGlobals`.

Done when unrelated writes do not invalidate unrelated slot metadata, while all
semantically relevant mutations invalidate the correct descriptors.

### Slice 2.2: Finite-Key Guarded Slots

Seam: same private `Table` module.

Interface: guarded descriptors for stable string and dynamic slots, including
map-backed string storage. The VM consumes descriptors and miss reasons.

Implementation hidden: finite-key storage, promotion, deletion, stale guard
detection, and fallback routing through existing table semantics.

Progress 2026-07-06: string slot descriptors now cover both inline string
fields and map-backed string storage behind the private `Table` seam.
`rawStringFieldSlot`, `rawStringFieldAtSlot`, and `setRawStringFieldAtSlot`
hide storage kind from callers, and the direct-frame dynamic string index cache
now consumes those descriptors instead of peeking at inline string fields.
Regression tests cover map-backed reads, writes, unrelated value updates,
layout-stale deletes, and dynamic cache stale-shape fallback.

Rows expected to move:

- `economy_market_tick`;
- `dialogue_condition_eval`;
- `threat_aggro_table`;
- `save_state_diff`;
- `behavior_tree_tick`;
- `path_relaxation`;
- `cooldown_scheduler`.

Checks:

```sh
go test -count=1 ./... -run 'Table|rawget|rawset|metatable|BenchmarksMatch'
go test -run '^$' -bench 'BenchmarkScenarioLuau/(economy_market_tick|dialogue_condition_eval|threat_aggro_table|save_state_diff|behavior_tree_tick|path_relaxation)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=3 ./...
```

Abandon or reorder if counters show table guards already hit reliably and the
remaining cost is mostly dispatch or calls.

## Phase 3: Fact Lifetime And Loop-Local Reuse

This phase gives the verifier a conservative lifetime model before composed
paths depend on it. The goal is locality: facts are created, killed, and reused
in one module instead of rediscovered by each fast path.

### Slice 3.1: Fact-Kill Rules

Seam: compiler/finalizer fact analysis to execution artifact verifier.

Interface: a private fact-lifetime descriptor conceptually records:

- where a fact is born;
- which region or loop may reuse it;
- which operation kills it;
- whether the kill is table-local, global, call-driven, metatable-driven, or
  unknown;
- which fallback pc preserves ordinary semantics.

Implementation hidden:

- alias-sensitive table writes;
- unknown host calls;
- global writes;
- metatable mutation;
- closure calls that may mutate captured tables;
- branch joins;
- loop backedges;
- debug and yield safety.

Leverage: table slots, paths, intrinsic guards, and block plans all need the
same conservative answer to "does this fact still hold?"

Correctness hazards:

- keeping facts alive across unknown calls;
- treating aliased table writes as independent;
- missing metatable invalidation;
- keeping value-kind facts alive across register mutation;
- allowing branch joins to merge incompatible facts.

Progress 2026-07-06: path fact rejections now carry a private fact-lifetime
explanation: first repeated fact birth PC, kill class, kill PC, fallback PC,
and human-readable reason. Existing loop-local path fact barriers still reject
conservatively on table writes, global writes, and calls, but the execution
artifact verifier and disassembly can now explain why a fact died without
callers knowing table/path internals.

Done when the verifier can explain why a fact survives or dies without a caller
knowing the underlying table/path implementation.

### Slice 3.2: Loop-Local Reuse Rules

Seam: same fact-lifetime module.

Interface: loop-local descriptors mark facts that may be reused across a loop
body and backedge, plus the instructions that kill them.

Implementation hidden:

- loop header discovery;
- backedge validation;
- invariant parent table identity;
- dynamic key reuse;
- path-parent reuse;
- rejection around calls, unknown writes, and metatable-relevant operations.

Progress 2026-07-06: accepted loop-local path facts now include explicit
birth PC, backedge PC, fallback PC, and `kill none` metadata. Rejected facts
carry matching kill descriptors from Slice 3.1. This keeps loop-local reuse
facts in the verifier artifact rather than forcing future path plans to infer
lifetimes from raw bytecode ranges.

Leverage rows:

- `economy_market_tick`;
- `dialogue_condition_eval`;
- `cooldown_scheduler`;
- `path_relaxation`;
- `behavior_tree_tick`;
- `quest_progress_update`.

Checks:

```sh
go test -count=1 ./... -run 'Fact|Loop|Path|Direct|BenchmarksMatch'
```

Done when later path plans can reuse parent/path facts inside loops through the
verifier instead of adding private lifetime logic.

Abandon or reorder if the model cannot stay conservative and simple. In that
case, keep facts per instruction and move sooner to narrower path descriptors.

## Phase 4: Composed Path Access

### Slice 4.1: Path Plan Module

Seam: between verified execution artifact facts and direct-frame VM.

Interface: a private path plan conceptually contains:

- base register;
- static parent segments;
- dynamic key source;
- access kind: read, write, or read-modify-write;
- parent table guard;
- child storage guard;
- fallback pc.

Implementation hidden:

- parent-path reuse;
- dynamic key checks;
- table identity checks;
- shape and value guards;
- metatable fallback;
- nil-parent fallback;
- invalidation after unknown writes or calls;
- loop-local reuse.

Progress 2026-07-06: added a private `pathPlanDesc` artifact derived from
loop-local path facts. Read plans now record PC, access kind, loop range, base
register, static parent/child fields or dynamic key source, and fallback PC.
The verifier rejects stale path-plan state, and disassembly exposes the private
plan shape for future direct-frame consumers.

Depth: one module owns nested table access instead of each opcode rediscovering
pieces of the same path.

### Slice 4.2: Shared Read, Write, And Read-Modify-Write Paths

Target ordinary Luau patterns:

```lua
map[key]
map[key] = value
map[key] = map[key] + delta
parent.child[key]
parent.child[key] = parent.child[key] - delta
```

Leverage rows:

- `economy_market_tick`;
- `threat_aggro_table`;
- `save_state_diff`;
- `dialogue_condition_eval`;
- `path_relaxation`;
- `behavior_tree_tick`;
- `cooldown_scheduler`.

Correctness hazards:

- a call between read and write mutates the parent table;
- one path aliases another path;
- dynamic key is not a string;
- intermediate value is nil or non-table;
- metatable lookup is required;
- `__index` returns a table that cannot be treated as raw stable storage;
- false and nil branch behavior must remain exact.

Checks:

```sh
go test -count=1 ./... -run 'Path|Dynamic|metatable|BenchmarksMatch'
go test -run '^$' -bench 'BenchmarkScenarioLuau/(economy_market_tick|threat_aggro_table|save_state_diff|dialogue_condition_eval|path_relaxation|behavior_tree_tick|cooldown_scheduler)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=3 ./...
```

Progress 2026-07-06: widened the private path-plan artifact from loop-local
reads to shared static and dynamic read/write path descriptors, plus
read-modify-write descriptors for the existing nested add/sub field-update
opcode. The verifier still owns stale-plan rejection, and disassembly now shows
the access kind, dynamic key source, value source, and fallback PC for these
planned path operations. A first runtime consumer now lets opt-in mechanism
counters and already-allocated path caches exercise static writes, dynamic
writes, and nested read-modify-write paths through the shared cache. Normal
runs do not allocate a new path cache solely because a path plan exists; a
benchmark probe showed that eager allocation regressed `economy_market_tick`,
`threat_aggro_table`, and `save_state_diff`.

Done when reads and writes share the same guarded parent path and side-exit
safely on mutation, aliasing, metatables, nil parents, or non-string keys.

Abandon or reorder if path hit rate is low, invalidation is constant, or guard
cost dominates saved lookup work.

## Phase 5: Typed Direct-Frame Superblock Families

The verifier and side-exit skeleton already exists by this point. This phase
fills it with broad block families after table metadata, fact lifetime, and
path plans have made the guards cheap and stable.

### Slice 5.1: Private Block Plan IR

Seam: compiler/finalizer produces execution artifact plans; the VM consumes
plans.

Interface: conceptually:

```text
blockPlanAt(pc) -> nil or typed block plan
executeBlock(frame, thread, plan) -> next pc or side exit
```

The interface should use typed plan enums, not stringly plan kinds, and never
Scenario-row names.

Implementation hidden:

- multiple bytecodes collapsed into one verified region;
- guard ordering;
- side-exit register repair;
- PC recovery;
- instruction budget accounting;
- branch target selection;
- value-kind checks;
- numeric checks;
- table guard checks;
- fallback to ordinary direct-frame dispatch.

Progress 2026-07-06: added a private typed `blockPlanDesc` artifact derived
from existing direct-block plans. The finalized prototype now carries
`blockPlanAt(pc)` metadata plus a PC map, the verifier rejects stale block-plan
state, and disassembly emits `block_plan` lines with typed family names and
fallback PCs. Verified direct-block execution now routes through the typed block
executor switch instead of switching on string plan kinds in the VM. Normal
`Run` remains unchanged; verified block execution is still gated behind the
private instrumentation/PIC seam until a later family proves profitable.

### Slice 5.2: First Global Block Families

Start with these families only:

1. dynamic path read plus arithmetic plus write;
2. predicate chain plus branch;
3. row or path field read plus numeric compare plus store;
4. numeric reduction;
5. intrinsic-guarded arithmetic or table operation.

Leverage rows:

- `economy_market_tick`;
- `cooldown_scheduler`;
- `path_relaxation`;
- `quest_progress_update`;
- `formation_layout_score`;
- `procgen_room_scoring`;
- `ai_utility_scoring`;
- `projectile_sweep`;
- `ability_resolution`;
- `combat_tick`.

Correctness hazards:

- side exits must restore exact PC and register state;
- branch behavior must match Luau exactly;
- NaN and numeric coercion behavior must stay correct;
- nil and false must remain distinct;
- table guards must not skip metamethod behavior;
- calls, yields, and debug hooks must reject or break blocks;
- instruction limits must remain observable;
- errors must point to the correct execution state;
- open returns and varargs must stay out of unsafe blocks.

Progress 2026-07-06: added the first typed Phase 5.2 family,
`dynamic_path_add_store`, for the repeated dynamic two-segment table update
shape seen in `economy_market_tick`: dynamic path read, numeric add/sub, and
write back to the same dynamic path. It is executed from the
`GET_STRING_FIELD_INDEX` opcode case rather than a global pre-dispatch probe,
so unrelated scalar dispatch does not pay a block-plan lookup. The family
side-exits before mutation for non-table parents, non-string keys, metatables,
missing values, or non-number operands. Attribution shows the family removes
the paired dynamic write dispatches and part of the arithmetic dispatch in
`economy_market_tick`; benchmark samples were roughly flat but allocation
counts stayed stable.

Progress 2026-07-06: fixed the first Phase 5.2 hot-path regression before
adding another block family. Normal `Run` is still protected from verified-plan
pre-dispatch lookup by the private PIC/instrumentation gate, but the dynamic
path block had introduced a `blockPlanAt` method call on every
`GET_STRING_FIELD_INDEX`. That lookup is now flattened to local PC-map arrays
inside `runDirectFrame`. The table epoch split also made string-only cache
tokens wider than necessary; dynamic index caches, runtime path caches,
table-field call caches, and intrinsic guards now use a smaller private
`tableStringShapeToken`, while the full `tableShapeToken` remains for broad
epoch tests. Focused guards moved `economy_market_tick` from about
`366891 ns/op` to `324541 ns/op`, `save_state_diff` from about `227078 ns/op`
to `203850 ns/op`, and kept allocations stable; longer runs still show
`threat_aggro_table` and `path_relaxation` outliers, so short full sweeps remain
diagnostic rather than decisive.

Checks:

```sh
go test -count=1 ./... -run 'Direct|Block|SideExit|BenchmarksMatch'
go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=3 ./...
scripts/scenario-ratio-gate < /tmp/scenario.out
```

Done when direct-frame dispatch count drops materially, side-exit rate stays
low, and no block family exists only for one Scenario row.

Abandon a block family if it fires narrowly, side-exits too often, or adds
guard cost equal to the dispatch it removes.

## Phase 6: Fixed Script-Call Frame Lifetime

This phase is listed after the first superblock families, but it should move
earlier if mechanism counters show event-style rows are dominated by fixed
script-call frame lifetime.

### Slice 6.1: Fixed Call Plan Module

Seam: execution artifact to VM call adapter.

Interface: a private fixed call plan contains:

- callee source;
- expected closure or proto identity guard;
- arg count;
- result count;
- yield and debug safety;
- reusable-frame eligibility;
- borrowed arg and result windows.

Implementation hidden:

- frame reuse;
- argument copying;
- register clearing;
- result placement;
- recursion and reentrancy safety;
- fallback materialization;
- hooks, yields, protected calls, and error unwinding.

Leverage rows:

- `event_dispatch`;
- `behavior_tree_tick`;
- method-call microbenchmarks;
- recursive and fixed-call benchmarks.

Correctness hazards:

- recursion corrupting reused state;
- yielded calls;
- debug hooks;
- protected calls;
- captured locals and upvalues;
- varargs;
- open result counts;
- host functions retaining arguments;
- coroutine state.

Checks:

```sh
go test -count=1 ./... -run 'Call|Frame|Yield|Hook|Protected|BenchmarksMatch'
go test -run '^$' -bench 'BenchmarkScenarioLuau/(event_dispatch|behavior_tree_tick)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=3 ./...
```

Done when call-heavy rows show fewer frame materializations and copies while
recursive, yield, debug, protected-call, and error cases remain exact.

Reorder earlier if counters show `event_dispatch` remains one of the worst rows
and fixed-call lifetime dominates.

## Phase 7: Intrinsic And Global Guard Hoisting

### Slice 7.1: Intrinsic/Global Guard Adapter

Seam: execution artifact and VM intrinsic/global adapter.

Interface: one guard plan for global name, library identity, version, native
identity, result behavior, and fallback pc.

Implementation hidden:

- `math.min`, `table.remove`, `rawlen`, and similar intrinsic identity checks;
- globals replacement;
- `RunWithGlobals`;
- library table mutation;
- fallback call behavior;
- arity and result adjustment;
- error behavior.

Leverage rows:

- `combat_tick`;
- `ability_resolution`;
- `quest_progress_update`;
- `buff_stack_tick`;
- `economy_market_tick`.

Checks:

```sh
go test -count=1 ./... -run 'Intrinsic|Global|RunWithGlobals|BenchmarksMatch'
go test -run '^$' -bench 'BenchmarkScenarioLuau/(combat_tick|ability_resolution|quest_progress_update|buff_stack_tick|economy_market_tick)/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=3 ./...
```

Done when verified blocks prove intrinsic safety once per region or loop instead
of every bytecode lap.

Keep this secondary unless counters show intrinsic guards remain a major hot
mechanism after superblocks.

## Execution Order

0. Finish mechanism counters enough to make decisions.
1. Build the verifier and side-exit skeleton early.
2. Fix table epoch locality and slot descriptors, metadata-first.
3. Add fact-lifetime and loop-local reuse rules.
4. Add composed dynamic path read, write, and read-modify-write plans.
5. Add typed direct-frame superblock families on top of those facts.
6. Pull fixed-call and frame lifetime earlier if event-style counters dominate.
7. Hoist intrinsic and global guards inside verified regions.

The main sequencing point is that the superblock/verifier seam comes early, but
large block families wait until table and path facts are ready. That prevents
table/path work from being designed in a vacuum while also avoiding a premature
mega-block implementation.

## Acceptance Gates

Every slice should pass the smallest focused tests first, then the standard
project gates before being considered complete:

```sh
scripts/check-lane root
scripts/check-fast
scripts/check
```

Performance acceptance should use same-run Luau comparisons:

```sh
go test -run '^$' -bench 'BenchmarkScenarioLuau/.*/(ember_run|luau_cli_batch)$' -benchmem -benchtime=500ms -count=3 ./... | tee /tmp/scenario.out
scripts/scenario-ratio-gate < /tmp/scenario.out
```

The strategic target is every Scenario row at or below 2.0x. A slice should be
kept only if it improves global mechanisms or creates a deeper seam that later
slices can exploit.

## Rejection Rules

Reject any change that:

- names a Scenario row in implementation;
- recognizes a whole benchmark body;
- introduces a one-row-only opcode or helper;
- expands the public interface for a private performance concern;
- puts table storage truth into the VM switch;
- duplicates metamethod semantics in the direct-frame fast path;
- adds a shallow adapter with only one real implementation and no test leverage.

## What Should Get Ember Under 2x

The credible path is cumulative:

1. verifier and side-exit skeleton gives later facts a real execution seam;
2. table storage facts reduce lookup and invalidation cost;
3. fact lifetime and loop-local reuse keep guarded facts alive safely;
4. composed path caches remove repeated nested dynamic lookup;
5. superblocks amortize dispatch and guard checks;
6. fixed-call lifetime fixes event-style rows;
7. intrinsic hoisting trims remaining library guard overhead.

One slice alone probably will not be enough. The broad 3x to 4x Scenario ratios
mean Ember needs layered, global leverage rather than another isolated opcode
fix.
