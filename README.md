# Ember

Ember is a Go-native Luau-compatible scripting runtime for Hearth.

The long-term goal is not to mechanically translate Luau's C++ source into Go.
The goal is to grow a small, testable Go implementation piece by piece, using
upstream Luau as the behavioral reference and Hearth as the first host that
proves the embedding API.

## Current Shape

Ember currently has a tiny root-package vertical slice:

1. a value model for nil, booleans, numbers, strings, host-visible tables, and
   opaque host userdata;
2. internal bytecode for constants, globals, register moves, table fields and
   indexes, arithmetic, calls, and return;
3. a minimal interpreter loop;
4. a narrow `Compile` path for scalar local bindings, assignments to existing
   locals and chained table selectors, erased Luau type annotations, generic
   function type parameters, `typeof` type queries, aliases, and casts, array,
   named-field, and computed-key table literals, chained dot-field and bracket
   reads,
   `do`/`end` lexical blocks,
   `if`/`then`/`elseif`/`else`/`end` control flow with Luau truthiness,
   `while`/`do`/`end`, `repeat`/`until`, numeric `for`, and generic `for`
   loops over iterator expressions or direct table values with `__iter`,
   `break`, and `continue`,
   host-visible table mutation, host globals read and assigned as expression
   values, host callback calls through names or selector expressions as
   expressions or statements, method calls with receiver
   self-arguments, minimal local and anonymous closures with upvalues, table
   field and method function declarations, table metatables with table-valued
   and function-valued `__index` and `__newindex`, function-valued `__iter`
   and `__tostring`, function-valued `__call`, arithmetic and concat
   metamethods including `__unm`, relational and equality metamethods, plus
   `__metatable` protection, multiple return values and value-list adjustment
   for returns,
   local bindings, assignment, and final call arguments, variadic script
   functions with `...`, plus expressions
   joined by `+`, `-`, `*`, `/`, `//`, `%`, `^`, `..`, `==`, `~=`, `<`, `<=`, `>`, `>=`,
   `and`, or `or`, `if`/`then`/`elseif`/`else` expressions, unary `not`, unary
   numeric `-`, unary length `#`, and parentheses for grouping;
5. a strict-mode `Check` foothold that recognizes `--!strict` file comments
   and retains an internal type tree with source ranges for the future
   typed-analysis path;
6. a tiny pure base-library foothold with `type`, `math`, `setmetatable`,
   `tonumber`, `tostring`, `getmetatable`, `next`, `pairs`, `ipairs`,
   `rawget`, `rawset`, and `rawlen`, plus `select`, `unpack`, and
   `table.pack`/`table.unpack`,
   `table.insert`, `table.remove`, `table.concat`, `table.find`, `table.clear`,
   and `table.sort`;
7. source-to-result tests such as `return 1 + 2`, scalar literals, and local
   references.

This is only a seed. Full Luau grammar, full function syntax, broader standard
libraries, and analyzer behavior remain future slices.

## Import Path

```go
import "github.com/besmpl/ember"
```

The root package should remain small. Future packages should exist only after a
slice proves that the split makes the public interface smaller or the
implementation easier to test.

## Project Direction

Ember should be:

- Go-native, with ordinary Go values, errors, tests, and package structure;
- Luau-compatible where compatibility is claimed;
- deterministic enough for headless Hearth simulations;
- embeddable without hidden global runtime ownership;
- explicit about host callbacks, clocks, I/O, randomness, and cancellation;
- built in vertical slices that run real scripts or conformance fixtures.

## Docs

Start with the small durable documentation set:

- [docs/README.md](docs/README.md)
- [docs/principles.md](docs/principles.md)
- [docs/design.md](docs/design.md)
- [docs/compatibility.md](docs/compatibility.md)
- [docs/public-surface.md](docs/public-surface.md)
- [docs/hearth-integration.md](docs/hearth-integration.md)
- [docs/checks.md](docs/checks.md)

The compatibility document is the maintained feature manifest: every claimed
Luau slice points to at least one behavior test, and a package test checks that
the referenced test names still exist.

Standing future-plan docs are intentionally not kept in the repository. When a
slice needs coordination, write the smallest temporary plan that proves useful
and retire or delete it when the slice lands or is abandoned.

## Checks

```sh
scripts/check-fast
scripts/check
```

For focused Go work, run package-local tests first, then use the scripts before
calling a slice done.
