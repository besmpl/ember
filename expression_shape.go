package ember

func expressionSingleCall(expr expression) (callExpression, bool) {
	value, ok := expressionRawSingleTerm(expr)
	value = termWithoutCasts(value)
	if !ok || value.call == nil || len(value.selectors) != 0 {
		return callExpression{}, false
	}
	return *value.call, true
}

func expressionSingleVararg(expr expression) (term, bool) {
	value, ok := expressionRawSingleTerm(expr)
	value = termWithoutCasts(value)
	if !ok || !value.vararg || len(value.selectors) != 0 {
		return term{}, false
	}
	return value, true
}

func expressionSingleTerm(expr expression) (term, bool) {
	value, ok := expressionRawSingleTerm(expr)
	if !ok {
		return term{}, false
	}
	return termWithoutCastsAndGroups(value), true
}

func expressionRawSingleTerm(expr expression) (term, bool) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return term{}, false
	}

	comparison := expr.terms[0].terms[0]
	if comparison.op != "" || comparison.right != nil {
		return term{}, false
	}
	if len(comparison.left.rest) != 0 {
		return term{}, false
	}

	additive := comparison.left.first
	if len(additive.rest) != 0 {
		return term{}, false
	}
	if len(additive.first.rest) != 0 {
		return term{}, false
	}

	return additive.first.first, true
}

func termWithoutCastsAndGroups(value term) term {
	for {
		value = termWithoutCasts(value)
		if value.group == nil || len(value.selectors) != 0 {
			return value
		}

		inner, ok := expressionSingleTerm(*value.group)
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
