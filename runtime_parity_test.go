package ember_test

import (
	"context"
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
	"github.com/besmpl/ember/internal/preparednative"
)

const (
	parityLuauSHA256  = "c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd"
	parityLuauVersion = "0.728"
	parityPlatform    = "Darwin 24.6.0 arm64"
	parityCPU         = "Apple M1"
	parityRawHeader   = "schema_version\tcapture_phase\tcapture_role\tcapture_pair\tcapture_id\tsource_commit\tcorpus\tname\tlifecycle\texecution_mode\tengine\trepeat\tacquisition_order\tn_guest_calls\truntime_seed\telapsed_ns\tresult_integer\tworkload_sha256\tprogram_sha256\tenvironment_sha256\tcontaminated"
	paritySlopeHeader = "schema_version\tcapture_phase\tcapture_role\tcapture_pair\tcapture_id\tsource_commit\tcorpus\tname\tlifecycle\texecution_mode\tengine\trepeat\tslope_ns_per_guest_call\tintercept_ns\tresult_set_sha256\tworkload_sha256\tprogram_sha256\tenvironment_sha256"
	parityRawDefault  = "tmp/runtime-parity/raw.tsv"
	parityRepeatCount = 3
	parityCaptureSeed = int64(0x454d42455206)
	paritySeedVersion = "guest-seed-v1"
	paritySeedHash    = "391b333d0af12e8757b34878f0f601c550c1dce902e7077bab3a532889c375fa"

	// The acceptance host has eight logical CPUs. This caps external activity
	// at three cores. The live CPU sample excludes this measuring process.
	parityCPUMax = 300.0

	parityPointAttemptLimit = 60
	parityPointRetryDelay   = time.Second
)

var parityIterations = [...]int{1, 10, 100, 1000}

const parityPairCount = 9

type parityCaptureContract struct {
	Phase         string
	Lifecycle     string
	CallableScope string
	GuestBatch    bool
}

func parityContractForPhase(phase string) (parityCaptureContract, error) {
	switch phase {
	case "full", "speed2x", "prepared-parity1x", "prepared-native-parity15":
		return parityCaptureContract{
			Phase:         phase,
			Lifecycle:     "guest_batch",
			CallableScope: "guest_batch_v1",
			GuestBatch:    true,
		}, nil
	default:
		return parityCaptureContract{}, fmt.Errorf("unknown parity capture phase %q", phase)
	}
}

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
	abs, err := filepath.Abs(rawPath)
	if err != nil {
		return "", fmt.Errorf("parity raw path: %w", err)
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

func parityExactIntegerResult(values []ember.Value) (string, error) {
	result, err := parityScalarString(values)
	if err != nil {
		return "", err
	}
	return parityCanonicalIntegerString(result)
}

func parityValidateIntegerString(result string) error {
	_, err := parityCanonicalIntegerString(result)
	return err
}

func parityCanonicalIntegerString(result string) (string, error) {
	value, err := strconv.ParseFloat(result, 64)
	if err != nil || !finiteParityFloat(value) || math.Trunc(value) != value {
		return "", fmt.Errorf("guest result %q is not an exact integer", result)
	}
	const maxExactInteger = float64(1 << 53)
	if value <= -maxExactInteger || value >= maxExactInteger {
		return "", fmt.Errorf("guest result %q exceeds exact Float64 integer range", result)
	}
	return strconv.FormatInt(int64(value), 10), nil
}

func parityCaseSource(source string, iterations int) string {
	return runtimeParityPublicCallFixture(source, iterations, false)
}

func parityLuauCaseSource(source string, iterations int) string {
	return runtimeParityPublicCallFixture(source, iterations, true)
}

func parityGuestBatchSource(source string, variant parityFixtureVariant) (string, error) {
	return runtimeParityGuestBatchFixture(source, variant, false)
}

func parityGuestBatchLuauSource(source string, variant parityFixtureVariant) (string, error) {
	return runtimeParityGuestBatchFixture(source, variant, true)
}

func parityCaseSelection(spec string) ([]top10LuauCase, error) {
	entries, err := parityManifestSelection(spec)
	if err != nil {
		return nil, err
	}
	selected := make([]top10LuauCase, 0, len(entries))
	for _, entry := range entries {
		selected = append(selected, entry.Case)
	}
	return selected, nil
}

func parityManifestSelection(spec string) ([]parityManifestEntry, error) {
	if spec == "" {
		selected := make([]parityManifestEntry, 0, len(parityCaseManifest()))
		for _, entry := range parityCaseManifest() {
			selected = append(selected, entry)
		}
		return selected, nil
	}
	requested := strings.Split(spec, ",")
	qualified := make(map[string]parityManifestEntry, len(parityCaseManifest()))
	bare := make(map[string][]parityManifestEntry, len(parityCaseManifest()))
	for _, entry := range parityCaseManifest() {
		qualified[entry.Corpus+"/"+entry.Name] = entry
		bare[entry.Name] = append(bare[entry.Name], entry)
	}
	selected := make([]parityManifestEntry, 0, len(requested))
	seen := make(map[string]bool, len(requested))
	for _, requestedName := range requested {
		name := strings.TrimSpace(requestedName)
		if name == "" || seen[name] {
			continue
		}
		if tc, ok := qualified[name]; ok {
			selected = append(selected, tc)
			seen[name] = true
			continue
		}
		matches := bare[name]
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

type parityManifestEntry struct {
	Corpus string
	Name   string
	Case   top10LuauCase
	Hash   string
}

var parityFrozenSourceHashes = map[string]string{
	"top10/arithmetic_for":               "bc42d7b066b110385e6ca8e0035511dfc84012f4f79f6c336dd01bd9756711f1",
	"top10/while_branching":              "b920d6f2ef543547794dedb84cfb103be6ea19b949b1b0e06048395d31d36c15",
	"top10/table_fields":                 "44a962b0fd05a5db98c4dd8c188a8217796d0b04cb31530df63d574a4ab704cd",
	"top10/array_ops":                    "d0eb61dec53b326b9aec71eb69f28f6422ae65d896b7237d5a775f2a7b872f72",
	"top10/generic_iteration":            "8469564700b557d50818683980844896a427eef492570284b2ee6c178687b2ac",
	"top10/closures_upvalues":            "fdc9767d1fcf09fc2c61721c31131cba7cb4c87b4ea61776a25499a1cbf87cd7",
	"top10/method_calls":                 "43e68c5826b5ead64771fe98fc7c59a4868485489817e297cabfa0f3fba4e2c8",
	"top10/metatable_index":              "4b08a177fd476c254ac3bc6788bcac6f881b947131d2551f7a50cad4896bbbc5",
	"top10/varargs_select":               "71d90b48af6a92c38609724edcf111f65e384aeef973dec531dfafde8b1fa7b6",
	"top10/coroutine_yield":              "4f48fbdb7d3edbca6fd7a2f2c6f5c223acec6506b6efc08e83cb06b0974c48fd",
	"classic/recursive_fibonacci":        "13995a466152c0792cf725904c7ae05ef7c6a893fcc1ef0c1913883fa6831608",
	"classic/iterative_fibonacci":        "aa55947fcb91bc0eca3284b251b3f67a4e041e0ad9733776dfc5f72e88055cf2",
	"scenario/combat_tick":               "3bda962c221dc0dacfeb622a071f75f388635d94f360ad0b00efd1f5aa793ff5",
	"scenario/inventory_value":           "2401c838684d853922acfdc00166849bb4fbe32f7e9ab88402430dd14f2fa83c",
	"scenario/event_dispatch":            "dc6142298883c058cd605339b2c3dc91f35a31b1717b71ec912c569b86d20c0f",
	"scenario/buff_stack_tick":           "0b51fdffe3b1d5194fe9415e656aa29dd84746b75d58386ff580347bbd6234e8",
	"scenario/ability_resolution":        "abb89f864ed22bf8fad94da7c3751d94fae6bdd1f86f521538846a95781c394f",
	"scenario/ai_utility_scoring":        "04682a6d53f1f529178c8b64f26a9c6ae6a37c83824b1ddfb29edb3f6d2f0121",
	"scenario/cooldown_scheduler":        "67e96875ea84efcdf3a2ee3883499e39bc0cb55340122dc1f05a92cd127d0018",
	"scenario/projectile_sweep":          "e0fb19c5670d9ccb2a93f367d2896a3f5e37dfc325fd19a272af35aa90fb7cfd",
	"scenario/quest_progress_update":     "9962018051ab80a084397a24c15c239c7a3a41bc1fe1038469ac0616b018938a",
	"scenario/behavior_tree_tick":        "c1c84369e591733276970b86a839b900608f2691c99242e5697aacf7d131a37b",
	"scenario/threat_aggro_table":        "f87b573bc228fcf3d7b49614fd2a697288d72a2fa9070a4957c57db879d67ebc",
	"scenario/economy_market_tick":       "529f24ee51e9d202cc5477e9bad28fadb4c59b386e02b66f09415216db11a5d6",
	"scenario/formation_layout_score":    "016286e343bc5f65cdb0c998663239264d96a788cf2a26b25f20e1fef71db8a6",
	"scenario/dialogue_condition_eval":   "7aa4160451be2c7863779644b98a3ff809e7dd82a6ace764fc9aafd44f5d0d3f",
	"scenario/procgen_room_scoring":      "f6c34b25fb9bc855befb510539170535aba6f1e4c05085d2277d4d11adfd54d7",
	"scenario/save_state_diff":           "276e696b38043fa87ed77280c3d29b52bf8cefa259a41d5e8addf0175b5da232",
	"scenario/path_relaxation":           "0a1f10deac6f84581547f74fc284f04160659709e7ebba7340f2684c7d5e0fbf",
	"scenario/component_churn":           "5c0bd542f684f7a7e73d0ffcd46e8ff969ba3891023d4e260708869d1b436e67",
	"scenario/prototype_fallback":        "8bd426aa556bfa474e09ace82b57129e42f2cb6b68290686ac01e424d71267ba",
	"scenario/signal_bus_callbacks":      "f0686a59e2753c184a9210dd1a34dbbe11703873d137ae007cd3d7304569d4d8",
	"scenario/state_machine_transitions": "3d399294e71db1397a434dc83eed27fa9fc3c577ed803f56239f4a13737f5604",
	"scenario/sparse_grid_neighbors":     "822f2c8ffa746feb670d283e15e23050f5443001da161ea37a4aa28b9066a170",
	"scenario/dirty_metatable_writes":    "06bfd3fe871cd329cb0d089debc6a45d131f4c8e07f0701483a64984bb23fdb9",
	"scenario/array_hole_compaction":     "d48c95ce4c899c04a07a4271b93f913d70873eafa7b1c47a00e09dcc18f94fe2",
	"scenario/command_vararg_router":     "fb4cb73c1075c421d49f3f2b909ec0ee26d6662f6aaa4fc7b6b894852514d59d",
}

func parityCaseManifest() []parityManifestEntry {
	entries := make([]parityManifestEntry, 0, len(top10LuauCases)+len(classicLuauCases)+len(scenarioLuauCases))
	for _, group := range []struct {
		name  string
		cases []top10LuauCase
	}{
		{name: "top10", cases: top10LuauCases},
		{name: "classic", cases: classicLuauCases},
		{name: "scenario", cases: scenarioLuauCases},
	} {
		for _, tc := range group.cases {
			sum := sha256.Sum256([]byte(tc.source))
			id := group.name + "/" + tc.name
			hash := parityFrozenSourceHashes[id]
			if hash == "" {
				hash = hex.EncodeToString(sum[:])
			}
			entries = append(entries, parityManifestEntry{Corpus: group.name, Name: tc.name, Case: tc, Hash: hash})
		}
	}
	return entries
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
	switch name {
	case "uname", "sysctl", "brew":
	default:
		return "", fmt.Errorf("parity runner command %q is not approved", name)
	}
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

	cpuCommand := exec.Command("ps", "-A", "-o", "pid=,%cpu=")
	cpuCommand.Env = append(os.Environ(), "LC_ALL=C")
	cpuOutput, err := cpuCommand.Output()
	if err != nil {
		return paritySystemSample{}, fmt.Errorf("parity system CPU: %w", err)
	}
	cpu, err := parseParityProcessCPU(string(cpuOutput), os.Getpid())
	if err != nil {
		return paritySystemSample{}, fmt.Errorf("parity system CPU: %w", err)
	}
	return paritySystemSample{Load: load, CPU: cpu}, nil
}

func parseParityProcessCPU(output string, selfPID int) (float64, error) {
	fields := strings.Fields(output)
	if len(fields)%2 != 0 {
		return 0, fmt.Errorf("invalid process sample %q", output)
	}
	var total float64
	for index := 0; index < len(fields); index += 2 {
		pid, err := strconv.Atoi(fields[index])
		if err != nil || pid <= 0 {
			return 0, fmt.Errorf("invalid process ID %q", fields[index])
		}
		cpu, err := strconv.ParseFloat(fields[index+1], 64)
		if err != nil || !finiteParityFloat(cpu) || cpu < 0 {
			return 0, fmt.Errorf("invalid process CPU %q", fields[index+1])
		}
		if pid != selfPID {
			total += cpu
		}
	}
	if !finiteParityFloat(total) {
		return 0, fmt.Errorf("invalid process CPU total %v", total)
	}
	return total, nil
}

func paritySystemSampleClean(sample paritySystemSample) bool {
	return finiteParityFloat(sample.Load) && finiteParityFloat(sample.CPU) &&
		sample.Load >= 0 && sample.CPU >= 0 &&
		sample.CPU <= parityCPUMax
}

type parityPointMeasurement struct {
	engineIndex int
	engine      string
	elapsed     float64
	result      string
}

func acquireCleanParityPoint(
	maxAttempts int,
	sample func() (paritySystemSample, error),
	measure func() ([]parityPointMeasurement, error),
	wait func(),
) ([]parityPointMeasurement, error) {
	if maxAttempts <= 0 {
		return nil, errors.New("parity point: attempt limit must be positive")
	}
	var last paritySystemSample
	var stage string
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		before, err := sample()
		if err != nil {
			return nil, fmt.Errorf("parity point before sample: %w", err)
		}
		if !paritySystemSampleClean(before) {
			last = before
			stage = "before"
			if attempt < maxAttempts {
				wait()
			}
			continue
		}
		point, err := measure()
		if err != nil {
			return nil, err
		}
		after, err := sample()
		if err != nil {
			return nil, fmt.Errorf("parity point after sample: %w", err)
		}
		if paritySystemSampleClean(after) {
			return point, nil
		}
		last = after
		stage = "after"
		if attempt < maxAttempts {
			wait()
		}
	}
	return nil, fmt.Errorf("parity point %s sample contaminated after %d attempts: load=%.6f cpu=%.6f", stage, maxAttempts, last.Load, last.CPU)
}

type paritySourceLoader map[string]ember.Source

func (loader paritySourceLoader) LoadModule(ctx context.Context, id ember.ModuleID) (ember.Source, error) {
	if err := ctx.Err(); err != nil {
		return ember.Source{}, err
	}
	source, ok := loader[id.String()]
	if !ok {
		return ember.Source{}, fmt.Errorf("parity runner: missing module %s", id)
	}
	return source, nil
}

type parityPreparedCallable struct {
	owner     *ember.Runtime
	callback  ember.Callback
	batchFunc parityTimedBatch
	closeFunc func() error
}

func prepareParityEmberRuntime(source string) (*parityPreparedCallable, error) {
	return prepareParityEmberRuntimeNamed(source, "parity/case")
}

func prepareParityEmberRuntimeNamed(source, moduleName string) (*parityPreparedCallable, error) {
	return prepareParityEmberRuntimeIdentity(source, moduleName, moduleName, "parity")
}

func prepareParityEmberRuntimeIdentity(source, moduleName, sourceName, entrypointName string) (*parityPreparedCallable, error) {
	entry := ember.LogicalModule(moduleName)
	program, _, err := ember.LoadProgram(context.Background(), paritySourceLoader{entry.String(): {
		Name: sourceName,
		Text: source,
	}}, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: entrypointName, Module: entry}},
		Parallelism: 1,
	})
	if err != nil {
		return nil, err
	}
	var callback ember.Callback
	host := ember.RuntimeHostFunc(func(context.Context, ember.HostCall) (map[string]ember.Value, error) {
		return map[string]ember.Value{
			"__parity_capture": ember.ContextHostFuncValue(func(ctx context.Context, args []ember.Value) ([]ember.Value, error) {
				if len(args) != 1 {
					return nil, fmt.Errorf("parity capture: got %d arguments, want callable", len(args))
				}
				var err error
				callback, err = ember.CaptureCallback(ctx, args[0])
				return nil, err
			}),
		}, nil
	})
	owner, err := program.NewRuntime(ember.RuntimeOptions{Host: host})
	if err != nil {
		return nil, err
	}
	if _, err := owner.RunHook(context.Background(), "startup"); err != nil {
		_ = owner.Close()
		return nil, err
	}
	return &parityPreparedCallable{owner: owner, callback: callback}, nil
}

func (callable *parityPreparedCallable) close() error {
	if callable.closeFunc != nil {
		return callable.closeFunc()
	}
	return errors.Join(callable.callback.Close(), callable.owner.Close())
}

func (callable *parityPreparedCallable) callBatch(iterations int, seed int64) ([]ember.Value, error) {
	if callable.batchFunc != nil {
		return callable.batchFunc(iterations, seed)
	}
	return callable.callback.Call(
		context.Background(),
		ember.NumberValue(float64(iterations)),
		ember.NumberValue(float64(seed)),
	)
}

func prepareParityExactGuestBatch(source string) (*parityPreparedCallable, error) {
	callback, err := ember.PrepareExactGuestBatchThroughputForParityTest(source)
	if err != nil {
		return nil, err
	}
	return &parityPreparedCallable{callback: callback, closeFunc: callback.Close}, nil
}

func prepareParityNativeGuestBatch(source, moduleName string) (*parityPreparedCallable, error) {
	if err := preparednative.Available(); err != nil {
		return nil, fmt.Errorf("prepare native parity execution: %w", err)
	}
	ctx := context.Background()
	module := ember.LogicalModule(moduleName)
	program, _, err := ember.LoadProgram(ctx, paritySourceLoader{module.String(): {
		Name: module.String(), Text: source,
	}}, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "parity", Module: module}},
		Parallelism: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("load prepared native parity Program: %w", err)
	}
	var slot ember.PreparedRuntimeSlot
	candidate, err := slot.Prepare(program, ember.RuntimeOptions{})
	if err != nil {
		return nil, fmt.Errorf("prepare native parity generation: %w", err)
	}
	if err := slot.Activate(candidate); err != nil {
		_ = candidate.Close()
		return nil, fmt.Errorf("activate native parity generation: %w", err)
	}
	callable := &parityPreparedCallable{}
	callable.batchFunc = func(iterations int, seed int64) ([]ember.Value, error) {
		var values []ember.Value
		err := slot.Use(func(runtime *ember.Runtime) error {
			var invokeErr error
			values, invokeErr = runtime.Invoke(
				ctx,
				ember.Invocation{Module: module},
				ember.NumberValue(float64(iterations)),
				ember.NumberValue(float64(seed)),
			)
			return invokeErr
		})
		return values, err
	}
	callable.closeFunc = slot.Close
	return callable, nil
}

func TestPreparedNativeGeneralRowsUseNativeBatchOnBothArchitectures(t *testing.T) {
	selected, err := parityManifestSelection(
		"top10/arithmetic_for,top10/while_branching,classic/recursive_fibonacci,classic/iterative_fibonacci",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range selected {
		t.Run(entry.Corpus+"/"+entry.Name, func(t *testing.T) {
			programSource, _, err := runtimeParityGuestBatchProgram(entry.Case.source, parityDefaultFixtureVariant)
			if err != nil {
				t.Fatal(err)
			}
			module := ember.LogicalModule("prepared-native-parity15/" + entry.Corpus + "/" + entry.Name)
			program, _, err := ember.LoadProgram(context.Background(), paritySourceLoader{module.String(): {
				Name: module.String(), Text: programSource + "return " + parityDefaultFixtureVariant.batchName + "\n",
			}}, ember.ProgramOptions{
				Entrypoints: []ember.Entrypoint{{Name: "main", Module: module}},
				Parallelism: 1,
			})
			if err != nil {
				t.Fatal(err)
			}

			arm64, err := ember.EmitPreparedNativeARM64ForTest(program)
			if err != nil {
				t.Fatal(err)
			}
			if len(arm64.Modules) != 1 || len(arm64.Modules[0].RootClosures) < 2 {
				t.Fatalf("ARM64 module inventory = %#v", arm64.Modules)
			}
			arm64Batch := arm64.Modules[0].RootClosures[len(arm64.Modules[0].RootClosures)-1]
			if arm64Batch < 0 || int(arm64Batch) >= len(arm64.Modules[0].Functions) ||
				!arm64.Modules[0].Functions[arm64Batch].Prepared {
				t.Fatalf("ARM64 batch Proto %d is not native: %#v", arm64Batch, arm64.Modules[0].Functions)
			}
			arm64Function := arm64.Modules[0].Functions[arm64Batch]
			if arm64Function.BodyOffset >= arm64Function.Offset {
				t.Fatalf(
					"ARM64 batch body/adapter offsets = %d/%d, want a private body before its boundary adapter",
					arm64Function.BodyOffset,
					arm64Function.Offset,
				)
			}

			x8664, err := ember.EmitPreparedNativeX8664ForTest(program)
			if err != nil {
				t.Fatal(err)
			}
			if len(x8664.Modules) != 1 || len(x8664.Modules[0].RootClosures) < 2 {
				t.Fatalf("x86-64 module inventory = %#v", x8664.Modules)
			}
			x8664Batch := x8664.Modules[0].RootClosures[len(x8664.Modules[0].RootClosures)-1]
			if x8664Batch < 0 || int(x8664Batch) >= len(x8664.Modules[0].Functions) ||
				!x8664.Modules[0].Functions[x8664Batch].Prepared {
				t.Fatalf("x86-64 batch Proto %d is not native: %#v", x8664Batch, x8664.Modules[0].Functions)
			}
			x8664Function := x8664.Modules[0].Functions[x8664Batch]
			if x8664Function.BodyOffset >= x8664Function.Offset {
				t.Fatalf(
					"x86-64 batch body/adapter offsets = %d/%d, want a private body before its boundary adapter",
					x8664Function.BodyOffset,
					x8664Function.Offset,
				)
			}
		})
	}
}

type parityTimedCall func() ([]ember.Value, error)
type parityTimedBatch func(int, int64) ([]ember.Value, error)

func measureParityTimedCall(call parityTimedCall, now func() time.Time, since func(time.Time) time.Duration) (time.Duration, []ember.Value, error) {
	start := now()
	values, err := call()
	if err != nil {
		return 0, nil, err
	}
	return since(start), values, nil
}

func measureParityTimedBatch(iterations int, seed int64, call parityTimedBatch, now func() time.Time, since func(time.Time) time.Duration) (time.Duration, []ember.Value, error) {
	return measureParityTimedCall(func() ([]ember.Value, error) {
		return call(iterations, seed)
	}, now, since)
}

func measureParityTimedCalls(iterations int, call parityTimedCall, now func() time.Time, since func(time.Time) time.Duration) (time.Duration, []ember.Value, error) {
	start := now()
	var values []ember.Value
	for range iterations {
		var err error
		values, err = call()
		if err != nil {
			return 0, nil, err
		}
	}
	return since(start), values, nil
}

func measureParityEmberPublicCall(callable *parityPreparedCallable, iterations int) (float64, string, error) {
	elapsed, values, err := measureParityTimedCalls(iterations, func() ([]ember.Value, error) {
		return callable.callback.Call(context.Background())
	}, time.Now, time.Since)
	if err != nil {
		return float64(elapsed.Nanoseconds()), "", err
	}
	result, err := parityScalarString(values)
	if err != nil {
		return float64(elapsed.Nanoseconds()), "", err
	}
	return float64(elapsed.Nanoseconds()), result, nil
}

func measureParityEmberGuestBatch(callable *parityPreparedCallable, iterations int, seed int64) (float64, string, error) {
	elapsed, values, err := measureParityTimedBatch(iterations, seed, func(iterations int, seed int64) ([]ember.Value, error) {
		return callable.callBatch(iterations, seed)
	}, time.Now, time.Since)
	if err != nil {
		return float64(elapsed.Nanoseconds()), "", err
	}
	result, err := parityExactIntegerResult(values)
	if err != nil {
		return float64(elapsed.Nanoseconds()), "", err
	}
	return float64(elapsed.Nanoseconds()), result, nil
}

func measureParityLuauGuestBatch(luauPath, scriptPath string, iterations int, seed int64) (float64, string, error) {
	commandArgs := []string{scriptPath, "-a"}
	commandArgs = append(commandArgs, strconv.Itoa(iterations), strconv.FormatInt(seed, 10))
	output, err := exec.Command(luauPath, commandArgs...).CombinedOutput()
	if err != nil {
		return 0, "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	elapsed, result, err := parseParityLuauOutput(output)
	if err != nil {
		return 0, "", err
	}
	result, err = parityCanonicalIntegerString(result)
	return elapsed, result, err
}

func measureParityLuauPublicCall(luauPath, scriptPath string, iterations int) (float64, string, error) {
	output, err := exec.Command(luauPath, scriptPath, "-a", strconv.Itoa(iterations)).Output()
	if err != nil {
		return 0, "", err
	}
	return parseParityLuauOutput(output)
}

func parityResultSetSHA256(results map[int]string) (string, error) {
	var builder strings.Builder
	for _, n := range parityIterations {
		result, ok := results[n]
		if !ok || result == "" {
			return "", fmt.Errorf("result set: missing N=%d", n)
		}
		fmt.Fprintf(&builder, "%d=%s\n", n, result)
	}
	return parityStringSHA256(builder.String()), nil
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
	const wantRawHeader = "schema_version\tcapture_phase\tcapture_role\tcapture_pair\tcapture_id\tsource_commit\tcorpus\tname\tlifecycle\texecution_mode\tengine\trepeat\tacquisition_order\tn_guest_calls\truntime_seed\telapsed_ns\tresult_integer\tworkload_sha256\tprogram_sha256\tenvironment_sha256\tcontaminated"
	const wantSlopeHeader = "schema_version\tcapture_phase\tcapture_role\tcapture_pair\tcapture_id\tsource_commit\tcorpus\tname\tlifecycle\texecution_mode\tengine\trepeat\tslope_ns_per_guest_call\tintercept_ns\tresult_set_sha256\tworkload_sha256\tprogram_sha256\tenvironment_sha256"
	if parityRawHeader != wantRawHeader || paritySlopeHeader != wantSlopeHeader {
		t.Fatal("runtime parity schema v2 changed")
	}
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

	manifest := parityCaseManifest()
	if len(manifest) != 37 {
		t.Fatalf("parity case count = %d, want 37", len(manifest))
	}
	if len(parityFrozenSourceHashes) != 37 {
		t.Fatalf("frozen source hash count = %d, want 37", len(parityFrozenSourceHashes))
	}
	defaultCases, err := parityCaseSelection("")
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultCases) != 37 {
		t.Fatalf("default parity selection has %d cases, want 37 all-corpus cases", len(defaultCases))
	}
	seenIDs := make(map[string]bool, len(manifest))
	for _, entry := range manifest {
		if entry.Corpus == "" || entry.Name == "" || entry.Case.source == "" || entry.Case.want == "" || entry.Hash == "" {
			t.Fatalf("incomplete parity case: %#v", entry)
		}
		id := entry.Corpus + "/" + entry.Name
		if seenIDs[id] {
			t.Fatalf("duplicate parity case %q", id)
		}
		seenIDs[id] = true
		sum := sha256.Sum256([]byte(entry.Case.source))
		if got := hex.EncodeToString(sum[:]); got != entry.Hash {
			t.Fatalf("source hash changed for %s: got %s want %s", id, got, entry.Hash)
		}
	}
	if _, err := parityCaseSelection("top10/arithmetic_for"); err != nil {
		t.Fatal(err)
	}
	arithmetic, err := parityCaseSelection("arithmetic_for")
	if err != nil {
		t.Fatal(err)
	}
	if len(arithmetic) != 1 || arithmetic[0].name != "arithmetic_for" || arithmetic[0].want != "1595" || arithmetic[0].source != top10LuauCases[0].source {
		t.Fatalf("explicit arithmetic_for selection = %#v", arithmetic)
	}
	if _, err := parityCaseSelection("scenario/arithmetic_for"); err == nil {
		t.Fatal("accepted unknown qualified corpus/name")
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
	if !paritySystemSampleClean(paritySystemSample{Load: 250.0, CPU: 250.0}) {
		t.Fatal("rejected clean system sample")
	}
	for _, contaminated := range []paritySystemSample{
		{Load: 1, CPU: parityCPUMax + 0.01},
		{Load: math.NaN(), CPU: 50},
		{Load: 1, CPU: math.Inf(1)},
	} {
		if paritySystemSampleClean(contaminated) {
			t.Fatalf("accepted contaminated system sample: %+v", contaminated)
		}
	}
	externalCPU, err := parseParityProcessCPU("42 99.5\n7 125.25\n", 42)
	if err != nil || externalCPU != 125.25 {
		t.Fatalf("external process CPU = %v, %v; want 125.25", externalCPU, err)
	}
	if _, err := parseParityProcessCPU("42 nope\n", 42); err == nil {
		t.Fatal("accepted invalid process CPU sample")
	}
	pointSamples := []paritySystemSample{
		{Load: 1, CPU: parityCPUMax + 1},
		{Load: 1, CPU: 0},
		{Load: 1, CPU: parityCPUMax + 1},
		{Load: 1, CPU: 0},
		{Load: 1, CPU: 0},
	}
	sampleIndex := 0
	measureCount := 0
	waitCount := 0
	point, err := acquireCleanParityPoint(3, func() (paritySystemSample, error) {
		sample := pointSamples[sampleIndex]
		sampleIndex++
		return sample, nil
	}, func() ([]parityPointMeasurement, error) {
		measureCount++
		return []parityPointMeasurement{{elapsed: float64(measureCount)}}, nil
	}, func() {
		waitCount++
	})
	if err != nil || len(point) != 1 || point[0].elapsed != 2 || measureCount != 2 || waitCount != 2 {
		t.Fatalf("retried point = %#v, measures=%d waits=%d error=%v", point, measureCount, waitCount, err)
	}
	if _, err := acquireCleanParityPoint(2, func() (paritySystemSample, error) {
		return paritySystemSample{Load: 1, CPU: parityCPUMax + 1}, nil
	}, func() ([]parityPointMeasurement, error) {
		t.Fatal("measured a contaminated point")
		return nil, nil
	}, func() {}); err == nil {
		t.Fatal("accepted exhausted contaminated point")
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

	if source := parityCaseSource("return 7", 10); !strings.Contains(source, "return 7") || strings.Contains(source, "for __i") || !strings.Contains(source, "__parity_capture(__case)") {
		t.Fatalf("Ember public-call wrapper changed: %q", source)
	}
	luauSource := parityLuauCaseSource("return 7", 1000)
	warm := strings.Index(luauSource, "local __warm = __case()")
	start := strings.Index(luauSource, "local __start = os.clock()")
	timedCall := strings.Index(luauSource, "__sink = __case()")
	stop := strings.Index(luauSource, "local __elapsed_ns = (os.clock() - __start) * 1000000000")
	printElapsed := strings.Index(luauSource, "print(__elapsed_ns)")
	printResult := strings.Index(luauSource, "print(__sink)")
	if !(warm >= 0 && warm < start && start < timedCall && timedCall < stop && stop < printElapsed && printElapsed < printResult) || strings.Contains(luauSource[start:stop], "print(") {
		t.Fatalf("Luau public-call timer/output placement changed: %q", luauSource)
	}
	guestSource, err := parityGuestBatchSource("local x = 7\nreturn x + 1", parityDefaultFixtureVariant)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(guestSource, "__parity_capture(__batch)") != 1 ||
		!strings.Contains(guestSource, "(7 + (__seed % 3))") ||
		strings.Contains(guestSource, "(1 + (__seed % 3))") {
		t.Fatalf("Ember guest-batch wrapper changed: %q", guestSource)
	}
	guestLuauSource, err := parityGuestBatchLuauSource("local x = 7\nreturn x + 1", parityDefaultFixtureVariant)
	if err != nil {
		t.Fatal(err)
	}
	region, err := timerRegion(guestLuauSource)
	if err != nil || region != "\nlocal __sink = __batch(__n, __seed)\n" {
		t.Fatalf("Luau guest-batch timer changed: %q, %v", region, err)
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
	selected, err := parityManifestSelection(os.Getenv("RUNTIME_PARITY_CASES"))
	if err != nil {
		t.Fatal(err)
	}
	contract, err := parityContractForPhase(os.Getenv("RUNTIME_PARITY_PHASE"))
	if err != nil {
		t.Fatal(err)
	}
	role := os.Getenv("RUNTIME_PARITY_CAPTURE_ROLE")
	pair := os.Getenv("RUNTIME_PARITY_CAPTURE_PAIR")
	captureID := os.Getenv("RUNTIME_PARITY_CAPTURE_ID")
	sourceCommit := os.Getenv("RUNTIME_PARITY_SOURCE_COMMIT")
	environmentHash := os.Getenv("RUNTIME_PARITY_ENVIRONMENT_SHA256")
	executionMode := os.Getenv("RUNTIME_PARITY_EXECUTION_MODE")
	if (role != "frozen-current" && role != "candidate") ||
		(pair != "a" && pair != "b") ||
		(executionMode != "vm" && executionMode != "machine" && executionMode != "prepared" && executionMode != "prepared-native") ||
		captureID == "" ||
		!parityHexDigest(sourceCommit, 40, 64) ||
		!parityHexDigest(environmentHash, 64) {
		t.Fatal("runtime parity capture metadata is incomplete or invalid")
	}
	if contract.Phase == "prepared-parity1x" && executionMode != "prepared" {
		t.Fatal("prepared-parity1x requires prepared execution")
	}
	if contract.Phase != "prepared-parity1x" && executionMode == "prepared" {
		t.Fatal("dynamic parity phase cannot emit prepared evidence")
	}
	if contract.Phase == "prepared-native-parity15" && executionMode != "prepared-native" {
		t.Fatal("prepared-native-parity15 requires prepared-native execution")
	}
	if contract.Phase != "prepared-native-parity15" && executionMode == "prepared-native" {
		t.Fatal("non-native parity phase cannot emit prepared-native evidence")
	}
	rawPath, err := parityRawPath(os.Getenv("RUNTIME_PARITY_RAW"))
	if err != nil {
		t.Fatal(err)
	}
	slopePath := os.Getenv("RUNTIME_PARITY_SLOPES")
	if slopePath == "" {
		t.Fatal("RUNTIME_PARITY_SLOPES is required")
	}
	slopePath, err = filepath.Abs(slopePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(rawPath), 0o700); err != nil {
		t.Fatalf("create parity artifact directory: %v", err)
	}
	raw, err := os.OpenFile(rawPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("create parity raw output: %v", err)
	}
	defer raw.Close()
	slopes, err := os.OpenFile(slopePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("create parity slopes output: %v", err)
	}
	defer slopes.Close()
	writeRaw := func(format string, args ...any) {
		if _, err := fmt.Fprintf(raw, format, args...); err != nil {
			t.Fatalf("write parity raw output: %v", err)
		}
	}
	writeSlope := func(format string, args ...any) {
		if _, err := fmt.Fprintf(slopes, format, args...); err != nil {
			t.Fatalf("write parity slopes output: %v", err)
		}
	}
	writeRaw("%s\n", parityRawHeader)
	writeSlope("%s\n", paritySlopeHeader)

	for _, entry := range selected {
		tc := entry.Case
		programSource, seededSource, err := runtimeParityGuestBatchProgram(tc.source, parityDefaultFixtureVariant)
		if err != nil {
			t.Fatalf("%s seed guest workload: %v", tc.name, err)
		}
		emberSource, err := parityGuestBatchSource(tc.source, parityDefaultFixtureVariant)
		if err != nil {
			t.Fatalf("%s create Ember guest workload: %v", tc.name, err)
		}
		luauSource, err := parityGuestBatchLuauSource(tc.source, parityDefaultFixtureVariant)
		if err != nil {
			t.Fatalf("%s create Luau guest workload: %v", tc.name, err)
		}
		var owner *parityPreparedCallable
		if executionMode == "prepared" {
			owner, err = prepareParityExactGuestBatch(programSource + "return " + parityDefaultFixtureVariant.batchName + "\n")
		} else if executionMode == "prepared-native" {
			owner, err = prepareParityNativeGuestBatch(
				programSource+"return "+parityDefaultFixtureVariant.batchName+"\n",
				"parity/"+entry.Corpus+"/"+entry.Name,
			)
		} else {
			owner, err = prepareParityEmberRuntimeNamed(emberSource, "parity/"+entry.Corpus+"/"+entry.Name)
		}
		if err != nil {
			t.Fatalf("%s prepare Ember guest runtime: %v", tc.name, err)
		}
		warm, err := owner.callBatch(1, parityCaptureSeed)
		if err != nil {
			_ = owner.close()
			t.Fatalf("%s warm Ember guest batch: %v", tc.name, err)
		}
		if _, err := parityExactIntegerResult(warm); err != nil {
			_ = owner.close()
			t.Fatalf("%s warm Ember guest batch: %v", tc.name, err)
		}
		scriptPath := filepath.Join(filepath.Dir(rawPath), "scripts", entry.Corpus+"-"+tc.name+".luau")
		if err := os.MkdirAll(filepath.Dir(scriptPath), 0o700); err != nil {
			_ = owner.close()
			t.Fatalf("%s create script directory: %v", tc.name, err)
		}
		if err := os.WriteFile(scriptPath, []byte(luauSource), 0o700); err != nil {
			_ = owner.close()
			t.Fatalf("%s write Luau script: %v", tc.name, err)
		}
		timings := map[string]map[int]map[int]float64{"ember": {}, "luau": {}}
		results := map[string]map[int]map[int]string{"ember": {}, "luau": {}}
		workloadHash := parityStringSHA256(seededSource)
		programHash := parityStringSHA256(programSource)
		pairIndex := 1
		if pair == "b" {
			pairIndex = 2
		}
		for repeat := 1; repeat <= parityRepeatCount; repeat++ {
			timings["ember"][repeat] = make(map[int]float64, len(parityIterations))
			timings["luau"][repeat] = make(map[int]float64, len(parityIterations))
			results["ember"][repeat] = make(map[int]string, len(parityIterations))
			results["luau"][repeat] = make(map[int]string, len(parityIterations))
			for iterationIndex, n := range parityIterations {
				order := parityEngineOrderFor(pairIndex, repeat, iterationIndex)
				point, err := acquireCleanParityPoint(parityPointAttemptLimit, sampleParitySystem, func() ([]parityPointMeasurement, error) {
					measurements := make([]parityPointMeasurement, 0, len(order))
					for engineIndex, engine := range order {
						var elapsed float64
						var result string
						var measureErr error
						switch engine {
						case "ember":
							elapsed, result, measureErr = measureParityEmberGuestBatch(owner, n, parityCaptureSeed)
						case "luau":
							elapsed, result, measureErr = measureParityLuauGuestBatch(environment.LuauPath, scriptPath, n, parityCaptureSeed)
						default:
							return nil, fmt.Errorf("unknown parity engine %q", engine)
						}
						if measureErr != nil {
							return nil, fmt.Errorf("engine=%s: %w", engine, measureErr)
						}
						if elapsed <= 0 || !finiteParityFloat(elapsed) {
							return nil, fmt.Errorf("engine=%s: invalid timing %v", engine, elapsed)
						}
						if err := parityValidateIntegerString(result); err != nil {
							return nil, fmt.Errorf("engine=%s: %w", engine, err)
						}
						measurements = append(measurements, parityPointMeasurement{
							engineIndex: engineIndex,
							engine:      engine,
							elapsed:     elapsed,
							result:      result,
						})
					}
					if len(measurements) != 2 || measurements[0].result != measurements[1].result {
						return nil, fmt.Errorf("guest result mismatch: %s=%q %s=%q", measurements[0].engine, measurements[0].result, measurements[1].engine, measurements[1].result)
					}
					return measurements, nil
				}, func() {
					time.Sleep(parityPointRetryDelay)
				})
				if err != nil {
					t.Fatalf("%s repeat=%d N=%d: %v", tc.name, repeat, n, err)
				}
				pointRows := make([]string, 0, len(point))
				for _, measurement := range point {
					engine := measurement.engine
					elapsed := measurement.elapsed
					timings[engine][repeat][n] = elapsed
					results[engine][repeat][n] = measurement.result
					acquisitionOrder := (repeat-1)*len(parityIterations)*2 + parityOrderForRepeat(repeat, measurement.engineIndex, iterationIndex)
					pointRows = append(pointRows, fmt.Sprintf("2\t%s\t%s\t%s\t%s\t%s\t%s\t%s\tguest_batch\t%s\t%s\t%d\t%d\t%d\t%d\t%.17g\t%s\t%s\t%s\t%s\t",
						contract.Phase,
						role,
						pair,
						captureID,
						sourceCommit,
						entry.Corpus,
						entry.Name,
						executionMode,
						engine,
						repeat,
						acquisitionOrder,
						n,
						parityCaptureSeed,
						elapsed,
						measurement.result,
						workloadHash,
						programHash,
						environmentHash,
					))
				}
				for _, row := range pointRows {
					writeRaw("%s0\n", row)
				}
			}
		}
		for _, engine := range []string{"ember", "luau"} {
			for repeat := 1; repeat <= parityRepeatCount; repeat++ {
				fit, err := fitParityLine(timings[engine][repeat])
				if err != nil {
					t.Fatalf("%s %s repeat=%d fit: %v", tc.name, engine, repeat, err)
				}
				resultSetHash, err := parityResultSetSHA256(results[engine][repeat])
				if err != nil {
					t.Fatalf("%s %s repeat=%d result set: %v", tc.name, engine, repeat, err)
				}
				writeSlope("2\t%s\t%s\t%s\t%s\t%s\t%s\t%s\tguest_batch\t%s\t%s\t%d\t%.17g\t%.17g\t%s\t%s\t%s\t%s\n",
					contract.Phase,
					role,
					pair,
					captureID,
					sourceCommit,
					entry.Corpus,
					entry.Name,
					executionMode,
					engine,
					repeat,
					fit.Inner,
					fit.Entry,
					resultSetHash,
					workloadHash,
					programHash,
					environmentHash,
				)
			}
		}
		if err := owner.close(); err != nil {
			t.Fatalf("%s close warmed callable: %v", tc.name, err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close parity raw output: %v", err)
	}
	if err := slopes.Close(); err != nil {
		t.Fatalf("close parity slopes output: %v", err)
	}
}

func parityHexDigest(value string, lengths ...int) bool {
	validLength := false
	for _, length := range lengths {
		validLength = validLength || len(value) == length
	}
	if !validLength {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func parityStringSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestRuntimeParityCommandVarargWrapperKeepsCallerRegistersAfterNestedGrowth(t *testing.T) {
	selected, err := parityCaseSelection("command_vararg_router")
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 {
		t.Fatalf("selected %d command cases, want 1", len(selected))
	}
	callable, err := prepareParityEmberRuntime(parityCaseSource(selected[0].source, 1))
	if err != nil {
		t.Fatalf("prepare wrapped command case: %v", err)
	}
	t.Cleanup(func() {
		if err := callable.close(); err != nil {
			t.Errorf("close wrapped command case: %v", err)
		}
	})
	results, err := callable.callback.Call(context.Background())
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

func TestRuntimeParityGuestBatchAll37(t *testing.T) {
	seeds := []int64{0, 1, 7, 29}
	for _, entry := range parityCaseManifest() {
		entry := entry
		t.Run(entry.Corpus+"/"+entry.Name, func(t *testing.T) {
			source, err := parityGuestBatchSource(entry.Case.source, parityDefaultFixtureVariant)
			if err != nil {
				t.Fatal(err)
			}
			callable, err := prepareParityEmberRuntimeNamed(source, "parity-seeded/"+entry.Corpus+"/"+entry.Name)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := callable.close(); err != nil {
					t.Errorf("close guest batch: %v", err)
				}
			}()
			seen := make(map[string]bool, len(seeds))
			for _, seed := range seeds {
				values, err := callable.callback.Call(
					context.Background(),
					ember.NumberValue(3),
					ember.NumberValue(float64(seed)),
				)
				if err != nil {
					t.Fatalf("seed=%d: %v", seed, err)
				}
				result, err := parityExactIntegerResult(values)
				if err != nil {
					t.Fatalf("seed=%d: %v", seed, err)
				}
				seen[result] = true
			}
			if len(seen) < 2 {
				t.Fatalf("runtime seed did not change the guest result: %v", seen)
			}
		})
	}
}

func TestRuntimeParitySeededManifest(t *testing.T) {
	hash := sha256.New()
	fmt.Fprintln(hash, paritySeedVersion)
	for _, entry := range parityCaseManifest() {
		program, seeded, err := runtimeParityGuestBatchProgram(entry.Case.source, parityDefaultFixtureVariant)
		if err != nil {
			t.Fatalf("%s/%s: %v", entry.Corpus, entry.Name, err)
		}
		fmt.Fprintf(hash, "%s/%s\t%s\t%s\n",
			entry.Corpus,
			entry.Name,
			parityStringSHA256(seeded),
			parityStringSHA256(program),
		)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != paritySeedHash {
		t.Fatalf("seeded all-37 manifest hash = %s, want %s", got, paritySeedHash)
	}
}

func TestRuntimeParityIdentityHoldoutAll37(t *testing.T) {
	for _, entry := range parityCaseManifest() {
		entry := entry
		t.Run(entry.Corpus+"/"+entry.Name, func(t *testing.T) {
			standardSource, err := parityGuestBatchSource(entry.Case.source, parityDefaultFixtureVariant)
			if err != nil {
				t.Fatal(err)
			}
			holdoutSource, err := parityGuestBatchSource(entry.Case.source, parityHoldoutFixtureVariant)
			if err != nil {
				t.Fatal(err)
			}
			if standardSource == holdoutSource {
				t.Fatal("identity holdout source did not change")
			}
			standard, err := prepareParityEmberRuntimeIdentity(
				standardSource,
				"parity-standard/"+entry.Corpus+"/"+entry.Name,
				"standard-"+entry.Name,
				"standard-entry",
			)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := standard.close(); err != nil {
					t.Errorf("close standard: %v", err)
				}
			}()
			holdout, err := prepareParityEmberRuntimeIdentity(
				holdoutSource,
				"parity-holdout/"+entry.Corpus+"/"+entry.Name,
				"holdout-"+entry.Name,
				"holdout-entry",
			)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := holdout.close(); err != nil {
					t.Errorf("close holdout: %v", err)
				}
			}()
			for _, seed := range []int64{1, 7, 29} {
				standardValues, err := standard.callback.Call(
					context.Background(),
					ember.NumberValue(3),
					ember.NumberValue(float64(seed)),
				)
				if err != nil {
					t.Fatalf("standard seed=%d: %v", seed, err)
				}
				holdoutValues, err := holdout.callback.Call(
					context.Background(),
					ember.NumberValue(3),
					ember.NumberValue(float64(seed)),
				)
				if err != nil {
					t.Fatalf("holdout seed=%d: %v", seed, err)
				}
				standardResult, err := parityExactIntegerResult(standardValues)
				if err != nil {
					t.Fatal(err)
				}
				holdoutResult, err := parityExactIntegerResult(holdoutValues)
				if err != nil {
					t.Fatal(err)
				}
				if standardResult != holdoutResult {
					t.Fatalf("seed=%d standard=%s holdout=%s", seed, standardResult, holdoutResult)
				}
			}
		})
	}
}

func TestRuntimeParityGuestBatchAll37MatchLuau(t *testing.T) {
	if os.Getenv("EMBER_RUNTIME_PARITY_SEEDED_LIVE") != "1" {
		t.Skip("set EMBER_RUNTIME_PARITY_SEEDED_LIVE=1 to compare seeded all-37 workloads with Luau")
	}
	environment, err := inspectParityEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	scriptDir := t.TempDir()
	for _, entry := range parityCaseManifest() {
		entry := entry
		t.Run(entry.Corpus+"/"+entry.Name, func(t *testing.T) {
			emberSource, err := parityGuestBatchSource(entry.Case.source, parityDefaultFixtureVariant)
			if err != nil {
				t.Fatal(err)
			}
			luauSource, err := parityGuestBatchLuauSource(entry.Case.source, parityDefaultFixtureVariant)
			if err != nil {
				t.Fatal(err)
			}
			callable, err := prepareParityEmberRuntimeNamed(emberSource, "parity-live/"+entry.Corpus+"/"+entry.Name)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := callable.close(); err != nil {
					t.Errorf("close guest batch: %v", err)
				}
			}()
			scriptPath := filepath.Join(scriptDir, entry.Corpus+"-"+entry.Name+".luau")
			if err := os.WriteFile(scriptPath, []byte(luauSource), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, seed := range []int64{0, 1, 7, 29} {
				values, err := callable.callback.Call(
					context.Background(),
					ember.NumberValue(3),
					ember.NumberValue(float64(seed)),
				)
				if err != nil {
					t.Fatalf("Ember seed=%d: %v", seed, err)
				}
				emberResult, err := parityExactIntegerResult(values)
				if err != nil {
					t.Fatalf("Ember seed=%d: %v", seed, err)
				}
				_, luauResult, err := measureParityLuauGuestBatch(environment.LuauPath, scriptPath, 3, seed)
				if err != nil {
					t.Fatalf("Luau seed=%d: %v", seed, err)
				}
				if emberResult != luauResult {
					t.Fatalf("seed=%d Ember=%s Luau=%s", seed, emberResult, luauResult)
				}
			}
		})
	}
}

// BenchmarkRuntimeParityGuestBatchVM exposes the allocation and GC pressure
// hidden by one-call warmed benchmarks. It uses the same dynamically compiled,
// owner-bound guest batch as the slope capture, but does not invoke Luau.
func BenchmarkRuntimeParityGuestBatchVM(b *testing.B) {
	for _, entry := range parityCaseManifest() {
		entry := entry
		b.Run(entry.Corpus+"/"+entry.Name, func(b *testing.B) {
			source, err := parityGuestBatchSource(entry.Case.source, parityDefaultFixtureVariant)
			if err != nil {
				b.Fatal(err)
			}
			callable, err := prepareParityEmberRuntimeNamed(source, "parity-benchmark/"+entry.Corpus+"/"+entry.Name)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() {
				if err := callable.close(); err != nil {
					b.Errorf("close guest batch: %v", err)
				}
			})

			for _, guestIterations := range []int{1, 100, 1000} {
				guestIterations := guestIterations
				b.Run("n="+strconv.Itoa(guestIterations), func(b *testing.B) {
					arguments := []ember.Value{
						ember.NumberValue(float64(guestIterations)),
						ember.NumberValue(float64(parityCaptureSeed)),
					}
					if _, err := callable.callback.Call(context.Background(), arguments...); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						result, err := callable.callback.Call(context.Background(), arguments...)
						if err != nil {
							b.Fatal(err)
						}
						benchmarkEmberResultsSink = result
					}
				})
			}
		})
	}
}

func parityIterationString() string {
	values := make([]string, len(parityIterations))
	for i, n := range parityIterations {
		values[i] = strconv.Itoa(n)
	}
	return strings.Join(values, ",")
}
