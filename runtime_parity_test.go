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
)

const (
	parityLuauSHA256  = "c921fa51dbc0d81f9acbddcfa9208aa58f039388301f9fba77d2c5a324cb42bd"
	parityLuauVersion = "0.728"
	parityPlatform    = "Darwin 24.6.0 arm64"
	parityCPU         = "Apple M1"
	parityRawHeader   = "schema_version\tcapture_role\tcapture_pair\tcapture_id\tsource_commit\tcorpus\tname\tlifecycle\tengine\trepeat\tacquisition_order\tn_calls\telapsed_ns\tresult_sha256\tcallable_scope\tenvironment_sha256\tcontaminated"
	paritySlopeHeader = "schema_version\tcapture_role\tcapture_pair\tcapture_id\tsource_commit\tcorpus\tname\tlifecycle\tengine\trepeat\tslope_ns_per_call\tintercept_ns\tresult_sha256\tcallable_scope\tenvironment_sha256"
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

func parityCaseFunction(source string) string {
	return fmt.Sprintf("local __case = function()\n%s\nend\n", source)
}

func parityCaseLoop(iterations int) string {
	return fmt.Sprintf("for __i = 1, %d do\n    __result = __case()\nend\n", iterations)
}

func parityCaseSource(source string, iterations int) string {
	return runtimeParityTimerFixture(source, iterations, false)
}

func parityLuauCaseSource(source string, iterations int) string {
	return runtimeParityTimerFixture(source, iterations, true)
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

type parityModuleLoader map[string]string

func (loader parityModuleLoader) LoadModule(ctx context.Context, id ember.ModuleID) (ember.Source, error) {
	if err := ctx.Err(); err != nil {
		return ember.Source{}, err
	}
	text, ok := loader[id.String()]
	if !ok {
		return ember.Source{}, fmt.Errorf("parity runner: missing module %s", id)
	}
	return ember.Source{Name: id.String(), Text: text}, nil
}

type parityPreparedCallable struct {
	owner    *ember.Runtime
	callback ember.Callback
}

func prepareParityEmberRuntime(source string) (*parityPreparedCallable, error) {
	entry := ember.LogicalModule("parity/case")
	program, _, err := ember.LoadProgram(context.Background(), parityModuleLoader{entry.String(): source}, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "parity", Module: entry}},
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
	return errors.Join(callable.callback.Close(), callable.owner.Close())
}

type parityTimedCall func() ([]ember.Value, error)

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

func measureParityEmber(callable *parityPreparedCallable, iterations int) (float64, string, error) {
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

	if source := parityCaseSource("return 7", 10); !strings.Contains(source, "return 7") || strings.Contains(source, "for __i") || !strings.Contains(source, "__parity_capture(__case)") {
		t.Fatalf("Ember parity wrapper changed: %q", source)
	}
	luauSource := parityLuauCaseSource("return 7", 1000)
	warm := strings.Index(luauSource, "local __warm = __case()")
	start := strings.Index(luauSource, "local __start = os.clock()")
	timedCall := strings.Index(luauSource, "__sink = __case()")
	stop := strings.Index(luauSource, "local __elapsed_ns = (os.clock() - __start) * 1000000000")
	printElapsed := strings.Index(luauSource, "print(__elapsed_ns)")
	printResult := strings.Index(luauSource, "print(__sink)")
	if !(warm >= 0 && warm < start && start < timedCall && timedCall < stop && stop < printElapsed && printElapsed < printResult) || strings.Contains(luauSource[start:stop], "print(") {
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
	role := os.Getenv("RUNTIME_PARITY_CAPTURE_ROLE")
	pair := os.Getenv("RUNTIME_PARITY_CAPTURE_PAIR")
	captureID := os.Getenv("RUNTIME_PARITY_CAPTURE_ID")
	sourceCommit := os.Getenv("RUNTIME_PARITY_SOURCE_COMMIT")
	environmentHash := os.Getenv("RUNTIME_PARITY_ENVIRONMENT_SHA256")
	if (role != "frozen-current" && role != "candidate") || (pair != "a" && pair != "b") || captureID == "" || !parityHexDigest(sourceCommit, 40, 64) || !parityHexDigest(environmentHash, 64) {
		t.Fatal("runtime parity capture metadata is incomplete or invalid")
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
		owners := make(map[int]*parityPreparedCallable, len(parityIterations))
		scripts := make(map[int]string, len(parityIterations))
		for _, n := range parityIterations {
			owner, err := prepareParityEmberRuntime(parityCaseSource(tc.source, n))
			if err != nil {
				t.Fatalf("%s prepare Ember runtime N=%d: %v", tc.name, n, err)
			}
			owners[n] = owner
			warm, err := owner.callback.Call(context.Background())
			if err != nil {
				t.Fatalf("%s warm Ember callable N=%d: %v", tc.name, n, err)
			}
			warmResult, err := parityScalarString(warm)
			if err != nil || warmResult != tc.want {
				t.Fatalf("%s warm Ember callable N=%d result=%q error=%v, want %q", tc.name, n, warmResult, err, tc.want)
			}
			scriptPath := filepath.Join(filepath.Dir(rawPath), "scripts", entry.Corpus+"-"+tc.name+"-"+strconv.Itoa(n)+".luau")
			if err := os.MkdirAll(filepath.Dir(scriptPath), 0o700); err != nil {
				t.Fatalf("%s create script directory: %v", tc.name, err)
			}
			if err := os.WriteFile(scriptPath, []byte(parityLuauCaseSource(tc.source, n)), 0o700); err != nil {
				t.Fatalf("%s write Luau script: %v", tc.name, err)
			}
			scripts[n] = scriptPath
		}
		timings := map[string]map[int]map[int]float64{"ember": {}, "luau": {}}
		resultHash := parityStringSHA256(tc.want)
		for repeat := 1; repeat <= parityRepeatCount; repeat++ {
			timings["ember"][repeat] = make(map[int]float64, len(parityIterations))
			timings["luau"][repeat] = make(map[int]float64, len(parityIterations))
			for iterationIndex, n := range parityIterations {
				before, err := sampleParitySystem()
				if err != nil {
					t.Fatalf("%s repeat=%d N=%d before sample: %v", tc.name, repeat, n, err)
				}
				if !paritySystemSampleClean(before) {
					t.Fatalf("%s repeat=%d N=%d before sample contaminated: load=%.6f cpu=%.6f", tc.name, repeat, n, before.Load, before.CPU)
				}
				order := parityEngineOrderFor(1, repeat, iterationIndex)
				pointRows := make([]string, 0, 2)
				for engineIndex, engine := range order {
					var elapsed float64
					var result string
					switch engine {
					case "ember":
						elapsed, result, err = measureParityEmber(owners[n], n)
					case "luau":
						elapsed, result, err = measureParityLuau(environment.LuauPath, scripts[n])
					default:
						t.Fatalf("unknown parity engine %q", engine)
					}
					if err != nil {
						t.Fatalf("%s repeat=%d engine=%s N=%d: %v", tc.name, repeat, engine, n, err)
					}
					if elapsed <= 0 || !finiteParityFloat(elapsed) {
						t.Fatalf("%s repeat=%d engine=%s N=%d: invalid timing %v", tc.name, repeat, engine, n, elapsed)
					}
					if result != tc.want {
						t.Fatalf("%s repeat=%d engine=%s N=%d: result %q, want %q", tc.name, repeat, engine, n, result, tc.want)
					}
					timings[engine][repeat][n] = elapsed
					acquisitionOrder := (repeat-1)*len(parityIterations)*2 + parityOrderForRepeat(repeat, engineIndex, iterationIndex)
					pointRows = append(pointRows, fmt.Sprintf("1\t%s\t%s\t%s\t%s\t%s\t%s\twarm_call\t%s\t%d\t%d\t%d\t%.17g\t%s\twarmed_callable_v1\t%s\t", role, pair, captureID, sourceCommit, entry.Corpus, entry.Name, engine, repeat, acquisitionOrder, n, elapsed, resultHash, environmentHash))
				}
				after, err := sampleParitySystem()
				if err != nil {
					t.Fatalf("%s repeat=%d N=%d after sample: %v", tc.name, repeat, n, err)
				}
				if !paritySystemSampleClean(after) {
					t.Fatalf("%s repeat=%d N=%d after sample contaminated: load=%.6f cpu=%.6f", tc.name, repeat, n, after.Load, after.CPU)
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
				writeSlope("1\t%s\t%s\t%s\t%s\t%s\t%s\twarm_call\t%s\t%d\t%.17g\t%.17g\t%s\twarmed_callable_v1\t%s\n", role, pair, captureID, sourceCommit, entry.Corpus, entry.Name, engine, repeat, fit.Inner, fit.Entry, resultHash, environmentHash)
			}
		}
		for _, owner := range owners {
			if err := owner.close(); err != nil {
				t.Fatalf("%s close warmed callable: %v", tc.name, err)
			}
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

func parityIterationString() string {
	values := make([]string, len(parityIterations))
	for i, n := range parityIterations {
		values[i] = strconv.Itoa(n)
	}
	return strings.Join(values, ",")
}
