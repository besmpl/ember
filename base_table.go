package ember

import (
	"fmt"
	"math"
	"strings"
)

func baseSetMetatable(globals *globalEnv, args []Value) ([]Value, error) {
	table, err := tableArg("setmetatable", args, 0)
	if err != nil {
		return nil, err
	}
	if protected, err := runtimeTableAccess(globals).protectedMetatable(table); err != nil {
		return nil, err
	} else if !protected.IsNil() {
		return nil, fmt.Errorf("setmetatable: cannot change protected metatable")
	}

	if len(args) < 2 || args[1].IsNil() {
		table.setMetatable(nil)
		return []Value{TableValue(table)}, nil
	}
	metatable, ok := args[1].Table()
	if !ok {
		return nil, fmt.Errorf("setmetatable: argument #2 is %s, want table or nil", args[1].Kind())
	}
	table.setMetatable(metatable)
	return []Value{TableValue(table)}, nil
}

func baseGetMetatable(globals *globalEnv, args []Value) ([]Value, error) {
	table, err := tableArg("getmetatable", args, 0)
	if err != nil {
		return nil, err
	}
	if table.metatable == nil {
		return []Value{NilValue()}, nil
	}
	protected, err := runtimeTableAccess(globals).protectedMetatable(table)
	if err != nil {
		return nil, err
	}
	if !protected.IsNil() {
		return []Value{protected}, nil
	}
	return []Value{TableValue(table.metatable)}, nil
}

func baseRawGet(args []Value) ([]Value, error) {
	table, err := tableArg("rawget", args, 0)
	if err != nil {
		return nil, err
	}
	key := NilValue()
	if len(args) > 1 {
		key = args[1]
	}
	value, err := table.rawGet(key)
	if err != nil {
		return nil, err
	}
	return []Value{value}, nil
}

func baseRawSet(args []Value) ([]Value, error) {
	table, err := tableArg("rawset", args, 0)
	if err != nil {
		return nil, err
	}
	key := NilValue()
	if len(args) > 1 {
		key = args[1]
	}
	value := NilValue()
	if len(args) > 2 {
		value = args[2]
	}
	if err := table.rawSet(key, value); err != nil {
		return nil, err
	}
	return []Value{TableValue(table)}, nil
}

func baseRawLen(args []Value) ([]Value, error) {
	length, err := baseRawLenValue(args)
	if err != nil {
		return nil, err
	}
	return []Value{length}, nil
}

func baseRawLenValue(args []Value) (Value, error) {
	value := NilValue()
	if len(args) > 0 {
		value = args[0]
	}
	length, err := rawLength(value)
	if err != nil {
		return NilValue(), fmt.Errorf("rawlen: %w", err)
	}
	return NumberValue(float64(length)), nil
}

func baseRawLenNative(_ *globalEnv, args []Value) ([]Value, error) {
	return baseRawLen(args)
}

func baseSelect(args []Value) ([]Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("select: argument #1 is nil, want number or #")
	}
	if marker, ok := args[0].String(); ok && marker == "#" {
		return []Value{NumberValue(float64(len(args) - 1))}, nil
	}
	index, ok := args[0].Number()
	if !ok {
		return nil, fmt.Errorf("select: argument #1 is %s, want number or #", args[0].Kind())
	}
	if index != math.Trunc(index) {
		return nil, fmt.Errorf("select: argument #1 is %v, want integer", index)
	}
	if index == 0 {
		return nil, fmt.Errorf("select: argument #1 is 0, want non-zero index")
	}
	count := len(args) - 1
	start := int(index)
	if start < 0 {
		start = count + start + 1
	}
	if start < 1 {
		return nil, fmt.Errorf("select: argument #1 is %v, want index in range", index)
	}
	if start > count {
		return nil, nil
	}
	return append([]Value(nil), args[start:]...), nil
}

func baseSelectNative(_ *globalEnv, args []Value) ([]Value, error) {
	return baseSelect(args)
}

func basePCall(globals *globalEnv, args []Value) ([]Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("pcall: missing function")
	}
	if !callableValue(args[0]) {
		return nil, fmt.Errorf("pcall: argument is %s, want function", args[0].Kind())
	}
	results, err := protectedCallValue(args[0], globals, args[1:])
	if err != nil {
		if yield, ok := err.(vmYieldRequest); ok {
			return nil, protectedYieldRequest(yield, NilValue(), false)
		}
		if isVMHostInterrupt(err) {
			return nil, err
		}
		if isProtectedBoundaryError(err) {
			return nil, err
		}
		return []Value{BoolValue(false), StringValue(err.Error())}, nil
	}
	return append([]Value{BoolValue(true)}, results...), nil
}

func baseXPCall(globals *globalEnv, args []Value) ([]Value, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("xpcall: missing function")
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("xpcall: missing error handler")
	}
	if !callableValue(args[0]) {
		return nil, fmt.Errorf("xpcall: argument #1 is %s, want function", args[0].Kind())
	}
	if !callableValue(args[1]) {
		return nil, fmt.Errorf("xpcall: argument #2 is %s, want function", args[1].Kind())
	}
	results, err := protectedCallValue(args[0], globals, args[2:])
	if err == nil {
		return append([]Value{BoolValue(true)}, results...), nil
	}
	if yield, ok := err.(vmYieldRequest); ok {
		return nil, protectedYieldRequest(yield, args[1], true)
	}
	if isVMHostInterrupt(err) {
		return nil, err
	}
	if isProtectedBoundaryError(err) {
		return nil, err
	}
	handled, handlerErr := protectedErrorHandlerCall(globals, args[1], StringValue(err.Error()))
	if handlerErr != nil {
		if _, ok := handlerErr.(vmYieldRequest); ok {
			return nil, handlerErr
		}
		if isVMHostInterrupt(handlerErr) {
			return nil, handlerErr
		}
		if isProtectedBoundaryError(handlerErr) {
			return nil, handlerErr
		}
		return []Value{BoolValue(false), StringValue(handlerErr.Error())}, nil
	}
	return append([]Value{BoolValue(false)}, handled...), nil
}

func protectedErrorHandlerCall(globals *globalEnv, handler Value, message Value) ([]Value, error) {
	if globals == nil || globals.thread == nil {
		return protectedCallValue(handler, globals, []Value{message})
	}
	restore := globals.thread.enterNonYieldable()
	defer restore()
	return protectedCallValue(handler, globals, []Value{message})
}

func baseNext(args []Value) ([]Value, error) {
	table, err := tableArg("next", args, 0)
	if err != nil {
		return nil, err
	}
	key := NilValue()
	if len(args) > 1 {
		key = args[1]
	}
	nextKey, value, err := table.rawNext(key)
	if err != nil {
		return nil, fmt.Errorf("next: %w", err)
	}
	if nextKey.IsNil() {
		return []Value{NilValue()}, nil
	}
	return []Value{nextKey, value}, nil
}

func baseNextNative(_ *globalEnv, args []Value) ([]Value, error) {
	return baseNext(args)
}

func basePairs(args []Value) ([]Value, error) {
	table, err := tableArg("pairs", args, 0)
	if err != nil {
		return nil, err
	}
	return []Value{nativeFuncValueWithID(baseNextNative, nativeFuncNext), TableValue(table), NilValue()}, nil
}

func baseIPairs(args []Value) ([]Value, error) {
	table, err := tableArg("ipairs", args, 0)
	if err != nil {
		return nil, err
	}
	return []Value{HostFuncValue(baseINext), TableValue(table), NilValue()}, nil
}

func baseINext(args []Value) ([]Value, error) {
	table, err := tableArg("ipairs iterator", args, 0)
	if err != nil {
		return nil, err
	}
	var index float64
	if len(args) > 1 && !args[1].IsNil() {
		var ok bool
		index, ok = args[1].Number()
		if !ok {
			return nil, fmt.Errorf("ipairs iterator: index is %s, want number or nil", args[1].Kind())
		}
	}
	nextIndex := index + 1
	value, err := table.rawGet(NumberValue(nextIndex))
	if err != nil {
		return nil, fmt.Errorf("ipairs iterator: %w", err)
	}
	if value.IsNil() {
		return []Value{NilValue()}, nil
	}
	return []Value{NumberValue(nextIndex), value}, nil
}

func baseTable() *Table {
	table := newTableWithCapacity(0, 8)
	_ = table.Set(StringValue("pack"), nativeFuncValue(baseTablePackNative))
	_ = table.Set(StringValue("unpack"), HostFuncValue(baseTableUnpack))
	_ = table.Set(StringValue("insert"), nativeFuncValueWithID(baseTableInsertNative, nativeFuncTableInsert))
	_ = table.Set(StringValue("remove"), nativeFuncValueWithID(baseTableRemoveNative, nativeFuncTableRemove))
	_ = table.Set(StringValue("concat"), nativeFuncValue(baseTableConcatNative))
	_ = table.Set(StringValue("find"), HostFuncValue(baseTableFind))
	_ = table.Set(StringValue("clear"), HostFuncValue(baseTableClear))
	_ = table.Set(StringValue("sort"), nativeFuncValue(baseTableSort))
	return table
}

func baseTablePack(args []Value) ([]Value, error) {
	return baseTablePackWithController(nil, args)
}

func baseTablePackNative(globals *globalEnv, args []Value) ([]Value, error) {
	return baseTablePackWithController(executionControllerForGlobals(globals), args)
}

func baseTablePackWithController(controller *executionController, args []Value) ([]Value, error) {
	if controller != nil {
		if err := controller.chargeRuntimeObject(); err != nil {
			return nil, err
		}
	}
	table := NewTable()
	seq := newRawSequenceWithController("table.pack", table, controller)
	if err := seq.writeValues(args); err != nil {
		return nil, err
	}
	if err := table.rawSetWithController(controller, StringValue("n"), NumberValue(float64(len(args)))); err != nil {
		return nil, fmt.Errorf("table.pack: %w", err)
	}
	return []Value{TableValue(table)}, nil
}

func baseTableUnpack(args []Value) ([]Value, error) {
	table, err := tableArg("table.unpack", args, 0)
	if err != nil {
		return nil, err
	}
	seq := newRawSequence("table.unpack", table)
	start := 1
	if len(args) > 1 && !args[1].IsNil() {
		start, err = integerArg("table.unpack", args, 1)
		if err != nil {
			return nil, err
		}
	}
	end := 0
	if len(args) > 2 && !args[2].IsNil() {
		end, err = integerArg("table.unpack", args, 2)
		if err != nil {
			return nil, err
		}
	} else {
		length, err := seq.len()
		if err != nil {
			return nil, err
		}
		end = length
	}
	return seq.values(start, end)
}

func baseTableInsert(args []Value) ([]Value, error) {
	return baseTableInsertWithController(nil, args)
}

func baseTableInsertWithController(controller *executionController, args []Value) ([]Value, error) {
	table, err := tableArg("table.insert", args, 0)
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("table.insert: argument #2 is nil, want value")
	}
	if len(args) == 2 && table.canAppendFastArray() {
		if err := table.fastArrayAppendWithController(controller, args[1]); err != nil {
			return nil, err
		}
		return nil, nil
	}
	seq := newRawSequenceWithController("table.insert", table, controller)
	length, err := seq.len()
	if err != nil {
		return nil, err
	}
	value := args[1]
	position := length + 1
	if len(args) > 2 {
		position, err = integerArg("table.insert", args, 1)
		if err != nil {
			return nil, err
		}
		value = args[2]
	}
	if position < 1 || position > length+1 {
		return nil, fmt.Errorf("table.insert: position %d out of range", position)
	}
	if table.canUseFastArraySequence(length) {
		if err := table.fastArrayInsertWithController(controller, position, value); err != nil {
			return nil, err
		}
		return nil, nil
	}
	for index := length; index >= position; index-- {
		value, err := seq.get(index)
		if err != nil {
			return nil, err
		}
		if err := seq.set(index+1, value); err != nil {
			return nil, err
		}
	}
	if err := seq.set(position, value); err != nil {
		return nil, err
	}
	return nil, nil
}

func baseTableInsertNative(globals *globalEnv, args []Value) ([]Value, error) {
	return baseTableInsertWithController(executionControllerForGlobals(globals), args)
}

func baseTableRemove(args []Value) ([]Value, error) {
	removed, err := baseTableRemoveValueWithController(nil, args)
	if err != nil {
		return nil, err
	}
	return []Value{removed}, nil
}

func baseTableRemoveValue(args []Value) (Value, error) {
	return baseTableRemoveValueWithController(nil, args)
}

func baseTableRemoveValueWithController(controller *executionController, args []Value) (Value, error) {
	table, err := tableArg("table.remove", args, 0)
	if err != nil {
		return NilValue(), err
	}
	if table.canUseFastArrayStorage() {
		length := len(table.array)
		if length == 0 {
			return NilValue(), nil
		}
		position := length
		if len(args) > 1 && !args[1].IsNil() {
			position, err = integerArg("table.remove", args, 1)
			if err != nil {
				return NilValue(), err
			}
		}
		if position < 1 || position > length {
			return NilValue(), nil
		}
		removed := table.fastArrayRemove(position)
		if !removed.IsNil() && table.entryCount > 0 {
			table.entryCount--
		}
		return removed, nil
	}
	seq := newRawSequenceWithController("table.remove", table, controller)
	length, err := seq.len()
	if err != nil {
		return NilValue(), err
	}
	if length == 0 {
		return NilValue(), nil
	}
	position := length
	if len(args) > 1 && !args[1].IsNil() {
		position, err = integerArg("table.remove", args, 1)
		if err != nil {
			return NilValue(), err
		}
	}
	if position < 1 || position > length {
		return NilValue(), nil
	}
	if table.canUseFastArraySequence(length) {
		removed := table.fastArrayRemove(position)
		if !removed.IsNil() && table.entryCount > 0 {
			table.entryCount--
		}
		return removed, nil
	}
	removed, err := seq.get(position)
	if err != nil {
		return NilValue(), err
	}
	for index := position; index < length; index++ {
		value, err := seq.get(index + 1)
		if err != nil {
			return NilValue(), err
		}
		if err := seq.set(index, value); err != nil {
			return NilValue(), err
		}
	}
	if err := seq.set(length, NilValue()); err != nil {
		return NilValue(), err
	}
	return removed, nil
}

func baseTableRemoveFastArrayValue(tableValue Value, positionValue Value, argCount int) (Value, bool, error) {
	if argCount < 1 {
		return NilValue(), false, nil
	}
	table, ok := tableValue.Table()
	if !ok || !table.canUseFastArrayStorage() {
		return NilValue(), false, nil
	}
	length := len(table.array)
	if length == 0 {
		return NilValue(), true, nil
	}
	position := length
	if argCount > 1 && !positionValue.IsNil() {
		number, ok := positionValue.Number()
		if !ok || number != math.Trunc(number) {
			return NilValue(), false, nil
		}
		position = int(number)
	}
	if position < 1 || position > length {
		return NilValue(), true, nil
	}
	removed := table.fastArrayRemove(position)
	if !removed.IsNil() && table.entryCount > 0 {
		table.entryCount--
	}
	return removed, true, nil
}

func baseTableRemoveNative(globals *globalEnv, args []Value) ([]Value, error) {
	removed, err := baseTableRemoveValueWithController(executionControllerForGlobals(globals), args)
	if err != nil {
		return nil, err
	}
	return []Value{removed}, nil
}

func baseTableConcat(args []Value) ([]Value, error) {
	return baseTableConcatWithThread(nil, nil, args)
}

func baseTableConcatNative(globals *globalEnv, args []Value) ([]Value, error) {
	var thread *vmThread
	if globals != nil {
		thread = globals.thread
	}
	return baseTableConcatWithThread(thread, executionControllerForGlobals(globals), args)
}

func baseTableConcatWithThread(thread *vmThread, controller *executionController, args []Value) ([]Value, error) {
	table, err := tableArg("table.concat", args, 0)
	if err != nil {
		return nil, err
	}
	seq := newRawSequence("table.concat", table)
	separator := ""
	if len(args) > 1 && !args[1].IsNil() {
		var ok bool
		separator, ok = args[1].String()
		if !ok {
			return nil, fmt.Errorf("table.concat: argument #2 is %s, want string or nil", args[1].Kind())
		}
	}
	start := 1
	if len(args) > 2 && !args[2].IsNil() {
		start, err = integerArg("table.concat", args, 2)
		if err != nil {
			return nil, err
		}
	}
	end := 0
	if len(args) > 3 && !args[3].IsNil() {
		end, err = integerArg("table.concat", args, 3)
		if err != nil {
			return nil, err
		}
	} else {
		length, err := seq.len()
		if err != nil {
			return nil, err
		}
		end = length
	}
	if end < start {
		value, err := generatedStringValueWithController(thread, controller, "")
		return []Value{value}, err
	}
	parts := make([]string, 0, end-start+1)
	for index := start; index <= end; index++ {
		value, err := seq.get(index)
		if err != nil {
			return nil, err
		}
		part, ok := value.String()
		if !ok {
			return nil, fmt.Errorf("table.concat: item %d is %s, want string", index, value.Kind())
		}
		parts = append(parts, part)
	}
	text := strings.Join(parts, separator)
	value, err := generatedStringValueWithController(thread, controller, text)
	return []Value{value}, err
}

func baseTableFind(args []Value) ([]Value, error) {
	table, err := tableArg("table.find", args, 0)
	if err != nil {
		return nil, err
	}
	seq := newRawSequence("table.find", table)
	if len(args) < 2 {
		return nil, fmt.Errorf("table.find: argument #2 is nil, want value")
	}
	target := args[1]
	start := 1
	if len(args) > 2 && !args[2].IsNil() {
		start, err = integerArg("table.find", args, 2)
		if err != nil {
			return nil, err
		}
	}
	if start < 1 {
		start = 1
	}
	for index := start; ; index++ {
		value, err := seq.get(index)
		if err != nil {
			return nil, err
		}
		if value.IsNil() {
			return []Value{NilValue()}, nil
		}
		if valuesEqual(value, target) {
			return []Value{NumberValue(float64(index))}, nil
		}
	}
}

func baseTableClear(args []Value) ([]Value, error) {
	table, err := tableArg("table.clear", args, 0)
	if err != nil {
		return nil, err
	}
	newRawSequence("table.clear", table).clear()
	return nil, nil
}

func baseTableSort(globals *globalEnv, args []Value) ([]Value, error) {
	table, err := tableArg("table.sort", args, 0)
	if err != nil {
		return nil, err
	}
	seq := newRawSequence("table.sort", table)
	comparator := NilValue()
	if len(args) > 1 && !args[1].IsNil() {
		comparator = args[1]
		callable, err := metamethodCallable(comparator)
		if err != nil {
			return nil, err
		}
		if !callable {
			return nil, fmt.Errorf("table.sort: argument #2 is %s, want function or nil", comparator.Kind())
		}
	}
	length, err := seq.len()
	if err != nil {
		return nil, err
	}
	values, err := seq.values(1, length)
	if err != nil {
		return nil, err
	}
	for index := 1; index < len(values); index++ {
		value := values[index]
		position := index
		for position > 0 {
			before, err := tableSortBefore(value, values[position-1], comparator, globals)
			if err != nil {
				return nil, fmt.Errorf("table.sort: %w", err)
			}
			if !before {
				break
			}
			values[position] = values[position-1]
			position--
		}
		values[position] = value
	}
	if err := seq.writeValues(values); err != nil {
		return nil, err
	}
	return nil, nil
}

func tableSortBefore(left Value, right Value, comparator Value, globals *globalEnv) (bool, error) {
	if comparator.IsNil() {
		return lessValue(left, right, globals)
	}
	results, err := callValue(comparator, globals, []Value{left, right})
	if err != nil {
		return false, err
	}
	result := adjustedResultAt(results, 0)
	before, ok := result.Bool()
	if !ok {
		return false, fmt.Errorf("comparison returned %s, want boolean", result.Kind())
	}
	return before, nil
}
