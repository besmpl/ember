# Performance Profile Audit

## Current baseline

The authoritative implementation-start baseline was captured on 2026-07-14
from commit `090ad905023b8ba9fb4f3c782a4c96c47cb36dce` in a clean worktree.
Both captures used Go 1.26.4 on Darwin/arm64 (Apple M1), the default
`GOMAXPROCS` (8 logical CPUs), `BENCHTIME=500ms`, five samples per row, and
CPU/allocation profiles.

| Capture | Directory | Source fingerprint | Environment fingerprint |
| --- | --- | --- | --- |
| A | `/tmp/ember-perf-v2-a` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` | `a916fa1daf595aa2271ca9409f8c90af1b9d6606a700af4395f28c4de5fab58b` |
| B | `/tmp/ember-perf-v2-b` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` | `a916fa1daf595aa2271ca9409f8c90af1b9d6606a700af4395f28c4de5fab58b` |

`metadata.txt`, `commands.txt`, `capture-facts.tsv`, raw output, summaries,
and profiles are retained in each directory. `allow_dirty=0`; these are clean
captures, not exploratory runs with the user-owned plan files present.

The reproducible capture contract is:

```sh
BENCHTIME=500ms COUNT=5 scripts/performance-audit \
  --output /tmp/ember-perf-v2-a --profiles
BENCHTIME=500ms COUNT=5 scripts/performance-audit \
  --output /tmp/ember-perf-v2-b --profiles
```

The derived gate `/tmp/ember-perf-v2-gates-full.tsv` binds the baseline commit,
source/environment fingerprints, five-sample contract, and per-row timing and
allocation ceilings. Comparing A against B with that gate produced
`/tmp/ember-perf-v2-a-vs-b-full.txt`: `result: PASS`, with no missing rows,
metrics, samples, or environment fields.

## Noise and allocation contract

For each row, the ten samples from A and B are pooled to compute `MAD`, and
`shift` is the absolute difference between the two capture medians. The
absolute timing envelope is `max(1.4826 * MAD, shift, 100 ns)`. The gate stores
`1 + envelope / median(A)` for that row, with a 1.05 minimum for Scenario
rows. Using A's median as the denominator makes the stored policy match the
candidate-versus-A comparison. Every row is checked; family summaries are
descriptive only.

Bytes/op and allocations/op use an observed baseline envelope, not timing
noise. They must not increase above the maximum retained baseline observation.
This matters for integer-rounded benchmark counters such as 1,000 B/op and
13 allocations/op.

## Representative baseline rows

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

## Focused five-second evidence

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

## Size and layout evidence

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
The active coordination document is
[`performance-optimization-implementation-plan.md`](performance-optimization-implementation-plan.md);
retire it when this evidence-gated program lands, is abandoned, or is
replaced.
