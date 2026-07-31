<!-- simple-loop-plan -->

# Static AOT transactional worker complete cutover implementation plan

## Outcome

- **Eventual goal:** Ship one typed transaction island with fast embedded static AOT and supervised static-AOT hot reload, then remove all same-process native/plugin reload code.
- **Current run:** The candidate transaction, worker, builder, and deletion
  architecture exists in the dirty working tree. Stages 1-6 have focused local
  implementation evidence, and stage 7 now has the complete six-target native
  workflow contract plus bounded paired-admission and soak jobs. The exact
  candidate still lacks one uncontaminated same-revision local receipt set and
  the target-native CI receipts, so S0 has not passed. ADR 0011's Accepted
  status records direction; it is not a substitute for those receipts.

### Done when

- D1. Callers see typed `Runner.Apply`, `Resolve`, and bounded `Close`; development alone gets `Prepare` and an opaque candidate with no-I/O `Activate`/`Close`. Releases expose no reload/generation type.
- D2. Each operation atomically commits canonical result, checkpoint/delta, ordered outbox, and quiescence. Duplicates replay exact receipts; conflicts/gaps fail before guest entry; routing is private.
- D3. Durable lookup resolves uncertainty as stored, absent, or unresolved. Unresolved work quarantines without guest retry; effects publish only after commit under stable ordered IDs.
- D4. Candidates restore, migrate, warm, and follow commits while active work continues. Only caught-up quiescent candidates activate by bounded pointer swap; old workers are reaped without Ember-object transfer.
- D5. Embedded, memory-fault, and process adapters produce identical bytes and behavior for the rich game turn and a materially different request/job contract.
- D6. All-37 is median `<=1.00x`/p90 `<=1.05x` Luau; worker is `<=1.50x` embedded, uses one exchange, and codec+IPC p99 is `<=10%` host budget. No-cgo lifecycle proof covers all Darwin/Linux/Windows ARM64/x86-64 targets with both physical ISAs measured.
- D7. Buildable surfaces contain no slot/native/plugin/executable-memory/private-cgocall/native-parity/`purego` reload family; static Go, Machine replay, and direct VM migration remain.

### Constraints

- Build/watch/source/cache are explicit host actions. Workers accept immutable artifacts and closed records, never remote `Runtime` or fine-grained host RPC.
- App adapters own schemas and quiescence; the module owns routing, ledger, duplicates, outbox, uncertainty, supervision, bounds, and cleanup.
- Any backend error after admission is terminal until durable recovery; domain rejection is a typed committed result, not an ambiguous Go error. No new dependency.

## Repository evidence

- Two earlier clean all-37 captures at `fcef38d4` passed; worst worker/Luau p90
  was `0.198x`, host slope `1.080x`, and exchange p99 `41.9us`. They predate the
  current identity policy and are historical evidence, not S0 promotion proof.
- The current candidate's focused `preparedworker`, `preparedworkerbuild`, and
  generated-fixture tests pass, and the old slot/native/plugin families are
  deleted in the working tree. The candidate is not a clean retained revision,
  and no same-revision stage-7 physical-platform, resource, or final repository
  receipt has been retained.
- The current stage-4 candidate requires an exact Go command, derives execution and
  descriptor identity from one versioned fixed policy, disables implicit PGO
  and host-native ISA selection, includes selected `HFiles` and standard-library
  sources, fingerprints the exact Go executable plus bounded whole
  `GOTOOLDIR`/include trees, and recaptures inputs after compilation. Focused
  builder, worker, fixture, and six-target cross-build checks pass locally;
  this is not a retained exact-revision native-platform receipt.
- Local S0 reacquisition exposed five harness defects rather than runtime
  failures. A frozen worker-B schedule incorrectly required capture A's 10 ms
  calibration observation instead of the hard 5 ms evidence floor, a single
  noisy baseline subtraction could reject B without reacquiring its complete
  prescribed calibration, and the
  runtime harness inherited Go's 10-minute timeout even though bounded
  contamination retries can legitimately extend a clean acquisition, and the
  intrinsically slow full VM all-37 capture could not finish inside the same
  35-minute bound used by dynamic and prepared captures. The
  candidate now keeps A's conservative 10 ms scale selection, verifies B at
  5 ms while reacquiring a structurally sub-floor complete three-sample B
  calibration at most three times, gives each contaminated or
  external-engine-timed-out point up to 300
  one-second retries inside a 60-minute full-VM or 35-minute dynamic/prepared
  test bound, and buffers each worker
  repeat so a structurally invalid fit is discarded for at most three bounded
  reacquisitions before any rows are published. Semantic/protocol errors and
  valid ratio failures are never retried. Attempts interrupted or rejected by
  external CPU contamination are not promotion evidence and are never
  converted into PASS.

## Active execution path

Execute one stage at a time. Each lettered item is one commit-sized logical
changeset and review boundary, not authorization to create a Git commit. A
repair invalidates every affected downstream receipt. The exact write scope,
commands, time bounds, and two workflow checkpoints are in
`docs/simplepower/plans/2026-07-29-prepared-worker-s0-certification.md`.

### 1. Freeze and stabilize the cutover

- **1A — Authority and inventory:** freeze the intended diff, maintained
  authority, compatibility claim, toolchain, targets, limits, all-37 workload,
  generated owners, thresholds, deletion set, rollback boundary, and non-goals.
- **1B — Reproducible baseline:** pass generated freshness, worker/builder and
  root tests, normal and no-cgo builds, vet, focused race, and focused checkptr
  against one recorded source/toolchain/environment input.
- **Accept:** one reproducible baseline passes and no buildable native/plugin/
  slot reload path remains.

### 2. Close Luau prepared semantics and replay

- **2A — Verified lowering:** prove CFG/SSA construction, liveness, barriers,
  charge/PC/spill maps, proof facts, and malformed-IR rejection.
- **2B — Pre-effect exits:** every generated guard either proves exact behavior
  or exits to the canonical Machine before the first effect.
- **2C — Differential closure:** VM, Machine, and prepared paths agree for the
  frozen compatibility inventory, limits, cancellation, errors, and state; code
  size and exit coverage remain accounted.
- **Accept:** each frozen path executes exactly or replays before effects.
- **Depends on:** stage 1 PASS.

### 3. Recertify Luau owner-entry AOT

- **3A — Capture contract:** freeze `guest_batch_v2`, seeds, checksums,
  allocation fields, the four base points with a conservatively selected
  per-case power-of-two call scale, toolchain, and comparator. Capture A
  selects against 10 ms; capture B reuses A's schedule and must clear the hard
  5 ms evidence floor. B may discard and reacquire a complete structurally
  sub-floor three-sample prescribed calibration at most three times, but never
  emits rejected samples or retries measurement errors.
- **3B — Independent pair:** acquire two complete pinned-Luau all-37 captures.
- **Accept:** prepared/Luau median is `<=1.00`, p90 is `<=1.05`, allocations and
  results match, and both captures pass independently. This does not certify the
  worker.
- **Depends on:** stage 2 PASS.

### 4. Freeze owner-framed identities with reuse bypassed

- **4A — Identity vectors:** freeze distinct source/check/language, Program,
  contribution, Layout/contract, BuildID, executable, launch, and candidate
  vectors with exact downstream invalidation tests.
- **4B — Build closure:** bind one explicit `GOTOOLCHAIN=local`, `-pgo=off`,
  baseline-ISA build to selected module/source/header inputs, exact Go binary,
  bounded tool/include trees, target, policy, executable digest, and byte limit.
- **4C — Clean-root falsifier:** bypass reusable output; prove construction,
  post-build recapture, drift rejection, publication-path independence, READY
  mismatch rejection, and preservation of the previous selection on failure.
- **Accept:** every mutation invalidates exactly its owner and downstream
  products without cache dependence; build and launch fail closed.
- **Depends on:** stage 3 PASS.

### 5. Certify embedded transaction durability

- **5A — Memory transaction:** prove serialized admission, conflict/gap
  rejection, atomic result/checkpoint/outbox/quiescence commit, duplicate replay,
  stable effect IDs, and post-commit delivery.
- **5B — Durable resolution:** prove `Committed`, `NotCommitted`, and
  `Unresolved`, quarantine unknown work, and forbid blind guest re-entry.
- **5C — File-journal faults:** prove framing, hash chain, torn tails, sync/write/
  rename faults, cancellation, corruption rejection, reopen, and memory parity.
- **Accept:** every admitted operation has one explicit durable state and effects
  publish only after commit.
- **Depends on:** stage 4 PASS.

### 6. Certify EPW2 reload and process ownership

- **6A — Protocol and launch:** prove artifact rehash, HELLO/READY identity,
  bounded frames, correlation, cancellation, terminal failure, one Apply
  exchange, and one `Wait` owner.
- **6B — Candidate lifecycle:** prove restore/migrate/warm/follow, caught-up
  quiescent no-I/O activation, isolation, forward replacement, and retryable
  retirement while active work continues.
- **6C — Platform ownership:** prove parent-death/watchdog behavior, bounded
  close, and child reaping on the declared platform implementations.
- **Accept:** embedded/process traces match for both application contracts;
  failed candidates do not disturb active work and every child is reaped.
- **Depends on:** stage 5 PASS.

### 7. Acquire and decide S0

- **7A — Paired worker admission:** acquire two frozen-schedule all-37 pairs;
  require worker and embedded versus Luau median `<=1.00`/p90 `<=1.05`, worker/
  embedded `<=1.50`, exact results, and one timed exchange.
- **7B — Transport and resources:** require 4,096 fixed-size exchanges at p99
  `<=1,666,666 ns`; run 1,024 alternating generations with per-child RSS
  `<=256 MiB`, aggregate RSS `<=512 MiB`, and zero retained descendants.
- **7C — Target matrix:** pass six no-cgo cross-builds, six target-native launch/
  reload/retirement jobs, and physical arm64/x86-64 performance captures.
- **7D — Promotion:** only after all same-revision receipts pass, align maintained
  docs/ADRs and record S0. Cross-build, emulated, cached, partial, or stale
  evidence cannot promote it.
- **Accept:** complete retained S0 PASS.
- **Depends on:** stage 6 PASS.

## Risks and assumptions

- External exactly-once visibility requires a transactional/idempotent effect adapter; Ember guarantees logical IDs and ordering, not arbitrary-sink atomicity.
- Source-to-ready may take seconds and game state must be serializable at quiescent points. Hosts needing unbounded synchronous crossings use embedded AOT, not a weakened worker seam.

## Gated follow-ons

Stages 8-17 are not active implementation work and gain no authority from this
plan. They open one at a time only after the preceding retained PASS:

8. Private S4 decides whether `preparedsource.Set`/`Layout` deletes net policy
   at both embedded and build/process boundaries; otherwise replace/remove it.
9. Supervised stateless Sprig/Seed proves independent concrete compilers and one
   application-owned EPW2 lifecycle without a shared ABI.
10. Certify only the `preparedworkerbuild` cache against a clean-root oracle.
11. Freeze one bounded, explicitly non-full-Ruby semantic product and oracle.
12. Certify the exact H1b checked fixed-capacity Ruby heap control.
13. Certify detached Ruby checkpoint projection, migration, failure, and reopen.
14. Prove the bounded Ruby artifact as ordinary cgo-free Go in a clean root.
15. Prove the stateful Ruby/Sprig embedded/process worker lifecycle and targets.
16. Run independent Ruby R1; require PASS or explicitly cut production scope.
17. Close the architecture only when every promoted claim has retained PASS.

The functional/3D language is deferred beyond stage 17. It requires separate
product semantics and approval and does not justify shared compiler/runtime
abstractions in the active Luau or gated Ruby work.
