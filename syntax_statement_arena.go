package ember

// statementID and assignTargetID are indexes into syntaxArena. Zero is the
// absence value; real IDs are one-based so malformed IDs can be rejected by
// the checked resolvers below.
type statementID uint32
type assignTargetID uint32
type typeID uint32

// arenaStatement is the only node stored for a statement. payload is an
// index+1 into the kind-specific payload array selected by kind. Keeping the
// discriminant and payload together removes the old pointer union while
// retaining a compact, checked representation.
type arenaStatement struct {
	id      syntaxID
	kind    syntaxStatementKind
	payload uint32
}

type arenaLocalStatement struct {
	nameID      syntaxID
	names       nodeSpan // stringID
	nameRanges  nodeSpan // sourceRange
	annotations nodeSpan // typeID
	values      nodeSpan // expressionID
}

type arenaTypeAliasStatement struct {
	id          syntaxID
	exported    bool
	name        stringID
	nameID      syntaxID
	start       int
	end         int
	nameStart   int
	nameEnd     int
	typeParams  nodeSpan // stringID
	typeParamID syntaxID
	typePacks   nodeSpan // stringID
	typePackID  syntaxID
	value       typeID
}

type arenaFunctionStatement struct {
	id                 syntaxID
	functionID         int
	name               stringID
	nameID             syntaxID
	target             assignTargetID
	typeParams         nodeSpan // stringID
	typeParamID        syntaxID
	typePacks          nodeSpan // stringID
	typePackID         syntaxID
	params             nodeSpan // stringID
	paramID            syntaxID
	selfID             syntaxID
	paramAnnotations   nodeSpan // typeID
	variadic           bool
	variadicAnnotation typeID
	returnAnnotation   typeID
	statements         nodeSpan // statementID
	method             bool
}

type arenaAssignStatement struct {
	targets nodeSpan // assignTargetID
	values  nodeSpan // expressionID
}

type arenaIfStatement struct {
	condition      expressionID
	thenStatements nodeSpan // statementID
	elseStatements nodeSpan // statementID
}

type arenaWhileStatement struct {
	condition  expressionID
	statements nodeSpan // statementID
}

type arenaForStatement struct {
	name       stringID
	nameID     syntaxID
	start      expressionID
	limit      expressionID
	step       expressionID
	statements nodeSpan // statementID
}

type arenaGenericForStatement struct {
	names      nodeSpan // stringID
	nameID     syntaxID
	values     nodeSpan // expressionID
	statements nodeSpan // statementID
}

type arenaRepeatStatement struct {
	statements nodeSpan // statementID
	condition  expressionID
}

type arenaBlockStatement struct{ statements nodeSpan } // statementID

type arenaReturnStatement struct {
	start  int
	end    int
	values nodeSpan // expressionID
}

type arenaAssignTarget struct {
	id        syntaxID
	start     int
	end       int
	name      stringID
	selectors nodeSpan // arenaSelector
}

// statementArena keeps node and payload storage separate from expression
// storage. Child lists are contiguous spans into typed sidecars, so parsing a
// one-statement assignment does not allocate a temporary []statement or
// []expressionID.
type statementArena struct {
	statements    []arenaStatement
	statementIDs  []statementID
	assignTargets []arenaAssignTarget

	locals               []arenaLocalStatement
	localFuncs           []arenaFunctionStatement
	functionDecls        []arenaFunctionStatement
	assigns              []arenaAssignStatement
	ifStatements         []arenaIfStatement
	whileStatements      []arenaWhileStatement
	forStatements        []arenaForStatement
	genericForStatements []arenaGenericForStatement
	repeatStatements     []arenaRepeatStatement
	blockStatements      []arenaBlockStatement
	returnStatements     []arenaReturnStatement
	typeAliases          []arenaTypeAliasStatement

	assignTargetIDs []assignTargetID
	stringIDs       []stringID
	typeIDs         []typeID
	expressionIDs   []expressionID
	sourceRanges    []sourceRange
}

func (a *statementArena) appendStatement(node arenaStatement) statementID {
	a.statements = append(a.statements, node)
	return statementID(len(a.statements))
}

func (a *statementArena) appendAssignTarget(node arenaAssignTarget) assignTargetID {
	a.assignTargets = append(a.assignTargets, node)
	return assignTargetID(len(a.assignTargets))
}

func (a *statementArena) appendLocal(node arenaLocalStatement) uint32 {
	a.locals = append(a.locals, node)
	return uint32(len(a.locals))
}
func (a *statementArena) appendLocalFunction(node arenaFunctionStatement) uint32 {
	a.localFuncs = append(a.localFuncs, node)
	return uint32(len(a.localFuncs))
}
func (a *statementArena) appendFunctionDeclaration(node arenaFunctionStatement) uint32 {
	a.functionDecls = append(a.functionDecls, node)
	return uint32(len(a.functionDecls))
}
func (a *statementArena) appendAssign(node arenaAssignStatement) uint32 {
	a.assigns = append(a.assigns, node)
	return uint32(len(a.assigns))
}
func (a *statementArena) appendIf(node arenaIfStatement) uint32 {
	a.ifStatements = append(a.ifStatements, node)
	return uint32(len(a.ifStatements))
}
func (a *statementArena) appendWhile(node arenaWhileStatement) uint32 {
	a.whileStatements = append(a.whileStatements, node)
	return uint32(len(a.whileStatements))
}
func (a *statementArena) appendFor(node arenaForStatement) uint32 {
	a.forStatements = append(a.forStatements, node)
	return uint32(len(a.forStatements))
}
func (a *statementArena) appendGenericFor(node arenaGenericForStatement) uint32 {
	a.genericForStatements = append(a.genericForStatements, node)
	return uint32(len(a.genericForStatements))
}
func (a *statementArena) appendRepeat(node arenaRepeatStatement) uint32 {
	a.repeatStatements = append(a.repeatStatements, node)
	return uint32(len(a.repeatStatements))
}
func (a *statementArena) appendBlock(node arenaBlockStatement) uint32 {
	a.blockStatements = append(a.blockStatements, node)
	return uint32(len(a.blockStatements))
}
func (a *statementArena) appendReturn(node arenaReturnStatement) uint32 {
	a.returnStatements = append(a.returnStatements, node)
	return uint32(len(a.returnStatements))
}
func (a *statementArena) appendTypeAlias(node arenaTypeAliasStatement) uint32 {
	a.typeAliases = append(a.typeAliases, node)
	return uint32(len(a.typeAliases))
}

func (a *statementArena) statement(id statementID) (arenaStatement, bool) {
	return arenaNode(a.statements, id)
}
func (a *statementArena) assignTarget(id assignTargetID) (arenaAssignTarget, bool) {
	return arenaNode(a.assignTargets, id)
}

func (a *statementArena) spanStatements(span nodeSpan) ([]statementID, bool) {
	return arenaSpan(a.statementIDs, span)
}
func (a *statementArena) spanAssignTargets(span nodeSpan) ([]assignTargetID, bool) {
	return arenaSpan(a.assignTargetIDs, span)
}
func (a *statementArena) spanStrings(span nodeSpan) ([]stringID, bool) {
	return arenaSpan(a.stringIDs, span)
}
func (a *statementArena) spanTypes(span nodeSpan) ([]typeID, bool) {
	return arenaSpan(a.typeIDs, span)
}
func (a *statementArena) spanExpressions(span nodeSpan) ([]expressionID, bool) {
	return arenaSpan(a.expressionIDs, span)
}
func (a *statementArena) spanRanges(span nodeSpan) ([]sourceRange, bool) {
	return arenaSpan(a.sourceRanges, span)
}

func appendStatementSpan[T any](dst *[]T, values []T) (nodeSpan, bool) {
	return appendArenaIDsChecked(dst, values)
}
