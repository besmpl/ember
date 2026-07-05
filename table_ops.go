package ember

import "fmt"

type tableAccess struct {
	globals             *globalEnv
	functionMetamethods bool
}

func publicTableAccess() tableAccess {
	return tableAccess{}
}

func runtimeTableAccess(globals *globalEnv) tableAccess {
	return tableAccess{
		globals:             globals,
		functionMetamethods: true,
	}
}

func (a tableAccess) get(table *Table, key Value) (Value, error) {
	return a.getSeen(table, key, nil)
}

func (a tableAccess) getString(table *Table, key string, keyValue Value) (Value, error) {
	if value, ok := table.rawStringField(key); ok {
		return value, nil
	}
	if table == nil || table.metatable == nil {
		return NilValue(), nil
	}
	return a.getSeen(table, keyValue, nil)
}

func (a tableAccess) getSeen(table *Table, key Value, seen map[*Table]bool) (Value, error) {
	value, err := table.rawGet(key)
	if err != nil {
		return NilValue(), err
	}
	if !value.IsNil() {
		return value, nil
	}
	if table == nil || table.metatable == nil {
		return NilValue(), nil
	}
	if seen != nil && seen[table] {
		return NilValue(), fmt.Errorf("table: cyclic __index chain")
	}
	if seen == nil {
		seen = make(map[*Table]bool)
	}
	seen[table] = true

	if indexTable, ok, err := table.cachedIndexTable(); err != nil {
		return NilValue(), err
	} else if ok {
		return a.getSeen(indexTable, key, seen)
	}

	index, err := table.metatable.rawGet(StringValue("__index"))
	if err != nil {
		return NilValue(), err
	}
	if index.IsNil() {
		return NilValue(), nil
	}
	if indexTable, ok := index.Table(); ok {
		return a.getSeen(indexTable, key, seen)
	}
	if a.functionMetamethods && callableValue(index) {
		return a.callIndex(index, table, key)
	}
	if a.functionMetamethods {
		return NilValue(), fmt.Errorf("table: __index is %s, want table or function", index.Kind())
	}
	return NilValue(), fmt.Errorf("table: __index is %s, want table", index.Kind())
}

func (a tableAccess) set(table *Table, key Value, value Value) error {
	return a.setSeen(table, key, value, nil)
}

func (a tableAccess) setSeen(table *Table, key Value, value Value, seen map[*Table]bool) error {
	current, err := table.rawGet(key)
	if err != nil {
		return err
	}
	if !current.IsNil() || table == nil || table.metatable == nil {
		return table.rawSet(key, value)
	}
	if seen != nil && seen[table] {
		return fmt.Errorf("table: cyclic __newindex chain")
	}
	if seen == nil {
		seen = make(map[*Table]bool)
	}
	seen[table] = true

	newIndex, err := table.metatable.rawGet(StringValue("__newindex"))
	if err != nil {
		return err
	}
	if newIndex.IsNil() {
		return table.rawSet(key, value)
	}
	if newIndexTable, ok := newIndex.Table(); ok {
		return a.setSeen(newIndexTable, key, value, seen)
	}
	if a.functionMetamethods && callableValue(newIndex) {
		return a.callNewIndex(newIndex, table, key, value)
	}
	if a.functionMetamethods {
		return fmt.Errorf("table: __newindex is %s, want table or function", newIndex.Kind())
	}
	return fmt.Errorf("table: __newindex is %s, want table", newIndex.Kind())
}

func (a tableAccess) protectedMetatable(table *Table) (Value, error) {
	if table == nil || table.metatable == nil {
		return NilValue(), nil
	}
	return table.metatable.rawGet(StringValue("__metatable"))
}

func (a tableAccess) callIndex(fn Value, table *Table, key Value) (Value, error) {
	results, err := callRuntimeMetamethod(fn, a.globals, []Value{TableValue(table), key})
	if err != nil {
		return NilValue(), err
	}
	return adjustedResultAt(results, 0), nil
}

func (a tableAccess) callNewIndex(fn Value, table *Table, key Value, value Value) error {
	_, err := callRuntimeMetamethod(fn, a.globals, []Value{TableValue(table), key, value})
	return err
}
