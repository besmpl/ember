package ember

import (
	"math"
	"strconv"
	"testing"
)

func TestSmallIntegerStringSignedBounds(t *testing.T) {
	tests := []struct {
		name   string
		number float64
		want   string
		ok     bool
	}{
		{name: "negative bound", number: -999, want: "-999", ok: true},
		{name: "positive bound", number: 999, want: "999", ok: true},
		{name: "negative outside", number: -1000},
		{name: "positive outside", number: 1000},
		{name: "fraction", number: 1.5},
		{name: "negative fraction", number: -1.5},
		{name: "nan", number: math.NaN()},
		{name: "positive infinity", number: math.Inf(1)},
		{name: "negative infinity", number: math.Inf(-1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := smallIntegerString(test.number)
			if ok != test.ok || (ok && got != test.want) {
				t.Fatalf("smallIntegerString(%v) = %q, %t; want %q, %t", test.number, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestSmallIntegerStringDistinguishesSignedZero(t *testing.T) {
	if got, ok := smallIntegerString(0); !ok || got != "0" {
		t.Fatalf("smallIntegerString(+0) = %q, %t; want 0, true", got, ok)
	}
	negativeZero := math.Copysign(0, -1)
	if _, ok := smallIntegerString(negativeZero); ok {
		t.Fatal("smallIntegerString(-0) hit signed cache")
	}
	if got := formatLuauNumber(negativeZero); got != "-0" {
		t.Fatalf("formatLuauNumber(-0) = %q, want -0", got)
	}
	if got := string(appendLuauNumber(nil, negativeZero)); got != "-0" {
		t.Fatalf("appendLuauNumber(-0) = %q, want -0", got)
	}
}

func TestLuauNumberFormatAndAppendParity(t *testing.T) {
	numbers := make([]float64, 0, 2012)
	for number := -999; number <= 999; number++ {
		numbers = append(numbers, float64(number))
	}
	numbers = append(numbers,
		-1_000_001, -1_000_000, -1000, 1000, 1_000_000, 1_000_001,
		1.5, -1.5, math.Copysign(0, -1), math.NaN(), math.Inf(1), math.Inf(-1),
	)
	for _, number := range numbers {
		want := referenceLuauNumber(number)
		formatted := formatLuauNumber(number)
		appended := string(appendLuauNumber(nil, number))
		if formatted != want || appended != want {
			t.Fatalf("number %v format=%q append=%q want=%q", number, formatted, appended, want)
		}
	}
}

func referenceLuauNumber(number float64) string {
	if number == math.Trunc(number) && !math.Signbit(number) && number < 1_000_000 {
		return strconv.FormatInt(int64(number), 10)
	}
	if number == math.Trunc(number) && math.Signbit(number) && number != 0 && number > -1_000_000 {
		return strconv.FormatInt(int64(number), 10)
	}
	return strconv.FormatFloat(number, 'g', -1, 64)
}

func TestLuauNumberFormatPreservesLargeIntegerFloatSemantics(t *testing.T) {
	for _, number := range []float64{-1_000_000, 1_000_000, -1_000_001, 1_000_001} {
		want := strconv.FormatFloat(number, 'g', -1, 64)
		if got := formatLuauNumber(number); got != want {
			t.Fatalf("formatLuauNumber(%v) = %q, want %q", number, got, want)
		}
	}
}

func TestWarmSmallIntegerFormattingDoesNotAllocate(t *testing.T) {
	if allocationInstrumentedTest() {
		t.Skip("allocation budgets run only with normal compiler/runtime instrumentation")
	}
	if allocs := testing.AllocsPerRun(1000, func() {
		if got := formatLuauNumber(-999); got != "-999" {
			t.Fatalf("formatLuauNumber(-999) = %q", got)
		}
	}); allocs != 0 {
		t.Fatalf("warm format allocation count = %.2f, want zero", allocs)
	}
	warmBuffer := make([]byte, 0, 8)
	if allocs := testing.AllocsPerRun(1000, func() {
		buffer := appendLuauNumber(warmBuffer[:0], -999)
		if string(buffer) != "-999" {
			t.Fatalf("appendLuauNumber(-999) = %q", buffer)
		}
	}); allocs != 0 {
		t.Fatalf("warm append allocation count = %.2f, want zero", allocs)
	}
}
