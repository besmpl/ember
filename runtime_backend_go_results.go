package ember

const backendGoMaxFixedResultCount = 32

func backendGoNumericFixedResultCount(ir *backendProtoIR) (int, bool) {
	if ir == nil {
		return 0, false
	}
	resultCount := 0
	for blockIndex := range ir.blocks {
		block := &ir.blocks[blockIndex]
		if !block.reachable {
			continue
		}
		for pc := block.first; pc < block.last; pc++ {
			operation := &ir.ops[pc]
			count := 0
			switch operation.op {
			case opReturnOne:
				count = 1
			case opReturn:
				count = int(operation.returnCount)
			default:
				continue
			}
			if count <= 0 || count > backendGoMaxFixedResultCount {
				return 0, false
			}
			if resultCount != 0 && resultCount != count {
				return 0, false
			}
			resultCount = count
		}
	}
	return resultCount, resultCount != 0
}
