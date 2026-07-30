package generated

import "context"

// This file is an independently implemented Go comparator for the bounded
// receiver-after-definition state measured by Target 42. It deliberately
// names and represents its own facts instead of sharing candidate helpers.
// Both arms are nonzero, current, and exact in that state. Zero-arm admission
// and stale-arm recovery are outside this comparator's scope; the diagnostic
// generic lane separately proves stale recovery against the current path.

type target42EquivalentError string

func (e target42EquivalentError) Error() string { return string(e) }

const (
	target42EquivalentInternal   target42EquivalentError = "target42: internal"
	target42EquivalentStepLimit  target42EquivalentError = "target42: step limit"
	target42EquivalentFrameLimit target42EquivalentError = "target42: frame limit"
)

type target42EquivalentOutcome uint8

const (
	target42EquivalentResolved target42EquivalentOutcome = iota + 1
)

type target42EquivalentVisibility uint8

const (
	target42EquivalentPublic target42EquivalentVisibility = iota + 1
)

type target42EquivalentControl struct {
	ctx                                      context.Context
	steps, frames, objects, depth, nextFrame uint64
}

func (c *target42EquivalentControl) poll() error { return c.ctx.Err() }

func (c *target42EquivalentControl) step() error {
	if err := c.poll(); err != nil {
		return err
	}
	if c.steps == 0 {
		return target42EquivalentStepLimit
	}
	c.steps--
	return nil
}

func (c *target42EquivalentControl) enterFrame() (uint64, error) {
	if err := c.poll(); err != nil {
		return 0, err
	}
	if c.depth >= c.frames {
		return 0, target42EquivalentFrameLimit
	}
	c.depth++
	c.nextFrame++
	return c.nextFrame, nil
}

func (c *target42EquivalentControl) leaveFrame() { c.depth-- }

type target42EquivalentReceipt struct {
	owner, slot, definition uint32
	outcome                 target42EquivalentOutcome
	visibility              target42EquivalentVisibility
}

type target42EquivalentCell struct {
	epoch   uint64
	receipt target42EquivalentReceipt
}

type target42EquivalentArm struct {
	epoch                                     uint64
	dispatch, shape, slot, definition, target uint32
}

type target42EquivalentLookup struct {
	receiver, peer target42EquivalentCell
	nextEpoch      uint64
}

type target42EquivalentSite struct {
	arms [2]target42EquivalentArm
}

type target42EquivalentObject struct {
	id              uint64
	dispatch, shape uint32
	value, trace    int64
}

type target42EquivalentStats struct {
	hits, coldAdmissions, staleMisses, repairs uint64
	uncachedFallbacks, admissions, evictions   uint64
	occupiedArms                               uint64
}

type target42EquivalentEngine struct {
	active, closed, poisoned uint32
	lookup                   target42EquivalentLookup
	site                     target42EquivalentSite
	hotObject                target42EquivalentObject
	nextObject               uint64
	stats                    target42EquivalentStats
}

func target42NewEquivalentEngine() (*target42EquivalentEngine, target42EquivalentObject, target42EquivalentObject) {
	receiverCell := target42EquivalentCell{
		epoch: 32,
		receipt: target42EquivalentReceipt{
			owner: 8, slot: 15, definition: 17,
			outcome: target42EquivalentResolved, visibility: target42EquivalentPublic,
		},
	}
	peerCell := target42EquivalentCell{
		epoch: 29,
		receipt: target42EquivalentReceipt{
			owner: 5, slot: 13, definition: 16,
			outcome: target42EquivalentResolved, visibility: target42EquivalentPublic,
		},
	}
	receiver := target42EquivalentObject{id: 1, dispatch: 5, shape: 1, value: 10}
	peer := target42EquivalentObject{id: 2, dispatch: 1, shape: 1, value: 10}
	engine := &target42EquivalentEngine{
		lookup: target42EquivalentLookup{receiver: receiverCell, peer: peerCell, nextEpoch: 32},
		site: target42EquivalentSite{arms: [2]target42EquivalentArm{
			{epoch: 32, dispatch: 5, shape: 1, slot: 15, definition: 17, target: 9},
			{epoch: 29, dispatch: 1, shape: 1, slot: 13, definition: 16, target: 8},
		}},
		hotObject:  receiver,
		nextObject: 2,
		stats: target42EquivalentStats{
			hits: 1, coldAdmissions: 2, staleMisses: 1, repairs: 1,
			admissions: 2, occupiedArms: 2,
		},
	}
	return engine, receiver, peer
}

func target42EquivalentReceiverCellValid(cell *target42EquivalentCell, nextEpoch uint64) bool {
	if cell.epoch == 0 || cell.epoch > nextEpoch || cell.receipt.outcome != target42EquivalentResolved || cell.receipt.visibility != target42EquivalentPublic {
		return false
	}
	return cell.receipt.owner == 5 && cell.receipt.slot == 13 && cell.receipt.definition == 16 ||
		cell.receipt.owner == 8 && cell.receipt.slot == 15 && cell.receipt.definition == 17
}

func target42EquivalentPeerCellValid(cell *target42EquivalentCell, nextEpoch uint64) bool {
	return cell.epoch != 0 && cell.epoch <= nextEpoch && cell.receipt == (target42EquivalentReceipt{
		owner: 5, slot: 13, definition: 16,
		outcome: target42EquivalentResolved, visibility: target42EquivalentPublic,
	})
}

func target42EquivalentSiteValid(site *target42EquivalentSite, nextEpoch uint64) bool {
	if site == nil {
		return false
	}
	receiver, peer := &site.arms[0], &site.arms[1]
	if receiver.target != 0 {
		if receiver.epoch == 0 || receiver.epoch > nextEpoch || receiver.dispatch != 5 || receiver.shape != 1 {
			return false
		}
		switch receiver.target {
		case 8:
			if receiver.slot != 13 || receiver.definition != 16 {
				return false
			}
		case 9:
			if receiver.slot != 15 || receiver.definition != 17 {
				return false
			}
		default:
			return false
		}
	} else if *receiver != (target42EquivalentArm{}) {
		return false
	}
	if peer.target != 0 {
		if peer.epoch == 0 || peer.epoch > nextEpoch || peer.dispatch != 1 || peer.shape != 1 ||
			peer.slot != 13 || peer.definition != 16 || peer.target != 8 {
			return false
		}
	} else if *peer != (target42EquivalentArm{}) {
		return false
	}
	return true
}

func target42EquivalentHit(arm *target42EquivalentArm, object *target42EquivalentObject, cell *target42EquivalentCell) bool {
	return arm.target != 0 && arm.dispatch == object.dispatch && arm.shape == object.shape &&
		arm.epoch == cell.epoch && arm.slot == cell.receipt.slot && arm.definition == cell.receipt.definition
}

func (e *target42EquivalentEngine) readAfterStep(c *target42EquivalentControl, object *target42EquivalentObject) (int64, error) {
	var cell *target42EquivalentCell
	var arm *target42EquivalentArm
	switch object.dispatch {
	case 5:
		cell, arm = &e.lookup.receiver, &e.site.arms[0]
		if object.shape != 1 || !target42EquivalentReceiverCellValid(cell, e.lookup.nextEpoch) {
			return 0, target42EquivalentInternal
		}
	case 1:
		cell, arm = &e.lookup.peer, &e.site.arms[1]
		if object.shape != 1 || !target42EquivalentPeerCellValid(cell, e.lookup.nextEpoch) {
			return 0, target42EquivalentInternal
		}
	default:
		return 0, target42EquivalentInternal
	}
	if !target42EquivalentSiteValid(&e.site, e.lookup.nextEpoch) || !target42EquivalentHit(arm, object, cell) {
		return 0, target42EquivalentInternal
	}
	var value int64
	var err error
	switch arm.target {
	case 8:
		value, err = e.labelPeer(c, object)
	case 9:
		if object.dispatch != 5 {
			return 0, target42EquivalentInternal
		}
		value, err = e.labelReceiver(c, object)
	default:
		return 0, target42EquivalentInternal
	}
	if err == nil {
		e.stats.hits++
	}
	return value, err
}

func (e *target42EquivalentEngine) labelPeer(c *target42EquivalentControl, object *target42EquivalentObject) (int64, error) {
	if _, err := c.enterFrame(); err != nil {
		return 0, err
	}
	defer c.leaveFrame()
	if err := e.mark(c, object, 3); err != nil {
		return 0, err
	}
	return 13, nil
}

func (e *target42EquivalentEngine) labelReceiver(c *target42EquivalentControl, object *target42EquivalentObject) (int64, error) {
	if _, err := c.enterFrame(); err != nil {
		return 0, err
	}
	defer c.leaveFrame()
	if err := e.mark(c, object, 4); err != nil {
		return 0, err
	}
	return 44, nil
}

func (e *target42EquivalentEngine) mark(c *target42EquivalentControl, object *target42EquivalentObject, digit int64) error {
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

func target42EquivalentRead(e *target42EquivalentEngine, c *target42EquivalentControl, object *target42EquivalentObject) (int64, error) {
	if _, err := c.enterFrame(); err != nil {
		return 0, err
	}
	defer c.leaveFrame()
	if err := c.step(); err != nil {
		return 0, err
	}
	return e.readAfterStep(c, object)
}
