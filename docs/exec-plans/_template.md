# Plan: <slice name>

## Goal

Describe the behavior this slice proves.

## Scope

- In:
- Out:

## Design

Name the module interfaces and seams the slice will introduce or change.

## Steps

1. Add the smallest failing test or fixture.
2. Implement the narrow behavior.
3. Run focused checks.
4. Update docs if the public surface changed.

## Checks

```sh
go test -count=1 ./...
scripts/check
```

## Risks

- Compatibility:
- Performance:
- Public interface:
