package rubyproof

import (
	"context"
	"errors"
)

// H1a is an isolated representation proof. These caps are deliberately tiny;
// they are not production Ruby heap limits.
const (
	h1ObjectCapacity      = 4
	h1EigenCapacity       = 2
	h1EnvironmentCapacity = 2
	h1RootCapacity        = 8
	h1EnvironmentCells    = 1
	h1ObjectFields        = 2
	h1MarkCapacity        = h1ObjectCapacity + h1EigenCapacity + h1EnvironmentCapacity

	h1ReferenceIndexBits = 8
	h1ReferenceIndexMask = 1<<h1ReferenceIndexBits - 1
	h1MaximumGeneration  = 1<<(32-h1ReferenceIndexBits) - 1
)

var (
	errH1ArenaClosed         = errors.New("ruby: H1 arena is closed")
	errH1ArenaCapacity       = errors.New("ruby: H1 arena capacity exceeded")
	errH1ArenaRootOrder      = errors.New("ruby: H1 arena root order mismatch")
	errH1ArenaInvalidRef     = errors.New("ruby: invalid H1 arena reference")
	errH1ArenaTransaction    = errors.New("ruby: invalid H1 singleton transaction")
	errH1ArenaTransactionRun = errors.New("ruby: H1 singleton transaction is active")
	errH1ArenaNoMethod       = errors.New("ruby: H1 singleton method is unavailable")
)

// References are packed, nonzero, owner-local index+generation scalars. A
// reference is meaningful only to the arena which issued it.
type h1ObjectRef uint32
type h1EigenRef uint32
type h1EnvironmentRef uint32
type h1TargetID uint16

type h1ValueKind uint8

const (
	h1ValueNil h1ValueKind = iota
	h1ValueInteger
	h1ValueObject
	h1ValueEnvironment
)

// h1Value is the closed value set needed by H1a roots and graph edges. It is
// intentionally separate from rubyValue so this proof inherits no Go pointer,
// slice, or map representation from the canonical evaluator.
type h1Value struct {
	payload uint64
	kind    h1ValueKind
}

func h1IntegerValue(value int64) h1Value {
	return h1Value{kind: h1ValueInteger, payload: uint64(value)}
}
func h1ObjectValue(reference h1ObjectRef) h1Value {
	return h1Value{kind: h1ValueObject, payload: uint64(reference)}
}
func h1EnvironmentValue(reference h1EnvironmentRef) h1Value {
	return h1Value{kind: h1ValueEnvironment, payload: uint64(reference)}
}

func (value h1Value) integerValue() int64              { return int64(value.payload) }
func (value h1Value) objectRef() h1ObjectRef           { return h1ObjectRef(value.payload) }
func (value h1Value) environmentRef() h1EnvironmentRef { return h1EnvironmentRef(value.payload) }

type h1ObjectSlot struct {
	generation uint32
	nextFree   uint8
	live       bool
	marked     bool
	retired    bool

	dispatch dispatchClassID
	shape    shapeID
	value    int64
	trace    int64
	eigen    h1EigenRef
	fields   [h1ObjectFields]h1Value
}

type h1EigenEntry struct {
	selector     selectorID
	target       h1TargetID
	environment  h1EnvironmentRef
	installation uint32
	visibility   lookupVisibility
	epoch        uint64
}

type h1EigenSlot struct {
	generation uint32
	nextFree   uint8
	live       bool
	reserved   bool
	marked     bool
	retired    bool
	fallback   dispatchClassID
	entry      h1EigenEntry
}

type h1EnvironmentSlot struct {
	generation uint32
	nextFree   uint8
	live       bool
	reserved   bool
	marked     bool
	retired    bool
	parent     h1EnvironmentRef
	cells      [h1EnvironmentCells]h1Value
}

type h1MarkKind uint8

const (
	h1MarkObject h1MarkKind = iota + 1
	h1MarkEigen
	h1MarkEnvironment
)

type h1MarkItem struct {
	kind       h1MarkKind
	index      uint8
	generation uint32
}

// h1SingletonTransaction is an opaque reservation token. Reserved slabs are
// unreachable from the object graph until commit publishes object.eigen.
type h1SingletonTransaction struct {
	id          uint32
	object      h1ObjectRef
	eigen       h1EigenRef
	environment h1EnvironmentRef
}

type h1CallData struct {
	object       h1ObjectRef
	eigen        h1EigenRef
	fallback     dispatchClassID
	selector     selectorID
	target       h1TargetID
	environment  h1EnvironmentRef
	installation uint32
	visibility   lookupVisibility
	epoch        uint64
}

// h1PICArm is weak: neither collection nor close scans it as a root.
type h1PICArm struct {
	eigen        h1EigenRef
	fallback     dispatchClassID
	selector     selectorID
	target       h1TargetID
	environment  h1EnvironmentRef
	installation uint32
	visibility   lookupVisibility
	epoch        uint64
}

type h1ArenaStats struct {
	ObjectAllocations      uint64
	EigenAllocations       uint64
	EnvironmentAllocations uint64
	ObjectReclaims         uint64
	EigenReclaims          uint64
	EnvironmentReclaims    uint64
	Collections            uint64
	LiveObjects            uint8
	LiveEigen              uint8
	LiveEnvironments       uint8
	RetiredObjects         uint8
	RetiredEigen           uint8
	RetiredEnvironments    uint8
	Roots                  uint8
	MarkHighWater          uint8
	LastMarkWork           uint8
	TotalMarkWork          uint64
}

type h1CollectionWork struct {
	RootValues         uint8
	MarkedObjects      uint8
	MarkedEigen        uint8
	MarkedEnvironments uint8
	ObjectScans        uint8
	EigenScans         uint8
	EnvironmentScans   uint8
	ReclaimedObjects   uint8
	ReclaimedEigen     uint8
	ReclaimedEnvs      uint8
}

type h1Arena struct {
	objects      [h1ObjectCapacity]h1ObjectSlot
	eigen        [h1EigenCapacity]h1EigenSlot
	environments [h1EnvironmentCapacity]h1EnvironmentSlot

	objectFree      uint8
	eigenFree       uint8
	environmentFree uint8
	roots           [h1RootCapacity]h1Value
	rootCount       uint8
	markWork        [h1MarkCapacity]h1MarkItem
	markCount       uint8

	transaction      h1SingletonTransaction
	nextTransaction  uint32
	nextInstallation uint32
	nextEpoch        uint64
	closed           bool
	statistics       h1ArenaStats
	lastWork         h1CollectionWork
}

func newH1Arena() *h1Arena {
	arena := &h1Arena{objectFree: 1, eigenFree: 1, environmentFree: 1}
	for index := range arena.objects {
		slot := &arena.objects[index]
		slot.generation = 1
		if index+1 < len(arena.objects) {
			slot.nextFree = uint8(index + 2)
		}
	}
	for index := range arena.eigen {
		slot := &arena.eigen[index]
		slot.generation = 1
		if index+1 < len(arena.eigen) {
			slot.nextFree = uint8(index + 2)
		}
	}
	for index := range arena.environments {
		slot := &arena.environments[index]
		slot.generation = 1
		if index+1 < len(arena.environments) {
			slot.nextFree = uint8(index + 2)
		}
	}
	return arena
}

func h1PackReference(index uint8, generation uint32) uint32 {
	if index == 0 || generation == 0 || generation > h1MaximumGeneration {
		return 0
	}
	return generation<<h1ReferenceIndexBits | uint32(index)
}

func h1UnpackReference(reference uint32) (uint8, uint32, bool) {
	index := uint8(reference & h1ReferenceIndexMask)
	generation := reference >> h1ReferenceIndexBits
	return index, generation, index != 0 && generation != 0
}

func (arena *h1Arena) allocateObject(dispatch dispatchClassID, shape shapeID, value int64) (h1ObjectRef, error) {
	if arena == nil || arena.closed {
		return 0, errH1ArenaClosed
	}
	index := arena.objectFree
	if index == 0 {
		return 0, errH1ArenaCapacity
	}
	slot := &arena.objects[index-1]
	if slot.live || slot.retired || slot.generation == 0 || dispatch == 0 || shape == 0 {
		return 0, errH1ArenaInvalidRef
	}
	arena.objectFree = slot.nextFree
	generation := slot.generation
	*slot = h1ObjectSlot{generation: generation, live: true, dispatch: dispatch, shape: shape, value: value}
	arena.statistics.ObjectAllocations++
	arena.statistics.LiveObjects++
	return h1ObjectRef(h1PackReference(index, generation)), nil
}

func (arena *h1Arena) resolveObject(reference h1ObjectRef) (*h1ObjectSlot, error) {
	if arena == nil || arena.closed {
		return nil, errH1ArenaClosed
	}
	index, generation, ok := h1UnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.objects) {
		return nil, errH1ArenaInvalidRef
	}
	slot := &arena.objects[index-1]
	if !slot.live || slot.retired || slot.generation != generation {
		return nil, errH1ArenaInvalidRef
	}
	return slot, nil
}

func (arena *h1Arena) rootMarker() uint8 {
	if arena == nil {
		return 0
	}
	return arena.rootCount
}

func (arena *h1Arena) pushRoot(value h1Value) error {
	if err := arena.validateValue(value); err != nil {
		return err
	}
	if int(arena.rootCount) == len(arena.roots) {
		return errH1ArenaCapacity
	}
	arena.roots[arena.rootCount] = value
	arena.rootCount++
	arena.statistics.Roots = arena.rootCount
	return nil
}

func (arena *h1Arena) popRoots(marker uint8) error {
	if arena == nil || arena.closed {
		return errH1ArenaClosed
	}
	if marker > arena.rootCount {
		return errH1ArenaRootOrder
	}
	for arena.rootCount > marker {
		arena.rootCount--
		arena.roots[arena.rootCount] = h1Value{}
	}
	arena.statistics.Roots = arena.rootCount
	return nil
}

func (arena *h1Arena) beginSingletonDefinition(
	object h1ObjectRef,
	selector selectorID,
	target h1TargetID,
	visibility lookupVisibility,
	captured h1Value,
	parent h1EnvironmentRef,
) (h1SingletonTransaction, error) {
	if arena == nil || arena.closed {
		return h1SingletonTransaction{}, errH1ArenaClosed
	}
	if arena.transaction.id != 0 {
		return h1SingletonTransaction{}, errH1ArenaTransactionRun
	}
	objectSlot, err := arena.resolveObject(object)
	if err != nil {
		return h1SingletonTransaction{}, err
	}
	if objectSlot.eigen != 0 || selector == 0 || target == 0 || !validLookupVisibility(visibility) ||
		arena.nextTransaction == ^uint32(0) || arena.nextInstallation == ^uint32(0) || arena.nextEpoch == ^uint64(0) {
		return h1SingletonTransaction{}, errH1ArenaTransaction
	}
	if err := arena.validateValue(captured); err != nil {
		return h1SingletonTransaction{}, err
	}
	if parent != 0 {
		if _, err := arena.resolveEnvironment(parent); err != nil {
			return h1SingletonTransaction{}, err
		}
	}
	environment, err := arena.reserveEnvironment(captured, parent)
	if err != nil {
		return h1SingletonTransaction{}, err
	}
	entry := h1EigenEntry{
		selector: selector, target: target, environment: environment,
		installation: arena.nextInstallation + 1, visibility: visibility, epoch: arena.nextEpoch + 1,
	}
	eigen, err := arena.reserveEigen(objectSlot.dispatch, entry)
	if err != nil {
		arena.releaseReservedEnvironment(environment)
		return h1SingletonTransaction{}, err
	}
	arena.nextTransaction++
	arena.transaction = h1SingletonTransaction{id: arena.nextTransaction, object: object, eigen: eigen, environment: environment}
	return arena.transaction, nil
}

func (arena *h1Arena) commitSingletonDefinition(transaction h1SingletonTransaction, canceled bool) error {
	if arena == nil || arena.closed {
		return errH1ArenaClosed
	}
	if !arena.validTransaction(transaction) {
		return errH1ArenaTransaction
	}
	if canceled {
		arena.abortSingletonDefinition(transaction)
		return context.Canceled
	}
	object, err := arena.resolveObject(transaction.object)
	if err != nil || object.eigen != 0 {
		arena.abortSingletonDefinition(transaction)
		return errH1ArenaTransaction
	}
	eigenIndex, eigenGeneration, eigenOK := h1UnpackReference(uint32(transaction.eigen))
	environmentIndex, environmentGeneration, environmentOK := h1UnpackReference(uint32(transaction.environment))
	if !eigenOK || int(eigenIndex) > len(arena.eigen) || !environmentOK || int(environmentIndex) > len(arena.environments) {
		arena.abortSingletonDefinition(transaction)
		return errH1ArenaTransaction
	}
	eigen := &arena.eigen[eigenIndex-1]
	environment := &arena.environments[environmentIndex-1]
	if !eigen.reserved || eigen.live || eigen.generation != eigenGeneration || !environment.reserved || environment.live ||
		environment.generation != environmentGeneration || eigen.entry.environment != transaction.environment ||
		eigen.entry.installation != arena.nextInstallation+1 || eigen.entry.epoch != arena.nextEpoch+1 {
		arena.abortSingletonDefinition(transaction)
		return errH1ArenaTransaction
	}

	// All fallible validation is complete. Make the reserved nodes live, then
	// publish the object's eigen reference as the final graph mutation.
	environment.reserved, environment.live = false, true
	eigen.reserved, eigen.live = false, true
	arena.nextInstallation = eigen.entry.installation
	arena.nextEpoch = eigen.entry.epoch
	arena.statistics.EnvironmentAllocations++
	arena.statistics.EigenAllocations++
	arena.statistics.LiveEnvironments++
	arena.statistics.LiveEigen++
	object.eigen = transaction.eigen
	arena.transaction = h1SingletonTransaction{}
	return nil
}

func (arena *h1Arena) abortSingletonDefinition(transaction h1SingletonTransaction) error {
	if arena == nil || arena.closed {
		return errH1ArenaClosed
	}
	if !arena.validTransaction(transaction) {
		return errH1ArenaTransaction
	}
	arena.releaseReservedEigen(transaction.eigen)
	arena.releaseReservedEnvironment(transaction.environment)
	arena.transaction = h1SingletonTransaction{}
	return nil
}

func (arena *h1Arena) validTransaction(transaction h1SingletonTransaction) bool {
	return transaction.id != 0 && transaction == arena.transaction
}

func (arena *h1Arena) reserveEigen(fallback dispatchClassID, entry h1EigenEntry) (h1EigenRef, error) {
	index := arena.eigenFree
	if index == 0 {
		return 0, errH1ArenaCapacity
	}
	slot := &arena.eigen[index-1]
	if slot.live || slot.reserved || slot.retired || slot.generation == 0 {
		return 0, errH1ArenaInvalidRef
	}
	arena.eigenFree = slot.nextFree
	generation := slot.generation
	*slot = h1EigenSlot{generation: generation, reserved: true, fallback: fallback, entry: entry}
	return h1EigenRef(h1PackReference(index, generation)), nil
}

func (arena *h1Arena) reserveEnvironment(captured h1Value, parent h1EnvironmentRef) (h1EnvironmentRef, error) {
	index := arena.environmentFree
	if index == 0 {
		return 0, errH1ArenaCapacity
	}
	slot := &arena.environments[index-1]
	if slot.live || slot.reserved || slot.retired || slot.generation == 0 {
		return 0, errH1ArenaInvalidRef
	}
	arena.environmentFree = slot.nextFree
	generation := slot.generation
	*slot = h1EnvironmentSlot{generation: generation, reserved: true, parent: parent, cells: [h1EnvironmentCells]h1Value{captured}}
	return h1EnvironmentRef(h1PackReference(index, generation)), nil
}

func (arena *h1Arena) releaseReservedEigen(reference h1EigenRef) {
	index, generation, ok := h1UnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.eigen) {
		return
	}
	slot := &arena.eigen[index-1]
	if !slot.reserved || slot.live || slot.generation != generation {
		return
	}
	*slot = h1EigenSlot{generation: generation}
	arena.insertEigenFree(index)
}

func (arena *h1Arena) releaseReservedEnvironment(reference h1EnvironmentRef) {
	index, generation, ok := h1UnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.environments) {
		return
	}
	slot := &arena.environments[index-1]
	if !slot.reserved || slot.live || slot.generation != generation {
		return
	}
	*slot = h1EnvironmentSlot{generation: generation}
	arena.insertEnvironmentFree(index)
}

func (arena *h1Arena) resolveCallData(objectRef h1ObjectRef, selector selectorID) (h1CallData, error) {
	object, err := arena.resolveObject(objectRef)
	if err != nil {
		return h1CallData{}, err
	}
	if object.eigen == 0 {
		return h1CallData{}, errH1ArenaNoMethod
	}
	eigen, err := arena.resolveEigen(object.eigen)
	if err != nil {
		return h1CallData{}, err
	}
	entry := eigen.entry
	if entry.selector != selector || entry.target == 0 || entry.installation == 0 || entry.epoch == 0 || !validLookupVisibility(entry.visibility) {
		return h1CallData{}, errH1ArenaNoMethod
	}
	if _, err := arena.resolveEnvironment(entry.environment); err != nil {
		return h1CallData{}, err
	}
	return h1CallData{
		object: objectRef, eigen: object.eigen, fallback: eigen.fallback, selector: entry.selector, target: entry.target,
		environment: entry.environment, installation: entry.installation, visibility: entry.visibility, epoch: entry.epoch,
	}, nil
}

func (arena *h1Arena) admitPICArm(data h1CallData) (h1PICArm, error) {
	current, err := arena.resolveCallData(data.object, data.selector)
	if err != nil || current != data {
		return h1PICArm{}, errH1ArenaInvalidRef
	}
	return h1PICArm{
		eigen: data.eigen, fallback: data.fallback, selector: data.selector, target: data.target, environment: data.environment,
		installation: data.installation, visibility: data.visibility, epoch: data.epoch,
	}, nil
}

func (arena *h1Arena) hitPICArm(objectRef h1ObjectRef, selector selectorID, arm h1PICArm) (h1CallData, bool) {
	current, err := arena.resolveCallData(objectRef, selector)
	if err != nil {
		return h1CallData{}, false
	}
	return current, current.eigen == arm.eigen && current.fallback == arm.fallback && current.selector == arm.selector && current.target == arm.target &&
		current.environment == arm.environment && current.installation == arm.installation && current.visibility == arm.visibility && current.epoch == arm.epoch
}

func (arena *h1Arena) resolveEigen(reference h1EigenRef) (*h1EigenSlot, error) {
	if arena == nil || arena.closed {
		return nil, errH1ArenaClosed
	}
	index, generation, ok := h1UnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.eigen) {
		return nil, errH1ArenaInvalidRef
	}
	slot := &arena.eigen[index-1]
	if !slot.live || slot.reserved || slot.retired || slot.generation != generation {
		return nil, errH1ArenaInvalidRef
	}
	return slot, nil
}

func (arena *h1Arena) resolveEnvironment(reference h1EnvironmentRef) (*h1EnvironmentSlot, error) {
	if arena == nil || arena.closed {
		return nil, errH1ArenaClosed
	}
	index, generation, ok := h1UnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.environments) {
		return nil, errH1ArenaInvalidRef
	}
	slot := &arena.environments[index-1]
	if !slot.live || slot.reserved || slot.retired || slot.generation != generation {
		return nil, errH1ArenaInvalidRef
	}
	return slot, nil
}

func (arena *h1Arena) validateValue(value h1Value) error {
	if arena == nil || arena.closed {
		return errH1ArenaClosed
	}
	switch value.kind {
	case h1ValueNil:
		if value != (h1Value{}) {
			return errH1ArenaInvalidRef
		}
	case h1ValueInteger:
	case h1ValueObject:
		if value.payload == 0 || value.payload > uint64(^uint32(0)) {
			return errH1ArenaInvalidRef
		}
		_, err := arena.resolveObject(value.objectRef())
		return err
	case h1ValueEnvironment:
		if value.payload == 0 || value.payload > uint64(^uint32(0)) {
			return errH1ArenaInvalidRef
		}
		_, err := arena.resolveEnvironment(value.environmentRef())
		return err
	default:
		return errH1ArenaInvalidRef
	}
	return nil
}

func (arena *h1Arena) loadObjectField(reference h1ObjectRef, field uint8) (h1Value, error) {
	slot, err := arena.resolveObject(reference)
	if err != nil {
		return h1Value{}, err
	}
	if int(field) >= len(slot.fields) {
		return h1Value{}, errH1ArenaInvalidRef
	}
	return slot.fields[field], nil
}

func (arena *h1Arena) storeObjectField(reference h1ObjectRef, field uint8, value h1Value) error {
	slot, err := arena.resolveObject(reference)
	if err != nil {
		return err
	}
	if int(field) >= len(slot.fields) {
		return errH1ArenaInvalidRef
	}
	if err := arena.validateValue(value); err != nil {
		return err
	}
	slot.fields[field] = value
	return nil
}

func (arena *h1Arena) loadEnvironment(reference h1EnvironmentRef, cell uint8) (h1Value, error) {
	slot, err := arena.resolveEnvironment(reference)
	if err != nil {
		return h1Value{}, err
	}
	if int(cell) >= len(slot.cells) {
		return h1Value{}, errH1ArenaInvalidRef
	}
	return slot.cells[cell], nil
}

func (arena *h1Arena) storeEnvironment(reference h1EnvironmentRef, cell uint8, value h1Value) error {
	slot, err := arena.resolveEnvironment(reference)
	if err != nil {
		return err
	}
	if int(cell) >= len(slot.cells) {
		return errH1ArenaInvalidRef
	}
	if err := arena.validateValue(value); err != nil {
		return err
	}
	slot.cells[cell] = value
	return nil
}

func (arena *h1Arena) collect(canceled bool) error {
	if arena == nil || arena.closed {
		return errH1ArenaClosed
	}
	if arena.transaction.id != 0 {
		return errH1ArenaTransactionRun
	}
	arena.clearMarks()
	work := h1CollectionWork{RootValues: arena.rootCount}
	for index := uint8(0); index < arena.rootCount; index++ {
		if err := arena.markValue(arena.roots[index]); err != nil {
			arena.clearMarks()
			return err
		}
	}
	for arena.markCount != 0 {
		arena.markCount--
		item := arena.markWork[arena.markCount]
		arena.markWork[arena.markCount] = h1MarkItem{}
		switch item.kind {
		case h1MarkObject:
			work.MarkedObjects++
		case h1MarkEigen:
			work.MarkedEigen++
		case h1MarkEnvironment:
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

	// Marking and cancellation are complete. The bounded sweep below cannot
	// fail, so collection never exposes a partially reclaimed graph.
	for index := range arena.objects {
		work.ObjectScans++
		slot := &arena.objects[index]
		if slot.live && !slot.marked {
			arena.reclaimObject(uint8(index + 1))
			work.ReclaimedObjects++
		}
	}
	for index := range arena.eigen {
		work.EigenScans++
		slot := &arena.eigen[index]
		if slot.live && !slot.marked {
			arena.reclaimEigen(uint8(index + 1))
			work.ReclaimedEigen++
		}
	}
	for index := range arena.environments {
		work.EnvironmentScans++
		slot := &arena.environments[index]
		if slot.live && !slot.marked {
			arena.reclaimEnvironment(uint8(index + 1))
			work.ReclaimedEnvs++
		}
	}
	markWork := work.MarkedObjects + work.MarkedEigen + work.MarkedEnvironments
	arena.clearMarks()
	arena.lastWork = work
	arena.statistics.LastMarkWork = markWork
	arena.statistics.TotalMarkWork += uint64(markWork)
	arena.statistics.Collections++
	return nil
}

func (arena *h1Arena) traceMarked(item h1MarkItem) error {
	switch item.kind {
	case h1MarkObject:
		if item.index == 0 || int(item.index) > len(arena.objects) {
			return errH1ArenaInvalidRef
		}
		slot := &arena.objects[item.index-1]
		if !slot.live || !slot.marked || slot.generation != item.generation {
			return errH1ArenaInvalidRef
		}
		for _, field := range slot.fields {
			if err := arena.markValue(field); err != nil {
				return err
			}
		}
		if slot.eigen != 0 {
			return arena.markEigen(slot.eigen)
		}
	case h1MarkEigen:
		if item.index == 0 || int(item.index) > len(arena.eigen) {
			return errH1ArenaInvalidRef
		}
		slot := &arena.eigen[item.index-1]
		if !slot.live || !slot.marked || slot.generation != item.generation {
			return errH1ArenaInvalidRef
		}
		return arena.markEnvironment(slot.entry.environment)
	case h1MarkEnvironment:
		if item.index == 0 || int(item.index) > len(arena.environments) {
			return errH1ArenaInvalidRef
		}
		slot := &arena.environments[item.index-1]
		if !slot.live || !slot.marked || slot.generation != item.generation {
			return errH1ArenaInvalidRef
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
	default:
		return errH1ArenaInvalidRef
	}
	return nil
}

func (arena *h1Arena) markValue(value h1Value) error {
	switch value.kind {
	case h1ValueNil:
		if value != (h1Value{}) {
			return errH1ArenaInvalidRef
		}
		return nil
	case h1ValueInteger:
		return nil
	case h1ValueObject:
		if value.payload == 0 || value.payload > uint64(^uint32(0)) {
			return errH1ArenaInvalidRef
		}
		return arena.markObject(value.objectRef())
	case h1ValueEnvironment:
		if value.payload == 0 || value.payload > uint64(^uint32(0)) {
			return errH1ArenaInvalidRef
		}
		return arena.markEnvironment(value.environmentRef())
	default:
		return errH1ArenaInvalidRef
	}
}

func (arena *h1Arena) markObject(reference h1ObjectRef) error {
	index, generation, ok := h1UnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.objects) {
		return errH1ArenaInvalidRef
	}
	slot := &arena.objects[index-1]
	if !slot.live || slot.generation != generation {
		return errH1ArenaInvalidRef
	}
	if slot.marked {
		return nil
	}
	slot.marked = true
	return arena.markPush(h1MarkItem{kind: h1MarkObject, index: index, generation: generation})
}

func (arena *h1Arena) markEigen(reference h1EigenRef) error {
	index, generation, ok := h1UnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.eigen) {
		return errH1ArenaInvalidRef
	}
	slot := &arena.eigen[index-1]
	if !slot.live || slot.generation != generation {
		return errH1ArenaInvalidRef
	}
	if slot.marked {
		return nil
	}
	slot.marked = true
	return arena.markPush(h1MarkItem{kind: h1MarkEigen, index: index, generation: generation})
}

func (arena *h1Arena) markEnvironment(reference h1EnvironmentRef) error {
	index, generation, ok := h1UnpackReference(uint32(reference))
	if !ok || int(index) > len(arena.environments) {
		return errH1ArenaInvalidRef
	}
	slot := &arena.environments[index-1]
	if !slot.live || slot.generation != generation {
		return errH1ArenaInvalidRef
	}
	if slot.marked {
		return nil
	}
	slot.marked = true
	return arena.markPush(h1MarkItem{kind: h1MarkEnvironment, index: index, generation: generation})
}

func (arena *h1Arena) markPush(item h1MarkItem) error {
	if int(arena.markCount) == len(arena.markWork) {
		return errH1ArenaCapacity
	}
	arena.markWork[arena.markCount] = item
	arena.markCount++
	if arena.markCount > arena.statistics.MarkHighWater {
		arena.statistics.MarkHighWater = arena.markCount
	}
	return nil
}

func (arena *h1Arena) clearMarks() {
	for index := range arena.objects {
		arena.objects[index].marked = false
	}
	for index := range arena.eigen {
		arena.eigen[index].marked = false
	}
	for index := range arena.environments {
		arena.environments[index].marked = false
	}
	arena.markWork = [h1MarkCapacity]h1MarkItem{}
	arena.markCount = 0
}

func (arena *h1Arena) reclaimObject(index uint8) {
	slot := &arena.objects[index-1]
	generation, retired := h1NextGeneration(slot.generation)
	*slot = h1ObjectSlot{generation: generation, retired: retired}
	if retired {
		arena.statistics.RetiredObjects++
	} else {
		arena.insertObjectFree(index)
	}
	arena.statistics.ObjectReclaims++
	arena.statistics.LiveObjects--
}

func (arena *h1Arena) reclaimEigen(index uint8) {
	slot := &arena.eigen[index-1]
	generation, retired := h1NextGeneration(slot.generation)
	*slot = h1EigenSlot{generation: generation, retired: retired}
	if retired {
		arena.statistics.RetiredEigen++
	} else {
		arena.insertEigenFree(index)
	}
	arena.statistics.EigenReclaims++
	arena.statistics.LiveEigen--
}

func (arena *h1Arena) reclaimEnvironment(index uint8) {
	slot := &arena.environments[index-1]
	generation, retired := h1NextGeneration(slot.generation)
	*slot = h1EnvironmentSlot{generation: generation, retired: retired}
	if retired {
		arena.statistics.RetiredEnvironments++
	} else {
		arena.insertEnvironmentFree(index)
	}
	arena.statistics.EnvironmentReclaims++
	arena.statistics.LiveEnvironments--
}

func h1NextGeneration(generation uint32) (uint32, bool) {
	if generation == 0 || generation >= h1MaximumGeneration {
		return h1MaximumGeneration, true
	}
	return generation + 1, false
}

func (arena *h1Arena) insertObjectFree(index uint8) {
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

func (arena *h1Arena) insertEigenFree(index uint8) {
	if arena.eigenFree == 0 || index < arena.eigenFree {
		arena.eigen[index-1].nextFree = arena.eigenFree
		arena.eigenFree = index
		return
	}
	current := arena.eigenFree
	for arena.eigen[current-1].nextFree != 0 && arena.eigen[current-1].nextFree < index {
		current = arena.eigen[current-1].nextFree
	}
	arena.eigen[index-1].nextFree = arena.eigen[current-1].nextFree
	arena.eigen[current-1].nextFree = index
}

func (arena *h1Arena) insertEnvironmentFree(index uint8) {
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

func (arena *h1Arena) stats() h1ArenaStats {
	if arena == nil {
		return h1ArenaStats{}
	}
	return arena.statistics
}

func (arena *h1Arena) collectionWork() h1CollectionWork {
	if arena == nil {
		return h1CollectionWork{}
	}
	return arena.lastWork
}

// close is idempotent and erases every owner-local identity and weak arm
// referent. No H1 reference is valid after close.
func (arena *h1Arena) close() error {
	if arena == nil || arena.closed {
		return nil
	}
	if arena.transaction.id != 0 {
		return errH1ArenaTransactionRun
	}
	*arena = h1Arena{closed: true}
	return nil
}
