package ember

import "math"

// Expression-ladder IDs are indexes into syntaxArena node slices. Zero is the
// absence value; a real ID is the node's zero-based index plus one.
type expressionID uint32
type andExpressionID uint32
type comparisonExpressionID uint32
type concatExpressionID uint32
type additiveExpressionID uint32
type multiplicativeExpressionID uint32
type termID uint32
type stringID uint32
type arenaTableID uint32
type arenaFunctionID uint32
type arenaIfExpressionID uint32
type arenaCallID uint32
type arenaPowerID uint32

// nodeSpan identifies a contiguous range in one of syntaxArena's typed child
// arrays. The checked span helper below deliberately validates subtraction
// before addition so malformed input cannot wrap a uint32 boundary.
type nodeSpan struct {
	start uint32
	count uint32
}

// syntaxNodeBudget is shared by every arena family.  The parser reserves a
// syntax node before growing the corresponding arena; this keeps limits
// deterministic and prevents a failed parse from allocating past its bound.
type syntaxNodeBudget struct {
	limit uint64
	used  uint64
	err   error
}

func (b *syntaxNodeBudget) reserve() bool {
	if b == nil {
		return true
	}
	if b.err != nil {
		return false
	}
	b.used++
	if b.limit != 0 && b.used > b.limit {
		b.err = &LimitError{Kind: LimitSyntaxNodes, Limit: b.limit, Used: b.used}
		return false
	}
	return true
}

func arenaNode[T any, ID ~uint32](nodes []T, id ID) (T, bool) {
	var zero T
	if id == 0 || uint64(id) > uint64(len(nodes)) {
		return zero, false
	}
	return nodes[uint64(id)-1], true
}

func arenaSpan[T any](values []T, span nodeSpan) ([]T, bool) {
	start := uint64(span.start)
	count := uint64(span.count)
	length := uint64(len(values))
	if start > length || count > length-start {
		return nil, false
	}
	return values[int(start):int(start+count)], true
}

// idListBuilder keeps the common one-to-four-child case on the stack. The
// caller appends the completed list to an arena child array exactly once.
type idListBuilder[T any] struct {
	inline [4]T
	extra  []T
	count  int
}

func (b *idListBuilder[T]) append(id T) {
	if b.count < len(b.inline) {
		b.inline[b.count] = id
	} else {
		b.extra = append(b.extra, id)
	}
	b.count++
}

func (b *idListBuilder[T]) at(index int) T {
	if index < len(b.inline) {
		return b.inline[index]
	}
	return b.extra[index-len(b.inline)]
}

func (b *idListBuilder[T]) span(dst *[]T) (nodeSpan, bool) {
	if b.count < 0 || uint64(len(*dst)) > math.MaxUint32 || uint64(b.count) > math.MaxUint32-uint64(len(*dst)) {
		return nodeSpan{}, false
	}
	start := uint32(len(*dst))
	if b.count <= len(b.inline) {
		*dst = append(*dst, b.inline[:b.count]...)
	} else {
		*dst = append(*dst, b.inline[:]...)
		*dst = append(*dst, b.extra...)
	}
	span := nodeSpan{start: start, count: uint32(b.count)}
	b.inline = [4]T{}
	b.extra = b.extra[:0]
	b.count = 0
	return span, true
}

type termKind uint8

const (
	termKindUnknown termKind = iota
	termKindNumber
	termKindNil
	termKindBool
	termKindString
	termKindTable
	termKindFunction
	termKindIf
	termKindCall
	termKindVararg
	termKindUnaryNot
	termKindUnaryMinus
	termKindUnaryLength
	termKindPower
	termKindGroup
	termKindName
)

type arenaExpression struct {
	id    syntaxID
	terms nodeSpan
}

type arenaAndExpression struct{ terms nodeSpan }

type arenaComparisonExpression struct {
	left  concatExpressionID
	op    arenaComparisonOp
	right concatExpressionID
}

type arenaConcatExpression struct {
	first additiveExpressionID
	rest  nodeSpan
}

type arenaAdditiveExpression struct {
	first multiplicativeExpressionID
	rest  nodeSpan
}

type arenaMultiplicativeExpression struct {
	first termID
	rest  nodeSpan
}

// arenaTerm is intentionally a discriminated payload rather than a struct of
// optional pointers. payload is a number's Float64bits or an index into the
// kind-specific auxiliary arrays/string storage.
type arenaTerm struct {
	id        syntaxID
	start     int
	end       int
	kind      termKind
	payload   uint64
	selectors nodeSpan
	castType  typeID
}

type arenaTable struct {
	fields nodeSpan
}

type arenaCall struct {
	target   termID
	receiver termID
	typeArgs nodeSpan // typeID
	args     nodeSpan
}

type arenaFunction struct {
	id                 syntaxID
	functionID         int
	typeParams         nodeSpan // stringID
	typeParamID        syntaxID
	typePacks          nodeSpan // stringID
	typePackID         syntaxID
	params             nodeSpan // stringID
	paramID            syntaxID
	paramAnnotations   nodeSpan // typeID
	variadic           bool
	variadicAnnotation typeID
	returnAnnotation   typeID
	statements         nodeSpan // statementID
}
type arenaIfExpression struct {
	condition expressionID
	thenValue expressionID
	elseValue expressionID
}
type arenaPower struct {
	base     termID
	exponent termID
}

// syntaxArena owns all expression-ladder nodes and their child lists. It is
// attached to syntaxTree (never to program) so copies of the parser's program
// value cannot accidentally lose the storage owner.
type syntaxArena struct {
	budget             syntaxNodeBudget
	expressions        []arenaExpression
	andExpressions     []arenaAndExpression
	comparisons        []arenaComparisonExpression
	concats            []arenaConcatExpression
	additives          []arenaAdditiveExpression
	multiplicatives    []arenaMultiplicativeExpression
	terms              []arenaTerm
	expressionTerms    []andExpressionID
	andTerms           []comparisonExpressionID
	concatRest         []additiveExpressionID
	additiveRest       []arenaAdditivePart
	multiplicativeRest []arenaMultiplicativePart
	selectors          []arenaSelector
	strings            []string
	// stringLiterals is populated during parsing for literal strings only. It
	// keeps the boxed Value identity stable when compiler passes revisit the
	// same arena term (important for immutable constant pools).
	stringLiterals map[stringID]Value
	tables         []arenaTable
	tableFields    []arenaTableField
	calls          []arenaCall
	callArgs       []expressionID
	functions      []arenaFunction
	ifExpressions  []arenaIfExpression
	powers         []arenaPower
	statements     statementArena
	types          syntaxTypeArena
}

func newSyntaxArena(tokenCount int, maxNodes uint64) *syntaxArena {
	if tokenCount <= 0 {
		return &syntaxArena{}
	}
	if maxNodes != 0 && uint64(tokenCount) > maxNodes {
		if maxNodes < uint64(^uint(0)>>1) {
			tokenCount = int(maxNodes)
		}
	}
	baseNodes := tokenCount/5 + 1
	termNodes := tokenCount*2/5 + 1
	statementNodes := tokenCount/3 + 1
	childNodes := tokenCount/2 + 1
	return &syntaxArena{
		expressions:     make([]arenaExpression, 0, baseNodes),
		andExpressions:  make([]arenaAndExpression, 0, baseNodes),
		comparisons:     make([]arenaComparisonExpression, 0, baseNodes),
		concats:         make([]arenaConcatExpression, 0, baseNodes),
		additives:       make([]arenaAdditiveExpression, 0, baseNodes),
		multiplicatives: make([]arenaMultiplicativeExpression, 0, termNodes),
		terms:           make([]arenaTerm, 0, termNodes),
		expressionTerms: make([]andExpressionID, 0, baseNodes),
		andTerms:        make([]comparisonExpressionID, 0, baseNodes),
		additiveRest:    make([]arenaAdditivePart, 0, baseNodes),
		strings:         make([]string, 0, baseNodes),
		types:           newSyntaxTypeArena(baseNodes),
		statements: statementArena{
			statements:           make([]arenaStatement, 0, statementNodes),
			statementIDs:         make([]statementID, 0, statementNodes),
			assignTargets:        make([]arenaAssignTarget, 0, childNodes),
			locals:               make([]arenaLocalStatement, 0, baseNodes),
			localFuncs:           make([]arenaFunctionStatement, 0, baseNodes/8+1),
			functionDecls:        make([]arenaFunctionStatement, 0, baseNodes/8+1),
			assigns:              make([]arenaAssignStatement, 0, baseNodes),
			ifStatements:         make([]arenaIfStatement, 0, baseNodes/8+1),
			whileStatements:      make([]arenaWhileStatement, 0, baseNodes/8+1),
			forStatements:        make([]arenaForStatement, 0, baseNodes/8+1),
			genericForStatements: make([]arenaGenericForStatement, 0, baseNodes/8+1),
			repeatStatements:     make([]arenaRepeatStatement, 0, baseNodes/8+1),
			blockStatements:      make([]arenaBlockStatement, 0, baseNodes/8+1),
			returnStatements:     make([]arenaReturnStatement, 0, baseNodes/8+1),
			typeAliases:          make([]arenaTypeAliasStatement, 0, baseNodes/8+1),
			assignTargetIDs:      make([]assignTargetID, 0, childNodes),
			stringIDs:            make([]stringID, 0, childNodes),
			typeIDs:              make([]typeID, 0, childNodes),
			expressionIDs:        make([]expressionID, 0, childNodes),
			sourceRanges:         make([]sourceRange, 0, childNodes),
		},
	}
}

type arenaComparisonOp uint8
type arenaAdditiveOp uint8
type arenaMultiplicativeOp uint8

const (
	arenaComparisonNone arenaComparisonOp = iota
	arenaComparisonEqual
	arenaComparisonNotEqual
	arenaComparisonLess
	arenaComparisonLessEqual
	arenaComparisonGreater
	arenaComparisonGreaterEqual
)
const (
	arenaAdditiveNone arenaAdditiveOp = iota
	arenaAdditiveAdd
	arenaAdditiveSubtract
)
const (
	arenaMultiplicativeNone arenaMultiplicativeOp = iota
	arenaMultiplicativeMultiply
	arenaMultiplicativeDivide
	arenaMultiplicativeModulo
	arenaMultiplicativeFloorDiv
)

type arenaAdditivePart struct {
	op    arenaAdditiveOp
	value multiplicativeExpressionID
}
type arenaMultiplicativePart struct {
	op    arenaMultiplicativeOp
	value termID
}
type arenaSelector struct {
	field stringID
	index expressionID
}
type arenaTableField struct {
	name       stringID
	arrayIndex int
	key        expressionID
	value      expressionID
}

// The append methods are the parser-facing arena API. They assign index+1 IDs
// and never reserve a sentinel node, keeping zero available for "none".
func (a *syntaxArena) appendExpression(node arenaExpression) expressionID {
	if a == nil || !a.budget.reserve() {
		return 0
	}
	a.expressions = append(a.expressions, node)
	return expressionID(len(a.expressions))
}
func (a *syntaxArena) appendAnd(node arenaAndExpression) andExpressionID {
	a.andExpressions = append(a.andExpressions, node)
	return andExpressionID(len(a.andExpressions))
}
func (a *syntaxArena) appendComparison(node arenaComparisonExpression) comparisonExpressionID {
	a.comparisons = append(a.comparisons, node)
	return comparisonExpressionID(len(a.comparisons))
}
func (a *syntaxArena) appendConcat(node arenaConcatExpression) concatExpressionID {
	a.concats = append(a.concats, node)
	return concatExpressionID(len(a.concats))
}
func (a *syntaxArena) appendAdditive(node arenaAdditiveExpression) additiveExpressionID {
	a.additives = append(a.additives, node)
	return additiveExpressionID(len(a.additives))
}
func (a *syntaxArena) appendMultiplicative(node arenaMultiplicativeExpression) multiplicativeExpressionID {
	a.multiplicatives = append(a.multiplicatives, node)
	return multiplicativeExpressionID(len(a.multiplicatives))
}
func (a *syntaxArena) appendTerm(node arenaTerm) termID {
	if a == nil || !a.budget.reserve() {
		return 0
	}
	a.terms = append(a.terms, node)
	return termID(len(a.terms))
}
func (a *syntaxArena) appendTable(node arenaTable) arenaTableID {
	a.tables = append(a.tables, node)
	return arenaTableID(len(a.tables))
}
func (a *syntaxArena) appendCall(node arenaCall) arenaCallID {
	a.calls = append(a.calls, node)
	return arenaCallID(len(a.calls))
}
func (a *syntaxArena) appendFunction(node arenaFunction) arenaFunctionID {
	if a == nil || !a.budget.reserve() {
		return 0
	}
	a.functions = append(a.functions, node)
	return arenaFunctionID(len(a.functions))
}
func (a *syntaxArena) appendIfExpression(node arenaIfExpression) arenaIfExpressionID {
	a.ifExpressions = append(a.ifExpressions, node)
	return arenaIfExpressionID(len(a.ifExpressions))
}
func (a *syntaxArena) appendPower(node arenaPower) arenaPowerID {
	a.powers = append(a.powers, node)
	return arenaPowerID(len(a.powers))
}

func appendArenaIDsChecked[T any](dst *[]T, values []T) (nodeSpan, bool) {
	if uint64(len(*dst)) > math.MaxUint32 || uint64(len(values)) > math.MaxUint32-uint64(len(*dst)) {
		return nodeSpan{}, false
	}
	start := len(*dst)
	*dst = append(*dst, values...)
	return nodeSpan{start: uint32(start), count: uint32(len(values))}, true
}

func appendArenaIDs[T any](dst *[]T, values []T) nodeSpan {
	span, _ := appendArenaIDsChecked(dst, values)
	return span
}

func (a *syntaxArena) appendExpressionTerms(values []andExpressionID) nodeSpan {
	return appendArenaIDs(&a.expressionTerms, values)
}
func (a *syntaxArena) appendAndTerms(values []comparisonExpressionID) nodeSpan {
	return appendArenaIDs(&a.andTerms, values)
}
func (a *syntaxArena) appendConcatRest(values []additiveExpressionID) nodeSpan {
	return appendArenaIDs(&a.concatRest, values)
}
func (a *syntaxArena) appendAdditiveRest(values []arenaAdditivePart) nodeSpan {
	return appendArenaIDs(&a.additiveRest, values)
}
func (a *syntaxArena) appendMultiplicativeRest(values []arenaMultiplicativePart) nodeSpan {
	return appendArenaIDs(&a.multiplicativeRest, values)
}
func (a *syntaxArena) appendSelectors(values []arenaSelector) nodeSpan {
	return appendArenaIDs(&a.selectors, values)
}
func (a *syntaxArena) appendCallArgs(values []expressionID) nodeSpan {
	return appendArenaIDs(&a.callArgs, values)
}
func (a *syntaxArena) appendTableFields(values []arenaTableField) nodeSpan {
	return appendArenaIDs(&a.tableFields, values)
}

func arenaComparisonOperator(op comparisonOperator) arenaComparisonOp {
	switch op {
	case comparisonEqual:
		return arenaComparisonEqual
	case comparisonNotEqual:
		return arenaComparisonNotEqual
	case comparisonLess:
		return arenaComparisonLess
	case comparisonLessEqual:
		return arenaComparisonLessEqual
	case comparisonGreater:
		return arenaComparisonGreater
	case comparisonGreaterEqual:
		return arenaComparisonGreaterEqual
	default:
		return arenaComparisonNone
	}
}

func arenaAdditiveOperator(op additiveOperator) arenaAdditiveOp {
	if op == additiveAdd {
		return arenaAdditiveAdd
	}
	if op == additiveSubtract {
		return arenaAdditiveSubtract
	}
	return arenaAdditiveNone
}

func arenaMultiplicativeOperator(op multiplicativeOperator) arenaMultiplicativeOp {
	switch op {
	case multiplicativeMultiply:
		return arenaMultiplicativeMultiply
	case multiplicativeDivide:
		return arenaMultiplicativeDivide
	case multiplicativeModulo:
		return arenaMultiplicativeModulo
	case multiplicativeFloorDiv:
		return arenaMultiplicativeFloorDiv
	default:
		return arenaMultiplicativeNone
	}
}

func (a *syntaxArena) expression(id expressionID) (arenaExpression, bool) {
	if a == nil {
		return arenaExpression{}, false
	}
	return arenaNode(a.expressions, id)
}
func (a *syntaxArena) and(id andExpressionID) (arenaAndExpression, bool) {
	if a == nil {
		return arenaAndExpression{}, false
	}
	return arenaNode(a.andExpressions, id)
}
func (a *syntaxArena) comparison(id comparisonExpressionID) (arenaComparisonExpression, bool) {
	if a == nil {
		return arenaComparisonExpression{}, false
	}
	return arenaNode(a.comparisons, id)
}
func (a *syntaxArena) concat(id concatExpressionID) (arenaConcatExpression, bool) {
	if a == nil {
		return arenaConcatExpression{}, false
	}
	return arenaNode(a.concats, id)
}
func (a *syntaxArena) additive(id additiveExpressionID) (arenaAdditiveExpression, bool) {
	if a == nil {
		return arenaAdditiveExpression{}, false
	}
	return arenaNode(a.additives, id)
}
func (a *syntaxArena) multiplicative(id multiplicativeExpressionID) (arenaMultiplicativeExpression, bool) {
	if a == nil {
		return arenaMultiplicativeExpression{}, false
	}
	return arenaNode(a.multiplicatives, id)
}
func (a *syntaxArena) term(id termID) (arenaTerm, bool) {
	if a == nil {
		return arenaTerm{}, false
	}
	return arenaNode(a.terms, id)
}
func (a *syntaxArena) table(id arenaTableID) (arenaTable, bool) {
	if a == nil {
		return arenaTable{}, false
	}
	return arenaNode(a.tables, id)
}
func (a *syntaxArena) function(id arenaFunctionID) (arenaFunction, bool) {
	if a == nil {
		return arenaFunction{}, false
	}
	return arenaNode(a.functions, id)
}
func (a *syntaxArena) ifExpression(id arenaIfExpressionID) (arenaIfExpression, bool) {
	if a == nil {
		return arenaIfExpression{}, false
	}
	return arenaNode(a.ifExpressions, id)
}
func (a *syntaxArena) call(id arenaCallID) (arenaCall, bool) {
	if a == nil {
		return arenaCall{}, false
	}
	return arenaNode(a.calls, id)
}
func (a *syntaxArena) power(id arenaPowerID) (arenaPower, bool) {
	if a == nil {
		return arenaPower{}, false
	}
	return arenaNode(a.powers, id)
}

func (a *syntaxArena) stringValue(id stringID) (string, bool) {
	if a == nil || id == 0 || uint64(id) > uint64(len(a.strings)) {
		return "", false
	}
	return a.strings[id-1], true
}

func (a *syntaxArena) andIDs(span nodeSpan) ([]andExpressionID, bool) {
	if a == nil {
		return nil, false
	}
	return arenaSpan(a.expressionTerms, span)
}
func (a *syntaxArena) comparisonIDs(span nodeSpan) ([]comparisonExpressionID, bool) {
	if a == nil {
		return nil, false
	}
	return arenaSpan(a.andTerms, span)
}
func (a *syntaxArena) concatIDs(span nodeSpan) ([]additiveExpressionID, bool) {
	if a == nil {
		return nil, false
	}
	return arenaSpan(a.concatRest, span)
}
func (a *syntaxArena) additiveParts(span nodeSpan) ([]arenaAdditivePart, bool) {
	if a == nil {
		return nil, false
	}
	return arenaSpan(a.additiveRest, span)
}
func (a *syntaxArena) multiplicativeParts(span nodeSpan) ([]arenaMultiplicativePart, bool) {
	if a == nil {
		return nil, false
	}
	return arenaSpan(a.multiplicativeRest, span)
}

func (a *syntaxArena) selectorIDs(span nodeSpan) ([]arenaSelector, bool) {
	if a == nil {
		return nil, false
	}
	return arenaSpan(a.selectors, span)
}

func (a *syntaxArena) tableFieldsIDs(span nodeSpan) ([]arenaTableField, bool) {
	if a == nil {
		return nil, false
	}
	return arenaSpan(a.tableFields, span)
}

func (a *syntaxArena) callArgIDs(span nodeSpan) ([]expressionID, bool) {
	if a == nil {
		return nil, false
	}
	return arenaSpan(a.callArgs, span)
}
