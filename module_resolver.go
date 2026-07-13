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
	return buildModuleGraphWithStoreAndLimits(resolver, root, store, CompileLimits{})
}

func buildModuleGraphWithStoreAndLimits(resolver moduleResolver, root moduleKey, store *sourceArtifactStore, limits CompileLimits) (moduleGraph, error) {
	graph := moduleGraph{
		Root:  root,
		Nodes: make(map[moduleKey]moduleGraphNode),
	}
	if err := graph.visit(resolver, store, root, nil, limits); err != nil {
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

func (g *moduleGraph) visit(resolver moduleResolver, store *sourceArtifactStore, key moduleKey, stack []moduleKey, limits CompileLimits) error {
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
	artifact, err := store.parseWithLimits(source.Source, source.Identity, limits)
	if err != nil {
		return fmt.Errorf("module resolver: parse %s: %w", key.String(), err)
	}

	node := moduleGraphNode{
		Source:               source.Source,
		Identity:             source.Identity,
		RequireBindings:      make(map[string]moduleKey),
		RequireFieldBindings: make(map[string]moduleRequireFieldBinding),
	}
	node.ReturnLocal, node.ReturnField = moduleReturnLocalReferenceTree(artifact.tree)
	requests := collectRequireRequestsTree(artifact.tree)
	for _, request := range requests {
		required, err := resolver.Resolve(key, request)
		if err != nil {
			return err
		}
		node.Requires = append(node.Requires, required.Key)
	}
	bindings, fieldBindings, err := collectRequireBindingsTree(resolver, key, artifact.tree)
	if err != nil {
		return err
	}
	node.RequireBindings = bindings
	node.RequireFieldBindings = fieldBindings
	g.Nodes[key] = node

	nextStack := append(append([]moduleKey(nil), stack...), key)
	for _, required := range node.Requires {
		if err := g.visit(resolver, store, required, nextStack, limits); err != nil {
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
	return collectRequireBindingsTree(resolver, from, newSyntaxTree(prog))
}

func collectRequireBindingsTree(resolver moduleResolver, from moduleKey, tree syntaxTree) (map[string]moduleKey, map[string]moduleRequireFieldBinding, error) {
	bindings := make(map[string]moduleKey)
	fieldBindings := make(map[string]moduleRequireFieldBinding)
	for i := range tree.statements() {
		stmt := tree.statement(i)
		local := tree.local(stmt)
		if local == nil {
			continue
		}
		for i, name := range tree.localNames(local) {
			values := tree.localValues(local)
			if i >= len(values) {
				continue
			}
			if call, ok := expressionSingleCall(tree, values[i]); ok {
				if request, ok := requireCallRequest(tree, call); ok {
					required, err := resolver.Resolve(from, request)
					if err != nil {
						return nil, nil, err
					}
					bindings[name] = required.Key
					continue
				}
			}
			base, field, ok := localFieldReference(tree, values[i])
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
	return moduleReturnLocalReferenceTree(newSyntaxTree(prog))
}

func moduleReturnLocalReferenceTree(tree syntaxTree) (string, string) {
	for i := range tree.statements() {
		stmt := tree.statement(i)
		ret := tree.returnStatement(stmt)
		if ret == nil || len(tree.returnValues(ret)) == 0 {
			continue
		}
		value, ok := expressionSingleTerm(tree, tree.returnValues(ret)[0])
		if !ok {
			return "", ""
		}
		selectors := tree.termSelectors(&value)
		name := tree.termName(&value)
		if len(selectors) == 0 {
			return name, ""
		}
		if len(selectors) == 1 && tree.selectorField(&selectors[0]) != "" {
			return name, tree.selectorField(&selectors[0])
		}
		return "", ""
	}
	return "", ""
}

func localFieldReference(tree syntaxTree, expr expression) (string, string, bool) {
	value, ok := expressionSingleTerm(tree, expr)
	if !ok || tree.termName(&value) == "" {
		return "", "", false
	}
	selectors := tree.termSelectors(&value)
	if len(selectors) != 1 || tree.selectorField(&selectors[0]) == "" {
		return "", "", false
	}
	return tree.termName(&value), tree.selectorField(&selectors[0]), true
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

func collectExpressionsRequireRequestsTree(tree syntaxTree, expressions []expression, requests *[]string) {
	for _, expr := range expressions {
		collectExpressionRequireRequests(tree, expr, requests)
	}
}

func collectRequireRequests(prog program) []string {
	return collectRequireRequestsTree(newSyntaxTree(prog))
}

func collectRequireRequestsTree(tree syntaxTree) []string {
	var requests []string
	collectStatementsRequireRequests(tree, tree.statements(), &requests)
	return requests
}

func collectStatementsRequireRequests(tree syntaxTree, statements []statement, requests *[]string) {
	for i := range statements {
		stmt := &statements[i]
		switch tree.statementKind(stmt) {
		case syntaxStatementLocal:
			collectExpressionsRequireRequestsTree(tree, tree.localValues(tree.local(stmt)), requests)
		case syntaxStatementAssign:
			collectExpressionsRequireRequestsTree(tree, tree.assignmentValues(tree.assignment(stmt)), requests)
		case syntaxStatementCall:
			collectTermRequireRequests(tree, *tree.call(stmt), requests)
		case syntaxStatementIf:
			ifStmt := tree.ifStatement(stmt)
			collectExpressionRequireRequests(tree, *tree.ifCondition(ifStmt), requests)
			collectStatementsRequireRequests(tree, tree.ifThenStatements(ifStmt), requests)
			collectStatementsRequireRequests(tree, tree.ifElseStatements(ifStmt), requests)
		case syntaxStatementWhile:
			while := tree.whileStatement(stmt)
			collectExpressionRequireRequests(tree, *tree.whileCondition(while), requests)
			collectStatementsRequireRequests(tree, tree.whileStatements(while), requests)
		case syntaxStatementFor:
			loop := tree.forStatement(stmt)
			collectExpressionRequireRequests(tree, *tree.numericForStart(loop), requests)
			collectExpressionRequireRequests(tree, *tree.numericForLimit(loop), requests)
			if step := tree.numericForStep(loop); step != nil {
				collectExpressionRequireRequests(tree, *step, requests)
			}
			collectStatementsRequireRequests(tree, tree.numericForStatements(loop), requests)
		case syntaxStatementGenericFor:
			loop := tree.genericForStatement(stmt)
			collectExpressionsRequireRequestsTree(tree, tree.genericForValues(loop), requests)
			collectStatementsRequireRequests(tree, tree.genericForStatements(loop), requests)
		case syntaxStatementRepeat:
			loop := tree.repeatStatement(stmt)
			collectStatementsRequireRequests(tree, tree.repeatStatements(loop), requests)
			collectExpressionRequireRequests(tree, *tree.repeatCondition(loop), requests)
		case syntaxStatementBlock:
			collectStatementsRequireRequests(tree, tree.blockStatements(tree.blockStatement(stmt)), requests)
		case syntaxStatementReturn:
			collectExpressionsRequireRequestsTree(tree, tree.returnValues(tree.returnStatement(stmt)), requests)
		}
	}
}

func collectExpressionRequireRequests(tree syntaxTree, expr expression, requests *[]string) {
	if call, ok := expressionSingleCall(tree, expr); ok {
		collectCallRequireRequest(tree, call, requests)
		return
	}
	if term, ok := expressionSingleTerm(tree, expr); ok {
		collectTermRequireRequests(tree, term, requests)
	}
}

func collectTermRequireRequests(tree syntaxTree, value term, requests *[]string) {
	if call := tree.termCall(&value); call != nil {
		collectCallRequireRequest(tree, *call, requests)
	}
	if table := tree.termTable(&value); table != nil {
		for i := range tree.tableFields(table) {
			field := &tree.tableFields(table)[i]
			if key := tree.tableFieldKey(field); key != nil {
				collectExpressionRequireRequests(tree, *key, requests)
			}
			collectExpressionRequireRequests(tree, *tree.tableFieldValue(field), requests)
		}
	}
	if function := tree.termFunction(&value); function != nil {
		collectStatementsRequireRequests(tree, tree.functionExpressionStatements(function), requests)
	}
	if ifExpr := tree.termIf(&value); ifExpr != nil {
		collectExpressionRequireRequests(tree, *tree.ifExpressionCondition(ifExpr), requests)
		collectExpressionRequireRequests(tree, *tree.ifExpressionThen(ifExpr), requests)
		collectExpressionRequireRequests(tree, *tree.ifExpressionElse(ifExpr), requests)
	}
	if group := tree.termGroup(&value); group != nil {
		collectExpressionRequireRequests(tree, *group, requests)
	}
	if unary := tree.termUnaryNot(&value); unary != nil {
		collectTermRequireRequests(tree, *unary, requests)
	}
	if unary := tree.termUnaryMinus(&value); unary != nil {
		collectTermRequireRequests(tree, *unary, requests)
	}
	if unary := tree.termUnaryLength(&value); unary != nil {
		collectTermRequireRequests(tree, *unary, requests)
	}
	if power := tree.termPower(&value); power != nil {
		collectTermRequireRequests(tree, *tree.powerBase(power), requests)
		collectTermRequireRequests(tree, *tree.powerExponent(power), requests)
	}
	for i := range tree.termSelectors(&value) {
		selector := &tree.termSelectors(&value)[i]
		if index := tree.selectorIndex(selector); index != nil {
			collectExpressionRequireRequests(tree, *index, requests)
		}
	}
}

func collectCallRequireRequest(tree syntaxTree, call callExpression, requests *[]string) {
	if request, ok := requireCallRequest(tree, call); ok {
		*requests = append(*requests, request)
	}
	collectTermRequireRequests(tree, *tree.callTarget(&call), requests)
	if receiver := tree.callReceiver(&call); receiver != nil {
		collectTermRequireRequests(tree, *receiver, requests)
	}
	collectExpressionsRequireRequestsTree(tree, tree.callArgs(&call), requests)
}

func requireCallRequest(tree syntaxTree, call callExpression) (string, bool) {
	target := tree.callTarget(&call)
	if tree.termName(target) != "require" || len(tree.termSelectors(target)) != 0 || len(tree.callArgs(&call)) == 0 {
		return "", false
	}
	return stringLiteralExpression(tree, tree.callArgs(&call)[0])
}

func stringLiteralExpression(tree syntaxTree, expr expression) (string, bool) {
	value, ok := expressionSingleTerm(tree, expr)
	lit := tree.termLiteral(&value)
	if !ok || lit == nil {
		return "", false
	}
	return lit.String()
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
