package ember

import "testing"

func TestVMStackOwnerCellSurvivesStackGrowth(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturnOne}}, nil, nil, 1, 0, false)
	thread := newVMThread(runtimeGlobals(nil))
	frame := thread.newFrame(proto, nil, nil)
	thread.pushFrame(frame)

	cell := thread.registerCell(frame, 0)
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

func TestVMThreadOpenUpvaluesDeduplicateByOwnerAndRegister(t *testing.T) {
	thread := newVMThread(runtimeGlobals(nil))
	owner := thread.ensureStackOwner()
	thread.growStack(2)

	first := thread.openUpvalue(owner, 1)
	second := thread.openUpvalue(owner, 1)
	if first == nil || second == nil {
		t.Fatal("open upvalue is nil")
	}
	if first != second {
		t.Fatal("same owner/register returned distinct open upvalue cells")
	}
	if got, want := len(thread.openUpvalues), 1; got != want {
		t.Fatalf("open upvalue index size = %d, want %d", got, want)
	}

	other := thread.openUpvalue(owner, 0)
	if other == first {
		t.Fatal("different register reused the same open upvalue cell")
	}
	if got, want := len(thread.openUpvalues), 2; got != want {
		t.Fatalf("open upvalue index size after second register = %d, want %d", got, want)
	}
	otherOwner := &vmStackOwner{values: make([]Value, 2)}
	third := thread.openUpvalue(otherOwner, 1)
	if third == first || third == other {
		t.Fatal("different stack owner reused an open upvalue cell")
	}
	if got, want := len(thread.openUpvalues), 3; got != want {
		t.Fatalf("open upvalue index size after second owner = %d, want %d", got, want)
	}
}

func TestVMThreadIndexesDeclaredCapturedLocals(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturnOne}}, nil, nil, 1, 0, false)
	proto.capturedLocals = []bool{true}
	thread := newVMThread(runtimeGlobals(nil))
	frame := thread.newFrame(proto, nil, nil)

	if got, want := len(thread.openUpvalues), 1; got != want {
		t.Fatalf("declared captured-local index size = %d, want %d", got, want)
	}
	if len(frame.cells) != 1 || frame.cells[0] == nil {
		t.Fatalf("declared captured local has cells %#v", frame.cells)
	}
	if frame.cells[0] != thread.openUpvalues[0] {
		t.Fatal("frame and thread retained distinct cells for one captured local")
	}
}

func TestVMThreadCloseOpenUpvaluesBeforeStackTruncation(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturnOne}}, nil, nil, 1, 0, false)
	thread := newVMThread(runtimeGlobals(nil))
	frame := thread.newFrame(proto, nil, nil)
	thread.pushFrame(frame)
	cell := thread.registerCell(frame, 0)
	cell.set(NumberValue(17))
	owner := frame.owner
	if owner == nil {
		t.Fatal("frame has no stack owner")
	}

	thread.releaseFrameWindow(frame)
	if cell.owner != nil {
		t.Fatal("released frame left an open upvalue owner")
	}
	if got, ok := cell.get().Number(); !ok || got != 17 {
		t.Fatalf("closed upvalue = %v (%t), want number 17", cell.get(), ok)
	}
	if got := len(owner.values); got != 0 {
		t.Fatalf("stack length after frame release = %d, want 0", got)
	}
	if got := len(thread.openUpvalues); got != 0 {
		t.Fatalf("open upvalue index after frame release = %d, want 0", got)
	}
}

func TestVMThreadSuspendResumePreservesOpenUpvalueIndex(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturnOne}}, nil, nil, 1, 0, false)
	thread := newVMThread(runtimeGlobals(nil))
	frame := thread.newFrame(proto, nil, nil)
	thread.pushFrame(frame)
	cell := thread.registerCell(frame, 0)
	cell.set(NumberValue(23))

	suspended := thread.suspendFrames()
	if got, want := len(suspended.openUpvalues), 1; got != want {
		t.Fatalf("suspended open upvalue index size = %d, want %d", got, want)
	}
	if got := len(thread.openUpvalues); got != 0 {
		t.Fatalf("live thread retained %d open upvalues after suspend", got)
	}

	thread.resumeFrames(suspended)
	if got, want := len(thread.openUpvalues), 1; got != want {
		t.Fatalf("resumed open upvalue index size = %d, want %d", got, want)
	}
	thread.releaseFrameWindow(frame)
	if cell.owner != nil {
		t.Fatal("released resumed frame left an open upvalue owner")
	}
}

func TestVMThreadResetClosesOpenUpvalues(t *testing.T) {
	thread := newVMThread(runtimeGlobals(nil))
	owner := thread.ensureStackOwner()
	thread.growStack(1)
	cell := thread.openUpvalue(owner, 0)
	cell.set(NumberValue(31))

	thread.resetForPool()
	if got := len(thread.openUpvalues); got != 0 {
		t.Fatalf("pooled thread retained %d open upvalues", got)
	}
	if cell.owner != nil {
		t.Fatal("pooled thread left an upvalue open")
	}
	if got, ok := cell.get().Number(); !ok || got != 31 {
		t.Fatalf("closed pooled upvalue = %v (%t), want number 31", cell.get(), ok)
	}
}
