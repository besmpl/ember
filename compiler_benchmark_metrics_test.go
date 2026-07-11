package ember

import (
	"reflect"
	"testing"
	"unsafe"
)

type CompilerBenchmarkMetrics struct {
	Instructions        int
	Constants           int
	RegisterSlots       int
	ChildProtos         int
	WordcodeBytes       int64
	ProtoOwnedBytes     int64
	RetainedStringBytes int64
}

func TestCompilerBenchmarkMetricsCountsRetainedStringBoxesOnce(t *testing.T) {
	shared := StringValue("shared")
	table := NewTable()
	if err := table.Set(StringValue("table-key"), StringValue("table-value")); err != nil {
		t.Fatalf("table.Set returned error: %v", err)
	}
	child := &Proto{constants: []Value{shared}}
	root := &Proto{
		constants:  []Value{shared, TableValue(table)},
		prototypes: []*Proto{child},
	}

	metrics := compilerBenchmarkMetrics([]*Proto{root})
	if got, want := metrics.RetainedStringBytes, int64(len("shared")); got != want {
		t.Fatalf("retained string bytes = %d, want shared string counted once as %d", got, want)
	}
	if metrics.ChildProtos != 1 {
		t.Fatalf("child protos = %d, want 1", metrics.ChildProtos)
	}
}

func TestCompilerBenchmarkMetricsDeduplicatesStringBackingAliases(t *testing.T) {
	shared := StringValue("shared-global")
	text, ok := shared.String()
	if !ok {
		t.Fatal("StringValue did not expose its string text")
	}
	proto := &Proto{
		constants:   []Value{shared},
		globalNames: []string{text},
	}

	metrics := compilerBenchmarkMetrics([]*Proto{proto})
	if got, want := metrics.RetainedStringBytes, int64(len(text)); got != want {
		t.Fatalf("retained string bytes = %d, want aliased backing counted once as %d", got, want)
	}
}

func CompilerBenchmarkMetricsForTest(proto *Proto) CompilerBenchmarkMetrics {
	if proto == nil {
		return CompilerBenchmarkMetrics{}
	}
	return compilerBenchmarkMetrics([]*Proto{proto})
}

func CompilerProgramBenchmarkMetricsForTest(program *Program) CompilerBenchmarkMetrics {
	if program == nil {
		return CompilerBenchmarkMetrics{}
	}
	roots := make([]*Proto, 0, len(program.protos))
	for _, proto := range program.protos {
		if proto != nil {
			roots = append(roots, proto)
		}
	}
	return compilerBenchmarkMetrics(roots)
}

func compilerBenchmarkMetrics(roots []*Proto) CompilerBenchmarkMetrics {
	rootSet := make(map[*Proto]bool, len(roots))
	for _, root := range roots {
		if root != nil {
			rootSet[root] = true
		}
	}
	seen := make(map[*Proto]bool)
	strings := newCompilerBenchmarkStringState()
	metrics := CompilerBenchmarkMetrics{}
	var visit func(*Proto)
	visit = func(proto *Proto) {
		if proto == nil || seen[proto] {
			return
		}
		seen[proto] = true
		if code, err := protoDecodedInstructions(proto); err == nil {
			metrics.Instructions += len(code)
		}
		metrics.Constants += len(proto.constants)
		metrics.RegisterSlots += proto.registers
		metrics.WordcodeBytes += int64(len(proto.words)) * int64(reflect.TypeOf(wordcodeWord(0)).Size())
		owned, retainedStrings := protoOwnedBenchmarkBytesWithStrings(proto, strings)
		metrics.ProtoOwnedBytes += owned
		metrics.RetainedStringBytes += retainedStrings
		if !rootSet[proto] {
			metrics.ChildProtos++
		}
		for _, child := range proto.prototypes {
			visit(child)
		}
	}
	for _, root := range roots {
		visit(root)
	}
	return metrics
}

func protoOwnedBenchmarkBytes(proto *Proto) int64 {
	owned, _ := protoOwnedBenchmarkBytesWithStrings(proto, newCompilerBenchmarkStringState())
	return owned
}

type compilerBenchmarkStringState struct {
	boxes   map[*stringBox]struct{}
	backing map[compilerBenchmarkStringBacking]struct{}
}

type compilerBenchmarkStringBacking struct {
	data *byte
	len  int
}

func newCompilerBenchmarkStringState() *compilerBenchmarkStringState {
	return &compilerBenchmarkStringState{
		boxes:   make(map[*stringBox]struct{}),
		backing: make(map[compilerBenchmarkStringBacking]struct{}),
	}
}

func compilerBenchmarkStringBytes(text string, state *compilerBenchmarkStringState) int64 {
	if len(text) == 0 {
		return 0
	}
	if state == nil {
		return int64(len(text))
	}
	key := compilerBenchmarkStringBacking{data: unsafe.StringData(text), len: len(text)}
	if _, ok := state.backing[key]; ok {
		return 0
	}
	state.backing[key] = struct{}{}
	return int64(len(text))
}

func protoOwnedBenchmarkBytesWithStrings(proto *Proto, strings *compilerBenchmarkStringState) (int64, int64) {
	if proto == nil {
		return 0, 0
	}
	value := reflect.ValueOf(proto).Elem()
	owned := int64(value.Type().Size())
	retainedStrings := int64(0)
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		switch field.Kind() {
		case reflect.String:
			bytes := compilerBenchmarkStringBytes(field.String(), strings)
			owned += bytes
			retainedStrings += bytes
		case reflect.Slice:
			owned += int64(field.Cap()) * int64(field.Type().Elem().Size())
			if field.Type().Elem().Kind() == reflect.String {
				for item := 0; item < field.Len(); item++ {
					bytes := compilerBenchmarkStringBytes(field.Index(item).String(), strings)
					owned += bytes
					retainedStrings += bytes
				}
			}
		}
	}
	for _, constant := range proto.constants {
		box := constant.stringBox()
		if box == nil {
			continue
		}
		if strings != nil {
			if _, ok := strings.boxes[box]; ok {
				continue
			}
			strings.boxes[box] = struct{}{}
		}
		bytes := compilerBenchmarkStringBytes(box.text, strings)
		owned += bytes
		retainedStrings += bytes
	}
	return owned, retainedStrings
}
