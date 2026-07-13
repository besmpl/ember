package ember

import (
	"crypto/sha256"
	"fmt"
	"reflect"
)

func buildModuleSummary(source Source, mode SourceMode, diagnostics []Diagnostic, exports []ModuleExport) ModuleSummary {
	return ModuleSummary{
		Version:             1,
		SourceName:          source.Name,
		Mode:                mode,
		CompatibilityTarget: "luau-0.728",
		InvalidationHash:    sourceInvalidationHash(source),
		Exports:             exports,
		Diagnostics:         append([]Diagnostic(nil), diagnostics...),
	}
}

func sourceInvalidationHash(source Source) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(source.Text)))
}

func buildTypedArtifactFacts(prog program, diagnostics []Diagnostic) typedArtifactFacts {
	return buildTypedArtifactFactsTree(newSyntaxTree(prog), diagnostics)
}

func buildTypedArtifactFactsTree(tree syntaxTree, diagnostics []Diagnostic) typedArtifactFacts {
	diagCodes := diagnosticCodes(diagnostics)
	store := newTypeStoreTree(tree)
	lowered := lowerTypeAliases(store, tree.statements())
	exports := make([]ModuleExport, 0, len(lowered))
	aliases := make([]ToolingTypeAliasFact, 0, len(lowered))
	for _, item := range lowered {
		summary := store.summary(item.typ)
		applyAliasTypeParameters(&summary, item)
		aliases = append(aliases, ToolingTypeAliasFact{
			Name:      item.name,
			Exported:  item.exported,
			Start:     item.start,
			End:       item.end,
			NameStart: item.nameStart,
			NameEnd:   item.nameEnd,
			Type:      summary,
			DiagCodes: diagCodes,
		})
		if item.exported {
			exports = append(exports, ModuleExport{
				Name:      item.name,
				Kind:      ModuleExportTypeAlias,
				Type:      summary,
				DiagCodes: diagCodes,
			})
		}
	}
	valueFlow := moduleLocalValueSummariesTree(tree, store)
	if value, ok := moduleReturnExportTree(tree, diagCodes, valueFlow); ok {
		exports = append(exports, value)
	}
	return typedArtifactFacts{
		exports: exports,
		tooling: ToolingFacts{
			TypeAliases: aliases,
		},
	}
}

func applyAliasTypeParameters(summary *TypeSummary, item loweredTypeAlias) {
	if len(item.typeParams) != 0 {
		summary.TypeParams = append([]string(nil), item.typeParams...)
	}
	if len(item.typePacks) != 0 {
		summary.TypePacks = append([]string(nil), item.typePacks...)
	}
}

type moduleValueFlow struct {
	tree   syntaxTree
	values map[string]TypeSummary
}

func moduleLocalValueSummaries(prog program, store *typeStore) moduleValueFlow {
	return moduleLocalValueSummariesTree(newSyntaxTree(prog), store)
}

func moduleLocalValueSummariesTree(tree syntaxTree, store *typeStore) moduleValueFlow {
	flow := moduleValueFlow{tree: tree, values: baseGlobalValueSummaries()}
	flow.applyStatements(tree, tree.statements(), store)
	return flow
}

func (f moduleValueFlow) applyAssignment(tree syntaxTree, stmt assignStatement) {
	targets := tree.assignmentTargets(&stmt)
	values := tree.assignmentValues(&stmt)
	for i := range targets {
		target := &targets[i]
		if i >= len(values) {
			continue
		}
		selectors := tree.assignTargetSelectors(target)
		name := tree.assignTargetName(target)
		if len(selectors) == 0 {
			if _, ok := f.values[name]; ok {
				f.values[name] = f.value(values[i])
			}
			continue
		}
		table, ok := f.values[name]
		if !ok || table.Kind != TypeSummaryTable {
			continue
		}
		if setTableSummaryPath(f.tree, &table, selectors, f.value(values[i])) {
			f.values[name] = table
		}
	}
}

func (f moduleValueFlow) applyIf(tree syntaxTree, stmt ifStatement, store *typeStore) {
	thenStatements := tree.ifThenStatements(&stmt)
	elseStatements := tree.ifElseStatements(&stmt)
	if len(thenStatements) == 0 {
		return
	}
	thenFlow := f.clone()
	thenFlow.applyStatements(tree, thenStatements, store)
	elseFlow := f.clone()
	if len(elseStatements) != 0 {
		elseFlow.applyStatements(tree, elseStatements, store)
	}
	f.mergeAgreed(thenFlow, elseFlow)
}

func (f moduleValueFlow) applyStatements(tree syntaxTree, statements []statement, store *typeStore) {
	for i := range statements {
		stmt := &statements[i]
		switch tree.statementKind(stmt) {
		case syntaxStatementLocal:
			local := tree.local(stmt)
			annotations := tree.localAnnotations(local)
			values := tree.localValues(local)
			for i, name := range tree.localNames(local) {
				if i < len(annotations) && annotations[i] != nil {
					f.values[name] = store.summary(store.lowerType(annotations[i]))
					continue
				}
				if i < len(values) {
					f.values[name] = f.value(values[i])
				}
			}
		case syntaxStatementAssign:
			f.applyAssignment(tree, *tree.assignment(stmt))
		case syntaxStatementIf:
			f.applyIf(tree, *tree.ifStatement(stmt), store)
		}
	}
}

func (f moduleValueFlow) clone() moduleValueFlow {
	return moduleValueFlow{tree: f.tree, values: cloneTypeSummaryMap(f.values)}
}

func cloneTypeSummaryMap(values map[string]TypeSummary) map[string]TypeSummary {
	clone := make(map[string]TypeSummary, len(values))
	for name, summary := range values {
		clone[name] = cloneTypeSummary(summary)
	}
	return clone
}

func (f moduleValueFlow) mergeAgreed(left, right moduleValueFlow) {
	for name, leftSummary := range left.values {
		rightSummary, ok := right.values[name]
		if !ok {
			continue
		}
		merged, ok := mergeBranchTypeSummaries(leftSummary, rightSummary)
		if ok {
			f.values[name] = merged
		}
	}
}

func typeSummarySameShape(left, right TypeSummary) bool {
	return reflect.DeepEqual(left, right)
}

func mergeBranchTypeSummaries(left, right TypeSummary) (TypeSummary, bool) {
	if typeSummarySameShape(left, right) {
		return cloneTypeSummary(left), true
	}
	if left.Kind == TypeSummaryTable && right.Kind == TypeSummaryTable {
		return mergeBranchTableSummaries(left, right), true
	}
	if left.Kind == TypeSummaryUnknown || right.Kind == TypeSummaryUnknown {
		return TypeSummary{}, false
	}
	return unionTypeSummary(left, right), true
}

func mergeBranchTableSummaries(left, right TypeSummary) TypeSummary {
	merged := TypeSummary{Kind: TypeSummaryTable, Display: "table"}
	seen := make(map[string]struct{}, len(left.Properties)+len(right.Properties))
	for _, leftProperty := range left.Properties {
		seen[leftProperty.Name] = struct{}{}
		propertyType := optionalTypeSummary(leftProperty.Type)
		if rightType, ok := tableSummaryProperty(right, leftProperty.Name); ok {
			mergedType, ok := mergeBranchTypeSummaries(leftProperty.Type, rightType)
			if !ok {
				continue
			}
			propertyType = mergedType
		}
		if propertyType.Kind == TypeSummaryUnknown {
			continue
		}
		merged.Properties = append(merged.Properties, TablePropertySummary{
			Name:   leftProperty.Name,
			Access: leftProperty.Access,
			Type:   propertyType,
		})
	}
	for _, rightProperty := range right.Properties {
		if _, ok := seen[rightProperty.Name]; ok {
			continue
		}
		propertyType := optionalTypeSummary(rightProperty.Type)
		if propertyType.Kind == TypeSummaryUnknown {
			continue
		}
		merged.Properties = append(merged.Properties, TablePropertySummary{
			Name:   rightProperty.Name,
			Access: rightProperty.Access,
			Type:   propertyType,
		})
	}
	if left.Metatable != nil && right.Metatable != nil {
		metatable, ok := mergeBranchTypeSummaries(*left.Metatable, *right.Metatable)
		if ok {
			merged.Metatable = &metatable
		}
	}
	return merged
}

func optionalTypeSummary(summary TypeSummary) TypeSummary {
	if summary.Kind == TypeSummaryUnknown {
		return unknownSummary()
	}
	if summary.Kind == TypeSummaryNilable {
		return cloneTypeSummary(summary)
	}
	inner := cloneTypeSummary(summary)
	return TypeSummary{
		Kind:    TypeSummaryNilable,
		Display: inner.Display + "?",
		Inner:   &inner,
	}
}

func unionTypeSummary(left, right TypeSummary) TypeSummary {
	members := make([]TypeSummary, 0, 2)
	members = appendUnionTypeMembers(members, left)
	members = appendUnionTypeMembers(members, right)
	if len(members) == 1 {
		return members[0]
	}
	return TypeSummary{
		Kind:    TypeSummaryUnion,
		Display: joinTypeDisplays(members, " | "),
		Types:   members,
	}
}

func appendUnionTypeMembers(members []TypeSummary, summary TypeSummary) []TypeSummary {
	if summary.Kind == TypeSummaryUnion {
		for _, member := range summary.Types {
			members = appendUnionTypeMembers(members, member)
		}
		return members
	}
	clone := cloneTypeSummary(summary)
	for _, member := range members {
		if typeSummarySameShape(member, clone) {
			return members
		}
	}
	return append(members, clone)
}

func moduleReturnExport(prog program, diagCodes []string, flow moduleValueFlow) (ModuleExport, bool) {
	return moduleReturnExportTree(newSyntaxTree(prog), diagCodes, flow)
}

func moduleReturnExportTree(tree syntaxTree, diagCodes []string, flow moduleValueFlow) (ModuleExport, bool) {
	for i := range tree.statements() {
		ret := tree.returnStatement(tree.statement(i))
		if ret == nil || len(tree.returnValues(ret)) == 0 {
			continue
		}
		return ModuleExport{
			Name:      "return",
			Kind:      ModuleExportValue,
			Type:      flow.value(tree.returnValues(ret)[0]),
			DiagCodes: diagCodes,
		}, true
	}
	return ModuleExport{}, false
}

func (f moduleValueFlow) value(expr expressionID) TypeSummary {
	tree := f.tree
	value, ok := expressionSingleTerm(f.tree, expr)
	if !ok {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	name := tree.termName(value)
	selectors, _ := tree.termSelectors(value)
	if name != "" {
		if len(selectors) != 0 {
			if summary, ok := f.values[name]; ok {
				if field, ok := tableSummaryTermPath(f.tree, summary, selectors); ok {
					return field
				}
			}
			return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
		}
		if summary, ok := f.values[name]; ok {
			return summary
		}
	}
	if len(selectors) != 0 {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	if call, ok := tree.termCall(value); ok {
		return f.callValue(call)
	}
	if table, ok := tree.termTable(value); ok {
		return f.tableValue(table)
	}
	return simpleTypeSummary(simpleTypeFromTerm(f.tree, value))
}

func (f moduleValueFlow) callValue(call arenaCallID) TypeSummary {
	tree := f.tree
	target := tree.callTarget(call)
	selectors, _ := tree.termSelectors(target)
	if len(selectors) != 0 {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	switch tree.termName(target) {
	case "setmetatable":
		return f.setMetatableCallValue(call)
	case "getmetatable":
		return f.getMetatableCallValue(call)
	default:
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
}

func (f moduleValueFlow) setMetatableCallValue(call arenaCallID) TypeSummary {
	args, _ := f.tree.callArgs(call)
	if len(args) < 2 {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	table := f.value(args[0])
	if table.Kind != TypeSummaryTable {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	metatable := f.value(args[1])
	if metatable.Kind != TypeSummaryTable {
		return table
	}
	table.Metatable = &metatable
	return table
}

func (f moduleValueFlow) getMetatableCallValue(call arenaCallID) TypeSummary {
	args, _ := f.tree.callArgs(call)
	if len(args) == 0 {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	table := f.value(args[0])
	if table.Metatable == nil {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	return *table.Metatable
}

func (f moduleValueFlow) tableValue(table arenaTableID) TypeSummary {
	tree := f.tree
	summary := TypeSummary{Kind: TypeSummaryTable, Display: "table"}
	fields, _ := tree.tableFields(table)
	for _, field := range fields {
		if tree.tableFieldName(field) == "" {
			continue
		}
		summary.Properties = append(summary.Properties, TablePropertySummary{
			Name: tree.tableFieldName(field),
			Type: f.value(tree.tableFieldValue(field)),
		})
	}
	return summary
}

func simpleTypeSummary(typ simpleType) TypeSummary {
	switch typ {
	case simpleTypeNil:
		return TypeSummary{Kind: TypeSummaryName, Display: "nil"}
	case simpleTypeBoolean:
		return TypeSummary{Kind: TypeSummaryName, Display: "boolean"}
	case simpleTypeNumber:
		return TypeSummary{Kind: TypeSummaryName, Display: "number"}
	case simpleTypeString:
		return TypeSummary{Kind: TypeSummaryName, Display: "string"}
	default:
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
}

func diagnosticCodes(diagnostics []Diagnostic) []string {
	if len(diagnostics) == 0 {
		return nil
	}
	codes := make([]string, 0, len(diagnostics))
	seen := make(map[string]bool, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "" || seen[diagnostic.Code] {
			continue
		}
		seen[diagnostic.Code] = true
		codes = append(codes, diagnostic.Code)
	}
	return codes
}
