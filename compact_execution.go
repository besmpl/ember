package ember

import (
	"math"
	"sort"
	"sync"
)

// compactProgram is an immutable, compiler-proven numeric call graph. The
// canonical wordcode remains the executable stream; this sidecar retains only
// direct call targets and their compact transport facts.
type compactProgram struct {
	functions []compactFunction
	entry     uint16
}

type compactFunction struct {
	proto       *Proto
	callSites   []compactCallSite
	selfUpvalue int16
}

type compactCallSite struct {
	wordPC        uint32
	target        uint16
	argumentStart uint16
	result        uint16
	flags         uint16
}

const compactCallBorrow uint16 = 1

// compactCallFrame is the complete caller continuation for the numeric call
// graph engine. It is pointer-free, so recursion does not produce Go write
// barriers or retain closures, cells, frames, or register slices.
type compactCallFrame struct {
	returnPC       uint32
	callerBase     uint32
	callerTop      uint32
	resultBase     uint32
	callerFunction uint16
	_              uint16
}

// compactExecutionState is isolated from the leaf scalar runner so adding call
// transport does not tax already-fast no-call programs. Both buffers contain
// scalar data only and can be logically reset without pointer clearing.
type compactExecutionState struct {
	stack  []float64
	frames []compactCallFrame
}

var compactExecutionPool = sync.Pool{
	New: func() any { return &compactExecutionState{} },
}

func acquireCompactExecutionState() *compactExecutionState {
	return compactExecutionPool.Get().(*compactExecutionState)
}

func releaseCompactExecutionState(state *compactExecutionState) {
	if state == nil {
		return
	}
	if cap(state.stack) > maxPooledSlotExecutionCapacity {
		state.stack = nil
	} else {
		state.stack = state.stack[:0]
	}
	if cap(state.frames) > maxPooledSlotExecutionCapacity {
		state.frames = nil
	} else {
		state.frames = state.frames[:0]
	}
	compactExecutionPool.Put(state)
}

func (state *compactExecutionState) prepareStack(count int) []float64 {
	if cap(state.stack) < count {
		state.stack = make([]float64, count)
	} else {
		state.stack = state.stack[:count]
	}
	state.frames = state.frames[:0]
	return state.stack
}

const (
	compactFactUnknown int32 = -1
	compactFactNumeric int32 = -2
)

type compactBuildFunction struct {
	proto       *Proto
	code        []instruction
	boundaries  []int
	selfUpvalue int16
	callSites   []compactCallSite
}

// buildCompactProgram proves a closed, side-effect-free numeric call graph.
// This first slice admits fixed-arity functions with one numeric result,
// nonescaping direct child functions, and direct self recursion. Any uncertain
// target or semantic feature rejects the complete graph, preserving safe
// whole-run fallback to the established VM.
func buildCompactProgram(root *Proto, rootCode []instruction) *compactProgram {
	if root == nil || len(rootCode) == 0 || root.variadic || len(root.upvalues) != 0 || len(root.prototypes) == 0 {
		return nil
	}
	hasCall := false
	for _, ins := range rootCode {
		switch ins.op {
		case opCall, opCallOne, opCallLocalOne, opCallUpvalueOne:
			hasCall = true
		}
	}
	if !hasCall {
		return nil
	}

	builders := make([]compactBuildFunction, 0, 1+len(root.prototypes))
	ids := make(map[*Proto]uint16)
	var collect func(*Proto, []instruction) bool
	collect = func(proto *Proto, supplied []instruction) bool {
		if proto == nil || len(builders) >= int(^uint16(0)) {
			return false
		}
		if _, duplicate := ids[proto]; duplicate {
			return false
		}
		code := supplied
		if code == nil {
			var err error
			code, err = protoDecodedInstructions(proto)
			if err != nil {
				return false
			}
		}
		boundaries, err := wordcodeBoundaries(code)
		if err != nil || len(boundaries) != len(code)+1 {
			return false
		}
		id := uint16(len(builders))
		ids[proto] = id
		builders = append(builders, compactBuildFunction{
			proto:       proto,
			code:        code,
			boundaries:  boundaries,
			selfUpvalue: -1,
		})
		for _, child := range proto.prototypes {
			if !collect(child, nil) {
				return false
			}
		}
		return true
	}
	if !collect(root, rootCode) {
		return nil
	}

	// A mutable self-capture is the only upvalue admitted in this slice. The
	// compact engine resolves it to the current function id and never creates
	// the closure cycle or cell used by the general VM.
	for parentIndex := range builders {
		parent := &builders[parentIndex]
		for _, ins := range parent.code {
			if ins.op != opClosure || ins.b < 0 || ins.b >= len(parent.proto.prototypes) {
				continue
			}
			child := parent.proto.prototypes[ins.b]
			childID, ok := ids[child]
			if !ok {
				return nil
			}
			childBuild := &builders[int(childID)]
			if len(child.upvalues) == 0 {
				continue
			}
			if len(child.upvalues) != 1 {
				return nil
			}
			desc := child.upvalues[0]
			if !desc.local || desc.copy || desc.index != ins.a {
				return nil
			}
			if childBuild.selfUpvalue >= 0 && childBuild.selfUpvalue != 0 {
				return nil
			}
			childBuild.selfUpvalue = 0
		}
	}

	for index := range builders {
		if !analyzeCompactFunction(uint16(index), builders, ids) {
			return nil
		}
	}

	functions := make([]compactFunction, len(builders))
	for index := range builders {
		builder := &builders[index]
		functions[index] = compactFunction{
			proto:       builder.proto,
			callSites:   builder.callSites,
			selfUpvalue: builder.selfUpvalue,
		}
	}
	return &compactProgram{functions: functions, entry: ids[root]}
}

func analyzeCompactFunction(functionID uint16, builders []compactBuildFunction, ids map[*Proto]uint16) bool {
	builder := &builders[int(functionID)]
	proto, code := builder.proto, builder.code
	if proto == nil || proto.variadic || proto.params < 0 || proto.params > proto.registers || proto.registers <= 0 {
		return false
	}
	if len(proto.upvalues) != 0 && (len(proto.upvalues) != 1 || builder.selfUpvalue != 0) {
		return false
	}
	for _, constant := range proto.constants {
		if valueKind(constant) != NumberKind {
			return false
		}
	}
	if len(code) == 0 {
		return false
	}

	registers := proto.registers
	facts := make([]int32, len(code)*registers)
	for index := range facts {
		facts[index] = compactFactUnknown
	}
	reachable := make([]bool, len(code))
	queued := make([]bool, len(code))
	work := make([]int, 0, len(code))
	entry := facts[:registers]
	for register := 0; register < proto.params; register++ {
		entry[register] = compactFactNumeric
	}
	reachable[0], queued[0] = true, true
	work = append(work, 0)
	returned := false
	callSites := make([]compactCallSite, 0, 4)

	merge := func(pc int, incoming []int32) bool {
		if pc < 0 || pc >= len(code) {
			return false
		}
		row := facts[pc*registers : (pc+1)*registers]
		changed := false
		if !reachable[pc] {
			copy(row, incoming)
			reachable[pc] = true
			changed = true
		} else {
			for index, fact := range incoming {
				merged := row[index]
				if merged != fact {
					merged = compactFactUnknown
				}
				if merged != row[index] {
					row[index] = merged
					changed = true
				}
			}
		}
		if changed && !queued[pc] {
			queued[pc] = true
			work = append(work, pc)
		}
		return true
	}

	current := make([]int32, registers)
	for head := 0; head < len(work); head++ {
		pc := work[head]
		queued[pc] = false
		copy(current, facts[pc*registers:(pc+1)*registers])
		ins := code[pc]
		regOK := func(index int) bool { return index >= 0 && index < registers }
		numeric := func(index int) bool { return regOK(index) && current[index] == compactFactNumeric }
		setNumeric := func(index int) bool {
			if !regOK(index) {
				return false
			}
			current[index] = compactFactNumeric
			return true
		}
		constantNumber := func(index int) bool {
			return index >= 0 && index < len(proto.constants) && valueKind(proto.constants[index]) == NumberKind
		}

		switch ins.op {
		case opLoadConst:
			if !constantNumber(ins.b) || !setNumeric(ins.a) {
				return false
			}
		case opMove:
			if !regOK(ins.a) || !regOK(ins.b) || current[ins.b] == compactFactUnknown {
				return false
			}
			current[ins.a] = current[ins.b]
		case opClosure:
			if !regOK(ins.a) || ins.b < 0 || ins.b >= len(proto.prototypes) {
				return false
			}
			target, ok := ids[proto.prototypes[ins.b]]
			if !ok {
				return false
			}
			current[ins.a] = int32(target)
		case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
			if !numeric(ins.b) || !numeric(ins.c) || !setNumeric(ins.a) {
				return false
			}
		case opNeg:
			if !numeric(ins.b) || !setNumeric(ins.a) {
				return false
			}
		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			if !numeric(ins.b) || !constantNumber(ins.c) || !setNumeric(ins.a) {
				return false
			}
		case opNumericForCheck:
			if !numeric(ins.a) || !numeric(ins.b) || !numeric(ins.c) {
				return false
			}
		case opNumericForLoop:
			if !numeric(ins.a) || !numeric(ins.b) || !setNumeric(ins.a) {
				return false
			}
		case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			if !numeric(ins.a) || !constantNumber(ins.b) {
				return false
			}
		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			if !numeric(ins.a) || !numeric(ins.b) {
				return false
			}
		case opJumpIfFalse:
			if !numeric(ins.a) {
				return false
			}
		case opJump:
		case opCall, opCallOne, opCallLocalOne, opCallUpvalueOne:
			target, argumentStart, argumentCount, result, borrow, ok := compactCallShape(functionID, builder, builders, ids, current, pc, ins)
			if !ok || int(target) >= len(builders) || !regOK(result) {
				return false
			}
			callee := builders[int(target)].proto
			if argumentCount < callee.params || argumentStart < 0 || argumentStart+argumentCount > registers {
				return false
			}
			for argument := 0; argument < callee.params; argument++ {
				if !numeric(argumentStart + argument) {
					return false
				}
			}
			current[result] = compactFactNumeric
			flags := uint16(0)
			if borrow {
				flags = compactCallBorrow
			}
			site := compactCallSite{
				wordPC:        uint32(builder.boundaries[pc]),
				target:        target,
				argumentStart: uint16(argumentStart),
				result:        uint16(result),
				flags:         flags,
			}
			replaced := false
			for index := range callSites {
				if callSites[index].wordPC == site.wordPC {
					callSites[index] = site
					replaced = true
					break
				}
			}
			if !replaced {
				callSites = append(callSites, site)
			}
		case opReturnOne:
			if !numeric(ins.a) {
				return false
			}
			returned = true
		case opReturn:
			count := ins.b
			if count < 0 {
				prefix := -count - 1
				if prefix != 0 || pc == 0 {
					return false
				}
				previous := code[pc-1]
				if previous.op != opCall || previous.a != ins.a {
					return false
				}
				count = 1
			}
			if count != 1 || !numeric(ins.a) {
				return false
			}
			returned = true
		default:
			return false
		}

		switch opcodeControlFlow(ins.op) {
		case opcodeControlReturn:
			continue
		case opcodeControlJump:
			target, ok := instructionJumpTarget(ins)
			if !ok || !merge(target, current) {
				return false
			}
		case opcodeControlBranch:
			target, ok := instructionJumpTarget(ins)
			if !ok || !merge(target, current) || !merge(pc+1, current) {
				return false
			}
		default:
			if pc+1 >= len(code) || !merge(pc+1, current) {
				return false
			}
		}
	}
	if !returned {
		return false
	}
	sort.Slice(callSites, func(left, right int) bool { return callSites[left].wordPC < callSites[right].wordPC })
	builder.callSites = callSites
	return true
}

func compactCallShape(functionID uint16, builder *compactBuildFunction, builders []compactBuildFunction, ids map[*Proto]uint16, facts []int32, pc int, ins instruction) (target uint16, argumentStart, argumentCount, result int, borrow, ok bool) {
	proto := builder.proto
	resolveRegister := func(register int) (uint16, bool) {
		if register < 0 || register >= len(facts) || facts[register] < 0 || facts[register] > int32(^uint16(0)) {
			return 0, false
		}
		return uint16(facts[register]), true
	}
	result = ins.a
	switch ins.op {
	case opCall:
		var resolved bool
		target, resolved = resolveRegister(ins.b)
		if !resolved {
			return 0, 0, 0, 0, false, false
		}
		argumentStart = ins.b + 1
		if prefix, open := openArgumentCallPrefix(ins); open {
			if pc == 0 {
				return 0, 0, 0, 0, false, false
			}
			previous := builder.code[pc-1]
			openStart := argumentStart + prefix
			if previous.op != opCall || previous.a != openStart {
				return 0, 0, 0, 0, false, false
			}
			argumentCount = prefix + 1
			borrow = false
		} else {
			argumentCount = ins.c
			borrow = decodeOpenResultCallMarker(ins.d)
		}
		if count, fixedMulti := decodeFixedMultiResultCount(ins.d, proto.registers); fixedMulti || count > 1 {
			return 0, 0, 0, 0, false, false
		}
		if ins.d > 1 {
			return 0, 0, 0, 0, false, false
		}
		return target, argumentStart, argumentCount, result, borrow, argumentCount >= 0
	case opCallOne:
		var resolved bool
		target, resolved = resolveRegister(ins.b)
		if !resolved {
			return 0, 0, 0, 0, false, false
		}
		argumentCount, borrow = decodeFixedCallCount(ins.c)
		return target, ins.b + 1, argumentCount, result, borrow, true
	case opCallLocalOne:
		var resolved bool
		target, resolved = resolveRegister(ins.b)
		if !resolved {
			return 0, 0, 0, 0, false, false
		}
		argumentCount, borrow = decodeFixedCallCount(ins.d)
		return target, ins.c, argumentCount, result, borrow, true
	case opCallUpvalueOne:
		if builder.selfUpvalue < 0 || ins.b != int(builder.selfUpvalue) {
			return 0, 0, 0, 0, false, false
		}
		argumentCount, borrow = decodeFixedCallCount(ins.d)
		return functionID, ins.c, argumentCount, result, borrow, true
	default:
		return 0, 0, 0, 0, false, false
	}
}

func (function *compactFunction) callSiteAt(wordPC int) (compactCallSite, bool) {
	if function == nil {
		return compactCallSite{}, false
	}
	for _, site := range function.callSites {
		if int(site.wordPC) == wordPC {
			return site, true
		}
	}
	return compactCallSite{}, false
}

func growCompactNumericStack(stack []float64, needed int) []float64 {
	if needed <= len(stack) {
		return stack
	}
	if needed <= cap(stack) {
		return stack[:needed]
	}
	capacity := cap(stack) * 2
	if capacity < needed {
		capacity = needed
	}
	grown := make([]float64, needed, capacity)
	copy(grown, stack)
	return grown
}

// runCompactNumericExecution executes every admitted function in one dispatch
// loop over one pointer-free scalar stack and one compact continuation stack.
// handled=false is a safe restart because admission proves the complete graph
// has no externally visible effect.
func runCompactNumericExecution(proto *Proto, args []Value, state *compactExecutionState) ([]Value, bool, error) {
	program := proto.compact
	if program == nil || state == nil || int(program.entry) >= len(program.functions) {
		return nil, false, nil
	}
	entry := &program.functions[int(program.entry)]
	if entry.proto != proto || len(args) < proto.params {
		return nil, false, nil
	}
	stack := state.prepareStack(proto.registers)
	for index := 0; index < proto.params; index++ {
		if valueKind(args[index]) != NumberKind {
			return nil, false, nil
		}
		stack[index] = valueNumber(args[index])
	}

	functionID := program.entry
	function := entry
	words := function.proto.words
	constants := function.proto.constantNumbers
	base, top, pc := 0, function.proto.registers, 0

	for {
		if uint(pc) >= uint(len(words)) {
			return nil, false, nil
		}
		raw := words[pc]
		op := opcode(uint8(raw) & uint8(wordcodeOpcodeMask))
		a := int(uint8(raw >> 8))
		b := int(uint8(raw >> 16))
		c := int(uint8(raw >> 24))
		next := pc + 1

		switch op {
		case opLoadConst:
			b = int(uint16(raw >> 16))
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return nil, false, nil
				}
				b = int(int32(words[next]))
				next++
			}
			stack[base+a] = constants[b]
		case opMove:
			stack[base+a] = stack[base+b]
		case opClosure:
			// Function identity is compile-time metadata in an admitted graph.
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return nil, false, nil
				}
				next++
			}
		case opAdd:
			stack[base+a] = stack[base+b] + stack[base+c]
		case opSub:
			stack[base+a] = stack[base+b] - stack[base+c]
		case opMul:
			stack[base+a] = stack[base+b] * stack[base+c]
		case opDiv:
			stack[base+a] = stack[base+b] / stack[base+c]
		case opMod:
			left, right := stack[base+b], stack[base+c]
			stack[base+a] = left - math.Floor(left/right)*right
		case opIDiv:
			stack[base+a] = math.Floor(stack[base+b] / stack[base+c])
		case opPow:
			stack[base+a] = math.Pow(stack[base+b], stack[base+c])
		case opNeg:
			stack[base+a] = -stack[base+b]
		case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return nil, false, nil
				}
				c = int(int32(words[next]))
				next++
			}
			left, right := stack[base+b], constants[c]
			switch op {
			case opAddK:
				stack[base+a] = left + right
			case opSubK:
				stack[base+a] = left - right
			case opMulK:
				stack[base+a] = left * right
			case opDivK:
				stack[base+a] = left / right
			case opModK:
				stack[base+a] = left - math.Floor(left/right)*right
			case opIDivK:
				stack[base+a] = math.Floor(left / right)
			}
		case opNumericForCheck:
			if next >= len(words) {
				return nil, false, nil
			}
			target := int(int32(words[next]))
			next++
			target += next
			loopValue, limitValue, stepValue := stack[base+a], stack[base+b], stack[base+c]
			if math.IsNaN(loopValue) || math.IsNaN(limitValue) || math.IsNaN(stepValue) {
				return nil, false, nil
			}
			if (stepValue > 0 && loopValue > limitValue) || (stepValue <= 0 && loopValue < limitValue) {
				pc = target
				continue
			}
		case opNumericForLoop:
			if next >= len(words) {
				return nil, false, nil
			}
			target := int(int32(words[next]))
			next++
			target += next
			stack[base+a] += stack[base+b]
			pc = target
			continue
		case opJumpIfNotEqualK, opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
			if next >= len(words) {
				return nil, false, nil
			}
			b = int(uint16(raw >> 16))
			target := int(int32(words[next]))
			next++
			target += next
			left, right := stack[base+a], constants[b]
			jump := false
			switch op {
			case opJumpIfNotEqualK:
				jump = left != right
			default:
				if math.IsNaN(left) || math.IsNaN(right) {
					return nil, false, nil
				}
				switch op {
				case opJumpIfNotLessK:
					jump = !(left < right)
				case opJumpIfNotGreaterK:
					jump = !(left > right)
				case opJumpIfLessK:
					jump = left < right
				case opJumpIfGreaterK:
					jump = left > right
				}
			}
			if jump {
				pc = target
				continue
			}
		case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
			if next >= len(words) {
				return nil, false, nil
			}
			target := int(int32(words[next]))
			next++
			target += next
			left, right := stack[base+a], stack[base+b]
			if math.IsNaN(left) || math.IsNaN(right) {
				return nil, false, nil
			}
			jump := false
			switch op {
			case opJumpIfNotLess:
				jump = !(left < right)
			case opJumpIfNotGreater:
				jump = !(left > right)
			case opJumpIfLess:
				jump = left < right
			case opJumpIfGreater:
				jump = left > right
			}
			if jump {
				pc = target
				continue
			}
		case opJumpIfFalse:
			// Every admitted value is a number, and every number is truthy.
			if raw&wordcodeAuxBit != 0 {
				if next >= len(words) {
					return nil, false, nil
				}
				next++
			}
		case opJump:
			pc = int(int32(raw)>>8) + next
			continue
		case opCall, opCallOne, opCallLocalOne, opCallUpvalueOne:
			if next >= len(words) {
				return nil, false, nil
			}
			next++ // all admitted call forms have one required AUX word
			site, ok := function.callSiteAt(pc)
			if !ok || int(site.target) >= len(program.functions) {
				return nil, false, nil
			}
			callee := &program.functions[int(site.target)]
			argumentStart := base + int(site.argumentStart)
			calleeBase := top
			if site.flags&compactCallBorrow != 0 {
				calleeBase = argumentStart
			}
			needed := calleeBase + callee.proto.registers
			stack = growCompactNumericStack(stack, needed)
			state.stack = stack
			if site.flags&compactCallBorrow == 0 {
				copy(stack[calleeBase:calleeBase+callee.proto.params], stack[argumentStart:argumentStart+callee.proto.params])
			}
			state.frames = append(state.frames, compactCallFrame{
				returnPC:       uint32(next),
				callerBase:     uint32(base),
				callerTop:      uint32(top),
				resultBase:     uint32(base + int(site.result)),
				callerFunction: functionID,
			})
			functionID = site.target
			function = callee
			words = function.proto.words
			constants = function.proto.constantNumbers
			base = calleeBase
			if needed > top {
				top = needed
			}
			pc = 0
			continue
		case opReturnOne:
			result := stack[base+a]
			if len(state.frames) == 0 {
				if slotExecutionNumberNeedsBox(result) {
					return nil, false, nil
				}
				return []Value{NumberValue(result)}, true, nil
			}
			last := len(state.frames) - 1
			frame := state.frames[last]
			state.frames = state.frames[:last]
			functionID = frame.callerFunction
			function = &program.functions[int(functionID)]
			words = function.proto.words
			constants = function.proto.constantNumbers
			base, top, pc = int(frame.callerBase), int(frame.callerTop), int(frame.returnPC)
			stack[int(frame.resultBase)] = result
			continue
		case opReturn:
			count := int(int16(uint16(raw >> 16)))
			if count < 0 {
				count = 1
			}
			if count != 1 {
				return nil, false, nil
			}
			result := stack[base+a]
			if len(state.frames) == 0 {
				if slotExecutionNumberNeedsBox(result) {
					return nil, false, nil
				}
				return []Value{NumberValue(result)}, true, nil
			}
			last := len(state.frames) - 1
			frame := state.frames[last]
			state.frames = state.frames[:last]
			functionID = frame.callerFunction
			function = &program.functions[int(functionID)]
			words = function.proto.words
			constants = function.proto.constantNumbers
			base, top, pc = int(frame.callerBase), int(frame.callerTop), int(frame.returnPC)
			stack[int(frame.resultBase)] = result
			continue
		default:
			return nil, false, nil
		}
		pc = next
	}
}
