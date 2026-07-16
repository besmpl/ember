package ember_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/besmpl/ember"
)

const (
	parityTimerStartMarker = "-- EMBER_PARITY_TIMER_START"
	parityTimerStopMarker  = "-- EMBER_PARITY_TIMER_STOP"
)

type parityFixtureVariant struct {
	caseName         string
	batchName        string
	transformHoldout bool
}

var (
	parityDefaultFixtureVariant = parityFixtureVariant{
		caseName:  "__case",
		batchName: "__batch",
	}
	parityHoldoutFixtureVariant = parityFixtureVariant{
		caseName:         "__holdout_case",
		batchName:        "__holdout_batch",
		transformHoldout: true,
	}
)

func seedParityNumericLiterals(source string) (string, error) {
	for _, declaration := range []string{
		"local total = 0",
		"local score = 0",
		"local removed = 0",
		"local a = 0",
		"local cash = 0",
		"local misses = 0",
	} {
		if index := strings.Index(source, declaration); index >= 0 {
			value := index + len(declaration) - 1
			return source[:value] + "(__seed % 3)" + source[value+1:], nil
		}
	}
	var output strings.Builder
	output.Grow(len(source) + len(source)/2)
	rewrites := 0
	for index := 0; index < len(source); {
		switch {
		case source[index] == '"' || source[index] == '\'':
			next, err := copyParityQuoted(&output, source, index, source[index])
			if err != nil {
				return "", err
			}
			index = next
		case index+1 < len(source) && source[index:index+2] == "--":
			next := strings.IndexByte(source[index:], '\n')
			if next < 0 {
				output.WriteString(source[index:])
				index = len(source)
				continue
			}
			next += index + 1
			output.WriteString(source[index:next])
			index = next
		case index+1 < len(source) && source[index:index+2] == "[[":
			end := strings.Index(source[index+2:], "]]")
			if end < 0 {
				return "", fmt.Errorf("seed parity source: unterminated long string")
			}
			end += index + 4
			output.WriteString(source[index:end])
			index = end
		case source[index] >= '0' && source[index] <= '9' && parityNumberBoundary(source, index):
			end := index + 1
			for end < len(source) && source[end] >= '0' && source[end] <= '9' {
				end++
			}
			if end < len(source) && (source[end] == '.' || isParityIdentifierByte(source[end])) {
				return "", fmt.Errorf("seed parity source: unsupported numeric literal near %q", source[index:])
			}
			fmt.Fprintf(&output, "(%s + (__seed %% 3))", source[index:end])
			rewrites++
			index = end
			if rewrites == 1 {
				output.WriteString(source[index:])
				index = len(source)
			}
		default:
			output.WriteByte(source[index])
			index++
		}
	}
	if rewrites == 0 {
		return "", fmt.Errorf("seed parity source: no numeric literals")
	}
	return output.String(), nil
}

func copyParityQuoted(output *strings.Builder, source string, start int, quote byte) (int, error) {
	for index := start; index < len(source); index++ {
		output.WriteByte(source[index])
		if source[index] == '\\' {
			index++
			if index >= len(source) {
				return 0, fmt.Errorf("seed parity source: unterminated escape")
			}
			output.WriteByte(source[index])
			continue
		}
		if index > start && source[index] == quote {
			return index + 1, nil
		}
	}
	return 0, fmt.Errorf("seed parity source: unterminated quoted string")
}

func parityNumberBoundary(source string, index int) bool {
	return index == 0 || !isParityIdentifierByte(source[index-1])
}

func isParityIdentifierByte(value byte) bool {
	return value == '_' ||
		value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9'
}

func transformParityHoldoutSource(source string) (string, error) {
	for index := 0; index < len(source); {
		switch {
		case source[index] == '"' || source[index] == '\'':
			var copied strings.Builder
			next, err := copyParityQuoted(&copied, source, index, source[index])
			if err != nil {
				return "", err
			}
			index = next
		case index+1 < len(source) && source[index:index+2] == "--":
			next := strings.IndexByte(source[index:], '\n')
			if next < 0 {
				return "", fmt.Errorf("holdout parity source: no numeric literal")
			}
			index += next + 1
		case index+1 < len(source) && source[index:index+2] == "[[":
			end := strings.Index(source[index+2:], "]]")
			if end < 0 {
				return "", fmt.Errorf("holdout parity source: unterminated long string")
			}
			index += end + 4
		case source[index] >= '0' && source[index] <= '9' && parityNumberBoundary(source, index):
			end := index + 1
			for end < len(source) && source[end] >= '0' && source[end] <= '9' {
				end++
			}
			if end < len(source) && (source[end] == '.' || isParityIdentifierByte(source[end])) {
				return "", fmt.Errorf("holdout parity source: unsupported numeric literal near %q", source[index:])
			}
			return "-- identity-holdout-v1\n" + source[:end] + ".0" + source[end:], nil
		default:
			index++
		}
	}
	return "", fmt.Errorf("holdout parity source: no numeric literal")
}

func runtimeParityGuestBatchProgram(source string, variant parityFixtureVariant) (string, string, error) {
	seeded, err := seedParityNumericLiterals(source)
	if err != nil {
		return "", "", err
	}
	if variant.transformHoldout {
		seeded, err = transformParityHoldoutSource(seeded)
		if err != nil {
			return "", "", err
		}
	}
	program := fmt.Sprintf(`local %s = function(__seed)
%s
end
local %s = function(__n, __seed)
    local __checksum = 0
    for __i = 1, __n do
        local __value = %s(__seed + __i)
        __checksum = __checksum + __value * (__i %% 7 + 1)
    end
    return __checksum
end
`, variant.caseName, seeded, variant.batchName, variant.caseName)
	return program, seeded, nil
}

func runtimeParityGuestBatchFixture(source string, variant parityFixtureVariant, luau bool) (string, error) {
	fixture, _, err := runtimeParityGuestBatchProgram(source, variant)
	if err != nil {
		return "", err
	}
	if !luau {
		return fixture + fmt.Sprintf("return { startup = function() __parity_capture(%s) end }\n", variant.batchName), nil
	}
	return fixture + fmt.Sprintf(`local __n_arg, __seed_arg = ...
local __n = tonumber(__n_arg)
local __seed = tonumber(__seed_arg)
local __warm = %s(1, __seed)
local __start = os.clock()
%s
local __sink = %s(__n, __seed)
%s
local __elapsed_ns = (os.clock() - __start) * 1000000000
print(__elapsed_ns)
print(__sink)
`, variant.batchName, parityTimerStartMarker, variant.batchName, parityTimerStopMarker), nil
}

func runtimeParityPublicCallFixture(source string, iterations int, luau bool) string {
	callable := "local __case = function()\n" + source + "\nend\n"
	if luau {
		return callable + fmt.Sprintf("local __sink = nil\nlocal __warm = __case()\nlocal __start = os.clock()\n%s\nfor __i = 1, %d do\n    __sink = __case()\nend\n%s\nlocal __elapsed_ns = (os.clock() - __start) * 1000000000\nprint(__elapsed_ns)\nprint(__sink)\n", parityTimerStartMarker, iterations, parityTimerStopMarker)
	}
	return callable + "return { startup = function() __parity_capture(__case) end }\n"
}

func timerRegion(source string) (string, error) {
	start := strings.Index(source, parityTimerStartMarker)
	stop := strings.Index(source, parityTimerStopMarker)
	if start < 0 || stop < 0 || stop <= start {
		return "", fmt.Errorf("timer markers missing or out of order")
	}
	return source[start+len(parityTimerStartMarker) : stop], nil
}

func TestRuntimeParityGuestBatchTimerContract(t *testing.T) {
	source, err := runtimeParityGuestBatchFixture("local x = 1\nreturn x + 2", parityDefaultFixtureVariant, true)
	if err != nil {
		t.Fatal(err)
	}
	region, err := timerRegion(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Compile", "function", "os.clock", "print(", "capture", "Close", "local __warm", "tonumber", "for __i"} {
		if strings.Contains(region, forbidden) {
			t.Fatalf("Luau timed region contains setup %q: %s", forbidden, region)
		}
	}
	if want := "\nlocal __sink = __batch(__n, __seed)\n"; region != want {
		t.Fatalf("Luau timed region = %q, want %q", region, want)
	}
	if strings.Count(source, "local __case = function(__seed)") != 1 ||
		strings.Count(source, "local __batch = function(__n, __seed)") != 1 ||
		strings.Count(source, "__case(__seed + __i)") != 1 {
		t.Fatalf("guest batch does not contain one runtime-parameterized program: %q", source)
	}
	for _, fixed := range []string{"function(__n, __seed)\n    local __checksum", "for __i = 1, __n"} {
		if !strings.Contains(source, fixed) {
			t.Fatalf("guest batch missing %q: %q", fixed, source)
		}
	}
}

func TestRuntimeParityPublicCallTimerContract(t *testing.T) {
	source := runtimeParityPublicCallFixture("return 1 + 2", 10, true)
	region, err := timerRegion(source)
	if err != nil {
		t.Fatal(err)
	}
	if want := "\nfor __i = 1, 10 do\n    __sink = __case()\nend\n"; region != want {
		t.Fatalf("public-call timed region = %q, want %q", region, want)
	}
	if emberSource := runtimeParityPublicCallFixture("return 1 + 2", 10, false); strings.Contains(emberSource, "for __i") || !strings.Contains(emberSource, "__parity_capture(__case)") {
		t.Fatalf("Ember public-call fixture changed: %q", emberSource)
	}
}

func TestRuntimeParityGuestBatchTimedEntrySentinel(t *testing.T) {
	const (
		n    = 100
		seed = int64(29)
	)
	calls := 0
	events := make([]string, 0, 3)
	call := func(gotN int, gotSeed int64) ([]ember.Value, error) {
		events = append(events, "call")
		calls++
		if gotN != n || gotSeed != seed {
			t.Fatalf("guest batch args = (%d,%d), want (%d,%d)", gotN, gotSeed, n, seed)
		}
		return []ember.Value{ember.NumberValue(float64(n) + float64(seed))}, nil
	}
	now := func() time.Time { events = append(events, "start"); return time.Unix(0, 1) }
	since := func(time.Time) time.Duration { events = append(events, "stop"); return time.Nanosecond }
	_, values, err := measureParityTimedBatch(n, seed, call, now, since)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parityScalarString(values)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || result != "129" || strings.Join(events, ",") != "start,call,stop" {
		t.Fatalf("guest batch calls/result/events = %d/%q/%v, want 1/129/[start call stop]", calls, result, events)
	}
}

func TestRuntimeParityPublicCallTimerSentinel(t *testing.T) {
	const n = 100
	calls := 0
	events := make([]string, 0, n+2)
	call := func() ([]ember.Value, error) {
		events = append(events, "call")
		calls++
		return []ember.Value{ember.NumberValue(7)}, nil
	}
	now := func() time.Time { events = append(events, "start"); return time.Unix(0, 1) }
	since := func(time.Time) time.Duration { events = append(events, "stop"); return time.Nanosecond }
	_, values, err := measureParityTimedCalls(n, call, now, since)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parityScalarString(values)
	if err != nil {
		t.Fatal(err)
	}
	if calls != n || result != "7" || len(events) != n+2 || events[0] != "start" || events[len(events)-1] != "stop" {
		t.Fatalf("public calls/result/events = %d/%q/%v", calls, result, events)
	}
}

func TestSeedParityNumericLiterals(t *testing.T) {
	source := "local x = 10 -- 20\nlocal s = \"30\"\nreturn x + 2"
	got, err := seedParityNumericLiterals(source)
	if err != nil {
		t.Fatal(err)
	}
	want := "local x = (10 + (__seed % 3)) -- 20\nlocal s = \"30\"\nreturn x + 2"
	if got != want {
		t.Fatalf("seeded source = %q, want %q", got, want)
	}
	accumulator, err := seedParityNumericLiterals("local total = 0\nreturn total + 7")
	if err != nil {
		t.Fatal(err)
	}
	if want := "local total = (__seed % 3)\nreturn total + 7"; accumulator != want {
		t.Fatalf("seeded accumulator = %q, want %q", accumulator, want)
	}
}

func TestRuntimeParityFixtureVariantsChangeIdentity(t *testing.T) {
	source := "return 1 + 2"
	standard, err := runtimeParityGuestBatchFixture(source, parityDefaultFixtureVariant, false)
	if err != nil {
		t.Fatal(err)
	}
	holdout, err := runtimeParityGuestBatchFixture(source, parityHoldoutFixtureVariant, false)
	if err != nil {
		t.Fatal(err)
	}
	if standard == holdout {
		t.Fatal("holdout fixture did not change generated identity")
	}
	if !strings.Contains(holdout, "-- identity-holdout-v1") || !strings.Contains(holdout, ".0") {
		t.Fatalf("holdout fixture did not transform source layout/literal spelling: %q", holdout)
	}
	for _, forbidden := range []string{"arithmetic_for", "top10/", "scenario/", "expected", "benchmark"} {
		if strings.Contains(standard, forbidden) || strings.Contains(holdout, forbidden) {
			t.Fatalf("fixture contains forbidden identity selector %q", forbidden)
		}
	}
}
