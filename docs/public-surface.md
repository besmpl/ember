# Public Surface

Ember's public surface should stay smaller than its implementation.

## Initial Import

```go
import "github.com/besmpl/ember"
```

The root package starts as the default import. Add subpackages only when a
slice proves that the split reduces public complexity or creates a useful
testable seam.

## Surface Rules

- Export only names that callers need for a proven slice.
- Document exported behavior, errors, defaults, and side effects.
- Keep host integration explicit; do not hide application or process ownership
  in the runtime.
- Do not expose internal bytecode, parser, stack, or GC machinery for
  convenience.
- Prefer stable plain values and small interfaces over large configuration
  graphs.

## Current Surface

- `Compile(source string) (*Proto, error)` compiles the currently supported
  source slice.
- `NewAnalyzer(options ...AnalyzerOption) *Analyzer` creates Ember's typed
  analyzer.
- `WithGlobalTypes(globals map[string]TypeSummary) AnalyzerOption` adds
  host-provided global type facts to analyzer checks. These facts affect typed
  analysis only; runtime globals still enter through runtime host seams such as
  `RunWithGlobals`.
- `AnalysisConfig` is the copy-owned typed-analysis input shared by
  `NewAnalyzer` through `WithAnalysisConfig` and by `LoadProgram` through
  `ProgramOptions.Analysis`. Host global and module-summary maps are cloned at
  the boundary.
- `WithModuleSummaries(summaries map[string]ModuleSummary) AnalyzerOption` adds
  trusted module summaries for single-source analyzer checks. Literal
  `require(...)` local bindings can use a dependency's exported return value
  fact and qualify exported type aliases without invoking runtime module
  loading. Missing summaries and summaries whose dependency hashes are stale
  produce typed diagnostics; missing exported type aliases report `unknown-type`
  at the missing alias segment.
- `(*Analyzer).Check(ctx context.Context, source Source) (*CheckResult, error)`
  parses and analyzes source. It accepts explicit Luau-style `--!strict`,
  `--!nonstrict`, and `--!nocheck` directives and returns a typed artifact
  with source mode and diagnostics. The seeded semantic diagnostics cover simple builtin local
  annotations including `any`, `unknown`, `never`, and runtime kind names
  that are valid type identifiers,
  nilable and union annotations made from those builtin types,
  simple scalar and table type aliases, generic scalar/table aliases with
  ordinary type parameters, assignments to scoped known locals, local function
  return annotations, local function argument annotations, local variables
  annotated with direct or aliased function types used as call targets,
  directly assigned function expression bodies checked against local function type annotations,
  standalone function expression parameter and return annotations used for body
  checks and call return facts, missing explicit returns from functions with
  annotated non-nil returns, concrete parenthesized multiple-return packs such
  as `() -> (number, string)`, and conservative all-branch return coverage for
  `if` statements,
  typed variadic function arguments including simple generic `...: T` consistency, simple
  inferred and explicit call-site instantiation for local generic function
  parameters and returns, annotated table function-field call arguments,
  method self-arguments, and returns, named fields in annotated table literals including
  missing nilable fields, simple dot-field reads from annotated table locals,
  including nilable field reads, direct and generic-alias
  string-indexer bracket and dot-property reads and writes, plus bracket key
  mismatches from annotated table locals, inferred named-field reads, known-empty inferred table literal
  field and indexer assignment checks, table literal named-field checks
  against string-indexer annotations, numeric indexers, and computed-key
  indexers from local table literals, computed-key table literal indexer
  and required-field checks, annotated table-to-table named-field and indexer assignment and
  reassignment checks including function-call results, annotated function table
  return checks, array-shorthand table type reads, writes, and literal element checks,
  read-only table property/indexer write diagnostics and write-only table
  property/indexer read diagnostics including write-only function-field call targets,
  arithmetic and unary length operand mismatches, plus deterministic logical
  `and`/`or` result facts, including simple control-flow condition expressions, numeric `for`
  bounds and loop-variable typing, initial `ipairs`,
  `pairs`, `next, table`, and direct table loop-variable typing from table
  indexers and named table fields,
  strict-mode unknown local/global names except known base globals,
  strict-mode unknown type names in annotations, nonstrict suppression of
  unknown-name uncertainty while preserving concrete annotation mismatches,
  nocheck diagnostic suppression, truthy and falsey nilable-local
  and nilable table-field refinement, nil equality and inequality refinement
  for locals and table fields, initial string/boolean singleton equality
  refinement for locals and table fields, while-body type-guard refinement,
  repeat-until exit type-guard refinement, and true/false branch
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
  exported aliases. Parser or
  cancellation failures are returned as Go errors; Luau type findings are
  returned as diagnostics. Arithmetic operand mismatch diagnostics identify the
  offending operand source range. Module return summaries track annotated
  locals, plain local reassignment, table-field assignment, and branch-merged
  local or table-field assignments.
- `Check(source string) error` is the convenience typed-analysis entry point.
  It requires an explicit check-mode directive and returns the first typed
  diagnostic as an error.
- Program diagnostics include `SourceName`, `Module`, and exact source-relative
  byte ranges. `LoadProgram` checks dependencies before consumers, lends only
  trusted current dependency summaries, and returns the same sorted report at
  every parallelism. Environment-dependent checks are rebuilt rather than
  stored in the shared source artifact cache.
- Syntax and compile failures expose `*SourceError` through `errors.As`.
  `SourceError.Source`, `Code`, `Start`, and `End` are structured source data;
  its wrapped cause preserves `errors.Is`, `errors.As`, and existing error text.
- `Run(proto *Proto) ([]Value, error)` executes with Ember's pure base globals
  and without host globals.
- `RunWithGlobals(proto *Proto, globals map[string]Value) ([]Value, error)`
  executes with Ember's pure base globals plus explicit host-provided globals.
  Host-provided globals override base globals with the same name.
- Execution failures returned to Go are represented by `*RuntimeError` when
  script frames are available. `RuntimeError.Message` is the stable message,
  `Frames` is ordered innermost-first, and `Cause` remains available through
  `errors.Is` and `errors.As`; `Runtime.Invoke` and `Runtime.Dispatch` add
  contextual `%w` wrapping while
  `Callback.Call` preserves the runtime error without flattening it. Ordinary
  script and host errors are values under `pcall`/`xpcall` (the latter may
  invoke its handler), while
  context cancellation/deadline and every `LimitError` are protected-boundary
  failures that escape to Go and preserve their `errors.Is`/`errors.As`
  identity.
- A compiled `*Proto` is immutable after sealing and may be executed
  concurrently. Each execution runtime owns its function instances, inline
  caches, and reusable closure values, so warming one runtime does not mutate
  or contaminate another runtime's state. Host-provided globals and tables
  remain caller-owned and still require ordinary synchronization when shared.
- `(*Program).WritePreparedGo` writes one deterministic Go source file for the
  exact loaded Program. `PreparedGoOptions.Package` supplies the generated
  package name; `MaxBytes` rejects oversized output before writing and defaults
  to a conservative bound. The generated package exports one immutable
  `PreparedBundle`, which a host supplies explicitly through
  `RuntimeOptions.Prepared`. A nil bundle preserves the ordinary dynamic
  Machine path. An ABI, semantic-version, Program-hash, or Proto-inventory
  mismatch returns `*PreparedBundleError` before runtime-owner mutation and
  never silently falls back. Generated bundles are trusted build artifacts,
  not an untrusted-code sandbox; Ember does not use `init` registration,
  plugins, helper processes, or runtime Go compilation.
- Scripts can read and assign globals as expression values, call host global
  functions, access fields or indexes on host global tables, and pass opaque
  host userdata values through script code. Local and upvalue names take
  precedence over globals. With `RunWithGlobals`, global assignments write back
  to the provided globals map.
- `Runtime.Invoke(ctx, Invocation, args...)` initializes one module if needed
  and calls either its named exported script function or, when
  `Invocation.Export` is empty, its returned script function directly.
  `Invocation.Globals` is copied and is
  explicit input to both initialization and the call. The method returns all
  script results and applies no entrypoint fan-out, ordering, skip, or reporting
  policy. A missing module/export or non-callable target is an error.
- `Runtime.InvokeResumable` is the suspension-capable form of the same
  single-module operation. Its `ExecutionResult` contains values or pending
  suspensions and has no dispatch report. Suspensions are attributed to the
  invoked `Module` and `Operation`; `Entrypoint` is empty.
- `Runtime.Dispatch` is an optional cohort convenience. It initializes every
  configured entrypoint in declared order, calls a shared named operation where
  present, and returns `DispatchReport`. `RuntimeHost.Globals` supplies its
  per-load and per-operation environments through `HostCall.Operation`.
  Applications without this table-or-nil, fan-out, and skip policy should build
  their own orchestration from `Invoke`.
- `CaptureCallback(ctx context.Context, value Value) (Callback, error)` captures
  a script function passed to a `ContextHostFuncValue` while Ember is executing
  a runtime call. The captured callback keeps the owning runtime, module
  `require` context, and host globals, but not the active invocation budget.
- `Callback.Call(ctx context.Context, args ...Value) ([]Value, error)` invokes a
  captured script callback later. The call context controls cancellation and is
  visible to context-aware host callbacks invoked by the script. Each call gets
  a fresh budget from the runtime's configured limits. A callback shares mutable
  state with its owning runtime, so hosts should serialize calls with other work
  on that runtime.
- `ResumableHostFuncValue` exposes a context-aware callback that returns
  `HostReturn`, `HostError`, or `HostSuspend`. A suspension carries an opaque
  host-owned token and creates no goroutine, timer, channel, retry, or
  scheduler.
- `Runtime.InvokeResumable`, `Runtime.DispatchResumable`, and
  `Callback.CallResumable` retain execution until completion or suspension.
  `ExecutionResult.Suspensions` contains every host-visible wait still pending;
  dispatch waits are ordered by declared entrypoint order. If an independently
  started operation is blocked only on a
  module initializer owned by another operation, its result contains one
  tokenless dependency continuation instead of pretending the operation
  completed or duplicating the initializer token. `ExecutionResult.Suspension`
  remains an alias for the first pending handle. Completed and skipped dispatch
  calls are reported in declared order even when the host resumes waits in
  another order.
- `Suspension.Token` exposes only a host-owned token and is nil for a dependency
  continuation. `Suspension.Ready` is immediately true for host waits and turns
  true for a dependency continuation when its initializer completes or fails.
  An early `Resume` or `Fail` returns `ErrSuspensionPending` without consuming
  the handle; once ready, resuming continues that operation with the shared
  initializer result. `Entrypoint`, `Module`, and `Operation` identify dispatch
  waits; direct invocation waits omit `Entrypoint`, and retained callback waits
  omit all attribution. `Cancel` is idempotent; after
  it, `Resume` and `Fail` report `ErrSuspensionStale`. A resumed script may
  expose zero, one, or several successor suspensions.
- Module top-level code and newly required module initializers participate in
  the same resumable operation as their invocation, dispatch, or callback.
  Every module,
  including an entrypoint root, has at most one in-flight initializer and one
  cache result. Independent initializers may remain suspended together and
  finish in host-selected order. Internal dependency waits are never exposed as
  host tokens; dependency continuations represent whole independently started
  operations, while ready dependents inside one dispatch operation are pumped in
  declared entrypoint order. Recursive require paths still report the complete
  active-loading cycle.
- Every resumable step uses normal runtime serialization and receives a fresh
  context and execution budget. Context validation and VM/Machine run admission
  occur before the handle is consumed under the same serialization decision.
  A context already canceled or a runtime already busy therefore leaves the
  exact handle retryable. Several idle suspended invocations may coexist. A
  completion-only `Invoke`, `Dispatch`, or `Callback.Call` returns an error
  before
  guest execution while module initialization is suspended because that call
  cannot return a continuation.
  Canceling the public owner of a module initializer aborts that initializer
  and its private dependents without guest execution, while unrelated public
  waits remain valid and later initialization retries cleanly. `Runtime.Close`
  applies the same cleanup to all retained execution state and tokens; later
  handle use reports `ErrSuspensionStale`.
- `RunHook`, `RunHookResumable`, `HookReport`, `HookCallReport`,
  `HostCall.Hook`, `ExecutionResult.Hook`, and `Suspension.Hook` are deprecated
  compatibility names. `Callback.Call` remains completion-only for callbacks
  that do not suspend.
- Seeded base globals are `type`, which returns Luau-style kind names for the
  current value model, and `math`, which currently provides `abs`, `floor`,
  `min`, `max`, and `pi`, plus `table`, which currently provides `pack`,
  `unpack`, `insert`, `remove`, `concat`, `find`, `clear`, and `sort`, plus
  `tonumber`, `tostring`, `setmetatable`, `getmetatable`, `next`, `pairs`,
  `ipairs`, `rawget`, `rawset`, `rawlen`, `select`, and `unpack`. Script
  functions and host callbacks both report as `"function"`.
- Tables can have metatables. The seeded metatable behavior is table-valued or
  function-valued `__index` fallback for missing table keys and table-valued or
  function-valued `__newindex` routing for missing-key writes. `__metatable`
  protects a metatable by making `getmetatable` return the protection value and
  making later `setmetatable` calls fail. Function-valued `__len` handles the
  length operator for tables, and function-valued `__iter` customizes generic
  `for` over direct table values. Function-valued `__tostring` customizes
  `tostring(table)` and must return a string. Function-valued `__call` lets a
  table value be called and receives the table as its first argument. Numeric
  operator metamethods `__add`, `__sub`, `__mul`, `__div`, `__idiv`, `__mod`,
  `__pow`, and `__unm` customize table arithmetic, and `__concat` customizes
  table concatenation. Relational metamethods `__lt` and `__le` customize table
  comparisons and must return booleans. Equality metamethod `__eq` customizes
  table equality. Other metamethods are not implemented yet.
- `rawget` and `rawset` bypass `__index` and `__newindex` while keeping the
  same table key validation and nil-deletion semantics.
- `#` returns string byte length, calls function-valued table `__len` when
  present, and otherwise returns the contiguous raw positive-integer table
  prefix length. `rawlen` returns the raw string or table length and bypasses
  `__len`.
- Numeric arithmetic operators `+`, `-`, `*`, `/`, `//`, `%`, and `^` accept
  number values or strings that parse as numbers. `//` floors the quotient. `^`
  is right-associative and binds tighter than unary numeric `-`. When raw
  numeric coercion is not possible, table operands can use the corresponding
  numeric operator metamethod; unary `-` can use `__unm`. `+` does not
  concatenate strings.
- `..` concatenates strings and coerces number operands to strings. When raw
  concatenation is not possible, table operands can use `__concat`. Other value
  kinds currently return a runtime error.
- Relational operators compare raw numbers or raw strings. When raw comparison
  is not possible, table operands can use `__lt` for `<` and `>`, or `__le` for
  `<=` and `>=`.
- Equality operators compare raw values by default. Table operands can use
  `__eq` for `==`, and `~=` returns the inverse of that result. Ember calls
  `__eq` even when both operands are the same table.
- `if`/`then`/`elseif`/`else` expressions select one branch using Luau
  truthiness and only evaluate the selected branch.
- `if` statements support `elseif` branches and Luau truthiness.
- `do`/`end` blocks create a lexical scope for locals without creating a loop
  context.
- `repeat`/`until` loops run the body before testing the condition. Body locals
  are visible to the `until` condition and do not leak after the loop.
- Numeric `for` loops support optional steps, positive and negative step
  direction, `break`, and `continue`. The loop variable is local to the loop
  body.
- Generic `for` loops support iterator expressions such as `pairs(table)`,
  `ipairs(table)`, and `next, table`, plus direct table values using the
  current raw table iteration order or a function-valued `__iter` metamethod.
  Raw table iteration is deterministic insertion order across array, string,
  table, userdata, boolean, and other hash keys. Updating an existing key keeps
  its position; setting nil removes the key from active iteration. Loop
  variables are scoped to the body. Explicit `pairs(table)` uses raw table
  iteration. `ipairs(table)` walks positive integer keys from 1 and stops at
  the first nil value.
- Table literals support array fields, named fields, and computed-key fields
  such as `{[key] = value}`.
- Return statements can produce multiple `Value` results. A final returned
  script or host call, such as `return 1, pair()`, expands that call's result
  list. Local bindings and assignments adjust value lists across multiple
  names or targets. Final call arguments expand result lists, so
  `collect(1, pair())` receives all final results from `pair`; non-final call
  arguments remain single-valued. Variadic script functions can receive and
  expand `...`.
- Local variable, function parameter, and function return type annotations,
  generic function type parameters, erased type aliases, and erased casts such
  as `local n: number = 1`, `local function id<T>(x: T): T`,
  `type Stats = {hp: number}`, `typeof(game.Workspace)`, and `value :: number`
  are accepted as syntax.
  Supported type syntax includes qualified names, nilable types, unions,
  intersections, table types, function types with optional argument names,
  generic function types, generic type arguments, variadic type packs such as
  `...T`, generic type packs such as `U...`, and Luau table type access
  modifiers such as `read Name: string` and `write [number]: boolean`.
  `typeof(...)` type queries parse their inner expression but do not evaluate
  it at runtime. The parser retains annotations, aliases, and casts as an
  internal type tree with source ranges for `Check`, but Ember does not
  enforce those annotations at runtime.
- `--` line comments and `--[[ ... ]]` multiline comments are accepted.
  `--!strict`, `--!nonstrict`, and `--!nocheck` directives are recognized by
  the parser; `Check` requires one of those explicit modes.
- `select("#", ...)` returns the number of received values. `select(index, ...)`
  returns the values from `index` through the end, with negative indexes
  counted from the end.
- `tonumber(value, base)` returns numbers unchanged, parses strings as decimal
  numbers by default, parses integer strings in bases 2 through 36 when `base`
  is provided, and returns nil for failed conversions. `tostring(value)`
  converts current scalar values to strings, calls table `__tostring` when
  present, and otherwise returns stable kind names for non-scalars in this
  seed.
- `table.pack(...)` stores varargs in a table and records the count in `n`.
  `table.unpack(table, first, last)` and global `unpack` expand a raw numeric
  range; pass `1, packed.n` when unpacking a table from `table.pack` that may
  contain nil values.
- `table.insert(table, value)` appends after the raw array prefix.
  `table.insert(table, index, value)` shifts later raw array-prefix values up.
  `table.remove(table, index)` returns the removed value and shifts later raw
  array-prefix values down; without an index it removes the last raw prefix
  value.
- `table.concat(table, separator, first, last)` joins raw string values across
  the requested raw numeric range. The separator defaults to `""`, `first`
  defaults to `1`, and `last` defaults to the raw array-prefix length.
- `table.find(table, value, init)` searches raw positive integer keys starting
  at `init` or `1` and stops at the first nil. `table.clear(table)` removes all
  raw entries while preserving the table object and metatable.
- `table.sort(table, compare)` sorts the raw array prefix in place. Without a
  comparator it uses normal `<` behavior, including `__lt` when needed. When
  provided, `compare(left, right)` must return a boolean.
- `Value` exposes kind-specific inspectors for nil, booleans, numbers,
  strings, tables, and userdata. Script closure values created by local
  function declarations or function expressions can be observed through `Kind`
  when returned internally, but they are not host-constructible yet.
- `NewTable() *Table` creates a host-visible Luau table.
- `TableValue(*Table) Value` passes a table through scripts and host globals.
- `Value.Table() (*Table, bool)` returns the backing table object.
- `NewUserData(payload any) *UserData` creates an opaque Go-owned host object.
  Scripts can pass userdata around, compare userdata by identity, call
  `type(value)` and use userdata as table keys, but cannot inspect the payload.
- `UserDataValue(*UserData) Value` passes userdata through scripts and host
  globals.
- `Value.UserData() (*UserData, bool)` returns the backing userdata object.
- `(*UserData).Payload() any` returns the Go payload for host callbacks and
  embedding adapters.
- `(*Table).Get(Value) (Value, error)` and `(*Table).Set(Value, Value) error`
  provide table storage through the same key semantics the VM currently
  supports: booleans, strings, numbers except NaN, table identity keys, and
  userdata identity keys. Missing keys read through table-valued `__index` when
  present and otherwise read as nil; missing-key writes route through
  table-valued `__newindex` when present. Setting nil deletes the key. Nil and
  NaN keys return errors.
- `HostFunc` and `HostFuncValue` let adapters expose Go callbacks to scripts.
  When a script calls a host function with method syntax, such as
  `api:add(1, 2)`, the receiver table is passed as the first argument.

Do not expose parser, bytecode, register, or stack details unless a later slice
proves callers need them.

## Likely Future Packages

These are candidates, not commitments:

- `bytecode`: instruction formats and decoding.
- `compile`: source-to-bytecode compilation.
- `vm`: execution internals if the root package gets too crowded.
- `conformance`: test harness helpers.
- Host adapters belong in their application repositories, not in Ember.

Do not create these until implementation pressure proves they are useful.
