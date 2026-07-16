//go:build darwin && arm64

package ember

import "runtime"

//go:noescape
func runMachineBurstArm64(control *machineBurstControl, region *machineBurstRegion, operations *machineBurstOperation, operationCount uintptr, guards *machineBurstGuard, guardCount uintptr, registers *slot, registerCount uintptr, numberBits *uint64, numberCount uintptr, workspace *uint64)

func runMachineBurstBackend(control *machineBurstControl, region *machineBurstRegion, operations []machineBurstOperation, guards []machineBurstGuard, registers []slot, numberBits []uint64) (machineBurstControl, bool) {
	// The first half is the numeric register file; the second half records
	// destinations that must be published before returning to Go.
	var workspace [machineBurstWorkspaceRegisters * 2]uint64
	regionOperations := operations[region.operationStart : region.operationStart+region.operationCount]
	regionGuards := guards[region.guardStart : region.guardStart+region.guardCount]
	runMachineBurstArm64(
		control,
		region,
		&regionOperations[0],
		uintptr(len(regionOperations)),
		machineBurstGuardBase(regionGuards),
		uintptr(len(regionGuards)),
		&registers[0],
		uintptr(len(registers)),
		&numberBits[0],
		uintptr(len(numberBits)),
		&workspace[0],
	)
	runtime.KeepAlive(region)
	runtime.KeepAlive(control)
	runtime.KeepAlive(operations)
	runtime.KeepAlive(guards)
	runtime.KeepAlive(registers)
	runtime.KeepAlive(numberBits)
	runtime.KeepAlive(&workspace)
	return *control, true
}

func machineBurstGuardBase(guards []machineBurstGuard) *machineBurstGuard {
	if len(guards) == 0 {
		return nil
	}
	return &guards[0]
}
