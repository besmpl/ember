package generated

import "context"

type target37EquivalentError string

func (e target37EquivalentError) Error() string { return string(e) }

const (
	target37EquivalentInternal   target37EquivalentError = "target37: internal"
	target37EquivalentNoMethod   target37EquivalentError = "target37: NoMethodError"
	target37EquivalentStepLimit  target37EquivalentError = "target37: step limit"
	target37EquivalentFrameLimit target37EquivalentError = "target37: frame limit"
)

type target37EquivalentVisibility uint8

const (
	target37EquivalentPublic target37EquivalentVisibility = iota + 1
	target37EquivalentProtected
	target37EquivalentPrivate
)

type target37EquivalentOutcome uint8

const (
	target37EquivalentMissing target37EquivalentOutcome = iota + 1
	target37EquivalentResolved
	target37EquivalentUndef
)

type target37EquivalentEntryKind uint8

const (
	target37EquivalentAbsent target37EquivalentEntryKind = iota
	target37EquivalentDefined
	target37EquivalentUndefined
)

type target37EquivalentControl struct {
	ctx                                      context.Context
	steps, frames, objects, depth, nextFrame uint64
}

func (c *target37EquivalentControl) poll() error { return c.ctx.Err() }

func (c *target37EquivalentControl) step() error {
	if err := c.poll(); err != nil {
		return err
	}
	if c.steps == 0 {
		return target37EquivalentStepLimit
	}
	c.steps--
	return nil
}

func (c *target37EquivalentControl) enterFrame() (uint64, error) {
	if err := c.poll(); err != nil {
		return 0, err
	}
	if c.depth >= c.frames {
		return 0, target37EquivalentFrameLimit
	}
	c.depth++
	c.nextFrame++
	return c.nextFrame, nil
}

func (c *target37EquivalentControl) leaveFrame() { c.depth-- }

type target37EquivalentReceipt struct {
	owner, slot, definition uint32
	outcome                 target37EquivalentOutcome
	visibility              target37EquivalentVisibility
}

type target37EquivalentCell struct {
	epoch   uint64
	receipt target37EquivalentReceipt
}

type target37EquivalentEntry struct {
	slot, definition uint32
	kind             target37EquivalentEntryKind
	visibility       target37EquivalentVisibility
}

type target37EquivalentArm struct {
	epoch                                     uint64
	dispatch, shape, slot, definition, target uint32
}

type target37EquivalentLookup struct {
	entries   [8]target37EquivalentEntry
	cells     [6]target37EquivalentCell
	nextEpoch uint64
}

type target37EquivalentSite struct {
	arms [2]target37EquivalentArm
}

type target37EquivalentObject struct {
	id              uint64
	dispatch, shape uint32
	value, trace    int64
}

type target37EquivalentStats struct {
	hits, coldAdmissions, staleMisses, repairs uint64
	uncachedFallbacks, admissions, evictions   uint64
	occupiedArms                               uint64
}

type target37EquivalentEngine struct {
	active, closed, poisoned uint32
	lookup                   target37EquivalentLookup
	labelSite                target37EquivalentSite
	savedSite                target37EquivalentSite
	publicSite               target37EquivalentSite
	hotObject                target37EquivalentObject
	nextObject               uint64
	stats                    target37EquivalentStats
}

func target37NewEquivalentEngine() (*target37EquivalentEngine, target37EquivalentObject, target37EquivalentObject) {
	entries := [8]target37EquivalentEntry{
		0: {slot: 3, definition: 3, kind: target37EquivalentDefined, visibility: target37EquivalentPublic},
		1: {slot: 4, definition: 4, kind: target37EquivalentDefined, visibility: target37EquivalentPublic},
		4: {slot: 10, definition: 10, kind: target37EquivalentDefined, visibility: target37EquivalentPublic},
		6: {slot: 12, definition: 12, kind: target37EquivalentDefined, visibility: target37EquivalentPublic},
	}
	engine := &target37EquivalentEngine{
		lookup: target37EquivalentLookup{entries: entries, nextEpoch: 6},
		hotObject: target37EquivalentObject{
			id: 1, dispatch: 1, shape: 1, value: 4,
		},
		nextObject: 1,
	}
	for dispatch := uint32(1); dispatch <= 3; dispatch++ {
		label, ok := engine.resolve(dispatch, false)
		if !ok {
			panic("target37: invalid equivalent label setup")
		}
		saved, ok := engine.resolve(dispatch, true)
		if !ok {
			panic("target37: invalid equivalent saved setup")
		}
		base := int(dispatch-1) * 2
		engine.lookup.cells[base] = target37EquivalentCell{epoch: uint64(base + 1), receipt: label}
		engine.lookup.cells[base+1] = target37EquivalentCell{epoch: uint64(base + 2), receipt: saved}
	}
	return engine,
		target37EquivalentObject{id: 1, dispatch: 1, shape: 1, value: 4},
		target37EquivalentObject{id: 2, dispatch: 2, shape: 2, value: 4}
}

func target37EquivalentVisibilityValid(visibility target37EquivalentVisibility) bool {
	return visibility >= target37EquivalentPublic && visibility <= target37EquivalentPrivate
}

func target37EquivalentSuperclass(node uint32) uint32 {
	if node >= 2 && node <= 4 {
		return 1
	}
	return 0
}

func target37EquivalentKnownSlot(node uint32, saved bool, slot uint32) bool {
	if slot == 0 {
		return false
	}
	if saved {
		return node == 1 && slot == 4
	}
	return node == 1 && slot == 3 || node == 3 && slot == 10 || node == 4 && slot == 12
}

func target37EquivalentEntryValid(node uint32, saved bool, entry target37EquivalentEntry) bool {
	switch entry.kind {
	case target37EquivalentAbsent:
		return entry.definition == 0 && entry.visibility == 0 && (entry.slot == 0 || target37EquivalentKnownSlot(node, saved, entry.slot))
	case target37EquivalentDefined:
		if !target37EquivalentVisibilityValid(entry.visibility) || !target37EquivalentKnownSlot(node, saved, entry.slot) {
			return false
		}
		if saved {
			return node == 1 && entry.slot == 4 && entry.definition == 4
		}
		return node == 1 && entry.slot == 3 && (entry.definition == 3 || entry.definition == 13) ||
			node == 3 && entry.slot == 10 && entry.definition == 10 ||
			node == 4 && entry.slot == 12 && entry.definition == 12
	case target37EquivalentUndefined:
		return entry.definition == 0 && entry.visibility == 0 && target37EquivalentKnownSlot(node, saved, entry.slot)
	default:
		return false
	}
}

func (e *target37EquivalentEngine) resolve(dispatch uint32, saved bool) (target37EquivalentReceipt, bool) {
	if dispatch == 0 || dispatch > 3 {
		return target37EquivalentReceipt{}, false
	}
	node := dispatch + 1
	offset := 0
	if saved {
		offset = 1
	}
	for depth := 0; depth < 4; depth++ {
		if node == 0 {
			return target37EquivalentReceipt{outcome: target37EquivalentMissing}, true
		}
		entry := e.lookup.entries[int(node-1)*2+offset]
		if !target37EquivalentEntryValid(node, saved, entry) {
			return target37EquivalentReceipt{}, false
		}
		switch entry.kind {
		case target37EquivalentDefined:
			return target37EquivalentReceipt{
				owner: node, slot: entry.slot, definition: entry.definition,
				outcome: target37EquivalentResolved, visibility: entry.visibility,
			}, true
		case target37EquivalentUndefined:
			return target37EquivalentReceipt{owner: node, slot: entry.slot, outcome: target37EquivalentUndef}, true
		case target37EquivalentAbsent:
			node = target37EquivalentSuperclass(node)
		default:
			return target37EquivalentReceipt{}, false
		}
	}
	return target37EquivalentReceipt{}, false
}

func target37EquivalentArmValid(index int, arm *target37EquivalentArm, nextEpoch uint64) bool {
	if arm.target == 0 {
		return *arm == (target37EquivalentArm{})
	}
	if arm.epoch == 0 || arm.epoch > nextEpoch {
		return false
	}
	switch index {
	case 0:
		if arm.dispatch != 1 || arm.shape != 1 {
			return false
		}
		switch arm.target {
		case 1:
			return arm.slot == 3 && arm.definition == 3
		case 4:
			return arm.slot == 3 && arm.definition == 13
		}
	case 1:
		if arm.dispatch != 2 || arm.shape != 2 {
			return false
		}
		switch arm.target {
		case 2:
			return arm.slot == 10 && arm.definition == 10
		case 4:
			return arm.slot == 3 && arm.definition == 13
		}
	}
	return false
}

func target37EquivalentSiteValid(site *target37EquivalentSite, nextEpoch uint64) bool {
	return site != nil && target37EquivalentArmValid(0, &site.arms[0], nextEpoch) && target37EquivalentArmValid(1, &site.arms[1], nextEpoch)
}

func target37EquivalentAlphaCellValid(cell *target37EquivalentCell, nextEpoch uint64) bool {
	if cell.epoch == 0 || cell.epoch > nextEpoch || cell.receipt.outcome != target37EquivalentResolved || !target37EquivalentVisibilityValid(cell.receipt.visibility) {
		return false
	}
	return cell.receipt.owner == 1 && cell.receipt.slot == 3 && (cell.receipt.definition == 3 || cell.receipt.definition == 13)
}

func target37EquivalentBetaCellValid(cell *target37EquivalentCell, nextEpoch uint64) bool {
	if cell.epoch == 0 || cell.epoch > nextEpoch || cell.receipt.outcome != target37EquivalentResolved || !target37EquivalentVisibilityValid(cell.receipt.visibility) {
		return false
	}
	return cell.receipt.owner == 3 && cell.receipt.slot == 10 && cell.receipt.definition == 10 ||
		cell.receipt.owner == 1 && cell.receipt.slot == 3 && cell.receipt.definition == 13
}

func target37EquivalentHits(arm *target37EquivalentArm, object *target37EquivalentObject, cell *target37EquivalentCell) bool {
	return arm.target != 0 && arm.dispatch == object.dispatch && arm.shape == object.shape &&
		arm.epoch == cell.epoch && arm.slot == cell.receipt.slot && arm.definition == cell.receipt.definition
}

func (e *target37EquivalentEngine) targetFor(object *target37EquivalentObject, receipt target37EquivalentReceipt) (uint32, bool) {
	if receipt.outcome != target37EquivalentResolved || !target37EquivalentVisibilityValid(receipt.visibility) {
		return 0, false
	}
	switch object.dispatch {
	case 1:
		if object.shape == 1 && receipt.owner == 1 && receipt.slot == 3 {
			switch receipt.definition {
			case 3:
				return 1, true
			case 13:
				return 4, true
			}
		}
	case 2:
		if object.shape == 2 {
			if receipt.owner == 3 && receipt.slot == 10 && receipt.definition == 10 {
				return 2, true
			}
			if receipt.owner == 1 && receipt.slot == 3 && receipt.definition == 13 {
				return 4, true
			}
		}
	case 3:
		if object.shape == 3 && receipt.owner == 4 && receipt.slot == 12 && receipt.definition == 12 {
			return 3, true
		}
	}
	return 0, false
}

func (e *target37EquivalentEngine) sendAfterStep(c *target37EquivalentControl, object *target37EquivalentObject) (int64, error) {
	var err error
	nextEpoch := e.lookup.nextEpoch
	switch object.dispatch {
	case 1:
		cell, arm := &e.lookup.cells[0], &e.labelSite.arms[0]
		if object.shape == 1 && target37EquivalentAlphaCellValid(cell, nextEpoch) && target37EquivalentSiteValid(&e.labelSite, nextEpoch) && target37EquivalentHits(arm, object, cell) {
			var value int64
			switch arm.target {
			case 1:
				value, err = e.labelAlpha(c, object)
			case 4:
				value, err = e.labelRedefined(c, object)
			default:
				return 0, target37EquivalentInternal
			}
			if err == nil {
				e.stats.hits++
			}
			return value, err
		}
	case 2:
		cell, arm := &e.lookup.cells[2], &e.labelSite.arms[1]
		if object.shape == 2 && target37EquivalentBetaCellValid(cell, nextEpoch) && target37EquivalentSiteValid(&e.labelSite, nextEpoch) && target37EquivalentHits(arm, object, cell) {
			var value int64
			switch arm.target {
			case 2:
				value, err = e.labelBeta(c, object)
			case 4:
				value, err = e.labelRedefined(c, object)
			default:
				return 0, target37EquivalentInternal
			}
			if err == nil {
				e.stats.hits++
			}
			return value, err
		}
	}
	return e.recoverAfterStep(c, object, &e.labelSite, false)
}

func (e *target37EquivalentEngine) publicAfterStep(c *target37EquivalentControl, object *target37EquivalentObject) (int64, error) {
	var err error
	nextEpoch := e.lookup.nextEpoch
	switch object.dispatch {
	case 1:
		cell, arm := &e.lookup.cells[0], &e.publicSite.arms[0]
		if object.shape == 1 && target37EquivalentAlphaCellValid(cell, nextEpoch) && target37EquivalentSiteValid(&e.publicSite, nextEpoch) {
			if cell.receipt.visibility != target37EquivalentPublic {
				return 0, target37EquivalentNoMethod
			}
			if target37EquivalentHits(arm, object, cell) {
				var value int64
				switch arm.target {
				case 1:
					value, err = e.labelAlpha(c, object)
				case 4:
					value, err = e.labelRedefined(c, object)
				default:
					return 0, target37EquivalentInternal
				}
				if err == nil {
					e.stats.hits++
				}
				return value, err
			}
		}
	case 2:
		cell, arm := &e.lookup.cells[2], &e.publicSite.arms[1]
		if object.shape == 2 && target37EquivalentBetaCellValid(cell, nextEpoch) && target37EquivalentSiteValid(&e.publicSite, nextEpoch) {
			if cell.receipt.visibility != target37EquivalentPublic {
				return 0, target37EquivalentNoMethod
			}
			if target37EquivalentHits(arm, object, cell) {
				var value int64
				switch arm.target {
				case 2:
					value, err = e.labelBeta(c, object)
				case 4:
					value, err = e.labelRedefined(c, object)
				default:
					return 0, target37EquivalentInternal
				}
				if err == nil {
					e.stats.hits++
				}
				return value, err
			}
		}
	}
	return e.recoverAfterStep(c, object, &e.publicSite, true)
}

func (e *target37EquivalentEngine) recoverAfterStep(c *target37EquivalentControl, object *target37EquivalentObject, site *target37EquivalentSite, publicOnly bool) (int64, error) {
	cellIndex := -1
	switch {
	case object.dispatch == 1 && object.shape == 1:
		cellIndex = 0
	case object.dispatch == 2 && object.shape == 2:
		cellIndex = 2
	case object.dispatch == 3 && object.shape == 3:
		cellIndex = 4
	default:
		return 0, target37EquivalentInternal
	}
	cell := e.lookup.cells[cellIndex]
	valid := object.dispatch == 1 && target37EquivalentAlphaCellValid(&cell, e.lookup.nextEpoch) ||
		object.dispatch == 2 && target37EquivalentBetaCellValid(&cell, e.lookup.nextEpoch) ||
		object.dispatch == 3 && cell.epoch != 0 && cell.epoch <= e.lookup.nextEpoch && cell.receipt == (target37EquivalentReceipt{owner: 4, slot: 12, definition: 12, outcome: target37EquivalentResolved, visibility: target37EquivalentPublic})
	if !valid || !target37EquivalentSiteValid(site, e.lookup.nextEpoch) {
		return 0, target37EquivalentInternal
	}
	if publicOnly && cell.receipt.visibility != target37EquivalentPublic {
		return 0, target37EquivalentNoMethod
	}
	for index := range site.arms {
		if target37EquivalentHits(&site.arms[index], object, &cell) {
			value, callErr := e.callTarget(c, object, site.arms[index].target)
			if callErr == nil {
				e.stats.hits++
			}
			return value, callErr
		}
	}
	stale := -1
	for index, arm := range site.arms {
		if arm.target != 0 && arm.dispatch == object.dispatch && arm.shape == object.shape {
			if arm.epoch == cell.epoch {
				return 0, target37EquivalentInternal
			}
			stale = index
		}
	}
	receipt, ok := e.resolve(object.dispatch, false)
	if !ok || receipt != cell.receipt {
		return 0, target37EquivalentInternal
	}
	target, ok := e.targetFor(object, receipt)
	if !ok {
		return 0, target37EquivalentInternal
	}
	value, err := e.callTarget(c, object, target)
	if err != nil {
		return 0, err
	}
	if err := c.poll(); err != nil {
		return 0, err
	}
	if !target37EquivalentSiteValid(site, e.lookup.nextEpoch) || e.lookup.cells[cellIndex] != cell {
		return 0, target37EquivalentInternal
	}
	current, ok := e.resolve(object.dispatch, false)
	if !ok || current != cell.receipt {
		return 0, target37EquivalentInternal
	}
	arm := -1
	if object.dispatch == 1 {
		arm = 0
	} else if object.dispatch == 2 {
		arm = 1
	}
	if stale >= 0 {
		if stale != arm {
			return 0, target37EquivalentInternal
		}
		e.publishArm(site, stale, object, cell, target)
		e.stats.staleMisses++
		e.stats.repairs++
		return value, nil
	}
	if arm < 0 {
		e.stats.uncachedFallbacks++
		return value, nil
	}
	if site.arms[arm] != (target37EquivalentArm{}) {
		return 0, target37EquivalentInternal
	}
	e.publishArm(site, arm, object, cell, target)
	e.stats.coldAdmissions++
	e.stats.admissions++
	e.stats.occupiedArms++
	return value, nil
}

func (e *target37EquivalentEngine) publishArm(site *target37EquivalentSite, index int, object *target37EquivalentObject, cell target37EquivalentCell, target uint32) {
	site.arms[index] = target37EquivalentArm{
		epoch: cell.epoch, dispatch: object.dispatch, shape: object.shape,
		slot: cell.receipt.slot, definition: cell.receipt.definition, target: target,
	}
}

func (e *target37EquivalentEngine) callTarget(c *target37EquivalentControl, object *target37EquivalentObject, target uint32) (int64, error) {
	switch target {
	case 1:
		return e.labelAlpha(c, object)
	case 2:
		return e.labelBeta(c, object)
	case 3:
		return e.labelGamma(c, object)
	case 4:
		return e.labelRedefined(c, object)
	default:
		return 0, target37EquivalentInternal
	}
}

func (e *target37EquivalentEngine) labelAlpha(c *target37EquivalentControl, object *target37EquivalentObject) (int64, error) {
	if _, err := c.enterFrame(); err != nil {
		return 0, err
	}
	defer c.leaveFrame()
	if err := e.mark(c, object, 6); err != nil {
		return 0, err
	}
	return 1, nil
}

func (e *target37EquivalentEngine) labelBeta(c *target37EquivalentControl, object *target37EquivalentObject) (int64, error) {
	if _, err := c.enterFrame(); err != nil {
		return 0, err
	}
	defer c.leaveFrame()
	if err := e.mark(c, object, 8); err != nil {
		return 0, err
	}
	return 3, nil
}

func (e *target37EquivalentEngine) labelGamma(c *target37EquivalentControl, object *target37EquivalentObject) (int64, error) {
	if _, err := c.enterFrame(); err != nil {
		return 0, err
	}
	defer c.leaveFrame()
	if err := e.mark(c, object, 9); err != nil {
		return 0, err
	}
	return 4, nil
}

func (e *target37EquivalentEngine) labelRedefined(c *target37EquivalentControl, object *target37EquivalentObject) (int64, error) {
	if _, err := c.enterFrame(); err != nil {
		return 0, err
	}
	defer c.leaveFrame()
	if err := e.mark(c, object, 7); err != nil {
		return 0, err
	}
	return 2, nil
}

func (e *target37EquivalentEngine) mark(c *target37EquivalentControl, object *target37EquivalentObject, digit int64) error {
	if _, err := c.enterFrame(); err != nil {
		return err
	}
	defer c.leaveFrame()
	if err := c.step(); err != nil {
		return err
	}
	object.trace = object.trace*10 + digit
	return nil
}

func target37EquivalentSend(e *target37EquivalentEngine, c *target37EquivalentControl, object *target37EquivalentObject) (int64, error) {
	if _, err := c.enterFrame(); err != nil {
		return 0, err
	}
	defer c.leaveFrame()
	if err := c.step(); err != nil {
		return 0, err
	}
	return e.sendAfterStep(c, object)
}

func target37EquivalentPublicCall(e *target37EquivalentEngine, c *target37EquivalentControl, object *target37EquivalentObject) (int64, error) {
	if _, err := c.enterFrame(); err != nil {
		return 0, err
	}
	defer c.leaveFrame()
	if err := c.step(); err != nil {
		return 0, err
	}
	value, err := e.publicAfterStep(c, object)
	if err == target37EquivalentNoMethod {
		return 5, nil
	}
	return value, err
}
