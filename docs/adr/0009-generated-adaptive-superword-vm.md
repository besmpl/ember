# ADR 0009: Prove a Generated Adaptive Superword VM Before Retention

Status: Rejected after the bounded P4 experiment; production implementation
deleted.

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

## Experiment result

The bounded implementation reached candidate commit
`d6b984eb0964d5385bdb779222d42722183c4cca`. It generated the semantic
catalog, kept mutable state owner-local, and added pointer-free numeric, call,
array, iteration, builtin, compact-leaf, and nested-dispatch functionlets.
Same-PC fallback, cancellation and instruction boundaries, alias guards,
metatable guards, owner isolation, retained-state caps, and immutable-wordcode
differentials passed before measurement.

Two exact `guest_batch_v1` all-37 captures used the same environment hash
`7d7d8f68a17a5295d7401e234d8cd6630ef20ceeef6e8d97aaae1908187d1d19`:

| Pair / capture ID | Arithmetic | Recursion | Arrays | Event dispatch |
| --- | ---: | ---: | ---: | ---: |
| A / `c95bac9df569e808ef67e721fb930573afa9f9dbaf47c3b80ac869f9ec22346a` | 1.669x | 1.600x | 2.836x | 2.265x |
| B / `7727c35163b1cf6c8db5b00516730434478c57d005df80f68a3ed8e73980b53f` | 1.671x | 1.598x | 1.827x | 2.050x |

Every value/result-set hash agreed. Arithmetic and recursion proved that
bounded fusion can repay Go dispatch for scalar work. Arrays missed in A and
event dispatch missed in both captures, so D3 failed without reinterpretation.
Against the comparable frozen B baseline, 26 of 37 P4-B rows also regressed
more than the allowed 5%, with the worst at +22.5%.
The independently sealed holdout was not opened after the standard gate had
already failed.

The retained test-only `BenchmarkRuntimeParityGuestBatchVM` isolates the
lifecycle that the ordinary one-call benchmark hid. On the rejected candidate,
the 1,000-guest array batch allocated about 2,000,178 bytes in 2,004
allocations; event dispatch allocated about 2,464,188 bytes in 14,004
allocations. That heap traffic also explains the unstable long-batch slopes.
Superwords remove dispatch and repeated guards, but canonical heap tables and
closures still materialize once per guest invocation.

This failure is architectural. Adding escape analysis, scalar replacement,
typed banks, snapshots, and materializing exits would turn this tier into the
separately bounded adaptive-SSA-region design. Identity-shaped fusions or a
weaker gate are not acceptable. Conditional C1 therefore deletes all
production shadow words, caches, handlers, tilers, and executors while keeping
only test/audit evidence. The generic VM remains the dynamic fallback and
prepared Go remains the path for scripts known at application build time.

## Consequences

The experiment proved that Ember can reuse its `Value`, table, frame, effect,
and fallback state while specializing arbitrary newly loaded scripts by
semantics rather than identity. Same-PC fallback stayed shallow and testable.

It also proved that this ceiling is too low: exact per-instruction accounting
and canonical heap objects leave too much object-work overhead after dispatch
is fused. The four-family gate stopped the tier before broader coverage could
turn it into a permanent second interpreter.

Prepared AOT, the canonical VM, the flat root package, and the public surface
remain unchanged. No CGO, dependency, assembly foundation, private Go ABI,
`go:linkname`, plugin, subprocess, foreign runtime, JIT, or executable memory
was introduced. Under those constraints, only a separately proved data-only
partial evaluator with escape analysis and scalar replacement has a plausible
path to removing both remaining costs; it must keep its own kill gate rather
than inheriting credit from this failed tier.
