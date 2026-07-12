package ember

import "testing"

func TestOpenArgumentCallUsesRecordOnlyWindow(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   float64
	}{
		{
			name: "open-result",
			source: `
	local function child()
		return 7, 8, 9
	end
	local function consume(a, b, c, d)
	return a + b + c + d
	end
	return consume(5, child())
		`,
			want: 29,
		},
		{
			name: "fixed-result",
			source: `
	local function child()
		return 7, 8, 9
	end
	local function consume(a, b, c, d)
		return a + b + c + d
	end
	local total = consume(5, child())
	return total
			`,
			want: 29,
		},
		{
			name: "two-prefix-arguments",
			source: `
local function child()
		return 7, 8
end
local function consume(a, b, c, d)
	return a + b + c + d
end
return consume(1, 2, child())
			`,
			want: 18,
		},
		{
			name: "chained-open-forwarding",
			source: `
local function leaf()
		return 4, 5
end
local function consume(a, b, c)
		return a + b + c
end
local function middle()
		return consume(1, leaf())
end
return middle()
			`,
			want: 10,
		},
		{
			name: "ignored-tail-cleanup",
			source: `
local function child()
		return 7, 8, 9
	end
local function consume(a)
		return a
	end
local total = consume(child())
return total
		`,
			want: 7,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proto, err := Compile(test.source)
			if err != nil {
				t.Fatalf("Compile returned error: %v", err)
			}
			thread := newVMThread(runtimeGlobals(nil))
			results, err := thread.run(proto, nil, nil)
			if err != nil {
				t.Fatalf("thread.run returned error: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("thread.run returned %d results, want 1 (%v)", len(results), results)
			}
			if got, ok := results[0].Number(); !ok || got != test.want {
				t.Fatalf("result = %v (%t), want %v", results[0], ok, test.want)
			}
			if thread.maxFrameRecords < 1 {
				t.Fatalf("max frame-record depth = %d, want record-only open-argument call", thread.maxFrameRecords)
			}
			if thread.maxFrames != 1 {
				t.Fatalf("max physical frame depth = %d, want 1", thread.maxFrames)
			}
			if test.name == "ignored-tail-cleanup" {
				for index, value := range thread.stackOwner.values[:cap(thread.stackOwner.values)] {
					if number, ok := value.Number(); ok && (number == 8 || number == 9) {
						t.Fatalf("owner slot %d retained forwarded argument %v", index, value)
					}
				}
			}
			if len(thread.frameRecords) != 0 || len(thread.frames) != 0 {
				t.Fatalf("thread retained %d records and %d frames", len(thread.frameRecords), len(thread.frames))
			}
		})
	}

	// Exercise the instrumented direct loop as well as the production loop
	// above. The open-argument marker must be a hot record-only transition in
	// both dispatchers; otherwise the marker is merely being accepted by the
	// cold fallback.
	proto, err := Compile(tests[0].source)
	if err != nil {
		t.Fatalf("Compile instrumented open-argument source: %v", err)
	}
	results, snapshot, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("instrumented open-argument run returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("instrumented open-argument run returned %d results, want one", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != tests[0].want {
		t.Fatalf("instrumented open-argument result = %v (%t), want %v", results[0], ok, tests[0].want)
	}
	if snapshot.picCounts.fixedCallTrampolineEntries == 0 {
		t.Fatal("instrumented open-argument call did not enter the record-only trampoline")
	}
	if snapshot.picCounts.fixedCallFrameMaterializations != 0 || snapshot.picCounts.fixedCallArgCopies != 0 {
		t.Fatalf("instrumented open-argument call materialized/copied = %d/%d, want zero dynamic frame work", snapshot.picCounts.fixedCallFrameMaterializations, snapshot.picCounts.fixedCallArgCopies)
	}
}

func TestOpenArgumentRecordErrorClearsOwnerTail(t *testing.T) {
	proto, err := Compile(`
local function child()
	return 7, 8, 9
end
local function fail(a)
	error("open argument failure")
end
return fail(5, child())
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	if _, err := thread.run(proto, nil, nil); err == nil {
		t.Fatal("thread.run returned nil error, want failure from open-argument callee")
	}
	if thread.maxFrameRecords < 1 {
		t.Fatalf("max frame-record depth = %d, want open-argument record before error", thread.maxFrameRecords)
	}
	if owner := thread.stackOwner; owner != nil {
		for index, value := range owner.values[:cap(owner.values)] {
			if number, ok := value.Number(); ok && (number == 5 || number == 7 || number == 8 || number == 9) {
				t.Fatalf("owner slot %d retained dropped open-argument value %v", index, value)
			}
		}
	}
	if len(thread.frameRecords) != 0 || len(thread.frames) != 0 {
		t.Fatalf("thread retained %d records and %d frames after error", len(thread.frameRecords), len(thread.frames))
	}
}
