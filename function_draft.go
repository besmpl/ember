package ember

import "fmt"

type functionDraft struct {
	constants    []Value
	assembly     assembledBytecodeIR
	children     []*functionDraft
	upvalues     []upvalueDesc
	registers    int
	params       int
	variadic     bool
	sourceName   string
	functionName string
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
	proto, err := sealFunctionDraftNode(draft)
	if err != nil {
		return nil, err
	}
	proto.compact = buildCompactCallProgram(proto)
	return proto, nil
}

func sealFunctionDraftNode(draft *functionDraft) (*Proto, error) {
	if draft == nil {
		return nil, fmt.Errorf("invalid finalized prototype: nil function draft")
	}

	var children []*Proto
	if len(draft.children) != 0 {
		children = make([]*Proto, len(draft.children))
	}
	for index, childDraft := range draft.children {
		child, err := sealFunctionDraftNode(childDraft)
		if err != nil {
			return nil, err
		}
		children[index] = child
	}

	proto := &Proto{
		constants:  draft.constants,
		prototypes: children,
		upvalues:   draft.upvalues,
		lines:      draft.assembly.lines,
		registers:  draft.registers,
		params:     draft.params,
		variadic:   draft.variadic,
		debugInfo: &protoDebugInfo{
			sourceName:   draft.sourceName,
			functionName: draft.functionName,
		},
	}
	if err := sealFunctionProto(proto, &draft.assembly); err != nil {
		return nil, fmt.Errorf("invalid finalized prototype: %w", err)
	}
	return proto, nil
}

func sealFunctionProto(proto *Proto, assembly *assembledBytecodeIR) error {
	assignProtoGlobalSlots(proto, assembly.code)
	assembly.code = normalizeOpenResultCallMarkers(assembly.code)
	assembly.code = normalizeOpenArgumentCallMarkers(assembly.code)
	assembly.code = normalizeFixedMultiResultCounts(assembly.code, proto.registers)
	artifact := buildExecutionArtifact(proto, assembly.code)
	artifact.apply(proto)
	assembly.code = markBorrowableFixedCallWindows(assembly.code, proto.registers, artifact.capturedLocals)
	markReusableZeroCaptureClosures(proto, assembly.code)
	proto.verifyErr = verifyFunctionProtoWithCode(proto, assembly.code)
	if proto.verifyErr != nil {
		return proto.verifyErr
	}
	if err := encodeProtoWords(proto, assembly.code); err != nil {
		proto.verifyErr = err
		return err
	}
	return proto.verifyErr
}

func verifyFunctionProto(proto *Proto) error {
	code, err := protoDecodedInstructions(proto)
	if err != nil {
		return err
	}
	return verifyFunctionProtoWithCode(proto, code)
}

func verifyFunctionProtoWithCode(proto *Proto, code []instruction) error {
	sealedChildren := make(map[*Proto]bool, len(proto.prototypes))
	for _, child := range proto.prototypes {
		sealedChildren[child] = true
	}
	return verifyProtoSeenWithCode(proto, sealedChildren, code)
}
