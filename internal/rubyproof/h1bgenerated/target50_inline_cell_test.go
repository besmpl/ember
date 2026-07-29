package h1bgenerated

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

// Target50 is an independent, test-only representation falsifier for exact
// H1b. It deliberately does not call the generated arena, collector, PIC, or
// transaction helpers. The only shared surface in the parity tests is the
// frozen observable Result.
const (
	target50ObjectCapacity = 4
	target50RootCapacity   = 3
	target50MarkCapacity   = 4
	target50IndexBits      = 8
	target50IndexMask      = 1<<target50IndexBits - 1
	target50MaxGeneration  = 1<<24 - 1
)

var (
	errTarget50Capacity          = errors.New("target50: capacity exceeded")
	errTarget50InvalidReference  = errors.New("target50: invalid reference")
	errTarget50RootOrder         = errors.New("target50: root order mismatch")
	errTarget50Transaction       = errors.New("target50: invalid transaction")
	errTarget50TransactionActive = errors.New("target50: transaction active")
	errTarget50Busy              = errors.New("target50: busy")
	errTarget50Closed            = errors.New("target50: closed")
	errTarget50Poisoned          = errors.New("target50: poisoned")
	errTarget50Limit             = errors.New("target50: insufficient limits")
)

type target50ObjectRef uint32

type target50ObjectSlot struct {
	cell       int64
	generation uint32
	dispatch   uint16
	shape      uint16
	nextFree   uint8
	live       bool
	marked     bool
	retired    bool
	present    bool
}

type target50Transaction struct {
	cell   int64
	id     uint32
	object target50ObjectRef
}

type target50WeakArm struct{ object target50ObjectRef }

type target50OrdinaryArm struct {
	value    int64
	dispatch uint16
	shape    uint16
	hits     uint8
	misses   uint8
	valid    bool
}

type target50Stats struct {
	ObjectAllocations uint64
	ObjectReclaims    uint64
	Collections       uint64
	TotalMarkWork     uint64
	LiveObjects       uint8
	Roots             uint8
	LastMarkWork      uint8
	MarkHighWater     uint8
}

type target50Arena struct {
	objects [target50ObjectCapacity]target50ObjectSlot
	roots   [target50RootCapacity]target50ObjectRef
	marks   [target50MarkCapacity]target50ObjectRef
	stats   target50Stats

	transaction     target50Transaction
	nextTransaction uint32
	objectFree      uint8
	rootCount       uint8
	markCount       uint8
}

func (a *target50Arena) init() {
	*a = target50Arena{objectFree: 1}
	for index := range a.objects {
		a.objects[index].generation = 1
		if index+1 < len(a.objects) {
			a.objects[index].nextFree = uint8(index + 2)
		}
	}
}

func target50Pack(index uint8, generation uint32) uint32 {
	if index == 0 || generation == 0 || generation > target50MaxGeneration {
		return 0
	}
	return generation<<target50IndexBits | uint32(index)
}

func target50Unpack(reference uint32) (uint8, uint32, bool) {
	index, generation := uint8(reference&target50IndexMask), reference>>target50IndexBits
	return index, generation, index != 0 && generation != 0
}

func (a *target50Arena) allocateObject() (target50ObjectRef, error) {
	index := a.objectFree
	if index == 0 {
		return 0, errTarget50Capacity
	}
	slot := &a.objects[index-1]
	if slot.live || slot.retired || slot.generation == 0 {
		return 0, errTarget50InvalidReference
	}
	a.objectFree = slot.nextFree
	generation := slot.generation
	*slot = target50ObjectSlot{generation: generation, dispatch: 1, shape: 1, live: true}
	a.stats.ObjectAllocations++
	a.stats.LiveObjects++
	return target50ObjectRef(target50Pack(index, generation)), nil
}

func (a *target50Arena) resolveObject(reference target50ObjectRef) (*target50ObjectSlot, error) {
	index, generation, ok := target50Unpack(uint32(reference))
	if !ok || int(index) > len(a.objects) {
		return nil, errTarget50InvalidReference
	}
	slot := &a.objects[index-1]
	if !slot.live || slot.retired || slot.generation != generation {
		return nil, errTarget50InvalidReference
	}
	return slot, nil
}

func (a *target50Arena) pushRoot(reference target50ObjectRef) error {
	if _, err := a.resolveObject(reference); err != nil {
		return err
	}
	if int(a.rootCount) == len(a.roots) {
		return errTarget50Capacity
	}
	a.roots[a.rootCount] = reference
	a.rootCount++
	a.stats.Roots = a.rootCount
	return nil
}

func (a *target50Arena) popRoots(marker uint8) error {
	if marker > a.rootCount {
		return errTarget50RootOrder
	}
	for a.rootCount > marker {
		a.rootCount--
		a.roots[a.rootCount] = 0
	}
	a.stats.Roots = a.rootCount
	return nil
}

func (a *target50Arena) beginDefinitionAtSafePoint(ctx context.Context, object target50ObjectRef, cell int64) (target50Transaction, error) {
	if err := ctx.Err(); err != nil {
		return target50Transaction{}, err
	}
	return a.beginDefinition(object, cell)
}

func (a *target50Arena) beginDefinition(object target50ObjectRef, cell int64) (target50Transaction, error) {
	if a.transaction.id != 0 {
		return target50Transaction{}, errTarget50TransactionActive
	}
	slot, err := a.resolveObject(object)
	if err != nil {
		return target50Transaction{}, err
	}
	if slot.present || a.nextTransaction == ^uint32(0) {
		return target50Transaction{}, errTarget50Transaction
	}
	a.nextTransaction++
	a.transaction = target50Transaction{id: a.nextTransaction, object: object, cell: cell}
	return a.transaction, nil
}

func (a *target50Arena) abortDefinition(transaction target50Transaction) error {
	if transaction.id == 0 || transaction != a.transaction {
		return errTarget50Transaction
	}
	a.transaction = target50Transaction{}
	return nil
}

func (a *target50Arena) commitDefinition(transaction target50Transaction) error {
	if transaction.id == 0 || transaction != a.transaction {
		return errTarget50Transaction
	}
	slot, err := a.resolveObject(transaction.object)
	if err != nil || slot.present {
		_ = a.abortDefinition(transaction)
		return errTarget50Transaction
	}
	// Staging lives only in the owner transaction. Publish the inline payload,
	// clear staging, then publish presence last with no later failure.
	slot.cell = transaction.cell
	a.transaction = target50Transaction{}
	slot.present = true
	return nil
}

func (a *target50Arena) collect() error {
	if a.transaction.id != 0 {
		return errTarget50TransactionActive
	}
	a.clearMarks()
	for index := uint8(0); index < a.rootCount; index++ {
		if a.roots[index] != 0 {
			if err := a.markObject(a.roots[index]); err != nil {
				a.clearMarks()
				return err
			}
		}
	}
	marked := uint8(0)
	for a.markCount != 0 {
		a.markCount--
		reference := a.marks[a.markCount]
		a.marks[a.markCount] = 0
		marked++
		if _, err := a.resolveObject(reference); err != nil {
			a.clearMarks()
			return err
		}
	}
	for index := range a.objects {
		if a.objects[index].live && !a.objects[index].marked {
			a.reclaimObject(uint8(index + 1))
		}
	}
	a.clearMarks()
	a.stats.LastMarkWork = marked
	a.stats.TotalMarkWork += uint64(marked)
	a.stats.Collections++
	if marked > a.stats.MarkHighWater {
		a.stats.MarkHighWater = marked
	}
	return nil
}

func (a *target50Arena) markObject(reference target50ObjectRef) error {
	slot, err := a.resolveObject(reference)
	if err != nil {
		return err
	}
	if slot.marked {
		return nil
	}
	if int(a.markCount) == len(a.marks) {
		return errTarget50Capacity
	}
	slot.marked = true
	a.marks[a.markCount] = reference
	a.markCount++
	return nil
}

func (a *target50Arena) clearMarks() {
	for index := range a.objects {
		a.objects[index].marked = false
	}
	a.marks = [target50MarkCapacity]target50ObjectRef{}
	a.markCount = 0
}

func (a *target50Arena) reclaimObject(index uint8) {
	slot := &a.objects[index-1]
	generation, retired := target50NextGeneration(slot.generation)
	*slot = target50ObjectSlot{generation: generation, retired: retired}
	if !retired {
		a.insertObjectFree(index)
	}
	a.stats.ObjectReclaims++
	a.stats.LiveObjects--
}

func target50NextGeneration(generation uint32) (uint32, bool) {
	if generation == 0 || generation >= target50MaxGeneration {
		return 0, true
	}
	return generation + 1, false
}

func (a *target50Arena) insertObjectFree(index uint8) {
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

type target50Result struct {
	Before, Warm, PeerBefore, First, Second, PeerAfter, PeerAfterReceiverCollection int64
	Stats                                                                           target50Stats
}

type target50Engine struct {
	arena        target50Arena
	ordinaryArm  target50OrdinaryArm
	singletonArm target50WeakArm
	pressure     bool
	busy         bool
	closed       bool
	poisoned     bool
}

func newTarget50Engine() *target50Engine {
	engine := &target50Engine{}
	engine.arena.init()
	return engine
}

func (e *target50Engine) run(ctx context.Context, pressure bool) (target50Result, error) {
	if e == nil || e.closed {
		return target50Result{}, errTarget50Closed
	}
	if e.busy {
		return target50Result{}, errTarget50Busy
	}
	if e.poisoned {
		return target50Result{}, errTarget50Poisoned
	}
	if err := ctx.Err(); err != nil {
		return target50Result{}, err
	}
	e.busy = true
	e.pressure = pressure
	e.arena.init()
	e.ordinaryArm = target50OrdinaryArm{}
	e.singletonArm = target50WeakArm{}
	result, err := e.execute(ctx)
	if err != nil {
		e.poisoned = true
	}
	e.busy = false
	return result, err
}

func (e *target50Engine) execute(ctx context.Context) (target50Result, error) {
	if e.pressure {
		// These exact-H1b-private unreachable objects fill the two slots not
		// needed by peer/receiver. The source GC after publish reclaims them;
		// definition itself cannot trigger collection because it allocates no
		// environment or other heap record.
		for range 2 {
			if _, err := e.arena.allocateObject(); err != nil {
				return target50Result{}, err
			}
		}
	}
	peer, err := e.arena.allocateObject()
	if err != nil {
		return target50Result{}, err
	}
	if err = e.arena.pushRoot(peer); err != nil {
		return target50Result{}, err
	}
	receiver, err := e.arena.allocateObject()
	if err != nil {
		return target50Result{}, err
	}
	if err = e.arena.pushRoot(receiver); err != nil {
		return target50Result{}, err
	}
	before, err := e.readLabel(receiver)
	if err != nil {
		return target50Result{}, err
	}
	warm, err := e.readLabel(receiver)
	if err != nil {
		return target50Result{}, err
	}
	peerBefore, err := e.readLabel(peer)
	if err != nil {
		return target50Result{}, err
	}

	e.arena.roots[1] = 0
	helperMarker := e.arena.rootCount
	if err = e.arena.pushRoot(receiver); err != nil {
		return target50Result{}, err
	}
	transaction, err := e.arena.beginDefinitionAtSafePoint(ctx, receiver, 40)
	if err != nil {
		return target50Result{}, err
	}
	if err = e.arena.commitDefinition(transaction); err != nil {
		return target50Result{}, err
	}
	if err = e.arena.collect(); err != nil {
		return target50Result{}, err
	}
	receiverSlot, err := e.arena.resolveObject(receiver)
	if err != nil || !receiverSlot.present {
		return target50Result{}, errTarget50InvalidReference
	}
	receiverSlot.cell += 2
	e.arena.roots[1] = receiver
	if err = e.arena.popRoots(helperMarker); err != nil {
		return target50Result{}, err
	}

	first, err := e.readLabel(receiver)
	if err != nil {
		return target50Result{}, err
	}
	second, err := e.readLabel(receiver)
	if err != nil {
		return target50Result{}, err
	}
	peerAfter, err := e.readLabel(peer)
	if err != nil {
		return target50Result{}, err
	}
	if err = e.arena.popRoots(1); err != nil {
		return target50Result{}, err
	}
	if err = e.arena.collect(); err != nil {
		return target50Result{}, err
	}
	peerAfterCollection, err := e.readLabel(peer)
	if err != nil {
		return target50Result{}, err
	}
	if err = e.arena.popRoots(0); err != nil {
		return target50Result{}, err
	}
	if err = e.arena.collect(); err != nil {
		return target50Result{}, err
	}
	return target50Result{
		Before: before, Warm: warm, PeerBefore: peerBefore, First: first,
		Second: second, PeerAfter: peerAfter, PeerAfterReceiverCollection: peerAfterCollection,
		Stats: e.arena.stats,
	}, nil
}

func (e *target50Engine) readLabel(reference target50ObjectRef) (int64, error) {
	object, err := e.arena.resolveObject(reference)
	if err != nil {
		return 0, err
	}
	if e.singletonArm.object != 0 && object.present && e.singletonArm.object == reference {
		object.cell++
		return object.cell, nil
	}
	if e.ordinaryArm.valid {
		if !object.present && object.dispatch == e.ordinaryArm.dispatch && object.shape == e.ordinaryArm.shape {
			e.ordinaryArm.hits++
			return e.ordinaryArm.value, nil
		}
		e.ordinaryArm.misses++
	}
	if !object.present {
		e.ordinaryArm = target50OrdinaryArm{
			dispatch: object.dispatch, shape: object.shape, value: 7, valid: true,
			hits: e.ordinaryArm.hits, misses: e.ordinaryArm.misses,
		}
		return 7, nil
	}
	if e.singletonArm.object != reference {
		e.singletonArm = target50WeakArm{object: reference}
	}
	object.cell++
	return object.cell, nil
}

func (e *target50Engine) close() error {
	if e == nil || e.closed {
		return nil
	}
	if e.busy {
		return errTarget50Busy
	}
	*e = target50Engine{closed: true}
	return nil
}

func assertTarget50Result(t testing.TB, got target50Result) {
	t.Helper()
	if got.Before != 7 || got.Warm != 7 || got.PeerBefore != 7 || got.First != 43 ||
		got.Second != 44 || got.PeerAfter != 7 || got.PeerAfterReceiverCollection != 7 {
		t.Fatalf("inline result = %#v, want 7/7/7/43/44/7/7", got)
	}
}

func TestTarget50InlineCellMatchesExactH1bAndReducesGraphWork(t *testing.T) {
	for _, pressure := range []bool{false, true} {
		name := "normal"
		if pressure {
			name = "pressure"
		}
		t.Run(name, func(t *testing.T) {
			candidate := newTarget50Engine()
			got, err := candidate.run(context.Background(), pressure)
			if err != nil {
				t.Fatal(err)
			}
			assertTarget50Result(t, got)
			want := target50Stats{
				ObjectAllocations: 2, ObjectReclaims: 2, Collections: 3,
				TotalMarkWork: 3, MarkHighWater: 2,
			}
			if pressure {
				want.ObjectAllocations, want.ObjectReclaims = 4, 4
			}
			if got.Stats != want {
				t.Fatalf("inline stats = %#v, want %#v", got.Stats, want)
			}
			if !candidate.ordinaryArm.valid || candidate.ordinaryArm.hits != 4 || candidate.ordinaryArm.misses != 1 {
				t.Fatalf("inline ordinary PIC = %#v", candidate.ordinaryArm)
			}
			if candidate.singletonArm.object == 0 {
				t.Fatal("inline singleton PIC was not populated")
			}
			if _, err := candidate.arena.resolveObject(candidate.singletonArm.object); !errors.Is(err, errTarget50InvalidReference) {
				t.Fatalf("inline weak arm retained reclaimed object: %v", err)
			}

			current := NewEngine()
			current.forceEnvironmentPressure = pressure
			baseline, err := current.Run(context.Background(), ProofLimits())
			if err != nil {
				t.Fatal(err)
			}
			assertProofResult(t, baseline)
			if baseline.Before != got.Before || baseline.Warm != got.Warm || baseline.PeerBefore != got.PeerBefore ||
				baseline.First != got.First || baseline.Second != got.Second || baseline.PeerAfter != got.PeerAfter ||
				baseline.PeerAfterReceiverCollection != got.PeerAfterReceiverCollection {
				t.Fatalf("baseline/candidate result mismatch: %#v / %#v", baseline, got)
			}
			if got.Stats.TotalMarkWork >= baseline.Stats.TotalMarkWork || got.Stats.MarkHighWater >= baseline.Stats.MarkHighWater {
				t.Fatalf("inline graph work %d/%d did not beat baseline %d/%d", got.Stats.TotalMarkWork, got.Stats.MarkHighWater, baseline.Stats.TotalMarkWork, baseline.Stats.MarkHighWater)
			}
		})
	}
}

func TestTarget50InlineCellTransactionCancellationABAAndRetirement(t *testing.T) {
	var arena target50Arena
	arena.init()
	object, err := arena.allocateObject()
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := arena.beginDefinitionAtSafePoint(cancelled, object, 40); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled begin = %v", err)
	}
	slot, _ := arena.resolveObject(object)
	if slot.present || slot.cell != 0 || arena.transaction.id != 0 {
		t.Fatalf("cancelled begin published state: %#v/%#v", slot, arena.transaction)
	}
	transaction, err := arena.beginDefinition(object, 40)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.collect(); !errors.Is(err, errTarget50TransactionActive) {
		t.Fatalf("collection during transaction = %v", err)
	}
	if err := arena.abortDefinition(transaction); err != nil {
		t.Fatal(err)
	}
	if slot.present || slot.cell != 0 || arena.transaction.id != 0 {
		t.Fatalf("abort published state: %#v/%#v", slot, arena.transaction)
	}
	transaction, err = arena.beginDefinition(object, 40)
	if err != nil || arena.commitDefinition(transaction) != nil {
		t.Fatalf("retry commit = %#v, %v", transaction, err)
	}
	oldArm := target50WeakArm{object: object}
	if err := arena.collect(); err != nil {
		t.Fatal(err)
	}
	reused, err := arena.allocateObject()
	if err != nil {
		t.Fatal(err)
	}
	oldIndex, _, _ := target50Unpack(uint32(object))
	newIndex, _, _ := target50Unpack(uint32(reused))
	if oldIndex != newIndex || object == reused || oldArm.object == reused {
		t.Fatalf("ABA reuse old=%#x new=%#x arm=%#v", object, reused, oldArm)
	}
	reusedSlot, _ := arena.resolveObject(reused)
	if reusedSlot.present || reusedSlot.cell != 0 {
		t.Fatalf("reused object retained inline definition: %#v", reusedSlot)
	}

	var retiring target50Arena
	retiring.init()
	retiring.objects[0].generation = target50MaxGeneration
	old, err := retiring.allocateObject()
	if err != nil {
		t.Fatal(err)
	}
	if err := retiring.collect(); err != nil {
		t.Fatal(err)
	}
	if !retiring.objects[0].retired || retiring.objects[0].generation != 0 || retiring.objectFree != 2 {
		t.Fatalf("retired slot/free list = %#v/%d", retiring.objects[0], retiring.objectFree)
	}
	if _, err := retiring.resolveObject(old); !errors.Is(err, errTarget50InvalidReference) {
		t.Fatalf("retired reference resolved: %v", err)
	}
}

func TestTarget50InlineCellOwnerIsSmallerAndHasNoEnvironmentState(t *testing.T) {
	currentOwner, candidateOwner := unsafe.Sizeof(Engine{}), unsafe.Sizeof(target50Engine{})
	currentArena, candidateArena := unsafe.Sizeof(arena{}), unsafe.Sizeof(target50Arena{})
	if candidateOwner >= currentOwner || candidateArena >= currentArena {
		t.Fatalf("inline owner/arena = %d/%d, want below environment owner/arena %d/%d", candidateOwner, candidateArena, currentOwner, currentArena)
	}
	if unsafe.Sizeof(target50WeakArm{}) >= unsafe.Sizeof(weakArm{}) {
		t.Fatalf("inline weak arm = %d, baseline = %d", unsafe.Sizeof(target50WeakArm{}), unsafe.Sizeof(weakArm{}))
	}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(target50ObjectSlot{}), reflect.TypeOf(target50Transaction{}),
		reflect.TypeOf(target50WeakArm{}), reflect.TypeOf(target50Arena{}), reflect.TypeOf(target50Engine{}),
	} {
		for index := 0; index < typ.NumField(); index++ {
			if strings.Contains(strings.ToLower(typ.Field(index).Name), "environment") {
				t.Fatalf("candidate type %s retains environment field %s", typ, typ.Field(index).Name)
			}
		}
	}
	t.Logf("Target50 64-bit structural result: owner %d -> %d bytes; arena %d -> %d; weak arm %d -> %d; max observed mark work 3 -> 2",
		currentOwner, candidateOwner, currentArena, candidateArena, unsafe.Sizeof(weakArm{}), unsafe.Sizeof(target50WeakArm{}))
}

func TestTarget50InlineCellRunsAllocateNoGoHeapAfterConstruction(t *testing.T) {
	ctx := context.Background()
	for _, pressure := range []bool{false, true} {
		engine := newTarget50Engine()
		var got target50Result
		var runErr error
		allocations := testing.AllocsPerRun(100, func() {
			if runErr == nil {
				got, runErr = engine.run(ctx, pressure)
			}
		})
		if runErr != nil {
			t.Fatal(runErr)
		}
		assertTarget50Result(t, got)
		if allocations != 0 {
			t.Fatalf("pressure=%v allocations = %g, want 0", pressure, allocations)
		}
	}
}

func TestTarget50InlineCellCloseAndFrozenArtifact(t *testing.T) {
	engine := newTarget50Engine()
	engine.busy = true
	if err := engine.close(); !errors.Is(err, errTarget50Busy) {
		t.Fatalf("busy close = %v", err)
	}
	engine.busy = false
	if err := engine.close(); err != nil {
		t.Fatal(err)
	}
	if err := engine.close(); err != nil {
		t.Fatalf("idempotent close = %v", err)
	}
	if _, err := engine.run(context.Background(), false); !errors.Is(err, errTarget50Closed) {
		t.Fatalf("closed run = %v", err)
	}

	generated, err := os.ReadFile("ruby_h1b_generated.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(generated)); got != target48GeneratedSHA256 {
		t.Fatalf("generated source SHA-256 = %s, want %s", got, target48GeneratedSHA256)
	}
}
