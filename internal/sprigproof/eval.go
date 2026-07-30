package sprigproof

import (
	"context"
	"fmt"
	"math"
)

type stepValue struct {
	tag      VariantID
	by, code int64
}
type evalEnv struct {
	ints     map[BindingID]int64
	slices   map[BindingID][]int64
	readings map[BindingID]Reading
	steps    map[BindingID]stepValue
}
type evaluation struct {
	ctx       context.Context
	remaining uint64
	program   *checkedProgram
}

func evaluate(ctx context.Context, program *checkedProgram, readings []Reading, limit uint64) (Result, error) {
	if ctx == nil {
		return Result{}, errorsNewNilContext()
	}
	e := evaluation{ctx: ctx, remaining: limit, program: program}
	if err := e.poll(); err != nil {
		return Result{}, err
	}
	if len(readings) > maximumInput {
		return Result{}, fmt.Errorf("sprig: input length %d exceeds limit %d", len(readings), maximumInput)
	}
	env := evalEnv{ints: make(map[BindingID]int64), slices: make(map[BindingID][]int64), readings: make(map[BindingID]Reading), steps: make(map[BindingID]stepValue)}
	env.slices[program.function.parameter.binding] = nil // marker; loop reads the detached Go input directly.
	result, returned, domain, err := e.exec(program, program.function.body, &env, readings)
	if err != nil {
		return Result{}, err
	}
	if domain != 0 {
		return Result{Tag: ResultError, Code: domain}, nil
	}
	if !returned {
		return Result{}, fmt.Errorf("sprig: Reduce completed without return")
	}
	return detachResult(result), nil
}

func errorsNewNilContext() error { return fmt.Errorf("sprig: nil context") }

func (e *evaluation) poll() error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	if e.remaining == 0 {
		return ErrStepLimit
	}
	e.remaining--
	return nil
}

func (e *evaluation) exec(program *checkedProgram, statements []stmt, env *evalEnv, input []Reading) (Result, bool, DomainCode, error) {
	for _, raw := range statements {
		switch s := raw.(type) {
		case letStmt:
			switch expressionClass(program, s.value) {
			case "slice":
				v, d, err := e.evalSlice(program, s.value, env)
				if err != nil {
					return Result{}, false, 0, err
				}
				if d != 0 {
					return Result{}, false, d, nil
				}
				env.slices[s.binding] = v
			case "step":
				v, d := e.evalStep(program, s.value, env)
				if d != 0 {
					return Result{}, false, d, nil
				}
				env.steps[s.binding] = v
			default:
				v, d := e.evalInt(s.value, env)
				if d != 0 {
					return Result{}, false, d, nil
				}
				env.ints[s.binding] = v
			}
		case assignStmt:
			if _, ok := env.slices[s.binding]; ok {
				v, d, err := e.evalSlice(program, s.value, env)
				if err != nil {
					return Result{}, false, 0, err
				}
				if d != 0 {
					return Result{}, false, d, nil
				}
				env.slices[s.binding] = v
			} else {
				v, d := e.evalInt(s.value, env)
				if d != 0 {
					return Result{}, false, d, nil
				}
				env.ints[s.binding] = v
			}
		case forStmt:
			if s.sourceBinding != program.function.parameter.binding {
				return Result{}, false, 0, fmt.Errorf("sprig: unsupported loop source %q", s.source)
			}
			for _, reading := range input {
				env.readings[s.binding] = reading
				r, returned, d, err := e.exec(program, s.body, env, input)
				if err != nil || returned || d != 0 {
					return r, returned, d, err
				}
				if err := e.poll(); err != nil {
					return Result{}, false, 0, err
				}
			}
		case matchStmt:
			step, d := e.evalStep(program, s.value, env)
			if d != 0 {
				return Result{}, false, d, nil
			}
			for _, arm := range s.arms {
				if arm.variantID != step.tag {
					continue
				}
				inner := cloneEnv(env)
				if arm.variantID == program.pkg.schema.stepAddVariant {
					inner.ints[arm.bindings[0]] = step.by
				} else {
					inner.ints[arm.bindings[0]] = step.code
				}
				r, returned, d, err := e.exec(program, arm.body, &inner, input)
				if err != nil || returned || d != 0 {
					return r, returned, d, err
				}
				mergeOuter(env, &inner)
				break
			}
		case returnStmt:
			r, d, err := e.evalResult(program, s.value, env)
			return r, true, d, err
		}
	}
	return Result{}, false, 0, nil
}

func expressionClass(program *checkedProgram, expression expr) string {
	typ := program.expressionTypes[expression.expressionSpan()]
	if typ.slice {
		return "slice"
	}
	switch typ.name {
	case "Step":
		return "step"
	case "Result":
		return "result"
	}
	return "int"
}

func (e *evaluation) evalInt(expression expr, env *evalEnv) (int64, DomainCode) {
	switch x := expression.(type) {
	case intExpr:
		return x.value, 0
	case nameExpr:
		if len(x.fieldIDs) == 0 {
			return env.ints[x.binding], 0
		}
		reading := env.readings[x.binding]
		if x.fieldIDs[0] == e.program.pkg.schema.readingValueField {
			return reading.Value, 0
		}
		if x.fieldIDs[0] == e.program.pkg.schema.readingDivisorField {
			return reading.Divisor, 0
		}
		return 0, DomainOverflow
	case binaryExpr:
		left, d := e.evalInt(x.left, env)
		if d != 0 {
			return 0, d
		}
		right, d := e.evalInt(x.right, env)
		if d != 0 {
			return 0, d
		}
		switch x.op {
		case tPlus:
			sum, overflow := checkedAdd(left, right)
			if overflow {
				return 0, DomainOverflow
			}
			return sum, 0
		case tSlash:
			if right == 0 {
				return 0, DomainDivideByZero
			}
			if left == math.MinInt64 && right == -1 {
				return 0, DomainOverflow
			}
			return left / right, 0
		}
	case callExpr:
		call := x.call
		target := e.program.project.packages[call.packageID]
		fn, ok := target.functionsByID[call.declID]
		if !ok {
			return 0, DomainOverflow
		}
		ints := make(map[BindingID]int64, len(fn.parameters))
		for i, arg := range x.args {
			value, d := e.evalInt(arg, env)
			if d != 0 {
				return 0, d
			}
			ints[fn.parameters[i].binding] = value
		}
		ret, ok := fn.body[0].(returnStmt)
		if !ok {
			return 0, DomainOverflow
		}
		return e.evalPureInt(target, ret.value, ints)
	}
	return 0, DomainOverflow
}

func (e *evaluation) evalPureInt(pkg *checkedPackage, expression expr, values map[BindingID]int64) (int64, DomainCode) {
	switch x := expression.(type) {
	case intExpr:
		return x.value, 0
	case nameExpr:
		return values[x.binding], 0
	case binaryExpr:
		left, d := e.evalPureInt(pkg, x.left, values)
		if d != 0 {
			return 0, d
		}
		right, d := e.evalPureInt(pkg, x.right, values)
		if d != 0 {
			return 0, d
		}
		switch x.op {
		case tPlus:
			sum, overflow := checkedAdd(left, right)
			if overflow {
				return 0, DomainOverflow
			}
			return sum, 0
		case tSlash:
			if right == 0 {
				return 0, DomainDivideByZero
			}
			if left == math.MinInt64 && right == -1 {
				return 0, DomainOverflow
			}
			return left / right, 0
		}
	case callExpr:
		call := x.call
		target := e.program.project.packages[call.packageID]
		fn := target.functionsByID[call.declID]
		args := make(map[BindingID]int64, len(fn.parameters))
		for i, arg := range x.args {
			v, d := e.evalPureInt(pkg, arg, values)
			if d != 0 {
				return 0, d
			}
			args[fn.parameters[i].binding] = v
		}
		return e.evalPureInt(target, fn.body[0].(returnStmt).value, args)
	}
	return 0, DomainOverflow
}
func (e *evaluation) evalBool(expression expr, env *evalEnv) (bool, DomainCode) {
	x := expression.(binaryExpr)
	left, d := e.evalInt(x.left, env)
	if d != 0 {
		return false, d
	}
	right, d := e.evalInt(x.right, env)
	return left == right, d
}
func (e *evaluation) evalStep(program *checkedProgram, expression expr, env *evalEnv) (stepValue, DomainCode) {
	switch x := expression.(type) {
	case nameExpr:
		return env.steps[x.binding], 0
	case ifExpr:
		yes, d := e.evalBool(x.condition, env)
		if d != 0 {
			return stepValue{}, d
		}
		if yes {
			return e.evalStep(program, x.yes, env)
		}
		return e.evalStep(program, x.no, env)
	case constructExpr:
		value, d := e.evalInt(x.fields[0].value, env)
		if d != 0 {
			return stepValue{}, d
		}
		if x.variantID == program.pkg.schema.stepAddVariant {
			return stepValue{tag: x.variantID, by: value}, 0
		}
		return stepValue{tag: x.variantID, code: value}, 0
	}
	return stepValue{}, DomainOverflow
}
func (e *evaluation) evalSlice(program *checkedProgram, expression expr, env *evalEnv) ([]int64, DomainCode, error) {
	switch x := expression.(type) {
	case sliceExpr:
		return nil, 0, nil
	case nameExpr:
		return append([]int64(nil), env.slices[x.binding]...), 0, nil
	case appendExpr:
		list, d, err := e.evalSlice(program, x.list, env)
		if err != nil {
			return nil, 0, err
		}
		if d != 0 {
			return nil, d, nil
		}
		value, d := e.evalInt(x.value, env)
		if d != 0 {
			return nil, d, nil
		}
		if len(list) >= maximumListElements {
			return nil, 0, ErrListLimit
		}
		return append(list, value), 0, nil
	}
	return nil, DomainOverflow, nil
}
func (e *evaluation) evalResult(program *checkedProgram, expression expr, env *evalEnv) (Result, DomainCode, error) {
	x := expression.(constructExpr)
	if x.variantID == program.pkg.schema.resultErrorVariant {
		code, d := e.evalInt(x.fields[0].value, env)
		return Result{Tag: ResultError, Code: DomainCode(code)}, d, nil
	}
	var total int64
	var rejected []int64
	for _, f := range x.fields {
		if f.fieldID == program.pkg.schema.resultTotalField {
			var d DomainCode
			total, d = e.evalInt(f.value, env)
			if d != 0 {
				return Result{}, d, nil
			}
		} else {
			var d DomainCode
			var err error
			rejected, d, err = e.evalSlice(program, f.value, env)
			if err != nil {
				return Result{}, 0, err
			}
			if d != 0 {
				return Result{}, d, nil
			}
		}
	}
	return Result{Tag: ResultOK, Total: total, Rejected: rejected}, 0, nil
}
func checkedAdd(a, b int64) (int64, bool) {
	sum := a + b
	return sum, (b > 0 && sum < a) || (b < 0 && sum > a)
}
func cloneEnv(in *evalEnv) evalEnv {
	return evalEnv{cloneMap(in.ints), cloneSliceMap(in.slices), cloneMap(in.readings), cloneMap(in.steps)}
}
func mergeOuter(out, in *evalEnv) {
	for k := range out.ints {
		out.ints[k] = in.ints[k]
	}
	for k := range out.slices {
		out.slices[k] = in.slices[k]
	}
	for k := range out.steps {
		out.steps[k] = in.steps[k]
	}
}
func cloneMap[K comparable, V any](in map[K]V) map[K]V {
	out := make(map[K]V, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneSliceMap(in map[BindingID][]int64) map[BindingID][]int64 {
	out := make(map[BindingID][]int64, len(in))
	for k, v := range in {
		out[k] = append([]int64(nil), v...)
	}
	return out
}
func detachResult(r Result) Result { r.Rejected = append([]int64(nil), r.Rejected...); return r }
