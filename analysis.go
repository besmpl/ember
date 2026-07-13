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

func analyzeSyntaxTree(source Source, tree syntaxTree, bind bindResult, mode SourceMode, env typeEnv, summaries moduleSummaryEnv) []Diagnostic {
	state := analysisState{
		source:          source,
		tree:            tree,
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
	statements, _ := tree.statementIDs()
	state.analyzeStatements(statements)
	return state.diagnostics
}

type analysisState struct {
	diagnostics     []Diagnostic
	source          Source
	tree            syntaxTree
	bind            bindResult
	mode            SourceMode
	typeEnv         typeEnv
	moduleSummaries moduleSummaryEnv
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
	value      typeID
}

type sourceRange struct {
	start int
	end   int
}

func annotationRange(tree syntaxTree, annotation typeID) sourceRange {
	if annotation == 0 {
		return sourceRange{}
	}
	nameIDs, _ := tree.typeNameIDs(annotation)
	start, end := tree.typeRange(annotation)
	if tree.typeKind(annotation) == typeKindName && len(nameIDs) != 0 {
		return sourceRange{start: start, end: start + syntaxStringsLength(tree, nameIDs, 1)}
	}
	return sourceRange{start: start, end: end}
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

func (a *analysisState) analyzeStatements(statements []statementID) {
	for _, statement := range statements {
		switch a.tree.statementKindID(statement) {
		case syntaxStatementLocal:
			stmt, _ := a.tree.localArena(statement)
			a.analyzeLocalStatement(stmt)
		case syntaxStatementTypeAlias:
			stmt, _ := a.tree.typeAliasArena(statement)
			a.analyzeTypeAliasStatement(stmt)
		case syntaxStatementAssign:
			stmt, _ := a.tree.assignmentArena(statement)
			a.analyzeAssignStatement(stmt)
		case syntaxStatementLocalFunction:
			stmt, _ := a.tree.localFunctionArena(statement)
			a.analyzeLocalFunctionStatement(stmt)
		case syntaxStatementFunctionDeclaration:
			function, _ := a.tree.functionDeclarationArena(statement)
			annotations := consumerStatementTypes(a.tree, function.paramAnnotations)
			variadicAnnotation, _ := a.tree.statementType(function.variadicAnnotation)
			returnAnnotation, _ := a.tree.statementType(function.returnAnnotation)
			body, _ := a.tree.statementChildren(function.statements)
			a.checkFunctionParameterTypeNames(annotations, variadicAnnotation)
			a.analyzeFunctionBody(returnAnnotation, body)
		case syntaxStatementCall:
			node, _ := a.tree.statementNode(statement)
			a.analyzeCallStatement(termID(node.payload))
		case syntaxStatementReturn:
			stmt, _ := a.tree.returnArena(statement)
			a.analyzeReturnStatement(stmt)
		case syntaxStatementIf:
			stmt, _ := a.tree.ifArena(statement)
			a.analyzeIfStatement(stmt)
		case syntaxStatementWhile:
			stmt, _ := a.tree.whileArena(statement)
			a.analyzeWhileStatement(stmt)
		case syntaxStatementFor:
			stmt, _ := a.tree.forArena(statement)
			a.analyzeNumericForStatement(stmt)
		case syntaxStatementGenericFor:
			stmt, _ := a.tree.genericForArena(statement)
			a.analyzeGenericForStatement(stmt)
		case syntaxStatementRepeat:
			stmt, _ := a.tree.repeatArena(statement)
			a.analyzeRepeatStatement(stmt)
		case syntaxStatementBlock:
			stmt, _ := a.tree.blockArena(statement)
			body, _ := a.tree.statementChildren(stmt.statements)
			a.analyzeScopedStatements(body)
		}
	}
}

func (a *analysisState) analyzeCallStatement(stmt termID) {
	call, ok := a.tree.termCall(stmt)
	if !ok {
		return
	}
	if fact, ok := a.functionFactForCall(call); ok {
		a.checkCallArguments(fact, call)
	}
	a.applyAssertRefinement(call)
}

func (a *analysisState) applyAssertRefinement(call arenaCallID) {
	target := termWithoutCastsAndGroups(a.tree, a.tree.callTarget(call))
	selectors, selOK := a.tree.termSelectors(target)
	args, argsOK := a.tree.callArgs(call)
	if !selOK || !argsOK || a.tree.termName(target) != "assert" || len(selectors) != 0 || len(args) == 0 {
		return
	}
	a.analyzeConditionExpression(args[0])
	refinements := a.trueConditionRefinements(a.tree, args[0])
	if len(refinements) != 0 {
		a.applyTrueRefinements(refinements)
		return
	}
	value, ok := expressionSingleTerm(a.tree, args[0])
	valueName := a.tree.termName(value)
	valueSelectors, valueSelOK := a.tree.termSelectors(value)
	if !ok || valueName == "" || !valueSelOK || len(valueSelectors) != 0 {
		return
	}
	narrowed := truthyType(a.lookupLocal(valueName))
	if narrowed == simpleTypeUnknown {
		return
	}
	a.defineLocal(valueName, narrowed)
}

func (a *analysisState) analyzeIfStatement(stmt arenaIfStatement) {
	condition := stmt.condition
	a.analyzeConditionExpression(condition)
	trueRefinements := a.trueConditionRefinements(a.tree, condition)

	a.pushScope()
	a.applyTrueRefinements(trueRefinements)
	thenStatements, _ := a.tree.statementChildren(stmt.thenStatements)
	a.analyzeStatements(thenStatements)
	thenFrame := a.popScope()

	a.pushScope()
	a.applyFalseConditionRefinements(a.tree, condition)
	elseStatements, _ := a.tree.statementChildren(stmt.elseStatements)
	a.analyzeStatements(elseStatements)
	elseFrame := a.popScope()

	a.applyBranchLocalFlowJoins(thenFrame.locals, elseFrame.locals)
	a.applyBranchTableFlowJoins(thenFrame.tables, elseFrame.tables)
}

func (a *analysisState) analyzeWhileStatement(stmt arenaWhileStatement) {
	condition := stmt.condition
	a.analyzeConditionExpression(condition)
	a.pushScope()
	a.applyTrueRefinements(a.trueConditionRefinements(a.tree, condition))
	body, _ := a.tree.statementChildren(stmt.statements)
	a.analyzeStatements(body)
	a.popScope()
}

func (a *analysisState) analyzeNumericForStatement(stmt arenaForStatement) {
	a.checkNumericForBound(stmt.start)
	a.checkNumericForBound(stmt.limit)
	if step := stmt.step; step != 0 {
		a.checkNumericForBound(step)
	}
	a.pushScope()
	name, _ := a.tree.stringValue(stmt.name)
	a.defineLocal(name, simpleTypeNumber)
	if symbol, ok := a.claimSymbol(stmt.nameID, symbolLocal); ok {
		a.symbolTypes[symbol.id] = simpleTypeNumber
	}
	body, _ := a.tree.statementChildren(stmt.statements)
	a.analyzeStatements(body)
	a.popScope()
}

func (a *analysisState) checkNumericForBound(expr expressionID) {
	actual := a.inferExpression(expr)
	constraint := typeConstraint{
		kind:     constraintAssignable,
		expected: simpleTypeNumber,
		actual:   actual,
		evidence: constraintEvidence{
			span: expressionRange(a.tree, expr),
		},
	}
	a.checkConstraint(constraint)
}

func (a *analysisState) analyzeGenericForStatement(stmt arenaGenericForStatement) {
	values, _ := a.tree.statementExpressions(stmt.values)
	for _, value := range values {
		a.inferExpression(value)
	}
	types := a.genericForValueTypes(stmt)
	a.pushScope()
	for i, name := range consumerStatementStrings(a.tree, stmt.names) {
		typ := simpleTypeUnknown
		if i < len(types) {
			typ = types[i]
		}
		a.defineLocal(name, typ)
		if symbol, ok := a.claimSymbol(syntaxNameID(stmt.nameID, i), symbolLocal); ok {
			a.symbolTypes[symbol.id] = typ
		}
	}
	body, _ := a.tree.statementChildren(stmt.statements)
	a.analyzeStatements(body)
	a.popScope()
}

func (a *analysisState) genericForValueTypes(stmt arenaGenericForStatement) []simpleType {
	values, _ := a.tree.statementExpressions(stmt.values)
	if types := a.nextTableForValueTypes(stmt); types != nil {
		return types
	}
	if len(values) != 1 {
		return nil
	}
	if value, ok := expressionSingleTerm(a.tree, values[0]); ok {
		selectors, _ := a.tree.termSelectors(value)
		if a.tree.termName(value) != "" && len(selectors) == 0 {
			if fact, ok := a.lookupTableFact(a.tree.termName(value)); ok {
				return tableIterationTypes(fact)
			}
		}
	}
	call, ok := expressionSingleCall(a.tree, values[0])
	if !ok {
		return nil
	}
	target := termWithoutCastsAndGroups(a.tree, a.tree.callTarget(call))
	targetSelectors, _ := a.tree.termSelectors(target)
	args, argsOK := a.tree.callArgs(call)
	if !argsOK || len(targetSelectors) != 0 || len(args) != 1 {
		return nil
	}
	arg, ok := expressionSingleTerm(a.tree, args[0])
	argSelectors, _ := a.tree.termSelectors(arg)
	if !ok || a.tree.termName(arg) == "" || len(argSelectors) != 0 {
		return nil
	}
	fact, ok := a.lookupTableFact(a.tree.termName(arg))
	if !ok {
		return nil
	}
	keyType := simpleTypeUnknown
	switch a.tree.termName(target) {
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

func (a *analysisState) nextTableForValueTypes(stmt arenaGenericForStatement) []simpleType {
	values, _ := a.tree.statementExpressions(stmt.values)
	if len(values) != 2 {
		return nil
	}
	iterator, ok := expressionSingleTerm(a.tree, values[0])
	iteratorSelectors, _ := a.tree.termSelectors(iterator)
	if !ok || a.tree.termName(iterator) != "next" || len(iteratorSelectors) != 0 {
		return nil
	}
	value, ok := expressionSingleTerm(a.tree, values[1])
	valueSelectors, _ := a.tree.termSelectors(value)
	if !ok || a.tree.termName(value) == "" || len(valueSelectors) != 0 {
		return nil
	}
	fact, ok := a.lookupTableFact(a.tree.termName(value))
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

func (a *analysisState) analyzeRepeatStatement(stmt arenaRepeatStatement) {
	a.pushScope()
	body, _ := a.tree.statementChildren(stmt.statements)
	a.analyzeStatements(body)
	condition := stmt.condition
	a.analyzeConditionExpression(condition)
	refinements := a.trueConditionRefinements(a.tree, condition)
	a.popScope()
	a.applyTrueRefinementsToExisting(refinements)
}

func (a *analysisState) analyzeConditionExpression(expr expressionID) {
	terms, ok := a.tree.expressionTerms(expr)
	if !ok {
		return
	}
	for _, term := range terms {
		comparisons, ok := a.tree.andTerms(term)
		if !ok {
			continue
		}
		for _, comparison := range comparisons {
			a.inferComparisonExpression(comparison)
		}
	}
}

func (a *analysisState) analyzeTypeAliasStatement(stmt arenaTypeAliasStatement) {
	if _, ok := a.claimSymbol(stmt.nameID, symbolTypeAlias); ok {
		a.defineTypeAlias(stmt)
	}
	a.checkUnknownTypeNames(stmt.value)
}

func (a *analysisState) analyzeScopedStatements(statements []statementID) {
	a.pushScope()
	a.analyzeStatements(statements)
	a.popScope()
}

func (a *analysisState) analyzeLocalFunctionStatement(stmt arenaFunctionStatement) {
	paramAnnotations := consumerStatementTypes(a.tree, stmt.paramAnnotations)
	variadicAnnotation := stmt.variadicAnnotation
	returnAnnotation := stmt.returnAnnotation
	typeParams := consumerStatementStrings(a.tree, stmt.typeParams)
	a.checkFunctionParameterTypeNames(paramAnnotations, variadicAnnotation)
	paramTypes := a.simpleTypesFromAnnotations(paramAnnotations)
	returnPack := a.returnPackFromAnnotation(returnAnnotation)
	fact := functionFact{
		typeParams:      append([]string(nil), typeParams...),
		params:          paramTypes,
		paramGenerics:   genericAnnotationNames(a.tree, paramAnnotations, typeParams),
		variadic:        a.simpleTypeFromAnnotation(variadicAnnotation),
		variadicGeneric: genericAnnotationName(a.tree, variadicAnnotation, typeParams),
		returnType:      returnPack.firstType(a.simpleTypeFromAnnotation(returnAnnotation)),
		returnTable:     returnPack.firstTable(a.tableFactFromAnnotation(returnAnnotation)),
		returnSpan:      annotationRange(a.tree, returnAnnotation),
		returnPack:      returnPack,
		returnGeneric:   genericAnnotationName(a.tree, returnAnnotation, typeParams),
	}
	if symbol, ok := a.claimSymbol(stmt.nameID, symbolLocalFunction); ok {
		a.functions[symbol.id] = fact
	}
	restore := a.bindLocals(consumerStatementStrings(a.tree, stmt.params), stmt.paramID, paramTypes)
	body, _ := a.tree.statementChildren(stmt.statements)
	a.analyzeFunctionBody(returnAnnotation, body)
	restore()
}

func (a *analysisState) analyzeFunctionBody(returnAnnotation typeID, statements []statementID) {
	a.checkUnknownTypeNames(returnAnnotation)
	returnPack := a.returnPackFromAnnotation(returnAnnotation)
	a.analyzeFunctionBodyWithReturn(
		returnPack.firstType(a.simpleTypeFromAnnotation(returnAnnotation)),
		returnPack.firstTable(a.tableFactFromAnnotation(returnAnnotation)),
		annotationRange(a.tree, returnAnnotation),
		returnPack,
		statements,
	)
}

func (a *analysisState) analyzeFunctionBodyWithReturn(returnType simpleType, returnTable tableFact, returnSpan sourceRange, returnPack returnPackFact, statements []statementID) {
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

func (a *analysisState) bindLocals(names []string, nameID syntaxID, types []simpleType) func() {
	previous := make(map[string]simpleType, len(names))
	hadPrevious := make(map[string]bool, len(names))
	for i, name := range names {
		previous[name], hadPrevious[name] = a.currentScope()[name]
		if i < len(types) {
			typ := types[i]
			a.currentScope()[name] = typ
			if symbol, ok := a.claimSymbol(syntaxNameID(nameID, i), symbolParameter); ok {
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

func (a *analysisState) bindLocalIDs(ids []stringID, nameID syntaxID, types []simpleType) func() {
	previous := make(map[string]simpleType, len(ids))
	hadPrevious := make(map[string]bool, len(ids))
	for i, id := range ids {
		name, ok := a.tree.stringValue(id)
		if !ok {
			continue
		}
		previous[name], hadPrevious[name] = a.currentScope()[name]
		if i < len(types) {
			typ := types[i]
			a.currentScope()[name] = typ
			if symbol, ok := a.claimSymbol(syntaxNameID(nameID, i), symbolParameter); ok {
				a.symbolTypes[symbol.id] = typ
			}
		}
	}
	return func() {
		for _, id := range ids {
			name, ok := a.tree.stringValue(id)
			if !ok {
				continue
			}
			if hadPrevious[name] {
				a.currentScope()[name] = previous[name]
			} else {
				delete(a.currentScope(), name)
			}
		}
	}
}

func (a *analysisState) analyzeLocalStatement(stmt arenaLocalStatement) {
	names := consumerStatementStrings(a.tree, stmt.names)
	annotations := consumerStatementTypes(a.tree, stmt.annotations)
	values, _ := a.tree.statementExpressions(stmt.values)
	nameID := stmt.nameID
	for i, name := range names {
		var expected simpleType
		var annotation typeID
		expectedTable := tableFact{}
		actualTable := tableFact{}
		var functionFact functionFact
		hasFunctionFact := false
		var moduleSummary ModuleSummary
		hasModuleSummary := false
		if i < len(annotations) {
			annotation = annotations[i]
			a.checkUnknownTypeNames(annotation)
			expected = a.simpleTypeFromAnnotation(annotation)
			expectedTable = a.tableFactFromAnnotation(annotation)
			functionFact, hasFunctionFact = a.functionFactFromAnnotation(annotation)
		}
		actual := simpleTypeUnknown
		if i < len(values) {
			moduleSummary, hasModuleSummary = a.moduleSummaryFromExpression(values[i])
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
				actual = a.inferExpression(values[i])
				if !expressionIsTableLiteral(a.tree, values[i]) {
					actualTable = a.tableFactFromExpression(values[i])
				}
			}
			if !hasFunctionFact {
				a.analyzeFunctionExpressionAnnotations(values[i])
			}
			a.analyzeAnnotatedExpression(annotation, values[i])
		}
		selected := selectedLocalType(expected, actual)
		a.defineLocal(name, selected)
		if hasModuleSummary {
			a.defineModuleLocal(name, moduleSummary)
		}
		if symbol, ok := a.claimSymbol(syntaxNameID(nameID, i), symbolLocal); ok {
			a.symbolTypes[symbol.id] = selected
			if !hasFunctionFact && i < len(values) {
				functionFact, hasFunctionFact = a.functionFactFromExpression(values[i])
			}
			if hasFunctionFact {
				a.functions[symbol.id] = functionFact
			}
		}
		tableFact := expectedTable
		if tableFact.empty() && !actualTable.empty() {
			tableFact = actualTable
		}
		if tableFact.empty() && i < len(values) {
			tableFact = a.tableFactFromLiteral(values[i])
		}
		a.defineTableLocal(name, tableFact)
		if expected == simpleTypeUnknown {
			continue
		}
		if i >= len(values) {
			constraint := typeConstraint{
				kind:     constraintAssignable,
				expected: expected,
				actual:   simpleTypeNil,
				evidence: constraintEvidence{
					span: localNameRange(a.tree, stmt, i),
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
				span: expressionRange(a.tree, values[i]),
			},
		}
		a.checkConstraint(constraint)
	}
}

func (a *analysisState) moduleReturnFactFromExpression(expr expressionID) (simpleType, tableFact, bool) {
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

func (a *analysisState) moduleSummaryFromExpression(expr expressionID) (ModuleSummary, bool) {
	call, ok := expressionSingleCall(a.tree, expr)
	if !ok {
		return ModuleSummary{}, false
	}
	request, ok := requireCallRequest(a.tree, call)
	if !ok {
		return ModuleSummary{}, false
	}
	summary, ok := a.moduleSummaries.summaryForRequire(a.source, request)
	args, argsOK := a.tree.callArgs(call)
	if !argsOK {
		return summary, ok
	}
	if !ok && a.moduleSummaries.active() && len(args) != 0 {
		span := expressionRange(a.tree, args[0])
		a.diagnostics = append(a.diagnostics, missingModuleSummaryDiagnostic(request, span.start, span.end))
	}
	if ok {
		if dependency, stale := a.moduleSummaries.staleDependency(summary); stale {
			span := expressionRange(a.tree, args[0])
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

func (a *analysisState) tableFactFromLiteral(value expressionID) tableFact {
	tree := a.tree
	tableTerm, ok := expressionSingleTerm(a.tree, value)
	table, tableOK := tree.termTable(tableTerm)
	if !ok || !tableOK {
		return tableFact{}
	}
	fieldFacts := make(map[string]simpleType)
	var indexers []tableIndexerFact
	fields, fieldsOK := tree.tableFields(table)
	if !fieldsOK {
		return tableFact{}
	}
	for _, field := range fields {
		name := tree.tableFieldName(field)
		key := tree.tableFieldKey(field)
		valueExpr := tree.tableFieldValue(field)
		if name == "" && key == 0 && tree.tableFieldArrayIndex(field) > 0 {
			typ := a.inferExpression(valueExpr)
			if typ != simpleTypeUnknown {
				indexers = mergeTableIndexerFact(indexers, simpleTypeNumber, typ)
			}
			continue
		}
		if name == "" && key != 0 {
			keyType := a.inferExpression(key)
			typ := a.inferExpression(valueExpr)
			if keyType != simpleTypeUnknown && typ != simpleTypeUnknown {
				indexers = mergeTableIndexerFact(indexers, keyType, typ)
			}
			continue
		}
		if name == "" {
			continue
		}
		typ := a.inferExpression(valueExpr)
		if typ == simpleTypeUnknown {
			continue
		}
		fieldFacts[name] = typ
	}
	return tableFact{known: true, fields: fieldFacts, indexers: indexers}
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

func (a *analysisState) tableFactFromExpression(value expressionID) tableFact {
	tree := a.tree
	valueTerm, ok := expressionSingleTerm(a.tree, value)
	if !ok {
		return tableFact{}
	}
	if _, ok := tree.termTable(valueTerm); ok {
		return a.tableFactFromLiteral(value)
	}
	if call, ok := tree.termCall(valueTerm); ok {
		fact, ok := a.functionFactForCallQuiet(call)
		if !ok {
			return tableFact{}
		}
		return fact.returnTable
	}
	selectors, _ := tree.termSelectors(valueTerm)
	if tree.termName(valueTerm) == "" || len(selectors) != 0 {
		return tableFact{}
	}
	fact, ok := a.lookupTableFact(tree.termName(valueTerm))
	if !ok {
		return tableFact{}
	}
	return fact
}

func expressionIsTableLiteral(tree syntaxTree, value expressionID) bool {
	valueTerm, ok := expressionSingleTerm(tree, value)
	_, tableOK := tree.termTable(valueTerm)
	return ok && tableOK
}

func localNameRange(tree syntaxTree, stmt arenaLocalStatement, index int) sourceRange {
	ranges, _ := tree.statementRanges(stmt.nameRanges)
	if index >= 0 && index < len(ranges) {
		return ranges[index]
	}
	return sourceRange{}
}

func (a *analysisState) analyzeAnnotatedExpression(annotation typeID, value expressionID) {
	a.analyzeAnnotatedFunctionExpression(annotation, value)
	expectedFact := a.tableFactFromAnnotation(annotation)
	if expectedFact.empty() {
		return
	}
	tableTerm, ok := expressionSingleTerm(a.tree, value)
	table, tableOK := a.tree.termTable(tableTerm)
	if !ok || !tableOK {
		return
	}
	expectedFields := expectedFact.fields
	actualFact := a.tableFactFromLiteral(value)
	fields, fieldsOK := a.tree.tableFields(table)
	if !fieldsOK {
		return
	}
	presentFields := make(map[string]bool, len(fields))
	for _, field := range fields {
		fieldName := a.tree.tableFieldName(field)
		fieldKey := a.tree.tableFieldKey(field)
		fieldValue := a.tree.tableFieldValue(field)
		if fieldName != "" {
			presentFields[fieldName] = true
		}
		if fieldName == "" && fieldKey == 0 && a.tree.tableFieldArrayIndex(field) > 0 {
			indexer, ok := tableIndexerForKeyType(expectedFact, simpleTypeNumber)
			if !ok || indexer.value == simpleTypeUnknown {
				continue
			}
			actual := a.inferExpression(fieldValue)
			constraint := typeConstraint{
				kind:     constraintAssignable,
				expected: indexer.value,
				actual:   actual,
				evidence: constraintEvidence{
					span: expressionRange(a.tree, fieldValue),
				},
			}
			a.checkConstraint(constraint)
			continue
		}
		if fieldName == "" && fieldKey != 0 {
			key := a.inferExpression(fieldKey)
			indexer, ok := a.tableIndexerForKey(expectedFact, key, expressionRange(a.tree, fieldKey))
			if !ok || indexer.value == simpleTypeUnknown {
				continue
			}
			actual := a.inferExpression(fieldValue)
			constraint := typeConstraint{
				kind:     constraintAssignable,
				expected: indexer.value,
				actual:   actual,
				evidence: constraintEvidence{
					span: expressionRange(a.tree, fieldValue),
				},
			}
			a.checkConstraint(constraint)
			continue
		}
		expected := expectedFields[fieldName]
		if fieldName != "" && expected == simpleTypeUnknown {
			if indexer, ok := tableIndexerForKeyType(expectedFact, simpleTypeString); ok {
				expected = indexer.value
			}
		}
		if fieldName == "" || expected == simpleTypeUnknown {
			continue
		}
		actual := a.inferExpression(fieldValue)
		constraint := typeConstraint{
			kind:     constraintAssignable,
			expected: expected,
			actual:   actual,
			evidence: constraintEvidence{
				span: expressionRange(a.tree, fieldValue),
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
			span := expressionRange(a.tree, value)
			a.diagnostics = append(a.diagnostics, typeMismatchDiagnostic(expected, actualIndexer.value, span.start, span.end))
			continue
		}
		span := expressionRange(a.tree, value)
		a.diagnostics = append(a.diagnostics, missingPropertyDiagnostic(field, span.start, span.end))
	}
}

func (a *analysisState) analyzeAnnotatedFunctionExpression(annotation typeID, value expressionID) {
	fact, ok := a.functionFactFromAnnotation(annotation)
	if !ok {
		return
	}
	functionTerm, ok := expressionSingleTerm(a.tree, value)
	function, functionOK := a.tree.termFunction(functionTerm)
	if !ok || !functionOK {
		return
	}
	restore := a.bindLocalIDs(functionExpressionParamNameIDs(a.tree, function), a.tree.functionExpressionParamID(function), functionExpressionParamTypes(a.tree, function, fact))
	span, _ := a.tree.functionExpressionStatementIDs(function)
	body, _ := a.tree.statementChildren(span)
	a.analyzeFunctionBodyWithReturn(fact.returnType, fact.returnTable, fact.returnSpan, fact.returnPack, body)
	restore()
}

func (a *analysisState) analyzeFunctionExpressionAnnotations(value expressionID) {
	function, ok := functionExpressionFromExpression(a.tree, value)
	if !ok || !functionExpressionHasAnnotations(a.tree, function) {
		return
	}
	a.checkFunctionParameterTypeNames(functionExpressionParamAnnotations(a.tree, function), a.tree.functionExpressionVariadicAnnotation(function))
	a.checkUnknownTypeNames(a.tree.functionExpressionReturnAnnotation(function))
	fact := a.functionFactFromFunctionExpression(function)
	restore := a.bindLocalIDs(functionExpressionParamNameIDs(a.tree, function), a.tree.functionExpressionParamID(function), functionExpressionParamTypes(a.tree, function, fact))
	span, _ := a.tree.functionExpressionStatementIDs(function)
	body, _ := a.tree.statementChildren(span)
	a.analyzeFunctionBodyWithReturn(fact.returnType, fact.returnTable, fact.returnSpan, fact.returnPack, body)
	restore()
}

func (a *analysisState) functionFactFromExpression(value expressionID) (functionFact, bool) {
	function, ok := functionExpressionFromExpression(a.tree, value)
	if !ok {
		return functionFact{}, false
	}
	return a.functionFactFromFunctionExpression(function), true
}

func functionExpressionFromExpression(tree syntaxTree, value expressionID) (arenaFunctionID, bool) {
	functionTerm, ok := expressionSingleTerm(tree, value)
	function, functionOK := tree.termFunction(functionTerm)
	if !ok || !functionOK {
		return 0, false
	}
	return function, true
}

func functionExpressionHasAnnotations(tree syntaxTree, function arenaFunctionID) bool {
	if tree.functionExpressionReturnAnnotation(function) != 0 || tree.functionExpressionVariadicAnnotation(function) != 0 {
		return true
	}
	for _, annotation := range functionExpressionParamAnnotations(tree, function) {
		if annotation != 0 {
			return true
		}
	}
	return false
}

func (a *analysisState) functionFactFromFunctionExpression(function arenaFunctionID) functionFact {
	returnAnnotation := a.tree.functionExpressionReturnAnnotation(function)
	paramAnnotations := functionExpressionParamAnnotations(a.tree, function)
	typeParamSpan, _ := a.tree.functionExpressionTypeParamIDs(function)
	typeParamIDs, _ := a.tree.statementStrings(typeParamSpan)
	typeParams := syntaxStrings(a.tree, typeParamIDs)
	returnPack := a.returnPackFromAnnotation(returnAnnotation)
	return functionFact{
		typeParams:      typeParams,
		params:          a.simpleTypesFromAnnotations(paramAnnotations),
		paramGenerics:   genericAnnotationNames(a.tree, paramAnnotations, typeParams),
		variadic:        a.simpleTypeFromAnnotation(a.tree.functionExpressionVariadicAnnotation(function)),
		variadicGeneric: genericAnnotationName(a.tree, a.tree.functionExpressionVariadicAnnotation(function), typeParams),
		returnType:      returnPack.firstType(a.simpleTypeFromAnnotation(returnAnnotation)),
		returnTable:     returnPack.firstTable(a.tableFactFromAnnotation(returnAnnotation)),
		returnSpan:      annotationRange(a.tree, returnAnnotation),
		returnPack:      returnPack,
		returnGeneric:   genericAnnotationName(a.tree, returnAnnotation, typeParams),
	}
}

func functionExpressionParamTypes(tree syntaxTree, function arenaFunctionID, fact functionFact) []simpleType {
	params := functionExpressionParamNameIDs(tree, function)
	types := make([]simpleType, len(params))
	for i := range params {
		if i < len(fact.params) {
			types[i] = fact.params[i]
			continue
		}
		types[i] = fact.variadic
	}
	return types
}

func functionExpressionParamNameIDs(tree syntaxTree, function arenaFunctionID) []stringID {
	span, ok := tree.functionExpressionParamIDs(function)
	if !ok {
		return nil
	}
	ids, _ := tree.statementStrings(span)
	return ids
}

func functionExpressionParamAnnotations(tree syntaxTree, function arenaFunctionID) []typeID {
	span, ok := tree.functionExpressionParamAnnotationIDs(function)
	if !ok {
		return nil
	}
	ids, _ := tree.statementTypes(span)
	return ids
}

func (a *analysisState) returnPackFromAnnotation(annotation typeID) returnPackFact {
	return a.returnPackFromAnnotationWith(annotation, nil)
}

func (a *analysisState) returnPackFromAnnotationWith(annotation typeID, substitutions map[string]simpleType) returnPackFact {
	tree := a.tree
	if annotation == 0 {
		return returnPackFact{}
	}
	params := tree.typeParams(annotation)
	_, hasReturn := tree.typeReturn(annotation)
	if tree.typeKind(annotation) == typeKindFunction && !hasReturn {
		pack := returnPackFact{
			known:  true,
			types:  make([]simpleType, 0, len(params)),
			tables: make([]tableFact, 0, len(params)),
			spans:  make([]sourceRange, 0, len(params)),
		}
		for _, param := range params {
			value := tree.typeParamValue(param)
			if value != 0 && tree.typeKind(value) == typeKindVariadic {
				value, _ = tree.typeInner(value)
			}
			pack.types = append(pack.types, a.simpleTypeFromAnnotationWith(value, substitutions))
			pack.tables = append(pack.tables, a.tableFactFromAnnotation(value))
			pack.spans = append(pack.spans, annotationRange(a.tree, value))
		}
		return pack
	}
	return returnPackFact{
		known:  true,
		types:  []simpleType{a.simpleTypeFromAnnotationWith(annotation, substitutions)},
		tables: []tableFact{a.tableFactFromAnnotation(annotation)},
		spans:  []sourceRange{annotationRange(a.tree, annotation)},
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

func (a *analysisState) analyzeReturnStatement(stmt arenaReturnStatement) {
	values, _ := a.tree.statementExpressions(stmt.values)
	start, end := stmt.start, stmt.end
	actual := simpleTypeNil
	actualTable := tableFact{}
	span := sourceRange{start: start, end: end}
	if len(values) != 0 {
		actual = a.inferExpression(values[0])
		actualTable = a.tableFactFromExpression(values[0])
		span = expressionRange(a.tree, values[0])
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

func (a *analysisState) checkAdditionalReturnPackValues(stmt arenaReturnStatement) {
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
		span := sourceRange{start: stmt.start, end: stmt.start + len("return")}
		values, _ := a.tree.statementExpressions(stmt.values)
		if i < len(values) {
			actual = a.inferExpression(values[i])
			span = expressionRange(a.tree, values[i])
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

func (a *analysisState) checkImplicitNilReturn(statements []statementID) {
	if len(a.returns) == 0 || statementsDefinitelyReturn(a.tree, statements) {
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

func statementsDefinitelyReturn(tree syntaxTree, statements []statementID) bool {
	for _, statement := range statements {
		if statementDefinitelyReturns(tree, statement) {
			return true
		}
	}
	return false
}

func statementDefinitelyReturns(tree syntaxTree, statement statementID) bool {
	switch tree.statementKindID(statement) {
	case syntaxStatementReturn:
		return true
	case syntaxStatementIf:
		ifStmt, _ := tree.ifArena(statement)
		thenStatements, _ := tree.statementChildren(ifStmt.thenStatements)
		elseStatements, _ := tree.statementChildren(ifStmt.elseStatements)
		return statementsDefinitelyReturn(tree, thenStatements) && statementsDefinitelyReturn(tree, elseStatements)
	case syntaxStatementBlock:
		block, _ := tree.blockArena(statement)
		body, _ := tree.statementChildren(block.statements)
		return statementsDefinitelyReturn(tree, body)
	case syntaxStatementRepeat:
		repeat, _ := tree.repeatArena(statement)
		body, _ := tree.statementChildren(repeat.statements)
		return statementsDefinitelyReturn(tree, body)
	default:
		return false
	}
}

func (a *analysisState) analyzeAssignStatement(stmt arenaAssignStatement) {
	targets, _ := a.tree.statementTargets(stmt.targets)
	values, _ := a.tree.statementExpressions(stmt.values)
	for i, targetID := range targets {
		target, ok := a.tree.statementArenaTarget(targetID)
		if !ok {
			continue
		}
		if i >= len(values) {
			a.analyzeMissingAssignValue(target)
			continue
		}
		selectors, _ := a.tree.selectorSpan(target.selectors)
		if len(selectors) != 0 {
			a.analyzeFieldAssign(target, values[i])
			continue
		}
		expected := a.lookupAssignTarget(target)
		if expected == simpleTypeUnknown {
			continue
		}
		actual := a.inferExpression(values[i])
		expectedTable := a.tableFactFromAssignTarget(target)
		actualTable := a.tableFactFromExpression(values[i])
		constraint := typeConstraint{
			kind:          constraintAssignable,
			expected:      expected,
			actual:        actual,
			expectedTable: expectedTable,
			actualTable:   actualTable,
			evidence: constraintEvidence{
				span: expressionRange(a.tree, values[i]),
			},
		}
		if a.checkConstraint(constraint) {
			name, _ := a.tree.stringValue(target.name)
			a.applyAssignmentRefinement(name, actual)
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

func (a *analysisState) analyzeMissingAssignValue(target arenaAssignTarget) {
	selectors, _ := a.tree.selectorSpan(target.selectors)
	if len(selectors) != 0 {
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

func (a *analysisState) analyzeFieldAssign(target arenaAssignTarget, value expressionID) {
	name, _ := a.tree.stringValue(target.name)
	selectors, _ := a.tree.selectorSpan(target.selectors)
	start, end := target.start, target.end
	if name == "" || len(selectors) != 1 {
		return
	}
	selector := selectors[0]
	fact, ok := a.lookupTableFact(name)
	if !ok {
		return
	}
	if index := a.tree.termSelectorIndex(selector); index != 0 {
		key := a.inferExpression(index)
		indexer, ok := a.tableIndexerForKey(fact, key, expressionRange(a.tree, index))
		if !ok || indexer.value == simpleTypeUnknown {
			return
		}
		if indexer.access == "read" {
			a.diagnostics = append(a.diagnostics, readonlyPropertyDiagnostic(
				"indexer",
				start,
				end,
			))
			return
		}
		actual := a.inferExpression(value)
		constraint := typeConstraint{
			kind:     constraintIndexWrite,
			expected: indexer.value,
			actual:   actual,
			evidence: constraintEvidence{
				span: expressionRange(a.tree, value),
			},
		}
		a.checkConstraint(constraint)
		return
	}
	field := a.tree.termSelectorField(selector)
	if field == "" {
		return
	}
	expected, exists := fact.fields[field]
	if !exists {
		if indexer, ok := tableIndexerForKeyType(fact, simpleTypeString); ok {
			if indexer.access == "read" {
				a.diagnostics = append(a.diagnostics, readonlyPropertyDiagnostic(
					"indexer",
					start,
					end,
				))
				return
			}
			actual := a.inferExpression(value)
			constraint := typeConstraint{
				kind:     constraintIndexWrite,
				expected: indexer.value,
				actual:   actual,
				evidence: constraintEvidence{
					span: expressionRange(a.tree, value),
				},
			}
			a.checkConstraint(constraint)
			return
		}
		constraint := typeConstraint{
			kind:  constraintIndexWrite,
			field: field,
			evidence: constraintEvidence{
				span: sourceRange{start: start, end: end},
			},
		}
		a.checkConstraint(constraint)
		return
	}
	if expected == simpleTypeUnknown {
		return
	}
	if fact.access[field] == "read" {
		a.diagnostics = append(a.diagnostics, readonlyPropertyDiagnostic(
			field,
			start,
			end,
		))
		return
	}
	actual := a.inferExpression(value)
	constraint := typeConstraint{
		kind:     constraintIndexWrite,
		expected: expected,
		actual:   actual,
		evidence: constraintEvidence{
			span: expressionRange(a.tree, value),
		},
	}
	if a.checkConstraint(constraint) {
		a.applyTableFieldAssignmentRefinement(name, field, actual)
	}
}

func (a *analysisState) applyTableFieldAssignmentRefinement(name string, field string, typ simpleType) {
	if typ == simpleTypeUnknown {
		return
	}
	a.defineTableField(name, field, typ)
}

func (a *analysisState) lookupAssignTarget(target arenaAssignTarget) simpleType {
	name, _ := a.tree.stringValue(target.name)
	if use, ok := a.bind.use(target.id); ok {
		if typ, ok := a.symbolTypes[use.symbol]; ok {
			return typ
		}
	}
	return a.lookupLocal(name)
}

func (a *analysisState) tableFactFromAssignTarget(target arenaAssignTarget) tableFact {
	name, _ := a.tree.stringValue(target.name)
	selectors, _ := a.tree.selectorSpan(target.selectors)
	if name == "" || len(selectors) != 0 {
		return tableFact{}
	}
	fact, ok := a.lookupTableFact(name)
	if !ok {
		return tableFact{}
	}
	return fact
}

func (a *analysisState) lookupNamedTerm(value termID) simpleType {
	return a.lookupBoundName(a.tree.termID(value), a.tree.termName(value))
}

func (a *analysisState) checkUnknownName(node syntaxID, name string, start int, end int) {
	if !policyForMode(a.mode).reportsUnknownNames() || name == "" || a.isKnownGlobalName(name) {
		return
	}
	if _, ok := a.bind.use(node); ok {
		return
	}
	a.diagnostics = append(a.diagnostics, unknownNameDiagnostic(name, start, end))
}

func (a *analysisState) isKnownGlobalName(name string) bool {
	_, ok := a.typeEnv.lookup(name)
	return ok
}

func (a *analysisState) lookupBoundName(node syntaxID, name string) simpleType {
	if use, ok := a.bind.use(node); ok {
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

func (a *analysisState) claimSymbol(node syntaxID, kind symbolKind) (boundSymbol, bool) {
	symbol, ok := a.bind.definition(node)
	return symbol, ok && symbol.kind == kind
}

func selectedLocalType(annotation, value simpleType) simpleType {
	if annotation != simpleTypeUnknown {
		return annotation
	}
	return value
}

func (a *analysisState) inferExpression(expr expressionID) simpleType {
	terms, ok := a.tree.expressionTerms(expr)
	if !ok || len(terms) == 0 {
		return simpleTypeUnknown
	}
	result := a.inferAndExpression(terms[0])
	resultTruthy, resultTruthKnown := andExpressionKnownTruthiness(a.tree, terms[0])
	for _, term := range terms[1:] {
		right := a.inferAndExpression(term)
		if resultTruthKnown {
			if resultTruthy {
				continue
			}
			result = right
			resultTruthy, resultTruthKnown = andExpressionKnownTruthiness(a.tree, term)
			continue
		}
		result = unionSimpleTypes(truthyType(result), right)
		resultTruthKnown = false
	}
	return result
}

func (a *analysisState) inferAndExpression(expr andExpressionID) simpleType {
	terms, ok := a.tree.andTerms(expr)
	if !ok || len(terms) == 0 {
		return simpleTypeUnknown
	}
	result := a.inferComparisonExpression(terms[0])
	resultTruthy, resultTruthKnown := comparisonKnownTruthiness(a.tree, terms[0])
	for _, term := range terms[1:] {
		right := a.inferComparisonExpression(term)
		if resultTruthKnown {
			if resultTruthy {
				result = right
				resultTruthy, resultTruthKnown = comparisonKnownTruthiness(a.tree, term)
			}
			continue
		}
		if truthy, known := simpleTypeKnownTruthiness(result); known {
			if truthy {
				result = right
				resultTruthy, resultTruthKnown = comparisonKnownTruthiness(a.tree, term)
			}
			continue
		}
		result = unionSimpleTypes(falseyType(result), right)
		resultTruthKnown = false
	}
	return result
}

func (a *analysisState) inferComparisonExpression(expr comparisonExpressionID) simpleType {
	leftExpr := a.tree.comparisonLeft(expr)
	operator := a.tree.comparisonOperator(expr)
	rightExpr := a.tree.comparisonRight(expr)
	left := a.inferConcatExpression(leftExpr)
	if operator == "" || rightExpr == 0 {
		return left
	}
	right := a.inferConcatExpression(rightExpr)
	if isOrderedComparison(operator) {
		a.checkComparisonOperand(string(operator), left, right, concatExpressionRange(a.tree, rightExpr))
	}
	return simpleTypeBoolean
}

func andExpressionKnownTruthiness(tree syntaxTree, expr andExpressionID) (bool, bool) {
	terms, ok := tree.andTerms(expr)
	if !ok || len(terms) != 1 {
		return false, false
	}
	return comparisonKnownTruthiness(tree, terms[0])
}

func comparisonKnownTruthiness(tree syntaxTree, expr comparisonExpressionID) (bool, bool) {
	if tree.comparisonOperator(expr) != "" || tree.comparisonRight(expr) != 0 {
		return false, false
	}
	return concatExpressionKnownTruthiness(tree, tree.comparisonLeft(expr))
}

func concatExpressionKnownTruthiness(tree syntaxTree, expr concatExpressionID) (bool, bool) {
	first := tree.concatFirst(expr)
	firstFirst := tree.additiveFirst(first)
	concatRest, _ := tree.concatRest(expr)
	addRest, _ := tree.additiveRest(first)
	mulRest, _ := tree.multiplicativeRest(firstFirst)
	if len(concatRest) != 0 || len(addRest) != 0 || len(mulRest) != 0 {
		return false, false
	}
	value := termWithoutCastsAndGroups(tree, tree.multiplicativeFirst(firstFirst))
	literal, ok := tree.termLiteral(value)
	if !ok {
		if typ := simpleTypeFromTerm(tree, value); typ != simpleTypeUnknown {
			return simpleTypeKnownTruthiness(typ)
		}
		return false, false
	}
	switch literal.Kind() {
	case NilKind:
		return false, true
	case BoolKind:
		boolean, ok := literal.Bool()
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

func (a *analysisState) inferConcatExpression(expr concatExpressionID) simpleType {
	first := a.tree.concatFirst(expr)
	result := a.inferAdditiveExpression(first)
	rest, _ := a.tree.concatRest(expr)
	if len(rest) == 0 {
		return result
	}
	a.checkConcatOperand(result, additiveExpressionRange(a.tree, first))
	for _, part := range rest {
		value := a.inferAdditiveExpression(part)
		a.checkConcatOperand(value, additiveExpressionRange(a.tree, part))
	}
	return simpleTypeString
}

func (a *analysisState) inferAdditiveExpression(expr additiveExpressionID) simpleType {
	first := a.tree.additiveFirst(expr)
	rest, _ := a.tree.additiveRest(expr)
	result := a.inferMultiplicativeExpression(first)
	if len(rest) == 0 {
		return result
	}
	a.checkBinaryOperand(string(a.tree.additivePartOperator(rest[0])), result, multiplicativeExpressionRange(a.tree, first))
	for _, part := range rest {
		valueExpr := a.tree.additivePartValue(part)
		value := a.inferMultiplicativeExpression(valueExpr)
		a.checkBinaryOperand(string(a.tree.additivePartOperator(part)), value, multiplicativeExpressionRange(a.tree, valueExpr))
	}
	return simpleTypeNumber
}

func (a *analysisState) inferMultiplicativeExpression(expr multiplicativeExpressionID) simpleType {
	first := a.tree.multiplicativeFirst(expr)
	rest, _ := a.tree.multiplicativeRest(expr)
	result := a.inferTerm(first)
	if len(rest) == 0 {
		return result
	}
	a.checkBinaryOperand(string(a.tree.multiplicativePartOperator(rest[0])), result, termRange(a.tree, first))
	for _, part := range rest {
		valueExpr := a.tree.multiplicativePartValue(part)
		value := a.inferTerm(valueExpr)
		a.checkBinaryOperand(string(a.tree.multiplicativePartOperator(part)), value, termRange(a.tree, valueExpr))
	}
	return simpleTypeNumber
}

func (a *analysisState) inferTerm(value termID) simpleType {
	cast, hasCast := a.tree.termCast(value)
	if hasCast {
		if name := a.tree.termName(value); name != "" {
			_ = a.lookupNamedTerm(value)
		}
		return a.simpleTypeFromAnnotation(cast)
	}
	if fieldType := a.inferFieldRead(value); fieldType != simpleTypeUnknown {
		return fieldType
	}
	if a.tree.termKind(value) == syntaxTermUnaryNot {
		unary, _ := a.tree.termChild(value)
		a.inferTerm(unary)
		return simpleTypeBoolean
	}
	if a.tree.termKind(value) == syntaxTermUnaryMinus {
		unary, _ := a.tree.termChild(value)
		actual := a.inferTerm(unary)
		constraint := typeConstraint{
			kind:     constraintUnaryOp,
			operator: "-",
			expected: simpleTypeNumber,
			actual:   actual,
			evidence: constraintEvidence{
				span: termRange(a.tree, unary),
			},
		}
		a.checkConstraint(constraint)
		return simpleTypeNumber
	}
	if a.tree.termKind(value) == syntaxTermUnaryLength {
		unary, _ := a.tree.termChild(value)
		actual := a.inferTerm(unary)
		constraint := typeConstraint{
			kind:     constraintUnaryOp,
			operator: "#",
			expected: simpleType("string|table"),
			actual:   actual,
			evidence: constraintEvidence{
				span: termRange(a.tree, unary),
			},
		}
		a.checkConstraint(constraint)
		return simpleTypeNumber
	}
	selectors, _ := a.tree.termSelectors(value)
	name := a.tree.termName(value)
	start, _ := a.tree.termRange(value)
	if len(selectors) != 0 {
		a.checkUnknownName(a.tree.termID(value), name, start, start+len(name))
		return simpleTypeUnknown
	}
	if name != "" {
		typ := a.lookupNamedTerm(value)
		if typ == simpleTypeUnknown {
			a.checkUnknownName(a.tree.termID(value), name, start, start+len(name))
		}
		return typ
	}
	call, ok := a.tree.termCall(value)
	if !ok {
		return simpleTypeFromTerm(a.tree, value)
	}
	if fact, ok := a.functionFactForCall(call); ok {
		return a.checkCallArguments(fact, call)
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

func expressionRange(tree syntaxTree, expr expressionID) sourceRange {
	terms, ok := tree.expressionTerms(expr)
	if !ok || len(terms) == 0 {
		return sourceRange{}
	}
	span := andExpressionRange(tree, terms[0])
	for _, term := range terms[1:] {
		span.end = andExpressionRange(tree, term).end
	}
	return span
}

func andExpressionRange(tree syntaxTree, expr andExpressionID) sourceRange {
	terms, ok := tree.andTerms(expr)
	if !ok || len(terms) == 0 {
		return sourceRange{}
	}
	span := comparisonExpressionRange(tree, terms[0])
	for _, term := range terms[1:] {
		span.end = comparisonExpressionRange(tree, term).end
	}
	return span
}

func comparisonExpressionRange(tree syntaxTree, expr comparisonExpressionID) sourceRange {
	span := concatExpressionRange(tree, tree.comparisonLeft(expr))
	if right := tree.comparisonRight(expr); right != 0 {
		span.end = concatExpressionRange(tree, right).end
	}
	return span
}

func concatExpressionRange(tree syntaxTree, expr concatExpressionID) sourceRange {
	span := additiveExpressionRange(tree, tree.concatFirst(expr))
	rest, _ := tree.concatRest(expr)
	for _, part := range rest {
		span.end = additiveExpressionRange(tree, part).end
	}
	return span
}

func additiveExpressionRange(tree syntaxTree, expr additiveExpressionID) sourceRange {
	span := multiplicativeExpressionRange(tree, tree.additiveFirst(expr))
	rest, _ := tree.additiveRest(expr)
	for _, part := range rest {
		span.end = multiplicativeExpressionRange(tree, tree.additivePartValue(part)).end
	}
	return span
}

func multiplicativeExpressionRange(tree syntaxTree, expr multiplicativeExpressionID) sourceRange {
	span := termRange(tree, tree.multiplicativeFirst(expr))
	rest, _ := tree.multiplicativeRest(expr)
	for _, part := range rest {
		span.end = termRange(tree, tree.multiplicativePartValue(part)).end
	}
	return span
}

func termRange(tree syntaxTree, value termID) sourceRange {
	start, end := tree.termRange(value)
	return sourceRange{start: start, end: end}
}

func (a *analysisState) functionFactForCall(call arenaCallID) (functionFact, bool) {
	return a.functionFactForCallWithDiagnostics(call, true)
}

func (a *analysisState) functionFactForCallQuiet(call arenaCallID) (functionFact, bool) {
	return a.functionFactForCallWithDiagnostics(call, false)
}

func (a *analysisState) functionFactForCallWithDiagnostics(call arenaCallID, diagnoseAccess bool) (functionFact, bool) {
	callTarget := a.tree.callTarget(call)
	if callTarget == 0 {
		return functionFact{}, false
	}
	target := termWithoutCastsAndGroups(a.tree, callTarget)
	name := a.tree.termName(target)
	selectors, _ := a.tree.termSelectors(target)
	if name == "" {
		return functionFact{}, false
	}
	if use, ok := a.bind.use(a.tree.termID(target)); ok {
		if len(selectors) != 0 {
			if fact, ok := a.tableFunctionFactForCallTarget(target, diagnoseAccess); ok {
				return fact, true
			}
			return functionFact{}, false
		}
		fact, ok := a.functions[use.symbol]
		return fact, ok
	}
	if len(selectors) != 0 {
		if fact, ok := a.tableFunctionFactForCallTarget(target, diagnoseAccess); ok {
			return fact, true
		}
		return functionFact{}, false
	}
	if len(selectors) == 0 {
		fact, ok := a.typeEnv.lookup(name)
		if ok && fact.hasFunction {
			return fact.function, true
		}
	}
	return functionFact{}, false
}

func (a *analysisState) tableFunctionFactForCallTarget(target termID, diagnoseAccess bool) (functionFact, bool) {
	tree := a.tree
	name := tree.termName(target)
	selectors, _ := tree.termSelectors(target)
	start, end := tree.termRange(target)
	if name == "" || len(selectors) != 1 {
		return functionFact{}, false
	}
	selector := selectors[0]
	field := tree.termSelectorField(selector)
	if field == "" || tree.termSelectorIndex(selector) != 0 {
		return functionFact{}, false
	}
	table, ok := a.lookupTableFact(name)
	if !ok || table.functions == nil {
		return functionFact{}, false
	}
	if table.access[field] == "write" {
		if diagnoseAccess {
			a.diagnostics = append(a.diagnostics, writeonlyPropertyDiagnostic(
				field,
				start,
				end,
			))
		}
		return functionFact{}, false
	}
	fact, ok := table.functions[field]
	return fact, ok
}

func (a *analysisState) functionFactFromAnnotation(annotation typeID) (functionFact, bool) {
	tree := a.tree
	if summary, ok := a.moduleExportedTypeAliasSummary(annotation); ok {
		if summary.Kind == TypeSummaryFunction || summary.Kind == TypeSummaryGenericFunction {
			return functionFactFromSummary(summary), true
		}
	}
	annotation, substitutions := a.functionAnnotation(annotation)
	if annotation == 0 || (tree.typeKind(annotation) != typeKindFunction && tree.typeKind(annotation) != typeKindGenericFunction) {
		return functionFact{}, false
	}
	returnAnnotation, _ := tree.typeReturn(annotation)
	params := tree.typeParams(annotation)
	typeParamIDs, _ := tree.typeTypeParamIDs(annotation)
	typeParams := syntaxStrings(tree, typeParamIDs)
	returnPack := a.returnPackFromAnnotationWith(returnAnnotation, substitutions)
	fact := functionFact{
		typeParams:    append([]string(nil), typeParams...),
		params:        make([]simpleType, 0, len(params)),
		paramGenerics: make([]string, 0, len(params)),
		returnType:    returnPack.firstType(a.simpleTypeFromAnnotationWith(returnAnnotation, substitutions)),
		returnTable:   returnPack.firstTable(a.tableFactFromAnnotation(returnAnnotation)),
		returnSpan:    annotationRange(a.tree, returnAnnotation),
		returnPack:    returnPack,
		returnGeneric: genericAnnotationName(a.tree, returnAnnotation, typeParams),
	}
	for _, param := range params {
		value := tree.typeParamValue(param)
		if value != 0 && tree.typeKind(value) == typeKindVariadic {
			value, _ = tree.typeInner(value)
		}
		if tree.typeParamVariadic(param) || (tree.typeParamValue(param) != 0 && tree.typeKind(tree.typeParamValue(param)) == typeKindVariadic) {
			fact.variadic = a.simpleTypeFromAnnotationWith(value, substitutions)
			fact.variadicGeneric = genericAnnotationName(a.tree, value, typeParams)
			continue
		}
		fact.params = append(fact.params, a.simpleTypeFromAnnotationWith(value, substitutions))
		fact.paramGenerics = append(fact.paramGenerics, genericAnnotationName(a.tree, value, typeParams))
	}
	return fact, true
}

func (a *analysisState) functionAnnotation(annotation typeID) (typeID, map[string]simpleType) {
	annotation, substitutions := a.resolveAliasAnnotation(annotation)
	kind := a.tree.typeKind(annotation)
	if annotation == 0 || (kind != typeKindFunction && kind != typeKindGenericFunction) {
		return 0, nil
	}
	return annotation, substitutions
}

func (a *analysisState) inferFieldRead(value termID) simpleType {
	name := a.tree.termName(value)
	selectors, _ := a.tree.termSelectors(value)
	if name == "" || len(selectors) != 1 {
		return simpleTypeUnknown
	}
	fact, ok := a.lookupTableFact(name)
	if !ok {
		return simpleTypeUnknown
	}
	selector := selectors[0]
	start, end := a.tree.termRange(value)
	if field, ok := stableFieldSelector(a.tree, value); ok {
		typ, exists := fact.fields[field]
		if exists {
			if fact.access[field] == "write" {
				a.diagnostics = append(a.diagnostics, writeonlyPropertyDiagnostic(
					field,
					start,
					end,
				))
				return simpleTypeUnknown
			}
			return typ
		}
	}
	selectorIndex := a.tree.termSelectorIndex(selector)
	if selectorIndex != 0 {
		key := a.inferExpression(selectorIndex)
		if indexer, ok := a.tableIndexerForKey(fact, key, expressionRange(a.tree, selectorIndex)); ok {
			if indexer.access == "write" {
				a.diagnostics = append(a.diagnostics, writeonlyPropertyDiagnostic(
					"indexer",
					start,
					end,
				))
				return simpleTypeUnknown
			}
			return indexer.value
		}
		return simpleTypeUnknown
	}
	selectorField := a.tree.termSelectorField(selector)
	if selectorField == "" {
		return simpleTypeUnknown
	}
	if indexer, ok := tableIndexerForKeyType(fact, simpleTypeString); ok {
		if indexer.access == "write" {
			a.diagnostics = append(a.diagnostics, writeonlyPropertyDiagnostic(
				"indexer",
				start,
				end,
			))
			return simpleTypeUnknown
		}
		return indexer.value
	}
	constraint := typeConstraint{
		kind:  constraintIndexRead,
		field: selectorField,
		evidence: constraintEvidence{
			span: termRange(a.tree, value),
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

func (a *analysisState) checkCallArguments(fact functionFact, call arenaCallID) simpleType {
	tree := a.tree
	target := tree.callTarget(call)
	receiver := tree.callReceiver(call)
	args, _ := tree.callArgs(call)
	substitutions := a.explicitGenericSubstitutions(fact, call)
	receiverOffset := methodReceiverOffset(a.tree, call)
	for i, expected := range fact.params {
		generic := genericAt(fact.paramGenerics, i)
		actual := simpleTypeNil
		span := termRange(a.tree, target)
		if receiver != 0 && i == 0 {
			actual = a.inferTerm(receiver)
			span = termRange(a.tree, receiver)
		} else if argIndex := i - receiverOffset; argIndex >= 0 && argIndex < len(args) {
			actual = a.inferExpression(args[argIndex])
			span = expressionRange(a.tree, args[argIndex])
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

func (a *analysisState) checkVariadicCallArguments(fact functionFact, call arenaCallID, substitutions map[string]simpleType) {
	args, _ := a.tree.callArgs(call)
	if fact.variadic == simpleTypeUnknown && fact.variadicGeneric == "" {
		return
	}
	start := len(fact.params) - methodReceiverOffset(a.tree, call)
	if start < 0 {
		start = 0
	}
	for i := start; i < len(args); i++ {
		expected := fact.variadic
		actual := a.inferExpression(args[i])
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
				span: expressionRange(a.tree, args[i]),
			},
		}
		a.checkConstraint(constraint)
	}
}

func methodReceiverOffset(tree syntaxTree, call arenaCallID) int {
	if tree.callReceiver(call) == 0 {
		return 0
	}
	return 1
}

func (a *analysisState) explicitGenericSubstitutions(fact functionFact, call arenaCallID) map[string]simpleType {
	typeArgs := a.tree.callTypeArgs(call)
	substitutions := make(map[string]simpleType)
	for i, typeParam := range fact.typeParams {
		if i >= len(typeArgs) {
			break
		}
		typ := a.simpleTypeFromAnnotation(typeArgs[i])
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

func genericAnnotationNames(tree syntaxTree, annotations []typeID, typeParams []string) []string {
	names := make([]string, len(annotations))
	for i, annotation := range annotations {
		names[i] = genericAnnotationName(tree, annotation, typeParams)
	}
	return names
}

func genericAnnotationName(tree syntaxTree, annotation typeID, typeParams []string) string {
	nameIDs, ok := tree.typeNameIDs(annotation)
	if annotation == 0 || !ok || tree.typeKind(annotation) != typeKindName || len(nameIDs) != 1 || len(tree.typeArgs(annotation)) != 0 {
		return ""
	}
	name, _ := tree.stringValue(nameIDs[0])
	for _, typeParam := range typeParams {
		if name == typeParam {
			return name
		}
	}
	return ""
}

func (a *analysisState) checkFunctionParameterTypeNames(annotations []typeID, variadicAnnotation typeID) {
	for _, annotation := range annotations {
		a.checkUnknownTypeNames(annotation)
	}
	a.checkUnknownTypeNames(variadicAnnotation)
}

func (a *analysisState) checkUnknownTypeNames(annotation typeID) {
	if !policyForMode(a.mode).reportsUnknownTypes() || annotation == 0 {
		return
	}
	tree := a.tree
	switch tree.typeKind(annotation) {
	case typeKindName:
		a.checkUnknownTypeName(annotation)
		for _, arg := range tree.typeArgs(annotation) {
			a.checkUnknownTypeNames(arg)
		}
	case typeKindUnion, typeKindIntersection:
		for _, option := range tree.typeChildren(annotation) {
			a.checkUnknownTypeNames(option)
		}
	case typeKindNilable, typeKindVariadic, typeKindGenericPack:
		inner, _ := tree.typeInner(annotation)
		a.checkUnknownTypeNames(inner)
	case typeKindTable:
		for _, field := range tree.typeFields(annotation) {
			a.checkUnknownTypeNames(tree.typeFieldKey(field))
			a.checkUnknownTypeNames(tree.typeFieldValue(field))
		}
	case typeKindFunction:
		a.checkFunctionTypeNames(annotation)
	case typeKindGenericFunction:
		a.checkFunctionTypeNames(annotation)
	case typeKindTypeof, typeKindSingleton:
	}
}

func (a *analysisState) checkFunctionTypeNames(annotation typeID) {
	for _, param := range a.tree.typeParams(annotation) {
		a.checkUnknownTypeNames(a.tree.typeParamValue(param))
	}
	ret, _ := a.tree.typeReturn(annotation)
	a.checkUnknownTypeNames(ret)
}

func (a *analysisState) checkUnknownTypeName(annotation typeID) {
	tree := a.tree
	nameIDs, ok := tree.typeNameIDs(annotation)
	if !ok || len(nameIDs) == 0 {
		return
	}
	first, _ := tree.stringValue(nameIDs[0])
	if a.isKnownBuiltinTypeName(first) {
		return
	}
	start, _ := tree.typeRange(annotation)
	end := start + len(first)
	if _, ok := a.bind.use(tree.typeID(annotation)); ok {
		if len(nameIDs) == 2 {
			second, _ := tree.stringValue(nameIDs[1])
			if _, isModule := a.lookupModuleLocal(first); isModule {
				if _, ok := a.lookupModuleExportedTypeAlias(first, second); !ok {
					aliasStart := end + 1
					a.diagnostics = append(a.diagnostics, unknownTypeDiagnostic(
						second,
						aliasStart,
						aliasStart+len(second),
					))
				}
			}
		}
		return
	}
	a.diagnostics = append(a.diagnostics, unknownTypeDiagnostic(first, start, end))
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

func (a *analysisState) simpleTypeFromAnnotation(annotation typeID) simpleType {
	return a.simpleTypeFromAnnotationWith(annotation, nil)
}

func (a *analysisState) simpleTypeFromAnnotationWith(annotation typeID, substitutions map[string]simpleType) simpleType {
	if annotation == 0 {
		return simpleTypeUnknown
	}
	tree := a.tree
	kind := tree.typeKind(annotation)
	switch kind {
	case typeKindTable:
		return simpleTypeTable
	case typeKindFunction, typeKindGenericFunction:
		return simpleTypeFunction
	case typeKindNilable:
		innerID, _ := tree.typeInner(annotation)
		inner := a.simpleTypeFromAnnotationWith(innerID, substitutions)
		if inner == simpleTypeUnknown {
			return simpleTypeUnknown
		}
		return inner + "?"
	case typeKindUnion:
		var parts []string
		for _, option := range tree.typeChildren(annotation) {
			optionType := a.simpleTypeFromAnnotationWith(option, substitutions)
			if optionType == simpleTypeUnknown {
				return simpleTypeUnknown
			}
			parts = append(parts, string(optionType))
		}
		return simpleType(strings.Join(parts, "|"))
	}
	nameIDs, nameOK := tree.typeNameIDs(annotation)
	if kind != typeKindName || !nameOK || len(nameIDs) != 1 {
		if summary, ok := a.moduleExportedTypeAliasSummary(annotation); ok {
			return simpleTypeFromSummary(summary)
		}
		return simpleTypeUnknown
	}
	name, _ := tree.stringValue(nameIDs[0])
	typeArgs := tree.typeArgs(annotation)
	if len(typeArgs) == 0 {
		if substituted, ok := substitutions[name]; ok {
			return substituted
		}
	}
	switch name {
	case "any":
		if len(typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeAny
	case "unknown":
		if len(typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeCheckedUnknown
	case "never":
		if len(typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeNever
	case "nil":
		if len(typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeNil
	case "boolean":
		if len(typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeBoolean
	case "number":
		if len(typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeNumber
	case "string":
		if len(typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeString
	case "table":
		if len(typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeTable
	case "thread":
		if len(typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeThread
	case "userdata":
		if len(typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeUserData
	case "buffer":
		if len(typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeBuffer
	case "vector":
		if len(typeArgs) != 0 {
			return simpleTypeUnknown
		}
		return simpleTypeVector
	default:
		if alias, ok := a.lookupTypeAlias(name); ok {
			next, ok := a.genericAliasSubstitutions(alias, typeArgs, substitutions)
			if !ok {
				return simpleTypeUnknown
			}
			return a.simpleTypeFromAnnotationWith(alias.value, next)
		}
		return simpleTypeUnknown
	}
}

func (a *analysisState) genericAliasSubstitutions(alias typeAliasFact, args []typeID, outer map[string]simpleType) (map[string]simpleType, bool) {
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

func (a *analysisState) simpleTypesFromAnnotations(annotations []typeID) []simpleType {
	types := make([]simpleType, len(annotations))
	for i, annotation := range annotations {
		types[i] = a.simpleTypeFromAnnotation(annotation)
	}
	return types
}

func (a *analysisState) tableFieldTypes(annotation typeID) map[string]simpleType {
	return a.tableFactFromAnnotation(annotation).fields
}

func (a *analysisState) tableFactFromAnnotation(annotation typeID) tableFact {
	if summary, ok := a.moduleExportedTypeAliasSummary(annotation); ok && summary.Kind == TypeSummaryTable {
		return tableFactFromSummary(summary)
	}
	fields := make(map[string]simpleType)
	access := make(map[string]string)
	functions := make(map[string]functionFact)
	annotation, substitutions := a.tableAnnotation(annotation)
	if annotation == 0 {
		return tableFact{}
	}
	var indexers []tableIndexerFact
	for _, field := range a.tree.typeFields(annotation) {
		fieldName := a.tree.typeFieldName(field)
		fieldValue := a.tree.typeFieldValue(field)
		fieldAccess := a.tree.typeFieldAccess(field)
		fieldKey := a.tree.typeFieldKey(field)
		if fieldName != "" {
			fields[fieldName] = a.simpleTypeFromAnnotationWith(fieldValue, substitutions)
			if fieldAccess != "" {
				access[fieldName] = fieldAccess
			}
			if fact, ok := a.functionFactFromAnnotation(fieldValue); ok {
				functions[fieldName] = fact
			}
			continue
		}
		if fieldKey == 0 {
			value := a.simpleTypeFromAnnotationWith(fieldValue, substitutions)
			if value != simpleTypeUnknown {
				indexers = append(indexers, tableIndexerFact{key: simpleTypeNumber, value: value, access: fieldAccess})
			}
			continue
		}
		key := a.simpleTypeFromAnnotationWith(fieldKey, substitutions)
		value := a.simpleTypeFromAnnotationWith(fieldValue, substitutions)
		if key == simpleTypeUnknown || value == simpleTypeUnknown {
			continue
		}
		indexers = append(indexers, tableIndexerFact{key: key, value: value, access: fieldAccess})
	}
	return tableFact{known: true, fields: fields, access: access, functions: functions, indexers: indexers}
}

func (a *analysisState) moduleExportedTypeAliasSummary(annotation typeID) (TypeSummary, bool) {
	nameIDs, ok := a.tree.typeNameIDs(annotation)
	if annotation == 0 || !ok || a.tree.typeKind(annotation) != typeKindName || len(nameIDs) != 2 || len(a.tree.typeArgs(annotation)) != 0 {
		return TypeSummary{}, false
	}
	moduleName, _ := a.tree.stringValue(nameIDs[0])
	aliasName, _ := a.tree.stringValue(nameIDs[1])
	exported, ok := a.lookupModuleExportedTypeAlias(moduleName, aliasName)
	if !ok {
		return TypeSummary{}, false
	}
	return exported.Type, true
}

func (a *analysisState) tableAnnotation(annotation typeID) (typeID, map[string]simpleType) {
	annotation, substitutions := a.resolveAliasAnnotation(annotation)
	if annotation == 0 || a.tree.typeKind(annotation) != typeKindTable {
		return 0, nil
	}
	return annotation, substitutions
}

func (a *analysisState) resolveAliasAnnotation(annotation typeID) (typeID, map[string]simpleType) {
	nameIDs, ok := a.tree.typeNameIDs(annotation)
	if annotation == 0 || !ok || a.tree.typeKind(annotation) != typeKindName || len(nameIDs) != 1 {
		return annotation, nil
	}
	name, _ := a.tree.stringValue(nameIDs[0])
	if alias, ok := a.lookupTypeAlias(name); ok {
		substitutions, ok := a.genericAliasSubstitutions(alias, a.tree.typeArgs(annotation), nil)
		if !ok {
			return 0, nil
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

func simpleTypeFromExpression(tree syntaxTree, expr expressionID) simpleType {
	value, ok := expressionSingleTerm(tree, expr)
	selectors, _ := tree.termSelectors(value)
	if !ok || len(selectors) != 0 {
		return simpleTypeUnknown
	}
	return simpleTypeFromTerm(tree, value)
}

func simpleTypeFromTerm(tree syntaxTree, value termID) simpleType {
	switch {
	case func() bool { _, ok := tree.termNumber(value); return ok }():
		return simpleTypeNumber
	case func() bool { _, ok := tree.termLiteral(value); return ok }():
		literal, _ := tree.termLiteral(value)
		switch literal.Kind() {
		case NilKind:
			return simpleTypeNil
		case BoolKind:
			return simpleTypeBoolean
		case NumberKind:
			return simpleTypeNumber
		case StringKind:
			return simpleTypeString
		}
	case func() bool { _, ok := tree.termTable(value); return ok }():
		return simpleTypeTable
	case func() bool { _, ok := tree.termFunction(value); return ok }():
		return simpleTypeFunction
	case func() bool { _, ok := tree.termGroup(value); return ok }():
		group, _ := tree.termGroup(value)
		return simpleTypeFromExpression(tree, group)
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
