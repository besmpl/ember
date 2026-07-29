package preparedworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
)

const (
	processTransportAdmissionSamples = 4096
	processTransportAdmissionWarmup  = 128
	processTransportFrameBudget      = time.Second / 60
	processTransportBudget           = processTransportFrameBudget / 10
)

type processTransportCapture struct {
	id          string
	pair        string
	source      string
	environment string
	output      string
}

type processTransportSample struct {
	elapsed  time.Duration
	response contentDigest
}

// TestProcessBackendTransportAdmissionLive measures only the production EPW2
// codec and OS-pipe exchange. Durable journal work is deliberately outside
// this observer because its file.Sync tails are measured by the host turn
// receipt and cannot be isolated by subtracting two independent transactions.
func TestProcessBackendTransportAdmissionLive(t *testing.T) {
	if os.Getenv("EMBER_PREPARED_WORKER_TRANSPORT_ADMISSION_LIVE") != "1" {
		t.Skip("set EMBER_PREPARED_WORKER_TRANSPORT_ADMISSION_LIVE=1 to capture EPW2 transport admission")
	}
	capture := processTransportCaptureFromEnvironment(t)
	limits := processProtocolTestLimits()
	contract := identityFor("process-transport-admission-contract-v1")
	checkpointSchema := identityFor("process-transport-admission-checkpoint-v1")
	artifact := processBackendTestArtifact(t, contract)
	checkpoint := bytes.Repeat([]byte{0x5a}, limits.MaxCheckpointBytes)
	backend, _, err := newProcessPreparer(processTransportAdmissionLauncher{}).Prepare(
		context.Background(),
		backendSpec{
			Artifact: artifact.Identity(), Contract: contract, Stream: identityFor("process-transport-admission-stream-v1"),
			CheckpointSchema: checkpointSchema, CheckpointDigest: hashContent(checkpoint),
			Checkpoint: checkpoint, Limits: limits, artifact: artifact,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := backend.Close(ctx); err != nil {
			t.Error(err)
		}
	})

	request := bytes.Repeat([]byte{0xa5}, limits.MaxRequestBytes)
	for sample := 1; sample <= processTransportAdmissionWarmup; sample++ {
		sequence := uint64(sample)
		if _, err := backend.Apply(context.Background(), encodedOperation{
			Sequence: sequence, BaseRevision: sequence - 1, Request: request,
		}); err != nil {
			t.Fatalf("warmup sample=%d: %v", sample, err)
		}
	}

	samples := make([]processTransportSample, 0, processTransportAdmissionSamples)
	var responseBytes int
	for sample := 1; sample <= processTransportAdmissionSamples; sample++ {
		sequence := uint64(processTransportAdmissionWarmup + sample)
		start := time.Now()
		decision, err := backend.Apply(context.Background(), encodedOperation{
			Sequence: sequence, BaseRevision: sequence - 1, Request: request,
		})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("transport sample=%d: %v", sample, err)
		}
		encoded, err := encodeProcessDecision(decision, limits)
		if err != nil {
			t.Fatalf("encode transport response sample=%d: %v", sample, err)
		}
		if sample == 1 {
			responseBytes = len(encoded)
		} else if len(encoded) != responseBytes {
			t.Fatalf("transport response size changed at sample=%d: %d, want %d", sample, len(encoded), responseBytes)
		}
		samples = append(samples, processTransportSample{elapsed: elapsed, response: hashContent(encoded)})
	}

	p50 := processTransportQuantile(samples, 0.50)
	p95 := processTransportQuantile(samples, 0.95)
	p99 := processTransportQuantile(samples, 0.99)
	sampleSet := processTransportSampleSetDigest(samples)
	status := "PASS"
	if p99 > processTransportBudget {
		status = "FAIL"
	}
	writeProcessTransportSamples(t, capture, samples)
	writeProcessTransportSummary(t, capture, artifact, len(request), responseBytes, sampleSet, p50, p95, p99, status)
	if status != "PASS" {
		t.Fatalf("OS-process EPW2 transport p99 %s exceeds %s", p99, processTransportBudget)
	}
}

func TestProcessBackendTransportAdmissionHelper(t *testing.T) {
	if os.Getenv("EMBER_PREPARED_WORKER_TRANSPORT_ADMISSION_HELPER") != "1" {
		return
	}
	buildID, err := parseWorkerBuildID(os.Getenv("EMBER_PREPARED_WORKER_TRANSPORT_BUILD_ID"))
	if err == nil {
		err = serveProcessWorker(
			context.Background(),
			os.Stdin,
			os.Stdout,
			processWorkerOptions{
				BuildID:          buildID,
				Contract:         identityFor("process-transport-admission-contract-v1"),
				CheckpointSchema: identityFor("process-transport-admission-checkpoint-v1"),
				Limits:           processProtocolTestLimits(),
				Preparer: &processServerPreparer{
					backend: newProcessTransportAdmissionBackend(processProtocolTestLimits()),
				},
			},
			false,
		)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

type processTransportAdmissionLauncher struct{}

func (processTransportAdmissionLauncher) Launch(artifact *processArtifact) (*processChild, error) {
	command := exec.Command(
		artifact.executable,
		"-test.run=^TestProcessBackendTransportAdmissionHelper$",
	)
	command.Env = append(
		workerProcessEnvironment(),
		"EMBER_PREPARED_WORKER_TRANSPORT_ADMISSION_HELPER=1",
		"EMBER_PREPARED_WORKER_TRANSPORT_BUILD_ID="+hex.EncodeToString(artifact.buildID[:]),
	)
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, err
	}
	stderr := &boundedProcessBuffer{limit: processStderrBytes}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, err
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	return &processChild{
		input: input, output: output, wait: wait,
		terminate: command.Process.Kill,
		stderr:    stderr.String,
	}, nil
}

type processTransportAdmissionBackend struct {
	result     []byte
	checkpoint []byte
	effects    [][]byte
}

func newProcessTransportAdmissionBackend(limits transactionLimits) *processTransportAdmissionBackend {
	effects := make([][]byte, limits.MaxOutboxItems)
	perEffect := limits.MaxOutboxBytes / limits.MaxOutboxItems
	for index := range effects {
		effects[index] = bytes.Repeat([]byte{byte(index + 1)}, perEffect)
	}
	return &processTransportAdmissionBackend{
		result:     bytes.Repeat([]byte{0x3c}, limits.MaxResultBytes),
		checkpoint: bytes.Repeat([]byte{0xc3}, limits.MaxCheckpointBytes),
		effects:    effects,
	}
}

func (backend *processTransportAdmissionBackend) Apply(
	_ context.Context,
	operation encodedOperation,
) (wireDecision, error) {
	return wireDecision{
		Position:      Position{Sequence: operation.Sequence, Revision: operation.BaseRevision + 1},
		Result:        backend.result,
		Checkpoint:    backend.checkpoint,
		HasCheckpoint: true,
		Effects:       backend.effects,
		Quiescent:     true,
	}, nil
}

func (*processTransportAdmissionBackend) Close(context.Context) error { return nil }

func processTransportCaptureFromEnvironment(t *testing.T) processTransportCapture {
	t.Helper()
	capture := processTransportCapture{
		id:          os.Getenv("PREPARED_WORKER_CAPTURE_ID"),
		pair:        os.Getenv("PREPARED_WORKER_CAPTURE_PAIR"),
		source:      os.Getenv("PREPARED_WORKER_SOURCE_COMMIT"),
		environment: os.Getenv("PREPARED_WORKER_ENVIRONMENT_SHA256"),
		output:      os.Getenv("PREPARED_WORKER_OUTPUT"),
	}
	if capture.pair != "a" && capture.pair != "b" {
		t.Fatalf("transport capture pair %q is not a or b", capture.pair)
	}
	if !processTransportLowerHex(capture.id, 64) {
		t.Fatal("transport capture ID is not one lowercase SHA-256 digest")
	}
	if !processTransportLowerHex(capture.environment, 64) {
		t.Fatal("transport environment ID is not one lowercase SHA-256 digest")
	}
	if !processTransportLowerHex(capture.source, 40) && !processTransportLowerHex(capture.source, 64) {
		t.Fatal("transport source identity is not lowercase hexadecimal commit or content identity")
	}
	if capture.output == "" {
		t.Fatal("transport capture output is empty")
	}
	resolved, err := filepath.Abs(capture.output)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("transport capture output is not a directory")
	}
	capture.output = resolved
	return capture
}

func processTransportLowerHex(value string, size int) bool {
	if len(value) != size {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func processTransportQuantile(samples []processTransportSample, quantile float64) time.Duration {
	if len(samples) == 0 || quantile <= 0 || quantile > 1 {
		return 0
	}
	ordered := append([]processTransportSample(nil), samples...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].elapsed < ordered[right].elapsed })
	index := int(math.Ceil(quantile*float64(len(ordered)))) - 1
	return ordered[index].elapsed
}

func processTransportSampleSetDigest(samples []processTransportSample) contentDigest {
	hasher := sha256.New()
	for index, sample := range samples {
		_, _ = fmt.Fprintf(hasher, "%d=%d/%s\n", index+1, sample.elapsed.Nanoseconds(), sample.response.String())
	}
	var digest contentDigest
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func writeProcessTransportSamples(
	t *testing.T,
	capture processTransportCapture,
	samples []processTransportSample,
) {
	t.Helper()
	var content strings.Builder
	content.WriteString("schema_version\tcapture_id\tcapture_pair\tsource_commit\tsample\telapsed_ns\tresponse_sha256\tenvironment_sha256\n")
	for index, sample := range samples {
		_, _ = fmt.Fprintf(
			&content,
			"1\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			capture.id,
			capture.pair,
			capture.source,
			index+1,
			sample.elapsed.Nanoseconds(),
			sample.response.String(),
			capture.environment,
		)
	}
	writeProcessTransportFile(t, filepath.Join(capture.output, "transport-latency.tsv"), content.String())
}

func writeProcessTransportSummary(
	t *testing.T,
	capture processTransportCapture,
	artifact preparedArtifact,
	requestBytes int,
	responseBytes int,
	sampleSet contentDigest,
	p50 time.Duration,
	p95 time.Duration,
	p99 time.Duration,
	status string,
) {
	t.Helper()
	fields := [][2]string{
		{"schema_version", "1"},
		{"capture_id", capture.id},
		{"capture_pair", capture.pair},
		{"source_commit", capture.source},
		{"environment_sha256", capture.environment},
		{"wire_magic", "EPW2"},
		{"protocol_version", "1"},
		{"sample_count", strconv.Itoa(processTransportAdmissionSamples)},
		{"target_os", runtime.GOOS},
		{"target_arch", runtime.GOARCH},
		{"observer_build_id", artifact.process.buildID.String()},
		{"observer_executable_sha256", artifact.process.binaryDigest.String()},
		{"request_bytes", strconv.Itoa(requestBytes)},
		{"response_bytes", strconv.Itoa(responseBytes)},
		{"sample_set_sha256", sampleSet.String()},
		{"frame_budget_ns", strconv.FormatInt(processTransportFrameBudget.Nanoseconds(), 10)},
		{"transport_budget_ns", strconv.FormatInt(processTransportBudget.Nanoseconds(), 10)},
		{"transport_latency_p50_ns", strconv.FormatInt(p50.Nanoseconds(), 10)},
		{"transport_latency_p95_ns", strconv.FormatInt(p95.Nanoseconds(), 10)},
		{"transport_latency_p99_ns", strconv.FormatInt(p99.Nanoseconds(), 10)},
		{"status", status},
	}
	var content strings.Builder
	content.WriteString("field\tvalue\n")
	for _, field := range fields {
		_, _ = fmt.Fprintf(&content, "%s\t%s\n", field[0], field[1])
	}
	writeProcessTransportFile(t, filepath.Join(capture.output, "transport-summary.tsv"), content.String())
}

func writeProcessTransportFile(t *testing.T, path string, content string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
