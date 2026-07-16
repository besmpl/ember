package ember

// backendRegisterSet is the private compiler's deterministic register bitset.
// It is owner-neutral and contains no runtime pointers.
type backendRegisterSet []uint64

func newBackendRegisterSet(registers int) backendRegisterSet {
	if registers <= 0 {
		return nil
	}
	return make(backendRegisterSet, (registers+63)/64)
}

func (set backendRegisterSet) add(register int) {
	if register < 0 || register/64 >= len(set) {
		return
	}
	set[register/64] |= uint64(1) << uint(register%64)
}

func (set backendRegisterSet) remove(register int) {
	if register < 0 || register/64 >= len(set) {
		return
	}
	set[register/64] &^= uint64(1) << uint(register%64)
}

func (set backendRegisterSet) has(register int) bool {
	return register >= 0 &&
		register/64 < len(set) &&
		set[register/64]&(uint64(1)<<uint(register%64)) != 0
}

func (set backendRegisterSet) clone() backendRegisterSet {
	return append(backendRegisterSet(nil), set...)
}

func (set backendRegisterSet) equal(other backendRegisterSet) bool {
	if len(set) != len(other) {
		return false
	}
	for index := range set {
		if set[index] != other[index] {
			return false
		}
	}
	return true
}

func (set backendRegisterSet) union(other backendRegisterSet) bool {
	changed := false
	for index := range set {
		before := set[index]
		set[index] |= other[index]
		changed = changed || set[index] != before
	}
	return changed
}

func (set backendRegisterSet) subtract(other backendRegisterSet) {
	for index := range set {
		set[index] &^= other[index]
	}
}

type backendEffect uint16

const (
	backendEffectCall backendEffect = 1 << iota
	backendEffectYield
	backendEffectError
	backendEffectAllocate
	backendEffectReadGlobal
	backendEffectWriteGlobal
	backendEffectReadUpvalue
	backendEffectWriteUpvalue
	backendEffectReadTable
	backendEffectWriteTable
	backendEffectReadUnknownHeap
	backendEffectWriteUnknownHeap
)

type backendExitPolicy uint8

const (
	backendExitNone backendExitPolicy = iota
	backendExitBeforeOperation
)

type backendValueID uint32

const invalidBackendValueID backendValueID = 0

type backendValueKind uint8

const (
	backendValueUndef backendValueKind = iota
	backendValueParameter
	backendValueOperation
	backendValuePhi
)

type backendTagMask uint16

const (
	backendTagNil backendTagMask = 1 << iota
	backendTagBool
	backendTagNumber
	backendTagString
	backendTagTable
	backendTagUserData
	backendTagFunction
	backendTagHostFunction
)

const backendTagAny = backendTagNil |
	backendTagBool |
	backendTagNumber |
	backendTagString |
	backendTagTable |
	backendTagUserData |
	backendTagFunction |
	backendTagHostFunction

type backendRepresentation uint8

const (
	backendRepresentationGeneric backendRepresentation = iota
	backendRepresentationNil
	backendRepresentationBool
	backendRepresentationNumber
	backendRepresentationString
	backendRepresentationTable
	backendRepresentationFunction
)

type backendObjectKind uint8

const (
	backendObjectNone backendObjectKind = iota
	backendObjectTable
	backendObjectClosure
	backendObjectString
	backendObjectMixed
)

type backendCallKind uint8

const (
	backendCallNone backendCallKind = iota
	backendCallDirectProto
	backendCallDirectNative
	backendCallGuarded
	backendCallDynamic
)

type backendCallIR struct {
	kind        backendCallKind
	targetProto int32
	nativeID    int32
}

type backendAccessKind uint8

const (
	backendAccessNone backendAccessKind = iota
	backendAccessGlobal
	backendAccessStaticProperty
	backendAccessDynamicIndex
	backendAccessArrayIteration
	backendAccessMetatableGuard
)

type backendAccessIR struct {
	kind        backendAccessKind
	constant    int32
	globalIndex int32
}

type backendValueIR struct {
	id             backendValueID
	kind           backendValueKind
	register       int32
	block          int32
	pc             int32
	tags           backendTagMask
	representation backendRepresentation
	object         backendObjectKind
	fromVararg     bool
	escapes        bool
	origins        []backendValueID
	targetProtos   []int32
	targetUnknown  bool
}

type backendValueRef struct {
	register int32
	value    backendValueID
}

type backendPhiIR struct {
	register int32
	value    backendValueID
	inputs   []backendValueID
}

type backendPhiCopyIR struct {
	register    int32
	source      backendValueID
	destination backendValueID
}

type backendEdgeIR struct {
	id        int32
	from      int32
	to        int32
	critical  bool
	phiCopies []backendPhiCopyIR
}

type backendOperationIR struct {
	op           opcode
	pc           int32
	wordPC       int32
	line         int32
	block        int32
	targetPC     int32
	a            int32
	b            int32
	c            int32
	d            int32
	targetProto  int32
	callArgStart int32
	callArgCount int32
	callPrefix   int32
	callResults  int32
	returnCount  int32
	tailCall     uint8
	globalIndex  int32
	nativeID     int32
	guardField   machineStringID
	guestCharge  uint8
	tailCharge   uint8
	errorClass   opcodeMachineErrorClass
	effects      backendEffect
	exit         backendExitPolicy
	reads        backendRegisterSet
	writes       backendRegisterSet
	liveBefore   backendRegisterSet
	liveAfter    backendRegisterSet
	spill        backendRegisterSet
	uses         []backendValueRef
	defs         []backendValueRef
	call         backendCallIR
	access       backendAccessIR
}

type backendBlockIR struct {
	id                 int32
	first              int32
	last               int32
	predecessors       []int32
	successors         []int32
	immediateDominator int32
	reachable          bool
	loopHeader         bool
	guestCharge        uint64
	use                backendRegisterSet
	def                backendRegisterSet
	liveIn             backendRegisterSet
	liveOut            backendRegisterSet
	dominators         []uint64
	phis               []backendPhiIR
	entryValues        []backendValueID
	exitValues         []backendValueID
}

type backendProtoIR struct {
	registers                int
	params                   int
	variadic                 bool
	maxResults               int
	detachable               bool
	requiresOwner            bool
	requiresNumericCoercion  bool
	requiresGeneratedStrings bool
	sourceName               string
	functionName             string
	blocks                   []backendBlockIR
	ops                      []backendOperationIR
	pcToBlock                []int32
	values                   []backendValueIR
	initial                  []backendValueID
	constants                []machineConstant
	upvalues                 []machineUpvalue
	edges                    []backendEdgeIR
}
