package ember

import (
	"fmt"
	"strings"
)

type simpleType string

const (
	simpleTypeUnknown        simpleType = ""
	simpleTypeAny            simpleType = "any"
	simpleTypeNil            simpleType = "nil"
	simpleTypeBoolean        simpleType = "boolean"
	simpleTypeNumber         simpleType = "number"
	simpleTypeString         simpleType = "string"
	simpleTypeCheckedUnknown simpleType = "unknown"
	simpleTypeNever          simpleType = "never"
	simpleTypeTable          simpleType = "table"
	simpleTypeFunction       simpleType = "function"
	simpleTypeThread         simpleType = "thread"
	simpleTypeUserData       simpleType = "userdata"
	simpleTypeBuffer         simpleType = "buffer"
	simpleTypeVector         simpleType = "vector"
)

func analyzeProgram(source Source, prog program, bind bindResult, mode SourceMode, env typeEnv, summaries moduleSummaryEnv) []Diagnostic {
	state := analysisState{
		source:          source,
		bind:            bind,
		mode:            mode,
		typeEnv:         env,
		moduleSummaries: summaries,
		symbolTypes:     make(map[int]simpleType),
		functions:       make(map[int]functionFact),
		scopes:          []map[string]simpleType{{}},
		tableScopes:     []map[string]tableFact{{}},
		aliasScopes:     []map[string]typeAliasFact{{}},
		moduleScopes:    []map[string]ModuleSummary{{}},
	}
	state.analyzeStatements(prog.statements)
	return state.diagnostics
}

type analysisState struct {
	diagnostics     []Diagnostic
	source          Source
	bind            bindResult
	mode            SourceMode
	typeEnv         typeEnv
	moduleSummaries moduleSummaryEnv
	bindCursor      int
	symbolTypes     map[int]simpleType
	functions       map[int]functionFact
	scopes          []map[string]simpleType
	tableScopes     []map[string]tableFact
	aliasScopes     []map[string]typeAliasFact
	moduleScopes    []map[string]ModuleSummary
	returns         []simpleType
	returnTables    []tableFact
	returnSpans     []sourceRange
	returnPacks     []returnPackFact
}

type tableFact struct {
	known     bool
	fields    map[string]simpleType
	access    map[string]string
	functions map[string]functionFact
	indexers  []tableIndexerFact
}

type tableIndexerFact struct {
	key    simpleType
	value  simpleType
	access string
}

type functionFact struct {
	typeParams      []string
	params          []simpleType
	paramGenerics   []string
	variadic        simpleType
	variadicGeneric string
	returnType      simpleType
	returnTable     tableFact
	returnSpan      sourceRange
	returnPack      returnPackFact
	returnGeneric   string
}

type returnPackFact struct {
	known  bool
	types  []simpleType
	tables []tableFact
	spans  []sourceRange
}

type typeAliasFact struct {
	typeParams []string
	typePacks  []string
	value      *typeExpression
}

type sourceRange struct {
	start int
	end   int
}

func annotationRange(annotation *typeExpression) sourceRange {
	if annotation == nil {
		return sourceRange{}
	}
	if annotation.kind == typeKindName && len(annotation.name) != 0 {
		return sourceRange{start: annotation.start, end: annotation.start + len(strings.Join(annotation.name, "."))}
	}
	return sourceRange{start: annotation.start, end: annotation.end}
}

type constraintKind string

const (
	constraintAssignable constraintKind = "assignable"
	constraintBinaryOp   constraintKind = "binary-op"
	constraintIndexRead  constraintKind = "index-read"
	constraintIndexWrite constraintKind = "index-write"
	constraintUnaryOp    constraintKind = "unary-op"
)

type typeConstraint struct {
	kind          constraintKind
	operator      string
	field         string
	expected      simpleType
	actual        simpleType
	expectedTable tableFact
	actualTable   tableFact
	evidence      constraintEvidence
}

type constraintEvidence struct {
	span sourceRange
}

func (a *analysisState) analyzeStatements(statements []statement) {
	for _, stmt := range statements {
		switch {
		case stmt.local != nil:
			a.analyzeLocalStatement(*stmt.local)
		case stmt.typeAlias != nil:
			a.analyzeTypeAliasStatement(*stmt.typeAlias)
		case stmt.assign != nil:
			a.analyzeAssignStatement(*stmt.assign)
		case stmt.localFunc != nil:
			a.analyzeLocalFunctionStatement(*stmt.localFunc)
		case stmt.funcDecl != nil:
			a.checkFunctionParameterTypeNames(stmt.funcDecl.paramAnnotations, stmt.funcDecl.variadicAnnotation)
			a.analyzeFunctionBody(stmt.funcDecl.returnAnnotation, stmt.funcDecl.statements)
		case stmt.call != nil:
			a.analyzeCallStatement(*stmt.call)
		case stmt.ret != nil:
			a.analyzeReturnStatement(*stmt.ret)
		case stmt.ifStmt != nil:
			a.analyzeIfStatement(*stmt.ifStmt)
		case stmt.while != nil:
			a.analyzeWhileStatement(*stmt.while)
		case stmt.forLoop != nil:
			a.analyzeNumericForStatement(*stmt.forLoop)
		case stmt.genericFor != nil:
			a.analyzeGenericForStatement(*stmt.genericFor)
		case stmt.repeat != nil:
			a.analyzeRepeatStatement(*stmt.repeat)
		case stmt.block != nil:
			a.analyzeScopedStatements(stmt.block.statements)
		}
	}
}

func (a *analysisState) analyzeCallStatement(stmt term) {
	if stmt.call == nil {
		return
	}
	call := *stmt.call
	if fact, ok := a.functionFactForCall(call); ok {
		a.checkCallArguments(fact, call)
	}
	a.applyAssertRefinement(call)
}

func (a *analysisState) applyAssertRefinement(call callExpression) {
	target := termWithoutCastsAndGroups(call.target)
	if target.name != "assert" || len(target.selectors) != 0 || len(call.args) == 0 {
		return
	}
	a.analyzeConditionExpression(call.args[0])
	refinements := a.trueConditionRefinements(call.args[0])
	if len(refinements) != 0 {
		a.applyTrueRefinements(refinements)
		return
	}
	value, ok := expressionSingleTerm(call.args[0])
	if !ok || value.name == "" || len(value.selectors) != 0 {
		return
	}
	narrowed := truthyType(a.lookupLocal(value.name))
	if narrowed == simpleTypeUnknown {
		return
	}
	a.defineLocal(value.name, narrowed)
}

func (a *analysisState) analyzeIfStatement(stmt ifStatement) {
	a.analyzeConditionExpression(stmt.condition)
	trueRefinements := a.trueConditionRefinements(stmt.condition)

	a.pushScope()
	a.applyTrueRefinements(trueRefinements)
	a.analyzeStatements(stmt.thenStatements)
	thenFrame := a.popScope()

	a.pushScope()
	a.applyFalseConditionRefinements(stmt.condition)
	a.analyzeStatements(stmt.elseStatements)
	elseFrame := a.popScope()

	a.applyBranchLocalFlowJoins(thenFrame.locals, elseFrame.locals)
	a.applyBranchTableFlowJoins(thenFrame.tables, elseFrame.tables)
}

func (a *analysisState) analyzeWhileStatement(stmt whileStatement) {
	a.analyzeConditionExpression(stmt.condition)
	a.pushScope()
	a.applyTrueRefinements(a.trueConditionRefinements(stmt.condition))
	a.analyzeStatements(stmt.statements)
	a.popScope()
}

func (a *analysisState) analyzeNumericForStatement(stmt forStatement) {
	a.checkNumericForBound(stmt.start)
	a.checkNumericForBound(stmt.limit)
	if stmt.step != nil {
		a.checkNumericForBound(*stmt.step)
	}
	a.pushScope()
	a.defineLocal(stmt.name, simpleTypeNumber)
	if symbol, ok := a.claimSymbol(stmt.name, symbolLocal); ok {
		a.symbolTypes[symbol.id] = simpleTypeNumber
	}
	a.analyzeStatements(stmt.statements)
	a.popScope()
}

func (a *analysisState) checkNumericForBound(expr expression) {
	actual := a.inferExpression(expr)
	constraint := typeConstraint{
		kind:     constraintAssignable,
		expected: simpleTypeNumber,
		actual:   actual,
		evidence: constraintEvidence{
			span: expressionRange(expr),
		},
	}
	a.checkConstraint(constraint)
}

func (a *analysisState) analyzeGenericForStatement(stmt genericForStatement) {
	for _, value := range stmt.values {
		a.inferExpression(value)
	}
	types := a.genericForValueTypes(stmt)
	a.pushScope()
	for i, name := range stmt.names {
		typ := simpleTypeUnknown
		if i < len(types) {
			typ = types[i]
		}
		a.defineLocal(name, typ)
		if symbol, ok := a.claimSymbol(name, symbolLocal); ok {
			a.symbolTypes[symbol.id] = typ
		}
	}
	a.analyzeStatements(stmt.statements)
	a.popScope()
}

func (a *analysisState) genericForValueTypes(stmt genericForStatement) []simpleType {
	if types := a.nextTableForValueTypes(stmt); types != nil {
		return types
	}
	if len(stmt.values) != 1 {
		return nil
	}
	if value, ok := expressionSingleTerm(stmt.values[0]); ok && value.name != "" && len(value.selectors) == 0 {
		if fact, ok := a.lookupTableFact(value.name); ok {
			return tableIterationTypes(fact)
		}
	}
	call, ok := expressionSingleCall(stmt.values[0])
	if !ok {
		return nil
	}
	target := termWithoutCastsAndGroups(call.target)
	if len(target.selectors) != 0 || len(call.args) != 1 {
		return nil
	}
	arg, ok := expressionSingleTerm(call.args[0])
	if !ok || arg.name == "" || len(arg.selectors) != 0 {
		return nil
	}
	fact, ok := a.lookupTableFact(arg.name)
	if !ok {
		return nil
	}
	keyType := simpleTypeUnknown
	switch target.name {
	case "ipairs":
		keyType = simpleTypeNumber
	case "pairs":
		return tableIterationTypes(fact)
	default:
		return nil
	}
	indexer, ok := tableIndexerForKeyType(fact, keyType)
	if !ok {
		return nil
	}
	return []simpleType{indexer.key, indexer.value}
}

func (a *analysisState) nextTableForValueTypes(stmt genericForStatement) []simpleType {
	if len(stmt.values) != 2 {
		return nil
	}
	iterator, ok := expressionSingleTerm(stmt.values[0])
	if !ok || iterator.name != "next" || len(iterator.selectors) != 0 {
		return nil
	}
	value, ok := expressionSingleTerm(stmt.values[1])
	if !ok || value.name == "" || len(value.selectors) != 0 {
		return nil
	}
	fact, ok := a.lookupTableFact(value.name)
	if !ok {
		return nil
	}
	return tableIterationTypes(fact)
}

func tableIterationTypes(fact tableFact) []simpleType {
	if indexer, ok := firstTableIndexer(fact); ok {
		return []simpleType{indexer.key, indexer.value}
	}
	if valueType := tableFieldValueUnion(fact); valueType != simpleTypeUnknown {
		return []simpleType{simpleTypeString, valueType}
	}
	return nil
}

func firstTableIndexer(fact tableFact) (tableIndexerFact, bool) {
	if len(fact.indexers) == 0 {
		return tableIndexerFact{}, false
	}
	return fact.indexers[0], true
}

func tableFieldValueUnion(fact tableFact) simpleType {
	typ := simpleTypeUnknown
	for _, fieldType := range fact.fields {
		typ = unionSimpleTypes(typ, fieldType)
	}
	return typ
}

func (a *analysisState) analyzeRepeatStatement(stmt repeatStatement) {
	a.pushScope()
	a.analyzeStatements(stmt.statements)
	a.analyzeConditionExpression(stmt.condition)
	refinements := a.trueConditionRefinements(stmt.condition)
	a.popScope()
	a.applyTrueRefinementsToExisting(refinements)
}

func (a *analysisState) analyzeConditionExpression(expr expression) {
	for _, term := range expr.terms {
		for _, comparison := range term.terms {
			a.inferComparisonExpression(comparison)
		}
	}
}

func (a *analysisState) analyzeTypeAliasStatement(stmt typeAliasStatement) {
	if _, ok := a.claimSymbol(stmt.name, symbolTypeAlias); ok {
		a.defineTypeAlias(stmt)
	}
	a.checkUnknownTypeNames(stmt.value)
}

func (a *analysisState) analyzeScopedStatements(statements []statement) {
	a.pushScope()
	a.analyzeStatements(statements)
	a.popScope()
}

func (a *analysisState) analyzeLocalFunctionStatement(stmt localFunctionStatement) {
	a.checkFunctionParameterTypeNames(stmt.paramAnnotations, stmt.variadicAnnotation)
	paramTypes := a.simpleTypesFromAnnotations(stmt.paramAnnotations)
	returnPack := a.returnPackFromAnnotation(stmt.returnAnnotation)
	fact := functionFact{
		typeParams:      append([]string(nil), stmt.typeParams...),
		params:          paramTypes,
		paramGenerics:   genericAnnotationNames(stmt.paramAnnotations, stmt.typeParams),
		variadic:        a.simpleTypeFromAnnotation(stmt.variadicAnnotation),
		variadicGeneric: genericAnnotationName(stmt.variadicAnnotation, stmt.typeParams),
		returnType:      returnPack.firstType(a.simpleTypeFromAnnotation(stmt.returnAnnotation)),
		returnTable:     returnPack.firstTable(a.tableFactFromAnnotation(stmt.returnAnnotation)),
		returnSpan:      annotationRange(stmt.returnAnnotation),
		returnPack:      returnPack,
		returnGeneric:   genericAnnotationName(stmt.returnAnnotation, stmt.typeParams),
	}
	if symbol, ok := a.claimSymbol(stmt.name, symbolLocalFunction); ok {
		a.functions[symbol.id] = fact
	}
	restore := a.bindLocals(stmt.params, paramTypes)
	a.analyzeFunctionBody(stmt.returnAnnotation, stmt.statements)
	restore()
}

func (a *analysisState) analyzeFunctionBody(returnAnnotation *typeExpression, statements []statement) {
	a.checkUnknownTypeNames(returnAnnotation)
	returnPack := a.returnPackFromAnnotation(returnAnnotation)
	a.analyzeFunctionBodyWithReturn(
		returnPack.firstType(a.simpleTypeFromAnnotation(returnAnnotation)),
		returnPack.firstTable(a.tableFactFromAnnotation(returnAnnotation)),
		annotationRange(returnAnnotation),
		returnPack,
		statements,
	)
}

func (a *analysisState) analyzeFunctionBodyWithReturn(returnType simpleType, returnTable tableFact, returnSpan sourceRange, returnPack returnPackFact, statements []statement) {
	a.returns = append(a.returns, returnType)
	a.returnTables = append(a.returnTables, returnTable)
	a.returnSpans = append(a.returnSpans, returnSpan)
	a.returnPacks = append(a.returnPacks, returnPack)
	a.analyzeStatements(statements)
	a.checkImplicitNilReturn(statements)
	a.returns = a.returns[:len(a.returns)-1]
	a.returnTables = a.returnTables[:len(a.returnTables)-1]
	a.returnSpans = a.returnSpans[:len(a.returnSpans)-1]
	a.returnPacks = a.returnPacks[:len(a.returnPacks)-1]
}

func (a *analysisState) bindLocals(names []string, types []simpleType) func() {
	previous := make(map[string]simpleType, len(names))
	hadPrevious := make(map[string]bool, len(names))
	for i, name := range names {
		previous[name], hadPrevious[name] = a.currentScope()[name]
		if i < len(types) {
			typ := types[i]
			a.currentScope()[name] = typ
			if symbol, ok := a.claimSymbol(name, symbolParameter); ok {
				a.symbolTypes[symbol.id] = typ
			}
		}
	}
	return func() {
		for _, name := range names {
			if hadPrevious[name] {
				a.currentScope()[name] = previous[name]
			} else {
				delete(a.currentScope(), name)
			}
		}
	}
}

func (a *analysisState) analyzeLocalStatement(stmt localStatement) {
	for i, name := range stmt.names {
		var expected simpleType
		var annotation *typeExpression
		expectedTable := tableFact{}
		actualTable := tableFact{}
		var functionFact functionFact
		hasFunctionFact := false
		var moduleSummary ModuleSummary
		hasModuleSummary := false
		if i < len(stmt.annotations) {
			annotation = stmt.annotations[i]
			a.checkUnknownTypeNames(annotation)
			expected = a.simpleTypeFromAnnotation(annotation)
			expectedTable = a.tableFactFromAnnotation(annotation)
			functionFact, hasFunctionFact = a.functionFactFromAnnotation(annotation)
		}
		actual := simpleTypeUnknown
		if i < len(stmt.values) {
			moduleSummary, hasModuleSummary = a.moduleSummaryFromExpression(stmt.values[i])
			if hasModuleSummary {
				if moduleFact, ok := moduleReturnFact(moduleSummary); ok {
					actual = moduleFact.typ
					actualTable = moduleFact.table
					if moduleFact.hasFunction {
						functionFact = moduleFact.function
						hasFunctionFact = true
					}
				}
			} else {
				actual = a.inferExpression(stmt.values[i])
				if !expressionIsTableLiteral(stmt.values[i]) {
					actualTable = a.tableFactFromExpression(stmt.values[i])
				}
			}
			if !hasFunctionFact {
				a.analyzeFunctionExpressionAnnotations(stmt.values[i])
			}
			a.analyzeAnnotatedExpression(annotation, stmt.values[i])
		}
		selected := selectedLocalType(expected, actual)
		a.defineLocal(name, selected)
		if hasModuleSummary {
			a.defineModuleLocal(name, moduleSummary)
		}
		if symbol, ok := a.claimSymbol(name, symbolLocal); ok {
			a.symbolTypes[symbol.id] = selected
			if !hasFunctionFact && i < len(stmt.values) {
				functionFact, hasFunctionFact = a.functionFactFromExpression(stmt.values[i])
			}
			if hasFunctionFact {
				a.functions[symbol.id] = functionFact
			}
		}
		tableFact := expectedTable
		if tableFact.empty() && !actualTable.empty() {
			tableFact = actualTable
		}
		if tableFact.empty() && i < len(stmt.values) {
			tableFact = a.tableFactFromLiteral(stmt.values[i])
		}
		a.defineTableLocal(name, tableFact)
		if expected == simpleTypeUnknown {
			continue
		}
		if i >= len(stmt.values) {
			constraint := typeConstraint{
				kind:     constraintAssignable,
				expected: expected,
				actual:   simpleTypeNil,
				evidence: constraintEvidence{
					span: localNameRange(stmt, i),
				},
			}
			a.checkConstraint(constraint)
			continue
		}
		constraint := typeConstraint{
			kind:          constraintAssignable,
			expected:      expected,
			actual:        actual,
			expectedTable: expectedTable,
			actualTable:   actualTable,
			evidence: constraintEvidence{
				span: expressionRange(stmt.values[i]),
			},
		}
		a.checkConstraint(constraint)
	}
}

func (a *analysisState) moduleReturnFactFromExpression(expr expression) (simpleType, tableFact, bool) {
	summary, ok := a.moduleSummaryFromExpression(expr)
	if !ok {
		return simpleTypeUnknown, tableFact{}, false
	}
	fact, ok := moduleReturnFact(summary)
	if !ok {
		return simpleTypeUnknown, tableFact{}, false
	}
	return fact.typ, fact.table, true
}

func (a *analysisState) moduleSummaryFromExpression(expr expression) (ModuleSummary, bool) {
	call, ok := expressionSingleCall(expr)
	if !ok {
		return ModuleSummary{}, false
	}
	request, ok := requireCallRequest(call)
	if !ok {
		return ModuleSummary{}, false
	}
	summary, ok := a.moduleSummaries.summaryForRequire(a.source, request)
	if !ok && a.moduleSummaries.active() {
		span := expressionRange(call.args[0])
		a.diagnostics = append(a.diagnostics, missingModuleSummaryDiagnostic(request, span.start, span.end))
	}
	if ok {
		if dependency, stale := a.moduleSummaries.staleDependency(summary); stale {
			span := expressionRange(call.args[0])
			a.diagnostics = append(a.diagnostics, staleModuleSummaryDiagnostic(summary, dependency, span.start, span.end))
		}
	}
	return summary, ok
}

func moduleReturnFact(summary ModuleSummary) (globalTypeFact, bool) {
	exported, ok := moduleExportByNameKind(summary.Exports, "return", ModuleExportValue)
	if !ok {
		return globalTypeFact{}, false
	}
	return globalFactFromSummary(exported.Type), true
}

func (a *analysisState) tableFactFromLiteral(value expression) tableFact {
	tableTerm, ok := expressionSingleTerm(value)
	if !ok || tableTerm.table == nil {
		return tableFact{}
	}
	fields := make(map[string]simpleType)
	var indexers []tableIndexerFact
	for _, field := range tableTerm.table.fields {
		if field.name == "" && field.key == nil && field.arrayIndex > 0 {
			typ := a.inferExpression(field.value)
			if typ != simpleTypeUnknown {
				indexers = mergeTableIndexerFact(indexers, simpleTypeNumber, typ)
			}
			continue
		}
		if field.name == "" && field.key != nil {
			key := a.inferExpression(*field.key)
			typ := a.inferExpression(field.value)
			if key != simpleTypeUnknown && typ != simpleTypeUnknown {
				indexers = mergeTableIndexerFact(indexers, key, typ)
			}
			continue
		}
		if field.name == "" {
			continue
		}
		typ := a.inferExpression(field.value)
		if typ == simpleTypeUnknown {
			continue
		}
		fields[field.name] = typ
	}
	return tableFact{known: true, fields: fields, indexers: indexers}
}

func mergeTableIndexerFact(indexers []tableIndexerFact, key, value simpleType) []tableIndexerFact {
	for i := range indexers {
		if indexers[i].key == key {
			indexers[i].value = unionSimpleTypes(indexers[i].value, value)
			return indexers
		}
	}
	return append(indexers, tableIndexerFact{key: key, value: value})
}

func (a *analysisState) tableFactFromExpression(value expression) tableFact {
	valueTerm, ok := expressionSingleTerm(value)
	if !ok {
		return tableFact{}
	}
	if valueTerm.table != nil {
		return a.tableFactFromLiteral(value)
	}
	if valueTerm.call != nil {
		fact, ok := a.functionFactForCallQuiet(*valueTerm.call)
		if !ok {
			return tableFact{}
		}
		return fact.returnTable
	}
	if valueTerm.name == "" || len(valueTerm.selectors) != 0 {
		return tableFact{}
	}
	fact, ok := a.lookupTableFact(valueTerm.name)
	if !ok {
		return tableFact{}
	}
	return fact
}

func expressionIsTableLiteral(value expression) bool {
	valueTerm, ok := expressionSingleTerm(value)
	return ok && valueTerm.table != nil
}

func localNameRange(stmt localStatement, index int) sourceRange {
	if index >= 0 && index < len(stmt.nameRanges) {
		return stmt.nameRanges[index]
	}
	return sourceRange{}
}

func (a *analysisState) analyzeAnnotatedExpression(annotation *typeExpression, value expression) {
	a.analyzeAnnotatedFunctionExpression(annotation, value)
	expectedFact := a.tableFactFromAnnotation(annotation)
	if expectedFact.empty() {
		return
	}
	tableTerm, ok := expressionSingleTerm(value)
	if !ok || tableTerm.table == nil {
		return
	}
	expectedFields := expectedFact.fields
	actualFact := a.tableFactFromLiteral(value)
	presentFields := make(map[string]bool, len(tableTerm.table.fields))
	for _, field := range tableTerm.table.fields {
		if field.name != "" {
			presentFields[field.name] = true
		}
		if field.name == "" && field.key == nil && field.arrayIndex > 0 {
			indexer, ok := tableIndexerForKeyType(expectedFact, simpleTypeNumber)
			if !ok || indexer.value == simpleTypeUnknown {
				continue
			}
			actual := a.inferExpression(field.value)
			constraint := typeConstraint{
				kind:     constraintAssignable,
				expected: indexer.value,
				actual:   actual,
				evidence: constraintEvidence{
					span: expressionRange(field.value),
				},
			}
			a.checkConstraint(constraint)
			continue
		}
		if field.name == "" && field.key != nil {
			key := a.inferExpression(*field.key)
			indexer, ok := a.tableIndexerForKey(expectedFact, key, expressionRange(*field.key))
			if !ok || indexer.value == simpleTypeUnknown {
				continue
			}
			actual := a.inferExpression(field.value)
			constraint := typeConstraint{
				kind:     constraintAssignable,
				expected: indexer.value,
				actual:   actual,
				evidence: constraintEvidence{
					span: expressionRange(field.value),
				},
			}
			a.checkConstraint(constraint)
			continue
		}
		expected := expectedFields[field.name]
		if field.name != "" && expected == simpleTypeUnknown {
			if indexer, ok := tableIndexerForKeyType(expectedFact, simpleTypeString); ok {
				expected = indexer.value
			}
		}
		if field.name == "" || expected == simpleTypeUnknown {
			continue
		}
		actual := a.inferExpression(field.value)
		constraint := typeConstraint{
			kind:     constraintAssignable,
			expected: expected,
			actual:   actual,
			evidence: constraintEvidence{
				span: expressionRange(field.value),
			},
		}
		if a.checkConstraint(constraint) {
			continue
		}
	}
	for field, expected := range expectedFact.fields {
		if presentFields[field] {
			continue
		}
		if expected == simpleTypeUnknown {
			continue
		}
		if typeAllows(expected, simpleTypeNil) {
			continue
		}
		if actualIndexer, ok := tableIndexerForKeyType(actualFact, simpleTypeString); ok {
			if actualIndexer.value == simpleTypeUnknown || typeAllows(expected, actualIndexer.value) {
				continue
			}
			span := expressionRange(value)
			a.diagnostics = append(a.diagnostics, typeMismatchDiagnostic(expected, actualIndexer.value, span.start, span.end))
			continue
		}
		span := expressionRange(value)
		a.diagnostics = append(a.diagnostics, missingPropertyDiagnostic(field, span.start, span.end))
	}
}

func (a *analysisState) analyzeAnnotatedFunctionExpression(annotation *typeExpression, value expression) {
	fact, ok := a.functionFactFromAnnotation(annotation)
	if !ok {
		return
	}
	functionTerm, ok := expressionSingleTerm(value)
	if !ok || functionTerm.function == nil {
		return
	}
	function := *functionTerm.function
	restore := a.bindLocals(function.params, functionExpressionParamTypes(function, fact))
	a.analyzeFunctionBodyWithReturn(fact.returnType, fact.returnTable, fact.returnSpan, fact.returnPack, function.statements)
	restore()
}

func (a *analysisState) analyzeFunctionExpressionAnnotations(value expression) {
	function, ok := functionExpressionFromExpression(value)
	if !ok || !functionExpressionHasAnnotations(function) {
		return
	}
	a.checkFunctionParameterTypeNames(function.paramAnnotations, function.variadicAnnotation)
	a.checkUnknownTypeNames(function.returnAnnotation)
	fact := a.functionFactFromFunctionExpression(function)
	restore := a.bindLocals(function.params, functionExpressionParamTypes(function, fact))
	a.analyzeFunctionBodyWithReturn(fact.returnType, fact.returnTable, fact.returnSpan, fact.returnPack, function.statements)
	restore()
}

func (a *analysisState) functionFactFromExpression(value expression) (functionFact, bool) {
	function, ok := functionExpressionFromExpression(value)
	if !ok {
		return functionFact{}, false
	}
	return a.functionFactFromFunctionExpression(function), true
}

func functionExpressionFromExpression(value expression) (functionExpression, bool) {
	functionTerm, ok := expressionSingleTerm(value)
	if !ok || functionTerm.function == nil {
		return functionExpression{}, false
	}
	return *functionTerm.function, true
}

func functionExpressionHasAnnotations(function functionExpression) bool {
	if function.returnAnnotation != nil || function.variadicAnnotation != nil {
		return true
	}
	for _, annotation := range function.paramAnnotations {
		if annotation != nil {
			return true
		}
	}
	return false
}

func (a *analysisState) functionFactFromFunctionExpression(function functionExpression) functionFact {
	returnPack := a.returnPackFromAnnotation(function.returnAnnotation)
	return functionFact{
		typeParams:      append([]string(nil), function.typeParams...),
		params:          a.simpleTypesFromAnnotations(function.paramAnnotations),
		paramGenerics:   genericAnnotationNames(function.paramAnnotations, function.typeParams),
		variadic:        a.simpleTypeFromAnnotation(function.variadicAnnotation),
		variadicGeneric: genericAnnotationName(function.variadicAnnotation, function.typeParams),
		returnType:      returnPack.firstType(a.simpleTypeFromAnnotation(function.returnAnnotation)),
		returnTable:     returnPack.firstTable(a.tableFactFromAnnotation(function.returnAnnotation)),
		returnSpan:      annotationRange(function.returnAnnotation),
		returnPack:      returnPack,
		returnGeneric:   genericAnnotationName(function.returnAnnotation, function.typeParams),
	}
}

func functionExpressionParamTypes(function functionExpression, fact functionFact) []simpleType {
	types := make([]simpleType, len(function.params))
	for i := range function.params {
		if i < len(fact.params) {
			types[i] = fact.params[i]
			continue
		}
		types[i] = fact.variadic
	}
	return types
}

func (a *analysisState) returnPackFromAnnotation(annotation *typeExpression) returnPackFact {
	return a.returnPackFromAnnotationWith(annotation, nil)
}

func (a *analysisState) returnPackFromAnnotationWith(annotation *typeExpression, substitutions map[string]simpleType) returnPackFact {
	if annotation == nil {
		return returnPackFact{}
	}
	if annotation.kind == typeKindFunction && annotation.returnType == nil {
		pack := returnPackFact{
			known:  true,
			types:  make([]simpleType, 0, len(annotation.params)),
			tables: make([]tableFact, 0, len(annotation.params)),
			spans:  make([]sourceRange, 0, len(annotation.params)),
		}
		for _, param := range annotation.params {
			value := param.value
			if value != nil && value.kind == typeKindVariadic {
				value = value.inner
			}
			pack.types = append(pack.types, a.simpleTypeFromAnnotationWith(value, substitutions))
			pack.tables = append(pack.tables, a.tableFactFromAnnotation(value))
			pack.spans = append(pack.spans, annotationRange(value))
		}
		return pack
	}
	return returnPackFact{
		known:  true,
		types:  []simpleType{a.simpleTypeFromAnnotationWith(annotation, substitutions)},
		tables: []tableFact{a.tableFactFromAnnotation(annotation)},
		spans:  []sourceRange{annotationRange(annotation)},
	}
}

func (p returnPackFact) firstType(fallback simpleType) simpleType {
	if !p.known {
		return fallback
	}
	if len(p.types) == 0 {
		return simpleTypeNil
	}
	return p.types[0]
}

func (p returnPackFact) firstTable(fallback tableFact) tableFact {
	if !p.known || len(p.tables) == 0 {
		return fallback
	}
	return p.tables[0]
}

func (a *analysisState) analyzeReturnStatement(stmt returnStatement) {
	actual := simpleTypeNil
	actualTable := tableFact{}
	span := sourceRange{start: stmt.start, end: stmt.end}
	if len(stmt.values) != 0 {
		actual = a.inferExpression(stmt.values[0])
		actualTable = a.tableFactFromExpression(stmt.values[0])
		span = expressionRange(stmt.values[0])
	}
	if len(a.returns) == 0 {
		return
	}
	expected := a.returns[len(a.returns)-1]
	expectedTable := a.returnTables[len(a.returnTables)-1]
	if expected == simpleTypeUnknown {
		return
	}
	constraint := typeConstraint{
		kind:          constraintAssignable,
		expected:      expected,
		actual:        actual,
		expectedTable: expectedTable,
		actualTable:   actualTable,
		evidence: constraintEvidence{
			span: span,
		},
	}
	a.checkConstraint(constraint)
	a.checkAdditionalReturnPackValues(stmt)
}

func (a *analysisState) checkAdditionalReturnPackValues(stmt returnStatement) {
	if len(a.returnPacks) == 0 {
		return
	}
	expectedPack := a.returnPacks[len(a.returnPacks)-1]
	if !expectedPack.known || len(expectedPack.types) <= 1 {
		return
	}
	for i := 1; i < len(expectedPack.types); i++ {
		expected := expectedPack.types[i]
		if expected == simpleTypeUnknown {
			continue
		}
		actual := simpleTypeNil
		span := sourceRange{start: stmt.start, end: stmt.end}
		if i < len(stmt.values) {
			actual = a.inferExpression(stmt.values[i])
			span = expressionRange(stmt.values[i])
		}
		constraint := typeConstraint{
			kind:     constraintAssignable,
			expected: expected,
			actual:   actual,
			evidence: constraintEvidence{
				span: span,
			},
		}
		a.checkConstraint(constraint)
	}
}

func (a *analysisState) checkImplicitNilReturn(statements []statement) {
	if len(a.returns) == 0 || statementsDefinitelyReturn(statements) {
		return
	}
	expected := a.returns[len(a.returns)-1]
	if expected == simpleTypeUnknown {
		return
	}
	constraint := typeConstraint{
		kind:     constraintAssignable,
		expected: expected,
		actual:   simpleTypeNil,
		evidence: constraintEvidence{
			span: a.returnSpans[len(a.returnSpans)-1],
		},
	}
	a.checkConstraint(constraint)
}

func statementsDefinitelyReturn(statements []statement) bool {
	for _, stmt := range statements {
		if statementDefinitelyReturns(stmt) {
			return true
		}
	}
	return false
}

func statementDefinitelyReturns(stmt statement) bool {
	switch {
	case stmt.ret != nil:
		return true
	case stmt.ifStmt != nil:
		return statementsDefinitelyReturn(stmt.ifStmt.thenStatements) &&
			statementsDefinitelyReturn(stmt.ifStmt.elseStatements)
	case stmt.block != nil:
		return statementsDefinitelyReturn(stmt.block.statements)
	case stmt.repeat != nil:
		return statementsDefinitelyReturn(stmt.repeat.statements)
	default:
		return false
	}
}

func (a *analysisState) analyzeAssignStatement(stmt assignStatement) {
	for i, target := range stmt.targets {
		if i >= len(stmt.values) {
			a.analyzeMissingAssignValue(target)
			continue
		}
		if len(target.selectors) != 0 {
			a.analyzeFieldAssign(target, stmt.values[i])
			continue
		}
		expected := a.lookupAssignTarget(target)
		if expected == simpleTypeUnknown {
			continue
		}
		actual := a.inferExpression(stmt.values[i])
		expectedTable := a.tableFactFromAssignTarget(target)
		actualTable := a.tableFactFromExpression(stmt.values[i])
		constraint := typeConstraint{
			kind:          constraintAssignable,
			expected:      expected,
			actual:        actual,
			expectedTable: expectedTable,
			actualTable:   actualTable,
			evidence: constraintEvidence{
				span: expressionRange(stmt.values[i]),
			},
		}
		if a.checkConstraint(constraint) {
			a.applyAssignmentRefinement(target.name, actual)
			continue
		}
	}
}

func (a *analysisState) applyAssignmentRefinement(name string, typ simpleType) {
	if typ == simpleTypeUnknown {
		return
	}
	a.defineLocal(name, typ)
}

func (a *analysisState) analyzeMissingAssignValue(target assignTarget) {
	if len(target.selectors) != 0 {
		return
	}
	expected := a.lookupAssignTarget(target)
	if expected == simpleTypeUnknown {
		return
	}
	constraint := typeConstraint{
		kind:     constraintAssignable,
		expected: expected,
		actual:   simpleTypeNil,
		evidence: constraintEvidence{
			span: sourceRange{start: target.start, end: target.end},
		},
	}
	a.checkConstraint(constraint)
}

func (a *analysisState) analyzeFieldAssign(target assignTarget, value expression) {
	if target.name == "" || len(target.selectors) != 1 {
		return
	}
	selector := target.selectors[0]
	fact, ok := a.lookupTableFact(target.name)
	if !ok {
		return
	}
	if selector.index != nil {
		key := a.inferExpression(*selector.index)
		indexer, ok := a.tableIndexerForKey(fact, key, expressionRange(*selector.index))
		if !ok || indexer.value == simpleTypeUnknown {
			return
		}
		if indexer.access == "read" {
			a.diagnostics = append(a.diagnostics, readonlyPropertyDiagnostic(
				"indexer",
				target.start,
				target.end,
			))
			return
		}
		actual := a.inferExpression(value)
		constraint := typeConstraint{
			kind:     constraintIndexWrite,
			expected: indexer.value,
			actual:   actual,
			evidence: constraintEvidence{
				span: expressionRange(value),
			},
		}
		a.checkConstraint(constraint)
		return
	}
	if selector.field == "" {
		return
	}
	expected, exists := fact.fields[selector.field]
	if !exists {
		if indexer, ok := tableIndexerForKeyType(fact, simpleTypeString); ok {
			if indexer.access == "read" {
				a.diagnostics = append(a.diagnostics, readonlyPropertyDiagnostic(
					"indexer",
					target.start,
					target.end,
				))
				return
			}
			actual := a.inferExpression(value)
			constraint := typeConstraint{
				kind:     constraintIndexWrite,
				expected: indexer.value,
				actual:   actual,
				evidence: constraintEvidence{
					span: expressionRange(value),
				},
			}
			a.checkConstraint(constraint)
			return
		}
		constraint := typeConstraint{
			kind:  constraintIndexWrite,
			field: selector.field,
			evidence: constraintEvidence{
				span: sourceRange{start: target.start, end: target.end},
			},
		}
		a.checkConstraint(constraint)
		return
	}
	if expected == simpleTypeUnknown {
		return
	}
	if fact.access[selector.field] == "read" {
		a.diagnostics = append(a.diagnostics, readonlyPropertyDiagnostic(
			selector.field,
			target.start,
			target.end,
		))
		return
	}
	actual := a.inferExpression(value)
	constraint := typeConstraint{
		kind:     constraintIndexWrite,
		expected: expected,
		actual:   actual,
		evidence: constraintEvidence{
			span: expressionRange(value),
		},
	}
	if a.checkConstraint(constraint) {
		a.applyTableFieldAssignmentRefinement(target.name, selector.field, actual)
	}
}

func (a *analysisState) applyTableFieldAssignmentRefinement(name string, field string, typ simpleType) {
	if typ == simpleTypeUnknown {
		return
	}
	a.defineTableField(name, field, typ)
}

func (a *analysisState) lookupAssignTarget(target assignTarget) simpleType {
	if use, ok := a.bind.useAt(target.start, target.end); ok {
		if typ, ok := a.symbolTypes[use.symbol]; ok {
			return typ
		}
	}
	return a.lookupLocal(target.name)
}

func (a *analysisState) tableFactFromAssignTarget(target assignTarget) tableFact {
	if target.name == "" || len(target.selectors) != 0 {
		return tableFact{}
	}
	fact, ok := a.lookupTableFact(target.name)
	if !ok {
		return tableFact{}
	}
	return fact
}

func (a *analysisState) lookupNamedTerm(value term) simpleType {
	return a.lookupBoundName(value.name, value.start, value.start+len(value.name))
}

func (a *analysisState) checkUnknownName(name string, start int, end int) {
	if !policyForMode(a.mode).reportsUnknownNames() || name == "" || a.isKnownGlobalName(name) {
		return
	}
	if _, ok := a.bind.useAt(start, end); ok {
		return
	}
	a.diagnostics = append(a.diagnostics, unknownNameDiagnostic(name, start, end))
}

func (a *analysisState) isKnownGlobalName(name string) bool {
	_, ok := a.typeEnv.lookup(name)
	return ok
}

func (a *analysisState) lookupBoundName(name string, start int, end int) simpleType {
	if use, ok := a.bind.useAt(start, end); ok {
		if typ, ok := a.symbolTypes[use.symbol]; ok {
			local := a.lookupLocal(name)
			if local != simpleTypeUnknown && typeAllows(typ, local) {
				return local
			}
			return typ
		}
	}
	if a.hasLocal(name) {
		return a.lookupLocal(name)
	}
	if fact, ok := a.typeEnv.lookup(name); ok {
		return fact.typ
	}
	return simpleTypeUnknown
}

func (a *analysisState) claimSymbol(name string, kind symbolKind) (boundSymbol, bool) {
	for a.bindCursor < len(a.bind.symbols) {
		symbol := a.bind.symbols[a.bindCursor]
		a.bindCursor++
		if symbol.name == name && symbol.kind == kind {
			return symbol, true
		}
	}
	return boundSymbol{}, false
}

func selectedLocalType(annotation, value simpleType) simpleType {
	if annotation != simpleTypeUnknown {
		return annotation
	}
	return value
}

type conditionRefinement struct {
	name      string
	field     string
	trueType  simpleType
	falseType simpleType
}

func (a *analysisState) trueConditionRefinements(expr expression) []conditionRefinement {
	if len(expr.terms) != 1 {
		return a.orTrueConditionRefinements(expr)
	}
	var refinements []conditionRefinement
	for _, term := range expr.terms[0].terms {
		single := expression{terms: []andExpression{{terms: []comparisonExpression{term}}}}
		refinement := a.conditionRefinement(single)
		if refinement.name == "" || refinement.trueType == simpleTypeUnknown {
			continue
		}
		refinements = append(refinements, refinement)
	}
	return refinements
}

func (a *analysisState) orTrueConditionRefinements(expr expression) []conditionRefinement {
	var name string
	var field string
	typ := simpleTypeUnknown
	for _, term := range expr.terms {
		if len(term.terms) != 1 {
			return nil
		}
		single := expression{terms: []andExpression{{terms: []comparisonExpression{term.terms[0]}}}}
		refinement := a.conditionRefinement(single)
		if refinement.name == "" || refinement.trueType == simpleTypeUnknown {
			return nil
		}
		if name == "" {
			name = refinement.name
			field = refinement.field
		} else if name != refinement.name || field != refinement.field {
			return nil
		}
		typ = unionSimpleTypes(typ, refinement.trueType)
	}
	if name == "" || typ == simpleTypeUnknown {
		return nil
	}
	return []conditionRefinement{{
		name:     name,
		field:    field,
		trueType: typ,
	}}
}

func (a *analysisState) applyTrueRefinements(refinements []conditionRefinement) {
	for _, refinement := range refinements {
		if refinement.name == "" || refinement.trueType == simpleTypeUnknown {
			continue
		}
		a.applyRefinement(refinement.name, refinement.field, refinement.trueType)
	}
}

func (a *analysisState) applyTrueRefinementsToExisting(refinements []conditionRefinement) {
	for _, refinement := range refinements {
		if refinement.name == "" || refinement.trueType == simpleTypeUnknown {
			continue
		}
		if refinement.field == "" && !a.hasLocal(refinement.name) {
			continue
		}
		if refinement.field != "" && !a.hasTableFact(refinement.name) {
			continue
		}
		a.applyRefinement(refinement.name, refinement.field, refinement.trueType)
	}
}

func (a *analysisState) applyFalseConditionRefinements(expr expression) {
	if len(expr.terms) == 0 {
		return
	}
	if len(expr.terms) == 1 {
		refinement := a.conditionRefinement(expr)
		if refinement.name != "" && refinement.falseType != simpleTypeUnknown {
			a.applyRefinement(refinement.name, refinement.field, refinement.falseType)
		}
		return
	}
	for _, term := range expr.terms {
		if len(term.terms) != 1 {
			return
		}
		single := expression{terms: []andExpression{{terms: []comparisonExpression{term.terms[0]}}}}
		refinement := a.conditionRefinement(single)
		if refinement.name == "" || refinement.falseType == simpleTypeUnknown {
			return
		}
		a.applyRefinement(refinement.name, refinement.field, refinement.falseType)
	}
}

func (a *analysisState) applyRefinement(name string, field string, typ simpleType) {
	if field == "" {
		a.defineLocal(name, typ)
		return
	}
	a.defineTableField(name, field, typ)
}

func (a *analysisState) conditionRefinement(expr expression) conditionRefinement {
	if refinement, ok := a.nilComparisonRefinement(expr); ok {
		return refinement
	}
	if refinement, ok := a.singletonComparisonRefinement(expr); ok {
		return refinement
	}
	if refinement, ok := a.typeGuardRefinement(expr); ok {
		return refinement
	}
	value, ok := expressionSingleTerm(expr)
	if !ok || value.name == "" {
		if ok && value.unaryNot != nil && len(value.selectors) == 0 {
			return a.negatedConditionRefinement(*value.unaryNot)
		}
		return conditionRefinement{}
	}
	field := refinementField(value)
	if field == "" && len(value.selectors) != 0 {
		return conditionRefinement{}
	}
	current := a.lookupLocal(value.name)
	if field != "" {
		current = a.lookupTableField(value.name, field)
	}
	narrowed := truthyType(current)
	if narrowed == simpleTypeUnknown {
		return conditionRefinement{}
	}
	return conditionRefinement{
		name:      value.name,
		field:     field,
		trueType:  narrowed,
		falseType: falseyType(current),
	}
}

func refinementField(value term) string {
	field, ok := stableFieldSelector(value)
	if !ok {
		return ""
	}
	return field
}

func stableFieldSelector(value term) (string, bool) {
	if len(value.selectors) != 1 {
		return "", false
	}
	selector := value.selectors[0]
	if selector.field != "" && selector.index == nil {
		return selector.field, true
	}
	if selector.index == nil {
		return "", false
	}
	field, ok := expressionStringLiteral(*selector.index)
	return field, ok
}

func (a *analysisState) negatedConditionRefinement(value term) conditionRefinement {
	value = termWithoutCasts(value)
	var expr expression
	if value.group != nil && len(value.selectors) == 0 {
		expr = *value.group
	} else {
		expr = expressionFromTerm(value)
	}
	refinement := a.conditionRefinement(expr)
	if refinement.name == "" {
		return conditionRefinement{}
	}
	refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	return refinement
}

func (a *analysisState) nilComparisonRefinement(expr expression) (conditionRefinement, bool) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return conditionRefinement{}, false
	}
	comparison := expr.terms[0].terms[0]
	if comparison.right == nil || (comparison.op != comparisonEqual && comparison.op != comparisonNotEqual) {
		return conditionRefinement{}, false
	}
	name, field, ok := nilComparisonPlace(comparison)
	if !ok {
		return conditionRefinement{}, false
	}
	current := a.lookupLocal(name)
	if field != "" {
		current = a.lookupTableField(name, field)
	}
	if !typeAllows(current, simpleTypeNil) {
		return conditionRefinement{}, false
	}
	refinement := conditionRefinement{
		name:      name,
		field:     field,
		trueType:  simpleTypeNil,
		falseType: truthyType(current),
	}
	if comparison.op == comparisonNotEqual {
		refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	}
	return refinement, true
}

func nilComparisonPlace(comparison comparisonExpression) (string, string, bool) {
	if comparison.right == nil {
		return "", "", false
	}
	if name, field, ok := concatSinglePlace(comparison.left); ok && concatSingleNil(*comparison.right) {
		return name, field, true
	}
	if concatSingleNil(comparison.left) {
		return concatSinglePlace(*comparison.right)
	}
	return "", "", false
}

func (a *analysisState) singletonComparisonRefinement(expr expression) (conditionRefinement, bool) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return conditionRefinement{}, false
	}
	comparison := expr.terms[0].terms[0]
	if comparison.right == nil || (comparison.op != comparisonEqual && comparison.op != comparisonNotEqual) {
		return conditionRefinement{}, false
	}
	name, field, typ, ok := singletonComparisonPlace(comparison)
	if !ok {
		return conditionRefinement{}, false
	}
	current := a.lookupLocal(name)
	if field != "" {
		current = a.lookupTableField(name, field)
	}
	if !typeAllows(current, typ) {
		return conditionRefinement{}, false
	}
	refinement := conditionRefinement{
		name:      name,
		field:     field,
		trueType:  typ,
		falseType: simpleTypeUnknown,
	}
	if comparison.op == comparisonNotEqual {
		refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	}
	return refinement, true
}

func singletonComparisonPlace(comparison comparisonExpression) (string, string, simpleType, bool) {
	if comparison.right == nil {
		return "", "", simpleTypeUnknown, false
	}
	if name, field, ok := concatSinglePlace(comparison.left); ok {
		if typ, ok := concatSingleSingletonType(*comparison.right); ok {
			return name, field, typ, true
		}
	}
	if typ, ok := concatSingleSingletonType(comparison.left); ok {
		name, field, ok := concatSinglePlace(*comparison.right)
		return name, field, typ, ok
	}
	return "", "", simpleTypeUnknown, false
}

func (a *analysisState) typeGuardRefinement(expr expression) (conditionRefinement, bool) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return conditionRefinement{}, false
	}
	comparison := expr.terms[0].terms[0]
	if comparison.right == nil || (comparison.op != comparisonEqual && comparison.op != comparisonNotEqual) {
		return conditionRefinement{}, false
	}
	leftCall, ok := concatSingleCall(comparison.left)
	if !ok {
		return conditionRefinement{}, false
	}
	name, field, ok := typeCallPlace(leftCall)
	if !ok {
		return conditionRefinement{}, false
	}
	kind, ok := concatSingleString(*comparison.right)
	if !ok {
		return conditionRefinement{}, false
	}
	typ := simpleType(kind)
	current := a.lookupLocal(name)
	if field != "" {
		current = a.lookupTableField(name, field)
	}
	if !typeAllows(current, typ) {
		return conditionRefinement{}, false
	}
	refinement := conditionRefinement{
		name:      name,
		field:     field,
		trueType:  typ,
		falseType: typeWithout(current, typ),
	}
	if comparison.op == comparisonNotEqual {
		refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	}
	return refinement, true
}

func concatSinglePlace(expr concatExpression) (string, string, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return "", "", false
	}
	value := termWithoutCastsAndGroups(expr.first.first.first)
	if value.name == "" {
		return "", "", false
	}
	field := refinementField(value)
	if field == "" && len(value.selectors) != 0 {
		return "", "", false
	}
	return value.name, field, true
}

func concatSingleNil(expr concatExpression) bool {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return false
	}
	value := termWithoutCastsAndGroups(expr.first.first.first)
	if value.lit == nil {
		return false
	}
	return value.lit.Kind() == NilKind
}

func concatSingleSingletonType(expr concatExpression) (simpleType, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return simpleTypeUnknown, false
	}
	value := termWithoutCastsAndGroups(expr.first.first.first)
	if value.lit == nil {
		return simpleTypeUnknown, false
	}
	switch value.lit.Kind() {
	case BoolKind:
		return simpleTypeBoolean, true
	case StringKind:
		return simpleTypeString, true
	default:
		return simpleTypeUnknown, false
	}
}

func concatSingleCall(expr concatExpression) (callExpression, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return callExpression{}, false
	}
	value := termWithoutCastsAndGroups(expr.first.first.first)
	if value.call == nil || len(value.selectors) != 0 {
		return callExpression{}, false
	}
	return *value.call, true
}

func typeCallPlace(call callExpression) (string, string, bool) {
	target := termWithoutCastsAndGroups(call.target)
	if target.name != "type" || len(call.args) != 1 {
		return "", "", false
	}
	arg, ok := expressionSingleTerm(call.args[0])
	if !ok || arg.name == "" {
		return "", "", false
	}
	field := refinementField(arg)
	if field == "" && len(arg.selectors) != 0 {
		return "", "", false
	}
	return arg.name, field, true
}

func concatSingleString(expr concatExpression) (string, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return "", false
	}
	value := termWithoutCastsAndGroups(expr.first.first.first)
	if value.lit == nil {
		return "", false
	}
	return value.lit.String()
}

func truthyType(typ simpleType) simpleType {
	if strings.HasSuffix(string(typ), "?") {
		return simpleType(strings.TrimSuffix(string(typ), "?"))
	}
	if typ == simpleTypeNil {
		return simpleTypeUnknown
	}
	return typ
}

func falseyType(typ simpleType) simpleType {
	if strings.HasSuffix(string(typ), "?") {
		return simpleTypeNil
	}
	if typ == simpleTypeNil {
		return simpleTypeNil
	}
	return simpleTypeUnknown
}

func (a *analysisState) inferExpression(expr expression) simpleType {
	if len(expr.terms) == 0 {
		return simpleTypeUnknown
	}
	result := a.inferAndExpression(expr.terms[0])
	resultTruthy, resultTruthKnown := andExpressionKnownTruthiness(expr.terms[0])
	for _, term := range expr.terms[1:] {
		right := a.inferAndExpression(term)
		if resultTruthKnown {
			if resultTruthy {
				continue
			}
			result = right
			resultTruthy, resultTruthKnown = andExpressionKnownTruthiness(term)
			continue
		}
		result = unionSimpleTypes(truthyType(result), right)
		resultTruthKnown = false
	}
	return result
}

func (a *analysisState) inferAndExpression(expr andExpression) simpleType {
	if len(expr.terms) == 0 {
		return simpleTypeUnknown
	}
	result := a.inferComparisonExpression(expr.terms[0])
	resultTruthy, resultTruthKnown := comparisonKnownTruthiness(expr.terms[0])
	for _, term := range expr.terms[1:] {
		right := a.inferComparisonExpression(term)
		if resultTruthKnown {
			if resultTruthy {
				result = right
				resultTruthy, resultTruthKnown = comparisonKnownTruthiness(term)
			}
			continue
		}
		if truthy, known := simpleTypeKnownTruthiness(result); known {
			if truthy {
				result = right
				resultTruthy, resultTruthKnown = comparisonKnownTruthiness(term)
			}
			continue
		}
		result = unionSimpleTypes(falseyType(result), right)
		resultTruthKnown = false
	}
	return result
}

func (a *analysisState) inferComparisonExpression(expr comparisonExpression) simpleType {
	left := a.inferConcatExpression(expr.left)
	if expr.op == "" || expr.right == nil {
		return left
	}
	right := a.inferConcatExpression(*expr.right)
	if isOrderedComparison(expr.op) {
		a.checkComparisonOperand(string(expr.op), left, right, concatExpressionRange(*expr.right))
	}
	return simpleTypeBoolean
}

func andExpressionKnownTruthiness(expr andExpression) (bool, bool) {
	if len(expr.terms) != 1 {
		return false, false
	}
	return comparisonKnownTruthiness(expr.terms[0])
}

func comparisonKnownTruthiness(expr comparisonExpression) (bool, bool) {
	if expr.op != "" || expr.right != nil {
		return false, false
	}
	return concatExpressionKnownTruthiness(expr.left)
}

func concatExpressionKnownTruthiness(expr concatExpression) (bool, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return false, false
	}
	value := termWithoutCastsAndGroups(expr.first.first.first)
	if value.lit == nil {
		if typ := simpleTypeFromTerm(value); typ != simpleTypeUnknown {
			return simpleTypeKnownTruthiness(typ)
		}
		return false, false
	}
	switch value.lit.Kind() {
	case NilKind:
		return false, true
	case BoolKind:
		boolean, ok := value.lit.Bool()
		return boolean, ok
	case NumberKind, StringKind:
		return true, true
	default:
		return false, false
	}
}

func simpleTypeKnownTruthiness(typ simpleType) (bool, bool) {
	switch typ {
	case simpleTypeNil:
		return false, true
	case simpleTypeNumber, simpleTypeString, simpleTypeTable, simpleTypeFunction,
		simpleTypeThread, simpleTypeUserData, simpleTypeBuffer, simpleTypeVector:
		return true, true
	default:
		return false, false
	}
}

func isOrderedComparison(op comparisonOperator) bool {
	switch op {
	case comparisonLess, comparisonLessEqual, comparisonGreater, comparisonGreaterEqual:
		return true
	default:
		return false
	}
}

func (a *analysisState) inferConcatExpression(expr concatExpression) simpleType {
	result := a.inferAdditiveExpression(expr.first)
	if len(expr.rest) == 0 {
		return result
	}
	a.checkConcatOperand(result, additiveExpressionRange(expr.first))
	for _, part := range expr.rest {
		value := a.inferAdditiveExpression(part)
		a.checkConcatOperand(value, additiveExpressionRange(part))
	}
	return simpleTypeString
}

func (a *analysisState) inferAdditiveExpression(expr additiveExpression) simpleType {
	result := a.inferMultiplicativeExpression(expr.first)
	if len(expr.rest) == 0 {
		return result
	}
	a.checkBinaryOperand(string(expr.rest[0].op), result, multiplicativeExpressionRange(expr.first))
	for _, part := range expr.rest {
		value := a.inferMultiplicativeExpression(part.value)
		a.checkBinaryOperand(string(part.op), value, multiplicativeExpressionRange(part.value))
	}
	return simpleTypeNumber
}

func (a *analysisState) inferMultiplicativeExpression(expr multiplicativeExpression) simpleType {
	result := a.inferTerm(expr.first)
	if len(expr.rest) == 0 {
		return result
	}
	a.checkBinaryOperand(string(expr.rest[0].op), result, termRange(expr.first))
	for _, part := range expr.rest {
		value := a.inferTerm(part.value)
		a.checkBinaryOperand(string(part.op), value, termRange(part.value))
	}
	return simpleTypeNumber
}

func (a *analysisState) inferTerm(value term) simpleType {
	if value.cast != nil {
		uncast := value
		uncast.cast = nil
		a.inferTerm(uncast)
		return a.simpleTypeFromAnnotation(value.cast)
	}
	if fieldType := a.inferFieldRead(value); fieldType != simpleTypeUnknown {
		return fieldType
	}
	if value.unaryNot != nil {
		a.inferTerm(*value.unaryNot)
		return simpleTypeBoolean
	}
	if value.unaryMinus != nil {
		actual := a.inferTerm(*value.unaryMinus)
		constraint := typeConstraint{
			kind:     constraintUnaryOp,
			operator: "-",
			expected: simpleTypeNumber,
			actual:   actual,
			evidence: constraintEvidence{
				span: termRange(*value.unaryMinus),
			},
		}
		a.checkConstraint(constraint)
		return simpleTypeNumber
	}
	if value.unaryLen != nil {
		actual := a.inferTerm(*value.unaryLen)
		constraint := typeConstraint{
			kind:     constraintUnaryOp,
			operator: "#",
			expected: simpleType("string|table"),
			actual:   actual,
			evidence: constraintEvidence{
				span: termRange(*value.unaryLen),
			},
		}
		a.checkConstraint(constraint)
		return simpleTypeNumber
	}
	if len(value.selectors) != 0 {
		a.checkUnknownName(value.name, value.start, value.start+len(value.name))
		return simpleTypeUnknown
	}
	if value.name != "" {
		typ := a.lookupNamedTerm(value)
		if typ == simpleTypeUnknown {
			a.checkUnknownName(value.name, value.start, value.start+len(value.name))
		}
		return typ
	}
	if value.call == nil {
		return simpleTypeFromTerm(value)
	}
	if fact, ok := a.functionFactForCall(*value.call); ok {
		return a.checkCallArguments(fact, *value.call)
	}
	return simpleTypeUnknown
}

func (a *analysisState) checkBinaryOperand(operator string, actual simpleType, span sourceRange) {
	constraint := typeConstraint{
		kind:     constraintBinaryOp,
		operator: operator,
		expected: simpleTypeNumber,
		actual:   actual,
		evidence: constraintEvidence{
			span: span,
		},
	}
	a.checkConstraint(constraint)
}

func (a *analysisState) checkConcatOperand(actual simpleType, span sourceRange) {
	constraint := typeConstraint{
		kind:     constraintBinaryOp,
		operator: "..",
		expected: simpleType("string|number"),
		actual:   actual,
		evidence: constraintEvidence{
			span: span,
		},
	}
	a.checkConstraint(constraint)
}

func (a *analysisState) checkComparisonOperand(operator string, left, right simpleType, span sourceRange) {
	expected := simpleType("string|number")
	if left == simpleTypeNumber || left == simpleTypeString {
		expected = left
	}
	constraint := typeConstraint{
		kind:     constraintBinaryOp,
		operator: operator,
		expected: expected,
		actual:   right,
		evidence: constraintEvidence{
			span: span,
		},
	}
	a.checkConstraint(constraint)
}

func (a *analysisState) checkConstraint(constraint typeConstraint) bool {
	switch constraint.kind {
	case constraintIndexRead:
		if constraint.expected != simpleTypeUnknown {
			return true
		}
		a.diagnostics = append(a.diagnostics, unknownPropertyDiagnostic(
			constraint.field,
			constraint.evidence.span.start,
			constraint.evidence.span.end,
		))
		return false
	case constraintIndexWrite:
		if constraint.expected == simpleTypeUnknown {
			a.diagnostics = append(a.diagnostics, unknownPropertyDiagnostic(
				constraint.field,
				constraint.evidence.span.start,
				constraint.evidence.span.end,
			))
			return false
		}
		if constraint.actual == simpleTypeUnknown || typeAllows(constraint.expected, constraint.actual) {
			return true
		}
		a.diagnostics = append(a.diagnostics, typeMismatchDiagnostic(
			constraint.expected,
			constraint.actual,
			constraint.evidence.span.start,
			constraint.evidence.span.end,
		))
		return false
	case constraintAssignable:
		if constraint.actual == simpleTypeUnknown || typeAllows(constraint.expected, constraint.actual) {
			if !constraint.expectedTable.empty() && !constraint.actualTable.empty() {
				return a.checkTableAssignable(constraint.expectedTable, constraint.actualTable, constraint.evidence.span)
			}
			return true
		}
		a.diagnostics = append(a.diagnostics, typeMismatchDiagnostic(
			constraint.expected,
			constraint.actual,
			constraint.evidence.span.start,
			constraint.evidence.span.end,
		))
		return false
	case constraintBinaryOp, constraintUnaryOp:
		if constraint.actual == simpleTypeUnknown || typeAllows(constraint.expected, constraint.actual) {
			return true
		}
		a.diagnostics = append(a.diagnostics, typeMismatchDiagnostic(
			constraint.expected,
			constraint.actual,
			constraint.evidence.span.start,
			constraint.evidence.span.end,
		))
		return false
	default:
		return true
	}
}

func (a *analysisState) checkTableAssignable(expected, actual tableFact, span sourceRange) bool {
	for field, expectedType := range expected.fields {
		actualType, ok := actual.fields[field]
		if !ok {
			actualIndexer, hasIndexer := tableIndexerForKeyType(actual, simpleTypeString)
			if hasIndexer && (actualIndexer.value == simpleTypeUnknown || typeAllows(expectedType, actualIndexer.value)) {
				continue
			}
			if hasIndexer {
				a.diagnostics = append(a.diagnostics, typeMismatchDiagnostic(
					expectedType,
					actualIndexer.value,
					span.start,
					span.end,
				))
				return false
			}
			if typeAllows(expectedType, simpleTypeNil) {
				continue
			}
			a.diagnostics = append(a.diagnostics, missingPropertyDiagnostic(field, span.start, span.end))
			return false
		}
		if actualType == simpleTypeUnknown || typeAllows(expectedType, actualType) {
			continue
		}
		a.diagnostics = append(a.diagnostics, typeMismatchDiagnostic(
			expectedType,
			actualType,
			span.start,
			span.end,
		))
		return false
	}
	for _, expectedIndexer := range expected.indexers {
		actualIndexer, ok := tableIndexerForKeyType(actual, expectedIndexer.key)
		if !ok {
			if expectedIndexer.key == simpleTypeString && len(actual.fields) != 0 {
				continue
			}
			a.diagnostics = append(a.diagnostics, missingPropertyDiagnostic("indexer", span.start, span.end))
			return false
		}
		if actualIndexer.value == simpleTypeUnknown || typeAllows(expectedIndexer.value, actualIndexer.value) {
			continue
		}
		a.diagnostics = append(a.diagnostics, typeMismatchDiagnostic(
			expectedIndexer.value,
			actualIndexer.value,
			span.start,
			span.end,
		))
		return false
	}
	if expectedIndexer, ok := tableIndexerForKeyType(expected, simpleTypeString); ok {
		for _, actualType := range actual.fields {
			if expectedIndexer.value == simpleTypeUnknown {
				continue
			}
			if actualType == simpleTypeUnknown || typeAllows(expectedIndexer.value, actualType) {
				continue
			}
			a.diagnostics = append(a.diagnostics, typeMismatchDiagnostic(
				expectedIndexer.value,
				actualType,
				span.start,
				span.end,
			))
			return false
		}
	}
	return true
}

func expressionRange(expr expression) sourceRange {
	if len(expr.terms) == 0 {
		return sourceRange{}
	}
	span := andExpressionRange(expr.terms[0])
	for _, term := range expr.terms[1:] {
		span.end = andExpressionRange(term).end
	}
	return span
}

func andExpressionRange(expr andExpression) sourceRange {
	if len(expr.terms) == 0 {
		return sourceRange{}
	}
	span := comparisonExpressionRange(expr.terms[0])
	for _, term := range expr.terms[1:] {
		span.end = comparisonExpressionRange(term).end
	}
	return span
}

func comparisonExpressionRange(expr comparisonExpression) sourceRange {
	span := concatExpressionRange(expr.left)
	if expr.right != nil {
		span.end = concatExpressionRange(*expr.right).end
	}
	return span
}

func concatExpressionRange(expr concatExpression) sourceRange {
	span := additiveExpressionRange(expr.first)
	for _, part := range expr.rest {
		span.end = additiveExpressionRange(part).end
	}
	return span
}

func additiveExpressionRange(expr additiveExpression) sourceRange {
	span := multiplicativeExpressionRange(expr.first)
	for _, part := range expr.rest {
		span.end = multiplicativeExpressionRange(part.value).end
	}
	return span
}

func multiplicativeExpressionRange(expr multiplicativeExpression) sourceRange {
	span := termRange(expr.first)
	for _, part := range expr.rest {
		span.end = termRange(part.value).end
	}
	return span
}

func termRange(value term) sourceRange {
	return sourceRange{start: value.start, end: value.end}
}

func (a *analysisState) functionFactForCall(call callExpression) (functionFact, bool) {
	return a.functionFactForCallWithDiagnostics(call, true)
}

func (a *analysisState) functionFactForCallQuiet(call callExpression) (functionFact, bool) {
	return a.functionFactForCallWithDiagnostics(call, false)
}

func (a *analysisState) functionFactForCallWithDiagnostics(call callExpression, diagnoseAccess bool) (functionFact, bool) {
	target := termWithoutCastsAndGroups(call.target)
	if target.name == "" {
		return functionFact{}, false
	}
	if use, ok := a.bind.useAt(target.start, target.start+len(target.name)); ok {
		if len(target.selectors) != 0 {
			if fact, ok := a.tableFunctionFactForCallTarget(target, diagnoseAccess); ok {
				return fact, true
			}
			return functionFact{}, false
		}
		fact, ok := a.functions[use.symbol]
		return fact, ok
	}
	if len(target.selectors) != 0 {
		if fact, ok := a.tableFunctionFactForCallTarget(target, diagnoseAccess); ok {
			return fact, true
		}
		return functionFact{}, false
	}
	if len(target.selectors) == 0 {
		fact, ok := a.typeEnv.lookup(target.name)
		if ok && fact.hasFunction {
			return fact.function, true
		}
	}
	return functionFact{}, false
}

func (a *analysisState) tableFunctionFactForCallTarget(target term, diagnoseAccess bool) (functionFact, bool) {
	if target.name == "" || len(target.selectors) != 1 {
		return functionFact{}, false
	}
	selector := target.selectors[0]
	if selector.field == "" || selector.index != nil {
		return functionFact{}, false
	}
	table, ok := a.lookupTableFact(target.name)
	if !ok || table.functions == nil {
		return functionFact{}, false
	}
	if table.access[selector.field] == "write" {
		if diagnoseAccess {
			a.diagnostics = append(a.diagnostics, writeonlyPropertyDiagnostic(
				selector.field,
				target.start,
				target.end,
			))
		}
		return functionFact{}, false
	}
	fact, ok := table.functions[selector.field]
	return fact, ok
}

func (a *analysisState) functionFactFromAnnotation(annotation *typeExpression) (functionFact, bool) {
	if summary, ok := a.moduleExportedTypeAliasSummary(annotation); ok {
		if summary.Kind == TypeSummaryFunction || summary.Kind == TypeSummaryGenericFunction {
			return functionFactFromSummary(summary), true
		}
	}
	annotation, substitutions := a.functionAnnotation(annotation)
	if annotation == nil || (annotation.kind != typeKindFunction && annotation.kind != typeKindGenericFunction) {
		return functionFact{}, false
	}
	returnPack := a.returnPackFromAnnotationWith(annotation.returnType, substitutions)
	fact := functionFact{
		typeParams:    append([]string(nil), annotation.typeParams...),
		params:        make([]simpleType, 0, len(annotation.params)),
		paramGenerics: make([]string, 0, len(annotation.params)),
		returnType:    returnPack.firstType(a.simpleTypeFromAnnotationWith(annotation.returnType, substitutions)),
		returnTable:   returnPack.firstTable(a.tableFactFromAnnotation(annotation.returnType)),
		returnSpan:    annotationRange(annotation.returnType),
		returnPack:    returnPack,
		returnGeneric: genericAnnotationName(annotation.returnType, annotation.typeParams),
	}
	for _, param := range annotation.params {
		value := param.value
		if value != nil && value.kind == typeKindVariadic {
			value = value.inner
		}
		if param.variadic || (param.value != nil && param.value.kind == typeKindVariadic) {
			fact.variadic = a.simpleTypeFromAnnotationWith(value, substitutions)
			fact.variadicGeneric = genericAnnotationName(value, annotation.typeParams)
			continue
		}
		fact.params = append(fact.params, a.simpleTypeFromAnnotationWith(value, substitutions))
		fact.paramGenerics = append(fact.paramGenerics, genericAnnotationName(value, annotation.typeParams))
	}
	return fact, true
}

func (a *analysisState) functionAnnotation(annotation *typeExpression) (*typeExpression, map[string]simpleType) {
	annotation, substitutions := a.resolveAliasAnnotation(annotation)
	if annotation == nil || (annotation.kind != typeKindFunction && annotation.kind != typeKindGenericFunction) {
		return nil, nil
	}
	return annotation, substitutions
}

func (a *analysisState) inferFieldRead(value term) simpleType {
	if value.name == "" || len(value.selectors) != 1 {
		return simpleTypeUnknown
	}
	fact, ok := a.lookupTableFact(value.name)
	if !ok {
		return simpleTypeUnknown
	}
	selector := value.selectors[0]
	if field, ok := stableFieldSelector(value); ok {
		typ, exists := fact.fields[field]
		if exists {
			if fact.access[field] == "write" {
				a.diagnostics = append(a.diagnostics, writeonlyPropertyDiagnostic(
					field,
					value.start,
					value.end,
				))
				return simpleTypeUnknown
			}
			return typ
		}
	}
	if selector.index != nil {
		key := a.inferExpression(*selector.index)
		if indexer, ok := a.tableIndexerForKey(fact, key, expressionRange(*selector.index)); ok {
			if indexer.access == "write" {
				a.diagnostics = append(a.diagnostics, writeonlyPropertyDiagnostic(
					"indexer",
					value.start,
					value.end,
				))
				return simpleTypeUnknown
			}
			return indexer.value
		}
		return simpleTypeUnknown
	}
	if selector.field == "" {
		return simpleTypeUnknown
	}
	if indexer, ok := tableIndexerForKeyType(fact, simpleTypeString); ok {
		if indexer.access == "write" {
			a.diagnostics = append(a.diagnostics, writeonlyPropertyDiagnostic(
				"indexer",
				value.start,
				value.end,
			))
			return simpleTypeUnknown
		}
		return indexer.value
	}
	constraint := typeConstraint{
		kind:  constraintIndexRead,
		field: selector.field,
		evidence: constraintEvidence{
			span: termRange(value),
		},
	}
	a.checkConstraint(constraint)
	return simpleTypeUnknown
}

func (a *analysisState) tableIndexerForKey(fact tableFact, key simpleType, span sourceRange) (tableIndexerFact, bool) {
	if indexer, ok := tableIndexerForKeyType(fact, key); ok {
		return indexer, true
	}
	if key == simpleTypeUnknown || len(fact.indexers) == 0 {
		return tableIndexerFact{}, false
	}
	expected := fact.indexers[0].key
	if expected == simpleTypeUnknown {
		return tableIndexerFact{}, false
	}
	constraint := typeConstraint{
		kind:     constraintAssignable,
		expected: expected,
		actual:   key,
		evidence: constraintEvidence{
			span: span,
		},
	}
	a.checkConstraint(constraint)
	return tableIndexerFact{}, false
}

func tableIndexerForKeyType(fact tableFact, key simpleType) (tableIndexerFact, bool) {
	for _, indexer := range fact.indexers {
		if typeAllows(indexer.key, key) {
			return indexer, true
		}
	}
	return tableIndexerFact{}, false
}

func (a *analysisState) checkCallArguments(fact functionFact, call callExpression) simpleType {
	substitutions := a.explicitGenericSubstitutions(fact, call)
	receiverOffset := methodReceiverOffset(call)
	for i, expected := range fact.params {
		generic := genericAt(fact.paramGenerics, i)
		actual := simpleTypeNil
		span := termRange(call.target)
		if call.receiver != nil && i == 0 {
			actual = a.inferTerm(*call.receiver)
			span = termRange(*call.receiver)
		} else if argIndex := i - receiverOffset; argIndex >= 0 && argIndex < len(call.args) {
			actual = a.inferExpression(call.args[argIndex])
			span = expressionRange(call.args[argIndex])
		}
		if generic != "" {
			if substituted, ok := substitutions[generic]; ok {
				expected = substituted
			} else {
				substitutions[generic] = actual
				expected = actual
			}
		}
		if expected == simpleTypeUnknown {
			continue
		}
		constraint := typeConstraint{
			kind:     constraintAssignable,
			expected: expected,
			actual:   actual,
			evidence: constraintEvidence{
				span: span,
			},
		}
		if a.checkConstraint(constraint) {
			continue
		}
	}
	a.checkVariadicCallArguments(fact, call, substitutions)
	if fact.returnGeneric != "" {
		if typ, ok := substitutions[fact.returnGeneric]; ok {
			return typ
		}
		return simpleTypeUnknown
	}
	return fact.returnType
}

func (a *analysisState) checkVariadicCallArguments(fact functionFact, call callExpression, substitutions map[string]simpleType) {
	if fact.variadic == simpleTypeUnknown && fact.variadicGeneric == "" {
		return
	}
	start := len(fact.params) - methodReceiverOffset(call)
	if start < 0 {
		start = 0
	}
	for i := start; i < len(call.args); i++ {
		expected := fact.variadic
		actual := a.inferExpression(call.args[i])
		if fact.variadicGeneric != "" {
			if substituted, ok := substitutions[fact.variadicGeneric]; ok {
				expected = substituted
			} else {
				substitutions[fact.variadicGeneric] = actual
				expected = actual
			}
		}
		if expected == simpleTypeUnknown {
			continue
		}
		constraint := typeConstraint{
			kind:     constraintAssignable,
			expected: expected,
			actual:   actual,
			evidence: constraintEvidence{
				span: expressionRange(call.args[i]),
			},
		}
		a.checkConstraint(constraint)
	}
}

func methodReceiverOffset(call callExpression) int {
	if call.receiver == nil {
		return 0
	}
	return 1
}

func (a *analysisState) explicitGenericSubstitutions(fact functionFact, call callExpression) map[string]simpleType {
	substitutions := make(map[string]simpleType)
	for i, typeParam := range fact.typeParams {
		if i >= len(call.typeArgs) {
			break
		}
		typ := a.simpleTypeFromAnnotation(call.typeArgs[i])
		if typ == simpleTypeUnknown {
			continue
		}
		substitutions[typeParam] = typ
	}
	return substitutions
}

func genericAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

func genericAnnotationNames(annotations []*typeExpression, typeParams []string) []string {
	names := make([]string, len(annotations))
	for i, annotation := range annotations {
		names[i] = genericAnnotationName(annotation, typeParams)
	}
	return names
}

func genericAnnotationName(annotation *typeExpression, typeParams []string) string {
	if annotation == nil || annotation.kind != typeKindName || len(annotation.name) != 1 || len(annotation.typeArgs) != 0 {
		return ""
	}
	name := annotation.name[0]
	for _, typeParam := range typeParams {
		if name == typeParam {
			return name
		}
	}
	return ""
}

func (a *analysisState) checkFunctionParameterTypeNames(annotations []*typeExpression, variadicAnnotation *typeExpression) {
	for _, annotation := range annotations {
		a.checkUnknownTypeNames(annotation)
	}
	a.checkUnknownTypeNames(variadicAnnotation)
}

func (a *analysisState) checkUnknownTypeNames(annotation *typeExpression) {
	if !policyForMode(a.mode).reportsUnknownTypes() || annotation == nil {
		return
	}
	switch annotation.kind {
	case typeKindName:
		a.checkUnknownTypeName(annotation)
		for _, arg := range annotation.typeArgs {
			a.checkUnknownTypeNames(arg)
		}
	case typeKindUnion, typeKindIntersection:
		for _, option := range annotation.types {
			a.checkUnknownTypeNames(option)
		}
	case typeKindNilable, typeKindVariadic, typeKindGenericPack:
		a.checkUnknownTypeNames(annotation.inner)
	case typeKindTable:
		for _, field := range annotation.fields {
			a.checkUnknownTypeNames(field.key)
			a.checkUnknownTypeNames(field.value)
		}
	case typeKindFunction:
		a.checkFunctionTypeNames(annotation)
	case typeKindGenericFunction:
		a.checkFunctionTypeNames(annotation)
	case typeKindTypeof, typeKindSingleton:
	}
}

func (a *analysisState) checkFunctionTypeNames(annotation *typeExpression) {
	for _, param := range annotation.params {
		a.checkUnknownTypeNames(param.value)
	}
	a.checkUnknownTypeNames(annotation.returnType)
}

func (a *analysisState) checkUnknownTypeName(annotation *typeExpression) {
	if len(annotation.name) == 0 || a.isKnownBuiltinTypeName(annotation.name[0]) {
		return
	}
	start := annotation.start
	end := start + len(annotation.name[0])
	if _, ok := a.bind.useAt(start, end); ok {
		if len(annotation.name) == 2 {
			if _, isModule := a.lookupModuleLocal(annotation.name[0]); isModule {
				if _, ok := a.lookupModuleExportedTypeAlias(annotation.name[0], annotation.name[1]); !ok {
					aliasStart := end + 1
					a.diagnostics = append(a.diagnostics, unknownTypeDiagnostic(
						annotation.name[1],
						aliasStart,
						aliasStart+len(annotation.name[1]),
					))
				}
			}
		}
		return
	}
	a.diagnostics = append(a.diagnostics, unknownTypeDiagnostic(annotation.name[0], start, end))
}

func (a *analysisState) isKnownBuiltinTypeName(name string) bool {
	switch name {
	case "any", "unknown", "never", "nil", "boolean", "number", "string",
		"table", "thread", "userdata", "buffer", "vector":
		return true
	default:
		return false
	}
}

func (a *analysisState) simpleTypeFromAnnotation(annotation *typeExpression) simpleType {
	return a.simpleTypeFromAnnotationWith(annotation, nil)
}

func (a *analysisState) simpleTypeFromAnnotationWith(annotation *typeExpression, substitutions map[string]simpleType) simpleType {
	if annotation == nil {
		return simpleTypeUnknown
	}
	switch annotation.kind {
	case typeKindTable:
		return simpleTypeTable
	case typeKindFunction, typeKindGenericFunction:
		return simpleTypeFunction
	case typeKindNilable:
		inner := a.simpleTypeFromAnnotationWith(annotation.inner, substitutions)
		if inner == simpleTypeUnknown {
			return simpleTypeUnknown
		}
		return inner + "?"
	case typeKindUnion:
		var parts []string
		for _, option := range annotation.types {
			optionType := a.simpleTypeFromAnnotationWith(option, substitutions)
			if optionType == simpleTypeUnknown {
				return simpleTypeUnknown
			}
			parts = append(parts, string(optionType))
		}
		return simpleType(strings.Join(parts, "|"))
	}
	if annotation.kind != typeKindName || len(annotation.name) != 1 {
		if summary, ok := a.moduleExportedTypeAliasSummary(annotation); ok {
			return simpleTypeFromSummary(summary)
		}
		return simpleTypeUnknown
	}
	name := annotation.name[0]
	if len(annotation.typeArgs) == 0 {
		if substituted, ok := substitutions[name]; ok {
			return substituted
		}
	}
	switch name {
	case "any":
		if len(annotation.typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeAny
	case "unknown":
		if len(annotation.typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeCheckedUnknown
	case "never":
		if len(annotation.typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeNever
	case "nil":
		if len(annotation.typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeNil
	case "boolean":
		if len(annotation.typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeBoolean
	case "number":
		if len(annotation.typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeNumber
	case "string":
		if len(annotation.typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeString
	case "table":
		if len(annotation.typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeTable
	case "thread":
		if len(annotation.typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeThread
	case "userdata":
		if len(annotation.typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeUserData
	case "buffer":
		if len(annotation.typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeBuffer
	case "vector":
		if len(annotation.typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeVector
	default:
		if alias, ok := a.lookupTypeAlias(name); ok {
			next, ok := a.genericAliasSubstitutions(alias, annotation.typeArgs, substitutions)
			if !ok {
				return simpleTypeUnknown
			}
			return a.simpleTypeFromAnnotationWith(alias.value, next)
		}
		return simpleTypeUnknown
	}
}

func (a *analysisState) genericAliasSubstitutions(alias typeAliasFact, args []*typeExpression, outer map[string]simpleType) (map[string]simpleType, bool) {
	if len(alias.typePacks) != 0 || len(args) != len(alias.typeParams) {
		return nil, false
	}
	if len(alias.typeParams) == 0 {
		if len(args) == 0 {
			return outer, true
		}
		return nil, false
	}
	next := make(map[string]simpleType, len(outer)+len(alias.typeParams))
	for name, typ := range outer {
		next[name] = typ
	}
	for i, name := range alias.typeParams {
		typ := a.simpleTypeFromAnnotationWith(args[i], outer)
		if typ == simpleTypeUnknown {
			return nil, false
		}
		next[name] = typ
	}
	return next, true
}

func (a *analysisState) simpleTypesFromAnnotations(annotations []*typeExpression) []simpleType {
	types := make([]simpleType, len(annotations))
	for i, annotation := range annotations {
		types[i] = a.simpleTypeFromAnnotation(annotation)
	}
	return types
}

func (a *analysisState) tableFieldTypes(annotation *typeExpression) map[string]simpleType {
	return a.tableFactFromAnnotation(annotation).fields
}

func (a *analysisState) tableFactFromAnnotation(annotation *typeExpression) tableFact {
	if summary, ok := a.moduleExportedTypeAliasSummary(annotation); ok && summary.Kind == TypeSummaryTable {
		return tableFactFromSummary(summary)
	}
	fields := make(map[string]simpleType)
	access := make(map[string]string)
	functions := make(map[string]functionFact)
	annotation, substitutions := a.tableAnnotation(annotation)
	if annotation == nil {
		return tableFact{}
	}
	var indexers []tableIndexerFact
	for _, field := range annotation.fields {
		if field.name != "" {
			fields[field.name] = a.simpleTypeFromAnnotationWith(field.value, substitutions)
			if field.access != "" {
				access[field.name] = field.access
			}
			if fact, ok := a.functionFactFromAnnotation(field.value); ok {
				functions[field.name] = fact
			}
			continue
		}
		if field.key == nil {
			value := a.simpleTypeFromAnnotationWith(field.value, substitutions)
			if value != simpleTypeUnknown {
				indexers = append(indexers, tableIndexerFact{key: simpleTypeNumber, value: value, access: field.access})
			}
			continue
		}
		key := a.simpleTypeFromAnnotationWith(field.key, substitutions)
		value := a.simpleTypeFromAnnotationWith(field.value, substitutions)
		if key == simpleTypeUnknown || value == simpleTypeUnknown {
			continue
		}
		indexers = append(indexers, tableIndexerFact{key: key, value: value, access: field.access})
	}
	return tableFact{known: true, fields: fields, access: access, functions: functions, indexers: indexers}
}

func (a *analysisState) moduleExportedTypeAliasSummary(annotation *typeExpression) (TypeSummary, bool) {
	if annotation == nil || annotation.kind != typeKindName || len(annotation.name) != 2 || len(annotation.typeArgs) != 0 {
		return TypeSummary{}, false
	}
	exported, ok := a.lookupModuleExportedTypeAlias(annotation.name[0], annotation.name[1])
	if !ok {
		return TypeSummary{}, false
	}
	return exported.Type, true
}

func (a *analysisState) tableAnnotation(annotation *typeExpression) (*typeExpression, map[string]simpleType) {
	annotation, substitutions := a.resolveAliasAnnotation(annotation)
	if annotation == nil || annotation.kind != typeKindTable {
		return nil, nil
	}
	return annotation, substitutions
}

func (a *analysisState) resolveAliasAnnotation(annotation *typeExpression) (*typeExpression, map[string]simpleType) {
	if annotation == nil || annotation.kind != typeKindName || len(annotation.name) != 1 {
		return annotation, nil
	}
	if alias, ok := a.lookupTypeAlias(annotation.name[0]); ok {
		substitutions, ok := a.genericAliasSubstitutions(alias, annotation.typeArgs, nil)
		if !ok {
			return nil, nil
		}
		return alias.value, substitutions
	}
	return annotation, nil
}

func typeAllows(expected, actual simpleType) bool {
	if expected == actual {
		return true
	}
	if expected == simpleTypeAny || actual == simpleTypeAny {
		return true
	}
	if expected == simpleTypeCheckedUnknown || actual == simpleTypeNever {
		return true
	}
	if strings.Contains(string(actual), "|") {
		for _, option := range strings.Split(string(actual), "|") {
			if option == "" {
				continue
			}
			if !typeAllows(expected, simpleType(option)) {
				return false
			}
		}
		return true
	}
	for _, option := range strings.Split(string(expected), "|") {
		if optionAllows(simpleType(option), actual) {
			return true
		}
	}
	return false
}

func typeWithout(source, removed simpleType) simpleType {
	var kept []string
	for _, option := range strings.Split(string(source), "|") {
		if option == "" || typeAllows(simpleType(option), removed) {
			continue
		}
		kept = append(kept, option)
	}
	if len(kept) == 0 {
		return simpleTypeUnknown
	}
	return simpleType(strings.Join(kept, "|"))
}

func unionSimpleTypes(left, right simpleType) simpleType {
	if left == simpleTypeUnknown {
		return right
	}
	if right == simpleTypeUnknown || typeAllows(left, right) {
		return left
	}
	var joined []string
	seen := make(map[string]bool)
	for _, typ := range []simpleType{left, right} {
		for _, option := range strings.Split(string(typ), "|") {
			if option == "" || seen[option] {
				continue
			}
			seen[option] = true
			joined = append(joined, option)
		}
	}
	return simpleType(strings.Join(joined, "|"))
}

func optionAllows(expected, actual simpleType) bool {
	if expected == actual {
		return true
	}
	if !strings.HasSuffix(string(expected), "?") {
		return false
	}
	base := simpleType(strings.TrimSuffix(string(expected), "?"))
	return actual == simpleTypeNil || actual == base
}

func simpleTypeFromExpression(expr expression) simpleType {
	value, ok := expressionSingleTerm(expr)
	if !ok || len(value.selectors) != 0 {
		return simpleTypeUnknown
	}
	return simpleTypeFromTerm(value)
}

func simpleTypeFromTerm(value term) simpleType {
	switch {
	case value.number != nil:
		return simpleTypeNumber
	case value.lit != nil:
		switch value.lit.Kind() {
		case NilKind:
			return simpleTypeNil
		case BoolKind:
			return simpleTypeBoolean
		case NumberKind:
			return simpleTypeNumber
		case StringKind:
			return simpleTypeString
		}
	case value.table != nil:
		return simpleTypeTable
	case value.function != nil:
		return simpleTypeFunction
	case value.group != nil:
		return simpleTypeFromExpression(*value.group)
	}
	return simpleTypeUnknown
}

func typeMismatchDiagnostic(expected, actual simpleType, start, end int) Diagnostic {
	return Diagnostic{
		Code:    "type-mismatch",
		Message: fmt.Sprintf("cannot assign %s to %s", actual, expected),
		Start:   start,
		End:     end,
	}
}

func unknownPropertyDiagnostic(field string, start, end int) Diagnostic {
	return Diagnostic{
		Code:    "unknown-property",
		Message: fmt.Sprintf("unknown property %q", field),
		Start:   start,
		End:     end,
	}
}

func readonlyPropertyDiagnostic(field string, start, end int) Diagnostic {
	return Diagnostic{
		Code:    "readonly-property",
		Message: fmt.Sprintf("cannot write read-only property %q", field),
		Start:   start,
		End:     end,
	}
}

func writeonlyPropertyDiagnostic(field string, start, end int) Diagnostic {
	return Diagnostic{
		Code:    "writeonly-property",
		Message: fmt.Sprintf("cannot read write-only property %q", field),
		Start:   start,
		End:     end,
	}
}

func unknownNameDiagnostic(name string, start, end int) Diagnostic {
	return Diagnostic{
		Code:    "unknown-name",
		Message: fmt.Sprintf("unknown name %q", name),
		Start:   start,
		End:     end,
	}
}

func unknownTypeDiagnostic(name string, start, end int) Diagnostic {
	return Diagnostic{
		Code:    "unknown-type",
		Message: fmt.Sprintf("unknown type %q", name),
		Start:   start,
		End:     end,
	}
}

func missingPropertyDiagnostic(field string, start, end int) Diagnostic {
	return Diagnostic{
		Code:    "missing-property",
		Message: fmt.Sprintf("missing property %q", field),
		Start:   start,
		End:     end,
	}
}

func missingModuleSummaryDiagnostic(request string, start, end int) Diagnostic {
	return Diagnostic{
		Code:    "missing-module-summary",
		Message: fmt.Sprintf("missing module summary for %q", request),
		Start:   start,
		End:     end,
	}
}

func staleModuleSummaryDiagnostic(summary ModuleSummary, dependency ModuleDependencySummary, start, end int) Diagnostic {
	name := summary.SourceName
	if name == "" {
		name = "<unknown>"
	}
	dependencyName := dependency.Key
	if dependencyName == "" {
		dependencyName = dependency.Path
	}
	return Diagnostic{
		Code:    "stale-module-summary",
		Message: fmt.Sprintf("stale module summary for %q: dependency %q changed", name, dependencyName),
		Start:   start,
		End:     end,
	}
}
