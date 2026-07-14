package ember

import (
	"fmt"
	"math"
	"sync"
	"time"
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
	mark       bool
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
	mark       bool
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

	// collectMu is the heap/world stop-the-world gate. Runtime ownership is
	// responsible for calling collect only when no VM execution is active; the
	// gate still serializes collection requests made by independent owners.
	collectMu sync.Mutex
	lastStats runtimeHeapStats
}

// runtimeHeapStats describes the last stop-the-world collection. The heap is
// private, so the fields intentionally stay small and package-local. allocated
// is the number of live entries observed before sweeping; live is the number
// that survived; reclaimed is the number returned to free lists.
type runtimeHeapStats struct {
	allocated    uint64
	live         uint64
	reclaimed    uint64
	pause        time.Duration
	staleHandles uint64
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
		entry.mark = false
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
		mark:       false,
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
	entry.mark = false
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
		entry.mark = false
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
		mark:       false,
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
	entry.mark = false
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

// runtimeHeapCollector is the small stop-the-world mark/sweep engine used by
// runtimeHeap.collect. Public Values still contain Go pointers during the
// migration, so scanners translate those pointers back to handles through
// the typed slab maps. Missing references are imported at the boundary; this
// keeps a reachable graph sound even when a caller supplied a public object
// before its first explicit heap import.
type runtimeHeapCollector struct {
	heap  *runtimeHeap
	work  []slot
	stats *runtimeHeapStats
	err   error

	protos      map[*Proto]struct{}
	frames      map[*vmFrame]struct{}
	threads     map[*vmThread]struct{}
	coroutines  map[*vmCoroutine]struct{}
	stackOwners map[*vmStackOwner]struct{}
	globals     map[*globalEnv]struct{}
}

// collect performs one complete stop-the-world collection from explicit slot
// roots. The owner is responsible for ensuring that no VM execution is active
// while this method runs; collectMu only serializes collection requests.
func (heap *runtimeHeap) collect(roots []slot) (runtimeHeapStats, error) {
	return heap.collectWithScanner(roots, nil)
}

// collectWithScanner performs one collection while allowing the runtime owner
// to contribute roots that have not migrated to slots yet. The callback runs
// after explicit handles and pins are marked but before the worklist drains,
// so Values, threads, coroutines, and prototypes all join the same graph walk.
// The owner must keep its world lock held for the entire call.
func (heap *runtimeHeap) collectWithScanner(roots []slot, scan func(*runtimeHeapCollector)) (runtimeHeapStats, error) {
	if heap == nil {
		return runtimeHeapStats{}, errRuntimeOwnerReleased
	}
	heap.collectMu.Lock()
	defer heap.collectMu.Unlock()

	started := time.Now()
	stats := runtimeHeapStats{}
	collector := runtimeHeapCollector{heap: heap, stats: &stats}
	collector.clearMarks()
	for _, root := range roots {
		collector.markSlot(root)
	}
	collector.markPinned()
	if scan != nil {
		scan(&collector)
	}
	collector.drain()
	// Scanning a still-Value-backed runtime may import a reachable object at
	// this boundary. Count that object as allocated for this cycle so the
	// published relationship allocated = live + reclaimed remains meaningful.
	stats.allocated = heap.liveEntryCount()
	if collector.err != nil {
		stats.live = stats.allocated
		stats.pause = time.Since(started)
		heap.lastStats = stats
		return stats, collector.err
	}
	stats.reclaimed = collector.sweep()
	stats.live = heap.liveEntryCount()
	stats.pause = time.Since(started)
	heap.lastStats = stats
	return stats, nil
}

// collectWithRoots is a convenience for callers that naturally hold roots as
// variadic arguments. It intentionally remains private with the heap seam.
func (heap *runtimeHeap) collectWithRoots(roots ...slot) (runtimeHeapStats, error) {
	return heap.collect(roots)
}

func (collector *runtimeHeapCollector) fail(err error) {
	if err != nil && collector.err == nil {
		collector.err = fmt.Errorf("runtime heap collect: %w", err)
	}
}

func (heap *runtimeHeap) collectionStats() runtimeHeapStats {
	if heap == nil {
		return runtimeHeapStats{}
	}
	heap.collectMu.Lock()
	defer heap.collectMu.Unlock()
	return heap.lastStats
}

func (collector *runtimeHeapCollector) clearMarks() {
	heap := collector.heap
	for index := 1; index < len(heap.boxedNumbers.entries); index++ {
		heap.boxedNumbers.entries[index].mark = false
	}
	clearSlabMarks(&heap.strings)
	clearSlabMarks(&heap.tables)
	clearSlabMarks(&heap.closures)
	clearSlabMarks(&heap.upvalues)
	clearSlabMarks(&heap.userdata)
	clearSlabMarks(&heap.hostCallables)
}

func clearSlabMarks[T comparable](slab *slotSlab[T]) {
	if slab == nil {
		return
	}
	for index := 1; index < len(slab.entries); index++ {
		slab.entries[index].mark = false
	}
}

func (collector *runtimeHeapCollector) markPinned() {
	heap := collector.heap
	for index := 1; index < len(heap.strings.entries); index++ {
		entry := heap.strings.entries[index]
		if entry.live && entry.pinned {
			collector.markHandle(slotTagString, uint32(index), entry.generation)
		}
	}
	for index := 1; index < len(heap.tables.entries); index++ {
		entry := heap.tables.entries[index]
		if entry.live && entry.pinned {
			collector.markHandle(slotTagTable, uint32(index), entry.generation)
		}
	}
	for index := 1; index < len(heap.closures.entries); index++ {
		entry := heap.closures.entries[index]
		if entry.live && entry.pinned {
			collector.markHandle(slotTagClosure, uint32(index), entry.generation)
		}
	}
	for index := 1; index < len(heap.upvalues.entries); index++ {
		entry := heap.upvalues.entries[index]
		if entry.live && entry.pinned {
			collector.markHandle(slotTagUpvalue, uint32(index), entry.generation)
		}
	}
	for index := 1; index < len(heap.userdata.entries); index++ {
		entry := heap.userdata.entries[index]
		if entry.live && entry.pinned {
			collector.markHandle(slotTagUserdata, uint32(index), entry.generation)
		}
	}
	for index := 1; index < len(heap.hostCallables.entries); index++ {
		entry := heap.hostCallables.entries[index]
		if entry.live && entry.pinned {
			collector.markHandle(slotTagHostCallable, uint32(index), entry.generation)
		}
	}
}

func (collector *runtimeHeapCollector) markSlot(value slot) {
	if !slotIsTagged(value) {
		return
	}
	kind := slotTagOf(value)
	if !slotHandleKind(kind) {
		return
	}
	_, index, generation, err := slotUnpackHandle(value)
	if err != nil {
		collector.stats.staleHandles++
		return
	}
	// Typed nil handles are deliberately not slab entries.
	if index == 0 && generation == 0 {
		return
	}
	if index == 0 || generation == 0 || !collector.markHandle(kind, index, generation) {
		collector.stats.staleHandles++
	}
}

func (collector *runtimeHeapCollector) markHandle(kind slotTag, index uint32, generation uint16) bool {
	if index == 0 || generation == 0 {
		return false
	}
	heap := collector.heap
	var marked *bool
	switch kind {
	case slotTagBoxedNumber:
		if int(index) >= len(heap.boxedNumbers.entries) {
			return false
		}
		entry := &heap.boxedNumbers.entries[index]
		if !entry.live || entry.retired || entry.generation != generation {
			return false
		}
		marked = &entry.mark
	case slotTagString:
		marked = slabMark(&heap.strings, index, generation)
	case slotTagTable:
		marked = slabMark(&heap.tables, index, generation)
	case slotTagClosure:
		marked = slabMark(&heap.closures, index, generation)
	case slotTagUpvalue:
		marked = slabMark(&heap.upvalues, index, generation)
	case slotTagUserdata:
		marked = slabMark(&heap.userdata, index, generation)
	case slotTagHostCallable:
		marked = slabMark(&heap.hostCallables, index, generation)
	default:
		return false
	}
	if marked == nil {
		return false
	}
	if *marked {
		return true
	}
	*marked = true
	handle, err := slotPackHandle(kind, index, generation)
	if err != nil {
		return false
	}
	collector.work = append(collector.work, handle)
	return true
}

func slabMark[T comparable](slab *slotSlab[T], index uint32, generation uint16) *bool {
	if slab == nil || index == 0 || int(index) >= len(slab.entries) {
		return nil
	}
	entry := &slab.entries[index]
	if !entry.live || entry.retired || entry.generation != generation {
		return nil
	}
	return &entry.mark
}

func (collector *runtimeHeapCollector) drain() {
	for len(collector.work) != 0 {
		last := len(collector.work) - 1
		handle := collector.work[last]
		collector.work = collector.work[:last]
		kind, index, generation, err := slotUnpackHandle(handle)
		if err != nil {
			continue
		}
		switch kind {
		case slotTagString, slotTagBoxedNumber:
			// Leaf entries.
		case slotTagTable:
			if table, err := collector.heap.tables.resolve(index, generation); err == nil {
				collector.scanTable(table)
			}
		case slotTagClosure:
			if closure, err := collector.heap.closures.resolve(index, generation); err == nil {
				collector.scanClosure(closure)
			}
		case slotTagUpvalue:
			if cell, err := collector.heap.upvalues.resolve(index, generation); err == nil {
				collector.scanCell(cell)
			}
		case slotTagUserdata:
			if userdata, err := collector.heap.userdata.resolve(index, generation); err == nil {
				collector.scanUserData(userdata)
			}
		case slotTagHostCallable:
			// Host function closures are opaque and are pinned at import.
		}
	}
}

func (collector *runtimeHeapCollector) scanValue(value Value) {
	switch valueKind(value) {
	case NumberKind:
		// Public Values still carry colliding NaN bits directly and therefore do
		// not identify one particular box. Preserve every live box for those
		// bits; boxed numbers have scalar semantics, so duplicates are
		// interchangeable and future migration can deduplicate them separately.
		if slotIsTagged(slot(value.bits)) {
			for index := 1; index < len(collector.heap.boxedNumbers.entries); index++ {
				entry := collector.heap.boxedNumbers.entries[index]
				if entry.live && !entry.retired && entry.bits == value.bits {
					collector.markHandle(slotTagBoxedNumber, uint32(index), entry.generation)
				}
			}
		}
	case StringKind:
		collector.markString(value.stringBox())
	case TableKind:
		collector.markTable(value.tableRef())
	case FunctionKind:
		closure, ok := value.scriptFunction()
		if ok {
			collector.markClosure(closure)
		}
	case UserDataKind:
		collector.markUserData(value.userdataRef())
	case HostFuncKind:
		collector.markHostCallable(value.hostCallableRef())
	}
}

func slabHandle[T comparable](slab *slotSlab[T], kind slotTag, value T) (slot, bool) {
	if slab == nil || slab.byValue == nil {
		return 0, false
	}
	index, ok := slab.byValue[value]
	if !ok || index == 0 || int(index) >= len(slab.entries) {
		return 0, false
	}
	entry := slab.entries[index]
	if !entry.live || entry.retired || entry.value != value {
		return 0, false
	}
	handle, err := slotPackHandle(kind, index, entry.generation)
	return handle, err == nil
}

func (collector *runtimeHeapCollector) markString(box *stringBox) {
	if box == nil {
		return
	}
	if handle, ok := slabHandle(&collector.heap.strings, slotTagString, box); ok {
		collector.markSlot(handle)
		return
	}
	if handle, err := collector.heap.importString(box); err == nil {
		collector.markSlot(handle)
	} else {
		collector.fail(err)
	}
}

func (collector *runtimeHeapCollector) markTable(table *Table) {
	if table == nil {
		return
	}
	if handle, ok := slabHandle(&collector.heap.tables, slotTagTable, table); ok {
		collector.markSlot(handle)
		return
	}
	if handle, err := collector.heap.importTable(table); err == nil {
		collector.markSlot(handle)
	} else {
		collector.fail(err)
	}
}

func (collector *runtimeHeapCollector) markClosure(closure *closure) {
	if closure == nil {
		return
	}
	if handle, ok := slabHandle(&collector.heap.closures, slotTagClosure, closure); ok {
		collector.markSlot(handle)
		return
	}
	if handle, err := collector.heap.importClosure(closure); err == nil {
		collector.markSlot(handle)
	} else {
		collector.fail(err)
	}
}

func (collector *runtimeHeapCollector) markCell(cell *cell) {
	if cell == nil {
		return
	}
	if handle, ok := slabHandle(&collector.heap.upvalues, slotTagUpvalue, cell); ok {
		collector.markSlot(handle)
		return
	}
	if handle, err := collector.heap.importCell(cell); err == nil {
		collector.markSlot(handle)
	} else {
		collector.fail(err)
	}
}

func (collector *runtimeHeapCollector) markUserData(userdata *UserData) {
	if userdata == nil {
		return
	}
	if handle, ok := slabHandle(&collector.heap.userdata, slotTagUserdata, userdata); ok {
		collector.markSlot(handle)
		return
	}
	if handle, err := collector.heap.importUserData(userdata); err == nil {
		collector.markSlot(handle)
	} else {
		collector.fail(err)
	}
}

func (collector *runtimeHeapCollector) markHostCallable(callable *hostCallable) {
	if callable == nil {
		return
	}
	if handle, ok := slabHandle(&collector.heap.hostCallables, slotTagHostCallable, callable); ok {
		collector.markSlot(handle)
		return
	}
	if handle, err := collector.heap.importHostCallable(callable); err == nil {
		collector.markSlot(handle)
	} else {
		collector.fail(err)
	}
}

func (collector *runtimeHeapCollector) scanTable(table *Table) {
	if table == nil {
		return
	}
	for _, value := range table.array {
		collector.scanValue(value)
	}
	for _, field := range table.stringFields {
		collector.markString(field.box)
		collector.scanValue(field.value)
	}
	if table.inlineFields != nil {
		for _, field := range table.inlineFields[:] {
			collector.markString(field.box)
			collector.scanValue(field.value)
		}
	}
	collector.markTable(table.metatable)
	if table.cold == nil {
		return
	}
	if fields := &table.cold.fields; fields != nil {
		for _, entry := range fields.entries {
			if entry.state != tableHashFull || entry.value.IsNil() {
				continue
			}
			collector.scanTableKey(entry.key)
			collector.scanValue(entry.value)
		}
	}
	if table.iteration != nil {
		for _, entry := range table.iteration.keys {
			collector.scanTableKey(entry.key)
		}
		for key := range table.iteration.index {
			collector.scanTableKey(key)
		}
	}
	// Metamethod lookup records are weak caches. Clear them while the table is
	// stopped so a dead table/string cannot remain reachable through a cache.
	table.cold.indexCache = tableMetamethodCache{}
	table.cold.newIndexCache = tableMetamethodCache{}
}

func (collector *runtimeHeapCollector) scanTableKey(key tableKey) {
	switch key.kind {
	case StringKind:
		collector.markString(key.strBox)
	case TableKind:
		collector.markTable(key.table)
	case UserDataKind:
		collector.markUserData(key.userdata)
	}
}

func (collector *runtimeHeapCollector) scanClosure(value *closure) {
	if value == nil {
		return
	}
	collector.scanProto(value.proto)
	for _, cell := range value.upvalues {
		collector.markCell(cell)
	}
	for index, item := range value.upvalueValues {
		if index < len(value.upvalueValueOK) && value.upvalueValueOK[index] {
			collector.scanValue(item)
		}
	}
	for index, cell := range value.inlineUpvalues {
		collector.markCell(cell)
		if index < len(value.inlineUpvalueOK) && value.inlineUpvalueOK[index] {
			collector.scanValue(value.inlineUpvalueValues[index])
		}
	}
}

func (collector *runtimeHeapCollector) scanProto(proto *Proto) {
	if proto == nil {
		return
	}
	if collector.protos == nil {
		collector.protos = make(map[*Proto]struct{})
	}
	if _, seen := collector.protos[proto]; seen {
		return
	}
	collector.protos[proto] = struct{}{}
	for _, value := range proto.constants {
		collector.scanValue(value)
	}
	for index, key := range proto.constantKeys {
		if index < len(proto.constantKeyOK) && proto.constantKeyOK[index] {
			collector.scanTableKey(key)
		}
	}
	for _, child := range proto.prototypes {
		collector.scanProto(child)
	}
}

func (collector *runtimeHeapCollector) scanCell(value *cell) {
	if value == nil {
		return
	}
	collector.scanValue(value.get())
	collector.scanStackOwner(value.owner)
}

func (collector *runtimeHeapCollector) scanUserData(value *UserData) {
	if value == nil {
		return
	}
	// UserData payloads are opaque by default. vmCoroutine is the one runtime
	// payload whose graph is known and therefore has a private scanner.
	if coroutine, ok := value.payload.(*vmCoroutine); ok {
		collector.scanCoroutine(coroutine)
	}
}

func (collector *runtimeHeapCollector) scanCoroutine(value *vmCoroutine) {
	if value == nil {
		return
	}
	if collector.coroutines == nil {
		collector.coroutines = make(map[*vmCoroutine]struct{})
	}
	if _, seen := collector.coroutines[value]; seen {
		return
	}
	collector.coroutines[value] = struct{}{}
	collector.markClosure(value.root)
	collector.markUserData(value.userdata)
	collector.scanThread(&value.thread)
	collector.scanSuspendedFrames(value.suspended)
	collector.scanValues(value.yieldedValues)
	collector.scanValues(value.resumeArgs)
	collector.scanValues(value.resumeResults)
	collector.clearInactiveFrameSlots(&value.thread, value.suspended.frames)
}

// clearInactiveFrameSlots drops references from reusable frame descriptors
// without clearing their backing arrays: a stale register slice can alias the
// live shared stack. Active and suspended frames remain semantic roots and are
// left intact.
func (collector *runtimeHeapCollector) clearInactiveFrameSlots(thread *vmThread, suspended []*vmFrame) {
	if thread == nil || len(thread.frameSlots) == 0 {
		return
	}
	active := make(map[*vmFrame]struct{}, len(thread.frames)+len(suspended))
	for _, frame := range thread.frames {
		active[frame] = struct{}{}
	}
	for _, frame := range suspended {
		active[frame] = struct{}{}
	}
	for _, frame := range thread.frameSlots {
		if frame == nil {
			continue
		}
		if _, ok := active[frame]; ok {
			continue
		}
		frame.clearOpenResultRange()
		frame.proto = nil
		frame.currentClosure = nil
		frame.window = vmRegisterWindow{}
		frame.owner = nil
		frame.registers = nil
		frame.cells = nil
		frame.upvalues = nil
		frame.upvalueValues = nil
		frame.upvalueValueOK = nil
		frame.varargOwner = nil
		frame.varargBase = 0
		frame.varargCount = 0
		frame.openResults = vmResultWindow{}
		frame.openRangeOwner = nil
		frame.openRangeBase = -1
		frame.openRangeCount = 0
		frame.openRangeLogicalTop = -1
		frame.pendingCall = vmPendingCall{}
		frame.hasPendingCall = false
	}
}

func (collector *runtimeHeapCollector) scanSuspendedFrames(value vmSuspendedFrames) {
	collector.scanGlobal(value.globals)
	collector.scanStackOwner(value.owner)
	collector.scanValues(value.stack)
	for _, cell := range value.openUpvalues {
		collector.markCell(cell)
	}
	for _, frame := range value.frames {
		collector.scanFrame(frame)
	}
	for _, record := range value.frameRecords {
		collector.scanClosure(record.closure)
	}
	collector.scanCoroutine(value.coroutine)
}

func (collector *runtimeHeapCollector) scanThread(value *vmThread) {
	if value == nil {
		return
	}
	if collector.threads == nil {
		collector.threads = make(map[*vmThread]struct{})
	}
	if _, seen := collector.threads[value]; seen {
		return
	}
	collector.threads[value] = struct{}{}
	collector.scanGlobal(value.globals)
	collector.scanGlobal(&value.baseGlobals)
	collector.scanStackOwner(value.stackOwner)
	collector.scanValues(value.stack)
	for _, cell := range value.openUpvalues {
		collector.markCell(cell)
	}
	for _, frame := range value.frames {
		collector.scanFrame(frame)
	}
	for _, record := range value.frameRecords {
		collector.scanClosure(record.closure)
	}
	collector.scanCoroutine(value.coroutine)
	// Function instances and their canonical closures are accelerators. Every
	// semantic closure is reachable through a frame, stack, cell, or result;
	// retaining this map would turn the cache into an accidental root.
	value.functionInstances = nil
	value.functionInstanceSites = 0
	value.stringIntern = nil
	value.stringConcatIntern = nil
	value.intrinsicGuards = nil
}

func (collector *runtimeHeapCollector) scanFrame(value *vmFrame) {
	if value == nil {
		return
	}
	if collector.frames == nil {
		collector.frames = make(map[*vmFrame]struct{})
	}
	if _, seen := collector.frames[value]; seen {
		return
	}
	collector.frames[value] = struct{}{}
	collector.scanProto(value.proto)
	collector.scanClosure(value.currentClosure)
	collector.scanStackOwner(value.owner)
	collector.scanStackOwner(value.window.owner)
	collector.scanStackOwner(value.varargOwner)
	collector.scanStackOwner(value.openRangeOwner)
	collector.scanValues(value.registers)
	for _, cell := range value.cells {
		collector.markCell(cell)
	}
	for _, cell := range value.upvalues {
		collector.markCell(cell)
	}
	for index, item := range value.upvalueValues {
		if index < len(value.upvalueValueOK) && value.upvalueValueOK[index] {
			collector.scanValue(item)
		}
	}
	collector.scanResultWindow(value.openResults)
	if value.hasPendingCall && value.pendingCall.protected != nil && value.pendingCall.protected.hasHandler {
		collector.scanValue(value.pendingCall.protected.handler)
	}
}

func (collector *runtimeHeapCollector) scanResultWindow(value vmResultWindow) {
	if value.usingInline {
		if value.count < 0 || value.count > len(value.inline) {
			return
		}
		collector.scanValues(value.inline[:value.count])
		return
	}
	if value.count < 0 || value.count > len(value.values) {
		return
	}
	collector.scanValues(value.values[:value.count])
}

func (collector *runtimeHeapCollector) scanGlobal(value *globalEnv) {
	if value == nil {
		return
	}
	if collector.globals == nil {
		collector.globals = make(map[*globalEnv]struct{})
	}
	if _, seen := collector.globals[value]; seen {
		return
	}
	collector.globals[value] = struct{}{}
	for _, item := range value.values {
		collector.scanValue(item)
	}
	for _, item := range value.host {
		collector.scanValue(item)
	}
	for _, item := range value.slots {
		collector.scanValue(item.value)
	}
	collector.scanThread(value.thread)
}

func (collector *runtimeHeapCollector) scanStackOwner(value *vmStackOwner) {
	if value == nil {
		return
	}
	if collector.stackOwners == nil {
		collector.stackOwners = make(map[*vmStackOwner]struct{})
	}
	if _, seen := collector.stackOwners[value]; seen {
		return
	}
	collector.stackOwners[value] = struct{}{}
	collector.scanValues(value.values)
}

func (collector *runtimeHeapCollector) scanValues(values []Value) {
	for _, value := range values {
		collector.scanValue(value)
	}
}

func (heap *runtimeHeap) liveEntryCount() uint64 {
	if heap == nil {
		return 0
	}
	var count uint64
	for index := 1; index < len(heap.boxedNumbers.entries); index++ {
		if heap.boxedNumbers.entries[index].live {
			count++
		}
	}
	count += liveSlabEntries(&heap.strings)
	count += liveSlabEntries(&heap.tables)
	count += liveSlabEntries(&heap.closures)
	count += liveSlabEntries(&heap.upvalues)
	count += liveSlabEntries(&heap.userdata)
	count += liveSlabEntries(&heap.hostCallables)
	return count
}

func liveSlabEntries[T comparable](slab *slotSlab[T]) uint64 {
	if slab == nil {
		return 0
	}
	var count uint64
	for index := 1; index < len(slab.entries); index++ {
		if slab.entries[index].live {
			count++
		}
	}
	return count
}

func (collector *runtimeHeapCollector) sweep() uint64 {
	heap := collector.heap
	var reclaimed uint64
	for index := 1; index < len(heap.boxedNumbers.entries); index++ {
		entry := &heap.boxedNumbers.entries[index]
		if !entry.live || entry.mark {
			continue
		}
		entry.bits = 0
		entry.live = false
		entry.mark = false
		reclaimed++
		if entry.generation == slotMaxGeneration {
			entry.retired = true
			continue
		}
		entry.generation++
		heap.boxedNumbers.free = append(heap.boxedNumbers.free, uint32(index))
	}
	reclaimed += sweepSlab(&heap.strings)
	reclaimed += sweepSlab(&heap.tables)
	reclaimed += sweepSlab(&heap.closures)
	reclaimed += sweepSlab(&heap.upvalues)
	reclaimed += sweepSlab(&heap.userdata)
	reclaimed += sweepSlab(&heap.hostCallables)
	return reclaimed
}

func sweepSlab[T comparable](slab *slotSlab[T]) uint64 {
	if slab == nil {
		return 0
	}
	var reclaimed uint64
	for index := 1; index < len(slab.entries); index++ {
		entry := &slab.entries[index]
		if !entry.live || entry.mark || entry.pinned {
			continue
		}
		delete(slab.byValue, entry.value)
		var zero T
		entry.value = zero
		entry.live = false
		entry.mark = false
		reclaimed++
		if entry.generation == slotMaxGeneration {
			entry.retired = true
			continue
		}
		entry.generation++
		slab.free = append(slab.free, uint32(index))
	}
	return reclaimed
}
