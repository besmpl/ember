package ember

import "testing"

func TestBackendNativeARM64FrameStaysWithinImmediatePage(t *testing.T) {
	accepted := &backendNativeCandidate{ir: &backendProtoIR{values: make([]backendValueIR, 509)}}
	if frame, err := planBackendNativeARM64Frame(accepted); err != nil {
		t.Fatalf("plan page-safe ARM64 frame: %v", err)
	} else if frame.size > backendNativeARM64MaximumFrameSize {
		t.Fatalf("page-safe ARM64 frame = %d, maximum %d", frame.size, backendNativeARM64MaximumFrameSize)
	}

	oversized := &backendNativeCandidate{ir: &backendProtoIR{values: make([]backendValueIR, 510)}}
	if _, err := planBackendNativeARM64Frame(oversized); err == nil {
		t.Fatal("planned ARM64 frame above the immediate page ceiling")
	}
}

func TestBackendNativeX8664FrameStaysWithinUnprobedPage(t *testing.T) {
	accepted := &backendNativeCandidate{ir: &backendProtoIR{values: make([]backendValueIR, 500)}}
	if frame, err := planBackendNativeX8664Frame(accepted); err != nil {
		t.Fatalf("plan page-safe x86-64 frame: %v", err)
	} else if frame.size > backendNativeX8664MaximumFrameSize {
		t.Fatalf("page-safe x86-64 frame = %d, maximum %d", frame.size, backendNativeX8664MaximumFrameSize)
	}

	oversized := &backendNativeCandidate{ir: &backendProtoIR{values: make([]backendValueIR, 512)}}
	if _, err := planBackendNativeX8664Frame(oversized); err == nil {
		t.Fatal("planned x86-64 frame that requires stack probing")
	}
}

func TestBackendNativeStackBudgetPrunesUnsafeCallGraphs(t *testing.T) {
	t.Run("short direct chain", func(t *testing.T) {
		candidates := []*backendNativeCandidate{
			{dependencies: []int32{1}},
			{},
		}
		pruneBackendNativeStackCandidates(candidates, []int{1024, 1024})
		if candidates[0] == nil || candidates[1] == nil {
			t.Fatal("pruned a call chain within the native stack budget")
		}
	})

	t.Run("aggregate overflow", func(t *testing.T) {
		const count = backendNativeMaximumStackPathBytes/4096 + 1
		candidates := make([]*backendNativeCandidate, count)
		frames := make([]int, count)
		for index := range candidates {
			candidates[index] = &backendNativeCandidate{}
			frames[index] = 4096
			if index+1 < count {
				candidates[index].dependencies = []int32{int32(index + 1)}
			}
		}
		pruneBackendNativeStackCandidates(candidates, frames)
		if candidates[0] != nil {
			t.Fatal("kept a direct call chain above the native stack budget")
		}
		if candidates[count-1] == nil {
			t.Fatal("pruned the safe leaf along with its unsafe caller")
		}
	})

	t.Run("bounded self recursion", func(t *testing.T) {
		candidates := []*backendNativeCandidate{{
			options:      backendGoNumericOptions{selfRecursive: true},
			dependencies: []int32{0},
		}}
		pruneBackendNativeStackCandidates(candidates, []int{1024})
		if candidates[0] == nil {
			t.Fatal("pruned bounded recursion within the native stack budget")
		}
	})

	t.Run("recursive overflow", func(t *testing.T) {
		candidates := []*backendNativeCandidate{{
			options:      backendGoNumericOptions{selfRecursive: true},
			dependencies: []int32{0},
		}}
		pruneBackendNativeStackCandidates(candidates, []int{3000})
		if candidates[0] != nil {
			t.Fatal("kept bounded recursion above the native stack budget")
		}
	})

	t.Run("mutual cycle", func(t *testing.T) {
		candidates := []*backendNativeCandidate{
			{dependencies: []int32{1}},
			{dependencies: []int32{0}},
		}
		pruneBackendNativeStackCandidates(candidates, []int{64, 64})
		if candidates[0] != nil || candidates[1] != nil {
			t.Fatal("kept a native call-graph cycle without a recursion proof")
		}
	})

	t.Run("unavailable dependency", func(t *testing.T) {
		candidates := []*backendNativeCandidate{
			{dependencies: []int32{1}},
			nil,
		}
		pruneBackendNativeStackCandidates(candidates, []int{64, 0})
		if candidates[0] != nil {
			t.Fatal("kept a caller whose native dependency was unavailable")
		}
	})
}
