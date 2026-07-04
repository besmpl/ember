# Go Coding Rules

Use these rules when writing Go for Ember.

## Core Style

- Prefer small private functions that do one specific job.
- Keep dependencies minimal and obvious.
- Write boring, readable code first.
- Keep runtime logic pure when practical.
- Keep outer wiring layers thin.
- Make the smallest correct change that solves the task.
- Do not add features until real use proves they are useful or necessary.

## Package Design

- Minimize the number of packages.
- Split packages by responsibility, not by file type or architecture trend.
- Avoid junk-drawer packages named `utils`, `helpers`, `common`, or `misc`.
- Keep public interfaces small.
- Export only what callers actually need.
- Design for the caller, not for internal machinery.
- Prefer accepting interfaces and returning concrete types.
- Do not use pointers to interfaces.
- Avoid package-level mutable state.
- Avoid `init()` unless there is a strong reason.

Before designing a public package or library, read:
<https://abhinavg.net/2022/12/06/designing-go-libraries>

## Naming

- Follow Go naming guidance: <https://go.dev/talks/2014/names.slide>
- Use short names when context is clear.
- Use longer names when scope is larger or ambiguity exists.
- Name interfaces by behavior, such as `Reader`, `Writer`, `Store`, or
  `Clock`.
- Avoid stutter when a clearer name exists.
- Avoid cryptic abbreviations unless they are standard in the domain.

## Interfaces And Abstractions

- Do not create interfaces just in case.
- Define interfaces close to where they are consumed.
- Prefer functions over interfaces when that is enough.
- Keep interfaces small.
- Add abstractions only when they remove real complexity, provide multiple real
  implementations, or create a useful test seam.
- Avoid unnecessary inheritance-style object graphs, factories, registries, or
  service locators.

## State And Side Effects

- Avoid global mutable state.
- Keep configuration at startup and pass typed values into real code.
- Do not hide expensive work in constructors, imports, or package setup.
- Make startup behavior explicit.
- Give every background task a clear lifetime and cancellation path.

## Error Handling

- Return errors instead of panicking in normal application flow.
- Add useful context before returning errors upward.
- Do not swallow errors silently.
- Do not log and return the same error from multiple layers.
- Use sentinel errors and custom error types only when callers need to branch on
  them.
- Make error messages actionable.

## Testing

- Write tests for small units first when practical.
- Test behavior, not implementation details.
- Prefer table-driven tests when multiple cases become clearer.
- Do not force a table test for one case.
- Keep tests readable, even when a little repetition helps.
- Use private tests in the same package when testing internals makes sense.
- Use external package tests when validating public interface.
- Mock at boundaries; avoid excessive mocking of tiny objects.
- Test important error paths.
- Aim for meaningful coverage once code exists.
- For concurrent code, run `go test -race ./...`.

## Dependencies

- Prefer the standard library.
- Add dependencies only when they clearly reduce complexity or provide serious
  value.
- Choose boring, maintained dependencies.
- Do not add a dependency to save a few simple lines.
- Use simple dependency injection: pass functions, values, interfaces, or
  configured structs directly.

## Concurrency

- Do not add goroutines casually.
- Every goroutine needs a clear owner and stop path.
- Use `context.Context` for I/O, blocking work, network calls, database calls,
  or long-running operations.
- Put `ctx context.Context` first for functions that accept it.
- Prefer ownership, message passing, immutability, or simple synchronization
  over shared mutable state.
- Avoid leaked timers, orphaned tasks, unbounded queues, and hidden retries.

## Data Handling

- Validate external input at boundaries.
- Convert raw input into well-shaped internal types early.
- Keep parsing, validation, and normalization separate from core runtime logic
  when that improves clarity.
- Preserve existing data contracts unless the task explicitly changes them.
- Be careful with bytecode, serialization formats, and public wire contracts.

## Public Interface Design

- Make public interfaces hard to misuse.
- Keep public interfaces small, simple, and useful.
- Prefer explicit parameters and clear return values.
- Avoid hidden mutation and surprising defaults.
- Document exported names.
- Document public behavior, edge cases, errors, defaults, and side effects.
- Do not expose internals for convenience.
- Treat public interface changes as deliberate compatibility decisions.

## Tooling

- Always run `gofmt` after editing Go.
- Run package-local `go test` commands first for the package you changed.
- Run `scripts/check-lane <lane>` as the lane-first iteration check for the
  ownership area you changed.
- Run `scripts/check-fast` as the pre-finish sweep when practical.
- Run `scripts/check` before completing broader changes when practical.
- Run `scripts/check-full` only when the user explicitly requests it.
- Use `staticcheck` when available.
- Keep CI strict enough to catch simple mistakes before review.

## Final Rule

Keep it simple until complexity proves it deserves to exist.
