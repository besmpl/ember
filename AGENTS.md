# Ember Agent Guide

Keep this file short. It is the entrypoint for agents, not the full project
manual.

## Start Here

1. Read `README.md` for the project direction.
2. Read `docs/README.md` to find the smallest relevant doc set.
3. Read `docs/golang-rules.md` before writing Go.
4. Read `docs/checks.md` before choosing verification commands.
5. Read nearby package docs before editing that area.

## Project Direction

Ember is a Go-native Luau-compatible scripting runtime for Hearth. It should be
grown in small vertical slices: bytecode, values, VM execution, compiler paths,
standard libraries, host embedding, compatibility tests, and only later deeper
analysis or native-code experiments.

## Core Idea

Ember should make Luau-shaped scripting feel natural from Go. Core runtime
logic should stay easy to test with plain values, while host systems such as
Hearth worlds, files, clocks, randomness, networking, logging, and platform
lifecycles remain explicit outer layers.

- Keep the root package small until real examples prove a split is useful.
- Use upstream Luau as the behavioral reference, not as a mechanical porting
  target.
- Preserve deterministic behavior where Hearth simulation needs it.
- Prefer compatibility tests and vertical examples over speculative public API.
- Keep unsafe code, native codegen, and host I/O at explicit seams.

## Working Rules

- Make the smallest cohesive change that proves useful behavior.
- Prefer simple, readable Go over clever abstractions.
- Keep public interfaces minimal, explicit, and test-backed.
- Add features only when examples, tests, or implementation pressure prove they
  are useful or necessary.
- Keep side effects near the edges and core runtime behavior easy to test.
- Do not add dependencies unless they clearly reduce real complexity.
- Do not add Hearth integration to the root runtime package until an embedding
  slice proves the interface.
- Prefer a flat root package until implementation pressure proves a split is
  needed.

## Context Budget

- Prefer targeted reads over whole-repo dumps.
- If more than three files seem necessary, explain why before reading widely.
- Do not paste full logs, full diffs, repeated file lists, benchmark dumps, or
  large generated files unless asked.
- Summarize long command output and preserve the exact command.
- Keep final output compact: changed files, checks, risks, and next pressure.

## Tooling Rules

- Use `rg` or `rg --files` for search.
- Use focused package tests first for the package or example you changed.
- Use `scripts/check-lane <lane>` for lane-local iteration.
- Use `scripts/check-fast` as the pre-finish sweep.
- Use `scripts/check` before finishing code changes.
- Use `scripts/check-full` only when the user explicitly requests it.
