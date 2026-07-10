package ember

import "sync"

type baseGlobalDefinition struct {
	name    string
	value   func() Value
	cache   bool
	summary func() TypeSummary
}

type baseFieldIntrinsicDefinition struct {
	globalName string
	field      string
	op         opcode
	nativeID   nativeFuncID
	nativeName string
}

type nativeFuncDefinition struct {
	id   nativeFuncID
	name string
}

var (
	baseGlobalDefinitionsOnce  sync.Once
	baseGlobalDefinitionsCache []baseGlobalDefinition
	baseIntrinsicsOnce         sync.Once
	baseFieldIntrinsicsCache   []baseFieldIntrinsicDefinition
	nativeFuncDefinitionsCache []nativeFuncDefinition
)

func baseGlobalDefinitions() []baseGlobalDefinition {
	baseGlobalDefinitionsOnce.Do(func() {
		baseGlobalDefinitionsCache = []baseGlobalDefinition{
			{name: "type", value: func() Value { return HostFuncValue(baseType) }, summary: baseTypeSummary},
			{name: "tonumber", value: func() Value { return HostFuncValue(baseToNumber) }},
			{name: "tostring", value: func() Value { return nativeFuncValueWithID(baseToString, nativeFuncToString) }},
			{name: "setmetatable", value: func() Value { return nativeFuncValue(baseSetMetatable) }},
			{name: "getmetatable", value: func() Value { return nativeFuncValue(baseGetMetatable) }},
			{name: "next", value: func() Value { return nativeFuncValueWithID(baseNextNative, nativeFuncNext) }},
			{name: "pairs", value: func() Value { return HostFuncValue(basePairs) }},
			{name: "ipairs", value: func() Value { return HostFuncValue(baseIPairs) }},
			{name: "rawget", value: func() Value { return HostFuncValue(baseRawGet) }},
			{name: "rawset", value: func() Value { return HostFuncValue(baseRawSet) }},
			{name: "rawlen", value: func() Value { return nativeFuncValueWithID(baseRawLenNative, nativeFuncRawLen) }},
			{name: "select", value: func() Value { return nativeFuncValueWithID(baseSelectNative, nativeFuncSelect) }},
			{name: "unpack", value: func() Value { return HostFuncValue(baseTableUnpack) }},
			{name: "pcall", value: func() Value { return nativeFuncValue(basePCall) }},
			{name: "xpcall", value: func() Value { return nativeFuncValue(baseXPCall) }},
			{name: "math", value: func() Value { return TableValue(baseMath()) }, cache: true},
			{name: "table", value: func() Value { return TableValue(baseTable()) }, cache: true},
			{name: "coroutine", value: func() Value { return TableValue(baseCoroutine()) }, cache: true, summary: coroutineLibrarySummary},
		}
	})
	return baseGlobalDefinitionsCache
}

func baseFieldIntrinsics() []baseFieldIntrinsicDefinition {
	baseIntrinsicsOnce.Do(func() {
		baseFieldIntrinsicsCache = []baseFieldIntrinsicDefinition{
			{globalName: "table", field: "insert", op: opFastCall, nativeID: nativeFuncTableInsert, nativeName: "TABLE_INSERT"},
			{globalName: "table", field: "remove", op: opFastCall, nativeID: nativeFuncTableRemove, nativeName: "TABLE_REMOVE"},
			{globalName: "coroutine", field: "resume", op: opFastCall, nativeID: nativeFuncCoroutineResume, nativeName: "COROUTINE_RESUME"},
			{globalName: "math", field: "min", op: opFastCall, nativeID: nativeFuncMathMin, nativeName: "MATH_MIN"},
		}
		nativeFuncDefinitionsCache = []nativeFuncDefinition{
			{id: nativeFuncSelect, name: "SELECT"},
			{id: nativeFuncRawLen, name: "RAW_LEN"},
			{id: nativeFuncToString, name: "TOSTRING"},
			{id: nativeFuncNext, name: "NEXT"},
			{id: nativeFuncArrayNext, name: "ARRAY_NEXT"},
			{id: nativeFuncTableNext, name: "TABLE_NEXT"},
		}
		for _, intrinsic := range baseFieldIntrinsicsCache {
			nativeFuncDefinitionsCache = append(nativeFuncDefinitionsCache, nativeFuncDefinition{
				id:   intrinsic.nativeID,
				name: intrinsic.nativeName,
			})
		}
	})
	return baseFieldIntrinsicsCache
}

func baseGlobalDefinitionFor(name string) (baseGlobalDefinition, bool) {
	for _, definition := range baseGlobalDefinitions() {
		if definition.name == name {
			return definition, true
		}
	}
	return baseGlobalDefinition{}, false
}

func baseFieldIntrinsic(globalName string, field string) (baseFieldIntrinsicDefinition, bool) {
	for _, intrinsic := range baseFieldIntrinsics() {
		if intrinsic.globalName == globalName && intrinsic.field == field {
			return intrinsic, true
		}
	}
	return baseFieldIntrinsicDefinition{}, false
}

func baseNativeFuncName(nativeID nativeFuncID) (string, bool) {
	baseFieldIntrinsics()
	for _, definition := range nativeFuncDefinitionsCache {
		if definition.id == nativeID {
			return definition.name, true
		}
	}
	return "", false
}

func nativeFuncByID(nativeID nativeFuncID) (nativeFunc, bool) {
	switch nativeID {
	case nativeFuncSelect:
		return baseSelectNative, true
	case nativeFuncTableInsert:
		return baseTableInsertNative, true
	case nativeFuncTableRemove:
		return baseTableRemoveNative, true
	case nativeFuncCoroutineStatus:
		return baseCoroutineStatusNative, true
	case nativeFuncCoroutineResume:
		return baseCoroutineResume, true
	case nativeFuncMathMin:
		return baseMathMinNative, true
	case nativeFuncRawLen:
		return baseRawLenNative, true
	case nativeFuncToString:
		return baseToString, true
	case nativeFuncNext:
		return baseNextNative, true
	case nativeFuncArrayNext:
		return baseArrayNextNative, true
	case nativeFuncTableNext:
		return baseTableNextNative, true
	default:
		return nil, false
	}
}

func baseGlobalValue(name string) (Value, bool) {
	definition, ok := baseGlobalDefinitionFor(name)
	if !ok || definition.value == nil {
		return NilValue(), false
	}
	return definition.value(), true
}

func baseGlobalNeedsCache(name string) bool {
	definition, ok := baseGlobalDefinitionFor(name)
	return ok && definition.cache
}

func addBaseGlobalTypeFacts(env *typeEnv) {
	if env == nil {
		return
	}
	if env.globals == nil {
		env.globals = make(map[string]globalTypeFact)
	}
	for _, definition := range baseGlobalDefinitions() {
		if definition.summary == nil {
			env.globals[definition.name] = globalTypeFact{typ: simpleTypeUnknown}
			continue
		}
		env.setGlobalSummary(definition.name, definition.summary())
	}
}

func baseGlobalValueSummaries() map[string]TypeSummary {
	summaries := make(map[string]TypeSummary)
	for _, definition := range baseGlobalDefinitions() {
		if definition.summary != nil {
			summaries[definition.name] = definition.summary()
		}
	}
	return summaries
}

func baseTypeSummary() TypeSummary {
	return functionSummary(
		[]TypeSummary{{Kind: TypeSummaryUnknown, Display: "unknown"}},
		TypeSummary{Kind: TypeSummaryName, Display: "string"},
	)
}

func coroutineLibrarySummary() TypeSummary {
	return TypeSummary{
		Kind:    TypeSummaryTable,
		Display: "table",
		Properties: []TablePropertySummary{
			{Name: "create", Type: functionSummary([]TypeSummary{functionSummary(nil, unknownSummary())}, namedTypeSummary("thread"))},
			{Name: "resume", Type: variadicFunctionSummary(
				[]TypeSummary{namedTypeSummary("thread")},
				unknownSummary(),
				[]TypeSummary{namedTypeSummary("boolean")},
				unknownSummary(),
			)},
			{Name: "yield", Type: variadicFunctionSummary(nil, unknownSummary(), nil, unknownSummary())},
			{Name: "status", Type: functionSummary([]TypeSummary{namedTypeSummary("thread")}, namedTypeSummary("string"))},
			{Name: "close", Type: functionSummary([]TypeSummary{namedTypeSummary("thread")}, namedTypeSummary("boolean"))},
			{Name: "running", Type: functionSummary(nil, namedTypeSummary("thread"))},
			{Name: "isyieldable", Type: functionSummary(nil, namedTypeSummary("boolean"))},
			{Name: "wrap", Type: functionSummary([]TypeSummary{functionSummary(nil, unknownSummary())}, functionSummary(nil, unknownSummary()))},
		},
	}
}

func functionSummary(params []TypeSummary, ret TypeSummary) TypeSummary {
	summary := TypeSummary{
		Kind:      TypeSummaryFunction,
		Display:   "function",
		Params:    append([]TypeSummary(nil), params...),
		ParamPack: typePackSummary(params, nil),
		ReturnPack: typePackSummary(
			[]TypeSummary{ret},
			nil,
		),
	}
	summary.Return = &ret
	return summary
}

func variadicFunctionSummary(params []TypeSummary, paramTail TypeSummary, returns []TypeSummary, returnTail TypeSummary) TypeSummary {
	ret := unknownSummary()
	if len(returns) > 0 {
		ret = returns[0]
	}
	summary := TypeSummary{
		Kind:       TypeSummaryFunction,
		Display:    "function",
		Params:     append([]TypeSummary(nil), params...),
		ParamPack:  typePackSummary(params, &paramTail),
		ReturnPack: typePackSummary(returns, &returnTail),
	}
	summary.Return = &ret
	return summary
}

func typePackSummary(head []TypeSummary, tail *TypeSummary) TypePackSummary {
	summary := TypePackSummary{
		Kind:    TypeSummaryFunction,
		Display: joinTypeDisplays(head, ", "),
		Head:    append([]TypeSummary(nil), head...),
	}
	if tail != nil {
		tailCopy := *tail
		summary.Tail = &tailCopy
		if summary.Display == "" {
			summary.Display = "..."
		}
	}
	return summary
}

func namedTypeSummary(name string) TypeSummary {
	return TypeSummary{Kind: TypeSummaryName, Display: name}
}

func unknownSummary() TypeSummary {
	return TypeSummary{Kind: TypeSummaryUnknown, Display: "unknown"}
}
