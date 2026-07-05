# Plan: Compiler And Runtime Optimization

## Goal

Grow Ember from the current correct vertical runtime slice into a full,
optimized Luau-compatible implementation without widening the root public
surface prematurely.

The slice is done when Ember has:

- a shared Source pipeline for parsing, compilation, and Typed Analysis;
- a diagnostics lane that can recover, collect warnings, and report precise
  source ranges without coupling diagnostics to one traversal;
- a private module resolver for `require` graphs, cycle handling, and
  per-module artifact caches;
- a deeper compiler pipeline with explicit binding, lowering, register
  allocation, optimization passes, and bytecode finalization;
- verified bytecode that the VM can execute without hot-loop defensive work;
- a VM frame, call, coroutine, yield, debug-hook, and execution-limit model that
  implements full Luau-compatible coroutine behavior, including resumable script
  calls, yieldable protected-call and host-call paths, non-yieldable runtime
  operation errors, coroutine standard-library behavior, and host interruption
  semantics;
- runtime table storage that can scale beyond the seed map-only model;
- source-to-result, bytecode, and compatibility tests that prove the behavior
  claimed at each step.

The public shape should remain simple for the seed:

```go
proto, err := ember.Compile(source)
results, err := ember.Run(proto)

analyzer := ember.NewAnalyzer()
artifact, err := analyzer.Check(ctx, ember.Source{Name: name, Text: source})
```

Phase 3 triggers a richer compile result before parser recovery or warnings
land. `Compile` remains a compatibility wrapper, but the diagnostics-aware seam
becomes:

```go
result, err := ember.CompileSource(ctx, ember.Source{Name: name, Text: source})
if err != nil {
	// cancellation, invalid source encoding, hard budget, or internal failure
}
if result.Proto == nil {
	// blocked by source diagnostics
}
```

```go
type CompileResult struct {
	Source      Source
	Proto       *Proto
	Diagnostics []Diagnostic
}
```

`Compile(source string) (*Proto, error)` should call `CompileSource` and convert
blocking diagnostics into a compact `CompileError` for older callers. New
callers should inspect structured diagnostics instead of parsing error strings.

## Current Pressure

The current root package is correct for the seed, but several modules are now
shallow enough that new behavior will keep spreading complexity:

- `parser.go` owns scanning, comments, directives, statement grammar,
  expression precedence, Type Syntax, and source ranges.
- `emitter.go` lowers directly from syntax to bytecode and allocates registers
  monotonically for a whole prototype.
- `bytecode.go` has an internal instruction shape, but no verifier,
  disassembler, debug metadata, or compact encoding path.
- `vm.go` validates operands in the hot loop and recursively calls `runProto`
  for script functions.
- `value.go`, `table_ops.go`, and `raw_sequence.go` keep tables simple and
  deterministic, but the map-only table model makes raw length, iteration, and
  sequence operations expensive.
- `analysis.go` is a useful foothold, but the future Typed Analysis pipeline
  needs shared parse and bind output rather than ad hoc syntax walks.
- Diagnostics currently ride along with whichever phase notices a problem.
  Error recovery, warnings, related spans, and source-range reporting need one
  visible lane before parser and analyzer behavior grow.
- There is no module-resolution owner yet. Luau-shaped `require` support will
  need a graph, cycle policy, and per-module compile/check cache rather than
  ad hoc lookups from compiler or analyzer code.

These are in-process dependencies. They can be deepened behind internal seams
and tested through source-to-result behavior, bytecode fixtures, and typed
artifact diagnostics.

## Scope

- In: private Source pipeline work.
- In: private syntax, diagnostics, bind, module resolver, compile, bytecode,
  VM, table, and Typed Analysis module deepening.
- In: bytecode verifier and disassembler for tests and diagnostics.
- In: register allocation, constant folding, dead code elimination,
  local/upvalue access optimization, import/global fast paths, peephole
  optimization, and VM frame cleanup when tests and benchmarks justify them.
- In: full coroutine and yield support, including `coroutine.create`,
  `coroutine.resume`, `coroutine.yield`, `coroutine.status`,
  `coroutine.running`, `coroutine.wrap`, `coroutine.isyieldable`, and
  `coroutine.close`.
- In: resumable VM frames, resumable script calls, yielded value lists, resume
  arguments, coroutine error propagation, and dead-coroutine behavior.
- In: yield behavior through nested script calls, yieldable protected-call
  targets, opt-in yieldable host calls, and non-yieldable runtime operations
  such as metamethod calls where Luau prohibits yielding.
- In: host-function yield policy, host interruption policy, debug-hook
  behavior, and instruction-budget behavior.
- In: source-to-result tests, bytecode tests, typed-analysis tests, and
  benchmarks for the optimized paths.
- Out: widening root public interfaces without a proven caller.
- Out: Hearth integration in the root package.
- Out: native codegen, custom GC, and unsafe code.
- Out: goroutine-backed coroutine machinery. Coroutines are implemented through
  explicit VM frames and resumable interpreter state, not Go goroutine stacks.

## Release Milestones

The nineteen phases should land as smaller checkpoints. A milestone is not
complete until its compatibility target, diagnostics behavior, benchmarks, and
known failures are documented.

- **Milestone 1: Source, diagnostics, and bind** covers Phases 1-4. It pins the
  first compatibility target, adds bytecode observability, lexer ranges,
  recovery-capable diagnostics, shared bind output, and early Analyzer.Check
  integration with source, syntax, bind output, and diagnostics.
- **Milestone 2: Modules and HIR** covers Phases 5-6. It defines module
  identity, `require` graph semantics, resolver adapters, cyclic-module policy,
  cache ownership, and HIR lowering before bytecode-shaped decisions.
- **Milestone 3: MIR, allocation, and verification** covers Phases 7-10. It
  splits optimizations by IR level, adds an optimization disable/debug mode,
  lowers to MIR / Bytecode IR, adds liveness-based register allocation, and
  makes the verifier trust boundary explicit.
- **Milestone 4: VM frames, coroutines, yielding, debug hooks, runtime
  behavior, and tables** covers Phases 11-18. It defines explicit VM frames,
  resumable call states, coroutine state, yield boundaries, yieldable script
  calls, yieldable protected-call and host-call paths, explicit non-yieldable
  metamethod behavior, protected-call interaction, debug hooks, instruction
  budgets, runtime operations, and scalable table storage.
- **Milestone 5: Full Typed Analysis** covers Phase 19. It builds on the
  earlier source, bind, diagnostics, and module-summary integration, then adds
  the hard solver work: type packs, generic functions, table shapes, metatable
  types, mode policy, and union/intersection normalization.

## Compatibility Target And Tests

Target Roblox production-shaped Luau behavior as of 2026-07-04. The language
specification baseline is **Lua 5.1**, not Lua 5.2 or Lua 5.3. Where exact Lua
compatibility matters, use Lua 5.1.4 behavior as the comparison point, then
layer Luau's Roblox-documented extensions on top.

Use Roblox Creator Hub Luau behavior as the semantic target. Use upstream
`luau-lang/luau` release `0.728`, tag commit
`ddcea05e1cc6f534e5eaac33325690c12f1ed274`, only as the imported fixture
snapshot for public Luau tests. Roblox can ship platform behavior ahead of or
beside the public repository, so Creator Hub behavior wins when it conflicts
with standalone CLI behavior.

The target includes Luau's adopted Lua 5.4-style `coroutine.close`; Ember
implements it rather than treating it as optional target drift. `close` accepts
dead or suspended coroutines, moves suspended coroutines to dead, returns true
for already-dead coroutines, returns false plus the stored error for
error-stopped coroutines, and rejects currently running coroutines. The target
also supports yieldable `pcall`/`xpcall` target functions, prohibits yielding
from `xpcall` error handlers and metamethods, and treats debug-hook and
instruction-budget interruption as host control flow rather than
language-level coroutine yield.

Initial support categories:

- **Supported**: behavior already listed in the current README seed and covered
  by Ember source-to-result tests.
- **Partially supported**: parser completeness, standard libraries, module
  loading, coroutine/yield behavior, typed analysis, and optimizer behavior.
- **Intentionally unsupported in this plan**: Roblox DataModel APIs, Hearth
  integration in the root package, native codegen, custom GC, JIT/inliner work,
  and parallel Luau execution.
- **Not yet tested**: any upstream or Creator Hub category without a named Ember
  fixture.

The compatibility target is part of cache keys and benchmark baselines. Updating
the public Luau fixture snapshot from `0.728` requires a plan edit that lists
changed categories, imported tests, new known failures, and benchmark baseline
updates.

Upstream tests should be imported and categorized by surface:

- parser tests;
- runtime behavior tests;
- bytecode shape tests where Ember claims a shape;
- type analysis tests;
- conformance tests that span multiple surfaces;
- known failures with the reason, target category, and owner phase.

Error-message compatibility is a deliberate policy, not an accident. Each
category should state whether Ember matches upstream wording exactly, matches an
error category and span only, or intentionally uses Go-native wording. Exact
wording is expensive and should be required only where callers or conformance
fixtures prove it matters.

## Runtime Behavior Matrix

Before VM, runtime-operation, or table-storage rewrites, maintain a matrix of
claimed behavior and tests for:

- `__index`, `__newindex`, `__call`, `__len`, `__eq`, arithmetic metamethods,
  and concat/comparison metamethods;
- `rawget`, `rawset`, `rawlen`, table iteration, table length, callable tables,
  and cyclic metamethod chains;
- value-list adjustment, varargs, multiple-return calls, host callbacks, and
  error messages.

The Runtime Behavior Matrix must also cover:

- `coroutine.create`;
- `coroutine.resume`;
- `coroutine.yield`;
- `coroutine.status`;
- `coroutine.running`;
- `coroutine.wrap`;
- `coroutine.isyieldable`;
- `coroutine.close`;
- yield outside coroutine;
- resume dead coroutine;
- multiple yielded values;
- multiple returned values from coroutine completion;
- resume arguments after yield;
- errors inside coroutines;
- nested coroutine resume;
- `coroutine.wrap` error propagation;
- status during nested resume;
- yielding through nested script calls;
- yielding through `pcall` and `xpcall` target functions;
- rejected yield from `xpcall` error handlers;
- rejected yield from metamethods;
- host-function yield policy;
- debug-hook interaction;
- instruction-budget interaction.

Use this table shape:

```text
Feature | Target behavior | Ember support | Fixture | Error policy | Known deviation
```

Each matrix row should name the target behavior, current Ember support level,
test fixture, error-message policy, and any known deviation.

## Cache And Artifact Keys

Per-module syntax, bind, proto, summary, and typed artifact caches need stable
keys before module resolution becomes shared infrastructure. Cache keys should
include:

- source identity and source hash;
- compiler options and optimization/debug mode;
- analyzer mode, including strict, nonstrict, or nocheck;
- dependency versions and module summary hashes;
- resolver configuration and host loader identity;
- Luau compatibility target;
- public environment or type-environment version when it affects results.

The module resolver owns cache invalidation facts. Compiler and analyzer phases
may consume cached artifacts, but they should not know cache storage details.

## Module Cycle Policy

The first resolver treats cycles as errors, not partial artifacts.

- A module that is already loading and is required again produces a
  `module-cycle` diagnostic with the cycle path when known.
- Graph compilation treats a `module-cycle` diagnostic as compile-blocking and
  emits no `Proto` for the cyclic graph.
- Runtime `require` mirrors Roblox-shaped recursive require behavior: an
  already-loaded module returns its cached value, but an active-loading module
  required recursively fails instead of exposing a partial value.
- Analyzer checks may continue after reporting the cycle, but summaries for
  cyclic modules are marked untrusted and do not export usable type facts.
- Cyclic artifacts are not cached as successful syntax, bind, proto, summary,
  or typed artifact results. Only the diagnostic and dependency facts may be
  cached for invalidation.

## Performance Gates

Benchmarks should have pass/fail expectations before they justify
optimization. A phase can use thresholds such as:

- fewer allocations or no allocation regression beyond a documented percentage;
- smaller register count for named fixtures;
- faster table reads, writes, iteration, and raw length;
- faster script calls, host calls, and metamethod calls;
- reduced frame, cell, temporary slice, call-stack, or constant-pool growth.

When a phase cannot set an exact numeric threshold yet, it should state the
baseline fixture and the regression budget it is protecting.

## Design

The desired shape is a small external seam with deep internal modules:

```text
Source
  |
  v
Lexer
  - token stream
  - comments
  - directives
  - ranges
  |
  v
Parser
  - Syntax
  - Type Syntax
  - no name resolution
  - recovery points
  |
  v
Binder
  - symbols
  - scopes
  - locals, globals, upvalues
  - type binders and aliases
  |
  v
Module Resolver
  - require graph
  - cyclic modules
  - per-module typed artifact cache
  |
  +--> Typed Analysis
  |     - lower Type Syntax
  |     - type packs
  |     - generic functions
  |     - table shapes
  |     - metatable types
  |     - strict/nonstrict modes
  |     - union/intersection normalization
  |     - build Typed Artifact
  |
  v
Compiler Orchestrator
  |
  v
HIR
  - value-list semantics
  - closures
  - CFG and loop lowering
  |
  v
Optimization Passes
  - HIR semantic simplification
  - MIR access fast paths
  - bytecode peepholes
  - VM/runtime fast paths
  - disable/debug mode
  |
  v
MIR / Bytecode IR
  - instruction selection
  - local/upvalue access optimization
  - import/global fast paths
  |
  v
Register Allocation
  |
  v
Assembly
  |
  v
Proto Finalizer
  - verifier
  - constant pool
  - debug line table
  - disassembly support
  |
  v
VM
  - verified hot loop
  - explicit frames
  - resumable call stack
  - coroutine/thread state
  - yield and resume machinery
  - protected-call boundaries
  - debug hooks
  - instruction budgets
  - runtime operations
  |
  v
Runtime Operations
  - callability
  - metamethod lookup
  - table access
  - arithmetic
  - comparison
  - length
  - iteration
  - yield policy states for calls that invoke user code
  - non-yieldable operation errors where Luau prohibits yield
  |
  v
Runtime Behavior

Diagnostics is a cross-cutting internal module fed by lexer, parser, binder,
module resolver, compiler, and Typed Analysis phases. It owns diagnostic codes,
severities, source spans and ranges, related spans, warnings, and recovery
state. Public callers see source-level diagnostics only, never phase-private
nodes.
```

### Example Lowering

Use small examples like this to keep HIR, MIR, and bytecode responsibilities
concrete:

```luau
local x = 1 + 2
return x + y
```

HIR owns source semantics and bind identities:

```text
block entry:
  let local#x = add(number 1, number 2)
  return add(local#x, global "y")
```

After HIR constant folding:

```text
block entry:
  let local#x = const(number 3)
  return add(local#x, global "y")
```

MIR owns bytecode-shaped operands before registers:

```text
block b0:
  v0 = const k0(number 3)
  v1 = global k1(string "y")
  v2 = add v0, v1
  return v2 count=1
```

Register allocation maps MIR values to registers:

```text
v0 -> r0
v1 -> r1
v2 -> r2
```

Assembly emits current-style Ember bytecode:

```text
constants:
  k0 = number 3
  k1 = string "y"

LOAD_CONST r0 k0(number 3)
LOAD_GLOBAL r1 k1(string "y")
ADD r2 r0 r1
RETURN r2 1
```

If a later optimization folds or rewrites more aggressively, its fixture should
show the before/after HIR, MIR, and bytecode with that optimization both enabled
and disabled.

### External Seam

The external module remains the root `ember` package. Its interface should
stay centered on Source, Proto, Value, Table, HostFunc, Compile, Run,
RunWithGlobals, NewAnalyzer, and Analyzer.Check.

Diagnostic and check-result types may be part of the analyzer result, but they
should carry source-level facts: codes, messages, severity, spans, related
spans, summaries, and tooling facts.

Compiler diagnostics and analyzer diagnostics can share the same diagnostics
module, but they have different blocking rules. Parse, bind, module-resolution,
or bytecode-generation diagnostics that make executable output incoherent block
`Proto` emission. Type diagnostics, warnings, and tooling diagnostics belong in
the analyzer result whenever analysis can continue.

`Proto` should be effectively sealed from public mutation. If a future public
constructor, decoder, or mutation path exists, every execution path must verify
that artifact before the VM trusts it.

Do not expose lexer tokens, syntax nodes, symbols, HIR, register allocation,
instruction fields, VM frames, stack slots, solver nodes, or table storage
details for convenience.

### Internal Seams

Internal seams are allowed when they concentrate real complexity:

- **Source pipeline**: parse once, share syntax and directives with Compile and
  Typed Analysis.
- **Diagnostics**: own error recovery, warning collection, source spans/ranges,
  related spans, and stable diagnostic codes across parser, compiler, module,
  and analyzer phases.
- **Binder**: own names, scopes, shadowing, globals, upvalues, and type binders.
- **Module resolver**: own logical module names, `require` graph traversal,
  cyclic-module diagnostics, and per-module syntax, bind, proto, and typed
  artifact caches. File systems and host loaders remain adapters at the edge.
- **Compiler HIR lowering**: own Luau value-list adjustment, closure shape,
  loop shape, and control-flow lowering before bytecode-level decisions.
- **Optimization passes**: own semantics-preserving transformations such as
  HIR constant folding and semantic simplification, MIR access fast paths and
  instruction-selection improvements, bytecode peephole rewrites, and VM
  dispatch/runtime fast paths after tests or benchmarks prove the behavior.
  Each optimization must be switchable off for tests, debugging, and
  disassembly comparisons.
- **MIR / Bytecode IR and assembly**: own instruction selection, operand
  conventions, register-allocation inputs, and final instruction emission.
- **Bytecode finalizer**: own instruction verification, debug metadata, and
  optional compact encoding.
- **VM execution**: own frames, resumable call stack, coroutine/thread state,
  yield/resume transitions, protected-call boundaries, debug hooks, instruction
  budgets, and instruction dispatch.
- **Runtime operations**: own callability, metamethod lookup, table access,
  arithmetic, comparison, length, iteration semantics, and resumable operation
  state for yieldable calls where the compatibility target requires it. Also
  own explicit non-yieldable operation state for metamethods and callbacks
  where Luau rejects yield.
- **Typed Analysis**: own type facts, constraints, evidence, diagnostics,
  summaries, and tooling facts, including type packs, generic functions, table
  shapes, metatable types, union/intersection normalization, and
  strict/nonstrict/nocheck mode behavior.

Each internal seam should have at least one of these proofs before it becomes a
package split:

- multiple callers, such as compiler and analyzer sharing bind output;
- a useful independent test surface, such as bytecode verifier fixtures;
- measured performance pressure, such as table storage or VM frame allocation;
- significant locality gain, such as moving all upvalue rules out of emitter
  statement lowering.

## TDD Rules For This Plan

Use vertical slices. Do not write all tests first.

For each step:

1. Add one focused failing test, fixture, or benchmark.
2. Implement the smallest behavior that makes it pass.
3. Refactor only while green.
4. Run the focused package tests.
5. Run `scripts/check-lane root` when the slice touches root runtime behavior.
6. Run `scripts/check-fast` before moving to the next larger item.
7. Run `scripts/check` before treating a phase as complete.

Prefer tests through `Compile`, `Run`, `RunWithGlobals`, and
`Analyzer.Check`. Use same-package bytecode or parser tests only when the
behavior is intentionally internal and cannot be observed through the public
interface without exposing implementation details.

Done criteria should be measurable whenever possible. Prefer:

- passes a named upstream or Ember test category;
- supports a named syntax or runtime form;
- rejects a named malformed bytecode fixture;
- reduces a register count in a named fixture from one recorded number to
  another;
- avoids a map scan or allocation in a named benchmark;
- stays within a documented regression budget.

## Phases

### Phase 1: Observability Before Optimization

Goal: make internal behavior visible to tests without expanding the public
surface.

Steps:

1. Add private bytecode disassembly helpers for tests and error messages.
2. Add a private bytecode verifier called by `newProto` or a finalization path.
3. Add focused tests for invalid instruction operands using internal fixtures.
4. Add small benchmarks for representative Source categories:
   arithmetic, table reads, function calls, loops, and metatable dispatch.

Done when:

- generated bytecode can be inspected in stable test output;
- malformed internal bytecode is rejected before execution;
- benchmark names describe user-facing Runtime Behavior.

### Phase 2: Tokenize Source

Goal: separate lexical concerns from grammar without changing accepted Runtime
Behavior.

Steps:

1. Add a private lexer that preserves byte ranges, comments, directives, and
   token text.
2. Port comment and directive recognition from the parser to the lexer.
3. Move string and number scanning into the lexer behind tests.
4. Convert parser statement and expression entry points incrementally.

Done when:

- existing Compile and Analyzer.Check behavior is unchanged;
- parser code no longer performs ad hoc whitespace and comment scanning;
- Type Syntax and runtime syntax receive source ranges from the same source.

### Phase 3: Diagnostics Lane

Goal: make parse, compile, module, and analysis problems source-friendly
without making diagnostics depend on one phase's private data structures.

Steps:

1. Add a private diagnostics collector with code, severity, primary span/range,
   related spans, and optional notes.
2. Thread diagnostics through lexer, parser, binder, compiler, module resolver,
   and analyzer entry points without exposing phase-private nodes.
3. Add parser recovery points for statements, expressions, and Type Syntax so
   `Analyzer.Check` can collect multiple diagnostics when recovery is safe.
4. Route Analyzer.Check through the shared source and diagnostics collector
   here, even though the full type solver waits until the final milestone.
5. Add warning collection for non-fatal issues once a concrete warning category
   exists.
6. Preserve ordinary Go errors for cancellation, invalid inputs, hard budgets,
   and internal consistency failures.

Done when:

- syntax and type diagnostics carry precise source ranges;
- parser recovery is tested without hiding fatal compile errors;
- early Analyzer.Check results use the same diagnostic shape as parser and
  binder failures;
- warning collection has one owner and stable ordering;
- public diagnostics do not expose syntax, solver, or bytecode internals.

### Phase 4: Bind Source

Goal: give names one owner before compiler and Typed Analysis grow further.

Progress:

- Added private bind output for lexical symbols, scopes, shadowing, captures,
  type aliases, type parameters, and type packs.
- Added type annotation reference binding for alias bodies, local annotations,
  function signatures, casts, call type arguments, table types, and `typeof`
  types without turning type-only references into runtime captures.
- Routed `Compile` and `Analyzer.Check` through the shared parse/bind source
  artifact.
- Moved compiler local reads, writes, and upvalue capture onto binder symbol
  identity for shadowed closure behavior.
- Moved analyzer bare assignment targets and simple named expression reads onto
  binder symbol identity while preserving branch refinements.
- Moved analyzer local-function facts and call checks onto binder symbol
  identity so closed or shadowed local functions do not leak by name.
- Made analyzer type alias lookup lexical so aliases declared in closed scopes
  do not leak into later annotations.
- Tightened analyzer loop-body scopes so closed loop locals do not leak into
  later type checks.

Steps:

1. Add a binder over syntax that records lexical symbols and scopes.
2. Move local/global/upvalue resolution out of emitter-only logic.
3. Represent type parameters, type packs, aliases, and function binders in the
   same bind output.
4. Teach Compile and Analyzer.Check to consume bind output privately.

Done when:

- local shadowing, upvalue capture, globals, and type alias scopes are tested
  through public behavior;
- emitter no longer owns semantic name identity;
- analyzer no longer reinvents scope facts.

### Phase 5: Resolve Modules

Goal: give `require` graphs and per-module artifacts one owner before
cross-module compilation or analysis spreads across compiler and analyzer code.

Progress:

- Added private module source identities, logical and host module keys,
  relative/logical/host require normalization, and an in-memory resolver fixture.
- Added private module graph traversal over literal `require` edges with
  deliberate cycle-path errors.
- Added a private module artifact cache for parsed and bound source artifacts
  keyed by source identity during graph traversal.
- Added private per-module proto caching on the same module artifact cache.
- Added a private module runtime for `require` that caches loaded module values
  and rejects active-loading recursive requires.
- Added private per-module typed check-result caching on the same module
  artifact cache.
- Added stable `module-cycle` diagnostics and cache rollback so rejected cyclic
  graphs do not leave partial module artifacts behind.
- Added a private module source loader interface and resolver adapter seam for
  future filesystem, package-store, and Hearth loaders.
- Added a private module graph compile result that treats `module-cycle`
  diagnostics as compile-blocking and emits no protos for cyclic graphs.
- Added a private module summary result so Typed Analysis consumers can read
  trusted acyclic summaries and untrusted cyclic summaries without accessing
  module syntax or bind internals.

Steps:

1. Define private source identity and logical module keys for resolver output.
2. Define `require` path normalization for relative, logical, and host-provided
   module names before filesystem adapters exist.
3. Add a private in-memory resolver fixture before adding host or filesystem
   adapters.
4. Specify the host loader interface and keep file systems, package stores, and
   Hearth loaders as adapters at the resolver seam.
5. Build a `require` graph from parsed and bound source.
6. Implement the module cycle policy from this plan: compile-blocking graph
   diagnostics, runtime active-loading errors, untrusted analyzer summaries, and
   no successful partial artifact caching.
7. Diagnose cyclic modules deliberately, including the cycle path when
   available.
8. Cache per-module syntax, bind, proto, summary, and typed artifact results
   with invalidation facts that do not expose private caches publicly.

Done when:

- module graph traversal is tested through compile/check entry points or
  resolver fixtures;
- cyclic modules produce stable diagnostics instead of recursion or partial
  artifacts;
- source identity, logical keys, path normalization, host loader behavior,
  cache ownership, and cycle policy are documented in the resolver tests or
  nearby docs;
- Typed Analysis can consume per-module summaries without reading another
  module's private syntax or solver state.

### Phase 6: Lower To HIR

Goal: stop emitting bytecode directly from syntax in places where Luau
semantics need global context.

Progress:

- Added a private value-list lowerer for fixed and open Luau value-list
  adjustment.
- Routed local/assignment target lists, returns, and final call arguments
  through that lowerer while preserving current bytecode behavior.
- Added a private call lowerer so receiver/self argument shape and open final
  arguments are described before bytecode emission.
- Added private pre-test and post-test loop lowering for `while` and `repeat`
  loops so their condition, body, and `continue` target shape are explicit
  before bytecode emission.
- Added private numeric `for` loop lowering for loop variable, start, limit,
  default or explicit step, body, and `continue`-to-increment shape.
- Added private generic `for` loop lowering for loop variables, iterator
  expressions, direct-iterator preparation, body, and `continue`-to-iterator
  shape.
- Added private closure lowering for anonymous functions, local functions, and
  function declarations, including method `self` injection before proto
  emission.
- Added private table literal lowering for array, named, and computed fields
  before bytecode emission.
- Added private `if` statement and expression lowering for condition and
  branch bodies/values before bytecode emission.
- Added private assignment lowering for assignment targets and fixed value-list
  adjustment before bytecode emission.
- Added private local binding lowering for local names, annotations, and fixed
  value-list adjustment before bytecode emission.
- Added private return lowering for open value-list adjustment before bytecode
  emission.
- Added private block lowering for lexical scope and nested statement body
  shape before bytecode emission.
- Added private call-statement lowering for side-effect calls with discarded
  results before bytecode emission.
- Added a private statement-level lowering entry point so statement dispatch
  consumes HIR-shaped payloads before bytecode emission.

Steps:

1. Add a private high-level representation for statements, expressions, value
   lists, calls, loops, and closures.
2. Lower syntax plus bind output into that representation.
3. Encode multiple-return adjustment, final-call expansion, varargs, and
   assignment adjustment in one place.
4. Lower loops to explicit control-flow blocks before bytecode assembly.

Done when:

- value-list behavior has one implementation path;
- loop lowering has a clear home for break, continue, and coroutine yield
  points;
- bytecode-level allocation and instruction selection are not mixed into HIR
  lowering.

### Phase 7: Add Optimization Passes

Goal: create a deliberate optimization slot without mixing semantic lowering,
instruction selection, and register allocation.

Progress:

- Added a private compiler optimization options slot before proto finalization,
  with disable-all and bytecode-peephole category controls for tests and future
  debug comparisons.
- Added a narrow HIR-simplify category that folds pure numeric literal
  arithmetic before bytecode emission and can be disabled independently for
  disassembly comparisons.
- Added a bytecode peephole pass that removes straight-line self-assignment
  move round trips while preserving disabled-mode disassembly fixtures.
- Kept bytecode peepholes conservative around control-flow bytecode until a
  later MIR/block-aware pass can rewrite jump targets safely.

Steps:

1. Add tests or benchmarks that justify each optimization category before
   implementing it.
2. Add HIR optimizations such as constant folding and semantic simplification
   only for expressions whose Luau behavior is fully known.
3. Add MIR optimizations such as local/upvalue access improvements,
   instruction-selection improvements, and import/global fast paths only after
   module/global semantics have a stable owner.
4. Add bytecode peephole rewrites after disassembly fixtures can prove the
   rewritten instruction shape.
5. Add VM dispatch or runtime fast paths only after runtime behavior matrix
   rows and benchmarks prove the claimed behavior.
6. Add an optimization disable/debug mode that can turn off all passes or one
   pass category at a time.

Done when:

- every enabled optimization has behavior tests or benchmark pressure;
- optimized HIR or MIR still disassembles to explainable bytecode;
- HIR, MIR, bytecode, and VM optimizations have separate owners and fixtures;
- disabling an optimization remains possible for debugging and fixtures.

### Phase 8: Lower To MIR / Bytecode IR

Goal: introduce a bytecode-level representation before register allocation and
assembly.

Progress:

- Added a private Bytecode IR instruction form with explicit operand
  categories for registers, constants, prototypes, upvalues, jump targets, and
  counts.
- Routed bytecode builder emission, jump patching, optimization, and final
  `Proto` construction through Bytecode IR assembly instead of compiling
  directly into final instruction slices.
- Added stable Bytecode IR disassembly fixtures before final `Proto`
  construction so bytecode-shaped decisions can be tested before finalization.
- Added Bytecode IR source metadata for compiler-emitted expression
  instructions, preserving original source ranges even when HIR simplification
  rewrites the expression before bytecode emission.
- Added private Bytecode IR block-order partitioning from entry, jump targets,
  and fallthrough after control transfers so later register allocation and
  liveness work has a real control-flow surface.

Steps:

1. Lower HIR into MIR / Bytecode IR with explicit operands, block order, and
   source metadata.
2. Keep instruction selection separate from final instruction encoding.
3. Represent local, upvalue, constant, global, import, and jump operands
   explicitly.
4. Add fixtures that compare stable disassembly before final `Proto`
   construction.

Done when:

- HIR remains source-semantic while MIR owns bytecode-shaped decisions;
- register allocation consumes MIR rather than syntax or HIR directly;
- bytecode assembly becomes a small final encoding step.

### Phase 9: Allocate Registers Deliberately

Goal: reduce frame size and make temporary lifetimes explicit.

Progress:

- Added a named nested-expression register-count fixture that reduced the
  frame from the recorded six-register baseline to three registers while
  preserving source-to-result behavior.
- Added private Bytecode IR liveness analysis over block order, including block
  use/def sets, successor edges, and fixed-point live-in/live-out register
  facts for branch joins.
- Added a scoped temporary register allocator for ordinary binary-expression
  right operands, leaving named locals and captured locals on stable registers.
- Extended scoped temporary reuse to table literal field values and computed
  keys, reducing a named computed-field fixture from seven registers to four
  while preserving table contents.
- Claimed multi-result call and generic-for result ranges before loop bodies
  can allocate temps, keeping iterator result locals stable while table literal
  temporaries are reused.
- Claimed and reserved fixed vararg result spans so temp reuse cannot overlap
  `...` results when multiple registers are requested.
- Released branch and loop condition temporaries after their jump instruction
  consumes them, reducing a branch-then-table fixture from five registers to
  four while preserving table contents.

Steps:

1. Add tests or benchmarks that expose excessive register count for nested
   expressions, calls, branches, and loops.
2. Add liveness analysis over MIR blocks before allocating registers.
3. Track liveness across branches, loops, closures, upvalues, varargs,
   multi-return calls, and debug locals.
4. Add a scoped temporary allocator in the bytecode assembly path only as a
   local simplification on top of liveness facts.
5. Reuse expression temporaries once their values are no longer live.
6. Keep captured locals in stable cells while ordinary temporaries stay plain.

Done when:

- named register-allocation fixtures reduce register counts from recorded
  baselines without changing behavior;
- register lifetime rules are local to the compiler implementation;
- upvalue capture tests still pass.

### Phase 10: Finalize Bytecode

Goal: make `Proto` a verified executable artifact.

Progress:

- Added a private bytecode finalizer that assembles Bytecode IR, constructs a
  `Proto`, and turns verifier failures into finalization errors for
  compiler-created artifacts.
- Routed top-level and nested function compilation through the finalizer so
  invalid compiler-created prototypes fail during `Compile` instead of being
  returned for `Run` to reject later.
- Preserved the lower-level malformed-proto construction path for verifier and
  trust-boundary tests.
- Renamed the internal interpreter entry to `runVerifiedProto` and routed
  public, module, and script-call execution through the public verification or
  compiler-finalization trust boundary before dispatch.
- Removed verifier-proven `LOAD_CONST`, `MOVE`, and `NEW_TABLE` range checks
  from the hot loop while preserving malformed-proto rejection before
  execution.
- Added finalizer coverage for non-string global constants and removed
  verifier-proven `LOAD_GLOBAL` and `SET_GLOBAL` operand range/type checks from
  the hot loop while preserving undefined-global runtime errors.
- Added finalizer coverage for field constant operands and removed
  verifier-proven upvalue, field, and index operand range checks from the hot
  loop while preserving table-target and table-operation runtime errors.
- Added finalizer coverage for arithmetic register operands and removed
  verifier-proven arithmetic, unary, concat, and comparison register range
  checks from the hot loop while preserving runtime value and metamethod
  errors.
- Added finalizer coverage for call argument spans and removed verifier-proven
  vararg, iterator-prep, call, jump, and return operand range checks from the
  hot loop while preserving dynamic open-call state validation.
- Added finalizer coverage for closure upvalue descriptors and removed
  verifier-proven closure operand and capture descriptor range checks from the
  trusted VM path.

Completion evidence:

- Compiler-created top-level and nested prototypes are built through
  `finalizeProto`, which rejects verifier failures before returning a `*Proto`.
- Manually constructed malformed prototypes keep `verifyErr`, and public
  execution rejects them before host calls or bytecode dispatch.
- `Proto` fields are unexported; the remaining internal construction path
  verifies before `Run`.
- The trusted VM path has no verifier-duplicated register, constant, jump,
  prototype, upvalue, vararg, call-span, or return-span range checks.

Steps:

1. Introduce a private proto builder or finalizer.
2. Verify register ranges, constant ranges, prototype ranges, jump targets,
   upvalue descriptors, parameter counts, and variadic constraints once.
3. Move hot-loop operand checks out of VM cases where verification can prove
   safety.
4. Enforce the trust boundary: compiler-created protos are verified at
   finalization, and any future decoded or user-constructed proto is verified
   before execution.
5. Add debug metadata only when diagnostics or tests need it.

Done when:

- the VM can assume verified internal bytecode for compiler-created protos;
- malformed protos fail early with useful errors;
- `Proto` has no public mutation path that bypasses verification, or every
  bypass is verified before Run;
- root callers still only receive `*Proto`.

### Phase 11: Redesign VM Frames And Calls

Goal: build the execution model required for full Luau-compatible calls,
coroutines, yielding, protected calls, debug hooks, and execution limits.

Progress:

- Introduced private `vmThread` and `vmFrame` runtime structures so the current
  interpreter owns program counter, register cells, upvalues, varargs, globals,
  and open-call state through an explicit frame object.
- Added allocation-reporting call benchmarks, including a recursive script-call
  benchmark, to establish a baseline before replacing recursive Go script calls
  with an explicit VM call stack.
- Replaced all-register frame cells with plain `[]Value` slots plus sparse
  capture cells for locals referenced by child closures; focused call
  benchmarks in a one-iteration smoke run moved from 45 to 31 allocs/op for
  function calls and from 149 to 83 allocs/op for recursive script calls.
- Added a thread-owned frame stack for direct script `opCall` execution:
  callers pause with pending result placement, child frames run on
  `vmThread.frames`, and return values are applied when the child frame pops.
  Helper and metamethod call paths still retain their script-call fallback and
  are the next migration target.
- Routed helper, table-operation, base-library, and metamethod script calls
  through the active `vmThread` when one is available, so script closures
  reached through `callValue` also run on `vmThread.frames`; the verified-proto
  fallback remains only for calls made outside an active runtime thread.
- Replaced implicit frame-result branching with a private `vmCallState` enum
  covering returned, script-call, yielded, protected-return, and host-interrupt
  states; current execution uses returned and script-call states while the
  remaining states name the future coroutine/protected-call/debug-hook
  transitions.
- Added a private `vmSuspendedFrames` representation that detaches and restores
  the current frame stack without rebuilding frame objects, preserving program
  counters, pending result placement, register slots, captured cells, and
  open-call state for future coroutine yield/resume support.
- Added a private instruction budget on `vmThread` that produces the explicit
  host-interrupt call state when exhausted; whole-thread execution converts the
  state into a runtime error while default execution remains unlimited.
- Made remaining frame metadata explicit: each `vmFrame` now records caller,
  base register, register count, debug-line placeholder, and pending call
  result destination for future coroutine, protected-call, and debug-hook
  transitions.
- Cached captured-local metadata on `Proto` and stopped allocating capture-cell
  slices for frames whose locals cannot escape through child closures.
- Avoided fixed-argument slice allocation for script calls when the argument
  registers do not contain captured cells; host calls keep the defensive
  copied argument slice.
- Moved pending script-call metadata and script-call frame results from
  per-call heap objects into value-owned frame/result state.
- Added inline one-value script returns, so ordinary recursive calls can place a
  single returned value into the caller without allocating a result slice.
- Fixed non-capturing frame reuse so `vmFrame.reset` reuses existing register
  storage instead of allocating before checking frame capacity.

Completion evidence:

- Focused closure and upvalue fixtures still pass after ordinary slots moved
  from all-cell frames to plain `[]Value` plus sparse captured-local cells.
- Recursive script calls, including calls reached through table/metamethod
  helpers, run through `vmThread.frames` instead of recursive `runVerifiedProto`
  calls while preserving multiple-result placement.
- `vmSuspendedFrames` can detach and restore the same frame objects and resume
  to completion without rebuilding frames.
- Frame state now includes program counter, caller, register span, result
  destination, varargs, open-call state, debug-line placeholder, call state, and
  instruction-budget host interruption.
- Allocation guard fixtures keep the recursive script-call benchmark shape under
  the old high-allocation behavior, and a smaller recursive-fibonacci guard
  protects the call-heavy shape that exposed the 87k allocs/op benchmark.
- Benchmark smoke runs improved versus the original Phase 11 baselines:
  function calls moved from 45 to 27 allocs/op, recursive script calls moved
  from 149 to 59 allocs/op, and classic recursive fibonacci moved from 87,628
  allocs/op and 23.1 MB/op to 76 allocs/op and 33 KB/op in one-iteration
  benchmark smoke runs after frame, script-argument, pending-call, one-value
  return, and register-storage reuse cleanup.

Steps:

1. Replace all-register `*cell` frames with plain `[]Value` for ordinary slots.
2. Allocate cells only for captured locals and upvalues.
3. Add an explicit VM call stack so script calls do not rely on recursive Go
   calls.
4. Define a frame program counter, base register range, result destination,
   caller relationship, vararg state, open-call state, and debug line state.
5. Define the suspended-frame representation used by coroutine yield/resume.
6. Define call states for normal return, error return, yielded call,
   protected-call return, and host interrupt.
7. Keep host callbacks explicit and preserve current error wrapping.
8. Define how script calls, host calls, metamethod calls, protected calls,
   debug hooks, and instruction budgets interact with yielding and
   interruption.
9. Add baseline measurements for frame allocation, cell allocation, call-stack
   growth, and recursive script calls.

Done when:

- existing closure and upvalue behavior still passes;
- recursive script calls are represented by VM frames, not Go recursion;
- the VM can suspend and resume an execution frame without rebuilding it;
- frame state includes enough information for future coroutine, protected-call,
  debug-hook, and instruction-budget phases;
- allocation benchmarks improve or explain why they do not.

### Phase 12: Coroutine Core

Goal: implement core Luau coroutine behavior on top of explicit VM frames.

Progress:

- Added a private `vmCoroutine` object with status, owned `vmThread`, root
  closure, yielded values, resume arguments, and error state.
- Added the `coroutine` base table with initial `create`, `resume`, and
  `status` support; resuming a newly created coroutine runs the root script
  closure on the coroutine-owned VM thread and returns `true` plus the script
  return values, then marks the coroutine `dead`.
- Added `coroutine.yield` for ordinary coroutine frames: yielding stores the
  suspended VM frame stack, returns `true` plus yielded values from
  `coroutine.resume`, and a later resume applies its arguments as the return
  values of the suspended `coroutine.yield` call before continuing to
  completion.
- Preserved coroutine ownership in suspended VM frame snapshots, so repeated
  yield/resume cycles keep `coroutine.yield` inside the active coroutine
  instead of resuming with a nil coroutine owner.
- Added initial `coroutine.close` support for suspended and dead coroutine
  objects: closing clears the suspended frame stack, transitions status to
  `dead`, and prevents later resume.
- Added initial `coroutine.running` support: the main thread reports nil, and
  a running coroutine can recover its own thread object for status and identity
  checks.
- Added initial `coroutine.isyieldable` support from the active VM
  coroutine/thread state: the main thread reports false and ordinary running
  coroutine frames report true.
- Added initial `coroutine.wrap` support that creates a private coroutine,
  resumes it on each wrapper call, returns yielded/final values without the
  leading resume boolean, and reuses the same resume state machine as
  `coroutine.resume`.
- Added nested-resume status handling: a coroutine that resumes another
  coroutine reports `normal` while the child is running, then returns to its
  previous state after the child yields, returns, or errors.
- Rejected attempts to resume a `normal` coroutine before the VM can re-enter
  its active frame stack.
- Covered `coroutine.close` for both successfully returned dead coroutines and
  error-stopped dead coroutines; returned-dead close succeeds, while
  error-stopped close returns false plus the stored error.
- Covered `coroutine.wrap` error propagation through the public `Run` path:
  errors inside wrapped coroutine bodies surface as call errors instead of
  returning a resume-style false flag.

Steps:

1. Add a private coroutine/thread object that owns status, call stack, current
   frame, yielded values, resume arguments, error state, and root proto.
2. Implement coroutine states:

   - suspended;
   - running;
   - normal;
   - dead.

3. Implement:

   - `coroutine.create`;
   - `coroutine.resume`;
   - `coroutine.yield`;
   - `coroutine.status`;
   - `coroutine.running`;
   - `coroutine.wrap`;
   - `coroutine.isyieldable`;
   - `coroutine.close`.

4. Preserve multiple yielded values and multiple return values using the same
   value-list semantics as ordinary calls.
5. Make resume arguments become the return values of the suspended
   `coroutine.yield` call.
6. Handle errors inside coroutines without corrupting the parent VM state.
7. Diagnose yield outside coroutine according to the compatibility target.
8. Diagnose resume of a dead coroutine according to the compatibility target.
9. Implement `coroutine.close` for dead or suspended coroutines. Closing a
   suspended coroutine transitions it to dead, clears its stack, returns true
   unless the coroutine stopped with an error, returns false plus that stored
   error for error-stopped coroutines, and prevents future resume.
10. Implement `coroutine.isyieldable` from the active call state instead of a
    global flag.

Done when:

- basic create/resume/yield/return behavior matches compatibility fixtures;
- yielded values and resume arguments preserve value-list semantics;
- coroutine status transitions are tested;
- dead coroutine resume behavior is tested;
- `coroutine.close` behavior is tested for suspended, dead, running, and
  error-stopped coroutines;
- `coroutine.isyieldable` is tested in ordinary, protected-call, host-call,
  metamethod, and debug-hook contexts;
- errors inside coroutines return or propagate according to the target policy.

### Phase 13: Yieldable Script Calls

Goal: make yielding work through real nested script call paths.

Progress:

- Added public source-to-result fixtures proving coroutine yield/resume through
  nested script calls while preserving caller result destinations.
- Added yield-through-return-chain coverage for open multi-return call
  positions, proving yielded frames resume and propagate the final value list
  through `return inner()` / `return outer()` paths.
- Added recursive script-call yield coverage, proving repeated VM frames resume
  at the suspended instruction and bubble resume values back through callers.
- Added final-call-argument value-list coverage, proving a yielding final
  argument can later return multiple values into the enclosing call.
- Added repeated-yield aggregate coverage, proving a coroutine can yield more
  than once, remain suspended between resumes, and return the final value after
  the last resume.

Completion evidence:

- Public source-to-result tests cover yield/resume through nested script calls,
  recursive script calls, direct return-call chains, and final-argument
  value-list expansion.
- Existing VM frame tests prove suspended frame stacks preserve frame object
  identity instead of rebuilding parent/child frames during resume.
- Script calls already run through `vmThread.frames`; the recursive yield
  fixture exercises repeated VM frames rather than recursive verified-proto
  fallback calls.

Steps:

1. Make nested script calls resumable across multiple VM frames.
2. Preserve caller result destinations across yield and resume.
3. Preserve open call state for multi-return calls that yield before producing
   final values.
4. Support yielding through functions that return the result of another call.
5. Support yielding through tail-call-shaped paths if the compiler/runtime
   implements tail calls.
6. Ensure yielded frames resume at the exact suspended instruction.
7. Add tests for yield through nested script calls, recursive calls, return
   chains, and value-list-sensitive call positions.

Done when:

- yield works through nested script calls;
- resume continues at the exact suspended instruction;
- yielded values and final returned values are adjusted correctly at the caller;
- no call frame is rebuilt from scratch during resume;
- recursive yielding calls do not use Go recursion.

### Phase 14: Protected Calls And Error Boundaries

Goal: define how coroutine yield, runtime errors, and protected calls interact.

Progress:

- Added basic `pcall` support for successful target calls and runtime-error
  capture through the public source-to-result path.
- Added basic `xpcall` support for successful target calls and error-handler
  mapping of runtime errors.
- Added a private protected script-call path that truncates failed protected
  target frames back to the caller instead of clearing the whole active VM
  frame stack.
- Kept coroutine yield control flow distinct from runtime errors in protected
  script-call cleanup so language-level yield does not get converted into a
  protected-call error value.
- Added yieldable `pcall` and `xpcall` target support: target functions can
  yield, resume through the protected-call boundary, and complete with
  `true` plus target results.
- Preserved protected-call error handling after suspension: a `pcall` target
  that errors after resume returns `false` plus the error, and an `xpcall`
  target that errors after resume invokes its stored error handler.
- Rejected yield from `xpcall` error handlers by marking handler execution as
  non-yieldable; handlers observe `coroutine.isyieldable()` as false and
  `coroutine.yield` becomes an ordinary protected-call error.
- Added host-interrupt separation evidence: instruction-budget exhaustion is a
  private host-interrupt error and protected-call recovery does not convert it
  into a language-level `pcall`/`xpcall` result.

Completion evidence:

- Public fixtures cover basic `pcall` and `xpcall` success, runtime-error
  capture, and `xpcall` error-handler mapping.
- Public coroutine fixtures cover yieldable `pcall` and `xpcall` targets,
  including targets that yield and later return, and targets that yield and
  later raise a runtime error.
- Public fixtures cover `xpcall` error handlers as non-yieldable in both
  immediate-error and post-resume-error paths.
- Internal VM fixtures prove host interruption is typed separately from
  language-level coroutine yield and is not converted into protected-call
  language results.

Steps:

1. Implement or update `pcall` and `xpcall` according to the compatibility
   target.
2. Represent protected-call boundaries in the explicit VM call stack.
3. Distinguish normal return, runtime error, coroutine yield, and host
   interrupt.
4. Allow the target function passed to `pcall` or `xpcall` to yield; the entire
   coroutine yields and resumes back into the protected-call frame.
5. Preserve traceback/debug metadata across protected calls where required.
6. Ensure errors inside coroutines interact correctly with `coroutine.resume`
   and `coroutine.wrap`.
7. Reject yield from the `xpcall` error handler according to the target.
8. Keep host interruption separate from language-level coroutine yield.

Done when:

- `pcall` and `xpcall` behavior matches target fixtures;
- yielded `pcall` and `xpcall` target functions resume through the protected
  boundary correctly;
- `xpcall` error handlers cannot yield;
- coroutine errors do not corrupt protected-call state;
- `coroutine.resume` reports errors correctly;
- `coroutine.wrap` propagates errors correctly;
- host interrupts are not accidentally treated as coroutine yields.

### Phase 15: Yieldable Host Calls

Goal: define and implement the host-function yield contract without exposing VM
internals publicly.

Progress:

- Preserved public `HostFunc` as the simple non-yielding callback interface and
  added a private opt-in yieldable host callback value for VM-owned host
  operations.
- Added a private host-call result shape that can return normal values, runtime
  errors, yielded state with a continuation, or host interruption.
- Extended VM pending-call metadata so yielded host calls preserve the original
  result destination, protected-call boundary, and host continuation across
  coroutine suspension.
- Resumed yielded host calls with coroutine resume arguments and allowed host
  continuations to complete normally, raise runtime errors, yield repeatedly, or
  report host interruption.
- Kept host interruption separate from language-level protected-call results:
  `pcall` and `xpcall` propagate host interrupts instead of converting them
  into `false` plus an error string.

Completion evidence:

- Internal source-to-result fixtures prove opt-in yieldable host callbacks can
  yield values, resume with coroutine resume arguments, and return values into
  the suspended call destination.
- Internal fixtures prove host continuations can yield repeatedly and update
  the stored continuation before the next resume.
- Internal fixtures prove host continuation runtime errors stop coroutines when
  unprotected and become protected-call error results when the host call yielded
  through `pcall`.
- Internal fixtures prove immediate host interrupts and post-resume host
  interrupts bypass protected-call language results.
- Existing public host-callback fixtures continue to cover ordinary `HostFunc`
  calls without requiring callers to learn the private VM result shape.

Steps:

1. Keep ordinary `HostFunc` simple for non-yielding callbacks.
2. Add a private or future-compatible host-call result shape that can represent:

   - normal return values;
   - runtime error;
   - yielded state;
   - host interrupt.

3. Require host functions to opt into yielding deliberately. Plain Go callbacks
   must not accidentally yield.
4. Preserve current `HostFunc` behavior for existing callers.
5. Resume yielded host calls with the coroutine resume arguments. A host
   continuation may complete with values, raise a runtime error, yield again, or
   report a host interrupt.
6. Add tests for ordinary host calls, yielding host calls, host errors, and host
   interruption.
7. Keep host interruption separate from `coroutine.yield`.

Done when:

- current host callbacks still work unchanged;
- yield-capable host callbacks have an explicit internal contract;
- yielded host calls resume safely with values, errors, repeated yields, or
  host interrupts;
- host errors and host interrupts do not get mistaken for coroutine yields.

### Phase 16: Runtime Operation Yield Policy And Metamethods

Goal: make runtime operations explicit about whether calls to user code can
yield, error, or interrupt.

Progress:

- Added a private runtime-metamethod call helper that routes user-code
  metamethod calls through the shared call machinery while marking the active
  VM thread non-yieldable.
- Routed table `__index` and `__newindex`, callable-table `__call`, `__len`,
  unary and binary arithmetic metamethods, concat metamethods, equality and
  comparison metamethods, and iterator metamethod calls through that helper.
- Preserved the existing raw table paths: public table access still rejects
  function-valued metamethods, and runtime `rawget`, `rawset`, and `rawlen`
  remain direct non-metamethod operations.
- Kept cyclic metamethod detection at the existing call sites while moving the
  yieldability policy into the private call helper.

Completion evidence:

- Public coroutine fixtures prove `__len`, `__index`, `__newindex`, `__call`,
  arithmetic, concat, comparison, and `__eq` metamethod bodies observe
  `coroutine.isyieldable()` as false.
- The same fixtures prove `pcall(coroutine.yield)` inside those metamethod
  bodies returns `false` plus a string error instead of suspending the
  coroutine.
- Focused runtime regression tests cover existing raw operations, metamethod
  behavior, coroutine/protected-call behavior, and yieldable host calls after
  the helper was introduced.

Steps:

1. Route metamethod calls through the same resumable call machinery as ordinary
   calls, but mark them non-yieldable for the current Luau target.
2. Reject coroutine yield from these metamethod contexts according to the
   target:

   - `__index`;
   - `__newindex`;
   - `__call`;
   - `__len`;
   - `__eq`;
   - arithmetic metamethods;
   - concat metamethods;
   - comparison metamethods.

3. Represent runtime operations with an explicit result mode: normal values,
   runtime error, rejected yield, or host interrupt.
4. Preserve original operands, target register, and operation kind across any
   operation state that can call user code.
5. Keep cyclic metamethod detection working across operation states.
6. Add tests where metamethods attempt to yield and Ember reports the target
   non-yieldable behavior without corrupting the original table access,
   arithmetic operation, comparison, call, or length operation.
7. Keep `rawget`, `rawset`, and `rawlen` non-metamethod paths direct and
   predictable.

Done when:

- metamethod calls use the shared call machinery while remaining non-yieldable;
- attempted metamethod yields produce target-compatible errors;
- operation results or errors are written to the correct destination after user
  code returns;
- cyclic metamethod diagnostics still work;
- raw operations remain non-yielding.

### Phase 17: Debug Hooks And Instruction Budgets

Goal: support debug hooks and execution limits without corrupting coroutine or
call-stack state.

Progress:

- Kept instruction-budget exhaustion as a private `vmHostInterrupt` emitted at
  safe instruction boundaries before dispatch.
- Added a private VM debug-hook adapter with count, line, call, and return
  events. Hook callbacks run under the VM's non-yieldable guard.
- Added count hooks at the same safe instruction boundary as budget checks.
  Count hooks can report ordinary runtime errors or host interruption without
  being mistaken for coroutine yield.
- Added private final-prototype line metadata by carrying source text through
  the compiler, preserving bytecode IR source ranges across peephole
  optimization, and deriving a per-PC line table for the VM.
- Added line hooks that fire on positive source-line changes and preserve each
  frame's current debug line across coroutine suspension and resume.
- Added call and return hooks for script frame entry and exit without exposing
  a public debug API prematurely.
- Propagated debug hook configuration into coroutine threads and suspended
  frame state so hook behavior continues across coroutine resume.

Completion evidence:

- Internal VM fixtures prove instruction budgets produce host interruption and
  protected-call recovery does not convert that interruption into language
  results.
- Internal count-hook fixtures prove hooks run at instruction boundaries,
  execute non-yieldably, report runtime errors, and report host interruption.
- Internal line-hook fixtures prove source line changes are reported from final
  optimized prototypes and continue through coroutine suspension/resume.
- Internal call/return-hook fixtures prove root and nested script frame events
  are balanced while preserving normal return behavior.

Steps:

1. Add instruction-count budgeting at safe instruction boundaries.
2. Treat budget exhaustion as a host interrupt, not a runtime error, resumable
   pause, or language-level coroutine yield.
3. Keep host interruption separate from `coroutine.yield`.
4. Add debug hook support according to the compatibility target:

   - line hooks;
   - call hooks;
   - return hooks;
   - count hooks.

5. Make debug hooks non-yielding host instrumentation. A hook can report a host
   interrupt or runtime error through its adapter, but it cannot produce a
   coroutine yield.
6. Preserve debug line state across coroutine suspension and resume.
7. Ensure protected calls, `coroutine.resume`, and host interruption all report
   errors or pauses through distinct paths.

Done when:

- instruction budgets stop runaway code at safe boundaries;
- budget interruption does not look like `coroutine.yield`;
- coroutine suspension preserves debug line state;
- debug hooks are non-yielding and do not corrupt frame stacks;
- hook behavior has compatibility fixtures or documented known deviations.

### Phase 18: Deepen Runtime Operations And Table Storage

Goal: make call, table, arithmetic, metamethod, and iteration semantics easier
to optimize after coroutine/yield behavior is correct.

Progress:

- Added runtime benchmarks for array literals, `table.insert`,
  `table.remove`, `table.unpack`, raw length, string field reads, global access,
  method calls, and iteration, alongside the existing arithmetic, field,
  function-call, loop, recursive-call, and metatable baselines.
- Added a private table array part for common positive-integer sequence keys
  while keeping sparse numeric keys, string keys, table identity keys, userdata
  keys, and other hash keys in the existing hash part.
- Added contiguous sparse-key promotion: numeric hash entries move into the
  array part when earlier sequence keys are filled.
- Moved raw sequence length for common arrays onto the array part instead of
  repeated map-backed lookup scans.
- Preserved deterministic `next`/iteration ordering by merging array and hash
  keys through the existing sorted key order.
- Kept public table access and runtime table access distinct; the storage split
  sits behind `rawGet`, `rawSet`, `rawLen`, and `rawNext`.
- Fixed call-result register clobbering for calls compiled into live register
  regions: fixed-result calls now use a scratch call frame when direct lowering
  would overwrite locals or loop state before arguments are consumed.
- Closed the repeated front-removal compatibility gap for
  `table.remove(values, 1)` loops and kept the top-10 `array_ops` benchmark on
  the upstream-verified shape.
- Closed the broader coroutine aggregate compatibility gap for repeated
  yield/resume value flow and kept the top-10 `coroutine_yield` benchmark on
  the upstream-verified shape.
- Deepened `tableAccess` so raw hits and tables without metatables return
  before recursive `__index`/`__newindex` chain state is created; the public
  `Table.Get`/`Table.Set`, VM field/index operations, host globals, and
  future runtime operations keep the same narrow table-access seam.
- Added VM number-number fast paths for arithmetic, unary minus, and ordered
  comparisons while keeping numeric-string coercion, NaN comparison errors, and
  table metamethods on the existing generic runtime-operation path.

Completion evidence:

- Internal storage fixtures prove common contiguous numeric writes use the
  array part, sparse numeric keys promote when they become contiguous, and
  deterministic `rawNext` includes both array and hash keys in order.
- Public source-to-result fixtures prove `table.insert`, repeated
  `table.remove(values, 1)`, `table.unpack`, `rawlen`, direct numeric indexing,
  table clearing, and sequence behavior remain stable after the split.
- Public source-to-result fixtures prove fixed-result calls used inside binary
  expressions do not clobber live locals or numeric-for loop state.
- Public source-to-result fixtures prove repeated coroutine yield/resume cycles
  preserve coroutine ownership and final return values.
- Existing table-key fixtures continue to prove table identity keys, userdata
  keys, public key validation, raw access, and deterministic table behavior.
- A focused metatable raw-hit fixture proves existing table fields stay local
  while missing reads and writes still route through `__index` and
  `__newindex`.
- Existing arithmetic coercion, arithmetic metamethod, unary metamethod,
  relational comparison, comparison metamethod, and runtime-error fixtures
  continue to pass after the VM numeric fast path.
- A 200 ms benchmark smoke after table and numeric fast paths measured top-10
  `ember_run` cases at 59 us for `arithmetic_for`, 92 us for
  `while_branching`, 88 us for `table_fields`, 157 us for `array_ops`, 67 us
  for `method_calls`, 51 us for `metatable_index`, and 10 us for classic
  `iterative_fibonacci`, with allocations unchanged from the
  post-frame-cleanup baseline.
- Focused table/runtime/coroutine regression tests, top-10 Ember/Luau
  comparison fixtures, and the full Go suite pass after the storage and
  coroutine changes.

Steps:

1. Move runtime operations behind private functions with narrow inputs and
   well-described result modes:

   - normal values;
   - runtime error;
   - yielded state;
   - host interrupt.

2. Add benchmarks for array literals, `table.insert`, `table.remove`,
   `table.unpack`, raw length, string field reads, global access, method calls,
   and iteration.
3. Add the minimal array/hash table split once benchmarks show map-only storage
   is distorting runtime behavior or performance.
4. Cache common metamethod names or lookup paths only after behavior matrix
   rows and benchmarks justify it.
5. Keep public table access and runtime table access distinct.
6. Preserve table identity keys, userdata keys, raw access behavior, and claimed
   deterministic iteration behavior.
7. Add raw length hints or invalidation only after tests describe exact
   behavior.
8. Track table map scans, temporary slice growth, field-read cost, iteration
   cost, and allocation changes.

Done when:

- VM instruction cases are mostly operand movement plus operation calls;
- operation code can be benchmarked and changed without rewriting dispatch;
- sequence operations avoid repeated map scans for common arrays;
- string fields have a fast path;
- table identity keys and userdata keys still behave correctly;
- public `Table` behavior remains stable.

### Phase 19: Typed Analysis Integration

Goal: let Typed Analysis become full Luau-compatible analysis without touching
Runtime Behavior.

Progress:

- Kept `Compile` and `Run` independent from Typed Analysis while continuing to
  route `Analyzer.Check` through shared parse and bind artifacts.
- Added a versioned public `ModuleSummary` artifact containing source name,
  strictness mode, compatibility target, invalidation hash, exported facts, and
  trust-affecting diagnostics.
- Added public summary fact shapes for exported type aliases, named type
  references, unions/intersections, nilable types, table properties, table
  indexers, function types, generic functions, variadic packs, generic packs,
  singleton types, and `typeof` placeholders without exposing parser nodes,
  private locals, constraints, flow views, VM frames, or bytecode state.
- Built module summaries from parsed and analyzed check artifacts after
  diagnostics are produced, so module artifact caches can reuse summaries
  without reparsing or depending on runtime internals.
- Added conservative export trust evidence: exported facts carry diagnostic
  codes when module diagnostics may affect trust in the exported summary.
- Introduced a private type store that lowers exported Type Syntax into opaque
  `TypeRef` and `PackRef` facts before building public summaries, keeping
  parser nodes and analyzer internals out of the artifact interface.
- Lowered exported generic functions, variadic packs, table properties, table
  indexers, unions, intersections, nilable types, singletons, and `typeof`
  placeholders through store-owned facts.
- Added the first private relation-normalization tool: exported unions and
  intersections flatten nested relations and deduplicate repeated members under
  explicit relation budgets before summary display strings are formed.
- Added semantic intersection normalization for impossible primitive
  intersections, which collapse to `never` before tooling sees the exported
  summary.
- Added summary dependency facts for resolved module graphs: summaries can now
  carry normalized logical or host dependency keys, dependency paths, and
  dependency invalidation hashes without exposing resolver internals.
- Added the first public tooling facts: `Analyzer.Check` now returns type-alias
  facts for private and exported aliases, built from the same lowered type
  store facts that feed module summaries.
- Added source evidence to tooling type-alias facts: callers receive full alias
  and alias-name ranges without seeing parser nodes.
- Seeded the first private constraint/evidence path for Typed Analysis:
  local initialization, assignment, table literal field compatibility, return,
  and function argument compatibility can now flow through an internal
  assignable constraint, and public diagnostics point at the value span.
- Added the first pack-adjustment behaviors for typed analysis: missing local
  initializer values, missing assignment values, missing return values, and
  missing fixed function arguments are treated as nil in the assignable
  constraint path, with local-name, assignment-target, return-keyword, or
  call-site evidence for diagnostics.
- Added initial explicit cast result typing for typed analysis: casted scalar
  expressions expose their annotated result type to compatibility checks while
  staying erased from runtime execution.
- Seeded the first private operator constraint paths for typed analysis: unary
  minus, numeric binary arithmetic, concat, and ordered comparison operand
  compatibility now flow through internal unary/binary operator constraints with
  operand-span evidence; valid concat expressions infer string results and
  valid comparison/equality expressions infer boolean results for compatibility
  checks.
- Added unary result inference for `not` and `#`, allowing annotation
  compatibility to diagnose boolean/number result mismatches while still
  visiting operands for nested diagnostics.
- Seeded the first private table access constraint paths for typed analysis:
  missing known-table field reads and writes now produce `unknown-property`
  diagnostics, and existing known table field writes use index-write
  constraints with value-span evidence.
- Added first required-property checking for annotated table literals: missing
  required named fields now produce `missing-property` diagnostics at the table
  literal span.
- Extended initial flow refinements for typed analysis: `type(x) ~= "kind"`
  now narrows true and false branches by swapping the existing equality type
  guard views.
- Added initial `assert(predicate)` flow refinement for typed analysis:
  `assert(value)` narrows a local by truthiness, and
  `assert(type(value) == "kind")` reuses the existing true-branch type guard
  refinement for following statements in the current scope.
- Added the first boolean-composition flow refinement for typed analysis:
  `and`-composed conditions now apply all recognized true-branch facts for
  following `then` statements and `assert(...)` continuation analysis.
- Extended boolean-composition flow refinement for typed analysis:
  `or`-composed same-local type guards now form a narrowed true-branch union
  and apply sequential false-branch narrowing for the `else` branch.
- Added negated predicate flow refinement for typed analysis: `not value`
  swaps truthy/falsey nilable local facts, and
  `not (type(value) == "kind")` swaps grouped type-guard branch facts.
- Extended the scalar subtyping fast path for typed analysis so narrowed union
  views are accepted when every actual union member is allowed by the expected
  type.
- Added the first exported value fact: module summaries now include a public
  `return` value export for top-level module returns, with table-field and
  primitive type summaries for simple returned values.
- Added top-level local value facts for summary construction, so annotated
  locals returned by modules export their public table shapes without executing
  runtime code.
- Added simple alias resolution in the private type-store lowering path, so
  public summaries and tooling facts expose resolved facts instead of private
  alias names where a non-generic alias can be resolved.
- Added first require-summary consumption in graph summaries: modules that
  return a locally required module can reuse the dependency's trusted public
  return value fact without executing runtime code.
- Added require-summary field consumption for simple module field reads, so
  `return required.field` can reuse the required module's public returned table
  property type.
- Added require-summary field-alias consumption for top-level locals, so
  `local field = required.field; return field` can reuse the same trusted
  dependency summary without runtime execution.
- Added diagnostic trust gating for graph summaries: modules with typed
  diagnostics are marked untrusted, and their public facts are not consumed by
  requiring modules.
- Added local table-field value facts for summary construction, so modules that
  return a field from an annotated top-level local expose that field's public
  type.
- Added initial coroutine standard-library typed facts: `Analyzer.Check` can
  summarize the public `coroutine` table and infer return types for simple
  `coroutine.status`, `coroutine.resume`, `coroutine.close`, and
  `coroutine.isyieldable` calls without coupling typed analysis to VM frames.
- Exposed public function argument and return pack summaries for module summary
  facts, including exported function aliases and the variadic value-list shape
  of `coroutine.resume` and `coroutine.yield`.
- Preserved generic function type parameters and type packs through private
  lowering, so public summaries can describe generic signatures without
  depending on parser nodes.
- Moved mode policy into artifact construction: `Analyzer.Check` now returns
  typed artifacts for strict, nonstrict, and nocheck sources, while nocheck
  suppresses diagnostics without suppressing summary construction.
- Added initial metatable-shaped summary facts: public table summaries can carry
  a metatable summary, and simple `setmetatable(table, metatable)` return values
  expose both table and metatable shapes without executing runtime code.
- Added initial metatable readback summaries: simple `getmetatable(table)` calls
  can reuse attached metatable facts during module return summary construction.
- Added initial table-property evolution facts for summaries: top-level
  `localTable.field = value` and nested dot-path assignments update
  conservative local table facts before module return summaries are built.
- Added nested table-field readback for value summaries, so module returns such
  as `return localTable.stats.level` can reuse evolved table facts.
- Added the first control-flow-shaped summary merge: top-level `if` statements
  with assignments in both branches can preserve agreed table facts, including
  union facts for differing branch value summaries and nilable facts for
  one-sided branch assignments, for module return summaries.

Completion evidence:

- Public `Analyzer.Check` fixtures prove exported table type aliases produce a
  versioned summary with source identity, strictness mode, invalidation hash,
  type parameters, type packs, table properties, and table indexers.
- Public fixtures prove private aliases are not exported in summaries.
- Public fixtures prove diagnostics are preserved both in `CheckResult` and in
  `ModuleSummary`, and exported facts name the diagnostic codes that affect
  trust.
- Private type-core fixtures prove exported aliases lower to opaque `TypeRef`
  values, function argument and return packs lower to opaque `PackRef` values,
  and summary construction reads back through the store rather than parser
  syntax.
- Public fixtures prove exported union summaries are normalized before tooling
  observes their display string or member list, and impossible primitive
  intersections summarize as `never`.
- Private type-core fixtures prove nested duplicate intersections collapse back
  to the single underlying type instead of leaking one-child intersection facts.
- Graph-summary fixtures prove logical and host module dependencies appear in
  module summaries with stable invalidation hashes for staleness checks.
- Public `Analyzer.Check` fixtures prove tooling type-alias facts include
  private and exported aliases, use normalized summary shapes, and do not make
  private aliases part of the module export summary.
- Public `Analyzer.Check` fixtures prove tooling type-alias facts carry stable
  source and name ranges for editor callers.
- Public `Analyzer.Check` fixtures prove local annotation mismatch diagnostics
  carry source evidence for the initializer value span.
- Public `Analyzer.Check` fixtures prove assignment mismatch diagnostics carry
  source evidence for the assigned value span.
- Public `Analyzer.Check` fixtures prove function return mismatch diagnostics
  carry source evidence for the returned value span.
- Public `Analyzer.Check` fixtures prove function argument mismatch diagnostics
  carry source evidence for the argument value span.
- Public `Analyzer.Check` fixtures prove missing fixed function arguments are
  treated as nil and produce call-site evidence when incompatible.
- Public `Analyzer.Check` fixtures prove module summaries include a top-level
  returned table value as a value export while preserving type-alias exports as
  separate facts.
- Public `Analyzer.Check` fixtures prove returned annotated locals contribute
  their annotation-derived table shape to the module value export.
- Public `Analyzer.Check` fixtures prove exported/tooling table property
  summaries resolve private non-generic aliases before facts reach callers.
- Graph-summary fixtures prove `local value = require(...) ; return value`
  modules can expose the dependency module's public return shape through the
  requiring module's summary.
- Graph-summary fixtures prove `local value = require(...) ; return value.field`
  modules can expose the dependency module's returned table property type.
- Graph-summary fixtures prove `local field = required.field; return field`
  modules can expose the dependency module's returned table property type.
- Graph-summary fixtures prove typed diagnostics mark a dependency summary
  untrusted and block require-summary enrichment from that dependency.
- Public `Analyzer.Check` fixtures prove `return localTable.field` reads the
  field type from an annotated top-level local table summary.
- Public `Analyzer.Check` fixtures prove `return localTable.nested.field` reads
  nested field types from evolved top-level local table summaries.
- Public `Analyzer.Check` fixtures prove `return coroutine` exposes typed
  function properties for the coroutine library through module summaries.
- Public `Analyzer.Check` fixtures prove coroutine library call return facts can
  produce ordinary type-mismatch diagnostics, while the facts remain independent
  from runtime coroutine state.
- Public `Analyzer.Check` fixtures prove exported function aliases expose
  argument and return pack summaries, and coroutine `resume`/`yield` summaries
  carry value-list tails for resumed, yielded, and final values.
- Public and private fixtures prove generic function signatures keep their type
  parameters and type packs after lowering into store-owned facts.
- Public `Analyzer.Check` fixtures prove nonstrict and nocheck sources produce
  mode-bearing typed artifacts, and nocheck still builds public summary facts
  while suppressing diagnostics.
- Public and private fixtures prove table summaries can carry metatable facts,
  including a source-to-summary `setmetatable` return path and store-owned
  metatable references.
- Public `Analyzer.Check` fixtures prove `getmetatable(setmetatable(...))`
  returns the attached metatable shape in module summaries.
- Public `Analyzer.Check` fixtures prove top-level local table field assignments,
  including nested dot-path assignments, contribute to the returned table shape
  without executing runtime code.
- Public `Analyzer.Check` fixtures prove branch-local table field assignments
  contribute to the returned table shape when both branches agree on the public
  summary fact.
- Public `Analyzer.Check` fixtures prove branch-local table field assignments
  with differing public value facts contribute a union field summary instead of
  being dropped.
- Public `Analyzer.Check` fixtures prove one-sided branch table field
  assignments contribute nilable field summaries instead of being dropped.
- Public `Analyzer.Check` fixtures prove negated nilable-local and grouped
  type-guard conditions narrow both `then` and `else` branches by swapping the
  existing predicate flow facts.
- Module artifact cache and graph-summary fixtures continue to pass while
  consuming the richer `CheckResult.Summary`.
- Runtime checks remain independent: the full Go suite passes after typed
  summary integration.

Steps:

1. Make Analyzer.Check use shared Source, syntax, bind, diagnostics, and module
   resolver outputs.
2. Keep Type Syntax separate from Type Facts.
3. Lower Type Syntax into opaque internal type and Type Pack references.
4. Model type packs, generic functions, table shapes, and metatable types as
   first-class analyzer facts.
5. Put strict, nonstrict, and nocheck policy near solving and artifact
   construction, not parser or binder code.
6. Add union/intersection normalization as a private relation tool with
   budgets and explainable diagnostics.
7. Preserve evidence for diagnostics.
8. Build Module Summary and Tooling Facts from solved facts, not parser nodes.
9. Define module summaries as versioned typed artifacts containing exports,
   public types, generic signatures, public table shapes, diagnostics that
   affect trust in exports, dependency facts, strictness mode, compatibility
   target, and invalidation hash.
10. Consume module summaries and runtime compatibility facts without depending
    on coroutine implementation internals, VM frames, or bytecode state.

Current step status:

- Steps 1-3 are covered by the shared source pipeline, binder reuse, and
  store-owned opaque `TypeRef`/`PackRef` lowering.
- Step 4 is substantially covered for exported aliases, function packs,
  generic functions, table shapes, and initial metatable facts; it still needs
  deeper solved facts for full control-flow-sensitive table/property evolution.
- Step 5 is covered for strict, nonstrict, and nocheck artifact construction;
  legacy `ember.Check` intentionally remains strict-only.
- Step 6 is partial: relation flattening, deduplication, budgets, and impossible
  primitive intersections are implemented, and scalar cast result typing has
  started; explainable budget diagnostics and full cast relation checks remain
  future pressure.
- Step 7 is covered for diagnostic codes, summary trust, alias source ranges,
  the first initialization/assignment/table-field/return/argument constraint
  evidence path, initial unary/binary operator constraint evidence, and initial
  index-read/index-write evidence; richer constraint/flow evidence remains
  future pressure as the solver grows.
- Step 7 flow evidence has initial truthy/nilable, `type(x) ==/~= "kind"`
  branch refinement, `assert(value)` local truthiness behavior, assert-wrapped
  type guard refinement, first `and`-composed true-branch refinement, and
  same-local `or` true/false branch refinement, plus negated truthiness and
  grouped type-guard refinement behind `Analyzer.Check`; richer flow views
  remain future pressure as the solver grows.
- Step 8 is partial: type summaries and tooling aliases use lowered store
  facts, while returned value summaries now combine conservative expression
  facts with initial top-level and nested dot-path table read/write evolution,
  plus agreed-branch, branch-union, and one-sided nilable table assignment
  evolution; annotated table literals now check provided field types and missing
  required named fields.
- Step 9 is covered for the current public artifact shape, including version,
  source identity, mode, target, invalidation hash, exports, dependencies, and
  trust diagnostics.
- Step 10 is initial: module-summary consumption, coroutine library facts, type
  packs, metatable summaries, and the first missing-local/missing-assignment/
  missing-return/missing-argument pack adjustments are independent of
  VM/bytecode internals, but cross-module solved typing and richer runtime
  compatibility facts remain the next pressure.

Done when:

- Compile and Run do not depend on Typed Analysis;
- Analyzer.Check can grow richer diagnostics without changing runtime code;
- module summaries and typed artifact caches support cross-module checks;
- summaries do not contain private locals, raw constraints, flow views, parser
  nodes, or normalization caches;
- Typed Analysis can reason about coroutine library types and yielded/resumed
  value packs without coupling to VM frame internals;
- the type system plan remains compatible with the runtime pipeline.

## Compatibility Strategy

Claim compatibility by category:

- Parsed: Source is accepted and represented.
- Compiled: Source compiles to verified bytecode with tested instruction shape.
- Executed: Runtime Behavior matches tested Luau behavior.
- Coroutine-compatible: coroutine library behavior, yield/resume value flow,
  status transitions, errors, and dead coroutine behavior match the target.
- Yield-compatible: yielding works through all target-supported script calls,
  opt-in host calls, and `pcall`/`xpcall` target functions, while disallowed
  `xpcall` handler and metamethod yields fail with target-compatible behavior.
- Interrupt-compatible: host interruption, instruction budgets, and debug hooks
  are distinct from language-level coroutine yield.
- Conformant: an imported upstream category passes.
- Embedded: the behavior is usable through a stable host interface.

When optimization changes internals, preserve existing compatibility claims or
move the claim backward explicitly in tests and docs.

## Checks

Focused checks:

```sh
go test -count=1 ./...
scripts/check-lane root
scripts/check-fast
```

Phase completion check:

```sh
scripts/check
```

Use `scripts/check-full` only when explicitly requested.

A VM/runtime phase is not complete until it has fixtures for:

- basic coroutine create/resume/yield;
- multiple yielded values;
- resume arguments;
- coroutine final return values;
- resume dead coroutine;
- yield outside coroutine;
- error inside coroutine;
- `coroutine.wrap` behavior;
- nested script-call yield;
- protected-call interaction;
- rejected `xpcall` handler and metamethod yield behavior;
- host interruption versus coroutine yield;
- instruction budget interruption.

Any runtime operation that can invoke user code must declare whether it can
return normal values, an error, a yielded state, or a host interrupt.

## Risks

- Compatibility: Luau has many edge cases around value lists, environments,
  iteration, metatables, coroutines, and typed analysis. Each optimization must
  preserve claimed behavior through tests.
- Coroutine correctness: yielding is not only `coroutine.yield`. It affects
  call frames, value-list adjustment, protected calls, metamethod calls, host
  callbacks, debug hooks, and instruction budgets.
- State explosion: resumable runtime operations can make the VM harder to reason
  about unless each operation has an explicit suspended-state shape.
- Host boundary: host interruption, host errors, and coroutine yields must stay
  separate or embedders will see confusing behavior.
- Debug behavior: line hooks, call hooks, return hooks, and count hooks can
  interact badly with suspension unless frame debug state is preserved.
- Performance: optimizations without benchmarks can make the code harder to
  maintain without proving a gain.
- Public interface: exposing syntax, bytecode, frames, or type solver data too
  early would freeze immature implementation details.
- Locality: splitting packages too early can turn private implementation
  movement into public coordination cost.
- Typed analysis: reusing parser syntax is good, but letting Type Syntax become
  Type Facts would make the analyzer shallow and fragile.
