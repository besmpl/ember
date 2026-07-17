# ADR 0009: Prove a Generated Adaptive Superword VM Before Retention

Status: Accepted for bounded implementation; production retention pending the
four-family gate.

## Context

ADR 0008 makes generated Go the prepared path for scripts known before
`go build`. General game scripts loaded later still need a dynamic engine. The
complete direct VM is semantically mature, but two clean schema-v2 captures at
exact runtime commit `2cf1e08d6929702cbcb611c527e1a6d1e87d4f5a` measure
5.708984x and 5.718731x Luau geomeans. Arithmetic is about 5.36x-5.39x,
recursive Fibonacci 5.27x, arrays 4.60x-5.20x, and event dispatch
8.07x-8.37x. The bound capture hashes, allocation ceilings, profile hashes,
commands, and environment are recorded in `performance-audit.md`.

Explicit VM-only profiles identify structural costs rather than one missing
table optimization. Arithmetic spends 61.61% flat in the generated direct
loop, 9.38% in `valueKind`, and 7.59% in instruction accounting. Recursion
adds about 22.5% fixed-call entry/resume. Arrays and event dispatch also keep
the direct loop dominant. The deterministic normalized observer shows that
field PICs already overwhelmingly hit while moves, arithmetic, branches,
iteration, calls, and field operations recur across unrelated scenarios.

The production loop is already 62,944 linked text bytes. Adding handwritten
cases or all-pairs superinstructions would increase instruction-cache pressure
without establishing a general specialization policy.

## Candidate cutoff

| Candidate | Transferable evidence | Decision |
| --- | --- | --- |
| [CPython adaptive instructions](https://peps.python.org/pep-0659/) | Per-site typed/call/field quickening, adjacent caches, and trivial local deoptimization. | Use as policy; its reported 10%-60% range is insufficient alone. |
| [BEAM loader rewriting](https://www.erlang.org/doc/apps/erts/beam_makeops.html) | Generated operand variants, repeated safe rewriting, packing, and finite peephole fusion. | Use for load-time tiling and one semantic definition. |
| [Deegen](https://fredrikbk.com/publications/deegen.pdf) | Build-time generation of specialization, caches, and outlined slow paths. | Use the portable generation model; reject LLVM CPS/tail dispatch, register pinning, and JIT machinery. |
| Lazy basic-block versioning | Bounded incoming type context can remove repeated tag checks. | Permit at block entries only; reject native cloning and unbounded versions. |
| Adaptive SSA regions | Can unbox and scalar-replace across larger scopes. | Keep as a separate unimplemented plan, not a simultaneous tier; its snapshots and materializing deoptimization are not this experiment. |
| Function-threaded Go, native stencils, JIT, foreign VM, or helper compiler | Can reduce dispatch with a custom calling convention or native code. | Reject: Go lacks safe tail-threading here and the product forbids private ABI, executable memory, foreign backends, and runtime subprocesses. |

This is a bounded population, not a claim of global optimality. Quickening
alone cannot close a 5.7x gap, and Deegen's 2.79x result over PUC Lua would
project only about 2.04x if it transferred perfectly. The combined candidate
therefore has an early architecture kill gate.

## Decision

Implement one private Generated Adaptive Superword VM as a deep module behind
the existing VM entry seam. It has no public interface. Shared `Proto.words`
remains immutable; each owner-bound `vmFunctionInstance` lazily owns compact
shadow wordcode, saturating counters, variants, and nonsemantic cache cells.
Concurrent owners never share feedback.

One declarative opcode/effect specification must generate generic,
instrumented, typed, cached, and fused handler families, their encoding and
cache layout, original-PC/charge/error metadata, and fusion legality. The
existing template-copy generator is transitional and must not remain a second
semantic source.

Selection may use only opcode and operand roles, observed value kinds,
callee/arity, table shape/metatable versions, and control-flow legality.
Source names, hashes, corpus names, expected results, benchmark cases, and
literal values used only as identity are forbidden. Constants remain semantic
operands, never family selectors.

Canonical VM frames and `Value` registers stay authoritative. A specialized
guard fails before mutation and runs the generic operation at the same word
PC. Only preguarded pure sequences may fuse, while preserving every original
instruction charge, cancellation check, error PC, and limit boundary.
Effectful mutation, host calls, yields, protected calls, modules, debugging,
and unproved metamethod paths remain unfused semantic islands.

The hard bounds are two variants per site, 96 adaptive/fused handlers beyond
the 64 generic opcodes, 96 KiB linked production-loop text, and four times
wordcode plus 64 KiB per owner-Program. Warm shadow execution allocates zero;
caches cannot retain semantic roots and are cleared on collection, pool reset,
detach, and close.

The independent holdout seed is `0x454d42455205`. It is sealed now and must
not be derived or inspected until the handler catalog is frozen at P4. P4 then
uses the existing source-renamed/literal-rewritten all-37 parity transformation
with that seed; no later family may tune from the revealed holdout.

Production retention requires two exploratory captures where arithmetic,
recursion/direct calls, arrays/tables, and event dispatch each have median
`<=2.00x` Luau, exact results, no warmed allocation increase, no unrelated row
over 5% slower, and holdout agreement. Missing any condition triggers deletion
of production shadow machinery and records the failed premise without relaxing
the gate. Broadening and default selection happen only after that decision.

Final acceptance remains two all-37 captures with every median `<=1.00x` and
nearest-rank p90 `<=1.05x`, exact result hashes, frozen allocation ceilings,
semantic differentials, lifecycle plateau, cold/compile limits, and the full
pure-Go verification matrix.

## Consequences

This route reuses Ember's proven `Value`, table, frame, effect, and fallback
state instead of introducing SSA snapshots or another heap. Same-PC fallback
is shallow and testable, and arbitrary newly loaded scripts are eligible by
semantics rather than identity.

The theoretical ceiling is lower than unboxed regions or native code. Exact
per-instruction accounting and canonical values may leave too much overhead.
That uncertainty is intentional: the four-family gate decides retention before
object coverage or handler count broadens.

Prepared AOT, the canonical VM, the flat root package, and the public surface
remain unchanged. No CGO, dependency, assembly foundation, private Go ABI,
`go:linkname`, plugin, subprocess, foreign runtime, JIT, or executable memory
is introduced.
