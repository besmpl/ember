package ember_test

import (
	"testing"

	"github.com/besmpl/ember"
)

func BenchmarkCompileArithmetic(b *testing.B) {
	for b.Loop() {
		if _, err := ember.Compile(`
local x = 1
local y = 2
return (x + y) * 3 - 4 / 2
`); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunArithmetic(b *testing.B) {
	proto := benchmarkCompile(b, `
local x = 1
local y = 2
return (x + y) * 3 - 4 / 2
`)

	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunTableFields(b *testing.B) {
	proto := benchmarkCompile(b, `
local player = {stats = {hp = 10, shield = 4}}
player.stats.hp = player.stats.hp + player.stats.shield
return player.stats.hp
`)

	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunFunctionCalls(b *testing.B) {
	proto := benchmarkCompile(b, `
local function add(left, right)
    return left + right
end
return add(add(1, 2), add(3, 4))
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunRecursiveScriptCalls(b *testing.B) {
	proto := benchmarkCompile(b, `
local function sum(n)
    if n == 0 then
        return 0
    end
    return n + sum(n - 1)
end
return sum(12)
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunWhileLoop(b *testing.B) {
	proto := benchmarkCompile(b, `
local i = 0
local total = 0
while i < 20 do
    i = i + 1
    total = total + i
end
return total
`)

	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunMetatableIndex(b *testing.B) {
	proto := benchmarkCompile(b, `
local fallback = {hp = 10}
local player = setmetatable({}, {__index = fallback})
local total = 0
local i = 0
while i < 20 do
    i = i + 1
    total = total + player.hp
end
return total
`)

	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunArrayLiteral(b *testing.B) {
	proto := benchmarkCompile(b, `
local values = {1, 2, 3, 4, 5, 6, 7, 8}
return values[1] + values[8]
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunTableInsert(b *testing.B) {
	proto := benchmarkCompile(b, `
local values = {}
local i = 0
while i < 20 do
    i = i + 1
    table.insert(values, i)
end
return rawlen(values)
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunTableRemove(b *testing.B) {
	proto := benchmarkCompile(b, `
local values = {1, 2, 3, 4, 5, 6, 7, 8}
table.remove(values, 4)
return values[4], rawlen(values)
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunTableUnpack(b *testing.B) {
	proto := benchmarkCompile(b, `
local a, b, c, d = table.unpack({1, 2, 3, 4})
return a + b + c + d
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunRawLength(b *testing.B) {
	proto := benchmarkCompile(b, `
local values = {1, 2, 3, 4, 5, 6, 7, 8}
local total = 0
local i = 0
while i < 20 do
    i = i + 1
    total = total + rawlen(values)
end
return total
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunStringFieldReads(b *testing.B) {
	proto := benchmarkCompile(b, `
local player = {hp = 10, shield = 4}
local total = 0
local i = 0
while i < 20 do
    i = i + 1
    total = total + player.hp + player.shield
end
return total
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunGlobalAccess(b *testing.B) {
	proto := benchmarkCompile(b, `
local total = 0
local i = 0
while i < 20 do
    i = i + 1
    total = total + math.abs(-i)
end
return total
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunMethodCalls(b *testing.B) {
	proto := benchmarkCompile(b, `
local counter = {value = 0}
function counter:add(amount)
    self.value = self.value + amount
    return self.value
end
return counter:add(1) + counter:add(2)
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunIteration(b *testing.B) {
	proto := benchmarkCompile(b, `
local values = {1, 2, 3, 4, 5, 6}
local total = 0
for _, value in values do
    total = total + value
end
return total
`)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ember.Run(proto); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkCompile(tb testing.TB, source string) *ember.Proto {
	tb.Helper()
	proto, err := ember.Compile(source)
	if err != nil {
		tb.Fatal(err)
	}
	return proto
}
