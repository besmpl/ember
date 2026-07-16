package ember

import "testing"

func TestCompilerEmitsImplicitReturnAfterPartialConditionalReturn(t *testing.T) {
	proto, err := Compile(`
local function choose(flag)
    if flag then
        return 1
    end
end
return choose(false)
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(proto.prototypes) != 1 {
		t.Fatalf("nested Proto count = %d, want 1", len(proto.prototypes))
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	child := image.prototypes[1]
	if len(child.operations) == 0 {
		t.Fatal("partial-return function emitted no instructions")
	}
	if child.operations[len(child.operations)-1].op != opReturn {
		t.Fatalf("partial-return function ends with %v, want implicit RETURN", child.operations[len(child.operations)-1].op)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("partial-return result = %#v, want zero results", values)
	}
}
