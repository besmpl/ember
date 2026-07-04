# Roadmap

Ember should grow through small vertical slices. Each slice should add tests
that prove real behavior.

## Slice 0: Scaffolding

- Project docs.
- Go module.
- Check scripts.
- Empty root package.

## Slice 1: Bytecode Seed

- Status: seeded for constants, globals, register moves, table fields and
  indexes, addition, host calls, and return as internal bytecode.
- Next pressure: add instruction encoding or decoding helpers when fixtures or
  compatibility tests need them.

## Slice 2: Tiny VM

- Status: seeded for nil, booleans, numbers, strings, host-visible tables, and
  a tiny register VM.
- Next pressure: add bytecode fixtures when compatibility tests need them.

## Slice 3: Minimal Compiler Path

- Status: seeded for numeric return expressions, local number bindings,
  scalar literals, array and named-field table literals, dot-field and bracket
  reads, identifier references, assignment to existing locals and table
  indexes, local shadowing around initializers, and `if`/`then`/`else`/`end`
  control flow with Luau truthiness.
- Next pressure: expand source categories one at a time with source-to-result
  tests.

## Slice 4: Tables And Calls

- Status: seeded for host-visible `Table` objects, array and named-field table
  literals, dot-field and bracket reads, field and bracket assignment, nil
  assignment deletion, boolean keys, table identity keys, and explicit Go host
  callbacks through `RunWithGlobals`.
- Next pressure: add closures and richer call semantics only when
  source-to-result tests need them.
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
