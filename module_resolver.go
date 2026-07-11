package ember

import (
	"crypto/sha256"
	"fmt"
	"path"
	"strings"
)

type sourceIdentity struct {
	name string
	hash [32]byte
}

func identifyModuleSource(source Source) sourceIdentity {
	return sourceIdentity{
		name: source.Name,
		hash: sha256.Sum256([]byte(source.Text)),
	}
}

type moduleKeyKind string

const (
	moduleKeyLogical moduleKeyKind = "logical"
	moduleKeyHost    moduleKeyKind = "host"
)

type moduleKey struct {
	kind moduleKeyKind
	path string
}

func (k moduleKey) String() string {
	if k.kind == "" {
		return ""
	}
	return string(k.kind) + ":" + k.path
}

type resolvedModuleSource struct {
	Key      moduleKey
	Source   Source
	Identity sourceIdentity
}

type moduleResolver interface {
	Source(key moduleKey) (resolvedModuleSource, error)
	Resolve(from moduleKey, request string) (resolvedModuleSource, error)
}

type moduleGraph struct {
	Root  moduleKey
	Nodes map[moduleKey]moduleGraphNode
}

type moduleSummaryArtifact struct {
	Summary ModuleSummary
	Trusted bool
}

type moduleGraphNode struct {
	Source               Source
	Identity             sourceIdentity
	Requires             []moduleKey
	RequireBindings      map[string]moduleKey
	RequireFieldBindings map[string]moduleRequireFieldBinding
	ReturnLocal          string
	ReturnField          string
}

type moduleCycleError struct {
	Path []string
}

func (e moduleCycleError) Error() string {
	return "module resolver: cycle " + strings.Join(e.Path, " -> ")
}

func (e moduleCycleError) Diagnostic() moduleDiagnostic {
	return moduleDiagnostic{
		Code:    "module-cycle",
		Message: e.Error(),
		Path:    append([]string(nil), e.Path...),
	}
}

type moduleDiagnostic struct {
	Code    string
	Message string
	Path    []string
}

func buildModuleGraphWithStore(resolver moduleResolver, root moduleKey, store *sourceArtifactStore) (moduleGraph, error) {
	graph := moduleGraph{
		Root:  root,
		Nodes: make(map[moduleKey]moduleGraphNode),
	}
	if err := graph.visit(resolver, store, root, nil); err != nil {
		return moduleGraph{}, err
	}
	return graph, nil
}

func moduleExportByNameKind(exports []ModuleExport, name string, kind ModuleExportKind) (ModuleExport, bool) {
	for _, item := range exports {
		if item.Name == name && item.Kind == kind {
			return item, true
		}
	}
	return ModuleExport{}, false
}

func moduleDependencySummaries(graph moduleGraph, requires []moduleKey) []ModuleDependencySummary {
	if len(requires) == 0 {
		return nil
	}
	dependencies := make([]ModuleDependencySummary, 0, len(requires))
	for _, key := range requires {
		dependency := ModuleDependencySummary{
			Key:  key.String(),
			Kind: moduleDependencyKind(key.kind),
			Path: key.path,
		}
		if node, ok := graph.Nodes[key]; ok {
			dependency.InvalidationHash = sourceInvalidationHash(node.Source)
		}
		dependencies = append(dependencies, dependency)
	}
	return dependencies
}

func moduleDependencyKind(kind moduleKeyKind) ModuleDependencyKind {
	switch kind {
	case moduleKeyHost:
		return ModuleDependencyHost
	default:
		return ModuleDependencyLogical
	}
}

func (g *moduleGraph) visit(resolver moduleResolver, store *sourceArtifactStore, key moduleKey, stack []moduleKey) error {
	if cyclePath, ok := moduleCyclePath(stack, key); ok {
		return moduleCycleError{Path: cyclePath}
	}
	if _, ok := g.Nodes[key]; ok {
		return nil
	}

	source, err := resolver.Source(key)
	if err != nil {
		return err
	}
	artifact, err := store.parse(source.Source, source.Identity)
	if err != nil {
		return fmt.Errorf("module resolver: parse %s: %w", key.String(), err)
	}

	node := moduleGraphNode{
		Source:               source.Source,
		Identity:             source.Identity,
		RequireBindings:      make(map[string]moduleKey),
		RequireFieldBindings: make(map[string]moduleRequireFieldBinding),
	}
	node.ReturnLocal, node.ReturnField = moduleReturnLocalReference(artifact.program)
	requests := collectRequireRequests(artifact.program)
	for _, request := range requests {
		required, err := resolver.Resolve(key, request)
		if err != nil {
			return err
		}
		node.Requires = append(node.Requires, required.Key)
	}
	bindings, fieldBindings, err := collectRequireBindings(resolver, key, artifact.program)
	if err != nil {
		return err
	}
	node.RequireBindings = bindings
	node.RequireFieldBindings = fieldBindings
	g.Nodes[key] = node

	nextStack := append(append([]moduleKey(nil), stack...), key)
	for _, required := range node.Requires {
		if err := g.visit(resolver, store, required, nextStack); err != nil {
			return err
		}
	}
	return nil
}

type moduleRequireFieldBinding struct {
	Required moduleKey
	Field    string
}

func collectRequireBindings(resolver moduleResolver, from moduleKey, prog program) (map[string]moduleKey, map[string]moduleRequireFieldBinding, error) {
	bindings := make(map[string]moduleKey)
	fieldBindings := make(map[string]moduleRequireFieldBinding)
	for _, stmt := range prog.statements {
		if stmt.local == nil {
			continue
		}
		for i, name := range stmt.local.names {
			if i >= len(stmt.local.values) {
				continue
			}
			if call, ok := expressionSingleCall(stmt.local.values[i]); ok {
				if request, ok := requireCallRequest(call); ok {
					required, err := resolver.Resolve(from, request)
					if err != nil {
						return nil, nil, err
					}
					bindings[name] = required.Key
					continue
				}
			}
			base, field, ok := localFieldReference(stmt.local.values[i])
			if !ok {
				continue
			}
			if required, ok := bindings[base]; ok {
				fieldBindings[name] = moduleRequireFieldBinding{
					Required: required,
					Field:    field,
				}
			}
		}
	}
	return bindings, fieldBindings, nil
}

func moduleReturnLocalReference(prog program) (string, string) {
	for _, stmt := range prog.statements {
		if stmt.ret == nil || len(stmt.ret.values) == 0 {
			continue
		}
		value, ok := expressionSingleTerm(stmt.ret.values[0])
		if !ok {
			return "", ""
		}
		if len(value.selectors) == 0 {
			return value.name, ""
		}
		if len(value.selectors) == 1 && value.selectors[0].field != "" {
			return value.name, value.selectors[0].field
		}
		return "", ""
	}
	return "", ""
}

func localFieldReference(expr expression) (string, string, bool) {
	value, ok := expressionSingleTerm(expr)
	if !ok || value.name == "" {
		return "", "", false
	}
	if len(value.selectors) != 1 || value.selectors[0].field == "" {
		return "", "", false
	}
	return value.name, value.selectors[0].field, true
}

func moduleCyclePath(stack []moduleKey, key moduleKey) ([]string, bool) {
	for i, existing := range stack {
		if existing != key {
			continue
		}
		path := make([]string, 0, len(stack)-i+1)
		for _, item := range stack[i:] {
			path = append(path, item.String())
		}
		path = append(path, key.String())
		return path, true
	}
	return nil, false
}

func collectExpressionsRequireRequests(expressions []expression, requests *[]string) {
	for _, expr := range expressions {
		collectExpressionRequireRequests(expr, requests)
	}
}

func collectRequireRequests(prog program) []string {
	var requests []string
	collectStatementsRequireRequests(prog.statements, &requests)
	return requests
}

func collectStatementsRequireRequests(statements []statement, requests *[]string) {
	for _, stmt := range statements {
		switch {
		case stmt.local != nil:
			collectExpressionsRequireRequests(stmt.local.values, requests)
		case stmt.assign != nil:
			collectExpressionsRequireRequests(stmt.assign.values, requests)
		case stmt.call != nil:
			collectTermRequireRequests(*stmt.call, requests)
		case stmt.ifStmt != nil:
			collectExpressionRequireRequests(stmt.ifStmt.condition, requests)
			collectStatementsRequireRequests(stmt.ifStmt.thenStatements, requests)
			collectStatementsRequireRequests(stmt.ifStmt.elseStatements, requests)
		case stmt.while != nil:
			collectExpressionRequireRequests(stmt.while.condition, requests)
			collectStatementsRequireRequests(stmt.while.statements, requests)
		case stmt.forLoop != nil:
			collectExpressionRequireRequests(stmt.forLoop.start, requests)
			collectExpressionRequireRequests(stmt.forLoop.limit, requests)
			if stmt.forLoop.step != nil {
				collectExpressionRequireRequests(*stmt.forLoop.step, requests)
			}
			collectStatementsRequireRequests(stmt.forLoop.statements, requests)
		case stmt.genericFor != nil:
			collectExpressionsRequireRequests(stmt.genericFor.values, requests)
			collectStatementsRequireRequests(stmt.genericFor.statements, requests)
		case stmt.repeat != nil:
			collectStatementsRequireRequests(stmt.repeat.statements, requests)
			collectExpressionRequireRequests(stmt.repeat.condition, requests)
		case stmt.block != nil:
			collectStatementsRequireRequests(stmt.block.statements, requests)
		case stmt.ret != nil:
			collectExpressionsRequireRequests(stmt.ret.values, requests)
		}
	}
}

func collectExpressionRequireRequests(expr expression, requests *[]string) {
	if call, ok := expressionSingleCall(expr); ok {
		collectCallRequireRequest(call, requests)
		return
	}
	if term, ok := expressionSingleTerm(expr); ok {
		collectTermRequireRequests(term, requests)
	}
}

func collectTermRequireRequests(value term, requests *[]string) {
	if value.call != nil {
		collectCallRequireRequest(*value.call, requests)
	}
	if value.table != nil {
		for _, field := range value.table.fields {
			if field.key != nil {
				collectExpressionRequireRequests(*field.key, requests)
			}
			collectExpressionRequireRequests(field.value, requests)
		}
	}
	if value.function != nil {
		collectStatementsRequireRequests(value.function.statements, requests)
	}
	if value.ifExpr != nil {
		collectExpressionRequireRequests(value.ifExpr.condition, requests)
		collectExpressionRequireRequests(value.ifExpr.thenValue, requests)
		collectExpressionRequireRequests(value.ifExpr.elseValue, requests)
	}
	if value.group != nil {
		collectExpressionRequireRequests(*value.group, requests)
	}
	if value.unaryNot != nil {
		collectTermRequireRequests(*value.unaryNot, requests)
	}
	if value.unaryMinus != nil {
		collectTermRequireRequests(*value.unaryMinus, requests)
	}
	if value.unaryLen != nil {
		collectTermRequireRequests(*value.unaryLen, requests)
	}
	if value.power != nil {
		collectTermRequireRequests(value.power.base, requests)
		collectTermRequireRequests(value.power.exponent, requests)
	}
	for _, selector := range value.selectors {
		if selector.index != nil {
			collectExpressionRequireRequests(*selector.index, requests)
		}
	}
}

func collectCallRequireRequest(call callExpression, requests *[]string) {
	if request, ok := requireCallRequest(call); ok {
		*requests = append(*requests, request)
	}
	collectTermRequireRequests(call.target, requests)
	if call.receiver != nil {
		collectTermRequireRequests(*call.receiver, requests)
	}
	collectExpressionsRequireRequests(call.args, requests)
}

func requireCallRequest(call callExpression) (string, bool) {
	if call.target.name != "require" || len(call.target.selectors) != 0 || len(call.args) == 0 {
		return "", false
	}
	return stringLiteralExpression(call.args[0])
}

func stringLiteralExpression(expr expression) (string, bool) {
	value, ok := expressionSingleTerm(expr)
	if !ok || value.lit == nil {
		return "", false
	}
	return value.lit.String()
}

func parseModuleKey(name string) (moduleKey, error) {
	if strings.HasPrefix(name, string(moduleKeyHost)+":") {
		return hostModuleKey(strings.TrimPrefix(name, string(moduleKeyHost)+":"))
	}
	if strings.HasPrefix(name, string(moduleKeyLogical)+":") {
		return logicalModuleKey(strings.TrimPrefix(name, string(moduleKeyLogical)+":"))
	}
	return logicalModuleKey(name)
}

func normalizeRequireKey(from moduleKey, request string) (moduleKey, error) {
	if request == "" {
		return moduleKey{}, fmt.Errorf("module resolver: empty require path")
	}
	if strings.HasPrefix(request, string(moduleKeyHost)+":") {
		return hostModuleKey(strings.TrimPrefix(request, string(moduleKeyHost)+":"))
	}
	if isRelativeModuleRequest(request) {
		if from.kind != moduleKeyLogical {
			return moduleKey{}, fmt.Errorf("module resolver: relative require from non-logical module %s", from.String())
		}
		base := path.Dir(from.path)
		if from.path == "" || from.path == "." {
			base = "."
		}
		return logicalModuleKey(path.Join(base, request))
	}
	return logicalModuleKey(request)
}

func logicalModuleKey(name string) (moduleKey, error) {
	clean, err := normalizeModulePath(name)
	if err != nil {
		return moduleKey{}, err
	}
	return moduleKey{kind: moduleKeyLogical, path: clean}, nil
}

func hostModuleKey(name string) (moduleKey, error) {
	clean, err := normalizeModulePath(name)
	if err != nil {
		return moduleKey{}, err
	}
	return moduleKey{kind: moduleKeyHost, path: clean}, nil
}

func normalizeModulePath(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("module resolver: empty module path")
	}
	if strings.Contains(name, "\\") {
		name = strings.ReplaceAll(name, "\\", "/")
	}
	clean := path.Clean(name)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("module resolver: empty module path")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("module resolver: module path %q escapes root", name)
	}
	return clean, nil
}

func isRelativeModuleRequest(request string) bool {
	return request == "." ||
		request == ".." ||
		strings.HasPrefix(request, "./") ||
		strings.HasPrefix(request, "../")
}
