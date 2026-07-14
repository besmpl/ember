# Ember Runtime Within 2x of Luau: General No-CGO Architecture

Status: production integration plan; not yet executed; blocked until the
standalone architecture proof records `PROCEED`

Created: 2026-07-14

Proof prerequisite:
[`runtime-speed-2x-no-cgo-proof-implementation-plan.md`](runtime-speed-2x-no-cgo-proof-implementation-plan.md)

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

After the companion proof passes, bind its frozen pointer-free execution
image, independent Go kernel, and bounded Darwin/arm64 handlers to Ember's real
owner graph and public entrypoints. The production cutover completes roots,
pins, GC, modules, callbacks, coroutines, errors, limits, public results, table
growth, descriptors, and portable fallback without inventing a second backend
layout. There is no CGO, C, C++, dynamic foreign-function bridge, embedded
upstream Luau, runtime-generated code, executable-memory mapping, or runtime
helper process.

This is not claimed to be certain. Before an artifact exists, current evidence
supports only roughly 20% to 30% confidence that a complete arm64
implementation can put every frozen workload under 2.00x. A passing companion
proof can raise that to roughly 60% to 65%. Confidence exceeds 90% only after
the full production held-out gates and two independent clean all-row captures
pass. If the companion proof records `STOP`, do not execute this plan and do
not resume a sequence of 15% micro-optimizations.

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

The original architecture plan was created at `09cd4722`; the proof split is
planned from `0ea32563`. The worktree contains user-owned
edits in `base_coroutine.go`, `base_env.go`, `callback.go`,
`module_runtime.go`, `program.go`, `runtime_call.go`, `runtime_heap.go`, and
`vm.go`, plus untracked plans. These overlap the future implementation. An
executor must preserve them and may not stage, revert, overwrite, or build a
clean acceptance artifact from them. Production work starts only after the
companion proof records `PROCEED` and the owner lands, replaces, or otherwise
resolves overlapping edits. Dirty diagnostic work is allowed only when labeled
non-acceptance.

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

This all-37 product policy intentionally differs from the companion proof's
cold-point policy. The proof and Phase 4 integration-tax acquisitions include
`Compile` inside both compared paths so the candidate cannot hide lowering or
image construction. The all-37 runtime target excludes Ember `Compile` for
both baseline and candidate because compiler work is outside the declared
runtime target; Luau child compilation remains an unavoidable fitted
intercept. Proof ratios, integration-tax ratios, and all-37 product ratios are
separate gates and are never divided into or substituted for one another.

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

This is high-risk, cross-cutting production integration. Use one implementation
owner for each hot runtime file group and a Sol review at every architectural
retention gate.
Every agent re-reads `AGENTS.md`, preserves user work, works on `main`, and
does not stage files outside its assigned slice.

Work classification:

- scope: whole production runtime execution core plus final acceptance tooling;
- uncertainty: high even after the prerequisite proof because public ownership
  and effect integration can consume its measured headroom;
- coupling: compiler metadata, VM semantics, ownership/GC, calls, tables,
  coroutines, callbacks, and host entrypoints;
- risk: very high for semantic drift and Go runtime integration, with explicit
  kill gates before irreversible production routing.

| Work | Assigned agent | Why |
| --- | --- | --- |
| Harness, statistics, manifests, docs | Luna high | Exact bounded tooling work |
| Generated semantic corpus and differential oracle | Luna max | Broad correctness and anti-overfit design |
| Owner binding, roots, public lifetimes, stacks, tables | Luna max | Cross-cutting performance-critical integration |
| ARM64 proof promotion and production-only extensions | Luna max | Native correctness and integration uncertainty |
| Architecture/kill-gate review | Sol high | Reject locally attractive but insufficient paths |
| Final integrated correctness/speed review | Sol high | Independent challenge of completion evidence |

Parallel work is allowed only across disjoint ownership. Harness scripts/tests
may proceed beside a runtime slice. No two workers edit `vm.go`,
`vm_dispatch_template.go.tmpl`, `bytecode.go`, `slot.go`, `runtime_heap.go`, or
the generated backend files concurrently. Assembly layout changes and Go state
layout changes are one retention group with one owner.

## Dependency graph

```text
Companion proof plan: exact image/ABI + Go/arm64 class proof
        |
        +-- STOP --> do not execute this plan
        v PROCEED
Phase 0: production all-row + fresh held-out + allocation gates
        |
        v
Phase 2: bind the proven core to owners, public edges, and full state
        |
        v
Phase 3: complete the proven Go engine, then production cut to it
        |
        +-- all rows already pass --> reject assembly, finish
        v
Phase 4: promote and complete the proven ARM64 backend
        |
        +-- coverage/safety/speed fails --> delete assembly; target unachieved
        v
Phase 5: delete old oracle, verify twice, document result
```

## Prerequisite: passing standalone architecture proof

Do not begin Phase 0 or any runtime edit in this plan until the companion proof
has produced all of the following with separately reviewed baseline-surface
and candidate-surface hashes:

- an explicit `PROCEED` decision in ADR 0007;
- frozen operation/image, slot, continuation, table/global/property/iteration,
  request/exit/generic-effect transport, quantum, and owner-cookie layouts,
  plus the explicitly extensible Go-only effect-kind catalog boundary;
- an independently structured Go proof kernel over that ABI;
- passing general arm64 handlers for scalar/control, direct calls/varargs/
  upvalues, globals/tables/properties, stable iteration, and precise effects;
- exact visible and post-freeze held-out differential evidence;
- passing class-relative speed, coverage, allocation, latency, code-size, W^X,
  liveness, disassembly, and no-CGO gates;
- a per-program production integration-tax ceiling of 1.057142857 for median
  and 1.052631579 for p90, derived from the proof/product threshold margins;
- `testdata/runtime-proof/proof-handoff-v1.manifest`, content-addressing every
  retained visible/held-out/transition artifact and exact row inventory,
  including the normalized leaf instruction/constant fingerprint.

Production may bind and extend those artifacts, but may not reimplement them
under a second layout. Any change to a backend-visible layout, operation cost,
retirement rule, quantum, wrapper-liveness rule, or proven handler semantic
invalidates the proof and reruns its affected gates before production
retention. A Go-only opaque effect-kind addition follows the proof's narrower
catalog/vector/normalized-handler identity gate; it escalates to a full proof
rerun if transport, handler behavior, or timed hot transition rates change.

## Phase 0: Make success and overfitting measurable

### Slice 0.1 - Consume the proof decision and promote production gates

Assigned agent: primary orchestrator, reviewed by Sol high.

Objective and reason: verify that this plan is consuming the exact passing
proof, then promote its scoped constraints into fail-closed production and
acceptance gates without changing the proven core.

Files and symbols:

- `testdata/runtime-proof/proof-handoff-v1.manifest` and its raw artifacts;
- `docs/adr/0007-runtime-speed-no-cgo.md`;
- `docs/README.md`, `docs/design.md`, and `performance-audit.md`;
- `docs/checks.md`;
- `scripts/check-purego` and the proof-scoped no-foreign scanner;
- retained generated ABI/layout metadata and proof files.

Ordered work:

1. Verify `testdata/runtime-proof/proof-handoff-v1.manifest`, the exact row
   inventory, and the proof decision is `PROCEED`, then verify that its
   baseline/candidate surfaces, ABI/layout, operation-cost, class-manifest,
   generator, timer, linked-binary, normalized leaf, and raw evidence hashes
   match the retained files. A missing or changed artifact blocks production work; do not
   reconstruct it from a summary. Object/linked hashes may change under normal
   build-tag promotion, so identity of the leaf uses the proof's normalized
   ordered instruction/immediate/constant/branch/table fingerprint.
2. Record `git status --short --branch` and the current HEAD. Preserve all
   user-owned files. Wait for the owner to resolve overlapping edits before a
   clean production baseline or overlapping runtime change.
3. Keep ADR 0007 at "Accepted for production integration", not final product
   acceptance. Link it and the companion proof from `docs/README.md`,
   `docs/design.md`, `performance-audit.md`, and both runtime-speed plans.
4. Promote the proof-scoped no-foreign-runtime scan into
   `scripts/check-purego` and the normal repository check. Reject repo-owned
   production C/C++/Objective-C/object/archive inputs, `import "C"`, cgo/linker/
   wasm import directives, private or foreign `go:linkname`, dynamic-loader
   and executable-memory/JIT APIs, helper-process backend APIs, and foreign
   `CALL`/`BL` targets from Ember-owned assembly. The pinned Luau process is
   allowlisted only in measurement/test paths.
5. Retain fixture coverage for every forbidden mechanism and add production
   paths to the scan. Scope pointer/`uintptr` structural checks to the
   execution image, backend-visible state, request, and exit; public Go edge
   adapters remain explicit pointer-bearing owners.
6. Add structural handoff tests that hash the frozen proof-owned types, core
   generated metadata, generic effect transport, and normalized leaf. A
   Go-only effect catalog extension must prove the leaf does not reference its
   enumerated IDs and pass the generic exit/resume vector. Other
   backend-visible changes fail with an instruction to rerun the companion
   proof.
7. Commit only handoff, ADR/link, and gate changes after focused checks. Do not
   stage unrelated user work.

Verification:

```sh
scripts/runtime-architecture-proof verify-handoff \
  --manifest testdata/runtime-proof/proof-handoff-v1.manifest
scripts/check-purego
scripts/check-runtime-architecture-proof-purego --self-test
go run ./cmd/ember-vmgen -check
git diff --check -- docs/README.md docs/design.md docs/checks.md \
  docs/adr/0007-runtime-speed-no-cgo.md performance-audit.md \
  runtime-speed-2x-luau-implementation-plan.md \
  runtime-speed-2x-no-cgo-proof-implementation-plan.md
```

Completion criteria:

- the production plan is bound to one passing proof artifact group;
- no backend-visible proof contract changed during handoff;
- no-CGO/no-FFI/no-JIT/no-helper rules are fail-closed in normal checks;
- no acceptance capture is labeled clean while runtime files are dirty.

Risks and dependencies:

- this slice cannot turn a `STOP` decision into conditional production work;
- broad scan exclusions can accidentally permit a foreign backend; exceptions
  stay limited to explicit test/measurement oracle paths.

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
- new engine-neutral invocation-event contract and current/candidate/Luau
  adapter fixtures;
- `testdata/runtime-parity/speed2x-schedule-v1.tsv` after clean calibration;
- a checked timeout/job-split recommendation consumed by
  `.github/workflows/scheduled.yml` only after final local production
  acceptance.

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
4. Freeze an engine-neutral invocation event contract before baseline capture:
   immutable input preparation occurs before the point; the timed trace is
   `invoke-start -> execute -> validate -> release -> invoke-stop`. The current
   Ember adapter wraps `[]Value` with an explicit no-op releaser; the candidate
   adapter accepts the owner-bearing `Result` and calls `Close`; the Luau
   adapter maps process exit/result parse to the same validate/release boundary.
   Start one external Go monotonic timer at `invoke-start` and stop only after
   `release`. Remove Luau `os.clock()` from ratio computation. Freeze the event
   contract, adapter interface, and old/new/Luau mapping hashes, but record each
   adapter implementation hash separately because the current and candidate
   APIs intentionally differ. A fixture compares the complete event trace for
   every N, engine, baseline, and candidate and proves release runs on success
   and error.
5. Require live acquisition to name the immutable schedule and a fresh
   replicate ID: `--schedule PATH --replicate ID`. Validate IDs with
   `[a-z0-9][a-z0-9-]{0,31}` and write under
   `tmp/runtime-parity/speed2x/<fingerprint>/<replicate>/`. Existing replicate
   directories fail instead of being reused. Bind the schedule SHA-256,
   calibration source commit, manifest SHA-256, invocation-contract/adapter
   mapping hashes, and acquisition schema version into every raw header and
   fingerprint.
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
   invocation event contract and selected adapter must otherwise be identical
   at all four points.
7. Extend the raw schema with ordered corpus manifest/counts/hashes, per-row N
   schedule, schedule/calibration hashes, source commit and clean status,
   recursive production/harness fingerprint, selected adapter implementation
   hash, invariant invocation-contract/mapping hash, Go toolchain, OS/CPU, timer,
   `CGO_ENABLED`, `GOMAXPROCS`, Luau binary hash, exact Luau argv/default flags
   and verified codegen-disabled mode, acquisition version, all threshold
   constants, and quiet-system samples. Fingerprint all tracked production,
   lowering, generator, harness, `.s`, template/spec, module, and relevant
   script files recursively plus HEAD. A speed2x capture requires a clean tree;
   `--allow-dirty` records a diff hash and is permanently non-acceptance.
8. Make each replicate a real acquisition. The script may validate or report a
   named existing artifact only through a separate read-only command; a normal
   capture never follows the old "one completed acquisition per fingerprint"
   reuse path. Schedule, manifest, argv, threshold, clock, environment, or
   invocation-contract mismatch invalidates the whole replicate. An engine
   implementation hash change is recorded, not confused with invariant
   contract drift. Preserve and report all nine raw
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
    budget including Luau launches and 25% runner margin. Record the required
    independent-job split, timeout, and always-upload contract as a checked
    artifact. Apply it to the scheduled workflow only after final local
    production acceptance. A timeout is a failed/missing capture, never partial
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
- current `[]Value`, candidate `Result.Close`, and Luau process lifecycles use
  one frozen event trace without requiring their implementation hashes to match;
- two replicate IDs necessarily perform two independent acquisitions;
- the previous nonexistent `speed2x` command is now executable.

Risks and dependencies:

- calibration itself can overfit if rerun for each candidate; freezing and
  fingerprinting are mandatory;
- threshold values must be fixed in ADR 0007 before candidate capture;
- shell arithmetic must remain portable to the repo's existing POSIX style.

### Slice 0.3 - Extend the proof corpus to production integration oracles

Assigned agent: Luna max.

Objective and reason: reuse the proven class taxonomy and generator while
adding the real owner/public/effect semantics that the isolated proof
deliberately excluded.

Files and symbols:

- retained proof class manifest, grammar, expected-state model, and revealed
  proof epoch;
- existing execution differential/fuzz tests discovered by `rg` at execution;
- `bytecode.go`: `opcodeMetadataTable`, operand/effect metadata;
- `opcode_info.go`, `register_effects.go`;
- production owner, module, callback, coroutine, limit, error, and public-result
  tests;
- production anti-overfit source scan and diagnostic class reports.

Ordered work:

1. Import the frozen proof semantic taxonomy and all revealed proof cases as
   ordinary regression inputs. Do not rename or reclassify a proven family to
   improve later coverage. Extend the taxonomy only for production-only
   semantics: owner/pin/result lifetime, modules, callbacks, host values,
   protected errors, all limits, cancellation, collection, and coroutine
   lifecycle.
2. Extend the deterministic grammar and independent expected-state/event model
   for real `Run`, `RunWithGlobals`, persistent Runtime, module cycles,
   callback re-entry, owner close, public table/result lifetime, cross-owner
   rejection, host errors, every limit, and yield/resume. Continue to compile
   source through the normal compiler; verified state vectors cover boundaries
   that cannot be expressed externally.
3. Maintain exactly two fresh one-use 5,000-case epochs for this plan: the
   Phase 4 production-backend retention epoch and the final pre-deletion epoch.
   Derive each only after its named mechanism freeze, then seal generator
   version, seed commitment, accepted/rejected counts, per-family counts, and
   corpus hash before execution. Never reuse the companion proof epoch.
4. Compare the old VM, completed new Go engine, selected assembly, and pinned
   Luau wherever externally expressible. For Ember-only host, cancellation,
   owner/result-lifetime, limit, and raw-state cases, compare every engine
   against the independently generated model. Check exact values/NaN bits,
   identity, table mutation/order, metamethod order, callbacks, yields/resume,
   limits, errors, frames, causes, globals writeback, and close behavior.
5. Add real-state differential vectors for every production effect exit,
   descriptor invalidation, arena growth, root refresh, and public adapter.
   Invalid images and mixed-owner requests fail before any backend executes.
6. Expand the proof anti-overfit scan recursively across all production Go,
   generated Go, assembly, build-tag fallbacks, generator specs/templates, and
   linked Ember strings/symbols. Reject all-37 names, source/result hashes,
   fixture paths, and source/fingerprint routing APIs in production backend
   paths. Corpus labels remain measurement/test metadata only.
7. Extend diagnostic counts for production-only semantic/effect/guard classes.
   Reports emit every declared family including zero; a required zero count
   fails.
8. Consume the proof's frozen class-performance gate as architecture evidence;
   do not rebuild the proof inventory or retune its schedule here. New
   production-only semantics are judged by exact behavior, transition/
   allocation budgets, and the all-37 product gate. If an extension changes a
   proven backend layout or handler, rerun the companion proof instead of
   silently adding a new class benchmark.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestExecution.*Differential|TestGeneratedExecution|TestProductionBackendHasNoCorpusFingerprints)$' ./...
CGO_ENABLED=0 go test -race -run '^(TestExecution.*Differential|TestGeneratedExecution)$' ./...
CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 -run '^(TestExecution.*Differential|TestGeneratedExecution)$' ./...
```

Completion criteria:

- proof families remain frozen and production-only semantics have nonzero
  class-balanced coverage;
- two untouched production epochs remain available for their named decisions;
- the independent model covers behavior that Luau or the old VM cannot
  independently establish;
- production source contains no benchmark identity or fingerprint route.

Risks and dependencies:

- the proof Go kernel and assembly are not independent of each other when they
  share metadata; the old VM, pinned Luau, and expected-state model remain
  required until final deletion;
- generated programs must include churn, misses, host effects, errors, and
  lifetime transitions rather than only stable hits.

### Slice 0.4 - Capture clean mechanism, speed, and allocation baselines

Assigned agent: primary orchestrator, reviewed by Sol high.

Objective and reason: freeze the complete product-level speed, allocation,
lifecycle, and static-size baselines that the class-level companion proof did
not attempt.

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
- the frozen companion-proof family gate and ABI/layout hashes.

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
   Use the exact speed2x source, checked-in calibrated N schedule, and frozen
   engine-neutral invocation event contract. Capture through the current
   `[]Value`/no-op-release adapter now; preregister the candidate
   owner-bearing-Result/`Close` mapping before any candidate result exists.
   Include release in allocation/time accounting. Normalize both per public
   invocation and per retired guest operation; also retain a separate
   repeated-hook no-growth lane.
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
8. Import the companion proof's machine-readable family membership, baseline
   ratio/interval, required gain, event minima, and eligible-exit cap. Bind its
   hash and ABI/layout hash into the product baseline. Do not rerun or retune
   the proof class inventory here. If source or a backend-visible contract has
   changed enough to invalidate it, rerun the companion proof before taking a
   production baseline.
9. Freeze per-row/per-lifecycle B/op and allocs/op ceilings from the two all-37
   allocation captures. A candidate may not increase either value on any row;
   the five-family `performance-audit` remains diagnostic and cannot certify
   the other rows. Check the reviewed manifest into `testdata` with source,
   schedule, invocation-contract/adapter-mapping, current-adapter, and raw
   baseline hashes. Candidate captures bind their distinct adapter/engine hash
   and must produce the identical event trace; they fail on invariant contract,
   source-program, schedule, or mapping drift, not merely because the intended
   API/engine implementation hash changed.
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
  exist at one baseline engine hash and one invariant invocation-contract hash;
- every proof family gate is hash-bound to the product evidence, and every one
  of the 37 rows/lifecycles has an allocation ceiling;
- cold formulas, warmed image identity, warm-up break-even, and the static text
  cap have executable gates;
- instrumentation is absent from the production loop and measured separately.

Risks and dependencies:

- target runner time is substantial; partial data cannot authorize production
  state work;
- if source changes invalidate the proof or acceptance baseline, reacquire the
  affected artifact rather than mixing commits.

## Proof implementation lives only in the companion plan

The former Phase 1 implementation has been removed from this document. The
companion proof exclusively owns the proof class timer/baseline, post-freeze
proof epoch, exact image/ABI layouts, synthetic pointer-free state, independent
Go kernel, static arm64 ABI/dispatch/handlers, randomized state differential,
and the class-relative proceed/kill gate.

This production plan consumes only a passing, content-addressed handoff. It
does not keep a second proof adapter, recreate the proof layouts, or repeat the
same scalar/call/table handler implementation under different names.

## Phase 2: Replace the canonical runtime state

Phase 2 is one architectural retention group. It adopts the frozen proof core
inside a separate private candidate owner graph and adds only production
ownership/public/effect state around it; the existing VM remains the production
route and no production entry constructs, mirrors, or warms the candidate.
Phase 3 completes semantics, then Slice 3.4 performs the sole production cut to
the generated Go engine. Phase 4 promotes the proven assembly over that live
state; Phase 5 deletes the now test-only old oracle and migration residue.

### Slice 2.1 - Bind the proven execution image to production ownership

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: adopt the frozen owner-neutral lowerer/image without
reimplementing it, then attach explicit Program and Runtime lifetimes around
the proven backend-visible records.

Files and symbols:

- retained proof image/lowering/ABI files and handoff manifest;
- `program.go`: `Program`, Proto ownership and validation;
- `bytecode.go`: opcodes, metadata, wordcode encoding;
- `vm_dispatch_spec.go`, `cmd/ember-vmgen/main.go`;
- `module_runtime.go`, `runtime_heap.go`;
- new production owner binding, preparation, and focused tests;
- no public package split unless the resulting interface is independently
  useful and smaller than the root implementation.

Ordered work:

1. Verify the retained proof image word, guest-PC/cost map, verifier, lowerer,
   request/exit, and generated metadata hashes before editing production
   ownership. Copy or remove proof build constraints only where necessary; do
   not fork the records or operation inventory.
2. Split preparation into the already-proven shareable immutable lowered-code
   object owned by `Program` and a production owner binding containing runtime
   constants, descriptor bindings, roots, and mutable guard/cache state. The
   backend-visible portion stays byte-for-byte/layout-hash identical to proof.
3. Keep Program and Proto immutable/shareable. Never write owner handles,
   globals, mutable caches, roots, or backend bases into them.
4. Resolve constant and Proto identities to dense production owner indices.
   Materialize string hashes/intern handles, native/call identities, global
   keys, field keys, and descriptor bindings once per owner while preserving
   the proof image's indexed interface.
5. Add an explicit owner-bound image to persistent Runtime ownership. Prepare
   it once at a named preflight, preserve identity across repeated hooks, and
   release it once on successful inactive close. Give stateless preparation
   and release equally explicit lifetimes; constructors hide no repeated work.
6. Classify production-only opcodes/effects through the same generator and
   verifier. Prefer a declared Go effect for semantics outside the proven
   island. If support requires changing a proven word, record, cost, exit,
   quantum, or handler, version the ABI and rerun the companion proof before
   continuing.
7. Add cold benchmarks for owner binding, repeated Runtime hooks, and release.
   Enforce the all-37 cold allocation/byte caps and prove warm calls do not
   rebuild lowered or owner-bound records.
8. Add full opcode inventory tests: every opcode has verified lowering, effect,
   bounded-work, guest-cost, semantic-family, and backend/effect
   classification.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestExecutionImage|TestExecutionOwnerBinding|TestOpcode.*Classified)' ./...
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkExecutionImage' -benchmem -count=5 ./...
go generate ./...
go run ./cmd/ember-vmgen -check
scripts/check-lane root
```

Completion criteria:

- production uses the frozen proof lowerer and backend-visible layout rather
  than a parallel implementation;
- Program/Proto remain owner-neutral and repeated execution reuses one binding;
- every production-only semantic is either classified as a precise effect or
  covered by a newly re-proven ABI version;
- warm preparation allocations remain zero.

Risks and dependencies:

- binding can move preparation into every stateless call; measure ephemeral and
  persistent lifetimes separately;
- a convenience side table per integration concern would destroy the compact
  proof representation; fold state into a few explicit owner arrays;
- this slice depends on the passing companion handoff, not on a recreated
  local prototype.

### Slice 2.2 - Extend the proven slots into the complete owner graph

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: replace the proof's synthetic roots with real production
roots, pins, payloads, and public lifetimes while leaving its hot 64-bit slot
and scalar-array contract unchanged.

Files and symbols:

- `slot.go`: tags, handles, boxed-number escape;
- `value.go`: `Value`, `Table`, closures, cells, public adapters;
- `runtime_heap.go` and collector/root tests;
- `base_env.go`, `base_globals.go`, `module_runtime.go`;
- `callback.go`, `runtime_call.go`, `base_coroutine.go`;
- new owner-slot and public-edge tests.

Ordered work:

1. Adopt the proof's exact slot/tag/handle bits and scalar hot-array layouts.
   Re-run the proof before any change. Preserve every float64 bit pattern;
   colliding NaNs use the proven rare boxed-number handle.
2. Replace the synthetic proof owner with GC-visible production root/payload
   slabs around the unchanged pointer-free arrays. Slots remain
   `{kind,index,generation}` handles, never raw pointers or a `uintptr`
   conversion of a Go pointer.
3. Connect runtime constants, globals, module exports, registers, arguments,
   varargs, results, closures, cells/upvalues, coroutines, native-builtin
   staging, callback staging, and image/cache payloads to those proven arrays.
   Do not introduce a second Value-shaped hot representation during migration.
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

### Slice 2.3 - Bind proven flat calls to threads, limits, and coroutines

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: attach the proven value/continuation mechanics to real
threads, protected state, limits, errors, and coroutine ownership without
rewriting the already-measured direct-call algorithms.

Files and symbols:

- retained proof value stack, continuation, call/arity/result, closure/upvalue,
  and effect records;
- `vm.go`: `vmThread`, `vmFrame`, current direct loop and call paths;
- `runtime_call.go` and `runtime_call_test.go`;
- `base_coroutine.go` and runtime-owner coroutine tests;
- production execution-image call descriptors;
- protected-call, limit, error-frame, and call differential tests.

Ordered work:

1. Adopt the proof's exact continuation record, stack-index conventions,
   call/return/tail/vararg/multi-result operations, guest-PC mapping, and
   capacity exits. A layout or semantic change reruns the proof.
2. Give each production thread one owner-managed value stack and continuation
   stack with the same backend-visible arrays. Replace `vmFrame` reconstruction
   on the candidate path by binding real globals, roots, closures, and
   invocation scope around those indices.
3. Bind actual closure/Proto/arity/upvalue records to the proven dense
   descriptors. Unknown host/native/dynamic calls keep the proof's precise
   effect shape and are completed by the production effect executor.
4. Add real call-depth and instruction limits, protected-call state, error
   frame order/causes, debug observability, and tail-call semantics around the
   proven mechanics. Charges occur exactly once across side exit/resume.
5. Replace proof coroutine model records with stable owner slot/continuation
   blocks. Yield/resume changes active production ownership in Go and resumes
   the same image PC; it does not copy `[]Value` or reconstruct frame objects.
6. Grow stacks only through the existing precise Go capacity exit. Refresh
   roots and typed bases before resume and prove no stale native address
   survives allocation or collection.
7. Differentially test direct, indirect, mutually recursive, tail-recursive,
   fixed/open/vararg, multi-result, closure, upvalue, protected/error, limit,
   and coroutine shapes against the old VM while it remains an oracle.
8. Add structural and profile checks showing the candidate path does not enter
   `enterRecordOnlyFixedCall`, `resumeRecordOnlyFixedCallOne`, construct a
   `vmFrame`, or allocate per direct call.
9. Keep old frame/call code reachable only by the old oracle until the final
   production cut and independent semantic gate; do not delete it in this
   slice.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestRuntimeCall|TestExecution.*Call|Test.*Vararg|Test.*TailCall|Test.*Coroutine|Test.*Protected|Test.*Limit)' ./...
CGO_ENABLED=0 go test -run '^$' -bench 'Call|Vararg|Coroutine' -benchmem -count=9 ./...
CGO_ENABLED=0 GOGC=1 go test -count=20 -run 'Call|Vararg|Coroutine|Protected|Limit' ./...
CGO_ENABLED=0 go test -race -run 'Call|Coroutine|Owner' ./...
```

Completion criteria:

- production threads and coroutines use the proven flat mechanics and exact
  backend-visible records;
- protected calls, limits, errors, suspension, and resume match the old VM;
- no frame-object construction, Value conversion, or per-call allocation
  remains on the candidate direct path;
- the frozen proof class gain remains valid; changed core mechanics trigger a
  proof rerun.

Risks and dependencies:

- production protected/error/coroutine semantics are deliberately outside the
  synthetic proof and carry high integration risk;
- stack growth cannot occur inside assembly and must refresh every root/base
  before resume;
- this slice owns call state; no concurrent worker edits call/frame symbols.

### Slice 2.4 - Bind proven packed data state to real tables and globals

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: replace the proof's synthetic packed owner with real
public/runtime tables, globals, growth, mutation, and metatable invalidation
while retaining the measured lookup/probe/property/iteration algorithms.

Files and symbols:

- retained proof table directory/cell/control/order, global, property,
  iteration, and descriptor layouts;
- `value.go`: `Table`, `tableStorage`;
- `table_ops.go`, `table_shape.go`, `property_ic.go`;
- `base_env.go`: `globalEnv`;
- public table, shape/hash/iteration/metamethod tests;
- production arena, root, growth, and invalidation helpers.

Ordered work:

1. Adopt the proof's exact table directory/cell/control/order, global,
   property, and iteration records plus its bounded probe/chain algorithms.
   Do not implement a parallel production lookup. A record or algorithm change
   reruns the proof.
2. Allocate the proven pointer-free records from owner-managed production
   arenas and attach GC-visible roots/payload slabs outside the backend-visible
   data. Public `Table` views resolve through explicit owner leases rather than
   exposing arena addresses.
3. Bind script/public table creation, import, identity, numeric/string key
   normalization, stable hashes, metatables, and globals to the packed records.
   Preserve the proof hit path exactly for all matching states.
4. Implement production Go effects for allocation, growth, rehash, new-key
   insertion, deletion/compaction, long string work, and structural mutation.
   Each effect updates roots, versions, arena bases, journals, and descriptors
   before rebuilding the request and resuming.
5. Bind host/global mutation to dense global descriptors and complete
   invalidation. A public or callback mutation becomes visible at the exact
   semantic boundary and never leaves a stale stable descriptor.
6. Extend proof property descriptors with real metatable/prototype ownership,
   full metamethod ordering, public table identity, and host/script target
   binding without changing their backend-visible guard fields. Unsupported
   host targets remain precise effects.
7. Bind deterministic production iteration and mutation behavior to the proven
   scalar cursor/order records. Reconstruct invalidated cursors only in Go.
8. Add differential/model tests for collisions, holes, signed zero/NaN, mixed
   keys, public aliases, shape churn, growth, deletion, metatable replacement,
   deep finite chains, descriptor invalidation, iteration mutation, cyclic
   graphs, owner isolation, and callback-observed changes.
9. Enforce cold arena formulas separately. Warm array/hash/global/property/
   scripted-target hits and stable iteration use the proof path, allocate zero,
   and retain the frozen coverage/gain characteristics.
10. Do not revive ADR 0006's rejected table allocator for allocation savings.
    This packed state is retained only as the proven native-accessible canonical
    representation and must pass the full production CPU/semantic gates.

Verification:

```sh
CGO_ENABLED=0 go test -run 'Table|Property|Global|Metatable|Iteration|Hash|Shape|Owner' ./...
CGO_ENABLED=0 go test -race -run 'Table|Property|Global|Metatable|Iteration|Hash|Shape|Owner' ./...
CGO_ENABLED=0 go test -gcflags=all=-d=checkptr=2 -run 'Table|Property|Global|Metatable|Iteration|Hash|Shape' ./...
CGO_ENABLED=0 go test -run '^$' -bench 'Table|Property|Global|Iteration' -benchmem -count=9 ./...
```

Completion criteria:

- real tables/globals use the proof's bounded scalar hit paths;
- every production structural mutation updates roots, bases, versions, and
  descriptors before resume;
- public identity, aliases, deterministic iteration, metatable order, and
  owner isolation are exact;
- warm operations allocate zero without a duplicate Go-table hot path.

Risks and dependencies:

- arena growth may move backing arrays; the request is rebuilt only at a Go
  safepoint after every such move;
- public table lifetime and cyclic aliases make this much riskier than the
  synthetic proof;
- a high structural-exit rate can erase proof headroom and must fail the
  product gate, not motivate benchmark-specific capacity choices.

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

### Slice 3.1 - Promote and complete the proven Go kernel

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: turn the independent proof kernel into the complete
portable production engine while preserving its measured image/ABI and
semantic bodies for the proven island.

Files and symbols:

- retained proof Go kernel, lowerer, image/ABI spec, and generated metadata;
- `vm_dispatch_spec.go`, `cmd/ember-vmgen/main.go`;
- `bytecode.go` opcode/effect metadata;
- production Go burst files and generated output;
- `execution_control.go` for policy integration;
- generation-current and structural tests.

Ordered work:

1. Verify the proof handoff hashes, then promote the independent Go kernel and
   shared generator inputs rather than generating a second switch. Remove proof
   constraints only after the production owner/state bindings match its ABI.
2. Preserve the proven monolithic switch, hot locals, <=64-op burst contract,
   `burstExit`, and scalar/call/table/property/iteration bodies. Do not route
   back into `stepInstruction`, the old direct loop, function-value tables,
   interfaces, or per-operation helpers.
3. Extend the same generated inventory for production-only opcodes and bounded
   semantics. Effectful or unproven work exits to the shared production Go
   effect executor. A change to a proven body/layout/cost is versioned and
   re-proven first.
4. Generate unrestricted, controlled, and diagnostic policy variants from one
   small reviewed spec. Production unrestricted output contains no dormant
   counters, engine flag checks, or instrumentation branches.
5. Add root/base refresh and production owner-cookie handling only at wrapper
   and effect edges. The backend-visible mutable records stay pointer-free and
   identical across Go and assembly.
6. Make stale generation, missing opcode, mismatched layout, unclassified
   effect, or proof-hash drift fail `go run ./cmd/ember-vmgen -check` and
   focused tests.
7. Inspect compiler output for switch dispatch, bounds-check elimination,
   spills, and escapes. Fix data flow rather than adding unsafe directives.
8. Measure proof-kernel versus promoted-kernel transitions on the frozen proof
   corpus. They must be bit-for-bit identical before production-only extensions
   are exercised.

Verification:

```sh
go generate ./...
go run ./cmd/ember-vmgen -check
CGO_ENABLED=0 go test -run '^(TestGenerated|TestGoBurst|TestExecutionImage|TestProofProductionKernelIdentity)' ./...
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkGoBurst/' -benchmem -count=9 ./...
CGO_ENABLED=0 go test -gcflags='all=-m=2' -run '^$' ./... 2>/tmp/ember-escape.txt
```

Completion criteria:

- production has one complete Go engine derived from the proven kernel, not a
  parallel semantic implementation;
- proof-island transitions remain exact and production-only effects are
  fail-closed;
- generated files are current and production variants contain no dormant
  instrumentation;
- recurring warm allocations remain zero.

Risks and dependencies:

- generated source can become a maintenance island; keep human-owned policy and
  metadata small;
- a convenient fallback into the old VM would invalidate both the proof and
  later deletion.

### Slice 3.2 - Implement one Go effect executor and exact control policy

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: replace the proof's synthetic effect model with one real
production effect seam shared by both backends, without changing the proven
exit/retirement protocol.

Files and symbols:

- `execution_control.go`, context/limit/error helpers;
- `callback.go`, `base_coroutine.go`, `module_runtime.go`;
- native builtin and host call adapters;
- new `execution_effect.go` and exit differential tests.

Ordered work:

1. Adopt the proof's closed transport reasons, generic `effect` record, phase,
   retirement, quantum, and request-refresh protocol. Extend only the Go-side
   opaque effect-kind catalog for production completion, budget, descriptor
   miss, stack/arena growth, allocation, table structural mutation, string
   work, host/native call, unknown/metamethod call, protected error, debug hook,
   yield/resume, cancellation, collection, module, and public-adapter effects.
   Assembly copies the verified kind and never branches on catalog IDs. Each
   extension runs catalog validation, the generic exit/resume vector, and the
   normalized handler-identity check. A field, phase, retirement, handler, or
   transport change reruns the companion proof before this slice continues.
2. Replace the synthetic proof effect model with one Go dispatcher from a
   validated exit record to a small pure or effectful production helper. It
   either completes execution, returns an error/outcome, or refreshes roots,
   request bases, descriptors, and ownership before resume.
3. Preserve the proven exact guest instruction cost and pre/post-mutation
   retirement point. A MaxInstructions boundary stops before the over-budget
   operation and reports the same guest PC/error as the old VM and Luau-facing
   contract.
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
2. Run all 5,000 now-revealed companion-proof regression programs, the
   development corpus, and the state-level corpus under normal, race,
   checkptr, GOGC=1, and repeated owner close/reopen. Do not reveal the reserved
   Phase 4 production epoch here.
3. Run the full existing fuzz seed corpus. Add any discovered mismatch as a
   minimized semantic regression test, not as a backend special case.
4. Capture the all-37 sources through an internal test-only Go-engine runner
   using the frozen schedules. Apply allocations and class coverage and record
   profiles even if the speed gate fails. This screening artifact is not the
   official parity capture because production entrypoints have not moved yet.
5. Treat a passing test-only screen as evidence to expect a Go-only finish, not
   as acceptance. Slice 3.4 must still cut the real production route and take
   two official captures before Phase 4 can be skipped.
6. For failing screens, verify the frozen companion proof still predicts enough
   remaining gain for every failing semantic family. If production integration
   has consumed that headroom or changed the mechanism, stop or rerun the proof
   rather than entering a doomed native phase.

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
   retained backend and skip Phase 4. Otherwise proceed only when the frozen
   companion proof still covers every failing family with adequate measured
   leverage and no backend-visible proof contract has changed.

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

## Phase 4: Promote and complete the bounded Darwin/arm64 backend

All Phase 4 files use normal Go build constraints so unsupported targets build
and run the generated Go backend. Performance support is initially declared
for Darwin/arm64 only. Add amd64 later only through a separate measured plan;
do not duplicate a second architecture spec merely to claim portability.

### Slice 4.1 - Promote the proven assembly ABI and bounded dispatcher

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: replace the isolated proof with a production adapter over
the canonical execution image without changing semantics or lifetime rules.

Files and components:

- retained proof wrapper, generated `.s`/include files, and their production
  normal-build-constraint form;
- generic build fallback in `execution_burst_generic.go`;
- `execution_burst_abi.go`, generator, layout tests;
- backend selection inside private `executeImage` only.

Ordered work:

1. Promote the retained proof mechanism as one artifact group: ABI0 wrapper,
   no-call leaf, in-text dispatch, frozen maximum/default quantum, exact
   request/exit spill, generated layouts, and passing handler blocks. Do not
   copy them into a separately editable production implementation. Verify the
   normalized ordered leaf instruction/immediate/constant/branch/table
   fingerprint; do not require proof and production object/linked hashes,
   symbol spelling, addresses, or relocation offsets to be identical.
2. Select assembly only on the explicitly supported Darwin/arm64 target and
   only for verified images with supported semantic version/layout. Generic
   builds always select Go.
3. Keep functional support identical. A backend mismatch or unsupported image
   version falls back before execution begins, never mid-state after mutation.
4. Add a diagnostic force-Go mode available only to tests and benchmark
   tooling. Production API has no user-visible engine choice.
5. Add object-code tests for the normalized proof-leaf identity, no
   calls/foreign symbols, maximum function/text
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

### Slice 4.2 - Bind the proven scalar/control handlers to production state

Assigned agent: Luna max.

Objective and reason: enable the already-passing scalar/control/retirement
handlers on the live canonical state without rewriting or retuning them.

Files and components:

- retained proof arm64 handler source/generated output and hashes;
- production request/image/slot bindings;
- scalar/control differential vectors and disassembly checks;
- production limit/error/effect executor.

Ordered work:

1. Verify the proof handler, generated metadata, image word, slot, cost,
   retirement, quantum, and normalized leaf fingerprint. Promote the same
   handler blocks under normal Darwin/arm64 constraints; do not create
   production variants of their semantic bodies. Raw object/linked hashes are
   evidence for each build, not the cross-build identity contract.
2. Bind live value-stack, image, descriptor, budget, and exit bases through the
   production request. Assert their offsets/layout hashes equal proof before
   the backend is selected.
3. Route production-only boxed-number allocation, metamethod ambiguity,
   unsupported pow/transcendental/string-number work, errors, hooks, and limits
   through the precise shared Go effect path. Do not widen a proven native
   handler merely to avoid an honest effect.
4. Differentially run real production scalar/control states, IEEE edge vectors,
   randomized CFGs, and every quantum/instruction-limit cut through promoted
   assembly and the completed Go engine.
5. Re-run proof class coverage, speed, allocation, latency, and disassembly
   checks after linking into production. A source or semantic change to a
   handler reruns the companion P2/P3 gates before retention.
6. Prove no production Value conversion, old-loop dispatch, recurring
   allocation, or extra spill/transition has entered the promoted path.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestAssemblyScalar|TestAssemblyControl|TestAssemblyLimitCut|TestProofHandlerIdentity)' -count=50 ./...
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkBurst(Scalar|Control)/' -benchmem -count=9 ./...
CGO_ENABLED=0 go test -c -o /tmp/ember-arm64-scalar.test .
go tool objdump -s '.*runAssemblyBurst.*' /tmp/ember-arm64-scalar.test
```

Completion criteria:

- production executes the normalized-instruction-identical proven
  scalar/control handlers;
- real state/PC/count/limit behavior matches the Go engine at every cut;
- no recurring allocation or avoidable Go transition appears;
- frozen class gain and >=99% eligible coverage still pass.

Risks and dependencies:

- production wrapper/root work can consume proof headroom despite identical
  handler code; fail the gate rather than retuning to named rows;
- any semantic handler change is proof work first, production promotion second.

### Slice 4.3 - Bind proven call/upvalue handlers to real closures and effects

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: connect the proven flat call/continuation/vararg/upvalue
handlers to production closures, roots, protected state, and host/coroutine
effects without recreating their mechanics.

Files and components:

- retained proof call/return/continuation/vararg/upvalue handlers;
- production call/arity/result descriptors and owner-bound closure/upvalue
  records;
- completed flat stacks and production effect executor;
- generated call-graph, limit, protected-call, and coroutine differential
  corpus.

Ordered work:

1. Verify and promote the proof's direct/fixed/open/tail/recursive call,
   continuation, return, vararg, multi-result, closure-invocation, and upvalue
   handler blocks unchanged.
2. Bind real owner-relative closure/Proto/upvalue handles and production
   call/arity/result descriptors to their frozen fields. Mixed-owner, stale,
   dynamic, or host targets exit before mutation.
3. Attach real call-depth limits, protected-state IDs, stack/error frames,
   callback/native effects, module calls, coroutine yield/resume, and capacity
   growth through the production Go effect executor. Refresh every root/base
   before re-entry.
4. Keep stable scripted metamethod targets on the same proven direct-call path
   when their production descriptor validates. Unknown/host targets remain
   effects.
5. Generate production direct/indirect/mutually recursive/tail/fixed/open/
   vararg/multi-result/closure/upvalue graphs plus protected, limit, module,
   callback, and coroutine boundaries. Compare every intermediate state and
   exit to the Go engine and old oracle.
6. Re-run proof allocation/latency/class gates and require >=95% native direct
   script edges. A handler extension or record change is implemented and
   measured in the companion proof before promotion.
7. Add structural checks that direct script calls never enter the old frame
   trampolines or construct Go slices/frames on the assembly path.

Verification:

```sh
CGO_ENABLED=0 go test -run 'Assembly.*(Call|Return|Vararg|Upvalue|Closure|Recursion|Protected|Coroutine)' -count=50 ./...
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkAssembly(Call|Vararg|Closure)/' -benchmem -count=9 ./...
CGO_ENABLED=0 GOGC=1 go test -count=20 -run 'Assembly.*(Call|Vararg|Upvalue|Coroutine)' ./...
```

Completion criteria:

- production uses the proven direct-call handlers and flat records unchanged;
- real protected/host/module/coroutine boundaries exit once with complete state;
- no per-call allocation, frame reconstruction, or Value conversion remains;
- required class speed and >=95% edge coverage still pass.

Risks and dependencies:

- production protected/coroutine ownership was outside the proof and must be
  established at the Go edge before native resume;
- continuation overflow and every rejected limit charge fail before partial
  mutation.

### Slice 4.4 - Bind proven data handlers to production descriptors and tables

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: connect the proven global/table/property/iteration
handlers to the live packed arenas, public mutation, metatables, and complete
descriptor invalidation without reimplementing stable hits.

Files and components:

- retained proof global/table/property/scripted-target/iteration handlers;
- production descriptor/arena layout includes and owner roots;
- full table/metatable/intrinsic effect executor;
- production data differential and guard-miss corpus.

Ordered work:

1. Verify and promote the proof's dense global, bounded array/hash,
   existing-cell write, guarded property-chain, stable scripted target, and
   stable iteration handlers unchanged.
2. Bind production arena bases, table handles, shape/mutation/metatable
   versions, key normalization, descriptor chains, and iteration cursors to the
   frozen request/record fields. Assert layout hashes before selection.
3. Route growth, rehash, insertion, deletion, compaction, public/host mutation,
   long string work, setmetatable, unknown targets, and cursor invalidation
   through exact pre-mutation Go effects. Rebuild roots, bases, versions, and
   descriptors before resume.
4. Exercise every supported scripted metamethod target through the proven
   call/continuation path with real operand/result conventions. Unknown/host
   targets and string generation remain effects.
5. Promote only proof-validated bounded slot intrinsics. Any new native
   intrinsic or chunked table operation first extends the generated proof
   inventory and reruns its semantic, work-bound, coverage, speed, and latency
   gates.
6. Differentially test collisions, holes, signed zero/NaN, shape churn,
   metatable replacement, deep finite chains, aliases, iterator mutation,
   growth, deletion, and every effect/resume point against Go, old VM, and the
   independent model.
7. Re-run production anti-overfit, class coverage, allocations, and speed.
   Require >=90% global/table/property attempts, >=95% stable iteration, and
   the frozen eligible-exit ceilings without key/source specialization.

Verification:

```sh
CGO_ENABLED=0 go test -run 'Assembly.*(Global|Table|Property|Metatable|Iteration)' -count=50 ./...
CGO_ENABLED=0 go test -run '^$' -bench '^BenchmarkAssembly(Global|Table|Property|Iteration)/' \
  -benchmem -count=9 ./...
CGO_ENABLED=0 go test -race -run 'OwnerIsolation|Descriptor|Table|Property' ./...
```

Completion criteria:

- stable production data operations execute the proven handlers at declared
  coverage;
- every guard miss and structural/public effect exits uncommitted and resumes
  with refreshed state;
- deterministic iteration, aliases, and metatable order match all references;
- class speed gates pass without source/key/case specialization.

Risks and dependencies:

- real mutation may raise side-exit frequency beyond the synthetic proof;
  production coverage and all-37 timing decide retention;
- assembly never touches pointer-rich public `Table`; only the canonical packed
  owner state is backend-visible.

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
2. Use the proof-frozen general quantum and work bounds. Re-measure wrapper
   overhead and worst-case heartbeat/GC/cancellation latency on production
   state. If integration requires a different default or bound, return to the
   companion proof, freeze and pass it there, then promote the new version.
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
- retained owner-neutral proof artifacts and
  `testdata/runtime-proof/proof-handoff-v1.manifest`;
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
5. At the frozen production mechanism, load the exact ordered inventory from
   `proof-handoff-v1.manifest` and run every visible, composition, and selected
   held-out performance program through the owner-neutral proof assembly path
   and integrated production assembly path in two fresh paired acquisitions
   using the proof's exact schedules and cold-point event mapping: sealed source
   is the input, and `Compile`, lowering/binding, execution, digest validation,
   result release, and cleanup are inside both paired timers. This mapping is
   deliberately independent of the final all-37 runtime policy below. For
   paired fit `i`, define `D_i=productionSlope_i/proofSlope_i`,
   `M_D=median(D_i)`, and `Q_D=max(D_i)`. In both captures, every individual
   program must have `M_D <=1.057142857` and `Q_D <=1.052631579`. Apply the
   same two ceilings to the median and maximum paired
   `productionElapsed_i(N)/proofElapsed_i(N)` at each scheduled N so lifecycle/
   intercept overhead cannot disappear into the slope. No class, stratum, or
   aggregate may substitute for a failing row. This tax includes real owner
   binding, public/result lifecycle, root refresh, descriptors, and real effect
   handling that occurs in the program. A missing proof artifact, incompatible
   semantic core, failed fit, or exceeded program budget is a hard stop before
   backend retention and old-oracle deletion.
6. Take a clean all-37 `speed2x` capture. Require every row <=1.85 median and
   <=2.00 p90. Also require no row regress more than 3% relative to the new Go
   engine unless both engines are comfortably below 1.75 and the change is
   explained by shared representation.
7. Run the production source anti-overfit scan and disassembly checks. Add
   self-tests for an omitted/extra/duplicate/hash-mismatched row or artifact,
   schedule substitution, ratio-of-summary instead of paired `D_i`, and
   attempted class aggregation.
8. Sol reviews raw artifacts, not only summaries, and records retain/delete.

Retention outcomes:

- Retain assembly only if semantics, safety, class coverage, allocations,
  every-class integration tax, and every-row speed all pass.
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
CGO_ENABLED=0 GOMAXPROCS=1 scripts/runtime-architecture-proof integration-tax \
  --proof-handoff testdata/runtime-proof/proof-handoff-v1.manifest \
  --production --replicate assembly-integration-tax-a
CGO_ENABLED=0 GOMAXPROCS=1 scripts/runtime-architecture-proof integration-tax \
  --proof-handoff testdata/runtime-proof/proof-handoff-v1.manifest \
  --production --replicate assembly-integration-tax-b
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
  allocation, integration-tax, speed, scheduler, and disassembly evidence;
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

1. verified proof handoff plus promoted production no-foreign gate;
2. speed2x all-row calibrated harness;
3. production held-out/differential/anti-overfit and allocation baselines;
4. execution-image owner binding;
5. slot/root/pin/public-lifetime cutover;
6. flat-stack production calls/errors/limits/coroutines;
7. packed public tables/globals/properties/descriptors;
8. completed production Go engine/effect executor;
9. each passing proof-handler promotion or production-only extension;
10. production cutover/deletion;
11. final evidence/docs/scheduled gate.

Do not commit a failed production adapter merely to preserve it. Record its
measurements and delete the dead path in the same retention decision. The
companion plan owns proof retention; this plan may not keep a second tagged
prototype after the promoted path exists.

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

- the companion proof records `STOP`, its handoff cannot be verified, or a
  later backend-visible change fails the required proof rerun;
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
- integrated production slope or point-total overhead exceeds the frozen
  proof-to-production tax ceiling for any performance program in either
  capture;
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
