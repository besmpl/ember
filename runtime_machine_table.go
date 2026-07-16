package ember

import (
	"errors"
	"math"
)

// machineTableID is an owner-local dense handle. Zero is invalid so an ID can
// also be stored in a zero-initialized scalar slot.
type machineTableID uint32

const invalidMachineTableID machineTableID = 0

type machineTableLayoutID uint32

const (
	machineTableDynamicLayout machineTableLayoutID = 0
	machineTableRootLayout    machineTableLayoutID = 1
)

// machineTableRecord addresses this table's spans in the owner arena. It has
// no Go pointers; stopped mutations may relocate spans without changing the
// table ID observed by the guest kernel.
type machineTableRecord struct {
	arrayOffset    uint32
	arrayLength    uint32
	arrayCapacity  uint32
	fieldOffset    uint32
	fieldCapacity  uint32
	fieldCount     uint32
	fieldTombstone uint32
	orderOffset    uint32
	orderLength    uint32
	orderCapacity  uint32
	orderTombstone uint32
	entryCount     uint32
	metatable      machineTableID
	layout         machineTableLayoutID
	rawVersion     uint32
	metaVersion    uint32
	protection     slot
}

type machineTableKeyKind uint8

const (
	machineTableKeyInvalid machineTableKeyKind = iota
	machineTableKeyArray
	machineTableKeySlot
	machineTableKeyString
)

// machineTableKey carries either an array index, a canonical scalar slot key,
// or an owner-local interned string ID.
type machineTableKey struct {
	value slot
	id    uint32
	kind  machineTableKeyKind
	_     [3]byte
}

type machineTableFieldState uint8

const (
	machineTableFieldEmpty machineTableFieldState = iota
	machineTableFieldLive
	machineTableFieldDeleted
)

// machineTableField is one open-addressed index cell. The key and value live
// in the insertion-order arena; this cell only points to them by scalar index.
type machineTableField struct {
	hash       uint64
	orderIndex uint32
	state      machineTableFieldState
	_          [3]byte
}

// machineTableOrderEntry preserves deterministic insertion order across every
// guest-visible key storage class.
type machineTableOrderEntry struct {
	key     machineTableKey
	value   slot
	present uint8
	_       [7]byte
}

// machineTableArena owns all mutable table state for one Machine. Its slices
// are stopped-boundary storage; guest-visible records and elements contain
// only scalar offsets, IDs, keys, and values.
type machineTableArena struct {
	tables              []machineTableRecord
	arrays              []slot
	fields              []machineTableField
	orders              []machineTableOrderEntry
	layouts             []machineTableLayout
	layoutsByTransition map[machineTableLayoutTransition]machineTableLayoutID
	// The allocation cursors may be shorter than the backing slices after a
	// callback rollback. Reusing that high-water storage avoids append-driven
	// clearing on the next warmed call.
	arrayCursor int
	fieldCursor int
	orderCursor int
	closed      bool
}

// machineTableArenaCheckpoint marks one stopped boundary. A callback may roll
// back storage appended after the mark when none of the new tables escaped and
// no older table relocated one of its spans into the appended region.
type machineTableArenaCheckpoint struct {
	tables int
	arrays int
	fields int
	orders int
}

type machineTableCursor struct {
	key   machineTableKey
	index uint32
	set   uint8
	_     [3]byte
}

const (
	machineTableInitialFieldCapacity uint32 = 8
	machineTableCompactOrderMinimum  uint32 = 32
	machineTableLayoutLimit                 = 16 * 1024
)

type machineTableLayout struct {
	parent machineTableLayoutID
	key    machineTableKey
	offset uint32
}

type machineTableLayoutTransition struct {
	parent machineTableLayoutID
	key    machineTableKey
}

var (
	errMachineTableArenaClosed = errors.New("machine table arena is closed")
	errMachineTableInvalidID   = errors.New("machine table ID is invalid")
	errMachineTableInvalidKey  = errors.New("machine table key is invalid")
)

func machineTableArrayKey(index uint32) machineTableKey {
	return machineTableKey{kind: machineTableKeyArray, id: index}
}

func machineTableSlotKey(value slot) machineTableKey {
	return machineTableKey{kind: machineTableKeySlot, value: value}
}

func machineTableStringKey(id machineStringID) machineTableKey {
	return machineTableKey{kind: machineTableKeyString, id: uint32(id)}
}

// newTableStopped creates a table with optional array and record capacity
// hints. It may grow backing storage and therefore belongs at a stopped Go
// boundary.
func (arena *machineTableArena) newTableStopped(arrayCapacity, recordCapacity uint32) (machineTableID, error) {
	if arena == nil || arena.closed {
		return invalidMachineTableID, errMachineTableArenaClosed
	}
	if len(arena.tables) == math.MaxUint32 {
		return invalidMachineTableID, errors.New("machine table arena has too many tables")
	}
	fieldCapacity, err := machineTableFieldCapacity(recordCapacity)
	if err != nil {
		return invalidMachineTableID, err
	}
	orderCapacity := uint64(arrayCapacity) + uint64(recordCapacity)
	if orderCapacity > math.MaxUint32 {
		return invalidMachineTableID, errors.New("machine table arena capacity overflows uint32")
	}
	if !machineTableSpanFits(arena.arrayCursor, arrayCapacity) ||
		!machineTableSpanFits(arena.fieldCursor, fieldCapacity) ||
		!machineTableSpanFits(arena.orderCursor, uint32(orderCapacity)) {
		return invalidMachineTableID, errors.New("machine table arena capacity overflows uint32")
	}
	arrayOffset := arena.reserveArraysStopped(arrayCapacity)
	fieldOffset := arena.reserveFieldsStopped(fieldCapacity)
	orderOffset := arena.reserveOrdersStopped(uint32(orderCapacity))
	arena.ensureLayoutDomainStopped()

	record := machineTableRecord{
		arrayOffset:   arrayOffset,
		arrayCapacity: arrayCapacity,
		fieldOffset:   fieldOffset,
		fieldCapacity: fieldCapacity,
		orderOffset:   orderOffset,
		orderCapacity: uint32(orderCapacity),
		layout:        machineTableRootLayout,
		protection:    slotNil,
	}
	if fieldCapacity != 0 {
		clear(arena.fields[int(fieldOffset):int(fieldOffset+fieldCapacity)])
	}
	arena.tables = append(arena.tables, record)
	return machineTableID(len(arena.tables)), nil
}

func (arena *machineTableArena) setArrayStopped(id machineTableID, index uint32, value slot) error {
	tableIndex, ok := arena.tableIndex(id)
	if !ok {
		return arena.tableError()
	}
	if index == 0 {
		return errMachineTableInvalidKey
	}
	key := machineTableArrayKey(index)
	if value == slotNil {
		record := &arena.tables[tableIndex]
		if index > record.arrayLength {
			return nil
		}
		if arena.arrays[int(record.arrayOffset+index-1)] == slotNil {
			return nil
		}
		arena.arrays[int(record.arrayOffset+index-1)] = slotNil
		arena.markOrderDeletedStopped(tableIndex, key)
		record.entryCount--
		record.layout = machineTableDynamicLayout
		record.rawVersion = machineTableNextVersion(record.rawVersion)
		for record.arrayLength > 0 && arena.arrays[int(record.arrayOffset+record.arrayLength-1)] == slotNil {
			record.arrayLength--
		}
		return nil
	}
	record := &arena.tables[tableIndex]
	previousLength := record.arrayLength
	var previous slot = slotNil
	if index <= record.arrayLength {
		previous = arena.arrays[int(record.arrayOffset+index-1)]
	}
	if previous == value {
		return nil
	}
	if err := arena.ensureArrayStopped(tableIndex, index); err != nil {
		return err
	}
	if err := arena.setOrderStopped(tableIndex, key, value); err != nil {
		return err
	}
	record = &arena.tables[tableIndex]
	for gap := previousLength + 1; gap < index; gap++ {
		arena.arrays[int(record.arrayOffset+gap-1)] = slotNil
	}
	arena.arrays[int(record.arrayOffset+index-1)] = value
	if previous == slotNil {
		record.entryCount++
		arena.noteLayoutInsertStopped(tableIndex, key)
	}
	record.rawVersion = machineTableNextVersion(record.rawVersion)
	if index > record.arrayLength {
		record.arrayLength = index
	}
	return nil
}

func (arena *machineTableArena) setSlotStopped(id machineTableID, key, value slot) error {
	if key == slotNil {
		return errMachineTableInvalidKey
	}
	return arena.setRecordStopped(id, machineTableSlotKey(key), value)
}

func (arena *machineTableArena) setStringStopped(id machineTableID, key machineStringID, value slot) error {
	if key == invalidMachineStringID {
		return errMachineTableInvalidKey
	}
	return arena.setRecordStopped(id, machineTableStringKey(key), value)
}

func (arena *machineTableArena) getArray(id machineTableID, index uint32) (slot, bool) {
	record, ok := arena.lookupStopped(id)
	if !ok || index == 0 || index > record.arrayLength {
		return slotNil, false
	}
	value := arena.arrays[int(record.arrayOffset+index-1)]
	return value, value != slotNil
}

func (arena *machineTableArena) getSlot(id machineTableID, key slot) (slot, bool) {
	if key == slotNil {
		return slotNil, false
	}
	return arena.getRecord(id, machineTableSlotKey(key))
}

func (arena *machineTableArena) getString(id machineTableID, key machineStringID) (slot, bool) {
	if key == invalidMachineStringID {
		return slotNil, false
	}
	return arena.getRecord(id, machineTableStringKey(key))
}

func (arena *machineTableArena) next(id machineTableID, cursor machineTableCursor) (machineTableKey, slot, machineTableCursor, bool, error) {
	record, ok := arena.lookupStopped(id)
	if !ok {
		return machineTableKey{}, slotNil, machineTableCursor{}, false, arena.tableError()
	}
	start := uint32(0)
	if cursor.set > 1 {
		return machineTableKey{}, slotNil, machineTableCursor{}, false, errMachineTableInvalidKey
	}
	if cursor.set != 0 {
		index, ok := arena.liveOrderIndex(record, cursor.key, cursor.index)
		if !ok {
			return machineTableKey{}, slotNil, machineTableCursor{}, false, errMachineTableInvalidKey
		}
		start = index + 1
	}
	for index := start; index < record.orderLength; index++ {
		entry := arena.orders[int(record.orderOffset+index)]
		if entry.present != 0 && entry.value != slotNil {
			return entry.key, entry.value,
				machineTableCursor{key: entry.key, index: index, set: 1}, true, nil
		}
	}
	return machineTableKey{}, slotNil, machineTableCursor{}, false, nil
}

func (arena *machineTableArena) checkpointStopped() machineTableArenaCheckpoint {
	if arena == nil {
		return machineTableArenaCheckpoint{}
	}
	return machineTableArenaCheckpoint{
		tables: len(arena.tables),
		arrays: arena.arrayCursor,
		fields: arena.fieldCursor,
		orders: arena.orderCursor,
	}
}

// rollbackStopped drops storage appended after checkpoint while retaining its
// backing capacity for the next callback. It refuses rollback if an older
// table grew into the appended region because that span is persistent state.
func (arena *machineTableArena) rollbackStopped(checkpoint machineTableArenaCheckpoint) bool {
	if arena == nil || arena.closed || checkpoint.tables < 0 || checkpoint.tables > len(arena.tables) ||
		checkpoint.arrays < 0 || checkpoint.arrays > arena.arrayCursor ||
		checkpoint.fields < 0 || checkpoint.fields > arena.fieldCursor ||
		checkpoint.orders < 0 || checkpoint.orders > arena.orderCursor {
		return false
	}
	for index := 0; index < checkpoint.tables; index++ {
		record := arena.tables[index]
		if !machineTableCheckpointSpanFits(record.arrayOffset, record.arrayCapacity, checkpoint.arrays) ||
			!machineTableCheckpointSpanFits(record.fieldOffset, record.fieldCapacity, checkpoint.fields) ||
			!machineTableCheckpointSpanFits(record.orderOffset, record.orderCapacity, checkpoint.orders) {
			return false
		}
	}
	clear(arena.tables[checkpoint.tables:])
	arena.tables = arena.tables[:checkpoint.tables]
	arena.arrayCursor = checkpoint.arrays
	arena.fieldCursor = checkpoint.fields
	arena.orderCursor = checkpoint.orders
	return true
}

func machineTableCheckpointSpanFits(offset, capacity uint32, limit int) bool {
	return uint64(offset)+uint64(capacity) <= uint64(limit)
}

// reset drops all logical tables while retaining cleared backing storage for
// the next stopped bind. IDs are owner-local and restart densely at one.
func (arena *machineTableArena) reset() {
	if arena == nil {
		return
	}
	clear(arena.tables)
	clear(arena.arrays)
	clear(arena.fields)
	clear(arena.orders)
	arena.tables = arena.tables[:0]
	arena.arrays = arena.arrays[:0]
	arena.fields = arena.fields[:0]
	arena.orders = arena.orders[:0]
	arena.layouts = nil
	arena.layoutsByTransition = nil
	arena.arrayCursor = 0
	arena.fieldCursor = 0
	arena.orderCursor = 0
	arena.closed = false
}

// close releases backing storage and makes future mutation fail closed. It is
// idempotent and is intended for the Machine's stopped teardown boundary.
func (arena *machineTableArena) close() {
	if arena == nil {
		return
	}
	arena.tables = nil
	arena.arrays = nil
	arena.fields = nil
	arena.orders = nil
	arena.layouts = nil
	arena.layoutsByTransition = nil
	arena.arrayCursor = 0
	arena.fieldCursor = 0
	arena.orderCursor = 0
	arena.closed = true
}

func (arena *machineTableArena) setRecordStopped(id machineTableID, key machineTableKey, value slot) error {
	tableIndex, ok := arena.tableIndex(id)
	if !ok {
		return arena.tableError()
	}
	if key.kind != machineTableKeySlot && key.kind != machineTableKeyString {
		return errMachineTableInvalidKey
	}
	hash := machineTableHash(key)
	if fieldIndex, found := arena.findLiveField(arena.tables[tableIndex], key, hash); found {
		field := arena.fields[fieldIndex]
		if value == slotNil {
			arena.fields[fieldIndex].state = machineTableFieldDeleted
			record := &arena.tables[tableIndex]
			record.fieldCount--
			record.fieldTombstone++
			record.entryCount--
			record.layout = machineTableDynamicLayout
			record.rawVersion = machineTableNextVersion(record.rawVersion)
			arena.markOrderDeletedStopped(tableIndex, key)
			return nil
		}
		entry := &arena.orders[int(arena.tables[tableIndex].orderOffset+field.orderIndex)]
		if entry.value == value {
			return nil
		}
		entry.value = value
		arena.tables[tableIndex].rawVersion = machineTableNextVersion(arena.tables[tableIndex].rawVersion)
		return nil
	}
	if value == slotNil {
		return nil
	}
	if err := arena.ensureFieldsStopped(tableIndex); err != nil {
		return err
	}
	record := arena.tables[tableIndex]
	orderIndex, retained := arena.deletedOrderIndex(record, key)
	if !retained {
		if err := arena.ensureOrderStopped(tableIndex, record.orderLength+1); err != nil {
			return err
		}
		record = arena.tables[tableIndex]
		orderIndex = record.orderLength
		arena.tables[tableIndex].orderLength++
	} else {
		arena.tables[tableIndex].orderTombstone--
	}
	entry := &arena.orders[int(record.orderOffset+orderIndex)]
	*entry = machineTableOrderEntry{key: key, value: value, present: 1}
	fieldIndex, wasDeleted := arena.findInsertField(record, key, hash)
	arena.fields[fieldIndex] = machineTableField{hash: hash, orderIndex: orderIndex, state: machineTableFieldLive}
	arena.tables[tableIndex].fieldCount++
	arena.tables[tableIndex].entryCount++
	arena.noteLayoutInsertStopped(tableIndex, key)
	arena.tables[tableIndex].rawVersion = machineTableNextVersion(arena.tables[tableIndex].rawVersion)
	if wasDeleted {
		arena.tables[tableIndex].fieldTombstone--
	}
	return nil
}

func (arena *machineTableArena) getRecord(id machineTableID, key machineTableKey) (slot, bool) {
	record, ok := arena.lookupStopped(id)
	if !ok {
		return slotNil, false
	}
	fieldIndex, found := arena.findLiveField(record, key, machineTableHash(key))
	if !found {
		return slotNil, false
	}
	field := arena.fields[fieldIndex]
	if field.orderIndex >= record.orderLength {
		return slotNil, false
	}
	entry := arena.orders[int(record.orderOffset+field.orderIndex)]
	if entry.present == 0 || entry.value == slotNil || entry.key != key {
		return slotNil, false
	}
	return entry.value, true
}

func (arena *machineTableArena) lookup(id machineTableID) (machineTableRecord, bool) {
	record, ok := arena.lookupStopped(id)
	if !ok {
		return machineTableRecord{}, false
	}
	if record.arrayLength > record.arrayCapacity ||
		record.fieldCount > record.fieldCapacity ||
		uint64(record.entryCount) > uint64(record.arrayLength)+uint64(record.fieldCount) ||
		record.fieldTombstone > record.fieldCapacity-record.fieldCount ||
		record.orderLength > record.orderCapacity ||
		record.orderTombstone > record.orderLength ||
		!machineTableSpanValid(record.arrayOffset, record.arrayCapacity, len(arena.arrays)) ||
		!machineTableSpanValid(record.fieldOffset, record.fieldCapacity, len(arena.fields)) ||
		!machineTableSpanValid(record.orderOffset, record.orderCapacity, len(arena.orders)) ||
		(record.metatable != invalidMachineTableID && uint64(record.metatable) > uint64(len(arena.tables))) {
		return machineTableRecord{}, false
	}
	if record.layout != machineTableDynamicLayout && uint64(record.layout) > uint64(len(arena.layouts)) {
		return machineTableRecord{}, false
	}
	return record, true
}

func (arena *machineTableArena) ensureLayoutDomainStopped() {
	if arena == nil || len(arena.layouts) != 0 {
		return
	}
	arena.layouts = append(arena.layouts, machineTableLayout{})
	arena.layoutsByTransition = make(map[machineTableLayoutTransition]machineTableLayoutID)
}

func (arena *machineTableArena) noteLayoutInsertStopped(tableIndex int, key machineTableKey) {
	if arena == nil || tableIndex < 0 || tableIndex >= len(arena.tables) {
		return
	}
	record := &arena.tables[tableIndex]
	if record.layout == machineTableDynamicLayout {
		return
	}
	orderIndex, ok := arena.orderIndex(*record, key)
	if !ok {
		record.layout = machineTableDynamicLayout
		return
	}
	transition := machineTableLayoutTransition{parent: record.layout, key: key}
	if layout, ok := arena.layoutsByTransition[transition]; ok {
		if int(layout-1) >= len(arena.layouts) || arena.layouts[layout-1].offset != orderIndex {
			record.layout = machineTableDynamicLayout
			return
		}
		record.layout = layout
		return
	}
	if len(arena.layouts) >= machineTableLayoutLimit {
		record.layout = machineTableDynamicLayout
		return
	}
	layout := machineTableLayoutID(len(arena.layouts) + 1)
	arena.layouts = append(arena.layouts, machineTableLayout{
		parent: record.layout,
		key:    key,
		offset: orderIndex,
	})
	arena.layoutsByTransition[transition] = layout
	record.layout = layout
}

func (arena *machineTableArena) tableLayout(id machineTableID) (machineTableLayoutID, uint32, uint32, error) {
	record, ok := arena.lookup(id)
	if !ok {
		return machineTableDynamicLayout, 0, 0, arena.tableError()
	}
	return record.layout, record.rawVersion, record.metaVersion, nil
}

func (arena *machineTableArena) layoutPropertyOffset(layout machineTableLayoutID, key machineTableKey) (uint32, bool) {
	if arena == nil || layout == machineTableDynamicLayout {
		return 0, false
	}
	for layout > machineTableRootLayout {
		index := int(layout - 1)
		if index < 0 || index >= len(arena.layouts) {
			return 0, false
		}
		record := arena.layouts[index]
		if record.key == key {
			return record.offset, true
		}
		layout = record.parent
	}
	return 0, false
}

func (arena *machineTableArena) getAtLayoutOffset(id machineTableID, layout machineTableLayoutID, key machineTableKey, offset uint32) (slot, bool) {
	record, ok := arena.lookupStopped(id)
	if !ok || layout == machineTableDynamicLayout || record.layout != layout || offset >= record.orderLength {
		return slotNil, false
	}
	entry := arena.orders[int(record.orderOffset+offset)]
	if entry.present == 0 || entry.value == slotNil || entry.key != key {
		return slotNil, false
	}
	return entry.value, true
}

// lookupStopped is the hot internal lookup after a scalar table handle has
// crossed machine.tableID's fully validated boundary. Arena mutation preserves
// record invariants, so repeated raw operations only need the dense ID check.
func (arena *machineTableArena) lookupStopped(id machineTableID) (machineTableRecord, bool) {
	if arena == nil || arena.closed || id == invalidMachineTableID {
		return machineTableRecord{}, false
	}
	index := uint64(id - 1)
	if index >= uint64(len(arena.tables)) {
		return machineTableRecord{}, false
	}
	return arena.tables[index], true
}

func (arena *machineTableArena) tableIndex(id machineTableID) (int, bool) {
	if _, ok := arena.lookupStopped(id); !ok {
		return 0, false
	}
	return int(id - 1), true
}

func (arena *machineTableArena) tableError() error {
	if arena == nil || arena.closed {
		return errMachineTableArenaClosed
	}
	return errMachineTableInvalidID
}

func (arena *machineTableArena) ensureArrayStopped(tableIndex int, needed uint32) error {
	record := arena.tables[tableIndex]
	if needed <= record.arrayCapacity {
		return nil
	}
	capacity, err := machineTableGrowCapacity(record.arrayCapacity, needed, 4)
	if err != nil || !machineTableSpanFits(arena.arrayCursor, capacity) {
		return errors.New("machine table array capacity overflows uint32")
	}
	offset := arena.reserveArraysStopped(capacity)
	copy(arena.arrays[int(offset):int(offset+record.arrayLength)],
		arena.arrays[int(record.arrayOffset):int(record.arrayOffset+record.arrayLength)])
	arena.tables[tableIndex].arrayOffset = offset
	arena.tables[tableIndex].arrayCapacity = capacity
	return nil
}

func (arena *machineTableArena) ensureOrderStopped(tableIndex int, needed uint32) error {
	record := arena.tables[tableIndex]
	if needed <= record.orderCapacity {
		return nil
	}
	capacity, err := machineTableGrowCapacity(record.orderCapacity, needed, 4)
	if err != nil || !machineTableSpanFits(arena.orderCursor, capacity) {
		return errors.New("machine table order capacity overflows uint32")
	}
	offset := arena.reserveOrdersStopped(capacity)
	copy(arena.orders[int(offset):int(offset+record.orderLength)],
		arena.orders[int(record.orderOffset):int(record.orderOffset+record.orderLength)])
	arena.tables[tableIndex].orderOffset = offset
	arena.tables[tableIndex].orderCapacity = capacity
	return nil
}

func (arena *machineTableArena) setOrderStopped(tableIndex int, key machineTableKey, value slot) error {
	record := arena.tables[tableIndex]
	if index, ok := arena.orderIndex(record, key); ok {
		entry := &arena.orders[int(record.orderOffset+index)]
		if entry.present == 0 {
			arena.tables[tableIndex].orderTombstone--
		}
		*entry = machineTableOrderEntry{key: key, value: value, present: 1}
		return nil
	}
	if err := arena.ensureOrderStopped(tableIndex, record.orderLength+1); err != nil {
		return err
	}
	record = arena.tables[tableIndex]
	index := record.orderLength
	arena.tables[tableIndex].orderLength++
	arena.orders[int(record.orderOffset+index)] = machineTableOrderEntry{key: key, value: value, present: 1}
	return nil
}

func (arena *machineTableArena) markOrderDeletedStopped(tableIndex int, key machineTableKey) {
	record := arena.tables[tableIndex]
	index, ok := arena.orderIndex(record, key)
	if !ok {
		return
	}
	entry := &arena.orders[int(record.orderOffset+index)]
	if entry.present == 0 {
		return
	}
	entry.value = slotNil
	entry.present = 0
	arena.tables[tableIndex].orderTombstone++
	arena.compactOrderStopped(tableIndex)
}

func (arena *machineTableArena) compactOrderStopped(tableIndex int) {
	record := arena.tables[tableIndex]
	if record.orderLength <= machineTableCompactOrderMinimum || record.orderTombstone*2 <= record.orderLength {
		return
	}
	write := uint32(0)
	for read := uint32(0); read < record.orderLength; read++ {
		entry := arena.orders[int(record.orderOffset+read)]
		if entry.present == 0 || entry.value == slotNil {
			continue
		}
		arena.orders[int(record.orderOffset+write)] = entry
		write++
	}
	clear(arena.orders[int(record.orderOffset+write):int(record.orderOffset+record.orderLength)])
	arena.tables[tableIndex].orderLength = write
	arena.tables[tableIndex].orderTombstone = 0
	if record.fieldCapacity != 0 {
		arena.rebuildFieldsStopped(tableIndex)
	}
}

func (arena *machineTableArena) orderIndex(record machineTableRecord, key machineTableKey) (uint32, bool) {
	// Dense arrays are normally inserted in index order, so their order entry
	// sits at index-1. Validate the hint before falling back because mixed keys,
	// deletion, and compaction can move it.
	if key.kind == machineTableKeyArray && key.id != 0 {
		hint := key.id - 1
		if hint < record.orderLength && arena.orders[int(record.orderOffset+hint)].key == key {
			return hint, true
		}
	}
	for index := uint32(0); index < record.orderLength; index++ {
		if arena.orders[int(record.orderOffset+index)].key == key {
			return index, true
		}
	}
	return 0, false
}

func (arena *machineTableArena) deletedOrderIndex(record machineTableRecord, key machineTableKey) (uint32, bool) {
	index, ok := arena.orderIndex(record, key)
	if !ok || arena.orders[int(record.orderOffset+index)].present != 0 {
		return 0, false
	}
	return index, true
}

func (arena *machineTableArena) liveOrderIndex(record machineTableRecord, key machineTableKey, hint uint32) (uint32, bool) {
	if hint < record.orderLength {
		entry := arena.orders[int(record.orderOffset+hint)]
		if entry.present != 0 && entry.value != slotNil && entry.key == key {
			return hint, true
		}
	}
	index, ok := arena.orderIndex(record, key)
	if !ok {
		return 0, false
	}
	entry := arena.orders[int(record.orderOffset+index)]
	return index, entry.present != 0 && entry.value != slotNil
}

func (arena *machineTableArena) ensureFieldsStopped(tableIndex int) error {
	record := arena.tables[tableIndex]
	capacity := record.fieldCapacity
	if capacity == 0 {
		capacity = machineTableInitialFieldCapacity
	} else if uint64(record.fieldCount+record.fieldTombstone+1)*4 <= uint64(capacity)*3 {
		return nil
	} else if uint64(record.fieldCount+1)*4 > uint64(capacity)*3 {
		if capacity > math.MaxUint32/2 {
			return errors.New("machine table field capacity overflows uint32")
		}
		capacity *= 2
	}
	return arena.rehashFieldsStopped(tableIndex, capacity)
}

func (arena *machineTableArena) rehashFieldsStopped(tableIndex int, capacity uint32) error {
	if capacity < machineTableInitialFieldCapacity || capacity&(capacity-1) != 0 ||
		!machineTableSpanFits(arena.fieldCursor, capacity) {
		return errors.New("machine table field capacity overflows uint32")
	}
	offset := arena.reserveFieldsStopped(capacity)
	arena.tables[tableIndex].fieldOffset = offset
	arena.tables[tableIndex].fieldCapacity = capacity
	arena.rebuildFieldsStopped(tableIndex)
	return nil
}

func (arena *machineTableArena) rebuildFieldsStopped(tableIndex int) {
	record := arena.tables[tableIndex]
	clear(arena.fields[int(record.fieldOffset):int(record.fieldOffset+record.fieldCapacity)])
	fresh := record
	fresh.fieldCount = 0
	fresh.fieldTombstone = 0
	for orderIndex := uint32(0); orderIndex < record.orderLength; orderIndex++ {
		entry := arena.orders[int(record.orderOffset+orderIndex)]
		if entry.present == 0 || entry.value == slotNil ||
			(entry.key.kind != machineTableKeySlot && entry.key.kind != machineTableKeyString) {
			continue
		}
		fieldIndex, _ := arena.findInsertField(fresh, entry.key, machineTableHash(entry.key))
		arena.fields[fieldIndex] = machineTableField{
			hash:       machineTableHash(entry.key),
			orderIndex: orderIndex,
			state:      machineTableFieldLive,
		}
		fresh.fieldCount++
	}
	arena.tables[tableIndex].fieldCount = fresh.fieldCount
	arena.tables[tableIndex].fieldTombstone = 0
}

func (arena *machineTableArena) findLiveField(record machineTableRecord, key machineTableKey, hash uint64) (int, bool) {
	if record.fieldCapacity == 0 {
		return 0, false
	}
	mask := record.fieldCapacity - 1
	for probe := uint32(0); probe < record.fieldCapacity; probe++ {
		index := record.fieldOffset + (uint32(hash)+probe)&mask
		field := arena.fields[int(index)]
		if field.state == machineTableFieldEmpty {
			return 0, false
		}
		if field.state != machineTableFieldLive || field.hash != hash || field.orderIndex >= record.orderLength {
			continue
		}
		entry := arena.orders[int(record.orderOffset+field.orderIndex)]
		if entry.present != 0 && entry.key == key {
			return int(index), true
		}
	}
	return 0, false
}

func (arena *machineTableArena) findInsertField(record machineTableRecord, key machineTableKey, hash uint64) (int, bool) {
	mask := record.fieldCapacity - 1
	firstDeleted := uint32(math.MaxUint32)
	for probe := uint32(0); probe < record.fieldCapacity; probe++ {
		index := record.fieldOffset + (uint32(hash)+probe)&mask
		field := arena.fields[int(index)]
		switch field.state {
		case machineTableFieldEmpty:
			if firstDeleted != math.MaxUint32 {
				return int(firstDeleted), true
			}
			return int(index), false
		case machineTableFieldDeleted:
			if field.hash == hash && field.orderIndex < record.orderLength {
				entry := arena.orders[int(record.orderOffset+field.orderIndex)]
				if entry.key == key {
					return int(index), true
				}
			}
			if firstDeleted == math.MaxUint32 {
				firstDeleted = index
			}
		}
	}
	if firstDeleted != math.MaxUint32 {
		return int(firstDeleted), true
	}
	panic("machine table field index is full")
}

func machineTableHash(key machineTableKey) uint64 {
	value := uint64(key.value)
	if key.kind != machineTableKeySlot {
		value = uint64(key.id)
	}
	value ^= uint64(key.kind) * 0x9e3779b97f4a7c15
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	return value ^ value>>31
}

func machineTableFieldCapacity(entries uint32) (uint32, error) {
	if entries == 0 {
		return 0, nil
	}
	capacity := machineTableInitialFieldCapacity
	for uint64(entries)*4 > uint64(capacity)*3 {
		if capacity > math.MaxUint32/2 {
			return 0, errors.New("machine table field capacity overflows uint32")
		}
		capacity *= 2
	}
	return capacity, nil
}

func machineTableGrowCapacity(current, needed, minimum uint32) (uint32, error) {
	capacity := current
	if capacity < minimum {
		capacity = minimum
	}
	for capacity < needed {
		if capacity > math.MaxUint32/2 {
			return 0, errors.New("machine table capacity overflows uint32")
		}
		capacity *= 2
	}
	return capacity, nil
}

func machineTableSpanFits(length int, capacity uint32) bool {
	return uint64(length)+uint64(capacity) <= math.MaxUint32
}

func (arena *machineTableArena) reserveArraysStopped(capacity uint32) uint32 {
	offset := arena.arrayCursor
	end := offset + int(capacity)
	if end > len(arena.arrays) {
		arena.arrays = append(arena.arrays, make([]slot, end-len(arena.arrays))...)
	}
	arena.arrayCursor = end
	return uint32(offset)
}

func (arena *machineTableArena) reserveFieldsStopped(capacity uint32) uint32 {
	offset := arena.fieldCursor
	end := offset + int(capacity)
	if end > len(arena.fields) {
		arena.fields = append(arena.fields, make([]machineTableField, end-len(arena.fields))...)
	}
	arena.fieldCursor = end
	return uint32(offset)
}

func (arena *machineTableArena) reserveOrdersStopped(capacity uint32) uint32 {
	offset := arena.orderCursor
	end := offset + int(capacity)
	if end > len(arena.orders) {
		arena.orders = append(arena.orders, make([]machineTableOrderEntry, end-len(arena.orders))...)
	}
	arena.orderCursor = end
	return uint32(offset)
}

func machineTableSpanValid(offset, capacity uint32, length int) bool {
	return uint64(offset)+uint64(capacity) <= uint64(length)
}
