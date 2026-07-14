# ADR 0006: Stop the Table Allocator Campaign at the Persistent Gate

## Context

Phase G requires a fresh profile after the VM consolidation work. Immutable
table-literal templates and runtime-owned table slabs are justified only when
table construction or storage remains a top-three CPU or allocation source in
the persistent workloads that matter to Hearth.

The profile was captured at commit `2146770a` on Darwin arm64 with Go 1.26.4.
The repeatable audit used five 200 ms samples and CPU/allocation profiles:

```sh
BENCHTIME=200ms scripts/performance-audit \
  --output /tmp/ember-g1-audit-20260714 --profiles --allow-dirty

go test -run '^$' \
  -bench '^BenchmarkRuntimeLane(StatelessRun|PersistentRuntimeRunHook)/(nested_table_mutation|dynamic_string_growth)$' \
  -benchmem -benchtime=500ms -count=5 .

go test -run '^$' -bench '^BenchmarkRuntimeLaneHostBoundary/' \
  -benchmem -benchtime=500ms -count=5 .

go test -run '^$' -bench '^BenchmarkRuntimeLaneRetainedResult$' \
  -benchmem -benchtime=500ms -count=5 .
```

Stateless dynamic string growth still allocates heavily in table storage
(about 166 KiB and 196 allocations per operation), and the retained-result
lane is likewise dominated by fresh table storage. Those lanes construct a new
object graph on every invocation.

The persistent runtime lanes tell a different story. Their module load and
startup occur before timing, then repeated `update` hooks mutate the same
script-owned state:

| Persistent workload | Median ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| nested table mutation | 1,736 | 1,000 | 13 |
| dynamic string growth | 13,795 | 1,049 | 14 |

Their allocation profiles contain no table allocator among the leading
symbols. The dominant sources are runtime global-environment construction,
execution-controller creation, call-context plumbing, and `Runtime.RunHook`
itself. Individual table/hash/string helpers contribute low-single-digit CPU
percentages in the dynamic workload, but table construction and storage are
not a top-three persistent pressure. Host callback profiles are similarly led
by globals, argument/result windows, and error/context handling.

## Decision

Stop Phase G after G1. Do not add table-literal templates, an owner-safe table
slab, `MaxRuntimeBytes`, or an A/B allocator branch. Slices G2 through G4 are
conditional and their persistent-workload prerequisite is not satisfied.

Keep `BenchmarkRuntimeLaneRetainedResult` so future profiles cover a host that
retains a returned table graph while later stateless calls execute.

The next measured allocation pressure is the persistent `RunHook` boundary:
global environments, controllers, contexts, argument/result windows, and
related setup. Any future optimization proposal should start there with a
fresh profile rather than reviving the table allocator from stateless data.

## Consequences

- Ember keeps ordinary Go table allocation and the existing runtime ownership
  model.
- No dormant allocator flags, migration adapters, or alternate table paths are
  introduced.
- Stateless table-heavy workloads remain a known cost, but they do not justify
  complexity that fails to improve persistent Hearth updates.
- A future table-allocation slice must re-run this gate and show a material
  persistent benefit before changing the decision.

## Alternatives Considered

- **Add immutable literal templates for stateless wins.** Rejected because the
  required persistent gate is false; persistent startup is outside the timed
  update loop.
- **Prototype table slabs and exact byte accounting anyway.** Rejected because
  G3 requires G1 to prove persistent table allocation dominance and demands a
  material Hearth gain.
- **Use retained stateless results as the persistent proof.** Rejected because
  retaining one result proves escape safety pressure, not repeated mutation on
  a loaded Hearth runtime.
