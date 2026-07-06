# Execution Plans

Use an execution plan for work that spans multiple packages, compatibility
layers, or verification gates.

Keep plans short, concrete, and easy to retire after the slice lands.

## Active Plans

- `compiler-seed.md`: first tiny source-to-bytecode compiler path.
- `compiler-runtime-optimization.md`: ambitious parser, compiler, bytecode,
  VM, runtime table, and typed-analysis optimization plan.
- `fast-execution-artifact.md`: ambitious no-codegen performance plan for the
  verified interpreter artifact.
- `hearth-runtime-integration.md`: core Program/Runtime design for embedding
  Ember in Hearth through a deep host adapter seam.
- `internal-legacy-cleanup.md`: breaking-change cleanup plan for deleting
  private legacy seams after Program/Runtime, lowering, VM, table, and typed
  analysis modules have deepened.
- `scenario-general-performance.md`: blank-page Scenario performance plan
  focused on general direct-frame, table, iteration, call, and numeric
  mechanisms.
- `type-system.md`: MVP Luau-shaped typed-analysis system in phased slices.

## Historical Plans

- `scenario-performance.md`: superseded by
  `scenario-general-performance.md`; kept for benchmark-shaped artifact removal
  history and prior Scenario ratio evidence.
