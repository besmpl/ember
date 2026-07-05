# Plan: Hearth Runtime Integration Core

## Goal

Make Ember embeddable in Hearth through one deep runtime module.

The slice is done when a Hearth-side adapter can load several authored Luau
scripts plus shared ModuleScripts, run `startup` and `update` hooks against
caller-owned host state, share `require` results across scripts, enforce an
execution budget, and observe a report without knowing Ember parser, bytecode,
VM frame, global-env, or module-cache internals.

## Design Vocabulary

This plan uses the codebase-design vocabulary:

- **Program module**: immutable compiled source graph behind a small public
  interface.
- **Runtime module**: mutable execution state for one loaded program.
- **Host adapter seam**: the place where Hearth supplies globals, callbacks,
  source loading, and world-owned behavior.
- **Adapter**: Hearth's implementation of the host adapter seam. Ember keeps no
  Hearth imports.

The deletion test: if the Program and Runtime modules disappeared, Hearth would
need to reimplement module discovery, `require` normalization, source identity,
compile caching, hook loading, hook ordering, active-loading cycle checks,
instruction budgets, context propagation, and reports. That complexity belongs
behind Ember's seam.

## Current Pressure

Ember already has the raw implementation material:

- `Compile(source string) (*Proto, error)` compiles one source.
- `RunWithGlobals(proto *Proto, globals map[string]Value) ([]Value, error)`
  runs one prototype with explicit globals.
- `module_resolver.go` has private module keys, source identity, graph walking,
  artifact caching, typed summaries, and cycle detection.
- `module_runtime.go` has private lazy `require`, active-loading detection, and
  cached module return values.
- `vm.go` has internal thread state, coroutine machinery, and instruction budget
  plumbing, but no public budget/context interface.

The missing depth is a public module that combines those pieces without exposing
them.

## Interface Shape

Recommended public surface, names still subject to normal Go naming pressure:

```go
type ModuleKind string

const (
	ModuleLogical ModuleKind = "logical"
	ModuleHost    ModuleKind = "host"
)

type ModuleID struct {
	// opaque comparable value; construct with LogicalModule or HostModule
}

func LogicalModule(path string) ModuleID
func HostModule(path string) ModuleID

type ModuleLoader interface {
	LoadModule(ctx context.Context, id ModuleID) (Source, error)
}

type Entrypoint struct {
	Name   string
	Module ModuleID
}

type ProgramOptions struct {
	Entrypoints []Entrypoint
	Check        bool
	Parallelism int
}

type Program struct {
	// immutable compiled graph
}

func LoadProgram(ctx context.Context, loader ModuleLoader, options ProgramOptions) (*Program, LoadReport, error)

type RuntimeOptions struct {
	Host RuntimeHost
	MaxInstructions uint64
}

type RuntimeHost interface {
	Globals(ctx context.Context, call HostCall) (map[string]Value, error)
}

type HostCall struct {
	Entrypoint string
	Module     ModuleID
	Hook       string
}

type Runtime struct {
	// mutable loaded entrypoints and module cache
}

func (p *Program) NewRuntime(options RuntimeOptions) (*Runtime, error)
func (r *Runtime) RunHook(ctx context.Context, hook string, args ...Value) (HookReport, error)
func (r *Runtime) Close() error
```

This is intentionally small. `Program` hides graph construction, source identity,
parallel source loading, parsing, checking, compiling, dependency summaries,
artifact cache ownership, deterministic report ordering, and proto storage.
`Runtime` hides entrypoint loading, lazy `require`, module result caching,
active-loading checks, per-call global assembly, VM thread creation, instruction
budgeting, and report construction.

## Interface Semantics

### Module IDs

`LogicalModule("ServerScriptService/Main")` and
`LogicalModule("ReplicatedStorage/Shared/Config")` are the normal source module
namespace. Relative `require("./x")` inside a logical module is normalized
against the requiring module's path.

`HostModule("clock")` is reserved for host-provided module facts or source. It
should not be used for Hearth services in the first slice; services should enter
through host globals such as `game` and `Instance`.

`ModuleID` should be comparable and printable, but callers should construct it
through helpers so Ember can keep normalization policy local.

### Program Loading

`LoadProgram`:

- validates options and entrypoint names;
- loads entrypoint and discovered module sources through `ModuleLoader`;
- parses literal `require("...")` edges reachable from entrypoints;
- builds one combined graph for all entrypoints;
- detects graph cycles before compiling;
- optionally checks each source and computes module summaries;
- compiles each source once by source identity;
- returns `LoadReport` diagnostics for recoverable graph/check findings;
- returns an error for loader, parse, compile, or invalid-option failures.

No script top-level code executes during `LoadProgram`. That keeps constructors
boring and makes program loading pure except for explicit source loading.

`ProgramOptions.Parallelism` controls bounded internal workers. Zero means an
Ember default such as `runtime.GOMAXPROCS(0)`. One forces sequential execution
for tests and debugging. Negative values are invalid. This option is a policy
knob, not an exposed worker-pool interface.

### Parallel Program Loading

Parallelism belongs inside the Program module because callers should not need to
know graph, parser, checker, compiler, or cache internals to get fast project
loads. The external interface remains `LoadProgram`; the implementation can
contain private internal seams for workers and cache tests.

The load pipeline should be phased:

1. **Graph frontier load**: start from sorted entrypoint modules. Load missing
   sources in bounded parallel batches through `ModuleLoader`.
2. **Parse and bind**: parse loaded sources in parallel and store source
   artifacts by source identity. Parse failures are gathered with module
   identity and returned in deterministic module order.
3. **Require-edge expansion**: collect literal `require` requests from parsed
   artifacts, normalize them against the requiring module, sort discovered
   module IDs, and repeat the frontier load until the graph closes.
4. **Cycle detection**: detect cycles on the closed graph before compile output
   is accepted. Cycle diagnostics must be deterministic and independent of
   worker completion order.
5. **Typed analysis**: if `ProgramOptions.Check` is true, check every parsed
   source in parallel, build `CheckResult` and `ModuleSummary` artifacts, then
   enrich dependency summaries after all checks finish.
6. **Compile**: compile every trusted source in parallel, using parsed artifacts
   from the cache. Compile failures are returned in deterministic module order.
7. **Assemble Program**: freeze the Program's module table, entrypoint table,
   dependency summaries, and protos. No goroutine survives `LoadProgram`.

The artifact cache must be owned by one `LoadProgram` call or guarded behind a
private cache module. A future incremental-reload slice can make the cache
caller-reusable, but the first public interface should not expose cache maps or
locks.

Reports must be sorted by stable keys: entrypoints in caller-declared order,
then modules by `ModuleID.String()`, then diagnostics by source range and code.
Worker completion order must never affect report order, graph order, require
order, or hook order.

Context cancellation should stop launching new worker jobs and let in-flight
jobs return promptly at phase boundaries. `ModuleLoader.LoadModule` receives the
same context so filesystem or authored-place adapters can stop their own work.
Compiler and checker phases should check `ctx.Err()` between modules; finer
internal cancellation can be added later without changing the interface.

### Runtime Creation

`Program.NewRuntime` creates mutable execution state for one owner. It does not
touch Hearth worlds, start goroutines, open files, or run scripts.

A `Program` can create multiple runtimes. This lets Hearth create isolated test
worlds or reload a place without recompiling unchanged source.

### Entrypoint Loading

Entrypoints are loaded lazily on first `RunHook`. Loading an entrypoint means
executing its module once with globals from `RuntimeHost.Globals`. The first
result is the entrypoint export.

Initial supported script shape:

```lua
local state = {}

return {
	startup = function(ctx)
		state.ready = true
	end,
	update = function(ctx)
		state.frames = (state.frames or 0) + 1
	end,
}
```

If an entrypoint returns nil, it has no hooks. If it returns a table, hook names
are read from that table. A present hook must be callable. Other return shapes
are errors for entrypoints, while ordinary required modules may return any
Ember value.

This shape avoids exposing script globals as public API before there is real
pressure. A later compatibility slice can support top-level global
`function startup(ctx)` by adapting it into the same internal hook table.

### Hook Execution

`RunHook(ctx, "startup", args...)`:

- rejects nil or closed runtimes;
- loads unloaded entrypoints in declared order;
- for each entrypoint, asks the host adapter for globals using `HostCall`;
- skips missing hooks and records them in `HookReport`;
- calls present hooks in declared entrypoint order;
- returns the first hard runtime error with entrypoint and hook context;
- preserves loaded entrypoint state and required module cache across calls.

`startup` and `update` are not special to Ember. They are hook names. Hearth can
choose the names it calls.

### Require Semantics

Runtime-owned `require`:

- accepts a module path string in the first slice;
- normalizes relative paths against the requesting module;
- executes a module at most once per runtime;
- caches nil when a module returns no values;
- returns only the first module result;
- rejects active-loading cycles with a diagnostic path;
- shares the same module cache across all entrypoints in the runtime.

Later, a Hearth adapter can add Roblox-like ModuleScript instance requires by
exposing a host `require` wrapper or by adding an Ember host-value require
extension after the string path behavior is proven.

### Host Adapter Seam

`RuntimeHost.Globals` is the only host adapter method in the first public shape.
It is intentionally deep: Hearth can provide `game`, `workspace`, `script`,
`Instance`, `task`, `Enum`, service tables, and hook context values without
Ember knowing those domains.

The host adapter should be called at entrypoint load and hook call sites. The
returned map is copied into the active runtime global environment; host globals
override Ember base globals explicitly, matching `RunWithGlobals`.

Host callbacks need context propagation:

```go
type ContextHostFunc func(context.Context, []Value) ([]Value, error)

func ContextHostFuncValue(fn ContextHostFunc) Value
```

Plain `HostFunc` remains valid for simple callers. Context-aware host functions
let Hearth observe cancellation and use the current world/hook context without
global state.

### Execution Budget

`RuntimeOptions.MaxInstructions` applies to each top-level entrypoint load,
module load, and hook call. Exhaustion returns an error that callers can branch
on eventually; first slice can return a contextual error string.

Budget state must reset per public call. A runaway script in one hook must not
poison the next `RunHook`.

### Close

`Close` is repeat-safe. It releases references to loaded entrypoint exports,
module cache values, host adapter, and program-owned mutable runtime state. It
does not mutate host worlds or call script finalizers.

Closed runtimes reject `RunHook`.

## Reports

Reports should be plain data and stable enough for Hearth tests.

```go
type LoadReport struct {
	Entrypoints []EntrypointReport
	Modules     []ModuleReport
	Diagnostics []Diagnostic
}

type HookReport struct {
	Hook    string
	Calls   []HookCallReport
	Logs    []LogEntry // optional later; first slice may omit
	Budget  BudgetReport
}

type HookCallReport struct {
	Entrypoint string
	Module     ModuleID
	Hook       string
	Loaded     bool
	Called     bool
	Skipped    bool
}
```

First slice should report called/skipped hooks and load failures. Logs can
remain a Hearth host service concern until Ember grows a generic host logging
seam.

## Alternative Interfaces Considered

### Alternative A: One-Shot Runner

```go
RunSource(ctx, source, globals)
```

High convenience for tests, poor depth for Hearth. It does not own module cache,
many script roots, hook ordering, reload reuse, or persistent script state. This
should remain as `Compile`/`RunWithGlobals`, not become the integration seam.

### Alternative B: Runtime Owns Source Loading Directly

```go
runtime, err := NewRuntime(ctx, loader, options)
runtime.RunHook(ctx, "update")
```

Good default ergonomics, but it mixes immutable compile artifacts with mutable
execution state. Hearth reload and test isolation will want to compile once and
create multiple runtimes. This shape is acceptable as a convenience wrapper
later, but not as the core.

### Alternative C: Program Plus Runtime

```go
program, report, err := LoadProgram(ctx, loader, options)
runtime, err := program.NewRuntime(runtimeOptions)
report, err := runtime.RunHook(ctx, "update", frame)
```

Best depth and locality. Compile/cache/check behavior concentrates in Program;
execution/cache/hook behavior concentrates in Runtime; Hearth remains an
adapter. This is the recommended design.

## Implementation Slices

### Slice 1: Immutable Multi-Entrypoint Program

- Add public `ModuleID`, `ModuleLoader`, `Entrypoint`, `ProgramOptions`,
  `Program`, and `LoadProgram`.
- Adapt existing private `moduleKey`, resolver, graph, source identity, and
  artifact cache behind the new Program module.
- Support multiple entrypoints and shared dependency graph.
- Preserve existing `Compile` behavior.
- Tests:
  - two entrypoints requiring the same module compile that module once;
  - relative require normalization matches existing private behavior;
  - graph cycle reports a deterministic cycle path;
  - loader errors include module identity.

### Slice 2: Parallel Program Artifacts

- Add bounded internal worker phases for source loading, parse/bind, optional
  check, and compile.
- Keep `LoadProgram` as the only public seam for this behavior.
- Add `ProgramOptions.Check` and `ProgramOptions.Parallelism`.
- Preserve deterministic graph, diagnostics, and report order.
- Tests:
  - parallel and sequential loads return identical `Program` reports;
  - slow module loads complete faster with `Parallelism > 1` without changing
    report order;
  - context cancellation stops a load and returns `context.Canceled`;
  - checker diagnostics from multiple modules are sorted deterministically;
  - compile errors from parallel workers include stable module identity.

### Slice 3: Runtime Require Cache

- Add `Program.NewRuntime`, runtime module cache, and public `Close`.
- Execute required modules lazily with cached first return value.
- Keep module cache shared across entrypoints.
- Tests:
  - two entrypoints require one counter module and observe one module load;
  - active-loading require cycle is rejected at runtime;
  - `Close` is repeat-safe and later hook calls fail.

### Slice 4: Hook Entrypoints

- Entry modules return a table of hook functions.
- `RunHook` loads entrypoints lazily and calls hooks in entrypoint order.
- Missing hooks are skipped and reported.
- Non-callable hooks fail with entrypoint and hook context.
- Tests:
  - `startup` initializes captured table state used by later `update`;
  - missing hook reports skipped, not error;
  - hook order follows `ProgramOptions.Entrypoints`.

### Slice 5: Host Adapter Globals

- Add `RuntimeHost`, `HostCall`, and dynamic globals per entrypoint/hook.
- Add context-aware host callbacks while preserving `HostFunc`.
- Tests:
  - host receives entrypoint and hook names;
  - host global overrides base global;
  - context cancellation reaches context-aware host function;
  - globals for one entrypoint do not leak into another except through explicit
    module/cache state.

### Slice 6: Instruction Budget

- Expose per-call instruction budget through `RuntimeOptions`.
- Wire existing VM budget plumbing into public runtime calls.
- Tests:
  - infinite loop fails with budget exhaustion;
  - next hook call gets a fresh budget;
  - coroutine/resume paths honor the same budget policy.

### Slice 7: Hearth Adapter Proof

This belongs outside Ember, likely in a Hearth optional scripting module or
authoring runtime.

- Map `.server.luau`, `.client.luau`, and `.module.luau` files to Ember
  entrypoints/modules.
- Provide `RuntimeHost.Globals` with `game`, `workspace`, `script`, `Instance`,
  `task`, and services.
- Reuse Hearth's caller-owned `World` and frame loop.
- Keep root Hearth free of Ember imports unless a later import-surface decision
  proves otherwise.

## Dependency Strategy

- Ember Program/Runtime dependencies are in-process.
- `ModuleLoader` is local-substitutable: tests use an in-memory adapter; Hearth
  uses an authored-place or filesystem adapter.
- `RuntimeHost` is a real seam: tests use an in-memory adapter; Hearth uses a
  world-backed adapter.
- Program loading can use bounded goroutines internally because `LoadProgram` is
  explicitly called, context-bound, and pure except for `ModuleLoader`. Runtime
  hook execution remains sequential and deterministic until Hearth proves a
  parallel schedule seam.

## Checks

Focused Ember checks:

```sh
go test -count=1 ./...
scripts/check-lane root
```

Before calling the integration slice done:

```sh
scripts/check-fast
scripts/check
```

For the Hearth proof, run the relevant Hearth optional scripting lane after the
adapter exists.

## Risks

- Compatibility: hook-table entrypoints are an embedding convention, not full
  Luau/Roblox script compatibility. Keep it documented and adapt later.
- Public interface: `Program` and `Runtime` will become sticky. Keep options
  structs small and avoid exporting graph internals.
- Determinism: parallel loading must not change diagnostics, require order,
  graph order, compile result order, or hook order. Sort every externally
  visible result by stable keys before returning.
- Concurrency: no goroutine may outlive `LoadProgram`; cancel and drain worker
  phases on the first hard error.
- Context propagation: old `HostFunc` cannot observe cancellation. Add a
  context-aware host callback without breaking existing users.
- Hearth leakage: do not add `World`, `Entity`, `Frame`, or DataModel classes to
  Ember. They belong in the Hearth adapter.
