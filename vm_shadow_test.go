package ember

import (
	"strings"
	"testing"
	"unsafe"
)

func TestBuildDirectShadowPreservesPhysicalWordcodeAndGeneratedHandlers(t *testing.T) {
	proto, err := Compile(`
local total = 0
local values = {2, 3, 5}
for _, value in values do
	total = total + value * 2
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	decoded, boundaries, err := wordcodeDecodeWords(proto.words)
	if err != nil {
		t.Fatalf("wordcodeDecodeWords returned error: %v", err)
	}
	if len(proto.words) <= len(decoded) {
		t.Fatalf("physical words = %d, logical instructions = %d, want AUX coverage", len(proto.words), len(decoded))
	}

	shadow, err := buildDirectShadow(proto.words, generatedDirectSemanticMetadata)
	if err != nil {
		t.Fatalf("buildDirectShadow returned error: %v", err)
	}
	if len(shadow.words) != len(proto.words) {
		t.Fatalf("shadow words = %d, want %d", len(shadow.words), len(proto.words))
	}
	cacheSites := 0
	for index, ins := range decoded {
		pc := boundaries[index]
		word := shadow.words[pc]
		if word.raw() != proto.words[pc] {
			t.Fatalf("instruction %d raw word = %#x, want %#x", index, word.raw(), proto.words[pc])
		}
		if word.handler() != directHandlerID(ins.ins.op) || word.counter() != 0 {
			t.Fatalf("instruction %d handler/counter = %d/%d", index, word.handler(), word.counter())
		}
		metadata, _ := directSemanticMetadataFor(ins.ins.op)
		cacheIndex, cached := word.cacheIndex()
		if cached != (metadata.cache != directCacheNone) {
			t.Fatalf("instruction %d cache presence = %t, layout = %d", index, cached, metadata.cache)
		}
		if cached {
			if cacheIndex != cacheSites {
				t.Fatalf("instruction %d cache index = %d, want %d", index, cacheIndex, cacheSites)
			}
			if shadow.caches[cacheIndex].layout() != metadata.cache {
				t.Fatalf("instruction %d cache layout = %d, want %d", index, shadow.caches[cacheIndex].layout(), metadata.cache)
			}
			cacheSites++
		}
		next := boundaries[index+1]
		for auxPC := pc + 1; auxPC < next; auxPC++ {
			if shadow.words[auxPC].raw() != proto.words[auxPC] || shadow.words[auxPC].handler() != directHandlerInvalid {
				t.Fatalf("AUX word %d was made executable", auxPC)
			}
		}
	}
	if len(shadow.caches) != cacheSites {
		t.Fatalf("shadow caches = %d, want %d", len(shadow.caches), cacheSites)
	}

	firstPC := boundaries[0]
	original := proto.words[firstPC]
	shadow.words[firstPC] = shadow.words[firstPC].withHandler(directHandlerID(opcodeLimit)).incrementCounter()
	if proto.words[firstPC] != original {
		t.Fatal("mutating owner shadow changed immutable Proto wordcode")
	}
	if shadow.words[firstPC].handler() != directHandlerID(opcodeLimit) || shadow.words[firstPC].counter() != 1 {
		t.Fatal("shadow handler/counter mutation did not round-trip")
	}
}

func TestDirectShadowEncodingIsBoundedAndSaturating(t *testing.T) {
	if size := unsafe.Sizeof(directShadowWord(0)); size != 8 {
		t.Fatalf("directShadowWord size = %d, want 8", size)
	}
	if size := unsafe.Sizeof(directAdaptiveCacheCell(0)); size != 8 {
		t.Fatalf("directAdaptiveCacheCell size = %d, want 8", size)
	}
	word := newDirectShadowWord(0xfedcba98, directHandlerID(opAdd), 7)
	for range 300 {
		word = word.incrementCounter()
	}
	if word.raw() != 0xfedcba98 || word.handler() != directHandlerID(opAdd) || word.counter() != 255 {
		t.Fatalf("saturated word = raw:%#x handler:%d counter:%d", word.raw(), word.handler(), word.counter())
	}
	if cacheIndex, ok := word.cacheIndex(); !ok || cacheIndex != 7 {
		t.Fatalf("cache index = %d/%t, want 7/true", cacheIndex, ok)
	}
	if bytes := directShadowStateBytes(100, 100); bytes != 1600 {
		t.Fatalf("shadow state bytes = %d, want 1600", bytes)
	}
	if directShadowStateBytes(100, 100) > directShadowStateLimit(100) {
		t.Fatal("maximally dense shadow exceeds the hard owner-Program budget")
	}
}

func TestBuildDirectShadowFailsClosed(t *testing.T) {
	tests := []struct {
		name  string
		words []wordcodeWord
		table [opcodeLimit]directSemanticMetadata
		want  string
	}{
		{
			name:  "unknown opcode",
			words: []wordcodeWord{wordcodeOpcodeMask},
			table: generatedDirectSemanticMetadata,
			want:  "unknown opcode",
		},
		{
			name:  "truncated AUX",
			words: []wordcodeWord{wordcodeWord(opLoadGlobal) | wordcodeAuxBit},
			table: generatedDirectSemanticMetadata,
			want:  "truncated AUX",
		},
		{
			name:  "missing semantic metadata",
			words: []wordcodeWord{wordcodeWord(opMove)},
			table: func() [opcodeLimit]directSemanticMetadata {
				table := generatedDirectSemanticMetadata
				table[opMove] = directSemanticMetadata{}
				return table
			}(),
			want: "unclassified",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := buildDirectShadow(test.words, test.table)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("buildDirectShadow error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestVMFunctionInstancesOwnIndependentLazyShadows(t *testing.T) {
	proto, err := Compile(`local x = 1 return x + 2`)
	if err != nil {
		t.Fatal(err)
	}
	firstThread := newVMThread(runtimeGlobals(nil))
	first, err := firstThread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatalf("first shadowFunctionInstance returned error: %v", err)
	}
	again, err := firstThread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatalf("second shadowFunctionInstance returned error: %v", err)
	}
	if first != again || len(first.shadow.words) != len(proto.words) {
		t.Fatal("one thread did not reuse its lazily built shadow")
	}

	secondThread := newVMThread(runtimeGlobals(nil))
	second, err := secondThread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatalf("independent shadowFunctionInstance returned error: %v", err)
	}
	if first == second || len(second.shadow.words) == 0 || &first.shadow.words[0] == &second.shadow.words[0] {
		t.Fatal("independent threads shared mutable shadow state")
	}
	original := proto.words[0]
	first.shadow.words[0] = first.shadow.words[0].incrementCounter()
	if second.shadow.words[0].counter() != 0 || proto.words[0] != original {
		t.Fatal("owner-local shadow mutation escaped its thread")
	}
}

func TestUnownedPoolResetDropsDirectShadows(t *testing.T) {
	proto, err := Compile(`return 1 + 2`)
	if err != nil {
		t.Fatal(err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	instance, err := thread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(instance.shadow.words) == 0 {
		t.Fatal("shadow was not built")
	}
	thread.resetForPool()
	if retained := thread.functionInstances[proto]; retained != nil && len(retained.shadow.words) != 0 {
		t.Fatal("unowned pooled thread retained shadow wordcode")
	}
}

func TestOwnerDetachReusesShadowAndCollectionClearsIt(t *testing.T) {
	proto, err := Compile(`local x = 1 return x + 2`)
	if err != nil {
		t.Fatal(err)
	}
	owner := newRuntimeOwner()
	firstThread := newVMThread(runtimeGlobals(nil))
	if err := firstThread.bindOwner(owner); err != nil {
		t.Fatal(err)
	}
	first, err := firstThread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatal(err)
	}
	first.shadow.words[0] = first.shadow.words[0].incrementCounter()
	firstThread.unbindOwner()

	secondThread := newVMThread(runtimeGlobals(nil))
	if err := secondThread.bindOwner(owner); err != nil {
		t.Fatal(err)
	}
	second, err := secondThread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatal(err)
	}
	if second.shadow.words[0].counter() != 1 {
		t.Fatal("owner detach discarded reusable shadow feedback")
	}
	secondThread.unbindOwner()

	if _, err := owner.collect(nil); err != nil {
		t.Fatalf("owner.collect returned error: %v", err)
	}
	thirdThread := newVMThread(runtimeGlobals(nil))
	if err := thirdThread.bindOwner(owner); err != nil {
		t.Fatal(err)
	}
	third, err := thirdThread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatal(err)
	}
	if third.shadow.words[0].counter() != 0 {
		t.Fatal("collection retained stale shadow feedback")
	}
	thirdThread.unbindOwner()
	if err := owner.close(); err != nil {
		t.Fatalf("owner.close returned error: %v", err)
	}
	if owner.idleVMCaches != nil {
		t.Fatal("owner close retained shadow cache bundles")
	}
}

func TestProductionDirectFrameRunsGeneratedGenericShadow(t *testing.T) {
	proto, err := Compile(`
local function twice(value)
	return value * 2
end
local values = {amount = 4}
return twice(values.amount) + 1
`)
	if err != nil {
		t.Fatal(err)
	}
	production := newVMThread(runtimeGlobals(nil))
	got, err := production.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("production run returned error: %v", err)
	}
	want, _, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("instrumented oracle returned error: %v", err)
	}
	if !valuesEqualList(got, want) {
		t.Fatalf("production results = %#v, instrumented = %#v", got, want)
	}
	instance := production.functionInstances[proto]
	if instance == nil || len(instance.shadow.words) != len(proto.words) {
		t.Fatal("production direct frame did not retain its owner-local generic shadow")
	}
	for pc, word := range instance.shadow.words {
		if word.handler() == directHandlerInvalid {
			continue
		}
		op := opcode(uint8(proto.words[pc]) & uint8(wordcodeOpcodeMask))
		if word.handler() != directHandlerID(op) {
			t.Fatalf("word %d handler = %d, want generic %d", pc, word.handler(), op)
		}
	}
}

func valuesEqualList(left []Value, right []Value) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !valuesEqual(left[index], right[index]) {
			return false
		}
	}
	return true
}
