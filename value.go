package ember

import (
	"fmt"
	"math"
)

// ValueKind names the kind of data stored in a Value.
type ValueKind string

const (
	// NilKind is the kind for the Luau nil value.
	NilKind ValueKind = "nil"
	// BoolKind is the kind for Luau boolean values.
	BoolKind ValueKind = "boolean"
	// NumberKind is the kind for Luau number values.
	NumberKind ValueKind = "number"
	// StringKind is the kind for Luau string values.
	StringKind ValueKind = "string"
	// TableKind is the kind for Luau table values.
	TableKind ValueKind = "table"
	// HostFuncKind is the kind for Go host callback values.
	HostFuncKind ValueKind = "host_function"
)

// HostFunc is a Go callback callable from Ember scripts.
type HostFunc func(args []Value) ([]Value, error)

// Value is an Ember runtime value.
type Value struct {
	kind     ValueKind
	bool     bool
	number   float64
	str      string
	table    *Table
	hostFunc HostFunc
}

// Table is a Luau table object.
type Table struct {
	fields map[tableKey]Value
}

type tableKey struct {
	kind   ValueKind
	bool   bool
	number float64
	str    string
	table  *Table
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
		hostFunc: fn,
	}
}

// NewTable returns an empty Luau table.
func NewTable() *Table {
	return &Table{
		fields: make(map[tableKey]Value),
	}
}

// TableValue returns a Luau table value backed by table.
func TableValue(table *Table) Value {
	return Value{
		kind:  TableKind,
		table: table,
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

// Get returns the table value stored at key, or nil when the key is missing.
func (t *Table) Get(key Value) (Value, error) {
	if t == nil {
		return NilValue(), fmt.Errorf("table: nil table")
	}
	storedKey, ok := tableKeyFromValue(key)
	if err := validateTableKey(key, ok); err != nil {
		return NilValue(), err
	}
	if t.fields == nil {
		return NilValue(), nil
	}
	value, ok := t.fields[storedKey]
	if !ok {
		return NilValue(), nil
	}
	return value, nil
}

// Set stores value at key. Setting nil deletes the key.
func (t *Table) Set(key Value, value Value) error {
	if t == nil {
		return fmt.Errorf("table: nil table")
	}
	storedKey, ok := tableKeyFromValue(key)
	if err := validateTableKey(key, ok); err != nil {
		return err
	}
	if value.IsNil() {
		delete(t.fields, storedKey)
		return nil
	}
	if t.fields == nil {
		t.fields = make(map[tableKey]Value)
	}
	t.fields[storedKey] = value
	return nil
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
	default:
		return tableKey{}, false
	}
}

func validateTableKey(key Value, ok bool) error {
	if ok {
		return nil
	}
	if key.kind == NumberKind && math.IsNaN(key.number) {
		return fmt.Errorf("table: key is NaN")
	}
	return fmt.Errorf("table: key is %s, want boolean, string, number, or table", key.Kind())
}

func (v Value) hostFunction() (HostFunc, bool) {
	if v.kind != HostFuncKind {
		return nil, false
	}
	return v.hostFunc, true
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
