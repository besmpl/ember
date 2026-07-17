package ember

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestBuildDirectShadowPreservesPhysicalWordcodeAndGeneratedHandlers(t *testing.T) {
	proto, err := Compile(`
local total = 0
local values = {2, 3, 5}
for _, value in values do
	total = total + value * 2
end
return total
`)
	if err != nil {
		t.Fatalf("Compile returned error: %v", err)
	}
	decoded, boundaries, err := wordcodeDecodeWords(proto.words)
	if err != nil {
		t.Fatalf("wordcodeDecodeWords returned error: %v", err)
	}
	if len(proto.words) <= len(decoded) {
		t.Fatalf("physical words = %d, logical instructions = %d, want AUX coverage", len(proto.words), len(decoded))
	}

	shadow, err := buildDirectShadow(proto.words, generatedDirectSemanticMetadata)
	if err != nil {
		t.Fatalf("buildDirectShadow returned error: %v", err)
	}
	if len(shadow.words) != len(proto.words) {
		t.Fatalf("shadow words = %d, want %d", len(shadow.words), len(proto.words))
	}
	cacheSites := 0
	for index, ins := range decoded {
		pc := boundaries[index]
		word := shadow.words[pc]
		if word.raw() != proto.words[pc] {
			t.Fatalf("instruction %d raw word = %#x, want %#x", index, word.raw(), proto.words[pc])
		}
		if word.handler() != directHandlerID(ins.ins.op) || word.counter() != 0 {
			t.Fatalf("instruction %d handler/counter = %d/%d", index, word.handler(), word.counter())
		}
		metadata, _ := directSemanticMetadataFor(ins.ins.op)
		cacheIndex, cached := word.cacheIndex()
		if cached != (metadata.cache != directCacheNone) {
			t.Fatalf("instruction %d cache presence = %t, layout = %d", index, cached, metadata.cache)
		}
		if cached {
			if cacheIndex != cacheSites {
				t.Fatalf("instruction %d cache index = %d, want %d", index, cacheIndex, cacheSites)
			}
			if shadow.caches[cacheIndex].layout() != metadata.cache {
				t.Fatalf("instruction %d cache layout = %d, want %d", index, shadow.caches[cacheIndex].layout(), metadata.cache)
			}
			cacheSites++
		}
		next := boundaries[index+1]
		for auxPC := pc + 1; auxPC < next; auxPC++ {
			if shadow.words[auxPC].raw() != proto.words[auxPC] || shadow.words[auxPC].handler() != directHandlerInvalid {
				t.Fatalf("AUX word %d was made executable", auxPC)
			}
		}
	}
	if len(shadow.caches) != cacheSites {
		t.Fatalf("shadow caches = %d, want %d", len(shadow.caches), cacheSites)
	}

	firstPC := boundaries[0]
	original := proto.words[firstPC]
	shadow.words[firstPC] = shadow.words[firstPC].withHandler(directHandlerID(opcodeLimit)).incrementCounter()
	if proto.words[firstPC] != original {
		t.Fatal("mutating owner shadow changed immutable Proto wordcode")
	}
	if shadow.words[firstPC].handler() != directHandlerID(opcodeLimit) || shadow.words[firstPC].counter() != 1 {
		t.Fatal("shadow handler/counter mutation did not round-trip")
	}
}

func TestDirectShadowEncodingIsBoundedAndSaturating(t *testing.T) {
	if size := unsafe.Sizeof(directShadowWord(0)); size != 8 {
		t.Fatalf("directShadowWord size = %d, want 8", size)
	}
	if size := unsafe.Sizeof(directAdaptiveCacheCell(0)); size != 8 {
		t.Fatalf("directAdaptiveCacheCell size = %d, want 8", size)
	}
	if size := unsafe.Sizeof(directNumericTracePlan{}); size != directNumericTracePlanBytes {
		t.Fatalf("directNumericTracePlan size = %d, declared %d", size, directNumericTracePlanBytes)
	}
	word := newDirectShadowWord(0xfedcba98, directHandlerID(opAdd), 7)
	for range 300 {
		word = word.incrementCounter()
	}
	if word.raw() != 0xfedcba98 || word.handler() != directHandlerID(opAdd) || word.counter() != 255 {
		t.Fatalf("saturated word = raw:%#x handler:%d counter:%d", word.raw(), word.handler(), word.counter())
	}
	if cacheIndex, ok := word.cacheIndex(); !ok || cacheIndex != 7 {
		t.Fatalf("cache index = %d/%t, want 7/true", cacheIndex, ok)
	}
	if bytes := directShadowStateBytes(100, 100); bytes != 1600 {
		t.Fatalf("shadow state bytes = %d, want 1600", bytes)
	}
	if directShadowStateBytes(100, 100) > directShadowStateLimit(100) {
		t.Fatal("maximally dense shadow exceeds the hard owner-Program budget")
	}
}

func TestDirectAdaptiveCacheCellEncodesBoundedGuardRegisters(t *testing.T) {
	cell, ok := newDirectAdaptiveCacheCell(directCacheType).withGuardRegisters([]uint8{1, 3, 5, 7, 9, 11})
	if !ok {
		t.Fatal("six guard registers did not fit")
	}
	if cell.layout() != directCacheType || cell.guardCount() != 6 {
		t.Fatalf("cell layout/count = %d/%d, want %d/6", cell.layout(), cell.guardCount(), directCacheType)
	}
	for index, want := range []uint8{1, 3, 5, 7, 9, 11} {
		if got := cell.guardRegister(index); got != want {
			t.Fatalf("guard %d = %d, want %d", index, got, want)
		}
	}
	if _, ok := cell.withGuardRegisters([]uint8{0, 1, 2, 3, 4, 5, 6}); ok {
		t.Fatal("seven guard registers exceeded the cache-cell bound")
	}
}

func TestShadowTilesBoundedNumericForTrace(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 200 do
	local mixed = i * 3 - i // 2
	total = total + mixed % 5
end
return total
`)
	if err != nil {
		t.Fatal(err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	instance, err := thread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := wordcodeDecodeWords(proto.words)
	if err != nil {
		t.Fatal(err)
	}
	checkPC := -1
	loopPC := -1
	for _, entry := range decoded {
		switch entry.ins.op {
		case opNumericForCheck:
			checkPC = entry.wordPC
		case opNumericForLoop:
			loopPC = entry.wordPC
		}
	}
	if checkPC < 0 || loopPC < 0 {
		t.Fatal("compiled source did not contain a numeric loop")
	}
	if len(instance.shadow.numericTraces) != 1 {
		t.Fatalf("numeric trace plans = %d, want one", len(instance.shadow.numericTraces))
	}
	plan := instance.shadow.numericTraces[0]
	if int(plan.checkPC) != checkPC || int(plan.loopPC) != loopPC || plan.operationCount == 0 {
		t.Fatalf("numeric trace plan = %#v, want check=%d loop=%d and operations", plan, checkPC, loopPC)
	}
	folded := false
	for index := uint8(0); index < plan.operationCount; index++ {
		if plan.operations[index].guestCharge == 2 {
			folded = true
		}
	}
	if !folded {
		t.Fatal("numeric trace plan did not form a general Move+numeric superword")
	}
	word := instance.shadow.words[checkPC]
	if word.handler() != directHandlerNumericForTrace {
		t.Fatalf("numeric check handler = %d, want fused %d", word.handler(), directHandlerNumericForTrace)
	}
	cacheIndex, ok := word.cacheIndex()
	if !ok {
		t.Fatal("fused numeric trace has no guard cache")
	}
	cache := instance.shadow.caches[cacheIndex]
	if cache.guardCount() == 0 || cache.guardCount() > 6 {
		t.Fatalf("guard count = %d, want 1..6", cache.guardCount())
	}
	for _, entry := range decoded {
		if entry.wordPC <= checkPC || entry.wordPC > loopPC {
			continue
		}
		if got := instance.shadow.words[entry.wordPC].handler(); got != directHandlerID(entry.ins.op) {
			t.Fatalf("interior %s handler = %d, want generic %d", opcodeName(entry.ins.op), got, entry.ins.op)
		}
	}
}

func TestShadowDoesNotTileNumericLoopAcrossObservableEffects(t *testing.T) {
	proto, err := Compile(`
local total = 0
local values = {}
for i = 1, 20 do
	values[i] = i
	total = total + i
end
return total
`)
	if err != nil {
		t.Fatal(err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	instance, err := thread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := wordcodeDecodeWords(proto.words)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range decoded {
		if entry.ins.op == opNumericForCheck && instance.shadow.words[entry.wordPC].handler() == directHandlerNumericForTrace {
			t.Fatal("numeric loop containing a table write was fused")
		}
	}
}

func TestProductionExecutesNumericTraceAndMatchesInstrumentedOracle(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 200 do
	local mixed = i * 3 - i // 2
	total = total + mixed % 5
end
return total
`)
	if err != nil {
		t.Fatal(err)
	}
	production := newVMThread(runtimeGlobals(nil))
	got, err := production.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("production run returned error: %v", err)
	}
	instrumented := newVMThread(runtimeGlobals(nil))
	instrumented.directFrameInstrumented = true
	want, err := instrumented.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("instrumented run returned error: %v", err)
	}
	if !valuesEqualList(got, want) {
		t.Fatalf("production result = %#v, instrumented = %#v", got, want)
	}
	instance := production.functionInstances[proto]
	fusedExecutions := uint8(0)
	for _, word := range instance.shadow.words {
		if word.handler() == directHandlerNumericForTrace {
			fusedExecutions += word.counter()
		}
	}
	if fusedExecutions == 0 {
		t.Fatal("production run did not execute a fused numeric trace")
	}
}

func TestNumericTraceGuardMissDequickensBeforeMutation(t *testing.T) {
	proto, err := Compile(`
local total = seed
for i = 1, 2 do
	total = total + i
end
return total
`)
	if err != nil {
		t.Fatal(err)
	}
	globals := map[string]Value{"seed": StringValue("not-a-number")}
	production := newVMThread(runtimeGlobals(globals))
	_, gotErr := production.run(proto, nil, nil)
	instrumented := newVMThread(runtimeGlobals(globals))
	instrumented.directFrameInstrumented = true
	_, wantErr := instrumented.run(proto, nil, nil)
	if difference := errorsEquivalent(wantErr, gotErr); difference != "" {
		t.Fatalf("guard fallback error differs: %s\nproduction=%s\ninstrumented=%s", difference, errorDiagnostic(gotErr), errorDiagnostic(wantErr))
	}
	instance := production.functionInstances[proto]
	for pc, word := range instance.shadow.words {
		if word.handler() == directHandlerNumericForTrace {
			t.Fatalf("guard miss left word %d quickened", pc)
		}
	}
}

func TestNumericTraceNaNControllerFallsBackToCanonicalError(t *testing.T) {
	proto, err := Compile(`local total = 0 for i = seed, 2 do total = total + i end return total`)
	if err != nil {
		t.Fatal(err)
	}
	globals := map[string]Value{"seed": NumberValue(math.NaN())}
	production := newVMThread(runtimeGlobals(globals))
	_, gotErr := production.run(proto, nil, nil)
	instrumented := newVMThread(runtimeGlobals(globals))
	instrumented.directFrameInstrumented = true
	_, wantErr := instrumented.run(proto, nil, nil)
	if difference := errorsEquivalent(wantErr, gotErr); difference != "" {
		t.Fatalf("NaN fallback error differs: %s\nproduction=%s\ninstrumented=%s", difference, errorDiagnostic(gotErr), errorDiagnostic(wantErr))
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "numeric for operand is NaN") {
		t.Fatalf("NaN controller error = %v", gotErr)
	}
	instance := production.functionInstances[proto]
	for pc, word := range instance.shadow.words {
		if word.handler() == directHandlerNumericForTrace {
			t.Fatalf("NaN guard miss left word %d quickened", pc)
		}
	}
}

func TestNumericTraceMatchesEveryInstrumentedInstructionBoundary(t *testing.T) {
	proto, err := Compile(`
local total = 0
for i = 1, 6 do
	local mixed = i * 3 - i // 2
	total = total + mixed % 5
end
return total
`)
	if err != nil {
		t.Fatal(err)
	}
	for budget := uint64(1); budget <= 80; budget++ {
		test := executionDifferentialCase{
			name:   "numeric trace instruction boundary",
			limits: ExecutionLimits{MaxInstructions: budget},
		}
		direct := runDifferentialCase(proto, test, false)
		instrumented := runDifferentialCase(proto, test, true)
		if difference := differentialDifference(instrumented, direct); difference != "" {
			t.Fatalf("budget %d: %s\ndirect=%s\ninstrumented=%s", budget, difference, errorDiagnostic(direct.err), errorDiagnostic(instrumented.err))
		}
	}
}

type numericTracePollContext struct {
	polls int
}

func (*numericTracePollContext) Deadline() (deadline time.Time, ok bool) { return time.Time{}, false }
func (*numericTracePollContext) Done() <-chan struct{}                   { return nil }
func (*numericTracePollContext) Value(any) any                           { return nil }

func (ctx *numericTracePollContext) Err() error {
	ctx.polls++
	if ctx.polls >= 2 {
		return context.Canceled
	}
	return nil
}

func TestNumericTracePollsCancellationInsideFusedLoop(t *testing.T) {
	proto, err := Compile(`local total = 0 for i = 1, 10000 do total = total + i end return total`)
	if err != nil {
		t.Fatal(err)
	}
	for _, instrumented := range []bool{false, true} {
		ctx := &numericTracePollContext{}
		controller, err := newExecutionController(ctx, ExecutionLimits{})
		if err != nil {
			t.Fatal(err)
		}
		thread := newVMThreadWithContext(ctx, runtimeGlobals(nil))
		thread.controller = controller
		thread.directFrameInstrumented = instrumented
		values, runErr := thread.run(proto, nil, nil)
		if values != nil || !errors.Is(runErr, context.Canceled) {
			t.Fatalf("instrumented=%t result=(%#v, %v), want cancellation", instrumented, values, runErr)
		}
		if ctx.polls != 2 {
			t.Fatalf("instrumented=%t context polls = %d, want 2", instrumented, ctx.polls)
		}
	}
}

func TestBuildDirectShadowFailsClosed(t *testing.T) {
	tests := []struct {
		name  string
		words []wordcodeWord
		table [opcodeLimit]directSemanticMetadata
		want  string
	}{
		{
			name:  "unknown opcode",
			words: []wordcodeWord{wordcodeOpcodeMask},
			table: generatedDirectSemanticMetadata,
			want:  "unknown opcode",
		},
		{
			name:  "truncated AUX",
			words: []wordcodeWord{wordcodeWord(opLoadGlobal) | wordcodeAuxBit},
			table: generatedDirectSemanticMetadata,
			want:  "truncated AUX",
		},
		{
			name:  "missing semantic metadata",
			words: []wordcodeWord{wordcodeWord(opMove)},
			table: func() [opcodeLimit]directSemanticMetadata {
				table := generatedDirectSemanticMetadata
				table[opMove] = directSemanticMetadata{}
				return table
			}(),
			want: "unclassified",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := buildDirectShadow(test.words, test.table)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("buildDirectShadow error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestVMFunctionInstancesOwnIndependentLazyShadows(t *testing.T) {
	proto, err := Compile(`local x = 1 return x + 2`)
	if err != nil {
		t.Fatal(err)
	}
	firstThread := newVMThread(runtimeGlobals(nil))
	first, err := firstThread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatalf("first shadowFunctionInstance returned error: %v", err)
	}
	again, err := firstThread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatalf("second shadowFunctionInstance returned error: %v", err)
	}
	if first != again || len(first.shadow.words) != len(proto.words) {
		t.Fatal("one thread did not reuse its lazily built shadow")
	}

	secondThread := newVMThread(runtimeGlobals(nil))
	second, err := secondThread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatalf("independent shadowFunctionInstance returned error: %v", err)
	}
	if first == second || len(second.shadow.words) == 0 || &first.shadow.words[0] == &second.shadow.words[0] {
		t.Fatal("independent threads shared mutable shadow state")
	}
	original := proto.words[0]
	first.shadow.words[0] = first.shadow.words[0].incrementCounter()
	if second.shadow.words[0].counter() != 0 || proto.words[0] != original {
		t.Fatal("owner-local shadow mutation escaped its thread")
	}
}

func TestUnownedPoolResetDropsDirectShadows(t *testing.T) {
	proto, err := Compile(`return 1 + 2`)
	if err != nil {
		t.Fatal(err)
	}
	thread := newVMThread(runtimeGlobals(nil))
	instance, err := thread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatal(err)
	}
	if len(instance.shadow.words) == 0 {
		t.Fatal("shadow was not built")
	}
	thread.resetForPool()
	if retained := thread.functionInstances[proto]; retained != nil && len(retained.shadow.words) != 0 {
		t.Fatal("unowned pooled thread retained shadow wordcode")
	}
}

func TestOwnerDetachReusesShadowAndCollectionClearsIt(t *testing.T) {
	proto, err := Compile(`local x = 1 return x + 2`)
	if err != nil {
		t.Fatal(err)
	}
	owner := newRuntimeOwner()
	firstThread := newVMThread(runtimeGlobals(nil))
	if err := firstThread.bindOwner(owner); err != nil {
		t.Fatal(err)
	}
	first, err := firstThread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatal(err)
	}
	first.shadow.words[0] = first.shadow.words[0].incrementCounter()
	firstThread.unbindOwner()

	secondThread := newVMThread(runtimeGlobals(nil))
	if err := secondThread.bindOwner(owner); err != nil {
		t.Fatal(err)
	}
	second, err := secondThread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatal(err)
	}
	if second.shadow.words[0].counter() != 1 {
		t.Fatal("owner detach discarded reusable shadow feedback")
	}
	secondThread.unbindOwner()

	if _, err := owner.collect(nil); err != nil {
		t.Fatalf("owner.collect returned error: %v", err)
	}
	thirdThread := newVMThread(runtimeGlobals(nil))
	if err := thirdThread.bindOwner(owner); err != nil {
		t.Fatal(err)
	}
	third, err := thirdThread.shadowFunctionInstance(proto)
	if err != nil {
		t.Fatal(err)
	}
	if third.shadow.words[0].counter() != 0 {
		t.Fatal("collection retained stale shadow feedback")
	}
	thirdThread.unbindOwner()
	if err := owner.close(); err != nil {
		t.Fatalf("owner.close returned error: %v", err)
	}
	if owner.idleVMCaches != nil {
		t.Fatal("owner close retained shadow cache bundles")
	}
}

func TestProductionDirectFrameRunsGeneratedGenericShadow(t *testing.T) {
	proto, err := Compile(`
local function twice(value)
	return value * 2
end
local values = {amount = 4}
return twice(values.amount) + 1
`)
	if err != nil {
		t.Fatal(err)
	}
	production := newVMThread(runtimeGlobals(nil))
	got, err := production.run(proto, nil, nil)
	if err != nil {
		t.Fatalf("production run returned error: %v", err)
	}
	want, _, err := runWithDirectFrameMechanismCounters(proto, nil)
	if err != nil {
		t.Fatalf("instrumented oracle returned error: %v", err)
	}
	if !valuesEqualList(got, want) {
		t.Fatalf("production results = %#v, instrumented = %#v", got, want)
	}
	instance := production.functionInstances[proto]
	if instance == nil || len(instance.shadow.words) != len(proto.words) {
		t.Fatal("production direct frame did not retain its owner-local generic shadow")
	}
	for pc, word := range instance.shadow.words {
		if word.handler() == directHandlerInvalid {
			continue
		}
		op := opcode(uint8(proto.words[pc]) & uint8(wordcodeOpcodeMask))
		if word.handler() != directHandlerID(op) {
			t.Fatalf("word %d handler = %d, want generic %d", pc, word.handler(), op)
		}
	}
}

func valuesEqualList(left []Value, right []Value) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !valuesEqual(left[index], right[index]) {
			return false
		}
	}
	return true
}
