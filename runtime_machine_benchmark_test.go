package ember

import (
	"context"
	"testing"
)

var runtimeMachineBenchmarkSink []Value

func BenchmarkRunArithmetic(b *testing.B) {
	benchmarkScalarMachineBackends(b, `
local x = 1
local y = 2
return (x + y) * 3 - 4 / 2
`)
}

func BenchmarkRunWhileLoop(b *testing.B) {
	benchmarkScalarMachineBackends(b, `
local i = 0
local total = 0
while i < 20 do
    i = i + 1
    total = total + i
end
return total
`)
}

func benchmarkScalarMachineBackends(b *testing.B, source string) {
	proto, err := Compile(source)
	if err != nil {
		b.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		b.Fatal(err)
	}
	if !image.eligible {
		b.Fatalf("benchmark image is ineligible: %s", image.rejectReason)
	}
	b.Run("old", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			values, err := executeProto(context.Background(), proto, nil, executeOptions{})
			if err != nil {
				b.Fatal(err)
			}
			runtimeMachineBenchmarkSink = values
		}
	})
	b.Run("machine", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			values, err := executeCodeImage(image, nil)
			if err != nil {
				b.Fatal(err)
			}
			runtimeMachineBenchmarkSink = values
		}
	})
}
