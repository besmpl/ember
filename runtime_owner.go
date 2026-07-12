package ember

import (
	"errors"
	"sync"
)

var (
	errRuntimeOwnerActive   = errors.New("runtime owner is active")
	errRuntimeOwnerClosed   = errors.New("runtime owner is closed")
	errRuntimeOwnerReleased = errors.New("runtime owner token is released")
	errRuntimeOwnerInvalid  = errors.New("runtime owner argument is invalid")
	errRuntimeOwnerOpaque   = errors.New("runtime owner has opaque suspended roots")
)

type runtimeOwnerState uint8

const (
	runtimeOwnerOpen runtimeOwnerState = iota
	runtimeOwnerClosed
)

// runtimeOwner serializes heap ownership and lifecycle transitions. The
// owner is private: public Runtime/Value APIs can acquire explicit leases and
// pins without exposing heap or token representations.
type runtimeOwner struct {
	mu sync.Mutex

	heap  *runtimeHeap
	state runtimeOwnerState

	activeRuns int
	// activeSlotRuns keeps close from racing an owner-bound compact execution
	// that does not acquire a VM thread registration.
	activeSlotRuns int
	threads        map[*vmThread]struct{}
	coroutines     map[*vmCoroutine]struct{}
	// coroutineRefs retains suspended owned coroutines so Close can dispose
	// transferred stacks without treating suspension as active execution.
	coroutineRefs map[*vmCoroutine]struct{}

	roots map[uint64]*runtimeRoot
	pins  map[uint64]*runtimePin

	pinStates map[slot]runtimeOwnerPinState
	nextToken uint64

	idleVMCaches []vmCacheBundle
}

type runtimeOwnerPinState struct {
	count    int
	addedPin bool
}

func newRuntimeOwner() *runtimeOwner {
	return &runtimeOwner{
		heap: &runtimeHeap{},
	}
}

type runtimeRunLease struct {
	owner *runtimeOwner
	once  sync.Once
}

func (owner *runtimeOwner) beginRun() (*runtimeRunLease, error) {
	if owner == nil {
		return nil, errRuntimeOwnerReleased
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == runtimeOwnerClosed {
		return nil, errRuntimeOwnerClosed
	}
	owner.activeRuns++
	return &runtimeRunLease{owner: owner}, nil
}

func (lease *runtimeRunLease) end() {
	if lease == nil {
		return
	}
	lease.once.Do(func() {
		if lease.owner == nil {
			return
		}
		lease.owner.mu.Lock()
		if lease.owner.activeRuns > 0 {
			lease.owner.activeRuns--
		}
		lease.owner.mu.Unlock()
	})
}

func (lease *runtimeRunLease) release() {
	lease.end()
}

// beginSlotRun accounts for a compact owner-bound execution that does not
// acquire a VM thread. The activity counter is deliberately kept on the owner
// rather than in a lease object so the hot path remains allocation-free. The
// caller must pair it with endSlotRun exactly once.
func (owner *runtimeOwner) beginSlotRun() error {
	if owner == nil {
		return errRuntimeOwnerReleased
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == runtimeOwnerClosed {
		return errRuntimeOwnerClosed
	}
	owner.activeSlotRuns++
	return nil
}

func (owner *runtimeOwner) endSlotRun() {
	if owner == nil {
		return
	}
	owner.mu.Lock()
	if owner.activeSlotRuns > 0 {
		owner.activeSlotRuns--
	}
	owner.mu.Unlock()
}

func (owner *runtimeOwner) close() error {
	if owner == nil {
		return nil
	}
	owner.mu.Lock()
	if owner.state == runtimeOwnerClosed {
		owner.mu.Unlock()
		return nil
	}
	if owner.activeRuns != 0 || owner.activeSlotRuns != 0 || len(owner.threads) != 0 || len(owner.coroutines) != 0 {
		owner.mu.Unlock()
		return errRuntimeOwnerActive
	}
	owner.state = runtimeOwnerClosed
	owner.idleVMCaches = nil
	coroutines := make([]*vmCoroutine, 0, len(owner.coroutineRefs))
	for coroutine := range owner.coroutineRefs {
		coroutines = append(coroutines, coroutine)
	}
	clear(owner.coroutineRefs)
	for _, root := range owner.roots {
		root.released = true
	}
	clear(owner.roots)
	owner.mu.Unlock()
	for _, coroutine := range coroutines {
		coroutine.disposeFrames()
	}
	return nil
}

// collect performs one stop-the-world collection while the owner is idle.
// Holding owner.mu across the heap walk closes the admission gate for runs,
// slot runs, thread registrations, roots, and pins. Callers may contribute
// roots that still use the public Value representation through scan.
func (owner *runtimeOwner) collect(scan func(*runtimeHeapCollector)) (runtimeHeapStats, error) {
	if owner == nil {
		return runtimeHeapStats{}, errRuntimeOwnerReleased
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == runtimeOwnerClosed {
		return runtimeHeapStats{}, errRuntimeOwnerClosed
	}
	if owner.activeRuns != 0 || owner.activeSlotRuns != 0 || len(owner.threads) != 0 || len(owner.coroutines) != 0 {
		return runtimeHeapStats{}, errRuntimeOwnerActive
	}
	if runtimeCoroutinesHaveOpaqueRoots(owner.coroutineRefs) {
		return runtimeHeapStats{}, errRuntimeOwnerOpaque
	}

	roots := make([]slot, 0, len(owner.roots)+len(owner.pins))
	for _, root := range owner.roots {
		if root != nil && !root.released {
			roots = append(roots, root.handle)
		}
	}
	// Pin metadata lives on typed slabs, but boxed NaN payloads have no pin
	// bit. Treat every live pin token as an explicit root so all slot families
	// receive the same external-lifetime guarantee.
	for _, pin := range owner.pins {
		if pin != nil && !pin.released {
			roots = append(roots, pin.handle)
		}
	}
	// These bundles contain only weak accelerators. Keeping them across a
	// collection would accidentally retain tables, strings, and closures.
	owner.idleVMCaches = nil
	stats, err := owner.heap.collectWithScanner(roots, func(collector *runtimeHeapCollector) {
		for coroutine := range owner.coroutineRefs {
			collector.scanCoroutine(coroutine)
		}
		if scan != nil {
			scan(collector)
		}
	})
	return stats, err
}

func runtimeCoroutinesHaveOpaqueRoots(coroutines map[*vmCoroutine]struct{}) bool {
	if len(coroutines) == 0 {
		return false
	}
	seenCoroutines := make(map[*vmCoroutine]struct{}, len(coroutines))
	frameOpaque := func(frame *vmFrame) bool {
		if frame == nil {
			return false
		}
		return frame.hasPendingCall && frame.pendingCall.host != nil
	}
	var coroutineOpaque func(*vmCoroutine) bool
	coroutineOpaque = func(coroutine *vmCoroutine) bool {
		if coroutine == nil {
			return false
		}
		if _, seen := seenCoroutines[coroutine]; seen {
			return false
		}
		seenCoroutines[coroutine] = struct{}{}
		if coroutine.thread.debugHook != nil || coroutine.suspended.debugHook != nil {
			return true
		}
		for _, frame := range coroutine.thread.frames {
			if frameOpaque(frame) {
				return true
			}
		}
		for _, frame := range coroutine.suspended.frames {
			if frameOpaque(frame) {
				return true
			}
		}
		return coroutineOpaque(coroutine.thread.coroutine) || coroutineOpaque(coroutine.suspended.coroutine)
	}
	for coroutine := range coroutines {
		if coroutineOpaque(coroutine) {
			return true
		}
	}
	return false
}

const maxIdleVMCacheBundles = 4

func (owner *runtimeOwner) checkoutVMThread(thread *vmThread) (vmCacheBundle, error) {
	if owner == nil || thread == nil {
		return vmCacheBundle{}, errRuntimeOwnerInvalid
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == runtimeOwnerClosed {
		return vmCacheBundle{}, errRuntimeOwnerClosed
	}
	if owner.threads == nil {
		owner.threads = make(map[*vmThread]struct{})
	}
	owner.threads[thread] = struct{}{}
	if len(owner.idleVMCaches) == 0 {
		return vmCacheBundle{}, nil
	}
	last := len(owner.idleVMCaches) - 1
	bundle := owner.idleVMCaches[last]
	owner.idleVMCaches[last] = vmCacheBundle{}
	owner.idleVMCaches = owner.idleVMCaches[:last]
	return bundle, nil
}

func (owner *runtimeOwner) returnVMThread(thread *vmThread, bundle vmCacheBundle) {
	if owner == nil || thread == nil {
		return
	}
	owner.mu.Lock()
	if owner.state == runtimeOwnerOpen && len(owner.idleVMCaches) < maxIdleVMCacheBundles {
		owner.idleVMCaches = append(owner.idleVMCaches, bundle)
	}
	delete(owner.threads, thread)
	owner.mu.Unlock()
}

type runtimeRoot struct {
	owner    *runtimeOwner
	handle   slot
	tokenID  uint64
	released bool
}

func (owner *runtimeOwner) root(handle slot) (*runtimeRoot, error) {
	if owner == nil {
		return nil, errRuntimeOwnerReleased
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == runtimeOwnerClosed {
		return nil, errRuntimeOwnerClosed
	}
	owner.heap.collectMu.Lock()
	defer owner.heap.collectMu.Unlock()
	// A slot is meaningful only in its runtime owner's heap. Handles omit heap
	// identity deliberately, so callers must never move raw slots between
	// owners; public Values are the cross-owner adapter.
	if err := owner.heap.validateSlot(handle); err != nil {
		return nil, err
	}
	owner.nextToken++
	root := &runtimeRoot{owner: owner, handle: handle, tokenID: owner.nextToken}
	if owner.roots == nil {
		owner.roots = make(map[uint64]*runtimeRoot)
	}
	owner.roots[root.tokenID] = root
	return root, nil
}

// rootValues imports and registers a group of public Values atomically. It is
// used by owner-bearing escape wrappers such as Callback so collection cannot
// observe a partially rooted capture.
func (owner *runtimeOwner) rootValues(values []Value) ([]*runtimeRoot, error) {
	if owner == nil {
		return nil, errRuntimeOwnerReleased
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == runtimeOwnerClosed {
		return nil, errRuntimeOwnerClosed
	}
	owner.heap.collectMu.Lock()
	defer owner.heap.collectMu.Unlock()
	roots := make([]*runtimeRoot, 0, len(values))
	created := make([]slot, 0, len(values))
	for _, value := range values {
		_, found, _ := owner.heap.lookupExistingValue(value)
		handle, err := owner.heap.importValue(value)
		if err != nil {
			for _, root := range roots {
				delete(owner.roots, root.tokenID)
				root.released = true
			}
			for _, createdHandle := range created {
				if pinned, pinErr := owner.heap.handlePinned(createdHandle); pinErr == nil && pinned {
					_ = owner.heap.unpinHandle(createdHandle)
				}
				_ = owner.heap.releaseHandle(createdHandle)
			}
			return nil, err
		}
		if !found && slotIsLiveHandle(handle) {
			created = append(created, handle)
		}
		owner.nextToken++
		root := &runtimeRoot{owner: owner, handle: handle, tokenID: owner.nextToken}
		if owner.roots == nil {
			owner.roots = make(map[uint64]*runtimeRoot)
		}
		owner.roots[root.tokenID] = root
		roots = append(roots, root)
	}
	return roots, nil
}

func (root *runtimeRoot) value() (slot, error) {
	if root == nil || root.owner == nil {
		return 0, errRuntimeOwnerReleased
	}
	owner := root.owner
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if root.released {
		return 0, errRuntimeOwnerReleased
	}
	if _, ok := owner.roots[root.tokenID]; !ok {
		return 0, errRuntimeOwnerReleased
	}
	owner.heap.collectMu.Lock()
	defer owner.heap.collectMu.Unlock()
	if err := owner.heap.validateSlot(root.handle); err != nil {
		return 0, err
	}
	return root.handle, nil
}

func (root *runtimeRoot) release() {
	if root == nil || root.owner == nil {
		return
	}
	owner := root.owner
	owner.mu.Lock()
	if !root.released {
		delete(owner.roots, root.tokenID)
		root.released = true
	}
	owner.mu.Unlock()
}

func lookupSlabValue[T comparable](slab *slotSlab[T], kind slotTag, value T) (slot, bool, bool) {
	if slab == nil || slab.byValue == nil {
		return 0, false, false
	}
	index, ok := slab.byValue[value]
	if !ok || int(index) >= len(slab.entries) {
		return 0, false, false
	}
	entry := slab.entries[index]
	if !entry.live || entry.retired || entry.value != value {
		return 0, false, false
	}
	handle, err := slotPackHandle(kind, index, entry.generation)
	if err != nil {
		return 0, false, false
	}
	return handle, true, entry.pinned
}

func (heap *runtimeHeap) lookupExistingValue(value Value) (slot, bool, bool) {
	if heap == nil {
		return 0, false, false
	}
	switch valueKind(value) {
	case StringKind:
		if box := value.stringBox(); box != nil {
			return lookupSlabValue(&heap.strings, slotTagString, box)
		}
	case TableKind:
		if table := value.tableRef(); table != nil {
			return lookupSlabValue(&heap.tables, slotTagTable, table)
		}
	case FunctionKind:
		if closure, ok := value.scriptFunction(); ok && closure != nil {
			return lookupSlabValue(&heap.closures, slotTagClosure, closure)
		}
	case UserDataKind:
		if userdata := value.userdataRef(); userdata != nil {
			return lookupSlabValue(&heap.userdata, slotTagUserdata, userdata)
		}
	case HostFuncKind:
		if callable := value.hostCallableRef(); callable != nil {
			return lookupSlabValue(&heap.hostCallables, slotTagHostCallable, callable)
		}
	}
	return 0, false, false
}

func valueHasDefaultOpaquePin(value Value) bool {
	return valueKind(value) == UserDataKind || valueKind(value) == HostFuncKind
}

type runtimePin struct {
	owner    *runtimeOwner
	handle   slot
	tokenID  uint64
	released bool
}

func (owner *runtimeOwner) pin(value Value) (*runtimePin, error) {
	if owner == nil {
		return nil, errRuntimeOwnerReleased
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == runtimeOwnerClosed {
		return nil, errRuntimeOwnerClosed
	}
	owner.heap.collectMu.Lock()
	defer owner.heap.collectMu.Unlock()
	_, found, wasPinned := owner.heap.lookupExistingValue(value)
	handle, err := owner.heap.importValue(value)
	if err != nil {
		return nil, err
	}
	addedPin := false
	if slotIsLiveHandle(handle) {
		addedPin = (found && !wasPinned) || (!found && !valueHasDefaultOpaquePin(value))
		if owner.pinStates == nil {
			owner.pinStates = make(map[slot]runtimeOwnerPinState)
		}
		state := owner.pinStates[handle]
		if state.count == 0 {
			state.addedPin = addedPin
		} else if addedPin && !state.addedPin {
			state.addedPin = true
		}
		if addedPin {
			if err := owner.heap.pinHandle(handle); err != nil {
				return nil, err
			}
		}
		state.count++
		owner.pinStates[handle] = state
	}
	owner.nextToken++
	pin := &runtimePin{owner: owner, handle: handle, tokenID: owner.nextToken}
	if owner.pins == nil {
		owner.pins = make(map[uint64]*runtimePin)
	}
	owner.pins[pin.tokenID] = pin
	return pin, nil
}

func (pin *runtimePin) value() (Value, error) {
	if pin == nil || pin.owner == nil {
		return NilValue(), errRuntimeOwnerReleased
	}
	owner := pin.owner
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if pin.released {
		return NilValue(), errRuntimeOwnerReleased
	}
	if _, ok := owner.pins[pin.tokenID]; !ok {
		return NilValue(), errRuntimeOwnerReleased
	}
	owner.heap.collectMu.Lock()
	defer owner.heap.collectMu.Unlock()
	return owner.heap.exportValue(pin.handle)
}

func (pin *runtimePin) release() {
	if pin == nil || pin.owner == nil {
		return
	}
	owner := pin.owner
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if pin.released {
		return
	}
	owner.heap.collectMu.Lock()
	defer owner.heap.collectMu.Unlock()
	delete(owner.pins, pin.tokenID)
	if state, ok := owner.pinStates[pin.handle]; ok {
		if state.count <= 1 {
			delete(owner.pinStates, pin.handle)
			if state.addedPin {
				_ = owner.heap.unpinHandle(pin.handle)
			}
		} else {
			state.count--
			owner.pinStates[pin.handle] = state
		}
	}
	pin.released = true
}

func slotIsLiveHandle(value slot) bool {
	if !slotIsTagged(value) {
		return false
	}
	_, index, generation, err := slotUnpackHandle(value)
	return err == nil && index != 0 && generation != 0
}

// Handle bits intentionally omit a heap-owner identity. This private owner
// context is therefore the authority for interpreting every non-immediate
// slot; cross-owner use is outside this seam until an owner-bearing API exists.
func (heap *runtimeHeap) validateSlot(value slot) error {
	if heap == nil {
		return errRuntimeOwnerReleased
	}
	if !slotIsTagged(value) {
		return nil
	}
	switch slotTagOf(value) {
	case slotTagNil:
		if !slotImmediatePayloadZero(value) {
			return errRuntimeOwnerInvalid
		}
		return nil
	case slotTagFalse, slotTagTrue:
		_, err := slotBoolValue(value)
		return err
	case slotTagNativeID:
		_, err := slotNativeIDValue(value)
		return err
	case slotTagString, slotTagTable, slotTagClosure, slotTagUpvalue, slotTagUserdata, slotTagHostCallable, slotTagBoxedNumber:
		kind, index, generation, err := slotUnpackHandle(value)
		if err != nil {
			return err
		}
		if index == 0 && generation == 0 {
			if kind == slotTagBoxedNumber {
				return errRuntimeOwnerInvalid
			}
			return nil
		}
		if index == 0 || generation == 0 {
			return errRuntimeOwnerInvalid
		}
		_, _, err = heap.validateHandle(value, kind)
		return err
	default:
		return errRuntimeOwnerInvalid
	}
}

func (heap *runtimeHeap) pinHandle(value slot) error {
	kind, index, generation, err := slotUnpackHandle(value)
	if err != nil {
		return err
	}
	if index == 0 || generation == 0 {
		return errRuntimeOwnerInvalid
	}
	switch kind {
	case slotTagString:
		return heap.strings.pin(index, generation)
	case slotTagTable:
		return heap.tables.pin(index, generation)
	case slotTagClosure:
		return heap.closures.pin(index, generation)
	case slotTagUpvalue:
		return heap.upvalues.pin(index, generation)
	case slotTagUserdata:
		return heap.userdata.pin(index, generation)
	case slotTagHostCallable:
		return heap.hostCallables.pin(index, generation)
	case slotTagBoxedNumber:
		return nil
	default:
		return errRuntimeOwnerInvalid
	}
}

func (heap *runtimeHeap) handlePinned(value slot) (bool, error) {
	kind, index, generation, err := slotUnpackHandle(value)
	if err != nil {
		return false, err
	}
	if index == 0 && generation == 0 {
		return false, nil
	}
	if index == 0 || generation == 0 {
		return false, errRuntimeOwnerInvalid
	}
	switch kind {
	case slotTagString:
		return heap.strings.handlePinned(index, generation)
	case slotTagTable:
		return heap.tables.handlePinned(index, generation)
	case slotTagClosure:
		return heap.closures.handlePinned(index, generation)
	case slotTagUpvalue:
		return heap.upvalues.handlePinned(index, generation)
	case slotTagUserdata:
		return heap.userdata.handlePinned(index, generation)
	case slotTagHostCallable:
		return heap.hostCallables.handlePinned(index, generation)
	case slotTagBoxedNumber:
		return false, nil
	default:
		return false, errRuntimeOwnerInvalid
	}
}

func (slab *slotSlab[T]) pin(index uint32, generation uint16) error {
	if index == 0 || int(index) >= len(slab.entries) {
		return errRuntimeOwnerInvalid
	}
	entry := &slab.entries[index]
	if !entry.live || entry.retired || entry.generation != generation {
		return errRuntimeOwnerReleased
	}
	entry.pinned = true
	return nil
}

func (slab *slotSlab[T]) handlePinned(index uint32, generation uint16) (bool, error) {
	if index == 0 || int(index) >= len(slab.entries) {
		return false, errRuntimeOwnerInvalid
	}
	entry := slab.entries[index]
	if !entry.live || entry.retired || entry.generation != generation {
		return false, errRuntimeOwnerReleased
	}
	return entry.pinned, nil
}

func (slab *slotSlab[T]) pinnedFor(value slot) bool {
	if !slotIsLiveHandle(value) {
		return false
	}
	_, index, generation, err := slotUnpackHandle(value)
	if err != nil || index == 0 || int(index) >= len(slab.entries) {
		return false
	}
	entry := slab.entries[index]
	return entry.live && !entry.retired && entry.generation == generation && entry.pinned
}

func (owner *runtimeOwner) registerThread(thread *vmThread) error {
	if owner == nil || thread == nil {
		return errRuntimeOwnerInvalid
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == runtimeOwnerClosed {
		return errRuntimeOwnerClosed
	}
	if owner.threads == nil {
		owner.threads = make(map[*vmThread]struct{})
	}
	owner.threads[thread] = struct{}{}
	return nil
}

func (owner *runtimeOwner) unregisterThread(thread *vmThread) {
	if owner == nil || thread == nil {
		return
	}
	owner.mu.Lock()
	delete(owner.threads, thread)
	owner.mu.Unlock()
}

func (owner *runtimeOwner) registerCoroutine(coroutine *vmCoroutine) error {
	if owner == nil || coroutine == nil {
		return errRuntimeOwnerInvalid
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == runtimeOwnerClosed {
		return errRuntimeOwnerClosed
	}
	if owner.coroutines == nil {
		owner.coroutines = make(map[*vmCoroutine]struct{})
	}
	owner.coroutines[coroutine] = struct{}{}
	return nil
}

func (owner *runtimeOwner) unregisterCoroutine(coroutine *vmCoroutine) {
	if owner == nil || coroutine == nil {
		return
	}
	owner.mu.Lock()
	delete(owner.coroutines, coroutine)
	owner.mu.Unlock()
}

func (owner *runtimeOwner) retainCoroutine(coroutine *vmCoroutine) error {
	if owner == nil || coroutine == nil {
		return errRuntimeOwnerInvalid
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.state == runtimeOwnerClosed {
		return errRuntimeOwnerClosed
	}
	if owner.coroutineRefs == nil {
		owner.coroutineRefs = make(map[*vmCoroutine]struct{})
	}
	owner.coroutineRefs[coroutine] = struct{}{}
	return nil
}

func (owner *runtimeOwner) releaseCoroutine(coroutine *vmCoroutine) {
	if owner == nil || coroutine == nil {
		return
	}
	owner.mu.Lock()
	delete(owner.coroutineRefs, coroutine)
	owner.mu.Unlock()
}

func (owner *runtimeOwner) threadCount() int {
	if owner == nil {
		return 0
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return len(owner.threads)
}

func (owner *runtimeOwner) coroutineCount() int {
	if owner == nil {
		return 0
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return len(owner.coroutines)
}
