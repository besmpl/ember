# ADR 0014: Layer Compilation and Delivery Identities by Owner

Status: Accepted

## Context

Ember has several lawful reuse boundaries: source parsing and binding, typed
analysis under an environment, sealed bytecode, prepared generation,
application placement, Go builds, published executables, and worker launch.
The same source text may be reusable at one boundary and stale at another. A
single project digest would be sound only by invalidating too much, while a
source-only cache key would be unsound for environment-dependent facts.

The architecture also needs small edits to avoid unnecessary frontend work
without weakening final executable identity. An implementation-only edit may
leave a module's consumer-visible facts unchanged even though the final
program and build must still change.

## Decision

### Give each boundary its own identity

The owner of an immutable product frames every input that can affect that
product and exposes or retains the resulting identity with it. Identities are
domain-separated, canonically ordered, length-framed where ambiguity is
possible, and derived from immutable values.

The pipeline is deliberately layered. A concrete language may omit a layer it
does not need, but it may not merge identities whose products have different
inputs or owners.

1. **Source artifact identity** covers the language/frontend ABI, source name
   and bytes, mode or compatibility target, and semantic frontend options. A
   cache hit revalidates stored source metrics against the caller's current
   limits; policy-only limits do not multiply identical syntax artifacts.
2. **Project-check identity** additionally covers the analyzer configuration,
   type environment, require bindings, and exact public module-summary facts
   consumed by checking. Environment-dependent facts never live under the
   neutral source key.
3. **Compiled-language identity** covers the compiler ABI, source artifact,
   semantic compile options, and only checked facts actually consumed by
   lowering. A whole-program specialization is a separate optional product
   whose key covers its complete closed-world inputs; correctness never
   depends on it.
4. **Program identity** covers the program-image ABI, canonical module and edge
   inventory, exact compiled-module identities, and ordered entrypoints. A
   language without Ember's `Program` owns an equivalent identity for its
   complete executable semantic product.
5. **Prepared contribution identity** covers the prepared/backend ABI,
   semantic version, verified Program or equivalent language product,
   generation options, and exact canonical generated content.
6. **Application layout and contract identities** cover placement and the
   closed request/result/checkpoint/effect schemas, codecs, and bounds.
7. **Build identity** covers every pre-link input that can change the
   executable: generated and wrapper source, selected dependency source
   closure, module graph and admitted replacement chains, toolchain, target,
   tags, flags, build policy, contract, prepared ABI, semantics, and relevant
   program inventory.
8. **Executable and launch identities** separately cover published bytes and
   the exact verified build/executable pair admitted to a generation.

No opaque universal generation digest replaces these identities. A top-level
descriptor may reference them, but it must not hide which owner or invalidator
changed.

### Invalidate downstream, reuse upstream

A changed input invalidates its owning product and every product that consumes
it. Products earlier than that boundary remain reusable. In particular:

- a private implementation edit reuses the unaffected modules' source
  artifacts and may stop dependent **checking** after the edited module's
  consumer-visible summary is proved unchanged;
- the edited module's compiled product, Program, generated contribution, and
  final build still change when their consumed content changes; and
- an exported fact or dependency-edge change rechecks affected consumers in
  deterministic dependency order until their public summaries stabilize.

The source artifact store remains a neutral artifact owner, not a project
graph orchestrator. If graph-aware incremental checking is added, an explicit
operation- or host-owned project scheduler owns the ephemeral reverse index,
bounded parallelism, cancellation, and deterministic publication. The exact
module-summary schema and propagation algorithm require differential
invalidation tests before implementation; this ADR does not freeze them.

### Prove both misses and reuse

Every cache boundary needs structural tests for:

- an identical request hitting every eligible layer;
- each relevant input causing a miss at its owner and downstream consumers;
- an unrelated input not invalidating lower independent layers;
- limit revalidation on hits;
- canonical ordering and collision resistance; and
- failed, cancelled, partial, or drifted work never being published as a hit.

Digest equality alone is not evidence of correct minimal invalidation.

### Keep downstream correctness broader than latency optimization

Build identity is not weakened to improve edit-to-ready latency. Changed builds
use the Go toolchain's measured incremental cache path; they are not relabeled
as exact artifact hits. Optimization work belongs in reusable upstream
products, stable package layout, and explicit critical-path measurement.

Persistent compiler caches, fine-grained AST/node caches, and cross-language
graph services are not part of this decision. They require a proved dominant
cost plus explicit format versioning, byte bounds, eviction, cancellation,
ownership, and shutdown contracts.

## Consequences

- Identical source can safely reuse parsing and binding across different type
  environments without reusing stale checked facts.
- Private edits can avoid unnecessary dependent analysis while still producing
  a correctly distinct executable.
- More identities exist, but each is narrow, inspectable, and owned by the
  layer whose reuse it controls.
- Hashing cost must be measured and amortized by immutable products; mutable
  environments and syntax graphs are not repeatedly serialized merely to make
  cache keys.
- Build and launch remain fail-closed even when frontend incrementality is
  aggressive.

## Alternatives considered

- **One whole-project cache key:** rejected as the default because every edit
  becomes a project miss and independent source work cannot be reused.
- **Source identity for checked facts:** rejected as unsound because analysis
  depends on environments and dependency summaries.
- **Put limits in every identity:** rejected for policy-only limits; cached
  metrics are revalidated instead. A limit that changes semantics belongs in
  the relevant identity.
- **One universal invalidation graph across languages:** rejected because
  dependency and public-fact semantics remain language-owned.
- **One opaque generation digest:** rejected because it obscures ownership,
  encourages under-invalidation, and prevents precise reuse diagnostics.

## Relationship to earlier ADRs

ADR 0011 owns the complete worker build and executable admission identity.
ADR 0013 owns language isolation and the post-lowering integration boundary.
This ADR refines identity and invalidation across those owners; it does not
authorize a persistent cache, a cross-language project graph, or a new public
compiler interface.
