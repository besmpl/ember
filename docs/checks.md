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
scripts/performance-audit --output /tmp/ember-audit-before
scripts/performance-audit --output /tmp/ember-audit-after --profiles
```

The output directory must not already exist. The runner requires a clean
worktree; pass `--allow-dirty` when the current changes are intentionally part
of the capture. Set `BENCHTIME` to shorten exploratory runs. `--profiles`
writes unique CPU and allocation profiles for each family and never overwrites
an existing capture. The compiler-stage family is retained as raw `go test`
output because `scripts/bench-summary` only formats runtime and parity rows.
Failed runs leave an `INCOMPLETE` marker in the newly created directory.

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
- `go test -vet=off -count=1 ./...`;
- `git diff --check` when the directory is inside a Git worktree.

## Full Helper

```sh
scripts/check-full
```

For now this aliases `scripts/check`. Expand it only when the repository has
real slower checks to run.

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
