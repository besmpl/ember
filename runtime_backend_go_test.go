package ember

import (
	"bytes"
	"context"
	"fmt"
	goparser "go/parser"
	"go/token"
	"math"
	"os"
	"strings"
	"testing"
)

const backendNumericProofSource = `
local function kernel(seed)
    local total = seed
    for index = 1, 64 do
        if index % 2 == 0 then
            total = total + index * seed
        else
            total = total - 1
        end
    end
    return total
end
return kernel
`

const backendNumericExitProofSource = `
local function guarded(seed)
    local adjusted = seed + 1
    if adjusted < 10 then
        return adjusted
    end
    return adjusted * 2
end
return guarded
`

const backendNumericCallProofSource = `
local function kernel(seed)
    local function add(value)
        if value < 1000000000000 then
            return value + 1
        end
        return value + 1
    end
    local total = seed
    for index = 1, 64 do
        total = add(total)
    end
    return total
end
return kernel
`

const backendTableFieldProofSource = `
local function kernel(seed)
    local player = {stats = {hp = 100 + seed % 7, shield = 25}, inventory = {coins = 3}}
    local i = 0
    while i < 80 do
        i = i + 1
        player.stats.hp = player.stats.hp + player.stats.shield - player.inventory.coins
    end
    return player.stats.hp
end
return kernel
`

const backendMetatableIndexProofSource = `
local function kernel(seed)
    local fallback = {hp = 7 + seed % 2, shield = 3}
    local player = setmetatable({shield = 5}, {__index = fallback})
    local total = 0
    for i = 1, 90 do
        total = total + player.hp + player.shield
    end
    return total
end
return kernel
`

const backendMethodProofSource = `
local function kernel(seed)
    local counter = {value = seed % 2}
    function counter:add(amount)
        self.value = self.value + amount
        return self.value
    end
    local total = 0
    for i = 1, 70 do
        total = total + counter:add(i % 5)
    end
    return total
end
return kernel
`

const backendArrayIterationProofSource = `
local function kernel(seed)
    local values = {1 + seed % 5, 2, 3, 4, 5, 6, 7, 8}
    local total = 0
    for _, value in values do
        total = total + value * value
    end
    return total
end
return kernel
`

const backendFiniteStringStateProofSource = `
local function kernel(seed)
    local events = {"see", "near", "cooldown", "lost", "hit", "safe", "rest"}
    local state = "idle"
    local energy = 20
    local total = 0
    for pass = 1, 20 + seed % 2 do
        for _, event in events do
            if state == "idle" then
                if event == "see" then
                    state = "chase"
                end
            elseif state == "chase" then
                if event == "near" then
                    state = "attack"
                elseif event == "lost" then
                    state = "search"
                elseif event == "hit" then
                    state = "evade"
                end
            elseif state == "attack" then
                if event == "cooldown" then
                    state = "chase"
                elseif event == "hit" then
                    state = "evade"
                end
            elseif state == "evade" then
                if event == "safe" then
                    state = "search"
                elseif event == "rest" then
                    state = "idle"
                end
            elseif event == "see" then
                state = "chase"
            elseif event == "rest" then
                state = "idle"
            end
            if state == "attack" then
                energy = energy - 3
                total = total + 15
            elseif state == "evade" then
                energy = energy - 1
                total = total + 9
            elseif state == "chase" then
                energy = energy + 1
                total = total + 8
            elseif state == "search" then
                energy = energy + 1
                total = total + 5
            else
                energy = energy + 1
                total = total + 2
            end
            if energy < 0 then
                energy = 4
            elseif energy > 35 then
                energy = 20
            end
            total = total + energy
        end
    end
    return total + energy
end
return kernel
`

const backendStructuralStringKeyProofSource = `
local function kernel(seed)
    local function cellKey(x, y)
        return tostring(x) .. ":" .. tostring(y)
    end
    local x = seed % 5
    local first = cellKey(x, 2)
    local second = cellKey(x, 2)
    if first == second then
        return seed + 1
    end
    return seed - 1
end
return kernel
`

const backendSparseGridProofSource = `
local function kernel(seed)
    local cells = {
        ["0:0"] = {terrain = 1, heat = 5 + seed % 5},
        ["1:0"] = {terrain = 2, heat = 3},
        ["2:1"] = {terrain = 1, heat = 7},
        ["3:2"] = {terrain = 3, heat = 2},
        ["4:4"] = {terrain = 2, heat = 9},
    }
    local offsets = {
        {dx = 1, dy = 0},
        {dx = -1, dy = 0},
        {dx = 0, dy = 1},
        {dx = 0, dy = -1},
    }
    local function cellKey(x, y)
        return tostring(x) .. ":" .. tostring(y)
    end
    local total = 0
    for tick = 1, 36 do
        for x = 0, 4 do
            for y = 0, 4 do
                local key = cellKey(x, y)
                local center = cells[key]
                if center ~= nil then
                    for _, offset in offsets do
                        local neighborKey = cellKey(x + offset.dx, y + offset.dy)
                        local neighbor = cells[neighborKey]
                        if neighbor ~= nil then
                            local flow = center.heat - neighbor.heat
                            if flow < 0 then flow = -flow end
                            center.heat = center.heat + tick % 3 - neighbor.terrain
                            total = total + flow + center.heat
                        elseif tick % 5 == 0 then
                            cells[neighborKey] = {terrain = tick % 3 + 1, heat = x + y + tick % 4}
                            total = total + cells[neighborKey].heat
                        end
                    end
                end
            end
        end
    end
    return total + (cells["2:2"] and cells["2:2"].heat or 0) + (cells["4:4"] and cells["4:4"].heat or 0)
end
return kernel
`

const backendArrayOpsProofSource = `
local function kernel(seed)
    local values = {}
    for i = 1, 80 do
        table.insert(values, i % 9 + seed % 3)
    end
    local removed = 0
    for i = 1, 20 do
        removed = removed + table.remove(values, 1)
    end
    return removed + rawlen(values)
end
return kernel
`

const backendClosureProofSource = `
local function kernel(seed)
    local function makeCounter(initial)
        local value = initial
        return function(step)
            value = value + step
            return value
        end
    end
    local counter = makeCounter(10 + seed % 3)
    local total = 0
    for i = 1, 60 do
        total = total + counter(i % 4)
    end
    return total
end
return kernel
`

const backendRecursiveProofSource = `
local function kernel(seed)
    local function fib(n)
        if n < 2 then
            return n
        end
        return fib(n - 1) + fib(n - 2)
    end
    local result = fib(20 + seed % 2)
    return result
end
return kernel
`

const backendVarargProofSource = `
local function kernel(seed)
    local function score(...)
        local count = select("#", ...)
        local a, b, c, d, e = ...
        return count + a * 2 + b * 3 + c * 5 + d * 7 + e * 11
    end
    local total = 0
    for i = 1, 50 + seed % 2 do
        total = total + score(i, i + 1, i + 2, i + 3, i + 4)
    end
    return total
end
return kernel
`

const backendTupleProofSource = `
local function kernel(seed)
    local function split(value)
        return value, value + 1, value + 2
    end
    local total = 0
    for i = 1, 50 + seed % 2 do
        local a, b, c = split(i)
        total = total + a + b * 2 + c * 3
    end
    return total
end
return kernel
`

const backendCoroutineProofSource = `
local function kernel(seed)
    local co = coroutine.create(function(limit)
        local total = 0
        for i = 1, limit do
            total = total + i
            if i % 10 == 0 then
                coroutine.yield(total)
            end
        end
        return total
    end)
    local total = 0
    local ok, value = coroutine.resume(co, 45 + seed % 2)
    while coroutine.status(co) ~= "dead" do
        total = total + value
        ok, value = coroutine.resume(co)
    end
    return total + value
end
return kernel
`

func TestBackendGoNumericProofEmitsDeterministicDirectSource(t *testing.T) {
	ir := backendNumericProofIR(t)
	options := backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedNumericFixture",
		preparedFunctionName: "backendGeneratedNumericPreparedFixture",
	}
	first, err := emitBackendGoNumericProof(ir, options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := emitBackendGoNumericProof(ir, options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("numeric Go proof source is not deterministic")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "generated.go", first, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated source: %v", err)
	}
	text := string(first)
	for _, forbidden := range []string{"switch", "opcode", "descriptor", "for {"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated source contains dispatch marker %q", forbidden)
		}
	}
	if !strings.Contains(text, "math.Floor") ||
		!strings.Contains(text, "goto b") ||
		!strings.Contains(text, "context.numberParameter(0)") ||
		!strings.Contains(text, "context.replayBeforeOperation(") ||
		!strings.Contains(text, "machinePreparedReturnOneNumber(v") {
		t.Fatalf("generated source does not contain direct arithmetic CFG:\n%s", text)
	}
}

func TestBackendGoNumericProofRejectsEscapingObjectProgram(t *testing.T) {
	proto, err := Compile("local value = { field = 1 }\nreturn value")
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProtoIR(&image.prototypes[0])
	if err != nil {
		t.Fatal(err)
	}
	_, err = emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:  "ember",
		functionName: "rejected",
	})
	if err == nil || !strings.Contains(err.Error(), "not scalar replaceable") {
		t.Fatalf("emit object program = %v", err)
	}
}

func TestBackendGoNumericProofRejectsMissingOrNonnumericDirectTarget(t *testing.T) {
	caller, _ := backendNumericCallProofIRs(t)
	_, err := emitBackendGoNumericProof(caller, backendGoNumericOptions{
		packageName:  "ember",
		functionName: "missingTarget",
	})
	if err == nil {
		t.Fatal("emitted direct call without a bound target")
	}

	proto, err := Compile("local function object(value) return { value } end\nreturn object")
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	objectIR, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]backendGoNumericTarget, 3)
	targets[2] = backendGoNumericTarget{
		ir:           objectIR,
		functionName: "objectTarget",
	}
	_, err = emitBackendGoNumericProof(caller, backendGoNumericOptions{
		packageName:   "ember",
		functionName:  "nonnumericTarget",
		directTargets: targets,
	})
	if err == nil || !strings.Contains(err.Error(), "not a numeric leaf") {
		t.Fatalf("emit nonnumeric direct target = %v", err)
	}
}

func TestBackendGoNumericProofIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendNumericProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendNumericProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated Programs unexpectedly share a binding hash")
	}
	options := backendGoNumericOptions{packageName: "ember", functionName: "identityBlind"}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	renamedSource, err := emitBackendGoNumericProof(renamed.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, renamedSource) {
		t.Fatal("source or entrypoint identity selected generated executable code")
	}
}

func TestBackendGoNumericDirectCallIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendNumericCallProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendNumericCallProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated direct-call Programs unexpectedly share a binding hash")
	}
	emit := func(program *backendProgramIR) []byte {
		t.Helper()
		targets := make([]backendGoNumericTarget, 3)
		targets[2] = backendGoNumericTarget{
			ir:           program.modules[0].protos[2],
			functionName: "identityBlindDirectTarget",
		}
		source, err := emitBackendGoNumericProof(program.modules[0].protos[1], backendGoNumericOptions{
			packageName:   "ember",
			functionName:  "identityBlindDirectCaller",
			directTargets: targets,
		})
		if err != nil {
			t.Fatal(err)
		}
		return source
	}
	if !bytes.Equal(emit(base), emit(renamed)) {
		t.Fatal("source or entrypoint identity selected generated direct-call code")
	}
}

func TestBackendGoFixedTuplesIgnoreSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendTupleProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendTupleProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated fixed-tuple Programs unexpectedly share a binding hash")
	}
	emit := func(program *backendProgramIR) []byte {
		t.Helper()
		targets := backendTupleProofTargets(program.modules[0].protos)
		source, err := emitBackendGoNumericProof(program.modules[0].protos[1], backendGoNumericOptions{
			packageName:   "ember",
			functionName:  "identityBlindTupleKernel",
			directTargets: targets,
		})
		if err != nil {
			t.Fatal(err)
		}
		return source
	}
	if !bytes.Equal(emit(base), emit(renamed)) {
		t.Fatal("source or entrypoint identity selected fixed-tuple code")
	}
}

func TestBackendGoScalarTableFieldsIgnoreSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendTableFieldProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendTableFieldProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated table-field Programs unexpectedly share a binding hash")
	}
	options := backendGoNumericOptions{packageName: "ember", functionName: "identityBlindTableFields"}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	renamedSource, err := emitBackendGoNumericProof(renamed.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, renamedSource) {
		t.Fatal("source or entrypoint identity selected scalar table-field code")
	}
}

func TestBackendGoScalarMetatableIndexIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendMetatableIndexProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendMetatableIndexProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated metatable Programs unexpectedly share a binding hash")
	}
	options := backendGoNumericOptions{packageName: "ember", functionName: "identityBlindMetatable"}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	renamedSource, err := emitBackendGoNumericProof(renamed.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, renamedSource) {
		t.Fatal("source or entrypoint identity selected scalar metatable code")
	}
}

func TestBackendGoScalarArrayIterationIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendArrayIterationProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendArrayIterationProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated array-iteration Programs unexpectedly share a binding hash")
	}
	options := backendGoNumericOptions{packageName: "ember", functionName: "identityBlindArrayIteration"}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	renamedSource, err := emitBackendGoNumericProof(renamed.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, renamedSource) {
		t.Fatal("source or entrypoint identity selected scalar array-iteration code")
	}
}

func TestBackendGoFiniteStringStateIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendFiniteStringStateProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendFiniteStringStateProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated finite-string Programs unexpectedly share a binding hash")
	}
	options := backendGoNumericOptions{packageName: "ember", functionName: "identityBlindFiniteStringState"}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	renamedSource, err := emitBackendGoNumericProof(renamed.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, renamedSource) {
		t.Fatal("source or entrypoint identity selected finite-string code")
	}
}

func TestBackendGoFiniteStringStateIgnoresLiteralTextIdentity(t *testing.T) {
	transformed := strings.NewReplacer(
		`"see"`, `"observe"`,
		`"near"`, `"close"`,
		`"cooldown"`, `"recover"`,
		`"lost"`, `"missing"`,
		`"hit"`, `"struck"`,
		`"safe"`, `"secure"`,
		`"rest"`, `"pause"`,
		`"idle"`, `"waiting"`,
		`"chase"`, `"pursuit"`,
		`"attack"`, `"strike"`,
		`"search"`, `"seek"`,
		`"evade"`, `"dodge"`,
	).Replace(backendFiniteStringStateProofSource)
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendFiniteStringStateProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	holdout := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: transformed},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	if base.programHash == holdout.programHash {
		t.Fatal("literal-mutated finite-string Programs unexpectedly share a binding hash")
	}
	options := backendGoNumericOptions{packageName: "ember", functionName: "literalBlindFiniteStringState"}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	holdoutSource, err := emitBackendGoNumericProof(holdout.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, holdoutSource) {
		t.Fatal("literal text identity selected finite-string code")
	}
}

func TestBackendGoScalarArrayOpsIgnoreSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendArrayOpsProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendArrayOpsProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated array-ops Programs unexpectedly share a binding hash")
	}
	options := backendGoNumericOptions{packageName: "ember", functionName: "identityBlindArrayOps"}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	renamedSource, err := emitBackendGoNumericProof(renamed.modules[0].protos[1], options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, renamedSource) {
		t.Fatal("source or entrypoint identity selected scalar array-ops code")
	}
}

func TestBackendGoScalarClosureIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendClosureProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendClosureProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated closure Programs unexpectedly share a binding hash")
	}
	emit := func(program *backendProgramIR) []byte {
		t.Helper()
		targets := backendClosureProofTargets(program.modules[0].protos)
		source, err := emitBackendGoNumericProof(program.modules[0].protos[1], backendGoNumericOptions{
			packageName:   "ember",
			functionName:  "identityBlindClosureKernel",
			directTargets: targets,
		})
		if err != nil {
			t.Fatal(err)
		}
		return source
	}
	if !bytes.Equal(emit(base), emit(renamed)) {
		t.Fatal("source or entrypoint identity selected scalar closure code")
	}
}

func TestBackendGoRecursiveCallIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendRecursiveProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendRecursiveProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated recursive Programs unexpectedly share a binding hash")
	}
	emit := func(program *backendProgramIR) []byte {
		t.Helper()
		targets := backendRecursiveProofTargets(program.modules[0].protos)
		source, err := emitBackendGoNumericProof(program.modules[0].protos[1], backendGoNumericOptions{
			packageName:   "ember",
			functionName:  "identityBlindRecursiveKernel",
			directTargets: targets,
		})
		if err != nil {
			t.Fatal(err)
		}
		return source
	}
	if !bytes.Equal(emit(base), emit(renamed)) {
		t.Fatal("source or entrypoint identity selected recursive code")
	}
}

func TestBackendGoFixedVarargsIgnoreSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendVarargProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendVarargProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated vararg Programs unexpectedly share a binding hash")
	}
	emit := func(program *backendProgramIR) []byte {
		t.Helper()
		targets := backendVarargProofTargets(program.modules[0].protos)
		source, err := emitBackendGoNumericProof(program.modules[0].protos[1], backendGoNumericOptions{
			packageName:   "ember",
			functionName:  "identityBlindVarargKernel",
			directTargets: targets,
		})
		if err != nil {
			t.Fatal(err)
		}
		return source
	}
	if !bytes.Equal(emit(base), emit(renamed)) {
		t.Fatal("source or entrypoint identity selected fixed-vararg code")
	}
}

func TestBackendGoNumericProofGeneratedFixtureIsFreshAndCorrect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendNumericProofIR(t), backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedNumericFixture",
		preparedFunctionName: "backendGeneratedNumericPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("runtime_backend_numeric_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated numeric proof fixture is stale")
	}
	root, err := Compile(backendNumericProofSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.prototypes) != 1 {
		t.Fatalf("numeric proof source child count = %d, want 1", len(root.prototypes))
	}
	for _, seed := range []float64{0, 1, 7, 29} {
		got, ok := backendGeneratedNumericFixture(seed)
		if !ok {
			t.Fatalf("generated numeric proof exited for seed %v", seed)
		}
		want := seed
		for index := 1.0; index <= 64; index++ {
			if index-math.Floor(index/2)*2 == 0 {
				want += index * seed
			} else {
				want--
			}
		}
		if got != want {
			t.Fatalf("generated numeric proof seed %v = %v, want %v", seed, got, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("numeric proof oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedNumericFixture(29)
		}); allocations != 0 {
			t.Fatalf("generated numeric proof allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoNumericPreparedExitFixtureIsFreshAndDirect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendNumericExitProofIR(t), backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedNumericExitFixture",
		preparedFunctionName: "backendGeneratedNumericExitPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("runtime_backend_numeric_exit_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated numeric exit fixture is stale")
	}
	for _, test := range []struct {
		seed float64
		want float64
	}{
		{seed: 1, want: 2},
		{seed: 9, want: 20},
		{seed: 29, want: 60},
	} {
		got, ok := backendGeneratedNumericExitFixture(test.seed)
		if !ok || got != test.want {
			t.Fatalf("numeric exit fixture(%v) = (%v, %t), want (%v, true)", test.seed, got, ok, test.want)
		}
	}
	if _, ok := backendGeneratedNumericExitFixture(math.NaN()); ok {
		t.Fatal("numeric exit fixture accepted NaN comparison")
	}
}

func TestBackendGoNumericDirectCallFixturesAreFreshAndCorrect(t *testing.T) {
	caller, callee := backendNumericCallProofIRs(t)
	calleeOptions := backendGoNumericOptions{
		packageName:  "ember",
		functionName: "backendGeneratedNumericCallAdd",
	}
	generatedCallee, err := emitBackendGoNumericProof(callee, calleeOptions)
	if err != nil {
		t.Fatal(err)
	}
	targets := make([]backendGoNumericTarget, 3)
	targets[2] = backendGoNumericTarget{
		ir:           callee,
		functionName: calleeOptions.functionName,
	}
	callerOptions := backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedNumericCallKernel",
		preparedFunctionName: "backendGeneratedNumericCallPreparedFixture",
		directTargets:        targets,
	}
	generatedCaller, err := emitBackendGoNumericProof(caller, callerOptions)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		path      string
		generated []byte
	}{
		{path: "runtime_backend_numeric_call_add_generated_test.go", generated: generatedCallee},
		{path: "runtime_backend_numeric_call_kernel_generated_test.go", generated: generatedCaller},
	} {
		onDisk, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixture.generated, onDisk) {
			t.Fatalf("generated numeric call fixture %s is stale", fixture.path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), fixture.path, fixture.generated, goparser.AllErrors); err != nil {
			t.Fatalf("parse %s: %v", fixture.path, err)
		}
	}
	callerText := string(generatedCaller)
	if !strings.Contains(callerText, "backendGeneratedNumericCallAdd(v") ||
		!strings.Contains(callerText, "return machinePreparedReplayEntry()") {
		t.Fatalf("generated caller lacks direct call or replay-safe fallback:\n%s", callerText)
	}
	for _, forbidden := range []string{"switch", "opcode", "descriptor", "closure"} {
		if strings.Contains(callerText, forbidden) {
			t.Fatalf("generated caller contains dispatch or materialized closure marker %q", forbidden)
		}
	}

	root, err := Compile(backendNumericCallProofSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.prototypes) != 1 {
		t.Fatalf("numeric call source child count = %d, want 1", len(root.prototypes))
	}
	for _, seed := range []float64{0, 1, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedNumericCallKernel(seed)
		if !ok || got != seed+64 {
			t.Fatalf("generated numeric call kernel(%v) = (%v, %t), want (%v, true)", seed, got, ok, seed+64)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("numeric call oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if _, ok := backendGeneratedNumericCallKernel(math.NaN()); ok {
		t.Fatal("generated direct caller failed to propagate the callee guard")
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedNumericCallKernel(29)
		}); allocations != 0 {
			t.Fatalf("generated numeric direct-call allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoScalarTableFieldFixtureIsFreshAndCorrect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendTableFieldProofIR(t), backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedTableFieldFixture",
		preparedFunctionName: "backendGeneratedTableFieldPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("runtime_backend_table_field_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated scalar table-field fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "runtime_backend_table_field_generated_test.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated scalar table-field source: %v", err)
	}
	text := string(generated)
	if !strings.Contains(text, "var f0 float64") || !strings.Contains(text, "f0 = v") {
		t.Fatalf("generated scalar table-field source lacks typed field locals:\n%s", text)
	}
	for _, forbidden := range []string{"switch", "opcode", "descriptor", "machineTable", "NEW_TABLE", "GET_STRING_FIELD"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated scalar table-field source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendTableFieldProofSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.prototypes) != 1 {
		t.Fatalf("table-field source child count = %d, want 1", len(root.prototypes))
	}
	for _, seed := range []float64{0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedTableFieldFixture(seed)
		want := 1860 + seed - math.Floor(seed/7)*7
		if !ok || got != want {
			t.Fatalf("generated scalar table-field fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("table-field oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle table-field seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedTableFieldFixture(29)
		}); allocations != 0 {
			t.Fatalf("generated scalar table-field allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoScalarMetatableIndexFixtureIsFreshAndCorrect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendMetatableIndexProofIR(t), backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedMetatableIndexFixture",
		preparedFunctionName: "backendGeneratedMetatableIndexPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("runtime_backend_metatable_index_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated scalar metatable-index fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "runtime_backend_metatable_index_generated_test.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated scalar metatable-index source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var f0 float64",
		"var f2 float64",
		"v39 = f0",
		"v41 = f2",
		"context.intrinsicUnchanged(14)",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated scalar metatable-index source lacks %q:\n%s", required, text)
		}
	}
	for _, forbidden := range []string{
		"switch", "opcode", "descriptor", "machineTable",
		"FAST_CALL", "setmetatable", "GET_STRING_FIELD", "NEW_TABLE",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated scalar metatable-index source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendMetatableIndexProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedMetatableIndexFixture(seed)
		mod := seed - math.Floor(seed/2)*2
		want := 1080 + 90*mod
		if !ok || got != want {
			t.Fatalf("generated scalar metatable-index fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle metatable-index seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedMetatableIndexFixture(29)
		}); allocations != 0 {
			t.Fatalf("generated scalar metatable-index allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoScalarMetatableIndexRejectsUnprovedShapes(t *testing.T) {
	tests := map[string]string{
		"function index": `
local function kernel(seed)
    local object = setmetatable({}, {__index = function() return seed end})
    return object.value
end
return kernel
`,
		"dynamic metatable": `
local function kernel(seed, metatable)
    local object = setmetatable({}, metatable)
    return object.value + seed
end
return kernel
`,
		"newindex field": `
local function kernel(seed)
    local fallback = {value = seed}
    local object = setmetatable({}, {__index = fallback, __newindex = fallback})
    return object.value
end
return kernel
`,
		"protected metatable": `
local function kernel(seed)
    local fallback = {value = seed}
    local object = setmetatable({}, {__index = fallback, __metatable = false})
    return object.value
end
return kernel
`,
		"changed index": `
local function kernel(seed)
    local fallback = {value = seed}
    local metatable = {__index = fallback}
    local object = setmetatable({}, metatable)
    metatable.__index = {value = 2}
    return object.value
end
return kernel
`,
		"get metatable": `
local function kernel(seed)
    local object = setmetatable({}, {__index = {value = seed}})
    return getmetatable(object).value
end
return kernel
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			proto, err := Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := proto.preparedCodeImage()
			if err != nil {
				t.Fatal(err)
			}
			ir, err := buildBackendProtoIR(&image.prototypes[1])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
				packageName:  "ember",
				functionName: "rejectUnprovedMetatable",
			}); err == nil {
				t.Fatal("emitted scalar metatable lookup for an unproved shape")
			}
		})
	}
}

func TestBackendGoScalarMethodFixturesAreFreshAndCorrect(t *testing.T) {
	irs := backendMethodProofIRs(t)
	targets := backendMethodProofTargets(irs)
	methodCall := &irs[1].ops[16]
	if methodCall.op != opCallMethodOne ||
		methodCall.access.kind != backendAccessStaticProperty ||
		methodCall.access.constant != methodCall.c {
		t.Fatalf("method call access = opcode %s access %+v", opcodeName(methodCall.op), methodCall.access)
	}
	generatedTarget, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName:   "ember",
		functionName:  "backendGeneratedCounterAdd",
		receiverTable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	generatedCaller, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedMethodKernel",
		preparedFunctionName: "backendGeneratedMethodPreparedFixture",
		directTargets:        targets,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		path      string
		generated []byte
	}{
		{path: "runtime_backend_method_add_generated_test.go", generated: generatedTarget},
		{path: "runtime_backend_method_kernel_generated_test.go", generated: generatedCaller},
	} {
		onDisk, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixture.generated, onDisk) {
			t.Fatalf("generated scalar method fixture %s is stale", fixture.path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), fixture.path, fixture.generated, goparser.AllErrors); err != nil {
			t.Fatalf("parse %s: %v", fixture.path, err)
		}
	}
	targetText := string(generatedTarget)
	callerText := string(generatedCaller)
	for _, required := range []string{
		"backendGeneratedCounterAdd(r0 *float64, p1 float64)",
		"v7 = *r0",
		"*r0 = v8",
		"m16_0 = f0",
		"backendGeneratedCounterAdd(&m16_0, v35)",
		"if !ok16",
		"f0 = m16_0",
	} {
		if !strings.Contains(targetText, required) && !strings.Contains(callerText, required) {
			t.Fatalf("generated scalar method source lacks %q:\ntarget:\n%s\ncaller:\n%s", required, targetText, callerText)
		}
	}
	copyIndex := strings.Index(callerText, "m16_0 = f0")
	callIndex := strings.Index(callerText, "backendGeneratedCounterAdd(&m16_0, v35)")
	guardIndex := strings.Index(callerText, "if !ok16")
	commitIndex := strings.Index(callerText, "f0 = m16_0")
	if copyIndex < 0 || callIndex <= copyIndex || guardIndex <= callIndex || commitIndex <= guardIndex {
		t.Fatalf("generated scalar method caller does not copy, call, guard, then commit:\n%s", callerText)
	}
	for _, forbidden := range []string{
		"switch", "opcode", "descriptor", "machineTable", "machineClosure",
		"CALL_METHOD_ONE", "GET_STRING_FIELD", "SET_STRING_FIELD", "NEW_TABLE",
	} {
		if strings.Contains(targetText, forbidden) || strings.Contains(callerText, forbidden) {
			t.Fatalf("generated scalar method source contains materialization/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendMethodProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedMethodKernel(seed)
		mod := seed - math.Floor(seed/2)*2
		want := 4970 + 70*mod
		if !ok || got != want {
			t.Fatalf("generated scalar method kernel(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("scalar method oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle scalar method seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if _, ok := backendGeneratedCounterAdd(nil, 1); ok {
		t.Fatal("generated scalar method target accepted a nil receiver field")
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedMethodKernel(29)
		}); allocations != 0 {
			t.Fatalf("generated scalar method allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoScalarMethodIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendMethodProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendMethodProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated method Programs unexpectedly share a binding hash")
	}
	options := func(module backendModuleIR) backendGoNumericOptions {
		targets := make([]backendGoNumericTarget, len(module.protos))
		targets[2] = backendGoNumericTarget{
			ir:            module.protos[2],
			functionName:  "identityBlindMethod",
			receiverTable: true,
		}
		return backendGoNumericOptions{
			packageName:   "ember",
			functionName:  "identityBlindKernel",
			directTargets: targets,
		}
	}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options(base.modules[0]))
	if err != nil {
		t.Fatal(err)
	}
	renamedSource, err := emitBackendGoNumericProof(renamed.modules[0].protos[1], options(renamed.modules[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, renamedSource) {
		t.Fatal("source or entrypoint identity selected scalar method code")
	}
}

func TestBackendGoScalarMethodRejectsUnprovedShapes(t *testing.T) {
	tests := map[string]string{
		"captured method": `
local function kernel(seed)
    local counter = {value = 0}
    local bonus = seed
    function counter:add(amount)
        self.value = self.value + amount + bonus
        return self.value
    end
    return counter:add(1)
end
return kernel
`,
		"reassigned method": `
local function kernel(seed)
    local counter = {value = seed}
    function counter:add(amount)
        self.value = self.value + amount
        return self.value
    end
    function counter:add(amount)
        self.value = self.value - amount
        return self.value
    end
    return counter:add(1)
end
return kernel
`,
		"conditional method": `
local function kernel(seed)
    local counter = {value = seed}
    if seed > 0 then
        function counter:add(amount)
            self.value = self.value + amount
            return self.value
        end
    end
    return counter:add(1)
end
return kernel
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			proto, err := Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := proto.preparedCodeImage()
			if err != nil {
				t.Fatal(err)
			}
			irs := make([]*backendProtoIR, len(image.prototypes))
			for protoID := range image.prototypes {
				irs[protoID], err = buildBackendProtoIR(&image.prototypes[protoID])
				if err != nil {
					t.Fatal(err)
				}
			}
			targets := make([]backendGoNumericTarget, len(irs))
			for protoID := 2; protoID < len(irs); protoID++ {
				targets[protoID] = backendGoNumericTarget{
					ir:            irs[protoID],
					functionName:  fmt.Sprintf("rejectedMethod%d", protoID),
					receiverTable: true,
				}
			}
			if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
				packageName:   "ember",
				functionName:  "rejectedMethodKernel",
				directTargets: targets,
			}); err == nil {
				t.Fatal("emitted an unproved scalar method shape")
			}
		})
	}
}

func TestBackendGoScalarArrayIterationFixtureIsFreshAndCorrect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendArrayIterationProofIR(t), backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedArrayIterationFixture",
		preparedFunctionName: "backendGeneratedArrayIterationPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("runtime_backend_array_iteration_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated scalar array-iteration fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "runtime_backend_array_iteration_generated_test.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated scalar array-iteration source: %v", err)
	}
	text := string(generated)
	if !strings.Contains(text, "var a0 [8]float64") ||
		!strings.Contains(text, "v39 = a0[i0]") ||
		!strings.Contains(text, "i0++") {
		t.Fatalf("generated scalar array-iteration source lacks a direct typed loop:\n%s", text)
	}
	for _, forbidden := range []string{
		"switch", "opcode", "descriptor", "machineTable",
		"NEW_TABLE", "SET_FIELD", "PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated scalar array-iteration source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendArrayIterationProofSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.prototypes) != 1 {
		t.Fatalf("array-iteration source child count = %d, want 1", len(root.prototypes))
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedArrayIterationFixture(seed)
		first := 1 + seed - math.Floor(seed/5)*5
		want := 203 + first*first
		if !ok || got != want {
			t.Fatalf("generated scalar array-iteration fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("array-iteration oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle array-iteration seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedArrayIterationFixture(29)
		}); allocations != 0 {
			t.Fatalf("generated scalar array-iteration allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoStructuralStringKeyFixturesAreFreshAndCorrect(t *testing.T) {
	irs := backendStructuralStringKeyProofIRs(t, backendStructuralStringKeyProofSource)
	targets := backendStructuralStringKeyProofTargets(irs)
	generatedTarget, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName:  "ember",
		functionName: "backendGeneratedStructuralStringKey",
	})
	if err != nil {
		t.Fatal(err)
	}
	generatedCaller, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedStructuralStringKeyKernel",
		preparedFunctionName: "backendGeneratedStructuralStringKeyPreparedFixture",
		directTargets:        targets,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		path      string
		generated []byte
	}{
		{path: "runtime_backend_structural_string_key_generated_test.go", generated: generatedTarget},
		{path: "runtime_backend_structural_string_key_kernel_generated_test.go", generated: generatedCaller},
	} {
		onDisk, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixture.generated, onDisk) {
			t.Fatalf("generated structural string-key fixture %s is stale", fixture.path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), fixture.path, fixture.generated, goparser.AllErrors); err != nil {
			t.Fatalf("parse %s: %v", fixture.path, err)
		}
	}
	targetText := string(generatedTarget)
	for _, required := range []string{
		"backendPreparedStringKey",
		"math.Trunc",
		"math.Signbit",
		"first: int32(",
		"second: int32(",
	} {
		if !strings.Contains(targetText, required) {
			t.Fatalf("generated structural string-key target lacks %q:\n%s", required, targetText)
		}
	}
	for _, forbidden := range []string{"tostring", "CONCAT", "machineString", "appendLuauNumber"} {
		if strings.Contains(targetText, forbidden) {
			t.Fatalf("generated structural string-key target contains runtime string marker %q", forbidden)
		}
	}
	callerText := string(generatedCaller)
	if got := strings.Count(callerText, "context.intrinsicUnchangedAt(2, "); got != 2 {
		t.Fatalf("generated structural string-key caller has %d tostring guards, want 2:\n%s", got, callerText)
	}

	root, err := Compile(backendStructuralStringKeyProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedStructuralStringKeyKernel(seed)
		if !ok || got != seed+1 {
			t.Fatalf("generated structural string-key kernel(%v) = (%v, %t), want (%v, true)", seed, got, ok, seed+1)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle structural string-key seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	for _, invalid := range []float64{math.NaN(), math.Inf(1), math.Copysign(0, -1)} {
		if _, ok := backendGeneratedStructuralStringKey(invalid, 2); ok {
			t.Fatalf("generated structural string-key target accepted %v", invalid)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedStructuralStringKeyKernel(29)
		}); allocations != 0 {
			t.Fatalf("generated structural string-key allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoStructuralStringKeyIsIdentityBlindAndRejectsMixedDomains(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendStructuralStringKeyProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	holdoutSource := strings.Replace(backendStructuralStringKeyProofSource, `":"`, `"|"`, 1)
	holdout := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: holdoutSource},
	}, []Entrypoint{{Name: "renamed", Module: LogicalModule("main")}})
	emit := func(program *backendProgramIR) ([]byte, []byte) {
		targets := backendStructuralStringKeyProofTargets(program.modules[0].protos)
		target, err := emitBackendGoNumericProof(program.modules[0].protos[2], backendGoNumericOptions{
			packageName:  "ember",
			functionName: "identityBlindStructuralStringKey",
		})
		if err != nil {
			t.Fatal(err)
		}
		caller, err := emitBackendGoNumericProof(program.modules[0].protos[1], backendGoNumericOptions{
			packageName:          "ember",
			functionName:         "identityBlindStructuralStringKeyKernel",
			preparedFunctionName: "identityBlindStructuralStringKeyPrepared",
			directTargets:        targets,
		})
		if err != nil {
			t.Fatal(err)
		}
		return target, caller
	}
	baseTarget, baseCaller := emit(base)
	holdoutTarget, holdoutCaller := emit(holdout)
	if !bytes.Equal(baseTarget, holdoutTarget) || !bytes.Equal(baseCaller, holdoutCaller) {
		t.Fatal("structural string-key source depends on source/module/entrypoint or separator text identity")
	}

	irs := backendStructuralStringKeyProofIRs(t, backendStructuralStringKeyProofSource)
	options := backendGoNumericOptions{
		packageName:   "ember",
		functionName:  "mixedStructuralStringKeyKernel",
		directTargets: backendStructuralStringKeyProofTargets(irs),
	}
	plan, err := buildBackendGoNumericPlan(irs[1], options)
	if err != nil {
		t.Fatal(err)
	}
	for pc := range irs[1].ops {
		operation := &irs[1].ops[pc]
		if operation.op != opEqual {
			continue
		}
		right := backendOperationUse(operation, operation.c)
		key, ok := plan.keys.keys[right]
		if !ok {
			t.Fatal("structural string-key equality has no right key domain")
		}
		key.domain++
		plan.keys.keys[right] = key
		err := verifyBackendGoNumericOperation(irs[1], plan, options, operation)
		if err == nil || !strings.Contains(err.Error(), "incompatible string domains") {
			t.Fatalf("mixed structural string domains error = %v", err)
		}
		return
	}
	t.Fatal("structural string-key proof has no equality")
}

func TestBackendGoStructuralStringKeyRejectsAmbiguousSeparators(t *testing.T) {
	for _, separator := range []string{`""`, `"1"`, `"-"`, `"12-3"`} {
		source := strings.Replace(backendStructuralStringKeyProofSource, `":"`, separator, 1)
		irs := backendStructuralStringKeyProofIRs(t, source)
		_, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
			packageName:  "ember",
			functionName: "rejectAmbiguousStructuralStringKey",
		})
		if err == nil {
			t.Fatalf("structural string-key compiler accepted ambiguous separator %s", separator)
		}
	}
}

func TestBackendGoStructuralStringKeyRejectsDifferentConstructors(t *testing.T) {
	const source = `
local function kernel(seed)
    local first = tostring(seed) .. ":" .. tostring(2)
    local second = tostring(seed) .. "|" .. tostring(2)
    if first == second then
        return seed + 1
    end
    return seed - 1
end
return kernel
`
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProtoIRWithStrings(
		&image.prototypes[1],
		image.stringRecords,
		image.stringData,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:  "ember",
		functionName: "rejectDifferentStructuralStringConstructors",
	})
	if err == nil || !strings.Contains(err.Error(), "incompatible string domains") {
		t.Fatalf("different structural string constructors error = %v", err)
	}
}

func TestBackendGoStructuralStringKeyGuardsInlineToString(t *testing.T) {
	const source = `
local function kernel(seed)
    local key = tostring(seed) .. ":" .. tostring(2)
    if key == key then
        return seed + 1
    end
    return seed - 1
end
return kernel
`
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProtoIRWithStrings(
		&image.prototypes[1],
		image.stringRecords,
		image.stringData,
	)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "inlineStructuralStringKey",
		preparedFunctionName: "inlineStructuralStringKeyPrepared",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(generated), "context.intrinsicUnchanged("); got != 2 {
		t.Fatalf("inline structural tostring guard count = %d, want 2:\n%s", got, generated)
	}
}

func TestBackendGoSparseGridRecordShapeIsRecognized(t *testing.T) {
	irs := backendStructuralStringKeyProofIRs(t, backendSparseGridProofSource)
	options := backendGoNumericOptions{
		directTargets: backendStructuralStringKeyProofTargets(irs),
	}
	keys := analyzeBackendGoStructuralKeys(irs[1], options)
	records := analyzeBackendGoRecordTables(irs[1], keys)
	if !records.enabled {
		t.Fatalf("sparse-grid record shape was not recognized: %s", records.rejectReason)
	}
	if len(records.maps) != 1 || len(records.arrays) != 1 || len(records.records) != 10 {
		t.Fatalf(
			"sparse-grid record inventory = maps %d arrays %d records %d, want 1/1/10",
			len(records.maps),
			len(records.arrays),
			len(records.records),
		)
	}
	if records.maps[0].recordCount != 6 ||
		len(records.maps[0].fieldNames) != 2 ||
		records.arrays[0].length != 4 ||
		len(records.arrays[0].fieldNames) != 2 {
		t.Fatalf("sparse-grid record shapes = map %#v array %#v", records.maps[0], records.arrays[0])
	}
	generated, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedSparseGrid",
		preparedFunctionName: "backendGeneratedSparseGridPrepared",
		directTargets:        options.directTargets,
	})
	if err != nil {
		t.Fatalf("emit sparse-grid record shape: %v", err)
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "generated_sparse_grid.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated sparse-grid source: %v", err)
	}
}

func TestBackendGoSparseGridFixtureIsFreshAndCorrect(t *testing.T) {
	irs := backendStructuralStringKeyProofIRs(t, backendSparseGridProofSource)
	generated, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedSparseGrid",
		preparedFunctionName: "backendGeneratedSparseGridPreparedFixture",
		directTargets:        backendStructuralStringKeyProofTargets(irs),
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_sparse_grid_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated sparse-grid fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), fixture, generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated sparse-grid source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var mk0 [128]backendPreparedStringKey",
		"var mu0 [128]bool",
		"var mf0_0 [128]float64",
		"var mf0_1 [128]float64",
		"var ra0_0 [4]float64",
		"var ra0_1 [4]float64",
		"for rp",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated sparse-grid source lacks %q", required)
		}
	}
	for _, forbidden := range []string{
		"map[", "make(", "machineTable", "opcode", "descriptor",
		"NEW_TABLE", "SET_FIELD", "GET_FIELD", "SET_INDEX", "GET_INDEX",
		"PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated sparse-grid source contains runtime table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendSparseGridProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_005} {
		got, ok := backendGeneratedSparseGrid(seed)
		if !ok {
			t.Fatalf("generated sparse-grid fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("sparse-grid oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle sparse-grid seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedSparseGrid(29)
		}); allocations != 0 {
			t.Fatalf("generated sparse-grid allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoSparseGridIsIdentityBlindAndRejectsUnprovedShapes(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendSparseGridProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	holdoutSource := strings.ReplaceAll(backendSparseGridProofSource, `:`, `|`)
	holdout := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: holdoutSource},
	}, []Entrypoint{{Name: "renamed", Module: LogicalModule("main")}})
	emit := func(program *backendProgramIR) []byte {
		irs := program.modules[0].protos
		generated, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
			packageName:          "ember",
			functionName:         "identityBlindSparseGrid",
			preparedFunctionName: "identityBlindSparseGridPrepared",
			directTargets:        backendStructuralStringKeyProofTargets(irs),
		})
		if err != nil {
			t.Fatal(err)
		}
		return generated
	}
	if !bytes.Equal(emit(base), emit(holdout)) {
		t.Fatal("sparse-grid source depends on source/module/entrypoint or safe separator text identity")
	}

	tests := map[string]string{
		"noncanonical literal key": strings.Replace(
			backendSparseGridProofSource,
			`["1:0"]`,
			`["01:0"]`,
			1,
		),
		"literal and generated key domains differ": strings.Replace(
			backendSparseGridProofSource,
			`return tostring(x) .. ":" .. tostring(y)`,
			`return tostring(x) .. "|" .. tostring(y)`,
			1,
		),
		"record shape mismatch": strings.Replace(
			backendSparseGridProofSource,
			`{terrain = 2, heat = 3}`,
			`{terrain = 2, moisture = 3}`,
			1,
		),
		"record initialized after insertion": strings.Replace(
			backendSparseGridProofSource,
			`cells[neighborKey] = {terrain = tick % 3 + 1, heat = x + y + tick % 4}
                            total = total + cells[neighborKey].heat`,
			`local created = {terrain = tick % 3 + 1}
                            cells[neighborKey] = created
                            created.heat = x + y + tick % 4
                            total = total + cells[neighborKey].heat`,
			1,
		),
		"escaping map": strings.Replace(
			backendSparseGridProofSource,
			`return total + (cells["2:2"] and cells["2:2"].heat or 0) + (cells["4:4"] and cells["4:4"].heat or 0)`,
			`return cells`,
			1,
		),
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			irs := backendStructuralStringKeyProofIRs(t, source)
			options := backendGoNumericOptions{
				packageName:   "ember",
				functionName:  "rejectUnprovedSparseGrid",
				directTargets: backendStructuralStringKeyProofTargets(irs),
			}
			keys := analyzeBackendGoStructuralKeys(irs[1], options)
			records := analyzeBackendGoRecordTables(irs[1], keys)
			if records.enabled {
				t.Fatalf("sparse-grid record analyzer accepted %s", name)
			}
			if _, err := emitBackendGoNumericProof(irs[1], options); err == nil {
				t.Fatalf("sparse-grid compiler accepted %s", name)
			}
		})
	}
}

func TestBackendGoFiniteStringStateFixtureIsFreshAndCorrect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendFiniteStringStateProofIR(t), backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedFiniteStringStateFixture",
		preparedFunctionName: "backendGeneratedFiniteStringStatePreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	const fixture = "runtime_backend_finite_string_state_generated_test.go"
	onDisk, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated finite-string state fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), fixture, generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated finite-string state source: %v", err)
	}
	text := string(generated)
	if !strings.Contains(text, "var a0 [7]uint32") ||
		!strings.Contains(text, "= uint32(") ||
		!strings.Contains(text, " == v") {
		t.Fatalf("generated finite-string state source lacks typed string IDs and direct comparisons:\n%s", text)
	}
	for _, forbidden := range []string{
		"switch", "opcode", "descriptor", "machineString", "intern",
		"machineTable", "NEW_TABLE", "SET_FIELD", "PREPARE_ITER", "ARRAY_NEXT_JUMP2",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated finite-string state source contains runtime string/table/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendFiniteStringStateProofSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.prototypes) != 1 {
		t.Fatalf("finite-string state source child count = %d, want 1", len(root.prototypes))
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedFiniteStringStateFixture(seed)
		if !ok {
			t.Fatalf("generated finite-string state fixture exited for seed %v", seed)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("finite-string state oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle finite-string state seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if _, ok := backendGeneratedFiniteStringStateFixture(math.NaN()); ok {
		t.Fatal("generated finite-string state fixture accepted NaN loop input")
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedFiniteStringStateFixture(29)
		}); allocations != 0 {
			t.Fatalf("generated finite-string state allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoFiniteStringStateRejectsUnprovedShapes(t *testing.T) {
	tests := map[string]string{
		"generated concatenation": `
local function kernel(seed)
    local state = "idle"
    for i = 1, 4 do
        state = state .. tostring(seed + i)
    end
    return seed
end
return kernel
`,
		"escaping result": `
local function kernel(seed)
    if seed > 0 then
        return "positive"
    end
    return "other"
end
return kernel
`,
		"mixed scalar state": `
local function kernel(seed)
    local state = "idle"
    if seed > 0 then
        state = 1
    end
    if state == "idle" then
        return 1
    end
    return 2
end
return kernel
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			proto, err := Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := proto.preparedCodeImage()
			if err != nil {
				t.Fatal(err)
			}
			ir, err := buildBackendProtoIR(&image.prototypes[1])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
				packageName:  "ember",
				functionName: "rejectUnprovedFiniteStringState",
			}); err == nil {
				t.Fatal("emitted an unproved finite-string shape")
			}
		})
	}
}

func TestBackendGoScalarArrayIterationRejectsUnprovedShapes(t *testing.T) {
	tests := map[string]string{
		"write after iteration": `
local function kernel(seed)
    local values = {seed, 2}
    local total = 0
    for _, value in values do
        total = total + value
    end
    values[3] = 3
    return total
end
return kernel
`,
		"mixed array and hash fields": `
local function kernel(seed)
    local values = {seed, 2}
    values.extra = 3
    local total = 0
    for _, value in values do
        total = total + value
    end
    return total
end
return kernel
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			proto, err := Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := proto.preparedCodeImage()
			if err != nil {
				t.Fatal(err)
			}
			ir, err := buildBackendProtoIR(&image.prototypes[1])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
				packageName:  "ember",
				functionName: "rejectUnprovedArrayShape",
			}); err == nil {
				t.Fatal("emitted scalar array iteration for an unproved shape")
			}
		})
	}
}

func TestBackendGoScalarArrayOpsFixtureIsFreshAndCorrect(t *testing.T) {
	generated, err := emitBackendGoNumericProof(backendArrayOpsProofIR(t), backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedArrayOpsFixture",
		preparedFunctionName: "backendGeneratedArrayOpsPreparedFixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("runtime_backend_array_ops_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated scalar array-ops fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "runtime_backend_array_ops_generated_test.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated scalar array-ops source: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"var a0 [80]float64",
		"t0 = h0 + n0",
		"a0[t0] = v",
		"v70 = a0[h0]",
		"v75 = float64(n0)",
		"context.intrinsicUnchanged(14)",
		"context.intrinsicUnchanged(23)",
		"context.intrinsicUnchanged(28)",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated scalar array-ops source lacks %q:\n%s", required, text)
		}
	}
	for _, forbidden := range []string{
		"switch", "opcode", "descriptor", "machineTable",
		"FAST_CALL", "tableInsert", "tableRemove", "rawLen",
		"append(", "copy(",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated scalar array-ops source contains runtime mutation/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendArrayOpsProofSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.prototypes) != 1 {
		t.Fatalf("array-ops source child count = %d, want 1", len(root.prototypes))
	}
	for _, seed := range []float64{0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedArrayOpsFixture(seed)
		want := backendArrayOpsExpected(seed)
		if !ok || got != want {
			t.Fatalf("generated scalar array-ops fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("array-ops oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle array-ops seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedArrayOpsFixture(29)
		}); allocations != 0 {
			t.Fatalf("generated scalar array-ops allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoScalarArrayOpsRejectUnprovedMutationShapes(t *testing.T) {
	tests := map[string]string{
		"position insert": `
local function kernel(seed)
    local values = {1}
    table.insert(values, 1, seed)
    return rawlen(values)
end
return kernel
`,
		"non-front remove": `
local function kernel(seed)
    local values = {seed, 2}
    return table.remove(values, 2)
end
return kernel
`,
		"unbounded append": `
local function kernel(seed)
    local values = {}
    while seed > 0 do
        table.insert(values, seed)
        seed = seed - 1
    end
    return rawlen(values)
end
return kernel
`,
		"nonprogressing numeric loop": `
local function kernel(seed)
    local values = {}
    for i = 9007199254740992, 9007199254740994 do
        table.insert(values, seed)
    end
    return rawlen(values)
end
return kernel
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			proto, err := Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := proto.preparedCodeImage()
			if err != nil {
				t.Fatal(err)
			}
			ir, err := buildBackendProtoIR(&image.prototypes[1])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
				packageName:  "ember",
				functionName: "rejectUnprovedArrayMutation",
			}); err == nil {
				t.Fatal("emitted scalar array operations for an unproved mutation shape")
			}
		})
	}
}

func TestBackendGoScalarClosureFixturesAreFreshAndCorrect(t *testing.T) {
	irs := backendClosureProofIRs(t)
	targetOptions := backendGoNumericOptions{
		packageName:  "ember",
		functionName: "backendGeneratedCounterBody",
	}
	generatedTarget, err := emitBackendGoNumericProof(irs[3], targetOptions)
	if err != nil {
		t.Fatal(err)
	}
	targets := backendClosureProofTargets(irs)
	callerOptions := backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedClosureKernel",
		preparedFunctionName: "backendGeneratedClosurePreparedFixture",
		directTargets:        targets,
	}
	generatedCaller, err := emitBackendGoNumericProof(irs[1], callerOptions)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		path      string
		generated []byte
	}{
		{path: "runtime_backend_closure_body_generated_test.go", generated: generatedTarget},
		{path: "runtime_backend_closure_kernel_generated_test.go", generated: generatedCaller},
	} {
		onDisk, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixture.generated, onDisk) {
			t.Fatalf("generated scalar closure fixture %s is stale", fixture.path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), fixture.path, fixture.generated, goparser.AllErrors); err != nil {
			t.Fatalf("parse %s: %v", fixture.path, err)
		}
	}
	targetText := string(generatedTarget)
	for _, required := range []string{"u0 *float64", "v5 = *u0", "*u0 = v7"} {
		if !strings.Contains(targetText, required) {
			t.Fatalf("generated closure body lacks %q:\n%s", required, targetText)
		}
	}
	callerText := string(generatedCaller)
	for _, required := range []string{
		"var c0 float64",
		"c0 = v23",
		"s0 = c0",
		"backendGeneratedCounterBody(&s0",
		"c0 = s0",
	} {
		if !strings.Contains(callerText, required) {
			t.Fatalf("generated closure caller lacks %q:\n%s", required, callerText)
		}
	}
	scratch := strings.Index(callerText, "s0 = c0")
	call := strings.Index(callerText, "backendGeneratedCounterBody(&s0")
	guard := strings.Index(callerText, "if !ok16")
	commit := strings.Index(callerText, "c0 = s0")
	if scratch < 0 || call <= scratch || guard <= call || commit <= guard {
		t.Fatalf("generated closure caller does not copy, guard, then commit captured state:\n%s", callerText)
	}
	for _, forbidden := range []string{
		"switch", "opcode", "descriptor", "machineClosure", "machineUpvalue",
		"CALL_LOCAL_ONE", "GET_UPVALUE", "SET_UPVALUE",
	} {
		if strings.Contains(targetText, forbidden) || strings.Contains(callerText, forbidden) {
			t.Fatalf("generated scalar closure source contains runtime dispatch/materialization marker %q", forbidden)
		}
	}

	root, err := Compile(backendClosureProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedClosureKernel(seed)
		want := backendClosureExpected(seed)
		if !ok || got != want {
			t.Fatalf("generated scalar closure fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("closure oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle closure seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if got, ok := backendGeneratedClosureKernel(math.NaN()); !ok || !math.IsNaN(got) {
		t.Fatalf("generated scalar closure NaN result = (%v, %t), want (NaN, true)", got, ok)
	}
	oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
		args: []Value{NumberValue(math.NaN())},
	})
	if err != nil {
		t.Fatal(err)
	}
	oracleNaN, ok := oracle[0].Number()
	if !ok || !math.IsNaN(oracleNaN) {
		t.Fatalf("closure oracle NaN result = %v (%t), want NaN", oracleNaN, ok)
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedClosureKernel(29)
		}); allocations != 0 {
			t.Fatalf("generated scalar closure allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoScalarClosureRejectsUnprovedShapes(t *testing.T) {
	tests := map[string]string{
		"two captures": `
local function kernel(seed)
    local function makeCounter(initial)
        local value = initial
        local calls = 0
        return function(step)
            calls = calls + 1
            value = value + step
            return value + calls
        end
    end
    local counter = makeCounter(seed)
    return counter(1)
end
return kernel
`,
		"derived capture": `
local function kernel(seed)
    local function makeCounter(initial)
        local value = initial + 1
        return function(step)
            value = value + step
            return value
        end
    end
    local counter = makeCounter(seed)
    return counter(1)
end
return kernel
`,
		"read only copied capture": `
local function kernel(seed)
    local function makeCounter(initial)
        local value = initial
        return function()
            return value
        end
    end
    local counter = makeCounter(seed)
    return counter()
end
return kernel
`,
		"merged independent closures": `
local function kernel(seed)
    local function makeCounter(initial)
        local value = initial
        return function(step)
            value = value + step
            return value
        end
    end
    local counter = nil
    if seed > 0 then
        counter = makeCounter(seed)
    else
        counter = makeCounter(-seed)
    end
    return counter(1)
end
return kernel
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			proto, err := Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := proto.preparedCodeImage()
			if err != nil {
				t.Fatal(err)
			}
			irs := make([]*backendProtoIR, len(image.prototypes))
			for protoID := range image.prototypes {
				irs[protoID], err = buildBackendProtoIR(&image.prototypes[protoID])
				if err != nil {
					t.Fatal(err)
				}
			}
			targets := make([]backendGoNumericTarget, len(irs))
			for protoID := 2; protoID < len(irs); protoID++ {
				targets[protoID] = backendGoNumericTarget{
					ir:           irs[protoID],
					functionName: fmt.Sprintf("rejectedClosureTarget%d", protoID),
				}
			}
			if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
				packageName:   "ember",
				functionName:  "rejectUnprovedClosure",
				directTargets: targets,
			}); err == nil {
				t.Fatal("emitted scalar closure for an unproved capture shape")
			}
		})
	}
}

func TestBackendGoRecursiveFixturesAreFreshAndCorrect(t *testing.T) {
	irs := backendRecursiveProofIRs(t)
	targetOptions := backendGoNumericOptions{
		packageName:   "ember",
		functionName:  "backendGeneratedRecursiveFib",
		selfRecursive: true,
	}
	generatedTarget, err := emitBackendGoNumericProof(irs[2], targetOptions)
	if err != nil {
		t.Fatal(err)
	}
	targets := backendRecursiveProofTargets(irs)
	callerOptions := backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedRecursiveKernel",
		preparedFunctionName: "backendGeneratedRecursivePreparedFixture",
		directTargets:        targets,
	}
	generatedCaller, err := emitBackendGoNumericProof(irs[1], callerOptions)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		path      string
		generated []byte
	}{
		{path: "runtime_backend_recursive_fib_generated_test.go", generated: generatedTarget},
		{path: "runtime_backend_recursive_kernel_generated_test.go", generated: generatedCaller},
	} {
		onDisk, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixture.generated, onDisk) {
			t.Fatalf("generated recursive fixture %s is stale", fixture.path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), fixture.path, fixture.generated, goparser.AllErrors); err != nil {
			t.Fatalf("parse %s: %v", fixture.path, err)
		}
	}
	targetText := string(generatedTarget)
	for _, required := range []string{
		"math.IsNaN(p0) || p0 > 24",
		"backendGeneratedRecursiveFibBody(v5)",
		"backendGeneratedRecursiveFibBody(v8)",
	} {
		if !strings.Contains(targetText, required) {
			t.Fatalf("generated recursive target lacks %q:\n%s", required, targetText)
		}
	}
	for _, forbidden := range []string{
		"u0 *float64", "machineClosure", "machineUpvalue",
		"CALL_UPVALUE_ONE", "switch", "opcode", "descriptor",
	} {
		if strings.Contains(targetText, forbidden) || strings.Contains(string(generatedCaller), forbidden) {
			t.Fatalf("generated recursive source contains runtime dispatch/materialization marker %q", forbidden)
		}
	}

	root, err := Compile(backendRecursiveProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedRecursiveKernel(seed)
		want := backendRecursiveExpected(seed)
		if !ok || got != want {
			t.Fatalf("generated recursive fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("recursive oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle recursion seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if _, ok := backendGeneratedRecursiveFib(33); ok {
		t.Fatal("generated recursive target accepted an unproved recursion argument")
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedRecursiveKernel(29)
		}); allocations != 0 {
			t.Fatalf("generated recursive allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoRecursiveTargetRejectsUnboundedSelfCall(t *testing.T) {
	tests := map[string]string{
		"nondecreasing": `
local function kernel(seed)
    local function recurse(n)
        if n < 1 then
            return n
        end
        return recurse(n + 1)
    end
    local result = recurse(seed)
    return result
end
return kernel
`,
		"subunit decrement": `
local function kernel(seed)
    local function recurse(n)
        if n < 1 then
            return n
        end
        return recurse(n - 0.5)
    end
    local result = recurse(seed)
    return result
end
return kernel
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			proto, err := Compile(source)
			if err != nil {
				t.Fatal(err)
			}
			image, err := proto.preparedCodeImage()
			if err != nil {
				t.Fatal(err)
			}
			ir, err := buildBackendProtoIR(&image.prototypes[2])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
				packageName:   "ember",
				functionName:  "rejectUnboundedRecursion",
				selfRecursive: true,
			}); err == nil {
				t.Fatal("emitted unbounded self recursion")
			}
		})
	}
}

func TestBackendGoRecursiveTargetRejectsUnsupportedSelfCapture(t *testing.T) {
	irs := backendRecursiveProofIRs(t)
	for name, mutate := range map[string]func(*machineUpvalue){
		"nonlocal": func(upvalue *machineUpvalue) {
			upvalue.local = 0
		},
		"copied": func(upvalue *machineUpvalue) {
			upvalue.copy = 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			target := *irs[2]
			target.upvalues = append([]machineUpvalue(nil), target.upvalues...)
			mutate(&target.upvalues[0])
			if _, err := emitBackendGoNumericProof(&target, backendGoNumericOptions{
				packageName:   "ember",
				functionName:  "rejectUnsupportedSelfCapture",
				selfRecursive: true,
			}); err == nil {
				t.Fatal("emitted recursion through an unsupported capture")
			}
		})
	}
}

func TestBackendGoRecursiveCallerRejectsDifferentCapturedClosure(t *testing.T) {
	irs := backendRecursiveProofIRs(t)
	target := *irs[2]
	target.upvalues = append([]machineUpvalue(nil), target.upvalues...)
	target.upvalues[0].index++
	targets := backendRecursiveProofTargets(irs)
	targets[2].ir = &target
	if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:   "ember",
		functionName:  "rejectDifferentCapturedClosure",
		directTargets: targets,
	}); err == nil {
		t.Fatal("emitted a captured call that was not proven to target itself")
	}
}

func TestBackendGoFixedVarargFixturesAreFreshAndCorrect(t *testing.T) {
	irs := backendVarargProofIRs(t)
	generatedTarget, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName:      "ember",
		functionName:     "backendGeneratedVarargScore",
		fixedVarargCount: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	targets := backendVarargProofTargets(irs)
	generatedCaller, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedVarargKernel",
		preparedFunctionName: "backendGeneratedVarargPreparedFixture",
		directTargets:        targets,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		path      string
		generated []byte
	}{
		{path: "runtime_backend_vararg_score_generated_test.go", generated: generatedTarget},
		{path: "runtime_backend_vararg_kernel_generated_test.go", generated: generatedCaller},
	} {
		onDisk, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixture.generated, onDisk) {
			t.Fatalf("generated fixed-vararg fixture %s is stale", fixture.path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), fixture.path, fixture.generated, goparser.AllErrors); err != nil {
			t.Fatalf("parse %s: %v", fixture.path, err)
		}
	}
	targetText := string(generatedTarget)
	callerText := string(generatedCaller)
	for _, required := range []string{
		"backendGeneratedVarargScore(p0 float64, p1 float64, p2 float64, p3 float64, p4 float64)",
		"v9 = 5",
		"v10 = p0",
		"v14 = p4",
	} {
		if !strings.Contains(targetText, required) {
			t.Fatalf("generated fixed-vararg target lacks %q:\n%s", required, targetText)
		}
	}
	for _, required := range []string{
		"backendGeneratedVarargScore(v36, v38, v40, v42, v44)",
		"context.intrinsicUnchangedAt(2, 0)",
	} {
		if !strings.Contains(callerText, required) {
			t.Fatalf("generated fixed-vararg caller lacks %q:\n%s", required, callerText)
		}
	}
	for _, forbidden := range []string{
		"[]float64", "...", "VARARG", "FAST_CALL",
		"switch", "opcode", "descriptor", "machineClosure",
	} {
		if strings.Contains(targetText, forbidden) || strings.Contains(callerText, forbidden) {
			t.Fatalf("generated fixed-vararg source contains materialization/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendVarargProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedVarargKernel(seed)
		want := backendVarargExpected(seed)
		if !ok || got != want {
			t.Fatalf("generated fixed-vararg fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("fixed-vararg oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle fixed varargs seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedVarargKernel(29)
		}); allocations != 0 {
			t.Fatalf("generated fixed-vararg allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoFixedVarargTargetRejectsMismatchedArity(t *testing.T) {
	irs := backendVarargProofIRs(t)
	targets := backendVarargProofTargets(irs)
	targets[2].fixedVarargCount = 4
	if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:   "ember",
		functionName:  "rejectMismatchedVarargArity",
		directTargets: targets,
	}); err == nil {
		t.Fatal("emitted a fixed-vararg call with mismatched arity")
	}
	if _, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName:      "ember",
		functionName:     "rejectUnboundedVarargArity",
		fixedVarargCount: backendGoMaxFixedVarargCount + 1,
	}); err == nil {
		t.Fatal("emitted a fixed-vararg target above the code-size bound")
	}
}

func TestBackendGoFixedVarargTargetRejectsOpenResults(t *testing.T) {
	const source = `
local function kernel()
    local function passthrough(...)
        return ...
    end
    return passthrough(1, 2, 3)
end
return kernel
`
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProtoIR(&image.prototypes[2])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:      "ember",
		functionName:     "rejectOpenVarargResults",
		fixedVarargCount: 3,
	}); err == nil {
		t.Fatal("emitted open fixed-vararg results")
	}
}

func TestBackendGoFixedVarargTargetKeepsNamedParameterPrefix(t *testing.T) {
	const source = `
local function score(base, ...)
    local count = select("#", ...)
    local a, b = ...
    return base + count + a + b
end
return score
`
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	generated, err := emitBackendGoNumericProof(ir, backendGoNumericOptions{
		packageName:      "ember",
		functionName:     "fixedVarargsAfterNamedParameter",
		fixedVarargCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	for _, required := range []string{
		"fixedVarargsAfterNamedParameter(p0 float64, p1 float64, p2 float64)",
		"= p0",
		"= p1",
		"= p2",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated named-prefix fixed varargs lack %q:\n%s", required, text)
		}
	}
}

func TestBackendGoFixedTupleFixturesAreFreshAndCorrect(t *testing.T) {
	irs := backendTupleProofIRs(t)
	generatedTarget, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName:  "ember",
		functionName: "backendGeneratedTupleSplit",
	})
	if err != nil {
		t.Fatal(err)
	}
	targets := backendTupleProofTargets(irs)
	generatedCaller, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedTupleKernel",
		preparedFunctionName: "backendGeneratedTuplePreparedFixture",
		directTargets:        targets,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		path      string
		generated []byte
	}{
		{path: "runtime_backend_tuple_split_generated_test.go", generated: generatedTarget},
		{path: "runtime_backend_tuple_kernel_generated_test.go", generated: generatedCaller},
	} {
		onDisk, err := os.ReadFile(fixture.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(fixture.generated, onDisk) {
			t.Fatalf("generated fixed-tuple fixture %s is stale", fixture.path)
		}
		if _, err := goparser.ParseFile(token.NewFileSet(), fixture.path, fixture.generated, goparser.AllErrors); err != nil {
			t.Fatalf("parse %s: %v", fixture.path, err)
		}
	}
	targetText := string(generatedTarget)
	callerText := string(generatedCaller)
	for _, required := range []string{
		"backendGeneratedTupleSplit(p0 float64) (float64, float64, float64, bool)",
		"return v5, v7, v9, true",
		"v40, v41, v42, ok14 = backendGeneratedTupleSplit(v39)",
	} {
		if !strings.Contains(targetText, required) && !strings.Contains(callerText, required) {
			t.Fatalf("generated fixed-tuple source lacks %q:\ntarget:\n%s\ncaller:\n%s", required, targetText, callerText)
		}
	}
	for _, forbidden := range []string{
		"[]float64", "machineClosure", "CALL", "RETURN",
		"switch", "opcode", "descriptor",
	} {
		if strings.Contains(targetText, forbidden) || strings.Contains(callerText, forbidden) {
			t.Fatalf("generated fixed-tuple source contains materialization/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendTupleProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedTupleKernel(seed)
		want := backendTupleExpected(seed)
		if !ok || got != want {
			t.Fatalf("generated fixed-tuple fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("fixed-tuple oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle fixed tuple seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedTupleKernel(29)
		}); allocations != 0 {
			t.Fatalf("generated fixed-tuple allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoFixedTupleRejectsUnboundedOrMismatchedResults(t *testing.T) {
	irs := backendTupleProofIRs(t)
	targets := backendTupleProofTargets(irs)

	if _, err := emitBackendGoNumericProof(irs[2], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "rejectPreparedTupleEntry",
		preparedFunctionName: "rejectPreparedTupleEntryOwner",
	}); err == nil {
		t.Fatal("emitted a multiple-result prepared entry")
	}

	caller := *irs[1]
	caller.ops = append([]backendOperationIR(nil), caller.ops...)
	for pc := range caller.ops {
		if caller.ops[pc].op == opCall {
			caller.ops[pc].callResults--
			break
		}
	}
	if _, err := emitBackendGoNumericProof(&caller, backendGoNumericOptions{
		packageName:   "ember",
		functionName:  "rejectMismatchedTupleResults",
		directTargets: targets,
	}); err == nil {
		t.Fatal("emitted a fixed tuple with mismatched call results")
	}

	target := *irs[2]
	target.ops = append([]backendOperationIR(nil), target.ops...)
	for pc := range target.ops {
		if target.ops[pc].op == opReturn {
			target.ops[pc].returnCount = backendGoMaxFixedResultCount + 1
			break
		}
	}
	if _, err := emitBackendGoNumericProof(&target, backendGoNumericOptions{
		packageName:  "ember",
		functionName: "rejectUnboundedTupleResults",
	}); err == nil {
		t.Fatal("emitted a fixed tuple above the code-size bound")
	}
}

func TestBackendGoScalarCoroutineFixtureIsFreshDirectAndCorrect(t *testing.T) {
	irs, deadString := backendCoroutineProofIRs(t, backendCoroutineProofSource)
	targets := backendCoroutineProofTargets(irs)
	generated, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
		packageName:          "ember",
		functionName:         "backendGeneratedCoroutineKernel",
		preparedFunctionName: "backendGeneratedCoroutinePreparedFixture",
		directTargets:        targets,
		coroutineDeadString:  deadString,
	})
	if err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile("runtime_backend_coroutine_generated_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, onDisk) {
		t.Fatal("generated scalar coroutine fixture is stale")
	}
	if _, err := goparser.ParseFile(token.NewFileSet(), "runtime_backend_coroutine_generated_test.go", generated, goparser.AllErrors); err != nil {
		t.Fatalf("parse generated scalar coroutine fixture: %v", err)
	}
	text := string(generated)
	for _, required := range []string{
		"type backendGeneratedCoroutineBodyState struct",
		"switch state.state",
		"state.state = 1",
		"return v30, false, true",
		"backendGeneratedCoroutineBody(&q0, v36, true)",
		"backendGeneratedCoroutineBody(&q0, 0, false)",
		"context.intrinsicUnchangedAt(2, 11)",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("generated scalar coroutine source lacks %q:\n%s", required, text)
		}
	}
	for _, forbidden := range []string{
		"opcode", "descriptor", "machineCoroutine", "machineClosure",
		"FAST_CALL", "COROUTINE_", "LOAD_GLOBAL", "GET_STRING_FIELD",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("generated scalar coroutine source contains materialization/dispatch marker %q", forbidden)
		}
	}

	root, err := Compile(backendCoroutineProofSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, seed := range []float64{-29, -1, 0, 1, 7, 29, 1_000_000_000_005} {
		got, ok := backendGeneratedCoroutineKernel(seed)
		want := backendCoroutineExpected(seed)
		if !ok || got != want {
			t.Fatalf("generated scalar coroutine fixture(%v) = (%v, %t), want (%v, true)", seed, got, ok, want)
		}
		oracle, err := executeProto(context.Background(), root.prototypes[0], nil, executeOptions{
			args: []Value{NumberValue(seed)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(oracle) != 1 {
			t.Fatalf("scalar coroutine oracle result count = %d, want 1", len(oracle))
		}
		oracleNumber, ok := oracle[0].Number()
		if !ok || oracleNumber != got {
			t.Fatalf("generated/oracle scalar coroutine seed %v = %v/%v (%t)", seed, got, oracleNumber, ok)
		}
	}
	if _, ok := backendGeneratedCoroutineKernel(math.NaN()); ok {
		t.Fatal("generated scalar coroutine accepted a NaN loop limit")
	}
	if _, _, ok := backendGeneratedCoroutineBody(nil, 45, true); ok {
		t.Fatal("generated scalar coroutine body accepted a nil state")
	}
	if !checkptrInstrumentedTest() {
		if allocations := testing.AllocsPerRun(1000, func() {
			_, _ = backendGeneratedCoroutineKernel(29)
		}); allocations != 0 {
			t.Fatalf("generated scalar coroutine allocations = %v, want 0", allocations)
		}
	}
}

func TestBackendGoScalarCoroutineIgnoresSourceIdentity(t *testing.T) {
	base := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "source/main", Text: backendCoroutineProofSource},
	}, []Entrypoint{{Name: "main", Module: LogicalModule("main")}})
	renamed := buildBackendProgramTest(t, backendProgramTestLoader{
		"logical:main": {Name: "opaque/renamed/source", Text: backendCoroutineProofSource},
	}, []Entrypoint{{Name: "renamed-entrypoint", Module: LogicalModule("main")}})
	if base.programHash == renamed.programHash {
		t.Fatal("identity-mutated coroutine Programs unexpectedly share a binding hash")
	}
	options := func(module backendModuleIR) backendGoNumericOptions {
		targets := make([]backendGoNumericTarget, len(module.protos))
		targets[2] = backendGoNumericTarget{
			ir:           module.protos[2],
			functionName: "identityBlindCoroutineBody",
		}
		return backendGoNumericOptions{
			packageName:         "ember",
			functionName:        "identityBlindCoroutineKernel",
			directTargets:       targets,
			coroutineDeadString: backendCoroutineDeadStringID(t, module.code),
		}
	}
	baseSource, err := emitBackendGoNumericProof(base.modules[0].protos[1], options(base.modules[0]))
	if err != nil {
		t.Fatal(err)
	}
	renamedSource, err := emitBackendGoNumericProof(renamed.modules[0].protos[1], options(renamed.modules[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseSource, renamedSource) {
		t.Fatal("source or entrypoint identity selected scalar coroutine code")
	}
}

func TestBackendGoScalarCoroutineRejectsUnprovedShapes(t *testing.T) {
	tests := map[string]string{
		"captured body": `
local function kernel(seed)
    local bonus = seed
    local co = coroutine.create(function(limit)
        coroutine.yield(limit + bonus)
        return limit
    end)
    local ok, value = coroutine.resume(co, 1)
    while coroutine.status(co) ~= "dead" do
        ok, value = coroutine.resume(co)
    end
    return value
end
return kernel
`,
		"escaping coroutine": `
local function kernel(seed)
    local co = coroutine.create(function(limit)
        coroutine.yield(limit)
        return limit
    end)
    return co
end
return kernel
`,
		"multiple coroutines": `
local function kernel(seed)
    local first = coroutine.create(function(limit) coroutine.yield(limit) return limit end)
    local second = coroutine.create(function(limit) coroutine.yield(limit) return limit end)
    local ok, value = coroutine.resume(first, seed)
    return value
end
return kernel
`,
		"resume arguments after first": `
local function kernel(seed)
    local co = coroutine.create(function(limit)
        coroutine.yield(limit)
        return limit
    end)
    local ok, value = coroutine.resume(co, seed)
    while coroutine.status(co) ~= "dead" do
        ok, value = coroutine.resume(co, seed)
    end
    return value
end
return kernel
`,
		"consumed resumed value": `
local function kernel(seed)
    local co = coroutine.create(function(limit)
        local resumed = coroutine.yield(limit)
        return limit + resumed
    end)
    local ok, value = coroutine.resume(co, seed)
    while coroutine.status(co) ~= "dead" do
        ok, value = coroutine.resume(co, 1)
    end
    return value
end
return kernel
`,
		"non-dead status comparison": `
local function kernel(seed)
    local co = coroutine.create(function(limit)
        coroutine.yield(limit)
        return limit
    end)
    local ok, value = coroutine.resume(co, seed)
    while coroutine.status(co) == "suspended" do
        ok, value = coroutine.resume(co)
    end
    return value
end
return kernel
`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			irs, deadString := backendCoroutineProofIRs(t, source)
			targets := backendCoroutineProofTargets(irs)
			if _, err := emitBackendGoNumericProof(irs[1], backendGoNumericOptions{
				packageName:         "ember",
				functionName:        "rejectUnprovedCoroutine",
				directTargets:       targets,
				coroutineDeadString: deadString,
			}); err == nil {
				t.Fatal("emitted an unproved scalar coroutine shape")
			}
		})
	}
}

func backendRecursiveExpected(seed float64) float64 {
	n := 20 + seed - math.Floor(seed/2)*2
	var fib func(float64) float64
	fib = func(value float64) float64 {
		if value < 2 {
			return value
		}
		return fib(value-1) + fib(value-2)
	}
	return fib(n)
}

func backendCoroutineExpected(seed float64) float64 {
	mod := seed - math.Floor(seed/2)*2
	return 2585 + 46*mod
}

func backendVarargExpected(seed float64) float64 {
	limit := 50 + seed - math.Floor(seed/2)*2
	total := 0.0
	for index := 1.0; index <= limit; index++ {
		total += 5 +
			index*2 +
			(index+1)*3 +
			(index+2)*5 +
			(index+3)*7 +
			(index+4)*11
	}
	return total
}

func backendTupleExpected(seed float64) float64 {
	limit := 50 + seed - math.Floor(seed/2)*2
	total := 0.0
	for value := 1.0; value <= limit; value++ {
		total += value + (value+1)*2 + (value+2)*3
	}
	return total
}

func backendClosureExpected(seed float64) float64 {
	value := 10 + seed - math.Floor(seed/3)*3
	total := 0.0
	for index := 1.0; index <= 60; index++ {
		value += index - math.Floor(index/4)*4
		total += value
	}
	return total
}

func backendArrayOpsExpected(seed float64) float64 {
	seedMod := seed - math.Floor(seed/3)*3
	removed := 0.0
	for index := 1.0; index <= 20; index++ {
		removed += index - math.Floor(index/9)*9 + seedMod
	}
	return removed + 60
}

func backendNumericProofIR(t *testing.T) *backendProtoIR {
	t.Helper()
	proto, err := Compile(backendNumericProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("numeric proof Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func backendNumericExitProofIR(t *testing.T) *backendProtoIR {
	t.Helper()
	proto, err := Compile(backendNumericExitProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("numeric exit proof Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func backendNumericCallProofIRs(t *testing.T) (caller, callee *backendProtoIR) {
	t.Helper()
	proto, err := Compile(backendNumericCallProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 3 {
		t.Fatalf("numeric call proof Proto count = %d, want 3", len(image.prototypes))
	}
	caller, err = buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	callee, err = buildBackendProtoIR(&image.prototypes[2])
	if err != nil {
		t.Fatal(err)
	}
	return caller, callee
}

func backendTableFieldProofIR(t *testing.T) *backendProtoIR {
	t.Helper()
	proto, err := Compile(backendTableFieldProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("table-field proof Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func backendMetatableIndexProofIR(t *testing.T) *backendProtoIR {
	t.Helper()
	proto, err := Compile(backendMetatableIndexProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("metatable-index proof Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func backendMethodProofIRs(t *testing.T) []*backendProtoIR {
	t.Helper()
	proto, err := Compile(backendMethodProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 3 {
		t.Fatalf("method proof Proto count = %d, want 3", len(image.prototypes))
	}
	irs := make([]*backendProtoIR, len(image.prototypes))
	for protoID := range image.prototypes {
		irs[protoID], err = buildBackendProtoIR(&image.prototypes[protoID])
		if err != nil {
			t.Fatal(err)
		}
	}
	return irs
}

func backendMethodProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	targets[2] = backendGoNumericTarget{
		ir:            irs[2],
		functionName:  "backendGeneratedCounterAdd",
		receiverTable: true,
	}
	return targets
}

func backendArrayIterationProofIR(t *testing.T) *backendProtoIR {
	t.Helper()
	proto, err := Compile(backendArrayIterationProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("array-iteration proof Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func backendFiniteStringStateProofIR(t *testing.T) *backendProtoIR {
	t.Helper()
	proto, err := Compile(backendFiniteStringStateProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("finite-string state proof Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func backendStructuralStringKeyProofIRs(t *testing.T, source string) []*backendProtoIR {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) < 3 {
		t.Fatalf("structural string-key proof Proto count = %d, want at least 3", len(image.prototypes))
	}
	irs := make([]*backendProtoIR, len(image.prototypes))
	for protoID := range image.prototypes {
		irs[protoID], err = buildBackendProtoIRWithStrings(
			&image.prototypes[protoID],
			image.stringRecords,
			image.stringData,
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	return irs
}

func backendStructuralStringKeyProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	if len(irs) > 2 {
		targets[2] = backendGoNumericTarget{
			ir:           irs[2],
			functionName: "backendGeneratedStructuralStringKey",
		}
	}
	return targets
}

func backendArrayOpsProofIR(t *testing.T) *backendProtoIR {
	t.Helper()
	proto, err := Compile(backendArrayOpsProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 2 {
		t.Fatalf("array-ops proof Proto count = %d, want 2", len(image.prototypes))
	}
	ir, err := buildBackendProtoIR(&image.prototypes[1])
	if err != nil {
		t.Fatal(err)
	}
	return ir
}

func backendClosureProofIRs(t *testing.T) []*backendProtoIR {
	t.Helper()
	proto, err := Compile(backendClosureProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 4 {
		t.Fatalf("closure proof Proto count = %d, want 4", len(image.prototypes))
	}
	irs := make([]*backendProtoIR, len(image.prototypes))
	for protoID := range image.prototypes {
		irs[protoID], err = buildBackendProtoIR(&image.prototypes[protoID])
		if err != nil {
			t.Fatal(err)
		}
	}
	return irs
}

func backendClosureProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	targets[2] = backendGoNumericTarget{
		ir:           irs[2],
		functionName: "backendGeneratedCounterFactory",
	}
	targets[3] = backendGoNumericTarget{
		ir:           irs[3],
		functionName: "backendGeneratedCounterBody",
	}
	return targets
}

func backendRecursiveProofIRs(t *testing.T) []*backendProtoIR {
	t.Helper()
	proto, err := Compile(backendRecursiveProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 3 {
		t.Fatalf("recursive proof Proto count = %d, want 3", len(image.prototypes))
	}
	irs := make([]*backendProtoIR, len(image.prototypes))
	for protoID := range image.prototypes {
		irs[protoID], err = buildBackendProtoIR(&image.prototypes[protoID])
		if err != nil {
			t.Fatal(err)
		}
	}
	return irs
}

func backendRecursiveProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	targets[2] = backendGoNumericTarget{
		ir:            irs[2],
		functionName:  "backendGeneratedRecursiveFib",
		selfRecursive: true,
	}
	return targets
}

func backendVarargProofIRs(t *testing.T) []*backendProtoIR {
	t.Helper()
	proto, err := Compile(backendVarargProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 3 {
		t.Fatalf("vararg proof Proto count = %d, want 3", len(image.prototypes))
	}
	irs := make([]*backendProtoIR, len(image.prototypes))
	for protoID := range image.prototypes {
		irs[protoID], err = buildBackendProtoIR(&image.prototypes[protoID])
		if err != nil {
			t.Fatal(err)
		}
	}
	return irs
}

func backendVarargProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	targets[2] = backendGoNumericTarget{
		ir:               irs[2],
		functionName:     "backendGeneratedVarargScore",
		fixedVarargCount: 5,
	}
	return targets
}

func backendTupleProofIRs(t *testing.T) []*backendProtoIR {
	t.Helper()
	proto, err := Compile(backendTupleProofSource)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) != 3 {
		t.Fatalf("fixed-tuple proof Proto count = %d, want 3", len(image.prototypes))
	}
	irs := make([]*backendProtoIR, len(image.prototypes))
	for protoID := range image.prototypes {
		irs[protoID], err = buildBackendProtoIR(&image.prototypes[protoID])
		if err != nil {
			t.Fatal(err)
		}
	}
	return irs
}

func backendTupleProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	targets[2] = backendGoNumericTarget{
		ir:           irs[2],
		functionName: "backendGeneratedTupleSplit",
	}
	return targets
}

func backendCoroutineProofIRs(
	t *testing.T,
	source string,
) ([]*backendProtoIR, machineStringID) {
	t.Helper()
	proto, err := Compile(source)
	if err != nil {
		t.Fatal(err)
	}
	image, err := proto.preparedCodeImage()
	if err != nil {
		t.Fatal(err)
	}
	if len(image.prototypes) < 3 {
		t.Fatalf("coroutine proof Proto count = %d, want at least 3", len(image.prototypes))
	}
	irs := make([]*backendProtoIR, len(image.prototypes))
	for protoID := range image.prototypes {
		irs[protoID], err = buildBackendProtoIR(&image.prototypes[protoID])
		if err != nil {
			t.Fatal(err)
		}
	}
	return irs, backendCoroutineDeadStringID(t, image)
}

func backendCoroutineProofTargets(irs []*backendProtoIR) []backendGoNumericTarget {
	targets := make([]backendGoNumericTarget, len(irs))
	if len(irs) > 2 {
		targets[2] = backendGoNumericTarget{
			ir:           irs[2],
			functionName: "backendGeneratedCoroutineBody",
		}
	}
	return targets
}

func backendCoroutineDeadStringID(t *testing.T, image *codeImage) machineStringID {
	t.Helper()
	if image == nil {
		return invalidMachineStringID
	}
	for index, record := range image.stringRecords {
		start := uint64(record.offset)
		end := start + uint64(record.length)
		if end > uint64(len(image.stringData)) {
			t.Fatalf("coroutine proof string %d has an invalid span", index+1)
		}
		if string(image.stringData[int(start):int(end)]) == "dead" {
			return machineStringID(index + 1)
		}
	}
	return invalidMachineStringID
}

func BenchmarkBackendGeneratedNumericFixture(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedNumericFixture(float64(iteration & 31))
		if !ok {
			b.Fatal("generated numeric fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedArrayIterationFixture(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedArrayIterationFixture(float64(iteration & 31))
		if !ok {
			b.Fatal("generated scalar array-iteration fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedFiniteStringStateFixture(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedFiniteStringStateFixture(float64(iteration & 31))
		if !ok {
			b.Fatal("generated finite-string state fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedStructuralStringKeyKernel(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedStructuralStringKeyKernel(float64(iteration & 31))
		if !ok {
			b.Fatal("generated structural string-key fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedSparseGrid(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedSparseGrid(float64(iteration & 31))
		if !ok {
			b.Fatal("generated sparse-grid fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedArrayOpsFixture(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedArrayOpsFixture(float64(iteration & 31))
		if !ok {
			b.Fatal("generated scalar array-ops fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedMethodKernel(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedMethodKernel(float64(iteration & 31))
		if !ok {
			b.Fatal("generated scalar method fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedClosureKernel(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedClosureKernel(float64(iteration & 31))
		if !ok {
			b.Fatal("generated scalar closure fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedRecursiveKernel(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedRecursiveKernel(float64(iteration & 31))
		if !ok {
			b.Fatal("generated recursive fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedVarargKernel(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedVarargKernel(float64(iteration & 31))
		if !ok {
			b.Fatal("generated fixed-vararg fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedTupleKernel(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedTupleKernel(float64(iteration & 31))
		if !ok {
			b.Fatal("generated fixed-tuple fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedCoroutineKernel(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedCoroutineKernel(float64(iteration & 31))
		if !ok {
			b.Fatal("generated scalar coroutine fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

func BenchmarkBackendGeneratedMetatableIndexKernel(b *testing.B) {
	var result float64
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		value, ok := backendGeneratedMetatableIndexFixture(float64(iteration & 31))
		if !ok {
			b.Fatal("generated scalar metatable-index fixture exited")
		}
		result = value
	}
	backendGeneratedNumericSink = result
}

var backendGeneratedNumericSink float64

func FuzzBackendGoNumericProofDeterministicAndNeverPanics(f *testing.F) {
	for _, source := range []string{
		backendNumericProofSource,
		backendNumericExitProofSource,
		backendTableFieldProofSource,
		backendMetatableIndexProofSource,
		backendArrayIterationProofSource,
		backendFiniteStringStateProofSource,
		backendArrayOpsProofSource,
		backendClosureProofSource,
		backendRecursiveProofSource,
		backendVarargProofSource,
		backendTupleProofSource,
		backendCoroutineProofSource,
		"local function add(value) return value + 1 end return add",
		"return { field = 1 }",
	} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		proto, err := Compile(source)
		if err != nil {
			return
		}
		image, err := proto.preparedCodeImage()
		if err != nil {
			return
		}
		irs := make([]*backendProtoIR, len(image.prototypes))
		for protoIndex := range image.prototypes {
			prepared := &image.prototypes[protoIndex]
			if !prepared.eligible {
				continue
			}
			ir, err := buildBackendProtoIR(prepared)
			if err != nil {
				t.Fatalf("build Proto %d: %v", protoIndex, err)
			}
			irs[protoIndex] = ir
		}
		for protoIndex, ir := range irs {
			if ir == nil {
				continue
			}
			targets := make([]backendGoNumericTarget, len(irs))
			for operationIndex := range ir.ops {
				operation := &ir.ops[operationIndex]
				if operation.op == opClosure &&
					operation.targetProto >= 0 &&
					int(operation.targetProto) < len(irs) &&
					irs[operation.targetProto] != nil &&
					backendGoNumericHasCoroutineYield(irs[operation.targetProto]) {
					targets[operation.targetProto] = backendGoNumericTarget{
						ir:           irs[operation.targetProto],
						functionName: fmt.Sprintf("Target%d", operation.targetProto),
					}
				}
				if operation.call.kind != backendCallDirectProto ||
					operation.call.targetProto < 0 ||
					int(operation.call.targetProto) >= len(irs) ||
					irs[operation.call.targetProto] == nil {
					continue
				}
				targets[operation.call.targetProto] = backendGoNumericTarget{
					ir:           irs[operation.call.targetProto],
					functionName: fmt.Sprintf("Target%d", operation.call.targetProto),
				}
			}
			options := backendGoNumericOptions{
				packageName:          "proof",
				functionName:         "Run",
				preparedFunctionName: "RunPrepared",
				directTargets:        targets,
				coroutineDeadString:  backendCoroutineDeadStringID(t, image),
			}
			first, firstErr := emitBackendGoNumericProof(ir, options)
			second, secondErr := emitBackendGoNumericProof(ir, options)
			if (firstErr == nil) != (secondErr == nil) ||
				firstErr != nil && firstErr.Error() != secondErr.Error() ||
				!bytes.Equal(first, second) {
				t.Fatalf("Proto %d generated nondeterministically: %v / %v", protoIndex, firstErr, secondErr)
			}
			if firstErr == nil {
				if _, err := goparser.ParseFile(token.NewFileSet(), "generated.go", first, goparser.AllErrors); err != nil {
					t.Fatalf("parse generated Proto %d: %v", protoIndex, err)
				}
			}
		}
	})
}
