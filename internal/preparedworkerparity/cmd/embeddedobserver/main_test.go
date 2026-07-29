package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeMeasuresPublicEmbeddedRunner(t *testing.T) {
	input := strings.NewReader("call 10 3 17\nclose\n")
	var output bytes.Buffer
	if err := serve(input, &output, filepath.Join(t.TempDir(), "state.journal")); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 || lines[0] != embeddedObserverReady || lines[2] != "bye" {
		t.Fatalf("observer transcript = %q", output.String())
	}
	var elapsed int64
	var checksum int64
	if count, err := fmt.Sscanf(lines[1], "ok %d %d", &elapsed, &checksum); err != nil || count != 2 {
		t.Fatalf("observer result = %q: %v", lines[1], err)
	}
	if elapsed <= 0 || checksum != 90152 {
		t.Fatalf("observer elapsed/checksum = %d/%d, want positive/90152", elapsed, checksum)
	}
}

func TestServeRejectsMalformedFrame(t *testing.T) {
	var output bytes.Buffer
	err := serve(strings.NewReader("call 0 nope 17\n"), &output, filepath.Join(t.TempDir(), "state.journal"))
	if err == nil || !strings.Contains(err.Error(), "invalid iterations") {
		t.Fatalf("malformed frame error = %v", err)
	}
}
