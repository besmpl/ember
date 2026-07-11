# ADR 0004: Runtime Slots, Handle Ownership, and Collection

## Context

Ember's public `Value` currently has a 16-byte, Go-GC-visible representation:
an `unsafe.Pointer` plus a 64-bit payload. Tables, closures, userdata, and host
callables are ordinary Go objects, and public constructors return Go pointers.
This is the safe compatibility boundary established by ADR 0001.

The runtime parity campaign now has measured pressure in a different place:
registers, arguments, constants, and table cells copy wide pointer-bearing
Values through the VM. The campaign therefore calls for an 8-byte private slot,
typed object slabs, stale-handle detection, and an Ember-owned collector. That
design cannot be added by changing only `Value`: public Values, `*Table`,
`*UserData`, host callbacks, coroutine userdata, and arbitrary host payloads can
all escape the VM.

The ownership problem is visible in the current shapes:

- `Table` values can point to other tables, userdata, string boxes, metatables,
  hash keys, iteration journals, and values, including cycles.
- `closure` values retain prototypes, open cells, and copied captured Values.
- `cell` values can retain a stack owner after a frame has been released.
- `vmCoroutine` is carried by `UserData.payload` and owns a thread, root
  closure, suspended frames, yielded values, and resume buffers.
- `hostCallable` contains opaque Go function closures. Ember cannot inspect
  Values captured by those closures.
- `Runtime` retains module exports and loaded values, while `Program` retains
  immutable prototypes and their constants.
- Pooled `vmThread` instances can retain intern tables, function instances, and
  cache references unless reset is part of the root protocol.

An explicit collector must know every root and every edge, and must define what
happens when a value crosses into Go. Go finalizers cannot provide that contract.

## Decision

Supersede only the internal-storage default in ADR 0001 when campaign evidence
shows that pointer-bearing VM slots are the measured bottleneck. Preserve the
public Go-object mapping and source compatibility. The public `Value`, `Table`,
`UserData`, host callback, and `Runtime` interfaces remain Go-facing adapters;
the private VM representation may become handle-based behind them.

The handle design is adopted only behind a private `runtimeHeap` seam. No raw
Go pointer or `uintptr` is stored in an integer slot, and no public caller is
required to understand a handle.

### Private slot and handle representation

The VM uses one machine word. The current red-tracer encoding reserves the
top sixteen bits as a quiet-NaN tagged prefix (`0x7ff8`), uses the next four
bits for the slot tag, and leaves a 44-bit payload:

```text
slot = direct IEEE-754 number
     | tagged nil/bool
     | tagged {kind, index, generation} handle

bits 63..48: 0x7ff8 tagged prefix
bits 47..44: four-bit kind/tag
bits 43..28: 16-bit generation
bits 27..00: 28-bit slab index
```

The four-bit tag selects the typed slab. Real handles use non-zero generation
and index values. The heap owner supplies the heap identity, so a handle need
not carry a process-global pointer or registry index. If generation space is
exhausted, the slot is retired instead of wrapping and making an old handle
valid again. Slot zero is the IEEE-754 `+0` number; nil is the explicit tagged
`slotNil` value. NaN payloads that collide with the tagged prefix use a rare
boxed-number object so all existing number bits survive.

Handles are comparable and may be used in table keys. Table and userdata
identity hashes use the handle identity (including generation) rather than a
reused address. Existing monotonic object IDs remain available for stable
iteration ordering where the language requires ordering independent of slab
reuse.

### Runtime ownership seam

Each persistent `Runtime` owns one `runtimeHeap`. A stateless `Run` or
`RunWithGlobals` call owns an ephemeral heap for the duration of the call.
`Program` and `Proto` remain immutable and are not heap owners.

The private heap interface owns:

- typed allocation and handle validation;
- stable slab/free-list storage;
- root and pin registration;
- stop-the-world collection and statistics;
- promotion of an internal slot to a public Go-owned `Value`.

The public adapters have two legal directions:

1. A public Go `Value` entering the VM is imported into the current heap and
   held as an external pin for the duration of the call.
2. A slot leaving the VM is promoted to a Go-owned public `Value` before the
   ephemeral owner is released.

The existing `Run` return type cannot carry a heap owner. Therefore every
reference reachable from a stateless result must be promoted before `Run`
returns. A future owner-bearing result API may avoid that promotion, but it is
an explicit API addition, not an implicit lifetime change.

Persistent runtimes keep their heap alive until `Runtime.Close`. A host value
retained beyond a callback must use an explicit pin/retain seam; merely copying
the public `Value` is not registration with the collector.

### Typed slabs

Use non-moving typed slabs with a common header:

```text
live | mark | pin count | generation | free-list link
```

The first implementation owns slabs for string boxes, tables, closures, cells
/upvalues, userdata, host callables, and rare boxed numbers. A table slab entry
must own the current `tableStorage` wrapper rather than moving the embedded
`Table` interior pointer. Large backing arrays, hash storage, and strings may
remain Go allocations owned by the slab entry until sweep clears them.

The slab is stable: handles never change when another object is allocated or
freed. Reusing a slot increments its generation. A stale handle fails closed
in debug and test builds and produces nil/error behavior at the runtime seam;
it must never alias the new occupant.

### External lifetime and pinning

The collector uses explicit roots and pins:

- internal stack/frame storage is scanned as a root range;
- `Runtime` entrypoint and loaded-value maps are roots while the runtime is
  open;
- immutable `Program`/`Proto` constants are roots while their owner is live;
- suspended coroutines, open cells, and pending calls are roots;
- public or host-retained objects use a pin token with explicit release.

`UserData.payload` is opaque. It is pinned by default. A future internal
payload may implement a private tracer for known types such as `*vmCoroutine`;
arbitrary Go payloads are never guessed or reflected into.

`hostCallable` is also opaque because a Go function closure may capture Ember
Values. Host callables remain pinned until their owner releases them unless a
host adapter supplies an explicit root tracer.

`NewTable` and `NewUserData` continue to return Go-owned pointers for public
compatibility. Such objects are imported as externally pinned nodes when passed
into a runtime. Script-created objects may live in the runtime heap, but any
object graph that escapes through a public Value is promoted or pinned before
the owner can be collected.

`Runtime.Close` rejects new work, drops runtime roots, and releases the heap
only after active calls and child coroutines have ended. Outstanding external
pins keep their objects valid; using an unpinned value after close is an
explicit lifetime error, not undefined behavior.

### Root scanning and weak state

The first root scanner covers:

- active `vmThread` stack owners, frames, registers, varargs, result windows,
  cells, protected handlers, and globals;
- suspended `vmCoroutine` frames, stacks, root closure, yielded values, and
  resume buffers;
- `Runtime` maps and host globals;
- live `Program` prototypes and reference constants;
- explicit public/host pins.

Inline caches, metamethod lookup caches, function-instance ICs, and string
intern maps are weak runtime accelerators, not semantic roots. They are cleared
or invalidated during collection. If a cache is needed after collection, it is
re-warmed from the owning table/prototype graph.

Pooled threads must clear heap references before returning to `sync.Pool`, or
the pool must be registered as a root owner. A collector must never rely on the
Go runtime's opaque pool retention policy.

### First collector

Slice 4.5 implements a simple stop-the-world mark/sweep collector. Collection
is allowed only when the heap owner reports no active VM execution. The steps
are:

1. acquire the heap/world lock;
2. snapshot internal roots and external pins;
3. mark handles through a worklist, dispatching to a typed scanner per slab;
4. scan table keys/values, closure captures, cells, coroutine state, and all
   other typed edges while deduplicating by handle and generation;
5. sweep unmarked, unpinned entries;
6. clear Go slices, maps, strings, and pointers in dead entries;
7. increment generations and return reusable entries to free lists, retiring
   entries whose generation would wrap;
8. publish allocation, live, reclaimed, pause, and stale-handle statistics.

The first collector has no write barrier because collection is stop-the-world.
Incremental or generational marking, barriers, and nursery policy are deferred
until profiles prove that stop-the-world pauses are material.

No user finalizer runs under the heap lock. Resource ownership remains explicit
through host/runtime close methods.

## Staged migration

1. **Representation tracers.** Keep the current public Value tests as the red
   contract: exact size, NaN bits, typed nils, identity, forced Go GC, and
   scalar zero-allocation behavior. Add private slot encoding, stale-handle,
   and generation-wrap tests without changing production execution.
2. **Ownership adapter.** Add a private heap owner, root tokens, pin tokens,
   promotion/import helpers, active-run accounting, and pool reset rules. Prove
   stateless result promotion and persistent `Runtime.Close` behavior.
3. **Slab-backed objects.** Move one object family at a time, starting with
   strings and tables, while importing public Go objects as pinned adapters.
   Add typed trace functions before allowing that family to be swept.
4. **Internal slots.** Convert registers, constants, globals, arguments,
   varargs, and results to slots. Opcode bodies must stop repeatedly converting
   between `Value` and `slot`; conversion stays at host/public seams.
5. **Collector cutover.** Enable stop-the-world mark/sweep only after every
   live object edge has a scanner and every escape path has a root/pin rule.
   Delete the old pointer-bearing hot paths only after parity, race, checkptr,
   and allocation gates pass.

## Consequences

This preserves the public Go-facing model while creating a deep private runtime
module with a small ownership interface. It also makes lifetime obligations
explicit instead of hiding them in a global handle registry.

The mixed migration period is expected to cost conversions and may regress
performance. The handle collector is retained only if the full corpus shows a
broad reduction in pointer scanning, register traffic, and GC cost; a microbench
win is insufficient.

The design adds pause and pin-management complexity, and host adapters must
learn explicit lifetime rules. That cost is accepted only for internal runtime
slots, not for a public handle API.

## Alternatives considered

- **Keep direct Go pointers everywhere.** Safest and simplest, and remains the
  public adapter representation. Rejected only for measured internal slot
  pressure, not as a public API decision.
- **Store Go pointers or `uintptr` in 64-bit slots.** Rejected: it hides
  pointers from the Go collector and weakens `checkptr`/liveness guarantees.
- **Global handle registry.** Rejected: it creates cross-runtime ownership,
  synchronization, unbounded retention, and generation policy in one hidden
  singleton.
- **Moving or compacting slabs.** Rejected for the first collector: public raw
  pointers, table identity, cache slots, and coroutine references require
  stable addresses or an additional relocation protocol.
- **Trace arbitrary userdata or Go callback closures automatically.** Rejected
  because Go does not expose safe closure/object graph reflection. Pin opaque
  values instead.
- **Use finalizers as lifetime management.** Rejected because finalization is
  nondeterministic and cannot define callback, coroutine, or Runtime.Close
  semantics.
