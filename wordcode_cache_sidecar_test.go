package ember

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestWordcodeDirectCacheSidecarMapsAllCacheableOpcodes(t *testing.T) {
	code := []instruction{
		{op: opMove, a: 0, b: 1},
		{op: opGetIndex, a: 0, b: 1, c: 2},
		{op: opLoadConst, a: 0, b: 0},
		{op: opSetStringField, a: 1, b: 0, c: 2},
		{op: opGetStringField, a: 0, b: 1, c: 0},
		{op: opSetStringFieldIndex, a: 1, b: 1, c: 2, d: 0},
		{op: opGetStringFieldIndex, a: 0, b: 1, c: 2, d: 0},
		{op: opSetIndex, a: 1, b: 2, c: 0},
	}
	words, err := encodeWordcode(code, 3, 3)
	if err != nil {
		t.Fatal(err)
	}
	boundaries, err := wordcodeBoundaries(code)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := buildWordcodeCacheIndex(code, boundaries, len(words))
	if err != nil {
		t.Fatal(err)
	}
	if err := direct.validateWords(words, 6, 3); err != nil {
		t.Fatalf("direct validation failed: %v", err)
	}
	if got, want := len(direct.sidecar), len(words); got != want {
		t.Fatalf("sidecar length = %d, want %d", got, want)
	}
	wantDescriptors := []int{wordcodeCacheDynamicConstant, 0, 0, 1, 2, wordcodeCacheDynamicConstant}
	wantID := uint32(0)
	cachePCs := make(map[int]bool)
	for logical, ins := range code {
		if !wordcodeCacheableOpcode(ins.op) {
			continue
		}
		pc := boundaries[logical]
		cachePCs[pc] = true
		id, ok := direct.cacheIDAt(pc)
		if !ok || id != wantID || direct.sidecar[pc] != id+1 {
			t.Fatalf("cache site logical %d physical %d = id %d/%t marker %d, want %d/true marker %d", logical, pc, id, ok, direct.sidecar[pc], wantID, wantID+1)
		}
		if got := direct.constants[id]; got != wantDescriptors[id] {
			t.Fatalf("cache site %d descriptor = %d, want %d", id, got, wantDescriptors[id])
		}
		wantID++
	}
	for pc, marker := range direct.sidecar {
		if !cachePCs[pc] && marker != 0 {
			t.Fatalf("non-cache physical pc %d marker = %d, want zero", pc, marker)
		}
	}
}

func TestWordcodeDirectCacheSidecarRejectsMalformedMarkers(t *testing.T) {
	code := []instruction{{op: opGetIndex, a: 0, b: 1, c: 2}, {op: opSetIndex, a: 0, b: 1, c: 2}}
	words, err := encodeWordcode(code, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	boundaries, err := wordcodeBoundaries(code)
	if err != nil {
		t.Fatal(err)
	}
	index, err := buildWordcodeCacheIndex(code, boundaries, len(words))
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(name string, fn func(*wordcodeCacheIndex)) {
		t.Run(name, func(t *testing.T) {
			bad := *index
			bad.sidecar = append([]uint32(nil), index.sidecar...)
			bad.constants = append([]int(nil), index.constants...)
			fn(&bad)
			if err := bad.validateWords(words, 2, 0); err == nil {
				t.Fatal("accepted malformed direct sidecar")
			}
		})
	}
	mutate("short", func(bad *wordcodeCacheIndex) { bad.sidecar = bad.sidecar[:1] })
	mutate("gapped marker", func(bad *wordcodeCacheIndex) { bad.sidecar[0], bad.sidecar[1] = 2, 0 })
	mutate("reordered marker", func(bad *wordcodeCacheIndex) { bad.sidecar[0], bad.sidecar[1] = 2, 1 })
	mutate("duplicate marker", func(bad *wordcodeCacheIndex) { bad.sidecar[0], bad.sidecar[1] = 1, 1 })
	mutate("extra sidecar metadata", func(bad *wordcodeCacheIndex) { bad.sidecar = append(bad.sidecar, 0) })
	mutate("extra descriptor", func(bad *wordcodeCacheIndex) { bad.constants = append(bad.constants, wordcodeCacheDynamicConstant) })
	mutate("non-cache primary", func(bad *wordcodeCacheIndex) { bad.sidecar[0] = 0; bad.sidecar[1] = 0 })
	mutate("marker out of range", func(bad *wordcodeCacheIndex) { bad.sidecar[0] = 3 })
}

func TestWordcodeDirectCacheSidecarRejectsNonCacheAndAUXMarks(t *testing.T) {
	t.Run("non-cache primary", func(t *testing.T) {
		code := []instruction{{op: opMove, a: 0, b: 1}, {op: opGetIndex, a: 0, b: 1, c: 2}, {op: opSetIndex, a: 0, b: 1, c: 2}}
		words, err := encodeWordcode(code, 3, 0)
		if err != nil {
			t.Fatal(err)
		}
		boundaries, err := wordcodeBoundaries(code)
		if err != nil {
			t.Fatal(err)
		}
		index, err := buildWordcodeCacheIndex(code, boundaries, len(words))
		if err != nil {
			t.Fatal(err)
		}
		index.sidecar[0] = 1
		index.sidecar[1] = 2
		index.sidecar[2] = 0
		if err := index.validateWords(words, 2, 0); err == nil || !strings.Contains(err.Error(), "cache mark") {
			t.Fatalf("accepted non-cache mark: %v", err)
		}
	})
	t.Run("AUX word", func(t *testing.T) {
		code := []instruction{{op: opLoadConst, a: 0, b: 70000}, {op: opGetIndex, a: 0, b: 1, c: 2}}
		words, err := encodeWordcode(code, 3, 70001)
		if err != nil {
			t.Fatal(err)
		}
		boundaries, err := wordcodeBoundaries(code)
		if err != nil {
			t.Fatal(err)
		}
		index, err := buildWordcodeCacheIndex(code, boundaries, len(words))
		if err != nil {
			t.Fatal(err)
		}
		index.sidecar[0] = 0
		index.sidecar[1] = 1
		index.sidecar[2] = 2
		index.constants = append(index.constants, wordcodeCacheDynamicConstant)
		if err := index.validateWords(words, 2, 70001); err == nil || !strings.Contains(err.Error(), "AUX pc") {
			t.Fatalf("accepted AUX mark: %v", err)
		}
	})
}

func TestWordcodeDirectCacheSidecarSupportsMoreThan65535Sites(t *testing.T) {
	const sites = 70000
	code := make([]instruction, sites)
	for i := range code {
		code[i] = instruction{op: opGetIndex, a: 0, b: 1, c: 2}
	}
	words, err := encodeWordcode(code, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	boundaries, err := wordcodeBoundaries(code)
	if err != nil {
		t.Fatal(err)
	}
	index, err := buildWordcodeCacheIndex(code, boundaries, len(words))
	if err != nil {
		t.Fatal(err)
	}
	if err := index.validateWords(words, sites, 0); err != nil {
		t.Fatal(err)
	}
	if got, ok := index.cacheIDAt(65536); !ok || got != 65536 {
		t.Fatalf("cache ID at 65536 = %d/%t, want 65536/true", got, ok)
	}
}

func TestWordcodeDirectCacheSidecarPreservesProtoExecution(t *testing.T) {
	proto, err := Compile(`
local t = {}
t.value = 7
return t.value
`)
	if err != nil {
		t.Fatal(err)
	}
	if proto.cacheIndex == nil {
		t.Fatal("expected cache-bearing prototype")
	}
	want, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	code, err := decodeWordcode(proto.words, proto.cacheIndex)
	if err != nil {
		t.Fatal(err)
	}
	if err := encodeProtoWords(proto, code); err != nil {
		t.Fatal(err)
	}
	markersBefore := append([]uint32(nil), proto.cacheIndex.sidecar...)
	descriptorsBefore := append([]int(nil), proto.cacheIndex.constants...)
	if err := verifyProto(proto); err != nil {
		t.Fatal(err)
	}
	got, err := Run(proto)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("direct sidecar result = %#v, baseline result = %#v", got, want)
	}
	if !reflect.DeepEqual(proto.cacheIndex.sidecar, markersBefore) || !reflect.DeepEqual(proto.cacheIndex.constants, descriptorsBefore) {
		t.Fatal("Run mutated immutable direct cache metadata")
	}
}

func TestWordcodeDirectCacheSidecarPerInstanceIDs(t *testing.T) {
	source := `
local t = {}
t.value = 7
return t.value
`
	first, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, proto := range []*Proto{first, second} {
		code, err := decodeWordcode(proto.words, proto.cacheIndex)
		if err != nil {
			t.Fatal(err)
		}
		if err := encodeProtoWords(proto, code); err != nil {
			t.Fatal(err)
		}
	}
	if first.cacheIndex == second.cacheIndex || first.cacheSiteCount != second.cacheSiteCount {
		t.Fatalf("direct prototypes share metadata or disagree on count: %p/%p count=%d/%d", first.cacheIndex, second.cacheIndex, first.cacheSiteCount, second.cacheSiteCount)
	}
	for pc := range first.words {
		firstID, firstOK := first.cacheIndex.cacheIDAt(pc)
		secondID, secondOK := second.cacheIndex.cacheIDAt(pc)
		if firstOK != secondOK || firstOK && firstID != secondID {
			t.Fatalf("per-instance cache id at pc %d = %d/%t and %d/%t", pc, firstID, firstOK, secondID, secondOK)
		}
	}
}

func TestWordcodeDirectCacheSidecarRejectsInvalidDescriptors(t *testing.T) {
	t.Run("dynamic descriptor", func(t *testing.T) {
		code := []instruction{{op: opGetIndex, a: 0, b: 1, c: 2}}
		words, err := encodeWordcode(code, 3, 0)
		if err != nil {
			t.Fatal(err)
		}
		boundaries, _ := wordcodeBoundaries(code)
		index, err := buildWordcodeCacheIndex(code, boundaries, len(words))
		if err != nil {
			t.Fatal(err)
		}
		index.constants[0] = 0
		if err := index.validateWords(words, 1, 0); err == nil {
			t.Fatal("accepted non-sentinel GET_INDEX descriptor")
		}
	})
	t.Run("string descriptor", func(t *testing.T) {
		code := []instruction{{op: opGetStringField, a: 0, b: 1, c: 0}}
		words, err := encodeWordcode(code, 2, 1)
		if err != nil {
			t.Fatal(err)
		}
		boundaries, _ := wordcodeBoundaries(code)
		index, err := buildWordcodeCacheIndex(code, boundaries, len(words))
		if err != nil {
			t.Fatal(err)
		}
		for _, descriptor := range []int{-1, 1} {
			index.constants[0] = descriptor
			if err := index.validateWords(words, 1, 1); err == nil {
				t.Fatalf("accepted string descriptor %d", descriptor)
			}
		}
	})
}

func TestWordcodeCacheIndexFootprintAccounting(t *testing.T) {
	code := make([]instruction, 128)
	for i := range code {
		code[i] = instruction{op: opGetIndex, a: 0, b: 1, c: 2}
	}
	words, err := encodeWordcode(code, 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	boundaries, err := wordcodeBoundaries(code)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := buildWordcodeCacheIndex(code, boundaries, len(words))
	if err != nil {
		t.Fatal(err)
	}
	structBytes := int(unsafe.Sizeof(wordcodeCacheIndex{}))
	directBytes := structBytes + len(direct.sidecar)*4 + len(direct.constants)*int(unsafe.Sizeof(int(0)))
	wantBytes := structBytes + len(words)*4 + len(code)*int(unsafe.Sizeof(int(0)))
	// Winner-only direct layout: sizeof(wordcodeCacheIndex) + 4 bytes per
	// physical word + sizeof(int) per descriptor. On the current 64-bit
	// darwin-arm64 target this is a 56-byte header and 1592 bytes for the
	// 128-word/128-site fixture, 460 bytes (+40.6%) above the retired rank
	// estimate of 1132 bytes; the rank implementation is intentionally gone.
	t.Logf("cache index footprint: direct=%d bytes (struct=%d, words=%d, sites=%d)", directBytes, structBytes, len(words), len(direct.constants))
	if directBytes != wantBytes || directBytes <= 0 {
		t.Fatal("cache index footprint must be positive")
	}
}

func TestWordcodeDirectCacheSidecarKeepsCacheFreeProtoNil(t *testing.T) {
	code := []instruction{{op: opLoadConst, a: 0, b: 0}, {op: opReturnOne, a: 0}}
	index, err := buildWordcodeCacheIndex(code, []int{0, 1, 2}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if index != nil {
		t.Fatalf("cache-free direct index = %#v, want nil", index)
	}
}
