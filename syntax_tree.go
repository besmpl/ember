package ember

import "math"

// syntaxTree owns parser storage and is the only read seam used by compiler
// consumers. Statements and types remain concrete until E7; expressions are
// resolved through the arena-backed typed-ID accessors below.
type syntaxTree struct {
	root  program
	arena *syntaxArena
}

func newSyntaxTree(root program) syntaxTree {
	return syntaxTree{root: root}
}

func newSyntaxTreeWithArena(root program, arena *syntaxArena) syntaxTree {
	return syntaxTree{root: root, arena: arena}
}

func (tree syntaxTree) statements() []statement { return tree.root.statements }
func (tree syntaxTree) statementIDs() ([]statementID, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.statements.spanStatements(tree.root.statementSpan)
}
func (tree syntaxTree) statementAt(index int) (statementID, bool) {
	ids, ok := tree.statementIDs()
	if !ok || index < 0 || index >= len(ids) {
		return 0, false
	}
	return ids[index], true
}
func (tree syntaxTree) statementNode(id statementID) (arenaStatement, bool) {
	if tree.arena == nil {
		return arenaStatement{}, false
	}
	return tree.arena.statements.statement(id)
}
func (tree syntaxTree) statementKindID(id statementID) syntaxStatementKind {
	node, ok := tree.statementNode(id)
	if !ok {
		return syntaxStatementUnknown
	}
	return node.kind
}
func (tree syntaxTree) localArena(id statementID) (arenaLocalStatement, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementLocal {
		return arenaLocalStatement{}, false
	}
	return arenaNode(tree.arena.statements.locals, uint32(node.payload))
}
func (tree syntaxTree) localFunctionArena(id statementID) (arenaFunctionStatement, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementLocalFunction {
		return arenaFunctionStatement{}, false
	}
	return arenaNode(tree.arena.statements.localFuncs, uint32(node.payload))
}
func (tree syntaxTree) functionDeclarationArena(id statementID) (arenaFunctionStatement, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementFunctionDeclaration {
		return arenaFunctionStatement{}, false
	}
	return arenaNode(tree.arena.statements.functionDecls, uint32(node.payload))
}
func (tree syntaxTree) assignmentArena(id statementID) (arenaAssignStatement, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementAssign {
		return arenaAssignStatement{}, false
	}
	return arenaNode(tree.arena.statements.assigns, uint32(node.payload))
}
func (tree syntaxTree) ifArena(id statementID) (arenaIfStatement, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementIf {
		return arenaIfStatement{}, false
	}
	return arenaNode(tree.arena.statements.ifStatements, uint32(node.payload))
}
func (tree syntaxTree) whileArena(id statementID) (arenaWhileStatement, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementWhile {
		return arenaWhileStatement{}, false
	}
	return arenaNode(tree.arena.statements.whileStatements, uint32(node.payload))
}
func (tree syntaxTree) forArena(id statementID) (arenaForStatement, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementFor {
		return arenaForStatement{}, false
	}
	return arenaNode(tree.arena.statements.forStatements, uint32(node.payload))
}
func (tree syntaxTree) genericForArena(id statementID) (arenaGenericForStatement, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementGenericFor {
		return arenaGenericForStatement{}, false
	}
	return arenaNode(tree.arena.statements.genericForStatements, uint32(node.payload))
}
func (tree syntaxTree) repeatArena(id statementID) (arenaRepeatStatement, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementRepeat {
		return arenaRepeatStatement{}, false
	}
	return arenaNode(tree.arena.statements.repeatStatements, uint32(node.payload))
}
func (tree syntaxTree) blockArena(id statementID) (arenaBlockStatement, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementBlock {
		return arenaBlockStatement{}, false
	}
	return arenaNode(tree.arena.statements.blockStatements, uint32(node.payload))
}
func (tree syntaxTree) returnArena(id statementID) (arenaReturnStatement, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementReturn {
		return arenaReturnStatement{}, false
	}
	return arenaNode(tree.arena.statements.returnStatements, uint32(node.payload))
}
func (tree syntaxTree) typeAliasArena(id statementID) (arenaTypeAliasStatement, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementTypeAlias {
		return arenaTypeAliasStatement{}, false
	}
	return arenaNode(tree.arena.statements.typeAliases, uint32(node.payload))
}
func (tree syntaxTree) callStatementTerm(id statementID) (termID, bool) {
	node, ok := tree.statementNode(id)
	if !ok || node.kind != syntaxStatementCall || node.payload == 0 {
		return 0, false
	}
	term := termID(node.payload)
	if _, ok := tree.arenaTerm(term); !ok {
		return 0, false
	}
	return term, true
}
func (tree syntaxTree) statementArenaTarget(id assignTargetID) (arenaAssignTarget, bool) {
	if tree.arena == nil {
		return arenaAssignTarget{}, false
	}
	return tree.arena.statements.assignTarget(id)
}
func (tree syntaxTree) statementType(id typeID) (*typeExpression, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.typeNode(id)
}
func (tree syntaxTree) statementStrings(span nodeSpan) ([]stringID, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.statements.spanStrings(span)
}
func (tree syntaxTree) statementTypes(span nodeSpan) ([]typeID, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.statements.spanTypes(span)
}
func (tree syntaxTree) statementExpressions(span nodeSpan) ([]expressionID, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.statements.spanExpressions(span)
}
func (tree syntaxTree) statementChildren(span nodeSpan) ([]statementID, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.statements.spanStatements(span)
}
func (tree syntaxTree) statementTargets(span nodeSpan) ([]assignTargetID, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.statements.spanAssignTargets(span)
}
func (tree syntaxTree) statementRanges(span nodeSpan) ([]sourceRange, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.statements.spanRanges(span)
}
func (tree syntaxTree) selectorSpan(span nodeSpan) ([]arenaSelector, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.selectorIDs(span)
}
func (tree syntaxTree) mode() sourceMode { return tree.root.mode }
func (tree syntaxTree) nodeCount() int   { return tree.root.nodeCount }
func (tree syntaxTree) id() syntaxID     { return tree.root.id }

func (tree syntaxTree) statement(index int) *statement {
	if index < 0 || index >= len(tree.root.statements) {
		return nil
	}
	return &tree.root.statements[index]
}

func (tree syntaxTree) statementID(stmt *statement) syntaxID {
	if stmt == nil {
		return 0
	}
	if stmt.arenaID != 0 {
		if node, ok := tree.statementNode(stmt.arenaID); ok {
			return node.id
		}
	}
	return stmt.id
}

// Arena accessors form the checked facade used by ID-based parser consumers.
// A zero ID, a nil arena, or a malformed child span always resolves as absent.
func (tree syntaxTree) arenaExpression(id expressionID) (arenaExpression, bool) {
	if tree.arena == nil {
		return arenaExpression{}, false
	}
	return tree.arena.expression(id)
}
func (tree syntaxTree) arenaAnd(id andExpressionID) (arenaAndExpression, bool) {
	if tree.arena == nil {
		return arenaAndExpression{}, false
	}
	return tree.arena.and(id)
}
func (tree syntaxTree) arenaComparison(id comparisonExpressionID) (arenaComparisonExpression, bool) {
	if tree.arena == nil {
		return arenaComparisonExpression{}, false
	}
	return tree.arena.comparison(id)
}
func (tree syntaxTree) arenaConcat(id concatExpressionID) (arenaConcatExpression, bool) {
	if tree.arena == nil {
		return arenaConcatExpression{}, false
	}
	return tree.arena.concat(id)
}
func (tree syntaxTree) arenaAdditive(id additiveExpressionID) (arenaAdditiveExpression, bool) {
	if tree.arena == nil {
		return arenaAdditiveExpression{}, false
	}
	return tree.arena.additive(id)
}
func (tree syntaxTree) arenaMultiplicative(id multiplicativeExpressionID) (arenaMultiplicativeExpression, bool) {
	if tree.arena == nil {
		return arenaMultiplicativeExpression{}, false
	}
	return tree.arena.multiplicative(id)
}
func (tree syntaxTree) arenaTerm(id termID) (arenaTerm, bool) {
	if tree.arena == nil {
		return arenaTerm{}, false
	}
	return tree.arena.term(id)
}
func (tree syntaxTree) arenaExpressionTerms(span nodeSpan) ([]andExpressionID, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.andIDs(span)
}
func (tree syntaxTree) arenaAndTerms(span nodeSpan) ([]comparisonExpressionID, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.comparisonIDs(span)
}
func (tree syntaxTree) arenaConcatRest(span nodeSpan) ([]additiveExpressionID, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.concatIDs(span)
}
func (tree syntaxTree) arenaAdditiveRest(span nodeSpan) ([]arenaAdditivePart, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.additiveParts(span)
}
func (tree syntaxTree) arenaMultiplicativeRest(span nodeSpan) ([]arenaMultiplicativePart, bool) {
	if tree.arena == nil {
		return nil, false
	}
	return tree.arena.multiplicativeParts(span)
}

type syntaxStatementKind uint8

const (
	syntaxStatementUnknown syntaxStatementKind = iota
	syntaxStatementLocal
	syntaxStatementLocalFunction
	syntaxStatementFunctionDeclaration
	syntaxStatementAssign
	syntaxStatementCall
	syntaxStatementIf
	syntaxStatementWhile
	syntaxStatementFor
	syntaxStatementGenericFor
	syntaxStatementRepeat
	syntaxStatementBlock
	syntaxStatementTypeAlias
	syntaxStatementBreak
	syntaxStatementContinue
	syntaxStatementReturn
)

func (tree syntaxTree) statementKind(stmt *statement) syntaxStatementKind {
	if stmt == nil {
		return syntaxStatementUnknown
	}
	if stmt.arenaID != 0 {
		return tree.statementKindID(stmt.arenaID)
	}
	switch {
	case stmt.local != nil:
		return syntaxStatementLocal
	case stmt.localFunc != nil:
		return syntaxStatementLocalFunction
	case stmt.funcDecl != nil:
		return syntaxStatementFunctionDeclaration
	case stmt.assign != nil:
		return syntaxStatementAssign
	case stmt.call != 0:
		return syntaxStatementCall
	case stmt.ifStmt != nil:
		return syntaxStatementIf
	case stmt.while != nil:
		return syntaxStatementWhile
	case stmt.forLoop != nil:
		return syntaxStatementFor
	case stmt.genericFor != nil:
		return syntaxStatementGenericFor
	case stmt.repeat != nil:
		return syntaxStatementRepeat
	case stmt.block != nil:
		return syntaxStatementBlock
	case stmt.typeAlias != nil:
		return syntaxStatementTypeAlias
	case stmt.breaking:
		return syntaxStatementBreak
	case stmt.continues:
		return syntaxStatementContinue
	case stmt.ret != nil:
		return syntaxStatementReturn
	default:
		return syntaxStatementUnknown
	}
}

// Statement payload accessors preserve nil when the payload is absent.
func (tree syntaxTree) local(stmt *statement) *localStatement {
	if stmt == nil {
		return nil
	}
	return stmt.local
}
func (tree syntaxTree) localFunction(stmt *statement) *localFunctionStatement {
	if stmt == nil {
		return nil
	}
	return stmt.localFunc
}
func (tree syntaxTree) functionDeclaration(stmt *statement) *functionDeclarationStatement {
	if stmt == nil {
		return nil
	}
	return stmt.funcDecl
}
func (tree syntaxTree) assignment(stmt *statement) *assignStatement {
	if stmt == nil {
		return nil
	}
	return stmt.assign
}
func (tree syntaxTree) call(stmt *statement) termID {
	if stmt == nil {
		return 0
	}
	return stmt.call
}
func (tree syntaxTree) ifStatement(stmt *statement) *ifStatement {
	if stmt == nil {
		return nil
	}
	return stmt.ifStmt
}
func (tree syntaxTree) whileStatement(stmt *statement) *whileStatement {
	if stmt == nil {
		return nil
	}
	return stmt.while
}
func (tree syntaxTree) forStatement(stmt *statement) *forStatement {
	if stmt == nil {
		return nil
	}
	return stmt.forLoop
}
func (tree syntaxTree) genericForStatement(stmt *statement) *genericForStatement {
	if stmt == nil {
		return nil
	}
	return stmt.genericFor
}
func (tree syntaxTree) repeatStatement(stmt *statement) *repeatStatement {
	if stmt == nil {
		return nil
	}
	return stmt.repeat
}
func (tree syntaxTree) blockStatement(stmt *statement) *blockStatement {
	if stmt == nil {
		return nil
	}
	return stmt.block
}
func (tree syntaxTree) typeAliasStatement(stmt *statement) *typeAliasStatement {
	if stmt == nil {
		return nil
	}
	return stmt.typeAlias
}
func (tree syntaxTree) returnStatement(stmt *statement) *returnStatement {
	if stmt == nil {
		return nil
	}
	return stmt.ret
}

// Statement metadata and children. These methods intentionally return the
// parser's existing slices and pointers so callers do not allocate or copy.
func (tree syntaxTree) localNames(stmt *localStatement) []string {
	if stmt == nil {
		return nil
	}
	return stmt.names
}
func (tree syntaxTree) localNameID(stmt *localStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.nameID
}
func (tree syntaxTree) localNameRanges(stmt *localStatement) []sourceRange {
	if stmt == nil {
		return nil
	}
	return stmt.nameRanges
}
func (tree syntaxTree) localAnnotations(stmt *localStatement) []*typeExpression {
	if stmt == nil {
		return nil
	}
	return stmt.annotations
}
func (tree syntaxTree) localValues(stmt *localStatement) []expressionID {
	if stmt == nil {
		return nil
	}
	return stmt.values
}
func (tree syntaxTree) localFunctionID(stmt *localFunctionStatement) int {
	if stmt == nil {
		return 0
	}
	return stmt.functionID
}
func (tree syntaxTree) localFunctionName(stmt *localFunctionStatement) string {
	if stmt == nil {
		return ""
	}
	return stmt.name
}
func (tree syntaxTree) localFunctionNameID(stmt *localFunctionStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.nameID
}
func (tree syntaxTree) localFunctionTypeParams(stmt *localFunctionStatement) []string {
	if stmt == nil {
		return nil
	}
	return stmt.typeParams
}
func (tree syntaxTree) localFunctionTypeParamID(stmt *localFunctionStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.typeParamID
}
func (tree syntaxTree) localFunctionTypePacks(stmt *localFunctionStatement) []string {
	if stmt == nil {
		return nil
	}
	return stmt.typePacks
}
func (tree syntaxTree) localFunctionTypePackID(stmt *localFunctionStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.typePackID
}
func (tree syntaxTree) localFunctionParams(stmt *localFunctionStatement) []string {
	if stmt == nil {
		return nil
	}
	return stmt.params
}
func (tree syntaxTree) localFunctionParamID(stmt *localFunctionStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.paramID
}
func (tree syntaxTree) localFunctionParamAnnotations(stmt *localFunctionStatement) []*typeExpression {
	if stmt == nil {
		return nil
	}
	return stmt.paramAnnotations
}
func (tree syntaxTree) localFunctionVariadic(stmt *localFunctionStatement) bool {
	return stmt != nil && stmt.variadic
}
func (tree syntaxTree) localFunctionVariadicAnnotation(stmt *localFunctionStatement) *typeExpression {
	if stmt == nil {
		return nil
	}
	return stmt.variadicAnnotation
}
func (tree syntaxTree) localFunctionReturnAnnotation(stmt *localFunctionStatement) *typeExpression {
	if stmt == nil {
		return nil
	}
	return stmt.returnAnnotation
}
func (tree syntaxTree) localFunctionStatements(stmt *localFunctionStatement) []statement {
	if stmt == nil {
		return nil
	}
	return stmt.statements
}
func (tree syntaxTree) functionDeclarationID(stmt *functionDeclarationStatement) int {
	if stmt == nil {
		return 0
	}
	return stmt.functionID
}
func (tree syntaxTree) functionDeclarationTarget(stmt *functionDeclarationStatement) *assignTarget {
	if stmt == nil {
		return nil
	}
	return &stmt.target
}
func (tree syntaxTree) functionDeclarationTypeParams(stmt *functionDeclarationStatement) []string {
	if stmt == nil {
		return nil
	}
	return stmt.typeParams
}
func (tree syntaxTree) functionDeclarationTypeParamID(stmt *functionDeclarationStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.typeParamID
}
func (tree syntaxTree) functionDeclarationTypePacks(stmt *functionDeclarationStatement) []string {
	if stmt == nil {
		return nil
	}
	return stmt.typePacks
}
func (tree syntaxTree) functionDeclarationTypePackID(stmt *functionDeclarationStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.typePackID
}
func (tree syntaxTree) functionDeclarationParams(stmt *functionDeclarationStatement) []string {
	if stmt == nil {
		return nil
	}
	return stmt.params
}
func (tree syntaxTree) functionDeclarationParamID(stmt *functionDeclarationStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.paramID
}
func (tree syntaxTree) functionDeclarationSelfID(stmt *functionDeclarationStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.selfID
}
func (tree syntaxTree) functionDeclarationParamAnnotations(stmt *functionDeclarationStatement) []*typeExpression {
	if stmt == nil {
		return nil
	}
	return stmt.paramAnnotations
}
func (tree syntaxTree) functionDeclarationVariadic(stmt *functionDeclarationStatement) bool {
	return stmt != nil && stmt.variadic
}
func (tree syntaxTree) functionDeclarationVariadicAnnotation(stmt *functionDeclarationStatement) *typeExpression {
	if stmt == nil {
		return nil
	}
	return stmt.variadicAnnotation
}
func (tree syntaxTree) functionDeclarationReturnAnnotation(stmt *functionDeclarationStatement) *typeExpression {
	if stmt == nil {
		return nil
	}
	return stmt.returnAnnotation
}
func (tree syntaxTree) functionDeclarationStatements(stmt *functionDeclarationStatement) []statement {
	if stmt == nil {
		return nil
	}
	return stmt.statements
}
func (tree syntaxTree) functionDeclarationMethod(stmt *functionDeclarationStatement) bool {
	return stmt != nil && stmt.method
}

func (tree syntaxTree) assignmentTargets(stmt *assignStatement) []assignTarget {
	if stmt == nil {
		return nil
	}
	return stmt.targets
}
func (tree syntaxTree) assignmentValues(stmt *assignStatement) []expressionID {
	if stmt == nil {
		return nil
	}
	return stmt.values
}
func (tree syntaxTree) assignTargetID(target *assignTarget) syntaxID {
	if target == nil {
		return 0
	}
	return target.id
}
func (tree syntaxTree) assignTargetName(target *assignTarget) string {
	if target == nil {
		return ""
	}
	return target.name
}
func (tree syntaxTree) assignTargetSelectors(target *assignTarget) []selector {
	if target == nil {
		return nil
	}
	return target.selectors
}
func (tree syntaxTree) assignTargetRange(target *assignTarget) (int, int) {
	if target == nil {
		return 0, 0
	}
	return target.start, target.end
}
func (tree syntaxTree) selectorField(value *selector) string {
	if value == nil {
		return ""
	}
	return value.field
}
func (tree syntaxTree) selectorIndex(value *selector) expressionID {
	if value == nil {
		return 0
	}
	return value.index
}

func (tree syntaxTree) typeAliasID(stmt *typeAliasStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.id
}
func (tree syntaxTree) typeAliasExported(stmt *typeAliasStatement) bool {
	return stmt != nil && stmt.exported
}
func (tree syntaxTree) typeAliasName(stmt *typeAliasStatement) string {
	if stmt == nil {
		return ""
	}
	return stmt.name
}
func (tree syntaxTree) typeAliasNameID(stmt *typeAliasStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.nameID
}
func (tree syntaxTree) typeAliasTypeParams(stmt *typeAliasStatement) []string {
	if stmt == nil {
		return nil
	}
	return stmt.typeParams
}
func (tree syntaxTree) typeAliasTypeParamID(stmt *typeAliasStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.typeParamID
}
func (tree syntaxTree) typeAliasTypePacks(stmt *typeAliasStatement) []string {
	if stmt == nil {
		return nil
	}
	return stmt.typePacks
}
func (tree syntaxTree) typeAliasTypePackID(stmt *typeAliasStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.typePackID
}
func (tree syntaxTree) typeAliasValue(stmt *typeAliasStatement) *typeExpression {
	if stmt == nil {
		return nil
	}
	return stmt.value
}
func (tree syntaxTree) typeAliasRange(stmt *typeAliasStatement) (int, int) {
	if stmt == nil {
		return 0, 0
	}
	return stmt.start, stmt.end
}
func (tree syntaxTree) typeAliasNameRange(stmt *typeAliasStatement) (int, int) {
	if stmt == nil {
		return 0, 0
	}
	return stmt.nameStart, stmt.nameEnd
}
func (tree syntaxTree) ifCondition(stmt *ifStatement) expressionID {
	if stmt == nil {
		return 0
	}
	return stmt.condition
}
func (tree syntaxTree) ifThenStatements(stmt *ifStatement) []statement {
	if stmt == nil {
		return nil
	}
	return stmt.thenStatements
}
func (tree syntaxTree) ifElseStatements(stmt *ifStatement) []statement {
	if stmt == nil {
		return nil
	}
	return stmt.elseStatements
}
func (tree syntaxTree) whileCondition(stmt *whileStatement) expressionID {
	if stmt == nil {
		return 0
	}
	return stmt.condition
}
func (tree syntaxTree) whileStatements(stmt *whileStatement) []statement {
	if stmt == nil {
		return nil
	}
	return stmt.statements
}
func (tree syntaxTree) numericForName(stmt *forStatement) string {
	if stmt == nil {
		return ""
	}
	return stmt.name
}
func (tree syntaxTree) numericForNameID(stmt *forStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.nameID
}
func (tree syntaxTree) numericForStart(stmt *forStatement) expressionID {
	if stmt == nil {
		return 0
	}
	return stmt.start
}
func (tree syntaxTree) numericForLimit(stmt *forStatement) expressionID {
	if stmt == nil {
		return 0
	}
	return stmt.limit
}
func (tree syntaxTree) numericForStep(stmt *forStatement) expressionID {
	if stmt == nil || stmt.step == 0 {
		return 0
	}
	return stmt.step
}
func (tree syntaxTree) numericForStatements(stmt *forStatement) []statement {
	if stmt == nil {
		return nil
	}
	return stmt.statements
}
func (tree syntaxTree) genericForNames(stmt *genericForStatement) []string {
	if stmt == nil {
		return nil
	}
	return stmt.names
}
func (tree syntaxTree) genericForNameID(stmt *genericForStatement) syntaxID {
	if stmt == nil {
		return 0
	}
	return stmt.nameID
}
func (tree syntaxTree) genericForValues(stmt *genericForStatement) []expressionID {
	if stmt == nil {
		return nil
	}
	return stmt.values
}
func (tree syntaxTree) genericForStatements(stmt *genericForStatement) []statement {
	if stmt == nil {
		return nil
	}
	return stmt.statements
}
func (tree syntaxTree) repeatStatements(stmt *repeatStatement) []statement {
	if stmt == nil {
		return nil
	}
	return stmt.statements
}
func (tree syntaxTree) repeatCondition(stmt *repeatStatement) expressionID {
	if stmt == nil {
		return 0
	}
	return stmt.condition
}
func (tree syntaxTree) blockStatements(stmt *blockStatement) []statement {
	if stmt == nil {
		return nil
	}
	return stmt.statements
}
func (tree syntaxTree) returnValues(stmt *returnStatement) []expressionID {
	if stmt == nil {
		return nil
	}
	return stmt.values
}
func (tree syntaxTree) returnRange(stmt *returnStatement) (int, int) {
	if stmt == nil {
		return 0, 0
	}
	return stmt.start, stmt.end
}

func (tree syntaxTree) expressionTerms(id expressionID) ([]andExpressionID, bool) {
	node, ok := tree.arenaExpression(id)
	if !ok {
		return nil, false
	}
	return tree.arenaExpressionTerms(node.terms)
}
func (tree syntaxTree) expressionSyntaxID(id expressionID) syntaxID {
	node, ok := tree.arenaExpression(id)
	if !ok {
		return 0
	}
	return node.id
}
func (tree syntaxTree) expressionID(id expressionID) syntaxID { return tree.expressionSyntaxID(id) }
func (tree syntaxTree) andTerms(id andExpressionID) ([]comparisonExpressionID, bool) {
	node, ok := tree.arenaAnd(id)
	if !ok {
		return nil, false
	}
	return tree.arenaAndTerms(node.terms)
}
func (tree syntaxTree) comparisonLeft(id comparisonExpressionID) concatExpressionID {
	node, ok := tree.arenaComparison(id)
	if !ok {
		return 0
	}
	return node.left
}
func (tree syntaxTree) comparisonOperator(id comparisonExpressionID) comparisonOperator {
	node, ok := tree.arenaComparison(id)
	if !ok {
		return ""
	}
	switch node.op {
	case arenaComparisonEqual:
		return comparisonEqual
	case arenaComparisonNotEqual:
		return comparisonNotEqual
	case arenaComparisonLess:
		return comparisonLess
	case arenaComparisonLessEqual:
		return comparisonLessEqual
	case arenaComparisonGreater:
		return comparisonGreater
	case arenaComparisonGreaterEqual:
		return comparisonGreaterEqual
	default:
		return ""
	}
}
func (tree syntaxTree) comparisonRight(id comparisonExpressionID) concatExpressionID {
	node, ok := tree.arenaComparison(id)
	if !ok {
		return 0
	}
	return node.right
}
func (tree syntaxTree) concatFirst(id concatExpressionID) additiveExpressionID {
	node, ok := tree.arenaConcat(id)
	if !ok {
		return 0
	}
	return node.first
}
func (tree syntaxTree) concatRest(id concatExpressionID) ([]additiveExpressionID, bool) {
	node, ok := tree.arenaConcat(id)
	if !ok {
		return nil, false
	}
	return tree.arenaConcatRest(node.rest)
}
func (tree syntaxTree) additiveFirst(id additiveExpressionID) multiplicativeExpressionID {
	node, ok := tree.arenaAdditive(id)
	if !ok {
		return 0
	}
	return node.first
}
func (tree syntaxTree) additiveRest(id additiveExpressionID) ([]arenaAdditivePart, bool) {
	node, ok := tree.arenaAdditive(id)
	if !ok {
		return nil, false
	}
	return tree.arenaAdditiveRest(node.rest)
}
func (tree syntaxTree) additivePartOperator(part arenaAdditivePart) additiveOperator {
	switch part.op {
	case arenaAdditiveAdd:
		return additiveAdd
	case arenaAdditiveSubtract:
		return additiveSubtract
	default:
		return ""
	}
}
func (tree syntaxTree) additivePartValue(part arenaAdditivePart) multiplicativeExpressionID {
	return part.value
}
func (tree syntaxTree) multiplicativeFirst(id multiplicativeExpressionID) termID {
	node, ok := tree.arenaMultiplicative(id)
	if !ok {
		return 0
	}
	return node.first
}
func (tree syntaxTree) multiplicativeRest(id multiplicativeExpressionID) ([]arenaMultiplicativePart, bool) {
	node, ok := tree.arenaMultiplicative(id)
	if !ok {
		return nil, false
	}
	return tree.arenaMultiplicativeRest(node.rest)
}
func (tree syntaxTree) multiplicativePartOperator(part arenaMultiplicativePart) multiplicativeOperator {
	switch part.op {
	case arenaMultiplicativeMultiply:
		return multiplicativeMultiply
	case arenaMultiplicativeDivide:
		return multiplicativeDivide
	case arenaMultiplicativeModulo:
		return multiplicativeModulo
	case arenaMultiplicativeFloorDiv:
		return multiplicativeFloorDiv
	default:
		return ""
	}
}
func (tree syntaxTree) multiplicativePartValue(part arenaMultiplicativePart) termID {
	return part.value
}

type syntaxTermKind uint8

const (
	syntaxTermUnknown syntaxTermKind = iota
	syntaxTermNumber
	syntaxTermLiteral
	syntaxTermTable
	syntaxTermFunction
	syntaxTermIf
	syntaxTermCall
	syntaxTermVararg
	syntaxTermUnaryNot
	syntaxTermUnaryMinus
	syntaxTermUnaryLength
	syntaxTermPower
	syntaxTermGroup
	syntaxTermCast
	syntaxTermName
)

func (tree syntaxTree) termKind(id termID) syntaxTermKind {
	node, ok := tree.arenaTerm(id)
	if !ok {
		return syntaxTermUnknown
	}
	if node.kind == termKindName && node.castType != 0 {
		return syntaxTermCast
	}
	switch node.kind {
	case termKindNumber:
		return syntaxTermNumber
	case termKindNil, termKindBool, termKindString:
		return syntaxTermLiteral
	case termKindTable:
		return syntaxTermTable
	case termKindFunction:
		return syntaxTermFunction
	case termKindIf:
		return syntaxTermIf
	case termKindCall:
		return syntaxTermCall
	case termKindVararg:
		return syntaxTermVararg
	case termKindUnaryNot:
		return syntaxTermUnaryNot
	case termKindUnaryMinus:
		return syntaxTermUnaryMinus
	case termKindUnaryLength:
		return syntaxTermUnaryLength
	case termKindPower:
		return syntaxTermPower
	case termKindGroup:
		return syntaxTermGroup
	case termKindName:
		return syntaxTermName
	default:
		return syntaxTermUnknown
	}
}
func (tree syntaxTree) termSyntaxID(id termID) syntaxID {
	node, ok := tree.arenaTerm(id)
	if !ok {
		return 0
	}
	return node.id
}
func (tree syntaxTree) termID(id termID) syntaxID { return tree.termSyntaxID(id) }
func (tree syntaxTree) termRange(id termID) (int, int) {
	node, ok := tree.arenaTerm(id)
	if !ok {
		return 0, 0
	}
	return node.start, node.end
}
func (tree syntaxTree) termVararg(id termID) bool { return tree.termKind(id) == syntaxTermVararg }
func (tree syntaxTree) termSelectors(id termID) ([]arenaSelector, bool) {
	node, ok := tree.arenaTerm(id)
	if !ok {
		return nil, false
	}
	return tree.arena.selectorIDs(node.selectors)
}
func (tree syntaxTree) termSelectorField(value arenaSelector) string {
	if value.field == 0 || tree.arena == nil || int(value.field) > len(tree.arena.strings) {
		return ""
	}
	return tree.arena.strings[value.field-1]
}
func (tree syntaxTree) termSelectorIndex(value arenaSelector) expressionID { return value.index }
func (tree syntaxTree) termNumber(id termID) (float64, bool) {
	node, ok := tree.arenaTerm(id)
	if !ok || node.kind != termKindNumber {
		return 0, false
	}
	return math.Float64frombits(node.payload), true
}
func (tree syntaxTree) termLiteral(id termID) (Value, bool) {
	node, ok := tree.arenaTerm(id)
	if !ok {
		return Value{}, false
	}
	switch node.kind {
	case termKindNil:
		return NilValue(), true
	case termKindBool:
		return BoolValue(node.payload != 0), true
	case termKindString:
		if node.payload == 0 || int(node.payload) > len(tree.arena.strings) {
			return Value{}, false
		}
		if literal, ok := tree.arena.stringLiterals[stringID(node.payload)]; ok {
			return literal, true
		}
		return StringValue(tree.arena.strings[node.payload-1]), true
	default:
		return Value{}, false
	}
}
func (tree syntaxTree) termChild(id termID) (termID, bool) {
	node, ok := tree.arenaTerm(id)
	if !ok {
		return 0, false
	}
	if node.kind != termKindUnaryNot && node.kind != termKindUnaryMinus && node.kind != termKindUnaryLength {
		return 0, false
	}
	child := termID(node.payload)
	_, ok = tree.arenaTerm(child)
	return child, ok
}
func (tree syntaxTree) termName(id termID) string {
	node, ok := tree.arenaTerm(id)
	if !ok || node.kind != termKindName || node.payload == 0 || int(node.payload) > len(tree.arena.strings) {
		return ""
	}
	return tree.arena.strings[node.payload-1]
}
func (tree syntaxTree) stringValue(id stringID) (string, bool) {
	if tree.arena == nil {
		return "", false
	}
	return tree.arena.stringValue(id)
}
func (tree syntaxTree) termPayload(id termID) uint64 {
	node, ok := tree.arenaTerm(id)
	if !ok {
		return 0
	}
	return node.payload
}
func (tree syntaxTree) termTable(id termID) (arenaTableID, bool) {
	node, ok := tree.arenaTerm(id)
	if !ok || node.kind != termKindTable {
		return 0, false
	}
	value := arenaTableID(node.payload)
	_, ok = tree.arena.table(value)
	return value, ok
}
func (tree syntaxTree) termFunction(id termID) (arenaFunctionID, bool) {
	node, ok := tree.arenaTerm(id)
	if !ok || node.kind != termKindFunction {
		return 0, false
	}
	value := arenaFunctionID(node.payload)
	_, ok = tree.arena.function(value)
	return value, ok
}
func (tree syntaxTree) termIf(id termID) (arenaIfExpressionID, bool) {
	node, ok := tree.arenaTerm(id)
	if !ok || node.kind != termKindIf {
		return 0, false
	}
	value := arenaIfExpressionID(node.payload)
	_, ok = tree.arena.ifExpression(value)
	return value, ok
}
func (tree syntaxTree) termCall(id termID) (arenaCallID, bool) {
	node, ok := tree.arenaTerm(id)
	if !ok || node.kind != termKindCall {
		return 0, false
	}
	value := arenaCallID(node.payload)
	_, ok = tree.arena.call(value)
	return value, ok
}
func (tree syntaxTree) termPower(id termID) (arenaPowerID, bool) {
	node, ok := tree.arenaTerm(id)
	if !ok || node.kind != termKindPower {
		return 0, false
	}
	value := arenaPowerID(node.payload)
	_, ok = tree.arena.power(value)
	return value, ok
}
func (tree syntaxTree) termGroup(id termID) (expressionID, bool) {
	node, ok := tree.arenaTerm(id)
	if !ok || node.kind != termKindGroup {
		return 0, false
	}
	value := expressionID(node.payload)
	_, ok = tree.arenaExpression(value)
	return value, ok
}
func (tree syntaxTree) termCast(id termID) (*typeExpression, bool) {
	node, ok := tree.arenaTerm(id)
	if !ok || node.castType == 0 {
		return nil, false
	}
	return tree.arena.cast(uint32(node.castType))
}
func (tree syntaxTree) tableFields(id arenaTableID) ([]arenaTableField, bool) {
	node, ok := tree.arena.table(id)
	if !ok {
		return nil, false
	}
	return tree.arena.tableFieldsIDs(node.fields)
}
func (tree syntaxTree) tableFieldName(field arenaTableField) string {
	if field.name == 0 || int(field.name) > len(tree.arena.strings) {
		return ""
	}
	return tree.arena.strings[field.name-1]
}
func (tree syntaxTree) tableFieldKey(field arenaTableField) expressionID   { return field.key }
func (tree syntaxTree) tableFieldValue(field arenaTableField) expressionID { return field.value }
func (tree syntaxTree) tableFieldArrayIndex(field arenaTableField) int     { return field.arrayIndex }
func (tree syntaxTree) callTarget(call arenaCallID) termID {
	node, ok := tree.arena.call(call)
	if !ok {
		return 0
	}
	return node.target
}
func (tree syntaxTree) callReceiver(call arenaCallID) termID {
	node, ok := tree.arena.call(call)
	if !ok {
		return 0
	}
	return node.receiver
}
func (tree syntaxTree) callTypeArgs(call arenaCallID) []*typeExpression {
	node, ok := tree.arena.call(call)
	if !ok {
		return nil
	}
	return node.typeArgs
}
func (tree syntaxTree) callArgs(call arenaCallID) ([]expressionID, bool) {
	node, ok := tree.arena.call(call)
	if !ok {
		return nil, false
	}
	return tree.arena.callArgIDs(node.args)
}
func (tree syntaxTree) powerBase(power arenaPowerID) termID {
	node, ok := tree.arena.power(power)
	if !ok {
		return 0
	}
	return node.base
}
func (tree syntaxTree) powerExponent(power arenaPowerID) termID {
	node, ok := tree.arena.power(power)
	if !ok {
		return 0
	}
	return node.exponent
}
func (tree syntaxTree) ifExpressionCondition(value arenaIfExpressionID) expressionID {
	node, ok := tree.arena.ifExpression(value)
	if !ok {
		return 0
	}
	return node.condition
}
func (tree syntaxTree) ifExpressionThen(value arenaIfExpressionID) expressionID {
	node, ok := tree.arena.ifExpression(value)
	if !ok {
		return 0
	}
	return node.thenValue
}
func (tree syntaxTree) ifExpressionElse(value arenaIfExpressionID) expressionID {
	node, ok := tree.arena.ifExpression(value)
	if !ok {
		return 0
	}
	return node.elseValue
}
func (tree syntaxTree) functionExpressionID(value arenaFunctionID) syntaxID {
	node, ok := tree.arena.function(value)
	if !ok {
		return 0
	}
	return node.id
}
func (tree syntaxTree) functionExpressionFunctionID(value arenaFunctionID) int {
	node, ok := tree.arena.function(value)
	if !ok {
		return 0
	}
	return node.functionID
}
func (tree syntaxTree) functionExpressionTypeParams(value arenaFunctionID) []string {
	node, ok := tree.arena.function(value)
	if !ok {
		return nil
	}
	ids, ok := tree.arena.statements.spanStrings(node.typeParams)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := tree.stringValue(id); ok {
			values = append(values, name)
		}
	}
	return values
}
func (tree syntaxTree) functionExpressionTypeParamID(value arenaFunctionID) syntaxID {
	node, ok := tree.arena.function(value)
	if !ok {
		return 0
	}
	return node.typeParamID
}
func (tree syntaxTree) functionExpressionTypePacks(value arenaFunctionID) []string {
	node, ok := tree.arena.function(value)
	if !ok {
		return nil
	}
	ids, ok := tree.arena.statements.spanStrings(node.typePacks)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := tree.stringValue(id); ok {
			values = append(values, name)
		}
	}
	return values
}
func (tree syntaxTree) functionExpressionTypePackID(value arenaFunctionID) syntaxID {
	node, ok := tree.arena.function(value)
	if !ok {
		return 0
	}
	return node.typePackID
}
func (tree syntaxTree) functionExpressionParams(value arenaFunctionID) []string {
	node, ok := tree.arena.function(value)
	if !ok {
		return nil
	}
	ids, ok := tree.arena.statements.spanStrings(node.params)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := tree.stringValue(id); ok {
			values = append(values, name)
		}
	}
	return values
}
func (tree syntaxTree) functionExpressionParamID(value arenaFunctionID) syntaxID {
	node, ok := tree.arena.function(value)
	if !ok {
		return 0
	}
	return node.paramID
}
func (tree syntaxTree) functionExpressionParamAnnotations(value arenaFunctionID) []*typeExpression {
	node, ok := tree.arena.function(value)
	if !ok {
		return nil
	}
	ids, ok := tree.arena.statements.spanTypes(node.paramAnnotations)
	if !ok {
		return nil
	}
	values := make([]*typeExpression, 0, len(ids))
	for _, id := range ids {
		value, _ := tree.arena.typeNode(id)
		values = append(values, value)
	}
	return values
}
func (tree syntaxTree) functionExpressionVariadic(value arenaFunctionID) bool {
	node, ok := tree.arena.function(value)
	return ok && node.variadic
}
func (tree syntaxTree) functionExpressionVariadicAnnotation(value arenaFunctionID) *typeExpression {
	node, ok := tree.arena.function(value)
	if !ok {
		return nil
	}
	annotation, _ := tree.arena.typeNode(node.variadicAnnotation)
	return annotation
}
func (tree syntaxTree) functionExpressionReturnAnnotation(value arenaFunctionID) *typeExpression {
	node, ok := tree.arena.function(value)
	if !ok {
		return nil
	}
	annotation, _ := tree.arena.typeNode(node.returnAnnotation)
	return annotation
}
func (tree syntaxTree) functionExpressionStatements(value arenaFunctionID) []statement {
	node, ok := tree.arena.function(value)
	if !ok {
		return nil
	}
	ids, ok := tree.arena.statements.spanStatements(node.statements)
	if !ok {
		return nil
	}
	values := make([]statement, 0, len(ids))
	for _, id := range ids {
		if int(id) <= 0 || int(id) > len(tree.arena.statements.statements) {
			continue
		}
		// The legacy parser body remains available through the root only; typed
		// consumers should use statementIDs/statementNode instead.
	}
	return values
}

func (tree syntaxTree) functionExpressionTypeParamIDs(value arenaFunctionID) (nodeSpan, bool) {
	node, ok := tree.arena.function(value)
	if !ok {
		return nodeSpan{}, false
	}
	return node.typeParams, true
}
func (tree syntaxTree) functionExpressionTypePackIDs(value arenaFunctionID) (nodeSpan, bool) {
	node, ok := tree.arena.function(value)
	if !ok {
		return nodeSpan{}, false
	}
	return node.typePacks, true
}
func (tree syntaxTree) functionExpressionParamIDs(value arenaFunctionID) (nodeSpan, bool) {
	node, ok := tree.arena.function(value)
	if !ok {
		return nodeSpan{}, false
	}
	return node.params, true
}
func (tree syntaxTree) functionExpressionParamAnnotationIDs(value arenaFunctionID) (nodeSpan, bool) {
	node, ok := tree.arena.function(value)
	if !ok {
		return nodeSpan{}, false
	}
	return node.paramAnnotations, true
}
func (tree syntaxTree) functionExpressionStatementIDs(value arenaFunctionID) (nodeSpan, bool) {
	node, ok := tree.arena.function(value)
	if !ok {
		return nodeSpan{}, false
	}
	return node.statements, true
}

func (tree syntaxTree) typeArgs(value *typeExpression) []*typeExpression {
	if value == nil {
		return nil
	}
	return value.typeArgs
}
func (tree syntaxTree) typeID(value *typeExpression) syntaxID {
	if value == nil {
		return 0
	}
	return value.id
}
func (tree syntaxTree) typeKind(value *typeExpression) typeKind {
	if value == nil {
		return ""
	}
	return value.kind
}
func (tree syntaxTree) typeName(value *typeExpression) []string {
	if value == nil {
		return nil
	}
	return value.name
}
func (tree syntaxTree) typeTypeParams(value *typeExpression) []string {
	if value == nil {
		return nil
	}
	return value.typeParams
}
func (tree syntaxTree) typeParamID(value *typeExpression) syntaxID {
	if value == nil {
		return 0
	}
	return value.typeParamID
}
func (tree syntaxTree) typePacks(value *typeExpression) []string {
	if value == nil {
		return nil
	}
	return value.typePacks
}
func (tree syntaxTree) typePackID(value *typeExpression) syntaxID {
	if value == nil {
		return 0
	}
	return value.typePackID
}
func (tree syntaxTree) typeExpression(value *typeExpression) expressionID {
	if value == nil {
		return 0
	}
	return value.expr
}
func (tree syntaxTree) typeLiteral(value *typeExpression) *Value {
	if value == nil {
		return nil
	}
	return value.literal
}
func (tree syntaxTree) typeRange(value *typeExpression) (int, int) {
	if value == nil {
		return 0, 0
	}
	return value.start, value.end
}
func (tree syntaxTree) typeChildren(value *typeExpression) []*typeExpression {
	if value == nil {
		return nil
	}
	return value.types
}
func (tree syntaxTree) typeInner(value *typeExpression) *typeExpression {
	if value == nil {
		return nil
	}
	return value.inner
}
func (tree syntaxTree) typeFields(value *typeExpression) []typeField {
	if value == nil {
		return nil
	}
	return value.fields
}
func (tree syntaxTree) typeParams(value *typeExpression) []typeFunctionParam {
	if value == nil {
		return nil
	}
	return value.params
}
func (tree syntaxTree) typeReturn(value *typeExpression) *typeExpression {
	if value == nil {
		return nil
	}
	return value.returnType
}
func (tree syntaxTree) typeFieldAccess(value *typeField) string {
	if value == nil {
		return ""
	}
	return value.access
}
func (tree syntaxTree) typeFieldName(value *typeField) string {
	if value == nil {
		return ""
	}
	return value.name
}
func (tree syntaxTree) typeFieldKey(value *typeField) *typeExpression {
	if value == nil {
		return nil
	}
	return value.key
}
func (tree syntaxTree) typeFieldValue(value *typeField) *typeExpression {
	if value == nil {
		return nil
	}
	return value.value
}
func (tree syntaxTree) typeParamName(value *typeFunctionParam) string {
	if value == nil {
		return ""
	}
	return value.name
}
func (tree syntaxTree) typeParamValue(value *typeFunctionParam) *typeExpression {
	if value == nil {
		return nil
	}
	return value.value
}
func (tree syntaxTree) typeParamVariadic(value *typeFunctionParam) bool {
	return value != nil && value.variadic
}
