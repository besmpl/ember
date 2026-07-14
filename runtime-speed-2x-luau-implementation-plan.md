# Ember Runtime Speed: At Most 2x Luau

Status: implementation plan, not yet executed

Created: 2026-07-14

Target platform: Darwin 24.6.0, Apple M1, arm64, Go 1.26.4, CGO disabled,
GOMAXPROCS=1, pinned Luau 0.728 at SHA-256
c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd,
Luau interpreter at its default optimization level, and no Luau codegen.

This plan supersedes performance-optimization-implementation-plan.md for new
runtime-speed work. Completed slices in that document remain historical
evidence. Do not delete the old document until its completed work and rejected
experiments have been linked from the performance audit.

## Outcome

Make Ember's steady-state runtime no more than 2.00 times slower than the
pinned Luau interpreter on every frozen representative workload, not merely on
an average. Use 1.85 times as the engineering margin so normal measurement
noise does not turn a nominal pass into a regression.

The target is complete only when all 37 unique workloads from the Top10,
Classic, and Scenario corpora satisfy both of these conditions in a clean,
paired slope capture:

- the median of nine paired slope ratios is at most 1.85;
- the nearest-rank p90 of nine paired slope ratios is at most 2.00.

No geomean, corpus median, exception list, or benchmark-specific waiver may
hide a failing row. Geomean and worst-row summaries are useful diagnostics but
are not substitutes for the per-row gate.

Allocation count and bytes are not optimization objectives in this program,
but speed may not be bought with recurring hot-path allocation. Split the gate
by lifetime:

- warmed steady-state execution, internal script calls, loop iterations, and
  repeated hooks may not exceed the exact per-row B/op or allocs/op ceilings
  derived from two clean same-source Ember audit captures;
- one-time executionImage construction may add at most one aggregate allocation
  plus one allocation per executable Proto, and at most 32 bytes per word plus
  512 bytes per Proto above the cold baseline, after old sidecars are removed;
- a retained native tier may add at most one metadata allocation per executable
  Proto and page-rounded executable storage bounded by 32 bytes per word plus
  4 KiB per Proto. It must also demonstrate warm-up break-even within 100 case
  invocations or 10,000 dynamic instructions, whichever is the relevant unit;
- no allowed cold allocation may recur per instruction, internal call, loop
  iteration, resumed coroutine, or warmed hook.

These cold caps are ceilings, not budgets to spend automatically. A cold-cost
increase is retained only when the associated architecture meets its speed
gate and the cost is visible in cold benchmarks. Peak retained memory and
executable bytes are recorded and bounded against runaway growth, but otherwise
memory optimization is deferred.

Backwards compatibility is explicitly subordinate to this target. Break the
Go API, private bytecode layout, and runtime ownership model when that creates
a substantially deeper and faster runtime module. Preserve Luau language
semantics, deterministic table behavior, execution limits, cancellation,
errors, callbacks, coroutines, and host isolation unless a separate language
compatibility decision explicitly changes them.

## Current evidence and its limits

At plan creation, HEAD is 1b549d7d. The worktree also contains user-owned,
uncommitted runtime work in base_coroutine.go, base_env.go, callback.go,
module_runtime.go, program.go, runtime_call.go, runtime_heap.go, and vm.go, plus
untracked planning documents. Preserve all of it. The first clean baseline
must be taken only after the owner intentionally lands, replaces, or otherwise
resolves those edits. A dirty diagnostic capture may guide work but can never
be an acceptance artifact.

The current authoritative Ember-only evidence is performance-audit.md. Its
clean A/B captures show, among other rows:

- combat_tick: 18,298 ns/op, 1,952 B/op, 11 allocs/op;
- event_dispatch: 75,840 ns/op, 2,480 B/op, 15 allocs/op;
- sparse_grid_neighbors: 2,457,809 ns/op, 39,174 B/op, 71 allocs/op;
- recursive_fibonacci: 3,080,499 ns/op, 232 B/op, 4 allocs/op.

Those numbers are Ember regression baselines, not Luau ratios. The existing
performance audit must remain the allocation and Ember-relative regression
gate.

A current-tree exploratory batch run suggested the following ratios:

| Workload | Exploratory Ember/Luau ratio |
| --- | ---: |
| combat_tick | 2.446 |
| event_dispatch | 5.557 |
| prototype_fallback | 7.786 |
| command_vararg_router | 6.603 |
| arithmetic_for | 2.120 |
| table_fields | 1.919 |
| closures_upvalues | 2.653 |
| recursive_fibonacci | 6.995 |

The eight-row exploratory geomean was 3.893. These figures are triage only:
they used the batch harness rather than the official paired slope harness and
were captured from a dirty tree. Replace them in Phase 0. The old
tmp/runtime-parity/exploratory-full data is rejected v1 busy-runner evidence
and must never be cited as a current baseline.

Existing profiles identify several credible mechanisms:

- the generated direct loop is the largest cumulative CPU island;
- executionWindow.stepInstruction and valueKind recur in hot profiles;
- fixed script calls repeatedly validate, enter, resume, clear, and reconstruct
  frame state in enterRecordOnlyFixedCall and resumeRecordOnlyFixedCallOne;
- property cache, table lookup, and metatable paths dominate field-heavy rows;
- sparse rows spend heavily in hash growth, ordered iteration storage, and
  table storage;
- the private slot is 8 bytes while the canonical Value transported through
  registers, stacks, tables, globals, closures, and calls is 16 bytes.

These are hypotheses about leverage, not permission to retain a change. Each
slice below has a measured retention gate.

## Architectural decision

The target architecture is one deep runtime execution module behind a narrow
host boundary:

    immutable Program / Proto
                |
                v
    Runtime-owned executionImage
      - executable prototype records
      - runtime constants
      - global and native descriptors
      - call descriptors
      - property-chain descriptors and cache state
      - quickened words, if retained
                |
                v
    canonical execution backend
      - unrestricted interpreter
      - controlled/instrumented interpreter
      - optional native adapter only if the interpreter misses the target
                |
                v
    one flat value stack + one continuation stack + runtime-owned heap/tables
                |
                v
    explicit host adapters and durable export/pin boundary

The executionImage is the seam. Proto stays immutable and shareable; it never
contains owner-relative values or mutable cache state. Runtime materializes
messy compiler and host inputs into owner-local, execution-ready data once.
Hot instructions address image records directly and do not perform map lookup,
public Value conversion, global-name decoding, or repeated call-shape
validation.

This is a replace-don't-layer design:

- Do not revive the deleted alternate VM engines.
- Do not leave a mixed Value/slot steady state after a slot cutover.
- Do not add independent side tables for each optimization; fold runtime-local
  state into executionImage.
- Do not create a second copy of effectful or complex Luau semantics for native
  execution. Tables, metamethods, errors, calls, host effects, callbacks, and
  coroutines remain in shared semantic helpers. Scalar and control rules needed
  by a backend must be generated from shared definitions and differentially
  tested rather than hand-maintained as an unrelated second specification.
- Do not split the root package merely to make the change look architectural.
  Add private root files when they improve locality. A package seam must be
  justified by an independently useful interface.

## Non-goals

- Compiler throughput work unrelated to execution-image construction.
- More RunHook report or lease allocation tuning before steady-state parity.
- A custom nursery or allocator based only on allocation profiles.
- Shrinking vmFrameRecord without removing frame reconstruction work.
- Narrow benchmark-pattern superinstructions without dynamic coverage.
- A dependency addition. If the native path truly requires one, stop and ask
  before changing go.mod.
- scripts/check-full; project instructions reserve it for an explicit request.

## Agent strategy

The program is broad and high risk. Use agents dynamically, with one file owner
per slice. Every worker must re-read AGENTS.md, preserve concurrent user work,
and avoid reverting edits outside its assignment.

| Work | Assigned agent | Reason |
| --- | --- | --- |
| Measurement scripts and deterministic statistics | Luna high | Bounded tooling work with exact tests |
| Execution image, call ABI, slot/table cutover | Luna max | Large cross-cutting runtime changes |
| Architecture and retention-gate review | Sol high | Challenge seams and reject attractive local wins |
| Focused compatibility/corpus tests in disjoint test files | Luna high | Parallelizable test ownership |
| Native feasibility and backend design | Sol high, then Luna max | High-risk decision first, implementation second |
| Final integrated review | Sol high | Check target evidence, ownership, and deleted paths |

Parallel work is safe only when ownership is disjoint. In particular, do not
assign vm.go, vm_dispatch_template.go.tmpl, bytecode.go, or value.go to two
workers at once. Test workers may add new corpus or oracle files while the
implementation owner edits runtime files, but they must coordinate names and
public contracts before starting.

## Dependency graph and checkpoints

    Phase 0: trustworthy target gate and baseline
       |\
       | +--> early Darwin/arm64 native feasibility probe
       v
    Phase 1: executionImage + unrestricted loop
       v
    Phase 2: global/property descriptors
       v
    Phase 3: flat call and continuation ABI
       v
    Checkpoint A: stop if every row already passes
       |
       +--> table branch when table/property CPU remains material
       +--> slot branch when Value transport/classification remains material
       |       (combine these branches when both trigger)
       v
    Phase 5: measured quickening and general specializations
       v
    Checkpoint B: stop if every row passes
       |
       +--> mandatory native-tier decision/prototype if any row fails
              \--> re-enter slot boundary/cutover as the same retention group
                   when Checkpoint A skipped slots
       v
    Phase 7: delete superseded paths, full verification, final evidence

The numbered phases express dependency, not permission to batch months of work
into one merge. Each slice is independently reviewed and retained only when it
passes its gate.

## Phase 0: Make the target measurable

### Slice 0.1 - Reconcile the baseline source and freeze the corpus

Assigned agent: Luna high.

Objective and reason: establish one clean source fingerprint and one frozen
37-row workload inventory before optimizing. Current diagnostic ratios are not
acceptance evidence, and the existing full parity phase covers only the 25
Scenario rows.

Files and symbols:

- runtime_parity_test.go: parityCaseSelection, TestRuntimeParityLive,
  parityRawHeader, inspectParityEnvironment;
- top10_luau_benchmark_test.go: top10LuauCases, classicLuauCases,
  scenarioLuauCases;
- scripts/check-runtime-parity: phase selection, fingerprint_manifest,
  current_fingerprint;
- scripts/scenario-ratio-gate: frozen case inventory and expected results;
- docs/checks.md and performance-audit.md;
- new docs/adr/0007-runtime-speed-target.md.

Ordered work:

1. Stop if git status still contains unresolved user-owned edits that would
   change runtime behavior. Ask the owner which resulting commit is the
   baseline; never reset or absorb the edits implicitly.
2. Derive and test a unique list of all Top10, Classic, and Scenario case names.
   Fail closed on duplicate names, missing expected outputs, or inventory drift.
3. Add a named speed2x parity phase that captures all 37 rows. Keep the old
   faster-than-Luau thresholds as an explicitly named stretch report rather
   than silently weakening or deleting them.
4. Record Luau default optimization and no-codegen mode in the raw header and
   source fingerprint. Verify the launched command rather than relying on an
   undocumented default.
5. Record the clean Ember commit, source fingerprint, Go version, OS, CPU,
   CGO_ENABLED, GOMAXPROCS, Luau path/version/hash, iteration points, repeat
   count, pair count, and case inventory in every acquisition.
6. Write ADR 0007 with the per-row 1.85 median and 2.00 p90 target, exact hot
   allocation non-regression rule, bounded cold image/native caps, breaking-API
   authority, and conditional native fallback.
7. Update docs/checks.md with the exact speed2x command and artifact locations.

Verification:

    go test -run '^(TestRuntimeParity|TestTop10Luau)' -count=1 .
    scripts/check-runtime-parity --self-test
    git diff --check

Completion criteria:

- One deterministic test proves the inventory is exactly 37 unique cases.
- A raw artifact cannot be reused across a corpus, source, Luau mode, or
  environment change.
- speed2x and stretch are visibly different commands and reports.
- ADR 0007 makes worst-row success non-negotiable.

Risks and dependencies:

- The current worktree state is a hard dependency, not a reason to discard
  user work.
- If corpus names overlap, give cases stable corpus-qualified IDs instead of
  dropping a case.

### Slice 0.2 - Strengthen the paired slope statistic

Assigned agent: Luna high, with Sol high reviewing the gate before retention.

Objective and reason: reject noisy or nonlinear fits and make the hard target
about the workload's inner execution cost rather than startup.

Files and symbols:

- runtime_parity_test.go: parityFit, fitParityLine, parityRatio,
  summarizeParityRepeatRatios, summarizeParityRatios;
- scripts/scenario-ratio-gate: fit_line and final aggregation;
- scripts/check-runtime-parity: acquisition and re-gating;
- new runtime_parity_gate_test.go if keeping gate fixtures separate improves
  locality.

Ordered work:

1. Extend parityFit with residual sum of squares, R-squared, slope standard
   error, and a 95 percent relative slope confidence width.
2. Implement the same equations in Go and in the shell gate's awk program.
   Cross-check them with deterministic fixtures, including negative intercept,
   noisy slope, nonlinear data, zero slope, and contaminated acquisition.
3. Start with R-squared at least 0.995 and relative 95 percent slope width at
   most 10 percent. If the clean baseline cannot meet that, add larger N points
   or repeats. Do not weaken below R-squared 0.98 or 15 percent relative width
   without a new ADR containing evidence.
4. For each case, retain the existing nine pair ratios after each pair has
   medians across three repeats. Gate that case's median at 1.85 and p90 at
   2.00. Report the corpus geomean and worst case without using either to excuse
   a row.
5. Add an explicit --row-p90-max argument and reject an input whose case set is
   incomplete, duplicated, non-finite, contaminated, or not the fingerprinted
   inventory.
6. Preserve raw acquisitions on a performance failure and reject/reacquire on
   statistical invalidity or contamination.

Verification:

    go test -run '^TestRuntimeParity' -count=1 .
    scripts/check-runtime-parity --self-test
    scripts/performance-audit-compare-test

Completion criteria:

- Go and awk produce the same fit and summary within a documented tolerance.
- A single 2.01 p90 row fails even if every other row is faster than Luau.
- Bad fit quality causes reacquisition rather than a performance verdict.
- Entry cost is reported but only inner slope enters the speed ratio.

Risks and dependencies:

- Four timing points may be insufficient for very small cases; increasing N is
  preferable to accepting an unstable fit.
- Do not compare Ember process time with Luau process startup. The current
  wrappers deliberately compare the fitted inner loop.

### Slice 0.3 - Add low-distortion mechanism counters and capture baseline

Assigned agent: Luna max for runtime counters and Luna high for capture tooling,
with exclusive ownership of runtime files during the slice.

Objective and reason: attribute each failing row to mechanisms so later branch
decisions are evidence-based.

Files and symbols:

- vm.go: vmThread, directFramePICCounts, fixed-call enter/resume paths;
- vm_dispatch_template.go.tmpl and vm_dispatch_generated.go;
- execution_control.go: executionWindow;
- property_ic.go, table_ops.go, value.go tableHashFields;
- runtime_parity_test.go and scripts/check-runtime-parity;
- runtime_benchmark_test.go and top10_luau_benchmark_test.go;
- scripts/performance-audit, scripts/performance-audit-derive-manifest,
  scripts/performance-audit-compare, and
  scripts/performance-audit-compare-test;
- performance-audit.md.

Ordered work:

1. Define one test/diagnostic-only counter sink selected outside the hot loop.
   The nil/unrestricted loop must contain no per-instruction counter branch.
2. Count dynamic opcodes/basic blocks, call shapes and fallbacks, frame
   clears/copies, global descriptor hits/fallbacks, property-chain cache states,
   table probes/grows, controller polls, coroutine suspend/resume copies, and
   Value kind classifications.
3. Generate an instrumented loop from the same dispatch template. Do not hand
   edit vm_dispatch_generated.go.
4. Add a deterministic counter report keyed by frozen case and source
   fingerprint.
5. Add an explicit lifetime class to every audit row and its manifest entry.
   Class repeated Runtime/hook/call/iteration rows as hot; class one-shot
   scenario_ember_run, recursive_fibonacci, sparse_grid_neighbors,
   RuntimeLaneStatelessRun, Runtime/image creation, and first-call rows as cold.
   Fail closed on a missing or changed class.
6. Extend derive-manifest and compare so hot rows use exact observed B/op and
   allocs/op ceilings, while cold rows carry measured Proto/word counts and
   apply the explicit construction formulas from this plan. Timing envelopes
   remain row-specific for both classes.
7. Add comparator self-tests proving a permitted one-time cold image increase
   passes, the same recurring increase on a hot row fails, a cold increase over
   its formula fails, and missing lifetime metadata fails.
8. From one clean baseline commit, take two Ember audits with profiles and
   derive the lifetime-aware allocation manifest. Add cold image-build, Runtime
   creation, and first-call rows that report Proto/word counts.
9. On a quiet pinned runner, take the official speed2x acquisition for all 37
   rows plus focused CPU profiles for every row above 2.00 and at least one
   passing control from each family.
10. Update performance-audit.md with measured ratios, fit diagnostics, mechanism
   counters, and profile paths. Label all older batch evidence exploratory.

Verification:

    go generate ./...
    go run ./cmd/ember-vmgen -check
    go test -run '^(TestVMDispatch|TestRuntimeParity|TestExecutionDifferential)' -count=1 .
    scripts/performance-audit-compare-test
    scripts/performance-audit --output /tmp/ember-speed2x-base-a --profiles
    scripts/performance-audit --output /tmp/ember-speed2x-base-b --profiles
    scripts/performance-audit-derive-manifest --before /tmp/ember-speed2x-base-a --after /tmp/ember-speed2x-base-b --output /tmp/ember-speed2x-gates.tsv
    scripts/check-runtime-parity --phase speed2x

Completion criteria:

- The counter-disabled unrestricted candidate has no counter branch in its
  generated loop and is statistically indistinguishable from counters absent.
- Every failing row has a ranked mechanism attribution with CPU samples and
  dynamic counts, not only a cumulative pprof percentage.
- Every audit row has a tested lifetime class. The lifetime-aware allocation
  manifest, cold-construction baselines, and parity baseline share one clean
  source fingerprint.
- The comparator accepts only formula-bounded cold construction and never
  treats recurring hot allocation as cold.

Risks and dependencies:

- Counters perturb execution. They diagnose relative frequency; only
  counter-disabled captures decide retention.
- Cumulative pprof percentages overlap and must not be summed.

### Slice 0.4 - Prove or reject Darwin/arm64 native execution feasibility

Assigned agent: Sol high for the probe design, then Luna max for the bounded
probe. Delete probe code before completing the slice.

Objective and reason: remove late program risk while native code is still only
a contingency. The diagnostic worst rows are far enough above target that an
interpreter-only promise would be speculative.

Files and symbols (temporary probe files are deleted before completion):

- temporary native_probe_darwin_arm64_test.go;
- temporary native_probe_arm64.s;
- temporary generic_arm64_backend_probe_test.go when runtime JIT is unavailable;
- docs/adr/0007-runtime-speed-target.md, native feasibility appendix;
- no go.mod or go.sum change.

Ordered work:

1. Specify the smallest probe: allocate a page, transition it through a valid
   W^X lifecycle, emit a constant-return arm64 function, make instruction-cache
   visibility explicit, call it through a tiny assembly trampoline, and release
   the page.
2. Run under the same unsigned/signed development conditions used by Ember.
   Record whether MAP_JIT, entitlements, cgo, or private platform calls are
   required.
3. Measure creation, protection transition, call overhead, and executable-byte
   accounting. Exercise repeated creation and cleanup under the race detector
   where supported.
4. Delete the executable probe and assembly trampoline after recording the
   result. The slice delivers a decision, not dormant production machinery.
5. If dependency-free W^X execution is not viable, do not declare a generic
   assembly interpreter viable by name. Either measure a real generic arm64
   backend that consumes executionImage records, or specify and validate an
   explicit AOT Program compile/load/close API whose artifact lifecycle fits
   Hearth deployment. Record its measured dispatch floor and the dynamic
   coverage it could provide on current failing rows.
6. For every candidate, trace an actual lifecycle from Program or Proto through
   prepare, executable artifact ownership, entry, side exit, and cleanup. If no
   candidate can plausibly supply the required coverage/gain, record the native
   contingency as blocked. If a dependency is required, stop and ask rather
   than adding it.

Verification:

    go test -run '^TestNativeProbe' -count=100 .
    go test -race -run '^TestNativeProbe' -count=1 .
    git diff --check

Completion criteria:

- ADR 0007 states feasible/not feasible, exact platform requirements, cleanup
  behavior, and measured fixed costs.
- No probe implementation remains in the production tree.
- The later native decision has one exercised artifact/lifecycle mechanism with
  measured costs and coverage, or an explicit external blocker. A mechanism
  name without executable evidence is not sufficient.

Risks and dependencies:

- Darwin arm64 instruction-cache coherency and hardened runtime rules are easy
  to get subtly wrong. A successful one-off call without repeated W^X and
  cleanup tests is not proof.
- This slice does not authorize a production JIT.

## Phase 1: Build the execution module and remove unrestricted-loop policy work

### Slice 1.1 - Introduce one runtime-local executionImage

Assigned agent: Luna max, followed by Sol high interface review.

Objective and reason: convert immutable Proto and host state into one clean,
owner-local execution representation before entering the hot loop. This creates
the deep seam needed by descriptors, quickening, slots, and an optional native
adapter without proliferating side tables.

Files and symbols:

- new runtime_image.go: executionImage, executableProto, imageProtoID,
  materializeExecutionImage;
- bytecode.go: Proto and finalization metadata;
- program.go: Program, Runtime, NewRuntime, Close;
- runtime_owner.go and runtime_heap.go;
- vm.go: vmThread, executeProtoWithInvocationScope, frame proto access;
- vm_runtime_state_test.go, execution_differential_test.go;
- new runtime_image_test.go.

Ordered work:

1. Define executionImage as private and owned by one runtimeOwner. Give each
   Proto a dense imageProtoID during materialization. A map from Proto pointer
   to ID is allowed only in cold construction and cold compatibility paths.
2. Define executableProto with verified words, dense child IDs, register count,
   constants/descriptors, call metadata, cache offsets, and source/debug links.
   Keep owner-relative values out of Proto.
3. Consolidate existing runtime-local PIC/cache sidecars into image-owned
   storage. Replace independent maps or slices rather than wrapping them.
4. Make vmThread and hot frames carry an image and imageProtoID. Reload hot
   arrays from the executable record; do not repeatedly validate Proto.verifyErr
   inside a verified execution.
5. Materialize one image per Runtime. For the cold Run(proto) API, either build
   a short-lived runtime/image or replace the API with an explicit Runtime
   lifecycle. Choose the narrower interface even if this breaks compatibility.
6. Keep compatibility adapters cold and clearly named. No adapter conversion is
   allowed per instruction or per internal script call.
7. Differentially execute the whole corpus through the old and image paths
   during development, then delete the old materialization path before merge.

Verification:

    go test -run '^(TestRuntimeImage|TestExecutionDifferential|TestRuntimeOwner|TestProgram)' -count=1 .
    go test -race -run '^(TestRuntimeOwner|TestCallback|TestCoroutine)' -count=1 .
    go test -gcflags=all=-d=checkptr=2 -run '^TestExecutionDifferential' -count=1 .
    scripts/performance-audit --output /tmp/ember-speed2x-image --profiles
    scripts/performance-audit-compare --before /tmp/ember-speed2x-base-a --after /tmp/ember-speed2x-image --manifest /tmp/ember-speed2x-gates.tsv

Completion criteria:

- One owner-local module contains all execution descriptors and cache state.
- Hot execution reaches executableProto without a map lookup.
- Proto remains immutable and safe to share between runtimes.
- Old runtime PIC/cache sidecars and repeated per-entry Proto verification
  replaced by the image are deleted rather than wrapped.
- The hot path adds no pointer hop or guard relative to the path it replaces,
  no warmed row exceeds its exact allocation ceiling, and cold construction is
  within the image cap.
- No parity row is slower outside its statistical envelope. If this independent
  structural deletion gate cannot pass, keep the work uncommitted through
  Slices 2.1 and 2.2 and retain the group only when the combined descriptor
  module passes its speed/deletion gates. Never merge a neutral or regressive
  image merely because later work expects it.

Risks and dependencies:

- A shallow executionImage that merely points at every old structure adds
  indirection without leverage and must be rejected.
- Runtime ownership and exported values are the difficult seam. Do not smuggle
  owner-relative handles into shared Proto constants.

### Slice 1.2 - Generate a truly unrestricted loop

Assigned agent: Luna max.

Objective and reason: remove controller/poll policy entirely from the common
unbounded loop. The current production loop always constructs an
executionWindow and calls stepInstruction once per instruction even when its
controller is nil.

Files and symbols:

- vm_dispatch_template.go.tmpl: runDirectFrameProductionLoop template body;
- cmd/ember-vmgen/main.go;
- vm_dispatch_generated.go;
- vm_dispatch_structure_test.go;
- vm.go: direct-loop selection and side exits;
- execution_control.go: executionWindow only for controlled execution.

Ordered work:

1. Generate three explicit variants from one semantic template: unrestricted,
   controlled, and instrumented. Do not make one loop branch on a mode flag.
2. Select the variant once at execution entry and at a side exit where policy
   changes. The unrestricted generated function must not reference
   executionWindow, stepInstruction, controller, debug hooks, or counter sinks.
3. Keep instruction accounting and cancellation exact in controlled execution,
   including nested calls, callbacks, requires, errors, and coroutine resume.
4. Add structural source tests that inspect the generated unrestricted function
   for forbidden markers and confirm every opcode appears in all semantic
   variants.
5. Compare counter-disabled profiles and all 37 ratios against the clean
   baseline.

Verification:

    go generate ./...
    go run ./cmd/ember-vmgen -check
    go test -run '^(TestVMDispatch|TestExecutionControl|TestExecutionDifferential)' -count=1 .
    scripts/check-runtime-parity --phase speed2x
    scripts/performance-audit --output /tmp/ember-speed2x-unrestricted --profiles
    scripts/performance-audit-compare --before /tmp/ember-speed2x-base-a --after /tmp/ember-speed2x-unrestricted --manifest /tmp/ember-speed2x-gates.tsv

Completion criteria:

- Forbidden policy symbols are structurally absent from unrestricted generated
  code.
- Failing-row geomean improves by at least 4 percent.
- No individual parity row slows by more than 3 percent beyond its statistical
  envelope.
- Warmed allocation ceilings, applicable cold caps, and all controlled-mode
  behavior remain unchanged.

Risks and dependencies:

- Code generation can duplicate semantics accidentally. All variants must come
  from one opcode definition/template and pass differential tests.
- This is expected to be useful but insufficient by itself.

## Phase 2: Make globals and property chains descriptor-driven

### Slice 2.1 - Resolve global, native, and call-site identities once

Assigned agent: Luna max.

Objective and reason: remove repeated name decoding, Value classification, and
call identity discovery from hot global/native access.

Files and symbols:

- runtime_image.go: globalDescriptor, nativeDescriptor, callSiteDescriptor;
- base_env.go: globalEnv, getSlot, versioning;
- bytecode.go: Proto global slots and call metadata;
- runtime_call.go: runtimeRequireAdapter and invocation scope;
- module_runtime.go, program.go;
- vm_dispatch_template.go.tmpl: opLoadGlobal and global stores/calls;
- new runtime_descriptor_test.go.

Ordered work:

1. Give each logical global environment a runtime-owned environmentID that is
   not derived from a reusable address, plus a monotonically changing version.
   Reuse a stable environment object where the invocation contract allows it;
   allocate a new ID when identity really changes.
2. Materialize dense descriptors containing expected environmentID and version,
   global slot, fallback name, and native/call identity. Use the fallback only
   when the complete identity/version guard fails.
3. Give environment mutation one explicit versioning rule. A cached descriptor
   validates the minimum state needed for correctness and updates itself in
   image-owned storage after fallback.
4. Resolve stable base-library and require adapter identities once per Runtime.
   Never capture invocation context in a stable descriptor; read the active
   invocation scope at the effect boundary.
5. Add counters for direct hits, identity misses, version misses, name fallback,
   and native call fallback. Exercise host global mutation, two equal-version
   environments through the same image, and multiple runtimes sharing a
   Program.
6. Remove superseded global cache arrays/maps after the descriptor path covers
   all operations.

Verification:

    go test -run '^(TestRuntimeDescriptor|TestGlobal|TestRuntimeRequire|TestProgram)' -count=1 .
    go test -race -run '^(TestRuntimeRequire|TestRuntimeOwner)' -count=1 .
    scripts/check-runtime-parity --phase speed2x
    scripts/performance-audit-compare --before /tmp/ember-speed2x-base-a --after /tmp/ember-speed2x-descriptors --manifest /tmp/ember-speed2x-gates.tsv

Completion criteria:

- A global/native hit has no map lookup and no string comparison after its
  guard succeeds.
- Descriptor invalidation is deterministic across host mutation and runtime
  isolation.
- Switching equal-version environments through one image cannot produce a
  stale descriptor hit.
- Retain only if one targeted failing row improves at least 10 percent or two
  independent workload families improve at least 5 percent, with no row more
  than 3 percent slower, no warmed allocation regression, and cold metadata
  within the image cap.

Risks and dependencies:

- Stable require identity must not accidentally retain a stale context,
  controller, or host-global snapshot.
- One descriptor per use site is acceptable; one heap object per descriptor is
  not. Store descriptors densely.

### Slice 2.2 - Cache complete property lookup states

Assigned agent: Luna max, with Luna high owning new property oracle tests only.

Objective and reason: own-field shape caches do not cover prototype/metatable
fallback, negative lookup, or function-valued __index. The diagnostic
prototype_fallback row is the current worst table/property case.

Files and symbols:

- property_ic.go: propertyIC and cache guards;
- runtime_image.go: propertyDescriptor and propertyState;
- table_ops.go: tableAccess.getString, getSeen, callIndex;
- table_shape.go and value.go metatable/shape versioning;
- vm_dispatch_template.go.tmpl: string field get/set and method call cases;
- vm_constant_field_pic_test.go, vm_pic_test.go,
  table_metamethod_cache_test.go;
- new property_chain_differential_test.go.

Ordered work:

1. Define explicit descriptor states for own shaped field, own hash field,
   table-valued __index chain, stable negative lookup, function-valued __index
   side exit, and megamorphic fallback.
2. Guard every table identity/shape/metatable generation needed by a state.
   A hit must be a short straight-line load; a miss calls one shared semantic
   resolver and rewrites the owner-local descriptor.
3. Add negative-cache invalidation for inserts, deletes, metatable changes,
   __index replacement, shape transition, and delete/reinsert ordering.
4. Keep cycle detection and callable metamethod behavior in the semantic
   resolver, not duplicated in generated cases.
5. Measure prototype_fallback, metatable_index, method_calls,
   dirty_metatable_writes, and a non-metatable control independently.

Verification:

    go test -run '^(TestProperty|TestVM.*PIC|TestTableMetamethod|TestExecutionDifferential)' -count=1 .
    go test -race -run '^(TestProperty|TestTableMetamethod)' -count=1 .
    scripts/check-runtime-parity --phase speed2x
    scripts/performance-audit --output /tmp/ember-speed2x-property --profiles

Completion criteria:

- Warm complete-chain hits do not rewalk a metatable chain.
- All invalidation and cycle cases match the public semantic oracle.
- Retain only if a targeted failing row improves at least 10 percent or two
  independent families improve at least 5 percent, with no row more than 3
  percent slower, no warmed allocation regression, and cold metadata within
  the image cap.

Risks and dependencies:

- A function-valued __index can execute arbitrary code and mutate the chain;
  it is a guarded side exit, not a direct cached load.
- Cache sophistication without dynamic hit evidence is a shallow module. Reject
  unused descriptor states.

## Phase 3: Replace the call/frame ABI

### Slice 3.1 - Seal call descriptors and classify dynamic call edges

Assigned agent: Luna max.

Objective and reason: move immutable call-shape decisions out of
enterRecordOnlyFixedCall and related hot paths before changing stack ownership.

Files and symbols:

- runtime_image.go: callDescriptor and callSiteDescriptor;
- bytecode.go: opCall, opCallOne, opCallLocalOne, opCallUpvalueOne,
  opCallMethodOne metadata and verifier;
- function_analysis.go and optimizer.go call analysis;
- vm.go: fixed-call eligibility and fallback reasons;
- vm_cold.go;
- function_analysis_test.go, vm_flat_call_test.go,
  vm_borrowed_window_test.go.

Ordered work:

1. Materialize verified callee parameter count, vararg shape, register need,
   upvalue layout, result mode, protection mode, and direct/tail eligibility in
   dense image call descriptors.
2. Give every dynamic call edge exactly one classified path: direct script,
   tail script, variadic script, method/metamethod, protected, coroutine,
   internal native, or public host adapter.
3. Count classified edges and fallback reason by workload. Do not optimize a
   call form until its dynamic coverage is known.
4. Remove repeated verifier/shape checks from direct edges while retaining one
   semantic fallback for guard misses.
5. Require at least 95 percent of dynamic call edges in the frozen corpus to
   have a non-generic descriptor before starting the stack rewrite.

Verification:

    go test -run '^(TestFunctionAnalysis|TestVMFlatCall|TestVMBorrowed|TestExecutionDifferential)' -count=1 .
    go test -race -run '^(TestCallback|TestCoroutine|TestProtected)' -count=1 .
    scripts/check-runtime-parity --phase speed2x

Completion criteria:

- At least 95 percent of dynamic corpus call edges are classified.
- No direct edge repeats immutable arity/register/upvalue validation.
- No row slows more than 3 percent; warmed allocation ceilings and applicable
  cold caps hold.

Risks and dependencies:

- A descriptor that embeds a Closure instance is wrong; descriptors describe
  immutable shape while the stack carries dynamic callee state.
- Compiler analysis is advisory until the bytecode verifier seals it.

### Slice 3.2 - Install one flat value stack and one continuation stack

Assigned agent: Luna max, with Sol high reviewing ownership before cutover.

Objective and reason: eliminate repeated vmFrame construction, record-only
bridges, register slice rebinding, broad clearing, and state reconstruction.

Files and symbols:

- new vm_stack.go: valueStack, stackWindow, continuation, continuationStack;
- new vm_call.go: enterScriptCall, enterTailCall, returnToContinuation,
  sideExitCall;
- vm.go: vmThread, vmFrame, vmFrameRecord, vmFrameCold,
  enterRecordOnlyFixedCall, resumeRecordOnlyFixedCallOne and all sibling
  enter/resume helpers;
- vm_cold.go and vm_dispatch_template.go.tmpl call/return cases;
- value.go: vmStackOwner, cells, open upvalues;
- callback.go, coroutine implementation in base_coroutine.go;
- runtime_budget_b3_test.go, vm_flat_call_test.go,
  vm_protected_chain_test.go, runtime_owner_coroutine_test.go;
- new vm_stack_liveness_test.go.

Ordered work:

1. Make vmThread own one contiguous value stack and one compact continuation
   stack. A continuation stores only dynamic return state: caller image proto,
   pc, base/top, result destination, protection state, and open-result state.
   Immutable callee shape remains in callDescriptor.
2. Represent the active frame as registers at stack[base:top] plus image proto
   and pc. Direct calls switch image proto/base/top/pc; they do not allocate or
   reset a full vmFrame.
3. Implement proper tail calls by reusing the current stack window and
   continuation depth when Luau semantics allow it.
4. Integrate fixed, open-result, variadic, local, upvalue, method, protected,
   callback, require, and coroutine call forms into the same ABI. Temporary
   bridges are allowed only within the uncommitted development slice.
5. Make open upvalues refer to stable stack-owner offsets and close exactly
   when their window leaves ownership. Stack growth updates one owner base, not
   every frame's register slice.
6. Replace broad clears with compiler/verifier-proven live-window clearing.
   With pointer-bearing []Value, clear every dead slot in the backing allocation,
   not only the logical slice length, on normal return, tail-call reuse,
   protected unwind, error, cancellation, coroutine suspend/close, callback
   release, and stack shrink. Do not skip a clear without a liveness proof.
7. Add forced-GC reachability tests using finalizers or runtime cleanup hooks.
   Put unique heap objects in every dead region, take each exit path, force GC,
   and prove dead objects are collectible while live upvalues/results remain.
8. Delete vmFrameRecord, record-only enter/resume helpers, and duplicate direct
   child resume paths after every call form uses the new continuation ABI.

Verification:

    go test -run '^(TestVMFlatCall|TestVMBorrowed|TestVMProtected|TestCallback|TestRuntimeOwnerCoroutine|TestExecutionDifferential)' -count=1 .
    go test -run '^TestVMStackLiveness' -count=20 .
    go test -race -run '^(TestCallback|TestRuntimeOwnerCoroutine|TestRuntimeBusy)' -count=1 .
    go test -gcflags=all=-d=checkptr=2 -run '^(TestVMFlatCall|TestExecutionDifferential)' -count=1 .
    scripts/check-runtime-parity --phase speed2x
    scripts/performance-audit --output /tmp/ember-speed2x-call-abi --profiles
    scripts/performance-audit-compare --before /tmp/ember-speed2x-base-a --after /tmp/ember-speed2x-call-abi --manifest /tmp/ember-speed2x-gates.tsv

Completion criteria:

- Recursive Fibonacci improves at least 45 percent from the Phase 0 baseline.
- The closure, method, vararg, and coroutine family each improves at least 20
  percent where it was above target.
- At least 95 percent of dynamic call edges use the new ABI.
- Generic, protected, callback, and coroutine calls use the same stack model;
  a fixed-call-only fast bridge is not an acceptable endpoint.
- Forced-GC tests prove dead pointers do not remain in the backing stack after
  every exit/unwind/suspension path.
- No row slows more than 3 percent, warmed allocation ceilings and cold caps
  hold, and collector, limit, error, and cancellation tests remain exact.

Risks and dependencies:

- Stack growth, open upvalues, protected errors, and coroutine suspension are
  the critical correctness risks. Treat their ownership tests as design tests,
  not cleanup.
- If recursive, vararg, or coroutine remains above 2.30 and call mechanics
  still exceed 20 percent of its CPU, permit one bounded redesign before moving
  on. Do not patch individual call opcodes indefinitely.

## Checkpoint A: Decide whether representation work is necessary

Assigned agent: Sol high using counter-disabled captures.

1. Capture all 37 parity rows, Ember audit metrics, CPU profiles for every
   failing row, and diagnostic counters from the retained Phase 3 tree.
2. Stop the optimization program and proceed to final cleanup if every row is
   at most 1.85 median and 2.00 p90.
3. Trigger the slot branch only when remaining failing rows attribute at least
   12 percent of CPU to Value classification/transport, pointer-driven stack or
   table clearing, or Go-GC scanning attributable to 16-byte Value storage.
4. Trigger the table branch when a remaining failing row attributes at least
   15 percent of CPU to table probes, ordered-iteration maintenance, shape/hash
   transitions, array density handling, or metatable lookup not addressed by
   Phase 2.
5. If both trigger, implement them as one canonical slot-backed table cutover.
   Do not first rewrite Value-backed tables and then rewrite them again.
6. If neither triggers, skip Phase 4 and use the measured remaining opcode/call
   mechanisms to select Phase 5 specializations.

The checkpoint output is appended to ADR 0007 and performance-audit.md with a
go/skip decision for each branch.

## Phase 4: Replace the canonical value and table representation when triggered

### Slice 4.1 - Define the breaking host/value ownership boundary

Assigned agent: Sol high for the interface decision, then Luna max for the
approved migration. This slice runs if the slot branch triggers at Checkpoint A
for measured interpreter leverage or at Checkpoint B as the pointer-free native
safety prerequisite.

Objective and reason: make owner-relative 8-byte slots canonical internally
without leaking them into shared Proto or silently pinning arbitrary host
objects forever.

Files and symbols:

- slot.go: slot tags and constructors;
- runtime_heap.go: typed slabs, roots, pins, import/export;
- runtime_owner.go;
- value.go: public Value, Table, Closure, cell, vmStackOwner;
- program.go: Runtime and lifecycle;
- callback.go and host function APIs;
- docs/public-surface.md, docs/hearth-integration.md;
- docs/adr/0004-runtime-slot-handle-ownership.md and ADR 0007.

Ordered work:

1. Record the public boundary before implementation. Internal slots are
   owner-relative and 8 bytes. Shared Proto contains portable constants only;
   executionImage materializes them into owner-local slots.
2. Make the fast host contract Runtime-owned and borrowed for the duration of a
   call. Internal builtins consume slot windows directly. Public host adapters
   convert arguments/results only at the host seam.
3. Make durable object export explicit through a root/pin lifetime with an
   explicit release or Result lifetime. Do not rely on hidden finalizers or a
   global owner. Immediate scalar export may remain allocation-free.
4. Remove or demote Run, RunWithGlobals, Value, Table, Callback, and host
   function forms that force internal Value conversion. Compatibility is not a
   reason to keep conversion in the hot path.
5. Document owner mismatch, use-after-release, Runtime.Close, callback capture,
   and cross-runtime transfer behavior. Fail explicitly at the boundary.
6. Add adapter benchmarks so later work cannot move runtime cost into host
   conversion unnoticed.

Verification:

    go test -run '^(TestValue|TestRuntimeHeap|TestRuntimeOwner|TestCallback|TestProgram)' -count=1 .
    go test -race -run '^(TestRuntimeHeap|TestRuntimeOwner|TestCallback)' -count=1 .
    go test -gcflags=all=-d=checkptr=2 -run '^(TestRuntimeHeap|TestValue)' -count=1 .

Completion criteria:

- The ownership interface is documented and test-backed before hot storage
  changes.
- No public adapter is called by an internal script-to-script or builtin call.
- Cross-runtime handles fail safely and deterministic cleanup is observable.

Risks and dependencies:

- This is the deliberately breaking API seam. Avoid preserving two equivalent
  public invocation models.
- A pin-every-export policy can grow retained memory without bound; explicit
  release/lifetime is required even though memory optimization is deferred.
- On the Checkpoint-B-only path, do not commit this breaking interface by
  itself. It belongs to the combined slot/native retention group described at
  Checkpoint B.

### Slice 4.2 - Cut the entire canonical engine to slots

Assigned agent: Luna max, with Sol high reviewing the complete cutover before
commit. Prototype work may be local, but main must never retain a mixed
steady-state engine.

Objective and reason: halve hot value traffic and remove repeated valueKind
pointer classification when Checkpoint A proves enough leverage, or provide the
pointer-free canonical stack required for a safe native prototype when
Checkpoint B becomes the second slot trigger.

Files and symbols:

- slot.go and runtime_heap.go;
- runtime_image.go constants and descriptors;
- vm_stack.go, vm_call.go, vm.go, vm_cold.go;
- vm_dispatch_template.go.tmpl and generated output;
- base_env.go, base library files, runtime_call.go, callback.go,
  base_coroutine.go;
- value.go cells/closures/tables, or the runtime table files from Slice 4.3;
- slot_test.go, value_layout_test.go, runtime_heap_collector_test.go,
  execution_differential_test.go.

Ordered work:

1. Materialize Proto constants into executionImage slot arrays once. Use dense
   heap slab indexes in hot code; do not call public import/export or a byValue
   map from an opcode.
2. Replace register stack, continuations, globals, upvalues/cells, closures,
   varargs, open results, coroutine state, native builtin arguments/results,
   cache payloads, and table keys/values with slots in one canonical cutover.
3. Give heap objects stable typed slab indexes/generations. Validate at the
   boundary and collector, not on every already-verified direct load.
4. Keep scalar arithmetic/comparison on direct slot bits. Move exceptional
   types and metamethod behavior to shared cold helpers.
5. Update root scanning for value stack, continuation-owned ranges, globals,
   closures, coroutines, tables, pins, and temporary side exits. Preserve
   MaxRuntimeObjects and explicit Runtime.Close behavior.
6. Differentially run the whole compatibility corpus against the pre-cutover
   commit during development. Before merge, delete the Value-backed hot path,
   alternate dispatch, and conversion bridges.
7. Replace layout tests with explicit slot, continuation, and execution-image
   budgets. Do not keep obsolete exact sizes merely for compatibility.

Verification:

    go test -run '^(TestSlot|TestRuntimeHeap|TestExecutionDifferential|TestVM|TestTable|TestCallback|TestCoroutine)' -count=1 .
    go test -race -run '^(TestRuntimeHeap|TestCallback|TestCoroutine|TestRuntimeOwner)' -count=1 .
    go test -gcflags=all=-d=checkptr=2 -run '^(TestRuntimeHeap|TestExecutionDifferential)' -count=1 .
    scripts/check-runtime-parity --phase speed2x
    scripts/performance-audit --output /tmp/ember-speed2x-slots --profiles
    scripts/performance-audit-compare --before /tmp/ember-speed2x-base-a --after /tmp/ember-speed2x-slots --manifest /tmp/ember-speed2x-gates.tsv

Completion criteria:

- No canonical VM register, stack, global, closure, cell, coroutine, cache, or
  table payload uses Value.
- valueKind is absent from counter-disabled VM CPU profiles except at explicit
  public adapters.
- Hot stack traffic is approximately halved and measured, not inferred only
  from type size.
- Failing-row geomean improves at least 12 percent, no row slows more than 3
  percent, warmed allocation ceilings and cold image/export caps hold, and
  heap/RSS growth is bounded.

Risks and dependencies:

- Prior mixed Value/slot experiments regressed. Partial migration is a failed
  endpoint even if one microbenchmark improves.
- The collector, owner generation, and durable export rules must be complete
  before a slot can survive suspension or host escape.
- On the Checkpoint-B-only path, the speed retention decision is made for the
  combined Slices 4.1, 4.2, 6.1, and 6.2. Do not publish an otherwise
  unqualified slot cutover merely to enable a native experiment.

### Slice 4.3 - Replace table storage around measured access patterns

Assigned agent: Luna max, with a Luna high test worker owning only new table
oracle/corpus files. Run when the table branch triggers. If slots also trigger,
implement this within the slot cutover rather than as a second migration.

Objective and reason: remove duplicate hash/iteration structures and make the
common array, shaped-string, and ordered hash operations short and predictable.
This is a CPU representation change, not an allocator campaign.

Files and symbols:

- value.go: Table, tableStorage, tableHashFields and iteration journal;
- table_shape.go;
- table_ops.go;
- raw_sequence.go;
- property_ic.go and runtime_image.go table descriptors;
- vm_dispatch_template.go.tmpl table/field/iteration cases;
- table_array_density_test.go, table_clear_test.go,
  table_hash_generation_test.go, table_iteration_capacity_test.go,
  table_iteration_identity_test.go, table_numeric_zero_test.go;
- new table_differential_corpus_test.go.

Ordered work:

1. Use Phase 0/Checkpoint A counters to choose capacities and state transitions.
   Do not optimize only sparse_grid_neighbors if the design harms normal rows.
2. Keep a dense array for positive integral keys. Store string/other keys in a
   compact open-addressed table whose entry metadata also supports deterministic
   iteration order, avoiding a separate Go map plus duplicate journal/index.
3. Preserve insertion order, deletion and reinsertion position, numeric -0/0
   normalization, NaN rejection, array holes, length behavior, metatables,
   identity, and generation invalidation.
4. Keep shaped string fields only when dynamic hit counts justify them. Shape
   and property descriptors should point directly at dense value offsets.
5. Define growth/tombstone/compaction policy as pure decisions over counts and
   capacities. Perform allocation and copying at the table edge.
6. Add a differential operation-sequence corpus covering array/hash migration,
   iteration during mutation where defined, metatable chains, delete/reinsert,
   and adversarial collision/growth cases.
7. Delete old tableHashFields, journal, index, and adapters once all operations
   use the replacement.

Verification:

    go test -run '^TestTable' -count=1 .
    go test -race -run '^TestTable' -count=1 .
    go test -gcflags=all=-d=checkptr=2 -run '^TestTable' -count=1 .
    scripts/check-runtime-parity --phase speed2x
    scripts/performance-audit --output /tmp/ember-speed2x-tables --profiles
    scripts/performance-audit-compare --before /tmp/ember-speed2x-base-a --after /tmp/ember-speed2x-tables --manifest /tmp/ember-speed2x-gates.tsv

Completion criteria:

- One table representation owns lookup, mutation, and deterministic iteration
  state; old duplicate structures are deleted.
- Each targeted failing table row improves at least 15 percent, or the failing
  table-family geomean improves at least 12 percent.
- The worst failing table row improves at least 10 percent even when the family
  geomean gate is used.
- No row among all 37, including table rows, slows more than 3 percent; all
  semantics pass, warmed allocation ceilings hold, and cold construction stays
  within its cap.

Risks and dependencies:

- Deterministic iteration is a required Ember property. A faster Go map-backed
  design that reconstructs order later is not acceptable.
- ADR 0006 rejected an allocator campaign, not a measured CPU/table
  representation replacement. Update it only if the evidence changes its
  scope.

## Phase 5: Quickening and corpus-derived specialization

### Slice 5.1 - Add guarded owner-local quickening

Assigned agent: Luna max.

Objective and reason: specialize stable runtime facts after the fundamental
dispatch, call, value, and table costs are visible. Quickening belongs in the
owner-local image so shared Proto remains immutable.

Files and symbols:

- runtime_image.go: quickenedWord, quickeningState, descriptor offsets;
- new vm_quicken.go: observe, specialize, deoptimize;
- vm_dispatch_template.go.tmpl and cmd/ember-vmgen/main.go;
- bytecode.go and wordcode.go verifier boundaries;
- execution_differential_test.go;
- new vm_quicken_test.go.

Ordered work:

1. Rank remaining failing rows by dynamic opcode/call/property states. Select a
   specialization only when it names one repeated decision with a stable guard.
2. Keep compiler wordcode immutable. Store mutable quickened words or
   descriptors in executionImage, with an explicit unquickened state and a
   shared semantic fallback.
3. Candidate states include numeric arithmetic/comparison, stable global/native
   calls, direct script-call identity, own/property-chain loads, and stable
   iteration shape. Implement only candidates selected by counters.
4. On a failed guard, perform the operation through the shared helper and
   rewrite/deopt the owner-local state. Bound polymorphism and memory per site.
5. Require each specialization to improve one hard failing row by at least 10
   percent or two independent workload families by at least 5 percent.
6. Delete specializations that do not meet retention gates; do not keep them
   because they appear theoretically useful.

Verification:

    go generate ./...
    go run ./cmd/ember-vmgen -check
    go test -run '^(TestVMQuicken|TestVMDispatch|TestExecutionDifferential)' -count=1 .
    go test -race -run '^TestVMQuicken' -count=1 .
    scripts/check-runtime-parity --phase speed2x
    scripts/performance-audit-compare --before /tmp/ember-speed2x-base-a --after /tmp/ember-speed2x-quicken --manifest /tmp/ember-speed2x-gates.tsv

Completion criteria:

- Shared Proto is bit-for-bit unchanged by execution.
- Guard failure always reaches one tested semantic implementation.
- Every retained specialization meets its measured threshold, no row slows
  more than 3 percent, warmed allocation ceilings hold, and quickened image
  storage stays within the cold cap.

Risks and dependencies:

- Owner-local word copies can inflate cold allocations. Consolidate them into
  the execution image, delete old sidecars, and enforce the per-word/per-Proto
  cold cap.
- Megamorphic sites must settle on a bounded generic state, not thrash.

### Slice 5.2 - Add only dynamically broad superinstructions

Assigned agent: Luna max, conditional on n-gram evidence.

Objective and reason: reduce dispatch only for patterns broad enough to matter
across real workloads. Prior narrow field-RMW candidates had negligible dynamic
eligibility.

Files and symbols:

- counter report from Slice 0.3;
- bytecode.go opcode metadata only if compiler wordcode changes;
- runtime_image.go quickened opcode namespace if fusion stays runtime-local;
- vm_dispatch_template.go.tmpl, cmd/ember-vmgen/main.go;
- vm_dispatch_structure_test.go and execution_differential_test.go.

Ordered work:

1. Mine post-Phase-5 dynamic opcode/basic-block n-grams from all failing rows.
2. Consider only a fusion covering at least 2 percent of dynamic instructions
   in at least two independent workload families.
3. Prefer quickened image-only fusions so the compiler's 64-opcode wire budget
   remains stable. Change private wordcode only if the fusion is broadly useful
   before runtime observation.
4. Generate fused semantics from the same component definitions and preserve
   precise errors, limits, debug lines, side exits, and register effects.
5. Retain only if a failing row improves at least 3 percent with no row more
   than 3 percent slower.

Verification:

    go generate ./...
    go run ./cmd/ember-vmgen -check
    go test -run '^(TestVMDispatch|TestRegisterEffects|TestExecutionDifferential)' -count=1 .
    scripts/check-runtime-parity --phase speed2x

Completion criteria:

- Coverage and speed thresholds are recorded beside each retained fusion.
- No benchmark-specific or single-row-only fusion remains.
- Warmed allocation gates, applicable cold caps, and compatibility tests pass.

Risks and dependencies:

- More opcodes can increase instruction-cache pressure enough to erase dispatch
  gains. Generated code size is a reported retention metric.

## Checkpoint B: Mandatory native-tier decision

Assigned agent: Sol high.

Take a clean full capture after every retained interpreter slice.

- If every row is at most 1.85 median and 2.00 p90, reject the native tier and
  proceed to final cleanup.
- If any row remains above 2.00 p90, the native product prototype is mandatory.
- If any row remains above 2.40 and no measured interpreter mechanism can close
  the gap, stop adding interpreter micro-optimizations and start the native
  prototype immediately.
- The decision report must list each failing row, ratio, fit quality, top CPU
  mechanisms, dynamic backend coverage available, and estimated required gain.
- A native phase may not write pointer-bearing Value records from assembly or
  generated machine code into Go memory without write barriers. The default
  prerequisite is the complete slot-backed canonical stack from Slices 4.1 and
  4.2. Checkpoint B is a second explicit slot trigger. If Checkpoint A skipped
  slots, develop Slices 4.1, 4.2, 6.1, and 6.2 as one uncommitted bounded
  retention group on the main worktree; do not commit or push an intermediate
  breaking interface or slot engine. Retain the group only if the final native
  target and allocation caps pass. If native fails, remove the interface,
  slots, and backend together unless the slot cutover independently passes the
  original Slice 4.2 speed gate. An alternative barrier-safe design requires a
  separate ADR and concurrent-GC proof before Phase 6.

There are no permanent exceptions. Failure of the native feasibility path is a
real external blocker to the requested target, not permission to redefine the
target.

## Phase 6: Bounded native-tier product prototype when triggered

### Slice 6.1 - Specify one backend seam from executionImage

Assigned agent: Sol high.

Objective and reason: make native execution an adapter for scalar/control
mechanics while keeping effectful and complex language semantics in the one
canonical runtime implementation.

Files and symbols:

- runtime_image.go: backend-neutral executable records and deopt metadata;
- new execution_backend.go: executionBackend private interface;
- slot.go, runtime_heap.go, vm_stack.go, and vm_call.go shared state ABI;
- table_ops.go, runtime errors, callback/coroutine helpers as shared side exits;
- docs/adr/0005-execution-path-retention.md and ADR 0007.

Ordered work:

1. Define the smallest private backend interface: prepare executable proto,
   enter with image/base/top/pc, return/side-exit with precise state, and close.
2. Require pointer-free internal slots in stacks and continuations before native
   execution. Keep those stacks, heap, tables, calls, errors,
   callbacks, coroutines, and limits owned by the canonical runtime. Native code
   may implement scalar operations, branches, register moves, and guarded
   descriptor hits.
3. Specify deoptimization state precisely enough to resume the interpreter at
   one word with identical registers, pc, continuation depth, open results, and
   error/debug location.
4. Use only the concrete JIT, measured generic assembly backend, or explicit AOT
   artifact lifecycle exercised in Slice 0.4. A named but unproven generated
   backend is not a fallback. Stop for approval before adding a dependency.
5. Define executable-byte, compile-time, cleanup, and warm-up accounting before
   implementation.
6. Generate scalar arithmetic/comparison/branch rules from the same definitions
   used by the interpreter where practical, and require differential oracles for
   every rule the backend implements.

Verification:

    go test -run '^(TestExecutionBackend|TestExecutionDifferential)' -count=1 .
    git diff --check

Completion criteria:

- The backend interface is narrower than the runtime semantics it uses.
- Interpreter and candidate backend share semantic helpers and runtime state.
- ADR 0005 is superseded only if the prototype later passes retention gates.

Risks and dependencies:

- An interface mirroring every opcode is shallow and likely creates two VMs.
  Reject it.
- Go ownership alone does not provide write barriers for assembly writes. A
  pointer-bearing Value stack is forbidden unless a separately proven
  barrier-safe mechanism replaces the default slot prerequisite.

### Slice 6.2 - Implement only coverage needed by failing rows

Assigned agent: Luna max, with a Luna high worker owning differential corpus
expansion in disjoint test files.

Objective and reason: determine whether a bounded native adapter can close the
remaining measured gap without committing to a speculative compiler.

Files and symbols:

- execution_backend.go;
- new native_backend_darwin_arm64.go and native_backend_arm64.s, or generated
  architecture-specific equivalents selected in Slice 0.4;
- new native_lower.go: lowering from verified executableProto;
- runtime_image.go deopt/descriptor metadata;
- new native_backend_test.go and native_differential_test.go;
- scripts/performance-audit profiling support if native symbols need mapping.

Ordered work:

1. Lower only operations accounting for at least 90 percent of dynamic
   instructions in every still-failing target row. Unsupported operations
   side-exit with precise state.
2. Reuse call descriptors and the canonical call ABI. Native direct calls may
   link only when both caller and callee coverage is safe; otherwise side-exit.
3. Reuse property/global descriptors for guarded hits and shared helpers for
   misses. Do not duplicate table, metamethod, error, callback, or coroutine
   semantics.
4. Charge instruction limits and cancellation at verified basic-block
   boundaries with exact observable totals. Preserve debug/error pc mapping.
5. Stress reference creation, slot moves, deoptimization, callbacks, and
   coroutine suspension during concurrent GC. Prove that native code transports
   only pointer-free handles and that all Go-pointer creation/mutation remains
   in barrier-aware Go helpers.
6. Bound executable bytes per source word and release all executable resources
   at Runtime.Close. Record compilation and warm-up break-even independently
   from steady-state slope.
7. Differentially execute generated programs, all 37 frozen cases, error paths,
   limits, callbacks, coroutines, tables, and deopts against the interpreter.
8. Capture paired parity with native enabled and disabled from the same commit.

Verification:

    go test -run '^(TestNativeBackend|TestNativeDifferential|TestExecutionDifferential)' -count=1 .
    go test -race -run '^(TestNativeBackend|TestRuntimeOwner)' -count=1 .
    GOGC=1 go test -race -run '^TestNative.*ConcurrentGC' -count=20 .
    go test -gcflags=all=-d=checkptr=2 -run '^TestNativeBackend' -count=1 .
    scripts/check-runtime-parity --phase speed2x
    scripts/performance-audit --output /tmp/ember-speed2x-native --profiles
    scripts/performance-audit-compare --before /tmp/ember-speed2x-base-a --after /tmp/ember-speed2x-native --manifest /tmp/ember-speed2x-gates.tsv

Completion criteria:

- At least 90 percent dynamic coverage on every still-failing target row.
- The failing-row family improves at least 25 percent.
- The measured result, not merely a projection, puts every row at most 1.85
  median and 2.00 p90.
- Executable bytes and warm-up break-even are bounded and reported.
- Warmed allocation ceilings hold; image/native construction stays within its
  explicit per-Proto, per-word, executable-byte, and recurrence caps.

Risks and dependencies:

- A native tier that helps arithmetic but repeatedly side-exits on calls or
  property access will not close the observed worst rows and must be deleted.
- If it cannot meet the target, preserve its evidence and delete the backend;
  report the program blocked rather than shipping permanent duplicate
  semantics.

### Slice 6.3 - Retain or delete the native tier completely

Assigned agent: Sol high review, Luna max cleanup.

Objective and reason: make a binary architecture decision from measured target
evidence. A partial dormant backend would preserve duplicate complexity without
delivering the requested outcome.

Files and symbols:

- execution_backend.go and all retained or rejected native_backend files;
- runtime_image.go backend selection and executable-resource ownership;
- program.go Runtime.Close;
- native_backend_test.go and native_differential_test.go;
- docs/adr/0005-execution-path-retention.md, ADR 0007,
  performance-audit.md, and docs/design.md.

Ordered work:

If Slice 6.2 meets every retention criterion:

1. Supersede ADR 0005. State that executionImage is the semantic seam and the
   interpreter/native implementations are two real adapters.
2. Delete temporary lowering paths, duplicated helpers, diagnostic switches,
   and unsupported partial modes not needed by the retained backend.
3. Make backend selection explicit and deterministic. Never silently require
   native execution for correctness.
4. Add platform fallback tests and clear unsupported-platform behavior.

If any criterion fails:

1. Preserve reports and the decision in ADR 0007.
2. Delete all production native/backend code and restore the single interpreter
   module.
3. Identify the remaining target blocker with exact ratios and mechanism
   evidence. Do not retain dead architecture for a hypothetical future.

Verification:

    go generate ./...
    go run ./cmd/ember-vmgen -check
    go test -run '^(TestNativeBackend|TestNativeDifferential|TestExecutionDifferential|TestRuntimeOwner)' -count=1 .
    rg -n 'executionBackend|nativeBackend|native_backend' --glob '*.go' --glob '*.s' .
    scripts/check-runtime-parity --phase speed2x

Completion criteria:

- The retained outcome has one explicit, cleaned-up backend seam and satisfies
  every Slice 6.2 gate, or the rejected outcome contains no production native
  backend code.
- Runtime.Close and unsupported-platform behavior are deterministic in the
  retained outcome.
- ADR 0005, ADR 0007, design docs, and performance evidence agree with the
  code that remains.

Risks and dependencies:

- Prototype sunk-cost bias is not a retention criterion. Delete a fast local
  demo if it misses worst-row coverage, semantics, cleanup, or allocation
  gates.
- Re-gate only raw acquisitions whose source and environment fingerprint still
  matches the decision commit.

## Phase 7: Finish the program and remove accidental complexity

### Slice 7.1 - Delete superseded paths and update the design record

Assigned agent: Luna max cleanup, Sol high final architecture review.

Objective and reason: leave one comprehensible execution architecture after
the speed work, so later fixes do not have to preserve abandoned bridges and
experiments.

Files and symbols:

- vm.go, vm_cold.go, vm_dispatch_template.go.tmpl;
- runtime_image.go, vm_stack.go, vm_call.go;
- value.go, slot.go, table files according to retained branches;
- performance-optimization-implementation-plan.md and performance-audit.md;
- docs/README.md, docs/design.md, docs/public-surface.md,
  docs/hearth-integration.md;
- ADR 0004, ADR 0005, ADR 0006, and ADR 0007.

Ordered work:

1. Use rg to prove deleted alternate engines, old frame-record enter/resume
   helpers, mixed Value/slot adapters, duplicate table structures, temporary
   native probes, and rejected specializations are gone.
2. Consolidate cold semantic helpers by decision, not by opcode accident. Keep
   the hot module narrow and keep effectful host/runtime ownership at the edge.
3. Update design and public-surface docs to the actual retained architecture;
   do not document skipped conditional branches as implemented.
4. Mark the prior optimization plan historical and link its completed/rejected
   evidence. Make this plan's completed/skipped decisions explicit.
5. Refresh comments, generated files, layout budgets, and package docs.

Verification:

    rg -n 'enterRecordOnlyFixedCall|resumeRecordOnly|alternate.*VM|native_probe' .
    go generate ./...
    go run ./cmd/ember-vmgen -check
    go test -run '^TestExecutionDifferential' -count=1 .
    git diff --check

Completion criteria:

- One canonical interpreter remains, plus a native adapter only if it passed
  the hard target.
- No temporary bridge or dormant duplicate semantic path remains.
- Documentation describes the code that exists.

Risks and dependencies:

- Cleanup must follow the final retention decision; deleting an old path before
  the replacement passes compatibility would make failures harder to localize.
- rg absence is not enough by itself. Generated output and public docs must also
  prove that the old contract is gone.

### Slice 7.2 - Run final correctness, allocation, and speed acceptance

Assigned agent: Luna high for controlled acquisition, Sol high for independent
artifact review.

Objective and reason: prove the requested result from a clean commit with
independent, reproducible artifacts before publishing it.

Files and symbols:

- scripts/check-runtime-parity and scripts/scenario-ratio-gate;
- scripts/performance-audit, performance-audit-derive-manifest, and
  performance-audit-compare;
- runtime_parity_test.go and runtime benchmark corpora;
- generated VM files and all retained runtime implementation files;
- performance-audit.md, ADR 0007, and docs/checks.md.

Ordered work:

1. Start from a clean main worktree and record commit/source fingerprints.
2. Run focused root tests first, then race/checkptr tests for ownership-sensitive
   surfaces.
3. Run the root check lane, check-fast, build, and full scripts/check. Do not run
   scripts/check-full without explicit user instruction.
4. Take two clean candidate Ember audits with profiles. Compare warmed rows
   against the frozen allocation manifest and cold image/native rows against
   their per-Proto/per-word caps. Reacquire on contamination or source drift.
5. Take a new official 37-row speed2x parity capture. Do not re-gate stale raw
   data if the source or environment fingerprint changed.
6. Have Sol high independently verify case inventory, raw headers, fit quality,
   every row's median/p90, warmed allocation ceilings, cold caps, and
   generated-code freshness.
7. Commit and push to origin/main only after all required checks pass, following
   AGENTS.md.

Verification:

    go test ./...
    go test -race ./...
    go test -gcflags=all=-d=checkptr=2 ./...
    scripts/check-lane root
    scripts/check-fast
    go build ./...
    scripts/check
    scripts/performance-audit --output /tmp/ember-speed2x-final-a --profiles
    scripts/performance-audit --output /tmp/ember-speed2x-final-b --profiles
    scripts/performance-audit-compare --before /tmp/ember-speed2x-base-a --after /tmp/ember-speed2x-final-a --manifest /tmp/ember-speed2x-gates.tsv
    scripts/performance-audit-compare --before /tmp/ember-speed2x-base-a --after /tmp/ember-speed2x-final-b --manifest /tmp/ember-speed2x-gates.tsv
    scripts/check-runtime-parity --phase speed2x

Completion criteria:

- All 37 unique rows: median ratio <= 1.85 and p90 ratio <= 2.00.
- Every fit meets the accepted quality/uncertainty limits.
- No warmed row exceeds its frozen B/op or allocs/op ceiling; cold image/native
  construction remains within the explicit recurrence, allocation, byte, code
  size, and warm-up caps.
- Luau results, compatibility tests, deterministic table semantics, limits,
  cancellation, errors, callbacks, coroutines, collector ownership, and host
  isolation pass.
- go generate is clean, go build succeeds, scripts/check succeeds, and the
  worktree is clean after the requested commit/push.

Risks and dependencies:

- A busy runner, changed source fingerprint, missing row, invalid fit, or stale
  raw acquisition requires a new capture; none is a performance pass or fail.
- Final push depends on all warmed allocation, bounded cold-cost, and worst-row
  gates, not only the ordinary unit-test suite.

## Commit boundaries

Prefer one reviewable commit per numbered slice, except Slice 3.2 and a full
slot cutover may require one larger cohesive commit to avoid an invalid mixed
architecture. Each commit message should name the mechanism, not a benchmark,
for example:

- perf: remove policy from unrestricted dispatch
- perf: materialize runtime execution descriptors
- perf: replace VM call continuations
- perf: cut canonical runtime values to slots
- perf: replace ordered table storage
- perf: quicken stable runtime operations
- perf: add measured arm64 execution backend
- docs: record runtime speed acceptance

Do not commit generated output separately from its generator/template. Do not
mix unrelated cleanup into a performance slice. Record rejected experiments in
performance-audit.md, then remove their code.

## Definition of failure

The program has not succeeded if any representative case is still more than
2.00 times Luau at p90, even when the overall median or geomean looks good. It
has also not succeeded if speed was bought by recurring hot-path allocations,
cold construction above its explicit cap, weakened Luau semantics, disabled
execution controls, stale benchmark data, or an unbounded duplicate engine.

When a conditional architecture is skipped, say why with counter-disabled
evidence. When the target is blocked, report the exact remaining rows,
ratios, confidence, CPU mechanisms, attempted designs, and external constraint.
Do not turn a hard target into an aspirational conclusion.
