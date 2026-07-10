package ember

import "fmt"

type functionDraft struct {
	constants []Value
	assembly  assembledBytecodeIR
	children  []*functionDraft
	upvalues  []upvalueDesc
	registers int
	params    int
	variadic  bool
}

func newFunctionDraft(constants []Value, assembly assembledBytecodeIR, children []*functionDraft, upvalues []upvalueDesc, registers int, params int, variadic bool) *functionDraft {
	return &functionDraft{
		constants: constants,
		assembly:  assembly,
		children:  children,
		upvalues:  upvalues,
		registers: registers,
		params:    params,
		variadic:  variadic,
	}
}

func (c *compiler) addFunctionDraft(draft *functionDraft) int {
	index := len(c.prototypeDrafts)
	c.prototypeDrafts = append(c.prototypeDrafts, draft)
	return index
}

func (c *compiler) optimizeFunction(options optimizationOptions) {
	c.ir = optimizeBytecodeIRWithFacts(c.ir, bytecodeIROptimizationFacts{
		constants:         c.constants,
		capturedRegisters: functionDraftCapturedRegisters(c.prototypeDrafts),
		constantPool:      &c.bytecodeBuilder,
	}, options)
}

func functionDraftCapturedRegisters(children []*functionDraft) []bool {
	var captured []bool
	for _, child := range children {
		if child == nil {
			continue
		}
		for _, desc := range child.upvalues {
			if !desc.local || desc.copy || desc.index < 0 {
				continue
			}
			for len(captured) <= desc.index {
				captured = append(captured, false)
			}
			captured[desc.index] = true
		}
	}
	return captured
}

func sealFunctionDraft(draft *functionDraft) (*Proto, error) {
	if draft == nil {
		return nil, fmt.Errorf("invalid finalized prototype: nil function draft")
	}

	var children []*Proto
	if len(draft.children) != 0 {
		children = make([]*Proto, len(draft.children))
	}
	for index, childDraft := range draft.children {
		child, err := sealFunctionDraft(childDraft)
		if err != nil {
			return nil, err
		}
		children[index] = child
	}

	proto := &Proto{
		constants:  draft.constants,
		code:       draft.assembly.code,
		prototypes: children,
		upvalues:   draft.upvalues,
		lines:      draft.assembly.lines,
		registers:  draft.registers,
		params:     draft.params,
		variadic:   draft.variadic,
	}
	if err := sealFunctionProto(proto, &draft.assembly); err != nil {
		return nil, fmt.Errorf("invalid finalized prototype: %w", err)
	}
	return proto, nil
}

func sealFunctionProto(proto *Proto, assembly *assembledBytecodeIR) error {
	assignProtoGlobalSlots(proto)
	artifact := buildExecutionArtifact(proto)
	artifact.apply(proto)
	markReusableZeroCaptureClosures(proto)
	if err := assembly.pack(); err != nil {
		proto.verifyErr = err
		return err
	}
	proto.packedCode = assembly.packedCode
	proto.verifyErr = verifyFunctionProto(proto)
	return proto.verifyErr
}

func verifyFunctionProto(proto *Proto) error {
	sealedChildren := make(map[*Proto]bool, len(proto.prototypes))
	for _, child := range proto.prototypes {
		sealedChildren[child] = true
	}
	return verifyProtoSeen(proto, sealedChildren)
}
