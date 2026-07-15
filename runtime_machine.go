package ember

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

var errCompactMachineFellOffEnd = errors.New("run: compact Machine reached the end without return")

type scalarMachine struct {
	image       *codeImage
	registers   []slot
	results     []slot
	scratch     slot
	numberBits  []uint64
	resultCount int
	window      executionWindow
	bound       bool
}

var scalarMachinePool = sync.Pool{
	New: func() any { return new(scalarMachine) },
}

func executeCodeImage(image *codeImage, controller *executionController) ([]Value, error) {
	if image == nil || !image.eligible {
		return nil, fmt.Errorf("run: compact Machine received an ineligible image")
	}
	machine, err := bindScalarMachine(image, controller)
	if err != nil {
		return nil, err
	}
	defer releaseScalarMachine(machine)
	defer machine.window.commit()

	if controller != nil {
		if err := controller.enterCall(); err != nil {
			return nil, machine.wrapError(0, err)
		}
		defer controller.leaveCall()
	}
	errorPC, err := runGeneratedScalarMachineLoop(machine)
	if err != nil {
		return nil, machine.wrapError(errorPC, err)
	}
	return machine.exportResults()
}

func bindScalarMachine(image *codeImage, controller *executionController) (*scalarMachine, error) {
	if image == nil || !image.eligible {
		return nil, fmt.Errorf("bind compact Machine: ineligible image")
	}
	numberCells := image.registers + image.maxResults + 1
	if numberCells < 0 || uint64(numberCells+1) > slotIndexMask {
		return nil, fmt.Errorf("bind compact Machine: %d scalar cells exceed slot capacity", numberCells)
	}
	machine := scalarMachinePool.Get().(*scalarMachine)
	machine.image = image
	machine.registers = resizeSlots(machine.registers, image.registers)
	machine.results = resizeSlots(machine.results, image.maxResults)
	machine.numberBits = resizeUint64s(machine.numberBits, numberCells)
	machine.resultCount = 0
	machine.scratch = 0
	machine.window = newExecutionWindow(controller)
	machine.bound = true
	return machine, nil
}

func releaseScalarMachine(machine *scalarMachine) {
	if machine == nil || !machine.bound {
		return
	}
	machine.image = nil
	machine.registers = machine.registers[:0]
	machine.results = machine.results[:0]
	machine.numberBits = machine.numberBits[:0]
	machine.resultCount = 0
	machine.scratch = 0
	machine.window = executionWindow{}
	machine.bound = false
	scalarMachinePool.Put(machine)
}

func resizeSlots(values []slot, length int) []slot {
	if cap(values) < length {
		values = make([]slot, length)
	} else {
		values = values[:length]
	}
	for index := range values {
		values[index] = slotNil
	}
	return values
}

func resizeUint64s(values []uint64, length int) []uint64 {
	if cap(values) < length {
		return make([]uint64, length)
	}
	values = values[:length]
	clear(values)
	return values
}

func (machine *scalarMachine) charge(operation machineOperation) error {
	for count := uint8(0); count < operation.guestCharge; count++ {
		if err := machine.window.stepInstruction(); err != nil {
			return err
		}
	}
	return nil
}

func (machine *scalarMachine) wrapError(pc int, err error) error {
	if err == nil || runtimeErrorAlreadyWrapped(err) {
		return err
	}
	frame := ScriptFrame{}
	if machine != nil && machine.image != nil && pc >= 0 && pc < len(machine.image.operations) {
		frame = machine.image.scriptFrame(machine.image.operations[pc])
	}
	return newRuntimeErrorWithController(err, []ScriptFrame{frame}, machine.window.controller)
}

func (machine *scalarMachine) loadConstant(destination, constant int) error {
	if destination < 0 || destination >= len(machine.registers) || constant < 0 || constant >= len(machine.image.constants) {
		return fmt.Errorf("compact Machine LOAD_CONST operands are out of range")
	}
	descriptor := machine.image.constants[constant]
	switch descriptor.kind {
	case NilKind:
		machine.registers[destination] = slotNil
	case BoolKind:
		machine.registers[destination] = slotBool(descriptor.bits != 0)
	case NumberKind:
		machine.setNumber(destination, math.Float64frombits(descriptor.bits))
	default:
		return fmt.Errorf("compact Machine LOAD_CONST kind %s is unsupported", descriptor.kind)
	}
	return nil
}

func (machine *scalarMachine) move(destination, source int) error {
	if destination < 0 || destination >= len(machine.registers) || source < 0 || source >= len(machine.registers) {
		return fmt.Errorf("compact Machine MOVE operands are out of range")
	}
	return machine.copySlot(destination, machine.registers[source])
}

func (machine *scalarMachine) copySlot(destination int, value slot) error {
	if slotValueKind(value) == NumberKind {
		number, err := machine.number(value)
		if err != nil {
			return err
		}
		machine.setNumber(destination, number)
		return nil
	}
	machine.registers[destination] = value
	return nil
}

func (machine *scalarMachine) setNumber(cell int, number float64) {
	bits := math.Float64bits(number)
	if bits&slotTaggedMask != slotTaggedPrefix {
		machine.setCell(cell, slot(bits))
		return
	}
	machine.numberBits[cell] = bits
	machine.setCell(cell, slot(slotTaggedPrefix|
		uint64(slotTagBoxedNumber)<<slotTagShift|
		uint64(1)<<slotGenerationShift|
		uint64(cell+1)))
}

func (machine *scalarMachine) setCell(cell int, value slot) {
	if cell < len(machine.registers) {
		machine.registers[cell] = value
		return
	}
	result := cell - len(machine.registers)
	if result < len(machine.results) {
		machine.results[result] = value
		return
	}
	machine.scratch = value
}

func (machine *scalarMachine) number(value slot) (float64, error) {
	if !slotIsTagged(value) {
		return math.Float64frombits(uint64(value)), nil
	}
	if slotTagOf(value) != slotTagBoxedNumber {
		return 0, fmt.Errorf("value is %s, want number", slotValueKind(value))
	}
	_, index, generation, err := slotUnpackHandle(value)
	if err != nil || generation != 1 || index == 0 || int(index) > len(machine.numberBits) {
		return 0, fmt.Errorf("compact Machine boxed number handle is invalid")
	}
	return math.Float64frombits(machine.numberBits[index-1]), nil
}

func (machine *scalarMachine) binary(op opcode, destination int, left, right slot) error {
	operator, prefix := machineArithmeticNames(op)
	leftNumber, err := machine.numericOperand(left, "left", operator)
	if err != nil {
		return fmt.Errorf("run: %s failed: %w", prefix, err)
	}
	rightNumber, err := machine.numericOperand(right, "right", operator)
	if err != nil {
		return fmt.Errorf("run: %s failed: %w", prefix, err)
	}
	var result float64
	switch op {
	case opAdd, opAddK:
		result = leftNumber + rightNumber
	case opSub, opSubK:
		result = leftNumber - rightNumber
	case opMul, opMulK:
		result = leftNumber * rightNumber
	case opDiv, opDivK:
		result = leftNumber / rightNumber
	case opMod, opModK:
		result = leftNumber - math.Floor(leftNumber/rightNumber)*rightNumber
	case opIDiv, opIDivK:
		result = math.Floor(leftNumber / rightNumber)
	case opPow:
		result = math.Pow(leftNumber, rightNumber)
	default:
		return fmt.Errorf("compact Machine arithmetic opcode %s is unsupported", opcodeName(op))
	}
	machine.setNumber(destination, result)
	return nil
}

func machineArithmeticNames(op opcode) (operator, prefix string) {
	switch op {
	case opAdd, opAddK:
		return "add", "add"
	case opSub, opSubK:
		return "subtract", "subtract"
	case opMul, opMulK:
		return "multiply", "multiply"
	case opDiv, opDivK:
		return "divide", "divide"
	case opMod, opModK:
		return "modulo", "modulo"
	case opIDiv, opIDivK:
		return "floor divide", "floor divide"
	case opPow:
		return "power", "power"
	default:
		return opcodeName(op), opcodeName(op)
	}
}

func (machine *scalarMachine) binaryRegisters(op opcode, destination, left, right int) error {
	return machine.binary(op, destination, machine.registers[left], machine.registers[right])
}

func (machine *scalarMachine) binaryConstant(op opcode, destination, left, constant int) error {
	right, err := machine.constantSlot(constant, destination)
	if err != nil {
		return err
	}
	return machine.binary(op, destination, machine.registers[left], right)
}

func (machine *scalarMachine) constantSlot(index, _ int) (slot, error) {
	if index < 0 || index >= len(machine.image.constants) {
		return 0, fmt.Errorf("compact Machine constant index %d is out of range", index)
	}
	descriptor := machine.image.constants[index]
	switch descriptor.kind {
	case NilKind:
		return slotNil, nil
	case BoolKind:
		return slotBool(descriptor.bits != 0), nil
	case NumberKind:
		scratchCell := len(machine.registers) + len(machine.results)
		machine.setNumber(scratchCell, math.Float64frombits(descriptor.bits))
		return machine.scratch, nil
	default:
		return 0, fmt.Errorf("compact Machine constant kind %s is unsupported", descriptor.kind)
	}
}

func (machine *scalarMachine) numericOperand(value slot, side, operator string) (float64, error) {
	if slotValueKind(value) == NumberKind {
		return machine.number(value)
	}
	return 0, fmt.Errorf("%s %s operand is %s, want number", operator, side, slotValueKind(value))
}

func (machine *scalarMachine) negate(destination, operand int) error {
	value := machine.registers[operand]
	if slotValueKind(value) != NumberKind {
		return fmt.Errorf("run: negate operand is %s, want number", slotValueKind(value))
	}
	number, err := machine.number(value)
	if err != nil {
		return err
	}
	machine.setNumber(destination, -number)
	return nil
}

func (machine *scalarMachine) equal(left, right slot) (bool, error) {
	leftKind, rightKind := slotValueKind(left), slotValueKind(right)
	if leftKind != rightKind {
		return false, nil
	}
	switch leftKind {
	case NilKind:
		return true, nil
	case BoolKind:
		leftBool, err := slotBoolValue(left)
		if err != nil {
			return false, err
		}
		rightBool, err := slotBoolValue(right)
		return leftBool == rightBool, err
	case NumberKind:
		leftNumber, err := machine.number(left)
		if err != nil {
			return false, err
		}
		rightNumber, err := machine.number(right)
		if err != nil {
			return false, err
		}
		return !math.IsNaN(leftNumber) && !math.IsNaN(rightNumber) && leftNumber == rightNumber, nil
	default:
		return false, fmt.Errorf("compact Machine equality kind %s is unsupported", leftKind)
	}
}

func (machine *scalarMachine) compare(op opcode, left, right slot) (bool, error) {
	prefix := machineComparisonName(op)
	if slotValueKind(left) != slotValueKind(right) {
		return false, fmt.Errorf("run: %s failed: compare operands are %s and %s", prefix, slotValueKind(left), slotValueKind(right))
	}
	if slotValueKind(left) != NumberKind {
		return false, fmt.Errorf("run: %s failed: compare operands are %s, want number or string", prefix, slotValueKind(left))
	}
	leftNumber, err := machine.number(left)
	if err != nil {
		return false, err
	}
	rightNumber, err := machine.number(right)
	if err != nil {
		return false, err
	}
	if math.IsNaN(leftNumber) || math.IsNaN(rightNumber) {
		return false, fmt.Errorf("run: %s failed: compare operand is NaN", prefix)
	}
	switch op {
	case opLess, opJumpIfNotLess, opJumpIfLess, opJumpIfNotLessK, opJumpIfLessK:
		return leftNumber < rightNumber, nil
	case opLessEqual:
		return leftNumber <= rightNumber, nil
	case opGreater, opJumpIfNotGreater, opJumpIfGreater, opJumpIfNotGreaterK, opJumpIfGreaterK:
		return leftNumber > rightNumber, nil
	case opGreaterEqual:
		return leftNumber >= rightNumber, nil
	default:
		return false, fmt.Errorf("compact Machine comparison opcode %s is unsupported", opcodeName(op))
	}
}

func machineComparisonName(op opcode) string {
	switch op {
	case opLess, opJumpIfNotLess, opJumpIfLess, opJumpIfNotLessK, opJumpIfLessK:
		return "less"
	case opLessEqual:
		return "less equal"
	case opGreater, opJumpIfNotGreater, opJumpIfGreater, opJumpIfNotGreaterK, opJumpIfGreaterK:
		return "greater"
	case opGreaterEqual:
		return "greater equal"
	default:
		return opcodeName(op)
	}
}

func (machine *scalarMachine) storeComparison(op opcode, destination, left, right int) error {
	var result bool
	var err error
	if op == opEqual || op == opNotEqual {
		result, err = machine.equal(machine.registers[left], machine.registers[right])
		if op == opNotEqual {
			result = !result
		}
	} else {
		result, err = machine.compare(op, machine.registers[left], machine.registers[right])
	}
	if err != nil {
		return err
	}
	machine.registers[destination] = slotBool(result)
	return nil
}

func (machine *scalarMachine) equalityConstant(register, constant, scratch int) (bool, error) {
	right, err := machine.constantSlot(constant, scratch)
	if err != nil {
		return false, err
	}
	return machine.equal(machine.registers[register], right)
}

func (machine *scalarMachine) compareConstant(op opcode, register, constant, scratch int) (bool, error) {
	if constant >= 0 && constant < len(machine.image.constants) {
		descriptor := machine.image.constants[constant]
		left := machine.registers[register]
		if descriptor.kind == NumberKind && slotValueKind(left) == NumberKind {
			leftNumber, err := machine.number(left)
			if err != nil {
				return false, err
			}
			rightNumber := math.Float64frombits(descriptor.bits)
			if math.IsNaN(leftNumber) || math.IsNaN(rightNumber) {
				return false, fmt.Errorf("run: %s failed: compare operand is NaN", machineComparisonName(op))
			}
			switch op {
			case opJumpIfNotLessK, opJumpIfLessK:
				return leftNumber < rightNumber, nil
			case opJumpIfNotGreaterK, opJumpIfGreaterK:
				return leftNumber > rightNumber, nil
			}
		}
	}
	right, err := machine.constantSlot(constant, scratch)
	if err != nil {
		return false, err
	}
	return machine.compare(op, machine.registers[register], right)
}

func (machine *scalarMachine) numericForCheck(loop, limit, step int) (bool, error) {
	values := [...]struct {
		name     string
		register int
	}{
		{name: "loop value", register: loop},
		{name: "limit", register: limit},
		{name: "step", register: step},
	}
	numbers := [3]float64{}
	for index, value := range values {
		slotValue := machine.registers[value.register]
		if slotValueKind(slotValue) != NumberKind {
			return false, fmt.Errorf("run: numeric for %s is %s, want number", value.name, slotValueKind(slotValue))
		}
		number, err := machine.number(slotValue)
		if err != nil {
			return false, err
		}
		numbers[index] = number
	}
	if math.IsNaN(numbers[0]) || math.IsNaN(numbers[1]) || math.IsNaN(numbers[2]) {
		return false, fmt.Errorf("run: numeric for operand is NaN")
	}
	if numbers[2] > 0 {
		return numbers[0] > numbers[1], nil
	}
	return numbers[0] < numbers[1], nil
}

func (machine *scalarMachine) numericForLoop(loop, step int) error {
	loopValue := machine.registers[loop]
	if slotValueKind(loopValue) != NumberKind {
		return fmt.Errorf("run: numeric for loop value is %s, want number", slotValueKind(loopValue))
	}
	stepValue := machine.registers[step]
	if slotValueKind(stepValue) != NumberKind {
		return fmt.Errorf("run: numeric for step is %s, want number", slotValueKind(stepValue))
	}
	loopNumber, err := machine.number(loopValue)
	if err != nil {
		return err
	}
	stepNumber, err := machine.number(stepValue)
	if err != nil {
		return err
	}
	machine.setNumber(loop, loopNumber+stepNumber)
	return nil
}

func machineTruthy(value slot) bool {
	switch slotValueKind(value) {
	case NilKind:
		return false
	case BoolKind:
		result, err := slotBoolValue(value)
		return err == nil && result
	default:
		return true
	}
}

func (machine *scalarMachine) returnValues(start, count int) error {
	if count == 0 {
		machine.resultCount = 0
		return nil
	}
	if count < 0 || count > len(machine.results) || start < 0 || start+count > len(machine.registers) {
		return fmt.Errorf("compact Machine return window is out of range")
	}
	for index := 0; index < count; index++ {
		value := machine.registers[start+index]
		cell := len(machine.registers) + index
		if slotValueKind(value) == NumberKind {
			number, err := machine.number(value)
			if err != nil {
				return err
			}
			machine.setNumber(cell, number)
		} else {
			machine.results[index] = value
		}
	}
	machine.resultCount = count
	return nil
}

func (machine *scalarMachine) exportResults() ([]Value, error) {
	if machine.resultCount == 0 {
		return nil, nil
	}
	values := make([]Value, machine.resultCount)
	for index, value := range machine.results[:machine.resultCount] {
		switch slotValueKind(value) {
		case NilKind:
			values[index] = NilValue()
		case BoolKind:
			boolean, err := slotBoolValue(value)
			if err != nil {
				return nil, err
			}
			values[index] = BoolValue(boolean)
		case NumberKind:
			number, err := machine.number(value)
			if err != nil {
				return nil, err
			}
			values[index] = NumberValue(number)
		default:
			return nil, fmt.Errorf("compact Machine result kind %s is unsupported", slotValueKind(value))
		}
	}
	return values, nil
}
