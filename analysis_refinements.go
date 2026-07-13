package ember

import "strings"

type conditionRefinement struct {
	place     refinablePlace
	trueType  simpleType
	falseType simpleType
}

type refinablePlace struct {
	name  string
	field string
}

func (p refinablePlace) valid() bool {
	return p.name != ""
}

func (p refinablePlace) hasField() bool {
	return p.field != ""
}

func (a *analysisState) trueConditionRefinements(tree syntaxTree, expr expressionID) []conditionRefinement {
	terms, ok := tree.expressionTerms(expr)
	if !ok {
		return nil
	}
	if len(terms) != 1 {
		return a.orTrueConditionRefinements(tree, expr)
	}
	var refinements []conditionRefinement
	andTerms, ok := tree.andTerms(terms[0])
	if !ok {
		return nil
	}
	for _, term := range andTerms {
		refinement := a.conditionRefinementForComparison(tree, term)
		if !refinement.place.valid() || refinement.trueType == simpleTypeUnknown {
			continue
		}
		refinements = append(refinements, refinement)
	}
	return refinements
}

func (a *analysisState) orTrueConditionRefinements(tree syntaxTree, expr expressionID) []conditionRefinement {
	var place refinablePlace
	typ := simpleTypeUnknown
	terms, ok := tree.expressionTerms(expr)
	if !ok {
		return nil
	}
	for _, term := range terms {
		andTerms, ok := tree.andTerms(term)
		if !ok || len(andTerms) != 1 {
			return nil
		}
		refinement := a.conditionRefinementForComparison(tree, andTerms[0])
		if !refinement.place.valid() || refinement.trueType == simpleTypeUnknown {
			return nil
		}
		if !place.valid() {
			place = refinement.place
		} else if place != refinement.place {
			return nil
		}
		typ = unionSimpleTypes(typ, refinement.trueType)
	}
	if !place.valid() || typ == simpleTypeUnknown {
		return nil
	}
	return []conditionRefinement{{
		place:    place,
		trueType: typ,
	}}
}

func (a *analysisState) applyTrueRefinements(refinements []conditionRefinement) {
	for _, refinement := range refinements {
		if !refinement.place.valid() || refinement.trueType == simpleTypeUnknown {
			continue
		}
		a.applyRefinement(refinement.place, refinement.trueType)
	}
}

func (a *analysisState) applyTrueRefinementsToExisting(refinements []conditionRefinement) {
	for _, refinement := range refinements {
		if !refinement.place.valid() || refinement.trueType == simpleTypeUnknown {
			continue
		}
		if !a.hasRefinablePlace(refinement.place) {
			continue
		}
		a.applyRefinement(refinement.place, refinement.trueType)
	}
}

func (a *analysisState) applyFalseConditionRefinements(tree syntaxTree, expr expressionID) {
	terms, ok := tree.expressionTerms(expr)
	if !ok {
		return
	}
	if len(terms) == 0 {
		return
	}
	if len(terms) == 1 {
		refinement := a.conditionRefinement(tree, expr)
		if refinement.place.valid() && refinement.falseType != simpleTypeUnknown {
			a.applyRefinement(refinement.place, refinement.falseType)
		}
		return
	}
	for _, term := range terms {
		andTerms, ok := tree.andTerms(term)
		if !ok || len(andTerms) != 1 {
			return
		}
		refinement := a.conditionRefinementForComparison(tree, andTerms[0])
		if !refinement.place.valid() || refinement.falseType == simpleTypeUnknown {
			return
		}
		a.applyRefinement(refinement.place, refinement.falseType)
	}
}

func (a *analysisState) applyRefinement(place refinablePlace, typ simpleType) {
	if !place.hasField() {
		a.defineLocal(place.name, typ)
		return
	}
	a.defineTableField(place.name, place.field, typ)
}

func (a *analysisState) hasRefinablePlace(place refinablePlace) bool {
	if !place.hasField() {
		return a.hasLocal(place.name)
	}
	return a.hasTableFact(place.name)
}

func (a *analysisState) refinablePlaceType(place refinablePlace) simpleType {
	if place.hasField() {
		return a.lookupTableField(place.name, place.field)
	}
	return a.lookupLocal(place.name)
}

func (a *analysisState) conditionRefinement(tree syntaxTree, expr expressionID) conditionRefinement {
	if refinement, ok := a.nilComparisonRefinement(tree, expr); ok {
		return refinement
	}
	if refinement, ok := a.singletonComparisonRefinement(tree, expr); ok {
		return refinement
	}
	if refinement, ok := a.typeGuardRefinement(tree, expr); ok {
		return refinement
	}
	value, ok := refinementSingleTerm(tree, expr)
	if !ok || tree.termName(value) == "" {
		if ok && tree.termKind(value) == syntaxTermUnaryNot {
			child, childOK := tree.termChild(value)
			selectors, selOK := tree.termSelectors(value)
			if childOK && selOK && len(selectors) == 0 {
				return a.negatedConditionRefinement(tree, child)
			}
		}
		return conditionRefinement{}
	}
	place, ok := refinablePlaceFromTerm(tree, value)
	if !ok {
		return conditionRefinement{}
	}
	current := a.refinablePlaceType(place)
	narrowed := truthyType(current)
	if narrowed == simpleTypeUnknown {
		return conditionRefinement{}
	}
	return conditionRefinement{
		place:     place,
		trueType:  narrowed,
		falseType: falseyType(current),
	}
}

func (a *analysisState) conditionRefinementForComparison(tree syntaxTree, comparison comparisonExpressionID) conditionRefinement {
	if tree.comparisonRight(comparison) != 0 {
		if refinement, ok := a.nilComparisonRefinementComparison(tree, comparison); ok {
			return refinement
		}
		if refinement, ok := a.singletonComparisonRefinementComparison(tree, comparison); ok {
			return refinement
		}
		if refinement, ok := a.typeGuardRefinementComparison(tree, comparison); ok {
			return refinement
		}
		return conditionRefinement{}
	}
	place, ok := concatSinglePlace(tree, tree.comparisonLeft(comparison))
	if !ok {
		return conditionRefinement{}
	}
	current := a.refinablePlaceType(place)
	return conditionRefinement{place: place, trueType: truthyType(current), falseType: falseyType(current)}
}

func (a *analysisState) nilComparisonRefinementComparison(tree syntaxTree, comparison comparisonExpressionID) (conditionRefinement, bool) {
	if tree.comparisonOperator(comparison) != comparisonEqual && tree.comparisonOperator(comparison) != comparisonNotEqual {
		return conditionRefinement{}, false
	}
	place, ok := nilComparisonPlace(tree, comparison)
	if !ok {
		return conditionRefinement{}, false
	}
	current := a.refinablePlaceType(place)
	if !typeAllows(current, simpleTypeNil) {
		return conditionRefinement{}, false
	}
	r := conditionRefinement{place: place, trueType: simpleTypeNil, falseType: truthyType(current)}
	if tree.comparisonOperator(comparison) == comparisonNotEqual {
		r.trueType, r.falseType = r.falseType, r.trueType
	}
	return r, true
}

func (a *analysisState) singletonComparisonRefinementComparison(tree syntaxTree, comparison comparisonExpressionID) (conditionRefinement, bool) {
	if tree.comparisonOperator(comparison) != comparisonEqual && tree.comparisonOperator(comparison) != comparisonNotEqual {
		return conditionRefinement{}, false
	}
	place, typ, ok := singletonComparisonPlace(tree, comparison)
	if !ok {
		return conditionRefinement{}, false
	}
	current := a.refinablePlaceType(place)
	if !typeAllows(current, typ) {
		return conditionRefinement{}, false
	}
	r := conditionRefinement{place: place, trueType: typ}
	if tree.comparisonOperator(comparison) == comparisonNotEqual {
		r.trueType, r.falseType = r.falseType, r.trueType
	}
	return r, true
}

func (a *analysisState) typeGuardRefinementComparison(tree syntaxTree, comparison comparisonExpressionID) (conditionRefinement, bool) {
	if tree.comparisonOperator(comparison) != comparisonEqual && tree.comparisonOperator(comparison) != comparisonNotEqual {
		return conditionRefinement{}, false
	}
	call, ok := concatSingleCall(tree, tree.comparisonLeft(comparison))
	if !ok {
		return conditionRefinement{}, false
	}
	kind, ok := concatSingleString(tree, tree.comparisonRight(comparison))
	if !ok {
		return conditionRefinement{}, false
	}
	place, ok := typeCallPlace(tree, call)
	if !ok {
		return conditionRefinement{}, false
	}
	current := a.refinablePlaceType(place)
	typ := simpleType(kind)
	if !typeAllows(current, typ) {
		return conditionRefinement{}, false
	}
	r := conditionRefinement{place: place, trueType: typ, falseType: typeWithout(current, typ)}
	if tree.comparisonOperator(comparison) == comparisonNotEqual {
		r.trueType, r.falseType = r.falseType, r.trueType
	}
	return r, true
}

func refinementField(tree syntaxTree, value termID) string {
	field, ok := stableFieldSelector(tree, value)
	if !ok {
		return ""
	}
	return field
}

func stableFieldSelector(tree syntaxTree, value termID) (string, bool) {
	selectors, ok := tree.termSelectors(value)
	if !ok || len(selectors) != 1 {
		return "", false
	}
	selector := selectors[0]
	field := tree.termSelectorField(selector)
	index := tree.termSelectorIndex(selector)
	if field != "" && index == 0 {
		return field, true
	}
	if index == 0 {
		return "", false
	}
	return refinementStringLiteral(tree, index)
}

func (a *analysisState) negatedConditionRefinement(tree syntaxTree, value termID) conditionRefinement {
	var expr expressionID
	group, groupOK := tree.termGroup(value)
	selectors, selOK := tree.termSelectors(value)
	if groupOK && selOK && len(selectors) == 0 {
		expr = group
	} else {
		place, ok := refinablePlaceFromTerm(tree, value)
		if !ok {
			return conditionRefinement{}
		}
		current := a.refinablePlaceType(place)
		return conditionRefinement{place: place, trueType: falseyType(current), falseType: truthyType(current)}
	}
	refinement := a.conditionRefinement(tree, expr)
	if !refinement.place.valid() {
		return conditionRefinement{}
	}
	refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	return refinement
}

func (a *analysisState) nilComparisonRefinement(tree syntaxTree, expr expressionID) (conditionRefinement, bool) {
	terms, ok := tree.expressionTerms(expr)
	if !ok || len(terms) != 1 {
		return conditionRefinement{}, false
	}
	andTerms, ok := tree.andTerms(terms[0])
	if !ok || len(andTerms) != 1 {
		return conditionRefinement{}, false
	}
	comparison := andTerms[0]
	if tree.comparisonRight(comparison) == 0 || (tree.comparisonOperator(comparison) != comparisonEqual && tree.comparisonOperator(comparison) != comparisonNotEqual) {
		return conditionRefinement{}, false
	}
	place, ok := nilComparisonPlace(tree, comparison)
	if !ok {
		return conditionRefinement{}, false
	}
	current := a.refinablePlaceType(place)
	if !typeAllows(current, simpleTypeNil) {
		return conditionRefinement{}, false
	}
	refinement := conditionRefinement{
		place:     place,
		trueType:  simpleTypeNil,
		falseType: truthyType(current),
	}
	if tree.comparisonOperator(comparison) == comparisonNotEqual {
		refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	}
	return refinement, true
}

func nilComparisonPlace(tree syntaxTree, comparison comparisonExpressionID) (refinablePlace, bool) {
	right := tree.comparisonRight(comparison)
	if right == 0 {
		return refinablePlace{}, false
	}
	left := tree.comparisonLeft(comparison)
	if place, ok := concatSinglePlace(tree, left); ok && concatSingleNil(tree, right) {
		return place, true
	}
	if concatSingleNil(tree, left) {
		return concatSinglePlace(tree, right)
	}
	return refinablePlace{}, false
}

func (a *analysisState) singletonComparisonRefinement(tree syntaxTree, expr expressionID) (conditionRefinement, bool) {
	terms, ok := tree.expressionTerms(expr)
	if !ok || len(terms) != 1 {
		return conditionRefinement{}, false
	}
	andTerms, ok := tree.andTerms(terms[0])
	if !ok || len(andTerms) != 1 {
		return conditionRefinement{}, false
	}
	comparison := andTerms[0]
	if tree.comparisonRight(comparison) == 0 || (tree.comparisonOperator(comparison) != comparisonEqual && tree.comparisonOperator(comparison) != comparisonNotEqual) {
		return conditionRefinement{}, false
	}
	place, typ, ok := singletonComparisonPlace(tree, comparison)
	if !ok {
		return conditionRefinement{}, false
	}
	current := a.refinablePlaceType(place)
	if !typeAllows(current, typ) {
		return conditionRefinement{}, false
	}
	refinement := conditionRefinement{
		place:     place,
		trueType:  typ,
		falseType: simpleTypeUnknown,
	}
	if tree.comparisonOperator(comparison) == comparisonNotEqual {
		refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	}
	return refinement, true
}

func singletonComparisonPlace(tree syntaxTree, comparison comparisonExpressionID) (refinablePlace, simpleType, bool) {
	right := tree.comparisonRight(comparison)
	if right == 0 {
		return refinablePlace{}, simpleTypeUnknown, false
	}
	left := tree.comparisonLeft(comparison)
	if place, ok := concatSinglePlace(tree, left); ok {
		if typ, ok := concatSingleSingletonType(tree, right); ok {
			return place, typ, true
		}
	}
	if typ, ok := concatSingleSingletonType(tree, left); ok {
		place, ok := concatSinglePlace(tree, right)
		return place, typ, ok
	}
	return refinablePlace{}, simpleTypeUnknown, false
}

func (a *analysisState) typeGuardRefinement(tree syntaxTree, expr expressionID) (conditionRefinement, bool) {
	terms, ok := tree.expressionTerms(expr)
	if !ok || len(terms) != 1 {
		return conditionRefinement{}, false
	}
	andTerms, ok := tree.andTerms(terms[0])
	if !ok || len(andTerms) != 1 {
		return conditionRefinement{}, false
	}
	comparison := andTerms[0]
	right := tree.comparisonRight(comparison)
	if right == 0 || (tree.comparisonOperator(comparison) != comparisonEqual && tree.comparisonOperator(comparison) != comparisonNotEqual) {
		return conditionRefinement{}, false
	}
	leftCall, ok := concatSingleCall(tree, tree.comparisonLeft(comparison))
	if !ok {
		return conditionRefinement{}, false
	}
	place, ok := typeCallPlace(tree, leftCall)
	if !ok {
		return conditionRefinement{}, false
	}
	kind, ok := concatSingleString(tree, right)
	if !ok {
		return conditionRefinement{}, false
	}
	typ := simpleType(kind)
	current := a.refinablePlaceType(place)
	if !typeAllows(current, typ) {
		return conditionRefinement{}, false
	}
	refinement := conditionRefinement{
		place:     place,
		trueType:  typ,
		falseType: typeWithout(current, typ),
	}
	if tree.comparisonOperator(comparison) == comparisonNotEqual {
		refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	}
	return refinement, true
}

func concatSinglePlace(tree syntaxTree, expr concatExpressionID) (refinablePlace, bool) {
	rest, ok := tree.concatRest(expr)
	if !ok || len(rest) != 0 {
		return refinablePlace{}, false
	}
	first := tree.concatFirst(expr)
	addRest, ok := tree.additiveRest(first)
	if !ok || len(addRest) != 0 {
		return refinablePlace{}, false
	}
	additiveFirst := tree.additiveFirst(first)
	mulRest, ok := tree.multiplicativeRest(additiveFirst)
	if !ok || len(mulRest) != 0 {
		return refinablePlace{}, false
	}
	value := refinementTermWithoutCastsAndGroups(tree, tree.multiplicativeFirst(additiveFirst))
	return refinablePlaceFromTerm(tree, value)
}

func refinablePlaceFromTerm(tree syntaxTree, value termID) (refinablePlace, bool) {
	name := tree.termName(value)
	if name == "" {
		return refinablePlace{}, false
	}
	field := refinementField(tree, value)
	selectors, _ := tree.termSelectors(value)
	if field == "" && len(selectors) != 0 {
		return refinablePlace{}, false
	}
	return refinablePlace{name: name, field: field}, true
}

func concatSingleNil(tree syntaxTree, expr concatExpressionID) bool {
	rest, ok := tree.concatRest(expr)
	if !ok || len(rest) != 0 {
		return false
	}
	first := tree.concatFirst(expr)
	addRest, ok := tree.additiveRest(first)
	if !ok || len(addRest) != 0 {
		return false
	}
	additiveFirst := tree.additiveFirst(first)
	mulRest, ok := tree.multiplicativeRest(additiveFirst)
	if !ok || len(mulRest) != 0 {
		return false
	}
	value := refinementTermWithoutCastsAndGroups(tree, tree.multiplicativeFirst(additiveFirst))
	literal, ok := tree.termLiteral(value)
	if !ok {
		return false
	}
	return literal.Kind() == NilKind
}

func concatSingleSingletonType(tree syntaxTree, expr concatExpressionID) (simpleType, bool) {
	rest, ok := tree.concatRest(expr)
	if !ok || len(rest) != 0 {
		return simpleTypeUnknown, false
	}
	first := tree.concatFirst(expr)
	addRest, ok := tree.additiveRest(first)
	if !ok || len(addRest) != 0 {
		return simpleTypeUnknown, false
	}
	additiveFirst := tree.additiveFirst(first)
	mulRest, ok := tree.multiplicativeRest(additiveFirst)
	if !ok || len(mulRest) != 0 {
		return simpleTypeUnknown, false
	}
	value := refinementTermWithoutCastsAndGroups(tree, tree.multiplicativeFirst(additiveFirst))
	literal, ok := tree.termLiteral(value)
	if !ok {
		return simpleTypeUnknown, false
	}
	switch literal.Kind() {
	case BoolKind:
		return simpleTypeBoolean, true
	case StringKind:
		return simpleTypeString, true
	default:
		return simpleTypeUnknown, false
	}
}

func concatSingleCall(tree syntaxTree, expr concatExpressionID) (arenaCallID, bool) {
	rest, ok := tree.concatRest(expr)
	if !ok || len(rest) != 0 {
		return 0, false
	}
	first := tree.concatFirst(expr)
	addRest, ok := tree.additiveRest(first)
	if !ok || len(addRest) != 0 {
		return 0, false
	}
	additiveFirst := tree.additiveFirst(first)
	mulRest, ok := tree.multiplicativeRest(additiveFirst)
	if !ok || len(mulRest) != 0 {
		return 0, false
	}
	value := refinementTermWithoutCastsAndGroups(tree, tree.multiplicativeFirst(additiveFirst))
	call, ok := tree.termCall(value)
	selectors, selOK := tree.termSelectors(value)
	if !ok || !selOK || len(selectors) != 0 {
		return 0, false
	}
	return call, true
}

func typeCallPlace(tree syntaxTree, call arenaCallID) (refinablePlace, bool) {
	target := tree.callTarget(call)
	if target == 0 {
		return refinablePlace{}, false
	}
	target = refinementTermWithoutCastsAndGroups(tree, target)
	args, ok := tree.callArgs(call)
	if !ok || tree.termName(target) != "type" || len(args) != 1 {
		return refinablePlace{}, false
	}
	arg, ok := refinementSingleTerm(tree, args[0])
	if !ok || tree.termName(arg) == "" {
		return refinablePlace{}, false
	}
	return refinablePlaceFromTerm(tree, arg)
}

func concatSingleString(tree syntaxTree, expr concatExpressionID) (string, bool) {
	rest, ok := tree.concatRest(expr)
	if !ok || len(rest) != 0 {
		return "", false
	}
	first := tree.concatFirst(expr)
	addRest, ok := tree.additiveRest(first)
	if !ok || len(addRest) != 0 {
		return "", false
	}
	additiveFirst := tree.additiveFirst(first)
	mulRest, ok := tree.multiplicativeRest(additiveFirst)
	if !ok || len(mulRest) != 0 {
		return "", false
	}
	value := refinementTermWithoutCastsAndGroups(tree, tree.multiplicativeFirst(additiveFirst))
	literal, ok := tree.termLiteral(value)
	if !ok {
		return "", false
	}
	return literal.String()
}

// refinementSingleTerm extracts a plain term while keeping all parser-tree
// traversal behind the syntaxTree facade used by the analyzer.

func refinementSingleTerm(tree syntaxTree, expr expressionID) (termID, bool) {
	terms, ok := tree.expressionTerms(expr)
	if !ok || len(terms) != 1 {
		return 0, false
	}
	andTerms, ok := tree.andTerms(terms[0])
	if !ok || len(andTerms) != 1 {
		return 0, false
	}
	comparison := andTerms[0]
	if tree.comparisonOperator(comparison) != "" || tree.comparisonRight(comparison) != 0 {
		return 0, false
	}
	concat := tree.comparisonLeft(comparison)
	rest, ok := tree.concatRest(concat)
	if !ok || len(rest) != 0 {
		return 0, false
	}
	additive := tree.concatFirst(concat)
	addRest, ok := tree.additiveRest(additive)
	if !ok || len(addRest) != 0 {
		return 0, false
	}
	multiplicative := tree.additiveFirst(additive)
	mulRest, ok := tree.multiplicativeRest(multiplicative)
	if !ok || len(mulRest) != 0 {
		return 0, false
	}
	return refinementTermWithoutCastsAndGroups(tree, tree.multiplicativeFirst(multiplicative)), true
}

func refinementTermWithoutCastsAndGroups(tree syntaxTree, value termID) termID {
	for {
		selectors, ok := tree.termSelectors(value)
		if !ok || len(selectors) != 0 {
			return value
		}
		group, groupOK := tree.termGroup(value)
		if !groupOK {
			return value
		}
		inner, ok := refinementSingleTerm(tree, group)
		if !ok {
			return value
		}
		value = inner
	}
}

func refinementStringLiteral(tree syntaxTree, expr expressionID) (string, bool) {
	value, ok := refinementSingleTerm(tree, expr)
	if !ok {
		return "", false
	}
	literal, ok := tree.termLiteral(value)
	if !ok {
		return "", false
	}
	return literal.String()
}

func truthyType(typ simpleType) simpleType {
	if strings.HasSuffix(string(typ), "?") {
		return simpleType(strings.TrimSuffix(string(typ), "?"))
	}
	if typ == simpleTypeNil {
		return simpleTypeUnknown
	}
	return typ
}

func falseyType(typ simpleType) simpleType {
	if strings.HasSuffix(string(typ), "?") {
		return simpleTypeNil
	}
	if typ == simpleTypeNil {
		return simpleTypeNil
	}
	return simpleTypeUnknown
}
