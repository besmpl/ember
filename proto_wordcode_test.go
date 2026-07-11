package ember

import (
	"reflect"
	"testing"
)

func TestProtoPublishesWordcodeAndPhysicalLineMap(t *testing.T) {
	constants := []Value{StringValue("global"), StringValue("field")}
	code := []instruction{
		{op: opLoadGlobal, a: 0, b: 0, c: 0},
		{op: opGetStringField, a: 1, b: 0, c: 1},
		{op: opReturnOne, a: 1},
	}
	proto := newProtoWithDescriptors(constants, code, nil, nil, 2, 0, false)
	proto.lines = []int{11, 12, 13}
	if err := finalizeProtoExecutionArtifact(proto, code); err != nil {
		t.Fatalf("finalizeProtoExecutionArtifact returned error: %v", err)
	}

	decoded, err := decodeWordcode(proto.words)
	if err != nil {
		t.Fatalf("decodeWordcode returned error: %v", err)
	}
	if !reflect.DeepEqual(decoded, code) {
		t.Fatalf("decoded words = %#v, want canonical code %#v", decoded, code)
	}
	boundaries, err := wordcodeBoundaries(code)
	if err != nil {
		t.Fatalf("wordcodeBoundaries returned error: %v", err)
	}
	wantLines := wordcodeLogicalLineMap(proto.lines, boundaries)
	if !reflect.DeepEqual(proto.wordLines, wantLines) {
		t.Fatalf("word line map = %#v, want %#v", proto.wordLines, wantLines)
	}
	if len(proto.wordLines) != len(proto.words) {
		t.Fatalf("word line map length = %d, want word stream length %d", len(proto.wordLines), len(proto.words))
	}
	if len(proto.words) <= len(code) {
		t.Fatalf("word stream length = %d, want AUX-expanded stream longer than %d instructions", len(proto.words), len(code))
	}
	if got := proto.cacheSiteCount; got != 1 {
		t.Fatalf("cache site count = %d, want one cache site", got)
	}
	if proto.wordLines[boundaries[0]] != 11 || proto.wordLines[boundaries[0]+1] != 0 {
		t.Fatalf("AUX line mapping = %#v, want primary line 11 followed by zero", proto.wordLines)
	}
}
