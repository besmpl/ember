package ember

import "testing"

func TestVMStackOwnerCellSurvivesStackGrowth(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturnOne}}, nil, nil, 1, 0, false)
	thread := newVMThread(runtimeGlobals(nil))
	frame := thread.newFrame(proto, nil, nil)
	thread.pushFrame(frame)

	cell := frame.registerCell(0)
	cell.set(NumberValue(7))
	owner := frame.owner
	if owner == nil {
		t.Fatal("captured register has no stack owner")
	}

	// Force the owner's backing slice to move.  The cell keeps the owner and
	// absolute register index, so no pointer rebinding is needed.
	thread.growStack(cap(owner.values) + 1)
	if thread.stackOwner != owner {
		t.Fatal("stack growth replaced the stack owner")
	}
	if got, ok := cell.get().Number(); !ok || got != 7 {
		t.Fatalf("cell after stack growth = %v (%t), want number 7", cell.get(), ok)
	}

	frame.registers[0] = NumberValue(11)
	if got, ok := cell.get().Number(); !ok || got != 11 {
		t.Fatalf("cell did not observe grown register window = %v (%t), want 11", cell.get(), ok)
	}
	cell.set(NumberValue(13))
	if got, ok := frame.registers[0].Number(); !ok || got != 13 {
		t.Fatalf("register after cell set = %v (%t), want 13", frame.registers[0], ok)
	}

	thread.popFrame()
	if cell.owner != nil {
		t.Fatal("released cell still points at a stack owner")
	}
	if got, ok := cell.get().Number(); !ok || got != 13 {
		t.Fatalf("closed cell = %v (%t), want number 13", cell.get(), ok)
	}
}
