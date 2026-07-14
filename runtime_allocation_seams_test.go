package ember

import (
	"context"
	"reflect"
	"testing"
)

// RuntimeAllocationMachineForTest exposes the current VM bind/close seam to
// the external acceptance harness without adding a production API. It is
// deliberately test-only: Phase 0 measures the implementation that exists
// before CodeImage and Machine become real runtime types.
type RuntimeAllocationMachineForTest struct {
	owner  *runtimeOwner
	thread *vmThread
}

func NewRuntimeAllocationMachineForTest() *RuntimeAllocationMachineForTest {
	return &RuntimeAllocationMachineForTest{owner: newRuntimeOwner()}
}

func (machine *RuntimeAllocationMachineForTest) Bind() error {
	if machine.thread != nil {
		return errRuntimeOwnerBusy
	}
	machine.thread = acquireVMThread(context.Background(), nil)
	if err := machine.thread.bindOwner(machine.owner); err != nil {
		releaseVMThread(machine.thread)
		machine.thread = nil
		return err
	}
	return nil
}

func (machine *RuntimeAllocationMachineForTest) Detach() {
	if machine.thread != nil {
		machine.thread.unbindOwner()
	}
}

func (machine *RuntimeAllocationMachineForTest) Close() error {
	if machine.thread != nil {
		releaseVMThread(machine.thread)
		machine.thread = nil
	}
	return machine.owner.close()
}

func RuntimeAllocationPrepareImageForTest(proto *Proto) (*Proto, error) {
	if proto == nil {
		return nil, nil
	}
	code, err := protoDecodedInstructions(proto)
	if err != nil {
		return nil, err
	}
	prototypes := make([]*Proto, len(proto.prototypes))
	for i, child := range proto.prototypes {
		prototypes[i], err = RuntimeAllocationPrepareImageForTest(child)
		if err != nil {
			return nil, err
		}
	}
	prepared := newProtoWithDescriptors(
		append([]Value(nil), proto.constants...),
		code,
		prototypes,
		append([]upvalueDesc(nil), proto.upvalues...),
		proto.registers,
		proto.params,
		proto.variadic,
	)
	prepared.lines = append([]int(nil), proto.lines...)
	prepared.debugInfo = proto.debugInfo
	if err := finalizeProtoExecutionArtifact(prepared, code); err != nil {
		return nil, err
	}
	return prepared, nil
}

func RuntimeAllocationDetachValuesForTest(values []Value) []Value {
	frame := &vmFrame{window: vmRegisterWindow{borrowed: true}}
	result := stabilizeFrameResultBeforeRelease(frame, vmReturnedBorrowedValues(values))
	return result.values()
}

// RuntimeAllocationRetainedBytesForTest returns deterministic lower-bound
// bytes owned by the current typed handle slabs and explicit root/pin records.
// Map bucket overhead is intentionally excluded; the counters describe Ember
// ownership rather than process-wide heap noise.
func RuntimeAllocationRetainedBytesForTest(runtime *Runtime) (arena, roots uint64) {
	if runtime == nil || runtime.owner == nil {
		return 0, 0
	}
	owner := runtime.owner
	owner.mu.Lock()
	defer owner.mu.Unlock()
	heap := owner.heap
	arena += uint64(cap(heap.boxedNumbers.entries)) * uint64(reflect.TypeOf(slotNumberEntry{}).Size())
	arena += uint64(cap(heap.boxedNumbers.free)) * uint64(reflect.TypeOf(uint32(0)).Size())
	arena += slabRetainedBytesForTest(&heap.strings)
	arena += slabRetainedBytesForTest(&heap.tables)
	arena += slabRetainedBytesForTest(&heap.closures)
	arena += slabRetainedBytesForTest(&heap.upvalues)
	arena += slabRetainedBytesForTest(&heap.userdata)
	arena += slabRetainedBytesForTest(&heap.hostCallables)
	roots += uint64(len(owner.roots)) * uint64(reflect.TypeOf(runtimeRoot{}).Size())
	roots += uint64(len(owner.pins)) * uint64(reflect.TypeOf(runtimePin{}).Size())
	return arena, roots
}

func slabRetainedBytesForTest[T comparable](slab *slotSlab[T]) uint64 {
	entrySize := uint64(reflect.TypeOf(slotSlabEntry[T]{}).Size())
	indexSize := uint64(reflect.TypeOf(uint32(0)).Size())
	return uint64(cap(slab.entries))*entrySize + uint64(cap(slab.free))*indexSize
}

func TestRuntimeAllocationSeamsUseProductionTransitions(t *testing.T) {
	proto, err := Compile("return 1, 2, 3, 4")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := RuntimeAllocationPrepareImageForTest(proto)
	if err != nil {
		t.Fatal(err)
	}
	if prepared == proto {
		t.Fatal("image preparation reused the already finalized prototype")
	}
	results, err := Run(prepared)
	if err != nil {
		t.Fatal(err)
	}
	detached := RuntimeAllocationDetachValuesForTest(results)
	if len(detached) != 4 || &detached[0] == &results[0] {
		t.Fatal("result stabilization did not detach the borrowed result window")
	}
	results[0] = NilValue()
	if number, ok := detached[0].Number(); !ok || number != 1 {
		t.Fatalf("detached result changed with source window: %v", detached[0])
	}

	machine := NewRuntimeAllocationMachineForTest()
	if err := machine.Bind(); err != nil {
		t.Fatal(err)
	}
	if !machine.thread.ownerBound {
		t.Fatal("machine bind did not checkout the owner cache bundle")
	}
	machine.Detach()
	if machine.thread.ownerBound {
		t.Fatal("machine detach did not return the owner cache bundle")
	}
	if err := machine.Close(); err != nil {
		t.Fatal(err)
	}
}
