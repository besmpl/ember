# Plan: MVP Type System

## Goal

Build a high-quality MVP typed-analysis system that makes Ember useful for
strict Luau-shaped scripts without coupling type checking to runtime execution.
The MVP should be small enough to complete in roughly 20 vertical slices, but
shaped so later Luau features can deepen the same modules instead of replacing
them.

The target is an analyzer that can:

- parse, bind, lower, check, and summarize common annotated source;
- report source-ranged diagnostics for locals, functions, tables, calls,
  assignments, operators, and basic flow refinement;
- preserve gradual modes: `--!strict`, `--!nonstrict`, and `--!nocheck`;
- emit stable module summaries that other modules and tools can consume;
- keep all runtime annotations erased from `Compile` and `Run`.

## Current Baseline

Ember already has a typed-analysis foothold:

- public `Analyzer.Check(ctx, Source)` and convenience `Check(source string)`
  entry points;
- `CheckResult`, `Diagnostic`, `ModuleSummary`, `TypeSummary`, and
  `ToolingFacts` public artifacts;
- parsed type syntax with source ranges for aliases, annotations, table types,
  function types, packs, generics, casts, `typeof`, singleton types, and access
  modifiers;
- a binder and many source-level strict-mode checks;
- an internal `typeStore` with opaque `TypeRef` and `PackRef` handles;
- module return summaries for selected annotated and inferred values.

This plan does not restart that work. It turns the seeded checker into a
cohesive MVP by moving scattered behavior behind explicit internal phases and
filling the most important compatibility gaps.

## Scope

In:

- strict-mode correctness for common scripts;
- nonstrict and nocheck mode policy;
- builtin primitive, table, function, generic, union, nilable, singleton, and
  pack facts needed by the MVP;
- local variables, parameters, returns, table fields/indexers, calls, casts,
  arithmetic/comparison/logical operators, and basic type guards;
- module summaries for returned values and exported type aliases;
- host-provided type environment seam, initially for base globals and foreign
  values;
- source-ranged, explainable diagnostics;
- tests through `Analyzer.Check` first, with focused internal tests only for
  store and relation invariants that cannot be observed yet.

Out:

- full semantic subtyping and full normalization;
- user-defined type functions;
- complete Luau standard-library type coverage;
- require graph analysis beyond a small module-summary adapter;
- editor protocol integration;
- optimizer use of type facts;
- Hearth-specific type facts in the root package;
- runtime enforcement of annotations.

## Design

The analyzer should remain a deep module. Most callers should only need:

```go
analyzer := ember.NewAnalyzer(options...)
result, err := analyzer.Check(ctx, ember.Source{Name: name, Text: text})
```

The public interface returns diagnostics, summaries, and optional tooling facts.
It must not expose syntax nodes, solver constraints, mutable scopes,
normalization state, or raw type-store nodes.

Internally, the MVP should converge on this pipeline:

```text
Source
  -> Parse
  -> Bind
  -> Lower Types
  -> Infer And Check
  -> Refine Flow
  -> Build Artifact
```

This pipeline is an ownership model, not a requirement to split packages early.
The root package can keep private helpers until implementation pressure proves a
package split.

### Module Interfaces

Keep these seams private until a second adapter or caller proves they should be
public:

- `typeStore`: owns `TypeRef`, `PackRef`, construction, interning, display,
  summary conversion, and relation caches.
- `typeEnv`: supplies builtin and host facts to analysis without hard-coding
  Hearth or host globals into the checker.
- `scopeGraph`: owns value symbols, type symbols, aliases, generic binders,
  shadowing, and name resolution.
- `relationEngine`: answers assignability, equality, call compatibility,
  table access, union membership, and pack adjustment questions.
- `flowState`: holds temporary refinements for locals and stable table fields.
- `artifactBuilder`: converts internal facts into `CheckResult` without leaking
  solver details.

The deletion test should guide each seam: if deleting a private module only
moves logic into expression visitors, it is earning its keep. If deleting it
removes complexity entirely, it was likely a pass-through.

## Non-Negotiable Invariants

- Typed analysis remains erased from runtime execution.
- `Compile` and `Run` do not require a successful typed artifact.
- Parser output is syntax, not semantic type facts.
- Public summaries are stable data, not pointers into analyzer internals.
- Every type and pack reference belongs to exactly one store.
- Diagnostics point to source evidence and use stable codes.
- `nocheck` produces no type diagnostics when parsing succeeds.
- `nonstrict` favors useful, high-confidence diagnostics over proof.
- `strict` preserves uncertainty long enough to catch incompatible use.
- Host knowledge enters through an adapter seam.

## Phase Gates

Use these gates to keep the work shippable.

### Phase 1: Core Shape

Create the internal vocabulary and relation core that every later slice can
reuse.

Exit gate:

- analyzer phases have clear helper boundaries;
- builtin type facts lower through `typeStore`;
- diagnostics have stable codes, ranges, and tests;
- current seeded behavior still passes.

### Phase 2: Values, Tables, And Functions

Make the MVP useful for ordinary strict scripts with annotations, inference,
calls, and table shapes.

Exit gate:

- common local/function/table programs check through `Analyzer.Check`;
- incompatible assignment, call, return, and table access diagnostics are
  source-ranged;
- module summaries describe exported aliases and returned values.

### Phase 3: Gradual Typing And Flow

Add mode policy, uncertainty, simple narrowing, and safer joins so strict code
is precise without making nonstrict code noisy.

Exit gate:

- strict/nonstrict/nocheck behavior is explicit and tested;
- nil, singleton, boolean, and `type(x)` refinements work for locals and stable
  fields;
- branch and loop joins do not mutate base facts incorrectly.

### Phase 4: Module And Host MVP

Prove the seam for external type knowledge without adding Hearth to the root
runtime.

Exit gate:

- base globals and host globals enter through `typeEnv`;
- a minimal module-summary adapter can type a required module from a summary;
- public artifacts are deterministic and documented.

## Slices

Each slice should follow the same loop:

1. Add one behavior test through `Analyzer.Check` unless the behavior is only
   observable through a private invariant.
2. Implement the smallest cohesive internal change.
3. Run the focused test, then `scripts/check-lane root`.
4. Update docs only when the public surface or plan status changes.

### Phase 1: Core Shape

1. **Phase audit and fixture cleanup**
   - Behavior: existing strict, nonstrict, nocheck, type syntax, summary, and
     runtime-erasure tests still describe the intended MVP baseline.
   - Change: group checker tests by behavior and add missing regression names
     for the already-seeded facts before moving internals.
   - Checks: `go test -count=1 ./... -run 'Test(Check|Analyzer|TypeStore)'`.

2. **Builtin type facts through one store path**
   - Behavior: `number`, `string`, `boolean`, `nil`, `table`, `function`,
     `thread`, `userdata`, `any`, `unknown`, and `never` lower to stable
     semantic facts and summaries.
   - Change: route builtin type names through `typeStore` constructors instead
     of treating them as ordinary names everywhere.
   - Checks: focused builtin annotation and exported-alias tests.

3. **Stable diagnostic catalog**
   - Behavior: common MVP failures return stable codes, messages, and precise
     source ranges.
   - Change: centralize diagnostic construction enough to avoid ad hoc message
     drift while keeping call sites readable.
   - Checks: tests assert codes and source slices, not full prose unless the
     wording is important.

4. **Binder-owned symbols**
   - Behavior: locals, parameters, aliases, generic binders, and shadowing are
     resolved consistently before type checking.
   - Change: make analyzer checks consume binder identities where practical
     instead of raw names.
   - Checks: shadowing, duplicate alias, unknown name, and parameter annotation
     tests.

5. **Lowered annotation map**
   - Behavior: every annotation used by the checker is lowered once and reused
     by later checks.
   - Change: build a private map from syntax sites or symbols to `TypeRef` and
     `PackRef`, preserving source evidence for failures.
   - Checks: alias, nilable, union, generic alias, table, and function
     annotation tests.

### Phase 2: Values, Tables, And Functions

6. **Assignable relation MVP**
   - Behavior: scalar, nilable, union, `any`, `unknown`, and `never`
     assignments follow mode-aware assignability.
   - Change: introduce a private relation helper used by local binding,
     reassignment, returns, and table writes.
   - Checks: compatible and incompatible assignments with exact diagnostic
     ranges.

7. **Expression type facts**
   - Behavior: literals, locals, parenthesized expressions, casts, table
     literals, and function expressions produce reusable type facts.
   - Change: separate expression fact computation from statement checking so
     operators, calls, and summaries share the same source of truth.
   - Checks: inferred local and return summary tests.

8. **Function call relation**
   - Behavior: annotated function values check argument packs and produce return
     facts, including table facts and final-call value-list adjustment.
   - Change: route direct calls, variable calls, table-field calls, and method
     calls through one call relation.
   - Progress: direct local calls and annotated table-field calls check
     argument, method self-argument, and return facts through the shared call
     relation lookup.
   - Checks: argument mismatch, return type, method self, and multiple-return tests.

9. **Return checking**
   - Behavior: local and anonymous functions validate annotated return packs,
     including no return, single return, and multiple returns.
   - Change: track expected return packs in function-checking context.
   - Progress: local function declarations, function expressions checked
     against local function type annotations, and standalone annotated function
     expressions validate parameter and return annotations through the shared
     function body context. Local variables bound to annotated function
     expressions preserve return facts for later calls. Function bodies with
     annotated non-nil returns now report implicit nil when control reaches the
     end without a return statement. Concrete parenthesized return packs such
     as `() -> (number, string)` check returned head values, including missing
     values as nil. Conservative definite-return checks avoid implicit nil
     diagnostics when all `if` branches return and report fallthrough when only
     some branches return.
   - Checks: return mismatch diagnostics point at returned expressions.

10. **Table shape relation**
    - Behavior: named properties, missing required fields, nilable fields,
      numeric/string indexers, and table-to-table assignment check consistently,
      including table facts produced by function-call results.
    - Change: make table literal checking and table assignment use the same
      table relation helper.
    - Progress: known-empty inferred table literals are preserved as table
      facts, so assigning an inferred `{}` local to a shape with required
      fields reports the same missing-property diagnostic as a direct literal.
      Required indexers also reject known-empty inferred table sources instead
      of silently treating absent indexer facts as compatible. Direct table
      literal named fields now check against expected string-indexer value
      types when no named property annotation exists for that field. Computed
      string-key literal facts can satisfy required named fields, with value
      mismatches reported through the table relation. Dot-property reads and
      writes on tables with string indexers now use the same indexer value
      relation as bracket access.
    - Checks: literal, read, write, reassignment, indexer, and missing-field
      tests.

11. **Access modifiers**
    - Behavior: `read` properties/indexers reject writes; `write` properties
      and indexers reject reads.
    - Change: store access mode on table facts and enforce it at read/write
      relation sites.
    - Progress: named properties and string indexers enforce read-only writes
      and write-only reads, including bracket access and dot-property access
      through a string indexer. Write-only function properties also reject
      direct calls and method calls because call targets read the callable
      field.
    - Checks: source ranges point at the illegal access.

12. **Operator facts**
    - Behavior: arithmetic, concat, comparison, equality, logical `and`/`or`,
      unary `not`, unary `-`, and length checks use operand facts and produce
      useful result facts.
    - Change: route operator checking through relation helpers rather than
      expression-specific branches.
    - Progress: arithmetic, concat, ordered comparison, equality, unary
      `not`, unary `-`, and length produce checked result facts. Unary length
      now reports operand mismatches for non-string, non-table values.
      Deterministic logical `and`/`or` expressions now produce concrete result
      facts when literal truthiness determines the selected value.
    - Checks: both success summaries and operand mismatch diagnostics.

### Phase 3: Gradual Typing And Flow

13. **Mode policy table**
    - Behavior: the same source can produce different strict, nonstrict, and
      nocheck results where Luau gradual typing expects it.
    - Change: make mode policy explicit near relation and artifact building,
      not scattered through parser or binder code.
    - Progress: `Analyzer.Check` and the convenience `Check` entry point now
      accept explicit `--!strict`, `--!nonstrict`, and `--!nocheck` modes.
      `nocheck` preserves parsing and summaries while suppressing typed
      diagnostics. A private mode-policy helper now owns analyzer decisions
      for no-analysis and strict-only uncertainty diagnostics; nonstrict
      suppresses unknown-name uncertainty while still reporting concrete
      annotation mismatches.
    - Checks: paired strict/nonstrict/nocheck tests for unknown locals,
      annotation mismatches, and exported summaries.

14. **Nil and truthiness refinement**
    - Behavior: `if x then`, `if not x then`, `x == nil`, and `x ~= nil`
      refine nilable locals and stable table fields inside branches.
    - Change: isolate `flowState` so refinements do not mutate base symbol
      facts.
    - Progress: nilable locals and stable table fields refine through truthy,
      falsey, `not`, `== nil`, and `~= nil` branch conditions; assert guards
      preserve truthy refinements after the call. Stable string-literal bracket
      access, such as `player["name"]`, now shares the same named-field read and
      refinement path as dot access.
    - Checks: true branch, false branch, else branch, and after-merge tests.

15. **Singleton and `type()` refinement**
    - Behavior: string/boolean singleton comparisons and `type(x) == "kind"`
      narrow simple unions for locals and stable table fields.
    - Change: represent predicate evidence and branch-specific facts with the
      same flow helper used for nil checks.
    - Progress: string and boolean singleton comparisons narrow locals and
      stable table fields in matching branches. `type(x) == "kind"` and
      `type(x) ~= "kind"` narrow simple union locals and stable table fields
      through true, false, negated, grouped, and assert-guarded conditions.
    - Checks: matching branch narrows; nonmatching branch excludes when the
      relation is precise enough.

16. **Loop refinement and joins**
    - Behavior: `while` and `repeat` bodies see guard refinements, and facts
      after loops remain conservative.
    - Change: add explicit join rules for loop back edges and exits.
    - Progress: `while` bodies see guard refinements, `repeat` exits preserve
      until-condition refinements without leaking body locals, and compatible
      assignment facts for existing locals and tracked table fields join after
      `if` statements.
    - Checks: while-body, repeat-exit, and assignment-inside-loop tests.

17. **Generic function MVP**
    - Behavior: simple generic functions infer and accept explicit type
      arguments for scalar/table parameters and returns.
    - Change: implement private instantiation for generic function binders
      without exposing type variables publicly.
    - Progress: local generic functions and generic function type aliases infer
      scalar call-site substitutions, enforce repeated generic argument
      consistency, accept explicit type arguments, and preserve generic return
      facts in calls and exported summaries.
    - Checks: inferred identity, explicit type args, mismatch, and summary
      tests.

18. **Type pack MVP**
    - Behavior: variadic parameters, variadic return packs, generic packs, and
      value-list adjustment work for common function calls and returns.
    - Change: move pack adjustment into `typeStore`/relation helpers instead of
      local call-site branches.
    - Progress: typed variadic function arguments, simple generic variadic
      `...: T` consistency, exported generic packs, exported variadic return
      packs, final-call expansion, truncation, and nil-fill behavior are covered
      through analyzer and runtime-facing tests.
    - Checks: `...: T`, `U...`, final-call expansion, truncation, and nil-fill
      tests.

### Phase 4: Module And Host MVP

19. **Module summary import seam**
    - Behavior: one source can consume another source's exported aliases and
      returned value facts through a summary adapter.
    - Change: add an analyzer option for a private module-summary resolver;
      keep require/runtime module loading separate.
    - Progress: `WithModuleSummaries` adds trusted module summaries to
      single-source analyzer checks without loading or executing modules.
      Literal `require(...)` local bindings can now consume a dependency's
      exported return value fact, including returned table property facts and
      returned function call facts.
      Required module bindings can also qualify exported type aliases, including
      scalar aliases and table aliases used for literal validation and field
      reads. Missing summaries and summaries with stale dependency hashes now
      produce stable diagnostics at the require argument; missing exported type
      aliases report an `unknown-type` diagnostic at the missing alias segment.
    - Checks: importing alias, importing returned table/function, stale or
      missing summary diagnostics.

20. **Host type environment seam**
    - Behavior: base globals and host-provided globals/classes are checked from
      a supplied environment instead of hard-coded checker branches.
    - Change: add a private `typeEnv` adapter behind `AnalyzerOption`; seed it
      with current base-library facts.
    - Progress: `AnalyzerOption` now carries a private `typeEnv` seeded with
      base globals. `WithGlobalTypes` lets host adapters supply global
      `TypeSummary` facts; host global functions check call arguments and
      returns, and host global tables check property reads, string indexer
      reads, access modifiers, and class-like table-valued metatable `__index`
      facts through the same table relation as locals.
    - Checks: default base globals, custom host callback/table, unknown host
      global, and no Hearth dependency in root tests.

## Verification

Use focused checks during each slice:

```sh
go test -count=1 ./... -run '<focused test pattern>'
scripts/check-lane root
```

Before calling a phase complete:

```sh
scripts/check-fast
```

Before retiring this MVP plan:

```sh
scripts/check
```

For documentation-only edits to this plan:

```sh
git diff --check
perl -ne 'print "$ARGV:$.:$_" if /[^\x00-\x7F]/' docs/exec-plans/type-system.md
awk '/[ \t]$/ { print FILENAME ":" FNR ": trailing whitespace" }' docs/exec-plans/type-system.md
```

## Risks

- **Compatibility:** Luau's analyzer is large; the MVP must state exactly which
  behaviors it claims and avoid implying full compatibility.
- **Architecture:** premature package splits could freeze shallow interfaces.
  Keep seams private until real variation proves them.
- **Diagnostics:** centralizing too much can make messages generic. Preserve
  source evidence at relation sites.
- **Flow:** mutating base facts during refinement can create unsound behavior
  after branch and loop joins. Keep refinements in flow views.
- **Packs:** modeling packs as tuple sugar will fail for varargs, final-call
  expansion, and generic packs. Keep pack references first-class.
- **Host facts:** hard-coding Hearth or application globals into root analysis
  would violate the runtime seam. Use an adapter.
- **Performance:** relation and normalization helpers need recursion budgets
  before recursive aliases and unions get broad coverage.

## Retirement Criteria

Retire this plan when all 20 slices have landed, the phase gates are met, and
`Analyzer.Check` can type-check representative strict scripts using annotated
locals, functions, tables, simple generics, packs, flow guards, imported
summaries, and host-provided globals without exposing analyzer internals.
