package h1bgenerated

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	target48ProcessOutputMaxBytes = 64 << 10
	target48PackagePath           = "github.com/besmpl/ember/internal/rubyproof/h1bgenerated"
)

type target48Family uint8

const (
	target48HitFamily target48Family = iota + 1
	target48CollectorFamily
)

type target48Lane uint8

const (
	target48CurrentLane target48Lane = iota + 1
	target48EquivalentLane
)

type target48Phase uint8

const (
	target48WarmupPhase target48Phase = iota + 1
	target48PrimaryPhase
	target48ReservePhase
)

type target48ProcessExpectation struct {
	goos, goarch, cpu string
	family            target48Family
	lane              target48Lane
	phase             target48Phase
}

type target48Decimal struct {
	text       string
	value      *big.Rat
	resolution *big.Rat
}

type target48ParsedProcess struct {
	benchmark  string
	iterations uint64
	metrics    map[string]target48Decimal
}

func target48BenchmarkName(family target48Family, lane target48Lane) (string, error) {
	prefix := "BenchmarkTarget48H1b"
	switch family {
	case target48HitFamily:
		prefix += "BalancedHit"
	case target48CollectorFamily:
		prefix += "Collector"
	default:
		return "", fmt.Errorf("target48: invalid family %d", family)
	}
	switch lane {
	case target48CurrentLane:
		return prefix + "Current", nil
	case target48EquivalentLane:
		return prefix + "Equivalent", nil
	default:
		return "", fmt.Errorf("target48: invalid lane %d", lane)
	}
}

func target48ParseUnsignedDecimal(text string) (target48Decimal, error) {
	if text == "" {
		return target48Decimal{}, errors.New("target48: empty decimal")
	}
	dot := -1
	for index, char := range []byte(text) {
		switch {
		case char >= '0' && char <= '9':
		case char == '.' && dot < 0:
			dot = index
		default:
			return target48Decimal{}, fmt.Errorf("target48: invalid decimal %q", text)
		}
	}
	if text[0] == '.' || text[len(text)-1] == '.' || (len(text) > 1 && text[0] == '0' && dot != 1) {
		return target48Decimal{}, fmt.Errorf("target48: noncanonical decimal %q", text)
	}
	if dot < 0 && len(text) > 1 && text[0] == '0' {
		return target48Decimal{}, fmt.Errorf("target48: noncanonical decimal %q", text)
	}
	value := new(big.Rat)
	if _, ok := value.SetString(text); !ok {
		return target48Decimal{}, fmt.Errorf("target48: parse decimal %q", text)
	}
	scale := 0
	if dot >= 0 {
		scale = len(text) - dot - 1
	}
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	return target48Decimal{text: text, value: value, resolution: new(big.Rat).SetFrac(big.NewInt(1), denominator)}, nil
}

func target48ParseProcessOutput(raw []byte, expected target48ProcessExpectation) (target48ParsedProcess, error) {
	if len(raw) == 0 || len(raw) > target48ProcessOutputMaxBytes {
		return target48ParsedProcess{}, fmt.Errorf("target48: process stdout size %d outside 1..%d", len(raw), target48ProcessOutputMaxBytes)
	}
	if !utf8.Valid(raw) || raw[len(raw)-1] != '\n' || bytes.ContainsAny(raw, "\x00\r") {
		return target48ParsedProcess{}, errors.New("target48: stdout must be UTF-8 LF-only text with terminal LF")
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 6 {
		return target48ParsedProcess{}, fmt.Errorf("target48: stdout has %d lines, want 6", len(lines))
	}
	for _, line := range lines {
		if line == "" {
			return target48ParsedProcess{}, errors.New("target48: stdout contains blank line")
		}
	}
	headers := []string{
		"goos: " + expected.goos,
		"goarch: " + expected.goarch,
		"pkg: " + target48PackagePath,
		"cpu: " + expected.cpu,
	}
	for index, header := range headers {
		if lines[index] != header {
			return target48ParsedProcess{}, fmt.Errorf("target48: header %d = %q, want %q", index+1, lines[index], header)
		}
	}
	if lines[5] != "PASS" {
		return target48ParsedProcess{}, fmt.Errorf("target48: terminal line = %q, want PASS", lines[5])
	}
	wantBenchmark, err := target48BenchmarkName(expected.family, expected.lane)
	if err != nil {
		return target48ParsedProcess{}, err
	}
	fields := strings.Fields(lines[4])
	if len(fields) < 4 || fields[0] != wantBenchmark || len(fields[1:])%2 == 0 {
		return target48ParsedProcess{}, fmt.Errorf("target48: malformed benchmark row")
	}
	iterations, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil || iterations == 0 {
		return target48ParsedProcess{}, fmt.Errorf("target48: invalid iteration count %q", fields[1])
	}
	metrics := make(map[string]target48Decimal, (len(fields)-2)/2)
	units := make([]string, 0, (len(fields)-2)/2)
	for index := 2; index < len(fields); index += 2 {
		decimal, parseErr := target48ParseUnsignedDecimal(fields[index])
		if parseErr != nil {
			return target48ParsedProcess{}, parseErr
		}
		unit := fields[index+1]
		if _, duplicate := metrics[unit]; duplicate {
			return target48ParsedProcess{}, fmt.Errorf("target48: duplicate metric %q", unit)
		}
		metrics[unit] = decimal
		units = append(units, unit)
	}
	wantUnits := target48HitMetricUnits
	if expected.family == target48CollectorFamily {
		wantUnits = target48CollectorMetricUnits
	}
	if strings.Join(units, "\x00") != strings.Join(wantUnits, "\x00") {
		return target48ParsedProcess{}, fmt.Errorf("target48: metric units = %v, want %v", units, wantUnits)
	}
	parsed := target48ParsedProcess{benchmark: wantBenchmark, iterations: iterations, metrics: metrics}
	if err := target48ValidateProcessMetrics(parsed, expected); err != nil {
		return target48ParsedProcess{}, err
	}
	return parsed, nil
}

var target48HitMetricUnits = []string{
	"ns/op", "calls/op", "ns/semantic-call", "singleton-calls/op", "B/op", "allocs/op",
}

var target48CollectorMetricUnits = []string{
	"ns/op", "arenas/batch", "bank-bytes", "collections/op", "duplicate-marks/op",
	"edges-examined/op", "free-list-state", "invalid-stale-rejections/op", "mark-high-water",
	"ns/collection-cycle", "reclaimed-slot-bytes/op", "roots-examined/op", "slots-reclaimed/op",
	"slots-swept/op", "successful-marks/op", "B/op", "allocs/op",
}

func target48ValidateProcessMetrics(parsed target48ParsedProcess, expected target48ProcessExpectation) error {
	require := func(unit string, numerator int64) error {
		want := new(big.Rat).SetInt64(numerator)
		if parsed.metrics[unit].value.Cmp(want) != 0 {
			return fmt.Errorf("target48: %s = %s, want %d", unit, parsed.metrics[unit].text, numerator)
		}
		return nil
	}
	for _, unit := range []string{"B/op", "allocs/op"} {
		if err := require(unit, 0); err != nil {
			return err
		}
	}
	comparisonUnit, divisor := "ns/semantic-call", int64(64)
	if expected.family == target48HitFamily {
		for unit, value := range map[string]int64{"calls/op": 64, "singleton-calls/op": 32} {
			if err := require(unit, value); err != nil {
				return err
			}
		}
	} else {
		comparisonUnit, divisor = "ns/collection-cycle", 1
		for unit, value := range map[string]int64{
			"arenas/batch": 65536, "bank-bytes": 29360128, "collections/op": 3,
			"duplicate-marks/op": 0, "edges-examined/op": 1, "free-list-state": 257,
			"invalid-stale-rejections/op": 0, "mark-high-water": 3, "reclaimed-slot-bytes/op": 72,
			"roots-examined/op": 4, "slots-reclaimed/op": 3, "slots-swept/op": 18,
			"successful-marks/op": 4,
		} {
			if err := require(unit, value); err != nil {
				return err
			}
		}
	}
	if !target48RoundedIntervalsOverlap(parsed.metrics["ns/op"], divisor, parsed.metrics[comparisonUnit]) {
		return fmt.Errorf("target48: %s is inconsistent with ns/op/%d", comparisonUnit, divisor)
	}

	duration := new(big.Rat).Mul(parsed.metrics["ns/op"].value, new(big.Rat).SetInt(new(big.Int).SetUint64(parsed.iterations)))
	minimum, maximum := int64(1_500_000_000), int64(8_000_000_000)
	if expected.phase == target48WarmupPhase {
		minimum, maximum = 375_000_000, 2_000_000_000
	} else if expected.phase != target48PrimaryPhase && expected.phase != target48ReservePhase {
		return fmt.Errorf("target48: invalid phase %d", expected.phase)
	}
	if duration.Cmp(new(big.Rat).SetInt64(minimum)) < 0 || duration.Cmp(new(big.Rat).SetInt64(maximum)) > 0 {
		return fmt.Errorf("target48: displayed timed duration %s ns outside [%d,%d]", duration.RatString(), minimum, maximum)
	}
	return nil
}

func target48RoundedIntervalsOverlap(total target48Decimal, divisor int64, reported target48Decimal) bool {
	half := func(decimal target48Decimal) *big.Rat {
		return new(big.Rat).Quo(decimal.resolution, big.NewRat(2, 1))
	}
	totalLow := new(big.Rat).Sub(total.value, half(total))
	totalHigh := new(big.Rat).Add(total.value, half(total))
	totalLow.Quo(totalLow, big.NewRat(divisor, 1))
	totalHigh.Quo(totalHigh, big.NewRat(divisor, 1))
	reportedLow := new(big.Rat).Sub(reported.value, half(reported))
	reportedHigh := new(big.Rat).Add(reported.value, half(reported))
	return totalLow.Cmp(reportedHigh) <= 0 && reportedLow.Cmp(totalHigh) <= 0
}

func target48SyntheticOutput(row string) []byte {
	return []byte("goos: linux\ngoarch: amd64\npkg: " + target48PackagePath + "\ncpu: pinned-test-core\n" + row + "\nPASS\n")
}

func TestTarget48ProcessParserAcceptsExactRows(t *testing.T) {
	tests := []struct {
		name     string
		expected target48ProcessExpectation
		row      string
	}{
		{
			name:     "hit warmup",
			expected: target48ProcessExpectation{goos: "linux", goarch: "amd64", cpu: "pinned-test-core", family: target48HitFamily, lane: target48CurrentLane, phase: target48WarmupPhase},
			row:      "BenchmarkTarget48H1bBalancedHitCurrent 1000000 500 ns/op 64.00 calls/op 7.8125 ns/semantic-call 32.00 singleton-calls/op 0 B/op 0 allocs/op",
		},
		{
			name:     "collector primary",
			expected: target48ProcessExpectation{goos: "linux", goarch: "amd64", cpu: "pinned-test-core", family: target48CollectorFamily, lane: target48EquivalentLane, phase: target48PrimaryPhase},
			row:      "BenchmarkTarget48H1bCollectorEquivalent 1000000 2000 ns/op 65536 arenas/batch 29360128 bank-bytes 3.000 collections/op 0 duplicate-marks/op 1.000 edges-examined/op 257.0 free-list-state 0 invalid-stale-rejections/op 3.000 mark-high-water 2000 ns/collection-cycle 72.00 reclaimed-slot-bytes/op 4.000 roots-examined/op 3.000 slots-reclaimed/op 18.00 slots-swept/op 4.000 successful-marks/op 0 B/op 0 allocs/op",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := target48ParseProcessOutput(target48SyntheticOutput(test.row), test.expected)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.iterations != 1_000_000 {
				t.Fatalf("iterations = %d", parsed.iterations)
			}
		})
	}
}

func TestTarget48ProcessParserFailsClosed(t *testing.T) {
	expected := target48ProcessExpectation{goos: "linux", goarch: "amd64", cpu: "pinned-test-core", family: target48HitFamily, lane: target48CurrentLane, phase: target48WarmupPhase}
	row := "BenchmarkTarget48H1bBalancedHitCurrent 1000000 500 ns/op 64.00 calls/op 7.8125 ns/semantic-call 32.00 singleton-calls/op 0 B/op 0 allocs/op"
	valid := target48SyntheticOutput(row)
	mutations := []struct {
		name string
		raw  []byte
	}{
		{"missing terminal LF", bytes.TrimSuffix(valid, []byte("\n"))},
		{"CRLF", bytes.ReplaceAll(valid, []byte("\n"), []byte("\r\n"))},
		{"extra line", bytes.Replace(valid, []byte("PASS\n"), []byte("PASS\nextra\n"), 1)},
		{"wrong benchmark", bytes.Replace(valid, []byte("BalancedHitCurrent"), []byte("BalancedHitEquivalent"), 1)},
		{"exponent decimal", bytes.Replace(valid, []byte("500 ns/op"), []byte("5e2 ns/op"), 1)},
		{"nonzero allocation", bytes.Replace(valid, []byte("0 B/op"), []byte("1 B/op"), 1)},
		{"inconsistent semantic metric", bytes.Replace(valid, []byte("7.8125 ns/semantic-call"), []byte("8.0 ns/semantic-call"), 1)},
		{"short duration", bytes.Replace(valid, []byte("1000000 500 ns/op"), []byte("1 500 ns/op"), 1)},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			if _, err := target48ParseProcessOutput(mutation.raw, expected); err == nil {
				t.Fatal("malformed process output accepted")
			}
		})
	}
}
