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

func (a *analysisState) trueConditionRefinements(tree syntaxTree, expr expression) []conditionRefinement {
	terms := tree.expressionTerms(&expr)
	if len(terms) != 1 {
		return a.orTrueConditionRefinements(tree, expr)
	}
	var refinements []conditionRefinement
	andTerms := tree.andTerms(&terms[0])
	for _, term := range andTerms {
		single := expression{terms: []andExpression{{terms: []comparisonExpression{term}}}}
		refinement := a.conditionRefinement(tree, single)
		if !refinement.place.valid() || refinement.trueType == simpleTypeUnknown {
			continue
		}
		refinements = append(refinements, refinement)
	}
	return refinements
}

func (a *analysisState) orTrueConditionRefinements(tree syntaxTree, expr expression) []conditionRefinement {
	var place refinablePlace
	typ := simpleTypeUnknown
	for _, term := range tree.expressionTerms(&expr) {
		andTerms := tree.andTerms(&term)
		if len(andTerms) != 1 {
			return nil
		}
		single := expression{terms: []andExpression{{terms: []comparisonExpression{andTerms[0]}}}}
		refinement := a.conditionRefinement(tree, single)
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

func (a *analysisState) applyFalseConditionRefinements(tree syntaxTree, expr expression) {
	terms := tree.expressionTerms(&expr)
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
		andTerms := tree.andTerms(&term)
		if len(andTerms) != 1 {
			return
		}
		single := expression{terms: []andExpression{{terms: []comparisonExpression{andTerms[0]}}}}
		refinement := a.conditionRefinement(tree, single)
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

func (a *analysisState) conditionRefinement(tree syntaxTree, expr expression) conditionRefinement {
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
	if !ok || tree.termName(&value) == "" {
		if ok && tree.termUnaryNot(&value) != nil && len(tree.termSelectors(&value)) == 0 {
			return a.negatedConditionRefinement(tree, *tree.termUnaryNot(&value))
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

func refinementField(tree syntaxTree, value term) string {
	field, ok := stableFieldSelector(tree, value)
	if !ok {
		return ""
	}
	return field
}

func stableFieldSelector(tree syntaxTree, value term) (string, bool) {
	selectors := tree.termSelectors(&value)
	if len(selectors) != 1 {
		return "", false
	}
	selector := &selectors[0]
	field := tree.selectorField(selector)
	index := tree.selectorIndex(selector)
	if field != "" && index == nil {
		return field, true
	}
	if index == nil {
		return "", false
	}
	return refinementStringLiteral(tree, *index)
}

func (a *analysisState) negatedConditionRefinement(tree syntaxTree, value term) conditionRefinement {
	value = termWithoutCasts(value)
	var expr expression
	group := tree.termGroup(&value)
	if group != nil && len(tree.termSelectors(&value)) == 0 {
		expr = *group
	} else {
		expr = expressionFromTerm(value)
	}
	refinement := a.conditionRefinement(tree, expr)
	if !refinement.place.valid() {
		return conditionRefinement{}
	}
	refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	return refinement
}

func (a *analysisState) nilComparisonRefinement(tree syntaxTree, expr expression) (conditionRefinement, bool) {
	terms := tree.expressionTerms(&expr)
	if len(terms) != 1 || len(tree.andTerms(&terms[0])) != 1 {
		return conditionRefinement{}, false
	}
	comparison := tree.andTerms(&terms[0])[0]
	if tree.comparisonRight(&comparison) == nil || (tree.comparisonOperator(&comparison) != comparisonEqual && tree.comparisonOperator(&comparison) != comparisonNotEqual) {
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
	if tree.comparisonOperator(&comparison) == comparisonNotEqual {
		refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	}
	return refinement, true
}

func nilComparisonPlace(tree syntaxTree, comparison comparisonExpression) (refinablePlace, bool) {
	right := tree.comparisonRight(&comparison)
	if right == nil {
		return refinablePlace{}, false
	}
	left := tree.comparisonLeft(&comparison)
	if place, ok := concatSinglePlace(tree, left); ok && concatSingleNil(tree, *right) {
		return place, true
	}
	if concatSingleNil(tree, left) {
		return concatSinglePlace(tree, *right)
	}
	return refinablePlace{}, false
}

func (a *analysisState) singletonComparisonRefinement(tree syntaxTree, expr expression) (conditionRefinement, bool) {
	terms := tree.expressionTerms(&expr)
	if len(terms) != 1 || len(tree.andTerms(&terms[0])) != 1 {
		return conditionRefinement{}, false
	}
	comparison := tree.andTerms(&terms[0])[0]
	if tree.comparisonRight(&comparison) == nil || (tree.comparisonOperator(&comparison) != comparisonEqual && tree.comparisonOperator(&comparison) != comparisonNotEqual) {
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
	if tree.comparisonOperator(&comparison) == comparisonNotEqual {
		refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	}
	return refinement, true
}

func singletonComparisonPlace(tree syntaxTree, comparison comparisonExpression) (refinablePlace, simpleType, bool) {
	right := tree.comparisonRight(&comparison)
	if right == nil {
		return refinablePlace{}, simpleTypeUnknown, false
	}
	left := tree.comparisonLeft(&comparison)
	if place, ok := concatSinglePlace(tree, left); ok {
		if typ, ok := concatSingleSingletonType(tree, *right); ok {
			return place, typ, true
		}
	}
	if typ, ok := concatSingleSingletonType(tree, left); ok {
		place, ok := concatSinglePlace(tree, *right)
		return place, typ, ok
	}
	return refinablePlace{}, simpleTypeUnknown, false
}

func (a *analysisState) typeGuardRefinement(tree syntaxTree, expr expression) (conditionRefinement, bool) {
	terms := tree.expressionTerms(&expr)
	if len(terms) != 1 || len(tree.andTerms(&terms[0])) != 1 {
		return conditionRefinement{}, false
	}
	comparison := tree.andTerms(&terms[0])[0]
	right := tree.comparisonRight(&comparison)
	if right == nil || (tree.comparisonOperator(&comparison) != comparisonEqual && tree.comparisonOperator(&comparison) != comparisonNotEqual) {
		return conditionRefinement{}, false
	}
	leftCall, ok := concatSingleCall(tree, tree.comparisonLeft(&comparison))
	if !ok {
		return conditionRefinement{}, false
	}
	place, ok := typeCallPlace(tree, leftCall)
	if !ok {
		return conditionRefinement{}, false
	}
	kind, ok := concatSingleString(tree, *right)
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
	if tree.comparisonOperator(&comparison) == comparisonNotEqual {
		refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	}
	return refinement, true
}

func concatSinglePlace(tree syntaxTree, expr concatExpression) (refinablePlace, bool) {
	if len(tree.concatRest(&expr)) != 0 {
		return refinablePlace{}, false
	}
	first := tree.concatFirst(&expr)
	if len(tree.additiveRest(&first)) != 0 {
		return refinablePlace{}, false
	}
	additiveFirst := tree.additiveFirst(&first)
	if len(tree.multiplicativeRest(&additiveFirst)) != 0 {
		return refinablePlace{}, false
	}
	value := refinementTermWithoutCastsAndGroups(tree, tree.multiplicativeFirst(&additiveFirst))
	return refinablePlaceFromTerm(tree, value)
}

func refinablePlaceFromTerm(tree syntaxTree, value term) (refinablePlace, bool) {
	name := tree.termName(&value)
	if name == "" {
		return refinablePlace{}, false
	}
	field := refinementField(tree, value)
	if field == "" && len(tree.termSelectors(&value)) != 0 {
		return refinablePlace{}, false
	}
	return refinablePlace{name: name, field: field}, true
}

func concatSingleNil(tree syntaxTree, expr concatExpression) bool {
	if len(tree.concatRest(&expr)) != 0 {
		return false
	}
	first := tree.concatFirst(&expr)
	if len(tree.additiveRest(&first)) != 0 {
		return false
	}
	additiveFirst := tree.additiveFirst(&first)
	if len(tree.multiplicativeRest(&additiveFirst)) != 0 {
		return false
	}
	value := refinementTermWithoutCastsAndGroups(tree, tree.multiplicativeFirst(&additiveFirst))
	literal := tree.termLiteral(&value)
	if literal == nil {
		return false
	}
	return literal.Kind() == NilKind
}

func concatSingleSingletonType(tree syntaxTree, expr concatExpression) (simpleType, bool) {
	if len(tree.concatRest(&expr)) != 0 {
		return simpleTypeUnknown, false
	}
	first := tree.concatFirst(&expr)
	if len(tree.additiveRest(&first)) != 0 {
		return simpleTypeUnknown, false
	}
	additiveFirst := tree.additiveFirst(&first)
	if len(tree.multiplicativeRest(&additiveFirst)) != 0 {
		return simpleTypeUnknown, false
	}
	value := refinementTermWithoutCastsAndGroups(tree, tree.multiplicativeFirst(&additiveFirst))
	literal := tree.termLiteral(&value)
	if literal == nil {
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

func concatSingleCall(tree syntaxTree, expr concatExpression) (callExpression, bool) {
	if len(tree.concatRest(&expr)) != 0 {
		return callExpression{}, false
	}
	first := tree.concatFirst(&expr)
	if len(tree.additiveRest(&first)) != 0 {
		return callExpression{}, false
	}
	additiveFirst := tree.additiveFirst(&first)
	if len(tree.multiplicativeRest(&additiveFirst)) != 0 {
		return callExpression{}, false
	}
	value := refinementTermWithoutCastsAndGroups(tree, tree.multiplicativeFirst(&additiveFirst))
	call := tree.termCall(&value)
	if call == nil || len(tree.termSelectors(&value)) != 0 {
		return callExpression{}, false
	}
	return *call, true
}

func typeCallPlace(tree syntaxTree, call callExpression) (refinablePlace, bool) {
	targetNode := tree.callTarget(&call)
	if targetNode == nil {
		return refinablePlace{}, false
	}
	target := refinementTermWithoutCastsAndGroups(tree, *targetNode)
	args := tree.callArgs(&call)
	if tree.termName(&target) != "type" || len(args) != 1 {
		return refinablePlace{}, false
	}
	arg, ok := refinementSingleTerm(tree, args[0])
	if !ok || tree.termName(&arg) == "" {
		return refinablePlace{}, false
	}
	return refinablePlaceFromTerm(tree, arg)
}

func concatSingleString(tree syntaxTree, expr concatExpression) (string, bool) {
	if len(tree.concatRest(&expr)) != 0 {
		return "", false
	}
	first := tree.concatFirst(&expr)
	if len(tree.additiveRest(&first)) != 0 {
		return "", false
	}
	additiveFirst := tree.additiveFirst(&first)
	if len(tree.multiplicativeRest(&additiveFirst)) != 0 {
		return "", false
	}
	value := refinementTermWithoutCastsAndGroups(tree, tree.multiplicativeFirst(&additiveFirst))
	literal := tree.termLiteral(&value)
	if literal == nil {
		return "", false
	}
	return literal.String()
}

// refinementSingleTerm extracts a plain term while keeping all parser-tree
// traversal behind the syntaxTree facade used by the analyzer.
func refinementSingleTerm(tree syntaxTree, expr expression) (term, bool) {
	terms := tree.expressionTerms(&expr)
	if len(terms) != 1 {
		return term{}, false
	}
	andTerms := tree.andTerms(&terms[0])
	if len(andTerms) != 1 {
		return term{}, false
	}
	comparison := andTerms[0]
	if tree.comparisonOperator(&comparison) != "" || tree.comparisonRight(&comparison) != nil {
		return term{}, false
	}
	concat := tree.comparisonLeft(&comparison)
	if len(tree.concatRest(&concat)) != 0 {
		return term{}, false
	}
	additive := tree.concatFirst(&concat)
	if len(tree.additiveRest(&additive)) != 0 {
		return term{}, false
	}
	multiplicative := tree.additiveFirst(&additive)
	if len(tree.multiplicativeRest(&multiplicative)) != 0 {
		return term{}, false
	}
	return refinementTermWithoutCastsAndGroups(tree, tree.multiplicativeFirst(&multiplicative)), true
}

func refinementTermWithoutCastsAndGroups(tree syntaxTree, value term) term {
	for {
		value = termWithoutCasts(value)
		group := tree.termGroup(&value)
		if group == nil || len(tree.termSelectors(&value)) != 0 {
			return value
		}
		inner, ok := refinementSingleTerm(tree, *group)
		if !ok {
			return value
		}
		value = inner
	}
}

func refinementStringLiteral(tree syntaxTree, expr expression) (string, bool) {
	value, ok := refinementSingleTerm(tree, expr)
	if !ok {
		return "", false
	}
	literal := tree.termLiteral(&value)
	if literal == nil {
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
