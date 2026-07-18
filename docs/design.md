# Ember Design

Ember is a Go-native runtime for Luau-compatible scripting. It should be small
enough to test without an application framework, files, windows, clocks,
networking, or native codegen.

## Runtime Model

The root vocabulary should grow around a few concepts:

- Source: Luau text accepted by a compiler package or adapter.
- Bytecode: encoded instructions, constants, and function prototypes.
- Value: runtime data such as nil, booleans, numbers, strings, tables,
  closures, functions, userdata-like host values, and threads when supported.
- State: owned VM state, stacks, globals, interned strings, tables, and
  garbage-collection bookkeeping.
- Function: either an Ember closure or an explicit Go host callback.
- Host: an outer adapter that provides callbacks, modules, I/O, clocks,
  randomness, logging, and application integration.

## Go-Native Mapping

Ember should map Luau runtime concepts onto ordinary Go objects and Go runtime
features wherever that preserves the claimed Luau behavior. Tables, closures,
threads, and host values should first be designed as small Ember interfaces
backed by Go structs, pointers, maps, slices, channels, goroutines, or other
standard library tools as appropriate.

Use Go's garbage collector by default. Do not build an Ember-specific GC until a
proven feature needs it, such as weak tables, finalization, memory quotas,
deterministic memory accounting, or performance work that cannot be solved
behind the existing interfaces.

Goroutines may become an implementation tool for Luau coroutine or thread
support, but they are not the public semantic model. Luau coroutines are
cooperative and resume/yield shaped; any goroutine-backed implementation must
preserve that behavior behind an Ember-owned interface.

See `docs/adr/0001-go-native-runtime-mapping.md` for the full decision.

## Early Boundary

The root package should not own game worlds, request routing, job scheduling,
rendering, audio, assets, editor tooling, network transports, background
workers, or external process lifecycle. ADR 0010 records the narrow exception
for generation-owned executable memory: only the explicit
`PreparedRuntimeSlot.Prepare` operation may install the proved reload-time
numeric tier, behind one private boundary and exact Machine replay.

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
