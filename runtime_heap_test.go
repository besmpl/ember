package ember

import (
	"math"
	"runtime"
	"testing"
)

func TestRuntimeHeapValueAdapterScalars(t *testing.T) {
	var heap runtimeHeap
	wantNumbers := []uint64{
		0x0000_0000_0000_0000,
		0x8000_0000_0000_0000,
		0x7ff8_0000_0000_0042,
	}
	values := []Value{NilValue(), BoolValue(false), BoolValue(true)}
	for _, bits := range wantNumbers {
		values = append(values, NumberValue(math.Float64frombits(bits)))
	}
	values = append(values, nativeFuncValueWithID(nil, nativeFuncMathMin))

	for _, want := range values {
		encoded, err := heap.importValue(want)
		if err != nil {
			t.Fatalf("import %s: %v", want.Kind(), err)
		}
		got, err := heap.exportValue(encoded)
		if err != nil {
			t.Fatalf("export %s: %v", want.Kind(), err)
		}
		switch want.Kind() {
		case NumberKind:
			wantBits := valueFloat64Bits(valueNumber(want))
			gotBits := valueFloat64Bits(valueNumber(got))
			if gotBits != wantBits {
				t.Fatalf("number bits = %#x, want %#x", gotBits, wantBits)
			}
		case HostFuncKind:
			if valueNativeID(got) != nativeFuncMathMin {
				t.Fatalf("native id = %d, want %d", valueNativeID(got), nativeFuncMathMin)
			}
		default:
			if !valuesEqual(got, want) {
				t.Fatalf("round trip %s changed value", want.Kind())
			}
		}
	}
}

func TestRuntimeHeapResetForReuseClearsOwnersAndInvalidatesHandles(t *testing.T) {
	var heap runtimeHeap
	stringHandle, err := heap.importValue(stringValueFromBox(newStringBox("reset")))
	if err != nil {
		t.Fatalf("import string: %v", err)
	}
	tableHandle, err := heap.importValue(TableValue(NewTable()))
	if err != nil {
		t.Fatalf("import table: %v", err)
	}
	closureHandle, err := heap.importValue(closureFunctionValue(&closure{}))
	if err != nil {
		t.Fatalf("import closure: %v", err)
	}
	cellHandle, err := heap.importCell(&cell{})
	if err != nil {
		t.Fatalf("import cell: %v", err)
	}
	userdataHandle, err := heap.importValue(UserDataValue(NewUserData("reset")))
	if err != nil {
		t.Fatalf("import userdata: %v", err)
	}
	hostHandle, err := heap.importValue(HostFuncValue(func([]Value) ([]Value, error) { return nil, nil }))
	if err != nil {
		t.Fatalf("import host callable: %v", err)
	}
	boxedHandle, err := slotFromNumberBits(0x7ff8_0000_0000_0042, &heap)
	if err != nil {
		t.Fatalf("import boxed number: %v", err)
	}

	stringEntriesCap := cap(heap.strings.entries)
	boxedEntriesCap := cap(heap.boxedNumbers.entries)
	_, oldStringIndex, oldStringGeneration, err := slotUnpackHandle(stringHandle)
	if err != nil {
		t.Fatalf("unpack string handle: %v", err)
	}

	heap.resetForReuse()
	if got := len(heap.strings.byValue); got != 0 {
		t.Fatalf("string import map length after reset = %d, want 0", got)
	}
	if got := len(heap.tables.byValue); got != 0 {
		t.Fatalf("table import map length after reset = %d, want 0", got)
	}
	if heap.strings.byValue == nil || heap.tables.byValue == nil || cap(heap.strings.entries) != stringEntriesCap || cap(heap.boxedNumbers.entries) != boxedEntriesCap {
		t.Fatal("reset discarded reusable slab or map backing")
	}
	for name, handle := range map[string]slot{
		"string": stringHandle, "table": tableHandle, "closure": closureHandle,
		"cell": cellHandle, "userdata": userdataHandle, "host": hostHandle, "boxed": boxedHandle,
	} {
		if err := heap.validateSlot(handle); err == nil {
			t.Fatalf("stale %s handle validated after reset", name)
		}
	}
	if entry := heap.strings.entries[oldStringIndex]; entry.value != nil || entry.live || entry.pinned {
		t.Fatalf("string entry after reset = %#v, want empty", entry)
	}
	if entry := heap.userdata.entries[1]; entry.value != nil || entry.live || entry.pinned {
		t.Fatalf("userdata entry after reset = %#v, want empty and unpinned", entry)
	}
	if entry := heap.hostCallables.entries[1]; entry.value != nil || entry.live || entry.pinned {
		t.Fatalf("host entry after reset = %#v, want empty and unpinned", entry)
	}
	if entry := heap.boxedNumbers.entries[1]; entry.bits != 0 || entry.live {
		t.Fatalf("boxed number entry after reset = %#v, want empty", entry)
	}
	if got := heap.strings.entries[oldStringIndex].generation; got == oldStringGeneration {
		t.Fatalf("string generation after reset = %d, want advancement from %d", got, oldStringGeneration)
	}

	replacement, err := heap.importValue(stringValueFromBox(newStringBox("replacement")))
	if err != nil {
		t.Fatalf("import replacement string: %v", err)
	}
	_, replacementIndex, replacementGeneration, err := slotUnpackHandle(replacement)
	if err != nil {
		t.Fatalf("unpack replacement handle: %v", err)
	}
	if replacementIndex != oldStringIndex || replacementGeneration == oldStringGeneration {
		t.Fatalf("replacement handle = (index %d, generation %d), want index %d and new generation", replacementIndex, replacementGeneration, oldStringIndex)
	}
}

func TestRuntimeHeapResetForReuseRejectsGenerationExhaustionAtomically(t *testing.T) {
	box := newStringBox("exhausted")
	table := NewTable()
	heap := runtimeHeap{
		strings: slotSlab[*stringBox]{
			entries: []slotSlabEntry[*stringBox]{
				{},
				{value: box, generation: slotMaxGeneration, live: true},
			},
			byValue: map[*stringBox]uint32{box: 1},
		},
		tables: slotSlab[*Table]{
			entries: []slotSlabEntry[*Table]{
				{},
				{value: table, generation: 7, live: true},
			},
			byValue: map[*Table]uint32{table: 1},
		},
	}

	if heap.resetForReuse() {
		t.Fatal("generation-exhausted heap reported reusable")
	}
	if entry := heap.strings.entries[1]; entry.value != box || !entry.live || entry.generation != slotMaxGeneration {
		t.Fatalf("exhausted string entry mutated on rejected reset: %#v", entry)
	}
	if entry := heap.tables.entries[1]; entry.value != table || !entry.live || entry.generation != 7 {
		t.Fatalf("unrelated table entry mutated on rejected reset: %#v", entry)
	}
	if heap.strings.byValue[box] != 1 || heap.tables.byValue[table] != 1 {
		t.Fatal("rejected reset mutated import indexes")
	}
}

func TestRuntimeHeapStringSlabIdentityLifetimeAndGC(t *testing.T) {
	var heap runtimeHeap
	firstBox := newStringBox("same")
	firstValue := stringValueFromBox(firstBox)
	first, err := heap.importValue(firstValue)
	if err != nil {
		t.Fatalf("import first string: %v", err)
	}
	same, err := heap.importValue(firstValue)
	if err != nil {
		t.Fatalf("reimport first string: %v", err)
	}
	if same != first {
		t.Fatalf("same string box handles differ: %#x vs %#x", first, same)
	}

	secondBox := newStringBox("same")
	second, err := heap.importValue(stringValueFromBox(secondBox))
	if err != nil {
		t.Fatalf("import equal string box: %v", err)
	}
	if second == first {
		t.Fatal("equal but distinct string boxes share a handle")
	}

	for _, test := range []struct {
		name string
		slot slot
		want *stringBox
	}{
		{name: "first", slot: first, want: firstBox},
		{name: "second", slot: second, want: secondBox},
	} {
		got, err := heap.exportValue(test.slot)
		if err != nil {
			t.Fatalf("export %s string: %v", test.name, err)
		}
		if got.stringBox() != test.want {
			t.Fatalf("export %s box = %p, want %p", test.name, got.stringBox(), test.want)
		}
	}

	typedNil, err := heap.importValue(stringValueFromBox(nil))
	if err != nil {
		t.Fatalf("import typed nil string: %v", err)
	}
	if slotTagOf(typedNil) != slotTagString {
		t.Fatalf("typed nil string tag = %v, want string", slotTagOf(typedNil))
	}
	gotNil, err := heap.exportValue(typedNil)
	if err != nil {
		t.Fatalf("export typed nil string: %v", err)
	}
	if gotNil.Kind() != StringKind || gotNil.stringBox() != nil {
		t.Fatalf("typed nil string export = (%s, %p)", gotNil.Kind(), gotNil.stringBox())
	}

	runtime.GC()
	gotAfterGC, err := heap.exportValue(first)
	if err != nil {
		t.Fatalf("export string after GC: %v", err)
	}
	if gotAfterGC.stringBox() != firstBox {
		t.Fatalf("GC changed string box identity: got %p, want %p", gotAfterGC.stringBox(), firstBox)
	}

	_, firstIndex, firstGeneration, err := slotUnpackHandle(first)
	if err != nil {
		t.Fatalf("unpack first string handle: %v", err)
	}
	if err := heap.releaseHandle(first); err != nil {
		t.Fatalf("release first string handle: %v", err)
	}
	if _, err := heap.exportValue(first); err == nil {
		t.Fatal("released string handle still exported")
	}
	replacement, err := heap.importValue(stringValueFromBox(newStringBox("replacement")))
	if err != nil {
		t.Fatalf("import replacement string: %v", err)
	}
	_, replacementIndex, replacementGeneration, err := slotUnpackHandle(replacement)
	if err != nil {
		t.Fatalf("unpack replacement string handle: %v", err)
	}
	if replacementIndex != firstIndex || replacementGeneration == firstGeneration {
		t.Fatalf("replacement handle = (index %d, generation %d), want index %d and a new generation", replacementIndex, replacementGeneration, firstIndex)
	}
}

func TestRuntimeHeapTypedReferenceSlabsAndIdentity(t *testing.T) {
	var heap runtimeHeap
	table := NewTable()
	closureValue := closureFunctionValue(&closure{})
	userdata := NewUserData("payload")
	hostValue := HostFuncValue(func([]Value) ([]Value, error) { return nil, nil })
	cellValue := &cell{}

	type referenceCase struct {
		name  string
		want  Value
		check func(t *testing.T, got Value)
	}
	cases := []referenceCase{
		{name: "table", want: TableValue(table), check: func(t *testing.T, got Value) {
			value, ok := got.Table()
			if !ok || value != table {
				t.Fatalf("table identity = (%p, %v), want (%p, true)", value, ok, table)
			}
		}},
		{name: "closure", want: closureValue, check: func(t *testing.T, got Value) {
			value, ok := got.scriptFunction()
			want, _ := closureValue.scriptFunction()
			if !ok || value != want {
				t.Fatalf("closure identity = (%p, %v), want (%p, true)", value, ok, want)
			}
		}},
		{name: "userdata", want: UserDataValue(userdata), check: func(t *testing.T, got Value) {
			value, ok := got.UserData()
			if !ok || value != userdata {
				t.Fatalf("userdata identity = (%p, %v), want (%p, true)", value, ok, userdata)
			}
		}},
		{name: "host callable", want: hostValue, check: func(t *testing.T, got Value) {
			value := got.hostCallableRef()
			want := hostValue.hostCallableRef()
			if value == nil || value != want {
				t.Fatalf("host callable identity = (%p), want (%p)", value, want)
			}
		}},
	}
	for _, test := range cases {
		first, err := heap.importValue(test.want)
		if err != nil {
			t.Fatalf("import %s: %v", test.name, err)
		}
		same, err := heap.importValue(test.want)
		if err != nil {
			t.Fatalf("reimport %s: %v", test.name, err)
		}
		if same != first {
			t.Fatalf("same %s pointer produced handles %#x and %#x", test.name, first, same)
		}
		got, err := heap.exportValue(first)
		if err != nil {
			t.Fatalf("export %s: %v", test.name, err)
		}
		test.check(t, got)
	}

	cellSlot, err := heap.importCell(cellValue)
	if err != nil {
		t.Fatalf("import upvalue cell: %v", err)
	}
	cellAgain, err := heap.importCell(cellValue)
	if err != nil {
		t.Fatalf("reimport upvalue cell: %v", err)
	}
	if cellAgain != cellSlot || slotTagOf(cellSlot) != slotTagUpvalue {
		t.Fatalf("upvalue handles = %#x and %#x, tag %v", cellSlot, cellAgain, slotTagOf(cellSlot))
	}
	gotCell, err := heap.exportCell(cellSlot)
	if err != nil {
		t.Fatalf("export upvalue cell: %v", err)
	}
	if gotCell != cellValue {
		t.Fatalf("upvalue identity = %p, want %p", gotCell, cellValue)
	}

	typedNils := []Value{
		TableValue(nil),
		closureFunctionValue(nil),
		UserDataValue(nil),
		valueWithRef(HostFuncKind, nil),
	}
	for _, want := range typedNils {
		encoded, err := heap.importValue(want)
		if err != nil {
			t.Fatalf("import typed nil %s: %v", want.Kind(), err)
		}
		got, err := heap.exportValue(encoded)
		if err != nil {
			t.Fatalf("export typed nil %s: %v", want.Kind(), err)
		}
		if got.Kind() != want.Kind() || valueRef(got) != nil {
			t.Fatalf("typed nil %s export = kind %s, ref %p", want.Kind(), got.Kind(), valueRef(got))
		}
	}
	upvalueNil, err := heap.importCell(nil)
	if err != nil {
		t.Fatalf("import typed nil upvalue: %v", err)
	}
	if slotTagOf(upvalueNil) != slotTagUpvalue {
		t.Fatalf("typed nil upvalue tag = %v, want upvalue", slotTagOf(upvalueNil))
	}
	gotNilCell, err := heap.exportCell(upvalueNil)
	if err != nil {
		t.Fatalf("export typed nil upvalue: %v", err)
	}
	if gotNilCell != nil {
		t.Fatalf("typed nil upvalue export = %p, want nil", gotNilCell)
	}

	_, userdataIndex, _, err := slotUnpackHandle(mustImportValue(&heap, UserDataValue(userdata)))
	if err != nil {
		t.Fatalf("unpack userdata handle: %v", err)
	}
	if !heap.userdata.entries[userdataIndex].pinned {
		t.Fatal("userdata slab entry is not pinned by default")
	}
	_, hostIndex, _, err := slotUnpackHandle(mustImportValue(&heap, hostValue))
	if err != nil {
		t.Fatalf("unpack host callable handle: %v", err)
	}
	if !heap.hostCallables.entries[hostIndex].pinned {
		t.Fatal("host callable slab entry is not pinned by default")
	}
}

func mustImportValue(heap *runtimeHeap, value Value) slot {
	encoded, err := heap.importValue(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func TestRuntimeHeapReleaseRejectsStaleAndCrossKindHandles(t *testing.T) {
	table := NewTable()
	closureValue := closureFunctionValue(&closure{})
	cellValue := &cell{}
	userdata := NewUserData("payload")
	hostValue := HostFuncValue(func([]Value) ([]Value, error) { return nil, nil })
	type handleCase struct {
		name        string
		kind        slotTag
		importValue func(*runtimeHeap) (slot, error)
	}
	cases := []handleCase{
		{name: "boxed number", kind: slotTagBoxedNumber, importValue: func(heap *runtimeHeap) (slot, error) {
			return slotFromNumberBits(0x7ff8_0000_0000_0101, heap)
		}},
		{name: "string", kind: slotTagString, importValue: func(heap *runtimeHeap) (slot, error) {
			return heap.importValue(stringValueFromBox(newStringBox("release")))
		}},
		{name: "table", kind: slotTagTable, importValue: func(heap *runtimeHeap) (slot, error) {
			return heap.importValue(TableValue(table))
		}},
		{name: "closure", kind: slotTagClosure, importValue: func(heap *runtimeHeap) (slot, error) {
			return heap.importValue(closureValue)
		}},
		{name: "upvalue", kind: slotTagUpvalue, importValue: func(heap *runtimeHeap) (slot, error) {
			return heap.importCell(cellValue)
		}},
		{name: "userdata", kind: slotTagUserdata, importValue: func(heap *runtimeHeap) (slot, error) {
			return heap.importValue(UserDataValue(userdata))
		}},
		{name: "host callable", kind: slotTagHostCallable, importValue: func(heap *runtimeHeap) (slot, error) {
			return heap.importValue(hostValue)
		}},
	}

	var heap runtimeHeap
	for _, test := range cases {
		first, err := test.importValue(&heap)
		if err != nil {
			t.Fatalf("import %s: %v", test.name, err)
		}
		index, generation, err := heap.validateHandle(first, test.kind)
		if err != nil {
			t.Fatalf("validate live %s handle: %v", test.name, err)
		}
		if test.kind == slotTagUserdata || test.kind == slotTagHostCallable {
			if err := heap.unpinHandle(first); err != nil {
				t.Fatalf("unpin %s handle: %v", test.name, err)
			}
		}
		if err := heap.releaseHandle(first); err != nil {
			t.Fatalf("release %s handle: %v", test.name, err)
		}
		if err := heap.releaseHandle(first); err == nil {
			t.Fatalf("release stale %s handle succeeded", test.name)
		}
		if _, _, err := heap.validateHandle(first, test.kind); err == nil {
			t.Fatalf("validate stale %s handle succeeded", test.name)
		}
		replacement, err := test.importValue(&heap)
		if err != nil {
			t.Fatalf("reimport %s handle: %v", test.name, err)
		}
		replacementIndex, replacementGeneration, err := heap.validateHandle(replacement, test.kind)
		if err != nil {
			t.Fatalf("validate replacement %s handle: %v", test.name, err)
		}
		if replacementIndex != index || replacementGeneration == generation {
			t.Fatalf("replacement %s handle = (index %d, generation %d), want index %d and a new generation", test.name, replacementIndex, replacementGeneration, index)
		}
	}

	tableHandle, err := heap.importValue(TableValue(table))
	if err != nil {
		t.Fatalf("import cross-kind table: %v", err)
	}
	if _, _, err := slotValidateHandle(tableHandle, slotTagString); err == nil {
		t.Fatal("cross-kind table handle validated as string")
	}
	stringHandle, err := heap.importValue(stringValueFromBox(newStringBox("cross-kind")))
	if err != nil {
		t.Fatalf("import cross-kind string: %v", err)
	}
	if _, _, err := slotValidateHandle(stringHandle, slotTagTable); err == nil {
		t.Fatal("cross-kind string handle validated as table")
	}
}

func TestRuntimeHeapPinnedOpaqueReleaseRequiresUnpin(t *testing.T) {
	var heap runtimeHeap
	userdata := NewUserData("pinned")
	host := HostFuncValue(func([]Value) ([]Value, error) { return nil, nil })

	userdataHandle, err := slotImportRef(&heap.userdata, slotTagUserdata, userdata, false)
	if err != nil {
		t.Fatalf("import initially unpinned userdata: %v", err)
	}
	promotedUserdata, err := heap.importValue(UserDataValue(userdata))
	if err != nil {
		t.Fatalf("reimport userdata with pin: %v", err)
	}
	if promotedUserdata != userdataHandle {
		t.Fatal("pinned userdata reimport changed handle")
	}
	_, userdataIndex, _, err := slotUnpackHandle(userdataHandle)
	if err != nil {
		t.Fatalf("unpack userdata handle: %v", err)
	}
	if !heap.userdata.entries[userdataIndex].pinned {
		t.Fatal("existing userdata entry was not promoted to pinned")
	}

	hostRef := host.hostCallableRef()
	hostHandle, err := slotImportRef(&heap.hostCallables, slotTagHostCallable, hostRef, false)
	if err != nil {
		t.Fatalf("import initially unpinned host callable: %v", err)
	}
	promotedHost, err := heap.importValue(host)
	if err != nil {
		t.Fatalf("reimport host callable with pin: %v", err)
	}
	if promotedHost != hostHandle {
		t.Fatal("pinned host callable reimport changed handle")
	}
	_, hostIndex, _, err := slotUnpackHandle(hostHandle)
	if err != nil {
		t.Fatalf("unpack host callable handle: %v", err)
	}
	if !heap.hostCallables.entries[hostIndex].pinned {
		t.Fatal("existing host callable entry was not promoted to pinned")
	}

	for _, test := range []struct {
		name   string
		handle slot
	}{
		{name: "userdata", handle: userdataHandle},
		{name: "host callable", handle: hostHandle},
	} {
		if err := heap.releaseHandle(test.handle); err == nil {
			t.Fatalf("release pinned %s succeeded", test.name)
		}
		if err := heap.unpinHandle(test.handle); err != nil {
			t.Fatalf("unpin %s: %v", test.name, err)
		}
		if err := heap.releaseHandle(test.handle); err != nil {
			t.Fatalf("release unpinned %s: %v", test.name, err)
		}
		if err := heap.unpinHandle(test.handle); err == nil {
			t.Fatalf("unpin stale %s succeeded", test.name)
		}
	}

	for _, malformed := range []slot{slotNil, slotBool(false), slotBool(true)} {
		if err := heap.unpinHandle(malformed); err == nil {
			t.Fatalf("unpin immediate %#x succeeded", malformed)
		}
	}
}

func TestRuntimeHeapRejectsMalformedImmediatePayloads(t *testing.T) {
	var heap runtimeHeap
	malformed := []struct {
		name  string
		value slot
	}{
		{name: "nil", value: slot(slotTaggedPrefix | uint64(slotTagNil)<<slotTagShift | 1)},
		{name: "false", value: slot(slotTaggedPrefix | uint64(slotTagFalse)<<slotTagShift | 2)},
		{name: "true", value: slot(slotTaggedPrefix | uint64(slotTagTrue)<<slotTagShift | 3)},
	}
	for _, test := range malformed {
		if _, err := heap.exportValue(test.value); err == nil {
			t.Fatalf("export malformed %s immediate %#x succeeded", test.name, test.value)
		}
	}
}
