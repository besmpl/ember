# Ember Design

Ember is a Go-native runtime for Luau-compatible scripting. It should be small
enough to test without Hearth, files, windows, clocks, networking, or native
codegen.

## Runtime Model

The root vocabulary should grow around a few concepts:

- Source: Luau text accepted by a compiler package or adapter.
- Bytecode: encoded instructions, constants, and function prototypes.
- Value: runtime data such as nil, booleans, numbers, strings, tables,
  closures, functions, userdata-like host values, and threads when supported.
- State: owned VM state, stacks, globals, interned strings, tables, and
  garbage-collection bookkeeping.
- Function: either an Ember closure or an explicit Go host callback.
- Host: the outer adapter that provides callbacks, modules, I/O, clocks,
  randomness, logging, and Hearth integration.

## Early Boundary

The root package should not own Hearth worlds, rendering, audio, assets,
editor tooling, network transports, background workers, native executable
memory, or external process lifecycle.

This is not a permanent ban. It keeps the runtime small while the VM proves
itself. Outer packages can attach these capabilities later if real examples
need them.

The default answer to new root-runtime features is no until examples, tests, or
implementation pressure prove the feature is useful enough to carry in the
public interface.

## Determinism

Ember should prefer deterministic behavior for host-visible operations:

- bytecode decoding is stable;
- table iteration order is specified only where Luau specifies it;
- host callbacks are invoked at explicit points;
- randomness and clocks come from host-provided adapters;
- background work is not hidden inside the runtime.

## Deep Modules

Design modules with small interfaces and meaningful implementation depth.
Callers should learn a small surface and get useful behavior from it. Internal
seams are welcome for tests, but external seams should be earned by real
variation or compatibility pressure.
