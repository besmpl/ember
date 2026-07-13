package ember

import "fmt"

type rawSequence struct {
	table      *Table
	label      string
	controller *executionController
}

func newRawSequenceWithController(label string, table *Table, controller *executionController) rawSequence {
	return rawSequence{table: table, label: label, controller: controller}
}

func newRawSequence(label string, table *Table) rawSequence {
	return rawSequence{
		table: table,
		label: label,
	}
}

func (s rawSequence) len() (int, error) {
	length, err := s.table.rawLen()
	if err != nil {
		return 0, fmt.Errorf("%s: %w", s.label, err)
	}
	return length, nil
}

func (s rawSequence) get(index int) (Value, error) {
	value, err := s.table.rawGet(NumberValue(float64(index)))
	if err != nil {
		return NilValue(), fmt.Errorf("%s: %w", s.label, err)
	}
	return value, nil
}

func (s rawSequence) set(index int, value Value) error {
	if err := s.table.rawSetWithController(s.controller, NumberValue(float64(index)), value); err != nil {
		return fmt.Errorf("%s: %w", s.label, err)
	}
	return nil
}

func (s rawSequence) values(start int, end int) ([]Value, error) {
	if end < start {
		return nil, nil
	}
	values := make([]Value, 0, end-start+1)
	for index := start; index <= end; index++ {
		value, err := s.get(index)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func (s rawSequence) insert(position int, value Value) error {
	length, err := s.len()
	if err != nil {
		return err
	}
	if position < 1 || position > length+1 {
		return fmt.Errorf("%s: position %d out of range", s.label, position)
	}
	if s.table.canUseFastArraySequence(length) {
		if err := s.table.checkEntryQuotaWithController(s.controller, NumberValue(float64(position)), value); err != nil {
			return err
		}
		s.table.fastArrayInsert(position, value)
		if !value.IsNil() {
			s.table.entryCount++
		}
		return nil
	}
	for index := length; index >= position; index-- {
		value, err := s.get(index)
		if err != nil {
			return err
		}
		if err := s.set(index+1, value); err != nil {
			return err
		}
	}
	return s.set(position, value)
}

func (s rawSequence) remove(position int) (Value, error) {
	length, err := s.len()
	if err != nil {
		return NilValue(), err
	}
	if length == 0 {
		return NilValue(), nil
	}
	if position < 1 || position > length {
		return NilValue(), nil
	}
	if s.table.canUseFastArraySequence(length) {
		removed := s.table.fastArrayRemove(position)
		if !removed.IsNil() && s.table.entryCount > 0 {
			s.table.entryCount--
		}
		return removed, nil
	}
	removed, err := s.get(position)
	if err != nil {
		return NilValue(), err
	}
	for index := position; index < length; index++ {
		value, err := s.get(index + 1)
		if err != nil {
			return NilValue(), err
		}
		if err := s.set(index, value); err != nil {
			return NilValue(), err
		}
	}
	if err := s.set(length, NilValue()); err != nil {
		return NilValue(), err
	}
	return removed, nil
}

func (s rawSequence) clear() {
	s.table.clearRawStorage()
}

func (s rawSequence) writeValues(values []Value) error {
	for index, value := range values {
		if err := s.set(index+1, value); err != nil {
			return err
		}
	}
	return nil
}

func (t *Table) canUseFastArraySequence(length int) bool {
	return t.canUseFastArrayStorage() && length == len(t.array)
}

func (t *Table) canAppendFastArray() bool {
	return t.canUseFastArrayStorage()
}

func (t *Table) canUseFastArrayStorage() bool {
	return t != nil && !t.arrayHasNil && len(t.stringFields) == 0 && t.hashFieldCount() == 0
}

func (t *Table) fastArrayAppend(value Value) {
	t.growFastArray(1)
	t.array[len(t.array)-1] = value
	if value.IsNil() {
		t.arrayHasNil = true
	}
}

func (t *Table) fastArrayAppendWithController(controller *executionController, value Value) error {
	if err := t.checkEntryQuotaWithController(controller, NumberValue(float64(len(t.array)+1)), value); err != nil {
		return err
	}
	t.fastArrayAppend(value)
	t.noteEntryMutation(NumberValue(float64(len(t.array))), value, false)
	return nil
}

func (t *Table) fastArrayInsert(position int, value Value) {
	index := position - 1
	t.growFastArray(1)
	copy(t.array[index+1:], t.array[index:])
	t.array[index] = value
	if value.IsNil() {
		t.arrayHasNil = true
	}
}

func (t *Table) fastArrayInsertWithController(controller *executionController, position int, value Value) error {
	if err := t.checkEntryQuotaWithController(controller, NumberValue(float64(position)), value); err != nil {
		return err
	}
	t.fastArrayInsert(position, value)
	if !value.IsNil() {
		t.entryCount++
	}
	return nil
}

func (t *Table) growFastArray(extra int) {
	needed := len(t.array) + extra
	if needed <= cap(t.array) {
		t.array = t.array[:needed]
		return
	}
	nextCap := cap(t.array) * 2
	if nextCap < 96 {
		nextCap = 96
	}
	if nextCap < needed {
		nextCap = needed
	}
	grown := make([]Value, needed, nextCap)
	copy(grown, t.array)
	t.array = grown
}

func (t *Table) fastArrayRemove(position int) Value {
	index := position - 1
	removed := t.array[index]
	if index == 0 {
		t.array[0] = Value{}
		t.array = t.array[1:]
		return removed
	}
	if index == len(t.array)-1 {
		t.array[index] = Value{}
		t.array = t.array[:index]
		return removed
	}
	copy(t.array[index:], t.array[index+1:])
	t.array[len(t.array)-1] = Value{}
	t.array = t.array[:len(t.array)-1]
	return removed
}
