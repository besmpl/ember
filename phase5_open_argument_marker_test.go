package ember

import (
	"reflect"
	"strings"
	"testing"
)

func openArgumentMarkerForTest(prefixCount int) int {
	marker, ok := encodeOpenArgumentCallMarker(prefixCount)
	if !ok {
		panic("open argument marker is not representable")
	}
	return marker
}

func TestOpenArgumentCallMarkerEncoding(t *testing.T) {
	for _, prefix := range []int{0, 1, 255} {
		raw := openArgumentMarkerForTest(prefix)
		got, marked := decodeOpenArgumentCallMarker(raw)
		if !marked || got != prefix {
			t.Fatalf("prefix %d encoded as %d, decoded (%d, %t)", prefix, raw, got, marked)
		}
	}
	for _, prefix := range []int{-1, 256} {
		if raw, ok := encodeOpenArgumentCallMarker(prefix); ok {
			t.Fatalf("prefix %d encoded as %d, want unrepresentable", prefix, raw)
		}
	}
	if _, marked := decodeOpenArgumentCallMarker(encodeOpenResultCallMarker()); marked {
		t.Fatal("open-result marker decoded as open-argument marker")
	}
	for _, raw := range []int{openArgumentCallMarkerMin - 1, openArgumentCallMarkerMax + 1} {
		if _, marked := decodeOpenArgumentCallMarker(raw); marked {
			t.Fatalf("out-of-band marker %d decoded", raw)
		}
	}
}

func TestOpenArgumentCallMarkerNormalizationIsIdempotent(t *testing.T) {
	code := []instruction{{op: opVararg, a: 2, b: -1}, {op: opCall, a: 0, b: 1, c: openArgumentMarkerForTest(0), d: 1}}
	normalized := normalizeOpenArgumentCallMarkers(code)
	if got, want := normalized[1].c, -1; got != want {
		t.Fatalf("normalized c = %d, want %d", got, want)
	}
	if again := normalizeOpenArgumentCallMarkers(normalized); !reflect.DeepEqual(again, normalized) {
		t.Fatalf("normalization changed semantic code: %#v -> %#v", normalized, again)
	}
}

func TestOpenArgumentCallBorrowMarkingIsIdempotent(t *testing.T) {
	code := []instruction{
		{op: opVararg, a: 4, b: -1},
		{op: opCall, a: 0, b: 1, c: -3, d: 1},
		{op: opReturnOne, a: 0},
	}
	marked := markBorrowableFixedCallWindows(code, 6, nil)
	remarked := markBorrowableFixedCallWindows(marked, 6, nil)
	if !reflect.DeepEqual(remarked, marked) {
		t.Fatalf("re-marking changed encoded code: %#v -> %#v", marked, remarked)
	}
}

func TestOpenArgumentCallBorrowProof(t *testing.T) {
	safe := []instruction{{op: opVararg, a: 2, b: -1}, {op: opCall, a: 0, b: 1, c: -1, d: 1}, {op: opReturnOne, a: 0}}
	marked := markBorrowableFixedCallWindows(safe, 4, nil)
	if got, want := marked[1].c, openArgumentMarkerForTest(0); got != want {
		t.Fatalf("safe marker = %d, want %d", got, want)
	}

	liveProducer := []instruction{{op: opVararg, a: 2, b: -1}, {op: opCall, a: 0, b: 1, c: -1, d: 1}, {op: opMove, a: 3, b: 2}, {op: opReturnOne, a: 0}}
	if got := markBorrowableFixedCallWindows(liveProducer, 4, nil)[1].c; got != openArgumentMarkerForTest(0) {
		t.Fatalf("live producer marker = %d, want zero-prefix marker", got)
	}
	withTail := []instruction{{op: opVararg, a: 4, b: -1}, {op: opCall, a: 0, b: 1, c: -3, d: 1}, {op: opMove, a: 1, b: 4}, {op: opReturnOne, a: 0}}
	if got := markBorrowableFixedCallWindows(withTail, 6, nil)[1].c; got != -3 {
		t.Fatalf("live scratch tail marker = %d, want semantic -3", got)
	}
	captured := []bool{false, false, false, false, true, false}
	if got := markBorrowableFixedCallWindows(withTail, 6, captured)[1].c; got != -3 {
		t.Fatalf("captured scratch tail marker = %d, want semantic -3", got)
	}

	prefix := []instruction{{op: opVararg, a: 4, b: -1}, {op: opCall, a: 0, b: 1, c: -3, d: 1}, {op: opReturnOne, a: 0}}
	if got, want := markBorrowableFixedCallWindows(prefix, 6, nil)[1].c, openArgumentMarkerForTest(2); got != want {
		t.Fatalf("multi-prefix marker = %d, want %d", got, want)
	}
	overlappingScratch := []instruction{
		{op: opVararg, a: 5, b: -1},
		{op: opCall, a: 0, b: 2, c: -3, d: 1},
		{op: opReturnOne, a: 0},
	}
	// With six caller registers, two fixed arguments start at R3 and the
	// borrow scratch window starts at R4. The prefix source and destination
	// overlap, but the runtime copy is memmove-safe and the suffix proof still
	// protects every overwritten register.
	if got, want := markBorrowableFixedCallWindows(overlappingScratch, 6, nil)[1].c, openArgumentMarkerForTest(2); got != want {
		t.Fatalf("overlapping scratch marker = %d, want %d", got, want)
	}
}

func TestOpenArgumentCallBorrowProofMarksOpenResults(t *testing.T) {
	code := []instruction{
		{op: opVararg, a: 2, b: -1},
		{op: opCall, a: 0, b: 1, c: -1, d: -1},
		{op: opReturn, a: 0, b: -1},
	}
	marked := markBorrowableFixedCallWindows(code, 4, nil)
	if got, want := marked[1].c, openArgumentMarkerForTest(0); got != want {
		t.Fatalf("open-result call c marker = %d, want %d", got, want)
	}
	if got, want := marked[1].d, encodeOpenResultCallMarker(); got != want {
		t.Fatalf("open-result call d marker = %d, want %d", got, want)
	}
}

func TestOpenArgumentCallBorrowProofFailsClosedForUnrepresentablePrefix(t *testing.T) {
	const prefixCount = 256
	code := []instruction{
		{op: opVararg, a: prefixCount + 1, b: -1},
		{op: opCall, a: 0, b: 0, c: -(prefixCount + 1), d: 1},
		{op: opReturnOne, a: 0},
	}
	marked := markBorrowableFixedCallWindows(code, 600, nil)
	if got, want := marked[1].c, code[1].c; got != want {
		t.Fatalf("unrepresentable prefix changed c from %d to %d", want, got)
	}
}

func TestOpenArgumentCallMarkerWordcodeVerifier(t *testing.T) {
	valid := []instruction{{op: opVararg, a: 2, b: -1}, {op: opCall, a: 0, b: 1, c: openArgumentMarkerForTest(0), d: 1}}
	words, err := encodeWordcode(valid, 4, 0)
	if err != nil {
		t.Fatalf("encode marker: %v", err)
	}
	if err := verifyWordcode(words, 4, 0); err != nil {
		t.Fatalf("verify marker: %v", err)
	}
	forged := []instruction{{op: opCall, a: 0, b: 1, c: openArgumentMarkerForTest(0), d: 1}}
	words, err = encodeWordcode(forged, 4, 0)
	if err != nil {
		t.Fatalf("encode forged marker: %v", err)
	}
	if err := verifyWordcode(words, 4, 0); err == nil || !strings.Contains(err.Error(), "no preceding open-result producer") {
		t.Fatalf("forged marker error = %v", err)
	}
	coexisting := []instruction{
		{op: opVararg, a: 2, b: -1},
		{op: opCall, a: 0, b: 1, c: openArgumentMarkerForTest(0), d: encodeOpenResultCallMarker()},
		{op: opReturn, a: 0, b: -1},
	}
	words, err = encodeWordcode(coexisting, 4, 0)
	if err != nil {
		t.Fatalf("encode coexisting markers: %v", err)
	}
	if err := verifyWordcode(words, 4, 0); err != nil {
		t.Fatalf("verify coexisting markers: %v", err)
	}
	chained := []instruction{
		{op: opVararg, a: 2, b: -1},
		{op: opCall, a: 4, b: 1, c: openArgumentMarkerForTest(0), d: encodeOpenResultCallMarker()},
		{op: opCall, a: 0, b: 3, c: openArgumentMarkerForTest(0), d: 1},
	}
	words, err = encodeWordcode(chained, 6, 0)
	if err != nil {
		t.Fatalf("encode chained markers: %v", err)
	}
	if err := verifyWordcode(words, 6, 0); err != nil {
		t.Fatalf("verify chained markers: %v", err)
	}
}

func TestOpenArgumentCallMarkerRegisterEffects(t *testing.T) {
	ins := instruction{op: opCall, a: 0, b: 1, c: openArgumentMarkerForTest(2), d: 1}
	got := []int{}
	iterator := instructionRegistersBounded(ins, instructionRegisterRead, 7)
	for register, ok := iterator.next(); ok; register, ok = iterator.next() {
		got = append(got, register)
	}
	want := []int{1, 2, 3, 4, 5, 6}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("marker read registers = %#v, want %#v", got, want)
	}
}
