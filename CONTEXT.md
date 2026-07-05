# Ember Context

Ember is a Go-native Luau-compatible scripting runtime. This glossary names the
domain concepts future runtime, compiler, analyzer, and host-embedding work
should use consistently.

## Language

**Source**:
Luau text accepted by Ember for parsing, typed analysis, compilation, or
execution.
_Avoid_: input string, script text, code blob

**Runtime Behavior**:
The observable behavior produced when Ember executes compiled source.
_Avoid_: type behavior, checker behavior

**Typed Analysis**:
The analysis of source annotations, inferred types, and type-directed
diagnostics before or apart from execution.
_Avoid_: runtime type checking, static pass

**Type Syntax**:
The parsed source form of a type annotation, alias body, cast, type argument, or
type-function use before names and aliases are resolved.
_Avoid_: parsed type, type AST when discussing the domain model

**Type Fact**:
A resolved statement about a source expression, binding, module export, or host
name that typed analysis can rely on or report to callers.
_Avoid_: checker state, inferred blob

**Uncertainty**:
The typed-analysis state where Ember does not know enough to prove a precise
type fact and must follow the current check mode's policy.
_Avoid_: failure, missing type

**Type Pack**:
The type-level shape of Luau value lists, including function parameters,
multiple returns, varargs, and generic pack parameters.
_Avoid_: tuple type, vararg type

**Constraint**:
A typed-analysis obligation that must be solved or deliberately tolerated under
the current check mode.
_Avoid_: rule, validation

**Evidence**:
The source-level reason a constraint exists, used to explain diagnostics without
exposing solver internals.
_Avoid_: debug info, error context

**Refinable Place**:
A source place whose type can be narrowed by control flow, such as a local,
upvalue, global, or property access that typed analysis can track safely.
_Avoid_: lvalue, variable when property accesses are included

**Flow View**:
The temporary set of narrowed type facts visible at one control-flow point.
_Avoid_: mutated environment, branch scope

**Check Mode**:
The source directive mode that controls how typed analysis treats uncertainty:
nocheck, nonstrict, or strict.
_Avoid_: strictness flag, checker setting

**Diagnostic**:
A typed-analysis finding with a source range and enough context for a caller to
show or report the problem.
_Avoid_: error string, warning blob

**Module Summary**:
The durable typed facts about one source module that another source module can
use without reanalyzing private implementation details.
_Avoid_: cache entry, export map

**Type Environment**:
The set of global names, foreign types, standard-library types, and module
summaries available while analyzing source.
_Avoid_: globals map, checker config

**Foreign Type**:
A type supplied by an embedder or host platform rather than declared directly in
the analyzed source.
_Avoid_: native type, external class

**Host Adapter**:
The outer code that supplies host behavior or host type knowledge to Ember while
keeping host ownership outside the root runtime.
_Avoid_: integration layer, plugin, binding

**Type Runtime**:
The analysis-time execution environment used by Luau type functions to operate
on reflected type values.
_Avoid_: compile-time evaluator, macro runtime

**Typed Artifact**:
The complete typed-analysis result for a source module, including diagnostics,
module summary, and optional tooling facts.
_Avoid_: AST with types, checker output
