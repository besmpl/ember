package ember

import "fmt"

// Run executes a compiled Ember prototype and returns its result values.
func Run(proto *Proto) ([]Value, error) {
	return RunWithGlobals(proto, nil)
}

// RunWithGlobals executes a compiled Ember prototype with explicit global
// values available to the script.
func RunWithGlobals(proto *Proto, globals map[string]Value) ([]Value, error) {
	if proto == nil {
		return nil, fmt.Errorf("run: nil prototype")
	}

	registers := make([]Value, proto.registers)

	for pc := 0; pc < len(proto.code); pc++ {
		ins := proto.code[pc]

		switch ins.op {
		case opLoadConst:
			if ins.b < 0 || ins.b >= len(proto.constants) {
				return nil, fmt.Errorf("run: constant index %d out of range", ins.b)
			}
			if ins.a < 0 || ins.a >= len(registers) {
				return nil, fmt.Errorf("run: register index %d out of range", ins.a)
			}
			registers[ins.a] = proto.constants[ins.b]

		case opLoadGlobal:
			if ins.b < 0 || ins.b >= len(proto.constants) {
				return nil, fmt.Errorf("run: global name constant index %d out of range", ins.b)
			}
			if ins.a < 0 || ins.a >= len(registers) {
				return nil, fmt.Errorf("run: register index %d out of range", ins.a)
			}
			name, ok := proto.constants[ins.b].String()
			if !ok {
				return nil, fmt.Errorf("run: global name constant is %s, want string", proto.constants[ins.b].Kind())
			}
			value, ok := globals[name]
			if !ok {
				return nil, fmt.Errorf("run: undefined global %q", name)
			}
			registers[ins.a] = value

		case opMove:
			if ins.a < 0 || ins.a >= len(registers) ||
				ins.b < 0 || ins.b >= len(registers) {
				return nil, fmt.Errorf("run: move register out of range")
			}
			registers[ins.a] = registers[ins.b]

		case opNewTable:
			if ins.a < 0 || ins.a >= len(registers) {
				return nil, fmt.Errorf("run: register index %d out of range", ins.a)
			}
			registers[ins.a] = TableValue(NewTable())

		case opSetField:
			if ins.a < 0 || ins.a >= len(registers) ||
				ins.c < 0 || ins.c >= len(registers) ||
				ins.b < 0 || ins.b >= len(proto.constants) {
				return nil, fmt.Errorf("run: set field operand out of range")
			}
			table, ok := registers[ins.a].Table()
			if !ok {
				return nil, fmt.Errorf("run: set field target is %s, want table", registers[ins.a].Kind())
			}
			if err := table.Set(proto.constants[ins.b], registers[ins.c]); err != nil {
				return nil, fmt.Errorf("run: set field failed: %w", err)
			}

		case opGetField:
			if ins.a < 0 || ins.a >= len(registers) ||
				ins.b < 0 || ins.b >= len(registers) ||
				ins.c < 0 || ins.c >= len(proto.constants) {
				return nil, fmt.Errorf("run: get field operand out of range")
			}
			table, ok := registers[ins.b].Table()
			if !ok {
				return nil, fmt.Errorf("run: get field target is %s, want table", registers[ins.b].Kind())
			}
			value, err := table.Get(proto.constants[ins.c])
			if err != nil {
				return nil, fmt.Errorf("run: get field failed: %w", err)
			}
			registers[ins.a] = value

		case opSetIndex:
			if ins.a < 0 || ins.a >= len(registers) ||
				ins.b < 0 || ins.b >= len(registers) ||
				ins.c < 0 || ins.c >= len(registers) {
				return nil, fmt.Errorf("run: set index operand out of range")
			}
			table, ok := registers[ins.a].Table()
			if !ok {
				return nil, fmt.Errorf("run: set index target is %s, want table", registers[ins.a].Kind())
			}
			if err := table.Set(registers[ins.b], registers[ins.c]); err != nil {
				return nil, fmt.Errorf("run: set index failed: %w", err)
			}

		case opGetIndex:
			if ins.a < 0 || ins.a >= len(registers) ||
				ins.b < 0 || ins.b >= len(registers) ||
				ins.c < 0 || ins.c >= len(registers) {
				return nil, fmt.Errorf("run: get index operand out of range")
			}
			table, ok := registers[ins.b].Table()
			if !ok {
				return nil, fmt.Errorf("run: get index target is %s, want table", registers[ins.b].Kind())
			}
			value, err := table.Get(registers[ins.c])
			if err != nil {
				return nil, fmt.Errorf("run: get index failed: %w", err)
			}
			registers[ins.a] = value

		case opAdd:
			if ins.a < 0 || ins.a >= len(registers) ||
				ins.b < 0 || ins.b >= len(registers) ||
				ins.c < 0 || ins.c >= len(registers) {
				return nil, fmt.Errorf("run: add register out of range")
			}

			left, ok := registers[ins.b].Number()
			if !ok {
				return nil, fmt.Errorf("run: add left operand is %s, want number", registers[ins.b].Kind())
			}
			right, ok := registers[ins.c].Number()
			if !ok {
				return nil, fmt.Errorf("run: add right operand is %s, want number", registers[ins.c].Kind())
			}
			registers[ins.a] = NumberValue(left + right)

		case opCall:
			if ins.a < 0 || ins.a >= len(registers) ||
				ins.b < 0 || ins.b >= len(registers) ||
				ins.c < 0 || ins.b+ins.c >= len(registers) {
				return nil, fmt.Errorf("run: call register out of range")
			}

			fn, ok := registers[ins.b].hostFunction()
			if !ok {
				return nil, fmt.Errorf("run: call target is %s, want host_function", registers[ins.b].Kind())
			}
			if fn == nil {
				return nil, fmt.Errorf("run: call target is nil host_function")
			}

			args := make([]Value, ins.c)
			copy(args, registers[ins.b+1:ins.b+1+ins.c])
			results, err := fn(args)
			if err != nil {
				return nil, fmt.Errorf("run: host function failed: %w", err)
			}
			if len(results) == 0 {
				registers[ins.a] = NilValue()
			} else {
				registers[ins.a] = results[0]
			}

		case opJumpIfFalse:
			if ins.a < 0 || ins.a >= len(registers) {
				return nil, fmt.Errorf("run: jump condition register %d out of range", ins.a)
			}
			if ins.b < 0 || ins.b > len(proto.code) {
				return nil, fmt.Errorf("run: jump target %d out of range", ins.b)
			}
			if !registers[ins.a].truthy() {
				pc = ins.b - 1
			}

		case opJump:
			if ins.b < 0 || ins.b > len(proto.code) {
				return nil, fmt.Errorf("run: jump target %d out of range", ins.b)
			}
			pc = ins.b - 1

		case opReturn:
			if ins.a < 0 || ins.a >= len(registers) {
				return nil, fmt.Errorf("run: return register index %d out of range", ins.a)
			}
			return []Value{registers[ins.a]}, nil

		default:
			return nil, fmt.Errorf("run: unknown opcode %d", ins.op)
		}
	}

	return nil, fmt.Errorf("run: prototype did not return")
}
