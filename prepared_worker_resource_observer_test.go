package ember_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	preparedWorkerMaximumProcessResidentBytes   = uint64(256 << 20)
	preparedWorkerMaximumAggregateResidentBytes = uint64(512 << 20)
	preparedWorkerResourceSamplerHelper         = "EMBER_PREPARED_WORKER_RESOURCE_SAMPLER_HELPER"
)

type preparedWorkerProcessResource struct {
	PID           int
	ParentPID     int
	CreatedAt     int64
	ResidentBytes uint64
}

type preparedWorkerResourcePoint struct {
	Children              int
	UnobservedRSSChildren int
	ResidentBytes         uint64
	LargestProcessBytes   uint64
}

type preparedWorkerResourceEvidencePoint struct {
	Phase               string
	Swap                int
	Children            int
	ResidentBytes       uint64
	LargestProcessBytes uint64
}

type preparedWorkerResourceEvidence struct {
	TargetOS      string
	TargetArch    string
	GoVersion     string
	StandardBuild string
	HoldoutBuild  string
	Points        []preparedWorkerResourceEvidencePoint
}

func observePreparedWorkerResources(
	t testing.TB,
	evidence *preparedWorkerResourceEvidence,
	phase string,
	swap int,
) {
	t.Helper()
	if evidence == nil {
		t.Fatal("prepared worker resource evidence is nil")
	}
	point, err := samplePreparedWorkerResourcePoint(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if point.UnobservedRSSChildren != 0 {
		t.Fatalf(
			"prepared worker resource observation missed RSS for %d of %d children",
			point.UnobservedRSSChildren,
			point.Children,
		)
	}
	evidence.Points = append(evidence.Points, preparedWorkerResourceEvidencePoint{
		Phase:               phase,
		Swap:                swap,
		Children:            point.Children,
		ResidentBytes:       point.ResidentBytes,
		LargestProcessBytes: point.LargestProcessBytes,
	})
}

func validatePreparedWorkerResourceEvidence(
	evidence preparedWorkerResourceEvidence,
	swaps int,
) error {
	if swaps <= 0 {
		return fmt.Errorf("prepared worker resource evidence: swap count must be positive")
	}
	hasInitial := false
	hasClosed := false
	for _, point := range evidence.Points {
		hasInitial = hasInitial || point.Phase == "initial"
		hasClosed = hasClosed || point.Phase == "closed"
	}
	if !hasInitial {
		return fmt.Errorf("prepared worker resource evidence: initial sample is missing")
	}
	if !hasClosed {
		return fmt.Errorf("prepared worker resource evidence: closed sample is missing")
	}
	for index, point := range evidence.Points {
		expectedPhase := "prepared"
		expectedSwap := index
		if index == 0 {
			expectedPhase = "initial"
			expectedSwap = 0
		} else if index == swaps+1 {
			expectedPhase = "closed"
			expectedSwap = swaps
		} else if index > swaps+1 {
			return fmt.Errorf("prepared worker resource evidence: unexpected sample %d", index)
		}
		if point.Phase != expectedPhase || point.Swap != expectedSwap {
			if expectedPhase == "prepared" {
				return fmt.Errorf(
					"prepared worker resource evidence: prepared sample %d is missing; sample %d is %s/%d",
					expectedSwap,
					index,
					point.Phase,
					point.Swap,
				)
			}
			return fmt.Errorf(
				"prepared worker resource evidence: sample %d is %s/%d, want %s/%d",
				index,
				point.Phase,
				point.Swap,
				expectedPhase,
				expectedSwap,
			)
		}
		if (point.Phase == "initial" || point.Phase == "prepared") &&
			(point.ResidentBytes == 0 || point.LargestProcessBytes == 0) {
			return fmt.Errorf(
				"prepared worker resource evidence: %s sample has no resident-memory observation",
				point.Phase,
			)
		}
		if point.LargestProcessBytes > preparedWorkerMaximumProcessResidentBytes {
			return fmt.Errorf(
				"prepared worker resource evidence: %s sample %d process RSS %d exceeds %d bytes",
				point.Phase,
				point.Swap,
				point.LargestProcessBytes,
				preparedWorkerMaximumProcessResidentBytes,
			)
		}
		if point.ResidentBytes > preparedWorkerMaximumAggregateResidentBytes {
			return fmt.Errorf(
				"prepared worker resource evidence: %s sample %d aggregate RSS %d exceeds %d bytes",
				point.Phase,
				point.Swap,
				point.ResidentBytes,
				preparedWorkerMaximumAggregateResidentBytes,
			)
		}
		if point.Phase == "prepared" {
			if point.Children != 2 {
				return fmt.Errorf(
					"prepared worker resource evidence: prepared sample %d has %d children, want 2",
					point.Swap,
					point.Children,
				)
			}
		}
		if point.Phase == "initial" {
			if point.Children != 1 {
				return fmt.Errorf(
					"prepared worker resource evidence: initial sample has %d children, want 1",
					point.Children,
				)
			}
		}
		if point.Phase == "closed" {
			if point.Children != 0 {
				return fmt.Errorf("prepared worker resource evidence: closed sample has %d children", point.Children)
			}
			if point.ResidentBytes != 0 || point.LargestProcessBytes != 0 {
				return fmt.Errorf(
					"prepared worker resource evidence: closed sample RSS is %d/%d bytes, want zero",
					point.ResidentBytes,
					point.LargestProcessBytes,
				)
			}
		}
	}
	if len(evidence.Points) < swaps+2 {
		return fmt.Errorf("prepared worker resource evidence: prepared sample %d is missing", len(evidence.Points))
	}
	return nil
}

func encodePreparedWorkerResourceEvidence(
	evidence preparedWorkerResourceEvidence,
	swaps int,
) (string, string, error) {
	if err := validatePreparedWorkerResourceEvidenceIdentity(evidence); err != nil {
		return "", "", err
	}
	if err := validatePreparedWorkerResourceEvidence(evidence, swaps); err != nil {
		return "", "", err
	}
	samples := encodePreparedWorkerResourceSamples(evidence)
	return samples, encodePreparedWorkerResourceSummary(evidence, swaps, samples), nil
}

func validatePreparedWorkerResourceEvidenceIdentity(evidence preparedWorkerResourceEvidence) error {
	if evidence.TargetOS == "" || evidence.TargetArch == "" || evidence.GoVersion == "" ||
		evidence.StandardBuild == "" || evidence.HoldoutBuild == "" {
		return fmt.Errorf("prepared worker resource evidence: incomplete identity")
	}
	return nil
}

func encodePreparedWorkerResourceSamples(evidence preparedWorkerResourceEvidence) string {
	var samples strings.Builder
	samples.WriteString("schema_version\tphase\tswap\tchildren\taggregate_rss_bytes\tmax_process_rss_bytes\n")
	for _, point := range evidence.Points {
		fmt.Fprintf(
			&samples,
			"1\t%s\t%d\t%d\t%d\t%d\n",
			point.Phase,
			point.Swap,
			point.Children,
			point.ResidentBytes,
			point.LargestProcessBytes,
		)
	}
	return samples.String()
}

func encodePreparedWorkerResourceSummary(
	evidence preparedWorkerResourceEvidence,
	swaps int,
	samples string,
) string {
	peakChildren := 0
	var peakAggregate uint64
	var peakProcess uint64
	for _, point := range evidence.Points {
		if point.Children > peakChildren {
			peakChildren = point.Children
		}
		if point.ResidentBytes > peakAggregate {
			peakAggregate = point.ResidentBytes
		}
		if point.LargestProcessBytes > peakProcess {
			peakProcess = point.LargestProcessBytes
		}
	}
	samplesDigest := sha256.Sum256([]byte(samples))
	var summary strings.Builder
	summary.WriteString("field\tvalue\n")
	for _, field := range [][2]string{
		{"schema_version", "1"},
		{"target", evidence.TargetOS + "/" + evidence.TargetArch},
		{"go_version", evidence.GoVersion},
		{"standard_build_id", evidence.StandardBuild},
		{"holdout_build_id", evidence.HoldoutBuild},
		{"swaps", fmt.Sprint(swaps)},
		{"samples", fmt.Sprint(len(evidence.Points))},
		{"child_budget", "2"},
		{"process_rss_budget_bytes", fmt.Sprint(preparedWorkerMaximumProcessResidentBytes)},
		{"aggregate_rss_budget_bytes", fmt.Sprint(preparedWorkerMaximumAggregateResidentBytes)},
		{"peak_children", fmt.Sprint(peakChildren)},
		{"peak_process_rss_bytes", fmt.Sprint(peakProcess)},
		{"peak_aggregate_rss_bytes", fmt.Sprint(peakAggregate)},
		{"final_children", "0"},
		{"final_aggregate_rss_bytes", "0"},
		{"samples_sha256", fmt.Sprintf("%x", samplesDigest)},
		{"status", "PASS"},
	} {
		fmt.Fprintf(&summary, "%s\t%s\n", field[0], field[1])
	}
	return summary.String()
}

func writePreparedWorkerResourceEvidence(
	output string,
	evidence preparedWorkerResourceEvidence,
	swaps int,
) error {
	if !filepath.IsAbs(output) {
		return fmt.Errorf("prepared worker resource evidence: output must be absolute")
	}
	info, err := os.Stat(output)
	if err != nil {
		return fmt.Errorf("prepared worker resource evidence: inspect output: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("prepared worker resource evidence: output is not a directory")
	}
	if err := validatePreparedWorkerResourceEvidenceIdentity(evidence); err != nil {
		return err
	}
	samples := encodePreparedWorkerResourceSamples(evidence)
	if err := writePreparedWorkerResourceEvidenceFile(
		filepath.Join(output, "resource-samples.tsv"),
		samples,
	); err != nil {
		return err
	}
	if err := validatePreparedWorkerResourceEvidence(evidence, swaps); err != nil {
		return err
	}
	summary := encodePreparedWorkerResourceSummary(evidence, swaps, samples)
	if err := writePreparedWorkerResourceEvidenceFile(
		filepath.Join(output, "resource-summary.tsv"),
		summary,
	); err != nil {
		return err
	}
	return nil
}

func writePreparedWorkerResourceEvidenceFile(path, content string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("prepared worker resource evidence: create %s: %w", path, err)
	}
	_, writeErr := file.WriteString(content)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("prepared worker resource evidence: publish %s: %w", path, err)
	}
	return nil
}

func summarizePreparedWorkerResourceSample(
	rootPID int,
	processes []preparedWorkerProcessResource,
) preparedWorkerResourcePoint {
	descendants := preparedWorkerDescendantProcesses(rootPID, processes)
	point := preparedWorkerResourcePoint{Children: len(descendants)}
	for _, process := range descendants {
		if process.ResidentBytes == 0 {
			point.UnobservedRSSChildren++
		}
		point.ResidentBytes += process.ResidentBytes
		if process.ResidentBytes > point.LargestProcessBytes {
			point.LargestProcessBytes = process.ResidentBytes
		}
	}
	return point
}

func preparedWorkerDescendantProcesses(
	rootPID int,
	processes []preparedWorkerProcessResource,
) []preparedWorkerProcessResource {
	childrenByParent := make(map[int][]preparedWorkerProcessResource, len(processes))
	processByPID := make(map[int]preparedWorkerProcessResource, len(processes))
	for _, process := range processes {
		childrenByParent[process.ParentPID] = append(childrenByParent[process.ParentPID], process)
		processByPID[process.PID] = process
	}
	descendants := make([]preparedWorkerProcessResource, 0)
	seen := map[int]struct{}{rootPID: {}}
	type pendingProcess struct {
		process         preparedWorkerProcessResource
		parentCreatedAt int64
	}
	rootCreatedAt := processByPID[rootPID].CreatedAt
	pending := make([]pendingProcess, 0, len(childrenByParent[rootPID]))
	for _, process := range childrenByParent[rootPID] {
		pending = append(pending, pendingProcess{process: process, parentCreatedAt: rootCreatedAt})
	}
	for len(pending) > 0 {
		candidate := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		process := candidate.process
		if candidate.parentCreatedAt != 0 && process.CreatedAt != 0 &&
			process.CreatedAt < candidate.parentCreatedAt {
			continue
		}
		if _, ok := seen[process.PID]; ok {
			continue
		}
		seen[process.PID] = struct{}{}
		descendants = append(descendants, process)
		for _, child := range childrenByParent[process.PID] {
			pending = append(pending, pendingProcess{process: child, parentCreatedAt: process.CreatedAt})
		}
	}
	return descendants
}

func TestPreparedWorkerResourcePointCountsOnlyDescendants(t *testing.T) {
	point := summarizePreparedWorkerResourceSample(10, []preparedWorkerProcessResource{
		{PID: 10, ParentPID: 1, ResidentBytes: 100},
		{PID: 11, ParentPID: 10, ResidentBytes: 200},
		{PID: 12, ParentPID: 11, ResidentBytes: 300},
		{PID: 13, ParentPID: 99, ResidentBytes: 400},
	})
	if point.Children != 2 {
		t.Fatalf("children = %d, want 2", point.Children)
	}
	if point.ResidentBytes != 500 {
		t.Fatalf("resident bytes = %d, want 500", point.ResidentBytes)
	}
	if point.LargestProcessBytes != 300 {
		t.Fatalf("largest process bytes = %d, want 300", point.LargestProcessBytes)
	}
}

func TestPreparedWorkerResourcePointRejectsOlderProcessBehindReusedParentPID(t *testing.T) {
	point := summarizePreparedWorkerResourceSample(10, []preparedWorkerProcessResource{
		{PID: 10, ParentPID: 1, CreatedAt: 100, ResidentBytes: 100},
		{PID: 11, ParentPID: 10, CreatedAt: 300, ResidentBytes: 200},
		{PID: 12, ParentPID: 11, CreatedAt: 200, ResidentBytes: 500 << 20},
	})
	if point.Children != 1 {
		t.Fatalf("children = %d, want only the live worker", point.Children)
	}
	if point.ResidentBytes != 200 || point.LargestProcessBytes != 200 {
		t.Fatalf("resident bytes = %d/%d, want 200/200", point.ResidentBytes, point.LargestProcessBytes)
	}
}

func TestPreparedWorkerResourcePointReportsDescendantWithoutRSS(t *testing.T) {
	point := summarizePreparedWorkerResourceSample(10, []preparedWorkerProcessResource{
		{PID: 11, ParentPID: 10, ResidentBytes: 200},
		{PID: 12, ParentPID: 10},
	})
	if point.UnobservedRSSChildren != 1 {
		t.Fatalf("unobserved RSS children = %d, want 1", point.UnobservedRSSChildren)
	}
}

func TestPreparedWorkerResourceEvidenceRejectsLiveChildAfterClose(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "initial", Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
			{Phase: "prepared", Swap: 1, Children: 2, ResidentBytes: 20, LargestProcessBytes: 10},
			{Phase: "closed", Swap: 1, Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
		},
	}
	err := validatePreparedWorkerResourceEvidence(evidence, 1)
	if err == nil || !strings.Contains(err.Error(), "closed sample has 1 children") {
		t.Fatalf("validation error = %v, want closed-child failure", err)
	}
}

func TestPreparedWorkerResourceEvidenceRequiresEveryPreparedSwap(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "initial", Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
			{Phase: "prepared", Swap: 1, Children: 2, ResidentBytes: 20, LargestProcessBytes: 10},
			{Phase: "closed", Swap: 2},
		},
	}
	err := validatePreparedWorkerResourceEvidence(evidence, 2)
	if err == nil || !strings.Contains(err.Error(), "prepared sample 2") {
		t.Fatalf("validation error = %v, want missing prepared sample failure", err)
	}
}

func TestPreparedWorkerResourceEvidenceRejectsExtraLiveGeneration(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "initial", Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
			{Phase: "prepared", Swap: 1, Children: 3, ResidentBytes: 30, LargestProcessBytes: 10},
			{Phase: "closed", Swap: 1},
		},
	}
	err := validatePreparedWorkerResourceEvidence(evidence, 1)
	if err == nil || !strings.Contains(err.Error(), "prepared sample 1 has 3 children") {
		t.Fatalf("validation error = %v, want live-generation bound failure", err)
	}
}

func TestPreparedWorkerResourceEvidenceRejectsResidentMemoryOverBudget(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "initial", Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
			{
				Phase: "prepared", Swap: 1, Children: 2,
				ResidentBytes:       preparedWorkerMaximumAggregateResidentBytes + 1,
				LargestProcessBytes: 10,
			},
			{Phase: "closed", Swap: 1},
		},
	}
	err := validatePreparedWorkerResourceEvidence(evidence, 1)
	if err == nil || !strings.Contains(err.Error(), "aggregate RSS") {
		t.Fatalf("validation error = %v, want aggregate RSS budget failure", err)
	}
}

func TestPreparedWorkerResourceEvidenceRejectsOneOversizedGeneration(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "initial", Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
			{
				Phase: "prepared", Swap: 1, Children: 2,
				ResidentBytes:       preparedWorkerMaximumProcessResidentBytes + 1,
				LargestProcessBytes: preparedWorkerMaximumProcessResidentBytes + 1,
			},
			{Phase: "closed", Swap: 1},
		},
	}
	err := validatePreparedWorkerResourceEvidence(evidence, 1)
	if err == nil || !strings.Contains(err.Error(), "process RSS") {
		t.Fatalf("validation error = %v, want process RSS budget failure", err)
	}
}

func TestPreparedWorkerResourceEvidenceAcceptsBoundedLifecycle(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "initial", Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
			{Phase: "prepared", Swap: 1, Children: 2, ResidentBytes: 20, LargestProcessBytes: 10},
			{Phase: "prepared", Swap: 2, Children: 2, ResidentBytes: 22, LargestProcessBytes: 12},
			{Phase: "closed", Swap: 2},
		},
	}
	if err := validatePreparedWorkerResourceEvidence(evidence, 2); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedWorkerResourceEvidenceRequiresOneInitialGeneration(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "prepared", Swap: 1, Children: 2, ResidentBytes: 20, LargestProcessBytes: 10},
			{Phase: "closed", Swap: 1},
		},
	}
	err := validatePreparedWorkerResourceEvidence(evidence, 1)
	if err == nil || !strings.Contains(err.Error(), "initial sample") {
		t.Fatalf("validation error = %v, want initial-sample failure", err)
	}
}

func TestPreparedWorkerResourceEvidenceRequiresClosedSample(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "initial", Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
			{Phase: "prepared", Swap: 1, Children: 2, ResidentBytes: 20, LargestProcessBytes: 10},
		},
	}
	err := validatePreparedWorkerResourceEvidence(evidence, 1)
	if err == nil || !strings.Contains(err.Error(), "closed sample is missing") {
		t.Fatalf("validation error = %v, want closed-sample failure", err)
	}
}

func TestPreparedWorkerResourceEvidenceRejectsUnaccountedRSSAfterClose(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "initial", Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
			{Phase: "prepared", Swap: 1, Children: 2, ResidentBytes: 20, LargestProcessBytes: 10},
			{Phase: "closed", Swap: 1, ResidentBytes: 10, LargestProcessBytes: 10},
		},
	}
	err := validatePreparedWorkerResourceEvidence(evidence, 1)
	if err == nil || !strings.Contains(err.Error(), "closed sample RSS") {
		t.Fatalf("validation error = %v, want closed-RSS failure", err)
	}
}

func TestPreparedWorkerResourceEvidenceRequiresLiveRSS(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "initial", Children: 1},
			{Phase: "prepared", Swap: 1, Children: 2, ResidentBytes: 20, LargestProcessBytes: 10},
			{Phase: "closed", Swap: 1},
		},
	}
	err := validatePreparedWorkerResourceEvidence(evidence, 1)
	if err == nil || !strings.Contains(err.Error(), "initial sample has no resident-memory observation") {
		t.Fatalf("validation error = %v, want missing-live-RSS failure", err)
	}
}

func TestPreparedWorkerResourceEvidenceRejectsOutOfOrderSamples(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "prepared", Swap: 1, Children: 2, ResidentBytes: 20, LargestProcessBytes: 10},
			{Phase: "initial", Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
			{Phase: "closed", Swap: 1},
		},
	}
	err := validatePreparedWorkerResourceEvidence(evidence, 1)
	if err == nil || !strings.Contains(err.Error(), "sample 0 is prepared/1, want initial/0") {
		t.Fatalf("validation error = %v, want sample-order failure", err)
	}
}

func TestPreparedWorkerResourceSamplerSeesOwnedChild(t *testing.T) {
	if os.Getenv(preparedWorkerResourceSamplerHelper) == "1" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestPreparedWorkerResourceSamplerSeesOwnedChild$")
	command.Env = append(os.Environ(), preparedWorkerResourceSamplerHelper+"=1")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = input.Close()
		if err := command.Wait(); err != nil {
			t.Error(err)
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		point, err := samplePreparedWorkerResourcePoint(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		if point.Children >= 1 && point.ResidentBytes > 0 && point.LargestProcessBytes > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("resource point = %#v, want live child RSS", point)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPreparedWorkerResourceEvidenceEncodingBindsLifecycleAndBuilds(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		TargetOS: "testos", TargetArch: "testarch", GoVersion: "go-test",
		StandardBuild: "standard-build", HoldoutBuild: "holdout-build",
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "initial", Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
			{Phase: "prepared", Swap: 1, Children: 2, ResidentBytes: 20, LargestProcessBytes: 10},
			{Phase: "closed", Swap: 1},
		},
	}
	samples, summary, err := encodePreparedWorkerResourceEvidence(evidence, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(samples, "phase\tswap\tchildren\taggregate_rss_bytes\tmax_process_rss_bytes") ||
		!strings.Contains(samples, "prepared\t1\t2\t20\t10") {
		t.Fatalf("samples = %q, want prepared resource row", samples)
	}
	samplesDigest := sha256.Sum256([]byte(samples))
	for _, field := range []string{
		"target\ttestos/testarch",
		"go_version\tgo-test",
		"standard_build_id\tstandard-build",
		"holdout_build_id\tholdout-build",
		"peak_children\t2",
		fmt.Sprintf("samples_sha256\t%x", samplesDigest),
		"status\tPASS",
	} {
		if !strings.Contains(summary, field+"\n") {
			t.Fatalf("summary = %q, want %q", summary, field)
		}
	}
}

func TestWritePreparedWorkerResourceEvidencePublishesExactReceiptOnce(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		TargetOS: "testos", TargetArch: "testarch", GoVersion: "go-test",
		StandardBuild: "standard-build", HoldoutBuild: "holdout-build",
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "initial", Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
			{Phase: "prepared", Swap: 1, Children: 2, ResidentBytes: 20, LargestProcessBytes: 10},
			{Phase: "closed", Swap: 1},
		},
	}
	wantSamples, wantSummary, err := encodePreparedWorkerResourceEvidence(evidence, 1)
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	if err := writePreparedWorkerResourceEvidence(output, evidence, 1); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"resource-samples.tsv": wantSamples,
		"resource-summary.tsv": wantSummary,
	} {
		data, err := os.ReadFile(filepath.Join(output, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want %q", name, data, want)
		}
	}
	if err := writePreparedWorkerResourceEvidence(output, evidence, 1); err == nil {
		t.Fatal("second receipt publication succeeded, want no overwrite")
	}
}

func TestWritePreparedWorkerResourceEvidenceRetainsRawSamplesWithoutPassSummaryOnGateFailure(t *testing.T) {
	evidence := preparedWorkerResourceEvidence{
		TargetOS: "testos", TargetArch: "testarch", GoVersion: "go-test",
		StandardBuild: "standard-build", HoldoutBuild: "holdout-build",
		Points: []preparedWorkerResourceEvidencePoint{
			{Phase: "initial", Children: 1, ResidentBytes: 10, LargestProcessBytes: 10},
			{
				Phase: "prepared", Swap: 1, Children: 2,
				ResidentBytes:       preparedWorkerMaximumProcessResidentBytes + 1,
				LargestProcessBytes: preparedWorkerMaximumProcessResidentBytes + 1,
			},
			{Phase: "closed", Swap: 1},
		},
	}
	output := t.TempDir()
	err := writePreparedWorkerResourceEvidence(output, evidence, 1)
	if err == nil || !strings.Contains(err.Error(), "process RSS") {
		t.Fatalf("write error = %v, want process RSS gate failure", err)
	}
	samples, err := os.ReadFile(filepath.Join(output, "resource-samples.tsv"))
	if err != nil {
		t.Fatalf("read retained samples: %v", err)
	}
	if !strings.Contains(
		string(samples),
		fmt.Sprintf("prepared\t1\t2\t%d\t%d", preparedWorkerMaximumProcessResidentBytes+1, preparedWorkerMaximumProcessResidentBytes+1),
	) {
		t.Fatalf("retained samples = %q, want over-budget observation", samples)
	}
	if _, err := os.Stat(filepath.Join(output, "resource-summary.tsv")); !os.IsNotExist(err) {
		t.Fatalf("PASS summary stat error = %v, want missing file", err)
	}
}
