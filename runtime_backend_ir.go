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

type backendOperationIR struct {
	op          opcode
	pc          int32
	wordPC      int32
	line        int32
	block       int32
	targetPC    int32
	guestCharge uint8
	effects     backendEffect
	exit        backendExitPolicy
	reads       backendRegisterSet
	writes      backendRegisterSet
	liveBefore  backendRegisterSet
	liveAfter   backendRegisterSet
	spill       backendRegisterSet
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
}

type backendProtoIR struct {
	registers int
	params    int
	variadic  bool
	blocks    []backendBlockIR
	ops       []backendOperationIR
	pcToBlock []int32
}
