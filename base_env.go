package ember

type globalEnv struct {
	values  map[string]Value
	host    map[string]Value
	thread  *vmThread
	version uint64
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
		env.version = 1
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
	env.version++
	if env.host != nil {
		env.host[name] = value
	}
}

func (env *globalEnv) ensureValues() {
	if env.values == nil {
		env.values = make(map[string]Value)
	}
}
