# Ember Runtime Parity Campaign

## Purpose

Close Ember's current roughly 7-15x runtime gap against the pinned Luau interpreter without CGo, native code generation, benchmark-specific opcodes, or mutable shared prototypes.

This plan extends the existing `docs/exec-plans/interpreter-core-speed.md`. That document correctly identifies dispatch, calls, builtin transport, allocation, and string-key access as structural losses, but its final acceptance target is only `<= 2.0x`. This campaign continues through actual parity.

The supplied worktree currently contains the exact architectural costs that must be removed:

- `Value` is a 16-byte pointer-bearing structure in `value.go`; scalar values use pointer sentinels.
- executable code is held in both `instruction` and 16-byte `packedInstruction` forms in `bytecode.go`;
- `runDirectFrameCore` in `vm.go` unpacks each instruction, calls a generic trace seam, and carries guarded-block/debug/budget branches through the production loop;
- `vmFrame`, `vmResultWindow`, `vmPendingCall`, borrowed windows, and Go recursion remain in script call paths;
- `dynamicStringIndexCache` is a four-entry heavyweight cache, and `executionArtifact.apply` allocates one cache object for every PC when a function contains any cacheable operation;
- `Proto` owns mutable execution caches, preventing safe concurrent execution of one compiled prototype;
- `Table`, `tableKey`, and `tableHashEntry` are pointer-heavy and much larger than the data they represent;
- native fast calls still manufacture `[]Value` result slices;
- current compiler output executes too many instructions, especially `MOVE`, field-access, and call-setup instructions.

The performance numbers in this plan are gates, not promises. Each gate must be measured on the pinned reference machine before the next phase is accepted.

---

## Final Acceptance Contract

### Reference environment

Freeze one comparison environment until the campaign is complete:

- Apple M1 / Darwin reference machine;
- the repository's pinned Go 1.26 toolchain;
- `CGO_ENABLED=0`;
- `GOMAXPROCS=1` for the primary interpreter comparison;
- pinned Luau CLI 0.728 and its existing SHA-256;
- the existing 25 Scenario programs plus Top10 and classic markers;
- nine paired runs using `N=1,10,100,1000`, fitting `T(N)=entry+N*inner` exactly as `runtime_parity_test.go` already does.

Do not update Luau, Go, compiler flags, the machine, or the corpus during the campaign without starting a new baseline series.

### Runtime target

The campaign is complete only when the warm inner-slope gate passes:

- median Ember/Luau ratio `<= 0.95x`;
- p90 ratio `<= 1.00x`;
- no Scenario row above `1.25x`;
- geometric mean `<= 1.00x`;
- no result, error, metamethod, coroutine, iteration, or host-boundary regression;
- no Scenario-specific runtime mechanism;
- `Proto` is immutable after compilation;
- no CGo, native code generator, hidden executable memory, or new dependency.

A secondary cold-entry gate should keep Ember's `Run` entry cost at `<= 1.50x` Luau's measured entry component. The engine-facing persistent runtime API must also have a separate steady-state benchmark that excludes one-time construction.

### Memory target

- no allocation per fixed-arity script call or return;
- no allocation per warm field-cache hit;
- no allocation per ordinary builtin call;
- no register-stack allocation after a runtime is warmed;
- GC/runtime background samples below 5% on the Scenario aggregate profile;
- long-running heap usage reaches a stable plateau in a scripted churn soak test;
- internal register/table slots are 8 bytes and contain no Go pointers.

---

## Non-Negotiable Engineering Rules

1. **A slice replaces a mechanism; it does not add a second permanent path.** Temporary differential paths are test-only and deleted at cutover.
2. **No benchmark-shaped feature.** An opcode, cache, quickening rule, or compiler transform must be justified by a general language shape and broad corpus frequency.
3. **Every slice starts with a red tracer.** The tracer proves behavior, size, allocation, instruction count, or an explicit profile invariant.
4. **The production loop contains no diagnostics.** Hooks, budgets, opcode counts, cache counters, and tracing run in a separate instrumented loop.
5. **No mutable runtime state in `Proto`.** Caches, quickened code, globals, and canonical closure state belong to a runtime instance.
6. **Do not hide Go pointers in integers.** Compact values use arena handles, never raw pointer NaN-boxing that the Go collector cannot see.
7. **Profile before and after every phase.** Optimizations that fail to move the full corpus are reverted, even when their microbenchmark looks impressive.
8. **Complexity is gated.** Opcode count, runtime side-table count, engine lines, cache bytes, and generated code size are recorded and ratcheted.

---

## Phase Map

| Phase | Main result | Principal loss removed | Exit target |
| --- | --- | --- | --- |
| 0 | Frozen evidence and gates | Benchmark ambiguity | Trusted baseline |
| 1 | Honest production loop | Instrumentation, unpacking, guarded scaffolding | Arithmetic `<=1.4x`; geomean materially lower |
| 2 | 32-bit wordcode | Instruction bandwidth and decode cost | Code bytes down at least 60% |
| 3 | Immutable runtime instance | Shared mutable caches and future specialization races | Same `Proto` safely runs concurrently |
| 4 | 64-bit internal slots and handle heap | Pointer scanning, barriers, wide copies | GC under 8%; stack traffic roughly halved |
| 5 | In-loop calls and compact frames | Go recursion, frame objects, result windows | Recursive/call-heavy rows `<=2.5x` |
| 6 | Compact tables, symbols, and shape ICs | Wide keys, byte compares, huge per-PC caches | Table-heavy rows `<=2.0x` |
| 7 | In-place builtins and warm runtime reuse | Result slices, global refill, run churn | Builtin-heavy allocations at floor |
| 8 | Compiler emits less work | Excess dynamic instructions and moves | Geomean `<=1.5x`, max `<=2.0x` |
| 9 | Adaptive quickening | Remaining tag/shape dispatch | Median `<=1.10x`, p90 `<=1.25x` |
| 10 | Heap polish and parity closure | Long-run GC, rare outliers, branch layout | Final parity contract |

The exit targets are cumulative checkpoints. A phase does not pass because one favorite benchmark improves.

---

# Phase 0 - Freeze Reality

## Goal

Create one trustworthy baseline and prevent the team from optimizing a moving target.

## Slice 0.1 - Preserve the supplied worktree

The supplied repository is heavily dirty. Before implementation:

- record `git status`, the worktree tree hash, and hashes of every modified/untracked source file;
- archive the exact tree or create a dedicated baseline commit on a performance branch;
- preserve the current raw parity TSV, CPU profile, heap profile, disassembly counts, and allocation table;
- do not mix compiler-throughput work, analyzer work, or unrelated compatibility changes into runtime slices.

**Exit:** any engineer can reproduce the same source tree and identify whether a benchmark came from it.

## Slice 0.2 - Re-capture the full baseline

Capture:

- nine-pair runtime parity data for all 25 Scenario rows;
- Top10 and classic markers;
- `pprof` CPU, alloc-object, alloc-space, and block profiles;
- dynamic opcode counts per program;
- script-call, host-call, metamethod-call, yield, and return counts;
- code bytes, constants, registers, cache bytes, table allocations, and frame high-water marks;
- Go GC CPU fraction and bytes scanned.

Store raw data under a fingerprint containing source tree hash, toolchain, Luau binary hash, CPU, OS, environment, and command line.

## Slice 0.3 - Add permanent structural budgets

Add tests for:

- `unsafe.Sizeof`/reflection sizes of the hot structures;
- opcode count and runtime-only `Proto` field count;
- cache bytes per cacheable site and per instruction;
- generated code bytes and dynamic instruction counts for representative fixtures;
- absence of benchmark names in runtime/compiler identifiers;
- absence of runtime mutation in `Proto` after Phase 3;
- production execution leaving all instrumentation counters untouched.

## Slice 0.4 - Establish phase gates

Use ledger mode during a representation cutover, then immediately restore a hard gate. Recommended cumulative hard gates:

- after Phase 2: every row `<=6.0x`, geomean `<=4.0x`;
- after Phase 5: every row `<=3.0x`, geomean `<=2.2x`;
- after Phase 6: every row `<=2.0x`, geomean `<=1.6x`;
- after Phase 8: every row `<=2.0x`, geomean `<=1.5x`;
- after Phase 9: median `<=1.10x`, p90 `<=1.25x`, max `<=1.50x`;
- final: the parity contract above.

**Phase 0 exit:** reproducible baseline, complete correctness suite, permanent metrics, and a clean ownership map.

---

# Phase 1 - Make the Production Loop Honest

## Goal

Remove work that occurs before the actual opcode body.

## Slice 1.1 - Delete the guarded-block experiment

The current loop carries `opGuardedBlock`, `guardedFieldBlockPlans`, fallback ranges, counters, and an environment switch. The supplied profile analysis indicates that real compiler output rarely forms the selected sequence and the machinery does not justify its loop cost.

- run one same-binary A/B using the existing harness;
- keep it only if it produces at least a 5% Scenario geometric-mean win without a regression above 3% on any row;
- otherwise delete the opcode, planner, side table, counters, tests, and environment variable;
- recover the opcode/side-table budget for general wordcode operations later.

## Slice 1.2 - Split production and instrumented execution

Create two loops only:

- `runLoop`: no generic type parameter, trace interface, PIC counters, hook checks, or metric calls;
- `runLoopInstrumented`: debug hooks, budgets, counters, and test diagnostics.

Selection occurs once before entering the loop. No per-instruction `if trace != nil` or no-op method call survives.

**Red tracer:** plain `Run` cannot change opcode/PIC/PC counters.

## Slice 1.3 - Decode current packed operands in place

As the immediate bridge to wordcode:

- remove `packedInstruction.unpack()` from execution;
- fetch the packed element once and read `op/a/b/c/d` directly;
- keep `pc`, code, constants, register base, and current frame metadata in locals;
- write state back only at calls, yields, protected exits, hooks, and returns;
- use `-d=ssa/check_bce` to prove bounds checks are removed or hoisted on hot register/code accesses.

## Slice 1.4 - One loop, cold helpers

Remove the duplicate cold interpreter loop. Rare complex operations may call a cold helper that returns a compact action (`continue`, `call`, `yield`, `return`, `error`), but execution returns to the same production switch.

**Phase 1 exit:** no guarded planner in the hot path, no unpacking, no no-op tracing, no second full interpreter; arithmetic markers improve without allocation movement.

---

# Phase 2 - Replace 16-Byte Instructions With 32-Bit Wordcode

## Goal

Make instruction fetch and decode resemble a real register VM rather than a decoded IR array.

## Slice 2.1 - Approve the word formats

Use one 32-bit word with an 8-bit opcode and these general layouts:

- `ABC`: opcode + three 8-bit operands;
- `AD`: opcode + one 8-bit operand + signed/unsigned 16-bit operand;
- `E`: opcode + signed 24-bit displacement;
- `AUX`: a following word for a large constant, cache index, import path, or extended count.

Registers are limited to 0-254 in executable functions. The compiler must shrink/split/error before exceeding the format; it may not silently fall back to wide runtime instructions.

## Slice 2.2 - Build verifier and disassembler first

- central metadata declares each opcode's format, effects, reads/writes, control flow, and AUX use;
- verifier rejects malformed AUX sequences, register/constant bounds, invalid jumps, and open-call misuse;
- disassembly and tests round-trip every encoding, including negative jumps and extended constants;
- fuzz the decoder/verifier with arbitrary word streams.

## Slice 2.3 - Emit wordcode directly

The compiler retains a high-level private IR, but final assembly writes `[]uint32` directly. Delete the permanent `[]instruction` plus `[]packedInstruction` duplication from `Proto`.

Keep only data execution consumes:

- words;
- constants in runtime-ready form;
- child prototypes;
- upvalue descriptors;
- line/debug data kept outside the hot function body unless hooks are active;
- compact cache descriptors for cacheable sites.

## Slice 2.4 - Switch execution atomically

The production loop becomes conceptually:

```go
word := code[pc]
pc++
switch opcode(word) {
case opMove:
    regs[a(word)] = regs[b(word)]
// ...
}
```

No old-format execution remains after cutover.

## Slice 2.5 - Reclaim opcode space

Delete narrow benchmark-like branch/field opcodes and replace them, within the existing budget, with broadly useful wordcode forms such as:

- immediate nil/bool/number loads;
- constant arithmetic;
- constant-symbol field get/set;
- method lookup/call setup (`NAMECALL`-shaped);
- base builtin fast-call forms;
- compact numeric and generic loop operations;
- one-result call/return forms.

**Phase 2 exit:** executable code storage falls by at least 60%, instruction fetch no longer materializes structs, arithmetic is near the target, and old instruction representations are deleted.

---

# Phase 3 - Make `Proto` Immutable and Introduce Runtime Instances

## Goal

Separate compiled truth from mutable execution state. This is required for concurrency, compact handles, caches, and quickening.

## Slice 3.1 - Add private runtime ownership

Introduce a private runtime/state object owning:

- globals and global slot epochs;
- string interning and symbol IDs;
- stacks and frames;
- runtime heap/slabs;
- per-function inline caches;
- per-function quickened words later;
- coroutine states;
- instrumentation state when explicitly enabled.

`Run(proto)` remains public and uses an acquired runtime internally. A persistent engine-facing runtime API can be added separately after the private seam is stable.

## Slice 3.2 - Add runtime function instances

A runtime function instance pairs immutable `*Proto` with mutable per-runtime data:

- compact IC array containing only actual cacheable sites;
- global slot cache;
- optional quickened word copy;
- canonical zero-capture closure handle;
- hotness and deoptimization state.

Instruction operands refer to compact cache indices, not raw PC-sized arrays.

## Slice 3.3 - Remove all runtime writes to `Proto`

Move or delete:

- `directFrameIndexCaches`;
- canonical closure mutation;
- any warmed global/cache state;
- future quickening state.

Add a race test that executes the same `Proto` concurrently in independent runtimes and compares results.

**Phase 3 exit:** `Proto` is immutable after sealing, one compiled program is safely reusable, and cache memory scales with cacheable sites rather than instruction count.

---

# Phase 4 - Introduce 64-Bit Internal Slots and a Handle Heap

## Goal

Stop making every numeric register look like a Go pointer and stop copying 16-byte values throughout the VM.

The public `Value` type remains stable. The runtime receives a private representation.

## Slice 4.1 - Prove the encoding

Define `type slot uint64` with:

- ordinary IEEE-754 numbers stored directly;
- tagged nil and booleans;
- tagged handles for strings, tables, closures, upvalues, userdata, and host callables;
- a rare boxed-number escape for NaN bit patterns that collide with the tag space if bit-exact NaN preservation is required.

Handles contain an object-kind tag, index, and generation. They never contain a Go pointer or `uintptr` disguised as data.

**Red tracers:** all scalar/reference semantics, unusual NaNs, identity, equality, forced Go GC, stale-handle rejection, and public conversion round-trips.

## Slice 4.2 - Add typed object slabs

The runtime heap owns typed slabs/free lists for:

- interned strings and symbols;
- tables;
- closures;
- open/closed upvalues;
- host userdata/callables;
- any rare boxed number.

Objects use stable indices. Reuse increments generation so stale handles fail closed in debug/test builds.

## Slice 4.3 - Convert registers and constants

Move these to `slot`:

- register stack;
- constants consumed by execution;
- globals;
- arguments, varargs, and return values;
- closure captures and table cells as later slices land.

Keep conversion at the public/host seam. Opcode bodies may not repeatedly convert between `Value` and `slot`.

## Slice 4.4 - Define external lifetime rules

When a runtime object crosses to Go:

- public wrappers retain the owning runtime/heap or an explicit pin token;
- host-global values are roots;
- objects returned by stateless `Run` remain valid;
- pooled runtimes cannot be reset while escaped objects still depend on them;
- a persistent runtime owns its heap explicitly.

Do not use finalizers as the primary correctness mechanism.

## Slice 4.5 - Add a correct first collector

Implement a simple handle mark/sweep before optimizing it. Roots are:

- live stack range;
- frames and open results;
- globals;
- open upvalues;
- coroutine stacks;
- pinned host/public objects.

Clearing a dead slab entry releases its Go-owned slices/strings/pointers to the Go collector. Incremental/generational work comes in Phase 10.

**Phase 4 exit:** internal slots are exactly 8 bytes with no pointers, numeric stack writes have no Go write barrier, stack scan pressure is roughly halved, and GC/background CPU falls below 8%.

---

# Phase 5 - Keep Script Calls Inside One Dispatch Loop

## Goal

Make a script call a frame-record push, not a Go call, heap object, window object, and result wrapper.

## Slice 5.1 - Define the compact frame record

Target `<=48` bytes. It should contain only information that survives an opcode boundary:

- function instance/proto reference or handle;
- return PC;
- absolute register base and logical top;
- result destination and requested count;
- argument/vararg base and count;
- protected-call depth/flags;
- closure/upvalue handle as needed.

Current frame state remains in loop locals. Delete pointer-linked caller fields.

## Slice 5.2 - Fixed calls and returns

For script callees:

- `CALL1` pushes a record, establishes the callee base, nil-fills missing parameters, reloads loop locals, and continues;
- `RETURN1` copies one slot directly to the caller destination, pops the record, restores locals, and continues;
- script recursion does not consume Go stack depth;
- warmed fixed calls allocate zero objects.

## Slice 5.3 - Open calls, multiple returns, and varargs

Use a Lua-style logical `top`:

- open result counts are ranges in the shared slot stack;
- varargs are recorded as base/count in the frame;
- forwarding does not construct slices;
- fixed-result callers copy only the requested count;
- remove `vmResultWindow`, owned result slices, and copied call-argument paths from internal execution.

## Slice 5.4 - Upvalues without per-local cells

Represent open captures by stack index and a heap upvalue handle. Maintain one ordered/open-upvalue structure per thread. On frame close, copy only captured live slots into the upvalue object.

Zero-capture and immutable copied captures remain allocation-free or slab-allocated in bulk.

## Slice 5.5 - Metamethod calls use the same scheduler

Function-valued `__index`, `__newindex`, `__call`, arithmetic/comparison metamethods, and `__tostring` push ordinary frame records. They may not recurse through `runInlineScriptCall*`.

## Slice 5.6 - Protected calls, hooks, and interrupts

- protected frames record a boundary depth;
- an error unwinds frame records and stack top to that boundary;
- debug call/return events are generated only in the instrumented loop;
- instruction budgets and host interrupts preserve exact PCs;
- differential tests cover errors during metamethods, nested pcall, yield, and host continuation.

## Slice 5.7 - Coroutines own stack/frame pairs

A coroutine owns:

- slot stack;
- compact frame slice;
- open-upvalue state;
- status and continuation metadata.

Yield/resume transfers or references ranges; it does not clone result slices or rebuild frame objects.

**Phase 5 exit:** no `vmFrame` pool in normal execution, no Go recursion for script calls, no internal result-window objects, fixed calls allocate zero, and recursive/call-heavy rows are at or below their cumulative gates.

---

# Phase 6 - Rebuild Tables Around Symbols, Shapes, and Compact ICs

## Goal

Make the common table operation a handful of integer comparisons and one slot load/store.

## Slice 6.1 - Replace runtime string keys with symbols

- intern compile-time strings when loading constants into a runtime;
- assign stable per-runtime `symbolID` values;
- constant field opcodes carry a symbol ID/constant reference;
- table field storage and ICs compare symbol IDs, never Go string bytes;
- dynamic external strings intern on first table-key use or use a tested hash/byte fallback.

## Slice 6.2 - Compact the table header and key representation

Target budgets:

- common table header `<=64` bytes before inline storage;
- generic hash entry `<=24` bytes;
- generic key represented by one 8-byte slot plus cached hash/control metadata;
- no `tableKey` structure on numeric-array or constant-symbol paths.

Keep cold features in a sidecar: metatable, weak-mode state, iterator bookkeeping, diagnostics, and uncommon hash metadata.

## Slice 6.3 - Add hidden-class/shape storage for string fields

A shape describes ordered symbol-to-slot layout. A table stores:

- `shapeID`;
- dense field values;
- compact array storage;
- dictionary storage only for dynamic/churn-heavy cases;
- metatable handle/epoch in the cold or compact header as profiling dictates.

Adding a field follows a shape transition. Repeated deletion or excessive polymorphism moves the table into dictionary mode rather than creating shape explosions.

## Slice 6.4 - Replace the current PIC

A monomorphic field IC should be roughly 16 bytes:

- expected shape ID;
- slot index;
- metatable epoch/flags;
- optional second shape only after the site proves polymorphic.

Allocate one IC per cacheable instruction, not per PC. On a warm direct hit:

1. compare shape ID and metatable epoch;
2. load/store `fields[slot]`;
3. continue.

No table pointer comparison, key length, hash, generation chain, or byte comparison belongs on that hit path.

## Slice 6.5 - Metatable lookup caches

Cache `__index`, `__newindex`, `__call`, and common metamethod resolution by metatable shape/epoch and symbol. A dirty metatable write bumps one epoch. Function-valued fallbacks push a Phase 5 frame record.

Negative lookup caching must be explicit and invalidated by the same epoch.

## Slice 6.6 - Fast arrays and length

- positive integral keys test the array part before any generic hash work;
- compiler-provided capacities avoid early growth;
- append growth is geometric;
- holes use compact metadata, not full rescans on every operation;
- length behavior remains differential-tested against the claimed Luau semantics.

## Slice 6.7 - Decide raw iteration semantics

The current deterministic insertion-order journal costs mutation and memory. Pick one policy deliberately:

**Preferred parity policy:** document raw iteration as unspecified and iterate array/hash storage directly, matching Luau's freedom and removing the journal.

**If deterministic order is mandatory for Hearth:** allocate a compact order sidecar lazily on the first active iteration and update it only while deterministic iteration is in use. Do not charge every never-iterated table.

This decision requires explicit compatibility documentation and tests.

## Slice 6.8 - Table literal templates

The compiler supplies array capacity, field symbols, and a shape template. Instantiation performs one object/slab allocation and fills dense slots directly.

**Phase 6 exit:** warm field access touches no string bytes, cache storage is proportional to cacheable sites, common table/key/hash sizes meet budgets, and table/metatable-heavy rows reach the cumulative `<=2.0x` target.

---

# Phase 7 - Make Builtins, Globals, and Run Entry Allocation-Free

## Goal

Stop crossing through public slice-based APIs for internal operations.

## Slice 7.1 - In-place native ABI

Internal builtins receive the runtime, argument base/count, result base, and requested result count. They read/write slots directly and return a compact status/result count.

Delete:

- `[]Value{result}` wrappers;
- internal argument slices for fixed counts;
- `directFrameApplyCallIslandResults`-style result transport;
- fallback to public `HostFunc` for known base builtins.

Builtins that invoke script (`table.sort` comparator, `tostring` metamethod) schedule a normal frame and continuation.

## Slice 7.2 - Guarded intrinsic IDs

The compiler emits a builtin ID only when resolving a known base global. The runtime function instance guards the relevant global slot identity/epoch. If the host replaces the builtin, execution takes the general call path without changing semantics.

## Slice 7.3 - Warm global slots

Resolve global names to runtime slot indexes once. Warm loads/stores perform an index plus epoch/identity check, not a map/string lookup.

## Slice 7.4 - Reuse runtime stacks and slabs

- persistent runtimes keep stacks, frame slices, and heap slabs;
- stateless `Run` uses a pool only when no escaped object owns the runtime;
- truncating an 8-byte non-pointer stack does not clear every slot unless required for semantic or memory-retention reasons;
- globals/base environment are initialized once per runtime, not per script invocation.

## Slice 7.5 - Optional high-performance host ABI

Keep `HostFunc` and `ContextHostFunc` unchanged. Add an opt-in engine-facing callback API only if host-heavy benchmarks prove the need. It should expose borrowed typed argument/result views with explicit lifetime and reentrancy rules, avoiding conversion slices.

**Phase 7 exit:** ordinary builtin calls allocate zero, run-entry stack growth disappears from steady-state profiles, global string lookup disappears from warm profiles, and builtin-heavy allocation budgets are at or below their old floors.

---

# Phase 8 - Make the Compiler Emit Less Work

## Goal

Reduce dynamic instructions after each remaining instruction has become cheap.

## Slice 8.1 - Make dynamic instruction count a hard KPI

For every Scenario row record:

- total dynamic instructions;
- opcode distribution;
- `MOVE` share;
- calls and call-setup instructions;
- field operations and cache hit rate;
- code bytes and register high-water mark.

Ratchet fixture ceilings. A runtime speed change that merely moves work into hidden helpers must also report helper entries.

## Slice 8.2 - General wordcode lowering

Use general language-shape operations:

- immediate scalar loads;
- register/constant arithmetic;
- constant-symbol field get/set;
- method lookup plus self setup;
- fixed-result call/return;
- intrinsic fast calls;
- numeric/generic loop operations;
- table-literal templates.

Do not reintroduce row-field or Scenario-pattern fusions.

## Slice 8.3 - Virtual-register IR and coalescing

Replace ad-hoc temporary reuse with a small virtual-register IR that preserves:

- explicit use/def information;
- basic blocks and control-flow edges;
- call argument/result bundles;
- multiple-return/open-top semantics;
- capture constraints.

Then run:

- copy propagation;
- dead definition elimination;
- local value numbering where errors/metamethods permit;
- liveness;
- move coalescing;
- linear-scan physical register assignment;
- parallel-copy scheduling around calls/branches.

Target `MOVE <=10%` of dynamic instructions on the Scenario aggregate unless semantics make a particular row exceptional.

## Slice 8.4 - CFG and constant simplification

- thread jump chains;
- invert branches to prefer fallthrough;
- remove jump-to-next;
- fold constants without changing metamethod/error behavior;
- remove unreachable blocks;
- shrink frames using full CFG liveness, including captures and varargs.

## Slice 8.5 - Recognize builtins and methods early

Resolve base builtin identities and constant method names before generic global/field/call lowering. Emit one general `NAMECALL`-shaped sequence rather than field load, self move, argument shuffle, and call.

## Slice 8.6 - Optional conservative local inlining

Only after Phase 5 call costs are measured:

- inline small statically known local closures with no yield/vararg/complex upvalue behavior;
- require a corpus-wide gain and a strict code-size ceiling;
- reject if I-cache/code growth erases the win.

**Phase 8 exit:** dynamic instruction count falls at least 30% on the aggregate and materially on every current worst offender; compiler throughput/allocations regress by no more than the approved budget; geomean is `<=1.5x`, no row exceeds `2.0x`.

---

# Phase 9 - Adaptive Quickening for the Last Mile

## Goal

Specialize stable runtime behavior without native code and without mutating shared bytecode.

## Slice 9.1 - Runtime-owned quickened words

A runtime function instance starts on immutable Proto words. After a small warm threshold, it may allocate/copy a private word stream and rewrite only that stream.

Quickening state is therefore:

- safe across concurrent runtimes;
- disposable;
- excluded from the compile artifact;
- observable in the instrumented loop but nearly free in production.

## Slice 9.2 - Numeric quickening

Generic arithmetic/comparison/loop sites that repeatedly see numbers rewrite to guarded numeric forms. On a tag mismatch they execute the generic slow helper and either retain or dequicken according to a saturating miss policy.

## Slice 9.3 - Field/method quickening

Stable shape sites rewrite to a compact field/method form carrying the IC index. The IC still validates shape/epoch; the quickened opcode removes generic kind/key routing.

## Slice 9.4 - Stable call-target quickening

Stable closure or intrinsic targets may use a guarded direct call form. A changed global, field, metatable, or closure identity immediately falls back through the general path.

## Slice 9.5 - Corpus-driven superinstructions

Only consider a superinstruction when all are true:

- the pair/triple is common across at least eight Scenario rows or at least 5% of aggregate dynamic instructions;
- it represents a general language shape;
- same-binary A/B shows at least a 3% geometric-mean win;
- no row regresses more than 2%;
- it fits the opcode/complexity budget by deleting or combining another mechanism.

Do not fuse arbitrary field-read/modify/write benchmark patterns again.

**Phase 9 exit:** median `<=1.10x`, p90 `<=1.25x`, no row above `1.50x`, with bounded warm-up and stable deoptimization behavior.

---

# Phase 10 - Collector, Long-Run Stability, and Parity Closure

## Goal

Finish the engine, not merely the benchmark run.

## Slice 10.1 - Incremental handle collector

Evolve the correct Phase 4 mark/sweep collector into bounded increments:

- tri-color marking over handle graphs;
- work proportional to allocated bytes or executed instruction budget;
- roots from runtime state, coroutines, globals, open upvalues, and pins;
- write barrier only on heap-object slot writes during marking;
- no barrier on register moves/arithmetic.

## Slice 10.2 - Slab and nursery tuning

If allocation-heavy rows still lag:

- bump-allocate young table/closure/upvalue objects in typed slabs;
- recycle cleared entries through free lists;
- consider a small generational nursery only after profiles prove old-object scanning is material;
- keep object handles stable.

## Slice 10.3 - Long-running soaks

Add deterministic stress programs for:

- table churn and deletion;
- closure/upvalue churn;
- coroutine creation/yield/death;
- metamethod mutation;
- host-pinned object escape/release;
- repeated module/runtime use.

Gate heap plateau, stale handles, pause distribution, and result correctness.

## Slice 10.4 - Branch and layout polish

With all architecture stable:

- reorder hot switch cases/functions according to final profiles;
- isolate cold error formatting and host conversion;
- remove redundant bounds checks;
- tune frame/stack/table growth constants;
- compact cache arrays and metadata;
- inspect assembly for accidental interface calls, typedmemmove, write barriers, and non-inlined accessors.

## Slice 10.5 - Close every outlier by attribution

For each row above final max:

1. compare dynamic instruction count;
2. compare ns/instruction;
3. inspect cache hit/miss and metamethod calls;
4. inspect allocation/GC;
5. inspect helper and side-exit counts;
6. fix the general mechanism responsible.

No one-off row patch is accepted.

## Slice 10.6 - Final gate and cleanup

- run the full nine-pair parity harness three independent times;
- require the final median/p90/max contract each time;
- run correctness, race, pure-Go, checkptr, fuzz-smoke, and long-run soak gates;
- delete temporary A/B flags, legacy loops, old representations, counters, and migration adapters;
- update compatibility/performance docs;
- retire superseded execution plans.

---

## Workstream Ownership and Merge Order

### Measurement and conformance lane

Owns Phase 0, differential tests, ratio scripts, profile retention, size/allocation gates, and final acceptance. It never implements the optimization it evaluates.

### VM/wordcode lane

Owns Phases 1-3: production loop, wordcode, verifier, runtime function instances, cache indexing, and immutable Proto.

### Value/heap lane

Owns Phase 4 and collector foundations. It defines the `slot`/handle contract before call or table migrations begin.

### Call/coroutine lane

Owns Phase 5 after the slot and runtime-instance contracts are frozen.

### Table/IC lane

Owns Phase 6 after symbols, slots, and in-loop calls are available. Metamethod behavior is jointly reviewed with the call lane.

### Builtin/host lane

Owns Phase 7 and cannot bypass the internal slot/call ABI through public `[]Value` APIs.

### Compiler lane

Can build metrics during Phases 1-7, but executable wordcode transforms land only after the wordcode contract is stable. It owns Phase 8.

### Adaptive/runtime lane

Owns Phase 9 only after immutable Proto and runtime function instances are proven. It may not mutate shared code.

### Merge order

1. Phase 0 gates.
2. Phase 1 loop cleanup.
3. Phase 2 wordcode.
4. Phase 3 runtime ownership.
5. Phase 4 slot/heap contract.
6. Phase 5 calls and Phase 6 table groundwork may proceed in parallel only after shared interfaces are frozen; call cutover lands before function-valued metamethod table cutover.
7. Phase 7.
8. Phase 8.
9. Phase 9.
10. Phase 10.

Do not merge a half-converted runtime where some hot paths use `Value` and others use `slot` indefinitely. Migration adapters are temporary and tracked for deletion.

---

## Per-Slice Review Template

Every slice report includes:

1. **Hypothesis:** profile cost being removed.
2. **Red tracer:** failing test/budget before implementation.
3. **Changed mechanism:** old path deleted and new path installed.
4. **Correctness:** focused, differential, race/checkptr as applicable.
5. **Focused performance:** affected markers and allocation results.
6. **Full-corpus movement:** median, p90, max, geomean, and regressions.
7. **Profile proof:** named cost removed or reduced.
8. **Complexity ledger:** opcodes, side tables, code lines, bytes, caches.
9. **Risks and rollback:** exact condition under which the slice is reverted.

A slice is rejected when:

- the full-corpus geometric mean improves less than 2-3% and it is not a required architectural prerequisite;
- any row regresses over the approved noise budget without a documented following slice that removes the temporary cost;
- it creates a permanent dual path;
- it relies on a benchmark name or static source pattern;
- it increases hot-path allocation, pointer scanning, or cache footprint without compensating evidence.

---

## Row-to-Phase Attribution

Use this to stop engineers from attacking the wrong layer:

- **Arithmetic/iterative loops:** Phases 1, 2, 4, 8, 9.
- **Recursive Fibonacci, method calls, closures, signal callbacks, vararg router:** Phases 5, 7, 8, 9.
- **Prototype fallback and dirty metatables:** Phases 5 and 6.
- **Formation/economy/sparse-grid/table churn:** Phases 4, 6, 8, 10.
- **Builtin-heavy state/component/array rows:** Phase 7, then Phase 8.
- **Every row:** wordcode, internal slots, compiler instruction reduction, and final quickening.

Expected gains overlap; never multiply headline microbenchmark wins into a fantasy total. The only accepted total is the paired full-corpus gate.

---

## Explicitly Forbidden Shortcuts

- bringing back Scenario-named plans, row executors, or field-update fusions;
- hiding Go pointers in `uint64` values;
- storing mutable ICs or quickened words in shared `Proto`;
- allocating a 4-way cache for every instruction PC;
- using `interface{}`, reflection, or error-string construction inside opcode handlers;
- calling script functions recursively through Go;
- using public `HostFunc` slice transport for internal builtins;
- claiming victory at `2.0x` when the target is parity;
- updating the comparison Luau build during the campaign because Ember is getting close;
- accepting a speedup that disappears under the nine-pair slope harness.

---

## First Three Implementation Slices

The correct immediate queue is:

1. **Delete/quarantine guarded-block execution after same-binary A/B, then remove its hot-loop branches and side table.**
2. **Split the production loop and eliminate `packedInstruction.unpack`, keeping PC/code/register state in locals.**
3. **Approve and implement immutable 32-bit wordcode plus runtime-owned cache descriptors.**

Do not start NaN/handle slots, custom GC, or compiler superinstructions before those three slices make the baseline honest. Do not spend another cycle expanding the current 520-byte PIC or borrowed-window frame machinery; both are scheduled for replacement.
