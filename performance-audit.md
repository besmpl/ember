# Performance Profile Audit

## Current baseline

The compact-Machine migration baseline was captured on 2026-07-14 from commit
`f94a61d7668c628924224164fc8275048ff6f885` in a clean detached worktree. Both
captures used Go 1.26.4 on Darwin 24.6.0/arm64 (Apple M1), `CGO_ENABLED=0`,
`GOMAXPROCS=1`, `BENCHTIME=500ms`, five samples per row, and CPU/allocation
profiles. The shared acceptance environment SHA-256 is
`9b83720ee86181a5ed57d66153b5c8d14c8017e2aecc15f83aab9b530be6b8e0`.

| Capture | Directory | Bound capture fingerprint |
| --- | --- | --- |
| A | `/tmp/ember-compact-baseline-a` | `d7735be182aa284748e09634bba9a85fd3d26aa31e4b0e44cfe6328424690fe0` |
| B | `/tmp/ember-compact-baseline-b` | `2b8650f683e863cfde3987a972f1243a9645856f9fbb6dab1b9e12b572489590` |

`metadata.txt`, `commands.txt`, `capture-facts.tsv`, raw output, summaries,
and profiles are retained in each directory. `allow_dirty=0`; these are clean
captures, not exploratory runs with the user-owned plan files present.

The reproducible capture contract is:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 BENCHTIME=500ms scripts/performance-audit \
  --output /tmp/ember-compact-baseline-a --profiles
CGO_ENABLED=0 GOMAXPROCS=1 BENCHTIME=500ms scripts/performance-audit \
  --output /tmp/ember-compact-baseline-b --profiles
```

The derived version-2 performance gate has SHA-256
`d7bec9389c99d2519a5a017eba0a4fc5a52594dfcf52ece61d61a3cd5648427d`.
It binds both independent capture fingerprints, the baseline commit,
environment, five-sample contract, and every exact timing/allocation row. A
review found and repaired an asymmetric timing-envelope formula before the
gate was frozen. Each row stores independent A/B timing ratios, normalizing the
same absolute noise envelope by the selected role's own median. Both role-A
A-to-B and role-B B-to-A comparisons pass with no missing rows, metrics,
samples, or environment fields.

Two independent all-37/lifecycle allocation captures bind directory hashes
`abb48b3155f10b0aa3b8191d8e1984f7a895c6586bfc34188c1136f17c13e0d1`
and
`2cf801eece63bf6793aab562570b5595652d9a4c74c36c6c230b338bec62478b`.
Their 56-row ceiling manifest has SHA-256
`6d275313172967f724453906763f0bfdf47f3e0837e1a273b981aa7274883207`.
It covers all 37 warmed corpus rows and 19 public/lifecycle rows, including
compile/load, image preparation, Machine bind/detach/close, stateless and
persistent calls, globals, modules, host calls, callbacks, coroutines, and
result lifetime.

The all-37 Ember/Luau speed pair is intentionally not frozen from an unquiet
workstation. Acquisition fails closed unless load and aggregate CPU satisfy
the acceptance threshold for three consecutive probes. A failed quietness
wait produces no reusable capture directory and contributes no ratios.

## Compact-migration mechanism profile

The complete Scenario CPU profile attributes 35.60% flat and 77.92%
cumulative CPU to `runGeneratedDirectFrameProductionLoop`; `valueKind` is
9.28% flat and per-instruction execution accounting is 4.17% flat. Property
cache lookup and table/reference work remain visible below those structural
costs. This supports compact registers and generated semantic/accounting
metadata as one architecture rather than another side engine.

Recursive Fibonacci attributes 42.86% flat CPU to the generated loop,
14.29% to fixed-call entry, and additional samples to fixed-call resume,
frame cleanup, frame records, open-upvalue closing, and stack growth. The
runtime-mode profile has the same shape: the generated loop is 24.12% flat,
fixed-call enter/resume paths are 5.18% cumulative each, `valueKind` is 2.71%
flat, and execution accounting is 2.35% flat. These are the direct reasons for
flat scalar continuations and in-Machine calls.

The sparse-grid profile keeps the generated loop at 35.85% flat, with hash
finds, accounting, cache lookup, kind checks, globals, result clearing, and
iteration/table work distributed below it. Its allocation profile is led by
hash growth (23.20% of sampled bytes), table storage (11.54%), iteration-index
construction (7.73%), and iteration-key growth (7.72%). This does not overturn
ADR 0006: Phase 4 replaces tables as part of the complete execution/state
architecture, not as an allocation-only campaign.

Across runtime modes, `Runtime.runHook` owns 31.35% of sampled allocation
space, controller construction 13.20%, report append 12.22%, owner begin-run
4.28%, and string/host-call boxes another 7.47%; table growth/storage/order
also remains material in state-building lanes. The warmed lifecycle ceilings
make these boundary effects independently visible so later Machine slices can
prove whether they actually remove recurring execution allocations.

No explicit host callback is a dominant flat CPU symbol in either clean
profile. Runtime housekeeping is material: `kevent` is 6.07-6.33% flat in
Scenario and 19.64-21.88% in runtime modes; `madvise` is 12.13-12.30% and
13.45-13.88%, respectively. Write-barrier paths reach 4.25-5.19% cumulative
in Scenario and 2.82-3.45% in runtime modes. No Go collector function is a
leading application symbol; aggregate profiles include benchmark/runtime
activity, and the shorter recursive and sparse profiles have coarse 10 ms
sampling, so their low-percentage rankings are directional rather than exact.

## Implementation-progress ledger

This ledger records completed slices only; it is not a final-completion claim.
The authoritative baseline above remains the implementation-start reference.

| Slice | Commit/evidence | Decision and result |
| --- | --- | --- |
| compact migration 0.1 | `f5e336d7` | The overlapping invocation-environment work was reconciled, tested, and made the clean starting tree instead of being silently absorbed into a baseline. |
| compact migration 0.2 | `7a0c7a1f` | `check-purego` became a permanent recursive source/object/runtime boundary with positive and negative self-tests. |
| compact migration 0.3 | `c383cf4e` | The exact Top10, Classic, and Scenario manifests, matched warmed-call timer contract, all-37 speed artifact schema, ratio gate, lifecycle allocation gate, and shared acceptance environment were frozen. |
| compact migration 0.4 tooling | `f94a61d7`, `a75a014e`, `2012409a` | Performance captures bind separate A/B roles and the shared environment; review repaired directional envelope asymmetry and exact row-width validation before evidence was frozen. Both performance directions pass; the allocation pair derives one bound maximum-per-row ceiling manifest. |
| 0.1 baseline capture | `204c0de4`; clean captures from `090ad905` | Two comparable five-sample captures, focused profiles, size/layout evidence, and the reproducible gate were retained. `scripts/check` passed. |
| 0.2 audit comparison | `e53c61d3` | Comparison and manifest paths preserve precision and fail closed on incomplete or mismatched evidence. |
| 0.3 roadmap retirement | `204c0de4` | Superseded roadmaps were retired from navigation while their durable rationale stayed in the audit and ADRs. |
| 1.1 assignment walk | `cca4f803` | One symbol-aware dependency walk replaced repeated name walkers. Visits fell from 650 to 185 (71.5%); emit median moved 19.666 ms to 16.818 ms (~14.5%), compile 43.203 ms to 40.113 ms (~7.2%), allocations stayed at 13/191, and retained median moved 529 to 524 B. Sol accepted the design after focused assignment/fidelity/bytecode tests, the five-sample stage benchmark, CPU profiles, retained-memory clean-head comparison, and `go test ./...`. |
| 1.2 retention decision | `cca4f803` follow-up | STOP: no second demonstrated consumer for a retained assignment fact; no speculative binder cache was added. Sol accepted the stop. |
| 2.1 hook core | `ab6997f9` | Report collection was separated from the private runner. Report mode measured 984 B/op and 12 allocs/op versus discard mode at 904 B/op and 11 allocs/op. Focused `RunHook` tests and the race lane passed. |
| 2.2 controller elision | `4112d8b1` | Unlimited, non-cancelable background calls elide the controller; the measured lane reached 872 B/op and 12 allocs/op. Focused limits/RunHook/callback tests plus race and checkptr passed. The required-module inherited-frame case was not covered by this focused pass and was repaired in 2.3. |
| 2.3 invocation scope | `43fca453` | Invocation scope now travels through private runtime state; context wrappers are lazy at the context-aware host boundary. The common lane measured 904 B/op and 10 allocs/op. The inherited-frame regression was repaired by carrying frames through module/VM state and error capture, with runtime-error/context/callback/coroutine coverage. Focused tests, race/checkptr, `go test ./...`, and `scripts/check-lane root` passed; Sol accepted after revisions. |

Slices 2.4 and later Phase 2 work, plus Phases 3 and 4, remain pending. The
final `scripts/check` sweep and later performance gates are intentionally not
claimed here.

## Noise and allocation contract

For each row, the ten samples from A and B are pooled to compute `MAD`, and
`shift` is the absolute difference between the two capture medians. The
absolute timing envelope is `max(1.4826 * MAD, shift, 100 ns)`. The gate stores
both `1 + envelope / median(A)` and `1 + envelope / median(B)` for that row,
with a 1.05 minimum for each Scenario role. Comparison selects the ceiling for
the exact bound baseline role. Every row is checked; family summaries are
descriptive only.

Bytes/op and allocations/op use an observed baseline envelope, not timing
noise. They must not increase above the maximum retained baseline observation.
This matters for integer-rounded benchmark counters.

## Historical 090ad905 baseline rows

The following rows predate the compact-Machine migration baseline. They are
retained as evidence for the completed invocation-boundary slices in the
ledger, not as current allocation ceilings.

| Family/mode | Median ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Scenario: `combat_tick` | 18,298 | 1,952 | 11 |
| Scenario: `event_dispatch` | 75,840 | 2,480 | 15 |
| Scenario: `sparse_grid_neighbors` | 2,457,809 | 39,174 | 71 |
| Recursive Fibonacci | 3,080,499 | 232 | 4 |
| Persistent RunHook: scalar | 5,055 | 1,000 | 13 |
| Persistent RunHook: recursive | 70,204 | 1,000 | 13 |
| Persistent RunHook: nested table | 1,568 | 1,000 | 13 |
| Persistent RunHook: dynamic strings | 21,758 | 1,049 | 14 |
| Bounded RunHook: scalar | 4,276 | 1,000 | 13 |
| Cancelable RunHook: scalar | 3,060 | 1,000 | 13 |
| Stateless run: dynamic strings | 1,633,816 | 165,702 | 196 |

The persistent, bounded, and cancelable hook lanes all retain the same
1,000 B/op, 13 allocations/op floor for scalar, recursive, and nested-table
updates. Dynamic-string updates add one script-owned allocation. This makes
the common invocation boundary, rather than a table allocator, the first
Hearth-shaped allocation target.

## Historical focused five-second evidence

The retained focused profiles are:

```text
/tmp/ember-perf-v2-focused-persistent.{cpu,alloc}.pprof
/tmp/ember-perf-v2-focused-recursive.{cpu,alloc}.pprof
/tmp/ember-perf-v2-focused-sparse.{cpu,alloc}.pprof
/tmp/ember-perf-v2-focused-compiler.{cpu,alloc}.pprof
```

The persistent allocation profile attributes the 13-call floor to named
boundary owners: the `RunHook` report slice, owner run lease, execution
controller, two `globalEnv` values, hook string box, two context-wrapper
objects, bound `require` closure and host-callable box, the `require` map, and
its first map bucket. The bounded and cancelable lanes show the same floor, so
controller elision is only valid for unlimited background calls.

The persistent CPU profile is dominated by script execution: the generated
direct loop is 52.84% cumulative (4.88 s flat), fixed-call entry/resume is
about 7% cumulative, `executionWindow.stepInstruction` is 4.10% flat, and
`valueKind` is 3.71% flat. Boundary allocation removal is still the right
first Hearth target, but it is not the dominant script CPU cost.

The focused recursive profile keeps the canonical direct loop and fixed-call
entry/resume among the leading symbols. The sparse profile is allocation-heavy
in `tableHashFields.grow` (31.87%), iteration-key growth (23.02%), table
storage (20.00%), and iteration-index construction (15.35%). ADR 0006 still
stops a persistent table-allocator campaign; this stateless evidence does not
reopen it.

The focused compiler profile is decisive for the first compiler slice:
`compileAssignment` is 63.67% cumulative, with
`canCompileSingleLocalAssignmentInPlace` at 29.94% and the old
`expressionCanAssignToNameInPlace` walker at 21.46% (the name-reference helper
family is also visible: `termReferencesName` 10.18%, multiplicative 10.83%,
concat 6.72%, comparison 6.26%, additive 5.41%). A/B emit medians are
19.666 ms and 22.036 ms; compile medians are 43.203 ms and 49.908 ms, within
the derived 12-16% timing envelopes, with exact 13 and 191 allocations/op.
This supports Phase 1.1. There is no second consumer for a retained
assignment fact in the current code, so Phase 1.2 is a stop decision unless a
later slice proves one.

## Historical size and layout evidence

At the baseline commit:

- `vm_dispatch_generated.go`: 129,276 bytes;
- retained test binary `/tmp/ember-perf-v2.test`: 13,705,314 bytes;
- `vmFrameRecord`: 48 bytes on this 64-bit target, with at most one pointer;
- `TestProtoFieldClassificationBudget` passes its field-classification budget.

The Proto check is a field-classification test, not a complete `sizeof(Proto)`
measurement.

## Evidence-gated execution order

1. **Phase 2 - persistent hook boundary.** Establish the private hook core and
   allocation contract, then elide only the unlimited-background controller,
   wrap contexts lazily, remove the bound `require` closure if it remains, and
   reuse nil-host global state only after each preceding profile. Add `CallHook`
   only if the error-only path proves the report is the final common allocation.
2. **Phase 1.1 - compiler assignment analysis.** GO from the focused profile;
   replace the name walkers with one symbol-aware traversal and re-run the
   compiler fidelity and retained-memory gates.
3. **Phase 1.2 - assignment-fact retention.** STOP: the current repository has
   no demonstrated second consumer. Do not add speculative binder storage.
4. **Phase 3 - canonical fixed-call continuation.** Begin only after Phase 2
   changes the persistent boundary and targeted counters confirm the remaining
   fixed-call cost.
5. **Phase 4 - generated-loop and dispatch work.** Conditional on post-Phase-2
   profiles; the current evidence supports measurement and counters, not a
   cache or specialization campaign by assumption.

## Reproducibility and retired documents

Use the retained `commands.txt` files and the exact environment fingerprint
before comparing a candidate. The standard local checks are:

```sh
scripts/bench-summary-test
go test -run 'TestVMFrameRecordFitsCompactCallStateBudget|TestProtoFieldClassificationBudget' .
```

The tracked files `docs/exec-plans/interpreter-core-speed.md`,
`docs/exec-plans/general-optimization.md`, and
`docs/exec-plans/massive-optimization.md` are retired temporary roadmaps.
Their durable rationale remains in this audit and ADRs 0004, 0005, and 0006.
The active runtime coordination document is
[`runtime-speed-2x-no-cgo-production-migration-implementation-plan.md`](runtime-speed-2x-no-cgo-production-migration-implementation-plan.md).
The earlier `performance-optimization-implementation-plan.md` is historical;
its durable measurements and retained decisions remain in this audit and the
ADRs.
