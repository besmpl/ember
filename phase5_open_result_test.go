package ember

import (
	"reflect"
	"strings"
	"testing"
)

func TestOpenResultBorrowProofRequiresDeadSuffix(t *testing.T) {
	safe := []instruction{
		{op: opCall, a: 0, b: 1, c: 0, d: -1},
		{op: opReturn, a: 0, b: -1},
	}
	marked := markBorrowableFixedCallWindows(safe, 4, nil)
	if got, want := marked[0].d, encodeOpenResultCallMarker(); got != want {
		t.Fatalf("safe open call marker = %d, want %d", got, want)
	}

	// Register 2 is in the borrowed suffix and is read after the call. The
	// proof must reject the marker even though the result is open.
	liveSuffix := []instruction{
		{op: opCall, a: 0, b: 1, c: 0, d: -1},
		{op: opMove, a: 3, b: 2},
		{op: opReturn, a: 0, b: -1},
	}
	marked = markBorrowableFixedCallWindows(liveSuffix, 4, nil)
	if got := marked[0].d; got != -1 {
		t.Fatalf("live suffix open call marker = %d, want semantic -1", got)
	}
}

func TestOpenResultMarkerWordcodeVerifier(t *testing.T) {
	code := []instruction{
		{op: opCall, a: 0, b: 1, c: 0, d: encodeOpenResultCallMarker()},
		{op: opReturn, a: 0, b: -1},
	}
	words, err := encodeWordcode(code, 4, 0)
	if err != nil {
		t.Fatalf("encode open-result marker: %v", err)
	}
	if err := verifyWordcode(words, 4, 0); err != nil {
		t.Fatalf("verify open-result marker producer: %v", err)
	}
	nonmatching := []instruction{
		{op: opCall, a: 0, b: 1, c: 0, d: encodeOpenResultCallMarker()},
		{op: opReturn, a: 1, b: -1},
	}
	words, err = encodeWordcode(nonmatching, 4, 0)
	if err != nil {
		t.Fatalf("encode nonmatching open-result marker: %v", err)
	}
	if err := verifyWordcode(words, 4, 0); err == nil || !strings.Contains(err.Error(), "not consumed by a matching open RETURN") {
		t.Fatalf("nonmatching open-result marker error = %v, want matching-return rejection", err)
	}

	invalid := []instruction{
		{op: opCall, a: 0, b: 1, c: -1, d: encodeOpenResultCallMarker()},
	}
	if err := verifyWordcodeInstruction(invalid[0], 4, 0, 0, 0, 1); err == nil || !strings.Contains(err.Error(), "requires fixed arguments") {
		t.Fatalf("invalid open-result marker error = %v, want fixed-argument rejection", err)
	}

	unconsumed := []instruction{
		{op: opCall, a: 0, b: 1, c: 0, d: encodeOpenResultCallMarker()},
		{op: opReturnOne, a: 0},
	}
	words, err = encodeWordcode(unconsumed, 4, 0)
	if err != nil {
		t.Fatalf("encode unconsumed open-result marker: %v", err)
	}
	if err := verifyWordcode(words, 4, 0); err == nil || !strings.Contains(err.Error(), "not consumed by a matching open RETURN") {
		t.Fatalf("unconsumed open-result marker error = %v, want matching-return rejection", err)
	}
}

func TestOpenResultMarkerRefinalizationIsIdempotent(t *testing.T) {
	proto := newProto(nil, []instruction{
		{op: opCall, a: 0, b: 1, c: 0, d: -1},
		{op: opReturn, a: 0, b: -1},
	}, nil, nil, 4, 0, false)
	if proto.verifyErr != nil {
		t.Fatalf("initial open-result prototype verification: %v", proto.verifyErr)
	}
	first, err := protoDecodedInstructions(proto)
	if err != nil {
		t.Fatalf("decode initial open-result prototype: %v", err)
	}
	if got, want := first[0].d, encodeOpenResultCallMarker(); got != want {
		t.Fatalf("initial marker = %d, want %d", got, want)
	}
	if err := finalizeProtoExecutionArtifact(proto); err != nil {
		t.Fatalf("re-finalize open-result prototype: %v", err)
	}
	second, err := protoDecodedInstructions(proto)
	if err != nil {
		t.Fatalf("decode re-finalized open-result prototype: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("re-finalized code changed:\nfirst=%#v\nsecond=%#v", first, second)
	}
}

func forceOpenResultBorrowMarkers(t *testing.T, proto *Proto) int {
	t.Helper()
	if proto == nil {
		return 0
	}
	marked := 0
	code, err := protoDecodedInstructions(proto)
	if err != nil {
		t.Fatalf("decode prototype for open-result marker: %v", err)
	}
	changed := false
	for index, ins := range code {
		if ins.op != opCall || ins.c < 0 || ins.d >= 0 {
			continue
		}
		code[index].d = encodeOpenResultCallMarker()
		changed = true
		marked++
	}
	if changed {
		if err := encodeProtoWords(proto, code); err != nil {
			t.Fatalf("encode forced open-result markers: %v", err)
		}
		proto.verifyErr = nil
	}
	for _, child := range proto.prototypes {
		marked += forceOpenResultBorrowMarkers(t, child)
	}
	return marked
}

func countOpenResultBorrowMarkers(t *testing.T, proto *Proto) int {
	t.Helper()
	if proto == nil {
		return 0
	}
	code, err := protoDecodedInstructions(proto)
	if err != nil {
		t.Fatalf("decode prototype for open-result marker count: %v", err)
	}
	marked := 0
	for _, ins := range code {
		if ins.op == opCall && decodeOpenResultCallMarker(ins.d) {
			marked++
		}
	}
	for _, child := range proto.prototypes {
		marked += countOpenResultBorrowMarkers(t, child)
	}
	return marked
}

func assertNumberResults(t *testing.T, got []Value, want ...float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("result count = %d, want %d (%v)", len(got), len(want), got)
	}
	for index, expected := range want {
		value, ok := got[index].Number()
		if !ok || value != expected {
			t.Fatalf("result %d = %v (%t), want %v", index, got[index], ok, expected)
		}
	}
}

func TestOpenResultRecordOnlyLoopsAndFallbacks(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []float64
		force  bool
	}{
		{
			name: "empty",
			source: `
local function child()
end
local function pass()
		return child()
end
return pass()
`,
			want: []float64{},
		},
		{
			name: "one",
			source: `
local function child()
		return 7
end
local function pass()
		return child()
end
return pass()
`,
			want: []float64{7},
		},
		{
			name: "many",
			source: `
local function child()
		return 1, 2, 3
end
local function pass()
		return child()
end
return pass()
`,
			want: []float64{1, 2, 3},
		},
		{
			name: "recursive",
			source: `
local function recurse(n)
		if n == 0 then
			return 1, 2
		end
		return recurse(n - 1)
end
return recurse(4)
`,
			want: []float64{1, 2},
		},
		{
			name: "captured-fallback",
			source: `
local value = 10
local function child()
		value = value + 1
		return value, value + 1
end
local function pass()
		return child()
end
return pass()
`,
			want:  []float64{11, 12},
			force: true,
		},
		{
			name: "variadic-fallback",
			source: `
local function child(...)
		return ...
end
local function pass()
		return child(1, 2, 3)
end
return pass()
`,
			want:  []float64{1, 2, 3},
			force: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proto, err := Compile(test.source)
			if err != nil {
				t.Fatalf("Compile returned error: %v", err)
			}
			markerKind := "naturally marked"
			marked := countOpenResultBorrowMarkers(t, proto)
			if test.force {
				markerKind = "forced"
				marked = forceOpenResultBorrowMarkers(t, proto)
			}
			if marked == 0 {
				t.Fatalf("source produced no %s fixed-argument open-result calls", markerKind)
			}

			thread := newVMThread(runtimeGlobals(nil))
			production, err := thread.run(proto, nil, nil)
			if err != nil {
				t.Fatalf("production run returned error: %v", err)
			}
			if test.name == "empty" {
				if len(production) != 1 || !production[0].IsNil() {
					t.Fatalf("empty production result = %v, want one nil", production)
				}
			} else {
				assertNumberResults(t, production, test.want...)
			}
			if thread.maxFrameRecords == 0 {
				t.Fatalf("production run used no compact frame records")
			}
			switch test.name {
			case "empty", "one", "many", "recursive":
				if thread.maxFrames != 1 {
					t.Fatalf("%s production max physical frames = %d, want 1", test.name, thread.maxFrames)
				}
			}

			instrumented, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
			if err != nil {
				t.Fatalf("instrumented run returned error: %v", err)
			}
			if test.name == "empty" {
				if len(instrumented) != 1 || !instrumented[0].IsNil() {
					t.Fatalf("empty instrumented result = %v, want one nil", instrumented)
				}
			} else {
				assertNumberResults(t, instrumented, test.want...)
			}
			if snapshot.picCounts.fixedCallTrampolineEntries == 0 {
				t.Fatalf("instrumented run used no compact frame records")
			}
		})
	}
}
