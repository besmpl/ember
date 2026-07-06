package ember

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
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
	nativeFuncArrayNext
)

// Value is an Ember runtime value.
type Value struct {
	kind     ValueKind
	bool     bool
	nativeID nativeFuncID
	number   float64
	str      string
	table    *Table
	userdata *UserData
	function *closure
	callable *hostCallable
}

type hostCallable struct {
	hostFunc      HostFunc
	native        nativeFunc
	yieldableHost yieldableHostFunc
}

type cell struct {
	value Value
}

type closure struct {
	proto    *Proto
	upvalues []*cell
}

// UserData is an opaque Go-owned host object passed through Ember scripts.
type UserData struct {
	payload any
}

// Table is a Luau table object.
type Table struct {
	array          []Value
	arrayHasNil    bool
	stringFields   []tableStringField
	stringFieldMap map[string]Value
	fields         map[tableKey]Value
	metatable      *Table
	// Layout versions track key/storage changes; value versions track stored value
	// changes for each independent table storage family.
	stringVersion       uint32
	stringValueVersion  uint32
	arrayVersion        uint32
	arrayValueVersion   uint32
	genericVersion      uint32
	genericValueVersion uint32
	indexCacheMetatable *Table
	indexCacheVersion   uint32
	indexCacheTable     *Table
}

type tableStringField struct {
	key   string
	value Value
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
	if table.stringFieldMap != nil {
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
	if t.stringFieldMap != nil {
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

type tableKey struct {
	kind     ValueKind
	bool     bool
	number   float64
	str      string
	table    *Table
	userdata *UserData
}

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
	return Value{
		kind: StringKind,
		str:  s,
	}
}

// HostFuncValue returns a Go host callback value.
func HostFuncValue(fn HostFunc) Value {
	return Value{
		kind:     HostFuncKind,
		callable: &hostCallable{hostFunc: fn},
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
		callable: &hostCallable{native: fn},
	}
}

func yieldableHostFuncValue(fn yieldableHostFunc) Value {
	return Value{
		kind:     HostFuncKind,
		callable: &hostCallable{yieldableHost: fn},
	}
}

// NewUserData returns an opaque host object carrying payload.
func NewUserData(payload any) *UserData {
	return &UserData{
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
		kind:     UserDataKind,
		userdata: userdata,
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
	table := &Table{
		array: make([]Value, 0, arrayCapacity),
	}
	if fieldCapacity > maxInlineStringFields {
		table.stringFieldMap = make(map[string]Value, fieldCapacity)
	} else if fieldCapacity > 0 {
		table.stringFields = make([]tableStringField, 0, fieldCapacity)
	}
	return table
}

// TableValue returns a Luau table value backed by table.
func TableValue(table *Table) Value {
	return Value{
		kind:  TableKind,
		table: table,
	}
}

func functionValue(proto *Proto, upvalues []*cell) Value {
	return Value{
		kind:     FunctionKind,
		function: &closure{proto: proto, upvalues: upvalues},
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
	return v.str, true
}

// Table returns the table object and whether this Value is a table.
func (v Value) Table() (*Table, bool) {
	if v.kind != TableKind || v.table == nil {
		return nil, false
	}
	return v.table, true
}

// UserData returns the userdata object and whether this Value is userdata.
func (v Value) UserData() (*UserData, bool) {
	if v.kind != UserDataKind || v.userdata == nil {
		return nil, false
	}
	return v.userdata, true
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
	if t.fields == nil {
		t.fields = make(map[tableKey]Value)
	}
	if _, ok := t.fields[storedKey]; !ok {
		t.genericVersion++
	}
	t.fields[storedKey] = value
	t.genericValueVersion++
}

func (t *Table) deleteRawGenericField(storedKey tableKey) {
	if t.fields == nil {
		return
	}
	if _, ok := t.fields[storedKey]; !ok {
		return
	}
	delete(t.fields, storedKey)
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
			t.arrayVersion++
		}
		t.array[index-1] = value
		t.arrayValueVersion++
		t.deleteRawGenericField(key)
		return nil
	}
	if index == len(t.array)+1 {
		t.array = append(t.array, value)
		t.arrayVersion++
		t.arrayValueVersion++
		t.deleteRawGenericField(key)
		t.promoteContiguousArrayFields()
		return nil
	}
	t.setRawGenericField(key, value)
	return nil
}

func (t *Table) promoteContiguousArrayFields() {
	for {
		next := len(t.array) + 1
		key := tableKey{kind: NumberKind, number: float64(next)}
		value, ok := t.fields[key]
		if !ok || value.IsNil() {
			return
		}
		t.array = append(t.array, value)
		t.arrayVersion++
		t.arrayValueVersion++
		t.deleteRawGenericField(key)
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
	t.indexCacheMetatable = nil
	t.indexCacheVersion = 0
	t.indexCacheTable = nil
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
		len(table.stringFields) == 0 && len(table.stringFieldMap) == 0 && len(table.fields) == 0
}

func (t *Table) rawNext(key Value) (Value, Value, error) {
	if t == nil {
		return NilValue(), NilValue(), fmt.Errorf("table: nil table")
	}
	keys := t.sortedKeys()
	if key.IsNil() {
		if len(keys) == 0 {
			return NilValue(), NilValue(), nil
		}
		nextKey := keys[0]
		value, err := t.rawGet(nextKey.value())
		if err != nil {
			return NilValue(), NilValue(), err
		}
		return nextKey.value(), value, nil
	}

	storedKey, ok := tableKeyFromValue(key)
	if err := validateTableKey(key, ok); err != nil {
		return NilValue(), NilValue(), err
	}
	for i, candidate := range keys {
		if candidate == storedKey {
			if i+1 >= len(keys) {
				return NilValue(), NilValue(), nil
			}
			nextKey := keys[i+1]
			value, err := t.rawGet(nextKey.value())
			if err != nil {
				return NilValue(), NilValue(), err
			}
			return nextKey.value(), value, nil
		}
	}
	return NilValue(), NilValue(), fmt.Errorf("invalid key")
}

func (t *Table) sortedKeys() []tableKey {
	if t == nil || (len(t.stringFields) == 0 && len(t.stringFieldMap) == 0 && len(t.fields) == 0 && len(t.array) == 0) {
		return nil
	}
	keys := make([]tableKey, 0, len(t.stringFields)+len(t.stringFieldMap)+len(t.fields)+len(t.array))
	for index, value := range t.array {
		if !value.IsNil() {
			keys = append(keys, tableKey{kind: NumberKind, number: float64(index + 1)})
		}
	}
	for _, field := range t.stringFields {
		if !field.value.IsNil() {
			keys = append(keys, tableKey{kind: StringKind, str: field.key})
		}
	}
	for key, value := range t.stringFieldMap {
		if !value.IsNil() {
			keys = append(keys, tableKey{kind: StringKind, str: key})
		}
	}
	for key, value := range t.fields {
		if !value.IsNil() {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i int, j int) bool {
		return keys[i].less(keys[j])
	})
	return keys
}

func (t *Table) rawStringField(key string) (Value, bool) {
	if t.stringFieldMap != nil {
		value, ok := t.stringFieldMap[key]
		return value, ok
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
	if t == nil || t.fields == nil {
		return NilValue(), false
	}
	value, ok := t.fields[key]
	if !ok || value.IsNil() {
		return NilValue(), false
	}
	return value, true
}

func (t *Table) rawStringFieldSlot(key string) (tableStringFieldSlot, bool) {
	if t == nil {
		return tableStringFieldSlot{}, false
	}
	if t.stringFieldMap != nil {
		if _, ok := t.stringFieldMap[key]; ok {
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
		t.stringFieldMap != nil ||
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
	if t.stringFieldMap != nil {
		if slot.token.storage != 1 {
			return NilValue(), false
		}
		value, ok := t.stringFieldMap[key]
		if !ok {
			return NilValue(), false
		}
		return value, true
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
		t.stringFieldMap != nil ||
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
	if t.stringFieldMap != nil {
		if slot.token.storage != 1 {
			return false
		}
		if _, ok := t.stringFieldMap[key]; !ok {
			return false
		}
		t.stringFieldMap[key] = value
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

func (t *Table) setRawStringField(key string, value Value) {
	if value.IsNil() {
		t.deleteRawStringField(key)
		return
	}
	if t.stringFieldMap != nil {
		if _, ok := t.stringFieldMap[key]; !ok {
			t.stringVersion++
		}
		t.stringFieldMap[key] = value
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
		t.stringFields = append(t.stringFields, tableStringField{key: key, value: value})
		t.stringVersion++
		t.stringValueVersion++
		return
	}
	t.stringFieldMap = make(map[string]Value, len(t.stringFields)+1)
	for _, field := range t.stringFields {
		t.stringFieldMap[field.key] = field.value
	}
	t.stringFields = nil
	t.stringFieldMap[key] = value
	t.stringVersion++
	t.stringValueVersion++
}

func (t *Table) deleteRawStringField(key string) {
	if t.stringFieldMap != nil {
		if _, ok := t.stringFieldMap[key]; ok {
			delete(t.stringFieldMap, key)
			t.stringVersion++
			t.stringValueVersion++
		}
		return
	}
	for i := range t.stringFields {
		if t.stringFields[i].key != key {
			continue
		}
		last := len(t.stringFields) - 1
		t.stringFields[i] = t.stringFields[last]
		t.stringFields[last] = tableStringField{}
		t.stringFields = t.stringFields[:last]
		t.stringVersion++
		t.stringValueVersion++
		return
	}
}

func (t *Table) cachedIndexTable() (*Table, bool, error) {
	if t == nil || t.metatable == nil {
		return nil, false, nil
	}
	metatable := t.metatable
	if t.indexCacheMetatable == metatable &&
		t.indexCacheVersion == metatable.stringValueVersion &&
		t.indexCacheTable != nil {
		return t.indexCacheTable, true, nil
	}
	index, err := metatable.rawGetString("__index")
	if err != nil {
		return nil, false, err
	}
	indexTable, ok := index.Table()
	if !ok {
		return nil, false, nil
	}
	t.indexCacheMetatable = metatable
	t.indexCacheVersion = metatable.stringValueVersion
	t.indexCacheTable = indexTable
	return indexTable, true, nil
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
		return tableKey{kind: StringKind, str: v.str}, true
	case TableKind:
		if v.table == nil {
			return tableKey{}, false
		}
		return tableKey{kind: TableKind, table: v.table}, true
	case UserDataKind:
		if v.userdata == nil {
			return tableKey{}, false
		}
		return tableKey{kind: UserDataKind, userdata: v.userdata}, true
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
		return fmt.Sprintf("%p", k.table) < fmt.Sprintf("%p", other.table)
	case UserDataKind:
		return fmt.Sprintf("%p", k.userdata) < fmt.Sprintf("%p", other.userdata)
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
	if v.kind != HostFuncKind || v.callable == nil || v.callable.hostFunc == nil {
		return nil, false
	}
	return v.callable.hostFunc, true
}

func (v Value) nativeFunction() (nativeFunc, bool) {
	if v.kind != HostFuncKind {
		return nil, false
	}
	if v.nativeID != nativeFuncUnknown {
		return nativeFuncByID(v.nativeID)
	}
	if v.callable == nil || v.callable.native == nil {
		return nil, false
	}
	return v.callable.native, true
}

func (v Value) yieldableHostFunction() (yieldableHostFunc, bool) {
	if v.kind != HostFuncKind || v.callable == nil || v.callable.yieldableHost == nil {
		return nil, false
	}
	return v.callable.yieldableHost, true
}

func (v Value) scriptFunction() (*closure, bool) {
	if v.kind != FunctionKind || v.function == nil {
		return nil, false
	}
	return v.function, true
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
		return left.str == right.str
	case TableKind:
		return left.table != nil && left.table == right.table
	case UserDataKind:
		return left.userdata != nil && left.userdata == right.userdata
	case FunctionKind:
		return left.function != nil && left.function == right.function
	case HostFuncKind:
		return false
	default:
		return false
	}
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
		return left.str < right.str, nil
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
	if number, ok := value.Number(); ok {
		return number, nil
	}
	if str, ok := value.String(); ok {
		number, err := strconv.ParseFloat(str, 64)
		if err == nil {
			return number, nil
		}
	}
	operand := "operand"
	if side != "" {
		operand = side + " operand"
	}
	return 0, fmt.Errorf("%s %s is %s, want number", op, operand, value.Kind())
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

func concatOperandString(value Value, side string) (string, error) {
	if str, ok := value.String(); ok {
		return str, nil
	}
	if number, ok := value.Number(); ok {
		return strconv.FormatFloat(number, 'g', -1, 64), nil
	}
	return "", fmt.Errorf("concat %s operand is %s, want string or number", side, value.Kind())
}
