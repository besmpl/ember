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
the breaking `guest_batch_v1` contract: one parameterized program and callable
per case, one positive runtime seed shared by every N point and repeat, runtime
`N={1,10,100,1000}`, one timed outer entry that executes N seed-dependent guest
calls, exact integer checksums, and three fitted-slope repeats. Public-call
lifecycle and allocation measurements remain separate. Output directories are
caller-owned and must not already exist.

```sh
CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase full \
  --output /tmp/ember-parity-full

CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase speed2x --capture-only \
  --capture-role frozen-current --capture-pair a \
  --output /tmp/ember-speed2x-a

CGO_ENABLED=0 GOMAXPROCS=1 LUAU_BIN=/opt/homebrew/bin/luau \
  scripts/check-runtime-parity --phase prepared-native-parity15 \
  --capture-role candidate --capture-pair a \
  --output /tmp/ember-prepared-native-a
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
enters the bound owner directly so `guest_batch_v1` does not include
`Runtime.Invoke` lifecycle work. Separate public-path tests prove that the same
verified bundle executes through `Runtime.Invoke`, owns and detaches results
correctly, and closes independently. The phase and execution mode are recorded
in schema-v2 artifacts and `command.txt`; workload identity never participates
in engine selection.

`prepared-native-parity15` accepts only `candidate` and selects
`prepared-native`. It freezes four materially different general numeric rows:
arithmetic `for`, `while` plus branching, captured recursive Fibonacci, and
iterative Fibonacci. The measured path is the public unknown-source
`LoadProgram -> Prepare -> Activate -> Use -> Runtime.Invoke` path. Every
median and worst paired Ember/Luau ratio must be at most 1.50. The phase fails
when native execution is unavailable; exact Machine fallback is correct but is
not native-performance evidence. Accepted ratio receipts now cover both
emitted instruction sets: the pinned Darwin ARM64 environment below and the
explicit Linux x86-64 profile. Native CI on every supported OS/ISA separately
proves execution, ABI, mapping protection, and lifecycle behavior; a platform
job never promotes a missing or contaminated timing acquisition into
performance evidence.

The explicit `linux-amd64` acceptance profile extends this same four-row gate
to physical x86-64 without weakening the Darwin pin. It requires Go 1.26.4,
`CGO_ENABLED=0`, `GOMAXPROCS=1`, Linux/amd64, and the official Luau 0.728 Linux
binary at SHA-256
`2a6ff9e7c17a0a6fed47c04da67495d1594eda38ce915f01c78c7fa5e9e796b8`.
The environment artifact records the exact kernel and CPU model because hosted
runner hardware is attributable but not a stable CPU SKU. Run it with:

```sh
CGO_ENABLED=0 GOMAXPROCS=1 \
  EMBER_RUNTIME_ACCEPTANCE_PROFILE=linux-amd64 \
  LUAU_BIN=/path/to/pinned/linux-amd64/luau \
  scripts/check-runtime-parity --phase prepared-native-parity15 \
  --capture-role candidate --capture-pair a \
  --output /tmp/ember-prepared-native-linux-amd64
```

Pull-request CI verifies both the release archive and extracted executable
digests before running this profile, then uploads the complete capture. A green
job is an exact x86-64 receipt; a timing failure remains visible rather than
being converted to semantic-only success.

Exact commit `6bd26873b09aadbfe473098ae20dde2bc978703d`, CI run
`29651961727`, and capture
`afeaee0a2f8cc43c8fc367195d5e8cf6333b161c670bdea8e07819c290dfa534`
record Linux `6.17.0-1020-azure` on an AMD EPYC 9V74, with environment hash
`28a72d89a5d48af3851b647979595791be357e2f9b50c1386b479d41c883bceb`.
All 96 raw rows are uncontaminated. The paired median/worst ratios are:

| Frozen row | Darwin ARM64 | Linux x86-64 |
| --- | ---: | ---: |
| arithmetic `for` | 0.681820 / 0.691079 | 0.513470 / 0.717228 |
| `while` plus branch | 0.294280 / 0.296317 | 0.585633 / 0.589150 |
| recursive Fibonacci | 0.167200 / 0.171297 | 0.237191 / 0.237788 |
| iterative Fibonacci | 1.150250 / 1.218300 | 0.673680 / 0.728353 |

The fit is `T(N)=entry+N*inner`, and `N` repetitions execute inside one
qualified guest batch. The ratio therefore measures the private repeated
native body while placing the one-per-batch Go/OS adapter in the intercept.
That body is OS-independent for a given ISA; platform-specific mapping and the
Windows x86-64 calling-convention adapter remain outside it and have their own
native execution/ABI/lifecycle tests. This makes the ARM64 and x86-64 receipts
faithful performance equivalents for the qualified repeated body on the other
supported OSes. It is not a claim about a different CPU model, one-shot entry
latency, an arbitrary native/fallback mix, or all Luau programs.

Acquisition starts after three one-second samples with aggregate CPU at most
300%. One-minute load remains diagnostic but is not an admission gate: it is
lagging, core-count-blind, and includes blocked work that may not compete with
this single-threaded benchmark. Live before/after probes cap external processes
at three cores while excluding the measuring Go process. A live point whose
before or after probe is contaminated is discarded and retried after one
second, up to 60 attempts; only clean paired rows are emitted, and exhaustion
still fails the capture. A failed sampling command is a distinct observer
error, not runner contention.

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
all 37 cases in both captures. Dynamic acceptance defaults to per-row median at
most 1.85 and p90 at most 2.0. Prepared final evidence uses median at most 1.0
and p90 at most 1.05. Reload-time native evidence uses median and worst at most
1.50 for its frozen four-row subset. Candidate/current comparisons bind the
manifest and both baseline directories and require each paired median to stay
at or below 1.05.

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

`.github/workflows/scheduled.yml` runs the long-lived checks weekly (and by
manual dispatch). The five fuzz targets each have a separate matrix entry,
`fail-fast: false`, and a bounded 10-minute fuzz budget. Every entry uploads its
log and `testdata/fuzz/<target>` corpus, including when the fuzz process fails.

The runtime parity job runs caller-named `full` and `speed2x` all-37 captures on the
controlled self-hosted Darwin 24.6.0 arm64 Apple M1 runner labeled
`[self-hosted, macOS, ARM64, apple-m1, ember-parity]`, with the pinned Luau
executable. It uploads exact schema-v2 raw and fitted-slope artifacts plus the command,
source, toolchain, Luau, CPU, OS, and environment fingerprint. Invalid or
contaminated acquisition fails; a baseline speed miss is retained honestly.

The performance job runs `scripts/performance-audit --profiles` on a controlled
Apple M1 runner labeled
`[self-hosted, macOS, ARM64, apple-m1, ember-performance]`. This covers the
Scenario, recursive Fibonacci, sparse-grid, compiler-stage, and runtime-mode
families, and writes CPU and allocation profiles for each. Its output, command
log, fingerprint, and any `INCOMPLETE` marker are uploaded regardless of the
result. Pull-request CI remains the owner of structural and allocation-budget
gates; the scheduled job supplies repeatable long-run evidence without
duplicating noisy wall-time assertions in PRs.

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

Platform coverage is explicit in CI: Linux amd64 runs the standard checks and
the pinned native parity gate, macOS and Linux arm64 run the test suite (arm64
also builds), Windows x86-64 and ARM64 run strict-cgo-off tests and builds on
native hosted runners, and the Linux 386 lane uses the compile-only command
below:

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
