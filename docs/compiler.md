# Compiler representation and budgets

The compiler stores syntax in typed arenas. Nodes contain compact IDs and
`nodeSpan` ranges into arena-owned child arrays; they do not retain pointers to
child nodes. Zero is reserved for an absent ID, and every resolver checks the
ID or span before indexing. The same representation is used for expressions,
statements, and types. Source locations remain explicit `sourceRange` values.

The hot representation gates are enforced by `TestCompilerE8LayoutBudgets`:

| representation | measured size on darwin/arm64 | gate |
| --- | ---: | ---: |
| `syntaxID` | 8 B | <= 8 B |
| expression, statement, and type IDs | 4 B | <= 4 B |
| `nodeSpan` | 8 B | <= 8 B |
| `sourceRange` | 16 B | <= 16 B |
| `arenaExpression` | 16 B | <= 16 B |
| `arenaCall` | 24 B | <= 32 B |
| `arenaFunction` | 96 B | <= 96 B |
| `arenaTerm` | 56 B | <= 64 B |
| `arenaStatement` | 16 B | <= 16 B |
| `arenaLocalStatement` | 40 B | <= 48 B |
| `arenaFunctionStatement` | 128 B | <= 128 B |
| `arenaType` | 48 B | <= 64 B |
| `arenaFunctionType` | 48 B | <= 64 B |
| `bytecodeIRInstruction` | 28 B | <= 32 B |

`TestCompilerE8AllocationBudgets` measures compile allocation space and
allocation count over three deterministic repetitions. The fixtures are
generated from fixed source shapes: a 256 KiB straight-line program, 512
branches, 512 type aliases, and 10,000 nested parentheses rejected at a
64-level nesting limit. One representative darwin/arm64 three-run sample is:

| fixture | source | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| straight-line parse | 256 KiB | 14.5 MiB | 67 |
| straight-line full compile | 256 KiB | 20.8 MiB | 199 |
| straight-line optimize | 256 KiB | 5.7 MiB | 86 |
| branch-heavy compile | 29 KiB | 4.1 MiB | 16,104 |
| type-heavy compile | 33 KiB | 3.1 MiB | 282 |
| malformed deep compile | 20 KiB | 4.0 MiB | 39 |

The final compiler gates are intentionally expressed as ceilings rather than
machine-specific performance targets:

- `bytecodeIRInstruction` is at most 32 bytes;
- 256 KiB optimizer allocation space is below 10 MiB per operation;
- 256 KiB parsing uses fewer than 30,000 allocations per operation;
- 256 KiB full compilation uses fewer than 30 MiB and fewer than 30,000
  allocations per operation;
- compiler corpus and malformed-input tests remain green.

The allocation tests are structural regression gates, not wall-clock
benchmarks. For stage-by-stage profiling, run the existing
`BenchmarkCompilerStageMatrix` benchmarks and record the source fingerprint,
Go version, and target architecture with the result.
