package ember_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/besmpl/ember/internal/preparedworkerfixture"
)

const (
	preparedBatchWorkerChildEnvironment = "EMBER_PREPARED_BATCH_WORKER_CHILD"
	preparedBatchWorkerMagic            = "EPB1"
	preparedBatchWorkerMaxError         = 4 << 10
	preparedWorkerAdmissionEnvironment  = "EMBER_PREPARED_WORKER_ADMISSION_LIVE"
	preparedWorkerParityCallScale       = 32
)

func TestPreparedBatchWorkerMatchesEmbedded(t *testing.T) {
	const caseID = "classic/recursive_fibonacci"
	entry := preparedWorkerParityEntry(t, caseID)
	programSource, _, err := runtimeParityGuestBatchProgram(entry.Case.source, parityDefaultFixtureVariant)
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := prepareParityExactGuestBatch(
		programSource + "return " + parityDefaultFixtureVariant.batchName + "\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := embedded.close(); err != nil {
			t.Error(err)
		}
	})
	worker := startPreparedBatchWorker(t, caseID)

	wantValues, err := embedded.callBatch(3, 17)
	if err != nil {
		t.Fatal(err)
	}
	want, err := parityExactIntegerResult(wantValues)
	if err != nil {
		t.Fatal(err)
	}
	got, err := worker.Transact(context.Background(), preparedworkerfixture.BatchRequest{
		Iterations: 3,
		Seed:       17,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strconv.FormatInt(got.Checksum, 10) != want || got.Checksum != 90152 {
		t.Fatalf("worker result = %d, embedded = %q, want frozen 90152", got.Checksum, want)
	}
}

func TestPreparedBatchWorkerDeadlineAbortsBlockedIO(t *testing.T) {
	blocked := make(chan struct{})
	var once sync.Once
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := performPreparedWorkerIO(
		ctx,
		func() error {
			once.Do(func() { close(blocked) })
			return nil
		},
		func() error {
			<-blocked
			return io.ErrClosedPipe
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked batch I/O error = %v, want deadline exceeded", err)
	}
}

func TestPreparedWorkerAdmissionGateRequiresBothSlopeAndLuauTargets(t *testing.T) {
	if _, err := preparedWorkerAdmissionGate(
		[]float64{100, 100, 100},
		[]float64{100, 100, 100},
		[]float64{110, 110, 110},
	); err != nil {
		t.Fatalf("matching worker and faster-than-Luau slopes failed: %v", err)
	}
	if _, err := preparedWorkerAdmissionGate(
		[]float64{100, 100, 100},
		[]float64{106, 106, 106},
		[]float64{110, 110, 110},
	); err != nil {
		t.Fatalf("all-37 gate incorrectly applied the host-shaped matched-slope target: %v", err)
	}
	if _, err := preparedWorkerAdmissionGate(
		[]float64{110, 110, 110},
		[]float64{110, 110, 110},
		[]float64{100, 100, 100},
	); err == nil {
		t.Fatal("worker slope above Luau target passed")
	}
	if _, err := preparedWorkerAdmissionGate(
		[]float64{110, 110, 110},
		[]float64{90, 90, 90},
		[]float64{100, 100, 100},
	); err == nil {
		t.Fatal("embedded slope above Luau target passed")
	}
}

func TestPreparedWorkerParityCallScalePreservesPerCallSlope(t *testing.T) {
	samples := make(map[int]float64, len(parityIterations))
	for _, base := range parityIterations {
		n := base * preparedWorkerParityCallScale
		samples[n] = float64(n * 4)
	}
	if err := validatePreparedWorkerParityWindow(samples); err != nil {
		t.Fatal(err)
	}
	fit, err := fitPreparedWorkerParityLine(samples)
	if err != nil {
		t.Fatal(err)
	}
	if fit.Inner != 4 || fit.Entry != 0 {
		t.Fatalf("scaled fit = %#v, want 4ns per call with zero intercept", fit)
	}
}

func TestPreparedWorkerAll37AdmissionLive(t *testing.T) {
	if os.Getenv(preparedWorkerAdmissionEnvironment) != "1" {
		t.Skip("run through scripts/check-prepared-worker-admission")
	}
	environment, err := inspectParityEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	pair := os.Getenv("PREPARED_WORKER_CAPTURE_PAIR")
	captureID := os.Getenv("PREPARED_WORKER_CAPTURE_ID")
	sourceCommit := os.Getenv("PREPARED_WORKER_SOURCE_COMMIT")
	environmentHash := os.Getenv("PREPARED_WORKER_ENVIRONMENT_SHA256")
	output := os.Getenv("PREPARED_WORKER_OUTPUT")
	if (pair != "a" && pair != "b") || captureID == "" || output == "" ||
		!parityHexDigest(sourceCommit, 40, 64) || !parityHexDigest(environmentHash, 64) {
		t.Fatal("prepared worker capture metadata is incomplete or invalid")
	}
	selected, err := parityManifestSelection(os.Getenv("PREPARED_WORKER_CASES"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatalf("create prepared worker capture: %v", err)
	}
	raw := createPreparedWorkerCaptureFile(t, filepath.Join(output, "raw.tsv"))
	defer raw.Close()
	slopes := createPreparedWorkerCaptureFile(t, filepath.Join(output, "slopes.tsv"))
	defer slopes.Close()
	summaryFile := createPreparedWorkerCaptureFile(t, filepath.Join(output, "summary.tsv"))
	defer summaryFile.Close()
	metadata := createPreparedWorkerCaptureFile(t, filepath.Join(output, "environment.tsv"))
	defer metadata.Close()

	writePreparedWorkerCapture(t, metadata, "field\tvalue\n")
	for _, field := range [][2]string{
		{"schema_version", "1"},
		{"capture_id", captureID},
		{"capture_pair", pair},
		{"source_commit", sourceCommit},
		{"environment_sha256", environmentHash},
		{"go_version", runtime.Version()},
		{"platform", environment.Platform},
		{"cpu", environment.CPU},
		{"cgo_enabled", environment.CGOEnabled},
		{"gomaxprocs", strconv.Itoa(environment.GOMAXPROCS)},
		{"luau_sha256", environment.LuauSHA256},
		{"luau_version", environment.LuauVersion},
		{"case_count", strconv.Itoa(len(selected))},
	} {
		writePreparedWorkerCapture(t, metadata, "%s\t%s\n", field[0], field[1])
	}
	writePreparedWorkerCapture(t, raw, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tcorpus\tname\tengine\trepeat\torder\tn\tseed\telapsed_ns\tresult\tworkload_sha256\tprogram_sha256\tenvironment_sha256\n")
	writePreparedWorkerCapture(t, slopes, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tcorpus\tname\tengine\trepeat\tslope_ns_per_guest_call\tintercept_ns\tresult_set_sha256\tworkload_sha256\tprogram_sha256\tenvironment_sha256\n")
	writePreparedWorkerCapture(t, summaryFile, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tcorpus\tname\tworker_luau_median\tworker_luau_p90\tembedded_luau_median\tembedded_luau_p90\tworker_embedded_max\tstatus\tenvironment_sha256\n")
	capturePreparedWorkerHostAdmission(t, preparedWorkerCaptureContext{
		ID:              captureID,
		Pair:            pair,
		SourceCommit:    sourceCommit,
		EnvironmentHash: environmentHash,
		Output:          output,
		LuauPath:        environment.LuauPath,
	})

	pairIndex := 1
	if pair == "b" {
		pairIndex = 2
	}
	var gateFailures []string
	for _, entry := range selected {
		caseID := entry.Corpus + "/" + entry.Name
		programSource, seededSource, err := runtimeParityGuestBatchProgram(entry.Case.source, parityDefaultFixtureVariant)
		if err != nil {
			t.Fatalf("%s build guest batch: %v", caseID, err)
		}
		preparedSource := programSource + "return " + parityDefaultFixtureVariant.batchName + "\n"
		embedded, err := prepareParityExactGuestBatch(preparedSource)
		if err != nil {
			t.Fatalf("%s prepare embedded: %v", caseID, err)
		}
		worker := startPreparedBatchWorker(t, caseID)
		luauSource, err := parityGuestBatchLuauSource(entry.Case.source, parityDefaultFixtureVariant)
		if err != nil {
			_ = embedded.close()
			_ = worker.Close()
			t.Fatalf("%s build Luau batch: %v", caseID, err)
		}
		scriptPath := filepath.Join(output, "scripts", entry.Corpus+"-"+entry.Name+".luau")
		if err := os.MkdirAll(filepath.Dir(scriptPath), 0o700); err != nil {
			_ = embedded.close()
			_ = worker.Close()
			t.Fatal(err)
		}
		if err := os.WriteFile(scriptPath, []byte(luauSource), 0o700); err != nil {
			_ = embedded.close()
			_ = worker.Close()
			t.Fatal(err)
		}

		engines := []string{"embedded", "worker", "luau"}
		timings := make(map[string][]map[int]float64, len(engines))
		results := make(map[string][]map[int]string, len(engines))
		for _, engine := range engines {
			timings[engine] = make([]map[int]float64, parityRepeatCount+1)
			results[engine] = make([]map[int]string, parityRepeatCount+1)
			for repeat := 1; repeat <= parityRepeatCount; repeat++ {
				timings[engine][repeat] = make(map[int]float64, len(parityIterations))
				results[engine][repeat] = make(map[int]string, len(parityIterations))
			}
		}
		workloadHash := parityStringSHA256(seededSource)
		programHash := parityStringSHA256(programSource)
		for repeat := 1; repeat <= parityRepeatCount; repeat++ {
			for iterationIndex, baseN := range parityIterations {
				n := baseN * preparedWorkerParityCallScale
				order := preparedWorkerEngineOrder(pairIndex, repeat, iterationIndex)
				point, err := acquireCleanParityPoint(parityPointAttemptLimit, sampleParitySystem, func() ([]parityPointMeasurement, error) {
					measurements := make([]parityPointMeasurement, 0, len(order))
					for engineIndex, engine := range order {
						var elapsed float64
						var result string
						var measureErr error
						switch engine {
						case "embedded":
							elapsed, result, measureErr = measureParityEmberGuestBatch(embedded, n, parityCaptureSeed)
						case "worker":
							elapsed, result, measureErr = measurePreparedBatchWorker(worker, n, parityCaptureSeed)
						case "luau":
							elapsed, result, measureErr = measureParityLuauGuestBatch(environment.LuauPath, scriptPath, n, parityCaptureSeed)
						default:
							measureErr = fmt.Errorf("unknown engine %q", engine)
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
					if measurements[0].result != measurements[1].result || measurements[0].result != measurements[2].result {
						return nil, fmt.Errorf(
							"guest result mismatch: %s=%q %s=%q %s=%q",
							measurements[0].engine,
							measurements[0].result,
							measurements[1].engine,
							measurements[1].result,
							measurements[2].engine,
							measurements[2].result,
						)
					}
					return measurements, nil
				}, func() { time.Sleep(parityPointRetryDelay) })
				if err != nil {
					_ = embedded.close()
					_ = worker.Close()
					t.Fatalf("%s repeat=%d N=%d: %v", caseID, repeat, n, err)
				}
				for _, measurement := range point {
					timings[measurement.engine][repeat][n] = measurement.elapsed
					results[measurement.engine][repeat][n] = measurement.result
					acquisitionOrder := (repeat-1)*len(parityIterations)*3 + iterationIndex*3 + measurement.engineIndex + 1
					writePreparedWorkerCapture(t, raw, "1\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%.17g\t%s\t%s\t%s\t%s\n",
						captureID,
						pair,
						sourceCommit,
						entry.Corpus,
						entry.Name,
						measurement.engine,
						repeat,
						acquisitionOrder,
						n,
						parityCaptureSeed,
						measurement.elapsed,
						measurement.result,
						workloadHash,
						programHash,
						environmentHash,
					)
				}
			}
		}

		fits := make(map[string][]float64, len(engines))
		for _, engine := range engines {
			fits[engine] = make([]float64, parityRepeatCount)
			for repeat := 1; repeat <= parityRepeatCount; repeat++ {
				if err := validatePreparedWorkerParityWindow(timings[engine][repeat]); err != nil {
					_ = embedded.close()
					_ = worker.Close()
					t.Fatalf("%s %s repeat=%d: %v", caseID, engine, repeat, err)
				}
				fit, err := fitPreparedWorkerParityLine(timings[engine][repeat])
				if err != nil {
					_ = embedded.close()
					_ = worker.Close()
					t.Fatalf("%s %s repeat=%d: %v", caseID, engine, repeat, err)
				}
				fits[engine][repeat-1] = fit.Inner
				resultHash, err := preparedWorkerResultSetSHA256(results[engine][repeat])
				if err != nil {
					_ = embedded.close()
					_ = worker.Close()
					t.Fatal(err)
				}
				writePreparedWorkerCapture(t, slopes, "1\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%.17g\t%.17g\t%s\t%s\t%s\t%s\n",
					captureID,
					pair,
					sourceCommit,
					entry.Corpus,
					entry.Name,
					engine,
					repeat,
					fit.Inner,
					fit.Entry,
					resultHash,
					workloadHash,
					programHash,
					environmentHash,
				)
			}
		}
		caseSummary, gateErr := preparedWorkerAdmissionGate(fits["embedded"], fits["worker"], fits["luau"])
		status := "PASS"
		if gateErr != nil {
			status = "FAIL"
			gateFailures = append(gateFailures, caseID+": "+gateErr.Error())
		}
		writePreparedWorkerCapture(t, summaryFile, "1\t%s\t%s\t%s\t%s\t%s\t%.17g\t%.17g\t%.17g\t%.17g\t%.17g\t%s\t%s\n",
			captureID,
			pair,
			sourceCommit,
			entry.Corpus,
			entry.Name,
			caseSummary.WorkerLuauMedian,
			caseSummary.WorkerLuauP90,
			caseSummary.EmbeddedLuauMedian,
			caseSummary.EmbeddedLuauP90,
			caseSummary.WorkerEmbeddedMax,
			status,
			environmentHash,
		)
		if err := embedded.close(); err != nil {
			t.Fatalf("%s close embedded: %v", caseID, err)
		}
		if err := worker.Close(); err != nil {
			t.Fatalf("%s close worker: %v", caseID, err)
		}
		t.Logf(
			"%s worker/Luau %.4f/%.4f embedded/Luau %.4f/%.4f worker/embedded max %.4f %s",
			caseID,
			caseSummary.WorkerLuauMedian,
			caseSummary.WorkerLuauP90,
			caseSummary.EmbeddedLuauMedian,
			caseSummary.EmbeddedLuauP90,
			caseSummary.WorkerEmbeddedMax,
			status,
		)
	}
	if len(gateFailures) != 0 {
		t.Fatalf("prepared worker admission failures:\n%s", strings.Join(gateFailures, "\n"))
	}
}

func TestPreparedBatchWorkerChild(t *testing.T) {
	caseID := os.Getenv(preparedBatchWorkerChildEnvironment)
	if caseID == "" {
		t.Skip("run as the prepared batch worker child")
	}
	entry := preparedWorkerParityEntry(t, caseID)
	programSource, _, err := runtimeParityGuestBatchProgram(entry.Case.source, parityDefaultFixtureVariant)
	if err != nil {
		t.Fatal(err)
	}
	callable, err := prepareParityExactGuestBatch(
		programSource + "return " + parityDefaultFixtureVariant.batchName + "\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := callable.close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := callable.callBatch(1, parityCaptureSeed); err != nil {
		t.Fatal(err)
	}
	if err := servePreparedBatchWorker(os.Stdin, os.Stdout, callable); err != nil {
		t.Fatal(err)
	}
}

type preparedBatchWorker struct {
	mu      sync.Mutex
	command preparedWorkerCommand
	pid     int
	input   io.WriteCloser
	output  io.ReadCloser
	reader  *bufio.Reader
	writer  *bufio.Writer
	stderr  *bytes.Buffer
	closed  bool
	aborted bool
}

func startPreparedBatchWorker(t *testing.T, caseID string) *preparedBatchWorker {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestPreparedBatchWorkerChild$", "-test.count=1")
	command.Env = append(os.Environ(), preparedBatchWorkerChildEnvironment+"="+caseID)
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	processCommand := preparedWorkerCommand{
		wait: command.Wait,
		kill: func() error { return command.Process.Kill() },
	}
	reader := bufio.NewReader(output)
	magic := make([]byte, len(preparedBatchWorkerMagic))
	readyContext, cancelReady := context.WithTimeout(context.Background(), preparedWorkerReadyTimeout)
	readyErr := performPreparedWorkerIO(
		readyContext,
		processCommand.terminate,
		func() error {
			if _, err := io.ReadFull(reader, magic); err != nil {
				return err
			}
			if string(magic) != preparedBatchWorkerMagic {
				return fmt.Errorf("READY %q, want %q", magic, preparedBatchWorkerMagic)
			}
			return nil
		},
	)
	cancelReady()
	if readyErr != nil {
		_ = input.Close()
		_ = output.Close()
		_ = waitPreparedWorkerCommand(processCommand, preparedWorkerCloseTimeout)
		t.Fatalf("start prepared batch worker: %v: %s", readyErr, stderr.String())
	}
	worker := &preparedBatchWorker{
		command: processCommand,
		pid:     command.Process.Pid,
		input:   input,
		output:  output,
		reader:  reader,
		writer:  bufio.NewWriter(input),
		stderr:  stderr,
	}
	t.Cleanup(func() {
		if err := worker.Close(); err != nil {
			t.Error(err)
		}
	})
	return worker
}

func (worker *preparedBatchWorker) Call(ctx context.Context, iterations int, seed int64) (string, error) {
	if worker == nil {
		return "", fmt.Errorf("prepared batch worker: closed")
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.closed {
		return "", fmt.Errorf("prepared batch worker: closed")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		bounded, cancel := context.WithTimeout(ctx, preparedWorkerTransactionTimeout)
		defer cancel()
		ctx = bounded
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if iterations <= 0 {
		return "", fmt.Errorf("prepared batch worker: iterations %d, want positive", iterations)
	}
	var request [16]byte
	binary.BigEndian.PutUint64(request[:8], uint64(iterations))
	binary.BigEndian.PutUint64(request[8:], uint64(seed))
	var result string
	err := performPreparedWorkerIO(
		ctx,
		func() error {
			worker.aborted = true
			return errors.Join(worker.command.terminate(), worker.input.Close(), worker.output.Close())
		},
		func() error {
			if _, err := worker.writer.Write(request[:]); err != nil {
				return fmt.Errorf("prepared batch worker: write: %w", err)
			}
			if err := worker.writer.Flush(); err != nil {
				return fmt.Errorf("prepared batch worker: flush: %w", err)
			}
			status, err := worker.reader.ReadByte()
			if err != nil {
				return fmt.Errorf("prepared batch worker: read status: %w", err)
			}
			if status == 1 {
				message, err := readPreparedBatchWorkerError(worker.reader)
				if err != nil {
					return err
				}
				return errors.New(message)
			}
			if status != 0 {
				return fmt.Errorf("prepared batch worker: status %d is invalid", status)
			}
			var encoded [8]byte
			if _, err := io.ReadFull(worker.reader, encoded[:]); err != nil {
				return fmt.Errorf("prepared batch worker: read result: %w", err)
			}
			result = strconv.FormatInt(int64(binary.BigEndian.Uint64(encoded[:])), 10)
			return nil
		},
	)
	if err != nil {
		return "", err
	}
	return result, nil
}

func (worker *preparedBatchWorker) Transact(
	ctx context.Context,
	request preparedworkerfixture.BatchRequest,
) (preparedworkerfixture.BatchResult, error) {
	if request.Iterations == 0 || uint64(request.Iterations) > uint64(^uint(0)>>1) {
		return preparedworkerfixture.BatchResult{}, fmt.Errorf(
			"prepared batch worker: iterations %d are out of bounds",
			request.Iterations,
		)
	}
	result, err := worker.Call(ctx, int(request.Iterations), request.Seed)
	if err != nil {
		return preparedworkerfixture.BatchResult{}, err
	}
	checksum, err := strconv.ParseInt(result, 10, 64)
	if err != nil {
		return preparedworkerfixture.BatchResult{}, fmt.Errorf("prepared batch worker: checksum: %w", err)
	}
	return preparedworkerfixture.BatchResult{Checksum: checksum}, nil
}

func (worker *preparedBatchWorker) Close() error {
	if worker == nil {
		return nil
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if worker.closed {
		return nil
	}
	worker.closed = true
	inputErr := worker.input.Close()
	waitErr := waitPreparedWorkerCommand(worker.command, preparedWorkerCloseTimeout)
	outputErr := worker.output.Close()
	if waitErr != nil && !worker.aborted {
		return fmt.Errorf("prepared batch worker: wait: %w: %s", waitErr, worker.stderr.String())
	}
	if inputErr != nil && !errors.Is(inputErr, os.ErrClosed) {
		return fmt.Errorf("prepared batch worker: close input: %w", inputErr)
	}
	if outputErr != nil && !errors.Is(outputErr, os.ErrClosed) {
		return fmt.Errorf("prepared batch worker: close output: %w", outputErr)
	}
	return nil
}

func performPreparedWorkerIO(ctx context.Context, abort func() error, operation func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	completed := make(chan error, 1)
	go func() { completed <- operation() }()
	select {
	case err := <-completed:
		return err
	case <-ctx.Done():
		return errors.Join(ctx.Err(), abort())
	}
}

func servePreparedBatchWorker(reader io.Reader, writer io.Writer, callable *parityPreparedCallable) error {
	bufferedReader := bufio.NewReader(reader)
	bufferedWriter := bufio.NewWriter(writer)
	if _, err := bufferedWriter.WriteString(preparedBatchWorkerMagic); err != nil {
		return err
	}
	if err := bufferedWriter.Flush(); err != nil {
		return err
	}
	for {
		var request [16]byte
		read, err := io.ReadFull(bufferedReader, request[:])
		if errors.Is(err, io.EOF) && read == 0 {
			return nil
		}
		if err != nil {
			return err
		}
		iterations := binary.BigEndian.Uint64(request[:8])
		if iterations == 0 || iterations > uint64(^uint(0)>>1) {
			if err := writePreparedBatchWorkerError(bufferedWriter, fmt.Errorf("invalid iterations %d", iterations)); err != nil {
				return err
			}
			continue
		}
		seed := int64(binary.BigEndian.Uint64(request[8:]))
		values, err := callable.callBatch(int(iterations), seed)
		if err != nil {
			if err := writePreparedBatchWorkerError(bufferedWriter, err); err != nil {
				return err
			}
			continue
		}
		result, err := parityExactIntegerResult(values)
		if err != nil {
			if err := writePreparedBatchWorkerError(bufferedWriter, err); err != nil {
				return err
			}
			continue
		}
		integer, err := strconv.ParseInt(result, 10, 64)
		if err != nil {
			return err
		}
		var response [9]byte
		binary.BigEndian.PutUint64(response[1:], uint64(integer))
		if _, err := bufferedWriter.Write(response[:]); err != nil {
			return err
		}
		if err := bufferedWriter.Flush(); err != nil {
			return err
		}
	}
}

func writePreparedBatchWorkerError(writer *bufio.Writer, resultErr error) error {
	message := resultErr.Error()
	if len(message) > preparedBatchWorkerMaxError {
		return fmt.Errorf("prepared batch worker: error exceeds %d bytes", preparedBatchWorkerMaxError)
	}
	var header [3]byte
	header[0] = 1
	binary.BigEndian.PutUint16(header[1:], uint16(len(message)))
	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	if _, err := writer.WriteString(message); err != nil {
		return err
	}
	return writer.Flush()
}

func readPreparedBatchWorkerError(reader *bufio.Reader) (string, error) {
	var encoded [2]byte
	if _, err := io.ReadFull(reader, encoded[:]); err != nil {
		return "", err
	}
	size := int(binary.BigEndian.Uint16(encoded[:]))
	if size == 0 || size > preparedBatchWorkerMaxError {
		return "", fmt.Errorf("prepared batch worker: error size %d is invalid", size)
	}
	message := make([]byte, size)
	if _, err := io.ReadFull(reader, message); err != nil {
		return "", err
	}
	return string(message), nil
}

func preparedWorkerParityEntry(t testing.TB, caseID string) parityManifestEntry {
	t.Helper()
	selected, err := parityManifestSelection(caseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].Corpus+"/"+selected[0].Name != caseID {
		t.Fatalf("prepared worker case selection = %#v, want exactly %s", selected, caseID)
	}
	return selected[0]
}

func normalizePreparedWorkerParitySamples(samples map[int]float64) (map[int]float64, error) {
	if len(samples) != len(parityIterations) {
		return nil, fmt.Errorf(
			"prepared worker parity window: got %d points, want %d",
			len(samples),
			len(parityIterations),
		)
	}
	normalized := make(map[int]float64, len(parityIterations))
	for _, base := range parityIterations {
		n := base * preparedWorkerParityCallScale
		elapsed, ok := samples[n]
		if !ok {
			return nil, fmt.Errorf("prepared worker parity window: missing N=%d", n)
		}
		normalized[base] = elapsed
	}
	return normalized, nil
}

func validatePreparedWorkerParityWindow(samples map[int]float64) error {
	normalized, err := normalizePreparedWorkerParitySamples(samples)
	if err != nil {
		return err
	}
	return validateParityMeasurementWindow(normalized)
}

func fitPreparedWorkerParityLine(samples map[int]float64) (parityFit, error) {
	normalized, err := normalizePreparedWorkerParitySamples(samples)
	if err != nil {
		return parityFit{}, err
	}
	fit, err := fitParityLine(normalized)
	if err != nil {
		return parityFit{}, err
	}
	fit.Inner /= preparedWorkerParityCallScale
	return fit, nil
}

func preparedWorkerResultSetSHA256(results map[int]string) (string, error) {
	var builder strings.Builder
	for _, base := range parityIterations {
		n := base * preparedWorkerParityCallScale
		result, ok := results[n]
		if !ok || result == "" {
			return "", fmt.Errorf("prepared worker result set: missing N=%d", n)
		}
		fmt.Fprintf(&builder, "%d=%s\n", n, result)
	}
	return parityStringSHA256(builder.String()), nil
}

type preparedWorkerAdmissionSummary struct {
	WorkerLuauMedian   float64
	WorkerLuauP90      float64
	EmbeddedLuauMedian float64
	EmbeddedLuauP90    float64
	WorkerEmbeddedMax  float64
}

func preparedWorkerAdmissionGate(
	embedded []float64,
	worker []float64,
	luau []float64,
) (preparedWorkerAdmissionSummary, error) {
	// This corpus gate answers whether both deployments retain the Luau target.
	// The stricter matched worker/embedded target belongs to the typed host turn,
	// where one exchange and identical application work can be paired directly.
	if len(embedded) != parityRepeatCount || len(worker) != parityRepeatCount || len(luau) != parityRepeatCount {
		return preparedWorkerAdmissionSummary{}, fmt.Errorf(
			"prepared worker gate: want %d slopes per engine",
			parityRepeatCount,
		)
	}
	workerLuau, err := preparedWorkerCrossRatios(worker, luau)
	if err != nil {
		return preparedWorkerAdmissionSummary{}, err
	}
	embeddedLuau, err := preparedWorkerCrossRatios(embedded, luau)
	if err != nil {
		return preparedWorkerAdmissionSummary{}, err
	}
	summary := preparedWorkerAdmissionSummary{
		WorkerLuauMedian:   workerLuau[4],
		WorkerLuauP90:      workerLuau[8],
		EmbeddedLuauMedian: embeddedLuau[4],
		EmbeddedLuauP90:    embeddedLuau[8],
	}
	for index := range worker {
		if embedded[index] <= 0 || !finiteParityFloat(embedded[index]) ||
			worker[index] <= 0 || !finiteParityFloat(worker[index]) {
			return preparedWorkerAdmissionSummary{}, fmt.Errorf("prepared worker gate: invalid matched slope %d", index+1)
		}
		ratio := worker[index] / embedded[index]
		if ratio > summary.WorkerEmbeddedMax {
			summary.WorkerEmbeddedMax = ratio
		}
	}
	if summary.WorkerLuauMedian > 1.00 || summary.WorkerLuauP90 > 1.05 {
		return summary, fmt.Errorf(
			"prepared worker gate: worker/Luau median/p90 %.6f/%.6f exceeds 1.00/1.05",
			summary.WorkerLuauMedian,
			summary.WorkerLuauP90,
		)
	}
	if summary.EmbeddedLuauMedian > 1.00 || summary.EmbeddedLuauP90 > 1.05 {
		return summary, fmt.Errorf(
			"prepared worker gate: embedded/Luau median/p90 %.6f/%.6f exceeds 1.00/1.05",
			summary.EmbeddedLuauMedian,
			summary.EmbeddedLuauP90,
		)
	}
	return summary, nil
}

func preparedWorkerCrossRatios(left, right []float64) ([]float64, error) {
	ratios := make([]float64, 0, len(left)*len(right))
	for _, numerator := range left {
		if numerator <= 0 || !finiteParityFloat(numerator) {
			return nil, fmt.Errorf("prepared worker gate: invalid numerator slope %v", numerator)
		}
		for _, denominator := range right {
			if denominator <= 0 || !finiteParityFloat(denominator) {
				return nil, fmt.Errorf("prepared worker gate: invalid denominator slope %v", denominator)
			}
			ratios = append(ratios, numerator/denominator)
		}
	}
	sort.Float64s(ratios)
	return ratios, nil
}

func preparedWorkerEngineOrder(pair, repeat, iterationIndex int) [3]string {
	orders := [...][3]string{
		{"embedded", "worker", "luau"},
		{"worker", "luau", "embedded"},
		{"luau", "embedded", "worker"},
	}
	return orders[(pair+repeat+iterationIndex)%len(orders)]
}

func measurePreparedBatchWorker(
	worker *preparedBatchWorker,
	iterations int,
	seed int64,
) (float64, string, error) {
	start := time.Now()
	result, err := worker.Call(context.Background(), iterations, seed)
	elapsed := time.Since(start)
	return float64(elapsed.Nanoseconds()), result, err
}

func createPreparedWorkerCaptureFile(t testing.TB, path string) *os.File {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("create prepared worker capture file %s: %v", path, err)
	}
	return file
}

func writePreparedWorkerCapture(t testing.TB, writer io.Writer, format string, args ...any) {
	t.Helper()
	if _, err := fmt.Fprintf(writer, format, args...); err != nil {
		t.Fatalf("write prepared worker capture: %v", err)
	}
}
