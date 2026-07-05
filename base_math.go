package ember

import (
	"fmt"
	"math"
)

func baseMath() *Table {
	table := newTableWithCapacity(0, 5)
	_ = table.Set(StringValue("abs"), HostFuncValue(baseMathAbs))
	_ = table.Set(StringValue("floor"), HostFuncValue(baseMathFloor))
	_ = table.Set(StringValue("max"), HostFuncValue(baseMathMax))
	_ = table.Set(StringValue("min"), nativeFuncValueWithID(baseMathMinNative, nativeFuncMathMin))
	_ = table.Set(StringValue("pi"), NumberValue(math.Pi))
	return table
}

func baseMathAbs(args []Value) ([]Value, error) {
	value, err := numberArg("math.abs", args, 0)
	if err != nil {
		return nil, err
	}
	return []Value{NumberValue(math.Abs(value))}, nil
}

func baseMathFloor(args []Value) ([]Value, error) {
	value, err := numberArg("math.floor", args, 0)
	if err != nil {
		return nil, err
	}
	return []Value{NumberValue(math.Floor(value))}, nil
}

func baseMathMax(args []Value) ([]Value, error) {
	values, err := numberArgs("math.max", args)
	if err != nil {
		return nil, err
	}
	maximum := values[0]
	for _, value := range values[1:] {
		maximum = math.Max(maximum, value)
	}
	return []Value{NumberValue(maximum)}, nil
}

func baseMathMin(args []Value) ([]Value, error) {
	minimum, err := baseMathMinValue(args)
	if err != nil {
		return nil, err
	}
	return []Value{NumberValue(minimum)}, nil
}

func baseMathMinNative(_ *globalEnv, args []Value) ([]Value, error) {
	return baseMathMin(args)
}

func baseMathMinValue(args []Value) (float64, error) {
	if len(args) == 0 {
		return 0, fmt.Errorf("math.min: argument #1 is nil, want number")
	}
	minimum, err := numberArg("math.min", args, 0)
	if err != nil {
		return 0, err
	}
	for i := 1; i < len(args); i++ {
		value, err := numberArg("math.min", args, i)
		if err != nil {
			return 0, err
		}
		minimum = math.Min(minimum, value)
	}
	return minimum, nil
}

func numberArgs(fn string, args []Value) ([]float64, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("%s: argument #1 is nil, want number", fn)
	}
	values := make([]float64, len(args))
	for i := range args {
		value, err := numberArg(fn, args, i)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}
