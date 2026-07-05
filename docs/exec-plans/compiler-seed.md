# Plan: Compiler Seed

## Goal

Prove the first Go-native Luau compiler path by compiling a tiny source program
to Ember bytecode that the tiny VM can execute.

Start with one source shape:

```lua
return 1 + 2
```

The slice is done when a test can take that source through the compiler seam,
run the produced bytecode through the VM, and observe the Luau-compatible result
`3`.

## Status

Implemented as the first root-package vertical slice. The accepted source
category is scalar local bindings, scalar returns, and numeric expressions
joined by `+`, `-`, `*`, `/`, `//`, `%`, and `^` accepting numeric strings, string
concatenation with `..`, plus unary numeric `-` accepting numeric strings,
assignment to existing locals
and table selectors, array,
named-field, and computed-key table literals, chained dot-field and bracket
reads, mutation of host-visible tables through
chained selectors, `do`/`end` lexical blocks,
`if`/`then`/`elseif`/`else`/`end` control flow with Luau truthiness,
`while`/`do`/`end`, `repeat`/`until`, numeric `for`, and generic `for` loops
with `break` and `continue`, equality, inequality,
relational comparisons, value-returning `and`/`or` short-circuit expressions
with `if`/`then`/`elseif`/`else` expressions, unary `not`, unary length `#`,
and parentheses for grouping, explicit Go host globals read and assigned as
expression values, and calls to host callback values through names, table
selectors, or returned callback values as
expressions or side-effect statements, method calls with receiver
self-arguments, plus minimal local and anonymous script closures with upvalues.
Table field and method function declarations lower to existing
closure and table-assignment behavior. Table metatables support table-valued
or function-valued `__index` fallback for missing keys and table-valued or
function-valued `__newindex` routing for missing-key writes, plus
`__metatable` protection. Return statements can produce multiple
values, final returned script or host calls expand their result lists, and
local bindings or assignments adjust value lists across multiple names or
targets. Final call arguments expand result lists, while non-final call
arguments remain single-valued. Variadic script functions can receive and
expand `...`. Function-valued `__len` handles table length. `rawget`, `rawset`,
and `rawlen` provide raw table access around metatable hooks. Function-valued
`__iter` customizes direct table iteration, and function-valued `__tostring`
customizes `tostring(table)`. Function-valued `__call` makes table values
callable. Numeric operator metamethods customize table arithmetic, and
unary `__unm`; `__concat` customizes table concatenation. Relational
metamethods `__lt` and `__le` customize table comparisons. `__eq` customizes
table equality. A tiny pure base-library foothold provides `type`, `math`,
`tonumber`, `tostring`,
`setmetatable`, `getmetatable`, `next`, `pairs`, `ipairs`, `rawget`, `rawset`,
`rawlen`, `select`, `unpack`, `table.pack`/`table.unpack`, `table.insert`,
`table.remove`, `table.concat`, `table.find`, `table.clear`, and `table.sort`.
Examples
include `return 1 + 2`, `return 2 + 3 * 4`,
`return 4 * -2 + 11`, `return (1 + 2) + 3`, `return "ember"`, `return false or 7`,
`return not false and 7`,
`return if hp > 0 then "alive" else "down"`,
`return "hp=" .. 10`,
`return add(1, 2)`, `return api.add(1, 2)`, `return api:add(1, 2)`,
`return greeting`, `return type({hp = 10})`, `return math.max(1, 5, 3)`,
`return getmetatable(setmetatable({}, {}))`,
`return rawget({hp = 25}, "hp")`,
`return #("ember")`,
`return 1, "ember", true`,
`api.log("spawned")`, and:

```lua
local function add(left, right)
    return left + right
end
return add(2, 3)
```

```lua
local function pair()
    return 2, 3
end
return pair()
```

```lua
local function pair()
    return 2, 3
end
local left, right = pair()
left, right = right, left
return left, right
```

```lua
local function echo(head, ...)
    return head, ...
end
return echo("first", 2, 3)
```

```lua
local player = {hp = 10}
function player:heal(amount)
    self.hp = self.hp + amount
    return self.hp
end
return player:heal(5)
```

```lua
local function pair()
    return 2, 3
end
local function collect(...)
    return ...
end
return collect(1, pair())
```

```lua
local tools = {
    add = function(left, right)
        return left + right
    end,
}
return tools.add(4, 6)
```

```lua
local player = {
    hp = 10,
    heal = function(self, amount)
        self.hp = self.hp + amount
        return self.hp
    end,
}
return player:heal(5)
```

```lua
local function makeAdder(base)
    return function(value)
        return value + base
    end
end
local addTwo = makeAdder(2)
return addTwo(3)
```

```lua
local x = 1
local y = x + 2
return y + x
```

```lua
local point = {x = 2, y = 3}
point.x = point.x + point.y
return point.x
```

```lua
local values = {10, 20, 30}
values[2] = values[1] + 5
return values[2]
```

```lua
local players = {{stats = {hp = 10}}, {stats = {hp = 20}}}
players[2].stats.hp = 35
return players[2].stats.hp
```

```lua
local enabled = true
if enabled == true then
    return 10
else
    return 20
end
```

```lua
local count = 0
local total = 0
while true do
    count = count + 1
    if count == 3 then
        continue
    end
    total = total + count
    if count == 5 then
        break
    end
end
return total
```

Host-created tables and script-created tables now share the same Go `*Table`
object model. Scripts can mutate tables passed through `RunWithGlobals`, and Go
can inspect table results returned by scripts. Host-created userdata values
carry opaque Go payloads through scripts. Table keys currently support
booleans, strings, numbers except NaN, table identity, and userdata identity;
nil keys are rejected, and assigning nil deletes an existing key.

## Scope

- In: a narrow compiler entry point for source text.
- In: the smallest parser or source recognizer needed for scalar literals,
  array, named-field, and computed-key table literals, numeric return
  expressions, local bindings, identifier references, assignment to existing
  locals and chained table selectors, chained dot-field and bracket reads,
  `do`/`end` lexical blocks, `if`/`then`/`elseif`/`else`/`end` control flow,
  `while`/`do`/`end`,
  `repeat`/`until`, numeric `for`, and generic `for` loops with `break` and `continue`,
  equality, inequality, relational comparisons,
  value-returning `and`/`or` short-circuit expressions, string concatenation
  with `..`, `if`/`then`/`elseif`/`else` expressions, unary `not`, unary length
  `#`, parenthesized expressions, host globals as readable and assignable
  expression values, and host callback calls through callable expressions,
  method syntax, or side-effect statements,
  multiple return values, local binding, assignment, and final call argument
  value-list adjustment, variadic script functions with `...`, table field and
  method function declarations, erased Luau type annotations retained as an
  internal type tree, generic function type parameters, variadic and generic
  type packs, `typeof` type queries, aliases, and casts, table-valued and
  function-valued `__index` metatable fallback, table-valued and function-valued
  `__newindex` routing,
  `__metatable` protection, function-valued `__len`, function-valued `__iter`,
  function-valued `__tostring`, function-valued `__call`,
  numeric operator metamethods including `__unm`, `__concat`, relational
  `__lt`/`__le`, `__eq`, `rawget`/`rawset`/`rawlen` bypass of those metatable
  hooks, plus minimal local and anonymous script closures with upvalues.
- In: a tiny pure base-library foothold with `type`, `math`, `tonumber`,
  `tostring`, `setmetatable`, `getmetatable`, `next`, `pairs`, `ipairs`,
  `rawget`, `rawset`, `rawlen`, and `select`, plus `unpack`,
  `table.pack`/`table.unpack`, `table.insert`, `table.remove`,
  `table.concat`, `table.find`, `table.clear`, and `table.sort`, with explicit
  host globals able to override base globals.
- In: generic `for` over iterator expressions and direct table values using
  raw table iteration, `ipairs`, or `__iter`.
- In: bytecode emission for constants, globals, register moves, table fields
  and indexes, arithmetic, equality, relational comparison, conditional and
  unconditional jumps including backward loop jumps, loop-exit break jumps, and
  loop-iteration continue jumps, logical short-circuit jumps, grouped
  expression emission, closure creation with upvalue get/set, host-visible
  table mutation, host calls, fixed and open call results, multi-value return,
  adjusted local, assignment, and call-argument value lists, and vararg loads,
  using the existing or next minimal bytecode model.
- In: focused compatibility tests that name the accepted source forms.
- Out: full Luau grammar, multiple assignment, type syntax, error-message
  parity, optimization, and analyzer behavior.

## Design

Keep the first external compiler interface small. A caller should only need to
provide source text and receive either a compiled function/prototype or a clear
compile error.

Suggested initial shape:

```go
func Compile(source string) (*Proto, error)
```

Use concrete names that match the bytecode model once it exists. Avoid exposing
parser nodes, emitter internals, token streams, compiler passes, or upstream
C++ structure until tests prove callers need them.

Internally, keep three responsibilities separate enough to test:

- lexical/source recognition for the tiny accepted syntax;
- expression shaping for numeric literals, arithmetic precedence, unary numeric
  `-`, string concatenation, and unary length `#`;
- bytecode emission for the VM-owned instruction format.

Those are internal seams, not public packages by default. Split packages only
after the implementation pressure makes the root package harder to test or
understand.

## Steps

1. Finish or stub the bytecode seed needed for constants, add, and return.
2. Finish or stub the tiny VM path that can execute the emitted bytecode.
3. Add a source-to-result test for `return 1 + 2`.
4. Implement the narrow compiler path behind the smallest public interface.
5. Add one or two adjacent tests, such as numeric literals and compile errors
   for unsupported source.
6. Document the accepted compatibility level as compiled and executed for this
   tiny source category.

## Checks

Use focused tests first, then the repository checks:

```sh
go test -count=1 ./...
scripts/check-lane root
scripts/check-fast
scripts/check
```

For documentation-only edits to this plan:

```sh
git diff --check
perl -ne 'print "$ARGV:$.:$_" if /[^\x00-\x7F]/' docs/exec-plans/compiler-seed.md
awk '/[ \t]$/ { print FILENAME ":" FNR ": trailing whitespace" }' docs/exec-plans/compiler-seed.md
```

## Risks

- Compatibility: early syntax recognition can accidentally become a fake parser.
  Keep accepted forms explicit in test names.
- Public interface: exporting parser or emitter details too early will make
  later compatibility work harder. Prefer one small compile seam.
- Bytecode coupling: the compiler should emit the VM-owned bytecode format, not
  invent a parallel representation.
- Scope creep: do not add broader grammar until one source-to-bytecode-to-result
  path is green.
