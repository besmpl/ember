package ember

import "fmt"

// PreparedContext is the opaque execution context supplied to generated
// prepared functions. Its methods expose guarded reads and exact replay exits;
// it does not expose mutable Machine storage.
type PreparedContext struct {
	machine *scalarMachine
	target  *machineProto
}

// PreparedExit describes how a generated prepared function returns control to
// Ember. Construct exits with the PreparedContext methods or the exported
// Prepared functions in this file.
type PreparedExit struct {
	kind       machinePreparedExitKind
	pc         int32
	spillCount uint32
	numberBits uint64
}

// PreparedFunction is one generated function in a PreparedBundle.
// Applications should treat prepared functions as trusted build artifacts.
type PreparedFunction func(PreparedContext) PreparedExit

// PreparedBundle is an immutable generated-code descriptor for one exact
// Program. A bundle is safe to share across Runtime owners.
type PreparedBundle struct {
	program machinePreparedProgram
}

// PreparedBundleError reports that a supplied PreparedBundle does not match
// the Program being bound. Rebuild the generated bundle from the same Program
// inputs rather than falling back silently.
type PreparedBundleError struct {
	reason string
}

func (err *PreparedBundleError) Error() string {
	if err == nil {
		return "ember: invalid prepared bundle"
	}
	return "ember: invalid prepared bundle: " + err.reason
}

// NewPreparedBundle copies a generated descriptor. Function inventories are
// ordered by module and then Proto ID. Validation happens when NewRuntime binds
// the bundle to its exact Program.
func NewPreparedBundle(
	abiVersion uint32,
	semanticVersion uint32,
	programHash [32]byte,
	functions [][]PreparedFunction,
) *PreparedBundle {
	modules := make([]machinePreparedModule, len(functions))
	for moduleIndex := range functions {
		moduleFunctions := make([]machinePreparedFunction, len(functions[moduleIndex]))
		for protoIndex, function := range functions[moduleIndex] {
			moduleFunctions[protoIndex] = machinePreparedFunction(function)
		}
		modules[moduleIndex] = machinePreparedModule{
			moduleID:  programModuleID(moduleIndex),
			functions: moduleFunctions,
		}
	}
	return &PreparedBundle{program: machinePreparedProgram{
		abiVersion:      abiVersion,
		semanticVersion: semanticVersion,
		programHash:     programHash,
		modules:         modules,
	}}
}

func (bundle *PreparedBundle) machineProgram() *machinePreparedProgram {
	if bundle == nil {
		return nil
	}
	return &bundle.program
}

func preparedBundleErrorf(format string, args ...any) error {
	return &PreparedBundleError{reason: fmt.Sprintf(format, args...)}
}

// NumberParameter returns one numeric argument when its generated guard holds.
func (context PreparedContext) NumberParameter(index int) (float64, bool) {
	return context.numberParameter(index)
}

// IntrinsicUnchanged reports whether the intrinsic guarded at pc still has its
// original owner-local identity.
func (context PreparedContext) IntrinsicUnchanged(pc int32) bool {
	return context.intrinsicUnchanged(pc)
}

// IntrinsicUnchangedAt is IntrinsicUnchanged for another Proto in the active
// module.
func (context PreparedContext) IntrinsicUnchangedAt(protoID, pc int32) bool {
	return context.intrinsicUnchangedAt(protoID, pc)
}

// ReplayBeforeOperation returns an exact pre-operation slow-path exit after
// spill values have been staged.
func (context PreparedContext) ReplayBeforeOperation(pc int32, spillCount int) PreparedExit {
	return context.replayBeforeOperation(pc, spillCount)
}

// SpillNil stages a nil value for an exact pre-operation replay.
func (context PreparedContext) SpillNil(index int, register int32) {
	context.spillNil(index, register)
}

// SpillBool stages a Boolean value for an exact pre-operation replay.
func (context PreparedContext) SpillBool(index int, register int32, value bool) {
	context.spillBool(index, register, value)
}

// SpillNumber stages a numeric value for an exact pre-operation replay.
func (context PreparedContext) SpillNumber(index int, register int32, value float64) {
	context.spillNumber(index, register, value)
}

// PreparedReplayEntry returns control to the canonical Machine at function
// entry without committing generated state.
func PreparedReplayEntry() PreparedExit {
	return machinePreparedReplayEntry()
}

// PreparedReturnOneNumber commits one numeric result.
func PreparedReturnOneNumber(number float64) PreparedExit {
	return machinePreparedReturnOneNumber(number)
}
