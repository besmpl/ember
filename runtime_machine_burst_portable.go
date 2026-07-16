//go:build !darwin || !arm64

package ember

func runMachineBurstBackend(control *machineBurstControl, _ *machineBurstRegion, _ []machineBurstOperation, _ []machineBurstGuard, _ []slot, _ []uint64) (machineBurstControl, bool) {
	return *control, false
}
