# ADR 0003: Represent Analyzer Types With Opaque References

## Context

Luau-compatible typed analysis needs recursive table types, function overloads,
unions, intersections, aliases, generic instantiation, type packs, flow
refinements, normalization caches, and eventually reflected type values for type
functions. Exposing recursive Go structs as the analyzer's type representation
would make cycles, memoization, identity, formatting, and future representation
changes leak across the whole codebase.

## Decision

Represent analyzer types and type packs internally with opaque references owned
by a type store. Public typed artifacts may expose stable summaries,
diagnostics, and display strings, but not raw recursive type nodes. Internal
phases pass references plus evidence, and the type store owns construction,
interning, normalization caches, debug formatting, and future reflection into
the type runtime.

## Consequences

This makes the type core harder to sketch at first, but gives Ember locality for
the hard parts: alias cycles, generic instantiation, pack adjustment,
normalization, and type-function reflection. Tests should prefer source-level
diagnostics and module summaries; focused internal tests can cover the type
store only where public behavior cannot yet express a slice.

## Alternatives Considered

- Pass recursive structs between phases. Rejected because it makes type identity
  and cycles accidental.
- Export a public type-node tree. Rejected because it freezes implementation
  details before compatibility pressure proves the right representation.
- Store only display strings. Rejected because subtyping, flow, packs, and type
  functions need semantic structure.
