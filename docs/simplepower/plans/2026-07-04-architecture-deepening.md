# Architecture Deepening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `simplepower:subagent-driven-development` for aggregate parallel implementation. Dispatch all non-conflicting `sp-impl` file-edit workers whose coordination needs are satisfied by the approved Interface Contract, run the quick verifier after all workers finish, commit the quick-verified implementation, then run one REVIEW-tier review+fix agent before final verification and final commit.

**Goal:** Deepen Ember's root runtime modules so table/metamethod behavior, raw table sequences, bytecode emission, expression shaping, and checker tests have better locality without expanding the public package surface.

**Design Summary:** The approved architecture review found shallow modules and leaking seams in the flat root package: table and metamethod semantics are split across `value.go`, `vm.go`, and `baselib.go`; raw sequence behavior repeats through the table base library; bytecode is a thin data carrier whose operand conventions leak into the emitter and VM; expression parser nesting leaks into emitter helpers and tests; checker depth is mostly hidden behind parser-internal tests. The implementation must keep Ember Go-native, preserve current public behavior, avoid new dependencies, keep the root package flat, and prove behavior through `Compile`, `Run`, `RunWithGlobals`, `Table.Get`, `Table.Set`, and `Check`.

**Architecture:** Add private root-package modules, not new packages: a table-access module, a raw-sequence module, a bytecode-builder module, expression-shape helpers, and checker-facing test helpers. The Interface Contract below gives exact internal names and behavior so runtime, compiler, and checker workers can edit disjoint files in aggregate parallel while relying on shared public behavior and agreed private module names.

**Tech Stack:** Go root package `github.com/besmpl/ember`; standard library only; existing scripts `scripts/check-lane root`, `scripts/check-fast`, and `scripts/check`; no new dependencies.

**Model Allocation:** FAST/NORMAL/BEST/REVIEW tiers are assigned below. Resolve each tier by explicit user override, quoted assignment in project root AGENTS.md, process environment variable, then built-in default. The project root AGENTS.md lookup reads only `<repo>/AGENTS.md`, not nested AGENTS.md files or repo-wide grep. FAST defaults to `SIMPLEPOWER_FAST_MODEL` (`gpt-5.3-codex-spark-high` when unset), NORMAL defaults to `SIMPLEPOWER_NORMAL_MODEL` (`gpt-5.4-high` when unset), BEST defaults to `SIMPLEPOWER_BEST_MODEL` (`gpt-5.5-high` when unset), and REVIEW defaults to `SIMPLEPOWER_REVIEW_MODEL` (`gpt-5.5-xhigh` when unset). The plan reviewer is a REVIEW-tier plan reviewer, and the final review+fix agent is a REVIEW-tier review+fix agent. The quick verifier uses the FAST tier by default, resolving to `model="gpt-5.3-codex-spark"` and `reasoning_effort="high"` unless `SIMPLEPOWER_FAST_MODEL` is overridden.

**Commit Policy:** The coordinator commits after the reviewed plan, allocation, and immediate current-session execution receive combined approval, after all file edits and quick verification complete before final review, and after final review/fix plus final verification. Workers, plan reviewers, quick verifiers, and review+fix agents must not commit. No per-task commits. Coordinator-owned temporary scratch refs under `refs/simplepower/scratch/<run-id>/...` may be created only as local review diff anchors; they are not accepted history commits, not pushed, not merged, not rebased, and must be cleaned up after successful checkpoints or reported for manual cleanup on blockers or failed checkpoints.

---

## Interface Contract

### IC-PUBLIC-1: Public behavior stays stable

- `Compile(source string) (*Proto, error)` continues to parse supported Luau and emit executable bytecode.
- `Check(source string) error` continues to require a `--!strict` directive and validate syntax only.
- `Run(proto *Proto) ([]Value, error)` and `RunWithGlobals(proto *Proto, globals map[string]Value) ([]Value, error)` continue to execute compiled prototypes with base globals plus host globals.
- `Table.Get(key Value) (Value, error)` and `Table.Set(key Value, value Value) error` continue to support raw storage plus table-valued `__index` and `__newindex`, reject nil and NaN keys, and preserve current errors for unsupported function-valued public table access.
- No new exported package names are approved by this plan. Any need for a new exported name is an approved-path deviation requiring fresh user approval.

### IC-RUNTIME-1: Private table access module

Create `/Users/mark/Desktop/ember/table_ops.go` with these unexported names:

- `type tableAccess struct { globals *globalEnv; functionMetamethods bool }`
- `func publicTableAccess() tableAccess`
- `func runtimeTableAccess(globals *globalEnv) tableAccess`
- `func (a tableAccess) get(table *Table, key Value) (Value, error)`
- `func (a tableAccess) set(table *Table, key Value, value Value) error`
- `func (a tableAccess) protectedMetatable(table *Table) (Value, error)`
- `func (a tableAccess) callIndex(fn Value, table *Table, key Value) (Value, error)`
- `func (a tableAccess) callNewIndex(fn Value, table *Table, key Value, value Value) error`

Behavior guarantees:

- `publicTableAccess()` sets `functionMetamethods` to false and is used only by public `Table.Get` and `Table.Set`.
- `runtimeTableAccess(globals)` sets `functionMetamethods` to true and is used by VM opcode handlers and base-library functions that have `*globalEnv`.
- Both access modes share one implementation for raw lookup, table-valued metatable chains, cycle detection, nil deletion, protected metatable lookup, and error wording prefixes beginning with `table:`.
- Function-valued `__index` and `__newindex` call through existing `callValue` only when `functionMetamethods` is true.
- Existing helpers `getTableValue`, `setTableValue`, `callIndexMetamethod`, and `callNewIndexMetamethod` in `vm.go` are removed or replaced by calls into `tableAccess`.

### IC-RUNTIME-2: Private raw sequence module

Create `/Users/mark/Desktop/ember/raw_sequence.go` with these unexported names:

- `type rawSequence struct { table *Table; label string }`
- `func newRawSequence(label string, table *Table) rawSequence`
- `func (s rawSequence) len() (int, error)`
- `func (s rawSequence) get(index int) (Value, error)`
- `func (s rawSequence) set(index int, value Value) error`
- `func (s rawSequence) values(start int, end int) ([]Value, error)`
- `func (s rawSequence) insert(position int, value Value) error`
- `func (s rawSequence) remove(position int) (Value, error)`
- `func (s rawSequence) clear()`
- `func (s rawSequence) writeValues(values []Value) error`

Behavior guarantees:

- Methods use `Table.rawLen`, `Table.rawGet`, and `Table.rawSet`; they do not invoke metatables.
- Error messages wrap storage errors with the caller label, for example `table.insert: <reason>`.
- `insert`, `remove`, `values`, and `writeValues` preserve current base-library observable behavior for prefix arrays.
- `clear` is the only approved place outside `value.go` to iterate over `table.fields` directly.

### IC-COMPILER-1: Private bytecode builder module

Modify `/Users/mark/Desktop/ember/bytecode.go` to include these unexported names:

- `type bytecodeBuilder struct { constants []Value; code []instruction; prototypes []*Proto }`
- `func (b *bytecodeBuilder) addConstant(value Value) int`
- `func (b *bytecodeBuilder) addPrototype(proto *Proto) int`
- `func (b *bytecodeBuilder) emit(ins instruction) int`
- `func (b *bytecodeBuilder) emitLoadConst(target int, value Value) int`
- `func (b *bytecodeBuilder) emitJump() int`
- `func (b *bytecodeBuilder) emitJumpIfFalse(condition int) int`
- `func (b *bytecodeBuilder) patchJump(at int, target int)`
- `func (b *bytecodeBuilder) proto(upvalues []upvalueDesc, registers int, params int, variadic bool) *Proto`

Behavior guarantees:

- `compiler` embeds or owns `bytecodeBuilder`; direct appends to `c.constants`, `c.code`, and `c.prototypes` are removed from `emitter.go` where a builder method covers the operation.
- `patchJump` mutates only instruction operand `b` at the given instruction index.
- `proto` calls existing `newProto` and preserves current bytecode representation.
- This plan does not require changing opcode numeric values or `Proto` exported behavior.

### IC-COMPILER-2: Private expression shape helpers

Create `/Users/mark/Desktop/ember/expression_shape.go` with these unexported names:

- `func expressionSingleCall(expr expression) (callExpression, bool)`
- `func expressionSingleVararg(expr expression) (term, bool)`
- `func expressionSingleTerm(expr expression) (term, bool)`
- `func termWithoutCastsAndGroups(value term) term`

Behavior guarantees:

- Existing `singleCallExpression` and `singleVarargExpression` in `emitter.go` are replaced by the new helpers.
- Helpers hide parser precedence nesting from emitter call expansion and tests.
- `termWithoutCastsAndGroups` removes type casts and parenthesized groups only when the term still represents one semantic expression.
- Tests that need to inspect call type arguments use helper functions in `parser_type_test.go`, not raw chains such as `terms[0].terms[0].left.first.first.first`.

### IC-CHECKER-1: Checker-facing typed syntax foothold

Modify `/Users/mark/Desktop/ember/checker.go` and tests with these unexported names:

- `type checkResult struct { program program }`
- `func checkSource(source string) (checkResult, error)`
- `func requireStrictMode(prog program) error`

Behavior guarantees:

- `Check` calls `checkSource` and returns only the error, preserving its public interface.
- `checkSource` parses source, requires strict mode, and returns the parsed program for package-internal tests.
- `parser_type_test.go` may use `checkSource` to validate typed syntax retention through the checker module instead of constructing `parser` directly.
- External tests in `strict_test.go` continue to validate only public `Check` behavior.

### IC-COMMANDS-1: Verification commands

All workers and verifiers run commands from `/Users/mark/Desktop/ember`. The approved commands are:

- `timeout 30s gofmt -w <owned-go-files>` after Go edits.
- `timeout 120s go test -vet=off -count=1 .` for focused root-package behavior.
- `timeout 180s scripts/check-lane root` for lane verification.
- `timeout 180s scripts/check-fast` for pre-review verification.
- `timeout 300s scripts/check` for final verification.

## File Ownership

| File | Owner task | Change type | Responsibility | Parallel safety notes |
| --- | --- | --- | --- | --- |
| `/Users/mark/Desktop/ember/docs/simplepower/plans/2026-07-04-architecture-deepening.md` | Coordinator plan writer | create | Authoritative implementation plan | Coordinator-owned; not edited by implementation workers unless user requests plan revision |
| `/Users/mark/Desktop/ember/table_ops.go` | Runtime semantics deepening | create | Private table access module | Exclusive to runtime task |
| `/Users/mark/Desktop/ember/raw_sequence.go` | Runtime semantics deepening | create | Private raw sequence module | Exclusive to runtime task |
| `/Users/mark/Desktop/ember/value.go` | Runtime semantics deepening | modify | Public `Table.Get`/`Set` delegate to `publicTableAccess`; raw table helpers remain private | Exclusive to runtime task |
| `/Users/mark/Desktop/ember/vm.go` | Runtime semantics deepening | modify | VM table opcode handlers use `runtimeTableAccess`; duplicated table helpers removed | Exclusive to runtime task |
| `/Users/mark/Desktop/ember/baselib.go` | Runtime semantics deepening | modify | Base table functions use `rawSequence` and table access helpers | Exclusive to runtime task |
| `/Users/mark/Desktop/ember/table_test.go` | Runtime semantics deepening | modify | Add/adjust behavior tests for table access and raw sequence behavior | Exclusive to runtime task |
| `/Users/mark/Desktop/ember/bytecode.go` | Compiler bytecode and expression deepening | modify | Add `bytecodeBuilder` and builder methods | Exclusive to compiler task |
| `/Users/mark/Desktop/ember/emitter.go` | Compiler bytecode and expression deepening | modify | Use `bytecodeBuilder` and expression shape helpers | Exclusive to compiler task |
| `/Users/mark/Desktop/ember/expression_shape.go` | Compiler bytecode and expression deepening | create | Private expression shape helper module | Exclusive to compiler task |
| `/Users/mark/Desktop/ember/compiler_test.go` | Compiler bytecode and expression deepening | modify | Add source-to-result regression coverage if emitter behavior changes need tests | Exclusive to compiler task |
| `/Users/mark/Desktop/ember/checker.go` | Checker foothold deepening | modify | Add `checkResult`, `checkSource`, and `requireStrictMode` | Exclusive to checker task |
| `/Users/mark/Desktop/ember/parser_type_test.go` | Checker foothold deepening | modify | Route typed syntax retention tests through `checkSource` and helper accessors | Exclusive to checker task |
| `/Users/mark/Desktop/ember/strict_test.go` | Checker foothold deepening | modify | Preserve public `Check` tests and add strict typed syntax public cases | Exclusive to checker task |

## Visual Aids

```text
Runtime task:
  value.go Table.Get/Set
          \                         vm.go opGet/opSet
           \                       /
            table_ops.go tableAccess
           /                       \
  baselib.go metatable helpers      function metamethod calls

Compiler task:
  emitter.go -> expression_shape.go for parser nesting
  emitter.go -> bytecode.go bytecodeBuilder for operands and jumps

Checker task:
  strict_test.go -> public Check
  parser_type_test.go -> checkSource -> parser retention
```

## Implementation Tasks

### Task 1: Runtime semantics deepening

**Goal:** Concentrate table access, function-valued metamethod handling, protected metatable lookup, and raw sequence behavior behind private runtime modules.

**Contract inputs:** IC-PUBLIC-1, IC-RUNTIME-1, IC-RUNTIME-2, IC-COMMANDS-1, approved design detail "root package stays flat and public behavior stays stable."

**Serialization required:** No.

**Write scope:** `/Users/mark/Desktop/ember/table_ops.go`, `/Users/mark/Desktop/ember/raw_sequence.go`, `/Users/mark/Desktop/ember/value.go`, `/Users/mark/Desktop/ember/vm.go`, `/Users/mark/Desktop/ember/baselib.go`, `/Users/mark/Desktop/ember/table_test.go`.

**Parallel:** Yes, compatible with "Compiler bytecode and expression deepening" and "Checker foothold deepening."

**Risk:** High, because table and call semantics affect VM execution, host-visible tables, base library functions, and metatable behavior.

**Model tier:** BEST, resolved model `gpt-5.5`, reasoning effort `high`.

**Worker role:** `sp-impl`.

**Outputs and file-level responsibilities:**

- `table_ops.go`: implement `tableAccess` exactly as specified in IC-RUNTIME-1.
- `raw_sequence.go`: implement `rawSequence` exactly as specified in IC-RUNTIME-2.
- `value.go`: make `Table.Get` and `Table.Set` delegate to `publicTableAccess`; keep raw storage helpers private and behavior-compatible.
- `vm.go`: replace `getTableValue`, `setTableValue`, `callIndexMetamethod`, and `callNewIndexMetamethod` usages with `runtimeTableAccess(globals)`; keep call and arithmetic helpers behavior-compatible.
- `baselib.go`: use `runtimeTableAccess(globals)` for `protectedMetatable` and metatable-sensitive base functions; use `rawSequence` for table library sequence operations and clearing.
- `table_test.go`: add behavior tests proving public `Table.Get`/`Set` table-valued metatable behavior still works, runtime function-valued `__index`/`__newindex` still works through scripts, and `table.clear`/`table.insert`/`table.remove` preserve results.

**Implementation steps:**

1. Add `table_ops.go` with the names and behavior from IC-RUNTIME-1. Move the recursive table-valued `__index`/`__newindex` chain logic out of `value.go` into `tableAccess.get` and `tableAccess.set`. Preserve the existing cycle errors `table: cyclic __index chain` and `table: cyclic __newindex chain`.
2. Update `Table.Get` and `Table.Set` in `value.go` to call `publicTableAccess().get(t, key)` and `publicTableAccess().set(t, key, value)`.
3. Update VM table field/index op handlers in `vm.go` to call `runtimeTableAccess(globals).get(...)` and `runtimeTableAccess(globals).set(...)`. Remove or leave unused no functions named `getTableValue`, `setTableValue`, `callIndexMetamethod`, or `callNewIndexMetamethod`.
4. Add `raw_sequence.go` with the names and behavior from IC-RUNTIME-2. Keep all methods small and label-aware for errors.
5. Update `baselib.go` table functions to create `seq := newRawSequence("<function name>", table)` and use `seq.len`, `seq.get`, `seq.set`, `seq.values`, `seq.insert`, `seq.remove`, `seq.clear`, and `seq.writeValues` where applicable.
6. Update `baseSetMetatable`, `baseGetMetatable`, and `stringValue` to use the shared protected-metatable and call helpers where that preserves behavior.
7. Add focused tests to `table_test.go`; prefer source-to-result tests through `Compile` and `RunWithGlobals` for script-visible behavior and direct `Table` tests for public host behavior.
8. Run `timeout 30s gofmt -w table_ops.go raw_sequence.go value.go vm.go baselib.go table_test.go`.

**Verification commands:**

- `timeout 120s go test -vet=off -count=1 .` should pass root-package tests.
- `timeout 180s scripts/check-lane root` should pass the root lane.

**Completion report requirements:** Report changed files, whether old VM table helper functions were removed, tests added, commands run with results, and any behavior that could not preserve current errors exactly.

### Task 2: Compiler bytecode and expression deepening

**Goal:** Make bytecode construction and expression shape classification private deep modules so emitter code stops owning operand conventions and parser nesting.

**Contract inputs:** IC-PUBLIC-1, IC-COMPILER-1, IC-COMPILER-2, IC-COMMANDS-1, approved design detail "no opcode numeric value or public `Proto` behavior changes."

**Serialization required:** No.

**Write scope:** `/Users/mark/Desktop/ember/bytecode.go`, `/Users/mark/Desktop/ember/emitter.go`, `/Users/mark/Desktop/ember/expression_shape.go`, `/Users/mark/Desktop/ember/compiler_test.go`.

**Parallel:** Yes, compatible with "Runtime semantics deepening" and "Checker foothold deepening."

**Risk:** High, because emitter changes can silently alter bytecode behavior across all source-to-result tests.

**Model tier:** BEST, resolved model `gpt-5.5`, reasoning effort `high`.

**Worker role:** `sp-impl`.

**Outputs and file-level responsibilities:**

- `bytecode.go`: add `bytecodeBuilder` and builder methods from IC-COMPILER-1.
- `expression_shape.go`: add helper functions from IC-COMPILER-2.
- `emitter.go`: embed or own `bytecodeBuilder`, replace direct appends where covered, and replace expression shape crawling helpers.
- `compiler_test.go`: add source-to-result regression tests only if the refactor touches behavior paths without existing coverage.

**Implementation steps:**

1. Add `bytecodeBuilder` and methods in `bytecode.go`. Keep `instruction`, `opcode`, and `Proto` representation intact.
2. Change `compiler` in `emitter.go` to own `bytecodeBuilder`; update `compileProgram`, closure emission, return emission, jump emission, and constant loading to use builder methods from IC-COMPILER-1.
3. Keep direct `instruction{...}` construction only for opcodes not yet covered by the builder when adding a typed builder method would not reduce duplication. If a direct construction remains, it must be local and obvious, not repeated across multiple functions.
4. Add `expression_shape.go` helpers from IC-COMPILER-2.
5. Replace `singleCallExpression` and `singleVarargExpression` in `emitter.go` with `expressionSingleCall` and `expressionSingleVararg`; remove the old helper names.
6. Adjust emitter call expansion, vararg expansion, and return emission to use the new helpers without changing observable multiple-return behavior.
7. Add or keep source-to-result tests in `compiler_test.go` for final call expansion, final vararg expansion, parenthesized call-as-single-result, and cast-erased calls if existing tests do not already cover the changed shape.
8. Run `timeout 30s gofmt -w bytecode.go emitter.go expression_shape.go compiler_test.go`.

**Verification commands:**

- `timeout 120s go test -vet=off -count=1 .` should pass root-package tests.
- `timeout 180s scripts/check-lane root` should pass the root lane.

**Completion report requirements:** Report changed files, remaining direct instruction constructions and why they remain, tests added or reused, commands run with results, and any bytecode behavior risk.

### Task 3: Checker foothold deepening

**Goal:** Route typed syntax retention checks through the checker module and reduce tests that crawl parser implementation fields directly.

**Contract inputs:** IC-PUBLIC-1, IC-CHECKER-1, IC-COMMANDS-1, approved design detail "typed analysis remains a foothold; `Check` still returns only an error."

**Serialization required:** No.

**Write scope:** `/Users/mark/Desktop/ember/checker.go`, `/Users/mark/Desktop/ember/parser_type_test.go`, `/Users/mark/Desktop/ember/strict_test.go`.

**Parallel:** Yes, compatible with "Runtime semantics deepening" and "Compiler bytecode and expression deepening."

**Risk:** Medium, because this changes test seams and checker internals but should not alter runtime behavior.

**Model tier:** NORMAL, resolved model `gpt-5.4`, reasoning effort `high`.

**Worker role:** `sp-impl`.

**Outputs and file-level responsibilities:**

- `checker.go`: add `checkResult`, `checkSource`, and `requireStrictMode`; make `Check` call `checkSource`.
- `parser_type_test.go`: parse typed syntax through `checkSource` with `--!strict`; add small helper functions for commonly inspected shapes, especially call type arguments.
- `strict_test.go`: keep existing public `Check` tests and add public tests for strict typed syntax accepted by `Check`.

**Implementation steps:**

1. Update `checker.go` with `checkResult`, `checkSource`, and `requireStrictMode` from IC-CHECKER-1.
2. Preserve exact error text `check: --!strict directive required` for missing or non-strict directives.
3. Update each `parser_type_test.go` source fixture to include `--!strict` and call `checkSource` instead of constructing `parser` directly.
4. Add package-test helper functions in `parser_type_test.go` for repeated navigation, such as retrieving the first local statement, first type alias, and first returned call term. Helpers must hide nested precedence paths from individual tests.
5. Add external `strict_test.go` cases proving `Check` accepts strict source containing typed local annotations, type aliases, function annotations, casts, and call type arguments.
6. Run `timeout 30s gofmt -w checker.go parser_type_test.go strict_test.go`.

**Verification commands:**

- `timeout 120s go test -vet=off -count=1 .` should pass root-package tests.
- `timeout 180s scripts/check-lane root` should pass the root lane.

**Completion report requirements:** Report changed files, parser-internal direct access that remains and why, public `Check` cases added, commands run with results, and any checker-depth risk left for future analyzer work.

## Model Allocation

| Stage | Role | Model tier | Resolved model | Reasoning effort | Reason |
| --- | --- | --- | --- | --- | --- |
| Plan review | REVIEW-tier plan reviewer | REVIEW | `gpt-5.5` | `xhigh` | Required by Simple Power for plan completeness and aggregate parallel readiness |
| Implementation | Runtime semantics deepening `sp-impl` | BEST | `gpt-5.5` | `high` | Broad behavior-shaping runtime work touching VM, base library, and public table behavior |
| Implementation | Compiler bytecode and expression deepening `sp-impl` | BEST | `gpt-5.5` | `high` | Broad behavior-shaping compiler work where bytecode regressions are hard to spot locally |
| Implementation | Checker foothold deepening `sp-impl` | NORMAL | `gpt-5.4` | `high` | Localized checker and test seam work with moderate risk |
| Quick verification | FAST-tier quick verifier | FAST | `gpt-5.3-codex-spark` | `high` | Runs concrete lint/build/test commands and may fix only tiny typo-level issues |
| Final review/fix | REVIEW-tier review+fix agent | REVIEW | `gpt-5.5` | `xhigh` | Required whole-implementation review and fixes before final verification |

Resolved tier sources for this plan: no explicit user model override; `/Users/mark/Desktop/ember/AGENTS.md` contains no quoted Simple Power model assignments; no `SIMPLEPOWER_` process environment variables were present in the resolution command output; built-in defaults are used.

## Plan Review

Self-review checklist status:

- Design Summary: captures the approved architecture report, constraints, success criteria, and key decisions.
- Interface Contract: concrete public APIs, private filenames, data shapes, behavior guarantees, and command contracts are listed before File Ownership.
- File ownership: every created or modified file is assigned to exactly one task; parallel implementation tasks do not collide.
- Task allocation: every implementation task references Contract inputs and includes Serialization required with a concrete value.
- Aggregate parallel readiness: all three implementation tasks have non-overlapping write scopes and can dispatch together from the Interface Contract.
- Visual aids: present as inline text and consistent with written sections.
- Model allocation: FAST/NORMAL/BEST/REVIEW choices match risk; resolution precedence and root `AGENTS.md` lookup rule are explicit; plan reviewer and review+fix use REVIEW; quick verifier uses FAST.
- Review allocation: one REVIEW-tier review+fix agent is required after quick verification.
- Commit policy: exactly three future coordinator checkpoints are defined; non-coordinator roles must not commit; scratch refs are local review anchors only.
- Scratch refs: run id, namespace, creation, revised-plan diff handoff, cleanup, blocker preservation, and final cleanup check are defined below.
- Verification: quick and final commands are concrete and timeout-wrapped.
- Approved path enforcement: this plan does not authorize route changes, skipped checks, reduced deliverables, placeholder implementations, or docs-only substitutes.

Before first plan review, the coordinator creates `refs/simplepower/scratch/<run-id>/plan-review/before` for this saved plan file using the temporary-index pattern. The run id format is `YYYYMMDD-HHMMSS-<short-head>`.

Plan reviewer dispatch:

- Reviewer role: REVIEW-tier plan document reviewer.
- Resolved model: `gpt-5.5`; reasoning effort: `xhigh`.
- Reviewer prompt source: `/Users/mark/.codex/plugins/cache/simplepower-source-local/simplepower/1.0.0/skills/writing-plans/plan-document-reviewer-prompt.md`.
- Plan path: `/Users/mark/Desktop/ember/docs/simplepower/plans/2026-07-04-architecture-deepening.md`.
- Approved brainstorming design context: architecture report generated at `/var/folders/v1/zhfpvfk176l2jg35pl9tx4cm0000gn/T/architecture-review-20260704-140838.html`, with top recommendation table/metamethod locality plus bytecode depth, expression shape locality, raw sequence locality, and checker foothold depth.
- Reviewer constraints: perform the assigned review directly in the current worker; do not run Codex CLI; do not spawn subagents; do not invoke Simple Power skills; do not restart execution; do not reroute the workflow.

If the reviewer reports issues, the coordinator fixes this plan, reruns the focused self-review checks for changed categories, creates `refs/simplepower/scratch/<run-id>/plan-review/after-<n>`, and sends the same reviewer this diff command:

```bash
git diff refs/simplepower/scratch/<run-id>/plan-review/before refs/simplepower/scratch/<run-id>/plan-review/after-<n> -- /Users/mark/Desktop/ember/docs/simplepower/plans/2026-07-04-architecture-deepening.md
```

For later revisions, compare the previous `after-<n>` to the new `after-<n+1>`. If a needed scratch ref is missing, stop the review loop before relying on the missing anchor. Close the reviewer only after approval, unrecoverable interruption, or explicit user direction.

After plan reviewer approval, ask the user for one combined approval covering the reviewed plan, model/task allocation, and immediate current-session execution. The accepted plan checkpoint happens only after that combined approval.

## Quick Verification

Before dispatching the quick verifier, the coordinator creates `refs/simplepower/scratch/<run-id>/quick-verifier/before` for these approved implementation files:

```bash
/Users/mark/Desktop/ember/table_ops.go
/Users/mark/Desktop/ember/raw_sequence.go
/Users/mark/Desktop/ember/value.go
/Users/mark/Desktop/ember/vm.go
/Users/mark/Desktop/ember/baselib.go
/Users/mark/Desktop/ember/table_test.go
/Users/mark/Desktop/ember/bytecode.go
/Users/mark/Desktop/ember/emitter.go
/Users/mark/Desktop/ember/expression_shape.go
/Users/mark/Desktop/ember/compiler_test.go
/Users/mark/Desktop/ember/checker.go
/Users/mark/Desktop/ember/parser_type_test.go
/Users/mark/Desktop/ember/strict_test.go
```

Quick verifier role: FAST-tier quick verifier, resolved model `gpt-5.3-codex-spark`, reasoning effort `high`.

Quick verifier commands from `/Users/mark/Desktop/ember`:

- `timeout 30s gofmt -w table_ops.go raw_sequence.go value.go vm.go baselib.go table_test.go bytecode.go emitter.go expression_shape.go compiler_test.go checker.go parser_type_test.go strict_test.go`
- `timeout 120s go test -vet=off -count=1 .`
- `timeout 180s scripts/check-lane root`
- `timeout 180s scripts/check-fast`

Expected result: all commands exit 0. Failure means the implementation is not coherent enough for the quick-verified implementation checkpoint.

Quick verifier scope: may fix only tiny typo-level errors discovered while running these checks. Any behavior change, structural edit, test rewrite, public interface change, or unclear issue must be reported to the coordinator instead of fixed. If tiny fixes happen, the coordinator creates `refs/simplepower/scratch/<run-id>/quick-verifier/after` and inspects or hands off:

```bash
git diff refs/simplepower/scratch/<run-id>/quick-verifier/before refs/simplepower/scratch/<run-id>/quick-verifier/after -- /Users/mark/Desktop/ember/table_ops.go /Users/mark/Desktop/ember/raw_sequence.go /Users/mark/Desktop/ember/value.go /Users/mark/Desktop/ember/vm.go /Users/mark/Desktop/ember/baselib.go /Users/mark/Desktop/ember/table_test.go /Users/mark/Desktop/ember/bytecode.go /Users/mark/Desktop/ember/emitter.go /Users/mark/Desktop/ember/expression_shape.go /Users/mark/Desktop/ember/compiler_test.go /Users/mark/Desktop/ember/checker.go /Users/mark/Desktop/ember/parser_type_test.go /Users/mark/Desktop/ember/strict_test.go
```

If no file changes happen during quick verification, omit the `quick-verifier/after` ref. After the quick-verified implementation checkpoint succeeds, delete this run's `quick-verifier` scratch refs. If the checkpoint fails or the workflow stops before the checkpoint, preserve refs and report the manual cleanup command from Commit Checkpoints.

## Final Review And Fix

After the quick-verified implementation checkpoint, dispatch one REVIEW-tier review+fix agent. Resolved model: `gpt-5.5`; reasoning effort: `xhigh`.

Before dispatch, the coordinator creates `refs/simplepower/scratch/<run-id>/review-fix/before` for the same approved implementation file list from Quick Verification.

Review+fix agent responsibilities:

- Review the whole implementation against this accepted plan, File Ownership, Interface Contract, approved path enforcement, aggregate parallel dispatch semantics, and verification requirements.
- Fix issues directly only within approved implementation files.
- Do not commit.
- Perform the assigned review and fixes directly in the current worker.
- Do not run Codex CLI.
- Do not spawn subagents.
- Do not invoke Simple Power skills.
- Do not restart execution.
- Do not reroute the workflow.
- Report changed files, commands run, results, remaining risks, and unresolved deviations requiring user approval.

If review+fix edits files, the coordinator creates `refs/simplepower/scratch/<run-id>/review-fix/after` after those edits and before final verification, then inspects or hands off:

```bash
git diff refs/simplepower/scratch/<run-id>/review-fix/before refs/simplepower/scratch/<run-id>/review-fix/after -- /Users/mark/Desktop/ember/table_ops.go /Users/mark/Desktop/ember/raw_sequence.go /Users/mark/Desktop/ember/value.go /Users/mark/Desktop/ember/vm.go /Users/mark/Desktop/ember/baselib.go /Users/mark/Desktop/ember/table_test.go /Users/mark/Desktop/ember/bytecode.go /Users/mark/Desktop/ember/emitter.go /Users/mark/Desktop/ember/expression_shape.go /Users/mark/Desktop/ember/compiler_test.go /Users/mark/Desktop/ember/checker.go /Users/mark/Desktop/ember/parser_type_test.go /Users/mark/Desktop/ember/strict_test.go
```

If no file changes happen during review+fix, omit the `review-fix/after` ref. After the final checkpoint succeeds, delete this run's `review-fix` scratch refs. If the checkpoint fails or the workflow stops before the checkpoint, preserve refs and report the manual cleanup command from Commit Checkpoints.

## Commit Checkpoints

Exactly three future coordinator commit checkpoints are authorized:

1. Accepted plan checkpoint: after the user gives combined approval for the reviewed plan, model/task allocation, and immediate current-session execution, and before invoking `simplepower:subagent-driven-development`.
2. Quick-verified implementation checkpoint: after all `sp-impl` file edits complete and the quick verifier passes.
3. Final checkpoint: after the REVIEW-tier review+fix agent completes and final verification passes.

Workers, plan reviewers, quick verifiers, review+fix agents, and individual tasks must not commit. No worker-owned commits and no per-task commits are allowed.

Scratch refs are coordinator-owned local review diff anchors only. They live under `refs/simplepower/scratch/<run-id>/`, are not branches, are not accepted checkpoint commits, are not pushed, merged, or rebased, and must not change the exactly-three-checkpoint policy.

Scratch-ref creation pattern:

```bash
SP_RUN_ID="${SP_RUN_ID:-$(date -u +%Y%m%d-%H%M%S)-$(git rev-parse --short HEAD)}"
SP_SCRATCH_PREFIX="refs/simplepower/scratch/$SP_RUN_ID"
SP_REF="$SP_SCRATCH_PREFIX/<phase>/<label>"
SP_TMP_INDEX="$(mktemp)"
GIT_INDEX_FILE="$SP_TMP_INDEX" git read-tree HEAD
GIT_INDEX_FILE="$SP_TMP_INDEX" git add -- <approved-files>
SP_TREE="$(GIT_INDEX_FILE="$SP_TMP_INDEX" git write-tree)"
SP_COMMIT="$(printf '%s\n' "simplepower scratch $SP_RUN_ID <phase>/<label>" | git commit-tree "$SP_TREE" -p HEAD)"
git update-ref "$SP_REF" "$SP_COMMIT"
rm -f "$SP_TMP_INDEX"
```

Successful checkpoint cleanup:

```bash
git for-each-ref --format='%(refname)' "refs/simplepower/scratch/<run-id>/<phase>" | while read -r ref; do git update-ref -d "$ref"; done
```

If the workflow stops because of user direction, a blocker, or a failed checkpoint commit, preserve remaining scratch refs and report:

```bash
git for-each-ref --format='%(refname)' "refs/simplepower/scratch/<run-id>" | while read -r ref; do git update-ref -d "$ref"; done
```

## Current-Session Auto-Dispatch

The saved plan is the execution artifact. Do not write a project-local implementation JSON artifact. Do not run routing heuristics or offer alternate execution routes.

After the plan reviewer approves, ask the user for one combined approval that covers:

- The reviewed plan
- The model/task allocation
- Immediate current-session execution

If the user requests changes, update this plan, rerun focused self-review checks for changed categories, create the next `plan-review/after-<n>` scratch ref, and send the revised plan back to the same reviewer with the concrete scratch-ref diff command when review approval must be refreshed. Do not create the accepted plan checkpoint until the user gives combined approval.

After combined approval, the coordinator creates the accepted plan checkpoint commit, deletes successful `plan-review` scratch refs, then immediately invokes `simplepower:subagent-driven-development` in the current session with this instruction:

```text
Execute `/Users/mark/Desktop/ember/docs/simplepower/plans/2026-07-04-architecture-deepening.md` with aggregate parallel implementation from the approved Interface Contract. Use the approved FAST/NORMAL/BEST/REVIEW model allocation. Dispatch all non-conflicting `sp-impl` file-edit workers whose coordination needs are satisfied by their Contract inputs, run the quick FAST-tier verifier with lint/build/tests and timeouts after all workers finish, commit the quick-verified implementation, then run one REVIEW-tier review+fix agent, final verification, and final commit.
```

## Verification

Final verification runs after the REVIEW-tier review+fix agent completes and before the final checkpoint.

Final commands from `/Users/mark/Desktop/ember`:

| Command | When to run | Expected result | Failure means |
| --- | --- | --- | --- |
| `timeout 30s gofmt -w table_ops.go raw_sequence.go value.go vm.go baselib.go table_test.go bytecode.go emitter.go expression_shape.go compiler_test.go checker.go parser_type_test.go strict_test.go` | After review+fix edits and before tests | Command exits 0 and files are formatted | Go files may contain formatting or syntax issues |
| `timeout 120s go test -vet=off -count=1 .` | After gofmt | Command exits 0 | Root package behavior or tests regressed |
| `timeout 180s scripts/check-lane root` | After focused tests | Command exits 0 | Root lane is not coherent |
| `timeout 180s scripts/check-fast` | After lane check | Command exits 0 | Repository fast check found a regression |
| `timeout 300s scripts/check` | Final proof before final checkpoint | Command exits 0 | Full standard repository check failed |

The coordinator performs the final checkpoint only after the REVIEW-tier review+fix agent has completed and all final commands pass.

Final reporting must include a cleanup check:

```bash
git for-each-ref --format='%(refname)' "refs/simplepower/scratch/<run-id>"
```

If the final checkpoint succeeds, no scratch refs for that run should remain after phase cleanup. If the workflow stopped because of user direction, a blocker, or a failed checkpoint commit, preserve remaining scratch refs and report the manual cleanup command from Commit Checkpoints.

Approved path enforcement: this plan is authoritative after combined approval. It does not authorize backup routes, scope reduction, docs-only substitutes, stub substitutes, placeholder implementations, skipped verification, skipped review, execution-route changes, or public interface expansion without fresh explicit user approval at the moment the deviation is needed.
