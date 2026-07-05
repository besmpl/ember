package ember

type moduleSummaryEnv struct {
	summaries map[string]ModuleSummary
}

func moduleSummaryEnvFromMap(summaries map[string]ModuleSummary) moduleSummaryEnv {
	env := moduleSummaryEnv{summaries: make(map[string]ModuleSummary, len(summaries)*2)}
	for key, summary := range summaries {
		clone := cloneModuleSummary(summary)
		if key != "" {
			env.summaries[key] = clone
		}
		if summary.SourceName != "" {
			env.summaries[summary.SourceName] = clone
			if parsed, err := parseModuleKey(summary.SourceName); err == nil {
				env.summaries[parsed.String()] = clone
			}
		}
	}
	return env
}

func (e moduleSummaryEnv) returnExportForRequire(from Source, request string) (ModuleExport, bool) {
	summary, ok := e.summaryForRequire(from, request)
	if !ok {
		return ModuleExport{}, false
	}
	return moduleExportByNameKind(summary.Exports, "return", ModuleExportValue)
}

func (e moduleSummaryEnv) active() bool {
	return e.summaries != nil
}

func (e moduleSummaryEnv) summaryForRequire(from Source, request string) (ModuleSummary, bool) {
	if e.summaries == nil {
		return ModuleSummary{}, false
	}
	fromKey, err := parseModuleKey(from.Name)
	if err != nil {
		return ModuleSummary{}, false
	}
	required, err := normalizeRequireKey(fromKey, request)
	if err != nil {
		return ModuleSummary{}, false
	}
	summary, ok := e.summaries[required.String()]
	if !ok {
		summary, ok = e.summaries[required.path]
	}
	if !ok {
		return ModuleSummary{}, false
	}
	return summary, true
}

func (e moduleSummaryEnv) staleDependency(summary ModuleSummary) (ModuleDependencySummary, bool) {
	for _, dependency := range summary.Dependencies {
		if dependency.InvalidationHash == "" {
			continue
		}
		current, ok := e.summaryForDependency(dependency)
		if !ok || current.InvalidationHash == "" {
			continue
		}
		if current.InvalidationHash != dependency.InvalidationHash {
			return dependency, true
		}
	}
	return ModuleDependencySummary{}, false
}

func (e moduleSummaryEnv) summaryForDependency(dependency ModuleDependencySummary) (ModuleSummary, bool) {
	if e.summaries == nil {
		return ModuleSummary{}, false
	}
	for _, key := range []string{dependency.Key, dependency.Path} {
		if key == "" {
			continue
		}
		if summary, ok := e.summaries[key]; ok {
			return summary, true
		}
	}
	return ModuleSummary{}, false
}

func cloneModuleSummary(summary ModuleSummary) ModuleSummary {
	clone := summary
	clone.Exports = make([]ModuleExport, len(summary.Exports))
	for i, export := range summary.Exports {
		clone.Exports[i] = export
		clone.Exports[i].Type = cloneTypeSummary(export.Type)
		clone.Exports[i].DiagCodes = append([]string(nil), export.DiagCodes...)
	}
	clone.Dependencies = append([]ModuleDependencySummary(nil), summary.Dependencies...)
	clone.Diagnostics = append([]Diagnostic(nil), summary.Diagnostics...)
	return clone
}
