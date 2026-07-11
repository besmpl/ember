package ember

import "testing"

func TestOpcodeDietRemovesCompilerUnreachableOperations(t *testing.T) {
	if got, want := int(opcodeCount), 71; got != want {
		t.Fatalf("opcode count = %d, want %d after canonical field-branch lowering", got, want)
	}
}

func TestOpcodeDietPreservesEstablishedWireIDs(t *testing.T) {
	wantIDs := map[opcode]uint8{
		opLoadConst:      1,
		opSetStringField: 8,
		opFastCall:       68,
		opJumpIfFalse:    74,
		opJump:           75,
		opReturnOne:      76,
		opReturn:         77,
	}
	for op, want := range wantIDs {
		if got := uint8(op); got != want {
			t.Errorf("%s wire ID = %d, want %d", opcodeName(op), got, want)
		}
	}
	for _, removed := range []opcode{0, 7, 63, 64, 65, 66, 67} {
		if _, ok := opcodeMetadata(removed); ok {
			t.Errorf("removed wire ID %d still has opcode metadata", removed)
		}
	}
}
