package ember

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"sync/atomic"
	"unsafe"
)

// ValueKind names the kind of data stored in a Value.
type ValueKind uint8

const (
	// NilKind is the kind for the Luau nil value.
	NilKind ValueKind = iota
	// BoolKind is the kind for Luau boolean values.
	BoolKind
	// NumberKind is the kind for Luau number values.
	NumberKind
	// StringKind is the kind for Luau string values.
	StringKind
	// TableKind is the kind for Luau table values.
	TableKind
	// UserDataKind is the kind for opaque Go host values.
	UserDataKind
	// FunctionKind is the kind for Ember script function values.
	FunctionKind
	// HostFuncKind is the kind for Go host callback values.
	HostFuncKind
)

// String returns the Luau-facing name of this kind.
func (k ValueKind) String() string {
	switch k {
	case NilKind:
		return "nil"
	case BoolKind:
		return "boolean"
	case NumberKind:
		return "number"
	case StringKind:
		return "string"
	case TableKind:
		return "table"
	case UserDataKind:
		return "userdata"
	case FunctionKind:
		return "function"
	case HostFuncKind:
		return "host_function"
	default:
		return "unknown"
	}
}

// HostFunc is a Go callback callable from Ember scripts.
type HostFunc func(args []Value) ([]Value, error)

// ContextHostFunc is a Go callback callable from Ember scripts with the active
// runtime context.
type ContextHostFunc func(context.Context, []Value) ([]Value, error)

type nativeFunc func(globals *globalEnv, args []Value) ([]Value, error)
type yieldableHostFunc func(globals *globalEnv, args []Value) vmHostCallResult

type nativeFuncID uint8

const (
	nativeFuncUnknown nativeFuncID = iota
	nativeFuncSelect
	nativeFuncTableInsert
	nativeFuncTableRemove
	nativeFuncCoroutineStatus
	nativeFuncCoroutineResume
	nativeFuncMathMin
	nativeFuncRawLen
	nativeFuncToString
	nativeFuncNext
	nativeFuncArrayNext
	nativeFuncTableNext
	nativeFuncSetMetatable
	nativeFuncGetMetatable
	nativeFuncCoroutineCreate
	nativeFuncCoroutineYield
)

// Value is an Ember runtime value.
type Value struct {
	ref  unsafe.Pointer
	bits uint64
}

// Scalar values keep their payload in bits while retaining a real, GC-visible
// pointer in ref.  Distinct non-zero sentinels make the zero Value available
// for nil and avoid stealing bits from a complete float64 payload.
type valueScalarSentinel struct{ marker byte }

var (
	numberValueSentinel    = valueScalarSentinel{marker: 1}
	numberValueSentinelRef = unsafe.Pointer(&numberValueSentinel)
	boolValueSentinel      = valueScalarSentinel{marker: 2}
	boolValueSentinelRef   = unsafe.Pointer(&boolValueSentinel)
)

const (
	valueKindBits                   = uint64(0xff)
	valueNativeIDShift              = 8
	valueNativeIDBits               = uint64(0xff) << valueNativeIDShift
	valueTransientScriptCallableTag = uint64(1) << 8
	valueMachineCoroutineTag        = uint64(1) << 8
)

func valueKind(v Value) ValueKind {
	switch v.ref {
	case numberValueSentinelRef:
		return NumberKind
	case boolValueSentinelRef:
		return BoolKind
	case nil:
		if v.bits == 0 {
			return NilKind
		}
	}
	return ValueKind(v.bits & valueKindBits)
}

func valueNumber(v Value) float64 {
	return math.Float64frombits(v.bits)
}

//go:nocheckptr
func valueFloat64Bits(number float64) uint64 {
	return math.Float64bits(number)
}

func valueBool(v Value) bool {
	return v.bits != 0
}

func valueNativeID(v Value) nativeFuncID {
	if valueKind(v) != HostFuncKind {
		return nativeFuncUnknown
	}
	return nativeFuncID((v.bits & valueNativeIDBits) >> valueNativeIDShift)
}

func valueRef(v Value) unsafe.Pointer {
	return v.ref
}

func valueWithRef(kind ValueKind, ref unsafe.Pointer) Value {
	return Value{ref: ref, bits: uint64(kind)}
}

func valueWithRefAndNativeID(kind ValueKind, ref unsafe.Pointer, id nativeFuncID) Value {
	return Value{ref: ref, bits: uint64(kind) | uint64(id)<<valueNativeIDShift}
}

type stringBox struct {
	text string
	hash uint64
}

// scriptCallableHandle is an engine-neutral scalar reference to a transient
// script call target. The owning execution engine remains responsible for
// validating that the indexed generation is live.
type scriptCallableHandle struct {
	owner      uint64
	index      uint32
	generation uint32
}

type transientScriptCallablePayload struct {
	handle scriptCallableHandle
}

// machineCoroutineValuePayload carries an owner-bearing handle across the
// public Value boundary. Compact slots deliberately omit the owner cookie;
// the receiving Machine controller restores and validates it before use.
type machineCoroutineValuePayload struct {
	handle machineCoroutineHandle
}

type scriptCallableHandleValidator func(scriptCallableHandle) bool

var (
	errScriptCallableValueInvalid      = errors.New("script callable: invalid value")
	errScriptCallableValueCrossOwner   = errors.New("script callable: cross-owner value")
	errScriptCallableValueStale        = errors.New("script callable: stale value")
	errMachineCoroutineValueInvalid    = errors.New("machine coroutine: invalid value")
	errMachineCoroutineValueCrossOwner = errors.New("machine coroutine: cross-owner value")
	errMachineCoroutineValueStale      = errors.New("machine coroutine: stale value")
)

type hostCallable struct {
	hostFunc      HostFunc
	contextHost   ContextHostFunc
	native        nativeFunc
	yieldableHost yieldableHostFunc
}

// vmStackOwner gives stack-backed cells a stable identity across stack
// growth.  The values slice may be replaced when the stack grows, but the
// owner itself remains stable until all of its frames have been released.
// Cells address values by absolute index rather than by a pointer into the
// current backing array.
type vmStackOwner struct {
	values []Value
	// thread is set for the active shared owner so stack-backed range cleanup
	// can keep the thread's short-lived stack view synchronized with values.
	// It is not a semantic root; the thread already owns the stack owner.
	thread *vmThread
}

type cell struct {
	value                Value
	owner                *vmStackOwner
	index                int
	runtimeObjectCharged bool
}

func (c *cell) get() Value {
	if c == nil {
		return NilValue()
	}
	if c.owner != nil && c.index >= 0 && c.index < len(c.owner.values) {
		return c.owner.values[c.index]
	}
	return c.value
}

func (c *cell) set(value Value) {
	if c == nil {
		return
	}
	c.value = value
	if c.owner != nil && c.index >= 0 && c.index < len(c.owner.values) {
		c.owner.values[c.index] = value
	}
}

func (c *cell) openAt(owner *vmStackOwner, index int) {
	if c == nil {
		return
	}
	c.owner = owner
	c.index = index
	if owner != nil && index >= 0 && index < len(owner.values) {
		c.value = owner.values[index]
		return
	}
	c.index = -1
	c.value = NilValue()
}

// close copies the live stack value into the cell and releases the stack
// owner.  A closed cell is safe for a closure to retain after its creator's
// frame window is truncated.
func (c *cell) close() {
	if c == nil {
		return
	}
	if c.owner != nil && c.index >= 0 && c.index < len(c.owner.values) {
		c.value = c.owner.values[c.index]
	}
	c.owner = nil
	c.index = -1
}

type closure struct {
	proto               *Proto
	upvalues            []*cell
	upvalueValues       []Value
	upvalueValueOK      []bool
	inlineUpvalues      [2]*cell
	inlineUpvalueValues [2]Value
	inlineUpvalueOK     [2]bool
}

// UserData is an opaque Go-owned host object passed through Ember scripts.
type UserData struct {
	id      uint64
	payload any
}

// Table is a Luau table object.
type Table struct {
	array        []Value
	arrayHasNil  bool
	stringFields []tableStringField
	inlineFields *[tableInlineStringFieldCapacity]tableStringField
	shape        *tableShape
	metatable    *Table
	cold         *tableCold
	iteration    *tableIterationJournal
	// Layout versions track key/storage changes; value versions track stored value
	// changes for each independent table storage family.
	stringVersion       uint32
	stringValueVersion  uint32
	arrayVersion        uint32
	arrayValueVersion   uint32
	genericVersion      uint32
	genericValueVersion uint32
	entryCount          uint64
}

type tableStorage struct {
	table        Table
	inlineFields [tableInlineStringFieldCapacity]tableStringField
}

type tableArrayStorage struct {
	table        Table
	inlineArray  [tableInlineArrayCapacity]Value
	inlineFields [tableInlineStringFieldCapacity]tableStringField
}

type tableCold struct {
	id              uint64
	indexCache      tableMetamethodCache
	newIndexCache   tableMetamethodCache
	stringHashCount int
	fields          tableHashFields
}

// tableMetamethodCache is a live lookup record for one metatable field.  A
// positive record points at a stable table slot; a negative record remembers
// that the field was absent for the guarded shape.  Neither form copies the
// fallback Value, so an in-place update is visible on the next read.
type tableMetamethodCache struct {
	metatable *Table
	slot      tableStringFieldSlot
	ready     bool
	negative  bool
}

type tableHashFields struct {
	entries    []tableHashEntry
	count      int
	tombstones int
	generation uint64
}

type tableHashEntry struct {
	key   tableKey
	hash  uint64
	value Value
	state uint8
}

const tableHashInitialCapacity = 8

type tableStringField struct {
	// key is retained for the VM's existing string-key fast paths and for
	// host-facing raw string adapters.  box preserves the original String
	// Value identity when the field was written through a Value key.
	key   string
	box   *stringBox
	value Value
}

type tableIterationKey struct {
	key     tableKey
	present bool
}

type tableIterationJournal struct {
	keys       []tableIterationKey
	index      map[tableKey]int
	tombstones int
}

type tableStringFieldSlot struct {
	index   int
	token   tableStringShapeToken
	key     *stringBox
	keyText string
	keyHash uint64
}

type rowStringFieldSlotRef struct {
	index int
}

func rowStringFieldSlotRefFromIndex(index int) rowStringFieldSlotRef {
	return rowStringFieldSlotRef{index: index}
}

func (ref rowStringFieldSlotRef) valid() bool {
	return ref.index >= 0
}

type tableShapeToken struct {
	stringLayout  uint32
	stringValues  uint32
	stringStorage uint8
	arrayLayout   uint32
	arrayValues   uint32
	genericLayout uint32
	genericValues uint32
	metatable     *Table
}

type tableStringShapeToken struct {
	metatable         *Table
	layout            uint32
	values            uint32
	storage           uint8
	storageGeneration uint64
}

func (left tableStringShapeToken) sameLayout(right tableStringShapeToken) bool {
	return left.layout == right.layout &&
		left.storage == right.storage &&
		left.storageGeneration == right.storageGeneration &&
		left.metatable == right.metatable
}

func (left tableStringShapeToken) sameValues(right tableStringShapeToken) bool {
	return left.values == right.values && left.sameLayout(right)
}

func (token tableStringShapeToken) matchesTableLayout(table *Table) bool {
	if table == nil ||
		token.layout != table.stringVersion ||
		token.metatable != table.metatable {
		return false
	}
	currentStorage := table.stringStorageKind()
	if token.storage != currentStorage {
		return false
	}
	if currentStorage == 1 {
		fields := table.hashFields()
		if fields == nil {
			return false
		}
		return token.storageGeneration == fields.generation
	}
	return token.storageGeneration == 0
}

func (token tableStringShapeToken) matchesTableValues(table *Table) bool {
	return table != nil &&
		token.values == table.stringValueVersion &&
		token.matchesTableLayout(table)
}

func (left tableShapeToken) sameStringLayout(right tableShapeToken) bool {
	return left.stringLayout == right.stringLayout &&
		left.stringStorage == right.stringStorage &&
		left.metatable == right.metatable
}

func (left tableShapeToken) sameStringValues(right tableShapeToken) bool {
	return left.stringValues == right.stringValues && left.sameStringLayout(right)
}

func (left tableShapeToken) sameArrayLayout(right tableShapeToken) bool {
	return left.arrayLayout == right.arrayLayout &&
		left.metatable == right.metatable
}

func (left tableShapeToken) sameArrayValues(right tableShapeToken) bool {
	return left.arrayValues == right.arrayValues && left.sameArrayLayout(right)
}

func (left tableShapeToken) sameGenericLayout(right tableShapeToken) bool {
	return left.genericLayout == right.genericLayout &&
		left.metatable == right.metatable
}

func (left tableShapeToken) sameGenericValues(right tableShapeToken) bool {
	return left.genericValues == right.genericValues && left.sameGenericLayout(right)
}

func (left tableShapeToken) sameMetatable(right tableShapeToken) bool {
	return left.metatable == right.metatable
}

func (t *Table) shapeToken() tableShapeToken {
	if t == nil {
		return tableShapeToken{}
	}
	stringToken := t.stringShapeToken()
	return tableShapeToken{
		stringLayout:  stringToken.layout,
		stringValues:  stringToken.values,
		stringStorage: stringToken.storage,
		arrayLayout:   t.arrayVersion,
		arrayValues:   t.arrayValueVersion,
		genericLayout: t.genericVersion,
		genericValues: t.genericValueVersion,
		metatable:     t.metatable,
	}
}

func (t *Table) stringShapeToken() tableStringShapeToken {
	if t == nil {
		return tableStringShapeToken{}
	}
	storage := t.stringStorageKind()
	var storageGeneration uint64
	if storage == 1 {
		if fields := t.hashFields(); fields != nil {
			storageGeneration = fields.generation
		}
	}
	return tableStringShapeToken{
		metatable:         t.metatable,
		layout:            t.stringVersion,
		values:            t.stringValueVersion,
		storage:           storage,
		storageGeneration: storageGeneration,
	}
}

const maxInlineStringFields = 8
const tableInlineArrayCapacity = 2
const tableInlineStringFieldCapacity = 2

type tableKey struct {
	kind     ValueKind
	bool     bool
	number   float64
	str      string
	strBox   *stringBox
	strHash  uint64
	table    *Table
	userdata *UserData
}

const (
	tableHashEmpty uint8 = iota
	tableHashFull
	tableHashDeleted
)

func (fields *tableHashFields) len() int {
	if fields == nil {
		return 0
	}
	return fields.count
}

func (fields *tableHashFields) get(key tableKey) (Value, bool) {
	if fields == nil || fields.count == 0 || len(fields.entries) == 0 {
		return NilValue(), false
	}
	index, ok := fields.find(key)
	if !ok {
		return NilValue(), false
	}
	value := fields.entries[index].value
	if value.IsNil() {
		return NilValue(), false
	}
	return value, true
}

func (fields *tableHashFields) has(key tableKey) bool {
	_, ok := fields.get(key)
	return ok
}

func (fields *tableHashFields) set(key tableKey, value Value) bool {
	if value.IsNil() {
		return fields.delete(key)
	}
	if len(fields.entries) == 0 {
		fields.grow()
	}
	index, ok := fields.findInsert(key)
	if ok {
		fields.entries[index].value = value
		return false
	}
	if (fields.count+fields.tombstones+1)*4 >= len(fields.entries)*3 {
		fields.grow()
		index, _ = fields.findInsert(key)
	}
	if fields.entries[index].state == tableHashDeleted {
		fields.tombstones--
	}
	fields.entries[index] = tableHashEntry{key: key, hash: key.hash(), value: value, state: tableHashFull}
	fields.count++
	fields.generation++
	return true
}

func (fields *tableHashFields) delete(key tableKey) bool {
	if fields == nil || fields.count == 0 || len(fields.entries) == 0 {
		return false
	}
	index, ok := fields.find(key)
	if !ok {
		return false
	}
	fields.entries[index].key = tableKey{}
	fields.entries[index].hash = 0
	fields.entries[index].value = NilValue()
	fields.entries[index].state = tableHashDeleted
	fields.count--
	fields.tombstones++
	fields.generation++
	return true
}

func (fields *tableHashFields) grow() {
	next := tableHashInitialCapacity
	if len(fields.entries) > 0 {
		next = len(fields.entries) * 2
	}
	old := fields.entries
	fields.entries = make([]tableHashEntry, next)
	fields.count = 0
	fields.tombstones = 0
	fields.generation++
	for _, entry := range old {
		if entry.state == tableHashFull && !entry.value.IsNil() {
			index, ok := fields.findInsert(entry.key)
			if ok {
				fields.entries[index].value = entry.value
				continue
			}
			fields.entries[index] = tableHashEntry{
				key:   entry.key,
				hash:  entry.hash,
				value: entry.value,
				state: tableHashFull,
			}
			fields.count++
		}
	}
}

func (fields *tableHashFields) find(key tableKey) (int, bool) {
	if fields == nil || fields.count == 0 || len(fields.entries) == 0 {
		return 0, false
	}
	mask := uint64(len(fields.entries) - 1)
	keyHash := key.hash()
	index := int(keyHash & mask)
	for probe := 0; probe < len(fields.entries); probe++ {
		entry := fields.entries[index]
		switch entry.state {
		case tableHashEmpty:
			return 0, false
		case tableHashFull:
			if entry.hash == keyHash && tableKeysEqual(entry.key, key) {
				return index, true
			}
		}
		index = (index + 1) & int(mask)
	}
	return 0, false
}

func (fields *tableHashFields) findInsert(key tableKey) (int, bool) {
	if fields == nil || len(fields.entries) == 0 {
		return 0, false
	}
	mask := uint64(len(fields.entries) - 1)
	keyHash := key.hash()
	index := int(keyHash & mask)
	firstDeleted := -1
	for probe := 0; probe < len(fields.entries); probe++ {
		entry := fields.entries[index]
		switch entry.state {
		case tableHashEmpty:
			if firstDeleted >= 0 {
				return firstDeleted, false
			}
			return index, false
		case tableHashDeleted:
			if firstDeleted < 0 {
				firstDeleted = index
			}
		case tableHashFull:
			if entry.hash == keyHash && tableKeysEqual(entry.key, key) {
				return index, true
			}
		}
		index = (index + 1) & int(mask)
	}
	return firstDeleted, false
}

func (fields *tableHashFields) forEach(fn func(tableKey, Value)) {
	if fields == nil || fields.count == 0 {
		return
	}
	for _, entry := range fields.entries {
		if entry.state == tableHashFull && !entry.value.IsNil() {
			fn(entry.key, entry.value)
		}
	}
}

func (key tableKey) hash() uint64 {
	hash := uint64(key.kind) + 0x9e3779b97f4a7c15
	switch key.kind {
	case BoolKind:
		if key.bool {
			return hash ^ 0x100000001b3
		}
		return hash
	case NumberKind:
		number := key.number
		if number == 0 {
			// Luau numeric equality considers -0 and +0 equal, so they must
			// select the same hash bucket as well.
			number = 0
		}
		return hash ^ math.Float64bits(number)
	case StringKind:
		return hash ^ key.stringHash()
	case TableKind:
		return hash ^ uintptrHash(uintptr(unsafe.Pointer(key.table)))
	case UserDataKind:
		return hash ^ key.userdata.id
	default:
		return hash
	}
}

func (key tableKey) stringHash() uint64 {
	if key.strHash != 0 {
		return key.strHash
	}
	if key.strBox != nil {
		return key.strBox.hash
	}
	return hashString(key.str)
}

func tableKeysEqual(left, right tableKey) bool {
	if left.kind != right.kind {
		return false
	}
	switch left.kind {
	case StringKind:
		return tableStringProbesEqual(tableStringProbeFromKey(left), tableStringProbeFromKey(right))
	default:
		return left.bool == right.bool && left.number == right.number &&
			left.table == right.table && left.userdata == right.userdata
	}
}

func tableStringProbesEqual(left, right tableStringProbe) bool {
	if left.box != nil && right.box != nil && left.box == right.box {
		return true
	}
	leftHash := hashStringIfNeeded(left.hash, left.text)
	rightHash := hashStringIfNeeded(right.hash, right.text)
	if leftHash != rightHash || len(left.text) != len(right.text) {
		return false
	}
	return left.text == right.text
}

func uintptrHash(value uintptr) uint64 {
	hash := uint64(value)
	hash ^= hash >> 33
	hash *= 0xff51afd7ed558ccd
	hash ^= hash >> 33
	hash *= 0xc4ceb9fe1a85ec53
	hash ^= hash >> 33
	return hash
}

var nextRuntimeObjectID atomic.Uint64

// NilValue returns the Luau nil value.
func NilValue() Value {
	return Value{}
}

// BoolValue returns a Luau boolean value.
func BoolValue(b bool) Value {
	var bits uint64
	if b {
		bits = 1
	}
	return Value{ref: boolValueSentinelRef, bits: bits}
}

// NumberValue returns a Luau number value.
func NumberValue(n float64) Value {
	return Value{ref: numberValueSentinelRef, bits: valueFloat64Bits(n)}
}

// StringValue returns a Luau string value.
func StringValue(s string) Value {
	return stringValueFromBox(newStringBox(s))
}

func newStringBox(s string) *stringBox {
	return &stringBox{text: s, hash: hashString(s)}
}

// tableStringProbe is the normalized key form used by inline fields and hash
// probes.  A probe may carry a boxed String Value (the hot path) or only host
// text (raw string adapters).  The latter computes its hash once at the
// boundary and never allocates a box merely to look up a field.
type tableStringProbe struct {
	box  *stringBox
	text string
	hash uint64
}

func tableStringProbeFromText(text string) tableStringProbe {
	return tableStringProbe{text: text, hash: hashString(text)}
}

func tableStringProbeFromBox(box *stringBox) tableStringProbe {
	if box == nil {
		return tableStringProbe{}
	}
	return tableStringProbe{box: box, text: box.text, hash: box.hash}
}

func tableStringProbeFromKey(key tableKey) tableStringProbe {
	if key.strBox != nil {
		return tableStringProbeFromBox(key.strBox)
	}
	return tableStringProbe{text: key.str, hash: key.stringHash()}
}

func stringBoxHash(box *stringBox, text string) uint64 {
	if box != nil {
		return box.hash
	}
	return hashString(text)
}

func (probe tableStringProbe) matchesBox(box *stringBox, text string) bool {
	if probe.box != nil && box != nil && probe.box == box {
		return true
	}
	probeHash := hashStringIfNeeded(probe.hash, probe.text)
	if len(probe.text) != len(text) {
		return false
	}
	if box != nil {
		if box.hash != probeHash || len(box.text) != len(probe.text) {
			return false
		}
		return box.text == probe.text
	}
	return text == probe.text
}

func hashStringIfNeeded(hash uint64, text string) uint64 {
	if hash != 0 || text == "" {
		return hash
	}
	return hashString(text)
}

func stringValueFromBox(box *stringBox) Value {
	return valueWithRef(StringKind, unsafe.Pointer(box))
}

func hashString(s string) uint64 {
	const (
		offset uint64 = 14695981039346656037
		prime  uint64 = 1099511628211
	)
	hash := offset
	for i := 0; i < len(s); i++ {
		hash ^= uint64(s[i])
		hash *= prime
	}
	return hash
}

// HostFuncValue returns a Go host callback value.
func HostFuncValue(fn HostFunc) Value {
	return valueWithRef(HostFuncKind, unsafe.Pointer(&hostCallable{hostFunc: fn}))
}

// ContextHostFuncValue returns a Go host callback value that receives the
// active runtime context when called.
func ContextHostFuncValue(fn ContextHostFunc) Value {
	callable := &hostCallable{contextHost: fn}
	callable.native = func(globals *globalEnv, args []Value) ([]Value, error) {
		ctx := contextFromGlobalEnv(globals)
		if scope, ok := invocationScopeFromGlobalEnv(globals); ok {
			ctx = contextWithInvocationScope(ctx, scope)
		}
		return fn(ctx, ownedHostArgs(args))
	}
	return valueWithRef(HostFuncKind, unsafe.Pointer(callable))
}

// ownedHostArgs creates the escape-barrier copy passed to public host
// callbacks. VM register windows are reusable and may be cleared or reused as
// soon as a callback returns; retaining or mutating a borrowed slice must not
// affect script registers or a later invocation.
func ownedHostArgs(args []Value) []Value {
	if len(args) == 0 {
		return nil
	}
	owned := make([]Value, len(args))
	copy(owned, args)
	return owned
}

func nativeFuncValue(fn nativeFunc) Value {
	return nativeFuncValueWithID(fn, nativeFuncUnknown)
}

func nativeFuncValueWithID(fn nativeFunc, id nativeFuncID) Value {
	if id != nativeFuncUnknown {
		return valueWithRefAndNativeID(HostFuncKind, nil, id)
	}
	return valueWithRefAndNativeID(HostFuncKind, unsafe.Pointer(&hostCallable{native: fn}), id)
}

func yieldableHostFuncValue(fn yieldableHostFunc) Value {
	return valueWithRef(HostFuncKind, unsafe.Pointer(&hostCallable{yieldableHost: fn}))
}

// NewUserData returns an opaque host object carrying payload.
func NewUserData(payload any) *UserData {
	return &UserData{
		id:      nextRuntimeObjectID.Add(1),
		payload: payload,
	}
}

// Payload returns the Go value carried by this userdata object.
func (u *UserData) Payload() any {
	if u == nil {
		return nil
	}
	return u.payload
}

// UserDataValue returns a Luau userdata value backed by userdata.
func UserDataValue(userdata *UserData) Value {
	return valueWithRef(UserDataKind, unsafe.Pointer(userdata))
}

// NewTable returns an empty Luau table.
func NewTable() *Table {
	return newTableWithCapacity(0, 0)
}

func newTableWithCapacity(arrayCapacity int, fieldCapacity int) *Table {
	if arrayCapacity < 0 {
		arrayCapacity = 0
	}
	if fieldCapacity < 0 {
		fieldCapacity = 0
	}
	var table *Table
	if arrayCapacity > 0 && arrayCapacity <= tableInlineArrayCapacity {
		storage := newTableArrayStorage()
		table = &storage.table
		table.array = storage.inlineArray[:0:arrayCapacity]
	} else {
		table = newTableStorage()
		if arrayCapacity > 0 {
			table.array = make([]Value, 0, arrayCapacity)
		}
	}
	if fieldCapacity > maxInlineStringFields {
		table.coldData().fields.entries = make([]tableHashEntry, tableHashCapacity(fieldCapacity))
	} else if fieldCapacity > 0 && fieldCapacity <= tableInlineStringFieldCapacity {
		table.stringFields = table.inlineFields[:0]
	} else if fieldCapacity > 0 {
		table.stringFields = make([]tableStringField, 0, fieldCapacity)
	}
	return table
}

func tableHashCapacity(count int) int {
	capacity := 8
	for capacity*3 < count*4 {
		capacity *= 2
	}
	return capacity
}

func newTableStorage() *Table {
	storage := &tableStorage{}
	storage.table.inlineFields = &storage.inlineFields
	storage.table.resetShape()
	return &storage.table
}

func newTableArrayStorage() *tableArrayStorage {
	storage := &tableArrayStorage{}
	storage.table.inlineFields = &storage.inlineFields
	storage.table.resetShape()
	return storage
}

func tableInlineFields(table *Table) *[tableInlineStringFieldCapacity]tableStringField {
	if table.inlineFields != nil {
		return table.inlineFields
	}
	table.inlineFields = new([tableInlineStringFieldCapacity]tableStringField)
	return table.inlineFields
}

func (t *Table) coldData() *tableCold {
	if t.cold == nil {
		t.cold = &tableCold{}
	}
	return t.cold
}

func (t *Table) objectID() uint64 {
	if t == nil {
		return 0
	}
	cold := t.coldData()
	if cold.id == 0 {
		cold.id = nextRuntimeObjectID.Add(1)
	}
	return cold.id
}

func (t *Table) hashFields() *tableHashFields {
	if t == nil || t.cold == nil {
		return nil
	}
	return &t.cold.fields
}

func (t *Table) ensureHashFields() *tableHashFields {
	return &t.coldData().fields
}

func (t *Table) hashFieldCount() int {
	if fields := t.hashFields(); fields != nil {
		return fields.len()
	}
	return 0
}

func (t *Table) hasStringOverflow() bool {
	return t != nil && t.cold != nil && t.cold.stringHashCount > 0
}

func (t *Table) stringStorageKind() uint8 {
	if t != nil && t.hasStringOverflow() {
		return 1
	}
	return 0
}

// TableValue returns a Luau table value backed by table.
func TableValue(table *Table) Value {
	return valueWithRef(TableKind, unsafe.Pointer(table))
}

func functionValue(proto *Proto, upvalues []*cell) Value {
	return functionValueWithUpvalues(proto, upvalues, nil, nil)
}

func functionValueWithUpvalues(proto *Proto, upvalues []*cell, values []Value, valueOK []bool) Value {
	return closureFunctionValue(&closure{proto: proto, upvalues: upvalues, upvalueValues: values, upvalueValueOK: valueOK})
}

func closureFunctionValue(closure *closure) Value {
	return valueWithRef(FunctionKind, unsafe.Pointer(closure))
}

func transientScriptCallableValue(handle scriptCallableHandle) (Value, error) {
	if handle.owner == 0 || handle.index == 0 || handle.generation == 0 {
		return Value{}, errScriptCallableValueInvalid
	}
	payload := &transientScriptCallablePayload{handle: handle}
	return Value{
		ref:  unsafe.Pointer(payload),
		bits: uint64(FunctionKind) | valueTransientScriptCallableTag,
	}, nil
}

func decodeTransientScriptCallableValue(value Value, owner uint64, validate scriptCallableHandleValidator) (scriptCallableHandle, error) {
	if owner == 0 || valueKind(value) != FunctionKind ||
		value.bits != uint64(FunctionKind)|valueTransientScriptCallableTag || valueRef(value) == nil {
		return scriptCallableHandle{}, errScriptCallableValueInvalid
	}
	handle := (*transientScriptCallablePayload)(valueRef(value)).handle
	if handle.owner == 0 || handle.index == 0 || handle.generation == 0 {
		return scriptCallableHandle{}, errScriptCallableValueInvalid
	}
	if handle.owner != owner {
		return scriptCallableHandle{}, errScriptCallableValueCrossOwner
	}
	if validate == nil || !validate(handle) {
		return scriptCallableHandle{}, errScriptCallableValueStale
	}
	return handle, nil
}

func machineCoroutineValue(handle machineCoroutineHandle) (Value, error) {
	if handle.owner == 0 || handle.index == 0 || handle.generation == 0 {
		return Value{}, errMachineCoroutineValueInvalid
	}
	payload := &machineCoroutineValuePayload{handle: handle}
	return Value{
		ref:  unsafe.Pointer(payload),
		bits: uint64(UserDataKind) | valueMachineCoroutineTag,
	}, nil
}

func decodeMachineCoroutineValue(value Value, owner uint64, validate func(machineCoroutineHandle) bool) (machineCoroutineHandle, error) {
	if owner == 0 || valueKind(value) != UserDataKind ||
		value.bits != uint64(UserDataKind)|valueMachineCoroutineTag || valueRef(value) == nil {
		return machineCoroutineHandle{}, errMachineCoroutineValueInvalid
	}
	handle := (*machineCoroutineValuePayload)(valueRef(value)).handle
	if handle.owner == 0 || handle.index == 0 || handle.generation == 0 {
		return machineCoroutineHandle{}, errMachineCoroutineValueInvalid
	}
	if handle.owner != owner {
		return machineCoroutineHandle{}, errMachineCoroutineValueCrossOwner
	}
	if validate == nil || !validate(handle) {
		return machineCoroutineHandle{}, errMachineCoroutineValueStale
	}
	return handle, nil
}

// Kind returns the value kind.
func (v Value) Kind() ValueKind {
	return valueKind(v)
}

// IsNil returns whether this Value is nil.
func (v Value) IsNil() bool {
	return valueKind(v) == NilKind
}

// Bool returns the boolean value and whether this Value is a boolean.
func (v Value) Bool() (bool, bool) {
	if valueKind(v) != BoolKind {
		return false, false
	}
	return valueBool(v), true
}

// Number returns the numeric value and whether this Value is a number.
func (v Value) Number() (float64, bool) {
	if valueKind(v) != NumberKind {
		return 0, false
	}
	return valueNumber(v), true
}

// String returns the string value and whether this Value is a string.
func (v Value) String() (string, bool) {
	if valueKind(v) != StringKind {
		return "", false
	}
	box := v.stringBox()
	if box == nil {
		return "", false
	}
	return box.text, true
}

func (v Value) stringBox() *stringBox {
	if valueKind(v) != StringKind || valueRef(v) == nil {
		return nil
	}
	return (*stringBox)(valueRef(v))
}

func (v Value) stringText() string {
	box := v.stringBox()
	if box == nil {
		return ""
	}
	return box.text
}

func (v Value) stringHash() uint64 {
	box := v.stringBox()
	if box == nil {
		return 0
	}
	return box.hash
}

// Table returns the table object and whether this Value is a table.
func (v Value) Table() (*Table, bool) {
	table := v.tableRef()
	if table == nil {
		return nil, false
	}
	return table, true
}

func (v Value) tableRef() *Table {
	if valueKind(v) != TableKind || valueRef(v) == nil {
		return nil
	}
	return (*Table)(valueRef(v))
}

// UserData returns the userdata object and whether this Value is userdata.
func (v Value) UserData() (*UserData, bool) {
	userdata := v.userdataRef()
	if userdata == nil {
		return nil, false
	}
	return userdata, true
}

func (v Value) userdataRef() *UserData {
	if valueKind(v) != UserDataKind || v.bits != uint64(UserDataKind) || valueRef(v) == nil {
		return nil
	}
	return (*UserData)(valueRef(v))
}

// Get returns the table value stored at key, or nil when the key is missing.
func (t *Table) Get(key Value) (Value, error) {
	return publicTableAccess().get(t, key)
}

func (t *Table) rawGet(key Value) (Value, error) {
	if t == nil {
		return NilValue(), fmt.Errorf("table: nil table")
	}
	if index, ok := tableArrayIndexFromValue(key); ok && index <= len(t.array) {
		if value, ok := t.rawArrayValue(index); ok {
			return value, nil
		}
		return NilValue(), nil
	}
	storedKey, ok := tableKeyFromValue(key)
	if err := validateTableKey(key, ok); err != nil {
		return NilValue(), err
	}
	if storedKey.kind == StringKind {
		return t.rawGetStringKey(storedKey)
	}
	value, ok := t.rawGenericField(storedKey)
	if !ok {
		return NilValue(), nil
	}
	return value, nil
}

func (t *Table) rawGetString(key string) (Value, error) {
	if t == nil {
		return NilValue(), fmt.Errorf("table: nil table")
	}
	value, ok := t.rawStringFieldWithProbe(tableStringProbeFromText(key))
	if !ok {
		return NilValue(), nil
	}
	return value, nil
}

func (t *Table) rawGetStringKey(key tableKey) (Value, error) {
	if t == nil {
		return NilValue(), fmt.Errorf("table: nil table")
	}
	value, ok := t.rawStringFieldWithProbe(tableStringProbeFromKey(key))
	if !ok {
		return NilValue(), nil
	}
	return value, nil
}

func (t *Table) rawGetKey(key tableKey) (Value, error) {
	if t == nil {
		return NilValue(), fmt.Errorf("table: nil table")
	}
	if key.kind == StringKind {
		return t.rawGetStringKey(key)
	}
	value, ok := t.rawGenericField(key)
	if !ok {
		return NilValue(), nil
	}
	return value, nil
}

// Set stores value at key. Setting nil deletes the key.
func (t *Table) Set(key Value, value Value) error {
	return publicTableAccess().set(t, key, value)
}

func (t *Table) rawSet(key Value, value Value) error {
	return t.rawSetWithController(nil, key, value)
}

func (t *Table) rawSetWithController(controller *executionController, key Value, value Value) error {
	if t == nil {
		return fmt.Errorf("table: nil table")
	}
	if err := t.checkEntryQuotaWithController(controller, key, value); err != nil {
		return err
	}
	existed := false
	if current, err := t.rawGet(key); err == nil {
		existed = !current.IsNil()
	}
	if index, ok := tableArrayIndexFromValue(key); ok {
		err := t.rawSetArrayIndex(index, value)
		t.noteEntryMutation(key, value, existed)
		return err
	}
	storedKey, ok := tableKeyFromValue(key)
	if err := validateTableKey(key, ok); err != nil {
		return err
	}
	if storedKey.kind == StringKind {
		return t.rawSetStringKeyWithController(controller, storedKey, value)
	}
	t.setRawGenericField(storedKey, value)
	t.noteEntryMutation(key, value, existed)
	return nil
}

func (t *Table) checkEntryQuotaWithController(controller *executionController, key Value, value Value) error {
	if t == nil || controller == nil || controller.limits.MaxTableEntriesPerTable == 0 || value.IsNil() {
		return nil
	}
	limit := controller.limits.MaxTableEntriesPerTable
	current, err := t.rawGet(key)
	if err != nil {
		return err
	}
	if !current.IsNil() || t.entryCount < limit {
		return nil
	}
	return &LimitError{Kind: LimitTableEntriesPerTable, Limit: limit, Used: t.entryCount + 1}
}

func (t *Table) checkEntryQuota(key Value, value Value) error {
	return t.checkEntryQuotaWithController(nil, key, value)
}

func (t *Table) noteEntryMutation(key Value, value Value, existed bool) {
	if t == nil {
		return
	}
	if !existed && !value.IsNil() {
		t.entryCount++
	} else if existed && value.IsNil() && t.entryCount > 0 {
		t.entryCount--
	}
}

func (t *Table) rawSetString(key string, value Value) error {
	if t == nil {
		return fmt.Errorf("table: nil table")
	}
	old, _ := t.rawGetString(key)
	t.setRawStringField(key, value)
	t.noteEntryMutation(StringValue(key), value, !old.IsNil())
	return nil
}

func (t *Table) setRawStringFieldBoxWithController(controller *executionController, key string, box *stringBox, value Value) error {
	if t == nil {
		return fmt.Errorf("table: nil table")
	}
	if err := t.checkEntryQuotaWithController(controller, StringValue(key), value); err != nil {
		return err
	}
	old, _ := t.rawGetString(key)
	t.setRawStringFieldBox(key, box, value)
	t.noteEntryMutation(StringValue(key), value, !old.IsNil())
	return nil
}

func (t *Table) rawSetStringKey(key tableKey, value Value) error {
	return t.rawSetStringKeyWithController(nil, key, value)
}

func (t *Table) rawSetStringKeyWithController(controller *executionController, key tableKey, value Value) error {
	if t == nil {
		return fmt.Errorf("table: nil table")
	}
	if err := t.checkEntryQuotaWithController(controller, StringValue(key.str), value); err != nil {
		return err
	}
	old, _ := t.rawGetStringKey(key)
	t.setRawStringFieldBox(key.str, key.strBox, value)
	t.noteEntryMutation(StringValue(key.str), value, !old.IsNil())
	return nil
}

func (t *Table) rawSetKey(storedKey tableKey, value Value) error {
	return t.rawSetKeyWithController(nil, storedKey, value)
}

func tableKeyValue(key tableKey) Value {
	switch key.kind {
	case StringKind:
		return StringValue(key.str)
	case NumberKind:
		return NumberValue(key.number)
	case BoolKind:
		return BoolValue(key.bool)
	case TableKind:
		return TableValue(key.table)
	case UserDataKind:
		return UserDataValue(key.userdata)
	default:
		return NilValue()
	}
}

func (t *Table) rawSetKeyWithController(controller *executionController, storedKey tableKey, value Value) error {
	if t == nil {
		return fmt.Errorf("table: nil table")
	}
	if storedKey.kind == StringKind {
		return t.rawSetStringKeyWithController(controller, storedKey, value)
	}
	key := tableKeyValue(storedKey)
	if err := t.checkEntryQuotaWithController(controller, key, value); err != nil {
		return err
	}
	old, _ := t.rawGenericField(storedKey)
	t.setRawGenericField(storedKey, value)
	t.noteEntryMutation(key, value, !old.IsNil())
	return nil
}

func (t *Table) setRawGenericField(storedKey tableKey, value Value) {
	if value.IsNil() {
		t.deleteRawGenericField(storedKey)
		return
	}
	t.ensureIterationJournal()
	if added := t.ensureHashFields().set(storedKey, value); added {
		if storedKey.kind == StringKind {
			t.coldData().stringHashCount++
		}
		t.genericVersion++
		t.markIterationKeyPresent(storedKey)
	}
	t.genericValueVersion++
}

func (t *Table) deleteRawGenericField(storedKey tableKey) {
	t.deleteRawGenericFieldWithJournal(storedKey, true)
}

func (t *Table) deleteRawGenericFieldWithJournal(storedKey tableKey, markDeleted bool) {
	fields := t.hashFields()
	if fields == nil {
		return
	}
	if !fields.delete(storedKey) {
		return
	}
	if storedKey.kind == StringKind && t.cold != nil && t.cold.stringHashCount > 0 {
		t.cold.stringHashCount--
	}
	if markDeleted {
		t.markIterationKeyDeleted(storedKey)
	}
	t.genericVersion++
	t.genericValueVersion++
}

func (t *Table) rawSetArrayIndex(index int, value Value) error {
	key := tableKey{kind: NumberKind, number: float64(index)}
	if value.IsNil() {
		if index <= len(t.array) {
			if !t.array[index-1].IsNil() {
				t.array[index-1] = NilValue()
				t.arrayHasNil = true
				t.markIterationKeyDeleted(key)
				t.arrayVersion++
				t.arrayValueVersion++
				t.trimArray()
			}
		}
		t.deleteRawGenericField(key)
		return nil
	}
	if index <= len(t.array) {
		filledHole := t.array[index-1].IsNil()
		if filledHole {
			if t.needsJournalForArrayKey() {
				t.ensureIterationJournal()
			}
			if t.iteration != nil {
				t.markIterationKeyPresent(key)
			}
			t.arrayVersion++
		}
		t.array[index-1] = value
		if filledHole && t.arrayHasNil && !tableArrayHasNil(t.array) {
			t.arrayHasNil = false
		}
		t.arrayValueVersion++
		t.deleteRawGenericField(key)
		return nil
	}
	if index == len(t.array)+1 {
		if t.needsJournalForArrayKey() {
			t.ensureIterationJournal()
		}
		if t.iteration != nil {
			t.markIterationKeyPresent(key)
		}
		t.array = append(t.array, value)
		t.arrayVersion++
		t.arrayValueVersion++
		t.deleteRawGenericField(key)
		t.promoteContiguousArrayFields()
		return nil
	}
	t.ensureIterationJournal()
	t.setRawGenericField(key, value)
	return nil
}

func (t *Table) promoteContiguousArrayFields() {
	for {
		next := len(t.array) + 1
		key := tableKey{kind: NumberKind, number: float64(next)}
		value, ok := t.rawGenericField(key)
		if !ok {
			return
		}
		t.array = append(t.array, value)
		t.arrayVersion++
		t.arrayValueVersion++
		t.deleteRawGenericFieldWithJournal(key, false)
	}
}

func (t *Table) trimArray() {
	for len(t.array) > 0 && t.array[len(t.array)-1].IsNil() {
		t.array = t.array[:len(t.array)-1]
	}
	if t.arrayHasNil && !tableArrayHasNil(t.array) {
		t.arrayHasNil = false
	}
}

func (t *Table) setMetatable(metatable *Table) {
	if t == nil || t.metatable == metatable {
		return
	}
	t.metatable = metatable
	if t.cold != nil {
		t.cold.indexCache = tableMetamethodCache{}
		t.cold.newIndexCache = tableMetamethodCache{}
	}
}

func (t *Table) clearRawStorage() {
	if t == nil {
		return
	}
	t.entryCount = 0

	clear(t.array)
	t.array = nil
	t.arrayHasNil = false
	for i := range t.stringFields {
		t.stringFields[i] = tableStringField{}
	}
	t.stringFields = t.stringFields[:0]
	t.resetShape()

	if t.cold != nil {
		generation := t.cold.fields.generation + 1
		clear(t.cold.fields.entries)
		t.cold.fields = tableHashFields{generation: generation}
		t.cold.stringHashCount = 0
		t.cold.indexCache = tableMetamethodCache{}
		t.cold.newIndexCache = tableMetamethodCache{}
	}
	t.iteration = nil

	// table.clear is a structural deletion in every storage family. Advance
	// both layout and value generations so no live slot, shape token, sequence
	// fast path, or metamethod lookup can observe pre-clear state.
	t.stringVersion++
	t.stringValueVersion++
	t.arrayVersion++
	t.arrayValueVersion++
	t.genericVersion++
	t.genericValueVersion++
}

func (t *Table) rawLen() (int, error) {
	if t == nil {
		return 0, fmt.Errorf("table: nil table")
	}
	if !t.arrayHasNil {
		return len(t.array), nil
	}
	for i, value := range t.array {
		if value.IsNil() {
			return i, nil
		}
	}
	return len(t.array), nil
}

func tableArrayHasNil(values []Value) bool {
	for _, value := range values {
		if value.IsNil() {
			return true
		}
	}
	return false
}

func tableCanIterateCleanArray(table *Table) bool {
	return table != nil && table.metatable == nil && !table.arrayHasNil &&
		len(table.stringFields) == 0 && table.hashFieldCount() == 0
}

func (t *Table) rawNext(key Value) (Value, Value, error) {
	if t == nil {
		return NilValue(), NilValue(), fmt.Errorf("table: nil table")
	}
	if t.iteration == nil {
		return t.rawNextStorageOrder(key)
	}
	if key.IsNil() {
		return t.rawNextAfter(-1)
	}

	storedKey, ok := tableKeyFromValue(key)
	if err := validateTableKey(key, ok); err != nil {
		return NilValue(), NilValue(), err
	}
	index, ok := t.iterationKeyIndex(storedKey)
	if !ok || index < 0 || index >= len(t.iteration.keys) || !t.iteration.keys[index].present {
		return NilValue(), NilValue(), fmt.Errorf("invalid key")
	}
	return t.rawNextAfter(index)
}

func (t *Table) rawNextAfter(index int) (Value, Value, error) {
	journal := t.iteration
	for i := index + 1; i < len(journal.keys); i++ {
		entry := journal.keys[i]
		if !entry.present {
			continue
		}
		key := entry.key.value()
		value, err := t.rawGet(key)
		if err != nil {
			return NilValue(), NilValue(), err
		}
		if value.IsNil() {
			t.markIterationKeyDeleted(entry.key)
			continue
		}
		return key, value, nil
	}
	return NilValue(), NilValue(), nil
}

func (t *Table) rawNextStorageOrder(key Value) (Value, Value, error) {
	if key.IsNil() {
		if nextKey, value, ok := t.firstArrayIterationKey(0); ok {
			return nextKey, value, nil
		}
		if nextKey, value, ok := t.firstStringIterationKey(0); ok {
			return nextKey, value, nil
		}
		if t.hashFieldCount() != 0 {
			t.ensureIterationJournal()
			return t.rawNext(key)
		}
		return NilValue(), NilValue(), nil
	}

	storedKey, ok := tableKeyFromValue(key)
	if err := validateTableKey(key, ok); err != nil {
		return NilValue(), NilValue(), err
	}
	if storedKey.kind == NumberKind {
		index, ok := tableArrayIndexFromValue(key)
		if !ok || index > len(t.array) || t.array[index-1].IsNil() {
			return NilValue(), NilValue(), fmt.Errorf("invalid key")
		}
		if nextKey, value, ok := t.firstArrayIterationKey(index); ok {
			return nextKey, value, nil
		}
		if nextKey, value, ok := t.firstStringIterationKey(0); ok {
			return nextKey, value, nil
		}
		return NilValue(), NilValue(), nil
	}
	if storedKey.kind == StringKind && !t.hasStringOverflow() {
		probe := tableStringProbeFromKey(storedKey)
		for i := range t.stringFields {
			if t.stringFields[i].value.IsNil() || !probe.matchesBox(t.stringFields[i].box, t.stringFields[i].key) {
				continue
			}
			if nextKey, value, ok := t.firstStringIterationKey(i + 1); ok {
				return nextKey, value, nil
			}
			return NilValue(), NilValue(), nil
		}
		return NilValue(), NilValue(), fmt.Errorf("invalid key")
	}

	t.ensureIterationJournal()
	return t.rawNext(key)
}

func (t *Table) firstArrayIterationKey(start int) (Value, Value, bool) {
	for i := start; i < len(t.array); i++ {
		if t.array[i].IsNil() {
			continue
		}
		return NumberValue(float64(i + 1)), t.array[i], true
	}
	return NilValue(), NilValue(), false
}

func (t *Table) firstStringIterationKey(start int) (Value, Value, bool) {
	for i := start; i < len(t.stringFields); i++ {
		if t.stringFields[i].value.IsNil() {
			continue
		}
		if t.stringFields[i].box != nil {
			return stringValueFromBox(t.stringFields[i].box), t.stringFields[i].value, true
		}
		return StringValue(t.stringFields[i].key), t.stringFields[i].value, true
	}
	return NilValue(), NilValue(), false
}

func (t *Table) ensureIterationJournal() {
	if t == nil || t.iteration != nil {
		return
	}
	activeCount := 0
	for _, value := range t.array {
		if !value.IsNil() {
			activeCount++
		}
	}
	activeCount += t.activeInlineStringFieldCount()
	if fields := t.hashFields(); fields != nil {
		activeCount += fields.count
	}
	journal := &tableIterationJournal{keys: make([]tableIterationKey, 0, activeCount)}
	for index, value := range t.array {
		if !value.IsNil() {
			journal.keys = append(journal.keys, tableIterationKey{
				key:     tableKey{kind: NumberKind, number: float64(index + 1)},
				present: true,
			})
		}
	}
	for _, field := range t.stringFields {
		if !field.value.IsNil() {
			journal.keys = append(journal.keys, tableIterationKey{
				key:     tableKey{kind: StringKind, str: field.key, strBox: field.box, strHash: stringBoxHash(field.box, field.key)},
				present: true,
			})
		}
	}
	if fields := t.hashFields(); fields != nil {
		fields.forEach(func(key tableKey, value Value) {
			journal.keys = append(journal.keys, tableIterationKey{key: key, present: true})
		})
	}
	if len(journal.keys) > 32 {
		t.iteration = journal
		t.buildIterationIndex()
		return
	}
	t.iteration = journal
}

func (t *Table) needsJournalForNewStringKey(key string) bool {
	if t.iteration != nil {
		return true
	}
	probe := tableStringProbeFromText(key)
	if t.hasStringOverflow() {
		if fields := t.hashFields(); fields != nil && fields.has(tableKey{kind: StringKind, str: key, strHash: probe.hash}) {
			return false
		}
		for i := range t.stringFields {
			if !t.stringFields[i].value.IsNil() && probe.matchesBox(t.stringFields[i].box, t.stringFields[i].key) {
				return false
			}
		}
		return true
	}
	for i := range t.stringFields {
		if !t.stringFields[i].value.IsNil() && probe.matchesBox(t.stringFields[i].box, t.stringFields[i].key) {
			return false
		}
	}
	if t.hashFieldCount() != 0 {
		return true
	}
	if t.activeInlineStringFieldCount() < maxInlineStringFields {
		return false
	}
	return len(t.stringFields) >= maxInlineStringFields
}

func (t *Table) needsJournalForArrayKey() bool {
	return t.iteration != nil || len(t.stringFields) != 0 || t.hashFieldCount() != 0
}

func (t *Table) markIterationKeyPresent(key tableKey) {
	if t.iteration == nil {
		return
	}
	if index, ok := t.iterationKeyIndex(key); ok {
		if index >= 0 && index < len(t.iteration.keys) && !t.iteration.keys[index].present {
			oldKey := t.iteration.keys[index].key
			t.iteration.keys[index].key = key
			t.iteration.keys[index].present = true
			t.iteration.tombstones--
			if t.iteration.index != nil && oldKey != key {
				delete(t.iteration.index, oldKey)
				t.iteration.index[key] = index
			}
		}
		return
	}
	if t.iteration.index != nil {
		t.iteration.index[key] = len(t.iteration.keys)
	}
	t.reserveIterationKeyCapacity()
	t.iteration.keys = append(t.iteration.keys, tableIterationKey{key: key, present: true})
}

func (t *Table) reserveIterationKeyCapacity() {
	if t == nil || t.iteration == nil || t.hashFieldCount() == 0 || len(t.iteration.keys) < cap(t.iteration.keys) {
		return
	}
	fields := t.hashFields()
	if fields == nil || len(fields.entries) == 0 {
		return
	}
	// Hash growth occurs at a 75% live-entry load. Reserve the remaining
	// headroom in this hash table so subsequent known inserts do not grow the
	// journal one entry at a time.
	nextGrowth := len(fields.entries) - len(fields.entries)/4
	reserve := nextGrowth - fields.count
	if reserve < 1 {
		reserve = 1
	}
	t.iteration.keys = slices.Grow(t.iteration.keys, reserve)
}

func (t *Table) markIterationKeyDeleted(key tableKey) {
	if t.iteration == nil {
		return
	}
	index, ok := t.iterationKeyIndex(key)
	if !ok || index < 0 || index >= len(t.iteration.keys) || !t.iteration.keys[index].present {
		return
	}
	t.iteration.keys[index].present = false
	t.iteration.tombstones++
	t.compactIterationKeysIfSparse()
}

func (t *Table) iterationKeyIndex(key tableKey) (int, bool) {
	if t.iteration == nil {
		return 0, false
	}
	if t.iteration.index != nil {
		if index, ok := t.iteration.index[key]; ok {
			return index, true
		}
		// Equal-content strings may have distinct boxes.  The index map keeps
		// the stored identity, so use the compact journal as the collision-safe
		// fallback instead of treating a host-created equal key as absent.
		for index, entry := range t.iteration.keys {
			if tableKeysEqual(entry.key, key) {
				return index, true
			}
		}
		return 0, false
	}
	if len(t.iteration.keys) > 32 {
		t.buildIterationIndex()
		index, ok := t.iteration.index[key]
		if ok {
			return index, true
		}
		for index, entry := range t.iteration.keys {
			if tableKeysEqual(entry.key, key) {
				return index, true
			}
		}
		return 0, false
	}
	for i, entry := range t.iteration.keys {
		if tableKeysEqual(entry.key, key) {
			return i, true
		}
	}
	return 0, false
}

func (t *Table) buildIterationIndex() {
	if t.iteration == nil {
		return
	}
	t.iteration.index = make(map[tableKey]int, len(t.iteration.keys))
	for i, entry := range t.iteration.keys {
		t.iteration.index[entry.key] = i
	}
}

func (t *Table) compactIterationKeysIfSparse() {
	if t.iteration == nil || len(t.iteration.keys) <= 32 || t.iteration.tombstones*2 <= len(t.iteration.keys) {
		return
	}
	keys := t.iteration.keys[:0]
	if t.iteration.index != nil {
		clear(t.iteration.index)
	}
	for _, entry := range t.iteration.keys {
		if !entry.present {
			continue
		}
		if t.iteration.index != nil {
			t.iteration.index[entry.key] = len(keys)
		}
		keys = append(keys, entry)
	}
	t.iteration.keys = keys
	t.iteration.tombstones = 0
}

func (t *Table) rawStringField(key string) (Value, bool) {
	return t.rawStringFieldWithProbe(tableStringProbeFromText(key))
}

// rawStringFieldBox is the boxed-key counterpart to rawStringField.  Runtime
// constants already own a stringBox, so callers on the script hot path can
// carry that identity through the probe and avoid rebuilding the hash or
// comparing bytes when the stored key is the same box.
func (t *Table) rawStringFieldBox(box *stringBox) (Value, bool) {
	if box == nil {
		return NilValue(), false
	}
	return t.rawStringFieldWithProbe(tableStringProbeFromBox(box))
}

func (t *Table) rawStringFieldWithProbe(probe tableStringProbe) (Value, bool) {
	if t == nil {
		return NilValue(), false
	}
	if t.hasStringOverflow() {
		if fields := t.hashFields(); fields != nil {
			key := tableKey{kind: StringKind, str: probe.text, strBox: probe.box, strHash: probe.hash}
			if value, ok := fields.get(key); ok {
				return value, true
			}
		}
	}
	for i := range t.stringFields {
		field := &t.stringFields[i]
		if field.value.IsNil() || !probe.matchesBox(field.box, field.key) {
			continue
		}
		return field.value, true
	}
	return NilValue(), false
}

func (t *Table) rawArrayValue(index int) (Value, bool) {
	if t == nil || index < 1 || index > len(t.array) {
		return NilValue(), false
	}
	value := t.array[index-1]
	if value.IsNil() {
		return NilValue(), false
	}
	return value, true
}

func (t *Table) rawGenericField(key tableKey) (Value, bool) {
	if t == nil {
		return NilValue(), false
	}
	fields := t.hashFields()
	if fields == nil {
		return NilValue(), false
	}
	return fields.get(key)
}

func (t *Table) rawStringFieldSlot(key string) (tableStringFieldSlot, bool) {
	return t.rawStringFieldSlotWithProbe(tableStringProbeFromText(key))
}

func (t *Table) rawStringFieldSlotBox(box *stringBox) (tableStringFieldSlot, bool) {
	if box == nil {
		return tableStringFieldSlot{}, false
	}
	return t.rawStringFieldSlotWithProbe(tableStringProbeFromBox(box))
}

func (t *Table) rawStringFieldSlotWithProbe(probe tableStringProbe) (tableStringFieldSlot, bool) {
	if t == nil {
		return tableStringFieldSlot{}, false
	}
	overflow := t.hasStringOverflow()
	if overflow {
		if fields := t.hashFields(); fields != nil {
			storedKey := tableKey{kind: StringKind, str: probe.text, strBox: probe.box, strHash: probe.hash}
			if index, ok := fields.find(storedKey); ok {
				entry := &fields.entries[index]
				return tableStringFieldSlot{
					index:   index,
					token:   t.stringShapeToken(),
					key:     entry.key.strBox,
					keyText: entry.key.str,
					keyHash: entry.hash,
				}, true
			}
		}
	}
	for i := range t.stringFields {
		field := &t.stringFields[i]
		if field.value.IsNil() || !probe.matchesBox(field.box, field.key) {
			continue
		}
		if overflow {
			// The slot token's storage bit follows the table, so it cannot
			// represent an inline index once the hash sidecar is active.
			return tableStringFieldSlot{}, false
		}
		return tableStringFieldSlot{
			index:   i,
			token:   t.stringShapeToken(),
			key:     field.box,
			keyText: field.key,
			keyHash: stringBoxHash(field.box, field.key),
		}, true
	}
	return tableStringFieldSlot{}, false
}

func (t *Table) rawStringFieldAtIndex(index int, key string) (Value, bool) {
	if t == nil ||
		t.hasStringOverflow() ||
		index < 0 ||
		index >= len(t.stringFields) ||
		t.stringFields[index].key != key ||
		t.stringFields[index].value.IsNil() {
		return NilValue(), false
	}
	return t.stringFields[index].value, true
}

func (t *Table) rawRowStringField(ref rowStringFieldSlotRef, key string) (Value, bool) {
	if ref.valid() {
		if value, ok := t.rawStringFieldAtIndex(ref.index, key); ok {
			return value, true
		}
	}
	return t.rawStringField(key)
}

func (t *Table) rawRowStringFieldBox(ref rowStringFieldSlotRef, box *stringBox) (Value, bool) {
	if box == nil {
		return NilValue(), false
	}
	if ref.valid() && !t.hasStringOverflow() && ref.index >= 0 && ref.index < len(t.stringFields) {
		field := &t.stringFields[ref.index]
		if !field.value.IsNil() && field.box == box {
			return field.value, true
		}
	}
	return t.rawStringFieldBox(box)
}

func (t *Table) rawStringFieldAtSlot(slot tableStringFieldSlot, key string) (Value, bool) {
	if !slot.token.matchesTableLayout(t) {
		return NilValue(), false
	}
	probe := tableStringProbeFromText(key)
	if slot.token.storage == 1 {
		fields := t.hashFields()
		if fields == nil || slot.index < 0 || slot.index >= len(fields.entries) {
			return NilValue(), false
		}
		entry := &fields.entries[slot.index]
		if entry.state != tableHashFull || entry.value.IsNil() || entry.hash != hashStringIfNeeded(slot.keyHash, slot.keyText) ||
			!probe.matchesBox(entry.key.strBox, entry.key.str) {
			return NilValue(), false
		}
		return entry.value, true
	}
	if slot.token.storage != 0 ||
		slot.index < 0 ||
		slot.index >= len(t.stringFields) ||
		t.stringFields[slot.index].value.IsNil() ||
		!probe.matchesBox(t.stringFields[slot.index].box, t.stringFields[slot.index].key) {
		return NilValue(), false
	}
	return t.stringFields[slot.index].value, true
}

func (t *Table) rawStringFieldAtExactCachedSlot(slot tableStringFieldSlot, key string) (Value, bool) {
	if t == nil ||
		slot.token.layout != t.stringVersion ||
		slot.token.metatable != t.metatable ||
		(slot.token.storage == 1 && (t.hashFields() == nil || slot.token.storageGeneration != t.hashFields().generation)) {
		return NilValue(), false
	}
	return t.rawStringFieldAtSlot(slot, key)
}

func (t *Table) setRawStringFieldAtIndex(index int, key string, value Value) bool {
	if t == nil ||
		value.IsNil() ||
		t.hasStringOverflow() ||
		index < 0 ||
		index >= len(t.stringFields) ||
		t.stringFields[index].key != key ||
		t.stringFields[index].value.IsNil() {
		return false
	}
	t.stringFields[index].value = value
	t.stringValueVersion++
	return true
}

func (t *Table) setRawRowStringField(ref rowStringFieldSlotRef, key string, value Value) {
	if ref.valid() && t.setRawStringFieldAtIndex(ref.index, key, value) {
		return
	}
	t.setRawStringField(key, value)
}

func (t *Table) setRawRowStringFieldBox(ref rowStringFieldSlotRef, box *stringBox, value Value) {
	if box != nil && ref.valid() && !t.hasStringOverflow() && ref.index >= 0 && ref.index < len(t.stringFields) {
		field := &t.stringFields[ref.index]
		if !field.value.IsNil() && field.box == box && !value.IsNil() {
			field.value = value
			t.stringValueVersion++
			return
		}
	}
	if box != nil {
		t.setRawStringFieldBox(box.text, box, value)
	}
}

func (t *Table) setRawStringFieldAtSlot(slot tableStringFieldSlot, key string, value Value) bool {
	if value.IsNil() || !slot.token.matchesTableLayout(t) {
		return false
	}
	if slot.token.storage == 1 {
		fields := t.hashFields()
		if fields == nil || slot.index < 0 || slot.index >= len(fields.entries) {
			return false
		}
		entry := &fields.entries[slot.index]
		if entry.state != tableHashFull || entry.value.IsNil() || !tableStringProbeFromText(key).matchesBox(entry.key.strBox, entry.key.str) {
			return false
		}
		entry.value = value
		t.stringValueVersion++
		return true
	}
	if slot.token.storage != 0 ||
		slot.index < 0 ||
		slot.index >= len(t.stringFields) ||
		t.stringFields[slot.index].value.IsNil() ||
		!tableStringProbeFromText(key).matchesBox(t.stringFields[slot.index].box, t.stringFields[slot.index].key) {
		return false
	}
	t.stringFields[slot.index].value = value
	t.stringValueVersion++
	return true
}

func (t *Table) setRawStringFieldAtExactCachedSlot(slot tableStringFieldSlot, key string, value Value) bool {
	if t == nil ||
		value.IsNil() ||
		slot.token.layout != t.stringVersion ||
		slot.token.metatable != t.metatable ||
		(slot.token.storage == 1 && (t.hashFields() == nil || slot.token.storageGeneration != t.hashFields().generation)) {
		return false
	}
	return t.setRawStringFieldAtSlot(slot, key, value)
}

func (t *Table) addRawStringFieldNumber(key string, delta Value) (Value, bool) {
	return t.addRawStringFieldNumberProbe(tableStringProbeFromText(key), delta)
}

func (t *Table) addRawStringFieldNumberBox(box *stringBox, delta Value) (Value, bool) {
	if box == nil {
		return NilValue(), false
	}
	return t.addRawStringFieldNumberProbe(tableStringProbeFromBox(box), delta)
}

func (t *Table) addRawStringFieldNumberProbe(probe tableStringProbe, delta Value) (Value, bool) {
	if t == nil || valueKind(delta) != NumberKind {
		return NilValue(), false
	}
	deltaNumber := valueNumber(delta)
	for index := range t.stringFields {
		field := &t.stringFields[index]
		if field.value.IsNil() || !probe.matchesBox(field.box, field.key) {
			continue
		}
		if valueKind(field.value) != NumberKind {
			return NilValue(), false
		}
		value := NumberValue(valueNumber(field.value) + deltaNumber)
		field.value = value
		t.stringValueVersion++
		return value, true
	}
	if t.hasStringOverflow() {
		storedKey := tableKey{kind: StringKind, str: probe.text, strBox: probe.box, strHash: probe.hash}
		fields := t.hashFields()
		if fields == nil {
			return NilValue(), false
		}
		current, ok := fields.get(storedKey)
		if !ok || valueKind(current) != NumberKind {
			return NilValue(), false
		}
		value := NumberValue(valueNumber(current) + deltaNumber)
		if index, ok := fields.find(storedKey); ok {
			fields.entries[index].value = value
		}
		t.stringValueVersion++
		return value, true
	}
	return NilValue(), false
}

func (t *Table) setExistingRawStringFieldNumber(key string, number float64) bool {
	if t == nil {
		return false
	}
	value := NumberValue(number)
	probe := tableStringProbeFromText(key)
	for index := range t.stringFields {
		if t.stringFields[index].value.IsNil() || !probe.matchesBox(t.stringFields[index].box, t.stringFields[index].key) {
			continue
		}
		t.stringFields[index].value = value
		t.stringValueVersion++
		return true
	}
	if t.hasStringOverflow() {
		storedKey := tableKey{kind: StringKind, str: key, strHash: probe.hash}
		fields := t.hashFields()
		if fields == nil {
			return false
		}
		if index, ok := fields.find(storedKey); !ok {
			return false
		} else {
			fields.entries[index].value = value
		}
		t.stringValueVersion++
		return true
	}
	return false
}

func (t *Table) setRawStringField(key string, value Value) {
	t.setRawStringFieldBox(key, nil, value)
}

func (t *Table) setRawStringFieldBox(key string, box *stringBox, value Value) {
	if value.IsNil() {
		t.deleteRawStringField(key)
		return
	}
	if t.needsJournalForNewStringKey(key) {
		t.ensureIterationJournal()
	}
	probe := tableStringProbe{box: box, text: key, hash: stringBoxHash(box, key)}
	for i := range t.stringFields {
		field := &t.stringFields[i]
		if !probe.matchesBox(field.box, field.key) {
			continue
		}
		if !field.value.IsNil() {
			// Updating an existing equal key preserves its original boxed key
			// identity, matching the previous table behavior.
			field.value = value
			t.stringValueVersion++
			return
		}
		// A shaped deletion is a tombstone. Re-adding the same property keeps
		// its stable offset while publishing the new key identity to iteration.
		field.key = key
		field.box = box
		field.value = value
		if t.iteration != nil {
			t.markIterationKeyPresent(tableKey{kind: StringKind, str: key, strBox: box, strHash: probe.hash})
		}
		t.stringVersion++
		t.stringValueVersion++
		return
	}
	if t.hasStringOverflow() {
		storedKey := tableKey{kind: StringKind, str: key, strBox: box, strHash: probe.hash}
		if added := t.ensureHashFields().set(storedKey, value); added {
			t.coldData().stringHashCount++
			t.stringVersion++
			t.markIterationKeyPresent(storedKey)
		}
		t.stringValueVersion++
		return
	}
	if len(t.stringFields) < maxInlineStringFields {
		if t.stringFields == nil {
			t.stringFields = tableInlineFields(t)[:0]
		}
		offset := len(t.stringFields)
		t.stringFields = append(t.stringFields, tableStringField{key: key, box: box, value: value})
		if t.iteration != nil {
			t.markIterationKeyPresent(tableKey{kind: StringKind, str: key, strBox: box, strHash: probe.hash})
		}
		// Publish after append so a visible shape always proves the slot exists.
		t.publishAppendedStringShape(key, box, offset)
		t.stringVersion++
		t.stringValueVersion++
		return
	}
	// Keep the existing inline slots in place. New fields share the open
	// addressed sidecar. Unrelated dictionary growth does not alter the inline
	// shape, so constant-property caches for the stable prefix stay valid.
	t.ensureIterationJournal()
	fields := t.ensureHashFields()
	storedKey := tableKey{kind: StringKind, str: key, strBox: box, strHash: probe.hash}
	if added := fields.set(storedKey, value); added {
		t.coldData().stringHashCount++
	}
	t.markIterationKeyPresent(storedKey)
	t.stringVersion++
	t.stringValueVersion++
}

func (t *Table) deleteRawStringField(key string) {
	probe := tableStringProbeFromText(key)
	for i := range t.stringFields {
		field := &t.stringFields[i]
		if field.value.IsNil() || !probe.matchesBox(field.box, field.key) {
			continue
		}
		if t.iteration == nil && t.activeInlineStringFieldCount() > 1 {
			t.ensureIterationJournal()
		}
		storedKey := tableKey{kind: StringKind, str: field.key, strBox: field.box, strHash: stringBoxHash(field.box, field.key)}
		// Keep the key text as the shaped slot descriptor, but release the old
		// box and clear only the value. The shape and property offset survive.
		field.box = nil
		field.value = NilValue()
		t.markIterationKeyDeleted(storedKey)
		t.stringVersion++
		t.stringValueVersion++
		return
	}
	if t.hasStringOverflow() {
		storedKey := tableKey{kind: StringKind, str: key, strHash: probe.hash}
		fields := t.hashFields()
		if fields != nil {
			if index, ok := fields.find(storedKey); ok {
				deletedKey := fields.entries[index].key
				if fields.delete(storedKey) {
					if t.cold != nil && t.cold.stringHashCount > 0 {
						t.cold.stringHashCount--
					}
					t.markIterationKeyDeleted(deletedKey)
					t.stringVersion++
					t.stringValueVersion++
				}
			}
		}
	}
}

func (t *Table) activeInlineStringFieldCount() int {
	if t == nil {
		return 0
	}
	count := 0
	for _, field := range t.stringFields {
		if !field.value.IsNil() {
			count++
		}
	}
	return count
}

func (t *Table) cachedIndexTable() (*Table, bool, error) {
	index, ok, err := t.cachedIndexFallback()
	if err != nil || !ok {
		return nil, false, err
	}
	indexTable, ok := index.Table()
	if !ok {
		return nil, false, nil
	}
	return indexTable, true, nil
}

func (t *Table) cachedIndexFallback() (Value, bool, error) {
	if t == nil || t.metatable == nil {
		return NilValue(), false, nil
	}
	metatable := t.metatable
	cold := t.coldData()
	cache := &cold.indexCache
	if cache.ready && cache.metatable == metatable {
		if cache.negative {
			if cache.slot.token.matchesTableLayout(metatable) {
				return NilValue(), false, nil
			}
		} else if value, ok := metatable.rawStringFieldAtSlot(cache.slot, "__index"); ok {
			return value, true, nil
		}
	}
	slot, ok := metatable.rawStringFieldSlot("__index")
	if !ok {
		if index, found := metatable.rawStringField("__index"); found {
			// An inline key remains readable after overflow, but its slot token
			// cannot represent the table's hash-backed storage bit. Leave this
			// lookup uncached rather than installing a false negative.
			*cache = tableMetamethodCache{metatable: metatable}
			return index, true, nil
		}
		cache.metatable = metatable
		cache.ready = true
		cache.slot = slot
		cache.negative = true
		cache.slot = tableStringFieldSlot{index: -1, keyText: "__index", keyHash: hashString("__index"), token: metatable.stringShapeToken()}
		return NilValue(), false, nil
	}
	cache.metatable = metatable
	cache.ready = true
	cache.slot = slot
	cache.negative = false
	index, ok := metatable.rawStringFieldAtSlot(slot, "__index")
	if !ok {
		cache.negative = true
		return NilValue(), false, nil
	}
	return index, true, nil
}

func (t *Table) cachedNewIndexFallback() (Value, bool, error) {
	if t == nil || t.metatable == nil {
		return NilValue(), false, nil
	}
	metatable := t.metatable
	cold := t.coldData()
	cache := &cold.newIndexCache
	if cache.ready && cache.metatable == metatable {
		if cache.negative {
			if cache.slot.token.matchesTableLayout(metatable) {
				return NilValue(), false, nil
			}
		} else if value, ok := metatable.rawStringFieldAtSlot(cache.slot, "__newindex"); ok {
			return value, true, nil
		}
	}
	slot, ok := metatable.rawStringFieldSlot("__newindex")
	if !ok {
		if newIndex, found := metatable.rawStringField("__newindex"); found {
			// See cachedIndexFallback: preserve semantics without a false
			// negative cache when the inline key is uncacheable after overflow.
			*cache = tableMetamethodCache{metatable: metatable}
			return newIndex, true, nil
		}
		cache.metatable = metatable
		cache.ready = true
		cache.slot = slot
		cache.negative = true
		cache.slot = tableStringFieldSlot{index: -1, keyText: "__newindex", keyHash: hashString("__newindex"), token: metatable.stringShapeToken()}
		return NilValue(), false, nil
	}
	cache.metatable = metatable
	cache.ready = true
	cache.slot = slot
	cache.negative = false
	newIndex, ok := metatable.rawStringFieldAtSlot(slot, "__newindex")
	if !ok {
		cache.negative = true
		return NilValue(), false, nil
	}
	return newIndex, true, nil
}

func tableArrayIndexFromValue(v Value) (int, bool) {
	if valueKind(v) != NumberKind {
		return 0, false
	}
	number := valueNumber(v)
	if math.IsNaN(number) || number < 1 || math.Trunc(number) != number {
		return 0, false
	}
	maxInt := int(^uint(0) >> 1)
	if number > float64(maxInt) {
		return 0, false
	}
	return int(number), true
}

func tableKeyFromValue(v Value) (tableKey, bool) {
	switch valueKind(v) {
	case BoolKind:
		return tableKey{kind: BoolKind, bool: valueBool(v)}, true
	case NumberKind:
		number := valueNumber(v)
		if math.IsNaN(number) {
			return tableKey{}, false
		}
		return tableKey{kind: NumberKind, number: number}, true
	case StringKind:
		box := v.stringBox()
		if box == nil {
			return tableKey{}, false
		}
		return tableKey{kind: StringKind, str: box.text, strBox: box, strHash: box.hash}, true
	case TableKind:
		table := v.tableRef()
		if table == nil {
			return tableKey{}, false
		}
		return tableKey{kind: TableKind, table: table}, true
	case UserDataKind:
		userdata := v.userdataRef()
		if userdata == nil {
			return tableKey{}, false
		}
		return tableKey{kind: UserDataKind, userdata: userdata}, true
	default:
		return tableKey{}, false
	}
}

func (k tableKey) value() Value {
	switch k.kind {
	case BoolKind:
		return BoolValue(k.bool)
	case NumberKind:
		return NumberValue(k.number)
	case StringKind:
		if k.strBox != nil {
			return stringValueFromBox(k.strBox)
		}
		return StringValue(k.str)
	case TableKind:
		return TableValue(k.table)
	case UserDataKind:
		return UserDataValue(k.userdata)
	default:
		return NilValue()
	}
}

func (k tableKey) less(other tableKey) bool {
	leftRank := tableKeyRank(k)
	rightRank := tableKeyRank(other)
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	switch k.kind {
	case NumberKind:
		return k.number < other.number
	case StringKind:
		return k.str < other.str
	case BoolKind:
		return !k.bool && other.bool
	case TableKind:
		return k.table.objectID() < other.table.objectID()
	case UserDataKind:
		return k.userdata.id < other.userdata.id
	default:
		return false
	}
}

func tableKeyRank(key tableKey) int {
	switch key.kind {
	case NumberKind:
		return 0
	case StringKind:
		return 1
	case BoolKind:
		return 2
	case TableKind:
		return 3
	case UserDataKind:
		return 4
	default:
		return 5
	}
}

func validateTableKey(key Value, ok bool) error {
	if ok {
		return nil
	}
	if valueKind(key) == NumberKind && math.IsNaN(valueNumber(key)) {
		return fmt.Errorf("table: key is NaN")
	}
	return fmt.Errorf("table: key is %s, want boolean, string, number, table, or userdata", key.Kind())
}

func (v Value) hostFunction() (HostFunc, bool) {
	callable := v.hostCallableRef()
	if callable == nil || callable.hostFunc == nil {
		return nil, false
	}
	return callable.hostFunc, true
}

func (v Value) contextHostFunction() (ContextHostFunc, bool) {
	callable := v.hostCallableRef()
	if callable == nil || callable.contextHost == nil {
		return nil, false
	}
	return callable.contextHost, true
}

func (v Value) nativeFunction() (nativeFunc, bool) {
	if valueKind(v) != HostFuncKind {
		return nil, false
	}
	if id := valueNativeID(v); id != nativeFuncUnknown {
		return nativeFuncByID(id)
	}
	callable := v.hostCallableRef()
	if callable == nil || callable.native == nil {
		return nil, false
	}
	return callable.native, true
}

func (v Value) yieldableHostFunction() (yieldableHostFunc, bool) {
	callable := v.hostCallableRef()
	if callable == nil || callable.yieldableHost == nil {
		return nil, false
	}
	return callable.yieldableHost, true
}

func (v Value) hostCallableRef() *hostCallable {
	if valueKind(v) != HostFuncKind || valueRef(v) == nil {
		return nil
	}
	return (*hostCallable)(valueRef(v))
}

func (v Value) scriptFunction() (*closure, bool) {
	if valueKind(v) != FunctionKind || v.bits != uint64(FunctionKind) || valueRef(v) == nil {
		return nil, false
	}
	return (*closure)(valueRef(v)), true
}

func (v Value) truthy() bool {
	if valueKind(v) == NilKind {
		return false
	}
	if valueKind(v) == BoolKind {
		return valueBool(v)
	}
	return true
}

func valuesEqual(left Value, right Value) bool {
	leftKind := valueKind(left)
	if leftKind != valueKind(right) {
		return false
	}

	switch leftKind {
	case NilKind:
		return true
	case BoolKind:
		return valueBool(left) == valueBool(right)
	case NumberKind:
		leftNumber, rightNumber := valueNumber(left), valueNumber(right)
		if math.IsNaN(leftNumber) || math.IsNaN(rightNumber) {
			return false
		}
		return leftNumber == rightNumber
	case StringKind:
		return stringBoxesEqual(left.stringBox(), right.stringBox())
	case TableKind:
		return left.tableRef() != nil && left.tableRef() == right.tableRef()
	case UserDataKind:
		return left.userdataRef() != nil && left.userdataRef() == right.userdataRef()
	case FunctionKind:
		leftFunction, _ := left.scriptFunction()
		rightFunction, _ := right.scriptFunction()
		return leftFunction != nil && leftFunction == rightFunction
	case HostFuncKind:
		return false
	default:
		return false
	}
}

func stringBoxesEqual(left *stringBox, right *stringBox) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left == right {
		return true
	}
	return left.hash == right.hash && len(left.text) == len(right.text) && left.text == right.text
}

func valuesLess(left Value, right Value) (bool, error) {
	leftKind := valueKind(left)
	if leftKind != valueKind(right) {
		return false, fmt.Errorf("compare operands are %s and %s", left.Kind(), right.Kind())
	}

	switch leftKind {
	case NumberKind:
		leftNumber, rightNumber := valueNumber(left), valueNumber(right)
		if math.IsNaN(leftNumber) || math.IsNaN(rightNumber) {
			return false, fmt.Errorf("compare operand is NaN")
		}
		return leftNumber < rightNumber, nil
	case StringKind:
		return left.stringText() < right.stringText(), nil
	default:
		return false, fmt.Errorf("compare operands are %s, want number or string", left.Kind())
	}
}

func valuesLessEqual(left Value, right Value) (bool, error) {
	if valuesEqual(left, right) {
		return true, nil
	}
	return valuesLess(left, right)
}

func rawLength(value Value) (int, error) {
	if str, ok := value.String(); ok {
		return len(str), nil
	}
	if table, ok := value.Table(); ok {
		return table.rawLen()
	}
	return 0, fmt.Errorf("length operand is %s, want string or table", value.Kind())
}

func numericOperand(value Value, side string, op string) (float64, error) {
	if number, ok := numericOperandValue(value); ok {
		return number, nil
	}
	operand := "operand"
	if side != "" {
		operand = side + " operand"
	}
	return 0, fmt.Errorf("%s %s is %s, want number", op, operand, value.Kind())
}

func numericOperandValue(value Value) (float64, bool) {
	if number, ok := value.Number(); ok {
		return number, true
	}
	if str, ok := value.String(); ok {
		number, err := strconv.ParseFloat(str, 64)
		if err == nil {
			return number, true
		}
	}
	return 0, false
}

func valuesConcat(left Value, right Value) (string, error) {
	leftString, err := concatOperandString(left, "left")
	if err != nil {
		return "", err
	}
	rightString, err := concatOperandString(right, "right")
	if err != nil {
		return "", err
	}
	return leftString + rightString, nil
}

func valuesConcatRawChain(values []Value) (string, bool, error) {
	for _, value := range values {
		switch valueKind(value) {
		case StringKind, NumberKind:
		default:
			return "", false, nil
		}
	}
	scratch, err := appendConcatRawChain(nil, values)
	if err != nil {
		return "", false, err
	}
	return string(scratch), true, nil
}

func appendConcatRawChain(dst []byte, values []Value) ([]byte, error) {
	for _, value := range values {
		var err error
		dst, err = appendConcatOperandString(dst, value, "")
		if err != nil {
			return dst, err
		}
	}
	return dst, nil
}

func formatLuauNumber(number float64) string {
	if text, ok := smallIntegerString(number); ok {
		return text
	}
	if number == math.Trunc(number) &&
		!math.Signbit(number) &&
		number < 1_000_000 {
		return strconv.FormatInt(int64(number), 10)
	}
	if number == math.Trunc(number) &&
		math.Signbit(number) &&
		number != 0 &&
		number > -1_000_000 {
		return strconv.FormatInt(int64(number), 10)
	}
	return strconv.FormatFloat(number, 'g', -1, 64)
}

func appendLuauNumber(dst []byte, number float64) []byte {
	if text, ok := smallIntegerString(number); ok {
		return append(dst, text...)
	}
	if number == math.Trunc(number) &&
		!math.Signbit(number) &&
		number < 1_000_000 {
		return strconv.AppendInt(dst, int64(number), 10)
	}
	if number == math.Trunc(number) &&
		math.Signbit(number) &&
		number != 0 &&
		number > -1_000_000 {
		return strconv.AppendInt(dst, int64(number), 10)
	}
	return strconv.AppendFloat(dst, number, 'g', -1, 64)
}

func smallIntegerString(number float64) (string, bool) {
	if number != math.Trunc(number) ||
		(math.Signbit(number) && number == 0) ||
		number < -999 || number > 999 {
		return "", false
	}
	index := int(number) + 999
	if index < 0 || index >= len(smallIntegerStrings) || float64(index-999) != number {
		return "", false
	}
	return smallIntegerStrings[index], true
}

var smallIntegerStrings = func() [1999]string {
	var values [1999]string
	for i := range values {
		values[i] = strconv.Itoa(i - 999)
	}
	return values
}()

func concatOperandString(value Value, side string) (string, error) {
	if str, ok := value.String(); ok {
		return str, nil
	}
	if number, ok := value.Number(); ok {
		return formatLuauNumber(number), nil
	}
	return "", fmt.Errorf("concat %s operand is %s, want string or number", side, value.Kind())
}

func appendConcatOperandString(dst []byte, value Value, side string) ([]byte, error) {
	if str, ok := value.String(); ok {
		return append(dst, str...), nil
	}
	if number, ok := value.Number(); ok {
		return appendLuauNumber(dst, number), nil
	}
	return dst, fmt.Errorf("concat %s operand is %s, want string or number", side, value.Kind())
}
