# No-CGO native prepared reload architecture audit

Status: implemented; final repository verification pending.

This file is the implementation audit requested for Ember's no-cgo hot-reload
slice. It records the architecture that survived measurement, the designs that
did not, the exact performance proof, and the remaining production limits.
The durable public decision belongs in ADR 0010 and `docs/prepared.md`.

## Outcome

`PreparedRuntimeSlot.Prepare` can now accept a changed `Program` whose source
was unknown when the host was built. When `RuntimeOptions.Prepared` is nil, it
performs reload-time native preparation before activation:

1. Build the existing immutable backend IR and exact Program identity.
2. Qualify only pure numeric functions with proved static dependencies.
3. Emit one deterministic module image for ARM64 or x86-64.
4. Validate ABI, semantic version, Program hash, inventories, offsets, and ISA.
5. Install the image in generation-owned executable memory.
6. Bind native entries into an exact `PreparedBundle`; unsupported entries are
   nil and therefore request canonical Machine replay.
7. Construct an inert `Runtime` candidate while the active generation remains
   usable.
8. Publish only at `PreparedRuntimeSlot.Activate`, then close the retired
   Runtime and executable mappings after calls have drained.

This is ahead-of-activation compilation, not build-time AOT in the strict
toolchain sense. Changed source is compiled inside the running process, but no
compilation occurs in an active script call or at activation. Static generated
Go remains the fastest and most portable release path.

## Architecture

### One semantic qualifier, two ISA emitters

`backendNativeCandidate` owns all semantic admission decisions. It accepts the
same backend IR used by prepared Go and proves:

- at most eight explicit plus hidden numeric arguments;
- one numeric result;
- numeric constants, arithmetic, comparisons, branches, and loops;
- direct static calls whose complete dependency graph also qualifies;
- bounded numeric self-recursion;
- immutable module-private numeric captures;
- no host effects, table effects, varargs, mutable captures, coroutines, or
  other operation that could make replay after a partial effect unsafe.

The ARM64 and x86-64 emitters only select instructions, lay out frames, and
resolve relocations. They do not duplicate language policy. If one dependency
fails admission, every caller that requires it also falls back exactly.

### Boundary adapter and private fast-call body

Every emitted function has two entries:

- a boundary adapter accepts the Go-facing pointer/count/result ABI and may
  request canonical replay before effects;
- a private body receives explicit and hidden numeric arguments directly in
  D0-D7 on ARM64 or XMM0-XMM7 on x86-64, returns the value in D0/XMM0, and
  returns prepared/replay status in X0/RAX.

Native-to-native calls target private bodies. This removes argument arrays,
result pointers, and count checks from internal calls while keeping Go pointers
out of the generated call graph. Artifact validation requires every body to
precede its public adapter and checks architecture-specific alignment.

### Exact replay is the semantic boundary

The native bundle has the complete module and Proto inventory. A qualified
entry executes native code only when its runtime values satisfy the numeric
contract. An unsupported Proto or value returns `PreparedReplayEntry` before
any effect and the existing Machine executes the entry from the beginning.

This is a hybrid runtime, not a claim of full-language native compilation.
Tables, host calls, mutable or escaping closures, varargs, coroutines,
metamethod-heavy behavior, and every unproved shape remain on the canonical
Machine. Static generated Go bundles use the same nil-entry replay rule, so one
unsupported sibling no longer rejects an otherwise exact bundle.

### Generation ownership

One `preparedNativeGeneration` owns the exact bundle and all executable images
for its modules. Ownership transfers from candidate to slot only after a
successful activation.

- Preparation failure closes only the inert candidate.
- Activation performs no compilation, mapping, I/O, or guest execution.
- `Use` pins one stable Runtime generation and excludes activation.
- Each executable holds a read lease during calls; `Close` takes the write
  lease before unmapping, so code cannot disappear under an in-flight call.
- Retired mappings reject later calls and repeated activation reclaims prior
  images.
- Callbacks, suspensions, closures, tables, stacks, and module state are never
  transplanted. Application state remains host-owned and crosses generations
  only through an explicit detached schema.

### Native boundary

The native edge is isolated in `internal/preparednative`:

- executable bytes are copied into a private Darwin `MAP_JIT` mapping;
- the write window is serialized, pinned to one OS thread, and closed with
  `pthread_jit_write_protect_np`;
- `sys_icache_invalidate` publishes the completed image;
- calls enter on the Go runtime's foreign/system stack through a tiny
  architecture-specific trampoline;
- generated code cannot retain Go pointers;
- close waits for calls and then unmaps the image.

The host builds and tests with `CGO_ENABLED=0`. The implementation deliberately
uses `purego` plus one audited `runtime.cgocall` linkname, so "no cgo" does not
mean "no private runtime ABI." That risk is contained in one internal package
and enforced by the pure-Go boundary scanner.

## Platform matrix

| Platform | Lowering | Native installation | Runtime result |
| --- | --- | --- | --- |
| Darwin ARM64 | Implemented and executed | `MAP_JIT` | Native qualified subset plus exact Machine replay |
| Darwin x86-64 | Implemented and executed; SSE4.1 required | `MAP_JIT` | Native qualified subset plus exact Machine replay |
| Linux/Windows x86-64 | Emitter cross-builds | Not implemented | Exact all-Machine bundle |
| Other OS/ISA | Not selected | Not implemented | Exact all-Machine bundle |

The x86-64 backend is not an ARM translation shim. Its instruction selection,
relocations, System V boundary trampoline, SSE4.1 floor operations, and native
execution all passed under Rosetta. Native Intel-hardware timing and native
installers outside Darwin remain separate portability work.

## Performance proof

The accepted observer uses the real unknown-source path:

`LoadProgram -> Prepare -> Activate -> Use -> Runtime.Invoke -> native/replay`

It compares fitted guest-batch slopes with pinned Luau 0.728 using four points,
three repeats, `CGO_ENABLED=0`, and `GOMAXPROCS=1`. Every paired median and
worst ratio must be at most 1.50; no aggregate can hide a failing row.

| General numeric row | Median Ember/Luau | Worst Ember/Luau |
| --- | ---: | ---: |
| arithmetic `for` | 0.688620 | 0.692415 |
| `while` plus branching | 0.280739 | 0.286104 |
| captured recursive Fibonacci | 0.167772 | 0.176960 |
| iterative Fibonacci | 1.268322 | 1.290334 |

Receipt: `/private/tmp/ember-native-reload-parity.NC2P37-fastcall-20260718-a`.
The environment, raw, and slope SHA-256 values are respectively
`7d7d8f68a17a5295d7401e234d8cd6630ef20ceeef6e8d97aaae1908187d1d19`,
`b21bd36aae1de2b4a7f29e638298da4fc0195f7b895a7b31cb4a7b069a4d40e3`,
and `72ef4e024d7e5a2f9b0dbc4a7eded56f8923e0656c324d6fb4bcb62b42cc1848`.

This proves the qualified numeric tier, not arbitrary game scripts. Mixed game
workloads inherit Machine performance for unsupported code and must be measured
with their real native-coverage ratio and host-call shape.

## What worked

- **Static prepared Go:** remains the speed and portability ceiling for source
  known at application build time.
- **One target-independent semantic plan:** kept ARM64 and x86-64 behavior
  aligned and made fallback a property of semantics rather than ISA quirks.
- **Direct CFG branches:** avoided a bytecode dispatcher inside native code.
- **Adapter/private-body split:** removed internal boundary overhead and moved
  iterative Fibonacci from a 1.667855 worst failure to 1.290334.
- **Immutable numeric captures:** generalized recursive and nested numeric code
  without accepting mutable cell semantics.
- **Exact nil-entry replay:** preserved the complete Machine as the oracle and
  allowed partial module qualification without source or benchmark routing.
- **Generation-level mappings:** made activation and reclamation explicit and
  kept executable lifetime out of ordinary Runtime state.

## What failed and was removed

| Experiment | Best observed result | Decision |
| --- | ---: | --- |
| Generated adaptive superword VM | Wins did not transfer across workload families | Removed; dynamic specialization was not a general parity architecture |
| Adaptive SSA regions | Benchmark-zone-sensitive | Rejected; it did not provide general script coverage |
| Whole-Go prepared Wasm reactor | about 2.14x Luau | Removed |
| Direct general-CFG Wasm | about 4.1x Luau | Removed |
| Structured-loop Wasm | about 2.44x Luau | Removed |
| Semantics-complete floor/mod Wasm | about 3.53x Luau | Removed |
| Pointer/count ABI for every native call | iterative worst 1.667855x | Replaced by private register fast calls |

Wasm offered a clean sandbox, unloadable generations, and a stable ABI, but
the complete Ember capsule retained too much fixed and steady runtime cost.
More local tuning would not have been a new architecture. Go plugins can reuse
generated Go speed but require cgo, cannot unload, and bind to exact Go build
identity. Helper processes reclaim cleanly but make fine-grained host calls an
IPC problem. These remain explicit host choices, not the primary no-cgo path.

## Correctness lesson from unsupported shapes

A mutable-capture fallback test exposed an older optimizer bug: both VM and
Machine returned a stale value when a captured local was assigned on one branch.
The child descriptor was correctly by-reference, but DCE could not see a later
closure read and deleted the write. DCE now treats writes to shared captured
registers as observable hidden-heap effects. Native admission still rejects the
shape, and canonical replay now produces Luau behavior on both engines.

This validates the hybrid architecture's most important discipline: fallback
tests must exercise the supposedly safe path, not merely prove that native code
was skipped.

## Production limits and next pressure

The current tier is suitable for production experiments on Darwin ARM64 and
x86-64 when scripts have substantial pure numeric regions and hosts accept the
audited executable-memory/runtime-ABI seam. It is not yet a universal
production-ready Luau replacement.

Remaining pressure is explicit:

1. Run the complete repository, race, checkptr, and cross-build gates on the
   cohesive result.
2. Add native installation and ABI tests for Linux/Windows x86-64 before
   advertising native speed there.
3. Measure real game scripts by native coverage and end-to-end frame cost;
   never extrapolate the four numeric rows to tables or host-heavy behavior.
4. Add a long resident-memory soak beyond the deterministic 32-generation
   reclamation test.
5. Widen qualification only through Luau behavior tests and replay-safe
   semantics, never workload names or benchmark fingerprints.

## Acceptance checklist

- [x] Unknown source compiles before activation with no Go toolchain or cgo.
- [x] ARM64 and x86-64 share one semantic qualifier and artifact contract.
- [x] Every qualified benchmark row is at most 1.5x pinned Luau.
- [x] Unsupported Protos and values replay exactly before effects.
- [x] Activation, retirement, repeated reclamation, and close are explicit.
- [x] Static prepared Go and ordinary Machine paths remain intact.
- [x] Mutable-capture fallback semantics pass VM and Machine differentials.
- [ ] Final repository checks, documentation verification, commit, and push.
