package ember_test

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/besmpl/ember/internal/preparedworkerfixture"
	"github.com/besmpl/ember/internal/preparedworkerprobe"
)

const (
	preparedWorkerLatencySampleCount = 4096
	preparedWorkerHostPointTrials    = 3
	preparedWorkerFrameBudget        = time.Second / 60
	preparedWorkerExchangeBudget     = preparedWorkerFrameBudget / 10
	preparedWorkerDevSlopeBudget     = 1.50
	preparedWorkerRSSSafetyCeiling   = 512 << 20
)

var preparedWorkerHostWork = [...]int{200, 2000, 10_000, 20_000}

type preparedWorkerCaptureContext struct {
	ID              string
	Pair            string
	SourceCommit    string
	EnvironmentHash string
	Output          string
	LuauPath        string
}

type preparedWorkerHostSummary struct {
	SemanticMatch      bool
	ExchangePhases     int
	WorkerEmbeddedMax  float64
	EmbeddedLatencyP50 time.Duration
	EmbeddedLatencyP95 time.Duration
	EmbeddedLatencyP99 time.Duration
	WorkerLatencyP50   time.Duration
	WorkerLatencyP95   time.Duration
	WorkerLatencyP99   time.Duration
	ExchangeCostP50    time.Duration
	ExchangeCostP95    time.Duration
	ExchangeCostP99    time.Duration
	Wall               time.Duration
	ParentCPU          time.Duration
	ChildCPU           time.Duration
	ParentPeakRSS      uint64
	ChildPeakRSS       uint64
}

func TestPreparedWorkerHostAdmissionGate(t *testing.T) {
	passing := preparedWorkerHostSummary{
		SemanticMatch:      true,
		ExchangePhases:     1,
		WorkerEmbeddedMax:  1.49,
		EmbeddedLatencyP50: 100 * time.Microsecond,
		EmbeddedLatencyP95: 150 * time.Microsecond,
		EmbeddedLatencyP99: 200 * time.Microsecond,
		WorkerLatencyP50:   150 * time.Microsecond,
		WorkerLatencyP95:   200 * time.Microsecond,
		WorkerLatencyP99:   250 * time.Microsecond,
		ExchangeCostP50:    50 * time.Microsecond,
		ExchangeCostP95:    100 * time.Microsecond,
		ExchangeCostP99:    preparedWorkerExchangeBudget,
		Wall:               time.Second,
		ParentCPU:          250 * time.Millisecond,
		ChildCPU:           500 * time.Millisecond,
		ParentPeakRSS:      64 << 20,
		ChildPeakRSS:       32 << 20,
	}
	if err := preparedWorkerHostAdmissionGate(passing); err != nil {
		t.Fatalf("passing host admission failed: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*preparedWorkerHostSummary)
	}{
		{name: "semantic mismatch", mutate: func(summary *preparedWorkerHostSummary) { summary.SemanticMatch = false }},
		{name: "extra phase", mutate: func(summary *preparedWorkerHostSummary) { summary.ExchangePhases = 2 }},
		{name: "slope", mutate: func(summary *preparedWorkerHostSummary) {
			summary.WorkerEmbeddedMax = preparedWorkerDevSlopeBudget + 0.001
		}},
		{name: "embedded latency", mutate: func(summary *preparedWorkerHostSummary) { summary.EmbeddedLatencyP95 = 50 * time.Microsecond }},
		{name: "worker latency", mutate: func(summary *preparedWorkerHostSummary) { summary.WorkerLatencyP99 = 0 }},
		{name: "exchange latency", mutate: func(summary *preparedWorkerHostSummary) { summary.ExchangeCostP95 = summary.ExchangeCostP99 + 1 }},
		{name: "exchange p99", mutate: func(summary *preparedWorkerHostSummary) { summary.ExchangeCostP99++ }},
		{name: "parent RSS", mutate: func(summary *preparedWorkerHostSummary) { summary.ParentPeakRSS = preparedWorkerRSSSafetyCeiling + 1 }},
		{name: "child RSS", mutate: func(summary *preparedWorkerHostSummary) { summary.ChildPeakRSS = preparedWorkerRSSSafetyCeiling + 1 }},
		{name: "parent CPU", mutate: func(summary *preparedWorkerHostSummary) { summary.ParentCPU = 3 * time.Second }},
		{name: "child CPU", mutate: func(summary *preparedWorkerHostSummary) { summary.ChildCPU = 3 * time.Second }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			summary := passing
			test.mutate(&summary)
			if err := preparedWorkerHostAdmissionGate(summary); err == nil {
				t.Fatal("invalid host admission passed")
			}
		})
	}
}

func TestPreparedWorkerDurationQuantile(t *testing.T) {
	samples := []time.Duration{5, 1, 4, 2, 3}
	if got := preparedWorkerDurationQuantile(samples, 0.50); got != 3 {
		t.Fatalf("p50 = %s, want 3ns", got)
	}
	if got := preparedWorkerDurationQuantile(samples, 0.99); got != 5 {
		t.Fatalf("p99 = %s, want 5ns", got)
	}
}

func capturePreparedWorkerHostAdmission(t *testing.T, capture preparedWorkerCaptureContext) {
	t.Helper()
	embeddedTrace := runPreparedWorkerTrace(t, newPreparedWorkerHandler(t))
	worker := startPreparedWorkerProcess(t)
	workerTrace := runPreparedWorkerTrace(t, worker)
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	luauTrace := runPreparedWorkerLuauTrace(t, capture.Output, capture.LuauPath)
	semanticMatch := luauTrace == embeddedTrace && workerTrace == embeddedTrace
	writePreparedWorkerSemanticEvidence(t, capture, embeddedTrace, workerTrace, luauTrace, semanticMatch)
	if !semanticMatch {
		t.Fatalf(
			"host traces differ\nembedded:\n%s\nworker:\n%s\nLuau language oracle:\n%s",
			embeddedTrace,
			workerTrace,
			luauTrace,
		)
	}

	slopeMax := capturePreparedWorkerHostSlopes(t, capture)
	latency := capturePreparedWorkerLatency(t, capture)
	capturePreparedWorkerCrossingScaling(t, capture)
	latency.SemanticMatch = true
	latency.ExchangePhases = 1
	latency.WorkerEmbeddedMax = slopeMax
	gateErr := preparedWorkerHostAdmissionGate(latency)
	writePreparedWorkerHostSummary(t, capture, latency, gateErr)
	if gateErr != nil {
		t.Fatal(gateErr)
	}
}

func preparedWorkerHostAdmissionGate(summary preparedWorkerHostSummary) error {
	if !summary.SemanticMatch {
		return fmt.Errorf("prepared worker host gate: semantic trace mismatch")
	}
	if summary.ExchangePhases != 1 {
		return fmt.Errorf("prepared worker host gate: ordinary turn uses %d phases, want 1", summary.ExchangePhases)
	}
	if summary.WorkerEmbeddedMax <= 0 || summary.WorkerEmbeddedMax > preparedWorkerDevSlopeBudget || math.IsNaN(summary.WorkerEmbeddedMax) {
		return fmt.Errorf(
			"prepared worker host gate: worker/embedded development slope %.6f exceeds %.2f",
			summary.WorkerEmbeddedMax,
			preparedWorkerDevSlopeBudget,
		)
	}
	if !preparedWorkerValidLatencyQuantiles(
		summary.EmbeddedLatencyP50,
		summary.EmbeddedLatencyP95,
		summary.EmbeddedLatencyP99,
		false,
	) {
		return fmt.Errorf("prepared worker host gate: invalid embedded latency quantiles")
	}
	if !preparedWorkerValidLatencyQuantiles(
		summary.WorkerLatencyP50,
		summary.WorkerLatencyP95,
		summary.WorkerLatencyP99,
		false,
	) {
		return fmt.Errorf("prepared worker host gate: invalid worker latency quantiles")
	}
	if !preparedWorkerValidLatencyQuantiles(
		summary.ExchangeCostP50,
		summary.ExchangeCostP95,
		summary.ExchangeCostP99,
		true,
	) {
		return fmt.Errorf("prepared worker host gate: invalid exchange latency quantiles")
	}
	if summary.ExchangeCostP99 < 0 || summary.ExchangeCostP99 > preparedWorkerExchangeBudget {
		return fmt.Errorf(
			"prepared worker host gate: codec plus IPC p99 %s exceeds %s",
			summary.ExchangeCostP99,
			preparedWorkerExchangeBudget,
		)
	}
	if summary.ParentPeakRSS == 0 || summary.ParentPeakRSS > preparedWorkerRSSSafetyCeiling {
		return fmt.Errorf("prepared worker host gate: parent peak RSS %d exceeds safety ceiling %d", summary.ParentPeakRSS, preparedWorkerRSSSafetyCeiling)
	}
	if summary.ChildPeakRSS == 0 || summary.ChildPeakRSS > preparedWorkerRSSSafetyCeiling {
		return fmt.Errorf("prepared worker host gate: child peak RSS %d exceeds safety ceiling %d", summary.ChildPeakRSS, preparedWorkerRSSSafetyCeiling)
	}
	if summary.Wall <= 0 {
		return fmt.Errorf("prepared worker host gate: invalid wall time %s", summary.Wall)
	}
	cpuCeiling := 2*summary.Wall + 100*time.Millisecond
	if summary.ParentCPU < 0 || summary.ParentCPU > cpuCeiling {
		return fmt.Errorf("prepared worker host gate: parent CPU %s exceeds %s", summary.ParentCPU, cpuCeiling)
	}
	if summary.ChildCPU < 0 || summary.ChildCPU > cpuCeiling {
		return fmt.Errorf("prepared worker host gate: child CPU %s exceeds %s", summary.ChildCPU, cpuCeiling)
	}
	return nil
}

func preparedWorkerValidLatencyQuantiles(p50, p95, p99 time.Duration, zeroAllowed bool) bool {
	if p50 < 0 || p95 < p50 || p99 < p95 {
		return false
	}
	return zeroAllowed || p50 > 0
}

func newPreparedWorkerHandler(t testing.TB) *preparedworkerprobe.Handler {
	t.Helper()
	handler, err := preparedworkerprobe.NewHandler()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handler.Close(); err != nil {
			t.Error(err)
		}
	})
	return handler
}

func preparedWorkerTraceRequests() []preparedworkerfixture.TurnRequest {
	return []preparedworkerfixture.TurnRequest{
		{
			Sequence:   1,
			Revision:   0,
			Projection: preparedworkerfixture.Projection{Step: 1, Seed: 5, Work: 1},
			Events: []preparedworkerfixture.DamageEvent{{
				Route: preparedworkerfixture.RouteDamage, Entity: 7, Amount: 5,
			}},
		},
		{
			Sequence:   2,
			Revision:   1,
			State:      preparedworkerfixture.StateSnapshot{Tick: 1, Total: 6, ModuleCalls: 1},
			Projection: preparedworkerfixture.Projection{Step: 1, Seed: 6, Work: 1},
			Completions: []preparedworkerfixture.Completion{{
				EffectID: preparedWorkerEffectID(1, 1), Status: preparedworkerfixture.CompletionOK, Value: 11,
			}},
		},
		{
			Sequence:   3,
			Revision:   2,
			State:      preparedworkerfixture.StateSnapshot{Tick: 2, Total: 34, Ready: 11, Entity7: 5, ModuleCalls: 2},
			Projection: preparedworkerfixture.Projection{Step: 1, Seed: 4, Work: 1},
			Events: []preparedworkerfixture.DamageEvent{{
				Route: preparedworkerfixture.RouteDamage, Entity: 7, Amount: 4,
			}},
		},
		{
			Sequence:   4,
			Revision:   3,
			State:      preparedworkerfixture.StateSnapshot{Tick: 3, Total: 38, Ready: 11, Entity7: 5, ModuleCalls: 3},
			Projection: preparedworkerfixture.Projection{Step: 1, Seed: 3, Work: 1},
			Completions: []preparedworkerfixture.Completion{{
				EffectID: preparedWorkerEffectID(3, 1), Status: preparedworkerfixture.CompletionOK, Value: 2,
			}},
		},
	}
}

func preparedWorkerEffectID(sequence, ordinal uint64) uint64 {
	return sequence<<8 | ordinal
}

func runPreparedWorkerTrace(t testing.TB, transactor preparedworkerprobe.TurnTransactor) string {
	t.Helper()
	lines := make([]string, 0, len(preparedWorkerTraceRequests()))
	for _, request := range preparedWorkerTraceRequests() {
		result, err := transactor.Transact(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, canonicalPreparedWorkerTurn(result))
	}
	return strings.Join(lines, "\n")
}

func canonicalPreparedWorkerTurn(result preparedworkerfixture.TurnResult) string {
	parts := []string{
		strconv.FormatUint(result.Sequence, 10),
		strconv.FormatUint(result.Revision, 10),
		strconv.FormatInt(result.State.Tick, 10),
		strconv.FormatInt(result.State.Total, 10),
		strconv.FormatInt(result.State.Ready, 10),
		strconv.FormatInt(result.State.Entity7, 10),
		strconv.FormatInt(result.State.ModuleCalls, 10),
	}
	for _, command := range result.Commands {
		parts = append(parts, fmt.Sprintf("c,%d,%d,%d,%d", command.Kind, command.Entity, command.A, command.B))
	}
	for _, effect := range result.Effects {
		needsCompletion := 0
		if effect.NeedsCompletion {
			needsCompletion = 1
		}
		parts = append(parts, fmt.Sprintf(
			"e,%d,%d,%d,%d,%d",
			effect.ID,
			effect.Kind,
			effect.Entity,
			effect.Value,
			needsCompletion,
		))
	}
	for _, pending := range result.Pending {
		parts = append(parts, "p,"+strconv.FormatUint(pending, 10))
	}
	return strings.Join(parts, "|")
}

func runPreparedWorkerLuauTrace(t testing.TB, output, luauPath string) string {
	t.Helper()
	mainSource := strings.ReplaceAll(
		preparedworkerfixture.MainSource,
		`local dependency = require("./shared")`,
		"",
	)
	mainSource = strings.ReplaceAll(mainSource, `local compute = require("./numeric")`, "")
	source := fmt.Sprintf(
		preparedWorkerLuauTraceHarness,
		preparedworkerfixture.SharedSource,
		preparedworkerfixture.NumericSource,
		mainSource,
	)
	scriptDirectory := filepath.Join(output, "scripts")
	if err := os.MkdirAll(scriptDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(scriptDirectory, "host-turn-trace.luau")
	if err := os.WriteFile(scriptPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(luauPath, "-O2", scriptPath)
	combined, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run pinned Luau host trace: %v: %s", err, strings.TrimSpace(string(combined)))
	}
	return strings.TrimSpace(string(combined))
}

const preparedWorkerLuauTraceHarness = `
local callbacks = {}
local pending = {}
local commands = {}
local effects = {}
local currentSequence = 0
local nextEffectOrdinal = 1

local function capture(route, callback)
    callbacks[route] = callback
end

local function command(kind, entity, a, b)
    table.insert(commands, {kind, entity, a, b})
end

local function takeEffectID()
    local id = currentSequence * 256 + nextEffectOrdinal
    nextEffectOrdinal = nextEffectOrdinal + 1
    return id
end

local function effect(kind, entity, value)
    table.insert(effects, {takeEffectID(), kind, entity, value, 0})
end

local function wait(kind, entity)
    local id = takeEffectID()
    table.insert(effects, {id, kind, entity, 0, 1})
    return coroutine.yield(id)
end

local dependency = (function()
%s
end)()

local compute = (function()
%s
end)()

local main = (function()
%s
end)()

local function startEvent(event)
    local thread = coroutine.create(callbacks[event[1]])
    local ok, effectID = coroutine.resume(thread, event[2], event[3])
    if not ok then
        error(effectID)
    end
    if coroutine.status(thread) ~= "suspended" then
        error("event did not suspend")
    end
    pending[effectID] = {thread, event[2], event[3]}
end

local function emit(sequence, revision, tick, total, ready, entity7, moduleCalls)
    local parts = {
        tostring(sequence), tostring(revision), tostring(tick), tostring(total),
        tostring(ready), tostring(entity7), tostring(moduleCalls),
    }
    for _, item in ipairs(commands) do
        table.insert(parts, "c," .. table.concat(item, ","))
    end
    for _, item in ipairs(effects) do
        table.insert(parts, "e," .. table.concat(item, ","))
    end
    local ids = {}
    for id in pairs(pending) do
        table.insert(ids, id)
    end
    table.sort(ids)
    for _, id in ipairs(ids) do
        table.insert(parts, "p," .. tostring(id))
    end
    print(table.concat(parts, "|"))
end

local function transact(sequence, revision, tick, total, ready, entity7, moduleCalls, step, seed, work, event, completion)
    commands = {}
    effects = {}
    currentSequence = sequence
    nextEffectOrdinal = 1
    if completion then
        local retained = pending[completion[1]]
        pending[completion[1]] = nil
        local ok, loaded, totalDelta, entityDelta = coroutine.resume(retained[1], completion[2])
        if not ok then
            error(loaded)
        end
        if coroutine.status(retained[1]) ~= "dead" then
            error("completion suspended again")
        end
        ready = ready + loaded
        total = total + totalDelta
        entity7 = entity7 + entityDelta
    end
    if event then
        startEvent(event)
    end
    emit(sequence, revision, main.turn(tick, total, ready, entity7, moduleCalls, step, seed, work))
end

main.startup()
transact(1, 1, 0, 0, 0, 0, 0, 1, 5, 1, {1, 7, 5}, nil)
transact(2, 2, 1, 6, 0, 0, 1, 1, 6, 1, nil, {257, 11})
transact(3, 3, 2, 34, 11, 5, 2, 1, 4, 1, {1, 7, 4}, nil)
transact(4, 4, 3, 38, 11, 5, 3, 1, 3, 1, nil, {769, 2})
`

func writePreparedWorkerSemanticEvidence(
	t testing.TB,
	capture preparedWorkerCaptureContext,
	embedded string,
	worker string,
	luau string,
	match bool,
) {
	t.Helper()
	file := createPreparedWorkerCaptureFile(t, filepath.Join(capture.Output, "host-semantic.tsv"))
	defer file.Close()
	writePreparedWorkerCapture(t, file, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tengine\ttrace_sha256\tstatus\tenvironment_sha256\n")
	status := "FAIL"
	if match {
		status = "PASS"
	}
	for _, item := range []struct {
		engine string
		trace  string
	}{{"embedded", embedded}, {"worker", worker}, {"luau-language-oracle", luau}} {
		writePreparedWorkerCapture(t, file, "1\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			capture.ID,
			capture.Pair,
			capture.SourceCommit,
			item.engine,
			parityStringSHA256(item.trace),
			status,
			capture.EnvironmentHash,
		)
	}
}

func capturePreparedWorkerHostSlopes(t *testing.T, capture preparedWorkerCaptureContext) float64 {
	t.Helper()
	file := createPreparedWorkerCaptureFile(t, filepath.Join(capture.Output, "host-slopes.tsv"))
	defer file.Close()
	raw := createPreparedWorkerCaptureFile(t, filepath.Join(capture.Output, "host-slope-raw.tsv"))
	defer raw.Close()
	writePreparedWorkerCapture(t, file, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tengine\trepeat\tslope_ns_per_work\tintercept_ns\tresult_set_sha256\tenvironment_sha256\n")
	writePreparedWorkerCapture(t, raw, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tengine\trepeat\ttrial\torder\twork\telapsed_ns\tresult_sha256\tenvironment_sha256\n")

	var maximum float64
	for repeat := 1; repeat <= parityRepeatCount; repeat++ {
		embedded := newPreparedWorkerHandler(t)
		worker := startPreparedWorkerProcess(t)
		warm := preparedworkerfixture.TurnRequest{
			Sequence:   1,
			Revision:   0,
			Projection: preparedworkerfixture.Projection{Step: 1, Seed: 8, Work: 1},
		}
		want, err := embedded.Transact(context.Background(), warm)
		if err != nil {
			t.Fatal(err)
		}
		got, err := worker.Transact(context.Background(), warm)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("host slope warmup differs: worker=%#v embedded=%#v", got, want)
		}
		state := got.State

		timings := map[string]map[int]float64{
			"embedded": {},
			"worker":   {},
		}
		var resultDigests strings.Builder
		sequence := uint64(1)
		for index, work := range preparedWorkerHostWork {
			durations := map[string][]time.Duration{
				"embedded": make([]time.Duration, 0, preparedWorkerHostPointTrials),
				"worker":   make([]time.Duration, 0, preparedWorkerHostPointTrials),
			}
			for trial := 1; trial <= preparedWorkerHostPointTrials; trial++ {
				sequence++
				request := preparedworkerfixture.TurnRequest{
					Sequence:   sequence,
					Revision:   sequence - 1,
					State:      state,
					Projection: preparedworkerfixture.Projection{Step: 1, Seed: 8, Work: int64(work)},
				}
				order := [2]string{"embedded", "worker"}
				if (repeat+trial+index+preparedWorkerPairIndex(capture.Pair))%2 != 0 {
					order = [2]string{"worker", "embedded"}
				}
				measured := make(map[string]preparedworkerfixture.TurnResult, 2)
				for _, engine := range order {
					var transactor preparedworkerprobe.TurnTransactor = embedded
					if engine == "worker" {
						transactor = worker
					}
					start := time.Now()
					result, err := transactor.Transact(context.Background(), request)
					elapsed := time.Since(start)
					if err != nil {
						t.Fatalf("host slope %s repeat=%d trial=%d work=%d: %v", engine, repeat, trial, work, err)
					}
					durations[engine] = append(durations[engine], elapsed)
					measured[engine] = result
					writePreparedWorkerCapture(t, raw, "1\t%s\t%s\t%s\t%s\t%d\t%d\t%s,%s\t%d\t%d\t%s\t%s\n",
						capture.ID,
						capture.Pair,
						capture.SourceCommit,
						engine,
						repeat,
						trial,
						order[0],
						order[1],
						work,
						elapsed.Nanoseconds(),
						parityStringSHA256(canonicalPreparedWorkerTurn(result)),
						capture.EnvironmentHash,
					)
				}
				if !reflect.DeepEqual(measured["worker"], measured["embedded"]) {
					t.Fatalf("host slope repeat=%d trial=%d work=%d results differ", repeat, trial, work)
				}
				state = measured["worker"].State
				fmt.Fprintf(
					&resultDigests,
					"%d/%d=%s\n",
					work,
					trial,
					parityStringSHA256(canonicalPreparedWorkerTurn(measured["worker"])),
				)
			}
			for _, engine := range []string{"embedded", "worker"} {
				timings[engine][work] = float64(preparedWorkerDurationQuantile(durations[engine], 0.50).Nanoseconds())
			}
		}
		resultHash := parityStringSHA256(resultDigests.String())
		fits := make(map[string]parityFit, 2)
		for _, engine := range []string{"embedded", "worker"} {
			if err := validatePreparedWorkerHostWindow(timings[engine]); err != nil {
				t.Fatalf("host slope %s repeat=%d: %v", engine, repeat, err)
			}
			fit, err := fitPreparedWorkerHostLine(timings[engine])
			if err != nil {
				t.Fatal(err)
			}
			fits[engine] = fit
			writePreparedWorkerCapture(t, file, "1\t%s\t%s\t%s\t%s\t%d\t%.17g\t%.17g\t%s\t%s\n",
				capture.ID,
				capture.Pair,
				capture.SourceCommit,
				engine,
				repeat,
				fit.Inner,
				fit.Entry,
				resultHash,
				capture.EnvironmentHash,
			)
		}
		ratio := fits["worker"].Inner / fits["embedded"].Inner
		if ratio > maximum {
			maximum = ratio
		}
		if err := worker.Close(); err != nil {
			t.Fatal(err)
		}
		if err := embedded.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return maximum
}

func validatePreparedWorkerHostWindow(samples map[int]float64) error {
	if len(samples) != len(preparedWorkerHostWork) {
		return fmt.Errorf("host measurement window: got %d points, want %d", len(samples), len(preparedWorkerHostWork))
	}
	for _, work := range preparedWorkerHostWork {
		elapsed, ok := samples[work]
		if !ok || elapsed <= 0 || !finiteParityFloat(elapsed) {
			return fmt.Errorf("host measurement window: invalid work=%d timing %v", work, elapsed)
		}
	}
	maximumWork := preparedWorkerHostWork[len(preparedWorkerHostWork)-1]
	if samples[maximumWork] < float64((5 * time.Millisecond).Nanoseconds()) {
		return fmt.Errorf("host measurement window: work=%d elapsed %.0fns is below 5ms", maximumWork, samples[maximumWork])
	}
	return nil
}

func fitPreparedWorkerHostLine(samples map[int]float64) (parityFit, error) {
	if err := validatePreparedWorkerHostWindow(samples); err != nil {
		return parityFit{}, err
	}
	var meanWork float64
	var meanElapsed float64
	for _, work := range preparedWorkerHostWork {
		meanWork += float64(work)
		meanElapsed += samples[work]
	}
	meanWork /= float64(len(preparedWorkerHostWork))
	meanElapsed /= float64(len(preparedWorkerHostWork))
	var numerator float64
	var denominator float64
	for _, work := range preparedWorkerHostWork {
		deltaWork := float64(work) - meanWork
		numerator += deltaWork * (samples[work] - meanElapsed)
		denominator += deltaWork * deltaWork
	}
	if denominator <= 0 || !finiteParityFloat(denominator) {
		return parityFit{}, fmt.Errorf("host fit: invalid denominator %v", denominator)
	}
	slope := numerator / denominator
	intercept := meanElapsed - slope*meanWork
	if slope <= 0 || !finiteParityFloat(slope) || !finiteParityFloat(intercept) {
		return parityFit{}, fmt.Errorf("host fit: invalid intercept/slope %v/%v", intercept, slope)
	}
	return parityFit{Entry: intercept, Inner: slope}, nil
}

func capturePreparedWorkerLatency(t *testing.T, capture preparedWorkerCaptureContext) preparedWorkerHostSummary {
	t.Helper()
	exchangeSamples := capturePreparedWorkerExchangeLatency(t, capture)
	file := createPreparedWorkerCaptureFile(t, filepath.Join(capture.Output, "host-latency.tsv"))
	defer file.Close()
	writePreparedWorkerCapture(t, file, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tsample\torder\tembedded_ns\tworker_ns\tobserved_worker_minus_embedded_ns_clamped\tresult_sha256\tenvironment_sha256\n")

	embedded := newPreparedWorkerHandler(t)
	worker := startPreparedWorkerProcess(t)
	warm := preparedworkerfixture.TurnRequest{
		Sequence:   1,
		Revision:   0,
		Projection: preparedworkerfixture.Projection{Step: 1, Seed: 3, Work: 1},
	}
	want, err := embedded.Transact(context.Background(), warm)
	if err != nil {
		t.Fatal(err)
	}
	got, err := worker.Transact(context.Background(), warm)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("host latency warmup results differ")
	}
	state := got.State
	var pendingID uint64

	parentStart, err := samplePreparedWorkerProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	childStart, err := samplePreparedWorkerProcess(worker.pid)
	if err != nil {
		t.Fatal(err)
	}
	parentPeak := parentStart.RSS
	childPeak := childStart.RSS
	embeddedSamples := make([]time.Duration, 0, preparedWorkerLatencySampleCount)
	workerSamples := make([]time.Duration, 0, preparedWorkerLatencySampleCount)
	wallStart := time.Now()
	for sample := 1; sample <= preparedWorkerLatencySampleCount; sample++ {
		request := preparedworkerfixture.TurnRequest{
			Sequence:   uint64(sample + 1),
			Revision:   uint64(sample),
			State:      state,
			Projection: preparedworkerfixture.Projection{Step: 1, Seed: 3, Work: 1},
		}
		if sample%2 != 0 {
			request.Events = []preparedworkerfixture.DamageEvent{{
				Route:  preparedworkerfixture.RouteDamage,
				Entity: 7,
				Amount: 1,
			}}
		} else {
			if pendingID == 0 {
				t.Fatalf("host latency sample=%d has no pending effect to complete", sample)
			}
			request.Completions = []preparedworkerfixture.Completion{{
				EffectID: pendingID,
				Status:   preparedworkerfixture.CompletionOK,
				Value:    2,
			}}
		}
		order := [2]string{"embedded", "worker"}
		if (sample+preparedWorkerPairIndex(capture.Pair))%2 != 0 {
			order = [2]string{"worker", "embedded"}
		}
		measured := make(map[string]preparedworkerfixture.TurnResult, 2)
		durations := make(map[string]time.Duration, 2)
		for _, engine := range order {
			var transactor preparedworkerprobe.TurnTransactor = embedded
			if engine == "worker" {
				transactor = worker
			}
			start := time.Now()
			result, err := transactor.Transact(context.Background(), request)
			durations[engine] = time.Since(start)
			if err != nil {
				t.Fatalf("host latency sample=%d engine=%s: %v", sample, engine, err)
			}
			measured[engine] = result
		}
		if !reflect.DeepEqual(measured["worker"], measured["embedded"]) {
			t.Fatalf("host latency sample=%d results differ", sample)
		}
		state = measured["worker"].State
		if sample%2 != 0 {
			if len(measured["worker"].Pending) != 1 {
				t.Fatalf("host latency sample=%d pending=%v, want one", sample, measured["worker"].Pending)
			}
			pendingID = measured["worker"].Pending[0]
		} else {
			if len(measured["worker"].Pending) != 0 {
				t.Fatalf("host latency sample=%d pending=%v, want none", sample, measured["worker"].Pending)
			}
			pendingID = 0
		}
		embeddedDuration := durations["embedded"]
		workerDuration := durations["worker"]
		pairedDelta := workerDuration - embeddedDuration
		if pairedDelta < 0 {
			pairedDelta = 0
		}
		embeddedSamples = append(embeddedSamples, embeddedDuration)
		workerSamples = append(workerSamples, workerDuration)
		writePreparedWorkerCapture(t, file, "1\t%s\t%s\t%s\t%d\t%s,%s\t%d\t%d\t%d\t%s\t%s\n",
			capture.ID,
			capture.Pair,
			capture.SourceCommit,
			sample,
			order[0],
			order[1],
			embeddedDuration.Nanoseconds(),
			workerDuration.Nanoseconds(),
			pairedDelta.Nanoseconds(),
			parityStringSHA256(canonicalPreparedWorkerTurn(measured["worker"])),
			capture.EnvironmentHash,
		)
		if sample%256 == 0 {
			parent, sampleErr := samplePreparedWorkerProcess(os.Getpid())
			if sampleErr != nil {
				t.Fatal(sampleErr)
			}
			child, sampleErr := samplePreparedWorkerProcess(worker.pid)
			if sampleErr != nil {
				t.Fatal(sampleErr)
			}
			parentPeak = max(parentPeak, parent.RSS)
			childPeak = max(childPeak, child.RSS)
		}
	}
	wall := time.Since(wallStart)
	parentEnd, err := samplePreparedWorkerProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	childEnd, err := samplePreparedWorkerProcess(worker.pid)
	if err != nil {
		t.Fatal(err)
	}
	parentPeak = max(parentPeak, parentEnd.RSS)
	childPeak = max(childPeak, childEnd.RSS)
	if parentEnd.CPU < parentStart.CPU || childEnd.CPU < childStart.CPU {
		t.Fatal("process CPU counter moved backwards")
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	if err := embedded.Close(); err != nil {
		t.Fatal(err)
	}

	summary := preparedWorkerHostSummary{
		EmbeddedLatencyP50: preparedWorkerDurationQuantile(embeddedSamples, 0.50),
		EmbeddedLatencyP95: preparedWorkerDurationQuantile(embeddedSamples, 0.95),
		EmbeddedLatencyP99: preparedWorkerDurationQuantile(embeddedSamples, 0.99),
		WorkerLatencyP50:   preparedWorkerDurationQuantile(workerSamples, 0.50),
		WorkerLatencyP95:   preparedWorkerDurationQuantile(workerSamples, 0.95),
		WorkerLatencyP99:   preparedWorkerDurationQuantile(workerSamples, 0.99),
		ExchangeCostP50:    preparedWorkerDurationQuantile(exchangeSamples, 0.50),
		ExchangeCostP95:    preparedWorkerDurationQuantile(exchangeSamples, 0.95),
		ExchangeCostP99:    preparedWorkerDurationQuantile(exchangeSamples, 0.99),
		Wall:               wall,
		ParentCPU:          parentEnd.CPU - parentStart.CPU,
		ChildCPU:           childEnd.CPU - childStart.CPU,
		ParentPeakRSS:      parentPeak,
		ChildPeakRSS:       childPeak,
	}
	writePreparedWorkerResources(t, capture, summary)
	return summary
}

func capturePreparedWorkerExchangeLatency(
	t *testing.T,
	capture preparedWorkerCaptureContext,
) []time.Duration {
	t.Helper()
	file := createPreparedWorkerCaptureFile(t, filepath.Join(capture.Output, "host-exchange-latency.tsv"))
	defer file.Close()
	writePreparedWorkerCapture(t, file, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tsample\tround_trip_ns\tresult_sha256\tenvironment_sha256\n")
	worker := startPreparedWorkerProcess(t, "echo")
	samples := make([]time.Duration, 0, preparedWorkerLatencySampleCount)
	for sample := 1; sample <= preparedWorkerLatencySampleCount; sample++ {
		request := preparedWorkerRepresentativeExchangeRequest(uint64(sample))
		start := time.Now()
		result, err := worker.Transact(context.Background(), request)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("host exchange sample=%d: %v", sample, err)
		}
		if result.Sequence != request.Sequence || result.Revision != request.Revision+1 ||
			len(result.Commands) != 1 || len(result.Effects) != 1 || len(result.Pending) != 1 {
			t.Fatalf("host exchange sample=%d returned malformed representative record: %#v", sample, result)
		}
		samples = append(samples, elapsed)
		writePreparedWorkerCapture(t, file, "1\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			capture.ID,
			capture.Pair,
			capture.SourceCommit,
			sample,
			elapsed.Nanoseconds(),
			parityStringSHA256(canonicalPreparedWorkerTurn(result)),
			capture.EnvironmentHash,
		)
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
	return samples
}

func preparedWorkerRepresentativeExchangeRequest(sequence uint64) preparedworkerfixture.TurnRequest {
	return preparedworkerfixture.TurnRequest{
		Sequence:   sequence,
		Revision:   sequence - 1,
		Projection: preparedworkerfixture.Projection{Step: 1, Seed: 8, Work: 4},
		Events: []preparedworkerfixture.DamageEvent{{
			Route:  preparedworkerfixture.RouteDamage,
			Entity: 7,
			Amount: 3,
		}},
		Completions: []preparedworkerfixture.Completion{{
			EffectID: sequence,
			Status:   preparedworkerfixture.CompletionOK,
			Value:    2,
		}},
	}
}

// capturePreparedWorkerCrossingScaling is a generic process-crossing
// discriminator. It is not a second game-turn protocol: the typed TurnRequest
// lane above is the production-shaped seam.
func capturePreparedWorkerCrossingScaling(t *testing.T, capture preparedWorkerCaptureContext) {
	t.Helper()
	file := createPreparedWorkerCaptureFile(t, filepath.Join(capture.Output, "host-crossing-scaling.tsv"))
	defer file.Close()
	writePreparedWorkerCapture(t, file, "schema_version\tcapture_id\tcapture_pair\tsource_commit\trepeat\texchanges\tshape\telapsed_ns\tresult_sha256\tenvironment_sha256\n")
	worker := startPreparedBatchWorker(t, "classic/recursive_fibonacci")
	if _, err := worker.Call(context.Background(), 1, parityCaptureSeed); err != nil {
		t.Fatal(err)
	}
	for repeat := 1; repeat <= parityRepeatCount; repeat++ {
		for _, exchanges := range []int{0, 1, 10, 100, 1000} {
			shapes := [2]string{"batched", "chatty"}
			if (repeat+exchanges+preparedWorkerPairIndex(capture.Pair))%2 != 0 {
				shapes = [2]string{"chatty", "batched"}
			}
			elapsedByShape := make(map[string]time.Duration, len(shapes))
			checksumByShape := make(map[string]int64, len(shapes))
			for _, shape := range shapes {
				elapsed, checksum, err := measurePreparedWorkerChattyShape(worker, shape, exchanges)
				if err != nil {
					t.Fatal(err)
				}
				elapsedByShape[shape] = elapsed
				checksumByShape[shape] = checksum
			}
			if checksumByShape["chatty"] != checksumByShape["batched"] {
				t.Fatalf(
					"chatty workload repeat=%d exchanges=%d checksum=%d, batched=%d",
					repeat,
					exchanges,
					checksumByShape["chatty"],
					checksumByShape["batched"],
				)
			}
			for _, shape := range shapes {
				writePreparedWorkerCapture(t, file, "1\t%s\t%s\t%s\t%d\t%d\t%s\t%d\t%s\t%s\n",
					capture.ID,
					capture.Pair,
					capture.SourceCommit,
					repeat,
					exchanges,
					shape,
					elapsedByShape[shape].Nanoseconds(),
					parityStringSHA256(strconv.FormatInt(checksumByShape[shape], 10)),
					capture.EnvironmentHash,
				)
			}
		}
	}
	if err := worker.Close(); err != nil {
		t.Fatal(err)
	}
}

func measurePreparedWorkerChattyShape(
	worker *preparedBatchWorker,
	shape string,
	exchanges int,
) (time.Duration, int64, error) {
	if exchanges < 0 {
		return 0, 0, fmt.Errorf("prepared worker chattiness: invalid exchange count %d", exchanges)
	}
	start := time.Now()
	switch shape {
	case "batched":
		if exchanges == 0 {
			return time.Since(start), 0, nil
		}
		result, err := worker.Call(context.Background(), exchanges, parityCaptureSeed)
		if err != nil {
			return time.Since(start), 0, err
		}
		checksum, err := strconv.ParseInt(result, 10, 64)
		return time.Since(start), checksum, err
	case "chatty":
		var checksum int64
		for index := 0; index < exchanges; index++ {
			result, err := worker.Call(context.Background(), 1, parityCaptureSeed+int64(index))
			if err != nil {
				return time.Since(start), 0, err
			}
			individual, err := strconv.ParseInt(result, 10, 64)
			if err != nil || individual%2 != 0 {
				return time.Since(start), 0, fmt.Errorf(
					"prepared worker chattiness: one-call checksum %q cannot recover the guest value",
					result,
				)
			}
			guestValue := individual / 2
			weight := int64((index+1)%7 + 1)
			checksum += guestValue * weight
		}
		return time.Since(start), checksum, nil
	default:
		return time.Since(start), 0, fmt.Errorf("prepared worker chattiness: unknown shape %q", shape)
	}
}

type preparedWorkerProcessSample struct {
	RSS uint64
	CPU time.Duration
}

func samplePreparedWorkerProcess(pid int) (preparedWorkerProcessSample, error) {
	if pid <= 0 {
		return preparedWorkerProcessSample{}, fmt.Errorf("prepared worker resource probe: invalid PID %d", pid)
	}
	command := exec.Command("ps", "-o", "rss=,time=", "-p", strconv.Itoa(pid))
	command.Env = append(os.Environ(), "LC_ALL=C")
	output, err := command.Output()
	if err != nil {
		return preparedWorkerProcessSample{}, fmt.Errorf("prepared worker resource probe: %w", err)
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 {
		return preparedWorkerProcessSample{}, fmt.Errorf("prepared worker resource probe: invalid ps output %q", output)
	}
	rssKiB, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || rssKiB == 0 || rssKiB > math.MaxUint64/1024 {
		return preparedWorkerProcessSample{}, fmt.Errorf("prepared worker resource probe: invalid RSS %q", fields[0])
	}
	cpu, err := parsePreparedWorkerProcessTime(fields[1])
	if err != nil {
		return preparedWorkerProcessSample{}, err
	}
	return preparedWorkerProcessSample{RSS: rssKiB * 1024, CPU: cpu}, nil
}

func parsePreparedWorkerProcessTime(value string) (time.Duration, error) {
	day := int64(0)
	clock := value
	if split := strings.SplitN(value, "-", 2); len(split) == 2 {
		parsed, err := strconv.ParseInt(split[0], 10, 64)
		if err != nil || parsed < 0 {
			return 0, fmt.Errorf("prepared worker resource probe: invalid CPU time %q", value)
		}
		day = parsed
		clock = split[1]
	}
	parts := strings.Split(clock, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("prepared worker resource probe: invalid CPU time %q", value)
	}
	seconds, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil || seconds < 0 || !finiteParityFloat(seconds) {
		return 0, fmt.Errorf("prepared worker resource probe: invalid CPU time %q", value)
	}
	minutes, err := strconv.ParseInt(parts[len(parts)-2], 10, 64)
	if err != nil || minutes < 0 || minutes >= 60 {
		return 0, fmt.Errorf("prepared worker resource probe: invalid CPU time %q", value)
	}
	hours := int64(0)
	if len(parts) == 3 {
		hours, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil || hours < 0 || hours >= 24 && day != 0 {
			return 0, fmt.Errorf("prepared worker resource probe: invalid CPU time %q", value)
		}
	}
	totalSeconds := float64(day*24*60*60+hours*60*60+minutes*60) + seconds
	return time.Duration(totalSeconds * float64(time.Second)), nil
}

func preparedWorkerDurationQuantile(samples []time.Duration, quantile float64) time.Duration {
	if len(samples) == 0 || quantile <= 0 || quantile > 1 {
		return 0
	}
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
	index := int(math.Ceil(quantile*float64(len(ordered)))) - 1
	return ordered[index]
}

func preparedWorkerPairIndex(pair string) int {
	if pair == "b" {
		return 2
	}
	return 1
}

func writePreparedWorkerResources(t testing.TB, capture preparedWorkerCaptureContext, summary preparedWorkerHostSummary) {
	t.Helper()
	file := createPreparedWorkerCaptureFile(t, filepath.Join(capture.Output, "host-resources.tsv"))
	defer file.Close()
	writePreparedWorkerCapture(t, file, "schema_version\tcapture_id\tcapture_pair\tsource_commit\twall_ns\tparent_cpu_ns\tchild_cpu_ns\tparent_peak_rss_bytes\tchild_peak_rss_bytes\tenvironment_sha256\n")
	writePreparedWorkerCapture(t, file, "1\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\n",
		capture.ID,
		capture.Pair,
		capture.SourceCommit,
		summary.Wall.Nanoseconds(),
		summary.ParentCPU.Nanoseconds(),
		summary.ChildCPU.Nanoseconds(),
		summary.ParentPeakRSS,
		summary.ChildPeakRSS,
		capture.EnvironmentHash,
	)
}

func writePreparedWorkerHostSummary(
	t testing.TB,
	capture preparedWorkerCaptureContext,
	summary preparedWorkerHostSummary,
	gateErr error,
) {
	t.Helper()
	file := createPreparedWorkerCaptureFile(t, filepath.Join(capture.Output, "host-summary.tsv"))
	defer file.Close()
	writePreparedWorkerCapture(t, file, "field\tvalue\n")
	status := "PASS"
	if gateErr != nil {
		status = "FAIL: " + gateErr.Error()
	}
	for _, field := range [][2]string{
		{"schema_version", "1"},
		{"capture_id", capture.ID},
		{"capture_pair", capture.Pair},
		{"source_commit", capture.SourceCommit},
		{"environment_sha256", capture.EnvironmentHash},
		{"semantic_match", strconv.FormatBool(summary.SemanticMatch)},
		{"exchange_phases", strconv.Itoa(summary.ExchangePhases)},
		{"worker_embedded_slope_max", strconv.FormatFloat(summary.WorkerEmbeddedMax, 'g', -1, 64)},
		{"worker_embedded_slope_budget", strconv.FormatFloat(preparedWorkerDevSlopeBudget, 'f', 2, 64)},
		{"frame_budget_ns", strconv.FormatInt(preparedWorkerFrameBudget.Nanoseconds(), 10)},
		{"exchange_budget_ns", strconv.FormatInt(preparedWorkerExchangeBudget.Nanoseconds(), 10)},
		{"embedded_latency_p50_ns", strconv.FormatInt(summary.EmbeddedLatencyP50.Nanoseconds(), 10)},
		{"embedded_latency_p95_ns", strconv.FormatInt(summary.EmbeddedLatencyP95.Nanoseconds(), 10)},
		{"embedded_latency_p99_ns", strconv.FormatInt(summary.EmbeddedLatencyP99.Nanoseconds(), 10)},
		{"worker_latency_p50_ns", strconv.FormatInt(summary.WorkerLatencyP50.Nanoseconds(), 10)},
		{"worker_latency_p95_ns", strconv.FormatInt(summary.WorkerLatencyP95.Nanoseconds(), 10)},
		{"worker_latency_p99_ns", strconv.FormatInt(summary.WorkerLatencyP99.Nanoseconds(), 10)},
		{"representative_exchange_p50_ns", strconv.FormatInt(summary.ExchangeCostP50.Nanoseconds(), 10)},
		{"representative_exchange_p95_ns", strconv.FormatInt(summary.ExchangeCostP95.Nanoseconds(), 10)},
		{"representative_exchange_p99_ns", strconv.FormatInt(summary.ExchangeCostP99.Nanoseconds(), 10)},
		{"rss_safety_ceiling_bytes", strconv.FormatUint(preparedWorkerRSSSafetyCeiling, 10)},
		{"status", status},
	} {
		writePreparedWorkerCapture(t, file, "%s\t%s\n", field[0], field[1])
	}
}
