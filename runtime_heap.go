package ember

import (
	"fmt"
	"math"
	"unsafe"
)

// runtimeHeap is the private owner for out-of-line slot payloads. It is kept
// intentionally small until the runtime's public Value/slot conversion seam
// is introduced.
type slotNumberEntry struct {
	bits       uint64
	generation uint16
	live       bool
	retired    bool
}

// slotNumberSlab is the first typed generation-checked slab. Index zero is
// reserved for typed-nil payloads, while retired entries are never recycled.
type slotNumberSlab struct {
	entries []slotNumberEntry
	free    []uint32
}

type slotSlabEntry[T comparable] struct {
	value      T
	generation uint16
	live       bool
	retired    bool
	pinned     bool
}

// slotSlab owns one typed family of Go references. The slab keeps references
// strongly reachable for the lifetime of a live handle, while generation
// checks prevent a released handle from resolving to a recycled entry.
type slotSlab[T comparable] struct {
	entries []slotSlabEntry[T]
	free    []uint32
	byValue map[T]uint32
}

type runtimeHeap struct {
	boxedNumbers  slotNumberSlab
	strings       slotSlab[*stringBox]
	tables        slotSlab[*Table]
	closures      slotSlab[*closure]
	upvalues      slotSlab[*cell]
	userdata      slotSlab[*UserData]
	hostCallables slotSlab[*hostCallable]
}

func (slab *slotSlab[T]) add(value T, pinned bool) (uint32, uint16, error) {
	if slab.byValue == nil {
		slab.byValue = make(map[T]uint32)
	}
	if index, ok := slab.byValue[value]; ok {
		if int(index) < len(slab.entries) {
			entry := &slab.entries[index]
			if entry.live && !entry.retired && entry.value == value {
				if pinned {
					entry.pinned = true
				}
				return index, entry.generation, nil
			}
		}
		delete(slab.byValue, value)
	}
	if len(slab.entries) == 0 {
		// Index zero is reserved for typed nil handles.
		slab.entries = append(slab.entries, slotSlabEntry[T]{})
	}
	for len(slab.free) > 0 {
		last := len(slab.free) - 1
		index := slab.free[last]
		slab.free = slab.free[:last]
		entry := &slab.entries[index]
		if entry.retired || entry.live {
			continue
		}
		entry.value = value
		entry.live = true
		entry.pinned = pinned
		slab.byValue[value] = index
		return index, entry.generation, nil
	}
	if len(slab.entries) > int(slotIndexMask) {
		return 0, 0, fmt.Errorf("slot: typed slab exhausted")
	}
	index := uint32(len(slab.entries))
	slab.entries = append(slab.entries, slotSlabEntry[T]{
		value:      value,
		generation: 1,
		live:       true,
		pinned:     pinned,
	})
	slab.byValue[value] = index
	return index, 1, nil
}

func (slab *slotSlab[T]) resolve(index uint32, generation uint16) (T, error) {
	var zero T
	if index == 0 || int(index) >= len(slab.entries) {
		return zero, fmt.Errorf("slot: typed handle index %d is invalid", index)
	}
	entry := slab.entries[index]
	if !entry.live || entry.retired || entry.generation != generation {
		return zero, fmt.Errorf("slot: typed handle is stale")
	}
	return entry.value, nil
}

func (slab *slotSlab[T]) release(index uint32, generation uint16) error {
	if index == 0 || int(index) >= len(slab.entries) {
		return fmt.Errorf("slot: typed handle index %d is invalid", index)
	}
	entry := &slab.entries[index]
	if !entry.live || entry.retired || entry.generation != generation {
		return fmt.Errorf("slot: typed handle is stale")
	}
	if entry.pinned {
		return fmt.Errorf("slot: typed handle is pinned")
	}
	delete(slab.byValue, entry.value)
	var zero T
	entry.value = zero
	entry.live = false
	entry.pinned = false
	if generation == slotMaxGeneration {
		entry.retired = true
		return nil
	}
	entry.generation++
	slab.free = append(slab.free, index)
	return nil
}

func (slab *slotSlab[T]) unpin(index uint32, generation uint16) error {
	if index == 0 || int(index) >= len(slab.entries) {
		return fmt.Errorf("slot: typed handle index %d is invalid", index)
	}
	entry := &slab.entries[index]
	if !entry.live || entry.retired || entry.generation != generation {
		return fmt.Errorf("slot: typed handle is stale")
	}
	entry.pinned = false
	return nil
}

func (heap *runtimeHeap) importValue(value Value) (slot, error) {
	switch valueKind(value) {
	case NilKind:
		return slotNil, nil
	case BoolKind:
		return slotBool(valueBool(value)), nil
	case NumberKind:
		return slotFromNumberBits(value.bits, heap)
	case StringKind:
		return heap.importString(value.stringBox())
	case TableKind:
		return heap.importTable(value.tableRef())
	case FunctionKind:
		closure, _ := value.scriptFunction()
		return heap.importClosure(closure)
	case UserDataKind:
		return heap.importUserData(value.userdataRef())
	case HostFuncKind:
		if valueRef(value) == nil {
			id := valueNativeID(value)
			if id == nativeFuncUnknown {
				return slotPackTypedNil(slotTagHostCallable)
			}
			return slotNativeID(id), nil
		}
		return heap.importHostCallable(value.hostCallableRef())
	default:
		return 0, fmt.Errorf("slot: %s value adapter is unsupported", valueKind(value))
	}
}

func (heap *runtimeHeap) exportValue(value slot) (Value, error) {
	if !slotIsTagged(value) {
		bits, err := slotNumberBits(value, heap)
		if err != nil {
			return NilValue(), err
		}
		return NumberValue(math.Float64frombits(bits)), nil
	}
	switch slotTagOf(value) {
	case slotTagNil:
		if !slotImmediatePayloadZero(value) {
			return NilValue(), fmt.Errorf("slot: nil immediate has a non-zero payload")
		}
		return NilValue(), nil
	case slotTagFalse, slotTagTrue:
		boolean, err := slotBoolValue(value)
		if err != nil {
			return NilValue(), err
		}
		return BoolValue(boolean), nil
	case slotTagBoxedNumber:
		bits, err := slotNumberBits(value, heap)
		if err != nil {
			return NilValue(), err
		}
		return NumberValue(math.Float64frombits(bits)), nil
	case slotTagString:
		box, err := heap.exportString(value)
		if err != nil {
			return NilValue(), err
		}
		return stringValueFromBox(box), nil
	case slotTagTable:
		table, err := heap.exportTable(value)
		if err != nil {
			return NilValue(), err
		}
		return TableValue(table), nil
	case slotTagClosure:
		closure, err := heap.exportClosure(value)
		if err != nil {
			return NilValue(), err
		}
		return closureFunctionValue(closure), nil
	case slotTagUserdata:
		userdata, err := heap.exportUserData(value)
		if err != nil {
			return NilValue(), err
		}
		return UserDataValue(userdata), nil
	case slotTagHostCallable:
		callable, err := heap.exportHostCallable(value)
		if err != nil {
			return NilValue(), err
		}
		if callable == nil {
			return valueWithRef(HostFuncKind, nil), nil
		}
		return valueWithRef(HostFuncKind, unsafe.Pointer(callable)), nil
	case slotTagNativeID:
		id, err := slotNativeIDValue(value)
		if err != nil {
			return NilValue(), err
		}
		return nativeFuncValueWithID(nil, id), nil
	default:
		return NilValue(), fmt.Errorf("slot: %v adapter is not initialized", slotTagOf(value))
	}
}

func slotImportRef[T comparable](slab *slotSlab[T], kind slotTag, value T, pinned bool) (slot, error) {
	index, generation, err := slab.add(value, pinned)
	if err != nil {
		return 0, err
	}
	return slotPackHandle(kind, index, generation)
}

func slotExportRef[T comparable](slab *slotSlab[T], value slot, wantKind slotTag) (T, error) {
	var zero T
	kind, index, generation, err := slotUnpackHandle(value)
	if err != nil {
		return zero, err
	}
	if kind != wantKind {
		return zero, fmt.Errorf("slot: %v handle is not a %v handle", kind, wantKind)
	}
	if index == 0 && generation == 0 {
		return zero, nil
	}
	index, generation, err = slotValidateHandle(value, wantKind)
	if err != nil {
		return zero, err
	}
	return slab.resolve(index, generation)
}

func (heap *runtimeHeap) importString(box *stringBox) (slot, error) {
	if box == nil {
		return slotPackTypedNil(slotTagString)
	}
	index, generation, err := heap.strings.add(box, false)
	if err != nil {
		return 0, err
	}
	return slotPackHandle(slotTagString, index, generation)
}

func (heap *runtimeHeap) exportString(value slot) (*stringBox, error) {
	kind, index, generation, err := slotUnpackHandle(value)
	if err != nil {
		return nil, err
	}
	if kind != slotTagString {
		return nil, fmt.Errorf("slot: %v is not a string handle", kind)
	}
	if index == 0 && generation == 0 {
		return nil, nil
	}
	if index == 0 || generation == 0 {
		return nil, fmt.Errorf("slot: malformed typed nil string handle")
	}
	return heap.strings.resolve(index, generation)
}

func (heap *runtimeHeap) importTable(table *Table) (slot, error) {
	if table == nil {
		return slotPackTypedNil(slotTagTable)
	}
	return slotImportRef(&heap.tables, slotTagTable, table, false)
}

func (heap *runtimeHeap) exportTable(value slot) (*Table, error) {
	return slotExportRef(&heap.tables, value, slotTagTable)
}

func (heap *runtimeHeap) importClosure(value *closure) (slot, error) {
	if value == nil {
		return slotPackTypedNil(slotTagClosure)
	}
	return slotImportRef(&heap.closures, slotTagClosure, value, false)
}

func (heap *runtimeHeap) exportClosure(value slot) (*closure, error) {
	return slotExportRef(&heap.closures, value, slotTagClosure)
}

func (heap *runtimeHeap) importCell(value *cell) (slot, error) {
	if value == nil {
		return slotPackTypedNil(slotTagUpvalue)
	}
	return slotImportRef(&heap.upvalues, slotTagUpvalue, value, false)
}

func (heap *runtimeHeap) exportCell(value slot) (*cell, error) {
	return slotExportRef(&heap.upvalues, value, slotTagUpvalue)
}

func (heap *runtimeHeap) importUserData(value *UserData) (slot, error) {
	if value == nil {
		return slotPackTypedNil(slotTagUserdata)
	}
	return slotImportRef(&heap.userdata, slotTagUserdata, value, true)
}

func (heap *runtimeHeap) exportUserData(value slot) (*UserData, error) {
	return slotExportRef(&heap.userdata, value, slotTagUserdata)
}

func (heap *runtimeHeap) importHostCallable(value *hostCallable) (slot, error) {
	if value == nil {
		return slotPackTypedNil(slotTagHostCallable)
	}
	return slotImportRef(&heap.hostCallables, slotTagHostCallable, value, true)
}

func (heap *runtimeHeap) exportHostCallable(value slot) (*hostCallable, error) {
	return slotExportRef(&heap.hostCallables, value, slotTagHostCallable)
}

func (heap *runtimeHeap) releaseHandle(value slot) error {
	kind, index, generation, err := slotUnpackHandle(value)
	if err != nil {
		return err
	}
	if index == 0 || generation == 0 {
		return fmt.Errorf("slot: typed nil handle cannot be released")
	}
	switch kind {
	case slotTagBoxedNumber:
		return heap.releaseBoxedNumber(value)
	case slotTagString:
		return heap.strings.release(index, generation)
	case slotTagTable:
		return heap.tables.release(index, generation)
	case slotTagClosure:
		return heap.closures.release(index, generation)
	case slotTagUpvalue:
		return heap.upvalues.release(index, generation)
	case slotTagUserdata:
		return heap.userdata.release(index, generation)
	case slotTagHostCallable:
		return heap.hostCallables.release(index, generation)
	default:
		return fmt.Errorf("slot: %v handle slab is unsupported", kind)
	}
}

func (heap *runtimeHeap) unpinHandle(value slot) error {
	kind, index, generation, err := slotUnpackHandle(value)
	if err != nil {
		return err
	}
	if index == 0 || generation == 0 {
		return fmt.Errorf("slot: typed nil handle cannot be unpinned")
	}
	switch kind {
	case slotTagString:
		return heap.strings.unpin(index, generation)
	case slotTagTable:
		return heap.tables.unpin(index, generation)
	case slotTagClosure:
		return heap.closures.unpin(index, generation)
	case slotTagUpvalue:
		return heap.upvalues.unpin(index, generation)
	case slotTagUserdata:
		return heap.userdata.unpin(index, generation)
	case slotTagHostCallable:
		return heap.hostCallables.unpin(index, generation)
	case slotTagBoxedNumber:
		return fmt.Errorf("slot: boxed number handles are not pinnable")
	default:
		return fmt.Errorf("slot: %v handle slab is unsupported", kind)
	}
}

func (heap *runtimeHeap) validateHandle(value slot, wantKind slotTag) (uint32, uint16, error) {
	index, generation, err := slotValidateHandle(value, wantKind)
	if err != nil {
		return 0, 0, err
	}
	switch wantKind {
	case slotTagBoxedNumber:
		if _, err := slotNumberBits(value, heap); err != nil {
			return 0, 0, err
		}
	case slotTagString:
		if _, err := heap.strings.resolve(index, generation); err != nil {
			return 0, 0, err
		}
	case slotTagTable:
		if _, err := heap.tables.resolve(index, generation); err != nil {
			return 0, 0, err
		}
	case slotTagClosure:
		if _, err := heap.closures.resolve(index, generation); err != nil {
			return 0, 0, err
		}
	case slotTagUpvalue:
		if _, err := heap.upvalues.resolve(index, generation); err != nil {
			return 0, 0, err
		}
	case slotTagUserdata:
		if _, err := heap.userdata.resolve(index, generation); err != nil {
			return 0, 0, err
		}
	case slotTagHostCallable:
		if _, err := heap.hostCallables.resolve(index, generation); err != nil {
			return 0, 0, err
		}
	}
	return index, generation, nil
}

func slotFromNumberBits(bits uint64, heap *runtimeHeap) (slot, error) {
	value := slot(bits)
	if !slotIsTagged(value) {
		return value, nil
	}
	if heap == nil {
		return 0, fmt.Errorf("slot: number bits %#x require a heap box", bits)
	}
	return heap.boxNumberBits(bits)
}

func (heap *runtimeHeap) boxNumberBits(bits uint64) (slot, error) {
	slab := &heap.boxedNumbers
	if len(slab.entries) == 0 {
		// Index zero is reserved so that an all-zero handle payload can remain a
		// typed-nil sentinel once other reference kinds use this slab.
		slab.entries = append(slab.entries, slotNumberEntry{})
	}
	for len(slab.free) > 0 {
		last := len(slab.free) - 1
		index := slab.free[last]
		slab.free = slab.free[:last]
		entry := &slab.entries[index]
		if entry.retired || entry.live {
			continue
		}
		entry.bits = bits
		entry.live = true
		return slotPackHandle(slotTagBoxedNumber, index, entry.generation)
	}
	if len(slab.entries) > int(slotIndexMask) {
		return 0, fmt.Errorf("slot: boxed number slab exhausted")
	}
	index := uint32(len(slab.entries))
	slab.entries = append(slab.entries, slotNumberEntry{
		bits:       bits,
		generation: 1,
		live:       true,
	})
	return slotPackHandle(slotTagBoxedNumber, index, 1)
}

func (heap *runtimeHeap) releaseBoxedNumber(value slot) error {
	kind, index, generation, err := slotUnpackHandle(value)
	if err != nil {
		return err
	}
	if kind != slotTagBoxedNumber {
		return fmt.Errorf("slot: %v is not a boxed number handle", kind)
	}
	slab := &heap.boxedNumbers
	if index == 0 || int(index) >= len(slab.entries) {
		return fmt.Errorf("slot: boxed number index %d is invalid", index)
	}
	entry := &slab.entries[index]
	if !entry.live || entry.retired || entry.generation != generation {
		return fmt.Errorf("slot: boxed number handle is stale")
	}
	entry.bits = 0
	entry.live = false
	if generation == slotMaxGeneration {
		entry.retired = true
		return nil
	}
	entry.generation++
	slab.free = append(slab.free, index)
	return nil
}

func slotNumberBits(value slot, heap *runtimeHeap) (uint64, error) {
	if !slotIsTagged(value) {
		return uint64(value), nil
	}
	if slotTagOf(value) == slotTagBoxedNumber {
		if heap == nil {
			return 0, fmt.Errorf("slot: boxed number needs a heap")
		}
		kind, index, generation, err := slotUnpackHandle(value)
		if err != nil {
			return 0, err
		}
		if kind != slotTagBoxedNumber {
			return 0, fmt.Errorf("slot: %v is not a boxed number handle", kind)
		}
		if index == 0 || int(index) >= len(heap.boxedNumbers.entries) {
			return 0, fmt.Errorf("slot: boxed number index %d is invalid", index)
		}
		entry := heap.boxedNumbers.entries[index]
		if !entry.live || entry.retired || entry.generation != generation {
			return 0, fmt.Errorf("slot: boxed number handle is stale")
		}
		return entry.bits, nil
	}
	return 0, fmt.Errorf("slot: tagged %v is not an inline number", slotTagOf(value))
}
