package ember

import (
	"reflect"
	"sync"
	"testing"
)

func TestMachineOpcodeMetadataIsComplete(t *testing.T) {
	eligible := make(map[opcode]bool, len(machineEligibleOpcodes))
	for _, op := range machineEligibleOpcodes {
		eligible[op] = true
	}
	for _, op := range allOpcodes {
		meta, ok := opcodeMetadata(op)
		if !ok {
			t.Fatalf("opcode %s has no metadata", opcodeName(op))
		}
		if !meta.machine.classified || meta.machine.guestCharge == 0 || meta.machine.safepoint == opcodeMachineSafepointUnclassified {
			t.Fatalf("opcode %s has incomplete Machine policy: %#v", opcodeName(op), meta.machine)
		}
		if meta.machine.eligible != eligible[op] {
			t.Fatalf("opcode %s eligibility = %t, want %t", opcodeName(op), meta.machine.eligible, eligible[op])
		}
	}
}

func TestPrepareCodeImageIsDeterministicAndMapped(t *testing.T) {
	proto, err := Compile("local total = 0\nwhile total < 3 do\n total = total + 1\nend\nreturn total")
	if err != nil {
		t.Fatal(err)
	}
	first, err := prepareCodeImage(proto)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepareCodeImage(proto)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("code image preparation is not deterministic:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if !first.eligible || len(first.operations) == 0 || len(first.blocks) < 2 {
		t.Fatalf("prepared image = eligible:%t operations:%d blocks:%d, want eligible control-flow image", first.eligible, len(first.operations), len(first.blocks))
	}
	for index, operation := range first.operations {
		if operation.wordPC < 0 || int(operation.wordPC) >= len(proto.words) {
			t.Fatalf("operation %d word PC = %d out of range", index, operation.wordPC)
		}
		if operation.line <= 0 {
			t.Fatalf("operation %d source line = %d, want positive line", index, operation.line)
		}
		meta, _ := opcodeMetadata(operation.op)
		if operation.guestCharge != meta.machine.guestCharge || operation.errorClass != meta.machine.errorClass {
			t.Fatalf("operation %d policy drifted from opcode metadata", index)
		}
	}
}

func TestPrepareCodeImageMarksOwnerAndDetachRequirements(t *testing.T) {
	tests := []struct {
		name          string
		source        string
		detachable    bool
		requiresOwner bool
	}{
		{name: "escaping closure alias", source: `local function value() return 1 end local copy = value return copy`},
		{name: "global", source: `return math.pi`, detachable: true, requiresOwner: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proto, err := Compile(test.source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := prepareCodeImage(proto)
			if err != nil {
				t.Fatal(err)
			}
			if !image.eligible || image.detachable != test.detachable || image.requiresOwner != test.requiresOwner {
				t.Fatalf("image = eligible:%t detachable:%t requiresOwner:%t reason:%q", image.eligible, image.detachable, image.requiresOwner, image.rejectReason)
			}
			if _, err := Run(proto); err != nil {
				t.Fatalf("old-VM fallback failed: %v", err)
			}
		})
	}

	malformed := &Proto{registers: 1, words: []wordcodeWord{wordcodeOpcodeMask}}
	if _, err := prepareCodeImage(malformed); err == nil {
		t.Fatal("prepareCodeImage accepted malformed wordcode")
	}
}

func TestPrepareCodeImageRejectsUnprovenNumericStringFromTable(t *testing.T) {
	proto, err := Compile(`local values = {amount = "40"} return values.amount + 2`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := prepareCodeImage(proto)
	if err != nil {
		t.Fatal(err)
	}
	if image.eligible || image.rejectReason == "" {
		t.Fatalf("image = eligible:%t reason:%q, want conservative numeric-string rejection", image.eligible, image.rejectReason)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("Run returned %d values, want 1", len(values))
	}
	if number, ok := values[0].Number(); !ok || number != 42 {
		t.Fatalf("Run result = %v (%t), want old-VM coercion result 42", number, ok)
	}
}

func TestPreparedCodeImageIsSharedConcurrently(t *testing.T) {
	proto, err := Compile("local total = 0 for i = 1, 20 do total = total + i end return total")
	if err != nil {
		t.Fatal(err)
	}
	const workers = 32
	images := make(chan *codeImage, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			image, err := proto.preparedCodeImage()
			images <- image
			errors <- err
		}()
	}
	group.Wait()
	close(images)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first *codeImage
	for image := range images {
		if first == nil {
			first = image
		}
		if image != first {
			t.Fatal("concurrent preparation returned distinct cached images")
		}
	}
}
