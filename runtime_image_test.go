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

func TestPrepareCodeImageAllowsHelperBackedNumericStringFromTable(t *testing.T) {
	proto, err := Compile(`local values = {amount = "40"} return values.amount + 2`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := prepareCodeImage(proto)
	if err != nil {
		t.Fatal(err)
	}
	if !image.eligible || image.rejectReason != "" {
		t.Fatalf("image = eligible:%t reason:%q, want helper-backed numeric-string eligibility", image.eligible, image.rejectReason)
	}
	if !image.requiresNumericCoercion {
		t.Fatal("image did not record its numeric-coercion executor requirement")
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

func TestPrepareCodeImageRecordsHelperRequirementsWithoutWorkloadIdentity(t *testing.T) {
	tests := []struct {
		name          string
		source        string
		wantCoercion  bool
		wantGenerated bool
		wantEligible  bool
	}{
		{
			name:         "proven numeric with unrelated string",
			source:       `local label = "hp" local value = 40 return value + 2, label`,
			wantEligible: true,
		},
		{
			name:         "string comparison is not numeric coercion",
			source:       `local values = {"10", "2"} return values[1] < values[2]`,
			wantEligible: true,
		},
		{
			name: "table_fields",
			source: `
local player = {stats = {hp = 100, shield = 25}, inventory = {coins = 3}}
local i = 0
while i < 80 do
    i = i + 1
    player.stats.hp = player.stats.hp + player.stats.shield - player.inventory.coins
end
return player.stats.hp
`,
			wantCoercion: true,
			wantEligible: true,
		},
		{
			name: "behavior_tree",
			source: `
local blackboard = {hp = 65, ammo = 6}
local nodes = {
    {kind = "condition", key = "hp", threshold = 35, pass = 2},
    {kind = "action", weight = 15},
}
local node = nodes[1]
local value = blackboard[node.key]
if node.kind == "condition" and value > node.threshold then
    blackboard.ammo = blackboard.ammo - 1
end
return blackboard.ammo + nodes[2].weight
`,
			wantCoercion: true,
			wantEligible: true,
		},
		{
			name:          "generated concat",
			source:        `local values = {25} return "hp:" .. values[1] .. "/ready"`,
			wantGenerated: true,
			wantEligible:  true,
		},
		{
			name:          "numeric tostring",
			source:        `return tostring(123456)`,
			wantGenerated: true,
			wantEligible:  true,
		},
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
			if image.requiresNumericCoercion != test.wantCoercion || image.requiresGeneratedStrings != test.wantGenerated {
				t.Fatalf("facts = coercion:%t generated:%t, want coercion:%t generated:%t (eligible:%t reason:%q operations:%#v)",
					image.requiresNumericCoercion, image.requiresGeneratedStrings,
					test.wantCoercion, test.wantGenerated, image.eligible, image.rejectReason, image.operations)
			}
			if image.eligible != test.wantEligible {
				t.Fatalf("eligible = %t, want %t (reason %q)", image.eligible, test.wantEligible, image.rejectReason)
			}
		})
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
