# ADR 0013: Keep Language Semantics Owned and Integrate After Lowering

Status: Accepted. Supersedes ADR 0012 as the multi-language architecture
decision. ADR 0012 remains research and proof history, not an active
specification.

## Context

Ember's Luau implementation, the bounded Ruby and Sprig proofs, and the
independently versioned Seed compiler do not differ only in syntax. They own
different semantic facts, dependency models, diagnostics, object and value
models, control flow, recovery rules, compatibility oracles, generated APIs,
and optimization opportunities.

There is nevertheless useful common work after those decisions have been
made. Generated packages need bounded immutable delivery, applications need
deterministic placement and composition, the Go toolchain needs one explicit
owner, and development reload needs one transaction and generation-lifetime
protocol. Duplicating those effects per language would add policy without
improving language semantics.

The main alternatives were therefore:

1. a universal compiler, semantic IR, runtime, guest value, or registry;
2. completely isolated language products, including build and reload policy;
3. language-owned compilers and runtimes sharing only post-lowering values and
   effects.

The existing proofs support the third alternative. Sprig and Seed compose as
concrete generated packages without a common compiler interface, guest ABI,
package graph, runtime owner, or value representation. Ruby additionally
demonstrates why dynamic-language lookup, identity, visibility, blocks, and
non-local control flow must not be inferred from Luau's runtime.

## Decision

### Keep each language a deep concrete module

Each language owns its:

- source identities, parser, binder or resolver, checker, and diagnostics;
- dependency and public-summary model;
- compatibility scope and oracle;
- semantic facts and private intermediate representations;
- complete general execution and failure behavior;
- optimization, specialization, fallback, and generated ABI; and
- runtime values, mutable owners, limits, cancellation, and cleanup where the
  language requires them.

Language packages expose concrete Go APIs shaped around their real inputs and
products. Ember will not add a public `Language`, `Compiler`, `Backend`,
`Runtime`, `Value`, registry, adaptive planner, or cross-language package graph
as the extension contract.

Compilers are deterministic producers. They do not discover files, select
mounts, invoke the Go toolchain, mutate build caches, publish artifacts, start
processes, register themselves, watch sources, or activate generations. Hosts
finish those effects before or after the compiler call through explicit
owners.

### Share four post-lowering boundaries

Cross-language integration begins only after a language has produced its
concrete buildable contribution:

1. the **Language Producer** owns an immutable, bounded, location-neutral
   prepared source contribution;
2. the **Application** owns placement and statically typed composition of all
   selected contributions;
3. the **Builder** owns filesystem materialization, the Go-toolchain effect,
   verification, content-addressed publication, and the immutable executable
   artifact; and
4. the **Worker** owns transaction durability and the complete lifetime of one
   live generation.

The current carrier for the first two boundaries is
`preparedsource.Set` plus application-owned `preparedsource.Layout`. That
package standardizes generated-source validation, canonical ordering,
identity, and placement; it does not define language semantics, a guest ABI, a
runtime interface, or worker behavior.

`preparedsource` remains provisional until a concrete worker consumer proves
that the same values delete duplicated validation and placement policy at both
the embedded and build boundaries. If that proof fails, the package may be
replaced or removed without reopening the language-ownership decision. The
stable decision is the post-lowering ownership boundary, not the permanence of
one carrier type.

Applications compose concrete generated packages through ordinary imports and
closed typed handlers. Cross-language state, ordering, limits, checkpoint
projection, migration, and effects are application policy. Mutable guest
values, callbacks, frames, closures, owner handles, caches, and language heaps
never cross languages, processes, or generations.

### Use ordinary generated Go without making Go a semantic ABI

Ordinary generated Go is the common static delivery target because it is
portable, cgo-free, inspectable, cacheable by the Go toolchain, and compatible
with supervised process generations. It does not require languages to share a
runtime or value model.

Luau retains its verified SSA/value/shape-specialized prepared backend and
exact Machine replay. Any promoted Ruby implementation must retain Ruby-owned
lookup, visibility, blocks, unwind, identity, epochs, and recovery rather than
relabeling Luau mechanisms. A small functional language may lower mostly to
typed Go and avoid a dynamic runtime. Each language decides which runtime
support its generated package imports.

### Gate deeper sharing on deletion evidence

A shared compiler or runtime mechanism may be proposed only when all of the
following are true:

- at least two production language implementations need the same complete
  operation, not merely similarly named stages;
- the operation has the same types, semantics, ownership, failure behavior,
  and identity inputs for both consumers;
- extracting it deletes more policy and conversion code than it introduces;
- it contains no language tags, generic guest calls, opaque semantic values,
  Ruby- or Luau-specific operations, or fallback dispatch to language code;
- it preserves or improves compile latency, runtime performance, allocations,
  code size, portability, and lifecycle evidence; and
- a focused accepted ADR records the new seam and its replacement conditions.

Until then, keep a mechanism private or duplicate a small operation. Visible
local duplication is preferred to a shallow abstraction that couples every
future language.

## Consequences

- Adding Ruby, Sprig, or another language still requires a real semantic
  implementation and compatibility suite. The architecture does not pretend
  those can be swapped like parsers.
- A third-party language can integrate without changing Ember's semantic core:
  it supplies a concrete compiler and generated package, while the application
  supplies placement and typed bindings.
- Languages share build identity, publication, transaction, reload, and
  generation-lifetime machinery without sharing hot-path values or runtime
  owners.
- Cross-language calls remain explicit application orchestration. There is no
  dynamic language discovery or implicit common object model.
- Some small mechanisms may initially exist more than once. They are extracted
  only when repeated production use proves a deep common operation.
- Moving Luau out of the root package is not implied. Package symmetry is a
  separate change that requires a concrete caller benefit.

## Alternatives considered

- **Universal semantic Core IR:** deferred. Current languages do not yet prove
  a common optimizer, target-independent semantic model, or complete recovery
  operation worth the conversion and coupling cost.
- **Universal runtime or guest `Value`:** rejected because ownership,
  allocation, identity, calls, exceptions, suspension, and mutation differ by
  language and generation.
- **Public compiler/plugin registry:** rejected because static Go composition
  is explicit and type checked, while discovery adds hidden authority and
  lifecycle policy.
- **Completely independent build and reload stacks:** rejected because it
  duplicates effect ownership already proved language-neutral.
- **Mandatory Wasm or foreign-runtime ABI:** rejected as an additional runtime
  and portability contract without evidence that ordinary generated Go is
  insufficient.

## Relationship to earlier ADRs

ADR 0008 continues to own Luau prepared execution and exact Machine replay.
ADR 0011 continues to own build, transaction, process, recovery, activation,
and generation lifetime. This ADR supersedes ADR 0012 only as the
multi-language architecture decision; it does not accept ADR 0012's unfinished
worker-builder, Ruby, Sprig, or compiler-sharing stages.
