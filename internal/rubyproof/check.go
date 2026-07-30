package rubyproof

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
)

type classID uint32
type bindingID uint32
type topMethodID uint32

type checkedOwnerKind uint8

const (
	checkedOwnerClass checkedOwnerKind = iota + 1
	checkedOwnerModule
	checkedOwnerSingleton
)

type checkedCallKind uint8

const (
	checkedCallReceiver checkedCallKind = iota + 1
	checkedCallTop
	checkedCallNew
	checkedCallEqual
	checkedCallDefineSingleton
)

type checkedCallMode uint8

const (
	// checkedCallAnyVisibility is used by implicit Ruby sends and Kernel#send.
	checkedCallAnyVisibility checkedCallMode = iota + 1
	// checkedCallPublicVisibility is used by explicit sends in this bounded
	// proof and by Kernel#public_send. Caller-sensitive protected access is a
	// deliberately separate future category.
	checkedCallPublicVisibility
)

type checkedCall struct {
	kind           checkedCallKind
	mode           checkedCallMode
	argumentOffset uint8
	selector       selectorID
	site           callSiteID
	class          classID
	top            topMethodID
	dispatch       dispatchClassID
	shape          shapeID
	mutation       uint32
}

type checkedProgram struct {
	source                   string
	module                   *syntaxModule
	identity                 [sha256.Size]byte
	classes                  map[string]*checkedClass
	classOrder               []*checkedClass
	topMethods               map[string][]*checkedMethod
	methods                  map[*statement]*checkedMethod
	classOperations          map[*statement]*checkedClassOperation
	orderedOperations        []*checkedClassOperation
	attachments              []*checkedAttachment
	definitions              []*checkedDefinition
	aliasBodies              map[string][]*checkedMethod
	bindings                 map[*expression]bindingID
	assignments              map[*statement]bindingID
	blockBindings            map[*block][]bindingID
	calls                    map[*expression]checkedCall
	selectors                map[string]selectorID
	selectorNames            []string
	topMethodNames           []string
	topMethodIDs             map[string]topMethodID
	methodsByDefinition      []*checkedMethod
	lookup                   *lookupImage
	finalResultBinding       bindingID
	topologyResultBinding    bindingID
	singletonResultBinding   bindingID
	initialLabelMethod       *checkedMethod
	redefinedLabelMethod     *checkedMethod
	lookupOrder              []*checkedClass
	singletonOwner           *checkedClass
	singletonAllocation      *statement
	singletonAllocationCall  *expression
	singletonDefinitionCall  *expression
	singletonDefinition      *checkedDefinition
	singletonOperation       *checkedClassOperation
	singletonReceiverBinding bindingID
}

type checkedClass struct {
	id              classID
	name            string
	kind            checkedOwnerKind
	declarationSpan Span
	superclass      *checkedClass
	node            lookupNodeID
	dispatch        dispatchClassID
	shape           shapeID
	realized        bool
	methods         map[string][]*checkedMethod
}

type checkedMethod struct {
	selector          selectorID
	slot              methodSlotID
	definition        definitionID
	topID             topMethodID
	version           uint32
	owner             *checkedClass
	declaration       *statement
	parameterBindings []bindingID
}

// checkedDefinition separates the Ruby method-table identity installed at an
// owner/selector coordinate from the snapshotted executable body. Ordinary
// definitions point at their own body; aliases receive a new definition while
// retaining the source body's checked method.
type checkedDefinition struct {
	id          definitionID
	owner       *checkedClass
	name        string
	selector    selectorID
	slot        methodSlotID
	body        *checkedMethod
	declaration *statement
}

type checkedClassOperation struct {
	id         uint32
	kind       lookupMutationKind
	owner      *checkedClass
	name       string
	selector   selectorID
	slot       methodSlotID
	definition *checkedDefinition
	visibility lookupVisibility
	statement  *statement
	attachment *checkedAttachment
}

type checkedAttachment struct {
	id      lookupAttachmentID
	kind    lookupAttachmentKind
	target  *checkedClass
	module  *checkedClass
	ordinal uint32
}

type declaredEntry struct {
	kind       lookupEntryKind
	definition *checkedDefinition
	visibility lookupVisibility
}

type checker struct {
	program         *checkedProgram
	diagnostics     []Diagnostic
	nextClass       classID
	nextBinding     bindingID
	declaredEntries map[*checkedClass]map[string]declaredEntry
}

type lexicalScope struct {
	method *checkedMethod
	locals map[string]bindingID
}

// Compile lexes, parses, resolves, and validates the deliberately bounded Ruby
// subset. Diagnostics are deterministic and source-attributed.
func Compile(source string) (Program, []Diagnostic) {
	tokens, diagnostics := lex(source)
	if len(diagnostics) != 0 {
		return Program{}, diagnostics
	}
	module, diagnostics := parse(tokens)
	if len(diagnostics) != 0 {
		return Program{}, diagnostics
	}
	checked, diagnostics := checkRuby(source, module)
	if len(diagnostics) != 0 {
		slices.SortStableFunc(diagnostics, func(left, right Diagnostic) int {
			if order := cmp.Compare(left.Span.Start, right.Span.Start); order != 0 {
				return order
			}
			if order := cmp.Compare(left.Span.End, right.Span.End); order != 0 {
				return order
			}
			return cmp.Compare(left.Message, right.Message)
		})
		return Program{}, diagnostics
	}
	return Program{checked: checked}, nil
}

func checkRuby(source string, module *syntaxModule) (*checkedProgram, []Diagnostic) {
	program := &checkedProgram{
		source:          source,
		module:          module,
		identity:        rubyProgramIdentity(source),
		classes:         make(map[string]*checkedClass),
		topMethods:      make(map[string][]*checkedMethod),
		methods:         make(map[*statement]*checkedMethod),
		classOperations: make(map[*statement]*checkedClassOperation),
		aliasBodies:     make(map[string][]*checkedMethod),
		bindings:        make(map[*expression]bindingID),
		assignments:     make(map[*statement]bindingID),
		blockBindings:   make(map[*block][]bindingID),
		calls:           make(map[*expression]checkedCall),
		selectors:       make(map[string]selectorID),
		topMethodIDs:    make(map[string]topMethodID),
	}
	c := checker{program: program, declaredEntries: make(map[*checkedClass]map[string]declaredEntry)}
	c.declare(module.statements)
	if len(c.diagnostics) == 0 {
		c.declareSingletonProofFacts(module.statements)
	}
	if len(c.diagnostics) == 0 {
		c.assignMethodFacts()
		c.resolveMethods()
		c.resolveTopLevel(module.statements)
		c.assignCallSites()
		c.validateProofShape()
		c.buildLookupImage()
	}
	if len(c.diagnostics) != 0 {
		return nil, c.diagnostics
	}
	return program, nil
}

func (c *checker) declare(statements []*statement) {
	if c.declaredEntries == nil {
		c.declaredEntries = make(map[*checkedClass]map[string]declaredEntry)
	}
	if c.program.classOperations == nil {
		c.program.classOperations = make(map[*statement]*checkedClassOperation)
	}
	if c.program.aliasBodies == nil {
		c.program.aliasBodies = make(map[string][]*checkedMethod)
	}
	for _, item := range statements {
		switch item.kind {
		case statementClass, statementModule:
			class := c.declareOwner(item)
			if class == nil {
				continue
			}
			for _, member := range item.body {
				switch member.kind {
				case statementMethod:
					c.declareMethod(class, member)
				case statementExpression:
					c.declareClassMutation(class, member)
				default:
					c.problem(member.span, "ruby: class body statement is outside the proof subset")
				}
			}
		case statementMethod:
			c.declareMethod(nil, item)
		case statementAssign, statementExpression, statementBegin, statementReturn, statementRaise:
			if item.kind == statementReturn || item.kind == statementRaise || item.kind == statementBegin {
				c.problem(item.span, "ruby: this statement is not admitted at top level")
			}
		default:
			c.problem(item.span, "ruby: unsupported top-level statement")
		}
	}
}

// declareSingletonProofFacts recognizes the one allocation-site singleton
// method admitted by the bounded proof. The source form is ordinary Ruby, but
// its block is normalized to a checked method body rather than retained as a
// runtime Proc. This keeps receiver lookup language-owned without introducing
// a general object-to-singleton-class registry or heap lifetime.
func (c *checker) declareSingletonProofFacts(statements []*statement) {
	var allocation *statement
	var definitionStatement *statement
	var definitionCall *expression
	for _, item := range statements {
		if item.kind == statementAssign && item.name == "c2_receiver" {
			if allocation != nil {
				c.problem(item.span, "ruby: proof c2_receiver allocation must appear exactly once")
				continue
			}
			allocation = item
		}
		if item.kind != statementExpression || item.expression == nil || item.expression.kind != expressionCall || item.expression.name != "define_singleton_method" {
			continue
		}
		if definitionCall != nil {
			c.problem(item.span, "ruby: proof admits exactly one define_singleton_method call")
			continue
		}
		definitionStatement = item
		definitionCall = item.expression
	}
	if allocation == nil {
		c.problem(Span{}, "ruby: proof requires one c2_receiver allocation")
		return
	}
	alpha := c.program.classes["Alpha"]
	allocationCall := allocation.expression
	if alpha == nil || allocationCall == nil || allocationCall.kind != expressionCall || allocationCall.receiver == nil ||
		allocationCall.receiver.kind != expressionConstant || allocationCall.receiver.text != "Alpha" || allocationCall.name != "new" ||
		allocationCall.block != nil || len(allocationCall.args) != 1 || allocationCall.args[0].kind != expressionInteger || allocationCall.args[0].integer != 10 {
		c.problem(allocation.span, "ruby: proof c2_receiver must be assigned directly from Alpha.new(10)")
		return
	}
	if definitionCall == nil {
		c.problem(Span{}, "ruby: proof requires one c2_receiver define_singleton_method(:label) block")
		return
	}
	if definitionCall.receiver == nil || definitionCall.receiver.kind != expressionLocal || definitionCall.receiver.text != "c2_receiver" ||
		len(definitionCall.args) != 1 || definitionCall.args[0].kind != expressionSymbol || definitionCall.args[0].text != "label" ||
		definitionCall.block == nil || len(definitionCall.block.parameters) != 0 {
		c.problem(definitionCall.span, "ruby: proof singleton definition requires c2_receiver.define_singleton_method(:label) with a zero-parameter block")
		return
	}
	blockBody := definitionCall.block.body
	if len(blockBody) != 2 || !isImplicitIntegerCall(blockBody[0], "mark", 4) ||
		blockBody[1].kind != statementExpression || blockBody[1].expression == nil ||
		blockBody[1].expression.kind != expressionInteger || blockBody[1].expression.integer != 44 {
		c.problem(definitionCall.block.span, "ruby: proof singleton label block must be exactly mark(4) followed by 44")
		return
	}

	c.nextClass++
	singleton := &checkedClass{
		id: c.nextClass, name: "#<singleton:c2_receiver>", kind: checkedOwnerSingleton,
		declarationSpan: definitionCall.span, superclass: alpha, realized: true,
		methods: make(map[string][]*checkedMethod),
	}
	c.declaredEntries[singleton] = make(map[string]declaredEntry)
	c.program.lookupOrder = append(append([]*checkedClass(nil), c.program.classOrder...), singleton)
	c.program.singletonOwner = singleton
	c.program.singletonAllocation = allocation
	c.program.singletonAllocationCall = allocationCall
	c.program.singletonDefinitionCall = definitionCall

	declaration := &statement{
		kind: statementMethod, span: definitionCall.block.span, name: "label",
		body: definitionCall.block.body,
	}
	method := &checkedMethod{version: 1, owner: singleton, declaration: declaration}
	singleton.methods["label"] = []*checkedMethod{method}
	c.program.methods[declaration] = method
	definition := &checkedDefinition{owner: singleton, name: "label", body: method, declaration: declaration}
	operation := &checkedClassOperation{
		kind: lookupMutationDefine, owner: singleton, name: "label", definition: definition,
		visibility: lookupVisibilityPublic, statement: definitionStatement,
	}
	c.program.definitions = append(c.program.definitions, definition)
	c.program.orderedOperations = append(c.program.orderedOperations, operation)
	c.declaredEntries[singleton]["label"] = declaredEntry{kind: lookupEntryDefined, definition: definition, visibility: lookupVisibilityPublic}
	c.program.singletonDefinition = definition
	c.program.singletonOperation = operation
}

func isImplicitIntegerCall(item *statement, name string, argument int64) bool {
	if item == nil || item.kind != statementExpression || item.expression == nil {
		return false
	}
	call := item.expression
	return call.kind == expressionCall && call.receiver == nil && call.name == name && call.block == nil &&
		len(call.args) == 1 && call.args[0].kind == expressionInteger && call.args[0].integer == argument
}

func (c *checker) declareOwner(item *statement) *checkedClass {
	kind := checkedOwnerClass
	kindName := "class"
	if item.kind == statementModule {
		kind = checkedOwnerModule
		kindName = "module"
	}
	owner := c.program.classes[item.name]
	if owner != nil {
		if owner.kind != kind {
			c.problem(item.span, fmt.Sprintf("ruby: %s %q was first declared as a %s", kindName, item.name, owner.kind.name()))
			return nil
		}
		if item.superclass != "" && (owner.superclass == nil || owner.superclass.name != item.superclass) {
			want := "no explicit superclass"
			if owner.superclass != nil {
				want = owner.superclass.name
			}
			c.problem(item.span, fmt.Sprintf("ruby: superclass mismatch for %q: first definition uses %s", item.name, want))
		}
		if c.declaredEntries[owner] == nil {
			c.declaredEntries[owner] = make(map[string]declaredEntry)
		}
		return owner
	}

	var superclass *checkedClass
	if item.superclass != "" {
		if item.superclass == item.name {
			c.problem(item.span, fmt.Sprintf("ruby: class %q cannot inherit from itself", item.name))
		} else {
			superclass = c.program.classes[item.superclass]
			if superclass == nil {
				c.problem(item.span, fmt.Sprintf("ruby: unknown superclass %q", item.superclass))
			} else if superclass.kind != checkedOwnerClass {
				c.problem(item.span, fmt.Sprintf("ruby: superclass %q is not a class", item.superclass))
				superclass = nil
			}
		}
	}
	c.nextClass++
	owner = &checkedClass{
		id: c.nextClass, name: item.name, kind: kind, declarationSpan: item.span,
		superclass: superclass, methods: make(map[string][]*checkedMethod),
	}
	c.program.classes[item.name] = owner
	c.program.classOrder = append(c.program.classOrder, owner)
	c.declaredEntries[owner] = make(map[string]declaredEntry)
	return owner
}

func (kind checkedOwnerKind) name() string {
	if kind == checkedOwnerSingleton {
		return "singleton"
	}
	if kind == checkedOwnerModule {
		return "module"
	}
	return "class"
}

func (c *checker) declareMethod(owner *checkedClass, declaration *statement) {
	methods := c.program.topMethods[declaration.name]
	if owner != nil {
		methods = owner.methods[declaration.name]
	}
	method := &checkedMethod{version: uint32(len(methods) + 1), owner: owner, declaration: declaration}
	c.program.methods[declaration] = method
	if owner == nil {
		c.program.topMethods[declaration.name] = append(methods, method)
	} else {
		owner.methods[declaration.name] = append(methods, method)
		definition := &checkedDefinition{owner: owner, name: declaration.name, body: method, declaration: declaration}
		operation := &checkedClassOperation{kind: lookupMutationDefine, owner: owner, name: declaration.name, definition: definition, visibility: lookupVisibilityPublic, statement: declaration}
		c.program.definitions = append(c.program.definitions, definition)
		c.program.orderedOperations = append(c.program.orderedOperations, operation)
		c.program.classOperations[declaration] = operation
		c.declaredEntries[owner][declaration.name] = declaredEntry{kind: lookupEntryDefined, definition: definition, visibility: lookupVisibilityPublic}
	}
}

func (c *checker) declareClassMutation(owner *checkedClass, item *statement) {
	call := item.expression
	if call == nil || call.kind != expressionCall || call.receiver != nil || call.block != nil {
		c.problem(item.span, "ruby: class body statement is outside the proof subset")
		return
	}
	symbol := func(index int) (string, bool) {
		if index >= len(call.args) || call.args[index].kind != expressionSymbol || call.args[index].text == "" {
			return "", false
		}
		return call.args[index].text, true
	}
	constant := func(index int) (string, bool) {
		if index >= len(call.args) || call.args[index].kind != expressionConstant || call.args[index].text == "" {
			return "", false
		}
		return call.args[index].text, true
	}
	var operation *checkedClassOperation
	switch call.name {
	case "include", "prepend":
		name, ok := constant(0)
		if len(call.args) != 1 || !ok {
			c.problem(item.span, fmt.Sprintf("ruby: %s requires one literal module constant", call.name))
			return
		}
		if owner.kind != checkedOwnerClass {
			c.problem(item.span, fmt.Sprintf("ruby: %s is admitted only in a class body", call.name))
			return
		}
		module := c.program.classes[name]
		if module == nil {
			c.problem(item.span, fmt.Sprintf("ruby: unknown module %q", name))
			return
		}
		if module.kind != checkedOwnerModule {
			c.problem(item.span, fmt.Sprintf("ruby: %s target %q is not a module", call.name, name))
			return
		}
		var ordinal uint32
		for _, attachment := range c.program.attachments {
			if attachment.target != owner {
				continue
			}
			if attachment.module == module {
				c.problem(item.span, fmt.Sprintf("ruby: module %q is already attached to %q", name, owner.name))
				return
			}
			ordinal++
		}
		if len(c.program.attachments) >= maximumLookupAttachments {
			c.problem(item.span, fmt.Sprintf("ruby: attachment count exceeds %d", maximumLookupAttachments))
			return
		}
		attachmentKind := lookupAttachmentInclude
		mutationKind := lookupMutationInclude
		if call.name == "prepend" {
			attachmentKind = lookupAttachmentPrepend
			mutationKind = lookupMutationPrepend
		}
		attachment := &checkedAttachment{
			id: lookupAttachmentID(len(c.program.attachments) + 1), kind: attachmentKind,
			target: owner, module: module, ordinal: ordinal,
		}
		c.program.attachments = append(c.program.attachments, attachment)
		operation = &checkedClassOperation{kind: mutationKind, owner: owner, statement: item, attachment: attachment}
	case "alias_method":
		to, toOK := symbol(0)
		from, fromOK := symbol(1)
		if len(call.args) != 2 || !toOK || !fromOK {
			c.problem(item.span, "ruby: alias_method requires two symbol arguments")
			return
		}
		source := c.declaredLookup(owner, from)
		if source.kind != lookupEntryDefined || source.definition == nil || source.definition.body == nil {
			c.problem(item.span, fmt.Sprintf("ruby: alias_method source %q is not defined", from))
			return
		}
		definition := &checkedDefinition{owner: owner, name: to, body: source.definition.body, declaration: item}
		operation = &checkedClassOperation{kind: lookupMutationAlias, owner: owner, name: to, definition: definition, visibility: source.visibility, statement: item}
		c.program.definitions = append(c.program.definitions, definition)
		c.program.aliasBodies[to] = append(c.program.aliasBodies[to], source.definition.body)
		c.declaredEntries[owner][to] = declaredEntry{kind: lookupEntryDefined, definition: definition, visibility: source.visibility}
	case "remove_method":
		name, ok := symbol(0)
		if len(call.args) != 1 || !ok {
			c.problem(item.span, "ruby: remove_method requires one symbol argument")
			return
		}
		entry := c.declaredEntries[owner][name]
		if entry.kind != lookupEntryDefined {
			c.problem(item.span, fmt.Sprintf("ruby: remove_method target %q is not locally defined", name))
			return
		}
		operation = &checkedClassOperation{kind: lookupMutationRemove, owner: owner, name: name, statement: item}
		c.declaredEntries[owner][name] = declaredEntry{kind: lookupEntryAbsent}
	case "undef_method":
		name, ok := symbol(0)
		if len(call.args) != 1 || !ok {
			c.problem(item.span, "ruby: undef_method requires one symbol argument")
			return
		}
		if entry := c.declaredEntries[owner][name]; entry.kind != lookupEntryDefined {
			c.problem(item.span, fmt.Sprintf("ruby: undef_method target %q is not locally defined in the proof subset", name))
			return
		}
		operation = &checkedClassOperation{kind: lookupMutationUndef, owner: owner, name: name, statement: item}
		c.declaredEntries[owner][name] = declaredEntry{kind: lookupEntryUndef}
	case "public", "protected", "private":
		name, ok := symbol(0)
		if len(call.args) != 1 || !ok {
			c.problem(item.span, fmt.Sprintf("ruby: %s requires one symbol argument", call.name))
			return
		}
		entry := c.declaredEntries[owner][name]
		if entry.kind != lookupEntryDefined || entry.definition == nil {
			c.problem(item.span, fmt.Sprintf("ruby: %s target %q is not locally defined", call.name, name))
			return
		}
		visibility := map[string]lookupVisibility{
			"public": lookupVisibilityPublic, "protected": lookupVisibilityProtected, "private": lookupVisibilityPrivate,
		}[call.name]
		if entry.visibility == visibility {
			c.problem(item.span, fmt.Sprintf("ruby: %s target %q already has that visibility", call.name, name))
			return
		}
		operation = &checkedClassOperation{kind: lookupMutationVisibility, owner: owner, name: name, visibility: visibility, statement: item}
		entry.visibility = visibility
		c.declaredEntries[owner][name] = entry
	default:
		c.problem(item.span, fmt.Sprintf("ruby: class mutation %q is outside the proof subset", call.name))
		return
	}
	c.program.orderedOperations = append(c.program.orderedOperations, operation)
	c.program.classOperations[item] = operation
}

func (c *checker) declaredLookup(owner *checkedClass, name string) declaredEntry {
	for current := owner; current != nil; current = current.superclass {
		entry := c.declaredEntries[current][name]
		if entry.kind == lookupEntryUndef || entry.kind == lookupEntryDefined {
			return entry
		}
	}
	return declaredEntry{kind: lookupEntryAbsent}
}

func (c *checker) assignMethodFacts() {
	nameSet := make(map[string]bool)
	topNameSet := make(map[string]bool)
	for _, method := range c.program.methods {
		if method.owner == nil {
			topNameSet[method.declaration.name] = true
		}
	}
	for _, operation := range c.program.orderedOperations {
		if operation.attachment == nil {
			nameSet[operation.name] = true
		}
	}
	for name := range nameSet {
		c.program.selectorNames = append(c.program.selectorNames, name)
	}
	slices.Sort(c.program.selectorNames)
	if len(c.program.selectorNames) > maximumPreparedSelectors {
		c.problem(Span{}, fmt.Sprintf("ruby: prepared selector count exceeds %d", maximumPreparedSelectors))
		return
	}
	for index, name := range c.program.selectorNames {
		c.program.selectors[name] = selectorID(index + 1)
	}
	for name := range topNameSet {
		c.program.topMethodNames = append(c.program.topMethodNames, name)
	}
	slices.Sort(c.program.topMethodNames)
	for index, name := range c.program.topMethodNames {
		c.program.topMethodIDs[name] = topMethodID(index + 1)
		for _, method := range c.program.topMethods[name] {
			method.topID = topMethodID(index + 1)
		}
	}

	type slotKey struct {
		owner    classID
		selector selectorID
	}
	slots := make(map[slotKey]methodSlotID)
	var nextSlot methodSlotID
	for operationIndex, operation := range c.program.orderedOperations {
		operation.id = uint32(operationIndex + 1)
		if operation.attachment != nil {
			continue
		}
		operation.selector = c.program.selectors[operation.name]
		key := slotKey{owner: operation.owner.id, selector: operation.selector}
		operation.slot = slots[key]
		if operation.slot == 0 {
			nextSlot++
			operation.slot = nextSlot
			slots[key] = operation.slot
		}
		if operation.definition == nil {
			continue
		}
		definition := operation.definition
		definition.id = definitionID(len(c.program.methodsByDefinition) + 1)
		definition.selector = operation.selector
		definition.slot = operation.slot
		if definition.body == nil {
			c.problem(operation.statement.span, "ruby: definition has no snapshotted body")
			continue
		}
		c.program.methodsByDefinition = append(c.program.methodsByDefinition, definition.body)
		if definition.declaration.kind == statementMethod {
			method := c.program.methods[definition.declaration]
			method.selector = definition.selector
			method.slot = definition.slot
			method.definition = definition.id
		}
	}
}

func (c *checker) resolveMethods() {
	methods := make([]*checkedMethod, 0, len(c.program.methods))
	for _, method := range c.program.methods {
		methods = append(methods, method)
	}
	slices.SortFunc(methods, func(left, right *checkedMethod) int {
		return cmp.Compare(left.declaration.span.Start, right.declaration.span.Start)
	})
	for _, method := range methods {
		scope := lexicalScope{method: method, locals: make(map[string]bindingID)}
		seen := make(map[string]bool)
		for _, name := range method.declaration.parameters {
			if seen[name] {
				c.problem(method.declaration.span, fmt.Sprintf("ruby: duplicate parameter %q", name))
				continue
			}
			seen[name] = true
			binding := c.newBinding()
			scope.locals[name] = binding
			method.parameterBindings = append(method.parameterBindings, binding)
		}
		c.resolveStatements(method.declaration.body, &scope)
	}
}

func (c *checker) resolveTopLevel(statements []*statement) {
	scope := lexicalScope{locals: make(map[string]bindingID)}
	for _, item := range statements {
		switch item.kind {
		case statementClass, statementModule, statementMethod:
			continue
		case statementAssign:
			c.resolveAssignment(item, &scope)
		default:
			c.resolveStatement(item, &scope)
		}
	}
	binding, ok := scope.locals["result"]
	if !ok {
		c.problem(Span{}, "ruby: proof program must assign final local result")
		return
	}
	c.program.finalResultBinding = binding
	topology, ok := scope.locals["c1_result"]
	if !ok {
		c.problem(Span{}, "ruby: proof program must assign final local c1_result")
		return
	}
	c.program.topologyResultBinding = topology
	singleton, ok := scope.locals["c2_result"]
	if !ok {
		c.problem(Span{}, "ruby: proof program must assign final local c2_result")
		return
	}
	c.program.singletonResultBinding = singleton
}

func (c *checker) resolveStatements(statements []*statement, scope *lexicalScope) {
	for _, item := range statements {
		c.resolveStatement(item, scope)
	}
}

func (c *checker) resolveStatement(item *statement, scope *lexicalScope) {
	switch item.kind {
	case statementAssign:
		c.resolveAssignment(item, scope)
	case statementExpression, statementRaise:
		c.resolveExpression(item.expression, scope)
	case statementReturn:
		if scope.method == nil {
			c.problem(item.span, "ruby: return requires a method or its block")
		}
		c.resolveExpression(item.expression, scope)
	case statementBegin:
		if item.rescueClass != "" && item.rescueClass != "RuntimeError" && item.rescueClass != "NoMethodError" {
			c.problem(item.span, fmt.Sprintf("ruby: rescue class %q is outside the proof subset", item.rescueClass))
		}
		c.resolveStatements(item.body, scope)
		c.resolveStatements(item.rescueBody, scope)
		c.resolveStatements(item.ensureBody, scope)
	case statementClass, statementModule, statementMethod:
		c.problem(item.span, "ruby: nested class, module, or method definition is outside the proof subset")
	default:
		c.problem(item.span, "ruby: unsupported statement")
	}
}

func (c *checker) resolveAssignment(item *statement, scope *lexicalScope) {
	c.resolveExpression(item.expression, scope)
	if item.name[0] == '@' {
		if scope.method == nil || scope.method.owner == nil {
			c.problem(item.span, "ruby: instance variable assignment requires an instance method")
		}
		return
	}
	binding, ok := scope.locals[item.name]
	if !ok {
		binding = c.newBinding()
		scope.locals[item.name] = binding
	}
	c.program.assignments[item] = binding
	if item == c.program.singletonAllocation {
		c.program.singletonReceiverBinding = binding
	}
}

func (c *checker) resolveExpression(value *expression, scope *lexicalScope) {
	if value == nil {
		return
	}
	switch value.kind {
	case expressionInteger, expressionString, expressionSymbol:
	case expressionLocal:
		binding, ok := scope.locals[value.text]
		if !ok {
			c.problem(value.span, fmt.Sprintf("ruby: unknown local %q", value.text))
			return
		}
		c.program.bindings[value] = binding
	case expressionInstanceVariable:
		if scope.method == nil || scope.method.owner == nil {
			c.problem(value.span, "ruby: instance variable read requires an instance method")
		}
	case expressionConstant:
		if value.text != "RuntimeError" && value.text != "NoMethodError" && c.program.classes[value.text] == nil {
			c.problem(value.span, fmt.Sprintf("ruby: unknown constant %q", value.text))
		}
	case expressionArray:
		for _, item := range value.items {
			c.resolveExpression(item, scope)
		}
	case expressionBinary:
		c.resolveExpression(value.left, scope)
		c.resolveExpression(value.right, scope)
	case expressionYield:
		if scope.method == nil {
			c.problem(value.span, "ruby: yield requires a method")
		}
		for _, argument := range value.args {
			c.resolveExpression(argument, scope)
		}
	case expressionCall:
		if value == c.program.singletonDefinitionCall {
			if value.receiver != nil {
				c.resolveExpression(value.receiver, scope)
			}
			for _, argument := range value.args {
				c.resolveExpression(argument, scope)
			}
			c.validateCall(value, scope)
			return
		}
		if value.receiver != nil {
			c.resolveExpression(value.receiver, scope)
		}
		for _, argument := range value.args {
			c.resolveExpression(argument, scope)
		}
		c.validateCall(value, scope)
		if value.block != nil {
			blockScope := lexicalScope{method: scope.method, locals: make(map[string]bindingID, len(scope.locals)+len(value.block.parameters))}
			for name, binding := range scope.locals {
				blockScope.locals[name] = binding
			}
			seen := make(map[string]bool)
			for _, name := range value.block.parameters {
				if seen[name] {
					c.problem(value.block.span, fmt.Sprintf("ruby: duplicate block parameter %q", name))
					continue
				}
				seen[name] = true
				binding := c.newBinding()
				blockScope.locals[name] = binding
				c.program.blockBindings[value.block] = append(c.program.blockBindings[value.block], binding)
			}
			c.resolveStatements(value.block.body, &blockScope)
		}
	default:
		c.problem(value.span, "ruby: unsupported expression")
	}
}

func (c *checker) validateCall(call *expression, scope *lexicalScope) {
	if call == c.program.singletonDefinitionCall {
		binding, ok := c.program.bindings[call.receiver]
		if scope.method != nil || !ok || binding == 0 || binding != c.program.singletonReceiverBinding ||
			c.program.singletonOperation == nil || c.program.singletonOperation.id == 0 ||
			c.program.singletonDefinition == nil || c.program.singletonDefinition.id == 0 {
			c.problem(call.span, "ruby: singleton definition receiver does not match the checked c2_receiver allocation")
			return
		}
		c.program.calls[call] = checkedCall{
			kind: checkedCallDefineSingleton, mode: checkedCallPublicVisibility,
			argumentOffset: 1, selector: c.program.singletonDefinition.selector,
			mutation: c.program.singletonOperation.id,
		}
		return
	}
	if call.receiver == nil {
		if scope.method != nil && scope.method.owner != nil {
			methods := classMethods(scope.method.owner, call.name)
			if len(methods) == 0 {
				// A module body is checked before any particular receiver class is
				// known. Its implicit send remains receiver-dispatched and is
				// admitted only when every checked implementation has one arity.
				methods = c.receiverMethods(call.name)
			}
			if len(methods) != 0 {
				c.requireArity(call, methods, 0)
				c.program.calls[call] = checkedCall{kind: checkedCallReceiver, mode: checkedCallAnyVisibility, selector: c.program.selectors[call.name]}
				return
			}
		}
		if methods := c.program.topMethods[call.name]; len(methods) != 0 {
			c.requireArity(call, methods, 0)
			c.program.calls[call] = checkedCall{kind: checkedCallTop, top: c.program.topMethodIDs[call.name]}
			return
		}
		c.problem(call.span, fmt.Sprintf("ruby: unknown implicit method %q", call.name))
		return
	}
	if call.name == "new" {
		if call.receiver.kind != expressionConstant || c.program.classes[call.receiver.text] == nil || c.program.classes[call.receiver.text].kind != checkedOwnerClass {
			c.problem(call.span, "ruby: new receiver must be a known class")
			return
		}
		class := c.program.classes[call.receiver.text]
		class.realized = true
		c.program.calls[call] = checkedCall{kind: checkedCallNew, mode: checkedCallAnyVisibility, class: class.id, selector: c.program.selectors["initialize"]}
		methods := classMethods(class, "initialize")
		if len(methods) == 0 {
			if len(call.args) != 0 {
				c.problem(call.span, "ruby: default initialize accepts no arguments")
			}
			return
		}
		c.requireArity(call, methods, 0)
		return
	}
	if call.name == "equal?" {
		if len(call.args) != 1 {
			c.problem(call.span, "ruby: equal? requires one argument")
		}
		c.program.calls[call] = checkedCall{kind: checkedCallEqual}
		return
	}
	if call.name == "send" || call.name == "public_send" {
		if len(call.args) == 0 || call.args[0].kind != expressionSymbol || call.args[0].text == "" {
			c.problem(call.span, fmt.Sprintf("ruby: %s requires a literal method symbol", call.name))
			return
		}
		name := call.args[0].text
		methods := c.receiverMethods(name)
		selector := c.program.selectors[name]
		if len(methods) == 0 || selector == 0 {
			c.problem(call.span, fmt.Sprintf("ruby: unknown %s target %q", call.name, name))
			return
		}
		c.requireArity(call, methods, 1)
		mode := checkedCallAnyVisibility
		if call.name == "public_send" {
			mode = checkedCallPublicVisibility
		}
		c.program.calls[call] = checkedCall{kind: checkedCallReceiver, mode: mode, argumentOffset: 1, selector: selector}
		return
	}
	methods := c.receiverMethods(call.name)
	if len(methods) == 0 {
		c.problem(call.span, fmt.Sprintf("ruby: unknown receiver method %q", call.name))
		return
	}
	c.requireArity(call, methods, 0)
	c.program.calls[call] = checkedCall{kind: checkedCallReceiver, mode: checkedCallPublicVisibility, selector: c.program.selectors[call.name]}
}

func (c *checker) receiverMethods(name string) []*checkedMethod {
	var methods []*checkedMethod
	for _, class := range c.program.lookupOrder {
		methods = append(methods, class.methods[name]...)
	}
	return append(methods, c.program.aliasBodies[name]...)
}

func classMethods(class *checkedClass, name string) []*checkedMethod {
	for current := class; current != nil; current = current.superclass {
		if methods := current.methods[name]; len(methods) != 0 {
			return methods
		}
	}
	return nil
}

func (c *checker) assignCallSites() {
	var calls []*expression
	for expression, fact := range c.program.calls {
		if fact.kind == checkedCallReceiver {
			calls = append(calls, expression)
		}
	}
	slices.SortFunc(calls, func(left, right *expression) int {
		if order := cmp.Compare(left.span.Start, right.span.Start); order != 0 {
			return order
		}
		return cmp.Compare(left.span.End, right.span.End)
	})
	if len(calls) > maximumLookupSites {
		c.problem(Span{}, fmt.Sprintf("ruby: receiver call-site count exceeds %d", maximumLookupSites))
		return
	}
	for index, expression := range calls {
		fact := c.program.calls[expression]
		fact.site = callSiteID(index + 1)
		c.program.calls[expression] = fact
	}
}

func (c *checker) requireArity(call *expression, methods []*checkedMethod, argumentOffset int) {
	got := len(call.args) - argumentOffset
	if got < 0 {
		got = 0
	}
	for _, method := range methods {
		if len(method.declaration.parameters) != got {
			c.problem(call.span, fmt.Sprintf("ruby: method %q version %d expects %d arguments, got %d", call.name, method.version, len(method.declaration.parameters), got))
			return
		}
	}
}

func (c *checker) validateProofShape() {
	wantOwners := []struct {
		name string
		kind checkedOwnerKind
	}{
		{"Base", checkedOwnerClass},
		{"Alpha", checkedOwnerClass},
		{"Beta", checkedOwnerClass},
		{"Gamma", checkedOwnerClass},
		{"IncludedLabel", checkedOwnerModule},
		{"PrependedLabel", checkedOwnerModule},
		{"Delta", checkedOwnerClass},
	}
	if len(c.program.classOrder) != len(wantOwners) {
		c.problem(Span{}, "ruby: proof program requires five classes and two label modules")
		return
	}
	for index, want := range wantOwners {
		owner := c.program.classOrder[index]
		if owner.name != want.name || owner.kind != want.kind || c.program.classes[want.name] != owner {
			c.problem(Span{}, "ruby: proof owners must first appear as Base, Alpha, Beta, Gamma, IncludedLabel, PrependedLabel, and Delta with exact class/module kinds")
			return
		}
	}
	base := c.program.classes["Base"]
	alpha := c.program.classes["Alpha"]
	beta := c.program.classes["Beta"]
	gamma := c.program.classes["Gamma"]
	included := c.program.classes["IncludedLabel"]
	prepended := c.program.classes["PrependedLabel"]
	delta := c.program.classes["Delta"]
	if base.superclass != nil || alpha.superclass != base || beta.superclass != base || gamma.superclass != base || delta.superclass != alpha ||
		included.superclass != nil || prepended.superclass != nil {
		c.problem(Span{}, "ruby: proof inheritance must be Alpha/Beta/Gamma < Base and Delta < Alpha")
	}
	singleton := c.program.singletonOwner
	if singleton == nil || singleton.id != classID(len(wantOwners)+1) || singleton.name != "#<singleton:c2_receiver>" ||
		singleton.kind != checkedOwnerSingleton || singleton.superclass != alpha || !singleton.realized ||
		len(c.program.lookupOrder) != len(wantOwners)+1 || c.program.lookupOrder[len(wantOwners)] != singleton {
		c.problem(Span{}, "ruby: proof requires one realized c2_receiver singleton lookup owner above Alpha")
	}
	required := map[string]int{"initialize": 1, "mark": 1, "label": 0, "hot": 0, "apply": 0, "value": 0, "trace": 0}
	for name, arity := range required {
		methods := base.methods[name]
		if len(methods) == 0 {
			c.problem(Span{}, fmt.Sprintf("ruby: proof Base is missing %s", name))
			continue
		}
		for _, method := range methods {
			if len(method.declaration.parameters) != arity {
				c.problem(method.declaration.span, fmt.Sprintf("ruby: proof %s arity must be %d", name, arity))
			}
		}
	}
	labels := base.methods["label"]
	if len(labels) != 2 {
		c.problem(Span{}, fmt.Sprintf("ruby: proof requires exactly two Base label versions, got %d", len(labels)))
	} else {
		c.program.initialLabelMethod = labels[0]
		c.program.redefinedLabelMethod = labels[1]
	}
	for _, class := range []*checkedClass{alpha, delta} {
		if len(class.methods) != 0 {
			c.problem(Span{}, fmt.Sprintf("ruby: proof %s must define no local methods", class.name))
		}
	}
	for _, class := range []*checkedClass{beta, gamma} {
		if len(class.methods) != 2 || len(class.methods["initialize"]) != 1 || len(class.methods["label"]) != 1 ||
			len(class.methods["initialize"][0].declaration.parameters) != 1 || len(class.methods["label"][0].declaration.parameters) != 0 {
			c.problem(Span{}, fmt.Sprintf("ruby: proof %s must define initialize(value) and label()", class.name))
		}
	}
	if len(included.methods) != 1 || len(included.methods["label"]) != 2 {
		c.problem(Span{}, "ruby: proof IncludedLabel must define exactly two label versions")
	} else {
		for _, method := range included.methods["label"] {
			if len(method.declaration.parameters) != 0 {
				c.problem(method.declaration.span, "ruby: proof IncludedLabel label arity must be 0")
			}
		}
	}
	if len(prepended.methods) != 1 || len(prepended.methods["label"]) != 1 || len(prepended.methods["label"][0].declaration.parameters) != 0 {
		c.problem(Span{}, "ruby: proof PrependedLabel must define exactly one zero-argument label")
	}
	for _, name := range []string{"read_label", "read_saved", "read_public", "run_return", "read_singleton"} {
		if len(c.program.topMethods[name]) != 1 {
			c.problem(Span{}, fmt.Sprintf("ruby: proof requires one top-level %s method", name))
		}
	}
	if methods := c.program.topMethods["read_singleton"]; len(methods) == 1 && len(methods[0].declaration.parameters) != 1 {
		c.problem(methods[0].declaration.span, "ruby: proof read_singleton arity must be 1")
	}
	c.validateSingletonProofUses()
	if singleton != nil {
		methods := singleton.methods["label"]
		if len(singleton.methods) != 1 || len(methods) != 1 || len(methods[0].declaration.parameters) != 0 ||
			c.program.singletonDefinition == nil || c.program.singletonDefinition.owner != singleton ||
			c.program.singletonDefinition.name != "label" || c.program.singletonDefinition.body != methods[0] ||
			c.program.singletonOperation == nil || c.program.singletonOperation.kind != lookupMutationDefine ||
			c.program.singletonOperation.owner != singleton || c.program.singletonOperation.name != "label" ||
			c.program.singletonOperation.definition != c.program.singletonDefinition ||
			c.program.singletonOperation.visibility != lookupVisibilityPublic ||
			len(c.program.orderedOperations) == 0 || c.program.orderedOperations[len(c.program.orderedOperations)-1] != c.program.singletonOperation {
			c.problem(singleton.methodsSpan(), "ruby: proof singleton owner must contain only the final public label definition")
		}
	}
	if c.program.singletonAllocation == nil || c.program.singletonAllocationCall == nil || c.program.singletonDefinitionCall == nil ||
		c.program.singletonReceiverBinding == 0 || c.program.singletonResultBinding == 0 {
		c.problem(Span{}, "ruby: proof singleton allocation, definition, and result facts are incomplete")
	} else if fact, ok := c.program.calls[c.program.singletonDefinitionCall]; !ok || fact.kind != checkedCallDefineSingleton ||
		fact.mode != checkedCallPublicVisibility || fact.argumentOffset != 1 || c.program.singletonDefinition == nil ||
		fact.selector != c.program.singletonDefinition.selector || c.program.singletonOperation == nil || fact.mutation != c.program.singletonOperation.id {
		c.problem(c.program.singletonDefinitionCall.span, "ruby: proof singleton definition call facts are incomplete")
	}
	var alias, remove, undef *checkedClassOperation
	var visibility []*checkedClassOperation
	var topology []*checkedClassOperation
	for _, operation := range c.program.orderedOperations {
		switch operation.kind {
		case lookupMutationAlias:
			if alias != nil {
				c.problem(operation.statement.span, "ruby: proof admits exactly one alias mutation")
			}
			alias = operation
		case lookupMutationRemove:
			if remove != nil {
				c.problem(operation.statement.span, "ruby: proof admits exactly one remove mutation")
			}
			remove = operation
		case lookupMutationUndef:
			if undef != nil {
				c.problem(operation.statement.span, "ruby: proof admits exactly one undef mutation")
			}
			undef = operation
		case lookupMutationVisibility:
			visibility = append(visibility, operation)
		case lookupMutationInclude, lookupMutationPrepend:
			topology = append(topology, operation)
		}
	}
	if alias == nil || alias.owner != base || alias.name != "saved_label" || alias.definition == nil || alias.definition.body != c.program.initialLabelMethod {
		c.problem(Span{}, "ruby: proof requires Base alias saved_label snapshotting label v1")
	}
	if remove == nil || remove.owner != beta || remove.name != "label" {
		c.problem(Span{}, "ruby: proof requires Beta remove_method(:label)")
	}
	if undef == nil || undef.owner != gamma || undef.name != "label" {
		c.problem(Span{}, "ruby: proof requires Gamma undef_method(:label)")
	}
	wantVisibility := []lookupVisibility{lookupVisibilityProtected, lookupVisibilityPrivate, lookupVisibilityPublic}
	if len(visibility) != len(wantVisibility) {
		c.problem(Span{}, "ruby: proof requires protected/private/public Base label transitions")
	} else {
		for index, operation := range visibility {
			if operation.owner != base || operation.name != "label" || operation.visibility != wantVisibility[index] {
				c.problem(operation.statement.span, "ruby: proof visibility transition is outside protected/private/public Base label schedule")
			}
		}
	}
	if len(topology) != 2 || topology[0].kind != lookupMutationInclude || topology[0].owner != alpha || topology[0].attachment == nil || topology[0].attachment.module != included ||
		topology[1].kind != lookupMutationPrepend || topology[1].owner != beta || topology[1].attachment == nil || topology[1].attachment.module != prepended {
		c.problem(Span{}, "ruby: proof requires Alpha include IncludedLabel then Beta prepend PrependedLabel")
	}
}

func (c *checker) validateSingletonProofUses() {
	const singletonStatementCount = 12
	statements := c.program.module.statements
	if len(statements) < singletonStatementCount {
		c.problem(Span{}, "ruby: proof requires the straight-line c2 singleton schedule")
		return
	}
	items := statements[len(statements)-singletonStatementCount:]
	wantKinds := [...]statementKind{
		statementMethod,
		statementAssign, statementAssign, statementAssign, statementAssign, statementAssign,
		statementExpression,
		statementAssign, statementAssign, statementAssign, statementAssign, statementAssign,
	}
	wantNames := [...]string{
		"read_singleton",
		"c2_receiver", "c2_peer", "c2_before", "c2_warm", "c2_peer_before",
		"",
		"c2_after", "c2_after_hit", "c2_peer_after", "c2_trace", "c2_result",
	}
	for index := range items {
		if items[index].kind != wantKinds[index] || items[index].name != wantNames[index] {
			c.problem(items[index].span, fmt.Sprintf("ruby: proof c2 statement %d is outside the straight-line singleton schedule", index+1))
			return
		}
	}
	if c.program.singletonAllocation != items[1] || c.program.singletonDefinitionCall == nil || items[6].expression != c.program.singletonDefinitionCall {
		c.problem(items[1].span, "ruby: proof singleton allocation and definition are outside the c2 schedule")
		return
	}

	readMethods := c.program.topMethods["read_singleton"]
	if len(readMethods) != 1 || readMethods[0].declaration != items[0] || len(items[0].parameters) != 1 || items[0].parameters[0] != "receiver" ||
		len(items[0].body) != 1 || !isSingletonReadBody(items[0].body[0]) {
		c.problem(items[0].span, "ruby: proof read_singleton must only send the literal :label selector")
		return
	}
	if !isExactClassNew(items[2].expression, "Alpha", 10) {
		c.problem(items[2].span, "ruby: proof c2_peer must be assigned directly from Alpha.new(10)")
		return
	}

	allowedReceiverUses := map[*expression]bool{c.program.singletonDefinitionCall.receiver: true}
	for _, index := range [...]int{3, 4, 7, 8} {
		receiver := exactTopCallArgument(items[index].expression, "read_singleton", "c2_receiver")
		if receiver == nil {
			c.problem(items[index].span, fmt.Sprintf("ruby: proof %s must read the checked c2_receiver", items[index].name))
			return
		}
		allowedReceiverUses[receiver] = true
	}
	for _, index := range [...]int{5, 9} {
		if exactTopCallArgument(items[index].expression, "read_singleton", "c2_peer") == nil {
			c.problem(items[index].span, fmt.Sprintf("ruby: proof %s must read the ordinary c2_peer", items[index].name))
			return
		}
	}
	receiverTrace := exactSingletonTrace(items[10].expression)
	if receiverTrace == nil {
		c.problem(items[10].span, "ruby: proof c2_trace must combine c2_receiver and c2_peer traces")
		return
	}
	allowedReceiverUses[receiverTrace] = true
	if !isExactLocalArray(items[11].expression, []string{
		"c2_before", "c2_warm", "c2_peer_before", "c2_after", "c2_after_hit", "c2_peer_after", "c2_trace",
	}) {
		c.problem(items[11].span, "ruby: proof c2_result must contain the exact seven scalar observations")
		return
	}

	for expression := range allowedReceiverUses {
		if expression == nil || c.program.bindings[expression] != c.program.singletonReceiverBinding {
			c.problem(items[1].span, "ruby: proof c2_receiver use does not resolve to its exact allocation binding")
			return
		}
	}
	var escaped []*expression
	for expression, binding := range c.program.bindings {
		if binding == c.program.singletonReceiverBinding && !allowedReceiverUses[expression] {
			escaped = append(escaped, expression)
		}
	}
	slices.SortFunc(escaped, func(left, right *expression) int {
		return cmp.Compare(left.span.Start, right.span.Start)
	})
	for _, expression := range escaped {
		c.problem(expression.span, "ruby: proof c2_receiver allocation escapes its admitted singleton uses")
	}
}

func isSingletonReadBody(item *statement) bool {
	if item == nil || item.kind != statementExpression || item.expression == nil {
		return false
	}
	call := item.expression
	return call.kind == expressionCall && call.receiver != nil && call.receiver.kind == expressionLocal && call.receiver.text == "receiver" &&
		call.name == "send" && call.block == nil && len(call.args) == 1 && call.args[0].kind == expressionSymbol && call.args[0].text == "label"
}

func isExactClassNew(value *expression, class string, argument int64) bool {
	return value != nil && value.kind == expressionCall && value.receiver != nil && value.receiver.kind == expressionConstant &&
		value.receiver.text == class && value.name == "new" && value.block == nil && len(value.args) == 1 &&
		value.args[0].kind == expressionInteger && value.args[0].integer == argument
}

func exactTopCallArgument(value *expression, name, argument string) *expression {
	if value == nil || value.kind != expressionCall || value.receiver != nil || value.name != name || value.block != nil ||
		len(value.args) != 1 || value.args[0].kind != expressionLocal || value.args[0].text != argument {
		return nil
	}
	return value.args[0]
}

func exactSingletonTrace(value *expression) *expression {
	if value == nil || value.kind != expressionBinary || value.text != "+" || value.left == nil || value.left.kind != expressionBinary ||
		value.left.text != "*" || value.left.right == nil || value.left.right.kind != expressionInteger || value.left.right.integer != 100 ||
		!isExactReceiverCall(value.right, "c2_peer", "trace") || !isExactReceiverCall(value.left.left, "c2_receiver", "trace") {
		return nil
	}
	return value.left.left.receiver
}

func isExactReceiverCall(value *expression, receiver, name string) bool {
	return value != nil && value.kind == expressionCall && value.receiver != nil && value.receiver.kind == expressionLocal &&
		value.receiver.text == receiver && value.name == name && value.block == nil && len(value.args) == 0
}

func isExactLocalArray(value *expression, names []string) bool {
	if value == nil || value.kind != expressionArray || len(value.items) != len(names) {
		return false
	}
	for index, name := range names {
		if value.items[index].kind != expressionLocal || value.items[index].text != name {
			return false
		}
	}
	return true
}

func (c *checker) buildLookupImage() {
	if len(c.diagnostics) != 0 {
		return
	}
	if len(c.program.lookupOrder) == 0 || len(c.program.lookupOrder) > maximumLookupNodes {
		c.problem(Span{}, fmt.Sprintf("ruby: lookup node count exceeds %d", maximumLookupNodes))
		return
	}

	nodes := make([]lookupNodeDescriptor, len(c.program.lookupOrder))
	var dispatchCount dispatchClassID
	var shapeCount shapeID
	for index, owner := range c.program.lookupOrder {
		if owner == nil || owner.id != classID(index+1) {
			c.problem(Span{}, "ruby: checked lookup owners are not in dense identity order")
			return
		}
		owner.node = lookupNodeID(index + 1)
		if owner.realized {
			dispatchCount++
			owner.dispatch = dispatchCount
			switch owner.kind {
			case checkedOwnerClass:
				shapeCount++
				owner.shape = shapeCount
			case checkedOwnerSingleton:
				if owner.superclass == nil || owner.superclass.kind != checkedOwnerClass || owner.superclass.shape == 0 {
					c.problem(owner.methodsSpan(), "ruby: singleton lookup owner requires a realized ordinary class shape")
					return
				}
				owner.shape = owner.superclass.shape
			default:
				c.problem(owner.methodsSpan(), "ruby: a module cannot own a dispatch row or shape")
				return
			}
		} else if owner.kind == checkedOwnerSingleton {
			c.problem(owner.methodsSpan(), "ruby: singleton lookup owner must own a dispatch row")
			return
		}
		var kind lookupNodeKind
		switch owner.kind {
		case checkedOwnerClass:
			kind = lookupNodeClass
		case checkedOwnerModule:
			kind = lookupNodeModule
		case checkedOwnerSingleton:
			kind = lookupNodeSingleton
		default:
			c.problem(owner.methodsSpan(), "ruby: checked owner has an invalid lookup kind")
			return
		}
		nodes[index] = lookupNodeDescriptor{
			id:         owner.node,
			superclass: lookupNodeIDOf(owner.superclass),
			dispatch:   owner.dispatch,
			kind:       kind,
		}
	}

	checkedAttachments := append([]*checkedAttachment(nil), c.program.attachments...)
	slices.SortStableFunc(checkedAttachments, func(left, right *checkedAttachment) int {
		if order := cmp.Compare(left.target.id, right.target.id); order != 0 {
			return order
		}
		return cmp.Compare(left.ordinal, right.ordinal)
	})
	attachments := make([]lookupAttachmentDescriptor, len(checkedAttachments))
	for index, attachment := range checkedAttachments {
		if attachment.target == nil || attachment.target.kind != checkedOwnerClass || attachment.module == nil || attachment.module.kind != checkedOwnerModule {
			c.problem(Span{}, "ruby: checked attachment has invalid class/module kinds")
			return
		}
		attachment.id = lookupAttachmentID(index + 1)
	}
	attachmentCursor := 0
	for nodeIndex := range nodes {
		node := &nodes[nodeIndex]
		node.attachmentStart = uint32(attachmentCursor)
		for attachmentCursor < len(checkedAttachments) && checkedAttachments[attachmentCursor].target.node == node.id {
			attachment := checkedAttachments[attachmentCursor]
			if attachment.ordinal != uint32(node.attachmentCount) {
				c.problem(Span{}, "ruby: attachment ordinals are not dense per target")
				return
			}
			node.attachmentCount++
			attachments[attachmentCursor] = lookupAttachmentDescriptor{
				id: attachment.id, target: attachment.target.node, module: attachment.module.node,
				ordinal: attachment.ordinal, kind: attachment.kind,
			}
			attachmentCursor++
		}
	}
	if attachmentCursor != len(checkedAttachments) {
		c.problem(Span{}, "ruby: checked attachment targets are outside the owner table")
		return
	}
	c.program.attachments = checkedAttachments

	rows := make([]dispatchRowDescriptor, 0, dispatchCount)
	for _, owner := range c.program.lookupOrder {
		if owner.dispatch == 0 {
			continue
		}
		rows = append(rows, dispatchRowDescriptor{
			id:            owner.dispatch,
			node:          owner.node,
			cellStart:     uint32((int(owner.dispatch) - 1) * len(c.program.selectorNames)),
			selectorCount: uint16(len(c.program.selectorNames)),
		})
	}
	if !c.assignCallLayoutFacts() {
		return
	}

	selectors := make([]selectorDescriptor, len(c.program.selectorNames))
	seenHashes := make(map[uint64]string, len(c.program.selectorNames))
	for index, name := range c.program.selectorNames {
		hash := selectorNameHash(name)
		if previous := seenHashes[hash]; previous != "" {
			c.problem(Span{}, fmt.Sprintf("ruby: selector hash collision between %q and %q", previous, name))
			return
		}
		seenHashes[hash] = name
		selectors[index] = selectorDescriptor{id: selectorID(index + 1), ordinal: uint32(index), nameHash: hash}
	}

	definitions := make([]definitionDescriptor, len(c.program.definitions))
	for index, definition := range c.program.definitions {
		if definition.body == nil || definition.body.definition == 0 {
			c.problem(definition.declaration.span, "ruby: definition body identity is unavailable")
			return
		}
		definitions[index] = definitionDescriptor{
			id:       definition.id,
			owner:    definition.owner.node,
			selector: definition.selector,
			slot:     definition.slot,
			body:     definition.body.definition,
		}
	}
	entryCount, ok := checkedProduct(len(nodes), len(selectors), maximumLocalEntries)
	if !ok {
		c.problem(Span{}, "ruby: lookup entry product exceeds admission limit")
		return
	}
	cellCount, ok := checkedProduct(len(rows), len(selectors), maximumResolutionCells)
	if !ok {
		c.problem(Span{}, "ruby: resolution cell product exceeds admission limit")
		return
	}
	entries := make([]lookupEntry, entryCount)
	mutationEntries := make([]lookupEntry, entryCount)
	mutations := make([]lookupMutationDescriptor, len(c.program.orderedOperations))
	var activations uint64
	for index, operation := range c.program.orderedOperations {
		mutation := lookupMutationDescriptor{
			id: uint32(index + 1), owner: operation.owner.node, kind: operation.kind,
			beforeActivations: activations, afterActivations: activations,
		}
		if operation.attachment != nil {
			if operation.attachment.id == 0 || operation.attachment.id > maximumLookupAttachments {
				c.problem(operation.statement.span, "ruby: topology mutation has an invalid attachment ID")
				return
			}
			bit := uint64(1) << (operation.attachment.id - 1)
			if activations&bit != 0 {
				c.problem(operation.statement.span, "ruby: topology mutation repeats an active attachment")
				return
			}
			activations |= bit
			mutation.attachment = operation.attachment.id
			mutation.afterActivations = activations
			mutations[index] = mutation
			continue
		}
		entryIndex := (int(operation.owner.node)-1)*len(selectors) + int(operation.selector) - 1
		if entryIndex < 0 || entryIndex >= len(mutationEntries) {
			c.problem(operation.statement.span, "ruby: mutation coordinate is outside the checked image")
			return
		}
		before := mutationEntries[entryIndex]
		var after lookupEntry
		switch operation.kind {
		case lookupMutationDefine, lookupMutationAlias:
			if operation.definition == nil || operation.definition.id == 0 || !validLookupVisibility(operation.visibility) {
				c.problem(operation.statement.span, "ruby: defining mutation lacks a definition")
				return
			}
			after = lookupEntry{slot: operation.slot, definition: operation.definition.id, kind: lookupEntryDefined, visibility: operation.visibility}
		case lookupMutationRemove:
			after = lookupEntry{slot: operation.slot, kind: lookupEntryAbsent}
		case lookupMutationUndef:
			after = lookupEntry{slot: operation.slot, kind: lookupEntryUndef}
		case lookupMutationVisibility:
			if before.kind != lookupEntryDefined || !validLookupVisibility(operation.visibility) || before.visibility == operation.visibility {
				c.problem(operation.statement.span, "ruby: visibility mutation has invalid checked entry state")
				return
			}
			after = before
			after.visibility = operation.visibility
		default:
			c.problem(operation.statement.span, "ruby: unknown checked mutation kind")
			return
		}
		mutation.selector = operation.selector
		mutation.before = before
		mutation.after = after
		mutations[index] = mutation
		mutationEntries[entryIndex] = after
	}

	// The frozen C1 proof owns exactly two attachment bits. Seal all four masks,
	// including schedule-unreachable prepend-only mask 10, so the runtime route
	// builder is checked independently of the mutation schedule. Do not turn
	// this bounded proof into a generic exponential topology enumerator.
	if len(checkedAttachments) != 2 {
		c.problem(Span{}, "ruby: complete C1 route oracle requires exactly two attachments")
		return
	}
	activationMasks := [...]uint64{0b00, 0b01, 0b10, 0b11}
	if _, ok := checkedProduct(len(activationMasks), len(rows), maximumLookupRouteOracles); !ok {
		c.problem(Span{}, "ruby: lookup route-oracle product exceeds admission limit")
		return
	}
	routeOracles := make([]lookupRouteOracle, 0, len(activationMasks)*len(rows))
	oraclePaths := make([]lookupNodeID, 0, len(activationMasks)*len(rows)*3)
	for _, mask := range activationMasks {
		for _, row := range rows {
			class := c.program.lookupOrder[row.node-1]
			path, ok := checkedLookupRoute(class, checkedAttachments, mask)
			if !ok || len(path) == 0 || len(path) > maximumLookupDepth || len(oraclePaths)+len(path) > maximumLookupPathSlots {
				c.problem(class.methodsSpan(), "ruby: checked lookup route exceeds its admission bounds")
				return
			}
			routeOracles = append(routeOracles, lookupRouteOracle{
				activations: mask, row: row.id, pathStart: uint32(len(oraclePaths)), pathLength: uint16(len(path)),
			})
			oraclePaths = append(oraclePaths, path...)
		}
	}
	receipts := make([]lookupReceipt, cellCount)
	for index := range receipts {
		receipts[index].outcome = lookupMissing
	}
	image := sealLookupImage(c.program.identity, nodes, rows, selectors, definitions, attachments, mutations, routeOracles, oraclePaths, entries, receipts)
	if err := validateLookupImage(&image); err != nil {
		c.problem(Span{}, fmt.Sprintf("ruby: seal lookup facts: %v", err))
		return
	}
	c.program.lookup = &image
}

func (c *checker) assignCallLayoutFacts() bool {
	var singletonAllocation, singletonDefinition bool
	for expression, fact := range c.program.calls {
		switch fact.kind {
		case checkedCallNew:
			if fact.class == 0 || int(fact.class) > len(c.program.lookupOrder) {
				c.problem(expression.span, "ruby: object allocation has an invalid checked class")
				return false
			}
			owner := c.program.lookupOrder[fact.class-1]
			if owner.kind != checkedOwnerClass || owner.dispatch == 0 || owner.shape == 0 {
				c.problem(expression.span, "ruby: object allocation class has no checked dispatch or shape")
				return false
			}
			fact.dispatch = owner.dispatch
			fact.shape = owner.shape
			if expression == c.program.singletonAllocationCall {
				if singletonAllocation {
					c.problem(expression.span, "ruby: singleton allocation has more than one checked call fact")
					return false
				}
				singleton := c.program.singletonOwner
				if singleton == nil || singleton.superclass != owner || singleton.dispatch == 0 || singleton.shape != owner.shape {
					c.problem(expression.span, "ruby: singleton allocation has invalid checked dispatch or shape")
					return false
				}
				fact.dispatch = singleton.dispatch
				fact.shape = singleton.shape
				singletonAllocation = true
			}
			c.program.calls[expression] = fact
		case checkedCallDefineSingleton:
			if singletonDefinition {
				c.problem(expression.span, "ruby: singleton definition has more than one checked call fact")
				return false
			}
			singleton := c.program.singletonOwner
			if expression != c.program.singletonDefinitionCall || singleton == nil || singleton.dispatch == 0 || singleton.shape == 0 ||
				fact.selector == 0 || fact.mutation == 0 {
				c.problem(expression.span, "ruby: singleton definition has invalid checked dispatch or shape")
				return false
			}
			fact.dispatch = singleton.dispatch
			fact.shape = singleton.shape
			c.program.calls[expression] = fact
			singletonDefinition = true
		}
	}
	if !singletonAllocation || !singletonDefinition {
		c.problem(Span{}, "ruby: singleton allocation or definition call fact is missing")
		return false
	}
	return true
}

func checkedLookupRoute(class *checkedClass, attachments []*checkedAttachment, activations uint64) ([]lookupNodeID, bool) {
	if class == nil || class.kind != checkedOwnerClass && class.kind != checkedOwnerSingleton {
		return nil, false
	}
	path := make([]lookupNodeID, 0, maximumLookupDepth)
	for current := class; current != nil; current = current.superclass {
		start := len(attachments)
		for start > 0 && attachments[start-1].target.id > current.id {
			start--
		}
		for start > 0 && attachments[start-1].target == current {
			start--
		}
		end := start
		for end < len(attachments) && attachments[end].target == current {
			end++
		}
		for index := end - 1; index >= start; index-- {
			attachment := attachments[index]
			if attachment.kind == lookupAttachmentPrepend && activations&(uint64(1)<<(attachment.id-1)) != 0 {
				path = append(path, attachment.module.node)
			}
		}
		path = append(path, current.node)
		for index := end - 1; index >= start; index-- {
			attachment := attachments[index]
			if attachment.kind == lookupAttachmentInclude && activations&(uint64(1)<<(attachment.id-1)) != 0 {
				path = append(path, attachment.module.node)
			}
		}
		if len(path) > maximumLookupDepth {
			return nil, false
		}
	}
	return path, true
}

func lookupNodeIDOf(class *checkedClass) lookupNodeID {
	if class == nil {
		return 0
	}
	return lookupNodeID(class.id)
}

func (class *checkedClass) methodsSpan() Span {
	return class.declarationSpan
}

func (c *checker) newBinding() bindingID {
	c.nextBinding++
	return c.nextBinding
}

func (c *checker) problem(span Span, message string) {
	if len(c.diagnostics) < maximumDiagnostics {
		c.diagnostics = append(c.diagnostics, Diagnostic{Span: span, Message: message})
	}
}

func rubyProgramIdentity(source string) [sha256.Size]byte {
	hash := sha256.New()
	hash.Write([]byte("github.com/besmpl/ember/internal/rubyproof:program:v4\x00"))
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(source)))
	hash.Write(size[:])
	hash.Write([]byte(source))
	var out [sha256.Size]byte
	copy(out[:], hash.Sum(nil))
	return out
}
