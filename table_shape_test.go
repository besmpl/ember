package ember

import (
	"reflect"
	"sync"
	"testing"
	"unsafe"
)

func TestPropertyICIsCompactAndDoesNotRetainReceiverOrKey(t *testing.T) {
	if got, want := unsafe.Sizeof(propertyIC{}), uintptr(16); got != want {
		t.Fatalf("propertyIC size = %d bytes, want %d", got, want)
	}

	cacheType := reflect.TypeOf(propertyIC{})
	tablePointer := reflect.TypeOf((*Table)(nil))
	for index := 0; index < cacheType.NumField(); index++ {
		field := cacheType.Field(index)
		if field.Type == tablePointer {
			t.Fatalf("propertyIC field %q retains a receiver table", field.Name)
		}
		if field.Type.Kind() == reflect.String {
			t.Fatalf("propertyIC field %q retains a key string", field.Name)
		}
	}
}

func TestTableShapesAreSharedAcrossEqualLayouts(t *testing.T) {
	first := NewTable()
	second := NewTable()
	for _, table := range []*Table{first, second} {
		table.setRawStringFieldBox("hp", newStringBox("hp"), NumberValue(7))
		table.setRawStringFieldBox("mana", newStringBox("mana"), NumberValue(3))
	}

	if first.currentShape() == defaultTableShapes.root {
		t.Fatal("populated table retained the root shape")
	}
	if first.currentShape() != second.currentShape() {
		t.Fatal("equal insertion layouts did not share one shape")
	}

	var cache propertyIC
	firstHP, ok := cache.resolve(first, "hp", nil)
	if got, numberOK := firstHP.Number(); !ok || !numberOK || got != 7 {
		t.Fatalf("first resolve = %v, %t/%t; want 7", got, ok, numberOK)
	}
	secondHP, ok := cache.get(second)
	if got, numberOK := secondHP.Number(); !ok || !numberOK || got != 7 {
		t.Fatalf("same-shape cache hit = %v, %t/%t; want 7", got, ok, numberOK)
	}
}

func TestPropertyICReusesObservedAppendTransition(t *testing.T) {
	keyBox := newStringBox("field")
	first := NewTable()
	first.setRawStringFieldBox(keyBox.text, keyBox, NumberValue(1))
	childShape := first.currentShape()
	if childShape == defaultTableShapes.root {
		t.Fatal("first append retained the root shape")
	}

	var cache propertyIC
	cache.observe(first, keyBox.text, keyBox)
	if cache.shape != childShape || cache.state != propertyICPresent {
		t.Fatal("observed append did not seed the child shape")
	}

	second := NewTable()
	if !cache.resolveWrite(second, keyBox.text, keyBox, NumberValue(2)) {
		t.Fatal("warmed cache did not apply the observed append transition")
	}
	if second.currentShape() != childShape {
		t.Fatal("cached append did not publish the shared child shape")
	}
	value, ok := second.rawStringFieldBox(keyBox)
	if got, numberOK := value.Number(); !ok || !numberOK || got != 2 {
		t.Fatalf("cached append value = %v, %t/%t; want 2", got, ok, numberOK)
	}

	withMetatable := NewTable()
	withMetatable.metatable = NewTable()
	if cache.resolveWrite(withMetatable, keyBox.text, keyBox, NumberValue(3)) {
		t.Fatal("cached append bypassed __newindex-capable metatable semantics")
	}
	if _, ok := withMetatable.rawStringFieldBox(keyBox); ok {
		t.Fatal("rejected cached append mutated the receiver")
	}
}

func TestTableShapesDistinguishInsertionOrder(t *testing.T) {
	first := NewTable()
	first.setRawStringField("hp", NumberValue(1))
	first.setRawStringField("mana", NumberValue(2))

	second := NewTable()
	second.setRawStringField("mana", NumberValue(2))
	second.setRawStringField("hp", NumberValue(1))

	if first.currentShape() == second.currentShape() {
		t.Fatal("different insertion orders unexpectedly shared one shape")
	}

	var cache propertyIC
	if _, ok := cache.resolve(first, "hp", nil); !ok {
		t.Fatal("failed to seed property cache")
	}
	if _, ok := cache.get(second); ok {
		t.Fatal("property cache hit a different layout")
	}
}

func TestPropertyICSurvivesDeleteAndSameKeyReinsert(t *testing.T) {
	table := NewTable()
	firstBox := newStringBox("field")
	table.setRawStringFieldBox(firstBox.text, firstBox, NumberValue(1))
	shape := table.currentShape()

	var cache propertyIC
	if _, ok := cache.resolve(table, firstBox.text, firstBox); !ok {
		t.Fatal("failed to seed property cache")
	}

	table.setRawStringFieldBox(firstBox.text, firstBox, NilValue())
	if table.currentShape() != shape {
		t.Fatal("deleting a shaped property changed the shape")
	}
	if _, ok := cache.get(table); ok {
		t.Fatal("property cache returned a deleted value")
	}
	if cache.write(table, NumberValue(9)) {
		t.Fatal("property cache resurrected a deleted value")
	}

	secondBox := newStringBox("field")
	if secondBox == firstBox {
		t.Fatal("test requires distinct equal-content string boxes")
	}
	table.setRawStringFieldBox(secondBox.text, secondBox, NumberValue(2))
	if table.currentShape() != shape {
		t.Fatal("reinserting the same property changed its shape")
	}
	value, ok := cache.get(table)
	if got, numberOK := value.Number(); !ok || !numberOK || got != 2 {
		t.Fatalf("cache after reinsert = %v, %t/%t; want 2", got, ok, numberOK)
	}
	key, _, err := table.rawNext(NilValue())
	if err != nil {
		t.Fatalf("rawNext after reinsert: %v", err)
	}
	if key.stringBox() != secondBox {
		t.Fatal("iteration retained the deleted key box")
	}
}

func TestPropertyICStaysValidAcrossDictionaryGrowth(t *testing.T) {
	table := NewTable()
	table.setRawStringField("target", NumberValue(1))
	for index := 1; index < maxInlineStringFields; index++ {
		table.setRawStringField("field"+string(rune('a'+index)), NumberValue(float64(index)))
	}
	shape := table.currentShape()

	var cache propertyIC
	if _, ok := cache.resolve(table, "target", nil); !ok {
		t.Fatal("failed to seed target cache")
	}
	table.setRawStringField("overflow", NumberValue(99))
	if !table.hasStringOverflow() {
		t.Fatal("fixture did not create dictionary overflow")
	}
	if table.currentShape() != shape {
		t.Fatal("dictionary growth invalidated the stable inline shape")
	}
	value, ok := cache.get(table)
	if got, numberOK := value.Number(); !ok || !numberOK || got != 1 {
		t.Fatalf("cache after dictionary growth = %v, %t/%t; want 1", got, ok, numberOK)
	}
	if !cache.write(table, NumberValue(7)) {
		t.Fatal("cache write missed after dictionary growth")
	}
	value, ok = table.rawStringField("target")
	if got, numberOK := value.Number(); !ok || !numberOK || got != 7 {
		t.Fatalf("target after cached write = %v, %t/%t; want 7", got, ok, numberOK)
	}
}

func TestTableShapeTransitionPublicationIsConcurrent(t *testing.T) {
	domain := newTableShapeDomain()
	const workers = 32
	results := make([]*tableShape, workers)
	start := make(chan struct{})
	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		worker := worker
		go func() {
			defer group.Done()
			<-start
			results[worker] = domain.transition(domain.root, "shared", hashString("shared"))
		}()
	}
	close(start)
	group.Wait()

	want := results[0]
	if want == nil || !want.cacheable() {
		t.Fatalf("published shape = %#v, want cacheable shape", want)
	}
	for worker, got := range results[1:] {
		if got != want {
			t.Fatalf("worker %d received shape %p, want shared shape %p", worker+1, got, want)
		}
	}
	if domain.transitionCount != 1 {
		t.Fatalf("transition count = %d, want exactly 1", domain.transitionCount)
	}
}

func TestConstantFieldRuntimeUsesShapeCacheWithoutDynamicCacheBody(t *testing.T) {
	proto, err := Compile(`
local row = { hp = 7, mana = 3 }
local total = 0
for i = 1, 32 do
    total = total + row.hp + row.mana
end
row.hp = 8
return total + row.hp
`)
	if err != nil {
		t.Fatalf("Compile constant-field fixture: %v", err)
	}
	if proto.cacheSiteCount == 0 {
		t.Fatal("constant-field fixture has no cache sites")
	}

	for _, instrumented := range []bool{false, true} {
		name := "production"
		if instrumented {
			name = "instrumented"
		}
		t.Run(name, func(t *testing.T) {
			thread := newVMThread(runtimeGlobals(nil))
			thread.directFrameInstrumented = instrumented
			results, err := thread.run(proto, nil, nil)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if got, ok := results[0].Number(); !ok || got != 328 {
				t.Fatalf("result = %v, %t; want 328", got, ok)
			}

			instance := thread.functionInstances[proto]
			if instance == nil {
				t.Fatal("run did not create a function instance")
			}
			if got := len(instance.fieldCaches); got != proto.cacheSiteCount {
				t.Fatalf("field cache slots = %d, want %d", got, proto.cacheSiteCount)
			}
			warmed := 0
			for _, cache := range instance.fieldCaches {
				if cache.state == propertyICPresent {
					warmed++
				}
			}
			if warmed == 0 {
				t.Fatal("constant-field execution did not warm a shape cache")
			}
			for site, cache := range instance.caches {
				if cache != nil {
					t.Fatalf("constant-field site %d activated the dynamic cache: %p", site, cache)
				}
			}
		})
	}
}

func TestOverflowConstantFieldRetainsDynamicCacheFallback(t *testing.T) {
	proto, err := Compile(`
local row = { a=1, b=2, c=3, d=4, e=5, f=6, g=7, h=8, overflow=9 }
local total = 0
for i = 1, 8 do
    total = total + row.overflow
end
return total
`)
	if err != nil {
		t.Fatalf("Compile overflow fixture: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	results, err := thread.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("run overflow fixture: %v", err)
	}
	if got, ok := results[0].Number(); !ok || got != 72 {
		t.Fatalf("result = %v, %t; want 72", got, ok)
	}
	instance := thread.functionInstances[proto]
	if instance == nil {
		t.Fatal("overflow fixture did not create a function instance")
	}
	activated := false
	for _, cache := range instance.caches {
		if cache != nil && cache != dynamicStringIndexCacheCold {
			activated = true
			break
		}
	}
	if !activated {
		t.Fatal("overflow field did not activate the dynamic cache fallback")
	}
}
