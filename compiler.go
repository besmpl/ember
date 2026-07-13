package ember

// CompileLimits bounds source parsing and syntax construction. Zero means
// unlimited for backward compatibility.
type CompileLimits struct {
	// MaxSourceBytes bounds the source length before lexing.
	MaxSourceBytes uint64
	// MaxTokens bounds lexer output.
	MaxTokens uint64
	// MaxNesting bounds recursive parser entry depth.
	MaxNesting uint32
	// MaxSyntaxNodes bounds syntax IDs assigned to the parsed program.
	MaxSyntaxNodes uint64
}

// CompileOptions configures one source compilation.
type CompileOptions struct {
	Limits CompileLimits
}

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
	return CompileWithOptions(source, CompileOptions{})
}

// CompileWithOptions compiles source with explicit parser and source limits.
func CompileWithOptions(source string, options CompileOptions) (*Proto, error) {
	artifact, err := parseSourceWithLimits(Source{Text: source}, options.Limits)
	if err != nil {
		return nil, err
	}

	return compileProgram(artifact)
}
