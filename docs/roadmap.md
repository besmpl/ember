# Roadmap

Ember should grow through small vertical slices. Each slice should add tests
that prove real behavior.

## Slice 0: Scaffolding

- Project docs.
- Go module.
- Check scripts.
- Empty root package.

## Slice 1: Bytecode Seed

- Status: seeded for constants, globals, register moves, table fields and
  indexes, closures with upvalues, arithmetic, equality, relational comparison,
  host calls, and return as internal bytecode.
- Next pressure: add instruction encoding or decoding helpers when fixtures or
  compatibility tests need them.

## Slice 2: Tiny VM

- Status: seeded for nil, booleans, numbers, strings, host-visible tables,
  opaque host userdata, and a tiny register VM.
- Next pressure: add bytecode fixtures when compatibility tests need them.

## Slice 3: Minimal Compiler Path

- Status: seeded for numeric return expressions, local number bindings,
  scalar literals, erased Luau type annotations, generic function type
  parameters, `typeof` type queries, aliases, and casts, array, named-field,
  and computed-key table literals, chained
  dot-field and bracket reads, identifier references, assignment to existing
  locals and chained table selectors, local shadowing around initializers, and
  `do`/`end` lexical blocks,
  `if`/`then`/`elseif`/`else`/`end` control flow with Luau truthiness,
  `while`/`do`/`end`, `repeat`/`until`, numeric `for`, and generic `for` loops
  with `break` and `continue`,
  arithmetic operators
  `+`, `-`, `*`, `/`, `//`, `%`, and `^` accepting numeric strings, string
  concatenation with `..`, equality comparisons, relational comparisons,
  value-returning `and`/`or` short-circuit expressions,
  `if`/`then`/`elseif`/`else` expressions, unary `not`, unary numeric `-`
  accepting numeric strings, unary length `#`, and parenthesized expressions.
- Next pressure: expand source categories one at a time with source-to-result
  tests.

## Slice 3A: Typed Analysis Foothold

- Status: seeded for `--` line comments, `--[[ ... ]]` multiline comments,
  Luau-style mode directives `--!strict`, `--!nonstrict`, and `--!nocheck`,
  a `Check` entry point that accepts explicit check modes, and an
  `Analyzer.Check` typed-artifact seam with diagnostics. The parser now
  retains local annotations, type aliases, function type parameters, parameter
  annotations, return annotations, table types, function types, unions,
  intersections, nilable types, generic arguments, variadic type packs,
  generic type packs, `typeof` queries, singleton literal types, and `read` or
  `write` table property and indexer modifiers as an internal type tree with
  source ranges. Expression casts retain their parsed type tree while staying
  erased at runtime. The analyzer now reports seeded semantic diagnostics for
  simple builtin local annotations including `any`, `unknown`, `never`, and
  runtime kind names that are valid type identifiers, nilable and union
  annotations made from those builtin types, simple scalar and table type aliases, generic
  scalar/table aliases with ordinary type parameters, assignments to scoped
  known locals, local function return annotations, local function argument
  annotations, local variables annotated with direct or aliased function types
  used as call targets, directly assigned function expression bodies checked
  against local function type annotations, standalone function expression
  parameter and return annotations used for body checks and call return facts,
  missing explicit returns from functions with annotated non-nil returns,
  concrete parenthesized multiple-return packs such as
  `() -> (number, string)`, and conservative all-branch return coverage for
  `if` statements,
  typed variadic function arguments including simple
  generic `...: T` consistency, simple inferred and explicit call-site instantiation
  for local generic function parameters and returns, annotated table
  function-field call arguments, method self-arguments, and returns, named fields in annotated
  table literals including missing nilable fields, simple dot-field reads from
  annotated table locals including nilable field reads, direct and
  generic-alias string-indexer bracket and dot-property reads and writes, plus
  bracket key mismatches from annotated table locals, inferred named-field reads, known-empty inferred
  table literal field and indexer assignment checks, table literal named-field
  checks against string-indexer annotations, numeric indexers,
  and computed-key indexers from local table literals, computed-key table
  literal indexer and required-field checks, annotated table-to-table named-field and indexer
  assignment and reassignment checks including function-call results,
  annotated function table return checks, array-shorthand table type reads, writes, and literal element checks,
  read-only table property/indexer write diagnostics,
  write-only table property/indexer read diagnostics including write-only
  function-field call targets, arithmetic and unary length operand mismatches,
  plus deterministic logical `and`/`or` result facts, including simple control-flow condition
  expressions, arithmetic operand source ranges, numeric `for` bounds and loop-variable typing, initial `ipairs`, `pairs`,
  `next, table`, and direct table loop-variable typing from table indexers and
  named table fields, strict-mode unknown local/global names except known base
  globals, strict-mode unknown type names in annotations, nonstrict suppression
  of unknown-name uncertainty while preserving concrete annotation mismatches,
  nocheck diagnostic suppression, truthy and falsey nilable-local and nilable
  table-field refinement, nil equality and inequality refinement for locals
  and table fields, initial string/boolean singleton equality refinement for
  locals and table fields, while-body type-guard refinement, repeat-until exit
  type-guard refinement, and true/false branch
  `type(local-or-field) == "kind"` refinement for simple union locals and
  table fields. Successful compatible plain-local and tracked table-field
  assignments update the current flow fact for later reads in the same scope,
  and compatible branch-local assignment facts for existing locals and tracked
  table fields are joined after `if` statements. Analyzer host-global type
  facts can suppress unknown-global diagnostics and check host callback
  arguments, host callback returns, host table property and indexer reads,
  host table access modifiers, and class-like table-valued metatable `__index`
  facts. Analyzer
  module summaries can type literal `require(...)` local bindings from
  exported dependency return table and function facts, qualify exported
  scalar/table aliases, and report missing or stale summary inputs plus missing
  exported aliases. Module return summaries track annotated locals, plain local
  reassignment, table-field assignment, and branch-merged local or table-field
  assignments.
- Next pressure: add richer type references, broader source-ranged diagnostic
  evidence, and more table, function, flow-refinement, and pack forms.

## Slice 4: Tables And Calls

- Status: seeded for host-visible `Table` objects, array, named-field, and
  computed-key table literals, chained dot-field and bracket reads, chained field and bracket
  assignment, nil assignment deletion, boolean keys, table identity keys, and
  explicit Go host globals readable and assignable as expression values, with
  callback values callable through names, table selectors, or returned callback
  values as expressions or side-effect statements, plus
  method calls with receiver self-arguments, and minimal local and anonymous
  script closures with upvalues. Table field and method function declarations
  lower to existing table assignment and closure behavior. Table metatables can
  be set and read; table-valued or function-valued `__index` supplies fallback
  reads for missing keys, table-valued or function-valued `__newindex` routes
  missing-key writes, and `__metatable` protects metatables from inspection and
  replacement. Function-valued `__len` handles table length, and
  function-valued `__iter` customizes direct table iteration. Function-valued
  `__tostring` customizes `tostring(table)`. Function-valued `__call` makes
  table values callable. Numeric operator metamethods customize table
  arithmetic, including unary `__unm`; `__concat` customizes table
  concatenation, and `__lt`/`__le` customize table comparisons. `__eq`
  customizes table equality. `rawget`, `rawset`, and `rawlen` provide raw
  table access that bypasses those
  metatable hooks.
  Return statements can produce multiple values, final returned
  script or host calls expand their result lists, and local bindings,
  assignments, or final call arguments adjust value lists across multiple
  names, targets, or arguments. Variadic script functions can receive and
  expand `...`. A tiny pure base-library foothold provides `type`, `math`,
  `tonumber`, `tostring`, `setmetatable`, `getmetatable`, `next`, `pairs`,
  `ipairs`, `rawget`, `rawset`, `rawlen`, `select`, `unpack`,
  `table.pack`/`table.unpack`, `table.insert`, `table.remove`, `table.concat`,
  `table.find`, `table.clear`, and `table.sort`, with host globals able to
  override base globals explicitly.
  Generic `for` loops support iterator expressions and direct table values
  using raw table iteration, `ipairs`, or `__iter`.
- Next pressure: add richer metatable behavior, richer call semantics, broader
  base-library functions, or standard-library tables only when source-to-result
  tests need them.
- Keep host side effects explicit.

## Slice 5: Conformance Categories

- Import selected upstream conformance categories.
- Track unsupported categories directly in docs or test names.
- Add differential tests where practical.

## Later Pressure

- Standard libraries.
- Coroutine and yielding semantics.
- Parser completeness.
- Compiler completeness.
- Analyzer and type checking.
- Performance work.
- Native codegen experiments only if real usage proves the need.
