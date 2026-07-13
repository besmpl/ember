package ember

func tableSummaryProperty(summary TypeSummary, name string) (TypeSummary, bool) {
	for _, property := range summary.Properties {
		if property.Name == name {
			return property.Type, true
		}
	}
	return TypeSummary{}, false
}

func tableSummaryPath(tree syntaxTree, summary TypeSummary, selectors []arenaSelector) (TypeSummary, bool) {
	current := summary
	for i := range selectors {
		field := tree.termSelectorField(selectors[i])
		if field == "" || current.Kind != TypeSummaryTable {
			return TypeSummary{}, false
		}
		next, ok := tableSummaryProperty(current, field)
		if !ok {
			return TypeSummary{}, false
		}
		current = next
	}
	return current, true
}

func tableSummaryTermPath(tree syntaxTree, summary TypeSummary, selectors []arenaSelector) (TypeSummary, bool) {
	current := summary
	for _, selector := range selectors {
		field := tree.termSelectorField(selector)
		if field == "" || current.Kind != TypeSummaryTable {
			return TypeSummary{}, false
		}
		next, ok := tableSummaryProperty(current, field)
		if !ok {
			return TypeSummary{}, false
		}
		current = next
	}
	return current, true
}

func setTableSummaryProperty(summary *TypeSummary, name string, value TypeSummary) {
	for i := range summary.Properties {
		if summary.Properties[i].Name == name {
			summary.Properties[i].Type = value
			return
		}
	}
	summary.Properties = append(summary.Properties, TablePropertySummary{
		Name: name,
		Type: value,
	})
}

func setTableSummaryPath(tree syntaxTree, summary *TypeSummary, selectors []arenaSelector, value TypeSummary) bool {
	if len(selectors) == 0 {
		return false
	}
	field := tree.termSelectorField(selectors[0])
	if field == "" {
		return false
	}
	if len(selectors) == 1 {
		setTableSummaryProperty(summary, field, value)
		return true
	}
	for i := range summary.Properties {
		property := &summary.Properties[i]
		if property.Name != field || property.Type.Kind != TypeSummaryTable {
			continue
		}
		return setTableSummaryPath(tree, &property.Type, selectors[1:], value)
	}
	return false
}
