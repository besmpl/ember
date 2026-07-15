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
