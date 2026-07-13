package ember

import (
	"sync"
	"sync/atomic"
)

const (
	// String properties beyond the inline field family stay in the generic hash
	// sidecar.  Keeping the shape graph aligned with inline storage makes a
	// shape/offset guard sufficient for the hottest constant-field path.
	maxTableShapeProperties = maxInlineStringFields
	// The default domain is deliberately bounded.  Once adversarial or highly
	// dynamic code fills it, new layouts use the uncacheable dynamic sentinel
	// rather than retaining arbitrary strings forever.
	maxDefaultTableShapeTransitions = 16 * 1024
)

const (
	tableShapeFlagDynamic uint8 = 1 << iota
)

// tableShape has immutable property identity plus an atomically published
// transition list. Each node adds one string
// property at a stable inline-field offset.  Tables with the same insertion
// order share the same shape pointer, so a cache hit does not retain or compare
// a receiver table.
type tableShape struct {
	parent        *tableShape
	key           string
	hash          uint64
	propertyCount uint16
	addedOffset   uint16
	flags         uint8
	_             [7]byte
	transitions   atomic.Pointer[tableShapeTransition]
}

type tableShapeTransition struct {
	next  *tableShapeTransition
	shape *tableShape
}

func (shape *tableShape) cacheable() bool {
	return shape != nil && shape.flags&tableShapeFlagDynamic == 0
}

func (shape *tableShape) propertyOffset(key string, hash uint64) (uint16, bool) {
	for current := shape; current != nil && current.parent != nil; current = current.parent {
		if current.hash == hash && len(current.key) == len(key) && current.key == key {
			return current.addedOffset, true
		}
	}
	return 0, false
}

type tableShapeDomain struct {
	mu              sync.Mutex
	root            *tableShape
	dynamic         *tableShape
	transitionCount int
}

func newTableShapeDomain() *tableShapeDomain {
	root := &tableShape{}
	return &tableShapeDomain{
		root: root,
		dynamic: &tableShape{
			flags: tableShapeFlagDynamic,
		},
	}
}

var defaultTableShapes = newTableShapeDomain()

func tableShapeTransitionFor(parent *tableShape, key string, hash uint64) *tableShape {
	if parent == nil {
		return nil
	}
	for transition := parent.transitions.Load(); transition != nil; transition = transition.next {
		shape := transition.shape
		if shape != nil && shape.hash == hash && len(shape.key) == len(key) && shape.key == key {
			return shape
		}
	}
	return nil
}

func (domain *tableShapeDomain) transition(parent *tableShape, key string, hash uint64) *tableShape {
	if domain == nil {
		return nil
	}
	if parent == nil {
		parent = domain.root
	}
	if !parent.cacheable() || parent.propertyCount >= maxTableShapeProperties {
		return domain.dynamic
	}
	// Existing transitions are immutable and published atomically. Once a
	// layout has warmed, table literals pay only a short pointer walk and no
	// mutex or map lookup.
	if shape := tableShapeTransitionFor(parent, key, hash); shape != nil {
		return shape
	}

	domain.mu.Lock()
	defer domain.mu.Unlock()
	// Another goroutine may have created the transition while we waited.
	if shape := tableShapeTransitionFor(parent, key, hash); shape != nil {
		return shape
	}
	if domain.transitionCount >= maxDefaultTableShapeTransitions {
		return domain.dynamic
	}
	shape := &tableShape{
		parent:        parent,
		key:           key,
		hash:          hash,
		propertyCount: parent.propertyCount + 1,
		addedOffset:   parent.propertyCount,
	}
	transition := &tableShapeTransition{
		next:  parent.transitions.Load(),
		shape: shape,
	}
	parent.transitions.Store(transition)
	domain.transitionCount++
	return shape
}

func (t *Table) currentShape() *tableShape {
	if t == nil {
		return nil
	}
	if t.shape == nil {
		return defaultTableShapes.root
	}
	return t.shape
}

func (t *Table) resetShape() {
	if t != nil {
		t.shape = defaultTableShapes.root
	}
}

func (t *Table) publishAppendedStringShape(key string, box *stringBox, offset int) {
	if t == nil {
		return
	}
	parent := t.currentShape()
	if parent == nil || !parent.cacheable() || offset < 0 || offset != int(parent.propertyCount) {
		t.shape = defaultTableShapes.dynamic
		return
	}
	hash := stringBoxHash(box, key)
	next := defaultTableShapes.transition(parent, key, hash)
	if next == nil || !next.cacheable() || int(next.addedOffset) != offset {
		t.shape = defaultTableShapes.dynamic
		return
	}
	// The field has already been appended and initialized before this publish.
	// A visible shape therefore always implies that its offset is addressable.
	t.shape = next
}

// appendShapedStringFieldBox applies a previously observed hidden-class
// transition without consulting the shared transition graph or scanning the
// existing fields. The cache owns the key site; the child shape proves the
// parent layout and append offset.
func (t *Table) appendShapedStringFieldBox(next *tableShape, key string, box *stringBox, value Value) bool {
	if t == nil || value.IsNil() || t.metatable != nil || next == nil || !next.cacheable() || next.parent == nil {
		return false
	}
	parent := t.currentShape()
	hash := stringBoxHash(box, key)
	if parent != next.parent ||
		int(parent.propertyCount) != len(t.stringFields) ||
		int(next.addedOffset) != len(t.stringFields) ||
		len(t.stringFields) >= maxInlineStringFields ||
		t.hasStringOverflow() ||
		next.hash != hash || len(next.key) != len(key) || next.key != key {
		return false
	}
	// A shape transition can only add a key absent from its parent. Rechecking
	// the parent chain here would turn the warmed literal path back into an
	// O(property-count) operation.
	if t.iteration == nil && t.cold != nil && t.hashFieldCount() != 0 {
		t.ensureIterationJournal()
	}
	if t.stringFields == nil {
		t.stringFields = tableInlineFields(t)[:0]
	}
	t.stringFields = append(t.stringFields, tableStringField{key: key, box: box, value: value})
	if t.iteration != nil {
		t.markIterationKeyPresent(tableKey{kind: StringKind, str: key, strBox: box, strHash: hash})
	}
	// Publish only after the initialized slot is addressable.
	t.shape = next
	t.stringVersion++
	t.stringValueVersion++
	return true
}

func (t *Table) shapedStringFieldOffset(key string, box *stringBox) (uint16, bool) {
	if t == nil {
		return 0, false
	}
	shape := t.currentShape()
	if !shape.cacheable() {
		return 0, false
	}
	hash := stringBoxHash(box, key)
	offset, ok := shape.propertyOffset(key, hash)
	if !ok || int(offset) >= len(t.stringFields) {
		return 0, false
	}
	field := &t.stringFields[offset]
	probe := tableStringProbe{box: box, text: key, hash: hash}
	if !probe.matchesBox(field.box, field.key) {
		return 0, false
	}
	return offset, true
}

func (t *Table) shapedStringFieldValue(offset uint16) (Value, bool) {
	if t == nil || int(offset) >= len(t.stringFields) {
		return NilValue(), false
	}
	value := t.stringFields[offset].value
	if value.IsNil() {
		return NilValue(), false
	}
	return value, true
}

func (t *Table) setExistingShapedStringField(offset uint16, value Value) bool {
	if t == nil || value.IsNil() || int(offset) >= len(t.stringFields) {
		return false
	}
	field := &t.stringFields[offset]
	if field.value.IsNil() {
		return false
	}
	field.value = value
	t.stringValueVersion++
	return true
}

func (t *Table) needsDynamicStringFieldCache() bool {
	if t == nil {
		return true
	}
	shape := t.currentShape()
	return !shape.cacheable() || int(shape.propertyCount) != len(t.stringFields) || t.hasStringOverflow()
}
