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

// runtimeParityTimerFixture keeps the timed region deliberately narrow: all
// setup, compilation, binding, and result validation precede the start marker.
func runtimeParityTimerFixture(source string, iterations int, luau bool) string {
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

func TestRuntimeParityTimerContract(t *testing.T) {
	source := runtimeParityTimerFixture("return 1 + 2", 10, true)
	region, err := timerRegion(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Compile", "__case = function", "os.clock", "print(", "capture", "Close", "local __warm"} {
		if strings.Contains(region, forbidden) {
			t.Fatalf("Luau timed region contains setup %q: %s", forbidden, region)
		}
	}
	if strings.Count(source, parityTimerStartMarker) != 1 || strings.Count(source, parityTimerStopMarker) != 1 {
		t.Fatal("Luau marker count mismatch")
	}
	wantRegion := "\nfor __i = 1, 10 do\n    __sink = __case()\nend\n"
	if region != wantRegion {
		t.Fatalf("Luau timed region = %q, want exact direct loop %q", region, wantRegion)
	}
	if ember := runtimeParityTimerFixture("return 1 + 2", 10, false); strings.Contains(ember, "for __i") || !strings.Contains(ember, "__parity_capture(__case)") {
		t.Fatalf("Ember fixture must capture the prepared callable directly: %q", ember)
	}
}

func TestRuntimeParityTimerSentinel(t *testing.T) {
	const n = 100
	source := runtimeParityTimerFixture("return 7", n, true)
	if got := strings.Count(source, "local __case = function"); got != 1 {
		t.Fatalf("callable constructions = %d, want 1 setup construction", got)
	}
	region, err := timerRegion(source)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(region, "function()") || strings.Contains(region, "local __case") || strings.Contains(region, "local __warm") {
		t.Fatal("callable construction appeared inside timed region")
	}
	if got := strings.Count(region, "__case()"); got != 1 || !strings.Contains(region, "1, 100") {
		t.Fatalf("Luau generated timed loop changed: %q", region)
	}

	constructions := 0
	calls := 0
	events := make([]string, 0, n+2)
	construct := func() parityTimedCall {
		constructions++
		return func() ([]ember.Value, error) {
			events = append(events, "call")
			calls++
			return []ember.Value{ember.NumberValue(7)}, nil
		}
	}
	callable := construct()
	now := func() time.Time { events = append(events, "start"); return time.Unix(0, 1) }
	since := func(time.Time) time.Duration { events = append(events, "stop"); return time.Nanosecond }
	before := constructions
	_, values, err := measureParityTimedCalls(n, callable, now, since)
	if err != nil {
		t.Fatal(err)
	}
	result, err := parityScalarString(values)
	if err != nil {
		t.Fatal(err)
	}
	if constructions-before != 0 || calls != n || result != "7" {
		t.Fatalf("Ember timed sentinel constructions/calls/result = %d/%d/%q, want 0/%d/7", constructions-before, calls, result, n)
	}
	if len(events) != n+2 || events[0] != "start" || events[len(events)-1] != "stop" {
		t.Fatalf("Ember timer events do not bracket exactly N calls: %v", events)
	}
}
