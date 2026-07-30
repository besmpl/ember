package generated

import (
	"crypto/sha256"
	"debug/buildinfo"
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
	target37IdentityPrefix  = "TARGET37_IDENTITY\t"
	target37CaptureAPathEnv = "EMBER_RUBY_TARGET37_CAPTURE_A_PATH"
	target37CaptureBPathEnv = "EMBER_RUBY_TARGET37_CAPTURE_B_PATH"
	target37CaptureMaxBytes = 2 << 20
	target37MinTimedNS      = 750_000_000
	target37MaxTimedNS      = 3_000_000_000
)

var target37IdentityFiles = []struct {
	key, path string
}{
	{key: "generated", path: "ruby_generated.go"},
	{key: "benchmark", path: "target37_benchmark_test.go"},
	{key: "equivalent", path: "target37_equivalent_test.go"},
	{key: "gate", path: "target37_capture_gate_test.go"},
}

func target37CaptureIdentity(capture string) (string, error) {
	if capture != "A" && capture != "B" {
		return "", fmt.Errorf("target37: invalid capture identity %q", capture)
	}
	if runtime.GOMAXPROCS(0) != 1 {
		return "", fmt.Errorf("target37: identity requires GOMAXPROCS=1, got %d", runtime.GOMAXPROCS(0))
	}
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("target37: locate benchmark executable: %w", err)
	}
	info, err := buildinfo.ReadFile(executable)
	if err != nil {
		return "", fmt.Errorf("target37: read benchmark build identity: %w", err)
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		if _, exists := settings[setting.Key]; exists {
			return "", fmt.Errorf("target37: duplicate build setting %q", setting.Key)
		}
		settings[setting.Key] = setting.Value
	}
	if settings["CGO_ENABLED"] != "0" {
		return "", fmt.Errorf("target37: capture requires CGO_ENABLED=0, got %q", settings["CGO_ENABLED"])
	}
	if settings["GOOS"] != runtime.GOOS || settings["GOARCH"] != runtime.GOARCH {
		return "", fmt.Errorf("target37: build/runtime target mismatch %s/%s versus %s/%s", settings["GOOS"], settings["GOARCH"], runtime.GOOS, runtime.GOARCH)
	}
	goarm64, goamd64 := settings["GOARM64"], settings["GOAMD64"]
	if goarm64 == "" {
		goarm64 = "-"
	}
	if goamd64 == "" {
		goamd64 = "-"
	}
	fields := []string{
		"schema=1",
		"capture=" + capture,
		"go=" + runtime.Version(),
		"goos=" + runtime.GOOS,
		"goarch=" + runtime.GOARCH,
		"goarm64=" + goarm64,
		"goamd64=" + goamd64,
		"gomaxprocs=1",
		"cgo=0",
	}
	executableSHA, err := target37FileSHA256(executable, 64<<20)
	if err != nil {
		return "", fmt.Errorf("target37: hash benchmark executable: %w", err)
	}
	fields = append(fields, "binary="+executableSHA)
	for _, file := range target37IdentityFiles {
		digest, err := target37FileSHA256(file.path, target37CaptureMaxBytes)
		if err != nil {
			return "", fmt.Errorf("target37: hash %s identity: %w", file.key, err)
		}
		fields = append(fields, file.key+"="+digest)
	}
	return target37IdentityPrefix + strings.Join(fields, "\t"), nil
}

func target37FileSHA256(path string, limit int64) (string, error) {
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

type target37CaptureRow struct {
	iterations int64
	ns         float64
}

type target37ParsedCapture struct {
	identity map[string]string
	rows     map[string]target37CaptureRow
	sequence []string
	cpu      string
	rawSHA   string
}

func TestTarget37CaptureParserFailsClosed(t *testing.T) {
	digest := strings.Repeat("a", 64)
	identity := target37IdentityPrefix + strings.Join([]string{
		"schema=1", "capture=A", "go=go-test", "goos=test", "goarch=test",
		"goarm64=-", "goamd64=-", "gomaxprocs=1", "cgo=0",
		"binary=" + digest, "generated=" + digest, "benchmark=" + digest,
		"equivalent=" + digest, "gate=" + digest,
	}, "\t")
	var raw strings.Builder
	fmt.Fprintln(&raw, "goos: test")
	fmt.Fprintln(&raw, "goarch: test")
	fmt.Fprintln(&raw, "pkg: github.com/besmpl/ember/internal/rubyproof/generated")
	fmt.Fprintln(&raw, "cpu: test")
	fmt.Fprintln(&raw, "BenchmarkTarget37CaptureA")
	fmt.Fprintln(&raw, identity)
	var firstRow, secondRow string
	lastMode, lastRound := "", ""
	for _, coordinate := range target37ExpectedCaptureSequence("A") {
		parts := strings.Split(coordinate, "/")
		if parts[0] != lastMode {
			fmt.Fprintf(&raw, "BenchmarkTarget37CaptureA/%s\n", parts[0])
			lastMode, lastRound = parts[0], ""
		}
		if parts[1] != lastRound {
			fmt.Fprintf(&raw, "BenchmarkTarget37CaptureA/%s/%s\n", parts[0], parts[1])
			lastRound = parts[1]
		}
		fmt.Fprintf(&raw, "BenchmarkTarget37CaptureA/%s\n", coordinate)
		row := fmt.Sprintf("BenchmarkTarget37CaptureA/%s 1000000 1000 ns/op 0 B/op 0 allocs/op\n", coordinate)
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
	reordered := strings.Replace(want, firstRow, "TARGET37_FIRST_ROW\n", 1)
	reordered = strings.Replace(reordered, secondRow, firstRow, 1)
	reordered = strings.Replace(reordered, "TARGET37_FIRST_ROW\n", secondRow, 1)
	writeAndParse := func(t *testing.T, content string) error {
		t.Helper()
		path := t.TempDir() + "/capture.txt"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := target37ParseCapture(path, "A")
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

func TestOptInTarget37CaptureGate(t *testing.T) {
	aPath, bPath := os.Getenv(target37CaptureAPathEnv), os.Getenv(target37CaptureBPathEnv)
	if aPath == "" && bPath == "" {
		t.Skipf("set %s and %s to compare Target 37 captures", target37CaptureAPathEnv, target37CaptureBPathEnv)
	}
	if aPath == "" || bPath == "" || aPath == bPath {
		t.Fatalf("Target 37 gate requires two distinct capture paths")
	}
	captures := make(map[string]target37ParsedCapture, 2)
	for _, item := range []struct {
		name, path string
	}{{name: "A", path: aPath}, {name: "B", path: bPath}} {
		capture, err := target37ParseCapture(item.path, item.name)
		if err != nil {
			t.Fatalf("capture %s: %v", item.name, err)
		}
		wantIdentity, err := target37CaptureIdentity(item.name)
		if err != nil {
			t.Fatal(err)
		}
		want, err := target37ParseIdentity(wantIdentity)
		if err != nil {
			t.Fatal(err)
		}
		if !target37SameIdentity(capture.identity, want) {
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
		for _, mode := range []string{"Send", "Public"} {
			genericRatios := make([]float64, 0, 7)
			equivalentRatios := make([]float64, 0, 7)
			for round := 1; round <= 7; round++ {
				prefix := fmt.Sprintf("%s/Round%02d/", mode, round)
				generic := capture.rows[prefix+"Generic"].ns
				specialized := capture.rows[prefix+"Specialized"].ns
				equivalent := capture.rows[prefix+"Equivalent"].ns
				genericRatios = append(genericRatios, specialized/generic)
				equivalentRatios = append(equivalentRatios, specialized/equivalent)
			}
			sort.Float64s(genericRatios)
			sort.Float64s(equivalentRatios)
			genericMedian, equivalentMedian := genericRatios[3], equivalentRatios[3]
			if math.IsNaN(genericMedian) || math.IsInf(genericMedian, 0) || genericMedian <= 0 ||
				math.IsNaN(equivalentMedian) || math.IsInf(equivalentMedian, 0) || equivalentMedian <= 0 {
				t.Fatalf("capture %s %s produced non-finite or non-positive medians", captureName, mode)
			}
			t.Logf("capture %s %s: specialized/generic median %.6f; specialized/equivalent median %.6f", captureName, mode, genericMedian, equivalentMedian)
			if genericMedian > 0.95 {
				t.Errorf("capture %s %s specialized/generic median %.6f exceeds 0.95", captureName, mode, genericMedian)
			}
			if equivalentMedian > 1.15 {
				t.Errorf("capture %s %s specialized/equivalent median %.6f exceeds 1.15", captureName, mode, equivalentMedian)
			}
		}
		t.Logf("capture %s raw SHA-256 %s", captureName, capture.rawSHA)
	}
}

func target37ParseCapture(path, captureName string) (target37ParsedCapture, error) {
	info, err := os.Stat(path)
	if err != nil {
		return target37ParsedCapture{}, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > target37CaptureMaxBytes {
		return target37ParsedCapture{}, fmt.Errorf("invalid raw capture size %d", info.Size())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return target37ParsedCapture{}, err
	}
	if !utf8.Valid(content) || content[len(content)-1] != '\n' || strings.ContainsRune(string(content), '\x00') {
		return target37ParsedCapture{}, fmt.Errorf("raw capture is not canonical newline-terminated UTF-8")
	}
	digest := sha256.Sum256(content)
	parsed := target37ParsedCapture{rows: make(map[string]target37CaptureRow), rawSHA: fmt.Sprintf("%x", digest)}
	markerCount, passCount, okCount := 0, 0, 0
	headerValues := make(map[string]string, 4)
	containerCounts := make(map[string]int, 59)
	prefix := "BenchmarkTarget37Capture" + captureName
	for lineNumber, line := range strings.Split(strings.TrimSuffix(string(content), "\n"), "\n") {
		if index := strings.Index(line, target37IdentityPrefix); index >= 0 {
			if strings.Count(line, target37IdentityPrefix) != 1 {
				return target37ParsedCapture{}, fmt.Errorf("line %d contains multiple identity markers", lineNumber+1)
			}
			logPrefix := strings.TrimSpace(line[:index])
			if logPrefix != "" {
				lineNumberText, ok := strings.CutPrefix(logPrefix, "target37_benchmark_test.go:")
				if !ok || !strings.HasSuffix(lineNumberText, ":") {
					return target37ParsedCapture{}, fmt.Errorf("line %d has an invalid identity log prefix", lineNumber+1)
				}
				loggedLine, err := strconv.Atoi(strings.TrimSuffix(lineNumberText, ":"))
				if err != nil || loggedLine < 1 {
					return target37ParsedCapture{}, fmt.Errorf("line %d has an invalid identity log line", lineNumber+1)
				}
			}
			markerCount++
			identity, err := target37ParseIdentity(line[index:])
			if err != nil {
				return target37ParsedCapture{}, fmt.Errorf("line %d: %w", lineNumber+1, err)
			}
			parsed.identity = identity
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "FAIL") || strings.Contains(trimmed, "panic:") || strings.Contains(trimmed, "SKIP") {
			return target37ParsedCapture{}, fmt.Errorf("line %d contains failed or skipped work", lineNumber+1)
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
				return target37ParsedCapture{}, fmt.Errorf("line %d has an empty or duplicate %s header", lineNumber+1, strings.TrimSpace(header))
			}
			headerValues[header] = value
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		if !strings.HasPrefix(fields[0], "Benchmark") {
			return target37ParsedCapture{}, fmt.Errorf("line %d contains unexpected output", lineNumber+1)
		}
		name := fields[0]
		if name == prefix || strings.HasPrefix(name, prefix+"/") && len(strings.Split(name, "/")) < 4 {
			if len(fields) != 1 {
				return target37ParsedCapture{}, fmt.Errorf("line %d has malformed container benchmark", lineNumber+1)
			}
			containerCounts[name]++
			continue
		}
		if !strings.HasPrefix(name, prefix+"/") {
			return target37ParsedCapture{}, fmt.Errorf("line %d contains unexpected benchmark %q", lineNumber+1, name)
		}
		parts := strings.Split(name, "/")
		if len(parts) != 4 {
			return target37ParsedCapture{}, fmt.Errorf("line %d has malformed benchmark name %q", lineNumber+1, name)
		}
		lane := strings.TrimSuffix(parts[3], "-1")
		if (parts[1] != "Send" && parts[1] != "Public") || len(parts[2]) != 7 || !strings.HasPrefix(parts[2], "Round") ||
			(lane != "Generic" && lane != "Specialized" && lane != "Equivalent") {
			return target37ParsedCapture{}, fmt.Errorf("line %d has unknown benchmark coordinates %q", lineNumber+1, name)
		}
		round, err := strconv.Atoi(strings.TrimPrefix(parts[2], "Round"))
		if err != nil || round < 1 || round > 7 {
			return target37ParsedCapture{}, fmt.Errorf("line %d has invalid round", lineNumber+1)
		}
		if len(fields) == 1 {
			containerCounts[name]++
			continue
		}
		if len(fields) != 8 || fields[3] != "ns/op" || fields[5] != "B/op" || fields[7] != "allocs/op" {
			return target37ParsedCapture{}, fmt.Errorf("line %d lacks the exact benchmark metrics", lineNumber+1)
		}
		iterations, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || iterations < 1 {
			return target37ParsedCapture{}, fmt.Errorf("line %d has invalid iteration count", lineNumber+1)
		}
		ns, err := strconv.ParseFloat(fields[2], 64)
		if err != nil || math.IsNaN(ns) || math.IsInf(ns, 0) || ns <= 0 {
			return target37ParsedCapture{}, fmt.Errorf("line %d has invalid ns/op", lineNumber+1)
		}
		bytesPerOp, bytesErr := strconv.ParseFloat(fields[4], 64)
		allocsPerOp, allocsErr := strconv.ParseFloat(fields[6], 64)
		if bytesErr != nil || allocsErr != nil || bytesPerOp != 0 || allocsPerOp != 0 {
			return target37ParsedCapture{}, fmt.Errorf("line %d violates the zero-allocation gate", lineNumber+1)
		}
		timedNS := float64(iterations) * ns
		if timedNS < target37MinTimedNS || timedNS > target37MaxTimedNS {
			return target37ParsedCapture{}, fmt.Errorf("line %d timed %.0fns outside the 1s-capture envelope", lineNumber+1, timedNS)
		}
		key := fmt.Sprintf("%s/Round%02d/%s", parts[1], round, lane)
		if _, exists := parsed.rows[key]; exists {
			return target37ParsedCapture{}, fmt.Errorf("line %d duplicates %s", lineNumber+1, key)
		}
		parsed.rows[key] = target37CaptureRow{iterations: iterations, ns: ns}
		parsed.sequence = append(parsed.sequence, key)
	}
	if markerCount != 1 || parsed.identity == nil || parsed.identity["capture"] != captureName {
		return target37ParsedCapture{}, fmt.Errorf("expected one authenticated %s identity marker, got %d", captureName, markerCount)
	}
	if headerValues["goos: "] != parsed.identity["goos"] || headerValues["goarch: "] != parsed.identity["goarch"] ||
		headerValues["pkg: "] != "github.com/besmpl/ember/internal/rubyproof/generated" || headerValues["cpu: "] == "" {
		return target37ParsedCapture{}, fmt.Errorf("capture headers disagree with the authenticated identity")
	}
	parsed.cpu = headerValues["cpu: "]
	expectedSequence := target37ExpectedCaptureSequence(captureName)
	if len(parsed.sequence) != len(expectedSequence) {
		return target37ParsedCapture{}, fmt.Errorf("metric sequence has %d rows, want %d", len(parsed.sequence), len(expectedSequence))
	}
	for index := range expectedSequence {
		if parsed.sequence[index] != expectedSequence[index] {
			return target37ParsedCapture{}, fmt.Errorf("metric row %d is %s, want %s", index+1, parsed.sequence[index], expectedSequence[index])
		}
	}
	expectedContainers := target37ExpectedCaptureContainers(captureName)
	if len(containerCounts) != len(expectedContainers) {
		return target37ParsedCapture{}, fmt.Errorf("container inventory has %d names, want %d", len(containerCounts), len(expectedContainers))
	}
	for _, name := range expectedContainers {
		if containerCounts[name] != 1 {
			return target37ParsedCapture{}, fmt.Errorf("container %s occurs %d times", name, containerCounts[name])
		}
	}
	if len(parsed.rows) != 42 || passCount != 1 || okCount != 1 {
		return target37ParsedCapture{}, fmt.Errorf("incomplete capture: rows=%d PASS=%d ok=%d", len(parsed.rows), passCount, okCount)
	}
	return parsed, nil
}

func target37ExpectedCaptureSequence(captureName string) []string {
	orders := target37CaptureAOrders
	if captureName == "B" {
		orders = target37CaptureBOrders
	}
	sequence := make([]string, 0, 42)
	for _, mode := range []string{"Send", "Public"} {
		for round, order := range orders {
			for _, lane := range order {
				name := ""
				switch lane {
				case target37CaptureGeneric:
					name = "Generic"
				case target37CaptureSpecialized:
					name = "Specialized"
				case target37CaptureEquivalent:
					name = "Equivalent"
				default:
					panic("target37: invalid capture order")
				}
				sequence = append(sequence, fmt.Sprintf("%s/Round%02d/%s", mode, round+1, name))
			}
		}
	}
	return sequence
}

func target37ExpectedCaptureContainers(captureName string) []string {
	prefix := "BenchmarkTarget37Capture" + captureName
	containers := []string{prefix}
	for _, mode := range []string{"Send", "Public"} {
		containers = append(containers, prefix+"/"+mode)
		for round := 1; round <= 7; round++ {
			containers = append(containers, fmt.Sprintf("%s/%s/Round%02d", prefix, mode, round))
		}
	}
	for _, coordinate := range target37ExpectedCaptureSequence(captureName) {
		containers = append(containers, prefix+"/"+coordinate)
	}
	return containers
}

func target37ParseIdentity(line string) (map[string]string, error) {
	if !strings.HasPrefix(line, target37IdentityPrefix) {
		return nil, fmt.Errorf("invalid identity prefix")
	}
	fields := strings.Split(strings.TrimPrefix(line, target37IdentityPrefix), "\t")
	identity := make(map[string]string, len(fields))
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" || value == "" || identity[key] != "" {
			return nil, fmt.Errorf("malformed identity field %q", field)
		}
		identity[key] = value
	}
	required := []string{"schema", "capture", "go", "goos", "goarch", "goarm64", "goamd64", "gomaxprocs", "cgo", "binary", "generated", "benchmark", "equivalent", "gate"}
	if len(identity) != len(required) {
		return nil, fmt.Errorf("identity has %d fields, want %d", len(identity), len(required))
	}
	for _, key := range required {
		if _, ok := identity[key]; !ok {
			return nil, fmt.Errorf("identity is missing %s", key)
		}
	}
	if identity["schema"] != "1" || (identity["capture"] != "A" && identity["capture"] != "B") || identity["gomaxprocs"] != "1" || identity["cgo"] != "0" {
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

func target37SameIdentity(left, right map[string]string) bool {
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
