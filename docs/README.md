# Documentation Inventory

Use the smallest durable document set for the change you are making.

The docs directory is not a standing plan archive. Do not keep active plans,
benchmark ledgers, speculative roadmaps, or dated strategy notes here by
default. If a slice needs a written plan, keep it short, make ownership clear,
and retire or delete it when the slice lands or is abandoned.

## Recommended Reading Paths

| Goal | First docs | Notes |
| --- | --- | --- |
| First contact | `../README.md`, `principles.md` | Learn what Ember is and which constraints should survive new work. |
| Runtime design | `../README.md`, `principles.md`, `design.md`, `public-surface.md` | Use before changing bytecode, values, VM state, errors, or package shape. |
| Go implementation work | `golang-rules.md`, `checks.md`, nearby package docs | Keep tests focused and public interfaces small. |
| Luau compatibility work | `compatibility.md`, `design.md` | State which upstream behavior is being matched and how it is verified. |
| Host embedding work | `embedding.md`, `public-surface.md` | Start with one explicit module invocation; keep application orchestration in adapters. |
| Durable decisions | `adr/README.md`, relevant ADRs | Use when a decision should outlive the implementation slice that produced it. |

## Core Documents

- `../README.md`: project purpose, scope, and first runtime direction.
- `principles.md`: decision rules for a Go-native Luau-compatible runtime.
- `design.md`: runtime model and early boundaries.
- `compatibility.md`: the test-validated Luau compatibility manifest, levels,
  and explicit non-goals.
- `compiler.md`: arena representation, layout checks, and compiler allocation
  gates.
- `public-surface.md`: initial import and API surface rules.
- `embedding.md`: generic host invocation, dispatch, typed catalogs, and
  cooperative suspension; Hearth appears only as a first-host case study.
- `prepared.md`: static prepared-Go generation, typed transactional worker
  reload, exact Machine replay, and explicit artifact/runtime binding.
- `golang-rules.md`: Go coding rules for this repository.
- `checks.md`: local verification commands.
- `adr/0001-go-native-runtime-mapping.md`: public Go ownership decision; ADR 0007
  supersedes its ordinary-Go-object default for hot internal state.
- `adr/0004-runtime-slot-handle-ownership.md`: private runtime slots, typed
  handle ownership, external pins, and the earlier collector design now
  partially superseded by ADR 0007.
- `adr/0006-persistent-table-allocation-gate.md`: evidence for stopping the
  conditional table allocator campaign after persistent workload profiling.
- `adr/0007-compact-production-machine.md`: accepted owner-neutral `CodeImage`,
  owner-bound scalar `Machine`, pure-Go kernel, and production migration; ADR
  0008 supersedes its final-backend and performance-target clauses.
- `adr/0008-ssa-aot-for-luau-parity.md`: accepted prepared SSA AOT-to-Go
  architecture, exact Machine side exits, lifecycle claim split, and candidate
  rejection evidence.
- `adr/0009-generated-adaptive-superword-vm.md`: rejected bounded dynamic-VM
  experiment, exact four-family failure evidence, transferable mechanisms,
  and completed deletion gate.
- `adr/0010-hybrid-aot-generation-reload.md`: retained static prepared release
  and exact Machine replay clauses; ADR 0011 supersedes its reload design.
- `adr/0011-supervised-aot-worker-generations.md`: accepted no-cgo supervised
  static-AOT workers, typed transaction semantics, recovery, and generation
  lifetime.
- `adr/0012-language-owned-runtimes-and-staged-prepared-sharing.md`: superseded
  research and proof history for the later focused multi-language decisions.
- `adr/0013-language-owned-compilers-and-post-lowering-integration.md`: accepted
  language ownership and narrow post-lowering integration boundary.
- `adr/0014-layer-compilation-and-delivery-identities-by-owner.md`: accepted
  layered identity and invalidation ownership across compiler, generated,
  build, and launch products.
- `adr/0015-require-receipts-for-architecture-promotion.md`: accepted
  receipt-gated compatibility, portability, performance, and lifecycle
  promotion policy.

## Workflow Documents

- `adr/`: architecture decision records for compatibility, runtime, and public
  interface decisions.
- `../performance-audit.md`: current reproducible performance evidence and
  noise envelope.
- `../static-aot-transactional-worker-complete-cutover-implementation-plan.md`:
  active worker-cutover delivery contract. Its seven ordered S0 stages cover
  stabilization, Luau prepared replay, AOT recertification, owner-framed
  identities, embedded durability, EPW2 lifecycle, and final admission; stages
  8-17 and the functional/3D language remain gated.

Historical performance execution plans are intentionally not part of the
navigation. Their durable decisions remain in the performance audit and ADRs;
the retired files are available through Git history for archaeology.

## First-Contact Rule

Beginner docs should teach the smallest path that runs real behavior. Do not
present parser, compiler, VM, analyzer, JIT, and host embedding as competing
starts. Add public packages, examples, or first-contact concepts only when a
slice proves they reduce the user's decision surface.
