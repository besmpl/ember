package ember

type globalEnv struct {
	values  map[string]Value
	host    map[string]Value
	slots   []globalSlot
	thread  *vmThread
	owner   *runtimeOwner
	version uint64
	pooled  bool
}

type globalSlot struct {
	name    string
	value   Value
	version uint64
	ok      bool
	ready   bool
}

func runtimeGlobals(globals map[string]Value) *globalEnv {
	env := &globalEnv{
		host: globals,
	}
	if len(globals) != 0 {
		env.version = 1
	}
	return env
}

func runtimeGlobalsWithOwner(globals map[string]Value, owner *runtimeOwner) *globalEnv {
	env := runtimeGlobals(globals)
	env.owner = owner
	return env
}

func (env *globalEnv) getSlot(slot int, name string) (Value, bool, bool) {
	if env == nil || slot < 0 {
		value, ok := env.get(name)
		return value, ok, false
	}
	if slot < len(env.slots) {
		cached := env.slots[slot]
		if cached.ready && cached.version == env.version && cached.name == name {
			return cached.value, cached.ok, true
		}
	}
	value, ok := env.get(name)
	env.storeSlot(slot, name, value, ok)
	return value, ok, false
}

func (env *globalEnv) get(name string) (Value, bool) {
	if env == nil {
		return NilValue(), false
	}
	if value, ok := env.values[name]; ok {
		return value, true
	}
	if value, ok := env.hostValue(name); ok {
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
			return valueNativeID(value) == nativeID
		}
	}
	if value, ok := env.hostValue(name); ok {
		return valueNativeID(value) == nativeID
	}
	return true
}

func (env *globalEnv) overrideValue(name string) (Value, bool) {
	if env == nil {
		return NilValue(), false
	}
	if env.values != nil {
		if value, ok := env.values[name]; ok {
			return value, true
		}
	}
	return env.hostValue(name)
}

func (env *globalEnv) setSlot(slot int, name string, value Value) {
	env.set(name, value)
	env.storeSlot(slot, name, value, true)
}

func (env *globalEnv) hostValue(name string) (Value, bool) {
	if env == nil || env.host == nil {
		return NilValue(), false
	}
	value, ok := env.host[name]
	return value, ok
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

func (env *globalEnv) storeSlot(slot int, name string, value Value, ok bool) {
	if env == nil || slot < 0 {
		return
	}
	env.ensureSlots(slot + 1)
	env.slots[slot] = globalSlot{
		name:    name,
		value:   value,
		version: env.version,
		ok:      ok,
		ready:   true,
	}
}

func (env *globalEnv) ensureSlots(count int) {
	if len(env.slots) >= count {
		return
	}
	slots := make([]globalSlot, count)
	copy(slots, env.slots)
	env.slots = slots
}
