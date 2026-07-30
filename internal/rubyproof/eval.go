package rubyproof

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
)

type rubyKind uint8

const (
	rubyNil rubyKind = iota
	rubyInteger
	rubyBoolean
	rubyString
	rubySymbol
	rubyObjectValue
	rubyClassValue
	rubyModuleValue
	rubyArray
)

type rubyValue struct {
	kind    rubyKind
	integer int64
	boolean bool
	text    string
	object  *rubyObject
	class   *checkedClass
	items   []rubyValue
}

type rubyObject struct {
	id       uint64
	class    *checkedClass
	dispatch dispatchClassID
	shape    shapeID
	fields   map[string]rubyValue
}

type rubyBlock struct {
	syntax       *block
	environment  map[bindingID]rubyValue
	self         *rubyObject
	returnTarget uint64
}

type flowKind uint8

const (
	flowNormal flowKind = iota
	flowReturn
	flowRaise
)

type rubyException struct {
	class, message string
}

type outcome struct {
	value     rubyValue
	flow      flowKind
	target    uint64
	exception rubyException
}

type evalContext struct {
	self         *rubyObject
	environment  map[bindingID]rubyValue
	block        *rubyBlock
	returnTarget uint64
}

type executionControl struct {
	ctx              context.Context
	remainingSteps   uint64
	maximumFrames    uint64
	remainingObjects uint64
	frameDepth       uint64
}

type runtimeState struct {
	lookup       *lookupOwner
	topMethods   []*checkedMethod
	nextObjectID uint64
	nextFrameID  uint64
	control      executionControl
	top          map[bindingID]rubyValue
}

// Runtime is one mutable Ruby execution owner. It admits one active operation;
// interruption poisons it because partial Ruby state is deliberately not
// promised reusable by this proof.
type Runtime struct {
	program  *checkedProgram
	active   atomic.Bool
	closed   atomic.Bool
	poisoned atomic.Bool
	state    runtimeState
}

// RubyError is an uncaught guest RuntimeError. It is distinct from host
// cancellation, resource exhaustion, and internal implementation failures.
type RubyError struct {
	Class   string
	Message string
}

func (e *RubyError) Error() string {
	return fmt.Sprintf("ruby: uncaught %s: %s", e.Class, e.Message)
}

type internalRuntimeError struct{ message string }

func (e *internalRuntimeError) Error() string { return "ruby: internal runtime failure: " + e.message }

func newRuntime(program *checkedProgram) *Runtime { return &Runtime{program: program} }

// Run evaluates the complete checked program and detaches its result. Host
// cancellation and limits escape Ruby rescue/ensure and poison this owner.
func (r *Runtime) Run(ctx context.Context, limits Limits) (Result, error) {
	if r == nil || r.program == nil {
		return Result{}, errors.New("ruby: zero runtime")
	}
	if ctx == nil {
		return Result{}, errors.New("ruby: nil context")
	}
	if r.closed.Load() {
		return Result{}, ErrClosed
	}
	if r.poisoned.Load() {
		return Result{}, ErrPoisoned
	}
	if !r.active.CompareAndSwap(false, true) {
		return Result{}, ErrBusy
	}
	defer r.active.Store(false)
	if r.closed.Load() {
		return Result{}, ErrClosed
	}
	if r.poisoned.Load() {
		return Result{}, ErrPoisoned
	}
	if err := ctx.Err(); err != nil {
		r.poisoned.Store(true)
		return Result{}, err
	}

	lookup, err := newLookupOwner(r.program.lookup)
	if err != nil {
		r.poisoned.Store(true)
		return Result{}, &internalRuntimeError{message: fmt.Sprintf("construct lookup owner: %v", err)}
	}
	r.state.close()
	r.state = runtimeState{
		lookup:     lookup,
		topMethods: make([]*checkedMethod, len(r.program.topMethodNames)),
		top:        make(map[bindingID]rubyValue),
		control: executionControl{
			ctx:              ctx,
			remainingSteps:   limits.Steps,
			maximumFrames:    limits.Frames,
			remainingObjects: limits.Objects,
		},
	}
	result, err := r.evalStatements(r.program.module.statements, &evalContext{environment: r.state.top})
	if err != nil {
		r.poisoned.Store(true)
		return Result{}, err
	}
	switch result.flow {
	case flowRaise:
		return Result{}, &RubyError{Class: result.exception.class, Message: result.exception.message}
	case flowReturn:
		r.poisoned.Store(true)
		return Result{}, &internalRuntimeError{message: "return escaped its defining method"}
	}
	value, ok := r.state.top[r.program.finalResultBinding]
	if !ok {
		r.poisoned.Store(true)
		return Result{}, &internalRuntimeError{message: "checked result binding was not assigned"}
	}
	topology, ok := r.state.top[r.program.topologyResultBinding]
	if !ok {
		r.poisoned.Store(true)
		return Result{}, &internalRuntimeError{message: "checked topology result binding was not assigned"}
	}
	singleton, ok := r.state.top[r.program.singletonResultBinding]
	if !ok {
		r.poisoned.Store(true)
		return Result{}, &internalRuntimeError{message: "checked singleton result binding was not assigned"}
	}
	detached, err := detachResult(value, topology, singleton)
	if err != nil {
		r.poisoned.Store(true)
		return Result{}, err
	}
	return detached, nil
}

// Close is idempotent. It rejects concurrent close rather than racing active
// Ruby state teardown.
func (r *Runtime) Close() error {
	if r == nil || r.closed.Load() {
		return nil
	}
	if !r.active.CompareAndSwap(false, true) {
		return ErrBusy
	}
	defer r.active.Store(false)
	if r.closed.Swap(true) {
		return nil
	}
	r.state.close()
	return nil
}

func (state *runtimeState) close() {
	if state.lookup != nil {
		state.lookup.close()
	}
	*state = runtimeState{}
}

func (r *Runtime) evalStatements(statements []*statement, scope *evalContext) (outcome, error) {
	result := outcome{value: rubyValue{kind: rubyNil}}
	for _, item := range statements {
		if err := r.state.control.step(); err != nil {
			return outcome{}, err
		}
		current, err := r.evalStatement(item, scope)
		if err != nil {
			return outcome{}, err
		}
		result = current
		if current.flow != flowNormal {
			return current, nil
		}
	}
	return result, nil
}

func (r *Runtime) evalStatement(item *statement, scope *evalContext) (outcome, error) {
	switch item.kind {
	case statementClass, statementModule:
		for _, member := range item.body {
			operation := r.program.classOperations[member]
			if operation == nil {
				return outcome{}, &internalRuntimeError{message: "class member has no checked mutation fact"}
			}
			mutation, err := r.program.lookup.mutation(operation.id)
			if err != nil {
				return outcome{}, &internalRuntimeError{message: err.Error()}
			}
			if err := r.state.lookup.apply(r.state.control.ctx, mutation); err != nil {
				return outcome{}, err
			}
		}
		return normal(rubyValue{kind: rubyNil}), nil
	case statementMethod:
		method := r.program.methods[item]
		if method.topID == 0 || int(method.topID) > len(r.state.topMethods) {
			return outcome{}, &internalRuntimeError{message: "invalid checked top-method ID"}
		}
		r.state.topMethods[method.topID-1] = method
		return normal(rubyValue{kind: rubyNil}), nil
	case statementAssign:
		value, err := r.evalExpression(item.expression, scope)
		if err != nil || value.flow != flowNormal {
			return value, err
		}
		if item.name[0] == '@' {
			if scope.self == nil {
				return outcome{}, &internalRuntimeError{message: "instance assignment without self"}
			}
			scope.self.fields[item.name] = value.value
		} else {
			scope.environment[r.program.assignments[item]] = value.value
		}
		return value, nil
	case statementExpression:
		return r.evalExpression(item.expression, scope)
	case statementReturn:
		value, err := r.evalExpression(item.expression, scope)
		if err != nil || value.flow != flowNormal {
			return value, err
		}
		return outcome{value: value.value, flow: flowReturn, target: scope.returnTarget}, nil
	case statementRaise:
		value, err := r.evalExpression(item.expression, scope)
		if err != nil || value.flow != flowNormal {
			return value, err
		}
		if value.value.kind != rubyString {
			return outcome{}, &internalRuntimeError{message: "raise payload is not a string"}
		}
		return outcome{flow: flowRaise, exception: rubyException{class: "RuntimeError", message: value.value.text}}, nil
	case statementBegin:
		return r.evalBegin(item, scope)
	default:
		return outcome{}, &internalRuntimeError{message: "unknown checked statement"}
	}
}

func (r *Runtime) evalBegin(item *statement, scope *evalContext) (outcome, error) {
	result, err := r.evalStatements(item.body, scope)
	if err != nil {
		// Host aborts deliberately bypass guest ensure.
		return outcome{}, err
	}
	if result.flow == flowRaise && item.rescueClass == result.exception.class {
		result, err = r.evalStatements(item.rescueBody, scope)
		if err != nil {
			return outcome{}, err
		}
	}
	if len(item.ensureBody) != 0 {
		ensured, err := r.evalStatements(item.ensureBody, scope)
		if err != nil {
			return outcome{}, err
		}
		if ensured.flow != flowNormal {
			return ensured, nil
		}
	}
	return result, nil
}

func (r *Runtime) evalExpression(expression *expression, scope *evalContext) (outcome, error) {
	if err := r.state.control.step(); err != nil {
		return outcome{}, err
	}
	switch expression.kind {
	case expressionInteger:
		return normal(rubyValue{kind: rubyInteger, integer: expression.integer}), nil
	case expressionString:
		return normal(rubyValue{kind: rubyString, text: expression.text}), nil
	case expressionSymbol:
		return normal(rubyValue{kind: rubySymbol, text: expression.text}), nil
	case expressionLocal:
		value, ok := scope.environment[r.program.bindings[expression]]
		if !ok {
			return outcome{}, &internalRuntimeError{message: fmt.Sprintf("checked local %q is unassigned", expression.text)}
		}
		return normal(value), nil
	case expressionInstanceVariable:
		if scope.self == nil {
			return outcome{}, &internalRuntimeError{message: "instance read without self"}
		}
		value, ok := scope.self.fields[expression.text]
		if !ok {
			return normal(rubyValue{kind: rubyNil}), nil
		}
		return normal(value), nil
	case expressionConstant:
		class := r.program.classes[expression.text]
		if class == nil {
			return outcome{}, &internalRuntimeError{message: fmt.Sprintf("checked constant %q is unavailable", expression.text)}
		}
		kind := rubyClassValue
		if class.kind == checkedOwnerModule {
			kind = rubyModuleValue
		}
		return normal(rubyValue{kind: kind, class: class}), nil
	case expressionArray:
		items := make([]rubyValue, 0, len(expression.items))
		for _, item := range expression.items {
			value, err := r.evalExpression(item, scope)
			if err != nil || value.flow != flowNormal {
				return value, err
			}
			items = append(items, value.value)
		}
		return normal(rubyValue{kind: rubyArray, items: items}), nil
	case expressionBinary:
		left, err := r.evalExpression(expression.left, scope)
		if err != nil || left.flow != flowNormal {
			return left, err
		}
		right, err := r.evalExpression(expression.right, scope)
		if err != nil || right.flow != flowNormal {
			return right, err
		}
		if left.value.kind != rubyInteger || right.value.kind != rubyInteger {
			return outcome{}, &internalRuntimeError{message: "proof arithmetic requires integers"}
		}
		switch expression.text {
		case "+":
			return normal(rubyValue{kind: rubyInteger, integer: left.value.integer + right.value.integer}), nil
		case "*":
			return normal(rubyValue{kind: rubyInteger, integer: left.value.integer * right.value.integer}), nil
		default:
			return outcome{}, &internalRuntimeError{message: "unknown checked arithmetic operator"}
		}
	case expressionYield:
		if scope.block == nil {
			return outcome{flow: flowRaise, exception: rubyException{class: "LocalJumpError", message: "no block given"}}, nil
		}
		argument, err := r.evalExpression(expression.args[0], scope)
		if err != nil || argument.flow != flowNormal {
			return argument, err
		}
		return r.invokeBlock(scope.block, []rubyValue{argument.value})
	case expressionCall:
		return r.evalCall(expression, scope)
	default:
		return outcome{}, &internalRuntimeError{message: "unknown checked expression"}
	}
}

func (r *Runtime) evalCall(call *expression, scope *evalContext) (outcome, error) {
	fact, ok := r.program.calls[call]
	if !ok {
		return outcome{}, &internalRuntimeError{message: fmt.Sprintf("checked call %q has no call fact", call.name)}
	}
	var receiver rubyValue
	if call.receiver != nil {
		value, err := r.evalExpression(call.receiver, scope)
		if err != nil || value.flow != flowNormal {
			return value, err
		}
		receiver = value.value
	}
	arguments := make([]rubyValue, 0, len(call.args))
	for _, raw := range call.args {
		value, err := r.evalExpression(raw, scope)
		if err != nil || value.flow != flowNormal {
			return value, err
		}
		arguments = append(arguments, value.value)
	}
	var blockValue *rubyBlock
	if call.block != nil {
		blockValue = &rubyBlock{syntax: call.block, environment: scope.environment, self: scope.self, returnTarget: scope.returnTarget}
	}
	if int(fact.argumentOffset) > len(arguments) {
		return outcome{}, &internalRuntimeError{message: "checked call argument offset is invalid"}
	}
	methodArguments := arguments[fact.argumentOffset:]
	if call.receiver == nil {
		switch fact.kind {
		case checkedCallReceiver:
			if scope.self == nil {
				return outcome{}, &internalRuntimeError{message: fmt.Sprintf("implicit receiver method %q has no self", call.name)}
			}
			return r.send(scope.self, fact.selector, methodArguments, blockValue, fact.mode)
		case checkedCallTop:
			if fact.top == 0 || int(fact.top) > len(r.state.topMethods) || r.state.topMethods[fact.top-1] == nil {
				return outcome{}, &internalRuntimeError{message: fmt.Sprintf("checked top method %q is not installed", call.name)}
			}
			return r.invoke(r.state.topMethods[fact.top-1], nil, methodArguments, blockValue)
		default:
			return outcome{}, &internalRuntimeError{message: "invalid implicit call fact"}
		}
	}
	if fact.kind == checkedCallNew {
		if receiver.kind != rubyClassValue {
			return outcome{}, &internalRuntimeError{message: "new receiver is not a class"}
		}
		if receiver.class.id != fact.class {
			return outcome{}, &internalRuntimeError{message: "new receiver class differs from checked class"}
		}
		object, err := r.allocateObject(receiver.class, fact.dispatch, fact.shape)
		if err != nil {
			return outcome{}, err
		}
		if fact.selector != 0 {
			initialized, err := r.send(object, fact.selector, methodArguments, nil, checkedCallAnyVisibility)
			if err != nil || initialized.flow != flowNormal {
				return initialized, err
			}
		}
		return normal(rubyValue{kind: rubyObjectValue, object: object}), nil
	}
	if fact.kind == checkedCallEqual {
		if receiver.kind != rubyObjectValue {
			return outcome{}, &internalRuntimeError{message: "proof equal? receiver is not an object"}
		}
		other := arguments[0]
		return normal(rubyValue{kind: rubyBoolean, boolean: other.kind == rubyObjectValue && receiver.object.id == other.object.id}), nil
	}
	if receiver.kind != rubyObjectValue {
		return outcome{}, &internalRuntimeError{message: fmt.Sprintf("method %q receiver is not an object", call.name)}
	}
	if fact.kind == checkedCallDefineSingleton {
		if receiver.object == nil || receiver.object.dispatch != fact.dispatch || receiver.object.shape != fact.shape || fact.mutation == 0 ||
			fact.selector == 0 || int(fact.selector) > len(r.program.selectorNames) || fact.mode != checkedCallPublicVisibility ||
			len(arguments) != 1 || arguments[0].kind != rubySymbol || arguments[0].text != r.program.selectorNames[fact.selector-1] ||
			len(methodArguments) != 0 || blockValue == nil || len(call.block.parameters) != 0 ||
			receiver.object.class == nil || !r.program.lookup.validObjectBinding(
			receiver.object.class.node, receiver.object.class.dispatch, receiver.object.class.shape, receiver.object.dispatch, receiver.object.shape,
		) {
			return outcome{}, &internalRuntimeError{message: "singleton definition differs from checked allocation and body"}
		}
		mutation, err := r.program.lookup.mutation(fact.mutation)
		if err != nil || mutation.selector != fact.selector || mutation.kind != lookupMutationDefine ||
			mutation.owner == 0 || int(mutation.owner) > len(r.program.lookup.nodes) ||
			r.program.lookup.nodes[mutation.owner-1].kind != lookupNodeSingleton ||
			r.program.lookup.nodes[mutation.owner-1].dispatch != fact.dispatch {
			return outcome{}, &internalRuntimeError{message: "singleton definition has invalid checked mutation facts"}
		}
		if err := r.state.lookup.apply(r.state.control.ctx, mutation); err != nil {
			return outcome{}, err
		}
		return normal(rubyValue{kind: rubySymbol, text: r.program.selectorNames[fact.selector-1]}), nil
	}
	if fact.kind != checkedCallReceiver {
		return outcome{}, &internalRuntimeError{message: "invalid receiver call fact"}
	}
	return r.send(receiver.object, fact.selector, methodArguments, blockValue, fact.mode)
}

func (r *Runtime) send(receiver *rubyObject, selector selectorID, arguments []rubyValue, block *rubyBlock, mode checkedCallMode) (outcome, error) {
	if receiver == nil || receiver.class == nil || receiver.dispatch == 0 || receiver.shape == 0 ||
		!r.program.lookup.validObjectBinding(receiver.class.node, receiver.class.dispatch, receiver.class.shape, receiver.dispatch, receiver.shape) {
		return outcome{}, &internalRuntimeError{message: "receiver has no checked dispatch class"}
	}
	cell, err := r.state.lookup.effectiveCell(receiver.dispatch, selector)
	if err != nil {
		return outcome{}, &internalRuntimeError{message: err.Error()}
	}
	receipt := cell.receipt
	if receipt.outcome != lookupResolved || mode == checkedCallPublicVisibility && receipt.visibility != lookupVisibilityPublic {
		name := "unknown"
		if selector != 0 && int(selector) <= len(r.program.selectorNames) {
			name = r.program.selectorNames[selector-1]
		}
		return outcome{flow: flowRaise, exception: rubyException{class: "NoMethodError", message: name}}, nil
	}
	if mode != checkedCallAnyVisibility && mode != checkedCallPublicVisibility {
		return outcome{}, &internalRuntimeError{message: "lookup call has invalid visibility mode"}
	}
	if receipt.definition == 0 || int(receipt.definition) > len(r.program.methodsByDefinition) {
		return outcome{}, &internalRuntimeError{message: "lookup selected an invalid definition"}
	}
	return r.invoke(r.program.methodsByDefinition[receipt.definition-1], receiver, arguments, block)
}

func (r *Runtime) invoke(method *checkedMethod, self *rubyObject, arguments []rubyValue, block *rubyBlock) (outcome, error) {
	frame, err := r.state.control.enterFrame(&r.state.nextFrameID)
	if err != nil {
		return outcome{}, err
	}
	defer r.state.control.leaveFrame()
	environment := make(map[bindingID]rubyValue, len(method.parameterBindings)+4)
	for index, binding := range method.parameterBindings {
		environment[binding] = arguments[index]
	}
	result, err := r.evalStatements(method.declaration.body, &evalContext{self: self, environment: environment, block: block, returnTarget: frame})
	if err != nil {
		return outcome{}, err
	}
	if result.flow == flowReturn && result.target == frame {
		return normal(result.value), nil
	}
	return result, nil
}

func (r *Runtime) invokeBlock(block *rubyBlock, arguments []rubyValue) (outcome, error) {
	_, err := r.state.control.enterFrame(&r.state.nextFrameID)
	if err != nil {
		return outcome{}, err
	}
	defer r.state.control.leaveFrame()
	bindings := r.program.blockBindings[block.syntax]
	for index, binding := range bindings {
		block.environment[binding] = arguments[index]
	}
	return r.evalStatements(block.syntax.body, &evalContext{self: block.self, environment: block.environment, returnTarget: block.returnTarget})
}

func (r *Runtime) allocateObject(class *checkedClass, dispatch dispatchClassID, shape shapeID) (*rubyObject, error) {
	if class == nil || !r.program.lookup.validObjectBinding(class.node, class.dispatch, class.shape, dispatch, shape) {
		return nil, &internalRuntimeError{message: "object allocation has invalid checked dispatch or shape"}
	}
	if err := r.state.control.poll(); err != nil {
		return nil, err
	}
	if r.state.control.remainingObjects == 0 {
		return nil, ErrObjectLimit
	}
	r.state.control.remainingObjects--
	r.state.nextObjectID++
	return &rubyObject{id: r.state.nextObjectID, class: class, dispatch: dispatch, shape: shape, fields: make(map[string]rubyValue, 2)}, nil
}

func (c *executionControl) poll() error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (c *executionControl) step() error {
	if err := c.poll(); err != nil {
		return err
	}
	if c.remainingSteps == 0 {
		return ErrStepLimit
	}
	c.remainingSteps--
	return nil
}

func (c *executionControl) enterFrame(next *uint64) (uint64, error) {
	if err := c.poll(); err != nil {
		return 0, err
	}
	if c.frameDepth >= c.maximumFrames {
		return 0, ErrFrameLimit
	}
	c.frameDepth++
	*next = *next + 1
	return *next, nil
}

func (c *executionControl) leaveFrame() { c.frameDepth-- }

func normal(value rubyValue) outcome { return outcome{value: value} }

func detachResult(value, topology, singleton rubyValue) (Result, error) {
	if value.kind != rubyArray || len(value.items) != 24 {
		return Result{}, &internalRuntimeError{message: "result is not the checked twenty-four-element array"}
	}
	if value.items[0].kind != rubyBoolean || value.items[1].kind != rubyBoolean {
		return Result{}, &internalRuntimeError{message: "result identity fields are not booleans"}
	}
	for index := 2; index < len(value.items); index++ {
		if value.items[index].kind != rubyInteger {
			return Result{}, &internalRuntimeError{message: "result numeric field is not an integer"}
		}
	}
	topologyResult, err := detachTopologyResult(topology)
	if err != nil {
		return Result{}, err
	}
	singletonResult, err := detachSingletonResult(singleton)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Same:              value.items[0].boolean,
		Other:             value.items[1].boolean,
		Before:            value.items[2].integer,
		BetaBefore:        value.items[3].integer,
		GammaBefore:       value.items[4].integer,
		AlphaWarm:         value.items[5].integer,
		BetaWarm:          value.items[6].integer,
		After:             value.items[7].integer,
		AlphaAfterHit:     value.items[8].integer,
		BetaAfter:         value.items[9].integer,
		GammaAfter:        value.items[10].integer,
		Saved:             value.items[11].integer,
		PublicBefore:      value.items[12].integer,
		ProtectedRejected: value.items[13].integer,
		PrivateRejected:   value.items[14].integer,
		PrivateSent:       value.items[15].integer,
		PrivateBeta:       value.items[16].integer,
		PublicRestored:    value.items[17].integer,
		BetaRemoved:       value.items[18].integer,
		Returned:          value.items[19].integer,
		Value:             value.items[20].integer,
		Trace:             value.items[21].integer,
		BetaTrace:         value.items[22].integer,
		GammaTrace:        value.items[23].integer,
		Topology:          topologyResult,
		Singleton:         singletonResult,
	}, nil
}

func detachTopologyResult(value rubyValue) (TopologyResult, error) {
	if value.kind != rubyArray || len(value.items) != 7 {
		return TopologyResult{}, &internalRuntimeError{message: "topology result is not the checked seven-element array"}
	}
	for _, item := range value.items {
		if item.kind != rubyInteger {
			return TopologyResult{}, &internalRuntimeError{message: "topology result field is not an integer"}
		}
	}
	return TopologyResult{
		DeltaBefore:    value.items[0].integer,
		DeltaIncluded:  value.items[1].integer,
		BetaRoute:      value.items[2].integer,
		BetaDefined:    value.items[3].integer,
		DeltaRedefined: value.items[4].integer,
		SavedStable:    value.items[5].integer,
		Trace:          value.items[6].integer,
	}, nil
}

func detachSingletonResult(value rubyValue) (SingletonResult, error) {
	if value.kind != rubyArray || len(value.items) != 7 {
		return SingletonResult{}, &internalRuntimeError{message: "singleton result is not the checked seven-element array"}
	}
	for _, item := range value.items {
		if item.kind != rubyInteger {
			return SingletonResult{}, &internalRuntimeError{message: "singleton result field is not an integer"}
		}
	}
	return SingletonResult{
		Before:     value.items[0].integer,
		Warm:       value.items[1].integer,
		PeerBefore: value.items[2].integer,
		After:      value.items[3].integer,
		AfterHit:   value.items[4].integer,
		PeerAfter:  value.items[5].integer,
		Trace:      value.items[6].integer,
	}, nil
}
