# ADR 0005: Retain the Canonical Direct Execution Path

## Context

F1 and F2 added private forced execution modes for the general slot engine,
numeric slot engine, compact call program, and direct loop kernel. F3 requires
that each tier earn retention on the complete workload shape rather than on a
single favorable microbenchmark. The public `Value` representation and the
canonical wordcode VM are already the compatibility boundary; the decision is
whether any private alternate should remain production machinery.

The checked-in internal harness is
`f3_execution_retention_benchmark_test.go`. It compiles representative Top10
arithmetic, recursive-call, and fixed-call fixtures plus the canonical 25-row
Scenario corpus. The Scenario rows are loaded in the test by parsing
`top10_luau_benchmark_test.go`, so the evidence cannot drift from the existing
compatibility cases. The harness measures forced `direct`, `direct_no_kernel`,
`slot`, `numeric_slot`, and `compact_call` paths where those paths are
meaningful. `TestF3ExecutionPathCoverage` records representative eligibility;
`TestF3ScenarioPathCoverage` records full-corpus parity, eligibility, and
dynamic direct-loop-kernel coverage without making the decision depend on one
machine's timing noise.

## Evidence

Reproducible checked-in harness commands:

```text
go test -run '^TestF3ExecutionPathCoverage$' -count=1 -v .
go test -run '^TestF3ScenarioPathCoverage$' -count=1 -v .
go test -run '^$' -bench '^BenchmarkF3ExecutionPaths/' -benchmem -benchtime=200ms -count=5 .
go test -run '^$' -bench '^BenchmarkF3ScenarioPaths/' -benchmem -benchtime=200ms -count=5 .
```

The full-corpus coverage run currently reports 25 rows, 0 alternate-eligible
rows, 8 direct-loop-kernel target rows, and 24.12% dynamic kernel instructions
(106137 of 439956 direct instructions, including cold instructions).

A short M1 decision run used `-benchtime=100ms -count=3`; each row below is the
median of three samples and each family ratio is the equal-weight geometric
mean of alternate/direct ratios:

| engine | eligible family | forced ratio | allocation result |
| --- | --- | ---: | --- |
| general slot | arithmetic, while branching, iterative Fibonacci | 0.620x | unchanged: 16 B/op, 1 alloc/op |
| numeric slot | same three scalar rows | 0.494x | unchanged: 16 B/op, 1 alloc/op |
| compact call | recursive Fibonacci and fixed call chain | 0.279x | 232/160 B and 4/2 allocs direct; 16 B and 1 alloc compact |
| direct loop kernel | eight Scenario target rows | 1.223x | unchanged per row |

The general and numeric engines clear their isolated >=20% speed gate, and
compact clears its isolated >=10% call-family gate. They still execute 0% of
dynamic Scenario instructions because all 25 rows are ineligible. Full
Scenario `auto/direct` measured a 0.991x full-corpus geometric mean, which is
neutral timing noise around
the same direct engine rather than alternate-engine coverage. The kernel slows
its eight-row target family by 22.3%; treating the other seventeen rows as
unchanged gives a 1.067x full-Scenario geomean regression. Its eight median
ratios ranged from 1.001x to 1.385x and none supplied an allocation reduction.

The full Scenario benchmark includes `auto` and `direct` for every row and
`direct_no_kernel` only for the eight rows that compile a kernel target. Rerun
the longer commands above on a target machine before using the numbers as a
general performance forecast; the retention decision depends primarily on
the exact coverage failures and the kernel's consistent target-family loss.

The per-engine CPU and GC evidence used these commands:

```text
go test -run '^$' -bench '^BenchmarkF3ExecutionPaths/top10_arithmetic_for/(direct|slot|numeric_slot)$' -benchtime=1s -count=1 -cpuprofile=/tmp/ember-f3-slots.cpu.pprof .
go tool pprof -top /tmp/ember-f3-slots.cpu.pprof
go test -run '^$' -bench '^BenchmarkF3ExecutionPaths/top10_recursive_fibonacci/(direct|compact_call)$' -benchtime=1s -count=1 -cpuprofile=/tmp/ember-f3.cpu.pprof -memprofile=/tmp/ember-f3.mem.pprof .
go tool pprof -top /tmp/ember-f3.cpu.pprof
go test -run '^$' -bench '^BenchmarkF3ScenarioPaths/inventory_value/(direct|direct_no_kernel)$' -benchtime=1s -count=1 -cpuprofile=/tmp/ember-f3-kernel.cpu.pprof .
go tool pprof -top /tmp/ember-f3-kernel.cpu.pprof
GODEBUG=gctrace=1 go test -run '^$' -bench '^BenchmarkF3ExecutionPaths/top10_recursive_fibonacci/(direct|compact_call)$' -benchtime=1s -count=1 .
```

The profiles attribute the measured CPU to the selected dispatch loops: direct,
general slot, and numeric slot lead the scalar profile; compact and direct lead
the call profile; and the kernel plus direct loop lead the kernel profile. Slot
and numeric keep the same 16 B/op allocation shape as direct, while the kernel
keeps the same per-row B/op and allocation count as no-kernel direct, so neither
offers a GC reduction. Compact does reduce allocations, but only for its
Scenario-ineligible call graph. The GC trace recorded no workload collection
during the timed call runs, only the benchmark harness's forced collection
between sub-benchmarks.

Maintenance is also a decision input: `slot_execution.go`,
`compact_execution.go`, and `direct_loop_kernel.go` total 3,828 lines and 190
opcode `case` bodies, versus 138 `case` bodies across the two canonical direct
loops. Those alternate semantic families and kernel-specific state must stay
aligned with cancellation, limits, error frames, and coroutine boundaries.

The internal benchmark also records the expected shape of the trade-off:
alternate paths can win isolated scalar or call-heavy cases, while the
canonical direct path remains the only path covering arbitrary tables,
metatables, globals, callbacks, protected calls, coroutine state, errors, and
limits without a boundary fallback. F2's differential corpus, race lane, and
checkptr lane are the semantic and safety gates for all of these paths.

Parity and safety commands used for the gate, all passing, were:

```text
go test -run '^TestExecutionDifferentialCorpus$' -count=1 .
go test -race -run '^TestExecutionDifferentialCorpus$' -count=1 .
go test -gcflags=all=-d=checkptr=2 -run '^TestExecutionDifferentialCorpus$' -count=1 .
```

## Decision

Retain the canonical direct wordcode VM as Ember's only production execution
engine. Delete the general slot engine, numeric slot engine, compact call
program, and direct loop kernel experiments. Keep the forced-mode and
differential tests until deletion is complete so that the migration proves the
same source-level behavior before removing the alternate implementations.

The canonical semantic representation is the existing immutable wordcode plus
the established `Value`/VM frame state. No new slot ABI, compact continuation
format, or kernel-specific opcode semantics may become a second source of
truth.

## Migration and deletion order

1. Land F3 evidence and this ADR.
2. Remove automatic selection and forced entry points for slot, numeric, and
   compact execution; keep direct differential coverage green during the edit.
3. Remove direct-loop-kernel construction, dispatch, counters, and tests.
4. Remove the remaining alternate-engine source, path counters, and temporary
   benchmark-only compatibility shims.
5. Re-run source/result compatibility, cancellation, every runtime limit,
   error-stack, race, checkptr, allocation, and benchmark gates.

No public API is added by this decision. The private alternate engines are
deleted because their broad-workload coverage and full-corpus gates fail, not
because their microbenchmarks lack engineering value.

## Consequences

The runtime has one semantic execution path, reducing duplicated opcode
families, special state, and fallback behavior. Some scalar and recursive-call
microbenchmarks will lose their isolated speedups, but production behavior is
easier to profile, maintain, and make safe. Future optimization must improve
the canonical direct path or present a new complete-corpus measurement before
introducing another execution tier.

## Alternatives considered

- **Retain all tiers because individual microbenchmarks win.** Rejected: the
  plan requires full Scenario coverage and geomean gates.
- **Retain slot/numeric only.** Rejected: their eligible set is scalar and
  narrow; neither reaches the Scenario coverage gate.
- **Retain compact calls only.** Rejected: the recursive-call win is real but
  isolated and does not affect the full workload family.
- **Retain the loop kernel.** Rejected: its targeted Scenario family regressed
  and the kernel duplicates broad opcode semantics.
