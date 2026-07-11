package ember

import "testing"

func TestOpcodeDietRemovesCompilerUnreachableOperations(t *testing.T) {
	const executableOpcodeCeiling = 69
	if got := int(opcodeCount); got > executableOpcodeCeiling {
		t.Fatalf("executable opcode count = %d, want at most %d", got, executableOpcodeCeiling)
	}
	if got := int(opcodeCount); got != 69 {
		t.Fatalf("executable opcode count = %d, want 69 after removing unreachable field arithmetic", got)
	}
	seen := make(map[opcode]struct{}, opcodeCount)
	for _, op := range allOpcodes {
		if _, duplicate := seen[op]; duplicate {
			t.Fatalf("executable opcode %d appears more than once", op)
		}
		seen[op] = struct{}{}
		meta, ok := opcodeMetadata(op)
		if !ok || !meta.effects.classified {
			t.Fatalf("executable opcode %d is missing validated metadata", op)
		}
	}
	for op := opcode(0); op < opcodeLimit; op++ {
		if _, ok := opcodeMetadata(op); ok {
			if _, listed := seen[op]; !listed {
				t.Fatalf("opcode metadata for %d is not in executable opcode set", op)
			}
		}
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
	for _, removed := range []opcode{0, 7, 12, 13, 63, 64, 65, 66, 67} {
		if _, ok := opcodeMetadata(removed); ok {
			t.Errorf("removed wire ID %d still has opcode metadata", removed)
		}
	}
}
