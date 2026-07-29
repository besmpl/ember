# ADR 0010: Use Static and Reload-Time Prepared Generations

Status: Superseded by ADR 0011 for reload and generation lifetime. Retained for
static prepared Go and exact Machine replay.

## Context

Static SSA AOT-to-Go became Ember's fastest and most portable execution path.
It also established two durable requirements: a generated bundle must bind the
exact Program and prepared ABI, and unsupported prepared behavior must replay
through the canonical Machine before effects.

The first hot-reload implementation added a same-process prepared slot, a
partial ARM64/x86-64 numeric backend, executable-memory installers, private Go
runtime call mechanics, and an optional Go-plugin adapter. That route proved a
safe-point generation transaction and met its four-row numeric experiment, but
it did not extend static-AOT performance to general Luau behavior. Tables,
host effects, mutable closures, varargs, coroutines, limits, and other behavior
still ran through the much slower Machine.

The resulting architecture duplicated runtime concerns across two ISAs and
accepted executable-memory, private-runtime, plugin/cgo, and unload costs. A
coarse supervised process no longer had the earlier fine-grained-IPC problem
once the host seam became one typed application transaction. ADR 0011 records
that later decision and the evidence that made it real.

## Retained decision

1. **Static generated Go is the portable release path.** A host generates the
   exact loaded Program, compiles the generated package with the application,
   and supplies its immutable bundle through `RuntimeOptions.Prepared`.
2. **Binding fails closed.** Prepared ABI, semantic version, Program hash, and
   module/Proto inventory are checked before runtime-owner mutation. A mismatch
   never silently selects another artifact.
3. **Canonical Machine replay remains complete.** Unsupported Protos or
   guarded runtime values side-exit before effects and replay exact behavior in
   the Machine.
4. **Prepared code is trusted application code.** Generation and compilation
   are explicit host effects; the prepared tier is an optimization and not a
   sandbox.
5. **Cancellation is compiler-safe-point aware.** One opaque prepared context
   is threaded through generated helpers and polled at function entries and
   control-flow backedges. Cancellation-only calls stay prepared and side-exit
   before effects; configured execution limits retain exact Machine accounting.

These clauses continue to own `Program.GeneratePreparedGo`,
`Program.WritePreparedGo`, generated `PreparedBundle` values, and exact runtime
binding.

## Superseded decision

ADR 0011 replaces all of ADR 0010's reload-specific clauses:

- `PreparedRuntimeSlot.Prepare`/`Activate`/`Use` generation ownership;
- reload-time native qualification and ARM64/x86-64 emitters;
- executable mapping, architecture trampolines, and private runtime calls;
- same-process executable-image leases and retirement;
- the optional `preparedplugin` loading path; and
- the claim that a partial numeric tier is the production hot-reload engine.

Those packages, selectors, dependencies, tests, and current documentation were
deleted in the ADR 0011 cutover. Historical four-row measurements remain valid
evidence about the removed experiment, not a current product capability.

## Consequences

- Embedded releases keep the broad, fast generated-Go path without process
  IPC.
- Hot reload uses the same generated-Go opportunity in a supervised worker,
  with explicit detached state and process reclamation rather than executable
  memory inside the host.
- The canonical Machine remains one semantic fallback instead of requiring a
  second complete native runtime.
- Hosts needing synchronous fine-grained Ember callbacks use embedded
  execution; a worker is deliberately a coarse transaction island.

## Alternatives considered

- **Keep the partial same-process native tier:** rejected because its general
  fallback mix could not approach Luau parity and its maintenance/safety costs
  remained those of a second runtime.
- **Go plugin:** rejected because it requires cgo on supported platforms,
  cannot unload, and binds tightly to Go build identity.
- **Whole-process rebuild/re-exec:** still valid when the complete host can
  restart and transfer all process state; it is not transparent editor/game
  reload.
- **One generic loader interface:** rejected because static packages,
  supervised processes, and Machine replay have different trust, state,
  failure, and close semantics.
