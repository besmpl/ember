# Luau Compatibility

Ember should claim compatibility only where tests prove it.

## Compatibility Sources

Use upstream Luau as the reference for:

- bytecode instruction encoding and decoding;
- parser and compiler behavior;
- VM execution semantics;
- standard library behavior;
- error messages where callers reasonably depend on them;
- coroutine and yielding behavior when implemented;
- analyzer behavior when implemented.

## Compatibility Levels

Ember can describe support in levels rather than all-or-nothing claims:

| Level | Meaning |
| --- | --- |
| Parsed | Source is accepted and represented in Ember data structures. |
| Compiled | Source compiles to Ember bytecode with tested instruction shapes. |
| Executed | Bytecode runs with tested runtime behavior. |
| Conformant | A named upstream conformance slice passes. |
| Embedded | The behavior is usable through a stable Go host interface. |

## Test Strategy

- Start with small handwritten fixtures for each runtime slice.
- Add differential tests against upstream Luau when practical.
- Import upstream conformance tests by category, not as a single giant gate.
- Keep failing or unsupported categories documented.
- Prefer bytecode fixtures for VM work before the compiler exists.

## Documented Ember Choices

- Raw table iteration through `next`, `pairs`, and direct table generic `for`
  uses deterministic insertion order. Luau does not guarantee a portable raw
  table order, so tests that depend on Ember's order are testing Ember's host
  contract rather than upstream ordering.

## Non-Goals For Early Ember

- Full native codegen.
- Full analyzer parity.
- C API compatibility.
- Automatic translation of upstream C++ files.

These can become future projects, but they should not block the first useful
Go-native runtime.
