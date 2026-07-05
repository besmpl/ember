package ember

import (
	"fmt"
	"strings"
)

func typeSummaryFromAlias(alias typeAliasStatement) TypeSummary {
	summary := typeSummaryFromExpression(alias.value)
	summary.TypeParams = append([]string(nil), alias.typeParams...)
	summary.TypePacks = append([]string(nil), alias.typePacks...)
	return summary
}

func typeSummaryFromExpression(expr *typeExpression) TypeSummary {
	if expr == nil {
		return TypeSummary{
			Kind:    TypeSummaryUnknown,
			Display: "unknown",
		}
	}
	switch expr.kind {
	case typeKindName:
		display := strings.Join(expr.name, ".")
		if display == "" {
			display = "unknown"
		}
		if len(expr.typeArgs) > 0 {
			display += typeArgumentSummaryDisplay(expr.typeArgs)
		}
		return TypeSummary{Kind: TypeSummaryName, Display: display}
	case typeKindUnion:
		types := typeSummariesFromExpressions(expr.types)
		return TypeSummary{Kind: TypeSummaryUnion, Display: joinTypeDisplays(types, " | "), Types: types}
	case typeKindIntersection:
		types := typeSummariesFromExpressions(expr.types)
		return TypeSummary{Kind: TypeSummaryIntersection, Display: joinTypeDisplays(types, " & "), Types: types}
	case typeKindNilable:
		inner := typeSummaryFromExpression(expr.inner)
		return TypeSummary{Kind: TypeSummaryNilable, Display: inner.Display + "?", Inner: &inner}
	case typeKindTable:
		return tableTypeSummary(expr)
	case typeKindFunction, typeKindGenericFunction:
		return functionTypeSummary(expr)
	case typeKindVariadic:
		inner := typeSummaryFromExpression(expr.inner)
		display := "..."
		if expr.inner != nil {
			display += inner.Display
		}
		return TypeSummary{Kind: TypeSummaryVariadic, Display: display, Inner: &inner}
	case typeKindGenericPack:
		display := strings.Join(expr.name, ".")
		if display == "" {
			display = "..."
		}
		return TypeSummary{Kind: TypeSummaryGenericPack, Display: display + "..."}
	case typeKindSingleton:
		if expr.literal == nil {
			return TypeSummary{Kind: TypeSummarySingleton, Display: "unknown"}
		}
		return TypeSummary{Kind: TypeSummarySingleton, Display: valueSummaryDisplay(*expr.literal)}
	case typeKindTypeof:
		return TypeSummary{Kind: TypeSummaryTypeof, Display: "typeof"}
	default:
		return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
	}
}

func typeSummariesFromExpressions(expressions []*typeExpression) []TypeSummary {
	summaries := make([]TypeSummary, len(expressions))
	for i, expr := range expressions {
		summaries[i] = typeSummaryFromExpression(expr)
	}
	return summaries
}

func tableTypeSummary(expr *typeExpression) TypeSummary {
	summary := TypeSummary{Kind: TypeSummaryTable, Display: "table"}
	for _, field := range expr.fields {
		value := typeSummaryFromExpression(field.value)
		if field.name != "" {
			summary.Properties = append(summary.Properties, TablePropertySummary{
				Name:   field.name,
				Access: field.access,
				Type:   value,
			})
			continue
		}
		if field.key != nil {
			summary.Indexers = append(summary.Indexers, TableIndexerSummary{
				Access: field.access,
				Key:    typeSummaryFromExpression(field.key),
				Value:  value,
			})
		}
	}
	return summary
}

func functionTypeSummary(expr *typeExpression) TypeSummary {
	kind := TypeSummaryFunction
	if expr.kind == typeKindGenericFunction {
		kind = TypeSummaryGenericFunction
	}
	params := typeSummariesFromExpressions(typePackValueExpressions(expr.params))
	ret := typeSummaryFromExpression(expr.returnType)
	summary := TypeSummary{
		Kind:       kind,
		Display:    "(" + joinTypeDisplays(params, ", ") + ") -> " + ret.Display,
		TypeParams: append([]string(nil), expr.typeParams...),
		TypePacks:  append([]string(nil), expr.typePacks...),
		Params:     params,
		Return:     &ret,
		ParamPack:  typePackSummary(params, nil),
		ReturnPack: typePackSummary([]TypeSummary{ret}, nil),
	}
	return summary
}

func typePackValueExpressions(values []typeFunctionParam) []*typeExpression {
	expressions := make([]*typeExpression, 0, len(values))
	for _, value := range values {
		expressions = append(expressions, value.value)
	}
	return expressions
}

func typeArgumentSummaryDisplay(args []*typeExpression) string {
	summaries := typeSummariesFromExpressions(args)
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
