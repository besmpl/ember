# Plan: Ambitious No-Codegen Performance

## Goal

Make Ember dramatically faster without native codegen, JIT, generated
interpreters, unsafe code, custom GC, or a wider public runtime interface.

The plan is ambitious on purpose: move hot decisions out of the VM loop,
specialize the verified interpreter artifact, and drive benchmark-shaped
runtime overhead down while preserving Luau-shaped behavior. The work should fit
in about 20 vertical slices, each with a behavior guard and a benchmark gate.

The external seam stays small:

```go
proto, err := ember.Compile(source)
results, err := ember.Run(proto)
```

`RunWithGlobals`, `Program`, and later embedding APIs may benefit from the same
private artifact, but the plan does not expose bytecode IR, VM frames, table
layout, register facts, or optimizer metadata as public contract.

## Non-Goals

- No native code generation.
- No JIT.
- No generated interpreter backend.
- No unsafe dispatch tricks.
- No custom allocator or custom GC.
- No exact Luau bytecode cloning.
- No public `Proto` metadata API.
- No Hearth-specific optimization in the root package.
- No optimization that changes observable runtime semantics.

## Current Baseline

The current tree is already past the seed stage. The performance campaign has
seeded:

- compact `ValueKind` tags with public kind names preserved;
- private `Proto` execution metadata;
- direct-register facts for no-capture prototypes;
- constant key and numeric constant side tables;
- string-field bytecodes and two-field chain bytecodes;
- fixed one-result call bytecodes for locals, upvalues, methods, and table
  fields;
- selected base-library fast paths for `select`, `rawlen`, `table.insert`,
  `table.remove`, `coroutine.status`, `coroutine.resume`, and `math.min`;
- inline small string-field table storage;
- table metatable/index caches;
- Top10, Classic, and Scenario Luau benchmark fixtures;
- allocation budget tests for Top10 Ember runs;
- `scripts/bench-summary` for compact local benchmark tables.

The remaining pressure is not one trick. It is the shape of the private
compile-to-run module: too much hot work still happens while executing rather
than while finalizing and verifying an artifact.

## Scope

In:

- private compile finalization and verifier work;
- private execution artifact facts and disassembly for tests;
- bytecode opcode variants when they simplify the VM hot path;
- VM frame, call, return, vararg, coroutine, table, global, and base-library
  fast paths;
- table storage and table-shape/version guards behind the public `Table`
  behavior;
- benchmark baselines, allocation budgets, and regression gates;
- source-to-result behavior tests through `Compile`, `Run`, and
  `RunWithGlobals`.

Out:

- public surface expansion for optimizer internals;
- native-code or generated-code backends;
- host-specific shortcuts that bypass normal override behavior;
- skipping metamethod, yield, protected-call, vararg, or value-list semantics;
- optimizer rewrites without disassembly and verifier coverage.

## Design

The deep module is the private verified execution artifact.

```text
source
  -> parse/bind/lower
  -> bytecode IR
  -> finalize and verify
  -> sealed Proto with execution facts
  -> VM
```

Finalization should decide. The VM should consume. That is the main design
rule.

The artifact can own facts such as:

- direct-register vs cell-aware frame shape;
- entry register initialization;
- constant number/key/string handles;
- fixed argument and result counts;
- known local/upvalue/script/native call targets;
- no-yield, may-yield, protected-call, or host-call boundaries;
- table literal capacity and shape hints;
- string-field slots and guarded field-chain slots;
- base-library identity guards;
- numeric-loop and branch operation shapes;
- coroutine resume/yield record shape.

The verifier is the trust seam. Any optimized opcode or artifact fact must be
verified before the VM relies on it.

## Quality Bar

- Behavior tests prove semantics through public execution surfaces.
- Benchmarks prove speed or allocation movement with named fixtures.
- Every fast path has a slow fallback unless the verifier proves fallback is
  impossible.
- Host override semantics are tested before base-global or library fast paths
  are accepted.
- Metamethod and yield restrictions are tested before table/call/coroutine fast
  paths are accepted.
- Disassembly or private debug output makes optimized shape inspectable in
  tests.
- A slice may keep a neutral performance result only if it removes complexity,
  improves verifier coverage, or protects a later optimization.

## Benchmark Gates

Primary suites:

- Top10: `arithmetic_for`, `while_branching`, `table_fields`, `array_ops`,
  `generic_iteration`, `closures_upvalues`, `method_calls`,
  `metatable_index`, `varargs_select`, `coroutine_yield`;
- Classic: `recursive_fibonacci`, `iterative_fibonacci`;
- Scenario: `combat_tick`, `inventory_value`, `event_dispatch`.

North-star target:

- reach 1.25x or better against same-machine `luau_cli_batch` for every row;
- push rows that reach 1.25x toward 1.00x without regressing already-fast rows;
- keep scalar loop, direct iteration, and other already-strong rows within the
  documented noise budget.

Local gates:

- no allocation regression beyond a slice-specific budget;
- no behavior regression in source-to-result compatibility tests;
- no optimized opcode without verifier and disassembly coverage;
- benchmark changes reported with `BENCHTIME`, `COUNT`, machine, and compared
  baseline.

## Phases

### Phase 1: Measurement And Artifact Trust

Make performance work repeatable and make optimized shapes observable.

Exit gate:

- benchmark and allocation gates are documented;
- verifier and disassembly cover every optimized artifact fact already in use;
- current fast paths have behavior guards for override, metamethod, yield, and
  value-list semantics.

### Phase 2: Frame And Dispatch Spine

Cut generic frame, register, call, and dispatch overhead for ordinary
non-yielding execution.

Exit gate:

- no-capture frames and ordinary fixed script calls avoid cell-aware and
  yield-aware machinery;
- recursive and method-heavy benchmarks move for the right reason, not by
  breaking calls, upvalues, or yields.

### Phase 3: Numeric And Branch Hot Paths

Make numeric loops and branch-heavy code pay for numeric Luau semantics once,
not at every instruction.

Exit gate:

- numeric arithmetic, comparisons, loop checks, and branch forms have verified
  fast variants;
- generic metamethod-capable operations remain correct fallback paths.

### Phase 4: Tables, Shapes, And Intrinsics

Make tables cheap when source shape is stable, while keeping Lua/Luau table
semantics exact where they are observable.

Exit gate:

- table field chains, array operations, table intrinsics, metatable-index
  lookups, and generic iteration have guarded fast paths;
- sparse arrays, nil holes, metatables, and host overrides remain covered.

### Phase 5: Calls, Varargs, And Coroutines

Make the expensive semantic cases compact: value-list adjustment, varargs,
method calls, closure calls, coroutine yield/resume, and protected call paths.

Exit gate:

- ordinary calls do not pay coroutine cost;
- coroutine calls do not copy avoidable state;
- varargs and multiple returns keep exact Luau result-list behavior.

### Phase 6: Scenario Sweep And Retirement

Drive the benchmark suite as a whole instead of overfitting Top10 rows.

Exit gate:

- Scenario rows are within the target band or have a documented blocker;
- every optimized path has a verifier check, disassembly check, behavior test,
  and benchmark row;
- this plan can be retired or replaced by a smaller maintenance plan.

## Slices

Each slice should follow this loop:

1. Add or tighten one behavior guard through `Compile`/`Run` or
   `RunWithGlobals`.
2. Add or tighten one artifact/verifier/disassembly assertion if the slice
   changes optimized shape.
3. Implement the smallest cohesive optimization.
4. Run focused tests and the relevant benchmark row.
5. Record the result and either keep, revise, or revert the optimization.

### Phase 1: Measurement And Artifact Trust

1. **Benchmark ledger**
   - Behavior guard: current benchmark fixtures still return expected values.
   - Change: add a checked-in benchmark ledger for baseline Top10, Classic,
     and Scenario `ember_run` ns/op, B/op, and allocs/op.
   - Progress: baseline rows are recorded in
     `docs/benchmarks/fast-execution-ledger.md` from
     `BENCHTIME=300ms COUNT=1 scripts/bench-summary`, including Go/OS context
     and initial watch rows for allocation/performance pressure.
   - Gate: `scripts/bench-summary` with explicit `BENCHTIME` and `COUNT`.

2. **Allocation budgets by row**
   - Behavior guard: benchmark result tests still prove scalar outputs.
   - Change: extend allocation budget coverage beyond the existing Top10 rows
     to Classic and Scenario rows that enter the optimization queue.
   - Progress: Classic `recursive_fibonacci` and `iterative_fibonacci` now
     have Ember run allocation budgets alongside existing Top10 and Scenario
     budget coverage.
   - Gate: `go test -count=1 ./... -run 'AllocationBudgets|BenchmarksMatch'`.

3. **Artifact verifier coverage**
   - Behavior guard: invalid optimized prototypes fail before execution.
   - Change: verify direct-register facts, constant side tables, specialized
     call descriptors, string-field descriptors, numeric descriptors, and
     intrinsic descriptors.
   - Progress: existing verifier coverage already guards optimized descriptor
     ranges, constant types, specialized call operands, and intrinsic register
     spans; this slice tightened the direct-register artifact fact so a proto
     cannot claim direct-register execution while carrying captured locals, and
     direct-frame dispatch now verifies that the proto is direct-register and
     contains only opcodes supported by the direct runner.
   - Gate: bytecode verifier tests plus `scripts/check-lane root`.

4. **Disassembly for optimized shape**
   - Behavior guard: source programs still run the same.
   - Change: expose private disassembly/debug strings for optimized opcode and
     artifact facts used by tests.
   - Progress: optimized opcode disassembly remains private and test-covered;
     `disassembleProtoFacts` now exposes direct-register mode, direct-frame
     dispatch mode, captured-local registers, constant table keys, and constant
     numbers for artifact-shape assertions without widening the public runtime
     interface.
   - Gate: focused tests assert shape without exporting internals.

### Phase 2: Frame And Dispatch Spine

5. **Direct-frame dispatch split**
   - Behavior guard: closures with captured locals and no-capture functions
     both preserve values.
   - Change: split direct-register frame execution from cell-aware execution so
     no-capture code avoids per-op captured-cell checks.
   - Progress: added a verified `directFrameDispatch` artifact fact and a
     scalar/control direct runner for no-hook, unbudgeted frames. Unsupported
     or semantic-heavy opcodes stay on the generic runner; runtime nonnumeric
     values fall back at the current instruction before state changes.
     `arithmetic_for` moved from the baseline 8964 ns/op to 7898 ns/op with
     unchanged 284 B/op and 6 allocs/op under the slice benchmark command.
     `method_calls` and `recursive_fibonacci` remain call-frame pressure rows
     for slices 7-8.
   - Gate: `recursive_fibonacci`, `method_calls`, `arithmetic_for`.

6. **Entry register initialization plan**
   - Behavior guard: uninitialized locals, parameters, and varargs still read
     as `nil` where Luau requires it.
   - Change: finalize exact entry nil ranges and avoid broad frame clearing.
   - Progress: the verifier now recomputes and checks `entryNilRegisters`, and
     `disassembleProtoFacts` exposes the entry nil plan. Reused-frame behavior
     is guarded for missing parameters, and the existing entry-nil dataflow
     artifact remains the VM reset mechanism instead of broad register
     clearing. Run allocation rows stayed flat in the slice benchmark command.
   - Gate: B/op and allocs/op for scalar loop and recursive call rows.

7. **Fixed script-call frame transition**
   - Behavior guard: recursive calls, nested calls, and multiple return callers
     keep result-list behavior.
   - Change: represent fixed script calls as frame transitions, not generic
     argument/result slice construction.
   - Progress: audited the fixed script-call paths. `CALL_ONE`, local/upvalue
     one-result calls, and fixed-result generic `CALL` already enter
     `runInlineScriptCallOneNoHook`/`runInlineScriptCall` and borrow caller
     register windows when no captured cells are present. A helper extraction
     was rejected because it made the hot fixed-call path slower; the current
     inline transition remains the better artifact shape for this slice.
   - Gate: `recursive_fibonacci`, `closures_upvalues`, `method_calls`.

8. **Call result application**
   - Behavior guard: one, two, many, zero, and open-result calls assign exactly
     the expected values.
   - Change: specialize one-result and two-result application while preserving
     generic value-list adjustment.
   - Progress: added focused coverage for fixed one/two direct-frame result
     destinations and nil padding. A direct-register switch inside the shared
     result-application helpers was benchmarked and rejected after it regressed
     `method_calls`; the existing inline call-site result writes remain the
     faster shape for now.
   - Gate: `method_calls`, `varargs_select`, fixed-call tests.

### Phase 3: Numeric And Branch Hot Paths

9. **Numeric loop descriptor**
   - Behavior guard: numeric `for` accepts numeric strings where runtime
     semantics require it and rejects invalid bounds correctly.
   - Change: finalize numeric loop descriptors for validated start, limit,
     step, and loop variable update.
   - Progress: `Proto` now carries verified numeric loop descriptors derived
     from `NUMERIC_FOR_CHECK` and its increment backedge. The verifier rejects
     stale descriptors and `disassembleProtoFacts` exposes the check registers,
     exit PC, and increment PC for optimized-shape assertions. VM execution is
     unchanged in this slice; the descriptor is groundwork for later numeric
     loop consumption.
   - Gate: `arithmetic_for`, `iterative_fibonacci`.

10. **Numeric expression superinstructions**
    - Behavior guard: arithmetic metamethod fallback and numeric string
      conversion remain correct.
    - Change: add verified interpreter superinstructions for repeated numeric
      forms such as add/mul/idiv/mod chains seen in hot loops.
    - Progress: existing `ADD_NUMERIC_MOD_K` verifier and disassembly coverage
      were audited, and behavior coverage now proves numeric-string loop bounds
      still execute correctly through the superinstruction shape. No new
      superinstruction was kept in this slice; later numeric work should build
      on measured repeated forms rather than broadening speculatively.
    - Gate: `arithmetic_for`, `combat_tick`.

11. **Direct branch forms**
    - Behavior guard: truthiness, `and`/`or`, comparisons, and branch side
      effects remain Luau-shaped.
    - Change: specialize common comparison-and-jump patterns without producing
      intermediate boolean registers when not observable.
    - Progress: added verified/disassembled `JUMP_IF_MOD_K_NOT_EQUAL_K` for
      local modulo-constant equality branches such as `i % 5 == 0`. The VM fast
      path handles numeric operands directly and keeps a semantic fallback
      through modulo/equality helpers for non-plain-number values. The
      `while_branching` row moved from 15588 ns/op baseline to 11851 ns/op with
      unchanged 284 B/op and 6 allocs/op.
    - Gate: `while_branching`, `combat_tick`.

12. **Number fact invalidation**
    - Behavior guard: any path that can call user code, metamethods, or host
      code invalidates unsafe numeric assumptions.
    - Change: make numeric descriptors explicit about invalidation points.
    - Progress: added coverage proving the direct modulo branch does not trust
      numeric facts when the runtime operand is nonnumeric. A table with `__mod`
      still calls the metamethod and then applies equality normally, so the
      optimized branch form has an explicit semantic fallback at the user-code
      boundary.
    - Gate: targeted metamethod and host-call regression tests.

### Phase 4: Tables, Shapes, And Intrinsics

13. **Table shape/version core**
    - Behavior guard: adding, deleting, and mutating fields changes table
      versions without changing public table behavior.
    - Change: centralize string-field and metatable version facts so optimized
      field reads can guard once and fall back cleanly.
    - Progress: existing `stringVersion` guards inline string-field slots and
      metatable `__index` table caches. Added coverage proving cached
      `__index` table lookups invalidate when the metatable's `__index` string
      field changes, preserving the version seam needed by later guarded field
      chain work.
    - Gate: `table_fields`, `metatable_index`, table mutation regressions.

14. **Guarded field-chain access**
    - Behavior guard: `a.b.c`, assignment through chains, and metatable
      fallback stay correct across mutation.
    - Change: finalize slot descriptors for stable two-field and row-field
      chains, including invalidation on table/metatable changes.
    - Progress: existing two-step field opcodes and row-field descriptors were
      kept. Added coverage proving `GET_STRING_FIELD2` observes replacement of
      the intermediate table instead of reusing a stale child, alongside the
      existing metatable fallback coverage for nested field updates.
    - Gate: `table_fields`, `inventory_value`, `metatable_index`.

15. **Array sequence storage**
    - Behavior guard: nil holes, sparse numeric keys, `rawlen`, `table.insert`,
      `table.remove`, and iteration preserve semantics.
    - Change: improve front removal and append-heavy arrays without breaking
      generic table keys.
    - Progress: existing fast-array storage already appends, inserts, removes,
      and advances front removal without shifting the whole array. Added a
      direct storage guard proving front remove plus append keeps sequence
      length and values coherent without spilling into hash fields.
    - Gate: `array_ops`, `inventory_value`, allocation budgets.

16. **Base-library intrinsic audit**
    - Behavior guard: `RunWithGlobals` can override every intrinsic target.
    - Change: make intrinsic selection a verified descriptor with explicit
      global/table identity guards, not scattered ad hoc checks.
    - Progress: `Proto` now carries verified intrinsic descriptors for
      optimized base-library opcodes (`TABLE_INSERT`, `TABLE_REMOVE`,
      `COROUTINE_RESUME`, `MATH_MIN`, and `SELECT_VARARG_COUNT`). Private
      artifact disassembly exposes those descriptors, and existing
      `RunWithGlobals` override tests cover the optimized targets.
    - Gate: `array_ops`, `varargs_select`, `coroutine_yield`, override tests.

### Phase 5: Calls, Varargs, And Coroutines

17. **Method and handler dispatch**
    - Behavior guard: method self arguments, table handler calls, and callable
      tables preserve exact call semantics.
    - Change: specialize lookup-plus-fixed-call patterns for `obj:method(...)`
      and `handlers[key](...)`.
    - Progress: existing `CALL_METHOD_ONE` and `CALL_TABLE_FIELD_KEY_ONE`
      opcodes were kept. Added coverage proving dynamic handler dispatch sees
      handler table mutation and does not cache a stale function for
      `handlers[event.kind](...)`.
    - Gate: `method_calls`, `event_dispatch`.

18. **Vararg windowing**
    - Behavior guard: `...`, `select`, final-call expansion, truncation, and
      nil-fill remain exact.
    - Change: represent common vararg reads as borrowed frame windows instead
      of allocated slices.
    - Progress: existing frame setup already borrows the vararg argument
      window. Added source-to-result coverage proving nil-fill and
      `select("#", ...)` count semantics remain exact when fixed locals read
      from a vararg window.
    - Gate: `varargs_select`, vararg source-to-result tests.

19. **Coroutine resume records**
    - Behavior guard: yielded values, resume arguments, dead coroutine errors,
      nested yields, protected-call yields, and non-yieldable runtime
      operations match current compatibility tests.
    - Change: store suspended state as compact resume records and keep ordinary
      non-yielding calls free of coroutine machinery.
    - Progress: audited the existing coroutine resume/yield path with focused
      coroutine compatibility tests. Current suspended frames already preserve
      resume arguments, host-yield continuations, protected-call yields, status
      transitions, and non-yieldable runtime behavior; no speculative record
      rewrite was kept in this MVP slice.
    - Gate: `coroutine_yield`, coroutine compatibility tests.

### Phase 6: Scenario Sweep And Retirement

20. **Scenario-driven final sweep**
    - Behavior guard: all Top10, Classic, and Scenario fixtures still match
      expected results.
    - Change: use profiles from `combat_tick`, `inventory_value`, and
      `event_dispatch` to remove remaining broad interpreter overhead without
      introducing new public seams.
    - Progress: benchmark result and allocation budget tests passed. The final
      compact summary records retained wins for `while_branching`
      (15588 -> 10690 ns/op) and `arithmetic_for` (8964 -> 7895 ns/op), with
      watched allocations stable. Scenario rows remain documented pressure for
      future work rather than forced speculative rewrites in this MVP.
    - Gate: every row has current benchmark data, known blockers, and either
      meets the target band or has a follow-up plan.

## Verification

Focused behavior check:

```sh
go test -count=1 ./... -run '<focused behavior pattern>'
```

Focused benchmark row:

```sh
go test -run '^$' -bench 'Benchmark(Top10Luau|ClassicLuau|ScenarioLuau)/<case>/ember_run$' -benchmem ./...
```

Compact benchmark table:

```sh
BENCHTIME=300ms COUNT=1 scripts/bench-summary
```

Phase completion:

```sh
scripts/check-lane root
scripts/check-fast
```

Plan retirement:

```sh
scripts/check
```

Documentation-only edits:

```sh
git diff --check
perl -ne 'print "$ARGV:$.:$_" if /[^\x00-\x7F]/' docs/exec-plans/fast-execution-artifact.md
awk '/[ \t]$/ { print FILENAME ":" FNR ": trailing whitespace" }' docs/exec-plans/fast-execution-artifact.md
```

## Risks

- **Compatibility:** fast paths can accidentally skip metamethods, host
  overrides, yield restrictions, or value-list adjustment. Every fast path
  needs behavior guards and a verified fallback.
- **Overfitting:** Top10 rows are useful but small. Scenario rows must decide
  the final shape so Ember does not optimize a toy suite at the expense of real
  scripts.
- **Shallow flags:** scattered VM booleans make performance work brittle. Keep
  selection in finalization and consumption in the VM.
- **Compile-time cost:** richer artifacts can slow compilation. Benchmark
  compile path when finalization or verification grows materially.
- **Debuggability:** optimized shape needs disassembly and verifier errors that
  explain what the artifact claimed.
- **State lifetime:** frame pools, borrowed vararg windows, table slots, and
  coroutine records must have obvious ownership and scrubbing.

## Retirement Criteria

Retire this plan when the no-codegen interpreter lane has:

- behavior coverage for every optimized path;
- verifier and disassembly coverage for every artifact fact;
- benchmark data for every Top10, Classic, and Scenario row;
- allocation budgets for rows in the active watch zone;
- no public interface expansion for optimizer internals;
- documented blockers for any row that cannot reach the target band without
  violating the non-goals.
