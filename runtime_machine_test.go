package ember

import (
	"context"
	"fmt"
	"math"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestPublicRunUsesSharedScalarImageConcurrently(t *testing.T) {
	proto := mustEligibleMachineProto(t, "local total = 0 for i = 1, 100 do total = total + i end return total")
	const workers = 16
	var group sync.WaitGroup
	errors := make(chan error, workers)
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 25 {
				values, err := Run(proto)
				if err != nil {
					errors <- err
					return
				}
				if number, ok := values[0].Number(); !ok || number != 5050 {
					errors <- fmt.Errorf("Run result = %v, want 5050", values)
					return
				}
			}
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
}

func TestScalarMachineRunsNestedNonCapturingFunction(t *testing.T) {
	proto, err := Compile(`local function add(a, b) return a + b end return add(20, 22)`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if !image.eligible {
		t.Fatalf("nested noncapturing function image is ineligible: %s", image.rejectReason)
	}
	if len(image.prototypes) != 2 || !image.prototypes[1].eligible {
		t.Fatalf("nested image prototypes = %d, child eligible = %t; want one eligible child", len(image.prototypes), len(image.prototypes) == 2 && image.prototypes[1].eligible)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("Run returned %d values, want 1", len(values))
	}
	if number, ok := values[0].Number(); !ok || number != 42 {
		t.Fatalf("Run result = %v (%t), want 42", number, ok)
	}
	oldValues, oldErr := executeProto(context.Background(), proto, nil, executeOptions{instrumented: true})
	if oldErr != nil {
		t.Fatal(oldErr)
	}
	if !valuesSliceEquivalent(oldValues, values) {
		t.Fatalf("Machine result differs from old VM: old=%s machine=%s", valuesDiagnostic(oldValues), valuesDiagnostic(values))
	}
}

func TestScalarMachineRunsRecursiveFibonacci(t *testing.T) {
	proto, err := Compile(`
		local function fib(n)
			if n < 2 then return n end
			return fib(n - 1) + fib(n - 2)
		end
		return fib(10)
	`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if !image.eligible {
		child := ""
		if len(proto.prototypes) != 0 {
			child = "\nchild:\n" + strings.Join(disassembleProto(proto.prototypes[0]), "\n")
		}
		t.Fatalf("recursive image is ineligible: %s\ndisassembly:\n%s%s", image.rejectReason, strings.Join(disassembleProto(proto), "\n"), child)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("Run returned %d values, want 1", len(values))
	}
	if number, ok := values[0].Number(); !ok || number != 55 {
		t.Fatalf("Run result = %v (%t), want 55", number, ok)
	}
	oldValues, oldErr := executeProto(context.Background(), proto, nil, executeOptions{instrumented: true})
	if oldErr != nil {
		t.Fatal(oldErr)
	}
	if !valuesSliceEquivalent(oldValues, values) {
		t.Fatalf("Machine result differs from old VM: old=%s machine=%s", valuesDiagnostic(oldValues), valuesDiagnostic(values))
	}
}

func TestScalarMachineForwardsMultipleResultsThroughTailCalls(t *testing.T) {
	proto, err := Compile(`
		local function pair(value)
			return value, value + 1
		end
		local function forward(value)
			return pair(value)
		end
		return forward(20)
	`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if !image.eligible {
		t.Fatalf("multi-result image is ineligible: %s", image.rejectReason)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 {
		t.Fatalf("Run returned %d values, want 2", len(values))
	}
	for index, want := range []float64{20, 21} {
		if number, ok := values[index].Number(); !ok || number != want {
			t.Fatalf("Run result %d = %v (%t), want %v", index, number, ok, want)
		}
	}
	oldValues, oldErr := executeProto(context.Background(), proto, nil, executeOptions{instrumented: true})
	if oldErr != nil {
		t.Fatal(oldErr)
	}
	if !valuesSliceEquivalent(oldValues, values) {
		t.Fatalf("Machine result differs from old VM: old=%s machine=%s", valuesDiagnostic(oldValues), valuesDiagnostic(values))
	}
}

func TestScalarMachineSharesNestedMutableCapture(t *testing.T) {
	proto, err := Compile(`
		local function count()
			local value = 0
			local function add(step)
				value = value + step
				return value
			end
			return add(1), add(2)
		end
		return count()
	`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if !image.eligible {
		disassembly := strings.Join(disassembleProto(proto), "\n")
		for index, child := range proto.prototypes {
			disassembly += fmt.Sprintf("\nchild %d:\n%s", index, strings.Join(disassembleProto(child), "\n"))
			for nestedIndex, nested := range child.prototypes {
				disassembly += fmt.Sprintf("\nchild %d.%d:\n%s", index, nestedIndex, strings.Join(disassembleProto(nested), "\n"))
			}
		}
		t.Fatalf("capturing image is ineligible: %s\n%s", image.rejectReason, disassembly)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 {
		t.Fatalf("Run returned %d values, want 2", len(values))
	}
	for index, want := range []float64{1, 3} {
		if number, ok := values[index].Number(); !ok || number != want {
			t.Fatalf("Run result %d = %v (%t), want %v", index, number, ok, want)
		}
	}
	oldValues, oldErr := executeProto(context.Background(), proto, nil, executeOptions{instrumented: true})
	if oldErr != nil {
		t.Fatal(oldErr)
	}
	if !valuesSliceEquivalent(oldValues, values) {
		t.Fatalf("Machine result differs from old VM: old=%s machine=%s", valuesDiagnostic(oldValues), valuesDiagnostic(values))
	}
}

func TestScalarMachineForwardsOpenVarargs(t *testing.T) {
	proto, err := Compile(`
		local function relay(...)
			return ...
		end
		local function take(first, second, third)
			return first, second, third
		end
		return take(relay(7, nil, 9))
	`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if !image.eligible {
		t.Fatalf("vararg image is ineligible: %s", image.rejectReason)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 3 {
		t.Fatalf("Run returned %d values, want 3", len(values))
	}
	if number, ok := values[0].Number(); !ok || number != 7 {
		t.Fatalf("first result = %v (%t), want 7", number, ok)
	}
	if values[1].Kind() != NilKind {
		t.Fatalf("second result kind = %s, want nil", values[1].Kind())
	}
	if number, ok := values[2].Number(); !ok || number != 9 {
		t.Fatalf("third result = %v (%t), want 9", number, ok)
	}
	oldValues, oldErr := executeProto(context.Background(), proto, nil, executeOptions{instrumented: true})
	if oldErr != nil {
		t.Fatal(oldErr)
	}
	if !valuesSliceEquivalent(oldValues, values) {
		t.Fatalf("Machine result differs from old VM: old=%s machine=%s", valuesDiagnostic(oldValues), valuesDiagnostic(values))
	}
}

func TestScalarMachineTailCallsReuseCallDepth(t *testing.T) {
	proto, err := Compile(`
		local function sum(count, total)
			if count == 0 then return total end
			return sum(count - 1, total + count)
		end
		return sum(1000, 0)
	`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if !image.eligible {
		t.Fatalf("tail-call image is ineligible: %s", image.rejectReason)
	}
	controller, err := newExecutionController(context.Background(), ExecutionLimits{MaxCallDepth: 4})
	if err != nil {
		t.Fatal(err)
	}
	values, err := executeCodeImage(image, controller)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("Machine returned %d values, want 1", len(values))
	}
	if number, ok := values[0].Number(); !ok || number != 500500 {
		t.Fatalf("Machine result = %v (%t), want 500500", number, ok)
	}
	publicValues, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if !valuesSliceEquivalent(publicValues, values) {
		t.Fatalf("public Run differs from Machine: public=%s machine=%s", valuesDiagnostic(publicValues), valuesDiagnostic(values))
	}
	machine, err := bindScalarMachine(image, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseScalarMachine(machine)
	if _, err := runGeneratedScalarMachineLoop(machine); err != nil {
		t.Fatal(err)
	}
	if len(machine.continuations) != 0 || len(machine.closures.openCells) != 0 {
		t.Fatalf("tail run retained continuations=%d open_cells=%d", len(machine.continuations), len(machine.closures.openCells))
	}
	if len(machine.tableNumbers) != 0 {
		t.Fatalf("ordinary numeric tail run retained %d boxed transfer numbers", len(machine.tableNumbers))
	}
	maxRegisters := 0
	for _, prepared := range image.prototypes {
		if prepared.registers > maxRegisters {
			maxRegisters = prepared.registers
		}
	}
	if len(machine.registers) > maxRegisters {
		t.Fatalf("tail run register stack length=%d, want at most one frame (%d)", len(machine.registers), maxRegisters)
	}
	oldController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	machineController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: 100_000})
	if err != nil {
		t.Fatal(err)
	}
	oldValues, oldErr := executeProto(context.Background(), proto, nil, executeOptions{controller: oldController, instrumented: true})
	machineValues, machineErr := executeCodeImage(image, machineController)
	if difference := errorsEquivalent(oldErr, machineErr); difference != "" {
		t.Fatalf("tail-call errors differ: %s", difference)
	}
	if !valuesSliceEquivalent(oldValues, machineValues) {
		t.Fatalf("tail-call values differ: old=%s machine=%s", valuesDiagnostic(oldValues), valuesDiagnostic(machineValues))
	}
	if oldController.remaining != machineController.remaining {
		t.Fatalf("tail-call instruction accounting differs: old=%d machine=%d", oldController.remaining, machineController.remaining)
	}
}

func TestPublicRunUsesScalarMachineForArrayTableMutation(t *testing.T) {
	proto, err := Compile(`local values = {10, 20} values[2] = 22 return values[1] + values[2]`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if !image.eligible {
		t.Fatalf("scalar array table image is ineligible: %s\ndisassembly:\n%s", image.rejectReason, strings.Join(disassembleProto(proto), "\n"))
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("Run returned %d values, want 1", len(values))
	}
	if number, ok := values[0].Number(); !ok || number != 32 {
		t.Fatalf("Run result = %v (%t), want 32", number, ok)
	}
	oldValues, oldErr := executeProto(context.Background(), proto, nil, executeOptions{instrumented: true})
	if oldErr != nil {
		t.Fatal(oldErr)
	}
	if !valuesSliceEquivalent(oldValues, values) {
		t.Fatalf("Machine result differs from old VM: old=%s machine=%s", valuesDiagnostic(oldValues), valuesDiagnostic(values))
	}
}

func TestPublicRunDetachesScalarMachineTableResult(t *testing.T) {
	proto := mustEligibleMachineProto(t, `return {10, 20}`)
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("Run returned %d values, want 1", len(values))
	}
	table, ok := values[0].Table()
	if !ok {
		t.Fatalf("Run result kind = %s, want table", values[0].Kind())
	}
	for index, want := range []float64{10, 20} {
		value, err := table.Get(NumberValue(float64(index + 1)))
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := value.Number(); !ok || got != want {
			t.Fatalf("table[%d] = %v (%t), want %v", index+1, got, ok, want)
		}
	}
}

func TestPublicRunUsesScalarMachineForStringTableField(t *testing.T) {
	proto, err := Compile(`local item = {name = "ember"} item.name = "fire" return item.name`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if !image.eligible {
		t.Fatalf("scalar string-field table image is ineligible: %s\ndisassembly:\n%s", image.rejectReason, strings.Join(disassembleProto(proto), "\n"))
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("Run returned %d values, want 1", len(values))
	}
	if text, ok := values[0].String(); !ok || text != "fire" {
		t.Fatalf("Run result = %q (%t), want fire", text, ok)
	}
	oldValues, oldErr := executeProto(context.Background(), proto, nil, executeOptions{instrumented: true})
	if oldErr != nil {
		t.Fatal(oldErr)
	}
	if !valuesSliceEquivalent(oldValues, values) {
		t.Fatalf("Machine result differs from old VM: old=%s machine=%s", valuesDiagnostic(oldValues), valuesDiagnostic(values))
	}
}

func TestPublicRunDetachesCyclicScalarMachineTableResult(t *testing.T) {
	proto := mustEligibleMachineProto(t, `local value = {} value.self = value return value`)
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("Run returned %d values, want 1", len(values))
	}
	table, ok := values[0].Table()
	if !ok {
		t.Fatalf("Run result kind = %s, want table", values[0].Kind())
	}
	self, err := table.Get(StringValue("self"))
	if err != nil {
		t.Fatal(err)
	}
	selfTable, ok := self.Table()
	if !ok || selfTable != table {
		t.Fatalf("Run result self = %v (%t), want detached table identity", selfTable, ok)
	}
}

func TestScalarMachineMatchesOldVM(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "arithmetic", source: `local a = 9 local b = 4 return a+b, a-b, a*b, a/b, a%b, a//b, a^b, -a`},
		{name: "equality ordering", source: `local a = 3 local b = 4 return nil == nil, true ~= false, a < b, a <= b, b > a, b >= a`},
		{name: "branches", source: `local a = 3 local b = 4 if a < b then a = a + 10 else a = a - 10 end return a`},
		{name: "while loop", source: `local i = 0 local total = 0 while i < 50 do i = i + 1 total = total + i end return total`},
		{name: "numeric for", source: `local total = 0 for i = 10, 1, -2 do total = total + i end return total`},
		{name: "return counts", source: `return nil, false, true, 1, -0`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proto := mustEligibleMachineProto(t, test.source)
			oldValues, oldErr := executeProto(context.Background(), proto, nil, executeOptions{instrumented: true})
			image, _ := proto.preparedCodeImage()
			machineValues, machineErr := executeCodeImage(image, nil)
			if difference := errorsEquivalent(oldErr, machineErr); difference != "" {
				t.Fatalf("errors differ: %s\nold=%v\nmachine=%v", difference, oldErr, machineErr)
			}
			if !valuesSliceEquivalent(oldValues, machineValues) {
				t.Fatalf("values differ:\nold=%s\nmachine=%s", valuesDiagnostic(oldValues), valuesDiagnostic(machineValues))
			}
		})
	}

	t.Run("entry nil register", func(t *testing.T) {
		proto := newProto(nil, []instruction{
			{op: opMove, a: 0, b: 1},
			{op: opReturnOne, a: 0},
		}, nil, nil, 2, 0, false)
		oldValues, oldErr := executeProto(context.Background(), proto, nil, executeOptions{instrumented: true})
		image, imageErr := proto.preparedCodeImage()
		if imageErr != nil || !image.eligible {
			t.Fatalf("image eligibility = %t, err=%v", image != nil && image.eligible, imageErr)
		}
		machineValues, machineErr := executeCodeImage(image, nil)
		if difference := errorsEquivalent(oldErr, machineErr); difference != "" {
			t.Fatalf("errors differ: %s\nold=%v\nmachine=%v", difference, oldErr, machineErr)
		}
		if !valuesSliceEquivalent(oldValues, machineValues) {
			t.Fatalf("values differ:\nold=%s\nmachine=%s", valuesDiagnostic(oldValues), valuesDiagnostic(machineValues))
		}
	})
}

func TestScalarMachinePreservesNumberBits(t *testing.T) {
	for _, bits := range []uint64{
		0x7ff8_0000_0000_0001,
		0x7ff8_ffff_ffff_ffff,
		0x8000_0000_0000_0000,
		0x0000_0000_0000_0000,
	} {
		proto := newProto(
			[]Value{NumberValue(math.Float64frombits(bits))},
			[]instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}},
			nil, nil, 1, 0, false,
		)
		image, err := proto.preparedCodeImage()
		if err != nil || !image.eligible {
			t.Fatalf("bits %#x image eligibility = %t, err=%v", bits, image != nil && image.eligible, err)
		}
		values, err := executeCodeImage(image, nil)
		if err != nil {
			t.Fatalf("bits %#x: %v", bits, err)
		}
		number, ok := values[0].Number()
		if !ok || math.Float64bits(number) != bits {
			t.Fatalf("number bits = %#x (%t), want %#x", math.Float64bits(number), ok, bits)
		}
	}
}

func TestScalarMachineRunsStringConstantAndExportsExactBytes(t *testing.T) {
	tests := []struct {
		name  string
		value []byte
	}{
		{name: "empty", value: []byte{}},
		{name: "nul", value: []byte{0, 1, 0, 255}},
		{name: "non utf8", value: []byte{0xff, 0xfe, 0x80}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proto := newProto(
				[]Value{StringValue(string(test.value))},
				[]instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}},
				nil, nil, 1, 0, false,
			)
			image, err := proto.preparedCodeImage()
			if err != nil || !image.eligible {
				t.Fatalf("image eligibility=%t err=%v reason=%q", image != nil && image.eligible, err, image.rejectReason)
			}
			if len(image.stringRecords) != 1 || !bytesEqual(image.stringData, test.value) {
				t.Fatalf("image strings records=%#v data=%#v, want exact bytes", image.stringRecords, image.stringData)
			}
			values, err := Run(proto)
			if err != nil {
				t.Fatal(err)
			}
			if len(values) != 1 {
				t.Fatalf("Run values=%v, want one result", values)
			}
			got, ok := values[0].String()
			if !ok || !bytesEqual([]byte(got), test.value) {
				t.Fatalf("Run string=%#v (%t), want exact %#v", []byte(got), ok, test.value)
			}
		})
	}
}

func TestScalarMachineInternsDuplicateStringConstants(t *testing.T) {
	value := []byte{0, 0xff, 'e', 'm', 'b', 'e', 'r'}
	proto := newProto(
		[]Value{StringValue(string(value)), StringValue(string(value))},
		[]instruction{
			{op: opLoadConst, a: 0, b: 0},
			{op: opLoadConst, a: 1, b: 1},
			{op: opReturn, a: 0, b: 2},
		},
		nil, nil, 2, 0, false,
	)
	image, err := proto.preparedCodeImage()
	if err != nil || !image.eligible {
		t.Fatalf("image eligibility=%t err=%v reason=%q", image != nil && image.eligible, err, image.rejectReason)
	}
	if len(image.stringRecords) != 1 || image.constants[0].bits != image.constants[1].bits {
		t.Fatalf("image duplicate strings records=%d constants=%#v", len(image.stringRecords), image.constants)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 {
		t.Fatalf("Run values=%v, want two results", values)
	}
	for index, result := range values {
		got, ok := result.String()
		if !ok || !bytesEqual([]byte(got), value) {
			t.Fatalf("result %d=%#v (%t), want %#v", index, []byte(got), ok, value)
		}
	}
}

func TestScalarMachineStringEqualityAndOrdering(t *testing.T) {
	proto, err := Compile(`return "a" == "a", "a" < "b", "b" > "a"`)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil || !image.eligible {
		t.Fatalf("image eligibility=%t err=%v reason=%q", image != nil && image.eligible, err, image.rejectReason)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 3 {
		t.Fatalf("Run values=%v, want three results", values)
	}
	for index, value := range values {
		if got, ok := value.Bool(); !ok || !got {
			t.Fatalf("result %d=%v (%t), want true", index, got, ok)
		}
	}
}

func TestScalarMachineStringArenaLifecycleAndConcurrentRun(t *testing.T) {
	proto, err := Compile(`local value = "ember" return value`)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var group sync.WaitGroup
	errors := make(chan error, workers)
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 20 {
				values, runErr := Run(proto)
				if runErr != nil {
					errors <- runErr
					return
				}
				if len(values) != 1 || values[0].stringText() != "ember" {
					errors <- fmt.Errorf("Run values=%v, want string result", values)
					return
				}
			}
		}()
	}
	group.Wait()
	close(errors)
	for runErr := range errors {
		t.Fatal(runErr)
	}

	image, _ := proto.preparedCodeImage()
	machine, err := bindScalarMachine(image, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runGeneratedScalarMachineLoop(machine); err != nil {
		t.Fatal(err)
	}
	if _, err := machine.exportResults(); err != nil {
		t.Fatal(err)
	}
	releaseScalarMachine(machine)
	if machine.bound || machine.strings.closed || len(machine.strings.records) != 0 || len(machine.strings.data) != 0 {
		t.Fatalf("released Machine retains string state: %#v", machine.strings)
	}
}

func TestScalarMachineErrorsAndPolicyMatchOldVM(t *testing.T) {
	proto := mustEligibleMachineProto(t, "local value = true\nreturn value + 1")
	oldValues, oldErr := executeProto(context.Background(), proto, nil, executeOptions{instrumented: true})
	image, _ := proto.preparedCodeImage()
	machineValues, machineErr := executeCodeImage(image, nil)
	if len(oldValues) != 0 || len(machineValues) != 0 {
		t.Fatalf("error results old=%v machine=%v, want none", oldValues, machineValues)
	}
	if difference := errorsEquivalent(oldErr, machineErr); difference != "" {
		t.Fatalf("runtime errors differ: %s\nold=%v\nmachine=%v", difference, oldErr, machineErr)
	}

	loop := mustEligibleMachineProto(t, "local total = 0 for i = 1, 20 do total = total + i end return total")
	loopImage, _ := loop.preparedCodeImage()
	for _, limit := range []uint64{1, 2, 3, 7, 16, 64, 256} {
		oldController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: limit})
		if err != nil {
			t.Fatal(err)
		}
		machineController, err := newExecutionController(context.Background(), ExecutionLimits{MaxInstructions: limit})
		if err != nil {
			t.Fatal(err)
		}
		oldValues, oldErr := executeProto(context.Background(), loop, nil, executeOptions{controller: oldController, instrumented: true})
		machineValues, machineErr := executeCodeImage(loopImage, machineController)
		if difference := errorsEquivalent(oldErr, machineErr); difference != "" {
			t.Fatalf("limit %d errors differ: %s\nold=%v\nmachine=%v", limit, difference, oldErr, machineErr)
		}
		if !valuesSliceEquivalent(oldValues, machineValues) {
			t.Fatalf("limit %d values differ", limit)
		}
		if oldController.remaining != machineController.remaining {
			t.Fatalf("limit %d remaining old=%d machine=%d", limit, oldController.remaining, machineController.remaining)
		}
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	oldController, _ := newExecutionController(canceled, ExecutionLimits{})
	machineController, _ := newExecutionController(canceled, ExecutionLimits{})
	_, oldErr = executeProto(canceled, loop, nil, executeOptions{controller: oldController, instrumented: true})
	_, machineErr = executeCodeImage(loopImage, machineController)
	if difference := errorsEquivalent(oldErr, machineErr); difference != "" {
		t.Fatalf("cancellation errors differ: %s\nold=%v\nmachine=%v", difference, oldErr, machineErr)
	}
}

func TestScalarMachineLifecycleAndLayout(t *testing.T) {
	if reflect.TypeOf(slot(0)).Kind() != reflect.Uint64 || reflect.TypeOf([]slot{}).Elem().Kind() != reflect.Uint64 {
		t.Fatal("Machine register/result slots are not pointer-free uint64 values")
	}
	proto := mustEligibleMachineProto(t, "local total = 0 for i = 1, 40 do total = total + i end return total")
	image, _ := proto.preparedCodeImage()
	machine, err := bindScalarMachine(image, nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	if _, err := runGeneratedScalarMachineLoop(machine); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	values, err := machine.exportResults()
	if err != nil || len(values) != 1 {
		t.Fatalf("export values=%v err=%v", values, err)
	}
	releaseScalarMachine(machine)
	if machine.bound || machine.image != nil || len(machine.registers) != 0 || len(machine.results) != 0 || len(machine.numberBits) != 0 {
		t.Fatalf("released Machine retains live binding: %#v", machine)
	}

	errorProto := mustEligibleMachineProto(t, "return true + 1")
	errorImage, _ := errorProto.preparedCodeImage()
	errorMachine, err := bindScalarMachine(errorImage, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runGeneratedScalarMachineLoop(errorMachine); err == nil {
		t.Fatal("Machine error fixture succeeded")
	}
	releaseScalarMachine(errorMachine)
	if errorMachine.bound || errorMachine.image != nil {
		t.Fatal("error Machine did not close")
	}
}

func TestScalarMachineAllocationsStayFlatWithTripCount(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation shape runs only with normal compiler/runtime instrumentation")
	}
	allocations := func(trips int) float64 {
		proto := mustEligibleMachineProto(t, "local total = 0 for i = 1, "+strconv.Itoa(trips)+" do total = total + i end return total")
		image, _ := proto.preparedCodeImage()
		if _, err := executeCodeImage(image, nil); err != nil {
			t.Fatal(err)
		}
		return testing.AllocsPerRun(100, func() {
			if _, err := executeCodeImage(image, nil); err != nil {
				panic(err)
			}
		})
	}
	short := allocations(1)
	long := allocations(1000)
	if long > short {
		t.Fatalf("Machine allocations grow with loop trips: 1=%g 1000=%g", short, long)
	}
}

func TestOnlyPublicRunRoutesToScalarMachine(t *testing.T) {
	source, err := os.ReadFile("vm.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Count(text, "executeCodeImage(") != 1 {
		t.Fatalf("vm.go executeCodeImage calls = %d, want exactly public Run", strings.Count(text, "executeCodeImage("))
	}
	machineSource, err := os.ReadFile("runtime_machine.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"executeProto(", "runWithUpvalues(", "runGeneratedDirectFrame"} {
		if strings.Contains(string(machineSource), forbidden) {
			t.Fatalf("compact Machine calls old VM through %q", forbidden)
		}
	}
}

func mustEligibleMachineProto(t testing.TB, source string) *Proto {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if !image.eligible {
		t.Fatalf("source compiled to ineligible image: %s\ndisassembly:\n%s", image.rejectReason, strings.Join(disassembleProto(proto), "\n"))
	}
	return proto
}
