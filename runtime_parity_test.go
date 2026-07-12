package ember_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/besmpl/ember"
)

const (
	parityLuauSHA256  = "c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd"
	parityLuauVersion = "0.728"
	parityPlatform    = "Darwin 24.6.0 arm64"
	parityCPU         = "Apple M1"
	parityRawHeader   = "# ember-runtime-parity raw/v2"
	parityRawDefault  = "tmp/runtime-parity/raw.tsv"
	parityRepeatCount = 3

	parityLoadMax = 2.0
	parityCPUMax  = 100.0
)

var parityIterations = [...]int{1, 10, 100, 1000}

const parityPairCount = 9

type parityFit struct {
	Entry float64
	Inner float64
}

type paritySystemSample struct {
	Load float64
	CPU  float64
}

type parityEnvironment struct {
	LuauPath    string
	LuauSHA256  string
	LuauVersion string
	Platform    string
	CPU         string
	CGOEnabled  string
	GOMAXPROCS  int
}

func finiteParityFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// fitParityLine fits T(N)=entry+N*inner with an intercept. Keeping this
// calculation in one small pure helper makes the gate's statistic explicit and
// gives the deterministic harness a direct oracle for the shell gate.
func fitParityLine(samples map[int]float64) (parityFit, error) {
	if len(samples) != len(parityIterations) {
		return parityFit{}, fmt.Errorf("parity fit: want %d points, got %d", len(parityIterations), len(samples))
	}

	var meanN float64
	var meanT float64
	for _, n := range parityIterations {
		timing, ok := samples[n]
		if !ok {
			return parityFit{}, fmt.Errorf("parity fit: missing N=%d", n)
		}
		if timing <= 0 || !finiteParityFloat(timing) {
			return parityFit{}, fmt.Errorf("parity fit: invalid timing N=%d: %v", n, timing)
		}
		meanN += float64(n)
		meanT += timing
	}
	meanN /= float64(len(parityIterations))
	meanT /= float64(len(parityIterations))

	var numerator float64
	var denominator float64
	for _, n := range parityIterations {
		deltaN := float64(n) - meanN
		deltaT := samples[n] - meanT
		numerator += deltaN * deltaT
		denominator += deltaN * deltaN
	}
	if denominator <= 0 || !finiteParityFloat(denominator) {
		return parityFit{}, errors.New("parity fit: non-positive denominator")
	}

	inner := numerator / denominator
	entry := meanT - inner*meanN
	if !finiteParityFloat(inner) || !finiteParityFloat(entry) || inner <= 0 {
		return parityFit{}, fmt.Errorf("parity fit: non-positive or non-finite slope: entry=%v inner=%v", entry, inner)
	}
	return parityFit{Entry: entry, Inner: inner}, nil
}

func parityRatio(emberSamples, luauSamples map[int]float64) (float64, parityFit, parityFit, error) {
	emberFit, err := fitParityLine(emberSamples)
	if err != nil {
		return 0, parityFit{}, parityFit{}, fmt.Errorf("ember: %w", err)
	}
	luauFit, err := fitParityLine(luauSamples)
	if err != nil {
		return 0, parityFit{}, parityFit{}, fmt.Errorf("luau: %w", err)
	}
	ratio := emberFit.Inner / luauFit.Inner
	if !finiteParityFloat(ratio) || ratio <= 0 {
		return 0, parityFit{}, parityFit{}, fmt.Errorf("parity ratio: invalid ratio %v", ratio)
	}
	return ratio, emberFit, luauFit, nil
}

func summarizeParityRepeatRatios(ratios []float64) (median, relativeSpread float64, err error) {
	if len(ratios) != parityRepeatCount {
		return 0, 0, fmt.Errorf("parity repeat ratios: want %d samples, got %d", parityRepeatCount, len(ratios))
	}
	sorted := append([]float64(nil), ratios...)
	for i, ratio := range sorted {
		if !finiteParityFloat(ratio) || ratio <= 0 {
			return 0, 0, fmt.Errorf("parity repeat ratios: invalid sample %d: %v", i+1, ratio)
		}
	}
	sort.Float64s(sorted)
	median = sorted[len(sorted)/2]
	if median <= 0 || !finiteParityFloat(median) {
		return 0, 0, fmt.Errorf("parity repeat ratios: invalid median %v", median)
	}
	relativeSpread = (sorted[len(sorted)-1] - sorted[0]) / median
	if relativeSpread < 0 || !finiteParityFloat(relativeSpread) {
		return 0, 0, fmt.Errorf("parity repeat ratios: invalid relative spread %v", relativeSpread)
	}
	return median, relativeSpread, nil
}

func summarizeParityRatios(ratios []float64) (median, p90 float64, err error) {
	if len(ratios) != parityPairCount {
		return 0, 0, fmt.Errorf("parity ratios: want %d samples, got %d", parityPairCount, len(ratios))
	}
	sorted := append([]float64(nil), ratios...)
	for i, ratio := range sorted {
		if !finiteParityFloat(ratio) || ratio <= 0 {
			return 0, 0, fmt.Errorf("parity ratios: invalid sample %d: %v", i+1, ratio)
		}
	}
	sort.Float64s(sorted)
	return sorted[4], sorted[8], nil
}

func parityEngineOrder(pair int) [2]string {
	if pair%2 == 1 {
		return [2]string{"ember", "luau"}
	}
	return [2]string{"luau", "ember"}
}

func parityEngineOrderFor(pair, repeat, iterationIndex int) [2]string {
	if (pair+repeat+iterationIndex)%2 == 0 {
		return [2]string{"ember", "luau"}
	}
	return [2]string{"luau", "ember"}
}

func parityOrderForRepeat(repeat, engineIndex, iterationIndex int) int {
	_ = repeat
	return iterationIndex*2 + engineIndex + 1
}

func parityOrderFor(pair, engineIndex, iterationIndex int) int {
	_ = pair
	return engineIndex*4 + iterationIndex + 1
}

func parityRawPath(rawPath string) (string, error) {
	if rawPath == "" {
		rawPath = parityRawDefault
	}
	root, err := filepath.Abs("tmp/runtime-parity")
	if err != nil {
		return "", fmt.Errorf("parity raw path root: %w", err)
	}
	abs, err := filepath.Abs(rawPath)
	if err != nil {
		return "", fmt.Errorf("parity raw path: %w", err)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("parity raw path %q is outside %s", rawPath, root)
	}
	return abs, nil
}

func parityScalarString(values []ember.Value) (string, error) {
	if len(values) != 1 {
		return "", fmt.Errorf("Ember returned %d results, want 1", len(values))
	}
	value := values[0]
	if number, ok := value.Number(); ok {
		return strconv.FormatFloat(number, 'g', -1, 64), nil
	}
	if str, ok := value.String(); ok {
		return str, nil
	}
	if boolean, ok := value.Bool(); ok {
		return strconv.FormatBool(boolean), nil
	}
	if value.IsNil() {
		return "nil", nil
	}
	return "", fmt.Errorf("Ember returned %s, want scalar benchmark result", value.Kind())
}

func parityCaseFunction(source string) string {
	return fmt.Sprintf("local __case = function()\n%s\nend\n", source)
}

func parityCaseLoop(iterations int) string {
	return fmt.Sprintf("for __i = 1, %d do\n    __result = __case()\nend\n", iterations)
}

func parityCaseSource(source string, iterations int) string {
	return parityCaseFunction(source) +
		"local __result = nil\n" +
		parityCaseLoop(iterations) +
		"return __result\n"
}

func parityLuauCaseSource(source string, iterations int) string {
	return parityCaseFunction(source) +
		"local __result = nil\n" +
		"local __start = os.clock()\n" +
		parityCaseLoop(iterations) +
		"local __elapsed_ns = (os.clock() - __start) * 1000000000\n" +
		"print(__elapsed_ns)\n" +
		"print(__result)\n"
}

func parityCaseSelection(spec string) ([]top10LuauCase, error) {
	if spec == "" {
		return append([]top10LuauCase(nil), scenarioLuauCases...), nil
	}
	requested := strings.Split(spec, ",")
	byName := make(map[string][]top10LuauCase, len(top10LuauCases)+len(classicLuauCases)+len(scenarioLuauCases))
	for _, corpus := range [][]top10LuauCase{top10LuauCases, classicLuauCases, scenarioLuauCases} {
		for _, tc := range corpus {
			byName[tc.name] = append(byName[tc.name], tc)
		}
	}
	selected := make([]top10LuauCase, 0, len(requested))
	seen := make(map[string]bool, len(requested))
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		matches := byName[name]
		if len(matches) == 0 {
			return nil, fmt.Errorf("unknown frozen parity case %q", name)
		}
		if len(matches) != 1 {
			return nil, fmt.Errorf("frozen parity case %q is not unique", name)
		}
		seen[name] = true
		selected = append(selected, matches[0])
	}
	if len(selected) == 0 {
		return nil, errors.New("parity case selection is empty")
	}
	return selected, nil
}

func paritySHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func parityCommandOutput(name string, args ...string) (string, error) {
	output, err := exec.Command(name, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func inspectParityEnvironment() (parityEnvironment, error) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return parityEnvironment{}, fmt.Errorf("parity runner: want darwin/arm64, got %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	platform, err := parityCommandOutput("uname", "-srm")
	if err != nil {
		return parityEnvironment{}, fmt.Errorf("parity runner uname: %w", err)
	}
	if platform != parityPlatform {
		return parityEnvironment{}, fmt.Errorf("parity runner: want %q, got %q", parityPlatform, platform)
	}
	cpu, err := parityCommandOutput("sysctl", "-n", "machdep.cpu.brand_string")
	if err != nil {
		return parityEnvironment{}, fmt.Errorf("parity runner cpu: %w", err)
	}
	if cpu != parityCPU {
		return parityEnvironment{}, fmt.Errorf("parity runner: want CPU %q, got %q", parityCPU, cpu)
	}
	if cgo := os.Getenv("CGO_ENABLED"); cgo != "0" {
		return parityEnvironment{}, fmt.Errorf("parity runner: CGO_ENABLED must be 0, got %q", cgo)
	}
	if maxProcs := runtime.GOMAXPROCS(0); maxProcs != 1 {
		return parityEnvironment{}, fmt.Errorf("parity runner: GOMAXPROCS must be 1, got %d", maxProcs)
	}
	luauPath := os.Getenv("LUAU_BIN")
	if luauPath == "" {
		return parityEnvironment{}, errors.New("parity runner: LUAU_BIN is required")
	}
	info, err := os.Stat(luauPath)
	if err != nil {
		return parityEnvironment{}, fmt.Errorf("parity runner Luau executable: %w", err)
	}
	if info.Mode()&0o111 == 0 {
		return parityEnvironment{}, fmt.Errorf("parity runner Luau path is not executable: %s", luauPath)
	}
	digest, err := paritySHA256(luauPath)
	if err != nil {
		return parityEnvironment{}, fmt.Errorf("parity runner Luau digest: %w", err)
	}
	if digest != parityLuauSHA256 {
		return parityEnvironment{}, fmt.Errorf("parity runner Luau SHA-256: want %s, got %s", parityLuauSHA256, digest)
	}
	brewVersion, err := parityCommandOutput("brew", "info", "luau", "--json=v2")
	if err != nil {
		return parityEnvironment{}, fmt.Errorf("parity runner Homebrew Luau info: %w", err)
	}
	if !strings.Contains(brewVersion, `"version":"`+parityLuauVersion+`"`) && !strings.Contains(brewVersion, `"version": "`+parityLuauVersion+`"`) {
		return parityEnvironment{}, fmt.Errorf("parity runner Homebrew Luau version: want %s", parityLuauVersion)
	}
	return parityEnvironment{
		LuauPath:    luauPath,
		LuauSHA256:  digest,
		LuauVersion: parityLuauVersion,
		Platform:    platform,
		CPU:         cpu,
		CGOEnabled:  os.Getenv("CGO_ENABLED"),
		GOMAXPROCS:  runtime.GOMAXPROCS(0),
	}, nil
}

func sampleParitySystem() (paritySystemSample, error) {
	if runtime.GOOS != "darwin" {
		return paritySystemSample{}, fmt.Errorf("parity system sample: want darwin, got %s", runtime.GOOS)
	}
	loadOutput, err := parityCommandOutput("sysctl", "-n", "vm.loadavg")
	if err != nil {
		return paritySystemSample{}, fmt.Errorf("parity system load: %w", err)
	}
	loadFields := strings.Fields(strings.Trim(loadOutput, "{}"))
	if len(loadFields) < 1 {
		return paritySystemSample{}, fmt.Errorf("parity system load: invalid output %q", loadOutput)
	}
	load, err := strconv.ParseFloat(loadFields[0], 64)
	if err != nil || !finiteParityFloat(load) || load < 0 {
		return paritySystemSample{}, fmt.Errorf("parity system load: invalid value %q", loadFields[0])
	}

	cpuCommand := exec.Command("ps", "-A", "-o", "%cpu=")
	cpuCommand.Env = append(os.Environ(), "LC_ALL=C")
	cpuOutput, err := cpuCommand.Output()
	if err != nil {
		return paritySystemSample{}, fmt.Errorf("parity system CPU: %w", err)
	}
	var cpu float64
	for _, field := range strings.Fields(string(cpuOutput)) {
		value, err := strconv.ParseFloat(field, 64)
		if err != nil || !finiteParityFloat(value) || value < 0 {
			return paritySystemSample{}, fmt.Errorf("parity system CPU: invalid value %q", field)
		}
		cpu += value
	}
	if !finiteParityFloat(cpu) {
		return paritySystemSample{}, fmt.Errorf("parity system CPU: invalid total %v", cpu)
	}
	return paritySystemSample{Load: load, CPU: cpu}, nil
}

func paritySystemSampleClean(sample paritySystemSample) bool {
	return finiteParityFloat(sample.Load) && finiteParityFloat(sample.CPU) &&
		sample.Load >= 0 && sample.CPU >= 0 &&
		sample.Load <= parityLoadMax && sample.CPU <= parityCPUMax
}

func measureParityEmber(proto *ember.Proto) (float64, string, error) {
	start := time.Now()
	values, err := ember.Run(proto)
	elapsed := time.Since(start)
	if err != nil {
		return float64(elapsed.Nanoseconds()), "", err
	}
	result, err := parityScalarString(values)
	if err != nil {
		return float64(elapsed.Nanoseconds()), "", err
	}
	return float64(elapsed.Nanoseconds()), result, nil
}

func measureParityLuau(luauPath, scriptPath string) (float64, string, error) {
	output, err := exec.Command(luauPath, scriptPath).Output()
	if err != nil {
		return 0, "", err
	}
	return parseParityLuauOutput(output)
}

func parseParityLuauOutput(output []byte) (float64, string, error) {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 2 {
		return 0, "", fmt.Errorf("Luau parity output has %d lines, want elapsed ns and result", len(lines))
	}
	elapsed, err := strconv.ParseFloat(strings.TrimSpace(lines[0]), 64)
	if err != nil || elapsed <= 0 || !finiteParityFloat(elapsed) {
		return 0, "", fmt.Errorf("Luau parity elapsed ns %q is invalid", lines[0])
	}
	result := strings.TrimSpace(lines[1])
	if result == "" {
		return 0, "", errors.New("Luau parity result is empty")
	}
	return elapsed, result, nil
}

func TestRuntimeParityHarness(t *testing.T) {
	if !reflectDeepEqualInts(parityIterations[:], []int{1, 10, 100, 1000}) {
		t.Fatalf("iteration points changed: %v", parityIterations)
	}
	if parityPairCount != 9 {
		t.Fatalf("pair count = %d, want 9", parityPairCount)
	}
	if parityLuauSHA256 != "c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd" || parityLuauVersion != "0.728" {
		t.Fatal("Luau reference pin changed")
	}
	if parityPlatform != "Darwin 24.6.0 arm64" || parityCPU != "Apple M1" {
		t.Fatal("runner pin changed")
	}

	if len(scenarioLuauCases) != 25 {
		t.Fatalf("Scenario case count = %d, want 25", len(scenarioLuauCases))
	}
	defaultCases, err := parityCaseSelection("")
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultCases) != 25 {
		t.Fatalf("default parity selection has %d cases, want 25 Scenario cases", len(defaultCases))
	}
	seenNames := make(map[string]bool, len(scenarioLuauCases))
	for _, tc := range scenarioLuauCases {
		if tc.name == "" || tc.source == "" || tc.want == "" {
			t.Fatalf("incomplete Scenario case: %#v", tc)
		}
		if seenNames[tc.name] {
			t.Fatalf("duplicate Scenario case %q", tc.name)
		}
		seenNames[tc.name] = true
	}
	if seenNames["arithmetic_for"] {
		t.Fatal("arithmetic_for was added to the default Scenario rows")
	}
	arithmetic, err := parityCaseSelection("arithmetic_for")
	if err != nil {
		t.Fatal(err)
	}
	if len(arithmetic) != 1 || arithmetic[0].name != "arithmetic_for" || arithmetic[0].want != "1595" || arithmetic[0].source != top10LuauCases[0].source {
		t.Fatalf("explicit arithmetic_for selection = %#v", arithmetic)
	}

	const entry = 17.0
	const inner = 3.5
	samples := make(map[int]float64, len(parityIterations))
	for _, n := range parityIterations {
		samples[n] = entry + float64(n)*inner
	}
	fit, err := fitParityLine(samples)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(fit.Entry-entry) > 1e-9 || math.Abs(fit.Inner-inner) > 1e-9 {
		t.Fatalf("fit = %+v, want entry=%v inner=%v", fit, entry, inner)
	}
	negativeEntry, err := fitParityLine(map[int]float64{1: 5, 10: 95, 100: 995, 1000: 9995})
	if err != nil {
		t.Fatal(err)
	}
	if negativeEntry.Entry != -5 || negativeEntry.Inner != 10 {
		t.Fatalf("negative-intercept fit = %+v, want entry=-5 inner=10", negativeEntry)
	}

	ratio, emberFit, luauFit, err := parityRatio(samples, map[int]float64{
		1:    22,
		10:   40,
		100:  220,
		1000: 2020,
	})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(ratio-1.75) > 1e-9 || emberFit.Entry != entry || luauFit.Entry != 20 {
		t.Fatalf("ratio=%v ember=%+v luau=%+v", ratio, emberFit, luauFit)
	}
	median, p90, err := summarizeParityRatios([]float64{0.91, 1.02, 0.95, 0.88, 1.00, 0.93, 0.97, 0.89, 0.94})
	if err != nil {
		t.Fatal(err)
	}
	if median != 0.94 || p90 != 1.02 {
		t.Fatalf("median/p90 = %v/%v, want 0.94/1.02", median, p90)
	}
	if _, _, err := summarizeParityRatios([]float64{1}); err == nil {
		t.Fatal("accepted incomplete ratio set")
	}
	if _, err := fitParityLine(map[int]float64{1: 1, 10: 2, 100: 3}); err == nil {
		t.Fatal("accepted missing timing point")
	}
	if _, err := fitParityLine(map[int]float64{1: 1, 10: 2, 100: 3, 1000: math.NaN()}); err == nil {
		t.Fatal("accepted non-finite timing")
	}
	if _, _, _, err := parityRatio(samples, map[int]float64{1: 1, 10: 1, 100: 1, 1000: 1}); err == nil {
		t.Fatal("accepted non-positive Luau slope")
	}
	repeatMedian, repeatSpread, err := summarizeParityRepeatRatios([]float64{0.90, 1.00, 1.10})
	if err != nil {
		t.Fatal(err)
	}
	if repeatMedian != 1.00 || math.Abs(repeatSpread-0.20) > 1e-9 {
		t.Fatalf("repeat median/spread = %v/%v, want 1/0.2", repeatMedian, repeatSpread)
	}
	if _, _, err := summarizeParityRepeatRatios([]float64{0.9, 1.0}); err == nil {
		t.Fatal("accepted incomplete repeat ratio set")
	}
	if _, _, err := summarizeParityRepeatRatios([]float64{0.9, math.NaN(), 1.0}); err == nil {
		t.Fatal("accepted non-finite repeat ratio")
	}
	if !paritySystemSampleClean(paritySystemSample{Load: 1.0, CPU: 50.0}) {
		t.Fatal("rejected clean system sample")
	}
	for _, contaminated := range []paritySystemSample{
		{Load: parityLoadMax + 0.01, CPU: 50},
		{Load: 1, CPU: parityCPUMax + 0.01},
		{Load: math.NaN(), CPU: 50},
		{Load: 1, CPU: math.Inf(1)},
	} {
		if paritySystemSampleClean(contaminated) {
			t.Fatalf("accepted contaminated system sample: %+v", contaminated)
		}
	}

	for pair := 1; pair <= parityPairCount; pair++ {
		order := parityEngineOrder(pair)
		if pair%2 == 1 && order != [2]string{"ember", "luau"} {
			t.Fatalf("pair %d order = %v", pair, order)
		}
		if pair%2 == 0 && order != [2]string{"luau", "ember"} {
			t.Fatalf("pair %d order = %v", pair, order)
		}
		for engineIndex := range order {
			for iterationIndex := range parityIterations {
				got := parityOrderFor(pair, engineIndex, iterationIndex)
				want := engineIndex*4 + iterationIndex + 1
				if got != want {
					t.Fatalf("pair %d engine %s N=%d order=%d, want %d", pair, order[engineIndex], parityIterations[iterationIndex], got, want)
				}
			}
		}
		for repeat := 1; repeat <= parityRepeatCount; repeat++ {
			for iterationIndex, n := range parityIterations {
				gotOrder := parityEngineOrderFor(pair, repeat, iterationIndex)
				wantEmberFirst := (pair+repeat+iterationIndex)%2 == 0
				if (gotOrder[0] == "ember") != wantEmberFirst || gotOrder[1] == gotOrder[0] {
					t.Fatalf("pair %d repeat %d N=%d order=%v, want alternating start", pair, repeat, n, gotOrder)
				}
				for engineIndex := range gotOrder {
					want := iterationIndex*2 + engineIndex + 1
					if got := parityOrderForRepeat(repeat, engineIndex, iterationIndex); got != want {
						t.Fatalf("pair %d repeat %d engine %s N=%d order=%d, want %d", pair, repeat, gotOrder[engineIndex], n, got, want)
					}
				}
			}
		}
	}

	if source := parityCaseSource("return 7", 10); !strings.Contains(source, "return 7") || !strings.Contains(source, "for __i = 1, 10 do") || strings.Contains(source, "print(__result)") {
		t.Fatalf("Ember parity wrapper changed: %q", source)
	}
	luauSource := parityLuauCaseSource("return 7", 1000)
	start := strings.Index(luauSource, "local __start = os.clock()")
	loop := strings.Index(luauSource, "for __i = 1, 1000 do")
	stop := strings.Index(luauSource, "local __elapsed_ns = (os.clock() - __start) * 1000000000")
	printElapsed := strings.Index(luauSource, "print(__elapsed_ns)")
	printResult := strings.Index(luauSource, "print(__result)")
	if !(start >= 0 && start < loop && loop < stop && stop < printElapsed && printElapsed < printResult) || strings.Contains(luauSource[start:stop], "print(") {
		t.Fatalf("Luau timer/output placement changed: %q", luauSource)
	}
	elapsed, result, err := parseParityLuauOutput([]byte("1250.5\n1595\n"))
	if err != nil || elapsed != 1250.5 || result != "1595" {
		t.Fatalf("parsed Luau output = %v, %q, %v", elapsed, result, err)
	}
	for _, invalid := range [][]byte{[]byte(""), []byte("0\n1595\n"), []byte("nan\n1595\n"), []byte("1\n\n"), []byte("1\n1595\nextra\n")} {
		if _, _, err := parseParityLuauOutput(invalid); err == nil {
			t.Fatalf("accepted invalid Luau output %q", invalid)
		}
	}
	if _, err := parityRawPath("/tmp/not-under-parity"); err == nil {
		t.Fatal("accepted raw artifact outside tmp/runtime-parity")
	}
	testParityGateAcceptsArithmeticFor(t)
}

func testParityGateAcceptsArithmeticFor(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "arithmetic.tsv")
	var raw strings.Builder
	fmt.Fprintf(&raw, "%s\n", parityRawHeader)
	fmt.Fprintln(&raw, "# luau_path=/opt/homebrew/bin/luau")
	fmt.Fprintf(&raw, "# luau_sha256=%s\n", parityLuauSHA256)
	fmt.Fprintf(&raw, "# luau_version=%s\n", parityLuauVersion)
	fmt.Fprintf(&raw, "# platform=%s\n", parityPlatform)
	fmt.Fprintf(&raw, "# cpu=%s\n", parityCPU)
	fmt.Fprintln(&raw, "# cgo_enabled=0")
	fmt.Fprintln(&raw, "# gomaxprocs=1")
	fmt.Fprintln(&raw, "# iterations=1,10,100,1000")
	fmt.Fprintln(&raw, "# pairs=9")
	fmt.Fprintln(&raw, "# repeats=3")
	fmt.Fprintln(&raw, "case\tpair\trepeat\torder\tengine\tn\telapsed_ns\tresult\texpected\tload_before\tcpu_before\tload_after\tcpu_after")
	for pair := 1; pair <= parityPairCount; pair++ {
		for repeat := 1; repeat <= parityRepeatCount; repeat++ {
			for iterationIndex, n := range parityIterations {
				for engineIndex, engine := range parityEngineOrderFor(pair, repeat, iterationIndex) {
					entry, inner := -1.0, 5.0
					if engine == "luau" {
						entry, inner = -2, 10
					}
					fmt.Fprintf(&raw, "arithmetic_for\t%d\t%d\t%d\t%s\t%d\t%.0f\t1595\t1595\t1.0\t50.0\t1.0\t50.0\n", pair, repeat, parityOrderForRepeat(repeat, engineIndex, iterationIndex), engine, n, entry+inner*float64(n))
				}
			}
		}
	}
	if err := os.WriteFile(path, []byte(raw.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("scripts/scenario-ratio-gate", "--median-max", "0.95", "--p90-max", "1.00", "--cases", "arithmetic_for", path)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("arithmetic_for gate failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "| arithmetic_for |") {
		t.Fatalf("arithmetic_for gate output missing row:\n%s", output)
	}
}

// reflectDeepEqualInts keeps the contract test dependency-free and makes the
// array-to-slice comparison explicit.
func reflectDeepEqualInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func TestRuntimeParityLive(t *testing.T) {
	if os.Getenv("EMBER_RUNTIME_PARITY_LIVE") != "1" {
		t.Skip("set EMBER_RUNTIME_PARITY_LIVE=1 to run live parity measurements")
	}
	environment, err := inspectParityEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	selected, err := parityCaseSelection(os.Getenv("RUNTIME_PARITY_CASES"))
	if err != nil {
		t.Fatal(err)
	}
	rawPath, err := parityRawPath(os.Getenv("RUNTIME_PARITY_RAW"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(rawPath), 0o700); err != nil {
		t.Fatalf("create parity artifact directory: %v", err)
	}
	raw, err := os.Create(rawPath)
	if err != nil {
		t.Fatalf("create parity raw output: %v", err)
	}
	defer raw.Close()
	writeRaw := func(format string, args ...any) {
		if _, err := fmt.Fprintf(raw, format, args...); err != nil {
			t.Fatalf("write parity raw output: %v", err)
		}
	}
	writeRaw("%s\n", parityRawHeader)
	writeRaw("# luau_path=%s\n", environment.LuauPath)
	writeRaw("# luau_sha256=%s\n", environment.LuauSHA256)
	writeRaw("# luau_version=%s\n", environment.LuauVersion)
	writeRaw("# platform=%s\n", environment.Platform)
	writeRaw("# cpu=%s\n", environment.CPU)
	writeRaw("# cgo_enabled=%s\n", environment.CGOEnabled)
	writeRaw("# gomaxprocs=%d\n", environment.GOMAXPROCS)
	writeRaw("# iterations=%s\n", parityIterationString())
	writeRaw("# pairs=%d\n", parityPairCount)
	writeRaw("# repeats=%d\n", parityRepeatCount)
	writeRaw("case\tpair\trepeat\torder\tengine\tn\telapsed_ns\tresult\texpected\tload_before\tcpu_before\tload_after\tcpu_after\n")

	for _, tc := range selected {
		protos := make(map[int]*ember.Proto, len(parityIterations))
		scripts := make(map[int]string, len(parityIterations))
		for _, n := range parityIterations {
			proto, err := ember.Compile(parityCaseSource(tc.source, n))
			if err != nil {
				t.Fatalf("%s compile N=%d: %v", tc.name, n, err)
			}
			protos[n] = proto
			scriptPath := filepath.Join(filepath.Dir(rawPath), "scripts", tc.name+"-"+strconv.Itoa(n)+".luau")
			if err := os.MkdirAll(filepath.Dir(scriptPath), 0o700); err != nil {
				t.Fatalf("%s create script directory: %v", tc.name, err)
			}
			if err := os.WriteFile(scriptPath, []byte(parityLuauCaseSource(tc.source, n)), 0o700); err != nil {
				t.Fatalf("%s write Luau script: %v", tc.name, err)
			}
			scripts[n] = scriptPath
		}

		for pair := 1; pair <= parityPairCount; pair++ {
			for repeat := 1; repeat <= parityRepeatCount; repeat++ {
				rows := make([]string, 0, len(parityIterations)*2)
				for iterationIndex, n := range parityIterations {
					before, err := sampleParitySystem()
					if err != nil {
						t.Fatalf("%s pair=%d repeat=%d N=%d before sample: %v", tc.name, pair, repeat, n, err)
					}
					if !paritySystemSampleClean(before) {
						t.Fatalf("%s pair=%d repeat=%d N=%d before sample contaminated: load=%.6f cpu=%.6f", tc.name, pair, repeat, n, before.Load, before.CPU)
					}
					order := parityEngineOrderFor(pair, repeat, iterationIndex)
					pointRows := make([]string, 0, len(order))
					for engineIndex, engine := range order {
						var elapsed float64
						var result string
						switch engine {
						case "ember":
							elapsed, result, err = measureParityEmber(protos[n])
						case "luau":
							elapsed, result, err = measureParityLuau(environment.LuauPath, scripts[n])
						default:
							t.Fatalf("unknown parity engine %q", engine)
						}
						if err != nil {
							t.Fatalf("%s pair=%d repeat=%d engine=%s N=%d: %v", tc.name, pair, repeat, engine, n, err)
						}
						if elapsed <= 0 || !finiteParityFloat(elapsed) {
							t.Fatalf("%s pair=%d repeat=%d engine=%s N=%d: invalid timing %v", tc.name, pair, repeat, engine, n, elapsed)
						}
						if result != tc.want {
							t.Fatalf("%s pair=%d repeat=%d engine=%s N=%d: result %q, want %q", tc.name, pair, repeat, engine, n, result, tc.want)
						}
						pointRows = append(pointRows, fmt.Sprintf("%s\t%d\t%d\t%d\t%s\t%d\t%.0f\t%s\t%s\t%.6f\t%.6f\t", tc.name, pair, repeat, parityOrderForRepeat(repeat, engineIndex, iterationIndex), engine, n, elapsed, result, tc.want, before.Load, before.CPU))
					}
					after, err := sampleParitySystem()
					if err != nil {
						t.Fatalf("%s pair=%d repeat=%d N=%d after sample: %v", tc.name, pair, repeat, n, err)
					}
					if !paritySystemSampleClean(after) {
						t.Fatalf("%s pair=%d repeat=%d N=%d after sample contaminated: load=%.6f cpu=%.6f", tc.name, pair, repeat, n, after.Load, after.CPU)
					}
					for _, row := range pointRows {
						rows = append(rows, fmt.Sprintf("%s%.6f\t%.6f\n", row, after.Load, after.CPU))
					}
				}
				// Emit only after every adjacent A/B point in the repeat has passed
				// its own before/after contamination probes.
				for _, row := range rows {
					writeRaw("%s", row)
				}
			}
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close parity raw output: %v", err)
	}
}

func TestRuntimeParityCommandVarargWrapperKeepsCallerRegistersAfterNestedGrowth(t *testing.T) {
	selected, err := parityCaseSelection("command_vararg_router")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 {
		t.Fatalf("selected %d command cases, want 1", len(selected))
	}
	proto, err := ember.Compile(parityCaseSource(selected[0].source, 1))
	if err != nil {
		t.Fatalf("compile wrapped command case: %v", err)
	}
	results, err := ember.Run(proto)
	if err != nil {
		t.Fatalf("run wrapped command case: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("wrapped command case returned %d results, want 1", len(results))
	}
	if got, ok := results[0].Number(); !ok || got != 824780 {
		t.Fatalf("wrapped command result = %v, %t; want 824780", got, ok)
	}
}

func parityIterationString() string {
	values := make([]string, len(parityIterations))
	for i, n := range parityIterations {
		values[i] = strconv.Itoa(n)
	}
	return strings.Join(values, ",")
}
