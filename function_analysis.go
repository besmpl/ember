package ember

type functionIR struct {
	instructions []bytecodeIRInstruction
	revision     uint64
	analysis     *functionAnalysis
}

type functionAnalysis struct {
	revision     uint64
	blocks       []bytecodeIRBlock
	successors   [][]int
	predecessors [][]int
	reachable    []bool
	use          []registerSet
	def          []registerSet
	liveness     []bytecodeIRLivenessBlock
	effects      []opcodeEffects
}

func newFunctionIR(ir []bytecodeIRInstruction) *functionIR {
	return &functionIR{instructions: ir}
}

func (function *functionIR) replace(ir []bytecodeIRInstruction) {
	if function == nil {
		return
	}
	if !equalBytecodeIR(function.instructions, ir) {
		function.revision++
		function.analysis = nil
	}
	function.instructions = ir
}

func (function *functionIR) currentAnalysis() *functionAnalysis {
	if function == nil {
		return nil
	}
	if function.analysis == nil || function.analysis.revision != function.revision {
		function.analysis = analyzeBytecodeIR(function.instructions, function.revision)
	}
	return function.analysis
}

func equalBytecodeIR(left []bytecodeIRInstruction, right []bytecodeIRInstruction) bool {
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

func analyzeBytecodeIR(ir []bytecodeIRInstruction, revision uint64) *functionAnalysis {
	blocks := bytecodeIRBlockOrder(ir)
	successors := bytecodeIRBlockSuccessors(ir, blocks)
	liveness := bytecodeIRLivenessForGraph(ir, blocks, successors)
	analysis := &functionAnalysis{
		revision:     revision,
		blocks:       blocks,
		successors:   successors,
		predecessors: bytecodeIRBlockPredecessors(successors),
		reachable:    bytecodeIRReachableBlocks(successors),
		use:          make([]registerSet, len(liveness)),
		def:          make([]registerSet, len(liveness)),
		liveness:     liveness,
		effects:      make([]opcodeEffects, len(ir)),
	}
	for block := range liveness {
		analysis.use[block] = liveness[block].use
		analysis.def[block] = liveness[block].def
	}
	for pc, ins := range ir {
		analysis.effects[pc] = opcodeEffect(ins.op)
	}
	return analysis
}

func bytecodeIRBlockPredecessors(successors [][]int) [][]int {
	predecessors := make([][]int, len(successors))
	for block, next := range successors {
		for _, successor := range next {
			if successor >= 0 && successor < len(predecessors) {
				predecessors[successor] = append(predecessors[successor], block)
			}
		}
	}
	return predecessors
}

func bytecodeIRReachableBlocks(successors [][]int) []bool {
	if len(successors) == 0 {
		return nil
	}
	reachable := make([]bool, len(successors))
	worklist := []int{0}
	reachable[0] = true
	for len(worklist) != 0 {
		last := len(worklist) - 1
		block := worklist[last]
		worklist = worklist[:last]
		for _, successor := range successors[block] {
			if successor < 0 || successor >= len(reachable) || reachable[successor] {
				continue
			}
			reachable[successor] = true
			worklist = append(worklist, successor)
		}
	}
	return reachable
}
