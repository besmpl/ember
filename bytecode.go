package ember

type opcode uint8

const (
	opLoadConst opcode = iota
	opLoadGlobal
	opMove
	opNewTable
	opSetField
	opGetField
	opSetIndex
	opGetIndex
	opAdd
	opCall
	opJumpIfFalse
	opJump
	opReturn
)

type instruction struct {
	op opcode
	a  int
	b  int
	c  int
}

// Proto is an executable Ember function prototype.
type Proto struct {
	constants []Value
	code      []instruction
	registers int
}

func newProto(constants []Value, code []instruction, registers int) *Proto {
	return &Proto{
		constants: constants,
		code:      code,
		registers: registers,
	}
}
