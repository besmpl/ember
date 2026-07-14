# Performance Profile Audit

## Snapshot and method

This audit records current benchmark weak points on 2026-07-14 at commit
`fd0aa29a7d3cf3c22ea1fc74ac11305109c81681`, using Go 1.26.4 on
darwin/arm64 and an Apple M1. The performance-audit source fingerprint was
`0efae3eeafd85be1758d950e7b97c92be714a171368c645c5c63108de97132d7`.

The worktree contained unrelated user changes. In particular, the untracked
`compatibility_manifest_test.go` imported `go/parser` as `parser`, which
conflicted with Ember's package-level `parser` type and prevented `go test`
from compiling. Profiles were therefore captured from a temporary copy that
excluded only that test file. Production Go files and benchmark files were
otherwise identical to the worktree.

The repeatable runner supplied five 500 ms samples and aggregate CPU and
allocation profiles. The three most important paths were then captured again
with clean, focused 5 second profiles:

- recursive Fibonacci;
- sparse-grid neighbors;
- the 256 KiB compiler `emit` and `compile` stages.

Wall-clock comparisons between unlike scripts are descriptive, not normalized
for work performed. CPU flat percentages name work done in a function;
cumulative percentages include callees and overlap with their callers.
Allocation profiles measure allocation over the profile lifetime, not retained
heap.

## Executive findings

The strongest current weak points are:

1. Recursive script call and return handling. The focused Fibonacci profile
   puts 29.80% flat CPU in the generated VM loop, 12.60% in
   `enterRecordOnlyFixedCall`, and 6.60% in
   `resumeRecordOnlyFixedCallOne`. The enter/resume paths are 26.20% and
   22.60% cumulative respectively.
2. Per-instruction VM mechanics. Across the complete Scenario family,
   `runGeneratedDirectFrameProductionLoop` owns 39.41% flat and 79.27%
   cumulative CPU, `valueKind` owns 9.57% flat, and
   `executionWindow.stepInstruction` owns 4.56% flat.
3. Persistent `Runtime.RunHook` setup. Persistent rows pay a stable 1,000 B and
   13 allocations per operation. Aggregate runtime allocation is led by global
   environment construction, the `require` binding, execution-controller
   creation, and context wrapping.
4. Stateless table construction. `newTableWithCapacity` accounts for 91.92%
   cumulative Scenario allocation space. This remains a large stateless cost,
   but the persistent table-allocation gate in ADR 0006 is still false.
5. Dynamic string-table growth. Sparse-grid allocation is led by
   `tableHashFields.grow` (30.01%), table storage (26.20%), iteration-journal
   slice growth (22.09%), and `Table.buildIterationIndex` (11.05%).
6. Repeated syntax-tree walks during emission. In the clean 256 KiB
   emit/compile profile, `canCompileSingleLocalAssignmentInPlace` is 27.41%
   cumulative CPU and `expressionCanAssignToNameInPlace` is 19.50%. The many
   arena accessor functions at the top of the flat profile show that the same
   small expression shape is being inspected repeatedly.

The previous audit's compact-call engine and direct-loop-kernel findings are
not current. ADR 0005 deliberately removed those alternate engines. The
parser's old tiny-object churn is also no longer a leading allocation-count
problem: typed arenas reduced the 256 KiB parse stage to 67 allocations per
operation.

## Slow benchmark rows

The clean five-sample Scenario medians below identify the longest rows in that
family. They do not prove that one row is less efficient than another.

| Scenario row | Median ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `sparse_grid_neighbors` | 2,433,663 | 39,174 | 71 |
| `formation_layout_score` | 511,207 | 4,512 | 25 |
| `economy_market_tick` | 398,403 | 5,921 | 37 |
| `threat_aggro_table` | 304,958 | 5,969 | 37 |
| `component_churn` | 257,041 | 6,241 | 34 |
| `ai_utility_scoring` | 220,870 | 3,840 | 23 |
| `save_state_diff` | 205,487 | 5,009 | 31 |
| `prototype_fallback` | 194,529 | 2,888 | 24 |

The focused 5 second rows measured recursive Fibonacci at 3,300,452 ns/op and
sparse-grid neighbors at 2,777,091 ns/op. Fibonacci allocated only 269 B and 4
objects per operation, so it is a CPU/call-machinery problem rather than an
allocation problem.

The clean focused 256 KiB compiler rows measured:

| Stage | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `emit` | 19,803,827 | 992,998 | 13 |
| `compile` | 43,856,921 | 21,843,539 | 191 |

The full five-sample compiler-stage run was partially overlapped by unrelated
workspace checks, so its wall times are only indicative. Its stable allocation
shape still agrees with the focused capture: parse uses about 14.5 MB and 67
allocations, optimize about 6.0 MB and 82 allocations, and full compile about
21.8 MB and 191 allocations.

## Profile findings and why they are slow

### 1. Recursive calls repeatedly rebuild and clean frame state

The clean recursive Fibonacci CPU profile contained about 5 seconds of CPU
samples:

| Function | Flat | Cumulative |
| --- | ---: | ---: |
| `runGeneratedDirectFrameProductionLoop` | 29.80% | 93.60% |
| `vmThread.enterRecordOnlyFixedCall` | 12.60% | 26.20% |
| `vmThread.resumeRecordOnlyFixedCallOne` | 6.60% | 22.60% |
| `vmThread.maybeEnterRecordOnlyFixedCall` | 3.20% | 29.40% |
| `vmFrame.resetFrameIntoRegistersWithVarargSource` | 3.40% | 6.80% |

Why this is slow:

- `fib(20)` crosses about 21,890 script call edges. Small fixed costs are
  multiplied by every edge.
- `enterRecordOnlyFixedCall` rechecks call eligibility, destination bounds,
  register ownership, cell ranges, encoded record widths, stack capacity, and
  call limits before rebinding the physical frame to the callee.
- `resumeRecordOnlyFixedCallOne` decodes the saved record, finds the owner
  range, closes upvalues, clears pointer-bearing child registers, restores the
  caller's closure/register/cell/cold state, and writes the result.
- The path is allocation-light because it reuses the shared stack and compact
  frame records. The remaining cost is validation, state mutation, and memory
  clearing rather than heap churn.

Useful experiments:

1. Precompute immutable fixed-call facts in the `Proto` or call-site sidecar so
   the hot path validates only state that can actually vary at runtime.
2. Split the proven one-result, non-variadic, no-cell return case from the
   generic restore path while retaining one canonical VM semantic path.
3. Use compiler liveness to narrow the register range that must be cleared, or
   prove which slots are immediately overwritten before skipping a clear.

Proof signal: the combined cumulative share of enter/resume falls materially,
Fibonacci ns/op falls, and call-limit, upvalue, error-stack, coroutine, race,
and checkptr tests remain green. Do not reintroduce the deleted compact engine
as a benchmark-only second VM.

### 2. Dispatch, safety polling, and value classification tax every instruction

The clean aggregate Scenario CPU profile contained 13.17 seconds of samples:

| Function | Flat | Cumulative |
| --- | ---: | ---: |
| `runGeneratedDirectFrameProductionLoop` | 39.41% | 79.27% |
| `valueKind` | 9.57% | 9.57% |
| `executionWindow.stepInstruction` | 4.56% | 4.63% |
| `propertyIC.get` | 2.05% | 3.42% |
| `wordcodeCacheIndex.cacheSiteAt` | 1.44% | 2.73% |
| `wordcodeCacheIndex.cacheIDAt` | 1.29% | 1.29% |

Why this is slow:

- Every wordcode instruction performs the loop bound check, calls
  `stepInstruction`, fetches and decodes a word, and branches through the large
  opcode switch.
- `stepInstruction` checks a controller pointer, maintains the 256-instruction
  context-poll countdown, and conditionally maintains the instruction budget.
  The common unlimited, non-cancelled path still pays the helper call and
  branches.
- `Value` is a 16-byte pointer-bearing public representation. Numbers and
  booleans use GC-visible pointer sentinels, so `valueKind` compares the
  reference against scalar sentinels before reading tag bits. Typed opcode
  guards repeat that classification throughout the loop.
- Cacheable property operations load `cacheID+1` from the immutable direct
  per-word sidecar, then use that ID to load the descriptor. This is the D8
  winner: the retired rank/popcount index saved memory but lost the measured
  cache-heavy gate. On darwin-arm64 the 128-word/128-site reference fixture is
  1,592 bytes, 460 bytes (+40.6%) above the retired rank estimate.

Useful experiments, in increasing blast radius:

1. Inline the common execution-window countdown and move context/error work to
   a cold helper. Measure bounded and cancelable runtime rows separately.
2. Cache or encode only the immutable type facts already proved by the
   compiler so numeric/table opcode bodies do not repeat `valueKind` calls.
3. Measure whether `cacheIDAt` and `cacheSiteAt` can be collapsed or inlined at
   generated property opcodes without changing the direct sidecar's validated
   layout or increasing its recorded memory cost.

Proof signal: `stepInstruction`, `valueKind`, and cache-index flat samples fall
across the full Scenario family, not only scalar microbenchmarks. Wordcode and
side-table size gates must be reported with any timing win.

### 3. Persistent hooks rebuild per-call boundary objects

The runtime-mode allocation profile attributes allocation space as follows:

| Function | Flat | Cumulative |
| --- | ---: | ---: |
| `globalEnv.set` | 25.04% | 28.92% |
| `runtimeGlobals` | 13.19% | 13.19% |
| `newExecutionController` | 10.57% | 10.57% |
| `contextWithRuntimeCallContext` | 7.21% | 11.34% |
| `Runtime.RunHook` | 7.18% | 84.30% |
| `runtimeCallContext.envWithRequire` | 5.98% | 43.42% |
| `context.WithValue` | 4.13% | 4.13% |

All persistent scalar, recursive, and nested-table rows measured exactly
1,000 B/op and 13 allocs/op. Persistent dynamic string growth measured 1,049
B/op and 14 allocs/op. This fixed floor is more important to Hearth-style
updates than stateless table allocation.

Why this is slow:

- Every hook creates a fresh execution controller even when limits are
  unlimited and the context is not cancelled.
- `envWithRequire` creates a fresh `globalEnv`, lazily allocates its values map,
  creates a bound native `require` value, and inserts it into the map.
- The internal call context is copied into a new `context.WithValue` node.
- Some data is genuinely invocation-specific: context, host globals, module
  origin, and controller. The current representation combines it with runtime
  invariants, so invariants are allocated again on every call.

Useful experiments:

1. Separate an immutable runtime/entrypoint global template from the small
   invocation-specific capability carrying context, host overrides, and the
   controller.
2. Pass `runtimeCallContext` explicitly through the private call seam instead
   of storing it in `context.Context`, if callbacks and module loading can keep
   the same public context behavior.
3. Reuse/reset an execution controller only after proving cancellation,
   instruction counts, object counts, inherited frames, and errors cannot leak
   between hooks.

Proof signal: persistent rows drop below 13 allocs/op and 1,000 B/op while
host-global freshness, `require` origin, cancellation, limits, callbacks, and
concurrent-runtime ownership tests retain their behavior.

### 4. Stateless tables dominate allocation but not persistent updates

The clean aggregate Scenario allocation-space profile contains:

| Function | Flat | Cumulative |
| --- | ---: | ---: |
| `newTableStorage` | 54.79% | 54.79% |
| `newTableWithCapacity` | 31.82% | 91.92% |
| `newTableArrayStorage` | 5.30% | 5.30% |

`runtime.madvise` also appears at 13.29% flat CPU in the Scenario profile,
consistent with allocator/page pressure being visible beyond the table
constructors themselves.

Why this is slow:

- Each stateless `ember_run` recreates every table literal and any nested table
  graph. A table may allocate the table object, array storage, string/hash
  storage, cold data, and iteration metadata.
- Returned tables and their children can escape to Go. Reusing them blindly
  would violate identity, retained-result, and mutation behavior.
- Persistent `RunHook` workloads mutate already-loaded tables and do not show
  table construction among their top three allocation sources. ADR 0006
  therefore correctly stopped the slab/template campaign at its persistent
  gate.

The current priority is to reduce the `RunHook` boundary floor first. Revisit
literal templates or owner-safe slabs only if a fresh persistent profile makes
table construction a top-three source. Any allocator experiment must include
retained-result and identity tests.

### 5. Sparse-grid tables duplicate growth and deterministic-order bookkeeping

The clean focused sparse-grid profile measured 2,777,091 ns/op, 39,205 B/op,
and 71 allocs/op. Its important entries were:

| Function | Profile | Flat | Cumulative |
| --- | --- | ---: | ---: |
| `tableHashFields.grow` | allocation space | 30.01% | 30.01% |
| `newTableStorage` | allocation space | 26.20% | 26.20% |
| iteration-key `slices.Grow` | allocation space | 22.09% | 22.09% |
| `Table.buildIterationIndex` | allocation space | 11.05% | 11.05% |
| `tableHashFields.find` | CPU | 1.99% | 5.42% |
| `Table.rawStringFieldSlot` | CPU | 1.08% | 6.86% |

Why this is slow:

- The benchmark repeatedly constructs `tostring(x) .. ":" .. tostring(y)`,
  performs dynamic lookups, and inserts previously absent neighbor keys.
- The open-address hash grows geometrically and rehashes live entries.
- Ember also preserves deterministic insertion order. Once the journal grows,
  it owns a second key slice and an index map, so one logical insert updates
  both lookup storage and iteration storage.
- Overflow-first string probing and signed small-integer formatting are already
  implemented. They should not be proposed again as new work.

Useful experiments:

1. Store an iteration position in hash entries, or otherwise share indexing
   between hash lookup and deterministic order, to avoid a second map.
2. Re-evaluate journal capacity at the exact hash-growth boundary; the current
   reservation helper still leaves `slices.Grow` as 22.09% of allocation space.
3. Test a larger first overflow capacity only on tables that have demonstrated
   continued dynamic insertion, and measure the memory regression on small
   tables.

Proof signal: sparse-grid B/op and allocs/op fall with lower hash-grow,
iteration-grow, and index-build profile shares. Table iteration identity,
deletion/reinsertion order, equal-content string keys, and small-table memory
tests are required gates.

### 6. Compiler emission repeatedly asks the syntax tree the same question

The clean focused compiler profile combines `emit` and full `compile`, each
run for 5 seconds. Important CPU entries were:

| Function | Flat | Cumulative |
| --- | ---: | ---: |
| `compiler.compileAssignment` | - | 66.22% |
| `compiler.canCompileSingleLocalAssignmentInPlace` | 0.96% | 27.41% |
| `expressionCanAssignToNameInPlace` | - | 19.50% |
| `syntaxTree.arenaTerm` | 6.50% | 10.07% |
| `syntaxTree.termName` | 3.25% | 5.48% |
| `syntaxTree.termChild` | 2.49% | 4.02% |
| `syntaxTree.expressionTerms` | 2.17% | 4.02% |

`-` means the function's own flat share was not material in the top listing;
the cumulative share is the relevant signal for its subtree.

Why this is slow:

- The 256 KiB fixture repeats `value = value + 1`, producing 14,565 emitted IR
  instructions that optimize to 3.
- Before compiling each assignment in place, the compiler proves that writing
  the target register cannot clobber a value still needed by the expression.
- The proof is recursive. Each `...CanAssignToNameInPlace` level first calls a
  matching `...ReferencesName` traversal and then descends again to identify
  where the reference occurs. The actual expression compiler traverses the
  same arena a third time.
- Typed arenas have already removed the previous tiny-object explosion, but
  repeated checked ID/span access now appears as CPU in `arenaTerm`,
  `termName`, `termChild`, and related accessors.

The clearest experiment is a single-pass assignment dependency analysis that
returns both "references target" and "safe to write in place", computed once
per assignment during binding or emission. A small cached classification on
the arena statement is another option if it does not complicate the syntax
representation.

The allocation picture is secondary but still measurable: full compile uses
about 21.8 MB/op, led by syntax-arena storage and lexed tokens, while optimizer
space is about 6.0 MB/op. Do not revive the old recommendation to replace
pointer AST nodes; that work has already landed.

Proof signal: `canCompileSingleLocalAssignmentInPlace` and the name-reference
walker approach zero cumulative share, `emit` time falls on straight-line and
mixed-expression fixtures, and branch-heavy, closures/upvalues, multi-target,
selector, and side-effect-order tests remain green.

## Recommended experiment order

1. Collapse the repeated compiler in-place-assignment analysis. It is isolated,
   27.41% cumulative in a clean compiler profile, and has a narrow semantic
   proof surface.
2. Reduce fixed-call enter/resume work in the canonical VM. It is the dominant
   current call-heavy cost, but requires stronger safety coverage.
3. Remove the persistent `RunHook` allocation floor by separating invariant
   runtime state from invocation state.
4. Inline the execution-window common path, then measure `valueKind` and cache
   indexing again before considering representation changes.
5. Share sparse-table hash and iteration bookkeeping. Keep this behind
   deterministic iteration and small-table memory gates.
6. Do not start a general table allocator campaign unless persistent profiles
   overturn ADR 0006's gate.

Each experiment should name one expected profile movement, run the focused row
first, then run the complete Scenario or runtime-mode family to detect cost
shifting.

## Reproduction

Repeatable family capture:

```sh
scripts/performance-audit --output /tmp/ember-audit --profiles
```

Focused captures:

```sh
go test -run '^$' \
  -bench '^BenchmarkClassicLuau/recursive_fibonacci/ember_run$' \
  -benchmem -benchtime=5s -count=1 \
  -cpuprofile=/tmp/recursive.cpu.pprof \
  -memprofile=/tmp/recursive.alloc.pprof .

go test -run '^$' \
  -bench '^BenchmarkScenarioLuau/sparse_grid_neighbors/ember_run$' \
  -benchmem -benchtime=5s -count=1 \
  -cpuprofile=/tmp/sparse.cpu.pprof \
  -memprofile=/tmp/sparse.alloc.pprof .

go test -run '^$' \
  -bench '^BenchmarkCompilerStageMatrix/256KiB/(emit|compile)$' \
  -benchmem -benchtime=5s -count=1 \
  -cpuprofile=/tmp/compiler.cpu.pprof \
  -memprofile=/tmp/compiler.alloc.pprof .
```

Inspect profiles with:

```sh
go tool pprof -top /tmp/recursive.cpu.pprof
go tool pprof -top /tmp/sparse.cpu.pprof
go tool pprof -top -alloc_space /tmp/sparse.alloc.pprof
go tool pprof -top /tmp/compiler.cpu.pprof
go tool pprof -top -alloc_space /tmp/compiler.alloc.pprof
```

For reliable timing deltas, use a clean worktree, keep other builds idle, take
at least five samples, and compare with `benchstat` or equivalent statistics.
Function-level CPU rankings are more trustworthy than source-line attribution
inside the generated switch. Generated-loop changes belong in
`vm_dispatch_template.go.tmpl`, not directly in `vm_dispatch_generated.go`.
