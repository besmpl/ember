# ADR 0012: Keep Languages Concrete and Share Static Prepared Delivery

Status: Superseded by ADR 0013

This record is retained only as research and proof history. Its unfinished
stages are not accepted by ADRs 0013-0015. The pure `preparedsource` S2 value
seam, the Luau/embedded S3a consumer, an external
static-delivery S5a producer, and a real independently versioned external
compiler now have locally verified working-tree candidates. A static mixed
application also composes that compiler with the real internal Sprig compiler
through one `Layout` and one concrete handler without a common ABI, package
graph, allocation, or material call tax. An internal proof-only S6 Sprig slice
parses, checks, and evaluates a bounded acyclic project. It exposes two
explicit preparation products: a location-neutral self-contained `Set` per
requested standalone entry, and an application-bound linked package closure
using caller-supplied Go import bindings. The application, not the language
producer, places either result in a `Layout`. The linked form is selected for
shared multi-entry application projects; the standalone form remains the
simpler distributable product. The proof-only Ruby D1 slice now parses and
checks actual Ruby source, seals passive lookup facts, executes a Ruby-owned
canonical object/method/block/unwind model, and emits an independent fixed
owner with eager effective-resolution cells, attachment-backed include/prepend
topology, three ordinary fixed two-arm direct-target sites, and one dedicated
two-arm allocation-site singleton site. It proves selective
inherited-method invalidation,
snapshot aliases, local removal exposing an ancestor, local undef blocking
that ancestor, literal `send`/`public_send`, public/protected/private entry
transitions, pre-effect visibility rejection and recovery, transactional
mutation, zero-allocation warmed dispatch, agreement with a pinned local CRuby
oracle, and disappearance from a static zero-cgo application. Target37 further
emits strict selector/site/mode-specialized warmed leaves behind tiny
one-charge wrappers and retains one generated cold-recovery authority. Two
local identity-bound captures put that bounded alternating-hit shape at
1.66-1.72x the current generic helper shape and within 1.00x of an independent
equivalent-Go lane; this is not open-world Ruby or CRuby/YJIT evidence. A
Target38 repeated that exact C4 site mix through 384 sites: strict leaf text
scaled approximately linearly, while exact generated-leaf-template sharing
materially reduced source/archive/text at 96 sites. Production remains strict
because the current three leaves have no alpha-equivalent pair and the shared
site-pointer hot gate is incomplete; array-backed sites are rejected for now.
A bounded Target39 C1 extension now adds `include` and `prepend`, a subclass of
an included class, a method defined after prepend, and a later included-module
redefinition. Immutable checked attachment descriptors plus owner-local
activation bits are the sole topology authority; routes are derived into
bounded non-allocating scratch only during construction and mutation. The
generated warmed leaves retain no attachment, topology, route, snapshot, or
oracle dependency. An independent all-mask oracle and a test-only sealed-
snapshot comparator establish the exact closed fixture tradeoff without
turning snapshots into production state or claiming elapsed-time superiority.
A proof-only C0
composition now
restores that real generated Ruby owner and real generated Sprig value state
from one application-owned scalar checkpoint, executes both behind one typed
handler, and commits/replays the result through production `OpenEmbedded`.
A separate fresh static application mounts only their two Sets, closes and
recreates the Ruby owner between two transactions, and retains neither
compiler nor delivery code. The separate proof-only RP0 slice now projects one
bounded Ruby object graph through a 52-byte application-owned record, closes
v1, passes only three scalars into v2's generated module, reconstructs aliases,
an equal-but-distinct object, a cycle, and captured `self` privately, and
executes v2's newly checked code. These are embedded and
static checkpoint falsifiers, not process reload or a production Ruby heap.
One later disposable S4 preflight also falsified the proposed multi-leaf Go
overlay mechanism: a real `doc.go` directory anchor did not make Go 1.26
discover any nonexistent overlay-only Go or embed leaf. The revised proposal
uses an application-owned minimal standalone worker module in a fresh physical
root. A follow-up disposable physical-root preflight passed ordinary cgo-free
Go discovery, build, execution, selected-leaf accounting, root-location
BuildID identity, and isolated v1/v2 changes. A third disposable observer
proved that Go 1.26 field-selected package and module JSON can preserve every
builder-consumed value while removing unused recursively expanded dependency
output and bounding raw decoding and direct-child process ownership. A
subsequent three-vector bound review selected one private complete-closure
residual ledger plus independent conjunctive discovery/work ceilings; general
Layout validity does not promise S4 worker admission. A later local-replacement
review selected versioned-only module replacement for the initial S4 cutover:
filesystem replacement targets remain a bounded revisit route, not an admitted
source-root mechanism. The language-neutral worker builder, that production
physical-root consumer, external worker composition, public/full Ruby, and a
general Sprig language remain unimplemented.

## Context

Ember currently implements one language. The root `ember` package parses,
checks, compiles, and executes Luau-compatible source. Its prepared compiler
lowers a Luau `Program` through Luau-shaped backend IR and emits one generated
Go file containing a concrete `PreparedBundle`. Exact Machine replay preserves
behavior outside proved generated paths.

The transactional reload modules are already less coupled:

- `preparedworker` accepts closed application records and opaque verified
  builds. It does not exchange an Ember `Runtime`, `Value`, frame, callback, or
  heap across the process seam.
- `preparedworkerbuild` owns the Go-toolchain effect, but its `Program` input
  and private build descriptor still name `*ember.PreparedGoArtifact`, the
  Luau Program recipe, prepared ABI, Program hash, and Proto inventory.
- The working-tree `PreparedGoArtifact` now owns one immutable
  `preparedsource.Set` plus Luau identity. `cmd/emberc` mounts it through one
  `Layout` and materializes the current fixed one-file shape; the worker
  builder still consumes its temporary singular writer view.
- A separate working-tree fixture module now imports only `preparedsource`,
  contributes distinct Ruby-shaped and Sprig-shaped Sets, and is deleted
  before a zero-dependency static application is built and executed. This is
  delivery-shape evidence, not either language's semantic implementation.
- A second independently versioned fixture owns a bounded Go-like functional
  language end to end: lexer, parser, nominal checker, evaluator, diagnostics,
  source identity, and direct-Go emitter. It also imports only
  `preparedsource`, and the compiler plus source disappear before its generated
  application builds offline. This is a real extension-mechanism proof, not a
  public or production language.
- `internal/sprigproof` is a second, deliberately narrow producer. Its checked
  project can resolve a `counter -> math -> util` call chain beside an
  unrelated package, has a canonical evaluator, and emits direct typed Go.
  Nominal facts, transitive closure, standalone behavior, cancellation,
  limits, ownership, dependencies, cache actions, allocations, and local
  performance are checked. It is an architecture proof, not a public or
  general Sprig implementation.
- A separate application-tooling fixture now invokes the real Sprig and Seed
  compilers, mounts their independent Sets in one canonical Layout, and emits
  one explicit statically typed handler. The fresh application retains only
  its two generated packages and handler. This proves bounded static
  composition, not worker reload or a public cross-language ABI.
- `internal/rubyproof` is a bounded D1 dynamic-language producer. It accepts
  actual Ruby 2.6 source for `Base`, inheriting `Alpha`, overriding `Beta` and
  `Gamma`, one ancestor redefinition, `alias_method`, `remove_method`,
  `undef_method`, identity, blocks, non-local return, and rescue/ensure. The
  checker owns a sealed passive lookup image; canonical and generated owners
  independently implement lookup and mutation. Generated Go projects only the
  admitted `label` and `saved_label` closure into fixed cells and scalar arms,
  while statically proved methods remain direct named bodies. This is
  architecture evidence, not a public `/ruby`, a broad RubySpec claim,
  production heap, or worker checkpoint/reload proof.
- `internal/mixedcheckpointproof` is the bounded C0 application proof. It
  statically imports the real generated Ruby and Sprig packages, owns the
  request/result/checkpoint/effect schema and execution order, restores one
  fresh Ruby owner, and uses the production embedded transaction journal. Its
  scalar Ruby state deliberately does not preserve Ruby object identity, and
  Sprig remains a pure value computation with no owner. It is not S5b, S7,
  R1, an EPW2 process generation, or hot-reload proof.
- `internal/rubyprojectionproof` is the bounded RP0 application proof. It owns
  one fixed canonical graph record and explicit concrete scalar mappings between
  two independently generated Ruby packages. Application IDs, scalar data, edge
  tags, and a reconstructable captured-self behavior survive only in the
  application record. Each generated package accepts just `Root`, `Other`, and
  `Tail` scalars and privately owns topology and behavior reconstruction. Ruby
  object IDs, pointers, epochs, frames, live return targets, and executable
  closures do not cross the seam. It is not a general heap serializer, process
  generation, or R1.

Ruby is not another syntax over Luau. Classes, open methods, blocks, non-local
returns, rescue/ensure, fibers, object identity, and monkey patching require a
Ruby-owned canonical runtime and recovery model. The frozen `matz/spinel`
compiler confirms that its useful AOT mechanisms sit around Ruby-specific
declaration, inference, object, and runtime policy rather than replacing it.

A prospective third language creates a different pressure. The working example
in this ADR is **Sprig**, a name used only to make the design concrete. It is a
small, statically typed, functional language with Go-like packages, records,
functions, explicit errors, and explicit effects. It should compile almost
entirely to ordinary typed Go and should not inherit Luau or Ruby runtime costs.

The architecture therefore needs a seam that all three languages can use
without making their semantics share a lowest-common-denominator runtime.

| Boundary | Current implementation | Proposed direction |
| --- | --- | --- |
| Language implementation | Luau in root `ember`; internal bounded multi-package Sprig and Ruby D1 proofs; external bounded Seed compiler proof; real Sprig/Seed static composition | Concrete `/luau`, `/ruby`, and `/sprig` modules when proved; independently versioned languages remain concrete |
| Prepared compiler result | Luau artifact owning one immutable one-file `Set`; Sprig project owning explicit standalone/linked packages; Ruby D1 Program emitting one fixed-owner Set | A concrete result per language containing only requested generated packages and language identities |
| Worker build input | One Luau Program recipe and artifact | One canonical application-owned Layout plus its fresh minimal physical worker-module root, with no language ABI |
| Native backend | Luau generated Go plus exact Machine replay; direct typed Sprig/Seed; fixed-cell/direct-target Ruby D1 Go plus Ruby-owned recovery | Ordinary generated Go, with language-owned runtime/recovery |
| Reload and state | Typed whole-worker transaction in `preparedworker`; bounded C0 scalar composition and RP0 Ruby graph projection | Keep application-owned closed records and prove process generations separately |

## Decision

### Put the public extension seam after language lowering

Each language is one deep compiler/runtime module with a concrete interface.
There is no public `Language`, `Compiler`, `Backend`, `Runtime`, `Value`, or
registry interface shared by all languages.

The common public value is an immutable generated Go package. A language-owned
compiler returns it; the application chooses where to mount it and assembles a
minimal physical worker module; the generic builder verifies that root and
compiles it into one worker generation.

```text
Luau source  -> Luau compiler/runtime  ----+
Ruby source  -> Ruby compiler/runtime  -----+-> generated Go packages
Sprig source -> Sprig compiler/runtime ----+        |
                                                     v
                                             canonical source Layout
                                                     |
                                    +----------------+----------------+
                                    |                                 |
                             ordinary go build        app-owned minimal physical root
                                    |                                 |
                             embedded release               preparedworkerbuild
                                                                      |
                                                              verified worker build
                                                                      |
                                                               preparedworker
                                                                      |
                                                        quiescent generation swap
```

The complete shared architecture has four values and owners:

1. a producer-owned `Set` containing one generated Go package;
2. an application-owned `Layout` placing all selected sets;
3. a builder-owned verified `Build` containing one executable; and
4. a worker-owned live generation containing transaction state and concrete
   language owners.

No shared compiler IR, guest value, runtime owner, or language registry sits
between those values.

Compilers are called explicitly by concrete package name:

```go
luauSet, luauReport, err := luau.CompilePrepared(ctx, luauInput, luauOptions)
rubySet, rubyReport, err := ruby.CompilePrepared(ctx, rubyInput, rubyOptions)
sprigProject, sprigReport, err := sprig.CompileProject(ctx, sprigSources, sprigOptions)
standalonePackages, err := sprigProject.PrepareStandalone(sprigEntries)
linkedPackages, err := sprigProject.PrepareLinked(sprigEntries, applicationBindings)
```

The similar function shape is convention, not a Go interface. Their inputs,
reports, limits, semantic identities, and errors remain language-specific.
The two Sprig calls are different products, not a mode flag or adaptive
planner: choose standalone artifacts when placement must remain unknown, and
linked packages when one application owns the complete build.

### Add one deep immutable prepared-source module

The working-tree `github.com/besmpl/ember/preparedsource` candidate is a pure
in-process module. It owns validation, immutable storage, canonical ordering,
collision rules, fixed construction limits, and content identity. It performs
no filesystem, toolchain, registration, import, process, or cache effect. S3a
and S5a now provide real in-module and external consumers; its public shape
remains provisional until S4's worker consumer proves that the same value
removes duplicated policy at both effect boundaries.

One `Set` is the producer's complete generated contribution to exactly one Go
package; it never spans packages. The language producer chooses the Go package
name and exact file contents. The application, not the producer, chooses the
stable module-relative mount. Keeping placement separate means the same
artifact can be used in an embedded build, a worker build, and a test without
changing its producer identity.

The S2 candidate exposes exactly this shape:

```go
package preparedsource

type FileKind uint8

const (
    GoFile FileKind = iota + 1
    AssetFile
)

type File struct {
    Name    string // portable basename, not a path
    Kind    FileKind
    Content string // arbitrary immutable bytes
}

type Set struct {
    // unexported canonical package name, files, and digest
}

func NewSet(packageName string, files []File) (Set, error)
func (set Set) PackageName() string
func (set Set) Digest() [32]byte
func (set Set) Files() []File
func (set Set) IsZero() bool

type Mount struct {
    Path string // application-owned module-relative package directory
    Set  Set
}

type Layout struct {
    // unexported canonical mounts and digest
}

func NewLayout(mounts []Mount) (Layout, error)
func (layout Layout) Digest() [32]byte
func (layout Layout) Mounts() []Mount
func (layout Layout) IsZero() bool
```

`Mount` is raw application input; effectful consumers accept only a valid,
nonzero `Layout`. There is no options object, configurable limit, builder,
writer, iterator, lookup, validator interface, or custom digest type. In
particular:

- a `Set` contains one complete producer-owned package contribution and cannot
  be incrementally mutated;
- zero `Set` and `Layout` values are explicitly observable through `IsZero`;
  their other accessors return zero or nil results, and constructors plus
  effectful consumers reject them rather than treating a zero digest as proof
  of validity;
- package, file, asset, and mount names have one lowercase-ASCII spelling;
  construction never consults an OS, accepts aliases, or normalizes Unicode;
- files are portable basenames and are either Go source or inert embed data;
- safe-Go strings allow arbitrary bytes without exposing mutable slices;
- construction rejects empty sets, invalid package names, duplicate names,
  `_test.go`, known target-selecting filenames, build and line
  directives, `go.mod`, `go.sum`, foreign/prebuilt source, decoded `import
  "C"`, and explicit `init` functions;
- Go files are scanner- and parser-valid under fixed token and delimiter-depth
  bounds and declare the selected package; this is deliberately not a type
  check, import-resolution proof, or successful-build claim;
- every asset has exactly one raw `//go:embed <basename>` attached to one
  package-level, single, explicitly typed, uninitialized `string` variable;
  the directive's file has the package's sole blank `_ "embed"` import, and
  every other generated `//go:` directive, including `go:linkname`, is
  rejected;
- fixed policy permits at most 256 files, 32 MiB per file, and 128 MiB per
  Set; limits remain package policy until a real producer proves that
  caller-selected limits are necessary;
- ordering and the domain-separated digest cover package name, kind, basename,
  length, and exact content;
- `NewLayout` rejects absolute paths, `.`/`..`, backslashes, control bytes,
  platform-reserved components, duplicate package-directory ownership, and
  every spelling outside its lowercase-ASCII grammar;
- a Layout permits at most 64 mounts, 32 path components, 240 path bytes,
  4,096 repeated mounted leaves, and 1 GiB of repeated mounted content, so
  mounting the same large Set repeatedly cannot amplify materialization work
  without bound;
- `NewLayout` sorts once and derives a separate domain-separated digest over
  each canonical mount path and `Set` digest; and
- detached accessors return canonical copies and never expose mutable storage.

This validation defines a portable artifact policy; it is not a sandbox or a
proof that arbitrary Go initializers are pure. Generated packages remain
trusted application build output, and their concrete handler owns semantic
startup validation before `READY`.

There is no `Merge` method that lets producers choose deployment layout. A
host composes independent sets by constructing one complete layout.

The deletion test justifies both levels. Without `Set`, every language
generator, embedded materializer, and worker builder must duplicate immutable
ownership, Go validation, limits, ordering, and package hashing. Without
`Layout`, every effectful consumer must duplicate mount grammar, cross-package
collision detection, canonical ordering, and application-layout identity.

### Fix ownership and failures in each concrete language

Each language owns three categories, expressed through concrete types rather
than a shared Go interface:

1. an immutable owner-neutral image or program value that may be shared;
2. zero or more mutable lane owners bound to that image, each admitting one
   active call and non-copyable by contract; and
3. boundary values explicitly classified as detached/caller-owned or
   owner-bound.

An owner-bound value never crosses an owner, lane, language, process, or worker
generation and becomes stale when its owner closes. Cross-language adapters
must detach into application-owned closed Go records. Only an owner that owns a
resource or mutable lifetime exposes `Close`; a scratch-only static-language
executor need not.

Cancellation is host control, not a universal guest exception. Each language
defines and tests its safe points and whether cancellation leaves its owner
reusable. The safe V1 rule for a mutable owner is an uncatchable host abort: if
invariants after interruption are not proved, poison and close the owner rather
than resume it. Direct embedded calls preserve `errors.Is` for
`context.Canceled` and `context.DeadlineExceeded`; process calls preserve the
stable `preparedworker` failure class instead of concrete Go error identity.

Resource limits remain concrete language semantics and start fresh for each
call. Luau steps/stack/heap, Ruby frames/objects/unwind, and Sprig loop/list
bounds are not forced into one counter. A mixed application passes one context
plus explicit per-language limits; an aggregate deterministic budget would be
separate application policy.

Expected domain rejection is a typed result. Go errors report cancellation,
limits, malformed boundary data, or an internal failure. A direct language call
does not promise state rollback; durable atomicity exists only at the existing
application transaction/checkpoint seam. Any backend Go error after worker
admission makes that generation terminal under ADR 0011.

### Keep mixed checkpoint policy application-owned

C0 exercises that rule with concrete generated packages rather than a
language interface. Its versioned checkpoint contains only Ruby's detached
`State{Value, Trace}` and one Sprig total. Restore validates the complete
record before constructing a fresh Ruby owner. Object identity and method
tables intentionally start fresh; this scalar proof is not a serializer for a
production Ruby heap. Sprig has no owner in this slice, so cleanup proves one
Ruby lifetime rather than a general multi-owner reverse-close algorithm.

The application orders Sprig before Ruby and passes one context with distinct
Sprig and Ruby limits. A typed Sprig domain result or rejection commits the
unchanged checkpoint and cannot enter Ruby. Once Ruby starts, cancellation,
limit, or internal Go failure returns no result, checkpoint, or effect and
poisons the handler. The production embedded runner then quarantines the
generation; durable resolution remains not-committed and later application
entry fails as lost. Close retains the Ruby owner until its close succeeds, so
a busy close remains retryable.

On success, one application `Result`, one detached `Checkpoint`, and at most
one ordered `Effect` form the complete decision. `OpenEmbedded` proves durable
commit, exact duplicate replay without duplicate effect delivery, close,
reopen from the journal checkpoint, and the next transaction. That close and
reopen is a restore observer, not a live candidate swap. The separate static
application proves that both compilers and `preparedsource` disappear; the
embedded runner intentionally retains `preparedworker`. Process launch,
builder neutrality, catch-up, activation, retirement, and target-native reload
remain S5b and S0 work.

### Project application topology, not a Ruby heap image

RP0 extends the same rule to one deliberately bounded object graph. The
generated Ruby package exposes a separate fixed-size projection owner; it does
not add fields or branches to the scalar `Engine`. Its entire external
projection value is three named `int64` scalars: `Root`, `Other`, and `Tail`.
The generated package exposes no application IDs, edges, behavior tags, graph
record, observation telemetry, initial scalar values, or default constructor.
Instead, its snapshot validates the complete private graph invariant, including
pairwise-distinct concrete nodes, before returning those scalars. Topology
validation precedes behavior classification, so malformed private structure
cannot masquerade as a live non-checkpointable continuation; only a sound
graph may reach the live-behavior rejection. The
application alone places them in its 52-byte little-endian record with exact
counts, canonical application node IDs, explicit node/edge/behavior tags, and
fixed arrays. It
validates bounds and the whole admitted topology before constructing v2. There
is no generic graph, shared `Value`, serializer interface, language registry,
reflection, `unsafe`, or builder change.

The admitted graph contains two roots that alias node 1, node 2 with the same
scalar but distinct identity, a node-1 self-cycle, a path from node 2 to node
3 and back to node 1, and one behavior tag capturing node 1. Projection
reference numbers exist only in the application record and its bounded
validation; generated packages never receive them. The record contains
application IDs, tags, and scalar data, not Ruby object IDs,
pointers, class or method tables, method epochs, frames, return targets, or
executable closures. A recognized live targeted-return behavior fails as
non-checkpointable; unknown tags and malformed topology fail closed.

The application creates v1 by mapping `InitialCheckpoint` through `RestoreV1`;
generated packages contain no independent `{40, 40, 7}` default. The v1 owner
is closed before v2 construction. Mapping into either generation names only its
concrete scalar type. Application `Validate` owns the record's nonnegative and
maximum scalar policy, while each generated constructor owns only the safe
language-local superset: equal-valued root/other objects and source-derived call
arithmetic. This keeps application policy out of generated code without making
either boundary fail open. v2 owns the fixed edges, aliases, distinct
allocation, and captured `self`. The record carries no source or method
identity, so the selected v2 package owns code: identical state calls return 41
under v1 and 49 under the one admitted v2 source change. The generated
constructor rejects root/other inequality and source-derived addition overflow
before allocation. Its three allocation steps, topology/binding step, and
final publication step all
poll cancellation and consume explicit budgets. Application validation,
mapping, post-construction, and pre-publication stages poll separately. Step,
object, cancellation, arithmetic, or injected post-construction failure returns
no owner; any constructed owner is closed before the error escapes. Tests hit
all nine generated context polls, every insufficient step/object budget, every
private topology predicate class, and blocked Call/Snapshot versus Close.

This is the smallest useful Ruby migration mechanism: application schemas
name durable meaning, while each generation owns heap representation and
code. It proves neither arbitrary object traversal nor stable Ruby identity.
Proc bodies, fibers, continuations, singleton classes, method-table mutations,
and live non-local return targets need an explicit application projection or
make that application non-reloadable. A production Ruby heap/GC and real
process-generation transaction remain R1/S5b work.

### Keep generated interfaces concrete and typed

Generated packages statically import their concrete language runtime and expose
the interface best suited to that language.

Luau may continue to expose a concrete prepared bundle and exact Program
loader. Ruby may expose a sealed Ruby image plus Ruby runtime binding. Sprig V1
starts with ordinary typed Go records and the simplest immutable,
concurrency-safe function:

```go
package rulesgen

func Reduce(
    context.Context,
    Limits,
    State,
    Request,
) (Decision, error)
```

Immutable program tables are package-static and shared. Inputs are not mutated;
the returned decision is detached. Domain rejection is a typed decision field.
This baseline fixes semantics and gives optimization a real comparator without
inventing ownership or construction ceremony.

An owner-bound `Engine.ReduceInto` is a later Sprig-specific optimization, not a
model other languages copy. If promoted, the engine owns only bounded reusable
scratch, admits one active call, and needs no `Close`. Per-call limits remain
fresh. `ReduceInto` logically empties its caller-owned output before execution
and on every error while retaining capacity; success may reuse only
caller-owned output storage. Reusing the same output overwrites it, other
outputs remain valid, and no engine memory escapes. Parallel callers create one
engine per lane. The optimization replaces `Reduce` only after clearing the
comparative gate below; otherwise it is deleted.

A language compiler may generate an application-specific
`preparedworker.HandlerFactory` as an additional adapter when that removes real
wiring. It is not the cross-language generated ABI. A mixed application keeps
one explicit Go handler that imports each generated package and decides call
order itself.

### Use Sprig to prove the seam without importing dynamic-language machinery

Sprig's working V1 language shape is intentionally small:

```text
package counter

type State struct {
    Total i64
}

type Event =
    | Add { By i64 }
    | Reset

func Update(state State, event Event) (State, []Effect) {
    match event {
        Add { By } => {
            next := State{Total: state.Total + By}
            return next, []Effect{ScoreChanged{Value: next.Total}}
        }
        Reset => return State{}, nil
    }
}
```

The example establishes architectural requirements, not a complete language
specification:

- static acyclic packages and explicit exported signatures;
- `bool`, `i64`, `f64`, `string`, immutable records, tagged unions, exhaustive
  matching, immutable lists, `Option`, and `Result`;
- local scalar rebinding is permitted, but there is no observable mutable
  global state or mutable heap identity;
- deterministic left-to-right evaluation and explicitly specified numeric
  behavior;
- domain failures are values rather than exceptions;
- time, randomness, files, networking, and host facts enter as values;
- application effects leave as closed ordered values;
- no reflection, macros, `eval`, inheritance, metatables, implicit coercion,
  goroutines, or hidden I/O;
- V1 needs no general recursion; loops poll at entry and every backedge.

Its private pipeline is:

```text
source packages
  -> compact syntax and spans
  -> language-owned checked project graph
       PackageID / DeclID / VariantID / FieldID / BindingID
  +-> canonical evaluator (test oracle only)
  `-> explicit requested preparation
       +-> PrepareStandalone(entries)
       |    -> imported-call closure copied into each requested entry Set
       `-> PrepareLinked(entries, application bindings)
            -> requested package closure with ordinary direct Go imports
            -> one Set per reachable guest package

application-selected Sets
  -> application-selected mount paths
  -> preparedsource.Layout
```

This is a deep language module: parsing, checking, nominal resolution,
evaluation, dependency closure, deterministic naming, and emission sit behind
the concrete `CompileProject`, `Project`, `PrepareStandalone`, and
`PrepareLinked` interface.
Callers do not orchestrate passes or implement adapters for a generic compiler
seam. Spelling resolves only at the checker seam. The checked tree then carries
frozen `BindingID`, `DeclID`, `VariantID`, and `FieldID` facts consumed by both
the evaluator and emitter; neither downstream path repeats name lookup.

`PrepareStandalone` emits only requested entries and copies each entry's
complete reachable imported-call closure into its Set. A location-neutral Set
cannot name another generated package's Go import path because only the
application knows module and mount placement. The standalone product therefore
preserves arbitrary placement, deletion of the producer, direct same-package
calls, and one independently distributable Set without a producer-owned Layout
or unresolved registry.

`PrepareLinked` is a separate application-bound product. It accepts a bounded
`PackageID -> Go import path` table only after checking, emits the requested
entries plus their transitive package closure in canonical PackageID order, and
uses direct Go imports rather than copied helpers. A caller may pass bindings
for every checked package so it need not rediscover Sprig's private dependency
graph; only reachable packages emit code. Missing reachable, duplicate,
unknown, invalid, reserved-support, or colliding bindings fail before any
Set is published. The application still constructs `preparedsource.Layout` and
owns `go.mod`, mount paths, and host imports. No producer Layout or generic link
graph was added.

Checked project and package identities never contain application paths. A
linked Set's content digest naturally changes when one of its direct imported
paths changes; an unrelated or transitive-only consumer Set stays byte-identical.
The application's Go source, module, Layout, and builder identity bind the
remaining placement. Cross-package domain codes are converted explicitly.
Calls consume checked `DeclID`; an already exported guest function keeps its Go
name, while a lowercase target receives a deterministic exported wrapper
derived from its nominal declaration. The emitter never repeats source lookup.

The bounded proof accepts lowercase ASCII PackageIDs that are valid importable
Go package names, acyclic first-order `i64` calls, pure function dependencies,
and the exact `counter` host schema. It proves a three-package call chain,
not general cross-package types, recursion, effects, state, or a stable Sprig
interface. Public generated names that cannot be represented directly fail
closed; private support and helper names use a deterministic allocator.

The internal proof shows that this frontier needs no ANF/Core,
closure-conversion stage, monomorphizer, public compiler interface, or shared
backend. Add a private
language-owned intermediate form only when a larger concrete language feature
requires it or measured optimization pays for it; never make that form the
cross-language seam. The baseline Go emitter implements every accepted
construct exactly. A small canonical evaluator is a differential-test oracle
and need not ship in release
binaries. Generated calls and representations are concrete; mixed-language
composition performs no intermediate codec, generic-value conversion, or
language switch. An optional scalar optimizer may be added only where it beats
the Go compiler and preserves the same entry/backedge Poll schedule. Polling
uses a stack-local bounded countdown in the baseline, or engine-local state in
the promoted optimization, so the common case is an inline decrement/branch
rather than an interface call. Sprig therefore does not pay for Luau tags,
Ruby sends, generic guest values, or interpreter dispatch.

#### Package-shape decision

S6b compared the real Sprig emitter on deterministic shared DAGs with 2 entries
and 32 functions per package, 4 and 128, and 8 and 256. Every entry called the
same two-level shared dependency; an unrelated package was checked but not
requested. Go 1.26.4 darwin/arm64, `CGO_ENABLED=0`, fixed offline flags, a
prewarmed standard-library cache, unique module identities, five rotated fresh
rounds, two steady samples per round, exact compile traces, and
`/usr/bin/time -l` supplied bounded local observations. Ratios below are linked
over standalone-closure medians; lower is better:

| Shape | Source bytes | Prepare | Fresh build | Peak build RSS | Binary bytes | Steady run | Warm build |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 2 x 32 | 0.711x | 0.755x | 1.106x | 0.985x | 0.992x | 0.956x | 0.970x |
| 4 x 128 | 0.544x | 0.624x | 0.903x | 0.956x | 0.915x | 0.811x | 0.931x |
| 8 x 256 | 0.461x | 0.543x | 0.612x | 0.888x | 0.761x | 0.870x | 0.847x |

At 8 x 256, generated source fell from 1,973,448 to 910,343 bytes,
fresh build from 849 to 520 ms, peak observed RSS from 116.8 to 103.8 MiB,
binary size from 5,035,554 to 3,830,530 bytes, and the same-result steady run
from 12,960 to 11,273 ns. At 4 x 128, an entry-only rebuild fell from 268 to
185 ms and a shared-leaf rebuild from 303 to 227 ms. The linked leaf was the
only changed Set; real Go dependents recompiled, while the application compile
was reusable in that capture. An unrelated change altered no Set, compiled no
package, and preserved the binary in both shapes. Linked preparation's target
symbol cache also removed an accidental quadratic planning cost.

The small case saved 17 KiB of generated source but added two Go packages and
its fresh-build direction changed across local repeats; that absolute result
does not earn mandatory linking. Conversely, the large result clears the
20-percent source, fresh-build, binary, and 100 ms gates with no steady
regression. A separate disposable topology control found that fusing everything
into one package nearly doubled large-case peak RSS (1.975x), raised medium RSS
to 1.392x, and destroyed independent package locality even when other metrics
improved. The fused emitter was not retained.

The selected rule is therefore structural and explicit, not a hidden size
heuristic:

1. use `PrepareStandalone` for independently mountable or distributable
   entries, especially one-off and small builds whose application placement is
   intentionally unknown;
2. use `PrepareLinked` for an application-owned multi-entry project with shared
   dependencies; and
3. do not add a fused package, adaptive planner, shared link graph, universal
   backend, or runtime registry.

These timings are local architecture evidence, not a retained release claim.
New toolchains or production graphs may move the crossover, but they do not
change the two product contracts.

The final real-emitter capture bound probe source
`e91b469683e49507d9e8c8e06ed22340ea6460b6d61fa2e53c141cd30bcd8726`,
probe executable
`257add5e95ddb79b46879aa4eeac1aad6a2185f59a9479f0b5b7afb083c03b21`,
orchestrator
`d8af8d0d7a037b49c6808ec4bef411cf37946e9e3cf1cc974225dbd117a5e92e`,
and raw JSON
`34ba166cfe362f465755213ff8036ebfc801d0ab64cc324339b811ebe60377a4`.
The earlier fused topology control bound script
`bf2b82794df77b074443da0a6d60f62b85f37937a7b2fd0563c12726d4c84fa9`
and JSON
`63d1175745fdfbf9858b2442b48f097bf244fe1ac9abeb555120a725703ccbb9`.

#### Real external compiler proof

`preparedsource/testdata/externalcompiler` closes the gap between a producer of
stored generated text and a third-party compiler. It is a separate Go module
whose only non-standard dependency is `preparedsource`. Its small Seed language
has Go-like package/function syntax and a pure `i64` expression body. The
fixture owns a bounded lexer, parser, nominal binding checker, deterministic
diagnostics, exact source identity, canonical evaluator, overflow/division
domain values, cancellation/limit policy, and direct typed-Go emitter:

```text
Seed source
  -> external lexer/parser/checker
  -> external immutable Program + canonical evaluator
  -> external direct-Go emitter
  -> preparedsource.Set
  -> application-owned Layout and fresh materialization
  -> delete compiler module and Seed source
  -> offline zero-cgo Go build and execution
```

The concrete call surface is only `Compile`, `Program.Evaluate`, and
`Program.PreparedSource`; it implements no Ember interface. The checked tree
resolves parameter spelling to private binding IDs before evaluator or emitter
use. Repeated compilation pins Program, Set, Layout, and complete application
bytes. Unknown bindings, invalid syntax/types/names, excessive source/tokens,
integer range, zero Programs, nil context, cancellation, and limits fail at
their owning boundary.

The parent proof copies the module outside Ember, runs it with the network
disabled, validates that its exact non-standard graph is the compiler command
plus `preparedsource`, and materializes one application-owned mount. It then
deletes the compiler, source, and tooling directory. With separate fresh
offline caches, the remaining four-file application has no module dependency,
no Ember/compiler/delivery symbol, reports `CGO_ENABLED=0`, executes positive,
left-first overflow, later divide-by-zero, addition-overflow, pre-cancellation,
and limit cases, and allocates zero on its steady scalar path. All six supported
OS/ISA combinations cross-build.

Go 1.26.4 darwin/arm64 local evidence records exact Seed source SHA-256
`811e657cfb9c37028681c783af12b5425ae5b5afe7360844f9f08b499e8fa1e7`,
Program identity
`b5f1ada51772f913502ae4a0cb40790dcfb4f98f620412f33e35df86b5b543f9`,
Set identity
`ab879222f8fb1795de30d5c2c958e3a1253390ee2ff314ec54c8c266ae1c1a67`,
Layout identity
`8e428a43250dfc85069c04d2f26bb0bed24447ba01727500990764fbf4f52b69`,
and 1,664 generated bytes with SHA-256
`7502fd7c3d508df930ecc89e61455bb0835f57014555392f9059c0be9a214b62`.
Seven bounded benchmark samples give a 3.854 ns generated median versus
3.851 ns for equivalent context-polled handwritten Go, or 1.001x, with zero
allocations for both. The local trimmed binary was 2,547,154 bytes with
SHA-256
`ce5aaa9a6394b1ebad1dfc3e68a5642795bf6011a7e5643a975a57c204a09ba7`.
These timing and binary values are local architecture evidence, not retained
release claims.

Comparing this compiler with Sprig does not earn another shared compiler
module. Both call SHA-256, deterministic sorting, `go/format`, and
`preparedsource.NewSet`; those complete operations already have owners. Their
lexers, syntax trees, diagnostic contracts, checked facts, package models,
generated ABIs, Poll schedules, and emitter policies differ. A scalar IR would
add a tree conversion without supplying a second optimizer or target. A helper
API would therefore expose callbacks for language-owned decisions to save a few
local lines. The deciding result is stronger and simpler: an external developer
extends Ember by returning immutable generated packages, not by implementing an
Ember Backend.

#### Real mixed-compiler composition proof

`preparedsource/testdata/mixedcompiler` answers the next question with the
existing mechanisms rather than another abstraction. Its application-owned
compile command invokes the internal Sprig compiler and the independently
versioned Seed compiler by their concrete APIs, checks both canonical
evaluators, requests one standalone Sprig package and one Seed Set, and places
them in one canonical two-mount Layout. Reversing the mount inputs preserves
the Layout identity. The generated handler statically imports both packages,
accepts one context with distinct language limits, calls Sprig before Seed,
and returns a closed application record containing the two concrete result
types:

```text
Sprig checked sources -> Sprig Project -> counter Set --+
                                                        +-> application Layout
Seed checked source   -> Seed Program  -> score Set ----+          |
                                                                   v
                                                     explicit typed handler
                                                                   |
                                                     ordinary static Go app
```

There is no common guest value, ABI descriptor, cross-language package graph,
serialization, reflection, registry, language switch, or generated runtime
adapter. Sprig domain rejection returns before Seed is entered; an observed
context panics if that ordering changes. A Seed domain result follows a
successful Sprig call. Pre-cancellation, independent Sprig and Seed limits,
Seed-stage cancellation, nil context, input nonmutation, and detached output
are checked. The positive mixed path returns Sprig total `4` and Seed value
`6` with zero allocations.

Repeated compilation pins Sprig Project
`d06d57d8034c95e7619a3781a4fb32475d71ef97fed3bbcfb8807053c2f0374f`,
Sprig package
`e92a97f8e5fc66e6531402275e1e3f02a9e869df10314603ecc4a7e1c32fb42b`,
Sprig Set
`fae4349553ee6e4d68731e64ca23d1c44142ded3e9721fbcd2edfa3dae4f3bc2`,
Seed Program and Set identities recorded above, and mixed Layout
`16e6d08c3a1ce7b1023530845f48590b469e51b021400a27df7f9176fea44b3a`.
The generated Sprig source is 3,402 bytes with SHA-256
`bbda8b676e490ad73ed273600ca736681178fc793fc4f631ae7bad3689b1ecec`;
the 1,216-byte application handler has SHA-256
`93e8291be58f31ad94af156c5ff9be955de55e28564af94b29cc9d83ce0175f7`.

The compile graph contains exactly the two concrete compilers,
`preparedsource`, and the application command. After materialization, the
copied command and Seed compiler are deleted. With fresh offline caches the
remaining six-file module has no dependency outside its own handler, counter,
and score packages; no compiler, Ember, delivery, registry, or generic-value
name survives in build information or linked symbols. The trimmed local binary
is 2,582,706 bytes with SHA-256
`f3ec8277482959424c443605484b470bb5a729f687e33e89250c8e21ab45880a`,
and all six supported OS/ISA targets build with cgo disabled.

Seven order-rotated local benchmark pairs give a 19.270 ns mixed-handler median
versus 18.820 ns for the same ordered concrete generated calls, or 1.024x;
both paths report zero bytes and zero allocations. This is below the explicit
1.05x composition gate, but remains dirty-host Go 1.26.4 Darwin/ARM64
architecture evidence rather than a retained performance claim.

The result removes another prospective layer. A shared package graph cannot
help because the two language packages do not call each other; the application
owns their order and policy. A generic `Compose` API would merely parameterize
the concrete handler, erase its result types, and add a dispatch boundary. The
next legitimate shared seam is still the whole-application build and
transaction, not a compiler or invocation facade. This proof is static and
does not complete S5b or S7.

#### Bounded Ruby D1 semantic and direct-target proof

`internal/rubyproof` answers the dynamic-language architecture question before
S4 or a public package migration. Its original 2,480-byte Ruby 2.6 C4 prefix
remains byte-identical at SHA-256
`aad50c456aad030a12365e85596cdc30ac1a8d85437c9adcacb6dbeb2c9907a5`.
Target39 appends an 850-byte/51-LF C1 schedule, giving 3,330 bytes/209 LF and
SHA-256
`8f2611ea5042f09f8313c1291af5b44a5bbf8e76ae3733e0745579e8c838c168`.
It adds `Delta < Alpha`, IncludedLabel and PrependedLabel modules, `include`,
`prepend`, a method defined after prepend, and a later included-method
redefinition without changing the existing `read_label`, `read_saved`, or
`read_public` sites. `read_label` is the literal-selector form `send(:label)`;
`read_public` is `public_send(:label)` with a bounded `NoMethodError` rescue.
This distinction keeps method resolution in one Ruby-owned cell while making
call legality an explicit site policy rather than a second resolver.

`Base#saved_label` snapshots the first `Base#label` body; reopening
`Base#label` changes only Alpha's ordinary label resolution and leaves the
alias on the old body. The source then changes Base's winning label from
public to protected, private, and public again. Alpha's `public_send` rejects
protected and private before the body's `mark` effect; literal `send` still
executes private Base v2; Beta's public override remains callable; and the
public restoration permits and repairs Alpha. Removing Beta's local label
then exposes Base v2, while undefining Gamma's local label installs a terminal
negative entry rather than exposing Base. The same source retains object
identity, block capture, targeted non-local return, `RuntimeError` rescue, and
ensure ordering. Full caller-sensitive protected ordinary-call legality is
not part of this bounded proof.

This remains one concrete Ruby module, not Ruby syntax over a common semantic
core:

```text
bounded Ruby source
  -> Ruby lexer/parser/checker + sealed passive lookup image
       +-> independent canonical Ruby owner
       `-> Ruby emitter -> fixed generated owner + direct named Go targets
                             -> one preparedsource.Set
                             -> application Layout / ordinary static Go
```

The complete checked image has eight lookup nodes, five dispatch rows, eight
selectors, seventeen definitions, twenty-four ordered finite operations, two
attachments, twenty all-mask route facts, and fifty-six compare-only path
slots. The generated package does not copy the maximum selector matrix or any
route oracle: its exact admitted closure has eight nodes, five rows, two
selectors, two attachments, sixteen local-entry positions, ten effective
cells, nine definition/body targets, four initial definitions, twelve
post-construction mutations, thirteen target matches, and four sites with
eight fixed arm positions. Base v2's target is reused for Alpha, Beta after
removal, Delta before include, and the pre-definition singleton receiver rather
than duplicating executable authority. The alias
has its own definition/slot identity but its target calls the statically named
Base-v1 body directly. The label and public sites admit Alpha and Beta; Gamma
and Delta use bounded uncached fallback. The saved site admits only Alpha.
Statically proved `hot`, `value`, `trace`, and `apply` calls remain direct named
Go instead of paying for a generic dispatch abstraction.

Canonical lookup walks topology and local entries independently of its derived
cell. Generated lookup, mutation, validation, and target selection are a
separate implementation over emitted numeric facts. A warmed `send` hit checks
only receiver dispatch/shape and scalar epoch/slot/definition/target facts,
then enters a local switch that calls a named Go body. A warmed `public_send`
adds one scalar `visibility == public` guard before that same arm path. Neither
performs a string or map lookup, ancestor walk, allocation, lock, an
interface/function-value **method-target** call, or a compiler/runtime import.
The surrounding control path still performs explicit `context.Context` polls;
zero allocation and direct targets do not by themselves prove near-handwritten
latency.

Reopening Base stages the replacement entry, independently recomputes only the
label column, proves exactly two changed receipts (Alpha and Delta) and
sufficient nonzero epoch
space, polls cancellation, and then commits infallibly. It never traverses PIC
arms. The next Alpha call exits its stale arm before `label` or `mark` effects,
executes one canonical generated fallback, and repairs the whole arm only after
normal success. Beta's and Gamma's label cells and every saved cell remain
byte-identical.

Each visibility operation reuses that same one-column exact-before/after
transaction. Visibility is copied into the winning receipt, so only Alpha's
and Delta's cells advance when Base changes while the overriding Beta and Gamma receipts
remain byte-identical. Protected/private `public_send` rejects before method
effects and publishes no arm or statistic; private `send` succeeds; Beta's
public override remains valid; and restoring public permits normal Alpha-arm
repair. Removing Beta later changes only Beta's label cell and repairs that arm
to the already emitted Base-v2 target. Undefining Gamma changes only Gamma's
label cell to an explicit `undef` receipt and admits no target or guest effect.
Defining IncludedLabel while detached changes no cell; including it into Alpha
changes exactly Alpha and Delta; prepending an empty module into Beta publishes
only the activation bit; defining that module's method later changes exactly
Beta; and redefining IncludedLabel changes exactly Alpha and Delta. These five
transactions have `K=0/2/0/1/2` and activation masks `00 -> 01 -> 11`.
The checker and independent literal observer also cover otherwise unreachable
mask `10`; four masks are exhaustive only for this two-attachment fixture.
Cancellation at every mutation poll, epoch exhaustion, malformed visibility,
cell/arm corruption, and a host abort after a target effect publish no partial
mutation or repair. Guest raise/ensure behavior remains covered by the
canonical and generated bounded program; arbitrary raising `label` bodies are
outside this admitted emission shape.

Canonical Ruby, generated Go, and the pinned CRuby oracle agree exactly on the
unchanged C4 result followed by the new topology result:

```text
true|false|1|3|4|1|3|2|2|3|4|1|2|5|5|2|3|2|2|14|106|66776777124532|88887|99
2|11|2|22|13|1|713726
```

The two lines plus final newline have SHA-256
`01c3e3bbbf334e42fe3940a0ef2c8d2e25ec0891b23ffc36a3bc9020ce066ea0`.

The complete generated site receipt is:

```text
Hits=6 ColdAdmissions=5 StaleMisses=5 Repairs=5
UncachedFallbacks=5 Admissions=5 Evictions=0 OccupiedArms=5
```

For generated package `generated`, the current Program identity is
`e318f17162550f67ef6f59d6aee5d069685a82e4c4bc6b003bd00ebc62893a5a`,
the Set identity is
`e0a6719f3022bab101ec9ae6fb252f9652161f4876a39bc4c05f62c39274bfda`,
and the 68,743-byte/2,065-LF generated source SHA-256 is
`93a2e374c8b2be08eb3033b69c0ee9a1d17fc6c7e557a88f0ecaff4374fee2a3`.
The generated file also carries the older projection and unwind proof, so its
total lines are not a lookup-mechanism budget. C4/Target39 fact switches,
sites, and independent mutation owner are non-contiguous, so
acceptance records the exact finite closure and generated-byte receipt instead
of misreporting the old target-34 contiguous line span. Same-package layout
tests pin
entry/receipt/cell/arm/lookup/route-workspace/site/object/control/stat/Engine
sizes at 12/16/24/32/376/36/64/32/56/64/688 bytes on the supported 64-bit proof
target. Target39 adds 17,975 bytes and 395 lines over the historical Target37
artifact. The three sites and six arms remain unchanged; the lookup and Engine
growth is cold owner-local topology state, while route scratch is local to each
generated resolution.

The generated package has no mutable package variables, maps, dynamic owner
fields, Ember/compiler/delivery dependency, old string/source-epoch authority,
flattened route, or route-oracle table. Construction is cold and owner-local;
close clears lookup, topology, object, counter, and statistic state;
checkpoints expose no dispatch, shape, slot, definition, epoch, target,
activation, route, or arm identity. A separately built fresh
application retains only concrete generated code and cross-builds all six
supported OS/ISA targets with cgo disabled.

The local differential still pins `/usr/bin/ruby` as
`ruby|2.6.10|universal.arm64e-darwin24`, executable SHA-256
`0c6118f2b4a9fe448d51eba2e79c81342ae1e111f015d58dce5c11740f4dfacd`.
This is one attributable semantic frontier, not RubySpec or full Ruby.

The historical R0 monomorphic `Hot` calibration measured 13.67x its canonical
evaluator, 0.900x equivalent explicit Go, and zero allocations. D1 proves the
intended fixed-cell/direct-target shape and zero warmed allocations. Its C4
baseline generic send/recovery function was 2,944 bytes and non-inlineable,
which rejected treating scalar source shape as a latency proxy.

Target37 introduced three one-charge wrappers, three
selector/site/mode-specialized entered leaves, and one production generated
cold helper without changing resolution authority. Target39 retains that
architecture while keeping attachment state out of all warmed leaves. On Go
1.26.4 Darwin/ARM64, the current label/saved/public wrappers are 288/288/400
bytes; their warmed leaves are 720/672/816 bytes, 2,208 bytes in aggregate; the
sole cold helper is 2,480 bytes. The warmed route has no topology lookup,
generic validation, target-dispatch, or recovery call and directly calls named
method bodies. The generic test lane is a reconstruction against current facts
and helpers, not the byte-identical pre-specialization artifact.

Two separate cgo-off single-P Apple M1 captures reran the existing
Target37-named harness against generated SHA-256
`93a2e374c8b2be08eb3033b69c0ee9a1d17fc6c7e557a88f0ecaff4374fee2a3`.
Each ran seven oppositely rotated rounds of eight pre-repaired alternating
Alpha/Beta hits for `send` and `public_send`, with exact
effects/control/statistics and zero allocations. Capture A's
specialized/generic medians were 0.607892/0.618291 and
specialized/equivalent medians 1.072247/1.039982; capture B measured
0.599669/0.592217 and 1.058290/1.059357. The raw SHA-256 values are
`b203e1c388072d7667800f5b2515f29b6069c7db3026bf10cc4313ad108287d4`
and `8ac3b73a15b03e1f163305493b728994028b1d0f4dd7d6ede173244dca3e4139`.
The current specialized warmed shape is therefore 1.62-1.69x the current
generic helper shape and at most 1.073x the equivalent lane in this bounded
workload. `docs/checks.md` owns the exact identities, historical comparison,
and gate receipts.

This is bounded local lowering evidence, not a quiet-host, lifecycle, release,
canonical-Ruby, or CRuby/YJIT result. It excludes repair/fallback,
Target39 route derivation and topology transactions, construction, owner
admission/close, build/RSS, IPC, and reload, and the acquisition command/load
environment is documentary rather than authenticated.
Therefore open-world Ruby performance evidence remains **7.2/10**. Promotion
still requires the independently acquired standalone R1 observer, separate
recovery costs, and retained code-size, heap/RSS, build, reload, and
CRuby/YJIT-relative evidence. The old direct-`Hot` command is only a
calibration. Do not change the resolution authority or add a shared runtime
merely to optimize this code shape.

#### Intern only exact generated leaf templates, conditionally

Target38 bounds the code-size side of strict specialization without changing
the production emitter. A disposable observer repeated the exact three-site
C4 mix at 3, 24, 96, and 384 total sites and compared the current one-leaf-per-
site form with shared entered leaves and then array-backed site storage. Every
variant kept one wrapper and one owner-local `rubySite` per semantic site, one
generated cold authority, direct named body targets, exact whole-site
validation, normal-success-only repair, one control charge, and complete
owner/reload erasure.

The architectural eligibility rule is deliberately stronger and simpler than
a hand-written selector/mode signature. Two entered leaves may share only when
their generated bodies are alpha-equivalent after replacing **only** the
per-site state pointer and source-site attribution. Every semantic constant and
operation remains part of the compared template: selector, call/visibility
mode, evaluation and error order, arm width and empty rules, allowed receipt
tuples, guards, direct target cases and named body calls, validation,
miss/repair/statistics policy, and cold handoff. A deterministic hash may find
candidates, but exact structural equality must confirm the match. Singleton
families retain their strict leaves. There is no runtime signature, plan/table
lookup, selector branch, function target, shared mutable site, or second
semantic authority.

At the frozen 96-site decision point, exact-template sharing reduced generated
source 57.52%, the export archive 37.46%, total text 49.01%, site text 66.18%,
and the linked binary 7.12%. The 3-site no-reuse variant had equal binary size,
0.23% more total text, and 0.52% more archive. Strict site text remained
approximately linear rather than accelerating superlinearly. Array storage
then saved only another 2.39% archive, 0% site text, and 0.78% binary at 96
sites, so it does not pay for another generated layout/planner policy.
`docs/checks.md` owns the complete metric table, identities, source hashes,
symbol/compiler-shape receipts, and observer limitations.

Production remains the current strict form. Its three leaves are semantically
different and therefore have no eligible pair, while the noisy host could not
admit the required at-most 2% warmed-hit regression gate for passing a site
pointer into a shared leaf. Exact-template interning is the selected
**conditional** future mechanism, not implemented production behavior. It may
be promoted only after an identity-bound quiet capture preserves direct named
targets, zero allocation and escape, the current control/semantic/lifetime
suite, warmed-hit regression at most 2%, and specialized/equivalent at most
1.15. This bounded multiplicity result does not establish representative Ruby
program scaling, CRuby/YJIT-relative speed, repair/fallback latency, worker
reload, or a reason to introduce a cross-language compiler/runtime seam.

#### Select a Ruby-private target-ID PIC

The selected R1 representation is a fixed two-arm, Ruby-private PIC whose
mutable arms contain only bounded scalar guard facts and a generation-local
target ID. The immutable generated package owns the admitted target inventory,
named method bodies, and a call-site-local `switch` whose cases directly call
those bodies. This is not a registry or interpreter: a hit performs unrolled
class/shape/epoch comparisons, selects one numeric ID, and makes one statically
named method-body call after specialized whole-site validation. It performs no
method-plan scan, method-name string dispatch,
function-value or interface call, boxing, allocation, shared lookup, or
delivery/build/reload operation.

The logical Ruby-owned validity key is the receiver's dispatch-class identity,
its immutable non-reused representation-shape identity, the call-site selector
and resolved method slot, and an effective lookup epoch. The selector is a
generated call-site constant; an arm may retain the class-specific slot/index
returned by canonical lookup but need not compare the selector again. A target
ID denotes one admitted slot/definition/body specialization inside one code
generation; it is payload, not a substitute for invalidation. The effective
lookup epoch must change whenever a supported method-table, ancestor, module,
singleton, alias/remove/undef, visibility, or equivalent mutation could change
resolution or call legality. Sites whose legality also depends on an unmodelled
dynamic caller context remain canonical rather than weakening this guard.

Receiver, arguments, and block are evaluated once in Ruby order before the
probe, and the common control boundary is charged exactly once. A stale or
unseen receiver exits before speculative callee effects and uses canonical
Ruby lookup and execution. Only normal successful completion may admit or
repair an arm, after revalidating the dispatch class and effective epoch in
case the invoked method changed lookup state. A stale matching arm repairs only
itself; Alpha remains valid when Beta changes. After the two source-selected
arms are occupied, an unseen Gamma neither grows nor evicts the cache. A
resolved definition without an emitted compatible target remains correct,
uncached canonical fallback.

An occupied arm with a zero, out-of-range, or class/slot/definition-incompatible
target ID is different from a valid unsupported definition. It is owner
corruption and must poison before guest effects; it must never fall through to
canonical lookup or silently repair itself. Redefinition and effective-epoch
advance run under the same owner admission as calls and advance monotonically
with checked exhaustion. When the bounded representation uses host integers,
admission must prove every effect and return intermediate for the complete
workload. Direct and fallback paths may not disagree through host overflow or
leave a partial effect that Ruby integer semantics would not produce.

The immutable target inventory is owner-neutral generation state; the arms,
epochs, shapes, statistics, and Ruby objects belong to one Ruby owner. No Ruby
object, function value, code pointer, epoch, target ID, or cache enters an
application checkpoint or crosses worker preparation, activation, retirement,
or reload. A candidate generation constructs fresh cold arms and discards them
with its owner. Luau, Sprig, Seed, `preparedsource`, the builder, and the worker
therefore pay no Ruby PIC storage, dispatch, invalidation, or cleanup cost.

This representation rates **9.6/10 as a design route**, not as measured
performance. Inline per-arm bodies rate **8.6/10**: they can be fastest for a
frozen monomorphic site but duplicate call-site-by-target code and converge on
an explicit target selector when a stale arm must retarget. Function values or
interfaces rate **4.0/10** because indirect calls weaken inlining and create
boxing, escape, and lifetime risks. Global/megamorphic caches or a shared JIT
layer rate **0.5/10** because they add synchronization, retention, unbounded
state, semantic leakage, or forbidden executable-memory machinery. Reconsider
inline bodies only if a real target-ID observer fails the 1.15x equivalent-Go
gate while a bounded inline version passes without call-site-by-target growth.
Until then no A/B micro-discriminator is warranted, and the open-world
performance evidence remains 7.2/10.

#### Reject the first direct-target observer, retain the representation

The first implementation of that observer deliberately remained disposable.
Its 1,292-byte Ruby source had SHA-256
`6f87554652f95cd6b0cbb665f9d073251a76cb2e81c75e80c29e776dc3e5cb2e`
and the finite canonical, generated, and independently owned handwritten lanes
reached `true|false|41|42|44|41|43|43|44|2|8|8`. Before any performance or
CRuby capture, however, its focused preflight failed generated freshness, an
exact source-fact receipt, and the negative schedule boundary. The observer
and stale generated files had SHA-256
`6d0197a07c457a6e61e48c47153ac1450f9aef2f3abce52020c2975ac483518b`
and `5e4fd82bfc8810fa965508b9a81bb6742a6959078d202eeebcc449cee6454cbd`.

Primary inspection and two independent all-line reviews rejected rather than
tuned the candidate. The checked-in target bodies were not the current emitter
output. An occupied invalid target silently recovered, while a valid target ID
belonging to another class could directly execute the wrong body. The
proof-local fallback consumed already selected scalar facts instead of doing
canonical Ruby-owned selector/slot lookup. Operand, block, targeted-return,
ensure, control, integer, epoch-owner, checkpoint, and reload claims were not
proved. The nominal two captures were also two order arrays in one test
process: they retained no independent raw identities, timed effect checksum,
non-hit allocation vector, or code/build/RSS/reload costs. A flattering ratio
could therefore have authenticated neither the artifact nor its work.

The surviving claim is deliberately narrow. For one uncorrupted sequential
flat-class owner, the candidate demonstrated the desired static hit shape: two
five-scalar arms, one local numeric switch, three named direct bodies, no hit
plan scan, bounded Alpha/Beta admission, a stale-Beta pre-target miss followed
by post-success whole-arm revalidation and repair, Alpha preservation, and
uncached Gamma. Review rates that mechanism shape 7.0/10, while measurement
validity is 1.5/10, R+2 semantic validity 2.4/10, and R+4 lifetime validity
3.0/10. These ratings do not change the 9.6/10 representation design or the
7.2/10 open-world performance evidence. Inline-body A does not reopen because
B never reached a valid performance comparison.

The next observer must separate three proof roles even if all remain private
test tooling: a source-bound emitter whose exact bytes and facts pass freshness
and corruption preflight; separately built canonical, generated, and
handwritten acquisition lanes that emit closed raw receipts; and a comparison
role that consumes those receipts without executing any lane. Capture lanes
must have distinct identity-bound runs, and `PASS` is written only after exact
timed effects, counters, allocations, environment, and cost records validate.
This is evidence architecture, not a public compiler kit, common runtime, or
new language ABI.

#### Select a standalone language-owned evidence fixture

The selected physical topology is one standalone Ruby evidence-fixture module,
not a repository-wide language runner and not one spawning Go test. The Ruby
compiler owner authenticates exact source, checked facts, semantic schedule,
corruption corpus, emitter identity, and generated bytes before acquisition is
enabled. The fixture then builds canonical, generated, and independently
handwritten acquisition commands in separate roots and caches. A fourth
command compares already closed receipts; it imports and executes none of the
three lanes. A fixture-private lifecycle driver may materialize, build, launch,
envelope, wait, and clean up, but it may not interpret Ruby facts, execute the
comparison, or decide the result.

This choice integrates the strongest parts of two alternatives without moving
their ownership boundary. A repository script offered the best bounded
supervision mechanics, so the fixture driver adopts fresh per-lane/per-round
processes, frozen forward/reverse order, exclusive output roots, `INCOMPLETE`
and write-last completion markers, length-and-digest-framed canonical TSV,
external executable/toolchain/environment authentication, bounded stdout,
stderr and time, exact process settlement, and receipt-only comparison. Those
are effect and evidence mechanics inside the fixture; source materialization,
language semantics, and verdict policy do not move into shell or a shared
runner.

No measured behavior is shared between acquisition lanes. In particular, the
generated and handwritten binaries share no owner, guard, lookup, fallback,
target, effect, statistic, control, or receipt-encoding implementation. A lane
cannot authenticate its own executable or environment and starts without its
counterpart's receipt. The lifecycle driver attaches those external identities;
the comparator independently rejects noncanonical framing, truncation,
duplicates, unknown or missing required fields, trailing bytes, swapped roles,
identity mismatch, incomplete settlement, and excess bounds. Shared inputs are
limited to immutable source bytes, a frozen language-owned manifest and
schedule, and the documented receipt grammar.

Each accepted timed sample uses a fresh process and owner. The hot interval is
pre-warmed Alpha/Beta hits only; exact return/effect checksums and counter
deltas make dead or partial work observable. Cold construction/admission,
stale-Beta repair, Gamma fallback, allocations, code and executable size,
build time, launch-to-ready, RSS, owner-generation reopen, and cleanup remain
separate raw observations. Owner-generation reopen is not reported as worker
reload. Two complete, oppositely rotated captures must independently clear the
same frozen gates; invalid provenance, semantics, environment, contamination,
timeout, or cleanup produces incomplete evidence rather than a mechanism
failure.

The topology reuses only a private convention until a second real language
proves the same complete invariant. Ruby keeps its cases, target inventory,
epochs, unwind, canonical recovery, integer rules, owner lifetime, and
thresholds. Luau keeps VM/Machine/prepared differential semantics; Sprig and
Seed keep their typed facts and limits. Applications keep checkpoint, result,
effect, and reload schemas. No common semantic test suite, guest value,
compiler interface, Backend, Runtime, registry, or evidence ABI is introduced.

Three independent evaluations rate the standalone fixture **9.6/10 for source
and artifact trust**, **9.7/10 for semantic validity**, **9.5/10 for lifetime
validity**, **9.6/10 for extensibility**, and about **7.8/10 for setup economy**.
The repository-script topology rates **9.7/10 for acquisition mechanics** but
only **8.4/10 as the owning topology** because it can absorb source and semantic
policy. One spawning Go test rates about **8.0/10**; the rejected in-process
benchmark rates **1.5/10 for evidence validity** despite its low setup cost.

Before any timing implementation, one disposable no-timing skeleton must prove
fresh source/fact/generated-byte identity, the complete corrupt-target and
schedule-negative corpus, disjoint dependency and symbol graphs, deterministic
closed receipts, external binary/environment envelopes, parser rejection of
every malformed class, bounded descendant settlement, and retry-safe cleanup.
It must also prove that the real canonical lane can be built without exporting
a generic Ruby runtime seam or copying measured semantics into a shared
harness. Failure of artifact-to-binary provenance, canonical-lane isolation,
or cleanup rejects this topology before performance work. No capture command
or performance promotion exists until that skeleton passes.

#### Reject the spawning-test skeleton, not the standalone topology

The first no-timing implementation attempt is rejected. It contained 1,914
lines across two root tests and one nested fixture module, but its lifecycle
driver was `TestEvidenceDriver`: one Go test built, launched, enveloped, and
settled all three lanes. That is the explicitly rejected spawning-test
topology, not the selected standalone driver. One permitted repair fixed the
initial private-import placement and a target-definition check; the next run
stopped before producing any role binary because the canonical command tried
to slice a non-addressable array return. The repair ceiling prevents treating
the compile error as an invitation to tune the candidate.

Primary and two independent all-line reviews found deeper invalidators. The
canonical lane executed two `label` calls while its receipt claimed the
four-call generated/handwritten schedule; effects, fallback counts, and close
state were hard-coded or self-reported. Later lanes inherited the capture-root
environment and could see earlier receipts and binaries. The environment and
argv digests did not describe the processes actually launched, the compare
binary was unauthenticated, and most external identities were accepted merely
for being nonempty. `exec.CommandContext` owned no descendant process tree,
`cleaned=true` preceded binary/work-root removal, and final cleanup failure was
ignored. Impossible definition/epoch pairs could also recover and execute an
effect instead of failing before effects.

Consequently P+1 through P+5 all fail and no timing is authorized. This
concrete implementation rates **1.5/10 for evidence validity**, **2.5/10 for
semantic validity**, **1.0/10 for lifetime validity**, and **3.0/10 for
implementation economy**. Exact source/generated freshness, bounded framing,
and a few corrupt-target cases are useful lessons, not a partial pass.

The selected standalone topology remains **provisional and unimplemented** at
9.6/10; this attempt supplies no positive evidence for it and does not falsify
it because it instantiated another topology. A future skeleton must use a
separately built lifecycle command and authenticated compare binary, scrub and
bind one isolated environment/root per lane, make counterpart artifacts
unreachable, derive schedule/effect records from observed language-owned work,
own and settle the complete process tree, and verify cleanup externally before
writing completion. If that requires a public/shared runtime API, source
rewriting, ambient filesystem visibility, or comparator-owned Ruby semantics,
reject the standalone topology rather than weakening the gate.

#### Defer the observer until it can select production policy

Do not implement another Ruby performance observer yet. The target-ID arms,
epochs, targets, and inventories are Ruby-private, generation-local,
checkpoint-excluded, and deliberately replaceable. A valid flat-class timing
result could raise the 7.2/10 evidence score, but it would not currently choose
a public API, checkpoint schema, generated ABI, S4/S5b design, worker contract,
or production Ruby representation. Evidence infrastructure is therefore not
the next architecture dependency.

The source/module part of a genuine standalone fixture is feasible without a
new public surface. A nested module beneath the `github.com/besmpl/ember`
import prefix may import `internal/rubyproof`, and the existing private
`Compile`, `Program.Identity`, `Program.NewRuntime`, `Runtime.Run`, and
`Runtime.Close` surface is sufficient for a complete-program canonical lane.
The current `ProofSource`, however, contains only one call before and one call
after redefinition. Adding source-owned cold/hit calls appears possible but is
only an inference about this bounded proof; it is not a production polymorphic
site with ancestor, shape, visibility, or effective-epoch behavior.

The lifecycle part is the disproportionate cost. A truthful A2 fixture is now
estimated at 22-26 files, roughly 2.4-3.6 K handwritten lines plus generated
code, and five authenticated binaries. Its ordinary lifecycle command would
need a new reviewed process-execution class. The private `preparedworker`
launcher is not a reusable arbitrary-command supervisor: it is EPW2-specific,
its Unix owner kills a process group without positively censusing arbitrary
descendants, its Windows job is attached after `Start`, and its Darwin
parent-death path requires a cooperative lease watcher. Those choices are
adequate for the controlled worker contract but do not authenticate arbitrary
Go-tool descendants. A repository-wide B orchestrator needs essentially the
same gated, platform-specific ownership and prematurely creates an
`ExecutionSpec`/provenance ABI for one evidence user.

Current-value ratings, where higher overhead and economy scores mean lower
cost, are:

| Route | Decision value now | Potential validity | Low overhead | Extensibility | Economy now |
| --- | ---: | ---: | ---: | ---: | ---: |
| A2 standalone private module | 3.5/10 | 9.6/10 | 5.0/10 | 8.8/10 | 4.5/10 |
| B repository orchestrator | 3.0/10 | 9.3/10 | 5.5/10 | 8.2/10 | 4.0/10 |
| Defer | **9.6/10** | 0 new evidence | **10.0/10** | **9.7/10** | **10.0/10** |

The zero in the defer row is deliberate: deferral produces no performance
evidence. It preserves the narrow claim rather than converting infrastructure
complexity into apparent confidence. A2 remains the preferred ownership
topology if the observer later becomes decision-valued; B is only a fallback
when Ruby semantics are ready and lifecycle mechanics alone remain blocked.

Actual capture reopens only after a retained R1-oriented slice supplies all of
the following:

1. a named CRuby/RubySpec frontier including flat classes and at least one
   supported ancestor/module or singleton lookup mutation, with explicit
   visibility and alias/remove/undef policy;
2. a production-owned polymorphic site with real class and shape identities,
   canonical selector/slot lookup, effective epochs, and generated direct
   targets;
3. exact receiver, argument, block, fallback, limit, cancellation, raise,
   targeted-unwind, corruption, and epoch-exhaustion behavior before timing;
4. an application projection proving that objects, functions, targets, epochs,
   and cache arms never cross generations;
5. a real V1/V2 prepare, restore, quiescent activation, retirement, and
   retryable-close lifecycle, or an explicit owner-reopen-only claim; and
6. enough representative sites to make target-ID indirection versus inline
   body code growth a remaining material choice.

A separately reviewed portable process authority may reduce later observer
cost, but it cannot substitute for that semantic milestone. If process
economics alone must be falsified first, authorize a disposable process-only
spike with an explicit small cap and real Darwin, Linux, and Windows descendant
tests; do not extract a shared process package from a single passing fixture.
Until a reopen condition holds, there is intentionally no A2/B implementation,
capture command, CRuby timing, or performance promotion. The target-ID design
remains 9.6/10, open-world evidence remains 7.2/10, and no polymorphic or
revolutionary-Ruby claim is earned.

#### Separate performance potential from retained release proof

Ember's performance direction is unusually strong, but the evidence layers
must not be collapsed into one claim. Ordinary generated Go and
language-owned hot state keep the shared delivery/reload mechanism out of
steady guest execution. That is the architectural reason to pursue this
design; it is not itself a released Lua or Ruby performance result.

| Evidence layer | Current attributable result | What it supports | What it does not support |
| --- | --- | --- | --- |
| Unprepared Luau runtime | Two retained all-37 captures are 5.708984x and 5.718731x slower than pinned Luau 0.728. | The existing VM is the exact fallback and differential path, not the performance destination. | Luau parity through the unprepared runtime. |
| Private prepared Luau guest batch | Two clean all-37 captures match every result; the worst median is 0.186646x and the worst p90 is 0.192705x pinned Luau. | Typed-Go AOT has enough guest-throughput headroom to justify the architecture. | Public build, `READY`, transport, quiescence, retirement, swap, or resource behavior. |
| Public prepared worker | The earlier exact-revision P1 capture reached 0.198x worst worker/Luau p90, 1.080x host slope, and 41.9 microsecond exchange p99. The current candidate rates 8.7/10 as implementation, while retained S0 proof remains 4.0/10. | The whole-application transaction design is plausible and has measured headroom. | Certification of the current dirty candidate or a released lifecycle; P3-P5 still need one clean retained revision and target-native receipts. |
| Ruby R0 | The bounded monomorphic generated send measured 13.67x its canonical evaluator, 0.900x equivalent explicit Go, and zero hot allocations. | A Ruby-owned guarded direct-Go path need not pay for a shared runtime or backend. | CRuby/YJIT-relative speed, inherited/open-world dispatch, broad Ruby, production heap behavior, or process reload. |
| Ruby D1 | The bounded Base/Alpha/Beta/Gamma/Delta nucleus now has a sealed passive lookup image, independent canonical/generated owners, ten generated eager cells, four fixed direct-target sites, snapshot alias/remove/undef transitions, public/protected/private visibility mutation, literal `send`/`public_send`, one attachment-backed include/prepend schedule, and one preassigned allocation-site singleton row with exact selective repair and zero warmed allocations. | One resolution authority can retain direct named targets while Ruby-owned call-site policy rejects non-public calls before effects; topology stays in immutable descriptors plus owner-local activation bits and never enters a warmed leaf; a unique immutable dispatch can represent one statically proved receiver without an object registry. | Fair polymorphic timing, CRuby/YJIT-relative speed, general singleton/eigenclass allocation, full caller-sensitive protected ordinary calls, production heap behavior, or process reload. |

The architecture therefore remains **9.8/10** and the selected steady-hit
shape **9.7/10**, while current revolutionary/release proof remains about
**4/10**. The first scores evaluate ownership and expected hot-path structure;
the last evaluates retained public evidence. Do not average them into a
readiness number. A revolutionary claim requires both production semantic
breadth and clean independent public lifecycle/performance receipts.

The next-work comparison uses decision ratings, not benchmark scores:

| Route | Decision value | Evidence lift | Feasibility / authority | Semantic breadth | Lifecycle / reload | Third-language leverage | Economy |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| A. Finish public prepared-Luau S0/P3-P5 | 9.8/10 | **10.0/10** | 4.5/10 | **9.4/10** | **10.0/10** | **9.4/10** | 5.0/10 |
| **B. Retain D1 and deepen one named Ruby frontier** | **9.8/10** | 8.8/10 | **8.8/10** | 8.5/10 | 6.5/10 | 9.2/10 | **8.4/10** |
| C. Build the open-world Ruby observer now | 2.5/10 | 2.5/10 | 3.0/10 | 3.0/10 | 2.5/10 | 3.5/10 | 2.0/10 |
| D. Wait | 1.0/10 | 0.0/10 | **10.0/10** | 0.0/10 | 1.0/10 | 3.0/10 | **10.0/10** |

Route **B** has now completed its bounded Base/Alpha/Beta/Gamma nucleus: it
proves selective effective epochs, pre-effect stale exit, successful
Alpha-only repair, corruption rejection, transactional mutation, complete
close, and cold reconstruction without changing the worker, builder, shared
delivery seam, or another language. The next B move is retention-quality
verification and then one named RubySpec category, not a larger generic core.
A remains release-critical and preempts B only when one final candidate can
immediately be frozen on a clean retained revision and all physical
ARM64/x86-64 plus target-native lifecycle jobs can actually be dispatched. C
stays deferred until the broader semantic frontier makes target-ID versus
inline-body policy the remaining material choice; D adds no evidence.

This implementation promotes no performance score. If deeper D1 work needs a
shared operational resolver, checkpointed or cross-generation cache identity,
process-worker participation, fallible mutation commit, or dynamic hit-time
lookup, reject that expansion rather than widening the architecture. A fair
Ruby observer comes only after the named semantic frontier and must use
separately built, independently identified lanes with equivalent work;
official benchmark or release claims remain calibration, never an Ember
ratio.

#### Retain lookup/dispatch before the production heap or broader projection

The current bounded Ruby architecture slice is **D1: lookup and dispatch
first**.
Do not start with H1's production allocator/GC and do not extend P1's
application projection first. D1 has replaced the generated R0 string/epoch
path with numeric checked facts, selective owner-local cells, and a direct
target site. A production heap can therefore grow around an explicit Ruby
invalidation authority rather than proof-wide epochs. C0 and RP0 already prove
that detached application records can exclude private language state, while
S0/S4 cannot establish Ruby lookup semantics. Expanding this same Ruby-private
boundary remains the smallest next semantic route without changing
`preparedsource`, the builder, the worker, or a public/shared API.

The implementation nucleus is intentionally smaller than the later
performance-observer milestone. It first admits superclass syntax, inherited
lookup, and redefinition of a winning ancestor method at one real polymorphic
site. The checked Ruby program must distinguish these generation-local facts:

- selector ID: the method name selected by the source call site;
- local method-slot ID: the stable table position owned by one class or module;
- definition ID: one installed body/version, including aliases when admitted;
- call-site ID and dispatch-class ID;
- immutable shape ID plus numeric instance-field slots required by a direct
  target;
- effective lookup epoch for one dispatch class and selector; and
- target ID for one compatible named generated body.

Canonical lookup searches the dispatch class and its admitted ancestors in
Ruby order and returns the owning class/module, slot, definition, effective
epoch, and executable method. A mutation checks epoch exhaustion before
publication and advances only resolutions whose effective receipt changes,
including negative outcome, visibility, or admitted call-legality changes. A
subclass override therefore stays valid when a farther ancestor is redefined.
The generated hit remains an unrolled scalar guard followed by a site-local
numeric target switch. A stale or unseen arm exits before callee effects,
executes canonical lookup and behavior, and publishes a complete arm only
after normal success and whole-arm revalidation. Impossible occupied-arm
combinations poison before guest effects; a valid definition without an
emitted target stays on correct bounded fallback.

D1 includes only the shallow representation fact needed to make those direct
targets honest: immutable owner-local shapes and slot-backed instance fields.
Objects may still use bounded Go-pointer allocation and whole-owner teardown.
This makes no production allocator, tracing GC, compaction, weak-reference,
pointer-stability, arbitrary dynamic-ivar, or heap-projection claim. Those H1
decisions follow lookup because target guards determine which identity and
rooting invariants the heap must preserve.

All lookup graphs, objects, singleton structures, shapes, slots, definitions,
epochs, targets, PIC arms, frames, statistics, and code references belong to
one Ruby owner and never enter an application checkpoint. A fresh owner
reconstructs them with cold caches from validated application scalars and
graph tags. This first proof is owner reopen, not worker reload. It must retain
single admission, busy close, poison-on-host-abort/corruption, checked
pre-mutation exhaustion, idempotent close, and exact cancellation, limit,
raise, ensure, and targeted-return behavior around canonical and generated
dispatch.

The full named behavioral frontier is pinned to Ruby 2.6.10, not RubySpec
`master`. The Ruby Spec Suite README maps Ruby 2.6.10 to commit
[`aaf998fb8c92c4e63ad423a2e7ca6e6921818c6e`](https://github.com/ruby/spec/commit/aaf998fb8c92c4e63ad423a2e7ca6e6921818c6e),
while current `master` describes Ruby 3.3 and newer. After the inheritance
nucleus, deepen the same Ruby-private lookup graph through these attributable
categories rather than one giant combined change:

1. included/prepended-module mutation after a warmed dispatch, including a
   method defined later in the prepended module;
2. receiver-specific `define_singleton_method` mutation;
3. distinct `alias_method`, `remove_method`, and `undef_method` policies; and
4. visibility-sensitive explicit dispatch, including `public_send` rejection
   of private/protected methods.

Those categories are authenticated by the pinned RubySpec
[`prepend`](https://github.com/ruby/spec/blob/aaf998fb8c92c4e63ad423a2e7ca6e6921818c6e/core/module/prepend_spec.rb),
[`define_singleton_method`](https://github.com/ruby/spec/blob/aaf998fb8c92c4e63ad423a2e7ca6e6921818c6e/core/kernel/define_singleton_method_spec.rb),
[`alias_method`](https://github.com/ruby/spec/blob/aaf998fb8c92c4e63ad423a2e7ca6e6921818c6e/core/module/alias_method_spec.rb),
[`remove_method`](https://github.com/ruby/spec/blob/aaf998fb8c92c4e63ad423a2e7ca6e6921818c6e/core/module/remove_method_spec.rb),
[`undef_method`](https://github.com/ruby/spec/blob/aaf998fb8c92c4e63ad423a2e7ca6e6921818c6e/core/module/undef_method_spec.rb),
and
[`public_send`](https://github.com/ruby/spec/blob/aaf998fb8c92c4e63ad423a2e7ca6e6921818c6e/core/kernel/public_send_spec.rb)
files plus the official CRuby `v2_6_10` release. Refinements,
`method_missing`, broad reflection, general `super`, `ObjectSpace`, and
arbitrary metaprogramming remain outside this frontier unless separately
admitted.

Categories 1 through 4 are now implemented as bounded C1, C2, C3, and C4
slices. Target41 implements the frozen Target40 singleton contract below; it
does not generalize that allocation-site proof. The earlier C3 review
selected local alias/remove/undef because it exercised the existing entry
transaction without topology or object identity. Target 36 then compared C1
module/prepend, C2 singleton mutation, C4
visibility/`public_send`, and an L0 clean prepared-Luau lifecycle receipt with
three independent `gpt-5.6-sol` high reviews:

- source/lowering selected C4 and rated architecture fit **9.5/10**, semantic
  value **9.8/10**, performance preservation **8.8/10**, economy **8.7/10**,
  and implementation risk **6.1/10** where higher is worse;
- owner/lifetime selected C4 and rated semantic exactness and hit preservation
  **9.5/10**, mutation locality, lifetime simplicity, and other-language
  isolation **10.0/10**, and economy **9.0/10**; and
- the independent Ruby-oracle review preferred C1 for breadth, rating it
  semantic leverage **9.8/10**, oracle strength **9.6/10**, compatibility
  breadth **9.7/10**, and economy **7.4/10**. It rated C4 **8.8/10** semantic,
  **8.9/10** oracle, **9.2/10** breadth, **9.2/10** economy, and **2.5/10**
  ambiguity where higher is worse.

Primary integration selected C4 first by decision value and switching cost,
not by vote. C4 consumes visibility bytes already reserved in the entry and
receipt, reuses the proved one-column transaction, and needs one additional
fixed site plus one public-legality scalar guard. C1 required a topology
authority and propagation of later module definitions; C2 still required
receiver-specific ownership. Target40 later proved that one exactly-once,
preassigned allocation-site row can avoid H1 only for the bounded slice. No
exact clean current-revision L0 receipt existed, so lifecycle
certification could not preempt the bounded Ruby proof.

The C4 implementation validated that first choice without promoting timing. Literal
`send(:label)` and `public_send(:label)` lower to one numeric selector;
resolution and visibility share one cell; legality remains site-owned;
protected/private rejection occurs before target effects or cache/stat
publication; and restoration repairs through the existing recovery path.
Target39 then reopened C1 after the warmed lowering and exact-site multiplicity
questions were bounded. The same three high-reasoning reviews froze one
two-attachment schedule and selected immutable descriptors plus owner-local
activation bits over persistent paths or sealed-snapshot authority. Its exact
layouts, artifact growth, comparator, and nonclaims are recorded below. Stop
or route unsupported cases to bounded canonical execution if broader Ruby
needs dynamic hit-time names/path/caller lookup, a second resolver/cell graph,
caller-sensitive protected checks on a warmed path, checkpointed lookup
identities, or topology state in a warmed leaf.

Current ordering ratings are:

| Route | Architecture leverage | Semantic validity | Performance leverage | Lifetime/reload leverage | Third-language extensibility | Economy |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| **D1 lookup/dispatch first** | **9.8/10** | **9.4/10** | **9.5/10** | 8.4/10 | **9.1/10** | **7.2/10** |
| H1 production heap/shape first | 9.0/10 | 8.0/10 | 8.0/10 | 8.8/10 | 8.5/10 | 4.0/10 |
| P1 broader projection/lifecycle first | 7.8/10 | 7.6/10 | 3.5/10 | **9.6/10** | **9.2/10** | 4.8/10 |
| Wait for S0/S4 | 3.0/10 | 0 new evidence | 1.0/10 | 6.0/10 | 7.0/10 | **10.0/10** |

#### Target41 implements Target40's allocation-site singleton row

Target40 froze the contract and Target41 now implements it as a **bounded
architecture proof, not a general Ruby singleton feature**. Three independent
`gpt-5.6-sol` high review lanes converged on one
eager, statically preassigned singleton lookup node and dispatch row for one
checker-proved, straight-line, exactly-once `Alpha.new` allocation site. A
local pinned CRuby 2.6.10 run independently confirmed the source result. The
contract deliberately avoids a production heap only for this closed
whole-owner lifetime; it does not claim general singleton classes.

The exact source append is 571 bytes/19 LF, SHA-256
`1cdf5cfadcf34d44e8b8eadd82213eab32589b9ad64db813f17e3aba85c07256`.
It adds `read_singleton(receiver)`, one designated Alpha receiver, one ordinary
Alpha peer, a warmed call on each, and this one admitted mutation:

```ruby
c2_receiver.define_singleton_method(:label) do
  mark(4)
  44
end
```

The full source is 3,901 bytes/228 LF, SHA-256
`bf355c37216015a49a5b2079e3ff842393755a01fe7cf85d070d1f4fbbf66a52`.
Its additional seven-field oracle is
`13|13|13|44|44|13|334433`; the prior two lines stay unchanged, and all three
lines plus their final LF are 123 bytes, SHA-256
`c09d4fceec6a72f86c690145113737013b4e2ad4a408341bae7506d177ec85ab`.
This proves that definition itself has no method-body effect, invocation binds
method `self` to the receiver, and the same-class peer remains unaffected.

The lexer and parser needed no new form: the existing explicit call plus
`do`/`end` block syntax already represents it. The checker admits only a
literal `:label`, zero-parameter, zero-capture block on the one exact allocation
binding and normalize it to a public zero-arity checked method. It must reject
dynamic receivers/selectors, captures, `Proc`/`Method` arguments, repeated or
escaping allocation sites, nonlocal return, `super`, reflection, and all other
singleton forms. The checked image seals synthetic lookup node 8 above Alpha
and dispatch row 5. The designated object receives dispatch 5 at construction;
ordinary Alpha objects retain dispatch 1. Both retain Alpha shape 1 and class
identity. Dispatch is immutable, so the unique generation-local dispatch is
the receiver identity on a warmed hit; no object ID, map, or pointer guard is
admitted.

The singleton route is `[singleton] + route(Alpha, activations)`, with lengths
`3/4/3/4` for masks `00/01/10/11`. Before definition its absent local entry
falls through to IncludedLabel v2. Mutation 24 reuses the existing public
define transaction at final activation mask `11`: complete preflight, poll,
stage the label column, poll immediately before commit, then publish the local
entry, complete staged cell bank, and one new epoch without allocation or
further fallible work. Exactly the singleton label receipt changes (`K=1`).
Object dispatch/shape, topology bits, routes, ordinary rows, PIC arms, and
statistics are not mutation outputs. The dedicated two-arm site subsequently
observes one stale receiver arm, executes and revalidates the direct singleton
target, repairs publish-last, then hits; the peer arm remains byte-identical.

The implemented inventory is eight nodes, five rows, four shapes,
eight selectors, seventeen definitions, twenty-four operations, two
attachments, sixty-four canonical entries, forty canonical cells, twenty
compare-only route oracles/fifty-six path slots, and four fixed sites/eight arm
positions. The generated projection has sixteen entries, ten cells,
nine emitted definitions/targets, twelve mutations, and thirteen target
matches. The
legacy four-row C1 `K` vector remains `0/2/0/1/2`; counting the eager singleton
descendant makes the full five-row vector `0/3/0/1/3`, followed by singleton
define `K=1`. The current generated source is 79,361 bytes/2,344 LF, SHA-256
`b30db631a57127bd5f420427262cfff3970c14f6cf5c91cd382fd3b08d61bf46`.
On the observed 64-bit layout, the route workspace is 40 bytes, lookup owner
448 bytes, object 32 bytes, and Engine 824 bytes. These are exact artifact and
layout receipts, not elapsed-time or representative-program evidence.

Primary decision ratings are:

| Singleton route | Exactness | Warm hit | Transaction | No-H1 lifetime fit | Economy | Overall |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| **Eager preassigned allocation-site row** | **9.8/10** | **9.8/10** | **9.7/10** | **9.7/10** | **9.2/10** | **9.6/10** |
| Dormant row plus mutable dispatch activation | 9.7/10 | 9.3/10 | 7.8/10 | 8.4/10 | 7.6/10 | 8.4/10 |
| Dynamically assigned bounded row pool | 9.8/10 | 9.7/10 | 8.7/10 | 7.0/10 | 5.2/10 | 8.0/10 |
| Object-ID singleton overlay/map | 9.2/10 | 6.0/10 | 8.2/10 | 5.8/10 | 7.4/10 | 6.8/10 |
| Encode singleton state as an ivar shape | 6.0/10 | 9.4/10 | 7.0/10 | 7.4/10 | 8.4/10 | 6.8/10 |
| Full eigenclass heap/GC now | **10.0/10** | 8.5/10 | 8.2/10 | 4.0/10 | 3.0/10 | 5.8/10 |
| Patch only one PIC target | 3.0/10 | **9.9/10** | 3.5/10 | 4.0/10 | 8.0/10 | 4.2/10 |

The dormant route adds a composite row/object publication solely to imitate
lazy materialization that the admitted slice cannot observe. A dynamic pool is
the likely general trajectory only after H1 owns allocation, rooting,
reclamation, and reflection. Overlays create a second lookup authority and tax
hits; shapes conflate method topology with field layout; PIC-only patching has
no canonical authority. Reopen the selected contract if the allocation can
repeat or escape, another object can acquire its dispatch, eager row existence
becomes observable, a warmed leaf needs object identity or topology, a failure
partially publishes, or singleton state must cross checkpoint/reload. Close
must erase the row state, object, cells, epochs, targets, and arms; a fresh
owner starts cold with no migrated identity.

The old 1.4-2.1 K handwritten plus 350-550 generated-line estimate described
the inheritance nucleus, not C3/C4 or a production Ruby runtime. The current
artifact is instead bounded by its exact inventories and byte/hash receipts;
the broader RubySpec frontier remains deliberately separate. D1 is rejected
rather than widened if correct
inherited fallback or honest direct field targets require a production GC,
stable raw identity across generations, checkpointed methods/epochs/targets,
process-generation participation in lookup, or a public/shared guest runtime.
Only after the full semantic/lifetime milestone passes does the deferred
target-ID-versus-inline-body observer become decision-valued. C4 proves a
semantic and ownership split, not compiler code shape: the generated control
path still has explicit context polls and full site validation. This ordering
does not raise the 7.2/10 open-world design evidence or earn a
revolutionary-Ruby claim.

#### Target42 retains the direct singleton warmed leaf

Target42 asked a deliberately local question after Target41: whether the
already-repaired receiver-specific singleton leaf pays a material abstraction
tax relative to independently written explicit Go, and whether changing its
control shape could therefore simplify production. It did not reopen the Ruby
lookup authority, singleton ownership contract, or any cross-language seam.

The comparator owns separate types, validation, control, target bodies, and
effect/statistic updates. For the frozen `ReceiverAfterDefinition` state it
matches the generated receiver leaf's five context polls, two step charges,
three frame entries, full two-arm site validation before effect, direct named
receiver target, trace/control outcome, and hit publication. Contract tests
also compare receiver and peer success, cancellation polls 1-6, every
frame/step limit, dispatch/shape errors, selected-cell and selected-arm
corruption, corruption of the unused peer arm, stale recovery through the
diagnostic generic form, and unchanged pre-effect state. Whole-site validation
is therefore semantic under the current fail-closed rule: a selected-arm-only
candidate is not an optimization of the same contract.

The first retained-capture attempt used twenty rounds of three one-second
lanes per process. It failed closed before ratio analysis when one capture-A
row reached 3.317913608 seconds and later rows exhibited severe
thermal/background outliers, although all 120 rows allocated zero. The run was
kept as invalid evidence and its disposable files were cleaned; the unchanged
protocol was not repeated.

The replacement schema-3 observer narrowed retained timing to Current and
Equivalent only while leaving Generic as a nonblocking semantic diagnostic.
Each of two distinct cgo-free, `GOMAXPROCS=1`, PGO-off processes ran twenty
paired 500 ms rounds with opposite A/B lane order. Identity covered the Go
build, target, executable, generated source, comparator, benchmark, gate,
benchtime, cgo, single-P, and PGO setting. The parser rejected malformed,
reordered, allocating, short, excessive, or identity-mismatched rows. The
acceptance bound was the fifteenth ordered Current/Equivalent paired ratio,
a conservative one-sided greater than 95 percent nonparametric upper bound for
the population median, at most 1.15 in both captures.

Both captures passed on Apple M1 with zero allocation in every row. Capture A
had median ratio 1.002069, upper bound 1.045358, and Current/Equivalent medians
24.769/24.188 ns per call. Capture B had 1.000265, 1.005731, and
21.956/21.994 ns per call. Raw SHA-256 values were
`fe6897ace7058715a879e6d33e507908be4406ca53dff4f86704d28ce2cc698d`
and
`341e4565a73c15169ba66308f03ee3704fb9effcdb7058971a861a79c0a32236`;
the successful gate output was
`ade6e962287fd90c8e877241a7a9019523011af06d0d6592e16016b351ae42c5`.
Exact comparator/benchmark/gate source identities were respectively
`39f5f29f39b488e153e50e8f97ff7493a0eef14cf4f0957d8718eb63e94e14fa`,
`8d2a3af71a9ae2f027915201bf1e57e9954532d4a1f0be5ad3c38ca6d96c3f71`,
and
`b190aca2a1a7155de8fa158a52477ba0925fc00825f3e6c4007bba2c5a51e947`.

**Decision:** retain the current explicit scalar leaf, full validation, cold
repair authority, and direct named target unchanged. The current measured
shape rates 9.7/10. A singleton receipt-predicate candidate rates 9.3/10, a
fused exact cell/site/arm guard 9.0/10, split leaves 8.2/10, a generic
production scan 7.1/10, and selected-arm-only validation 4.3/10. None clears a
need to edit production. Reopen only if source identity changes, the claim
broadens, or an exact candidate produces a reproducible material gain without
per-arm, allocation, failure-order, lifetime, or code-size regression.

This is evidence only for the warmed receiver-after-definition leaf. It says
nothing about peer throughput, admission, mutation, repair cost, owner/close
or generation lifetime, open-world Ruby, general eigenclasses or captures,
CRuby/YJIT-relative speed, worker reload, or release readiness. In particular,
it does not raise the 7.2/10 open-world Ruby evidence rating and does not
justify a generic runtime, shared compiler IR, target cache, or cross-language
backend interface.

#### Target43 selects a separate bounded Ruby lifetime nucleus

Target43 answers the lifetime question deliberately excluded by Targets 41
and 42: the smallest owner-local representation able to install a singleton
method on a runtime-selected receiver and retain a mutable captured local after
the defining frame returns. The selected direction is a **Ruby-private,
nonmoving, typed generational arena with explicit roots and synchronous bounded
tracing**. It is an H1 design commitment, not yet implementation evidence or a
production/open-world Ruby heap claim.

The existing static lane remains a separate representation. Its scalar
`rubyObject`, preassigned singleton row, fixed site, full fail-before-effect
validation, and direct named target remain unchanged and admit no handle
decode, eigenclass test, environment load, collector branch, map, interface
call, or row-generation guard. General objects and captures use the new lane
only when the checker admits the corresponding source shape. Dynamic rows do
not enter, renumber, or widen the sealed static lookup image. This preserves
Target42's measured leaf rather than making every object pay for future Ruby
semantics.

The bounded general lane owns three preallocated typed slabs:

- an object slot containing the ordinary class/shape, checker-planned fixed
  fields, and an optional eigenclass reference;
- an eigenclass-row slot containing its ordinary fallback row and bounded
  checked method entries; and
- a captured-environment slot containing checker-assigned dense mutable cells.

No separate closure allocation is needed for this first slice: an installed
entry contains the immutable named target plus its environment reference,
installation identity, visibility, and epoch. Separate Go handle types carry a
nonzero packed slot index and generation; the value tag or static field type
selects the slab, so the handle need not duplicate a runtime kind field.
Reclamation advances the generation before reuse and permanently retires a
slot rather than wrapping. A distinct monotonic Ruby object ID is deferred
until an admitted `object_id`/hashing behavior requires it; handle identity is
sufficient for the current equality proof.

Strong roots are explicit owner state: top bindings, active frames, receiver,
arguments, locals, expression temporaries, blocks, completions/unwind values,
and construction roots. Typed tracing follows object fields and
object-to-eigenclass, eigenclass-entry-to-environment, and environment-cell
references. PIC arms are weak. A dynamic arm carries the generational
eigenclass reference plus cell epoch, installation, slot, and target; a reused
row therefore produces a generation miss without scanning and clearing every
site. A weak stale arm misses and resolves cold. An arm claiming a current
receipt with a stale environment is corruption and fails closed.

Collection occurs only at explicit allocation safe points and outside a
staged lookup transaction. The owner polls before collection, marks from a
preallocated root/work array in deterministic slot order, validates every
traced reference, then performs one finite non-cancellable sweep. Sweep clears
payloads, advances or retires generations, and rebuilds deterministic free
lists. There are no finalizers. The collector never runs on a warmed method
hit. Capacities and maximum scan work come from checked product facts and also
obey hard proof ceilings; the first fixture needs only two dynamic objects, one
eigenclass row, one environment, one capture cell, and their explicit roots.

General singleton definition is one publish-last transaction: validate facts
and receiver; poll and preflight all budgets, IDs, epochs, environment and row
capacity; collect only before staging if necessary; reserve and fully stage the
environment, row entry, and effective cell; poll once more; then publish the
receiver's eigenclass reference last with no subsequent fallible work. A
failure before publication releases reservations without changing semantic
lookup state. Redefinition stages replacement state and leaves the previous
environment to tracing. Definition does not execute the body.

The implementation ladder is intentionally split:

1. **H1a representation proof:** implement the private slabs, roots,
   generational reuse, weak-row arm, deterministic tracing, and an independent
   graph oracle. Prove cycles, shared capture cells, stale rejection,
   generation retirement, publication rollback, cap boundaries, close, and
   exact size/work ceilings without changing generated Ruby semantics.
2. **H1b semantic integration:** first replace pointer/map-only live evaluator
   state with explicit root-bearing frames and temporary ranges, then admit one
   exact source suffix. A helper installs `:label` on a runtime-selected object,
   the body captures and mutates one local, the object escapes the helper, an
   exact `GC.start` intrinsic collects while it remains rooted, repeated calls
   prove retained mutation, a same-class peer proves isolation, and a final
   collection reclaims the object/eigenclass/environment cycle. Canonical,
   pinned CRuby 2.6.10, and generated Go must agree on detached scalar output.

H1a must precede allocation-triggered collection in the evaluator: current Go
locals, raw object pointers, slices, and aliased environment maps are not an
enumerable root contract. Collecting while those remain authoritative would
be unsound. The integrated checker records the exact free-binding set and dense
environment layout, rejects object-bearing detached results and unadmitted
reflection/control flow, and keeps the Target41/42 generated artifact and
direct-leaf structural inventory frozen.

The alternatives rate as follows for the bounded general-singleton/capture
problem:

| Lifetime design | Exactness | Interim cycle reclamation | Stale safety | Static-leaf isolation | Economy | Overall |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| **Typed generational slabs plus tracing** | **9.7/10** | **9.8/10** | **9.8/10** | **9.9/10** | 8.0/10 | **9.5/10** |
| Monotonic arena, owner-close reset only | 9.5/10 | 2.0/10 | 8.0/10 | 9.9/10 | **9.4/10** | 7.6/10 |
| Direct Go pointers plus owner teardown | 8.5/10 | 5.0/10 | 3.5/10 | 9.8/10 | 9.0/10 | 7.2/10 |
| Reference counting plus generations | 7.0/10 | 1.5/10 | 9.0/10 | 8.5/10 | 5.0/10 | 4.7/10 |
| Copying/compacting arena | 8.8/10 | 9.2/10 | 8.0/10 | 6.0/10 | 3.0/10 | 5.8/10 |
| Object-ID/eigenclass overlay map | 8.2/10 | 6.0/10 | 6.0/10 | 4.0/10 | 5.5/10 | 5.4/10 |

Reopen this selection if active roots cannot be enumerated, capacities or scan
work cannot be derived from checked source, row/environment reuse can authorize
an old arm, cancellation or exhaustion can partially publish, the static leaf
must inspect H1 state, detached checkpoint/reload requires live Ruby identity,
or a simpler design proves both bounded interim cycle reclamation and stale
rejection under the same semantics. H1 adds no public handle, shared heap,
universal value, compiler IR, backend registry, cross-generation identity, or
change to the `preparedsource`/build/worker seam.

#### Target44 implements H1a and keeps the eigen-sidecar simplifier live

Target44 implements the isolated H1a representation proof in
`internal/rubyproof/h1_arena.go`; it still does not change accepted Ruby source,
the canonical evaluator, or emitted Ruby semantics. The owner contains fixed
arrays for four objects, two eigenclass rows, two captured environments, eight
roots, and eight mark-work items. References are distinct 32-bit nonzero
index-plus-generation types. Root and edge values use one closed 16-byte tagged
scalar, not the evaluator's pointer/map-bearing `rubyValue`. The implementation
uses no cgo, `unsafe`, maps, interfaces, function values, goroutines, locks, or
finalizers.

The proof now makes the Target43 lifetime contract executable:

- object fields, object-to-eigenclass, eigenclass-to-environment, environment
  parent, and environment-cell edges are traced, including an
  object/eigenclass/environment cycle;
- mark failure or cancellation clears marks before any sweep, while the finite
  sweep is non-cancellable and cannot partially reclaim the graph;
- reclaimed slots advance their generation before deterministic lowest-index
  reuse and retire instead of wrapping at the maximum generation;
- singleton installation reserves and initializes its environment and row,
  validates the complete transaction, makes both reservations live, and
  publishes the object's eigenclass reference last;
- PIC arms remain weak and carry the complete generational row reference, so
  neither collection nor slot reuse requires scanning call sites; and
- collection and close reject an active transaction, while ordinary close is
  idempotent and erases every owner-local identity.

The independent test-owned graph oracle declares its own eight-node graph and
does not inspect mark bits, free lists, or traversal order. Across all 64 masks
of four object plus two environment root candidates, exact reachable and
reclaimed sets, per-kind work, full-capacity sweep counts, and reuse order match
the oracle. Separate lifetime tests prove retained cycles and captured cells,
stale object/row/environment rejection, generation retirement, rollback and
capacity restoration, invalid-edge failure before sweep, weak-arm behavior,
and busy/idempotent close. The admitted allocate/define/root/collect/drop/
collect/reuse cycle performs zero Go allocations after owner construction.

On the current 64-bit Go toolchain, the measured private shapes are: 4-byte
reference, 16-byte value, 72-byte object slot, 48-byte eigenclass slot,
32-byte environment slot, 8-byte work item, and 792-byte complete owner. Mark
work is at most eight items and every sweep scans exactly `4 + 2 + 2` slots.
These are proof ceilings and structural observations, not production caps,
portable layout guarantees, elapsed-time evidence, or a representative Ruby
heap claim. The Target41/42 generated artifact remains byte-identical at
SHA-256
`b30db631a57127bd5f420427262cfff3970c14f6cf5c91cd382fd3b08d61bf46`.

A visible Pro consultation proposed a narrower **object-indexed eigenclass
sidecar**: store the admitted entry beside each object, key weak arms by the
complete object reference, and remove the separate row allocator, row mark
kind, and row generation. This is a serious H1b simplifier, not accepted proof.
For the current fixed arrays, object plus eigen storage is 384 bytes
(`4*72 + 2*48`). A dense four-object sidecar is also approximately 384 bytes
before presence/cap metadata if removing the object row reference shrinks each
object to 64 bytes (`4*64 + 4*32`); the known gain is therefore control and
scan-work economy, not yet storage. It also makes row identity and retirement
inseparable from the object and fails once an admitted eigenclass can be
reified, rooted, or outlive the attached object.

The alternatives therefore have two deliberately different rankings:

| Representation | H1a general-lifetime proof | Exact unobservable one-entry H1b slice | Current disposition |
| --- | ---: | ---: | --- |
| Separate typed eigenclass slab | **9.5/10** | 8.7/10 | Implemented H1a baseline; retain until a narrower proof matches its required invariants. |
| Object-indexed eigenclass sidecar | 8.6/10 | **9.5/10 provisional** | Preferred simplification candidate only while eigenclass identity is object-lifetime-identical and unobservable. |
| Monotonic environment/row storage | 7.6/10 | 6.8/10 | Still rejected because it cannot test reclamation, roots, or cache ABA. |
| Direct Go pointers plus teardown | 7.2/10 | 6.0/10 | Still rejected because reuse and stale-reference failure disappear. |

Before H1b semantic integration, compare the sidecar against the implemented
baseline with the same cycle, rollback, stale-arm, retirement, close,
zero-allocation, artifact-freeze, and independent-oracle obligations. Select it
only if it removes an allocator and mark kind, reduces maximum graph work from
eight to six, keeps the owner at or below 792 bytes, and preserves publish-last
failure behavior without introducing a branch into the Target41/42 leaf. If
that bounded comparison passes, H1b should use the sidecar and defer the
separate row slab until eigenclass reification or independent lifetime is
actually admitted. If it fails, the implemented slab remains the reference
architecture. Neither outcome creates a shared runtime or changes the
cross-language delivery seam.

#### Target45 selects the sidecar for the exact H1b admission

Target45 performs the comparison required above without modifying the Target44
baseline. `internal/rubyproof/h1_sidecar.go` is a second isolated Ruby-private
representation. It has four object slots, two environment slots, eight roots,
and a six-item mark array. There is no eigenclass pool, reference, free list,
generation, retirement state, mark kind, root kind, or independent lifetime.
The complete stored row is one 4-byte generational environment reference plus
an object-owned presence bit published last. The complete object generation is
the row and weak-PIC identity.

The first checkpoint retained dynamic selector, target, visibility,
installation, epoch, and fallback fields and was rejected as one abstraction
too large. The accepted candidate applies the actual checker contract: selector
1, target 1, public visibility, one static body, and no redefinition are fixed
admission facts; fallback derives from the owning object's ordinary dispatch.
Those facts therefore occupy neither the sidecar nor call data. This is the
critical simplification: it does not merely move the Target44 row beside the
object.

Environment reservation remains independent and generational. Singleton
definition validates the fixed source facts and receiver, reserves and stages
the environment, validates the complete transaction, makes the environment
live, writes the hidden environment reference, clears staging state, and sets
the presence bit last. Collection is forbidden during staging. Object tracing
follows the sidecar environment only after the presence bit is valid;
environment parents and cells retain the same object/environment cycle support
as Target44. Reclaiming an object clears its sidecar and advances or retires the
object generation, so an old object-keyed weak arm cannot authorize a new slot
occupant. No call-site scan is needed.

Candidate-specific lifetime tests retain the Target44 obligations that apply:
cycle survival and reclamation, captured mutable cell lifetime, stale object
and environment rejection, weak-arm ABA miss and non-rooting, generation
retirement, cancellation and explicit rollback without hidden payload or
presence publication, staging exclusion, capacity failure, invalid-edge abort
before sweep, and busy/idempotent close. The deliberately absent obligations
are precisely independent eigenclass reference, root, generation, capacity,
reclamation, and post-close resolution behavior.

An independent candidate-owned BFS oracle again checks all 64 object/environment
root masks and exact reachable, reclaimed, root, mark, and sweep work without
reading the collector's traversal state. The admitted allocation/reclamation
cycle performs zero Go allocations after construction. On the current 64-bit
toolchain the candidate compares with Target44 as follows:

| Structural fact | Target44 separate row | Target45 object sidecar |
| --- | ---: | ---: |
| Complete owner | 792 B | **632 B** |
| Object plus row payload at fixed caps | 384 B | **288 B** |
| Stored row | 48 B allocated slot | **4 B embedded environment reference** |
| Call data | 40 B | **12 B** |
| Pools / free lists / mark kinds | 3 / 3 / 3 | **2 / 2 / 2** |
| Maximum graph work | 8 | **6** |

These are exact structural and allocation-shape observations, not elapsed-time
or production-capacity evidence. The generated Target41/42 artifact remains
byte-identical at SHA-256
`b30db631a57127bd5f420427262cfff3970c14f6cf5c91cd382fd3b08d61bf46`.

The final ratings deliberately depend on admitted semantics:

| Representation context | Object sidecar | Separate typed row slab | Decision |
| --- | ---: | ---: | --- |
| Exact one-symbol, one-static-body, public, no-redefinition, unobservable H1b | **9.7/10** | 8.3/10 | Select the sidecar. |
| Reified/rootable eigenclass or independent row lifetime | 2.5/10 | **9.7/10** | Retain and reactivate the separate slab. |

**Decision:** H1b must start from the Target45 sidecar, not the more general
Target44 row slab. Target44 remains the proved fallback and falsifier rather
than production debt. Reopen the representation immediately if multiple or
dynamic singleton bodies, visibility mutation, redefinition, eigenclass
reflection/reification, independent rooting/lifetime, or another observable
row identity enters the admitted source. The next semantic step must first
replace evaluator pointer/map authority with explicit root-bearing frames and
temporary ranges, then integrate only the fixed Target45 source slice. Neither
private proof is a shared runtime, public API, cross-generation identity, or
change to `preparedsource`, build, or worker transactions.

#### Target46 freezes a separate exact H1b integration product

Target46 is an implementation contract, not an H1b implementation. The Ruby
lexer and parser already accept the complete witness, so H1b does not justify
another parser, a shared compiler IR, or grammar work. One private product
classifier after parsing must admit the exact program and seal an immutable
Ruby-owned `checkedH1bPlan`. The plan contains stable binding, call-site,
selector, target, capture-layout, safe-point, root-capacity, and pool-capacity
facts; it must not retain syntax pointers or make the emitter rediscover
binding and lifetime decisions.

The accepted source is exactly 718 bytes, 35 line feeds, and a final line feed,
with SHA-256
`999dee89ddb14f0c197fa0edde6fead99910eb82996b541409945ae09c3f41cb`:

```ruby
class H1bReceiver
  def label()
    7
  end
end

def read_h1b(receiver)
  receiver.label()
end

def install_h1b(receiver)
  captured = 40
  receiver.define_singleton_method(:label) do
    captured = captured + 1
  end
  GC.start()
  captured = captured + 2
  receiver
end

receiver = H1bReceiver.new()
peer = H1bReceiver.new()
before = read_h1b(receiver)
warm = read_h1b(receiver)
peer_before = read_h1b(peer)
receiver = install_h1b(receiver)
first = read_h1b(receiver)
second = read_h1b(receiver)
peer_after = read_h1b(peer)
receiver = 0
GC.start()
peer_after_receiver_collection = read_h1b(peer)
h1b_result = [before, warm, peer_before, first, second, peer_after, peer_after_receiver_collection]
peer = 0
GC.start()
```

Two clean-environment runs of the pinned local `/usr/bin/ruby` 2.6.10
(`universal.arm64e-darwin24`, binary SHA-256
`0c6118f2b4a9fe448d51eba2e79c81342ae1e111f015d58dce5c11740f4dfacd`)
produce exactly `7|7|7|43|44|7|7\n`, output SHA-256
`bea4358be9700b9e664d3ac83b5385e2549c8889295e118ec79996cfe0ceff06`.
CRuby corroborates only Ruby-visible scalar semantics. It cannot prove Ember's
root census, deterministic reclamation, capacity failure, slot reuse, stale
reference rejection, or weak-PIC ABA behavior.

The existing `eval.go` pointer/map evaluator is not the lifetime oracle for
this product: a live object or environment held only in a Go pointer, map,
slice, interface, or outcome local is invisible to a deterministic guest
collector. The canonical product instead uses packed generational object and
environment references plus one dense, preallocated root stack. Frames and
temporary ranges have explicit markers; a callee writes a reference result
into a caller-owned rooted destination before its frame is popped. Every
reference is re-resolved after a collection. Common explicit epilogues unwind
markers on every success and failure path without `defer` in the hot generated
path.

Capture promotion is one checked storage transition. Before definition,
`captured` is scalar 40. Environment allocation copies it into cell zero.
All later helper and singleton-body reads and writes use that same cell. The
complete sidecar is staged and validated, the preplanned helper binding is
redirected only when no fallible operation remains, and object presence is
published last. Collection is allowed only at checked `GC.start`, object
allocation pressure, or environment allocation pressure; it is forbidden
during staging, warmed dispatch, cleanup, and sweep. An implementation test
must force collection at the environment-allocation safe point while the
receiver is protected only by the callee frame root. The source witness also
warms the ordinary class arm at the same `read_h1b` call site before
installation, then proves publish-last sidecar presence makes that cached arm
ineligible for the receiver while the same-class peer still uses it.

The deterministic collection contract is:

1. the helper-internal collection reaches receiver, peer, and one environment,
   performs three mark-work items, and reclaims nothing;
2. after `receiver = 0`, the peer-only root performs one mark-work item and
   reclaims the receiver plus its environment; and
3. after `peer = 0`, the rootless collection performs no mark work and reclaims
   the peer.

The complete product therefore allocates/reclaims two objects and one
environment, performs three collections and four total mark-work items, and
ends with no live guest records. Checked hard caps are four objects, two
environments, eight roots, and six mark-work items. The weak PIC is never a
root. Reclaimed generations must reject old object/environment/PIC references
before deterministic lowest-index reuse.

Admission remains intentionally narrow: one class, one public ordinary
zero-arity method, selector 1, target 1, one static zero-arity singleton body,
one mutable scalar capture in cell zero, helper `+2`, body `+1`, the three
explicit collections, and a detached seven-scalar result. Reject dynamic
selectors, alternate or multiple bodies, method redefinition/removal,
visibility mutation, `Proc` or block escape, reflective hooks, finalizers,
host pins, object-valued results, reified eigenclass identity, recursion, and
any control flow that invalidates the sealed root or capture plan.

The mechanism placement decision is:

| Placement | Rating | Disposition |
| --- | ---: | --- |
| Private checked plan plus product-owned canonical executor and artifact-owned generated machinery | **9.6/10** | Selected; shares checked facts without sharing a runtime representation. |
| Artifact-owned generated fixed arrays without a sealed checked plan | 8.4/10 | Fast but duplicates semantic decisions and weakens differential proof. |
| Reusable generated source fragments | 6.5/10 | Consider only after two concrete Ruby products prove identical emitted policy. |
| Retrofit the existing static evaluator/emitter/template | 5.2/10 | Rejected; risks contaminating the proved direct leaf. |
| Shared cross-language heap, registry, Value, or runtime | 0.5/10 | Rejected; places language semantics below the actual stable seam. |

**Decision:** Target47 must implement H1b as a sibling Ruby-private product.
Start with red source/oracle/rejection tests and a frozen-artifact test. Then
land the sealed plan and independently rooted canonical executor before a
self-contained generated fixed-array product. Prove scalar parity,
allocation-pressure and explicit-collection roots, exact reclamation, rollback,
capacity failure, generation retirement, stale-reference and PIC ABA misses,
busy/idempotent close, zero post-construction Go allocations, and absence of
compiler/Ember/runtime dependencies from generated output. Only after those
semantic and structural gates pass may retained paired captures claim elapsed
performance. The old static checker/evaluator/emitter/template and generated
artifact must remain byte-identical at SHA-256
`b30db631a57127bd5f420427262cfff3970c14f6cf5c91cd382fd3b08d61bf46`.

This decision does not claim general Ruby closures, eigenclasses, GC, or
performance, and it does not create a public Ruby package, shared guest
runtime, compiler kit, backend registry, or cross-generation identity. Reopen
it only if the exact source cannot be expressed without changing the frozen
static product, canonical and generated root schedules cannot be proved
independently, a live reference escapes the root census, or admitted semantics
require the Target44 separate eigenclass slab.

#### Target47 implements and simplifies the exact H1b product

Target47 is now implemented as the separate product required above. This is a
bounded architecture proof, not a general Ruby runtime or a new public Ember
surface. `CompileH1b` reuses the Ruby lexer/parser, admits only the exact
718-byte source, and seals an AST-pointer-free `checkedH1bPlan`. That plan owns
the stable Ruby binding/call/capture IDs, copied spans, capacities, three
collection records, detached result, and exact liveness facts. Neither the
canonical executor nor the emitter retains syntax or rediscovers lexical
capture policy.

Integration removed one unnecessary root from the first working version. The
final plan reserves two caller slots and one `install_h1b` object-parameter
root, requires **zero environment roots**, and reaches at most three root
slots. After the environment and object sidecar are complete, object presence
publishes last; the rooted receiver sidecar is then the environment's strong
edge across `GC.start`. Keeping a second environment root would add state and
mark-input without protecting any additional safe point. A forced-pressure
run fills both environment slots, clears the caller's receiver value, and
collects before reservation; successful continuation therefore proves the
receiver survived solely through the callee object root.

The canonical and generated products deliberately do not share arena, root,
collector, PIC, or value types. Both use packed nonwrapping generational
references, deterministic lowest-index free lists, fixed root/mark arrays,
publish-last object sidecars, and owner-local weak PIC arms because the checked
Ruby semantics independently require those concrete mechanisms. Sharing the
checked facts and comparing detached observations is the seam; sharing either
runtime implementation would destroy the differential oracle.

The generated sibling is `internal/rubyproof/h1bgenerated`. Its checked-in
`ruby_h1b_generated.go` is 22,151 bytes with 849 line feeds and SHA-256
`253527270149ecf1a8101257c83bb946ec27be88e999fde902c389d7ba467a6d`.
It imports only `context`, contains fixed arrays and concrete scanners, and
retains no Ember, compiler, `preparedsource`, runtime, registry, map,
reflection, Go closure, or function-valued dispatch dependency. Its owning
freshness test regenerates it only through `H1bProgram.PreparedSource`; the
existing static generated artifact remains byte-identical at SHA-256
`b30db631a57127bd5f420427262cfff3970c14f6cf5c91cd382fd3b08d61bf46`.

The proved normal observation is exactly `7|7|7|43|44|7|7`, two object and one
environment allocation/reclamation, three collections, four total mark-work
items, per-collection high-water three, and no live roots or records. Canonical
and generated outputs match every scalar and every detached statistic. The
additional forced-pressure lane allocates/reclaims four objects and three
environments, performs four collections and six total mark-work items, and
still produces the same Ruby-visible result. Tests also prove ordinary-arm
warming and presence-gated invalidation at the same call site, peer isolation,
rollback/cancellation before publication, collection failure before sweep,
capacity failure, deterministic ABA misses, maximum-generation retirement,
weak-PIC non-rooting, busy/idempotent close, pure-Go operation, and zero Go
allocations per `Run` after owner construction. Race, checkptr, `CGO_ENABLED=0`,
vet, fixture-freshness, and frozen-artifact gates pass for the bounded product.

Current and proposed claims remain separate:

| Choice or claim | Rating | Status and evidence boundary |
| --- | ---: | --- |
| Exact H1b checked/canonical/generated architecture | **9.7/10** | Current and proved for the frozen source, including the two external-review root/PIC gates. |
| Language-owned compilers sharing only prepared source/build/worker transactions | **9.4/10** | Current architectural direction; H1b strengthens it, but each additional language feature must still prove its own semantics. |
| Reusing the Ruby lexer/parser plus immutable checked-fact pattern | **9.3/10** | Reuse within Ruby; future Ruby products may copy the pattern without sharing a semantic IR or runtime. |
| Extracting reusable Ruby generated-support fragments now | 6.0/10 | Deferred until a second implemented Ruby heap product proves byte-identical policy and measured code-size benefit. |
| General Ruby closure/eigenclass/GC support | 2.5/10 | Not implemented; multiple layouts, escaping blocks, redefinition, or reified eigenclasses reopen representation. |
| Revolutionary elapsed performance | 5.5/10 | Plausible potential only. Zero allocation and bounded direct state are proved, but no fair retained warmed-hit/collector capture exists yet. |
| Shared cross-language runtime, heap, `Value`, backend registry, or compiler IR | **0.5/10** | Rejected; it would couple semantics below the actual stable seam. |

The next performance step must compare the generated warmed singleton-aware
hit with an independently equivalent fixed-array comparator that performs the
same reference validation, presence selection, PIC checks, environment
validation, and scalar load/add/store. The collector lane likewise needs an
equivalent fixed-capacity trace/reclaim comparator. Full-product timings or the
old static scalar leaf are diagnostically useful but are not fair acceptance
denominators. Until retained paired captures pass a frozen noise-aware rule,
Target47 proves architecture, semantics, lifetime, structure, and allocation
shape only; it does not prove a performance revolution.

#### Target48 freezes a fair H1b observer, not a performance result

Target48 implements the next observer without changing the generated H1b
artifact. The generated source remains SHA-256
`253527270149ecf1a8101257c83bb946ec27be88e999fde902c389d7ba467a6d`.
Its comparator is a separate same-layout fixed-array implementation with its
own value, reference, arena, collector, PIC, and helper types. An AST gate
rejects unresolved references from that comparator to package-scope generated
identifiers; size, alignment, field offset, field size, field alignment, and
field kind are compared for every owner component. This is deliberately a
test-local oracle, not a reusable Ruby runtime layer.

The primary hit workload is one fixed nontrivial 64-call vector containing 32
ordinary-peer and 32 singleton-receiver calls at one wrapper callsite per
lane. Both arms are admitted once, then the exact vector runs for 1,048,576
untimed calls before measurement. Teardown checks the exact modular checksum,
the final captured cell, both object/environment records, PIC identities and
counters, allocation statistics, and owner flags. Representative invalid and
stale receiver/environment states compare exact error classes and every
detached before/after field that `readLabel` can inspect or mutate. Roots,
mark-stack payloads, and the definition-only next-transaction counter are not
called complete owner state. The claim is therefore a **balanced warmed
selection vector**, never a standalone singleton latency.

The collector workload is one exact three-collection lifetime cycle over a
65,536-image, 29,360,128-byte sequential bank after two complete untimed warm
batches. Its three post-phase cumulative object/environment reclaim totals are
`0/0`, `1/1`, and `2/1`; it ends with object generations `[2,2,1,1]`,
environment generations `[2,1]`, object free links `[2,3,4,0]`, environment
free links `[2,0]`, no marks, and checksum `10*N`. The large sequential-bank
access pattern is part of the claim. Cache residency and miss behavior remain
host-dependent and unclaimed; this is not repeated single-arena resident-hot
behavior or general Ruby GC performance. Both workloads prove zero timed Go
allocation.

The retained timing protocol is now partly executable as a pure test-local
contract. It supersedes the Target42 20-round/500ms protocol for Target48
only. Effectful acquisition remains disabled by the explicitly named gaps
below:

1. Build one authenticated `CGO_ENABLED=0`, PGO-off Go 1.26.4 test binary.
   Invoke each Current and Equivalent lane as a separate direct-binary process
   with `GOMAXPROCS=1`, `GOGC=off`, and exactly
   `-test.run=^$ -test.bench=^<one-exact-name>$ -test.benchtime=2s`
   `-test.count=1 -test.cpu=1 -test.benchmem -test.timeout=10m`. Do not pass
   `go test` driver spellings such as `-bench` to the compiled binary. Run four
   500ms warmup process pairs per comparator with the same argv except
   `-test.benchtime=500ms`, after an authenticated 30-second CPU warmup; retain
   their commands, identities, outputs, and control receipts even though their
   timings are not analyzed.
2. Acquire 30 complete process pairs for each comparator in each of two
   separately retained, reboot-separated captures. Exactly 15 pairs run
   Current then Equivalent and 15 run Equivalent then Current. Seven
   randomized balanced blocks contain four pairs (two of each order); the
   terminal two-pair block contains one of each order. This explicit terminal
   block resolves the arithmetic impossibility of putting 30 pairs entirely
   into four-pair blocks. The exact domain-separated SHA-256
   minimum-permutation schedules are frozen in `target48_schedule_test.go`.
   Capture A uses seed
   `8e9323e9bcf267050598ec1250b516b1fa991184ca54d2e53d7013207389e60b`
   and plan SHA-256
   `196380b419f5de65c0187e777ecb60dca4b9ca26d249a0b685fc57b2dbc28c42`;
   capture B uses seed
   `839f5583ba1d5c1faf15cce961e7bc43e06508f262a19eafa0b37f684072e377`
   and plan SHA-256
   `5ae5093a80df1a0f563143f0922aecdbffea731ec92f334c920e93613bf38898`.
   Every primary slot contains adjacent hit and collector pairs in a frozen
   15/15 family-first order. Four always-acquired reserve slots follow the
   primaries and provide two reserves for every comparator/order category;
   unused reserves remain raw evidence. Pair members run back-to-back.
3. For each pair compute
   `d = log((Current ns/op)/(Equivalent ns/op))`, dividing the positive
   parsed values before the logarithm so exact threshold ratios do not inherit
   cancellation from two separately rounded logarithms.
   The point estimate is `exp(median(d))`; for 30 observations the median is
   the arithmetic mean of sorted positions 15 and 16. Compute a central
   two-sided 99% bias-corrected-and-accelerated bootstrap interval with
   100,000 resamples, SplitMix64 seed `0x5434384243413939`, unbiased bounded
   draws, type-7 linear quantiles,
   ordinary leave-one-pair-out jackknife acceleration, half-weighted bootstrap
   ties for bias correction, and an exact constant-sample interval equal to
   that constant. Bias probability is clamped to `[0.5/B,1-0.5/B]`,
   transformed probabilities to `[0,1]`, and a singular transform or
   non-finite value fails closed. Go 1.26.4 `math.Erfc`/`math.Erfinv`,
   operation order, draw stream, bootstrap counts, transformed probabilities,
   and endpoints are frozen by analytic and independently reproduced seeded
   goldens. For this even-sample median, ordinary leave-one-out acceleration
   is identically zero; the retained name remains BCa because the formula and
   zero-acceleration proof are explicit, not because acceleration changes this
   estimator.
4. Analyze hit, collector, capture A, and capture B separately; never pool.
   Every cell passes only when the upper endpoint of its central two-sided 99%
   interval is strictly below 1.05.
   A strong bounded speed result additionally requires every upper endpoint
   below 0.90. A lower endpoint strictly above 1.05, or any semantic or
   allocation hard failure, fails. Every other valid result is inconclusive.
5. A valid capture must also keep scaled MAD of log ratios at most 2.5% for
   hit and 5% for collector, Theil-Sen predicted endpoint drift at most 2%/3%,
   order effect at most 1%/2%, and frequency/temperature nuisance-predicted
   span at most 1%/2%. Scaled MAD is
   `expm1(1.482602218505602*median(abs(d-median(d))))`; drift is
   `expm1(abs(29*median(all 435 ordinal pair slopes)))`; order effect is
   `expm1(abs(median(C->E)-median(E->C)))`. Nuisance span is the range of
   centered two-predictor OLS predictions for authenticated frequency and
   temperature observations, dropping an exactly constant predictor and
   failing closed when two nonconstant predictors have determinant at most
   `1e-12*Sff*Stt`. The formulas, inclusive limits, collinearity behavior, and
   analytic vectors are frozen in `target48_statistics_test.go`.
6. Across both comparators, one capture may replace at most two objectively
   exogenous invalid pairs, retaining each invalid pair and using a
   comparator- and order-matched reserve so the analyzed 15/15 balance remains
   exact. Admissible invalidity is limited to an authenticated host-control
   breach, OS/process interruption, or malformed/missing output attributable to
   the runner/tool rather than the product. Result magnitude, outlier status,
   semantic/checksum failure, and nonzero allocation can never invalidate or
   exclude a pair; the latter product failures are hard unfavorable evidence.
   Whole-capture noise-limit failure cannot discard individual outcomes and is
   terminal unfavorable evidence for the campaign, not a retryable invalid
   attempt. This removes outcome-dependent optional stopping; only independently
   attested exogenous invalidity can consume another attempt. A capture attempt
   starts when its immutable identity manifest is written after reboot/session
   admission and before any CPU or process-pair warmup. Exactly two valid
   captures are required for a pass, with at most four started attempts total.
   Any valid non-pass, including an inconclusive result, is unfavorable, is
   never rerun or replaced, and stops pass acquisition. Any fixture, observer,
   compiler, binary, host-control, or protocol change resets the count under a
   new identity.

The pure test-local contract now freezes exact A/B and same-order reserve
schedules, family interleaving, schedule and analysis seeds, a 64-KiB LF-only
one-process parser with exact metric and zero-allocation checks, central 99%
BCa/noise formulas and goldens, a canonical attempt-plan identity, and the
four-start stopping ledger. The attempt identity uses the v2 canonical schema,
which frames GOOS and GOARCH separately rather than joining slash-permitting
tokens into one ambiguous field. The ledger rejects every row after a terminal
pass, requires every B attempt to be reboot-separated from valid A, and
has an explicit exhausted result at the four-start cap. Reserve
selection validates the complete schedule before expansion, and its replacement
proof exhausts every one- and two-primary combination. The attempt plan binds
the test binary; generated, comparator, observer, protocol, runner, parser, and
gate sources; exact argv and admitted environment;
build/toolchain/target/boot identities; raw pair order; and core,
frequency/turbo, temperature, migration, and background-load observations.

Capture nevertheless remains **NO-GO** until an explicit effectful runner owns
durable pre-warmup attempt publication, the authenticated 30-second CPU warmup,
one-file-per-process launch, comparator/order-matched replacement selection,
normalized raw-coordinate closure, a bounded content manifest and append-only
ledger, and concrete compiler/disassembly and host-control receipts. These
effects must remain outside package tests; a qualifying host controller must
be independently trusted rather than self-attested by the benchmark process.
The current Apple M1/macOS host
cannot attest one pinned physical core, an idle/offline SMT sibling where
applicable, fixed non-turbo frequency, turbo policy, absence of migration,
thermal envelope, or bounded background load. `GOMAXPROCS=1` is not that
proof. One-iteration local runs are structural smoke tests only.

Target48 does not change the architecture ratings: exact H1b remains 9.7/10,
the language-owned compiler plus post-lowering prepared/build/worker seam
remains 9.4/10, and revolutionary elapsed performance remains 5.5/10 until
valid external evidence exists. A passing Target48 would prove only bounded
non-inferiority of these two same-layout microkernels; it would not promote
full Ruby, Luau, whole-worker, reload, or cross-language performance claims.
The only currently admissible future Ruby checked facts suggested by this
observer are `MethodTopologyStableAfterPublish(callsite)` and
`EnvCannotEscapeOwner(receiver, environment)`. They are proposals, not shared
IR or current compiler facts.

#### Target49 stops acquisition work and selects an inline-cell falsifier

A four-way read-only audit found no reason to move the language boundary. The
stable mechanism remains language-owned syntax, checking, facts, values,
heaps, runtimes, lowering, and generated ABI followed by `preparedsource.Set`,
application-owned `Layout` and typed handlers, ordinary static Go/build
products, and whole-worker transaction/reload. A shared `Value`, heap, core IR,
backend registry, language registry, or semantic acquisition framework remains
rejected. Target48 is Ruby-private test evidence, not another platform layer.

The audit fixed the fail-open ledger and identity defects above, but rejected a
new acquisition module or end-to-end runner-shaped recipe. The parser,
statistics, schedules, and stopping policy remain test-local preregistration;
they do not create an effect owner or a reusable ABI. Building publication,
process, retention, disassembly, or host-control machinery before a qualifying
host and a concrete policy decision would add ownership without producing
admissible evidence. Reopen that route only when an independently trusted
controller can enforce the Target48 host conditions, a reviewed effect owner
can durably publish and close attempts, and an actual production choice depends
on the frozen 1.05/0.90 gates. The seeded floating-point goldens must also pass
on the admitted acquisition GOOS/GOARCH; current local success is not a
cross-architecture numeric receipt.

The next Ruby-private candidate is the checked fact
`EnvCannotEscapeOwner(receiver, environment)`. For exact H1b, compare an
independent sibling that stores its one captured cell directly in the receiver
sidecar and removes the environment pool, reference, free list, and mark kind.
Select it only if it materially reduces owner bytes and graph work, preserves
capture mutation, forced-pressure collection, publish-last rollback, stale
reference and PIC ABA rejection, and independent canonical/generated parity,
adds no warmed-hit branch, and leaves the frozen static Ruby artifact unchanged.
Reject it immediately if a Proc can escape, a capture is shared, an eigenclass
is reified or independently lived, or the environment has an independent
lifetime. Those cases keep the separate typed environment representation.

Target49 therefore keeps the exact H1b mechanism at **9.7/10**, the broader
language-owned architecture at **9.4/10**, and revolutionary elapsed evidence
at **5.5/10**. The post-lowering seam itself rates **9.9/10**; a generic
acquisition framework now rates **2.5/10**. The inline-cell comparison rates
**9.5/10 decision value** because either result deletes Ruby-private machinery
or proves precisely why it must remain without leaking semantics into the
shared delivery/build/worker seam.

#### Use eager effective-resolution cells, not hit-time path validation

D1's invalidation authority is an owner-local dense table of eager
**effective-resolution cells**. The bounded proof now implements this private
Ruby mechanism for two generated selectors, five dispatch rows, and four
fixed two-arm sites. It deletes the generated R0 `labelEpoch`/`hotEpoch`,
`labelVersion`, and string `sendLabel` authorities rather than wrapping them.
The product-capped matrix and broader mutation categories below remain the
production-frontier proposal; the current proof claims its bounded local
alias/remove/undef and Base visibility schedules, literal-selector
`send`/`public_send`, one C1 include/prepend schedule, and one preassigned C2
singleton row. It does not claim general singleton classes, dynamic selectors,
caller-sensitive protected ordinary
calls, arbitrary attachment graphs, or arbitrary repetitions of those
mutations.

The checked program assigns nonzero generation-local IDs for dispatch classes,
lookup nodes, attachments, selectors, local method slots, installed
definitions, immutable shapes, generated targets, and, only when admitted,
`super` continuations. The sealed image also carries compare-only all-mask
route facts; they are validation evidence, not a mutable route identity or a
generated-runtime input. A destination created by `alias_method` owns a distinct local
slot but retains the aliased definition/body identity; later replacement of
the source slot therefore does not retarget the alias. `remove_method` removes
the local entry and exposes the next ancestor, while `undef_method` installs a
terminal tombstone. Visibility is entry and receipt state. A singleton method
creates or changes the receiver's singleton dispatch-class row; it is not an
object-layout shape mutation.

Only constant selectors prepared from checked source receive a dense selector
column. Dynamic names and caller-sensitive protected checks remain on bounded
canonical lookup until separately admitted. Every admitted dispatch-class row
has a cell for every prepared selector, including missing and `undef`
outcomes. The cell contains a nonzero effective epoch and a canonical receipt:

- resolved, missing, or `undef` outcome;
- winning owner, local slot, and installed definition when resolved;
- visibility; call mode and context-free legality remain site policy rather
  than a second resolution graph; and
- an admitted `super` continuation identity when changing the continuation
  would change later behavior.

No route, route hash, snapshot ordinal, or topology-state ID is retained as
mutable owner authority. The checker seals independently constructed route
oracles for every admitted activation mask and row; constructors compare
descriptor-derived routes with those facts, and the emitter validates but
omits them. A route-only change does not invalidate an ordinary send whose
effective receipt is otherwise identical. Receipt equality deliberately does
include a missing/`undef` transition, a winner or definition change, a
visibility or admitted legality change, and a relevant continuation change.
`send` and `public_send` may consequently share a resolution cell and both
repair after a visibility mutation even though `send` remains legal. This is
safe conservative repair, not a hit-time mode branch through a second lookup
graph.

A generated two-arm site encodes its prepared-selector ordinal. Each unrolled
arm checks receiver dispatch-class and immutable shape IDs, directly indexes
the row and selector cell, compares the effective epoch and resolved
slot/definition, and enters a site-local numeric target switch. The steady hit
must perform no string or map lookup, ancestor walk, path/hash validation,
allocation, lock, atomic, interface-dispatched method target, or function-value
method target. Explicit context cancellation polls remain part of control; the
single-owner admission rule makes plain scalar lookup-state loads sufficient.
An occupied
arm with impossible IDs remains corruption and poisons before guest effects;
a valid resolution without a compatible emitted target executes canonical
fallback.

Mutations are owner transactions, not concurrent cache edits. A method,
visibility, alias, remove, or undef mutation stages its local entry and
recomputes one prepared-selector column across admitted dispatch rows. An
include/prepend mutation stages only the new activation bank, derives the old
and new routes transiently into bounded scratch, and recomputes the complete
prepared cell matrix. The admitted singleton define uses the same entry
transaction and changes exactly its preassigned row's label cell.
All capacities, IDs, route lengths, cancellation, and the number `K` of changed
effective receipts are checked before publication, including room for `K` new
nonzero epochs. Commit then publishes the local tables, activation bits, cells,
and epochs without
allocation, polling, or another fallible operation. The owner-wide allocator
provides unique epoch values but is not a global validity serial: cells whose
effective receipts are unchanged keep their epochs, so a shadowing subclass
does not stale when a farther ancestor changes. PIC arms are not traversed or
mutated during invalidation; their old epochs make them stale lazily.

Fallback evaluates receiver, arguments, and block exactly once, exits a stale
arm before callee effects, performs canonical lookup and execution, and
publishes no arm on a guest raise or host abort. After normal return it rereads
dispatch class, shape, and the cell; only an unchanged compatible resolution
may publish a complete arm, with the occupied marker written last. A guest
mutation that committed before a later guest raise remains committed, but the
raise does not itself poison the owner. Host cancellation, limit abort, or
detected corruption poisons as already required. Close clears tables, cells,
epochs, activation banks, route scratch, targets, and arms; a fresh owner
rebuilds them cold. None is an application checkpoint or cross-generation
identity.

The first proof uses hard admission ceilings, including their products rather
than pretending all individual maxima compose:

- at most 256 dispatch classes, 512 lookup nodes, 64 prepared selector
  columns, ancestor depth 32, 8,192 cells, 32,768 admitted local lookup
  entries, 64 attachments, 512 compare-only route oracles, 8,192 sealed oracle
  path slots, and 4,096 two-arm sites;
- `dispatch classes * prepared selectors <= 8,192`, `lookup nodes * prepared
  selectors <= 32,768`, and `dispatch classes * depth <= 8,192`; and
- `resolution cells * depth <= 262,144` canonical mutation probes, with an
  initial 1.5 MiB ceiling for invalidation-specific owner state whose actual Go
  layouts must be measured before implementation acceptance.

These are proof caps, not production Ruby capacity promises. The current
Base/Alpha/Beta/Gamma/Delta program has seven lookup nodes including two
modules, eight checked selectors, four realized dispatch rows, and two
attachments; the generated closure projects two selectors into eight cells.
Redefining `Base` changes only inheriting Alpha and Delta label receipts; all
saved-alias cells and the overriding Beta/Gamma label cells remain
byte-for-byte stable. Protected/private/public Base transitions also change
only Alpha and Delta; the exercised public-only site rejects Alpha before
method effects,
literal `send` bypasses them, Beta's public override remains valid, and public
restoration repairs Alpha. Removing Beta's label changes only Beta and exposes
Base v2. Undefining Gamma's label changes only Gamma and blocks Base. A
detached module definition changes no cell (`K=0`); include changes Alpha and
Delta (`K=2`); an empty prepend publishes its activation bit with `K=0`; the
later prepended-module definition changes only Beta (`K=1`); and redefining the
included method changes Alpha and Delta (`K=2`). The exact activation sequence
is `00 -> 01 -> 11`; an independent observer also derives the otherwise
unreached `10` mask. A max-fanout case hits the declared probe ceiling without
partial publication. One-object singleton implementation and full protected
ordinary-call caller relations remain separate if claimed. Public receipts
compare only results, error classes, and ordered effects; private observers may
additionally verify exact changed and unchanged cells.

Primary integration rates the alternatives below; these are decision ratings,
not averages of the three independent reviews:

| Invalidation route | Semantic exactness | Steady hit | Mutation/storage | Lifetime/reopen | Ruby frontier | Other-language isolation | Economy |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| **L1 dense eager effective-resolution cells** | **9.8/10** | **9.7/10** | 8.0/10 | **9.7/10** | **9.6/10** | **10.0/10** | 8.5/10 |
| L2 lazy path/version validation on every hit | 9.1/10 | 5.5/10 | **9.0/10** | 9.2/10 | 8.9/10 | **10.0/10** | 7.4/10 |
| L3 one coarse owner method/topology serial | 9.3/10 | **9.9/10** | **9.2/10** | **9.9/10** | 6.5/10 | **10.0/10** | **9.8/10** |
| L4 method/topology nodes directly watch PIC sites | 9.4/10 | 9.7/10 | 4.8/10 | 5.5/10 | 7.5/10 | 9.7/10 | 5.0/10 |

L3 is the cheapest fallback but destroys the selected shadow-aware precision
and can create unrelated repair storms. L2 moves mutation work onto every hot
send. L4 gives topology nodes references into mutable site lifetime and pays
the most complex cleanup cost. A sparse L1 table with bounded numeric reverse
dependency edges remains a reopen route, not part of the initial mechanism:
consider it only if the dense product caps reject representative programs or
the bounded full-matrix topology mutation misses a later latency gate. It must
preserve the same cell/hit contract and may never introduce site, object,
code-generation, or cross-generation references. If dense cells cannot meet
the stated caps without a hit-time map/path/hash, or correct mutation cannot
commit after complete bounded preflight, reject L1 rather than silently widen
the shared architecture.

#### Keep attachment descriptors as topology authority, not route snapshots

Target39 compared three Ruby-private representations for the exact two-
attachment C1 closure. **A** retained live/staged flattened routes. **B** sealed
all four activation snapshots and selected one with a compact mutable ordinal.
**C**, the selected form, seals seven node descriptors and two attachments,
keeps only live/staged activation bits, and derives routes into bounded scratch
during cold construction and mutation. None changes the dense-cell or warmed-
PIC contract.

A test-only comparator gives B every closed-fixture advantage: four compact
snapshot headers address exactly 42 `uint32` path slots, and its owner needs
only two one-byte selectors. That topology payload is 200 immutable plus 2
mutable bytes. The equally compact C replica is 80 immutable bytes plus 56
bytes for live/staged bits and its 36-byte route workspace, or 136 bytes total:
66 bytes (32.7%) less for this fixture. Both resolve the same receipts and
produce the exact mutation vector `K=0/2/0/1/2`. B reads a sealed span; C does
additional node/attachment fact reads while building the same routes. These
are deterministic modeled-layout and structural-work counters, not a wall-time
claim. No standalone B generated package was built, so source, archive, text,
binary, and build-cost comparisons remain unresolved.

The decision is about product authority rather than winning one tiny fixture.
B is the strongest exact closed-C1 baseline, but snapshot count grows with the
enumerated activation-state space and turns precomputed answers into the
runtime selection mechanism. C scales with admitted nodes and attachments,
keeps route identity derived, and leaves warmed code unchanged. The ratings
are therefore:

| Target39 topology route | Exact bounded evidence | Product trajectory | Disposition |
| --- | ---: | ---: | --- |
| A. Persistent live/staged routes | 8.9/10 | 7.0/10 | Reject as authority; reconsider only as a validated derived cache after representative cold evidence. |
| B. Sealed route snapshots | **9.5/10 closed C1** | 6.6/10 | Retain as the exact finite comparator and falsifier, never production authority. |
| **C. Attachments + activation bits + transient routes** | **9.8/10 semantics/lifetime; 9.2/10 representation selection** | **9.6/10 conditional** | Selected and implemented for the bounded C1 slice; representation score stays provisional until cold timing and a standalone B artifact exist. |

C is falsified if a route, path, snapshot/state ID, or adaptive semantic mode
becomes authoritative; a warmed leaf reads topology; `K=0` changes an epoch,
cell, or arm; failure partially publishes; scratch survives close; reload
retains bits/routes/arms; the generated artifact emits route oracles; or
canonical, generated, and independent literal routes diverge, especially for
mask `10`. A persistent route cache may be reconsidered only as owner-local
derived state validated from C after representative cold measurements; it may
never become a cross-language seam or warmed-hit dependency.

#### Make the checked lookup image a fact seam, not a runtime

The selected D1 ownership boundary is one private immutable checked lookup
image plus one deep mutable owner in each physical execution lane. This is
boundary route I1 in the local comparison below; it is unrelated to the later
ADR milestone `I1. Optional dynamic-language sealed image`. The D1 image is
not a public artifact, binary runtime image, compiler interface, Backend, or
cross-language ABI. It is one logical Ruby fact product with two physical
encodings:

```text
Ruby source -> syntax/checker -> checkedProgram + sealed lookupImage
                                      |                 |
                                      |                 +-> canonical Runtime
                                      |                     copies mutable state
                                      |
                                      +-> emitter serializes the same numeric facts
                                          -> dependency-free generated Go
                                             + independent Engine behavior
```

R0 had no such boundary: its canonical runtime read AST bodies and a flat
string method table, while its emitter re-walked checked syntax and generated
proof-specific epochs and string-send machinery. The bounded D1 cutover now
removes that duplicate **fact derivation** while preserving deliberately
independent canonical and generated behavior. The full checked image remains
larger than the generated closure, so the emitter cannot accidentally turn a
passive fact product into a common runtime.

The sealed `lookupImage` owns only passive, owner-neutral Ruby lookup facts:

- dense selector, lookup-node, attachment, local-slot, installed-definition,
  dispatch-row, and admitted-continuation IDs;
- declared superclass and attachment topology, initial local entries, dense
  row/selector coordinates, initial effective receipts, installed-definition
  to snapshotted-body IDs, and checked ordered mutation descriptors with exact
  entry and activation-bit before/after states;
- independently checked all-mask route oracles and path literals used only to
  validate descriptor-derived routes; these comparison facts are omitted from
  generated Go and never resolve a call;
- caps plus a private schema/identity; and
- deterministic source attribution sufficient to relate diagnostics and
  freshness receipts to the original checked program.

`checkedProgram` separately owns the immutable executable
`definitionID -> checkedMethod` body table and the minimum checked shape and
call-site facts. Alias installation therefore receives a new definition while
retaining the source definition's checked body; later replacement of the
source coordinate cannot retarget it. The image contains no AST body pointer,
owner object, mutable table, live epoch, shape or object state, PIC arm, target
ID, site arm policy, function value, callback, effect result, expected public
result, checkpoint identity, or cross-generation reference. In particular,
`definitionID` is a Ruby semantic fact while `targetID` is assigned only by
the emission plan for one generated package. A target denotes one admitted
definition/body implementation and may be reused by several separately
validated dispatch/shape/receipt matches. Named Go bodies, that finite match
table, and each site's two-arm policy remain generation-local generated-code
decisions.

The canonical `Runtime` and generated `Engine` each construct one mutable
owner from their physical copy of those facts. The canonical owner has local
entries, dense cells, epochs, two activation banks, bounded route/mutation
scratch, shapes, objects, execution control, and poison/close state, but no
targets, PIC arms, persistent routes, or topology-state ID. The generated owner
has switch-encoded node/attachment facts, independently implemented entries,
cells, scalar activation bits, stack-local route scratch, epochs, objects,
control, and fixed arms. It contains no emitted route oracle or flattened path.
There is no nested owner,
package-global cache, registry, public type, runtime interface, or shared
operational helper. Inner lookup and site loads are plain scalars under the
existing single-owner admission rule; atomics remain only at outer admission.

Image construction is fail closed. The checker assigns nonzero dense IDs,
validates topology and all individual/product/work limits, independently
builds every admitted activation-mask/dispatch-row route, resolves every
admitted dispatch/selector pair, and validates each initial receipt against the
topology and definition inventory. The Target39 image seals 16 compare-only
route oracles covering exactly 42 path slots. It then deep-copies builder
arrays, hashes a framed private encoding, seals the image, and discards name
maps and worklists. Runtime construction validates the sealed facts, computes
exact byte ledgers, allocates exact-count mutable storage, independently
derives and compares every sealed route, recomputes the initial cells into
scratch, and publishes only the complete owner. The emitter consumes only this
image plus checked definition, shape, and site facts; it validates all sealed
oracles while building its plan but serializes only descriptors, mutations,
targets, and matches. Re-walking declarations to recreate topology, IDs, or
epochs is a failure of the boundary.

The two execution lanes share facts and receipt definitions, but not a
`Resolve`, `DispatchCore`, `MutationEngine`, serialized execution trace, or
preselected-winner behavior. Canonical lookup walks mutable topology and local
entries and remains correct if a derived cell is corrupted. Owner cell
recomputation is a separately authored algorithm. Generated fallback and
mutation are independently emitted over private numeric arrays, and a site
implements its own scalar guard plus local target switch. Pinned CRuby running
the original source supplies public expected values, error classes, and
ordered effects. Source, image, Set, and generated-byte digests prove input
attribution and freshness; none authenticates the semantics of the lane that
produced it.

A warmed generated hit therefore remains exactly the selected direct path:

```text
receiver dispatch/shape
  -> constant-selector cell
  -> epoch/slot/definition checks
  -> two-arm target ID
  -> site-local numeric switch
  -> statically named Go body
```

It performs no string or map lookup, ancestor walk, hash/path validation,
allocation, lock, atomic operation, interface-dispatched method target,
function-value method target, or imported-runtime call. Explicit control polls
remain. A miss or stale arm uses the deep generated owner
for canonical fallback and post-success whole-arm revalidation; sites do not
duplicate mutation transactions. The occupied marker is written last.

The target-31 caps remain authoritative. The implemented conservative maximum
ledger charges exact-count local entries; live and staged cells; two activation
banks; route and mark scratch; changed indices and bits; one immutable copy of
nodes, rows, selectors, attachments, route oracles, and compare-only oracle
paths; two-arm sites; and an owner-header reserve. It totals **1,442,960 bytes**,
leaving **129,904 bytes** below 1.5 MiB. The calculation is checked against the
actual frozen 64-bit descriptor sizes, but constructor cost and
representative-program admission remain implementation gates. The object heap
is governed separately.

The current Base/Alpha/Beta/Gamma/Delta plus IncludedLabel/PrependedLabel proof
uses two emitted selectors, nine emitted definition/body targets, five
realized dispatch rows, ten generated cells, routes of depth at most four,
two attachment descriptors, and four fixed two-arm sites. Alpha inherits Base
v1; Beta and Gamma override; Delta initially follows Alpha. Include makes
IncludedLabel win for Alpha and Delta; empty prepend changes Beta's route but
not its effective receipt; defining PrependedLabel later changes only Beta;
redefining IncludedLabel changes Alpha and Delta. The label and public sites
admit Alpha and Beta while Gamma and Delta use bounded fallback; the saved site
admits Alpha's alias. The dedicated singleton site admits the one preassigned
receiver and an ordinary Alpha peer; singleton definition changes only the
receiver row. Earlier Base redefinition, visibility, remove, and undef behavior
remains exact. The proof corrupts a derived cell while leaving
topology and local entries intact: canonical lookup remains correct, while
generated validation fails or falls back. It also shows all four activation
masks, stale exit before effects, `K=0` topology publication, selective repair,
byte-stable unrelated cells and arms, atomic cap/epoch/cancellation failure,
complete close, and a cold fresh owner whose repeated numeric IDs convey no
identity.

Primary decision ratings are:

| D1 boundary route | Semantic authority | Hot hit | Mutation locality | Proof independence | Lifetime | Ruby frontier | Other-language isolation | Economy |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| **I1 passive image + owner per lane + generated site contract** | **9.9/10** | **9.8/10** | **9.6/10** | **9.5/10** | **9.8/10** | **9.7/10** | **10.0/10** | **8.4/10** |
| I2 monolithic Ruby runtime | 8.7/10 | 7.8/10 | 8.5/10 | 3.5/10 | 7.6/10 | 8.2/10 | 9.8/10 | 7.8/10 |
| I3 generated-only lookup duplication | 7.2/10 | **9.9/10** | 8.0/10 | 8.8/10 | 8.8/10 | 6.5/10 | **10.0/10** | 6.5/10 |
| I4 generic cross-language cache core | 3.0/10 | 4.5/10 | 5.0/10 | 3.0/10 | 4.5/10 | 4.5/10 | 0.5/10 | 2.5/10 |

I2 either makes canonical and prepared evidence self-comparing or eventually
extracts an informal weaker I1 to preserve dependency-free generation. I3
keeps physical separation but makes the emitter a second authority for
topology and identity. I4 translates Ruby topology, visibility, mutation,
unwind, and lifetime policy into callbacks while imposing a tax on languages
that have no such semantics. Keep the first implementation cohesive and
private under `internal/rubyproof`; do not split allocators, cells, topology,
sites, or transactions into pass-through modules. Delete the old flat/string
and proof-epoch paths rather than running both.

Reject I1 if the emitter must rederive topology from syntax, generated code
must import or delegate to the canonical runtime, sharing passive facts forces
shared lookup behavior, the byte/cap/infallible-commit claims fail on actual
layouts, a direct hit needs dynamic machinery, or reload requires cell/arm
identity across generations. Narrow I3 duplication is preferable to a
self-confirming I1 behavioral facade. The bounded nucleus changes no public
API, other language, checkpoint, build, worker, or performance claim and is
now implemented under `internal/rubyproof`. The product-capped production
matrix, singleton category, full protected ordinary-call legality, and retained
open-world timing observer remain unimplemented. Target39 implements only the
bounded C1 module/include/prepend schedule described above.

This proof changes the architectural answer in one precise way. Ruby still
cannot be obtained by swapping only a Luau parser: its object model, dynamic
send, block return target, unwind, heap, and recovery are unavoidable
language-owned components. D1 retains R0's proof that Ruby does not need a new
portable native target or static package-delivery value. C0 separately proves that one
bounded Ruby scalar projection can reuse the application transaction schema
and embedded journal beside Sprig. RP0 then proves that one bounded application
can preserve graph topology and reconstructable captured-self behavior across
two generated Ruby code generations without preserving heap identity or code
tables. None establishes that the process builder, loader, or reload mechanism
needs no Ruby-specific change; S5b and R1 retain that falsifier. Sharing still
stops after language lowering at `Set`/`Layout`, resumes at application-owned
closed records and typed decisions, and costs no universal guest
representation on the hot path.
The useful Spinel mechanisms are therefore private Ruby facts, typed unwind,
differential oracles, reachability, and guarded specialization; Spinel's C
runtime and a cross-language semantic Core remain unnecessary.

### Share compiler mechanisms only below proved semantic decisions

The following implementation mechanisms remain candidates for private sharing
only after two concrete users show matching code and invariants:

- source identities, byte spans, compile budgets, and stable diagnostic
  ordering;
- checked arena IDs/spans and dense bitsets where representation benchmarks
  justify extraction;
- deterministic Go names, imports, formatting, source-size sharding, and
  prepared-source construction;
- bounded versioned binary-envelope primitives;
- differential harness plumbing whose semantic oracles remain language-owned;
- a pure scalar IR restricted to `b1`, `i64`, `f64`, branches, return, and an
  explicit Poll terminator.

The scalar IR must not contain opaque guest values, tables, method sends,
exceptions, language tags, generic calls, runtime effects, or language-specific
recovery. Language planners own eligibility, dependency guards, and exact
recovery sites. Its emitter receives concrete Go names and context operations
at build time, so no runtime backend interface or language dispatch is added.

The real internal Sprig proof found no such match. Its source spans, diagnostics,
checked facts, polling, arithmetic, and tiny source writer have different
invariants from Luau; Go's formatter plus `preparedsource` already own the only
useful common mechanics. Do not extract `internal/scalaraot`, shared source
machinery, or an emitter facade. Reconsider a private helper only after another
real producer duplicates the same complete operation and a benchmark shows a
net win. A public `aotkit` additionally waits for an independently versioned
compiler and at least two production producers.

Ruby supplies the missing dynamic-language comparison and reaches the same
boundary. It also uses byte spans, SHA-256, deterministic diagnostics,
`go/format`, and `preparedsource.NewSet`, but its declaration versions, method
epochs, owner poisoning, object identity, captured return targets, typed
unwind, guard eligibility, and recovery sites do not match Sprig, Seed, or
Luau. Extracting a compiler kit would replace a handful of standard-library
calls with callbacks for the hard language decisions. The third concrete
producer therefore strengthens `preparedsource`; it does not earn shared
syntax, checked IR, runtime, optimizer, or emitter modules.

Current `backendProtoIR` is not that scalar IR. It contains Luau opcodes,
registers, tags, tables, globals, upvalues, calls, yield, allocation, and replay
policy and remains private to Luau.

### Use ordinary Go as the common native backend

Hot functions remain ordinary generated Go. For Luau and Ruby, the first
build-to-READY optimization candidate is a language-owned sealed code/program
image sufficient to bind an owner without parsing, checking, or compiling
embedded guest source after process launch. This directly targets the current
Luau worker's source-recipe reconstruction before `READY`; it does not change
the hot execution representation.

The image is embedded with one exact pattern into an unexported string:

```go
import _ "embed"

//go:embed program_v1.bin
var programImage string
```

An embedded string gives immutable linked data without a runtime blob copy.
The language owns magic, codec version, semantic ABI, bounds, offsets, program
identity, inventory, digest, and generated-function bindings. Decode and bind
are two fail-closed phases:

1. decode into temporary owner-neutral storage; reject truncation, trailing
   bytes, excessive counts before allocation, overflowed spans, invalid IDs or
   enums, noncanonical order, duplicates, digest mismatch, and inventory or
   generated-function mismatch; then publish one fully validated immutable
   image; and
2. bind that image to the concrete runtime/hot functions and allocate mutable
   owner state. A partially decoded image or partially initialized owner is
   never published.

The generic builder authenticates the selected bytes but never interprets this
language schema. The concrete `HandlerFactory` validates and binds every image
before it completes, which already precedes candidate `READY`. Luau, Ruby, and
Sprig do not share an image schema. Sprig may need no image at all. The image
remains a measured candidate rather than an accepted baseline until it improves
cached build-to-READY or candidate RSS under the gates below.

Generated Go is sharded by a bounded source-size policy, not one file per
function or Proto. Go caches compilation at package granularity, so file count
alone is not an incremental-build mechanism. Builder V1 validly replaces its
one already existing generated source path through Go's overlay mechanism. A
disposable multi-leaf preflight showed that this does not generalize to new
files: with real `doc.go` directory anchors, offline
`go list -deps -json -overlay` reported only `doc.go`, and the first supplied
leaf, `generated/ruby/identity.go`, was selected zero times rather than once.
Go overlay is replacement, not arbitrary file injection. The proof-local test
was removed after recording the result.

S4 therefore uses physical files in a fresh, minimal standalone worker module,
not per-leaf placeholders. The application owns the module path, fixed worker
wrapper, concrete handler/codecs, Layout placement, and exclusive root
assembly. The host owns the root lease and cleanup. It exposes the completed
root to the builder only after every file is closed and verified, and no owner
mutates it during a build. The root may be reused read-only for retries or
multiple target builds of the same generation and is released after the
executable is published. The builder never copies the caller's module,
materializes Layout files, or mutates this root.

A follow-up disposable observer constructed two byte-identical roots and a
separate changed-generation root from an empty directory. Each root contained
only `go.mod`, a worker-local `internal` package, one typed application
handler/main, and two physical generated packages with multiple Go leaves and
one embed asset. Offline `CGO_ENABLED=0` Go discovery selected every reachable
Layout leaf exactly once and an intentionally unreachable mount zero times.
Both identical roots produced the same package BuildIDs and
`ruby-v1|12|15|27`; v2 removed the v1 implementation leaf, preserved the
unrelated Sprig/support BuildIDs, changed the Ruby/application BuildIDs, and
produced `ruby-v2|17|15|32`. Mutation and missing-leaf admission failed before
Go ran. The exact v1/v2 Layout identities are
`5116530e1182a2de1869787662da289b6fe479ae9bba9413e542b68d06b428b6`
and `fb1930c648640d4c4e5939df2cc14098cfe40404cc53567ef6d5e376807ee96c`.
The relevant Go 1.26 package BuildIDs were:

| Package | v1, identical across both roots | v2 |
| --- | --- | --- |
| `cmd/app` | `Euciqk4gywZ63FMqoZGf/cewA_74ueZSCmSr4T4FK` | `E4V-B2Njs1gT3cdjYBTk/8V9wI6IPEJbMdGXgq6zR` |
| `generated/ruby` | `XB1Yv9DODsFj3HoDhmeL/RKhISoFEc2FUyrJapjhL` | `huWVgNStuR2E-ChZhgtd/AY3hxTxTpUPz20-pOIxG` |
| `generated/sprig` | `nWqNmMnoL7kMtHYvAqaw/B62XlzodZIVX7FSprrn9` | unchanged |
| `internal/support` | `ljk03O1AZBzpTKfmljih/77D2JPqCyEh3Snuuxjob` | unchanged |

The 640-line proof-only harness was removed after its focused default,
cgo-free, race, vet, and pure-Go observers passed; this is bounded local
mechanism evidence, not retained S4 or build-latency certification.

#### Bound Go discovery before decoding

The current V1 builder asks `go list -deps -json` for unrestricted package
records, buffers the complete result, copies it into a decoder, and never uses
the recursively expanded `Deps` or `DepsErrors` fields. Go 1.26 computes those
fields only when requested. A controlled package chain therefore made the
unused dependency references grow as `n*(n-1)/2` across records even though
the builder needs only package identity, source inventories, and failure
metadata.

A disposable Go 1.26.4 observer requested exactly these top-level fields:

- packages: `Dir`, `ImportPath`, `Standard`, `Incomplete`, `GoFiles`,
  `CgoFiles`, `CFiles`, `CXXFiles`, `MFiles`, `FFiles`, `HFiles`, `SFiles`,
  `SwigFiles`, `SwigCXXFiles`, `SysoFiles`, `EmbedFiles`, and `Error`; and
- modules: `Path`, `Version`, `Main`, `GoVersion`, `Sum`, `GoMod`, and
  `Replace`.

For controlled chains, selected decoding produced exactly the same ordered
`listedPackage` and `listedModule` values, selected source inventory, and
production wrapper, standard-library, and module digest framing as unrestricted
JSON:

| Generated chain | Unrestricted bytes | Selected bytes | Selected ratio | Decoded packages | Source entries | Recursive `Deps` references |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 64 | 296,911 | 35,185 | 11.8504% | 128 | 671 | 2,016 |
| 128 | 710,863 | 51,505 | 7.2454% | 192 | 735 | 8,128 |
| 256 | 2,226,895 | 84,145 | 3.7786% | 320 | 863 | 32,640 |

Absolute temporary paths in `Dir` and nested replacement data can vary the raw
byte totals between runs, so those sizes are observations rather than cache
identity. The stable standard-library digest was
`9f6e5a946f474fd00c29440f2e31a2af667f1c0ffeeea0803122d7f94b7507df`.
The stable wrapper/module digest pairs were:

| Generated chain | Wrapper digest | Module digest |
| ---: | --- | --- |
| 64 | `e8cea91ab07c2df4d7d252cb9af61548de1ad7df3c7d96029e17209b722ba01e` | `e40ae733ae611e8dc64029f569a2b9fc98986e963c80ac1b85c560b8cf7acbf3` |
| 128 | `d17abe2c9edef9bb4027196472fbe2c51c0b18c14756b64422840414547ab282` | `00eb723d98043e3d4131f5e0f1e7da3e18cf5bdd7b48b5bc3d05cab8375f6af2` |
| 256 | `689083c0113b9b1223a26d7662deea46ecf9d915453c50fa2801dd8a397923ec` | `74a8e0258a453d9070806b136aeda76eda180eff09ee2f81619e082325586ceb` |

Go field selection is top-level only. Selecting `Error` still emits its whole
nested value, including `ImportStack` and `Pos`; selecting `Replace` still
emits the whole nested module, including its absolute `Dir`. Future S4
discovery therefore uses three independent bounds rather than treating field
selection as validation:

1. a hard raw-stdout byte cap below the JSON decoder, before token or record
   allocation;
2. separate package, module, selected-source-entry, and semantic-metadata
   counters; and
3. a separately bounded stderr collector that continues draining after its
   retained prefix fills.

The process owner creates stdout and stderr pipes explicitly, closes the
parent's writer copies after `Start`, and runs direct-child `Wait` concurrently
with both drains. After the direct child exits it gives inherited descriptors a
bounded drain interval, closes its readers, and joins context, decoding, raw
limit, semantic limit, stderr, close, and `*exec.ExitError` failures. This
settles the direct child and bounds descriptor retention; it does not claim to
terminate a descendant process tree or hard-bound subsequent consumer CPU
work.

The observer exercised 2 MiB stdout and stderr overflows, malformed JSON plus
stderr plus exit status 27, cancellation, inherited descriptors, and invalid
limits before process acquisition. It also compared projection and digests for
six target selections, but did not cross-build or execute those targets. Its
1,227-line proof harness had SHA-256
`6588117362bc6d0253b4c71c5076c7eb3e39a828d23d7c2a0376deb21a34c2ab`
and was removed after focused repeated, race, vet, format, and independent
semantic/lifecycle reviews passed. The receipt is structural mechanism
evidence, not retained performance, production S4, process-tree ownership, or
proof of production ceiling values or near-limit resource behavior.

#### Compose ceilings, not their simultaneous maxima

S4 keeps the general `Set` and `Layout` construction limits unchanged and adds
no public or producer-configurable budget. A valid general Layout is not a
promise that every consumer can accept it. Worker admission uses one private
complete selected non-standard source ceiling and actual checked totals:

```text
B = complete selected non-standard source ceiling (currently 1 GiB)
L = repeated bytes of every canonical Layout leaf
F = bytes of every selected non-standard source outside Layout

require L <= B
residual = B - L
require F <= residual
```

The subtraction occurs only after `L <= B`. `F` includes the fixed worker
wrapper, handler, schema, application support, and selected versioned dependency
sources. Repeated mounts consume repeated bytes and entries. Standard-library
Go, assembly, header, and embed inputs retain their separate toolchain closure
ceiling; non-standard cgo, foreign, assembly, and prebuilt inputs are rejected.
A maximum-size Layout plus any nonzero `F` therefore fails S4 even though the
Layout remains a valid value for another consumer. Raising the complete closure
to `L` plus another allowance is revisited only if a real application requires
simultaneous maximum Layout and fixed-source capacity.

Before classifying discovery records, the builder derives expected Layout keys
from the admitted main-module path, canonical mount, source kind, and basename.
Every key must be selected exactly once and byte-match its immutable Set.
Missing, duplicate, unreachable, target-excluded, or standard-classified Layout
leaves fail rather than freeing residual capacity. Every other selected
non-standard key consumes `F`; there is no language, producer, dependency, or
root-provenance exemption. Absolute `Dir`, `GoMod`, and nested `Replace.Dir`
values are admission paths, never source or build identity.

Raw package JSON, raw module JSON, stderr, retained semantic/key bytes, package
records, module records, selected source entries, replacement depth, total
module nodes, and aggregate module-file work are independent private ceilings.
An input must satisfy their intersection; the individual maxima are not a
capacity promise and need not be algebraically attainable together. This is
both simpler and tighter than copying Go's JSON/path escaping rules into one
worst-case formula. Retained semantic accounting charges constructed source-key
and path bytes before append or further allocation, not only strings as emitted
on the wire.

Module work needs its own aggregate ledger. V1 counts at most 16,384 top-level
records, permits replacement depth 0 through 16, and caps each `go.mod` read at
4 MiB. Those independent maxima permit an algebraic 1,088 GiB of module-file
hashing per capture before considering the raw-wire cap. S4 counts every nested
replacement node, caps aggregate unique `go.mod` bytes, and memoizes each
canonical module-file digest within one immutable-root capture. Repeated logical
nodes still frame that digest into module identity, but do not reread the same
file. Numeric module-work and count ceilings remain private S4 constants and
must be justified by representative closures before production promotion.

Layout classification, physical-byte equality, residual accounting, and source
hashing happen in each already-required pre/post closure capture. They do not
add a third Layout scan or copy source strings. Admission limits and observed
totals do not enter Build ID because they cannot change accepted output; current
admission is nevertheless re-enforced before cache selection. Retries and
target builds receive fresh counters and never attach mutable budget state to
the application's root lease or to `preparedworker`.

The cheapest implementation proof is a private pure ledger with injected small
ceilings. It accepts `L=B,F=0` and `L=B-1,F=1`; rejects `L=B,F=1`; proves repeated
mounts, external dependencies, standard separation, missing/duplicate leaves,
permutation and root-prefix independence, checked arithmetic, derived-key
accounting, total replacement nodes, and aggregate module-file bytes. It belongs
inside the eventual S4 production seam, not a public budget package or another
retained process preflight.

#### Keep initial S4 replacements versioned-only

The initial S4 builder accepts ordinary versioned modules and only
module-to-module replacements whose replacement version is nonempty. After one
bounded parse of the stable main `go.mod` with the selected Go tool, every
versionless replacement RHS fails before module/package discovery, build, cache
selection, or access through that path. This rejects both absolute filesystem
targets and relative targets such as `./deps/local`; containment inside the
application root is not an implicit exception. The current V1 implementation
is unchanged by this proposed V2 policy.

Versioned-only is not permission for network or ambient cache state. S4 still
needs one explicit immutable offline module-cache owner, fixed no-network
environment, exact module attribution, selected-file hashing, and complete
pre/post recapture. Application-local packages can remain in the one minimal
main module. A dependency that needs a separate module boundary must be
provisioned as a versioned immutable module input. This policy belongs to the
Go delivery consumer, never a language producer, and adds no public
replacement switch.

A 1,757-line Darwin/arm64 Go 1.26.4 disposable at SHA-256
`bb3541bf209b34757857fd626e8d472e310844698e7d93c3269858dd2c7372d6`
tested the strongest narrower alternative. It used one main-module parser and
no child parser, two unrelated roots with independent build and module caches,
strict downward exact-spelling paths, no-follow/link-count-one file reads,
ordinary and replaced versioned modules, exact one-hop attribution, and
complete captures around the build. It deliberately accepted a mismatched
child module line and ignored child replacement directive because Go owns
those semantics. The contained route needs no subtree copy or walk and adds no
Go subprocess beyond the parse already required by versioned-only.

Independent review nevertheless rejected that harness as acceptance evidence.
It treated every `EmbedFiles` entry as a basename, while ordinary Go reports a
valid nested embed such as `assets/message.txt` as a package-relative slash
path. It also charged selected-source count and aggregate bytes only after
opening and reading the next file, and its proof Layout lacked an aggregate
byte ledger. Flattening embeds would create an Ember-specific Go dialect, so
the fixture cannot establish ordinary Go behavior or charge-before-work
admission. The harness was removed after this decision rather than repaired a
second time.

Versioned-only rates **9.6/10 for fail-closed initial safety** and **7.2/10 for
local-development usefulness**. The corrected one-root contained design rates
**8.9/10 as a revisit architecture**, while this exact evidence rates
**6.8/10**. Unrestricted filesystem replacements rate **3.5/10** and
builder-owned external snapshots **4.8/10**. Revisit contained replacements
only for a concrete application that cannot use one main module or provisioned
versioned inputs, and only with nested-embed support, admission before every
read/allocation, aggregate Layout/key/path work, enforceable root/ABA and
hardlink ownership, portable no-follow/reparse proof, and retained fresh-cache
multi-target measurements. Without every premise, versioned-only remains the
fail-closed result.

The first embedded-release materializer remains a different effect owner: it
owns one fixed generated leaf and never scans or deletes unrelated application
files. A same-directory rename gives atomic namespace replacement on the
supported POSIX path, but Go makes no equivalent Windows atomicity or
power-loss durability promise. Before an embedded producer emits multiple
leaves, that materializer must add an explicit generated-file index,
complete-plan staging, managed-leaf deletion policy, and crash recovery.

The hybrid image is adopted only after pinned production-size measurements
show a meaningful generation/compiler-memory or build-time improvement without
materially worsening start-to-READY time, RSS, executable size, or steady
execution.

### Make the worker builder language-neutral

`preparedworkerbuild.Options` should eventually replace its singular Luau
`Program` with one canonical prepared-source layout:

```go
type Options[Q, R, C, E any] struct {
    Layout        preparedsource.Layout
    Contract      preparedworker.Contract[Q, R, C, E]
    ModuleDir     string
    WorkerPackage string
    GoCommand     string
    Target        Target
    // output and explicit bounds
}
```

The builder:

1. admits one application-owned real module root and verifies every canonical
   Layout path and byte string before and after the build without writing the
   root;
2. runs ordinary overlay-free Go discovery and compilation, requires every
   supplied Go/embed leaf to appear exactly once in the selected dependency
   closure, and rejects unused or target-excluded inputs;
3. rejects cgo, foreign, assembly, and prebuilt inputs under the portable
   worker policy;
4. binds the layout digest, complete selected source and embed closure, selected
   module graph, typed contract, strong toolchain digest, target features,
   protocol, tags, and fixed build policy into build identity; and
5. never imports a language runtime, calls a language compiler, interprets a
   language ABI, copies an arbitrary caller module, or owns source-root
   materialization.

Selected-leaf accounting keys are Go import path, source kind, and basename,
not the absolute package `Dir`. The disposable observer exposed macOS's
equivalent `/var/...` and `/private/var/...` spellings; allowing a staging-root
spelling into identity would make a physically identical root appear absent or
different. Absolute root paths remain admission inputs only.

The standalone module is minimal rather than a mirror of the application's
complete development tree. It contains the fixed worker application sources
and physical Layout leaves; immutable versioned dependencies remain ordinary
module inputs authenticated by the builder. Initial S4 admits only versioned
module-to-module replacements. It rejects every versionless filesystem target,
including a relative target contained in this root, before discovery. No public
general filesystem materializer, local-replacement option, or root-manifest
abstraction is introduced.

Language semantic identities stay in generated artifacts and are checked by
the concrete generated/runtime adapter before worker `READY`. The private
builder descriptor and manifest move atomically to a new language-neutral
version and remove Luau recipe, ABI, Program, and Proto fields; there is no dual
public builder shape. There is no producer-supplied opaque component identity:
any input able to change output must be represented, selected, and hashed.

Identity has one owner at each level:

| Identity | Owner | Covers |
| --- | --- | --- |
| Set digest | language producer through `preparedsource` | package name and exact canonical generated files |
| Layout digest | application through `preparedsource` | canonical mount paths and Set digests |
| Build ID | `preparedworkerbuild` | layout, selected closure/module graph, contract, toolchain, target, protocol, and fixed flags |
| Program/image identity | concrete language adapter | semantic ABI, program inventory, and generated-function binding |
| Live generation | `preparedworker` | verified Build, checkpoint position, and transaction lifetime |

`Set` and `Layout` use framed delivery identities. They intentionally do not
equal the legacy generated-source digest, and descriptor V2 is an explicit
cache-identity break. Language semantic identities remain separate and are
still checked before `READY`.

Output directory, publication name/path, and maximum executable bytes are
publication or admission policy, not compiler-output identity; they do not
fragment safe cache reuse. The executable bound is enforced again whenever a
cached build is selected.

The root path and staging name likewise are not build identity. S4 may remove
V1's separate generated-source digest pass because Layout bytes can be checked
while the selected closure is hashed, but it must retain complete pre/post
source, module, and toolchain recapture. The selected bound contract resolves
the apparent Layout/closure mismatch without changing either general value
policy or public API: actual Layout bytes consume the complete non-standard
ceiling first, and every other selected non-standard source consumes the
checked residual. Raw and retained-semantic byte caps, package/module/source
counts, replacement nodes, aggregate module-file work, and stderr are
independent admission ceilings rather than promises that all maxima compose.
Their numeric production values and near-limit resource behavior remain S4
acceptance evidence, not architecture inferred from the small projection
fixture.

Before the ADR 0011 worker baseline can be considered proved, S1 below closes
current output-identity gaps without waiting for language generalization:

- require an explicit exact `GoCommand`, resolve it once, invoke only that
  executable, and set `GOTOOLCHAIN=local` so it cannot silently select another
  toolchain;
- derive actual arguments, environment, and canonical identity fields from one
  private immutable build policy, including `-pgo=off`, `-buildmode=exe`, the
  baseline ISA tier, `CGO_ENABLED=0`, `GOWORK=off`, `GOENV=off`, empty
  `GOEXPERIMENT`/`GOFLAGS`, fixed FIPS/external-link policy,
  `-mod=readonly`, `-trimpath`, and `-buildvcs=false`;
- include selected `HFiles` and selected standard-library Go, assembly, embed,
  and header inputs in the appropriate closure rather than skipping them; and
- bind the exact Go executable, the bounded complete `GOTOOLDIR`, selected
  standard-library closure, and `GOROOT/pkg/include` strongly enough that
  toolchains reporting the same short version cannot share an identity.

The builder recomputes toolchain, source, and module inputs after the build and
rejects drift without replacing the previous selection. S1 hardens the current
private descriptor V1; S4 carries the same policy and closure into V2 before
deleting V1. Every hardening survives even if multi-language work is abandoned.

Portable hot reload defaults to `-pgo=off` and the baseline ISA tier. A later
explicit optimization profile may select exact PGO bytes and a supported
`GOAMD64`/`GOARM64` tier only when the profile digest, tier, toolchain, and all
derived flags enter build identity and the launch host admits the tier before
`READY`. The builder never uses `-pgo=auto` or host-native feature discovery.
Profiled or higher-ISA builds are pinned release/candidate choices, not hidden
defaults, and must clear their separate steady-speed versus build-latency gate.

### Keep reload at the application transaction seam

One worker generation contains every language used by one application
transaction. The application handler may call Luau, Ruby, and Sprig in an
explicit order, combine their closed results, and return one canonical result,
checkpoint, and ordered effect outbox.

Languages do not call each other through a shared heap. Fine-grained language
RPC, direct guest-value exchange, and one worker process per language are not
part of V1. Cross-language communication uses application-owned closed records
or explicit static Go adapters. Those adapters detach or copy owner-bound data
when necessary; no guest handle, callback, continuation, or owner-backed memory
enters another language or the worker decision.

The handler invokes concrete languages sequentially with the same context and
explicit per-language limits. Expected rejection remains a typed value. If any
language returns a Go error, the handler returns no decision and the existing
worker contract marks the generation terminal; it does not attempt partial
language rollback or continue with another owner.

Reload continues to replace the complete worker generation:

1. compile changed language inputs independently;
2. assemble one fresh minimal physical worker module from the selected
   canonical source `Layout` and build one executable from it;
3. launch and validate an inert candidate;
4. restore the latest detached checkpoint and follow later commits;
5. activate only at a caught-up quiescent head; and
6. retire and reap the old process.

No guest heap, frame, closure, callback, coroutine, module cache, or generated
function pointer crosses generations. This preserves ADR 0011's transaction,
uncertainty, activation, and process-lifetime decisions unchanged.

The composite checkpoint is the sole durable application state across reload.
Every language projects required persistent state into that bounded closed
record. Dynamic heaps, identities, classes, monkey patches, callbacks, fibers,
caches, and continuations are generation-local and are reconstructed
deterministically from checkpoint plus code or deliberately discarded. The
concrete handler owns every language owner. Its factory constructs them in a
deterministic order; its `Close` path closes them in reverse order on restore
failure, terminal failure, candidate rejection, retirement, and runner close.

### Preserve zero shared-delivery-seam tax

`preparedsource`, mount validation, build identity, and reload terminate before
guest execution. They add no steady language dispatch, guest tags, boxing,
codec, allocation, or synchronization to a direct embedded generated call. A
release may bind the same generated packages directly in-process; development
hot reload pays one EPW2 exchange per application transaction, never one
exchange per language call.

This claim excludes real semantic work: concrete runtime operations,
cancellation polling, language-specific limits, application call ordering, and
typed detachment/copying remain visible costs. Prove the narrower claim both
structurally: a direct generated invocation has no dependency or call edge into
`preparedsource`, build, worker, registry, or shared codec code; and empirically
through allocations, profiles, and direct-versus-composed benchmarks.

The working-tree S5a fixture now proves the structural half for one external
static application. After materialization it deletes the producer module and
uses a fresh offline cache to list, test, build, inspect, and execute an
application whose exact non-standard dependency set is only its main package
and two concrete generated packages. Build metadata proves `CGO_ENABLED=0`;
six target builds cover Darwin, Linux, and Windows on ARM64 and x86-64. This
does not prove a hot-path cost bound, worker composition, or language
performance; those still require retained profiles and comparative receipts.

Retained Luau evidence already places the direct typed-Go ceiling at roughly
`0.011x` to `0.095x` pinned Luau and the proved private prepared-Go worst
median at roughly `0.187x`. It also found no architectural advantage for an
equivalent assembly or executable-memory leaf. This is evidence that the
shared static seam is not the current steady-state bottleneck; it is not Ruby
or Sprig performance evidence.

The internal Sprig proof adds bounded local evidence, not a retained release
receipt. On the dirty Apple M1 working tree, the latest seven-round project
gate placed generated execution at a median `269.61x` the evaluator and
`1.069x` equivalent context-polled handwritten Go. The corresponding fixed
single-package gate was `239.78x` and `1.100x`; one noisy round exceeded the
overhead threshold, while the specified median passed. The scalar path remains
`72/0/0` evaluator/generated/handwritten allocations and the detached
one-rejection path remains `1/1` generated/handwritten. Exact commands and the
retained-proof limitation are in `docs/checks.md`.

The exact local Go 1.26 cache observer for the two-package application recorded
compile actions for `counter/math/main` of `1/1/1` from a fresh cache, `0/0/0`
when warm, `1/0/1` after a counter-only change, and `1/1/1` after a math
dependency change. `go list -export -json` BuildIDs and executable SHA-256 also
changed or remained stable as attributed. These are structural observations
for this toolchain and project, not a stable Go interface, wall-time proof, or
production-size build claim.

The topology is therefore near the portable performance frontier, not declared
unconditionally optimal. Hold the shared seam fixed while testing owner-local
reuse and sealed dynamic-language images. A broader common runtime, semantic
IR, or backend interface is not a performance optimization: it would add work
to the hot path or move language-specific recovery behind indirection.

Performance work therefore stays on three concrete planes:

1. each language owns guarded specialization, exact cold recovery, compact
   values, and owner-local reusable hot state;
2. each independently changing language/component gets its own generated Go
   package so ordinary Go package caching can reuse the others; file sharding
   controls compiler memory but does not create cache granularity; and
3. generation, list/hash, compile, link, launch-to-READY, restore, catch-up,
   warmup, peak RSS, and steady execution are measured separately before a
   build/startup mechanism is promoted.

Splitting one language into additional generated packages remains conditional:
it must improve incremental build-to-READY enough to pay for exported seams,
lost inlining, wrapper code, and identity complexity. Direct `go tool link`,
prebuilt archives, a persistent compiler daemon, preforked workers, or package
per function/Proto are not architectural defaults.

### Stage package symmetry instead of adding a facade

The current root package remains the Luau implementation while this ADR is only
proposed. Moving 388 root Go files now would add migration cost without proving
a language seam.

When a second first-party language clears its worker/public promotion gate,
move Luau to `github.com/besmpl/ember/luau` before publishing the
multi-language surface. The private D1 nucleus is intentionally below that
threshold. Do not
retain a root compatibility facade. The target topology is:

```text
github.com/besmpl/ember/luau
github.com/besmpl/ember/ruby
github.com/besmpl/ember/sprig       // working name only
github.com/besmpl/ember/preparedsource
github.com/besmpl/ember/preparedworker
github.com/besmpl/ember/preparedworkerbuild
```

The working-tree external fixtures now prove both levels. A static producer and
a real independently versioned compiler can depend only on `preparedsource`,
contribute generated code, and disappear before steady execution. The compiler
owns a bounded Seed syntax, checked nominal facts, evaluator, domain behavior,
and direct-Go emitter; Ember supplies no Backend adapter. The application
composition proof then combines real Sprig and Seed compiler results through
one Layout and direct typed handler with zero allocations and 1.024x local
overhead relative to the same concrete calls. It adds no cross-language ABI,
package graph, registry, or adapter. The application
selects `preparedworkerbuild` and `preparedworker` when it wants supervised
reload. Shared compiler packages remain uncreated until their promotion gates
are met.

## Ratings

Scores describe the current evidence, not permanent preferences. The earlier
9.7 whole-architecture score conflated a strong seam placement with incomplete
ownership, failure, image-validation, and delivery proof; the revised score
separates those claims.

| Choice | Rating | Reason |
| --- | ---: | --- |
| Revised architecture contract as a whole | 9.8/10 | Luau, Seed, multi-package Sprig, Ruby-owned dynamic AOT, embedded Ruby/Sprig transactions, and bounded Ruby graph projection agree on the post-lowering/application-record seams; process-worker composition, production Ruby/Sprig, and retained release proof remain, so it is still not "golden." |
| Language-owned compiler/runtime modules plus generated-source seam | 9.9/10 | Static and dynamic languages preserve semantic locality, exact recovery, and near-handwritten hot code with no shared-seam guest tax. |
| Immutable one-package `Set` plus canonical application-owned `Layout` | 9.9/10 | Luau, Seed, Sprig, mixed composition, and actual Ruby semantics preserve language identity while the application alone owns placement; S4's physical worker-module consumer still decides final retention. |
| Lowercase-ASCII V1 names with no normalization | 9.0/10 | Makes byte identity equal portable name identity and keeps construction OS-free; future target names require a policy revision. |
| Evolve `PreparedGoArtifact` to own `Set`, with temporary V1 writer adapters | 9.5/10 | Proves the seam without a second Luau result or duplicate retained source bytes; the adapters have an explicit S3b deletion gate. |
| Artifact-only external producer seam | 9.7/10 | The producer owns only package Sets, the application owns Layout, and all producer/delivery code disappears before static execution. |
| Concrete external compiler producing `Set` directly | 9.8/10 | A separate module implements its own checked source language and near-handwritten direct Go using only one post-lowering dependency; compiler, source, and delivery code disappear before build. |
| Explicit application-owned mixed handler | 9.9/10 | Independent Sprig/Seed static packages and real generated Ruby/Sprig owners both compose through concrete policy. The latter restores one detached record, allocates zero in the inner transaction, and measured 1.014x its equivalent direct composition without shared invocation machinery. |
| Bounded application-owned mixed checkpoint C0 | 9.8/10 | One versioned Ruby/Sprig record restores a fresh Ruby owner, commits and replays atomically through `OpenEmbedded`, and discards partial Ruby mutation on failure; full heap projection and process generation swap remain unproved. |
| Bounded application-owned Ruby graph projection RP0 | 9.9/10 | A canonical 52-byte record preserves aliases, equal-but-distinct identity, a cycle, and reconstructable captured self across independent v1/v2 Sets; v2 code wins, each application/generated boundary rejects the invalid state it owns before publication, and the actual restored-v2 warmed call remains near explicit Go. The proof is one fixed graph, not a production heap serializer. |
| Generated scalar-only projection interface | 9.9/10 | Each generation exposes only three named scalars plus owner lifecycle and hides initial application values, application IDs, edges, behavior tags, object pointers, and proof telemetry. Pairwise topology validation and reconstruction make this a deep language-owned module rather than a second application schema. |
| Minimal application-owned standalone worker module in a fresh physical root | 9.6/10 | A disposable real-Go observer proves physical package/embed discovery, root-location BuildID reuse, generation isolation, and fixed worker sources plus canonical Layout bytes. Expected build economics rate 9.0/10 and third-party extensibility/reload 9.8/10; production S4 and retained route benchmarks remain absent. |
| Field-selected streaming Go discovery for S4 | 9.6/10 | Exact top-level projection preserved every consumed value and digest while removing controlled quadratic `Deps` output. A pre-decode raw cap, separate semantic counters, and explicit direct-child ownership make the route fail closed; implementation feasibility rates 9.2/10, while retained production proof remains. |
| Checked complete-closure residual plus conjunctive discovery/work ceilings | 9.8/10 | Actual Layout bytes and every other selected non-standard byte share one private cap; raw, retained metadata, counts, stderr, replacement nodes, and aggregate module work fail independently. This preserves general values, adds no API or language share, and avoids a third source pass. |
| Versioned-only module replacements for initial S4 | 9.6/10 safety | One exact main-module parse rejects every filesystem RHS before discovery and avoids another path, hardlink, ABA, and source-root owner. It still requires an explicit immutable offline module cache and fixed no-network policy. |
| Logical application-scoped sealed module-supply view | 9.6/10 | Preserves native Go sums, module roots, replacements, and selected-source identity while physical cache roots and unrelated immutable cache entries remain outside identity. One read lease is reused across generation roots and released before reload; production and retained performance proof remain. |
| Vendor tree as the general S4 supply route | 7.2/10 | Excellent one-root ownership and small selected-package storage, but Go 1.26 cannot natively list the full vendored `all` graph and omits dependency/replacement sums, `Dir`, and `GoMod`; every fresh root also rematerializes vendor bytes. Keep it as a bounded fallback. |
| Ambient or target-writable `GOMODCACHE` | 0.5/10 | Can download, lock, extract, mutate, or disappear during discovery/build and couples identity to unowned machine state. Provision separately and consume only a leased immutable epoch. |
| Narrow contained-relative replacements as a revisit route | 8.9/10 design; 6.8/10 current evidence | One leased root can avoid copying or publishing application-local modules, but the reviewed proof rejected valid nested embeds and admitted aggregate source work too late. Revisit only for concrete need plus portable root and retained performance proof. |
| Unrestricted filesystem replacements | 3.5/10 | Absolute/outside roots, mutable aliases, path-dependent identity, and extra cleanup authority violate the closed standalone-root transaction. |
| Guarantee every maximum Layout with another fixed-source allowance | 7.0/10 | Coherent if a real application requires it, but no such capacity promise exists; it needs a new measured allowance and raises worst-case hashing above the current complete-closure ceiling. |
| Derive raw JSON capacity algebraically from count maxima | 2.5/10 | Whole nested errors/replacements, absolute paths, escaping, and derived retained keys would duplicate Go encoding policy and still require independent safety caps. |
| Public or language-specific source/discovery budgets | 0.5/10 | They expose private builder safety policy, fragment extension ergonomics, and leak Ruby/Sprig/producer identity into a post-lowering delivery seam. |
| Retain unrestricted fully buffered Go discovery in S4 | 3.0/10 | It is functional in V1, but asks Go to compute and emit unused recursive dependencies, admits semantic counts only after whole-output allocation, and duplicates the bounded stream in memory. |
| Application-owned snapshot of the complete main module | 8.3/10 | Semantically direct and isolated, but coherent snapshotting and broad copy cost remain application/build-system problems. |
| Builder-owned whole-module shadow copy | 5.5/10 | Isolates generations but makes the builder traverse and snapshot arbitrary caller source, symlinks, nested modules, ignored files, and potentially 262,144 entries before its existing closure scans. |
| In-place multi-leaf generated-tree mutation | 3.8/10 | Uses ordinary Go discovery but lacks one portable atomic namespace commit, exposes partial generations, requires stale-file recovery, and collides across concurrent builds. |
| Per-leaf placeholders plus Go overlay | 2.5/10 | Every dynamic basename and asset must already exist physically; it duplicates Layout inventory and is strictly more machinery than writing the real immutable bytes into a fresh root. |
| Nested generated modules or `go.work` | 2.2/10 | Changes module/import/`internal` policy, conflicts with fixed `GOWORK=off`, constrains arbitrary mounts, and adds graph identity without solving placement. |
| Public general filesystem materializer now | 3.2/10 | Real consumers have different ownership, symlink, replacement, deletion, durability, and recovery contracts; a shared effect API would be shallow. |
| Add a parallel generic prepared-result type beside `PreparedGoArtifact` | 2.5/10 | Creates two identities and two producer paths before a second language exists. |
| One fixed owned leaf, exact header, and shared pure embedded-file plan | 8.8/10 | Smallest fail-closed materializer for the real one-file producer; portable path admission stays outside the pure layout value. |
| Add a sidecar ownership manifest for the one-leaf S3a producer | 4.0/10 | Creates a second commit and recovery state without deciding any current ownership question. |
| Add a generated-file index before any producer emits multiple leaves | 5.0/10 | Correct eventual mechanism, but premature for one fixed leaf; it becomes mandatory before multi-leaf output. |
| `Set` without `Layout` | 5.5/10 | Leaves every embedded and worker consumer to duplicate placement, collision, ordering, limits, and layout identity. |
| `Layout` without `Set` | 4.5/10 | Cannot give language producers one reusable package-owned immutable result without exposing application placement. |
| Pointer-style mutable artifact objects | 7.0/10 | Cheap to pass, but add nil/lifetime/mutation questions that the opaque immutable values avoid. |
| One opaque package blob | 2.5/10 | Hides file selection and prevents safe embed handling, deterministic physical placement, and selected-closure proof. |
| Language-neutral builder and descriptor V2 | 9.6/10 | Reuses the proved Go/toolchain/cache effect owner, verifies but never writes the application root, and deletes Luau knowledge. |
| Existing typed transaction and whole-worker reload | 9.8/10 | Already language-neutral and preserves atomic state/effects. |
| Current worker candidate implementation | 8.7/10 | Strong P1-P3 code/tests and a local S1 candidate; retained clean cross-platform acceptance remains. |
| Current retained S0 proof | 4.0/10 | No clean exact-revision P4/P5 platform, resource, performance, and final-check receipts. |
| Separate S1 identity closure before S0 | 9.4/10 | Correctness work survives S4 and prevents invalidating later acceptance captures. |
| Fold S1 identity repair into S4 | 3.8/10 | Couples two public migrations, obscures failures, and leaves the current baseline claim unsound. |
| Exact Go plus bounded whole `GOTOOLDIR`, selected stdlib, and include tree | 9.1/10 | Strong same-version closure without hashing unrelated docs, tests, or platform trees. |
| Exact Go plus a hard-coded compiler-tool list | 6.8/10 | Cheaper to hash but silently ages when Go changes helper names or invocation topology. |
| Exact Go plus the whole `GOROOT` | 5.8/10 | Conservative but needlessly invalidates cache entries and hashes unrelated bytes. |
| Host-supplied toolchain label without an immutable CAS | 2.0/10 | Fast but merely moves the unproved identity claim outside the builder. |
| Persistent toolchain fingerprint cache by path/mtime | 4.5/10 | Avoids hashing but expands invalidation and TOCTOU policy before build latency proves a need. |
| Detached application-record language seam | 9.9/10 | C0 proves typed scalar transaction state and RP0 proves bounded graph topology plus behavior reconstruction while heap identity/code stay generation-local; arbitrary heap projection and process reload remain. |
| Concrete per-language owner/cancellation/limit/error contracts | 9.8/10 | One context, distinct Sprig/Ruby budgets, typed early outcomes, poison-on-host-abort, retryable close, lost-generation handling, and detached decisions pass without giving pure Sprig a synthetic owner. |
| Immutable return-by-value Sprig baseline | 9.6/10 | The narrow internal slice proves semantics, limits, detachment, standalone static delivery, zero scalar allocations, and local near-handwritten speed without lifecycle ceremony. |
| Direct checked Sprig syntax/project facts to typed Go | 9.7/10 | Frozen nominal facts feed evaluator and emitter directly; another IR would currently duplicate traversal and weaken locality. |
| Concrete Sprig `Project`/`PreparedPackage` result | 9.6/10 | It owns canonical language identities and requested standalone or linked Sets but no Layout, module, build, or cache policy. |
| Private language-owned checked project graph | 8.8/10 | Required for nominal references, diagnostics, closure planning, and the evaluator; it is implementation, not a shared compiler interface. |
| Conditional Sprig `Engine.ReduceInto` | 2.0/10 | The baseline already has zero scalar and one detached-output allocation; defer or delete owner state absent a measured 5 percent or material RSS win. |
| Direct generated Go as common native target | 9.9/10 | Sprig, Seed, and guarded Ruby all produce portable cgo-free static code at near-handwritten local cost without a common Backend API. |
| Hot Go plus sealed dynamic-language image | 7.5/10 | Best build/startup candidate, but codec safety and material benefit are unproved. |
| Concrete Ruby-owned runtime plus guarded prepared paths | 9.8/10 | D1 retains R0's identity, block, targeted-return, rescue/ensure, and exact-recovery proof while adding inherited lookup, snapshot alias/remove/undef state, public/protected/private visibility, literal `send`/`public_send`, selective invalidation, independent fixed generated cells, and direct target IDs; C0 adds scalar restore and RP0 one bounded graph/code-generation migration. Public/full Ruby, a production heap checkpoint, and process reload remain. |
| Ruby-private target-ID PIC representation | 9.6/10 design; 9.7/10 semantic/representation shape | Fixed owner-local scalar arms select generation-local definition/body IDs whose site-local switch directly calls named bodies; one target can serve several separately validated dispatch/shape/receipt matches. Bounded D1 proves canonical recovery, selective repair, bounded growth, disposal, corruption rejection, and zero warmed allocations. Target37 removes generic recovery from bounded warmed hits without changing this authority; broader latency remains unproved. |
| Target37 specialized lowering on the current Target39 artifact | 9.9/10 exactness; 8.8/10 hot shape; 8.2/10 depth; 9.3/10 locality; 9.0/10 production economy | Three tiny one-charge wrappers feed three strict selector/site/mode-specialized leaves and one generated cold authority; attachment state stays out of warmed code. Two current-identity local captures clear the 5% and 1.15x gates at 1.62-1.69x the generic helper shape and within 1.073x independent equivalent Go, with zero allocations. The scope is only pre-repaired alternating bounded hits, not topology transactions, standalone R1, or release evidence. |
| Current strict per-site leaves under Target38 multiplicity | 9.9/10 semantic exactness; 8.8/10 bounded hot shape; 9.6/10 locality; 9.0/10 current economy; 4.2/10 repeated-template scaling | This remains production: the current three leaves are intentionally different, direct, and site-local. Exact C4 repetition is approximately linear, but at 96 sites it pays 121,068 source bytes, 697,120 archive bytes, and 75,008 text bytes over the conditional shared prototype. |
| Conditional exact generated-leaf-template interning | 9.6/10 semantic design; 9.2/10 scaling; 8.5/10 conditional overall; 7.8/10 promotion evidence | Share only alpha-equivalent generated bodies after substituting the independent site pointer and attribution; exact equality, not selector/mode metadata, is authority. It materially wins at 96 sites with no runtime plan or shared state, but production waits for the at-most 2% warmed-hit pointer-parameter gate. |
| Array-backed Ruby site storage after leaf interning | 5.5/10 | At 96 sites it removes another 10.44% source but only 2.39% archive, 0% site text, and 0.78% binary. Named fields are simpler and more inspectable until retained construction or artifact evidence materially changes the tradeoff. |
| Bounded Ruby open-world PIC performance route | 7.2/10 current evidence | A three-class preflight validated fixed arms, uncached fallback, and pre-effect single-arm repair, but its self-comparing mixed-workload benchmark was rejected. Promotion requires a direct-target, independently compared, pre-warmed R1 observer. |
| Standalone language-owned evidence fixture | 9.6/10 provisional topology | Separate canonical/generated/handwritten binaries, external identity envelopes, and receipt-only comparison best close the stale/self-confirming observer failure. The first skeleton implemented the rejected spawning-test topology and failed P+1-P+5; implementation is now deferred until production Ruby semantics make the representation decision-valued. |
| Defer Ruby observer infrastructure | 9.6/10 current priority | Target IDs remain private and reversible, while A2/B require a large new portable process/provenance owner before flat-class timing can choose production policy. Reopen only at the named R1 semantic milestone; open-world evidence stays 7.2/10. |
| Bounded Ruby D1 nucleus | 9.8/10 decision value, 8.4/10 economy | Inheritance, eight generated selective cells, transactional ancestor and include/prepend mutation, snapshot alias/remove/undef, visibility mutation, and three real direct-target sites are implemented and focused-green. This raises architecture confidence but not the 7.2/10 open-world timing evidence. |
| D1 Ruby lookup/dispatch-first ordering | 9.8/10 architecture leverage | The bounded inherited/local-entry/visibility/module nucleus and Target40's preassigned allocation-site singleton row are implemented. Measure that same private boundary before H1 heap/GC, P1 broader projection, or a polymorphic performance claim. |
| Target41 implementation of Target40's eager allocation-site singleton row | 9.6/10 bounded overall; 9.8/10 exactness and warm-hit design | One exactly-once allocation receives a unique immutable dispatch row but retains its class/layout shape; literal zero-capture define is `K=1`, the peer row stays unchanged, and no object map/ID reaches the hot path. Exact canonical/generated/CRuby results, atomic failure, close, inventory, and artifact receipts pass. This does not solve general eigenclass allocation, capture rooting, reflection, or GC. |
| Target42 retained direct singleton warmed leaf | 9.7/10 bounded measured shape | Two source-authenticated, cgo-free, single-P, PGO-off captures clear the conservative 15th-of-20 Current/Equivalent-Go bound at 1.045358 and 1.005731 with zero allocations. Keep the explicit scalar leaf and whole-site fail-before-effect validation unchanged; the result covers only the warmed receiver-after-definition state. |
| Target48 independent H1b observer | 9.7/10 structural observer; 5.5/10 revolutionary evidence | A same-layout but implementation-independent comparator now proves a balanced warmed 64-call selection vector and a 29,360,128-byte sequential-bank three-collection workload with exact checksums, final-state receipts, isolation, and zero timed allocation. Cache behavior remains host-dependent and unclaimed. Retained timing remains NO-GO until the preregistered paired BCa/noise protocol is executable and a qualifying controlled host exists. |
| Dense eager Ruby effective-resolution cells | 9.8/10 semantic architecture, 9.7/10 steady-hit design | The bounded generated proof implements ten owner-local cells and selective epoch advance for two constant selectors across define/alias/remove/undef, visibility, include, prepend, later module definitions, and the one singleton define. Target37/38 remain historical pre-Target39 lowering evidence; representative matrix admission and open-world latency remain unproved. |
| Target39 attachment descriptors plus activation bits | 9.8/10 semantic/lifetime proof; 9.2/10 provisional representation selection; 9.6/10 conditional product trajectory | Eight node descriptors including the Target41 singleton descendant, two attachments, live/staged bits, transient nonallocating route derivation, exact all-mask oracles, legacy four-row `K=0/2/0/1/2`, and full five-row `K=0/3/0/1/3` publication pass. No elapsed-time or standalone-artifact superiority is claimed. |
| Passive checked Ruby lookup image plus one owner per lane | 9.9/10 semantic authority, 9.8/10 hot-hit shape | One private fact product feeds canonical construction and a smaller emitted closure without a runtime ABI. Independent lookup, topology derivation, mutation, fallback, target policy, all-mask comparison, and corruption tests now pass for the bounded nucleus; generated warmed leaves contain no topology dependency. |
| Explicit standalone versus linked Sprig products | 9.7/10 | The lifecycle distinction is real, caller-visible, and smaller than a mode object or adaptive planner; both reuse one checked graph and direct-Go emitter. |
| Self-contained one-Set standalone entry closure | 9.4/10 | Best location-neutral artifact: no application paths, direct same-package calls, and one independently mountable Set. It is no longer the universal shared-project default because measured duplication is material. |
| Application-bound linked project preparation | 9.6/10 | Exact caller bindings, requested transitive packages, nominal call symbols, localized Set identity, and large measured build/size wins without a runtime seam justify the small concrete API. |
| Emit every checked package when only some are requested | 2.0/10 | Deleted behavior: it wastes generation and compilation and is unnecessary for either explicit product. |
| One fused `Set` per whole language project | 1.5/10 | Disposable evidence found 1.392x medium and 1.975x large peak RSS plus project-wide invalidation; linked packages capture the wins without destroying locality. |
| Hidden adaptive package-shape planner | 2.5/10 | The local crossover is workload/toolchain sensitive; hidden policy would destabilize identity and make builds surprising. Explicit products are simpler. |
| Shared cross-language generated-package graph now | 0.8/10 | Real Sprig/Seed composition needs only independent Sets and application mounts; a shared graph would model no cross-language edge and duplicate Layout plus handler policy. |
| Package per function or declaration | 1.0/10 | Maximizes package/tool overhead, loses inlining, and exposes internal declarations for speculative cache granularity. |
| Private pure scalar IR after two producers | 2.0/10 | Seed and Sprig overlap on checked `i64` add/divide, but a new IR would add a traversal and data model without a second optimizer or emitter; their package, ABI, polling, and diagnostic invariants already differ. |
| Shared source/span/emitter mechanics after three producers | 1.8/10 | Seed, Sprig, and Ruby share only shallow standard-library hashing/formatting plus `preparedsource`; syntax, facts, diagnostics, owner/control semantics, ABI, guards, and emission policy differ. |
| Explicit identity-bound PGO and ISA profile | 7.2/10 | Useful pinned release option, not a portable hot-reload default. |
| Source types inside effectful `preparedworkerbuild` | 7.2/10 | Fewer packages but wrong dependency direction for embedded generation. |
| Producer-owned mounted or multi-mount artifact | 3.5/10 | Mixes compiler identity with application placement and prevents reuse at another mount. |
| Public compiler kit or scalar IR now | 0.5/10 | Three unlike real producers succeed with `preparedsource` alone; callbacks would freeze shallow policy rather than remove a complete duplicated operation. |
| Public semantically complete typed Core IR | 0.2/10 | The Ruby proof needs object/method/block/unwind intrinsics that Sprig must not pay for; a complete Core becomes a tagged union of languages. |
| Universal build-time `Backend` interface | 0.3/10 | Its universal program parameter recreates the rejected common-IR problem while direct producers already emit the only shared target. |
| Runtime `Language` interface or registry | 0.0/10 | Adds hot-path indirection and hidden discovery while masking incompatible semantics. |
| Generic cross-language `Compose` facade | 0.0/10 | The real handler is smaller, statically typed, application-policy-bearing, allocation-free, and already within 1.024x of its direct calls. |
| Plugin, `init` registration, cgo, or dynamic loader | 0.0/10 | Violates portability, lifetime, identity, and explicit-effect constraints. |
| One process per language | 3.0/10 | Adds IPC and distributed-commit problems without isolation value here. |

Applicable Spinel mechanisms remain narrower. This comparison is frozen at
[`matz/spinel` commit
`21ba28f7c3e33de47f14fe5bc222296f73f3cc35`](https://github.com/matz/spinel/tree/21ba28f7c3e33de47f14fe5bc222296f73f3cc35):

| Spinel mechanism | Reuse rating | Decision |
| --- | ---: | --- |
| CRuby differential snapshots and RubySpec frontier | 10.0/10 | The pinned source/output oracle directly validates identity, redefinition, block return, and rescue/ensure; expand by named RubySpec category only. |
| Ruby declaration, class, method, and visibility facts | 9.0/10 | D1 uses Ruby-owned numeric selector/slot/definition/topology facts and owner-local effective epochs; keep both the facts and mutation policy out of a shared compiler Core. |
| Typed non-local unwind reasons | 9.5/10 | Explicit normal/targeted-return/raise completion preserves nested block and ensure order in Go; reject Spinel's C unwind mechanism. |
| Explicit block/capture/cell metadata | 9.0/10 | Captured environment, `self`, and defining return target are essential Ruby facts and belong in the Ruby runtime. |
| Compiler versus stateful project/build orchestrator split | 9.0/10 | The pure Ruby compiler returns one Set while Layout/build/reload effects remain outside it. |
| Per-node semantic fact side tables | 8.0/10 | Binding, assignment, block, and declaration maps feed evaluator and emitter without inflating syntax; do not copy the giant mutable `Compiler`. |
| Whole-program fixed-point inference | 5.0/10 | R0 needed no fixed point. Retain only as optional Ruby specialization evidence; correctness cannot require a closed world. |
| Guarded closed-world call specialization | 9.5/10 | Exact dispatch/shape/cell/target guards plus pre-effect recovery preserve bounded redefinition and yield zero warmed allocations. The old 9-10 ns result belongs only to the historical monomorphic calibration. |
| Feature reachability and generated-runtime dead-code elimination | 7.5/10 | The one-package proof ships only its admitted runtime operations and no canonical evaluator/compiler; broaden only from reachable Ruby behavior. |
| Representation-specialized containers/value classes | 4.0/10 | Potential prepared win, but CRuby identity remains an explicit allocation/boxing barrier and R0 intentionally keeps object IDs concrete. |
| Prism normalized AST boundary | 4.0/10 | Copy the boundary idea, not its text format, C integration, or trust model. |
| Yield-body call-site inlining | 2.0/10 | Consider only after guarded block semantics; it cannot be the canonical block/return mechanism. |
| Parse-time `require` source splicing | 2.0/10 | Report Ruby dependency facts and leave resolution/effects to the host instead. |
| Giant mutable compiler plus direct AST-to-C | 1.0/10 | D1 proves direct checked facts can feed a small Ruby evaluator/emitter while the common target stays Go; the giant compiler is not a shared architecture. |
| `setjmp`/`longjmp` exceptions | 0.5/10 | Typed Go completion is simpler, owner-safe, and compatible with uncatchable host control. |
| C object layout, GC, FFI, and system-C backend | 0.0/10 | Reject under Ember's cgo-free portable ownership contract. |

As a complete reference after D1, Spinel is rated 8.5/10 for Ruby frontend and
semantic ideas, 8.0/10 as a mechanism mine, 1.5/10 as the shared
Luau/Ruby/Sprig architecture, and 1.0/10 for direct source reuse. The proof
copied semantic invariants and oracle strategy, not Spinel C code or runtime
layout.

## Proof and promotion gates

The direction becomes accepted through reversible slices. Optional performance
experiments branch from the proved seam and never block multi-language delivery:

```text
B0 -> S1 -> S0 -> S4 -> S3b -----+-> S5b
                                  |
S2 -> S3a -> S5a ----------------+
             `-> S6

S5b + S6 -> S7 -> S8
S2 -> R0 -> D1
S6 + R0 -> C0
R0 -> RP0
S5b + D1 + C0 + RP0 -> R1

S4 -> I1 optional sealed Luau image -> I2 optional PGO/ISA profile
S7 or R1 -> M1 atomic root-Luau package migration
two matching real producers -> X1 private sharing -> X2 optional public kit
```

Every promoted slice names what becomes authoritative and what is deleted.
Failure of a later language experiment does not preserve an unused shared
package, adapter, registry, image codec, dual interface, or package facade.

At the evidence snapshot for this revision, `B0` plus S1, pure S2, S3a,
static external S5a, a real external compile-only extension, internal S6,
Ruby D1, embedded C0, and Ruby graph RP0 implementation candidates exist only
in the 135-entry
working tree over HEAD
`04cfa673484e294abafff612113a2b7b35b19ada`. The S1 candidate requires
an exact Go command, derives execution and identity from one fixed policy, disables
implicit PGO and host-native ISA selection, covers selected headers and
standard-library sources, fingerprints the exact Go executable plus bounded
whole tool/include trees, and recaptures inputs after compilation. Focused
`preparedworker`, `preparedworkerbuild`, generated-fixture, and six-target
cross-build and complete repository checks pass locally, but the tree is not a
retained clean revision and has no exact-revision P4/P5 native-platform,
performance, or resource receipts. The S2 package has focused tests for its
opaque value ownership, exact grammars, source policy, bounds, canonical order,
and versioned digest goldens. S3a adds the first real producer, canonical
Layout plan, fail-closed one-leaf materializer, and shared checked-fixture
consumer without changing the worker artifact. S5a adds an out-of-module
producer, application-owned two-mount composition, clean-root materialization,
producer deletion, exact steady dependency absence, native execution, and six
cgo-free target builds. It changes no worker artifact and explicitly proves no
Ruby or Sprig semantics. A second external module derives one Set from checked
Seed source through its own parser, checker, evaluator, and direct-Go emitter;
repeated identities/application bytes, deterministic failures, compiler/source
deletion, zero-dependency offline execution, zero steady allocations, 1.001x
local handwritten overhead, and six cgo-free target builds pass without a
Backend or internal import. The mixed application proof feeds that Seed Set
and a real Sprig standalone Set into one application Layout and direct handler;
ordered domain/error/cancellation/limit behavior, detached values, exact
dependency/symbol absence, zero positive-path allocations, 1.024x local
composition overhead, and all six cgo-free builds pass without a common ABI or
package graph. It is a static precursor, not S5b or S7. S6 independently adds
the bounded `counter` parser, checker, canonical evaluator, direct-Go emitter,
exact Set fixtures, offline standalone applications, semantic/ownership
differentials, and local paired performance/allocation gates. Its current
project extension adds canonical
package order, nominal checked IDs, requested standalone closures, an explicit
application-bound linked product with direct Go imports, deterministic Go-name
hygiene, application-owned Layout construction, and exact fresh/warm/change
cache attribution across a four-package DAG. Production-shaped local evidence
retains standalone output for location-neutral artifacts and linked output for
shared multi-entry projects while rejecting fusion. It proves the architecture
slice, not a general language or retained release performance.
R0 independently adds bounded actual Ruby source, deterministic parser/checker,
Ruby-owned canonical object/method/block/unwind execution, exact class/epoch
guard recovery, a pinned CRuby differential, one Set, zero-allocation generated
execution, compiler/runtime graph absence, 13.67x local hot-send speedup, and
six cgo-free builds. It proves the pre-worker architecture slice, not public or
full Ruby, a generation checkpoint, S5b, or R1.
C0 then composes the real generated Ruby and Sprig products without changing
either compiler. The app owns one 28-byte checkpoint, a 17-byte request,
41-byte result, 24-byte effect, order, codecs, restore, and close policy. Two
transactions on fresh Ruby owners produce `105|532|5` then
`206|532532|9`; embedded journal reopen and exact duplicate replay agree. A
one-step Ruby failure after its first trace mutation publishes no decision or
effect, resolves not-committed, quarantines later entry, and closes the owner.
The inner handler and equivalent direct composition both allocate zero; the
latest seven-order dirty-host median is 1.014x, below the 1.05x gate.

The independent static observer compiles Ruby Program
`e318f17162550f67ef6f59d6aee5d069685a82e4c4bc6b003bd00ebc62893a5a`
and mounts Ruby Set
`64a8ca74f277ca929a65b998e66dcca987e845767ed02e3843a4be742130d1ec`
and Sprig Set
`ba3ac0b681059088220800a5b5685feb363c20f03bbeebac95b461891f3936d3`
into Layout
`3fa94ce7104d0e1cdd7659e3d8c755ab29d0ddf07e33090e7cfdf9992966494c`.
Its exact five-file application builds offline with cgo disabled, executes the
same two states, retains only main plus the two generated packages, and builds
all six supported targets. The current Ruby/Sprig generated-source hashes are
`91c29ef924253eec8073ca7b664df36c87857c9c030446caf53b5331c9d1b924`
and `52334ba59cf0e65aed3e5c811dca919b30830c1af559bfccd3d7818bba571de2`.
The local 2,566,754-byte trimmed binary is
`4079d0db4772203026b3ae9945f7d3d22689c731d171a4dd4100e3dc08572cc2`.
C0 proves embedded scalar checkpoint composition, not object-identity
migration, a production Ruby heap, process reload, S5b, S7, or R1.
RP0 independently compiles the same admitted Ruby source twice with only the
initial `label` return changed from 1 to 9. The Programs are
`e318f17162550f67ef6f59d6aee5d069685a82e4c4bc6b003bd00ebc62893a5a`
and
`6d5efab536abd13c1835f530e5b1735696943ca4d0fb111e0723a179e07688de`;
the `rubyv1`/`rubyv2` Sets are
`8483665f75e21f94c61862dc779f7158bc10c470a4c05432d0f2381f44744d99`
and
`38db334f630763eead68754d3b30df854ef1d7d022caf86db87ac0dae6eea102`;
their generated-source hashes are
`1687ee1eee83967e9978bee07180852539f182cb434ea343b20a364ce3b6ca68`
and
`df69a2d94a0015b3a69074f15d256f4c4064eca23b597988add1212c4b59f078`.
One five-file application Layout
`d50eb52b72fce71da122fb6abd84e483facc13fb63ede218e0f8cede272cf9f5`
contains only main, the concrete application projection, and those two
generated packages. It closes v1, restores v2 through the 52-byte record, and
prints `true|true|true|40|40|41|49|true`. The dependency-free local
2,566,562-byte binary is
`ecc75d0192b1c202dc085cdc597bc318350d630b06c8d18fa5be8cf89d8b8cc0`;
all six cgo-free targets build. The actual warmed v2 owner returned by
application `RestoreV2` and its equivalent explicit `+9` captured-self owner
both allocate zero; the latest seven-order dirty-host median is 1.005x, below
the 1.05x gate. RP0 proves one application schema and generated-code migration,
not stable Ruby object identity, arbitrary Proc/fiber/frame state, process
reload, or retained release performance.
ADR 0011 is accepted
architectural direction; S0 is not yet passed. Current S0 readiness is 6.4/10:
roughly 8.7/10 implementation and 4.0/10 retained proof.

### S0. Proved worker baseline

S0 is a proof gate, not a status inferred from ADR 0011 being marked Accepted.
It passes only for one retained clean revision after S1 closes every known
output-changing build input and all of the following hold:

- the public `Runner`/`Reload` cutover and deletion of the replaced
  slot/native/plugin architecture are complete;
- generated worker fixtures are fresh;
- `scripts/check`, `go build ./...`, vet, race, and checkptr pass;
- target-native launch, identity handshake, apply, reload, cancellation,
  retirement, and close pass on all six supported OS/ISA pairs; and
- exact-revision paired all-37 admission plus 1,024-swap bounded-resource
  receipts exist on physical ARM64 and x86-64 hosts.

Scripts, workflow definitions, cross-builds, plan prose, or receipts from an
earlier revision do not satisfy S0. Until it passes, S4 may not change the
worker artifact model. Pure S2, non-worker S3a, and static S5a may proceed
independently because they neither alter the builder nor claim worker
certification.

### S1. Builder identity closure

Fold S1 into ADR 0011's artifact-exactness work before rerunning its platform
and performance receipts. The working-tree candidate implements the explicit
Go command and single fixed build policy described above; binds PGO, header,
selected standard-library, complete bounded tool directory, target-feature,
source/embed, module, contract, and protocol inputs; and recomputes mutable
inputs after the build. Its focused tests prove that fixed-policy changes and
same-version tool/header/stdlib byte changes alter Build ID. Promotion still
requires integrated repository checks and native target receipts on a retained
revision. Output path/name and maximum executable bytes must continue not to
fragment safe cache reuse, while the bound remains enforced on every
selection.

S1 is rated 9.4/10 as a separate correctness slice and 3.8/10 if folded into
S4: combining identity repair with the public artifact/overlay/descriptor
cutover makes failures harder to localize and leaves ADR 0011's baseline claim
unsound meanwhile. Keep each hardening whose input can change output even if
S4 is abandoned.

### S2. Pure prepared-source values

The working-tree candidate implements `Set`, raw `Mount`, and canonical
`Layout`. External and internal tests cover zero values, caller mutation, input
permutation, lowercase portable grammars, reserved/target-selecting names,
duplicate ownership, fixed and repeated-work limits, bounded Go scanning,
syntax/package/import/directive policy, exact embed bijection, and
domain-separated digest goldens. Its only non-standard dependency is its own
package; it remains free of root Ember, worker, filesystem, toolchain, process,
and cache dependencies.

That completes the pure implementation slice locally, not its promotion. The
working-tree S3a candidate supplies the first Luau producer and embedded
materialization consumer, while S5a supplies a second out-of-module producer
and a two-package clean-root consumer. S4 must still prove the same Layout
through the physical minimal worker-module root. Delete or collapse the package
if the consumers still need to duplicate normalization, collision, ordering,
or identity policy. Parser-valid source is not evidence of a type-correct or
successful Go build; the consumers and Go toolchain own that proof.

### S3a. Luau producer and embedded materialization

The working-tree candidate evolves the existing concrete Luau
`PreparedGoArtifact` in place rather than adding a parallel compiler-result
type. It owns one `Set` plus the existing Luau recipe/bundle identity and
exposes the immutable Set; it does not retain a second copy of generated
bytes. Its fixed producer filename is
`prepared_generated.go`, so changing an application mount never changes Set
identity. `Sources() preparedsource.Set` returns that value, including the zero
Set for a nil artifact. The legacy raw generated digest, `WriteTo`, and
`Program.WritePreparedGo` remain only while builder V1 needs them:
`WriteTo` reads the one Set file, and `WritePreparedGo` delegates to
`GeneratePreparedGo` so there is still one producer path. The raw digest
continues to identify exact source bytes and is intentionally distinct from the
framed Set digest.

The candidate replaces `cmd/emberc`'s caller-chosen output filename with one
portable `output_dir`. The manifest must be a regular non-symlink file whose
canonical parent directly contains a regular non-symlink `go.mod`; that parent
is the module root. `output_dir` is the one `Layout` mount and uses
`preparedsource`'s canonical relative-path grammar. Construct a one-mount
`Layout`, feed its already canonical `Mounts` and `Files` through one private
pure embedded-file-plan constructor, and make write, `-check`, and checked
fixture freshness consume that plan. The constructor preserves Layout digest,
relative slash keys, and order; it does not normalize, sort, or re-resolve
collisions.

Every output-directory component must already exist with exact lowercase
spelling and be a real non-symlink directory. Admission rejects case aliases,
the fixed target as a symlink or non-regular file, aliases of the manifest,
`go.mod`, or other protected inputs, and an existing target without Ember's
exact generated-file header. Unrelated siblings are ignored and preserved. A
fresh owned target is a no-op. An absent or stale owned target is written to a
same-directory `0600` temporary file, checked for short writes, synced, changed
to `0644`, closed, and revalidated before rename; all fallible work precedes
the namespace commit. A pre-commit failure preserves the old target or its
absence. POSIX local filesystems receive atomic namespace replacement;
Windows receives only the behavior documented by Go, with no universal
atomicity or crash-durability claim. Build starts only after this explicit
materialization command succeeds.

The checked generated fixture consumes the production plan rather than a
test-only reimplementation and preserves exact generated bytes and
source-to-built prepared behavior. The candidate passes focused artifact,
materializer, fixture, and boundary tests, scoped and full vet/build checks,
the pure-Go policy, six-target test compilation, and the full repository check
locally. S4 remains separately gated by S0.

S3a deliberately emits exactly one Go leaf and no assets. Before Luau emits
multiple leaves, `emberc` must gain an explicit generated-file index, stage the
complete bounded plan, update only indexed leaves, define crash recovery, and
never scan or delete unrelated application files. Through S3a, the current
builder's legacy `Program` input and descriptor remain unchanged; this avoids
quietly changing an uncertified worker artifact model.

### S4. Language-neutral builder cutover

Replace singular `preparedworkerbuild.Program` with one `Layout`, remove the Go
overlay from the worker build policy, and replace the descriptor/manifest with
V2. The application supplies a fresh minimal standalone worker module whose
physical tree already contains its fixed wrapper/handler/schema sources and
every Layout leaf. The builder verifies this root but never constructs, copies,
or mutates it. In the same cutover delete the old public Program, the builder's
root Ember import, descriptor V1, every Luau recipe/ABI/Program/Proto field,
the singular generated path, backing-map state, and overlay build flag; do not
retain parallel public builder interfaces.

Require every supplied Go/embed file exactly once in the selected closure and
prove exact physical-byte admission, no stale residue between fresh roots,
cache identity, publication-policy reuse, failure preservation, and concrete
handler semantic rejection before `READY`. Preserve ordinary Go module,
import, embed, build-tag, and `internal` behavior under `GOWORK=off`,
`-mod=readonly`, and cgo-free fixed policy. Before discovery, parse the stable
main `go.mod` exactly once with the selected pinned tool. Admit ordinary
versioned modules and only replacements with a nonempty RHS version; reject
every relative or absolute filesystem RHS. Versioned inputs come from an
explicit immutable offline module-cache owner, never ambient network/cache
policy.

The selected initial S4 supply contract is one **logical
application-scoped sealed module-supply view** consumed through an explicitly
leased physical `GOMODCACHE`. The logical view is the admitted module graph and
selected package/source closure, not a physical directory inventory. The
physical backing may move or contain unrelated immutable modules without
changing module, package, source, Go BuildID, executable, or application
identity. The builder must never walk, hash, copy, or key on a whole cache to
establish that property. A private application cache is the simplest first
backing; immutable shared or content-addressed backing may replace it later
without changing the logical contract.

Provisioning and target compilation are separate transactions. An application
provisioner may use an authorized proxy or network and mutable staging with the
pinned Go tool, but publishes only a complete immutable cache epoch. Target
commands borrow the application-root and supply-view read leases, use
`-mod=readonly`, set the leased root as `GOMODCACHE`, disable proxy, workspace,
sumdb, VCS/auth, toolchain switching, and cgo, and receive explicit private
`HOME`, `GOPATH`, temporary storage, and `GOCACHE`. `GOCACHE` is writable
derived acceleration with a separate host owner; it is never source authority.
Adding modules creates another unpublished epoch rather than mutating an active
one. Concurrent targets and retries may share read leases.

S4 identity binds the exact main `go.mod` and `go.sum`; original and replacement
module path/version plus applicable `Sum` and `GoModSum` values; actual selected
`GoMod` bytes; package `Module` attribution; and selected Go, assembly, header,
and embed bytes under logical module-relative names. Absolute application,
cache, proxy, and toolchain paths are admission evidence only. Discovery/build
recaptures these inputs before releasing either lease. Failure or cancellation
publishes no artifact. After executable publication, release both source leases
before `Reload.Prepare`; workers retain only the opaque executable build, so
cache eviction never waits for activation or old-generation retirement.

This supply policy stays in application delivery and private Go-builder
admission. It does not enter `preparedsource.Set`, `Layout`, a language
compiler/runtime, application records, `preparedworker`, or a generic Backend
or dependency-supply interface. Every Go-emitting language benefits from it
without knowing modules; a future non-Go artifact builder owns a sibling supply
mechanism behind the same completed-build/reload boundary. If S4 needs caller
input, prefer one narrow explicit Go-builder cache root/capability over a public
supply facade.

A disposable Go 1.26.4 proof exercised an ordinary module, versioned
replacement, `internal` import, build tag, and nested embed. A write-denied
complete cache worked with proxy off and retained full sums and module roots.
Two unrelated application roots, two unrelated physical caches, independent
fresh `GOCACHE` values, and an extra valid but unselected module produced equal
selected identities, package/final BuildIDs, executables, and behavior while
their whole-cache receipts differed. The same fixture showed that vendor mode
is offline and compact but cannot natively list the full `all` graph and omits
dependency/replacement sums, `Dir`, and `GoMod`. This selects the sealed logical
view at **9.6/10** and keeps vendor at **7.2/10** as a bounded fallback. Full-tree
hashing and chmod were proof-only mutation sentinels, not production identity or
lease mechanisms. The result is Darwin/arm64 structural evidence, not retained
timing, RSS, warm-retry, multi-target, ABA/hardlink, Windows, or production S4
proof.

Discovery requests only the top-level package/module fields the builder
consumes, hard-caps raw stdout before decoding, drains bounded stderr, enforces
separate retained-semantic and derived-key counters, owns pipes explicitly,
waits the direct child concurrently with drains, bounds inherited-descriptor
retention, and joins all consequential failures. Charge actual Layout bytes and
every selected non-Layout non-standard source to one checked complete-closure
residual. Independently bound package/module/source counts, total replacement
nodes, aggregate unique module-file bytes, standard sources, and stderr; do not
promise simultaneous maxima. Fuse these checks and physical Layout equality
into each required pre/post closure hash pass. If the builder must interpret a
language identity, copy arbitrary caller source, own root cleanup, accept a
filesystem replacement, or expose a shared limits facade, revert S4 rather
than add a language switch or shallow abstraction.

S4 creates the second real `Layout` consumer. Feed the same layout to the S3a
embedded-file-plan and a pure S4 physical-root plan with effects disabled and
require identical canonical relative-key/content order and layout identity.
Neither plan may revalidate, sort, or resolve collisions; their effect owners
add only distinct admission, filesystem, lifetime, or toolchain checks. The
application/host owns exclusive root assembly, publication-as-capability,
leases, cancellation cleanup, and optional later eviction. The builder owns
discovery/build/recapture and executable CAS publication. `preparedworker`
retains no source root. If `Layout` does not remove normalization, collision,
ordering, and identity policy from both real consumers, collapse or delete it
before S5b.

### S3b. Delete the legacy artifact path

Immediately after S4 callers migrate, delete legacy
`WritePreparedGo`, singular `WriteTo`, raw generated-byte digest, and every
transitional Luau producer adapter. Keep the concrete Luau compiler result and
its `Set`; rename or relocate it only with M1 rather than creating a facade.
S4 itself already deletes the old public builder input, root Ember import, and
descriptor V1 atomically. S3b is a cleanup slice with no compatibility epoch.
S5b begins only after the import and symbol absence proof passes.

### S5a. External static delivery

The working-tree candidate is an independently versioned fixture module under
`preparedsource/testdata/externalproducer`. Its producer imports only
`preparedsource` among external packages and returns two complete immutable
Sets with different concrete APIs: a Ruby-shaped owner/reference/detached
record and a Sprig-shaped typed functional composition. It never chooses
application mount paths. An application-owned command constructs the canonical
two-mount Layout and copies its already ordered Mounts/Files into one
exclusively claimed temporary root without sorting, normalization, collision
resolution, language dispatch, registry lookup, or semantic inspection.

The parent integration test copies that module out of Ember, binds an exact
local replacement without network access, checks producer dependencies and
permutation-stable identities, materializes the application, and deletes the
complete producer/tooling directory. Only then, with fresh offline Go caches,
it verifies the exact application source inventory, exact zero-dependency
module/package graph, `CGO_ENABLED=0` build metadata, delivery-symbol absence,
native execution, and Darwin/Linux/Windows ARM64/x86-64 cross-builds. The
steady application imports only its concrete generated packages.

This locally proves artifact delivery, application-owned placement, a bounded
clean-root effect adapter, owner-detachment shape, typed-composition shape, and
delivery-code absence. It does not prove Ruby or Sprig syntax, semantics,
compatibility, cancellation, limits, runtime ownership, or performance. It
does not change the worker builder and is not completion of S5b.

The companion `preparedsource/testdata/externalcompiler` module proves the
same post-lowering boundary with an actual source compiler rather than stored
generated text. Seed source is lexed, parsed, nominally checked, evaluated, and
emitted by the external module. Its Program, Set, Layout, and complete
application are deterministic. The parent test observes semantic/domain/error
agreement, deletes compiler and source before fresh-cache build, proves exact
dependency and symbol absence, checks zero scalar allocations and bounded
near-handwritten execution, and cross-builds all six targets. The fixture owns
all Seed policy; no generic compiler API, scalar IR, runtime Value, or registry
was needed or added.

The real compiler comparison leaves only standard hashing/sorting/formatting
and `preparedsource.NewSet` in common with Sprig. That is not one duplicated
deep operation: checked representations, diagnostics, package topology,
generated ABI, cancellation schedule, and emission decisions differ. The
evidence therefore strengthens the artifact seam and lowers, rather than
raises, the case for a public compiler kit now.

### S5b. External worker composition

After S4, feed equivalent Ruby-shaped and Sprig-shaped Sets through the real
minimal worker-module root, statically import both packages in one handler,
restore one checkpoint, return one ordered outbox, and swap generations without
editing `preparedworker` or adding common semantic types. The Ruby-shaped
fixture must detach one owner-bound object into a closed record and close all
constructed owners on restore failure. The direct static application and
worker handler must agree while the worker continues to pay one exchange per
application transaction rather than one per language.

S5b proves only delivery, ownership, and reload composition. It is not evidence
for Ruby syntax or semantics and does not justify publishing `/ruby`.

### S6. Sprig semantic baseline

Implement one package, record, tagged union, integer loop, exhaustive match,
canonical evaluator, and return-by-value `Reduce`. Prove deterministic
diagnostics, evaluator/generated-Go result and error agreement, integer edges,
division by zero, pre-canceled no-entry behavior, bounded entry/backedge polling
latency, direct `errors.Is` cancellation, independent per-call limits,
nonmutation of inputs, detached output, and engine-free concurrent calls. The
baseline must reach at least 2x its evaluator and no worse than 1.10x equivalent
context-polled handwritten Go or the slice is deleted.

The working-tree candidate passes this bounded local gate and a project seam
extension. The extension accepts multiple first-order pure functions, canonical
acyclic imports, and the checked `counter -> math -> util` call chain beside an
unrelated package. Name resolution freezes nominal IDs before evaluation or
emission. `PrepareStandalone` copies the reachable function closure into each
requested independently mountable entry. `PrepareLinked` emits requested
reachable packages with exact application bindings and direct imports. The
application constructs the Layout in both cases. Source and binding
permutation are byte-identical. Root, dependency, path-binding, and unrelated
changes have exact Set, BuildID, compile-action, and executable attribution.

The candidate deliberately accepts less than the proposed language above:
exact `Reading`, `Step`, and `Result` schemas, checked `i64` addition and
truncating division, immutable `[]i64` values, one input loop, exhaustive
`Step` matching, and terminal `Ok`/`Err`; imported packages contain only
first-order pure `i64` functions. Unsupported forms are deterministic source
diagnostics. Generated steady code has no Sprig compiler, Ember runtime,
delivery module, registry, shared IR, or
Engine. Standalone output has no generated-package dependency; linked output's
only non-standard dependencies are its explicitly bound package closure.
Promotion to public `/sprig`, S7, or a production-language claim still requires
a broader language frontier and retained clean evidence.

S6b is locally complete. The pinned real-emitter shapes and disposable fused
control are recorded under Package-shape decision above. Requested standalone
preparation replaced all-package emission; application-bound linking cleared
the material threshold for shared multi-entry projects; fusion was rejected
and deleted. The implementation added no shared IR, universal backend,
producer Layout, registry, or adaptive policy. A retained quiet-host repeat on
a production application can revise the crossover, but it is not required to
rediscover these contracts.

### S7. Mixed Luau/Sprig transaction

Use one explicit handler, one context, per-language limits, one bounded
checkpoint, and one ordered outbox. Prove V1-to-V2 supervised reload, checkpoint
as the sole persistent state, domain rejection as a committed typed result,
backend error/cancellation as terminal generation loss with no decision,
reverse cleanup, and no cross-language owner-backed value. Mixed embedded
composition must be no worse than 5 percent over the same concrete calls, use
one EPW2 exchange, and preserve the retained 1,024-swap resource slope.

### S8. Optional owner-local Sprig optimization

Only after S6/S7, compare `Engine.ReduceInto` with `Reduce`. Require logically
empty retained-capacity output after every failure, same-output overwrite,
other-output retention, no engine-memory escape, promised reuse after
cancellation, zero scalar allocations, zero warmed fixed-capacity composite
allocations, and at least 5 percent representative end-to-end improvement or a
material allocation/RSS win. Otherwise delete the Engine and keep `Reduce`.

### R0. Ruby pre-worker semantic/AOT falsifier

The working-tree `internal/rubyproof` candidate locally completes R0. It uses
actual bounded Ruby source and a pinned CRuby observer to prove class/instance
identity, open method send/redefinition, block/yield with a targeted non-local
return, rescue/ensure order, uncatchable host cancellation/limits, owner
poisoning, and a guarded direct-Go call with exact pre-effect recovery. Its
compiler returns one location-neutral Set; a fresh zero-dependency application
chooses Layout and builds all six targets without cgo. The stable prepared send
and complete prepared proof run allocate zero and clear the local 2x/
1.15x performance gates.

R0's performance claim remains monomorphic. The reviewed three-class
open-world preflight above proves only bounded cache and invalidation shape;
its timing result was rejected and moved no rating. Do not use it to claim
polymorphic Ruby throughput. The next admissible performance gate is the
standalone direct-target, independent-comparator observer specified above, but
its implementation is intentionally deferred until the retained R1
semantic/lifetime milestone makes broader representation policy a production
decision. Target37's implemented in-package warmed-hit microprobe decides only
the bounded lowering shape and is not that observer. Target38 separately bounds
exact repeated-leaf source/archive/text scaling; it neither changes production
nor supplies the missing shared-site-pointer hot gate. A reviewed process
authority alone does not authorize standalone capture.

R0 is deliberately private and reversible. It does not claim general Ruby
grammar, modules/ancestors/visibility, keywords/splats, fibers, GC, FFI,
production allocation, RubySpec coverage, a public `/ruby`, checkpoint
projection, worker restore, or transactional reload. An admitted checked shape
outside the prepared proof fails closed. Promote only the semantic mechanisms,
not the proof-specific source grammar or emission-plan matcher.

### D1. Ruby lookup/dispatch nucleus

The working-tree `internal/rubyproof` candidate now locally completes the
bounded D1 nucleus through C4 visibility, Target39's C1 module topology, and
Target41's C2 allocation-site singleton row. The checker seals eight lookup
nodes, five dispatch rows, eight selectors, seventeen definitions, twenty-four
checked operations, two attachments, and exact all-mask comparison routes. The
emitter projects only the exact two-selector `label`/`saved_label` closure:
sixteen local-entry positions, ten eager cells, nine definition/body targets,
twelve later mutations, thirteen target matches, four sites, and eight fixed
arm positions. Canonical and generated
owners independently derive routes, resolve, and mutate numeric lookup facts.
The generated owner invalidates only inheriting Alpha and Delta when Base is
reopened, preserves the snapshot alias and every saved cell, changes only Alpha
and Delta across Base public/protected/private transitions, rejects non-public
`public_send` before effects, permits private literal `send`, preserves Beta's
public override, changes only Beta when local removal exposes Base v2, and
changes only Gamma to a terminal `undef` receipt. Base v2 reuses one target ID
across Alpha, Beta, and Delta through separate finite compatibility matches
rather than duplicating executable authority. Include, empty prepend, late
module definition, and included-module redefinition publish the legacy
four-row `K=2/0/1/2` and full five-row `K=3/0/1/3` effective changes after a
detached `K=0` definition. The singleton define then publishes exactly `K=1`
without changing the ordinary Alpha row or peer arm.

Focused tests pin the exact result and eight-counter receipt, zero warmed
allocations, fail-closed corruption, cancellation and epoch-exhaustion
atomicity, post-effect host-abort behavior, complete owner cleanup, cold
reconstruction, generated layouts, source/Set freshness, dependency absence,
and cgo-free static builds. The R0 monomorphic direct-`Hot` calibration remains
historical. Target37 adds an accepted local identity-bound two-class
alternating-hit receipt for the historical specialized C4 lowering. Target38 proves that
strict exact-site repetition stays approximately linear and that conditional
alpha-equivalent leaf sharing would materially reduce generated/archive/text
size at 96 sites, but the three production leaves have no eligible pair and the
pointer-parameter hot gate is incomplete. Neither target supplies standalone
R1 or open-world timing evidence or raises the open-world Ruby performance
score. Those captures predate Target39 and do not authenticate its current
artifact. The current hit path retains explicit context polls and exact whole-
site validation while AST checks prove it contains no topology dependency.
Singleton classes, full caller-sensitive protected ordinary calls,
production heap/GC, broader
application projection, and real worker reload remain the next semantic and
lifecycle gates.

### C0. Embedded Ruby/Sprig checkpoint composition

The working-tree `internal/mixedcheckpointproof` candidate locally completes
C0. It imports the two concrete generated packages directly and defines one
application request, result, versioned checkpoint, effect, execution order,
and limit policy. The checkpoint contains only Ruby's bounded scalar state and
Sprig's value total. Restore validates it before creating a fresh Ruby owner;
successful close erases the owner, while a busy close retains enough state to
retry. Ruby object identity deliberately does not survive. Sprig is pure and
has no constructed owner in this slice.

The direct handler proves typed Sprig rejection/domain short-circuiting before
Ruby, positive sequential state, distinct budgets under one context,
poison-on-cancellation/limit, zero failed output, partial-restore cleanup, and
zero steady allocation. The production `OpenEmbedded` adapter proves one
atomic decision/checkpoint/effect, duplicate replay without duplicate effect
delivery, journal close/reopen, next-transaction restore, authoritative
not-committed resolution after a partial Ruby failure, lost-generation
admission, and final owner close. Its codecs are concrete fixed records;
`preparedworker` contains no Ruby/Sprig switch.

A separate app-owned source fixture compiles R0 and S6 independently, mounts
only their Sets, and builds a fresh dependency-free static application with
the exact two-transaction output. Compiler, `preparedsource`, worker, registry,
plugin, and cgo packages are absent from its graph and linked symbols; all six
targets cross-build. The direct inner composition stays within 1.05x of the
same transaction without handler lifecycle dispatch. Codec, journal, and
effect-publication costs are intentionally outside that gate.

C0 is an embedded checkpoint falsifier only. Close/reopen is not hot reload;
`OpenEmbedded` is not an EPW2 process generation; the scalar Ruby record is not
a production heap serializer; and one Ruby owner does not prove general
reverse cleanup for multiple resource owners. Do not use C0 to claim S5b, S7,
R1, target-native launch, or retained release performance.

### RP0. Bounded Ruby graph-projection falsifier

The working-tree `internal/rubyprojectionproof` candidate locally completes
RP0 for one fixed application graph. The application owns a 52-byte canonical
record with three roots, three nodes, three tagged edges, and one tagged
behavior. It proves root aliasing, an equal-valued distinct node, a self-cycle,
reachability through a third node, and captured-self reconstruction. The
record has no Ruby pointer/object ID, class/method table, method epoch, frame,
return target, executable closure, source identity, or language registry.

The two concrete generated Ruby packages come from independently identified
checked sources. v1 validates its private topology, snapshots only three
scalars into the application record, and closes before v2 construction. The
application initializes v1 and restores v2 from its record; generated packages
own no default application values. v2 privately reconstructs fresh nodes and
captured self, round-trips the same application record, and executes v2's
return value rather than frozen v1 code. Generated packages expose no
application graph record or observation interface. Exact
length/count/order/tag/topology/overflow validation, a malformed corpus, direct
v1/v2 intrinsic equality and source-derived arithmetic bounds, all generated
context polls and insufficient step/object budgets, every private topology
predicate class, cleanup before publication, Call/Snapshot close-admission,
static dependency/symbol absence, six cgo-free builds, zero warmed allocations,
and a
1.05x actual-restored-v2/equivalent-explicit call gate all pass locally.

RP0 is deliberately not a reusable graph codec or Ruby heap serializer. It
does not preserve Ruby identity across generations, arbitrary object layouts,
Proc code, fibers, continuations, singleton classes, monkey patches, or live
targeted returns. It does not use `preparedworker`, launch a process candidate,
or prove S5b/S7/R1. Production Ruby must define application projections for a
broader named semantic frontier and may reject reload when live state cannot
be represented safely.

### R1. Ruby executable vertical slice

D1 retains R0's pre-worker semantics and clears the bounded lookup/dispatch
nucleus, C0 clears bounded embedded mixed checkpoint composition, and RP0
clears one bounded graph/code migration without a shared Core, Backend, or
builder language switch. A real `/ruby` package still requires a broader named
CRuby/RubySpec frontier, a production Ruby heap and allocator, a production
application projection beyond C0/RP0's fixed records, and the same guarded/cold
behavior embedded and in one mixed worker transaction after S5b. Host
cancellation remains uncatchable, may skip guest `ensure`, and discards the
owner. If checkpoint/restore or mixed worker execution requires builder Ruby
knowledge or a shared guest heap, reject this architecture for that Ruby
scope; D1's bounded static result is not a substitute for those remaining
gates.

The retained implementation order is the completed bounded D1 C1/C2/C3/C4
nucleus including Target41's receiver-specific singleton proof, then H1
heap/allocation, then P1
application projection and physical lifecycle. The nucleus covers inherited
lookup, ancestor and module topology mutation, later module definitions,
alias/remove/undef, and public/protected/private literal-selector dispatch with
the minimum slot-backed shape facts required by direct targets. Its selected
production
invalidation authority is the product-capped dense effective-resolution table:
a generated hit directly loads one class/constant-selector cell, while
mutations derive routes transiently, recompute bounded cells, and advance only
changed effective epochs.
This sequencing is not R1 completion and does not waive S0/S4/S5b.

D1 has introduced the passive checked lookup image above. The canonical
runtime consumes it directly while the emitter serializes a smaller numeric
closure; each lane owns independent lookup/mutation behavior and one mutable
owner. Targets, arms, shapes, and site policy do not turn the image into a
runtime or public compiler seam. The bounded generated string/epoch path is
gone; the same fact boundary must now expand without acquiring shared
operational behavior.

Before R1 may promote polymorphic send performance, one retained observer must
also prove all of the following on exact source and generated identities:

- each bounded PIC arm owns a direct compiled target keyed by Ruby-owned
  class, shape, method slot, and epoch facts; hit execution performs no method
  table scan or interpreted field-name dispatch;
- stale-arm recovery exits before guest effects, uses the canonical Ruby-owned
  lookup, patches only that arm after success, and leaves unseen receivers on
  bounded fallback without cache growth;
- an independently implemented, representation- and control-equivalent Go
  comparator shares immutable facts but no owner, send, guard, fallback, or
  effect implementation with the generated lane;
- steady timing contains only pre-repaired alternating hits, while patch/first
  recovery and uncached fallback have separate time and allocation receipts;
- construction, shape, method/effect, and top-level schedule facts are derived
  from checked source; cancellation, limits, close, poisoning, and Ruby integer
  behavior remain exact; and
- two independent rotated captures clear zero allocations, at least 2x over
  canonical fallback, and no more than 1.15x equivalent Go, with code size,
  heap/RSS, build-to-READY, and reload costs reported rather than hidden.

### I1. Optional dynamic-language sealed image

After S4, compare source-recipe reconstruction with a language-owned sealed
image on a pinned production-size Luau application. In addition to exact
result/error/cancellation/limit/inventory identity, retain a corrupt corpus for
truncation, trailing bytes, excessive counts, overflowed spans, duplicates,
invalid IDs/enums/version/digest/function inventory, bounded allocation, and no
owner publication or `READY` on failure. Measure cold/warm generation,
list/hash, compile, link, launch-to-READY, restore, catch-up, warmup, executable
size, allocations, and peak RSS.

Promote only for at least 20 percent or 100 milliseconds less cached
build-to-READY time, or 15 percent less peak candidate RSS, with at most 2
percent steady regression. Delete the codec/assets if every gate misses, both
time and RSS improve by less than 10 percent, decode materially worsens
launch-to-READY, or parity/validation fails.

### I2. Optional PGO or ISA profile

Only after I1's baseline probe, bind exact profile bytes, tier, toolchain, and
flags into build identity and admit the host before `READY`. Promote only when
the retained all-37 steady geomean improves by at least 5 percent and cached
build-to-READY worsens by at most 20 percent. This remains an opt-in pinned
release profile, never the portable hot-reload default.

### M1. Atomic package migration

Move root Luau to `/luau` only after S4 and one mixed transaction with a second
first-party executable language. Move every maintained import, fixture,
generator, document, and differential test together; delete the root package
and add no facade. Revert the move atomically if that cannot be done.

### X1/X2. Shared compiler promotion

Extract a private mechanism only when two real producers have matching code and
invariants; inline and delete it again on semantic leakage, runtime dispatch,
or retained regression. The Seed compiler proves that an independently
versioned compiler needs no kit beyond `preparedsource`; it does not justify
inventing `aotkit`. Publish one only after an external compiler exposes a
complete duplicated need that the existing value seam cannot satisfy, can use
the candidate without Ember `internal` imports, and retained Luau, Ruby, and
Sprig profiles show no material regression. Any shared runtime dispatch,
allocation, language switch, or greater than 5 percent retained Luau regression
rejects promotion.

### Static absence proof

S5a locally proves exact dependency and linked-symbol absence after deleting
all producer tooling. The external Seed proof repeats it after deleting an
actual compiler and source, and also observes zero scalar allocations. The
mixed Sprig/Seed application repeats the absence proof with two real compiler
outputs, one typed handler, zero mixed positive-path allocations, and no common
ABI, graph, registry, or dispatch symbol. For S7,
R1, and every shared-mechanism promotion, repeat that structural proof for real
generated packages and require retained profiles and allocation results to
contain no `preparedsource`, compiler, worker, builder, registry, shared codec,
or other delivery frames or costs. Development composition remains one EPW2
exchange per application transaction.

## Falsifiers

Reconsider the selected seam if an independently versioned external language
cannot produce a closed immutable set and then disappear from build/reload, or
if the builder must inspect its ABI, invoke its compiler, discover it at
runtime, or dispatch on its language ID. Also reconsider it if shared source,
build, or reload machinery appears as language-symbol dispatch,
synchronization, boxing, codecs, or allocation in a retained guest hot-path
profile. The Seed proof clears this only for its bounded compile-only language;
the mixed proof additionally clears static two-language composition for the
bounded Sprig and Seed shapes. A production external language with materially
different artifact needs or a mixed worker generation remains a valid new
falsifier.

Keep mixed orchestration application-owned while languages have no semantic
call edge. Reconsider only if two production applications duplicate the same
complete typed transaction policy and the extracted form preserves concrete
results, static imports, zero allocations, and at most 5 percent retained
overhead. Any registry, reflection, generic guest value, codec, or language-ID
switch rejects the extraction.

Remove `Layout` if embedded and worker consumers still need separate mount
canonicalization or consumer-specific path policy. Reconsider one-package
`Set` only if a real independently versioned producer cannot emit a useful
artifact without atomically owning multiple Go packages.

Keep Sprig's two explicit preparation products while their lifecycle distinction
holds: standalone entries require location neutrality, while application-owned
shared projects benefit materially from linking. Remove `PrepareLinked` if a
retained production graph loses its source/build/size win, regresses steady
execution by more than 2 percent, or requires a runtime registry, generic
backend program, producer-owned Layout, nondeterministic identity, or broader
interface. Do not replace it with hidden adaptation or fusion. A dependency
recompiling all real consumers is not a falsifier; an unrelated package
changing output or rebuilding is. Reconsider extracting a shared package graph
only after another production language independently needs the same complete
binding and identity operation.

Reject a shared compiler IR if it needs opaque values, generic calls,
exceptions, language tags, Ruby/Luau operations, or recovery semantics inside
the shared module. Reconsider that rejection only after two production
frontends and two independent emitters converge on the same complete operation
and type model without those escapes.

Reject the sealed image if it misses the material-win gates above, regresses
steady execution by more than 2 percent, materially worsens decode/startup, or
cannot validate with bounded allocation before owner publication while
preserving exact identity and behavior. Reconsider it only after a new
production-size input, codec, or toolchain invalidates that evidence.

Revert Sprig to the simpler immutable, concurrency-safe, return-by-value
executor if owner-bound scratch and output reuse improve representative
end-to-end composite execution by less than 5 percent without a material
allocation or RSS win, or if their ownership/lifetime contract requires copies
that erase the gain. Conversely, explicit execution lanes are not accepted if
their idle engines materially regress startup or RSS.

Do not make PGO or a higher ISA tier a default unless it clears its separate
all-37 speed/build gate and remains fully identity-bound and host-admitted.

The architecture is unsuitable for an application whose required persistent
semantics cannot fit a bounded detached checkpoint or whose languages require
shared heap identity, callbacks, or fine-grained synchronous guest calls. That
does not change this ADR's selected coarse application-record scope; it does
falsify using the architecture for that application.

## Consequences

- Adding a language still requires its grammar, semantic analysis, canonical
  behavior, runtime representation where needed, and standard library. No
  honest architecture can swap those semantics away.
- Languages do not need to rebuild cgo-free deployment, Go compilation,
  content addressing, worker supervision, durable transactions, candidate
  catch-up, activation, or retirement.
- A small static language avoids building a machine-code backend and most of a
  dynamic runtime by lowering to typed Go.
- Language-owned engines may reuse bounded owner-local hot state without
  forcing synchronization or a universal execution frame on other languages;
  the simpler immutable executor wins when measurement does not pay for that
  ownership contract.
- A sealed dynamic-language image may remove parse/check/compile work after
  worker launch, but it adds a language-owned codec and is adopted only when
  production-size build-to-READY or RSS evidence pays for it.
- Static composition gives third parties an extension path without granting
  runtime registration or loader authority.
- Cross-language calls remain explicit application orchestration rather than a
  hidden universal object model.
- The public surface grows by one deep immutable value package containing
  `Set` and `Layout`; compiler and runtime variation remains behind concrete
  language interfaces.
- Only the bounded application checkpoint survives reload. A language that
  cannot project required state into it cannot use transactional hot reload
  under this architecture.
- Source-to-ready still includes the Go toolchain. Package-level caching,
  content identity, and off-active-path candidate preparation mitigate that
  cost; they do not make it disappear.

## Alternatives considered

- **One universal Runtime/Value/Frame model:** rejected because it either
  becomes Ruby-shaped and slows static/Luau paths or loses required Ruby
  semantics.
- **One public typed Core IR:** rejected because its apparent flexibility moves
  every language decision into a large shallow interface.
- **Generalize current Luau backend IR:** rejected because that IR already
  embeds Luau registers, opcodes, tags, tables, calls, yield, and replay.
- **A public scalar IR immediately:** rejected until two real producers prove
  construction ergonomics, exact recovery, and performance.
- **A runtime plugin or registration system:** rejected for cgo, portability,
  unload, identity, hidden effects, and stale-generation lifetime.
- **One worker per language:** rejected because mixed transactions would add
  IPC and distributed commit while still needing an application coordinator.
- **A universal Engine/owner interface:** rejected because ownership, limits,
  cancellation, output lifetime, and cleanup differ materially by language.
- **Owner-bound Sprig execution from day one:** rejected. Start with the
  immutable return-by-value function and add an Engine only if reusable
  scratch/output clears its comparative gate.
- **Raw `[]Mount` canonicalized by every consumer:** rejected because it makes
  embedded and worker consumers duplicate cross-mount policy and identity;
  opaque `Layout` makes `preparedsource` deeper.
- **Producer-owned multi-package/path artifact:** rejected because deployment
  placement would contaminate compiler identity and prevent reuse of one Set
  across embedded, worker, and test layouts.
- **Generalize Go overlay from a directory anchor:** rejected by the real Go
  1.26 preflight. Overlay replaces loader-enumerated files and did not inject
  any new Layout leaf. Per-leaf placeholders merely duplicate Layout inventory.
- **Mutate the application tree in place:** rejected for partial multi-file
  visibility, stale-leaf recovery, concurrent-generation collision, watcher
  noise, and nonportable replacement semantics.
- **Have the builder copy the caller's complete module:** rejected because it
  gives a narrow Go build owner arbitrary traversal, symlink, snapshot,
  nested-module, and potentially 262,144-entry copy policy before the existing
  selected-closure scans.
- **Put generated packages in nested modules or `go.work`:** rejected because
  it changes import, `internal`, replacement, and module-identity policy,
  conflicts with fixed `GOWORK=off`, and does not support arbitrary mounts
  without new application configuration.
- **Direct `go tool link`, producer-supplied archives, a persistent compiler
  daemon, or preforked workers:** rejected as defaults because they expand the
  trusted toolchain/process and cache-identity surface before measured
  build-to-READY evidence justifies it.
- **One generated package per function or Proto:** rejected because Go caches
  packages rather than files and the scheme sacrifices inlining while
  multiplying import, wrapper, and identity cost.
- **Move Luau out of root immediately:** rejected until another executable
  first-party language makes package symmetry real.

## Relationship to earlier ADRs

This ADR is superseded as an architecture decision by ADR 0013. Its bounded
proofs and rejected candidates remain historical evidence. ADR 0014 owns the
layered identity and invalidation decision, and ADR 0015 owns architecture
promotion gates. None of those records accepts this ADR's unfinished S4,
S5b, S7, R1, I1, I2, M1, X1, or X2 stages.

S1 does not supersede ADR 0011; it makes that ADR's selected complete build
identity enforceable before S0 certifies the worker baseline. S0 is an
implementation-and-evidence gate and does not reinterpret ADR status.

Acceptance of S4's language-neutral builder and descriptor V2 supersedes only
ADR 0011's Luau-specific artifact-construction and build-identity fields.
Acceptance of S5a establishes only the external static artifact boundary;
acceptance of S5b establishes cross-language worker delivery. Sprig, Ruby,
sealed images, shared compiler extraction, and package migration remain
separately gated and supersede nothing until their own slices are accepted.

ADR 0011's transaction, process, uncertainty, restore/follow, quiescent
activation, portability, and lifetime decisions remain authoritative. ADR 0008
continues to own Luau's prepared execution and exact Machine replay; it does
not become a cross-language compiler design.
