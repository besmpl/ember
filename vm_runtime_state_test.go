package ember

import (
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestRunSameProtoConcurrentlyOwnsRuntimeState(t *testing.T) {
	proto, err := Compile(`
local row = { value = 7 }
local key = "value"
local total = 0
for i = 1, 128 do
    total = total + row[key]
    total = total + (function() return 1 end)()
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if proto.cacheSiteCount == 0 {
		t.Fatal("compiled fixture has no dynamic cache sites")
	}
	if len(proto.prototypes) == 0 || proto.prototypes[0] == nil || !proto.prototypes[0].reuseZeroCaptureClosure {
		t.Fatal("compiled fixture did not mark its immediate zero-capture closure reusable")
	}

	const workers = 8
	ready := make(chan struct{}, workers)
	start := make(chan struct{})
	results := make([][]Value, workers)
	errs := make([]error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		worker := worker
		go func() {
			defer group.Done()
			ready <- struct{}{}
			<-start
			results[worker], errs[worker] = Run(proto)
		}()
	}
	for worker := 0; worker < workers; worker++ {
		<-ready
	}
	close(start)
	group.Wait()

	const want = 128 * (7 + 1)
	for worker := 0; worker < workers; worker++ {
		if errs[worker] != nil {
			t.Errorf("worker %d returned error: %v", worker, errs[worker])
			continue
		}
		if len(results[worker]) != 1 {
			t.Errorf("worker %d returned %d values, want 1", worker, len(results[worker]))
			continue
		}
		got, ok := results[worker][0].Number()
		if !ok || got != want {
			t.Errorf("worker %d returned %v, want %d", worker, results[worker][0], want)
		}
	}
}

func TestVMFunctionInstancesArePerThread(t *testing.T) {
	proto, err := Compile(`return (function() return 1 end)()`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	if len(proto.prototypes) == 0 || proto.prototypes[0] == nil {
		t.Fatal("compiled prototype has no zero-capture child prototype")
	}
	child := proto.prototypes[0]

	firstThread := newVMThread(runtimeGlobals(nil))
	secondThread := newVMThread(runtimeGlobals(nil))
	if _, err := firstThread.run(proto, nil, nil); err != nil {
		t.Fatalf("first thread run returned error: %v", err)
	}
	if _, err := secondThread.run(proto, nil, nil); err != nil {
		t.Fatalf("second thread run returned error: %v", err)
	}

	first := firstThread.functionInstance(child)
	repeated := firstThread.functionInstance(child)
	second := secondThread.functionInstance(child)
	if first == nil || repeated == nil || second == nil {
		t.Fatal("function instance lookup returned nil")
	}
	if first != repeated {
		t.Fatal("same vmThread returned different function instances for one Proto")
	}
	if first == second {
		t.Fatal("different vmThreads share a function instance for one Proto")
	}
}

func TestColdWordcodeDispatchPreservesCacheOperandsAcrossBoundaries(t *testing.T) {
	code := []instruction{
		{op: opSetStringField, a: 1, b: 9, c: 2},
		{op: opSetStringFieldIndex, a: 3, b: 4, c: 10, d: 5},
		{op: opGetStringField, a: 6, b: 7, c: 11},
		{op: opGetStringFieldIndex, a: 8, b: 9, c: 12, d: 10},
		{op: opSetIndex, a: 11, b: 12, c: 13},
		{op: opGetIndex, a: 14, b: 15, c: 16},
	}
	words, err := encodeWordcode(code, 17, 13)
	if err != nil {
		t.Fatalf("encode cacheable wordcode: %v", err)
	}
	boundaries, err := wordcodeBoundaries(code)
	if err != nil {
		t.Fatalf("wordcode boundaries: %v", err)
	}
	if len(boundaries) != len(code)+1 {
		t.Fatalf("wordcode boundaries = %d entries, want %d", len(boundaries), len(code)+1)
	}

	physicalPC := 0
	for logicalPC, want := range code {
		if physicalPC != boundaries[logicalPC] {
			t.Fatalf("logical pc %d starts at physical pc %d, want %d", logicalPC, physicalPC, boundaries[logicalPC])
		}
		dispatch, err := decodeWordcodeDispatch(words, physicalPC)
		if err != nil {
			t.Fatalf("decode dispatch at logical pc %d (word pc %d): %v", logicalPC, physicalPC, err)
		}
		if dispatch.op != want.op || dispatch.a != want.a || dispatch.b != want.b || dispatch.c != want.c || dispatch.d != want.d {
			t.Fatalf("dispatch at logical pc %d = (%s, %d, %d, %d, %d), want (%s, %d, %d, %d, %d)",
				logicalPC, opcodeName(dispatch.op), dispatch.a, dispatch.b, dispatch.c, dispatch.d,
				opcodeName(want.op), want.a, want.b, want.c, want.d)
		}
		if dispatch.cacheID != uint32(logicalPC) {
			t.Fatalf("dispatch at logical pc %d has cache site id %d, want %d", logicalPC, dispatch.cacheID, logicalPC)
		}
		if dispatch.nextWord != boundaries[logicalPC+1] {
			t.Fatalf("dispatch at logical pc %d ends at physical pc %d, want %d", logicalPC, dispatch.nextWord, boundaries[logicalPC+1])
		}
		physicalPC = dispatch.nextWord
	}
	if physicalPC != len(words) {
		t.Fatalf("physical walk ended at word pc %d, want stream length %d", physicalPC, len(words))
	}
}

func TestWordcodeRejectsUint16CacheSiteOverflow(t *testing.T) {
	code := make([]instruction, 1<<16+1)
	for index := range code {
		code[index] = instruction{op: opGetStringField, a: 0, b: 1, c: 0}
	}
	_, err := encodeWordcode(code, 2, 1)
	if err == nil || !strings.Contains(err.Error(), "cache site id 65536 out of uint16 range") {
		t.Fatalf("encode with uint16 cache-site overflow returned %v, want uint16 range error", err)
	}
}

func TestVMFunctionInstanceCachesUseLazyCacheSites(t *testing.T) {
	proto, err := Compile(`
local row = { hp = 1 }
local key = "hp"
local total = 0
for i = 1, 16 do
    total = total + row[key]
end
return total
`)
	if err != nil {
		t.Fatalf("Compile cache-heavy fixture: %v", err)
	}
	if proto.cacheSiteCount == 0 {
		t.Fatal("cache-heavy fixture has no cache sites")
	}
	if proto.cacheSiteCount >= len(proto.words) {
		t.Fatalf("cache site count = %d, want fewer than physical words %d", proto.cacheSiteCount, len(proto.words))
	}

	thread := newVMThread(runtimeGlobals(nil))
	instance := thread.functionInstance(proto)
	if instance == nil {
		t.Fatal("cache-heavy fixture did not create a function instance")
	}
	if got := len(instance.caches); got != proto.cacheSiteCount {
		t.Fatalf("function instance has %d cache slots, want cacheSiteCount %d", got, proto.cacheSiteCount)
	}
	cachePointerType := reflect.TypeOf((*dynamicStringIndexCache)(nil))
	if got := reflect.TypeOf(instance.caches).Elem(); got != cachePointerType {
		t.Fatalf("cache slot element type = %s, want lazy pointer %s", got, cachePointerType)
	}
	for site, cache := range instance.caches {
		if cache != nil {
			t.Fatalf("cache site %d allocated before first execution", site)
		}
	}

	arithmetic, err := Compile("return 20 + 22")
	if err != nil {
		t.Fatalf("Compile cache-free fixture: %v", err)
	}
	if arithmetic.cacheSiteCount != 0 {
		t.Fatalf("cache-free fixture has cacheSiteCount %d, want 0", arithmetic.cacheSiteCount)
	}
	cacheFreeThread := newVMThread(runtimeGlobals(nil))
	if got := cacheFreeThread.functionInstance(arithmetic); got != nil {
		t.Fatalf("cache-free fixture created function instance %#v", got)
	}
}

func TestProtoHasNoMutableRuntimeState(t *testing.T) {
	protoType := reflect.TypeOf(Proto{})
	for _, fieldName := range []string{"directFrameIndexCaches", "canonicalClosure"} {
		if _, ok := protoType.FieldByName(fieldName); ok {
			t.Errorf("Proto retains mutable runtime field %q", fieldName)
		}
	}
	if _, ok := protoType.FieldByName("cacheSiteCount"); !ok {
		t.Error("Proto is missing immutable cacheSiteCount")
	}

	cacheType := reflect.TypeOf(dynamicStringIndexCache{})
	closureType := reflect.TypeOf((*closure)(nil))
	for fieldIndex := 0; fieldIndex < protoType.NumField(); fieldIndex++ {
		field := protoType.Field(fieldIndex)
		if containsRuntimeCacheType(field.Type, cacheType, closureType) {
			t.Errorf("Proto field %q retains mutable runtime cache type %s", field.Name, field.Type)
		}
	}
}

func containsRuntimeCacheType(fieldType, cacheType, closureType reflect.Type) bool {
	if fieldType == cacheType || fieldType == closureType {
		return true
	}
	switch fieldType.Kind() {
	case reflect.Array, reflect.Pointer, reflect.Slice:
		return containsRuntimeCacheType(fieldType.Elem(), cacheType, closureType)
	case reflect.Map:
		return containsRuntimeCacheType(fieldType.Key(), cacheType, closureType) ||
			containsRuntimeCacheType(fieldType.Elem(), cacheType, closureType)
	default:
		return false
	}
}
