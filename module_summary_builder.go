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
	diagCodes := diagnosticCodes(diagnostics)
	store := newTypeStore()
	lowered := lowerTypeAliases(store, prog.statements)
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
	valueFlow := moduleLocalValueSummaries(prog, store)
	if value, ok := moduleReturnExport(prog, diagCodes, valueFlow); ok {
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
	values map[string]TypeSummary
}

func moduleLocalValueSummaries(prog program, store *typeStore) moduleValueFlow {
	flow := moduleValueFlow{values: baseGlobalValueSummaries()}
	flow.applyStatements(prog.statements, store)
	return flow
}

func (f moduleValueFlow) applyAssignment(stmt assignStatement) {
	for i, target := range stmt.targets {
		if i >= len(stmt.values) {
			continue
		}
		if len(target.selectors) == 0 {
			if _, ok := f.values[target.name]; ok {
				f.values[target.name] = f.value(stmt.values[i])
			}
			continue
		}
		table, ok := f.values[target.name]
		if !ok || table.Kind != TypeSummaryTable {
			continue
		}
		if setTableSummaryPath(&table, target.selectors, f.value(stmt.values[i])) {
			f.values[target.name] = table
		}
	}
}

func (f moduleValueFlow) applyIf(stmt ifStatement, store *typeStore) {
	if len(stmt.thenStatements) == 0 {
		return
	}
	thenFlow := f.clone()
	thenFlow.applyStatements(stmt.thenStatements, store)
	elseFlow := f.clone()
	if len(stmt.elseStatements) != 0 {
		elseFlow.applyStatements(stmt.elseStatements, store)
	}
	f.mergeAgreed(thenFlow, elseFlow)
}

func (f moduleValueFlow) applyStatements(statements []statement, store *typeStore) {
	for _, stmt := range statements {
		switch {
		case stmt.local != nil:
			for i, name := range stmt.local.names {
				if i < len(stmt.local.annotations) && stmt.local.annotations[i] != nil {
					f.values[name] = store.summary(store.lowerType(stmt.local.annotations[i]))
					continue
				}
				if i < len(stmt.local.values) {
					f.values[name] = f.value(stmt.local.values[i])
				}
			}
		case stmt.assign != nil:
			f.applyAssignment(*stmt.assign)
		case stmt.ifStmt != nil:
			f.applyIf(*stmt.ifStmt, store)
		}
	}
}

func (f moduleValueFlow) clone() moduleValueFlow {
	return moduleValueFlow{values: cloneTypeSummaryMap(f.values)}
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

func cloneTypeSummary(summary TypeSummary) TypeSummary {
	clone := summary
	clone.TypeParams = append([]string(nil), summary.TypeParams...)
	clone.TypePacks = append([]string(nil), summary.TypePacks...)
	clone.Types = cloneTypeSummarySlice(summary.Types)
	if summary.Inner != nil {
		inner := cloneTypeSummary(*summary.Inner)
		clone.Inner = &inner
	}
	clone.Properties = make([]TablePropertySummary, len(summary.Properties))
	for i, property := range summary.Properties {
		clone.Properties[i] = TablePropertySummary{
			Name:   property.Name,
			Access: property.Access,
			Type:   cloneTypeSummary(property.Type),
		}
	}
	clone.Indexers = make([]TableIndexerSummary, len(summary.Indexers))
	for i, indexer := range summary.Indexers {
		clone.Indexers[i] = TableIndexerSummary{
			Access: indexer.Access,
			Key:    cloneTypeSummary(indexer.Key),
			Value:  cloneTypeSummary(indexer.Value),
		}
	}
	if summary.Metatable != nil {
		metatable := cloneTypeSummary(*summary.Metatable)
		clone.Metatable = &metatable
	}
	clone.Params = cloneTypeSummarySlice(summary.Params)
	if summary.Return != nil {
		ret := cloneTypeSummary(*summary.Return)
		clone.Return = &ret
	}
	clone.ParamPack = cloneTypePackSummary(summary.ParamPack)
	clone.ReturnPack = cloneTypePackSummary(summary.ReturnPack)
	return clone
}

func cloneTypeSummarySlice(summaries []TypeSummary) []TypeSummary {
	if len(summaries) == 0 {
		return nil
	}
	clone := make([]TypeSummary, len(summaries))
	for i, summary := range summaries {
		clone[i] = cloneTypeSummary(summary)
	}
	return clone
}

func cloneTypePackSummary(summary TypePackSummary) TypePackSummary {
	clone := summary
	clone.Head = cloneTypeSummarySlice(summary.Head)
	if summary.Tail != nil {
		tail := cloneTypeSummary(*summary.Tail)
		clone.Tail = &tail
	}
	return clone
}

func moduleReturnExport(prog program, diagCodes []string, flow moduleValueFlow) (ModuleExport, bool) {
	for _, stmt := range prog.statements {
		if stmt.ret == nil || len(stmt.ret.values) == 0 {
			continue
		}
		return ModuleExport{
			Name:      "return",
			Kind:      ModuleExportValue,
			Type:      flow.value(stmt.ret.values[0]),
			DiagCodes: diagCodes,
		}, true
	}
	return ModuleExport{}, false
}

func (f moduleValueFlow) value(expr expression) TypeSummary {
	value, ok := expressionSingleTerm(expr)
	if !ok {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	if value.name != "" {
		if len(value.selectors) != 0 {
			if summary, ok := f.values[value.name]; ok {
				if field, ok := tableSummaryPath(summary, value.selectors); ok {
					return field
				}
			}
			return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
		}
		if summary, ok := f.values[value.name]; ok {
			return summary
		}
	}
	if len(value.selectors) != 0 {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	if value.call != nil {
		return f.callValue(*value.call)
	}
	if value.table != nil {
		return f.tableValue(*value.table)
	}
	return simpleTypeSummary(simpleTypeFromTerm(value))
}

func (f moduleValueFlow) callValue(call callExpression) TypeSummary {
	if len(call.target.selectors) != 0 {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	switch call.target.name {
	case "setmetatable":
		return f.setMetatableCallValue(call)
	case "getmetatable":
		return f.getMetatableCallValue(call)
	default:
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
}

func (f moduleValueFlow) setMetatableCallValue(call callExpression) TypeSummary {
	if len(call.args) < 2 {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	table := f.value(call.args[0])
	if table.Kind != TypeSummaryTable {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	metatable := f.value(call.args[1])
	if metatable.Kind != TypeSummaryTable {
		return table
	}
	table.Metatable = &metatable
	return table
}

func (f moduleValueFlow) getMetatableCallValue(call callExpression) TypeSummary {
	if len(call.args) == 0 {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	table := f.value(call.args[0])
	if table.Metatable == nil {
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
	return *table.Metatable
}

func (f moduleValueFlow) tableValue(table tableExpression) TypeSummary {
	summary := TypeSummary{Kind: TypeSummaryTable, Display: "table"}
	for _, field := range table.fields {
		if field.name == "" {
			continue
		}
		summary.Properties = append(summary.Properties, TablePropertySummary{
			Name: field.name,
			Type: f.value(field.value),
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
