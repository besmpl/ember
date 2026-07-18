<!-- simple-loop-plan -->

# Generated adaptive superword VM implementation plan

## Lead architecture audit

### Evidence, inspirations, and cutoff

The dynamic VM baseline remains the exact-commit Apple-M1 all-37 capture at `2cf1e08d`: 5.705x Luau geomean, 2.439x best, 11.567x worst, with arithmetic 5.359x, recursion 5.260x, arrays 4.604x, and event dispatch 8.066x. At current HEAD `683b77af`, `TestScenarioMechanismAttributionCoversCurrentWorstRows` shows stable field PICs overwhelmingly hit across general scenarios while `MOVE`, arithmetic, branches, iteration, calls, and field operations recur. The production direct loop is already 62,944 bytes of linked text; its generated Go is 129,276 bytes. Compact wordcode, 16-byte `Value`, direct `*Table`, PICs, fixed calls, and owner-local caches are proven good. The 68-byte Machine operation format, helper-heavy dynamic tables, and a second semantic state are proven bad outside prepared AOT.

| Primary design | Transfer to Ember | Cutoff |
| --- | --- | --- |
| [Luau interpreter](https://luau.org/performance/) and [CPython PEP 659](https://peps.python.org/pep-0659/) | Local typed/call/field specialization with adjacent caches and trivial per-instruction dequickening. | CPython reports roughly 10%-60%; quickening alone cannot close 5.705x. |
| [BEAM loader rewriting](https://www.erlang.org/doc/apps/erts/beam_makeops.html) | Repeated operand specialization, safe peephole fusion, compact operand packing, and generated handlers from one definition. | A finite catalog is required; all-pairs superinstructions would explode code and instruction cache. |
| [Deegen](https://fredrikbk.com/publications/deegen.pdf) | Build-time semantic generation, specialization, cache generation, and outlined slow paths are the strongest portable combination. | Its LLVM tail dispatch, fixed-register pinning, CPS convention, and runtime JIT do not transfer safely to Go. Even its 2.79x PUC-Lua result would only project Ember to about 2.04x. |
| [Lazy basic-block versioning](https://drops.dagstuhl.de/entities/document/10.4230/LIPIcs.ECOOP.2015.101) | Bounded block-entry type context can avoid repeated tag checks. | Native block cloning/JIT is inadmissible; retain only compact wordcode variants. |
| [Ignition](https://v8.dev/blog/ignition-interpreter) and [copy-and-patch](https://arxiv.org/abs/2011.13127) | Confirm generated handlers and stencils can be fast. | Native handlers, executable memory, private calling conventions, and runtime code generation violate hard boundaries. AST self-optimization, batching, and foreign/Wasm VMs either lose single-script generality or add a forbidden backend. |

### Selected alternative

Build a private **Generated Adaptive Superword VM**. A declarative opcode/effect specification generates generic, instrumented, typed, cached, and fused handler families plus loader-tiling legality. Each `vmFunctionInstance` lazily owns compact mutable shadow wordcode, counters, and cache cells; shared `Proto.words` stays immutable. Selection uses only opcode/operand roles, observed value kinds, callee/arity, table shape/metatable versions, and CFG legality. Constants may be operands but never workload selectors.

Canonical VM frames and `Value` registers remain live after every source instruction. A guard miss before mutation executes the generic equivalent at the same word PC and can dequicken a saturating site; pure fused handlers preserve original PC, charge, limit, cancellation, and error boundaries. Effectful, yielding, protected, debugging, module, host, or unproved mutation sequences never fuse. Stable hits bypass repeated tag/helper/dispatch work; bounded block context propagates proven kinds; BEAM-style tiling removes dispatch around recurring moves and other eligible sequences in any script.

This is not the SSA-region plan: it has no SSA IR/banks, scalar replacement, hot-region entry, inlined-frame snapshots, or materializing deoptimization. It has simpler fallback and broader eligibility but a lower peak ceiling. The four-family gate below must prove that Go dispatch still leaves enough headroom; otherwise delete it.

## Outcome

- **Eventual goal:** Dynamic scripts unavailable at Go build time match or beat pinned Luau across all 37 cases while retaining exact Ember production behavior and boundaries.
- **Current run:** Completed by the C1 deletion path after the bounded P4 evidence disproved the generated-superword performance premise.

### Run result (2026-07-18)

Candidate `d6b984eb` passed exact semantic and lifecycle differentials and
moved arithmetic and recursion to about 1.60x-1.67x Luau. The two required
all-37 captures still measured arrays at 2.836x/1.827x and event dispatch at
2.265x/2.050x. P4-B also regressed 26 of 37 rows by more than 5% against the
comparable frozen baseline. Guest-batch allocation scaled to about 2.0 MB for
1,000 array guests and 2.46 MB for 1,000 event guests.

The holdout remained sealed because the standard gate failed first. P5 and P6
were not run. C1 removed all production adaptive machinery and retained only
zero-cost audit fixtures. Compact pointer-free plans, semantic tiling,
owner-local feedback, and pre-mutation same-PC exits are proven transferable;
generic shadow tax and canonical heap-object construction are the decisive
limits. Crossing that limit requires escape analysis, scalar replacement, and
materializing exits—a distinct architecture, not another superword family.

### Done when

- D1. Two exact-commit all-37 VM captures bind results, timings, allocations, profiles, normalized instruction/cache traffic, and an independently sealed holdout; the attributable audit records wins, failures, transfer limits, and rank reversals.
- D2. One declarative semantic source generates generic/instrumented/specialized handlers, shadow encoding, cache layout, charge/effect/error metadata, and fusion legality; duplicate switch semantics are removed without changing public API.
- D3. Arithmetic, recursion/direct calls, arrays/tables, and event dispatch each reach median `<=2.00x` Luau in two exploratory captures, with exact results, no warmed allocation increase, no untargeted row regression over 5%, and holdout agreement.
- D4. Two final all-37 captures have every median `<=1.00x`, nearest-rank p90 `<=1.05x`, exact hashes, and warmed B/op and allocs/op no higher than frozen ceilings.
- D5. VM/shadow/prepared differentials prove values, errors/PCs/frames, metamethod order, aliases, iteration, limits, cancellation, effects, coroutines, lifecycle, owner isolation, and same-PC fallback at every specialized operation.
- D6. Warm execution allocates zero; at most two variants/site, 96 adaptive/fused handlers beyond the 64 generic opcodes, 96 KiB linked production-loop text, and `4x wordcode + 64 KiB` per owner-Program are hard caps; 100k-call churn plateaus and caches retain no semantic roots.
- D7. Compile/load/cold-call medians regress at most 10%; prepared AOT and artifact freshness remain green; pure Go, flat root package, VM fallback, and the public surface remain unchanged.

### Constraints

- No CGO, dependency, foreign runtime/backend, subprocess, plugin, JIT/executable memory, assembly foundation, private Go ABI, `go:linkname`, benchmark/source identity, or generated-script selector.
- Start each semantic slice red through `Compile`/`Run`, then add generic/shadow/prepared private differentials. Preserve user work and keep captures outside the repository.

## Execution path

### P1. Freeze evidence and generality [D1]
- **Touch:** performance audit, test-only VM observer, next runtime ADR.
- **Change:** Reproduce two baselines; record normalized opcode/type/cache/sequence traffic and linked handler size. Seal seed `0x454d42455205` holdout before inspecting it; bind candidate cutoff and rejected transfers.
- **Proof:** Hash-bound captures, deterministic observer with no production hook, source citations, Amdahl thought experiments, and candidate/rank table reproduce.

### P2. Generate one semantic substrate [D2, D5]
- **Touch:** `vm_dispatch_spec.go`, `cmd/ember-vmgen`, opcode metadata, generated loops and structure tests.
- **Change:** Replace template-copy semantics with one validated declarative spec generating generic and specialized families, encoding, metadata, slow paths, and legal tilings.
- **Proof:** Generator golden/freshness tests, exhaustive opcode coverage/rejection, generic behavior parity, checkptr/race, and production text-size accounting.
- **Depends on:** P1

### P3. Prove numeric and call superwords [D3, D5]
- **Touch:** private shadow loader/executor, `vmFunctionInstance`, arithmetic/branch/call public tests.
- **Change:** Add owner-local shadow code, saturating quickening, bounded block kinds, numeric/branch superwords, fixed direct calls, and same-PC generic fallback.
- **Proof:** Arithmetic and recursion meet D3 under cold, polymorphic, NaN, depth/error, controller, cancellation, forced-miss, and concurrent-owner cases.
- **Depends on:** P2

### P4. Prove objects and decide retention [D3, D5, D6]
- **Touch:** existing PIC/table/call/iteration handlers, owner attach/detach and cache lifecycle tests.
- **Change:** Specialize proven PIC hits, arrays, iteration, builtins, and stable callees; cap/dequicken megamorphism and clear nonsemantic state on collection, pooling, and close.
- **Proof:** Arrays and event dispatch meet D3; sealed holdout, cache churn, root retention, code/memory caps, and rank comparison pass. Otherwise C1 runs before broadening.
- **Depends on:** P3

### P5. Retain only general families [D4-D6]
- **Touch:** measured handler/tiling families and canonical fallback only.
- **Change:** Add remaining normalized families in profile order; retain each only for at least 2% geomean gain, no row over 5% slower, holdout agreement, and unchanged caps.
- **Proof:** Forced-miss/effect-boundary matrix, differential fuzzing, semantic/effect suites, lifetime/allocation gates, and residual profiles after each family.
- **Depends on:** P4

### P6. Select the dynamic default [D4, D7]
- **Touch:** private runtime routing, audit/ADR/docs; no public API.
- **Change:** Default unknown scripts to bounded shadow execution, preserve canonical VM and prepared AOT, and remove losing experiments/observers.
- **Proof:** Strict final capture pair, cold/compile gates, prepared pair, `go generate ./...`, pure-Go check, `scripts/check-fast`, `scripts/check`, and `git diff --check`.
- **Depends on:** P5

## Conditional work

### C1. Delete after architecture reversal
- **Trigger:** P3/P4 misses D3, needs identity tuning, exceeds D6, disagrees with holdout, or loses over 5% to an existing eligible path.
- **Then:** Delete production shadow code/caches, retain only zero-cost audit fixtures, record the failed Go-dispatch premise in the ADR, and compare the still-admissible cutoff without weakening any gate.
- **Proof:** VM/prepared checks and baseline captures pass; production search finds no adaptive executor/cache remnants; ADR binds the failure.
- **Result:** Triggered at P4 and executed; ADR 0009 and the performance audit bind the rejected candidate and exact capture pair.
- **Else:** no work

## Risks and assumptions

- Go switch dispatch and canonical 16-byte values may make parity impossible; D3 is the early stop.
- Fusion can hide cancellation, error, or effect boundaries; generated legality and forced-boundary tests are mandatory.
- Corpus tiling can overfit; semantic keys, the sealed holdout, family caps, and rank-reversal stop carry the generality claim.
