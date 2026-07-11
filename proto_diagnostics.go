package ember

type protoDiagnosticFacts struct {
	numericForLoops     []numericForLoopDesc
	intrinsicOps        []intrinsicOpDesc
	constantKindFacts   []constantKindFactDesc
	registerKindFacts   []registerKindFactDesc
	numericOperandFacts []numericOperandFactDesc
	slotKindFacts       []slotKindFactDesc
}

func deriveProtoDiagnosticFacts(proto *Proto) protoDiagnosticFacts {
	if proto == nil {
		return protoDiagnosticFacts{}
	}
	return protoDiagnosticFacts{
		numericForLoops:     detectNumericForLoops(proto.code),
		intrinsicOps:        detectIntrinsicOps(proto.code),
		constantKindFacts:   detectConstantKindFacts(proto.constants),
		registerKindFacts:   detectRegisterKindFacts(proto),
		numericOperandFacts: detectNumericOperandFacts(proto),
		slotKindFacts:       detectSlotKindFacts(proto),
	}
}
