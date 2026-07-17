# Core Principles

Ember is Luau-inspired and Go-native. It should preserve Luau behavior where
compatibility is claimed, but it should not clone C++ implementation structure
when ordinary Go can express the same behavior more clearly.

Use these principles as decision rules when designing the runtime.

## Decision Rules

- Build one vertical runtime slice at a time.
- Prefer compatibility tests over promises.
- Keep public interfaces small, typed, useful, and hard to misuse.
- Keep core behavior deterministic for hosts that need repeatable execution.
- Keep host systems at the edges: files, clocks, randomness, logging, I/O,
  networking, application state, process lifecycle, and native code.
- Avoid hidden global runtime ownership.
- Avoid unsafe code until profiling or compatibility proves it is necessary.
- Prefer ordinary Go objects and Go runtime features behind Ember interfaces:
  Go GC for object lifetime, Go structs/maps/slices for runtime objects, and
  goroutines only behind explicit cooperative coroutine semantics when needed.
- Let examples and conformance fixtures drive ergonomics before adding
  abstractions.
- Add compatibility costs deliberately. Bytecode shapes, error behavior, and
  public names should not change accidentally.

## Luau-Compatible, Not C++-Cloned

Ember should feel familiar to someone embedding Luau:

- source compiles to Luau-shaped bytecode;
- bytecode executes with Luau-compatible value and call behavior;
- standard libraries match the claimed compatibility level;
- host callbacks are explicit;
- errors and yielding have clear runtime semantics;
- analyzer and tooling can be added without reshaping the VM interface.

The implementation should still be idiomatic Go. Early Ember should favor
plain structs, small functions, clear errors, and focused tests over macro-like
ports, large object graphs, or speculative interfaces.

## How To Use This Now

Treat these principles as constraints for slices, not as an active queue. Keep
future-plan docs out of the durable documentation set unless a concrete slice
needs coordination, and retire them when they stop shaping current work.

When a future slice proposes public runtime surface, require concrete examples,
tests, or compatibility pressure before adding it.
