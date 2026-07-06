package ember

import (
	"errors"
	"testing"
)

func TestVMValueListOwnsInlineBorrowedAndAdjustedValues(t *testing.T) {
	inline := vmInlineValueList(NumberValue(4))
	if inline.len() != 1 {
		t.Fatalf("inline len = %d, want 1", inline.len())
	}
	if got, ok := inline.at(0).Number(); !ok || got != 4 {
		t.Fatalf("inline first value is %v (%t), want number 4", got, ok)
	}
	if !inline.at(1).IsNil() {
		t.Fatalf("inline out-of-range value is %s, want nil", inline.at(1).Kind())
	}

	backing := []Value{NumberValue(1), NumberValue(2)}
	borrowed := vmBorrowedValueList(backing)
	owned := borrowed.ownedValues()
	backing[0] = NumberValue(9)
	if got, _ := owned[0].Number(); got != 1 {
		t.Fatalf("owned borrowed copy changed to %v, want 1", got)
	}

	empty := vmBorrowedValueList(nil)
	adjusted := empty.adjustedOwnedValues()
	if len(adjusted) != 1 || !adjusted[0].IsNil() {
		t.Fatalf("adjusted empty values = %#v, want single nil", adjusted)
	}
}

func TestVMInlineArrayValueListPreservesFixedResultCount(t *testing.T) {
	list := vmInlineArrayValueList([2]Value{StringValue("left"), StringValue("right")}, 2)
	if list.len() != 2 {
		t.Fatalf("inline array len = %d, want 2", list.len())
	}
	if got, _ := list.at(0).String(); got != "left" {
		t.Fatalf("first inline array value is %q, want left", got)
	}
	if got, _ := list.at(1).String(); got != "right" {
		t.Fatalf("second inline array value is %q, want right", got)
	}

	empty := vmInlineArrayValueList([2]Value{NumberValue(1)}, 0)
	if !empty.at(0).IsNil() {
		t.Fatalf("empty inline array first value is %s, want nil", empty.at(0).Kind())
	}
}

func TestVMFrameFixedArgWindowsBorrowOnlySafeRegisters(t *testing.T) {
	proto := newProto(nil, []instruction{{op: opReturnOne}}, nil, nil, 4, 0, false)
	frame := newVMFrame(proto, nil, nil)
	frame.registers[0] = NumberValue(1)
	frame.registers[1] = NumberValue(2)

	borrowed := frame.borrowedFixedCallArgs(0, 2)
	if !borrowed.borrowed {
		t.Fatalf("borrowed fixed args were copied, want borrowed register window")
	}
	frame.registers[0] = NumberValue(9)
	if got, _ := borrowed.values[0].Number(); got != 9 {
		t.Fatalf("borrowed fixed arg stayed %v, want updated register value 9", got)
	}

	frame.registers[0] = NumberValue(1)
	frame.registerCell(1).value = NumberValue(7)
	frame.directRegisters = false
	withCell := frame.borrowedFixedCallArgs(0, 2)
	if withCell.borrowed {
		t.Fatalf("captured-register fixed args borrowed, want copied window")
	}
	frame.registers[0] = NumberValue(9)
	frame.registerCell(1).value = NumberValue(11)
	first, _ := withCell.values[0].Number()
	second, _ := withCell.values[1].Number()
	if first != 1 || second != 7 {
		t.Fatalf("copied fixed args changed to %v, %v; want 1, 7", first, second)
	}

	retained := frame.retainedFixedCallArgs(0, 2)
	if retained.borrowed {
		t.Fatalf("retained fixed args borrowed, want copied window")
	}
}

func TestRuntimeMetamethodScratchPreservesRetainedHostArguments(t *testing.T) {
	var retained [][]Value
	host := HostFuncValue(func(args []Value) ([]Value, error) {
		retained = append(retained, args)
		return nil, nil
	})

	if _, err := callRuntimeMetamethod2(host, nil, StringValue("first"), NumberValue(1)); err != nil {
		t.Fatalf("first metamethod call returned error: %v", err)
	}
	if _, err := callRuntimeMetamethod2(host, nil, StringValue("second"), NumberValue(2)); err != nil {
		t.Fatalf("second metamethod call returned error: %v", err)
	}
	if len(retained) != 2 {
		t.Fatalf("retained %d argument windows, want 2", len(retained))
	}
	first, _ := retained[0][0].String()
	second, _ := retained[1][0].String()
	if first != "first" || second != "second" {
		t.Fatalf("retained argument windows are %q and %q, want first and second", first, second)
	}
}

func TestDirectFrameSideExitContractMapsFrameResults(t *testing.T) {
	exit := directFrameEnterGenericFrame()
	result, complete, err := exit.frameResult()
	if complete || err != nil {
		t.Fatalf("generic-frame exit returned complete %t err %v, want incomplete nil", complete, err)
	}
	if result.state != vmCallStateReturned || result.valuesList.len() != 0 || result.scriptCall.closure != nil {
		t.Fatalf("generic-frame exit returned result %#v, want zero result", result)
	}

	returned := directFrameReturn(vmReturnedValue(NumberValue(7)))
	result, complete, err = returned.frameResult()
	if !complete || err != nil {
		t.Fatalf("return exit returned complete %t err %v, want complete nil", complete, err)
	}
	if got, ok := result.valuesList.at(0).Number(); result.state != vmCallStateReturned || result.valuesList.len() != 1 || !ok || got != 7 {
		t.Fatalf("return exit result = %#v, want returned number 7", result)
	}

	call := directFrameCall(vmFrameResult{state: vmCallStateScriptCall, scriptCall: vmScriptCall{args: []Value{NumberValue(3)}}})
	result, complete, err = call.frameResult()
	if !complete || err != nil {
		t.Fatalf("call exit returned complete %t err %v, want complete nil", complete, err)
	}
	if result.state != vmCallStateScriptCall || len(result.scriptCall.args) != 1 {
		t.Fatalf("call exit result = %#v, want script call with one arg", result)
	}

	yielded := directFrameYield(vmYieldedValues([]Value{StringValue("pause")}))
	result, complete, err = yielded.frameResult()
	if !complete || err != nil {
		t.Fatalf("yield exit returned complete %t err %v, want complete nil", complete, err)
	}
	if result.state != vmCallStateYielded || result.valuesList.len() != 1 || result.valuesList.at(0).str != "pause" {
		t.Fatalf("yield exit result = %#v, want yielded pause value", result)
	}

	failure := errors.New("boom")
	result, complete, err = directFrameFail(failure).frameResult()
	if !complete || !errors.Is(err, failure) {
		t.Fatalf("fail exit returned complete %t err %v, want complete boom", complete, err)
	}
	if result.state != vmCallStateReturned || result.valuesList.len() != 0 || result.scriptCall.closure != nil {
		t.Fatalf("fail exit returned result %#v, want zero result", result)
	}

	if !directFrameResume().resumesDirectFrame() {
		t.Fatalf("resume exit does not report direct-frame resume")
	}
}
