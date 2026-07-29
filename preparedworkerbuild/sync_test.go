package preparedworkerbuild

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncRegularFileFlushesWithoutMutatingContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact")
	want := []byte("durable worker artifact")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := syncRegularFile(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("synced contents = %q, want %q", got, want)
	}
}
