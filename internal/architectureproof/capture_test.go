package architectureproof

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	pinnedLuauSHA256 = "c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd"
	repeatCount      = 3
	maxExternalCPU   = 300.0
)

var batchSizes = [...]int{1, 10, 100, 1000}
var checksumSink int64

type config struct {
	allowBusy bool
	backend   string
	luau      string
	output    string
	pair      string
	seed      int64
}

type fit struct {
	intercept float64
	slope     float64
}

type systemSample struct {
	load float64
	cpu  float64
}

type measurement struct {
	engine   string
	elapsed  float64
	checksum int64
}

func TestArchitectureCapture(t *testing.T) {
	if os.Getenv("EMBER_ARCHITECTURE_PROOF_CAPTURE") != "1" {
		t.Skip("set EMBER_ARCHITECTURE_PROOF_CAPTURE=1 through scripts/check-architecture-proof")
	}
	seed, err := strconv.ParseInt(os.Getenv("EMBER_ARCHITECTURE_PROOF_SEED"), 10, 64)
	if err != nil {
		t.Fatalf("parse architecture proof seed: %v", err)
	}
	allowBusy, err := strconv.ParseBool(os.Getenv("EMBER_ARCHITECTURE_PROOF_ALLOW_BUSY"))
	if err != nil {
		t.Fatalf("parse architecture proof allow-busy: %v", err)
	}
	cfg := config{
		allowBusy: allowBusy,
		backend:   os.Getenv("EMBER_ARCHITECTURE_PROOF_BACKEND"),
		luau:      os.Getenv("LUAU_BIN"),
		output:    os.Getenv("EMBER_ARCHITECTURE_PROOF_OUTPUT"),
		pair:      os.Getenv("EMBER_ARCHITECTURE_PROOF_PAIR"),
		seed:      seed,
	}
	if err := run(cfg); err != nil {
		t.Fatal(err)
	}
}

func run(cfg config) error {
	if cfg.backend != "go-aot-ceiling" && cfg.backend != "go-aot-sensitivity" {
		return fmt.Errorf("unknown backend %q", cfg.backend)
	}
	if cfg.pair != "a" && cfg.pair != "b" {
		return fmt.Errorf("pair must be a or b, got %q", cfg.pair)
	}
	if cfg.output == "" {
		return errors.New("-output is required")
	}
	if _, err := os.Stat(cfg.output); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("output already exists: %s", cfg.output)
		}
		return fmt.Errorf("stat output: %w", err)
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return fmt.Errorf("requires darwin/arm64, got %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if os.Getenv("CGO_ENABLED") != "0" {
		return fmt.Errorf("CGO_ENABLED must be 0, got %q", os.Getenv("CGO_ENABLED"))
	}
	if runtime.GOMAXPROCS(0) != 1 {
		return fmt.Errorf("GOMAXPROCS must be 1, got %d", runtime.GOMAXPROCS(0))
	}
	if err := validateLuau(cfg.luau); err != nil {
		return err
	}
	if err := validateFrozenFunctions(); err != nil {
		return err
	}
	if !cfg.allowBusy {
		if err := waitForQuiet(60, time.Second); err != nil {
			return err
		}
	}
	if err := os.Mkdir(cfg.output, 0o700); err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	incomplete := filepath.Join(cfg.output, "INCOMPLETE")
	if err := os.WriteFile(incomplete, nil, 0o600); err != nil {
		return err
	}

	scriptsDir := filepath.Join(cfg.output, "scripts")
	if err := os.Mkdir(scriptsDir, 0o700); err != nil {
		return err
	}
	raw, err := createOutput(cfg.output, "raw.tsv")
	if err != nil {
		return err
	}
	defer raw.Close()
	slopes, err := createOutput(cfg.output, "slopes.tsv")
	if err != nil {
		return err
	}
	defer slopes.Close()
	allocations, err := createOutput(cfg.output, "allocations.tsv")
	if err != nil {
		return err
	}
	defer allocations.Close()
	facts, err := createOutput(cfg.output, "backend-facts.tsv")
	if err != nil {
		return err
	}
	defer facts.Close()

	fmt.Fprintln(raw, "schema_version\tbackend\tpair\tcase\tfamily\tengine\trepeat\tpoint\tn_calls\truntime_seed\telapsed_ns\tchecksum\tsemantic_sha256")
	fmt.Fprintln(slopes, "schema_version\tbackend\tpair\tcase\tfamily\tengine\trepeat\tslope_ns_per_guest_call\tintercept_ns\tsemantic_sha256")
	fmt.Fprintln(allocations, "schema_version\tbackend\tcase\tfamily\tn_calls\tns_per_batch\tbytes_per_batch\tallocs_per_batch\tbytes_per_guest\tallocs_per_guest")
	fmt.Fprintln(facts, "schema_version\tbackend\tpair\tkey\tvalue")

	for _, candidate := range proofCases {
		runFunc := candidate.run
		if cfg.backend == "go-aot-sensitivity" && candidate.sensitivity != nil {
			runFunc = candidate.sensitivity
		}
		script := luauScript(candidate.luauBody)
		scriptPath := filepath.Join(scriptsDir, strings.NewReplacer("/", "-", "\\", "-").Replace(candidate.id)+".luau")
		if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
			return fmt.Errorf("%s write Luau script: %w", candidate.id, err)
		}
		semanticHash := digestString(candidate.luauBody)
		timings := map[string]map[int]map[int]float64{
			"go":   {},
			"luau": {},
		}
		for repeat := 1; repeat <= repeatCount; repeat++ {
			timings["go"][repeat] = make(map[int]float64, len(batchSizes))
			timings["luau"][repeat] = make(map[int]float64, len(batchSizes))
			for point, n := range batchSizes {
				seed := cfg.seed + int64(point*1009+repeat*65537)
				order := engineOrder(cfg.pair, repeat, point)
				measurePoint := func() ([]measurement, error) {
					got := make([]measurement, 0, len(order))
					for _, engine := range order {
						var current measurement
						var measureErr error
						switch engine {
						case "go":
							current = measureGo(runFunc, n, seed)
						case "luau":
							current, measureErr = measureLuau(cfg.luau, scriptPath, n, seed)
						default:
							measureErr = fmt.Errorf("unknown engine %q", engine)
						}
						if measureErr != nil {
							return nil, measureErr
						}
						got = append(got, current)
					}
					return got, nil
				}
				var pointMeasurements []measurement
				var err error
				if cfg.allowBusy {
					pointMeasurements, err = measurePoint()
				} else {
					pointMeasurements, err = acquireCleanPoint(measurePoint)
				}
				if err != nil {
					return fmt.Errorf("%s repeat=%d N=%d: %w", candidate.id, repeat, n, err)
				}
				if len(pointMeasurements) != 2 || pointMeasurements[0].checksum != pointMeasurements[1].checksum {
					return fmt.Errorf("%s repeat=%d N=%d checksum mismatch: %#v", candidate.id, repeat, n, pointMeasurements)
				}
				for _, current := range pointMeasurements {
					timings[current.engine][repeat][n] = current.elapsed
					fmt.Fprintf(raw, "1\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%.17g\t%d\t%s\n",
						cfg.backend,
						cfg.pair,
						candidate.id,
						candidate.family,
						current.engine,
						repeat,
						point+1,
						n,
						seed,
						current.elapsed,
						current.checksum,
						semanticHash,
					)
				}
			}
		}
		for _, engine := range []string{"go", "luau"} {
			for repeat := 1; repeat <= repeatCount; repeat++ {
				currentFit, err := fitLine(timings[engine][repeat])
				if err != nil {
					return fmt.Errorf("%s %s repeat=%d: %w", candidate.id, engine, repeat, err)
				}
				fmt.Fprintf(slopes, "1\t%s\t%s\t%s\t%s\t%s\t%d\t%.17g\t%.17g\t%s\n",
					cfg.backend,
					cfg.pair,
					candidate.id,
					candidate.family,
					engine,
					repeat,
					currentFit.slope,
					currentFit.intercept,
					semanticHash,
				)
			}
		}
		for _, n := range []int{1, 1000} {
			result := benchmarkBatch(runFunc, n, cfg.seed)
			fmt.Fprintf(allocations, "1\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%.9g\t%.9g\n",
				cfg.backend,
				candidate.id,
				candidate.family,
				n,
				result.NsPerOp(),
				result.AllocedBytesPerOp(),
				result.AllocsPerOp(),
				float64(result.AllocedBytesPerOp())/float64(n),
				float64(result.AllocsPerOp())/float64(n),
			)
		}
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executableInfo, err := os.Stat(executable)
	if err != nil {
		return err
	}
	executableHash, err := digestFile(executable)
	if err != nil {
		return err
	}
	sourceHash, err := digestSources()
	if err != nil {
		return err
	}
	luauHash, _ := digestFile(cfg.luau)
	buildFacts := map[string]string{
		"binary_bytes":       strconv.FormatInt(executableInfo.Size(), 10),
		"binary_sha256":      executableHash,
		"cgo_enabled":        os.Getenv("CGO_ENABLED"),
		"go_version":         runtime.Version(),
		"gomaxprocs":         strconv.Itoa(runtime.GOMAXPROCS(0)),
		"luau_sha256":        luauHash,
		"runtime_seed":       strconv.FormatInt(cfg.seed, 10),
		"source_sha256":      sourceHash,
		"callable_scope":     "guest_batch_v1",
		"programs_per_case":  "1",
		"runtime_selected_n": "true",
		"allow_busy":         strconv.FormatBool(cfg.allowBusy),
		"proof_kind":         "manual_semantic_lowering_ceiling",
	}
	for key, value := range vcsFacts() {
		buildFacts[key] = value
	}
	for key, value := range buildFacts {
		fmt.Fprintf(facts, "1\t%s\t%s\t%s\t%s\n", cfg.backend, cfg.pair, key, value)
	}
	if err := raw.Close(); err != nil {
		return err
	}
	if err := slopes.Close(); err != nil {
		return err
	}
	if err := allocations.Close(); err != nil {
		return err
	}
	if err := facts.Close(); err != nil {
		return err
	}
	if err := writeSummary(cfg.output); err != nil {
		return err
	}
	if err := os.Remove(incomplete); err != nil {
		return err
	}
	fmt.Printf("architecture-proof: captured %s/%s in %s\n", cfg.backend, cfg.pair, cfg.output)
	return nil
}

func createOutput(directory, name string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", name, err)
	}
	return file, nil
}

func validateLuau(path string) error {
	hash, err := digestFile(path)
	if err != nil {
		return fmt.Errorf("hash Luau: %w", err)
	}
	if hash != pinnedLuauSHA256 {
		return fmt.Errorf("Luau SHA-256=%s, want %s", hash, pinnedLuauSHA256)
	}
	return nil
}

func validateFrozenFunctions() error {
	for _, candidate := range proofCases {
		if got := candidate.run(0); got != candidate.wantFrozen {
			return fmt.Errorf("%s frozen Go result=%d, want %d", candidate.id, got, candidate.wantFrozen)
		}
		if candidate.sensitivity != nil {
			if got := candidate.sensitivity(0); got != candidate.wantFrozen {
				return fmt.Errorf("%s sensitivity frozen result=%d, want %d", candidate.id, got, candidate.wantFrozen)
			}
			for _, seed := range []int64{1, 7, 29, 101} {
				if got, want := candidate.sensitivity(seed), candidate.run(seed); got != want {
					return fmt.Errorf("%s sensitivity seed=%d result=%d, want %d", candidate.id, seed, got, want)
				}
			}
		}
	}
	return nil
}

func luauScript(body string) string {
	return `local __n_arg, __seed_arg = ...
local __n = tonumber(__n_arg)
local __seed = tonumber(__seed_arg)
assert(__n ~= nil and __seed ~= nil)
local function __case(seed)
` + body + `
end
local __warm = __case(__seed)
local __checksum = 0
local __start = os.clock()
-- EMBER_ARCHITECTURE_TIMER_START
for __i = 1, __n do
    local __result = __case(__seed + __i)
    __checksum = __checksum + __result * (__i % 7 + 1)
end
-- EMBER_ARCHITECTURE_TIMER_STOP
local __elapsed_ns = (os.clock() - __start) * 1000000000
print(__elapsed_ns)
print(string.format("%.0f", __checksum))
`
}

func measureGo(run proofFunc, n int, seed int64) measurement {
	_ = run(seed)
	var checksum int64
	start := time.Now()
	for i := 1; i <= n; i++ {
		checksum += run(seed+int64(i)) * int64(i%7+1)
	}
	elapsed := time.Since(start)
	checksumSink = checksum
	return measurement{engine: "go", elapsed: float64(elapsed.Nanoseconds()), checksum: checksum}
}

func measureLuau(luau, script string, n int, seed int64) (measurement, error) {
	command := exec.Command(luau, script, "-a", strconv.Itoa(n), strconv.FormatInt(seed, 10))
	output, err := command.Output()
	if err != nil {
		return measurement{}, fmt.Errorf("run Luau: %w", err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	if !scanner.Scan() {
		return measurement{}, errors.New("Luau output missing elapsed time")
	}
	elapsed, err := strconv.ParseFloat(strings.TrimSpace(scanner.Text()), 64)
	if err != nil || elapsed <= 0 || math.IsNaN(elapsed) || math.IsInf(elapsed, 0) {
		return measurement{}, fmt.Errorf("invalid Luau elapsed %q", scanner.Text())
	}
	if !scanner.Scan() {
		return measurement{}, errors.New("Luau output missing checksum")
	}
	checksum, err := strconv.ParseInt(strings.TrimSpace(scanner.Text()), 10, 64)
	if err != nil {
		return measurement{}, fmt.Errorf("invalid Luau checksum %q: %w", scanner.Text(), err)
	}
	if scanner.Scan() {
		return measurement{}, fmt.Errorf("unexpected Luau output %q", scanner.Text())
	}
	return measurement{engine: "luau", elapsed: elapsed, checksum: checksum}, nil
}

func engineOrder(pair string, repeat, point int) [2]string {
	parity := repeat + point
	if pair == "b" {
		parity++
	}
	if parity%2 == 0 {
		return [2]string{"go", "luau"}
	}
	return [2]string{"luau", "go"}
}

func acquireCleanPoint(measure func() ([]measurement, error)) ([]measurement, error) {
	var last systemSample
	for attempt := 1; attempt <= 60; attempt++ {
		before, err := sampleSystem()
		if err != nil {
			return nil, err
		}
		if !before.clean() {
			last = before
			time.Sleep(time.Second)
			continue
		}
		point, err := measure()
		if err != nil {
			return nil, err
		}
		after, err := sampleSystem()
		if err != nil {
			return nil, err
		}
		if after.clean() {
			return point, nil
		}
		last = after
		time.Sleep(time.Second)
	}
	return nil, fmt.Errorf("point stayed contaminated: load=%.3f external_cpu=%.3f", last.load, last.cpu)
}

func waitForQuiet(maxAttempts int, delay time.Duration) error {
	consecutive := 0
	var last systemSample
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		current, err := sampleSystem()
		if err != nil {
			return err
		}
		last = current
		if current.clean() {
			consecutive++
			if consecutive >= 3 {
				return nil
			}
		} else {
			consecutive = 0
		}
		if attempt < maxAttempts {
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("runner stayed busy: load=%.3f external_cpu=%.3f", last.load, last.cpu)
}

func sampleSystem() (systemSample, error) {
	load, err := sampleLoad()
	if err != nil {
		return systemSample{}, err
	}
	cpu, err := sampleExternalCPU()
	if err != nil {
		return systemSample{}, err
	}
	return systemSample{load: load, cpu: cpu}, nil
}

func sampleLoad() (float64, error) {
	loadOutput, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		return 0, err
	}
	loadFields := strings.Fields(strings.Trim(string(loadOutput), "{} \n\t"))
	if len(loadFields) == 0 {
		return 0, fmt.Errorf("invalid vm.loadavg %q", loadOutput)
	}
	load, err := strconv.ParseFloat(loadFields[0], 64)
	if err != nil {
		return 0, err
	}
	return load, nil
}

func sampleExternalCPU() (float64, error) {
	cpuCommand := exec.Command("ps", "-A", "-o", "pid=,%cpu=")
	cpuCommand.Env = append(os.Environ(), "LC_ALL=C")
	cpuOutput, err := cpuCommand.Output()
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(cpuOutput))
	if len(fields)%2 != 0 {
		return 0, errors.New("invalid ps output")
	}
	var cpu float64
	for index := 0; index < len(fields); index += 2 {
		pid, err := strconv.Atoi(fields[index])
		if err != nil {
			return 0, err
		}
		current, err := strconv.ParseFloat(fields[index+1], 64)
		if err != nil {
			return 0, err
		}
		if pid != os.Getpid() {
			cpu += current
		}
	}
	return cpu, nil
}

func (sample systemSample) clean() bool {
	return sample.load >= 0 && sample.cpu >= 0 && sample.cpu <= maxExternalCPU &&
		!math.IsNaN(sample.load) && !math.IsNaN(sample.cpu) &&
		!math.IsInf(sample.load, 0) && !math.IsInf(sample.cpu, 0)
}

func fitLine(samples map[int]float64) (fit, error) {
	if len(samples) != len(batchSizes) {
		return fit{}, fmt.Errorf("want %d points, got %d", len(batchSizes), len(samples))
	}
	var meanN float64
	var meanT float64
	for _, n := range batchSizes {
		elapsed, ok := samples[n]
		if !ok || elapsed <= 0 {
			return fit{}, fmt.Errorf("invalid N=%d timing %v", n, elapsed)
		}
		meanN += float64(n)
		meanT += elapsed
	}
	meanN /= float64(len(batchSizes))
	meanT /= float64(len(batchSizes))
	var numerator float64
	var denominator float64
	for _, n := range batchSizes {
		deltaN := float64(n) - meanN
		numerator += deltaN * (samples[n] - meanT)
		denominator += deltaN * deltaN
	}
	slope := numerator / denominator
	intercept := meanT - slope*meanN
	if slope <= 0 || math.IsNaN(slope) || math.IsInf(slope, 0) {
		return fit{}, fmt.Errorf("invalid fit intercept=%g slope=%g", intercept, slope)
	}
	return fit{intercept: intercept, slope: slope}, nil
}

func benchmarkBatch(run proofFunc, n int, seed int64) testing.BenchmarkResult {
	return testing.Benchmark(func(benchmark *testing.B) {
		var checksum int64
		benchmark.ReportAllocs()
		benchmark.ResetTimer()
		for iteration := 0; iteration < benchmark.N; iteration++ {
			for i := 1; i <= n; i++ {
				checksum += run(seed+int64(iteration+i)) * int64(i%7+1)
			}
		}
		checksumSink = checksum
	})
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func digestFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func digestSources() (string, error) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("architecture proof source location unavailable")
	}
	sourceDirectory := filepath.Dir(sourceFile)
	var matches []string
	for _, pattern := range []string{"*.go", "*.s"} {
		current, err := filepath.Glob(filepath.Join(sourceDirectory, pattern))
		if err != nil {
			return "", err
		}
		matches = append(matches, current...)
	}
	sort.Strings(matches)
	hash := sha256.New()
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", filepath.Base(path), len(data))
		hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func vcsFacts() map[string]string {
	facts := map[string]string{
		"vcs_revision": "unavailable",
		"vcs_modified": "unavailable",
	}
	revision := os.Getenv("EMBER_ARCHITECTURE_PROOF_VCS_REVISION")
	modified := os.Getenv("EMBER_ARCHITECTURE_PROOF_VCS_MODIFIED")
	if revision != "" && modified != "" {
		facts["vcs_revision"] = revision
		facts["vcs_modified"] = modified
		return facts
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return facts
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			facts["vcs_revision"] = setting.Value
		case "vcs.modified":
			facts["vcs_modified"] = setting.Value
		}
	}
	return facts
}

func writeSummary(output string) error {
	slopesPath := filepath.Join(output, "slopes.tsv")
	file, err := os.Open(slopesPath)
	if err != nil {
		return err
	}
	defer file.Close()
	type key struct {
		caseID string
		repeat int
	}
	goSlopes := make(map[key]float64)
	luauSlopes := make(map[key]float64)
	families := make(map[string]string)
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return errors.New("empty slopes.tsv")
	}
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 10 {
			return fmt.Errorf("invalid slope row: %q", scanner.Text())
		}
		repeat, err := strconv.Atoi(fields[6])
		if err != nil {
			return err
		}
		slope, err := strconv.ParseFloat(fields[7], 64)
		if err != nil {
			return err
		}
		current := key{caseID: fields[3], repeat: repeat}
		families[fields[3]] = fields[4]
		switch fields[5] {
		case "go":
			goSlopes[current] = slope
		case "luau":
			luauSlopes[current] = slope
		default:
			return fmt.Errorf("invalid engine %q", fields[5])
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	summary, err := createOutput(output, "summary.tsv")
	if err != nil {
		return err
	}
	defer summary.Close()
	fmt.Fprintln(summary, "case\tfamily\tratio_min\tratio_median\tratio_max")
	caseIDs := make([]string, 0, len(families))
	for caseID := range families {
		caseIDs = append(caseIDs, caseID)
	}
	sort.Strings(caseIDs)
	for _, caseID := range caseIDs {
		ratios := make([]float64, 0, repeatCount*repeatCount)
		for goRepeat := 1; goRepeat <= repeatCount; goRepeat++ {
			for luauRepeat := 1; luauRepeat <= repeatCount; luauRepeat++ {
				goSlope := goSlopes[key{caseID: caseID, repeat: goRepeat}]
				luauSlope := luauSlopes[key{caseID: caseID, repeat: luauRepeat}]
				if goSlope <= 0 || luauSlope <= 0 {
					return fmt.Errorf("%s missing repeat slope", caseID)
				}
				ratios = append(ratios, goSlope/luauSlope)
			}
		}
		sort.Float64s(ratios)
		fmt.Fprintf(summary, "%s\t%s\t%.9g\t%.9g\t%.9g\n",
			caseID,
			families[caseID],
			ratios[0],
			ratios[len(ratios)/2],
			ratios[len(ratios)-1],
		)
	}
	return summary.Close()
}
