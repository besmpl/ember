package ember

type globalEnv struct {
	values map[string]Value
	host   map[string]Value
	slots  []globalSlot
	// require is injected for runtime-owned invocation environments without
	// pretending it is a script assignment. A script write to the same name
	// still wins through values, preserving normal global lookup precedence.
	require    Value
	hasRequire bool
	thread     *vmThread
	// scope carries the private invocation capability into a VM entry. The
	// active thread owns the dynamic copy used by host adapters, so yielded
	// coroutines can replace context and controller state on resume.
	scope    invocationScope
	hasScope bool
	// controller is the active invocation capability for native/base callbacks.
	// It is installed only for the duration of an execution and is never a
	// table-owned policy.
	controller *executionController
	owner      *runtimeOwner
	version    uint64
	pooled     bool
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

func runtimeGlobalsWithInvocation(globals map[string]Value, owner *runtimeOwner, scope invocationScope) *globalEnv {
	env := runtimeGlobalsWithOwner(globals, owner)
	env.scope = scope
	env.hasScope = true
	return env
}

func (env *globalEnv) invocationScope() (invocationScope, bool) {
	if env == nil {
		return invocationScope{}, false
	}
	if env.thread != nil && env.thread.hasScope {
		return env.thread.scope, true
	}
	if !env.hasScope {
		return invocationScope{}, false
	}
	return env.scope, true
}

func (env *globalEnv) clearInvocationScope() {
	if env == nil {
		return
	}
	env.scope = invocationScope{}
	env.hasScope = false
}

func invocationScopeFromGlobalEnv(env *globalEnv) (invocationScope, bool) {
	if env == nil {
		return invocationScope{}, false
	}
	return env.invocationScope()
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
	if name == "require" && env.hasRequire {
		return env.require, true
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
	if name == "require" && env.hasRequire {
		return valueNativeID(env.require) == nativeID
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
	if name == "require" && env.hasRequire {
		return env.require, true
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

// setRequire installs the private runtime require capability. It deliberately
// does not populate values, so a pooled nil-host invocation can stay free of a
// global map allocation. A subsequent script assignment to require is stored
// in values and therefore takes precedence over this capability.
func (env *globalEnv) setRequire(value Value) {
	if env == nil {
		return
	}
	env.require = value
	env.hasRequire = true
	env.version++
	if env.host != nil {
		env.host["require"] = value
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

// resetForInvocation prepares a reusable environment for one fresh runtime
// invocation. The host map is owned by the invocation and is intentionally
// not cleared here; callbacks may retain that copied snapshot.
func (env *globalEnv) resetForInvocation(host map[string]Value, owner *runtimeOwner, scope invocationScope, require Value) {
	if env == nil {
		return
	}
	values := env.values
	if values != nil {
		clear(values)
	}
	slots := env.slots[:0]
	pooled := env.pooled
	*env = globalEnv{
		values:   values,
		host:     host,
		slots:    slots,
		owner:    owner,
		pooled:   pooled,
		scope:    scope,
		hasScope: true,
	}
	if len(host) != 0 {
		env.version = 1
	}
	env.setRequire(require)
}

// releaseReusable removes all invocation-owned references before returning a
// thread to the pool. Host maps are snapshots that may be retained by
// callbacks, so the map itself is released but never cleared here.
func (env *globalEnv) releaseReusable() {
	if env == nil {
		return
	}
	if env.values != nil {
		clear(env.values)
	}
	for i := range env.slots {
		env.slots[i] = globalSlot{}
	}
	env.slots = env.slots[:0]
	env.host = nil
	env.require = Value{}
	env.hasRequire = false
	env.thread = nil
	env.scope = invocationScope{}
	env.hasScope = false
	env.controller = nil
	env.owner = nil
	env.version = 0
	env.pooled = true
}
