# Ember Runtime Within 2x of Luau: General No-CGO Architecture

Status: implementation plan, not yet executed

Created: 2026-07-14

Target acceptance platform: Darwin 24.6.0, Apple M1, arm64, Go 1.26.4,
`CGO_ENABLED=0`, `GOMAXPROCS=1`, pinned Luau 0.728 at SHA-256
`c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd`,
Luau's interpreter at its default optimization level, and no Luau codegen.

This plan supersedes `runtime-speed-2x-luau-implementation-plan.md` for future
speed work. The earlier plan remains evidence, but its sequence of incremental
interpreter optimizations is not a credible route for the remaining 2.8x to
3.9x gaps in the hardest semantic classes. Do not delete either plan or the
completed performance history.

## Decision in one paragraph

Replace Ember's pointer-bearing, frame-reconstructing interpreter core with a
runtime-owned execution module whose backend-visible mutable state is
pointer-free. Every Proto lowers through one general verifier into a compact
execution image. The canonical runtime state
uses 64-bit slots, owner-relative handles, a flat value stack, a compact
continuation stack, and assembly-readable table/global/property descriptors.
On Darwin/arm64, a statically linked Go-assembly burst engine executes bounded
groups of general semantic operations, including direct script calls and
stable table/property hits, then returns a precise side exit to Go for effects.
A generated pure-Go engine uses the same image as the correctness oracle and
portable fallback. There is no CGO, C, C++, dynamic foreign-function bridge,
embedded upstream Luau, runtime-generated code, executable-memory mapping, or
runtime helper process.

This is not claimed to be certain. Before an artifact exists, current evidence
supports only roughly 20% to 30% confidence that a complete arm64
implementation can put every frozen workload under 2.00x. A passing general
Phase 1 vertical proof can raise that to roughly 60% to 65%. Confidence exceeds
90% only after the full held-out gates and two independent clean all-row
captures pass. If the prototype cannot produce the class-relative multi-x
gains required by the baseline, stop and report that the target is not
achievable under the no-CGO constraints; do not resume a sequence of 15%
micro-optimizations.

## Hard constraints

### No CGO means no foreign runtime backend

The production repository must continue to pass `scripts/check-purego`.
Specifically:

- no `import "C"`, CgoFiles, C/C++ source, or cgo-generated shims;
- no dynamic C/libSystem FFI used as a substitute for cgo;
- no embedding, linking, or mechanically porting the upstream Luau VM;
- no out-of-process Ember execution backend or `go tool` compiler helper;
- no runtime JIT, `MAP_JIT`, RWX/RW-to-RX code pages, or host JIT entitlement;
- no dependency addition without the user's explicit approval;
- the pinned Luau executable remains an external measurement and differential
  oracle only. It is never part of the Ember product path.

Ordinary Go assembly in checked-in or reproducibly generated `.s` files is
allowed. It is assembled and linked by the Go toolchain, contains no foreign
ABI calls, and operates only on Go-owned, pointer-free state.

### Generality contract

The implementation may optimize language/runtime classes, never benchmark
identities. Production runtime code must not inspect or branch on:

- source text, source hash, chunk name, fixture name, benchmark case name, or
  corpus membership;
- a hand-picked guest opcode sequence introduced because one measured program
  contains it;
- constants, loop trip counts, table keys, function names, or Proto IDs copied
  from the acceptance corpus;
- environment fingerprints other than selecting a supported machine backend.

Allowed mechanisms are general and independently useful:

- canonical opcode-to-micro-op lowering and verification;
- runtime value type, table shape/version, property-chain, call-mode, arity,
  and effect descriptors;
- direct script call/return/tail-call mechanics;
- generic fixed/open/vararg/multi-result mechanics;
- bounded table array/hash access and stable iteration;
- semantic-class quickening driven only by runtime guards;
- standard compiler transformations that preserve every matching program,
  such as constant folding or dead-result elimination, if a future static
  lowering phase proves them generally.

The 37 named workloads are a black-box acceptance oracle, not a menu of
features to implement. A held-out generator creates programs only after the
mechanism set is frozen. Production backend packages are statically scanned
for corpus strings and fingerprint logic.

### Speed and allocation outcome

The target is complete only when every unique workload from the Top10,
Classic, and Scenario inventories passes a clean paired slope capture:

- median of nine paired ratios <= 1.85x Luau;
- nearest-rank p90 of nine paired ratios <= 2.00x Luau;
- no exception list, geomean waiver, corpus-median substitution, or named-row
  fallback.

The 1.85 median is engineering margin for a hard 2.00 p90 ceiling. Exploratory
captures may report a geomean, but retention and completion are per row.

Allocations are not the optimization objective, but speed may not be bought by
recurring allocation:

- warmed execution may not increase per-row B/op or allocs/op above the frozen
  clean-baseline ceiling;
- no allocation may recur per guest instruction, internal script call, loop
  iteration, stable property/table hit, coroutine resume, or warmed hook;
- one-time execution-image and owner preparation may not exceed the frozen
  cold-path allocation count for the same API. Within that existing ceiling,
  image-specific storage may use at most one aggregate allocation plus one per
  executable Proto and at most 32 bytes per guest word plus 512 bytes per
  Proto after superseded sidecars are removed;
- initial value, continuation, table-directory, table-cell/control/order,
  descriptor, and root arenas must publish logical capacity and retained bytes.
  Their combined bytes may not exceed the sum of their exact scalar layouts,
  plus 12.5% capacity rounding and 4 KiB per owner. Their allocation count may
  not exceed the frozen cold API ceiling. A dedicated cold artifact and gate,
  not the warmed performance-audit manifest, enforces these formulas;
- later arena growth is a semantic allocation. It may not increase allocs/op or
  B/op relative to the matching frozen workload/lifecycle and may never recur
  for a stable warmed operation;
- static assembly text is part of the binary and has no runtime allocator.
  Its complete hot `.text` plus dispatch `.rodata` is capped at 256 KiB and is
  checked from the linked binary; a lower measured limit may be frozen in ADR
  0007 before implementation.

Backwards compatibility is explicitly subordinate to speed. Go APIs, private
bytecode, Value ownership, Runtime lifetime, and table representation may
break. Luau language behavior, deterministic ordering, errors, execution
limits, cancellation, callbacks, coroutines, and owner isolation remain
semantic requirements unless separately changed and documented.

## Repository evidence

At plan creation, `main` is at `09cd4722`. The worktree contains user-owned
edits in `base_coroutine.go`, `base_env.go`, `callback.go`,
`module_runtime.go`, `program.go`, `runtime_call.go`, `runtime_heap.go`, and
`vm.go`, plus untracked plans. These overlap the future implementation. An
executor must preserve them and may not stage, revert, overwrite, or build a
clean acceptance artifact from them. Phase 0 starts only after the owner lands,
replaces, or otherwise resolves those edits. Dirty diagnostic work is allowed
only when labeled non-acceptance.

Current architecture and profiles show the problem is structural:

- `vm_dispatch_template.go.tmpl` generates a very large Go switch loop and
  calls `executionWindow.stepInstruction` at instruction boundaries;
- `Value` is 16 bytes and pointer-bearing, while the existing private `slot`
  is 8 bytes but is not the canonical runtime representation;
- fixed calls repeatedly validate, enter, resume, clear, and reconstruct
  `vmFrame`/`vmFrameRecord` state;
- direct-loop, `valueKind`, call entry/resume, property lookup, hash/table
  storage, and iteration dominate the relevant profiles;
- the deleted general slot experiment reached about 0.620x the direct engine
  on eligible code, numeric slots about 0.494x, and compact calls about 0.279x,
  proving local headroom but covering zero Scenario rows;
- the deleted direct-loop kernel itself was about 1.223x slower, proving that
  another Go dispatch layer is not automatically a win.

A dirty exploratory ratio capture suggested approximately 2.446x on
`combat_tick`, 5.557x on `event_dispatch`, 7.786x on `prototype_fallback`,
6.603x on `command_vararg_router`, and 6.995x on
`recursive_fibonacci`. These are triage, not baselines. They imply required
speedups of roughly 1.22x, 2.78x, 3.89x, 3.30x, and 3.50x respectively merely
to touch 2.00x. That is why a 15% to 20% campaign cannot be the main plan.

The current acceptance tooling also has correctness gaps:

- `parityCaseSelection("")` in `runtime_parity_test.go` selects only the 25
  Scenario rows;
- `scripts/check-runtime-parity --phase full` therefore does not mean all 37
  unique Top10, Classic, and Scenario rows;
- the script has no `speed2x` phase, although the previous plan referenced it;
- `scripts/scenario-ratio-gate` hardcodes N={1,10,100,1000}, three repeats, and
  nine pairs; N=1000 makes recursive rows expensive and can make entry cost or
  timer noise dominate other rows.

## Designs considered

### Design A: pure-Go execution-image rewrite only

This design performs the full slot, flat-call, descriptor, and table cutover,
then runs a generated Go switch. It has the lowest platform risk and remains
the required correctness oracle. It is not selected as the only speed plan:
prior direct-kernel evidence and Go dispatch/profile costs make the hardest
2.8x to 3.9x gains unlikely. If it unexpectedly passes the final target, stop
there and delete the native adapter.

### Design B: statically linked Go-assembly burst engine

This is the selected speed architecture. Dynamic programs remain ordinary
execution-image data. A leaf Go-assembly function reads verified fixed-width
operations, keeps hot scalar state in registers, uses an in-text branch table
or measured fallback dispatch, and mutates only pointer-free arrays. It handles
general scalar/control operations, script calls, stable globals, property/table
hits, iteration, and upvalues. It returns to Go for allocation, growth,
unknown effects, host callbacks, errors, and coroutine transitions.

Assembly is not an async safe point in Go 1.26, so each invocation retires a
small bounded quantum, initially at most 64 guest instructions, and then
returns to a normal Go wrapper. No assembly operation may contain an unbounded
probe, string loop, sort, allocation, rehash, or callback.

### Design C: pure-Go runtime JIT

Rejected for this no-CGO plan. A baseline JIT could remove dispatch and might
raise conditional arm64 target confidence. On Darwin/arm64, however, Apple's
supported workflow requires `MAP_JIT`, per-thread JIT write-protection and
instruction-cache APIs from libSystem, and a host-provided entitlement under
Hardened Runtime. See Apple's
[JIT-on-Apple-silicon guidance](https://developer.apple.com/documentation/apple-silicon/porting-just-in-time-compilers-to-apple-silicon)
and
[allow-JIT entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.security.cs.allow-jit).
A syscall-only RW-to-RX substitute is not the supported product path. Dynamic
foreign ABI calls would violate this plan's strict no-CGO/no-FFI contract, and
Ember cannot assign entitlements to embedding host executables. Generated PCs
also have worse Go unwinding and failure behavior. Do not revive this path
without a new explicit user decision.

### Design D: embed upstream Luau

Rejected. It requires C/C++ linkage or an out-of-process runtime, violates the
Go-native project direction and the explicit no-CGO requirement, and would
replace rather than improve Ember's runtime.

## Target architecture

```text
immutable Program / Proto
        |
        v
verify + lower once per Runtime owner
        |
        v
executionImage
  - fixed-width semantic operations and guest-PC map
  - executable Proto records and constants
  - call/arity/result descriptors
  - global/property/metatable/table descriptors
  - owner-local mutable guard/cache state
        |
        +----------------------------+
        |                            |
        v                            v
generated Go burst oracle       Darwin/arm64 Go assembly
portable/correctness path       bounded production adapter
        |                            |
        +------------+---------------+
                     v
                 burstExit
          exact PC/count/effect record
                     |
                     v
              Go effect executor
  allocation, growth, errors, host calls, yield/resume, GC/polls
                     |
                     v
                resume image
```

The private module exposes a narrow conceptual seam:

```go
func prepareExecutionImage(owner *runtimeOwner, program *Program) (*executionImage, error)
func executeImage(image *executionImage, invocation executionInvocation) (executionOutcome, error)
func releaseExecutionImage(image *executionImage) error
```

The backend seam remains private and smaller:

```go
func runGoBurst(request *burstRequest, exit *burstExit)
func runAssemblyBurst(request *burstRequest, exit *burstExit)
```

These are design shapes, not a commitment to exported names. `Program` and
Proto remain immutable/shareable. Every owner-relative constant, cache,
descriptor, stack, table view, and handle belongs to `executionImage` or its
`runtimeOwner`. Public Values and callbacks convert only at explicit host
edges. Constructors and module load do not hide repeated work.

`burstRequest` contains typed pointers plus lengths to immutable operation
arrays and pointer-free mutable arrays. `burstExit` contains only scalars and
handles: reason, image/Proto ID, micro-PC, guest PC, value base/top,
continuation depth, retired guest count, and effect operands. Assembly never
writes a Go pointer, `Value`, slice/map/string header, function value, error,
or interface. It never calls Go or foreign code. The Go caller keeps all typed
owners live through return.

Every request and exit carries an owner cookie plus image ABI/version cookie.
The Go wrapper validates them before entry and after return. Owner-relative
slots do not encode owner identity, so cross-owner import is permitted only
through the explicit public adapter; deliberately mixing owner A's image/slot
with owner B's request must fail before state access.

The request is deliberately the one GC-visible pointer boundary; it is not
itself pointer-free. Its pointer fields are a closed, generated allowlist of
typed bases and lengths. Assembly never mutates or retains them. Every mutable
record reachable through those bases, including exit, stack, continuation,
table, closure projection, global, and property records, is pointer-free. The
Go wrapper increments the active-burst count before entry and executes
`runtime.KeepAlive` for the owner, image, request, and every backing owner after
return, before allowing close or arena replacement.

## Canonical state model

The cutover is complete, not mixed:

- 64-bit `slot` is canonical in registers, constants, globals, stack windows,
  varargs/results, closures, upvalues, continuations, coroutine state, module
  exports, callback staging, table keys/values, and descriptor payloads;
- the existing NaN-tagged handle encoding is retained only after exact NaN,
  typed-nil, stale-handle, generation-wrap, and identity tests pass;
- Go pointers remain visible in an owner/root slab. Hot arrays contain slots,
  integers, offsets, versions, and handles, not hidden `uintptr` pointers;
- one flat value stack and one compact continuation stack replace recursive
  `vmFrame` reconstruction. Direct calls, recursion, returns, tail calls,
  fixed/open results, and varargs update indices in those arrays;
- coroutines own stable value/continuation blocks. Yield/resume changes active
  ownership through Go; it does not copy a pointer-bearing frame graph;
- tables use an assembly-readable directory plus dense array and ordered
  open-addressed hash storage. Fast hits are bounded. Growth, rehash, delete,
  and structural mutation side-exit to Go and refresh descriptor versions;
- interned strings and object handles provide stable equality/hash identities.
  Allocation or long string work remains a Go effect;
- property/global/metatable descriptors contain complete guarded lookup state,
  including finite chain targets and version checks. A stable scripted target
  enters through the native script-call path rather than exiting merely because
  a metatable is present.

The current pointer-rich `slotSlab[*Table]`, `slotSlab[*closure]`,
`slotSlab[*cell]`, pointer-key maps, and nested Go slice/map/string/function
headers are not assembly-visible hot state. Go may retain them temporarily as
the public/root bridge during migration, but the backend sees scalar table,
closure, cell, module, and descriptor projections indexed by handles. A
structural reflect/unsafe test rejects pointers, maps, slices, strings,
interfaces, functions, and hidden `uintptr` fields from every mutable
assembly-visible type; only the request's generated typed-base allowlist is
exempt.

### Entry, ownership, and preparation lifetimes

The speed and allocation gates distinguish cold adapters from warmed
execution:

| Entry | Owner/image lifetime | Timing/allocation treatment |
| --- | --- | --- |
| `Compile` | creates immutable Program/Proto and shareable lowered-code cache; no Runtime handles | compiler work, outside runtime target |
| `Run` / `RunWithGlobals` | explicit cold convenience adapter creates an owner-bearing result lease; owner-neutral lowered code is shared, owner state binds once, and graph-valued results remain valid until explicit result close | preparation/result-lifetime work is reported as intercept/cold cost and must stay within the frozen stateless allocation ceiling; globals write back on every semantic exit |
| persistent `Runtime.RunHook` | one owner and bound execution image from load until `Runtime.Close` | image preparation is outside the timed hook; repeated hooks are the primary warm zero-growth/zero-plumbing-allocation lane |
| `Callback.Call` | re-enters its captured owner/image under the existing busy/lease rules | no image rebuild; public args/results convert only at the callback edge |
| `require` / module hook | uses dense owner module IDs and slot caches; cold policy maps may resolve names, while loaded/active state and exports are owner slot arrays | no module relowering or Value conversion on a warmed hit; preserve cycles, inherited frames, and cached identity |
| coroutine resume | resumes stable owner slot/continuation blocks | transition is a Go effect; no image rebuild or frame graph copy |

The all-37 slope harness uses one literal point boundary. Before timing, it
builds the engine-neutral input that can be prepared externally: compile the
immutable Ember Program and write the Luau source file. One Go monotonic parent
timer starts immediately before invoking `Run` or starting the Luau command and
stops only after result parse/validation and Ember Result close or Luau process
exit. Ember owner/image binding and unavoidable Luau child startup/compilation
are therefore inside the complete point invocation and appear as N-invariant
fitted intercepts. Neither engine prepares inside the N-iteration guest body.
A separate persistent Runtime lane measures an already prepared image directly
and is never mixed into the stateless slope. Every baseline/candidate uses the
same start/stop event trace; no side silently excludes lifecycle work.

Public identity is an explicit bridge, not a copy accident. A public `Table`
becomes a stable owner-backed view/pin for one table handle under an explicit
Result/Runtime lease; cross-owner use fails unless the caller requests the cold
cycle-preserving detach/copy API. `TableValue(t).Table()`
identity, host `Table.Get`/`Set`, mutation retained across callbacks, equal
strings from distinct public constructors, userdata identity, callback leases,
cross-owner import, and post-close behavior all have tests before cutover.
Pointer-free internals do not silently invalidate existing host-visible object
identity. The user has authorized backwards-incompatible changes when they are
needed for the speed architecture, but each such change requires an explicit
ADR/public-surface migration rather than an incidental representation break.

### Entry/cutover matrix

Before old state is deleted, every current caller has one named replacement:

| Current path | Replacement obligation |
| --- | --- |
| `Run` / `RunWithGlobals` -> `executeProto` | cold owner-bearing result lease plus prepared shareable code, explicit close, and exact globals mutation journal |
| Runtime hooks -> `executeProtoWithInvocationScope` | persistent owner/image invocation with slot globals and controller |
| `module_runtime.go` entry | same persistent image, dense module record, inherited invocation/error context |
| `Callback.Call` -> `executeProtoWithInvocationScope` | captured-owner lease and slot/public edge adapter |
| require -> `runModuleWithContextGlobalsController` | dense module cache/active-cycle state and exact error inheritance |
| coroutine `vmSuspendedFrames` / `runUntilDepthResult` | stable slot and continuation blocks plus Go transition record |
| direct loop `vmFrame` / `vmFrameRecord` | flat stack and pointer-free continuation records |

Tests must prove each replacement through its public entry and a structural
symbol/call-path check must show the old route unreachable before deletion.

## Native semantic island and coverage budget

The arm64 backend is credible only if it covers the general mechanisms that
dominate dynamic execution. At minimum it must execute without a Go transition:

1. slot moves, constants, truth tests, numeric arithmetic/comparison, branches,
   numeric loops, and exact guest-instruction retirement;
2. direct closure call, fixed call, return, tail call, recursion, open/fixed
   results, vararg pack/unpack, and continuation management;
3. closed/open upvalue reads/writes and already-created closure invocation;
4. stable global descriptor get/set;
5. bounded array/hash hit, existing-slot write, own-field/property-chain hit,
   and stable scripted `__index`/`__newindex` target invocation;
6. deterministic stable array/generic table iteration.
7. bounded pure slot-ABI intrinsics selected by semantic identity, not source:
   `rawlen`, `select` index/count, numeric `math.min`/`math.max`, `next` and the
   stable portions of `pairs`/`ipairs`; and existing-capacity table insert/
   remove only if their copy count fits the primitive-work budget. Allocation,
   growth, setmetatable invalidation, coroutine APIs, unsupported numeric
   domains, and opaque native functions are deliberate Go effects.

Go side exits are reserved for allocation, table growth/rehash/delete,
unbounded string work, unknown/host calls or metamethods, module initialization,
errors/protected-call transitions, debug hooks, coroutine yield/resume,
cancellation, collection, and descriptor misses that genuinely change state.

Operations without a bounded hardware implementation are honest numeric or
string effects. In particular, pow/transcendentals and long concat/format work
side-exit to exact Go helpers unless a separately reviewed bounded software
implementation exists. Hardware-bounded scalar operations and deliberate
numeric effects have separate denominators. Interned string/key compare/hash
hits and string allocation/long traversal likewise have separate denominators.
Stable scripted metamethod descriptors are supported for every applicable
semantic class (field/index, arithmetic/comparison, concat, length, and call)
through the same direct script-call path when semantics allow; otherwise their
attempts remain visible effect exits.

One generated metadata table assigns every executable opcode, lowered
operation, intrinsic, metamethod attempt, and exit to exactly one semantic
family and one eligibility/effect class. Reports emit every declared family,
including zero counts; a required family with zero executions fails. `pairs`,
`ipairs`, `next`, direct generic iteration, and custom `__iter` are separate
subclasses. All metamethod kinds (`__index`, `__newindex`, `__len`, `__iter`,
`__tostring`, `__call`, arithmetic, concat, relational, and equality) retain
ordering/error/pcall/limit differential vectors even when their honest class is
a Go effect.

Frozen and held-out captures must meet all of these class-based floors:

- >=99% of value/control operations stay in the selected burst backend;
- >=95% of direct script call/return/tail/vararg edges stay in the backend;
- >=95% of already-created script closure invocations and >=90% of upvalue
  reads/writes stay in the backend;
- >=90% of all global/property/table/metatable attempts stay in the backend;
  stable and unstable/miss attempts are also reported as subcounters and may
  not be removed from the all-attempt denominator;
- >=95% of stable iteration steps stay in the backend;
- >=90% of backend-eligible interned string/key operations stay in the backend
  when that class executes; long/allocation string effects are counted
  separately and included in end-to-end timing;
- backend-eligible miss/unsupported exits <=5% of eligible dynamic operations
  per acceptance row and semantic family, and <=2% wherever the frozen
  baseline says the target needs more than a 2x Ember speedup;
- all Go transitions per 1,000 guest operations and mandatory-effect
  transitions per 1,000 guest operations are reported separately. Mandatory
  host/allocation/coroutine/error effects are not charged to the eligibility
  miss numerator, but their end-to-end time and allocation remain in every
  speed row and a predeclared generated-family effect budget may not be changed
  after measurement.

Coverage floors count backend-eligible operations. Semantically mandatory host
callbacks, coroutine transitions, debug hooks, errors, and allocation events
are reported in a separate effect denominator and must be correct and
allocation-bounded, but a generator intentionally issuing a host callback on
every operation is not required to pretend those callbacks can stay in
assembly. The <=2x product claim is for the frozen all-37 corpus; it is not a
universal performance promise for arbitrarily host-dominated scripts. This
qualification does not allow an otherwise eligible script call, table hit, or
property hit to be relabeled as an effect.

Every executed guest operation is classified exactly once as native-complete,
deliberate semantic effect, descriptor/shape miss, unsupported backend
operation, or failure. Quantum returns are transitions, not operations. An
effect transition that completes an unretired operation keeps that operation's
single classification; it is not counted as a second operation. Reports
include native and each exit category per 1,000 guest operations, transitions
per burst, guest operations per burst, and primitive work per burst. The <=2%
tighter transition threshold is selected from a predeclared generated
class-family whose clean Ember/Luau ratio requires more than a 2x Ember
speedup, never from a named acceptance row after results are visible.

Coverage counters are keyed by semantic/effect/guard class, never a source or
case identity. Diagnostic builds may label externally measured rows after
execution, but those labels never enter the runtime.

## Agent and ownership strategy

This is high-risk, cross-cutting work. Use one implementation owner for each
hot runtime file group and a Sol review at every architectural retention gate.
Every agent re-reads `AGENTS.md`, preserves user work, works on `main`, and
does not stage files outside its assigned slice.

Work classification:

- scope: whole runtime execution core plus performance tooling;
- uncertainty: high until the Phase 1 native vertical proof;
- coupling: compiler metadata, VM semantics, ownership/GC, calls, tables,
  coroutines, callbacks, and host entrypoints;
- risk: very high for semantic drift and Go runtime integration, with explicit
  kill gates before irreversible production routing.

| Work | Assigned agent | Why |
| --- | --- | --- |
| Harness, statistics, manifests, docs | Luna high | Exact bounded tooling work |
| Generated semantic corpus and differential oracle | Luna max | Broad correctness and anti-overfit design |
| Execution image, slot ownership, stacks, tables | Luna max | Cross-cutting performance-critical rewrite |
| ARM64 ABI/dispatch proof and backend | Luna max | Native correctness and measurement uncertainty |
| Architecture/kill-gate review | Sol high | Reject locally attractive but insufficient paths |
| Final integrated correctness/speed review | Sol high | Independent challenge of completion evidence |

Parallel work is allowed only across disjoint ownership. Harness scripts/tests
may proceed beside a runtime slice. No two workers edit `vm.go`,
`vm_dispatch_template.go.tmpl`, `bytecode.go`, `slot.go`, `runtime_heap.go`, or
the generated backend files concurrently. Assembly layout changes and Go state
layout changes are one retention group with one owner.

## Dependency graph

```text
Phase 0: trustworthy all-row + held-out gates
        |
        v
Phase 1: general assembly/ABI vertical proof
        |
        +-- fails required multi-x class gains --> STOP, report constraint
        v
Phase 2: canonical execution image + slots + flat calls + packed tables
        |
        v
Phase 3: full generated Go engine, then production cut to it
        |
        +-- all rows already pass --> reject assembly, finish
        v
Phase 4: bounded ARM64 backend across the full semantic island
        |
        +-- coverage/safety/speed fails --> delete assembly; target unachieved
        v
Phase 5: delete old oracle, verify twice, document result
```

The Phase 1 proof happens before the expensive production cutover. It is
deliberately broad enough to include calls and table/property hits; an
arithmetic-only assembly microbenchmark does not authorize Phase 2.

## Phase 0: Make success and overfitting measurable

### Slice 0.1 - Reconcile the tree and freeze the no-CGO target

Assigned agent: primary orchestrator, reviewed by Sol high.

Objective and reason: create one clean source fingerprint and explicit target
contract before any optimization. The current dirty tree cannot be acceptance
evidence and overlaps the planned implementation.

Files and symbols:

- current user-owned files listed under Repository evidence;
- `performance-audit.md`;
- `docs/adr/0007-runtime-speed-no-cgo.md` (new);
- `docs/checks.md`;
- `scripts/check-purego`.

Ordered work:

1. Record `git status --short --branch` and the current HEAD. Do not alter the
   listed dirty files. Wait for the owner to resolve them before a clean
   baseline or overlapping edit.
2. Add ADR 0007 with the target platform/hash, per-row 1.85/2.00 gate,
   allocation lifetime rule, breaking-change authority, strict no-CGO/no-FFI
   boundary, generality contract, selected assembly architecture, and explicit
   rejection of upstream embedding and Darwin JIT.
3. Link ADR 0007 from `docs/README.md`, `docs/design.md`,
   `performance-audit.md`, and the two runtime-speed plans without deleting
   historical evidence.
4. Extend `scripts/check-purego` with a mandatory no-foreign-runtime lane. It
   must reject repo-owned production `.c`, `.cc`, `.cpp`, `.cxx`, `.m`, and
   `.mm`, archive, or foreign-object files; `import "C"`; C/linker flags;
   `//go:cgo_import_dynamic`; `//go:wasmimport`; private-runtime or foreign
   `go:linkname`; dynamic-loader and executable-memory/JIT APIs such as `dlopen`, `MAP_JIT`,
   `PROT_EXEC`, and executable `mmap`; helper-process APIs such as `os/exec`,
   `syscall.Exec`, and fork/exec orchestration; and foreign `CALL`/`BL` targets
   from Ember-owned `.s` files. The Luau executable is allowlisted only in
   measurement/test scripts. The final linked-symbol check is scoped to
   Ember-owned objects and dynamic-import directives, because the Go runtime
   itself legitimately imports Darwin system symbols.
5. Add fail/pass fixtures for every forbidden mechanism. Scope structural
   pointer/`uintptr` checks to execution-image, backend-visible, request, and
   exit records; public edge adapters may continue to use pointer identity.
   The gate must be invoked by `scripts/check`, not left as an optional audit.
6. Commit only the ADR/link/check changes after focused checks. Do not stage
   unrelated user work.

Verification:

```sh
scripts/check-purego
scripts/check-purego --self-test
git diff --check -- docs/README.md docs/design.md docs/checks.md \
  docs/adr/0007-runtime-speed-no-cgo.md performance-audit.md \
  runtime-speed-2x-luau-implementation-plan.md
```

Completion criteria:

- clean baseline source is identified and user work is preserved;
- target/no-CGO/generality rules are fail-closed, fixture-tested, and invoked
  by the normal repository check;
- no acceptance capture is labeled clean while runtime files are dirty.

Risks and dependencies:

- this slice is blocked on resolving overlapping user edits, not on planning;
- do not solve the conflict with a reset, checkout, temporary branch, or
  selective staging of someone else's hunks.

### Slice 0.2 - Add a real all-37 speed2x acquisition phase

Assigned agent: Luna high.

Objective and reason: make the official command measure all unique Top10,
Classic, and Scenario rows with a schedule that is stable, affordable, and
valid for both fast and recursive programs.

Files and symbols:

- `runtime_parity_test.go`: `parityCaseSelection`, `parityIterations`,
  `TestRuntimeParityLive`, raw metadata, fit validation;
- `top10_luau_benchmark_test.go`: `top10LuauCases`, `classicLuauCases`,
  `scenarioLuauCases`;
- `scripts/check-runtime-parity`: phase parsing, fingerprint, capture command;
- `scripts/scenario-ratio-gate`: schedule parsing, inventory, line fits;
- new focused shell/Go fixtures beside the existing parity tests;
- `testdata/runtime-parity/speed2x-schedule-v1.tsv` after clean calibration;
- `.github/workflows/scheduled.yml` only after local proof.

Ordered work:

1. Build one ordered all-37 manifest from the three existing case tables.
   Assert the exact disjoint corpus counts Top10=10, Classic=2, Scenario=25,
   exact aggregate count 37, unique `(corpus,name)` and globally unique names,
   stable order, expected result, source hash, and result hash. Never dedupe a
   collision into a passing count. Empty selection becomes an error;
   `--phase full` passes an explicit Scenario inventory for its historical
   purpose, while `--phase speed2x` passes the explicit all-37 manifest. Generate
   or export this one manifest once; Go acquisition and the gate consume and
   verify its same content hash rather than maintaining a second AWK name list.
2. Add `--phase speed2x` with median 1.85 and p90 2.00. Leave existing phase
   semantics intact until their callers migrate; do not redefine `full`
   silently.
3. Add an explicit calibration mode:
   `--calibrate --schedule-out PATH`. For each row, a deterministic geometric
   pilot selects the same four strictly increasing positive integer N values
   for both engines, with `Nmax/Nmin >= 16`. Every timed point for each engine
   must be between 1 ms and 250 ms, the elapsed values must span at least eight
   measured timer ticks with at least three distinct nonzero deltas, and the
   largest point's fitted body share
   `Nmax*slope/(entry+Nmax*slope)` must be at least 0.80. If no four-point
   schedule meets all rules, calibration fails; there is no slow-row exception
   or candidate-time recalibration. Apply the same pre/post CPU-load and
   contamination checks as live acquisition; store pilot raw samples/status in
   the schedule artifact and reacquire the entire calibration if contaminated.
4. Standardize both engines on the exact external Go monotonic boundary above:
   start immediately before Ember `Run` or Luau command start; stop after
   result parse/validation and Result close or child exit. Remove Luau
   `os.clock()` from ratio computation. Record prebuilt-input hashes, timer
   start/stop event IDs, implementation/resolution/quantization, and which
   N-invariant preparation remained inside the invocation; reject points at or
   below the declared resolution. A fixture traces and compares the event
   sequence for every N, engine, baseline, and candidate.
5. Require live acquisition to name the immutable schedule and a fresh
   replicate ID: `--schedule PATH --replicate ID`. Validate IDs with
   `[a-z0-9][a-z0-9-]{0,31}` and write under
   `tmp/runtime-parity/speed2x/<fingerprint>/<replicate>/`. Existing replicate
   directories fail instead of being reused. Bind the schedule SHA-256,
   calibration source commit, manifest SHA-256, and acquisition schema version
   into every raw header and fingerprint.
   After baseline review, check the versioned schedule into `testdata` with its
   calibration raw-artifact SHA and source commit recorded in ADR 0007. CI and
   candidates refuse a missing or hash-mismatched checked-in schedule; `/tmp`
   output is only for the harness smoke test before that freeze.
6. Acquire exactly nine independent paired fits, organized as three rounds of
   three. Use a predeclared balanced order that alternates the first engine at
   every fit and N point. Each fit times all four scheduled N
   points once for each engine and produces one slope ratio; there is no hidden
   median-of-repeats inside a pair. For each engine/fit, use ordinary least squares
   `T=entry+N*inner` over exactly four points. Require positive slope,
   nonnegative fitted entry, R-squared >=0.995, maximum absolute residual <=5%
   of fitted elapsed, adjacent interval slopes all positive and within 10% of
   the fitted slope, and a two-sided 95% Student-t confidence interval with
   two residual degrees of freedom whose half-width is <=5% of slope. Define
   cross-fit spread as `(max(slope)-min(slope))/median(slope)` separately per
   engine and require <=0.20.
   A row ratio is the paired slope ratio; its median is the fifth sorted value
   and nearest-rank p90 of nine is the ninth value (the maximum).
   Instrumented calibration also rejects a schedule where a one-time stack/
   arena capacity transition or GC regime discontinuity dominates one point;
   ordinary workload-proportional allocation remains measured. The public
   lifecycle and wrapper must otherwise be identical at all four points.
7. Extend the raw schema with ordered corpus manifest/counts/hashes, per-row N
   schedule, schedule/calibration hashes, source commit and clean status,
   recursive production/harness fingerprint, Go toolchain, OS/CPU, timer,
   `CGO_ENABLED`, `GOMAXPROCS`, Luau binary hash, exact Luau argv/default flags
   and verified codegen-disabled mode, acquisition version, all threshold
   constants, and quiet-system samples. Fingerprint all tracked production,
   lowering, generator, harness, `.s`, template/spec, module, and relevant
   script files recursively plus HEAD. A speed2x capture requires a clean tree;
   `--allow-dirty` records a diff hash and is permanently non-acceptance.
8. Make each replicate a real acquisition. The script may validate or report a
   named existing artifact only through a separate read-only command; a normal
   capture never follows the old "one completed acquisition per fingerprint"
   reuse path. Schedule, manifest, argv, threshold, clock, or environment
   mismatch invalidates the whole replicate. Preserve and report all nine raw
   ratios; do not aggregate them before the per-row median/p90 gate.
9. If process startup dominates acquisition, batch points only after a test
   proves every timed point gets fresh VM/global state. Do not trade validity
   for fewer Luau process launches.
10. Add self-tests for corpus-count/name collisions, missing rows, changed
    schedules, candidate recalibration, nonlinear/nonpositive fits, exact CI
    and nearest-rank boundaries, timer event-order/quantization, contamination, result
    mismatch, threshold/spread failure, unsupported metadata, attempted
    replicate reuse, and two commands proving distinct output acquisitions.
11. Implement manifest parsing, OLS/CI, finite-number checks, and percentile
    decisions in a standard-library Go helper with the pinned toolchain. Shell
    scripts only orchestrate acquisition; BSD/GNU awk differences cannot
    change acceptance. Bind the helper source/version hash into the artifact.
12. From the calibrated pilot, compute a fail-closed worst-case acquisition
    budget including Luau launches and 25% runner margin. Split independent
    replicates into separate scheduled jobs when needed, raise the current
    45-minute timeout explicitly, and upload each raw replicate plus schedule
    even on gate failure. A timeout is a failed/missing capture, never partial
    evidence.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestParity|TestRuntimeParity)' ./...
scripts/check-runtime-parity --self-test
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x --calibrate \
  --schedule-out /tmp/ember-speed2x-schedule.tsv
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x \
  --schedule /tmp/ember-speed2x-schedule.tsv --replicate harness-smoke-a
scripts/check-fast
```

The live command requires the pinned quiet runner and is allowed to produce a
baseline failure; its artifact must be structurally valid and complete.

Completion criteria:

- one official command measures exactly 37 unique rows;
- schedules are baseline-frozen, content-addressed, uniformly calibrated, and
  never regenerated by a candidate capture;
- every row has nine valid paired ratios and the gate fails on any omission;
- two replicate IDs necessarily perform two independent acquisitions;
- the previous nonexistent `speed2x` command is now executable.

Risks and dependencies:

- calibration itself can overfit if rerun for each candidate; freezing and
  fingerprinting are mandatory;
- threshold values must be fixed in ADR 0007 before candidate capture;
- shell arithmetic must remain portable to the repo's existing POSIX style.

### Slice 0.3 - Add class-balanced held-out generation and differential oracles

Assigned agent: Luna max.

Objective and reason: prove mechanisms generalize beyond the visible corpus and
catch semantic drift before native execution can corrupt state.

Files and symbols:

- existing execution differential/fuzz tests discovered by `rg` at execution;
- `bytecode.go`: `opcodeMetadataTable`, operand/effect metadata;
- `opcode_info.go`, `register_effects.go`;
- new `execution_generated_corpus_test.go`;
- new deterministic generator helpers in test-only Go files;
- new backend anti-overfit source scan test.

Ordered work:

1. Define semantic families from existing opcode/effect metadata: scalar and
   truth behavior; CFG/loops/iteration; globals/fields/property chains;
   fixed/tail/open/vararg/multi-result calls; closures/upvalues; arrays/hash
   tables/metatables; errors/protected calls/limits; coroutine and host edges.
2. Create a deterministic grammar that emits valid Luau source and, where
   needed, verified execution-image states. Control depth, object count, loop
   bounds, and recursion so every generated case terminates.
3. Keep development seeds visible for debugging. Maintain three one-use
   held-out epochs: Phase 1 proof, Phase 4 retention, and final cutover. Derive
   each epoch only at its named freeze point, generate exactly 5,000 valid
   programs after deterministic rejection filtering, and freeze generator
   version, seed derivation, accepted count, rejection counts, and corpus hash
   before execution. Once revealed, those seeds become ordinary regression
   seeds and can never serve as held-out evidence again. A failure may be fixed
   against the revealed regression set, but the next retention decision must
   use its already-reserved untouched epoch. The backend never receives seed,
   source name, epoch, or family label.
4. Compare canonical Go execution, selected backend, and pinned Luau where the
   external oracle supports the behavior. Check exact values/NaN bits, object
   identity, table mutation/order, metamethod order, callbacks, yields/resume,
   limits/cancellation, and error frames/causes.
   For Ember-only host, cancellation, owner/result-lifetime, limit, and raw
   state vectors, generate an expected event/state trace from a small pure
   reference model over grammar nodes. It may share value-format constants but
   not production lowering, dispatch, descriptor, table, or effect helpers.
5. Add state-level differential vectors for every burst operation and every
   exit reason. Invalid images are rejected before execution, never dispatched
   into assembly.
6. Add a recursive static test over production Go, generated Go, `.s`, build-
   tagged fallback, generator templates/specs, and linked Ember symbol/string
   data. Reject the 37 names, source/result hashes, fixture paths, import/exec/
   environment APIs used for source fingerprint routing, and prohibited
   identity logic. Corpus labels may exist only in measurement, test, and docs
   paths. False positives are resolved by moving labels to those paths, not by
   broad exceptions.
7. Record semantic-class dynamic counts and backend/side-exit classification.
   Zero executed members in a required family is a generator failure.
8. Add a small, deterministic class-performance inventory generated from the
   same grammars. Each source must run unchanged on current Ember, the proof
   engine, and pinned Luau, with enough work to amortize entry. Keep this
   inventory broad and class-balanced; it supplies same-program baseline ratios
   for the Phase 1 gain formula and is not an acceptance replacement.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestExecution.*Differential|TestGeneratedExecution|TestBackendHasNoCorpusFingerprints)$' ./...
CGO_ENABLED=0 go test -race -run '^(TestExecution.*Differential|TestGeneratedExecution)$' ./...
CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 -run '^(TestExecution.*Differential|TestGeneratedExecution)$' ./...
```

Completion criteria:

- all semantic families have nonzero class-balanced coverage;
- held-out seeds are created after mechanism freeze and are absent from backend
  inputs;
- exact semantic differential is 100%; there is no accepted mismatch budget;
- production source contains no benchmark identity or fingerprint path.

Risks and dependencies:

- generated programs can accidentally test only easy monomorphic states;
  require shape churn, misses, unknown calls, errors, and mutation boundaries;
- upstream Luau is an oracle only in test tooling and must not leak into build
  or runtime dependencies.

### Slice 0.4 - Capture clean mechanism, speed, and allocation baselines

Assigned agent: primary orchestrator, reviewed by Sol high.

Objective and reason: replace dirty exploratory ratios with evidence that gives
each semantic class an explicit required speedup and allocation ceiling.

Files and components:

- `performance-audit.md`;
- `scripts/performance-audit` and compare/manifest tools;
- new `execution_cold_benchmark_test.go` and
  `scripts/execution-allocation-gate`;
- `testdata/runtime-performance/all37-allocation-v1.tsv` after baseline review;
- new `scripts/execution-code-size-gate`;
- the new `speed2x` parity artifacts;
- low-distortion counters in diagnostic-only generated VM variants;
- CPU profiles for direct loop, calls, tables/properties, iteration, strings,
  coroutines, and host boundaries.

Ordered work:

1. On the clean target commit, take two same-source Ember audit captures under
   `CGO_ENABLED=0 GOMAXPROCS=1` and derive the existing diagnostic manifest.
2. Add a separate all-37 allocation artifact. For every manifest row capture
   stateless cold `Run`/`RunWithGlobals` as applicable and persistent prepared
   Runtime/hook execution where applicable, with exact B/op and allocs/op from
   two baselines. Use `testing.AllocsPerRun` and explicit owner/image allocation
   counters around preparation/release and steady invocations; use logical
   arena capacity snapshots for retained bytes rather than relying on noisy
   process-wide `runtime.MemStats` alone. Classify public-result and host-edge
   conversions separately but never omit them from their API lane.
   Use the exact speed2x source wrapper, checked-in calibrated N schedule, and
   lifecycle. Normalize both per public invocation and per retired guest
   operation; also retain a separate repeated-hook no-growth lane.
3. Add a fail-closed cold image/arena schema. `guestWordCount` means every
   verified lowered word in all reachable executable Protos; `protoCount`
   means every reachable validated executable Proto including nested closure
   and module Protos. The 32-byte/word +512-byte/Proto image cap includes all
   immutable code, maps, descriptors, constants, and image sidecars. Owner
   value/continuation/table/control/order/root arenas use the separately stated
   scalar-layout +12.5% +4 KiB formula. Record logical length, capacity, element
   size, retained bytes, and allocation count for each arena after forced GC;
   no unreported sidecar or allocator-slack exclusion is permitted.
4. Add a warmed lifecycle gate: explicit prepare occurs once, the persistent
   Runtime stores one owner-bound image, repeated hooks preserve its identity,
   and no backend-specific build/allocation occurs after warm-up. Static
   assembly has no code generation; any descriptor warm-up must break even by
   ten invocations or 10,000 retired guest operations, whichever comes first.
5. Add a linked-binary code-size artifact using a versioned `go tool nm -size`
   parser plus Darwin segment inspection. It measures the named backend hot
   symbols (every accepted handler, wrapper, table, relocation-owned data, and
   dispatch rodata, not only a selected hot symbol) and fails above the
   pre-candidate 256 KiB cap; `otool -l` must show
   ordinary RX `__TEXT` and no writable-executable segment.
6. Take two complete `speed2x` captures using the frozen schedule with distinct
   replicate IDs. If either is contaminated or fails fit-quality rules,
   discard the whole attempt.
7. Capture diagnostic semantic-class counts: guest operations, direct versus
   dynamic call edges, fixed/open/vararg results, table hit/miss/grow,
   property-chain depth and descriptor stability, iteration class, metamethod
   target type, coroutine/host boundaries, and current allocations.
8. Run the generated class-performance inventory on current Ember and pinned
   Luau with the same calibrated paired method. For each semantic family,
   compute the required Ember reduction on the same programs:
   `candidate/current <= 1.75 / baseline_ratio` for prototype headroom and
   `<= 1.85 / baseline_ratio` for production median. Store the formula and
   data, not named optimization instructions. Do not infer a family ratio by
   assigning one mixed acceptance row to one cause.
   Freeze a machine-readable semantic-family manifest containing metadata
   membership, baseline ratio/interval, required proof gain, and whether its
   eligible-exit cap is 5% or 2%. Candidate reports and gates consume this same
   hash; no family is reclassified after results appear.
9. Freeze per-row/per-lifecycle B/op and allocs/op ceilings from the two all-37
   allocation captures. A candidate may not increase either value on any row;
   the five-family `performance-audit` remains diagnostic and cannot certify
   the other rows. Check the reviewed manifest into `testdata` with source,
   schedule, wrapper, and raw baseline hashes; candidates/CI fail on mismatch.
   Logical owner/image counters must match exactly across baselines; measured
   B/op must agree within 1% and allocs/op within the reporting resolution or
   the baseline is reacquired. Freeze the lower observed value for each metric,
   never the noisier higher result.
10. Record profiles and counts in `performance-audit.md`, clearly separating
   acceptance data from diagnostic attribution.

Verification:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 scripts/performance-audit \
  --output /tmp/ember-speed2x-base-a --profiles
CGO_ENABLED=0 GOMAXPROCS=1 scripts/performance-audit \
  --output /tmp/ember-speed2x-base-b --profiles
scripts/performance-audit-derive-manifest \
  --before /tmp/ember-speed2x-base-a \
  --after /tmp/ember-speed2x-base-b \
  --output /tmp/ember-speed2x-audit-gates.tsv
CGO_ENABLED=0 GOMAXPROCS=1 scripts/execution-allocation-gate \
  capture --manifest all37 --replicate baseline-a
CGO_ENABLED=0 GOMAXPROCS=1 scripts/execution-allocation-gate \
  capture --manifest all37 --replicate baseline-b
CGO_ENABLED=0 GOMAXPROCS=1 scripts/execution-allocation-gate \
  freeze --before baseline-a --after baseline-b \
  --output testdata/runtime-performance/all37-allocation-v1.tsv
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x \
  --schedule testdata/runtime-parity/speed2x-schedule-v1.tsv \
  --replicate baseline-speed-a
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x \
  --schedule testdata/runtime-parity/speed2x-schedule-v1.tsv \
  --replicate baseline-speed-b
CGO_ENABLED=0 scripts/execution-code-size-gate --baseline
```

Completion criteria:

- two clean, compatible all-row speed artifacts and two Ember audit artifacts
  exist at one source fingerprint;
- every family has a required speedup and coverage denominator, and every one
  of the 37 rows/lifecycles has an allocation ceiling;
- cold formulas, warmed image identity, warm-up break-even, and the static text
  cap have executable gates;
- instrumentation is absent from the production loop and measured separately.

Risks and dependencies:

- target runner time is substantial; partial data cannot authorize Phase 1;
- if the baseline changes before the architecture proof, reacquire it rather
  than mixing commits.

## Phase 1: Prove the bold mechanism before rewriting the runtime

### Slice 1.1 - Freeze the execution-image and burst ABI contract

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: define one pointer-free contract that both the Go oracle
and assembly backend can implement, without first changing production VM state.

Files and symbols:

- `bytecode.go`: opcode/effect/operand metadata;
- `vm_dispatch_spec.go` and `cmd/ember-vmgen/main.go`;
- `slot.go`;
- new `execution_image_spec.go` and `execution_burst_abi.go`;
- new generated layout/assertion tests;
- no production call site changes in this slice.

Ordered work:

1. Define dense micro-operation classes, operands, guest-PC mapping, guest
   instruction cost, effect class, maximum bounded work, and legal exit reasons.
   Initial lowering is one semantic operation per guest rule; do not add
   workload-shaped fusions.
2. Define fixed-width execution words and pointer-free descriptor/continuation
   records. Use offsets/indices and typed Go-owned base arrays, never persistent
   raw pointers or hidden `uintptr` roots.
3. Define `burstRequest` and `burstExit` with explicit size/alignment/offset
   constants generated from one spec. Add compile-time and runtime assertions
   for Go and arm64 layout. The request is a typed-pointer boundary, not a
   pointer-free record: every base is a generated typed pointer plus logical
   length/capacity, is read-only as a pointer value in assembly, and is kept
   live by the Go wrapper. Mutable arrays reached through it and the entire
   exit are pointer-free.
4. Specify verifier invariants: opcode bounds, operand/register bounds, valid
   block targets, descriptor kinds, continuation capacity, table probe bound,
   guest-PC mapping, and maximum work per operation.
5. Specify two independent budgets: at most 64 retired guest instructions and
   at most 256 primitive work units per burst. One scalar operation costs one
   unit; each copied slot, hash probe, property-chain step, upvalue close, or
   cleared cell costs one. Freeze native maxima of 16 hash probes, eight
   property-chain steps, and 64 copied/cleared slots per guest operation.
   Oversized work exits to Go before mutation or uses a reviewed resumable
   micro-phase; it never runs an unbounded assembly loop.
6. Retire a guest instruction exactly once, only after its semantic mutation is
   complete. A pre-effect exit reports it unretired; a Go-completed effect marks
   it retired before resume. Prefer a pre-mutation Go exit for work above a
   native bound. Any later resumable native micro-phase must carry explicit
   `{phase,instructionCharged}` state: charge MaxInstructions once before its
   first chunk, never recharge on quantum resume, expose no host-observable
   partial result, and finish or roll back before cancellation/error delivery.
   Limit/error tests cover every cut point.
7. Extend `cmd/ember-vmgen` or a sibling standard-library-only generator so
   enum/layout/effect metadata is shared by Go and assembly outputs. Generated
   files are checked in and stale generation fails tests.
8. Add tests that use reflection/unsafe only in test code to prove every
   assembly-mutable record is pointer-free and its layout matches generated
   offsets. Separately assert that `burstRequest` contains only its generated
   typed-pointer/length allowlist and that assembly never writes those fields.
9. Generate descriptor maxima, operation/cost tables, owner/image cookies, and
   the complete root/liveness manifest from the same spec. The guest cost of an
   opcode matches current semantics: one charge for the executable opcode;
   AUX/auxiliary words decoded by that opcode are not independently charged.
   Fused/lowered micro-ops preserve that one semantic charge and exact guest PC.

Verification:

```sh
go generate ./...
CGO_ENABLED=0 go test -run '^(TestExecutionImageSpec|TestBurstABI|TestGenerated.*Current)$' ./...
go vet ./...
git diff --check
```

Completion criteria:

- one reviewed ABI covers Go and arm64 without benchmark knowledge;
- every operation is bounded or explicitly side-exiting;
- verifier rejects malformed images before any assembly call;
- layout generation is deterministic and standard-library only.

Risks and dependencies:

- a too-wide word restores bandwidth pressure; start with a measured compact
  layout and cap at 16 bytes per operation unless evidence requires an escape
  record;
- exposing pointers to pointer-bearing Go objects or retaining any typed base
  past return is an immediate design failure;
- this spec is provisional until Slice 1.3 passes and must be deleted if the
  architecture is killed.

### Slice 1.2 - Prove linked arm64 dispatch, ABI, and bounded preemption

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: exercise the exact native mechanism on the target before
committing to the runtime rewrite. This is a general backend proof, not a
single arithmetic benchmark.

Files and components:

- new root-package proof files behind the explicit
  `asmprobe && darwin && arm64` build constraint, using the exact unexported
  request/state types but with no production routing;
- generated arm64 layout include;
- Go reference proof kernel;
- arm64 `.s` leaf burst function;
- no production runtime routing.

Ordered work:

1. Implement the stable ABI0 Go declaration and leaf assembly function. Let
   the compiler provide the ABIInternal wrapper; do not use `go:linkname` or
   private runtime ABI symbols. First prove by source review and disassembly
   that the leaf neither stores nor returns any request-derived pointer and
   that the wrapper keeps every typed base live through return. Then apply
   `//go:noescape` as a reviewed ABI assertion; only after that use `-m=2` and
   allocation tests to verify request/exit remain stack-resident. Apply a no-
   split leaf only after bounded stack/no-call disassembly proves it valid.
   Inspect the wrapper and leaf separately; neither may allocate per burst.
2. Preserve the exact Go/Darwin arm64 invariants: R18 is platform-reserved, R28
   holds `g`, SP remains 16-byte aligned, R29 follows the Go frame convention,
   R30/LR returns correctly, and FPCR is unchanged. Do not keep VM state in
   R16/R17 (linker scratch) or R27 (assembler scratch) across pseudo-instruction
   expansion. Do not assume a general C-style callee-saved register set. Use no
   local pointer frame, no Go or foreign calls, and no stack growth.
3. Prove an in-text dispatch table using `ADR` to a dense sequence of static
   `B handler` instructions and indirect `B (Rx)` after verifier bounds checks. Inspect
   the object code. If assembler/linker behavior is not reliable, compare a
   generated branch tree and generated Go jump-table dispatch; do not depend on
   undocumented code-address data.
4. Implement proof operations for all required families: scalar/control,
   direct guest call/return on a flat continuation array, fixed/open/vararg
   moves, guarded global hit, bounded array/hash/property hit, stable iteration,
   and one precise effect side exit. Use generated class-balanced states and
   CFGs, not a frozen benchmark sequence.
5. Enforce the 64-guest-operation and 256-primitive-work budgets plus per-op
   copy/probe/chain bounds. Return exact retired count, primitive count, phase,
   and next PC. On the pinned quiet M1, freeze these safety gates before speed
   tuning: uncontended burst p99 <=50 microseconds and observed max <=250
   microseconds; heartbeat/cancellation and a forced-GC rendezvous p99 <=2 ms
   and observed max <=10 ms. Any overrun caused by guest work kills the native
   handler; contaminated runner samples reacquire the whole latency artifact.
   Measure at least one million post-warm bursts spanning every operation and
   declared worst-case probe/copy/chain bound, plus 10,000 heartbeat,
   cancellation, and forced-GC rendezvous trials. Record quantum, work counts,
   CPU/load, GOGC, GC events, warm-up, raw histogram, nearest-rank p99, and max;
   no 20-run smoke test can authorize these bounds.
6. Run millions of randomized Go-versus-assembly state transitions. Compare
   every slot, continuation, table directory mutation, PC, count, and exit.
7. Use `go tool objdump` to prove the leaf has the expected dispatch, no calls,
   no accidental frame/spill shape, and no foreign symbol references.
   Use `go tool nm -size` and `otool -l` to record linked backend text/rodata,
   branch reach, and W^X segments. ADR 0007 records the chosen in-text layout,
   its arm64 +/-1 MiB conditional-branch reach assumptions, and the measured
   256 KiB maximum before promotion.
8. Build and test with `CGO_ENABLED=0`, `GOGC=1`, async preemption enabled,
   race, and checkptr. Race/checkptr do not instrument assembly, so combine
   them with serialized-owner assertions, canaries, red zones, and exact state
   differential.
9. In the Go wrapper, increment active-burst ownership before entry and call
   `runtime.KeepAlive` after return for request, exit, owner, image, and each
   backing array owner before decrementing it. Add escape/alloc tests proving
   the wrapper performs zero recurring allocations.
10. Delete proof files immediately if ABI, preemption, or state safety cannot be
   demonstrated. Retain only the ADR evidence of rejection.

Verification:

```sh
CGO_ENABLED=0 go test -tags=asmprobe -run '^TestAssemblyBurstProbe' -count=100 .
CGO_ENABLED=0 GOGC=1 GOMAXPROCS=1 go test -tags=asmprobe -run '^TestAssemblyBurstProbeGCAndHeartbeat$' -count=20 .
CGO_ENABLED=0 go test -race -tags=asmprobe -run '^TestAssemblyBurstProbe' .
CGO_ENABLED=0 go test -tags=asmprobe -gcflags=all=-d=checkptr=2 -run '^TestAssemblyBurstProbe' .
CGO_ENABLED=0 go test -tags=asmprobe -gcflags='all=-m=2' -run '^$' . 2>/tmp/ember-asm-escape.txt
CGO_ENABLED=0 go test -tags=asmprobe -c -o /tmp/ember-assembly-probe.test .
go tool objdump -s '.*runAssemblyBurst.*' /tmp/ember-assembly-probe.test
go tool nm -size /tmp/ember-assembly-probe.test | scripts/execution-code-size-gate --stdin
otool -l /tmp/ember-assembly-probe.test | scripts/execution-code-size-gate --segments
scripts/check-purego
```

Completion criteria:

- arm64 dispatch and ABI work in a fully cgo-disabled binary;
- all proof families are exact against Go over randomized states;
- no burst or operation can delay Go beyond the recorded bound;
- disassembly shows no Go/foreign calls and no unexpected pointer/frame path.

Risks and dependencies:

- Go assembly is not asynchronously preemptible; failure of bounded return is
  a hard kill, not a tuning issue;
- assembly is not race-instrumented; ownership and differential tests carry
  the proof burden;
- a dispatch proof without calls/table/property families does not authorize the
  next slice.

### Slice 1.3 - Run the general end-to-end architecture proof and kill gate

Assigned agent: Luna max, independently reviewed by Sol high.

Objective and reason: determine whether the complete general mechanism can
deliver the required multi-x gains before production state is rewritten.

Files and components:

- proof execution image and Go/assembly adapters from Slices 1.1-1.2;
- held-out semantic generator from Slice 0.3;
- baseline class requirements from Slice 0.4;
- proof benchmark and coverage report files kept outside production routing.

Ordered work:

1. Freeze the proof mechanism set before selecting held-out seeds: fixed-width
   lowering, slot layout, flat continuations, descriptor guards, bounded table
   probes, dispatch form, and side-exit classes.
   The proof uses the exact intended production word, slot, continuation,
   packed table, descriptor, request, exit, and effect-resume structures; an
   adapter that bypasses their loads, guards, ownership, or side exits cannot
   authorize Phase 2. This is the Phase 1 held-out epoch's lock point.
2. Compile the deterministic class-performance programs through the normal
   Ember compiler into proof execution images, then execute those same programs
   through current Ember, the proof Go engine, proof assembly, and pinned Luau.
   Also execute class-balanced generated states for exhaustive mechanics.
   Include scalar/control, recursive/direct/vararg calls,
   closures/upvalues, global/property/table/metatable hits, iteration, misses,
   allocation exits, host exits, errors, and coroutine boundaries.
3. Measure end-to-end time including the Go wrapper, quantum returns, spill/exit
   handling, and resume. Do not benchmark only the assembly inner loop.
4. Apply the clean same-program baseline-derived class gate. For each family,
   require `proof/current <= 1.75 / baseline_ratio`; algebraically this requires
   proof/Luau <=1.75 on that family. If the proof cannot execute an entire
   family end to end, the family fails rather than being estimated from an
   isolated operation benchmark.
   Freeze each baseline ratio from nine paired four-point slope fits under the
   same timer/statistical rules as speed2x, with its 95% interval, source and
   toolchain hashes. Use the conservative interval endpoint that demands the
   larger proof gain; a noisy or invalid family ratio fails rather than
   weakening the gate.
5. Apply the native coverage and side-exit floors from this plan. No family may
   be absent, merged into "other", or excluded because it is slow.
6. Require warmed allocation non-regression and zero recurring proof allocation
   per instruction/call/hit/iteration.
7. Run a screening all-37 capture through the production Ember path plus any
   safely integrated proof path. This is diagnostic; the class proof remains
   the authorization because a partial adapter may not cover every row yet.
8. Have Sol review disassembly, ABI ownership, semantics, coverage denominators,
   statistics, and whether any mechanism was derived from visible workload
   structure.

Retention gate:

- Proceed only if every semantic family is exact, meets its baseline-derived
  required gain, satisfies class coverage, and has bounded scheduler/GC impact.
- A mere 20% to 25% improvement does not pass unless the clean baseline proves
  that family needs no more. The hardest families must demonstrate their
  required multi-x reduction.
- If the proof fails, delete the production-intended image/assembly additions,
  retain measurement tests/evidence only where independently useful, mark ADR
  0007 rejected under current constraints, and stop the campaign.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestAssemblyBurstProof|TestGeneratedExecution)' -count=1 ./...
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkAssemblyArchitectureProof/' \
  -benchmem -benchtime=500ms -count=9 ./...
scripts/check-purego
scripts/check-fast
```

Completion criteria:

- the decision is based on general families and hidden programs, not named
  workload improvements;
- measured gains are large enough to bridge the actual clean baseline gaps;
- Sol records an explicit proceed/kill decision before Phase 2.

Risks and dependencies:

- a synthetic state kernel can overstate end-to-end gains; wrapper, side exits,
  stack/table representations, and resume cost must be included;
- this is the principal honesty gate. Do not weaken it after an expensive
  prototype.

## Phase 2: Replace the canonical runtime state

Phase 2 is one architectural retention group. It builds a separate private
candidate owner graph and candidate-only types; the existing VM remains the
production route and no production entry constructs, mirrors, or warms the new
state. Phase 3 completes semantics, then Slice 3.4 performs the sole production
cut to the generated Go engine. Phase 4 optimizes that live canonical state;
Phase 5 deletes the now test-only old oracle and migration residue.

### Slice 2.1 - Build the runtime-owned execution image

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: convert immutable compiler output and messy owner state
into execution-ready records once, eliminating repeated decode, identity
discovery, and map/string work from the hot loop.

Files and symbols:

- `program.go`: `Program`, Proto ownership and validation;
- `bytecode.go`: opcodes, metadata, wordcode encoding;
- `vm_dispatch_spec.go`, `cmd/ember-vmgen/main.go`;
- `module_runtime.go`, `runtime_heap.go`;
- new private `execution_image.go`, `execution_lower.go`, and focused tests;
- no public package split unless the resulting interface is independently
  useful and smaller than the root implementation.

Ordered work:

1. Split preparation into a shareable immutable lowered-code object owned by
   `Program` and an owner-bound `executionImage` containing runtime constants,
   descriptor bindings, exact guest-PC maps, and mutable guard/cache state.
   Build the lowered object during explicit Compile/finalization (or an equally
   explicit `Prepare` API), not through a hidden global identity map or normal
   Run constructor. It contains no owner handle, root, global, or writable
   cache.
2. Keep Program and Proto immutable and shareable after Compile/Prepare. Their
   immutable lowered representation is allowed; never write runtime handles,
   globals, mutable cache state, or owner/backend bases into them.
3. Lower every supported opcode through the verifier from Slice 1.1. Reject an
   entire image on malformed control flow, operands, descriptor capacity, or
   an unclassified effect. Do not fall back opcode-by-opcode after validation
   begins.
4. Resolve constant and Proto identities to dense indices. Materialize string
   hashes/intern handles, native/call identities, global keys, field keys, and
   descriptor slots once per owner.
5. Separate immutable image data from mutable owner data so multiple Runtime
   owners can use one Program without races or handle aliasing.
6. Add an explicit `image *executionImage` (or equivalently narrow owner
   registry) to persistent Runtime ownership. Prepare it lazily once at a
   named preflight, preserve pointer/generation identity across repeated hooks,
   and release it exactly once on successful inactive close. Give stateless
   preparation and release equally explicit lifetimes. Constructors remain
   boring: image work occurs at the explicit execution preparation seam.
7. Add cold benchmarks for image construction, repeated Runtime hooks, and
   image release. Enforce the cold allocation/byte caps and prove warmed calls
   do not rebuild image records.
8. Add full opcode inventory tests: every opcode has lowering, effects,
   bounded-work classification, guest-cost mapping, and either a backend class
   or a declared Go effect exit.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestExecutionImage|TestExecutionLower|TestOpcode.*Classified)' ./...
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkExecutionImage' -benchmem -count=5 ./...
go generate ./...
scripts/check-lane root
```

Completion criteria:

- every valid Proto lowers deterministically and every invalid image fails
  before execution;
- Program/Proto have no owner-local mutation;
- repeated execution uses one prepared image and adds no warm allocation;
- all opcodes and effect classes are fail-closed in generated inventory tests.

Risks and dependencies:

- lowering can move cost rather than remove it if rebuilt for stateless runs;
  measure both ephemeral and persistent Runtime lifetimes;
- do not add one side table per optimization. Fold state into the image's few
  dense arrays;
- this slice depends on a passing Phase 1 gate and the frozen ABI spec.

### Slice 2.2 - Cut the complete owner graph to 64-bit slots

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: remove 16-byte pointer-bearing Value traffic and make all
backend-visible state safe for assembly without hidden Go pointers.

Files and symbols:

- `slot.go`: tags, handles, boxed-number escape;
- `value.go`: `Value`, `Table`, closures, cells, public adapters;
- `runtime_heap.go` and collector/root tests;
- `base_env.go`, `base_globals.go`, `module_runtime.go`;
- `callback.go`, `runtime_call.go`, `base_coroutine.go`;
- new owner-slot and public-edge tests.

Ordered work:

1. Freeze the exact slot contract from ADR 0004 or deliberately supersede it
   in ADR 0007. Preserve every float64 bit pattern; colliding NaNs use the rare
   boxed-number handle rather than losing payload bits.
2. Split owner storage into pointer-free hot arrays and GC-visible root/payload
   slabs. Slots contain `{kind,index,generation}` handles, never raw pointers or
   a `uintptr` conversion of a Go pointer.
3. Convert runtime constants, globals, module exports, registers, arguments,
   varargs, results, closures, cells/upvalues, coroutines, native-builtin
   staging, callback staging, and all image/cache payloads to slots.
4. Define typed allocation, stale-handle validation, generation exhaustion,
   free-list reuse, object identity, and deterministic identity hash. Fail
   closed in tests/debug on stale or cross-owner handles.
5. Make the runtime heap scanner enumerate every slot root and object edge:
   active stacks, continuations, globals, image constants, module cache,
   suspended coroutines, open/closed upvalues, tables, callback arguments, and
   explicit host pins.
   Generate a root-field manifest test that fails when a new owner/image field
   is added without a scanner. Collection never imports public objects or
   discovers new roots during sweep; boundary import completes before the
   stop-the-world scan.
   Scan only logical live stack windows and continuation depth. Clear returned,
   spilled, truncated, and pooled slot ranges before reuse so reserved capacity
   cannot retain stale handles; verify reclamation under `GOGC=1`.
6. Isolate opaque Go userdata/function payloads in a pinned pointer-bearing
   slab. Assembly can carry their handles but cannot inspect or mutate them.
7. Make a deliberate breaking public-lifetime choice before implementation:
   stateless `Run` returns an owner-bearing `Result`/lease whose object graph
   remains valid until explicit `Close`, rather than releasing an ephemeral
   owner beneath returned tables/closures or deep-copying arbitrary cyclic
   graphs. Scalar convenience accessors may detach scalars. Document migration
   from `[]Value`, deterministic release, leak/finalizer policy, post-close
   failure, and callback/table lifetimes in ADR/public-surface docs.
8. Prohibit implicit cross-Runtime table or closure sharing. Importing an
   owner-bound object into another Runtime fails with a typed cross-owner
   error; explicit detach/copy is a cold host operation with documented cycle
   and identity semantics and is never used by the hot backend. This removes
   unsynchronized per-owner mirrors and makes host mutation/invalidation
   ownership unambiguous.
9. Ensure imports happen once at an entry edge and exports once at a return
   edge. Add a structural test rejecting Value<->slot conversions inside burst
   loops, script calls, table helpers, or stable descriptor hits.
   `RunWithGlobals` owns a dense slot projection plus a mutation journal and
   synchronizes caller-visible writes at every host/effect boundary and on all
   returns, including error, cancellation, and callback re-entry. Tests prove
   partial writes, deletion, alias identity, and callback-observed mutation;
   successful-only export is not an allowed semantic change unless separately
   documented as a breaking API decision.
10. Replace the current pointer-rich `slotSlab[*Table]`,
    `slotSlab[*closure]`, and `slotSlab[*cell]` hot dereferences with scalar
    projections. Pointer-bearing root/payload slabs are reachable only from Go
    effects. Intern strings by text/hash in the owner, preserving exact
    equality across distinct public constructors, empty/long strings, concat,
    and owner import.
11. Stress forced Go GC, owner close, stale handles, pin release, cyclic tables,
   callback captures, coroutine escape, and concurrent distinct Runtime owners.
   Include owner/image-cookie mismatch and same index/generation from two
   owners.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestSlot|TestRuntimeHeap|TestRuntimeOwner|TestCallbackCollector|Test.*Coroutine)' ./...
CGO_ENABLED=0 GOGC=1 go test -count=20 -run '^(TestRuntimeHeap|TestRuntimeOwner|TestCallbackCollector)' ./...
CGO_ENABLED=0 go test -race -run '^(TestRuntimeHeap|TestRuntimeOwner|Test.*Coroutine)' ./...
CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 -run '^(TestSlot|TestRuntimeHeap|TestRuntimeOwner)' ./...
```

Completion criteria:

- all canonical internal values are slots and all Go pointers stay in visible
  owner slabs;
- no conversion remains in a steady-state script call/instruction/table path;
- root scanning, owner-bearing result lifetime, pins, explicit detach, close,
  and stale-handle behavior pass under
  forced GC/race/checkptr;
- warmed allocation ceilings remain unchanged.

Risks and dependencies:

- this is a breaking ownership change with the highest correctness risk;
- a pointer-rich handle slab alone does not produce the required locality. Hot
  metadata and cells must be packed scalar arrays;
- arbitrary host closures cannot be traced; pin them explicitly rather than
  guessing captured Go state;
- do not enable collection until every root and edge has a scanner.

### Slice 2.3 - Replace frames with flat value and continuation stacks

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: remove `vmFrame` reconstruction and make direct calls,
returns, recursion, tail calls, varargs, and coroutines index-based operations
that both backends can execute.

Files and symbols:

- `vm.go`: `vmThread`, `vmFrame`, direct loop and call paths;
- `runtime_call.go` and `runtime_call_test.go`;
- `base_coroutine.go` and runtime-owner coroutine tests;
- execution-image call/arity/result descriptors;
- new continuation layout and call differential tests.

Ordered work:

1. Define a compact pointer-free continuation record containing caller Proto
   and micro-PC, guest return PC, caller base/top, result destination/mode,
   expected arity, vararg base/count, protected-call state ID, and exact limit
   metadata. Generate layout constants for Go and assembly.
2. Allocate one owner-managed value stack and one continuation stack per active
   thread. Addresses inside guest state are indices, not pointers into slice
   backing arrays.
3. Implement direct fixed-arity call, general call, return-one, open return,
   tail-call frame reuse, recursion, multiple results, and vararg windows using
   stack indices and descriptors. Do not create Go slices per call.
4. Resolve already-known closure/Proto/arity at image preparation or guarded
   descriptor hit. Dynamic/host call classification becomes a precise effect
   exit, not repeated generic validation on every direct script edge.
5. Preserve exact call-depth limits, instruction accounting, protected-call
   boundaries, error frame order, stack traces, and tail-call observability.
6. Give each coroutine stable slot/continuation blocks plus a scalar suspended
   state record. Yield/resume switches the active owner in Go and then resumes
   the same image PC; it does not copy `[]Value` or reconstruct frame objects.
7. Grow stacks only at a Go side exit. Refresh typed base pointers before
   re-entering a backend. Enforce maximum sizes and leave no stale native base
   after growth.
8. Differentially test every call/result/vararg/coroutine shape against the old
   VM while it remains the test oracle. Add recursive and mutually recursive
   generated graphs, not a single Fibonacci special case.
9. Profile and assert that `enterRecordOnlyFixedCall`,
   `resumeRecordOnlyFixedCallOne`, frame clear/reset, and per-call allocation
   are absent from the new path.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestRuntimeCall|TestExecution.*Call|Test.*Vararg|Test.*TailCall|Test.*Coroutine)' ./...
CGO_ENABLED=0 go test -run '^$' -bench 'Call|Vararg|Coroutine' -benchmem -count=9 ./...
CGO_ENABLED=0 GOGC=1 go test -count=20 -run 'Call|Vararg|Coroutine' ./...
```

Completion criteria:

- direct script call/return/tail/vararg mechanics require no Go allocation and
  no frame-object construction;
- coroutine suspension/resume preserves exact semantics without state copies;
- generated recursive/call-shape differential is exact;
- new call-class benchmarks meet the Phase 1 baseline-derived gain.

Risks and dependencies:

- error/protected-call and open-result semantics are easy to flatten
  incorrectly; retain precise guest-PC and continuation tests;
- stack growth cannot occur inside assembly and must invalidate all cached base
  pointers before resume;
- this slice owns call state; no concurrent worker edits call/frame symbols.

### Slice 2.4 - Install pointer-free table, global, and property storage

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: allow the backend to execute stable table/property/global
hits directly. Side-exiting these classes would leave the hardest dynamic rows
several times over target.

Files and symbols:

- `value.go`: `Table`, `tableStorage`;
- `table_ops.go`, `table_shape.go`, `property_ic.go`;
- `base_env.go`: `globalEnv`;
- table shape/hash/iteration/metamethod tests;
- execution-image table/global/property descriptors;
- new packed-storage and descriptor differential tests.

Ordered work:

1. Define an owner table directory indexed by table handle. Hot header fields
   are scalar: array/hash/order offsets and lengths, capacity/mask, shape and
   mutation versions, metatable handle/version, and deterministic identity.
2. Store table keys, values, hash controls, and ordered-iteration links in
   owner-managed pointer-free arrays. Prefer offsets into shared arenas over a
   nested forest of Go slice headers. Go owns arena backing slices and keeps
   them rooted.
3. Implement dense numeric-array access and ordered open-addressed hash lookup
   with a strict maximum native probe count. Normalize numeric zero and NaN
   behavior exactly. Intern string keys so equality can use owner handles plus
   cached hash.
4. Side-exit growth, rehash, new-key insertion when capacity is exhausted,
   delete/compaction, long string work, and structural changes. Go performs the
   operation, increments the right versions, refreshes arena bases, and resumes.
5. Replace global map/string discovery on stable paths with dense global
   descriptors. Host/global mutation invalidates by version; it never silently
   leaves an assembly descriptor pointing at stale state.
6. Make property descriptors represent the complete guarded state: receiver
   shape/version, normalized key, finite table/prototype chain, metatable
   versions, terminal value/slot or scripted target, and miss/effect reason.
7. Execute stable own-field, prototype-chain, and scripted metamethod targets
   through the same generic descriptor and direct call mechanics. Unknown or
   host metamethods exit to Go.
8. Preserve deterministic iteration and mutation rules through an ordered
   scalar journal/cursor. Stable iteration stays backend-visible; mutation that
   invalidates the cursor exits and reconstructs through Go.
9. Add differential model tests for collisions, holes, numeric zeros, NaNs,
   mixed keys, shape churn, metatable replacement, deep finite chains,
   descriptor invalidation, deletion, iteration under mutation, cyclic graphs,
   and owner isolation.
10. Measure cold arena reservations separately. Warm array/hash/property/global
    hits and iteration steps allocate zero and meet the class gain gate.

Verification:

```sh
CGO_ENABLED=0 go test -run 'Table|Property|Global|Metatable|Iteration|Hash|Shape' ./...
CGO_ENABLED=0 go test -race -run 'Table|Property|Global|Metatable|Iteration|Hash|Shape' ./...
CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 -run 'Table|Property|Global|Metatable|Iteration|Hash|Shape' ./...
CGO_ENABLED=0 go test -run '^$' -bench 'Table|Property|Global|Iteration' -benchmem -count=9 ./...
```

Completion criteria:

- stable array/hash/global/property/metatable/iteration paths use only bounded
  scalar/slot operations;
- every structural mutation increments the correct guard and stale descriptors
  fail to a semantic Go path;
- deterministic table semantics and owner isolation are exact;
- warm operations allocate zero and meet Phase 1 class-relative speed gates.

Risks and dependencies:

- table layout is likely the largest implementation change after slots;
- a maximum probe exit can raise transition frequency. Size/growth policy must
  keep measured stable-hit coverage above the declared floor without relying
  on workload keys;
- arena growth may move backing arrays; assembly requests are rebuilt only at
  Go safepoints after growth;
- do not revive rejected table-allocation work solely for allocation savings.
  This storage change is justified by native accessibility and measured CPU.

### Slice 2.5 - Seal descriptor invalidation and owner isolation

Assigned agent: Luna high, reviewed by Sol high.

Objective and reason: make all fast state explicit and auditable before either
backend depends on it for correctness.

Files and symbols:

- execution-image descriptor records;
- `property_ic.go`, table/global version owners;
- module/runtime close and concurrent-owner tests;
- diagnostic class counters.

Ordered work:

1. Enumerate every descriptor kind and the mutations that invalidate it:
   globals, table structure/value, shape, metatable, property chain, closure,
   call target/arity, stack growth, coroutine owner, and module replacement.
2. Use monotonic owner-local versions with explicit exhaustion behavior. Never
   reuse a version in a way that can make stale state valid.
3. Centralize miss resolution in small Go helpers that return a complete new
   descriptor or a semantic result. Backend code must not partially rebuild
   cache state.
4. Classify descriptors as immutable image data or mutable owner state. Add a
   race test where two Runtime owners execute one Program with independent
   globals/tables/caches.
5. Preserve the current fail-busy close contract. `Runtime.Close` blocks new
   entries but, if a run/callback/burst is active, returns the existing active
   error without freeing state; a later Close succeeds. It never waits inside
   a callback and therefore cannot deadlock when the callback calls Close.
   After a successful inactive close, clear descriptors, stacks, arenas, and
   pins in an obvious ownership order. No assembly function retains an address
   after return.
6. Add diagnostic counters for hit/miss/invalidation/exit by semantic class;
   compile them out of production measurement variants.

Verification:

```sh
CGO_ENABLED=0 go test -run 'Descriptor|Invalidation|OwnerIsolation|RuntimeClose' ./...
CGO_ENABLED=0 go test -race -run 'Descriptor|Invalidation|OwnerIsolation|RuntimeClose' ./...
CGO_ENABLED=0 GOGC=1 go test -count=20 -run 'OwnerIsolation|RuntimeClose' ./...
```

Completion criteria:

- every mutation has one documented invalidation owner;
- no descriptor or native base crosses Runtime ownership or close;
- same-Program/different-owner execution is race-free and semantically isolated;
- callback-calls-Close and run/lease/close interleavings preserve fail-busy
  behavior and never tear down active state;
- counters provide complete class denominators without affecting production
  timing.

Risks and dependencies:

- missing invalidation can produce fast wrong answers. Treat any differential
  mismatch as an architecture bug, not a cache corner case;
- version overflow behavior must fail closed and be testable.

## Phase 3: Build the complete generated Go oracle and portable backend

### Slice 3.1 - Generate lowering, dispatch metadata, and the Go burst loop

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: establish one complete, readable semantic engine on the
new state before production assembly duplicates bounded mechanics.

Files and symbols:

- `vm_dispatch_spec.go`, `cmd/ember-vmgen/main.go`;
- `bytecode.go` metadata and new execution-image spec;
- new `execution_burst_go.go` plus checked-in generated output;
- `execution_control.go` for policy integration;
- generation-current and structural tests.

Ordered work:

1. Extend the existing generator rather than creating unrelated inventories.
   Generate micro-op names, operand decoding, effect/bounded-work tables,
   Go dispatch cases, arm64 layout constants, and test vector inventory from
   the same reviewed source metadata.
2. Implement one monolithic Go switch over verified micro-ops. Do not use a
   function-value table, closure per operation, interface dispatch, or a second
   `stepInstruction` call inside the loop.
3. Keep hot state in locals for the duration of a burst and spill only at exit,
   quantum, effect, error, or return. The Go loop uses the same 64-guest-op
   contract and exact `burstExit` as assembly.
4. Implement all bounded semantic classes against slots, flat stacks, and
   packed tables. Effectful work calls the shared Go effect executor after the
   loop returns, not from a duplicate hidden path.
5. Generate unrestricted, controlled, and diagnostic policy variants from one
   template/spec. Production unrestricted output must contain no dead
   instrumentation branches or counters.
6. Make stale generation, missing opcode, mismatched layout, or unclassified
   effect fail `go run ./cmd/ember-vmgen -check` and focused tests. Any sibling
   generator gets its own documented `-check`; Go's `generate` command itself
   has no `-check` flag.
7. Inspect compiler output for switch dispatch, bounds-check elimination, hot
   spills, and unexpected escapes. Change data flow before adding unsafe
   directives.

Verification:

```sh
go generate ./...
go run ./cmd/ember-vmgen -check
CGO_ENABLED=0 go test -run '^(TestGenerated|TestGoBurst|TestExecutionImage)' ./...
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkGoBurst/' -benchmem -count=9 ./...
CGO_ENABLED=0 go test -gcflags='all=-m=2' -run '^$' ./... 2>/tmp/ember-escape.txt
```

Completion criteria:

- Go implements every semantic/bounded class over the canonical new state;
- all variants share one semantic inventory and generated files are current;
- production loop has no instrumentation policy work or recurring allocation;
- Go and proof assembly use an identical request/exit contract.

Risks and dependencies:

- generated source can become another 4,000-line maintenance island. Keep the
  human-owned spec small and generate mechanical cases only;
- do not retain old `stepInstruction` calls as a convenient fallback inside
  the new loop.

### Slice 3.2 - Implement one Go effect executor and exact control policy

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: keep allocation, host effects, errors, cancellation, and
coroutine transitions at one Go seam shared by both backends.

Files and symbols:

- `execution_control.go`, context/limit/error helpers;
- `callback.go`, `base_coroutine.go`, `module_runtime.go`;
- native builtin and host call adapters;
- new `execution_effect.go` and exit differential tests.

Ordered work:

1. Define a closed `burstExitReason` inventory covering completion, quantum,
   budget, descriptor miss, stack/arena growth, allocation, table structural
   mutation, string work, host/native call, unknown/metamethod call, protected
   error, debug hook, yield/resume, cancellation, and collection poll.
2. Implement one Go dispatcher from a validated exit record to a small pure or
   effectful helper. It either completes execution, returns an error/outcome,
   or refreshes request bases/descriptors and resumes.
3. Charge exact guest instruction cost before each operation. A MaxInstructions
   boundary stops before the over-budget operation and reports the same guest
   PC/error as the old VM and Luau-facing contract.
4. Check context cancellation and GC/owner policy at every quantum and at
   effect boundaries. Record and test the maximum cancellation/heartbeat
   latency; do not poll hidden wall clocks in core semantics.
5. Convert callback/native arguments/results at the host edge once. Internal
   native builtins that can safely accept slots get an explicit slot ABI; opaque
   Go callbacks use public adapters and remain effect exits.
6. Perform allocation/growth/rehash in Go, update roots and versions, then
   rebuild the typed request before resume. Never let an old base pointer
   survive an allocating helper.
7. Preserve error causes, source frames, metamethod order, pcall behavior,
   coroutine yield/resume, and module initialization exactly in differential
   tests.
8. Implement the following closed limit-accounting matrix. Each charge occurs
   before the semantic mutation, a side exit/resume may neither omit nor repeat
   it, and a rejected charge reports the prospective `Used` value. Every
   `LimitError{Kind,Limit,Used}` and guest PC is compared against the old engine
   at every cut point.

| Execution limit | Exact accounting obligation |
| --- | --- |
| `MaxInstructions` | Charge once before each executable guest opcode; AUX words and lowered micro-ops do not add charges. No release; quantum/resume preserves remaining count. |
| `MaxCallDepth` | Reserve before continuation/frame entry, report current depth+requested count on failure, release exactly once on return, tail reuse, or unwind. |
| `MaxModuleInitializations` | Charge only before a real uncached module initialization; cached/cyclic observation does not recharge. Once an attempt starts its charge is cumulative for that invocation even if module execution fails; there is no release. |
| `MaxCoroutines` | Reserve before publishing an owner coroutine; release exactly once on close/completion or failed construction, while suspension retains capacity across runs. |
| `MaxGeneratedStringBytes` | Charge newly generated/interned bytes before publication with saturating prospective `Used`; constants, host strings, and a cached repeated generated string follow current non-recharge rules. No rollback after successful publication. |
| `MaxTableEntriesPerTable` | Check prospective per-table count before a new non-nil key; overwrite is neutral, delete/clear releases entries, and all direct/library/metamethod mutation paths share the check. |
| `MaxRuntimeObjects` | Reserve exact object count before publishing tables, closures, shared cells, coroutines/userdata, and other owned heap records; canonical reuse does not recharge. Successful creation is cumulative for the invocation even after reclamation; only a failed multi-object construction rolls back its uncommitted reservation exactly once. |

9. Treat cancellation and all seven execution-limit failures as non-catchable by
   guest `pcall`/`xpcall`, matching the current controller. Test nested
   protected calls, metamethods, host callbacks, module initialization, and
   coroutine resume. Use `runtime_budget_b8_test.go`,
   `runtime_budget_b9_test.go`, `runtime_error_c3_test.go`, and
   `execution_differential_test.go` as a mandatory migration inventory.

Verification:

```sh
CGO_ENABLED=0 go test -run 'Limit|Cancellation|Error|Protected|Callback|Coroutine|Module|EffectExit|Budget' ./...
CGO_ENABLED=0 GOGC=1 go test -count=20 -run 'Limit|Cancellation|Callback|Coroutine|EffectExit' ./...
CGO_ENABLED=0 go test -race -run 'Cancellation|Callback|Coroutine|RuntimeClose' ./...
```

Completion criteria:

- all effects flow through one explicit Go seam;
- exact limits/errors/cancellation/coroutines match the reference;
- resume refreshes every movable base and invalidated descriptor;
- direct script calls and stable table/property hits do not appear as effects.

Risks and dependencies:

- an overly broad effect category will make assembly transition-bound; enforce
  class coverage continuously;
- callbacks can retain Values and require explicit pin/lifetime rules.

### Slice 3.3 - Differentially validate and screen the pure-Go architecture

Assigned agent: Luna max, independently reviewed by Sol high.

Objective and reason: prove the new representation/semantics before adding
production assembly, and stop early if Go alone already meets the target.

Files and components:

- old VM as temporary test oracle only;
- new Go image engine;
- existing compatibility and generated held-out corpora;
- speed2x and allocation artifacts;
- diagnostic coverage reports.

Ordered work:

1. Run all public source-to-result tests through old and new engines and compare
   results/errors/state. Add explicit backend selection only in test plumbing;
   no public permanent engine flag.
2. Run all 5,000 now-revealed Phase 1 regression programs, the development
   corpus, and the state-level corpus under normal, race, checkptr, GOGC=1, and
   repeated owner close/reopen. Do not reveal the reserved Phase 4 epoch here.
3. Run the full existing fuzz seed corpus. Add any discovered mismatch as a
   minimized semantic regression test, not as a backend special case.
4. Capture the all-37 sources through an internal test-only Go-engine runner
   using the frozen schedules. Apply allocations and class coverage and record
   profiles even if the speed gate fails. This screening artifact is not the
   official parity capture because production entrypoints have not moved yet.
5. Treat a passing test-only screen as evidence to expect a Go-only finish, not
   as acceptance. Slice 3.4 must still cut the real production route and take
   two official captures before Phase 4 can be skipped.
6. For failing screens, verify the Phase 1 assembly proof still predicts enough remaining
   gain for every failing semantic family. If not, stop rather than entering a
   doomed native phase.

Verification:

```sh
CGO_ENABLED=0 go test -count=1 ./...
CGO_ENABLED=0 go test -race -count=1 ./...
CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 -count=1 ./...
CGO_ENABLED=0 GOMAXPROCS=1 go test -run '^TestGoEngineAll37Screen$' ./...
CGO_ENABLED=0 GOMAXPROCS=1 scripts/performance-audit \
  --output /tmp/ember-speed2x-go-screen --profiles
```

Completion criteria:

- semantic differential is 100%; no known mismatch is deferred to assembly;
- Go-only speed/allocation/coverage is measured on all rows and families;
- Sol records either "Go meets target", "assembly has sufficient measured
  residual leverage", or "stop".

Risks and dependencies:

- temporary dual-engine tests must not become a public permanent selection
  surface;
- do not use `scripts/check-full`; the explicit race/checkptr commands above
  cover this slice without violating repository instructions.

### Slice 3.4 - Cut production once to the generated Go engine

Assigned agent: Luna max, independently reviewed by Sol high.

Objective and reason: put the correct new state on the real public path before
native optimization, so Phase 4 measures and improves production rather than a
test adapter.

Files and symbols:

- public `Run`/`RunWithGlobals`, Runtime hook, module, callback, and coroutine
  entrypoints from the entry/cutover matrix;
- `execution_control.go`, owner/image lifetime, public result lease;
- old VM call sites retained only for a same-package `_test.go` oracle wrapper;
- public-surface and compatibility docs for approved breaking changes.

Ordered work:

1. After Slice 3.3 semantic approval, route every public entry in the cutover
   matrix to the generated Go image engine in one retention group. There is no
   env var, global switch, public option, or shadow execution.
2. Keep the old VM callable only from same-package differential tests through a
   `_test.go` helper while its code remains compiled. No production entry or
   external benchmark can select it. Freeze a symbol/call-graph test proving
   this separation.
3. Prove owner-bearing stateless results, globals write-back on every exit,
   persistent image identity, callback/module/coroutine re-entry, fail-busy
   close, and same-Program/different-owner isolation through public tests.
4. Run normal/race/checkptr/forced-GC suites. Then take official speed2x and
   all-37 allocation captures; these now exercise the production Go engine
   naturally, without test selection plumbing.
5. If the production Go engine passes every speed row twice, declare Go the
   retained backend and skip Phase 4. Otherwise proceed only when the Phase 1
   proof still covers every failing family with adequate measured leverage.

Verification:

```sh
CGO_ENABLED=0 go test -count=1 ./...
CGO_ENABLED=0 go test -race -count=1 ./...
CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 -count=1 ./...
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x \
  --schedule testdata/runtime-parity/speed2x-schedule-v1.tsv \
  --replicate go-cutover-a
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x \
  --schedule testdata/runtime-parity/speed2x-schedule-v1.tsv \
  --replicate go-cutover-b
CGO_ENABLED=0 GOMAXPROCS=1 scripts/execution-allocation-gate \
  capture --manifest all37 --replicate go-cutover-a
CGO_ENABLED=0 GOMAXPROCS=1 scripts/execution-allocation-gate \
  compare --replicate go-cutover-a \
  --manifest testdata/runtime-performance/all37-allocation-v1.tsv
```

Completion criteria:

- every public production entry uses the generated Go engine and canonical new
  state;
- the old engine is unreachable outside same-package tests;
- official performance artifacts measure the actual production route;
- Sol records proceed-to-assembly, finish-Go-only, or stop.

Risks and dependencies:

- this is the semantic/API cutover, so it must be a cohesive reviewed commit;
- do not postpone public lifetime or globals-writeback correctness to Phase 5.

## Phase 4: Implement the bounded Darwin/arm64 production backend

All Phase 4 files use normal Go build constraints so unsupported targets build
and run the generated Go backend. Performance support is initially declared
for Darwin/arm64 only. Add amd64 later only through a separate measured plan;
do not duplicate a second architecture spec merely to claim portability.

### Slice 4.1 - Promote the proven assembly ABI and bounded dispatcher

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: replace the isolated proof with a production adapter over
the canonical execution image without changing semantics or lifetime rules.

Files and components:

- new `execution_burst_arm64.go` and generated `.s`/include files;
- generic build fallback in `execution_burst_generic.go`;
- `execution_burst_abi.go`, generator, layout tests;
- backend selection inside private `executeImage` only.

Ordered work:

1. Move only the passing proof mechanism into production: stable Go prototype,
   ABI0 wrapper, no-call leaf, in-text dispatch, 64-op maximum quantum, exact
   request/exit spill, and generated layouts.
2. Select assembly only on the explicitly supported Darwin/arm64 target and
   only for verified images with supported semantic version/layout. Generic
   builds always select Go.
3. Keep functional support identical. A backend mismatch or unsupported image
   version falls back before execution begins, never mid-state after mutation.
4. Add a diagnostic force-Go mode available only to tests and benchmark
   tooling. Production API has no user-visible engine choice.
5. Add object-code tests for no calls/foreign symbols, maximum function/text
   shape, dispatch table size, and required returns. Run the linked-binary
   `scripts/execution-code-size-gate`; backend hot text plus dispatch rodata
   must remain <=256 KiB and within the ADR's proven branch reach.
6. Prove every backend invocation is covered by an active owner/burst count so
   Runtime close cannot clear or move arrays until return.

Verification:

```sh
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -run 'Burst|Backend|ExecutionImage' ./...
CGO_ENABLED=0 go test -c -o /tmp/ember-arm64.test .
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/ember-linux-amd64.test .
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o /tmp/ember-linux-arm64.test .
scripts/check-purego
CGO_ENABLED=0 scripts/execution-code-size-gate --binary /tmp/ember-arm64.test
```

Completion criteria:

- target builds use the proven static assembly adapter and generic builds use
  the same Go semantics;
- no runtime code page, foreign symbol, or private Go runtime ABI is involved;
- owner lifetime and request layout are exact and versioned.

Risks and dependencies:

- build tags can hide stale generic code; compile both target and fallback
  architectures on every change;
- never use ABIInternal directly or `go:linkname` as a shortcut.

### Slice 4.2 - Implement scalar/control and exact retirement in assembly

Assigned agent: Luna max.

Objective and reason: execute the highest-frequency bounded value/control
mechanics with no Go dispatch or spill between guest instructions.

Files and components:

- generated arm64 semantic handlers/template;
- slot/layout constants;
- scalar/control differential vectors and disassembly checks.

Ordered work:

1. Implement slot load/move/constant, nil/bool truth tests, number tag/boxed
   escape checks, hardware-bounded arithmetic, unary operations, comparisons, branches,
   numeric-for, and loop backedges.
2. Preserve IEEE behavior, division/mod/idiv, negative zero, NaN payload
   boxing, overflow/error exits, and metamethod ambiguity exactly. `pow`,
   transcendentals, string-number coercion, and any division edge requiring an
   unbounded/runtime helper make a deliberate pre-mutation Go effect exit unless
   a separately reviewed bounded exact handler is proven. They remain in
   end-to-end timing and their declared effect denominator.
3. Bounds-check all image/register/stack indices through verifier invariants and
   explicit dynamic checks where sizes can change. No fault is an accepted
   guest error path.
4. Check remaining guest budget before each operation cost. Decrement quantum
   and return before the maximum. Spill one exact PC/top/count state on every
   exit.
5. Use descriptor/type guards that apply to every matching runtime state. A
   guard miss exits; it never patches code or matches a source sequence.
6. Differentially run every scalar/control vector and randomized CFG under Go
   and assembly, including every possible quantum and instruction-limit cut.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestAssemblyScalar|TestAssemblyControl|TestAssemblyLimitCut)' -count=50 ./...
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkBurst(Scalar|Control)/' -benchmem -count=9 ./...
```

Completion criteria:

- exact state/PC/count for all scalar/control vectors and limit cuts;
- no recurring allocation or Go exit for >=99% of class operations;
- measured class speed meets the clean baseline-derived requirement.

Risks and dependencies:

- arithmetic edge behavior can differ subtly from Go expressions; use shared
  vectors and explicit slow exits rather than approximating semantics;
- excessive spills erase the point of assembly and fail the speed gate.

### Slice 4.3 - Implement calls, continuations, varargs, and upvalues

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: close the largest remaining call-heavy gap through
general direct script mechanics, not a special recursive opcode path.

Files and components:

- arm64 call/return/continuation handlers;
- execution-image call/arity/result descriptors;
- flat stack/continuation and upvalue storage;
- generated call-graph differential corpus.

Ordered work:

1. Implement guarded direct closure/Proto entry, fixed/general call, return-one,
   open return, tail-call reuse, recursion, multiple-result moves, and
   continuation push/pop entirely in pointer-free state.
2. Implement vararg base/count, pack/unpack, select-style movement, and open
   argument/result windows without allocating Go slices.
3. Implement existing closure invocation and open/closed upvalue reads/writes.
   Closure/cell allocation remains a Go exit, but invoking an already-created
   script closure does not.
4. Exit for host/native/unknown/protected calls through a precise call record.
   Stable scripted metamethod targets use the same direct call mechanism.
5. Enforce stack/continuation capacity, call-depth limit, protected-state ID,
   tail-call frame semantics, and exact return guest PC. Capacity growth exits
   before mutation and resumes with refreshed bases.
6. Generate direct, indirect, mutually recursive, tail-recursive,
   fixed/open/vararg, multi-result, closure, and upvalue graphs. Compare every
   intermediate state and exit to Go.
7. Require >=95% native direct script edges and the Phase 1 class speed gain.
   If Go transitions remain on direct calls, kill the backend.

Verification:

```sh
CGO_ENABLED=0 go test -run 'Assembly.*(Call|Return|Vararg|Upvalue|Closure|Recursion)' -count=50 ./...
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkAssembly(Call|Vararg|Closure)/' -benchmem -count=9 ./...
CGO_ENABLED=0 GOGC=1 go test -count=20 -run 'Assembly.*(Call|Vararg|Upvalue)' ./...
```

Completion criteria:

- all direct script call shapes stay in assembly and are exact;
- host/effect calls exit once with complete state;
- no per-call allocation/frame reconstruction remains;
- required class speed and >=95% edge coverage pass.

Risks and dependencies:

- indirect dynamic script calls may still be native after a general type/owner
  guard; do not require a source-static callee;
- continuation overflow and protected calls must fail before partial mutation.

### Slice 4.4 - Implement stable globals, tables, properties, and iteration

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: make the packed storage from Phase 2 pay off across
dynamic object/data workloads.

Files and components:

- arm64 global/table/property/iteration handlers;
- descriptor/arena layout includes;
- table/property/iteration differential and guard-miss tests.

Ordered work:

1. Implement dense global descriptor get/set with owner/version guard.
2. Implement numeric array access, bounded open-address hash hit, existing-slot
   write, normalized key/hash checks, and precise miss/grow/structural exits.
3. Implement guarded own-field and finite property-chain traversal from the
   complete descriptor. Check every table/shape/metatable version before using
   a cached slot.
4. For stable scripted `__index`, `__newindex`, `__len`, `__iter`, `__call`,
   arithmetic, concat, relational, and equality targets, enter through the same
   native direct-call/continuation mechanism with the exact operand/result
   convention. Unknown/host targets and `__tostring` string-generation work
   exit with the original guest operation uncommitted.
5. Implement stable array and generic ordered iteration with a scalar cursor.
   Mutation/version mismatch exits before producing the next result.
6. Implement the generated bounded slot-ABI intrinsic inventory: `rawlen`,
   `select`, numeric `math.min`/`math.max`, `next`, and stable `pairs`/`ipairs`;
   existing-capacity `table.insert`/`table.remove` is chunked within the copy
   budget or exits before mutation. `setmetatable`, growth, allocation, and
   coroutine transitions remain precise Go effects. Emit per-intrinsic counts.
7. Cap every hash/property probe and chain step in the verified descriptor.
   There is no unbounded loop inside one assembly operation.
8. Differentially test collisions, misses, chain replacement, shape churn,
   metatable swaps, iterator mutation, holes, numeric zero/NaN, and every exit
   point.
9. Require >=90% stable global/property/table/metatable hits, >=95% stable
   iteration, total side exits within budget, and class-relative speed gates.

Verification:

```sh
CGO_ENABLED=0 go test -run 'Assembly.*(Global|Table|Property|Metatable|Iteration)' -count=50 ./...
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkAssembly(Global|Table|Property|Iteration)/' \
  -benchmem -count=9 ./...
CGO_ENABLED=0 go test -race -run 'OwnerIsolation|Descriptor|Table|Property' ./...
```

Completion criteria:

- stable data/object operations stay in assembly at declared coverage;
- all guard misses and structural effects are exact and uncommitted on exit;
- deterministic iteration and metatable order match Go/reference;
- class speed gates pass without key/source/case specialization.

Risks and dependencies:

- property chains that change frequently may exceed side-exit budget; resolver
  and version design must be general and measured;
- assembly cannot safely mutate Go pointer-rich `Table`; Phase 2 packed storage
  is a hard prerequisite.

### Slice 4.5 - Integrate side exits, quanta, GC, cancellation, and host edges

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: prove the fast backend remains a well-behaved Go citizen
and that real effect boundaries do not erase its gains.

Files and symbols:

- private `executeImage` resume loop;
- `execution_effect.go`, controller/context integration;
- Runtime active-burst/close lifecycle;
- callback/coroutine/module tests;
- diagnostic coverage and scheduler-latency tests.

Ordered work:

1. Route every assembly exit through the same Go effect executor used by the
   oracle. Reject unknown/malformed exit reason or state before resuming.
2. Tune quantum only within the proven safe envelope. Measure 16/32/64 guest
   operations on class-balanced programs; select one general default based on
   wrapper overhead and worst-case heartbeat/GC/cancellation latency.
3. Prove forced GC and STW complete while another goroutine repeatedly runs
   bursts on `GOMAXPROCS=1`. Record maximum latency and fail if a handler can
   exceed the bounded envelope.
4. Preserve fail-busy close: while a run/callback/burst is active,
   `Runtime.Close` neither blocks nor tears down state, returns the existing
   active error immediately, and releases nothing; a later inactive Close
   prevents new entries and releases owner state once. Cancellation is observed
   at the next bounded return/effect and unwinds normally. Test callback-calls-
   Close and burst/lease/close interleavings for no wait/deadlock/use-after-close.
5. Exercise host callbacks, native builtins, module initialization, error/pcall,
   yield/resume, debug hooks, and allocation/growth boundaries. Count actual
   exits and conversion allocations.
6. Keep debug/hook-heavy execution on the Go adapter when exact per-instruction
   callbacks would intentionally transition every operation. This is a policy
   mode over the same image, not benchmark-dependent routing.
7. Run all held-out programs with random quantum boundaries so every operation
   is tested as first/last in a burst and adjacent to an effect exit.

Verification:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 GOGC=1 go test -run 'Assembly.*(GC|Heartbeat|Cancellation|Close)' -count=50 ./...
CGO_ENABLED=0 go test -run 'Assembly.*(Callback|Coroutine|Module|Error|Effect)' -count=20 ./...
CGO_ENABLED=0 go test -race -run 'Assembly.*(Cancellation|Close|Owner)' ./...
CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 -run 'Assembly|ExecutionImage' ./...
```

Completion criteria:

- scheduler/GC/cancellation latency is bounded and measured;
- every effect shares the Go executor and resumes with refreshed roots/bases;
- owner close and concurrent distinct owners are safe;
- total transition and allocation budgets pass on held-out semantic families.

Risks and dependencies:

- assembly itself is invisible to race instrumentation; tests prove ownership,
  not arbitrary memory correctness;
- lowering quantum to fix latency can erase speed. Both gates must pass.

### Slice 4.6 - Apply the backend retention gate

Assigned agent: primary orchestrator, independently reviewed by Sol high.

Objective and reason: decide whether assembly is a complete general backend,
not merely a fast partial path.

Files and evidence:

- all generated/differential tests;
- class coverage and side-exit reports;
- all-37 speed2x artifacts;
- allocation audit and CPU profiles;
- assembly object/disassembly report;
- ADR 0007 decision appendix.

Ordered work:

1. Freeze source and mechanism metadata, then reveal the already-reserved Phase
   4 epoch and generate its exact 5,000 accepted programs. Revealed failures
   join regression tests; only the separately reserved final epoch can support
   the next independent retention decision.
2. Run all 5,000 held-out programs through the old VM oracle, new Go engine,
   and assembly, plus pinned Luau for every externally expressible case, under
   normal, GOGC=1, race-oracle, and checkptr modes. Host callback,
   cancellation, owner/result-lifetime, and state-level cases compare old VM
   against an independently generated expected-state model as well as both new
   engines. Require 100% equivalence; new Go versus assembly alone is never an
   independent semantic oracle.
3. Capture complete semantic-class coverage. Apply every class floor and per-
   family side-exit threshold. A missing denominator is a failure.
4. Run warmed allocation gates and cold image/arena caps. Any recurring hot
   allocation increase is a failure even if speed passes.
5. Take a clean all-37 `speed2x` capture. Require every row <=1.85 median and
   <=2.00 p90. Also require no row regress more than 3% relative to the new Go
   engine unless both engines are comfortably below 1.75 and the change is
   explained by shared representation.
6. Run the production source anti-overfit scan and disassembly checks.
7. Sol reviews raw artifacts, not only summaries, and records retain/delete.

Retention outcomes:

- Retain assembly only if semantics, safety, class coverage, allocations, and
  every-row speed all pass.
- If Go alone passes and assembly fails or adds unjustified complexity, delete
  assembly and retain the Go engine.
- If neither backend passes, delete production assembly, keep the correct Go
  architecture only if its independent gains justify it, mark the 2x objective
  unachieved, and stop. Do not lower thresholds or add case-specific paths.

Verification:

```sh
CGO_ENABLED=0 go test -count=1 ./...
CGO_ENABLED=0 go test -race -count=1 ./...
CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 -count=1 ./...
scripts/check-purego
CGO_ENABLED=0 GOMAXPROCS=1 scripts/performance-audit \
  --output /tmp/ember-speed2x-arm64 --profiles
CGO_ENABLED=0 GOMAXPROCS=1 scripts/execution-allocation-gate \
  capture --manifest all37 --replicate assembly-retention-a
CGO_ENABLED=0 GOMAXPROCS=1 scripts/execution-allocation-gate \
  compare --replicate assembly-retention-a \
  --manifest testdata/runtime-performance/all37-allocation-v1.tsv
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x \
  --schedule testdata/runtime-parity/speed2x-schedule-v1.tsv \
  --replicate assembly-retention-a
```

Completion criteria:

- a signed-off retain/delete decision cites raw correctness, coverage,
  allocation, speed, scheduler, and disassembly evidence;
- no partial assembly tier survives a failed gate;
- no threshold, row, or semantic family is waived.

Risks and dependencies:

- this long gate must run on the pinned quiet machine and clean source;
- `scripts/check-full` remains out of scope because it was not explicitly
  requested; equivalent targeted race/checkptr lanes are listed above.

## Phase 5: Delete the old oracle and prove completion

### Slice 5.1 - Run the final independent semantic gate, then delete the old VM

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: use the old VM for the last independent shared-semantics
check before finishing the replace-don't-layer architecture. Only after that
evidence passes may the project remove the old oracle, mixed Value/slot state,
and duplicate tables/frames.

Files and symbols:

- `vm.go`, `execution_control.go`, generated dispatch/template files;
- `runtime_call.go`, frame/stack helpers;
- old Value-backed table/global/property paths;
- new execution-image/Go/arm64 files;
- public runtime entrypoints, module and callback adapters;
- reserved final held-out epoch and independent expected-state model;
- design/compatibility/public-surface docs.

Ordered work:

1. Reconfirm that `Run`, Runtime hooks, module execution, callback re-entry, and
   coroutine resume already route through `prepareExecutionImage`/
   `executeImage` and explicit public adapters; do not perform a second cutover.
2. Freeze source/mechanisms, reveal the reserved final 5,000-program epoch, and
   run every applicable program through old VM, new Go, selected assembly, and
   pinned Luau. For host callbacks, cancellation, owner/result lifetime,
   limits, and state-level vectors, compare old VM and both new engines against
   the separately generated expected-state model. Seal raw inputs/results and
   require 100% agreement. A mismatch stops acceptance before deletion; fixing
   it requires a new plan and untouched final epoch, not reuse of this one.
3. Select the retained backend privately: accepted arm64 assembly on the target,
   generated Go elsewhere or for controlled/debug modes. Functional behavior
   does not depend on backend availability.
4. Only after the final four-way artifact passes, delete the old generated
   direct loop/template, test-oracle wrapper, and
   generator branches after the call-graph test confirms no production caller
   remained since Slice 3.4. Simplify the generator around the new spec.
5. Delete `vmFrame`/`vmFrameRecord` reconstruction, fixed-call enter/resume
   trampolines, Value-backed runtime stacks, old table/cache storage, and all
   migration converters not used at a public edge.
6. Remove proof/diagnostic routing flags, shadow image construction, alternate
   engine metrics, and rejected assembly/JIT files. Keep only production
   semantic-class diagnostics that are explicitly off in production.
7. Keep the revealed final programs as expected-state/new-Go/selected-backend/
   Luau regression tests, but remove the old-VM-only test wrapper after its raw
   comparison artifact is sealed.
8. Update docs with the breaking API/lifetime migration and one canonical data
   flow. Keep ADRs for rejected designs and benchmark evidence.
9. Run `rg` structural checks for superseded symbols and manual review every
   remaining Value<->slot conversion.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestFinalEpochFourWay|TestFinalHostStateModel)$' -count=1 ./...
# Only after the command above passes and its artifact is sealed, delete the old oracle.
rg -n 'enterRecordOnlyFixedCall|resumeRecordOnly|vmFrameRecord|alternate.*VM|native_probe|MAP_JIT|import "C"' \
  --glob '*.go' --glob '*.s' --glob '!**/*_test.go' .
rg -n 'Value.*slot|slot.*Value' --glob '*.go' --glob '!**/*_test.go' .
go generate ./...
go run ./cmd/ember-vmgen -check
CGO_ENABLED=0 go test -count=1 ./...
scripts/check-purego
```

Completion criteria:

- the fresh final epoch has independent old-VM/Luau/model evidence before old
  code is removed;
- one canonical internal representation, one execution image, one Go semantic
  engine, and at most one accepted arm64 adapter remain;
- no old frame/table/dispatch path is reachable or retained as dead code;
- Value conversion exists only at explicit documented host/public edges;
- public breaking changes are documented with migration examples.

Risks and dependencies:

- deletion is intentionally large. Use symbol/caller searches and focused
  commits, never an unrelated cleanup sweep;
- preserve compiler/checker/public behavior not required by the runtime cutover.

### Slice 5.2 - Run final semantic, safety, allocation, and portability checks

Assigned agent: primary orchestrator, reviewed by Sol high.

Objective and reason: verify the integrated tree rather than isolated backend
tests.

Files and components:

- entire Go module and generated sources;
- existing compatibility/fuzz corpora;
- held-out generator and anti-overfit scan;
- all supported compile targets;
- allocation audit manifest.

Ordered work:

1. Run focused execution, call, table, property, coroutine, callback, module,
   limit, error, owner, and differential tests first.
2. Run `scripts/check-lane root`, `scripts/check-fast`, then `scripts/check`.
   `scripts/check` includes the repository's pure-Go gate.
3. Run explicit race and checkptr suites because native ownership is involved.
4. Compile generic Linux amd64/arm64 and the documented Linux 386 compile lane.
   If the new representation intentionally drops 386, make that a separately
   documented compatibility decision before removing the gate; do not let it
   fail accidentally.
5. After old-oracle deletion, rerun the now-sealed final 5,000-program corpus,
   its independent expected-state traces, and all existing regression/fuzz
   seeds through new Go, the retained backend, and pinned Luau where applicable.
   This is an integrated regression of the independent four-way artifact from
   Slice 5.1, not a claim that forced-Go alone is an oracle.
6. Capture the final all-37 cold/prepared allocation artifact and compare it to
   the frozen per-row/lifecycle manifest. Separately run the five-family
   performance audit for profiles. Fail any warmed B/op/allocs/op increase,
   unreported arena, cold formula/cap overrun, image rebuild, or warm-up breach.
7. Run the linked code-size/W^X/no-foreign-symbol gate on the exact target test
   binary. Require <=256 KiB hot text+dispatch rodata and no writable-executable
   segment, dynamic import, helper backend, or foreign call from Ember objects.
8. Inspect generated code freshness, source anti-overfit scan, ASCII/trailing
   whitespace, and staged file scope.

Verification:

```sh
CGO_ENABLED=0 go test -run 'Execution|RuntimeCall|Table|Property|Coroutine|Callback|Limit|Error|Owner' ./...
scripts/check-lane root
scripts/check-fast
scripts/check
CGO_ENABLED=0 go test -race -count=1 ./...
CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 -count=1 ./...
go run ./cmd/ember-vmgen -check
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/ember-linux-amd64.test .
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -o /tmp/ember-linux-arm64.test .
GOOS=linux GOARCH=386 CGO_ENABLED=0 go build ./...
GOOS=linux GOARCH=386 CGO_ENABLED=0 go test -c -o /tmp/ember-linux-386.test .
CGO_ENABLED=0 go test -c -o /tmp/ember-final.test .
CGO_ENABLED=0 GOMAXPROCS=1 scripts/performance-audit \
  --output /tmp/ember-speed2x-final --profiles
scripts/performance-audit-compare \
  --before /tmp/ember-speed2x-base-a \
  --after /tmp/ember-speed2x-final \
  --manifest /tmp/ember-speed2x-audit-gates.tsv
CGO_ENABLED=0 GOMAXPROCS=1 scripts/execution-allocation-gate \
  capture --manifest all37 --replicate final
CGO_ENABLED=0 GOMAXPROCS=1 scripts/execution-allocation-gate \
  compare --replicate final \
  --manifest testdata/runtime-performance/all37-allocation-v1.tsv
CGO_ENABLED=0 scripts/execution-code-size-gate --binary /tmp/ember-final.test
```

Completion criteria:

- semantic, normal, race, checkptr, pure-Go, generation, and supported-platform
  checks pass;
- held-out differential remains 100%; source anti-overfit scan is clean;
- warmed allocations do not increase and cold caps pass;
- no user-owned or unrelated files are staged.

Risks and dependencies:

- race/checkptr passing cannot instrument assembly; it complements but does not
  replace state differential, ownership, canary, and disassembly evidence;
- do not run `scripts/check-full` without a new explicit user request.

### Slice 5.3 - Take two independent clean speed captures and close the ADR

Assigned agent: primary orchestrator, independently reviewed by Sol high.

Objective and reason: earn the <=2x claim on reproducible clean evidence and
record failures honestly if it is not achieved.

Files and evidence:

- `speed2x` raw artifacts and gate reports;
- `performance-audit.md`;
- ADR 0007;
- `docs/design.md`, `docs/checks.md`, `docs/compatibility.md`;
- `.github/workflows/scheduled.yml` after local acceptance.

Ordered work:

1. Tag the exact clean commit and verify source/toolchain/Luau/runner/schedule
   fingerprints before each capture.
2. Take two complete independent `speed2x` captures. Reacquire an entire
   capture on contamination, nonlinear fit, confidence/spread failure, missing
   row, result mismatch, or metadata drift.
3. Require every row in both captures to meet <=1.85 median and <=2.00 p90.
   Report geomean and worst row only as diagnostics.
4. Re-run semantic-class coverage on the same source and require all backend
   floors. Confirm no benchmark labels enter runtime input.
5. Update `performance-audit.md` with raw artifact fingerprints, per-row table,
   allocation comparison, class coverage, backend code/quantum, and profiles.
6. Mark ADR 0007 accepted only if all gates pass. If any row fails, mark the
   objective unachieved with the exact ratio/class/constraint; do not round it
   down or claim "roughly 2x".
7. After local acceptance, update the scheduled parity job to run `speed2x` and
   upload all artifacts. Preserve the old phase only where it still has an
   explicit purpose. Update `docs/checks.md` and performance docs with explicit
   `scenario` versus `all37` selectors, checked-in schedule hash, replicate
   commands/artifact layout, statistical rules, and the pilot-derived workflow
   timeout so no stale empty/25-row guidance remains.
8. Sol performs a final raw-evidence and code architecture review. Commit and
   push only after its blocking findings are resolved.

Verification:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x \
  --schedule testdata/runtime-parity/speed2x-schedule-v1.tsv \
  --replicate final-a
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x \
  --schedule testdata/runtime-parity/speed2x-schedule-v1.tsv \
  --replicate final-b
scripts/check
git diff --check
```

Completion criteria:

- both independent clean captures pass every row;
- speed, semantics, coverage, allocations, no-CGO, and architecture gates all
  pass at the same source commit;
- scheduled evidence uses the same fail-closed contract;
- ADR 0007 and performance audit accurately state accepted or unachieved.

Risks and dependencies:

- one passing run is insufficient;
- benchmark noise is handled by reacquisition and predeclared statistics, never
  by deleting points or changing thresholds;
- a passing 37-row corpus without held-out/class coverage is still incomplete.

## Commit and integration boundaries

Work directly on `main` as required by the repository. Do not create a branch
or PR unless the user explicitly changes that instruction. After each retained
slice passes its checks:

1. inspect `git status --short` and `git diff --name-only`;
2. stage only files owned by that slice, using explicit paths;
3. inspect `git diff --cached --check` and `git diff --cached`;
4. commit one cohesive outcome with no unrelated user hunks;
5. push `origin/main` only after the commit succeeds and the worktree still
   preserves all unrelated changes.

Recommended retained commit boundaries:

1. speed2x all-row calibrated harness and ADR;
2. held-out generator/differential/anti-overfit gates;
3. passing ABI/assembly architecture proof or its explicit rejection evidence;
4. execution image and verifier;
5. slot/owner cutover;
6. flat stacks/calls/coroutines;
7. packed tables/globals/properties/descriptors;
8. complete generated Go engine/effect executor;
9. each passing assembly semantic island slice;
10. production cutover/deletion;
11. final evidence/docs/scheduled gate.

Do not commit a failed production prototype merely to preserve it. Record its
measurements and delete the dead path in the same retention decision. Temporary
proof commits may be retained only when they are clearly test-only,
independently useful, and add no production routing/allocation.

## Verification matrix

| Concern | Required proof |
| --- | --- |
| Luau semantics | Public tests, old/new differential during migration, pinned Luau oracle, held-out generated corpus |
| Generality | Post-freeze held-out seeds, semantic-class coverage, backend source scan, no named waivers |
| No CGO/FFI | `scripts/check-purego`, no C/C++/foreign symbols/JIT pages, `CGO_ENABLED=0` target build |
| Assembly memory safety | Verified image, typed request boundary, pointer-free mutable records/exit, bounds/probe limits, canaries, exact state differential, disassembly |
| Go GC/ownership | Typed roots, KeepAlive at wrapper, GOGC=1, close/pin tests, no hidden pointer integers |
| Scheduler/cancellation | <=64-op bounded bursts, heartbeat/STW/cancellation latency tests |
| Calls/coroutines | Generated call graphs, exact continuations/results/varargs/limits/errors/yield-resume |
| Tables/properties | Collision/churn/metatable/iteration differential and descriptor invalidation tests |
| Allocations | Frozen warmed B/op/allocs/op ceilings and cold image/arena caps |
| Speed | Two independent clean all-37 paired slope captures, every row 1.85/2.00 |
| Portability | Go fallback builds/tests; Darwin/arm64 assembly target explicitly selected |

## Explicit stop conditions

Stop the campaign and report the target unachieved under current constraints if
any of the following remains after one complete general attempt:

- the Phase 1 vertical proof cannot deliver the baseline-derived multi-x gains
  for calls and table/property families;
- linked assembly cannot provide reliable bounded dispatch under the public Go
  ABI without private runtime hooks or foreign calls;
- direct script calls, stable table/property/metatable hits, or iteration cannot
  stay in the backend at the declared class floors;
- any assembly operation can run unboundedly or delay GC/scheduling beyond the
  proven quantum envelope;
- semantic differential, exact limit/error mapping, owner isolation, race
  oracle, checkptr oracle, or forced-GC tests find a mismatch that cannot be
  fixed generally;
- warmed allocation increases recur in a hot operation;
- the final general lowering still leaves any acceptance row above 2.00 p90;
- meeting the target would require CGO, C/C++, dynamic foreign ABI, upstream
  embedding, runtime JIT/entitlement, helper processes, or benchmark-specific
  recognition.

Failure is useful evidence. Update ADR 0007 and `performance-audit.md` with the
exact clean ratios, class coverage, profiles, attempted architecture, and
external constraint. Do not silently fall back to a new 15% optimization list.

## Definition of done

The work is done only when all of these are simultaneously true:

- one canonical execution module owns image, stacks, continuations, handles,
  tables, descriptors, and effects; every backend-visible mutable record is
  pointer-free, while the typed request and Go root/effect edges are explicit
  GC-visible pointer boundaries;
- production contains a complete generated Go engine and, only if it passed,
  one bounded statically linked Darwin/arm64 Go-assembly adapter;
- the repository has no CGO/C/C++/foreign runtime/JIT/helper execution path;
- all existing and held-out semantics are exact, every native class coverage
  floor passes, and scheduler/GC/owner safety is proven;
- warmed allocations do not increase and cold image/arena costs remain within
  declared caps;
- every one of the 37 frozen workloads passes <=1.85 median and <=2.00 p90 in
  two independent clean captures;
- superseded frames, Value-backed hot state, old dispatch, migration adapters,
  failed probes, and benchmark-specific logic are deleted;
- docs, ADR, audit, checks, and scheduled evidence describe the shipped design
  and its honest limits;
- relevant checks pass, commits contain only intended files, and retained work
  is pushed to `origin/main`.
