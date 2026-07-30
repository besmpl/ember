package ember_test

import (
	"os"
	"strings"
	"testing"
)

func TestTargetNativeCIKeepsTheCompleteNoCGOMatrix(t *testing.T) {
	tests := []struct {
		name        string
		job         string
		targetCheck string
		testCommand string
	}{
		{
			name:        "linux-amd64",
			job:         "ci",
			targetCheck: `test "$(go env GOOS)/$(go env GOARCH)" = linux/amd64`,
			testCommand: "scripts/check",
		},
		{
			name:        "darwin-arm64",
			job:         "macos-arm64",
			targetCheck: `test "$(go env GOOS)/$(go env GOARCH)" = darwin/arm64`,
			testCommand: "go test -count=1 ./...",
		},
		{
			name:        "darwin-amd64",
			job:         "macos-amd64",
			targetCheck: `test "$(go env GOOS)/$(go env GOARCH)" = darwin/amd64`,
			testCommand: "go test -count=1 ./...",
		},
		{
			name:        "linux-arm64",
			job:         "linux-arm64",
			targetCheck: `test "$(go env GOOS)/$(go env GOARCH)" = linux/arm64`,
			testCommand: "go test -count=1 ./...",
		},
		{
			name:        "windows-amd64",
			job:         "windows-amd64",
			targetCheck: "(go env GOOS) -ne 'windows' -or (go env GOARCH) -ne 'amd64'",
			testCommand: "go test -count=1 ./...",
		},
		{
			name:        "windows-arm64",
			job:         "windows-arm64",
			targetCheck: "(go env GOOS) -ne 'windows' -or (go env GOARCH) -ne 'arm64'",
			testCommand: "go test -count=1 ./...",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := workflowJob(t, ".github/workflows/ci.yml", test.job)
			for name, clause := range map[string]string{
				"no-cgo environment":    `CGO_ENABLED: "0"`,
				"physical target check": test.targetCheck,
				"native test suite":     test.testCommand,
			} {
				if !strings.Contains(job, clause) {
					t.Errorf("%s job lacks %s clause %q", test.name, name, clause)
				}
			}
		})
	}
}

func TestScheduledPhysicalSoaksRetainBoundedPassReceipts(t *testing.T) {
	job := workflowJob(t, ".github/workflows/scheduled.yml", "prepared-worker-soak")
	step := workflowStep(t, job, "Run exact alternating-generation soak")

	for name, clause := range map[string]string{
		"job timeout":              "timeout-minutes: 40",
		"Linux x86-64 target":      "target: linux/amd64",
		"Darwin ARM64 target":      "target: darwin/arm64",
		"pipeline failure":         "set -euo pipefail",
		"physical target check":    `test "$(go env GOOS)/$(go env GOARCH)" = '${{ matrix.target }}'`,
		"soak admission switch":    "EMBER_PREPARED_WORKER_SWAP_SOAK=1",
		"soak receipt output":      `EMBER_PREPARED_WORKER_SWAP_SOAK_OUTPUT="$PWD/prepared-worker-soak"`,
		"bounded Go test":          "-timeout=35m",
		"exact alternating test":   "^TestPreparedWorkerParityAlternatesIndependentStaticAOTGenerations$",
		"successful PASS receipt":  `test -f prepared-worker-soak/resource-summary.tsv`,
		"always preserve evidence": "if: ${{ always() }}",
		"retention period":         "retention-days: 30",
	} {
		if !strings.Contains(job, clause) && !strings.Contains(step, clause) {
			t.Errorf("scheduled physical soak lacks %s clause %q", name, clause)
		}
	}

	soak := strings.Index(job, "EMBER_PREPARED_WORKER_SWAP_SOAK=1")
	upload := strings.Index(job, "uses: actions/upload-artifact@v4")
	if soak < 0 || upload < 0 || soak > upload {
		t.Errorf("scheduled soak must run before evidence upload: soak index %d, upload index %d", soak, upload)
	}
}

func TestLinuxAMD64AdmissionCIIncludesResourceSoak(t *testing.T) {
	job := workflowJob(t, ".github/workflows/ci.yml", "linux-amd64-performance")
	step := workflowStep(t, job, "Run Linux x86-64 resource-bounded reload soak")

	required := map[string]string{
		"pipeline failure propagation": `set -euo pipefail`,
		"physical target check":        `test "$(go env GOOS)/$(go env GOARCH)" = linux/amd64`,
		"dedicated soak evidence":      `soak="${evidence}/soak"`,
		"soak admission switch":        `EMBER_PREPARED_WORKER_SWAP_SOAK=1`,
		"soak evidence output":         `EMBER_PREPARED_WORKER_SWAP_SOAK_OUTPUT="${soak}"`,
		"bounded Go test":              `-timeout=35m`,
		"exact reload test":            `^TestPreparedWorkerParityAlternatesIndependentStaticAOTGenerations$`,
		"successful receipt":           `test -f "${soak}/resource-summary.tsv"`,
	}
	for name, clause := range required {
		if !strings.Contains(step, clause) {
			t.Errorf("linux-amd64-performance job lacks %s clause %q", name, clause)
		}
	}

	soak := strings.Index(job, "EMBER_PREPARED_WORKER_SWAP_SOAK=1")
	upload := strings.Index(job, "uses: actions/upload-artifact@v4")
	if soak < 0 || upload < 0 || soak > upload {
		t.Errorf("resource soak must run before evidence upload: soak index %d, upload index %d", soak, upload)
	}
}

func workflowJob(t *testing.T, path, name string) string {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(contents), "\n")
	marker := "  " + name + ":"
	start := -1
	for i, line := range lines {
		if line == marker {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("workflow job %q not found in %s", name, path)
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "  ") &&
			!strings.HasPrefix(line, "    ") &&
			strings.HasSuffix(line, ":") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

func workflowStep(t *testing.T, job, name string) string {
	t.Helper()

	marker := "      - name: " + name + "\n"
	start := strings.Index(job, marker)
	if start < 0 {
		t.Fatalf("workflow step %q not found", name)
	}
	rest := job[start+len(marker):]
	if end := strings.Index(rest, "      - name: "); end >= 0 {
		rest = rest[:end]
	}
	return marker + rest
}
