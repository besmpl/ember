package ember

// expressionSingleCall recognizes a call with no surrounding operators or
// selectors. IDs are stable arena handles; callers resolve payloads through
// the same syntaxTree rather than copying mutable node views.
func expressionSingleCall(tree syntaxTree, expr expressionID) (arenaCallID, bool) {
	value, ok := expressionRawSingleTerm(tree, expr)
	if !ok || tree.termKind(value) == syntaxTermCast {
		return 0, false
	}
	for tree.termKind(value) == syntaxTermGroup {
		inner, ok := tree.termGroup(value)
		if !ok {
			return 0, false
		}
		value, ok = expressionRawSingleTerm(tree, inner)
		if !ok {
			return 0, false
		}
	}
	call, ok := tree.termCall(value)
	if !ok {
		return 0, false
	}
	selectors, ok := tree.termSelectors(value)
	return call, ok && len(selectors) == 0
}

func expressionSingleVararg(tree syntaxTree, expr expressionID) (termID, bool) {
	value, ok := expressionRawSingleTerm(tree, expr)
	if !ok || tree.termKind(value) == syntaxTermCast || !tree.termVararg(value) {
		return 0, false
	}
	selectors, ok := tree.termSelectors(value)
	return value, ok && len(selectors) == 0
}

func expressionSingleTerm(tree syntaxTree, expr expressionID) (termID, bool) {
	value, ok := expressionRawSingleTerm(tree, expr)
	if !ok {
		return 0, false
	}
	return termWithoutCastsAndGroups(tree, value), true
}

func expressionRawSingleTerm(tree syntaxTree, expr expressionID) (termID, bool) {
	terms, ok := tree.expressionTerms(expr)
	if !ok || len(terms) != 1 {
		return 0, false
	}
	comparisons, ok := tree.andTerms(terms[0])
	if !ok || len(comparisons) != 1 {
		return 0, false
	}
	comparison := comparisons[0]
	if tree.comparisonOperator(comparison) != "" || tree.comparisonRight(comparison) != 0 {
		return 0, false
	}
	left := tree.comparisonLeft(comparison)
	rest, ok := tree.concatRest(left)
	if !ok || len(rest) != 0 {
		return 0, false
	}
	additive := tree.concatFirst(left)
	restAdd, ok := tree.additiveRest(additive)
	if !ok || len(restAdd) != 0 {
		return 0, false
	}
	multiplicative := tree.additiveFirst(additive)
	restMul, ok := tree.multiplicativeRest(multiplicative)
	if !ok || len(restMul) != 0 {
		return 0, false
	}
	return tree.multiplicativeFirst(multiplicative), true
}

func termWithoutCastsAndGroups(tree syntaxTree, value termID) termID {
	for tree.termKind(value) == syntaxTermCast || tree.termKind(value) == syntaxTermGroup {
		if tree.termKind(value) == syntaxTermCast {
			// Casts are represented as a distinct immutable term; there is no
			// child expression to unwrap, so stop at the underlying handle.
			return value
		}
		inner, ok := tree.termGroup(value)
		if !ok {
			return value
		}
		value, ok = expressionSingleTerm(tree, inner)
		if !ok {
			return value
		}
	}
	return value
}
