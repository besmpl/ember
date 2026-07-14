package ember

import (
	"reflect"
	"testing"
	"unsafe"
)

func TestVMFrameLayout(t *testing.T) {
	if got, want := unsafe.Sizeof(vmFrame{}), uintptr(256); got > want {
		t.Fatalf("vmFrame size = %d, want <= %d bytes", got, want)
	}
	t.Logf("vmFrame size=%d (cold=%d)", unsafe.Sizeof(vmFrame{}), unsafe.Sizeof(vmFrameCold{}))
	typeOfFrame := reflect.TypeOf(vmFrame{})
	for i := 0; i < typeOfFrame.NumField(); i++ {
		field := typeOfFrame.Field(i)
		t.Logf("field %s offset=%d size=%d", field.Name, field.Offset, field.Type.Size())
	}
}

func TestFixedCallsDoNotAllocateColdFrameState(t *testing.T) {
	proto, err := Compile(`
local function add(left, right)
    return left + right
end
local result = add(1, 2)
return result
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	if _, err := thread.run(proto, nil, nil); err != nil {
		t.Fatalf("thread.run returned error: %v", err)
	}
	for index, frame := range thread.frameSlots {
		if frame != nil && frame.cold != nil {
			t.Fatalf("fixed frame slot %d allocated cold state", index)
		}
	}
}
