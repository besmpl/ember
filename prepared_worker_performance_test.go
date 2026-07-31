package ember_test

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/besmpl/ember/preparedworker"
)

const (
	preparedWorkerAdmissionEnvironment  = "EMBER_PREPARED_WORKER_ADMISSION_LIVE"
	preparedWorkerScheduleEnvironment   = "PREPARED_WORKER_SCHEDULE"
	preparedWorkerParityCalibrationRuns = 3
	preparedWorkerParityMaximumScale    = 1024
	preparedWorkerParityTarget          = 10 * time.Millisecond
	preparedWorkerRepeatAttemptLimit    = 3
)

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
	const callScale = 32
	samples := make(map[int]float64, len(parityIterations))
	for _, base := range parityIterations {
		n := base * callScale
		samples[n] = float64(n * 4)
	}
	if err := validatePreparedWorkerParityWindow(samples, callScale); err != nil {
		t.Fatal(err)
	}
	fit, err := fitPreparedWorkerParityLine(samples, callScale)
	if err != nil {
		t.Fatal(err)
	}
	if fit.Inner != 4 || fit.Entry != 0 {
		t.Fatalf("scaled fit = %#v, want 4ns per call with zero intercept", fit)
	}
}

func TestPreparedWorkerParityCallScaleClearsNoiseFloor(t *testing.T) {
	calls := 0
	calibration, err := selectPreparedWorkerParityCallScale(func(iterations int) (float64, error) {
		calls++
		return float64(iterations * 7), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calibration.Scale != 32 || len(calibration.Samples) != 18 || calls != 18 {
		t.Fatalf("calibration = %#v after %d calls, want scale 32 after 18 calls", calibration, calls)
	}
}

func TestPreparedWorkerParityPrescribedScaleMustClearEvidenceFloor(t *testing.T) {
	calibration, err := verifyPreparedWorkerParityCallScale(4, func(iterations int) (float64, error) {
		return float64(iterations * 60), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calibration.Scale != 4 || len(calibration.Samples) != preparedWorkerParityCalibrationRuns {
		t.Fatalf("prescribed calibration = %#v", calibration)
	}
	if _, err := verifyPreparedWorkerParityCallScale(1, func(int) (float64, error) {
		return float64(parityMinimumMaxPointElapsed.Nanoseconds()), nil
	}); err != nil {
		t.Fatalf("prescribed window at evidence floor failed: %v", err)
	}
	if _, err := verifyPreparedWorkerParityCallScale(2, func(iterations int) (float64, error) {
		return float64(iterations), nil
	}); err == nil {
		t.Fatal("under-scaled prescribed window passed")
	}
	if _, err := verifyPreparedWorkerParityCallScale(1, func(int) (float64, error) {
		return float64(parityMinimumMaxPointElapsed.Nanoseconds() - 1), nil
	}); err == nil {
		t.Fatal("prescribed window below evidence floor passed")
	}
}

func TestPreparedWorkerParityCallScaleUsesConservativeTrial(t *testing.T) {
	trial := 0
	calibration, err := selectPreparedWorkerParityCallScale(func(iterations int) (float64, error) {
		trial++
		if iterations == parityIterations[len(parityIterations)-1] && trial == 3 {
			return float64(preparedWorkerParityTarget.Nanoseconds() - 1), nil
		}
		return float64(preparedWorkerParityTarget.Nanoseconds()), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calibration.Scale != 2 || len(calibration.Samples) != 2*preparedWorkerParityCalibrationRuns {
		t.Fatalf("calibration = %#v, want scale 2 after one low trial at scale 1", calibration)
	}
}

func TestPreparedWorkerParityCallScaleRejectsUnmeasurableWindow(t *testing.T) {
	calibration, err := selectPreparedWorkerParityCallScale(func(int) (float64, error) {
		return 1, nil
	})
	if err == nil {
		t.Fatalf("calibration = %#v, want maximum-scale failure", calibration)
	}
	wantSamples := preparedWorkerParityCalibrationRuns * 11 // powers of two from 1 through 1024
	if len(calibration.Samples) != wantSamples {
		t.Fatalf("calibration recorded %d samples, want %d", len(calibration.Samples), wantSamples)
	}
}

func TestPreparedWorkerParityRepeatReacquiresStructurallyInvalidWindow(t *testing.T) {
	attempts := 0
	waits := 0
	got, err := acquirePreparedWorkerRepeat(
		preparedWorkerRepeatAttemptLimit,
		func() (int, error) {
			attempts++
			return attempts, nil
		},
		func(attempt int) error {
			if attempt == 1 {
				return fmt.Errorf("non-positive fitted slope")
			}
			return nil
		},
		func() { waits++ },
	)
	if err != nil || got != 2 || attempts != 2 || waits != 1 {
		t.Fatalf("reacquired repeat = %d attempts=%d waits=%d error=%v", got, attempts, waits, err)
	}

	attempts = 0
	if _, err := acquirePreparedWorkerRepeat(
		2,
		func() (int, error) {
			attempts++
			return attempts, nil
		},
		func(int) error { return fmt.Errorf("invalid fit") },
		func() {},
	); err == nil || attempts != 2 {
		t.Fatalf("repeat exhaustion attempts=%d error=%v", attempts, err)
	}

	attempts = 0
	if _, err := acquirePreparedWorkerRepeat(
		preparedWorkerRepeatAttemptLimit,
		func() (int, error) {
			attempts++
			return 0, fmt.Errorf("semantic failure")
		},
		func(int) error { return nil },
		func() {},
	); err == nil || attempts != 1 {
		t.Fatalf("acquisition failure attempts=%d error=%v", attempts, err)
	}
}

func acquirePreparedWorkerRepeat[T any](
	maxAttempts int,
	acquire func() (T, error),
	validate func(T) error,
	wait func(),
) (T, error) {
	var zero T
	if maxAttempts <= 0 || acquire == nil || validate == nil || wait == nil {
		return zero, fmt.Errorf("prepared worker repeat: invalid acquisition policy")
	}
	var last error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		repeat, err := acquire()
		if err != nil {
			return zero, err
		}
		if err := validate(repeat); err == nil {
			return repeat, nil
		} else {
			last = err
		}
		if attempt < maxAttempts {
			wait()
		}
	}
	return zero, fmt.Errorf(
		"prepared worker repeat: structural validation failed after %d attempts: %w",
		maxAttempts,
		last,
	)
}

type preparedWorkerParityRecordedMeasurement struct {
	iterationIndex int
	n              int
	measurement    parityPointMeasurement
}

type preparedWorkerParityRepeat struct {
	timings      map[string]map[int]float64
	results      map[string]map[int]string
	measurements []preparedWorkerParityRecordedMeasurement
}

func newPreparedWorkerParityRepeat() preparedWorkerParityRepeat {
	repeat := preparedWorkerParityRepeat{
		timings:      make(map[string]map[int]float64, 3),
		results:      make(map[string]map[int]string, 3),
		measurements: make([]preparedWorkerParityRecordedMeasurement, 0, len(parityIterations)*3),
	}
	for _, engine := range []string{"embedded", "worker", "luau"} {
		repeat.timings[engine] = make(map[int]float64, len(parityIterations))
		repeat.results[engine] = make(map[int]string, len(parityIterations))
	}
	return repeat
}

func validatePreparedWorkerParityRepeat(repeat preparedWorkerParityRepeat, callScale int) error {
	for _, engine := range []string{"embedded", "worker", "luau"} {
		if err := validatePreparedWorkerParityWindow(repeat.timings[engine], callScale); err != nil {
			return fmt.Errorf("engine=%s: %w", engine, err)
		}
		if _, err := fitPreparedWorkerParityLine(repeat.timings[engine], callScale); err != nil {
			return fmt.Errorf("engine=%s: %w", engine, err)
		}
		if _, err := preparedWorkerResultSetSHA256(repeat.results[engine], callScale); err != nil {
			return fmt.Errorf("engine=%s: %w", engine, err)
		}
	}
	return nil
}

func TestPreparedWorkerParityScheduleIsClosedAndCanonical(t *testing.T) {
	selected, err := parityManifestSelection("classic/recursive_fibonacci")
	if err != nil {
		t.Fatal(err)
	}
	const valid = "corpus\tname\tcall_scale\nclassic\trecursive_fibonacci\t32\n"
	schedule, err := parsePreparedWorkerParitySchedule([]byte(valid), selected)
	if err != nil {
		t.Fatal(err)
	}
	if got := schedule["classic/recursive_fibonacci"]; got != 32 {
		t.Fatalf("scheduled scale = %d, want 32", got)
	}
	for _, invalid := range []string{
		"corpus\tname\tcall_scale\nclassic\trecursive_fibonacci\t032\n",
		"corpus\tname\tcall_scale\nclassic\trecursive_fibonacci\t32.0\n",
		"corpus\tname\tcall_scale\nclassic\trecursive_fibonacci\t32e0\n",
		"corpus\tname\tcall_scale\nclassic\trecursive_fibonacci\t+32\n",
		"corpus\tname\tcall_scale\nclassic\trecursive_fibonacci\t3\n",
		"corpus\tname\tcall_scale\nclassic\tunknown\t32\n",
	} {
		if _, err := parsePreparedWorkerParitySchedule([]byte(invalid), selected); err == nil {
			t.Fatalf("invalid schedule passed: %q", invalid)
		}
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
	scheduledScales, calibrationMode, err := loadPreparedWorkerParitySchedule(
		pair,
		os.Getenv(preparedWorkerScheduleEnvironment),
		selected,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatalf("create prepared worker capture: %v", err)
	}
	capture := preparedWorkerCaptureContext{
		ID:              captureID,
		Pair:            pair,
		SourceCommit:    sourceCommit,
		EnvironmentHash: environmentHash,
		Output:          output,
		LuauPath:        environment.LuauPath,
	}
	caseIndices := preparedWorkerParityCaseIndices()
	artifact := preparedWorkerParityArtifact(t, parityDefaultFixtureVariant)
	publication := buildPreparedWorkerParityPublication(
		t, artifact, t.TempDir(), "ember-epw2-parity-admission",
	)
	embeddedClient := openPreparedWorkerEmbeddedObserver(t, capture)
	processRunner, _, err := preparedworker.OpenDevelopment(
		context.Background(),
		preparedWorkerParityRunnerOptions(t, "admission-process"),
		publication.build,
	)
	if err != nil {
		if closeErr := embeddedClient.Close(); closeErr != nil {
			t.Errorf("close embedded observer after process open failed: %v", closeErr)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := embeddedClient.Close(); err != nil {
			t.Errorf("close embedded observer: %v", err)
		}
		closePreparedWorkerParityRunner(t, processRunner)
	})
	processClient := preparedWorkerParityClient{runner: processRunner}
	for _, entry := range selected {
		caseID := entry.Corpus + "/" + entry.Name
		caseIndex, ok := caseIndices[caseID]
		if !ok {
			t.Fatalf("%s is absent from the combined prepared worker Program", caseID)
		}
		if _, _, err := embeddedClient.call(caseIndex, 1, parityCaptureSeed); err != nil {
			t.Fatalf("warm embedded %s: %v", caseID, err)
		}
		if _, _, err := processClient.call(caseIndex, 1, parityCaptureSeed); err != nil {
			t.Fatalf("warm process %s: %v", caseID, err)
		}
	}
	raw := createPreparedWorkerCaptureFile(t, filepath.Join(output, "raw.tsv"))
	defer raw.Close()
	slopes := createPreparedWorkerCaptureFile(t, filepath.Join(output, "slopes.tsv"))
	defer slopes.Close()
	summaryFile := createPreparedWorkerCaptureFile(t, filepath.Join(output, "summary.tsv"))
	defer summaryFile.Close()
	scheduleFile := createPreparedWorkerCaptureFile(t, filepath.Join(output, "parity-schedule.tsv"))
	defer scheduleFile.Close()
	calibrationFile := createPreparedWorkerCaptureFile(t, filepath.Join(output, "parity-calibration.tsv"))
	defer calibrationFile.Close()
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
	writePreparedWorkerCapture(t, raw, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tcorpus\tname\tcall_scale\tengine\trepeat\torder\tn\tseed\telapsed_ns\tresult\tworkload_sha256\tprogram_sha256\tenvironment_sha256\n")
	writePreparedWorkerCapture(t, slopes, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tcorpus\tname\tcall_scale\tengine\trepeat\tslope_ns_per_guest_call\tintercept_ns\tresult_set_sha256\tworkload_sha256\tprogram_sha256\tenvironment_sha256\n")
	writePreparedWorkerCapture(t, summaryFile, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tcorpus\tname\tcall_scale\tworker_luau_median\tworker_luau_p90\tembedded_luau_median\tembedded_luau_p90\tworker_embedded_max\tstatus\tenvironment_sha256\n")
	writePreparedWorkerCapture(t, scheduleFile, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tcorpus\tname\tcall_scale\tenvironment_sha256\n")
	writePreparedWorkerCapture(t, calibrationFile, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tcorpus\tname\tmode\tcall_scale\ttrial\telapsed_ns\tselected\tenvironment_sha256\n")
	writePreparedWorkerParityBuildEvidence(t, output, capture, publication)
	capturePreparedWorkerHostAdmission(t, capture, &processClient)

	pairIndex := 1
	if pair == "b" {
		pairIndex = 2
	}
	var gateFailures []string
	for _, entry := range selected {
		caseID := entry.Corpus + "/" + entry.Name
		caseIndex := caseIndices[caseID]
		programSource, seededSource, err := runtimeParityGuestBatchProgram(entry.Case.source, parityDefaultFixtureVariant)
		if err != nil {
			t.Fatalf("%s build guest batch: %v", caseID, err)
		}
		var calibration preparedWorkerParityCalibration
		if calibrationMode == "adaptive" {
			calibration, err = calibratePreparedWorkerParityClientScale(embeddedClient, caseIndex)
		} else {
			calibration, err = verifyPreparedWorkerParityClientScale(
				scheduledScales[caseID], embeddedClient, caseIndex,
			)
		}
		if err != nil {
			t.Fatalf("%s calibrate measurement window: %v", caseID, err)
		}
		callScale := calibration.Scale
		// Calibration already gives the embedded adapter three maximum-point
		// calls. Give the persistent process adapter the same heap/GC ramp-up so
		// its first fitted repeat does not measure construction-era growth that
		// the guest-throughput contract deliberately excludes.
		maximumN := parityIterations[len(parityIterations)-1] * callScale
		for trial := 1; trial <= preparedWorkerParityCalibrationRuns; trial++ {
			if _, _, err := processClient.call(caseIndex, maximumN, parityCaptureSeed); err != nil {
				t.Fatalf("%s warm process measurement window trial %d: %v", caseID, trial, err)
			}
		}
		writePreparedWorkerCapture(t, scheduleFile, "1\t%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			captureID,
			pair,
			sourceCommit,
			entry.Corpus,
			entry.Name,
			callScale,
			environmentHash,
		)
		for _, sample := range calibration.Samples {
			writePreparedWorkerCapture(t, calibrationFile, "1\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%d\t%.17g\t%s\t%s\n",
				captureID,
				pair,
				sourceCommit,
				entry.Corpus,
				entry.Name,
				calibrationMode,
				sample.Scale,
				sample.Trial,
				sample.Elapsed,
				strconv.FormatBool(sample.Scale == callScale),
				environmentHash,
			)
		}
		luauSource, err := parityGuestBatchLuauSource(entry.Case.source, parityDefaultFixtureVariant)
		if err != nil {
			t.Fatalf("%s build Luau batch: %v", caseID, err)
		}
		scriptPath := filepath.Join(output, "scripts", entry.Corpus+"-"+entry.Name+".luau")
		if err := os.MkdirAll(filepath.Dir(scriptPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(scriptPath, []byte(luauSource), 0o700); err != nil {
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
			acceptedRepeat, err := acquirePreparedWorkerRepeat(
				preparedWorkerRepeatAttemptLimit,
				func() (preparedWorkerParityRepeat, error) {
					candidateRepeat := newPreparedWorkerParityRepeat()
					for iterationIndex, baseN := range parityIterations {
						n := baseN * callScale
						order := preparedWorkerEngineOrder(pairIndex, repeat, iterationIndex)
						point, err := acquireCleanParityPoint(parityPointAttemptLimit, sampleParitySystem, func() ([]parityPointMeasurement, error) {
							measurements := make([]parityPointMeasurement, 0, len(order))
							for engineIndex, engine := range order {
								var elapsed float64
								var result string
								var measureErr error
								switch engine {
								case "embedded":
									elapsed, result, measureErr = embeddedClient.call(caseIndex, n, parityCaptureSeed)
								case "worker":
									elapsed, result, measureErr = processClient.call(caseIndex, n, parityCaptureSeed)
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
							return preparedWorkerParityRepeat{}, fmt.Errorf("N=%d: %w", n, err)
						}
						for _, measurement := range point {
							candidateRepeat.timings[measurement.engine][n] = measurement.elapsed
							candidateRepeat.results[measurement.engine][n] = measurement.result
							candidateRepeat.measurements = append(candidateRepeat.measurements, preparedWorkerParityRecordedMeasurement{
								iterationIndex: iterationIndex,
								n:              n,
								measurement:    measurement,
							})
						}
					}
					return candidateRepeat, nil
				},
				func(candidateRepeat preparedWorkerParityRepeat) error {
					return validatePreparedWorkerParityRepeat(candidateRepeat, callScale)
				},
				func() { time.Sleep(parityPointRetryDelay) },
			)
			if err != nil {
				t.Fatalf("%s repeat=%d: %v", caseID, repeat, err)
			}
			for _, engine := range engines {
				timings[engine][repeat] = acceptedRepeat.timings[engine]
				results[engine][repeat] = acceptedRepeat.results[engine]
			}
			for _, recorded := range acceptedRepeat.measurements {
				measurement := recorded.measurement
				acquisitionOrder := (repeat-1)*len(parityIterations)*3 + recorded.iterationIndex*3 + measurement.engineIndex + 1
				writePreparedWorkerCapture(t, raw, "1\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%d\t%d\t%d\t%d\t%.17g\t%s\t%s\t%s\t%s\n",
					captureID,
					pair,
					sourceCommit,
					entry.Corpus,
					entry.Name,
					callScale,
					measurement.engine,
					repeat,
					acquisitionOrder,
					recorded.n,
					parityCaptureSeed,
					measurement.elapsed,
					measurement.result,
					workloadHash,
					programHash,
					environmentHash,
				)
			}
		}

		fits := make(map[string][]float64, len(engines))
		for _, engine := range engines {
			fits[engine] = make([]float64, parityRepeatCount)
			for repeat := 1; repeat <= parityRepeatCount; repeat++ {
				if err := validatePreparedWorkerParityWindow(timings[engine][repeat], callScale); err != nil {
					t.Fatalf("%s %s repeat=%d: %v", caseID, engine, repeat, err)
				}
				fit, err := fitPreparedWorkerParityLine(timings[engine][repeat], callScale)
				if err != nil {
					t.Fatalf("%s %s repeat=%d: %v", caseID, engine, repeat, err)
				}
				fits[engine][repeat-1] = fit.Inner
				resultHash, err := preparedWorkerResultSetSHA256(results[engine][repeat], callScale)
				if err != nil {
					t.Fatal(err)
				}
				writePreparedWorkerCapture(t, slopes, "1\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%d\t%.17g\t%.17g\t%s\t%s\t%s\t%s\n",
					captureID,
					pair,
					sourceCommit,
					entry.Corpus,
					entry.Name,
					callScale,
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
		writePreparedWorkerCapture(t, summaryFile, "1\t%s\t%s\t%s\t%s\t%s\t%d\t%.17g\t%.17g\t%.17g\t%.17g\t%.17g\t%s\t%s\n",
			captureID,
			pair,
			sourceCommit,
			entry.Corpus,
			entry.Name,
			callScale,
			caseSummary.WorkerLuauMedian,
			caseSummary.WorkerLuauP90,
			caseSummary.EmbeddedLuauMedian,
			caseSummary.EmbeddedLuauP90,
			caseSummary.WorkerEmbeddedMax,
			status,
			environmentHash,
		)
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

type preparedWorkerParityCalibrationSample struct {
	Scale   int
	Trial   int
	Elapsed float64
}

type preparedWorkerParityCalibration struct {
	Scale   int
	Samples []preparedWorkerParityCalibrationSample
}

func selectPreparedWorkerParityCallScale(measure func(int) (float64, error)) (preparedWorkerParityCalibration, error) {
	calibration := preparedWorkerParityCalibration{}
	if measure == nil {
		return calibration, fmt.Errorf("prepared worker parity calibration: nil measurement")
	}
	target := float64(preparedWorkerParityTarget.Nanoseconds())
	maximumN := parityIterations[len(parityIterations)-1]
	for scale := 1; scale <= preparedWorkerParityMaximumScale; scale *= 2 {
		minimum := float64(0)
		for trial := 1; trial <= preparedWorkerParityCalibrationRuns; trial++ {
			elapsed, err := measure(maximumN * scale)
			if err != nil {
				return calibration, err
			}
			if elapsed <= 0 || !finiteParityFloat(elapsed) {
				return calibration, fmt.Errorf("prepared worker parity calibration: invalid timing %v", elapsed)
			}
			calibration.Samples = append(calibration.Samples, preparedWorkerParityCalibrationSample{
				Scale:   scale,
				Trial:   trial,
				Elapsed: elapsed,
			})
			if minimum == 0 || elapsed < minimum {
				minimum = elapsed
			}
		}
		if minimum >= target {
			calibration.Scale = scale
			return calibration, nil
		}
	}
	return calibration, fmt.Errorf(
		"prepared worker parity calibration: maximum scale %d did not reach %s",
		preparedWorkerParityMaximumScale,
		preparedWorkerParityTarget,
	)
}

func verifyPreparedWorkerParityCallScale(
	callScale int,
	measure func(int) (float64, error),
) (preparedWorkerParityCalibration, error) {
	calibration := preparedWorkerParityCalibration{Scale: callScale}
	if err := validatePreparedWorkerParityCallScale(callScale); err != nil {
		return calibration, err
	}
	if measure == nil {
		return calibration, fmt.Errorf("prepared worker parity calibration: nil measurement")
	}
	minimum := float64(0)
	maximumN := parityIterations[len(parityIterations)-1]
	for trial := 1; trial <= preparedWorkerParityCalibrationRuns; trial++ {
		elapsed, err := measure(maximumN * callScale)
		if err != nil {
			return calibration, err
		}
		if elapsed <= 0 || !finiteParityFloat(elapsed) {
			return calibration, fmt.Errorf("prepared worker parity calibration: invalid timing %v", elapsed)
		}
		calibration.Samples = append(calibration.Samples, preparedWorkerParityCalibrationSample{
			Scale:   callScale,
			Trial:   trial,
			Elapsed: elapsed,
		})
		if minimum == 0 || elapsed < minimum {
			minimum = elapsed
		}
	}
	if minimum < float64(parityMinimumMaxPointElapsed.Nanoseconds()) {
		return calibration, fmt.Errorf(
			"prepared worker parity calibration: prescribed scale %d minimum %gns is below %s",
			callScale,
			minimum,
			parityMinimumMaxPointElapsed,
		)
	}
	return calibration, nil
}

func validatePreparedWorkerParityCallScale(callScale int) error {
	if callScale <= 0 || callScale > preparedWorkerParityMaximumScale || callScale&(callScale-1) != 0 {
		return fmt.Errorf("prepared worker parity window: invalid call scale %d", callScale)
	}
	return nil
}

func loadPreparedWorkerParitySchedule(
	pair string,
	path string,
	selected []parityManifestEntry,
) (map[string]int, string, error) {
	switch pair {
	case "a":
		if path != "" {
			return nil, "", fmt.Errorf("prepared worker parity schedule: capture A must adapt independently")
		}
		return nil, "adaptive", nil
	case "b":
		if path == "" {
			return nil, "", fmt.Errorf("prepared worker parity schedule: capture B requires Capture A's schedule")
		}
	default:
		return nil, "", fmt.Errorf("prepared worker parity schedule: invalid capture pair %q", pair)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("prepared worker parity schedule: %w", err)
	}
	schedule, err := parsePreparedWorkerParitySchedule(data, selected)
	if err != nil {
		return nil, "", err
	}
	return schedule, "prescribed", nil
}

func parsePreparedWorkerParitySchedule(
	data []byte,
	selected []parityManifestEntry,
) (map[string]int, error) {
	const header = "corpus\tname\tcall_scale"
	wanted := make(map[string]struct{}, len(selected))
	for _, entry := range selected {
		wanted[entry.Corpus+"/"+entry.Name] = struct{}{}
	}
	schedule := make(map[string]int, len(selected))
	scanner := bufio.NewScanner(bytes.NewReader(data))
	line := 0
	for scanner.Scan() {
		line++
		if line == 1 {
			if scanner.Text() != header {
				return nil, fmt.Errorf("prepared worker parity schedule: invalid header")
			}
			continue
		}
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) != 3 || fields[0] == "" || fields[1] == "" {
			return nil, fmt.Errorf("prepared worker parity schedule: line %d is invalid", line)
		}
		caseID := fields[0] + "/" + fields[1]
		if _, ok := wanted[caseID]; !ok {
			return nil, fmt.Errorf("prepared worker parity schedule: unexpected case %s", caseID)
		}
		if _, exists := schedule[caseID]; exists {
			return nil, fmt.Errorf("prepared worker parity schedule: duplicate case %s", caseID)
		}
		callScale, err := strconv.Atoi(fields[2])
		if err != nil || strconv.Itoa(callScale) != fields[2] {
			return nil, fmt.Errorf("prepared worker parity schedule: scale %q is not canonical", fields[2])
		}
		if err := validatePreparedWorkerParityCallScale(callScale); err != nil {
			return nil, err
		}
		schedule[caseID] = callScale
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("prepared worker parity schedule: %w", err)
	}
	if line == 0 {
		return nil, fmt.Errorf("prepared worker parity schedule: missing header")
	}
	if len(schedule) != len(wanted) {
		return nil, fmt.Errorf(
			"prepared worker parity schedule: got %d cases, want %d",
			len(schedule),
			len(wanted),
		)
	}
	return schedule, nil
}

func normalizePreparedWorkerParitySamples(samples map[int]float64, callScale int) (map[int]float64, error) {
	if err := validatePreparedWorkerParityCallScale(callScale); err != nil {
		return nil, err
	}
	if len(samples) != len(parityIterations) {
		return nil, fmt.Errorf(
			"prepared worker parity window: got %d points, want %d",
			len(samples),
			len(parityIterations),
		)
	}
	normalized := make(map[int]float64, len(parityIterations))
	for _, base := range parityIterations {
		n := base * callScale
		elapsed, ok := samples[n]
		if !ok {
			return nil, fmt.Errorf("prepared worker parity window: missing N=%d", n)
		}
		normalized[base] = elapsed
	}
	return normalized, nil
}

func validatePreparedWorkerParityWindow(samples map[int]float64, callScale int) error {
	normalized, err := normalizePreparedWorkerParitySamples(samples, callScale)
	if err != nil {
		return err
	}
	return validateParityMeasurementWindow(normalized)
}

func fitPreparedWorkerParityLine(samples map[int]float64, callScale int) (parityFit, error) {
	normalized, err := normalizePreparedWorkerParitySamples(samples, callScale)
	if err != nil {
		return parityFit{}, err
	}
	fit, err := fitParityLine(normalized)
	if err != nil {
		return parityFit{}, err
	}
	fit.Inner /= float64(callScale)
	return fit, nil
}

func preparedWorkerResultSetSHA256(results map[int]string, callScale int) (string, error) {
	if err := validatePreparedWorkerParityCallScale(callScale); err != nil {
		return "", err
	}
	var builder strings.Builder
	for _, base := range parityIterations {
		n := base * callScale
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
