package ember

import "testing"

func TestProtoConstantTableKeysPreserveStringIdentity(t *testing.T) {
	constant := StringValue("field")
	keys, ok := protoConstantTableKeys([]Value{constant, NumberValue(1)})

	if len(keys) != 2 || len(ok) != 2 || !ok[0] || ok[1] {
		t.Fatalf("constant key flags = %v; want [true false]", ok)
	}
	box := constant.stringBox()
	if keys[0].strBox != box {
		t.Fatal("constant table key dropped its string box")
	}
	if keys[0].strHash != box.hash {
		t.Fatalf("constant table key hash = %d; want %d", keys[0].strHash, box.hash)
	}
}
