package ember

import "testing"

// TestAssignmentDependencyPreservesPublicBehavior exercises the assignment
// peephole through the public Compile/Run surface and compares it with the
// same source compiled with bytecode peepholes disabled. The cases cover the
// expression forms whose evaluation order determines whether the target
// register may be reused.
func TestAssignmentDependencyPreservesPublicBehavior(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []Value
	}{
		{
			name:   "target first operand",
			source: "local value = 1\nvalue = value + 1\nreturn value",
			want:   []Value{NumberValue(2)},
		},
		{
			name:   "target late operand",
			source: "local value = 1\nvalue = 2 + value\nreturn value",
			want:   []Value{NumberValue(3)},
		},
		{
			name:   "selector index does not read target",
			source: "local value = {11, 22}\nlocal index = 2\nvalue = value[index]\nreturn value",
			want:   []Value{NumberValue(22)},
		},
		{
			name:   "selector index reads target",
			source: "local value = {2, 11}\nvalue = value[value[1]]\nreturn value",
			want:   []Value{NumberValue(11)},
		},
		{
			name: "call argument",
			source: `local value = 1
local function add(input)
	return input + 1
end
value = add(value)
return value`,
			want: []Value{NumberValue(2)},
		},
		{
			name:   "table literal",
			source: "local value = 1\nvalue = {value, value + 1}\nreturn value[2]",
			want:   []Value{NumberValue(2)},
		},
		{
			name:   "short circuit target first",
			source: "local value = false\nvalue = value and true\nreturn value",
			want:   []Value{BoolValue(false)},
		},
		{
			name:   "short circuit target late",
			source: "local value = false\nvalue = true and value\nreturn value",
			want:   []Value{BoolValue(false)},
		},
		{
			name:   "power target base",
			source: "local value = 2\nvalue = value ^ 3\nreturn value",
			want:   []Value{NumberValue(8)},
		},
		{
			name:   "power target exponent",
			source: "local value = 2\nvalue = 2 ^ value\nreturn value",
			want:   []Value{NumberValue(4)},
		},
		{
			name:   "comparison target first operand",
			source: "local value = 1\nvalue = value < 2\nreturn value",
			want:   []Value{BoolValue(true)},
		},
		{
			name:   "comparison target late operand",
			source: "local value = 1\nvalue = 2 < value\nreturn value",
			want:   []Value{BoolValue(false)},
		},
		{
			name:   "concat target first operand",
			source: "local value = \"a\"\nvalue = value .. \"b\" .. \"c\"\nreturn value",
			want:   []Value{StringValue("abc")},
		},
		{
			name:   "concat target late operand",
			source: "local value = \"b\"\nvalue = \"a\" .. value .. \"c\"\nreturn value",
			want:   []Value{StringValue("abc")},
		},
		{
			name:   "if expression target condition",
			source: "local value = 1\nvalue = if value > 0 then 2 else 3\nreturn value",
			want:   []Value{NumberValue(2)},
		},
		{
			name:   "if expression target branch",
			source: "local value = 1\nvalue = if true then value else 3\nreturn value",
			want:   []Value{NumberValue(1)},
		},
		{
			name:   "computed table key",
			source: "local value = 1\nvalue = {[value] = 2}\nreturn value[1]",
			want:   []Value{NumberValue(2)},
		},
		{
			name:   "computed table value",
			source: "local value = 1\nvalue = {[1] = value}\nreturn value[1]",
			want:   []Value{NumberValue(1)},
		},
		{
			name:   "group",
			source: "local value = 2\nvalue = (value + 1) * 2\nreturn value",
			want:   []Value{NumberValue(6)},
		},
		{
			name:   "cast",
			source: "local value = 2\nvalue = (value :: number) + 1\nreturn value",
			want:   []Value{NumberValue(3)},
		},
		{
			name:   "cast target late operand",
			source: "local value = 2\nvalue = 2 + (value :: number)\nreturn value",
			want:   []Value{NumberValue(4)},
		},
		{
			name: "shadowed local",
			source: `local value = 1
do
	local value = 2
	value = value + 1
end
return value`,
			want: []Value{NumberValue(1)},
		},
		{
			name: "captured upvalue",
			source: `local value = 1
local function update()
	value = value + 1
end
update()
return value`,
			want: []Value{NumberValue(2)},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			optimized, err := Compile(test.source)
			if err != nil {
				t.Fatalf("optimized Compile returned error: %v", err)
			}
			artifact, err := parseSource(Source{Text: test.source})
			if err != nil {
				t.Fatalf("parseSource returned error: %v", err)
			}
			disabled, err := compileProgramWithOptions(artifact, compilerOptions{
				optimizations: optimizationOptions{
					disabledCategories: map[optimizationCategory]bool{
						optimizationBytecodePeephole: true,
					},
				},
			})
			if err != nil {
				t.Fatalf("peephole-disabled Compile returned error: %v", err)
			}

			optimizedValues, optimizedErr := Run(optimized)
			disabledValues, disabledErr := Run(disabled)
			if !equalTestErrors(optimizedErr, disabledErr) {
				t.Fatalf("optimized Run error is %v, peephole-disabled Run error is %v", optimizedErr, disabledErr)
			}
			if optimizedErr != nil {
				t.Fatalf("Run returned error: %v", optimizedErr)
			}
			if !valuesSliceEquivalent(optimizedValues, disabledValues) {
				t.Fatalf("optimized Run values = %#v, peephole-disabled values = %#v", optimizedValues, disabledValues)
			}
			if len(optimizedValues) != len(test.want) {
				t.Fatalf("Run returned %d values, want %d: %#v", len(optimizedValues), len(test.want), optimizedValues)
			}
			for index, want := range test.want {
				if !valuesEqual(optimizedValues[index], want) {
					t.Fatalf("Run value %d = %#v, want %#v", index, optimizedValues[index], want)
				}
			}
		})
	}
}

func TestAssignmentDependencyRejectsMalformedExpressionFacts(t *testing.T) {
	compiler := compiler{
		tree: syntaxTree{arena: &syntaxArena{
			expressions: []arenaExpression{{terms: nodeSpan{start: 0, count: 1}}},
		}},
		bind: bindResult{},
	}

	if got := compiler.assignmentDependency(expressionID(2), 0); got != (assignmentDependency{}) {
		t.Fatalf("invalid expression dependency = %#v, want empty decision", got)
	}
	if got := compiler.assignmentTermDependency(termID(2), 0); got != (assignmentDependency{}) {
		t.Fatalf("invalid term dependency = %#v, want empty decision", got)
	}
}
