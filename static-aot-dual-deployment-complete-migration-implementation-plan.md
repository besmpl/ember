<!-- simple-loop-plan -->

# Static AOT dual deployment complete migration implementation plan

## Outcome

- **Eventual goal:** Give games and other hosts one typed operation contract with two deployments: embedded static AOT for zero-IPC releases and supervised static-AOT worker generations for hot reload, then delete every superseded same-process native/plugin reload path.
- **Current run:** Prove, implement, migrate, and certify that complete reload architecture. If the host-shaped admission in P1 fails, stop before adding a public package or deleting ADR 0010.

### Done when

- D1. The same typed turn, application-owned state, generated bundle, inputs, and outputs run embedded and in the worker; releases know no reload concepts and hot reload exposes no Ember runtime object.
- D2. One deep worker module exposes `Prepare`, quiescent-only `Activate`, serialized `Transact`, and bounded `Close`; process and memory adapters share identity, ordering, limits, and failures.
- D3. State revisions, checksummed checkpoints/deltas, a durable operation journal, and post-commit outbox make duplicates, recovery, migration, and uncertain commit deterministic without moving guest-owned objects or continuations.
- D4. Changed source builds into an immutable worker while the active generation serves; the candidate restores, follows deltas, warms, activates without I/O or guest work, and the old process is reaped.
- D5. Exact all-37 worker execution retains median `<=1.00x` and p90 `<=1.05x` Luau; the development worker stays `<=1.50x` embedded AOT on matched host slopes, normal work uses one synchronous exchange, and codec plus IPC p99 is at most 10% of the declared frame/request budget. Releases remain embedded.
- D6. Builds and lifecycle tests use `CGO_ENABLED=0` on native Darwin, Linux, and Windows ARM64/x86-64 runners; at least one physical ARM64 and one physical x86-64 host carry matched performance receipts.
- D7. Buildable/current surfaces contain no slot, reload-native, `internal/preparednative`, or `preparedplugin` family; ADR 0011 supersedes only those ADR 0010 clauses, retaining its static-Go/replay decisions.

### Constraints

- Keep application types outside package `ember`. Build, watcher, cache, file, and process effects are explicit host actions.
- No transparent remote `Runtime`, arbitrary command envelope, or per-entity/property/host-function RPC. Wire values are bounded application records with closed routes and schemas.
- Canonical gameplay state crosses every turn. Callbacks and suspensions remain generation-owned; activation requires zero pending continuations.
- Retain static prepared Go, `PreparedBundle`/`PreparedContext`/exact replay, backend Go IR/emission, `cmd/emberc`, and Machine until a separate total-AOT proof replaces their current semantic work.
- Add no dependency. Retain `x/sys` only if native Windows job cleanup proves it necessary; remove `purego` with executable-memory code.

### Non-goals

- Preserving guest identity or live continuations across reload, sandboxing hostile code, or hiding build latency.
- Deleting the direct VM/`EMBER_RUNTIME_ENGINE` migration path or Machine. Those require separate complete semantic cutovers and are not reload implementations.

## Repository evidence

- Static prepared Go passes all 74 all-37 rows; unsupported entries replay through Machine. P1's numeric frame work binds generated Go while effect orchestration replays.
- ADR 0010's native reload proves only four numeric rows. The slot, `runtime_prepared_native.go`, executable-memory `runtime_backend_native*`, `internal/preparednative`, and `preparedplugin` form the deletion family.
- Owner-bound callbacks, suspensions, userdata, and values cannot cross processes. Existing slot tests prove safe-point/stale-handle behavior, not transferable state.
- Provisional dirty-worktree P1 probes matched all three hashes, kept prepared host slope near `1.0x`, and put direct exchange p99 far below budget; claims await paired clean receipts. Generic crossing scaling makes batching mandatory.
- `emberc` deterministically emits a bundle but does not embed a reconstructible Program, build an executable, supervise it, or attest an artifact. The pure-Go policy currently rejects unallowlisted `os/exec` use.

## Execution path

### P1. Freeze the typed turn and decisive admission [D1, D5]

- **Touch:** private typed turn/probe fixtures, generated numeric work, exact all-37 fixtures, and the admission wrapper.
- **Change:** Carry canonical state, projection, ordered events/completions, commands/effects, and revision. Run identical traces through pinned Luau, embedded generated numeric work, and one child exchange; keep effect orchestration exact. Prove semantic restart feasibility from a quiescent checkpoint. Use the batch observer only to discriminate crossing shape. Reject extra exchanges, pending activation, or missing D5.
- **Proof:** Two bound captures prove three-engine hashes, all-37/matched slopes, rich-turn and direct-exchange quantiles, process ceilings, raw inventories, and crossing scaling.

### P2. Deepen the transactional worker island [D2, D3]

- **Touch:** new private `internal/preparedworker` module and interface-level contract tests.
- **Change:** Implement typed `Prepare`/`Activate`/`Transact`/`Close` over opaque identities. Inject application checkpoint codec/quiescence policy plus journal and outbox ports with an in-memory adapter. Cache identical completions, reject conflicts/gaps, classify guest/identity/protocol/stale/lost/conflict/unknown-commit failures, and never retry uncertainty.
- **Proof:** Interface tests cover bounds, cancellation, uncertainty, corruption, stale identities, migration, recovery, the game turn, and one genuinely different request/job contract.
- **Depends on:** P1.

### P3. Make generated artifacts and processes exact [D2, D4, D6]

- **Touch:** `cmd/emberc`, generated fixture support, `internal/preparedworker` worker `Serve` and process adapters, pure-Go launch policy, and target CI fixtures.
- **Change:** Emit a deterministic self-contained Program/generation descriptor beside the bundle. Build worker mains temporarily, atomically publish by non-circular manifest ID plus executable digest, hash before launch, bootstrap at a supplied checkpoint, and verify source/Program/Proto, ABI/semantics, protocol/schema/limits, toolchain, target, tags, and flags before `READY`. Use framed inherited pipes, no shell, bounded resources, one `Wait` owner, and platform orphan prevention.
- **Proof:** Corrupt identities and interrupt every build/process stage; the active worker survives, artifacts deduplicate, children are reaped, and native no-cgo jobs cover every target.
- **Depends on:** P2.

### P4. Complete state, effects, reload, and recovery [D3, D4]

- **Touch:** island journal/outbox implementation, checkpoint/delta protocol, candidate follower, worker-local host adapter, and lifecycle tests.
- **Change:** Journal typed output, state delta, and stable-ID outbox before publication. Resolve uncertain CAS by status lookup; unresolved status quarantines the worker and never reruns guest work. Deliver only idempotent/transactional records. Candidates restore and follow hash-chained deltas. Activate only caught up with zero pending continuations; otherwise the old generation serves through bounded drain or reload abort, then becomes stale and is reaped.
- **Proof:** Traces cover waits/completions, projection and canonical schema transitions, every commit/delivery crash point, unknown status, catch-up, orphans, recovery, and 1,024 bounded-RSS swaps.
- **Depends on:** P3.

### P5. Prove both deployment adapters [D1, D4, D5]

- **Touch:** application-facing frame/request modules, embedded/worker adapters, host examples/tests, and prepared docs.
- **Change:** Release adapters call the typed handler with the static bundle; development adapters call the island and expose a separate reloader. Port examples and slot assertions, but retain current caller wiring as a differential fallback while the worker remains private.
- **Proof:** Each handler proves restart equivalence; one runner compares embedded, in-memory, and process results, state bytes, command/effect order, errors, limits, initialization, callback/wait lifecycle, and reload continuity. Source-to-`READY`, restore, activation, and retirement are separately timed.
- **Depends on:** P4.

### P6. Certify, promote, and migrate the worker path [D1, D5, D6]

- **Touch:** worker performance/protocol gates, `.github/workflows`, `docs/checks.md`, ADR status/index, and final supported-platform matrix.
- **Change:** Freeze evidence, budgets, limits, resource ceilings, crash outcomes, and both ISA receipts. Only after every gate passes, move the module to `preparedworker` without aliases, switch callers, remove differential wiring, and accept ADR 0011; failure leaves the private prototype removable and ADR 0010 active.
- **Proof:** `scripts/check`, `CGO_ENABLED=0 go build ./...`, race/checkptr worker tests, two clean pinned-Luau captures, and native process lifecycle jobs on every declared target.
- **Depends on:** P5.

### P7. Delete the superseded reload architecture [D7]

- **Touch:** `prepared_runtime_slot*`, `runtime_prepared_native*`, executable-memory `runtime_backend_native*` files/tests, `internal/preparednative`, `preparedplugin`, dependencies, pure-Go policy, native parity routes, CI, compatibility manifest, README, prepared/embedding/public-surface/design docs, and ADRs 0010/0011.
- **Change:** After P6 admission, remove the slot/native/plugin family; port only observable lifecycle coverage and delete ISA/mapping/trampoline/plugin tests with their implementations. Remove `purego`; retain `x/sys` only for worker cleanup. Remove the native observer and every deleted-path selector, then mark ADR 0010 superseded for reload.
- **Proof:** Production/current-doc scans allow only unrelated intrinsic names and ADR 0010's retained history; `go mod tidy`, generated freshness, retained embedded/worker traces, `CGO_ENABLED=0 go build ./...`, `scripts/check`, and cross-platform lifecycle jobs pass after deletion.
- **Depends on:** P6.

## Conditional work

### C1. Add mapped bulk transport only for measured copy cost

- **Trigger:** P1 or P6 shows D5 failing because codec/copy consumes more than half of p99 exchange time while normal phase count remains one.
- **Then:** Keep control framing and transaction semantics; add bounded generation-owned SPSC mapped arenas only for typed arrays/blobs.
- **Repairs:** D5, D6.
- **Proof:** Repeat the failed payload gate plus bounds, crash, cleanup, and platform lifecycle tests with identical bytes/results.
- **Else:** no work.

## Risks and assumptions

- The hard product constraint is data flow: hosts requiring sequential irreversible calls cannot use worker reload without changing their scripting contract.
- There is no serializable Runtime snapshot. Durable continuity is application state; each handler must prove restart equivalence, observable gameplay module globals are forbidden, and live continuations must drain before activation.
- Go build latency remains visible source-to-ready latency even though active play and activation do not wait for compilation.

## Source disposition

- This plan supersedes every active package in `supervised-aot-worker-generations-implementation-plan.md` with an explicit typed transaction, dual deployment, and mandatory post-admission deletion path.
- `complete-machine-removal-implementation-plan.md` stays inactive: current static generated code still depends on Machine ownership and exact replay.
