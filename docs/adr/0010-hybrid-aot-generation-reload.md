# ADR 0010: Use Hybrid Prepared-Generation Reload

Status: Accepted

## Context

Prepared SSA AOT-to-Go is Ember's only proven path comfortably inside the
Luau throughput target. It emits ordinary compiled Go functions, so source
unknown at the host's original build cannot enter that process as data alone.
The problem is native code installation, not parsing or lowering.

Four installation families were compared under the same priorities: prepared
throughput, changed-source capability, atomic failure, state ownership,
portability, unload behavior, reload latency, and interface locality.

| Family | Strength | Blocking liability |
| --- | --- | --- |
| Rebuild and re-exec | Portable compiled Go and clean reclamation | Not same-process; host state handoff is application-specific |
| Content-addressed Go plugin | Same-process direct prepared functions using the existing generator | cgo and supported Unix only; exact build identity; cannot unload |
| Integrated native JIT | Same-process code with custom backends | Executable memory, ABI, stack-map, unwind, and platform obligations |
| Portable data artifact and generic kernel | Same-process, pure-Go, reclaimable | Executes as another interpreter/VM and loses the proven AOT advantage |

Standard Go does not provide unknown-at-build, same-process, portable,
unloadable, no-cgo native code simultaneously. A single loader abstraction
would also be dishonest: static packages, plugins, replacement processes, and
JIT memory have different trust, close, and state semantics.

Three caller interfaces were compared. A broad Builder/Artifact/Lease graph was
rejected because it advertised substitutability those resources do not have
and could not automatically associate escaped callbacks with leases. A
game-first interface with generic StateTransfer was rejected because Ember
cannot safely serialize owner-bound values, closures, frames, or host objects.
A narrow generation transaction deleted the most caller complexity without
claiming either false capability.

## Decision

Shipping applications continue to statically link generated prepared bundles.
Rebuild/re-exec is the portable hot-reload fallback.

The root package exposes `PreparedRuntimeSlot` immediately above
`Program.NewRuntime`:

1. `Prepare` requires `RuntimeOptions.Prepared`, performs exact ABI, semantic,
   Program-hash, module, and Proto-inventory binding, and creates an inert
   candidate without changing or executing the active generation.
2. `Activate` is an explicit host safe-point transaction. It rejects active
   `Use`, foreign candidates, stale base generations, and closed slots before
   retiring the old Runtime and publishing the candidate.
3. `Use` scopes one synchronous host batch to a stable generation. Runtime
   references may not escape it. Host callbacks and continuation work must be
   serialized inside the same generation boundary.
4. Candidate and slot Close operations are explicit and idempotent. A failed
   bind, load, or activation leaves the active Runtime usable.

Successful activation starts fresh script-owned runtime state. World state,
files, clocks, networking, and other application objects remain host owned.
Any state transfer uses an application schema over detached data. Raw values,
tables, closures, callbacks, frames, and suspensions never cross generations;
closing the old Runtime makes those handles closed or stale.

The optional `preparedplugin` package is the one reviewed native edge. On a
cgo-enabled Darwin, FreeBSD, or Linux build it opens the standard generated
`Bundle` symbol from an absolute plugin path. Other builds compile a stub that
returns `ErrUnsupported`. File watching, source snapshots, Go command
execution, artifact caches, activation scheduling, generation caps, and
process restart policy remain Host Adapter effects.

Every plugin package path and filename must be content-derived and immutable;
Go will not load changed code under an already loaded plugin package path.
Identical build identities reuse their existing artifact. Exact
`Program.NewRuntime` binding remains the final correctness authority; the
artifact hash is cache identity, not trust. Plugins are trusted native
application code and must be built with the same Go toolchain, build flags, and
shared dependency sources as the host.

## Consequences

- Prepared guest execution is unchanged. No reload check enters generated
  functions, side exits, or guest operations. The scoped slot boundary measured
  28.6-29.6 ns/op and 0 B/0 allocations on the selection host; one boundary can
  cover a complete frame.
- The active generation continues while source compilation, Go compilation,
  plugin loading, and exact binding happen off the safe point. Activation does
  no toolchain or loader work.
- Same-process editor reload is available for source unknown at the original
  build without routing that source through the slower dynamic Machine.
- Go plugins cannot be unloaded. Editors must deduplicate by complete build
  identity, impose a generation/memory cap, and restart periodically. Static
  releases and re-exec reclaim code normally.
- Plugin loading inherits Go's narrower OS/cgo support, exact build-identity
  requirements, security risk, and race-detector limitations. It is not the
  portable production deployment mechanism.
- ADR 0008's no-plugin rule is superseded only for this explicit optional
  adapter. The root runtime, normal generated packages, and cgo-disabled builds
  retain the pure-Go boundary.

The platform and lifetime constraints come from Go's official
[`plugin` package documentation](https://pkg.go.dev/plugin); the standard
loader's cgo/OS build boundary is visible in
[`plugin_dlopen.go`](https://go.dev/src/plugin/plugin_dlopen.go). Luau's own
same-process native path likewise exposes an explicit code-generation context
and compile operation in its
[`CodeGen` interface](https://github.com/luau-lang/luau/blob/master/CodeGen/include/Luau/CodeGen.h),
which is JIT machinery rather than a portable data-artifact shortcut.

## Alternatives Rejected

- Continue optimizing only the dynamic VM: retained as compatibility fallback,
  but prior complete evidence is far outside the requested Luau ratio.
- Put compiler and plugin operations in `Runtime`: rejected because it hides
  filesystem/process/native effects and delays failure into the owner seam.
- Automatically migrate script heaps or callbacks: rejected because identity
  and continuation semantics cannot be preserved across independently compiled
  Programs.
- Integrate a native JIT now: rejected because it recreates Luau's executable
  allocator, custom entry ABI, unwind metadata, and platform backends without a
  demonstrated advantage over Ember's current compiled-Go backend.
