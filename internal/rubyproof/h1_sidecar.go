package rubyproof

import (
	"context"
	"errors"
)

// H1-prime is an isolated comparison candidate. A singleton row is an
// unobservable sidecar of an object slot and has exactly the object's lifetime.
const (
	h1SidecarObjectCapacity      = 4
	h1SidecarEnvironmentCapacity = 2
	h1SidecarRootCapacity        = 8
	h1SidecarObjectFields        = 2
	h1SidecarEnvironmentCells    = 1
	h1SidecarMarkCapacity        = h1SidecarObjectCapacity + h1SidecarEnvironmentCapacity
	h1SidecarSelector            = selectorID(1)
	h1SidecarTarget              = h1SidecarTargetID(1)

	h1SidecarReferenceIndexBits = 8
	h1SidecarReferenceIndexMask = 1<<h1SidecarReferenceIndexBits - 1
	h1SidecarMaximumGeneration  = 1<<(32-h1SidecarReferenceIndexBits) - 1
)

var (
	errH1SidecarClosed         = errors.New("ruby: H1-prime arena is closed")
	errH1SidecarCapacity       = errors.New("ruby: H1-prime arena capacity exceeded")
	errH1SidecarRootOrder      = errors.New("ruby: H1-prime arena root order mismatch")
	errH1SidecarInvalidRef     = errors.New("ruby: invalid H1-prime arena reference")
	errH1SidecarTransaction    = errors.New("ruby: invalid H1-prime singleton transaction")
	errH1SidecarTransactionRun = errors.New("ruby: H1-prime singleton transaction is active")
	errH1SidecarNoMethod       = errors.New("ruby: H1-prime singleton method is unavailable")
)

type h1SidecarObjectRef uint32
type h1SidecarEnvironmentRef uint32
type h1SidecarTargetID uint16

type h1SidecarValueKind uint8

const (
	h1SidecarValueNil h1SidecarValueKind = iota
	h1SidecarValueInteger
	h1SidecarValueObject
	h1SidecarValueEnvironment
)

type h1SidecarValue struct {
	payload uint64
	kind    h1SidecarValueKind
}

func h1SidecarIntegerValue(value int64) h1SidecarValue {
	return h1SidecarValue{kind: h1SidecarValueInteger, payload: uint64(value)}
}

func h1SidecarObjectValue(reference h1SidecarObjectRef) h1SidecarValue {
	return h1SidecarValue{kind: h1SidecarValueObject, payload: uint64(reference)}
}

func h1SidecarEnvironmentValue(reference h1SidecarEnvironmentRef) h1SidecarValue {
	return h1SidecarValue{kind: h1SidecarValueEnvironment, payload: uint64(reference)}
}

func (value h1SidecarValue) integerValue() int64 { return int64(value.payload) }
func (value h1SidecarValue) objectRef() h1SidecarObjectRef {
	return h1SidecarObjectRef(value.payload)
}
func (value h1SidecarValue) environmentRef() h1SidecarEnvironmentRef {
	return h1SidecarEnvironmentRef(value.payload)
}

type h1SidecarRow struct {
	environment h1SidecarEnvironmentRef
}

type h1SidecarObjectSlot struct {
	generation uint32
	nextFree   uint8
	live       bool
	marked     bool
	retired    bool

	dispatch dispatchClassID
	shape    shapeID
	value    int64
	trace    int64
	fields   [h1SidecarObjectFields]h1SidecarValue

	sidecar        h1SidecarRow
	sidecarPresent bool
}

type h1SidecarEnvironmentSlot struct {
	generation uint32
	nextFree   uint8
	live       bool
	reserved   bool
	marked     bool
	retired    bool
	parent     h1SidecarEnvironmentRef
	cells      [h1SidecarEnvironmentCells]h1SidecarValue
}

type h1SidecarMarkKind uint8

const (
	h1SidecarMarkObject h1SidecarMarkKind = iota + 1
	h1SidecarMarkEnvironment
)

type h1SidecarMarkItem struct {
	kind       h1SidecarMarkKind
	index      uint8
	generation uint32
}

// The complete unpublished row lives in the transaction. The object contains
// no partial row before commit publishes sidecarPresent.
type h1SidecarSingletonTransaction struct {
	id          uint32
	object      h1SidecarObjectRef
	environment h1SidecarEnvironmentRef
	row         h1SidecarRow
}

type h1SidecarCallData struct {
	object      h1SidecarObjectRef
	environment h1SidecarEnvironmentRef
	fallback    dispatchClassID
}

// h1SidecarPICArm is weak. Object generation, not a separately allocated row,
// is its lifetime identity.
type h1SidecarPICArm struct {
	object      h1SidecarObjectRef
	environment h1SidecarEnvironmentRef
}

type h1SidecarStats struct {
	ObjectAllocations      uint64
	EnvironmentAllocations uint64
	ObjectReclaims         uint64
	EnvironmentReclaims    uint64
	Collections            uint64
	LiveObjects            uint8
	LiveEnvironments       uint8
	RetiredObjects         uint8
	RetiredEnvironments    uint8
	Roots                  uint8
	MarkHighWater          uint8
	LastMarkWork           uint8
	TotalMarkWork          uint64
}

type h1SidecarCollectionWork struct {
	RootValues         uint8
	MarkedObjects      uint8
	MarkedEnvironments uint8
	ObjectScans        uint8
	EnvironmentScans   uint8
	ReclaimedObjects   uint8
	ReclaimedEnvs      uint8
}

type h1SidecarArena struct {
	objects      [h1SidecarObjectCapacity]h1SidecarObjectSlot
	environments [h1SidecarEnvironmentCapacity]h1SidecarEnvironmentSlot

	objectFree      uint8
	environmentFree uint8
	roots           [h1SidecarRootCapacity]h1SidecarValue
	rootCount       uint8
	markWork        [h1SidecarMarkCapacity]h1SidecarMarkItem
	markCount       uint8

	transaction     h1SidecarSingletonTransaction
	nextTransaction uint32
	closed          bool
	statistics      h1SidecarStats
	lastWork        h1SidecarCollectionWork
}

func newH1SidecarArena() *h1SidecarArena {
	arena := &h1SidecarArena{objectFree: 1, environmentFree: 1}
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

func h1SidecarPackReference(index uint8, generation uint32) uint32 {
	if index == 0 || generation == 0 || generation > h1SidecarMaximumGeneration {
		return 0
	}
	return generation<<h1SidecarReferenceIndexBits | uint32(index)
}

func h1SidecarUnpackReference(reference uint32) (uint8, uint32, bool) {
	index := uint8(reference & h1SidecarReferenceIndexMask)
	generation := reference >> h1SidecarReferenceIndexBits
	return index, generation, index != 0 && generation != 0
}

func (arena *h1SidecarArena) allocateObject(dispatch dispatchClassID, shape shapeID, value int64) (h1SidecarObjectRef, error) {
	if arena == nil || arena.closed {
		return 0, errH1SidecarClosed
	}
	index := arena.objectFree
	if index == 0 {
		return 0, errH1SidecarCapacity
	}
	slot := &arena.objects[index-1]
	if slot.live || slot.retired || slot.generation == 0 || dispatch == 0 || shape == 0 {
		return 0, errH1SidecarInvalidRef
	}
	arena.objectFree = slot.nextFree
	generation := slot.generation
	*slot = h1SidecarObjectSlot{generation: generation, live: true, dispatch: dispatch, shape: shape, value: value}
	arena.statistics.ObjectAllocations++
	arena.statistics.LiveObjects++
	return h1SidecarObjectRef(h1SidecarPackReference(index, generation)), nil
}

func (arena *h1SidecarArena) resolveObject(reference h1SidecarObjectRef) (*h1SidecarObjectSlot, error) {
	if arena == nil || arena.closed {
		return nil, errH1SidecarClosed
	}
	index, generation, ok := h1SidecarUnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.objects) {
		return nil, errH1SidecarInvalidRef
	}
	slot := &arena.objects[index-1]
	if !slot.live || slot.retired || slot.generation != generation {
		return nil, errH1SidecarInvalidRef
	}
	return slot, nil
}

func (arena *h1SidecarArena) rootMarker() uint8 {
	if arena == nil {
		return 0
	}
	return arena.rootCount
}

func (arena *h1SidecarArena) pushRoot(value h1SidecarValue) error {
	if err := arena.validateValue(value); err != nil {
		return err
	}
	if int(arena.rootCount) == len(arena.roots) {
		return errH1SidecarCapacity
	}
	arena.roots[arena.rootCount] = value
	arena.rootCount++
	arena.statistics.Roots = arena.rootCount
	return nil
}

func (arena *h1SidecarArena) popRoots(marker uint8) error {
	if arena == nil || arena.closed {
		return errH1SidecarClosed
	}
	if marker > arena.rootCount {
		return errH1SidecarRootOrder
	}
	for arena.rootCount > marker {
		arena.rootCount--
		arena.roots[arena.rootCount] = h1SidecarValue{}
	}
	arena.statistics.Roots = arena.rootCount
	return nil
}

func (arena *h1SidecarArena) beginSingletonDefinition(
	object h1SidecarObjectRef,
	selector selectorID,
	target h1SidecarTargetID,
	visibility lookupVisibility,
	captured h1SidecarValue,
	parent h1SidecarEnvironmentRef,
) (h1SidecarSingletonTransaction, error) {
	if arena == nil || arena.closed {
		return h1SidecarSingletonTransaction{}, errH1SidecarClosed
	}
	if arena.transaction.id != 0 {
		return h1SidecarSingletonTransaction{}, errH1SidecarTransactionRun
	}
	objectSlot, err := arena.resolveObject(object)
	if err != nil {
		return h1SidecarSingletonTransaction{}, err
	}
	if objectSlot.sidecarPresent || objectSlot.sidecar != (h1SidecarRow{}) || selector != h1SidecarSelector ||
		target != h1SidecarTarget || visibility != lookupVisibilityPublic || arena.nextTransaction == ^uint32(0) {
		return h1SidecarSingletonTransaction{}, errH1SidecarTransaction
	}
	if err := arena.validateValue(captured); err != nil {
		return h1SidecarSingletonTransaction{}, err
	}
	if parent != 0 {
		if _, err := arena.resolveEnvironment(parent); err != nil {
			return h1SidecarSingletonTransaction{}, err
		}
	}
	environment, err := arena.reserveEnvironment(captured, parent)
	if err != nil {
		return h1SidecarSingletonTransaction{}, err
	}
	row := h1SidecarRow{environment: environment}
	arena.nextTransaction++
	arena.transaction = h1SidecarSingletonTransaction{
		id: arena.nextTransaction, object: object, environment: environment, row: row,
	}
	return arena.transaction, nil
}

func (arena *h1SidecarArena) commitSingletonDefinition(transaction h1SidecarSingletonTransaction, canceled bool) error {
	if arena == nil || arena.closed {
		return errH1SidecarClosed
	}
	if !arena.validTransaction(transaction) {
		return errH1SidecarTransaction
	}
	if canceled {
		arena.abortSingletonDefinition(transaction)
		return context.Canceled
	}
	object, err := arena.resolveObject(transaction.object)
	if err != nil || object.sidecarPresent || object.sidecar != (h1SidecarRow{}) {
		arena.abortSingletonDefinition(transaction)
		return errH1SidecarTransaction
	}
	environmentIndex, environmentGeneration, ok := h1SidecarUnpackReference(uint32(transaction.environment))
	if !ok || int(environmentIndex) > len(arena.environments) {
		arena.abortSingletonDefinition(transaction)
		return errH1SidecarTransaction
	}
	environment := &arena.environments[environmentIndex-1]
	if !environment.reserved || environment.live || environment.retired || environment.generation != environmentGeneration ||
		transaction.row.environment != transaction.environment {
		arena.abortSingletonDefinition(transaction)
		return errH1SidecarTransaction
	}

	// Validation is complete. Publish the hidden row's presence only after its
	// environment and payload are fully committed; nothing fallible follows.
	environment.reserved, environment.live = false, true
	arena.statistics.EnvironmentAllocations++
	arena.statistics.LiveEnvironments++
	object.sidecar = transaction.row
	arena.transaction = h1SidecarSingletonTransaction{}
	object.sidecarPresent = true
	return nil
}

func (arena *h1SidecarArena) abortSingletonDefinition(transaction h1SidecarSingletonTransaction) error {
	if arena == nil || arena.closed {
		return errH1SidecarClosed
	}
	if !arena.validTransaction(transaction) {
		return errH1SidecarTransaction
	}
	arena.releaseReservedEnvironment(transaction.environment)
	arena.transaction = h1SidecarSingletonTransaction{}
	return nil
}

func (arena *h1SidecarArena) validTransaction(transaction h1SidecarSingletonTransaction) bool {
	return transaction.id != 0 && transaction == arena.transaction
}

func (arena *h1SidecarArena) reserveEnvironment(captured h1SidecarValue, parent h1SidecarEnvironmentRef) (h1SidecarEnvironmentRef, error) {
	index := arena.environmentFree
	if index == 0 {
		return 0, errH1SidecarCapacity
	}
	slot := &arena.environments[index-1]
	if slot.live || slot.reserved || slot.retired || slot.generation == 0 {
		return 0, errH1SidecarInvalidRef
	}
	arena.environmentFree = slot.nextFree
	generation := slot.generation
	*slot = h1SidecarEnvironmentSlot{
		generation: generation, reserved: true, parent: parent,
		cells: [h1SidecarEnvironmentCells]h1SidecarValue{captured},
	}
	return h1SidecarEnvironmentRef(h1SidecarPackReference(index, generation)), nil
}

func (arena *h1SidecarArena) releaseReservedEnvironment(reference h1SidecarEnvironmentRef) {
	index, generation, ok := h1SidecarUnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.environments) {
		return
	}
	slot := &arena.environments[index-1]
	if !slot.reserved || slot.live || slot.generation != generation {
		return
	}
	*slot = h1SidecarEnvironmentSlot{generation: generation}
	arena.insertEnvironmentFree(index)
}

func (arena *h1SidecarArena) resolveCallData(objectRef h1SidecarObjectRef, selector selectorID) (h1SidecarCallData, error) {
	object, err := arena.resolveObject(objectRef)
	if err != nil {
		return h1SidecarCallData{}, err
	}
	if !object.sidecarPresent {
		if object.sidecar != (h1SidecarRow{}) {
			return h1SidecarCallData{}, errH1SidecarInvalidRef
		}
		return h1SidecarCallData{}, errH1SidecarNoMethod
	}
	if selector != h1SidecarSelector || object.sidecar.environment == 0 {
		return h1SidecarCallData{}, errH1SidecarNoMethod
	}
	if _, err := arena.resolveEnvironment(object.sidecar.environment); err != nil {
		return h1SidecarCallData{}, err
	}
	return h1SidecarCallData{
		object: objectRef, environment: object.sidecar.environment, fallback: object.dispatch,
	}, nil
}

func (arena *h1SidecarArena) admitPICArm(data h1SidecarCallData) (h1SidecarPICArm, error) {
	current, err := arena.resolveCallData(data.object, h1SidecarSelector)
	if err != nil || current != data {
		return h1SidecarPICArm{}, errH1SidecarInvalidRef
	}
	return h1SidecarPICArm{object: data.object, environment: data.environment}, nil
}

func (arena *h1SidecarArena) hitPICArm(objectRef h1SidecarObjectRef, selector selectorID, arm h1SidecarPICArm) (h1SidecarCallData, bool) {
	if objectRef != arm.object {
		return h1SidecarCallData{}, false
	}
	current, err := arena.resolveCallData(objectRef, selector)
	if err != nil {
		return h1SidecarCallData{}, false
	}
	return current, current.object == arm.object && current.environment == arm.environment
}

func (arena *h1SidecarArena) resolveEnvironment(reference h1SidecarEnvironmentRef) (*h1SidecarEnvironmentSlot, error) {
	if arena == nil || arena.closed {
		return nil, errH1SidecarClosed
	}
	index, generation, ok := h1SidecarUnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.environments) {
		return nil, errH1SidecarInvalidRef
	}
	slot := &arena.environments[index-1]
	if !slot.live || slot.reserved || slot.retired || slot.generation != generation {
		return nil, errH1SidecarInvalidRef
	}
	return slot, nil
}

func (arena *h1SidecarArena) validateValue(value h1SidecarValue) error {
	if arena == nil || arena.closed {
		return errH1SidecarClosed
	}
	switch value.kind {
	case h1SidecarValueNil:
		if value != (h1SidecarValue{}) {
			return errH1SidecarInvalidRef
		}
	case h1SidecarValueInteger:
	case h1SidecarValueObject:
		if value.payload == 0 || value.payload > uint64(^uint32(0)) {
			return errH1SidecarInvalidRef
		}
		_, err := arena.resolveObject(value.objectRef())
		return err
	case h1SidecarValueEnvironment:
		if value.payload == 0 || value.payload > uint64(^uint32(0)) {
			return errH1SidecarInvalidRef
		}
		_, err := arena.resolveEnvironment(value.environmentRef())
		return err
	default:
		return errH1SidecarInvalidRef
	}
	return nil
}

func (arena *h1SidecarArena) loadObjectField(reference h1SidecarObjectRef, field uint8) (h1SidecarValue, error) {
	slot, err := arena.resolveObject(reference)
	if err != nil {
		return h1SidecarValue{}, err
	}
	if int(field) >= len(slot.fields) {
		return h1SidecarValue{}, errH1SidecarInvalidRef
	}
	return slot.fields[field], nil
}

func (arena *h1SidecarArena) storeObjectField(reference h1SidecarObjectRef, field uint8, value h1SidecarValue) error {
	slot, err := arena.resolveObject(reference)
	if err != nil {
		return err
	}
	if int(field) >= len(slot.fields) {
		return errH1SidecarInvalidRef
	}
	if err := arena.validateValue(value); err != nil {
		return err
	}
	slot.fields[field] = value
	return nil
}

func (arena *h1SidecarArena) loadEnvironment(reference h1SidecarEnvironmentRef, cell uint8) (h1SidecarValue, error) {
	slot, err := arena.resolveEnvironment(reference)
	if err != nil {
		return h1SidecarValue{}, err
	}
	if int(cell) >= len(slot.cells) {
		return h1SidecarValue{}, errH1SidecarInvalidRef
	}
	return slot.cells[cell], nil
}

func (arena *h1SidecarArena) storeEnvironment(reference h1SidecarEnvironmentRef, cell uint8, value h1SidecarValue) error {
	slot, err := arena.resolveEnvironment(reference)
	if err != nil {
		return err
	}
	if int(cell) >= len(slot.cells) {
		return errH1SidecarInvalidRef
	}
	if err := arena.validateValue(value); err != nil {
		return err
	}
	slot.cells[cell] = value
	return nil
}

func (arena *h1SidecarArena) collect(canceled bool) error {
	if arena == nil || arena.closed {
		return errH1SidecarClosed
	}
	if arena.transaction.id != 0 {
		return errH1SidecarTransactionRun
	}
	arena.clearMarks()
	work := h1SidecarCollectionWork{RootValues: arena.rootCount}
	for index := uint8(0); index < arena.rootCount; index++ {
		if err := arena.markValue(arena.roots[index]); err != nil {
			arena.clearMarks()
			return err
		}
	}
	for arena.markCount != 0 {
		arena.markCount--
		item := arena.markWork[arena.markCount]
		arena.markWork[arena.markCount] = h1SidecarMarkItem{}
		switch item.kind {
		case h1SidecarMarkObject:
			work.MarkedObjects++
		case h1SidecarMarkEnvironment:
			work.MarkedEnvironments++
		}
		if err := arena.traceMarked(item); err != nil {
			arena.clearMarks()
			return err
		}
	}
	if canceled {
		arena.clearMarks()
		return context.Canceled
	}

	// Every possible failure precedes this fixed, deterministic sweep.
	for index := range arena.objects {
		work.ObjectScans++
		if arena.objects[index].live && !arena.objects[index].marked {
			arena.reclaimObject(uint8(index + 1))
			work.ReclaimedObjects++
		}
	}
	for index := range arena.environments {
		work.EnvironmentScans++
		if arena.environments[index].live && !arena.environments[index].marked {
			arena.reclaimEnvironment(uint8(index + 1))
			work.ReclaimedEnvs++
		}
	}
	markWork := work.MarkedObjects + work.MarkedEnvironments
	arena.clearMarks()
	arena.lastWork = work
	arena.statistics.LastMarkWork = markWork
	arena.statistics.TotalMarkWork += uint64(markWork)
	arena.statistics.Collections++
	return nil
}

func (arena *h1SidecarArena) traceMarked(item h1SidecarMarkItem) error {
	switch item.kind {
	case h1SidecarMarkObject:
		if item.index == 0 || int(item.index) > len(arena.objects) {
			return errH1SidecarInvalidRef
		}
		slot := &arena.objects[item.index-1]
		if !slot.live || !slot.marked || slot.generation != item.generation {
			return errH1SidecarInvalidRef
		}
		for _, field := range slot.fields {
			if err := arena.markValue(field); err != nil {
				return err
			}
		}
		if !slot.sidecarPresent {
			if slot.sidecar != (h1SidecarRow{}) {
				return errH1SidecarInvalidRef
			}
			return nil
		}
		if slot.sidecar.environment == 0 {
			return errH1SidecarInvalidRef
		}
		return arena.markEnvironment(slot.sidecar.environment)
	case h1SidecarMarkEnvironment:
		if item.index == 0 || int(item.index) > len(arena.environments) {
			return errH1SidecarInvalidRef
		}
		slot := &arena.environments[item.index-1]
		if !slot.live || !slot.marked || slot.generation != item.generation {
			return errH1SidecarInvalidRef
		}
		if slot.parent != 0 {
			if err := arena.markEnvironment(slot.parent); err != nil {
				return err
			}
		}
		for _, cell := range slot.cells {
			if err := arena.markValue(cell); err != nil {
				return err
			}
		}
		return nil
	default:
		return errH1SidecarInvalidRef
	}
}

func (arena *h1SidecarArena) markValue(value h1SidecarValue) error {
	switch value.kind {
	case h1SidecarValueNil:
		if value != (h1SidecarValue{}) {
			return errH1SidecarInvalidRef
		}
		return nil
	case h1SidecarValueInteger:
		return nil
	case h1SidecarValueObject:
		if value.payload == 0 || value.payload > uint64(^uint32(0)) {
			return errH1SidecarInvalidRef
		}
		return arena.markObject(value.objectRef())
	case h1SidecarValueEnvironment:
		if value.payload == 0 || value.payload > uint64(^uint32(0)) {
			return errH1SidecarInvalidRef
		}
		return arena.markEnvironment(value.environmentRef())
	default:
		return errH1SidecarInvalidRef
	}
}

func (arena *h1SidecarArena) markObject(reference h1SidecarObjectRef) error {
	index, generation, ok := h1SidecarUnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.objects) {
		return errH1SidecarInvalidRef
	}
	slot := &arena.objects[index-1]
	if !slot.live || slot.generation != generation {
		return errH1SidecarInvalidRef
	}
	if slot.marked {
		return nil
	}
	slot.marked = true
	return arena.markPush(h1SidecarMarkItem{kind: h1SidecarMarkObject, index: index, generation: generation})
}

func (arena *h1SidecarArena) markEnvironment(reference h1SidecarEnvironmentRef) error {
	index, generation, ok := h1SidecarUnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.environments) {
		return errH1SidecarInvalidRef
	}
	slot := &arena.environments[index-1]
	if !slot.live || slot.reserved || slot.generation != generation {
		return errH1SidecarInvalidRef
	}
	if slot.marked {
		return nil
	}
	slot.marked = true
	return arena.markPush(h1SidecarMarkItem{kind: h1SidecarMarkEnvironment, index: index, generation: generation})
}

func (arena *h1SidecarArena) markPush(item h1SidecarMarkItem) error {
	if int(arena.markCount) == len(arena.markWork) {
		return errH1SidecarCapacity
	}
	arena.markWork[arena.markCount] = item
	arena.markCount++
	if arena.markCount > arena.statistics.MarkHighWater {
		arena.statistics.MarkHighWater = arena.markCount
	}
	return nil
}

func (arena *h1SidecarArena) clearMarks() {
	for index := range arena.objects {
		arena.objects[index].marked = false
	}
	for index := range arena.environments {
		arena.environments[index].marked = false
	}
	arena.markWork = [h1SidecarMarkCapacity]h1SidecarMarkItem{}
	arena.markCount = 0
}

func (arena *h1SidecarArena) reclaimObject(index uint8) {
	slot := &arena.objects[index-1]
	generation, retired := h1SidecarNextGeneration(slot.generation)
	*slot = h1SidecarObjectSlot{generation: generation, retired: retired}
	if retired {
		arena.statistics.RetiredObjects++
	} else {
		arena.insertObjectFree(index)
	}
	arena.statistics.ObjectReclaims++
	arena.statistics.LiveObjects--
}

func (arena *h1SidecarArena) reclaimEnvironment(index uint8) {
	slot := &arena.environments[index-1]
	generation, retired := h1SidecarNextGeneration(slot.generation)
	*slot = h1SidecarEnvironmentSlot{generation: generation, retired: retired}
	if retired {
		arena.statistics.RetiredEnvironments++
	} else {
		arena.insertEnvironmentFree(index)
	}
	arena.statistics.EnvironmentReclaims++
	arena.statistics.LiveEnvironments--
}

func h1SidecarNextGeneration(generation uint32) (uint32, bool) {
	if generation == 0 || generation >= h1SidecarMaximumGeneration {
		return h1SidecarMaximumGeneration, true
	}
	return generation + 1, false
}

func (arena *h1SidecarArena) insertObjectFree(index uint8) {
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

func (arena *h1SidecarArena) insertEnvironmentFree(index uint8) {
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

func (arena *h1SidecarArena) stats() h1SidecarStats {
	if arena == nil {
		return h1SidecarStats{}
	}
	return arena.statistics
}

func (arena *h1SidecarArena) collectionWork() h1SidecarCollectionWork {
	if arena == nil {
		return h1SidecarCollectionWork{}
	}
	return arena.lastWork
}

func (arena *h1SidecarArena) close() error {
	if arena == nil || arena.closed {
		return nil
	}
	if arena.transaction.id != 0 {
		return errH1SidecarTransactionRun
	}
	*arena = h1SidecarArena{closed: true}
	return nil
}
