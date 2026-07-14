# Ember Runtime Within 2x of Luau: Production-First No-CGO Migration

Status: implementation plan; proof cleanup completed with this plan, runtime
implementation not yet begun

Created: 2026-07-14

Target acceptance platform: Darwin 24.6.0, Apple M1, arm64, Go 1.26.4,
`CGO_ENABLED=0`, `GOMAXPROCS=1`, pinned Luau 0.728 at SHA-256
`c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd`,
Luau's interpreter at its default optimization level, and no Luau codegen.

This plan replaces the abandoned standalone proof and the proof-dependent
runtime architecture plan. It migrates the real product in vertical slices.
There is no synthetic owner, synthetic effect engine, proof prerequisite, or
throwaway alternate runtime.

## Decision

Replace Ember's pointer-rich `Value`/`vmFrame` execution core with two deep
modules inside the existing flat root package:

1. an immutable, owner-neutral `CodeImage` prepared once from verified
   `Program`/`Proto` artifacts and shareable by every runtime owner; and
2. an owner-bound `Machine` whose hot stacks, continuations, tables, closures,
   upvalues, globals, caches, and coroutine snapshots use compact scalar
   records and 64-bit slots.

The first complete backend is a generated pure-Go production kernel over that
state. It runs real `Run`, `Runtime.RunHook`, module loading, callbacks,
coroutines, limits, cancellation, errors, and host effects. A static
Darwin/arm64 Go-assembly burst backend may implement the same bounded
`RunBurst` seam later, but only if profiles of the complete production path
show that it delivers broad incremental gain. It is ordinary Go toolchain
assembly, not CGO or a foreign runtime.

Migration is whole-program and owner-local. During the transition, one
invocation uses either the old VM or the new machine from entry to exit. It
never converts mutable state mid-execution, mirrors tables in both engines, or
falls back per opcode. The old VM is only a temporary semantic oracle and is
deleted after the new machine owns every production entrypoint.

This is a deliberate architecture migration, not another 15% campaign. The
compact machine attacks the costs that recur across all language programs:
register traffic, kind checks, call/return state, instruction accounting,
pointer chasing, global lookup, table representation, property caches, and
iteration bookkeeping. General quickening, block execution, and static
assembly remain available after the state model is correct; benchmark names,
source fingerprints, or case-specific opcode paths are forbidden.

## Honest confidence and decision policy

The architecture is worth migrating to even if the first pure-Go cutover does
not immediately reach 2x. It replaces the current mixed `Value`/slot design
with one coherent execution state, removes broad structural overhead, and
creates one narrow backend seam. It does not depend on luck or on a synthetic
proof translating into production.

The final speed result is still empirical. Before implementation, confidence
that the compact pure-Go machine alone reaches every-row 2x is moderate, not
certain. Confidence that the full architecture can deliver at least a broad
30% speedup is materially higher because it simultaneously halves register
traffic, flattens calls, removes unconditional per-op controller work, and
replaces pointer-rich table state. Static assembly and prepared-bundle AOT are
explicit escalation paths rather than assumed wins.

Do not stop the migration merely because an early mixed slice shows less than
30%. Mixed `Value`/slot bridges are expected to understate the final design.
Do stop or delete an individual accelerator when a complete, clean,
representative production measurement shows that the mechanism does not pay
for its complexity.

## Hard constraints

### No CGO, no disguised foreign backend

The repository must continue to pass and strengthen `scripts/check-purego`.
The product path permits Go and checked-in or reproducibly generated Go
assembly only. It forbids:

- `import "C"`, CgoFiles, C/C++/Objective-C sources, cgo-generated shims, or
  foreign object/archive linkage;
- dynamic-loader or foreign-function bridges used to bypass cgo;
- embedding, linking, mechanically porting, or subprocess-hosting upstream
  Luau as Ember's execution backend;
- a helper process, `go tool` invocation, plugin, WebAssembly runtime, or
  external compiler in the runtime path;
- runtime-generated machine code, JIT entitlements, executable-memory
  mappings, or writable/executable pages;
- private `ABIInternal`, `go:linkname`, or calls from Go assembly into foreign
  symbols;
- new dependencies without explicit user approval.

The pinned Luau binary remains a test and measurement oracle only. It never
ships in or services Ember execution.

### Generality

Runtime and generator code must never branch on source text, source hash,
chunk name, fixture name, benchmark name, corpus membership, known constants,
known loop counts, or Proto IDs copied from acceptance workloads.

Allowed specialization keys are semantic and runtime-derived:

- opcode and verified operand form;
- scalar tag combination;
- table shape ID and version;
- property/global descriptor state;
- call target class, arity, vararg, and result mode;
- metatable chain version;
- effect and safepoint class;
- measured generic block shape emitted by a deterministic generator.

Any quickened operation must have a generic guard, a correct deoptimization
path within the same machine, and tests generated independently of benchmark
case names. The 37 acceptance workloads are an oracle, not a dispatch table.

### Speed target

The final product gate covers every workload in the ten Top10, two Classic,
and twenty-five Scenario inventories. Canonical IDs are
`top10/<name>`, `classic/<name>`, and `scenario/<name>`; a bare case name is
never sufficient identity. Each clean capture uses the same matched warmed
runtime contract:

- Ember compiles, prepares the image, binds one owner, and obtains the callable
  before the timer; Luau constructs the equivalent callable before `os.clock`;
- the generated Luau fixture defines `local __case = function() ... end` and
  `local __sink` before `local __start = os.clock()`, executes only
  `for __i = 1, N do __sink = __case() end` before reading the stop clock, and
  prints elapsed/result after the stop. The Ember fixture performs the same N
  calls into a prebound callable between `time.Now` and `time.Since` and reads
  or validates the sink only after the stop;
- timed work is N calls of that same callable plus assignment to an equivalent
  scalar result sink; compilation, process startup, image preparation, Machine
  binding, result validation, and teardown are excluded on both sides and
  measured in separate lifecycle lanes;
- each row uses N=`{1,10,100,1000}` and three independent repeats per engine;
  each repeat fits `elapsed = intercept + slope*N` by ordinary least squares
  over all four points and requires a finite positive slope;
- the three Ember slopes and three Luau slopes form nine pair ratios. Sort
  them; the fifth value is the median and the ninth value is nearest-rank p90;
- raw artifacts retain corpus/name, repeat, acquisition order, engine, N,
  elapsed time, result, slope, contamination probes, and environment.

Two independently acquired clean captures must each pass every row; ratios are
not merged and the better capture cannot hide the worse one:

- every row's median paired Ember/Luau runtime ratio is at most `1.85`,
  providing engineering margin;
- every row's nearest-rank p90 paired ratio is at most `2.00`;
- no row may be omitted, renamed, routed to a benchmark-only backend, or
  waived by geomean;
- candidate/current-Ember uses the same slope/cartesian-pair math, pairing
  candidate capture A with baseline A and B with baseline B. Every row's
  nine-ratio median must be at most `1.05` in both comparisons at an
  intermediate production cutover; a faster or slower external Luau build
  cannot hide an Ember regression;
- public `Compile`, `CompileWithOptions`, `LoadProgram` with `ModuleLoader`,
  `Run`, `RunWithGlobals`, `Program.NewRuntime`, `Runtime.RunHook`/`Close`,
  `RuntimeHost`, module-loader/host/callback/coroutine boundaries, preparation,
  binding, detach, and teardown are separately reported lifecycle lanes and
  cannot be substituted into or silently omitted from the warmed target.

During migration, a retained production slice must be semantically complete
for its declared feature set, improve or unlock the architecture, and cause no
more than a 5% regression on any already-supported acceptance row. A slice is
not rejected solely because the whole suite has not yet gained 30%; the hard
2x gate applies after the complete machine and real effects are cut over.

### Allocations and memory

Speed is the top priority. Allocation reduction is not a separate campaign,
but speed may not be purchased with recurring garbage:

- warmed B/op and allocs/op may not exceed the greater of two clean baseline
  captures for any acceptance row;
- no allocation may recur per guest instruction, internal script call, loop
  iteration, stable global/property/table hit, or warmed coroutine resume;
- edge adapters import or export values in batches and may not run per opcode;
- one-time image preparation and machine binding are measured separately;
- arena capacity, retained bytes, and root-store bytes are reported, but their
  absolute minimization is deferred unless they grow without bound or cause
  GC work in the timed path;
- any temporary dual representation must be outside the timed production path
  and deleted at its phase cutover.

### Compatibility policy

Go API compatibility, private bytecode compatibility, and internal object
identity may break. Stale private artifacts may require recompilation.

Luau-visible semantics remain required: values, errors, control flow, calls,
varargs, multiple results, tables, metatables, iteration, modules, callbacks,
coroutines, cancellation, exact instruction limits, deterministic behavior,
and runtime-owner isolation. Any intentional Luau-visible change requires a
separate explicit decision and compatibility update; it cannot be hidden as a
performance migration.

## Repository evidence

The current runtime is structurally expensive in broad ways:

- `Value` in `value.go` is 16 bytes and pointer-bearing; the private 8-byte
  `slot` in `slot.go` is not the canonical execution representation.
- `vmThread` registers remain `[]Value`, while `vmFrame` and
  `vmFrameRecord` in `vm.go` retain pointer-rich closure and Proto state.
- script calls repeatedly leave and re-enter the main loop through frame
  setup, resume, result application, and register clearing.
- `vm_dispatch_generated.go`, produced from
  `vm_dispatch_template.go.tmpl`, executes a large Go switch and charges
  `executionWindow.stepInstruction` at every guest instruction.
- `Table` in `value.go` owns pointer-bearing arrays, hash state, shapes,
  iteration journals, and several mutation/version counters.
- `runtimeHeap` resolves compact handles back into pointer slabs; adding slot
  handles without replacing the mutable object graph would add lookup and
  generation checks while preserving the original pointer chase.
- current profiles attribute substantial cost to the generated loop,
  call-entry/resume paths, `executionWindow`, `valueKind`, table hash growth,
  property access, and iteration bookkeeping.
- prior narrow alternate engines were fast on eligible micro-families but
  covered no Scenario rows; this plan therefore routes whole semantic
  programs, not selected opcode cases.

There are user-owned in-progress edits in `base_coroutine.go`, `base_env.go`,
`callback.go`, `module_runtime.go`, `program.go`, `runtime_call.go`,
`runtime_heap.go`, and `vm.go`. They improve invocation/runtime behavior and
overlap future seams. An executor must preserve them and begin Phase 0 only
after they are landed or deliberately reconciled. Do not revert, overwrite,
or silently absorb them into an acceptance baseline.

## Target architecture

```text
source / loaded Program / verified Proto graph
                    |
                    v
          immutable CodeImage preparation
          - owner-neutral constants/descriptors
          - compact operations plus side descriptors
          - blocks, PC maps, liveness, safepoints
          - no slots, roots, host values, or caches
                    |
             shared by many owners
                    |
                    v
              Machine binding
          - owner cookie and lifecycle
          - []slot registers/value stack
          - flat scalar continuations
          - scalar object/table/string arenas
          - dense globals/modules/caches
          - Go root/payload sidecar
                    |
                    v
       generated pure-Go RunBurst (canonical)
                    |
             bounded Exit record
                    |
                    v
             Go effect executor
       host/module/growth/error/yield/cancel/GC
                    |
          optional measured backends
       arm64 RunBurst | prepared-bundle Go AOT
```

### `CodeImage`: immutable and owner-neutral

`CodeImage` is prepared once from the complete verified Proto graph and may be
shared concurrently by multiple `Runtime` owners. It contains only immutable
data:

- executable Proto IDs and compact operation streams;
- guest wordcode PC to image PC and source/debug maps;
- basic-block boundaries, exact guest-instruction counts, and safepoints;
- register count, liveness/clear ranges, call/arity/result/vararg descriptors;
- constant descriptors containing only number bits, booleans, nil, and
  immutable string text/string-table IDs;
- global, property, metatable, table, and iterator descriptor identities;
- effect classification and generic quickening eligibility;
- a format version and content hash used for validation and stale-artifact
  rejection.

It must not contain owner slots, owner IDs, mutable inline caches, Go host
values, root pointers, module load state, coroutine state, or table shapes.
Owner-specific constant handles and caches are created once when a Machine is
bound.

Executable images reject pointer-bearing reference constants in `Proto`.
Compiler-produced Luau constants already fit the scalar/string set; opaque
host objects enter through explicit globals/arguments/effects. This deliberate
private-bytecode break closes the otherwise impossible ownership question of a
shareable image retaining a mutable Go `Value`. If a future bytecode API needs
reference constants, it requires a separately designed prepared-binding
sidecar with explicit copy/share semantics; this migration does not invent one.

Keep current 4-byte wordcode or another compact primary encoding until cache
profiles justify widening. Dense side arrays may predecode expensive operands.
Do not assume 16- or 32-byte semantic words are faster; instruction-cache
growth is a measured tradeoff.

### `Machine`: one owner-bound mutable world

Each Runtime binds a `CodeImage` to one `Machine`. The Machine owns:

- the owner cookie, open/busy/closed state, pin/lease count, and collection
  epoch;
- pointer-free-element `[]slot` registers, arguments, results, varargs, and
  scratch values;
- compact continuation records with integer Proto, PC, base, top, expected
  result, return PC, and flags fields;
- scalar closure, upvalue, cell, table, string, userdata-handle, module,
  global, descriptor-cache, coroutine, and error records;
- dense owner-local arrays for module exports, entrypoint state, globals,
  property caches, metatable chains, and quickened operation state;
- a Go-visible root/payload store for host functions, userdata, detached
  public values, leases, errors, and opaque host-owned values;
- allocator cursors, free/retired lists, collection marks, and diagnostic
  counters.

Hot internal references are dense owner-local indices. Production execution
does not load and compare a generation on every object dereference. Owner and
generation validation occurs when a value crosses a public, callback,
coroutine, import/export, or effect seam. Exact tracing at a quiescent Machine
point proves which internal indices are unreachable. An unmarked, unpinned
entry moves `live -> retired`, increments its boundary generation, clears all
scalar/root fields, and only then moves `retired -> reusable`; a long-lived
callback/result pins only its reachable graph, not every object ID in the
Runtime. Internal references need no generation check because collection
cannot free a reachable entry. Boundary handles carry owner cookie, index, and
generation and fail closed after reuse. The owner epoch changes only for a
whole-Machine reset/close. Debug tests may enable a mode that validates every
internal dereference.

The top-level Machine may contain typed Go slice pointers. The elements
mutated by a future assembly backend must contain no Go pointers. All pointer
payloads remain in the Go-owned root store and are accessed only by Go effects.

### Tables and strings

The table representation is part of the core migration, not a later memory
cleanup:

- a table directory maps dense table IDs to array/hash/shape/metatable spans;
- array values are contiguous slots;
- stable string properties use scalar shape IDs and offsets;
- the hash uses ordered dense entries plus compact control/bucket indices;
- deterministic iteration is represented once in the entry layout or by
  compact order links, never by a second `map[tableKey]int` journal;
- existing-key writes, in-capacity inserts, bounded probes, and in-capacity
  bump allocation remain inside the kernel;
- growth, rehash, compaction, long string work, and opaque comparison exit to
  Go;
- owner-local strings are copied/interned into a pointer-free Machine byte
  arena and referenced by scalar records `{offset, length, hash}`; equality on
  interned strings is an ID comparison, while hash/length and bounded byte
  comparison can remain in the kernel;
- concatenation, ordering, pattern work, or byte scans beyond the primitive
  work budget return to Go and resume on the same Machine. No table probe needs
  a Go string pointer.

Before selecting physical bucket order or explicit order links, tests must
freeze Ember's documented deterministic insertion-order behavior from
`docs/compatibility.md` and the old VM. Luau raw order is not portable, so use
the Luau oracle only where order is semantically comparable. Do not preserve
additional accidental current ordering merely because the old Table happened
to produce it.

### Execution and effects

The canonical pure-Go kernel operates only on `CodeImage` and `Machine` state.
One generated operation specification owns opcode identity, operands,
dataflow/clear sets, guest accounting, PC/error rules, effects, and quickening
metadata. Readable Go helpers remain the canonical semantic implementation;
the generated Go dispatch calls them. Assembly/AOT handlers may independently
implement a restricted measured subset using the same metadata, but generated
transition vectors must prove them against the Go helpers. Do not promise that
arbitrary Go helper bodies can be mechanically emitted as assembly.

`RunBurst` retires a bounded amount of work and returns one compact
`burstReturn` with disjoint classes:

- **terminal:** completion/error after all committed guest work is charged;
- **semantic effect:** host/native call, module transition, coroutine
  transition, opaque payload work, or semantic allocation requiring Go;
- **policy safepoint:** cancellation, budget, collector, scheduler, or debug
  polling between fully committed operations;
- **backend continuation:** arena growth/rehash retry, assembly unsupported
  operation, guard miss, or generic/deoptimization continuation on the same
  Machine.

For terminal and policy returns, every prior operation is fully mutated and
charged and the saved PC names the next operation. A semantic effect stages
validated scalar operands without externally visible mutation or charge; Go
commits the effect exactly once, applies its declared guest charge, and resumes
at the recorded next PC. A growth retry or backend continuation performs no
semantic mutation or charge for the current operation and resumes that same PC
in the Go generic kernel after capacity/deoptimization work. Every return has
explicit `operationPC`, `resumePC`, pre/post-state, replay, and charge tests.
The pure-Go generic path remains in-kernel; backend continuation is not
reported or timed as a host effect.

Direct script calls, returns, tail calls, stable scripted metamethod targets,
stable global/property/table hits, bounded iteration, and in-capacity scalar
allocation do not exit. The effect executor is the only place that calls host
Go code or touches pointer-bearing payloads.

Instruction accounting is exact in guest-instruction units. CodeImage blocks
carry exact counts; the kernel charges whole safe blocks when possible and
returns before a block that would exceed the remaining limit. Cancellation
and scheduling poll at bounded safepoints. The quantum is measured across
small and large values and primitive-work budgets; `64` guest instructions is
an initial experiment, not an ABI.

### Public boundary and lifetime

The public boundary may break to keep compact state compact:

- `Program` remains immutable; image preparation is explicit or cached once
  per Program, never repeated per owner.
- finalized standalone `Proto` values cache one immutable nested-Proto image
  through private once state. Public `Run(*Proto)` and `RunWithGlobals` are
  cold convenience wrappers that reuse that image, bind an ephemeral Machine,
  batch-import globals/arguments, execute, detach results/write back globals,
  and close the Machine. They never use a process-global image cache.
- `Runtime` owns a Machine and binds host/module state once.
- arguments, host globals, host results, and detached `Run` results are batch
  converted at the edge.
- fast owner-bound results, table/function references, callbacks, and
  coroutines use explicit leases/views with `Close`; cross-owner use fails at
  the seam before any arena access.
- a deliberate detach/copy operation produces durable public `Value` data
  when ownership must outlive the Runtime.
- invocation capability is passed explicitly through the Machine/effect
  request. It is not recovered from hidden `context.Value` state inside the
  hot path.

Phase 1 freezes exact exported names and ownership tests before broad runtime
implementation. If batch conversion is below measurement noise, the familiar
`Run` and `RunHook` wrappers may remain as convenience edges. They must not
force `Value` back into the execution core.

## Migration invariants

1. One invocation has one mutable representation. No table, closure, upvalue,
   module export, or coroutine is live in both the old VM and Machine.
2. Routing is whole-Program and based on supported semantic features, never a
   benchmark or source identity. Unsupported Programs stay wholly on the old
   VM until the next vertical slice.
3. Differential tests execute the same source twice with separate owners and
   compare observable outcomes. They do not shadow-execute inside the timed
   path.
4. `Value`/slot adapters are batch boundary code only. A per-op or per-call
   adapter is a phase blocker.
5. Every production entrypoint reaches one private execution facade. `Run`,
   `Runtime.RunHook`, module execution, callback calls, and coroutine resumes
   cannot each grow independent engines.
6. The old VM remains unchanged enough to serve as an oracle, then becomes
   test-only, then is deleted. New features do not land only in the old VM.
7. Generated state has one metadata/dataflow/accounting source. Go helpers are
   canonical semantics; assembly/AOT implementations of a restricted subset
   are independent and must pass generated transition vectors. Descriptor and
   accounting behavior cannot be hand-copied into divergent tables.
8. Failed temporary adapters, layouts, and accelerators are deleted in the
   phase that rejects them; dead experiments do not accumulate.

## Verification contract

### Correctness oracles

Retain and extend:

- `execution_differential_test.go` for direct versus instrumented execution;
- `compatibility_manifest_test.go` and `docs/compatibility.md` for supported
  language surface;
- `runtime_parity_test.go` against pinned Luau;
- module, callback, coroutine, owner, GC, execution-limit, cancellation,
  error-stack, NaN, table-order, metatable, vararg, and multiple-result tests;
- `program_test.go`, `runtime_owner_*_test.go`,
  `callback_collector_test.go`, `runtime_call_test.go`,
  `runtime_budget_b8_test.go`, `runtime_budget_b9_test.go`,
  `runtime_error_c2_test.go`, `runtime_error_c3_test.go`, and `limits_test.go`
  as concrete lifecycle/limit inventories;
- generated source programs that combine semantic families and use only public
  compile/load/run surfaces;
- forced-GC, race, and checkptr runs around result/callback/coroutine lifetime.

The public edge matrix must explicitly cover:

- `Compile` and `CompileWithOptions` success, syntax/bytecode failure, option
  boundaries, cancellation/limits where applicable, and the exact point at
  which an owner-neutral image is prepared;
- `LoadProgram` with a deterministic `ModuleLoader`: success, missing module,
  loader error, cycle, repeated/cache load, cancellation/limits, and image
  reuse across `Program.NewRuntime` owners;
- `RunWithGlobals` writeback on success, script error, cancellation, every
  instruction/runtime-object limit, and host callback;
- retained table/closure identity, detach, use-after-close, and cross-owner
  rejection;
- `Runtime.Close` while idle, active/busy, and with result/callback/coroutine
  leases;
- callback capture/call/close/copy/context/re-entry and require/module use;
- module cycles, repeated load/cache, inherited frames, and failed module state;
- coroutine create/yield/resume/error/close, owner mismatch, and owner limits;
- deterministic insertion-order table mutation/iteration and metamethod event
  order;
- all seven `ExecutionLimits` fields: `MaxInstructions`, `MaxCallDepth`,
  `MaxModuleInitializations`, `MaxCoroutines`, `MaxGeneratedStringBytes`,
  `MaxTableEntriesPerTable`, and `MaxRuntimeObjects`.

Differential comparison includes result values, errors and script frames,
global/module/table state, iteration order, host event order, instruction
counts, cancellation point, yield/resume state, owner-close behavior, and
object identity where the public contract requires it.

Generated differential coverage is reproducible, not an informal held-out
promise:

- `runtime_machine_corpus_test.go` (new) contains a private stable SplitMix64
  grammar generator and `TestMachineGeneratedDifferentialCorpus`;
- `testdata/runtime-machine/corpus-v1.tsv` (new) records schema version,
  corpus set, seed, case count, generator/source aggregate SHA-256, and required
  semantic-family coverage. Its exact header is
  `schema_version<TAB>set<TAB>seed_hex<TAB>case_count<TAB>generator_sha256<TAB>source_sha256<TAB>required_families`;
  v1 pins development seed `0x454d42455201` plus stage-specific independent
  seeds: pure-Go `0x454d42455202`, quickening `0x454d42455203`, assembly
  `0x454d42455204`, AOT `0x454d42455205`, and final
  `0x454d42455206`. Generated sources stay in-memory unless a failing case is
  minimized;
- while the old oracle exists, the test runs each source through public
  compile/load surfaces and separate old/new owners, comparing the complete
  differential state/event schema;
- development cases run throughout implementation. A stage-specific holdout is
  not run or inspected while tuning its named mechanism; a Sol reviewer runs
  it once the mechanism is frozen. Its result cannot tune that mechanism, and
  no revealed holdout is reused to justify a later mechanism;
- `-machine-corpus-set=development|purego-holdout|quickening-holdout|assembly-holdout|aot-holdout|final-holdout|all`
  selects manifest rows and fails on an unknown/missing/duplicate set. The
  exact final command is
  `go test -run '^TestMachineGeneratedDifferentialCorpus$' -count=1 . -args -machine-corpus-set=all`.

Before the old oracle is deleted, Phase 9 freezes
`testdata/runtime-machine/corpus-v1-expected.jsonl`. Each canonical JSON line
contains schema/set/case/source hash plus normalized outcome, values, error and
frames, globals/modules/tables and iteration order, host events, instruction/
cancellation/yield state, owner-close state, and required identity relations.
The corpus test has an explicit pre-deletion old/new mode and a default
post-deletion expected-artifact mode; generic, quickened, assembly, and prepared
backends also compare against each other when retained. The aggregate expected
artifact SHA-256 is recorded in ADR 0007. Updating it is an explicit reviewed
command, never an automatic side effect of `go test`.

### Performance oracles

Phase 0 fixes the current inventory gap: `full` currently means only 25
Scenario cases. The harness must expose an explicit all-37 `speed2x` phase and
fail closed on missing or duplicate corpus-qualified IDs. It records
independent Ember, frozen-current-Ember, and Luau slope samples, not a single
aggregate. Existing parity raw fields remain the base, but `speed2x` freezes an
exact v1 artifact contract. `raw.tsv` has the header
`schema_version<TAB>capture_role<TAB>capture_pair<TAB>capture_id<TAB>source_commit<TAB>corpus<TAB>name<TAB>lifecycle<TAB>engine<TAB>repeat<TAB>acquisition_order<TAB>n_calls<TAB>elapsed_ns<TAB>result_sha256<TAB>callable_scope<TAB>environment_sha256<TAB>contaminated`.
`slopes.tsv` has
`schema_version<TAB>capture_role<TAB>capture_pair<TAB>capture_id<TAB>source_commit<TAB>corpus<TAB>name<TAB>lifecycle<TAB>engine<TAB>repeat<TAB>slope_ns_per_call<TAB>intercept_ns<TAB>result_sha256<TAB>callable_scope<TAB>environment_sha256`.
Roles are exactly `frozen-current` or `candidate`, pairs exactly `a` or `b`,
engine exactly `ember` or `luau`, lifecycle exactly `warm_call`, and
`callable_scope` exactly `warmed_callable_v1`. Nanoseconds and
nanoseconds/call are the only timing units. The gate validates role/pair/source
metadata from file contents, never directory names.

Measure two clean baselines before code migration. `--capture-only` preserves a
correct, uncontaminated baseline even when it fails the future speed threshold;
correctness or contamination still fails acquisition. Each capture uses a
caller-supplied output directory that must not exist, so repeating the same
fingerprint cannot silently reuse an earlier attempt. Each candidate decision
uses the exact source revision, command, toolchain, OS, CPU, Luau digest,
`CGO_ENABLED=0`, `GOMAXPROCS=1`, thermal/busy checks, and raw samples. Geomean
is informational for the final gate; it may be a local accelerator heuristic
but can never waive a failing row.

Allocations use a separate all-37/lifecycle artifact because parity elapsed
records do not contain reliable B/op or allocs/op. Phase 0 adds
`scripts/runtime-allocation-gate` with:

- `--capture --output DIR`, which runs the shared all-37 warmed benchmark plus
  `Compile`, `CompileWithOptions`, `LoadProgram`/ModuleLoader, stateless `Run`,
  `RunWithGlobals`, image preparation, ephemeral Machine bind,
  `Program.NewRuntime`, persistent Runtime/RunHook modes, module require,
  RuntimeHost call, Callback, coroutine resume, result detach, and close lanes;
- a TSV v1 schema containing corpus/name, lifecycle, B/op, allocs/op, image and
  Machine preparation metrics, arena/root retained bytes, command, and source/
  environment fingerprint;
- `--derive --baseline-a DIR --baseline-b DIR --output FILE`, which binds both
  capture hashes and freezes the greater metric for each exact row;
- `--compare --manifest FILE --candidate DIR`, which fails missing/extra rows
  and any warmed B/op or allocs/op above the frozen ceiling. Cold allocation
  count may not rise; cold retained bytes are reported and block only unbounded
  growth until a later explicit memory gate.

All acceptance capture tools call one `scripts/runtime-acceptance-env`
validator before measurement and store its `environment.tsv` plus SHA-256.
Its exact v1 header is
`schema_version<TAB>go_version<TAB>goos<TAB>goarch<TAB>os_build<TAB>cpu_model<TAB>cgo_enabled<TAB>gomaxprocs<TAB>luau_version<TAB>luau_sha256`;
non-Luau captures write `not-applicable` in both Luau fields.
The validator requires Darwin 24.6.0, Apple M1 arm64, Go 1.26.4,
`go env CGO_ENABLED` equal to `0`, environment and Go-runtime
`GOMAXPROCS=1`, and, for
parity/speed captures, the pinned Luau version and binary digest. It fails
instead of merely recording a mismatch. `scripts/check-runtime-parity`,
`scripts/runtime-allocation-gate`, and `scripts/performance-audit` must invoke
it; compare/derive commands require equal environment hashes.

Diagnostic counters are enabled in untimed or separately timed runs and cover:

- guest instructions and image blocks by semantic family;
- direct script calls, returns, tail calls, and result modes;
- global/property/table/iterator hits, misses, probe lengths, and growth;
- effects and side exits by reason;
- quickening installs, guard hits, misses, and reversions;
- import/export counts and values converted;
- coroutine/host/module transitions and safepoint returns.

These counters prove broad mechanism coverage and reveal an architecture that
looks fast only because work escaped into untimed setup.

### Required command ladder

Each slice runs its focused tests, then its owning lane. Each phase ends with:

```sh
scripts/check-lane root
scripts/check-fast
```

At plan creation, `scripts/check-fast` delegates to `scripts/check`; that
standard helper already runs the benchmark/parser self-tests, `go test`,
`scripts/check-purego`, and diff checks, while `check-purego` performs the
`CGO_ENABLED=0` build/test. Do not immediately rerun those same helpers unless
the slice specifically changes pure-Go policy or platform assembly. Re-read
`docs/checks.md` if helper contents change during this long migration.

Before a production cutover commit:

```sh
scripts/check
go vet ./...
go test -race -count=1 ./...
go test -gcflags=all=-d=checkptr=2 -count=1 ./...
```

`scripts/check` includes the standard pure-Go/build evidence. The explicit vet,
race, and checkptr commands are the additional cutover evidence.

The user did not request `scripts/check-full`; do not substitute it for the
explicit commands unless they later request it. Platform-specific assembly
phases additionally cross-build/test the portable path for supported OS/arch
targets and inspect the linked arm64 binary.

## Agent and commit protocol

Use the simple-loop execution model when this plan is run:

- **Luna workers** own bounded implementation/test slices with exact files.
- **Sol reviewers** challenge representation, lifecycle, semantic cutover,
  performance evidence, and assembly/AOT safety before each irreversible
  boundary.
- At most one worker edits a shared hot file such as `vm.go`, `program.go`,
  `value.go`, or a generator at a time. Parallel work is limited to disjoint
  tooling, tests, or docs.
- Workers are not alone in the worktree. They preserve unrelated edits and do
  not revert another worker's changes.
- Each slice is one reviewable commit after focused verification. Do not mix
  benchmark infrastructure, representation changes, and production cutover
  in one commit.
- Performance captures come from clean commits. Exploratory dirty captures are
  labeled and cannot pass a retention or completion gate.

## Phase overview

| Phase | Outcome | Production state |
| --- | --- | --- |
| 0 | Verify proof retirement, freeze gates, capture all-37 baseline | Old VM only |
| 1 | Freeze semantic spec, CodeImage, Machine ABI, public ownership | Old VM only |
| 2 | Run real scalar/control/call Programs on pure-Go Machine | Whole-Program dual route |
| 3 | Move closures, upvalues, strings, globals, and modules into Machine | Whole-Program dual route |
| 4 | Replace table, property, metatable, and iteration state | Whole-Program dual route |
| 5 | Complete effects, callbacks, coroutines, errors, limits, GC, results | New route covers full semantics |
| 6 | Make pure-Go Machine canonical and remove production old-VM routing | Machine only in production |
| 7 | Add only profile-proven quickening, layout, and arm64 burst gains | Machine with optional backend |
| 8 | Decide general prepared-bundle AOT only if product target still misses | Optional build-time tier |
| 9 | Freeze oracle, delete old VM/adapters, certify and document final binary | One production runtime |

## Phase 0 - Remove the proof and freeze product gates

### Slice 0.1 - Verify proof retirement and reconcile the starting tree

**Assigned agent:** Luna implementation worker; Sol reviews scope and retained
evidence.

**Objective:** Start from an attributable tree with one runtime migration plan
and no proof-only product machinery. The plan-creation commit already removes
the known proof artifacts and superseded runtime-speed plans; execution begins
by verifying that cleanup and resolving the separate in-progress Go edits.

**Files:**

- verify the abandoned proof plan, proof-only runtime/script/testdata files,
  proof-dependent architecture plan, and older superseded 2x plan remain
  deleted; Git history preserves them;
- keep `performance-optimization-implementation-plan.md` explicitly marked
  historical and keep repository navigation pointed only at this active
  runtime migration plan;
- classify the user-owned untracked `EMBER_IMPLEMENTATION_PLAN.md` and
  `performance-optimization-plan.md` before implementation. Do not edit or
  delete them without their owner's direction, and do not treat them as active
  runtime authority merely because they exist in the worktree;
- inspect, land, or deliberately reconcile current user-owned edits in
  `base_coroutine.go`, `base_env.go`, `callback.go`, `module_runtime.go`,
  `program.go`, `runtime_call.go`, `runtime_heap.go`, and `vm.go`;
- update `performance-audit.md` and `docs/adr/README.md` so neither references
  a proof prerequisite.

**Steps:**

1. Record `git status --short --branch`, `git diff --stat`, and the current
   commit.
2. Classify every dirty path as user work, proof-only work, or this plan.
3. If any proof-only artifact was reintroduced after this plan commit, remove
   only that artifact. Do not stage user-owned Go changes.
4. Resolve overlapping Go edits through normal review/commit before any clean
   baseline. If they are still in progress, pause implementation after the
   docs-only cleanup; do not manufacture a clean tree by stashing or reverting
   them without their owner.
5. Search the repository for stale proof-plan names, `PROCEED`, synthetic
   proof types, proof script paths, and navigation that still calls a competing
   runtime plan active.
6. Commit the cleanup/plan changes before any baseline. A dirty plan file or
   staged deletion is not completed cleanup. Baseline capture additionally
   waits for the separate user-owned Go edits to be landed or reconciled.

**Verification:**

```sh
rg -n 'runtime-speed-2x-no-cgo-proof|runtime-architecture-proof|architecture proof|PROCEED' . \
  --glob '!/.git/**' --glob '!runtime-speed-2x-no-cgo-production-migration-implementation-plan.md'
git diff --check
```

The search may find historical prose intentionally retained in ADR evidence;
each hit must be classified. It must not find an executable proof path or a
current prerequisite.

**Completion:** One committed current production migration plan remains;
proof-only code, scripts, tests, testdata, and ADR proposal are gone; tracked
navigation agrees; unrelated work is intact; the baseline tree is clean.

**Risk/dependency:** The current dirty Go edits overlap future execution seams.
They are a hard baseline dependency, not permission to delete or rewrite them.

### Slice 0.2 - Make `check-purego` the permanent no-CGO boundary

**Assigned agent:** Luna tooling worker. Sol reviews false-negative coverage.

**Objective:** Retain the useful proof boundary as general repository tooling,
without retaining a proof runtime.

At phase entry, the existing helper only performs `CGO_ENABLED=0` build/test,
reports CgoFiles, and scans `import "C"`. Dynamic-loader/JIT/helper-process,
foreign-assembly, object/archive, and linked-binary checks below are new work;
do not cite the current helper as if it already enforces them.

**Files and symbols:**

- `scripts/check-purego`: extend the existing build/test/import-C checks;
- `purego_boundary_test.go` (new structural test if shell checks become too
  brittle): repository policy fixtures and scanner tests;
- `testdata/purego/exec-allowlist-v1.tsv` (new): exact test/tool helper-process
  sites, executable class, owning symbol, and reason;
- `.github/workflows/ci.yml` and/or the existing workflow that owns pure-Go
  checks: invoke the strengthened helper on supported platforms;
- `docs/checks.md`: document the exact policy and command.

**Steps:**

1. Keep `CGO_ENABLED=0 go build ./...` and `CGO_ENABLED=0 go test ./...`.
2. Fail on Go files importing `C`, packages reporting `CgoFiles`, and product
   C/C++/Objective-C sources or foreign archives/objects.
3. Fail on product use of dynamic loading, executable-memory APIs,
   `plugin.Open`, helper-process execution backends, private `go:linkname`, and
   non-Go assembly call targets. Reject every `os/exec` use outside a parsed
   allowlist. The initial allowlist contains only `runtime_parity_test.go`
   measurement-oracle/fingerprint helpers (`parityCommandOutput`,
   `sampleParitySystem`, `measureParityLuau`),
   `top10_luau_benchmark_test.go:runTop10LuauScript`, and
   `vm_dispatch_structure_test.go:TestGeneratedDispatchMatchesSemanticSource`
   invoking `go run ./cmd/ember-vmgen -check`.
   Entries name the exact file, owning function, executable class
   (`pinned-luau`, `runner-fingerprint`, or `generator-check`), and purpose;
   source moves must update the reviewed manifest. No non-test runtime package
   file is allowlisted, and Luau execution is accepted only when the binary
   digest is validated by the parity harness.
4. If static arm64 assembly is later added, inspect its linked symbols and
   segment permissions rather than banning `.s` files.
5. Add self-test fixtures for allowed Go assembly and rejected CGO/foreign
   patterns. Scanner comments and string fixtures must not trigger as product
   code.

**Verification:**

```sh
sh -n scripts/check-purego
scripts/check-purego
go test -run 'TestPureGoBoundary' .
```

**Completion:** One general helper enforces no CGO/no foreign runtime for every
future phase. There is no proof-specific checker.

**Risk/dependency:** Avoid a giant regex policy with silent exclusions. Prefer
Go package metadata and linked-binary inspection where they are authoritative.

### Slice 0.3 - Freeze all 37 rows and add the `speed2x` phase

**Assigned agent:** Luna measurement worker. Sol independently audits case
identity, sample math, and fail-closed behavior.

**Objective:** Make the acceptance harness mean what the product target says.

**Files and symbols:**

- `runtime_parity_test.go`: `parityCaseSelection`, raw artifact schema,
  environment fingerprint, case inventory tests;
- `top10_luau_benchmark_test.go`: the ten Top10, two Classic, and twenty-five
  Scenario inventories and shared runner;
- `scripts/check-runtime-parity`: phase parsing, command capture, acquisition
  lifecycle;
- `scripts/runtime-acceptance-env` (new): fail-closed platform/toolchain/
  process/Luau fingerprint shared by every acceptance capture;
- replace/generalize `scripts/scenario-ratio-gate` as
  `scripts/runtime-ratio-gate`, accepting the frozen 37-row manifest, two
  independent capture directories, and optional paired current baselines;
- `scripts/runtime-allocation-gate` (new): all-37 warmed and public-lifecycle
  allocation/retained-state capture and comparison;
- `runtime_parity_test.go` or `top10_luau_benchmark_test.go`: add the shared
  `BenchmarkRuntimeSpeed2x/<corpus>/<name>/warm_call` allocation surface;
- `scripts/check-runtime-parity` self-test fixtures;
- `runtime_parity_timer_contract_test.go` (new): exact warmed callable/timer
  boundary fixtures for Ember and Luau;
- `.github/workflows/scheduled.yml`: run all-37 `full`, capture `speed2x`, and
  upload caller-named raw directories on the controlled M1;
- `docs/checks.md`: all-37 command and decision contract.

**Steps:**

1. Define one canonical case manifest keyed by corpus/name: 10 Top10 + 2
   Classic + 25 Scenario. Assert exact count 37, no duplicate qualified ID,
   no ambiguous bare-name lookup, complete source/want data, and stable source
   hashes.
2. Preserve smaller `dispatch`, `calls`, `data`, and `canaries` phases for
   iteration, but make `full` semantic correctness cover all 37.
3. Add `--phase speed2x` with the matched warmed callable scope and exact
   N=`{1,10,100,1000}`, three-repeat, OLS-slope, nine-cartesian-ratio math from
   the Speed target. The existing `Run(proto)` versus Luau `os.clock` timing is
   not the new target because its lifecycle boundaries are asymmetric.
4. Generate the Luau/Ember timer fixtures exactly as specified in the Speed
   target. Add a structural fixture test that locates start/stop markers and
   fails if compile, image preparation, binding, callable/function creation,
   process startup, validation, or teardown appears between them. A behavioral
   sentinel counts callable constructions and calls and asserts construction is
   zero and calls are exactly N inside each timed region.
5. Add `--capture-only`, `--capture-role frozen-current|candidate`,
   `--capture-pair a|b`, and `--output DIR`; DIR must not exist. Correctness,
   fingerprint, busy/thermal, and contamination still fail acquisition, while
   a ratio over target writes a valid baseline artifact without claiming PASS.
6. Make `scripts/runtime-ratio-gate` require two directories and independently
   gate all 37 rows in each. With `--baseline-a/--baseline-b`, compare
   candidate A to baseline A and B to B using nine slope ratios per row; each
   candidate/current median must be <=1.05. `--report-only` performs all schema,
   fingerprint, slope, and row validation but prints the baseline miss without
   enforcing future thresholds. Never merge or select attempts.
7. Add `--derive --baseline-a DIR --baseline-b DIR --output FILE` to the ratio
   gate. The immutable v1 manifest embeds both canonical directory SHA-256s,
   their `frozen-current` role/pair/source/environment metadata, and every
   fitted baseline row. Its exact header is
   `schema_version<TAB>capture_role<TAB>capture_pair<TAB>capture_sha256<TAB>capture_id<TAB>source_commit<TAB>environment_sha256<TAB>corpus<TAB>name<TAB>engine<TAB>repeat<TAB>slope_ns_per_call`.
   Later comparisons require the manifest plus both raw directories and verify
   their hashes before using them.
8. Keep allocation outside parity raw data. Add the allocation capture/compare
   schema and benchmark/lifecycle inventory defined in Performance oracles.
9. Make all three capture tools call `scripts/runtime-acceptance-env` before
   acquisition; store and hash its exact TSV. The shared helper and each caller
   get self-tests for wrong Go/OS/CPU/CGO/GOMAXPROCS/Luau values and unequal
   derive/compare fingerprints.
10. Fail on absent metrics, missing/extra/duplicate IDs, non-finite/nonpositive
   slopes, invalid OLS points, changed source/environment fingerprints, or
   unmatched lifecycle scopes.
11. Add shell self-tests for missing, duplicate, extra, malformed, over-2x,
   exactly-2x, reused output, one-pass/one-fail capture pairs, and known-failing
   capture-only input.
12. Replace every `scripts/scenario-ratio-gate` call in
    `scripts/check-runtime-parity` and `runtime_parity_test.go`, port its useful
    fixtures/self-tests, then delete the old 25-row script. An `rg` assertion
    prevents the old name or a Scenario-only gate from remaining reachable.
13. Update scheduled CI and artifact upload paths. A test asserts its `full`
   command reaches 37 qualified IDs; another asserts `speed2x` uses the exact
   timer and sample contract.

The frozen IDs at plan creation are:

- Top10: `arithmetic_for`, `while_branching`, `table_fields`, `array_ops`,
  `generic_iteration`, `closures_upvalues`, `method_calls`, `metatable_index`,
  `varargs_select`, `coroutine_yield`;
- Classic: `recursive_fibonacci`, `iterative_fibonacci`;
- Scenario: `combat_tick`, `inventory_value`, `event_dispatch`,
  `buff_stack_tick`, `ability_resolution`, `ai_utility_scoring`,
  `cooldown_scheduler`, `projectile_sweep`, `quest_progress_update`,
  `behavior_tree_tick`, `threat_aggro_table`, `economy_market_tick`,
  `formation_layout_score`, `dialogue_condition_eval`,
  `procgen_room_scoring`, `save_state_diff`, `path_relaxation`,
  `component_churn`, `prototype_fallback`, `signal_bus_callbacks`,
  `state_machine_transitions`, `sparse_grid_neighbors`,
  `dirty_metatable_writes`, `array_hole_compaction`, and
  `command_vararg_router`.

**Verification:**

```sh
go test -run 'TestRuntimeParity|TestRuntimeParityTimerContract|TestBenchmarkCaseValidation|TestTop10LuauBenchmarksMatchExpectedResults|TestClassicLuauBenchmarksMatchExpectedResults|TestScenarioLuauBenchmarksMatchExpectedResults' .
scripts/check-runtime-parity --self-test
scripts/runtime-ratio-gate --self-test
scripts/runtime-allocation-gate --self-test
scripts/runtime-acceptance-env --self-test
sh -n scripts/check-runtime-parity scripts/runtime-ratio-gate \
  scripts/runtime-allocation-gate scripts/runtime-acceptance-env
! rg -n 'scenario-ratio-gate|Scenario-only gate' \
  scripts/check-runtime-parity runtime_parity_test.go scripts
```

On the controlled runner:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase full --output /tmp/ember-parity-full
```

Do not require `speed2x` to pass baseline; baseline must capture its failure
honestly.

**Completion:** `full` proves all-37 correctness and `speed2x` reports all-37
performance with no waiver path.

**Risk/dependency:** Top10 and Scenario names may overlap while representing
the same source. Canonical identity must be based on the declared inventory,
not an accidental benchmark label.

### Slice 0.4 - Capture two clean baselines and a mechanism profile

**Assigned agent:** Luna measurement worker on the controlled M1. Sol reviews
the evidence before any threshold is frozen in the ADR.

**Objective:** Establish current Ember, Luau, allocation, and profile facts for
the exact tree from which migration starts.

**Files/artifacts:**

- `performance-audit.md`: concise durable findings, not raw log dumps;
- `scripts/performance-audit`: call the shared acceptance-environment validator
  and store its artifact/hash with profiles;
- `scripts/performance-audit-derive-manifest` and
  `scripts/performance-audit-compare`: bind independent A/B baseline roles and
  validate the selected role's capture hash;
- `docs/adr/0007-compact-production-machine.md` (new): accepted architecture,
  hard constraints, migration invariant, final target, and explicit
  supersession map for earlier runtime decisions;
- `docs/adr/README.md`: index ADR 0007;
- untracked or CI-uploaded capture directories under `tmp/`; never commit raw
  multi-megabyte profiles unless repository convention explicitly requires it.

**Steps:**

1. Run two independent clean performance-audit captures with profiles.
2. Run two caller-named all-37 speed captures and two allocation/lifecycle
   captures. Never reuse a completed directory.
3. Extend the compact performance manifest with distinct
   `baseline_a_capture_fingerprint` and `baseline_b_capture_fingerprint`.
   `performance-audit-compare --baseline-role a|b` must verify the matching
   fingerprint; it may not accept baseline B under baseline A's field or trust
   a directory name. Add A/B/mismatch self-tests.
4. Record per-row current Ember/Luau ratios and the speedup required to reach
   2.0, plus B/op and allocs/op ceilings.
5. Attribute CPU to generated loop, execution accounting, call enter/resume,
   kind checks, global/property/table paths, iteration, host effects, and GC.
6. Measure public paths separately: `Compile`, `CompileWithOptions`,
   `LoadProgram` with a deterministic `ModuleLoader`, stateless `Run`,
   `RunWithGlobals` including writeback, image preparation, ephemeral Machine
   bind/detach/close, `Program.NewRuntime`, persistent Runtime creation and
   RunHook, bounded/cancelable RunHook, RuntimeHost, callback, coroutine resume,
   module require, host boundary, and retained result close.
7. In ADR 0007, supersede the internal ordinary-Go-object/Go-GC default from
   ADR 0001 while retaining Go ownership at host/public edges; supersede ADR
   0004's public source-compatibility, mandatory stateless result-promotion,
   typed pointer-slab, and hot handle-generation decisions where the new
   owner-bound view/scalar arena contract differs; and supersede ADR 0005's
   decision that direct `Value`/`vmFrame` wordcode execution is the permanent
   canonical engine. State that ADR 0006 remains valid evidence against an
   allocation-only table campaign, while Phase 4 replaces tables as part of
   the measured canonical execution architecture.
8. Update `docs/adr/README.md` and `docs/README.md` so the live decision set is
   not contradictory.
9. Freeze diagnostic counter names in ADR evidence. Generate the immutable
   speed-baseline manifest, record its SHA-256 and the two bound capture hashes
   in ADR evidence, and preserve the manifest beside CI artifacts. Do not copy
   dirty exploratory ratios into acceptance facts.

**Verification:**

```sh
CGO_ENABLED=0 GOMAXPROCS=1 \
  scripts/performance-audit --output /tmp/ember-compact-baseline-a --profiles
CGO_ENABLED=0 GOMAXPROCS=1 \
  scripts/performance-audit --output /tmp/ember-compact-baseline-b --profiles
scripts/performance-audit-derive-manifest \
  --before /tmp/ember-compact-baseline-a \
  --after /tmp/ember-compact-baseline-b \
  --output /tmp/ember-compact-gates.tsv
scripts/performance-audit-compare --self-test

CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x --capture-only \
  --capture-role frozen-current --capture-pair a \
  --output /tmp/ember-speed2x-baseline-a
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x --capture-only \
  --capture-role frozen-current --capture-pair b \
  --output /tmp/ember-speed2x-baseline-b
scripts/runtime-ratio-gate --derive \
  --baseline-a /tmp/ember-speed2x-baseline-a \
  --baseline-b /tmp/ember-speed2x-baseline-b \
  --output /tmp/ember-speed2x-baselines-v1.tsv
scripts/runtime-ratio-gate --report-only \
  --capture-a /tmp/ember-speed2x-baseline-a \
  --capture-b /tmp/ember-speed2x-baseline-b \
  --baseline-manifest /tmp/ember-speed2x-baselines-v1.tsv

CGO_ENABLED=0 GOMAXPROCS=1 scripts/runtime-allocation-gate --capture \
  --output /tmp/ember-runtime-alloc-baseline-a
CGO_ENABLED=0 GOMAXPROCS=1 scripts/runtime-allocation-gate --capture \
  --output /tmp/ember-runtime-alloc-baseline-b
scripts/runtime-allocation-gate --derive \
  --baseline-a /tmp/ember-runtime-alloc-baseline-a \
  --baseline-b /tmp/ember-runtime-alloc-baseline-b \
  --output /tmp/ember-runtime-alloc-ceilings.tsv
```

**Completion:** ADR 0007 is `Accepted`, the exact starting commit and environment
are attributable, all 37 current ratios are known, and allocation ceilings are
machine-readable.

**Risk/dependency:** If the controlled runner or pinned Luau differs, record a
diagnostic capture but do not freeze or compare it as acceptance evidence.

## Phase 1 - Freeze one semantic model, CodeImage, and Machine ABI

### Slice 1.1 - Replace duplicate dispatch knowledge with one operation spec

**Assigned agent:** Luna generator worker. Sol reviews semantic completeness and
generated-code size risk.

**Objective:** Make one deterministic specification the source of decode,
dataflow, accounting, effects, generated Go dispatch metadata, and later
backend transition vectors, while keeping semantic helpers readable Go.

**Files and symbols:**

- `vm_dispatch_spec.go`: expand from the current generation directive into the
  typed operation metadata contract or point it at a dedicated private spec;
- `vm_dispatch_template.go.tmpl`: reduce to generated scaffolding rather than
  hand-maintained duplicate semantics;
- `cmd/ember-vmgen/main.go`: emit metadata and current-loop artifacts from one
  spec during migration;
- `vm_dispatch_generated.go`: regenerated output;
- `vm_dispatch_structure_test.go`: coverage, stable generation, size, and
  forbidden duplicate-loop assertions;
- `bytecode.go`, `wordcode.go`, `function_analysis.go`: existing opcode,
  verified operand, CFG/liveness, and effect sources.

**Steps:**

1. Inventory every verified operation, auxiliary word, operand domain,
   register read/write/clear set, guest instruction charge, safepoint class,
   return class/pre-post rule, possible effect, error PC rule, and supported
   quickening/backend class.
2. Teach the generator to fail when any opcode lacks metadata or when the
   verifier and spec disagree about auxiliary words.
3. Generate the old VM loop without semantic change first; compare generated
   output and run the existing dispatch tests.
4. Emit compact metadata consumed by CodeImage lowering and deterministic
   single-operation transition vectors consumed by future Go/assembly/AOT
   differential tests. Do not attempt to encode arbitrary Go helper bodies in
   the spec and do not generate the new executor yet.
5. Add a generator `-check` or equivalent idempotence mode that renders to a
   temporary location and byte-compares tracked output. Do not use a plain
   `git diff` check in the same slice that intentionally changes output.
6. Record generated text bytes and build time. Set a regression threshold so
   future superinstructions cannot silently explode code size.

**Verification:**

```sh
go generate ./...
go run ./cmd/ember-vmgen -check
go test -run 'TestVMDispatch|TestOpcode|TestInstructionRegisterEffects|TestFunctionAnalysis' .
```

**Completion:** Every executable operation has one checked semantic metadata
entry, current behavior is unchanged, and generated artifacts are stable.

**Risk/dependency:** Do not attempt to express arbitrary Go bodies in a clever
DSL. The spec should name dataflow and semantic helpers; pure helpers remain
ordinary readable Go.

### Slice 1.2 - Add immutable owner-neutral CodeImage preparation

**Assigned agent:** Luna implementation worker owning new image files and the
small finalization seam in `bytecode.go`. Sol reviews immutability and owner
neutrality.

**Objective:** Lower a verified Proto graph once into compact executable
metadata without changing runtime routing.

**Planned files and symbols:**

- `runtime_image.go` (new): private `codeImage`, `imageProto`, operation stream,
  block, constant, call, property, and effect descriptors;
- `runtime_image_lower.go` (new): `prepareCodeImage`, Proto graph numbering,
  constant lowering, block construction, PC/source maps;
- `runtime_image_verify.go` (new): format and invariant validation;
- `runtime_image_test.go` (new): determinism, immutability, malformed Proto,
  rejected reference constants, owner-neutrality, and concurrent sharing;
- `bytecode.go`: `Proto` and `finalizeProtoExecutionArtifact` call the existing
  verifier; add private once/image/error state for standalone Proto execution;
  image preparation consumes only finalized artifacts;
- `program.go`: cache or explicitly retain one image per immutable Program,
  but do not bind owner state or route execution yet.

**Representation rules:**

- use compact primary operations plus dense side descriptors first;
- descriptor indices and Proto IDs are fixed-width integers with checked
  limits;
- strings in the image are immutable text/offset data, never owner handles;
- constant numbers preserve exact bits including signed zero and NaN behavior;
- blocks never split auxiliary-word instructions or error-PC semantics;
- source maps refer back to guest PCs, so errors stay compatible;
- the image has no maps or pointers in its hot operation stream; immutable Go
  strings/slices outside assembly-visible arrays are allowed.

**Steps:**

1. Number the complete nested Proto graph deterministically.
2. Lower constants, operations, blocks, and descriptors from finalized Proto
   data; reuse `function_analysis.go` liveness/CFG facts rather than recompute
   ad hoc analyses.
3. Validate register bounds, descriptor bounds, block counts, PC maps, and
   effect classifications.
4. Reject any pointer-bearing `Value` constant during finalization/preparation
   with a deterministic error. Cover table, closure, userdata, host callable,
   and boxed reference forms.
5. Prepare the same Program twice and byte-compare canonical image content.
6. Prepare one standalone finalized Proto concurrently, prove its once-cached
   nested graph is reused, and prove `Run`/`RunWithGlobals` bind independent
   ephemeral Machines without a global cache.
7. Bind no strings, roots, globals, shapes, or caches. A test creates two
   owners from one image and proves no mutable field is shared.
8. Measure image preparation, ephemeral Machine binding, globals writeback,
   result detach, and execution allocations separately.

**Verification:**

```sh
go test -run 'TestCodeImage|TestProto|TestWordcode|TestFunctionAnalysis' .
go test -race -run 'TestCodeImageConcurrentSharing' .
```

**Completion:** Every compiler-produced Program/Proto can prepare and validate
an immutable image; reference constants fail explicitly; standalone Proto and
Program image lifetimes are test-backed; no production call executes it yet;
one image is safely shareable across owners.

**Risk/dependency:** Tests or internal callers that hand-build reference
constants must move those values to explicit globals/arguments. Do not weaken
owner neutrality with a cold pointer descriptor.

### Slice 1.3 - Define Machine state, slot semantics, and root boundary

**Assigned agent:** Luna runtime-state worker. Sol reviews pointer layout,
stale-handle safety, and collector invariants.

**Objective:** Create the only mutable state representation the new executor
will use.

**Planned files and symbols:**

- `runtime_machine.go` (new): `machine`, owner/image identity, lifecycle, arena
  directories, reset/bind/dispose;
- `runtime_machine_slot.go` (new or a deliberate replacement of `slot.go`):
  canonical slot tags and scalar payloads;
- `runtime_machine_stack.go` (new): register/value stack and compact
  continuation records;
- `runtime_machine_roots.go` (new): pointer-bearing root/payload store and
  boundary handles;
- `runtime_machine_gc.go` (new): mark worklist over scalar arenas and
  quiescent reuse policy;
- `runtime_owner.go`, `runtime_heap.go`: temporary bridge only at owner/boundary
  seams; do not put old slabs under new hot slots;
- `runtime_machine_layout_test.go` (new): `unsafe.Sizeof`, pointer-closure,
  alignment, owner mismatch, stale handle, forced-GC, and close tests.

**Representation rules:**

- hot slots are 64-bit NaN-tagged or another measured 64-bit encoding;
- internal object payloads are dense indices; no per-dereference generation
  load in production;
- exported/retained handles carry owner cookie and generation/epoch outside
  the hot slot and validate before arena access;
- exact quiescent tracing reclaims unrelated unreachable entries even while a
  callback, coroutine, result lease, or public view pins another graph;
- each reusable entry increments a boundary-only generation before reuse;
  internal hot slots remain generation-free, while every external handle
  validates owner cookie/index/generation;
- mutable assembly-visible element types contain no Go pointers;
- arena growth occurs only in Go at a stopped burst and refreshes all base
  slices before execution resumes;
- collector scanning follows only declared slot/reference fields, with a test
  that fails when a new field is omitted.

**Steps:**

1. Freeze slot tag/payload encodings with round-trip tests for nil, booleans,
   all number bit patterns, short/interned strings, and every object class.
2. Define compact continuations and prove their exact size. Do not retain
   `*Proto`, `*closure`, `[]Value`, or `context.Context` in them.
3. Bind immediate nil/boolean/number image constants once per Machine. Defer
   string and object constant binding to their owning Phase 3 slices.
4. Implement root-store import/export for scalar and opaque public Values as a
   cold batch boundary, not an execution helper.
5. Implement owner close, busy, pin, result lease, exact trace, and the
   `live -> retired -> reusable` transition. Prove a long-lived result pins its
   graph while unrelated garbage is repeatedly reclaimed/reused safely.
6. Force Go GC between every import, bind, resize, burst stub, export, and close
   step to prove typed roots remain alive.
7. Add debug checked dereference mode for tests without enabling it in timed
   production builds.

**Verification:**

```sh
go test -run 'TestMachineSlot|TestMachineLayout|TestMachineOwner|TestMachineRoot|TestRuntimeOwner' .
go test -gcflags=all=-d=checkptr=2 -run 'TestMachine' .
go test -race -run 'TestMachineOwner|TestMachineRoot' .
```

**Completion:** A Machine can bind, import/export batches, grow at a stopped
state, survive forced GC, reject cross-owner/stale boundary handles, and close
without executing guest code.

**Risk/dependency:** Reusing existing `runtimeHeap` pointer slabs would preserve
the core cost and is not an acceptable shortcut. They may serve only as a
temporary import/export compatibility edge.

### Slice 1.4 - Freeze the public ownership and invocation contract

**Assigned agent:** Sol design/review agent first; Luna implements the accepted
API and behavior tests.

**Objective:** Prevent the compact core from being compromised later by
pointer-rich public lifetime assumptions.

**Files and symbols:**

- `program.go`: `Program.NewRuntime`, `Runtime`, `RuntimeOptions`,
  `Runtime.RunHook`, `Runtime.Close`, `RuntimeHost`, `HostCall`;
- `vm.go`: public `Run` compatibility edge;
- `callback.go`: capture/call/close contract;
- `value.go`: public value/table access contract at owner boundaries;
- `docs/public-surface.md`, `docs/design.md`, `docs/compatibility.md`;
- focused public tests in `program_test.go`, `callback_collector_test.go`, the
  existing `runtime_owner_*_test.go` files, new result-lifetime tests, and
  table API tests.

**Required decisions:**

1. Image preparation is once per immutable Program and once per finalized
   standalone Proto graph. Program Runtime binding happens in `NewRuntime` (or
   an explicit preflight API) before mutable module/global state exists;
   repeated `RunHook` never relowers. Stateless `Run`/`RunWithGlobals` reuse the
   Proto image but bind and close an ephemeral Machine per call.
2. Runtime owns one Machine. Runtime closure invalidates owner-bound views
   after outstanding runs are rejected or leases are released.
3. The primary fast result/callback/coroutine/table reference contract is
   owner-bound with explicit release. Detached public Values require an
   explicit batch copy.
4. Existing convenience `Run`/`RunHook` wrappers either return detached data
   or clearly own a result lease; they cannot leak a Machine silently.
5. Host globals and results use a batch builder/view boundary. If the existing
   `map[string]Value` interface is retained as a convenience adapter, it is
   outside the core and measured independently.
6. Invocation scope and require capability are explicit Machine/effect state;
   `context.Context` carries cancellation/deadline to the edge, not hidden
   runtime state through every script call.
7. Nested callback behavior remains explicitly serialized or is deliberately
   redesigned; it cannot accidentally change while moving state.

**Steps:**

1. Write public behavior tests first for close, busy, retained result,
   detached result, cross-owner values, callback copies, coroutine lifetime,
   and host re-entry.
2. Update docs with the chosen breaking changes and migration note.
3. Implement only ownership wrappers and adapters; production still runs the
   old VM in this slice.
4. Measure cold/warmed boundary allocation so later core gains are not hidden
   by per-call wrappers.

**Verification:**

```sh
go test -run 'TestProgram|TestRuntime|TestCallback|TestOwner|TestPublic|TestTable' .
go test -race -run 'TestRuntime.*Close|TestCallback|TestOwner' .
```

**Completion:** Public lifetime and ownership are test-backed before Machine
objects become visible; no future phase needs to reintroduce hot `Value`
storage to preserve an accidental API.

**Risk/dependency:** This is the highest compatibility break. Keep convenience
adapters only when they remain unambiguously at the edge and do not add
recurring allocations to the primary path.

## Phase 2 - First real pure-Go execution vertical

### Slice 2.1 - Add whole-Program eligibility routing and differential harness

**Assigned agent:** Luna integration worker. Sol reviews that routing is
semantic, whole-program, and absent from final timed code.

**Objective:** Route a small but real semantic subset through production APIs
without mixed execution state.

**Planned files and symbols:**

- `runtime_machine_route.go` (new): image feature summary and temporary
  whole-Program backend selection;
- `runtime_machine_differential_test.go` (new): independent-owner old/new runs
  and exact state comparison;
- `program.go`: one private execution facade reached from Program/Runtime;
- `vm.go`: keep `executeProtoWithInvocationScope` as old-oracle entry only;
- `runtime_parity_test.go`: opt-in machine route in test subprocesses.

**Steps:**

1. Derive eligibility from the complete CodeImage closure: every nested Proto,
   Program module, entrypoint, and declared semantic feature. No source names,
   constants, or benchmark identifiers enter the router. Dynamic host values
   are imported/effected generically and never trigger a later route switch.
2. For a persistent Runtime, select and pin old VM versus Machine in
   `Program.NewRuntime` (or its explicit preflight replacement) before module,
   global, callback, or coroutine state exists. That Runtime never changes
   backend. Stateless `Run`/`RunWithGlobals` may select once per ephemeral
   owner before importing globals.
3. Run old and new paths only in separate test invocations with separate
   owners; compare results, errors, state, events, and instruction count.
4. Add a structural test that the production route contains no per-op fallback
   or old-VM call after Machine execution starts.
5. Emit counters for number of eligible Programs and semantic reasons a Program
   remains old. These are diagnostics, not performance dispatch.

**Verification:**

```sh
go test -run 'TestMachineRoute|TestMachineDifferential' .
```

**Completion:** A Runtime or stateless owner selects the Machine for a declared
complete semantic subset before mutable state exists, and every future
invocation/callback/coroutine for that owner remains entirely in it.

**Risk/dependency:** Do not make the router a permanent compatibility layer.
Phase 6 deletes it after complete coverage.

### Slice 2.2 - Execute scalar registers, arithmetic, branches, and loops

**Assigned agent:** Luna kernel worker owning the new executor/generator files.
Sol reviews instruction counting, error PCs, and absence of per-op allocation.

**Objective:** Run complete scalar/control-flow Programs through the real
Machine and public entry surface.

**Planned files and symbols:**

- `runtime_machine_exec.go` (new): `runMachine`, burst loop, exit handling;
- `runtime_machine_dispatch_generated.go` (new): generated pure-Go operation
  kernel;
- `cmd/ember-vmgen/main.go` or a deliberately split generator command: emit
  the new kernel from the same operation spec;
- `runtime_machine_effect.go` (new): completion/error/budget/cancel effects
  needed by this slice;
- `execution_control.go`: block/safepoint accounting adapter, not a per-op call;
- `runtime_machine_exec_test.go` (new): public source-to-result tests;
- `runtime_machine_differential_test.go`: generated scalar/control corpus.

**Initial semantic set:** nil/boolean/number constants, register moves,
unary/binary scalar operations, comparisons, conditional/unconditional jumps,
numeric loops, returns, deterministic errors, instruction limits, and
cancellation safepoints. No table, closure, upvalue, host, module, or coroutine
state may be silently boxed back into old Values.

**Steps:**

1. Generate a single compact Go switch or measured block switch directly over
   `[]slot` and scalar Machine fields.
2. Keep frequently used PC/base/top/remaining-budget fields in locals and
   write them back only at exits/safepoints.
3. Charge exact block instruction counts; split at any operation that can
   error, exceed a budget, or poll cancellation.
4. Implement scalar errors with guest PC/source recovery through CodeImage.
5. Add public `Compile`/`Run` and Program/Runtime differential cases generated
   across operand tags, branch shapes, limit boundaries, NaNs, signed zero,
   and overflow behavior.
6. Assert zero recurring allocations for long warmed loops.

**Verification:**

```sh
go test -run 'TestMachine.*(Scalar|Arithmetic|Branch|Loop|Limit|Cancel|Error)|TestMachineDifferential' .
go test -bench 'BenchmarkRun(Arithmetic|WhileLoop)$' -benchmem -count=5 .
scripts/check-purego
```

**Completion:** Eligible real Programs execute scalar/control semantics through
the production Machine, match the old VM, and allocate nothing per loop or
instruction.

**Risk/dependency:** A second helper-heavy Go interpreter can regress like the
prior direct kernel. Keep state local, inspect profiles, and avoid widening the
operation stream without cache evidence.

### Slice 2.3 - Flatten direct calls, returns, varargs, and multiple results

**Assigned agent:** Luna call-engine worker. Sol reviews every call mode and
continuation invariant.

**Objective:** Remove recursive Go call bridges and pointer-rich frame state
for functions that do not yet capture heap upvalues.

**Files and symbols:**

- `runtime_machine_stack.go`: continuation push/pop, reserve, clear/liveness,
  vararg/result spans;
- `runtime_machine_dispatch_generated.go`: call, return, tail-call, vararg,
  result operations;
- `runtime_image.go`: call/arity/result/liveness descriptors;
- `runtime_machine_exec.go`: no recursive Go execution for script calls;
- old-oracle references in `vm.go`: `runUntilDepthResult`, frame enter/resume,
  result application tests;
- call-focused generated/differential tests.

**Steps:**

1. Lower direct Proto targets and noncapturing closures to scalar Proto IDs.
2. Push a compact continuation and jump within the same kernel for script
   calls; do not return to Go.
3. Implement fixed/open arguments, fixed/open results, varargs, register
   clearing from liveness descriptors, tail calls, and error stack capture.
4. Reserve continuation/value capacity in Go before the burst; use bounded
   in-capacity pushes inside the kernel and exit only for growth.
5. Generate cross-products of arity/result modes and recursion depth, including
   limits and errors at call boundaries.
6. Assert continuation records contain no Go pointers and warmed fixed calls
   allocate zero.

**Verification:**

```sh
go test -run 'TestMachine.*(Call|Return|Tail|Vararg|Result|Recurs)|TestMachineDifferential' .
go test -bench 'BenchmarkRun(FunctionCalls|RecursiveScriptCalls)$' -benchmem -count=5 .
```

**Completion:** Supported script calls remain inside one Machine burst loop;
no `vmFrame`, `*closure`, `[]Value`, or recursive Go executor appears on the
new hot path.

**Risk/dependency:** Error trace PCs and exact result-clearing behavior are
easy to shift. Differential tests must compare script frames and not only final
values.

### Phase 2 gate

Run the phase command ladder, the generated scalar/call corpus, and a clean
exploratory all-37 capture. Record mechanism coverage and gains, but do not
claim the final suite target while tables/effects still route whole Programs to
the old VM. Continue if the implementation is semantically sound, has no hot
bridge/allocation, and shows credible gains on every newly eligible general
family. Delete or redesign the kernel if it is slower after state-local tuning;
do not paper over a regression with benchmark routing.

## Phase 3 - Move closures, upvalues, strings, globals, and modules

### Slice 3.1 - Scalarize closures and open/closed upvalues

**Assigned agent:** Luna object-model worker. Sol reviews capture/close,
aliasing, recursion, and collection.

**Objective:** Support the complete closure/upvalue lifecycle without old heap
objects in Machine execution.

**Planned files and symbols:**

- `runtime_machine_object.go` (new): closure and upvalue/cell arenas;
- `runtime_machine_gc.go`: trace and retire closure/upvalue records;
- `runtime_machine_dispatch_generated.go`: closure creation, capture, get/set,
  close, call target resolution;
- `runtime_image.go`: capture descriptors and nested Proto references;
- closure/upvalue differential and forced-GC tests;
- old oracle types in `vm.go`/`value.go`: `closure`, `cell`, open-upvalue
  behavior, unchanged until Phase 9 deletion.

**Steps:**

1. Represent closures as Proto ID plus compact upvalue span and optional native
   root ID; represent open upvalues by stack index/epoch and closed values by
   slot.
2. Maintain open-upvalue ordering/indexing in scalar arrays; no Go map lookup
   on stable access.
3. Close captures on scope exit, tail call, coroutine suspension, and error
   exactly once.
4. Keep in-capacity closure/upvalue allocation inside the kernel; exit for
   arena growth only.
5. Differential-test sibling sharing, mutation, nested recursion, closed-over
   varargs, tail calls, errors, collection, and owner close.

**Verification:**

```sh
go test -run 'TestMachine.*(Closure|Upvalue|Capture)|TestMachineDifferential' .
go test -gcflags=all=-d=checkptr=2 -run 'TestMachine.*(Closure|Upvalue)' .
```

**Completion:** Every script function and upvalue in an eligible Program is a
Machine record; no old `*closure` or `*cell` is imported into execution.

### Slice 3.2 - Intern strings and bind constants once per owner

**Assigned agent:** Luna value-model worker. Sol reviews hashing, equality,
NaNs, long strings, and root lifetime.

**Objective:** Make stable string/global/property operations scalar and avoid
repeated public Value construction.

**Planned files and symbols:**

- `runtime_machine_string.go` (new): intern table, scalar string records,
  cached hash, pointer-free byte arena;
- `runtime_machine.go`: image constant binding;
- `runtime_machine_effect.go`: long/concatenated string allocation and growth;
- `runtime_machine_dispatch_generated.go`: scalar string equality and bounded
  operations;
- string and constant differential tests.

**Steps:**

1. Copy/bind image string constants into the Machine byte arena and owner-local
   IDs once at Machine creation; this completes the constant binding
   deliberately deferred in Slice 1.3.
2. Copy and intern dynamically created or host-imported strings through a Go
   effect with stable IDs, byte spans, and cached hashes. ID equality,
   hash/length access, and bounded byte comparison stay in the kernel; long
   comparison, concatenation, ordering, and pattern operations return to Go.
3. Preserve exact Lua string bytes and equality; do not normalize text.
4. Batch-import public strings/opaque constants at host boundaries.
5. Verify two Machines sharing an image never share owner handles or mutable
   intern caches.

**Verification:**

```sh
go test -run 'TestMachine.*(String|Constant|Owner)|TestMachineDifferential' .
```

**Completion:** Stable string comparisons and descriptor keys are scalar IDs;
pointer-bearing string data is touched only by Go effects/boundaries.

### Slice 3.3 - Replace global environments with dense owner arrays

**Assigned agent:** Luna globals worker owning global-machine files and the
single integration seam in `base_env.go`. Sol reviews versioning and host
snapshot behavior.

**Objective:** Eliminate map/string lookup from stable script global access.

**Planned files and symbols:**

- `runtime_machine_global.go` (new): image global IDs, owner values, versions,
  descriptor caches, host snapshot import;
- `runtime_machine_dispatch_generated.go`: get/set/import global operations;
- `base_env.go`, `program.go`, `runtime_call.go`: boundary construction and
  explicit invocation capability;
- global access, host-change, require, and owner tests.

**Steps:**

1. Assign global-name descriptor IDs in CodeImage and bind them to dense owner
   slots.
2. Batch-import each host snapshot once per public invocation. Reuse scalar
   owner handles for unchanged host Values where the public contract permits.
3. Perform stable script get/set by descriptor index and version guard. Misses
   resolve in Go and update scalar cache state.
4. Represent `require` as an explicit effect capability, not a native Value
   carrying hidden invocation context.
5. Preserve host/global precedence and mutation semantics through public
   differential tests.

**Verification:**

```sh
go test -run 'TestMachine.*Global|TestRuntime.*Global|TestRequire|TestMachineDifferential' .
go test -bench 'BenchmarkRunGlobalAccess|BenchmarkRuntimeLane.*RunHook|BenchmarkRuntimeRequire' -benchmem -count=5 .
```

**Completion:** Stable Machine global access has no Go map or string lookup;
host import occurs once at the invocation edge.

### Slice 3.4 - Move module graph and entrypoint state into the Machine

**Assigned agent:** Luna module worker owning `module_runtime.go` and module
Machine files. Sol reviews cycle/error/active-state behavior.

**Objective:** Execute real multi-module Programs without exporting state to
old `Value` maps.

**Planned files and symbols:**

- `runtime_machine_module.go` (new): dense module IDs, unloaded/loading/loaded/
  failed states, export slots, entrypoint IDs, require stack;
- `module_runtime.go`: route load/require through the common Machine effect;
- `program.go`: bind Program module IDs and entrypoints to the Machine;
- `runtime_call.go`: remove new-path `runtimeRequireAdapter` Value creation;
- module graph, lazy load, cycle, error frame, and repeated hook tests.

**Steps:**

1. Number Program modules deterministically in CodeImage and bind dense owner
   load/export state.
2. Execute module top levels inside the same Machine; require transitions do
   not instantiate another vmThread or convert exports through public Values.
3. Preserve lazy loading, active-cycle detection, module error wrapping,
   inherited script frames, and entrypoint order.
4. Keep host module results in the root store with scalar owner handles.
5. Differential-test multi-entrypoint Programs, diamond dependencies, repeated
   require, failed modules, and changing host globals.

**Verification:**

```sh
go test -run 'TestMachine.*Module|TestRuntime.*(Module|Entrypoint|Require)|TestLoadProgram|TestMachineDifferential' .
```

**Completion:** Eligible Program module/entrypoint state is entirely owner-bound
Machine data; no `map[moduleKey]Value` is consulted on its execution path.

### Phase 3 gate

Run the full phase ladder plus public multi-module differential tests and a
clean exploratory all-37 capture. Confirm zero recurring allocation for stable
script calls, globals, and warmed module hooks. Profile import/export and
effect counts; a high boundary count must be fixed before table work, not
accepted as temporary hot architecture.

## Phase 4 - Replace tables, properties, metatables, and iteration

### Slice 4.1 - Freeze table semantics and build scalar table arenas

**Assigned agent:** Sol first audits required table/order semantics; Luna then
owns the table representation files. A second Sol review covers probe and
mutation invariants before integration.

**Objective:** Replace the pointer-rich `Table` graph with one dense owner-local
representation that supports broad mutation without leaving the Machine.

**Planned files and symbols:**

- `runtime_machine_table.go` (new): table directory, array spans, dense hash
  entries, control/bucket arrays, mutation and resize requests;
- `runtime_machine_table_iter.go` (new if iteration would otherwise obscure the
  core): deterministic order links/cursors;
- `runtime_machine_gc.go`: trace table keys, values, metatables, and any order
  links;
- `runtime_machine_effect.go`: growth, rehash, compaction, and unbounded key
  operations;
- `value.go`: old `Table` remains only for old-oracle/public boundary during
  migration;
- `table_shape.go`, `table_ops.go`, `value.go`, and the existing
  `table_iteration_*_test.go` files: current semantics used as the behavior
  oracle, then superseded where appropriate in Phase 9;
- `runtime_machine_table_test.go` (new): layout, probe, growth, deletion,
  iteration, NaN/error, and differential cases.

**Required representation decision:**

Use a table directory indexed by owner-local table ID. Each record owns spans
in scalar arenas:

- contiguous array slots and logical length metadata;
- dense hash entries containing scalar key/value, cached hash/fingerprint,
  occupancy state, and compact iteration links if required;
- a control/bucket index array for bounded open-addressed or Swiss-style
  probing;
- scalar shape ID, metatable ID, mutation versions, and flags;
- allocator capacity sufficient for in-burst insertion until explicit growth.

Do not copy the current separate hash, iteration journal, iteration index map,
and shape pointer graph into scalar form one-for-one. First freeze
`docs/compatibility.md`'s deterministic insertion order and mutation behavior
against the old Ember VM/model; use Luau only for table behavior whose raw
order is portable. Then choose the smallest representation that produces the
documented Ember contract.

**Steps:**

1. Add focused cases for integer/noninteger keys, equal distinct string boxes,
   object keys, deletion/reinsertion, mixed array/hash migration, NaN
   rejection, signed zero, length boundaries, deterministic insertion order,
   mutation during iteration, and metatable event order. Use the compatibility
   model/old VM for Ember's stronger order contract and Luau where comparable.
2. Implement allocation from pre-reserved pointer-free arenas and a stopped-Go
   growth/rehash effect. New table creation and in-capacity entry insertion are
   kernel operations, not unconditional exits.
3. Implement get/set/delete/length with bounded probes. A probe cap exits to
   Go for rehash instead of spinning inside a nonpreemptible burst.
4. Encode required iteration order in the primary entry representation. Do not
   maintain a second Go map for order.
5. Implement exact table key equality/hash using scalar strings/numbers/object
   IDs and cold Go effects only for opaque host values.
6. Differential-fuzz long mutation sequences and force collection/growth
   between operations.

**Verification:**

```sh
go test -run 'TestMachineTable|TestTable|TestIteration|TestLength|TestMachineDifferential' .
go test -race -run 'TestMachineTable' .
go test -gcflags=all=-d=checkptr=2 -run 'TestMachineTable' .
go test -bench 'BenchmarkRun(ArrayLiteral|TableInsert|TableRemove|TableUnpack|RawLength|Iteration)$' -benchmem -count=5 .
```

**Completion:** Table-heavy eligible Programs use no old `*Table`, Go hash map,
or pointer-rich iteration journal in execution; stable warmed operations do not
allocate or exit.

**Risk/dependency:** Table semantics and growth are the largest correctness and
performance risk. Do not optimize only reads; mutation-heavy programs must
stay inside the kernel while capacity exists.

### Slice 4.2 - Add scalar shapes and property/global descriptors

**Assigned agent:** Luna property-cache worker. Sol reviews invalidation and
megamorphic behavior.

**Objective:** Turn common string property access into guarded scalar offset
loads/stores without pointer shape checks.

**Planned files and symbols:**

- `runtime_machine_shape.go` (new): scalar shapes, transitions, offsets, and
  invalidation versions;
- `runtime_machine_cache.go` (new): owner-bound property/global/metatable cache
  records keyed by CodeImage descriptor ID;
- `runtime_machine_dispatch_generated.go`: property get/set and descriptor
  hit/miss operations;
- `property_ic.go`, `table_shape.go`: old-oracle behavior and final deletion
  targets;
- property/shape transition and polymorphism tests.

**Steps:**

1. Bind CodeImage property names to owner string IDs and allocate cache records
   once per Machine.
2. Represent shapes by integer ID with immutable property offset tables and
   scalar transition keys. Keep mutable versions in owner arrays.
3. Implement monomorphic then small fixed polymorphic caches; a megamorphic
   descriptor uses the generic scalar table path rather than allocating an
   unbounded cache.
4. Guard on table shape/version, key ID, and metatable-chain version as
   required. Existing-key writes that do not change structure retain the
   correct fast path.
5. Resolve misses within the same Machine/Go effect and update caches without
   importing public Values.
6. Generate shape-transition, polymorphic, deletion, reinsertion, and
   metatable invalidation programs independently of benchmarks.

**Verification:**

```sh
go test -run 'TestMachine.*(Shape|Property|Cache)|TestProperty|TestMachineDifferential' .
go test -bench 'BenchmarkRun(StringFieldReads|TableFields)$' -benchmem -count=5 .
```

**Completion:** Stable property hits are scalar guard + offset operations;
cache misses preserve semantics and cannot grow unbounded state.

### Slice 4.3 - Complete metatables, method calls, and generic iteration

**Assigned agent:** Luna semantic integration worker. Sol reviews chain
versioning, recursive effects, and error behavior.

**Objective:** Cover the dynamic table semantics that previously forced broad
Programs back to the old VM.

**Files and symbols:**

- `runtime_machine_table.go`, `runtime_machine_shape.go`,
  `runtime_machine_cache.go`: metatable IDs and chain guards;
- `runtime_machine_dispatch_generated.go`: method lookup, scripted metamethod
  calls, raw operations, iterator operations;
- `runtime_machine_effect.go`: native/opaque metamethod calls and unbounded
  chains;
- old-oracle helpers in `vm.go`, `table_ops.go`, `value.go`, and metatable
  tests.

**Steps:**

1. Keep direct scripted metamethod targets inside the Machine call engine.
2. Guard stable metatable chains with scalar version records; invalidate all
   affected descriptors when a metatable or relevant property mutates.
3. Exit only for native/host targets, errors, unbounded chain work, or arena
   growth. A normal scripted `__index`, `__newindex`, arithmetic, comparison,
   length, or call target is not a Go round-trip.
4. Implement generic/numeric iteration from table cursor state that survives
   calls, yields where Luau permits them, and mutation semantics.
5. Differential-test chain cycles, missing handlers, handler errors, method
   self arguments, polymorphism, mutation invalidation, and iterator close.

**Verification:**

```sh
go test -run 'TestMachine.*(Metatable|Method|Iterator)|TestMetatable|TestMachineDifferential' .
go test -bench 'BenchmarkRun(MetatableIndex|MethodCalls|Iteration)$' -benchmem -count=5 .
```

**Completion:** Table/metatable/method/iteration semantic families can route
whole Programs to the Machine with no old representation.

### Phase 4 gate

Run the phase ladder, table mutation fuzz/differential tests, and forced-GC
checks. Use counters to require high stable-hit coverage and bounded probe/exit
rates across generated and acceptance programs. A representation that improves
read-only property benchmarks while causing frequent growth/insertion exits is
not accepted. Capture current/candidate performance and allocation for every
now-eligible row.

## Phase 5 - Complete real effects and lifecycle semantics

### Slice 5.1 - Make one explicit Go effect executor production-complete

**Assigned agent:** Luna effect worker. Sol reviews re-entry, root exposure,
panic/error conversion, and state restoration.

**Objective:** Centralize every operation that may touch Go pointers, host code,
unbounded work, or lifecycle policy.

**Planned files and symbols:**

- `runtime_machine_effect.go`: effect request/response union and dispatcher;
- `runtime_machine_exec.go`: stopped-state validation and resume;
- `runtime_machine_roots.go`: typed payload/root access;
- `runtime_call.go`, `base_env.go`, standard-library native function adapters:
  explicit host/native effects;
- `runtime_error.go`: Machine error state to public error conversion;
- `runtime_machine_effect_test.go` (new): every exit reason and resume rule.

**Effect rules:**

- every `burstReturn` records its terminal/effect/policy/backend class, exact
  operation/resume image and guest PCs, retired instruction count,
  stack/continuation depths, reason, and scalar operands;
- Go validates all indices before touching pointer-bearing payloads;
- effects may grow/reallocate arenas only while the burst is stopped;
- after growth, all Machine slice bases and cached spans are refreshed before
  resume;
- host/native calls receive batch argument views and write batch results;
- panics from host code follow the documented existing recovery policy;
- semantic effects stage before mutation/charge and Go commits them once at the
  next PC; growth/backend continuations stage before mutation/charge and retry
  the same PC in the Go generic kernel; policy returns occur between committed
  operations; partial mutation cannot be replayed accidentally.

**Steps:**

1. Enumerate and test every return class/reason/pre-post/resume combination;
   make unknown values fatal in tests and a deterministic internal error in
   production.
2. Add effect-level canaries around arena bounds and continuation depth.
3. Convert native standard-library operations incrementally, keeping their
   current observable semantics while eliminating hidden `globalEnv` lookup on
   stable Machine paths.
4. Test nested scripted calls initiated by native effects using the same
   Machine continuation protocol, not a second vmThread.
5. Emit side-exit counts and time by reason in diagnostics.

**Verification:**

```sh
go test -run 'TestMachineEffect|TestNative|TestRuntimeError|TestMachineDifferential' .
go test -race -run 'TestMachineEffect' .
```

**Completion:** Every pointer-bearing or unbounded action passes through one
auditable Go effect seam; common script semantics remain in the kernel.

### Slice 5.2 - Port callbacks and host calls to owner-bound handles

**Assigned agent:** Luna boundary worker owning `callback.go` and Machine
callback files. Sol reviews concurrency/busy/close behavior.

**Objective:** Capture and invoke script functions later without retaining old
Value/global snapshots or reconstructing the old execution environment.

**Planned files and symbols:**

- `runtime_machine_callback.go` (new): owner/image/closure handle, invocation
  metadata, explicit lease;
- `callback.go`: public capture/call/close implementation over the Machine;
- `runtime_machine_effect.go`: host/native callback request and result import;
- `program.go`: Runtime busy/run lease integration;
- callback, host boundary, close, re-entry, and allocation tests.

**Steps:**

1. Capture owner cookie, Machine lease, closure ID, module/global context IDs,
   and explicit capability data; do not copy `map[string]Value` or a `Value`
   closure.
2. Batch-import host callback arguments and batch-export results once.
3. Preserve Runtime serialization: overlapping `RunHook`/Callback calls fail
   with the documented busy error unless a separate design explicitly changes
   it.
4. Closing any shared Callback copy invalidates all copies and releases exactly
   one lease.
5. Test Runtime close races, callback after close, callback error frames,
   callback calling require, and host callbacks calling back into script where
   supported.
6. Assert warmed scalar callback invocation does not allocate beyond the frozen
   edge ceiling.

**Verification:**

```sh
go test -run 'TestCallback|TestMachine.*Callback|TestRuntime.*Busy|TestHost' .
go test -race -run 'TestCallback|TestRuntime.*Close' .
go test -bench 'BenchmarkRuntimeLaneHostBoundary' -benchmem -count=5 .
```

**Completion:** Callback/host execution resumes the same Machine state model;
no old closure/global snapshot survives in new-path callback state.

### Slice 5.3 - Port coroutines to Machine continuations

**Assigned agent:** Luna coroutine worker owning `base_coroutine.go` and new
Machine coroutine files. Sol reviews parent/normal/running/suspended/dead state,
yield boundaries, and collection.

**Objective:** Suspend and resume scalar stacks/continuations without retaining
`vmThread`, `vmFrame`, `[]Value`, or pooled global environments.

**Planned files and symbols:**

- `runtime_machine_coroutine.go` (new): coroutine directory, status, stack and
  continuation spans, yielded/resume spans, owner lease;
- `base_coroutine.go`: standard library operations over Machine handles;
- `runtime_machine_gc.go`: active/suspended coroutine roots;
- `runtime_machine_effect.go`: yield/resume/close transitions;
- coroutine differential, limit, cancellation, error, GC, and close tests.

**Steps:**

1. Represent coroutine status and saved execution state entirely with scalar
   Machine spans/indices.
2. On yield, clear invocation cancellation/controller capability while
   preserving script state; on resume, attach the new invocation policy.
3. Preserve parent status transitions, cross-owner rejection, max-coroutine
   accounting, error/result rules, and close semantics.
4. Reuse or transfer stack spans only at a stopped Machine state; no slice base
   may be retained across growth.
5. Test nested yields through script calls/metamethods/pcall-equivalent paths,
   errors after resume, close while suspended, owner collection, and repeated
   resume allocation.

**Verification:**

```sh
go test -run 'TestCoroutine|TestMachine.*Coroutine|TestExecutionLimit|TestCancellation' .
go test -race -run 'TestMachine.*Coroutine|TestRuntime.*Close' .
go test -gcflags=all=-d=checkptr=2 -run 'TestMachine.*Coroutine' .
```

**Completion:** Coroutine creation/yield/resume/close uses only Machine state
and explicit effects; warmed resumes do not allocate above baseline.

### Slice 5.4 - Complete exact limits, cancellation, errors, GC, and results

**Assigned agent:** Luna lifecycle worker. Sol performs a full semantic and
ownership audit before declaring complete coverage.

**Objective:** Close the remaining reasons a Program would need the old VM.

**Files and symbols:**

- `execution_control.go`: exact block/burst accounting and policy variants;
- `runtime_machine_exec.go`, `runtime_machine_effect.go`: safepoints and
  terminal exits;
- `runtime_machine_gc.go`, `runtime_owner.go`: collector integration and close;
- `runtime_error.go`: exact frame/source/error formatting;
- public result/lease implementation selected in Slice 1.4;
- `program.go`, `vm.go`: common private execution facade and compatibility
  wrappers;
- exhaustive differential and lifecycle tests.

**Steps:**

1. Select invocation policy once: unrestricted, instruction-bounded,
   cancelable/deadline, or instrumented. The unrestricted loop contains no
   per-op controller branch.
2. Charge blocks exactly and return before exceeding a limit. Test every limit
   from zero through block boundaries and around effect/call/yield operations.
3. Tune safepoint quantum against cancellation latency, scheduler/GC latency,
   and wrapper overhead; freeze a bound, not a guessed instruction count.
4. Trace every live slot span, continuation, module/global/table/object root,
   suspended coroutine, callback, and public result lease. Run collection at
   adversarial exits and growth points.
5. Convert Machine error state to public `RuntimeError` with identical script
   frames/PCs and wrapping.
6. Implement owner-bound result views and explicit detach/copy; reject use
   after close and cross-owner import.
7. Expand the feature inventory assertion until every verified opcode and
   supported standard-library/runtime behavior is Machine-capable.

**Verification:**

```sh
go test -run 'TestMachine|TestExecution|TestRuntimeError|TestRuntimeOwner|TestCallback|TestCoroutine|TestModule' .
go test -race -count=1 ./...
go test -gcflags=all=-d=checkptr=2 -count=1 ./...
scripts/check-runtime-parity --phase full
```

**Completion:** Every supported Program can run entirely on the pure-Go Machine
through every public entrypoint; feature routing reports no unsupported
semantic reason.

### Phase 5 gate

Run all correctness, race, checkptr, pure-Go, allocation, and public lifecycle
checks. Run a clean all-37 candidate capture. The candidate need not yet pass
2x, but before becoming the only production route it must have exact parity,
all public lifecycle/owner/limit gates, no recurring allocation regression,
and candidate/current median <=1.05 for every row in both clean comparisons.
This is an intermediate cutover safety gate, not the final Luau speed target.
If a complete Machine is slower, profile and fix it on the candidate route
before Phase 6; do not reintroduce mixed state.

## Phase 6 - Make the pure-Go Machine canonical

### Slice 6.1 - Cut every public entrypoint to one Machine facade

**Assigned agent:** Luna integration owner for `program.go`, `vm.go`,
`callback.go`, `module_runtime.go`, and coroutine seams. Sol audits the complete
call graph.

**Objective:** Remove production reachability of the old VM without yet
deleting its oracle implementation.

**Steps and files:**

1. Inventory and route public `Run`, `RunWithGlobals`, `Program.NewRuntime`,
   `Runtime.RunHook`, module execution, Callback calls, coroutine create/resume,
   and native scripted re-entry through one private Machine execution facade.
2. Delete whole-Program eligibility routing and feature fallback. A missing
   Machine operation is now a test/build failure, not old-VM execution.
3. Move/rename the old implementation behind explicit test-oracle names such
   as `oracleExecuteProto`, `oracleThread`, and `oracleFrame` in `_test.go`
   files. A compatibility-named production facade such as
   `executeProtoWithInvocationScope` may remain only if its body calls the
   Machine. If moving the oracle is too large for this slice, add a structural
   production call-graph test and schedule the named move before Phase 9.
4. Remove new-path `globalEnv`, old thread/frame, and `runtimeHeap` slab
   resolution from production profiles. Keep only named batch public adapters.
   `runtimeHeap` root/pin/collector code is not deleted until Machine
   root/lease tests replace every `runtime_owner_*_test.go` obligation.
5. Update docs to describe Machine as the canonical runtime.

**Verification:**

```sh
go test -run 'TestProductionExecutionUsesMachine|TestMachineEntryInventory|TestMachineDifferential' .
go test -count=1 ./...
scripts/check-purego
```

Use static symbol/call-path checks plus profiles to prove no old executor is
called; a boolean test-only backend flag is not sufficient evidence.

**Completion:** All product execution uses one pure-Go Machine. The old VM is a
separate test oracle only.

### Slice 6.2 - Tune the complete Go kernel without changing architecture

**Assigned agent:** Luna profiling worker proposes one measured change at a
time; Sol reviews each retained mechanism and cross-row evidence.

**Objective:** Realize the compact architecture's baseline performance before
adding assembly or AOT complexity.

**Allowed general tuning:**

- keep PC/base/top/budget and hot arena bases in locals across bursts;
- split invocation policy once outside the loop;
- reorder scalar records by measured hot fields and cache lines;
- combine adjacent bounds checks and use checked capacity invariants;
- reduce effect frequency and batch cold work;
- tune compact operation/side-descriptor layouts based on CPU and cache
  profiles;
- reserve arena/stack capacity from CodeImage static facts and prior owner
  high-water marks;
- inline small pure helpers only when profiles show call cost;
- remove redundant tag/shape/version checks proven by verifier or dominating
  guards.

**Forbidden tuning:**

- named benchmark fast paths;
- unsafe elision of owner/bounds checks without structural proof;
- widening all operations by assumption;
- adding recurring allocations or mirroring old state;
- compiler transforms that change instruction-limit accounting without a
  documented semantic rule.

**Process:**

1. Capture CPU and allocation profiles from the complete public path.
2. Rank cumulative costs and side-exit time. Change one structural mechanism.
3. Run generated development semantics, all supported rows, allocation checks,
   and code-size measurements while tuning.
4. Freeze the pure-Go mechanism set, then let the Sol reviewer run only
   `purego-holdout` for its retention decision. Retain the change only when it
   improves broad rows and regresses no row by more than 5%; revert failed
   complexity immediately. Do not use that revealed set for later quickening,
   assembly, or AOT decisions.
5. Repeat until the Go result stabilizes.

**Completion:** One clean pure-Go capture is the new backend baseline. If every
row has median <=1.85 and p90 <=2.0, skip assembly and AOT and proceed to Phase
9. Otherwise Phase 7 starts from profiles that quantify the remaining gap.

## Phase 7 - Add only profile-proven general accelerators

### Slice 7.1 - Add guarded quickening and measured block operations

**Assigned agent:** Luna generator/kernel worker. Sol reviews deoptimization,
semantic accounting, generality, and code-size growth.

**Objective:** Remove repeated decode/tag/descriptor work using runtime-derived
guards, without generating benchmark traces or a second state model.

**Files and symbols:**

- operation spec and `cmd/ember-vmgen/main.go`: quickening classes and generated
  generic/quickened handlers;
- `runtime_machine_cache.go`: owner-bound quickening state and counters;
- `runtime_machine_dispatch_generated.go`: guarded variants and generic
  deoptimization;
- `runtime_image.go`: generic block shape descriptors, never mutable guards;
- structural/code-size and generated-corpus tests.

**Steps:**

1. Start with broad guards already paid for by semantics: numeric arithmetic,
   stable global/property shape, direct script call mode, fixed arity/results,
   and iterator/table shape.
2. Install quickened state only after a small generic hit threshold; a miss
   reverts or updates owner cache state without changing image data.
3. Fuse operations only from a deterministic generic pattern catalog and only
   when exact guest-instruction/error-PC/safepoint accounting remains
   representable.
4. After the mechanism catalog is frozen, run only `quickening-holdout` without
   tuning the catalog from its result. Compare generic and quickened state/
   results/errors exactly.
5. Track text/rodata bytes and I-cache profiles. Reject blanket widening or
   fusion that improves a few rows but harms broad code locality.

**Retention gate:** Keep each quickening/fusion family only if it improves the
complete candidate geomean by at least 2%, improves or is neutral on at least
75% of rows that exercise it, regresses no row by more than 5%, and does not
violate the final every-row target. Multiple tiny retained families must also
show a meaningful aggregate win; otherwise simplify. This geomean is only a
local accelerator retention heuristic and never waives a failing final row.

**Verification:**

```sh
go test -run 'TestMachine.*(Quicken|Guard|Deopt|Block)|TestMachineDifferential' .
go test -bench 'Benchmark(Top10|Classic|Scenario)Luau/.*/ember_run$' -benchmem -count=5 .
```

**Completion:** Only broadly profitable guarded mechanisms remain; all run on
the same CodeImage/Machine and have exact generic fallback.

### Slice 7.2 - Implement the static Darwin/arm64 `RunBurst` backend if needed

**Assigned agent:** Luna assembly worker owns only backend files and generated
tables. Sol performs ABI, memory-safety, preemption, disassembly, and broad-gain
review before enablement.

**Entry condition:** The complete tuned/quickened pure-Go Machine still misses
the target, and profiles show dispatch, tag checks, scalar call mechanics, or
other assembly-addressable kernel work accounts for enough time to close the
gap after effect/wrapper overhead. If profiles instead show host effects,
unbounded table/string work, or public conversion dominating, fix those in Go;
assembly cannot solve them.

**Planned files and symbols:**

- `runtime_machine_burst.go` (new): backend interface and common wrapper;
- `runtime_machine_burst_arm64.go` (new, Darwin/arm64): typed Go prototype,
  feature selection, `runtime.KeepAlive`, stopped-state validation;
- `runtime_machine_burst_arm64.s` (new): bounded no-call Plan 9 assembly leaf;
- `runtime_machine_burst_portable.go` (new): pure-Go selection for all other
  targets;
- generator output for arm64 dispatch/operation constants derived from the
  single semantic spec;
- `runtime_machine_burst_arm64_test.go` and portable differential tests;
- `scripts/check-machine-backend` (new): build-tag, disassembly, segment,
  symbol, call-target, and 256 KiB backend text/rodata gate; invoke it from
  `scripts/check-purego` when the arm64 backend exists;
- `cmd/ember-machocheck/main.go` (new): dependency-free `debug/macho` segment,
  relocation, and symbol inspection that works on a non-Darwin cross host.

**ABI and safety rules:**

- use the supported Go assembler ABI with a Go prototype; never bind
  `ABIInternal` or use `go:linkname`;
- put `//go:build darwin && arm64` on the arm64 Go/assembly implementation and
  `//go:build !darwin || !arm64` on the portable fallback; split tests by the
  same constraints so symbols neither duplicate nor disappear;
- allocate every assembly-visible arena through a helper whose returned
  backing store escapes into the heap-owned Machine. Pass a pointer-free scalar
  control block plus separate typed arena base pointers/lengths; never pass a
  pointer-rich `Machine`, a slice header, or a stack-backed array;
- mutate only pointer-free-element arrays and scalar control fields explicitly
  passed through the wrapper;
- call no Go or foreign function and retain no pointer after return;
- `//go:noescape`, if justified, is only escape metadata, not a pin or lifetime
  guarantee;
- keep typed owners/slices live through the call and refresh bases after any Go
  effect/growth;
- return within a measured bounded primitive-work quantum for GC,
  cancellation, scheduling, and close; no unbounded probe/string loop is
  allowed;
- assembly memory accesses are invisible to `-race`, so canaries, differential
  tests, checked debug mode, bounds proofs, and disassembly are mandatory;
- unsupported operations return a backend-continuation class with no mutation
  or charge for the current operation; the Go generic kernel executes that
  operation on the same Machine. They are not semantic effects and never enter
  the old VM.

**Steps:**

1. Implement completion/budget/cancel exits and a tiny scalar operation set to
   validate the real seam, but do not retain it based on microbenchmarks.
2. Differential-test every instruction boundary and force GC immediately
   before/after each burst.
3. Expand by profile rank across broad semantic families: scalar/control,
   direct calls/results, stable globals/properties/tables, iteration, and
   in-capacity allocation/insertion.
4. Benchmark quantum choices such as 64/256/1024 guest operations and a
   primitive-work budget. Gate both wrapper cost and worst-case return latency.
5. Give `scripts/check-machine-backend` two explicit modes.
   `--cross --binary PATH` uses `go tool nm`, `go tool objdump`, and
   `cmd/ember-machocheck`'s standard-library `debug/macho` parser, so it can
   inspect a Darwin/arm64 Mach-O on any Go host. `--native --binary PATH`
   requires Darwin/arm64, runs every cross check, and corroborates segment
   flags with native `otool -l`. Both modes fail on calls/BL from the leaf,
   unexpected relocations/foreign symbols, writable+executable segments,
   missing build-tag-selected symbols, or combined backend text/rodata over
   256 KiB. Add fixture/self-tests for every rejection and for invoking each
   mode on the wrong artifact/host.
6. Run the same all-37 source through Go and assembly backends with independent
   owners and compare full state/events/errors, then freeze the backend and run
   only `assembly-holdout` before capturing retention performance.

**Retention gate:** Enable assembly only if it either makes the hard final
all-37 target pass when Go does not, or improves the complete suite median by
at least 20% relative to the Go Machine. In both cases it must improve or be
neutral on at least 80% of rows, regress no row by more than 5%, keep
allocations at the Go ceiling, and pass every semantic/safety/code-size gate.
Otherwise delete the backend and keep the pure-Go Machine.

**Verification:**

```sh
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./...
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 \
  go test -c -o /tmp/ember-machine-arm64.test .
scripts/check-machine-backend --cross \
  --binary /tmp/ember-machine-arm64.test
scripts/check-purego
```

The commands above cross-build and inspect but do not execute on a non-Darwin
arm64 host. On the controlled Darwin/arm64 runner, additionally run:

```sh
CGO_ENABLED=0 go test -run 'TestMachineBurst|TestMachineDifferential' .
CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 \
  -run 'TestMachineBurst' .
scripts/check-machine-backend --native \
  --binary /tmp/ember-machine-arm64.test
```

Run the race suite against the Go backend and all state/differential canaries
against assembly; document that the race detector cannot observe assembly
memory accesses.

**Completion:** The backend is either broadly retained with evidence or fully
deleted. The portable Go backend remains canonical semantics on every target.

### Slice 7.3 - Re-measure the complete dynamic runtime

**Assigned agent:** Luna measurement worker; Sol independently audits the two
clean attempts.

**Objective:** Decide whether the general dynamic runtime has met the product
target before considering build-time compilation.

**Steps:**

1. Run two clean all-37 speed captures and two performance-audit captures.
2. Compare every row against Luau, current baseline, allocation ceilings, and
   the pure-Go Machine baseline.
3. Report backend coverage, side exits, effect time, quickening hit/miss, code
   size, cancellation latency, and owner lifecycle overhead.
4. If all target gates pass, skip Phase 8 and proceed to final deletion.
5. If they fail, identify whether residual time is dispatch-addressable or
   semantic work. Do not assert that more assembly will help without profile
   mass sufficient to close each failing row.

**Completion:** A signed-off PASS proceeds to Phase 9. A miss enters Phase 8
only with an explicit product decision that build-time prepared scripts are a
valid primary deployment mode.

## Phase 8 - Optional general prepared-bundle Go AOT escalation

This is not a benchmark escape hatch and is not required if the dynamic
Machine meets 2x. It is the bold no-CGO contingency for Hearth deployments
whose script set is known before `go build`.

### Slice 8.1 - Make the product-mode decision

**Assigned agent:** Sol design agent; user/product owner approves the mode
because it changes deployment and compatibility.

**Decision:** Is it acceptable for maximum-speed Hearth scripts to be compiled
before the host binary is built, while dynamic `Compile` uses the portable
Machine interpreter as a separately reported compatibility path?

Proceed only if:

- build-time source availability is real for the target product;
- generated code can be rebuilt whenever source/compiler semantics change;
- the dynamic runtime's remaining gap is substantially dispatch/control
  overhead that AOT can remove;
- binary size and build time have explicit budgets;
- the speed claim clearly names the prepared mode and does not pretend dynamic
  `Compile` received the same gain.

If those conditions are false, record the dynamic target miss honestly and do
not implement AOT in this plan.

### Slice 8.2 - Generate Go blocks from the same semantic specification

**Assigned agent:** Luna compiler/generator worker. Sol reviews semantic
duplication, generated-code safety, size, and build integration.

**Objective:** Compile arbitrary verified CodeImages into ordinary Go source
that manipulates the same Machine/effect ABI, eliminating interpreter dispatch
for prepared scripts without CGO, JIT, or executable memory.

**Planned files and symbols:**

- `cmd/emberc/` (new command): load/compile/verify source bundles and emit a Go
  package plus image metadata;
- `runtime_machine_prepared.go` (new): register prepared Proto/block functions
  against image hashes and execute them through the common Machine/effect seam;
- `runtime_machine_prepared_fixture_generated_test.go` (new checked fixture)
  generated from `testdata/runtime-machine/aot-fixtures-v1.tsv`; do not commit
  acceptance benchmark-specific output;
- generator/golden/differential/code-size tests;
- build documentation for Hearth integration.

**Design rules:**

- generation is general for every supported CodeImage operation;
- generated functions are Proto or basic-block structured and use the same
  scalar arenas, continuations, effects, exact instruction counts, and error PC
  maps;
- decode/dataflow/accounting comes from the same operation spec; generated Go
  calls canonical helpers for complex semantics and any inlined restricted
  scalar handler must pass the generated transition vectors against those
  helpers;
- indirect/dynamic script calls dispatch by generated Proto ID through compact
  function tables or a common block switch, not source names;
- unknown/dynamic code stays on the Machine interpreter, never a foreign
  backend;
- ordinary Go compiler/PGO may optimize the final host binary; the runtime does
  not invoke the compiler or write executable code.

**Steps:**

1. Generate scalar/control and call blocks for a small checked semantic corpus
   and compare complete state against the interpreter.
2. Extend to tables, effects, callbacks, and coroutines through the existing
   `burstReturn` contract rather than copying effect logic.
3. Add deterministic generation, stale image/source hash rejection, and
   `emberc -check`, which renders the fixture to a temporary file and
   byte-compares it with the tracked output.
4. After the mechanism is frozen, run only `aot-holdout` through the generated
   prepared bundle; run all semantic and public lifecycle differentials without
   tuning the mechanism from its result.
5. Measure build time, generated lines, linked text/rodata, I-cache behavior,
   and performance. Establish per-operation/Proto size budgets.
6. Integrate with Hearth only in a separate repository slice after Ember's
   generator contract is stable.

**Retention gate:** Prepared AOT must pass every semantic/no-CGO/allocation
gate, make the declared prepared-mode all-37 target pass, improve at least 80%
of exercised rows, and fit frozen binary/build budgets. If not, delete it. The
dynamic Machine remains the single fallback state model either way.

**Verification:**

```sh
go generate ./...
go run ./cmd/emberc -check \
  -manifest testdata/runtime-machine/aot-fixtures-v1.tsv \
  -output runtime_machine_prepared_fixture_generated_test.go
CGO_ENABLED=0 go test ./...
scripts/check-purego
```

If retained, add `--phase prepared-speed2x` with the exact same all-37 sample
math and two caller-named capture directories. Its schema and reports say
`prepared`; they cannot be passed to the dynamic gate accidentally.

**Completion:** Either a general build-time prepared mode is retained and
honestly named, or Phase 8 records a rejected/declined decision and adds no
runtime complexity. A prepared PASS is a separate Hearth deployment result;
it does not mark this plan's dynamic <=2x objective complete.

## Phase 9 - Final acceptance and deletion

### Slice 9.1 - Freeze oracle evidence before deleting the old VM

**Assigned agent:** Luna verification worker owns the deterministic artifact;
two Sol reviewers independently audit its normalized schema and old/new result.

**Objective:** Preserve a complete immutable semantic oracle that remains
executable after the old VM is physically removed. This is a pre-deletion
correctness checkpoint, not the final performance claim.

**Steps:**

1. Freeze every dynamic/prepared mechanism and start from a clean commit. Run
   `final-holdout` through separate old/new owners; because no later mechanism
   decision remains, its result cannot be reused to tune a later backend.
2. Run all manifest sets in explicit old/new mode and write canonical expected
   JSONL only through `-machine-corpus-write-expected=PATH`. The flag fails if
   PATH exists or the tree is dirty and is unavailable unless
   `-machine-corpus-oracle=old` is also set.
3. Review the readable normalized outcomes, source hashes, state/event fields,
   and aggregate SHA-256. Install the exact reviewed file at
   `testdata/runtime-machine/corpus-v1-expected.jsonl` and record its hash in
   ADR 0007.
4. Make expected-artifact comparison the default test mode and prove generic
   Go plus every retained backend matches it. Keep old/new mode only until the
   next slice deletes the oracle.
5. If the final holdout exposes a semantic defect, fix correctness, bump the
   corpus artifact schema/version, regenerate from a clean reviewed commit,
   and disclose the failed set. Never silently rewrite expected output to make
   a candidate pass.

**Verification:**

```sh
scripts/check
go test -run '^TestMachineGeneratedDifferentialCorpus$' -count=1 . \
  -args -machine-corpus-set=final-holdout -machine-corpus-oracle=old
go test -run '^TestMachineGeneratedDifferentialCorpus$' -count=1 . \
  -args -machine-corpus-set=all -machine-corpus-oracle=old \
  -machine-corpus-write-expected=/tmp/corpus-v1-expected.jsonl
go test -run '^TestMachineGeneratedDifferentialCorpus$' -count=1 . \
  -args -machine-corpus-set=all \
  -machine-corpus-expected=testdata/runtime-machine/corpus-v1-expected.jsonl
```

**Completion:** The clean pre-deletion candidate passes old/new comparison;
the reviewed expected artifact contains every set, is hash-bound in ADR 0007,
and passes every retained Machine backend without invoking the old VM.

### Slice 9.2 - Delete the old VM and all migration adapters

**Assigned agent:** Luna deletion/refactor worker with exclusive ownership of
old executor files. Sol audits reachability and final simplicity.

**Objective:** Finish with one architecture, not a permanent dual runtime.

**Deletion targets, subject to `rg` reachability confirmation:**

- old `vmThread`, `vmFrame`, `vmFrameRecord`, recursive call bridge, and
  generated old dispatch loop in `vm.go`/`vm_dispatch_generated.go`;
- old `Value`-backed global/table/closure/upvalue execution helpers that have no
  public-boundary role;
- old `runtimeHeap` pointer slabs, roots/pins, collector, and generation checks
  only after Machine roots/leases/collector own every migrated owner test;
- whole-Program eligibility router, old/new differential adapter in production
  files, temporary feature flags, and duplicate operation metadata;
- obsolete generator templates and tests that only assert old layout;
- stale performance plans superseded by this completed migration.

Keep public Value/boundary code only to the degree required by the chosen
public contract. Rename remaining Machine files/types when deletion makes a
temporary migration prefix misleading, but do not combine that mechanical
cleanup with semantic changes.

**Steps:**

1. Move all useful old-VM semantic vectors to public source/result tests and
   port `runtime_owner_*_test.go`, callback collector, coroutine escape, root,
   pin, stale-handle, and forced-GC obligations to Machine types before
   deleting their implementations.
2. Make `TestMachineGeneratedDifferentialCorpus` read the reviewed expected
   JSONL by default. Remove its old/new adapter and oracle-update flags only
   after generic Go and every retained backend pass that default mode.
3. Rename any remaining oracle-only implementation to explicit `oracle*`
   symbols/files, then delete old execution paths in small commits grouped by
   calls, objects, tables, effects, and routing.
4. Delete `runtimeHeap` only after the replacement root/lease/collector suite
   passes race and checkptr. A zero hot-profile sample is not deletion proof.
5. Run `rg` for every deleted type/function and inspect production call paths.
6. Update or remove the current
   `vm_dispatch_spec.go://go:generate go run ./cmd/ember-vmgen` directive before
   deleting old output. If the new Machine still uses `ember-vmgen`, point the
   directive and `-check` test only at the final Machine spec/template/output;
   otherwise delete the directive, `vm_dispatch_template.go.tmpl`, obsolete
   generator branches, and their structural tests. Run `go generate ./...`
   followed by `ember-vmgen -check` when retained, and prove it cannot recreate
   `vm_dispatch_generated.go` or any old-VM symbol.
7. Delete `scripts/scenario-ratio-gate` and every stale invocation/test if not
   already removed in Phase 0.3; only the all-37 `runtime-ratio-gate` may remain.
8. Regenerate the single final dispatch/backend artifacts.
9. Run focused semantics, generated expected-artifact comparison, race, and
   checkptr after each deletion group. Do not reuse any pre-deletion speed
   result: dead-code removal can change inlining/layout, so Slice 9.3 performs
   the entire final performance and platform matrix on the delivered tree.

**Verification searches:**

```sh
go generate ./...
go test -run '^TestMachineGeneratedDifferentialCorpus$' -count=1 . \
  -args -machine-corpus-set=all
rg -n 'oracleExecute|oracleThread|oracleFrame|vmThread|vmFrame|vmFrameRecord|runtimeHeap|oldVMRoute|machineEligible' --glob '*.go'
rg -n 'runtime-speed-2x-no-cgo-(proof|architecture)|PROCEED' . --glob '!/.git/**'
rg -n 'scenario-ratio-gate|vm_dispatch_generated|runGeneratedDirectFrame|vm_dispatch_template' . --glob '!/.git/**'
```

Every remaining hit must be a deliberate public compatibility name, historical
ADR reference, or test fixture; classify it in review.

**Completion:** One CodeImage/Machine runtime remains. There is no production
old VM, synthetic proof, mixed-state adapter, or dead accelerator.

### Slice 9.3 - Certify the delivered post-deletion runtime

**Assigned agent:** Luna verification worker; two Sol reviewers independently
audit semantic, allocation, platform, and performance artifacts.

**Objective:** Earn the final claim on the exact clean commit that will ship,
after old-code deletion and final generation have changed binary layout.

**Required verification:**

```sh
scripts/check
go vet ./...
go test -race -count=1 ./...
go test -gcflags=all=-d=checkptr=2 -count=1 ./...
go test -run '^TestMachineGeneratedDifferentialCorpus$' -count=1 . \
  -args -machine-corpus-set=all
```

On controlled supported platforms, run portable builds/tests and the arm64
backend checks from Slice 7.2 if retained. Restore the two Phase 0 baseline
artifacts and immutable speed-baseline manifest by their ADR-recorded hashes;
the ratio gate must verify the raw directory hashes against that manifest
before reading slopes. On the controlled M1, use fresh output directories:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase full \
  --output /tmp/ember-final-full-a
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase full \
  --output /tmp/ember-final-full-b

CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x --capture-only \
  --capture-role candidate --capture-pair a \
  --output /tmp/ember-final-speed2x-a
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x --capture-only \
  --capture-role candidate --capture-pair b \
  --output /tmp/ember-final-speed2x-b
scripts/runtime-ratio-gate \
  --capture-a /tmp/ember-final-speed2x-a \
  --capture-b /tmp/ember-final-speed2x-b \
  --baseline-a /tmp/ember-speed2x-baseline-a \
  --baseline-b /tmp/ember-speed2x-baseline-b \
  --baseline-manifest /tmp/ember-speed2x-baselines-v1.tsv \
  --median-max 1.85 --p90-max 2.00 --current-median-max 1.05

CGO_ENABLED=0 GOMAXPROCS=1 scripts/runtime-allocation-gate --capture \
  --output /tmp/ember-final-runtime-alloc
scripts/runtime-allocation-gate --compare \
  --manifest /tmp/ember-runtime-alloc-ceilings.tsv \
  --candidate /tmp/ember-final-runtime-alloc

CGO_ENABLED=0 GOMAXPROCS=1 \
  scripts/performance-audit --output /tmp/ember-final-a --profiles
CGO_ENABLED=0 GOMAXPROCS=1 \
  scripts/performance-audit --output /tmp/ember-final-b --profiles
scripts/performance-audit-compare \
  --baseline-role a \
  --before /tmp/ember-compact-baseline-a \
  --after /tmp/ember-final-a \
  --manifest /tmp/ember-compact-gates.tsv
scripts/performance-audit-compare \
  --baseline-role b \
  --before /tmp/ember-compact-baseline-b \
  --after /tmp/ember-final-b \
  --manifest /tmp/ember-compact-gates.tsv
```

Run effect/side-exit/backend diagnostics outside timed samples and retain their
command/fingerprint, plus cancellation/limit/close latency and linked-binary
artifacts. Both `full` captures and both independent speed captures must pass;
no pre-deletion artifact, merge, or best-attempt selection is allowed.

**Completion:** On the post-deletion commit, every dynamic row has median
<=1.85 and nearest-rank p90 <=2.0 in each independent capture; every
candidate/current median is <=1.05; all semantics, allocation, lifecycle, and
platform gates pass. A prepared-mode result is reported separately and cannot
satisfy this completion condition.

### Slice 9.4 - Document the delivered architecture and evidence

**Assigned agent:** Luna docs worker. Sol checks claims against artifacts.

**Files:**

- `README.md`, `docs/design.md`, `docs/public-surface.md`,
  `docs/compatibility.md`, `docs/checks.md`;
- `docs/adr/0007-compact-production-machine.md`: final status and retained/
  rejected backends;
- `performance-audit.md`: concise final before/after table and profile facts;
- retire this implementation plan only after all completion criteria pass.

Document:

- CodeImage/Machine ownership and lifecycle;
- public breaking changes and migration examples;
- no-CGO boundary and any retained Go assembly/AOT mode;
- exact dynamic versus prepared performance claims;
- all-37 per-row result location and reproducibility commands;
- known platform differences and remaining risks.

**Completion:** A new engineer can understand and verify the runtime without
reading this migration history.

## Dependency graph

```text
0.1 tree reconciliation
  -> 0.2 pure-Go policy
  -> 0.3 all-37 harness
  -> 0.4 clean baseline + ADR
  -> 1.1 semantic spec
  -> 1.2 CodeImage
  -> 1.3 Machine state
  -> 1.4 public ownership
  -> 2.1 whole-Program router
  -> 2.2 scalar/control
  -> 2.3 flat calls
  -> 3.1 closures/upvalues
  -> 3.2 strings/constants
  -> 3.3 globals
  -> 3.4 modules
  -> 4.1 tables
  -> 4.2 shapes/properties
  -> 4.3 metatables/iteration
  -> 5.1 effects
  -> 5.2 callbacks
  -> 5.3 coroutines
  -> 5.4 limits/errors/GC/results
  -> 6.1 canonical cutover
  -> 6.2 pure-Go tuning
  -> 7.1 quickening/blocks (only if needed)
  -> 7.2 arm64 burst (only if needed and profile-justified)
  -> 7.3 dynamic decision
  -> 8 prepared AOT (only with explicit product approval)
  -> 9.1 freeze old/new oracle evidence
  -> 9.2 delete old VM and adapters
  -> 9.3 certify the post-deletion binary
  -> 9.4 document the delivered architecture
```

Some test/tooling slices may run in parallel when they do not edit shared hot
files. Representation and production cutover slices remain sequential because
they define one mutable state model.

## Planned file ownership map

This names intended seams; executors must confirm nearby conventions before
creating a file and keep the root package flat unless implementation pressure
proves a split.

| Area | Existing files touched | Planned private files |
| --- | --- | --- |
| Semantic generation | `vm_dispatch_spec.go`, `vm_dispatch_template.go.tmpl`, `cmd/ember-vmgen/main.go`, `function_analysis.go` | `runtime_machine_dispatch_generated.go` and generated metadata in root |
| Immutable image | `bytecode.go`, `program.go`, `wordcode.go` | `runtime_image.go`, `runtime_image_lower.go`, `runtime_image_verify.go` |
| Mutable machine | `runtime_owner.go`, temporary `runtime_heap.go`, `slot.go` | `runtime_machine.go`, `runtime_machine_slot.go`, `runtime_machine_stack.go`, `runtime_machine_roots.go`, `runtime_machine_gc.go` |
| Execution/effects | `execution_control.go`, temporary seams in `vm.go` | `runtime_machine_exec.go`, `runtime_machine_effect.go`, `runtime_machine_dispatch_generated.go` |
| Objects/globals/modules | `value.go`, `base_env.go`, `program.go`, `runtime_call.go`, `module_runtime.go` | `runtime_machine_object.go`, `runtime_machine_string.go`, `runtime_machine_global.go`, `runtime_machine_module.go`, `runtime_machine_cache.go` |
| Tables | `value.go`, `table_shape.go`, `property_ic.go`, table helpers | `runtime_machine_table.go`, `runtime_machine_table_iter.go`, `runtime_machine_shape.go` |
| Lifecycle | `callback.go`, `base_coroutine.go`, `runtime_error.go` | `runtime_machine_callback.go`, `runtime_machine_coroutine.go`, and the selected result/lease file |
| Optional arm64 | none initially | `runtime_machine_burst.go`, `runtime_machine_burst_arm64.go`, `runtime_machine_burst_arm64.s`, `runtime_machine_burst_portable.go`, `cmd/ember-machocheck/main.go` |
| Optional AOT | docs/build integration only initially | `cmd/emberc/`, `runtime_machine_prepared.go`, `runtime_machine_prepared_fixture_generated_test.go` |
| Gates | parity/benchmark tests, `scripts/check-runtime-parity`, `scripts/check-purego`, `docs/checks.md` | `scripts/runtime-acceptance-env`, `scripts/runtime-ratio-gate`, `scripts/runtime-allocation-gate`, `scripts/check-machine-backend`, `runtime_parity_timer_contract_test.go`, `runtime_machine_corpus_test.go`, `testdata/runtime-machine/corpus-v1-expected.jsonl`, focused structural/differential tests |

Do not create every listed file up front. Create the smallest file set for each
vertical slice and merge files when separation would not make ownership or
testing clearer.

## Risk register

### Risk: the compact Go interpreter is still more than 2x slower

**Mitigation:** The Machine state model is shared by pure Go, quickened Go,
static arm64 bursts, and optional prepared AOT. Profile the complete production
path, then escalate only the remaining dispatchable work. Do not rebuild state
again for each backend.

### Risk: boundary conversion erases core gains

**Mitigation:** Batch import/export, explicit owner-bound result/views, dense
host global descriptors, and separate boundary benchmarks. If profiles show
the old convenience API dominates, break it deliberately rather than leaking
public Values into hot state.

### Risk: side exits dominate table/call-heavy programs

**Mitigation:** Keep direct script calls, scripted metamethods, bounded probes,
in-capacity allocation/insertion, and stack/continuation reservation inside the
kernel. Count and time every exit reason. Assembly is forbidden until real
effect frequency is known.

### Risk: scalar handles trade speed for stale-reference bugs

**Mitigation:** Owner cookies and generations at public/effect seams, explicit
leases, debug checked mode, forced-GC/checkptr tests, and exhaustive scanner
field coverage. Exact tracing changes an unreachable entry
`live -> retired`, increments and publishes its boundary generation, clears
all scalar/root state, then permits `retired -> reusable` even while the owner
remains live; stale boundary handles therefore fail after reuse. The owner
epoch changes only on reset/close. Do not pay a generation branch on every
correct hot internal dereference.

### Risk: table order diverges from Luau

**Mitigation:** Freeze observable behavior against pinned Luau before choosing
the representation. Encode required ordering once in dense entries/order links
and fuzz mutation sequences.

### Risk: instruction limits/cancellation shift under blocks or fusion

**Mitigation:** Store exact guest counts and error PCs per block/operation,
split at semantic boundaries, test every small limit, and return before work
that cannot be fully charged. Quickening never changes guest accounting.

### Risk: Go assembly violates GC/preemption/toolchain assumptions

**Mitigation:** Bounded no-call leaf, Go prototype, pointer-free elements,
`runtime.KeepAlive`, stopped-state growth, linked-binary inspection, portable
oracle, canaries, and a deletion-first retention gate. Never use ABIInternal,
CGO, or foreign calls.

### Risk: generated code becomes unmaintainable or bloated

**Mitigation:** One semantic spec, deterministic generation, checked-in
structural tests, text/rodata budgets, stage-specific holdout semantics, and deletion of
quickening/fusion/AOT families that do not show broad gain.

### Risk: migration leaves two runtimes forever

**Mitigation:** Whole-Program routing only, no new features solely in the old
VM, Phase 6 production unreachability, and Phase 9 physical deletion. Every
phase completion names the bridge it removes.

### Risk: allocation appears flat while retained memory or GC grows

**Mitigation:** Report arena capacities, root-store bytes, retained result/
callback/coroutine state, and GC time separately. Allocation is not the primary
optimization, but unbounded or timed-path GC growth blocks cutover.

## Final completion checklist

- [ ] No proof plan, proof runtime, proof script, or proof prerequisite remains.
- [ ] `scripts/check-purego` rejects CGO and disguised foreign backends.
- [ ] One immutable owner-neutral CodeImage is shared across Runtime owners.
- [ ] Reference constants are rejected; Program and standalone Proto image
      preparation/lifetime are explicit and concurrency-tested.
- [ ] One owner-bound Machine owns all mutable execution state.
- [ ] Hot registers, continuations, objects, tables, globals, and caches use
      scalar pointer-free-element arrays.
- [ ] Internal hot dereferences do not pay generation checks; boundary handles
      enforce owner/index/generation safety while exact tracing reuses unrelated
      unreachable entries.
- [ ] All production entrypoints use one Machine execution facade.
- [ ] Direct script calls, stable table/property/global hits, bounded iteration,
      and in-capacity allocation do not side-exit.
- [ ] Limits, cancellation, errors, modules, host effects, callbacks,
      coroutines, GC, close, and results match the frozen semantics.
- [ ] Terminal, semantic-effect, policy-safepoint, and backend-continuation
      returns have exact pre/post/replay/charge tests.
- [ ] The frozen allocation manifest has every all-37 warmed and public
      lifecycle row; no recurring warmed allocation or cold allocation count
      exceeds its baseline ceiling.
- [ ] Both speed baselines and every allocation/profile capture pass the shared
      pinned environment validator; the immutable baseline manifest verifies
      both raw-directory hashes before candidate/current comparison.
- [ ] All 37 corpus-qualified rows are present. Each clean capture has three
      four-point OLS slopes per engine, nine ratios per row, fifth-value median
      <=1.85x Luau, and ninth-value p90 <=2.0x; both captures pass independently
      with no merge or best-attempt selection.
- [ ] The reviewed expected-corpus artifact replaces the deleted old oracle,
      and the complete two-capture speed gate was run after old-code deletion
      on the exact post-deletion runtime commit.
- [ ] Candidate A/current A and candidate B/current B each have nine slope
      ratios per row and median <=1.05 at the final cutover.
- [ ] Quickening, assembly, and AOT are retained only with broad measured gain;
      rejected mechanisms are deleted, and prepared AOT is never used to mark
      a failed dynamic target complete.
- [ ] No benchmark/source identity affects runtime routing or specialization.
- [ ] Old VM, mixed-state bridges, eligibility router, duplicate dispatch
      semantics, and stale plans are deleted.
- [ ] Standard, vet, race, checkptr, pure-Go, platform, differential,
      allocation, code-size, and lifecycle checks pass.
- [ ] Docs and ADRs describe the delivered architecture and honest performance
      mode.

## First execution handoff

Start with the remaining Phase 0 work. Verify the proof retirement already
landed with this plan, reconcile the currently dirty invocation/runtime edits,
then implement the pure-Go policy and measurement gates. Do not begin
CodeImage work in the same change that reconciles those Go edits. The first
runtime architecture commit begins only after a clean all-37 baseline and
accepted ADR 0007 exist.
