# ADR 0010: Use Static and Reload-Time Prepared Generations

Status: Accepted

## Context

Static prepared SSA AOT-to-Go is Ember's fastest and most portable execution
path, but it can compile only source known before the application build. Hot
reload needs changed source to become compiled code while the current Runtime
continues serving the host.

The generation transaction and the code-installation mechanism are separate
problems. `PreparedRuntimeSlot` already provides the honest transaction:
prepare an inert exact generation, activate only at an idle host safe point,
scope use to one generation, and close old owner-bound state. The missing piece
was a no-cgo compiled-code mechanism for a Program unknown at the original Go
build.

Measured candidates included a content-addressed Go plugin, rebuild/re-exec,
an external prepared worker, several whole-Go and direct Wasm designs, dynamic
VM/superword/SSA-region designs, and a small native capsule. The Wasm variants
missed the required 1.5x Luau row after materially different lowerings. Dynamic
specialization did not transfer generally. Plugins require cgo and cannot
unload. A process boundary gives clean reclamation but turns fine-grained host
interaction into IPC.

The native capsule was initially rejected because a leaf proof did not justify
executable memory, private runtime ABI, two ISAs, or reclamation machinery. It
became justified only after safer architectures failed the frozen performance
gate and a complete generation-owned implementation proved those obligations.

## Decision

Ember uses three explicit prepared modes:

1. **Static generated Go** is the portable release path. The host calls
   `Program.WritePreparedGo`, compiles the generated package with the
   application, and passes its exact bundle through `RuntimeOptions.Prepared`.
2. **Reload-time native preparation** is the same-process no-cgo hot-reload
   path. Calling `PreparedRuntimeSlot.Prepare` with a nil
   `RuntimeOptions.Prepared` lowers the exact changed Program into an inert
   native generation before activation.
3. **Canonical Machine replay** remains complete. Unsupported platforms,
   Protos, runtime values, or static-call dependency graphs receive nil bundle
   entries and replay from the entry before effects.

The optional `preparedplugin` package remains a separate host adapter for
applications that deliberately accept Go plugin and cgo constraints. It is not
the default reload mechanism and does not share native-generation lifetime
semantics.

### Semantic and ISA seams

One target-independent native candidate builder owns semantic admission. The
current subset contains pure numeric constants, arithmetic, comparisons,
branches, loops, direct static calls, bounded self-recursion, and immutable
module-private numeric captures, with at most eight explicit plus hidden
arguments and one numeric result.

ARM64 and x86-64 emitters consume that candidate. Each emitted function has a
Go-facing pointer/count/result adapter and a private register fast-call body.
Native-to-native calls use D0-D7 or XMM0-XMM7 and return through D0/XMM0 plus
X0/RAX status. ISA code owns instruction selection and relocation only; it
cannot change language qualification.

Tables, host effects, mutable or escaping closures, varargs, coroutines, and
unproved operations remain canonical. A runtime value mismatch requests replay
before the native body performs an effect.

### Preparation and activation

`PreparedRuntimeSlot.Prepare` is explicitly expensive. It builds backend IR,
emits and validates the native artifact, installs executable mappings, binds an
exact `PreparedBundle`, and constructs the Runtime while the active generation
remains usable. It performs no guest execution.

`Activate` performs no compilation, mapping, file I/O, source loading, or guest
work. It rejects busy, stale, foreign, used, and closed candidates, closes the
old Runtime, publishes the candidate, and retires the old executable images.
`Use` serializes one host batch against a stable generation.

Successful activation starts fresh script-owned state. World state, files,
clocks, networking, and other application objects remain host-owned. Raw
tables, closures, callbacks, frames, and suspensions never cross generations;
application state transfer uses an explicit detached schema.

### Native boundary and ownership

The executable-memory and private-runtime edge is confined to
`internal/preparednative`.

- One generation owns all module mappings and the Go closures that reference
  their entries.
- Calls hold read leases; close waits for them before unmapping.
- Darwin mappings use `MAP_JIT`, serialized per-thread write windows, and
  instruction-cache invalidation before publication.
- Linux mappings are populated read-write and sealed read-execute before
  publication; they are never writable and executable simultaneously.
- Windows mappings use `VirtualAlloc` read-write storage, seal it
  read-execute with `VirtualProtect`, flush the process instruction cache, and
  release it with `VirtualFree`. Dynamic-code policy rejection selects the
  canonical Machine fallback before effects.
- A tiny architecture-specific trampoline enters generated code on the Go
  runtime's foreign/system stack. Windows x86-64 translates its distinct outer
  ABI and preserves the nonvolatile registers touched by Ember's private
  kernel ABI; Windows ARM64 shares the existing register contract.
- Generated code may read argument and result pointers only for the duration of
  the call and cannot retain Go pointers.
- The repository's pure-Go scanner allowlists only the exact `purego`,
  executable-memory, assembly-call, and `runtime.cgocall` sites.

`CGO_ENABLED=0` is required and tested. This path nevertheless depends on one
private Go runtime ABI: cgo-disabled Unix builds initialize it through
`purego`, while Windows provides the transition directly. Toolchain upgrades
must keep the focused boundary, race, checkptr, and linked-symbol tests green.

### Platform support

Native installation is supported on Darwin, Linux, and Windows for ARM64 and
x86-64; x86-64 requires SSE4.1. Both emitters and complete execution paths are
tested with cgo disabled. Hosts whose dynamic-code policy rejects executable
pages retain the exact Machine fallback.

## Consequences

- Changed Luau source can become compiled same-process code without invoking
  the Go toolchain, using cgo, or pausing the active generation for compilation.
- Static prepared Go remains the release default and can cover a broader
  prepared subset without executable-memory policy.
- Native coverage is deliberately partial. Performance is a property of the
  qualified numeric region plus the host's real fallback mix, not a blanket
  claim about arbitrary Luau.
- The accepted four-row production-path capture measures median/worst ratios of
  0.688620/0.692415 for arithmetic `for`, 0.280739/0.286104 for branching,
  0.167772/0.176960 for captured recursion, and 1.268322/1.290334 for iterative
  Fibonacci against pinned Luau 0.728.
- Thirty-two repeated generation swaps reclaim each prior mapping and old
  handles reject calls. A longer resident-memory soak remains a production
  hardening item.
- Unsupported mutable captures also proved the fallback itself: a shared
  optimizer bug that removed conditional captured writes was repaired in the
  common compiler, and VM, Machine, and slot replay now agree.
- Executable memory, private runtime ABI, and per-ISA maintenance are real
  production costs. They remain behind one deep internal module rather than
  leaking into `Runtime`, backend IR, or host adapters.

## Alternatives considered

- **Continue only with static generated Go:** retained for releases, but it
  cannot admit source unknown at the original build.
- **Go plugin:** retained as an optional adapter; rejected as the no-cgo default
  because it requires cgo, has exact Go build-identity constraints, and cannot
  reclaim loaded code.
- **Rebuild/re-exec or prepared worker:** valid host fallbacks with clean code
  reclamation; rejected as transparent game hot reload because process-local
  state and fine-grained host calls need explicit handoff or IPC.
- **Prepared Wasm capsule:** rejected after whole-Go, direct-CFG, structured,
  and semantics-complete variants measured about 2.14x, 4.1x, 2.44x, and 3.53x
  Luau on the blocking row.
- **Generated superword VM or adaptive SSA regions:** rejected because measured
  wins were workload- or benchmark-zone-specific and did not provide a general
  parity architecture.
- **One generic loader interface:** rejected because static packages, plugins,
  processes, native mappings, and Machine replay have different trust, failure,
  close, and state semantics.
