# Ember General No-CGO Runtime Architecture Proof

Status: standalone implementation plan; not yet executed

Created: 2026-07-14

Companion production plan:
[`runtime-speed-2x-no-cgo-architecture-implementation-plan.md`](runtime-speed-2x-no-cgo-architecture-implementation-plan.md)

## Purpose and decision

This plan answers one question before Ember rewrites its production runtime:

> Can a general, statically linked Darwin/arm64 Go-assembly burst engine over
> the intended pointer-free execution representation run compiled Luau-shaped
> programs with enough end-to-end leverage to make the final <=2x Luau target
> credible, without CGO, FFI, JIT code, benchmark recognition, or recurring
> hot-path allocation?

The answer is a binary `PROCEED` or `STOP`. A passing result authorizes the
production integration plan. A failing result stops the architecture campaign
before public APIs, runtime ownership, tables, frames, modules, callbacks, or
coroutines are migrated.

This is an executable architecture proof, not a paper review and not an
arithmetic microbenchmark. Normal Ember source must pass through `Compile`,
verified `Proto` wordcode, the proposed execution image, the actual Go wrapper,
bounded assembly bursts, precise Go side exits, and resume. The proof includes
direct calls, continuations, varargs, upvalues, globals, tables, properties,
iteration, and effect exits because prior narrow engines won isolated cases but
covered 0% of the Scenario workload.

The proof is intentionally not constrained by implementation size. Its
boundary is the complete vertical slice needed to falsify the architecture
itself; cutting out calls, data access, held-out programs, or effect/resume
timing would prove only another local optimization.

## What a pass does and does not prove

A `PROCEED` result proves, on the pinned target and frozen proof corpus, that:

- the proposed Go/assembly ABI is linkable, bounded, pointer-safe by contract,
  and usable with `CGO_ENABLED=0`;
- one general execution image and state layout can represent the required hot
  semantic families without workload-specific recognition;
- an independently structured Go kernel and the arm64 leaf produce exact
  state, result, exit, and accounting transitions;
- the end-to-end assembly path, including wrapper, quantum return, and every
  source-comparable side exit/effect/resume in a hot class program, reaches the
  preregistered class-relative speed headroom;
- the transport overhead for production-only effects that have no comparable
  Luau operation is independently bounded without pretending the synthetic
  proof effect body represents the later production work;
- native coverage is broad enough that the measured gain is not an
  arithmetic-only or call-only artifact;
- warmed proof execution adds no recurring allocation per instruction, call,
  table/property hit, iteration step, burst, or side-exit/resume cycle;
- the mechanism generalizes to a post-freeze generated proof epoch.

A pass does not prove that the shipped runtime already meets the final all-37
`<=1.85` median and `<=2.00` p90 gates. The production plan still owns:

- the canonical `Run`, `RunWithGlobals`, `Runtime`, module, callback, coroutine,
  owner, heap, GC, public `Table`, result-lifetime, and cross-owner cutovers;
- the complete all-37 calibrated acquisition harness and two final clean
  captures;
- all-37 cold/prepared allocation ceilings and final arena/image formulas;
- complete limits, errors, protected calls, host effects, module cycles,
  coroutine lifetime, and callback re-entry;
- production backend routing, portability fallback, old-VM deletion, and
  scheduled evidence.

The proof therefore raises architecture confidence; only the companion plan's
final integrated evidence earns the product claim.

The proof also reserves a hard integration-tax budget. For every frozen
visible, composition, and selected held-out hot program, the later owner/public
production path may be at most
`1.85/1.75 = 1.057142857x` the proof median and
`2.00/1.90 = 1.052631579x` the proof p90. The production retention gate must
compare the integrated path with this retained proof path before accepting the
backend. Exceeding either budget is `STOP`; the margin is not available for
threshold rounding or a named-row waiver.

## Target and hard constraints

The proof decision is valid only on:

- Darwin 24.6.0;
- Apple M1, arm64;
- Go 1.26.4;
- `CGO_ENABLED=0` and `GOMAXPROCS=1`;
- pinned Luau 0.728 at SHA-256
  `c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd`;
- Luau's interpreter at its default optimization level, with Luau codegen
  disabled.

The proof and retained artifacts must contain none of the following:

- `import "C"`, C/C++/Objective-C source, cgo directives, archives, or foreign
  object files;
- dynamic FFI, `dlopen`, private runtime linkage, or foreign `CALL`/`BL`
  targets from Ember assembly;
- upstream Luau embedded, linked, translated into the backend, or used as a
  product execution path;
- runtime-generated code, executable-memory mappings, `MAP_JIT`, RWX or
  RW-to-RX pages, or host JIT entitlements;
- helper processes used to execute Ember programs;
- new dependencies without explicit user approval.

Checked-in or reproducibly generated Go arm64 assembly is allowed. The pinned
Luau executable is allowed only in test/measurement orchestration and is never
called by the proof backend.

## Generality contract

Proof and production-intended code may branch on semantic metadata only:

- opcode/effect/operand class;
- slot kind and owner-relative handle kind;
- call mode, arity, result mode, continuation state, and upvalue state;
- table shape/version, bounded probe state, property-chain guards, and
  iteration state;
- precise effect and exit reason;
- target backend availability.

It may not inspect or branch on source text, hashes, fixture names, corpus
membership, function names, constants copied from measured programs, trip
counts, Proto IDs, benchmark case names, or acceptance result values. A
generated source scan covers proof Go, assembly, generator inputs, templates,
and linked strings. The proof mechanism is frozen before the held-out epoch is
derived.

## Current repository shape

At plan creation, `main` is at `0ea32563`. The worktree contains user-owned
changes in `base_coroutine.go`, `base_env.go`, `callback.go`,
`module_runtime.go`, `program.go`, `runtime_call.go`, `runtime_heap.go`, and
`vm.go`, plus untracked user plans. An executor must preserve those files and
must not stage, revert, overwrite, or use their dirty state as clean acceptance
evidence. Non-overlapping proof tooling may be developed while the tree is
dirty, but the baseline and decision captures require one clean, recorded
source fingerprint.

Relevant existing seams are:

- `compiler.go`: `Compile` and `CompileWithOptions`;
- `bytecode.go`: `opcodeMetadataTable`, `Proto`,
  `finalizeProtoExecutionArtifact`, verification, and `encodeProtoWords`;
- `wordcode.go`: canonical wordcode encoding;
- `vm_dispatch_spec.go`, `vm_dispatch_template.go.tmpl`, and
  `cmd/ember-vmgen/main.go`: generated direct-loop metadata and conventions;
- `vm_dispatch_generated.go`, `vm.go:runUntilDepthResult`, and
  `vm.go:runFrame`: current production baselines, not independent proof
  oracles;
- `slot.go`: the existing private 64-bit slot encoding;
- `runtime_heap.go`: pointer-rich slabs that the proof must not expose to
  assembly;
- `value.go`, `property_ic.go`, and `base_env.go`: current pointer-bearing
  table, property, and global state;
- `execution_differential_test.go`: existing source corpus, opcode coverage,
  values/errors/globals/events comparison, and missing-op checks;
- `execution_direct_safety_test.go` and `vm_dispatch_structure_test.go`:
  existing execution-limit and structural safety tests;
- `runtime_parity_test.go`, `scripts/check-runtime-parity`, and
  `scripts/scenario-ratio-gate`: current parity tooling, whose mixed timer
  domains, fixed N values, Scenario-default inventory, and reusable artifact
  path are not valid proof measurement contracts.

The deleted slot, compact-call, and direct-kernel experiments recorded in ADR
0005 are important negative evidence. The proof must not recreate an alternate
engine that wins isolated operations but cannot execute complete compiled
class programs.

## Proof architecture

```text
ordinary Luau-shaped source
          |
          v
     Ember Compile
          |
          v
 verified Proto / wordcode
          |
          v
 owner-neutral proof lowerer
          |
          v
 frozen execution image
 - fixed-width semantic words
 - guest-PC and cost maps
 - Proto/call/arity descriptors
 - global/table/property descriptors
          |
          +---------------------------+
          |                           |
          v                           v
 independent Go proof kernel    Darwin/arm64 leaf bursts
          |                           |
          +-------------+-------------+
                        v
                   burstExit
             exact PC/count/effect
                        |
                        v
                proof Go effect model
                        |
                        v
                     resume
```

The proof uses the intended production scalar layouts:

- 64-bit slots and owner-relative handles;
- one flat value stack;
- one compact continuation stack;
- pointer-free closure/upvalue projections;
- pointer-free global, table directory/cell/order, property descriptor, and
  iteration records;
- a typed-pointer request boundary whose pointer fields are a generated closed
  allowlist;
- a completely pointer-free mutable state graph and `burstExit` behind that
  boundary.

The proof may use a synthetic owner/root store and a bounded pure Go effect
model. It must not route through production `runtimeOwner`, `runtimeHeap`,
public `Table`, `Runtime`, module, callback, or coroutine-lifetime state. Those
are integration work. Synthetic roots may hold Go pointers, but assembly sees
only typed base pointers supplied in the request plus pointer-free indexed
records.

## Frozen proof gates

Thresholds are fixed before candidate timing.

### Semantics

- Every preregistered source program must agree across current Ember, the
  independent Go proof kernel, and assembly.
- Every externally expressible source program must also agree with pinned
  Luau.
- Every randomized transition must match all mutable slots, continuations,
  descriptors, table cells/order state, PC, guest count, primitive count,
  phase, and exit reason.
- Values, NaN bits, errors, globals, event order, retired-instruction
  accounting, and effect resumption must be exact. There is no mismatch
  allowance.
- Invalid images must be rejected before either proof engine executes them.

### Speed

The manifest declares two disjoint sets before candidate code or timing exists:

- **performance-required hot families** are exactly the nine canonical family
  IDs in the fixed taxonomy table below. They cover scalar/control, loops,
  script calls, varargs/results, closures/upvalues, globals, tables,
  properties/scripted targets, and stable iteration. They have equivalent
  compiled source and observable work in current Ember, the proof engines, and
  Luau.
- **semantic/transition-only effect families** are exactly the eight canonical
  IDs in the transition table below. They cover synthetic owner/root work,
  allocation/growth and invalidation transport, host callback/re-entry,
  cancellation/GC/close, and error/coroutine/limit boundaries for which the
  proof cannot perform production-equivalent work in Luau. They prove state,
  retirement, bounded exit/resume transport, liveness, and allocation behavior;
  they do not receive a synthetic proof/Luau speed ratio.

Each performance-required hot family has at least two preregistered
compiled-source programs in each of at least two semantic shape strata, plus a
generated composition program. After mechanism freeze, the held-out epoch also
contributes the first two sealed cases per stratum in deterministic corpus-hash
order. Selection uses only preregistered semantic-event minima, never
calibration success or candidate time. For every visible and held-out
performance program:

- measure nine independent paired four-point slope fits with one external Go
  monotonic timer domain for current Ember, proof assembly, and Luau;
- include the proof wrapper, burst returns, side exits, effect execution, and
  resume in proof elapsed time;
- let `B_i=current_i/Luau_i`, `C_i=assembly_i/Luau_i`, and
  `G_i=assembly_i/current_i` for paired slope fit `i`;
- freeze `M_B=median(B_i)` and `Q_B=max(B_i)` from the current/Luau baseline;
  for each fit, form a conservative upper ratio from the current-slope upper
  95% CI endpoint divided by the Luau-slope lower endpoint, then freeze
  `U_B=median(upper_i)` and `U_Q=max(upper_i)` for each of the two baseline
  captures, then use the larger capture value for each frozen gain gate;
- for each candidate capture require `M_C=median(C_i) <=1.75`,
  `Q_C=max(C_i) <=1.90`, `M_G=median(G_i) <=1.75/U_B`, and
  `Q_G=max(G_i) <=1.90/U_Q`;
- freeze `I_cap` per program as the larger upper 95% fitted-entry endpoint from
  either current Ember or Luau across both baseline captures. Every assembly
  fit must have its own upper 95% fitted-entry endpoint `<= I_cap`, fitted body
  share >=50% at Nmin and >=90% at Nmax, and total elapsed within the frozen
  1-250 ms point range;
- for each scheduled N, let `P_i(N)=assemblyElapsed_i(N)/LuauElapsed_i(N)` over
  the same fit index, N point, and cold lifecycle (never a ratio of separately
  summarized times), and require `median(P_i(N)) <=1.75` and
  `max(P_i(N)) <=1.90`; these point-total gates keep Compile/lowering/state
  preparation/digest/cleanup cost from disappearing into the OLS intercept;
- report `M_B` and `Q_B`; never substitute them for the conservative
  `U_B`/`U_Q` gain denominators;
- fail a missing, unsupported, noisy, nonlinear, or invalid program rather
  than estimating or waiving it.

Every source-comparable side exit that occurs in one of those programs remains
inside its timed slope. Separately, each transition-only effect family runs a
batched transport test at frozen rates of one effect per 1, 8, and 64 guest
operations. The Go kernel and assembly use the same noinline fixed scalar
effect body and exact input/output state. P1.2 freezes one four-point geometric
batch schedule per rate using the Go kernel only, before assembly timing; each
point is 1-100 ms, `Nmax/Nmin >=16`, and fitted body share is >=90% at Nmax.

In each of the two proof captures, acquire nine independent paired Go/assembly
fits with balanced order and the same external Go monotonic timer/OLS quality
rules as the source gate. Retain every per-engine/per-fit/per-batch raw elapsed
value and lifecycle hash. Let `R_i=assemblyCycleSlope_i/goCycleSlope_i` and
`A_i=assemblyCycleSlope_i` in ns/transition. Nearest-rank p90 of nine is the
maximum; require `max(R_i) <=1.25` and `max(A_i) <=100 ns` for every family and
rate. Do not subtract an empty-loop or timer estimate: batching makes timer
cost an intercept while the measured slope retains wrapper/exit/effect/resume.
The cycle must allocate zero and preserve exact state/accounting. This bounds
architecture transport only. The production plan remains responsible for the
cost and semantics of real host, owner, GC, callback, and coroutine work.

The independent Go proof kernel is measured and reported, but it need not meet
the arm64 speed threshold. It is the portable semantic oracle for the later
production implementation.

### Coverage and exits

Across every frozen and held-out class program:

- >=99% of eligible scalar/control operations execute in assembly;
- >=95% of direct script call/return/tail/vararg edges execute in assembly;
- >=90% of already-created closure/upvalue operations execute in assembly;
- >=90% of all global/table/property attempts execute in assembly, with
  stable hits, guarded misses, and structural effects reported separately;
- >=95% of stable iteration steps execute in assembly;
- unsupported exits are <=5% of backend-eligible operations per family and
  <=2% for a family whose baseline requires more than a 2x Ember reduction;
- allocation, growth, host/coroutine/error, and other mandatory effects are
  counted separately; source-comparable effects in a performance program stay
  inside end-to-end timing, while transition-only effects use their separate
  transport gate and never receive a Luau ratio.

A required family with zero dynamic events fails. Mandatory effects are not
misclassified as eligible misses to improve a denominator.

The frozen manifest also carries `family_kind`, `shape_stratum`,
`minRetiredOps`, `minEligibleOps`, `maxMandatoryEffectGuestShare`, and
`maxMandatoryEffectTimeShare`. Every performance program at N=1 must retire at
least 256 guest operations, contain at least 128 backend-eligible operations,
keep mandatory effects to <=10% of retired guest operations and <=20% of the
full current-Ember calibrated N-body slope (mandatory-effect-attributable slope
divided by total body slope, with exit/effect/resume work included), and satisfy
its stricter family event minima. Each hot family has at least two predeclared
semantic shape strata.
The cross-family composition contains at least 64 eligible operations from
each of at least three hot families. Gate kind is sealed before timing: a
declared performance program above either cap fails the manifest or candidate;
it is never reclassified, dropped, or replaced as transition-only evidence.

The allowed performance stratum taxonomy and additional N=1 event minima are
also frozen before candidate code. The backticked first column contains the
canonical manifest IDs; aliases are forbidden. These are semantic shapes, not
workload labels. Counts may overlap when one guest operation legitimately
satisfies multiple minima, but every event still has one canonical coverage
classification:

| Canonical family ID | Required strata | Minimum in each program | Minimum across the two visible programs in each stratum |
| --- | --- | --- | --- |
| `scalar-control` | `arithmetic-compare`, `truth-branch` | 128 family-eligible ops; 64 primary-stratum ops | add, subtract, multiply, divide/mod each 16; comparisons 32; taken and not-taken truth branches each 16 |
| `numeric-loop` | `ascending-unit`, `descending-nonunit` | 128 loop-body ops, 32 backedges, 4 exits | each declared direction/step shape contributes at least 64 body ops and 16 backedges |
| `script-call` | `fixed-recursive`, `open-tail` | 64 script call edges, 64 returns, 32 primary-stratum edges | fixed, open, tail, and recursive edges each 16 |
| `vararg-result` | `argument-adjust`, `result-adjust` | 64 argument/result adjustments, 32 primary-stratum adjustments | empty/nonempty varargs and fixed/open multi-results each 16 |
| `closure-upvalue` | `open-mutate`, `closed-invoke` | 64 closure/upvalue ops, 32 primary-stratum ops | upvalue reads, writes, closure invokes, and closes each 16 |
| `global` | `read-dominant`, `write-versioned` | 128 global accesses, 64 primary reads/writes | guarded reads and existing-slot writes each 32 |
| `table` | `dense-array`, `hash-collision` | 128 attempts, 64 primary hits, 16 existing-cell writes | array hits, hash hits, and existing writes each 32; bounded collisions and guarded misses each 8 |
| `property-target` | `own-shallow`, `deep-scripted` | 96 property attempts, 64 primary hits/entries, 8 guarded misses | own hits, depth>=2 chain hits, and scripted target entries each 16 |
| `stable-iteration` | `dense-array`, `generic-ordered` | 128 successful steps, 8 enter/end pairs | the assigned array or generic form supplies 96 steps; holes/end transitions each 8 |

Every visible or preselected held-out performance program must prove its
assigned row/stratum from the sealed current-engine and independent-model trace.
A made-up label, missing subtype, or below-minimum trace fails; it cannot be
renamed to the other stratum. The composition's three families must each meet
their table's per-program primary minimum in addition to the 64-event rule.

The transition-only taxonomy is likewise closed for this proof. Every row has
exact state/retirement vectors at rates 1/1, 1/8, and 1/64 guest operations;
an opaque Go-only catalog ID may vary inside a row but may not create an
unmeasured ninth transport family. These IDs classify synthetic transport
vectors, not events out of compiled performance programs: a source-comparable
miss, growth, error, or other effect in a performance program remains in that
program's elapsed time and cannot be reclassified into this table.

| Canonical transition family ID | Required boundary vector |
| --- | --- |
| `allocation-growth` | stack/arena/table allocation and bounded growth request/resume |
| `descriptor-invalidation` | stale-version detection and structural invalidation before mutation |
| `host-reentry` | host callback request, nested synthetic re-entry, and result resume |
| `error-limit` | error, protected boundary, and instruction/depth/space limit |
| `coroutine` | yield, suspended state, resume values, and completion |
| `cancellation` | cancellation observed at a quantum boundary without partial mutation |
| `gc-close` | forced-GC rendezvous and owner-close rejection/liveness |
| `owner-boundary` | stale/mixed owner cookie and synthetic root/lease transition |

Eligibility and classification come from pre-candidate semantic metadata.
Every dynamic event is classified exactly once. Candidate support cannot
change a denominator: an unsupported operation that was frozen as eligible
remains an eligible unsupported exit and counts against coverage.

### Work bounds and Go latency

- A burst retires at most 64 guest instructions and performs at most 256
  primitive work units.
- One slot copy, probe, chain step, close, or clear costs one primitive unit.
- One operation performs at most 16 hash probes, eight property-chain steps,
  and 64 copied/cleared slots before a precise exit or reviewed resumable
  phase.
- A guest instruction is charged exactly once after its semantic mutation is
  complete. AUX words are not independently charged.
- On the pinned quiet M1, uncontended burst p99 is <=50 microseconds and
  observed maximum <=250 microseconds.
- Heartbeat, cancellation, and forced-GC rendezvous p99 is <=2 milliseconds
  and observed maximum <=10 milliseconds.

### Allocations and static size

- The Go wrapper and assembly burst allocate zero after preparation.
- No allocation recurs per guest instruction, internal script call, stable
  table/property hit, iteration step, quantum return, or effect/resume cycle.
- Warm proof execution may not increase B/op or allocs/op over the frozen
  current-Ember class-program ceiling.
- Preparation allocations and retained bytes are reported separately; the
  proof does not claim the later public/cold all-37 allocation gate.
- Complete proof backend hot text plus dispatch rodata is <=256 KiB in the
  linked test binary.

## Work classification and agent strategy

- scope: cross-cutting isolated runtime prototype and measurement tooling;
- uncertainty: very high because success requires multi-x gains across calls
  and data access, not a local dispatch win;
- coupling: compiler wordcode, value representation, calls, tables,
  descriptors, effects, Go GC liveness, arm64 ABI, and measurement;
- risk: very high for shared semantic bugs, unsafe pointer retention,
  scheduler delay, and misleading synthetic benchmarks.

| Work | Assigned agent | Reason |
| --- | --- | --- |
| Target contract and proof acquisition helper | Luna high | Bounded deterministic tooling work |
| Class grammar, held-out epoch, and independent state oracle | Luna max | Generality and semantic-oracle risk |
| Image/layout/Go kernel | Luna max | Cross-cutting performance-critical representation |
| arm64 ABI, dispatch, handlers, and disassembly | Luna max | Native correctness and scheduler risk |
| Freeze and final proceed/kill review | Sol high | Independent architecture and evidence challenge |

One owner controls the shared image/ABI spec. After that spec freezes, the Go
kernel and arm64 leaf may proceed in parallel only if they edit disjoint files.
No two agents may concurrently change `bytecode.go`, `slot.go`,
`vm_dispatch_spec.go`, the generated layout files, or the ABI declaration.

## Dependency graph

```text
P0 target + class/timer baseline
              |
              v
P1 exact image/ABI + independent Go kernel
              |
              +--------------------+
              |                    |
              v                    v
       Go semantic proof     P2 arm64 safety proof
              |                    |
              +----------+---------+
                         v
            P3 end-to-end held-out gate
                         |
             +-----------+-----------+
             |                       |
          STOP                    PROCEED
  delete dead prototype      hand frozen artifacts to
  record rejection           production integration plan
```

## Phase P0: Freeze the proof contract and honest baseline

### Slice P0.1 - Reconcile source and install a proof-scoped no-foreign gate

Assigned agent: primary orchestrator, reviewed by Sol high.

Objective and reason: bind the decision to explicit baseline and candidate
source surfaces plus one target fingerprint, and make zero-CGO/zero-FFI an
executable proof property before native code exists.

Files and components:

- current worktree and user-owned files listed above;
- new `docs/adr/0007-runtime-speed-no-cgo.md`, initially `Proposed`;
- `performance-audit.md`;
- existing `scripts/check-purego` as the default product gate;
- new proof-scoped standard-library shell/helper fixtures, preferably
  `scripts/check-runtime-architecture-proof-purego`;
- future `asmprobe` Go and `.s` files.

Ordered work:

1. Record `git status --short --branch`, HEAD, Go environment, target, Luau
   binary/version/hash, and dirty diff hash. Preserve every unrelated file. No
   decision capture may use the dirty tree.
2. Define two fail-closed source manifests and hashes:
   - the **baseline surface** enumerates the current compiler, Proto verifier/
     wordcode, current VM/runtime path, class sources/wrappers, timer/statistic
     helper, manifest/generator version, and Luau contract used to obtain
     current-Ember/Luau ratios;
   - the **candidate surface** recursively covers every proof lowerer, image,
     ABI, generated file, Go kernel, assembly source/object, effect model,
     acquisition helper, and relevant shared metadata.
   A path is excluded from the baseline surface only when a structural test
   proves it is proof-tagged and unreachable by the current-engine runner. Any
   edit to shared compiler/Proto/current-VM metadata changes the baseline hash.
   A proof-only addition may change the candidate hash without invalidating an
   otherwise identical baseline hash.
3. Add ADR 0007 with the decision question, strict no-CGO/no-FFI/JIT/helper
   boundary, generality contract, proof thresholds, and explicit distinction
   between architecture proof and final product acceptance.
4. Add a proof-scoped scanner that fails on foreign source/object/archive
   inputs, `import "C"`, cgo/linker/wasm import directives, private or foreign
   `go:linkname`, dynamic loader/executable-memory APIs, helper-process backend
   APIs, and foreign branch/call targets in Ember-owned assembly. Test and
   measurement files may invoke the pinned Luau oracle; proof backend files may
   not import `os/exec` or subprocess APIs.
5. Add a tagged linked-binary inspection that records Ember proof symbols,
   dynamic imports, W^X segments, and all branch/call targets. Scope foreign
   symbol rejection to Ember-owned objects because the Go runtime legitimately
   imports Darwin system symbols.
6. Add fail/pass fixtures for each prohibited mechanism. Do not weaken a
   failing pattern with broad path exclusions.
7. Keep `scripts/check-purego` behavior intact for ordinary repository checks
   unless the new proof scanner can be safely composed without rejecting the
   existing Luau measurement tests.

Verification:

```sh
scripts/check-purego
scripts/check-runtime-architecture-proof-purego --self-test
CGO_ENABLED=0 go test -tags=asmprobe -run '^TestProofNoForeignRuntime' .
git diff --check
```

Completion criteria:

- target/source identity is explicit and clean-capture rules fail closed;
- baseline and candidate surfaces are separately enumerated and fixture-tested;
- the proof cannot compile or link through CGO, FFI, JIT pages, helper
  execution, or a foreign assembly call;
- ADR 0007 remains proposed until the final proof decision.

Risks and dependencies:

- implementation may proceed around unrelated dirty files, but baseline and
  decision acquisition wait for a clean source state;
- the scanner must distinguish the external Luau measurement oracle from a
  forbidden Ember execution backend.

### Slice P0.2 - Build the class manifest, generator, and proof timer

Assigned agent: Luna max, measurement helper reviewed by Luna high.

Objective and reason: preregister broad semantic work and one timer domain so
the backend cannot be tuned to named acceptance programs or credited with
process/timer artifacts.

Files and symbols:

- `execution_differential_test.go`: existing corpus helpers and opcode checks;
- `bytecode.go:opcodeMetadataTable` and existing operand/effect metadata;
- new test-only proof grammar and independent expected-state helpers;
- new `testdata/runtime-proof/class-manifest-v1.tsv`;
- new `testdata/runtime-proof/schedule-v1.tsv` after clean calibration;
- new proof acquisition helper, preferably
  `scripts/runtime-architecture-proof` plus a standard-library Go parser/gate;
- no changes to legacy `--phase full` semantics.

Ordered work:

1. Define the disjoint performance-required and semantic/transition-only sets
   from semantic metadata, never benchmark inventory. The performance set is
   exactly the hot families named in the frozen speed gate, and its only legal
   shape strata and subtype minima are the fixed taxonomy table above. The
   transition set covers precise allocation/growth, host, error, coroutine,
   limit, cancellation, GC/close, and owner-boundary exits that lack
   production-equivalent Luau work.
2. Build an ordered manifest with at least two independently shaped compiled
   source programs in every `(performance-required family, shape stratum)` and
   at least two strata per family, plus one cross-family composition program.
   Each transition-only family instead has exact state
   vectors at the three frozen transition rates. Every entry stores gate kind,
   family, predeclared shape stratum from the fixed taxonomy, source/state
   hash, expected result/state hash, the frozen numeric work/effect-share
   fields above, the table's exact dynamic event minima, and permitted
   mandatory effects. Every hot family has exactly the two required strata,
   and the composition meets its three-family/64-event plus primary-stratum
   minima. No entry may use or be derived from a Top10, Classic, or Scenario
   name/source/hash.
3. Extend the existing differential grammar with bounded recursion, loop,
   object, collision, property-chain, call-arity, and mutation controls. It
   emits normal source through `Compile` and verified state vectors where an
   effect boundary cannot be expressed externally.
4. Keep visible development seeds. Define one proof epoch of exactly 5,000
   accepted class-balanced source/state cases, but do not derive its seed until
   the mechanism in P2.2 is frozen. The manifest's ordered generator cells are
   every `(performance family, required stratum)` and every
   `(transition-only family, frozen rate)`. Give each cell
   `floor(5000/cellCount)` accepted cases, then give one extra case to each of
   the first `5000 mod cellCount` cells in manifest order; a rejection retries
   the same cell and never transfers its quota. Record generator version, seed
   commitment, rejection counts, per-cell/per-family counts, and corpus hash.
   Before execution, the grammar assigns each accepted case an intended gate
   kind and, for performance cases, one legal shape stratum; dynamic traces may
   fail that assignment but may not reclassify it. Revealed cases become
   ordinary regressions and can never be reused as held-out evidence.
5. Implement a small independent expected-state/effect model for synthetic
   handles, flat calls, table/property operations, exits, and resume. It may
   share numeric/tag constants but not production lowering, dispatch, table,
   descriptor, or effect helpers.
6. Create a dedicated proof timer with one frozen **cold point lifecycle** for
   every performance engine. Before the timer, only sealed source bytes, N,
   expected externally observable digest, and output buffers may exist. Inside
   one external Go monotonic boundary:
   - current Ember performs `Compile`, current state preparation, N-body
     execution, canonical result serialization, the common parent digest
     parse/validation, and release;
   - proof Go/assembly performs the same `Compile`, proof lowering/image and
     state preparation, N-body execution including wrapper/burst/effect/resume,
     the same canonical result serialization and parent digest validation, and
     proof-owner cleanup;
   - Luau starts the pinned child, compiles and executes the same source/N,
     emits only the same canonical digest, exits, and passes through the same
     parent parse/validation before stop.
   After the timer, only raw-artifact serialization is allowed. Full internal
   state differential remains a separate correctness gate because Luau cannot
   expose the synthetic state. Bind the lifecycle/event-table and digest
   implementation hash to every row. Never compare a prebuilt proof image with
   a cold current/Luau point. Luau startup remains an intercept and the frozen
   Nmin/Nmax body-share floors prevent it from dominating. Do not use Luau
   `os.clock()`.
7. Before timing, trace every visible performance program at N=1 through the
   current engine and independent model. Reject rather than relabel any entry
   that misses its work, shape, or effect-share fields. Freeze eligibility and
   exact denominators now; candidate results cannot alter them.
8. Calibrate four strictly increasing geometric N points per performance
   program, identical for all engines, with `Nmax/Nmin >=16`. Each timed point must be
   1-250 ms and span at least eight timer ticks. Current Ember and Luau must
   each have fitted body share >=50% at Nmin and >=90% at Nmax and avoid a
   one-time capacity/GC transition. Freeze the schedule before candidate
   execution; candidates may never recalibrate it. Both proof candidate
   captures must independently satisfy the same point range and body-share
   floors on this unchanged schedule.
9. Acquire nine independent paired fits with balanced engine order. Fit
   `T=entry+N*inner` by OLS over exactly four points. Require positive slope,
   nonnegative entry, R-squared >=0.995, max absolute residual <=5% of fitted
   elapsed, positive adjacent slopes within 10% of the fit, two-sided 95%
   Student-t slope-CI half-width <=5%, and per-engine cross-fit spread <=0.20.
   With `n=4`, `df=2`, `Sxx=sum((N-Nbar)^2)`, and
   `s=sqrt(SSE/(n-2))`, compute `SE(slope)=s/sqrt(Sxx)` and
   `SE(entry)=s*sqrt(1/n + Nbar^2/Sxx)`. Use the fixed
   `t(0.975,2)=4.302652729911275` for both CIs; the entry upper endpoint is
   `entry+t*SE(entry)`. All inputs, standard errors, endpoints, and fitted
   values must be finite, and the upper entry endpoint must be nonnegative.
10. After clean calibration and independent review, atomically freeze the raw
    `/tmp` output into `testdata/runtime-proof/schedule-v1.tsv` with its
    calibration artifact hash, baseline-surface hash, manifest hash, cold-point
    lifecycle/digest hash, and timer contract. P0.3 cannot consume an
    unreviewed copy or a path with merely the same rows.
11. Every acquisition names a fresh replicate ID and writes a non-reusable
   content-addressed artifact. Bind the baseline-surface hash, candidate hash
   when applicable, class manifest, generator, schedule, timer helper, target,
   toolchain, Luau, lifecycle/digest, thresholds, and clean status. Invalid or
   contaminated acquisition fails completely. Retain every raw elapsed value
   keyed by program, engine, fit index, engine order, N point, start/stop event,
   and lifecycle hash; slopes, entries, ratios, and point-total `P_i(N)` are
   reproducibly derived views, never the only evidence.
12. Add tests for missing or overlapping performance/transition families,
    unknown/duplicate/mislabeled strata, any `(family,stratum)` count below
    two, every exact per-program and cross-program subtype-minimum boundary in
    the taxonomy table, zero events, incorrect 5,000-case cell apportionment,
    duplicate/hash-derived acceptance content, changed schedule, candidate
    recalibration, timer event mismatch, nonlinear fits, slope/intercept CI and
    `I_cap` boundaries, residual/spread boundaries, result mismatch, artifact
    reuse, and contamination.

Verification:

```sh
CGO_ENABLED=0 go test -run '^(TestArchitectureProofManifest|TestArchitectureProofGenerator|TestArchitectureProofFit|TestArchitectureProofTimer)$' .
scripts/runtime-architecture-proof --self-test
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/runtime-architecture-proof calibrate \
  --schedule-out /tmp/ember-proof-schedule.tsv --replicate calibration-a
scripts/runtime-architecture-proof freeze-schedule \
  --input /tmp/ember-proof-schedule.tsv \
  --output testdata/runtime-proof/schedule-v1.tsv
```

Completion criteria:

- every performance-required `(family,stratum)` has at least two compiled
  programs, every program and stratum proves the fixed taxonomy's exact event
  minima, and every transition-only family has exact vectors/rates with
  explicit event minima;
- current Ember and pinned Luau agree on every externally comparable program
  before a candidate is measured;
- one timer/lifecycle/digest/schedule/statistic contract is immutable and
  fixture-tested;
- held-out inputs remain unknown until the mechanism freeze.

Risks and dependencies:

- proof timing is class evidence, not the final all-37 acceptance harness;
- a class manifest that contains only monomorphic hits is invalid even if fast;
- process startup remains an intercept; slope fitting does not authorize
  excluding different lifecycle work from different engines.

### Slice P0.3 - Capture the clean current/Luau class baseline

Assigned agent: primary orchestrator, independently reviewed by Sol high.

Objective and reason: replace exploratory mixed-workload ratios with exact
same-program gain requirements before the proof implementation is timed.

Files and evidence:

- frozen class manifest and schedule from P0.2;
- proof acquisition raw artifacts;
- `performance-audit.md` proof appendix;
- ADR 0007 baseline hashes.

Ordered work:

1. On the pinned quiet machine and clean source, take two independent current
   Ember/Luau captures for every performance-required program using distinct
   replicate IDs. Record the exact baseline-surface hash.
2. Reacquire a whole capture on contamination, fit failure, result mismatch,
   environment drift, source drift, or schedule mismatch.
3. For every program, store every per-fit/per-engine/per-N raw elapsed value and
   lifecycle/digest hash, nine paired slopes, `B_i=current_i/Luau_i`, fitted
   entries and slope/intercept confidence intervals, residuals, spread,
   source/result hashes, and dynamic class counts from current Ember.
4. Compute `M_B`, `Q_B`, and the deterministic conservative `U_B`/`U_Q` from
   the frozen speed-gate formula for each capture. Freeze the larger of the two
   capture `U_B` values and the larger of the two `U_Q` values, then derive the
   required candidate/current gates as `1.75/U_B` for median and `1.90/U_Q` for
   p90. A nonpositive CI endpoint, noisy baseline, or invalid upper ratio fails
   instead of relaxing the gate. Freeze `I_cap` from the larger baseline
   current/Luau fitted-entry upper endpoint and retain every N-point elapsed
   value for the later candidate total-time gate.
5. Freeze one machine-readable gate containing each entry's gate kind, program
   or state vector, family, required gain when performance-required,
   `I_cap`, body-share/point-total thresholds, eligible-exit ceiling (5% or
   2%), transition-rate/cycle ceiling when transition-only, and event minima.
   No family may be renamed, merged, removed, or reclassified after proof
   results exist.
6. Record allocation ceilings for warmed class-program execution only. Keep
   cold/public all-37 allocation work explicitly deferred.

Verification:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/runtime-architecture-proof baseline \
  --schedule testdata/runtime-proof/schedule-v1.tsv --replicate baseline-a
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/runtime-architecture-proof baseline \
  --schedule testdata/runtime-proof/schedule-v1.tsv --replicate baseline-b
scripts/runtime-architecture-proof freeze-baseline \
  --before baseline-a --after baseline-b \
  --output testdata/runtime-proof/family-gates-v1.tsv
```

Completion criteria:

- every performance program has a clean, same-source current/Luau ratio and
  required proof gain, and every transition family has a frozen transport gate;
- thresholds and event denominators are frozen before backend timing;
- no acceptance benchmark identity is part of the proof manifest.

Risks and dependencies:

- this slice cannot run as acceptance while the overlapping user edits remain
  unresolved;
- two captures from one retained raw acquisition do not count as independent.

## Phase P1: Build the exact representation and independent Go proof engine

### Slice P1.1 - Freeze execution-image, state, and burst ABI layouts

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: define the exact backend-visible representation that a
passing proof hands unchanged to production integration.

Files and symbols:

- `bytecode.go`: opcode/effect/operand metadata and verified `Proto.words`;
- `slot.go`: existing 64-bit tags and owner-relative handle encoding;
- `vm_dispatch_spec.go` and `cmd/ember-vmgen/main.go`;
- new private `execution_image_spec.go` and `execution_burst_abi.go`, with no
  production callers;
- new generated Go/arm64 layout, enum, cost, and assertion files;
- tagged proof-only state adapters and tests.

Ordered work:

1. Lower only from normal verified `Proto` wordcode. Reject hand-authored or
   unverified proof words. Preserve exact guest-PC, AUX-word, source-line,
   operand, and instruction-charge mapping.
2. Define fixed-width semantic words, initially <=16 bytes each, with dense
   operation class, operands, descriptor index, guest cost, effect class, and
   maximum primitive work. No workload-shaped fusion is allowed.
3. Define pointer-free records for Proto metadata, flat continuations,
   closures/upvalues, globals, table directory/cells/order, property guards,
   stable iteration, effect state, and `burstExit`.
4. Define `burstRequest` as a closed generated list of typed base pointers plus
   lengths/capacities and scalar owner/image cookies. Assembly may read but not
   write or retain those pointer fields. Every reached mutable record and the
   complete exit remain pointer-free.
5. Define one flat value stack and one compact continuation stack for
   direct/fixed/open/tail/recursive calls, varargs, and multi-results. Use
   indices and offsets, never hidden pointers or `uintptr` roots.
6. Define bounded packed global/table/property/iteration state sufficient for
   stable read/write hits, bounded probes, finite guarded chains, scripted
   target entry, and deterministic iteration. Growth, rehash, allocation, and
   structural invalidation become precise pre-mutation effects.
7. Freeze operation budgets: <=64 retired guest instructions and <=256
   primitive work units per burst; <=16 probes, <=8 property-chain steps, and
   <=64 copied/cleared slots per guest operation.
8. Freeze a small closed transport reason set such as
   `{completed, quantum, effect, fault}` and every resume phase. The generic
   `effect` exit carries an opaque fixed-width `effectKind`, fixed scalar
   operands/result locations, and `{phase,instructionCharged}`. Assembly may
   copy an effect kind from verified image metadata but never branches on the
   Go-only production catalog. An instruction is retired once after complete
   mutation. A pre-effect exit is unretired; the Go-completed effect marks it
   retired once and exposes no host-visible partial result.
9. Generate operation, transport-reason, cost, layout, semantic-family,
   eligibility, owner-cookie, and liveness metadata from one standard-library
   core spec consumed by Go and assembly. Generate the extensible Go-only
   effect-kind catalog separately. A catalog extension is backend-invisible
   only if structural/disassembly tests prove no assembly reference to its
   enumerated IDs and a generic exit/resume vector passes unchanged.
10. Add reflection/unsafe tests proving every assembly-mutable record contains
    no pointer, map, slice, string, interface, function, or hidden pointer-like
    `uintptr`; separately verify the request against its exact typed-pointer
    allowlist.
11. Add a verifier for opcode, operand/register, block target, descriptor,
    continuation/table bounds, PC map, work maxima, and owner/image cookies.
    Malformed images fail before a backend call.
12. Record the core ABI/layout hash and separate Go effect-catalog hash in ADR
    0007. Any backend-visible layout, transport, semantic-cost, or handler
    change invalidates the performance decision and reruns P1-P3. Adding an
    opaque Go-only effect kind does not, unless it changes a handler, transport
    field, retirement rule, phase, or measured hot-program transition rate.

Verification:

```sh
go generate ./...
go run ./cmd/ember-vmgen -check
CGO_ENABLED=0 go test -tags=asmprobe -run '^(TestProofExecutionImage|TestProofBurstABI|TestProofGeneratedLayouts|TestProofPointerFreeState)$' .
go vet ./...
git diff --check
```

Completion criteria:

- one versioned layout covers the Go kernel and assembly leaf;
- all hot proof state is scalar and pointer-free behind a typed request edge;
- all work is bounded or has a precise effect exit;
- no production call path reaches the proof.

Risks and dependencies:

- an adapter that skips intended loads, guards, ownership cookies, side exits,
  or resumes cannot authorize production;
- persistent raw pointers or integer-hidden Go pointers are an immediate kill.

### Slice P1.2 - Implement an independently structured Go proof kernel

Assigned agent: Luna max.

Objective and reason: establish a readable semantic implementation over the
new ABI that does not share the current generated direct loop or the assembly
handler bodies.

Files and symbols:

- new proof-only lowering and Go kernel files behind `asmprobe` or test build
  constraints;
- generated operation metadata from P1.1;
- `execution_differential_test.go` corpus and comparison helpers;
- current `runGeneratedDirectFrameProductionLoop` only as a baseline engine;
- new proof effect/state model and transition tests.

Ordered work:

1. Implement source -> `Compile` -> verified Proto -> proof image through the
   frozen lowerer. Reuse immutable opcode metadata, but do not reuse current VM
   dispatch bodies.
2. Implement scalar/control, branches/loops, direct calls/returns/tail calls,
   fixed/open/vararg/multi-result moves, recursion, closure invocation,
   upvalue reads/writes, globals, bounded table/property hits, scripted target
   entry, and stable iteration.
3. Implement one closed proof effect executor for allocation/growth models,
   descriptor misses, host/error/coroutine model exits, quantum return, and
   resume. It mutates only the synthetic proof owner and returns exact events.
4. Apply guest and primitive budgets identically to the future assembly path.
   Test every exit and every possible quantum cut around calls, mutation, and
   effects.
5. Compare current Ember and Go proof execution through independently written
   observation code: values, errors, globals, event order, PCs, counts, and
   complete mutable proof state.
6. Compare the Go proof kernel to the independent expected-state model over
   millions of randomized operation/state transitions. Shared generated enums
   are permitted; shared semantic helpers are not.
7. Add exact tests for NaN/tag bits, typed nils, stale/mixed owner cookies,
   stack/continuation overflow, probe/chain bounds, vararg/result adjustment,
   upvalue close, table collisions, iteration mutation exits, and every effect
   resume phase.
8. Prove zero recurring warmed allocations for the Go request/exit/effect
   cycle. Report its speed without making it an arm64 retention gate.
9. Using only the Go kernel, freeze the four-point batch schedule for every
   transition-only family/rate under the 1-100 ms, 16x span, >=90% Nmax body
   share, and OLS fit-quality rules. Hash the schedule, Go effect body, timer,
   lifecycle, family/rate inventory, and candidate surface before any assembly
   transport timing.

Verification:

```sh
CGO_ENABLED=0 go test -tags=asmprobe -run '^(TestProofGoKernel|TestProofLowering|TestProofGoStateDifferential|TestProofEffects)$' -count=1 .
CGO_ENABLED=0 GOGC=1 go test -tags=asmprobe -run '^TestProofGoKernelGC$' -count=20 .
CGO_ENABLED=0 go test -race -tags=asmprobe -run '^(TestProofGoKernel|TestProofGoStateDifferential)$' .
CGO_ENABLED=0 go test -tags=asmprobe -gcflags=all=-d=checkptr=2 -run '^(TestProofGoKernel|TestProofGoStateDifferential)$' .
scripts/runtime-architecture-proof freeze-transition-schedule \
  --engine go --output testdata/runtime-proof/transition-schedule-v1.tsv
```

Completion criteria:

- all preregistered families execute end to end through the new image;
- the Go kernel is independent of current VM and assembly semantic bodies;
- state/effect/retirement differential is exact;
- warmed execution performs no recurring allocation.
- the Go-only transition batch schedule is frozen before assembly timing.

Risks and dependencies:

- current direct versus instrumented VM loops are not independent oracles
  because they share generated source; use them only as the current baseline;
- a synthetic effect model may not claim public Runtime semantics.

## Phase P2: Prove the static Darwin/arm64 mechanism

### Slice P2.1 - Prove ABI entry, dispatch, liveness, and bounded return

Assigned agent: Luna max, reviewed by Sol high.

Objective and reason: demonstrate that the exact request/state contract can be
entered safely from Go and return frequently enough for GC and scheduling.

Files and components:

- `asmprobe && darwin && arm64` Go wrapper/declaration;
- generated arm64 layout include;
- new leaf `.s` burst function and proof-only fallback stub for other targets;
- disassembly, liveness, canary, and latency tests;
- no production routing.

Ordered work:

1. Declare a stable ABI0 assembly function and let the Go compiler provide the
   ABIInternal wrapper. Do not use `go:linkname` or private runtime symbols.
2. First prove by source review and disassembly that the wrapper/leaf neither
   stores nor returns a request-derived pointer, that all bases remain live,
   and that neither path allocates. Only then apply `//go:noescape`; afterward
   verify escape output and allocation behavior. Apply `NOSPLIT` only after
   bounded frame/no-call disassembly proves it valid.
3. Preserve Go/Darwin arm64 rules: R18 reserved, R28 holds `g`, SP 16-byte
   aligned, correct R29 frame convention and R30/LR return, FPCR unchanged, no
   VM state retained in linker/assembler scratch registers R16/R17/R27 across
   expansions, no assumed C callee-saved set, no Go/foreign call, and no local
   pointer frame.
4. Implement generated bounds-checked dispatch using an in-text branch table
   or a measured generated branch tree. Inspect linked object behavior; do not
   depend on undocumented code-address data.
5. Implement request/image/owner cookie checks, exact quantum counters, exit
   writes, and canary/red-zone validation before semantic handlers.
6. In the Go wrapper, acquire exclusive proof-owner activity, call the leaf,
   then `runtime.KeepAlive` the request, exit, owner, image, and each backing
   array owner before releasing activity. Test close/replacement rejection
   while a burst is active.
7. Exercise at least one million warmed bursts spanning every operation and
   declared worst-case probe/copy/chain bound. Enforce the frozen p99/max burst
   and heartbeat/cancellation/forced-GC rendezvous limits.
8. Build with `CGO_ENABLED=0`, `GOGC=1`, async preemption, race, and checkptr.
   Race/checkptr cannot instrument assembly, so combine them with ownership,
   canaries, randomized differential, and disassembly.
9. Use `go tool objdump`, `go tool nm -size`, and `otool -l` to prove expected
   dispatch, no Go/foreign call, no accidental pointer frame, <=256 KiB proof
   text+dispatch rodata, ordinary RX text, and no writable-executable segment.

Verification:

```sh
CGO_ENABLED=0 go test -tags=asmprobe -run '^TestAssemblyProofABI' -count=100 .
CGO_ENABLED=0 GOGC=1 GOMAXPROCS=1 go test -tags=asmprobe -run '^TestAssemblyProofGCAndHeartbeat$' -count=20 .
CGO_ENABLED=0 go test -race -tags=asmprobe -run '^TestAssemblyProofABI' .
CGO_ENABLED=0 go test -tags=asmprobe -gcflags=all=-d=checkptr=2 -run '^TestAssemblyProofABI' .
CGO_ENABLED=0 go test -tags=asmprobe -gcflags='all=-m=2' -run '^$' . 2>/tmp/ember-proof-escape.txt
CGO_ENABLED=0 go test -tags=asmprobe -c -o /tmp/ember-proof.test .
go tool objdump -s '.*runAssemblyProofBurst.*' /tmp/ember-proof.test
go tool nm -size /tmp/ember-proof.test
otool -l /tmp/ember-proof.test
scripts/check-runtime-architecture-proof-purego
```

Completion criteria:

- the cgo-disabled linked binary enters and returns through the public Go ABI;
- no pointer is hidden, retained, written by assembly, or passed to a foreign
  call;
- every burst and operation is bounded within the latency contract;
- object code matches the reviewed dispatch and frame design.

Risks and dependencies:

- Go assembly is not an async safe point; a latency failure is a kill, not a
  later optimization task;
- `//go:noescape` is an assertion after proof, never a way to manufacture it.

### Slice P2.2 - Implement the complete proof semantic island in assembly

Assigned agent: Luna max.

Objective and reason: demonstrate multi-family native coverage on the same
representation, rather than isolated dispatch throughput.

Files and components:

- generated handler inventory and class counters;
- arm64 proof leaf and handler blocks;
- Go proof kernel from P1.2;
- randomized transition and compiled-source differential tests;
- class coverage/exit reports.

Ordered work:

1. Implement scalar moves/constants/truth, numeric arithmetic/comparison,
   branches, numeric loops, and exact retirement.
2. Implement flat direct/fixed/open/tail/recursive call entry, continuation,
   return, vararg, and multi-result mechanics.
3. Implement already-created closure invocation and closed/open upvalue
   reads/writes/close within bounded work.
4. Implement guarded global hits, bounded array/hash reads and existing-cell
   writes, finite property-chain hits, and stable scripted target entry.
5. Implement deterministic stable array/generic iteration and bounded pure
   intrinsics only when their slot semantics fit the same general metadata.
6. Implement precise pre-mutation exits for growth, allocation, structural
   invalidation, unsupported numeric/string work, host/error/coroutine model
   boundaries, and unknown calls. Resume through the shared proof effect model.
7. Keep mandatory effects and eligible misses in separate generated counters.
   Emit every declared class including zero; zero required coverage fails.
8. Run millions of generated Go-versus-assembly transitions. Compare every
   field, including state that is not visible in the final source result.
9. Run every visible class program through current Ember, Go proof, assembly,
   and Luau for exact results before freezing the mechanism.
10. Freeze handler source, generated core metadata, ABI/layout hash, Go-only
    effect catalog, class taxonomy, and coverage rules. Record the candidate
    mechanism commitment and candidate-surface hash.
11. Compare the current baseline-surface hash with P0.3. If it changed, rerun
    P0.3 current-Ember/Luau acquisition and freeze replacement gates before any
    candidate timing. Sol must record that no proof assembly timing was
    inspected between the surface change and refreeze. Only then derive the
    P0.2 held-out epoch.

Verification:

```sh
CGO_ENABLED=0 go test -tags=asmprobe -run '^(TestAssemblyProofScalar|TestAssemblyProofCalls|TestAssemblyProofTables|TestAssemblyProofEffects|TestAssemblyProofStateDifferential)$' -count=1 .
CGO_ENABLED=0 GOGC=1 go test -tags=asmprobe -run '^TestAssemblyProofStateDifferential$' -count=20 .
CGO_ENABLED=0 go test -race -tags=asmprobe -run '^TestArchitectureProofGoOracle$' .
CGO_ENABLED=0 go test -tags=asmprobe -gcflags=all=-d=checkptr=2 -run '^TestArchitectureProofGoOracle$' .
```

Completion criteria:

- the complete required island runs through assembly and the real side-exit
  path;
- all transitions are exact against the independent Go kernel/model;
- candidate mechanism, candidate hash, and matching baseline-surface gate are
  frozen before held-out inputs are known;
- no source/corpus identity enters dispatch or guards.

Risks and dependencies:

- an effect-heavy adapter can hide poor native coverage; enforce denominators
  before timing;
- pow/transcendentals, long strings, growth, callbacks, and coroutine
  transitions remain honest effects unless a separately bounded general
  implementation exists.

## Phase P3: Run the proof and make the irreversible decision

### Slice P3.1 - Reveal the held-out epoch and run the full architecture gate

Assigned agent: primary orchestrator, independently reviewed by Sol high.

Objective and reason: decide from unseen general programs, exact state, actual
end-to-end timing, native coverage, allocation, latency, and linked code.

Files and evidence:

- frozen mechanism/layout/effect/generator commitments;
- exact 5,000-case proof epoch;
- frozen class schedule and family gates;
- sealed held-out corpus, supplemental schedule, and supplemental gates;
- two fresh candidate acquisition artifacts;
- semantic, coverage, allocation, latency, code-size, W^X, disassembly, and
  no-foreign reports.

Ordered work:

1. Verify the clean source; immutable baseline-surface hash and its current/
   Luau gate; candidate-surface/mechanism hash; generator commitment; base
   schedule/gate; toolchain; target; Luau; and no-CGO fingerprints. A
   baseline-surface change requires the P2.2 pre-timing P0.3 reacquisition. A
   candidate-surface change invalidates the mechanism freeze and held-out
   epoch. Never hide either behind one recursive source hash.
2. Derive exactly 5,000 accepted held-out cases after deterministic rejection.
   Seal source/state inputs and family counts before executing them.
3. Before executing the epoch, use each case's sealed generator-assigned gate
   kind/shape stratum and select the first two intended-performance cases in
   corpus-hash order for every predeclared stratum of every
   performance-required family. Seal that selection before dynamic tracing or
   calibration.
4. Run all held-out cases through the independent model where applicable, the
   Go proof kernel, and assembly; run compiled external cases through current
   Ember and pinned Luau as well. Require exact agreement and complete event
   minima. A mismatch reveals the epoch and fails this decision; fixing it
   requires a newly committed epoch for any later retry. Apply the frozen
   eligibility/event rules to the exact preselected timing cases and take two
   current-Ember/Luau-only baseline acquisitions. Compute the conservative
   `M_B/Q_B/U_B/U_Q` gates, and freeze the supplemental schedule/gain gate
   before timing the unchanged candidate.
   A selected case that misses a work/event minimum, is effect-heavy, cannot
   calibrate, or fails fit quality fails the proof; it is never skipped or
   replaced. A stratum with fewer than two cases is a generator/proof failure.
   Write content-addressed
   `heldout-proof-v1.tsv`, `heldout-schedule-v1.tsv`, and
   `heldout-family-gates-v1.tsv`, then seal all three hashes and the generator/
   baseline-surface/candidate commitments plus lifecycle/digest contract in one
   `heldout-decision-v1.manifest` before timing.
5. Capture class coverage and side-exit counts for visible and held-out cases.
   Apply every frozen floor per program and per family. Missing or zero
   denominators fail.
6. Run every transition-only family at all three frozen rates. Require exact
   state/retirement, the 1.25x/100 ns transport ceilings, zero recurring
   wrapper/burst/effect allocations, and the broader latency gates. Also
   require no performance-program B/op or allocs/op increase over baseline.
7. Re-run burst/rendezvous latency, disassembly, text/rodata size, W^X,
   liveness, canary, and no-foreign gates on the exact candidate binary.
8. Take two independent proof candidate captures with fresh replicate IDs.
   Every preregistered performance program, generated composition, and selected
   held-out performance program must meet the exact `M_C`, `Q_C`, `M_G`, and
   `Q_G` slope gates, the `I_cap` and candidate body-share gates, and the
   per-N-point total-time gates. A candidate effect-share-cap violation fails;
   it cannot change gate kind. No family, shape stratum, row, statistic, or
   capture may be waived or aggregated away.
9. Run the static anti-overfit scan over proof Go, assembly, generated outputs,
   specs/templates, helper code, and linked strings. Self-test that candidate
   acquisition fails if any held-out corpus/schedule/gate/decision input is
   omitted, substituted, reused, or hash-mismatched.
10. Have Sol inspect raw data, fit diagnostics, mechanism freeze order,
   disassembly, ABI/liveness, semantics, coverage denominators, allocations,
   latency, and source scan. Summaries alone are insufficient.

Verification:

```sh
CGO_ENABLED=0 go test -tags=asmprobe -run '^(TestArchitectureProofHeldOut|TestArchitectureProofState|TestArchitectureProofCoverage|TestArchitectureProofAllocations)$' -count=1 .
CGO_ENABLED=0 GOGC=1 GOMAXPROCS=1 go test -tags=asmprobe -run '^TestArchitectureProofHeldOut$' -count=20 .
scripts/check-runtime-architecture-proof-purego
scripts/runtime-architecture-proof --self-test-heldout
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/runtime-architecture-proof candidate \
  --schedule testdata/runtime-proof/schedule-v1.tsv \
  --gates testdata/runtime-proof/family-gates-v1.tsv \
  --transition-schedule testdata/runtime-proof/transition-schedule-v1.tsv \
  --heldout-manifest testdata/runtime-proof/heldout-decision-v1.manifest \
  --heldout-corpus testdata/runtime-proof/heldout-proof-v1.tsv \
  --heldout-schedule testdata/runtime-proof/heldout-schedule-v1.tsv \
  --heldout-gates testdata/runtime-proof/heldout-family-gates-v1.tsv \
  --replicate proof-a
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/runtime-architecture-proof candidate \
  --schedule testdata/runtime-proof/schedule-v1.tsv \
  --gates testdata/runtime-proof/family-gates-v1.tsv \
  --transition-schedule testdata/runtime-proof/transition-schedule-v1.tsv \
  --heldout-manifest testdata/runtime-proof/heldout-decision-v1.manifest \
  --heldout-corpus testdata/runtime-proof/heldout-proof-v1.tsv \
  --heldout-schedule testdata/runtime-proof/heldout-schedule-v1.tsv \
  --heldout-gates testdata/runtime-proof/heldout-family-gates-v1.tsv \
  --replicate proof-b
CGO_ENABLED=0 go test -tags=asmprobe -c -o /tmp/ember-proof-decision.test .
go tool objdump -s '.*runAssemblyProofBurst.*' /tmp/ember-proof-decision.test
go tool nm -size /tmp/ember-proof-decision.test
otool -l /tmp/ember-proof-decision.test
scripts/check-fast
```

Completion criteria:

- all visible and held-out semantics are exact;
- every family meets its declared semantic/coverage/exit/allocation/latency/
  size/no-CGO gates, and transition-only families are never assigned a
  synthetic Luau ratio;
- both independent speed captures pass every visible and selected held-out
  class program;
- the retained evidence publishes per-program 1.057142857 median and
  1.052631579 p90 production integration-tax ceilings for every visible,
  composition, and selected held-out performance row;
- Sol records an explicit `PROCEED` or `STOP` against raw evidence.

Risks and dependencies:

- a candidate that passes visible programs but fails held-out programs stops;
- a synthetic inner-loop win without wrapper/effect/resume time stops;
- a single passing capture is insufficient.

### Slice P3.2 - Retain the handoff or delete the failed prototype

Assigned agent: primary orchestrator, reviewed by Sol high.

Objective and reason: leave no ambiguous alternate engine or duplicated future
work after the decision.

Files and components:

- ADR 0007 and `performance-audit.md`;
- proof raw artifact index and hashes;
- new `testdata/runtime-proof/proof-handoff-v1.manifest` on `PROCEED`;
- proof source/generated files;
- companion production plan.

Ordered work for `PROCEED`:

1. Mark ADR 0007 `Accepted for production integration`, not product-complete.
   Record exact baseline/candidate surfaces, core ABI/layout, Go effect catalog,
   class/generator/base-and-held-out schedules/gates, raw capture, linked
   binary, normalized leaf disassembly, and review hashes. The normalized leaf
   fingerprint strips addresses, symbol spelling, relocation offsets, and
   build-tag wrapper names while retaining ordered instructions, immediates,
   constants, branch structure, and generated semantic tables; proof and
   production linked/object hashes are expected to differ.
2. Create `testdata/runtime-proof/proof-handoff-v1.manifest` as the single
   content-addressed production handoff. It must enumerate and hash the
   baseline/candidate surfaces; visible class manifest, base schedule, and base
   gates; held-out decision, corpus, schedule, and gates; lifecycle/digest;
   both raw proof captures; `transition-schedule-v1.tsv` and both raw
   transition captures; linked proof binary and size/W^X reports; core ABI/
   catalog metadata; normalized leaf; and Sol decision. Its ordered row
   inventory names every visible, composition, and selected held-out
   performance program plus every transition-only vector. Missing, duplicate,
   extra, or aggregate-only rows fail sealing.
3. Retain the image word/layout, slot, continuation, packed table/global/
   property/iteration, request/exit, generic effect transport plus Go-only
   catalog boundary, independent Go kernel, normalized leaf fingerprint, and
   passing assembly handlers as one versioned proof artifact group.
4. Keep all execution entrypoints behind proof/test constraints. Do not route
   `Run`, `Runtime`, modules, callbacks, or coroutines through them in this
   plan.
5. Update the companion production plan to consume these exact artifacts. It
   may add owner/public edges and extend semantic coverage, but it must not
   reimplement or silently change the proven backend-visible layouts.
6. State the invalidation rule: any backend-visible layout, operation cost,
   retirement, side-exit transport, wrapper-liveness, quantum, or handler
   semantic change reruns the affected P1-P3 proof gates before production
   retention. An opaque Go-only effect-kind addition instead requires catalog
   validation, a generic exit/resume vector, and normalized handler-identity
   proof; if any of those changes core behavior, it escalates to P1-P3.

Ordered work for `STOP`:

1. Mark ADR 0007 rejected under the current constraints with the exact failing
   family, ratio, coverage, semantics, allocation, latency, ABI, or no-CGO
   evidence.
2. Delete production-intended proof engine and assembly artifacts in the same
   retention decision. Keep only generic measurement/differential tooling that
   remains independently useful and has no dormant runtime route.
3. Update the companion production plan status to blocked/rejected. Do not
   execute its runtime migration phases or replace failure with another list
   of 15-20% interpreter tweaks.

Verification for `PROCEED`:

```sh
scripts/runtime-architecture-proof seal-handoff \
  --class-manifest testdata/runtime-proof/class-manifest-v1.tsv \
  --schedule testdata/runtime-proof/schedule-v1.tsv \
  --gates testdata/runtime-proof/family-gates-v1.tsv \
  --heldout-manifest testdata/runtime-proof/heldout-decision-v1.manifest \
  --transition-schedule testdata/runtime-proof/transition-schedule-v1.tsv \
  --capture proof-a --capture proof-b \
  --output testdata/runtime-proof/proof-handoff-v1.manifest
scripts/runtime-architecture-proof verify-handoff \
  --manifest testdata/runtime-proof/proof-handoff-v1.manifest
```

Verification for `STOP`:

```sh
scripts/runtime-architecture-proof verify-stop \
  --decision testdata/runtime-proof/proof-decision-v1.manifest \
  --forbid-retained-engine
```

Verification for either decision:

```sh
go run ./cmd/ember-vmgen -check
scripts/check-purego
scripts/check-fast
scripts/check
git diff --check
git status --short --branch
```

Completion criteria:

- one unambiguous decision and complete verified handoff manifest exist;
- a passing core is frozen and ready for integration, or a failed core is
  removed;
- production routing and user-owned work remain untouched;
- the companion plan contains no duplicate implementation of proof-owned core
  mechanics.

## Handoff contract to the production plan

On `PROCEED`, the companion plan inherits rather than rebuilds:

| Proof-owned artifact | Production obligation |
| --- | --- |
| operation metadata and fixed-width image word | bind it to Program/Runtime ownership and complete unsupported semantics |
| slot and owner-relative handle bits | add real roots, pins, slabs, collection, public import/export, and lifetime policy |
| flat value/continuation records | add full protected-call, limit, error, callback, module, and coroutine behavior |
| packed table/global/property/iteration records | attach real public tables, growth/rehash/invalidation, metatable churn, and owner isolation |
| request/exit/generic-effect/quantum ABI | extend only the Go effect-kind catalog, integrate real effects, and preserve exact retirement/cancellation policy |
| independent Go kernel | complete it into the portable production engine and correctness oracle |
| passing arm64 handlers | promote them under normal platform constraints and extend only through the same generated spec |
| class generator and proof evidence | add fresh production-retention/final epochs, enforce the per-program integration-tax budget, and add the all-37 acceptance harness |

The production plan may version the ABI only with a new explicit proof. It may
not keep a proof layout beside a separately invented production layout; that
would discard the measured evidence and recreate the multi-engine maintenance
failure from ADR 0005.

## Commit and integration boundaries

Work directly on `main`. Preserve all unrelated user changes. After each
retained slice:

1. inspect `git status --short` and `git diff --name-only`;
2. stage only owned paths explicitly;
3. inspect `git diff --cached --check` and the complete cached diff;
4. commit one cohesive proof outcome;
5. push `origin/main` only after checks pass.

Recommended retained commits are:

1. proof target/no-foreign contract and ADR proposal;
2. class manifest/generator/timer and clean baseline gates;
3. frozen execution image/state/ABI generator;
4. independent Go proof kernel and differential;
5. arm64 ABI/dispatch/safety proof;
6. complete semantic island;
7. held-out performance decision and retain/delete cleanup.

Do not commit a failed dormant backend merely to preserve work. Raw evidence
and a rejection record are more valuable than unreachable alternate runtime
code.

## Explicit stop conditions

Return `STOP` if any remains after one complete general attempt:

- required call/continuation, table/property, or iteration families cannot
  execute end to end through the intended layouts;
- any required class misses <=1.75 median, <=1.90 p90, or its baseline-derived
  gain in either independent capture;
- the architecture needs source/corpus recognition to meet coverage or speed;
- assembly cannot use the public Go ABI without private runtime hooks, foreign
  calls, hidden pointers, or unsafe retained request bases;
- any operation is unbounded or the leaf violates burst/rendezvous latency;
- Go/assembly/model/current/Luau differential finds an unresolved mismatch;
- native coverage or eligible-exit floors fail;
- warmed allocations increase or recur in a hot operation;
- linked proof text/rodata exceeds 256 KiB or produces a writable-executable
  segment;
- success requires CGO, C/C++, dynamic FFI, upstream embedding, runtime JIT,
  helper processes, or a new dependency without approval.

After an initial `PROCEED`, a later production path that exceeds the frozen
1.057142857 median or 1.052631579 p90 integration-tax ceiling revokes the
handoff authorization and makes the companion production plan return `STOP`.
It does not rewrite the historical proof result.

## Definition of done

This proof plan is done only when:

- the target, separate baseline/candidate surfaces, class/stratum manifest,
  lifecycle/digest, schedules, baselines, mechanism, and held-out epoch are
  frozen in the required order;
- one independently structured Go kernel and one bounded static arm64 leaf use
  the same verified image/ABI;
- all required compiled-source and state families are exact and broad;
- both clean proof captures pass every speed and coverage gate;
- allocation, latency, code-size, W^X, liveness, and no-CGO evidence passes;
- Sol records `PROCEED` or `STOP` from raw artifacts;
- passing artifacts are frozen for production integration or failed artifacts
  are deleted;
- a complete `proof-handoff-v1.manifest` binds every visible, composition,
  held-out, transition, raw-capture, ABI, and normalized-leaf artifact;
- the production plan has been trimmed so it integrates, completes, and
  validates the proven core instead of implementing it again.
