package ember

import (
	"fmt"
	"math"
)

const (
	directNumericTraceOperationCap = directNumericForTraceInstructionCap - 2
	directNumericTracePlanBytes    = 184
)

// directNumericTraceMicroOp is the pointer-free, load-time decoded form of a
// numeric body instruction. guestCharge is two only when a Move followed by
// an in-place constant/unary operation has been formed into one superword.
type directNumericTraceMicroOp struct {
	sourcePC    int32
	operand     int32
	op          opcode
	a           uint8
	b           uint8
	guestCharge uint8
}

// directNumericTracePlan is fixed-size so owner-local adaptive state remains
// bounded and has no GC-scanned semantic references.
type directNumericTracePlan struct {
	checkPC        int32
	loopPC         int32
	exitPC         int32
	loopRegister   uint8
	limitRegister  uint8
	stepRegister   uint8
	operationCount uint8
	operations     [directNumericTraceOperationCap]directNumericTraceMicroOp
}

func buildDirectNumericTracePlan(trace []wordcodeDecoded) (directNumericTracePlan, bool) {
	if len(trace) < 2 || len(trace) > directNumericForTraceInstructionCap {
		return directNumericTracePlan{}, false
	}
	check := trace[0]
	loop := trace[len(trace)-1]
	if check.wordPC > math.MaxInt32 || loop.wordPC > math.MaxInt32 {
		return directNumericTracePlan{}, false
	}
	plan := directNumericTracePlan{
		checkPC:       int32(check.wordPC),
		loopPC:        int32(loop.wordPC),
		exitPC:        int32(check.nextWord + check.ins.d),
		loopRegister:  uint8(check.ins.a),
		limitRegister: uint8(check.ins.b),
		stepRegister:  uint8(check.ins.c),
	}
	for index := 1; index < len(trace)-1; index++ {
		entry := trace[index]
		operation := directNumericTraceMicroOp{
			sourcePC:    int32(entry.wordPC),
			operand:     int32(entry.ins.c),
			op:          entry.ins.op,
			a:           uint8(entry.ins.a),
			b:           uint8(entry.ins.b),
			guestCharge: 1,
		}
		if entry.ins.op == opMove && index+1 < len(trace)-1 {
			next := trace[index+1]
			if directNumericTraceCanFoldMove(entry.ins, next.ins) {
				operation.operand = int32(next.ins.c)
				operation.op = next.ins.op
				operation.guestCharge = 2
				index++
			}
		}
		if int(plan.operationCount) >= len(plan.operations) {
			return directNumericTracePlan{}, false
		}
		plan.operations[plan.operationCount] = operation
		plan.operationCount++
	}
	return plan, true
}

func directNumericTraceCanFoldMove(move instruction, next instruction) bool {
	if next.a != move.a || next.b != move.a {
		return false
	}
	switch next.op {
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK, opNeg:
		return true
	default:
		return false
	}
}

// runDirectNumericTrace executes one conservatively tiled numeric loop. The
// caller has already charged and decoded the initial NumericForCheck. All
// guards run before the first canonical register is mutated, so a miss can
// dequicken and redispatch the original instruction without rollback state.
func runDirectNumericTrace(
	registers []Value,
	constantNumbers []float64,
	cache directAdaptiveCacheCell,
	plan *directNumericTracePlan,
	window *executionWindow,
) (nextPC int, errorPC int, err error, guardOK bool) {
	if !directNumericTraceGuardsMatch(registers, cache) {
		return int(plan.checkPC), int(plan.checkPC), nil, false
	}
	checkPC := int(plan.checkPC)
	loopRegister := int(plan.loopRegister)
	limitRegister := int(plan.limitRegister)
	stepRegister := int(plan.stepRegister)
	if loopRegister >= len(registers) || limitRegister >= len(registers) || stepRegister >= len(registers) {
		return checkPC, checkPC, fmt.Errorf("numeric trace at word %d has an invalid controller register", checkPC), true
	}
	if math.IsNaN(valueNumber(registers[loopRegister])) ||
		math.IsNaN(valueNumber(registers[limitRegister])) ||
		math.IsNaN(valueNumber(registers[stepRegister])) {
		return checkPC, checkPC, nil, false
	}
	controlled := window != nil && window.controller != nil
	loop := valueNumber(registers[loopRegister])
	limit := valueNumber(registers[limitRegister])
	step := valueNumber(registers[stepRegister])

	for {
		if (step > 0 && loop > limit) || (step <= 0 && loop < limit) {
			return int(plan.exitPC), checkPC, nil, true
		}

		for index := uint8(0); index < plan.operationCount; index++ {
			operation := plan.operations[index]
			sourcePC := int(operation.sourcePC)
			if controlled {
				if err := window.stepInstruction(); err != nil {
					return sourcePC, sourcePC, err, true
				}
				if operation.guestCharge == 2 {
					registers[operation.a] = registers[operation.b]
					if err := window.stepInstruction(); err != nil {
						return sourcePC + 1, sourcePC + 1, err, true
					}
				}
			}
			a := int(operation.a)
			b := int(operation.b)
			c := int(operation.operand)
			switch operation.op {
			case opMove:
				registers[a] = registers[b]
			case opAdd:
				registers[a] = NumberValue(valueNumber(registers[b]) + valueNumber(registers[c]))
			case opSub:
				registers[a] = NumberValue(valueNumber(registers[b]) - valueNumber(registers[c]))
			case opMul:
				registers[a] = NumberValue(valueNumber(registers[b]) * valueNumber(registers[c]))
			case opDiv:
				registers[a] = NumberValue(valueNumber(registers[b]) / valueNumber(registers[c]))
			case opMod:
				left, right := valueNumber(registers[b]), valueNumber(registers[c])
				registers[a] = NumberValue(left - math.Floor(left/right)*right)
			case opIDiv:
				registers[a] = NumberValue(math.Floor(valueNumber(registers[b]) / valueNumber(registers[c])))
			case opPow:
				registers[a] = NumberValue(math.Pow(valueNumber(registers[b]), valueNumber(registers[c])))
			case opAddK:
				registers[a] = NumberValue(valueNumber(registers[b]) + constantNumbers[c])
			case opSubK:
				registers[a] = NumberValue(valueNumber(registers[b]) - constantNumbers[c])
			case opMulK:
				registers[a] = NumberValue(valueNumber(registers[b]) * constantNumbers[c])
			case opDivK:
				registers[a] = NumberValue(valueNumber(registers[b]) / constantNumbers[c])
			case opModK:
				left, right := valueNumber(registers[b]), constantNumbers[c]
				registers[a] = NumberValue(left - math.Floor(left/right)*right)
			case opIDivK:
				registers[a] = NumberValue(math.Floor(valueNumber(registers[b]) / constantNumbers[c]))
			case opNeg:
				registers[a] = NumberValue(-valueNumber(registers[b]))
			default:
				return sourcePC, sourcePC, fmt.Errorf("numeric trace from word %d reached %s", checkPC, opcodeName(operation.op)), true
			}
		}
		if controlled {
			if err := window.stepInstruction(); err != nil {
				return int(plan.loopPC), int(plan.loopPC), err, true
			}
		}
		loop += step
		registers[loopRegister] = NumberValue(loop)
		if controlled {
			if err := window.stepInstruction(); err != nil {
				return checkPC, checkPC, err, true
			}
		}
	}
}

func directNumericTraceGuardsMatch(registers []Value, cache directAdaptiveCacheCell) bool {
	count := cache.guardCount()
	if count < 1 || count > directAdaptiveGuardRegisterCap {
		return false
	}
	for index := 0; index < count; index++ {
		register := int(cache.guardRegister(index))
		if register >= len(registers) || valueKind(registers[register]) != NumberKind {
			return false
		}
	}
	return true
}
