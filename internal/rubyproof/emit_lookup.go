package rubyproof

import "fmt"

type lookupNodeEmission struct {
	ID, Superclass, AttachmentStart, AttachmentCount, Kind uint32
}

type lookupRowEmission struct {
	ID, Node uint32
}

type lookupAttachmentEmission struct {
	ID, Target, Module, Kind uint32
}

type lookupRouteEmission struct {
	Activations uint64
	Row         uint32
	Nodes       []uint32
}

type lookupDefinitionEmission struct {
	ID, Owner, Selector, Slot, Body uint32
}

type lookupEntryEmission struct {
	Slot, Definition, Kind, Visibility uint32
}

type lookupMutationEmission struct {
	ID, Owner, Selector, Kind, Attachment uint32
	BeforeActivations, AfterActivations   uint64
	Before, After                         lookupEntryEmission
}

type lookupTargetEmission struct {
	ID, Selector, Definition, Slot, Body uint32
}

type lookupTargetMatchEmission struct {
	Target, Dispatch, Shape, Selector, Definition, Slot uint32
}

// lookupEmissionPlan is the exact two-selector projection needed by the
// prepared proof. Ruby keeps ownership of topology and lookup semantics:
// generated code receives immutable node/attachment descriptors, entry
// mutations, and target identities. Independent route oracles are validated
// here and remain compiler/test proof only: no flattened route reaches the
// generated artifact or a warmed PIC hit.
type lookupEmissionPlan struct {
	NodeCount, RowCount, SelectorCount, AttachmentCount             uint32
	BaseNode, AlphaNode, BetaNode, GammaNode                        uint32
	IncludedNode, PrependedNode, DeltaNode, SingletonNode           uint32
	AlphaDispatch, BetaDispatch, GammaDispatch                      uint32
	DeltaDispatch, SingletonDispatch                                uint32
	AlphaShape, BetaShape, GammaShape, DeltaShape                   uint32
	LabelSelector, SavedSelector                                    uint32
	ReadLabelSite, ReadSavedSite, ReadPublicSite, ReadSingletonSite uint32
	BaseLabelInitialDefinition                                      uint32
	BetaLabelDefinition, GammaLabelDefinition                       uint32
	BaseLabelRedefinedDefinition, SavedLabelDefinition              uint32
	IncludedLabelInitialDefinition                                  uint32
	PrependedLabelDefinition                                        uint32
	IncludedLabelRedefinedDefinition                                uint32
	SingletonLabelDefinition                                        uint32
	BaseRedefineMutation                                            uint32
	BaseProtectedMutation, BasePrivateMutation                      uint32
	BasePublicMutation, BetaRemoveMutation                          uint32
	GammaUndefMutation                                              uint32
	IncludedLabelInitialMutation, AlphaIncludeMutation              uint32
	BetaPrependMutation, PrependedLabelMutation                     uint32
	IncludedLabelRedefinedMutation                                  uint32
	SingletonDefineMutation                                         uint32
	Nodes                                                           []lookupNodeEmission
	Rows                                                            []lookupRowEmission
	Attachments                                                     []lookupAttachmentEmission
	Routes                                                          []lookupRouteEmission
	Definitions                                                     []lookupDefinitionEmission
	InitialDefinitions                                              []uint32
	Mutations                                                       []lookupMutationEmission
	Targets                                                         []lookupTargetEmission
	TargetMatches                                                   []lookupTargetMatchEmission
}

func buildLookupEmissionPlan(program *checkedProgram) (lookupEmissionPlan, error) {
	if program == nil || program.lookup == nil {
		return lookupEmissionPlan{}, fmt.Errorf("ruby: checked lookup facts are unavailable")
	}
	if err := validateLookupImage(program.lookup); err != nil {
		return lookupEmissionPlan{}, fmt.Errorf("ruby: checked lookup facts are invalid: %w", err)
	}
	base, alpha := program.classes["Base"], program.classes["Alpha"]
	beta, gamma := program.classes["Beta"], program.classes["Gamma"]
	included, prepended := program.classes["IncludedLabel"], program.classes["PrependedLabel"]
	delta, singleton := program.classes["Delta"], program.singletonOwner
	if base == nil || alpha == nil || beta == nil || gamma == nil || included == nil || prepended == nil || delta == nil ||
		singleton == nil || singleton.kind != checkedOwnerSingleton || singleton.superclass != alpha || singleton.shape != alpha.shape || singleton.dispatch == 0 ||
		base.kind != checkedOwnerClass || alpha.kind != checkedOwnerClass || beta.kind != checkedOwnerClass || gamma.kind != checkedOwnerClass || delta.kind != checkedOwnerClass ||
		included.kind != checkedOwnerModule || prepended.kind != checkedOwnerModule || alpha.superclass != base || beta.superclass != base || gamma.superclass != base || delta.superclass != alpha {
		return lookupEmissionPlan{}, fmt.Errorf("ruby: checked owner roles are outside prepared proof")
	}
	label, saved := program.selectors["label"], program.selectors["saved_label"]
	baseLabels, includedLabels := base.methods["label"], included.methods["label"]
	if label == 0 || saved == 0 || label == saved || len(baseLabels) != 2 || len(beta.methods["label"]) != 1 || len(gamma.methods["label"]) != 1 ||
		len(includedLabels) != 2 || len(prepended.methods["label"]) != 1 {
		return lookupEmissionPlan{}, fmt.Errorf("ruby: checked lookup roles are incomplete")
	}
	betaLabel, gammaLabel := beta.methods["label"][0], gamma.methods["label"][0]
	prependedLabel := prepended.methods["label"][0]

	readSite := func(name string, selector selectorID, mode checkedCallMode) (callSiteID, error) {
		methods := program.topMethods[name]
		if len(methods) != 1 || len(methods[0].declaration.body) != 1 {
			return 0, fmt.Errorf("ruby: checked %s role is incomplete", name)
		}
		statement := methods[0].declaration.body[0]
		call := statement.expression
		if name == "read_public" {
			if statement.kind != statementBegin || len(statement.body) != 1 {
				return 0, fmt.Errorf("ruby: checked %s role is incomplete", name)
			}
			call = statement.body[0].expression
		}
		fact, ok := program.calls[call]
		if !ok || fact.kind != checkedCallReceiver || fact.mode != mode || fact.selector != selector || fact.site == 0 {
			return 0, fmt.Errorf("ruby: checked %s site fact is invalid", name)
		}
		return fact.site, nil
	}
	readLabelSite, err := readSite("read_label", label, checkedCallAnyVisibility)
	if err != nil {
		return lookupEmissionPlan{}, err
	}
	readSavedSite, err := readSite("read_saved", saved, checkedCallPublicVisibility)
	if err != nil {
		return lookupEmissionPlan{}, err
	}
	readPublicSite, err := readSite("read_public", label, checkedCallPublicVisibility)
	if err != nil {
		return lookupEmissionPlan{}, err
	}
	readSingletonSite, err := readSite("read_singleton", label, checkedCallAnyVisibility)
	if err != nil {
		return lookupEmissionPlan{}, err
	}

	var alias, remove, undef, includeOperation, prependOperation *checkedClassOperation
	var visibility []*checkedClassOperation
	for _, operation := range program.orderedOperations {
		switch operation.kind {
		case lookupMutationAlias:
			alias = operation
		case lookupMutationRemove:
			remove = operation
		case lookupMutationUndef:
			undef = operation
		case lookupMutationVisibility:
			visibility = append(visibility, operation)
		case lookupMutationInclude:
			includeOperation = operation
		case lookupMutationPrepend:
			prependOperation = operation
		}
	}
	baseRedefine := program.classOperations[baseLabels[1].declaration]
	includedInitial := program.classOperations[includedLabels[0].declaration]
	prependedDefine := program.classOperations[prependedLabel.declaration]
	includedRedefine := program.classOperations[includedLabels[1].declaration]
	singletonDefine := program.singletonOperation
	if alias == nil || alias.owner != base || alias.name != "saved_label" || alias.definition == nil ||
		remove == nil || remove.owner != beta || remove.name != "label" ||
		undef == nil || undef.owner != gamma || undef.name != "label" || baseRedefine == nil || len(visibility) != 3 ||
		includedInitial == nil || prependedDefine == nil || includedRedefine == nil || includeOperation == nil || prependOperation == nil ||
		includeOperation.owner != alpha || includeOperation.attachment == nil || includeOperation.attachment.module != included ||
		prependOperation.owner != beta || prependOperation.attachment == nil || prependOperation.attachment.module != prepended ||
		singletonDefine == nil || singletonDefine.owner != singleton || singletonDefine.definition != program.singletonDefinition || singletonDefine.kind != lookupMutationDefine {
		return lookupEmissionPlan{}, fmt.Errorf("ruby: checked mutation roles are incomplete")
	}
	if visibility[0].visibility != lookupVisibilityProtected || visibility[1].visibility != lookupVisibilityPrivate || visibility[2].visibility != lookupVisibilityPublic {
		return lookupEmissionPlan{}, fmt.Errorf("ruby: checked visibility roles are incomplete")
	}

	plan := lookupEmissionPlan{
		NodeCount:                        uint32(len(program.lookup.nodes)),
		RowCount:                         uint32(len(program.lookup.rows)),
		SelectorCount:                    2,
		AttachmentCount:                  uint32(len(program.lookup.attachments)),
		BaseNode:                         uint32(base.node),
		AlphaNode:                        uint32(alpha.node),
		BetaNode:                         uint32(beta.node),
		GammaNode:                        uint32(gamma.node),
		IncludedNode:                     uint32(included.node),
		PrependedNode:                    uint32(prepended.node),
		DeltaNode:                        uint32(delta.node),
		SingletonNode:                    uint32(singleton.node),
		AlphaDispatch:                    uint32(alpha.dispatch),
		BetaDispatch:                     uint32(beta.dispatch),
		GammaDispatch:                    uint32(gamma.dispatch),
		DeltaDispatch:                    uint32(delta.dispatch),
		SingletonDispatch:                uint32(singleton.dispatch),
		AlphaShape:                       uint32(alpha.shape),
		BetaShape:                        uint32(beta.shape),
		GammaShape:                       uint32(gamma.shape),
		DeltaShape:                       uint32(delta.shape),
		LabelSelector:                    uint32(label),
		SavedSelector:                    uint32(saved),
		ReadLabelSite:                    uint32(readLabelSite),
		ReadSavedSite:                    uint32(readSavedSite),
		ReadPublicSite:                   uint32(readPublicSite),
		ReadSingletonSite:                uint32(readSingletonSite),
		BaseLabelInitialDefinition:       uint32(baseLabels[0].definition),
		BetaLabelDefinition:              uint32(betaLabel.definition),
		GammaLabelDefinition:             uint32(gammaLabel.definition),
		BaseLabelRedefinedDefinition:     uint32(baseLabels[1].definition),
		SavedLabelDefinition:             uint32(alias.definition.id),
		IncludedLabelInitialDefinition:   uint32(includedLabels[0].definition),
		PrependedLabelDefinition:         uint32(prependedLabel.definition),
		IncludedLabelRedefinedDefinition: uint32(includedLabels[1].definition),
		SingletonLabelDefinition:         uint32(program.singletonDefinition.id),
		BaseRedefineMutation:             baseRedefine.id,
		BaseProtectedMutation:            visibility[0].id,
		BasePrivateMutation:              visibility[1].id,
		BasePublicMutation:               visibility[2].id,
		BetaRemoveMutation:               remove.id,
		GammaUndefMutation:               undef.id,
		IncludedLabelInitialMutation:     includedInitial.id,
		AlphaIncludeMutation:             includeOperation.id,
		BetaPrependMutation:              prependOperation.id,
		PrependedLabelMutation:           prependedDefine.id,
		IncludedLabelRedefinedMutation:   includedRedefine.id,
		SingletonDefineMutation:          singletonDefine.id,
	}
	for _, node := range program.lookup.nodes {
		plan.Nodes = append(plan.Nodes, lookupNodeEmission{
			ID: uint32(node.id), Superclass: uint32(node.superclass), AttachmentStart: node.attachmentStart,
			AttachmentCount: uint32(node.attachmentCount), Kind: uint32(node.kind),
		})
	}
	for _, row := range program.lookup.rows {
		plan.Rows = append(plan.Rows, lookupRowEmission{ID: uint32(row.id), Node: uint32(row.node)})
	}
	for _, attachment := range program.lookup.attachments {
		plan.Attachments = append(plan.Attachments, lookupAttachmentEmission{
			ID: uint32(attachment.id), Target: uint32(attachment.target), Module: uint32(attachment.module), Kind: uint32(attachment.kind),
		})
	}
	for _, oracle := range program.lookup.routeOracles {
		end := uint64(oracle.pathStart) + uint64(oracle.pathLength)
		if end > uint64(len(program.lookup.oraclePaths)) {
			return lookupEmissionPlan{}, fmt.Errorf("ruby: checked route oracle is outside its path table")
		}
		route := lookupRouteEmission{Activations: oracle.activations, Row: uint32(oracle.row)}
		for _, node := range program.lookup.oraclePaths[oracle.pathStart:uint32(end)] {
			route.Nodes = append(route.Nodes, uint32(node))
		}
		plan.Routes = append(plan.Routes, route)
	}

	checkedDefinitions := []*checkedDefinition{
		definitionForMethod(program, baseLabels[0]),
		definitionForMethod(program, betaLabel),
		definitionForMethod(program, gammaLabel),
		definitionForMethod(program, baseLabels[1]),
		alias.definition,
		definitionForMethod(program, includedLabels[0]),
		definitionForMethod(program, prependedLabel),
		definitionForMethod(program, includedLabels[1]),
		program.singletonDefinition,
	}
	for _, checked := range checkedDefinitions {
		if checked == nil {
			return lookupEmissionPlan{}, fmt.Errorf("ruby: checked definition role is unavailable")
		}
		definition, err := program.lookup.definition(checked.id)
		if err != nil || definition.owner != checked.owner.node || definition.selector != checked.selector || definition.slot != checked.slot || definition.body != checked.body.definition {
			return lookupEmissionPlan{}, fmt.Errorf("ruby: checked definition %d disagrees with sealed lookup facts", checked.id)
		}
		plan.Definitions = append(plan.Definitions, lookupDefinitionEmission{
			ID: uint32(definition.id), Owner: uint32(definition.owner), Selector: uint32(definition.selector), Slot: uint32(definition.slot), Body: uint32(definition.body),
		})
	}
	plan.InitialDefinitions = []uint32{
		uint32(baseLabels[0].definition), uint32(alias.definition.id), uint32(betaLabel.definition), uint32(gammaLabel.definition),
	}
	projectedOperations := []*checkedClassOperation{
		baseRedefine, visibility[0], visibility[1], visibility[2], remove, undef,
		includedInitial, includeOperation, prependOperation, prependedDefine, includedRedefine,
		singletonDefine,
	}
	for _, operation := range projectedOperations {
		mutation, err := program.lookup.mutation(operation.id)
		if err != nil {
			return lookupEmissionPlan{}, err
		}
		plan.Mutations = append(plan.Mutations, lookupMutationEmission{
			ID: uint32(mutation.id), Owner: uint32(mutation.owner), Selector: uint32(mutation.selector), Kind: uint32(mutation.kind),
			Attachment: uint32(mutation.attachment), BeforeActivations: mutation.beforeActivations, AfterActivations: mutation.afterActivations,
			Before: emitLookupEntry(mutation.before), After: emitLookupEntry(mutation.after),
		})
	}
	plan.Targets = []lookupTargetEmission{
		{ID: 1, Selector: uint32(label), Definition: uint32(baseLabels[0].definition), Slot: uint32(baseLabels[0].slot), Body: uint32(baseLabels[0].definition)},
		{ID: 2, Selector: uint32(label), Definition: uint32(betaLabel.definition), Slot: uint32(betaLabel.slot), Body: uint32(betaLabel.definition)},
		{ID: 3, Selector: uint32(label), Definition: uint32(gammaLabel.definition), Slot: uint32(gammaLabel.slot), Body: uint32(gammaLabel.definition)},
		{ID: 4, Selector: uint32(label), Definition: uint32(baseLabels[1].definition), Slot: uint32(baseLabels[1].slot), Body: uint32(baseLabels[1].definition)},
		{ID: 5, Selector: uint32(saved), Definition: uint32(alias.definition.id), Slot: uint32(alias.definition.slot), Body: uint32(baseLabels[0].definition)},
		{ID: 6, Selector: uint32(label), Definition: uint32(includedLabels[0].definition), Slot: uint32(includedLabels[0].slot), Body: uint32(includedLabels[0].definition)},
		{ID: 7, Selector: uint32(label), Definition: uint32(prependedLabel.definition), Slot: uint32(prependedLabel.slot), Body: uint32(prependedLabel.definition)},
		{ID: 8, Selector: uint32(label), Definition: uint32(includedLabels[1].definition), Slot: uint32(includedLabels[1].slot), Body: uint32(includedLabels[1].definition)},
		{ID: 9, Selector: uint32(label), Definition: uint32(program.singletonDefinition.id), Slot: uint32(program.singletonDefinition.slot), Body: uint32(program.singletonDefinition.body.definition)},
	}
	plan.TargetMatches = []lookupTargetMatchEmission{
		{Target: 1, Dispatch: uint32(alpha.dispatch), Shape: uint32(alpha.shape), Selector: uint32(label), Definition: uint32(baseLabels[0].definition), Slot: uint32(baseLabels[0].slot)},
		{Target: 2, Dispatch: uint32(beta.dispatch), Shape: uint32(beta.shape), Selector: uint32(label), Definition: uint32(betaLabel.definition), Slot: uint32(betaLabel.slot)},
		{Target: 3, Dispatch: uint32(gamma.dispatch), Shape: uint32(gamma.shape), Selector: uint32(label), Definition: uint32(gammaLabel.definition), Slot: uint32(gammaLabel.slot)},
		{Target: 4, Dispatch: uint32(alpha.dispatch), Shape: uint32(alpha.shape), Selector: uint32(label), Definition: uint32(baseLabels[1].definition), Slot: uint32(baseLabels[1].slot)},
		{Target: 4, Dispatch: uint32(beta.dispatch), Shape: uint32(beta.shape), Selector: uint32(label), Definition: uint32(baseLabels[1].definition), Slot: uint32(baseLabels[1].slot)},
		{Target: 5, Dispatch: uint32(alpha.dispatch), Shape: uint32(alpha.shape), Selector: uint32(saved), Definition: uint32(alias.definition.id), Slot: uint32(alias.definition.slot)},
		{Target: 4, Dispatch: uint32(delta.dispatch), Shape: uint32(delta.shape), Selector: uint32(label), Definition: uint32(baseLabels[1].definition), Slot: uint32(baseLabels[1].slot)},
		{Target: 6, Dispatch: uint32(delta.dispatch), Shape: uint32(delta.shape), Selector: uint32(label), Definition: uint32(includedLabels[0].definition), Slot: uint32(includedLabels[0].slot)},
		{Target: 7, Dispatch: uint32(beta.dispatch), Shape: uint32(beta.shape), Selector: uint32(label), Definition: uint32(prependedLabel.definition), Slot: uint32(prependedLabel.slot)},
		{Target: 8, Dispatch: uint32(delta.dispatch), Shape: uint32(delta.shape), Selector: uint32(label), Definition: uint32(includedLabels[1].definition), Slot: uint32(includedLabels[1].slot)},
		{Target: 8, Dispatch: uint32(alpha.dispatch), Shape: uint32(alpha.shape), Selector: uint32(label), Definition: uint32(includedLabels[1].definition), Slot: uint32(includedLabels[1].slot)},
		{Target: 8, Dispatch: uint32(singleton.dispatch), Shape: uint32(singleton.shape), Selector: uint32(label), Definition: uint32(includedLabels[1].definition), Slot: uint32(includedLabels[1].slot)},
		{Target: 9, Dispatch: uint32(singleton.dispatch), Shape: uint32(singleton.shape), Selector: uint32(label), Definition: uint32(program.singletonDefinition.id), Slot: uint32(program.singletonDefinition.slot)},
	}
	if err := validateLookupEmissionPlan(plan); err != nil {
		return lookupEmissionPlan{}, err
	}
	return plan, nil
}

func definitionForMethod(program *checkedProgram, method *checkedMethod) *checkedDefinition {
	if program == nil || method == nil || method.definition == 0 || int(method.definition) > len(program.definitions) {
		return nil
	}
	definition := program.definitions[method.definition-1]
	if definition.body != method {
		return nil
	}
	return definition
}

func emitLookupEntry(entry lookupEntry) lookupEntryEmission {
	return lookupEntryEmission{Slot: uint32(entry.slot), Definition: uint32(entry.definition), Kind: uint32(entry.kind), Visibility: uint32(entry.visibility)}
}

func validateLookupEmissionPlan(plan lookupEmissionPlan) error {
	if plan.NodeCount != 8 || plan.RowCount != 5 || plan.SelectorCount != 2 || plan.AttachmentCount != 2 ||
		plan.BaseNode == 0 || plan.AlphaNode == 0 || plan.BetaNode == 0 || plan.GammaNode == 0 || plan.IncludedNode == 0 || plan.PrependedNode == 0 || plan.DeltaNode == 0 || plan.SingletonNode == 0 ||
		plan.AlphaDispatch == 0 || plan.BetaDispatch == 0 || plan.GammaDispatch == 0 || plan.DeltaDispatch == 0 || plan.SingletonDispatch == 0 ||
		plan.AlphaShape == 0 || plan.BetaShape == 0 || plan.GammaShape == 0 || plan.DeltaShape == 0 ||
		plan.LabelSelector == 0 || plan.SavedSelector == 0 || plan.LabelSelector == plan.SavedSelector ||
		plan.ReadLabelSite == 0 || plan.ReadSavedSite == 0 || plan.ReadPublicSite == 0 || plan.ReadSingletonSite == 0 ||
		plan.ReadLabelSite == plan.ReadSavedSite || plan.ReadLabelSite == plan.ReadPublicSite || plan.ReadLabelSite == plan.ReadSingletonSite ||
		plan.ReadSavedSite == plan.ReadPublicSite || plan.ReadSavedSite == plan.ReadSingletonSite || plan.ReadPublicSite == plan.ReadSingletonSite ||
		len(plan.Nodes) != 8 || len(plan.Rows) != 5 || len(plan.Attachments) != 2 || len(plan.Routes) != 20 || len(plan.Definitions) != 9 ||
		len(plan.InitialDefinitions) != 4 || len(plan.Mutations) != 12 || len(plan.Targets) != 9 || len(plan.TargetMatches) != 13 {
		return fmt.Errorf("ruby: prepared lookup plan is incomplete")
	}
	wantNodeIDs := []uint32{plan.BaseNode, plan.AlphaNode, plan.BetaNode, plan.GammaNode, plan.IncludedNode, plan.PrependedNode, plan.DeltaNode, plan.SingletonNode}
	for index, id := range wantNodeIDs {
		if id != uint32(index+1) {
			return fmt.Errorf("ruby: prepared owner role %d is out of order", index+1)
		}
	}
	wantNodes := []lookupNodeEmission{
		{ID: plan.BaseNode, Kind: uint32(lookupNodeClass)},
		{ID: plan.AlphaNode, Superclass: plan.BaseNode, AttachmentCount: 1, Kind: uint32(lookupNodeClass)},
		{ID: plan.BetaNode, Superclass: plan.BaseNode, AttachmentStart: 1, AttachmentCount: 1, Kind: uint32(lookupNodeClass)},
		{ID: plan.GammaNode, Superclass: plan.BaseNode, AttachmentStart: 2, Kind: uint32(lookupNodeClass)},
		{ID: plan.IncludedNode, AttachmentStart: 2, Kind: uint32(lookupNodeModule)},
		{ID: plan.PrependedNode, AttachmentStart: 2, Kind: uint32(lookupNodeModule)},
		{ID: plan.DeltaNode, Superclass: plan.AlphaNode, AttachmentStart: 2, Kind: uint32(lookupNodeClass)},
		{ID: plan.SingletonNode, Superclass: plan.AlphaNode, AttachmentStart: 2, Kind: uint32(lookupNodeSingleton)},
	}
	for index, node := range plan.Nodes {
		if node != wantNodes[index] {
			return fmt.Errorf("ruby: prepared node %d is incompatible with its checked role", index+1)
		}
	}
	wantRows := []lookupRowEmission{
		{ID: plan.AlphaDispatch, Node: plan.AlphaNode},
		{ID: plan.BetaDispatch, Node: plan.BetaNode},
		{ID: plan.GammaDispatch, Node: plan.GammaNode},
		{ID: plan.DeltaDispatch, Node: plan.DeltaNode},
		{ID: plan.SingletonDispatch, Node: plan.SingletonNode},
	}
	for index, row := range plan.Rows {
		if row != wantRows[index] || row.ID != uint32(index+1) {
			return fmt.Errorf("ruby: prepared dispatch row %d is incompatible with its checked role", index+1)
		}
	}
	wantAttachments := []lookupAttachmentEmission{
		{ID: 1, Target: plan.AlphaNode, Module: plan.IncludedNode, Kind: uint32(lookupAttachmentInclude)},
		{ID: 2, Target: plan.BetaNode, Module: plan.PrependedNode, Kind: uint32(lookupAttachmentPrepend)},
	}
	for index, attachment := range plan.Attachments {
		if attachment != wantAttachments[index] {
			return fmt.Errorf("ruby: prepared attachment %d is incompatible with its checked role", index+1)
		}
	}
	wantRoutes := []lookupRouteEmission{
		{Activations: 0, Row: plan.AlphaDispatch, Nodes: []uint32{plan.AlphaNode, plan.BaseNode}},
		{Activations: 0, Row: plan.BetaDispatch, Nodes: []uint32{plan.BetaNode, plan.BaseNode}},
		{Activations: 0, Row: plan.GammaDispatch, Nodes: []uint32{plan.GammaNode, plan.BaseNode}},
		{Activations: 0, Row: plan.DeltaDispatch, Nodes: []uint32{plan.DeltaNode, plan.AlphaNode, plan.BaseNode}},
		{Activations: 0, Row: plan.SingletonDispatch, Nodes: []uint32{plan.SingletonNode, plan.AlphaNode, plan.BaseNode}},
		{Activations: 1, Row: plan.AlphaDispatch, Nodes: []uint32{plan.AlphaNode, plan.IncludedNode, plan.BaseNode}},
		{Activations: 1, Row: plan.BetaDispatch, Nodes: []uint32{plan.BetaNode, plan.BaseNode}},
		{Activations: 1, Row: plan.GammaDispatch, Nodes: []uint32{plan.GammaNode, plan.BaseNode}},
		{Activations: 1, Row: plan.DeltaDispatch, Nodes: []uint32{plan.DeltaNode, plan.AlphaNode, plan.IncludedNode, plan.BaseNode}},
		{Activations: 1, Row: plan.SingletonDispatch, Nodes: []uint32{plan.SingletonNode, plan.AlphaNode, plan.IncludedNode, plan.BaseNode}},
		{Activations: 2, Row: plan.AlphaDispatch, Nodes: []uint32{plan.AlphaNode, plan.BaseNode}},
		{Activations: 2, Row: plan.BetaDispatch, Nodes: []uint32{plan.PrependedNode, plan.BetaNode, plan.BaseNode}},
		{Activations: 2, Row: plan.GammaDispatch, Nodes: []uint32{plan.GammaNode, plan.BaseNode}},
		{Activations: 2, Row: plan.DeltaDispatch, Nodes: []uint32{plan.DeltaNode, plan.AlphaNode, plan.BaseNode}},
		{Activations: 2, Row: plan.SingletonDispatch, Nodes: []uint32{plan.SingletonNode, plan.AlphaNode, plan.BaseNode}},
		{Activations: 3, Row: plan.AlphaDispatch, Nodes: []uint32{plan.AlphaNode, plan.IncludedNode, plan.BaseNode}},
		{Activations: 3, Row: plan.BetaDispatch, Nodes: []uint32{plan.PrependedNode, plan.BetaNode, plan.BaseNode}},
		{Activations: 3, Row: plan.GammaDispatch, Nodes: []uint32{plan.GammaNode, plan.BaseNode}},
		{Activations: 3, Row: plan.DeltaDispatch, Nodes: []uint32{plan.DeltaNode, plan.AlphaNode, plan.IncludedNode, plan.BaseNode}},
		{Activations: 3, Row: plan.SingletonDispatch, Nodes: []uint32{plan.SingletonNode, plan.AlphaNode, plan.IncludedNode, plan.BaseNode}},
	}
	for index, route := range plan.Routes {
		want := wantRoutes[index]
		if route.Activations != want.Activations || route.Row != want.Row || len(route.Nodes) != len(want.Nodes) {
			return fmt.Errorf("ruby: prepared route oracle %d is incompatible with its checked role", index+1)
		}
		for nodeIndex := range route.Nodes {
			if route.Nodes[nodeIndex] != want.Nodes[nodeIndex] {
				return fmt.Errorf("ruby: prepared route oracle %d node %d is incompatible with its checked role", index+1, nodeIndex+1)
			}
		}
	}

	definitions := make(map[uint32]lookupDefinitionEmission, len(plan.Definitions))
	for _, definition := range plan.Definitions {
		if definition.ID == 0 || definition.Owner == 0 || definition.Owner > plan.NodeCount ||
			(definition.Selector != plan.LabelSelector && definition.Selector != plan.SavedSelector) || definition.Slot == 0 || definition.Body == 0 {
			return fmt.Errorf("ruby: prepared definition %d is invalid", definition.ID)
		}
		if _, duplicate := definitions[definition.ID]; duplicate {
			return fmt.Errorf("ruby: duplicate prepared definition %d", definition.ID)
		}
		definitions[definition.ID] = definition
	}
	wantDefinitionIDs := []uint32{
		plan.BaseLabelInitialDefinition, plan.BetaLabelDefinition, plan.GammaLabelDefinition, plan.BaseLabelRedefinedDefinition,
		plan.SavedLabelDefinition, plan.IncludedLabelInitialDefinition, plan.PrependedLabelDefinition, plan.IncludedLabelRedefinedDefinition,
		plan.SingletonLabelDefinition,
	}
	for _, id := range wantDefinitionIDs {
		if _, ok := definitions[id]; !ok {
			return fmt.Errorf("ruby: prepared definition %d is unavailable", id)
		}
	}
	if definitions[plan.SavedLabelDefinition].Body != plan.BaseLabelInitialDefinition || definitions[plan.SavedLabelDefinition].Selector != plan.SavedSelector {
		return fmt.Errorf("ruby: prepared alias does not snapshot Base label v1")
	}
	wantInitial := []uint32{plan.BaseLabelInitialDefinition, plan.SavedLabelDefinition, plan.BetaLabelDefinition, plan.GammaLabelDefinition}
	for index, id := range plan.InitialDefinitions {
		if id != wantInitial[index] {
			return fmt.Errorf("ruby: prepared initial definition %d is out of order", index+1)
		}
	}
	wantMutationIDs := []uint32{
		plan.BaseRedefineMutation, plan.BaseProtectedMutation, plan.BasePrivateMutation, plan.BasePublicMutation,
		plan.BetaRemoveMutation, plan.GammaUndefMutation, plan.IncludedLabelInitialMutation, plan.AlphaIncludeMutation,
		plan.BetaPrependMutation, plan.PrependedLabelMutation, plan.IncludedLabelRedefinedMutation,
		plan.SingletonDefineMutation,
	}
	for index, mutation := range plan.Mutations {
		if mutation.ID != wantMutationIDs[index] || mutation.Owner == 0 || mutation.Owner > plan.NodeCount {
			return fmt.Errorf("ruby: prepared mutation %d is invalid", index+1)
		}
		if mutation.Kind == uint32(lookupMutationInclude) || mutation.Kind == uint32(lookupMutationPrepend) {
			if mutation.Selector != 0 || mutation.Attachment == 0 || mutation.Before != (lookupEntryEmission{}) || mutation.After != (lookupEntryEmission{}) {
				return fmt.Errorf("ruby: prepared topology mutation %d is invalid", index+1)
			}
		} else if mutation.Selector != plan.LabelSelector && mutation.Selector != plan.SavedSelector || mutation.Attachment != 0 || mutation.BeforeActivations != mutation.AfterActivations {
			return fmt.Errorf("ruby: prepared entry mutation %d is invalid", index+1)
		}
	}
	if plan.Mutations[6].BeforeActivations != 0 || plan.Mutations[6].AfterActivations != 0 ||
		plan.Mutations[7].Kind != uint32(lookupMutationInclude) || plan.Mutations[7].Attachment != 1 || plan.Mutations[7].BeforeActivations != 0 || plan.Mutations[7].AfterActivations != 1 ||
		plan.Mutations[8].Kind != uint32(lookupMutationPrepend) || plan.Mutations[8].Attachment != 2 || plan.Mutations[8].BeforeActivations != 1 || plan.Mutations[8].AfterActivations != 3 ||
		plan.Mutations[9].BeforeActivations != 3 || plan.Mutations[9].AfterActivations != 3 || plan.Mutations[10].BeforeActivations != 3 || plan.Mutations[10].AfterActivations != 3 ||
		plan.Mutations[11].Kind != uint32(lookupMutationDefine) || plan.Mutations[11].Owner != plan.SingletonNode || plan.Mutations[11].BeforeActivations != 3 || plan.Mutations[11].AfterActivations != 3 ||
		plan.Mutations[11].Before != (lookupEntryEmission{}) || plan.Mutations[11].After.Definition != plan.SingletonLabelDefinition || plan.Mutations[11].After.Visibility != uint32(lookupVisibilityPublic) {
		return fmt.Errorf("ruby: prepared topology schedule is invalid")
	}
	wantTargets := []lookupTargetEmission{
		{ID: 1, Selector: plan.LabelSelector, Definition: plan.BaseLabelInitialDefinition, Slot: definitions[plan.BaseLabelInitialDefinition].Slot, Body: plan.BaseLabelInitialDefinition},
		{ID: 2, Selector: plan.LabelSelector, Definition: plan.BetaLabelDefinition, Slot: definitions[plan.BetaLabelDefinition].Slot, Body: plan.BetaLabelDefinition},
		{ID: 3, Selector: plan.LabelSelector, Definition: plan.GammaLabelDefinition, Slot: definitions[plan.GammaLabelDefinition].Slot, Body: plan.GammaLabelDefinition},
		{ID: 4, Selector: plan.LabelSelector, Definition: plan.BaseLabelRedefinedDefinition, Slot: definitions[plan.BaseLabelRedefinedDefinition].Slot, Body: plan.BaseLabelRedefinedDefinition},
		{ID: 5, Selector: plan.SavedSelector, Definition: plan.SavedLabelDefinition, Slot: definitions[plan.SavedLabelDefinition].Slot, Body: plan.BaseLabelInitialDefinition},
		{ID: 6, Selector: plan.LabelSelector, Definition: plan.IncludedLabelInitialDefinition, Slot: definitions[plan.IncludedLabelInitialDefinition].Slot, Body: plan.IncludedLabelInitialDefinition},
		{ID: 7, Selector: plan.LabelSelector, Definition: plan.PrependedLabelDefinition, Slot: definitions[plan.PrependedLabelDefinition].Slot, Body: plan.PrependedLabelDefinition},
		{ID: 8, Selector: plan.LabelSelector, Definition: plan.IncludedLabelRedefinedDefinition, Slot: definitions[plan.IncludedLabelRedefinedDefinition].Slot, Body: plan.IncludedLabelRedefinedDefinition},
		{ID: 9, Selector: plan.LabelSelector, Definition: plan.SingletonLabelDefinition, Slot: definitions[plan.SingletonLabelDefinition].Slot, Body: plan.SingletonLabelDefinition},
	}
	for index, target := range plan.Targets {
		if target != wantTargets[index] {
			return fmt.Errorf("ruby: prepared target %d is incompatible with its checked role", index+1)
		}
	}
	wantMatches := []lookupTargetMatchEmission{
		{Target: 1, Dispatch: plan.AlphaDispatch, Shape: plan.AlphaShape, Selector: plan.LabelSelector, Definition: plan.BaseLabelInitialDefinition, Slot: definitions[plan.BaseLabelInitialDefinition].Slot},
		{Target: 2, Dispatch: plan.BetaDispatch, Shape: plan.BetaShape, Selector: plan.LabelSelector, Definition: plan.BetaLabelDefinition, Slot: definitions[plan.BetaLabelDefinition].Slot},
		{Target: 3, Dispatch: plan.GammaDispatch, Shape: plan.GammaShape, Selector: plan.LabelSelector, Definition: plan.GammaLabelDefinition, Slot: definitions[plan.GammaLabelDefinition].Slot},
		{Target: 4, Dispatch: plan.AlphaDispatch, Shape: plan.AlphaShape, Selector: plan.LabelSelector, Definition: plan.BaseLabelRedefinedDefinition, Slot: definitions[plan.BaseLabelRedefinedDefinition].Slot},
		{Target: 4, Dispatch: plan.BetaDispatch, Shape: plan.BetaShape, Selector: plan.LabelSelector, Definition: plan.BaseLabelRedefinedDefinition, Slot: definitions[plan.BaseLabelRedefinedDefinition].Slot},
		{Target: 5, Dispatch: plan.AlphaDispatch, Shape: plan.AlphaShape, Selector: plan.SavedSelector, Definition: plan.SavedLabelDefinition, Slot: definitions[plan.SavedLabelDefinition].Slot},
		{Target: 4, Dispatch: plan.DeltaDispatch, Shape: plan.DeltaShape, Selector: plan.LabelSelector, Definition: plan.BaseLabelRedefinedDefinition, Slot: definitions[plan.BaseLabelRedefinedDefinition].Slot},
		{Target: 6, Dispatch: plan.DeltaDispatch, Shape: plan.DeltaShape, Selector: plan.LabelSelector, Definition: plan.IncludedLabelInitialDefinition, Slot: definitions[plan.IncludedLabelInitialDefinition].Slot},
		{Target: 7, Dispatch: plan.BetaDispatch, Shape: plan.BetaShape, Selector: plan.LabelSelector, Definition: plan.PrependedLabelDefinition, Slot: definitions[plan.PrependedLabelDefinition].Slot},
		{Target: 8, Dispatch: plan.DeltaDispatch, Shape: plan.DeltaShape, Selector: plan.LabelSelector, Definition: plan.IncludedLabelRedefinedDefinition, Slot: definitions[plan.IncludedLabelRedefinedDefinition].Slot},
		{Target: 8, Dispatch: plan.AlphaDispatch, Shape: plan.AlphaShape, Selector: plan.LabelSelector, Definition: plan.IncludedLabelRedefinedDefinition, Slot: definitions[plan.IncludedLabelRedefinedDefinition].Slot},
		{Target: 8, Dispatch: plan.SingletonDispatch, Shape: plan.AlphaShape, Selector: plan.LabelSelector, Definition: plan.IncludedLabelRedefinedDefinition, Slot: definitions[plan.IncludedLabelRedefinedDefinition].Slot},
		{Target: 9, Dispatch: plan.SingletonDispatch, Shape: plan.AlphaShape, Selector: plan.LabelSelector, Definition: plan.SingletonLabelDefinition, Slot: definitions[plan.SingletonLabelDefinition].Slot},
	}
	for index, match := range plan.TargetMatches {
		if match != wantMatches[index] {
			return fmt.Errorf("ruby: prepared target match %d is incompatible with its checked role", index+1)
		}
	}
	return nil
}
