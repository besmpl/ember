package ember

func enrichModuleSummaryFromRequireBindings(summary ModuleSummary, node moduleGraphNode, summaries map[moduleKey]moduleSummaryArtifact) ModuleSummary {
	if node.ReturnLocal == "" {
		return summary
	}
	exportType, ok := requireBindingExportType(node, summaries)
	if !ok {
		return summary
	}
	for i, item := range summary.Exports {
		if item.Name == "return" && item.Kind == ModuleExportValue {
			summary.Exports[i].Type = exportType
			return summary
		}
	}
	summary.Exports = append(summary.Exports, ModuleExport{
		Name: "return",
		Kind: ModuleExportValue,
		Type: exportType,
	})
	return summary
}

func requireBindingExportType(node moduleGraphNode, summaries map[moduleKey]moduleSummaryArtifact) (TypeSummary, bool) {
	if required, ok := node.RequireBindings[node.ReturnLocal]; ok {
		exportType, ok := dependencyReturnExportType(summaries, required)
		if !ok {
			return TypeSummary{}, false
		}
		if node.ReturnField != "" {
			fieldType, ok := tableSummaryProperty(exportType, node.ReturnField)
			if !ok {
				return TypeSummary{}, false
			}
			exportType = fieldType
		}
		return exportType, true
	}

	if binding, ok := node.RequireFieldBindings[node.ReturnLocal]; ok {
		exportType, ok := dependencyReturnExportType(summaries, binding.Required)
		if !ok {
			return TypeSummary{}, false
		}
		fieldType, ok := tableSummaryProperty(exportType, binding.Field)
		if !ok {
			return TypeSummary{}, false
		}
		if node.ReturnField != "" {
			fieldType, ok = tableSummaryProperty(fieldType, node.ReturnField)
			if !ok {
				return TypeSummary{}, false
			}
		}
		return fieldType, true
	}

	return TypeSummary{}, false
}

func dependencyReturnExportType(summaries map[moduleKey]moduleSummaryArtifact, required moduleKey) (TypeSummary, bool) {
	dependency, ok := summaries[required]
	if !ok || !dependency.Trusted {
		return TypeSummary{}, false
	}
	exported, ok := moduleExportByNameKind(dependency.Summary.Exports, "return", ModuleExportValue)
	if !ok {
		return TypeSummary{}, false
	}
	return exported.Type, true
}
