# Documentation Inventory

Use the smallest relevant document set for the change you are making.

## Recommended Reading Paths

| Goal | First docs | Notes |
| --- | --- | --- |
| Runtime design | `../README.md`, `principles.md`, `design.md` | Use before changing bytecode, values, VM state, errors, or package shape. |
| Go implementation work | `golang-rules.md`, `checks.md`, nearby package docs | Keep tests focused and public interfaces small. |
| Luau compatibility work | `compatibility.md`, `design.md`, `roadmap.md` | State which upstream behavior is being matched and how it is verified. |
| Hearth embedding work | `hearth-integration.md`, `public-surface.md` | Keep Hearth as an adapter above the runtime until the seam is proven. |
| Larger slices | `exec-plans/README.md`, `exec-plans/_template.md` | Use for work that spans multiple packages or compatibility layers. |

## Core Documents

- `../README.md`: project purpose, scope, and first runtime direction.
- `principles.md`: decision rules for a Go-native Luau-compatible runtime.
- `design.md`: runtime model and early boundaries.
- `roadmap.md`: staged implementation direction.
- `compatibility.md`: how Ember claims and proves Luau compatibility.
- `public-surface.md`: initial import and API surface rules.
- `hearth-integration.md`: host boundary notes for Hearth.
- `golang-rules.md`: Go coding rules for this repository.
- `checks.md`: local verification commands.
- `adr/0001-go-native-runtime-mapping.md`: decision to prefer Go objects,
  Go GC, and Go runtime features behind Ember interfaces.

## Workflow Documents

- `exec-plans/`: optional phased plans for larger vertical slices.
- `adr/`: architecture decision records for compatibility, runtime, and public
  interface decisions.

## First-Contact Rule

Beginner docs should teach the smallest path that runs real behavior. Do not
present parser, compiler, VM, analyzer, JIT, and Hearth embedding as competing
starts. Add public packages, examples, or first-contact concepts only when a
slice proves they reduce the user's decision surface.
