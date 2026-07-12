package ember

import (
	"encoding/binary"
	"reflect"
	"strings"
	"testing"
)

func TestWordcodeRoundTripFormatsAndRelativeJumps(t *testing.T) {
	code := []instruction{
		{op: opLoadConst, a: 0, b: 70000},
		{op: opJumpIfFalse, a: 0, b: 4},
		{op: opMove, a: 1, b: 0},
		{op: opJump, b: 1},
		{op: opReturnOne, a: 1},
	}
	words, err := encodeWordcode(code, 2, 70001)
	if err != nil {
		t.Fatalf("encodeWordcode returned error: %v", err)
	}
	if len(words) <= len(code) {
		t.Fatalf("wordcode length = %d, want AUX-expanded stream", len(words))
	}
	t.Logf("words=%#v", words)
	got, err := decodeWordcode(words)
	if err != nil {
		t.Fatalf("decodeWordcode returned error: %v", err)
	}
	if !reflect.DeepEqual(got, code) {
		t.Fatalf("wordcode round trip = %#v, want %#v", got, code)
	}
	if err := verifyWordcode(words, 2, 70001); err != nil {
		t.Fatalf("verifyWordcode returned error: %v", err)
	}
	lines, err := disassembleWordcodeChecked(words)
	if err != nil {
		t.Fatalf("disassembleWordcodeChecked returned error: %v", err)
	}
	if len(lines) != len(code) || !strings.Contains(lines[0], "LOAD_CONST") || !strings.Contains(lines[3], "JUMP") {
		t.Fatalf("wordcode disassembly = %#v", lines)
	}
}

func TestWordcodeRoundTripsEveryEncodableOpcodeShape(t *testing.T) {
	for _, op := range allOpcodes {
		ins := wordcodeSampleInstruction(op)
		words, err := encodeWordcode([]instruction{ins}, 16, 100)
		if err != nil {
			t.Fatalf("encodeWordcode(%s) returned error: %v (instruction %#v)", opcodeName(op), err, ins)
		}
		var cacheIndex *wordcodeCacheIndex
		if wordcodeCacheableOpcode(op) {
			boundaries, boundaryErr := wordcodeBoundaries([]instruction{ins})
			if boundaryErr != nil {
				t.Fatalf("wordcodeBoundaries(%s): %v", opcodeName(op), boundaryErr)
			}
			cacheIndex, err = buildWordcodeCacheIndex([]instruction{ins}, boundaries, len(words))
			if err != nil {
				t.Fatalf("build cache index(%s): %v", opcodeName(op), err)
			}
		}
		got, err := decodeWordcode(words, cacheIndex)
		if err != nil {
			t.Fatalf("decodeWordcode(%s) returned error: %v", opcodeName(op), err)
		}
		if len(got) != 1 || !reflect.DeepEqual(got[0], ins) {
			t.Fatalf("wordcode %s round trip = %#v, want %#v", opcodeName(op), got, ins)
		}
	}
}

func TestWordcodeMetadataCoversAllOpcodesAndHiddenOperands(t *testing.T) {
	if len(allOpcodes) != 64 {
		t.Fatalf("opcode count = %d, want 64 after removing narrow branches", len(allOpcodes))
	}
	for _, op := range allOpcodes {
		meta, ok := opcodeMetadata(op)
		if !ok || meta.wordcode.format == 0 {
			t.Fatalf("%s has no wordcode metadata", opcodeName(op))
		}
	}
	loadGlobal, _ := opcodeMetadata(opLoadGlobal)
	if loadGlobal.operands.c != bytecodeOperandGlobalSlot {
		t.Fatalf("LOAD_GLOBAL C kind = %d, want global slot", loadGlobal.operands.c)
	}
	setGlobal, _ := opcodeMetadata(opSetGlobal)
	if setGlobal.operands.c != bytecodeOperandGlobalSlot {
		t.Fatalf("SET_GLOBAL C kind = %d, want global slot", setGlobal.operands.c)
	}
	fastCall, _ := opcodeMetadata(opFastCall)
	if fastCall.operands.b != bytecodeOperandNativeID {
		t.Fatalf("FAST_CALL B kind = %d, want native id", fastCall.operands.b)
	}
}

func TestWordcodeRoundTripCallOneAndFastCallResultMarkers(t *testing.T) {
	code := []instruction{
		{op: opCallOne, a: 0, b: 0, c: encodeFixedCallCount(2, true), d: 1},
		{op: opFastCall, a: 0, b: int(nativeFuncSelect), c: 0, d: -1},
	}
	words, err := encodeWordcode(code, 4, 4)
	if err != nil {
		t.Fatalf("encode call markers: %v", err)
	}
	got, err := decodeWordcode(words)
	if err != nil {
		t.Fatalf("decode call markers: %v", err)
	}
	if !reflect.DeepEqual(got, code) {
		t.Fatalf("call marker round trip = %#v, want %#v", got, code)
	}
}

func TestWordcodeOptionalAuxBoundaryAndLineMap(t *testing.T) {
	code := []instruction{
		{op: opLoadConst, a: 0, b: 65535},
		{op: opLoadConst, a: 0, b: 65536},
	}
	words, err := encodeWordcode(code, 1, 65537)
	if err != nil {
		t.Fatalf("encode optional AUX boundary: %v", err)
	}
	if len(words) != 3 {
		t.Fatalf("word count = %d, want narrow+wide = 3", len(words))
	}
	boundaries, err := wordcodeBoundaries(code)
	if err != nil {
		t.Fatalf("wordcodeBoundaries: %v", err)
	}
	if !reflect.DeepEqual(boundaries, []int{0, 1, 3}) {
		t.Fatalf("boundaries = %#v, want [0 1 3]", boundaries)
	}
	mapLines := wordcodeLogicalLineMap([]int{10, 20}, boundaries)
	if !reflect.DeepEqual(mapLines, []int{10, 20, 0}) {
		t.Fatalf("line map = %#v, want [10 20 0]", mapLines)
	}
	mapping, err := buildWordcodeBoundaryMap(code)
	if err != nil {
		t.Fatalf("build boundary map: %v", err)
	}
	if word, ok := mapping.logicalPC(1); !ok || word != 1 {
		t.Fatalf("logical pc 1 = (%d,%t), want (1,true)", word, ok)
	}
	if logical, ok := mapping.wordPC(2); ok {
		t.Fatalf("AUX word pc 2 was exposed as logical %d", logical)
	}
	if logical, ok := mapping.wordPC(3); !ok || logical != 2 {
		t.Fatalf("word pc 3 = (%d,%t), want logical end 2", logical, ok)
	}
}

func TestWordcodeRelativeJumpBoundariesAndAuxTargetRejection(t *testing.T) {
	code := []instruction{
		{op: opJumpIfFalse, a: 0, b: 4},
		{op: opLoadConst, a: 1, b: 65536},
		{op: opJump, b: 0},
		{op: opMove, a: 1, b: 0},
		{op: opReturnOne, a: 1},
	}
	words, err := encodeWordcode(code, 2, 65537)
	if err != nil {
		t.Fatalf("encode forward/negative jumps: %v", err)
	}
	decoded, err := decodeWordcode(words)
	if err != nil {
		t.Fatalf("decode forward/negative jumps: %v", err)
	}
	if !reflect.DeepEqual(decoded, code) {
		t.Fatalf("jump round trip = %#v, want %#v", decoded, code)
	}

	endJump := []instruction{{op: opJump, b: 1}}
	endWords, err := encodeWordcode(endJump, 1, 0)
	if err != nil {
		t.Fatalf("encode end jump: %v", err)
	}
	if got, err := decodeWordcode(endWords); err != nil || !reflect.DeepEqual(got, endJump) {
		t.Fatalf("end jump round trip = %#v, %v", got, err)
	}

	// The AUX word of LOAD_CONST is at word PC 1. Make the following JUMP land
	// there instead of at a logical instruction boundary.
	jumpPC := 3
	bad := append([]wordcodeWord(nil), words...)
	jumpDisplacement := int32(-2)
	bad[jumpPC] = (bad[jumpPC] & 0xff) | (wordcodeWord(uint32(jumpDisplacement)&0x00ffffff) << 8)
	if _, err := decodeWordcode(bad); err == nil || !strings.Contains(err.Error(), "inside an instruction") {
		t.Fatalf("jump into AUX accepted: %v", err)
	}
}

func TestWordcodeLongConditionalJumpsPromoteToAux(t *testing.T) {
	const instructionCount = 32770
	forward := make([]instruction, instructionCount)
	for i := range forward {
		forward[i] = instruction{op: opMove, a: 0, b: 0}
	}
	forward[0] = instruction{op: opJumpIfFalse, a: 0, b: instructionCount - 1}
	forward[instructionCount-1] = instruction{op: opReturnOne, a: 0}
	words, err := encodeWordcode(forward, 1, 0)
	if err != nil {
		t.Fatalf("encode long forward conditional jump: %v", err)
	}
	if len(words) <= len(forward) {
		t.Fatalf("long forward jump did not gain AUX: words=%d instructions=%d", len(words), len(forward))
	}
	boundaries, err := wordcodeBoundaries(forward)
	if err != nil {
		t.Fatalf("forward boundaries: %v", err)
	}
	if boundaries[1]-boundaries[0] != 2 {
		t.Fatalf("long forward jump span = %d words, want 2", boundaries[1]-boundaries[0])
	}
	decoded, err := decodeWordcode(words)
	if err != nil || decoded[0] != forward[0] {
		t.Fatalf("long forward jump round trip = %#v, %v", decoded[0], err)
	}

	backward := make([]instruction, instructionCount)
	for i := range backward {
		backward[i] = instruction{op: opMove, a: 0, b: 0}
	}
	backward[instructionCount-2] = instruction{op: opJumpIfFalse, a: 0, b: 0}
	backward[instructionCount-1] = instruction{op: opReturnOne, a: 0}
	words, err = encodeWordcode(backward, 1, 0)
	if err != nil {
		t.Fatalf("encode long backward conditional jump: %v", err)
	}
	if len(words) <= len(backward) {
		t.Fatalf("long backward jump did not gain AUX: words=%d instructions=%d", len(words), len(backward))
	}
	boundaries, err = wordcodeBoundaries(backward)
	if err != nil {
		t.Fatalf("backward boundaries: %v", err)
	}
	if got := boundaries[instructionCount-1] - boundaries[instructionCount-2]; got != 2 {
		t.Fatalf("long backward jump span = %d words, want 2", got)
	}
	decoded, err = decodeWordcode(words)
	if err != nil || decoded[instructionCount-2] != backward[instructionCount-2] {
		t.Fatalf("long backward jump round trip = %#v, %v", decoded[instructionCount-2], err)
	}
}

func TestWordcodeSignedCallCountBoundaries(t *testing.T) {
	for _, raw := range []int{encodeFixedCallCount(0, true), encodeFixedCallCount(2, true), -32768} {
		ins := instruction{op: opCallOne, a: 0, b: 0, c: raw, d: 1}
		words, err := wordcodeEncodeInstruction(ins, 0, 2, []int{0, 2})
		if err != nil {
			t.Fatalf("encode fixed count %d: %v", raw, err)
		}
		got, err := decodeWordcode(words)
		if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], ins) {
			t.Fatalf("fixed count %d round trip = %#v, %v", raw, got, err)
		}
	}
	open := []instruction{
		{op: opVararg, a: 1, b: -1},
		{op: opCall, a: 1, b: 0, c: -1, d: 1},
	}
	words, err := encodeWordcode(open, 4, 0)
	if err != nil {
		t.Fatalf("encode open producer/consumer: %v", err)
	}
	if err := verifyWordcode(words, 4, 0); err != nil {
		t.Fatalf("verify open producer/consumer: %v", err)
	}
}

func TestWordcodeRejectsUnknownReservedOpcode(t *testing.T) {
	for _, raw := range []wordcodeWord{0x7f, 0xff} {
		if _, err := decodeWordcode([]wordcodeWord{raw}); err == nil || !strings.Contains(err.Error(), "unknown opcode") {
			t.Fatalf("reserved opcode %#x accepted: %v", raw, err)
		}
	}
}

func wordcodeSampleInstruction(op opcode) instruction {
	ins := instruction{op: op, a: 1, b: 2, c: 3, d: 4}
	meta, _ := opcodeMetadata(op)
	for _, slot := range []wordcodeOperandSlot{wordcodeSlotA, wordcodeSlotB, wordcodeSlotC, wordcodeSlotD} {
		switch wordcodeOperandKindFor(op, slot) {
		case bytecodeOperandRegister:
			setWordcodeSlot(&ins, slot, 1)
		case bytecodeOperandConstant:
			setWordcodeSlot(&ins, slot, 3)
		case bytecodeOperandGlobalSlot:
			setWordcodeSlot(&ins, slot, 0)
		case bytecodeOperandNativeID:
			setWordcodeSlot(&ins, slot, int(nativeFuncRawLen))
		case bytecodeOperandPrototype, bytecodeOperandUpvalue:
			setWordcodeSlot(&ins, slot, 0)
		case bytecodeOperandCount:
			setWordcodeSlot(&ins, slot, 1)
		case bytecodeOperandJumpTarget:
			setWordcodeSlot(&ins, slot, 0)
		case bytecodeOperandUnused:
			setWordcodeSlot(&ins, slot, 0)
		}
	}
	if op == opCallOne {
		ins.d = 1
	}
	if meta.jumpTarget != opcodeJumpTargetNone {
		setWordcodeSlot(&ins, opcodeJumpTargetSlotToWordcode(meta.jumpTarget), 0)
	}
	return ins
}

func TestWordcodeRejectsMalformedAUXAndReservedOpcodes(t *testing.T) {
	move := wordcodeWord(uint32(opMove) | uint32(1)<<8 | uint32(2)<<16)
	if err := verifyWordcode([]wordcodeWord{move | wordcodeAuxBit}, 4, 4); err == nil || !strings.Contains(err.Error(), "unexpected AUX") {
		t.Fatalf("verifyWordcode accepted unexpected AUX: %v", err)
	}
	if _, err := decodeWordcode([]wordcodeWord{uint32(opLoadGlobal) | uint32(1)<<8}); err == nil || !strings.Contains(err.Error(), "AUX") {
		t.Fatalf("decodeWordcode accepted missing required AUX: %v", err)
	}
	callOne := wordcodeWord(opCallOne) | wordcodeAuxBit
	if err := verifyWordcode([]wordcodeWord{callOne, 0}, 1, 0); err == nil || !strings.Contains(err.Error(), "canonical result count 1") {
		t.Fatalf("verifyWordcode accepted non-canonical CALL_ONE: %v", err)
	}
}

func TestWordcodeProtoVerifierReusesSemanticValidation(t *testing.T) {
	code := []instruction{{op: opLoadGlobal, a: 0, b: 0, c: 0}}
	words, err := encodeWordcode(code, 1, 1)
	if err != nil {
		t.Fatalf("encode LOAD_GLOBAL: %v", err)
	}
	proto := &Proto{registers: 1, constants: []Value{NumberValue(1)}}
	if err := verifyWordcodeForProto(proto, words); err == nil || !strings.Contains(err.Error(), "want string") {
		t.Fatalf("semantic verifier accepted numeric global name: %v", err)
	}
	if err := verifyWordcodeInstruction(instruction{op: opNewTable, a: 0, b: -1}, 1, 0, 0, 0, 1); err == nil || !strings.Contains(err.Error(), "non-negative") {
		t.Fatalf("word verifier accepted negative table capacity: %v", err)
	}
}

func TestWordcodeVerifierRejectsRegistersConstantsJumpsAndOpenCalls(t *testing.T) {
	forgedFixedMulti := instruction{
		op: opCall,
		a:  0,
		b:  0,
		c:  0,
		d:  encodeFixedMultiResultCount(5, 4),
	}
	cases := []struct {
		name string
		code []instruction
		want string
	}{
		{name: "register", code: []instruction{{op: opMove, a: 255, b: 0}}, want: "register"},
		{name: "constant", code: []instruction{{op: opLoadConst, a: 0, b: 4}}, want: "constant"},
		{name: "jump", code: []instruction{{op: opJump, b: 2}}, want: "jump target"},
		{name: "open call", code: []instruction{{op: opCall, a: 0, b: 1, c: -2, d: 1}}, want: "open call"},
		{name: "forged fixed multi span", code: []instruction{forgedFixedMulti}, want: "call result register range"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			words, err := encodeWordcode(tc.code, 4, 4)
			if err == nil {
				err = verifyWordcode(words, 4, 4)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wordcode verification error = %v, want %q", err, tc.want)
			}
		})
	}
	proto := &Proto{registers: 4}
	if err := verifyInstruction(proto, 0, forgedFixedMulti, 1); err == nil || !strings.Contains(err.Error(), "register range") {
		t.Fatalf("semantic verifier accepted forged fixed-multi result span: %v", err)
	}
}

func FuzzWordcodeDecoderNeverPanics(f *testing.F) {
	f.Add(uint32(opMove) | uint32(1)<<8 | uint32(2)<<16)
	f.Add(uint32(opJump) | uint32(0x80)<<8)
	f.Fuzz(func(t *testing.T, word uint32) {
		words := []wordcodeWord{word}
		_, _ = decodeWordcode(words)
		_ = verifyWordcode(words, 255, 256)
	})
}

func FuzzWordcodeDecoderBoundedBytes(f *testing.F) {
	f.Add([]byte{byte(opMove), 1, 2, 0})
	f.Add([]byte{byte(opLoadConst), 0x80, 0, 0, 1, 2, 3, 4})
	f.Fuzz(func(t *testing.T, data []byte) {
		count := len(data) / 4
		if count > 128 {
			count = 128
		}
		words := make([]wordcodeWord, count)
		for i := range words {
			words[i] = wordcodeWord(binary.LittleEndian.Uint32(data[i*4:]))
		}
		_, _ = decodeWordcode(words)
		_ = verifyWordcode(words, 255, 65536)
	})
}

func FuzzWordcodeRoundTrip(f *testing.F) {
	f.Add(byte(0), byte(1), uint16(0))
	f.Add(byte(2), byte(3), uint16(65535))
	f.Fuzz(func(t *testing.T, destination byte, source byte, constant uint16) {
		destination %= 255
		source %= 255
		registers := int(max(destination, source)) + 1
		code := []instruction{
			{op: opLoadConst, a: int(destination), b: int(constant)},
			{op: opMove, a: int(destination), b: int(source)},
			{op: opReturnOne, a: int(destination)},
		}
		words, err := encodeWordcode(code, registers, int(constant)+1)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		got, err := decodeWordcode(words)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !reflect.DeepEqual(got, code) {
			t.Fatalf("round trip = %#v, want %#v", got, code)
		}
	})
}

func TestWordcodeCacheSitesUseSemanticOperandsAndPhysicalRank(t *testing.T) {
	code := []instruction{
		{op: opSetStringField, a: 1, b: 1, c: 2},
		{op: opGetStringField, a: 3, b: 4, c: 2},
		{op: opSetStringFieldIndex, a: 1, b: 2, c: 3, d: 4},
		{op: opGetStringFieldIndex, a: 5, b: 6, c: 3, d: 7},
		{op: opSetIndex, a: 1, b: 2, c: 3},
		{op: opGetIndex, a: 4, b: 5, c: 6},
	}
	words, err := encodeWordcode(code, 8, 4)
	if err != nil {
		t.Fatalf("encode cache sites: %v", err)
	}
	boundaries, err := wordcodeBoundaries(code)
	if err != nil {
		t.Fatalf("cache boundaries: %v", err)
	}
	cacheIndex, err := buildWordcodeCacheIndex(code, boundaries, len(words))
	if err != nil {
		t.Fatalf("build cache index: %v", err)
	}
	decoded, _, err := wordcodeDecodeWords(words, cacheIndex)
	if err != nil {
		t.Fatalf("decode cache sites: %v", err)
	}
	if len(decoded) != len(code) {
		t.Fatalf("decoded %d instructions, want %d", len(decoded), len(code))
	}
	wantPrimary := [][3]uint32{
		{1, 1, 2},
		{3, 4, 2},
		{1, 3, 4},
		{5, 6, 7},
		{1, 2, 3},
		{4, 5, 6},
	}
	wantConstants := []int{1, 2, 2, 3, wordcodeCacheDynamicConstant, wordcodeCacheDynamicConstant}
	for index, entry := range decoded {
		wantID, ok := cacheIndex.cacheIDAt(entry.wordPC)
		if !ok || wantID != uint32(index) {
			t.Fatalf("cache site %d rank id = %d/%t, want %d", index, wantID, ok, index)
		}
		raw := words[entry.wordPC]
		gotPrimary := [3]uint32{(raw >> 8) & 0xff, (raw >> 16) & 0xff, (raw >> 24) & 0xff}
		if gotPrimary != wantPrimary[index] {
			t.Fatalf("cache site %d primary = %#v, want %#v", index, gotPrimary, wantPrimary[index])
		}
		if got := cacheIndex.constants[index]; got != wantConstants[index] {
			t.Fatalf("cache site %d descriptor = %d, want %d", index, got, wantConstants[index])
		}
	}
	got, err := decodeWordcode(words, cacheIndex)
	if err != nil || !reflect.DeepEqual(got, code) {
		t.Fatalf("cache round trip = %#v, err=%v, want %#v", got, err, code)
	}
}

func TestWordcodeRejectsMalformedCacheIndex(t *testing.T) {
	code := []instruction{
		{op: opGetIndex, a: 1, b: 2, c: 3},
		{op: opSetIndex, a: 1, b: 2, c: 3},
	}
	words, err := encodeWordcode(code, 4, 0)
	if err != nil {
		t.Fatalf("encode cache sites: %v", err)
	}
	boundaries, err := wordcodeBoundaries(code)
	if err != nil {
		t.Fatalf("cache boundaries: %v", err)
	}
	cacheIndex, err := buildWordcodeCacheIndex(code, boundaries, len(words))
	if err != nil {
		t.Fatalf("build cache index: %v", err)
	}
	proto := &Proto{registers: 4, cacheSiteCount: 2, cacheIndex: cacheIndex}
	for name, mutate := range map[string]func(*wordcodeCacheIndex){
		"rank length":      func(index *wordcodeCacheIndex) { index.rankPrefix = index.rankPrefix[:1] },
		"primary spoof":    func(index *wordcodeCacheIndex) { index.primaryBits[0] &^= 1 },
		"dynamic sentinel": func(index *wordcodeCacheIndex) { index.constants[0] = -2 },
		"dynamic constant": func(index *wordcodeCacheIndex) { index.constants[0] = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			bad := *cacheIndex
			bad.primaryBits = append([]uint64(nil), cacheIndex.primaryBits...)
			bad.rankPrefix = append([]uint32(nil), cacheIndex.rankPrefix...)
			bad.constants = append([]int(nil), cacheIndex.constants...)
			mutate(&bad)
			candidate := *proto
			candidate.cacheIndex = &bad
			if err := verifyWordcodeForProto(&candidate, words); err == nil {
				t.Fatal("accepted malformed cache index")
			}
		})
	}
}

func TestWordcodeRejectsCacheMarkOnNonCachePrimary(t *testing.T) {
	code := []instruction{
		{op: opMove, a: 0, b: 1},
		{op: opGetIndex, a: 0, b: 1, c: 2},
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
	index.primaryBits[0] = 1 // preserve one marked site, but move it from GET_INDEX to MOVE
	proto := &Proto{registers: 3, cacheSiteCount: 1, cacheIndex: index}
	if err := verifyWordcodeForProto(proto, words); err == nil || !strings.Contains(err.Error(), "cache mark") {
		t.Fatalf("accepted cache mark on non-cache primary: %v", err)
	}
}

func TestWordcodeCacheAUXBoundariesAndRelativeJumps(t *testing.T) {
	code := []instruction{
		{op: opJump, b: 4},
		{op: opGetIndex, a: 0, b: 0, c: 1},
		{op: opLoadConst, a: 0, b: 70000},
		{op: opSetIndex, a: 0, b: 0, c: 1},
		{op: opReturnOne, a: 0},
	}
	words, err := encodeWordcode(code, 2, 70001)
	if err != nil {
		t.Fatalf("encode cache boundary code: %v", err)
	}
	decoded, boundaries, err := wordcodeDecodeWords(words)
	if err != nil {
		t.Fatalf("decode cache boundary code: %v", err)
	}
	if len(decoded) != len(code) || boundaries[1] != 1 || boundaries[2] != 2 || boundaries[3] != 4 || boundaries[4] != 5 {
		t.Fatalf("boundaries = %#v, decoded=%d; want [0 1 3 5 7 8]", boundaries, len(decoded))
	}
	cacheIndex, err := buildWordcodeCacheIndex(code, boundaries, len(words))
	if err != nil {
		t.Fatalf("build cache index: %v", err)
	}
	if id, ok := cacheIndex.cacheIDAt(boundaries[1]); !ok || id != 0 {
		t.Fatalf("first cache ID = %d/%t, want 0", id, ok)
	}
	if id, ok := cacheIndex.cacheIDAt(boundaries[3]); !ok || id != 1 {
		t.Fatalf("second cache ID = %d/%t, want 1", id, ok)
	}
	got, err := decodeWordcode(words, cacheIndex)
	if err != nil || !reflect.DeepEqual(got, code) {
		t.Fatalf("relative cache jump round trip = %#v, err=%v, want %#v", got, err, code)
	}
	lines := wordcodeLogicalLineMap([]int{10, 20, 30, 40, 50}, boundaries)
	wantLines := []int{10, 20, 30, 0, 40, 50}
	if !reflect.DeepEqual(lines, wantLines) {
		t.Fatalf("physical line map = %#v, want %#v", lines, wantLines)
	}
	bad := append([]wordcodeWord(nil), words...)
	// Cache primaries are one word, while a non-cache AUX remains an invalid
	// jump target in the physical boundary map.
	bad[0] = (bad[0] & 0xff) | wordcodeWord(2)<<8
	if _, err := decodeWordcode(bad, cacheIndex); err == nil || !strings.Contains(err.Error(), "inside an instruction") {
		t.Fatalf("accepted jump into non-cache AUX: %v", err)
	}
}

func TestWordcodeCacheOpsStaySinglePrimaryWord(t *testing.T) {
	code := []instruction{
		{op: opSetStringField, a: 1, b: 300, c: 2},
		{op: opGetStringField, a: 3, b: 4, c: 301},
		{op: opSetStringFieldIndex, a: 1, b: 302, c: 3, d: 4},
		{op: opGetStringFieldIndex, a: 5, b: 6, c: 303, d: 7},
		{op: opSetIndex, a: 1, b: 2, c: 3},
		{op: opGetIndex, a: 4, b: 5, c: 6},
	}
	words, err := encodeWordcode(code, 8, 304)
	if err != nil {
		t.Fatalf("encode cache ops: %v", err)
	}
	if got, want := len(words), len(code); got != want {
		t.Fatalf("cache word count = %d, want one primary word per cache op (%d)", got, want)
	}
	for pc, word := range words {
		if word&wordcodeAuxBit != 0 {
			t.Fatalf("cache primary word %d carries AUX bit: %#x", pc, word)
		}
	}
}

func TestWordcodeCacheIndexRanksAcross64WordBlocks(t *testing.T) {
	code := make([]instruction, 0, 70)
	code = append(code, instruction{op: opGetIndex, a: 0, b: 1, c: 2})
	for len(code) < 65 {
		code = append(code, instruction{op: opMove, a: 0, b: 0})
	}
	code = append(code, instruction{op: opSetIndex, a: 0, b: 1, c: 2})
	words, err := encodeWordcode(code, 3, 0)
	if err != nil {
		t.Fatalf("encode rank fixture: %v", err)
	}
	boundaries, err := wordcodeBoundaries(code)
	if err != nil {
		t.Fatalf("rank boundaries: %v", err)
	}
	index, err := buildWordcodeCacheIndex(code, boundaries, len(words))
	if err != nil {
		t.Fatalf("build rank index: %v", err)
	}
	if got, ok := index.cacheIDAt(boundaries[0]); !ok || got != 0 {
		t.Fatalf("first cache rank = %d/%t, want 0", got, ok)
	}
	if got, ok := index.cacheIDAt(boundaries[65]); !ok || got != 1 {
		t.Fatalf("second-block cache rank = %d/%t, want 1", got, ok)
	}
	if got := len(index.rankPrefix); got != 3 {
		t.Fatalf("rank prefix entries = %d, want three block boundaries", got)
	}
}

func TestWordcodeCacheIndexIgnoresVariableAuxLowByteSpoof(t *testing.T) {
	code := []instruction{
		{op: opLoadConst, a: 0, b: 70000},
		{op: opGetIndex, a: 0, b: 1, c: 2},
	}
	words, err := encodeWordcode(code, 3, 70001)
	if err != nil {
		t.Fatalf("encode AUX spoof fixture: %v", err)
	}
	boundaries, err := wordcodeBoundaries(code)
	if err != nil {
		t.Fatalf("AUX spoof boundaries: %v", err)
	}
	index, err := buildWordcodeCacheIndex(code, boundaries, len(words))
	if err != nil {
		t.Fatalf("build AUX spoof index: %v", err)
	}
	// The non-cache LOAD_CONST AUX word is deliberately given a low byte that
	// looks like a cache opcode. Rank/select still marks only the real cache
	// primary at its physical PC.
	words[boundaries[0]+1] = wordcodeWord(opGetIndex)
	if _, ok := index.cacheIDAt(boundaries[0] + 1); ok {
		t.Fatal("variable AUX word was exposed as a cache primary")
	}
	if got, ok := index.cacheIDAt(boundaries[1]); !ok || got != 0 {
		t.Fatalf("cache rank after spoof = %d/%t, want 0", got, ok)
	}
	if _, err := decodeWordcode(words, index); err != nil {
		t.Fatalf("variable AUX low-byte spoof rejected: %v", err)
	}
}
