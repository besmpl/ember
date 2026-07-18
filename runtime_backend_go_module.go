package ember

import (
	"fmt"
	"strconv"
)

type backendGoNumericModuleOptions struct {
	packageName         string
	functionPrefix      string
	preparedImportPath  string
	preparedQualifier   string
	coroutineDeadString machineStringID
}

type backendGoNumericModule struct {
	files     []backendGoNumericProgramFile
	functions []string
}

// emitBackendGoNumericModule owns naming and emission for one complete module.
// Every direct Proto body is emitted at most once; eligible prepared wrappers
// share those stable bodies instead of cloning their reachable helper graphs.
func emitBackendGoNumericModule(
	irs []*backendProtoIR,
	options backendGoNumericModuleOptions,
) (backendGoNumericModule, error) {
	if len(irs) == 0 {
		return backendGoNumericModule{}, fmt.Errorf("emit backend Go numeric module: empty Proto inventory")
	}
	protoIDs := make([]int32, 0, len(irs)-1)
	for protoID := 1; protoID < len(irs); protoID++ {
		protoIDs = append(protoIDs, int32(protoID))
	}
	targets, err := inferBackendGoNumericTargets(irs, protoIDs, options.functionPrefix)
	if err != nil {
		return backendGoNumericModule{}, fmt.Errorf("emit backend Go numeric module: %w", err)
	}
	upvalueTargets := inferBackendGoNumericProgramUpvalueTargets(irs, protoIDs, targets)
	common := backendGoNumericOptions{
		packageName:          options.packageName,
		directTargets:        targets,
		targetUpvalueTargets: upvalueTargets,
		preparedImportPath:   options.preparedImportPath,
		preparedQualifier:    options.preparedQualifier,
		coroutineDeadString:  options.coroutineDeadString,
	}

	descriptorOnly := make([]bool, len(irs))
	embeddedCoroutine := make([]bool, len(irs))
	for _, protoID := range protoIDs {
		if _, ok := backendGoNumericClosureFactory(irs[protoID], targets); ok {
			descriptorOnly[protoID] = true
			continue
		}
		protoOptions := backendGoNumericTargetOptions(common, targets[protoID])
		if int(protoID) < len(upvalueTargets) {
			protoOptions.upvalueTargets = upvalueTargets[protoID]
		}
		plan, planErr := buildBackendGoNumericPlan(irs[protoID], protoOptions)
		if planErr != nil {
			continue
		}
		if plan.coroutines.enabled && plan.coroutines.targetProto >= 0 && int(plan.coroutines.targetProto) < len(irs) {
			embeddedCoroutine[plan.coroutines.targetProto] = true
		}
	}

	result := backendGoNumericModule{functions: make([]string, len(irs))}
	for protoIndex := range irs {
		protoID := int32(protoIndex)
		if descriptorOnly[protoID] || embeddedCoroutine[protoID] {
			continue
		}
		reachable, reachableErr := backendGoNumericReachableProtoIDs(irs, protoID)
		if reachableErr != nil {
			return backendGoNumericModule{}, fmt.Errorf("emit backend Go numeric module: Proto %d reachability: %w", protoID, reachableErr)
		}
		scoped := common
		scoped.directTargets = backendGoNumericScopedTargets(targets, reachable)
		protoOptions := scoped
		if protoID == 0 {
			protoOptions.functionName = options.functionPrefix + "Root"
		} else {
			protoOptions = backendGoNumericTargetOptions(scoped, targets[protoID])
		}
		if protoIndex < len(upvalueTargets) {
			protoOptions.upvalueTargets = upvalueTargets[protoIndex]
		}
		preparedName := options.functionPrefix + "PreparedProto" + strconv.Itoa(protoIndex)
		protoOptions.preparedFunctionName = preparedName
		source, preparedErr := emitBackendGoNumericProof(irs[protoIndex], protoOptions)
		if preparedErr != nil {
			protoOptions.preparedFunctionName = ""
			protoOptions.preparedImportPath = ""
			protoOptions.preparedQualifier = ""
			source, err = emitBackendGoNumericProof(irs[protoIndex], protoOptions)
			if err != nil {
				// A nil bundle entry is an exact, explicit request for the
				// canonical Machine implementation. Unsupported Protos must not
				// prevent supported siblings from being prepared.
				continue
			}
		} else {
			result.functions[protoIndex] = preparedName
		}
		result.files = append(result.files, backendGoNumericProgramFile{
			name:    fmt.Sprintf("prepared_proto_%d.go", protoID),
			protoID: protoID,
			source:  source,
		})
	}
	return result, nil
}

func backendGoNumericScopedTargets(targets []backendGoNumericTarget, protoIDs []int32) []backendGoNumericTarget {
	result := make([]backendGoNumericTarget, len(targets))
	for _, protoID := range protoIDs {
		if protoID >= 0 && int(protoID) < len(targets) {
			result[protoID] = targets[protoID]
		}
	}
	return result
}
