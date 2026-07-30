package rubyproof

import (
	"context"
	"errors"
)

const (
	h1bObjectCapacity      = 4
	h1bEnvironmentCapacity = 2
	h1bRootCapacity        = 8
	h1bObjectFields        = 2
	h1bEnvironmentCells    = 1
	h1bMarkCapacity        = h1bObjectCapacity + h1bEnvironmentCapacity

	h1bReferenceIndexBits = 8
	h1bReferenceIndexMask = 1<<h1bReferenceIndexBits - 1
	h1bMaximumGeneration  = 1<<(32-h1bReferenceIndexBits) - 1
)

var (
	// ErrH1bBusy reports concurrent use of one H1b owner.
	ErrH1bBusy = errors.New("ruby: H1b runtime is busy")
	// ErrH1bClosed reports use after Close.
	ErrH1bClosed = errors.New("ruby: H1b runtime is closed")
	// ErrH1bPoisoned reports reuse after an interrupted run.
	ErrH1bPoisoned = errors.New("ruby: H1b runtime is poisoned")
	// ErrH1bLimit reports a budget too small for the sealed H1b plan.
	ErrH1bLimit = errors.New("ruby: H1b execution limit exceeded")

	errH1bCapacity       = errors.New("ruby: H1b arena capacity exceeded")
	errH1bInvalidRef     = errors.New("ruby: invalid H1b arena reference")
	errH1bRootOrder      = errors.New("ruby: H1b arena root order mismatch")
	errH1bTransaction    = errors.New("ruby: invalid H1b singleton transaction")
	errH1bTransactionRun = errors.New("ruby: H1b singleton transaction is active")
	errH1bPlan           = errors.New("ruby: invalid H1b checked plan")
)

// H1bLimits are fresh for each Run. The exact checked product has no dynamic
// loop or recursion; these budgets bound its only guest allocations and
// collections.
type H1bLimits struct {
	Objects      uint64
	Environments uint64
	Collections  uint64
}

// H1bProofLimits returns the exact budget required by H1bSource.
func H1bProofLimits() H1bLimits {
	return H1bLimits{Objects: 2, Environments: 1, Collections: 3}
}

// H1bStats is a detached observation of one completed run. It contains no
// owner-bound Ruby reference.
type H1bStats struct {
	ObjectAllocations      uint64
	EnvironmentAllocations uint64
	ObjectReclaims         uint64
	EnvironmentReclaims    uint64
	Collections            uint64
	TotalMarkWork          uint64
	LastMarkWork           uint8
	MarkHighWater          uint8
	LiveObjects            uint8
	LiveEnvironments       uint8
	Roots                  uint8
}

// H1bResult is the exact detached scalar projection of H1bSource.
type H1bResult struct {
	Before                      int64
	Warm                        int64
	PeerBefore                  int64
	First                       int64
	Second                      int64
	PeerAfter                   int64
	PeerAfterReceiverCollection int64
	Stats                       H1bStats
}

type h1bObjectRef uint32
type h1bEnvironmentRef uint32

type h1bValueKind uint8

const (
	h1bValueNil h1bValueKind = iota
	h1bValueObject
	h1bValueEnvironment
)

type h1bValue struct {
	reference uint32
	kind      h1bValueKind
}

func h1bObjectValue(reference h1bObjectRef) h1bValue {
	return h1bValue{reference: uint32(reference), kind: h1bValueObject}
}

func h1bEnvironmentValue(reference h1bEnvironmentRef) h1bValue {
	return h1bValue{reference: uint32(reference), kind: h1bValueEnvironment}
}

type h1bObjectSlot struct {
	generation  uint32
	environment h1bEnvironmentRef
	fields      [h1bObjectFields]h1bValue
	nextFree    uint8
	dispatch    dispatchClassID
	shape       shapeID
	live        bool
	marked      bool
	retired     bool
	present     bool
}

type h1bEnvironmentSlot struct {
	cells      [h1bEnvironmentCells]int64
	generation uint32
	nextFree   uint8
	live       bool
	reserved   bool
	marked     bool
	retired    bool
}

type h1bMarkKind uint8

const (
	h1bMarkObject h1bMarkKind = iota + 1
	h1bMarkEnvironment
)

type h1bMarkItem struct {
	generation uint32
	index      uint8
	kind       h1bMarkKind
}

type h1bTransaction struct {
	id          uint32
	object      h1bObjectRef
	environment h1bEnvironmentRef
}

type h1bPICArm struct {
	object      h1bObjectRef
	environment h1bEnvironmentRef
}

type h1bPIC struct {
	ordinaryDispatch dispatchClassID
	singleton        h1bPICArm
	ordinaryHits     uint8
	ordinaryMisses   uint8
	singletonHits    uint8
	singletonMisses  uint8
	ordinaryValid    bool
	singletonValid   bool
}

type h1bCollectionWork struct {
	RootSlots             uint8
	RootValues            uint8
	MarkedObjects         uint8
	MarkedEnvironments    uint8
	ReclaimedObjects      uint8
	ReclaimedEnvironments uint8
}

func (work h1bCollectionWork) markWork() uint8 {
	return work.MarkedObjects + work.MarkedEnvironments
}

type h1bArena struct {
	objects      [h1bObjectCapacity]h1bObjectSlot
	environments [h1bEnvironmentCapacity]h1bEnvironmentSlot
	roots        [h1bRootCapacity]h1bValue
	markWork     [h1bMarkCapacity]h1bMarkItem

	statistics      H1bStats
	lastWork        h1bCollectionWork
	objectFree      uint8
	environmentFree uint8
	rootCount       uint8
	markCount       uint8
	nextTransaction uint32
	transaction     h1bTransaction
	closed          bool
}

func newH1bArena() h1bArena {
	arena := h1bArena{objectFree: 1, environmentFree: 1}
	for index := range arena.objects {
		arena.objects[index].generation = 1
		if index+1 < len(arena.objects) {
			arena.objects[index].nextFree = uint8(index + 2)
		}
	}
	for index := range arena.environments {
		arena.environments[index].generation = 1
		if index+1 < len(arena.environments) {
			arena.environments[index].nextFree = uint8(index + 2)
		}
	}
	return arena
}

func h1bPackReference(index uint8, generation uint32) uint32 {
	if index == 0 || generation == 0 || generation > h1bMaximumGeneration {
		return 0
	}
	return generation<<h1bReferenceIndexBits | uint32(index)
}

func h1bUnpackReference(reference uint32) (uint8, uint32, bool) {
	index := uint8(reference & h1bReferenceIndexMask)
	generation := reference >> h1bReferenceIndexBits
	return index, generation, index != 0 && generation != 0
}

func (arena *h1bArena) resetObservations() {
	arena.statistics = H1bStats{}
	arena.lastWork = h1bCollectionWork{}
}

func (arena *h1bArena) rootMarker() uint8 { return arena.rootCount }

func (arena *h1bArena) reserveRoot() (uint8, error) {
	if arena.closed {
		return 0, ErrH1bClosed
	}
	if int(arena.rootCount) == len(arena.roots) {
		return 0, errH1bCapacity
	}
	index := arena.rootCount
	arena.roots[index] = h1bValue{}
	arena.rootCount++
	arena.statistics.Roots = arena.rootCount
	return index, nil
}

func (arena *h1bArena) setRoot(index uint8, value h1bValue) error {
	if index >= arena.rootCount {
		return errH1bRootOrder
	}
	if err := arena.validateValue(value); err != nil {
		return err
	}
	arena.roots[index] = value
	return nil
}

func (arena *h1bArena) clearRoot(index uint8) error {
	if arena.closed {
		return ErrH1bClosed
	}
	if index >= arena.rootCount {
		return errH1bRootOrder
	}
	arena.roots[index] = h1bValue{}
	return nil
}

func (arena *h1bArena) popRoots(marker uint8) error {
	if arena.closed {
		return ErrH1bClosed
	}
	if marker > arena.rootCount {
		return errH1bRootOrder
	}
	for arena.rootCount > marker {
		arena.rootCount--
		arena.roots[arena.rootCount] = h1bValue{}
	}
	arena.statistics.Roots = arena.rootCount
	return nil
}

func (arena *h1bArena) validateValue(value h1bValue) error {
	switch value.kind {
	case h1bValueNil:
		if value != (h1bValue{}) {
			return errH1bInvalidRef
		}
	case h1bValueObject:
		_, err := arena.resolveObject(h1bObjectRef(value.reference))
		return err
	case h1bValueEnvironment:
		_, err := arena.resolveEnvironment(h1bEnvironmentRef(value.reference))
		return err
	default:
		return errH1bInvalidRef
	}
	return nil
}

func (arena *h1bArena) allocateObject(dispatch dispatchClassID, shape shapeID) (h1bObjectRef, error) {
	if arena.closed {
		return 0, ErrH1bClosed
	}
	index := arena.objectFree
	if index == 0 {
		return 0, errH1bCapacity
	}
	slot := &arena.objects[index-1]
	if slot.live || slot.retired || slot.generation == 0 || dispatch == 0 || shape == 0 {
		return 0, errH1bInvalidRef
	}
	arena.objectFree = slot.nextFree
	generation := slot.generation
	*slot = h1bObjectSlot{generation: generation, dispatch: dispatch, shape: shape, live: true}
	arena.statistics.ObjectAllocations++
	arena.statistics.LiveObjects++
	return h1bObjectRef(h1bPackReference(index, generation)), nil
}

func (arena *h1bArena) allocateObjectAtSafePoint(ctx context.Context, dispatch dispatchClassID, shape shapeID) (h1bObjectRef, error) {
	if err := h1bPoll(ctx); err != nil {
		return 0, err
	}
	if arena.objectFree == 0 {
		if err := arena.collect(ctx); err != nil {
			return 0, err
		}
	}
	return arena.allocateObject(dispatch, shape)
}

func (arena *h1bArena) resolveObject(reference h1bObjectRef) (*h1bObjectSlot, error) {
	if arena.closed {
		return nil, ErrH1bClosed
	}
	index, generation, ok := h1bUnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.objects) {
		return nil, errH1bInvalidRef
	}
	slot := &arena.objects[index-1]
	if !slot.live || slot.retired || slot.generation != generation {
		return nil, errH1bInvalidRef
	}
	return slot, nil
}

func (arena *h1bArena) storeObjectField(reference h1bObjectRef, field uint8, value h1bValue) error {
	slot, err := arena.resolveObject(reference)
	if err != nil {
		return err
	}
	if int(field) >= len(slot.fields) {
		return errH1bInvalidRef
	}
	if err := arena.validateValue(value); err != nil {
		return err
	}
	slot.fields[field] = value
	return nil
}

func (arena *h1bArena) beginDefinitionAtSafePoint(ctx context.Context, object h1bObjectRef, captured int64) (h1bTransaction, error) {
	if err := h1bPoll(ctx); err != nil {
		return h1bTransaction{}, err
	}
	if arena.environmentFree == 0 {
		if err := arena.collect(ctx); err != nil {
			return h1bTransaction{}, err
		}
	}
	// Collection may have reclaimed or advanced any unrooted reference.
	if _, err := arena.resolveObject(object); err != nil {
		return h1bTransaction{}, err
	}
	return arena.beginDefinition(object, captured)
}

func (arena *h1bArena) beginDefinition(object h1bObjectRef, captured int64) (h1bTransaction, error) {
	if arena.closed {
		return h1bTransaction{}, ErrH1bClosed
	}
	if arena.transaction.id != 0 {
		return h1bTransaction{}, errH1bTransactionRun
	}
	objectSlot, err := arena.resolveObject(object)
	if err != nil {
		return h1bTransaction{}, err
	}
	if objectSlot.present || objectSlot.environment != 0 || arena.nextTransaction == ^uint32(0) {
		return h1bTransaction{}, errH1bTransaction
	}
	index := arena.environmentFree
	if index == 0 {
		return h1bTransaction{}, errH1bCapacity
	}
	slot := &arena.environments[index-1]
	if slot.live || slot.reserved || slot.retired || slot.generation == 0 {
		return h1bTransaction{}, errH1bInvalidRef
	}
	arena.environmentFree = slot.nextFree
	generation := slot.generation
	*slot = h1bEnvironmentSlot{cells: [h1bEnvironmentCells]int64{captured}, generation: generation, reserved: true}
	arena.nextTransaction++
	arena.transaction = h1bTransaction{
		id:          arena.nextTransaction,
		object:      object,
		environment: h1bEnvironmentRef(h1bPackReference(index, generation)),
	}
	return arena.transaction, nil
}

func (arena *h1bArena) commitDefinition(ctx context.Context, transaction h1bTransaction) error {
	if !arena.validTransaction(transaction) {
		return errH1bTransaction
	}
	if err := h1bPoll(ctx); err != nil {
		_ = arena.abortDefinition(transaction)
		return err
	}
	object, err := arena.resolveObject(transaction.object)
	if err != nil || object.present || object.environment != 0 {
		_ = arena.abortDefinition(transaction)
		return errH1bTransaction
	}
	index, generation, ok := h1bUnpackReference(uint32(transaction.environment))
	if !ok || int(index) > len(arena.environments) {
		_ = arena.abortDefinition(transaction)
		return errH1bTransaction
	}
	environment := &arena.environments[index-1]
	if !environment.reserved || environment.live || environment.retired || environment.generation != generation {
		_ = arena.abortDefinition(transaction)
		return errH1bTransaction
	}

	// All validation and cancellation precede this non-fallible tail. The
	// environment and payload become complete before presence is published.
	environment.reserved = false
	environment.live = true
	arena.statistics.EnvironmentAllocations++
	arena.statistics.LiveEnvironments++
	object.environment = transaction.environment
	arena.transaction = h1bTransaction{}
	object.present = true
	return nil
}

func (arena *h1bArena) abortDefinition(transaction h1bTransaction) error {
	if !arena.validTransaction(transaction) {
		return errH1bTransaction
	}
	index, generation, ok := h1bUnpackReference(uint32(transaction.environment))
	if ok && int(index) <= len(arena.environments) {
		slot := &arena.environments[index-1]
		if slot.reserved && !slot.live && slot.generation == generation {
			*slot = h1bEnvironmentSlot{generation: generation}
			arena.insertEnvironmentFree(index)
		}
	}
	arena.transaction = h1bTransaction{}
	return nil
}

func (arena *h1bArena) validTransaction(transaction h1bTransaction) bool {
	return transaction.id != 0 && transaction == arena.transaction
}

func (arena *h1bArena) resolveEnvironment(reference h1bEnvironmentRef) (*h1bEnvironmentSlot, error) {
	if arena.closed {
		return nil, ErrH1bClosed
	}
	index, generation, ok := h1bUnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.environments) {
		return nil, errH1bInvalidRef
	}
	slot := &arena.environments[index-1]
	if !slot.live || slot.reserved || slot.retired || slot.generation != generation {
		return nil, errH1bInvalidRef
	}
	return slot, nil
}

func (arena *h1bArena) addCaptured(reference h1bEnvironmentRef, cell uint8, delta int64) (int64, error) {
	slot, err := arena.resolveEnvironment(reference)
	if err != nil {
		return 0, err
	}
	if int(cell) >= len(slot.cells) {
		return 0, errH1bInvalidRef
	}
	slot.cells[cell] += delta
	return slot.cells[cell], nil
}

func (arena *h1bArena) hitPICArm(objectRef h1bObjectRef, arm h1bPICArm) (h1bEnvironmentRef, bool) {
	if objectRef != arm.object {
		return 0, false
	}
	object, err := arena.resolveObject(objectRef)
	if err != nil || !object.present || object.environment != arm.environment {
		return 0, false
	}
	if _, err := arena.resolveEnvironment(arm.environment); err != nil {
		return 0, false
	}
	return arm.environment, true
}

func (arena *h1bArena) collect(ctx context.Context) error {
	if arena.closed {
		return ErrH1bClosed
	}
	if arena.transaction.id != 0 {
		return errH1bTransactionRun
	}
	if err := h1bPoll(ctx); err != nil {
		return err
	}
	arena.clearMarks()
	work := h1bCollectionWork{RootSlots: arena.rootCount}
	for index := uint8(0); index < arena.rootCount; index++ {
		if arena.roots[index].kind != h1bValueNil {
			work.RootValues++
		}
		if err := arena.markValue(arena.roots[index]); err != nil {
			arena.clearMarks()
			return err
		}
	}
	for arena.markCount != 0 {
		arena.markCount--
		item := arena.markWork[arena.markCount]
		arena.markWork[arena.markCount] = h1bMarkItem{}
		switch item.kind {
		case h1bMarkObject:
			work.MarkedObjects++
			slot := &arena.objects[item.index-1]
			if !slot.live || slot.generation != item.generation {
				arena.clearMarks()
				return errH1bInvalidRef
			}
			if slot.present {
				if slot.environment == 0 || arena.markEnvironment(slot.environment) != nil {
					arena.clearMarks()
					return errH1bInvalidRef
				}
			} else if slot.environment != 0 {
				arena.clearMarks()
				return errH1bInvalidRef
			}
			for field := range slot.fields {
				if err := arena.markValue(slot.fields[field]); err != nil {
					arena.clearMarks()
					return err
				}
			}
		case h1bMarkEnvironment:
			work.MarkedEnvironments++
			slot := &arena.environments[item.index-1]
			if !slot.live || slot.reserved || slot.generation != item.generation {
				arena.clearMarks()
				return errH1bInvalidRef
			}
		default:
			arena.clearMarks()
			return errH1bInvalidRef
		}
	}
	if err := h1bPoll(ctx); err != nil {
		arena.clearMarks()
		return err
	}

	// Every fallible operation precedes this deterministic sweep.
	for index := range arena.objects {
		if arena.objects[index].live && !arena.objects[index].marked {
			arena.reclaimObject(uint8(index + 1))
			work.ReclaimedObjects++
		}
	}
	for index := range arena.environments {
		if arena.environments[index].live && !arena.environments[index].marked {
			arena.reclaimEnvironment(uint8(index + 1))
			work.ReclaimedEnvironments++
		}
	}
	markWork := work.markWork()
	arena.statistics.Collections++
	arena.statistics.TotalMarkWork += uint64(markWork)
	arena.statistics.LastMarkWork = markWork
	if markWork > arena.statistics.MarkHighWater {
		arena.statistics.MarkHighWater = markWork
	}
	arena.lastWork = work
	arena.clearMarks()
	return nil
}

func (arena *h1bArena) markValue(value h1bValue) error {
	switch value.kind {
	case h1bValueNil:
		if value != (h1bValue{}) {
			return errH1bInvalidRef
		}
		return nil
	case h1bValueObject:
		return arena.markObject(h1bObjectRef(value.reference))
	case h1bValueEnvironment:
		return arena.markEnvironment(h1bEnvironmentRef(value.reference))
	default:
		return errH1bInvalidRef
	}
}

func (arena *h1bArena) markObject(reference h1bObjectRef) error {
	index, generation, ok := h1bUnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.objects) {
		return errH1bInvalidRef
	}
	slot := &arena.objects[index-1]
	if !slot.live || slot.retired || slot.generation != generation {
		return errH1bInvalidRef
	}
	if slot.marked {
		return nil
	}
	if int(arena.markCount) == len(arena.markWork) {
		return errH1bCapacity
	}
	slot.marked = true
	arena.markWork[arena.markCount] = h1bMarkItem{generation: generation, index: index, kind: h1bMarkObject}
	arena.markCount++
	return nil
}

func (arena *h1bArena) markEnvironment(reference h1bEnvironmentRef) error {
	index, generation, ok := h1bUnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.environments) {
		return errH1bInvalidRef
	}
	slot := &arena.environments[index-1]
	if !slot.live || slot.reserved || slot.retired || slot.generation != generation {
		return errH1bInvalidRef
	}
	if slot.marked {
		return nil
	}
	if int(arena.markCount) == len(arena.markWork) {
		return errH1bCapacity
	}
	slot.marked = true
	arena.markWork[arena.markCount] = h1bMarkItem{generation: generation, index: index, kind: h1bMarkEnvironment}
	arena.markCount++
	return nil
}

func (arena *h1bArena) clearMarks() {
	for arena.markCount != 0 {
		arena.markCount--
		arena.markWork[arena.markCount] = h1bMarkItem{}
	}
	for index := range arena.objects {
		arena.objects[index].marked = false
	}
	for index := range arena.environments {
		arena.environments[index].marked = false
	}
}

func (arena *h1bArena) reclaimObject(index uint8) {
	slot := &arena.objects[index-1]
	next, retired := h1bNextGeneration(slot.generation)
	*slot = h1bObjectSlot{generation: next, retired: retired}
	if !retired {
		arena.insertObjectFree(index)
	}
	arena.statistics.ObjectReclaims++
	arena.statistics.LiveObjects--
}

func (arena *h1bArena) reclaimEnvironment(index uint8) {
	slot := &arena.environments[index-1]
	next, retired := h1bNextGeneration(slot.generation)
	*slot = h1bEnvironmentSlot{generation: next, retired: retired}
	if !retired {
		arena.insertEnvironmentFree(index)
	}
	arena.statistics.EnvironmentReclaims++
	arena.statistics.LiveEnvironments--
}

func h1bNextGeneration(generation uint32) (uint32, bool) {
	if generation == 0 || generation >= h1bMaximumGeneration {
		return h1bMaximumGeneration, true
	}
	return generation + 1, false
}

func (arena *h1bArena) insertObjectFree(index uint8) {
	if arena.objectFree == 0 || index < arena.objectFree {
		arena.objects[index-1].nextFree = arena.objectFree
		arena.objectFree = index
		return
	}
	current := arena.objectFree
	for arena.objects[current-1].nextFree != 0 && arena.objects[current-1].nextFree < index {
		current = arena.objects[current-1].nextFree
	}
	arena.objects[index-1].nextFree = arena.objects[current-1].nextFree
	arena.objects[current-1].nextFree = index
}

func (arena *h1bArena) insertEnvironmentFree(index uint8) {
	if arena.environmentFree == 0 || index < arena.environmentFree {
		arena.environments[index-1].nextFree = arena.environmentFree
		arena.environmentFree = index
		return
	}
	current := arena.environmentFree
	for arena.environments[current-1].nextFree != 0 && arena.environments[current-1].nextFree < index {
		current = arena.environments[current-1].nextFree
	}
	arena.environments[index-1].nextFree = arena.environments[current-1].nextFree
	arena.environments[current-1].nextFree = index
}

func h1bPoll(ctx context.Context) error {
	if ctx == nil {
		return context.Canceled
	}
	return ctx.Err()
}

// H1bRuntime owns one independent canonical arena, explicit root bank, and
// weak call-site cache for the sealed H1b product.
type H1bRuntime struct {
	plan  *checkedH1bPlan
	arena h1bArena
	pic   h1bPIC

	collections      [5]h1bCollectionWork
	collectionCount  uint8
	lastReceiver     h1bObjectRef
	lastEnvironment  h1bEnvironmentRef
	lastSingletonArm h1bPICArm

	forceEnvironmentPressure bool
	busy                     bool
	closed                   bool
	poisoned                 bool
}

// NewRuntime binds one mutable H1b owner to the immutable checked plan.
func (program H1bProgram) NewRuntime() (*H1bRuntime, error) {
	if err := validateH1bRuntimePlan(program.checked); err != nil {
		return nil, err
	}
	return &H1bRuntime{plan: program.checked, arena: newH1bArena()}, nil
}

// Run is a convenience for a fresh owner.
func (program H1bProgram) Run(ctx context.Context, limits H1bLimits) (H1bResult, error) {
	runtime, err := program.NewRuntime()
	if err != nil {
		return H1bResult{}, err
	}
	result, runErr := runtime.Run(ctx, limits)
	closeErr := runtime.Close()
	if runErr != nil {
		return H1bResult{}, runErr
	}
	if closeErr != nil {
		return H1bResult{}, closeErr
	}
	return result, nil
}

// Run executes only the immutable H1b plan and returns detached scalars.
func (runtime *H1bRuntime) Run(ctx context.Context, limits H1bLimits) (H1bResult, error) {
	if runtime == nil || runtime.closed {
		return H1bResult{}, ErrH1bClosed
	}
	if runtime.busy {
		return H1bResult{}, ErrH1bBusy
	}
	if runtime.poisoned {
		return H1bResult{}, ErrH1bPoisoned
	}
	want := H1bProofLimits()
	if limits.Objects < want.Objects || limits.Environments < want.Environments || limits.Collections < want.Collections {
		return H1bResult{}, ErrH1bLimit
	}
	if err := h1bPoll(ctx); err != nil {
		return H1bResult{}, err
	}
	if runtime.arena.rootCount != 0 || runtime.arena.transaction.id != 0 ||
		runtime.arena.statistics.LiveObjects != 0 || runtime.arena.statistics.LiveEnvironments != 0 {
		return H1bResult{}, errH1bInvalidRef
	}

	runtime.busy = true
	runtime.arena.resetObservations()
	runtime.pic = h1bPIC{}
	runtime.collections = [5]h1bCollectionWork{}
	runtime.collectionCount = 0
	runtime.lastReceiver = 0
	runtime.lastEnvironment = 0
	runtime.lastSingletonArm = h1bPICArm{}

	result, err := runtime.execute(ctx)
	if err != nil {
		if runtime.arena.transaction.id != 0 {
			_ = runtime.arena.abortDefinition(runtime.arena.transaction)
		}
		_ = runtime.arena.popRoots(0)
		runtime.busy = false
		runtime.poisoned = true
		return H1bResult{}, err
	}
	runtime.busy = false
	return result, nil
}

func (runtime *H1bRuntime) execute(ctx context.Context) (H1bResult, error) {
	plan := runtime.plan
	if runtime.forceEnvironmentPressure {
		if err := runtime.primeEnvironmentPressure(ctx); err != nil {
			return H1bResult{}, err
		}
	}

	top := runtime.arena.rootMarker()
	receiverRoot, err := runtime.arena.reserveRoot()
	if err != nil {
		return H1bResult{}, err
	}
	peerRoot, err := runtime.arena.reserveRoot()
	if err != nil {
		return H1bResult{}, err
	}
	receiver, err := runtime.arena.allocateObjectAtSafePoint(ctx, plan.class.dispatch, plan.class.shape)
	if err != nil {
		return H1bResult{}, err
	}
	if err := runtime.arena.setRoot(receiverRoot, h1bObjectValue(receiver)); err != nil {
		return H1bResult{}, err
	}
	peer, err := runtime.arena.allocateObjectAtSafePoint(ctx, plan.class.dispatch, plan.class.shape)
	if err != nil {
		return H1bResult{}, err
	}
	if err := runtime.arena.setRoot(peerRoot, h1bObjectValue(peer)); err != nil {
		return H1bResult{}, err
	}

	result := H1bResult{}
	if result.Before, err = runtime.readLabel(receiver); err != nil {
		return H1bResult{}, err
	}
	if result.Warm, err = runtime.readLabel(receiver); err != nil {
		return H1bResult{}, err
	}
	if result.PeerBefore, err = runtime.readLabel(peer); err != nil {
		return H1bResult{}, err
	}

	// The caller-owned destination remains reserved but no longer roots the
	// argument. At environment pressure the receiver is therefore protected
	// only by install_h1b's explicit parameter root.
	helper := runtime.arena.rootMarker()
	helperReceiverRoot, err := runtime.arena.reserveRoot()
	if err != nil {
		return H1bResult{}, err
	}
	if err := runtime.arena.setRoot(helperReceiverRoot, h1bObjectValue(receiver)); err != nil {
		return H1bResult{}, err
	}
	if err := runtime.arena.clearRoot(receiverRoot); err != nil {
		return H1bResult{}, err
	}
	transaction, err := runtime.arena.beginDefinitionAtSafePoint(ctx, receiver, plan.capture.initial)
	if err != nil {
		return H1bResult{}, err
	}
	runtime.recordCollection()
	if err := runtime.arena.commitDefinition(ctx, transaction); err != nil {
		return H1bResult{}, err
	}
	environment := transaction.environment
	if err := runtime.collect(ctx); err != nil {
		return H1bResult{}, err
	}
	if _, err := runtime.arena.addCaptured(environment, plan.capture.cell, plan.capture.helperIncrement); err != nil {
		return H1bResult{}, err
	}
	// The callee hands the reference to its caller-owned rooted destination
	// before releasing its receiver root. The published receiver sidecar, not a
	// redundant environment root, protects the capture across GC.start.
	if err := runtime.arena.setRoot(receiverRoot, h1bObjectValue(receiver)); err != nil {
		return H1bResult{}, err
	}
	if err := runtime.arena.popRoots(helper); err != nil {
		return H1bResult{}, err
	}

	if result.First, err = runtime.readLabel(receiver); err != nil {
		return H1bResult{}, err
	}
	if result.Second, err = runtime.readLabel(receiver); err != nil {
		return H1bResult{}, err
	}
	if result.PeerAfter, err = runtime.readLabel(peer); err != nil {
		return H1bResult{}, err
	}
	runtime.lastReceiver = receiver
	runtime.lastEnvironment = environment
	runtime.lastSingletonArm = runtime.pic.singleton
	if err := runtime.arena.clearRoot(receiverRoot); err != nil {
		return H1bResult{}, err
	}
	if err := runtime.collect(ctx); err != nil {
		return H1bResult{}, err
	}
	if result.PeerAfterReceiverCollection, err = runtime.readLabel(peer); err != nil {
		return H1bResult{}, err
	}
	if err := runtime.arena.clearRoot(peerRoot); err != nil {
		return H1bResult{}, err
	}
	if err := runtime.collect(ctx); err != nil {
		return H1bResult{}, err
	}
	if err := runtime.arena.popRoots(top); err != nil {
		return H1bResult{}, err
	}
	result.Stats = runtime.arena.statistics
	return result, nil
}

func (runtime *H1bRuntime) readLabel(objectRef h1bObjectRef) (int64, error) {
	object, err := runtime.arena.resolveObject(objectRef)
	if err != nil {
		return 0, err
	}
	if runtime.pic.singletonValid {
		if environment, hit := runtime.arena.hitPICArm(objectRef, runtime.pic.singleton); hit {
			runtime.pic.singletonHits++
			return runtime.arena.addCaptured(environment, runtime.plan.capture.cell, runtime.plan.capture.bodyIncrement)
		}
		runtime.pic.singletonMisses++
	}
	if runtime.pic.ordinaryValid {
		if !object.present && object.environment == 0 && object.dispatch == runtime.pic.ordinaryDispatch {
			runtime.pic.ordinaryHits++
			return runtime.plan.class.ordinaryValue, nil
		}
		runtime.pic.ordinaryMisses++
	}
	if object.present {
		if object.environment == 0 {
			return 0, errH1bInvalidRef
		}
		if _, err := runtime.arena.resolveEnvironment(object.environment); err != nil {
			return 0, err
		}
		runtime.pic.singleton = h1bPICArm{object: objectRef, environment: object.environment}
		runtime.pic.singletonValid = true
		return runtime.arena.addCaptured(object.environment, runtime.plan.capture.cell, runtime.plan.capture.bodyIncrement)
	}
	if object.environment != 0 {
		return 0, errH1bInvalidRef
	}
	runtime.pic.ordinaryDispatch = object.dispatch
	runtime.pic.ordinaryValid = true
	return runtime.plan.class.ordinaryValue, nil
}

func (runtime *H1bRuntime) collect(ctx context.Context) error {
	before := runtime.arena.statistics.Collections
	if err := runtime.arena.collect(ctx); err != nil {
		return err
	}
	if runtime.arena.statistics.Collections != before+1 {
		return errH1bInvalidRef
	}
	runtime.recordCollection()
	return nil
}

func (runtime *H1bRuntime) recordCollection() {
	if runtime.arena.statistics.Collections == 0 ||
		runtime.collectionCount >= uint8(len(runtime.collections)) {
		return
	}
	if uint64(runtime.collectionCount) >= runtime.arena.statistics.Collections {
		return
	}
	runtime.collections[runtime.collectionCount] = runtime.arena.lastWork
	runtime.collectionCount++
}

func (runtime *H1bRuntime) primeEnvironmentPressure(ctx context.Context) error {
	for range h1bEnvironmentCapacity {
		object, err := runtime.arena.allocateObjectAtSafePoint(ctx, runtime.plan.class.dispatch, runtime.plan.class.shape)
		if err != nil {
			return err
		}
		transaction, err := runtime.arena.beginDefinition(object, runtime.plan.capture.initial)
		if err != nil {
			return err
		}
		if err := runtime.arena.commitDefinition(ctx, transaction); err != nil {
			return err
		}
	}
	if runtime.arena.environmentFree != 0 {
		return errH1bInvalidRef
	}
	return nil
}

// Close releases the owner. It is idempotent and never runs guest cleanup.
func (runtime *H1bRuntime) Close() error {
	if runtime == nil || runtime.closed {
		return nil
	}
	if runtime.busy || runtime.arena.transaction.id != 0 {
		return ErrH1bBusy
	}
	*runtime = H1bRuntime{closed: true, arena: h1bArena{closed: true}}
	return nil
}

func validateH1bRuntimePlan(plan *checkedH1bPlan) error {
	if plan == nil || plan.class.dispatch != dispatchClassID(1) || plan.class.shape != shapeID(1) || plan.class.ordinaryValue != 7 ||
		plan.call.selector != selectorID(1) || plan.call.singletonTarget != h1SidecarTargetID(1) || plan.call.visibility != lookupVisibilityPublic ||
		plan.capture.cell != 0 || plan.capture.initial != 40 || plan.capture.bodyIncrement != 1 || plan.capture.helperIncrement != 2 ||
		plan.roots != (h1bRootPlan{topSlots: 2, installObjectRoots: 1, maximumSlots: 3}) ||
		plan.capacity.objects != h1bObjectCapacity || plan.capacity.environments != h1bEnvironmentCapacity ||
		plan.capacity.roots != h1bRootCapacity || plan.capacity.markWork != h1bMarkCapacity ||
		plan.capacity.objectFields != h1bObjectFields || plan.capacity.environmentCells != h1bEnvironmentCells ||
		plan.capacity.referenceIndexBits != h1bReferenceIndexBits || plan.capacity.maximumGeneration != h1bMaximumGeneration {
		return errH1bPlan
	}
	return nil
}
