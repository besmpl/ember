# Roadmap

Ember should grow through small vertical slices. Each slice should add tests
that prove real behavior.

## Slice 0: Scaffolding

- Project docs.
- Go module.
- Check scripts.
- Empty root package.

## Slice 1: Bytecode Seed

- Define opcode constants for a small instruction subset.
- Add instruction decoding helpers.
- Test encoding and decoding against known fixtures.

## Slice 2: Tiny VM

- Add basic values: nil, bool, number, string.
- Execute a tiny bytecode fixture.
- Return values and runtime errors through Go.

## Slice 3: Minimal Compiler Path

- Parse or construct a tiny source path such as `return 1 + 2`.
- Compile to bytecode accepted by the tiny VM.
- Test source-to-result behavior.

## Slice 4: Tables And Calls

- Add tables, closures, and Go host callbacks.
- Prove a script can call Go and receive values.
- Keep host side effects explicit.

## Slice 5: Conformance Categories

- Import selected upstream conformance categories.
- Track unsupported categories directly in docs or test names.
- Add differential tests where practical.

## Later Pressure

- Standard libraries.
- Coroutine and yielding semantics.
- Parser completeness.
- Compiler completeness.
- Analyzer and type checking.
- Performance work.
- Native codegen experiments only if real usage proves the need.
