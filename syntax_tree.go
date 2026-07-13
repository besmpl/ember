package ember

// syntaxTree is the storage seam for the parser tree. It deliberately returns
// the existing concrete slices and pointers: later arena-backed storage can
// change these implementations without changing compiler consumers.
type syntaxTree struct {
	root program
}

func newSyntaxTree(root program) syntaxTree { return syntaxTree{root: root} }

func (tree syntaxTree) statements() []statement { return tree.root.statements }
func (tree syntaxTree) mode() sourceMode        { return tree.root.mode }
func (tree syntaxTree) nodeCount() int          { return tree.root.nodeCount }
func (tree syntaxTree) id() syntaxID            { return tree.root.id }

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
	return stmt.id
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
	switch {
	case stmt.local != nil:
		return syntaxStatementLocal
	case stmt.localFunc != nil:
		return syntaxStatementLocalFunction
	case stmt.funcDecl != nil:
		return syntaxStatementFunctionDeclaration
	case stmt.assign != nil:
		return syntaxStatementAssign
	case stmt.call != nil:
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
func (tree syntaxTree) call(stmt *statement) *term {
	if stmt == nil {
		return nil
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
func (tree syntaxTree) localValues(stmt *localStatement) []expression {
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
func (tree syntaxTree) assignmentValues(stmt *assignStatement) []expression {
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
func (tree syntaxTree) selectorIndex(value *selector) *expression {
	if value == nil {
		return nil
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
func (tree syntaxTree) ifCondition(stmt *ifStatement) *expression {
	if stmt == nil {
		return nil
	}
	return &stmt.condition
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
func (tree syntaxTree) whileCondition(stmt *whileStatement) *expression {
	if stmt == nil {
		return nil
	}
	return &stmt.condition
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
func (tree syntaxTree) numericForStart(stmt *forStatement) *expression {
	if stmt == nil {
		return nil
	}
	return &stmt.start
}
func (tree syntaxTree) numericForLimit(stmt *forStatement) *expression {
	if stmt == nil {
		return nil
	}
	return &stmt.limit
}
func (tree syntaxTree) numericForStep(stmt *forStatement) *expression {
	if stmt == nil || stmt.step == nil {
		return nil
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
func (tree syntaxTree) genericForValues(stmt *genericForStatement) []expression {
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
func (tree syntaxTree) repeatCondition(stmt *repeatStatement) *expression {
	if stmt == nil {
		return nil
	}
	return &stmt.condition
}
func (tree syntaxTree) blockStatements(stmt *blockStatement) []statement {
	if stmt == nil {
		return nil
	}
	return stmt.statements
}
func (tree syntaxTree) returnValues(stmt *returnStatement) []expression {
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

func (tree syntaxTree) expressionTerms(expr *expression) []andExpression {
	if expr == nil {
		return nil
	}
	return expr.terms
}
func (tree syntaxTree) expressionID(expr *expression) syntaxID {
	if expr == nil {
		return 0
	}
	return expr.id
}
func (tree syntaxTree) andTerms(expr *andExpression) []comparisonExpression {
	if expr == nil {
		return nil
	}
	return expr.terms
}
func (tree syntaxTree) comparisonLeft(expr *comparisonExpression) concatExpression {
	if expr == nil {
		return concatExpression{}
	}
	return expr.left
}
func (tree syntaxTree) comparisonLeftRef(expr *comparisonExpression) *concatExpression {
	if expr == nil {
		return nil
	}
	return &expr.left
}
func (tree syntaxTree) comparisonOperator(expr *comparisonExpression) comparisonOperator {
	if expr == nil {
		return ""
	}
	return expr.op
}
func (tree syntaxTree) comparisonRight(expr *comparisonExpression) *concatExpression {
	if expr == nil {
		return nil
	}
	return expr.right
}
func (tree syntaxTree) concatFirst(expr *concatExpression) additiveExpression {
	if expr == nil {
		return additiveExpression{}
	}
	return expr.first
}
func (tree syntaxTree) concatFirstRef(expr *concatExpression) *additiveExpression {
	if expr == nil {
		return nil
	}
	return &expr.first
}
func (tree syntaxTree) concatRest(expr *concatExpression) []additiveExpression {
	if expr == nil {
		return nil
	}
	return expr.rest
}
func (tree syntaxTree) additiveFirst(expr *additiveExpression) multiplicativeExpression {
	if expr == nil {
		return multiplicativeExpression{}
	}
	return expr.first
}
func (tree syntaxTree) additiveFirstRef(expr *additiveExpression) *multiplicativeExpression {
	if expr == nil {
		return nil
	}
	return &expr.first
}
func (tree syntaxTree) additivePartOperator(part *additivePart) additiveOperator {
	if part == nil {
		return ""
	}
	return part.op
}
func (tree syntaxTree) additivePartValue(part *additivePart) *multiplicativeExpression {
	if part == nil {
		return nil
	}
	return &part.value
}
func (tree syntaxTree) additiveRest(expr *additiveExpression) []additivePart {
	if expr == nil {
		return nil
	}
	return expr.rest
}
func (tree syntaxTree) multiplicativeFirst(expr *multiplicativeExpression) term {
	if expr == nil {
		return term{}
	}
	return expr.first
}
func (tree syntaxTree) multiplicativeFirstRef(expr *multiplicativeExpression) *term {
	if expr == nil {
		return nil
	}
	return &expr.first
}
func (tree syntaxTree) multiplicativePartOperator(part *multiplicativePart) multiplicativeOperator {
	if part == nil {
		return ""
	}
	return part.op
}
func (tree syntaxTree) multiplicativePartValue(part *multiplicativePart) *term {
	if part == nil {
		return nil
	}
	return &part.value
}
func (tree syntaxTree) multiplicativeRest(expr *multiplicativeExpression) []multiplicativePart {
	if expr == nil {
		return nil
	}
	return expr.rest
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

func (tree syntaxTree) termKind(value *term) syntaxTermKind {
	if value == nil {
		return syntaxTermUnknown
	}
	switch {
	case value.number != nil:
		return syntaxTermNumber
	case value.lit != nil:
		return syntaxTermLiteral
	case value.table != nil:
		return syntaxTermTable
	case value.function != nil:
		return syntaxTermFunction
	case value.ifExpr != nil:
		return syntaxTermIf
	case value.call != nil:
		return syntaxTermCall
	case value.vararg:
		return syntaxTermVararg
	case value.unaryNot != nil:
		return syntaxTermUnaryNot
	case value.unaryMinus != nil:
		return syntaxTermUnaryMinus
	case value.unaryLen != nil:
		return syntaxTermUnaryLength
	case value.power != nil:
		return syntaxTermPower
	case value.group != nil:
		return syntaxTermGroup
	case value.cast != nil:
		return syntaxTermCast
	case value.name != "":
		return syntaxTermName
	default:
		return syntaxTermUnknown
	}
}
func (tree syntaxTree) termID(value *term) syntaxID {
	if value == nil {
		return 0
	}
	return value.id
}
func (tree syntaxTree) termRange(value *term) (int, int) {
	if value == nil {
		return 0, 0
	}
	return value.start, value.end
}
func (tree syntaxTree) termVararg(value *term) bool {
	return value != nil && value.vararg
}
func (tree syntaxTree) termSelectors(value *term) []selector {
	if value == nil {
		return nil
	}
	return value.selectors
}

func (tree syntaxTree) termNumber(value *term) *float64 {
	if value == nil {
		return nil
	}
	return value.number
}
func (tree syntaxTree) termLiteral(value *term) *Value {
	if value == nil {
		return nil
	}
	return value.lit
}
func (tree syntaxTree) termTable(value *term) *tableExpression {
	if value == nil {
		return nil
	}
	return value.table
}
func (tree syntaxTree) termFunction(value *term) *functionExpression {
	if value == nil {
		return nil
	}
	return value.function
}
func (tree syntaxTree) termIf(value *term) *ifExpression {
	if value == nil {
		return nil
	}
	return value.ifExpr
}
func (tree syntaxTree) termCall(value *term) *callExpression {
	if value == nil {
		return nil
	}
	return value.call
}
func (tree syntaxTree) termUnaryNot(value *term) *term {
	if value == nil {
		return nil
	}
	return value.unaryNot
}
func (tree syntaxTree) termUnaryMinus(value *term) *term {
	if value == nil {
		return nil
	}
	return value.unaryMinus
}
func (tree syntaxTree) termUnaryLength(value *term) *term {
	if value == nil {
		return nil
	}
	return value.unaryLen
}
func (tree syntaxTree) termPower(value *term) *powerExpression {
	if value == nil {
		return nil
	}
	return value.power
}
func (tree syntaxTree) termGroup(value *term) *expression {
	if value == nil {
		return nil
	}
	return value.group
}
func (tree syntaxTree) termCast(value *term) *typeExpression {
	if value == nil {
		return nil
	}
	return value.cast
}
func (tree syntaxTree) termName(value *term) string {
	if value == nil {
		return ""
	}
	return value.name
}
func (tree syntaxTree) tableFields(value *tableExpression) []tableField {
	if value == nil {
		return nil
	}
	return value.fields
}
func (tree syntaxTree) tableFieldName(value *tableField) string {
	if value == nil {
		return ""
	}
	return value.name
}
func (tree syntaxTree) tableFieldArrayIndex(value *tableField) int {
	if value == nil {
		return 0
	}
	return value.arrayIndex
}
func (tree syntaxTree) tableFieldKey(value *tableField) *expression {
	if value == nil {
		return nil
	}
	return value.key
}
func (tree syntaxTree) tableFieldValue(value *tableField) *expression {
	if value == nil {
		return nil
	}
	return &value.value
}
func (tree syntaxTree) callTarget(value *callExpression) *term {
	if value == nil {
		return nil
	}
	return &value.target
}
func (tree syntaxTree) callReceiver(value *callExpression) *term {
	if value == nil {
		return nil
	}
	return value.receiver
}
func (tree syntaxTree) callTypeArgs(value *callExpression) []*typeExpression {
	if value == nil {
		return nil
	}
	return value.typeArgs
}
func (tree syntaxTree) callArgs(value *callExpression) []expression {
	if value == nil {
		return nil
	}
	return value.args
}
func (tree syntaxTree) powerBase(value *powerExpression) *term {
	if value == nil {
		return nil
	}
	return &value.base
}
func (tree syntaxTree) powerExponent(value *powerExpression) *term {
	if value == nil {
		return nil
	}
	return &value.exponent
}
func (tree syntaxTree) ifExpressionCondition(value *ifExpression) *expression {
	if value == nil {
		return nil
	}
	return &value.condition
}
func (tree syntaxTree) ifExpressionThen(value *ifExpression) *expression {
	if value == nil {
		return nil
	}
	return &value.thenValue
}
func (tree syntaxTree) ifExpressionElse(value *ifExpression) *expression {
	if value == nil {
		return nil
	}
	return &value.elseValue
}
func (tree syntaxTree) functionExpressionID(value *functionExpression) syntaxID {
	if value == nil {
		return 0
	}
	return value.id
}
func (tree syntaxTree) functionExpressionFunctionID(value *functionExpression) int {
	if value == nil {
		return 0
	}
	return value.functionID
}
func (tree syntaxTree) functionExpressionTypeParams(value *functionExpression) []string {
	if value == nil {
		return nil
	}
	return value.typeParams
}
func (tree syntaxTree) functionExpressionTypeParamID(value *functionExpression) syntaxID {
	if value == nil {
		return 0
	}
	return value.typeParamID
}
func (tree syntaxTree) functionExpressionTypePacks(value *functionExpression) []string {
	if value == nil {
		return nil
	}
	return value.typePacks
}
func (tree syntaxTree) functionExpressionTypePackID(value *functionExpression) syntaxID {
	if value == nil {
		return 0
	}
	return value.typePackID
}
func (tree syntaxTree) functionExpressionParams(value *functionExpression) []string {
	if value == nil {
		return nil
	}
	return value.params
}
func (tree syntaxTree) functionExpressionParamID(value *functionExpression) syntaxID {
	if value == nil {
		return 0
	}
	return value.paramID
}
func (tree syntaxTree) functionExpressionParamAnnotations(value *functionExpression) []*typeExpression {
	if value == nil {
		return nil
	}
	return value.paramAnnotations
}
func (tree syntaxTree) functionExpressionVariadic(value *functionExpression) bool {
	return value != nil && value.variadic
}
func (tree syntaxTree) functionExpressionVariadicAnnotation(value *functionExpression) *typeExpression {
	if value == nil {
		return nil
	}
	return value.variadicAnnotation
}
func (tree syntaxTree) functionExpressionReturnAnnotation(value *functionExpression) *typeExpression {
	if value == nil {
		return nil
	}
	return value.returnAnnotation
}
func (tree syntaxTree) functionExpressionStatements(value *functionExpression) []statement {
	if value == nil {
		return nil
	}
	return value.statements
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
func (tree syntaxTree) typeExpression(value *typeExpression) *expression {
	if value == nil {
		return nil
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
