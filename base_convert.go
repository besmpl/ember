package ember

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func baseType(args []Value) ([]Value, error) {
	value := NilValue()
	if len(args) > 0 {
		value = args[0]
	}
	return []Value{StringValue(typeName(value))}, nil
}

func baseToNumber(args []Value) ([]Value, error) {
	if len(args) == 0 || args[0].IsNil() {
		return []Value{NilValue()}, nil
	}
	if number, ok := args[0].Number(); ok {
		return []Value{NumberValue(number)}, nil
	}
	text, ok := args[0].String()
	if !ok {
		return []Value{NilValue()}, nil
	}
	if len(args) > 1 && !args[1].IsNil() {
		base, err := integerArg("tonumber", args, 1)
		if err != nil {
			return nil, err
		}
		if base < 2 || base > 36 {
			return nil, fmt.Errorf("tonumber: base %d out of range", base)
		}
		integer, err := strconv.ParseInt(strings.TrimSpace(text), base, 64)
		if err != nil {
			return []Value{NilValue()}, nil
		}
		return []Value{NumberValue(float64(integer))}, nil
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return []Value{NilValue()}, nil
	}
	return []Value{NumberValue(number)}, nil
}

func baseToString(globals *globalEnv, args []Value) ([]Value, error) {
	value := NilValue()
	if len(args) > 0 {
		value = args[0]
	}
	result, err := baseToStringValue(globals, value)
	if err != nil {
		return nil, err
	}
	return []Value{result}, nil
}

func baseToStringValue(globals *globalEnv, value Value) (Value, error) {
	text, err := stringValue(value, globals)
	if err != nil {
		return NilValue(), err
	}
	return stringValueInGlobalEnv(globals, text), nil
}

func stringValue(value Value, globals *globalEnv) (string, error) {
	metamethod, ok, err := valueMetamethod(value, "__tostring")
	if err != nil {
		return "", err
	}
	if ok {
		results, err := callRuntimeMetamethodWindow1(metamethod, globals, value)
		if err != nil {
			return "", err
		}
		result := results.at(0)
		text, ok := result.String()
		if !ok {
			return "", fmt.Errorf("__tostring returned %s, want string", result.Kind())
		}
		return text, nil
	}
	return valueToString(value), nil
}

func valueToString(value Value) string {
	if value.IsNil() {
		return "nil"
	}
	if boolean, ok := value.Bool(); ok {
		if boolean {
			return "true"
		}
		return "false"
	}
	if number, ok := value.Number(); ok {
		return formatLuauNumber(number)
	}
	if text, ok := value.String(); ok {
		return text
	}
	return typeName(value)
}

func typeName(value Value) string {
	if value.Kind() == HostFuncKind {
		return FunctionKind.String()
	}
	return value.Kind().String()
}

func tableArg(fn string, args []Value, index int) (*Table, error) {
	if index >= len(args) {
		return nil, fmt.Errorf("%s: argument #%d is nil, want table", fn, index+1)
	}
	value, ok := args[index].Table()
	if !ok {
		return nil, fmt.Errorf("%s: argument #%d is %s, want table", fn, index+1, args[index].Kind())
	}
	return value, nil
}

func numberArg(fn string, args []Value, index int) (float64, error) {
	if index >= len(args) {
		return 0, fmt.Errorf("%s: argument #%d is nil, want number", fn, index+1)
	}
	value, ok := args[index].Number()
	if !ok {
		return 0, fmt.Errorf("%s: argument #%d is %s, want number", fn, index+1, args[index].Kind())
	}
	return value, nil
}

func integerArg(fn string, args []Value, index int) (int, error) {
	value, err := numberArg(fn, args, index)
	if err != nil {
		return 0, err
	}
	if value != math.Trunc(value) {
		return 0, fmt.Errorf("%s: argument #%d is %v, want integer", fn, index+1, value)
	}
	return int(value), nil
}
