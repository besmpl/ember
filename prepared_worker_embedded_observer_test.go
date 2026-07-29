package ember_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	workerartifact "github.com/besmpl/ember/internal/preparedworkerartifact"
)

const (
	preparedWorkerEmbeddedObserverPathEnvironment   = "PREPARED_WORKER_EMBEDDED_OBSERVER"
	preparedWorkerEmbeddedObserverDigestEnvironment = "PREPARED_WORKER_EMBEDDED_OBSERVER_SHA256"
	preparedWorkerEmbeddedObserverReady             = "EMBER-EMBEDDED-OBSERVER-1"
	preparedWorkerEmbeddedObserverMaxBytes          = 256 << 20
)

type preparedWorkerParityCaller interface {
	call(caseIndex uint16, iterations int, seed int64) (float64, string, error)
}

type preparedWorkerEmbeddedObserver struct {
	input     io.WriteCloser
	writer    *bufio.Writer
	reader    *bufio.Reader
	terminate func() error
	wait      func() error
	stderr    bytes.Buffer
	closed    bool
}

func openPreparedWorkerEmbeddedObserver(
	t testing.TB,
	capture preparedWorkerCaptureContext,
) *preparedWorkerEmbeddedObserver {
	t.Helper()
	executable := os.Getenv(preparedWorkerEmbeddedObserverPathEnvironment)
	expectedDigest := os.Getenv(preparedWorkerEmbeddedObserverDigestEnvironment)
	if executable == "" || !parityHexDigest(expectedDigest, 64) {
		t.Fatal("embedded observer executable metadata is incomplete")
	}
	resolved, err := filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := workerartifact.HashExecutable(resolved, preparedWorkerEmbeddedObserverMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if digest.String() != expectedDigest {
		t.Fatal("embedded observer executable digest differs before launch")
	}

	command := exec.Command(resolved)
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		t.Fatal(err)
	}
	observer := &preparedWorkerEmbeddedObserver{
		input:  input,
		writer: bufio.NewWriter(input),
		reader: bufio.NewReader(output),
	}
	command.Stderr = &observer.stderr
	if err := command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		t.Fatal(err)
	}
	observer.terminate = command.Process.Kill
	observer.wait = command.Wait
	ready, err := observer.readLine()
	if err != nil || ready != preparedWorkerEmbeddedObserverReady {
		_ = observer.terminate()
		_ = observer.wait()
		t.Fatalf("embedded observer READY = %q/%v: %s", ready, err, strings.TrimSpace(observer.stderr.String()))
	}
	writePreparedWorkerEmbeddedObserverBuildEvidence(t, capture, digest)
	return observer
}

func (observer *preparedWorkerEmbeddedObserver) call(
	caseIndex uint16,
	iterations int,
	seed int64,
) (float64, string, error) {
	if observer == nil || observer.closed || observer.wait == nil {
		return 0, "", fmt.Errorf("embedded observer is closed")
	}
	if _, err := fmt.Fprintf(observer.writer, "call %d %d %d\n", caseIndex, iterations, seed); err != nil {
		return 0, "", err
	}
	if err := observer.writer.Flush(); err != nil {
		return 0, "", err
	}
	line, err := observer.readLine()
	if err != nil {
		return 0, "", fmt.Errorf("embedded observer response: %w: %s", err, strings.TrimSpace(observer.stderr.String()))
	}
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != "ok" {
		return 0, "", fmt.Errorf("embedded observer malformed response %q", line)
	}
	elapsed, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || elapsed <= 0 {
		return 0, "", fmt.Errorf("embedded observer invalid elapsed time %q", fields[1])
	}
	if err := parityValidateIntegerString(fields[2]); err != nil {
		return 0, "", fmt.Errorf("embedded observer result: %w", err)
	}
	return float64(elapsed), fields[2], nil
}

func (observer *preparedWorkerEmbeddedObserver) Close() error {
	if observer == nil || observer.closed {
		return nil
	}
	observer.closed = true
	if _, err := fmt.Fprintln(observer.writer, "close"); err != nil {
		_ = observer.terminate()
		_ = observer.wait()
		return err
	}
	if err := observer.writer.Flush(); err != nil {
		_ = observer.terminate()
		_ = observer.wait()
		return err
	}
	line, readErr := observer.readLine()
	closeErr := observer.input.Close()
	waitErr := observer.wait()
	if readErr != nil {
		return fmt.Errorf("embedded observer close response: %w: %s", readErr, strings.TrimSpace(observer.stderr.String()))
	}
	if line != "bye" {
		return fmt.Errorf("embedded observer close response %q", line)
	}
	if closeErr != nil {
		return closeErr
	}
	if waitErr != nil {
		return fmt.Errorf("embedded observer exit: %w: %s", waitErr, strings.TrimSpace(observer.stderr.String()))
	}
	return nil
}

func (observer *preparedWorkerEmbeddedObserver) readLine() (string, error) {
	line, err := observer.reader.ReadString('\n')
	if len(line) > 4096 {
		return "", fmt.Errorf("embedded observer response exceeds 4096 bytes")
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(line, "\n"), nil
}

func writePreparedWorkerEmbeddedObserverBuildEvidence(
	t testing.TB,
	capture preparedWorkerCaptureContext,
	digest workerartifact.Digest,
) {
	t.Helper()
	file := createPreparedWorkerCaptureFile(t, filepath.Join(capture.Output, "embedded-observer-build.tsv"))
	defer file.Close()
	writePreparedWorkerCapture(t, file, "schema_version\tcapture_id\tcapture_pair\tsource_commit\tmode\texecutable_sha256\ttarget_os\ttarget_arch\tactivated\tenvironment_sha256\n")
	writePreparedWorkerCapture(
		t,
		file,
		"1\t%s\t%s\t%s\tproduction-compiled-open-embedded\t%s\t%s\t%s\ttrue\t%s\n",
		capture.ID,
		capture.Pair,
		capture.SourceCommit,
		digest.String(),
		runtime.GOOS,
		runtime.GOARCH,
		capture.EnvironmentHash,
	)
}
