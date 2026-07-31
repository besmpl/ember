# ADR 0015: Require Receipts for Architecture Promotion

Status: Accepted

## Context

Compiler and runtime mechanisms can look elegant while moving cost into
preparation, artifacts, fallback, memory, or cleanup. Ember has already seen a
generated adaptive-superword design win scalar cases yet fail object-heavy and
allocation gates. Direct-Go and Ruby proofs likewise demonstrate mechanism
potential without establishing universal production performance.

Cross-compilation, workflow definitions, benchmark design, qualitative
ratings, and narrow proof programs establish different facts. Treating any of
them as completed portability or performance evidence would make architecture
claims drift faster than the implementation.

## Decision

Architecture mechanisms and compatibility claims are promoted only through a
fail-closed evidence ledger. The order is fixed:

1. freeze the exact source, toolchain, environment, limits, target,
   compatibility scope, workload inventory, and candidate identity;
2. pass semantic, ownership, cancellation, and lifecycle preflight across all
   applicable execution paths;
3. pass source and linked-binary pure-Go policy checks;
4. acquire separate cgo-disabled cross-build and target-native lifecycle
   receipts for every claimed platform pair;
5. acquire fresh independent timing, allocation, memory, artifact, coverage,
   and lifecycle captures; and
6. run the repository comparator, which writes PASS only after the complete
   inventory clears its predeclared gates.

Missing evidence yields **Incomplete**, not PASS. A semantically wrong,
effect-before-exit, stale, unbounded, or hard-threshold-failing candidate is
**Rejected**, not **Incomplete**.

### Keep claims finite

The maintained compatibility manifest and its named behavior tests define
Ember's Luau claim. The pinned runtime corpus is a differential and performance
instrument, not a substitute specification. Ruby claims remain limited to the
explicit frozen frontier and pinned CRuby evidence. A future language owns its
own compatibility oracle and may not inherit a claim from the delivery
mechanism.

### Measure distinct clocks and resources

Runtime throughput does not stand in for compiler or reload performance.
Captures report separately:

- lex, parse, bind, check, emit, optimize, assemble/seal, compile, and program
  loading;
- prepared generation, materialization, Go build, publication, launch,
  attestation, restore, migration, warmup, and catch-up;
- activation, which must remain a bounded no-I/O/no-guest route swap;
- steady execution, replay, fallback coverage, and transport;
- bytes and allocations per operation, retained and peak memory, RSS, code and
  artifact size; and
- swaps, child counts, retirement, journal release, and cleanup.

Comparators preserve the acquisition design. When implementations are
order-rotated inside the same repeat, that repeat is a block and ratios compare
only matched observations from that block; Cartesian recombination is reserved
for genuinely independent samples. Otherwise host or frequency drift between
blocks becomes a hidden gate unrelated to the implementation effect. A repair
to the acquisition or comparator contract is frozen before a fresh candidate
capture, and every artifact acquired under the replaced contract remains
diagnostic rather than being reinterpreted as promotion evidence.

The authoritative commands, numeric thresholds, sampling rules, and capture
formats live in `docs/checks.md` and the repository scripts. This ADR records
the decision rule rather than duplicating values that would drift.

### Separate build portability from native behavior

Every supported Darwin, Linux, and Windows ARM64/x86-64 pair requires two
different receipts:

- a `CGO_ENABLED=0` cross-build proving compilation; and
- target-native execution proving semantics, artifact opening, process
  ownership, reload, retirement, and cleanup.

Unavailable native runners leave that target incomplete. CI configuration or a
successful cross-build is never reported as native proof.

### Treat gates as replacement triggers

Thresholds, workloads, and comparison rules are frozen before candidate
acquisition. A repeated family-level miss replaces or removes the responsible
mechanism; it does not invite post-hoc workload relabeling, threshold
weakening, or hidden fallback.

The same rule governs architecture seams:

- remove or replace `preparedsource` if the worker consumer does not prove
  deletion of duplicated policy at both effect boundaries;
- do not promote shared compiler/runtime machinery without concrete multiple
  consumers and a deletion proof;
- do not delete the VM until the Machine covers every maintained unprepared
  behavior and an independent semantic oracle remains; and
- do not expand Luau, Ruby, or platform claims without retained receipts at
  the expanded boundary.

## Consequences

- Architecture promotion is slower than accepting a persuasive benchmark, but
  accepted claims remain reproducible and scoped to observed behavior.
- Preparation, fallback, memory, code size, and lifecycle costs cannot be
  hidden behind a fast steady-state number.
- Runner scarcity and noisy hosts remain visible as incomplete evidence rather
  than silently weakening portability claims.
- Mechanisms stay replaceable: the observable contract and gates are stable,
  while the implementation may change when a better candidate clears them.
- Research documents and proof fixtures remain evidence, not authority, until
  the corresponding decision is accepted and maintained docs are updated.

## Alternatives considered

- **Architecture ratings or expert judgment:** useful for selecting candidates,
  rejected for promotion because they are not measurements.
- **Microbenchmarks alone:** rejected because they omit semantic coverage,
  preparation, memory, fallback, artifacts, and lifecycle.
- **Cross-build-only portability:** rejected because it cannot prove native
  process, publication, reload, or cleanup behavior.
- **Absolute wall-clock budgets on arbitrary hosts:** rejected for architecture
  comparison; paired pinned captures and frozen noise handling are required.
  Product SLAs may define additional absolute budgets.
- **Tune gates after seeing candidate results:** rejected because it makes the
  benchmark part of the implementation and destroys falsifiability.

## Relationship to earlier ADRs

ADRs 0005, 0006, and 0009 established mechanism deletion after bounded
performance failures. ADRs 0008 and 0011 established semantic, exact-replay,
portability, and lifecycle gates for prepared execution. This ADR makes that
fail-closed evidence rule the common promotion policy without changing the
numeric thresholds or compatibility scopes owned by maintained documents.
