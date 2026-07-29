package ember_test

import (
	"os"
	"strings"
	"testing"
)

func TestLinuxAMD64AdmissionCIIncludesResourceSoak(t *testing.T) {
	job := workflowJob(t, ".github/workflows/ci.yml", "linux-amd64-performance")
	step := workflowStep(t, job, "Run Linux x86-64 resource-bounded reload soak")

	required := map[string]string{
		"pipeline failure propagation": `set -euo pipefail`,
		"physical target check":        `test "$(go env GOOS)/$(go env GOARCH)" = linux/amd64`,
		"dedicated soak evidence":      `soak="${evidence}/soak"`,
		"soak admission switch":        `EMBER_PREPARED_WORKER_SWAP_SOAK=1`,
		"soak evidence output":         `EMBER_PREPARED_WORKER_SWAP_SOAK_OUTPUT="${soak}"`,
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
