# ADR 0008: Use SSA AOT-to-Go for Prepared Luau Parity

Status: Accepted

## Context

ADR 0007 made the compact owner-bound Machine Ember's complete production
execution model and set a final dynamic-runtime target of 1.85x Luau. The
Machine now covers the complete 37-workload corpus, but two clean captures from
commit `24797945c04b1cd104e802b375f8233fe459909b` show that the remaining gap is
architectural rather than a narrow dispatch defect.

The captures used Go 1.26.4 on Darwin 24.6.0/arm64, Apple M1,
`CGO_ENABLED=0`, `GOMAXPROCS=1`, and pinned Luau 0.728 at SHA-256
`c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd`.
Their directory hashes are:

- A: `c0b2f7e4da30a532a265944642cf949d1d64747e8f8757d8bb00bea2231e70b0`;
- B: `8529c07faea2ccbe82dd7ee9477e201e8954c965f07dcf23942019ecc66739dc`.

Every row misses 1:1. The best row, `top10/while_branching`, is about 3.02x
Luau. Arithmetic and recursive calls are about 5.85x and 6.2x. Representative
table, iteration, callback, metatable, and vararg rows are about 9.6x-22.2x.
No interpreter-only mechanism has credible headroom to close every gap.

The old warmed-call parity contract also compared N public Ember
`Callback.Call` entries with N guest-internal Luau calls. An empty Machine
callback costs roughly 325 ns and 168 B/4 allocations, which already exceeds
some complete Luau rows. That contract remains useful for public lifecycle
latency, but it is not an equivalent guest-throughput comparison.

The corrected schema-v2 observer was later frozen and recaptured from exact
commit `a424022484e6afc1d976722a6fa2f6acaf8ef65e`. Its one-program,
runtime-N, fixed-runtime-seed `guest_batch_v1` artifacts contain 889 raw lines
and 223 fitted-slope lines per capture. The clean directory hashes are:

- A: `39299ed38ce690abc0fc00fa521d1722143176a3bf83bda3f63ceb1af1337905`;
- B: `ba129677c4458debfb709338cbe0df1ed088334f36cbadf0c8910342119aad70`.

The new observer reproduces the architectural rejection without public-call
mass: all 74 capture/case rows miss the dynamic threshold, medians span about
`3.00x-23.42x`, and the worst p90 is about `24.64x`. `while_branching` remains
the best row, recursive Fibonacci is about `5.38x`, arithmetic about `5.78x`,
and array operations remain the worst row. This supersedes the old observer
for guest-throughput decisions while retaining the old evidence as lifecycle
history.

The architecture decision therefore evaluated a bounded no-CGO candidate
population under a corrected `guest_batch_v1` contract:

- one parameterized program per case;
- runtime-selected N and seed;
- compilation, preparation, binding, and process startup outside the timer;
- N guest calls and one checksum inside the timer;
- the same semantic body and checksum under Ember's proof lowering and Luau;
- N=`{1,10,100,1000}`, three repeats, fitted slope, and nine Cartesian ratios;
- two independently acquired clean captures plus a generic-representation
  sensitivity capture.

The retained proof package is `internal/architectureproof`. Its functions are
manual semantic lowerings, not a compiler implementation. They establish a
performance ceiling and representation sensitivity only.

## Candidate cutoff

The decision population and cutoff were:

| Candidate | Decision | Evidence |
| --- | --- | --- |
| Complete Machine plus deeper quickening | Retain as canonical fallback, reject as the parity engine | Complete semantics, but every current all-37 row remains about 3.02x-22.2x Luau. |
| True SSA/value/shape-specialized AOT to ordinary Go | Select as the primary prepared engine | The manual lowering ceiling is roughly 0.011x-0.100x Luau across eleven representative families. Generic Go strings, maps, closures, and variadic slices keep the worst sensitivity row near 0.32x. |
| Static native AOT through generated Plan 9 ARM64 assembly | Defer as a conditional second lowerer over the same IR | A direct arithmetic lowering through supported Go assembly is statistically tied with compiled Go. Assembly removes no semantic or representation cost by itself. |
| Pure-Go ARM64 JIT | Reject as the production foundation | A pointer-free leaf is feasible, but the measured leaf is tied with Go/static assembly and required disabling asynchronous preemption in the proof. Arbitrary JIT PCs lack Go stack maps, traceback metadata, and a supported way to call Go or retain Go pointers. Apple Silicon also adds MAP_JIT, W^X, cache, entitlement, and lifetime obligations. |
| Raw Mach-O/`.syso` functions or private linker/ABI integration | Reject | General functions need Go `FuncInfo`, pclntab, stack maps, and stable calling conventions. `ABIInternal` and linker internals are not a product ABI. |
| Runtime compiler, plugin, helper process, foreign runtime, or upstream Luau backend | Reject as inadmissible | Violates the deployment and no-CGO product boundary. |

The bounded scan of Luau's interpreter/native implementation and Go's
assembler, linker, ABI, and runtime metadata did not expose another materially
distinct feasible family. Profile-guided multiversioning is an AOT policy, not
a separate execution architecture. A Wasm/LLVM/embedded-runtime route either
adds another interpreter/JIT or crosses the forbidden foreign-runtime and
dependency boundary.

The selected candidate is the highest-confidence architecture within this
bounded population. The decision does not claim a global optimum.

## Evidence

### Manual lowering ceiling

The proof covers:

- arithmetic and branches;
- stable fields, arrays, iteration, and generated strings;
- fixed and dynamic calls, closures, and upvalues;
- recursion, varargs, and multiple returns;
- metatable fallback and coroutine state machines;
- mutating arrays and front removal.

Every lowering is parameterized by a runtime seed. Frozen tests and live Luau
checks compare seeds `0`, `1`, `7`, and `29`; the capture uses distinct
runtime-derived seeds for each repeat and N point. No lowering branches on a
case name, source hash, expected result, timing point, or corpus identity.

Two clean ceiling captures and two clean sensitivity captures came from exact
proof commit `b9717cc835637d3902a3df5058bb8985a58fe85a`. All four record
`vcs_modified=false`, source SHA-256
`98d49d8afd03ab7ffd3881e7e9e965819cdd07c8ec40f26cceb80791d181ba53`,
and binary SHA-256
`cdf291e1d196c01552091984608fa0e0d137d70e99cc5d414edb5acbc0978a8b`.
Their directory hashes are:

- ceiling A: `2cfa240c51957e65574fca9c98364794cadd8e695775c293b72956d75eb2cf9c`;
- ceiling B: `72813a1be85dfb5ff985b711bcdfa249643774609f82fc1dcc4af4007b948906`;
- sensitivity A:
  `a24f21ff5a9abda1bbc72fd0d5546b5d48bd46b644c66a4b049a7587f8ab04f9`;
- sensitivity B:
  `caacfd7d7f4f995a66d354c975b4bf5402fa97167a6f2a98ad469cb066b50fb8`.

The ceiling medians span approximately:

- stable scalar-replaced fields: `0.011x`;
- dynamic calls/closures/upvalues: `0.023x`;
- varargs/multiple returns: `0.029x`;
- generic iteration and mutating arrays: `0.044x-0.048x`;
- recursion and generated-string tables: `0.050x-0.060x`;
- arithmetic and coroutine state machines: `0.095x-0.100x`.

The sensitivity variants deliberately preserve ordinary Go closures/maps,
string construction, and variadic slices. The generated-string sparse-grid
median is `0.310x` and `0.313x` in the two roles; no sensitivity result reverses
the candidate ranking. This leaves roughly 3.2x headroom even before using
Ember's interned strings, scalar arenas, shape guards, and direct owner state.

Most specialized lowerings allocate zero per guest call. The packed sparse
grid allocates about 2,840 B/52 allocations per guest call; its ordinary
string/map sensitivity variant allocates about 18.7 KiB/4,564 allocations.
These are ceilings, not acceptable production allocation budgets. Generated
production code must use Machine arenas, interned strings, scalar replacement,
or exact side exits and stay within the frozen warmed allocation ceilings.

The eleven specialized functions plus the recursive helper occupy about 5,232
bytes of linked text in the proof binary. This is evidence that direct code for
representative program shapes need not imply an immediate code-size explosion;
the general generator still requires explicit linear-growth and total-size
gates.

### Transition and slow-path semantics

The selected architecture reuses the canonical Machine/effect path rather than
inventing separate slow semantics. Focused repository tests pass for:

- mutation-safe exit before a mismatched array slot;
- metatable table-access islands and fast-loop re-entry;
- generic host-call islands and re-entry;
- coroutine yield/resume exits;
- debug and exact instruction-budget handling without whole-frame demotion;
- repeated cold-island exit followed by resumed fast arithmetic.

These tests prove the existing semantic fallback behavior that generated code
will target. They do not prove a generated SSA spill map; that remains a
blocking private compiler and side-exit gate in the implementation plan.

### Assembly and JIT probes

Five 2-second samples of the retained direct arithmetic fixture measured
compiled Go and static Plan 9 ARM64 assembly in the same approximate
226-238 ns range. The assembly function uses Go's supported ABI0 prototype,
contains no pointers, and makes no calls.

A discarded pure-Go JIT leaf emitted the same arithmetic body into MAP_JIT
memory. With `CGO_ENABLED=0`, `GOMAXPROCS=1`, and
`GODEBUG=asyncpreemptoff=1`, five samples put Go and JIT medians at about
275.5 ns and 275.9 ns. The test-binary SHA-256 was
`02c4a3298fb6ee91cf6e8086e15147edc7b50c4c5bee837c96fd18612802174c`;
the retained external benchmark log SHA-256 was
`7e2f64586258c5639556111bb49e34da340c98db913676f2474b2601d96dbe88`.
The spike proved instruction emission, not a safe Go runtime integration, and
its executable-memory code was deliberately not retained in the repository.

### Upstream and toolchain constraints

Luau 0.728 uses computed-goto interpreter dispatch, keeps critical interpreter
state in locals, patches lookup slots, and enters native code through explicit
execution callbacks:

- [Luau interpreter dispatch and patching at the pinned commit](https://github.com/luau-lang/luau/blob/ddcea05e1cc6f534e5eaac33325690c12f1ed274/VM/src/lvmexecute.cpp#L77-L149)
- [Luau interpreter state and native entry](https://github.com/luau-lang/luau/blob/ddcea05e1cc6f534e5eaac33325690c12f1ed274/VM/src/lvmexecute.cpp#L240-L290)

Its ARM64 code generator has an explicit custom entry ABI, VM exits, saved-PC
synchronization, interpreter fallback, and unwind metadata:

- [Luau ARM64 exits and re-entry](https://github.com/luau-lang/luau/blob/ddcea05e1cc6f534e5eaac33325690c12f1ed274/CodeGen/src/CodeGenA64.cpp#L32-L133)
- [Luau ARM64 entry and unwind setup](https://github.com/luau-lang/luau/blob/ddcea05e1cc6f534e5eaac33325690c12f1ed274/CodeGen/src/CodeGenA64.cpp#L224-L317)
- [Luau executable-memory allocation and cache maintenance](https://github.com/luau-lang/luau/blob/ddcea05e1cc6f534e5eaac33325690c12f1ed274/CodeGen/src/CodeAllocator.cpp#L88-L140)
- [Luau DWARF unwind construction](https://github.com/luau-lang/luau/blob/ddcea05e1cc6f534e5eaac33325690c12f1ed274/CodeGen/src/UnwindBuilderDwarf2.cpp#L140-L220)

Go assembly is a supported build-time path, but pointer maps and runtime
coordination are explicit obligations. The official guide recommends Go
prototypes for assembly functions and explains the `FUNCDATA`/`PCDATA` and
pointer restrictions:

- [Go assembler guide](https://go.dev/doc/asm)
- [Go internal ABI specification](https://go.dev/src/cmd/compile/abi-internal)
- [Go unsafe pointer rules](https://go.dev/src/unsafe/unsafe.go)
- [Go linker pclntab construction](https://go.dev/src/cmd/link/internal/ld/pcln.go)
- [Apple Silicon JIT guidance](https://developer.apple.com/documentation/apple-silicon/porting-just-in-time-compilers-to-apple-silicon)

These sources support a build-time Go or Go-assembly backend. They do not
provide a supported route for arbitrary runtime-generated Go frames.

## Decision

### Primary backend

For scripts known before `go build`, generate ordinary Go from a private,
verified, closed-world SSA backend IR. The generated code is the primary
prepared execution engine.

The backend IR is built from immutable `codeImage`/`machineProto` data and
contains:

- control-flow graphs, dominators, loops, SSA values, phis, and liveness;
- exact source/word-PC/image-PC and guest-charge maps;
- tag/value refinements and representation choices;
- effect tokens and mutation barriers;
- escape facts for tables, closures, tuples, strings, and varargs;
- table shape, property offset, metatable-chain, global, and call-target facts;
- exact spill maps and replay-safe side exits.

Generated Go keeps proven scalar values in typed locals. It may:

- scalar-replace nonescaping tables, closures, tuples, and coroutine state;
- devirtualize fixed calls and guarded stable dynamic calls;
- use fixed tuples for proven multiple-return/vararg shapes;
- use direct array/property offsets behind owner-local shape/version guards;
- represent coroutines as explicit resumable state machines;
- materialize a generic Machine object only when a value escapes or a guard
  requires the canonical path.

Specialization may depend only on verified program structure and
runtime-derived tags, shapes, versions, targets, arities, and metatable state.
Source names, source hashes, benchmark identities, expected results, and
corpus membership are forbidden execution keys.

### Canonical fallback and side exits

The complete Machine remains the semantic oracle, fallback, and dynamic
runtime. It is not deleted.

`machineOwner.executeStopped` is the backend-selection seam. A prepared
artifact is eligible only when its ABI version, semantic version, complete
program hash, module inventory, and Proto inventory match before owner
mutation.

Every generated guard has an exact side exit. The preferred form exits before
mutation, spills live SSA values into the owner Machine, records the exact
module/Proto/PC/charge state, and resumes through the canonical operation.
Existing `restartPC` and `skipCharge` behavior is the starting replay seam.
Operations that cannot exit before mutation require one explicitly tested
commit protocol; they may not rely on vague "resume nearby" behavior.

Host calls, modules, cancellation, limits, errors, collection, growth,
unsupported metatable paths, and other effects remain owned by the Machine
effect boundary. Unrestricted hot blocks may bulk-charge proven instruction
counts. Controlled execution falls back at an exact PC whenever the remaining
budget cannot cover the block.

### Table shapes

Add a stable owner-local shape/layout ID and property-offset contract to
`machineTableRecord`. `rawVersion` remains a mutation/version signal but is too
coarse to prove a property layout. Reuse the semantic model already exercised
by `tableShape` and `propertyIC`: equal stable insertion layouts share a shape,
property offsets survive value updates, and dynamic/dictionary transitions
invalidate guarded code.

Generated code may access an escaping table directly only after validating the
shape/layout ID, relevant metatable version, and bounds. A miss exits to the
Machine without partially applying the operation.

### Generated artifact and lifecycle

The first implementation remains private and test-only. No public prepared API
is retained until a real generated compiler proof passes representative
semantic, performance, allocation, code-size, and side-exit gates.

After that proof, the narrow public lifecycle may change:

- `Program.WritePreparedGo` emits a deterministic application-owned package;
- the package exposes one immutable `PreparedBundle`;
- `RuntimeOptions.Prepared` or an equivalent explicit constructor dependency
  binds that bundle;
- mismatches fail before owner mutation and never silently fall back;
- nil preserves the ordinary dynamic Machine path;
- no registration through `init`, package globals, plugins, helper processes,
  or source-name routing is allowed.

The generated package receives an opaque prepared context. The proof must show
that cross-package inlining or coarse block entry keeps the context ABI out of
hot per-operation paths. If it does not, the public shape is rejected before
retention rather than exposing Machine internals.

### Performance claim

Prepared guest throughput and public lifecycle latency are separate claims:

- `guest_batch_v1` measures one outer owner entry and N guest invocations in a
  parameterized guest loop. It is the 1:1 all-37 target.
- `public_call_v1` measures ordinary `Callback.Call`, runtime creation,
  binding, detach, close, errors, callbacks, and other host lifecycle paths.
  It remains a no-regression/allocation gate and is reported separately.

The final prepared claim requires two exact-commit clean all-37 captures. Every
row in each capture must have median Ember/Luau ratio `<=1.00` and nearest-rank
p90 `<=1.05`. Results, effects, errors, instruction accounting, owner
lifecycle, deployment facts, warmed allocations, build time, and code size are
independent blocking gates.

This does not claim that dynamic `Compile` or one public callback crossing is
1:1 unless those separately named measurements also pass.

### Conditional assembly lowerer

The backend IR may gain a generated Plan 9 ARM64 lowerer only after the
ordinary-Go backend is semantically complete and exact profiles show that
instruction selection or register allocation, rather than representation,
guards, effects, or exits, accounts for enough time to close every remaining
row.

That lowerer:

- uses supported Go assembly and ABI0 prototypes;
- operates on pointer-free state or explicit Go-owned pointer maps;
- implements the same IR and side exits;
- is never another descriptor interpreter;
- has a portable Go fallback;
- must improve broad rows, not one arithmetic microbenchmark.

## Supersession map

- ADR 0007 remains authoritative for immutable CodeImage data, owner-bound
  Machine state, scalar slots/arenas, explicit effects, lifecycle ownership,
  and the no-CGO boundary.
- ADR 0007 is superseded where it names the generated pure-Go Machine loop as
  the final performance architecture, requires deletion of the old semantic
  fallback, or treats prepared AOT as only a later optional escalation.
- ADR 0007's 1.85x dynamic target becomes a diagnostic milestone, not the
  prepared product finish line.
- ADR 0005's evidence against narrow alternate engines remains valid. The AOT
  backend is retained only if it covers the complete target and shares exact
  Machine exits; otherwise it is deleted.
- ADR 0001 still governs detached public Go values and opaque host payloads.

## Consequences

Ember accepts a prepared deployment mode and a small explicit lifecycle change
because the measured interpreter gap is too large for dispatch tuning alone.
The dynamic Machine remains valuable for runtime compilation, unsupported
prepared paths, debugging, exact effects, and semantic differential tests.

The compiler becomes a first-class performance component. Correctness now
depends on SSA construction, escape/shape facts, exact spill maps, and
side-exit replay. Those modules must remain private, deterministic, and heavily
differential-tested.

Generated source and linked code size become product costs. The implementation
must report generation time, Go build time, source bytes, text/rodata, number
of generated versions, and exit coverage. Linear growth is required; callers
may set a generation size limit.

The public API is deliberately not changed by this ADR alone. A failed private
compiler proof leaves only the proof harness, evidence, ADR, and rejected
implementation history; it does not leave a speculative prepared surface.

## Alternatives considered

- Keep optimizing the interpreter until it reaches parity. Rejected as the
  primary route because the complete Machine is still 3.02x-22.2x behind and a
  descriptor-driven assembly interpreter did not erase representation,
  calls, tables, or effects.
- Make static ARM64 assembly the architecture. Rejected because direct
  assembly is tied with Go for the same lowered program. Assembly is a
  conditional code-generation backend, not a substitute for SSA,
  specialization, escape analysis, shapes, and exact exits.
- Build a pure-Go JIT. Rejected because AOT is allowed, the leaf has no measured
  speed advantage, and arbitrary JIT frames do not integrate with Go's
  supported stack-map, traceback, preemption, and pointer model.
- Emit raw object files or patch the Go linker/runtime. Rejected as an unstable
  private-toolchain fork with a much larger correctness and maintenance
  surface than generated Go or supported Plan 9 assembly.
- Expose all Machine arenas publicly so generated packages can access them.
  Rejected unless the private proof demonstrates that a narrow opaque ABI
  cannot inline or batch sufficiently. Public raw state would freeze the wrong
  abstraction and make lifecycle misuse easy.
- Claim parity by batching only the benchmark. Rejected. The new contract is a
  general parameterized guest loop, and ordinary public-call latency remains
  separately visible.
