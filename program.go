package ember

import (
	"context"
	"errors"
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
	Limits      ProgramLimits
	Analysis    AnalysisConfig
}

// ProgramLimits bounds module graph discovery and compilation. Zero means
// unlimited for backward compatibility.
type ProgramLimits struct {
	// MaxModules bounds unique module identities accepted during discovery.
	MaxModules uint64
	// MaxTotalSourceBytes bounds aggregate loaded source text.
	MaxTotalSourceBytes uint64
	// Compile applies source parsing limits to every module.
	Compile CompileLimits
}

// Program is an immutable compiled module graph.
type Program struct {
	entrypoints []programEntrypoint
	graph       moduleGraph
	protos      map[moduleKey]*Proto

	programImageOnce sync.Once
	programImage     *programImage
	programImageErr  error
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
	Host   RuntimeHost
	Limits ExecutionLimits

	// MaxInstructions is the legacy instruction limit. Use Limits instead.
	// If both fields are nonzero, they must be equal.
	//
	// Deprecated: use Limits.MaxInstructions.
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
	suspensionMu    sync.Mutex
	execution       runtimeExecution
	owner           *runtimeOwner
	program         *Program
	host            RuntimeHost
	entrypoints     map[moduleKey]Value
	loaded          map[moduleKey]Value
	requireAdapters map[moduleKey]Value
	active          map[moduleKey]bool
	limits          ExecutionLimits
	stack           []moduleKey
	closed          bool
	suspensions     map[*suspensionState]struct{}
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

	combined, err := loadProgramGraph(ctx, loader, entrypoints, parallelism, artifacts, options.Limits)
	if err != nil {
		if cycle, ok := err.(moduleCycleError); ok {
			report.Diagnostics = []Diagnostic{diagnosticFromModuleDiagnostic(cycle.Diagnostic())}
			return nil, report, nil
		}
		return nil, report, err
	}

	protos, err := compileProgramModules(ctx, combined, artifacts, parallelism, options.Limits.Compile)
	if err != nil {
		return nil, report, err
	}
	var summaries map[moduleKey]moduleSummaryArtifact
	if options.Check {
		checkReport, err := checkProgramModules(ctx, combined, artifacts, parallelism, options.Limits.Compile, options.Analysis)
		if err != nil {
			return nil, report, err
		}
		report.Diagnostics = checkReport.diagnostics
		summaries = checkReport.summaries
	}
	report.Modules = moduleReports(combined, summaries)

	program := &Program{
		entrypoints: entrypoints,
		graph:       combined,
		protos:      protos,
	}
	if _, err := program.preparedProgramImage(); err != nil {
		return nil, report, fmt.Errorf("load program: %w", err)
	}
	return program, report, nil
}

// NewRuntime creates mutable execution state for p. It does not execute script
// code until RunHook is called.
func (p *Program) NewRuntime(options RuntimeOptions) (*Runtime, error) {
	if p == nil {
		return nil, fmt.Errorf("runtime: nil program")
	}
	limits, err := normalizeExecutionLimits(options.MaxInstructions, options.Limits)
	if err != nil {
		return nil, err
	}
	if err := validateExecutionLimits(limits); err != nil {
		return nil, err
	}
	execution, err := selectRuntimeExecution(p)
	if err != nil {
		return nil, err
	}
	runtime := &Runtime{
		execution: execution,
		program:   p,
		host:      options.Host,
		limits:    limits,
	}
	if err := execution.initialize(runtime); err != nil {
		return nil, err
	}
	return runtime, nil
}

// RunHook loads entrypoints lazily and calls hook in entrypoint order.
func (r *Runtime) RunHook(ctx context.Context, hook string, args ...Value) (HookReport, error) {
	report := HookReport{Hook: hook}
	err := r.runHook(ctx, hook, args, &report)
	return report, err
}

// RunHookResumable runs a hook until completion or host suspension.
func (r *Runtime) RunHookResumable(ctx context.Context, hook string, args ...Value) (ExecutionResult, error) {
	execution, err := r.executionAdapter()
	if err != nil {
		return ExecutionResult{}, err
	}
	outcome, err := execution.runHookResumable(r, ctx, hook, args)
	if err != nil {
		return ExecutionResult{}, err
	}
	return r.executionResult(outcome), nil
}

// runHook executes one hook invocation. A nil report keeps the same execution
// and error semantics while allowing private callers that only need success
// or failure to discard per-entrypoint outcomes.
func (r *Runtime) runHook(ctx context.Context, hook string, args []Value, report *HookReport) error {
	execution, err := r.executionAdapter()
	if err != nil {
		return err
	}
	return execution.runHook(r, ctx, hook, args, report)
}

func (r *Runtime) runVMHook(ctx context.Context, hook string, args []Value, report *HookReport) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil {
		return fmt.Errorf("runtime: nil runtime")
	}
	lease, err := r.beginRun()
	if err == errRuntimeOwnerClosed {
		return fmt.Errorf("runtime: closed")
	}
	if err == errRuntimeOwnerBusy {
		return fmt.Errorf("runtime: begin run: %w", ErrRuntimeBusy)
	}
	if err != nil {
		return fmt.Errorf("runtime: begin run: %w", err)
	}
	defer lease.end()
	if hook == "" {
		return fmt.Errorf("runtime: empty hook")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	controller, err := newExecutionPolicy(ctx, r.limits)
	if err != nil {
		return fmt.Errorf("runtime: create execution controller: %w", err)
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
			return fmt.Errorf("runtime: host globals for %s load: %w", entrypoint.name, err)
		}
		export, loaded, err := r.loadEntrypoint(ctx, entrypoint, loadGlobals, controller)
		if err != nil {
			return fmt.Errorf("runtime: load entrypoint %s: %w", entrypoint.name, err)
		}
		call.Loaded = loaded

		hookGlobals, err := r.hostGlobals(ctx, HostCall{
			Entrypoint: entrypoint.name,
			Module:     moduleIDFromKey(entrypoint.key),
			Hook:       hook,
		})
		if err != nil {
			return fmt.Errorf("runtime: host globals for %s.%s: %w", entrypoint.name, hook, err)
		}

		table, ok := export.Table()
		if export.IsNil() {
			call.Skipped = true
			appendHookCallReport(report, call)
			continue
		}
		if !ok {
			return fmt.Errorf("runtime: entrypoint %s returned %s, want table or nil", entrypoint.name, export.Kind())
		}
		hookEnv := globalEnv{host: hookGlobals, owner: r.owner}
		if len(hookGlobals) != 0 {
			hookEnv.version = 1
		}
		hookValue, err := runtimeTableAccess(&hookEnv).get(table, StringValue(hook))
		if err != nil {
			return fmt.Errorf("runtime: get hook %s.%s: %w", entrypoint.name, hook, err)
		}
		if hookValue.IsNil() {
			call.Skipped = true
			appendHookCallReport(report, call)
			continue
		}
		if !callableValue(hookValue) {
			return fmt.Errorf("runtime: hook %s.%s is %s, want function", entrypoint.name, hook, hookValue.Kind())
		}
		callContext := r.newInvocationScope(ctx, entrypoint.key, hookGlobals, controller)
		var callErr error
		if closure, ok := hookValue.scriptFunction(); ok {
			_, callErr = executeProtoWithInvocationScope(ctx, closure.proto, callContext, executeOptions{
				args:           args,
				upvalues:       closure.upvalues,
				upvalueValues:  closure.upvalueValues,
				upvalueValueOK: closure.upvalueValueOK,
				controller:     controller,
			})
		} else {
			_, callErr = callValueWithContextController(ctx, hookValue, callContext.envWithRequire(), args, controller)
		}
		if callErr != nil {
			return fmt.Errorf("runtime: call hook %s.%s: %w", entrypoint.name, hook, callErr)
		}
		call.Called = true
		appendHookCallReport(report, call)
	}
	return nil
}

func appendHookCallReport(report *HookReport, call HookCallReport) {
	if report != nil {
		report.Calls = append(report.Calls, call)
	}
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
	r.closeSuspensions()
	// Preserve the public zero-value contract without silently assigning the
	// old VM to a Runtime that has lost or never received execution ownership.
	r.closeMu.Lock()
	if r.execution == nil {
		r.closed = true
		r.closeMu.Unlock()
		return nil
	}
	r.closeMu.Unlock()
	execution, err := r.executionAdapter()
	if err != nil {
		return err
	}
	return execution.close(r)
}

func (r *Runtime) closeVM() error {
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
	r.requireAdapters = nil
	r.active = nil
	r.stack = nil
	return nil
}

// collect reclaims unreachable handle-heap entries at an idle runtime
// boundary. It stays private until the allocation policy has enough profile
// evidence for a stable public control surface.
func (r *Runtime) collect() (runtimeHeapStats, error) {
	if r == nil {
		return runtimeHeapStats{}, errRuntimeOwnerReleased
	}
	r.closeMu.Lock()
	defer r.closeMu.Unlock()
	if r.closed {
		return runtimeHeapStats{}, errRuntimeOwnerClosed
	}
	if r.owner == nil {
		return runtimeHeapStats{}, errRuntimeOwnerInvalid
	}
	return r.owner.collect(func(collector *runtimeHeapCollector) {
		for _, value := range r.entrypoints {
			collector.scanValue(value)
		}
		for _, value := range r.loaded {
			collector.scanValue(value)
		}
		for _, value := range r.requireAdapters {
			collector.scanValue(value)
		}
		if r.program != nil {
			for _, proto := range r.program.protos {
				collector.scanProto(proto)
			}
		}
	})
}

func (r *Runtime) loadEntrypoint(ctx context.Context, entrypoint programEntrypoint, globals map[string]Value, controller *executionController) (Value, bool, error) {
	if value, ok := r.entrypoints[entrypoint.key]; ok {
		return value, false, nil
	}
	if err := ctx.Err(); err != nil {
		return NilValue(), false, err
	}
	results, err := r.runModuleWithContextGlobalsController(ctx, entrypoint.key, globals, controller, nil)
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

func programParallelism(value int) (int, error) {
	if value < 0 {
		return 0, fmt.Errorf("load program: negative parallelism %d", value)
	}
	if value == 0 {
		return runtime.GOMAXPROCS(0), nil
	}
	return value, nil
}

func loadProgramGraph(ctx context.Context, loader ModuleLoader, entrypoints []programEntrypoint, parallelism int, artifacts *sourceArtifactStore, limits ProgramLimits) (moduleGraph, error) {
	if parallelism <= 1 || len(entrypoints) <= 1 {
		return loadProgramGraphSequential(ctx, loader, entrypoints, artifacts, limits)
	}
	return loadProgramGraphParallel(ctx, loader, entrypoints, parallelism, artifacts, limits)
}

func loadProgramGraphSequential(ctx context.Context, loader ModuleLoader, entrypoints []programEntrypoint, artifacts *sourceArtifactStore, limits ProgramLimits) (moduleGraph, error) {
	resolver := newProgramModuleResolverWithLimits(ctx, loader, limits)
	combined := moduleGraph{Nodes: make(map[moduleKey]moduleGraphNode)}
	for i, entrypoint := range entrypoints {
		if err := ctx.Err(); err != nil {
			return moduleGraph{}, err
		}
		graph, err := buildModuleGraphWithStoreAndLimits(resolver, entrypoint.key, artifacts, limits.Compile)
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

func loadProgramGraphParallel(ctx context.Context, loader ModuleLoader, entrypoints []programEntrypoint, parallelism int, artifacts *sourceArtifactStore, limits ProgramLimits) (moduleGraph, error) {
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	resolver := newProgramModuleResolverWithLimits(workCtx, loader, limits)
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
				graph, err := buildProgramEntrypointGraph(workCtx, resolver, entrypoints[index], artifacts)
				results[index] = programGraphResult{graph: graph, err: err}
				if errors.Is(err, ErrLimitExceeded) {
					cancel()
				}
			}
		}()
	}

	sendAborted := false
	for index := range entrypoints {
		select {
		case jobs <- index:
		case <-workCtx.Done():
			sendAborted = true
		}
		if sendAborted {
			break
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return moduleGraph{}, err
	}

	combined := moduleGraph{Nodes: make(map[moduleKey]moduleGraphNode)}
	for _, result := range results {
		if errors.Is(result.err, ErrLimitExceeded) {
			return moduleGraph{}, result.err
		}
	}
	for index, result := range results {
		if result.err != nil {
			return moduleGraph{}, result.err
		}
		mergeProgramGraph(&combined, result.graph, index == 0)
	}
	if sendAborted {
		return moduleGraph{}, workCtx.Err()
	}
	return combined, nil
}

func buildProgramEntrypointGraph(ctx context.Context, resolver *programModuleResolver, entrypoint programEntrypoint, artifacts *sourceArtifactStore) (moduleGraph, error) {
	if err := ctx.Err(); err != nil {
		return moduleGraph{}, err
	}
	return buildModuleGraphWithStoreAndLimits(resolver, entrypoint.key, artifacts, resolver.limits.Compile)
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
	ctx         context.Context
	loader      ModuleLoader
	limits      ProgramLimits
	mu          sync.Mutex
	sources     map[moduleKey]resolvedModuleSource
	loading     map[moduleKey]*programModuleLoad
	moduleCount uint64
	sourceBytes uint64
}

type programModuleLoad struct {
	done   chan struct{}
	source resolvedModuleSource
	err    error
}

func newProgramModuleResolver(ctx context.Context, loader ModuleLoader) *programModuleResolver {
	return newProgramModuleResolverWithLimits(ctx, loader, ProgramLimits{})
}

func newProgramModuleResolverWithLimits(ctx context.Context, loader ModuleLoader, configured ProgramLimits) *programModuleResolver {
	return &programModuleResolver{
		ctx:     ctx,
		loader:  loader,
		limits:  configured,
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
	if r.limits.MaxModules != 0 && r.moduleCount >= r.limits.MaxModules {
		used := r.moduleCount + 1
		r.mu.Unlock()
		return resolvedModuleSource{}, &LimitError{Kind: LimitModules, Limit: r.limits.MaxModules, Used: used}
	}
	r.moduleCount++
	load := &programModuleLoad{done: make(chan struct{})}
	r.loading[key] = load
	r.mu.Unlock()

	source, err := r.loadSource(key)

	r.mu.Lock()
	delete(r.loading, key)
	if err != nil {
		r.moduleCount--
	} else {
		used := r.sourceBytes
		if sourceBytes := uint64(len(source.Source.Text)); sourceBytes > ^uint64(0)-used {
			used = ^uint64(0)
		} else {
			used += sourceBytes
		}
		if r.limits.MaxTotalSourceBytes != 0 && used > r.limits.MaxTotalSourceBytes {
			err = &LimitError{Kind: LimitTotalSourceBytes, Limit: r.limits.MaxTotalSourceBytes, Used: used}
			r.moduleCount--
		} else {
			r.sourceBytes = used
			r.sources[key] = source
		}
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

func compileProgramModules(ctx context.Context, graph moduleGraph, cache *sourceArtifactStore, parallelism int, compileLimits CompileLimits) (map[moduleKey]*Proto, error) {
	if parallelism > 1 && len(graph.Nodes) > 1 {
		return compileProgramModulesParallel(ctx, graph, cache, parallelism, compileLimits)
	}
	protos := make(map[moduleKey]*Proto, len(graph.Nodes))
	for _, key := range sortedModuleKeys(graph.Nodes) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		node := graph.Nodes[key]
		proto, err := cache.compileWithLimits(node.Source, node.Identity, compileLimits)
		if err != nil {
			return nil, fmt.Errorf("load program: compile %s: %w", key.String(), err)
		}
		protos[key] = proto
	}
	return protos, nil
}

func compileProgramModulesParallel(ctx context.Context, graph moduleGraph, cache *sourceArtifactStore, parallelism int, compileLimits CompileLimits) (map[moduleKey]*Proto, error) {
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
				proto, err := cache.compileWithLimits(job.source, job.identity, compileLimits)
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

func checkProgramModules(
	ctx context.Context,
	graph moduleGraph,
	cache *sourceArtifactStore,
	parallelism int,
	compileLimits CompileLimits,
	config AnalysisConfig,
) (programCheckReport, error) {
	config = cloneAnalysisConfig(config)
	summaries := make(map[moduleKey]moduleSummaryArtifact, len(graph.Nodes))
	var diagnostics []programDiagnostic
	for _, level := range programDependencyLevels(graph) {
		results, err := checkProgramLevel(ctx, graph, cache, level, parallelism, compileLimits, config, summaries)
		if err != nil {
			return programCheckReport{}, err
		}
		for _, key := range level {
			result := results[key]
			node := graph.Nodes[key]
			summary := result.Summary
			summary.Dependencies = moduleDependencySummaries(graph, node.Requires)
			artifact := moduleSummaryArtifact{
				Summary: summary,
				Trusted: len(result.Diagnostics) == 0,
			}
			if artifact.Trusted {
				artifact.Summary = enrichModuleSummaryFromRequireBindings(artifact.Summary, node, summaries)
			}
			summaries[key] = artifact
			for _, diagnostic := range result.Diagnostics {
				diagnostic.Module = moduleIDFromKey(key)
				if diagnostic.SourceName == "" {
					diagnostic.SourceName = node.Source.Name
				}
				diagnostics = append(diagnostics, programDiagnostic{
					module:     key,
					diagnostic: diagnostic,
				})
			}
		}
	}
	return programCheckReport{
		diagnostics: sortedProgramDiagnostics(diagnostics),
		summaries:   summaries,
	}, nil
}

func checkProgramLevel(
	ctx context.Context,
	graph moduleGraph,
	cache *sourceArtifactStore,
	level []moduleKey,
	parallelism int,
	compileLimits CompileLimits,
	config AnalysisConfig,
	checked map[moduleKey]moduleSummaryArtifact,
) (map[moduleKey]CheckResult, error) {
	results := make([]programCheckArtifact, len(level))
	workers := boundedProgramWorkers(parallelism, len(level))
	if workers == 0 {
		return map[moduleKey]CheckResult{}, nil
	}
	indexes := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for index := range indexes {
				key := level[index]
				if err := ctx.Err(); err != nil {
					results[index].err = err
					continue
				}
				node := graph.Nodes[key]
				moduleSummaries := analysisModuleSummaries(config.ModuleSummaries, node.Requires, checked)
				artifact, err := cache.checkWithEnvWithLimits(
					node.Source,
					node.Identity,
					compileLimits,
					typeEnvFromAnalysisConfig(config),
					moduleSummaryEnvFromMap(moduleSummaries),
				)
				results[index] = programCheckArtifact{artifact: artifact, err: err}
			}
		}()
	}
	if err := sendProgramArtifactJobs(ctx, indexes, len(level)); err != nil {
		wg.Wait()
		return nil, err
	}
	wg.Wait()

	checks := make(map[moduleKey]CheckResult, len(level))
	for index, result := range results {
		key := level[index]
		if result.err != nil {
			if result.err == context.Canceled || result.err == context.DeadlineExceeded {
				return nil, result.err
			}
			return nil, fmt.Errorf("load program: check %s: %w", key.String(), result.err)
		}
		checks[key] = result.artifact.result
	}
	return checks, nil
}

func analysisModuleSummaries(
	configured map[string]ModuleSummary,
	requires []moduleKey,
	checked map[moduleKey]moduleSummaryArtifact,
) map[string]ModuleSummary {
	summaries := cloneModuleSummaryMap(configured)
	if summaries == nil {
		summaries = make(map[string]ModuleSummary)
	}
	for _, required := range requires {
		artifact, ok := checked[required]
		if !ok || !artifact.Trusted {
			summary := ModuleSummary{SourceName: required.String()}
			summaries[required.String()] = summary
			summaries[required.path] = summary
			continue
		}
		summary := cloneModuleSummary(artifact.Summary)
		summaries[required.String()] = summary
		summaries[required.path] = summary
		if summary.SourceName != "" {
			summaries[summary.SourceName] = summary
		}
	}
	return summaries
}

func programDependencyLevels(graph moduleGraph) [][]moduleKey {
	remaining := make(map[moduleKey]int, len(graph.Nodes))
	dependents := make(map[moduleKey][]moduleKey, len(graph.Nodes))
	for key, node := range graph.Nodes {
		seen := make(map[moduleKey]struct{}, len(node.Requires))
		for _, required := range node.Requires {
			if _, ok := graph.Nodes[required]; !ok {
				continue
			}
			if _, duplicate := seen[required]; duplicate {
				continue
			}
			seen[required] = struct{}{}
			remaining[key]++
			dependents[required] = append(dependents[required], key)
		}
	}
	var ready []moduleKey
	for key := range graph.Nodes {
		if remaining[key] == 0 {
			ready = append(ready, key)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].String() < ready[j].String() })
	levels := make([][]moduleKey, 0)
	for len(ready) != 0 {
		level := append([]moduleKey(nil), ready...)
		levels = append(levels, level)
		next := make([]moduleKey, 0)
		for _, key := range level {
			for _, dependent := range dependents[key] {
				remaining[dependent]--
				if remaining[dependent] == 0 {
					next = append(next, dependent)
				}
			}
		}
		sort.Slice(next, func(i, j int) bool { return next[i].String() < next[j].String() })
		ready = next
	}
	return levels
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
