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
