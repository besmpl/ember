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

func collectRequireBindingsTree(resolver moduleResolver, from moduleKey, tree syntaxTree) (map[string]moduleKey, map[string]moduleRequireFieldBinding, error) {
	bindings := make(map[string]moduleKey)
	fieldBindings := make(map[string]moduleRequireFieldBinding)
	statements, _ := tree.statementIDs()
	for _, statement := range statements {
		local, ok := tree.localArena(statement)
		if !ok {
			continue
		}
		values, _ := tree.statementExpressions(local.values)
		for i, name := range consumerStatementStrings(tree, local.names) {
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

func moduleReturnLocalReferenceTree(tree syntaxTree) (string, string) {
	statements, _ := tree.statementIDs()
	for _, statement := range statements {
		ret, ok := tree.returnArena(statement)
		if !ok {
			continue
		}
		values, _ := tree.statementExpressions(ret.values)
		if len(values) == 0 {
			continue
		}
		value, ok := expressionSingleTerm(tree, values[0])
		if !ok {
			return "", ""
		}
		selectors, _ := tree.termSelectors(value)
		name := tree.termName(value)
		if len(selectors) == 0 {
			return name, ""
		}
		if len(selectors) == 1 && tree.termSelectorField(selectors[0]) != "" {
			return name, tree.termSelectorField(selectors[0])
		}
		return "", ""
	}
	return "", ""
}

func localFieldReference(tree syntaxTree, expr expressionID) (string, string, bool) {
	value, ok := expressionSingleTerm(tree, expr)
	if !ok || tree.termName(value) == "" {
		return "", "", false
	}
	selectors, _ := tree.termSelectors(value)
	if len(selectors) != 1 || tree.termSelectorField(selectors[0]) == "" {
		return "", "", false
	}
	return tree.termName(value), tree.termSelectorField(selectors[0]), true
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

func collectExpressionsRequireRequestsTree(tree syntaxTree, expressions []expressionID, requests *[]string) {
	for _, expr := range expressions {
		collectExpressionRequireRequests(tree, expr, requests)
	}
}

func collectRequireRequestsTree(tree syntaxTree) []string {
	var requests []string
	statements, _ := tree.statementIDs()
	collectStatementsRequireRequests(tree, statements, &requests)
	return requests
}

func collectStatementsRequireRequests(tree syntaxTree, statements []statementID, requests *[]string) {
	for _, statement := range statements {
		switch tree.statementKindID(statement) {
		case syntaxStatementLocal:
			local, _ := tree.localArena(statement)
			values, _ := tree.statementExpressions(local.values)
			collectExpressionsRequireRequestsTree(tree, values, requests)
		case syntaxStatementAssign:
			assign, _ := tree.assignmentArena(statement)
			values, _ := tree.statementExpressions(assign.values)
			collectExpressionsRequireRequestsTree(tree, values, requests)
		case syntaxStatementCall:
			node, _ := tree.statementNode(statement)
			collectTermRequireRequests(tree, termID(node.payload), requests)
		case syntaxStatementIf:
			ifStmt, _ := tree.ifArena(statement)
			collectExpressionRequireRequests(tree, ifStmt.condition, requests)
			thenStatements, _ := tree.statementChildren(ifStmt.thenStatements)
			elseStatements, _ := tree.statementChildren(ifStmt.elseStatements)
			collectStatementsRequireRequests(tree, thenStatements, requests)
			collectStatementsRequireRequests(tree, elseStatements, requests)
		case syntaxStatementWhile:
			loop, _ := tree.whileArena(statement)
			collectExpressionRequireRequests(tree, loop.condition, requests)
			body, _ := tree.statementChildren(loop.statements)
			collectStatementsRequireRequests(tree, body, requests)
		case syntaxStatementFor:
			loop, _ := tree.forArena(statement)
			collectExpressionRequireRequests(tree, loop.start, requests)
			collectExpressionRequireRequests(tree, loop.limit, requests)
			if step := loop.step; step != 0 {
				collectExpressionRequireRequests(tree, step, requests)
			}
			body, _ := tree.statementChildren(loop.statements)
			collectStatementsRequireRequests(tree, body, requests)
		case syntaxStatementGenericFor:
			loop, _ := tree.genericForArena(statement)
			values, _ := tree.statementExpressions(loop.values)
			body, _ := tree.statementChildren(loop.statements)
			collectExpressionsRequireRequestsTree(tree, values, requests)
			collectStatementsRequireRequests(tree, body, requests)
		case syntaxStatementRepeat:
			loop, _ := tree.repeatArena(statement)
			body, _ := tree.statementChildren(loop.statements)
			collectStatementsRequireRequests(tree, body, requests)
			collectExpressionRequireRequests(tree, loop.condition, requests)
		case syntaxStatementBlock:
			block, _ := tree.blockArena(statement)
			body, _ := tree.statementChildren(block.statements)
			collectStatementsRequireRequests(tree, body, requests)
		case syntaxStatementReturn:
			ret, _ := tree.returnArena(statement)
			values, _ := tree.statementExpressions(ret.values)
			collectExpressionsRequireRequestsTree(tree, values, requests)
		}
	}
}

func collectExpressionRequireRequests(tree syntaxTree, expr expressionID, requests *[]string) {
	if call, ok := expressionSingleCall(tree, expr); ok {
		collectCallRequireRequest(tree, call, requests)
		return
	}
	if term, ok := expressionSingleTerm(tree, expr); ok {
		collectTermRequireRequests(tree, term, requests)
	}
}

func collectTermRequireRequests(tree syntaxTree, value termID, requests *[]string) {
	if call, ok := tree.termCall(value); ok {
		collectCallRequireRequest(tree, call, requests)
	}
	if table, ok := tree.termTable(value); ok {
		fields, _ := tree.tableFields(table)
		for _, field := range fields {
			if key := tree.tableFieldKey(field); key != 0 {
				collectExpressionRequireRequests(tree, key, requests)
			}
			collectExpressionRequireRequests(tree, tree.tableFieldValue(field), requests)
		}
	}
	if function, ok := tree.termFunction(value); ok {
		if span, ok := tree.functionExpressionStatementIDs(function); ok {
			statements, _ := tree.statementChildren(span)
			collectStatementsRequireRequests(tree, statements, requests)
		}
	}
	if ifExpr, ok := tree.termIf(value); ok {
		collectExpressionRequireRequests(tree, tree.ifExpressionCondition(ifExpr), requests)
		collectExpressionRequireRequests(tree, tree.ifExpressionThen(ifExpr), requests)
		collectExpressionRequireRequests(tree, tree.ifExpressionElse(ifExpr), requests)
	}
	if group, ok := tree.termGroup(value); ok {
		collectExpressionRequireRequests(tree, group, requests)
	}
	if kind := tree.termKind(value); kind == syntaxTermUnaryNot || kind == syntaxTermUnaryMinus || kind == syntaxTermUnaryLength {
		if unary, ok := tree.termChild(value); ok {
			collectTermRequireRequests(tree, unary, requests)
		}
	}
	if power, ok := tree.termPower(value); ok {
		collectTermRequireRequests(tree, tree.powerBase(power), requests)
		collectTermRequireRequests(tree, tree.powerExponent(power), requests)
	}
	selectors, _ := tree.termSelectors(value)
	for _, selector := range selectors {
		if index := tree.termSelectorIndex(selector); index != 0 {
			collectExpressionRequireRequests(tree, index, requests)
		}
	}
}

func collectCallRequireRequest(tree syntaxTree, call arenaCallID, requests *[]string) {
	if request, ok := requireCallRequest(tree, call); ok {
		*requests = append(*requests, request)
	}
	collectTermRequireRequests(tree, tree.callTarget(call), requests)
	if receiver := tree.callReceiver(call); receiver != 0 {
	}
	args, _ := tree.callArgs(call)
	collectExpressionsRequireRequestsTree(tree, args, requests)
}

func requireCallRequest(tree syntaxTree, call arenaCallID) (string, bool) {
	target := tree.callTarget(call)
	selectors, _ := tree.termSelectors(target)
	args, _ := tree.callArgs(call)
	if tree.termName(target) != "require" || len(selectors) != 0 || len(args) == 0 {
		return "", false
	}
	return stringLiteralExpression(tree, args[0])
}

func stringLiteralExpression(tree syntaxTree, expr expressionID) (string, bool) {
	value, ok := expressionSingleTerm(tree, expr)
	lit, litOK := tree.termLiteral(value)
	if !ok || !litOK {
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
