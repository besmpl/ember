# ADR 0001: Prefer Go-Native Runtime Mapping

## Context

Ember is a Go-native Luau-compatible runtime. It should match Luau behavior
where compatibility is claimed, but it should not mechanically port Luau's C++
ownership model, allocator shape, garbage collector, coroutine scheduler, or
object graph unless real compatibility or performance pressure requires it.

The first host-visible table slice already points in this direction: a Luau
table is represented as a Go object, exposed as `*Table`, stored inside `Value`,
and collected by Go's garbage collector when unreachable.

## Decision

Prefer mapping Luau runtime concepts onto ordinary Go objects and Go runtime
features behind small Ember interfaces.

- Represent heap values such as tables, closures, threads, and host values as Go
  objects owned by ordinary Go references.
- Use Go's garbage collector by default. Do not implement an Ember-specific GC
  until a compatibility, memory-limit, weak-reference, finalization,
  performance, or determinism requirement proves it is needed.
- Keep Luau semantics at the Ember interface. Go implementation details should
  remain private enough that internal storage can change without callers
  changing code.
- Treat Go goroutines as a possible implementation tool for future Luau thread
  or coroutine support, not as the public semantic model. Luau coroutines are
  cooperative and resume/yield shaped; any goroutine-backed implementation must
  preserve that behavior through an Ember-owned interface.
- Prefer standard Go synchronization, ownership, and cancellation mechanisms
  when Ember needs background execution, while keeping hidden concurrency out of
  deterministic runtime paths.

## Consequences

This should make early development simpler:

- object lifetime is handled by Go;
- cyclic table graphs can be collected by Go when unreachable;
- host code can share real objects with scripts without copying through a fake
  runtime heap;
- tests can use plain Go values and public Ember interfaces;
- future internals can improve from maps to array/hash table storage without
  changing the table interface.

The tradeoff is that compatibility-sensitive features need explicit design
before implementation:

- weak tables;
- `__gc`-style finalization if supported;
- memory quotas and deterministic memory accounting;
- coroutine yield/resume edge cases;
- debug hooks and stack inspection;
- deterministic scheduling for hosts with repeatable simulation or workflow
  requirements.

When one of those features arrives, add the smallest Ember interface that
preserves Luau behavior, and keep the Go runtime machinery behind that seam.

## Alternatives Considered

- Port Luau's C++ runtime machinery directly. Rejected for now because it would
  front-load allocator, GC, and scheduler complexity before Ember has enough
  compatibility tests or embedding examples to justify it.
- Expose Go implementation types directly everywhere. Rejected because it would
  make the public interface shallow and freeze storage details too early.
- Build a custom VM heap immediately. Rejected because ordinary Go references
  and Go GC already satisfy current table and value lifetime needs.
