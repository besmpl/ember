package ember

import (
	"fmt"
	"strings"
)

func typeSummaryFromAlias(tree syntaxTree, alias typeAliasStatement) TypeSummary {
	summary := typeSummaryFromExpression(tree, tree.typeAliasValue(&alias))
	summary.TypeParams = append([]string(nil), tree.typeAliasTypeParams(&alias)...)
	summary.TypePacks = append([]string(nil), tree.typeAliasTypePacks(&alias)...)
	return summary
}

func typeSummaryFromExpression(tree syntaxTree, expr *typeExpression) TypeSummary {
	if expr == nil {
		return TypeSummary{
			Kind:    TypeSummaryUnknown,
			Display: "unknown",
		}
	}
	switch tree.typeKind(expr) {
	case typeKindName:
		display := strings.Join(tree.typeName(expr), ".")
		if display == "" {
			display = "unknown"
		}
		if len(tree.typeArgs(expr)) > 0 {
			display += typeArgumentSummaryDisplay(tree, tree.typeArgs(expr))
		}
		return TypeSummary{Kind: TypeSummaryName, Display: display}
	case typeKindUnion:
		types := typeSummariesFromExpressions(tree, tree.typeChildren(expr))
		return TypeSummary{Kind: TypeSummaryUnion, Display: joinTypeDisplays(types, " | "), Types: types}
	case typeKindIntersection:
		types := typeSummariesFromExpressions(tree, tree.typeChildren(expr))
		return TypeSummary{Kind: TypeSummaryIntersection, Display: joinTypeDisplays(types, " & "), Types: types}
	case typeKindNilable:
		inner := typeSummaryFromExpression(tree, tree.typeInner(expr))
		return TypeSummary{Kind: TypeSummaryNilable, Display: inner.Display + "?", Inner: &inner}
	case typeKindTable:
		return tableTypeSummary(tree, expr)
	case typeKindFunction, typeKindGenericFunction:
		return functionTypeSummary(tree, expr)
	case typeKindVariadic:
		inner := typeSummaryFromExpression(tree, tree.typeInner(expr))
		display := "..."
		if tree.typeInner(expr) != nil {
			display += inner.Display
		}
		return TypeSummary{Kind: TypeSummaryVariadic, Display: display, Inner: &inner}
	case typeKindGenericPack:
		display := strings.Join(tree.typeName(expr), ".")
		if display == "" {
			display = "..."
		}
		return TypeSummary{Kind: TypeSummaryGenericPack, Display: display + "..."}
	case typeKindSingleton:
		if tree.typeLiteral(expr) == nil {
			return TypeSummary{Kind: TypeSummarySingleton, Display: "unknown"}
		}
		return TypeSummary{Kind: TypeSummarySingleton, Display: valueSummaryDisplay(*tree.typeLiteral(expr))}
	case typeKindTypeof:
		return TypeSummary{Kind: TypeSummaryTypeof, Display: "typeof"}
	default:
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
}

func typeSummariesFromExpressions(tree syntaxTree, expressions []*typeExpression) []TypeSummary {
	summaries := make([]TypeSummary, len(expressions))
	for i, expr := range expressions {
		summaries[i] = typeSummaryFromExpression(tree, expr)
	}
	return summaries
}

func tableTypeSummary(tree syntaxTree, expr *typeExpression) TypeSummary {
	summary := TypeSummary{Kind: TypeSummaryTable, Display: "table"}
	for i := range tree.typeFields(expr) {
		field := &tree.typeFields(expr)[i]
		value := typeSummaryFromExpression(tree, tree.typeFieldValue(field))
		if tree.typeFieldName(field) != "" {
			summary.Properties = append(summary.Properties, TablePropertySummary{
				Name:   tree.typeFieldName(field),
				Access: tree.typeFieldAccess(field),
				Type:   value,
			})
			continue
		}
		if tree.typeFieldKey(field) != nil {
			summary.Indexers = append(summary.Indexers, TableIndexerSummary{
				Access: tree.typeFieldAccess(field),
				Key:    typeSummaryFromExpression(tree, tree.typeFieldKey(field)),
				Value:  value,
			})
		}
	}
	return summary
}

func functionTypeSummary(tree syntaxTree, expr *typeExpression) TypeSummary {
	kind := TypeSummaryFunction
	if tree.typeKind(expr) == typeKindGenericFunction {
		kind = TypeSummaryGenericFunction
	}
	params := typeSummariesFromExpressions(tree, typePackValueExpressions(tree, tree.typeParams(expr)))
	ret := typeSummaryFromExpression(tree, tree.typeReturn(expr))
	summary := TypeSummary{
		Kind:       kind,
		Display:    "(" + joinTypeDisplays(params, ", ") + ") -> " + ret.Display,
		TypeParams: append([]string(nil), tree.typeTypeParams(expr)...),
		TypePacks:  append([]string(nil), tree.typePacks(expr)...),
		Params:     params,
		Return:     &ret,
		ParamPack:  typePackSummary(params, nil),
		ReturnPack: typePackSummary([]TypeSummary{ret}, nil),
	}
	return summary
}

func typePackValueExpressions(tree syntaxTree, values []typeFunctionParam) []*typeExpression {
	expressions := make([]*typeExpression, 0, len(values))
	for _, value := range values {
		expressions = append(expressions, tree.typeParamValue(&value))
	}
	return expressions
}

func typeArgumentSummaryDisplay(tree syntaxTree, args []*typeExpression) string {
	summaries := typeSummariesFromExpressions(tree, args)
	return "<" + joinTypeDisplays(summaries, ", ") + ">"
}

func joinTypeDisplays(types []TypeSummary, separator string) string {
	if len(types) == 0 {
		return ""
	}
	parts := make([]string, len(types))
	for i, typ := range types {
		parts[i] = typ.Display
	}
	return strings.Join(parts, separator)
}

func valueSummaryDisplay(value Value) string {
	if text, ok := value.String(); ok {
		return fmt.Sprintf("%q", text)
	}
	if number, ok := value.Number(); ok {
		return fmt.Sprintf("%g", number)
	}
	if boolean, ok := value.Bool(); ok {
		if boolean {
			return "true"
		}
		return "false"
	}
	if value.IsNil() {
		return "nil"
	}
	return string(value.Kind())
}
