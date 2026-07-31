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
	admission := workflowStep(t, job, "Run paired Linux x86-64 EPW2 admission")
	step := workflowStep(t, job, "Run Linux x86-64 resource-bounded reload soak")
	if !strings.Contains(admission, `mkdir -p "${evidence}"`) {
		t.Error("linux-amd64-performance admission must create its evidence parent")
	}

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

func TestScheduledHostedARM64JobsPinAcceptanceEnvironment(t *testing.T) {
	for _, jobName := range []string{"prepared-worker-admission-arm64", "parity", "performance"} {
		job := workflowJob(t, ".github/workflows/scheduled.yml", jobName)
		if !strings.Contains(job, `go-version: "1.26.4"`) {
			t.Errorf("scheduled %s job must pin the acceptance Go version", jobName)
		}
		if strings.Contains(job, "go-version-file: go.mod") {
			t.Errorf("scheduled %s job must not float with the go.mod patch version", jobName)
		}
		if !strings.Contains(job, "runs-on: macos-15") || strings.Contains(job, "self-hosted") {
			t.Errorf("scheduled %s job must use GitHub-hosted Darwin ARM64", jobName)
		}
		if !strings.Contains(job, "EMBER_RUNTIME_ACCEPTANCE_PROFILE: darwin-arm64-github-hosted") {
			t.Errorf("scheduled %s job must select the hosted ARM64 acceptance profile", jobName)
		}
	}

	admission := workflowJob(t, ".github/workflows/scheduled.yml", "prepared-worker-admission-arm64")
	if !strings.Contains(admission, `mkdir -p "$evidence"`) {
		t.Error("scheduled hosted ARM64 admission must create its evidence parent")
	}
	for _, jobName := range []string{"prepared-worker-admission-arm64", "parity"} {
		job := workflowJob(t, ".github/workflows/scheduled.yml", jobName)
		for label, clause := range map[string]string{
			"official archive pin": "60541670fc8b8a8289df3ff37bd88e81b8f7b45219b777d6f4afd4e8e3af07ec",
			"official binary pin":  parityDarwinHostedLuauSHA256,
			"release asset":        "luau-macos.zip",
		} {
			if !strings.Contains(job, clause) {
				t.Errorf("scheduled %s lacks %s %q", jobName, label, clause)
			}
		}
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
