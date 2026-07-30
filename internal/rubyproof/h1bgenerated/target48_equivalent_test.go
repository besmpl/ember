package h1bgenerated

const (
	target48ObjectCapacity      = 4
	target48EnvironmentCapacity = 2
	target48RootCapacity        = 8
	target48MarkCapacity        = 6
	target48IndexBits           = 8
	target48IndexMask           = 1<<target48IndexBits - 1
	target48MaximumGeneration   = 1<<(32-target48IndexBits) - 1
)

type target48Error string

func (e target48Error) Error() string { return string(e) }

const (
	target48ErrCapacity   target48Error = "target48: capacity exceeded"
	target48ErrInvalidRef target48Error = "target48: invalid reference"
	target48ErrRootOrder  target48Error = "target48: root order mismatch"
	target48ErrStaging    target48Error = "target48: transaction active"
)

type target48ObjectRef uint32
type target48EnvironmentRef uint32

type target48ValueKind uint8

const (
	target48ValueNil target48ValueKind = iota
	target48ValueInteger
	target48ValueObject
	target48ValueEnvironment
)

type target48Value struct {
	payload uint64
	kind    target48ValueKind
}

func target48IntegerValue(v int64) target48Value {
	return target48Value{payload: uint64(v), kind: target48ValueInteger}
}

func target48ObjectValue(v target48ObjectRef) target48Value {
	return target48Value{payload: uint64(v), kind: target48ValueObject}
}

func (v target48Value) target48Integer() int64 { return int64(v.payload) }
func (v target48Value) target48Object() target48ObjectRef {
	return target48ObjectRef(v.payload)
}
func (v target48Value) target48Environment() target48EnvironmentRef {
	return target48EnvironmentRef(v.payload)
}

type target48ObjectSlot struct {
	generation  uint32
	nextFree    uint8
	live        bool
	marked      bool
	retired     bool
	dispatch    uint16
	shape       uint16
	environment target48EnvironmentRef
	present     bool
}

type target48EnvironmentSlot struct {
	generation uint32
	nextFree   uint8
	live       bool
	reserved   bool
	marked     bool
	retired    bool
	cell       target48Value
}

type target48MarkKind uint8

const (
	target48MarkObject target48MarkKind = iota + 1
	target48MarkEnvironment
)

type target48MarkItem struct {
	kind       target48MarkKind
	index      uint8
	generation uint32
}

type target48Transaction struct {
	id          uint32
	object      target48ObjectRef
	environment target48EnvironmentRef
}

type target48WeakArm struct {
	object      target48ObjectRef
	environment target48EnvironmentRef
}

type target48OrdinaryArm struct {
	dispatch uint16
	shape    uint16
	value    int64
	valid    bool
	hits     uint8
	misses   uint8
}

type target48Stats struct {
	objectAllocations      uint64
	environmentAllocations uint64
	objectReclaims         uint64
	environmentReclaims    uint64
	collections            uint64
	totalMarkWork          uint64
	lastMarkWork           uint8
	markHighWater          uint8
	liveObjects            uint8
	liveEnvironments       uint8
	roots                  uint8
}

type target48Arena struct {
	objects         [target48ObjectCapacity]target48ObjectSlot
	environments    [target48EnvironmentCapacity]target48EnvironmentSlot
	objectFree      uint8
	environmentFree uint8
	roots           [target48RootCapacity]target48Value
	rootCount       uint8
	marks           [target48MarkCapacity]target48MarkItem
	markCount       uint8
	transaction     target48Transaction
	nextTransaction uint32
	stats           target48Stats
}

type target48Engine struct {
	arena                    target48Arena
	ordinaryArm              target48OrdinaryArm
	singletonArm             target48WeakArm
	forceEnvironmentPressure bool
	busy                     bool
	closed                   bool
	poisoned                 bool
}

func target48Pack(index uint8, generation uint32) uint32 {
	if index == 0 || generation == 0 || generation > target48MaximumGeneration {
		return 0
	}
	return generation<<target48IndexBits | uint32(index)
}

func target48Unpack(reference uint32) (uint8, uint32, bool) {
	index, generation := uint8(reference&target48IndexMask), reference>>target48IndexBits
	return index, generation, index != 0 && generation != 0
}

func (a *target48Arena) target48Init() {
	*a = target48Arena{objectFree: 1, environmentFree: 1}
	for i := range a.objects {
		a.objects[i].generation = 1
		if i+1 < len(a.objects) {
			a.objects[i].nextFree = uint8(i + 2)
		}
	}
	for i := range a.environments {
		a.environments[i].generation = 1
		if i+1 < len(a.environments) {
			a.environments[i].nextFree = uint8(i + 2)
		}
	}
}

func (a *target48Arena) target48AllocateObject() (target48ObjectRef, error) {
	index := a.objectFree
	if index == 0 {
		return 0, target48ErrCapacity
	}
	slot := &a.objects[index-1]
	if slot.live || slot.retired || slot.generation == 0 {
		return 0, target48ErrInvalidRef
	}
	a.objectFree = slot.nextFree
	generation := slot.generation
	*slot = target48ObjectSlot{generation: generation, live: true, dispatch: 1, shape: 1}
	a.stats.objectAllocations++
	a.stats.liveObjects++
	return target48ObjectRef(target48Pack(index, generation)), nil
}

func (a *target48Arena) target48ResolveObject(reference target48ObjectRef) (*target48ObjectSlot, error) {
	index, generation, ok := target48Unpack(uint32(reference))
	if !ok || int(index) > len(a.objects) {
		return nil, target48ErrInvalidRef
	}
	slot := &a.objects[index-1]
	if !slot.live || slot.retired || slot.generation != generation {
		return nil, target48ErrInvalidRef
	}
	return slot, nil
}

func (a *target48Arena) target48ResolveEnvironment(reference target48EnvironmentRef) (*target48EnvironmentSlot, error) {
	index, generation, ok := target48Unpack(uint32(reference))
	if !ok || int(index) > len(a.environments) {
		return nil, target48ErrInvalidRef
	}
	slot := &a.environments[index-1]
	if !slot.live || slot.reserved || slot.retired || slot.generation != generation {
		return nil, target48ErrInvalidRef
	}
	return slot, nil
}

func (a *target48Arena) target48ReserveEnvironment(captured target48Value) (target48EnvironmentRef, error) {
	index := a.environmentFree
	if index == 0 {
		return 0, target48ErrCapacity
	}
	slot := &a.environments[index-1]
	if slot.live || slot.reserved || slot.retired || slot.generation == 0 {
		return 0, target48ErrInvalidRef
	}
	a.environmentFree = slot.nextFree
	generation := slot.generation
	*slot = target48EnvironmentSlot{generation: generation, reserved: true, cell: captured}
	return target48EnvironmentRef(target48Pack(index, generation)), nil
}

func (a *target48Arena) target48Install(object target48ObjectRef, captured int64) error {
	objectSlot, err := a.target48ResolveObject(object)
	if err != nil || objectSlot.present || objectSlot.environment != 0 {
		return target48ErrInvalidRef
	}
	environment, err := a.target48ReserveEnvironment(target48IntegerValue(captured))
	if err != nil {
		return err
	}
	index, generation, ok := target48Unpack(uint32(environment))
	if !ok || int(index) > len(a.environments) {
		return target48ErrInvalidRef
	}
	environmentSlot := &a.environments[index-1]
	if !environmentSlot.reserved || environmentSlot.live || environmentSlot.generation != generation {
		return target48ErrInvalidRef
	}
	environmentSlot.reserved = false
	environmentSlot.live = true
	a.stats.environmentAllocations++
	a.stats.liveEnvironments++
	objectSlot.environment = environment
	objectSlot.present = true
	return nil
}

func (a *target48Arena) target48EnvironmentCell(reference target48EnvironmentRef) (int64, error) {
	slot, err := a.target48ResolveEnvironment(reference)
	if err != nil {
		return 0, err
	}
	if slot.cell.kind != target48ValueInteger {
		return 0, target48ErrInvalidRef
	}
	return slot.cell.target48Integer(), nil
}

func (a *target48Arena) target48StoreEnvironmentCell(reference target48EnvironmentRef, v int64) error {
	slot, err := a.target48ResolveEnvironment(reference)
	if err != nil {
		return err
	}
	slot.cell = target48IntegerValue(v)
	return nil
}

// target48ReadLabel independently reproduces the whole generated selection
// contract. The primary benchmark keeps the ordinary and singleton arms warm
// at one callsite; deterministic tests also keep the cold-repair path honest.
func target48ReadLabel(e *target48Engine, reference target48ObjectRef) (int64, error) {
	object, err := e.arena.target48ResolveObject(reference)
	if err != nil {
		return 0, err
	}
	if e.singletonArm.object != 0 {
		if object.present && object.environment != 0 && e.singletonArm.object == reference && e.singletonArm.environment == object.environment {
			cell, err := e.arena.target48EnvironmentCell(e.singletonArm.environment)
			if err != nil {
				return 0, err
			}
			cell++
			if err = e.arena.target48StoreEnvironmentCell(e.singletonArm.environment, cell); err != nil {
				return 0, err
			}
			return cell, nil
		}
	}
	if e.ordinaryArm.valid {
		if !object.present && object.environment == 0 && object.dispatch == e.ordinaryArm.dispatch && object.shape == e.ordinaryArm.shape {
			e.ordinaryArm.hits++
			return e.ordinaryArm.value, nil
		}
		e.ordinaryArm.misses++
	}
	if !object.present {
		if object.environment != 0 {
			return 0, target48ErrInvalidRef
		}
		e.ordinaryArm.dispatch = object.dispatch
		e.ordinaryArm.shape = object.shape
		e.ordinaryArm.value = 7
		e.ordinaryArm.valid = true
		return e.ordinaryArm.value, nil
	}
	if object.environment == 0 {
		return 0, target48ErrInvalidRef
	}
	if e.singletonArm.object != reference || e.singletonArm.environment != object.environment {
		if _, err = e.arena.target48ResolveEnvironment(object.environment); err != nil {
			return 0, err
		}
		e.singletonArm = target48WeakArm{object: reference, environment: object.environment}
	}
	cell, err := e.arena.target48EnvironmentCell(e.singletonArm.environment)
	if err != nil {
		return 0, err
	}
	cell++
	if err = e.arena.target48StoreEnvironmentCell(e.singletonArm.environment, cell); err != nil {
		return 0, err
	}
	return cell, nil
}

func (a *target48Arena) target48PopRoots(marker uint8) error {
	if marker > a.rootCount {
		return target48ErrRootOrder
	}
	for a.rootCount > marker {
		a.rootCount--
		a.roots[a.rootCount] = target48Value{}
	}
	a.stats.roots = a.rootCount
	return nil
}

func (a *target48Arena) target48Collect() error {
	if a.transaction.id != 0 {
		return target48ErrStaging
	}
	a.target48ClearMarks()
	for i := uint8(0); i < a.rootCount; i++ {
		if err := a.target48MarkValue(a.roots[i]); err != nil {
			a.target48ClearMarks()
			return err
		}
	}
	marked := uint8(0)
	for a.markCount != 0 {
		a.markCount--
		item := a.marks[a.markCount]
		a.marks[a.markCount] = target48MarkItem{}
		marked++
		if err := a.target48Trace(item); err != nil {
			a.target48ClearMarks()
			return err
		}
	}
	for i := range a.objects {
		if a.objects[i].live && !a.objects[i].marked {
			a.target48ReclaimObject(uint8(i + 1))
		}
	}
	for i := range a.environments {
		if a.environments[i].live && !a.environments[i].marked {
			a.target48ReclaimEnvironment(uint8(i + 1))
		}
	}
	a.target48ClearMarks()
	a.stats.lastMarkWork = marked
	a.stats.totalMarkWork += uint64(marked)
	a.stats.collections++
	if marked > a.stats.markHighWater {
		a.stats.markHighWater = marked
	}
	return nil
}

func (a *target48Arena) target48MarkValue(v target48Value) error {
	switch v.kind {
	case target48ValueNil, target48ValueInteger:
		return nil
	case target48ValueObject:
		return a.target48MarkObject(v.target48Object())
	case target48ValueEnvironment:
		return a.target48MarkEnvironment(v.target48Environment())
	default:
		return target48ErrInvalidRef
	}
}

func (a *target48Arena) target48MarkObject(reference target48ObjectRef) error {
	index, generation, ok := target48Unpack(uint32(reference))
	if !ok || int(index) > len(a.objects) {
		return target48ErrInvalidRef
	}
	slot := &a.objects[index-1]
	if !slot.live || slot.generation != generation {
		return target48ErrInvalidRef
	}
	if slot.marked {
		return nil
	}
	slot.marked = true
	return a.target48PushMark(target48MarkItem{kind: target48MarkObject, index: index, generation: generation})
}

func (a *target48Arena) target48MarkEnvironment(reference target48EnvironmentRef) error {
	index, generation, ok := target48Unpack(uint32(reference))
	if !ok || int(index) > len(a.environments) {
		return target48ErrInvalidRef
	}
	slot := &a.environments[index-1]
	if !slot.live || slot.reserved || slot.generation != generation {
		return target48ErrInvalidRef
	}
	if slot.marked {
		return nil
	}
	slot.marked = true
	return a.target48PushMark(target48MarkItem{kind: target48MarkEnvironment, index: index, generation: generation})

}

func (a *target48Arena) target48PushMark(item target48MarkItem) error {
	if int(a.markCount) == len(a.marks) {
		return target48ErrCapacity
	}
	a.marks[a.markCount] = item
	a.markCount++
	return nil
}

func (a *target48Arena) target48Trace(item target48MarkItem) error {
	switch item.kind {
	case target48MarkObject:
		if item.index == 0 || int(item.index) > len(a.objects) {
			return target48ErrInvalidRef
		}
		slot := &a.objects[item.index-1]
		if !slot.live || !slot.marked || slot.generation != item.generation {
			return target48ErrInvalidRef
		}
		if !slot.present {
			if slot.environment != 0 {
				return target48ErrInvalidRef
			}
			return nil
		}
		if slot.environment == 0 {
			return target48ErrInvalidRef
		}
		return a.target48MarkEnvironment(slot.environment)
	case target48MarkEnvironment:
		if item.index == 0 || int(item.index) > len(a.environments) {
			return target48ErrInvalidRef
		}
		slot := &a.environments[item.index-1]
		if !slot.live || slot.reserved || !slot.marked || slot.generation != item.generation || slot.cell.kind != target48ValueInteger {
			return target48ErrInvalidRef
		}
		return nil
	default:
		return target48ErrInvalidRef
	}
}

func (a *target48Arena) target48ClearMarks() {
	for i := range a.objects {
		a.objects[i].marked = false
	}
	for i := range a.environments {
		a.environments[i].marked = false
	}
	a.marks = [target48MarkCapacity]target48MarkItem{}
	a.markCount = 0
}

func target48NextGeneration(generation uint32) (uint32, bool) {
	if generation == 0 || generation >= target48MaximumGeneration {
		return 0, true
	}
	return generation + 1, false
}

func (a *target48Arena) target48ReclaimObject(index uint8) {
	slot := &a.objects[index-1]
	generation, retired := target48NextGeneration(slot.generation)
	*slot = target48ObjectSlot{generation: generation, retired: retired}
	if !retired {
		a.target48InsertObjectFree(index)
	}
	a.stats.objectReclaims++
	a.stats.liveObjects--
}

func (a *target48Arena) target48ReclaimEnvironment(index uint8) {
	slot := &a.environments[index-1]
	generation, retired := target48NextGeneration(slot.generation)
	*slot = target48EnvironmentSlot{generation: generation, retired: retired}
	if !retired {
		a.target48InsertEnvironmentFree(index)
	}
	a.stats.environmentReclaims++
	a.stats.liveEnvironments--
}

func (a *target48Arena) target48InsertObjectFree(index uint8) {
	if a.objectFree == 0 || index < a.objectFree {
		a.objects[index-1].nextFree = a.objectFree
		a.objectFree = index
		return
	}
	current := a.objectFree
	for a.objects[current-1].nextFree != 0 && a.objects[current-1].nextFree < index {
		current = a.objects[current-1].nextFree
	}
	a.objects[index-1].nextFree = a.objects[current-1].nextFree
	a.objects[current-1].nextFree = index
}

func (a *target48Arena) target48InsertEnvironmentFree(index uint8) {
	if a.environmentFree == 0 || index < a.environmentFree {
		a.environments[index-1].nextFree = a.environmentFree
		a.environmentFree = index
		return
	}
	current := a.environmentFree
	for a.environments[current-1].nextFree != 0 && a.environments[current-1].nextFree < index {
		current = a.environments[current-1].nextFree
	}
	a.environments[index-1].nextFree = a.environments[current-1].nextFree
	a.environments[current-1].nextFree = index
}

//go:noinline
func target48NewHitState() (target48Engine, [2]target48ObjectRef, error) {
	var e target48Engine
	e.arena.target48Init()
	peer, err := e.arena.target48AllocateObject()
	if err != nil {
		return target48Engine{}, [2]target48ObjectRef{}, err
	}
	receiver, err := e.arena.target48AllocateObject()
	if err != nil {
		return target48Engine{}, [2]target48ObjectRef{}, err
	}
	if _, err = target48ReadLabel(&e, peer); err != nil {
		return target48Engine{}, [2]target48ObjectRef{}, err
	}
	if err = e.arena.target48Install(receiver, 40); err != nil {
		return target48Engine{}, [2]target48ObjectRef{}, err
	}
	if _, err = target48ReadLabel(&e, receiver); err != nil {
		return target48Engine{}, [2]target48ObjectRef{}, err
	}
	return e, [2]target48ObjectRef{peer, receiver}, nil
}

//go:noinline
func target48NewCollectorState() target48Engine {
	var e target48Engine
	e.arena.target48Init()
	peer := target48ObjectRef(target48Pack(1, 1))
	receiver := target48ObjectRef(target48Pack(2, 1))
	environment := target48EnvironmentRef(target48Pack(1, 1))
	e.arena.objects[0] = target48ObjectSlot{generation: 1, live: true, dispatch: 1, shape: 1}
	e.arena.objects[1] = target48ObjectSlot{generation: 1, live: true, dispatch: 1, shape: 1, environment: environment, present: true}
	e.arena.objects[2] = target48ObjectSlot{generation: 1, nextFree: 4}
	e.arena.objects[3] = target48ObjectSlot{generation: 1}
	e.arena.environments[0] = target48EnvironmentSlot{generation: 1, live: true, cell: target48IntegerValue(40)}
	e.arena.environments[1] = target48EnvironmentSlot{generation: 1}
	e.arena.objectFree = 3
	e.arena.environmentFree = 2
	e.arena.roots[0] = target48ObjectValue(peer)
	e.arena.roots[2] = target48ObjectValue(receiver)
	e.arena.rootCount = 3
	e.arena.stats = target48Stats{objectAllocations: 2, environmentAllocations: 1, liveObjects: 2, liveEnvironments: 1, roots: 3}
	e.singletonArm = target48WeakArm{object: receiver, environment: environment}
	return e
}

//go:noinline
func target48EquivalentHit(e *target48Engine, reference target48ObjectRef) (int64, error) {
	return target48ReadLabel(e, reference)
}

//go:noinline
func target48EquivalentCollectionCycle(e *target48Engine) error {
	if err := e.arena.target48Collect(); err != nil {
		return err
	}
	if err := e.arena.target48PopRoots(1); err != nil {
		return err
	}
	if err := e.arena.target48Collect(); err != nil {
		return err
	}
	if err := e.arena.target48PopRoots(0); err != nil {
		return err
	}
	return e.arena.target48Collect()
}
