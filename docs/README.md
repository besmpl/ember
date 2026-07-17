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
- `prepared.md`: deterministic prepared-Go generation, the `emberc` manifest,
  freshness checking, and explicit runtime binding.
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
- `adr/0009-generated-adaptive-superword-vm.md`: bounded dynamic-VM decision,
  owner-local shadow-wordcode seam, semantic-generation requirement, and
  early delete-or-retain gate.

## Workflow Documents

- `adr/`: architecture decision records for compatibility, runtime, and public
  interface decisions.
- `../performance-audit.md`: current reproducible performance evidence and
  noise envelope.
- `../luau-parity-no-cgo-ssa-aot-implementation-plan.md`: active Simple Loop
  delivery contract for the prepared parity architecture.
- `../runtime-speed-2x-no-cgo-production-migration-implementation-plan.md`:
  retained background for the compact Machine migration. ADR 0008 and the
  prepared parity plan supersede its final-backend and final-target direction.

Historical performance execution plans are intentionally not part of the
navigation. Their durable decisions remain in the performance audit and ADRs;
the retired files are available through Git history for archaeology.

## First-Contact Rule

Beginner docs should teach the smallest path that runs real behavior. Do not
present parser, compiler, VM, analyzer, JIT, and host embedding as competing
starts. Add public packages, examples, or first-contact concepts only when a
slice proves they reduce the user's decision surface.
