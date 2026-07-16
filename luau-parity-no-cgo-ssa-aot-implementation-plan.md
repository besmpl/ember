<!-- simple-loop-plan -->

# Luau Parity Without CGO: SSA AOT-to-Go Implementation Plan

Status: active implementation; the parity observer, verified backend IR,
private generated-Go proof, owner binding, and first exact-exit/direct-call
slices are retained. No public prepared surface has been earned.

Architecture decision:
[ADR 0008](docs/adr/0008-ssa-aot-for-luau-parity.md)

Target platform: Darwin 24.6.0, Apple M1 arm64, Go 1.26.4,
`CGO_ENABLED=0`, `GOMAXPROCS=1`, pinned Luau 0.728 at SHA-256
`c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd`.

## Outcome

Implement and retain a build-time prepared execution mode only if a real,
general compiler can generate ordinary Go that matches or beats pinned Luau on
every qualified workload while preserving Ember semantics, effects, ownership,
and deployment constraints.

The selected architecture is:

```text
verified Program / codeImage
          |
          v
private backend IR
CFG + SSA + effects + liveness + shapes + escapes + exit maps
          |
          +-----------------------------+
          |                             |
          v                             v
ordinary Go lowerer              conditional Plan 9 ARM64 lowerer
primary prepared backend         only after complete Go evidence
          |
          v
explicit prepared bundle bound to one Machine owner
          |
          +---- exact side exit ----> canonical Machine/effect executor
```

The canonical Machine remains the complete dynamic runtime and exact fallback.
This plan does not replace it with another interpreter.

### Done when

- D1. Two independent exact-commit `prepared-parity1x` captures contain all 37
  correct `guest_batch_v1` rows. In each capture, every row has median
  Ember/Luau ratio `<=1.00` and nearest-rank p90 `<=1.05`.
- D2. Prepared, generic Machine, and the independent semantic oracle agree on
  values, errors/frames/PCs, instruction charges, effects, table order,
  metatables, modules, callbacks, coroutines, cancellation, limits, owner
  isolation, busy/close behavior, and detached-result lifetime.
- D3. Generated code specializes only from verified program structure and
  runtime-derived tags, shapes, versions, targets, arities, and metatable
  state. Transformed holdouts and source-identity mutation tests prove that
  source names, hashes, benchmark IDs, expected results, and corpus membership
  do not select behavior.
- D4. The retained tree passes the recursive pure-Go source/object/linked
  boundary with `CGO_ENABLED=0`. Runtime execution invokes no compiler, plugin,
  helper process, foreign runtime, dynamic loader, executable-memory API,
  private Go ABI, or `go:linkname`.
- D5. `guest_batch_v1` and `public_call_v1` remain separate schemas.
  Prepared guest throughput may satisfy D1; ordinary callback/runtime lifecycle
  must independently preserve current behavior and frozen warmed allocation
  ceilings. Documentation makes no dynamic or single-boundary 1:1 claim unless
  that separate measurement passes.
- D6. No all-37 warmed row exceeds its frozen B/op or allocs/op ceiling.
  Generated source bytes, generation time, Go build time, linked text/rodata,
  guard coverage, side-exit coverage, and per-program version count are
  reported and bounded. Generated output grows linearly with input operations.
- D7. A deterministic prepared bundle carries an explicit ABI version,
  semantic version, complete program hash, ordered module/Proto inventory, and
  entrypoint inventory. Mismatch fails before owner mutation and never silently
  falls back.
- D8. No public prepared API, generated production fixture, or alternate
  backend remains if the private compiler proof or final retention gate fails.
  Rejection retains tests, external evidence, ADR 0008, and this plan only.

### Constraints

- No CGO or disguised foreign backend: no `import "C"`, C/C++/Objective-C,
  foreign archive/object, embedded Luau, subprocess service, plugin, runtime
  compiler, JIT, executable mapping, private linker hook, or new dependency.
- The normal Go compiler may compile deterministic generated source during the
  host build. No compilation occurs during Ember runtime startup or execution.
- Keep the root package flat unless a real private compiler module makes the
  public surface smaller. Do not create public packages merely to organize the
  implementation.
- Generated code and the Machine share one owner and one mutable object graph.
  No mirrored tables, mid-run object conversion, or per-opcode old/new routing.
- All guard failures have exact, replay-safe Machine exits. Prefer exits before
  mutation. Any post-mutation exit needs one explicit commit protocol and
  differential tests.
- Preserve user work. Do not modify or delete the existing untracked plan files
  while executing this plan.
- Add no dependency without asking.

### Non-goals

- Dynamic `Compile` parity, runtime JIT, CGO/upstream Luau embedding, a raw
  Mach-O backend, other CPU architectures, Hearth repository wiring, new Luau
  language features, or deleting the canonical Machine.
- Making public callback latency disappear from product reporting.
- Retaining static assembly merely because one arithmetic function is fast.

## Starting evidence

### Current complete Machine

Two clean all-37 captures from commit
`24797945c04b1cd104e802b375f8233fe459909b` have directory hashes:

- A: `c0b2f7e4da30a532a265944642cf949d1d64747e8f8757d8bb00bea2231e70b0`;
- B: `8529c07faea2ccbe82dd7ee9477e201e8954c965f07dcf23942019ecc66739dc`.

Every row misses parity. The range is about 3.02x-22.2x Luau. Arithmetic is
about 5.85x, recursive Fibonacci about 6.2x, sparse-grid about 9.7x,
signal-bus callbacks about 15.8x-16.9x, command varargs about 18.0x, stable
fields about 21x, and array operations about 22x.

This rejects deeper interpreter dispatch or quickening as the primary parity
architecture. The Machine remains the complete fallback.

### Retained architecture ceiling

`internal/architectureproof` holds eleven parameterized manual semantic
lowerings and generic-representation sensitivity variants. The proof is not a
compiler implementation. It demonstrates:

- ideal direct Go medians around `0.011x-0.100x` Luau;
- ordinary Go string/map/closure/variadic sensitivity with the worst
  representative median around `0.32x`;
- zero allocations for most specialized rows;
- explicit allocation pressure in generated-string/table sensitivity;
- about 5,232 bytes of linked text for the eleven specialized functions and
  recursive helper;
- direct Plan 9 ARM64 arithmetic tied with compiled Go;
- a discarded pure-Go JIT arithmetic leaf tied with Go and lacking a
  production-safe Go runtime integration.

The exact proof commit is `b9717cc835637d3902a3df5058bb8985a58fe85a`.
Its clean capture directory hashes are:

- ceiling A: `2cfa240c51957e65574fca9c98364794cadd8e695775c293b72956d75eb2cf9c`;
- ceiling B: `72813a1be85dfb5ff985b711bcdfa249643774609f82fc1dcc4af4007b948906`;
- sensitivity A:
  `a24f21ff5a9abda1bbc72fd0d5546b5d48bd46b644c66a4b049a7587f8ab04f9`;
- sensitivity B:
  `caacfd7d7f4f995a66d354c975b4bf5402fa97167a6f2a98ad469cb066b50fb8`.

The proof runner records one program per case, runtime N/seed, checksums,
allocations, binary/source hashes, clean-runner facts, and exact Git revision.

### Existing implementation seams

- `runtime_image.go`: immutable `codeImage`, `machineProto`,
  `machineOperation`, blocks, exact word PCs, call/result descriptors, and
  guest charges.
- `function_analysis.go`: reusable verified function facts and the natural
  starting point for deeper backend analysis.
- `runtime_machine_owner.go`: `machineOwner.executeStopped` is the backend
  selection and owner-lifetime seam.
- `runtime_machine.go`: scalar registers/continuations plus `restartPC` and
  `skipCharge` for exact replay.
- `runtime_machine_table.go`: scalar table arena with `rawVersion` and
  `metaVersion`; it lacks the stable layout/shape ID needed by direct guarded
  field offsets.
- `table_shape.go` and `property_ic.go`: already-tested shape/property semantics
  that should inform the Machine shape contract.
- `runtime_machine_*`: complete effects, calls, closures/upvalues, modules,
  callbacks, coroutines, limits, errors, and owner lifecycle.

### Current implementation checkpoint

The retained path has moved beyond the original starting state:

- `9cc08155` replaced the parity observer with `guest_batch_v1`, and
  `dda2b8f0` recorded corrected Machine captures.
- `a4240224` and `bb960881` added deterministic verified control-flow/SSA IR,
  semantic facts, effects, liveness, spills, call classifications, program
  inventory, and binding hashes.
- `2067c99d`, `aa4c1068`, and `351ed300` added a deterministic private
  numeric Go lowerer, generated fixtures, and explicit owner-bound prepared
  functions.
- `284a5624` added typed exact non-entry spill exits and Machine replay,
  including malformed-exit rejection before canonical mutation.
- The current direct-call slice scalar-replaces a proven nonescaping local
  closure, calls its fixed leaf Proto as ordinary Go, propagates a callee guard
  through replay-safe caller-entry fallback, and preserves generic Machine
  errors. The generated owner path performs 64 guest calls in roughly
  245-292 ns on the Apple M1 checkpoint machine, versus roughly
  14.8-15.4 microseconds through generic Machine, with zero prepared
  allocations. Linked ARM64 inspection shows the Go compiler inlines the leaf
  callee into the prepared body with no `CALL` instruction and no opcode or
  descriptor dispatch.
- The current stable-field slice scalar-replaces the nested tables in the
  exact `top10/table_fields` architecture-proof shape into three typed Go
  field locals. The prepared owner path takes roughly 333-343 ns on the
  checkpoint machine, versus roughly 102-105 microseconds through generic
  Machine, with zero prepared allocations and no materialized Machine table.
  Linked ARM64 inspection contains no calls, opcode dispatch, descriptor
  dispatch, or table lookup.
- The current dense-array slice scalar-replaces the exact
  `top10/generic_iteration` architecture-proof shape into one fixed typed Go
  array and a direct index loop. It accepts only a fully initialized local
  dense array, one iterator, an unused guest key, one concrete scalar element
  type, no hash fields, and no writes after iteration begins; other shapes fail
  closed to Machine. The prepared owner path takes roughly 112-114 ns on the
  checkpoint machine, versus roughly 4.7-5.3 microseconds through generic
  Machine, with zero prepared allocations and no materialized Machine table.
  The direct generated body takes roughly 10.6-11.1 ns. Its linked ARM64 body
  is 192 bytes, contains no calls, and lowers the loop to indexed loads plus a
  fused multiply-add with no opcode, descriptor, iterator, or table dispatch.
- The current bounded mutable-array slice scalar-replaces the exact
  `top10/array_ops` architecture-proof shape into a fixed-capacity typed Go
  ring. The compiler derives the capacity only from verified constant numeric
  loop bounds and accepts only append-form `table.insert`, front-position
  `table.remove`, and `rawlen` on one nonescaping empty local array. Unbounded
  growth, positional insertion, non-front removal, mixed fields, mutation plus
  iteration, unsupported element unions, and capacity above the fixed proof
  limit fail closed to Machine. The prepared wrapper verifies the three
  intrinsic identities before any scalar mutation, and execution-policy runs
  remain on canonical Machine. The prepared owner path takes roughly
  257-270 ns on the checkpoint machine, versus roughly 65-71 microseconds
  through generic Machine, with zero prepared allocations and no materialized
  local Machine table. The direct generated body takes roughly 122-133 ns.
  Its linked ARM64 body is 528 bytes; explicit head/tail bounds remove every
  linked bounds-panic/helper call, leaving only Go's entry stack-growth slow
  path and no opcode, descriptor, intrinsic, or table dispatch in hot code.

This is proof of the selected architecture and one required call shape, not
proof of P2 coverage, the representative private gate, a public API, or final
all-37 parity. Non-dense iteration, general table mutation, strings,
closures/upvalues, varargs/results, recursion, metatables, host/effect exits,
modules, and coroutines remain on the active path.

## Active execution path

### P0. Freeze the equivalent parity and rejection contracts [D1, D3, D5]

**Touch**

- `runtime_parity_test.go`
- `scripts/check-runtime-parity`
- `scripts/runtime-ratio-gate`
- `docs/checks.md`
- new parity schema fixtures/tests

**Change**

Add `guest_batch_v1` and a schema-isolated `prepared-parity1x` phase.

For each case, construct one parameterized guest program:

```text
case(seed) -> result
batch(n, seed):
    checksum = 0
    for i = 1..n:
        checksum += case(seed + i) * (i % 7 + 1)
    return checksum
```

Compile/prepare/bind outside the timer. Time one outer entry executing N guest
calls. Use the same program shape and checksum in Ember and Luau. Keep
N=`{1,10,100,1000}`, three repeats, fitted slopes, alternating acquisition
order, contamination rejection, exact result hashes, and nine Cartesian
ratios.

Keep the existing public-call/lifecycle rows under a separately named
`public_call_v1` schema. A prepared capture cannot be accepted by a dynamic or
public-call gate.

Add transformed holdout generation that renames sources/functions, changes
irrelevant constants/layout, and uses seed `0x454d42455207` after the generator
is frozen.

**Proof**

- The generic Machine and Luau produce equal checksums for all 37 programs and
  at least four runtime seeds.
- Fixture tests reject swapped schemas, fixed-N programs, multiple programs per
  N point, source-name routing, missing lifecycle rows, and incomplete captures.
- Two clean generic Machine captures reproduce the current broad gap without
  silently including public-boundary slope.
- `scripts/check-runtime-parity --self-test`,
  `scripts/runtime-ratio-gate --self-test`, focused Go tests, and
  `git diff --check` pass.

**Retention**

Retain P0 regardless of the AOT result because it repairs measurement
equivalence and keeps lifecycle claims honest.

### P1. Build one private backend IR and stable Machine shape seam [D2, D3]

**Touch**

- new `runtime_backend_ir.go`
- new `runtime_backend_analysis.go`
- new `runtime_backend_verify.go`
- new focused tests/fuzz target
- `runtime_image.go`
- `runtime_machine_table.go`
- `runtime_machine_table_semantics.go`
- `table_shape.go`/`property_ic.go` only where semantic reuse is needed

**Change**

Build a deterministic private backend IR from verified `codeImage` data:

- CFG, predecessors, dominators, natural loops, and critical-edge splitting;
- SSA values and phis for registers, varargs, results, and open values;
- tag/value representation facts with conservative union joins;
- effect tokens for allocation, tables, metatables, globals, calls, yields,
  host effects, errors, limits, cancellation, and collection;
- liveness and exact spill sets at every guard/effect/safepoint;
- escape facts for tables, closures, cells, strings, tuples, and coroutine
  state;
- direct/fixed, guarded-polymorphic, and dynamic call classifications;
- exact module/Proto/image-PC/word-PC/source/guest-charge maps.

Add an owner-local table shape/layout ID and stable property-offset contract.
Equal stable layouts share an ID. Value updates preserve it. Layout-changing
mutations, dictionary promotion, and relevant metatable changes invalidate
guards. `rawVersion` and `metaVersion` remain independent mutation signals.

Do not route production execution to the backend.

**Proof**

- Every eligible Machine operation receives one verified backend
  classification and exact effect/exit policy.
- CFG/SSA/liveness verification rejects malformed phis, missing predecessors,
  charge gaps, effects without exits, and live values without spill locations.
- Differential shape tests cover insert/update/delete/reinsert, array/hash
  interaction, dictionary promotion, metatable replacement, and two independent
  owners.
- Fuzzing arbitrary verified Proto graphs never panics, produces deterministic
  IR, and either verifies or fails closed with context.
- `go test` focused on image, function analysis, tables, metatables, and the
  new IR passes under race and checkptr where practical.

**Retention**

P1 may remain only if it is consumed by the compiler proof in P2. If P2 rejects
AOT, remove the backend IR and any unused production shape fields; keep only
measurement and proof evidence.

### P2. Prove a real generated-Go compiler privately [D1, D2, D3, D6, D8]

**Touch**

- new `runtime_backend_go.go`
- new `runtime_backend_go_emit.go`
- new private prepared context/exit files
- generated test fixture under `internal/` or `_test.go`
- `scripts/check-prepared-proof`
- `internal/architectureproof` integration

**Change**

Implement the smallest real compiler from P1 IR to ordinary Go. This is not a
handwritten lowering:

- emit one function per Proto plus explicit coroutine resume states;
- keep refined scalar values in typed Go locals;
- scalar-replace proven nonescaping tables, closures, cells, tuples, and
  vararg/result packs;
- call fixed targets directly and guard stable dynamic targets;
- use fixed tuples for proven result shapes;
- use exact shape/property/global/metatable guards for escaping state;
- reserve required arena capacity before entering mutation-heavy blocks;
- emit pre-mutation side exits with exact spill/PC/charge data;
- call no external helper per guest operation in a hot scalar block.

The first generated fixture covers at least:

- arithmetic/branches;
- stable fields and array iteration;
- dynamic closures/upvalues;
- recursion and fixed calls;
- varargs/multiple returns;
- metatable fallback;
- coroutine resume/yield;
- generated strings and mutating tables.

Freeze the generator before revealing transformed holdouts.

**Private proof gate**

- Two clean representative captures: every row median `<=0.80`, p90 `<=0.90`.
  This reserves integration headroom before any public surface.
- Generated and Luau checksums match across runtime seeds and transformed
  holdouts.
- `go tool objdump` shows no opcode loop or sequential descriptor dispatch in
  generated functions.
- `-gcflags=all=-m=2` plus disassembly show that the context ABI is inlined or
  crossed only at coarse entry/exit/effect boundaries.
- No warmed allocation exceeds the relevant Machine ceiling.
- Generated source and text grow linearly; the proof records exact bytes and
  build time.

**Stop**

If any representative family cannot meet the private gate and one profile does
not identify a single already-designed seam with enough mass to close every
miss, delete the compiler, private prepared runtime changes, generated fixture,
and unused P1 data. Do not expose a public API and do not proceed.

### P3. Make side exits and owner semantics complete [D2, D4, D7]

**Touch**

- private prepared owner/context/exit files
- `runtime_machine_owner.go`
- `runtime_machine.go`
- affected `runtime_machine_*` effect modules
- differential, lifecycle, forced-GC, race, and checkptr tests

**Change**

Bind generated code to the same owner as the Machine:

- validate artifact identity before allocating or mutating owner state;
- enter through `machineOwner.executeStopped`;
- spill exact live values and continuation state on every side exit;
- reuse `restartPC`/`skipCharge` or replace them with one clearer exact replay
  record;
- preserve open upvalues, varargs, result modes, logical call frames, and
  coroutine snapshots;
- route host calls, modules, growth, collection, cancellation, limits, errors,
  and unsupported semantics through the existing effect executor;
- bulk-charge only when the whole block fits the remaining instruction budget;
  otherwise exit at the exact first PC;
- make exit-without-progress impossible or explicitly terminal.

Generated code may use the Go stack for a proven direct call only while a
logical Machine frame is maintained for limits, errors, and backtraces.
Suspended coroutines always use explicit owner state, never a suspended Go
stack.

**Proof**

- Three-way differential tests cover every semantic/effect family and forced
  guard failure at every generated exit.
- Injected failures before/after allocation, table growth, host calls, module
  load, yield, cancellation, and limit exhaustion prove exactly-once mutation
  and exact error PC/frame.
- Independent owners, busy/close, callback escape, detached results, repeated
  bind/run/close, and forced GC preserve lifetimes with no stale handle reuse.
- Race/checkptr and pure-Go linked-binary inspection pass.

### P4. Generalize generation and freeze coverage [D2, D3, D6]

**Touch**

- backend IR/lowerer
- generator golden tests
- opcode/effect coverage manifest
- generated holdout packages
- code-size/build-time reports

**Change**

Extend the compiler from representative fixtures to every operation required by
the all-37 inventory. Every operation is exactly one of:

- inline;
- guarded inline with an exact exit;
- canonical effect exit;
- explicitly ineligible with a fail-closed preparation error.

Add deterministic multi-versioning only where tag/shape/call alternatives have
measured value. Cap versions and generated bytes per Proto/program. Prefer one
generic guarded block over combinatorial versions.

Materialize escaping values into Machine arenas at the last safe point. Use
owner string IDs and scalar table/closure records rather than ordinary Go maps
or heap closures where the warmed allocation gate requires it.

**Proof**

- The coverage manifest has no unclassified operation or effect.
- Golden regeneration is byte-for-byte deterministic across repeated and
  concurrent runs.
- Generator fuzzing rejects malformed/oversized input without partial output.
- The frozen transformed holdout passes after generation code is no longer
  editable for that run.
- Source bytes, function count, text/rodata, build time, and version count are
  linear and below explicit limits.

### P5. Expose the prepared artifact only after the private proof [D5, D7, D8]

**Touch**

- `program.go`
- runtime options/constructor files
- new narrow public prepared declarations in the root package
- new `cmd/emberc`
- public-surface/design docs and examples

**Change**

Add the smallest explicit lifecycle proven by P2-P4:

- `Program.WritePreparedGo` writes one deterministic application-owned package;
- `cmd/emberc` is a thin manifest/IO wrapper around that method and supports
  `-check` freshness validation;
- generated code exposes one immutable `PreparedBundle`;
- `RuntimeOptions.Prepared` or an equivalent explicit constructor argument
  supplies the bundle;
- nil selects the ordinary Machine;
- a supplied invalid/stale bundle returns a typed error before owner mutation;
- no `init` registration, global registry, source-name lookup, plugin, runtime
  build, or public raw arena/register access.

The opaque generated-code context is public only to the degree required for
generated packages. Keep constructors boring and document bundles as trusted
build artifacts, not an untrusted-code sandbox.

**Proof**

- Public examples generate, compile, bind, run, mismatch, and close a bundle.
- API tests freeze explicit selection and nil behavior.
- Cross-package compiler/objdump tests preserve the private proof's hot shape.
- A clean removal test demonstrates that deleting the generated package leaves
  the dynamic runtime unchanged.

### P6. Earn all-37 retention or delete the mode [D1-D8]

**Touch**

- prepared parity/allocation/report scripts
- scheduled evidence workflow only if the local contract is stable
- `docs/checks.md`
- `performance-audit.md`
- ADR 0008 evidence section if final facts differ
- public docs

**Change**

Generate all 37 programs from one frozen compiler and acquire:

- two clean exact-commit `prepared-parity1x` captures;
- warmed allocation/lifecycle captures;
- generic Machine comparison captures;
- generation/build/source/text/rodata reports;
- guard-hit, side-exit, effect-exit, and fallback coverage;
- CPU/allocation profiles for every miss or near-threshold row.

No row may be omitted or waived by a family/geomean result.

**Retention gate**

- D1-D7 all pass on the exact retained commit.
- Prepared coverage, rather than generic fallback, owns the timed mass of every
  passing row.
- No current public/lifecycle row regresses beyond its frozen timing envelope
  or allocation ceiling.
- `scripts/check-purego`, `scripts/check-fast`, `scripts/check`, race, checkptr,
  generated freshness, and linked-binary inspection pass.

**Failure disposition**

If one bounded correction cannot close every remaining row, remove:

- the public prepared declarations;
- `cmd/emberc`;
- production prepared routing/context/exit code;
- generated production fixtures;
- backend-only Machine fields with no remaining consumer.

Retain the corrected measurement contract, architecture proof package, ADR,
external evidence hashes, and rejection report. Do not weaken D1 or relabel a
partial result as parity.

## Conditional work

### C1. Add a Plan 9 ARM64 lowerer only for a proven Go codegen deficit

**Trigger**

P6 is semantically and operationally complete, but one or more rows miss D1,
and exact profiles/disassembly show enough remaining time in Go instruction
selection, register spills, or unavoidable ABI moves to close every miss.
Representation, guard failure, effects, allocations, and public boundaries must
not be the dominant cause.

**Then**

- Lower the same verified backend IR and exact exits to generated Plan 9 ARM64.
- Use ABI0 Go prototypes and pointer-free leaf state where possible.
- Keep a generated-Go fallback and identical differential tests.
- Inspect symbols, text size, pointer maps, calls, stack bounds, preemption
  canaries, and linked segments.
- Retain assembly only if it passes the complete all-37 gate and improves broad
  rows without warmed allocation or lifecycle regression.

**Else**

No assembly work.

### C2. One bounded Go-lowerer correction

**Trigger**

A private or final gate misses, and one profile identifies one already-designed
spill, guard, shape, call, escape, or exit seam with enough mass to close every
remaining miss.

**Then**

Correct only that seam and rerun all downstream proof. Do not add a new
architecture, identity selector, unbounded versioning, JIT, or threshold
exception.

## Deferred / external

- Hearth build integration follows in the Hearth repository after P6 retains
  the public contract.
- Other architectures require their own generated-code and final-capture
  evidence.
- Dynamic-runtime 1:1 is a separate goal and cannot be inferred from prepared
  evidence.

## Commit sequence

Keep commits independently checkable and remove failed spikes before the final
tree:

1. `test: add equivalent prepared parity contract`
2. `runtime: add verified backend IR`
3. `runtime: add stable machine table shapes`
4. `test: prove private generated go backend`
5. `runtime: complete prepared side exits`
6. `runtime: generalize prepared generation`
7. `api: expose explicit prepared bundles`
8. `test: certify prepared parity`
9. `docs: document retained or rejected prepared mode`

Do not create commits 7-8 if the private gate fails.

## Final verification

On the final retained-or-deleted tree:

```sh
CGO_ENABLED=0 go test ./internal/architectureproof
CGO_ENABLED=0 GOMAXPROCS=1 \
  EMBER_ARCHITECTURE_PROOF_LIVE=1 \
  LUAU_BIN=/opt/homebrew/bin/luau \
  go test -run '^TestProofCasesMatchLuau$' ./internal/architectureproof
scripts/check-purego
scripts/check-fast
scripts/check
git diff --check
```

If prepared mode is retained, also run the exact generated freshness,
all-37 A/B, allocation/lifecycle, race, checkptr, object inspection, and
linked-binary commands introduced by P0-P6. Performance artifacts must bind the
exact final commit and clean-worktree fact.

## Risks and assumptions

- The manual ceiling proves available headroom, not that the compiler will
  recover it. P2 is the decisive compiler proof and may reject the route.
- Cross-package inlining may not erase a fine-grained opaque context ABI.
  Generated code must keep hot state local and cross the ABI at block/effect
  boundaries; otherwise reject the public shape before retention.
- Scalar replacement can change allocation behavior and object materialization
  timing. Escapes, identity, metatables, errors, weak/lifetime behavior, and
  callbacks require conservative materialization and differential tests.
- Direct Go recursion is fast but cannot replace logical guest frames or
  coroutine state. Owner-visible frames remain explicit.
- Stable shapes improve direct properties but cannot assume all tables are
  shaped. Dictionary and dynamic metatable paths must exit exactly.
- Code size can erase instruction-cache gains. Version and output caps are
  blocking product gates, not report-only polish.
- D1 is deliberately strict. A suite that is faster on average but loses one
  row is not complete.
