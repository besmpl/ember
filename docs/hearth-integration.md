# Hearth Integration

Hearth is Ember's first intended host, but Ember should not require Hearth to
run.

## Host Boundary

Keep Hearth at an adapter seam above the runtime. The core runtime should
operate on plain Go values, bytecode, state, and host callbacks. A Hearth
adapter can later map scripts to worlds, schedules, entities, resources, and
messages.

## Early Integration Goals

- Run a script in a headless test.
- Call a Go host function from script.
- Pass simple values across the host seam.
- Let a script request an explicit Hearth action through an adapter.
- Keep world mutation owned by Hearth, not hidden in the VM.

## Non-Goals For Early Integration

- Rendering, audio, assets, editor tooling, networking, or platform lifecycle.
- Hidden global script state.
- Background script workers without explicit ownership and cancellation.
- Direct exposure of Hearth internals as untyped script blobs.

## Design Pressure

The Hearth adapter should prove what the embedding interface needs. It should
not force the root runtime to know about engine concepts before scripts can
actually run.
