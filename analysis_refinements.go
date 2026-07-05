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

func (a *analysisState) trueConditionRefinements(expr expression) []conditionRefinement {
	if len(expr.terms) != 1 {
		return a.orTrueConditionRefinements(expr)
	}
	var refinements []conditionRefinement
	for _, term := range expr.terms[0].terms {
		single := expression{terms: []andExpression{{terms: []comparisonExpression{term}}}}
		refinement := a.conditionRefinement(single)
		if !refinement.place.valid() || refinement.trueType == simpleTypeUnknown {
			continue
		}
		refinements = append(refinements, refinement)
	}
	return refinements
}

func (a *analysisState) orTrueConditionRefinements(expr expression) []conditionRefinement {
	var place refinablePlace
	typ := simpleTypeUnknown
	for _, term := range expr.terms {
		if len(term.terms) != 1 {
			return nil
		}
		single := expression{terms: []andExpression{{terms: []comparisonExpression{term.terms[0]}}}}
		refinement := a.conditionRefinement(single)
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

func (a *analysisState) applyFalseConditionRefinements(expr expression) {
	if len(expr.terms) == 0 {
		return
	}
	if len(expr.terms) == 1 {
		refinement := a.conditionRefinement(expr)
		if refinement.place.valid() && refinement.falseType != simpleTypeUnknown {
			a.applyRefinement(refinement.place, refinement.falseType)
		}
		return
	}
	for _, term := range expr.terms {
		if len(term.terms) != 1 {
			return
		}
		single := expression{terms: []andExpression{{terms: []comparisonExpression{term.terms[0]}}}}
		refinement := a.conditionRefinement(single)
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

func (a *analysisState) conditionRefinement(expr expression) conditionRefinement {
	if refinement, ok := a.nilComparisonRefinement(expr); ok {
		return refinement
	}
	if refinement, ok := a.singletonComparisonRefinement(expr); ok {
		return refinement
	}
	if refinement, ok := a.typeGuardRefinement(expr); ok {
		return refinement
	}
	value, ok := expressionSingleTerm(expr)
	if !ok || value.name == "" {
		if ok && value.unaryNot != nil && len(value.selectors) == 0 {
			return a.negatedConditionRefinement(*value.unaryNot)
		}
		return conditionRefinement{}
	}
	place, ok := refinablePlaceFromTerm(value)
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

func refinementField(value term) string {
	field, ok := stableFieldSelector(value)
	if !ok {
		return ""
	}
	return field
}

func stableFieldSelector(value term) (string, bool) {
	if len(value.selectors) != 1 {
		return "", false
	}
	selector := value.selectors[0]
	if selector.field != "" && selector.index == nil {
		return selector.field, true
	}
	if selector.index == nil {
		return "", false
	}
	field, ok := expressionStringLiteral(*selector.index)
	return field, ok
}

func (a *analysisState) negatedConditionRefinement(value term) conditionRefinement {
	value = termWithoutCasts(value)
	var expr expression
	if value.group != nil && len(value.selectors) == 0 {
		expr = *value.group
	} else {
		expr = expressionFromTerm(value)
	}
	refinement := a.conditionRefinement(expr)
	if !refinement.place.valid() {
		return conditionRefinement{}
	}
	refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	return refinement
}

func (a *analysisState) nilComparisonRefinement(expr expression) (conditionRefinement, bool) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return conditionRefinement{}, false
	}
	comparison := expr.terms[0].terms[0]
	if comparison.right == nil || (comparison.op != comparisonEqual && comparison.op != comparisonNotEqual) {
		return conditionRefinement{}, false
	}
	place, ok := nilComparisonPlace(comparison)
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
	if comparison.op == comparisonNotEqual {
		refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	}
	return refinement, true
}

func nilComparisonPlace(comparison comparisonExpression) (refinablePlace, bool) {
	if comparison.right == nil {
		return refinablePlace{}, false
	}
	if place, ok := concatSinglePlace(comparison.left); ok && concatSingleNil(*comparison.right) {
		return place, true
	}
	if concatSingleNil(comparison.left) {
		return concatSinglePlace(*comparison.right)
	}
	return refinablePlace{}, false
}

func (a *analysisState) singletonComparisonRefinement(expr expression) (conditionRefinement, bool) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return conditionRefinement{}, false
	}
	comparison := expr.terms[0].terms[0]
	if comparison.right == nil || (comparison.op != comparisonEqual && comparison.op != comparisonNotEqual) {
		return conditionRefinement{}, false
	}
	place, typ, ok := singletonComparisonPlace(comparison)
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
	if comparison.op == comparisonNotEqual {
		refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	}
	return refinement, true
}

func singletonComparisonPlace(comparison comparisonExpression) (refinablePlace, simpleType, bool) {
	if comparison.right == nil {
		return refinablePlace{}, simpleTypeUnknown, false
	}
	if place, ok := concatSinglePlace(comparison.left); ok {
		if typ, ok := concatSingleSingletonType(*comparison.right); ok {
			return place, typ, true
		}
	}
	if typ, ok := concatSingleSingletonType(comparison.left); ok {
		place, ok := concatSinglePlace(*comparison.right)
		return place, typ, ok
	}
	return refinablePlace{}, simpleTypeUnknown, false
}

func (a *analysisState) typeGuardRefinement(expr expression) (conditionRefinement, bool) {
	if len(expr.terms) != 1 || len(expr.terms[0].terms) != 1 {
		return conditionRefinement{}, false
	}
	comparison := expr.terms[0].terms[0]
	if comparison.right == nil || (comparison.op != comparisonEqual && comparison.op != comparisonNotEqual) {
		return conditionRefinement{}, false
	}
	leftCall, ok := concatSingleCall(comparison.left)
	if !ok {
		return conditionRefinement{}, false
	}
	place, ok := typeCallPlace(leftCall)
	if !ok {
		return conditionRefinement{}, false
	}
	kind, ok := concatSingleString(*comparison.right)
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
	if comparison.op == comparisonNotEqual {
		refinement.trueType, refinement.falseType = refinement.falseType, refinement.trueType
	}
	return refinement, true
}

func concatSinglePlace(expr concatExpression) (refinablePlace, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return refinablePlace{}, false
	}
	value := termWithoutCastsAndGroups(expr.first.first.first)
	return refinablePlaceFromTerm(value)
}

func refinablePlaceFromTerm(value term) (refinablePlace, bool) {
	if value.name == "" {
		return refinablePlace{}, false
	}
	field := refinementField(value)
	if field == "" && len(value.selectors) != 0 {
		return refinablePlace{}, false
	}
	return refinablePlace{name: value.name, field: field}, true
}

func concatSingleNil(expr concatExpression) bool {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return false
	}
	value := termWithoutCastsAndGroups(expr.first.first.first)
	if value.lit == nil {
		return false
	}
	return value.lit.Kind() == NilKind
}

func concatSingleSingletonType(expr concatExpression) (simpleType, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return simpleTypeUnknown, false
	}
	value := termWithoutCastsAndGroups(expr.first.first.first)
	if value.lit == nil {
		return simpleTypeUnknown, false
	}
	switch value.lit.Kind() {
	case BoolKind:
		return simpleTypeBoolean, true
	case StringKind:
		return simpleTypeString, true
	default:
		return simpleTypeUnknown, false
	}
}

func concatSingleCall(expr concatExpression) (callExpression, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return callExpression{}, false
	}
	value := termWithoutCastsAndGroups(expr.first.first.first)
	if value.call == nil || len(value.selectors) != 0 {
		return callExpression{}, false
	}
	return *value.call, true
}

func typeCallPlace(call callExpression) (refinablePlace, bool) {
	target := termWithoutCastsAndGroups(call.target)
	if target.name != "type" || len(call.args) != 1 {
		return refinablePlace{}, false
	}
	arg, ok := expressionSingleTerm(call.args[0])
	if !ok || arg.name == "" {
		return refinablePlace{}, false
	}
	return refinablePlaceFromTerm(arg)
}

func concatSingleString(expr concatExpression) (string, bool) {
	if len(expr.rest) != 0 || len(expr.first.rest) != 0 || len(expr.first.first.rest) != 0 {
		return "", false
	}
	value := termWithoutCastsAndGroups(expr.first.first.first)
	if value.lit == nil {
		return "", false
	}
	return value.lit.String()
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
