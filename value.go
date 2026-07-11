package ember

import (
	"context"
	"fmt"
	"math"
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
)

// Value is an Ember runtime value.
type Value struct {
	number   float64
	ref      unsafe.Pointer
	kind     ValueKind
	bool     bool
	nativeID nativeFuncID
}

type stringBox struct {
	text string
	hash uint64
}

type hostCallable struct {
	hostFunc      HostFunc
	native        nativeFunc
	yieldableHost yieldableHostFunc
}

type cell struct {
	value Value
	slot  *Value
}

func (c *cell) get() Value {
	if c == nil {
		return NilValue()
	}
	if c.slot != nil {
		return *c.slot
	}
	return c.value
}

func (c *cell) set(value Value) {
	if c == nil {
		return
	}
	c.value = value
	if c.slot != nil {
		*c.slot = value
	}
}

func (c *cell) bindSlot(slot *Value) {
	if c == nil {
		return
	}
	c.slot = slot
	if slot != nil {
		c.value = *slot
	}
}

func (c *cell) detachSlot() {
	if c == nil || c.slot == nil {
		return
	}
	c.value = *c.slot
	c.slot = nil
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
	id                     uint64
	indexCacheMetatable    *Table
	indexCacheVersion      uint32
	indexCacheValue        Value
	indexCacheReady        bool
	newIndexCacheMetatable *Table
	newIndexCacheVersion   uint32
	newIndexCacheValue     Value
	newIndexCacheReady     bool
	stringHashCount        int
	fields                 tableHashFields
}

type tableHashFields struct {
	entries    []tableHashEntry
	count      int
	tombstones int
}

type tableHashEntry struct {
	key   tableKey
	value Value
	state uint8
}

type tableStringField struct {
	key   string
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
	index int
	token tableStringShapeToken
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
	metatable *Table
	layout    uint32
	values    uint32
	storage   uint8
}

func (left tableStringShapeToken) sameLayout(right tableStringShapeToken) bool {
	return left.layout == right.layout &&
		left.storage == right.storage &&
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
	currentStorage := uint8(0)
	if table.hasStringOverflow() {
		currentStorage = 1
	}
	return token.storage == currentStorage
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
	var storage uint8
	if t.hasStringOverflow() {
		storage = 1
	}
	return tableStringShapeToken{
		metatable: t.metatable,
		layout:    t.stringVersion,
		values:    t.stringValueVersion,
		storage:   storage,
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
	if len(fields.entries) == 0 || (fields.count+fields.tombstones+1)*4 >= len(fields.entries)*3 {
		fields.grow()
	}
	index, ok := fields.findInsert(key)
	if ok {
		fields.entries[index].value = value
		return false
	}
	if fields.entries[index].state == tableHashDeleted {
		fields.tombstones--
	}
	fields.entries[index] = tableHashEntry{key: key, value: value, state: tableHashFull}
	fields.count++
	return true
}

func (fields *tableHashFields) delete(key tableKey) bool {
	index, ok := fields.find(key)
	if !ok {
		return false
	}
	fields.entries[index].value = NilValue()
	fields.entries[index].state = tableHashDeleted
	fields.count--
	fields.tombstones++
	return true
}

func (fields *tableHashFields) grow() {
	next := 8
	if len(fields.entries) > 0 {
		next = len(fields.entries) * 2
	}
	old := fields.entries
	fields.entries = make([]tableHashEntry, next)
	fields.count = 0
	fields.tombstones = 0
	for _, entry := range old {
		if entry.state == tableHashFull && !entry.value.IsNil() {
			fields.set(entry.key, entry.value)
		}
	}
}

func (fields *tableHashFields) find(key tableKey) (int, bool) {
	mask := uint64(len(fields.entries) - 1)
	index := int(key.hash() & mask)
	for probe := 0; probe < len(fields.entries); probe++ {
		entry := fields.entries[index]
		switch entry.state {
		case tableHashEmpty:
			return 0, false
		case tableHashFull:
			if entry.key == key {
				return index, true
			}
		}
		index = (index + 1) & int(mask)
	}
	return 0, false
}

func (fields *tableHashFields) findInsert(key tableKey) (int, bool) {
	mask := uint64(len(fields.entries) - 1)
	index := int(key.hash() & mask)
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
			if entry.key == key {
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
		return hash ^ math.Float64bits(key.number)
	case StringKind:
		return hash ^ hashString(key.str)
	case TableKind:
		return hash ^ uintptrHash(uintptr(unsafe.Pointer(key.table)))
	case UserDataKind:
		return hash ^ key.userdata.id
	default:
		return hash
	}
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
	return Value{kind: NilKind}
}

// BoolValue returns a Luau boolean value.
func BoolValue(b bool) Value {
	return Value{
		kind: BoolKind,
		bool: b,
	}
}

// NumberValue returns a Luau number value.
func NumberValue(n float64) Value {
	return Value{
		kind:   NumberKind,
		number: n,
	}
}

// StringValue returns a Luau string value.
func StringValue(s string) Value {
	return stringValueFromBox(newStringBox(s))
}

func newStringBox(s string) *stringBox {
	return &stringBox{text: s, hash: hashString(s)}
}

func stringValueFromBox(box *stringBox) Value {
	return Value{
		kind: StringKind,
		ref:  unsafe.Pointer(box),
	}
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
	return Value{
		kind: HostFuncKind,
		ref:  unsafe.Pointer(&hostCallable{hostFunc: fn}),
	}
}

// ContextHostFuncValue returns a Go host callback value that receives the
// active runtime context when called.
func ContextHostFuncValue(fn ContextHostFunc) Value {
	return nativeFuncValue(func(globals *globalEnv, args []Value) ([]Value, error) {
		return fn(contextFromGlobalEnv(globals), args)
	})
}

func nativeFuncValue(fn nativeFunc) Value {
	return nativeFuncValueWithID(fn, nativeFuncUnknown)
}

func nativeFuncValueWithID(fn nativeFunc, id nativeFuncID) Value {
	if id != nativeFuncUnknown {
		return Value{
			kind:     HostFuncKind,
			nativeID: id,
		}
	}
	return Value{
		kind:     HostFuncKind,
		nativeID: id,
		ref:      unsafe.Pointer(&hostCallable{native: fn}),
	}
}

func yieldableHostFuncValue(fn yieldableHostFunc) Value {
	return Value{
		kind: HostFuncKind,
		ref:  unsafe.Pointer(&hostCallable{yieldableHost: fn}),
	}
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
	return Value{
		kind: UserDataKind,
		ref:  unsafe.Pointer(userdata),
	}
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
	return &storage.table
}

func newTableArrayStorage() *tableArrayStorage {
	storage := &tableArrayStorage{}
	storage.table.inlineFields = &storage.inlineFields
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

// TableValue returns a Luau table value backed by table.
func TableValue(table *Table) Value {
	return Value{
		kind: TableKind,
		ref:  unsafe.Pointer(table),
	}
}

func functionValue(proto *Proto, upvalues []*cell) Value {
	return functionValueWithUpvalues(proto, upvalues, nil, nil)
}

func functionValueWithUpvalues(proto *Proto, upvalues []*cell, values []Value, valueOK []bool) Value {
	return closureFunctionValue(&closure{proto: proto, upvalues: upvalues, upvalueValues: values, upvalueValueOK: valueOK})
}

func closureFunctionValue(closure *closure) Value {
	return Value{
		kind: FunctionKind,
		ref:  unsafe.Pointer(closure),
	}
}

// Kind returns the value kind.
func (v Value) Kind() ValueKind {
	return v.kind
}

// IsNil returns whether this Value is nil.
func (v Value) IsNil() bool {
	return v.kind == NilKind
}

// Bool returns the boolean value and whether this Value is a boolean.
func (v Value) Bool() (bool, bool) {
	if v.kind != BoolKind {
		return false, false
	}
	return v.bool, true
}

// Number returns the numeric value and whether this Value is a number.
func (v Value) Number() (float64, bool) {
	if v.kind != NumberKind {
		return 0, false
	}
	return v.number, true
}

// String returns the string value and whether this Value is a string.
func (v Value) String() (string, bool) {
	if v.kind != StringKind {
		return "", false
	}
	box := v.stringBox()
	if box == nil {
		return "", false
	}
	return box.text, true
}

func (v Value) stringBox() *stringBox {
	if v.kind != StringKind || v.ref == nil {
		return nil
	}
	return (*stringBox)(v.ref)
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
	if v.kind != TableKind || v.ref == nil {
		return nil
	}
	return (*Table)(v.ref)
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
	if v.kind != UserDataKind || v.ref == nil {
		return nil
	}
	return (*UserData)(v.ref)
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
		return t.rawGetString(storedKey.str)
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
	value, ok := t.rawStringField(key)
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
		return t.rawGetString(key.str)
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
	if t == nil {
		return fmt.Errorf("table: nil table")
	}
	if index, ok := tableArrayIndexFromValue(key); ok {
		return t.rawSetArrayIndex(index, value)
	}
	storedKey, ok := tableKeyFromValue(key)
	if err := validateTableKey(key, ok); err != nil {
		return err
	}
	if storedKey.kind == StringKind {
		return t.rawSetString(storedKey.str, value)
	}
	t.setRawGenericField(storedKey, value)
	return nil
}

func (t *Table) rawSetString(key string, value Value) error {
	if t == nil {
		return fmt.Errorf("table: nil table")
	}
	t.setRawStringField(key, value)
	return nil
}

func (t *Table) rawSetKey(storedKey tableKey, value Value) error {
	if t == nil {
		return fmt.Errorf("table: nil table")
	}
	if storedKey.kind == StringKind {
		return t.rawSetString(storedKey.str, value)
	}
	t.setRawGenericField(storedKey, value)
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
		if t.array[index-1].IsNil() {
			if t.needsJournalForArrayKey() {
				t.ensureIterationJournal()
			}
			if t.iteration != nil {
				t.markIterationKeyPresent(key)
			}
			t.arrayVersion++
		}
		t.array[index-1] = value
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
		t.cold.indexCacheMetatable = nil
		t.cold.indexCacheVersion = 0
		t.cold.indexCacheValue = NilValue()
		t.cold.indexCacheReady = false
		t.cold.newIndexCacheMetatable = nil
		t.cold.newIndexCacheVersion = 0
		t.cold.newIndexCacheValue = NilValue()
		t.cold.newIndexCacheReady = false
	}
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
		for i := range t.stringFields {
			if t.stringFields[i].key != storedKey.str {
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
		return StringValue(t.stringFields[i].key), t.stringFields[i].value, true
	}
	return NilValue(), NilValue(), false
}

func (t *Table) ensureIterationJournal() {
	if t == nil || t.iteration != nil {
		return
	}
	journal := &tableIterationJournal{}
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
				key:     tableKey{kind: StringKind, str: field.key},
				present: true,
			})
		}
	}
	if fields := t.hashFields(); fields != nil {
		fields.forEach(func(key tableKey, value Value) {
			journal.keys = append(journal.keys, tableIterationKey{key: key, present: true})
		})
	}
	t.iteration = journal
}

func (t *Table) needsJournalForNewStringKey(key string) bool {
	if t.iteration != nil {
		return true
	}
	if t.hasStringOverflow() {
		return !t.ensureHashFields().has(tableKey{kind: StringKind, str: key})
	}
	for i := range t.stringFields {
		if t.stringFields[i].key == key {
			return false
		}
	}
	return t.hashFieldCount() != 0 || len(t.stringFields) >= maxInlineStringFields
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
			t.iteration.keys[index].present = true
			t.iteration.tombstones--
		}
		return
	}
	if t.iteration.index != nil {
		t.iteration.index[key] = len(t.iteration.keys)
	}
	t.iteration.keys = append(t.iteration.keys, tableIterationKey{key: key, present: true})
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
		index, ok := t.iteration.index[key]
		return index, ok
	}
	if len(t.iteration.keys) > 32 {
		t.buildIterationIndex()
		index, ok := t.iteration.index[key]
		return index, ok
	}
	for i, entry := range t.iteration.keys {
		if entry.key == key {
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
	if t.hasStringOverflow() {
		return t.ensureHashFields().get(tableKey{kind: StringKind, str: key})
	}
	for i := range t.stringFields {
		if t.stringFields[i].key == key {
			return t.stringFields[i].value, true
		}
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
	if t == nil {
		return tableStringFieldSlot{}, false
	}
	if t.hasStringOverflow() {
		if t.ensureHashFields().has(tableKey{kind: StringKind, str: key}) {
			return tableStringFieldSlot{index: -1, token: t.stringShapeToken()}, true
		}
		return tableStringFieldSlot{}, false
	}
	for i := range t.stringFields {
		if t.stringFields[i].key == key {
			return tableStringFieldSlot{index: i, token: t.stringShapeToken()}, true
		}
	}
	return tableStringFieldSlot{}, false
}

func (t *Table) rawStringFieldAtIndex(index int, key string) (Value, bool) {
	if t == nil ||
		t.hasStringOverflow() ||
		index < 0 ||
		index >= len(t.stringFields) ||
		t.stringFields[index].key != key {
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

func (t *Table) rawStringFieldAtSlot(slot tableStringFieldSlot, key string) (Value, bool) {
	if !slot.token.matchesTableLayout(t) {
		return NilValue(), false
	}
	if t.hasStringOverflow() {
		if slot.token.storage != 1 {
			return NilValue(), false
		}
		return t.ensureHashFields().get(tableKey{kind: StringKind, str: key})
	}
	if slot.token.storage != 0 ||
		slot.index < 0 ||
		slot.index >= len(t.stringFields) ||
		t.stringFields[slot.index].key != key {
		return NilValue(), false
	}
	return t.stringFields[slot.index].value, true
}

func (t *Table) rawStringFieldAtExactCachedSlot(slot tableStringFieldSlot, key string) (Value, bool) {
	if t == nil ||
		slot.token.layout != t.stringVersion ||
		slot.token.metatable != t.metatable {
		return NilValue(), false
	}
	if t.hasStringOverflow() {
		if slot.token.storage != 1 {
			return NilValue(), false
		}
		return t.ensureHashFields().get(tableKey{kind: StringKind, str: key})
	}
	if slot.token.storage != 0 ||
		slot.index < 0 ||
		slot.index >= len(t.stringFields) ||
		t.stringFields[slot.index].key != key {
		return NilValue(), false
	}
	return t.stringFields[slot.index].value, true
}

func (t *Table) setRawStringFieldAtIndex(index int, key string, value Value) bool {
	if t == nil ||
		value.IsNil() ||
		t.hasStringOverflow() ||
		index < 0 ||
		index >= len(t.stringFields) ||
		t.stringFields[index].key != key {
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

func (t *Table) setRawStringFieldAtSlot(slot tableStringFieldSlot, key string, value Value) bool {
	if value.IsNil() || !slot.token.matchesTableLayout(t) {
		return false
	}
	if t.hasStringOverflow() {
		if slot.token.storage != 1 {
			return false
		}
		storedKey := tableKey{kind: StringKind, str: key}
		if !t.ensureHashFields().has(storedKey) {
			return false
		}
		t.ensureHashFields().set(storedKey, value)
		t.stringValueVersion++
		return true
	}
	if slot.token.storage != 0 ||
		slot.index < 0 ||
		slot.index >= len(t.stringFields) ||
		t.stringFields[slot.index].key != key {
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
		slot.token.metatable != t.metatable {
		return false
	}
	if t.hasStringOverflow() {
		if slot.token.storage != 1 {
			return false
		}
		storedKey := tableKey{kind: StringKind, str: key}
		if !t.ensureHashFields().has(storedKey) {
			return false
		}
		t.ensureHashFields().set(storedKey, value)
		t.stringValueVersion++
		return true
	}
	if slot.token.storage != 0 ||
		slot.index < 0 ||
		slot.index >= len(t.stringFields) ||
		t.stringFields[slot.index].key != key {
		return false
	}
	t.stringFields[slot.index].value = value
	t.stringValueVersion++
	return true
}

func (t *Table) addRawStringFieldNumber(key string, delta Value) (Value, bool) {
	if t == nil || delta.kind != NumberKind {
		return NilValue(), false
	}
	if t.hasStringOverflow() {
		storedKey := tableKey{kind: StringKind, str: key}
		current, ok := t.ensureHashFields().get(storedKey)
		if !ok || current.kind != NumberKind {
			return NilValue(), false
		}
		value := NumberValue(current.number + delta.number)
		t.ensureHashFields().set(storedKey, value)
		t.stringValueVersion++
		return value, true
	}
	for index := range t.stringFields {
		if t.stringFields[index].key != key {
			continue
		}
		current := t.stringFields[index].value
		if current.kind != NumberKind {
			return NilValue(), false
		}
		value := NumberValue(current.number + delta.number)
		t.stringFields[index].value = value
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
	if t.hasStringOverflow() {
		storedKey := tableKey{kind: StringKind, str: key}
		if !t.ensureHashFields().has(storedKey) {
			return false
		}
		t.ensureHashFields().set(storedKey, value)
		t.stringValueVersion++
		return true
	}
	for index := range t.stringFields {
		if t.stringFields[index].key != key {
			continue
		}
		t.stringFields[index].value = value
		t.stringValueVersion++
		return true
	}
	return false
}

func (t *Table) setRawStringField(key string, value Value) {
	if value.IsNil() {
		t.deleteRawStringField(key)
		return
	}
	if t.needsJournalForNewStringKey(key) {
		t.ensureIterationJournal()
	}
	if t.hasStringOverflow() {
		storedKey := tableKey{kind: StringKind, str: key}
		if added := t.ensureHashFields().set(storedKey, value); added {
			t.coldData().stringHashCount++
			t.stringVersion++
			t.markIterationKeyPresent(tableKey{kind: StringKind, str: key})
		}
		t.stringValueVersion++
		return
	}
	for i := range t.stringFields {
		if t.stringFields[i].key == key {
			t.stringFields[i].value = value
			t.stringValueVersion++
			return
		}
	}
	if len(t.stringFields) < maxInlineStringFields {
		if t.stringFields == nil {
			t.stringFields = tableInlineFields(t)[:0]
		}
		t.stringFields = append(t.stringFields, tableStringField{key: key, value: value})
		if t.iteration != nil {
			t.markIterationKeyPresent(tableKey{kind: StringKind, str: key})
		}
		t.stringVersion++
		t.stringValueVersion++
		return
	}
	t.ensureIterationJournal()
	fields := t.ensureHashFields()
	for _, field := range t.stringFields {
		if added := fields.set(tableKey{kind: StringKind, str: field.key}, field.value); added {
			t.coldData().stringHashCount++
		}
	}
	t.stringFields = nil
	if added := fields.set(tableKey{kind: StringKind, str: key}, value); added {
		t.coldData().stringHashCount++
	}
	t.markIterationKeyPresent(tableKey{kind: StringKind, str: key})
	t.stringVersion++
	t.stringValueVersion++
}

func (t *Table) deleteRawStringField(key string) {
	if t.hasStringOverflow() {
		if t.ensureHashFields().delete(tableKey{kind: StringKind, str: key}) {
			if t.cold != nil && t.cold.stringHashCount > 0 {
				t.cold.stringHashCount--
			}
			t.markIterationKeyDeleted(tableKey{kind: StringKind, str: key})
			t.stringVersion++
			t.stringValueVersion++
		}
		return
	}
	if t.iteration == nil && len(t.stringFields) > 1 {
		t.ensureIterationJournal()
	}
	for i := range t.stringFields {
		if t.stringFields[i].key != key {
			continue
		}
		last := len(t.stringFields) - 1
		t.stringFields[i] = t.stringFields[last]
		t.stringFields[last] = tableStringField{}
		t.stringFields = t.stringFields[:last]
		t.markIterationKeyDeleted(tableKey{kind: StringKind, str: key})
		t.stringVersion++
		t.stringValueVersion++
		return
	}
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
	if t.cold != nil &&
		t.cold.indexCacheMetatable == metatable &&
		t.cold.indexCacheVersion == metatable.stringValueVersion &&
		t.cold.indexCacheReady {
		return t.cold.indexCacheValue, !t.cold.indexCacheValue.IsNil(), nil
	}
	index, err := metatable.rawGetString("__index")
	if err != nil {
		return NilValue(), false, err
	}
	cold := t.coldData()
	cold.indexCacheMetatable = metatable
	cold.indexCacheVersion = metatable.stringValueVersion
	cold.indexCacheValue = index
	cold.indexCacheReady = true
	return index, !index.IsNil(), nil
}

func (t *Table) cachedNewIndexFallback() (Value, bool, error) {
	if t == nil || t.metatable == nil {
		return NilValue(), false, nil
	}
	metatable := t.metatable
	if t.cold != nil &&
		t.cold.newIndexCacheMetatable == metatable &&
		t.cold.newIndexCacheVersion == metatable.stringValueVersion &&
		t.cold.newIndexCacheReady {
		return t.cold.newIndexCacheValue, !t.cold.newIndexCacheValue.IsNil(), nil
	}
	newIndex, err := metatable.rawGetString("__newindex")
	if err != nil {
		return NilValue(), false, err
	}
	cold := t.coldData()
	cold.newIndexCacheMetatable = metatable
	cold.newIndexCacheVersion = metatable.stringValueVersion
	cold.newIndexCacheValue = newIndex
	cold.newIndexCacheReady = true
	return newIndex, !newIndex.IsNil(), nil
}

func tableArrayIndexFromValue(v Value) (int, bool) {
	if v.kind != NumberKind {
		return 0, false
	}
	if math.IsNaN(v.number) || v.number < 1 || math.Trunc(v.number) != v.number {
		return 0, false
	}
	maxInt := int(^uint(0) >> 1)
	if v.number > float64(maxInt) {
		return 0, false
	}
	return int(v.number), true
}

func tableKeyFromValue(v Value) (tableKey, bool) {
	switch v.kind {
	case BoolKind:
		return tableKey{kind: BoolKind, bool: v.bool}, true
	case NumberKind:
		if math.IsNaN(v.number) {
			return tableKey{}, false
		}
		return tableKey{kind: NumberKind, number: v.number}, true
	case StringKind:
		return tableKey{kind: StringKind, str: v.stringText()}, true
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
	if key.kind == NumberKind && math.IsNaN(key.number) {
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

func (v Value) nativeFunction() (nativeFunc, bool) {
	if v.kind != HostFuncKind {
		return nil, false
	}
	if v.nativeID != nativeFuncUnknown {
		return nativeFuncByID(v.nativeID)
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
	if v.kind != HostFuncKind || v.ref == nil {
		return nil
	}
	return (*hostCallable)(v.ref)
}

func (v Value) scriptFunction() (*closure, bool) {
	if v.kind != FunctionKind || v.ref == nil {
		return nil, false
	}
	return (*closure)(v.ref), true
}

func (v Value) truthy() bool {
	if v.kind == NilKind {
		return false
	}
	if v.kind == BoolKind {
		return v.bool
	}
	return true
}

func valuesEqual(left Value, right Value) bool {
	if left.kind != right.kind {
		return false
	}

	switch left.kind {
	case NilKind:
		return true
	case BoolKind:
		return left.bool == right.bool
	case NumberKind:
		if math.IsNaN(left.number) || math.IsNaN(right.number) {
			return false
		}
		return left.number == right.number
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
	return left.hash == right.hash && left.text == right.text
}

func valuesLess(left Value, right Value) (bool, error) {
	if left.kind != right.kind {
		return false, fmt.Errorf("compare operands are %s and %s", left.Kind(), right.Kind())
	}

	switch left.kind {
	case NumberKind:
		if math.IsNaN(left.number) || math.IsNaN(right.number) {
			return false, fmt.Errorf("compare operand is NaN")
		}
		return left.number < right.number, nil
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
		switch value.kind {
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
	if text, ok := smallNonNegativeIntegerString(number); ok {
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
	if text, ok := smallNonNegativeIntegerString(number); ok {
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

func smallNonNegativeIntegerString(number float64) (string, bool) {
	if number != math.Trunc(number) || math.Signbit(number) {
		return "", false
	}
	index := int(number)
	if index < 0 || index >= len(smallNonNegativeIntegerStrings) || float64(index) != number {
		return "", false
	}
	return smallNonNegativeIntegerStrings[index], true
}

var smallNonNegativeIntegerStrings = func() [1000]string {
	var values [1000]string
	for i := range values {
		values[i] = strconv.Itoa(i)
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
