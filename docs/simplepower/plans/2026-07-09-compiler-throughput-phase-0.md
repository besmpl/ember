# Compiler Throughput Phase 0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `simplepower:subagent-driven-development` for aggregate parallel implementation. Dispatch all non-conflicting `sp-impl` file-edit workers whose coordination needs are satisfied by the approved Interface Contract, run the quick verifier after all workers finish, commit the quick-verified implementation, then run one REVIEW-tier review+fix agent before final verification and final commit.

**Goal:** Establish the exact compile-throughput baseline, conservative opcode-effect safety model, deterministic compiler budgets, and permanent pure-Go gates required before Ember's planned 3-5x compiler work begins.

**Design Summary:** This is the first executable vertical slice, Phase 0/M0, of the approved compiler-throughput design. It preserves the public `Compile` and `Run` surface, measures compile-only workloads rather than compile/run deltas, replaces the flaky 150 microsecond unit-test ceiling with deterministic allocation and output-shape budgets, centralizes all optimizer-visible opcode effects, and proves that metamethod-capable operations cannot make loop-invariant load motion stale. The current dirty worktree at `e47a210` is the baseline to remeasure; existing interpreter work must be preserved. Existing ceilings of 78 executable opcodes and eight runtime-consumed `Proto` side tables, plus the existing benchmark-named-artifact guard, remain no-growth gates. No dependency, CGo path, unsafe compiler arena, new package, public compiler option, global cache, pool, SSA rewrite, or Phase 1-7 optimization is introduced in this slice. Later phases remain separate gated slices after M0 evidence is accepted.

**Architecture:** Test-only metrics adapt private `Proto` and `Program` state to one benchmark data shape while production APIs remain unchanged. Production opcode metadata owns one conservative `opcodeEffects` record per opcode, and optimizer policy queries that record rather than maintaining scattered effect assumptions. The Interface Contract fixes both shapes before dispatch, so benchmark, implementation, semantics-test, budget, and shell-gate workers can edit non-overlapping files in aggregate parallel.

**Tech Stack:** Go 1.26, standard `testing` benchmarks, existing Ember compiler/bytecode/optimizer internals, POSIX shell, repository check scripts, and pure-Go `CGO_ENABLED=0` builds and tests; no new dependencies.

**Model Allocation:** FAST/NORMAL/BEST/REVIEW tiers are assigned below. Resolve each tier by explicit user override, quoted assignment in project root AGENTS.md, process environment variable, then built-in default. The project root AGENTS.md lookup reads only `<repo>/AGENTS.md`, not nested AGENTS.md files or repo-wide grep. FAST defaults to `SIMPLEPOWER_FAST_MODEL` (`gpt-5.6-luna-high` when unset), NORMAL defaults to `SIMPLEPOWER_NORMAL_MODEL` (`gpt-5.6-terra-high` when unset), BEST defaults to `SIMPLEPOWER_BEST_MODEL` (`gpt-5.6-sol-high` when unset), and REVIEW defaults to `SIMPLEPOWER_REVIEW_MODEL` (`gpt-5.6-sol-high` when unset). The plan reviewer is a REVIEW-tier plan reviewer, and the final review+fix agent is a REVIEW-tier review+fix agent. The quick verifier uses the FAST tier by default, resolving to `model="gpt-5.6-luna"` and `reasoning_effort="high"` unless `SIMPLEPOWER_FAST_MODEL` is overridden.

**Commit Policy:** The coordinator commits after the reviewed plan, allocation, and immediate current-session execution receive combined approval, after all file edits and quick verification complete before final review, and after final review/fix plus final verification. Workers, plan reviewers, quick verifiers, and review+fix agents must not commit. No per-task commits. Coordinator-owned temporary scratch refs under `refs/simplepower/scratch/<run-id>/...` may be created only as local review diff anchors; they are not accepted history commits, not pushed, not merged, not rebased, and must be cleaned up after successful checkpoints or reported for manual cleanup on blockers or failed checkpoints.

**Planning Run ID:** `20260709-200814-e47a210`

---

## Interface Contract

### IC-1: Preserved production surface

- `func Compile(source string) (*Proto, error)` remains the standalone compiler entrypoint.
- `func LoadProgram(ctx context.Context, loader ModuleLoader, options ProgramOptions) (*Program, LoadReport, error)` remains the graph compiler entrypoint.
- `Run`, `RunWithGlobals`, bytecode semantics, error text, source lines, yields, and deterministic report ordering remain unchanged.
- This slice adds no exported non-test declaration and no compiler option.

### IC-2: Test-only compiler metric adapter

`compiler_benchmark_metrics_test.go`, in package `ember`, defines:

```go
type CompilerBenchmarkMetrics struct {
    Instructions    int
    Constants       int
    RegisterSlots   int
    ChildProtos     int
    PackedBytes     int64
    ProtoOwnedBytes int64
}

func CompilerBenchmarkMetricsForTest(proto *Proto) CompilerBenchmarkMetrics
func CompilerProgramBenchmarkMetricsForTest(program *Program) CompilerBenchmarkMetrics
```

The metric adapter walks each distinct `Proto` exactly once. `Instructions`, `Constants`, `RegisterSlots`, and `PackedBytes` are sums across the root and all descendants; `ChildProtos` excludes roots. `PackedBytes` is the sum of packed-instruction slice length times packed-instruction width. `ProtoOwnedBytes` is a deterministic retained-size estimate: each distinct `Proto` struct, capacity-backed storage directly owned by its slice fields, bytes held by directly owned strings and string slice elements, and descendant protos are counted once; shared runtime objects reachable through `Value`, tables, host functions, or globals are excluded. Program metrics deduplicate protos shared by module entries. Nil input returns all-zero metrics. These declarations exist only in `_test.go` files and therefore do not change Ember's shipped API.

### IC-3: Compile benchmark command and fixture contract

`compiler_throughput_benchmark_test.go`, in package `ember_test`, defines `BenchmarkCompileMatrix` and `BenchmarkLoadProgramCompile`.

`BenchmarkCompileMatrix` has these stable sub-benchmark families:

- `tiny_arithmetic`
- `straight_line/100`, `straight_line/1000`, and `straight_line/10000`
- `branch_dense_cfg`
- `constants/unique` and `constants/repeated`
- `closures_upvalues`
- `varargs_multi_return`
- `table_string_fields`
- `top10/<existing case name>` for every `top10LuauCases` entry
- `scenario/<existing case name>` for every `scenarioLuauCases` entry

The fixed sources are:

```lua
-- tiny_arithmetic
local x = 1
local y = 2
return (x + y) * 3 - 4 / 2

-- closures_upvalues
local base = 4
local function add(x)
    return base + x
end
return add(3)

-- varargs_multi_return
local function collect(...)
    local a, b = ...
    return a, b, select("#", ...)
end
return collect(1, 2, 3)

-- table_string_fields
local value = {name = "ember", hp = 10}
value.hp = value.hp + 5
return value.name, value.hp
```

Generated sources use one `strings.Builder`, decimal integers from `strconv.Itoa`, and these exact algorithms:

- `straight_line/N`: write `local value = 0\n`; for `i := 1; i <= N; i++`, write `value = value + ` followed by `i%7` and a newline; finish with `return value\n`.
- `branch_dense_cfg`: write `local value = 0\n`; repeat exactly 256 copies of `if flag then\nvalue = value + 1\nelse\nvalue = value + 2\nend\n`; finish with `return value\n`.
- `constants/unique`: write `local total = 0\n`; for `i := 1; i <= 512; i++`, write `total = total + ` followed by `i` and a newline; finish with `return total\n`.
- `constants/repeated`: use the same 512-line form but write the literal `7` on every assignment.

Top10 and Scenario sources are the exact existing `source` fields in `top10LuauCases` and `scenarioLuauCases`; their names and text are not copied or transformed.

Every compile sub-benchmark performs one untimed validation compile, reports allocations, calls `SetBytes(len(source))`, reports every IC-2 field with stable units (`instructions/op`, `constants/op`, `register_slots/op`, `child_protos/op`, `packed_B/op`, and `proto_owned_B/op`), resets the timer, and times only repeated `ember.Compile(source)` calls.

`BenchmarkLoadProgramCompile` uses this exact in-memory shared-dependency graph and covers the Cartesian product of `mode={cold,warm}`, `check={false,true}`, and `parallelism={1,2,4}`:

| Module ID | Source |
|---|---|
| `logical:game/server/init` | `local config = require("../shared/config") return {config = config, side = "server"}` |
| `logical:game/client/init` | `local config = require("../shared/config") return {config = config, side = "client"}` |
| `logical:game/shared/config` | `return {value = 1}` |

Entrypoints, in order, are `{Name: "server", Module: LogicalModule("game/server/init")}` and `{Name: "client", Module: LogicalModule("game/client/init")}`. A valid result has a non-nil program, entrypoint reports `server` then `client`, module reports sorted as `logical:game/client/init`, `logical:game/server/init`, and `logical:game/shared/config`, and no diagnostics. Cold mode constructs a fresh loader and a fresh copy of this three-entry source map inside each timed iteration. Warm mode constructs one immutable concurrency-safe loader, performs one untimed `LoadProgram`, then times repeated `LoadProgram` calls with that loader. Both modes validate the report and program once, report the sum of the three source lengths through `SetBytes`, and report deduplicated IC-2 program metrics from the untimed result. The benchmark never runs program code.

### IC-4: Central opcode-effect data shape

`bytecode.go` defines one private record and stores it on every metadata entry:

```go
type opcodeEffects struct {
    classified                 bool
    invokesScriptOrHostCode    bool
    mayYield                   bool
    mayError                   bool
    allocatesOrObservesIdentity bool
    readsGlobals               bool
    writesGlobals              bool
    readsUpvalues              bool
    writesUpvalues             bool
    readsTables                bool
    writesTables               bool
    readsUnknownHeap           bool
    writesUnknownHeap          bool
}
```

`opcodeMetadataEntry` contains `effects opcodeEffects`; the old top-level `mayCall`, `mayYield`, `readsTable`, `writesTable`, `readsGlobal`, `writesGlobal`, and `allocates` booleans are removed. Every opcode from zero through `opcodeCount-1` has `classified=true`, including pure opcodes whose remaining fields are false.

Classification is the union of the following exact masks. Every bit not assigned by these lists is false.

`callbackMask` sets all twelve non-`classified` fields true. Apply it to exactly:

```text
opGetField opSetField opGetStringField opSetStringField
opGetStringFieldIndex opSetStringFieldIndex opAddStringField opSubStringField
opGetIndex opSetIndex opPrepareIter opArrayNext opArrayNextJump2
opAdd opSub opMul opDiv opMod opIDiv opPow opNeg
opAddK opSubK opMulK opDivK opModK opIDivK
opLen opConcat opConcatChain
opEqual opNotEqual opLess opLessEqual opGreater opGreaterEqual
opJumpIfNotEqualK opJumpIfNotLessK opJumpIfNotGreaterK
opJumpIfLessK opJumpIfGreaterK opJumpIfNotLess opJumpIfNotGreater
opJumpIfLess opJumpIfGreater opJumpIfModKNotEqualK
opJumpIfStringFieldNotEqualK opJumpIfStringFieldNotGreaterK
opJumpIfStringFieldGreaterK opJumpIfStringFieldNotGreaterR
opJumpIfStringFieldFalse opJumpIfStringFieldNil
opJumpIfStringFieldTrue opJumpIfStringFieldNotNil
opCoroutineResume opFastCall opCall opCallOne
opCallLocalOne opCallUpvalueOne opCallMethodOne
```

The callback mask is intentionally conservative: any script, host, or metamethod callback may read or write globals, upvalues, tables, or unknown heap state and may allocate or observe identity.

After that mask, apply these exact direct-effect unions:

- `readsGlobals`: `opLoadGlobal`.
- `writesGlobals`: `opSetGlobal`.
- `readsUpvalues`: `opGetUpvalue`, `opClosure`.
- `writesUpvalues`: `opSetUpvalue`.
- `readsTables`: `opGetField`, `opGetStringField`, `opGetStringFieldIndex`, `opAddStringField`, `opSubStringField`, `opGetIndex`, `opSetIndex`, `opPrepareIter`, `opArrayNext`, `opArrayNextJump2`, `opJumpIfTableHasMetatable`, `opJumpIfStringFieldNotEqualK`, `opJumpIfStringFieldNotGreaterK`, `opJumpIfStringFieldGreaterK`, `opJumpIfStringFieldNotGreaterR`, `opJumpIfStringFieldFalse`, `opJumpIfStringFieldNil`, `opJumpIfStringFieldTrue`, `opJumpIfStringFieldNotNil`, `opFastCall`, and `opCallMethodOne`.
- `writesTables`: `opSetField`, `opSetStringField`, `opSetStringFieldIndex`, `opAddStringField`, `opSubStringField`, `opSetIndex`, and `opFastCall`.
- `allocatesOrObservesIdentity`: `opNewTable`, `opClosure`, and `opVararg` in addition to every callback-mask opcode.
- `mayError`: `opNumericForCheck` in addition to every callback-mask opcode.

`opJumpIfTableHasMetatable` receives only `readsTables=true`. These remaining opcodes have an otherwise zero effect record: `opNoop`, `opLoadConst`, `opMove`, `opNumericForLoop`, `opJumpIfFalse`, `opJump`, `opReturnOne`, and `opReturn`. Together with the direct-effect lists, this partitions all 78 opcodes and leaves no classification choice to a worker.

`validateOpcodeMetadataTable` rejects unclassified entries and rejects `mayYield=true` without `invokesScriptOrHostCode=true`.

### IC-5: Opcode effect query and optimizer policy

`opcode_info.go` defines `func opcodeEffect(op opcode) opcodeEffects`. Invalid opcodes return an unclassified zero value. Existing helper names remain private compatibility adapters and read the central record: `opcodeMayCall` maps to `invokesScriptOrHostCode`, `opcodeMayYield`, table/global read/write helpers, and `opcodeAllocates` map to the matching fields.

`optimizer.go` reads one `effects := opcodeEffect(ins.op)` record at each DCE or loop-invariant barrier decision. An instruction cannot be removed or crossed when it may invoke code, yield, error, allocate/observe identity, write relevant memory, or touch unknown heap state. The narrow guarded string-field LICM may remain enabled only if both IC-6 mutation tests pass; otherwise execution stops for user approval rather than silently changing the approved optimization route.

### IC-6: Effect-safety behavior tests

`compiler_effects_test.go`, in package `ember`, contains:

- `TestOpcodeEffectsCoverEveryOpcode`: every opcode is classified and invalid opcodes are not.
- `TestMetamethodCapableOpcodeEffects`: table-driven cases cover arithmetic, K arithmetic, comparisons and compare branches, length, concatenation, and table reads/writes; each required effect bit matches IC-4.
- `TestOpcodeEffectsRejectYieldWithoutInvocation`: mutated metadata fails validation.
- `TestLoopInvariantLoadTreatsMetamethodOperationsAsBarriers`: direct IR cases place a guarded string-field load at a loop header and an unrelated arithmetic, comparison, length, concat, or table operation in the body; optimization must keep the backedge aimed at the load rather than bypassing it. The arithmetic case is red against the pre-slice metadata.
- `TestLoopInvariantFieldLoadObservesArithmeticMetamethodMutation`: an `__add` callback mutates a captured table field between loop iterations; optimized `Compile`/`Run` returns the sequential result `3`, not a stale hoisted result `2`.
- `TestLoopInvariantFieldLoadObservesIndexMetamethodMutation`: an `__index` callback performs the same captured-table mutation and the optimized result is `3`.
- Both mutation tests also compile through the existing test-only disabled-peephole path and require optimized and unoptimized results to match, so they prove behavior rather than a particular bytecode shape.

### IC-7: Deterministic compiler budget contract

The existing allocation ceiling remains `<= 520` allocations for the arithmetic source. Its wall-clock assertion is deleted. The same source is ratcheted to at most 8 instructions, 3 constants, 3 aggregate register slots, zero child protos, and 8 packed instructions.

`compiler_complexity_test.go` adds no-growth output ceilings measured from the current `e47a210` dirty-worktree baseline. Its exact fixture sources are:

```lua
-- branch_dense
local x = 1
if flag then
    x = x + 2
else
    x = x + 3
end
return x

-- closure_upvalue
local base = 4
local function add(x)
    return base + x
end
return add(3)

-- vararg_multi_return
local function collect(...)
    local a, b = ...
    return a, b, select("#", ...)
end
return collect(1, 2, 3)

-- table_string_fields
local value = {name = "ember", hp = 10}
value.hp = value.hp + 5
return value.name, value.hp
```

The result contract is: `branch_dense` returns number `4` with no `flag` global; `closure_upvalue` returns number `7`; `vararg_multi_return` returns numbers `1`, `2`, and `3`; and `table_string_fields` returns string `"ember"` followed by number `15`.

| Fixture | Instructions | Constants | Register slots | Child protos | Packed instructions |
|---|---:|---:|---:|---:|---:|
| `branch_dense` | 7 | 4 | 2 | 0 | 7 |
| `closure_upvalue` | 9 | 2 | 7 | 1 | 9 |
| `vararg_multi_return` | 11 | 3 | 10 | 1 | 11 |
| `table_string_fields` | 10 | 6 | 4 | 0 | 10 |

Each number is a ceiling except child proto count, which is exact. The test uses the IC-2 adapter, reports the fixture name on failure, and keeps source-to-result assertions alongside shape budgets. Existing `TestOpcodeCountBudget` remains at 78 and `TestScenarioProgramsDoNotEmitBenchmarkNamedArtifacts` remains green.

`proto_budget_test.go` replaces the loophole in the hardcoded side-table list with a complete field classification. It maps allowed `Proto` fields into `core` or `runtimeSideTable`, asserts that every reflected struct field has exactly one classification, then asserts at most eight reflected `runtimeSideTable` fields. The exact side-table set is `numericForLoops`, `intrinsicOps`, `constantKindFacts`, `registerKindFacts`, `numericOperandFacts`, `numericOperandFactPCs`, `slotKindFacts`, and `entryNilRegisters`. The exact allowed core set is `constants`, `constantKeys`, `constantKeyOK`, `constantStringSymbols`, `constantNumbers`, `constantNumberOK`, `globalNames`, `sharedBaseGlobalSlots`, `code`, `packedCode`, `lines`, `prototypes`, `upvalues`, `registers`, `params`, `variadic`, `capturedLocals`, `directFrameDispatch`, `directFrameIndexCache`, `directFrameIndexCaches`, `reuseZeroCaptureClosure`, `canonicalClosure`, and `verifyErr`. `sharedBaseGlobalSlots` is allowed but not required because it belongs to the preserved pre-existing dirty interpreter work and is absent from `HEAD`; every field actually present must be classified. Any future unclassified `Proto` field fails the test, so adding a ninth runtime side table cannot bypass the ratchet.

### IC-8: Pure-Go gate contract

`scripts/check-purego` is executable, uses `set -eu`, changes to the repository root, and runs exactly:

```sh
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./...
```

`scripts/check` invokes `scripts/check-purego` after the existing normal Go test and before `git diff --check`. Failures propagate. No check is skipped or converted to an informational warning.

### IC-9: Cross-task and dirty-worktree assumptions

- All workers operate on the current worktree, not `HEAD` alone. In particular, Task 2 preserves the existing uncommitted `bytecode.go` table-template and shared-global-slot work and changes only metadata/effect-related regions.
- The benchmark and budget workers may compile against IC-2 before its worker finishes; aggregate dispatch waits for all workers before any repository-wide verification.
- Tests may be red while only a subset of aggregate workers has finished. Workers run focused checks that are possible in isolation, then report contract-dependent failures rather than editing another task's files.
- No worker edits `top10_luau_benchmark_test.go`, `program_test.go`, the dirty interpreter execution plan, VM files, or runtime files. Existing fixtures and test helpers are read-only inputs. Task 2 is the sole owner of metadata assertions in the already-dirty `bytecode_test.go` and must preserve every unrelated hunk.

### IC-10: Dirty-file baseline and partial-staging protocol

Before implementation workers start, the coordinator requires an empty real index (`git diff --cached --quiet`) and copies the current `bytecode.go` and `bytecode_test.go` into a coordinator-owned temporary directory. It records each copy's `git hash-object` value and the current combined baseline patch id in working notes. This snapshot is the durable pre-worker baseline; workers must not update it.

The planning-time hashes are `bytecode.go=c52e4036429dd3c1d66a1efd688a0dc234de7ed3`, `bytecode_test.go=eaf6b02130de182f2bc4680ae812c994dcd92a74`, and combined stable patch id `e6ccde0defca70bfb1e92bf4f971117beb3c465e`. The coordinator recomputes and requires these values before dispatch; a mismatch means the approved baseline changed and execution stops for fresh user direction.

```sh
git diff --cached --quiet
SP_DIR="$(mktemp -d)"
cp bytecode.go "$SP_DIR/bytecode.go"
cp bytecode_test.go "$SP_DIR/bytecode_test.go"
git hash-object "$SP_DIR/bytecode.go"
git hash-object "$SP_DIR/bytecode_test.go"
git diff HEAD -- bytecode.go bytecode_test.go | git patch-id --stable
```

At checkpoint 2, the coordinator stages the ten implementation files that were clean or absent at dispatch with `git add -- <paths>`. It does not run `git add` on `bytecode.go` or `bytecode_test.go`. For each dirty file it generates a unified patch from the saved baseline copy to the current file with labels `a/<file>` and `b/<file>`, applies only that delta with `git apply --cached`, and compares the stable patch id of the staged per-file diff with the generated worker-delta patch. A mismatch, failed apply, empty worker delta, or staged file outside the approved list stops the checkpoint.

```sh
git add -- compiler_benchmark_metrics_test.go compiler_throughput_benchmark_test.go opcode_info.go optimizer.go compiler_effects_test.go optimizer_test.go compiler_complexity_test.go proto_budget_test.go scripts/check-purego scripts/check
for file in bytecode.go bytecode_test.go; do
    patch="$SP_DIR/$file.worker.patch"
    status=0
    diff -u --label "a/$file" --label "b/$file" "$SP_DIR/$file" "$file" >"$patch" || status=$?
    test "$status" -eq 1
    test -s "$patch"
    git apply --cached "$patch"
    want="$(git patch-id --stable <"$patch" | awk '{print $1}')"
    got="$(git diff --cached -- "$file" | git patch-id --stable | awk '{print $1}')"
    test -n "$want"
    test "$want" = "$got"
done
test "$(git diff --cached --name-only | sort | tr '\n' ' ')" = "$(printf '%s\n' bytecode.go bytecode_test.go compiler_benchmark_metrics_test.go compiler_complexity_test.go compiler_effects_test.go compiler_throughput_benchmark_test.go opcode_info.go optimizer.go optimizer_test.go proto_budget_test.go scripts/check scripts/check-purego | sort | tr '\n' ' ')"
git diff --cached --check
```

Before committing, the coordinator writes the staged tree, exports it into a temporary directory, and runs `timeout 240s env CGO_ENABLED=0 go test ./...` there. This proves checkpoint 2 is self-contained without the pre-existing dirty worktree changes. If the worker delta cannot apply to `HEAD` or the staged tree fails, the coordinator preserves scratch refs, reports the exact conflict, and asks for fresh user approval; it does not stage or commit the pre-existing hunks.

```sh
SP_TREE="$(git write-tree)"
SP_TREE_DIR="$(mktemp -d)"
git archive "$SP_TREE" | tar -x -C "$SP_TREE_DIR"
(cd "$SP_TREE_DIR" && timeout 240s env CGO_ENABLED=0 go test ./...)
rm -rf "$SP_TREE_DIR"
```

Immediately after checkpoint 2, the coordinator refreshes the two baseline copies and blob ids before the REVIEW-tier review+fix agent starts. Checkpoint 3 repeats the same delta-only staging and staged-tree verification for any review/fix edits to those files. The original unrelated work remains unstaged in the real worktree throughout all three accepted commits.

## File Ownership

| File | Owner task | Change type | Responsibility | Parallel safety notes |
|---|---|---|---|---|
| `compiler_benchmark_metrics_test.go` | Task 1 | create | IC-2 test-only metric adapter | Sole owner; production API untouched |
| `compiler_throughput_benchmark_test.go` | Task 1 | create | IC-3 compile and LoadProgram benchmark matrix | Sole owner; reads existing external-test fixtures only |
| `bytecode.go` | Task 2 | modify | Store and validate IC-4 effects | Sole owner during dispatch; preserve all pre-existing dirty hunks |
| `bytecode_test.go` | Task 2 | modify | Update existing metadata assertions and malformed-entry cases to IC-4 | Sole owner during dispatch; preserve all unrelated pre-existing dirty hunks |
| `opcode_info.go` | Task 2 | modify | IC-5 central effect query and compatibility helpers | Sole owner |
| `optimizer.go` | Task 2 | modify | Consume central effects in DCE and LICM barriers | Sole owner |
| `compiler_effects_test.go` | Task 3 | create | IC-6 exhaustive and behavior safety tests | Sole owner; writes against approved IC-4/IC-5 contracts |
| `optimizer_test.go` | Task 4 | modify | Remove wall-clock gate and retain allocation plus arithmetic shape budget | Sole owner; remove now-unused `time` import only |
| `compiler_complexity_test.go` | Task 4 | create | IC-7 deterministic multi-fixture budgets | Sole owner; consumes IC-2 contract |
| `proto_budget_test.go` | Task 4 | create | Complete `Proto` field classification and eight-side-table ratchet | Sole owner; fails on every unclassified future field |
| `scripts/check-purego` | Task 5 | create | IC-8 CGo-disabled build/test gate | Sole owner; must be executable |
| `scripts/check` | Task 5 | modify | Invoke pure-Go gate in standard checks | Sole owner; preserve existing order and behavior otherwise |

## Implementation Tasks

### Task 1: Build the compile-only evidence matrix

**Goal:** Add deterministic test-only output metrics and the complete compile/LoadProgram benchmark corpus without changing production APIs.

**Contract inputs:** IC-1, IC-2, IC-3, IC-7 fixture ceilings, IC-9, existing `top10LuauCases`, `scenarioLuauCases`, `programTestLoader` conventions, and `ProgramOptions`.

**Serialization required:** No. The declarations and benchmark call sites are fixed by IC-2 and can be created together without waiting for production-effect work.

**Write scope:** `compiler_benchmark_metrics_test.go`, `compiler_throughput_benchmark_test.go`.

**Parallel:** Yes, with Tasks 2, 3, 4, and 5.

**Risk:** Medium. The test-only retained-size estimate and full fixture matrix must avoid double-counting shared protos and accidentally timing validation or setup.

**Model tier:** BEST, resolved as `model="gpt-5.6-sol"`, `reasoning_effort="high"`.

**Worker role:** `sp-impl`.

**Outputs and responsibilities:** Own the exact IC-2 declarations, proto/program tree aggregation, deterministic generated-source helpers, benchmark loader, fixture validation, metric reporting, and stable benchmark names. Do not move or rewrite existing Top10/Scenario data.

**Implementation steps:**

1. Create `compiler_benchmark_metrics_test.go` in package `ember`; implement IC-2 with pointer deduplication for programs and proto trees. Use reflection type sizes for struct and slice element widths; do not use unsafe pointer arithmetic.
2. Create `compiler_throughput_benchmark_test.go` in package `ember_test`. Generate straight-line, branch-dense, unique-constant, and repeated-constant sources deterministically from fixed integer loops; do not use randomness or the clock.
3. Reuse `top10LuauCases` and `scenarioLuauCases` directly. Validate one compile before `ResetTimer`; report IC-2 metrics and source bytes; call `ReportAllocs`; time only `ember.Compile` in the compile matrix.
4. Implement the exact cold/warm LoadProgram contract with a deterministic in-memory diamond graph, `Check` booleans, and parallelism 1/2/4. Validate module/report shape and use the untimed program for metrics.
5. Keep benchmark failures explicit: compilation or loading errors call `b.Fatal`, and unexpected report/module counts call `b.Fatalf` before the timer starts.

**Worker verification:**

- `timeout 60s go test -run '^$' -bench '^BenchmarkCompileMatrix/tiny_arithmetic$' -benchtime=20ms -count=1 .` - expected: one compile benchmark with all six custom metrics and allocation data.
- `timeout 90s go test -run '^$' -bench '^BenchmarkLoadProgramCompile/(cold|warm)/check=(false|true)/parallelism=(1|2|4)$' -benchtime=10ms -count=1 .` - expected: all 12 LoadProgram cells pass.
- `timeout 30s gofmt -d compiler_benchmark_metrics_test.go compiler_throughput_benchmark_test.go` - expected: no output.

**Completion report:** List both created files, exact commands and results, observed benchmark names/metrics, and any retained-size approximation risk. Do not commit.

### Task 2: Centralize conservative opcode effects

**Goal:** Replace scattered optimizer-visible booleans with the complete IC-4 effect record and make optimizer safety decisions consume it.

**Contract inputs:** IC-1, IC-4, IC-5, IC-6 expected semantics, IC-9 dirty-worktree preservation, coordinator-owned IC-10 baseline/staging protocol, current opcode list, current metadata validation, DCE, and guarded string-field LICM.

**Serialization required:** No. IC-4 and IC-5 fix the declarations and behavior that the parallel test worker targets.

**Write scope:** `bytecode.go`, `bytecode_test.go`, `opcode_info.go`, `optimizer.go`.

**Parallel:** Yes, with Tasks 1, 3, 4, and 5.

**Risk:** High. Conservative misclassification can either preserve an unsafe optimization or disable legitimate cleanup across most compiler output, and `bytecode.go` already contains user work that must not be disturbed.

**Model tier:** BEST, resolved as `model="gpt-5.6-sol"`, `reasoning_effort="high"`.

**Worker role:** `sp-impl`.

**Outputs and responsibilities:** Own the effect record, complete opcode classification, metadata validation, existing metadata test migration, effect accessors, DCE removal barriers, and LICM barriers. Preserve opcode count, operands, VM metadata, direct-frame metadata, current dirty changes, unrelated tests, and public behavior.

**Implementation steps:**

1. In the metadata type region of `bytecode.go`, add IC-4 `opcodeEffects`, replace the seven scattered effect fields with `effects`, and initialize `classified=true` for every valid opcode before applying conservative groups.
2. Translate every current effect assignment into the new record, then add the missing metamethod/error/upvalue/unknown-heap groups from IC-4. Prefer small private group-application helpers inside the metadata initializer only when they reduce repeated field assignment.
3. Extend `validateOpcodeMetadataTable` to reject unclassified opcodes and yield-without-invocation while keeping all existing control-flow, operand, and direct-frame validation.
4. In `bytecode_test.go`, update `TestOpcodeMetadataCoversEveryOpcode`, `TestOpcodeMetadataValidationRejectsMalformedEntries`, and their effect expectation helpers to inspect the central record and IC-4 families. Preserve unrelated dirty tests byte-for-byte.
5. In `opcode_info.go`, add `opcodeEffect` and make existing private adapters delegate to it. Add private upvalue, unknown-heap, identity, and may-error queries only if an actual optimizer call site uses them.
6. In `optimizer.go`, replace repeated helper chains in `instructionCanRemoveWhenResultDead` and `loopHasInvariantHeaderLoadBarrier` with one local effect value and conservative checks from IC-5. Do not broaden LICM or add a new optimization.
7. Run a focused diff against the pre-task worktree and confirm no existing table-template, global-slot, opcode operand, VM-facing metadata, or unrelated test hunk was reverted.

**Worker verification:**

- `timeout 90s go test -run '^(TestOpcodeMetadataCoversEveryOpcode|TestOpcodeMetadataValidationRejectsMalformedEntries|TestCompileArithmeticCostBudget)$' -count=1 .` - expected: existing metadata and compiler budget tests pass, or only contract-dependent failures name not-yet-created IC-2 declarations.
- `timeout 90s go test -run 'Test(Compiler|Optimizer|Proto)' -count=1 .` - expected: compiler/optimizer semantics pass.
- `timeout 30s gofmt -d bytecode.go bytecode_test.go opcode_info.go optimizer.go` - expected: no output.

**Completion report:** List the four modified files, summarize opcode groups and migrated assertions, commands/results, any conservative optimization loss observed, and unresolved classification uncertainty. Do not commit.

### Task 3: Prove effect completeness and metamethod invalidation

**Goal:** Add exhaustive metadata tests and public source-to-result regressions that expose stale LICM across arithmetic and `__index` callbacks.

**Contract inputs:** IC-1, IC-4, IC-5, IC-6, IC-9, current test-only disabled-optimization compilation helpers, and current metatable support.

**Serialization required:** No. The test names, private data shape, and expected behavior are fixed by the Interface Contract while Task 2 creates the implementation.

**Write scope:** `compiler_effects_test.go`.

**Parallel:** Yes, with Tasks 1, 2, 4, and 5.

**Risk:** Medium. Tests must trigger the semantic hazard through normal `Compile`/`Run` and avoid falsely passing because the intended loop form was not compiled.

**Model tier:** NORMAL, resolved as `model="gpt-5.6-terra"`, `reasoning_effort="high"`.

**Worker role:** `sp-impl`.

**Outputs and responsibilities:** Own all IC-6 tests and local assertion helpers. Tests may inspect private metadata for completeness but must prove optimizer correctness through compiled source behavior.

**Implementation steps:**

1. Add the exhaustive classification and validation tests with table-driven opcode families matching IC-4 exactly.
2. Add direct IR red tracers with an explicit no-metatable guard, string-field header load, metamethod-capable body instruction on unrelated registers, and a backedge. Assert optimization does not retarget the backedge past the load for every IC-4 metamethod family.
3. Add an arithmetic-metamethod program whose `__add` callback increments `state.value` after the loop reads it; assert two iterations return `3`.
4. Add the equivalent `__index` mutation case and expected result `3`.
5. Compile each source through default options and the existing disabled-bytecode-peephole test seam; run both and require equal scalar results and equal errors.
6. Keep test names general and mechanism-focused; do not name a Top10 or Scenario row.

**Worker verification:**

- `timeout 90s go test -run '^(TestOpcodeEffectsCoverEveryOpcode|TestMetamethodCapableOpcodeEffects|TestOpcodeEffectsRejectYieldWithoutInvocation|TestLoopInvariantLoadTreatsMetamethodOperationsAsBarriers|TestLoopInvariantFieldLoadObservesArithmeticMetamethodMutation|TestLoopInvariantFieldLoadObservesIndexMetamethodMutation)$' -count=1 .` - expected after aggregate integration: all tests pass; before Task 2 lands, compile failures may only be missing IC-4 declarations.
- `timeout 30s gofmt -d compiler_effects_test.go` - expected: no output.

**Completion report:** List the created file, commands/results, confirm the direct IR cases exercise the LICM candidate and the two programs prove public sequential semantics, and report any unsupported language behavior rather than weakening the tests. Do not commit.

### Task 4: Replace timing with deterministic compiler budgets

**Goal:** Remove the wall-clock unit-test gate while preserving allocation pressure and ratcheting representative output complexity to the measured Phase 0 baseline.

**Contract inputs:** IC-2, IC-7, IC-9, current `TestCompileArithmeticCostBudget`, and existing opcode/side-table/artifact guards.

**Serialization required:** No. IC-2 and IC-7 supply exact fields and thresholds before Task 1 finishes.

**Write scope:** `optimizer_test.go`, `compiler_complexity_test.go`, `proto_budget_test.go`.

**Parallel:** Yes, with Tasks 1, 2, 3, and 5.

**Risk:** Medium. Overly exact shape tests can block valid future improvements; every threshold must be a no-growth ceiling rather than bytecode-sequence snapshot, except exact child-proto counts.

**Model tier:** NORMAL, resolved as `model="gpt-5.6-terra"`, `reasoning_effort="high"`.

**Worker role:** `sp-impl`.

**Outputs and responsibilities:** Own removal of `time`-based assertions, the existing allocation limit, arithmetic metric ceilings, four fixture sources, source result checks, table-driven complexity assertions, and the complete `Proto` field/side-table classification.

**Implementation steps:**

1. In `optimizer_test.go`, remove the `time` import and elapsed-time loop from `TestCompileArithmeticCostBudget`; compile once for metrics, keep `testing.AllocsPerRun(100, ...) <= 520`, and assert the arithmetic IC-7 ceilings.
2. Create `compiler_complexity_test.go` in package `ember`. Use the exact sources from IC-7's baseline: branch, closure/upvalue, vararg/multi-return, and table/string-field programs.
3. For each case, run the proto and assert its observable result before checking ceilings. Compare IC-2 metrics field by field; convert the packed-byte metric to packed-instruction count using the packed instruction width.
4. Create `proto_budget_test.go` with IC-7's exact allowed `core` and `runtimeSideTable` sets. Reflect over `Proto`, fail on any actual field absent from both sets or present in both sets, and enforce a maximum of eight reflected runtime side tables. Do not require the optional dirty-worktree `sharedBaseGlobalSlots` field to exist in the staged `HEAD`-based tree.
5. Do not edit or loosen the existing 78-opcode, existing side-table, or benchmark-artifact tests.

**Worker verification:**

- `timeout 90s go test -run '^(TestCompileArithmeticCostBudget|TestCompilerComplexityBudgets|TestProtoFieldClassificationBudget|TestOpcodeCountBudget|TestProtoSideTableBudget|TestScenarioProgramsDoNotEmitBenchmarkNamedArtifacts)$' -count=1 .` - expected after aggregate integration: all no-growth gates pass.
- `timeout 30s gofmt -d optimizer_test.go compiler_complexity_test.go proto_budget_test.go` - expected: no output.

**Completion report:** List all three files, commands/results, exact retained allocation and shape ceilings, and any metric-contract dependency. Do not commit.

### Task 5: Make pure-Go support a permanent check

**Goal:** Add the exact CGo-disabled build/test gate and wire it into the standard repository check.

**Contract inputs:** IC-8, IC-9, existing `scripts/check` order and shell conventions, and Go module root behavior.

**Serialization required:** No. This shell-only task has no file or declaration overlap with the Go workers.

**Write scope:** `scripts/check-purego`, `scripts/check`.

**Parallel:** Yes, with Tasks 1, 2, 3, and 4.

**Risk:** Low. The change is mechanical, but the executable bit and failure propagation must be correct.

**Model tier:** FAST, resolved as `model="gpt-5.6-luna"`, `reasoning_effort="high"`.

**Worker role:** `sp-impl`.

**Outputs and responsibilities:** Own the executable pure-Go helper and its invocation from the standard check. Do not alter formatting, shell syntax, normal test, or diff-check behavior.

**Implementation steps:**

1. Create `scripts/check-purego` with IC-8's exact commands and repository-root `cd` pattern.
2. Set mode `0755` with `chmod +x scripts/check-purego`.
3. Insert `scripts/check-purego` into `scripts/check` after the normal Go test and before the Git diff check.

**Worker verification:**

- `timeout 30s sh -n scripts/check scripts/check-purego` - expected: exit 0.
- `timeout 180s scripts/check-purego` - expected: pure-Go build and tests pass.

**Completion report:** List both files including the new mode, commands/results, and any CGo-disabled failure. Do not commit.

## Model Allocation

No current-session user override was supplied. Root `AGENTS.md` contains no quoted Simple Power model assignment, and all four process variables are unset, so built-in defaults resolve as follows.

| Stage | Role | Model tier | Resolved model | Reasoning effort | Reason |
|---|---|---|---|---|---|
| Implementation Task 1 | `sp-impl` benchmark/metric worker | BEST | `gpt-5.6-sol` | high | Cross-package test adapter, retained-size accounting, and broad corpus design are easy to measure incorrectly |
| Implementation Task 2 | `sp-impl` effect implementation worker | BEST | `gpt-5.6-sol` | high | Behavior-shaping, cross-cutting optimizer safety work on a dirty core file |
| Implementation Task 3 | `sp-impl` effect semantics worker | NORMAL | `gpt-5.6-terra` | high | Localized tests against a fully specified behavior contract |
| Implementation Task 4 | `sp-impl` deterministic budget worker | NORMAL | `gpt-5.6-terra` | high | Localized test conversion with exact thresholds and moderate brittleness risk |
| Implementation Task 5 | `sp-impl` pure-Go gate worker | FAST | `gpt-5.6-luna` | high | Obvious two-file shell wiring |
| Plan review | Plan document reviewer | REVIEW | `gpt-5.6-sol` | high | Must validate contract, ownership, allocation, and execution policy as one artifact |
| Quick verification | Quick verifier | FAST | `gpt-5.6-luna` | high | Runs fixed commands and may make only typo-level fixes |
| Final review and fix | Whole-implementation reviewer/fixer | REVIEW | `gpt-5.6-sol` | high | Reviews semantics, dirty-worktree preservation, benchmarks, and gates across the whole slice |

## Plan Review

The coordinator self-reviews the saved plan for Design Summary coverage, Interface Contract completeness, one-owner file scopes, contract-backed aggregate dispatch, model resolution, exactly three checkpoints, concrete timeout commands, scratch-ref lifecycle, and approved-path enforcement before dispatching a reviewer.

For this run, the coordinator creates `refs/simplepower/scratch/20260709-200814-e47a210/plan-review/before` from `docs/simplepower/plans/2026-07-09-compiler-throughput-phase-0.md` with a temporary index before first review. Only the coordinator may create or delete scratch refs.

The REVIEW-tier plan reviewer uses `model="gpt-5.6-sol"`, `reasoning_effort="high"` and performs the review directly in the current worker. It must not run Codex CLI, spawn subagents, invoke Simple Power skills, restart execution, reroute the workflow, edit files, create refs, or commit.

If it reports a blocking issue, the coordinator edits only the plan, reruns focused self-review for the changed categories, creates `plan-review/after-1`, and sends the same reviewer:

```sh
git diff refs/simplepower/scratch/20260709-200814-e47a210/plan-review/before refs/simplepower/scratch/20260709-200814-e47a210/plan-review/after-1 -- docs/simplepower/plans/2026-07-09-compiler-throughput-phase-0.md
```

Further revisions use `after-N` and compare the immediately previous ref to the new ref. A missing anchor stops the review loop. The same reviewer remains open until approval, unrecoverable interruption, or explicit user direction.

After reviewer approval, the coordinator asks for one combined user approval covering the reviewed plan, the model/task allocation, and immediate current-session execution. No accepted-plan commit occurs before that approval. After the accepted-plan checkpoint succeeds, the coordinator deletes this run's `plan-review` refs. If approval is withheld, the checkpoint fails, or execution stops, refs remain as evidence and the coordinator reports the manual cleanup command in Commit Checkpoints.

## Quick Verification

After all five aggregate `sp-impl` workers finish, the coordinator creates `refs/simplepower/scratch/20260709-200814-e47a210/quick-verifier/before` for the twelve approved implementation files using a temporary index. The FAST-tier quick verifier then runs:

```sh
timeout 30s sh -c 'test -z "$(gofmt -l compiler_benchmark_metrics_test.go compiler_throughput_benchmark_test.go bytecode.go bytecode_test.go opcode_info.go optimizer.go compiler_effects_test.go optimizer_test.go compiler_complexity_test.go proto_budget_test.go)"'
timeout 30s sh -n scripts/check scripts/check-purego
timeout 30s git diff --check -- compiler_benchmark_metrics_test.go compiler_throughput_benchmark_test.go bytecode.go bytecode_test.go opcode_info.go optimizer.go compiler_effects_test.go optimizer_test.go compiler_complexity_test.go proto_budget_test.go scripts/check-purego scripts/check
timeout 60s env CGO_ENABLED=0 go build ./...
timeout 120s go test -count=1 -run '^(TestCompileArithmeticCostBudget|TestCompilerComplexityBudgets|TestProtoFieldClassificationBudget|TestOpcodeCountBudget|TestProtoSideTableBudget|TestScenarioProgramsDoNotEmitBenchmarkNamedArtifacts|TestOpcodeEffectsCoverEveryOpcode|TestMetamethodCapableOpcodeEffects|TestOpcodeEffectsRejectYieldWithoutInvocation|TestLoopInvariantLoadTreatsMetamethodOperationsAsBarriers|TestLoopInvariantFieldLoadObservesArithmeticMetamethodMutation|TestLoopInvariantFieldLoadObservesIndexMetamethodMutation)$' .
timeout 180s go test -run '^$' -bench '^(BenchmarkCompileMatrix|BenchmarkLoadProgramCompile)$' -benchmem -benchtime=50ms -count=1 .
```

Expected result: all approved Go files are formatted, both shell files parse, diff whitespace is clean, the pure-Go build succeeds, focused safety/budget tests are green, and every benchmark family executes with allocations plus IC-2 custom metrics.

The quick verifier may fix only tiny typo-level errors found by these commands. It must report any behavior change, structural edit, test rewrite, public interface change, dirty-hunk conflict, or unclear issue to the coordinator without fixing it. If it makes a typo-only edit, the coordinator creates `quick-verifier/after` and inspects:

```sh
git diff refs/simplepower/scratch/20260709-200814-e47a210/quick-verifier/before refs/simplepower/scratch/20260709-200814-e47a210/quick-verifier/after -- compiler_benchmark_metrics_test.go compiler_throughput_benchmark_test.go bytecode.go bytecode_test.go opcode_info.go optimizer.go compiler_effects_test.go optimizer_test.go compiler_complexity_test.go proto_budget_test.go scripts/check-purego scripts/check
```

If no edit occurs, there is no `after` ref. After the quick-verified implementation checkpoint succeeds, the coordinator deletes the quick-verifier refs. On a blocker or failed checkpoint they remain for manual cleanup.

## Final Review And Fix

After the quick-verified implementation checkpoint, the coordinator creates `refs/simplepower/scratch/20260709-200814-e47a210/review-fix/before` for the twelve approved implementation files and dispatches exactly one REVIEW-tier review+fix agent with `model="gpt-5.6-sol"`, `reasoning_effort="high"`.

That agent reviews the complete implementation against this plan, the IC-4 opcode-family completeness, public optimized/unoptimized metamethod behavior, benchmark timing boundaries, deterministic budget ceilings, dirty-worktree preservation, executable shell mode, and pure-Go integration. It may edit only the approved implementation files and must report changed files, commands, results, remaining risks, and deviations needing user approval. It must not commit, create refs, run Codex CLI, spawn subagents, invoke Simple Power skills, restart execution, or reroute the workflow.

If it edits files, the coordinator creates `review-fix/after` and inspects:

```sh
git diff refs/simplepower/scratch/20260709-200814-e47a210/review-fix/before refs/simplepower/scratch/20260709-200814-e47a210/review-fix/after -- compiler_benchmark_metrics_test.go compiler_throughput_benchmark_test.go bytecode.go bytecode_test.go opcode_info.go optimizer.go compiler_effects_test.go optimizer_test.go compiler_complexity_test.go proto_budget_test.go scripts/check-purego scripts/check
```

If no edit occurs, there is no `after` ref. After the final checkpoint succeeds, the coordinator deletes review-fix refs. On a blocker or failed checkpoint they remain for manual cleanup.

## Commit Checkpoints

Exactly three future accepted commits are coordinator-owned:

1. **Accepted plan checkpoint:** After the plan reviewer approves and the user gives combined approval for this reviewed plan, allocation, and immediate current-session execution. Stage only `docs/simplepower/plans/2026-07-09-compiler-throughput-phase-0.md`, commit it, delete successful plan-review refs, and immediately invoke `simplepower:subagent-driven-development`.
2. **Quick-verified implementation checkpoint:** After all five `sp-impl` workers finish and the quick verifier passes. Use IC-10 to stage only the twelve approved implementation deltas, verify the staged tree independently, commit it, then delete successful quick-verifier refs.
3. **Final checkpoint:** After the one REVIEW-tier review+fix agent finishes and every final verification command passes. Refresh and use IC-10 to stage only approved review/fix deltas, independently verify the staged tree, create the final commit, then delete successful review-fix refs.

Workers, the plan reviewer, quick verifier, review+fix agent, and individual tasks must not commit. There are no per-task commits. Scratch refs are coordinator-owned local diff anchors, never branches or accepted history, and are never pushed, merged, or rebased.

After each successful phase checkpoint, cleanup uses:

```sh
git for-each-ref --format='%(refname)' 'refs/simplepower/scratch/20260709-200814-e47a210/<phase>' | while read -r ref; do git update-ref -d "$ref"; done
```

If user direction stops the workflow, a blocker prevents the approved path, or a checkpoint commit fails, preserve remaining refs and report this manual cleanup command rather than running it:

```sh
git for-each-ref --format='%(refname)' 'refs/simplepower/scratch/20260709-200814-e47a210' | while read -r ref; do git update-ref -d "$ref"; done
```

After the final checkpoint, follow the repository rule to open or update the PR for the current `codex/` branch and never merge it.

## Current-Session Auto-Dispatch

The saved Markdown plan is the only execution artifact; do not create implementation JSON or offer another route. After combined approval, the coordinator creates checkpoint 1, cleans plan-review refs, and immediately invokes `simplepower:subagent-driven-development` in this session with:

```text
Execute `docs/simplepower/plans/2026-07-09-compiler-throughput-phase-0.md` with aggregate parallel implementation from the approved Interface Contract. Use the approved FAST/NORMAL/BEST/REVIEW model allocation. Dispatch all non-conflicting `sp-impl` file-edit workers whose coordination needs are satisfied by their Contract inputs, run the quick FAST-tier verifier with lint/build/tests and timeouts after all workers finish, commit the quick-verified implementation, then run one REVIEW-tier review+fix agent, final verification, and final commit.
```

Tasks 1-5 dispatch together because file scopes do not overlap and IC-2 through IC-10 fully specify their shared declarations, behavior, and dirty-file preservation. Do not replace aggregate dispatch with prerequisite staging. If the accepted contract or current worktree does not support the approved path, stop and request fresh explicit user approval before changing scope, files, tests, optimization policy, or execution mode.

## Verification

Run after the REVIEW-tier review+fix agent completes, in this order:

| Command | Timeout | Expected result | Failure means |
|---|---:|---|---|
| `go test -count=1 -run '^(TestCompileArithmeticCostBudget|TestCompilerComplexityBudgets|TestProtoFieldClassificationBudget|TestOpcodeCountBudget|TestProtoSideTableBudget|TestScenarioProgramsDoNotEmitBenchmarkNamedArtifacts|TestOpcodeEffectsCoverEveryOpcode|TestMetamethodCapableOpcodeEffects|TestOpcodeEffectsRejectYieldWithoutInvocation|TestLoopInvariantLoadTreatsMetamethodOperationsAsBarriers|TestLoopInvariantFieldLoadObservesArithmeticMetamethodMutation|TestLoopInvariantFieldLoadObservesIndexMetamethodMutation)$' .` | 120s | All Phase 0 budget/effect and no-growth tests pass | Contract, classification, deterministic baseline, or complexity ratchet is wrong |
| `go test -count=1 -run '^Test(Top10|Classic|Scenario)LuauBenchmarksMatchExpectedResults$' .` | 180s | Existing corpus results remain correct | Conservative effect changes altered compiled behavior |
| `go test -run '^$' -bench '^(BenchmarkCompileMatrix|BenchmarkLoadProgramCompile)$' -benchmem -benchtime=250ms -count=5 .` | 600s | Every matrix cell runs and reports allocations, bytes/s, and all IC-2 metrics | Benchmark coverage, setup boundaries, or fixture integration is incomplete |
| `scripts/check-fast` | 240s | Repository fast sweep passes | Formatting, shell, test, or diff integration failed |
| `scripts/check-purego` | 240s | CGo-disabled build and full tests pass | The pure-Go support gate is not met |
| `scripts/check` | 360s | Standard checks, including the wired pure-Go gate, pass | Final repository proof is incomplete |

The coordinator creates the final checkpoint only after the review+fix agent has finished and every command passes. The final report records benchmark baselines rather than asserting a Phase 1 speedup, lists changed files and all checks, calls out any conservative optimizer regression, and confirms that later compiler phases remain unimplemented.

Finally run:

```sh
git for-each-ref --format='%(refname)' 'refs/simplepower/scratch/20260709-200814-e47a210'
```

After a successful final checkpoint and phase cleanup, this prints nothing. If execution stopped or a checkpoint failed, preserve the listed refs and report the manual cleanup command from Commit Checkpoints.

## Approved Path Enforcement

This plan authorizes only Phase 0/M0. It does not authorize Phase 1 artifact reuse/finalization work, allocation-free dataflow, binder indexing, opcode deletion, SCCP/CSE/broader LICM, frontend arenas, O2, caching, pools, dependencies, CGo, native code, public options, docs-only substitutes, stubs, reduced benchmark coverage, skipped review, skipped verification, or alternate execution routes. A failed metamethod tracer does not pre-authorize disabling LICM; a benchmark or metric implementation difficulty does not pre-authorize dropping a metric; a dirty-file conflict does not pre-authorize reverting user work. Any such deviation requires the coordinator to stop, show the exact mismatch and completed work, and obtain fresh explicit user approval.
