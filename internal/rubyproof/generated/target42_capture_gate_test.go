package generated

import (
	"crypto/sha256"
	"debug/buildinfo"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	target42IdentityPrefix  = "TARGET42_IDENTITY\t"
	target42CaptureAPathEnv = "EMBER_RUBY_TARGET42_CAPTURE_A_PATH"
	target42CaptureBPathEnv = "EMBER_RUBY_TARGET42_CAPTURE_B_PATH"
	target42CaptureMaxBytes = 2 << 20
	target42CaptureTime     = "500ms"
	target42MinTimedNS      = 375_000_000
	target42MaxTimedNS      = 2_000_000_000
	target42GoCeiling       = 1.15
)

var target42IdentityFiles = []struct {
	key, path string
}{
	{key: "generated", path: "ruby_generated.go"},
	{key: "benchmark", path: "target42_benchmark_test.go"},
	{key: "equivalent", path: "target42_equivalent_test.go"},
	{key: "gate", path: "target42_capture_gate_test.go"},
}

func target42CaptureIdentity(capture string) (string, error) {
	if capture != "A" && capture != "B" {
		return "", fmt.Errorf("target42: invalid capture identity %q", capture)
	}
	if runtime.GOMAXPROCS(0) != 1 {
		return "", fmt.Errorf("target42: identity requires GOMAXPROCS=1, got %d", runtime.GOMAXPROCS(0))
	}
	benchtime := flag.Lookup("test.benchtime")
	if benchtime == nil || benchtime.Value.String() != target42CaptureTime {
		got := "<missing>"
		if benchtime != nil {
			got = benchtime.Value.String()
		}
		return "", fmt.Errorf("target42: identity requires -benchtime=%s, got %q", target42CaptureTime, got)
	}
	if info, err := os.Lstat("default.pgo"); err == nil {
		return "", fmt.Errorf("target42: default.pgo exists with mode %s; capture requires explicit PGO-off input", info.Mode())
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("target42: inspect default.pgo: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("target42: locate benchmark executable: %w", err)
	}
	info, err := buildinfo.ReadFile(executable)
	if err != nil {
		return "", fmt.Errorf("target42: read benchmark build identity: %w", err)
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		if _, exists := settings[setting.Key]; exists {
			return "", fmt.Errorf("target42: duplicate build setting %q", setting.Key)
		}
		settings[setting.Key] = setting.Value
	}
	if settings["CGO_ENABLED"] != "0" {
		return "", fmt.Errorf("target42: capture requires CGO_ENABLED=0, got %q", settings["CGO_ENABLED"])
	}
	if os.Getenv("GOFLAGS") != "-pgo=off" {
		return "", fmt.Errorf("target42: capture requires exact GOFLAGS=-pgo=off, got %q", os.Getenv("GOFLAGS"))
	}
	if settings["GOOS"] != runtime.GOOS || settings["GOARCH"] != runtime.GOARCH {
		return "", fmt.Errorf("target42: build/runtime target mismatch %s/%s versus %s/%s", settings["GOOS"], settings["GOARCH"], runtime.GOOS, runtime.GOARCH)
	}
	goarm64, goamd64 := settings["GOARM64"], settings["GOAMD64"]
	if goarm64 == "" {
		goarm64 = "-"
	}
	if goamd64 == "" {
		goamd64 = "-"
	}
	fields := []string{
		"schema=3",
		"capture=" + capture,
		"benchtime=" + target42CaptureTime,
		"go=" + runtime.Version(),
		"goos=" + runtime.GOOS,
		"goarch=" + runtime.GOARCH,
		"goarm64=" + goarm64,
		"goamd64=" + goamd64,
		"gomaxprocs=1",
		"cgo=0",
		"pgo=off",
	}
	executableSHA, err := target42FileSHA256(executable, 64<<20)
	if err != nil {
		return "", fmt.Errorf("target42: hash benchmark executable: %w", err)
	}
	fields = append(fields, "binary="+executableSHA)
	for _, file := range target42IdentityFiles {
		digest, err := target42FileSHA256(file.path, target42CaptureMaxBytes)
		if err != nil {
			return "", fmt.Errorf("target42: hash %s identity: %w", file.key, err)
		}
		fields = append(fields, file.key+"="+digest)
	}
	return target42IdentityPrefix + strings.Join(fields, "\t"), nil
}

func target42FileSHA256(path string, limit int64) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return "", fmt.Errorf("invalid size %d", info.Size())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(content)
	return fmt.Sprintf("%x", digest), nil
}

type target42CaptureRow struct {
	iterations int64
	ns         float64
}

type target42ParsedCapture struct {
	identity map[string]string
	rows     map[string]target42CaptureRow
	sequence []string
	cpu      string
	rawSHA   string
}

func TestTarget42CaptureOrdersAreCompleteAndDistinct(t *testing.T) {
	wantA := [2][2]target42CaptureLane{
		{target42CaptureCurrent, target42CaptureEquivalent},
		{target42CaptureEquivalent, target42CaptureCurrent},
	}
	wantB := [2][2]target42CaptureLane{
		{target42CaptureEquivalent, target42CaptureCurrent},
		{target42CaptureCurrent, target42CaptureEquivalent},
	}
	for round := range target42CaptureAOrders {
		if target42CaptureAOrders[round] != wantA[round%len(wantA)] || target42CaptureBOrders[round] != wantB[round%len(wantB)] {
			t.Fatalf("round %d A/B orders = %v/%v, want %v/%v", round+1, target42CaptureAOrders[round], target42CaptureBOrders[round], wantA[round%len(wantA)], wantB[round%len(wantB)])
		}
	}
	sequences := make(map[string][]string, 2)
	for _, capture := range []string{"A", "B"} {
		sequence := target42ExpectedCaptureSequence(capture)
		if len(sequence) != 40 || len(target42ExpectedCaptureContainers(capture)) != 62 {
			t.Fatalf("capture %s sequence/containers = %d/%d, want 40/62", capture, len(sequence), len(target42ExpectedCaptureContainers(capture)))
		}
		for round := 1; round <= target42CaptureRounds; round++ {
			seen := make(map[string]bool, 2)
			prefix := fmt.Sprintf("ReceiverAfterDefinition/Round%02d/", round)
			for _, coordinate := range sequence[(round-1)*2 : round*2] {
				lane, ok := strings.CutPrefix(coordinate, prefix)
				if !ok || (lane != "Current" && lane != "Equivalent") || seen[lane] {
					t.Fatalf("capture %s round %d has invalid coordinate %q", capture, round, coordinate)
				}
				seen[lane] = true
			}
			if len(seen) != 2 {
				t.Fatalf("capture %s round %d lanes = %v", capture, round, seen)
			}
		}
		sequences[capture] = sequence
	}
	if strings.Join(sequences["A"], "\n") == strings.Join(sequences["B"], "\n") {
		t.Fatal("capture A and B use the same twenty-round paired order")
	}
}

func TestTarget42CaptureStatisticUsesFifteenthOfTwenty(t *testing.T) {
	samples := []float64{20, 1, 19, 2, 18, 3, 17, 4, 16, 5, 15, 6, 14, 7, 13, 8, 12, 9, 11, 10}
	median, upper := target42MedianAndUpper(samples)
	if median != 10.5 || upper != 15 {
		t.Fatalf("median/15th ordered sample = %.1f/%.1f, want 10.5/15", median, upper)
	}
}

func TestTarget42CaptureParserFailsClosed(t *testing.T) {
	digest := strings.Repeat("a", 64)
	identity := target42IdentityPrefix + strings.Join([]string{
		"schema=3", "capture=A", "benchtime=500ms", "go=go-test", "goos=test", "goarch=test",
		"goarm64=-", "goamd64=-", "gomaxprocs=1", "cgo=0", "pgo=off",
		"binary=" + digest, "generated=" + digest, "benchmark=" + digest,
		"equivalent=" + digest, "gate=" + digest,
	}, "\t")
	var raw strings.Builder
	fmt.Fprintln(&raw, "goos: test")
	fmt.Fprintln(&raw, "goarch: test")
	fmt.Fprintln(&raw, "pkg: github.com/besmpl/ember/internal/rubyproof/generated")
	fmt.Fprintln(&raw, "cpu: test")
	fmt.Fprintln(&raw, "BenchmarkTarget42CaptureA")
	fmt.Fprintln(&raw, identity)
	var firstRow, secondRow string
	lastMode, lastRound := "", ""
	for _, coordinate := range target42ExpectedCaptureSequence("A") {
		parts := strings.Split(coordinate, "/")
		if parts[0] != lastMode {
			fmt.Fprintf(&raw, "BenchmarkTarget42CaptureA/%s\n", parts[0])
			lastMode, lastRound = parts[0], ""
		}
		if parts[1] != lastRound {
			fmt.Fprintf(&raw, "BenchmarkTarget42CaptureA/%s/%s\n", parts[0], parts[1])
			lastRound = parts[1]
		}
		fmt.Fprintf(&raw, "BenchmarkTarget42CaptureA/%s\n", coordinate)
		row := fmt.Sprintf("BenchmarkTarget42CaptureA/%s 1000000 1000 ns/op 0 B/op 0 allocs/op\n", coordinate)
		if firstRow == "" {
			firstRow = row
		} else if secondRow == "" {
			secondRow = row
		}
		raw.WriteString(row)
	}
	fmt.Fprintln(&raw, "PASS")
	fmt.Fprintln(&raw, "ok  \tgithub.com/besmpl/ember/internal/rubyproof/generated\t1.000s")
	want := raw.String()
	reordered := strings.Replace(want, firstRow, "TARGET42_FIRST_ROW\n", 1)
	reordered = strings.Replace(reordered, secondRow, firstRow, 1)
	reordered = strings.Replace(reordered, "TARGET42_FIRST_ROW\n", secondRow, 1)
	writeAndParse := func(t *testing.T, content string) error {
		t.Helper()
		path := t.TempDir() + "/capture.txt"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := target42ParseCapture(path, "A")
		return err
	}
	if err := writeAndParse(t, want); err != nil {
		t.Fatalf("valid synthetic capture: %v", err)
	}
	for name, content := range map[string]string{
		"duplicate row": strings.Replace(want, "PASS\n", firstRow+"PASS\n", 1),
		"missing row":   strings.Replace(want, firstRow, "", 1),
		"allocation":    strings.Replace(want, "0 B/op", "8 B/op", 1),
		"short timing":  strings.Replace(want, "1000000 1000 ns/op", "1000 1000 ns/op", 1),
		"NaN timing":    strings.Replace(want, "1000 ns/op", "NaN ns/op", 1),
		"Inf timing":    strings.Replace(want, "1000 ns/op", "+Inf ns/op", 1),
		"reordered":     reordered,
		"second marker": strings.Replace(want, "PASS\n", identity+"\nPASS\n", 1),
		"trailing data": want + "junk\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := writeAndParse(t, content); err == nil {
				t.Fatal("malformed synthetic capture passed")
			}
		})
	}
}

func TestOptInTarget42CaptureGate(t *testing.T) {
	aPath, bPath := os.Getenv(target42CaptureAPathEnv), os.Getenv(target42CaptureBPathEnv)
	if aPath == "" && bPath == "" {
		t.Skipf("set %s and %s to compare Target 42 captures", target42CaptureAPathEnv, target42CaptureBPathEnv)
	}
	if aPath == "" || bPath == "" || aPath == bPath {
		t.Fatalf("Target 42 gate requires two distinct capture paths")
	}
	// The generic lane is a separate diagnostic only. Keep its semantic shape
	// proved exact even though retained captures time only the blocking pair.
	target42ValidateGenericEquivalence(t)
	captures := make(map[string]target42ParsedCapture, 2)
	for _, item := range []struct {
		name, path string
	}{{name: "A", path: aPath}, {name: "B", path: bPath}} {
		capture, err := target42ParseCapture(item.path, item.name)
		if err != nil {
			t.Fatalf("capture %s: %v", item.name, err)
		}
		wantIdentity, err := target42CaptureIdentity(item.name)
		if err != nil {
			t.Fatal(err)
		}
		want, err := target42ParseIdentity(wantIdentity)
		if err != nil {
			t.Fatal(err)
		}
		if !target42SameIdentity(capture.identity, want) {
			t.Fatalf("capture %s identity does not match the current comparison binary and source", item.name)
		}
		captures[item.name] = capture
	}
	identityA, identityB := captures["A"].identity, captures["B"].identity
	if captures["A"].cpu != captures["B"].cpu {
		t.Fatalf("capture CPU identities disagree: %q versus %q", captures["A"].cpu, captures["B"].cpu)
	}
	t.Logf("capture CPU: %s", captures["A"].cpu)
	for key, left := range identityA {
		if key == "capture" {
			continue
		}
		if identityB[key] != left {
			t.Fatalf("capture identities disagree at %s: %q versus %q", key, left, identityB[key])
		}
	}
	for _, captureName := range []string{"A", "B"} {
		capture := captures[captureName]
		equivalentRatios := make([]float64, 0, target42CaptureRounds)
		currentNS := make([]float64, 0, target42CaptureRounds)
		equivalentNS := make([]float64, 0, target42CaptureRounds)
		for round := 1; round <= target42CaptureRounds; round++ {
			prefix := fmt.Sprintf("ReceiverAfterDefinition/Round%02d/", round)
			current := capture.rows[prefix+"Current"].ns
			equivalent := capture.rows[prefix+"Equivalent"].ns
			equivalentRatios = append(equivalentRatios, current/equivalent)
			currentNS = append(currentNS, current)
			equivalentNS = append(equivalentNS, equivalent)
		}
		equivalentMedian, equivalentUpper := target42MedianAndUpper(equivalentRatios)
		// For 20 independent paired rounds, the 15th ordered ratio is a
		// dependency-free one-sided nonparametric >95% upper confidence bound
		// for the population median.
		currentNSMedian, _ := target42MedianAndUpper(currentNS)
		equivalentNSMedian, _ := target42MedianAndUpper(equivalentNS)
		currentNSPerCall := currentNSMedian / 8
		equivalentNSPerCall := equivalentNSMedian / 8
		if math.IsNaN(equivalentMedian) || math.IsInf(equivalentMedian, 0) || equivalentMedian <= 0 ||
			math.IsNaN(equivalentUpper) || math.IsInf(equivalentUpper, 0) || equivalentUpper <= 0 ||
			math.IsNaN(currentNSPerCall) || math.IsInf(currentNSPerCall, 0) || currentNSPerCall <= 0 ||
			math.IsNaN(equivalentNSPerCall) || math.IsInf(equivalentNSPerCall, 0) || equivalentNSPerCall <= 0 {
			t.Fatalf("capture %s produced non-finite or non-positive medians", captureName)
		}
		t.Logf("capture %s receiver-after-definition: current/equivalent-Go paired-ratio median %.6f; 15th-of-20 upper bound %.6f", captureName, equivalentMedian, equivalentUpper)
		t.Logf("capture %s median ns/call: Current %.3f; Equivalent-Go %.3f", captureName, currentNSPerCall, equivalentNSPerCall)
		if equivalentUpper > target42GoCeiling {
			t.Errorf("capture %s current/equivalent-Go 15th-of-20 upper bound %.6f exceeds %.2f", captureName, equivalentUpper, target42GoCeiling)
		}
		t.Logf("capture %s raw SHA-256 %s", captureName, capture.rawSHA)
	}
}

func target42MedianAndUpper(samples []float64) (median, upper float64) {
	if len(samples) != target42CaptureRounds {
		panic("target42: statistic requires twenty samples")
	}
	sort.Float64s(samples)
	return (samples[9] + samples[10]) / 2, samples[14]
}

func target42ParseCapture(path, captureName string) (target42ParsedCapture, error) {
	info, err := os.Stat(path)
	if err != nil {
		return target42ParsedCapture{}, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > target42CaptureMaxBytes {
		return target42ParsedCapture{}, fmt.Errorf("invalid raw capture size %d", info.Size())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return target42ParsedCapture{}, err
	}
	if !utf8.Valid(content) || content[len(content)-1] != '\n' || strings.ContainsRune(string(content), '\x00') {
		return target42ParsedCapture{}, fmt.Errorf("raw capture is not canonical newline-terminated UTF-8")
	}
	digest := sha256.Sum256(content)
	parsed := target42ParsedCapture{rows: make(map[string]target42CaptureRow), rawSHA: fmt.Sprintf("%x", digest)}
	markerCount, passCount, okCount := 0, 0, 0
	headerValues := make(map[string]string, 4)
	containerCounts := make(map[string]int, 62)
	prefix := "BenchmarkTarget42Capture" + captureName
	for lineNumber, line := range strings.Split(strings.TrimSuffix(string(content), "\n"), "\n") {
		if index := strings.Index(line, target42IdentityPrefix); index >= 0 {
			if strings.Count(line, target42IdentityPrefix) != 1 {
				return target42ParsedCapture{}, fmt.Errorf("line %d contains multiple identity markers", lineNumber+1)
			}
			logPrefix := strings.TrimSpace(line[:index])
			if logPrefix != "" {
				lineNumberText, ok := strings.CutPrefix(logPrefix, "target42_benchmark_test.go:")
				if !ok || !strings.HasSuffix(lineNumberText, ":") {
					return target42ParsedCapture{}, fmt.Errorf("line %d has an invalid identity log prefix", lineNumber+1)
				}
				loggedLine, err := strconv.Atoi(strings.TrimSuffix(lineNumberText, ":"))
				if err != nil || loggedLine < 1 {
					return target42ParsedCapture{}, fmt.Errorf("line %d has an invalid identity log line", lineNumber+1)
				}
			}
			markerCount++
			identity, err := target42ParseIdentity(line[index:])
			if err != nil {
				return target42ParsedCapture{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
			}
			parsed.identity = identity
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "FAIL") || strings.Contains(trimmed, "panic:") || strings.Contains(trimmed, "SKIP") {
			return target42ParsedCapture{}, fmt.Errorf("line %d contains failed or skipped work", lineNumber+1)
		}
		if trimmed == "PASS" {
			passCount++
			continue
		}
		if strings.HasPrefix(trimmed, "ok  \tgithub.com/besmpl/ember/internal/rubyproof/generated\t") {
			okCount++
			continue
		}
		header := ""
		for _, candidate := range []string{"goos: ", "goarch: ", "pkg: ", "cpu: "} {
			if strings.HasPrefix(trimmed, candidate) {
				header = candidate
				break
			}
		}
		if header != "" {
			value := strings.TrimPrefix(trimmed, header)
			if value == "" || headerValues[header] != "" {
				return target42ParsedCapture{}, fmt.Errorf("line %d has an empty or duplicate %s header", lineNumber+1, strings.TrimSpace(header))
			}
			headerValues[header] = value
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		if !strings.HasPrefix(fields[0], "Benchmark") {
			return target42ParsedCapture{}, fmt.Errorf("line %d contains unexpected output", lineNumber+1)
		}
		name := fields[0]
		if name == prefix || strings.HasPrefix(name, prefix+"/") && len(strings.Split(name, "/")) < 4 {
			if len(fields) != 1 {
				return target42ParsedCapture{}, fmt.Errorf("line %d has malformed container benchmark", lineNumber+1)
			}
			containerCounts[name]++
			continue
		}
		if !strings.HasPrefix(name, prefix+"/") {
			return target42ParsedCapture{}, fmt.Errorf("line %d contains unexpected benchmark %q", lineNumber+1, name)
		}
		parts := strings.Split(name, "/")
		if len(parts) != 4 {
			return target42ParsedCapture{}, fmt.Errorf("line %d has malformed benchmark name %q", lineNumber+1, name)
		}
		lane := strings.TrimSuffix(parts[3], "-1")
		if parts[1] != "ReceiverAfterDefinition" || len(parts[2]) != 7 || !strings.HasPrefix(parts[2], "Round") ||
			(lane != "Current" && lane != "Equivalent") {
			return target42ParsedCapture{}, fmt.Errorf("line %d has unknown benchmark coordinates %q", lineNumber+1, name)
		}
		round, err := strconv.Atoi(strings.TrimPrefix(parts[2], "Round"))
		if err != nil || round < 1 || round > target42CaptureRounds {
			return target42ParsedCapture{}, fmt.Errorf("line %d has invalid round", lineNumber+1)
		}
		if len(fields) == 1 {
			containerCounts[name]++
			continue
		}
		if len(fields) != 8 || fields[3] != "ns/op" || fields[5] != "B/op" || fields[7] != "allocs/op" {
			return target42ParsedCapture{}, fmt.Errorf("line %d lacks the exact benchmark metrics", lineNumber+1)
		}
		iterations, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || iterations < 1 {
			return target42ParsedCapture{}, fmt.Errorf("line %d has invalid iteration count", lineNumber+1)
		}
		ns, err := strconv.ParseFloat(fields[2], 64)
		if err != nil || math.IsNaN(ns) || math.IsInf(ns, 0) || ns <= 0 {
			return target42ParsedCapture{}, fmt.Errorf("line %d has invalid ns/op", lineNumber+1)
		}
		bytesPerOp, bytesErr := strconv.ParseFloat(fields[4], 64)
		allocsPerOp, allocsErr := strconv.ParseFloat(fields[6], 64)
		if bytesErr != nil || allocsErr != nil || bytesPerOp != 0 || allocsPerOp != 0 {
			return target42ParsedCapture{}, fmt.Errorf("line %d violates the zero-allocation gate", lineNumber+1)
		}
		timedNS := float64(iterations) * ns
		if timedNS < target42MinTimedNS || timedNS > target42MaxTimedNS {
			return target42ParsedCapture{}, fmt.Errorf("line %d timed %.0fns outside the %s capture envelope", lineNumber+1, timedNS, target42CaptureTime)
		}
		key := fmt.Sprintf("%s/Round%02d/%s", parts[1], round, lane)
		if _, exists := parsed.rows[key]; exists {
			return target42ParsedCapture{}, fmt.Errorf("line %d duplicates %s", lineNumber+1, key)
		}
		parsed.rows[key] = target42CaptureRow{iterations: iterations, ns: ns}
		parsed.sequence = append(parsed.sequence, key)
	}
	if markerCount != 1 || parsed.identity == nil || parsed.identity["capture"] != captureName {
		return target42ParsedCapture{}, fmt.Errorf("expected one authenticated %s identity marker, got %d", captureName, markerCount)
	}
	if headerValues["goos: "] != parsed.identity["goos"] || headerValues["goarch: "] != parsed.identity["goarch"] ||
		headerValues["pkg: "] != "github.com/besmpl/ember/internal/rubyproof/generated" || headerValues["cpu: "] == "" {
		return target42ParsedCapture{}, fmt.Errorf("capture headers disagree with the authenticated identity")
	}
	parsed.cpu = headerValues["cpu: "]
	expectedSequence := target42ExpectedCaptureSequence(captureName)
	if len(parsed.sequence) != len(expectedSequence) {
		return target42ParsedCapture{}, fmt.Errorf("metric sequence has %d rows, want %d", len(parsed.sequence), len(expectedSequence))
	}
	for index := range expectedSequence {
		if parsed.sequence[index] != expectedSequence[index] {
			return target42ParsedCapture{}, fmt.Errorf("metric row %d is %s, want %s", index+1, parsed.sequence[index], expectedSequence[index])
		}
	}
	expectedContainers := target42ExpectedCaptureContainers(captureName)
	if len(containerCounts) != len(expectedContainers) {
		return target42ParsedCapture{}, fmt.Errorf("container inventory has %d names, want %d", len(containerCounts), len(expectedContainers))
	}
	for _, name := range expectedContainers {
		if containerCounts[name] != 1 {
			return target42ParsedCapture{}, fmt.Errorf("container %s occurs %d times", name, containerCounts[name])
		}
	}
	if len(parsed.rows) != 40 || passCount != 1 || okCount != 1 {
		return target42ParsedCapture{}, fmt.Errorf("incomplete capture: rows=%d PASS=%d ok=%d", len(parsed.rows), passCount, okCount)
	}
	return parsed, nil
}

func target42ExpectedCaptureSequence(captureName string) []string {
	orders := target42CaptureAOrders
	if captureName == "B" {
		orders = target42CaptureBOrders
	}
	sequence := make([]string, 0, 40)
	for round, order := range orders {
		for _, lane := range order {
			name := ""
			switch lane {
			case target42CaptureCurrent:
				name = "Current"
			case target42CaptureEquivalent:
				name = "Equivalent"
			default:
				panic("target42: invalid capture order")
			}
			sequence = append(sequence, fmt.Sprintf("ReceiverAfterDefinition/Round%02d/%s", round+1, name))
		}
	}
	return sequence
}

func target42ExpectedCaptureContainers(captureName string) []string {
	prefix := "BenchmarkTarget42Capture" + captureName
	containers := []string{prefix, prefix + "/ReceiverAfterDefinition"}
	for round := 1; round <= target42CaptureRounds; round++ {
		containers = append(containers, fmt.Sprintf("%s/ReceiverAfterDefinition/Round%02d", prefix, round))
	}
	for _, coordinate := range target42ExpectedCaptureSequence(captureName) {
		containers = append(containers, prefix+"/"+coordinate)
	}
	return containers
}

func target42ParseIdentity(line string) (map[string]string, error) {
	if !strings.HasPrefix(line, target42IdentityPrefix) {
		return nil, fmt.Errorf("invalid identity prefix")
	}
	fields := strings.Split(strings.TrimPrefix(line, target42IdentityPrefix), "\t")
	identity := make(map[string]string, len(fields))
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" || value == "" || identity[key] != "" {
			return nil, fmt.Errorf("malformed identity field %q", field)
		}
		identity[key] = value
	}
	required := []string{"schema", "capture", "benchtime", "go", "goos", "goarch", "goarm64", "goamd64", "gomaxprocs", "cgo", "pgo", "binary", "generated", "benchmark", "equivalent", "gate"}
	if len(identity) != len(required) {
		return nil, fmt.Errorf("identity has %d fields, want %d", len(identity), len(required))
	}
	for _, key := range required {
		if _, ok := identity[key]; !ok {
			return nil, fmt.Errorf("identity is missing %s", key)
		}
	}
	if identity["schema"] != "3" || (identity["capture"] != "A" && identity["capture"] != "B") || identity["benchtime"] != target42CaptureTime || identity["gomaxprocs"] != "1" || identity["cgo"] != "0" || identity["pgo"] != "off" {
		return nil, fmt.Errorf("identity policy fields are invalid")
	}
	for _, key := range []string{"binary", "generated", "benchmark", "equivalent", "gate"} {
		if len(identity[key]) != 64 {
			return nil, fmt.Errorf("identity digest %s has invalid length", key)
		}
		for _, character := range identity[key] {
			if !strings.ContainsRune("0123456789abcdef", character) {
				return nil, fmt.Errorf("identity digest %s is not lowercase hexadecimal", key)
			}
		}
	}
	return identity, nil
}

func target42SameIdentity(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
