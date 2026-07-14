# Ember Performance Optimization Implementation Plan

Status: proposed, evidence-gated execution plan

Repository inspected at: `cb913dee73f626d0ddb08bfa54c47a487256009a`

Source plan: `performance-optimization-plan.md`

Historical profile source: `performance-audit.md` at `fd0aa29a`

## 1. Purpose

This plan replaces the attached performance draft with a smaller executable
program. The original draft is a useful catalog of hypotheses, but it commits
Ember to representation, table, allocator, specialization, and native-tier
work before the earlier measurements can prove those changes are still useful.

The committed work in this plan is limited to the current measured seams:

1. refresh and make the performance evidence comparable;
2. collapse repeated compiler assignment analysis;
3. remove the persistent hook invocation allocation floor;
4. simplify the existing canonical fixed-call continuation path;
5. decide whether an unrestricted dispatch loop is still justified.

Private slots, an ordered-hash rewrite, nursery allocation, specialization,
inlining, fused opcodes, and native code are conditional campaigns. They do not
begin automatically. Each requires fresh evidence and a new architecture gate.

This document is temporary coordination material. Retire or delete it when the
committed program lands, is abandoned, or is replaced.

### 1.1 Explicit assumptions

- The task is to improve broad compiler and runtime performance for
  Hearth-shaped persistent use, not to win one benchmark at the expense of
  compatibility or maintainability.
- No public breaking change is authorized. A small additive API may be
  proposed only after a private implementation proves the need and Sol high
  reviews the lifetime contract.
- The implementation will use the repository's existing dependencies,
  benchmark tools, generated-VM workflow, and direct-on-`main` commit policy.
- The checked-in historical profile is too old to set current numeric targets;
  Phase 0 owns the authoritative baseline and may reorder or cancel later work.
- A multiple-factor end-to-end target was not specified. Each retained slice
  must beat measured noise and clear its family regression gates; conditional
  architecture campaigns require a new plan.
- Host-global freshness, exact limits, callback capture, deterministic table
  iteration, and retained public values are compatibility requirements unless
  an explicit later task changes them.

## 2. Repository understanding

### 2.1 Compiler

The compiler is a root-package pipeline:

- `source_pipeline.go` parses source into typed syntax arenas and runs binding;
- `binder.go` already assigns stable integer IDs to symbols and records every
  identifier use in `bindResult.nodeFacts`;
- `emitter.go` maps bound symbols to registers and emits bytecode IR;
- `optimizer.go`, `function_analysis.go`, and related files optimize and analyze
  that IR;
- `bytecode.go` finalizes a verified immutable `Proto`;
- `wordcode.go` encodes the canonical wordcode representation.

The assignment bottleneck is narrower than the source draft states.
`canCompileSingleLocalAssignmentInPlace` and the recursive
`expressionCanAssignToNameInPlace` family in `emitter.go` still traverse arena
expressions by source name. Stable symbol IDs are already available; they do
not need to be introduced as a separate phase.

### 2.2 Canonical VM and call stack

ADR 0005 retains one canonical direct wordcode VM. The generated production
and instrumented loops come from `vm_dispatch_template.go.tmpl`, with semantic
inventory in `vm_dispatch_spec.go` and generation in
`cmd/ember-vmgen/main.go`. Generated source is output, not an editing seam.

The loop already keeps `Proto`, words, constants, registers, and `pc` in local
variables. Fixed calls already use:

- liveness and capture facts from `function_analysis.go`;
- borrow hints encoded during `finalizeProtoExecutionArtifact`;
- a shared `vmStackOwner` register store;
- a compact `vmFrameRecord` continuation stack;
- guarded generic, variadic, protected, coroutine, and callback fallbacks.

On 64-bit platforms `vmFrameRecord` is currently exactly 48 bytes and carries
at most one pointer, enforced by `frame_record_test.go`. The remaining work is
to remove repeated immutable validation and unnecessary restoration from the
existing path, not to introduce another call engine.

### 2.3 Execution control

`execution_control.go` owns cancellation and exact runtime limits.
`executionWindow.stepInstruction` polls context every 256 instructions and
tracks instruction budget state. Plain `Run` already uses a nil controller for
background, unlimited execution. `Runtime.RunHook` always creates a controller,
even for the equivalent background and zero-limit case.

Both generated direct loops currently call `stepInstruction`. A controller-free
generated loop is therefore a possible later optimization, but it must be
selected once at entry and emitted from the same template as controlled and
instrumented execution.

### 2.4 Program and runtime invocation

`program.go` owns immutable `Program` graphs and serialized mutable `Runtime`
state. `Runtime.RunHook` currently:

- creates a `HookReport` and appends per-entrypoint records;
- acquires a runtime owner lease;
- creates an `executionController`;
- asks `RuntimeHost` for fresh globals and copies them;
- lazily loads entrypoints and modules;
- creates a `runtimeCallContext`;
- wraps `context.Context` with `context.WithValue`;
- creates an owner-bound `globalEnv` and a bound `require` native function;
- invokes the hook and discards its script results.

`runtime_call.go`, `module_runtime.go`, and `callback.go` preserve module origin,
host globals, callback capture, owner roots, and fresh budgets. These semantics
are part of the current public interface. The first invocation optimizations
must remain private and preserve `RunHook`, `CaptureCallback`, and
`Callback.Call` behavior.

### 2.5 Values, slots, ownership, and tables

The canonical VM stores public 16-byte `Value` objects in registers, globals,
tables, closures, cells, and result windows. `slot.go`, `runtime_heap.go`, and
`runtime_owner.go` provide private owner-relative handles, typed slabs,
generation checks, roots, pins, and collection infrastructure, but canonical
execution is not slot-backed.

A reference slot is relative to a runtime owner. Immutable shared `Proto`
objects therefore cannot store owner-bound reference constants directly. Any
future slot cutover needs runtime-local materialization keyed by `Proto` and a
complete ownership/GC design.

Tables are implemented mainly in `value.go` with array storage, inline string
fields, open-addressed overflow hash storage, and a lazy deterministic
iteration journal. Larger journals add a `map[tableKey]int` index. Equal-content
string boxes, deletion/reinsertion order, mixed array/hash order, and `next`
behavior are observable semantics.

ADR 0006 stopped the table allocator campaign because persistent Hearth-shaped
updates were dominated by the `RunHook` boundary, not table construction. A
table data-structure experiment may be reconsidered only with new persistent
evidence; stateless sparse-grid allocation alone does not reopen allocator or
nursery work.

### 2.6 Measurement and verification

The repository already has the main measurement surfaces:

- `scripts/performance-audit` captures five benchmark families and optional
  CPU/allocation profiles;
- `scripts/bench-summary` compares raw benchmark samples;
- `scripts/scenario-ratio-gate` gates runtime parity rows;
- `runtime_benchmark_test.go` separates stateless, persistent, bounded,
  cancelable, host-boundary, and retained-result workloads;
- `compiler_stage_benchmark_test.go` exposes the 256 KiB stage matrix;
- `top10_luau_benchmark_test.go` includes recursive Fibonacci and sparse grid;
- `execution_differential_test.go` compares production and instrumented VM
  behavior.

The missing piece is a single comparison contract for complete audit
directories, including raw samples for every family and a measured noise
envelope. The plan extends existing tooling rather than creating another
benchmark framework.

## 3. Complexity and subagent strategy

Complexity classification: very high.

The work is performance-critical, crosses compiler and runtime architecture,
touches exact execution limits and ownership, and contains experiments whose
outcomes cannot be known in advance.

Use these assignments during execution:

| Work | Primary agent | Review |
| --- | --- | --- |
| Audit capture, comparison scripts, documentation cleanup | Luna high | Sol high for the measurement contract |
| Symbol-aware assignment walk | Luna max | Sol high before retention |
| Invocation scope, controller, environment lifetime | Luna max | Sol high before public interface changes |
| Fixed-call facts and continuations | Luna max | Sol high design and retention review |
| Generated loop or block-accounting experiments | Luna max | Sol high before retaining added production code |
| Slot, table, nursery, specialization, or native campaign | Luna max only after approval | Sol high mandatory stop/go review |

Parallelism is deliberately limited:

- Phase 1 compiler work may run beside Phase 2.1 measurement work after the
  Phase 0 contract is fixed because their files do not overlap.
- Invocation, call-stack, and dispatch slices are sequential because they all
  change runtime profiles and overlapping VM seams.
- Conditional campaigns do not start in parallel with committed phases.

The planning audit used two Luna max agents independently for the
compiler/table and VM/runtime seams, followed by a Sol high cross-system
review. The execution assignments below incorporate that review and use Luna
high only for bounded tooling, counters, and measurement work; compiler and
runtime hot-path changes remain Luna max work.

## 4. Corrections to the source plan

The implementation agent must not carry these stale assumptions forward:

1. The implementation baseline is not `ea47dd1f`; current inspected HEAD is
   `cb913dee`, and all old percentages are hypotheses until Phase 0 refreshes
   them.
2. Stable binder symbol IDs already exist in `binder.go`.
3. Fixed-call liveness and borrow facts already exist in
   `function_analysis.go`.
4. The generated VM already has production and instrumented variants and
   already keeps the hot decoded state in loop locals.
5. The direct per-word cache sidecar is the retained design. Do not revive the
   deleted rank/popcount lookup.
6. Global slots, intrinsic `opFastCall`, and several diagnostic kind-fact
   detectors already exist. New work must identify missing consumers rather
   than duplicate metadata.
7. Public `CallContext`, prepared `HookHandle`, and `CallHookInto` are not
   justified. Lazy loading, multiple entrypoints, changing host globals, and
   callback capture leave their lifetime and invalidation rules undefined.
8. A blanket permission for breaking public changes is removed. Performance
   must first be recovered behind the current interface or through a small
   additive interface with a proven embedding use.
9. Canonical slots, ordered hashes, nurseries, inlining, fusions, and a native
   tier are not scheduled deliverables. They are conditional decisions.

## 5. Program-wide execution rules

### 5.1 One hypothesis per slice

Each code slice should normally be one commit and change one seam. Record:

- baseline and candidate commit;
- exact benchmark commands and environment fingerprint;
- raw samples, median, spread, B/op, and allocs/op;
- expected and actual profile movement;
- focused correctness checks;
- full-family and full-scenario effects;
- code, metadata, generated source, and binary size when relevant;
- keep, revise, or revert decision.

### 5.2 Noise-aware performance gates

Phase 0 defines the machine-specific noise envelope. Do not simultaneously
accept 3% run-to-run variation and reject a 1% candidate regression.

For every later slice:

- improvement must exceed the measured noise envelope;
- no relevant family may regress beyond the envelope without an explicit
  trade approved by Sol high;
- no retained Scenario row may regress by more than `max(5%, noise)`;
- allocation changes must be exact and reproducible;
- pprof percentages are diagnostic signals, not pass/fail targets;
- cumulative profile percentages must not be added because call trees overlap.

If a candidate does not clear its gate, revert it and keep only reusable tests
or measurement improvements.

### 5.3 Correctness invariants

Every accepted slice preserves:

- one canonical wordcode semantic implementation;
- exact instruction, call, module, string, table, object, and coroutine limits;
- context cancellation and deadline behavior;
- `RuntimeError`, `errors.Is`, `errors.As`, and script stack fidelity;
- deterministic table order and key identity;
- callback capture, roots, close, and runtime busy semantics;
- retained public values after execution;
- race and checkptr safety for touched ownership paths.

### 5.4 Standard verification ladder

Run the smallest focused tests first, then:

```sh
scripts/check-lane root
scripts/check-fast
scripts/check
```

For runtime ownership, callbacks, coroutines, cancellation, call frames, slots,
or tables, also run the applicable commands from `docs/checks.md`:

```sh
go test -race -count=1 ./...
go test -gcflags=all=-d=checkptr=2 -count=1 ./...
```

Do not run `scripts/check-full` unless explicitly requested.

## 6. Committed implementation phases

### Phase 0 - Refresh evidence and comparison tooling

#### Slice 0.1 - Capture the implementation-start baseline

Assigned subagent: Luna high

What and why:

Capture clean, current evidence before accepting any source change. The old
profile predates runtime lifecycle and benchmark-tooling changes.

Relevant files and modules:

- `performance-audit.md`
- `docs/checks.md`
- `scripts/performance-audit`
- `scripts/bench-summary`
- `runtime_benchmark_test.go`
- `compiler_stage_benchmark_test.go`
- `top10_luau_benchmark_test.go`

Implementation steps:

1. Start from the actual implementation commit with no unrelated worktree
   changes.
2. Record commit, Go version, OS, architecture, CPU, `GOMAXPROCS`, build flags,
   power mode, and relevant environment.
3. Run two independent baseline captures to separate noise from candidate
   movement:

   ```sh
   BENCHTIME=500ms COUNT=5 scripts/performance-audit --output /tmp/ember-perf-baseline-a --profiles
   BENCHTIME=500ms COUNT=5 scripts/performance-audit --output /tmp/ember-perf-baseline-b --profiles
   ```

4. Capture focused 5 second profiles for recursive Fibonacci, persistent hook
   lanes, sparse grid, and 256 KiB compiler emit/compile.
5. Record generated VM source size, package test binary size, and the size of
   `Proto` and `vmFrameRecord` structures reported by existing layout tests.
6. Compute the per-row and per-family baseline spread. Use the larger of a
   robust relative spread and a small absolute timing floor for later gates.
7. Update `performance-audit.md` only with reproducible current findings; do
   not preserve stale percentages as current facts.

Tests and verification:

- `scripts/performance-audit --help`
- `sh -n scripts/performance-audit scripts/bench-summary`
- `scripts/bench-summary-test`
- `go test -run 'TestVMFrameRecordFitsCompactCallStateBudget|TestProtoFieldClassificationBudget' .`

Completion criteria:

- both captures are complete and comparable;
- every family has raw samples and, when requested, non-empty profiles;
- the noise envelope is written into the baseline manifest;
- the top compiler, persistent invocation, call, and dispatch costs are either
  reconfirmed or explicitly reordered.

Risks and edge cases:

- thermal throttling and concurrent workspace checks can invalidate samples;
- a dirty worktree must be intentional and recorded, never silently accepted;
- profile percentages can move solely because total runtime changed.

#### Slice 0.2 - Compare complete audit directories

Assigned subagent: Luna high

What and why:

Extend the existing audit tooling so each later slice receives one stable,
fail-closed before/after report. The compiler family is currently retained as
raw output, and complete audit directories are not compared through one
contract.

Relevant files and modules:

- `scripts/performance-audit`
- `scripts/bench-summary`
- `scripts/bench-summary-test`
- `scripts/check`
- `docs/checks.md`

Implementation steps:

1. Make `performance-audit` retain raw benchmark output for every family, not
   only formatted runtime summaries.
2. Add a small repository script that compares two audit directories by
   delegating sample parsing to `bench-summary`; do not duplicate its median
   and metric parser.
3. Report runtime, compiler, B/op, allocs/op, missing rows, and environment
   mismatches separately.
4. Accept noise thresholds from the baseline manifest or explicit flags.
5. Fail closed on missing samples, metrics, incomplete markers, mismatched
   families, or incompatible environment fingerprints.
6. Add fixture-based shell tests for an improvement, a regression, a missing
   metric, a missing row, and a fingerprint mismatch.

Tests and verification:

- existing `scripts/bench-summary-test` plus the new comparison fixture test;
- `sh -n` for changed scripts;
- `scripts/check`.

Completion criteria:

- one command compares baseline and candidate audit directories;
- an injected regression beyond the configured envelope fails;
- missing compiler metrics fail instead of disappearing from the report;
- no new external dependency is added.

Risks and edge cases:

- do not treat unlike benchmark names or toolchains as comparable;
- do not reduce all families to one geomean that hides a severe row regression;
- avoid parsing human-formatted profile output as a stable data format.

#### Slice 0.3 - Retire competing historical roadmaps

Assigned subagent: Luna high

What and why:

Prevent superseded execution plans from being mistaken for current work. They
describe deleted VM designs, a rebuilt-sorted table iteration approach, or
campaigns that later ADRs stopped.

Relevant files and modules:

- `docs/exec-plans/interpreter-core-speed.md`
- `docs/exec-plans/general-optimization.md`
- `docs/exec-plans/massive-optimization.md`
- `docs/README.md`
- ADRs 0004, 0005, and 0006

Implementation steps:

1. Check inbound links and preserve any still-useful measurement history in
   the relevant ADR or audit document.
2. Delete the superseded plans if repository convention treats completed plans
   as disposable; otherwise add an unmistakable historical status and remove
   them from current navigation.
3. Link the refreshed `performance-audit.md` and this plan from the smallest
   appropriate documentation index while execution is active.
4. Do not rewrite historical commits or ADR decisions to match the new plan.

Tests and verification:

- search documentation for current links to the three historical plans;
- run the documentation-only checks from `docs/checks.md`;
- run `scripts/check` if navigation or generated documentation is affected.

Completion criteria:

- an implementation agent entering through `docs/README.md` sees one current
  performance program;
- historical rationale remains available without presenting stale work as an
  executable roadmap.

Risks and edge cases:

- deleting the only record of a rejected design would make later archaeology
  harder; preserve decisions, but not competing instructions;
- this slice must not edit code or change the Phase 0 benchmark baseline.

Phase 0 gate:

Sol high reviews the measurement contract. If the refreshed profile materially
reorders the next three bottlenecks, reorder or remove later phases before any
optimization code is written.

### Phase 1 - Collapse compiler assignment analysis

#### Slice 1.1 - Replace name walkers with one symbol-aware dependency walk

Assigned subagent: Luna max, with Sol high retention review

What and why:

Replace the repeated name-based recursive walkers in `emitter.go` with one
target-dependent expression traversal using binder symbol identity. This is a
local compiler change with a strong old profile signal and no public interface
change.

Relevant files and modules:

- `emitter.go`
- `binder.go` for existing `bindResult.useClassification`
- `syntax_tree.go` and syntax arena accessors
- `compiler_stage_benchmark_test.go`
- `compiler_benchmark_fidelity_test.go`
- `compiler_retained_memory_test.go`
- `binder_test.go`
- `bytecode_test.go`
- `compiler_effects_test.go`
- a focused `emitter_assignment_analysis_test.go` if existing public-surface
  tests cannot expose the visit-count assertion

Implementation steps:

1. Resolve the single assignment target once through existing binding facts and
   obtain its symbol ID. Reject globals, unresolved nodes, captures that cannot
   use the local register, and malformed arena IDs exactly as today.
2. Replace the `...ReferencesName` plus `...CanAssignToNameInPlace` recursion
   with one private walk returning the minimum decision needed by emission,
   such as safe, unsafe, or no target reference.
3. Compare named terms through `bindResult.useClassification(termID)`, not
   source spelling. A shadowed local with the same name is a different symbol.
4. Preserve evaluation-order decisions for short-circuit expressions,
   comparison/concat/additive/multiplicative chains, powers, selectors,
   computed table keys and values, calls, grouped expressions, and casts.
5. Add a temporary test-only visit counter or benchmark hook. It must not add a
   production field or branch, and remove it after the traversal reduction is
   demonstrated unless it remains a cheap regression assertion.
6. During development, differentially compare the old and new decision over a
   corpus of parsed expressions. Delete the old walkers before retention.
7. Do not add a general expression-facts framework or new binder side table in
   this slice.

Tests and verification:

- existing binder symbol/shadow/capture tests in `binder_test.go`;
- focused cases for `value = value + 1`, target use after another operand,
  selector indexes, calls, table literals, short circuit, shadowing, and
  captured upvalues;
- `TestCompileAndRunAssignmentRHSCanReadAssignedLocal` and assignment/value-list
  behavior in `compiler_test.go`;
- compiler fidelity and retained-memory tests;
- five-sample benchmark:

  ```sh
  go test -run '^$' -bench '^BenchmarkCompilerStageMatrix/256KiB/(emit|compile)$' -benchmem -benchtime=1s -count=5 .
  ```

- focused CPU profile followed by the standard verification ladder.

Completion criteria:

- bytecode and observable results are unchanged for the differential corpus;
- assignment-analysis expression visits fall by at least 45%;
- the old name-reference walker family is deleted and is absent from the new
  CPU profile;
- emit time improves beyond the Phase 0 noise envelope;
- compiler memory does not regress by more than 3% or the measured envelope,
  whichever is larger.

Risks and edge cases:

- assignment safety is target-dependent, so a fact for one symbol cannot be
  reused for another;
- using a name instead of a syntax node classification reintroduces the
  shadowing bug this slice is intended to remove;
- calls and selectors can observe or mutate state, so traversal order matters.

#### Slice 1.2 - Decide whether any assignment fact should be retained

Assigned subagent: Sol high

What and why:

Reprofile after Slice 1.1 and decide whether immutable cached facts have a real
second consumer. The source draft assumed assignment facts would naturally
feed call descriptors, cleanup, and kind specialization; current code does not
prove that relationship.

Relevant files and modules:

- `binder.go`
- `emitter.go`
- `function_analysis.go`
- `compiler_stage_benchmark_test.go`

Implementation steps:

1. Inspect the post-slice profile and expression visit counts.
2. Name every proposed consumer and verify it needs the same target-dependent
   fact.
3. If fewer than two real consumers exist, record the stop decision and keep
   the direct emitter result.
4. If two consumers exist, write a separate bounded slice specifying storage,
   invalidation, memory budget, and deletion criteria before implementation.

Tests and verification:

- no new code is required for a stop decision;
- if a follow-up is approved, rerun the compiler stage, fidelity, and retained
  memory gates from Slice 1.1.

Completion criteria:

- either the direct result is accepted as final, or a separately scoped cache
  slice is approved with two demonstrated consumers;
- no speculative general analysis framework is introduced.

Risks and edge cases:

- caching target-specific facts by expression alone is incorrect;
- extra binder storage can erase an emit-time win through parse/bind memory.

### Phase 2 - Remove persistent hook boundary overhead

#### Slice 2.1 - Establish the allocation contract and private hook core

Assigned subagent: Luna high

What and why:

Create one private hook execution module with optional report collection, then
measure each boundary cost separately. This deepens the existing `Runtime`
seam without changing public behavior.

Relevant files and modules:

- `program.go`
- `runtime_benchmark_test.go`
- `program_test.go`
- `callback.go`
- `runtime_call.go`

Implementation steps:

1. Extract the body of `Runtime.RunHook` into one private runner that accepts
   an optional report collector or sink.
2. Keep `RunHook` behavior byte-for-byte equivalent, including entrypoint
   order, loaded/called/skipped flags, wrapping, and partial report behavior on
   errors.
3. Add benchmark sublanes that distinguish report-producing execution from an
   internal error-only call, background versus cancelable contexts, zero versus
   nonzero limits, nil versus changing host globals, and one versus multiple
   entrypoints.
4. Use allocation profiles to attribute report growth, controller, context,
   environment, bound `require`, result windows, and host-global copying.
5. Add no broad counter inventory; add only counters needed for this boundary.

Tests and verification:

- existing `program_test.go` RunHook ordering, skip, module, host, cancellation,
  limit, close, and error tests;
- existing `runtime_benchmark_test.go` persistent/bounded/cancelable lanes;
- `go test -run 'TestRuntimeRunHook|TestRuntimeOwner|TestCallback' .`;
- allocation profile of the persistent runtime family;
- standard verification ladder.

Completion criteria:

- public `RunHook` behavior is unchanged;
- the error-only internal path is measurable without report allocations;
- each remaining allocation has a named owner and source symbol;
- no new public name has been added.

Risks and edge cases:

- reports may contain partial progress when a later entrypoint fails;
- lazy entrypoint loading and host calls happen in configured order;
- an internal error-only path must not silently change skipped-hook semantics.

#### Slice 2.2 - Elide the controller for unlimited background hooks

Assigned subagent: Luna max

What and why:

Use the same nil-controller mode already supported by `Run` when a hook call
has zero limits and `ctx.Done() == nil`. This removes an allocation and enables
the later unrestricted-loop decision without weakening bounded execution.

Relevant files and modules:

- `program.go`
- `callback.go`
- `execution_control.go`
- `module_runtime.go`
- `vm.go`
- `limits_test.go`
- `execution_direct_safety_test.go`
- `runtime_budget_b3_test.go`, `runtime_budget_b8_test.go`, and
  `runtime_budget_b9_test.go`

Implementation steps:

1. Add one private execution-policy constructor that returns nil only when all
   execution limits are zero and the supplied context has no cancellation
   channel or current error.
2. Reuse that decision from `RunHook` and `Callback.Call`; do not duplicate
   policy tests at call sites.
3. Audit every controller method used through a possibly nil receiver,
   especially module initialization, runtime objects, generated strings,
   inherited frames, and call depth.
4. Keep a real controller for any nonzero limit, cancelable context, deadline,
   or callback invocation that needs those capabilities.
5. Do not pool or reuse a non-nil controller in this slice.

Tests and verification:

- exact instruction and call limit boundaries;
- module, object, table, generated-string, and coroutine budget tests;
- cancellation and deadline tests, including the 256-instruction poll bound;
- callback fresh-budget tests;
- persistent, bounded, and cancelable benchmark lanes;
- race and checkptr lanes plus the standard verification ladder.

Completion criteria:

- background zero-limit hook calls allocate no controller;
- bounded and cancelable behavior is unchanged;
- module and callback work share the intended controller when one exists;
- persistent allocation count drops by at least one with no runtime-family
  regression beyond noise.

Risks and edge cases:

- a context with `Err() == nil` can still have a non-nil `Done()` channel;
- nested module execution must not create a second independent budget;
- nil-controller behavior must not disable runtime object accounting when a
  nonzero object limit is configured.

#### Slice 2.3 - Carry invocation scope privately and wrap context lazily

Assigned subagent: Luna max

What and why:

Remove `context.WithValue` from every module and hook entry. Carry the current
runtime/module/globals/controller capability explicitly through private runtime
state, and create the compatibility context wrapper only when invoking a
`ContextHostFuncValue` that can call `CaptureCallback`.

Relevant files and modules:

- `runtime_call.go`
- `module_runtime.go`
- `callback.go`
- `base_env.go`
- `vm.go`
- host-call paths in `value.go` and base-library files
- callback and collector tests

Implementation steps:

1. Define a private `invocationScope` containing only runtime, caller context,
   module origin, captured host-global snapshot, and optional controller.
2. Store or pass that scope through the active `globalEnv`/VM call mechanism;
   do not expose it from the package.
3. Remove `contextWithRuntimeCallContext` from hook and module entry.
4. At the exact adapter that invokes `ContextHostFuncValue`, derive a wrapped
   context containing the scope so existing `CaptureCallback(ctx, value)` keeps
   working.
5. Plain host functions and pure script execution must not create the wrapper.
6. Make callback capture copy or root the same global snapshot and clear the
   active controller exactly as today.
7. Preserve nested `require` source module and inherited script-frame behavior.

Tests and verification:

- `callback_collector_test.go`;
- callback capture/call/close tests and runtime busy tests;
- nested host callback and nested `require` tests;
- context cancellation visibility inside context-aware host callbacks;
- runtime error inherited-frame tests;
- persistent and host-boundary allocation profiles;
- race/checkptr and standard verification ladder.

Completion criteria:

- common script-only hooks do not allocate a context wrapper;
- context-aware host callbacks still receive cancellation, deadlines, and a
  valid callback-capture context;
- `CaptureCallback` and `Callback.Call` public signatures are unchanged;
- callback roots, globals, module provenance, and runtime ownership are
  unchanged.

Risks and edge cases:

- wrapping too late can omit runtime stack context from a host error;
- wrapping too early preserves the allocation floor;
- a captured callback must not retain the invocation controller or mutable
  scratch environment.

#### Slice 2.4 - Remove the per-invocation bound require closure

Assigned subagent: Luna max

What and why:

Remove `nativeFuncValue(call.require)` from the steady-state path without
changing relative module resolution. This is a separate hypothesis from
reusing `globalEnv`: the current bound method value carries module provenance,
so replacing it with a package-level function blindly would change retained
`require` behavior.

Relevant files and modules:

- `runtime_call.go`
- `module_runtime.go`
- `program.go`
- `base_env.go`
- `module_resolver_test.go`
- `program_test.go`

Implementation steps:

1. Add tests that retain the `require` value, call it after its originating
   module returns, and resolve the same relative path from another module.
2. Measure whether the bound method value is still an allocation after Slices
   2.2 and 2.3; stop if it has disappeared through escape-analysis changes.
3. If it remains, cache the minimum stable adapter at the runtime/module seam.
   The adapter may retain runtime identity and `moduleKey`, but it must read
   the current caller context, controller, and host-global snapshot from the
   active private invocation scope.
4. Reuse the cached value for that module instead of constructing a bound
   method on every environment creation.
5. Keep relative resolution, inherited script frames, active-cycle detection,
   and lazy loading in the existing `runtimeCallContext.require` mechanism.
6. Clear cached adapters on `Runtime.Close` and include any retained references
   in owner collection if the chosen representation needs it.

Tests and verification:

- module cache, nil module result, active cycle, and relative require tests in
  `program_test.go` and `module_resolver_test.go`;
- retained `require` provenance across two different module origins;
- changing host globals, callback capture, owner collection, and runtime close;
- persistent allocation profile before and after only this change;
- race/checkptr and standard verification ladder.

Completion criteria:

- the bound `require` method value disappears from the common persistent
  allocation profile;
- retained `require` values preserve the current module-origin behavior;
- no context, controller, or host-global snapshot is retained past its call;
- any runtime/module cache has an explicit close and collection lifetime.

Risks and edge cases:

- a single package-level `require` function would resolve relative paths from
  the active caller rather than the module that supplied the retained value;
- caching a full `runtimeCallContext` would retain stale context, limits, and
  globals and is forbidden.

#### Slice 2.5 - Eliminate the common global environment allocation

Assigned subagent: Luna max

What and why:

After bound `require` is removed, eliminate fresh `globalEnv` allocation from
the nil-host steady-state path. Reuse existing VM/runtime lifetime owners only
after escape analysis proves where the environment actually lives.

Relevant files and modules:

- `base_env.go`
- `vm.go`, especially `vmThread.baseGlobals`, acquire, reset, and release
- `runtime_call.go`
- `module_runtime.go`
- `program.go`
- global-slot, module resolver, callback, and collector tests

Implementation steps:

1. Reprofile `runtimeGlobals`, `runtimeGlobalsWithOwner`, `envWithRequire`, and
   `copyGlobals` after Slice 2.4. Keep host-map copying separate from the
   nil-host environment allocation.
2. Prove with tests and escape analysis that script closures do not retain the
   invocation `globalEnv`; identify callback capture and internal native calls
   as explicit boundary cases.
3. Prototype the narrowest owner for reusable environment state: prefer the
   existing pooled `vmThread.baseGlobals` for VM execution and a stack-local
   environment for hook table access. Use runtime-owned depth-indexed scratch
   only if nested module execution cannot use the thread-owned lifetime.
4. Give any reusable environment an explicit acquire, reset, and release path.
   Clear host maps, cached values, slots, thread, controller, owner, and private
   invocation scope before reuse.
5. Preserve `RuntimeHost.Globals` freshness and `copyGlobals` isolation. A
   reusable destination map for non-nil hosts is a later measured hypothesis,
   not part of this slice.
6. Reset or invalidate global slots whenever host-global shape or environment
   version changes.
7. Make callback capture snapshot the needed globals and roots rather than
   retaining reusable environment storage.

Tests and verification:

- changing host-global values and shape between invocations;
- global slot and intrinsic invalidation;
- nested module loading and relative `require`;
- callback capture after the originating hook returns;
- forced GC, owner collection, runtime close, and busy-runtime behavior;
- compiler escape diagnostics for the relevant constructors;
- persistent and host-boundary allocation profiles;
- race/checkptr and standard verification ladder.

Completion criteria:

- the nil-host common path no longer allocates a `globalEnv` or values map;
- nested module execution cannot overwrite its caller environment;
- changing host globals are observed on the next call;
- reusable state retains no callback, table, host map, controller, context, or
  owner after release;
- non-nil host behavior is unchanged even if its map-copy allocation remains.

Risks and edge cases:

- a `globalEnv` pointer escaping into a retained value makes reuse unsafe;
- one runtime-global scratch value is insufficient for nested module loading;
- cached slots can serve stale host values if version reset is wrong;
- reusing a host map without fully clearing it can publish removed names.

#### Slice 2.6 - Add an error-only hook interface

Assigned subagent: Sol high design review, then Luna max implementation

What and why:

If the internal error-only benchmark proves the report is the last common
allocation source, add a small additive interface for steady-state embedding.
Keep `RunHook` as the reporting/diagnostic interface.

Recommended interface:

```go
func (r *Runtime) CallHook(ctx context.Context, hook string, args ...Value) error
```

Relevant files and modules:

- `program.go`
- `docs/public-surface.md`
- `docs/hearth-integration.md`
- `program_test.go`
- `runtime_benchmark_test.go`
- VM call-result helpers in `vm.go`

Implementation steps:

1. Sol high verifies that a Hearth-shaped caller needs error-only repeated hook
   invocation and that the name is consistent with the package surface.
2. Route `CallHook` and `RunHook` through the private core from Slice 2.1.
3. Preserve entrypoint order, lazy loading, skip behavior, error wrapping, host
   globals, and owner lease semantics.
4. Add a discard-result VM call path so hooks whose results are ignored do not
   allocate a public result slice.
5. Keep `RunHook` implementation behavior unchanged and do not implement
   `HookHandle`, `PrepareHook`, or `CallHookInto`.
6. Document that hook results are discarded across entrypoints and detailed
   per-entrypoint outcome requires `RunHook`.

Tests and verification:

- differential `RunHook` versus `CallHook` success/error/skip/order tests;
- lazy load, changing host globals, module errors, callback capture, close, and
  busy-runtime tests;
- allocation assertion for background, zero-limit, nil-host-global calls;
- persistent benchmark lanes using both interfaces;
- race/checkptr and standard verification ladder.

Completion criteria:

- the background, zero-limit, nil-host-global, error-only hook path is
  `0 B/op, 0 allocs/op` when the script performs no allocation;
- `RunHook` remains fully compatible;
- the additive interface is documented and backed by an embedding example;
- name lookup is reprofiled before any prepared-handle proposal.

Risks and edge cases:

- name lookup may be negligible after allocation removal;
- a prepared handle would need lazy-load, multi-entrypoint, host-freshness, and
  hook-mutation invalidation semantics that do not currently exist;
- ignored script results must still be closed/cleared correctly on errors.

Phase 2 gate:

- common `CallHook`: 0 B/op and 0 allocs/op;
- cancelable or bounded call: no more than one allocation unless a profile
  proves the remaining allocation belongs to the supplied context;
- no public behavior regression in `RunHook` or callbacks;
- Sol high reviews the final interface and ownership result before Phase 3.

### Phase 3 - Simplify the canonical fixed-call continuation

#### Slice 3.1 - Add targeted call mechanism counters

Assigned subagent: Luna high

What and why:

Extend existing instrumented `directFramePICCounts` only with facts needed to
choose the next fixed-call edit. Do not add the source draft's full speculative
counter inventory.

Relevant files and modules:

- `vm.go`
- `vm_dispatch_template.go.tmpl`
- `vm_dispatch_generated.go` as generated output
- `vm_flat_call_test.go`
- `frame_record_test.go`
- `vm_borrowed_window_test.go`

Implementation steps:

1. Count fixed-call attempts, successful record-only entries, and fallback
   reasons.
2. Distinguish immutable/encoding rejection from dynamic state: debug,
   protected call, coroutine, vararg/open result, cells/upvalues, overlap,
   stack growth, and limit transition.
3. Count continuation bytes pushed, physical frame materializations, argument
   copies, register slots cleared, and common one-result restores.
4. Emit counters only in the instrumented generated loop or test hooks.
5. Add a corpus report covering recursive Fibonacci plus method, closure,
   upvalue, protected, callback, coroutine, multi-result, open-result, and
   vararg shapes.

Tests and verification:

- generator freshness and `vm_dispatch_structure_test.go`;
- mechanism counter tests in `vm_flat_call_test.go`;
- production versus instrumented differential corpus;
- focused recursive CPU profile and call-family benchmarks;
- standard verification ladder.

Completion criteria:

- production generated source contains no new counter field, branch, or call;
- at least 95% of attempted fixed calls have a classified fast or fallback
  reason;
- the next slice names one dominant removable cost.

Risks and edge cases:

- instrumentation must not change borrow eligibility or frame layout;
- overlapping fallback reasons need a stable priority so totals reconcile.

#### Slice 3.2 - Move immutable fixed-call checks to finalization

Assigned subagent: Luna max

What and why:

Use existing verified wordcode and borrow hints to stop rechecking immutable
call-site facts at every call edge. Runtime guards remain for state that can
actually vary.

Relevant files and modules:

- `function_analysis.go`
- `bytecode.go`
- `wordcode.go`
- `vm_dispatch_template.go.tmpl`
- `vm.go`
- `proto_budget_test.go`
- bytecode verifier and malformed-prototype tests

Implementation steps:

1. Inventory each guard in `maybeEnterRecordOnlyFixedCall`,
   `enterRecordOnlyFixedCall`, and the one-result resume path.
2. Classify it as immutable verified wordcode, per-Proto finalization fact, or
   dynamic frame/runtime state.
3. Extend `analyzeFixedCallBorrowFacts` and existing operand markers first.
   Do not duplicate argument counts, destinations, or return PCs already in
   wordcode.
4. Add a side table only if required facts cannot be encoded or derived, and
   only after measuring its bytes per call site. Update the explicit Proto
   side-table budget rather than bypassing `proto_budget_test.go`.
5. Make the hot entry helper accept a verified call shape and retain only
   dynamic guards for callee identity/layout, active cells, protection,
   coroutine/debug state, owner window, stack growth, and limits.
6. Keep malformed bytecode rejection at sealing/finalization.
7. Retain the old guarded fallback during the experiment, compare behavior,
   then delete any duplicate accepted path before merge.

Tests and verification:

- verifier failures for every moved invariant;
- `vm_flat_call_test.go`, `frame_record_test.go`, and
  `vm_borrowed_window_test.go`;
- `phase5_open_argument_marker_test.go`,
  `phase5_open_argument_runtime_test.go`, `phase5_open_result_test.go`, and
  `phase5_vararg_test.go`;
- call limit, runtime error, callback, coroutine, race, and checkptr tests;
- recursive and broad call-family before/after profiles.

Completion criteria:

- immutable conversion/range checks disappear from the hot profile;
- recursive Fibonacci improves beyond noise and by at least 10%;
- no Scenario row regresses beyond the program-wide gate;
- metadata growth is reported and justified;
- one canonical call semantic path remains.

Risks and edge cases:

- dynamic callees mean callee register layout is not always a static call-site
  fact;
- hand-built test prototypes must pass through the same sealing verifier;
- trusting an unsealed or malformed Proto is a correctness and safety bug.

#### Slice 3.3 - Reduce common continuation restoration

Assigned subagent: Luna max

What and why:

Specialize restoration inside the existing canonical continuation model for
the measured common fixed-one-result record. Avoid resetting state that the
record and dynamic guards prove absent.

Relevant files and modules:

- `vm.go`, especially `resumeRecordOnlyFixedCallOne`, cleanup helpers, and
  physical frame rebinding
- `vm_dispatch_template.go.tmpl`
- `frame_record_test.go`
- `vm_borrowed_window_test.go`
- `vm_runtime_state_test.go`

Implementation steps:

1. Use Slice 3.1 counters to define the common shape. Do not infer it from the
   recursive benchmark alone.
2. Separate common restoration from rare protected, open-result, variadic,
   debug, and coroutine state through one tagged continuation model.
3. Restore only caller fields that changed during record-only entry.
4. Preserve closure/upvalue/cell reconstruction, source stack traces, call
   depth release, stack owner length, and borrowed-window cleanup.
5. Measure cleared slots before changing cleanup. With `Value` storage, do not
   introduce pointer/handle clear plans justified by a future slot model.
6. If a smaller common record is possible, keep rare fields in an explicitly
   extended form and update exact layout tests. Do not create a second call
   stack.
7. Clear every dropped reference range on normal return, error unwind,
   protected recovery, and coroutine transfer.

Tests and verification:

- zero-capture and captured recursive record tests;
- one-result, zero-result, multi-result, nil-fill, open-result, and open-argument
  tests;
- protected errors, source frames, callback calls, and coroutine yield/resume;
- forced GC/retained reference tests;
- recursive and broad call-family benchmarks and profiles;
- race/checkptr and standard verification ladder.

Completion criteria:

- one-result resume and frame-reset cumulative work falls beyond profile noise;
- recursive Fibonacci improves by at least another 10% or the slice is
  reverted;
- common continuation size does not exceed 48 bytes and pointer count does not
  exceed one; any claimed reduction is enforced exactly by tests;
- no duplicate frame restoration path remains after retention.

Risks and edge cases:

- stale values beyond a shortened slice still keep Go pointers reachable;
- abnormal unwind cleanup is as important as the normal return path;
- compacting fields can overflow on deep stacks or wide register windows.

#### Slice 3.4 - Retention review and phase close

Assigned subagent: Sol high

What and why:

Validate that the accepted call changes deepen the existing VM module rather
than creating parallel continuation semantics.

Relevant files and modules:

- all Phase 3 files and profiles
- ADR 0005
- `docs/design.md`

Implementation steps:

1. Review static versus dynamic checks and every fallback.
2. Confirm one generated opcode implementation and one continuation stack.
3. Compare recursive, call-family, full Scenario, persistent runtime, binary,
   generated source, and metadata results.
4. Delete losing experiments and temporary switches.
5. Record an ADR amendment only if the canonical continuation contract changed
   materially.

Tests and verification:

- complete standard verification ladder;
- race and checkptr;
- production/instrumented differential corpus;
- full performance audit comparison.

Completion criteria:

- retained work clears the noise-aware full-family gates;
- exact limits, callbacks, coroutines, errors, and ownership remain green;
- the old and new paths do not coexist after the decision.

Risks and edge cases:

- a large isolated Fibonacci win can hide regressions in closures, methods, or
  coroutines;
- smaller records are not valuable if added decoding restores the lost cost.

### Phase 4 - Dispatch decision gate

#### Slice 4.1 - Reprofile dispatch after invocation and call work

Assigned subagent: Luna high

What and why:

Do not optimize the old instruction mix. Refresh the aggregate Scenario and
policy-lane profiles after Phases 2 and 3.

Relevant files and modules:

- `performance-audit.md`
- `execution_control.go`
- `vm_dispatch_template.go.tmpl`
- `wordcode.go`
- runtime policy benchmarks

Implementation steps:

1. Capture full Scenario, persistent, bounded, cancelable, recursive, and cache
   heavy profiles.
2. Measure `stepInstruction`, generated loop, `valueKind`, `cacheSiteAt`,
   `cacheIDAt`, and property-cache shares.
3. Record dynamic opcode frequencies using existing instrumented counters.
4. Estimate recoverable time separately for unrestricted and controlled modes.
5. Stop if helper work is no longer material or is smaller than expected code
   size and review cost.

Tests and verification:

- complete performance audit comparison;
- no source change is required for a stop decision.

Completion criteria:

- Sol high receives a quantitative stop/go recommendation for Slices 4.2 and
  4.3;
- old Phase 0 percentages are not reused.

Risks and edge cases:

- a flat helper percentage can rise when surrounding code becomes faster;
- controlled and unrestricted profiles must not be combined.

#### Slice 4.2 - Prototype one unrestricted generated loop

Assigned subagent: Luna max, conditional on Slice 4.1

What and why:

If nil-controller instruction polling remains a broad cost, emit one
unrestricted loop with no per-instruction controller branch. Keep controlled
and instrumented behavior generated from the same canonical template.

Relevant files and modules:

- `vm_dispatch_template.go.tmpl`
- `vm_dispatch_spec.go`
- `cmd/ember-vmgen/main.go`
- `vm_dispatch_structure_test.go`
- `execution_direct_safety_test.go`
- `execution_differential_test.go`
- `vm.go` loop selection

Implementation steps:

1. Extend the generator along the existing production/instrumented mechanism;
   do not hand-copy opcode bodies.
2. Generate an unrestricted variant in which `stepInstruction` and controller
   state are structurally absent.
3. Keep one controlled production loop for cancellation and all limits, plus
   the existing instrumented behavior.
4. Select the variant once at execution entry from the policy established in
   Slice 2.2.
5. Add AST/source-structure tests proving semantic opcode cases remain aligned
   and unrestricted output contains no controller/poll markers.
6. Measure generated Go size, compiled binary size, build time, and full
   runtime families.
7. Delete the variant if the broad win does not pay for code-size and
   instruction-cache cost.

Tests and verification:

- generator freshness and structure tests;
- production/instrumented/unrestricted differential corpus;
- exact limits and cancellation in the controlled loop;
- race/checkptr and standard verification ladder;
- full audit comparison and generated/binary size report.

Completion criteria:

- unrestricted execution contains no per-instruction controller branch;
- full Scenario and persistent unlimited families improve beyond noise and by
  at least 5%;
- controlled and cancelable families do not regress beyond noise;
- generated source and binary growth are explicitly approved by Sol high.

Risks and edge cases:

- another generated loop increases instruction-cache and review surface;
- an unrestricted call must never be selected for a context with a `Done`
  channel or any nonzero limit;
- semantic drift is unacceptable even when code is generated.

#### Slice 4.3 - Inline only the retained direct cache sidecar lookup

Assigned subagent: Luna max, conditional on Slice 4.1

What and why:

If `cacheIDAt`/`cacheSiteAt` remain material, collapse their current direct
per-word sidecar loads at generated property opcodes. Do not reopen the deleted
rank/popcount representation.

Relevant files and modules:

- `wordcode.go`
- `vm_dispatch_template.go.tmpl`
- `wordcode_cache_sidecar_test.go`
- `vm_constant_field_pic_test.go`
- `vm_pic_test.go`
- `d5_string_field_probe_test.go`

Implementation steps:

1. Measure helper call and bounds-check cost after compiler inlining.
2. Prototype direct in-template loads using the verifier-proven physical word
   relationship.
3. Preserve the current dense sidecar layout and descriptor validation.
4. Add malformed/incomplete cache primary tests at the verifier seam.
5. Compare property-heavy families, full Scenario, Proto memory, and binary
   size.
6. Revert if bounds-check removal is not visible or only one synthetic row
   improves.

Tests and verification:

- sidecar layout and malformed-wordcode tests;
- PIC correctness, invalidation, equal string, metatable, and dynamic-key tests;
- full audit comparison and focused property profile;
- standard verification ladder.

Completion criteria:

- cache helper flat cost falls below noise or disappears;
- at least two independent property-heavy workloads improve beyond noise;
- full Scenario does not regress;
- sidecar memory does not grow.

Risks and edge cases:

- unsafe indexing is not acceptable; use verifier-backed ordinary Go bounds
  relationships;
- duplicating descriptor decode code can increase generated source more than
  the helper call costs.

#### Slice 4.4 - Controlled block accounting decision

Assigned subagent: Sol high, then Luna max only if approved

What and why:

Consider basic-block charging only if the controlled loop remains a material
bounded/cancelable cost after the unrestricted split. Existing compiler CFG
analysis is ephemeral; persisting block metadata consumes Proto budget.

Relevant files and modules:

- `function_analysis.go`
- `bytecode.go`
- `execution_control.go`
- generated dispatch files
- exact limit and cancellation tests

Implementation steps:

1. Quantify controlled workload demand and recoverable time.
2. Specify exact instruction semantics at a budget boundary and cancellation
   latency on long straight-line blocks.
3. Compare metadata encodings and report bytes per instruction/function.
4. Approve a separate implementation slice only if controlled modes improve
   materially without changing exact limits.

Tests and verification:

- exhaustive small instruction-budget boundary tests;
- cancellation latency tests;
- bounded/cancelable runtime lanes;
- Proto budget and binary size checks.

Completion criteria:

- either a documented stop decision, or an approved bounded slice with exact
  semantics and metadata budget;
- no basic-block machinery is added merely because CFG data already exists.

Risks and edge cases:

- block charging can overshoot a documented exact instruction limit;
- cancellation polling only at backedges can starve long straight-line code.

## 7. Conditional campaign decisions

These are not implementation phases. Each decision is a separate Sol high
slice. Approval produces a new plan rather than appending unbounded work to the
current program.

### Decision 5.1 - Canonical private-slot cutover

Assigned subagent: Sol high architecture decision; Luna max only after approval

Trigger:

- post-Phase 4 profiles still attribute at least 10% of broad runtime time to
  `Value` classification/transport or demonstrate material Go-GC scanning cost;
- cheaper call, policy, and cache work is exhausted;
- a complete-corpus estimate supports at least a 10% runtime-family win.

Required design before implementation:

1. runtime-local slot constants for each shared immutable `Proto`;
2. slot registers, stack, calls, globals, cells, closures, caches, and
   coroutines;
3. a decision for Go-owned public tables versus runtime-owned slot tables;
4. root, pin, generation, collection, callback, retained-result, and close
   semantics;
5. one bounded integration branch that deletes the `Value`-backed runtime path
   before merge;
6. an ADR 0004 update only after the full cutover clears its gate.

Do not merge a sequence of mixed-representation states that turns every table,
global, or callback access into a conversion seam. Do not store owner-relative
reference slots directly in shared `Proto` objects.

### Decision 5.2 - Ordered hash-table redesign

Assigned subagent: Sol high architecture decision; Luna max only after approval

Trigger:

- a refreshed persistent workload that continues inserting and deleting shows
  hash growth or iteration bookkeeping as a top-three time/allocation source;
- the projected full-family gain exceeds noise and is at least 5%;
- ADR 0006 is explicitly reconsidered.

Required semantic proof:

- mixed array, inline-string, and overflow-hash insertion order;
- update keeps position;
- delete/reinsert appends a new position and publishes the new string box;
- equal-content string boxes and identity keys;
- mutation during iteration and invalid `next` keys;
- small-table footprint and retained memory.

Required staging for the approved follow-up plan:

1. Luna max first adds test-only or instrumented counters for overflow-hash
   growth, rehashes, probe lengths, journal growth, index construction,
   tombstones, and compaction. Production execution must not pay for them.
2. Re-run sparse-grid, retained-result, dynamic-string-growth, and persistent
   hook families. If bookkeeping is not material outside one stateless row,
   stop.
3. If justified, run one reversible `value.go` experiment that shares lookup
   position/index data with overflow-hash storage while retaining the journal
   for arrays and inline string fields. Do not combine it with an allocator.
4. Evaluate `emitter.go` table-capacity hints through a separate capacity-only
   experiment; do not hide emitter changes inside a table-layout result.
5. Require all existing iteration, identity, clear, hash-generation, numeric
   zero, array-density, and D9 capacity tests before a Sol high retention
   review.

Do not start with allocator slabs or nursery pages. First prove the lookup/order
data structure independently on the current canonical representation.

### Decision 5.3 - Nursery, graph promotion, or owner-bound results

Assigned subagent: Sol high mandatory throughout

Trigger:

- a canonical slot ownership model has been accepted;
- persistent and retained-result profiles overturn ADR 0006;
- runtime-owned allocation remains a top-three limiter after table layout work.

Required design before implementation:

- backing page ownership and interaction with ordinary Go pointers;
- graph promotion for cycles, sharing, metatables, closures, cells, userdata,
  callbacks, and coroutines;
- generation changes on reset and stale-handle behavior;
- resident-memory cap and release policy;
- ordinary durable `Run` results before any owner-bound result interface.

Do not add `RunOwned` merely to avoid solving safe ordinary results. A
closeable result is a public lifetime footgun and needs a separate embedding
use case.

### Decision 5.4 - Kind specialization, inlining, and fused opcodes

Assigned subagent: Sol high stop/go review; Luna max per approved candidate

Trigger:

- the canonical representation and call path have stabilized;
- new instruction-frequency and kind-check counters show a broad repeated
  pattern;
- existing global slots, `opFastCall`, constant/register/slot kind detectors,
  and PICs cannot already consume the fact.

Each candidate must be one experiment with a code-size budget, source/limit
semantics, two independent workload families, and a deletion condition. Hot/
cold switches and superinstructions have a high bar because ADR 0005 deleted a
direct-loop kernel that did not produce broad wins.

### Decision 5.5 - Native or baseline tier

Assigned subagent: Sol high RFC only

Trigger:

- the optimized canonical interpreter remains materially behind the chosen
  external reference on a persistent broad corpus;
- at least 30% of remaining time is proven dispatch/generic mechanics;
- compile latency, executable memory, deoptimization, source maps, limits,
  callbacks, coroutines, and GC integration have explicit budgets.

No implementation begins from this plan. If triggered, write a separate RFC
and a complete differential-testing plan. A narrow benchmark engine is not an
acceptable retained tier.

## 8. Delivery order and stop conditions

Required order:

1. Phase 0 measurement contract;
2. Phase 1 compiler assignment walk and stop/cache decision;
3. Phase 2 persistent invocation boundary;
4. Phase 3 canonical fixed-call continuation;
5. Phase 4 dispatch reprofile and only the approved experiments;
6. conditional campaign decisions.

Stop or revert a line of work when:

- the refreshed profile no longer shows the target as material;
- the result does not exceed the measured noise envelope;
- the win is isolated to one synthetic row;
- code or metadata growth is disproportionate to the full-family result;
- correctness needs hot checks equal to the work removed;
- ownership or lifetime behavior cannot be mechanically tested;
- retention requires a second semantic engine or indefinite dual paths;
- two bounded attempts fail the same gate.

Deleting a failed experiment is a successful outcome. Moving to the next phase
without clearing the current gate is not.

## 9. Per-slice handoff template

Every coding agent leaves this report:

```text
slice:
assigned agent:
baseline commit:
candidate commit:
environment fingerprint:
hypothesis:
changed files and symbols:
correctness checks:
focused benchmark command and raw artifact:
median / spread / B/op / allocs/op before and after:
full family and Scenario comparison:
CPU/allocation profile movement:
generated source / binary / metadata size movement:
risks and intentionally untouched work:
keep, revise, or revert:
next gate:
```

## 10. Definition of done for this plan

The committed program is complete when:

- current baseline and comparison artifacts are reproducible;
- assignment analysis is symbol-aware, single-walk, and no longer a material
  compiler profile node;
- background zero-limit error-only hook calls reach 0 B/op and 0 allocs/op;
- `RunHook`, callback, module, ownership, cancellation, and limit behavior stay
  compatible;
- fixed-call entry/resume costs improve materially across a broad call family
  while one continuation model remains;
- dispatch receives an explicit measured stop/go result;
- every losing experiment and temporary selector is deleted;
- all relevant focused tests, `scripts/check-lane root`, `scripts/check-fast`,
  `scripts/check`, race, and checkptr checks pass;
- slot, table, nursery, specialization, and native work is either rejected with
  evidence or moved into a separately approved plan.

This plan intentionally does not promise a preselected 0.50x total runtime
ratio. The first refreshed baseline and every retained slice must earn the next
target. If the committed phases do not approach the desired multiple-factor
improvement, the next architecture decision will be based on the new profiles,
not the old roadmap.
