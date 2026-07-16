package ember

import "math"

const (
	machineBurstMaxOperations      = 4096
	machineBurstWorkspaceRegisters = 32
)

type machineBurstStatus uint32

const (
	machineBurstFallback machineBurstStatus = iota
	machineBurstComplete
	machineBurstQuantum
)

// machineBurstRegion describes one immutable natural loop. All registers are
// relative to the active Machine frame base.
type machineBurstRegion struct {
	operationStart int32
	operationCount int32
	guardStart     int32
	guardCount     int32
	checkPC        int32
	bodyPC         int32
	latchPC        int32
	exitPC         int32
	iterationCost  int32
}

type machineBurstOperation struct {
	op   opcode
	_    [3]byte
	pc   int32
	a    int32
	b    int32
	c    int32
	bits uint64
}

type machineBurstGuard struct {
	register int32
	firstPC  int32
}

// machineBurstControl is the pointer-free input/output record shared with the
// static backend. nextPC is where generic Machine execution resumes; failingPC
// identifies the first operation whose numeric precondition did not hold.
type machineBurstControl struct {
	status    machineBurstStatus
	nextPC    int32
	failingPC int32
	retired   uint32
	quantum   uint32
	base      int32
}

func lowerMachineBurstRegions(code []instruction, operations []machineOperation, constants []machineConstant) ([]machineBurstRegion, []machineBurstOperation, []machineBurstGuard) {
	if len(code) != len(operations) || len(code) == 0 {
		return nil, nil, nil
	}
	predecessors := machineBurstPredecessors(code)
	var regions []machineBurstRegion
	var lowered []machineBurstOperation
	var guards []machineBurstGuard
	for checkPC, check := range code {
		if check.op != opNumericForCheck {
			continue
		}
		latchPC, ok := machineBurstLatch(code, checkPC, check)
		if !ok || latchPC <= checkPC+1 || check.d != latchPC+1 || !machineBurstHasStrictEntry(predecessors, checkPC, latchPC) {
			continue
		}
		valid := true
		for pc := checkPC + 1; pc <= latchPC; pc++ {
			want := pc - 1
			_, hasPredecessor := predecessors[pc][want]
			if len(predecessors[pc]) != 1 || !hasPredecessor {
				valid = false
				break
			}
			if pc < latchPC && !machineBurstBodyOpcode(code[pc].op) {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}

		operationStart := len(lowered)
		guardStart := len(guards)
		defined := make(map[int]bool)
		guardPC := make(map[int]int)
		addGuard := func(register, pc int) {
			if defined[register] {
				return
			}
			if old, ok := guardPC[register]; ok && old <= pc {
				return
			}
			guardPC[register] = pc
		}
		addGuard(check.a, checkPC)
		addGuard(check.b, checkPC)
		addGuard(check.c, checkPC)
		lowered = append(lowered, machineBurstOperation{op: check.op, pc: int32(checkPC), a: int32(check.a), b: int32(check.b), c: int32(check.c)})
		for pc := checkPC + 1; pc < latchPC; pc++ {
			ins := code[pc]
			burst, ok := lowerMachineBurstOperation(ins, operations[pc], constants, pc, defined, addGuard)
			if !ok {
				valid = false
				break
			}
			lowered = append(lowered, burst)
		}
		if !valid {
			lowered = lowered[:operationStart]
			guards = guards[:guardStart]
			continue
		}
		latch := code[latchPC]
		addGuard(latch.a, latchPC)
		addGuard(latch.b, latchPC)
		lowered = append(lowered, machineBurstOperation{op: latch.op, pc: int32(latchPC), a: int32(latch.a), b: int32(latch.b)})
		for register, pc := range guardPC {
			guards = append(guards, machineBurstGuard{register: int32(register), firstPC: int32(pc)})
		}
		machineBurstSortGuards(guards[guardStart:])
		regions = append(regions, machineBurstRegion{
			operationStart: int32(operationStart), operationCount: int32(len(lowered) - operationStart),
			guardStart: int32(guardStart), guardCount: int32(len(guards) - guardStart),
			checkPC: int32(checkPC), bodyPC: int32(checkPC + 1), latchPC: int32(latchPC), exitPC: int32(check.d),
			iterationCost: int32(latchPC - checkPC + 1),
		})
	}
	return regions, lowered, guards
}

func machineBurstPredecessors(code []instruction) []map[int]struct{} {
	result := make([]map[int]struct{}, len(code))
	for pc := range code {
		for _, successor := range instructionSuccessors(code, pc) {
			if successor < 0 || successor >= len(code) {
				continue
			}
			if result[successor] == nil {
				result[successor] = make(map[int]struct{})
			}
			result[successor][pc] = struct{}{}
		}
	}
	return result
}

func machineBurstLatch(code []instruction, checkPC int, check instruction) (int, bool) {
	latchPC := -1
	for pc, ins := range code {
		if ins.op != opNumericForLoop || ins.a != check.a || ins.b != check.c || ins.d != checkPC {
			continue
		}
		if latchPC >= 0 {
			return 0, false
		}
		latchPC = pc
	}
	return latchPC, latchPC >= 0
}

func machineBurstHasStrictEntry(predecessors []map[int]struct{}, checkPC, latchPC int) bool {
	pred := predecessors[checkPC]
	if _, ok := pred[latchPC]; !ok {
		return false
	}
	for pc := range pred {
		if pc == latchPC || pc == checkPC-1 {
			continue
		}
		return false
	}
	return true
}

func machineBurstBodyOpcode(op opcode) bool {
	switch op {
	case opMove, opMulK, opIDivK, opSub, opModK, opAdd:
		return true
	default:
		return false
	}
}

func lowerMachineBurstOperation(ins instruction, operation machineOperation, constants []machineConstant, pc int, defined map[int]bool, addGuard func(int, int)) (machineBurstOperation, bool) {
	result := machineBurstOperation{op: ins.op, pc: int32(pc), a: int32(ins.a), b: int32(ins.b), c: int32(ins.c)}
	switch ins.op {
	case opMove:
		addGuard(ins.b, pc)
	case opMulK, opIDivK, opModK:
		addGuard(ins.b, pc)
		if ins.c < 0 || ins.c >= len(constants) || constants[ins.c].kind != NumberKind {
			return machineBurstOperation{}, false
		}
		result.bits = constants[ins.c].bits
	case opSub, opAdd:
		addGuard(ins.b, pc)
		addGuard(ins.c, pc)
	default:
		return machineBurstOperation{}, false
	}
	defined[ins.a] = true
	_ = operation
	return result, true
}

func machineBurstSortGuards(guards []machineBurstGuard) {
	for i := 1; i < len(guards); i++ {
		guard := guards[i]
		j := i - 1
		for j >= 0 && (guards[j].firstPC > guard.firstPC || guards[j].firstPC == guard.firstPC && guards[j].register > guard.register) {
			guards[j+1] = guards[j]
			j--
		}
		guards[j+1] = guard
	}
}

func runMachineBurstReference(region machineBurstRegion, operations []machineBurstOperation, guards []machineBurstGuard, registers []slot, numberBits []uint64, base, quantum int) machineBurstControl {
	control := machineBurstControl{status: machineBurstFallback, nextPC: region.checkPC, failingPC: region.checkPC, quantum: uint32(quantum), base: int32(base)}
	if quantum < 0 || quantum > machineBurstMaxOperations || !machineBurstSlicesValid(region, operations, guards, registers, numberBits, base) {
		return control
	}
	regionOperations := operations[region.operationStart : region.operationStart+region.operationCount]
	regionGuards := guards[region.guardStart : region.guardStart+region.guardCount]
	guardIndex := 0
	for {
		for _, operation := range regionOperations {
			for guardIndex < len(regionGuards) && regionGuards[guardIndex].firstPC == operation.pc {
				guard := regionGuards[guardIndex]
				if _, ok := machineBurstNumber(registers, numberBits, base+int(guard.register)); !ok {
					control.nextPC = operation.pc
					control.failingPC = operation.pc
					return control
				}
				guardIndex++
			}
			if int(control.retired) >= quantum {
				control.status = machineBurstQuantum
				control.nextPC = operation.pc
				control.failingPC = -1
				return control
			}
			if operation.op == opNumericForCheck {
				loop, _ := machineBurstNumber(registers, numberBits, base+int(operation.a))
				limit, _ := machineBurstNumber(registers, numberBits, base+int(operation.b))
				step, _ := machineBurstNumber(registers, numberBits, base+int(operation.c))
				if math.IsNaN(loop) || math.IsNaN(limit) || math.IsNaN(step) {
					control.nextPC = operation.pc
					control.failingPC = operation.pc
					return control
				}
				control.retired++
				if step > 0 && loop > limit || step <= 0 && loop < limit {
					control.status = machineBurstComplete
					control.nextPC = region.exitPC
					control.failingPC = -1
					return control
				}
				continue
			}
			if !runMachineBurstOperation(operation, registers, numberBits, base) {
				control.failingPC = operation.pc
				return control
			}
			control.retired++
		}
		guardIndex = 0
	}
}

func runMachineBurst(region *machineBurstRegion, operations []machineBurstOperation, guards []machineBurstGuard, registers []slot, numberBits []uint64, base, quantum int) (machineBurstControl, bool) {
	control := machineBurstControl{status: machineBurstFallback}
	if region == nil {
		return control, false
	}
	control.nextPC = region.checkPC
	control.failingPC = region.checkPC
	control.quantum = uint32(quantum)
	control.base = int32(base)
	if quantum < 0 || quantum > machineBurstMaxOperations || !machineBurstSlicesValid(*region, operations, guards, registers, numberBits, base) {
		return control, false
	}
	return runMachineBurstBackend(&control, region, operations, guards, registers, numberBits)
}

func machineBurstRegionAt(proto *machineProto, pc int) (*machineBurstRegion, bool) {
	if proto == nil {
		return nil, false
	}
	for index := range proto.burstRegions {
		if int(proto.burstRegions[index].checkPC) == pc {
			return &proto.burstRegions[index], true
		}
	}
	return nil, false
}

func machineBurstPCInRegion(proto *machineProto, checkPC, pc int) bool {
	region, ok := machineBurstRegionAt(proto, checkPC)
	return ok && pc >= int(region.checkPC) && pc <= int(region.latchPC)
}

func machineBurstSlicesValid(region machineBurstRegion, operations []machineBurstOperation, guards []machineBurstGuard, registers []slot, numberBits []uint64, base int) bool {
	if base < 0 || base > len(registers) || len(registers) == 0 || len(numberBits) <= len(registers) || uint64(len(numberBits)) > slotIndexMask || region.operationStart < 0 || region.operationCount <= 0 || region.guardStart < 0 || region.guardCount < 0 {
		return false
	}
	if int64(region.operationStart)+int64(region.operationCount) > int64(len(operations)) || int64(region.guardStart)+int64(region.guardCount) > int64(len(guards)) {
		return false
	}
	regionOperations := operations[region.operationStart : region.operationStart+region.operationCount]
	if region.checkPC < 0 || region.bodyPC != region.checkPC+1 || region.latchPC < region.bodyPC || region.exitPC != region.latchPC+1 || region.iterationCost != region.operationCount {
		return false
	}
	for index, operation := range regionOperations {
		if operation.pc != region.checkPC+int32(index) {
			return false
		}
		if index == 0 && operation.op != opNumericForCheck || index == len(regionOperations)-1 && operation.op != opNumericForLoop {
			return false
		}
		if index > 0 && index < len(regionOperations)-1 && !machineBurstBodyOpcode(operation.op) {
			return false
		}
		if !machineBurstOperationRegistersValid(operation, base, len(registers), len(numberBits)) {
			return false
		}
	}
	previousPC := int32(-1)
	for _, guard := range guards[region.guardStart : region.guardStart+region.guardCount] {
		cell := base + int(guard.register)
		if guard.register < 0 || cell < 0 || cell >= len(registers) || cell >= len(numberBits) || guard.firstPC < region.checkPC || guard.firstPC > region.latchPC || guard.firstPC < previousPC {
			return false
		}
		previousPC = guard.firstPC
	}
	return true
}

func machineBurstOperationRegistersValid(operation machineBurstOperation, base, registerCount, numberCount int) bool {
	valid := func(register int32) bool {
		cell := base + int(register)
		return register >= 0 && register < machineBurstWorkspaceRegisters && cell >= 0 && cell < registerCount && cell < numberCount
	}
	switch operation.op {
	case opNumericForCheck:
		return valid(operation.a) && valid(operation.b) && valid(operation.c)
	case opMove, opMulK, opIDivK, opModK, opNumericForLoop:
		return valid(operation.a) && valid(operation.b)
	case opSub, opAdd:
		return valid(operation.a) && valid(operation.b) && valid(operation.c)
	default:
		return false
	}
}

func machineBurstNumber(registers []slot, numberBits []uint64, cell int) (float64, bool) {
	if cell < 0 || cell >= len(registers) || cell >= len(numberBits) {
		return 0, false
	}
	value := registers[cell]
	if !slotIsTagged(value) {
		return math.Float64frombits(uint64(value)), true
	}
	if slotTagOf(value) != slotTagBoxedNumber {
		return 0, false
	}
	_, index, generation, err := slotUnpackHandle(value)
	if err != nil || index == 0 || generation != 1 || int(index) > len(numberBits) {
		return 0, false
	}
	return math.Float64frombits(numberBits[index-1]), true
}

func runMachineBurstOperation(operation machineBurstOperation, registers []slot, numberBits []uint64, base int) bool {
	destination := base + int(operation.a)
	leftRegister := operation.b
	if operation.op == opNumericForLoop {
		leftRegister = operation.a
	}
	left, ok := machineBurstNumber(registers, numberBits, base+int(leftRegister))
	if !ok {
		return false
	}
	var result float64
	switch operation.op {
	case opMove:
		result = left
	case opMulK:
		machineBurstSetConstantScratch(numberBits, registers[base+int(operation.b)], operation.bits)
		result = left * math.Float64frombits(operation.bits)
	case opIDivK:
		machineBurstSetConstantScratch(numberBits, registers[base+int(operation.b)], operation.bits)
		result = math.Floor(left / math.Float64frombits(operation.bits))
	case opModK:
		machineBurstSetConstantScratch(numberBits, registers[base+int(operation.b)], operation.bits)
		right := math.Float64frombits(operation.bits)
		result = left - math.Floor(left/right)*right
	case opSub, opAdd:
		right, rightOK := machineBurstNumber(registers, numberBits, base+int(operation.c))
		if !rightOK {
			return false
		}
		if operation.op == opSub {
			result = left - right
		} else {
			result = left + right
		}
	case opNumericForLoop:
		step, stepOK := machineBurstNumber(registers, numberBits, base+int(operation.b))
		if !stepOK {
			return false
		}
		result = left + step
	default:
		return false
	}
	machineBurstSetNumber(registers, numberBits, destination, result)
	return true
}

func machineBurstSetConstantScratch(numberBits []uint64, left slot, bits uint64) {
	if slotIsTagged(left) && bits&slotTaggedMask == slotTaggedPrefix && len(numberBits) != 0 {
		numberBits[len(numberBits)-1] = bits
	}
}

func machineBurstSetNumber(registers []slot, numberBits []uint64, cell int, number float64) {
	bits := math.Float64bits(number)
	if bits&slotTaggedMask != slotTaggedPrefix {
		registers[cell] = slot(bits)
		return
	}
	numberBits[cell] = bits
	registers[cell] = slot(slotTaggedPrefix | uint64(slotTagBoxedNumber)<<slotTagShift | uint64(1)<<slotGenerationShift | uint64(cell+1))
}
