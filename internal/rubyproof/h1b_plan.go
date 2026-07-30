package rubyproof

import (
	"crypto/sha256"
	"encoding/binary"
)

const h1bIdentityDomain = "github.com/besmpl/ember/internal/rubyproof:h1b:v1\x00"

type h1bSafePointID uint8
type h1bBindingID uint8
type h1bCallSiteID uint8

const (
	h1bHelperCollection h1bSafePointID = iota + 1
	h1bReceiverCollection
	h1bPeerCollection
)

// H1bProgram is the exact immutable checked H1b product. Its representation is
// private so no syntax, Ruby value, or owner-bound reference can escape.
type H1bProgram struct{ checked *checkedH1bPlan }

// IsZero reports whether p is the invalid zero H1bProgram.
func (p H1bProgram) IsZero() bool { return p.checked == nil }

// Identity returns the exact-source and H1b-schema-bound product identity.
func (p H1bProgram) Identity() [sha256.Size]byte {
	if p.checked == nil {
		return [sha256.Size]byte{}
	}
	return p.checked.identity
}

// CompileH1b parses and checks only the exact bounded H1b product. It shares
// the Ruby lexer and parser with Compile, but it deliberately does not enter
// the static proof's checker or lookup product.
func CompileH1b(source string) (H1bProgram, []Diagnostic) {
	tokens, diagnostics := lex(source)
	if len(diagnostics) != 0 {
		return H1bProgram{}, diagnostics
	}
	module, diagnostics := parse(tokens)
	if len(diagnostics) != 0 {
		return H1bProgram{}, diagnostics
	}
	plan, diagnostics := checkH1bRuby(source, module)
	if len(diagnostics) != 0 {
		return H1bProgram{}, diagnostics
	}
	return H1bProgram{checked: plan}, nil
}

type checkedH1bPlan struct {
	identity        [sha256.Size]byte
	sourceHash      [sha256.Size]byte
	sourceBytes     uint16
	sourceLineFeeds uint8

	class       h1bClassPlan
	bindings    h1bBindingPlan
	call        h1bCallPlan
	capture     h1bCapturePlan
	roots       h1bRootPlan
	capacity    h1bCapacityPlan
	collections [3]h1bCollectionPlan
	result      [7]int64
	spans       h1bSpanPlan
}

type h1bClassPlan struct {
	dispatch      dispatchClassID
	shape         shapeID
	ordinaryValue int64
}

type h1bBindingPlan struct {
	readReceiver                h1bBindingID
	installReceiver             h1bBindingID
	captured                    h1bBindingID
	receiver                    h1bBindingID
	peer                        h1bBindingID
	before                      h1bBindingID
	warm                        h1bBindingID
	peerBefore                  h1bBindingID
	first                       h1bBindingID
	second                      h1bBindingID
	peerAfter                   h1bBindingID
	peerAfterReceiverCollection h1bBindingID
	result                      h1bBindingID
}

type h1bCallPlan struct {
	site            h1bCallSiteID
	selector        selectorID
	singletonTarget h1SidecarTargetID
	visibility      lookupVisibility
}

type h1bCapturePlan struct {
	binding         h1bBindingID
	cell            uint8
	initial         int64
	bodyIncrement   int64
	helperIncrement int64
}

// h1bRootPlan seals the exact reference liveness needed by both independent
// executors. The caller owns two reserved top slots. install_h1b adds one
// object parameter root and needs no environment root: after publish-last the
// receiver sidecar is the strong edge across the explicit GC.start safe point.
type h1bRootPlan struct {
	topSlots                uint8
	installObjectRoots      uint8
	installEnvironmentRoots uint8
	maximumSlots            uint8
}

type h1bCapacityPlan struct {
	objects            uint8
	environments       uint8
	roots              uint8
	markWork           uint8
	objectFields       uint8
	environmentCells   uint8
	referenceIndexBits uint8
	maximumGeneration  uint32
}

type h1bCollectionPlan struct {
	id                    h1bSafePointID
	markedObjects         uint8
	markedEnvironments    uint8
	reclaimedObjects      uint8
	reclaimedEnvironments uint8
	markWork              uint8
}

// h1bSpanPlan retains only copied source coordinates. In particular, the
// checked product retains no syntax node, token slice, or lexical environment.
type h1bSpanPlan struct {
	classDeclaration    Span
	ordinaryMethod      Span
	readMethod          Span
	readCall            Span
	installMethod       Span
	captureDeclaration  Span
	singletonDefinition Span
	singletonBlock      Span
	helperMutation      Span
	helperReturn        Span
	allocations         [2]Span
	reads               [7]Span
	drops               [2]Span
	collections         [3]Span
	result              Span
}

func checkH1bRuby(source string, module *syntaxModule) (*checkedH1bPlan, []Diagnostic) {
	if source != H1bSource {
		return nil, []Diagnostic{{
			Span:    h1bMismatchSpan(source),
			Message: "ruby: H1b source does not match the exact admitted program",
		}}
	}
	spans, ok := h1bCheckedSpans(module)
	if !ok {
		return nil, []Diagnostic{{
			Span:    h1bWholeSourceSpan(source),
			Message: "ruby: H1b parsed shape does not match the exact admitted program",
		}}
	}

	plan := &checkedH1bPlan{
		identity:        h1bProgramIdentity(source),
		sourceHash:      sha256.Sum256([]byte(source)),
		sourceBytes:     uint16(len(source)),
		sourceLineFeeds: 35,
		class: h1bClassPlan{
			dispatch:      dispatchClassID(1),
			shape:         shapeID(1),
			ordinaryValue: 7,
		},
		bindings: h1bBindingPlan{
			readReceiver:                h1bBindingID(1),
			installReceiver:             h1bBindingID(2),
			captured:                    h1bBindingID(3),
			receiver:                    h1bBindingID(4),
			peer:                        h1bBindingID(5),
			before:                      h1bBindingID(6),
			warm:                        h1bBindingID(7),
			peerBefore:                  h1bBindingID(8),
			first:                       h1bBindingID(9),
			second:                      h1bBindingID(10),
			peerAfter:                   h1bBindingID(11),
			peerAfterReceiverCollection: h1bBindingID(12),
			result:                      h1bBindingID(13),
		},
		call: h1bCallPlan{
			site:            h1bCallSiteID(1),
			selector:        h1SidecarSelector,
			singletonTarget: h1SidecarTarget,
			visibility:      lookupVisibilityPublic,
		},
		capture: h1bCapturePlan{
			binding:         h1bBindingID(3),
			cell:            0,
			initial:         40,
			bodyIncrement:   1,
			helperIncrement: 2,
		},
		roots: h1bRootPlan{
			topSlots:                2,
			installObjectRoots:      1,
			installEnvironmentRoots: 0,
			maximumSlots:            3,
		},
		capacity: h1bCapacityPlan{
			objects:            h1SidecarObjectCapacity,
			environments:       h1SidecarEnvironmentCapacity,
			roots:              h1SidecarRootCapacity,
			markWork:           h1SidecarMarkCapacity,
			objectFields:       h1SidecarObjectFields,
			environmentCells:   h1SidecarEnvironmentCells,
			referenceIndexBits: h1SidecarReferenceIndexBits,
			maximumGeneration:  h1SidecarMaximumGeneration,
		},
		collections: [3]h1bCollectionPlan{
			{id: h1bHelperCollection, markedObjects: 2, markedEnvironments: 1, markWork: 3},
			{id: h1bReceiverCollection, markedObjects: 1, reclaimedObjects: 1, reclaimedEnvironments: 1, markWork: 1},
			{id: h1bPeerCollection, reclaimedObjects: 1},
		},
		result: [7]int64{7, 7, 7, 43, 44, 7, 7},
		spans:  spans,
	}
	return plan, nil
}

// h1bCheckedSpans is intentionally a closed shape extraction rather than a
// second general checker. Exact source identity admits the product; these
// checks make the source-to-plan dependency fail closed if parser behavior
// ever changes.
func h1bCheckedSpans(module *syntaxModule) (h1bSpanPlan, bool) {
	if module == nil || len(module.statements) != 18 {
		return h1bSpanPlan{}, false
	}
	top := module.statements
	class := top[0]
	read := top[1]
	install := top[2]
	if class == nil || read == nil || install == nil ||
		class.kind != statementClass || class.name != "H1bReceiver" || class.superclass != "" || len(class.body) != 1 ||
		read.kind != statementMethod || read.name != "read_h1b" || len(read.parameters) != 1 || read.parameters[0] != "receiver" || len(read.body) != 1 ||
		install.kind != statementMethod || install.name != "install_h1b" || len(install.parameters) != 1 || install.parameters[0] != "receiver" || len(install.body) != 5 {
		return h1bSpanPlan{}, false
	}
	ordinary := class.body[0]
	if ordinary == nil || ordinary.kind != statementMethod || ordinary.name != "label" || len(ordinary.parameters) != 0 || len(ordinary.body) != 1 ||
		!h1bIntegerStatement(ordinary.body[0], 7) || !h1bReceiverCallStatement(read.body[0], "receiver", "label", 0) {
		return h1bSpanPlan{}, false
	}
	definition := install.body[1]
	if !h1bAssignment(install.body[0], "captured", expressionInteger, 40) ||
		definition == nil || definition.kind != statementExpression || !h1bSingletonDefinition(definition.expression) ||
		!h1bGCStatement(install.body[2]) || !h1bAdditionAssignment(install.body[3], "captured", 2) ||
		!h1bLocalStatement(install.body[4], "receiver") {
		return h1bSpanPlan{}, false
	}

	assignments := []struct {
		index int
		name  string
	}{
		{3, "receiver"}, {4, "peer"}, {5, "before"}, {6, "warm"}, {7, "peer_before"},
		{8, "receiver"}, {9, "first"}, {10, "second"}, {11, "peer_after"}, {12, "receiver"},
		{14, "peer_after_receiver_collection"}, {15, "h1b_result"}, {16, "peer"},
	}
	for _, assignment := range assignments {
		if top[assignment.index] == nil || top[assignment.index].kind != statementAssign || top[assignment.index].name != assignment.name {
			return h1bSpanPlan{}, false
		}
	}
	if !h1bNewAssignment(top[3], "receiver") || !h1bNewAssignment(top[4], "peer") ||
		!h1bReadAssignment(top[5], "before", "receiver") || !h1bReadAssignment(top[6], "warm", "receiver") ||
		!h1bReadAssignment(top[7], "peer_before", "peer") || !h1bInstallAssignment(top[8]) ||
		!h1bReadAssignment(top[9], "first", "receiver") || !h1bReadAssignment(top[10], "second", "receiver") ||
		!h1bReadAssignment(top[11], "peer_after", "peer") || !h1bAssignment(top[12], "receiver", expressionInteger, 0) ||
		!h1bGCStatement(top[13]) || !h1bReadAssignment(top[14], "peer_after_receiver_collection", "peer") ||
		!h1bResultAssignment(top[15]) || !h1bAssignment(top[16], "peer", expressionInteger, 0) || !h1bGCStatement(top[17]) {
		return h1bSpanPlan{}, false
	}

	return h1bSpanPlan{
		classDeclaration:    class.span,
		ordinaryMethod:      ordinary.span,
		readMethod:          read.span,
		readCall:            read.body[0].expression.span,
		installMethod:       install.span,
		captureDeclaration:  install.body[0].span,
		singletonDefinition: definition.span,
		singletonBlock:      definition.expression.block.span,
		helperMutation:      install.body[3].span,
		helperReturn:        install.body[4].span,
		allocations:         [2]Span{top[3].span, top[4].span},
		reads: [7]Span{
			top[5].span, top[6].span, top[7].span, top[9].span, top[10].span, top[11].span, top[14].span,
		},
		drops:       [2]Span{top[12].span, top[16].span},
		collections: [3]Span{install.body[2].span, top[13].span, top[17].span},
		result:      top[15].span,
	}, true
}

func h1bIntegerStatement(statement *statement, value int64) bool {
	return statement != nil && statement.kind == statementExpression && statement.expression != nil &&
		statement.expression.kind == expressionInteger && statement.expression.integer == value
}

func h1bLocalStatement(statement *statement, name string) bool {
	return statement != nil && statement.kind == statementExpression && statement.expression != nil &&
		statement.expression.kind == expressionLocal && statement.expression.text == name
}

func h1bAssignment(statement *statement, name string, kind expressionKind, integer int64) bool {
	return statement != nil && statement.kind == statementAssign && statement.name == name && statement.expression != nil &&
		statement.expression.kind == kind && statement.expression.integer == integer
}

func h1bAdditionAssignment(statement *statement, name string, increment int64) bool {
	if statement == nil || statement.kind != statementAssign || statement.name != name || statement.expression == nil {
		return false
	}
	expression := statement.expression
	return expression.kind == expressionBinary && expression.text == "+" && expression.left != nil &&
		expression.left.kind == expressionLocal && expression.left.text == name && expression.right != nil &&
		expression.right.kind == expressionInteger && expression.right.integer == increment
}

func h1bReceiverCallStatement(statement *statement, receiver, name string, argumentCount int) bool {
	return statement != nil && statement.kind == statementExpression &&
		h1bReceiverCall(statement.expression, receiver, name, argumentCount)
}

func h1bReceiverCall(expression *expression, receiver, name string, argumentCount int) bool {
	return expression != nil && expression.kind == expressionCall && expression.name == name && len(expression.args) == argumentCount &&
		expression.receiver != nil && expression.receiver.kind == expressionLocal && expression.receiver.text == receiver && expression.block == nil
}

func h1bSingletonDefinition(expression *expression) bool {
	if expression == nil || expression.kind != expressionCall || expression.name != "define_singleton_method" ||
		expression.receiver == nil || expression.receiver.kind != expressionLocal || expression.receiver.text != "receiver" ||
		len(expression.args) != 1 || expression.args[0] == nil || expression.args[0].kind != expressionSymbol || expression.args[0].text != "label" ||
		expression.block == nil || len(expression.block.parameters) != 0 || len(expression.block.body) != 1 {
		return false
	}
	return h1bAdditionAssignment(expression.block.body[0], "captured", 1)
}

func h1bGCStatement(statement *statement) bool {
	if statement == nil || statement.kind != statementExpression || statement.expression == nil {
		return false
	}
	expression := statement.expression
	return expression.kind == expressionCall && expression.name == "start" && len(expression.args) == 0 && expression.block == nil &&
		expression.receiver != nil && expression.receiver.kind == expressionConstant && expression.receiver.text == "GC"
}

func h1bNewAssignment(statement *statement, name string) bool {
	if statement == nil || statement.kind != statementAssign || statement.name != name || statement.expression == nil {
		return false
	}
	expression := statement.expression
	return expression.kind == expressionCall && expression.name == "new" && len(expression.args) == 0 && expression.block == nil &&
		expression.receiver != nil && expression.receiver.kind == expressionConstant && expression.receiver.text == "H1bReceiver"
}

func h1bReadAssignment(statement *statement, name, receiver string) bool {
	if statement == nil || statement.kind != statementAssign || statement.name != name || statement.expression == nil {
		return false
	}
	expression := statement.expression
	return expression.kind == expressionCall && expression.receiver == nil && expression.name == "read_h1b" && len(expression.args) == 1 &&
		expression.block == nil && expression.args[0] != nil && expression.args[0].kind == expressionLocal && expression.args[0].text == receiver
}

func h1bInstallAssignment(statement *statement) bool {
	if statement == nil || statement.kind != statementAssign || statement.name != "receiver" || statement.expression == nil {
		return false
	}
	expression := statement.expression
	return expression.kind == expressionCall && expression.receiver == nil && expression.name == "install_h1b" && len(expression.args) == 1 &&
		expression.block == nil && expression.args[0] != nil && expression.args[0].kind == expressionLocal && expression.args[0].text == "receiver"
}

func h1bResultAssignment(statement *statement) bool {
	if statement == nil || statement.kind != statementAssign || statement.name != "h1b_result" || statement.expression == nil ||
		statement.expression.kind != expressionArray || len(statement.expression.items) != 7 {
		return false
	}
	want := [...]string{"before", "warm", "peer_before", "first", "second", "peer_after", "peer_after_receiver_collection"}
	for index, item := range statement.expression.items {
		if item == nil || item.kind != expressionLocal || item.text != want[index] {
			return false
		}
	}
	return true
}

func h1bProgramIdentity(source string) [sha256.Size]byte {
	hash := sha256.New()
	hash.Write([]byte(h1bIdentityDomain))
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(source)))
	hash.Write(size[:])
	hash.Write([]byte(source))
	var out [sha256.Size]byte
	copy(out[:], hash.Sum(nil))
	return out
}

func h1bMismatchSpan(source string) Span {
	offset := 0
	for offset < len(source) && offset < len(H1bSource) && source[offset] == H1bSource[offset] {
		offset++
	}
	line, column := h1bLineColumn(source, offset)
	end, endLine, endColumn := offset, line, column
	if offset < len(source) {
		end++
		if source[offset] == '\n' {
			endLine, endColumn = line+1, 1
		} else {
			endColumn++
		}
	}
	return Span{Start: offset, End: end, StartLine: line, StartColumn: column, EndLine: endLine, EndColumn: endColumn}
}

func h1bWholeSourceSpan(source string) Span {
	line, column := h1bLineColumn(source, len(source))
	return Span{Start: 0, End: len(source), StartLine: 1, StartColumn: 1, EndLine: line, EndColumn: column}
}

func h1bLineColumn(source string, offset int) (line, column int) {
	line, column = 1, 1
	if offset > len(source) {
		offset = len(source)
	}
	for index := 0; index < offset; index++ {
		if source[index] == '\n' {
			line, column = line+1, 1
		} else {
			column++
		}
	}
	return line, column
}
