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

func TestCompilerBenchmarkMetricsCountsPrototypeDebugMetadata(t *testing.T) {
	proto := &Proto{debugInfo: &protoDebugInfo{
		sourceName:   "source-name",
		functionName: "function-name",
	}}

	metrics := compilerBenchmarkMetrics([]*Proto{proto})
	want := int64(len(proto.debugInfo.sourceName) + len(proto.debugInfo.functionName))
	if metrics.RetainedStringBytes != want {
		t.Fatalf("retained string bytes = %d, want debug metadata bytes %d", metrics.RetainedStringBytes, want)
	}
}

func TestPrototypeFallbackWordcodeFootprintUsesRankedCacheSites(t *testing.T) {
	proto, err := Compile(`
local prototype = {hp = 20, mana = 5, armor = 2}
local misses = 0
local mt = {
    __index = function(_, key)
        misses = misses + 1
        if key == "power" then
            return prototype.hp + prototype.mana
        end
        return prototype[key] or 0
    end,
}
local actors = {
    setmetatable({hp = 80}, mt),
    setmetatable({mana = 15, armor = 4}, mt),
    setmetatable({hp = 45, power = 9}, mt),
}
local total = 0
for tick = 1, 80 do
    for _, actor in actors do
        local value = actor.hp + actor.mana + actor.power
        if actor.armor > 3 then
            actor.hp = actor.hp + tick % 3 - actor.armor
        else
            actor.mana = actor.mana + tick % 4
        end
        total = total + value + actor.hp + actor.mana
    end
end
return total + misses
`)
	if err != nil {
		t.Fatalf("Compile prototype fallback fixture: %v", err)
	}
	cacheSites := 0
	var countCacheSites func(*Proto)
	countCacheSites = func(current *Proto) {
		if current == nil {
			return
		}
		cacheSites += current.cacheSiteCount
		for _, child := range current.prototypes {
			countCacheSites(child)
		}
	}
	countCacheSites(proto)
	if got, want := cacheSites, 23; got != want {
		t.Fatalf("prototype fallback cache sites = %d, want %d", got, want)
	}
	metrics := compilerBenchmarkMetrics([]*Proto{proto})
	if got, want := metrics.WordcodeBytes, int64(456); got != want {
		t.Fatalf("prototype fallback wordcode bytes = %d, want %d", got, want)
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
	if proto.debugInfo != nil {
		owned += int64(reflect.TypeOf(*proto.debugInfo).Size())
		for _, text := range []string{proto.debugInfo.sourceName, proto.debugInfo.functionName} {
			bytes := compilerBenchmarkStringBytes(text, strings)
			owned += bytes
			retainedStrings += bytes
		}
	}
	if proto.cacheIndex != nil {
		indexValue := reflect.ValueOf(proto.cacheIndex).Elem()
		owned += int64(indexValue.Type().Size())
		for fieldIndex := 0; fieldIndex < indexValue.NumField(); fieldIndex++ {
			field := indexValue.Field(fieldIndex)
			if field.Kind() == reflect.Slice {
				owned += int64(field.Cap()) * int64(field.Type().Elem().Size())
			}
		}
	}
	return owned, retainedStrings
}
