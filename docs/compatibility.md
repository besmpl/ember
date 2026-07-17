# Luau Compatibility

Ember should claim compatibility only where tests prove it.

## Compatibility Sources

Use upstream Luau as the reference for:

- bytecode instruction encoding and decoding;
- parser and compiler behavior;
- VM execution semantics;
- standard library behavior;
- error messages where callers reasonably depend on them;
- coroutine and yielding behavior when implemented;
- analyzer behavior when implemented.

## Compatibility Levels

Ember can describe support in levels rather than all-or-nothing claims:

| Level | Meaning |
| --- | --- |
| Parsed | Source is accepted and represented in Ember data structures. |
| Compiled | Source compiles to Ember bytecode with tested instruction shapes. |
| Executed | Bytecode runs with tested runtime behavior. |
| Conformant | A named upstream conformance slice passes. |
| Embedded | The behavior is usable through a stable Go host interface. |

## Test Strategy

- Start with small handwritten fixtures for each runtime slice.
- Add differential tests against upstream Luau when practical.
- Import upstream conformance tests by category, not as a single giant gate.
- Keep failing or unsupported categories documented.
- Prefer bytecode fixtures for VM work before the compiler exists.

## Supported-feature manifest

The table below is the compatibility claim for the current root package. Each
row names at least one behavior test; `TestCompatibilityManifestReferencesExistingBehaviorTests`
fails if a referenced test is renamed or removed. The behavior tests themselves
remain the source of truth for semantics and expected values.

| ID | Feature | Level | Behavior test(s) |
| --- | --- | --- | --- |
| values | nil, booleans, numbers, and strings | Executed | `TestCompileAndRunScalarLiterals` |
| locals | local bindings, assignment, lexical `do` blocks | Executed | `TestCompileAndRunLocalBindings`, `TestCompileAndRunDoBlock` |
| expressions | arithmetic, concatenation, comparisons, logical operators, unary operators, and grouping | Executed | `TestCompileAndRunMultiplicationPrecedence`, `TestCompileAndRunStringConcatenation`, `TestCompileAndRunRelationalComparisons`, `TestCompileAndRunLogicalAndBindsTighterThanOr`, `TestCompileAndRunNotUsesLuauTruthiness`, `TestCompileAndRunParenthesizedExpression` |
| conditionals | `if` statements and expressions with `elseif`, `else`, and Luau truthiness | Executed | `TestCompileAndRunIfStatementSelectsElseIfBranch`, `TestCompileAndRunIfExpressionDoesNotEvaluateUnselectedBranch`, `TestCompileAndRunIfUsesLuauTruthiness` |
| loops | `while`, `repeat`/`until`, numeric `for`, generic `for`, `break`, and `continue` | Executed | `TestCompileAndRunWhileLoopMutatesOuterLocal`, `TestCompileAndRunRepeatUntilLoop`, `TestCompileAndRunNumericForLoopWithStep`, `TestCompileAndRunGenericForPairsLoop`, `TestCompileAndRunBreakExitsWhileLoop`, `TestCompileAndRunContinueSkipsGenericForLoopBody` |
| tables | array, named-field, and computed-key literals; field/index reads and writes | Executed | `TestCompileAndRunComputedKeyTableLiteralExpressionKey`, `TestCompileAndRunTableBracketAssignment`, `TestCompileAndRunNestedTableFieldAssignment` |
| metatables | table-valued and function-valued `__index`/`__newindex`, arithmetic, concat, comparison, equality, unary, length, call, iterator, tostring, and protected `__metatable` behavior | Executed | `TestCompileAndRunMetatableIndexTableFieldRead`, `TestCompileAndRunRuntimeTableAccessCallsFunctionValuedMetamethods`, `TestCompileAndRunMetatableNewIndexTableFieldAssignment`, `TestCompileAndRunMetatableNewIndexFunctionAssignment`, `TestCompileAndRunArithmeticOperatorsUseMetamethods`, `TestCompileAndRunRelationalOperatorsUseMetamethods`, `TestCompileAndRunEqualityOperatorsUseMetamethod`, `TestCompileAndRunUnaryMinusUsesMetamethod`, `TestCompileAndRunCallUsesMetamethod`, `TestCompileAndRunGenericForUsesIterMetamethod`, `TestCompileAndRunToStringUsesMetamethod`, `TestCompileAndRunRawLenBypassesLenMetamethod`, `TestRunRejectsReplacingProtectedMetatable` |
| functions | local and anonymous functions, closures, upvalues, field functions, and methods with receiver self-arguments | Executed | `TestCompileAndRunAnonymousFunctionCapturesOuterLocal`, `TestCompileAndRunReturnedClosureKeepsOuterLocal`, `TestCompileAndRunMethodCallPassesReceiver`, `TestCompileAndRunMethodFunctionDeclaration`, `TestCompileAndRunFieldFunctionDeclaration` |
| results | multiple returns and value-list adjustment for returns, assignments, and calls | Executed | `TestCompileAndRunMultipleReturnValues`, `TestCompileAndRunMultipleAssignmentAdjustsValues`, `TestCompileAndRunReturnListExpandsFinalCall` |
| variadics | variadic script functions and `...` value adjustment | Executed | `TestCompileAndRunVarargFunctionReturnsArguments`, `TestCompileAndRunFinalVarargArgumentExpandsResults` |
| host | host globals, host function calls, host table mutation, and callback-visible values | Embedded | `TestCompileAndRunHostFunctionCall`, `TestRunWithGlobalsMutatesHostTable`, `TestRuntimeCallbackCanBeStoredAndCalledAfterHook` |
| base-type | `type` and host-visible value kinds | Executed | `TestCompileAndRunBaseTypeFunction` |
| base-math | `math.abs`, `math.floor`, `math.min`, `math.max`, and `math.pi` | Executed | `TestCompileAndRunBaseMathAbs`, `TestCompileAndRunBaseMathFloor`, `TestCompileAndRunBaseMathMinAndMax`, `TestCompileAndRunBaseMathPi` |
| base-conversion | `tonumber`, `tostring`, and `select` | Executed | `TestCompileAndRunToNumberConvertsDecimalString`, `TestCompileAndRunToStringConvertsScalarValues`, `TestCompileAndRunSelectReturnsValuesFromPositiveIndex` |
| base-tables | `setmetatable`, `getmetatable`, raw table helpers, and `table.pack`/`table.unpack`/mutation helpers | Executed | `TestCompileAndRunGetMetatable`, `TestCompileAndRunSetMetatableNilClearsMetatable`, `TestCompileAndRunRawGetBypassesIndexMetamethod`, `TestCompileAndRunRawSetBypassesNewIndexMetamethod`, `TestCompileAndRunTablePackStoresVarargsAndCount`, `TestCompileAndRunTableUnpackExpandsExplicitRange`, `TestCompileAndRunTableInsertAppendsValue`, `TestCompileAndRunTableRemoveShiftsValues`, `TestCompileAndRunTableConcatJoinsArrayPrefix`, `TestCompileAndRunTableFindReturnsFirstMatchingIndex`, `TestCompileAndRunTableSortOrdersArrayPrefix`, `TestCompileAndRunTableClearRemovesAllEntries` |
| iteration | `next`, `pairs`, `ipairs`, and direct table generic iteration | Executed | `TestCompileAndRunGenericForPairsLoop`, `TestCompileAndRunGenericForIPairsLoop`, `TestCompileAndRunGenericForNextTableLoop`, `TestCompileAndRunGenericForDirectTableLoop` |
| coroutines | create, yield, resume, status, wrap, and close | Executed | `TestCompileAndRunCoroutineCreateResumeAndStatus`, `TestCompileAndRunCoroutineYieldAndResumeValues`, `TestCompileAndRunCoroutineCloseSuspendedCoroutine`, `TestCompileAndRunCoroutineWrapResumesAndReturnsValues` |
| modules | module loading, require caching, and entrypoint hooks through the host runtime | Embedded | `TestRuntimeRunHookSharesRequireCacheAcrossEntrypoints`, `TestRuntimeHostReceivesEntrypointLoadAndHookCalls` |
| typed-syntax | erased Luau annotations, aliases, generic function parameters, `typeof`, and casts | Parsed | `TestCompileAndRunLocalTypeAnnotationIsErased`, `TestCompileAndRunTypeAliasDeclarationsAreErased`, `TestCompileAndRunGenericFunctionTypeParametersAreErased`, `TestCompileAndRunTypeofTypeAnnotationIsErased`, `TestCompileAndRunTypeCastIsErased` |
| strict-check | `--!strict` checking foothold and typed diagnostics | Compiled | `TestCheckRequiresStrictDirective`, `TestCheckReturnsTypeDiagnosticAsError`, `TestCheckAcceptsStrictDirective` |
| protected-calls | `pcall` and `xpcall` capture ordinary script/host errors while limits and cancellation escape | Executed | `TestCompileAndRunProtectedCallCapturesRuntimeError`, `TestCompileAndRunProtectedXCallUsesErrorHandler`, `TestProtectedCallsPropagateCancellationAndEveryLimitKind` |
| typed-host-analysis | copy-owned host catalogs, source-attributed program diagnostics, dependency-first trusted summaries, class-like table returns, metatable inheritance, and singleton-dispatched overloads | Embedded | `TestLoadProgramAnalysisUsesHostAndTrustedDependencyFactsDeterministically`, `TestAnalyzerProjectsLiteralFactoryOverloadsAndTableReturns`, `TestHearthShapedEmbeddingContract` |
| cooperative-host-suspension | explicit resumable roots, required modules, hooks, and captured callbacks with opaque host tokens; initialize-once sharing, independent host-selected completion, deterministic dependent pumping, idempotent cancellation with retry, atomic busy admission, error injection, close invalidation, and VM/Machine parity | Embedded | `TestResumableHookSharesSuspendedEntrypointInitializationWithRequire`, `TestResumableHookCompletesIndependentEntrypointInitializersInHostOrder`, `TestResumableHookCompletesIndependentRequiredModulesInHostOrder`, `TestResumableEntrypointInitializerCanSuspendRepeatedly`, `TestFailedResumableModuleInitializerRetriesCleanly`, `TestSuspensionCancelAbortsSharedInitializerAndAllowsRetry`, `TestSuspensionAdmissionKeepsBusyLoserRetryable`, `TestCapturedCallbackCanSuspendAndResumeOnBothEngines`, `TestCapturedCallbackCanRequireSuspendedModuleOnBothEngines`, `TestHearthShapedEmbeddingContract` |

This manifest deliberately claims slices, not all of Luau. A feature belongs in
the table only when a concrete test exercises it through the public compile,
run, check, or embedding interface.

## Documented Ember Choices

- Raw table iteration through `next`, `pairs`, and direct table generic `for`
  uses deterministic insertion order. Luau does not guarantee a portable raw
  table order, so tests that depend on Ember's order are testing Ember's host
  contract rather than upstream ordering.

## Non-Goals For Early Ember

- Full native codegen.
- Full analyzer parity.
- C API compatibility.
- Automatic translation of upstream C++ files.

The following are also explicitly unsupported or outside the current claim:

- full Luau grammar and standard-library parity beyond the functions listed in
  the manifest;
- full analyzer/type-checker parity (the typed-syntax row is parsing/erasure,
  while strict checking remains a foothold);
- C API compatibility, native code generation, and JIT behavior;
- implicit host I/O, networking, filesystem, or platform services in the root
  runtime package;
- portability of raw table iteration order beyond Ember's documented insertion
  order contract.

These can become future projects, but they should not block the first useful
Go-native runtime.
