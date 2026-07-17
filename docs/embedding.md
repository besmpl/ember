# Host Embedding

Ember is an embeddable Luau runtime. It owns script execution, module caching,
callbacks, limits, and continuations; an application adapter owns domain
objects, I/O, scheduling, clocks, and lifecycle policy.

## Generic Invocation Seam

`Runtime.Invoke` is the smallest host-neutral program interface. An
`Invocation` names one program module, optionally names one exported function,
and supplies explicit globals:

```go
values, err := runtime.Invoke(ctx, ember.Invocation{
    Module: ember.LogicalModule("workers/thumbnail"),
    Export: "process",
    Globals: map[string]ember.Value{
        "store": ember.ContextHostFuncValue(store),
    },
}, ember.StringValue("image-7"))
```

If `Export` is empty, Ember invokes the module's return value directly. This
supports both `return function(...) ... end` modules and modules that return a
table of named functions. A missing module, missing named export, or target that
is not a script function is an error; the runtime does not invent skip policy.

The supplied globals are copied. The first successful initialization of a
module determines its cached export, exactly as `require` does; later
invocations do not rerun top-level module code with a different globals map.
Required modules execute in the same explicit environment.

This seam deliberately has no entrypoint cohort, broadcast order, lifecycle
name, or per-script report. A request router, build tool, document processor,
game engine, or another host can apply its own orchestration above repeated
single-module invocations.

## Optional Cohort Dispatch

`Runtime.Dispatch` is a convenience for hosts whose configured entrypoints all
return table-or-nil and share a named operation. It initializes entrypoints in
declared order, calls the named function where present, and reports loaded,
called, and skipped entrypoints. `RuntimeHost.Globals` supplies the per-load and
per-operation environments for this convenience.

That fan-out, ordering, and skip behavior is adapter policy, not a requirement
of `Invoke`. Hosts that do not naturally have lifecycle cohorts should not use
it.

`RunHook`, `RunHookResumable`, `HookReport`, `HookCallReport`,
`HostCall.Hook`, `ExecutionResult.Hook`, and `Suspension.Hook` are deprecated
compatibility names. New code uses dispatch/operation vocabulary.

## Typed Host Catalogs

Any host can project its own descriptors into `TypeSummary` values and pass
them through `AnalysisConfig`. Ember copies those facts and understands generic
table properties, access modifiers, indexers, metatable `__index` inheritance,
table-valued function returns, and intersection overloads selected by singleton
arguments. Ember has no built-in class, service, entity, route, task, or object
model names.

`LoadProgram` checks dependencies before consumers. A consumer receives only a
dependency summary produced without diagnostics during the same load, so an
untrusted module remains conservative instead of lending false precision.
Program diagnostics carry module and source identity plus byte ranges for a
host to map through source transforms.

## Cooperative Suspension

A host exposes a wait-shaped call with `ResumableHostFuncValue` and
`HostSuspend(token)`. The token belongs to the host. Ember retains only the
script continuation; it starts no worker, timer, channel, retry loop, or
scheduler.

`InvokeResumable` and `Callback.CallResumable` return one invocation's values
or pending continuation. `DispatchResumable` returns a quiescent cohort snapshot
for hosts that intentionally choose dispatch policy. In every case, the host
chooses when and in what order to call `Resume`, `Fail`, or `Cancel`.

Top-level code and newly required module initializers may suspend. A module has
one in-flight initializer and one cached result. If an independent invocation
reaches an initializer owned by another invocation, Ember returns a tokenless
continuation for the dependent operation. `Suspension.Ready` becomes true when
the initializer can advance; early use returns `ErrSuspensionPending` without
consuming the handle.

Cancellation is idempotent. Canceling an initializer owner aborts its private
dependents without running guest code; unrelated public waits remain valid and
a later invocation may initialize the module again. `Runtime.Close` invalidates
all pending handles and releases retained frames, roots, callback references,
module initialization state, and host tokens.

## Hearth Case Study

Hearth is Ember's first production host. Its adapter maps DataModel scripts to
entrypoints, lifecycle methods to dispatch operations, engine descriptors to
typed catalogs, and task waits to host-owned suspension tokens. Worlds,
entities, resources, rendering, networking, and frame scheduling remain in
Hearth.

`TestHearthShapedEmbeddingContract` is the explicit first-host acceptance test.
The generic request-middleware and batch-processor tests independently prove
`Invoke`, explicit globals, direct and named exports, return values, suspension,
shared module initialization, and VM/Machine parity without Hearth concepts.

## Non-Goals

- Domain object models or service locators in the root package.
- Implicit files, networking, clocks, randomness, or platform lifecycle.
- Background script workers without explicit host ownership and cancellation.
- Untyped exposure of an application's internal state.
