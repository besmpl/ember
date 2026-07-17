# Hearth Integration

Hearth is Ember's first intended host, but Ember should not require Hearth to
run.

## Host Boundary

Keep Hearth at an adapter seam above the runtime. The core runtime should
operate on plain Go values, bytecode, state, and host callbacks. A Hearth
adapter can later map scripts to worlds, schedules, entities, resources, and
messages.

## Early Integration Goals

- Run a script in a headless test. Seeded in the root package.
- Call a Go host function from script. Seeded through `RunWithGlobals`.
- Pass simple values across the host seam. Seeded for nil, booleans, numbers,
  and strings.
- Let a script request an explicit Hearth action through an adapter.
- Keep world mutation owned by Hearth, not hidden in the VM.

## Typed Host Catalogs

Hearth can project its canonical descriptors into `TypeSummary` values and
pass them through `AnalysisConfig`. Ember copies those facts and understands
generic table properties, access modifiers, indexers, metatable `__index`
inheritance, table-valued function returns, and intersection overloads selected
by singleton arguments. Ember has no built-in class, service, `Instance.new`,
or `GetService` names.

`LoadProgram` checks dependencies before consumers. A consumer receives only a
dependency summary produced without diagnostics during the same load, so an
untrusted module remains conservative instead of lending false precision.
Program diagnostics carry module and source identity plus byte ranges for
Hearth to map through any source transform.

## Cooperative Wait Ownership

Hearth can expose a wait-shaped operation with `ResumableHostFuncValue` and
`HostSuspend(token)`. The token belongs to Hearth. Ember retains only the
script continuation needed to resume the call; it does not start a worker,
clock, timer, channel, retry loop, or scheduler.

`RunHookResumable` and `Callback.CallResumable` return a quiescent snapshot:
all entrypoints that can advance have advanced, completed calls are reported in
declared order, and `ExecutionResult.Suspensions` contains every host-visible
wait still pending. Hearth may resume those waits in its own deterministic
order. `ExecutionResult.Suspension` aliases the first wait for compatibility
with hosts that drive one continuation at a time.

Top-level entrypoint code and newly required module initializers can wait too.
Entrypoint roots and `require` use the same initialize-once record, so a later
entrypoint requiring a suspended earlier entrypoint observes one initializer,
one side-effect sequence, and one cached export. Independent initializers may
remain suspended together and finish in Hearth-selected order. Dependency
handoffs stay internal to Ember rather than becoming duplicate Hearth tokens;
recursive require paths still report the complete cycle. Resuming an
initializer pumps ready dependents in declared entrypoint order before Ember
returns the next quiescent snapshot.

Hearth updates its world or observes another event, then calls `Resume` with
values or `Fail` with an ordinary error. `Cancel` is idempotent. Canceling a
plain hook or callback wait abandons that invocation; canceling the public
owner of a module initializer also aborts private continuations blocked on that
initializer, without running guest code. Independent public waits stay valid,
and a later call may initialize the canceled module cleanly. Each step gets a
fresh context and execution budget and can suspend again. Context validation
and runtime admission precede consumption under one serialization decision, so
a pre-canceled context or busy runtime leaves the exact handle valid for retry.

Successfully resumed or failed suspension handles are single-use. Repeated
`Cancel` succeeds, while `Resume` or `Fail` after cancellation is stale.
`Runtime.Close` invalidates pending handles and releases their frames, roots,
callback references, module initialization state, and host tokens. Runtime
errors still report script frames after resume; protected calls receive a
failed resume as an ordinary protected error on both execution engines.

`TestHearthShapedEmbeddingContract` is the host-neutral public proof. It loads
a strict factory catalog and, on both runtime engines, combines simultaneous
top-level and required-module waits, an independently completing entrypoint, a
shared initializer, host-selected resume order, initializer cancellation and
retry, a typed `WaitForChild`-shaped hook wait, a repeated hook wait, and close
invalidation. The named check is run with cgo disabled.

## Non-Goals For Early Integration

- Rendering, audio, assets, editor tooling, networking, or platform lifecycle.
- Hidden global script state.
- Background script workers without explicit ownership and cancellation.
- Direct exposure of Hearth internals as untyped script blobs.

## Design Pressure

The Hearth adapter should prove what the embedding interface needs. It should
not force the root runtime to know about engine concepts before scripts can
actually run.
