# ADR 0002: Keep Typed Analysis Erased From Runtime Execution

## Context

Ember needs a full Luau-compatible type system without turning type information
into a hidden prerequisite for runtime execution. Luau's type system is gradual,
supports mode-specific behavior, rich table and function types, type packs,
semantic subtyping, flow refinements, foreign types, and analysis-time type
functions; making those concerns part of the VM interface would couple the
runtime to tooling before compatibility slices prove the need.

## Decision

Keep typed analysis as a deep module beside parsing and compilation: source
enters through a small `Check` or analyzer interface, and the result is a typed
artifact containing diagnostics and module summaries. Runtime compilation and
execution continue to erase annotations and must not require typed artifacts.

The analyzer may share syntax, source ranges, value vocabulary, and eventually a
sandboxed type runtime with the rest of Ember, but its internal type store,
solver, subtyping, normalization, flow refinements, and host type environment
remain private behind the typed-analysis seam.

The seam is intentionally one-way for normal execution:

```text
Source -> Compile -> Proto -> Run
Source -> Analyze -> Typed Artifact
```

Future tools may explicitly consume a typed artifact, but implicit global
checker state must not influence bytecode generation or VM behavior.

## Consequences

This keeps ordinary execution simple and deterministic while allowing the type
system to become large enough to match Luau. Tooling, editor integrations,
module checks, host foreign type definitions, and future optimization passes can
consume typed artifacts explicitly. The tradeoff is that Ember must keep runtime
semantics and typed-analysis semantics tested separately, and any future feature
that wants type facts at runtime must cross an explicit seam instead of reading
checker internals.

## Alternatives Considered

- Attach types directly to runtime values. Rejected because Luau annotations are
  erased and gradual typing includes `any`, `unknown`, type packs, generic
  functions, and type functions that are not ordinary runtime values.
- Make the compiler depend on a successful analysis result. Rejected because
  `nocheck` and existing untyped source must remain executable.
- Expose parser, solver, and type-store details publicly. Rejected because this
  would make the interface shallow and freeze internal algorithms too early.
