package ember

import (
	"runtime"
	"strings"
	"testing"
)

const compilerRetainedArtifactBatch = 16

// compilerRetainedArtifactSink keeps benchmark results observable. The batch
// sink keeps every artifact reachable through the post-compile collection.
var compilerRetainedArtifactSink *Proto
var compilerRetainedArtifactBatchSink []*Proto

func TestCompilerRetainedArtifactHarness(t *testing.T) {
	source := compilerRetainedArtifactSource()
	proto, err := Compile(source)
	if err != nil {
		t.Fatalf("retained-artifact fixture Compile returned error: %v", err)
	}
	values, err := Run(proto)
	if err != nil {
		t.Fatalf("retained-artifact fixture Run returned error: %v", err)
	}
	if len(values) != 2 || !valuesEqual(values[1], StringValue("stage")) {
		t.Fatalf("retained-artifact fixture returned %#v, want a number and %q", values, "stage")
	}

	retained, measured, err := measureCompilerRetainedArtifact(source)
	if err != nil {
		t.Fatalf("measureCompilerRetainedArtifact returned error: %v", err)
	}
	if measured == nil {
		t.Fatal("measureCompilerRetainedArtifact returned nil proto")
	}
	if retained < 0 {
		t.Fatalf("retained heap delta = %d bytes, want a non-negative per-artifact estimate", retained)
	}
	t.Logf("retained heap delta = %d bytes (alloc-space is reported separately by the benchmark)", retained)
}

func BenchmarkCompilerRetainedArtifact(b *testing.B) {
	source := compilerRetainedArtifactSource()
	validated, err := Compile(source)
	if err != nil {
		b.Fatalf("retained-artifact fixture Compile returned error: %v", err)
	}
	values, err := Run(validated)
	if err != nil {
		b.Fatalf("retained-artifact fixture Run returned error: %v", err)
	}
	if len(values) != 2 || !valuesEqual(values[1], StringValue("stage")) {
		b.Fatalf("retained-artifact fixture returned %#v, want a number and %q", values, "stage")
	}

	retained, _, err := measureCompilerRetainedArtifact(source)
	if err != nil {
		b.Fatalf("measureCompilerRetainedArtifact returned error: %v", err)
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for range b.N {
		proto, err := Compile(source)
		if err != nil {
			b.Fatal(err)
		}
		compilerRetainedArtifactSink = proto
	}
	b.StopTimer()

	// B/op is allocation space from the benchmark runtime. This independent
	// metric is the post-GC heap delta per artifact from a small retained batch.
	b.ReportMetric(float64(retained), "retained_heap_B/op")
	b.ReportMetric(float64(len(source)), "source_B/op")
	compilerRetainedArtifactSink = nil
}

func measureCompilerRetainedArtifact(source string) (int64, *Proto, error) {
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	protos := make([]*Proto, compilerRetainedArtifactBatch)
	for index := range protos {
		ownedSource := strings.Clone(source)
		proto, err := Compile(ownedSource)
		ownedSource = ""
		if err != nil {
			return 0, nil, err
		}
		protos[index] = proto
	}
	compilerRetainedArtifactBatchSink = protos
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	compilerRetainedArtifactBatchSink = nil
	runtime.KeepAlive(protos)
	delta := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if delta < 0 {
		delta = 0
	}
	return delta / int64(compilerRetainedArtifactBatch), protos[0], nil
}

func compilerRetainedArtifactSource() string {
	return compilerStageSource(16 << 10)
}
