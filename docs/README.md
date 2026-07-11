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
| Hearth embedding work | `hearth-integration.md`, `public-surface.md` | Keep Hearth as an adapter above the runtime until the seam is proven. |
| Durable decisions | `adr/README.md`, relevant ADRs | Use when a decision should outlive the implementation slice that produced it. |

## Core Documents

- `../README.md`: project purpose, scope, and first runtime direction.
- `principles.md`: decision rules for a Go-native Luau-compatible runtime.
- `design.md`: runtime model and early boundaries.
- `compatibility.md`: how Ember claims and proves Luau compatibility.
- `public-surface.md`: initial import and API surface rules.
- `hearth-integration.md`: host boundary notes for Hearth.
- `golang-rules.md`: Go coding rules for this repository.
- `checks.md`: local verification commands.
- `adr/0001-go-native-runtime-mapping.md`: decision to prefer Go objects,
  Go GC, and Go runtime features behind Ember interfaces.
- `adr/0004-runtime-slot-handle-ownership.md`: private runtime slots, typed
  handle ownership, external pins, and the first stop-the-world collector.

## Workflow Documents

- `adr/`: architecture decision records for compatibility, runtime, and public
  interface decisions.

## First-Contact Rule

Beginner docs should teach the smallest path that runs real behavior. Do not
present parser, compiler, VM, analyzer, JIT, and Hearth embedding as competing
starts. Add public packages, examples, or first-contact concepts only when a
slice proves they reduce the user's decision surface.
