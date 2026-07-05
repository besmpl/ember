package ember

import "strings"

type typeEnv struct {
	globals map[string]globalTypeFact
}

type globalTypeFact struct {
	typ         simpleType
	table       tableFact
	function    functionFact
	hasFunction bool
}

func defaultTypeEnv() typeEnv {
	env := typeEnv{globals: make(map[string]globalTypeFact)}
	for _, name := range []string{"assert", "require"} {
		env.globals[name] = globalTypeFact{typ: simpleTypeUnknown}
	}
	addBaseGlobalTypeFacts(&env)
	return env
}

func (e typeEnv) lookup(name string) (globalTypeFact, bool) {
	if e.globals == nil {
		return globalTypeFact{}, false
	}
	fact, ok := e.globals[name]
	return fact, ok
}

func (e *typeEnv) setGlobalSummary(name string, summary TypeSummary) {
	if name == "" {
		return
	}
	if e.globals == nil {
		e.globals = make(map[string]globalTypeFact)
	}
	e.globals[name] = globalFactFromSummary(cloneTypeSummary(summary))
}

func globalFactFromSummary(summary TypeSummary) globalTypeFact {
	fact := globalTypeFact{typ: simpleTypeFromSummary(summary)}
	if summary.Kind == TypeSummaryFunction || summary.Kind == TypeSummaryGenericFunction {
		fact.function = functionFactFromSummary(summary)
		fact.hasFunction = true
	}
	if summary.Kind == TypeSummaryTable {
		fact.table = tableFactFromSummary(summary)
	}
	return fact
}

func functionFactFromSummary(summary TypeSummary) functionFact {
	params := summary.Params
	if len(params) == 0 {
		params = summary.ParamPack.Head
	}
	fact := functionFact{
		typeParams:    append([]string(nil), summary.TypeParams...),
		params:        make([]simpleType, 0, len(params)),
		paramGenerics: make([]string, 0, len(params)),
		returnType:    simpleTypeUnknown,
	}
	for _, param := range params {
		fact.params = append(fact.params, simpleTypeFromSummary(param))
		fact.paramGenerics = append(fact.paramGenerics, genericSummaryName(param, summary.TypeParams))
	}
	if summary.ParamPack.Tail != nil {
		fact.variadic = simpleTypeFromSummary(*summary.ParamPack.Tail)
		if summary.ParamPack.Tail.Kind == TypeSummaryVariadic && summary.ParamPack.Tail.Inner != nil {
			fact.variadic = simpleTypeFromSummary(*summary.ParamPack.Tail.Inner)
		}
		fact.variadicGeneric = genericSummaryName(*summary.ParamPack.Tail, summary.TypeParams)
	}
	if summary.Return != nil {
		fact.returnType = simpleTypeFromSummary(*summary.Return)
		fact.returnGeneric = genericSummaryName(*summary.Return, summary.TypeParams)
		return fact
	}
	if len(summary.ReturnPack.Head) != 0 {
		fact.returnType = simpleTypeFromSummary(summary.ReturnPack.Head[0])
		fact.returnGeneric = genericSummaryName(summary.ReturnPack.Head[0], summary.TypeParams)
	}
	return fact
}

func tableFactFromSummary(summary TypeSummary) tableFact {
	fact := tableFact{
		known:     true,
		fields:    make(map[string]simpleType, len(summary.Properties)),
		access:    make(map[string]string, len(summary.Properties)),
		functions: make(map[string]functionFact),
		indexers:  make([]tableIndexerFact, 0, len(summary.Indexers)),
	}
	for _, property := range summary.Properties {
		fact.fields[property.Name] = simpleTypeFromSummary(property.Type)
		if property.Access != "" {
			fact.access[property.Name] = property.Access
		}
		if property.Type.Kind == TypeSummaryFunction || property.Type.Kind == TypeSummaryGenericFunction {
			fact.functions[property.Name] = functionFactFromSummary(property.Type)
		}
	}
	for _, indexer := range summary.Indexers {
		fact.indexers = append(fact.indexers, tableIndexerFact{
			key:    simpleTypeFromSummary(indexer.Key),
			value:  simpleTypeFromSummary(indexer.Value),
			access: indexer.Access,
		})
	}
	mergeMetatableIndexSummary(&fact, summary.Metatable)
	return fact
}

func mergeMetatableIndexSummary(fact *tableFact, metatable *TypeSummary) {
	if fact == nil || metatable == nil || metatable.Kind != TypeSummaryTable {
		return
	}
	indexSummary, ok := tableSummaryProperty(*metatable, "__index")
	if !ok || indexSummary.Kind != TypeSummaryTable {
		return
	}
	indexFact := tableFactFromSummary(indexSummary)
	for field, typ := range indexFact.fields {
		if _, exists := fact.fields[field]; exists {
			continue
		}
		fact.fields[field] = typ
		if access := indexFact.access[field]; access != "" {
			fact.access[field] = access
		}
		if function, ok := indexFact.functions[field]; ok {
			fact.functions[field] = function
		}
	}
	fact.indexers = append(fact.indexers, indexFact.indexers...)
}

func simpleTypeFromSummary(summary TypeSummary) simpleType {
	switch summary.Kind {
	case TypeSummaryName:
		return simpleTypeName(summary.Display)
	case TypeSummaryUnion:
		typ := simpleTypeUnknown
		for _, option := range summary.Types {
			typ = unionSimpleTypes(typ, simpleTypeFromSummary(option))
		}
		return typ
	case TypeSummaryNilable:
		if summary.Inner == nil {
			return simpleTypeUnknown
		}
		inner := simpleTypeFromSummary(*summary.Inner)
		if inner == simpleTypeUnknown {
			return simpleTypeUnknown
		}
		return unionSimpleTypes(inner, simpleTypeNil)
	case TypeSummaryTable:
		return simpleTypeTable
	case TypeSummaryFunction, TypeSummaryGenericFunction:
		return simpleTypeFunction
	case TypeSummaryVariadic:
		if summary.Inner == nil {
			return simpleTypeUnknown
		}
		return simpleTypeFromSummary(*summary.Inner)
	case TypeSummarySingleton:
		return simpleTypeFromSingletonDisplay(summary.Display)
	case TypeSummaryUnknown:
		return simpleTypeUnknown
	case TypeSummaryNever:
		return simpleTypeNever
	default:
		return simpleTypeUnknown
	}
}

func simpleTypeName(display string) simpleType {
	switch display {
	case "any":
		return simpleTypeAny
	case "unknown":
		return simpleTypeCheckedUnknown
	case "never":
		return simpleTypeNever
	case "nil":
		return simpleTypeNil
	case "boolean":
		return simpleTypeBoolean
	case "number":
		return simpleTypeNumber
	case "string":
		return simpleTypeString
	case "table":
		return simpleTypeTable
	case "function":
		return simpleTypeFunction
	case "thread":
		return simpleTypeThread
	case "userdata":
		return simpleTypeUserData
	case "buffer":
		return simpleTypeBuffer
	case "vector":
		return simpleTypeVector
	default:
		return simpleTypeUnknown
	}
}

func simpleTypeFromSingletonDisplay(display string) simpleType {
	switch {
	case display == "true" || display == "false":
		return simpleTypeBoolean
	case display == "nil":
		return simpleTypeNil
	case strings.HasPrefix(display, "\"") && strings.HasSuffix(display, "\""):
		return simpleTypeString
	default:
		return simpleTypeUnknown
	}
}

func genericSummaryName(summary TypeSummary, typeParams []string) string {
	if summary.Kind != TypeSummaryName {
		return ""
	}
	for _, param := range typeParams {
		if summary.Display == param {
			return param
		}
	}
	return ""
}
