package ember

func cloneTypeSummary(summary TypeSummary) TypeSummary {
	clone := summary
	clone.TypeParams = append([]string(nil), summary.TypeParams...)
	clone.TypePacks = append([]string(nil), summary.TypePacks...)
	clone.Types = cloneTypeSummarySlice(summary.Types)
	if summary.Inner != nil {
		inner := cloneTypeSummary(*summary.Inner)
		clone.Inner = &inner
	}
	clone.Properties = make([]TablePropertySummary, len(summary.Properties))
	for i, property := range summary.Properties {
		clone.Properties[i] = TablePropertySummary{
			Name:   property.Name,
			Access: property.Access,
			Type:   cloneTypeSummary(property.Type),
		}
	}
	clone.Indexers = make([]TableIndexerSummary, len(summary.Indexers))
	for i, indexer := range summary.Indexers {
		clone.Indexers[i] = TableIndexerSummary{
			Access: indexer.Access,
			Key:    cloneTypeSummary(indexer.Key),
			Value:  cloneTypeSummary(indexer.Value),
		}
	}
	if summary.Metatable != nil {
		metatable := cloneTypeSummary(*summary.Metatable)
		clone.Metatable = &metatable
	}
	clone.Params = cloneTypeSummarySlice(summary.Params)
	if summary.Return != nil {
		ret := cloneTypeSummary(*summary.Return)
		clone.Return = &ret
	}
	clone.ParamPack = cloneTypePackSummary(summary.ParamPack)
	clone.ReturnPack = cloneTypePackSummary(summary.ReturnPack)
	return clone
}

func cloneTypeSummarySlice(summaries []TypeSummary) []TypeSummary {
	if len(summaries) == 0 {
		return nil
	}
	clone := make([]TypeSummary, len(summaries))
	for i, summary := range summaries {
		clone[i] = cloneTypeSummary(summary)
	}
	return clone
}

func cloneTypePackSummary(summary TypePackSummary) TypePackSummary {
	clone := summary
	clone.Head = cloneTypeSummarySlice(summary.Head)
	if summary.Tail != nil {
		tail := cloneTypeSummary(*summary.Tail)
		clone.Tail = &tail
	}
	return clone
}
