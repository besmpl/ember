# Plan: Internal Legacy Cleanup

## Goal

Remove Ember's private legacy seams now that the root package has grown real
modules for source loading, typed analysis, bytecode building, table access,
runtime execution, and Hearth-facing Program/Runtime loading.

Breaking private changes are allowed. The cleanup should prefer one deep module
per behavior over compatibility wrappers, duplicate helper paths, and tests that
codify old implementation shape. The final code should feel like the current
design was built intentionally, not layered on top of early seed code.

## Scope

In:

- Break private type names, helper functions, tests, and file layout.
- Break newly experimental exported Program/Runtime details if the new
  interface becomes smaller and clearer.
- Delete obsolete private adapters, cache wrappers, duplicate graph walkers,
  duplicate call paths, and shallow test seams.
- Keep the root package flat unless a slice proves a package split reduces the
  public interface or improves locality.
- Preserve proven runtime behavior through public tests unless a slice names an
  intentional behavior change.

Out:

- Full Luau compatibility.
- Hearth adapter implementation.
- New dependencies.
- Native codegen, custom GC, or public VM/bytecode packages.
- Public exposure of parser, bytecode, frame, stack, or graph internals.

## Design Vocabulary

This plan uses the codebase-design vocabulary:

- **Program module**: owns immutable source graph loading, parse/check/compile
  artifacts, reports, and deterministic parallel loading.
- **Runtime module**: owns mutable execution state, entrypoint exports,
  `require` cache, hook dispatch, instruction budgets, and close semantics.
- **Source artifact module**: owns parse, bind, lower, check, and compile
  artifacts for one source identity.
- **VM module**: owns frame stack, instruction dispatch, call state, coroutine
  state, debug hooks, and budget/cancellation checks.
- **Table semantics module**: owns raw storage, metatable access, raw sequence
  helpers, and table-library sequence behavior.
- **Typed analysis module**: owns type facts, module summaries, diagnostics,
  flow views, and strict-mode policy.

The deletion test: if an old helper can be removed and only one deep module
needs to change, it was legacy glue. If removing it spreads complexity into
callers, keep or deepen it.

## Current Legacy Pressure

The worktree already contains many good private modules, but several old seams
remain visible:

- `module_resolver.go` still exposes private resolver-era concepts such as
  `inMemoryModuleResolver`, `loaderModuleResolver`,
  `moduleGraphCompileResult`, and `moduleGraphSummaryResult` even though
  `program.go` is now the public graph-loading seam.
- `module_runtime.go` and `program.go` both own runtime-ish loading concepts.
  Their responsibilities should collapse into one Runtime implementation.
- `source_pipeline.go`, `moduleArtifactCache`, checker caches, and Program
  loading each know pieces of parse/check/compile artifact ownership.
- `parser.go` still leaks deep expression nesting into compiler, lowering,
  module require scanning, and tests in places where lowered forms or
  expression-shape helpers should be the seam.
- `baselib.go` mixes base globals, global environment, coroutine runtime,
  table library, math library, argument parsing, and string conversion.
- `vm.go` is still the largest implementation file and owns dispatch, frames,
  call mechanics, metamethod operations, coroutine resume interactions, debug
  hooks, and budget/cancellation checks.
- Tests still validate many private storage and bytecode details. Some are
  valuable invariants; others preserve old implementation layout.

## Target Shape

The cleanup does not require new packages. It should make private modules deep
inside the root package:

```text
Public callers
  Compile / Check / Run / RunWithGlobals / LoadProgram / Program.NewRuntime
        |
        v
Program module
  source graph, parallel load phases, artifact store, reports
        |
        +--> Source artifact module
        |      parse -> bind -> lower -> check -> compile
        |
        +--> Runtime module
               entrypoint exports, require cache, hook dispatch, VM calls

Runtime module
  VM thread/frame/calls/coroutines/budget
        |
        +--> Table semantics module
        +--> Base library module
        +--> Typed analysis facts only through summaries
```

The core rule: callers and tests cross one intended module interface. They do
not tunnel through parser shapes, graph maps, VM frames, or table storage unless
the test is explicitly about that private module's invariant.

## Cleanup Principles

- Delete replaceable adapters instead of layering over them.
- Rename private concepts aggressively when names encode old assumptions.
- Prefer one artifact store over separate parse/check/compile caches.
- Prefer lowered source forms for compiler and graph analysis. Use parser nodes
  only while parsing and while preserving source ranges for diagnostics.
- Keep concurrency private to Program loading. Runtime hook execution stays
  ordered unless a future schedule seam proves parallel hooks.
- Preserve public behavior tests first, then delete old unit tests that only
  assert obsolete private shapes.
- If a public experimental Program/Runtime name is wrong, break it now while
  the surface is young.

## Module Contracts

### Program Module

The Program module owns all source graph loading.

Target private responsibilities:

- Convert public `ModuleID` to private normalized graph keys.
- Load graph frontiers through `ModuleLoader`.
- Own bounded parallel phases for load, parse/bind, check, and compile.
- Own deterministic graph, diagnostic, and report ordering.
- Return one immutable `Program` with entrypoint descriptors and compiled
  protos.

Legacy to remove or absorb:

- `inMemoryModuleResolver` and `loaderModuleResolver` as production-shaped
  private adapters. Tests can use small local loaders at the public seam.
- `moduleGraphCompileResult` and `compileModuleGraphResult`; Program loading is
  the compile result.
- `moduleGraphSummaryResult` and `checkModuleGraphSummaryResult`; Program
  loading or Analyzer should own summaries directly.
- Public or private tests that exercise graph compile/check helpers instead of
  `LoadProgram` or `Analyzer.Check`.

Allowed breaking changes:

- Change `ProgramOptions`, `LoadReport`, `ModuleReport`, and internal Program
  fields if it makes the interface smaller or reports clearer.
- Rename private `moduleKey` and graph structures once Program owns the seam.

### Source Artifact Module

The Source artifact module owns one source's durable internal facts.

Target shape:

```go
type sourceArtifact struct {
	identity sourceIdentity
	source   Source
	program  program
	bind     bindResult
	lowered  loweredProgram
	check    *CheckResult
	proto    *Proto
}
```

This is illustrative, not a required exact struct. The important part is one
owned artifact lifecycle.

Legacy to remove or absorb:

- Multiple independent cache maps for `sourceArtifact`, `checkArtifact`, and
  `Proto` unless a benchmark proves separate stores matter.
- Parse counters and compile counters as production fields. Keep them behind
  test-only helpers if needed.
- `parseSource(source string)` as the only source entry point if callers need
  source names for diagnostics. Prefer `parseSource(Source)`.

Allowed breaking changes:

- Change private `sourceArtifact` fields.
- Change private checker/build functions so they accept source artifacts rather
  than raw strings.

### Lowering Module

Lowering should become the implementation seam between parser syntax and
compiler/runtime analysis.

Target responsibilities:

- Normalize value-list adjustment, varargs, calls, method receivers, loop
  shapes, table field shapes, and block scopes.
- Provide require-edge scanning from lowered calls rather than parser nesting.
- Feed emitter with lower-level decisions so emitter stops crawling syntax.

Legacy to remove or absorb:

- Expression scanning helpers duplicated across emitter, module resolver, and
  tests.
- Tests that construct parser nodes solely to prove lowering behavior through
  nested parser fields.
- Any compiler branch that decides value-list semantics separately from
  lowering.

Allowed breaking changes:

- Replace `loweredStatement` and related private structs with clearer shapes.
- Move tests from parser-node fixtures to source-to-lowered fixtures or public
  source-to-result tests.

### Runtime Module

The Runtime module should be the only mutable program execution owner.

Target responsibilities:

- Own entrypoint export cache.
- Own module `require` cache and active-loading state.
- Own hook dispatch reports.
- Create VM threads with context and instruction budget.
- Propagate host globals and context-aware host callbacks.
- Release all owned values on `Close`.

Legacy to remove or absorb:

- `runModuleGraphWithGlobals` as a separate execution path.
- Separate `moduleRuntime` if its state can live directly in `Runtime`.
- Runtime tests that bypass Program to hand-build graph/proto maps unless they
  are narrowly testing a private Runtime invariant.

Allowed breaking changes:

- Change `Runtime` private fields and helper names.
- Change hook report shape while Program/Runtime is still experimental, as long
  as tests move with the intended interface.

### VM Module

The VM module is allowed to be internally split by file, but should expose one
private execution interface to Program/Runtime and `RunWithGlobals`.

Target private interface:

```go
func executeProto(ctx context.Context, proto *Proto, globals *globalEnv, options executeOptions) ([]Value, error)
```

Exact names may differ. The important part is that callers do not choose among
several run helpers.

Legacy to remove or absorb:

- Layered helpers such as `runVerifiedProto`,
  `runVerifiedProtoWithContext`, and `runVerifiedProtoWithContextBudget` if one
  options-based helper can replace them.
- Duplicate context/budget plumbing between host calls, coroutine resumes, and
  debug hooks.
- Debug-hook tests that require direct frame mutation when a private VM harness
  can express the invariant.

Allowed breaking changes:

- Rename `vmThread`, frame helpers, call-state helpers, and budget helpers.
- Split `vm.go` into cohesive files such as `vm_exec.go`, `vm_frame.go`,
  `vm_call.go`, `vm_coroutine.go`, `vm_debug.go`, and `vm_ops.go`.

### Base Library Module

Base library code should stop being a single catch-all file.

Target private modules:

- `base_env.go`: `globalEnv`, base global lookup, host overrides.
- `base_table.go`: table library functions.
- `base_coroutine.go`: coroutine table and coroutine values.
- `base_math.go`: math functions.
- `base_convert.go`: `type`, `tonumber`, `tostring`, argument helpers.

Legacy to remove or absorb:

- Generic helpers in `baselib.go` that are only used by one library.
- Coroutine runtime types living next to table-library sequence behavior.
- String conversion and metatable lookup duplicated between base library and
  VM operations.

Allowed breaking changes:

- Rename private base-library helpers freely.
- Move helper functions to files matching behavior ownership.

### Table Semantics Module

Table semantics should be the only place that knows raw storage and metatable
policy.

Target responsibilities:

- Public `Table.Get`/`Set` use public table access policy.
- Runtime ops and base library use runtime table access policy.
- Raw sequence operations use one raw sequence helper.
- Direct raw string-field fast paths stay isolated to VM/optimizer hot paths
  with tests proving they are optional optimizations.

Legacy to remove or absorb:

- Direct `Table` field or raw helper usage in base library where `rawSequence`
  or `tableAccess` should own the policy.
- Duplicate `__index`, `__newindex`, `__len`, and `__call` lookup code.
- Tests that require exact map/array storage placement unless the invariant is
  a performance-critical table storage invariant.

Allowed breaking changes:

- Rename private raw table helpers.
- Change storage layout if `Table.Get`, `Table.Set`, raw library behavior, and
  performance-sensitive tests are updated.

### Typed Analysis Module

Typed analysis should own type facts and module summaries without parser or
Program callers knowing solver internals.

Target responsibilities:

- `Analyzer.Check` is the public typed-analysis seam.
- Program loading may optionally request summaries through a private analyzer
  adapter, not by calling checker internals directly.
- Module summary enrichment should live with typed analysis or Program reports,
  not in legacy resolver helpers.

Legacy to remove or absorb:

- Checker functions that return parser internals to unrelated tests.
- Module summary propagation through `moduleGraphSummaryResult`.
- Parser type tests that assert syntax retention through raw parser nesting
  when `CheckResult.Facts` can express the need.

Allowed breaking changes:

- Change private type-core/test helpers.
- Expand `ToolingFacts` if that deletes parser-crawling tests.

## Cleanup Phases

The cleanup is phased because each phase removes a class of legacy seam and
creates the intended interface for the next phase. Work inside one phase can be
parallelized only when file ownership is disjoint and the phase contract below
is already clear.

### Phase 0: Baseline And Classification

Goal: name what is behavior, what is a private invariant, and what is legacy
shape before deleting anything.

Steps:

1. Run the current focused baseline checks.
2. Tag existing tests mentally or in temporary notes as public behavior,
   private invariant, or legacy shape lock.
3. Record any benchmark-sensitive files before changing table storage, VM
   frames, bytecode fast paths, or Program loading.
4. Do not edit production code in this phase unless a baseline test is already
   broken.

Gate:

- `go test -vet=off -count=1 .` passes or current failures are documented with
  exact test names.
- The first cleanup phase has a clear write scope.

### Phase 1: Program Owns Graph Loading

Goal: delete resolver-era graph compile/check paths and make `LoadProgram` the
only graph-loading implementation seam.

Steps:

1. Move normalized key conversion, graph frontier loading, require-edge
   expansion, and cycle diagnostics behind Program-owned helpers.
2. Replace `module_resolver_test.go` coverage with public `LoadProgram` tests
   or Program-private tests that do not instantiate legacy resolvers.
3. Delete `inMemoryModuleResolver`, `loaderModuleResolver`,
   `moduleGraphCompileResult`, `compileModuleGraphResult`,
   `moduleGraphSummaryResult`, and `checkModuleGraphSummaryResult` if no real
   caller remains.
4. Keep only small private helpers for key normalization and dependency
   extraction.
5. Run `go test -vet=off -count=1 .` and `scripts/check-lane root`.

Acceptance:

- Program tests cover shared graph loading, host namespace loading, cycle
  diagnostics, loader errors, check diagnostics, and deterministic reports.
- No test constructs a legacy resolver adapter.

### Phase 2: One Source Artifact Store

Goal: make parse, bind, lower, check, and compile artifacts live in one private
source artifact module.

Steps:

1. Change artifact cache ownership so Program loading and Analyzer use the same
   source identity policy.
2. Make parse functions accept `Source` when source names matter.
3. Store lowered facts beside parsed and bound facts.
4. Replace parse/check/compile counters with test-only assertions or remove
   them.
5. Update tests to assert behavior through Program reports, Analyzer results,
   or source-to-result execution.

Acceptance:

- A source is parsed once per source identity in Program loading.
- Check and compile phases reuse source artifacts without exposing cache maps.
- Source names appear in diagnostics consistently.

### Phase 3: Lowering Becomes The Compiler Seam

Goal: stop emitter and graph analysis from crawling parser implementation
nesting.

Steps:

1. Introduce or complete `loweredProgram`.
2. Move require scanning to lowered calls.
3. Move value-list, vararg, receiver, and loop-shape decisions fully into
   lowering.
4. Make emitter consume lowered forms instead of parser statements where
   practical.
5. Delete expression-shape helpers that become redundant after lowering owns
   the shape.

Acceptance:

- Emitter tests no longer depend on parser nesting.
- Existing source-to-result tests still pass.
- New lowering tests prove require scanning and hook-table friendly call shapes.

### Phase 4: Runtime Absorbs Module Runtime

Goal: make `Runtime` the only mutable owner for module execution and hook
dispatch.

Steps:

1. Move `moduleRuntime` state into `Runtime` or a private field-owned helper.
2. Replace `runModuleGraphWithGlobals` tests with Program/Runtime tests.
3. Route entrypoint loads and required module loads through one load function.
4. Ensure host globals and budgets are applied uniformly to entrypoint loads,
   module loads, and hook calls.
5. Delete old graph/proto direct runtime helpers.

Acceptance:

- Required modules are cached once per Runtime.
- Entrypoint exports are cached once per Runtime.
- Active-loading errors include deterministic module paths.
- `Close` releases module and entrypoint caches.

### Phase 5: One VM Execution Interface

Goal: replace layered run helpers with one options-based VM execution seam.

Steps:

1. Introduce a private execution options struct for context, budget, debug
   hooks, and upvalues.
2. Route `RunWithGlobals`, Runtime hook calls, coroutine resumes, protected
   calls, and debug-hook tests through the same execution path where possible.
3. Split `vm.go` by ownership after behavior is green.
4. Keep hot-path helpers private and benchmark-backed.
5. Delete duplicate context/budget helpers.

Acceptance:

- Instruction budget behavior is identical through `RunWithGlobals`,
  Program/Runtime hooks, and coroutine paths.
- Context cancellation reaches context-aware host calls.
- No public caller can observe VM frame or thread types.

### Phase 6: Base Library File Split And Policy Cleanup

Goal: separate base global environment, table library, coroutine library, math,
and conversion helpers.

Steps:

1. Move `globalEnv` and base global lookup to a base environment file.
2. Move table functions to table-library ownership and route sequence behavior
   through `rawSequence`.
3. Move coroutine state and functions to coroutine-library ownership.
4. Move math and conversion helpers to focused files.
5. Delete catch-all helpers or move them next to their only caller.

Acceptance:

- `baselib.go` is deleted or reduced to a tiny index only if useful.
- Each base library file owns one behavior cluster.
- Public base library behavior remains proven through source-to-result tests.

### Phase 7: Test Surface Replacement

Goal: delete tests that preserve legacy implementation shape and replace them
with tests across the intended module interfaces.

Steps:

1. Classify internal tests as public behavior, private invariant, or legacy
   shape lock.
2. Keep public behavior tests through `Compile`, `Run`, `RunWithGlobals`,
   `LoadProgram`, `Program.NewRuntime`, `Analyzer.Check`, `Table.Get`, and
   `Table.Set`.
3. Keep private invariant tests only for hot storage, bytecode verifier,
   lowering facts, and VM frame/coroutine invariants that cannot be tested
   clearly through public behavior.
4. Delete legacy shape-lock tests as the old helpers disappear.
5. Rename tests to describe observable behavior, not helper names.

Acceptance:

- The test suite still catches behavior regressions.
- Fewer tests need edits when private structs move or rename.
- Legacy helper names no longer appear in test names.

### Phase 8: Final Sweep And Retirement

Goal: remove cleanup scaffolding and decide whether this plan can be retired.

Steps:

1. Search for old helper names, resolver names, and shape-lock test names.
2. Run the full standard checks.
3. Update public-surface and roadmap docs only when public Program/Runtime
   behavior changed.
4. Retire this plan once all legacy seams named above are either deleted or
   deliberately kept with a short reason.

Gate:

- `scripts/check` passes.
- Remaining legacy names are intentional and documented in the final report.

## Phase Dependencies

```text
Phase 0 baseline
  -> Phase 1 Program graph ownership
      -> Phase 2 source artifact store
          -> Phase 3 lowering/compiler seam
          -> Phase 4 Runtime/module runtime merge
              -> Phase 5 VM execution interface
  -> Phase 6 base/table cleanup
  -> Phase 7 test surface replacement
      -> Phase 8 final sweep
```

Phase 6 can start after Phase 0 if it avoids VM execution-option changes.
Phase 7 should happen throughout, but the final deletion pass belongs near the
end after replacement tests exist.

## Parallelism And Ordering

Cleanup implementation can happen in parallel only after slices define
non-overlapping write ownership. Recommended grouping:

- Program/source artifact cleanup: `program.go`, `module_resolver.go`,
  `module_runtime.go`, `source_pipeline.go`, `program_test.go`,
  `module_resolver_test.go`.
- Lowering/compiler cleanup: `lowering.go`, `emitter.go`,
  `expression_shape.go`, `compiler.go`, `compiler_test.go`,
  `lowering_test.go`.
- Runtime/VM cleanup: `vm.go`, VM-focused tests, runtime Program tests.
- Base/table cleanup: `baselib.go`, `table_ops.go`, `raw_sequence.go`,
  `value.go`, table/base-library tests.
- Typed analysis cleanup: `checker.go`, `analysis.go`, `type_core.go`,
  strict/type tests.

Do not parallelize edits to the same ownership group. Runtime hook behavior and
VM execution options are especially coupled; serialize those unless a later
plan provides exact contracts.

## Checks

Focused iteration:

```sh
go test -vet=off -count=1 .
scripts/check-lane root
```

Pre-finish:

```sh
scripts/check-fast
scripts/check
```

If VM execution, coroutine, or parallel Program loading changes materially,
also run:

```sh
go test -race -run 'Program|Runtime|Coroutine|VM|RunWithGlobals' .
```

## Risks

- Public surface: Program/Runtime is young enough to break, but `Compile`,
  `Run`, `RunWithGlobals`, `Check`, `Value`, `Table`, and `UserData` should not
  change unless a slice explicitly says so.
- Compatibility: deleting parser-shape tests can hide syntax retention
  regressions unless Analyzer or lowering tests replace the behavior.
- Performance: table storage, VM frames, and bytecode fast paths need focused
  benchmarks before and after large rewrites.
- Concurrency: Program loading workers must remain private, bounded, context
  bound, and fully drained before returning.
- Scope: file splits are useful only when they improve locality. Do not turn a
  monolith into many shallow pass-through files.
