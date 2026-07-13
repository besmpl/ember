# Performance Profile Audit

## Snapshot and scope

This audit records current benchmark weak points at commit
`2c558013a0f372c5a2e6a508eaee7b195fe9d7fa` on 2026-07-13. Measurements
used Go 1.26.4 on darwin/arm64 and an Apple M1.

The audit answers two different questions:

1. Which benchmark workloads take the longest per operation?
2. Which functions consume the most CPU or allocation inside those workloads?

Absolute benchmark times are not a fair comparison of unlike scripts. A long
scenario can be healthy while a short scenario is inefficient. The function
profiles and stage-specific compiler benchmarks are the stronger prioritization
evidence.

No Luau comparison was collected for this audit. It ranks Ember's current
internal costs, not its parity gap.

## Executive findings

The strongest current weak points are:

1. The general VM dispatch path. `runDirectFrameProductionLoop`,
   `runDirectLoopKernel`, and `valueKind` account for 51.1% of flat Scenario CPU
   samples. This is broad interpreter and value-representation cost rather than
   one slow opcode.
2. Fresh table construction. `newTableWithCapacity` owns 91.4% cumulative
   Scenario allocation space and 85.3% cumulative allocated objects. This also
   makes allocator and Go runtime work visible in CPU profiles.
3. Dynamic string-key tables. `sparse_grid_neighbors` consistently takes about
   2.33 ms/op. Overflowed string-field probing, numeric key formatting, table
   growth, and insertion-order bookkeeping are all material.
4. Compact recursive calls. `recursive_fibonacci` takes about 637 us/op with
   only 2 allocations/op, but `compactProgram.callSite` alone consumes 11.4%
   of CPU because every call performs a non-inlined linear metadata lookup.
5. Parser object churn. A 256 KiB compile allocates about 102,000 objects;
   isolated parsing accounts for essentially all of that count through
   one-element AST slices and pointer-backed scalar nodes.
6. Repeated whole-IR optimization. The 256 KiB straight-line fixture emits
   14,565 IR instructions and optimizes them to 3, while allocating 20.2 MB in
   the optimizer. Multiple passes copy or reassemble the IR and intern
   intermediate constants that are later discarded.

## Benchmark observations

### Long runtime rows

The following values are medians of three 1-second runs for the selected rows.
The ordering is descriptive, not normalized for script work.

| Benchmark | Median ns/op | B/op | allocs/op | Note |
| --- | ---: | ---: | ---: | --- |
| `sparse_grid_neighbors` | 2,333,286 | 40,389 | 407 | Stable across all three runs |
| `formation_layout_score` | 1,494,920 | 4,522 | 26 | Noisy: 510,494 to 1,529,867 ns/op |
| `recursive_fibonacci` | 636,655 | 21 | 2 | Call-heavy; stable and allocation-light |
| `economy_market_tick` | 362,177 | 5,928 | 38 | Dynamic field reads and writes |
| `threat_aggro_table` | 298,665 | 5,979 | 38 | Dynamic string-key accesses |
| `prototype_fallback` | 270,639 | 2,878 | 25 | Function-valued `__index` calls |

The short baseline showed large timing variance on some rows, especially
`formation_layout_score`. Longer confirmation runs made `sparse_grid_neighbors`
and `recursive_fibonacci` stable, but benchmark comparisons should still use
multiple runs and a controlled machine before accepting a small improvement.

### Large-source compiler stages

These are medians of three 100 ms benchmark samples for the 256 KiB compiler
fixture. Stages are isolated and overlap in work, so they do not add to the
end-to-end `Compile` time.

| Stage | Median time | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Parse | 18.295 ms | 26,456,862 | 101,979 |
| Optimize | 12.021 ms | 20,199,520 | 216 |
| Emit | 9.079 ms | 7,570,345 | 33 |
| Bind | 2.451 ms | 1,171,944 | 11 |
| Lex | 2.303 ms | 8,577,027 | 13 |
| Assemble and seal | 2.063 us | 1,664 | 28 |
| Full `Compile` | 37.330 ms | 52,822,386 | 102,261 |
| `LoadProgram` | 38.934 ms | 53,035,853 | 102,294 |

Parse time varied in follow-up runs, but its allocation count and bytes were
stable. The allocation shape, rather than a single wall-time sample, is the
strong signal.

## Profile findings and causes

CPU percentages below are percentages of the named profile. Cumulative values
overlap with their callers and must not be added.

### 1. General VM dispatch and value tagging

The aggregate Scenario CPU profile contains 12.81 seconds of samples:

| Function | Flat | Cumulative | Why it is present |
| --- | ---: | ---: | --- |
| `runDirectFrameProductionLoop` | 30.68% | 79.78% | Main wordcode fetch, decode, switch, and opcode bodies |
| `runDirectLoopKernel` | 11.32% | 19.67% | Predecoded loop still performs an indexed op load, operand copies, and another large switch |
| `valueKind` | 9.06% | 9.06% | Most typed opcode guards compare the `Value.ref` sentinel before reading the tag |
| `wordcodeCacheIndex.cacheIDAt` | 2.26% | 2.26% | Cacheable field/index ops compute block, bit, rank, and popcount from physical PC |
| `wordcodeCacheIndex.cacheSiteAt` | 1.33% | 3.59% | Wraps cache-id lookup and descriptor access |
| `Value.tableRef` | 1.87% | 3.83% | Repeated type/ref extraction on table operations |

Why this is slow:

- General programs still move 16-byte, pointer-bearing `Value` objects through
  registers and repeatedly recover their type through sentinel comparisons.
- The production loop decodes operands and branches through a large switch for
  every instruction. The loop kernel removes wordcode decoding but retains a
  second switch and copies four operands from each predecoded record.
- Cache site IDs are not directly present in the instruction. Each cacheable
  operation derives an ID through a compact rank bitset. This saves wordcode
  space but costs arithmetic, bounds checks, and popcount in the hot path.

This is a structural hotspot, not permission to add benchmark-shaped opcodes.
The private slot work described by ADR 0004 is the broadest existing direction:
if general execution stops repeatedly classifying wide public `Value` objects,
`valueKind` and register traffic should fall together. A smaller experiment is
to compare the compact rank index with a direct, immutable per-PC cache-site
sidecar and measure whether the roughly 3.6% cumulative lookup cost justifies
the memory.

Proof signal: lower flat samples in `valueKind`, cache-index helpers, and the
two dispatch functions on the full Scenario profile, without growing opcode or
side-table budgets accidentally.

### 2. Fresh table construction and allocator pressure

The aggregate Scenario allocation-space profile contains 750.94 MB of sampled
allocation:

| Function | Flat | Cumulative |
| --- | ---: | ---: |
| `newTableStorage` | 55.41% | 55.41% |
| `newTableWithCapacity` | 30.43% | 91.44% |
| `newTableArrayStorage` | 5.59% | 5.59% |
| `Table.ensureIterationJournal` | 1.67% | 1.67% |

The allocated-object profile tells the same story: `newTableStorage` is 45.4%
of objects and `newTableWithCapacity` is 85.3% cumulative.

Why this is slow:

- Every `ember_run` operation starts from a fresh execution and recreates all
  table literals. Nested records therefore allocate a table object and, when
  the inline capacities are exceeded, array, string-field, or hash storage.
- Those allocations make Go runtime work visible in the CPU profile:
  `runtime.madvise` is 10.15% flat, with allocator spans, scheduler wakeups,
  and GC workers elsewhere in the profile.
- This benchmark shape includes legitimate language allocation. It cannot be
  removed by reusing a table whose identity or contents can escape through a
  returned `Value`.

The useful design space is therefore ownership-aware: compact table storage,
compiler-owned literal shape templates, or runtime-owned arenas/slabs with a
proven escape boundary. Blind `sync.Pool` reuse is unsafe because tables and
their graphs can escape `Run`.

Proof signal: `newTableStorage` and `newTableWithCapacity` bytes/op fall in
table-heavy rows, followed by lower Go runtime CPU. Retained-result and identity
tests must remain unchanged.

### 3. `sparse_grid_neighbors`: dynamic strings, hash growth, and iteration order

The focused sparse-grid CPU profile contains 5.02 seconds of samples:

| Function or path | Flat | Cumulative |
| --- | ---: | ---: |
| `runDirectFrameProductionLoop` | 28.88% | 90.04% |
| `Table.rawStringFieldSlotWithProbe` | 4.38% | 9.16% |
| `Table.rawStringFieldSlot` | 0.40% | 9.96% |
| `valueKind` | 6.77% | 6.77% |
| `baseToStringValue` | 0.80% | 6.97% |
| `vmThread.resumeRecordOnlyFixedCallOne` | 4.78% | 6.77% |
| `vmThread.enterRecordOnlyFixedCall` | 2.79% | 4.78% |
| `vmThread.internStringConcatValues` | 0.60% | 3.39% |

The benchmark builds keys with `tostring(x) .. ":" .. tostring(y)`, calls a
local `cellKey` function for centers and neighbors, and accesses a table that
grows from five keys into overflow hash storage.

Why this is slow:

- `rawStringFieldSlotWithProbe` scans inline string fields before checking the
  overflow hash. Once `cells` is mostly overflow-backed, dynamic lookups still
  pay the inline scan on every access.
- The four-entry dynamic string cache scans entries and validates table, key
  identity/hash/bytes, shape token, and slot before falling back to the raw
  lookup.
- Each `cellKey` invocation enters and resumes a compact one-result call
  record. Key creation also performs two numeric `tostring` operations and a
  concatenation/intern lookup.
- Negative neighbor coordinates miss the existing non-negative small-integer
  string cache and fall through to `strconv.FormatInt`.

Focused sparse-grid allocation space is also revealing:

| Function | Flat allocation space |
| --- | ---: |
| `tableHashFields.grow` | 28.21% |
| `newTableStorage` | 26.23% |
| `Table.buildIterationIndex` | 16.47% |
| `Table.markIterationKeyPresent` | 15.98% flat, 32.45% cumulative |
| `Table.ensureIterationJournal` | 4.22% |

The table grows geometrically and records deterministic insertion order. Once
the journal passes 32 entries it also builds a `map[tableKey]int`. This is real
semantic bookkeeping, but its repeated slice/map growth is expensive in this
workload. `strconv.FormatInt` is only 2.34% of allocation bytes but about 40%
of sampled allocated objects, so a signed small-integer cache is primarily an
allocation-count and modest CPU improvement, not the main byte win.

Best bounded experiments:

1. On overflowed tables, probe the hash sidecar before scanning inline fields,
   while preserving the inline fallback and key-identity rules.
2. Preallocate journal/index capacity from the hash capacity when a table is
   already growing, and measure the memory cost on small tables.
3. Compare current hash growth with a larger first overflow capacity on tables
   that continue receiving dynamic fields.
4. Extend the exact small-integer string cache to a bounded signed range.

Proof signal: sparse-grid ns/op and B/op fall; the raw-slot, hash-grow,
journal, and `FormatInt` profile entries move in the predicted direction.

### 4. `recursive_fibonacci`: compact call metadata

The focused recursive profile contains 5.16 seconds of CPU samples:

| Function | Flat | Cumulative |
| --- | ---: | ---: |
| `runCompactCallProgramWords` | 80.23% | 93.60% |
| `compactProgram.callSite` | 11.24% | 11.43% |
| `runtime.pthread_cond_signal` | 5.43% | 5.43% |
| `math.IsNaN` | 1.55% | 1.55% |

Why this is slow:

- The compact numeric path is one large, non-inlinable wordcode switch. It
  decodes operands, validates bounds and NaNs, and pushes/pops explicit compact
  frames for every recursive edge.
- `compactProgram.callSite` is also non-inlinable. For every call it validates
  function/range metadata and linearly scans the current function's sorted call
  sites for a matching word PC. Fibonacci has two static child call sites, but
  it traverses about 21,890 call edges for `fib(20)`, so even a one- or two-entry
  scan consumes 11.4% of total CPU.
- Each call appends a `compactCallFrame` and writes the possibly reallocated
  slice back to pooled state. That state write is required for backing-slice
  retention on exits; it is a redesign candidate, not a line that can simply
  be deleted.
- The script recursion is iterative inside this function. It does not recurse
  through Go frames per script call. Scheduler/preemption symbols are secondary
  profile noise: disabling async preemption removed those symbols but did not
  improve benchmark time.

The most isolated improvement is a direct call-site index keyed by function and
word PC, or an equivalent prebound call descriptor. It should remove the
`callSite` scan and make call setup easier to inline. A depth-indexed frame store
could then avoid append/clear/state-sync work, but has more pooling and fallback
invariants.

Proof signal: `compactProgram.callSite` approaches zero and recursive time falls
by roughly its current 10-12% share before attempting a larger decoded micro-op
or dispatch redesign.

### 5. Compiler parsing: many tiny AST allocations

The 256 KiB fixture contains 72,815 syntax nodes. Isolated parse allocates
26.46 MB and 101,979 objects per operation. Allocation-object attribution is
concentrated in the nested parser path:

- `parseIdentifierStatement`: 29.97% flat objects;
- `parseExpressionList`: 16.20%;
- `parseExpression`: 15.94%;
- `parseAndExpression`: 15.26%;
- `parseAdditiveExpression`: 15.08%;
- `parsePrimaryTerm`: 7.07%.

Why this is slow:

- A simple `value = value + 1` allocates one-element backing slices at several
  grammar levels: assignment targets, expression lists, `or` terms, `and`
  terms, and additive rest.
- Numeric terms store a pointer to a scalar, creating another small object.
- `parseStatement` attempts type-alias and keyword forms before reaching the
  common identifier-assignment path.
- The lexer initially caps token capacity at 4,096 while this fixture has tens
  of thousands of tokens, so the token slice repeatedly grows and copies.

The clearest improvement is a private compact AST representation with an inline
first element and an optional remainder for expression/value lists. This keeps
the common one-element shape allocation-free without weakening the public API.
A smaller first slice is token-kind dispatch in `parseStatement`, followed by a
bounded token-capacity estimate that scales farther for large source files.

Proof signal: parse allocs/op falls by several allocations per assignment,
ideally at least 50%, and parse CPU falls across straight-line, branch-heavy,
type-heavy, and malformed-source fixtures.

### 6. Compiler optimization and emission: full-stream work that is discarded

The large straight-line fixture emits 14,565 IR instructions and finishes with
3. Isolated optimization still takes 12.02 ms and allocates 20.20 MB/op.

Important allocation-space entries in the focused compiler profile are:

| Function | Flat allocation space |
| --- | ---: |
| `bytecodeBuilder.emitWithSource` | 11.66% |
| `assembleBytecodeIRRaw` | 5.42% |
| `bytecodeBuilder.addKeyedConstant` | 5.30% |
| `applyBytecodeIRRemovalSet` | 4.70% |
| `bytecodeIRLiveAfter` | 2.61% |
| `optimizeBytecodeIRWithFacts` | 35.62% cumulative |

Why this is slow:

- Emission appends source-bearing IR records to an initially nil slice. Only 33
  allocations occur, but geometric growth and copying total 7.57 MB/op.
- The optimizer performs peephole removal, control-flow simplification,
  constant propagation, move propagation/coalescing, loop-invariant hoisting,
  dead-code elimination, and another control-flow simplification. Several
  passes assemble a second instruction form, copy IR, or rebuild CFG/liveness.
- Straight-line scalar propagation interns every intermediate folded value into
  the constant pool. Thousands of transient constants are hashed and appended,
  then dead-code elimination and constant compaction discard almost all of
  them.
- Move-related analyses still run when the fixture contains no `opMove`, and
  loop work can run when there is no backedge.

The first experiments should be conservative guards and deferred materialization:

1. Skip move-only passes when no move opcode exists, and skip loop-only work
   when there is no backedge.
2. Defer interning folded constants until the surviving rewrite is known, or
   keep transient values in a local lattice/map rather than the durable pool.
3. Reuse one analysis across passes that do not invalidate its inputs, and
   avoid repeated IR-to-instruction materialization.
4. Give the emitter a conservative, bounded IR-capacity hint derived from
   syntax nodes or representative source density.

Proof signal: optimizer B/op falls by at least half on the straight-line case,
`addKeyedConstant` and repeated analysis allocations shrink, and compiler
corpus/CFG fixtures prove no semantic or code-quality regression.

## Recommended experiment order

1. Direct-index compact call sites. It is isolated, measured at 11.4% of the
   recursive profile, and has a precise success signal.
2. Guard no-op optimizer passes and stop interning transient folded constants.
   This targets large measured allocations without changing public structure.
3. Compact common parser list/node shapes. This has the largest compiler
   allocation-count opportunity but a broader internal blast radius.
4. Probe hash storage first on overflowed dynamic-string tables, then tune hash
   and iteration-journal capacity with mixed-table benchmarks.
5. Prototype ownership-safe table allocation or literal-shape reuse. The
   potential corpus-wide gain is large, but escape and identity semantics make
   this higher risk.
6. Reprofile before considering private-slot, decoded-micro-op, or dispatch
   restructuring. Those are broad changes and should follow the smaller
   measured wins.

Every experiment should start with one predicted profile movement above, then
be kept only if the relevant full benchmark family improves without moving the
cost to another function or increasing allocations elsewhere.

## GC A/B result

A bounded `GOGC=off` experiment did not make the 256 KiB compile faster. Five
three-iteration samples had a 37.576 ms median with normal GC and 41.551 ms with
GC disabled, about 10.6% slower. The run is intentionally small, so heap growth
and page acquisition affect it, but it is enough to reject GC tuning as the
primary compiler remedy. The target is less AST/IR allocation and copying.

## Reproduction

Unprofiled timing baselines:

```sh
go test -run '^$' \
  -bench 'Benchmark(Top10Luau|ClassicLuau|ScenarioLuau)/.*/ember_run$' \
  -benchmem -benchtime=200ms -count=3 .

go test -run '^$' \
  -bench 'Benchmark(CompileMatrix|CompilerStageMatrix)$' \
  -benchmem -benchtime=100ms -count=3 .
```

Focused profiles:

```sh
go test -run '^$' \
  -bench 'BenchmarkScenarioLuau/.*/ember_run$' \
  -benchtime=500ms -count=1 \
  -cpuprofile=/tmp/ember-scenario.cpu.prof \
  -memprofile=/tmp/ember-scenario.mem.prof .

go test -run '^$' \
  -bench 'BenchmarkScenarioLuau/sparse_grid_neighbors/ember_run$' \
  -benchtime=5s -count=1 \
  -cpuprofile=/tmp/ember-sparse.cpu.prof \
  -memprofile=/tmp/ember-sparse.mem.prof .

go test -run '^$' \
  -bench 'BenchmarkClassicLuau/recursive_fibonacci/ember_run$' \
  -benchtime=5s -count=1 \
  -cpuprofile=/tmp/ember-recursive.cpu.prof \
  -memprofile=/tmp/ember-recursive.mem.prof .

go test -run '^$' \
  -bench 'BenchmarkCompilerStageMatrix/256KiB/compile$' \
  -benchtime=5s -count=1 \
  -cpuprofile=/tmp/ember-compiler.cpu.prof \
  -memprofile=/tmp/ember-compiler.mem.prof .
```

Profile summaries:

```sh
go tool pprof -top /tmp/ember-scenario.cpu.prof
go tool pprof -top -cum /tmp/ember-sparse.cpu.prof
go tool pprof -top -alloc_space /tmp/ember-sparse.mem.prof
go tool pprof -top -alloc_objects /tmp/ember-compiler.mem.prof
```

Memory profiles report allocation over the profile lifetime, not retained heap.
CPU profiling also includes Go runtime and profiler work. The rankings above
exclude obvious `testing`, regexp, gzip, and pprof harness allocations. Source
line attribution inside very large switch functions is coarse; function-level
samples and focused benchmark deltas are more trustworthy.
