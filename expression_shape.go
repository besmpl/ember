package ember

func expressionSingleCall(tree syntaxTree, expr expression) (callExpression, bool) {
	value, ok := expressionRawSingleTerm(tree, expr)
	value = termWithoutCasts(value)
	call := tree.termCall(&value)
	if !ok || call == nil || len(tree.termSelectors(&value)) != 0 {
		return callExpression{}, false
	}
	return *call, true
}

func expressionSingleVararg(tree syntaxTree, expr expression) (term, bool) {
	value, ok := expressionRawSingleTerm(tree, expr)
	value = termWithoutCasts(value)
	if !ok || !tree.termVararg(&value) || len(tree.termSelectors(&value)) != 0 {
		return term{}, false
	}
	return value, true
}

func expressionSingleTerm(tree syntaxTree, expr expression) (term, bool) {
	value, ok := expressionRawSingleTerm(tree, expr)
	if !ok {
		return term{}, false
	}
	return termWithoutCastsAndGroups(tree, value), true
}

func expressionRawSingleTerm(tree syntaxTree, expr expression) (term, bool) {
	terms := tree.expressionTerms(&expr)
	if len(terms) != 1 || len(tree.andTerms(&terms[0])) != 1 {
		return term{}, false
	}

	andTerms := tree.andTerms(&terms[0])
	comparison := andTerms[0]
	if tree.comparisonOperator(&comparison) != "" || tree.comparisonRight(&comparison) != nil {
		return term{}, false
	}
	left := tree.comparisonLeft(&comparison)
	if len(tree.concatRest(&left)) != 0 {
		return term{}, false
	}

	additive := tree.concatFirst(&left)
	if len(tree.additiveRest(&additive)) != 0 {
		return term{}, false
	}
	multiplicative := tree.additiveFirst(&additive)
	if len(tree.multiplicativeRest(&multiplicative)) != 0 {
		return term{}, false
	}

	return tree.multiplicativeFirst(&multiplicative), true
}

func termWithoutCastsAndGroups(tree syntaxTree, value term) term {
	for {
		value = termWithoutCasts(value)
		if tree.termGroup(&value) == nil || len(tree.termSelectors(&value)) != 0 {
			return value
		}

		inner, ok := expressionSingleTerm(tree, *tree.termGroup(&value))
		if !ok {
			return value
		}
		value = inner
	}
}

func termWithoutCasts(value term) term {
	value.cast = nil
	return value
}
