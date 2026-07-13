package ember

type scopeFrame struct {
	locals map[string]simpleType
	tables map[string]tableFact
}

func (a *analysisState) pushScope() {
	a.scopes = append(a.scopes, make(map[string]simpleType))
	a.tableScopes = append(a.tableScopes, make(map[string]tableFact))
	a.aliasScopes = append(a.aliasScopes, make(map[string]typeAliasFact))
	a.moduleScopes = append(a.moduleScopes, make(map[string]ModuleSummary))
}

func (a *analysisState) popScope() scopeFrame {
	frame := scopeFrame{
		locals: a.currentScope(),
		tables: a.currentTableScope(),
	}
	a.scopes = a.scopes[:len(a.scopes)-1]
	a.tableScopes = a.tableScopes[:len(a.tableScopes)-1]
	a.aliasScopes = a.aliasScopes[:len(a.aliasScopes)-1]
	a.moduleScopes = a.moduleScopes[:len(a.moduleScopes)-1]
	return frame
}

func (a *analysisState) currentScope() map[string]simpleType {
	return a.scopes[len(a.scopes)-1]
}

func (a *analysisState) currentTableScope() map[string]tableFact {
	return a.tableScopes[len(a.tableScopes)-1]
}

func (a *analysisState) currentAliasScope() map[string]typeAliasFact {
	return a.aliasScopes[len(a.aliasScopes)-1]
}

func (a *analysisState) currentModuleScope() map[string]ModuleSummary {
	return a.moduleScopes[len(a.moduleScopes)-1]
}

func (a *analysisState) defineLocal(name string, typ simpleType) {
	a.currentScope()[name] = typ
}

func (a *analysisState) applyBranchLocalFlowJoins(left, right map[string]simpleType) {
	for name, leftType := range left {
		rightType, ok := right[name]
		if !ok || !a.hasLocal(name) {
			continue
		}
		joined := unionSimpleTypes(leftType, rightType)
		if joined == simpleTypeUnknown {
			continue
		}
		a.defineLocal(name, joined)
	}
}

func (a *analysisState) applyBranchTableFlowJoins(left, right map[string]tableFact) {
	for name, leftFact := range left {
		rightFact, ok := right[name]
		if !ok || !a.hasTableFact(name) {
			continue
		}
		for field, leftType := range leftFact.fields {
			rightType, ok := rightFact.fields[field]
			if !ok {
				continue
			}
			joined := unionSimpleTypes(leftType, rightType)
			if joined == simpleTypeUnknown {
				continue
			}
			a.defineTableField(name, field, joined)
		}
	}
}

func (a *analysisState) hasLocal(name string) bool {
	for i := len(a.scopes) - 1; i >= 0; i-- {
		if _, ok := a.scopes[i][name]; ok {
			return true
		}
	}
	return false
}

func (a *analysisState) hasTableFact(name string) bool {
	for i := len(a.tableScopes) - 1; i >= 0; i-- {
		if _, ok := a.tableScopes[i][name]; ok {
			return true
		}
	}
	return false
}

func (a *analysisState) defineTypeAlias(stmt arenaTypeAliasStatement) {
	tree := a.tree
	name, _ := tree.stringValue(stmt.name)
	value, _ := tree.statementType(stmt.value)
	a.currentAliasScope()[name] = typeAliasFact{
		typeParams: consumerStatementStrings(tree, stmt.typeParams),
		typePacks:  consumerStatementStrings(tree, stmt.typePacks),
		value:      value,
	}
}

func (a *analysisState) defineModuleLocal(name string, summary ModuleSummary) {
	a.currentModuleScope()[name] = cloneModuleSummary(summary)
}

func (a *analysisState) defineTableLocal(name string, fact tableFact) {
	if fact.empty() {
		delete(a.currentTableScope(), name)
		return
	}
	a.currentTableScope()[name] = fact
}

func (a *analysisState) defineTableField(name string, field string, typ simpleType) {
	fact, ok := a.lookupTableFact(name)
	if !ok {
		return
	}
	fields := make(map[string]simpleType, len(fact.fields)+1)
	for existingField, fieldType := range fact.fields {
		fields[existingField] = fieldType
	}
	fields[field] = typ
	access := make(map[string]string, len(fact.access))
	for existingField, fieldAccess := range fact.access {
		access[existingField] = fieldAccess
	}
	functions := make(map[string]functionFact, len(fact.functions))
	for existingField, function := range fact.functions {
		functions[existingField] = function
	}
	a.currentTableScope()[name] = tableFact{
		known:     true,
		fields:    fields,
		access:    access,
		functions: functions,
		indexers:  append([]tableIndexerFact(nil), fact.indexers...),
	}
}

func (f tableFact) empty() bool {
	return !f.known
}

func (a *analysisState) lookupLocal(name string) simpleType {
	for i := len(a.scopes) - 1; i >= 0; i-- {
		if typ, ok := a.scopes[i][name]; ok {
			return typ
		}
	}
	return simpleTypeUnknown
}

func (a *analysisState) lookupTableField(name, field string) simpleType {
	fact, ok := a.lookupTableFact(name)
	if !ok {
		return simpleTypeUnknown
	}
	return fact.fields[field]
}

func (a *analysisState) lookupTableFields(name string) (map[string]simpleType, bool) {
	fact, ok := a.lookupTableFact(name)
	if !ok {
		return nil, false
	}
	return fact.fields, true
}

func (a *analysisState) lookupTableFact(name string) (tableFact, bool) {
	for i := len(a.tableScopes) - 1; i >= 0; i-- {
		fact, ok := a.tableScopes[i][name]
		if !ok {
			continue
		}
		return fact, true
	}
	if a.hasLocal(name) {
		return tableFact{}, false
	}
	if fact, ok := a.typeEnv.lookup(name); ok && fact.table.known {
		return fact.table, true
	}
	return tableFact{}, false
}

func (a *analysisState) lookupTypeAlias(name string) (typeAliasFact, bool) {
	for i := len(a.aliasScopes) - 1; i >= 0; i-- {
		alias, ok := a.aliasScopes[i][name]
		if ok {
			return alias, true
		}
	}
	return typeAliasFact{}, false
}

func (a *analysisState) lookupModuleLocal(name string) (ModuleSummary, bool) {
	for i := len(a.moduleScopes) - 1; i >= 0; i-- {
		summary, ok := a.moduleScopes[i][name]
		if ok {
			return summary, true
		}
	}
	return ModuleSummary{}, false
}

func (a *analysisState) lookupModuleExportedTypeAlias(moduleName, aliasName string) (ModuleExport, bool) {
	summary, ok := a.lookupModuleLocal(moduleName)
	if !ok {
		return ModuleExport{}, false
	}
	return moduleExportByNameKind(summary.Exports, aliasName, ModuleExportTypeAlias)
}
