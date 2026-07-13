package ember

import "fmt"

const metatableWalkInlineLimit = 8

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
	depth := 0
	for {
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
		if seen != nil {
			if seen[table] {
				return NilValue(), fmt.Errorf("table: cyclic __index chain")
			}
			seen[table] = true
		} else if depth >= metatableWalkInlineLimit {
			seen = make(map[*Table]bool)
			seen[table] = true
		}

		index, ok, err := table.cachedIndexFallback()
		if err != nil {
			return NilValue(), err
		}
		if !ok {
			return NilValue(), nil
		}
		if indexTable, ok := index.Table(); ok {
			table = indexTable
			depth++
			continue
		}
		if a.functionMetamethods && callableValue(index) {
			return a.callIndex(index, table, key)
		}
		if a.functionMetamethods {
			return NilValue(), fmt.Errorf("table: __index is %s, want table or function", index.Kind())
		}
		return NilValue(), fmt.Errorf("table: __index is %s, want table", index.Kind())
	}
}

func (a tableAccess) set(table *Table, key Value, value Value) error {
	return a.setSeen(table, key, value, nil)
}

func (a tableAccess) setSeen(table *Table, key Value, value Value, seen map[*Table]bool) error {
	depth := 0
	for {
		current, err := table.rawGet(key)
		if err != nil {
			return err
		}
		if !current.IsNil() || table == nil || table.metatable == nil {
			return table.rawSet(key, value)
		}
		if seen != nil {
			if seen[table] {
				return fmt.Errorf("table: cyclic __newindex chain")
			}
			seen[table] = true
		} else if depth >= metatableWalkInlineLimit {
			seen = make(map[*Table]bool)
			seen[table] = true
		}

		newIndex, ok, err := table.cachedNewIndexFallback()
		if err != nil {
			return err
		}
		if !ok {
			return table.rawSet(key, value)
		}
		if newIndexTable, ok := newIndex.Table(); ok {
			table = newIndexTable
			depth++
			continue
		}
		if a.functionMetamethods && callableValue(newIndex) {
			return a.callNewIndex(newIndex, table, key, value)
		}
		if a.functionMetamethods {
			return fmt.Errorf("table: __newindex is %s, want table or function", newIndex.Kind())
		}
		return fmt.Errorf("table: __newindex is %s, want table", newIndex.Kind())
	}
}

func (a tableAccess) protectedMetatable(table *Table) (Value, error) {
	if table == nil || table.metatable == nil {
		return NilValue(), nil
	}
	return table.metatable.rawGet(StringValue("__metatable"))
}

func (a tableAccess) callIndex(fn Value, table *Table, key Value) (Value, error) {
	if a.globals != nil && a.globals.thread != nil {
		if closure, ok := fn.scriptFunction(); ok {
			thread := a.globals.thread
			restore := thread.enterNonYieldable()
			thread.executionWindow.commit()
			value, err := thread.runInlineScriptCallFixedOneNoHook(closure, TableValue(table), key, NilValue(), 2)
			thread.executionWindow.refresh()
			restore()
			return value, err
		}
	}
	results, err := callRuntimeMetamethodWindow2(fn, a.globals, TableValue(table), key)
	if err != nil {
		return NilValue(), err
	}
	return results.at(0), nil
}

func (a tableAccess) callNewIndex(fn Value, table *Table, key Value, value Value) error {
	if a.globals != nil && a.globals.thread != nil {
		if closure, ok := fn.scriptFunction(); ok {
			thread := a.globals.thread
			restore := thread.enterNonYieldable()
			thread.executionWindow.commit()
			_, err := thread.runInlineScriptCallFixedOneNoHook(closure, TableValue(table), key, value, 3)
			thread.executionWindow.refresh()
			restore()
			return err
		}
	}
	_, err := callRuntimeMetamethodWindow3(fn, a.globals, TableValue(table), key, value)
	return err
}
