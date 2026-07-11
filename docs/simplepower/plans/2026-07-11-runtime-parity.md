# Ember runtime parity with Luau

Goal: Bring pure-Go Ember steady-state runtime to buffered parity with Luau 0.728 on every frozen Scenario benchmark without workload-specific production behavior.
Risk: high

## Summary

Parity means every frozen Scenario row has a paired steady-state median ratio
at or below 0.95x and p90 at or below 1.00x. The buffer prevents measurement
noise from certifying a nominal tie. The comparison uses identical in-script
case loops for Ember and Luau and fits `T(N)=entry+N*inner`; the ratio gates the
inner slope, while public `Run` entry cost is reported separately.

Fresh clean-baseline evidence identifies five structural costs: the production
dispatcher is large and still unpacks instructions; numeric loops execute 1,806
instructions where Luau needs about 1,206; script calls recursively materialize
frame/result machinery; table storage discards boxed string identity; and
coroutines/table-heavy rows create avoidable scratch and copy churn. A dirty
experimental bundle improved arithmetic to 2.03x but remained 7.36x on recursive
Fibonacci, proving that a by-value frame-shaped record is not flat enough.

Implementation starts from clean `c0d24e552b2a741bb76f3e362244266cece3c5d3`
in a new worktree. Existing dirty worktrees remain untouched evidence. Default
execution after combined approval is `simplepower:subagent-driven-development`.
Before any implementation write, a fail-closed preflight verifies that exact
HEAD, a clean tracked tree, the pinned Luau binary, and the M1 runner platform.

## Decisions

- Freeze a fair parity harness before optimization. Preserve raw paired samples
  under ignored `tmp/runtime-parity` and keep the old direct-`Run` benchmark as
  a diagnostic only.
- Exclude harness asymmetry from the fitted slope: Ember measures `Run` only
  after compilation, while the pinned Luau wrapper uses `os.clock()` directly
  around the identical N-loop and prints elapsed time and the result afterward.
  CLI startup, source compilation, script I/O, and output parsing are never part
  of a Luau timing point. Finite negative fitted intercepts remain diagnostic;
  only missing, non-finite, or non-positive slopes fail closed.
- Make measurement attempts content-addressed by a SHA-256 fingerprint of HEAD
  plus sorted `relative-path<TAB>sha256` records for every root Go file and both
  parity scripts. An unchanged fingerprint reuses its retained raw data and
  report and may never collect a second sample set.
  Before creating an attempt, require three observations ten seconds apart with
  one-minute load average <=2.0 and summed process CPU <=100%; a busy runner
  exits before measurement and creates no attempt data.
- Pin the reference to Homebrew Luau 0.728 at executable SHA-256
  `c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd`
  on `Darwin 24.6.0 arm64`, Apple M1. Each paired ratio divides two ordinary
  least-squares slopes with an intercept over N=1,10,100,1000. Retain all nine
  ratios: median is sorted item 5 and p90 is nearest-rank item 9. A missing
  point, mismatch, non-finite value, or non-positive slope fails closed.
- Keep one small concrete production loop and one checked reference adapter.
  Plain execution contains no tracing, counters, hook/budget branches, unpack,
  or captured helper closures.
- Reduce the existing packed instruction from 16 to 12 bytes first. A 32-bit
  wordcode experiment is permitted only after Phase 3 if fetch/decode remains
  at least 5% of a failing row.
- Deepen numeric-for instructions to perform setup coercion and backedge checks;
  remove avoidable body moves without adding opcodes.
- Store the active callee in dispatch locals. A compact caller-only record is at
  most 48 bytes; ordinary calls use zero-copy argument windows and direct result
  writes. Cold state owns protected calls, host continuations, debug data, and
  unusual open results.
- Replace stack-slot pointers in open upvalues with stable stack owner/index
  references. Tail calls reuse the active frame only after Luau differential
  tests establish semantics.
- Make `Value` exactly 16 bytes while preserving a GC-visible pointer word. Hot
  opcode predicates bypass the generic kind decoder; the 24-byte implementation
  remains a temporary differential oracle.
- Carry `*stringBox` and cached hashes through field slots, table keys, and
  caches. Correctness uses pointer, hash/length, then bytes; it never depends on
  interning.
- Base natives write directly to stack destinations through stable offsets.
  Public host functions remain copying escape barriers, and host overrides stay
  on the cold path.
- Table shapes are immutable constant-pool metadata with verifier-enforced
  remapping. Every table instance retains independent identity and mutable
  storage.
- Pool only cleared VM scratch. Never pool tables, strings, closures, userdata,
  or coroutine identities. Do not introduce a custom arena or VM heap in this
  campaign; a demonstrated Go-GC floor requires a separate ADR.
- Keep at most 71 executable opcodes, add no `Proto` side tables, add no package
  or dependency, and add no benchmark/source recognizer, CGo, assembly, JIT,
  hidden concurrency, or public tuning flag.
- Route every execution task through SimplePower FAST (`gpt-5.6-luna/max`).
  Reserve `gpt-5.6-sol/xhigh` for planning, complete-state review, targeted
  correction decisions, and changed-diff review.

## Four phases

### Phase 1: fair measurement and instruction work

Build the slope-based paired harness and `scripts/check-runtime-parity`. Split
normal execution from the reference adapter, keep PC/register base in locals,
read packed operands directly, remove padding, and move cold bodies out of the
hot symbol. Deepen numeric-for setup/backedge behavior and eliminate safe body
moves. Gate arithmetic progress at median <=1.80x and p90 <=1.85x, dynamic
count <=1,206, no body `MOVE`, and at least 50% reductions in production symbol
size and stack reservation. These are sequencing gates only; final parity
remains median <=0.95x and p90 <=1.00x.

### Phase 2: flat calls and control flow

Replace hot frame-shaped records with active callee locals plus <=48-byte
caller records. Use compiler-proven argument scratch windows, direct fixed
returns, stable indexed upvalues, one dispatcher for script metamethods and
protected calls, and direct coroutine suspension. Gate zero allocations per
ordinary call, recursive Fibonacci <=1.25x, methods/closures/varargs <=1.15x,
and absence of legacy frame/reset/result helpers in profiles. The live command
runs separate frozen groups so recursive Fibonacci and the 1.15x call canaries
cannot be accidentally certified by a shared looser threshold.

### Phase 3: compact values and data paths

Land the 16-byte GC-safe value with fast opcode predicates, boxed string keys,
direct native destinations, immutable table shapes, and coroutine copy removal.
Gate warm field hits at zero hash/byte fallback, zero-allocation scalar natives,
status-only coroutine calls at zero allocations, table/coroutine canaries at
<=1.10x, every Scenario row <=1.25x, and Go runtime/GC work <=10% on remaining
failures. Canary and full-Scenario samples are captured and gated separately.

### Phase 4: measured residuals and parity proof

Re-profile and run only one A/B residual experiment at a time: verified
unchecked register access, 32-bit wordcode, deeper general lowering, compact
mixed-table journals, or additional cleared scratch pooling. Retain a candidate
only if it improves the Scenario geometric mean by at least 5%, helps three
predicted categories, and regresses no row more than 3%. Finish with nine paired
alternating samples and independent complete-state plus changed-diff review.

## Planning progress

- Audited dispatch/lowering, call/control-flow, and table/coroutine paths on the
  clean `c0d24e5` baseline. The resulting structural findings are reflected in
  the phase contracts; no runtime implementation was changed.
- SimplePower `plan lint` and `plan inspect` pass. Capability doctor enforces
  REVIEW exactly as `gpt-5.6-sol/xhigh`; the installed plugin artifact itself
  lacks provenance metadata, which is independent of plan/model enforcement.
- The first independent high-risk review found four blockers: baseline
  preflight, file ownership, executable phase gates, and deterministic
  statistics/reference pinning. This revision applies the targeted correction
  pass for all four. A second semantic review is intentionally not run without
  user choice, as required by the planning skill.
- Implementation was approved and executed through durable SimplePower runs.
  The current publishable slice contains the corrected harness and completed
  Phase 1; Phases 2-4 remain unimplemented.
- User-selected routing assigns every execution task to `gpt-5.6-luna/max`;
  Sol remains the planning and independent-review authority.
- Run `ember-runtime-parity-20260711` accepted preflight and the first harness,
  then blocked Phase 1 because that harness selected `combat_tick` instead of
  `arithmetic_for` and included Luau CLI startup/compilation in timing. This
  revision fixes those measurement defects without changing any speed gate.
- Corrected run `ember-runtime-parity-v2-20260711` measured stable
  `arithmetic_for` progress from 3.99x to 1.7481x median and 1.7785x p90, with
  production symbol and stack reductions above 80%. It then blocked on the
  overly early 1.25x sequencing gate and an undeclared root `ember.test`
  profiling artifact. The approved v3 revision uses 1.80x/1.85x only for Phase
  1, keeps final parity unchanged, requires real named focused tests, and puts
  every generated profile/object under `tmp/runtime-parity/dispatch`.
- Run `ember-runtime-parity-v3-20260711` accepted Phase 1 at 1.6865x median and
  1.7236x p90 with the full Go suite passing, then blocked before Phase 2
  because its script still selected Scenario proxies and the task did not own
  that script. The approved v4 contract gives Phase 2 and Phase 3 ownership of
  only their script branches/artifact directories and requires exact grouped
  workload gates plus real named focused tests.
- Run `ember-runtime-parity-v4-20260711` replayed green Phase 1 code but blocked
  because load average 6-11 made unchanged-code p90 range from 2.9990x to
  5.3032x, and the harness overwrote attempt artifacts. The approved v5
  correction adds fail-before-sampling quiescence and one retained result per
  code fingerprint; no workload or performance threshold changes.
- Run `ember-runtime-parity-v5-20260711` accepted the atomic fingerprinted
  harness and replayed the green Phase 1 implementation. All focused checks,
  both allocation regressions, and `go test -vet=off -count=1 ./...` passed.
  The required 600-second quiet wait expired with no fingerprint, raw sample,
  ratio, or parity attempt created, so the run blocked on external machine load
  without accepting or rejecting performance. The last valid quiet-run Phase 1
  evidence remains v3: 1.6865x median and 1.7236x p90.
- Publication was requested from the isolated v5 worktree on branch
  `codex/runtime-parity-phase1`; local SimplePower journals and ignored raw
  benchmark artifacts are intentionally excluded from source control.

## Review and verification

This is a high-risk broad runtime rewrite. It requires an independent
complete-state review, a targeted correction pass, and an independent
changed-diff review. The final parity command runs correctness, pure-Go build,
repository checks, holdouts, paired measurements, complexity ceilings, and raw
artifact retention. Missing parity is a blocked result, not permission to
weaken the gate or introduce workload-specific behavior.

```simplepower-plan
{
  "schemaVersion": 2,
  "goal": "Bring pure-Go Ember steady-state runtime to buffered parity with Luau 0.728 on every frozen Scenario benchmark without workload-specific production behavior.",
  "risk": "high",
  "tasks": [
    {
      "id": "baseline-preflight",
      "goal": "Prove the implementation starts from the frozen clean baseline on the pinned Luau 0.728 M1 runner before any runtime file can be changed.",
      "contracts": [
        "Before any implementation write, HEAD is exactly c0d24e552b2a741bb76f3e362244266cece3c5d3 and git status --porcelain --untracked-files=no is empty.",
        "The runner is exactly Darwin 24.6.0 arm64 on Apple M1.",
        "The Luau executable SHA-256 is exactly c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd and Homebrew reports version 0.728.",
        "The only output is an untracked tmp/runtime-parity/baseline.json receipt containing the verified commit, platform, CPU, Luau path, version, and digest.",
        "Any mismatch returns BLOCKED before parity-harness begins; no fallback baseline or runner is allowed."
      ],
      "writePaths": ["tmp/runtime-parity/baseline.json"],
      "readPaths": ["go.mod", "AGENTS.md"],
      "dependsOn": [],
      "resources": ["m1-parity-runner"],
      "checkIds": ["baseline-preflight"],
      "policy": "FAST",
      "serial": true
    },
    {
      "id": "parity-harness",
      "goal": "Create a reproducible paired steady-state harness that separates engine entry cost from per-case execution and fails any Scenario row slower than Luau.",
      "contracts": [
        "The 25 Scenario case sources and expected results remain unchanged.",
        "Ember and Luau execute identical case bodies and identical inner iteration counts for N values 1, 10, 100, and 1000.",
        "Ember timing surrounds Run only after Proto compilation; each pinned Luau wrapper records os.clock immediately before and after the same N-loop, then emits elapsed nanoseconds and the scalar result after timing so CLI startup, source compilation, script I/O, and teardown cannot affect the fitted slope.",
        "An empty case selection resolves to exactly the 25 frozen Scenario rows; explicit phase selections resolve unique names across frozen Top10, Classic, and Scenario corpora without changing any source or expected result.",
        "For each engine in each pair, ordinary least squares with an intercept fits T(N)=entry+N*inner over N=1,10,100,1000 using slope=sum((N-meanN)*(T-meanT))/sum((N-meanN)^2); the gate uses Ember slope divided by Luau slope and reports both intercepts separately.",
        "Each row uses nine paired ratios, running Ember then Luau for odd pairs and Luau then Ember for even pairs; after numeric ascending sort, median is item 5 and nearest-rank p90 is item 9.",
        "No measured point is discarded and no outlier filtering is permitted; a missing point, result mismatch, non-finite timing or ratio, or non-positive slope fails closed, while a finite negative intercept is retained and reported as diagnostic data.",
        "Before any raw measurement file is created, the runner polls every ten seconds for up to 600 seconds and requires three consecutive observations with one-minute load average <=2.0 and summed ps process CPU <=100 percent; a busy observation resets the consecutive count, and timeout exits as runner-busy without creating an attempt.",
        "Each phase attempt directory is keyed by SHA-256 of HEAD plus sorted relative-path<TAB>sha256 records for every root Go file, scripts/check-runtime-parity, and scripts/scenario-ratio-gate, so content-preserving path changes cannot collide.",
        "After quiescence, atomic mkdir exclusively claims phase/<fingerprint>; a concurrent or crashed directory without acquisition.complete fails closed and is never overwritten or resumed, while a completed directory is re-gated without measurement.",
        "Capture writes each group to a temporary file and atomically renames it, re-verifies the input fingerprint after all groups, then atomically creates acquisition.complete before any gate runs; therefore a failed gate retains a completed acquisition and every retry only re-gates it.",
        "A deterministic script self-test proves busy samples create no attempt, three consecutive quiet samples unlock exactly one exclusive claim, concurrent and incomplete claims fail closed, completed and failed-gate fingerprints reuse retained data, and a changed post-capture fingerprint is rejected.",
        "Every row median must be <=0.95x and p90 <=1.00x.",
        "Result formatting and validation occur outside timed inner work.",
        "The harness re-verifies Luau executable SHA-256 c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd, Homebrew version 0.728, Darwin 24.6.0 arm64, Apple M1, CGO_ENABLED=0, GOMAXPROCS=1, raw samples, and failure artifact paths.",
        "Generated raw outputs stay under ignored tmp/runtime-parity and are never committed."
      ],
      "writePaths": [
        "scripts/check-runtime-parity",
        "scripts/scenario-ratio-gate",
        "top10_luau_benchmark_test.go",
        "runtime_parity_test.go",
        "tmp/runtime-parity"
      ],
      "readPaths": [
        "README.md",
        "docs/checks.md",
        "docs/compatibility.md",
        "docs/public-surface.md",
        "scripts/check",
        "scripts/check-purego",
        "scripts/bench-summary"
      ],
      "dependsOn": ["baseline-preflight"],
      "resources": ["parity-benchmark-contract", "m1-parity-runner"],
      "checkIds": ["harness-focused"],
      "policy": "FAST",
      "serial": true
    },
    {
      "id": "dispatch-lowering",
      "goal": "Reduce normal execution to a small direct-operand loop and make Ember execute no more numeric-for work than Luau.",
      "contracts": [
        "runFrame selects production or checked reference execution once at entry.",
        "Normal dispatch keeps proto, code, constants, register base, and PC in locals and writes state only at observable edges.",
        "Normal dispatch performs no unpack, trace, counter, hook, budget, or captured-closure work per instruction.",
        "packedInstruction shrinks from 16 to 12 bytes without narrowing existing operands.",
        "Rare debug, host, error, and complex metamethod bodies are noinline cold helpers; common opcode bodies remain in one switch.",
        "NUMERIC_FOR_CHECK performs Luau-compatible start, limit, and step coercion once; NUMERIC_FOR_LOOP performs increment and backedge comparison.",
        "Safe loop variables alias their control register; captured or observable cases retain a semantics-preserving fallback.",
        "The arithmetic fixture executes at most 1206 instructions with no body MOVE.",
        "The Phase 1 sequencing gate reports arithmetic_for median <=1.80x and p90 <=1.85x with no invalid sample or non-positive slope; final parity thresholds remain unchanged.",
        "Production symbol size and stack reservation each fall at least 50 percent from the recorded clean baseline.",
        "Focused verification defines and executes TestRuntimeProductionDispatchBudgets, TestNumericForParity, TestOpcodeCountBudget, and TestProtoSideTableBudget; a missing named test fails the check instead of passing with no tests to run.",
        "Phase 1 may change only the dispatch branch in scripts/check-runtime-parity to median 1.80x and p90 1.85x; calls, data, canary, and final thresholds remain unchanged.",
        "Every generated test binary, object dump, CPU profile, and measurement artifact is written under tmp/runtime-parity/dispatch; no profiling artifact may appear at repository root.",
        "Executable opcode count remains <=71 and no Proto side table is added."
      ],
      "writePaths": [
        "vm.go",
        "bytecode.go",
        "emitter.go",
        "optimizer.go",
        "opcode_info.go",
        "bytecode_test.go",
        "compiler_test.go",
        "optimizer_test.go",
        "scripts/check-runtime-parity",
        "tmp/runtime-parity/dispatch"
      ],
      "readPaths": [
        "value.go",
        "docs/design.md",
        "docs/golang-rules.md",
        "top10_luau_benchmark_test.go"
      ],
      "dependsOn": ["parity-harness"],
      "resources": ["runtime-engine"],
      "checkIds": ["dispatch-focused", "dispatch-parity"],
      "policy": "FAST",
      "serial": true
    },
    {
      "id": "flat-call-engine",
      "goal": "Execute script calls, returns, metamethods, protected calls, and coroutine suspension through active locals and compact caller records without nested Go dispatch.",
      "contracts": [
        "The active callee is held in dispatch locals; the record stack contains suspended callers only.",
        "The ordinary caller record is <=48 bytes and contains no slices, owner/index pair, pending-call object, debug line, cells slice, or result window.",
        "Compiler-proven scratch argument windows become callee register bases; captured or aliasing hazards use a correctness fallback.",
        "One-result returns restore caller locals and write the destination directly; fixed multiple results copy once and open results use cold base/count state.",
        "Ordinary calls allocate zero and do not enter runFrame, runInlineScriptCall, resetFrame, vmFrameResult, vmResultWindow, or returnFrameToCaller.",
        "The calls command retains recursive_fibonacci raw samples and report under tmp/runtime-parity/calls/<fingerprint>/recursive and gates it at median <=1.25x and p90 <=1.50x, then separately retains method_calls, closures_upvalues, and varargs_select under tmp/runtime-parity/calls/<fingerprint>/shapes and gates every row at median <=1.15x and p90 <=1.35x; both subgroup gates must pass and no Scenario proxy may substitute for these frozen cases.",
        "Phase 2 may change only the calls branch in scripts/check-runtime-parity; dispatch, data, canary, and final case lists and thresholds remain unchanged.",
        "Focused verification defines and executes TestRuntimeCallRecord, TestRuntimeOpenUpvalue, TestRuntimeTailCall, TestRuntimeProtectedCall, and TestRuntimeCoroutineSuspension; a missing named test fails the check.",
        "Every generated call profile, object, binary, raw sample, and report stays under tmp/runtime-parity/calls.",
        "Open upvalues use stable stack owner/index references, deduplicate by absolute slot, and close by frame range without stack-growth rebinding.",
        "Script-valued metamethods, pcall, xpcall, and errors use the same dispatcher and cold protected state rather than nested Go execution or frame scans.",
        "Tail-call intent reuses an existing operand or flag only after Luau differential tests establish stack, debug, upvalue, and protection behavior.",
        "Coroutines suspend active locals, caller records, stack, open upvalues, protection, and host continuation directly and copy yielded values once.",
        "Hooks, budgets, host interrupts, traceback PCs, recursion limits, and error text remain behavior-identical through the checked reference adapter."
      ],
      "writePaths": [
        "vm.go",
        "value.go",
        "bytecode.go",
        "emitter.go",
        "base_coroutine.go",
        "base_env.go",
        "base_globals.go",
        "base_table.go",
        "bytecode_test.go",
        "compiler_test.go",
        "vm_test.go",
        "runtime_engine_mvp_test.go",
        "scripts/check-runtime-parity",
        "tmp/runtime-parity/calls"
      ],
      "readPaths": [
        "table_ops.go",
        "docs/design.md",
        "docs/public-surface.md",
        "docs/compatibility.md"
      ],
      "dependsOn": ["dispatch-lowering"],
      "resources": ["runtime-engine"],
      "checkIds": ["calls-focused", "calls-parity"],
      "policy": "FAST",
      "serial": true
    },
    {
      "id": "compact-data-path",
      "goal": "Make values, string fields, base natives, table construction, and coroutine scratch cache-sized without pooling user-visible identity.",
      "contracts": [
        "Value is exactly 16 bytes with a GC-visible unsafe.Pointer word; the 24-byte representation remains a temporary differential oracle until migration completes.",
        "Hot numeric and pointer-kind opcode predicates are inlinable and call no generic kind decoder.",
        "String fields, string table keys, and dynamic caches retain stringBox identity and cached hash; equality falls back through hash, length, and bytes for distinct equal boxes.",
        "Warm constant-box field hits perform zero hash and byte fallback while public independently boxed equal strings remain correct.",
        "Base natives receive stable stack offsets, re-index after re-entry, and write scalar results directly; public host functions remain copying escape barriers.",
        "Highest-frequency guarded builtins stay inline only when A/B evidence proves a generic capability call adds measurable Go-call tax.",
        "Table shapes are immutable typed constant-pool entries with explicit reachability, remapping, verifier, disassembly, clone, and mutation-isolation coverage.",
        "Each table receives independent identity and mutable storage; deterministic insertion, update, delete, and mixed-key iteration order remains unchanged.",
        "Unused coroutine resumeArgs copying is deleted, status Values are immutable/interned, yielded windows copy once, and only cleared dead scratch is reused.",
        "The data command retains table_fields, array_ops, generic_iteration, and coroutine_yield raw samples and report under tmp/runtime-parity/data/<fingerprint>/canaries and gates every canary at median <=1.10x and p90 <=1.25x, then separately retains exactly all 25 Scenario rows under tmp/runtime-parity/data/<fingerprint>/scenarios and gates every row at median <=1.25x and p90 <=1.50x; both subgroup gates must pass.",
        "Phase 3 may change only the data branch in scripts/check-runtime-parity; dispatch, calls, canary, and final case lists and thresholds remain unchanged.",
        "Focused verification defines and executes TestValueCompact, TestRuntimeNative, TestStringBoxIdentity, TestTableIterationParity, and TestRuntimeCoroutineScratch; a missing named test fails the check.",
        "Every generated data profile, object, binary, raw sample, and report stays under tmp/runtime-parity/data; profiles attribute <=10 percent to Go runtime and GC work on every remaining failure.",
        "No table, string, closure, userdata, coroutine identity, custom arena, or VM heap is introduced or pooled."
      ],
      "writePaths": [
        "value.go",
        "vm.go",
        "bytecode.go",
        "emitter.go",
        "table_ops.go",
        "raw_sequence.go",
        "base_convert.go",
        "base_coroutine.go",
        "base_env.go",
        "base_globals.go",
        "base_math.go",
        "base_table.go",
        "bytecode_test.go",
        "compiler_test.go",
        "table_test.go",
        "base_env_test.go",
        "value_compact_test.go",
        "runtime_native_mvp_test.go",
        "runtime_table_template_test.go",
        "scripts/check-runtime-parity",
        "tmp/runtime-parity/data"
      ],
      "readPaths": [
        "docs/adr/0001-go-native-runtime-mapping.md",
        "docs/compatibility.md",
        "docs/public-surface.md",
        "top10_luau_benchmark_test.go"
      ],
      "dependsOn": ["flat-call-engine"],
      "resources": ["runtime-engine"],
      "checkIds": ["data-focused", "data-parity"],
      "policy": "FAST",
      "serial": true
    },
    {
      "id": "residual-parity",
      "goal": "Use fresh profiles to retain only globally useful residual experiments and produce the final statistically buffered parity proof.",
      "contracts": [
        "Fresh Phase 3 CPU, heap, object-code, stack, bounds, dynamic-opcode, cold-exit, tag, field-fallback, and GC evidence ranks residual work.",
        "Only one residual experiment runs at a time: verified unchecked register access, 32-bit wordcode, deeper general lowering, compact mixed-table journals, or cleared VM scratch pooling.",
        "Unsafe register access requires verifier proof and checked-reference differential and fuzz coverage; every pointer remains GC-visible.",
        "32-bit wordcode is attempted only when fetch, decode, or code footprint remains >=5 percent and includes a verified wide fallback.",
        "A lowering rule must remove >=5 percent dynamic instructions across three unrelated failing rows and may not inspect benchmark identity.",
        "A residual winner improves Scenario geometric mean >=5 percent, helps three predicted categories, regresses no row >3 percent, preserves <=71 opcodes, and adds no Proto side table.",
        "Losing experimental representations are deleted rather than retained behind permanent switches.",
        "Final proof uses nine paired alternating samples; every row median is <=0.95x and p90 <=1.00x.",
        "A demonstrated Go-GC or object-allocation floor returns BLOCKED with raw evidence and requires a separate ownership ADR; the parity threshold is not weakened.",
        "The completed work is opened only as a draft PR and is never merged by the implementation run."
      ],
      "writePaths": [
        "vm.go",
        "value.go",
        "bytecode.go",
        "emitter.go",
        "optimizer.go",
        "table_ops.go",
        "raw_sequence.go",
        "base_coroutine.go",
        "bytecode_test.go",
        "compiler_test.go",
        "table_test.go",
        "vm_test.go",
        "top10_luau_benchmark_test.go",
        "scripts/check-runtime-parity",
        "scripts/scenario-ratio-gate",
        "tmp/runtime-parity"
      ],
      "readPaths": [
        "README.md",
        "docs/checks.md",
        "docs/design.md",
        "docs/compatibility.md",
        "docs/public-surface.md",
        "docs/adr/0001-go-native-runtime-mapping.md"
      ],
      "dependsOn": ["compact-data-path"],
      "resources": ["runtime-engine", "parity-benchmark-contract", "m1-parity-runner"],
      "checkIds": ["parity-canaries", "parity-full"],
      "policy": "FAST",
      "serial": true
    }
  ],
  "checks": [
    {
      "id": "baseline-preflight",
      "scope": "focused",
      "command": "sh",
      "args": ["-c", "test \"$(git rev-parse HEAD)\" = \"c0d24e552b2a741bb76f3e362244266cece3c5d3\" && test -z \"$(git status --porcelain --untracked-files=no)\" && test \"$(uname -srm)\" = \"Darwin 24.6.0 arm64\" && test \"$(sysctl -n machdep.cpu.brand_string)\" = \"Apple M1\" && test -n \"$LUAU_BIN\" && test \"$(shasum -a 256 \"$LUAU_BIN\" | awk '{print $1}')\" = \"c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd\" && brew info luau --json=v2 | grep -Eq '\"version\"[[:space:]]*:[[:space:]]*\"0.728\"' && test -f tmp/runtime-parity/baseline.json"],
      "cwd": ".",
      "timeoutMs": 30000,
      "killGraceMs": 5000,
      "maxOutputBytes": 262144,
      "dependsOn": [],
      "inputs": {
        "paths": ["go.mod", "AGENTS.md"],
        "environment": ["LUAU_BIN"],
        "tools": ["git", "sh", "shasum", "uname", "sysctl", "brew", "grep", "awk"]
      },
      "outputs": {"paths": ["tmp/runtime-parity/baseline.json"], "policy": "declared-only"},
      "resources": ["m1-parity-runner"],
      "cache": {"mode": "off"}
    },
    {
      "id": "harness-focused",
      "scope": "focused",
      "command": "sh",
      "args": ["-c", "set -eu; go test -run '^(TestRuntimeParityHarness|TestTop10LuauBenchmarksMatchExpectedResults|TestClassicLuauBenchmarksMatchExpectedResults|TestScenarioLuauBenchmarksMatchExpectedResults)$' -count=1 .; scripts/check-runtime-parity --self-test"],
      "cwd": ".",
      "timeoutMs": 180000,
      "killGraceMs": 5000,
      "maxOutputBytes": 1048576,
      "dependsOn": ["baseline-preflight"],
      "inputs": {
        "paths": ["runtime_parity_test.go", "top10_luau_benchmark_test.go", "scripts/check-runtime-parity", "scripts/scenario-ratio-gate"],
        "environment": ["LUAU_BIN", "CGO_ENABLED", "GOMAXPROCS"],
        "tools": ["go", "luau", "sh", "awk", "git", "ps", "sysctl", "shasum", "find", "sort", "mkdir", "mv", "rm"]
      },
      "outputs": {"paths": [], "policy": "declared-only"},
      "resources": ["go-build-cache", "m1-parity-runner"],
      "cache": {"mode": "off"}
    },
    {
      "id": "dispatch-focused",
      "scope": "focused",
      "command": "sh",
      "args": ["-c", "set -eu; names='TestRuntimeProductionDispatchBudgets TestNumericForParity TestOpcodeCountBudget TestProtoSideTableBudget'; tests=$(go test -list '^(TestRuntimeProductionDispatchBudgets|TestNumericForParity|TestOpcodeCountBudget|TestProtoSideTableBudget)$' .); for name in $names; do printf '%s\\n' \"$tests\" | grep -qx \"$name\"; done; go test -run '^(TestRuntimeProductionDispatchBudgets|TestNumericForParity|TestOpcodeCountBudget|TestProtoSideTableBudget)$' -count=1 ."],
      "cwd": ".",
      "timeoutMs": 240000,
      "killGraceMs": 5000,
      "maxOutputBytes": 1048576,
      "dependsOn": ["harness-focused"],
      "inputs": {
        "paths": ["vm.go", "bytecode.go", "emitter.go", "optimizer.go", "opcode_info.go", "bytecode_test.go", "compiler_test.go", "optimizer_test.go"],
        "environment": ["CGO_ENABLED", "GOMAXPROCS"],
        "tools": ["go", "sh", "grep"]
      },
      "outputs": {"paths": [], "policy": "declared-only"},
      "resources": ["go-build-cache"],
      "cache": {"mode": "off"}
    },
    {
      "id": "dispatch-parity",
      "scope": "focused",
      "command": "scripts/check-runtime-parity",
      "args": ["--phase", "dispatch"],
      "cwd": ".",
      "timeoutMs": 900000,
      "killGraceMs": 10000,
      "maxOutputBytes": 4194304,
      "dependsOn": ["dispatch-focused"],
      "inputs": {
        "paths": ["scripts/check-runtime-parity", "scripts/scenario-ratio-gate", "runtime_parity_test.go", "top10_luau_benchmark_test.go", "vm.go", "bytecode.go", "emitter.go"],
        "environment": ["LUAU_BIN", "CGO_ENABLED", "GOMAXPROCS"],
        "tools": ["go", "luau", "sh", "awk", "git", "ps", "sysctl", "sleep", "shasum", "find", "sort", "mkdir", "mv", "rm"]
      },
      "outputs": {"paths": ["tmp/runtime-parity/dispatch"], "policy": "declared-only"},
      "resources": ["go-build-cache", "m1-parity-runner", "runtime-engine"],
      "cache": {"mode": "off"}
    },
    {
      "id": "calls-focused",
      "scope": "focused",
      "command": "sh",
      "args": ["-c", "set -eu; names='TestRuntimeCallRecord TestRuntimeOpenUpvalue TestRuntimeTailCall TestRuntimeProtectedCall TestRuntimeCoroutineSuspension'; tests=$(go test -list '^(TestRuntimeCallRecord|TestRuntimeOpenUpvalue|TestRuntimeTailCall|TestRuntimeProtectedCall|TestRuntimeCoroutineSuspension)$' .); for name in $names; do printf '%s\\n' \"$tests\" | grep -qx \"$name\"; done; go test -run '^(TestRuntimeCallRecord|TestRuntimeOpenUpvalue|TestRuntimeTailCall|TestRuntimeProtectedCall|TestRuntimeCoroutineSuspension)$' -count=1 ."],
      "cwd": ".",
      "timeoutMs": 360000,
      "killGraceMs": 5000,
      "maxOutputBytes": 2097152,
      "dependsOn": ["dispatch-parity"],
      "inputs": {
        "paths": ["vm.go", "value.go", "bytecode.go", "emitter.go", "base_coroutine.go", "base_globals.go", "base_table.go", "runtime_engine_mvp_test.go", "compiler_test.go", "vm_test.go", "bytecode_test.go"],
        "environment": ["CGO_ENABLED", "GOMAXPROCS"],
        "tools": ["go", "sh", "grep"]
      },
      "outputs": {"paths": [], "policy": "declared-only"},
      "resources": ["go-build-cache", "runtime-engine"],
      "cache": {"mode": "off"}
    },
    {
      "id": "calls-parity",
      "scope": "focused",
      "command": "scripts/check-runtime-parity",
      "args": ["--phase", "calls"],
      "cwd": ".",
      "timeoutMs": 900000,
      "killGraceMs": 10000,
      "maxOutputBytes": 4194304,
      "dependsOn": ["calls-focused"],
      "inputs": {
        "paths": ["scripts/check-runtime-parity", "scripts/scenario-ratio-gate", "runtime_parity_test.go", "top10_luau_benchmark_test.go", "vm.go", "value.go", "emitter.go", "bytecode.go"],
        "environment": ["LUAU_BIN", "CGO_ENABLED", "GOMAXPROCS"],
        "tools": ["go", "luau", "sh", "awk", "git", "ps", "sysctl", "sleep", "shasum", "find", "sort", "mkdir", "mv", "rm"]
      },
      "outputs": {"paths": ["tmp/runtime-parity/calls"], "policy": "declared-only"},
      "resources": ["go-build-cache", "m1-parity-runner", "runtime-engine"],
      "cache": {"mode": "off"}
    },
    {
      "id": "data-focused",
      "scope": "focused",
      "command": "sh",
      "args": ["-c", "set -eu; names='TestValueCompact TestRuntimeNative TestStringBoxIdentity TestTableIterationParity TestRuntimeCoroutineScratch'; tests=$(go test -list '^(TestValueCompact|TestRuntimeNative|TestStringBoxIdentity|TestTableIterationParity|TestRuntimeCoroutineScratch)$' .); for name in $names; do printf '%s\\n' \"$tests\" | grep -qx \"$name\"; done; go test -run '^(TestValueCompact|TestRuntimeNative|TestStringBoxIdentity|TestTableIterationParity|TestRuntimeCoroutineScratch)$' -count=1 ."],
      "cwd": ".",
      "timeoutMs": 360000,
      "killGraceMs": 5000,
      "maxOutputBytes": 2097152,
      "dependsOn": ["calls-parity"],
      "inputs": {
        "paths": ["value.go", "vm.go", "table_ops.go", "raw_sequence.go", "base_coroutine.go", "value_compact_test.go", "runtime_native_mvp_test.go", "runtime_table_template_test.go", "table_test.go"],
        "environment": ["CGO_ENABLED", "GOMAXPROCS"],
        "tools": ["go", "sh", "grep"]
      },
      "outputs": {"paths": [], "policy": "declared-only"},
      "resources": ["go-build-cache", "runtime-engine"],
      "cache": {"mode": "off"}
    },
    {
      "id": "data-parity",
      "scope": "focused",
      "command": "scripts/check-runtime-parity",
      "args": ["--phase", "data"],
      "cwd": ".",
      "timeoutMs": 1200000,
      "killGraceMs": 10000,
      "maxOutputBytes": 8388608,
      "dependsOn": ["data-focused"],
      "inputs": {
        "paths": ["scripts/check-runtime-parity", "scripts/scenario-ratio-gate", "runtime_parity_test.go", "top10_luau_benchmark_test.go", "vm.go", "value.go", "table_ops.go", "base_coroutine.go"],
        "environment": ["LUAU_BIN", "CGO_ENABLED", "GOMAXPROCS"],
        "tools": ["go", "luau", "sh", "awk", "git", "ps", "sysctl", "sleep", "shasum", "find", "sort", "mkdir", "mv", "rm"]
      },
      "outputs": {"paths": ["tmp/runtime-parity/data"], "policy": "declared-only"},
      "resources": ["go-build-cache", "m1-parity-runner", "runtime-engine"],
      "cache": {"mode": "off"}
    },
    {
      "id": "parity-canaries",
      "scope": "focused",
      "command": "scripts/check-runtime-parity",
      "args": ["--phase", "canaries"],
      "cwd": ".",
      "timeoutMs": 900000,
      "killGraceMs": 10000,
      "maxOutputBytes": 4194304,
      "dependsOn": ["data-parity"],
      "inputs": {
        "paths": ["scripts/check-runtime-parity", "scripts/scenario-ratio-gate", "top10_luau_benchmark_test.go", "runtime_parity_test.go", "vm.go", "value.go"],
        "environment": ["LUAU_BIN", "CGO_ENABLED", "GOMAXPROCS"],
        "tools": ["go", "luau", "sh", "awk", "git", "ps", "sysctl", "sleep", "shasum", "find", "sort", "mkdir", "mv", "rm"]
      },
      "outputs": {"paths": ["tmp/runtime-parity/canaries"], "policy": "declared-only"},
      "resources": ["go-build-cache", "m1-parity-runner"],
      "cache": {"mode": "off"}
    },
    {
      "id": "parity-full",
      "scope": "full",
      "command": "scripts/check-runtime-parity",
      "args": [],
      "cwd": ".",
      "timeoutMs": 3600000,
      "killGraceMs": 30000,
      "maxOutputBytes": 16777216,
      "dependsOn": ["harness-focused", "dispatch-parity", "calls-parity", "data-parity", "parity-canaries"],
      "inputs": {
        "paths": ["scripts/check-runtime-parity", "scripts/check", "scripts/check-purego", "scripts/scenario-ratio-gate", "top10_luau_benchmark_test.go", "runtime_parity_test.go", "vm.go", "value.go", "bytecode.go", "emitter.go"],
        "environment": ["LUAU_BIN", "CGO_ENABLED", "GOMAXPROCS"],
        "tools": ["go", "luau", "sh", "awk", "git", "ps", "sysctl", "sleep", "shasum", "find", "sort", "mkdir", "mv", "rm"]
      },
      "outputs": {"paths": ["tmp/runtime-parity"], "policy": "declared-only"},
      "resources": ["go-build-cache", "m1-parity-runner", "parity-benchmark-contract", "runtime-engine"],
      "cache": {"mode": "off"}
    }
  ],
  "review": {
    "policy": "complete-state",
    "independent": true,
    "changedDiffReview": true
  }
}
```
