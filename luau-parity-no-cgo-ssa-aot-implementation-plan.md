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
- The current mutable-closure slice scalar-replaces a parameterized
  `top10/closures_upvalues` factory shape into one typed Go cell. It accepts
  only one nonescaping returned local closure, one mutable numeric capture
  initialized directly from one factory parameter, one fixed numeric target,
  and no closure merge, copied capture, derived capture, second upvalue,
  variadic target, or escaping use. The generated target receives the cell
  explicitly, while each caller invocation mutates a scratch copy and commits
  only after a successful return so any target guard remains replay-safe.
  Unsupported shapes fail closed to Machine. The prepared owner path takes
  roughly 115-139 ns on the checkpoint machine, versus roughly
  11.7 microseconds through generic Machine, with zero prepared allocations
  and no materialized Machine closure or cell. The direct generated kernel
  takes roughly 73-87 ns. Go inlines the 53-cost captured-cell helper into a
  160-byte prepared body containing no calls, stack frame, opcode/descriptor
  dispatch, closure lookup, or upvalue lookup.
- The current bounded-recursion slice scalar-replaces the self-capturing local
  closure in a parameterized `classic/recursive_fibonacci` shape and emits an
  ordinary direct recursive Go function. It accepts one numeric parameter, one
  self upvalue, a finite numeric base guard, and recursive arguments proven to
  subtract finite constants of at least one from that parameter. Other upvalue
  access, nondecreasing recursion, variadic calls, unsupported operations, or
  an entry argument above 24 fail closed to Machine. A checked wrapper
  validates the entry once; the generated 128-byte recursive body returns one
  scalar directly, avoiding a result/guard branch on every recursive edge. The
  prepared owner path has a five-sample median of about 38.3 microseconds on
  the checkpoint machine (37.2-44.7 microseconds observed), versus about
  4.58 milliseconds through generic Machine (4.54-4.86 milliseconds
  observed), with zero prepared allocations and no materialized self closure
  or cell. The direct generated kernel has a five-sample median of about
  30.8 microseconds (30.0-31.1 microseconds observed). Linked ARM64 contains
  only the two intentional recursive calls plus Go's stack-growth slow path,
  with no opcode, descriptor, closure, or upvalue dispatch.
- The current fixed-vararg slice scalar-replaces the local variadic closure and
  a proven five-value vararg pack in a parameterized `top10/varargs_select`
  shape. Variadic targets are specialized by verified call arity, capped at
  32 values for bounded generated size; mismatched arity, open vararg results,
  nonnumeric values, and unsupported operations fail closed. The guarded
  `select("#", ...)` intrinsic becomes the fixed numeric count, while the
  prepared entry checks the target Proto's actual global intrinsic before any
  generated work and replays the canonical entry if it was rebound. The
  prepared owner path has a five-sample median of about 267 ns on the
  checkpoint machine (254-285 ns observed), versus about 20.64 microseconds
  through generic Machine (20.31-21.93 microseconds observed), with zero
  allocations and no materialized closure or vararg slice. The direct
  generated kernel has a five-sample median of about 197 ns (196-203 ns
  observed). Its 64-byte score helper is straight floating-point code; the
  prepared body contains one direct helper call and no opcode, descriptor,
  intrinsic, closure, or vararg dispatch.
- `2fc39584` repaired generic fixed-result CALL frame reservation so a scratch
  destination owns its complete result window before register compaction.
  The regression covers a natural loop with a three-result nested call.
- The current fixed-tuple slice scalar-replaces a proven nonescaping local
  closure and its three numeric results in a seed-dependent natural loop.
  Every reachable target return must have one identical fixed arity, capped at
  32 results; open, inconsistent, mismatched, nonnumeric, escaping, or
  otherwise unsupported shapes fail closed. Fixed results become ordinary Go
  multiple returns and bind directly to caller SSA locals without a slice,
  Machine result pack, or materialized closure. Scalar replacement propagates
  through proven SSA aliases, including the loop-carried closure phi. The
  prepared owner path has a five-sample median of about 203 ns on the
  checkpoint machine (201-208 ns observed), versus about 12.0 microseconds
  through generic Machine (11.99-12.10 microseconds observed), with zero
  allocations. The direct generated kernel has a five-sample median of about
  102 ns (101-111 ns observed), while live pinned Luau `-O2 -g0` has a warmed
  median of about 657 ns on the same source. Go inlines the tuple helper into a
  176-byte kernel with no call instruction, stack frame, opcode/descriptor
  dispatch, closure lookup, or tuple allocation.
- The current static-metatable slice promotes guarded base
  `setmetatable`/`getmetatable` calls to Machine `FAST_CALL` operations and
  scalar-replaces the exact `top10/metatable_index` shape when one
  nonescaping local table receives one local metatable whose sole field is a
  table-valued `__index`. Own fields take precedence; fallback lookup follows
  the verified table chain with cycle rejection. Function-valued or dynamic
  handlers, `__newindex`, protected or changed metatables, `getmetatable`
  observation, unsupported call shapes, and unresolved fields fail closed.
  Prepared entry verifies the live `setmetatable` intrinsic identity before
  generated work and replays canonical Machine on mismatch. The prepared
  owner path has a five-sample median of about 224 ns on the checkpoint
  machine (219-256 ns observed), versus about 21.5 microseconds through
  generic Machine (20.4-22.4 microseconds observed), with zero allocations
  and no materialized Machine table. The direct generated kernel has a
  five-sample median of about 110 ns (108-111 ns observed), while live pinned
  Luau `-O2 -g0` has a warmed median of about 1.97 microseconds on the same
  source. Linked ARM64 is 128 bytes for the direct kernel and 160 bytes for
  the prepared body, with no call instruction, stack frame, opcode/descriptor
  dispatch, metatable lookup, or table lookup.
- The current static-method slice scalar-replaces the parameterized
  `top10/method_calls` shape when one nonescaping local table receives exactly
  one dominating, capture-free method closure and that fixed method reads or
  writes numeric fields on the identical receiver. The generated method target
  receives pointers only to those proven fields; the caller copies them to
  scratch, calls directly, checks the target result, and commits only on
  success, so replay-entry fallback cannot expose partial receiver mutation.
  Captured, reassigned, conditional, escaping, materialized, variadic,
  polymorphic, nonnumeric, or otherwise unsupported method shapes fail closed.
  Source/module/entrypoint identity mutation produces identical executable
  source. The prepared owner path has a five-sample median of about 126 ns on
  the checkpoint machine (124-130 ns observed), versus about 34.8 microseconds
  through generic Machine (34.4-46.0 microseconds observed),
  with zero allocations and no materialized Machine table or closure. The
  direct generated kernel has a five-sample median of about 87.6 ns
  (73.2-99.4 ns observed), while the repository's live pinned Luau CLI batch
  warmed to about 5.2 microseconds per run. Go inlines the method helper into a
  128-byte linked ARM64 kernel containing no call instruction, stack frame,
  opcode/descriptor dispatch, closure lookup, method lookup, or table lookup.
- The current scalar-coroutine slice completes the ten-family Top10
  architecture proof. The compiler promotes immutable
  `coroutine.create`/`resume`/`status`/`yield` members to guarded Machine
  `FAST_CALL` operations, then scalar-replaces one proven nonescaping,
  capture-free coroutine whose body has one numeric parameter, numeric yields,
  one numeric return, and no consumed resumed values. Generated Go uses one
  explicit state record and a resumable CFG step function; no Go stack remains
  suspended between guest yields. Prepared entry guards all four caller/target
  intrinsic identities before work and replays canonical Machine on any
  mismatch. Captured, escaping, multiple, conditionally ordered,
  resume-argument-changing, resumed-value-consuming, nonnumeric, non-`dead`
  status, or otherwise unsupported shapes fail closed. Source, module, and
  entrypoint identity mutation produces identical executable source. The
  prepared owner path has a five-sample median of about 401 ns on the
  checkpoint machine (393-448 ns observed), versus about 7.78 microseconds
  through generic Machine (7.39-7.87 microseconds observed), with zero
  allocations and no materialized Machine coroutine or closure. The direct
  generated kernel has a five-sample median of about 144 ns (143.9-144.5 ns
  observed), while live pinned Luau `-O2 -g0` has a warmed median of about
  992 ns on the same parameterized source. Linked ARM64 is 256 bytes for the
  caller, 480 bytes for the step function, and 304 bytes for the prepared
  body; caller code contains two direct step calls and no opcode, descriptor,
  intrinsic, closure, or coroutine-runtime dispatch.
- The first post-Top10 finite-string slice represents immutable image strings
  as owner-neutral `uint32` scalar IDs. String constants, phis, equality,
  scalar fields, and fixed scalar arrays now lower directly; nested scalar
  array iterators propagate their table/control identity through the SSA phi
  graph rather than requiring one direct loop-header phi. A parameterized
  five-state/seven-event transition kernel compiles to ordinary Go with no
  runtime string, table, opcode, descriptor, or interning operation in the hot
  path. Source/module/entrypoint renaming and a one-to-one literal-text
  holdout produce identical executable source, while generated concatenation,
  escaping string results, mixed string/number state, and other unproved
  shapes fail closed. An exit with a live image string replays entry rather
  than fabricating an owner-local handle. The direct fixture has a five-sample
  median of about 573 ns on the checkpoint machine (572.6-607.5 ns observed);
  the prepared owner path has a median of about 635.5 ns (634.7-636.8 ns
  observed), versus about 72.8 microseconds through generic Machine
  (72.8-73.5 microseconds observed). All prepared/direct samples report zero
  allocations, and the prepared path materializes no Machine table or new
  owner string. Live pinned Luau `-O2 -g0` produced the same
  `963500000` checksum and a warmed median of about 11.16 microseconds per
  call (11.11-11.47 microseconds observed), so direct and prepared are about
  `0.051x` and `0.057x` Luau respectively. Deterministic generated source is
  17,235 bytes; linked ARM64 is 656 bytes for the direct kernel, 704 bytes for
  the prepared body, and 128 bytes for the wrapper. The direct kernel has no
  call instruction or runtime dispatch.
- The generated-key prerequisite promotes unbound `tostring` calls to guarded
  `FAST_CALL` operations. The VM retains its allocation-free scalar and
  `__tostring` path, while a Machine guard miss now enters the ordinary
  callable path rather than accepting only a host replacement; rebound script
  functions therefore preserve the same continuation and result semantics as
  an ordinary call. Prepared code can now guard the exact `tostring` identity
  before specializing numeric string construction without weakening canonical
  fallback behavior.
- The first structural generated-key slice recognizes the exact
  `tostring(int32) .. safe-literal .. tostring(int32)` shape and represents it
  as a pointer-free pair of `int32` values. The separator must contain a byte
  outside the decimal integer alphabet, so distinct numeric pairs cannot
  collapse; empty, digit-only, and sign/digit separators fail closed. Local
  construction sites and direct target Protos receive distinct equality
  domains, and mixed domains are rejected. NaN, infinity, fractional values,
  values outside `int32`, negative zero, escaping results, and changed
  `tostring` identities replay or reject rather than fabricating a string.
  Source/module/entrypoint renaming and a safe separator-text holdout produce
  byte-identical generated source. The direct kernel has a five-sample median
  of about 7.96 ns (7.93-8.58 ns observed), while the prepared owner path has
  a median of about 71.6 ns (71.6-74.3 ns observed), versus about 842.6 ns
  through generic Machine (837.6-871.9 ns observed). Direct and prepared
  report zero allocations and materialize no owner strings. Live pinned Luau
  `-O2 -g0` has a warmed median of about 137.1 ns per call
  (134.6-140.4 ns observed), making prepared about `0.52x` Luau. Generated
  source totals 3,728 bytes. Linked ARM64 is 224 bytes for the key helper,
  240 bytes for the direct kernel, and 304 bytes for the prepared body; the
  caller has two direct helper calls but no opcode, descriptor, string,
  interning, or Machine dispatch.
- The bounded sparse-grid record slice composes structural generated keys with
  nonescaping fixed-shape numeric records, one fixed four-record offset array,
  and one capacity-128 open-addressed map. Record references are scalar slot
  numbers; record fields become typed locals or fixed Go arrays. Literal map
  keys must parse canonically in the same safe separator domain as every
  generated lookup key. Noncanonical literals, mismatched separator domains,
  inconsistent record shapes, escaping maps, unsupported uses, and unbounded
  topology fail closed to Machine. Prepared failure paths replay entry before
  canonical mutation; rebound `tostring` identities produce the same generic
  error. A separator-text/source/module/entrypoint holdout produces
  byte-identical generated source. The direct kernel has a five-sample median
  of about 28.3 microseconds on the checkpoint machine
  (28.3-29.7 microseconds observed), while the prepared owner path has a median
  of about 30.3 microseconds (29.7-33.3 microseconds observed), versus about
  5.04 milliseconds through generic Machine (4.85-5.09 milliseconds
  observed). Direct and prepared report zero allocations and materialize no
  owner tables or strings. Live pinned Luau `-O2 -g0` produced the same warmed
  result and checksum with a five-sample median of about 492.3 microseconds per
  call (485.9-510.9 microseconds observed), making prepared about `0.062x`
  Luau. Deterministic generated source is 33,800 bytes. Linked ARM64 is 2,784
  bytes for the direct kernel and 2,976 bytes for the prepared body; each has
  two direct structural-key helper calls, cold bounds/stack-growth helpers,
  and no opcode, descriptor, Machine table, runtime string, interning, or VM
  dispatch.
- The bounded typed-record-array slice removes the map prerequisite from the
  record scalarizer and infers one exact scalar kind per field from every
  initialization and mutation. Fixed arrays can now store number, boolean, or
  interned-string-ID fields directly in typed Go arrays; unresolved, mixed,
  nil-union, shape-changing, escaping, or otherwise unsupported fields fail
  closed. A parameterized `projectile_sweep` holdout proves two independent
  four-record arrays, nested iteration, mutable numeric fields, mutable
  booleans, loop breaks, and seed-dependent behavior. Generated code and the
  prepared owner agree with the independent interpreter across negative,
  ordinary, and large holdout seeds, allocate zero when warmed, and materialize
  no owner tables or strings. Private-function renaming produces byte-identical
  output; mixed boolean/number fields, shape drift, and array escape are
  rejected. The direct kernel has a five-sample median of about 810.9 ns
  (809.3-842.9 ns observed), while the prepared owner path has a median of
  about 878.3 ns (871.7-908.0 ns observed), versus about 268.3 microseconds
  through generic Machine (267.0-287.8 microseconds observed). Live pinned
  Luau `-O2 -g0` produced the same warmed result and checksum with a
  five-sample median of about 17.79 microseconds per call
  (17.69-17.97 microseconds observed), making prepared about `0.049x` Luau.
  Deterministic generated source is 23,072 bytes. Linked ARM64 is 1,184 bytes
  for the direct kernel and 1,328 bytes for the prepared body; each has only
  cold bounds/stack-growth calls and no opcode, descriptor, Machine table,
  runtime string, interning, or VM dispatch.
- The guarded numeric-intrinsic slice recognizes exactly the proved
  two-number, one-result `math.min` shape and emits ordinary Go `math.Min`
  over scalar SSA values. Prepared binding verifies the exact owner intrinsic
  identity before entering the generated body, so rebound `math.min` replays
  entry before any scalarized record mutation; wrong arity and nonnumeric
  operands fail closed. A parameterized `combat_tick` holdout proves one
  four-record array with mutable number and boolean fields plus the guarded
  intrinsic. Generated code, prepared owner, generic Machine, and the
  independent interpreter agree across negative, ordinary, and large seeds.
  A host replacement for `math.min` produces the same noncanonical result
  through prepared replay and generic Machine. Direct and prepared paths
  allocate zero when warmed and materialize no owner tables or strings.
  Private-function renaming produces byte-identical generated source. The
  direct kernel has a five-sample median of about 251.5 ns
  (243.5-262.2 ns observed), while the prepared owner path has a median of
  about 363.5 ns (359.1-381.3 ns observed), versus about 61.82 microseconds
  through generic Machine (61.77-61.92 microseconds observed). Live pinned
  Luau `-O2 -g0` produced the same warmed result and checksum with a
  five-sample median of about 3.518 microseconds per call
  (3.469-3.569 microseconds observed), making prepared about `0.103x` Luau.
  Deterministic generated source is 13,149 bytes. Linked ARM64 is 720 bytes
  for the direct kernel and 800 bytes for the prepared body; each calls Go's
  supported `math.archMin` helper and has only cold bounds/stack-growth
  helpers otherwise, with no opcode, descriptor, Machine table, runtime
  string, interning, or VM dispatch.
- The standalone-record slice extends the record scalarizer beyond container
  elements to nonescaping fixed-shape local records with dominated
  initialization, direct field reads, and same-kind mutations. A record may be
  stored into one proved container only when every write dominates the store
  and the original alias is dead afterward; escaping records, live aliases
  after storage, mixed field kinds, multiple stores, and unsupported uses fail
  closed. Proven-fresh scalar records also eliminate compiler-emitted
  metatable slow paths, and generated reachability removes the now-dead blocks
  and their SSA declarations deterministically. A parameterized
  `ability_resolution` holdout proves two mutable standalone records, one
  four-record typed array with finite string IDs, nested iteration, direct
  reads/writes, and guarded `math.min`. Generated code, prepared owner,
  generic Machine, and the independent interpreter agree across negative,
  ordinary, and large seeds. Rebound `math.min` replays entry before mutation;
  structural private-function mutation retains the same lowering shape.
  Direct and prepared paths allocate zero when warmed and materialize no owner
  tables or strings. The direct kernel has a five-sample median of about
  366.3 ns (358.4-373.0 ns observed), while the prepared owner path has a
  median of about 484.7 ns (477.5-544.0 ns observed), versus about
  165.84 microseconds through generic Machine
  (165.24-167.06 microseconds observed). Live Luau produced the same warmed
  checksum with a five-sample median of about 9.360 microseconds per call
  (9.349-9.546 microseconds observed), making prepared about `0.052x` Luau.
  Deterministic generated source is 18,756 bytes. Linked ARM64 is 960 bytes
  for the direct kernel and 1,040 bytes for the prepared body; each calls Go's
  supported `math.archMin` helper and has only cold bounds/stack-growth
  helpers otherwise, with no opcode, descriptor, Machine table, runtime
  string, interning, or VM dispatch.
- A second representative `ai_utility_scoring` capture composes one mutable
  standalone record with two independent four-record arrays, typed numeric
  fields, finite string IDs, nested iteration, comparisons, and integer
  division. Nine nonescaping records scalarize into ordinary locals and fixed
  arrays; generated code, prepared owner, generic Machine, and the independent
  interpreter agree across negative, ordinary, and large seeds. Private
  function mutation retains the same structural lowering; record escape,
  mixed standalone field kinds, and array shape drift fail closed. Direct and
  prepared paths allocate zero when warmed and materialize no owner tables or
  strings. The direct kernel has a five-sample median of about
  4.460 microseconds (4.189-5.432 microseconds observed), while the prepared
  owner path has a median of about 4.621 microseconds
  (4.466-5.165 microseconds observed), versus about 946.5 microseconds through
  generic Machine (842.9-3,010.0 microseconds observed). Pinned Luau `-O2 -g0`
  produced the same warmed checksum with a five-sample median of about
  69.805 microseconds per call (65.199-132.799 microseconds observed), making
  prepared about `0.066x` Luau. Deterministic generated source is 23,318
  bytes. Linked ARM64 is 1,168 bytes for the direct kernel and 1,280 bytes for
  the prepared body; each has only cold bounds/stack-growth helpers and no
  opcode, descriptor, Machine table, runtime string, interning, or VM
  dispatch. This supplies the second representative private compiler shape;
  the formal guest-batch capture contract still remains to be wired and run
  before any public prepared API.
- The dense record-array slice now preserves guest-visible iterator keys as
  ordinary numeric SSA values across loop phis and moves while keeping
  unobserved iterator control compiler-private. Verified integral dynamic
  indexing returns a scalar record reference; invalid, nonintegral, or
  out-of-range indices become nil and replay before dereference. A
  parameterized `procgen_room_scoring` holdout proves selection by a computed
  iterator index followed by repeated mutation of the selected record.
  Generated code, prepared owner, generic Machine, and the independent
  interpreter agree across negative, ordinary, and large seeds; NaN reaches
  the exact replay exit. Private-function mutation retains the structural
  lowering, while escaping arrays, mixed field kinds, nonnumeric keys, and
  sparse record arrays fail closed. Controlled execution remains on generic
  Machine, owner table/string counts do not change, and direct and prepared
  paths allocate zero when warmed. The direct kernel has a five-sample median
  of about 3.929 microseconds (3.765-4.303 microseconds observed), while the
  prepared owner path has a median of about 4.032 microseconds
  (3.951-4.397 microseconds observed), versus about 974.1 microseconds through
  generic Machine (958.3-990.1 microseconds observed). Pinned Luau `-O2 -g0`
  produced the warmed checksum with a five-sample median of about
  77.879 microseconds per call (50.300-90.837 microseconds observed), making
  prepared about `0.052x` Luau. Deterministic generated source is 22,616
  bytes. Linked ARM64 is 1,024 bytes for the direct kernel and 1,120 bytes for
  the prepared body; each has only cold bounds/stack-growth helpers and no
  opcode, descriptor, Machine table, runtime string, interning, or VM
  dispatch.
- The nested record-array slice represents each proved child array stored in a
  parent record as a small family selector. Child iterators carry explicit
  selector/position state, and yielded records use guarded packed numeric
  references into the already scalar-replaced fixed arrays. A parameterized
  `cooldown_scheduler` holdout proves one three-record actor array, three
  differently sized ability arrays, nested iteration, outer-to-inner field
  dependencies, and mutation of both parent and child records. Generated Go,
  the prepared owner, generic Machine, and the independent interpreter agree
  across negative, ordinary, and large seeds. Child-array or child-record
  escape, child identity replacement, shape mismatch, shared child aliases,
  and unsupported selector uses fail closed. Controlled execution stays on
  generic Machine; invalid prepared arguments replay entry; owner table/string
  counts remain unchanged; direct and prepared paths allocate zero when
  warmed. The direct kernel has a five-sample median of about 7.404
  microseconds (7.396-7.501 microseconds observed), while the prepared owner
  path has a median of about 6.932 microseconds (6.879-7.100 microseconds
  observed), versus about 620.9 microseconds through generic Machine
  (620.1-628.9 microseconds observed). The pinned Luau CLI warmed to a
  five-sample median of about 41.43 microseconds per call after the cold first
  process sample, making prepared about `0.167x` Luau. Deterministic generated
  source is 35,764 bytes. Linked ARM64 is 3,888 bytes for the direct kernel,
  4,496 bytes for the prepared body, and 128 bytes for the wrapper; generated
  bodies contain only cold bounds/stack-growth calls and no opcode,
  descriptor, Machine table, runtime string, interning, or VM dispatch.
- The mixed scalarizer and fused child-record slice lets record lowering and
  the existing fixed scalar-array lowering own disjoint table roots in one
  Proto. Uniform nonescaping child records stored in parent record arrays use
  small numeric selectors, and `GET_STRING_FIELD_INDEX` becomes guarded
  switches over the parent slot and finite string ID. A parameterized
  `save_state_diff` holdout proves two three-record arrays, six nested inventory
  records, one independent three-string key array, observed outer iterator
  indices, finite-string comparisons, and two fused child lookups. Generated
  Go, the prepared owner, generic Machine, and the independent interpreter
  agree across negative, ordinary, and large seeds. Heterogeneous finite child
  shapes now carry explicit typed presence state; an absent field replays
  canonical entry at the first operation that requires its payload. Child
  escape, mixed child-field kinds, and mixed dynamic-key kinds fail closed;
  unknown finite keys replay canonical entry rather than fabricating nil.
  Controlled execution stays on generic Machine; invalid prepared arguments
  replay entry; owner table/string counts remain unchanged; direct and
  prepared paths allocate zero when warmed. The direct kernel has a
  five-sample median of about 4.149 microseconds (4.142-4.204 microseconds
  observed), while the prepared owner path has a median of about 4.259
  microseconds (4.250-4.269 microseconds observed), versus about 515.2
  microseconds through generic Machine (514.5-529.3 microseconds observed).
  The pinned Luau CLI has a five-sample median of about 52.09 microseconds per
  call, including the cold first process sample, making prepared about
  `0.082x` Luau. Deterministic generated source is 21,842 bytes. Linked ARM64
  is 1,216 bytes for the direct kernel, 1,344 bytes for the prepared body, and
  128 bytes for the wrapper; generated bodies contain only cold
  bounds/stack-growth calls and no opcode, descriptor, Machine table, runtime
  string, interning, or VM dispatch.
- The fused child-record mutation slice extends the same verified selector
  model to `SET_STRING_FIELD_INDEX`. A dynamic finite string ID selects one
  uniformly typed field in one nonescaping child record, and generated Go
  writes the typed scalar local directly. Unknown parent selectors or field
  IDs replay canonical entry; mixed child-field kinds, kind-changing writes,
  child escape, unsupported bases, and unresolved keys fail closed. A
  parameterized `threat_aggro_table` holdout proves three fixed record arrays,
  three nested threat records, two fused dynamic reads, one fused dynamic
  write, finite-string decisions, and mutation inside nested loops. Generated
  Go, the prepared owner, generic Machine, and the independent interpreter
  agree across negative, ordinary, and large seeds. Controlled execution stays
  on generic Machine; invalid prepared arguments replay entry; owner
  table/string counts remain unchanged; direct and prepared paths allocate
  zero when warmed. The direct kernel has a five-sample median of about 6.464
  microseconds (6.459-6.513 microseconds observed), while the prepared owner
  path has a median of about 6.572 microseconds (6.560-6.593 microseconds
  observed), versus about 1.008 milliseconds through generic Machine
  (1.006-1.011 milliseconds observed). The pinned Luau CLI warmed to a
  five-sample median of about 66.21 microseconds per call after the cold first
  sample, making prepared about `0.099x` Luau. Deterministic generated source
  is 30,048 bytes. Linked ARM64 is 2,560 bytes for the direct kernel and 2,800
  bytes for the prepared body; both contain only cold bounds/stack-growth
  calls and no opcode, descriptor, Machine table, runtime string, interning,
  or VM dispatch. Twelve of the 25 Scenario kernels now emit through the
  single-Proto proof route; the six nested-Proto cases require the direct-call
  route before they can be classified by the same observer.
- The explicit child-record selector slice lets one verified
  `GET_STRING_FIELD` carry the parent member as a compiler-private numeric
  reference into subsequent ordinary field reads or same-kind writes. The
  selector propagates only through proved SSA moves/phis and remains tied to
  one finite child family; it never materializes a table or crosses the owner
  seam. Invalid selectors replay entry, while child escape, identity changes,
  mixed field kinds, kind-changing writes, and unresolved uses fail closed;
  heterogeneous finite shapes use typed presence state and replay before a
  missing payload is consumed. A parameterized `economy_market_tick` holdout proves one
  three-market array, nine nested stock/demand/price records, one four-order
  array, six fused dynamic reads, three fused dynamic writes, three explicit
  child-selector field reads, and guarded `math.min`. Generated Go, prepared
  owner, generic Machine, and the independent interpreter agree across
  negative, ordinary, and large seeds. Controlled execution remains generic;
  invalid prepared arguments replay entry; owner table/string counts remain
  unchanged; direct and prepared paths allocate zero when warmed. The direct
  kernel has a five-sample median of about 9.263 microseconds
  (9.256-9.343 microseconds observed), while the prepared owner path has a
  median of about 9.439 microseconds (9.422-9.486 microseconds observed),
  versus about 970.4 microseconds through generic Machine
  (969.0-972.4 microseconds observed). The pinned Luau CLI has a five-sample
  median of about 71.91 microseconds per call, including the cold first sample,
  making prepared about `0.131x` Luau. Deterministic generated source is
  40,836 bytes after the optional-field representation. Linked ARM64 at the
  original selector checkpoint was 4,288 bytes for the direct kernel and 4,688
  bytes for the prepared body; each calls Go's supported `math.archMin` helper
  and has only cold bounds/stack-growth helpers otherwise, with no opcode,
  descriptor, Machine table, runtime string, interning, or VM dispatch.
  Thirteen of the 25 Scenario kernels now emit through the single-Proto proof
  route.
- The guarded union-record slice lets a fixed record array contain a finite
  set of heterogeneous shapes. Each union field carries a deterministic
  per-member presence mask; generated reads and same-kind writes propagate a
  payload local plus a presence bit through SSA, so an absent field replays
  canonical Machine when an operation requires the payload rather than
  fabricating zero or changing shape. Uniform
  finite-string indexing over one standalone scalarized record now lowers to
  a direct switch over verified image string IDs; unknown keys replay entry.
  A parameterized `behavior_tree_tick` holdout proves five condition/action
  records with seven union fields, computed record selection, four guarded
  condition-only fields, two guarded action-only fields, and dynamic
  `blackboard[node.key]` lookup across five numeric fields. Generated Go,
  prepared owner, generic Machine, and the independent interpreter agree
  across negative, ordinary, and large seeds. A deliberate variant mismatch
  reaches the generated guard and the independent interpreter's canonical
  missing-field error. Mixed union-field kinds, mixed dynamic-field kinds,
  record-array escape, unknown keys, and shape-changing member access fail
  closed. Private-function renaming produces byte-identical executable
  source; controlled execution remains generic; invalid prepared arguments
  replay entry; owner table/string counts remain unchanged; direct and
  prepared paths allocate zero when warmed. The direct kernel has a
  current three-sample median of about 917.3 ns (917.0-918.8 ns observed), while the
  prepared owner path has a median of about 974.2 ns (972.6-975.9 ns
  observed), versus about 188.8 microseconds through generic Machine
  (188.3-189.0 microseconds observed). Pinned Luau `-O2 -g0` produced the same
  batch checksum with a warmed five-sample median of about 16.82 microseconds
  per call (16.79-17.89 microseconds observed), making prepared about `0.058x`
  Luau. Deterministic generated source is 22,398 bytes. Linked ARM64 at the
  original guarded-union checkpoint was 1,328 bytes for the direct kernel and
  1,472 bytes for the prepared body; each has
  only cold bounds/stack-growth helpers and no opcode, descriptor, Machine
  table, runtime string, interning, or VM dispatch. Fourteen of the 25
  Scenario kernels now emit through the single-Proto proof route.
- The typed optional-union slice represents `nil|bool`, `nil|number`, and
  `nil|string` SSA values as ordinary typed payload locals plus explicit
  presence bits. Presence propagates through moves and phi edges, equality and
  truthiness observe nil exactly, and payload-consuming arithmetic, lookup,
  comparison, and intrinsic operations replay canonical Machine if absent.
  Fixed heterogeneous record arrays use parallel presence arrays; standalone
  nested records and heterogeneous child families use presence locals. A
  bounded structural string-domain analysis follows constants, moves, phis,
  and record fields, expanding dynamic child-record switches only from proved
  image string IDs. A parameterized `dialogue_condition_eval` holdout proves
  two heterogeneous check shapes, optional strings/numbers/bools, one
  standalone flags child record, finite dynamic reads and insertions, four
  late-observed flags, and nested iteration. Generated Go, prepared owner,
  generic Machine, and the independent interpreter agree across negative,
  ordinary, and large seeds. Private-function renaming is byte-identical;
  mixed optional payloads and escaping child records fail closed; controlled
  execution stays generic; invalid prepared arguments replay entry; owner
  table/string counts remain unchanged; direct and prepared paths allocate
  zero when warmed. The direct kernel has a five-sample median of about 2.910
  microseconds (2.909-2.913 microseconds observed), while the prepared owner
  has a median of about 3.013 microseconds (3.011-3.032 microseconds observed),
  versus about 325 microseconds through generic Machine. Exploratory pinned
  Luau `-O2 -g0` batch measurements warmed to a five-sample median of about
  22.49 microseconds per call after the cold first process sample, making
  prepared about `0.134x` Luau; this is not a replacement for the required
  `guest_batch_v1` capture. Deterministic generated source is 33,418 bytes.
  Linked ARM64 is 3,232 bytes for the direct kernel, 3,536 bytes for the
  prepared body, and 128 bytes for the wrapper; generated bodies call only
  cold bounds/stack-growth helpers and contain no opcode, descriptor, Machine
  table, runtime string, interning, or VM dispatch. Fifteen of the 25 Scenario
  kernels now emit through the single-Proto proof route.
- The scalar string-array and optional-deletion slice lets one immutable,
  dense, bounded scalar array feed a dynamic numeric lookup. Generated code
  guards integer range before indexing its ordinary Go array, while finite
  string-domain analysis follows the lookup back to all dominating constant
  elements and expands only those verified image string IDs. This composes
  with typed child-record presence: dynamic numeric component insertion sets
  payload and presence, ordinary updates preserve presence, and assigning nil
  clears presence without fabricating a zero payload. A parameterized
  `component_churn` holdout proves a five-string key array, four heterogeneous
  component records, four dynamic reads, three dynamic writes, insertion,
  update, deletion, late static optional reads, and nested iteration.
  Generated Go, prepared owner, generic Machine, and the independent
  interpreter agree across negative, ordinary, and large seeds. Private
  function renaming is byte-identical; mixed scalar-array elements,
  multi-payload components, and escaping component records fail closed;
  controlled execution stays generic; invalid prepared arguments replay
  entry; owner table/string counts remain unchanged; direct and prepared paths
  allocate zero when warmed. The direct kernel has a five-sample median of
  about 4.575 microseconds (4.563-4.633 microseconds observed), while the
  prepared owner has a median of about 4.716 microseconds (4.712-4.733
  microseconds observed), versus about 504.6 microseconds through generic
  Machine. The corresponding pinned Luau `-O2 -g0` corpus batch has a
  five-sample median of about 42.24 microseconds per call, including its cold
  first process sample, making prepared about `0.112x` Luau; this is
  exploratory evidence, not the required `guest_batch_v1` capture.
  Deterministic generated source is 53,325 bytes. Linked ARM64 is 7,776 bytes
  for the direct kernel, 8,384 bytes for the prepared body, and 192 bytes for
  the wrapper; generated bodies call only cold bounds/stack-growth helpers and
  contain no opcode, descriptor, Machine table, runtime string, interning, or
  VM dispatch. Sixteen of the 25 Scenario kernels now emit through the
  compiler proof route.
- The empty nested record-array slice carries a complete zero-length shape for
  every member of a verified child-array family. Empty members now retain the
  family's field inventory and zero-length presence vectors, so declarations,
  guards, selectors, and field tags remain structurally valid without
  inventing an element. A parameterized `path_relaxation` holdout proves nine
  outer node records, ten distinct nested edge arrays, one genuinely empty
  member, twelve edge records, dynamic next-node selection, nested iteration,
  and cross-record mutation. Generated Go, prepared owner, generic Machine,
  and the independent interpreter agree across negative, ordinary, and large
  seeds. Private function renaming is byte-identical; mixed edge payloads,
  child shape drift, and escaping edge arrays fail closed; controlled
  execution stays generic; invalid prepared arguments replay entry; owner
  table/string counts remain unchanged; direct and prepared paths allocate
  zero when warmed. The direct kernel has a five-sample median of about 4.443
  microseconds (4.440-4.449 microseconds observed), while the prepared owner
  has a median of about 4.718 microseconds (4.706-4.727 microseconds observed),
  versus about 455.3 microseconds through generic Machine. The corresponding
  pinned Luau `-O2 -g0` corpus batch has a five-sample median of about 30.51
  microseconds per call, including its cold first process sample, making
  prepared about `0.155x` Luau; this is exploratory evidence, not the required
  `guest_batch_v1` capture. Deterministic generated source is 36,038 bytes.
  Linked ARM64 is 2,400 bytes for the direct kernel, 2,720 bytes for the
  prepared body, and 128 bytes for the wrapper; generated bodies call only
  cold bounds/stack-growth helpers and contain no opcode, descriptor, Machine
  table, runtime string, interning, or VM dispatch. Seventeen of the 25
  Scenario kernels now emit through the compiler proof route.

- The bounded nested record-array removal slice gives each mutable child-array
  family member a runtime length and lowers verified indexed selection,
  `rawlen`, and unobserved `table.remove` to scalar guards plus in-place field
  compaction. Every parallel field and optional-presence array shifts together;
  encoded element references are range-checked against the selected member's
  current length. A parameterized `buff_stack_tick` holdout proves three
  heterogeneous entities, three independently shrinking buff arrays, four
  string-dispatched buff kinds, nested mutation, repeated removal, and two
  runtime length observations. Generated Go, prepared owner, generic Machine,
  and the independent interpreter agree across negative, ordinary, and large
  seeds. Private-function renaming is byte-identical; mixed buff payloads,
  escaping buff arrays, and observing the removed record fail closed;
  rebinding either `table.remove` or `rawlen` replays canonical Machine before
  mutation; controlled execution remains generic; invalid prepared arguments
  replay entry; owner table/string counts remain unchanged; direct and
  prepared paths allocate zero when warmed. The direct kernel has a five-sample
  median of about 573.1 ns (564.5-575.5 ns observed), while the prepared owner
  has a median of about 718.9 ns (710.0-846.2 ns observed), versus about 89.83
  microseconds through generic Machine. The corresponding pinned Luau `-O2
  -g0` corpus batch has a five-sample median of about 10.46 microseconds per
  call, including its cold first process sample, making prepared about `0.069x`
  Luau; this is exploratory evidence, not the required `guest_batch_v1`
  capture. Deterministic generated source is 31,354 bytes. Linked ARM64 is
  3,552 bytes for the direct kernel, 4,000 bytes for the prepared body, and 288
  bytes for the wrapper; generated bodies contain no opcode, descriptor,
  Machine table, runtime string, interning, or VM dispatch. Eighteen of the 25
  Scenario kernels now emit through the compiler proof route.

- The finite string-table slice replaces the earlier flattened transition
  proof with the real nested `state_machine_transitions` shape. A dynamically
  selected key in one proved standalone record now yields a compact selector
  into one five-member child-record family; the following dynamic key reads a
  typed optional field from that selected member. Missing transition fields
  propagate explicit nil presence, while unknown outer states, mixed payload
  domains, mixed event arrays, selected-child escape, and graph escape fail
  closed. The ordinary numeric weights record and the independent seven-string
  event array compose with the same lowering without a map, allocation,
  runtime string, or dispatch operation. Generated Go, prepared owner, generic
  Machine, and the independent interpreter agree across negative, ordinary,
  and large seeds. Private-function renaming is byte-identical; rebinding
  `rawlen` replays canonical Machine before work; controlled execution remains
  generic; invalid prepared arguments replay entry; owner table/string counts
  remain unchanged; direct and prepared paths allocate zero when warmed. The
  direct kernel has a five-sample median of about 763.0 ns (762.7-860.6 ns
  observed), while the prepared owner has a median of about 889.2 ns
  (880.6-902.6 ns observed), versus about 71.57 microseconds through generic
  Machine. The corresponding pinned Luau `-O2 -g0` corpus batch has a
  five-sample median of about 14.04 microseconds per call, including its cold
  first process sample, making prepared about `0.063x` Luau; this is
  exploratory evidence, not the required `guest_batch_v1` capture.
  Deterministic generated source is 25,999 bytes. Linked ARM64 is 1,872 bytes
  for the direct kernel, 2,000 bytes for the prepared body, and 192 bytes for
  the wrapper; generated bodies contain no opcode, descriptor, Machine table,
  runtime string, interning, or VM dispatch. This strengthens an already
  counted Scenario route, so coverage remains eighteen of 25 rather than
  double-counting the same state-machine case.

This is proof of the selected architecture and one required call shape, not
proof of P2 coverage, the representative private gate, a public API, or final
all-37 parity. General non-dense iteration, general table mutation, general
generated strings, unbounded or polymorphic dynamic string-key maps,
multi-payload unions, escaping strings,
general closures/upvalues and dynamic call sets, open varargs/results,
heterogeneous or dynamically shaped multiple results,
general recursion and logical-frame/error cases, dynamic/callable metatables,
general captured or polymorphic methods, host/effect exits, modules, and
escaping, polymorphic, resumed-value-consuming, or otherwise general
coroutines remain on the active path.

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
