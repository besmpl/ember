package ember

import (
	"fmt"
	"math"
	"strings"
)

func typeSummaryFromAlias(tree syntaxTree, alias arenaTypeAliasStatement) TypeSummary {
	summary := typeSummaryFromExpression(tree, alias.value)
	summary.TypeParams = consumerStatementStrings(tree, alias.typeParams)
	summary.TypePacks = consumerStatementStrings(tree, alias.typePacks)
	return summary
}

func typeSummaryFromExpression(tree syntaxTree, expr typeID) TypeSummary {
	if expr == 0 {
		return TypeSummary{
			Kind:    TypeSummaryUnknown,
			Display: "unknown",
		}
	}
	switch tree.typeKind(expr) {
	case typeKindName:
		nameIDs, _ := tree.typeNameIDs(expr)
		display := joinSyntaxStrings(tree, nameIDs, ".")
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
		innerID, _ := tree.typeInner(expr)
		inner := typeSummaryFromExpression(tree, innerID)
		return TypeSummary{Kind: TypeSummaryNilable, Display: inner.Display + "?", Inner: &inner}
	case typeKindTable:
		return tableTypeSummary(tree, expr)
	case typeKindFunction, typeKindGenericFunction:
		return functionTypeSummary(tree, expr)
	case typeKindVariadic:
		innerID, hasInner := tree.typeInner(expr)
		inner := typeSummaryFromExpression(tree, innerID)
		display := "..."
		if hasInner {
			display += inner.Display
		}
		return TypeSummary{Kind: TypeSummaryVariadic, Display: display, Inner: &inner}
	case typeKindGenericPack:
		innerID, ok := tree.typeInner(expr)
		if !ok {
			return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
		}
		inner := typeSummaryFromExpression(tree, innerID)
		return TypeSummary{Kind: TypeSummaryGenericPack, Display: inner.Display + "...", Inner: &inner}
	case typeKindSingleton:
		return TypeSummary{Kind: TypeSummarySingleton, Display: typeSingletonDisplay(tree, expr)}
	case typeKindTypeof:
		return TypeSummary{Kind: TypeSummaryTypeof, Display: "typeof"}
	default:
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
}

func typeSummariesFromExpressions(tree syntaxTree, expressions []typeID) []TypeSummary {
	summaries := make([]TypeSummary, len(expressions))
	for i, expr := range expressions {
		summaries[i] = typeSummaryFromExpression(tree, expr)
	}
	return summaries
}

func tableTypeSummary(tree syntaxTree, expr typeID) TypeSummary {
	summary := TypeSummary{Kind: TypeSummaryTable, Display: "table"}
	for _, field := range tree.typeFields(expr) {
		value := typeSummaryFromExpression(tree, tree.typeFieldValue(field))
		if tree.typeFieldName(field) != "" {
			summary.Properties = append(summary.Properties, TablePropertySummary{
				Name:   tree.typeFieldName(field),
				Access: tree.typeFieldAccess(field),
				Type:   value,
			})
			continue
		}
		if tree.typeFieldKey(field) != 0 {
			summary.Indexers = append(summary.Indexers, TableIndexerSummary{
				Access: tree.typeFieldAccess(field),
				Key:    typeSummaryFromExpression(tree, tree.typeFieldKey(field)),
				Value:  value,
			})
		}
	}
	return summary
}

func functionTypeSummary(tree syntaxTree, expr typeID) TypeSummary {
	kind := TypeSummaryFunction
	if tree.typeKind(expr) == typeKindGenericFunction {
		kind = TypeSummaryGenericFunction
	}
	params := typeSummariesFromExpressions(tree, typePackValueExpressions(tree, tree.typeParams(expr)))
	returnID, _ := tree.typeReturn(expr)
	ret := typeSummaryFromExpression(tree, returnID)
	typeParamIDs, _ := tree.typeTypeParamIDs(expr)
	typePackIDs, _ := tree.typePackIDs(expr)
	summary := TypeSummary{
		Kind:       kind,
		Display:    "(" + joinTypeDisplays(params, ", ") + ") -> " + ret.Display,
		TypeParams: syntaxStrings(tree, typeParamIDs),
		TypePacks:  syntaxStrings(tree, typePackIDs),
		Params:     params,
		Return:     &ret,
		ParamPack:  typePackSummary(params, nil),
		ReturnPack: typePackSummary([]TypeSummary{ret}, nil),
	}
	return summary
}

func syntaxStrings(tree syntaxTree, ids []stringID) []string {
	if len(ids) == 0 {
		return nil
	}
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i], _ = tree.stringValue(id)
	}
	return values
}

func joinSyntaxStrings(tree syntaxTree, ids []stringID, separator string) string {
	if len(ids) == 0 {
		return ""
	}
	var builder strings.Builder
	for i, id := range ids {
		value, _ := tree.stringValue(id)
		if i != 0 {
			builder.WriteString(separator)
		}
		builder.WriteString(value)
	}
	return builder.String()
}

func syntaxStringsLength(tree syntaxTree, ids []stringID, separatorLength int) int {
	length := 0
	for _, id := range ids {
		value, _ := tree.stringValue(id)
		length += len(value)
	}
	if len(ids) > 1 {
		length += (len(ids) - 1) * separatorLength
	}
	return length
}

func typePackValueExpressions(tree syntaxTree, values []arenaTypeParam) []typeID {
	expressions := make([]typeID, 0, len(values))
	for _, value := range values {
		expressions = append(expressions, tree.typeParamValue(value))
	}
	return expressions
}

func typeArgumentSummaryDisplay(tree syntaxTree, args []typeID) string {
	summaries := typeSummariesFromExpressions(tree, args)
	return "<" + joinTypeDisplays(summaries, ", ") + ">"
}

func typeSingletonDisplay(tree syntaxTree, expr typeID) string {
	kind, payload, ok := tree.typeSingletonScalar(expr)
	if !ok {
		return "unknown"
	}
	switch kind {
	case BoolKind:
		if payload != 0 {
			return "true"
		}
		return "false"
	case NumberKind:
		return fmt.Sprintf("%g", math.Float64frombits(payload))
	case StringKind:
		id, ok := tree.typeSingletonStringID(expr)
		if !ok {
			return "unknown"
		}
		value, ok := tree.stringValue(id)
		if !ok {
			return "unknown"
		}
		return fmt.Sprintf("%q", value)
	default:
		return "unknown"
	}
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
