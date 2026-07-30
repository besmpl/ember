package sprigproof

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	gotoken "go/token"
	"slices"
	"strings"
)

type checkedProgram struct {
	project         *checkedProject
	pkg             *checkedPackage
	module          moduleAST
	function        funcDecl
	variants        map[string]checkedVariant
	expressionTypes map[Span]typeRef
}

type checkedProject struct {
	packages map[PackageID]*checkedPackage
	order    []PackageID
	identity [sha256.Size]byte
}

type checkedPackage struct {
	id              PackageID
	module          moduleAST
	imports         map[string]PackageID
	functions       map[string]funcDecl
	functionsByID   map[DeclID]funcDecl
	variants        map[string]checkedVariant
	variantsByID    map[VariantID]checkedVariant
	expressionTypes map[Span]typeRef
	calls           map[Span]checkedCall
	schema          counterSchema
	sourceIdentity  [sha256.Size]byte
	identity        [sha256.Size]byte
}

type checkedCall struct {
	packageID PackageID
	declID    DeclID
}

type checkedVariant struct {
	id      VariantID
	union   string
	unionID DeclID
	fields  []fieldDecl
}

type counterSchema struct {
	readingValueField    FieldID
	readingDivisorField  FieldID
	stepAddVariant       VariantID
	stepAddField         FieldID
	stepRejectVariant    VariantID
	stepRejectField      FieldID
	resultOKVariant      VariantID
	resultTotalField     FieldID
	resultRejectedField  FieldID
	resultErrorVariant   VariantID
	resultErrorCodeField FieldID
}

type checkedName struct {
	binding  BindingID
	fieldIDs []FieldID
}

type bindingFact struct {
	id  BindingID
	typ typeRef
}

// Compile lexes, parses, and checks source without effects. Diagnostics are in
// stable source order and carry exact half-open byte spans.
func Compile(source string) (Program, []Diagnostic) {
	tokens, diagnostics := lex(source)
	if len(diagnostics) != 0 {
		return Program{}, diagnostics
	}
	module, diagnostics := parse(tokens)
	if len(diagnostics) != 0 {
		return Program{}, diagnostics
	}
	checked, diagnostics := check(module)
	if len(diagnostics) != 0 {
		slices.SortStableFunc(diagnostics, func(a, b Diagnostic) int {
			if order := cmp.Compare(a.Span.Start, b.Span.Start); order != 0 {
				return order
			}
			if order := cmp.Compare(a.Span.End, b.Span.End); order != 0 {
				return order
			}
			return cmp.Compare(a.Message, b.Message)
		})
		return Program{}, diagnostics
	}
	return Program{checked}, nil
}

// CompileProject parses, checks, and canonically orders a bounded acyclic
// project. Input permutation does not affect identities or output order.
func CompileProject(input []SourcePackage) (Project, []Diagnostic) {
	if len(input) == 0 {
		return Project{}, []Diagnostic{{Message: "sprig: project is empty"}}
	}
	if len(input) > 32 {
		return Project{}, []Diagnostic{{Message: "sprig: package count exceeds limit 32"}}
	}
	sources := append([]SourcePackage(nil), input...)
	slices.SortFunc(sources, func(a, b SourcePackage) int { return cmp.Compare(a.ID, b.ID) })
	counts := make(map[PackageID]int, len(sources))
	for _, source := range sources {
		counts[source.ID]++
	}
	project := &checkedProject{packages: make(map[PackageID]*checkedPackage), order: make([]PackageID, 0, len(sources))}
	var diagnostics []Diagnostic
	for i, source := range sources {
		if counts[source.ID] != 1 {
			if i == 0 || sources[i-1].ID != source.ID {
				diagnostics = append(diagnostics, Diagnostic{Message: fmt.Sprintf("package %q: duplicate project package", source.ID)})
			}
			continue
		}
		if !validPackageID(source.ID) {
			diagnostics = append(diagnostics, Diagnostic{Message: fmt.Sprintf("package %q: invalid canonical PackageID", source.ID)})
			continue
		}
		tokens, ds := lex(source.Source)
		if len(ds) != 0 {
			diagnostics = appendPackageDiagnostics(diagnostics, source.ID, ds)
			continue
		}
		module, ds := parse(tokens)
		if len(ds) != 0 {
			diagnostics = appendPackageDiagnostics(diagnostics, source.ID, ds)
			continue
		}
		if PackageID(module.packageName) != source.ID {
			diagnostics = append(diagnostics, Diagnostic{Span: module.packageSpan, Message: fmt.Sprintf("package %q: source header %q does not match PackageID", source.ID, module.packageName)})
			continue
		}
		sourceID := sourceIdentity(source.ID, source.Source)
		pkg := &checkedPackage{id: source.ID, module: module, imports: make(map[string]PackageID), functions: make(map[string]funcDecl), sourceIdentity: sourceID, identity: sourceID}
		project.packages[source.ID] = pkg
		project.order = append(project.order, source.ID)
	}
	if len(diagnostics) != 0 {
		return Project{}, sortDiagnostics(diagnostics)
	}
	for _, id := range project.order {
		pkg := project.packages[id]
		seen := map[string]bool{}
		for _, imp := range pkg.module.imports {
			target := PackageID(imp.packageName)
			if !validPackageID(target) || project.packages[target] == nil {
				diagnostics = append(diagnostics, Diagnostic{Span: imp.span, Message: fmt.Sprintf("package %q: unknown import %q", id, target)})
				continue
			}
			if target == id {
				diagnostics = append(diagnostics, Diagnostic{Span: imp.span, Message: fmt.Sprintf("package %q: imports itself", id)})
				continue
			}
			if seen[imp.alias] {
				diagnostics = append(diagnostics, Diagnostic{Span: imp.span, Message: fmt.Sprintf("package %q: duplicate import alias %q", id, imp.alias)})
				continue
			}
			seen[imp.alias] = true
			pkg.imports[imp.alias] = target
		}
	}
	if cycle := projectCycle(project); len(cycle) != 0 {
		diagnostics = append(diagnostics, Diagnostic{Message: "sprig: import cycle: " + strings.Join(cycle, " -> ")})
	}
	if len(diagnostics) != 0 {
		return Project{}, sortDiagnostics(diagnostics)
	}
	// Check dependencies first; PackageID ordering is still the public order.
	for _, id := range dependencyOrder(project) {
		pkg, ds := checkPackage(project, project.packages[id])
		if len(ds) != 0 {
			diagnostics = appendPackageDiagnostics(diagnostics, id, ds)
			continue
		}
		project.packages[id] = pkg
	}
	if len(diagnostics) != 0 {
		return Project{}, sortDiagnostics(diagnostics)
	}
	for _, id := range dependencyOrder(project) {
		pkg := project.packages[id]
		pkg.identity = packageIdentity(pkg, project)
	}
	project.identity = projectIdentity(project)
	return Project{checked: project}, nil
}

func validPackageID(id PackageID) bool {
	s := string(id)
	// Both preparation products deliberately use the guest PackageID as the
	// generated Go package name. A package named main is not independently
	// importable, so reject it at the project boundary instead of publishing a
	// misleading Set.
	if s == "" || s == "main" || s[0] < 'a' || s[0] > 'z' || gotoken.Lookup(s).IsKeyword() {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !(s[i] >= 'a' && s[i] <= 'z' || s[i] >= '0' && s[i] <= '9' || s[i] == '_') {
			return false
		}
	}
	return len(s) <= 64
}
func sourceIdentity(id PackageID, source string) [sha256.Size]byte {
	h := sha256.New()
	h.Write([]byte("sprig:source:v1\x00"))
	h.Write([]byte(id))
	h.Write([]byte{0})
	h.Write([]byte(source))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
func projectIdentity(p *checkedProject) [sha256.Size]byte {
	h := sha256.New()
	h.Write([]byte("sprig:project:v1\x00"))
	var b [8]byte
	for _, id := range p.order {
		binary.BigEndian.PutUint64(b[:], uint64(len(id)))
		h.Write(b[:])
		h.Write([]byte(id))
		h.Write(p.packages[id].identity[:])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
func packageIdentity(pkg *checkedPackage, project *checkedProject) [sha256.Size]byte {
	h := sha256.New()
	h.Write([]byte("sprig:package:v1\x00"))
	h.Write(pkg.sourceIdentity[:])
	aliases := make([]string, 0, len(pkg.imports))
	for alias := range pkg.imports {
		aliases = append(aliases, alias)
	}
	slices.Sort(aliases)
	var b [8]byte
	for _, alias := range aliases {
		target := pkg.imports[alias]
		binary.BigEndian.PutUint64(b[:], uint64(len(alias)))
		h.Write(b[:])
		h.Write([]byte(alias))
		h.Write(project.packages[target].identity[:])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
func appendPackageDiagnostics(out []Diagnostic, id PackageID, ds []Diagnostic) []Diagnostic {
	for _, d := range ds {
		d.Message = fmt.Sprintf("package %q: %s", id, d.Message)
		out = append(out, d)
	}
	return out
}
func sortDiagnostics(ds []Diagnostic) []Diagnostic {
	slices.SortStableFunc(ds, func(a, b Diagnostic) int {
		if x := cmp.Compare(a.Message, b.Message); x != 0 {
			return x
		}
		if x := cmp.Compare(a.Span.Start, b.Span.Start); x != 0 {
			return x
		}
		return cmp.Compare(a.Span.End, b.Span.End)
	})
	return ds
}
func projectCycle(p *checkedProject) []string {
	state := map[PackageID]uint8{}
	stack := []PackageID{}
	var found []string
	var visit func(PackageID)
	visit = func(id PackageID) {
		if len(found) > 0 {
			return
		}
		state[id] = 1
		stack = append(stack, id)
		pkg := p.packages[id]
		targets := make([]PackageID, 0, len(pkg.imports))
		for _, t := range pkg.imports {
			targets = append(targets, t)
		}
		slices.Sort(targets)
		for _, t := range targets {
			if state[t] == 1 {
				at := slices.Index(stack, t)
				for _, x := range stack[at:] {
					found = append(found, string(x))
				}
				found = append(found, string(t))
				return
			}
			if state[t] == 0 {
				visit(t)
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = 2
	}
	for _, id := range p.order {
		if state[id] == 0 {
			visit(id)
		}
	}
	return found
}
func dependencyOrder(p *checkedProject) []PackageID {
	seen := map[PackageID]bool{}
	var out []PackageID
	var visit func(PackageID)
	visit = func(id PackageID) {
		if seen[id] {
			return
		}
		seen[id] = true
		ts := []PackageID{}
		for _, t := range p.packages[id].imports {
			ts = append(ts, t)
		}
		slices.Sort(ts)
		for _, t := range ts {
			visit(t)
		}
		out = append(out, id)
	}
	for _, id := range p.order {
		visit(id)
	}
	return out
}

type checker struct {
	project         *checkedProject
	pkg             *checkedPackage
	diagnostics     []Diagnostic
	types           map[string]typeRef
	variants        map[string]checkedVariant
	function        *funcDecl
	decls           []decl
	inputName       string
	loopDepth       int
	expressionTypes map[Span]typeRef
	calls           map[Span]checkedCall
	names           map[Span]checkedName
	assignments     map[Span]BindingID
	loopSources     map[Span]BindingID
	variantRefs     map[Span]VariantID
	fieldRefs       map[Span]FieldID
	schema          counterSchema
	nextDecl        DeclID
	nextVariant     VariantID
	nextField       FieldID
	nextBinding     BindingID
}

func check(module moduleAST) (*checkedProgram, []Diagnostic) {
	pkg := &checkedPackage{id: "counter", module: module, imports: map[string]PackageID{}, functions: map[string]funcDecl{}}
	project := &checkedProject{packages: map[PackageID]*checkedPackage{pkg.id: pkg}, order: []PackageID{pkg.id}}
	checked, diagnostics := checkPackage(project, pkg)
	if module.packageName != "counter" {
		diagnostics = append(diagnostics, Diagnostic{Span: module.packageSpan, Message: "proof package must be counter"})
	}
	if len(diagnostics) != 0 {
		return nil, diagnostics
	}
	fn, ok := checked.functions["Reduce"]
	if !ok {
		return nil, []Diagnostic{{Span: module.packageSpan, Message: "missing Reduce function"}}
	}
	project.packages[pkg.id] = checked
	return &checkedProgram{project: project, pkg: checked, module: checked.module, function: fn, variants: checked.variants, expressionTypes: checked.expressionTypes}, nil
}

func checkPackage(project *checkedProject, pkg *checkedPackage) (*checkedPackage, []Diagnostic) {
	module := pkg.module
	c := checker{
		project:         project,
		pkg:             pkg,
		types:           map[string]typeRef{"i64": {name: "i64"}},
		variants:        make(map[string]checkedVariant),
		decls:           module.decls,
		expressionTypes: make(map[Span]typeRef),
		calls:           make(map[Span]checkedCall),
		names:           make(map[Span]checkedName),
		assignments:     make(map[Span]BindingID),
		loopSources:     make(map[Span]BindingID),
		variantRefs:     make(map[Span]VariantID),
		fieldRefs:       make(map[Span]FieldID),
	}
	c.assignNominalIDs(&module)
	c.decls = module.decls
	pkg.module = module
	names := make(map[string]Span)
	for _, raw := range module.decls {
		var name string
		var span Span
		switch d := raw.(type) {
		case recordDecl:
			name, span = d.name, d.span
			c.types[name] = typeRef{name: name}
		case unionDecl:
			name, span = d.name, d.span
			c.types[name] = typeRef{name: name}
		case funcDecl:
			name, span = d.name, d.span
			copy := d
			if d.name == "Reduce" {
				c.function = &copy
			}
		}
		if previous, exists := names[name]; exists {
			c.problem(span, fmt.Sprintf("duplicate declaration %q (first at %d:%d)", name, previous.StartLine, previous.StartColumn))
		} else {
			names[name] = span
		}
		c.checkIdentifier(name, span)
	}
	if pkg.id == "counter" {
		c.checkHostSchema(module)
	}
	for _, raw := range module.decls {
		switch d := raw.(type) {
		case recordDecl:
			c.checkFields(d.fields)
		case unionDecl:
			seen := make(map[string]bool)
			for _, variant := range d.variants {
				c.checkIdentifier(variant.name, variant.span)
				if seen[variant.name] {
					c.problem(variant.span, fmt.Sprintf("duplicate variant %q", variant.name))
					continue
				}
				seen[variant.name] = true
				c.checkFields(variant.fields)
				if _, exists := c.variants[variant.name]; exists {
					c.problem(variant.span, fmt.Sprintf("variant %q is ambiguous", variant.name))
				} else {
					c.variants[variant.name] = checkedVariant{id: variant.id, union: d.name, unionID: d.id, fields: variant.fields}
				}
			}
		}
	}
	if pkg.id == "counter" && c.function == nil {
		c.problem(module.packageSpan, "missing Reduce function")
	}
	for _, raw := range module.decls {
		if fn, ok := raw.(funcDecl); ok {
			c.checkFunction(fn)
		}
	}
	if pkg.id != "counter" {
		for _, raw := range module.decls {
			if _, ok := raw.(funcDecl); !ok {
				c.problem(module.packageSpan, "non-counter packages contain functions only")
			}
		}
	}
	if len(c.diagnostics) != 0 {
		return nil, c.diagnostics
	}
	module = freezeCheckedModule(module, &c)
	functions := make(map[string]funcDecl)
	functionsByID := make(map[DeclID]funcDecl)
	for _, raw := range module.decls {
		if fn, ok := raw.(funcDecl); ok {
			functions[fn.name] = fn
			functionsByID[fn.id] = fn
		}
	}
	variantsByID := make(map[VariantID]checkedVariant, len(c.variants))
	for _, variant := range c.variants {
		variantsByID[variant.id] = variant
	}
	pkg.module = module
	pkg.functions = functions
	pkg.functionsByID = functionsByID
	pkg.variants = c.variants
	pkg.variantsByID = variantsByID
	pkg.expressionTypes = c.expressionTypes
	pkg.calls = c.calls
	pkg.schema = c.schema
	return pkg, nil
}

func (c *checker) assignNominalIDs(module *moduleAST) {
	for i, raw := range module.decls {
		c.nextDecl++
		switch d := raw.(type) {
		case recordDecl:
			d.id = c.nextDecl
			for j := range d.fields {
				c.nextField++
				d.fields[j].id = c.nextField
			}
			module.decls[i] = d
		case unionDecl:
			d.id = c.nextDecl
			for vi := range d.variants {
				c.nextVariant++
				d.variants[vi].id = c.nextVariant
				for fi := range d.variants[vi].fields {
					c.nextField++
					d.variants[vi].fields[fi].id = c.nextField
				}
			}
			module.decls[i] = d
		case funcDecl:
			d.id = c.nextDecl
			for pi := range d.parameters {
				c.nextField++
				c.nextBinding++
				d.parameters[pi].id = c.nextField
				d.parameters[pi].binding = c.nextBinding
			}
			if len(d.parameters) > 0 {
				d.parameter = d.parameters[0]
			}
			d.body = c.assignStatementIDs(d.body)
			module.decls[i] = d
		}
	}
}
func (c *checker) assignStatementIDs(statements []stmt) []stmt {
	out := make([]stmt, len(statements))
	for i, raw := range statements {
		switch s := raw.(type) {
		case letStmt:
			c.nextBinding++
			s.binding = c.nextBinding
			out[i] = s
		case forStmt:
			c.nextBinding++
			s.binding = c.nextBinding
			s.body = c.assignStatementIDs(s.body)
			out[i] = s
		case matchStmt:
			for ai := range s.arms {
				for range s.arms[ai].binds {
					c.nextBinding++
					s.arms[ai].bindings = append(s.arms[ai].bindings, c.nextBinding)
				}
				s.arms[ai].body = c.assignStatementIDs(s.arms[ai].body)
			}
			out[i] = s
		default:
			out[i] = raw
		}
	}
	return out
}

func freezeCheckedModule(module moduleAST, c *checker) moduleAST {
	for i, raw := range module.decls {
		fn, ok := raw.(funcDecl)
		if !ok {
			continue
		}
		fn.body = freezeCheckedStatements(fn.body, c)
		module.decls[i] = fn
	}
	return module
}

func freezeCheckedStatements(statements []stmt, c *checker) []stmt {
	out := make([]stmt, len(statements))
	for i, raw := range statements {
		switch s := raw.(type) {
		case letStmt:
			s.value = freezeCheckedExpression(s.value, c)
			out[i] = s
		case assignStmt:
			s.binding = c.assignments[s.span]
			s.value = freezeCheckedExpression(s.value, c)
			out[i] = s
		case forStmt:
			s.sourceBinding = c.loopSources[s.span]
			s.body = freezeCheckedStatements(s.body, c)
			out[i] = s
		case matchStmt:
			s.value = freezeCheckedExpression(s.value, c)
			for arm := range s.arms {
				s.arms[arm].variantID = c.variantRefs[s.arms[arm].span]
				s.arms[arm].body = freezeCheckedStatements(s.arms[arm].body, c)
			}
			out[i] = s
		case returnStmt:
			s.value = freezeCheckedExpression(s.value, c)
			out[i] = s
		default:
			panic("sprig: unknown checked statement")
		}
	}
	return out
}

func freezeCheckedExpression(expression expr, c *checker) expr {
	switch x := expression.(type) {
	case intExpr, sliceExpr:
		return expression
	case nameExpr:
		fact := c.names[x.span]
		x.binding = fact.binding
		x.fieldIDs = slices.Clone(fact.fieldIDs)
		return x
	case binaryExpr:
		x.left = freezeCheckedExpression(x.left, c)
		x.right = freezeCheckedExpression(x.right, c)
		return x
	case ifExpr:
		x.condition = freezeCheckedExpression(x.condition, c)
		x.yes = freezeCheckedExpression(x.yes, c)
		x.no = freezeCheckedExpression(x.no, c)
		return x
	case constructExpr:
		x.variantID = c.variantRefs[x.span]
		for field := range x.fields {
			x.fields[field].fieldID = c.fieldRefs[x.fields[field].span]
			x.fields[field].value = freezeCheckedExpression(x.fields[field].value, c)
		}
		return x
	case appendExpr:
		x.list = freezeCheckedExpression(x.list, c)
		x.value = freezeCheckedExpression(x.value, c)
		return x
	case callExpr:
		x.call = c.calls[x.span]
		for arg := range x.args {
			x.args[arg] = freezeCheckedExpression(x.args[arg], c)
		}
		return x
	default:
		panic("sprig: unknown checked expression")
	}
}

func (c *checker) checkHostSchema(module moduleAST) {
	records := make(map[string]recordDecl)
	unions := make(map[string]unionDecl)
	for _, raw := range module.decls {
		switch d := raw.(type) {
		case recordDecl:
			records[d.name] = d
		case unionDecl:
			unions[d.name] = d
		}
	}
	reading, ok := records["Reading"]
	if !ok {
		c.problem(module.packageSpan, "missing Reading record")
	} else if !fieldsEqual(reading.fields, []fieldShape{{"value", "i64", false}, {"divisor", "i64", false}}) {
		c.problem(reading.span, "Reading must contain value i64 and divisor i64")
	} else {
		c.schema.readingValueField = reading.fields[0].id
		c.schema.readingDivisorField = reading.fields[1].id
	}
	step, ok := unions["Step"]
	if !ok {
		c.problem(module.packageSpan, "missing Step union")
	} else if !variantsEqual(step.variants, []variantShape{{"Add", []fieldShape{{"by", "i64", false}}}, {"Reject", []fieldShape{{"code", "i64", false}}}}) {
		c.problem(step.span, "Step must contain Add { by i64 } and Reject { code i64 }")
	} else {
		c.schema.stepAddVariant = step.variants[0].id
		c.schema.stepAddField = step.variants[0].fields[0].id
		c.schema.stepRejectVariant = step.variants[1].id
		c.schema.stepRejectField = step.variants[1].fields[0].id
	}
	result, ok := unions["Result"]
	if !ok {
		c.problem(module.packageSpan, "missing Result union")
	} else if !variantsEqual(result.variants, []variantShape{{"Ok", []fieldShape{{"total", "i64", false}, {"rejected", "i64", true}}}, {"Err", []fieldShape{{"code", "i64", false}}}}) {
		c.problem(result.span, "Result must contain Ok { total i64 rejected []i64 } and Err { code i64 }")
	} else {
		c.schema.resultOKVariant = result.variants[0].id
		c.schema.resultTotalField = result.variants[0].fields[0].id
		c.schema.resultRejectedField = result.variants[0].fields[1].id
		c.schema.resultErrorVariant = result.variants[1].id
		c.schema.resultErrorCodeField = result.variants[1].fields[0].id
	}
}

type fieldShape struct {
	name, typ string
	slice     bool
}
type variantShape struct {
	name   string
	fields []fieldShape
}

func fieldsEqual(got []fieldDecl, want []fieldShape) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].name != want[i].name || got[i].typ.name != want[i].typ || got[i].typ.slice != want[i].slice {
			return false
		}
	}
	return true
}
func variantsEqual(got []variantDecl, want []variantShape) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].name != want[i].name || !fieldsEqual(got[i].fields, want[i].fields) {
			return false
		}
	}
	return true
}

func (c *checker) checkFields(fields []fieldDecl) {
	seen := make(map[string]bool)
	for _, field := range fields {
		c.checkIdentifier(field.name, field.span)
		if seen[field.name] {
			c.problem(field.span, fmt.Sprintf("duplicate field %q", field.name))
		}
		seen[field.name] = true
		if !c.validType(field.typ) {
			c.problem(field.typ.span, fmt.Sprintf("unknown type %q", displayType(field.typ)))
		}
	}
}

func (c *checker) checkFunction(fn funcDecl) {
	// These spellings cannot form an ordinary callable Go package-level
	// function. Keep the proof's public ABI direct instead of silently mangling
	// a declaration or deferring failure to the Go compiler.
	if fn.name == "init" || fn.name == "_" {
		c.problem(fn.span, fmt.Sprintf("function name %q cannot be represented by the generated package ABI", fn.name))
	}
	if c.pkg.id == "counter" && fn.name != "Reduce" {
		c.problem(fn.span, "counter function must be named Reduce")
	}
	if c.pkg.id != "counter" && generatedPureABINames[fn.name] {
		c.problem(fn.span, fmt.Sprintf("function name %q conflicts with generated package ABI", fn.name))
	}
	if c.pkg.id == "counter" {
		if len(fn.parameters) != 1 || !fn.parameter.typ.slice || fn.parameter.typ.name != "Reading" {
			c.problem(fn.span, "Reduce parameter must have type []Reading")
		}
		if fn.result.slice || fn.result.name != "Result" {
			c.problem(fn.result.span, "Reduce result must have type Result")
		}
	} else if fn.result.slice || fn.result.name != "i64" {
		c.problem(fn.result.span, "pure package function result must be i64")
	}
	env := make(map[string]bindingFact, len(fn.parameters))
	for _, parameter := range fn.parameters {
		if c.pkg.id != "counter" && (parameter.typ.slice || parameter.typ.name != "i64") {
			c.problem(parameter.typ.span, "pure package parameters must be i64")
		}
		if _, ok := env[parameter.name]; ok {
			c.problem(parameter.span, fmt.Sprintf("duplicate parameter %q", parameter.name))
		}
		env[parameter.name] = bindingFact{id: parameter.binding, typ: parameter.typ}
	}
	if len(fn.parameters) > 0 {
		c.inputName = fn.parameter.name
	}
	c.checkStatements(fn.body, env, fn.result)
	if len(fn.body) == 0 {
		c.problem(fn.span, "function must end with return")
	} else if _, ok := fn.body[len(fn.body)-1].(returnStmt); !ok {
		c.problem(fn.body[len(fn.body)-1].statementSpan(), "function must end with return")
	}
	if c.pkg.id != "counter" && len(fn.body) != 1 {
		c.problem(fn.span, "pure package functions contain exactly one return")
	}
}

var generatedPureABINames = map[string]bool{
	"DomainCode":         true,
	"DomainOverflow":     true,
	"DomainDivideByZero": true,
}

func (c *checker) checkStatements(statements []stmt, env map[string]bindingFact, result typeRef) {
	for _, statement := range statements {
		switch s := statement.(type) {
		case letStmt:
			c.checkIdentifier(s.name, s.span)
			c.checkLocalIdentifier(s.name, s.span)
			if _, exists := env[s.name]; exists {
				c.problem(s.span, fmt.Sprintf("binding %q already exists", s.name))
				continue
			}
			typ := c.expressionType(s.value, env)
			if typ.name == "Result" || typ.name == "bool" {
				c.problem(s.value.expressionSpan(), fmt.Sprintf("local %s values are not supported", typ.name))
			}
			env[s.name] = bindingFact{id: s.binding, typ: typ}
		case assignStmt:
			want, exists := env[s.name]
			if !exists {
				c.problem(s.span, fmt.Sprintf("unknown binding %q", s.name))
				continue
			}
			c.assignments[s.span] = want.id
			got := c.expressionType(s.value, env)
			if want.typ.name == "Step" || want.typ.name == "Result" || want.typ.name == "bool" {
				c.problem(s.span, fmt.Sprintf("assignment to %s bindings is not supported", want.typ.name))
				continue
			}
			if !sameType(want.typ, got) {
				c.problem(s.value.expressionSpan(), fmt.Sprintf("cannot assign %s to %s", displayType(got), displayType(want.typ)))
			}
		case forStmt:
			c.checkIdentifier(s.name, s.span)
			c.checkLocalIdentifier(s.name, s.span)
			typ, exists := env[s.source]
			if !exists || !typ.typ.slice {
				c.problem(s.span, fmt.Sprintf("loop source %q is not a slice", s.source))
				continue
			}
			c.loopSources[s.span] = typ.id
			if s.source != c.inputName {
				c.problem(s.span, "loops may range only over the Reduce input")
			}
			if c.loopDepth != 0 {
				c.problem(s.span, "nested loops are not supported")
			}
			inner := cloneTypes(env)
			if _, exists := inner[s.name]; exists {
				c.problem(s.span, fmt.Sprintf("loop binding %q shadows an existing binding", s.name))
			}
			inner[s.name] = bindingFact{id: s.binding, typ: typeRef{name: typ.typ.name}}
			c.loopDepth++
			c.checkStatements(s.body, inner, result)
			c.loopDepth--
		case matchStmt:
			typ := c.expressionType(s.value, env)
			if typ.slice || typ.name != "Step" {
				c.problem(s.value.expressionSpan(), "match value must have type Step")
				continue
			}
			seen := make(map[string]bool)
			for _, arm := range s.arms {
				variant, exists := c.variants[arm.variant]
				if !exists || variant.union != typ.name {
					c.problem(arm.span, fmt.Sprintf("variant %q does not belong to %s", arm.variant, typ.name))
					continue
				}
				c.variantRefs[arm.span] = variant.id
				if seen[arm.variant] {
					c.problem(arm.span, fmt.Sprintf("duplicate match arm %q", arm.variant))
				}
				seen[arm.variant] = true
				if len(arm.binds) != len(variant.fields) {
					c.problem(arm.span, fmt.Sprintf("variant %s binds %d fields, want %d", arm.variant, len(arm.binds), len(variant.fields)))
					continue
				}
				inner := cloneTypes(env)
				for i, name := range arm.binds {
					c.checkIdentifier(name, arm.span)
					c.checkLocalIdentifier(name, arm.span)
					if _, exists := inner[name]; exists {
						c.problem(arm.span, fmt.Sprintf("match binding %q shadows an existing binding", name))
					}
					inner[name] = bindingFact{id: arm.bindings[i], typ: variant.fields[i].typ}
				}
				c.checkStatements(arm.body, inner, result)
			}
			for _, raw := range c.decls {
				if union, ok := raw.(unionDecl); ok && union.name == typ.name {
					for _, variant := range union.variants {
						if !seen[variant.name] {
							c.problem(s.span, fmt.Sprintf("non-exhaustive match: missing %s", variant.name))
						}
					}
				}
			}
		case returnStmt:
			got := c.expressionType(s.value, env)
			if !sameType(got, result) {
				c.problem(s.value.expressionSpan(), fmt.Sprintf("return has type %s, want %s", displayType(got), displayType(result)))
			}
		}
	}
}

func (c *checker) expressionType(expression expr, env map[string]bindingFact) typeRef {
	typ := c.inferExpressionType(expression, env)
	c.expressionTypes[expression.expressionSpan()] = typ
	return typ
}

func (c *checker) inferExpressionType(expression expr, env map[string]bindingFact) typeRef {
	switch e := expression.(type) {
	case intExpr:
		return typeRef{name: "i64", span: e.span}
	case sliceExpr:
		return typeRef{name: "i64", slice: true, span: e.span}
	case nameExpr:
		fact, ok := env[e.parts[0]]
		if !ok {
			c.problem(e.span, fmt.Sprintf("unknown binding %q", e.parts[0]))
			return typeRef{span: e.span}
		}
		base := fact.typ
		resolved := checkedName{binding: fact.id}
		for _, field := range e.parts[1:] {
			if base.slice {
				c.problem(e.span, "slice has no fields")
				return typeRef{span: e.span}
			}
			found := false
			for _, raw := range c.currentDecls() {
				if d, ok := raw.(recordDecl); ok && d.name == base.name {
					for _, f := range d.fields {
						if f.name == field {
							base = f.typ
							resolved.fieldIDs = append(resolved.fieldIDs, f.id)
							found = true
						}
					}
				}
			}
			if !found {
				c.problem(e.span, fmt.Sprintf("type %s has no field %q", base.name, field))
				return typeRef{span: e.span}
			}
		}
		c.names[e.span] = resolved
		return base
	case binaryExpr:
		left, right := c.expressionType(e.left, env), c.expressionType(e.right, env)
		if left.name != "i64" || left.slice || right.name != "i64" || right.slice {
			c.problem(e.span, "integer operator requires i64 operands")
		}
		if e.op == tEqual {
			return typeRef{name: "bool", span: e.span}
		}
		return typeRef{name: "i64", span: e.span}
	case ifExpr:
		if got := c.expressionType(e.condition, env); got.name != "bool" || got.slice {
			c.problem(e.condition.expressionSpan(), "if condition must be bool")
		}
		yes, no := c.expressionType(e.yes, env), c.expressionType(e.no, env)
		if !sameType(yes, no) {
			c.problem(e.span, "if branches have different types")
		}
		if yes.name != "Step" || yes.slice {
			c.problem(e.span, "if expressions are restricted to Step construction")
		}
		return yes
	case constructExpr:
		variant, ok := c.variants[e.name]
		if !ok {
			c.problem(e.span, fmt.Sprintf("unknown variant %q", e.name))
			return typeRef{span: e.span}
		}
		c.variantRefs[e.span] = variant.id
		provided := make(map[string]expr)
		for index, f := range e.fields {
			if index >= len(variant.fields) || f.name != variant.fields[index].name {
				c.problem(f.span, fmt.Sprintf("constructor %s fields must follow declaration order", e.name))
			}
			if _, exists := provided[f.name]; exists {
				c.problem(f.span, fmt.Sprintf("duplicate constructor field %q", f.name))
			}
			provided[f.name] = f.value
		}
		for _, want := range variant.fields {
			value, exists := provided[want.name]
			if !exists {
				c.problem(e.span, fmt.Sprintf("constructor %s is missing field %s", e.name, want.name))
				continue
			}
			if got := c.expressionType(value, env); !sameType(got, want.typ) {
				c.problem(value.expressionSpan(), fmt.Sprintf("field %s has type %s, want %s", want.name, displayType(got), displayType(want.typ)))
			}
			for _, field := range e.fields {
				if field.name == want.name {
					c.fieldRefs[field.span] = want.id
				}
			}
			delete(provided, want.name)
		}
		for _, field := range e.fields {
			if _, known := findField(variant.fields, field.name); !known {
				c.problem(field.value.expressionSpan(), fmt.Sprintf("constructor %s has no field %s", e.name, field.name))
			}
		}
		return typeRef{name: variant.union, span: e.span}
	case appendExpr:
		list, value := c.expressionType(e.list, env), c.expressionType(e.value, env)
		if !list.slice || list.name != "i64" || value.slice || value.name != "i64" {
			c.problem(e.span, "append requires []i64 and i64")
		}
		return typeRef{name: "i64", slice: true, span: e.span}
	case callExpr:
		targetID, ok := c.pkg.imports[e.alias]
		if !ok {
			c.problem(e.span, fmt.Sprintf("unknown import alias %q", e.alias))
			return typeRef{span: e.span}
		}
		target := c.project.packages[targetID]
		fn, ok := target.functions[e.function]
		if !ok {
			c.problem(e.span, fmt.Sprintf("package %s has no function %q", targetID, e.function))
			return typeRef{span: e.span}
		}
		if len(e.args) != len(fn.parameters) {
			c.problem(e.span, fmt.Sprintf("call %s.%s has %d arguments, want %d", e.alias, e.function, len(e.args), len(fn.parameters)))
		}
		for i, arg := range e.args {
			got := c.expressionType(arg, env)
			if i < len(fn.parameters) && !sameType(got, fn.parameters[i].typ) {
				c.problem(arg.expressionSpan(), fmt.Sprintf("argument %d has type %s, want %s", i+1, displayType(got), displayType(fn.parameters[i].typ)))
			}
		}
		c.calls[e.span] = checkedCall{packageID: targetID, declID: fn.id}
		return fn.result
	}
	return typeRef{}
}
func findField(fields []fieldDecl, name string) (fieldDecl, bool) {
	for _, field := range fields {
		if field.name == name {
			return field, true
		}
	}
	return fieldDecl{}, false
}

// currentModule is assigned only during the synchronous checker call through
// a receiver field in the next slice; this helper is replaced below by a
// declaration inventory passed explicitly.
func (c *checker) currentDecls() []decl { return c.decls }

func (c *checker) validType(t typeRef) bool {
	_, ok := c.types[t.name]
	return ok && (t.name == "i64" || !t.slice)
}
func (c *checker) problem(span Span, message string) {
	if len(c.diagnostics) < maximumDiagnostics {
		c.diagnostics = append(c.diagnostics, Diagnostic{span, message})
	}
}
func (c *checker) checkIdentifier(name string, span Span) {
	if !gotoken.IsIdentifier(name) || gotoken.Lookup(name).IsKeyword() {
		c.problem(span, fmt.Sprintf("invalid identifier %q", name))
	}
}
func (c *checker) checkLocalIdentifier(name string, span Span) {}
func cloneTypes(input map[string]bindingFact) map[string]bindingFact {
	out := make(map[string]bindingFact, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}
func sameType(a, b typeRef) bool { return a.name == b.name && a.slice == b.slice }
func displayType(t typeRef) string {
	if t.slice {
		return "[]" + t.name
	}
	return t.name
}
