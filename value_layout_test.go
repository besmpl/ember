package ember

import (
	"context"
	"math"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestValueLayoutMatchesPointerWidth(t *testing.T) {
	want := expectedArchitectureLayoutSize(12, 16)
	if got := reflect.TypeOf(Value{}).Size(); got != want {
		t.Fatalf("Value size = %d bytes, want exactly %d for this pointer width", got, want)
	}
}

func TestValueSemanticMatrixRoundTripsScalarsAndReferences(t *testing.T) {
	finite := []float64{0, math.Copysign(0, -1), 1.5, -42.25, math.Inf(1), math.Inf(-1),
		math.Float64frombits(0x7ff8000000000001), math.Float64frombits(0x7ff0000000000001)}
	for _, want := range finite {
		got, ok := NumberValue(want).Number()
		if !ok || math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("number round trip bits = %#x, %t; want %#x, true", math.Float64bits(got), ok, math.Float64bits(want))
		}
	}

	for _, want := range []bool{false, true} {
		got, ok := BoolValue(want).Bool()
		if !ok || got != want {
			t.Fatalf("bool round trip = %v, %t; want %v, true", got, ok, want)
		}
	}
	if got := NilValue(); !got.IsNil() || got.Kind() != NilKind {
		t.Fatalf("nil round trip = %#v; want zero nil Value", got)
	}

	left := StringValue("ember")
	right := StringValue(strings.Join([]string{"em", "ber"}, ""))
	if left.Kind() != StringKind || right.Kind() != StringKind || !valuesEqual(left, right) {
		t.Fatalf("equal separately boxed strings did not round trip: %#v %#v", left, right)
	}
	if left.stringBox() == right.stringBox() {
		t.Fatal("StringValue unexpectedly reused the same box")
	}

	table := NewTable()
	userdata := NewUserData("payload")
	proto := newProto(nil, []instruction{{op: opReturn}}, nil, nil, 0, 0, false)
	closureValue := functionValue(proto, nil)
	host := HostFuncValue(func(args []Value) ([]Value, error) { return args, nil })
	contextHost := ContextHostFuncValue(func(_ context.Context, args []Value) ([]Value, error) { return args, nil })
	native := nativeFuncValueWithID(baseRawLenNative, nativeFuncRawLen)

	if got, ok := TableValue(table).Table(); !ok || got != table {
		t.Fatalf("table round trip = %p, %t; want %p, true", got, ok, table)
	}
	if got, ok := UserDataValue(userdata).UserData(); !ok || got != userdata {
		t.Fatalf("userdata round trip = %p, %t; want %p, true", got, ok, userdata)
	}
	if got, ok := closureValue.scriptFunction(); !ok || got == nil || got.proto != proto {
		t.Fatalf("closure round trip = %#v, %t; want closure for proto", got, ok)
	}
	if got, ok := host.hostFunction(); !ok || got == nil {
		t.Fatalf("host function round trip = %v, %t; want callback", got, ok)
	}
	if got, ok := contextHost.nativeFunction(); !ok || got == nil {
		t.Fatalf("context host round trip = %v, %t; want native callback", got, ok)
	}
	if got, ok := contextHost.contextHostFunction(); !ok || got == nil {
		t.Fatalf("context host direct round trip = %v, %t; want context callback", got, ok)
	}
	if got, ok := native.nativeFunction(); !ok || got == nil {
		t.Fatalf("native ID round trip = %v, %t; want native callback", got, ok)
	}

	if got := TableValue(nil); got.Kind() != TableKind {
		t.Fatalf("typed nil table kind = %s, want table", got.Kind())
	} else if _, ok := got.Table(); ok {
		t.Fatal("typed nil table unexpectedly exposed an object")
	}
	if got := UserDataValue(nil); got.Kind() != UserDataKind {
		t.Fatalf("typed nil userdata kind = %s, want userdata", got.Kind())
	} else if _, ok := got.UserData(); ok {
		t.Fatal("typed nil userdata unexpectedly exposed an object")
	}
	if got := closureFunctionValue(nil); got.Kind() != FunctionKind {
		t.Fatalf("typed nil closure kind = %s, want function", got.Kind())
	} else if _, ok := got.scriptFunction(); ok {
		t.Fatal("typed nil closure unexpectedly exposed an object")
	}
	if got := HostFuncValue(nil); got.Kind() != HostFuncKind {
		t.Fatalf("typed nil host kind = %s, want host_function", got.Kind())
	} else if _, ok := got.hostFunction(); ok {
		t.Fatal("typed nil host unexpectedly exposed a callback")
	}
}

func TestValueReferencesRemainVisibleAfterForcedGC(t *testing.T) {
	table := NewTable()
	userdata := NewUserData(&struct{ text string }{text: "payload"})
	proto := newProto(nil, []instruction{{op: opReturn}}, nil, nil, 0, 0, false)
	closureValue := functionValue(proto, nil)
	hostValue := HostFuncValue(func(args []Value) ([]Value, error) { return args, nil })
	stringValue := StringValue("gc-visible")
	values := []Value{StringValue(stringValue.stringText()), TableValue(table), UserDataValue(userdata), closureValue, hostValue}

	table = nil
	userdata = nil
	proto = nil
	runtime.GC()
	runtime.GC()

	if got, ok := values[0].String(); !ok || got != "gc-visible" {
		t.Fatalf("string after GC = %q, %t; want gc-visible, true", got, ok)
	}
	if got, ok := values[1].Table(); !ok || got == nil {
		t.Fatal("table Value lost its object after forced GC")
	}
	if got, ok := values[2].UserData(); !ok || got == nil || got.Payload() == nil {
		t.Fatal("userdata Value lost its object or payload after forced GC")
	}
	if got, ok := values[3].scriptFunction(); !ok || got == nil || got.proto == nil {
		t.Fatal("closure Value lost its object after forced GC")
	}
	if got, ok := values[4].hostFunction(); !ok || got == nil {
		t.Fatal("host callable Value lost its object after forced GC")
	}
	runtime.KeepAlive(values)
}

func TestValueNativeIDRequiresHostFunctionKind(t *testing.T) {
	for id := nativeFuncID(1); id <= nativeFuncTableNext; id++ {
		number := NumberValue(math.Float64frombits(uint64(id) << valueNativeIDShift))
		if got := valueNativeID(number); got != nativeFuncUnknown {
			t.Fatalf("number payload matching native ID %d decoded as %d", id, got)
		}
	}

	native := nativeFuncValueWithID(baseRawLenNative, nativeFuncRawLen)
	if got := valueNativeID(native); got != nativeFuncRawLen {
		t.Fatalf("native host function ID = %d, want %d", got, nativeFuncRawLen)
	}
}
