# Ember Agent Guide

Keep this file short. It is the entrypoint for agents, not the full project
manual.

Understand the local shape. Make the useful change. Verify it. Explain the
result plainly.

Work toward earned simplicity. Understand the runtime shape deeply enough that
the final code feels obvious: remove accidental complexity, keep essential
complexity contained, and leave Ember easier to reason about than you found it.

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
- Prefer Go-native runtime mapping behind Ember interfaces: ordinary Go objects,
  Go GC, and standard Go runtime tools before custom allocator, GC, or scheduler
  machinery.
- Keep unsafe code, native codegen, and host I/O at explicit seams.

## Working Rules

- Make the useful cohesive change at the right level.
- Prefer simple, readable Go with clear ownership and narrow interfaces.
- Prefer small private functions for single decisions or transformations; keep
  business logic pure when practical.
- Keep public interfaces minimal, explicit, and test-backed.
- Add features only when examples, tests, or implementation pressure prove they
  are useful or necessary.
- Convert parser, compiler, runtime, and host input into clean internal data
  early.
- Keep side effects near the edges; let runtime policy decide what should happen
  and host mechanisms perform effects.
- Pass dependencies explicitly. Do not smuggle clocks, randomness, host
  services, or Hearth worlds through globals.
- Keep constructors, imports, module loading, and global setup boring; hide no
  expensive work there.
- Do not add dependencies unless they clearly reduce real complexity.
- Do not add Hearth integration to the root runtime package until an embedding
  slice proves the interface.
- Prefer a flat root package until implementation pressure proves a split is
  needed.

## Recommended Plugins

Use `real-engineering-stack:tdd` and `real-engineering-stack:codebase-design`
for future compiler, VM, runtime, and embedding slices.

- Use `real-engineering-stack:tdd` to grow Ember through source-to-result or
  bytecode-to-result behavior tests before implementation. Tests should prove
  observable Luau-shaped behavior through public interfaces such as `Compile`
  and `Run`, not private parser, emitter, or VM internals.
- Use `real-engineering-stack:codebase-design` to keep modules deep: small
  public interfaces, private internal seams, and package splits only when real
  implementation pressure proves they improve locality or testability.

Together, these plugins keep the runtime moving in vertical slices while
protecting the root package from speculative architecture.

## Context Budget

- Prefer targeted reads over whole-repo dumps, but read enough code to
  understand the seam you are changing.
- If the change crosses compiler, VM, runtime, analysis, or embedding seams,
  explain the broader read before expanding it.
- Do not paste full logs, full diffs, repeated file lists, benchmark dumps, or
  large generated files unless asked.
- Summarize long command output and preserve the exact command.
- Keep final output compact: changed files, checks, risks, and next pressure.

## Tooling Rules

- Use `rg` or `rg --files` for search.
- Use focused package tests first for the package or example you changed.
- Prefer deterministic tests; control time, randomness, environment, filesystem,
  network access, and host callbacks.
- Use `scripts/check-lane <lane>` for lane-local iteration.
- Use `scripts/check-fast` as the pre-finish sweep.
- Use `scripts/check` before finishing code changes.
- Use `scripts/check-full` only when the user explicitly requests it.
