package ember

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
