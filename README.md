# Ember

Ember is a Go-native Luau-compatible scripting runtime for Hearth.

The long-term goal is not to mechanically translate Luau's C++ source into Go.
The goal is to grow a small, testable Go implementation piece by piece, using
upstream Luau as the behavioral reference and Hearth as the first host that
proves the embedding API.

## Current Shape

Ember currently has a tiny root-package vertical slice:

1. a value model for nil, booleans, numbers, strings, and host-visible tables;
2. internal bytecode for constants, globals, register moves, table fields and
   indexes, addition, calls, and return;
3. a minimal interpreter loop;
4. a narrow `Compile` path for scalar local bindings, assignments to existing
   locals and table indexes, array and named-field table literals, dot-field
   and bracket reads, `if`/`then`/`else`/`end` control flow with Luau
   truthiness, host-visible table mutation, host function calls, and numeric
   expressions joined by `+`;
5. source-to-result tests such as `return 1 + 2`, scalar literals, and local
   references.

This is only a seed. Full Luau grammar, Lua functions, standard libraries, and
analyzer behavior remain future slices.

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

Start with the documentation inventory:

- [docs/README.md](docs/README.md)
- [docs/principles.md](docs/principles.md)
- [docs/design.md](docs/design.md)
- [docs/roadmap.md](docs/roadmap.md)
- [docs/checks.md](docs/checks.md)

## Checks

```sh
scripts/check-fast
scripts/check
```

For focused Go work, run package-local tests first, then use the scripts before
calling a slice done.
