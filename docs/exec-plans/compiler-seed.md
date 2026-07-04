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
joined by `+`, plus assignment to existing locals and table indexes, array and
named-field table literals, dot-field and bracket reads, mutation of
host-visible tables, `if`/`then`/`else`/`end` control flow with Luau
truthiness, and calls to explicit Go host callbacks. Examples include
`return 1 + 2`, `return "ember"`, `return add(1, 2)`, and:

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
local enabled = true
if enabled then
    return 10
else
    return 20
end
```

Host-created tables and script-created tables now share the same Go `*Table`
object model. Scripts can mutate tables passed through `RunWithGlobals`, and Go
can inspect table results returned by scripts. Table keys currently support
booleans, strings, numbers except NaN, and table identity; nil keys are
rejected, and assigning nil deletes an existing key.

## Scope

- In: a narrow compiler entry point for source text.
- In: the smallest parser or source recognizer needed for scalar literals,
  array and named-field table literals, numeric return expressions, local
  bindings, identifier references, assignment to existing locals and table
  indexes, dot-field and bracket reads, `if`/`then`/`else`/`end` control flow,
  and host callback calls.
- In: bytecode emission for constants, globals, register moves, table fields
  and indexes, addition, conditional and unconditional jumps, host-visible
  table mutation, host calls, and return, using the existing or next minimal
  bytecode model.
- In: focused compatibility tests that name the accepted source forms.
- Out: full Luau grammar, multiple assignment, Lua functions, method calls,
  type syntax, error-message parity, optimization, and analyzer behavior.

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
- expression shaping for numeric literals and binary `+`;
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
