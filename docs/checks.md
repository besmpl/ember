# Checks

Run the smallest check set that proves the change, and prefer the stronger set
before commits or pushes.

## Inner-Loop Order

Start with the focused package test that proves the edited behavior. When the
change belongs to one ownership area, run `scripts/check-lane <lane>` as the
lane-first iteration check for that area. Use `scripts/check-fast` as the
pre-finish sweep after the focused lane is green. Use `scripts/check` for the
stronger final proof. Use `scripts/check-full` only when explicitly requested.

## Fast Helper

```sh
scripts/check-fast
```

It runs the current fast repository check.

## Performance Audit

Use the repeatable audit runner for five-sample comparisons across the
Scenario Ember, recursive Fibonacci, sparse-grid, 256 KiB compiler-stage, and
runtime-mode benchmark families:

```sh
scripts/performance-audit --output /tmp/ember-audit-baseline-a --profiles
scripts/performance-audit --output /tmp/ember-audit-baseline-b --profiles
```

Derive a gate manifest from two complete captures of the same source and
environment, then compare a candidate capture against either baseline:

```sh
scripts/performance-audit-derive-manifest \
  --before /tmp/ember-audit-baseline-a \
  --after /tmp/ember-audit-baseline-b \
  --output /tmp/ember-audit-gates.tsv

scripts/performance-audit-compare \
  --before /tmp/ember-audit-baseline-a \
  --after /tmp/ember-audit-candidate \
  --manifest /tmp/ember-audit-gates.tsv \
  --baseline-role a
```

The runner retains one raw benchmark file for each of the five families. The
manifest derives a separate robust timing envelope and observed allocation
ceiling for every benchmark row from both baseline captures. It binds distinct
A/B roles to hashes of each baseline's metadata, shared environment artifact,
capture facts, and five raw files; profiles and human-readable summaries are
evidence, but not gate inputs. The
comparison command parses retained raw files through `scripts/bench-summary`,
reports runtime timing, compiler timing, B/op, and allocs/op separately, and fails
closed for missing rows or metrics, incomplete captures, source or baseline
commit mismatches, incompatible environments, or capture-contract drift.

The output directory must not already exist. The runner requires a clean
worktree; pass `--allow-dirty` when the current changes are intentionally part
of the capture. Set `BENCHTIME` to shorten exploratory runs. `--profiles`
writes unique CPU and allocation profiles for each family and never overwrites
an existing capture. Raw `go test` output is retained for all five families;
`scripts/bench-summary` provides the shared table and TSV parsing contract.
Failed runs leave an `INCOMPLETE` marker in the newly created directory.

## Runtime parity and speed

`full` and `speed2x` both use the frozen corpus-qualified inventory: 10 Top10,
2 Classic, and 25 Scenario cases. `full` is the all-37 correctness capture;
`speed2x` additionally names the dynamic-runtime acceptance intent. Both use
the breaking `guest_batch_v2` contract: one parameterized program and callable
per case, one positive runtime seed shared by every N point and repeat, runtime
base points `N={50,500,5000,50000}` multiplied by one per-case power-of-two
call scale, one timed outer entry that executes the resulting N seed-dependent
guest calls, exact integer checksums, and three fitted-slope repeats. The
runner calibrates the scale conservatively against a 10 ms prepared/dynamic
Ember target, records every actual N in `raw.tsv`, and normalizes the fitted
slope back to one guest call. Every engine/repeat must still spend at least 5
ms at its scaled maximum point or acquisition fails before the slope is
accepted. Public-call lifecycle and allocation measurements remain separate.
Contaminated points are retried but never accepted. The intrinsically slow
full VM all-37 capture receives a 60-minute test timeout; dynamic and prepared
captures retain 35 minutes. These explicit bounds replace Go's brittle
10-minute default. Output directories are caller-owned and must not already
exist.

```sh
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase full \
  --output /tmp/ember-parity-full

CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x --capture-only \
  --capture-role frozen-current --capture-pair a \
  --output /tmp/ember-speed2x-a
```

`--capture-only` preserves correct, uncontaminated evidence even when the
future 2x target is missed. It does not waive environment, result, row,
contamination, or slope validation. Gate two independent captures and derive
their immutable manifest separately:

The capture role selects the whole runtime before binding: `frozen-current`
uses `EMBER_RUNTIME_ENGINE=vm`, while dynamic `candidate` uses
`EMBER_RUNTIME_ENGINE=machine`. `prepared-parity1x` accepts only `candidate`
and selects `EMBER_RUNTIME_ENGINE=prepared`. In test builds, that phase binds
the checked-in generated all-37 bundle by verified Program hash and Proto
inventory; an unknown or stale Program fails before owner creation. The bridge
enters the bound owner directly so `guest_batch_v2` does not include
`Runtime.Invoke` lifecycle work. Separate public-path tests prove that the same
verified bundle executes through `Runtime.Invoke`, owns and detaches results
correctly, and closes independently. The phase and execution mode are recorded
in schema-v2 artifacts and `command.txt`; workload identity never participates
in engine selection.

## Prepared worker admission

The supervised reload gate measures the same combined 37-module static-AOT
Program through two deployment adapters: an embedded release `Runner` and one
supervised no-cgo EPW2 process. It does not use a test-binary worker or rebuild
one executable per case. Each timed `Apply` is one typed request and one typed
decision; build, launch, restore, catch-up, activation, and retirement remain
outside the fitted guest slope.

The embedded all-37 slope is observed inside a small, content-hashed
production build of `internal/preparedworkerparity/cmd/embeddedobserver`. The
root test binary also links large generated test fixtures and is not the release
compiler shape. Its private line protocol therefore transports only the case,
iteration count, returned checksum, and internally measured duration; pipe time
is outside the sample. The timed interval is the public `OpenEmbedded`
`Runner.Apply`, including the same durable journal work used by an embedded
host. `embedded-observer-build.tsv` binds the helper executable digest and
target to the capture. This helper is evidence tooling, not an alternate
runtime path.

Acquire two independent runs and compare them:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-prepared-worker-admission \
  --capture-pair a --output /tmp/ember-worker-a

CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-prepared-worker-admission \
  --capture-pair b --schedule-from /tmp/ember-worker-a \
  --output /tmp/ember-worker-b

scripts/check-prepared-worker-admission \
  --compare-pair /tmp/ember-worker-a /tmp/ember-worker-b
```

Every capture contains `worker-build.tsv`, which binds the observed EPW2
generation to the contract, Program recipe, Program hash, generated Go,
complete build-source closure, toolchain/target descriptor, build ID, and
executable digest. `OpenDevelopment` succeeding proves that the activated
worker returned the same identities in `READY`; raw timing rows cannot be
accepted without this chain.

Capture A calibrates each case conservatively to the 10 ms target and freezes
its power-of-two call scale. Capture B reuses that exact schedule but requires
only the hard 5 ms evidence floor; it does not require ordinary run-to-run
timing variation to reproduce A's 10 ms calibration observation. Capture B
buffers one complete three-sample prescribed calibration before publication;
a structurally sub-floor set is discarded and reacquired at most three times,
while measurement errors remain terminal and rejected samples are never
emitted. For each fitted all-37 point, each engine is measured three times and
the engine order rotates between trials. All 108 raw rows per case are
retained, while the fitted point is the median of its three trials. The
admission script independently reconstructs those point medians and slopes
from `raw.tsv`; a missing trial, changed median, or mismatched result fails
closed. This prevents one scheduler pause from defining a positive but
meaningless fit without weakening any ratio threshold or hiding an underlying
measurement. Each repeat is one acquisition block: all three engines run at
every point, and their order rotates across trials and points. The comparator
therefore divides slopes only within the same repeat. It sorts the three
matched ratios, uses the second as the median, and uses the third (the worst
matched repeat) as nearest-rank p90. Cartesian recombination is invalid here:
it can divide the slowest numerator time block by the fastest denominator time
block and turn common frequency or host drift into an unrelated hidden
stability gate. The admission script independently derives the same blocked
ratios from `slopes.tsv`. These thresholds apply to the
maximum-minus-minimum measured guest-work span,
not total `Apply` latency, so journal/fsync intercept cannot admit a
noise-dominated slope. The all-37 worker/Luau and embedded/Luau gates are median
at most `1.00` and p90 at most `1.05`. The production-shaped rich-turn gate
separately requires
worker/embedded slope at most `1.50`, an exact semantic trace, and one exchange.
`host-latency.tsv` retains both absolute durable-turn distributions and the
paired clamped difference as diagnostics; the difference is not an IPC gate
because independent journal `file.Sync` tails do not cancel statistically.

The separate `transport-latency.tsv` receipt measures 4,096 steady-state
exchanges through the actual `processBackend`, EPW2 codecs, a real child
process, and OS pipes. Its bounded observer sends a 1,024-byte request and
receives a 14,398-byte maximum-shape decision; it does not execute guest code
or touch the journal. `transport-summary.tsv` binds every raw sample, observer
binary, target, and capture identity, and requires p99 at most `1,666,666ns`
(10% of a 60 Hz frame). This isolates codec/IPC cost while the production AOT
worker, semantic turn, durability, resources, and one-exchange shape remain
bound by the other artifacts in the same capture.

The alternating-generation lifecycle gate builds independent standard and
holdout Programs once, then performs exactly 1,024 development
`Reload.Prepare`/candidate `Activate` swaps and a semantic transaction after
every activation:

```sh
mkdir -p "$PWD/prepared-worker-soak"
CGO_ENABLED=0 \
  EMBER_PREPARED_WORKER_SWAP_SOAK=1 \
  EMBER_PREPARED_WORKER_SWAP_SOAK_OUTPUT="$PWD/prepared-worker-soak" \
  go test -run \
  '^TestPreparedWorkerParityAlternatesIndependentStaticAOTGenerations$' \
  -count=1 .
```

The resource observer starts only after both immutable workers are built. It
records one live child initially, exactly two children after every successful
candidate preparation, and zero descendants after `Runner.Close`. Each live
process must remain at or below 256 MiB RSS and aggregate worker RSS at or below
512 MiB. `resource-samples.tsv` is published before gate evaluation so a failed
run retains every deterministic lifecycle point for diagnosis. A failure never
publishes `resource-summary.tsv`; that file is written last only as the PASS
marker and binds both worker build IDs, target, Go version, swap count, peaks,
frozen budgets, and the exact sample file by SHA-256. Process-tree attribution
rejects creation-order-impossible edges so rapid PID reuse cannot attach an
older unrelated process to a newly launched worker. The observer is test-only
and does not widen `Runner`, `Reload`, or EPW2.

The process launcher owns the parent ends of explicit `os.Pipe` transports.
It does not use `exec.Cmd.StdinPipe` or `StdoutPipe`: `Cmd.Wait` closes those
managed endpoints after process exit and can otherwise race the parent's final
`CLOSED` decode. `TestOSProcessLauncherRetainsFinalFrameAfterWait` starts a
real helper process, waits for exit first, and then proves that the complete
final protocol frame remains readable. The one-Wait process owner and ordinary
transport cleanup remain unchanged.

Cross-build all declared worker targets with:

```sh
scripts/check-prepared-worker-targets
```

This covers Darwin, Linux, and Windows on ARM64 and x86-64 with `CGO_ENABLED=0`.
Cross-compilation is only build evidence. CI executes the process lifecycle on
target-native jobs for all six pairs. Pull-request Linux x86-64 admission
retains paired Luau captures and the matching 1,024-swap receipt for the same
commit in one artifact; scheduled physical Darwin ARM64 and Linux x86-64 jobs
retain independent lifecycle receipts. Paired Luau admission also runs on the
GitHub-hosted physical Darwin ARM64 runner, never on a developer machine. The
Darwin profile requires Go 1.26.4 and the official Luau 0.728 ARM64 executable
at SHA-256
`22571bbeea6bae3e6b2b3d6cbe41da9d6f3e3a7dd9edd1660b006476ce46db78`;
the downloaded archive is pinned at SHA-256
`60541670fc8b8a8289df3ff37bd88e81b8f7b45219b777d6f4afd4e8e3af07ec`.
The Linux profile requires Go 1.26.4 and the
official Luau 0.728 Linux executable at SHA-256
`2a6ff9e7c17a0a6fed47c04da67495d1594eda38ce915f01c78c7fa5e9e796b8`.

Acquisition starts after three one-second samples with aggregate CPU at most
300%. One-minute load remains diagnostic but is not an admission gate: it is
lagging, core-count-blind, and includes blocked work that may not compete with
this single-threaded benchmark. The runner gives its all-37 acquisition test the
same 35-minute bound documented for one capture instead of inheriting Go's
shorter default test timeout.
Live before/after probes cap external processes
at three cores while excluding the measuring Go process. A live point whose
before or after probe is contaminated is discarded and retried after one
second, up to 300 attempts; only clean paired rows are emitted, and exhaustion
still fails the capture. A failed sampling command is a distinct observer
error, not runner contention. An external-engine 60-second deadline discards
that point and enters the same bounded reacquisition loop; its partial timing
is never emitted or accepted. Capture B applies the same bounded whole-set
policy to a structurally sub-floor prescribed calibration: at most three
complete three-sample attempts, with no rejected calibration rows emitted.
The three-engine worker gate also buffers one
complete fitted repeat before publication. A structurally invalid window or
non-positive/non-finite fit discards that entire repeat and reacquires it up to
three times; rejected rows are never emitted, while semantic or protocol
errors still fail immediately and ratio failures are never retried. A ratio
failure derived from the three-trial point medians is terminal; captures are
never rerun merely to select a passing ratio.

```sh
scripts/runtime-ratio-gate --derive \
  --baseline-a /tmp/ember-speed2x-a \
  --baseline-b /tmp/ember-speed2x-b \
  --output /tmp/ember-speed2x-baselines-v2.tsv

scripts/runtime-ratio-gate --capture-a /tmp/ember-speed2x-a \
  --capture-b /tmp/ember-speed2x-b --report-only \
  --baseline-manifest /tmp/ember-speed2x-baselines-v2.tsv
```

The ratio gate rejects any missing, extra, duplicate, contaminated, malformed,
nonpositive, cross-schema, dynamically relabeled, seed-changing, result-
mismatched, workload-mismatched, or provenance-mismatched row. It recomputes
every slope result-set hash from raw integer checksums and independently checks
all 37 cases in both captures. Within one capture, every repeat is an
acquisition block containing both order-rotated engines, so the gate forms
three same-repeat Ember/Luau ratios. Their second value is the median and their
third (worst matched repeat) is nearest-rank p90; cross-repeat Cartesian
recombination would turn common host drift into an unrelated stability gate.
Dynamic acceptance defaults to median at most 1.85 and p90 at most 2.0.
Prepared final evidence uses median at most 1.0 and p90 at most 1.05.
Candidate/current comparisons are separate captures rather than acquisition
blocks, so they retain Cartesian comparison, bind the manifest and both
baseline directories, and require each paired median to stay at or below 1.05.

Allocation evidence is separate. Capture two baselines with explicit pairs,
derive the exact 56-row ceiling manifest, then compare candidate evidence:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 scripts/runtime-allocation-gate --capture \
  --capture-role frozen-current --capture-pair a \
  --output /tmp/ember-runtime-alloc-a
```

Warmed B/op and allocs/op cannot exceed their frozen ceilings. Cold allocation
counts cannot rise; cold byte and retained-state snapshots remain report-only.
A finite snapshot cannot establish unbounded growth, so a later repeated-growth
test must supply that evidence before retained bytes can become a blocking gate.

## External producer and compiler boundaries

The focused proof copies an independently versioned producer module to a
temporary root, uses only a local Ember replacement with the network disabled,
materializes two application-owned mounts, deletes all producer tooling, and
then lists, tests, builds, inspects, and executes a fresh-cache static
application with `CGO_ENABLED=0`:

```sh
go test -count=1 \
  -run '^TestExternalProducerBuildsConcreteGeneratedApplication$' \
  ./preparedsource
```

Run its explicit six-target compilation extension when prepared-source or
external-delivery policy changes:

```sh
EMBER_EXTERNAL_PRODUCER_CROSS=1 go test -count=1 \
  -run '^TestExternalProducerBuildsConcreteGeneratedApplication$' \
  ./preparedsource
```

The second command cross-builds Darwin, Linux, and Windows on ARM64 and x86-64;
only the current host executes the result. This is static delivery and
dependency-absence evidence, not Ruby/Sprig semantics, target-native lifecycle,
worker reload, or performance evidence.

The companion external-compiler proof derives generated Go from checked Seed
source rather than storing the output as fixture text. It copies the separate
module out of Ember, validates deterministic syntax/check/evaluator/emitter
behavior and the exact `preparedsource`-only dependency, repeats compilation,
then deletes compiler and source before an offline application build:

```sh
go test -count=1 \
  -run '^TestExternalCompilerBuildsFromCheckedSourceThenDisappears$' \
  ./preparedsource
```

The default test proves exact Program/Set/Layout/application identities,
deterministic diagnostics, positive and ordered domain behavior, cancellation,
limits, application-owned placement, compiler/tooling absence, zero steady
scalar allocations, `CGO_ENABLED=0`, and exact module/package/symbol absence.
Run its opt-in local steady comparison and six-target compilation separately:

```sh
EMBER_EXTERNAL_COMPILER_PERF=1 go test -count=1 -v \
  -run '^TestExternalCompilerBuildsFromCheckedSourceThenDisappears$' \
  ./preparedsource

EMBER_EXTERNAL_COMPILER_CROSS=1 go test -count=1 -v \
  -run '^TestExternalCompilerBuildsFromCheckedSourceThenDisappears$' \
  ./preparedsource
```

The performance extension takes seven generated and equivalent handwritten
samples and gates their medians at 1.10x. It is dirty-host architecture evidence,
not a retained benchmark. The cross extension builds Darwin, Linux, and Windows
on ARM64 and x86-64; only the current host runs the binary.

## Mixed real-compiler static composition

The mixed proof invokes the checked internal Sprig compiler and external Seed
compiler by their concrete APIs, combines their independent Sets in one
application-owned Layout, and emits one explicit typed application handler:

```sh
go test -count=1 \
  -run '^TestMixedRealCompilersComposeWithoutSharedABI$' \
  ./preparedsource
```

The default proof repeats compilation and exact identities, permutes Layout
inputs, checks both canonical evaluators, deletes copied compile tooling, then
builds a fresh six-file zero-dependency application offline with cgo disabled.
It verifies direct Sprig-before-Seed ordering, typed domain results,
pre-cancellation, distinct per-language limits, Seed-stage cancellation, nil
context, detachment, zero positive-path allocations, exact imports/modules,
and compiler/delivery symbol absence. It is static composition evidence, not
S5b/S7 worker-reload proof.

Run its bounded local composition comparison and six-target compilation as
separate opt-in extensions:

```sh
EMBER_MIXED_COMPILER_PERF=1 go test -count=1 -v \
  -run '^TestMixedRealCompilersComposeWithoutSharedABI$' \
  ./preparedsource

EMBER_MIXED_COMPILER_CROSS=1 go test -count=1 -v \
  -run '^TestMixedRealCompilersComposeWithoutSharedABI$' \
  ./preparedsource
```

The performance extension runs seven order-rotated handler/direct pairs and
gates the median ratio at 1.05x; both paths must remain allocation-free. This
is local dirty-host architecture evidence. The cross extension builds Darwin,
Linux, and Windows on ARM64 and x86-64; only the current host executes the
binary.

## Internal Sprig architecture proof

`internal/sprigproof` is the proof-only ADR 0012 S6 slice, not a public or
general Sprig language. Its default lane checks the bounded parser/checker,
nominal checked project facts, canonical evaluator, full transitive import
closure, requested standalone and application-linked preparation, exact
generated fixtures, deterministic name/package/link hygiene, semantic and
ownership differentials, allocation ceilings, and fresh-cache static
applications:

```sh
go test -count=1 ./internal/sprigproof/...
CGO_ENABLED=0 go test -race -count=1 ./internal/sprigproof/...
go vet ./internal/sprigproof/...
```

The single-package application uses an exact three-file source inventory and
fresh offline caches, asserts `CGO_ENABLED=0` from build information, checks
the exact non-standard dependency graph, rejects delivery/compiler names in
build info and linked symbols, and executes the generated result. The project
applications separately prove application-owned Layout for both independently
mountable entry closures and direct linked execution of a
`counter -> math -> util` package graph beside an unrelated package. Linked
tests cover project-wide binding tables, exact reachable output, direct-path
identity attribution, lowercase nominal wrappers, positive and ordered domain
results, pre-cancellation, limits, and zero-cgo offline builds. The Go 1.26
cache observers decode `go list -export -json`, hash built executables, and
attribute fresh, warm, entry-only, dependency, and unrelated compile actions
and BuildIDs through the actual package cone. These are exact local structural
observations, not stable Go trace syntax or wall-time claims.

Fixture regeneration is explicit and reviewable:

```sh
EMBER_UPDATE_SPRIG_FIXTURE=1 EMBER_UPDATE_SPRIG_PROJECT_FIXTURE=1 \
  go test -count=1 \
  -run '^(TestGeneratedFixtureIsFreshAndStdlibOnly|TestProjectGeneratedFixturesAreFresh)$' \
  ./internal/sprigproof
```

Run the repeated local performance gate with the exact environment below:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 EMBER_SPRIG_PERF=1 \
  go test -run '^(TestOptInProjectPerformanceGate|TestOptInPerformanceGate)$' \
  -v -count=1 \
  ./internal/sprigproof
```

Both gates use seven cyclically ordered paired rounds and require generated
execution to be at least 2x the evaluator and no more than 1.10x equivalent
context-polled handwritten Go by median. Always-on tests require zero generated
scalar allocations and one allocation for a detached one-element result. A
pass is dirty local architecture evidence only. Retained promotion evidence
still requires a clean quiet host, fixed toolchain and benchtime,
source/binary identities, raw per-round samples, and an independent repeat
capture.

ADR 0012's S6b package-shape decision additionally used a disposable,
identity-recorded real-emitter harness at 2 x 32, 4 x 128, and 8 x 256. It is
not an always-on timing gate: local wall time and `/usr/bin/time -l` RSS are
host observations, and a hidden auto-crossover would be less stable than the
selected explicit standalone/linked products. A future retained repeat must
pin the same sources, Go/tool/target/flags, cache prewarm, rotated order, raw
samples, executable identities, compile traces, and semantic outputs rather
than comparing one ad hoc timing.

## Internal Ruby architecture proof

`internal/rubyproof` is the proof-only ADR 0012 Ruby D1 slice, not a public or
general Ruby implementation. Its default lane checks bounded source parsing
and diagnostics, a sealed passive lookup image, inherited lookup and selective
ancestor invalidation, checked alias/remove/undef and
public/protected/private entry transitions, literal-selector `send` and
`public_send`, checked `include`/`prepend` attachment topology and later module
definitions, independent canonical/generated owners, fixed eager cells, three
two-arm direct-target sites, block capture and non-local return,
`RuntimeError`/`NoMethodError` rescue and ensure ordering, single-owner
admission, corruption and abort behavior, fixture freshness, zero-allocation
warmed generated execution, and a fresh offline static application with no
compiler, Ember runtime, or delivery graph:

```sh
go test -count=1 ./internal/rubyproof/...
CGO_ENABLED=0 go test -race -count=1 ./internal/rubyproof/...
go test -gcflags=all=-d=checkptr=2 -count=1 ./internal/rubyproof/...
go vet ./internal/rubyproof/...
go test -count=1 -run '^TestPureGoBoundary$' .
```

The isolated Target44 H1a baseline and Target45 object-sidecar comparison are
included in the package commands above. Their smallest focused checks are:

```sh
go test -count=1 -run '^TestH1' ./internal/rubyproof
go test -race -count=1 -run '^TestH1' ./internal/rubyproof
go test -gcflags=all=-d=checkptr=2 -count=1 -run '^TestH1' ./internal/rubyproof
CGO_ENABLED=0 go test -count=1 -run '^TestH1' ./internal/rubyproof
```

Those tests prove only two Ruby-private fixed-capacity representations and their
fair lifetime/economy comparison, not allocation-triggered evaluator
collection, H1b source lowering, generated dynamic singleton calls, production
heap capacities, or elapsed performance. They freeze the existing generated
artifact so neither proof can silently put heap machinery on the Target41/42
direct leaf.

The accepted Ruby 2.6 source is 3,330 bytes/209 LF with SHA-256
`8f2611ea5042f09f8313c1291af5b44a5bbf8e76ae3733e0745579e8c838c168`.
Its original 2,480-byte C4 prefix remains exact at SHA-256
`aad50c456aad030a12365e85596cdc30ac1a8d85437c9adcacb6dbeb2c9907a5`;
the 850-byte/51-LF Target39 extension is
`4c3388faa84323c4382ec815006e1a799f74b85738d4bd20148f8bb92429acf0`.
The canonical evaluator and generated package both produce the unchanged C4
line followed by `2|11|2|22|13|1|713726`; the two lines plus final LF hash to
`01c3e3bbbf334e42fe3940a0ef2c8d2e25ec0891b23ffc36a3bc9020ce066ea0`.
The generated sites together record exactly six hits, five cold admissions,
five stale misses, five repairs, five uncached fallbacks, five admissions, zero
evictions, and five occupied arms. Base redefinition and visibility mutation
change Alpha and Delta while preserving Beta/Gamma and every saved-alias cell.
Beta removal changes only Beta and exposes Base v2; Gamma undef changes only
Gamma to a terminal negative receipt. The Target39 transaction vector is
`K=0/2/0/1/2`: detached module definition, include, empty prepend, late
prepended-module definition, and included-module redefinition. The empty
prepend publishes its topology bit even at maximum epoch while changing no
cell, epoch, or arm.

Fixture regeneration is explicit:

```sh
EMBER_UPDATE_RUBY_FIXTURE=1 go test -count=1 \
  -run '^TestPreparedSourceIsOneLocationNeutralSetAndFixtureIsFresh$' \
  ./internal/rubyproof
```

Run the attributable local CRuby differential only on the pinned observer:

```sh
EMBER_RUBY_ORACLE=1 go test -count=1 -v \
  -run '^TestRubyProofAgainstPinnedLocalCRuby$' \
  ./internal/rubyproof
```

That opt-in test requires `/usr/bin/ruby` engine/version/platform
`ruby|2.6.10|universal.arm64e-darwin24` and executable SHA-256
`0c6118f2b4a9fe448d51eba2e79c81342ae1e111f015d58dce5c11740f4dfacd`.
It is a bounded local oracle, not a RubySpec or full-Ruby compatibility claim.

Run the historical direct-`Hot` calibration and six-target static compilation
separately:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 EMBER_RUBY_PERF=1 \
  go test -count=1 -v \
  -run '^TestOptInRubyPreparedDirectHotCalibration$' \
  ./internal/rubyproof

EMBER_RUBY_CROSS=1 go test -count=1 -v \
  -run '^TestRubyPreparedApplicationBuildsOfflineWithoutCompilerOrRuntimeGraph$' \
  ./internal/rubyproof
```

The calibration uses seven cyclically ordered canonical/prepared/handwritten
rounds and retains the old 2x/1.15x thresholds. It measures the monomorphic
direct `Hot` body, not D1's polymorphic `read_label` site, so it cannot promote
D1/C4 or open-world Ruby performance. The always-on allocation tests require
zero allocations for warmed generated `send`/`public_send` and the complete
prepared proof run after owner setup. They do not measure latency: the current
generated call path still performs explicit `context.Context` polls, whole-site
validation, numeric definition/target switches, frame accounting, and a hit
counter update. Retained D1 performance evidence still needs independently
identified lanes, pre-repaired alternating hits, separate repair/fallback
costs, compiler/disassembly receipts, and clean repeated captures. The cross
extension builds Darwin, Linux, and Windows on ARM64 and x86-64 with cgo
disabled; only the current host executes the result.

### Target48 H1b observer preflight

Target48 adds independent same-layout Current/Equivalent microkernels for the
balanced warmed H1b selection vector and the exact three-collection H1b
lifetime cycle. Run the deterministic semantic, isolation, layout, receipt,
and zero-allocation gates with:

```sh
GOFLAGS=-pgo=off CGO_ENABLED=0 GOMAXPROCS=1 GOGC=off \
  go test -count=1 -run '^TestTarget48' \
  ./internal/rubyproof/h1bgenerated
```

The smallest structural benchmark smoke is:

```sh
GOFLAGS=-pgo=off CGO_ENABLED=0 GOMAXPROCS=1 GOGC=off \
  go test -count=1 -run '^$' \
  -bench '^BenchmarkTarget48H1b(BalancedHit|Collector)(Current|Equivalent)$' \
  -benchtime=1x -benchmem \
  ./internal/rubyproof/h1bgenerated
```

`-benchtime=1x` proves only that all four benchmark entrypoints complete and
emit their semantic sidecars. Its elapsed values are not performance evidence.
The bounded claims are:

- one fixed 64-call, 32/32, post-admission warmed hit vector at one wrapper
  callsite per lane, with exact checksum and every final field that `readLabel`
  can inspect or mutate; and
- one sequential 65,536-image, 29,360,128-byte bank of exact three-collection
  cycles, after two complete untimed warm batches, with exact final
  generations/free chains and checksum `10*N`. Cache residency and miss rate
  are host-dependent and unclaimed; this is not a resident-hot single-arena
  benchmark.

The pure Target48 protocol preflight is also selected by `^TestTarget48`. It
freezes the exact A/B, family-interleaved, always-acquired reserve schedules;
the separately framed GOOS/GOARCH attempt identity and fail-closed four-start
stopping policy; a strict
64-KiB one-direct-process parser; and central 99% BCa plus MAD, drift, order,
and nuisance formulas with analytic and seeded goldens. It deliberately runs
no subprocess and writes no capture artifact. A noise-limit breach is terminal
unfavorable evidence rather than a retryable attempt, avoiding
outcome-dependent optional stopping.

Acquisition remains disabled and there is intentionally no capture command.
ADR 0012 lists the missing effectful runner, durable pre-warmup attempt and
raw-coordinate/content manifests, authenticated CPU warmup and host controller,
replacement closure, and concrete compiler/disassembly receipts. A qualifying
host must enforce and attest a pinned physical core, fixed non-turbo frequency
and turbo policy, migration absence, thermal bounds, and background-load
bounds. The current Apple M1/macOS host is not qualifying; `GOMAXPROCS=1`
alone is insufficient. Do not substitute Target42's retained
20-round/500ms protocol or promote exploratory output.

Target49 freezes this route rather than growing a runner-shaped or generic
acquisition module. Reopen it only when a qualifying independent controller, a
reviewed durable effect owner, and a concrete decision that depends on the
frozen gates all exist. Before acquisition on another GOOS/GOARCH, the pure
seeded floating-point goldens must pass there; a local pass is not a
cross-architecture numeric receipt.

### Rejected open-world PIC preflight

A disposable three-class Ruby preflight tested two fixed Alpha/Beta PIC arms,
an uncached Gamma receiver, and a Beta-only method redefinition. Its exact
source SHA-256 was
`af419478b6304f176c3dee0aa161aa426fe2a97edd281929a5585931f9ce2193`.
Pinned CRuby, the real checker/canonical evaluator, and the proof-local
candidate agreed on
`true|false|41|42|44|41|43|43|44|11|233|44`; the candidate recorded four hits,
one stale-Beta miss, one repair, and two uncached fallbacks. Code inspection
confirmed that the stale arm exited before the new `mark` effect, repaired only
Beta, preserved Alpha, and never grew the two-arm cache.

The candidate's performance observer was rejected rather than retained. Its
handwritten comparator called the prepared implementation; its timed loop
included the first repair and Gamma fallback; and a nominal hit still scanned
and interpreted method plans. It also manually substituted part of object and
schedule setup, overflowed the effect trace during long Go timing, incompletely
matched control behavior, and required the pinned local CRuby process in the
default lane without a timeout. Consequently, the locally reported 33.901x
canonical/prepared and 0.998x prepared/handwritten medians are invalid and are
not performance evidence. Candidate SHA-256
`55d00bd741a6bf45a61c8704cdf2690b6df63890dcf689e9f8dc8b13b4982230`
was deleted; there is intentionally no command to rerun it.

A future R1 open-world performance gate is admissible only when it:

1. lowers a direct source-derived target into each fixed arm, keeping method
   lookup and generic recovery off hits;
2. compares with an independent but representation-, admission-, control-, and
   recovery-equivalent handwritten implementation;
3. repairs and warms Alpha/Beta before timing, times only alternating hits, and
   records exact zero miss/repair/uncached statistics for the timed interval;
4. measures stale-Beta patch/first recovery and Gamma fallback separately,
   including deterministic allocation observations;
5. derives constructor, shape, method, effect, and call-schedule facts from the
   checked source and preserves Ruby integer, cancellation, limit, poisoning,
   and cleanup behavior;
6. runs the pinned CRuby oracle only by explicit opt-in with a bounded process
   timeout; and
7. repeats the existing seven-order protocol in two independent captures,
   requiring zero prepared/handwritten hit allocations, at least 2x over the
   canonical send, and at most 1.15x equivalent Go while separately reporting
   code size, heap/RSS, build-to-READY, and reload costs.

The selected candidate shape for that gate is a Ruby-private, fixed two-arm
target-ID PIC. Each owner-local arm contains only scalar class, shape, resolved
method-slot/index when required, effective-lookup-epoch, and target-ID facts;
the selector is a generated call-site constant. The generated package owns a
bounded target inventory and a site-local numeric `switch` whose cases directly
call named method bodies. Alpha/Beta arms may repair after successful canonical
recovery; Gamma never grows or evicts the cache. Invalid/default IDs, epoch
exhaustion, unsupported targets, raises, non-local unwind, cancellation, and
limits must not publish an arm. Checkpoints and reload records must contain no
Ruby object, function, epoch, target ID, or cache state.

An occupied zero, unknown, or class/slot/definition-incompatible target ID is
internal corruption and must poison before effects. It is not the same as a
valid resolved definition lacking an emitted target, which remains a usable,
uncached canonical fallback. Epoch changes must be owner-serialized,
monotonic, and exhaustion-checked. If the proof uses bounded host integers,
its source-derived admission proof must cover every effect and return
intermediate before capture; direct and fallback behavior may not diverge or
partially mutate on overflow.

### Rejected direct-target observer

The first disposable implementation of the selected target-ID shape did not
reach timing. Its exact source SHA-256 was
`6f87554652f95cd6b0cbb665f9d073251a76cb2e81c75e80c29e776dc3e5cb2e`;
the observer and generated files had SHA-256
`6d0197a07c457a6e61e48c47153ac1450f9aef2f3abce52020c2975ac483518b`
and `5e4fd82bfc8810fa965508b9a81bb6742a6959078d202eeebcc449cee6454cbd`.
The permitted non-performance preflight failed three ways: generated bytes
were stale against their emitter, the expected fact record omitted actual
definition IDs, and a changed receiver schedule passed the claimed exact-source
negative boundary.

Primary inspection and two independent reviews found additional blockers.
Invalid occupied target IDs silently repaired, and a valid target from the
wrong class could dispatch the wrong body. Fallback used preselected scalar
facts rather than canonical selector/slot lookup. The tests did not establish
operand/block order, real targeted unwind/ensure, control parity, complete
integer bounds, serialized epoch mutation, or checkpoint/reload lifetime. The
two timing captures were in-memory orders in one process with no independent
raw receipt, timed effect checksum, non-hit allocation vector, or separate
code/build/RSS/reload accounting. No performance or CRuby command was run, and
the disposable files were deleted after review.

Only mechanism-shape evidence survives: two five-scalar arms, a local numeric
switch with named direct bodies and no hit scan, stale-Beta repair after normal
fallback plus revalidation, Alpha preservation, and uncached Gamma for one
uncorrupted sequential flat-class owner. Review rates measurement validity
1.5/10, semantic validity 2.4/10, lifetime validity 3.0/10, and that narrow
mechanism 7.0/10. The selected representation remains a 9.6/10 design and
open-world performance evidence remains 7.2/10. There is intentionally no
command for the rejected observer, and inline-body A does not reopen because B
failed preflight rather than the 1.15x performance gate.

A replacement observer must pass source facts, generated freshness,
corruption, semantics, control, integer, and owner-lifetime preflight before
timing is enabled. The selected topology is one standalone language-owned Ruby
evidence-fixture module. It builds canonical, generated, and independently
handwritten acquisition commands in separate roots and caches, plus a compare
command that imports and executes none of them. A fixture-private lifecycle
driver may build, launch, externally authenticate, wait, and clean up; it owns
no Ruby facts, measured implementation, semantic interpretation, or verdict.

Every lane/round sample uses a fresh process and owner. The lanes share only
immutable source bytes, a frozen language-owned facts/schedule manifest, and a
documented closed framing grammar; they share no guard, lookup, fallback,
target, effect, statistic, control, or receipt-encoder implementation. Each raw
receipt is length and SHA-256 framed canonical TSV. The driver separately binds
the executable digest and BuildID, source closure, toolchain, target, flags,
environment, isolated root/cache, argv, exit/timeout, stdout/stderr digests,
and descendant settlement. A lane cannot authenticate itself or see the other
lane's receipt. The comparator rejects truncation, trailing bytes, duplicate,
unknown or missing required keys, swapped roles, identity mismatch, excess
bounds, incomplete processes, and failed cleanup.

Two independently acquired, oppositely rotated capture sets must each retain
raw return/effect checksums, exact timed counters, allocations, and separate
cold construction/admission, stale-Beta, Gamma, code/build, launch-to-ready,
RSS, owner-generation-reopen, and cleanup records. The timed generated and
handwritten delta contains pre-warmed Alpha/Beta hits only. Incomplete,
invalid, contaminated, timed-out, or unsettled acquisition remains
`INCOMPLETE`; only the receipt-only comparator writes `PASS`, and writes it
last. Owner-generation reopen is not called worker reload.

Before timing exists, a disposable no-timing skeleton must prove:

1. exact source, Program/facts, schedule, emitter, generated-byte, and binary
   identities, including every required definition ID;
2. rejection before effects of zero, unknown, out-of-range, and
   class/slot/definition-incompatible occupied targets, stale/exhausted epochs,
   altered schedules, and malformed receipts;
3. disjoint dependency and symbol graphs for canonical, generated,
   handwritten, and compare commands, with no compiler, opposite lane, shared
   guest runtime, or semantic harness in acquisition binaries;
4. deterministic zero-duration role receipts and independent comparator
   rejection of single-byte corruption, truncation, duplication, swapped
   roles, stale artifacts, and trailing data; and
5. bounded output, timeout, descendant settlement, failed-candidate close,
   artifact retention, and retry-safe cleanup.

The skeleton must also prove that the real canonical lane can be acquired
without exporting a generic Ruby runtime seam or copying measured semantics
into shared evidence code. There is intentionally no command for this
unimplemented standalone R1 skeleton or its performance capture. The bounded
in-package Target37 lowering capture below is not that observer. A
repository-wide script
rates 9.7/10 for acquisition mechanics but 8.4/10 as the owner because it can
absorb language policy; the selected standalone fixture rates 9.6/10 overall,
one spawning Go test about 8.0/10, and the rejected in-process observer 1.5/10
for validity.

The first 1,914-line no-timing attempt is rejected and is not such a command.
It put build, launch, envelopes, settlement, and cleanup in one spawning Go
test, then stopped before any role binary completed after its one repair was
consumed and the canonical command still failed to compile. All-line primary
and two independent reviews also found non-equivalent schedules and synthetic
effects, cross-lane receipt/binary visibility, ambient but incompletely hashed
environments, an unauthenticated compare command, weak envelope validation,
no descendant ownership, false early cleanup attestation, incomplete
epoch/definition corruption checks, and self-reported owner closure. P+1
through P+5 therefore fail and no timing is enabled.

That concrete implementation rates 1.5/10 for evidence validity, 2.5/10 for
semantic validity, 1.0/10 for lifetime validity, and 3.0/10 for economy. The
selected 9.6/10 standalone topology remains provisional rather than proved:
the failed attempt instantiated the explicitly rejected spawning-test
alternative. A future skeleton must be a separately built lifecycle command,
authenticate the separately built comparator, give each lane a scrubbed and
identity-bound root/environment with no counterpart visibility, derive
language-owned schedule/effect receipts from observed work, settle the whole
process tree, and verify cleanup before completion. Requiring shared/public
runtime machinery, source rewriting, ambient artifact visibility, or
comparator-owned Ruby semantics rejects the topology rather than relaxing the
gate.

Observer implementation is intentionally deferred. The target-ID arms and
inventory are private, generation-local, absent from checkpoints, and
replaceable without changing `preparedsource`, the builder, the worker, or an
application schema. A genuine standalone fixture would currently require
roughly 22-26 files, 2.4-3.6 K handwritten lines, five authenticated binaries,
and a new reviewed portable process owner for arbitrary Go-tool descendants.
The EPW2 launcher is evidence for ownership invariants, not a reusable
acquisition supervisor, and a repository-wide orchestrator would need nearly
the same machinery while creating a premature evidence ABI.

There remains intentionally no standalone R1 skeleton or capture command. The
in-package Target37 microprobe below does not change this boundary. Reopen
standalone production-oriented acquisition only after a retained R1-oriented
slice has all of:

1. a named CRuby/RubySpec frontier with one real ancestor/module or singleton
   lookup mutation and explicit visibility/alias/remove/undef policy;
2. a production polymorphic site with real class/shape identity, canonical
   selector/slot lookup, effective epochs, and generated direct targets;
3. exact receiver/argument/block order plus fallback, corruption, integer,
   cancellation, limit, raise, targeted-unwind, and epoch-exhaustion proof;
4. an application projection excluding objects, functions, epochs, targets,
   and cache arms from generation transfer;
5. a real V1/V2 prepare/restore/activate/retire/close lifecycle, or an explicit
   owner-reopen-only scope; and
6. representative sites that make target-ID versus inline-body code growth a
   remaining material choice.

A separately reviewed process authority may reduce the later acquisition cost
but cannot replace that semantic milestone. Current-value ratings are A2
3.5/10 for decision value, 9.6/10 potential validity, and 4.5/10 economy; B
3.0/10, 9.3/10, and 4.0/10; deferral 9.6/10 decision value and 10.0/10 economy
while producing zero new evidence. The target-ID design remains 9.6/10 and
open-world performance evidence remains 7.2/10.

This design choice rates 9.6/10, but it is not a performance result. The
open-world performance evidence remains 7.2/10 until the complete observer
above passes. Reopen inline per-arm bodies only if the real target-ID form
misses the 1.15x equivalent-Go gate while an otherwise identical bounded
inline form passes without call-site-by-target code growth. Function values,
interfaces, global caches, shared dispatch, and executable-memory/JIT routes
are not admissible substitutes for the observer.

Until that observer exists, the retained R0 performance statement is the
single-class guard result above. Open-world PIC semantics are promising but do
not support a polymorphic-performance or revolutionary-Ruby claim.

## Implemented Ruby lookup/dispatch proof

The retained private D1 proof now covers the bounded C3 local-entry, C4
visibility, Target39 C1 module/include/prepend, and Target41 C2
allocation-site singleton categories. It is not a
public Ruby package or a broad compatibility claim. Its complete checked image
contains eight lookup nodes, five dispatch rows, eight selectors, seventeen
definitions, twenty-four ordered operations, and two attachments. The
generated closure deliberately projects only two selectors into sixteen local
entry positions, ten cells, nine targets, four initial definitions, twelve
mutations, thirteen dispatch/shape/receipt matches, and four sites with eight
physical arms.

The checker distinguishes installed definitions from snapshotted bodies, so
`alias_method(:saved_label, :label)` keeps Base-v1 behavior after the source slot
is replaced. `remove_method` exposes the next ancestor; `undef_method` installs
a terminal negative entry. Literal `send(:label)` uses any-visibility policy,
while `public_send(:label)` uses public-only policy without creating a second
resolution graph. Full caller-sensitive protected ordinary-call legality and
dynamic selectors remain out of scope.

Modules own local methods but no superclass, dispatch row, shape, realization,
or `.new`. In this slice `include` and `prepend` accept exactly one known
literal module constant in a class body. Duplicate attachments, class/module
kind mismatches, module-body attachment, malformed ordering, replay, and
capacity overflow fail closed. This is one exact two-attachment schedule, not
general module graphs, cycles, refinements, `super`, or arbitrary repeated
attachment semantics.

One sealed private image owns node, attachment, definition/body, visibility,
call-mode, mutation, and comparison facts. It owns no live owner, epoch,
object, target, arm, function, checkpoint, or result. The checker seals all
four masks for this exact two-attachment closure: sixteen row-route oracles over
42 path nodes, with per-mask totals 9/11/10/12. Mask `10` is deliberately
unreachable in the source schedule and independently tested. Canonical runtime
construction derives routes from node/attachment descriptors and activation
bits, compares every sealed oracle, and resolves only from its scratch. The
emitter validates those facts but emits no route oracle or flattened route.

The selected topology authority is immutable descriptors plus owner-local
live/staged activation bits. Method operations stage one entry and one selector
column. Include/prepend operations stage only the activation bank, derive old
and new routes into bounded nonallocating scratch, and recompute the complete
eight-cell generated matrix. Every fallible check and the final cancellation
poll precede publication; commit publishes entries, activation bits, cells, and
the required nonzero epochs without another fallible operation. Sites are
never mutation dependencies.

Target39 operations 19 through 23 have this exact behavior:

| Operation | Activation after | Changed receipts `K` | Required effect |
| --- | ---: | ---: | --- |
| Define IncludedLabel while detached | `00` | 0 | Entry publishes; cells and epoch stay byte-identical. |
| Include IncludedLabel into Alpha | `01` | 2 | Alpha and Delta change. |
| Prepend empty PrependedLabel into Beta | `11` | 0 | Topology publishes even at `MaxUint64`; no cell, epoch, or arm changes. |
| Define PrependedLabel after prepend | `11` | 1 | Only Beta changes. |
| Redefine IncludedLabel | `11` | 2 | Alpha and Delta change. |

Cancellation after complete staging, corruption, replay, insufficient capacity,
and epoch exhaustion publish nothing. Close erases entries, cells, activation
banks, scratch, arms, objects, and statistics; a fresh owner reconstructs cold
state with no route/cache identity. The conservative maximum lookup/site ledger
is 1,442,960 bytes, leaving 129,904 bytes below the 1.5 MiB gate.

Generated target IDs name definition/body implementations, not receiver
classes. Base-v2 is reused across separately validated Alpha, Beta, and Delta
matches. Included, prepended, and redefined included methods receive distinct
targets. A warmed `send` remains scalar receiver/shape/cell/arm checks plus a
site-local numeric target switch and direct named Go body call; warmed
`public_send` adds one scalar visibility guard. AST tests prove all three
entered leaves contain no attachment, topology, or route identifier. They
perform no name/map lookup, ancestor walk, allocation, lock, atomic lookup-state
operation, shared runtime call, or interface/function-value method target.

The exact current identities and layouts are:

- unchanged C4 prefix: 2,480 bytes, SHA-256
  `aad50c456aad030a12365e85596cdc30ac1a8d85437c9adcacb6dbeb2c9907a5`;
- Target39 extension: 850 bytes/51 LF, SHA-256
  `4c3388faa84323c4382ec815006e1a799f74b85738d4bd20148f8bb92429acf0`;
- full source: 3,330 bytes/209 LF, SHA-256
  `8f2611ea5042f09f8313c1291af5b44a5bbf8e76ae3733e0745579e8c838c168`;
- topology output: `2|11|2|22|13|1|713726`; unchanged C4 output plus
  topology output and final LF SHA-256
  `01c3e3bbbf334e42fe3940a0ef2c8d2e25ec0891b23ffc36a3bc9020ce066ea0`;
- Program:
  `e318f17162550f67ef6f59d6aee5d069685a82e4c4bc6b003bd00ebc62893a5a`;
- package-`generated` Set:
  `e0a6719f3022bab101ec9ae6fb252f9652161f4876a39bc4c05f62c39274bfda`;
- generated source: 68,743 bytes/2,065 LF, SHA-256
  `93a2e374c8b2be08eb3033b69c0ee9a1d17fc6c7e557a88f0ecaff4374fee2a3`,
  an increase of 17,975 bytes and 395 lines over the historical Target37
  artifact; and
- entry/receipt/cell/arm/lookup/route-workspace/site/object/control/stats/Engine
  layouts:
  `12/16/24/32/376/36/64/32/56/64/688` bytes on the supported 64-bit proof
  target.

The final statistics are exactly hits/cold/stale/repairs/fallbacks/admissions/
evictions/occupied = `6/5/5/5/5/5/0/5`.

On Go 1.26.4 Darwin/ARM64 with cgo disabled, the current generated export
archive is 683,240 bytes and package text is 40,864 bytes. The
label/saved/public wrappers remain 288/288/400 bytes. Their entered warmed
leaves are 720/672/816 bytes, 2,208 bytes in aggregate; the sole cold helper is
2,480 bytes. These are current compiler-shape receipts. The current-identity
warmed recapture below separately authenticates the bounded direct-hit timing;
Target38's scaling measurements and the older Target37 capture remain
historical pre-Target39 evidence.

Run the current focused and broad proof with:

```sh
go test -count=1 -run '^TestTarget39' ./internal/rubyproof
go test -count=1 ./internal/rubyproof/...
go test -race -count=1 -run '^TestTarget39'   ./internal/rubyproof ./internal/rubyproof/generated
go test -gcflags=all=-d=checkptr=2 -count=1 -run '^TestTarget39'   ./internal/rubyproof ./internal/rubyproof/generated
CGO_ENABLED=0 go test -count=1 ./internal/rubyproof/...
go vet ./internal/rubyproof/...
```

Freshness and pinned CRuby corroboration are explicit:

```sh
go test -count=1   -run '^TestPreparedSourceIsOneLocationNeutralSetAndFixtureIsFresh$'   ./internal/rubyproof

EMBER_RUBY_ORACLE=1 go test -count=1   -run '^TestRubyProofAgainstPinnedLocalCRuby$'   ./internal/rubyproof
```

A fresh offline application must retain neither compiler/runtime graph nor
route-oracle text, and the repository pure-Go check remains:

```sh
go test -count=1   -run '^TestRubyPreparedApplicationBuildsOfflineWithoutCompilerOrRuntimeGraph$'   ./internal/rubyproof
CGO_ENABLED=0 go test -count=1 -run '^TestPureGoBoundary$' .
```

### Target39 sealed-snapshot comparator

The explicit test-only B comparator is independent of production route
derivation and resolution. It models four sealed snapshots, each with four
compact row spans over exactly 42 `uint32` path slots. The selected-C replica
contains the seven compact node facts, two attachment facts, two activation
banks, and one 36-byte route workspace.

| Representation | Immutable topology | Mutable/scratch | Total modeled topology |
| --- | ---: | ---: | ---: |
| B sealed snapshots | 200 B | 2 B live/staged IDs | 202 B |
| C descriptors + activation bits | 80 B | 56 B including workspace | 136 B |

C is 66 bytes (32.67%) smaller in this exact modeled payload, while B has much
smaller mutable topology and fewer cold structural reads. All-mask work is B:
16 span reads/42 slots; C: 16 route builds/42 slots/42 node facts/24 attachment
facts. The five-operation cold schedule is:

| Operation | Common routes/slots/resolutions/probes/checks/writes | C node/attachment reads |
| --- | --- | --- |
| Detached define | `4/9/8/16/4/0` | `9/6` |
| Include | `8/20/64/152/32/2` | `20/12` |
| Empty prepend | `8/23/64/174/32/0` | `23/12` |
| Late prepended define | `4/12/8/16/4/1` | `12/6` |
| Included redefine | `4/12/8/14/4/2` | `12/6` |

The common totals are 28 routes, 76 route slots, 152 resolutions, 372 entry
probes, 76 cell checks, and 5 writes. C additionally reads 76 node and 42
attachment facts. These are deterministic structural counters, not elapsed
time. There is no standalone B generated package, so source/archive/text,
binary, build-cost, and warmed-time comparisons remain unresolved.

A persistent-route A rates 8.9/10. B rates 9.5/10 for the exact closed fixture
but 6.6/10 as a product trajectory. Selected C rates 9.8/10 for the bounded
semantic/lifetime proof, 9.2/10 provisional for representation selection, and
9.6/10 conditional for product trajectory. C wins the current commitment
because authority scales with nodes/attachments and warmed code stays
topology-free, not because it has proved cold-speed superiority. Reopen a
persistent route cache only as owner-local validated derived state after
representative cold evidence; never make it semantic authority or a
cross-language seam.

One focused ChatGPT Pro consultation was attempted on whether the generated
artifact should retain a compare-only route oracle, but the browser bridge was
unavailable and configuration could not be verified. No Pro answer is claimed
or used. The implemented decision follows the frozen selected-C contract and
the independent repository observers: compiler/test literals remain, while
generated Go contains no oracle or flattened route.

### Current Target39 warmed-hit recapture

The existing Target37-named capture harness was rerun unchanged against the
current Target39 generated artifact after freshness passed. Two separate
cgo-off, single-P Go processes each ran seven oppositely rotated rounds for
direct generic, specialized, and independently implemented equivalent-Go
`send` and `public_send` lanes. The gate accepted all 84 measured rows with
zero bytes and zero allocations.

The identity marker binds Apple M1, Go 1.26.4, Darwin/ARM64, `GOMAXPROCS=1`,
cgo off, test binary SHA-256
`e4e819b3dee2ebf164f66e6012f1a86cb5d7bffb16f62e97435fe030b6b96c33`,
and generated/benchmark/equivalent/gate source SHA-256 values
`93a2e374c8b2be08eb3033b69c0ee9a1d17fc6c7e557a88f0ecaff4374fee2a3`,
`e82b48a6393c81c43b8ede3dc0f2abb97bbf979c7a96f3060af9500409e9e142`,
`c6cee86607364b0e8243c41f29f2fa9924d6a1ba56f30db27856fae9491fc849`,
and `2cc826756aa746b5fb12461fb3f59b7dac00e926e7e9eaa8fd5ab88c43c4f326`.

| Capture | Lane | Specialized/generic median | Specialized/equivalent median |
| --- | --- | ---: | ---: |
| A | `send` | 0.607892 | 1.072247 |
| A | `public_send` | 0.618291 | 1.039982 |
| B | `send` | 0.599669 | 1.058290 |
| B | `public_send` | 0.592217 | 1.059357 |

Raw A/B SHA-256 values are
`b203e1c388072d7667800f5b2515f29b6069c7db3026bf10cc4313ad108287d4`
and `8ac3b73a15b03e1f163305493b728994028b1d0f4dd7d6ede173244dca3e4139`;
the gate-output SHA-256 is
`7a4dc6218c1f8f16425e452f821c2da624e4a0838c124e1ede2ca12cee10f1b0`.
Thus this current artifact's specialized warmed shape is 1.62-1.69x the
current generic helper shape and no worse than 1.073x the equivalent lane in
this bounded workload.

This recapture validates only pre-repaired alternating Alpha/Beta hits. It
does not time Target39 route derivation, topology transactions, cold admission,
repair/fallback, construction, owner lifecycle, projection, build/RSS, IPC,
reload, canonical Ruby, or CRuby/YJIT. The command/load environment is still
documentary rather than a quiet-host or release observer, so open-world Ruby
performance remains 7.2/10.

### Historical Target37 warmed-hit capture

The receipts in this subsection bind the pre-Target39 generated SHA
`fc490de9b0f098ca48b74c283e6e8e5d7b74104928704d8596ce497972767bbd`.
They remain attributable lowering history but do not authenticate the current
Target39 artifact. The command shape remains the reproducibility protocol used
by the current recapture above: first prove freshness, then acquire A and B as
separate Go processes; do not reuse output paths or continue after a failed
command.

```sh
go test -count=1 \
  -run '^TestPreparedSourceIsOneLocationNeutralSetAndFixtureIsFresh$' \
  ./internal/rubyproof

receipt_dir="$(mktemp -d /tmp/ember-ruby-target37.XXXXXX)"

CGO_ENABLED=0 GOMAXPROCS=1 EMBER_RUBY_TARGET37_CAPTURE=A \
  go test -trimpath -pgo=off -buildvcs=false -count=1 \
  -run '^$' -bench '^BenchmarkTarget37CaptureA$' \
  -benchtime=1s -benchmem -v \
  ./internal/rubyproof/generated >"$receipt_dir/capture-a.txt" 2>&1

CGO_ENABLED=0 GOMAXPROCS=1 EMBER_RUBY_TARGET37_CAPTURE=B \
  go test -trimpath -pgo=off -buildvcs=false -count=1 \
  -run '^$' -bench '^BenchmarkTarget37CaptureB$' \
  -benchtime=1s -benchmem -v \
  ./internal/rubyproof/generated >"$receipt_dir/capture-b.txt" 2>&1

CGO_ENABLED=0 GOMAXPROCS=1 \
  EMBER_RUBY_TARGET37_CAPTURE_A_PATH="$receipt_dir/capture-a.txt" \
  EMBER_RUBY_TARGET37_CAPTURE_B_PATH="$receipt_dir/capture-b.txt" \
  go test -trimpath -pgo=off -buildvcs=false -count=1 -v \
  -run '^TestOptInTarget37CaptureGate$' \
  ./internal/rubyproof/generated
```

One timed operation resets two traces and executes eight pre-repaired
alternating Alpha/Beta hits. Each capture contains seven oppositely rotated
rounds for direct generic, specialized, and independently implemented
equivalent-Go `send` and `public_send` lanes. The parser requires the exact
captured binary and four source identities, CPU/target/toolchain agreement,
closed row and container inventories, the frozen order, 42 unique rows,
finite positive 0.75-3.0-second measured work per row, and zero bytes and
allocations. Each capture and mode requires median specialized/generic at most
0.95 and specialized/equivalent at most 1.15.

The accepted local Apple M1, Go 1.26.4, Darwin/ARM64, cgo-off captures bind
binary SHA-256
`a401f19017b4c6d3cd89beb8988e796eff199d68552a9a04ba3a836b880cdce2`
and generated/benchmark/equivalent/gate source SHA-256 values
`fc490de9b0f098ca48b74c283e6e8e5d7b74104928704d8596ce497972767bbd`,
`bc0ea0a79e6cefad0facf4627515af379e7379afd337f80e3f6c01b623d8888a`,
`c6cee86607364b0e8243c41f29f2fa9924d6a1ba56f30db27856fae9491fc849`,
and `2cc826756aa746b5fb12461fb3f59b7dac00e926e7e9eaa8fd5ab88c43c4f326`.
Raw A SHA-256
`2cffd5773bce1b21a93e4cdd4ebe1999dbd12daf84dad1cc91d22cc4b96f29f6`
measured send 0.602020/0.998031 and public 0.581892/0.973797
specialized/generic and specialized/equivalent medians. Raw B SHA-256
`bf9e59ddf1242e31ad5678263bcb9e9654b980338e6a35adbd17a6d47646bf68`
measured 0.593560/0.976646 and 0.584314/0.973367. Thus the specialized
shape was 1.66-1.72x the then-current generic helper shape and within 1.00x of the
equivalent lane in this bounded workload; all 84 rows allocated zero. The
gate output SHA-256 is
`db22091e93b9139acc9874c5dbe7eb8e1e0f9d3691ff4a29754303d77b283a06`.
An earlier B file, SHA-256
`32e8b53a3671866c2b15665718ec6e35eb5f63fc49f397ca42c925f32281af10`,
was rejected because one row represented 5.22 seconds of measured work; it is
not evidence.

This is bounded local, identity-bound lowering evidence, not a quiet-host or
release receipt. Command flags, full runtime environment, load/thermal state,
and A/B independence remain documentary rather than authenticated. The
generic lane reconstructed the pre-specialization algorithm against its bound
facts; it was not the exact old binary. Target37 timed neither canonical Ruby,
CRuby/YJIT, cold admission, repair/fallback, construction, public owner
admission, poisoning/close, projection, build/RSS, IPC, nor reload. It therefore
does not satisfy the deferred standalone R1 observer or raise the 7.2/10
open-world Ruby evidence rating.

Current focused same-package tests must retain all of these properties:

1. canonical and generated owners prove alias body identity, Base-redefinition
   selectivity, protected/private pre-effect rejection, private `send`, public
   restoration, Beta-remove ancestor exposure, Gamma-undef termination,
   include/prepend and later module definitions, and byte-stable unrelated
   cells;
2. generated tests prove unchanged site arms during mutation, Base-v2 target
   reuse, no target for `undef`, normal-success-only repair, no rejection
   publication, zero warmed allocations, and exact statistics;
3. all four attachment masks match independent literal routes; detached define,
   include, empty prepend, late define, and redefine publish exact
   `K=0/2/0/1/2`, and generated warmed leaves contain no topology dependency;
4. malformed image/entry/receipt/visibility/attachment/mutation/arm/target
   facts fail closed before guest effects; cancellation and epoch exhaustion
   publish no partial entry, activation, cell, arm, statistic, or epoch state;
5. close clears owner-local cells, entries, activation banks, scratch, arms,
   objects, and statistics, and a fresh owner starts cold with no checkpointed
   cache identity; and
6. fixture freshness, the pinned CRuby result, static dependency absence,
   pure-Go policy, race, checkptr, vet, and six-target cgo-free cross-build pass
   through the commands in this section.

The production-frontier caps remain 256 dispatch classes, 512 lookup nodes, 64
attachments, 64 prepared selectors, depth 32, 8,192 cells, 32,768 local
entries, 512 compare-only route oracles, 8,192 sealed oracle path slots, 4,096
two-arm sites, 262,144 mutation probes, and a 1.5 MiB lookup/site ledger. The
bounded fixture does not prove representative-program
admission, production constructor cost, representative generated-code/build
scaling, or a production heap. Target38 below measures only exact repeated C4
leaf multiplicity. Reopen the dense matrix only if representative programs fail
those product caps or a bounded topology mutation misses a later latency gate;
any sparse alternative must keep numeric reverse edges Ruby-private and retain
no site, object, or code pointer.

Three independent `gpt-5.6-sol` high reviews first selected C4 ahead of C1,
C2 singleton mutation, and an unavailable clean L0 lifecycle receipt because
C4 reused the existing entry transaction. Target39 later reused the same three
agents to freeze source/lowering, semantic/lifetime, and observer contracts for
C1. Primary selected attachment descriptors plus activation bits by decision
value, not vote: persistent paths make derived answers authoritative, while
sealed snapshots are excellent for this finite closure but scale with
enumerated topology states. C2 still needs per-object ownership.

Target41 implements Target40's smallest C2 ownership contract. One
checker-proved, straight-line, exactly-once Alpha allocation gets
a statically preassigned singleton node/dispatch row while retaining Alpha's
ordinary class and layout shape. The literal zero-capture
`define_singleton_method(:label)` reuses the public define transaction and must
change only that row's label cell (`K=1`). The same-class peer remains on the
ordinary Alpha row. A pinned local Ruby 2.6.10 observation produced the frozen
additional line `13|13|13|44|44|13|334433`; the 571-byte source delta is SHA-256
`1cdf5cfadcf34d44e8b8eadd82213eab32589b9ad64db813f17e3aba85c07256`,
the 3,901-byte full source is
`bf355c37216015a49a5b2079e3ff842393755a01fe7cf85d070d1f4fbbf66a52`,
and the three-line output plus final LF is 123 bytes/SHA-256
`c09d4fceec6a72f86c690145113737013b4e2ad4a408341bae7506d177ec85ab`.

The current focused proof independently establishes all of the following:

1. pinned CRuby 2.6.10, canonical, and generated lanes agree on all three
   output lines without sharing an expected-value implementation;
2. the checker rejects repeated/escaping allocation sites, dynamic selectors
   or receivers, captures, parameters, `Proc`/`Method`, return, `super`, and
   reflection, and no second object can acquire the singleton dispatch;
3. node 8/row 5 has route `[singleton] + route(Alpha, mask)`, Alpha shape is
   unchanged, all four masks have exact route lengths `3/4/3/4`, and the
   compare-only inventory is twenty routes/fifty-six path slots;
4. the legacy four-row C1 `K` vector remains `0/2/0/1/2`, the full eager
   five-row vector is `0/3/0/1/3`, and singleton define is exactly `K=1` with
   topology still `11` and one epoch advance;
5. entry/cell/epoch snapshots show the peer row and peer arm byte-identical;
   cancellation, cap, exhaustion, corruption, replay, and receiver mismatch
   publish no partial state;
6. the dedicated two-arm site records the expected delta of three hits, two
   cold admissions, one stale miss, one repair, zero eviction, and zero
   uncached fallback, with repair only after normal completion; and
7. warmed generated leaves contain no object ID, map, string, topology,
   attachment, route, allocation, lock, atomic, interface call, or function
   target, while close/reopen erases all generation-local singleton identity.

The implemented generated artifact is 79,361 bytes/2,344 LF, SHA-256
`b30db631a57127bd5f420427262cfff3970c14f6cf5c91cd382fd3b08d61bf46`.
Observed 64-bit layouts are 40 bytes for route workspace, 448 bytes for lookup,
32 bytes for object, and 824 bytes for Engine. Final dedicated-site-inclusive
statistics are `9/7/6/6/5/7/0/7` in
hits/cold/stale/repair/fallback/admission/eviction/occupied order. These exact
source, layout, and statistics receipts are structural evidence; Target41 ran
no elapsed-time gate and makes no representative artifact/build-size claim.
General singleton allocation/reclamation, captured environments, eigenclass
reflection, clone/dup, and checkpoint/reload remain H1 or later work.

### Target42 bounded singleton warmed-leaf observer

Target42 measures only the already-repaired `ReceiverAfterDefinition` leaf
from Target41. It does not time construction, admission, definition, repair,
the peer arm, close, generation lifetime, or worker reload. The independent Go
comparator owns separate scalar types and control/effect code but performs the
same five polls, two steps, three frame entries, complete two-arm validation,
direct receiver target, trace/control updates, and post-body hit publication.
Exact tests cover receiver and peer success, cancellation polls 1-6, every
frame/step limit, wrong dispatch/shape, selected-cell/arm corruption, unused
peer-arm corruption, stale recovery, unchanged pre-effect state, and exact
trace/control/statistics.

Run the deterministic contract and parser checks with:

```sh
GOFLAGS=-pgo=off CGO_ENABLED=0 GOMAXPROCS=1 \
  go test -count=1 -run '^TestTarget42' \
  ./internal/rubyproof/generated
```

The retained schema-3 capture protocol requires two distinct processes,
`CGO_ENABLED=0`, `GOMAXPROCS=1`, exact `GOFLAGS=-pgo=off`, no local
`default.pgo`, and exact `-benchtime=500ms`. Each process runs twenty paired
Current/Equivalent rounds and inverts lane order; capture B starts with the
opposite order from capture A. Generic remains a standalone semantic and
diagnostic benchmark and is not part of the economy gate. For a caller-owned
temporary output directory:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 GOFLAGS=-pgo=off \
EMBER_RUBY_TARGET42_CAPTURE=A \
go test -v -run '^$' -bench '^BenchmarkTarget42CaptureA$' \
  -benchtime=500ms -count=1 ./internal/rubyproof/generated > "$dir/capture-a.txt"

# Cool the host before the independent second process.
sleep 30

CGO_ENABLED=0 GOMAXPROCS=1 GOFLAGS=-pgo=off \
EMBER_RUBY_TARGET42_CAPTURE=B \
go test -v -run '^$' -bench '^BenchmarkTarget42CaptureB$' \
  -benchtime=500ms -count=1 ./internal/rubyproof/generated > "$dir/capture-b.txt"

CGO_ENABLED=0 GOMAXPROCS=1 GOFLAGS=-pgo=off \
EMBER_RUBY_TARGET42_CAPTURE_A_PATH="$dir/capture-a.txt" \
EMBER_RUBY_TARGET42_CAPTURE_B_PATH="$dir/capture-b.txt" \
go test -count=1 -benchtime=500ms \
  -run '^TestOptInTarget42CaptureGate$' -v \
  ./internal/rubyproof/generated
```

The gate fails closed on source/build/environment identity, row order,
missing/duplicate/non-finite rows, timing outside 375 ms to 2 s, or any
allocation. Its economy criterion is the fifteenth ordered paired
Current/Equivalent ratio from twenty rounds, a conservative one-sided greater
than 95 percent nonparametric upper bound for the population median, at most
1.15 in **both** captures. It also reports the paired median and median
nanoseconds per call.

The first attempted 20-round, three-lane, one-second protocol was rejected
before ratio analysis: although all 120 rows allocated zero, one capture-A row
took 3.317913608 seconds and later rows showed severe environmental/thermal
outliers. Its raw SHA-256 values were `ff13d593...`, `127d32a4...`, and gate
`2519124e...`. That protocol must not be selectively repeated.

The bounded replacement protocol passed on Apple M1 with zero allocation in
every retained row:

| Capture | Current/Equivalent median | 15th-of-20 upper bound | Current median ns/call | Equivalent median ns/call | Raw SHA-256 |
| --- | ---: | ---: | ---: | ---: | --- |
| A | 1.002069 | 1.045358 | 24.769 | 24.188 | `fe6897ace7058715a879e6d33e507908be4406ca53dff4f86704d28ce2cc698d` |
| B | 1.000265 | 1.005731 | 21.956 | 21.994 | `341e4565a73c15169ba66308f03ee3704fb9effcdb7058971a861a79c0a32236` |

The successful gate output was 3,828 bytes/56 LF/SHA-256
`ade6e962287fd90c8e877241a7a9019523011af06d0d6592e16016b351ae42c5`.
The exact observer source identities are comparator
`39f5f29f39b488e153e50e8f97ff7493a0eef14cf4f0957d8718eb63e94e14fa`,
benchmark
`8d2a3af71a9ae2f027915201bf1e57e9954532d4a1f0be5ad3c38ca6d96c3f71`,
gate
`b190aca2a1a7155de8fa158a52477ba0925fc00825f3e6c4007bba2c5a51e947`,
and generated artifact
`b30db631a57127bd5f420427262cfff3970c14f6cf5c91cd382fd3b08d61bf46`.

**Decision:** keep the current direct leaf unchanged. It rates 9.7/10 for the
bounded measured shape. A singleton receipt-predicate variant rates 9.3/10, a
fused exact cell/site/arm guard 9.0/10, split leaves 8.2/10, a generic
production scan 7.1/10, and selected-arm-only validation 4.3/10 because it
violates the current unused-arm fail-before-effect contract. Reopen only for a
source-identity change, a broader claim, or an exact semantics-preserving
candidate with a reproducible material gain and no per-arm, allocation,
failure-order, lifetime, or code-size regression. These captures do not
support peer-throughput, open-world Ruby, CRuby/YJIT, general eigenclass,
worker, reload, or revolutionary-performance claims.

The language-owned architecture remains 9.8/10 and the direct-hit **design**
9.7/10. C4 itself is 9.8/10 as a semantic/ownership split. Target37's bounded
source/compiler shape rates exactness 9.9/10, hot shape 8.8/10, depth 8.2/10,
locality 9.3/10, and production economy 9.0/10. The current Target39 recapture
confirms that warmed lowering shape but does not measure topology work.
Target39 C rates 9.8/10 semantic/lifetime, 9.2/10 provisional representation
evidence, and 9.6/10 conditional product trajectory. Open-world Ruby
performance evidence remains 7.2/10 because no standalone R1 or CRuby/YJIT
comparison has run. Receiver-specific singleton implementation, full protected
ordinary-call legality, production heap/GC, application-specific broader
projection, process-generation reload, representative-program generated-code
scaling, and clean public lifecycle evidence remain separate gates. Target38
below remains a historical repeated exact-C4 leaf observer. Do not call the Ruby
slice revolutionary or generalize either microprobe beyond its population.

### Historical Target38 strict-leaf scaling observer

Target38 is a historical disposable compiler/build observer, not a production
emitter change or an open-world Ruby benchmark. It repeats the exact pre-
Target39 three-site C4 generated package at total site counts 3, 24, 96, and
conditionally 384. Each
population retains the same equal mix of label/send, saved/send, and
label/public-send sites. It compares:

- **A — strict:** the current one wrapper, one owner-local `rubySite`, and one
  entered leaf per site;
- **B — shared leaf:** the same distinct wrapper and `rubySite` per site, but
  one entered leaf for each exact normalized generated leaf template; and
- **C — array:** B plus literal-indexed site storage instead of named fields.

B is deliberately narrower than a selector/mode family. A generated leaf is
eligible to share only when its normalized body is alpha-equivalent after
substituting **only** the per-site state pointer and source-site attribution.
All selector, visibility/call mode, evaluation and error order, arm width and
empty-arm rules, complete guard and receipt validation, miss/repair/statistic
policy, direct target cases, named body calls, and cold handoff remain literal
parts of the compared body. A canonical hash may index candidates, but exact
structural equality must confirm a match. The wrappers, arms, warming,
statistics, corruption state, close/reset lifetime, and reload generation stay
independent. There is no runtime key, plan table, function target, generic
dispatcher, or second cold authority.

The observer generated every source twice in independent temporary roots,
formatted it, compiled it cgo-off with
`GOFLAGS='-trimpath -pgo=off -buildvcs=false'`, and required byte-identical
sources, export archives, executable identities, and build IDs. A source
skeleton verifier found exactly six normalized generated shapes for every
strategy/count. The symbol verifier found exactly N wrappers and N defer
wrappers for all strategies, N entered leaves for A, and three entered leaves
for B/C. Named target relocations remained direct. The N=3 B prototype passed
the complete existing generated semantic/control/corruption/lifetime suite and
its zero-allocation warmed-hit test. Compiler diagnostics reported no `site`
escape and no `runtime.deferprocStack` in the wrapper, leaf, or cold path.

Exact deterministic results are:

| Sites | Shape | Source bytes | Lines | Go tokens | Export archive | Total text | Site text | Binary |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 3 | A strict | 50,768 | 1,670 | 11,952 | 577,614 | 35,056 | 3,536 | 2,143,026 |
| 3 | B shared | 50,755 | 1,670 | 11,945 | 580,636 | 35,136 | 3,616 | 2,143,026 |
| 3 | C array | 50,800 | 1,670 | 11,981 | 582,818 | 35,200 | 3,616 | 2,143,074 |
| 24 | A strict | 86,832 | 2,846 | 20,331 | 867,308 | 61,424 | 28,288 | 2,214,594 |
| 24 | B shared | 59,484 | 2,013 | 14,332 | 712,292 | 44,592 | 11,456 | 2,163,298 |
| 24 | C array | 57,394 | 1,950 | 14,032 | 708,140 | 43,440 | 11,456 | 2,163,058 |
| 96 | A strict | 210,480 | 6,878 | 49,059 | 1,861,050 | 153,040 | 113,344 | 2,431,698 |
| 96 | B shared | 89,412 | 3,189 | 22,516 | 1,163,930 | 78,032 | 38,336 | 2,258,674 |
| 96 | C array | 80,077 | 2,910 | 21,064 | 1,136,142 | 71,264 | 38,336 | 2,241,058 |
| 384 | A strict | 705,072 | 23,006 | 163,971 | 5,867,818 | 528,752 | 461,392 | 3,333,042 |
| 384 | B shared | 209,124 | 7,893 | 55,252 | 2,972,174 | 213,216 | 145,856 | 2,640,178 |
| 384 | C array | 170,890 | 6,750 | 49,192 | 2,847,784 | 182,272 | 145,856 | 2,602,610 |

At the frozen N=96 decision point, B reduces source by 57.52%, export archive
by 37.46% (697,120 bytes), total text by 49.01%, site text by 66.18%, and the
linked binary by 7.12% relative to A. Its N=3 no-reuse cost is 0.02% less
source, 0.23% more total text, 0.52% more archive, and an equal-size binary.
Strict marginal site text stays approximately linear: 1,178.67 bytes/site
from 3 to 24, 1,181.33 from 24 to 96, and 1,208.50 from 96 to 384. Thus the
observer proves a material repeated-exact-template code-size problem, not a
superlinear compiler failure.

C does not justify its extra layout/planner state. At N=96 it improves over B
by 10.44% source, 2.39% archive, 8.67% total text, 0% site text, and 0.78%
binary. Even at N=384 it saves only 4.19% archive and 1.42% binary. Keep named
site fields unless a later representative program proves a material retained
artifact or construction benefit.

The source SHA-256 receipts, ordered A/B/C, are:

- N=3:
  `fc490de9b0f098ca48b74c283e6e8e5d7b74104928704d8596ce497972767bbd`,
  `78fa23a0ea091cb62ac295df0a14d34195ce89a776099a682c48fcdcd1ab5cf2`,
  `2b5e2b929d1b37f83b171a78b7981ff6f6a0c46ca725a20fa06f978b85e6ab44`;
- N=24:
  `c2a28dbd0b35fae020766f8cb8837f57b422255c1b0f5ccb699e790beaf57a5e`,
  `39a95c02c38465dcc66a6669701a44678c1df3d1113eeb2db9846f50ca194afa`,
  `12cb14a2255be2521af234570d4cecbc1e5958e1bc78fb6fc10836e5bcf67a01`;
- N=96:
  `189d42eb63cc9042620b1a46dce05e61dd5faab96912836e6aa7d1208d283618`,
  `f4dc77841c0c6270f8eb658b05aef9481e6c51dc24ad9fbaf4b80a119243d2c9`,
  `66b8b970324177f431fc19f59ad370c4033bb29b9f8a2d207f903d1337c6ecd1`;
  and
- N=384:
  `dfaad952326cbea4dc4e7efb9e44d4ba35032bf902ded1e64d4aa4722f12a2a2`,
  `5eacfcc8392548de60839e64c7dd5dda7c69cce3d138c71cb897d335b1d1c057`,
  `03aa7d824462f1627854e4ea9c185078f78613404a0d65fea9de9f58744e342c`.

The disposable generator, measurement driver, token counter, metrics JSON,
measurement stdout, and B escape-analysis output have SHA-256 values
`2992618f873ecd732770864dbcc24108a220dff73ad309d98cd749598e046a74`,
`7274ff22bae9f28a31eda83b4f60137e74e71498d5118b303927a4c5b96c5add`,
`f3f6dcbeaebbe1cfa742b10c87c03cb721a72606de2fe6ff2d14776e6d25c135`,
`25ed40e11e10f9ed972455703e473082aad5394574308881df1fa705ef34d451`,
`ccc3faa53864c22f28846977e626e9c9bd2c8492b7768bd98a6179d4a2b6bf97`,
and `9f57f609d1134fb043349bac1232a6c00514548c43581a59edecaed59e0e52b2`.
The temporary sources and build caches are not repository artifacts.

Fresh-build wall time and `/usr/bin/time -l` peak RSS were diagnostic only.
The host load was 6.31/4.56/4.78 with WebKit, Carbon, `coreduetd`,
WindowServer, ChatGPT, and Codex consuming material CPU, so no warmed-hit timing
was admitted. On this noisy host the N=96 fresh diagnostics were 0.51/0.29/0.28
seconds and 68.8/61.1/approximately 61 MiB for A/B/C; immediate warm builds
compiled zero packages. These values do not satisfy the required at-most 2%
warmed-hit regression gate for passing a site pointer into B.

Three `gpt-5.6-sol` high read-only reviews independently covered source/lowering,
semantic/lifetime, and observer fairness. A verified ChatGPT Pro consultation
was used only as an advisory challenge to the grouping rule; its useful delta
was to require normalized generated-body equality rather than trust a
hand-maintained selector/mode key. Model judgments are not measurement proof.

**Decision:** retain A in production because the three current sites have no
alpha-equivalent pair and B's only unresolved cost is on the hottest boundary.
Record B as the selected conditional compiler mechanism for future exact
duplicates, but do not implement or promote it until an identity-bound quiet
capture proves unchanged direct named targets, zero allocations/escapes, no
more than 2% warmed-hit regression, and the existing specialized/equivalent
ratio at most 1.15. Singleton template families stay strict. Reject C for now.
This decision changes neither the Ruby resolution authority nor any
cross-language seam, and it says nothing about CRuby/YJIT or representative
Ruby programs. Current A rates 9.9/10 semantic exactness, 8.8/10 bounded hot
shape, 9.6/10 locality, 9.0/10 current economy, and 4.2/10 repeated-template
scaling. Conditional B rates 9.6/10 semantic design, 9.2/10 scaling, 8.5/10
overall, and 7.8/10 promotion evidence until the hot gate. C rates 5.5/10.
The overall language-owned architecture remains 9.8/10.

## Bounded Ruby graph-projection architecture proof

`internal/rubyprojectionproof` is ADR 0012's private RP0 slice. It proves one
application-owned graph checkpoint across two concrete generated Ruby code
generations. It is not a public Ruby package, general heap serializer,
process-worker reload, S5b/S7/R1, or retained performance result. Run its
record, mapping, restore, generated-core, static-application, and boundary
checks with:

```sh
go test -count=1 ./internal/rubyprojectionproof ./internal/rubyproof/...
CGO_ENABLED=0 go test -race -count=1 \
  ./internal/rubyprojectionproof ./internal/rubyproof/...
go test -gcflags=all=-d=checkptr=2 -count=1 \
  ./internal/rubyprojectionproof ./internal/rubyproof/...
go vet ./internal/rubyprojectionproof ./internal/rubyproof/...
go test -count=1 -run '^TestPureGoBoundary$' .
```

The application record is exactly 52 little-endian bytes with bounded header
counts and fixed arrays. Tests pin its golden bytes and reject wrong length or
version, excessive or non-exact counts, noncanonical IDs/order, unknown tags,
dangling or redirected edges, broken alias/equal-distinct/cycle topology,
live targeted-return behavior, scalar overflow, and nonzero unused slots.
Failures satisfy both the application checkpoint sentinel and their specific
category. The record contains only closed application counts, IDs, tags, and
scalar data; structural tests exclude pointers, maps, slices, interfaces,
functions, and Ruby object/method/frame/epoch/return-target fields. Generated
Ruby packages do not duplicate that record: their projection interface contains
only `Root`, `Other`, and `Tail` scalar fields. They contain no initial scalar
values or default projection constructor; application `RestoreV1` maps
`InitialCheckpoint` into the concrete v1 type.

The migration test snapshots v1, closes it, proves post-close rejection, and
restores a fresh v2 owner. The application record round-trips identical alias,
equal-but-distinct, cycle, and scalar meaning, while same-package generated
tests inspect each generation's private pointer topology. Calls return 41 under
v1 and 49 under v2. The record therefore reconstructs captured self but not
code: the selected generation's checked facts win. Application validation owns
its nonnegative/maximum scalar policy; direct v1 and v2 constructors reject the
generation-owned equal-value invariant and source-derived addition overflow
before publication. Nil/pre-canceled context, every one of the nine generated
context polls, all insufficient step/object budgets, every private topology
predicate class, blocked Call/Snapshot versus Close, plus injection after every
application restore stage publish no owner or malformed record. Owners
constructed before an injected failure are proved closed, and a fresh restore
succeeds immediately afterward.

The two generated fixtures have explicit freshness owners:

```sh
EMBER_UPDATE_RUBY_FIXTURE=1 go test -count=1 \
  -run '^TestPreparedSourceIsOneLocationNeutralSetAndFixtureIsFresh$' \
  ./internal/rubyproof

EMBER_UPDATE_RUBY_PROJECTION_FIXTURE=1 go test -count=1 \
  -run '^TestRubyProjectionV2FixtureIsFresh$' \
  ./internal/rubyproof
```

Never hand-edit either generated Ruby file. The normal v1 fixture remains
owned by `EMBER_UPDATE_RUBY_FIXTURE=1`; the RP0 command owns only
`internal/rubyprojectionproof/generated/rubyv2/ruby_generated.go`.

Run the six-target static application and warmed-call performance observers
separately:

```sh
CGO_ENABLED=0 EMBER_RUBY_PROJECTION_CROSS=1 \
  go test -count=1 -v \
  -run '^TestRubyProjectionApplicationBuildsTwoGenerationsWithoutCompilerOrSharedRuntime$' \
  ./internal/rubyproof

CGO_ENABLED=0 GOMAXPROCS=1 EMBER_RUBY_PROJECTION_PERF=1 \
  go test -count=1 -v \
  -run '^TestOptInRubyProjectionPerformanceGate$' \
  ./internal/rubyproof
```

The static observer compiles two real checked sources into independent Sets,
mounts both in one Layout, and writes exactly five files: `go.mod`, the two
generated packages, the application's projection, and main. It builds offline
with separate source/cache/output roots, executes
`true|true|true|40|40|41|49|true`, and checks exact module/dependency/source
kinds plus compiler/delivery/shared-runtime symbol absence. All six supported
targets cross-build with cgo disabled; only the current host executes.

The performance observer constructs the generated owner through the actual
application `RestoreV2` seam, then times only its warmed captured-self `Call`
against an equivalent explicit `+9` pointer graph with the same atomic
admission, cancellation order, behavior dispatch, and close contract. Restore
and both setups remain outside the timed region. Both calls must allocate zero.
Seven alternating orders gate the median restored-v2/explicit ratio at 1.05x;
the latest local run measured 1.005x. Compilation, checkpoint codec, mapping,
restore, close, Go build, journal, IPC, and reload are excluded and must not be
inferred from this number.

## Mixed Ruby/Sprig checkpoint architecture proof

`internal/mixedcheckpointproof` is ADR 0012's private C0 slice. It is an
application composition proof, not a public language package, a production
Ruby heap serializer, S5b/S7/R1, or a process-worker reload test. Run its
semantic, restore, codec, lifecycle, static-application, and boundary checks:

```sh
go test -count=1 ./internal/mixedcheckpointproof
CGO_ENABLED=0 go test -race -count=1 ./internal/mixedcheckpointproof
go test -gcflags=all=-d=checkptr=2 -count=1 \
  ./internal/mixedcheckpointproof
go vet ./internal/mixedcheckpointproof
go test -count=1 -run '^TestPureGoBoundary$' .
```

The direct handler tests restore Ruby's detached scalar state and Sprig's
value total into a fresh Ruby owner, then produce `105|532|5` and
`206|532532|9` across close/reopen. They separately check typed Sprig
rejection/domain outcomes, distinct language budgets, cancellation and limit
poisoning, no failed result/checkpoint/effect, validation before restore,
cleanup after construction failure, retryable close, and exact fixed-record
codecs. Sprig is pure in this slice; the only resource owner being closed is
Ruby.

The production `OpenEmbedded` test commits one decision/checkpoint/effect,
replays a duplicate without duplicate delivery, closes, reopens the same
journal, restores the next transaction, and resolves it as committed. A
one-step Ruby failure begins mutating its private trace but publishes no
decision or effect; resolution is authoritatively not-committed, later entry
is lost, and runner close retires the owner. `OpenEmbedded` intentionally
retains `preparedworker`; this is durable embedded behavior, not EPW2 launch or
hot reload.

The separate static observer compiles the real Ruby D1 and Sprig S6 sources,
mounts their independent Sets in one application Layout, and writes exactly
five fresh-module files: `go.mod`, two generated packages, and the
application's handler and main. It builds offline with clean caches and
`CGO_ENABLED=0`, executes the two states above, and checks exact module and
non-standard dependency graphs, build information, and compiler/delivery/cgo
symbol absence. That observer, unlike the embedded test, retains neither
`preparedsource`, `preparedworker`, compiler, nor Ember runtime code.

Run the inner-composition performance gate and all supported cross-builds
separately:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 EMBER_MIXED_CHECKPOINT_PERF=1 \
  go test -count=1 -v \
  -run '^TestOptInMixedCheckpointPerformanceGate$' \
  ./internal/mixedcheckpointproof

EMBER_MIXED_CHECKPOINT_CROSS=1 go test -count=1 -v \
  -run '^TestStaticApplicationComposesRubyAndSprigAcrossCheckpoint$' \
  ./internal/mixedcheckpointproof
```

Both the handler and observably equivalent direct composition must allocate
zero on warmed observe and advance transactions. Seven alternating H/D and
D/H samples gate the median handler/direct ratio at 1.05x; the latest local
run measured 1.014x. This deliberately excludes `preparedworker` codecs,
journal, and effect publication, whose costs belong to the transaction layer
and must be reported separately. The cross extension builds Darwin, Linux,
and Windows on ARM64 and x86-64 and verifies each binary's no-cgo build
information; only the current host executes it.

## No-CGO architecture proof

`internal/architectureproof` is a reusable architecture ceiling, not a
production backend. It contains parameterized manual semantic lowerings,
generic-representation sensitivity variants, and a direct static ARM64
comparison.

Run deterministic and live semantic checks:

```sh
CGO_ENABLED=0 go test ./internal/architectureproof
CGO_ENABLED=0 GOMAXPROCS=1 \
  EMBER_ARCHITECTURE_PROOF_LIVE=1 \
  LUAU_BIN=/opt/homebrew/bin/luau \
  go test -run '^TestProofCasesMatchLuau$' ./internal/architectureproof
```

Acquire an exact-revision clean capture from a clean worktree:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-architecture-proof \
  --backend go-aot-ceiling --capture-pair a \
  --output /tmp/ember-architecture-ceiling-a
```

Use `go-aot-sensitivity` for the ordinary Go
string/map/closure/variadic variants and acquire independent A/B roles for
decision evidence. The runner records `guest_batch_v1`, runtime N/seed,
checksums, allocations, binary/source hashes, exact Git revision, and whether
busy-runner admission was waived. `--allow-busy` is exploratory only and is
never acceptance evidence.

## Scheduled evidence

`.github/workflows/scheduled.yml` runs the long-lived checks weekly. Manual
dispatch defaults to the narrower `s0` scope, which runs only validation,
physical ARM64 admission, and Linux/Darwin lifecycle so certification does not
start unrelated fuzz, full-VM parity, or profiling work. The explicit `full`
scope runs the complete scheduled suite. All long jobs use GitHub-hosted
runners; developer machines are not evidence workers. The five fuzz targets
each have a separate matrix entry,
`fail-fast: false`, and a bounded 10-minute fuzz budget. Every entry uploads its
log and `testdata/fuzz/<target>` corpus, including when the fuzz process fails.

The runtime parity job runs caller-named `full` and `speed2x` all-37 captures on
a GitHub-hosted physical Darwin ARM64 runner with the pinned official Luau
executable. It uploads exact schema-v2 raw and fitted-slope artifacts plus the command,
source, toolchain, Luau, CPU, OS, and environment fingerprint. Invalid or
contaminated acquisition fails; a baseline speed miss is retained honestly.

The performance job runs `scripts/performance-audit --profiles` on a
GitHub-hosted physical Darwin ARM64 runner. This covers the
Scenario, recursive Fibonacci, sparse-grid, compiler-stage, and runtime-mode
families, and writes CPU and allocation profiles for each. Its output, command
log, fingerprint, and any `INCOMPLETE` marker are uploaded regardless of the
result. Pull-request CI remains the owner of structural and allocation-budget
gates; the scheduled job supplies repeatable long-run evidence without
duplicating noisy wall-time assertions in PRs.

Scheduled EPW2 jobs retain exact 1,024-swap logs plus per-swap child/RSS samples
and a summary PASS marker on physical Linux x86-64 and Darwin ARM64 hosts. A
separate GitHub-hosted Apple Silicon job acquires and compares the two all-37
prepared-worker admission captures. Pull-request Linux x86-64 CI owns the
same-revision x86-64 admission artifact: paired captures under `a` and `b`, and
the resource receipt under `soak`.

## Lane Helper

```sh
scripts/check-lane root
```

The root lane owns the current module.

## Standard Helper

```sh
scripts/check
```

It runs:

- non-writing `gofmt`;
- shell syntax checks for scripts;
- `go test -count=1 ./...` (including the default vet pass);
- `git diff --check` when the directory is inside a Git worktree.

The standard CI workflow also runs these required lanes independently:

```sh
go vet ./...
go test -race -count=1 ./...
go test -gcflags=all=-d=checkptr=2 -count=1 ./...
```

Platform coverage is explicit in CI. Target-native Darwin, Linux, and Windows jobs on
both ARM64 and x86-64 run the no-cgo test suite, including the real EPW2
launch/handshake/apply/reload/close lifecycle, and build the packages. The
Linux x86-64 job acquires paired pinned-Luau worker evidence. The standard
Linux lane also cross-builds the combined worker for all six declared targets.
The Linux 386 lane remains a separate compile-only compatibility check:

```sh
GOOS=linux GOARCH=386 go test -run '^$' ./...
```

Although this command is compile-only by test selection, `go test` still starts
the produced test binary. Run it on a Linux host; a Darwin host reports
`exec format error` after successfully compiling the 386 packages.

Allocation-budget tests that cannot produce meaningful numbers under pointer
instrumentation skip only their measurement section; semantic checks remain
active.

## Full Helper

```sh
scripts/check-full
```

It runs `scripts/check` and then the vet, race, and checkptr lanes above.

## Documentation-Only Changes

Run:

```sh
git diff --check
perl -ne 'print "$ARGV:$.:$_" if /[^\x00-\x7F]/' <changed-doc-files>
awk '/[ \t]$/ { print FILENAME ":" FNR ": trailing whitespace" }' <changed-doc-files>
```

ASCII is the default for repository docs. Use non-ASCII only when the file
already uses it or the content clearly needs it.

## Notes

- Do not run formatters that rewrite unrelated files.
- Do not invent new tooling for one-off checks.
- If a check helper fails, fix the underlying issue instead of skipping the
  helper.
