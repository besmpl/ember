package ember

type globalEnv struct {
	values map[string]Value
	host   map[string]Value
	thread *vmThread
}

func runtimeGlobals(globals map[string]Value) *globalEnv {
	env := &globalEnv{
		host: globals,
	}
	if len(globals) != 0 {
		env.values = make(map[string]Value, len(globals))
		for name, value := range globals {
			env.values[name] = value
		}
	}
	return env
}

func (env *globalEnv) get(name string) (Value, bool) {
	if env == nil {
		return NilValue(), false
	}
	if value, ok := env.values[name]; ok {
		return value, true
	}
	value, ok := baseGlobalValue(name)
	if !ok {
		return NilValue(), false
	}
	if !baseGlobalNeedsCache(name) {
		return value, true
	}
	env.ensureValues()
	env.values[name] = value
	return value, true
}

func (env *globalEnv) nativeGlobalUnchanged(name string, nativeID nativeFuncID) bool {
	if env == nil {
		return false
	}
	if env.values != nil {
		if value, ok := env.values[name]; ok {
			return value.nativeID == nativeID
		}
	}
	return true
}

func (env *globalEnv) set(name string, value Value) {
	if env == nil {
		return
	}
	env.ensureValues()
	env.values[name] = value
	if env.host != nil {
		env.host[name] = value
	}
}

func (env *globalEnv) ensureValues() {
	if env.values == nil {
		env.values = make(map[string]Value)
	}
}

func baseGlobalValue(name string) (Value, bool) {
	switch name {
	case "type":
		return HostFuncValue(baseType), true
	case "tonumber":
		return HostFuncValue(baseToNumber), true
	case "tostring":
		return nativeFuncValue(baseToString), true
	case "setmetatable":
		return nativeFuncValue(baseSetMetatable), true
	case "getmetatable":
		return nativeFuncValue(baseGetMetatable), true
	case "next":
		return HostFuncValue(baseNext), true
	case "pairs":
		return HostFuncValue(basePairs), true
	case "ipairs":
		return HostFuncValue(baseIPairs), true
	case "rawget":
		return HostFuncValue(baseRawGet), true
	case "rawset":
		return HostFuncValue(baseRawSet), true
	case "rawlen":
		return nativeFuncValueWithID(baseRawLenNative, nativeFuncRawLen), true
	case "select":
		return nativeFuncValueWithID(baseSelectNative, nativeFuncSelect), true
	case "unpack":
		return HostFuncValue(baseTableUnpack), true
	case "pcall":
		return nativeFuncValue(basePCall), true
	case "xpcall":
		return nativeFuncValue(baseXPCall), true
	case "math":
		return TableValue(baseMath()), true
	case "table":
		return TableValue(baseTable()), true
	case "coroutine":
		return TableValue(baseCoroutine()), true
	default:
		return NilValue(), false
	}
}

func baseGlobalNeedsCache(name string) bool {
	switch name {
	case "math", "table", "coroutine":
		return true
	default:
		return false
	}
}
