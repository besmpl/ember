# No-CGO Architecture Proof

This command measures an architecture ceiling, not an Ember production
backend. Each Go function is a manual semantic lowering of a parameterized
Luau case. The lowerings use runtime-selected seeds and one program for all
`N={1,10,100,1000}` points; they do not branch on the case name, expected
result, timing point, or source hash.

The proof answers two bounded questions:

1. Can direct, typed, whole-program Go code beat pinned Luau across the runtime
   families that dominate Ember's all-37 gaps?
2. Does preserving generic Go strings, maps, closures, and variadic slices
   reverse that result?

It does not prove that Ember can generate these lowerings. Production
retention still requires a general compiler, transformed holdouts, exact side
exits, lifecycle tests, and the complete all-37 gate.

Run deterministic semantic checks:

```sh
CGO_ENABLED=0 go test ./internal/architectureproof
CGO_ENABLED=0 GOMAXPROCS=1 \
  EMBER_ARCHITECTURE_PROOF_LIVE=1 \
  LUAU_BIN=/opt/homebrew/bin/luau \
  go test -run '^TestProofCasesMatchLuau$' ./internal/architectureproof
```

Acquire a clean capture through `scripts/check-architecture-proof`. The
`--allow-busy` mode is exploratory only and records that fact in
`backend-facts.tsv`.

The static ARM64 fixture is direct function text behind Go's supported ABI0
assembly boundary. It intentionally is not a descriptor interpreter. The
discarded JIT feasibility spike remains external evidence because executable
memory and indirect calls are forbidden by Ember's retained pure-Go boundary.
