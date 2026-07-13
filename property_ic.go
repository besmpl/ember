package ember

const (
	propertyICEmpty uint8 = iota
	propertyICPresent
)

// propertyIC is the compact monomorphic cache for compiler-known string
// fields. The opcode site already owns the key constant, so the mutable entry
// needs only a shared shape guard and the stable property offset. It never
// retains a receiver table.
type propertyIC struct {
	shape  *tableShape
	offset uint16
	state  uint8
	_      [5]byte
}

func (cache *propertyIC) install(shape *tableShape, offset uint16) {
	if cache == nil || !shape.cacheable() {
		return
	}
	cache.shape = shape
	cache.offset = offset
	cache.state = propertyICPresent
}

func (cache *propertyIC) observe(table *Table, key string, box *stringBox) {
	if cache == nil || table == nil {
		return
	}
	shape := table.currentShape()
	offset, ok := table.shapedStringFieldOffset(key, box)
	if !ok {
		return
	}
	cache.install(shape, offset)
}

func (cache *propertyIC) get(table *Table) (Value, bool) {
	if cache == nil || cache.state != propertyICPresent || table == nil || table.currentShape() != cache.shape {
		return NilValue(), false
	}
	return table.shapedStringFieldValue(cache.offset)
}

func (cache *propertyIC) getCounted(table *Table, counts *directFramePICCounts) (Value, bool) {
	value, ok := cache.get(table)
	if ok {
		// This counter historically meant an interned key-pointer hit. A shape
		// pointer hit is strictly stronger and preserves the existing diagnostic
		// contract for warmed constant fields.
		counts.addPointerHit()
		counts.addHit(0)
	}
	return value, ok
}

func (cache *propertyIC) resolve(table *Table, key string, box *stringBox) (Value, bool) {
	if table == nil {
		return NilValue(), false
	}
	shape := table.currentShape()
	if !shape.cacheable() {
		return NilValue(), false
	}
	offset, ok := table.shapedStringFieldOffset(key, box)
	if !ok {
		return NilValue(), false
	}
	value, ok := table.shapedStringFieldValue(offset)
	if !ok {
		return NilValue(), false
	}
	cache.install(shape, offset)
	return value, true
}

func (cache *propertyIC) resolveCounted(table *Table, key string, box *stringBox, counts *directFramePICCounts) (Value, bool) {
	if cache != nil && cache.state == propertyICPresent && table != nil && table.currentShape() != cache.shape {
		counts.addShapeMiss()
	}
	return cache.resolve(table, key, box)
}

func (cache *propertyIC) write(table *Table, value Value) bool {
	if cache == nil || cache.state != propertyICPresent || table == nil || table.currentShape() != cache.shape {
		return false
	}
	return table.setExistingShapedStringField(cache.offset, value)
}

func (cache *propertyIC) writeCounted(table *Table, value Value, counts *directFramePICCounts) bool {
	if value.IsNil() {
		return false
	}
	if cache.write(table, value) {
		counts.addPointerHit()
		counts.addHit(0)
		return true
	}
	return false
}

func (cache *propertyIC) append(table *Table, key string, box *stringBox, value Value) bool {
	if cache == nil || cache.state != propertyICPresent || cache.shape == nil {
		return false
	}
	return table.appendShapedStringFieldBox(cache.shape, key, box, value)
}

func (cache *propertyIC) resolveExistingWrite(table *Table, key string, box *stringBox, value Value) bool {
	if table == nil || value.IsNil() {
		return false
	}
	shape := table.currentShape()
	if !shape.cacheable() {
		return false
	}
	offset, ok := table.shapedStringFieldOffset(key, box)
	if !ok || !table.setExistingShapedStringField(offset, value) {
		return false
	}
	cache.install(shape, offset)
	return true
}

func (cache *propertyIC) resolveWrite(table *Table, key string, box *stringBox, value Value, controller *executionController) bool {
	if table == nil || table.checkEntryQuotaWithController(controller, StringValue(key), value) != nil {
		return false
	}
	if cache.append(table, key, box, value) {
		table.noteEntryMutation(StringValue(key), value, false)
		return true
	}
	return cache.resolveExistingWrite(table, key, box, value)
}

func (cache *propertyIC) resolveWriteCounted(table *Table, key string, box *stringBox, value Value, counts *directFramePICCounts, controller *executionController) bool {
	if table == nil || table.checkEntryQuotaWithController(controller, StringValue(key), value) != nil {
		return false
	}
	if cache.append(table, key, box, value) {
		table.noteEntryMutation(StringValue(key), value, false)
		counts.addPointerHit()
		counts.addHit(0)
		return true
	}
	if cache != nil && cache.state == propertyICPresent && table != nil && table.currentShape() != cache.shape {
		counts.addShapeMiss()
	}
	return cache.resolveExistingWrite(table, key, box, value)
}
