# Prepared Worker Cutover S0 Implementation Plan

## Goal

Complete and certify the existing Luau static-AOT transactional-worker cutover through roadmap stages 1-7 as ordered, reviewable logical changesets, while leaving stages 8-17 gated and keeping the functional/3D language outside active work.

## Design Summary

This plan replaces the five broad phases in `static-aot-transactional-worker-complete-cutover-implementation-plan.md` with the research roadmap's seven S0 stages. The candidate implementation already exists in the dirty working tree, so each active stage first proves the present behavior, then makes only the smallest root-cause repair required by its frozen contract. A **logical changeset** below is a commit-sized review boundary; it does not authorize a Git commit, branch, push, or PR.

Exactly one active stage advances at a time. A stage is complete only after its focused checks and acceptance receipt pass. Later-stage evidence obtained before an earlier repair is invalidated and rerun. Independent evidence commands may run concurrently only after the candidate inputs, toolchain, environment, schedule, and identity policy are frozen for that stage.

The architecture remains language-owned: Luau owns parsing, checking, lowering, bytecode, runtime semantics, and generated prepared Go. Sharing remains limited to immutable generated-source delivery and the host-owned typed transaction/supervision boundary already accepted by the maintained ADRs. This plan does not add a universal compiler IR, backend/runtime registry, generic guest value, shared heap, cross-language module graph, common ABI, cgo, executable memory, Go plugins, same-process reload, hidden compilation, or a new dependency.

The active S0 outcome is one exact candidate whose prepared paths either execute exactly or replay through the canonical Machine before effects; whose owner-framed identities invalidate precisely; whose embedded and EPW2 adapters agree on durable transaction behavior; and whose retained performance, resource, cross-build, and target-native receipts all pass. Missing target-native or physical-ISA evidence blocks S0 rather than narrowing the matrix.

Stages 8-17 are follow-on gates only and create no implementation authority in this plan:

8. **Private S4 carrier decision:** after retained S0 PASS, retain or replace `preparedsource.Set`/`Layout` based on demonstrated net policy deletion at embedded and build/process boundaries.
9. **Supervised stateless Sprig/Seed:** after stage 8, prove concrete independent compilers in one application-owned EPW2 lifecycle without shared language policy.
10. **Builder-cache certification:** after stage 9, certify only `preparedworkerbuild` clean-root hit/miss/invalidation behavior; cache hits may not bypass READY or admission.
11. **Bounded Ruby semantics:** after stage 10, freeze one explicit non-full-Ruby semantic product and independent oracle.
12. **H1b control:** after stage 11, certify the exact checked fixed-capacity Ruby heap control and generated parity.
13. **Ruby projection:** after stage 12, certify detached canonical checkpoint, migration, corruption rejection, cancellation cleanup, and reopen.
14. **Standalone Ruby artifact:** after stage 13, prove ordinary cgo-free Go output in a clean root after removing compiler tooling.
15. **Stateful Ruby/Sprig worker:** after stage 14, prove detached-state embedded/process lifecycle, migration, 1,024 swaps, resources, and targets.
16. **Independent Ruby R1:** after stage 15, run the frozen direct-target comparison; require PASS or an explicit cut of Ruby production scope.
17. **Architecture closure:** after stage 16, audit every promoted claim, identity, public surface, artifact, target, and retained receipt.

The future functional/3D language is deferred beyond stage 17 and requires its own approved product semantics and plan. It is not a reason to generalize the Luau or Ruby paths now.

The active stages are one cohesive package: they certify the same prepared program, artifact, transaction contract, worker protocol, and exact candidate. Their write scopes overlap, and a specialized worker could not complete one stage independently without invalidating another stage's evidence. Main-agent ownership therefore reduces provenance and integration risk.

## Implementation Route

**Main agent** — after the accepted-plan checkpoint, the main agent directly executes the ordered logical changesets in the current session with the resolved BEST tier (`gpt-5.6-sol`, `high`). After all active edits, the mandatory FAST quick verifier uses `gpt-5.3-codex-spark` at `xhigh`, may make only typo-level fixes, and returns every behavioral, structural, interface, test, or scope issue to the main agent. The main agent then reviews the complete diff, makes in-scope repairs, runs final verification, and reaches the final checkpoint only on complete S0 PASS.

## Exact Files

Only these repository paths may be created, modified, deleted, or regenerated. A required edit outside this list stops execution for fresh approval. Generated files are changed only through their owning freshness test.

### Plan authority and maintained claims

- `docs/simplepower/plans/2026-07-29-prepared-worker-s0-certification.md`
- `static-aot-transactional-worker-complete-cutover-implementation-plan.md`
- `README.md`
- `AGENTS.md`
- `docs/README.md`
- `docs/checks.md`
- `docs/compatibility.md`
- `docs/design.md`
- `docs/embedding.md`
- `docs/prepared.md`
- `docs/public-surface.md`
- `docs/adr/README.md`
- `docs/adr/0008-ssa-aot-for-luau-parity.md`
- `docs/adr/0010-hybrid-aot-generation-reload.md`
- `docs/adr/0011-supervised-aot-worker-generations.md`
- `docs/adr/0014-layer-compilation-and-delivery-identities-by-owner.md`
- `docs/adr/0015-require-receipts-for-architecture-promotion.md`
- `performance-audit.md`

### Luau prepared backend, replay, and generated artifacts

- `execution_control.go`
- `prepared_program_recipe.go`
- `prepared_program_recipe_external_test.go`
- `runtime_backend_analysis.go`
- `runtime_backend_facts.go`
- `runtime_backend_ir.go`
- `runtime_backend_ir_test.go`
- `runtime_backend_program.go`
- `runtime_backend_program_test.go`
- `runtime_backend_ssa.go`
- `runtime_backend_verify.go`
- `runtime_backend_go.go`
- `runtime_backend_go_closure_sets.go`
- `runtime_backend_go_coroutines.go`
- `runtime_backend_go_index_functions.go`
- `runtime_backend_go_module.go`
- `runtime_backend_go_mutation_metatables.go`
- `runtime_machine_owner.go`
- `runtime_machine_generated.go`
- `runtime_machine_prepared.go`
- `runtime_machine_prepared_test.go`
- `runtime_parity_test.go`
- `runtime_prepared_bundle.go`
- `runtime_prepared_bundle_external_test.go`
- `runtime_prepared_bundle_test.go`
- `runtime_prepared_external_fixture_test.go`
- `runtime_prepared_go.go`
- `runtime_prepared_go_external_test.go`
- `runtime_prepared_worker_fixture_test.go`
- `runtime_prepared_worker_parity_fixture_test.go`
- `runtime_resumable_machine.go`
- `vm_dispatch_generated.go`
- `cmd/emberc/main.go`
- `cmd/emberc/main_test.go`
- `internal/preparedfixture/prepared_generated.go`
- `internal/preparedworkerfixture/generated/prepared_generated.go`

### Worker transaction, protocol, lifecycle, and build

- `preparedworker/artifact.go`
- `preparedworker/artifact_test.go`
- `preparedworker/build_manifest.go`
- `preparedworker/contract.go`
- `preparedworker/export_test.go`
- `preparedworker/file_journal.go`
- `preparedworker/file_journal_fault_test.go`
- `preparedworker/file_journal_platform_other.go`
- `preparedworker/file_journal_platform_unix.go`
- `preparedworker/file_journal_platform_windows.go`
- `preparedworker/file_journal_test.go`
- `preparedworker/job_queue_contract_test.go`
- `preparedworker/journal.go`
- `preparedworker/lifecycle_contract_test.go`
- `preparedworker/memory_backend.go`
- `preparedworker/memory_store.go`
- `preparedworker/module.go`
- `preparedworker/module_test.go`
- `preparedworker/outbox_fault_test.go`
- `preparedworker/prepare_cleanup_test.go`
- `preparedworker/process_backend.go`
- `preparedworker/process_backend_test.go`
- `preparedworker/process_integration_test.go`
- `preparedworker/process_job_parity_test.go`
- `preparedworker/process_launcher.go`
- `preparedworker/process_parent_lease_darwin.go`
- `preparedworker/process_parent_lease_other.go`
- `preparedworker/process_platform_attr_darwin.go`
- `preparedworker/process_platform_attr_linux.go`
- `preparedworker/process_platform_unix.go`
- `preparedworker/process_platform_unsupported.go`
- `preparedworker/process_platform_windows.go`
- `preparedworker/process_protocol.go`
- `preparedworker/process_protocol_test.go`
- `preparedworker/process_schema_migration_test.go`
- `preparedworker/process_server.go`
- `preparedworker/process_server_test.go`
- `preparedworker/process_transport_admission_test.go`
- `preparedworker/process_watchdog_darwin.go`
- `preparedworker/process_watchdog_other.go`
- `preparedworker/rich_game_contract_test.go`
- `preparedworker/runner.go`
- `preparedworker/runner_lifecycle_test.go`
- `preparedworker/uncertainty_test.go`
- `preparedworker/worker.go`
- `preparedworkerbuild/build.go`
- `preparedworkerbuild/build_identity_test.go`
- `preparedworkerbuild/build_test.go`
- `preparedworkerbuild/sync_other.go`
- `preparedworkerbuild/sync_test.go`
- `preparedworkerbuild/sync_unix.go`
- `internal/preparedworkerartifact/artifact.go`
- `internal/preparedworkerartifact/manifest.go`
- `internal/preparedworkerartifact/platform_other.go`
- `internal/preparedworkerartifact/platform_unix.go`
- `internal/preparedworkerartifact/platform_windows.go`
- `internal/preparedworkercontract/contract.go`
- `internal/preparedworkerjobfixture/cmd/worker/main.go`
- `internal/preparedworkerjobfixture/cmd/worker_v1/main.go`
- `internal/preparedworkerjobfixture/codec.go`
- `internal/preparedworkerjobfixture/contract.go`
- `internal/preparedworkerjobfixture/handler.go`
- `internal/preparedworkerparity/cmd/embeddedobserver/main.go`
- `internal/preparedworkerparity/cmd/embeddedobserver/main_test.go`
- `internal/preparedworkerparity/cmd/worker/main.go`
- `internal/preparedworkerparity/contract.go`
- `internal/preparedworkerparity/generated/doc.go`
- `internal/preparedworkerparity/generated/prepared_generated.go`
- `internal/preparedworkerparity/handler.go`
- `internal/preparedworkerprobe/cmd/stallworker/main.go`
- `internal/preparedworkerprobe/cmd/worker/main.go`
- `internal/preparedworkerprobe/handler.go`
- `internal/preparedworkerprobe/transaction_handler.go`

### Admission, resources, scripts, workflows, and module policy

- `internal/rubyproof/h1bgenerated/target50_inline_cell_test.go`
- `prepared_worker_admission_test.go`
- `prepared_worker_ci_contract_test.go`
- `prepared_worker_embedded_observer_test.go`
- `prepared_worker_epw2_test.go`
- `prepared_worker_host_admission_test.go`
- `prepared_worker_host_runner_test.go`
- `prepared_worker_performance_test.go`
- `prepared_worker_public_surface_test.go`
- `prepared_worker_resource_darwin_test.go`
- `prepared_worker_resource_linux_test.go`
- `prepared_worker_resource_observer_test.go`
- `prepared_worker_resource_unsupported_test.go`
- `prepared_worker_resource_windows_test.go`
- `purego_boundary_test.go`
- `testdata/purego/exec-allowlist-v1.tsv`
- `scripts/check`
- `scripts/check-prepared-worker-admission`
- `scripts/check-prepared-worker-targets`
- `scripts/check-purego`
- `scripts/check-runtime-parity`
- `.github/workflows/ci.yml`
- `.github/workflows/scheduled.yml`
- `go.mod`
- `go.sum`

### Existing cutover deletions that remain deleted

- `internal/preparednative/availability.go`
- `internal/preparednative/availability_test.go`
- `internal/preparednative/call.go`
- `internal/preparednative/call_arm64.s`
- `internal/preparednative/call_unix_amd64.s`
- `internal/preparednative/call_unix_runtime.go`
- `internal/preparednative/call_windows_amd64.s`
- `internal/preparednative/executable.go`
- `internal/preparednative/executable_darwin.go`
- `internal/preparednative/executable_linux.go`
- `internal/preparednative/executable_linux_test.go`
- `internal/preparednative/executable_test.go`
- `internal/preparednative/executable_unsupported.go`
- `internal/preparednative/executable_windows.go`
- `internal/preparednative/executable_windows_test.go`
- `internal/preparednative/mapping_policy_unix.go`
- `internal/preparednative/mapping_policy_unix_test.go`
- `internal/preparednative/pointer.go`
- `internal/preparedworkerprobe/protocol.go`
- `internal/preparedworkerprobe/protocol_test.go`
- `prepared_runtime_slot.go`
- `prepared_runtime_slot_test.go`
- `preparedplugin/doc.go`
- `preparedplugin/open.go`
- `preparedplugin/open_integration_test.go`
- `preparedplugin/open_supported.go`
- `preparedplugin/open_test.go`
- `preparedplugin/open_unsupported.go`
- `preparedplugin/open_unsupported_test.go`
- `runtime_backend_native.go`
- `runtime_backend_native_arm64_emitter.go`
- `runtime_backend_native_arm64_export_test.go`
- `runtime_backend_native_arm64_external_test.go`
- `runtime_backend_native_candidate.go`
- `runtime_backend_native_stack_test.go`
- `runtime_backend_native_x86_64.go`
- `runtime_backend_native_x86_64_export_test.go`
- `runtime_backend_native_x86_64_external_test.go`
- `runtime_prepared_native.go`
- `runtime_prepared_native_internal_test.go`

Local receipt commands first set `EMBER_S0_EVIDENCE_ROOT=/tmp/ember-s0-$(git rev-parse --short=12 HEAD)-$(date -u +%Y%m%dT%H%M%SZ)` and may create only that candidate-attempt root and its `luau-a`, `luau-b`, `worker-a`, `worker-b`, and `swap-soak` children. The candidate-attempt root must be absent before the first command. Execution never deletes, moves, reuses, or overwrites an earlier attempt's receipt root; a changed exact revision or contaminated acquisition therefore receives a new immutable evidence namespace while the failed attempt remains diagnostic evidence.

## Implementation Steps

After the accepted-plan checkpoint, the main agent executes the following logical changesets in order. Each changeset ends with its named focused proof and diff review before the next begins.

1. **Freeze and stabilize the cutover.**
   **1A — authority and inventory:** update the existing worker cutover plan and `docs/README.md` so stages 1-7 are the only active work, stages 8-17 are gated exactly as above, ADR status is evidence-based, and the current target/workload/threshold/deletion inventory is explicit. Run `! go list -deps ./... | grep -E 'github.com/besmpl/ember/(internal/preparednative|preparedplugin)$'`, `rg -n 'preparednative|preparedplugin|prepared_runtime_slot|runtime_backend_native|runtime_prepared_native' README.md AGENTS.md docs scripts || true`, and `rg -n --glob '*.go' 'preparednative|preparedplugin|prepared_runtime_slot|runtime_backend_native|runtime_prepared_native' . || true`; classify every surviving documentation/history match and require no buildable import or current supported-path claim.
   **1B — generated and focused baseline:** run `go generate ./...`, `go run ./cmd/ember-vmgen -check`, `go test -count=1 ./preparedworker ./preparedworkerbuild`, `go test -count=1 .`, `go build ./...`, `go vet ./...`, focused race, and focused checkptr. Repair only failures caused by the frozen candidate.
   **Acceptance:** one recorded source/toolchain/environment baseline passes reproducibly; no later receipt is accepted against different inputs.

2. **Close Luau prepared semantics and replay.**
   **2A — verified lowering:** make `runtime_backend_analysis.go`, `runtime_backend_facts.go`, `runtime_backend_ir.go`, `runtime_backend_ssa.go`, and `runtime_backend_verify.go` own verified CFG/SSA, liveness, barriers, charge/PC maps, spill maps, and rejection of malformed backend IR; prove with `go test -count=1 -run '^TestBackend(ProtoIR|ProgramIR|RegisterSet)' .`.
   **2B — pre-effect exits:** keep generated guards and fast paths in the listed `runtime_backend_go*` files exact, and bind every unsupported or invalidated path to `runtime_machine_prepared.go` before its first observable effect; prove cancellation, limits, coroutine, metatable, table, call/result, and module parity with `go test -count=1 -run '^(TestMachinePrepared|TestRuntimeParity)' .`.
   **2C — generated parity closure:** regenerate only through owning freshness tests, compare VM/Machine/prepared source-to-result behavior, and reject unaccounted code-size or exit-coverage growth.
   **Acceptance:** every frozen Luau path executes exactly in prepared Go or exits before effects to the canonical Machine with identical result, error, limit, cancellation, and state behavior.

3. **Recertify Luau owner-entry AOT.**
   **3A — capture contract:** freeze `guest_batch_v2`, seeds, checksums, allocation fields, toolchain, the four base points `N={50,500,5000,50000}` with a conservatively calibrated per-case power-of-two call scale, and the median/p90 comparator in `scripts/check-runtime-parity`, `scripts/check-prepared-worker-admission`, and the matching admission tests. Each raw row records actual scaled N and fitted slopes normalize back to one guest call; this preserves the 5 ms evidence floor without forcing already-slow cases through the scale required by exceptionally fast prepared cases.
   **3B — independent pair:** from a clean candidate worktree, run `export EMBER_S0_EVIDENCE_ROOT=/tmp/ember-s0-$(git rev-parse --short=12 HEAD)-$(date -u +%Y%m%dT%H%M%SZ); test ! -e "$EMBER_S0_EVIDENCE_ROOT" && mkdir -m 700 "$EMBER_S0_EVIDENCE_ROOT"`, then run `CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau scripts/check-runtime-parity --phase prepared-parity1x --capture-role candidate --capture-pair a --output "$EMBER_S0_EVIDENCE_ROOT/luau-a"` followed by the same command with `--capture-pair b --output "$EMBER_S0_EVIDENCE_ROOT/luau-b"`; retain both independently gated outputs without worker promotion claims.
   **Acceptance:** both complete compatible captures pass correctness and allocation checks with prepared/Luau median `<=1.00` and p90 `<=1.05`.

4. **Freeze owner-framed identities with reuse bypassed.**
   **4A — identity vectors:** freeze distinct versioned vectors for source/check/language, Program recipe, prepared contribution, Layout/contract, worker BuildID, executable manifest, launch descriptor, and candidate; mutation tests must invalidate exactly the owning vector and downstream products.
   **4B — build closure:** keep one explicit `GOTOOLCHAIN=local` and `-pgo=off` Go command, baseline ISA, selected module/source/header closure, exact Go executable, bounded tool/include trees, target/policy, post-build recapture, atomic publication, executable digest, and byte-limit enforcement in `preparedworkerbuild` and private artifact code.
   **4C — clean-root falsifier:** bypass reusable output and prove clean construction, exact invalidation, post-build drift rejection, publication-path independence, READY mismatch rejection, and preservation of the previous selected artifact on failed work. Run `go test -count=1 -run '^(TestHashToolchain|TestBuildPolicy|TestCanonicalPolicy|TestListedSourceGroups|TestBuildPublishes|TestBuildRequires|TestBuildFailure|TestBuildRejects|TestConcurrent)' ./preparedworkerbuild`, `go test -count=1 -run '^(TestOpenBuild|TestRunnerContractIdentity|TestBuildDescriptor|TestContractIdentity)' ./preparedworker`, and `go test -count=1 -run '^TestPreparedProgramRecipe' .`.
   **Acceptance:** every mutation invalidates exactly its owner and downstream products without cache dependence; build and launch fail closed.

5. **Certify embedded transaction durability.**
   **5A — memory transaction:** prove serialized admission, conflict/gap rejection before guest entry, atomic canonical result/checkpoint/outbox/quiescence commit, duplicate replay without guest entry, stable ordered effect IDs, and post-commit delivery through public `Runner`.
   **5B — durable resolution:** prove `Committed`, `NotCommitted`, and `Unresolved` lookup, quarantine of unresolved work, cancellation boundaries, retryable cleanup, and no blind guest re-entry.
   **5C — file-journal faults:** prove framing, checksums/hash chain, torn-tail handling, sync/write/rename faults, reopen, corruption rejection, and parity with the memory backend. Run `go test -count=1 -run '^(TestModule|TestApply|TestResolve|TestUnknown|TestAuthoritative|TestCommitted|TestFileJournal|TestRunnerContract|TestRichGame|TestJobQueue)' ./preparedworker`.
   **Acceptance:** every admitted operation resolves to one explicit durable state, effects publish only after commit, and all memory/file fault matrices pass.

6. **Certify EPW2 reload and process ownership.**
   **6A — protocol and launch:** prove artifact rehash, HELLO/READY identity match, bounded inherited frames, correlation, cancellation, terminal protocol failure, exact one-exchange Apply, and one `Wait` owner.
   **6B — candidate lifecycle:** prove restore/migrate/warm/follow while active work continues, caught-up quiescent no-I/O activation, candidate isolation, forward-only replacement, and retryable retirement.
   **6C — platform ownership:** prove parent-death/watchdog behavior, bounded close, and child reaping across the Darwin/Linux/Windows implementations and target-specific observers. Run `go test -count=1 -run '^(TestProcess|TestCandidate|TestRunnerClose|TestPrepare)' ./preparedworker` and `go test -count=1 -run '^TestPreparedWorker' .`.
   **Acceptance:** embedded and process adapters produce identical canonical behavior for both rich-game and request/job contracts; failed candidates never disturb active work and every child is reaped.

7. **Acquire and decide S0.**
   **7A — paired worker admission:** run Final Verification commands 8-10 against `$EMBER_S0_EVIDENCE_ROOT/worker-a` and `$EMBER_S0_EVIDENCE_ROOT/worker-b`; require all-37 correctness, worker and embedded versus Luau median `<=1.00` and p90 `<=1.05`, worker/embedded `<=1.50`, and one exchange per timed Apply.
   **7B — transport and resource receipts:** require the command-10 comparison to validate 4,096 exchanges at 1,024-byte request and 14,398-byte response with p99 `<=1,666,666 ns`; run Final Verification command 11 and require 1,024 alternating generations with per-child RSS `<=256 MiB`, aggregate RSS `<=512 MiB`, and zero retained descendants.
   **7C — portable and native matrix:** run Final Verification command 7 for six `CGO_ENABLED=0` cross-builds, then require command 15's six target-native launch/reload/retirement jobs for Darwin/Linux/Windows on amd64/arm64 and physical arm64/x86-64 performance receipts. Separate explicit authorization is required before any commit, push, or CI dispatch; without it, report the complete local state and remain before S0 PASS.
   **7D — promotion:** after every receipt passes on one exact revision, update only the listed maintained docs/ADRs to record S0 and preserve stages 8-17 as unstarted gates.
   **Acceptance:** complete retained S0 PASS; no partial, cached, cross-build-only, emulated-only, or stale-revision evidence promotes the architecture.

After 7D, run the mandatory FAST quick verifier using the commands below. Return non-trivial findings to the main agent, rerun repaired checks, perform the main-agent full diff review, and run Final Verification. Coordinator scratch anchors follow `/Users/mark/.codex/plugins/cache/garyfpga-codex-plugins/simplepower/1.1.0/skills/subagent-driven-development/scratch-ref-workflow.md`; the verifier does not inspect or manage refs and makes no commits.

## Risks

- **Dirty-tree provenance:** freeze the initial status and content inventory, never clean/reset unrelated work, review every final hunk, and bind receipts to the promoted exact revision.
- **Oversized changesets:** each numbered substep has one contract and focused proof; do not combine later-stage repairs or follow-on language work into it.
- **Stale evidence:** any semantic, identity, protocol, schedule, threshold, toolchain, or generated-source change invalidates affected downstream receipts.
- **Post-effect replay:** optimized code may discover a failed guard too late; differential tests must prove every side exit precedes effects.
- **Durability ambiguity:** crash or cancellation near commit can cause unsafe retry; durable resolution and quarantine replace blind re-entry.
- **Cache-masked identity defects:** identity construction is proved with reuse bypassed and post-build input recapture before cache certification is allowed at stage 10.
- **Platform divergence:** local Darwin and cross-build success cannot prove Linux/Windows ownership; all six native jobs remain mandatory.
- **Premature generalization:** S4, Sprig/Seed, Ruby, and the deferred functional/3D language cannot alter stages 1-7 or create shared compiler/runtime APIs.

## Quick Verification

The FAST verifier runs after all active edits and reports command, duration, exit status, and first actionable failure. Expected total time is at most 20 minutes.

1. `go test -count=1 ./preparedworker ./preparedworkerbuild` — within 5 minutes; worker transaction, lifecycle, protocol, and identity tests pass.
2. `go test -count=1 -run '^(TestPreparedWorker|TestPreparedProgramRecipe|TestBackend|TestMachinePrepared|TestRuntimePrepared|TestPreparedBundle)' .` — within 8 minutes; matching prepared and worker contracts pass.
3. `CGO_ENABLED=0 go build ./...` — within 5 minutes; the repository builds without cgo or deleted native/plugin paths.
4. `git diff --check` — within 1 minute; no patch-format or whitespace error exists.

## Final Verification

The main agent runs every command from `/Users/mark/Desktop/ember`, records duration/result, inspects the complete file-scoped diff against this plan, and resolves every in-scope issue before the final checkpoint. Local verification may take up to 3 hours; separately authorized native CI may take another 90 minutes.

1. `go generate ./... && go run ./cmd/ember-vmgen -check` — within 10 minutes; generated artifacts are fresh.
2. `go test -count=1 ./preparedworker ./preparedworkerbuild` — within 5 minutes; focused worker packages pass uncached.
3. `go test -count=1 .` — within 20 minutes; root semantic, parity, worker, resource, and admission-contract tests pass uncached.
4. `CGO_ENABLED=0 go test -count=1 ./...` — within 25 minutes; the complete portable module passes.
5. `go test -race -count=1 ./preparedworker ./preparedworkerbuild` — within 25 minutes; no race is reported.
6. `go test -gcflags=all=-d=checkptr=2 -count=1 ./preparedworker ./preparedworkerbuild` — within 15 minutes; no pointer/lifetime violation is reported.
7. `scripts/check-prepared-worker-targets` — within 20 minutes; all six no-cgo worker/observer cross-builds pass.
8. `test -n "$EMBER_S0_EVIDENCE_ROOT" && test ! -e "$EMBER_S0_EVIDENCE_ROOT/worker-a" && CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau scripts/check-prepared-worker-admission --capture-pair a --output "$EMBER_S0_EVIDENCE_ROOT/worker-a"` — within 35 minutes; clean capture A passes and freezes its schedule.
9. `test -n "$EMBER_S0_EVIDENCE_ROOT" && test ! -e "$EMBER_S0_EVIDENCE_ROOT/worker-b" && CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau scripts/check-prepared-worker-admission --capture-pair b --schedule-from "$EMBER_S0_EVIDENCE_ROOT/worker-a" --output "$EMBER_S0_EVIDENCE_ROOT/worker-b"` — within 35 minutes; clean capture B passes under A's schedule.
10. `scripts/check-prepared-worker-admission --compare-pair "$EMBER_S0_EVIDENCE_ROOT/worker-a" "$EMBER_S0_EVIDENCE_ROOT/worker-b"` — within 5 minutes; all frozen correctness, identity, ratio, transport, and stability gates pass.
11. `test -n "$EMBER_S0_EVIDENCE_ROOT" && test ! -e "$EMBER_S0_EVIDENCE_ROOT/swap-soak" && mkdir "$EMBER_S0_EVIDENCE_ROOT/swap-soak" && CGO_ENABLED=0 EMBER_PREPARED_WORKER_SWAP_SOAK=1 EMBER_PREPARED_WORKER_SWAP_SOAK_OUTPUT="$EMBER_S0_EVIDENCE_ROOT/swap-soak" go test -timeout=35m -run '^TestPreparedWorkerParityAlternatesIndependentStaticAOTGenerations$' -count=1 .` — within 35 minutes; 1,024 swaps and resource bounds pass.
12. `scripts/check` — within 30 minutes; formatting, script self-tests, all Go tests, pure-Go policy, and diff checks pass.
13. `go vet ./... && go build ./...` — within 20 minutes; vet and normal builds pass.
14. `git diff --check && git status --short` — within 1 minute; the main-agent diff review confirms only preserved user work and approved-scope changes.
15. After separate explicit Git/CI authorization, the exact-revision target-native jobs in `.github/workflows/ci.yml` and `.github/workflows/scheduled.yml` — within 90 minutes after dispatch; Darwin/Linux/Windows amd64/arm64 lifecycle and physical arm64/x86-64 performance receipts all pass. Without this evidence, the final checkpoint remains blocked.

## Checkpoint Conditions

1. **Accepted plan checkpoint:** the user gives combined approval for this plan, the `Main agent` route, and immediate current-session execution; the coordinator records the checkpoint and invokes `simplepower:subagent-driven-development` with this exact plan path and route.
2. **Final reviewed/verified implementation checkpoint:** stages 1-7 and their focused acceptances are complete, stages 8-17 and the functional/3D language remain unstarted, the mandatory FAST verifier passes, every non-trivial verifier issue is repaired by the main agent, the complete diff review matches the approved scope, all local final commands pass, and all exact-revision target-native and physical-ISA receipts pass.
