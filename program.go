package ember

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync"
)

// ModuleKind names an Ember module namespace.
type ModuleKind string

const (
	// ModuleLogical is the authored source module namespace.
	ModuleLogical ModuleKind = "logical"
	// ModuleHost is the host-provided module namespace.
	ModuleHost ModuleKind = "host"
)

// ModuleID identifies one source module without exposing resolver internals.
type ModuleID struct {
	kind ModuleKind
	path string
}

// LogicalModule returns an identifier in the authored source module namespace.
func LogicalModule(path string) ModuleID {
	return ModuleID{kind: ModuleLogical, path: path}
}

// HostModule returns an identifier in the host-provided module namespace.
func HostModule(path string) ModuleID {
	return ModuleID{kind: ModuleHost, path: path}
}

// String returns the stable printable module identifier.
func (id ModuleID) String() string {
	if id.kind == "" || id.path == "" {
		return ""
	}
	return string(id.kind) + ":" + id.path
}

// ModuleLoader loads source text for one module.
type ModuleLoader interface {
	LoadModule(context.Context, ModuleID) (Source, error)
}

// Entrypoint names a module loaded by a runtime.
type Entrypoint struct {
	Name   string
	Module ModuleID
}

// ProgramOptions configures immutable program loading.
type ProgramOptions struct {
	Entrypoints []Entrypoint
	Check       bool
	Parallelism int
}

// Program is an immutable compiled module graph.
type Program struct {
	entrypoints []programEntrypoint
	graph       moduleGraph
	protos      map[moduleKey]*Proto
}

type programEntrypoint struct {
	name string
	key  moduleKey
}

// LoadReport describes the stable outcome of loading a Program.
type LoadReport struct {
	Entrypoints []EntrypointReport
	Modules     []ModuleReport
	Diagnostics []Diagnostic
}

// EntrypointReport describes one configured entrypoint.
type EntrypointReport struct {
	Name   string
	Module ModuleID
}

// ModuleReport describes one module in a loaded Program graph.
type ModuleReport struct {
	Module     ModuleID
	SourceName string
	Summary    ModuleSummary
}

// RuntimeOptions configures mutable execution state for a Program.
type RuntimeOptions struct {
	Host            RuntimeHost
	MaxInstructions uint64
}

// RuntimeHost supplies host-owned values for runtime calls.
type RuntimeHost interface {
	Globals(context.Context, HostCall) (map[string]Value, error)
}

// RuntimeHostFunc adapts a function into a RuntimeHost.
type RuntimeHostFunc func(context.Context, HostCall) (map[string]Value, error)

// Globals calls fn(ctx, call).
func (fn RuntimeHostFunc) Globals(ctx context.Context, call HostCall) (map[string]Value, error) {
	return fn(ctx, call)
}

// HostCall describes the runtime call site requesting host globals.
type HostCall struct {
	Entrypoint string
	Module     ModuleID
	Hook       string
}

// Runtime owns mutable execution state for one Program owner. RunHook and
// captured Callback calls must not overlap. Close may run concurrently; it
// reports an active runtime without tearing down the in-flight call.
type Runtime struct {
	closeMu         sync.Mutex
	owner           *runtimeOwner
	program         *Program
	host            RuntimeHost
	entrypoints     map[moduleKey]Value
	loaded          map[moduleKey]Value
	active          map[moduleKey]bool
	stack           []moduleKey
	maxInstructions int
	closed          bool
}

// HookReport describes one RunHook call.
type HookReport struct {
	Hook  string
	Calls []HookCallReport
}

// HookCallReport describes one entrypoint considered during RunHook.
type HookCallReport struct {
	Entrypoint string
	Module     ModuleID
	Hook       string
	Loaded     bool
	Called     bool
	Skipped    bool
}

// LoadProgram loads, parses, checks if requested, and compiles an immutable
// module graph. Top-level script code is not executed during loading.
func LoadProgram(ctx context.Context, loader ModuleLoader, options ProgramOptions) (*Program, LoadReport, error) {
	return loadProgramWithArtifactStore(ctx, loader, options, newSourceArtifactStore())
}

func loadProgramWithArtifactStore(ctx context.Context, loader ModuleLoader, options ProgramOptions, artifacts *sourceArtifactStore) (*Program, LoadReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, LoadReport{}, err
	}
	if loader == nil {
		return nil, LoadReport{}, fmt.Errorf("load program: nil module loader")
	}
	parallelism, err := programParallelism(options.Parallelism)
	if err != nil {
		return nil, LoadReport{}, err
	}
	if len(options.Entrypoints) == 0 {
		return nil, LoadReport{}, fmt.Errorf("load program: no entrypoints")
	}

	entrypoints, report, err := validateProgramEntrypoints(options.Entrypoints)
	if err != nil {
		return nil, report, err
	}

	combined, err := loadProgramGraph(ctx, loader, entrypoints, parallelism, artifacts)
	if err != nil {
		if cycle, ok := err.(moduleCycleError); ok {
			report.Diagnostics = []Diagnostic{diagnosticFromModuleDiagnostic(cycle.Diagnostic())}
			return nil, report, nil
		}
		return nil, report, err
	}

	protos, err := compileProgramModules(ctx, combined, artifacts, parallelism)
	if err != nil {
		return nil, report, err
	}
	var summaries map[moduleKey]moduleSummaryArtifact
	if options.Check {
		checkReport, err := checkProgramModules(ctx, combined, artifacts, parallelism)
		if err != nil {
			return nil, report, err
		}
		report.Diagnostics = checkReport.diagnostics
		summaries = checkReport.summaries
	}
	report.Modules = moduleReports(combined, summaries)

	return &Program{
		entrypoints: entrypoints,
		graph:       combined,
		protos:      protos,
	}, report, nil
}

// NewRuntime creates mutable execution state for p. It does not execute script
// code until RunHook is called.
func (p *Program) NewRuntime(options RuntimeOptions) (*Runtime, error) {
	if p == nil {
		return nil, fmt.Errorf("runtime: nil program")
	}
	maxInstructions, err := runtimeInstructionBudget(options.MaxInstructions)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		owner:           newRuntimeOwner(),
		program:         p,
		host:            options.Host,
		entrypoints:     make(map[moduleKey]Value),
		loaded:          make(map[moduleKey]Value),
		active:          make(map[moduleKey]bool),
		maxInstructions: maxInstructions,
	}, nil
}

// RunHook loads entrypoints lazily and calls hook in entrypoint order.
func (r *Runtime) RunHook(ctx context.Context, hook string, args ...Value) (HookReport, error) {
	report := HookReport{Hook: hook}
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil {
		return report, fmt.Errorf("runtime: nil runtime")
	}
	lease, err := r.beginRun()
	if err == errRuntimeOwnerClosed {
		return report, fmt.Errorf("runtime: closed")
	}
	if err != nil {
		return report, fmt.Errorf("runtime: begin run: %w", err)
	}
	defer lease.end()
	if hook == "" {
		return report, fmt.Errorf("runtime: empty hook")
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}

	for _, entrypoint := range r.program.entrypoints {
		call := HookCallReport{
			Entrypoint: entrypoint.name,
			Module:     moduleIDFromKey(entrypoint.key),
			Hook:       hook,
		}
		loadGlobals, err := r.hostGlobals(ctx, HostCall{
			Entrypoint: entrypoint.name,
			Module:     moduleIDFromKey(entrypoint.key),
		})
		if err != nil {
			return report, fmt.Errorf("runtime: host globals for %s load: %w", entrypoint.name, err)
		}
		export, loaded, err := r.loadEntrypoint(ctx, entrypoint, loadGlobals)
		if err != nil {
			return report, fmt.Errorf("runtime: load entrypoint %s: %w", entrypoint.name, err)
		}
		call.Loaded = loaded

		hookGlobals, err := r.hostGlobals(ctx, HostCall{
			Entrypoint: entrypoint.name,
			Module:     moduleIDFromKey(entrypoint.key),
			Hook:       hook,
		})
		if err != nil {
			return report, fmt.Errorf("runtime: host globals for %s.%s: %w", entrypoint.name, hook, err)
		}

		table, ok := export.Table()
		if export.IsNil() {
			call.Skipped = true
			report.Calls = append(report.Calls, call)
			continue
		}
		if !ok {
			return report, fmt.Errorf("runtime: entrypoint %s returned %s, want table or nil", entrypoint.name, export.Kind())
		}
		hookValue, err := runtimeTableAccess(runtimeGlobalsWithOwner(hookGlobals, r.owner)).get(table, StringValue(hook))
		if err != nil {
			return report, fmt.Errorf("runtime: get hook %s.%s: %w", entrypoint.name, hook, err)
		}
		if hookValue.IsNil() {
			call.Skipped = true
			report.Calls = append(report.Calls, call)
			continue
		}
		if !callableValue(hookValue) {
			return report, fmt.Errorf("runtime: hook %s.%s is %s, want function", entrypoint.name, hook, hookValue.Kind())
		}
		callContext := r.newRuntimeCallContext(ctx, entrypoint.key, hookGlobals, r.maxInstructions)
		callCtx := contextWithRuntimeCallContext(ctx, callContext)
		if _, err := callValueWithContextBudget(callCtx, hookValue, callContext.envWithRequire(), args, r.maxInstructions); err != nil {
			return report, fmt.Errorf("runtime: call hook %s.%s: %w", entrypoint.name, hook, err)
		}
		call.Called = true
		report.Calls = append(report.Calls, call)
	}
	return report, nil
}

func (r *Runtime) beginRun() (*runtimeRunLease, error) {
	if r == nil {
		return nil, errRuntimeOwnerReleased
	}
	r.closeMu.Lock()
	closed := r.closed
	owner := r.owner
	r.closeMu.Unlock()
	if closed {
		return nil, errRuntimeOwnerClosed
	}
	if owner == nil {
		return nil, errRuntimeOwnerInvalid
	}
	return owner.beginRun()
}

// Close releases mutable runtime references. It is safe to call repeatedly.
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.closeMu.Lock()
	defer r.closeMu.Unlock()
	if r.closed {
		return nil
	}
	if err := r.owner.close(); err != nil {
		if err == errRuntimeOwnerActive {
			return fmt.Errorf("runtime: active")
		}
		return fmt.Errorf("runtime: close: %w", err)
	}
	r.closed = true
	r.program = nil
	r.host = nil
	r.entrypoints = nil
	r.loaded = nil
	r.active = nil
	r.stack = nil
	return nil
}

func (r *Runtime) loadEntrypoint(ctx context.Context, entrypoint programEntrypoint, globals map[string]Value) (Value, bool, error) {
	if value, ok := r.entrypoints[entrypoint.key]; ok {
		return value, false, nil
	}
	if err := ctx.Err(); err != nil {
		return NilValue(), false, err
	}
	results, err := r.runModuleWithContextGlobalsBudget(ctx, entrypoint.key, globals, r.maxInstructions)
	if err != nil {
		return NilValue(), false, err
	}
	value := firstRuntimeResult(results)
	r.entrypoints[entrypoint.key] = value
	return value, true, nil
}

func (r *Runtime) hostGlobals(ctx context.Context, call HostCall) (map[string]Value, error) {
	if r.host == nil {
		return nil, nil
	}
	globals, err := r.host.Globals(ctx, call)
	if err != nil {
		return nil, err
	}
	return copyGlobals(globals), nil
}

func copyGlobals(globals map[string]Value) map[string]Value {
	if len(globals) == 0 {
		return nil
	}
	copied := make(map[string]Value, len(globals))
	for name, value := range globals {
		copied[name] = value
	}
	return copied
}

func runtimeInstructionBudget(max uint64) (int, error) {
	if max == 0 {
		return -1, nil
	}
	maxInt := int(^uint(0) >> 1)
	if max > uint64(maxInt) {
		return 0, fmt.Errorf("runtime: max instructions %d exceeds platform int", max)
	}
	return int(max), nil
}

func programParallelism(value int) (int, error) {
	if value < 0 {
		return 0, fmt.Errorf("load program: negative parallelism %d", value)
	}
	if value == 0 {
		return runtime.GOMAXPROCS(0), nil
	}
	return value, nil
}

func loadProgramGraph(ctx context.Context, loader ModuleLoader, entrypoints []programEntrypoint, parallelism int, artifacts *sourceArtifactStore) (moduleGraph, error) {
	if parallelism <= 1 || len(entrypoints) <= 1 {
		return loadProgramGraphSequential(ctx, loader, entrypoints, artifacts)
	}
	return loadProgramGraphParallel(ctx, loader, entrypoints, parallelism, artifacts)
}

func loadProgramGraphSequential(ctx context.Context, loader ModuleLoader, entrypoints []programEntrypoint, artifacts *sourceArtifactStore) (moduleGraph, error) {
	resolver := newProgramModuleResolver(ctx, loader)
	combined := moduleGraph{Nodes: make(map[moduleKey]moduleGraphNode)}
	for i, entrypoint := range entrypoints {
		if err := ctx.Err(); err != nil {
			return moduleGraph{}, err
		}
		graph, err := buildModuleGraphWithStore(resolver, entrypoint.key, artifacts)
		if err != nil {
			return moduleGraph{}, err
		}
		mergeProgramGraph(&combined, graph, i == 0)
	}
	return combined, nil
}

type programGraphResult struct {
	graph moduleGraph
	err   error
}

func loadProgramGraphParallel(ctx context.Context, loader ModuleLoader, entrypoints []programEntrypoint, parallelism int, artifacts *sourceArtifactStore) (moduleGraph, error) {
	resolver := newProgramModuleResolver(ctx, loader)
	jobs := make(chan int)
	results := make([]programGraphResult, len(entrypoints))
	workers := parallelism
	if workers > len(entrypoints) {
		workers = len(entrypoints)
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for index := range jobs {
				graph, err := buildProgramEntrypointGraph(ctx, resolver, entrypoints[index], artifacts)
				results[index] = programGraphResult{graph: graph, err: err}
			}
		}()
	}

	for index := range entrypoints {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return moduleGraph{}, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()

	combined := moduleGraph{Nodes: make(map[moduleKey]moduleGraphNode)}
	for index, result := range results {
		if result.err != nil {
			return moduleGraph{}, result.err
		}
		mergeProgramGraph(&combined, result.graph, index == 0)
	}
	return combined, nil
}

func buildProgramEntrypointGraph(ctx context.Context, resolver *programModuleResolver, entrypoint programEntrypoint, artifacts *sourceArtifactStore) (moduleGraph, error) {
	if err := ctx.Err(); err != nil {
		return moduleGraph{}, err
	}
	return buildModuleGraphWithStore(resolver, entrypoint.key, artifacts)
}

func mergeProgramGraph(combined *moduleGraph, graph moduleGraph, setRoot bool) {
	if setRoot {
		combined.Root = graph.Root
	}
	for key, node := range graph.Nodes {
		combined.Nodes[key] = node
	}
}

type programModuleResolver struct {
	ctx     context.Context
	loader  ModuleLoader
	mu      sync.Mutex
	sources map[moduleKey]resolvedModuleSource
	loading map[moduleKey]*programModuleLoad
}

type programModuleLoad struct {
	done   chan struct{}
	source resolvedModuleSource
	err    error
}

func newProgramModuleResolver(ctx context.Context, loader ModuleLoader) *programModuleResolver {
	return &programModuleResolver{
		ctx:     ctx,
		loader:  loader,
		sources: make(map[moduleKey]resolvedModuleSource),
		loading: make(map[moduleKey]*programModuleLoad),
	}
}

func (r *programModuleResolver) Resolve(from moduleKey, request string) (resolvedModuleSource, error) {
	key, err := normalizeRequireKey(from, request)
	if err != nil {
		return resolvedModuleSource{}, err
	}
	return r.Source(key)
}

func (r *programModuleResolver) Source(key moduleKey) (resolvedModuleSource, error) {
	r.mu.Lock()
	if source, ok := r.sources[key]; ok {
		r.mu.Unlock()
		return source, nil
	}
	if load, ok := r.loading[key]; ok {
		r.mu.Unlock()
		select {
		case <-load.done:
			return load.source, load.err
		case <-r.ctx.Done():
			return resolvedModuleSource{}, r.ctx.Err()
		}
	}
	load := &programModuleLoad{done: make(chan struct{})}
	r.loading[key] = load
	r.mu.Unlock()

	source, err := r.loadSource(key)

	r.mu.Lock()
	delete(r.loading, key)
	if err == nil {
		r.sources[key] = source
	}
	load.source = source
	load.err = err
	close(load.done)
	r.mu.Unlock()

	return source, err
}

func (r *programModuleResolver) loadSource(key moduleKey) (resolvedModuleSource, error) {
	if err := r.ctx.Err(); err != nil {
		return resolvedModuleSource{}, err
	}
	id := moduleIDFromKey(key)
	source, err := r.loader.LoadModule(r.ctx, id)
	if err != nil {
		return resolvedModuleSource{}, fmt.Errorf("load program: load %s: %w", id.String(), err)
	}
	if source.Name == "" {
		source.Name = id.String()
	}
	resolved := resolvedModuleSource{
		Key:      key,
		Source:   source,
		Identity: identifyModuleSource(source),
	}
	return resolved, nil
}

func validateProgramEntrypoints(entrypoints []Entrypoint) ([]programEntrypoint, LoadReport, error) {
	seen := make(map[string]bool, len(entrypoints))
	validated := make([]programEntrypoint, 0, len(entrypoints))
	report := LoadReport{
		Entrypoints: make([]EntrypointReport, 0, len(entrypoints)),
	}
	for _, entrypoint := range entrypoints {
		if entrypoint.Name == "" {
			return nil, report, fmt.Errorf("load program: empty entrypoint name")
		}
		if seen[entrypoint.Name] {
			return nil, report, fmt.Errorf("load program: duplicate entrypoint %q", entrypoint.Name)
		}
		seen[entrypoint.Name] = true
		key, err := moduleKeyFromID(entrypoint.Module)
		if err != nil {
			return nil, report, fmt.Errorf("load program: entrypoint %q: %w", entrypoint.Name, err)
		}
		normalized := moduleIDFromKey(key)
		validated = append(validated, programEntrypoint{
			name: entrypoint.Name,
			key:  key,
		})
		report.Entrypoints = append(report.Entrypoints, EntrypointReport{
			Name:   entrypoint.Name,
			Module: normalized,
		})
	}
	return validated, report, nil
}

func moduleKeyFromID(id ModuleID) (moduleKey, error) {
	switch id.kind {
	case ModuleLogical:
		return logicalModuleKey(id.path)
	case ModuleHost:
		return hostModuleKey(id.path)
	default:
		return moduleKey{}, fmt.Errorf("module id has unknown kind %q", id.kind)
	}
}

func moduleIDFromKey(key moduleKey) ModuleID {
	switch key.kind {
	case moduleKeyHost:
		return ModuleID{kind: ModuleHost, path: key.path}
	default:
		return ModuleID{kind: ModuleLogical, path: key.path}
	}
}

func compileProgramModules(ctx context.Context, graph moduleGraph, cache *sourceArtifactStore, parallelism int) (map[moduleKey]*Proto, error) {
	if parallelism > 1 && len(graph.Nodes) > 1 {
		return compileProgramModulesParallel(ctx, graph, cache, parallelism)
	}
	protos := make(map[moduleKey]*Proto, len(graph.Nodes))
	for _, key := range sortedModuleKeys(graph.Nodes) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		node := graph.Nodes[key]
		proto, err := cache.compile(node.Source, node.Identity)
		if err != nil {
			return nil, fmt.Errorf("load program: compile %s: %w", key.String(), err)
		}
		protos[key] = proto
	}
	return protos, nil
}

func compileProgramModulesParallel(ctx context.Context, graph moduleGraph, cache *sourceArtifactStore, parallelism int) (map[moduleKey]*Proto, error) {
	jobs := uniqueProgramArtifactJobs(graph)
	results := make([]programCompileArtifact, len(jobs))
	workers := boundedProgramWorkers(parallelism, len(jobs))
	indexes := make(chan int)

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for index := range indexes {
				job := jobs[index]
				if err := ctx.Err(); err != nil {
					results[index].err = err
					continue
				}
				proto, err := cache.compile(job.source, job.identity)
				if err != nil {
					results[index].err = err
					continue
				}
				results[index] = programCompileArtifact{
					proto: proto,
				}
			}
		}()
	}

	if err := sendProgramArtifactJobs(ctx, indexes, len(jobs)); err != nil {
		wg.Wait()
		return nil, err
	}
	wg.Wait()

	protos := make(map[moduleKey]*Proto, len(graph.Nodes))
	for index, result := range results {
		job := jobs[index]
		if result.err != nil {
			if result.err == context.Canceled || result.err == context.DeadlineExceeded {
				return nil, result.err
			}
			return nil, fmt.Errorf("load program: compile %s: %w", job.key.String(), result.err)
		}
		for _, key := range job.keys {
			protos[key] = result.proto
		}
	}
	return protos, nil
}

type programCheckReport struct {
	diagnostics []Diagnostic
	summaries   map[moduleKey]moduleSummaryArtifact
}

func checkProgramModules(ctx context.Context, graph moduleGraph, cache *sourceArtifactStore, parallelism int) (programCheckReport, error) {
	if parallelism > 1 && len(graph.Nodes) > 1 {
		return checkProgramModulesParallel(ctx, graph, cache, parallelism)
	}
	var diagnostics []programDiagnostic
	results := make(map[moduleKey]CheckResult, len(graph.Nodes))
	for _, key := range sortedModuleKeys(graph.Nodes) {
		if err := ctx.Err(); err != nil {
			return programCheckReport{}, err
		}
		node := graph.Nodes[key]
		artifact, err := cache.check(node.Source, node.Identity)
		if err != nil {
			return programCheckReport{}, fmt.Errorf("load program: check %s: %w", key.String(), err)
		}
		results[key] = artifact.result
		for _, diagnostic := range artifact.result.Diagnostics {
			diagnostics = append(diagnostics, programDiagnostic{
				module:     key,
				diagnostic: diagnostic,
			})
		}
	}
	return programCheckReport{
		diagnostics: sortedProgramDiagnostics(diagnostics),
		summaries:   programModuleSummaries(graph, results),
	}, nil
}

func checkProgramModulesParallel(ctx context.Context, graph moduleGraph, cache *sourceArtifactStore, parallelism int) (programCheckReport, error) {
	jobs := uniqueProgramArtifactJobs(graph)
	results := make([]programCheckArtifact, len(jobs))
	workers := boundedProgramWorkers(parallelism, len(jobs))
	indexes := make(chan int)

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for index := range indexes {
				job := jobs[index]
				if err := ctx.Err(); err != nil {
					results[index].err = err
					continue
				}
				artifact, err := cache.check(job.source, job.identity)
				if err != nil {
					results[index].err = err
					continue
				}
				results[index] = programCheckArtifact{
					artifact: artifact,
				}
			}
		}()
	}

	if err := sendProgramArtifactJobs(ctx, indexes, len(jobs)); err != nil {
		wg.Wait()
		return programCheckReport{}, err
	}
	wg.Wait()

	var diagnostics []programDiagnostic
	checks := make(map[moduleKey]CheckResult, len(graph.Nodes))
	for index, result := range results {
		job := jobs[index]
		if result.err != nil {
			if result.err == context.Canceled || result.err == context.DeadlineExceeded {
				return programCheckReport{}, result.err
			}
			return programCheckReport{}, fmt.Errorf("load program: check %s: %w", job.key.String(), result.err)
		}
		for _, key := range job.keys {
			checks[key] = result.artifact.result
			for _, diagnostic := range result.artifact.result.Diagnostics {
				diagnostics = append(diagnostics, programDiagnostic{
					module:     key,
					diagnostic: diagnostic,
				})
			}
		}
	}
	return programCheckReport{
		diagnostics: sortedProgramDiagnostics(diagnostics),
		summaries:   programModuleSummaries(graph, checks),
	}, nil
}

func programModuleSummaries(graph moduleGraph, results map[moduleKey]CheckResult) map[moduleKey]moduleSummaryArtifact {
	summaries := make(map[moduleKey]moduleSummaryArtifact, len(results))
	for key, result := range results {
		summary := result.Summary
		if node, ok := graph.Nodes[key]; ok {
			summary.Dependencies = moduleDependencySummaries(graph, node.Requires)
		}
		summaries[key] = moduleSummaryArtifact{
			Summary: summary,
			Trusted: len(result.Diagnostics) == 0,
		}
	}
	for key, node := range graph.Nodes {
		artifact := summaries[key]
		artifact.Summary = enrichModuleSummaryFromRequireBindings(artifact.Summary, node, summaries)
		summaries[key] = artifact
	}
	return summaries
}

type programDiagnostic struct {
	module     moduleKey
	diagnostic Diagnostic
}

type programArtifactJob struct {
	key      moduleKey
	keys     []moduleKey
	source   Source
	identity sourceIdentity
}

type programCompileArtifact struct {
	proto *Proto
	err   error
}

type programCheckArtifact struct {
	artifact checkArtifact
	err      error
}

func uniqueProgramArtifactJobs(graph moduleGraph) []programArtifactJob {
	var jobs []programArtifactJob
	indexes := make(map[sourceIdentity]int, len(graph.Nodes))
	for _, key := range sortedModuleKeys(graph.Nodes) {
		node := graph.Nodes[key]
		if index, ok := indexes[node.Identity]; ok {
			jobs[index].keys = append(jobs[index].keys, key)
			continue
		}
		indexes[node.Identity] = len(jobs)
		jobs = append(jobs, programArtifactJob{
			key:      key,
			keys:     []moduleKey{key},
			source:   node.Source,
			identity: node.Identity,
		})
	}
	return jobs
}

func boundedProgramWorkers(parallelism int, jobs int) int {
	if parallelism < 1 {
		parallelism = 1
	}
	if jobs < parallelism {
		return jobs
	}
	return parallelism
}

func sendProgramArtifactJobs(ctx context.Context, indexes chan<- int, count int) error {
	defer close(indexes)
	for index := 0; index < count; index++ {
		select {
		case indexes <- index:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func sortedProgramDiagnostics(diagnostics []programDiagnostic) []Diagnostic {
	sort.SliceStable(diagnostics, func(i, j int) bool {
		left := diagnostics[i]
		right := diagnostics[j]
		if left.module != right.module {
			return left.module.String() < right.module.String()
		}
		if left.diagnostic.Start != right.diagnostic.Start {
			return left.diagnostic.Start < right.diagnostic.Start
		}
		if left.diagnostic.End != right.diagnostic.End {
			return left.diagnostic.End < right.diagnostic.End
		}
		return left.diagnostic.Code < right.diagnostic.Code
	})
	result := make([]Diagnostic, 0, len(diagnostics))
	for _, item := range diagnostics {
		result = append(result, item.diagnostic)
	}
	return result
}

func moduleReports(graph moduleGraph, summaries map[moduleKey]moduleSummaryArtifact) []ModuleReport {
	keys := sortedModuleKeys(graph.Nodes)
	reports := make([]ModuleReport, 0, len(keys))
	for _, key := range keys {
		report := ModuleReport{
			Module:     moduleIDFromKey(key),
			SourceName: graph.Nodes[key].Source.Name,
		}
		if summary, ok := summaries[key]; ok {
			report.Summary = summary.Summary
		}
		reports = append(reports, report)
	}
	return reports
}

func sortedModuleKeys(nodes map[moduleKey]moduleGraphNode) []moduleKey {
	keys := make([]moduleKey, 0, len(nodes))
	for key := range nodes {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].String() < keys[j].String()
	})
	return keys
}

func diagnosticFromModuleDiagnostic(diagnostic moduleDiagnostic) Diagnostic {
	return Diagnostic{
		Code:    diagnostic.Code,
		Message: diagnostic.Message,
		Path:    append([]string(nil), diagnostic.Path...),
	}
}
