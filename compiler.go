package ember

// Compile compiles a tiny supported Luau source category into Ember bytecode.
//
// This seed compiler currently accepts scalar literals, array, named-field, and
// computed-key table literals, local bindings, assignments to existing locals
// or table selector chains, do blocks, if/elseif, while, repeat/until, numeric
// for, and generic for statements over iterator expressions or direct table
// values with __iter support, break, and continue, host
// globals read and assigned as expression values, host callback calls through
// names or selector expressions as expressions or statements, method calls with
// receiver self-arguments, local and anonymous closures with upvalues, table
// field and method function declarations, multiple return values and value-list
// adjustment for returns, local bindings, assignment, and final call arguments,
// variadic script functions with ..., and expressions containing number
// literals or local names joined by +, -, *, /, //, %, ^, .., ==, ~=, <, <=, >, >=, and,
// or, if/then/elseif/else expressions, prefixed by not, unary -, or unary #, or
// grouped with parentheses.
func Compile(source string) (*Proto, error) {
	artifact, err := parseSource(Source{Text: source})
	if err != nil {
		return nil, err
	}

	return compileProgram(artifact)
}
