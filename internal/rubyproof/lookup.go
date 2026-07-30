package rubyproof

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

type selectorID uint32
type lookupNodeID uint32
type lookupAttachmentID uint32
type methodSlotID uint32
type definitionID uint32
type dispatchClassID uint32
type shapeID uint32
type callSiteID uint32
type targetID uint32

const (
	lookupImageSchema uint32 = 4

	maximumLookupNodes        = 512
	maximumLookupAttachments  = 64
	maximumLookupRouteOracles = 512
	maximumDispatchClasses    = 256
	maximumPreparedSelectors  = 64
	maximumLookupDepth        = 32
	maximumResolutionCells    = 8_192
	maximumLocalEntries       = 32_768
	maximumLookupPathSlots    = 8_192
	maximumLookupSites        = 4_096
	maximumMutationProbes     = 262_144
	maximumLookupOwnerBytes   = 1_572_864 // 1.5 MiB
)

var (
	errLookupImage      = errors.New("ruby: malformed lookup image")
	errLookupCorrupt    = errors.New("ruby: corrupt lookup state")
	errLookupEpochLimit = errors.New("ruby: lookup epoch exhausted")
)

type lookupOutcome uint8

const (
	lookupMissing lookupOutcome = iota
	lookupResolved
	lookupUndef
)

type lookupEntryKind uint8

const (
	lookupEntryAbsent lookupEntryKind = iota
	lookupEntryDefined
	lookupEntryUndef
)

// lookupVisibility is method-entry state. Resolution cells carry the winning
// entry's visibility, while each checked call site owns the legality policy
// for that receipt. Keeping those concerns separate avoids a second lookup
// graph for send/public_send.
type lookupVisibility uint8

const (
	lookupVisibilityPublic lookupVisibility = iota + 1
	lookupVisibilityProtected
	lookupVisibilityPrivate
)

type lookupNodeKind uint8

const (
	lookupNodeClass lookupNodeKind = iota + 1
	lookupNodeModule
	// lookupNodeSingleton is one statically assigned receiver-specific lookup
	// root. Its superclass is the receiver's ordinary class node, while its
	// dispatch row is unique to that receiver. It deliberately owns no object
	// layout: singleton methods change lookup, not the receiver's shape.
	lookupNodeSingleton
)

type lookupAttachmentKind uint8

const (
	lookupAttachmentInclude lookupAttachmentKind = iota + 1
	lookupAttachmentPrepend
)

type lookupMutationKind uint8

const (
	lookupMutationDefine lookupMutationKind = iota + 1
	lookupMutationAlias
	lookupMutationRemove
	lookupMutationUndef
	lookupMutationVisibility
	lookupMutationInclude
	lookupMutationPrepend
)

// lookupEntry is one mutable local method-table position. The selector and
// owner are supplied by its dense coordinate; the entry itself contains only
// the stable local slot and the currently installed definition.
type lookupEntry struct {
	slot       methodSlotID
	definition definitionID
	kind       lookupEntryKind
	visibility lookupVisibility
	reserved8  [2]byte
	reserved   uint32
}

// lookupReceipt is the effective semantic result of one dispatch-row and
// prepared-selector pair. Route identity is deliberately absent: a route-only
// change does not stale an ordinary send whose winner is unchanged.
type lookupReceipt struct {
	owner        lookupNodeID
	slot         methodSlotID
	definition   definitionID
	outcome      lookupOutcome
	visibility   lookupVisibility
	legality     uint8
	reserved8    uint8
	continuation uint32
	reserved32   uint32
}

type resolutionCell struct {
	epoch   uint64
	receipt lookupReceipt
}

// The descriptors have deliberately frozen 64-bit sizes. Nodes own immutable
// contiguous attachment ranges; they never own or point at a derived route.
type lookupNodeDescriptor struct {
	id              lookupNodeID
	superclass      lookupNodeID
	dispatch        dispatchClassID
	attachmentStart uint32
	attachmentCount uint16
	kind            lookupNodeKind
	flags           uint8
	reserved        uint32
	reserved2       uint32
	reserved3       uint32
}

type dispatchRowDescriptor struct {
	id            dispatchClassID
	node          lookupNodeID
	cellStart     uint32
	selectorCount uint16
	flags         uint16
	reserved      uint32
	reserved2     uint32
	reserved3     uint32
	reserved4     uint32
}

type selectorDescriptor struct {
	id       selectorID
	ordinal  uint32
	nameHash uint64
}

type definitionDescriptor struct {
	id       definitionID
	owner    lookupNodeID
	selector selectorID
	slot     methodSlotID
	body     definitionID
}

// lookupAttachmentDescriptor is immutable topology. Its one-based ID is also
// its activation bit. ordinal is dense within the target's attachment range
// and preserves Ruby's source application order; route construction visits
// active attachments in reverse order for both include and prepend.
type lookupAttachmentDescriptor struct {
	id         lookupAttachmentID
	target     lookupNodeID
	module     lookupNodeID
	ordinal    uint32
	kind       lookupAttachmentKind
	reserved   [3]byte
	reserved32 uint32
}

// lookupRouteOracle is an independently checked, sealed comparison fact for
// one reachable activation mask and dispatch row. The canonical runtime never
// resolves from oraclePaths: it derives into owner-local scratch, compares the
// exact route, and resolves only from that scratch.
type lookupRouteOracle struct {
	activations uint64
	row         dispatchClassID
	pathStart   uint32
	pathLength  uint16
	reserved16  uint16
	reserved    [3]uint32
}

// lookupMutationDescriptor is one finite checked operation. Entry operations
// carry complete before/after entries and preserve the activation mask.
// Include/prepend operations carry one attachment, zero entry coordinates,
// and an exact before/after activation transition. The explicit masks reject
// reverse, skipped, and replayed topology operations without adding a mutable
// topology-state ordinal.
type lookupMutationDescriptor struct {
	id                uint32
	owner             lookupNodeID
	selector          selectorID
	kind              lookupMutationKind
	reserved          [3]byte
	attachment        lookupAttachmentID
	reserved32        uint32
	beforeActivations uint64
	afterActivations  uint64
	before            lookupEntry
	after             lookupEntry
}

// lookupArm contains scalar generation-local identities only. target==0 is
// the unoccupied marker and is published last after complete revalidation.
type lookupArm struct {
	epoch      uint64
	dispatch   dispatchClassID
	shape      shapeID
	selector   selectorID
	slot       methodSlotID
	definition definitionID
	target     targetID
}

type lookupSite struct{ arms [2]lookupArm }

// lookupImage is a passive checked fact product. It owns no method bodies,
// mutable entries/cells/activation bits, target IDs, sites, objects, or
// executable behavior. oraclePaths are comparison facts, not runtime routes.
type lookupImage struct {
	schema          uint32
	identity        [sha256.Size]byte
	attribution     [sha256.Size]byte
	nodes           []lookupNodeDescriptor
	rows            []dispatchRowDescriptor
	selectors       []selectorDescriptor
	definitions     []definitionDescriptor
	attachments     []lookupAttachmentDescriptor
	mutations       []lookupMutationDescriptor
	routeOracles    []lookupRouteOracle
	oraclePaths     []lookupNodeID
	initialEntries  []lookupEntry
	initialReceipts []lookupReceipt
}

// lookupTopologyBank is the complete mutable topology authority. It contains
// no state ordinal, route, or pointer. The owner is single-operation, so an
// ordinary scalar bank swap is the only publication mechanism required.
type lookupTopologyBank struct {
	activations uint64
}

type lookupOwner struct {
	image          *lookupImage
	entries        []lookupEntry
	cells          []resolutionCell
	staged         []resolutionCell
	changed        []uint32
	marks          []uint8
	topology       lookupTopologyBank
	stagedTopology lookupTopologyBank
	routeScratch   []lookupNodeID
	routeMarks     []uint8

	nextEpoch uint64
}

func sealLookupImage(
	attribution [sha256.Size]byte,
	nodes []lookupNodeDescriptor,
	rows []dispatchRowDescriptor,
	selectors []selectorDescriptor,
	definitions []definitionDescriptor,
	attachments []lookupAttachmentDescriptor,
	mutations []lookupMutationDescriptor,
	routeOracles []lookupRouteOracle,
	oraclePaths []lookupNodeID,
	initialEntries []lookupEntry,
	initialReceipts []lookupReceipt,
) lookupImage {
	image := lookupImage{
		schema:          lookupImageSchema,
		attribution:     attribution,
		nodes:           append([]lookupNodeDescriptor(nil), nodes...),
		rows:            append([]dispatchRowDescriptor(nil), rows...),
		selectors:       append([]selectorDescriptor(nil), selectors...),
		definitions:     append([]definitionDescriptor(nil), definitions...),
		attachments:     append([]lookupAttachmentDescriptor(nil), attachments...),
		mutations:       append([]lookupMutationDescriptor(nil), mutations...),
		routeOracles:    append([]lookupRouteOracle(nil), routeOracles...),
		oraclePaths:     append([]lookupNodeID(nil), oraclePaths...),
		initialEntries:  append([]lookupEntry(nil), initialEntries...),
		initialReceipts: append([]lookupReceipt(nil), initialReceipts...),
	}
	image.identity = lookupImageIdentity(&image)
	return image
}

func validateLookupImage(image *lookupImage) error {
	if image == nil || image.schema != lookupImageSchema || image.attribution == ([sha256.Size]byte{}) || image.identity == ([sha256.Size]byte{}) || image.identity != lookupImageIdentity(image) {
		return errLookupImage
	}
	if len(image.nodes) == 0 || len(image.nodes) > maximumLookupNodes || len(image.attachments) > maximumLookupAttachments ||
		len(image.rows) == 0 || len(image.rows) > maximumDispatchClasses || len(image.selectors) == 0 || len(image.selectors) > maximumPreparedSelectors ||
		len(image.definitions) == 0 || len(image.definitions) > maximumLocalEntries || len(image.mutations) == 0 || len(image.mutations) > maximumLocalEntries ||
		len(image.routeOracles) == 0 || len(image.routeOracles) > maximumLookupRouteOracles || len(image.oraclePaths) == 0 || len(image.oraclePaths) > maximumLookupPathSlots {
		return errLookupImage
	}
	cellCount, ok := checkedProduct(len(image.rows), len(image.selectors), maximumResolutionCells)
	if !ok {
		return errLookupImage
	}
	entryCount, ok := checkedProduct(len(image.nodes), len(image.selectors), maximumLocalEntries)
	if !ok || len(image.initialEntries) != entryCount || len(image.initialReceipts) != cellCount {
		return errLookupImage
	}
	if _, ok := checkedProduct(cellCount, maximumLookupDepth, maximumMutationProbes); !ok {
		return errLookupImage
	}

	for index, selector := range image.selectors {
		if selector.id != selectorID(index+1) || selector.ordinal != uint32(index) || selector.nameHash == 0 {
			return errLookupImage
		}
	}

	seenDispatch := make([]bool, len(image.rows)+1)
	attachmentCursor := 0
	var singletonNode lookupNodeID
	for index, node := range image.nodes {
		if node.id != lookupNodeID(index+1) || node.flags != 0 || node.reserved != 0 || node.reserved2 != 0 || node.reserved3 != 0 ||
			int(node.attachmentStart) != attachmentCursor {
			return errLookupImage
		}
		switch node.kind {
		case lookupNodeClass:
			if node.superclass >= node.id {
				return errLookupImage
			}
		case lookupNodeModule:
			if node.superclass != 0 || node.dispatch != 0 {
				return errLookupImage
			}
		case lookupNodeSingleton:
			// This proof admits one eager singleton root above an ordinary
			// class. Keeping it attachment-free and ordered after its base
			// rejects a second, independently mutable topology category.
			if singletonNode != 0 || node.superclass == 0 || node.superclass >= node.id || node.dispatch == 0 || node.attachmentCount != 0 ||
				image.nodes[node.superclass-1].kind != lookupNodeClass || image.nodes[node.superclass-1].dispatch == 0 {
				return errLookupImage
			}
			singletonNode = node.id
		default:
			return errLookupImage
		}
		if node.dispatch != 0 {
			if int(node.dispatch) > len(image.rows) || seenDispatch[node.dispatch] {
				return errLookupImage
			}
			seenDispatch[node.dispatch] = true
		}
		end := attachmentCursor + int(node.attachmentCount)
		if end < attachmentCursor || end > len(image.attachments) {
			return errLookupImage
		}
		for localIndex, attachment := range image.attachments[attachmentCursor:end] {
			if attachment.id == 0 || int(attachment.id) > len(image.attachments) || attachment.target != node.id ||
				attachment.ordinal != uint32(localIndex) || attachment.reserved != [3]byte{} || attachment.reserved32 != 0 ||
				attachment.module == 0 || int(attachment.module) > len(image.nodes) || attachment.module == attachment.target ||
				image.nodes[attachment.module-1].kind != lookupNodeModule ||
				(attachment.kind != lookupAttachmentInclude && attachment.kind != lookupAttachmentPrepend) {
				return errLookupImage
			}
			for priorIndex := attachmentCursor; priorIndex < attachmentCursor+localIndex; priorIndex++ {
				prior := image.attachments[priorIndex]
				if prior.module == attachment.module && prior.kind == attachment.kind {
					return errLookupImage
				}
			}
		}
		attachmentCursor = end
	}
	if attachmentCursor != len(image.attachments) {
		return errLookupImage
	}
	for index, attachment := range image.attachments {
		if attachment.id != lookupAttachmentID(index+1) {
			return errLookupImage
		}
	}

	for index, row := range image.rows {
		if row.id != dispatchClassID(index+1) || row.node == 0 || int(row.node) > len(image.nodes) ||
			row.cellStart != uint32(index*len(image.selectors)) || row.selectorCount != uint16(len(image.selectors)) ||
			row.flags != 0 || row.reserved != 0 || row.reserved2 != 0 || row.reserved3 != 0 || row.reserved4 != 0 {
			return errLookupImage
		}
		node := image.nodes[row.node-1]
		if (node.kind != lookupNodeClass && node.kind != lookupNodeSingleton) || node.dispatch != row.id {
			return errLookupImage
		}
	}

	singletonDefinitions := 0
	for index, definition := range image.definitions {
		if definition.id != definitionID(index+1) || definition.owner == 0 || int(definition.owner) > len(image.nodes) ||
			definition.selector == 0 || int(definition.selector) > len(image.selectors) || definition.slot == 0 ||
			definition.body == 0 || int(definition.body) > len(image.definitions) || definition.body > definition.id {
			return errLookupImage
		}
		body := image.definitions[definition.body-1]
		if body.body != body.id {
			return errLookupImage
		}
		if definition.owner == singletonNode {
			singletonDefinitions++
		}
	}
	for index, entry := range image.initialEntries {
		owner := lookupNodeID(index/len(image.selectors) + 1)
		selector := selectorID(index%len(image.selectors) + 1)
		if !validLookupEntry(image, owner, selector, entry) {
			return errLookupImage
		}
	}

	mutationEntries := append([]lookupEntry(nil), image.initialEntries...)
	mutationActivations := uint64(0)
	attachmentUses := make([]uint8, len(image.attachments))
	requiredMasks := map[uint64]struct{}{0: {}}
	singletonMutations := 0
	for index, mutation := range image.mutations {
		if mutation.id != uint32(index+1) || mutation.reserved != [3]byte{} || mutation.reserved32 != 0 ||
			!validLookupActivationMask(image, mutation.beforeActivations) || !validLookupActivationMask(image, mutation.afterActivations) ||
			mutation.beforeActivations != mutationActivations {
			return errLookupImage
		}
		requiredMasks[mutation.beforeActivations] = struct{}{}
		requiredMasks[mutation.afterActivations] = struct{}{}
		switch mutation.kind {
		case lookupMutationDefine, lookupMutationAlias, lookupMutationRemove, lookupMutationUndef, lookupMutationVisibility:
			if mutation.attachment != 0 || mutation.owner == 0 || int(mutation.owner) > len(image.nodes) ||
				mutation.selector == 0 || int(mutation.selector) > len(image.selectors) ||
				mutation.beforeActivations != mutation.afterActivations ||
				!validLookupEntry(image, mutation.owner, mutation.selector, mutation.before) ||
				!validLookupEntry(image, mutation.owner, mutation.selector, mutation.after) {
				return errLookupImage
			}
			entryIndex := (int(mutation.owner)-1)*len(image.selectors) + int(mutation.selector) - 1
			if mutationEntries[entryIndex] != mutation.before || !validLookupEntryTransition(image, mutation) {
				return errLookupImage
			}
			mutationEntries[entryIndex] = mutation.after
		case lookupMutationInclude, lookupMutationPrepend:
			if mutation.selector != 0 || mutation.before != (lookupEntry{}) || mutation.after != (lookupEntry{}) ||
				mutation.attachment == 0 || int(mutation.attachment) > len(image.attachments) {
				return errLookupImage
			}
			attachment := image.attachments[mutation.attachment-1]
			wantKind := lookupAttachmentInclude
			if mutation.kind == lookupMutationPrepend {
				wantKind = lookupAttachmentPrepend
			}
			bit := lookupAttachmentBit(mutation.attachment)
			if attachment.target != mutation.owner || attachment.kind != wantKind || mutation.beforeActivations&bit != 0 ||
				mutation.afterActivations != mutation.beforeActivations|bit || attachmentUses[mutation.attachment-1] != 0 {
				return errLookupImage
			}
			attachmentUses[mutation.attachment-1] = 1
		default:
			return errLookupImage
		}
		if mutation.owner == singletonNode {
			singletonMutations++
			if index != len(image.mutations)-1 || mutation.kind != lookupMutationDefine || mutation.before != (lookupEntry{}) ||
				mutation.after.kind != lookupEntryDefined || mutation.after.visibility != lookupVisibilityPublic {
				return errLookupImage
			}
		}
		mutationActivations = mutation.afterActivations
	}
	if singletonNode != 0 {
		if singletonDefinitions != 1 || singletonMutations != 1 {
			return errLookupImage
		}
		start := (int(singletonNode) - 1) * len(image.selectors)
		for _, entry := range image.initialEntries[start : start+len(image.selectors)] {
			if entry != (lookupEntry{}) {
				return errLookupImage
			}
		}
	} else if singletonDefinitions != 0 || singletonMutations != 0 {
		return errLookupImage
	}
	for _, uses := range attachmentUses {
		if uses != 1 {
			return errLookupImage
		}
	}
	// Every descriptor is active by the end of the monotonic checked schedule.
	// Derive from every class and module, not only dispatch rows, so an orphaned
	// module cycle or an over-depth superclass/module chain cannot hide outside
	// the sealed row-oracle set.
	var topologyRouteScratch [maximumLookupDepth]lookupNodeID
	var topologyRouteMarks [maximumLookupNodes]uint8
	for _, node := range image.nodes {
		if _, err := deriveLookupRoute(image, mutationActivations, node.id, topologyRouteScratch[:], topologyRouteMarks[:]); err != nil {
			return errLookupImage
		}
	}

	if len(image.routeOracles)%len(image.rows) != 0 {
		return errLookupImage
	}
	oracleMaskCount := len(image.routeOracles) / len(image.rows)
	if _, ok := checkedProduct(oracleMaskCount, len(image.rows), maximumLookupRouteOracles); !ok {
		return errLookupImage
	}
	pathCursor := 0
	var routeScratch [maximumLookupDepth]lookupNodeID
	var routeMarks [maximumLookupNodes]uint8
	seenOracleMasks := make(map[uint64]struct{}, oracleMaskCount)
	var previousMask uint64
	for maskIndex := 0; maskIndex < oracleMaskCount; maskIndex++ {
		activations := image.routeOracles[maskIndex*len(image.rows)].activations
		if !validLookupActivationMask(image, activations) || maskIndex != 0 && activations <= previousMask {
			return errLookupImage
		}
		previousMask = activations
		seenOracleMasks[activations] = struct{}{}
		for rowIndex, row := range image.rows {
			oracle := image.routeOracles[maskIndex*len(image.rows)+rowIndex]
			if oracle.activations != activations || oracle.row != row.id || int(oracle.pathStart) != pathCursor || oracle.pathLength == 0 ||
				oracle.reserved16 != 0 || oracle.reserved != [3]uint32{} {
				return errLookupImage
			}
			end := pathCursor + int(oracle.pathLength)
			if end < pathCursor || end > len(image.oraclePaths) {
				return errLookupImage
			}
			length, err := deriveLookupRoute(image, activations, row.node, routeScratch[:], routeMarks[:])
			if err != nil || length != int(oracle.pathLength) || !equalLookupRoute(routeScratch[:length], image.oraclePaths[pathCursor:end]) {
				return errLookupImage
			}
			clear(routeScratch[:])
			pathCursor = end
		}
	}
	if pathCursor != len(image.oraclePaths) {
		return errLookupImage
	}
	for requiredMask := range requiredMasks {
		if _, ok := seenOracleMasks[requiredMask]; !ok {
			return errLookupImage
		}
	}

	for rowIndex, row := range image.rows {
		length, err := deriveLookupRoute(image, 0, row.node, routeScratch[:], routeMarks[:])
		if err != nil || !lookupRouteMatchesOracle(image, 0, row.id, routeScratch[:length]) {
			return errLookupImage
		}
		for selectorIndex := range image.selectors {
			selector := selectorID(selectorIndex + 1)
			receipt := image.initialReceipts[rowIndex*len(image.selectors)+selectorIndex]
			if !validLookupReceipt(image, selector, receipt) {
				return errLookupImage
			}
			computed, err := resolveLookupRoute(image, image.initialEntries, routeScratch[:length], selector, 0, lookupEntry{})
			if err != nil || computed != receipt {
				return errLookupImage
			}
		}
		clear(routeScratch[:])
	}
	if _, ok := lookupAdmissionBytes(len(image.nodes), len(image.rows), len(image.selectors), len(image.attachments), len(image.routeOracles), len(image.oraclePaths), 0); !ok {
		return errLookupImage
	}
	return nil
}

func validLookupEntryTransition(image *lookupImage, mutation lookupMutationDescriptor) bool {
	switch mutation.kind {
	case lookupMutationDefine:
		if mutation.after.kind != lookupEntryDefined || mutation.after.definition == 0 {
			return false
		}
		definition := image.definitions[mutation.after.definition-1]
		return definition.body == definition.id
	case lookupMutationAlias:
		if mutation.after.kind != lookupEntryDefined || mutation.after.definition == 0 {
			return false
		}
		definition := image.definitions[mutation.after.definition-1]
		return definition.body != definition.id
	case lookupMutationRemove:
		return mutation.before.kind == lookupEntryDefined && mutation.after.kind == lookupEntryAbsent &&
			mutation.after.definition == 0 && mutation.after.slot == mutation.before.slot
	case lookupMutationUndef:
		return mutation.before.kind == lookupEntryDefined && mutation.after.kind == lookupEntryUndef &&
			mutation.after.definition == 0 && mutation.after.slot == mutation.before.slot
	case lookupMutationVisibility:
		return mutation.before.kind == lookupEntryDefined && mutation.after.kind == lookupEntryDefined &&
			mutation.before.slot == mutation.after.slot && mutation.before.definition == mutation.after.definition &&
			mutation.before.visibility != mutation.after.visibility
	default:
		return false
	}
}

func newLookupOwner(image *lookupImage) (*lookupOwner, error) {
	if err := validateLookupImage(image); err != nil {
		return nil, err
	}
	entryCount := len(image.nodes) * len(image.selectors)
	cellCount := len(image.rows) * len(image.selectors)
	owner := &lookupOwner{
		image:        image,
		entries:      make([]lookupEntry, entryCount),
		cells:        make([]resolutionCell, cellCount),
		staged:       make([]resolutionCell, cellCount),
		changed:      make([]uint32, 0, cellCount),
		marks:        make([]uint8, cellCount),
		routeScratch: make([]lookupNodeID, maximumLookupDepth),
		routeMarks:   make([]uint8, len(image.nodes)),
	}
	copy(owner.entries, image.initialEntries)
	for rowIndex, row := range image.rows {
		length, err := owner.deriveRoute(0, row)
		if err != nil {
			owner.close()
			return nil, errLookupImage
		}
		for selectorIndex := range image.selectors {
			index := rowIndex*len(image.selectors) + selectorIndex
			selector := selectorID(selectorIndex + 1)
			canonical, err := resolveLookupRoute(image, owner.entries, owner.routeScratch[:length], selector, 0, lookupEntry{})
			if err != nil || canonical != image.initialReceipts[index] {
				owner.close()
				return nil, errLookupImage
			}
			owner.nextEpoch++
			owner.cells[index] = resolutionCell{epoch: owner.nextEpoch, receipt: canonical}
		}
		owner.clearRouteScratch()
	}
	copy(owner.staged, owner.cells)
	owner.stagedTopology = owner.topology
	return owner, nil
}

func (owner *lookupOwner) apply(ctx context.Context, mutation lookupMutationDescriptor) error {
	if owner == nil || owner.image == nil || ctx == nil || !owner.validShape() {
		return errLookupCorrupt
	}
	owner.clearRouteScratch()
	defer owner.clearRouteScratch()
	if err := ctx.Err(); err != nil {
		return err
	}
	if mutation.id == 0 || int(mutation.id) > len(owner.image.mutations) || owner.image.mutations[mutation.id-1] != mutation ||
		owner.topology.activations != mutation.beforeActivations {
		return errLookupCorrupt
	}
	copy(owner.staged, owner.cells)
	owner.changed = owner.changed[:0]
	clear(owner.marks)
	owner.stagedTopology = owner.topology

	entryIndex := -1
	switch mutation.kind {
	case lookupMutationDefine, lookupMutationAlias, lookupMutationRemove, lookupMutationUndef, lookupMutationVisibility:
		selectorCount := len(owner.image.selectors)
		entryIndex = (int(mutation.owner)-1)*selectorCount + int(mutation.selector) - 1
		if mutation.beforeActivations != mutation.afterActivations || mutation.attachment != 0 || entryIndex < 0 || entryIndex >= len(owner.entries) ||
			owner.entries[entryIndex] != mutation.before {
			return errLookupCorrupt
		}
		if err := owner.stageEntryMutation(ctx, mutation); err != nil {
			return err
		}
	case lookupMutationInclude, lookupMutationPrepend:
		if mutation.attachment == 0 || int(mutation.attachment) > len(owner.image.attachments) {
			return errLookupCorrupt
		}
		attachment := owner.image.attachments[mutation.attachment-1]
		wantKind := lookupAttachmentInclude
		if mutation.kind == lookupMutationPrepend {
			wantKind = lookupAttachmentPrepend
		}
		if attachment.target != mutation.owner || attachment.kind != wantKind || mutation.selector != 0 ||
			mutation.before != (lookupEntry{}) || mutation.after != (lookupEntry{}) ||
			mutation.afterActivations != mutation.beforeActivations|lookupAttachmentBit(mutation.attachment) ||
			mutation.beforeActivations&lookupAttachmentBit(mutation.attachment) != 0 {
			return errLookupCorrupt
		}
		owner.stagedTopology.activations = mutation.afterActivations
		if err := owner.stageTopologyMutation(ctx); err != nil {
			return err
		}
	default:
		return errLookupCorrupt
	}

	if mutation.owner != 0 && int(mutation.owner) <= len(owner.image.nodes) && owner.image.nodes[mutation.owner-1].kind == lookupNodeSingleton {
		node := owner.image.nodes[mutation.owner-1]
		cellIndex := (int(node.dispatch)-1)*len(owner.image.selectors) + int(mutation.selector) - 1
		if mutation.kind != lookupMutationDefine || mutation.before != (lookupEntry{}) || mutation.after.kind != lookupEntryDefined ||
			mutation.after.visibility != lookupVisibilityPublic || len(owner.changed) != 1 || int(owner.changed[0]) != cellIndex ||
			cellIndex < 0 || cellIndex >= len(owner.staged) || owner.staged[cellIndex].receipt.owner != mutation.owner {
			return errLookupCorrupt
		}
	}

	if uint64(len(owner.changed)) > math.MaxUint64-owner.nextEpoch {
		return errLookupEpochLimit
	}
	nextEpoch := owner.nextEpoch
	for _, rawIndex := range owner.changed {
		if int(rawIndex) >= len(owner.staged) || owner.marks[rawIndex] != 1 {
			return errLookupCorrupt
		}
		nextEpoch++
		owner.staged[rawIndex].epoch = nextEpoch
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Commit is deliberately infallible and publish-last. Entry operations
	// publish one local table coordinate; topology operations publish the
	// staged activation bank. Both swap the complete dense cell bank and then
	// publish the scalar epoch. Sites are not mutation dependencies.
	if entryIndex >= 0 {
		owner.entries[entryIndex] = mutation.after
	} else {
		owner.topology, owner.stagedTopology = owner.stagedTopology, owner.topology
	}
	owner.cells, owner.staged = owner.staged, owner.cells
	owner.nextEpoch = nextEpoch
	return nil
}

func (owner *lookupOwner) stageEntryMutation(ctx context.Context, mutation lookupMutationDescriptor) error {
	selectorCount := len(owner.image.selectors)
	for rowIndex, row := range owner.image.rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		length, err := owner.deriveRoute(owner.topology.activations, row)
		if err != nil {
			return err
		}
		cellIndex := rowIndex*selectorCount + int(mutation.selector) - 1
		current, err := resolveLookupRoute(owner.image, owner.entries, owner.routeScratch[:length], mutation.selector, 0, lookupEntry{})
		if err != nil || cellIndex < 0 || cellIndex >= len(owner.cells) || owner.cells[cellIndex].epoch == 0 || owner.cells[cellIndex].receipt != current {
			return errLookupCorrupt
		}
		proposed, err := resolveLookupRoute(owner.image, owner.entries, owner.routeScratch[:length], mutation.selector, mutation.owner, mutation.after)
		if err != nil {
			return err
		}
		if proposed != current {
			owner.changed = append(owner.changed, uint32(cellIndex))
			owner.marks[cellIndex] = 1
			owner.staged[cellIndex].receipt = proposed
		}
		owner.clearRouteScratch()
	}
	return nil
}

func (owner *lookupOwner) stageTopologyMutation(ctx context.Context) error {
	selectorCount := len(owner.image.selectors)
	for rowIndex, row := range owner.image.rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		currentLength, err := owner.deriveRoute(owner.topology.activations, row)
		if err != nil {
			return err
		}
		for selectorIndex := range owner.image.selectors {
			selector := selectorID(selectorIndex + 1)
			cellIndex := rowIndex*selectorCount + selectorIndex
			current, err := resolveLookupRoute(owner.image, owner.entries, owner.routeScratch[:currentLength], selector, 0, lookupEntry{})
			if err != nil || owner.cells[cellIndex].epoch == 0 || owner.cells[cellIndex].receipt != current {
				return errLookupCorrupt
			}
		}
		owner.clearRouteScratch()

		proposedLength, err := owner.deriveRoute(owner.stagedTopology.activations, row)
		if err != nil {
			return err
		}
		for selectorIndex := range owner.image.selectors {
			selector := selectorID(selectorIndex + 1)
			cellIndex := rowIndex*selectorCount + selectorIndex
			proposed, err := resolveLookupRoute(owner.image, owner.entries, owner.routeScratch[:proposedLength], selector, 0, lookupEntry{})
			if err != nil {
				return err
			}
			if proposed != owner.cells[cellIndex].receipt {
				owner.changed = append(owner.changed, uint32(cellIndex))
				owner.marks[cellIndex] = 1
				owner.staged[cellIndex].receipt = proposed
			}
		}
		owner.clearRouteScratch()
	}
	return nil
}

func (owner *lookupOwner) validShape() bool {
	if owner == nil || owner.image == nil {
		return false
	}
	entryCount := len(owner.image.nodes) * len(owner.image.selectors)
	cellCount := len(owner.image.rows) * len(owner.image.selectors)
	return len(owner.entries) == entryCount && len(owner.cells) == cellCount && len(owner.staged) == cellCount &&
		len(owner.marks) == cellCount && cap(owner.changed) >= cellCount && len(owner.routeScratch) == maximumLookupDepth &&
		len(owner.routeMarks) == len(owner.image.nodes) && validLookupActivationMask(owner.image, owner.topology.activations)
}

func (owner *lookupOwner) deriveRoute(activations uint64, row dispatchRowDescriptor) (int, error) {
	if owner == nil || owner.image == nil || row.id == 0 || int(row.id) > len(owner.image.rows) || owner.image.rows[row.id-1] != row {
		return 0, errLookupCorrupt
	}
	owner.clearRouteScratch()
	length, err := deriveLookupRoute(owner.image, activations, row.node, owner.routeScratch, owner.routeMarks)
	if err != nil || !lookupRouteMatchesOracle(owner.image, activations, row.id, owner.routeScratch[:length]) {
		return 0, errLookupCorrupt
	}
	return length, nil
}

// deriveLookupRoute is the sole canonical topology linearizer. It consumes
// immutable node/attachment descriptors and an activation mask, writes only
// caller-owned bounded scratch, performs no allocation, and retains no route.
// The caller must not retain scratch across guest effects.
func deriveLookupRoute(image *lookupImage, activations uint64, root lookupNodeID, route []lookupNodeID, marks []uint8) (int, error) {
	if image == nil || root == 0 || int(root) > len(image.nodes) || len(route) < maximumLookupDepth || len(marks) < len(image.nodes) ||
		!validLookupActivationMask(image, activations) {
		return 0, errLookupCorrupt
	}
	clear(route)
	clear(marks[:len(image.nodes)])
	builder := lookupRouteBuilder{
		image:       image,
		activations: activations,
		route:       route[:maximumLookupDepth],
		marks:       marks[:len(image.nodes)],
	}
	err := builder.appendNode(root)
	clear(builder.marks)
	if err != nil || builder.length == 0 {
		return 0, errLookupCorrupt
	}
	return builder.length, nil
}

type lookupRouteBuilder struct {
	image       *lookupImage
	activations uint64
	route       []lookupNodeID
	marks       []uint8
	length      int
}

func (builder *lookupRouteBuilder) appendNode(id lookupNodeID) error {
	if builder == nil || builder.image == nil || id == 0 || int(id) > len(builder.image.nodes) {
		return errLookupCorrupt
	}
	mark := &builder.marks[id-1]
	switch *mark {
	case 2:
		return nil
	case 1:
		return errLookupCorrupt
	case 0:
	default:
		return errLookupCorrupt
	}
	*mark = 1
	node := builder.image.nodes[id-1]
	if node.id != id || (node.kind != lookupNodeClass && node.kind != lookupNodeModule && node.kind != lookupNodeSingleton) {
		return errLookupCorrupt
	}
	start := int(node.attachmentStart)
	end := start + int(node.attachmentCount)
	if start < 0 || end < start || end > len(builder.image.attachments) {
		return errLookupCorrupt
	}
	for index := end - 1; index >= start; index-- {
		attachment := builder.image.attachments[index]
		if attachment.target != id || attachment.kind != lookupAttachmentPrepend || !lookupAttachmentActive(builder.activations, attachment.id) {
			continue
		}
		if err := builder.appendNode(attachment.module); err != nil {
			return err
		}
	}
	if builder.length >= len(builder.route) || builder.length >= maximumLookupDepth {
		return errLookupCorrupt
	}
	builder.route[builder.length] = id
	builder.length++
	for index := end - 1; index >= start; index-- {
		attachment := builder.image.attachments[index]
		if attachment.target != id || attachment.kind != lookupAttachmentInclude || !lookupAttachmentActive(builder.activations, attachment.id) {
			continue
		}
		if err := builder.appendNode(attachment.module); err != nil {
			return err
		}
	}
	*mark = 2
	if (node.kind == lookupNodeClass || node.kind == lookupNodeSingleton) && node.superclass != 0 {
		return builder.appendNode(node.superclass)
	}
	return nil
}

func resolveLookupRoute(image *lookupImage, entries []lookupEntry, route []lookupNodeID, selector selectorID, overlayOwner lookupNodeID, overlay lookupEntry) (lookupReceipt, error) {
	if image == nil || len(route) == 0 || len(route) > maximumLookupDepth || selector == 0 || int(selector) > len(image.selectors) {
		return lookupReceipt{}, errLookupCorrupt
	}
	selectorCount := len(image.selectors)
	for _, node := range route {
		if node == 0 || int(node) > len(image.nodes) {
			return lookupReceipt{}, errLookupCorrupt
		}
		index := (int(node)-1)*selectorCount + int(selector) - 1
		if index < 0 || index >= len(entries) {
			return lookupReceipt{}, errLookupCorrupt
		}
		entry := entries[index]
		if node == overlayOwner {
			entry = overlay
		}
		if !validLookupEntry(image, node, selector, entry) {
			return lookupReceipt{}, errLookupCorrupt
		}
		switch entry.kind {
		case lookupEntryAbsent:
			continue
		case lookupEntryUndef:
			return lookupReceipt{owner: node, slot: entry.slot, outcome: lookupUndef}, nil
		case lookupEntryDefined:
		default:
			return lookupReceipt{}, errLookupCorrupt
		}
		definition, err := image.definition(entry.definition)
		if err != nil || definition.owner != node || definition.selector != selector || definition.slot != entry.slot {
			return lookupReceipt{}, errLookupCorrupt
		}
		return lookupReceipt{owner: node, slot: entry.slot, definition: entry.definition, outcome: lookupResolved, visibility: entry.visibility}, nil
	}
	return lookupReceipt{outcome: lookupMissing}, nil
}

func lookupRouteMatchesOracle(image *lookupImage, activations uint64, row dispatchClassID, route []lookupNodeID) bool {
	if image == nil || row == 0 || len(route) == 0 || len(route) > maximumLookupDepth {
		return false
	}
	left, right := 0, len(image.routeOracles)
	for left < right {
		middle := int(uint(left+right) >> 1)
		oracle := image.routeOracles[middle]
		if oracle.activations < activations || oracle.activations == activations && oracle.row < row {
			left = middle + 1
		} else {
			right = middle
		}
	}
	if left >= len(image.routeOracles) {
		return false
	}
	oracle := image.routeOracles[left]
	if oracle.activations != activations || oracle.row != row || int(oracle.pathLength) != len(route) {
		return false
	}
	end := uint64(oracle.pathStart) + uint64(oracle.pathLength)
	return end <= uint64(len(image.oraclePaths)) && equalLookupRoute(route, image.oraclePaths[oracle.pathStart:uint32(end)])
}

func equalLookupRoute(left, right []lookupNodeID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func lookupAttachmentBit(id lookupAttachmentID) uint64 {
	if id == 0 || id > maximumLookupAttachments {
		return 0
	}
	return uint64(1) << (id - 1)
}

func lookupAttachmentActive(activations uint64, id lookupAttachmentID) bool {
	bit := lookupAttachmentBit(id)
	return bit != 0 && activations&bit != 0
}

func validLookupActivationMask(image *lookupImage, activations uint64) bool {
	if image == nil || len(image.attachments) > maximumLookupAttachments {
		return false
	}
	if len(image.attachments) == maximumLookupAttachments {
		return true
	}
	return activations < uint64(1)<<len(image.attachments)
}

func (owner *lookupOwner) canonical(dispatch dispatchClassID, selector selectorID) (lookupReceipt, error) {
	if owner == nil || owner.image == nil || !owner.validShape() || dispatch == 0 || int(dispatch) > len(owner.image.rows) ||
		selector == 0 || int(selector) > len(owner.image.selectors) {
		return lookupReceipt{}, errLookupCorrupt
	}
	owner.clearRouteScratch()
	defer owner.clearRouteScratch()
	row := owner.image.rows[dispatch-1]
	length, err := owner.deriveRoute(owner.topology.activations, row)
	if err != nil {
		return lookupReceipt{}, err
	}
	return resolveLookupRoute(owner.image, owner.entries, owner.routeScratch[:length], selector, 0, lookupEntry{})
}

func (owner *lookupOwner) effectiveCell(dispatch dispatchClassID, selector selectorID) (resolutionCell, error) {
	receipt, err := owner.canonical(dispatch, selector)
	if err != nil {
		return resolutionCell{}, err
	}
	row := owner.image.rows[dispatch-1]
	index := int(row.cellStart) + int(selector) - 1
	if index < 0 || index >= len(owner.cells) || owner.cells[index].epoch == 0 || owner.cells[index].receipt != receipt {
		return resolutionCell{}, errLookupCorrupt
	}
	return owner.cells[index], nil
}

func validLookupReceipt(image *lookupImage, selector selectorID, receipt lookupReceipt) bool {
	if receipt.reserved8 != 0 || receipt.reserved32 != 0 || receipt.legality != 0 || receipt.continuation != 0 {
		return false
	}
	switch receipt.outcome {
	case lookupMissing:
		return receipt.owner == 0 && receipt.slot == 0 && receipt.definition == 0 && receipt.visibility == 0
	case lookupResolved:
		definition, err := image.definition(receipt.definition)
		return err == nil && receipt.owner != 0 && receipt.slot != 0 && validLookupVisibility(receipt.visibility) &&
			definition.owner == receipt.owner && definition.selector == selector && definition.slot == receipt.slot
	case lookupUndef:
		return receipt.owner != 0 && int(receipt.owner) <= len(image.nodes) && receipt.slot != 0 && receipt.definition == 0 && receipt.visibility == 0
	default:
		return false
	}
}

func (owner *lookupOwner) clearRouteScratch() {
	if owner == nil {
		return
	}
	clear(owner.routeScratch)
	clear(owner.routeMarks)
}

func (owner *lookupOwner) close() {
	if owner == nil {
		return
	}
	clear(owner.entries)
	clear(owner.cells)
	clear(owner.staged)
	clear(owner.changed)
	clear(owner.marks)
	owner.clearRouteScratch()
	owner.image = nil
	owner.entries = nil
	owner.cells = nil
	owner.staged = nil
	owner.changed = nil
	owner.marks = nil
	owner.topology = lookupTopologyBank{}
	owner.stagedTopology = lookupTopologyBank{}
	owner.routeScratch = nil
	owner.routeMarks = nil
	owner.nextEpoch = 0
}

func lookupImageIdentity(image *lookupImage) [sha256.Size]byte {
	if image == nil {
		return [sha256.Size]byte{}
	}
	hash := sha256.New()
	hash.Write([]byte("github.com/besmpl/ember/internal/rubyproof:lookup-image:v3\x00"))
	writeLookupUint32(hash, image.schema)
	hash.Write(image.attribution[:])
	writeLookupUint32(hash, uint32(len(image.nodes)))
	writeLookupUint32(hash, uint32(len(image.rows)))
	writeLookupUint32(hash, uint32(len(image.selectors)))
	writeLookupUint32(hash, uint32(len(image.definitions)))
	writeLookupUint32(hash, uint32(len(image.attachments)))
	writeLookupUint32(hash, uint32(len(image.mutations)))
	writeLookupUint32(hash, uint32(len(image.routeOracles)))
	writeLookupUint32(hash, uint32(len(image.oraclePaths)))
	writeLookupUint32(hash, uint32(len(image.initialEntries)))
	writeLookupUint32(hash, uint32(len(image.initialReceipts)))
	for _, node := range image.nodes {
		writeLookupUint32(hash, uint32(node.id))
		writeLookupUint32(hash, uint32(node.superclass))
		writeLookupUint32(hash, uint32(node.dispatch))
		writeLookupUint32(hash, node.attachmentStart)
		writeLookupUint32(hash, uint32(node.attachmentCount))
		writeLookupUint32(hash, uint32(node.kind))
		writeLookupUint32(hash, uint32(node.flags))
		writeLookupUint32(hash, node.reserved)
		writeLookupUint32(hash, node.reserved2)
		writeLookupUint32(hash, node.reserved3)
	}
	for _, row := range image.rows {
		writeLookupUint32(hash, uint32(row.id))
		writeLookupUint32(hash, uint32(row.node))
		writeLookupUint32(hash, row.cellStart)
		writeLookupUint32(hash, uint32(row.selectorCount))
		writeLookupUint32(hash, uint32(row.flags))
		writeLookupUint32(hash, row.reserved)
		writeLookupUint32(hash, row.reserved2)
		writeLookupUint32(hash, row.reserved3)
		writeLookupUint32(hash, row.reserved4)
	}
	for _, selector := range image.selectors {
		writeLookupUint32(hash, uint32(selector.id))
		writeLookupUint32(hash, selector.ordinal)
		writeLookupUint64(hash, selector.nameHash)
	}
	for _, definition := range image.definitions {
		writeLookupUint32(hash, uint32(definition.id))
		writeLookupUint32(hash, uint32(definition.owner))
		writeLookupUint32(hash, uint32(definition.selector))
		writeLookupUint32(hash, uint32(definition.slot))
		writeLookupUint32(hash, uint32(definition.body))
	}
	for _, attachment := range image.attachments {
		writeLookupUint32(hash, uint32(attachment.id))
		writeLookupUint32(hash, uint32(attachment.target))
		writeLookupUint32(hash, uint32(attachment.module))
		writeLookupUint32(hash, attachment.ordinal)
		writeLookupUint32(hash, uint32(attachment.kind))
		writeLookupUint32(hash, attachment.reserved32)
	}
	for _, mutation := range image.mutations {
		writeLookupUint32(hash, mutation.id)
		writeLookupUint32(hash, uint32(mutation.owner))
		writeLookupUint32(hash, uint32(mutation.selector))
		writeLookupUint32(hash, uint32(mutation.kind))
		writeLookupUint32(hash, uint32(mutation.attachment))
		writeLookupUint32(hash, mutation.reserved32)
		writeLookupUint64(hash, mutation.beforeActivations)
		writeLookupUint64(hash, mutation.afterActivations)
		writeLookupEntryIdentity(hash, mutation.before)
		writeLookupEntryIdentity(hash, mutation.after)
	}
	for _, oracle := range image.routeOracles {
		writeLookupUint64(hash, oracle.activations)
		writeLookupUint32(hash, uint32(oracle.row))
		writeLookupUint32(hash, oracle.pathStart)
		writeLookupUint32(hash, uint32(oracle.pathLength))
		writeLookupUint32(hash, uint32(oracle.reserved16))
		for _, reserved := range oracle.reserved {
			writeLookupUint32(hash, reserved)
		}
	}
	for _, node := range image.oraclePaths {
		writeLookupUint32(hash, uint32(node))
	}
	for _, entry := range image.initialEntries {
		writeLookupEntryIdentity(hash, entry)
	}
	for _, receipt := range image.initialReceipts {
		writeLookupUint32(hash, uint32(receipt.owner))
		writeLookupUint32(hash, uint32(receipt.slot))
		writeLookupUint32(hash, uint32(receipt.definition))
		writeLookupUint32(hash, uint32(receipt.outcome))
		writeLookupUint32(hash, uint32(receipt.visibility))
		writeLookupUint32(hash, uint32(receipt.legality))
		writeLookupUint32(hash, uint32(receipt.reserved8))
		writeLookupUint32(hash, receipt.continuation)
		writeLookupUint32(hash, receipt.reserved32)
	}
	var out [sha256.Size]byte
	copy(out[:], hash.Sum(nil))
	return out
}

func writeLookupEntryIdentity(hash interface{ Write([]byte) (int, error) }, entry lookupEntry) {
	writeLookupUint32(hash, uint32(entry.slot))
	writeLookupUint32(hash, uint32(entry.definition))
	writeLookupUint32(hash, uint32(entry.kind))
	writeLookupUint32(hash, uint32(entry.visibility))
	writeLookupUint32(hash, entry.reserved)
}

func selectorNameHash(name string) uint64 {
	hash := sha256.Sum256(append([]byte("ruby-selector\x00"), name...))
	return binary.BigEndian.Uint64(hash[:8])
}

func writeLookupUint32(hash interface{ Write([]byte) (int, error) }, value uint32) {
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], value)
	_, _ = hash.Write(raw[:])
}

func writeLookupUint64(hash interface{ Write([]byte) (int, error) }, value uint64) {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], value)
	_, _ = hash.Write(raw[:])
}

func checkedProduct(left, right, maximum int) (int, bool) {
	if left < 0 || right < 0 || left != 0 && right > maximum/left {
		return 0, false
	}
	value := left * right
	return value, value <= maximum
}

const (
	lookupEntryBytes                = 16
	resolutionCellBytes             = 32
	lookupNodeIDBytes               = 4
	changedIndexBytes               = 4
	changeMarkBytes                 = 1
	lookupNodeDescriptorBytes       = 32
	dispatchRowDescriptorBytes      = 32
	selectorDescriptorBytes         = 16
	lookupAttachmentDescriptorBytes = 24
	lookupRouteOracleBytes          = 32
	lookupTopologyBankBytes         = 8
	lookupSiteBytes                 = 64
	lookupOwnerHeaderReserve        = 14_336
)

// lookupAdmissionBytes conservatively charges all mutable owner state, its
// bounded route scratch, the sealed topology facts retained through image, and
// sites. Unlike the superseded persistent-path design, nodes/rows/selectors
// and oracle paths have one immutable copy; only cells and activation banks
// have live/staged copies.
func lookupAdmissionBytes(nodeCount, rowCount, selectorCount, attachmentCount, oracleCount, pathCount, siteCount int) (int, bool) {
	entryCount, ok := checkedProduct(nodeCount, selectorCount, maximumLocalEntries)
	if !ok {
		return 0, false
	}
	cellCount, ok := checkedProduct(rowCount, selectorCount, maximumResolutionCells)
	if !ok || nodeCount < 0 || nodeCount > maximumLookupNodes || attachmentCount < 0 || attachmentCount > maximumLookupAttachments ||
		oracleCount < 0 || oracleCount > maximumLookupRouteOracles || pathCount < 0 || pathCount > maximumLookupPathSlots ||
		siteCount < 0 || siteCount > maximumLookupSites {
		return 0, false
	}
	return sumLookupLedger([][2]int{
		{entryCount, lookupEntryBytes},
		{cellCount * 2, resolutionCellBytes},
		{2, lookupTopologyBankBytes},
		{maximumLookupDepth, lookupNodeIDBytes},
		{nodeCount, changeMarkBytes},
		{cellCount, changedIndexBytes},
		{cellCount, changeMarkBytes},
		{nodeCount, lookupNodeDescriptorBytes},
		{rowCount, dispatchRowDescriptorBytes},
		{selectorCount, selectorDescriptorBytes},
		{attachmentCount, lookupAttachmentDescriptorBytes},
		{oracleCount, lookupRouteOracleBytes},
		{pathCount, lookupNodeIDBytes},
		{siteCount, lookupSiteBytes},
		{1, lookupOwnerHeaderReserve},
	})
}

func maximumLookupAdmissionBytes() (int, bool) {
	return sumLookupLedger([][2]int{
		{maximumLocalEntries, lookupEntryBytes},
		{maximumResolutionCells * 2, resolutionCellBytes},
		{2, lookupTopologyBankBytes},
		{maximumLookupDepth, lookupNodeIDBytes},
		{maximumLookupNodes, changeMarkBytes},
		{maximumResolutionCells, changedIndexBytes},
		{maximumResolutionCells, changeMarkBytes},
		{maximumLookupNodes, lookupNodeDescriptorBytes},
		{maximumDispatchClasses, dispatchRowDescriptorBytes},
		{maximumPreparedSelectors, selectorDescriptorBytes},
		{maximumLookupAttachments, lookupAttachmentDescriptorBytes},
		{maximumLookupRouteOracles, lookupRouteOracleBytes},
		{maximumLookupPathSlots, lookupNodeIDBytes},
		{maximumLookupSites, lookupSiteBytes},
		{1, lookupOwnerHeaderReserve},
	})
}

func sumLookupLedger(components [][2]int) (int, bool) {
	total := 0
	for _, component := range components {
		bytes, ok := checkedProduct(component[0], component[1], maximumLookupOwnerBytes)
		if !ok || total > maximumLookupOwnerBytes-bytes {
			return 0, false
		}
		total += bytes
	}
	return total, total <= maximumLookupOwnerBytes
}

func (image *lookupImage) definition(id definitionID) (definitionDescriptor, error) {
	if image == nil || id == 0 || int(id) > len(image.definitions) {
		return definitionDescriptor{}, fmt.Errorf("%w: definition %d", errLookupCorrupt, id)
	}
	return image.definitions[id-1], nil
}

func (image *lookupImage) mutation(id uint32) (lookupMutationDescriptor, error) {
	if image == nil || id == 0 || int(id) > len(image.mutations) {
		return lookupMutationDescriptor{}, fmt.Errorf("%w: mutation %d", errLookupCorrupt, id)
	}
	return image.mutations[id-1], nil
}

// validObjectBinding relates an object's immutable lookup and layout scalars
// to its ordinary checked class. A singleton dispatch may differ from the
// class dispatch only when its explicit lookup root directly inherits that
// class node. Allocation-site facts decide which object receives that row;
// this boundary merely rejects malformed scalar combinations.
func (image *lookupImage) validObjectBinding(
	classNode lookupNodeID,
	classDispatch dispatchClassID,
	classShape shapeID,
	dispatch dispatchClassID,
	shape shapeID,
) bool {
	if image == nil || classNode == 0 || int(classNode) > len(image.nodes) || classDispatch == 0 || int(classDispatch) > len(image.rows) ||
		classShape == 0 || dispatch == 0 || int(dispatch) > len(image.rows) || shape != classShape {
		return false
	}
	class := image.nodes[classNode-1]
	if class.kind != lookupNodeClass || class.dispatch != classDispatch || image.rows[classDispatch-1].node != classNode {
		return false
	}
	rootNode := image.rows[dispatch-1].node
	if rootNode == 0 || int(rootNode) > len(image.nodes) {
		return false
	}
	root := image.nodes[rootNode-1]
	if dispatch == classDispatch {
		return root.id == classNode && root.kind == lookupNodeClass
	}
	return root.kind == lookupNodeSingleton && root.dispatch == dispatch && root.superclass == classNode
}

func validLookupEntry(image *lookupImage, owner lookupNodeID, selector selectorID, entry lookupEntry) bool {
	if image == nil || owner == 0 || int(owner) > len(image.nodes) || selector == 0 || int(selector) > len(image.selectors) ||
		entry.reserved8 != [2]byte{} || entry.reserved != 0 {
		return false
	}
	coordinateSlot := func(slot methodSlotID) bool {
		if slot == 0 {
			return false
		}
		for _, definition := range image.definitions {
			if definition.owner == owner && definition.selector == selector && definition.slot == slot {
				return true
			}
		}
		return false
	}
	switch entry.kind {
	case lookupEntryAbsent:
		return entry.definition == 0 && entry.visibility == 0 && (entry.slot == 0 || coordinateSlot(entry.slot))
	case lookupEntryDefined:
		definition, err := image.definition(entry.definition)
		return err == nil && entry.slot != 0 && validLookupVisibility(entry.visibility) &&
			definition.owner == owner && definition.selector == selector && definition.slot == entry.slot
	case lookupEntryUndef:
		return entry.definition == 0 && entry.visibility == 0 && coordinateSlot(entry.slot)
	default:
		return false
	}
}

func validLookupVisibility(visibility lookupVisibility) bool {
	return visibility >= lookupVisibilityPublic && visibility <= lookupVisibilityPrivate
}
