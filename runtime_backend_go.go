package ember

import (
	"fmt"
	"go/format"
	"go/token"
	"strconv"
	"strings"
)

type backendGoNumericOptions struct {
	packageName          string
	functionName         string
	preparedFunctionName string
	directTargets        []backendGoNumericTarget
	selfRecursive        bool
	fixedVarargCount     int
	receiverTable        bool
	receiverTables       int
	coroutineTarget      bool
	coroutineDeadString  machineStringID
}

type backendGoNumericTarget struct {
	ir               *backendProtoIR
	functionName     string
	selfRecursive    bool
	fixedVarargCount int
	receiverTable    bool
	receiverTables   int
}

type backendGoNumericPlan struct {
	tags          []backendTagMask
	used          []bool
	parameterTags []backendTagMask
	// scalarReplacedValues have no canonical Machine value in prepared code.
	scalarReplacedValues []bool
	// replayEntry marks exits whose exact spill would require one of those values.
	replayEntry []bool
	tables      backendGoScalarTablePlan
	keys        backendGoStructuralKeyPlan
	records     backendGoRecordTablePlan
	closures    backendGoScalarClosurePlan
	methods     backendGoScalarMethodPlan
	captured    backendGoCapturedRecordPlan
	callSets    backendGoFiniteCallSetPlan
	closureSets backendGoFiniteClosureSetPlan
	coroutines  backendGoScalarCoroutinePlan
}

type backendGoNumericEmitter struct {
	ir          *backendProtoIR
	plan        backendGoNumericPlan
	body        strings.Builder
	needsMath   bool
	prepared    bool
	resultCount int
	resultTypes []string
	options     backendGoNumericOptions
}

func backendGoNumericReceiverTableCount(legacy bool, count int) int {
	if count > 0 {
		return count
	}
	if legacy {
		return 1
	}
	return 0
}

func backendGoNumericMathMin(operation *backendOperationIR) bool {
	return operation != nil &&
		operation.op == opFastCall &&
		nativeFuncID(operation.nativeID) == nativeFuncMathMin &&
		operation.c == 2 &&
		operation.d == 1 &&
		len(operation.defs) == 1 &&
		operation.defs[0].register == operation.a
}

func emitBackendGoNumericProof(ir *backendProtoIR, options backendGoNumericOptions) ([]byte, error) {
	if err := verifyBackendProtoIR(ir); err != nil {
		return nil, fmt.Errorf("emit backend Go numeric proof: %w", err)
	}
	if !token.IsIdentifier(options.packageName) || token.Lookup(options.packageName).IsKeyword() {
		return nil, fmt.Errorf("emit backend Go numeric proof: invalid package name %q", options.packageName)
	}
	if !token.IsIdentifier(options.functionName) || token.Lookup(options.functionName).IsKeyword() {
		return nil, fmt.Errorf("emit backend Go numeric proof: invalid function name %q", options.functionName)
	}
	if options.preparedFunctionName != "" &&
		(!token.IsIdentifier(options.preparedFunctionName) || token.Lookup(options.preparedFunctionName).IsKeyword()) {
		return nil, fmt.Errorf("emit backend Go numeric proof: invalid prepared function name %q", options.preparedFunctionName)
	}
	if options.preparedFunctionName != "" && len(ir.upvalues) != 0 {
		return nil, fmt.Errorf("emit backend Go numeric proof: prepared entry has %d upvalues", len(ir.upvalues))
	}
	if options.preparedFunctionName != "" && ir.variadic {
		return nil, fmt.Errorf("emit backend Go numeric proof: prepared entry is variadic")
	}
	receiverTables := backendGoNumericReceiverTableCount(options.receiverTable, options.receiverTables)
	if receiverTables > 0 &&
		(receiverTables > ir.params || options.preparedFunctionName != "") {
		return nil, fmt.Errorf("emit backend Go numeric proof: invalid scalar receiver-table target")
	}
	if options.coroutineTarget && options.preparedFunctionName != "" {
		return nil, fmt.Errorf("emit backend Go numeric proof: coroutine target cannot be a prepared entry")
	}
	resultCount, ok := backendGoNumericFixedResultCountFor(ir, options.fixedVarargCount)
	if !ok {
		return nil, fmt.Errorf("emit backend Go numeric proof: function has no bounded fixed result shape")
	}
	if options.preparedFunctionName != "" && resultCount != 1 {
		return nil, fmt.Errorf("emit backend Go numeric proof: prepared entry returns %d values, want 1", resultCount)
	}
	parameterCount, ok := backendGoNumericArgumentCount(ir, options.fixedVarargCount)
	if !ok {
		return nil, fmt.Errorf("emit backend Go numeric proof: invalid fixed vararg count %d", options.fixedVarargCount)
	}
	parameterTags, ok := backendGoNumericParameterTags(ir, options.fixedVarargCount)
	if !ok {
		return nil, fmt.Errorf("emit backend Go numeric proof: function has no bounded scalar parameter types")
	}
	if options.preparedFunctionName != "" {
		for parameter := 0; parameter < ir.params; parameter++ {
			if parameterTags[parameter] != backendTagNumber {
				return nil, fmt.Errorf("emit backend Go numeric proof: prepared parameter %d is not numeric", parameter)
			}
		}
	}
	if options.selfRecursive && (resultCount != 1 || !backendGoNumericSelfRecursiveTarget(ir)) {
		return nil, fmt.Errorf("emit backend Go numeric proof: function is not a bounded self-recursive target")
	}
	if err := verifyBackendGoNumericTargets(options.directTargets); err != nil {
		return nil, err
	}
	plan, err := buildBackendGoNumericPlan(ir, options)
	if err != nil {
		return nil, err
	}
	resultTypes, err := backendGoNumericResultTypes(ir, plan, options.fixedVarargCount)
	if err != nil {
		return nil, err
	}
	if options.preparedFunctionName != "" &&
		(len(resultTypes) != 1 || resultTypes[0] != "float64") {
		return nil, fmt.Errorf("emit backend Go numeric proof: prepared entry result is not numeric")
	}
	directEmitter := backendGoNumericEmitter{
		ir: ir, plan: plan, resultCount: resultCount, resultTypes: resultTypes, options: options,
	}
	if err := directEmitter.emitBody(); err != nil {
		return nil, err
	}
	if options.selfRecursive {
		directEmitter.needsMath = true
	}
	preparedEmitter := backendGoNumericEmitter{
		ir: ir, plan: plan, prepared: true, resultCount: resultCount, resultTypes: resultTypes, options: options,
	}
	if options.preparedFunctionName != "" {
		if err := preparedEmitter.emitBody(); err != nil {
			return nil, err
		}
	}
	var coroutineSource strings.Builder
	coroutineNeedsMath := false
	if plan.coroutines.enabled {
		if plan.coroutines.targetPlan == nil {
			return nil, fmt.Errorf("emit backend Go numeric proof: scalar coroutine target plan is unavailable")
		}
		coroutineNeedsMath, err = emitBackendGoCoroutineTarget(
			&coroutineSource,
			plan.coroutines.target,
			*plan.coroutines.targetPlan,
			plan.coroutines.yields,
		)
		if err != nil {
			return nil, err
		}
	}

	var source strings.Builder
	source.WriteString("// Code generated by Ember's private prepared proof compiler; DO NOT EDIT.\n\n")
	fmt.Fprintf(&source, "package %s\n\n", options.packageName)
	if directEmitter.needsMath || preparedEmitter.needsMath || coroutineNeedsMath {
		source.WriteString("import \"math\"\n\n")
	}
	source.WriteString(coroutineSource.String())
	pointerUpvalues := len(ir.upvalues)
	capturedRecord := plan.tables.externalRoot != invalidBackendValueID && receiverTables == 0
	if options.selfRecursive || capturedRecord {
		pointerUpvalues = 0
	}
	if options.selfRecursive {
		fmt.Fprintf(
			&source,
			"func %s(p0 float64) (float64, bool) {\n\tif math.IsNaN(p0) || p0 > %d {\n\t\treturn 0, false\n\t}\n\treturn %sBody(p0), true\n}\n\n",
			options.functionName,
			backendGoMaxPreparedRecursiveArgument,
			options.functionName,
		)
		fmt.Fprintf(&source, "func %sBody(p0 float64) float64 {\n", options.functionName)
	} else {
		fmt.Fprintf(&source, "func %s(", options.functionName)
		wroteParameter := false
		for upvalue := 0; upvalue < pointerUpvalues; upvalue++ {
			if wroteParameter {
				source.WriteString(", ")
			}
			fmt.Fprintf(&source, "u%d *float64", upvalue)
			wroteParameter = true
		}
		if receiverTables > 0 || capturedRecord {
			for fieldIndex, field := range plan.tables.fields {
				if !plan.tables.isExternalRoot(field.key.table) || field.tags == 0 {
					continue
				}
				goType, ok := backendGoNumericType(field.tags)
				if !ok {
					return nil, fmt.Errorf("emit backend Go numeric proof: receiver field %d has unsupported tags %x", fieldIndex, field.tags)
				}
				if wroteParameter {
					source.WriteString(", ")
				}
				fmt.Fprintf(&source, "r%d *%s", fieldIndex, goType)
				wroteParameter = true
			}
		}
		for parameter := 0; parameter < parameterCount; parameter++ {
			if parameter < receiverTables {
				continue
			}
			if wroteParameter {
				source.WriteString(", ")
			}
			goType, ok := backendGoNumericType(parameterTags[parameter])
			if !ok {
				return nil, fmt.Errorf("emit backend Go numeric proof: parameter %d has unsupported tags %x", parameter, parameterTags[parameter])
			}
			fmt.Fprintf(&source, "p%d %s", parameter, goType)
			wroteParameter = true
		}
		source.WriteString(") (")
		writeBackendGoNumericResultTypes(&source, resultTypes)
		source.WriteString(") {\n")
		for upvalue := 0; upvalue < pointerUpvalues; upvalue++ {
			fmt.Fprintf(&source, "\tif u%d == nil {\n\t\t%s\n\t}\n", upvalue, backendGoNumericFailureReturn(resultTypes))
		}
		if receiverTables > 0 || capturedRecord {
			for fieldIndex, field := range plan.tables.fields {
				if !plan.tables.isExternalRoot(field.key.table) || field.tags == 0 {
					continue
				}
				fmt.Fprintf(&source, "\tif r%d == nil {\n\t\t%s\n\t}\n", fieldIndex, backendGoNumericFailureReturn(resultTypes))
			}
		}
	}
	generatedReachable := directEmitter.generatedReachableBlocks()
	for valueIndex := range ir.values {
		if !plan.used[valueIndex] {
			continue
		}
		value := &ir.values[valueIndex]
		if value.block >= 0 &&
			(int(value.block) >= len(generatedReachable) || !generatedReachable[value.block]) {
			continue
		}
		if plan.tags[valueIndex] == backendTagNil {
			continue
		}
		goType, ok := backendGoNumericTypeForValue(plan, backendValueID(valueIndex+1))
		if !ok {
			return nil, fmt.Errorf("emit backend Go numeric proof: SSA value %d has unsupported tags %x", valueIndex+1, plan.tags[valueIndex])
		}
		fmt.Fprintf(&source, "\tvar v%d %s\n", valueIndex+1, goType)
		if _, optional := backendGoOptionalScalarTags(plan.tags[valueIndex]); optional {
			fmt.Fprintf(&source, "\tvar vp%d bool\n", valueIndex+1)
			fmt.Fprintf(&source, "\t_ = v%d\n", valueIndex+1)
			fmt.Fprintf(&source, "\t_ = vp%d\n", valueIndex+1)
		}
	}
	for fieldIndex, field := range plan.tables.fields {
		if plan.tables.isExternalRoot(field.key.table) {
			continue
		}
		goType, ok := backendGoNumericType(field.tags)
		if !ok {
			continue
		}
		fmt.Fprintf(&source, "\tvar f%d %s\n", fieldIndex, goType)
		fmt.Fprintf(&source, "\t_ = f%d\n", fieldIndex)
	}
	for arrayIndex, array := range plan.tables.arrays {
		goType, ok := backendGoNumericType(array.tags)
		if !ok {
			return nil, fmt.Errorf("emit backend Go numeric proof: scalar array %d has unsupported tags %x", arrayIndex, array.tags)
		}
		fmt.Fprintf(&source, "\tvar a%d [%d]%s\n", arrayIndex, array.capacity, goType)
		fmt.Fprintf(&source, "\t_ = a%d\n", arrayIndex)
		if array.mutable {
			fmt.Fprintf(&source, "\tvar h%d int\n", arrayIndex)
			fmt.Fprintf(&source, "\t_ = h%d\n", arrayIndex)
			fmt.Fprintf(&source, "\tvar n%d = %d\n", arrayIndex, array.length)
			fmt.Fprintf(&source, "\t_ = n%d\n", arrayIndex)
			fmt.Fprintf(&source, "\tvar t%d int\n", arrayIndex)
			fmt.Fprintf(&source, "\t_ = t%d\n", arrayIndex)
		}
		fmt.Fprintf(&source, "\tvar i%d int\n", arrayIndex)
		fmt.Fprintf(&source, "\t_ = i%d\n", arrayIndex)
	}
	writeBackendGoRecordDeclarations(&source, plan.records)
	for cell := 0; cell < plan.closures.cellCount; cell++ {
		fmt.Fprintf(&source, "\tvar c%d float64\n", cell)
		fmt.Fprintf(&source, "\t_ = c%d\n", cell)
		fmt.Fprintf(&source, "\tvar s%d float64\n", cell)
		fmt.Fprintf(&source, "\t_ = s%d\n", cell)
	}
	writeBackendGoFiniteClosureDeclarations(&source, ir, plan)
	if plan.coroutines.enabled {
		fmt.Fprintf(&source, "\tvar q0 %sState\n", plan.coroutines.target.functionName)
		fmt.Fprintln(&source, "\t_ = q0")
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if _, _, factory := plan.closures.factory(operation); factory {
			continue
		}
		needsOK := !backendGoNumericOperationDead(plan, operation) &&
			(operation.op == opCallOne || operation.op == opCallLocalOne ||
				operation.op == opCall && backendGoNumericScalarReplacedCall(ir, options, operation) ||
				operation.op == opCallMethodOne)
		if _, ok := plan.coroutines.resumes[operation.pc]; ok {
			needsOK = true
		}
		if needsOK {
			fmt.Fprintf(&source, "\tvar ok%d bool\n", operation.pc)
		}
		if call, ok := plan.methods.call(operation); ok {
			for field := range call.callerFields {
				fmt.Fprintf(&source, "\tvar m%d_%d float64\n", operation.pc, field)
			}
		}
		if call, ok := plan.captured.call(operation); ok {
			for field := range call.callerFields {
				fmt.Fprintf(&source, "\tvar m%d_%d float64\n", operation.pc, field)
			}
		}
	}
	for parameter := 0; parameter < ir.params; parameter++ {
		id := ir.initial[parameter]
		if plan.used[id-1] {
			fmt.Fprintf(&source, "\tv%d = p%d\n", id, parameter)
		}
	}
	source.WriteString(directEmitter.body.String())
	source.WriteString("}\n")
	if options.preparedFunctionName != "" {
		bodyName := options.preparedFunctionName + "Body"
		fmt.Fprintf(&source, "\nfunc %s(context machinePreparedContext", bodyName)
		for parameter := 0; parameter < ir.params; parameter++ {
			fmt.Fprintf(&source, ", p%d float64", parameter)
		}
		source.WriteString(") machinePreparedExit {\n")
		for valueIndex := range ir.values {
			if !plan.used[valueIndex] || plan.tags[valueIndex] == backendTagNil {
				continue
			}
			value := &ir.values[valueIndex]
			if value.block >= 0 &&
				(int(value.block) >= len(generatedReachable) || !generatedReachable[value.block]) {
				continue
			}
			goType, ok := backendGoNumericTypeForValue(plan, backendValueID(valueIndex+1))
			if !ok {
				return nil, fmt.Errorf("emit backend Go numeric proof: SSA value %d has unsupported tags %x", valueIndex+1, plan.tags[valueIndex])
			}
			fmt.Fprintf(&source, "\tvar v%d %s\n", valueIndex+1, goType)
			if _, optional := backendGoOptionalScalarTags(plan.tags[valueIndex]); optional {
				fmt.Fprintf(&source, "\tvar vp%d bool\n", valueIndex+1)
				fmt.Fprintf(&source, "\t_ = v%d\n", valueIndex+1)
				fmt.Fprintf(&source, "\t_ = vp%d\n", valueIndex+1)
			}
		}
		for fieldIndex, field := range plan.tables.fields {
			if plan.tables.isExternalRoot(field.key.table) {
				continue
			}
			goType, ok := backendGoNumericType(field.tags)
			if !ok {
				continue
			}
			fmt.Fprintf(&source, "\tvar f%d %s\n", fieldIndex, goType)
			fmt.Fprintf(&source, "\t_ = f%d\n", fieldIndex)
		}
		for arrayIndex, array := range plan.tables.arrays {
			goType, ok := backendGoNumericType(array.tags)
			if !ok {
				return nil, fmt.Errorf("emit backend Go numeric proof: scalar array %d has unsupported tags %x", arrayIndex, array.tags)
			}
			fmt.Fprintf(&source, "\tvar a%d [%d]%s\n", arrayIndex, array.capacity, goType)
			fmt.Fprintf(&source, "\t_ = a%d\n", arrayIndex)
			if array.mutable {
				fmt.Fprintf(&source, "\tvar h%d int\n", arrayIndex)
				fmt.Fprintf(&source, "\t_ = h%d\n", arrayIndex)
				fmt.Fprintf(&source, "\tvar n%d = %d\n", arrayIndex, array.length)
				fmt.Fprintf(&source, "\t_ = n%d\n", arrayIndex)
				fmt.Fprintf(&source, "\tvar t%d int\n", arrayIndex)
				fmt.Fprintf(&source, "\t_ = t%d\n", arrayIndex)
			}
			fmt.Fprintf(&source, "\tvar i%d int\n", arrayIndex)
			fmt.Fprintf(&source, "\t_ = i%d\n", arrayIndex)
		}
		writeBackendGoRecordDeclarations(&source, plan.records)
		for cell := 0; cell < plan.closures.cellCount; cell++ {
			fmt.Fprintf(&source, "\tvar c%d float64\n", cell)
			fmt.Fprintf(&source, "\t_ = c%d\n", cell)
			fmt.Fprintf(&source, "\tvar s%d float64\n", cell)
			fmt.Fprintf(&source, "\t_ = s%d\n", cell)
		}
		writeBackendGoFiniteClosureDeclarations(&source, ir, plan)
		if plan.coroutines.enabled {
			fmt.Fprintf(&source, "\tvar q0 %sState\n", plan.coroutines.target.functionName)
			fmt.Fprintln(&source, "\t_ = q0")
		}
		for pc := range ir.ops {
			operation := &ir.ops[pc]
			if _, _, factory := plan.closures.factory(operation); factory {
				continue
			}
			needsOK := !backendGoNumericOperationDead(plan, operation) &&
				(operation.op == opCallOne || operation.op == opCallLocalOne ||
					operation.op == opCall && backendGoNumericScalarReplacedCall(ir, options, operation) ||
					operation.op == opCallMethodOne)
			if _, ok := plan.coroutines.resumes[operation.pc]; ok {
				needsOK = true
			}
			if needsOK {
				fmt.Fprintf(&source, "\tvar ok%d bool\n", operation.pc)
			}
			if call, ok := plan.methods.call(operation); ok {
				for field := range call.callerFields {
					fmt.Fprintf(&source, "\tvar m%d_%d float64\n", operation.pc, field)
				}
			}
			if call, ok := plan.captured.call(operation); ok {
				for field := range call.callerFields {
					fmt.Fprintf(&source, "\tvar m%d_%d float64\n", operation.pc, field)
				}
			}
		}
		for parameter := 0; parameter < ir.params; parameter++ {
			id := ir.initial[parameter]
			if plan.used[id-1] {
				fmt.Fprintf(&source, "\tv%d = p%d\n", id, parameter)
			}
		}
		source.WriteString(preparedEmitter.body.String())
		source.WriteString("}\n")

		fmt.Fprintf(&source, "\nfunc %s(context machinePreparedContext) machinePreparedExit {\n", options.preparedFunctionName)
		for parameter := 0; parameter < ir.params; parameter++ {
			fmt.Fprintf(&source, "\tp%d, ok := context.numberParameter(%d)\n", parameter, parameter)
			source.WriteString("\tif !ok {\n\t\treturn machinePreparedReplayEntry()\n\t}\n")
		}
		for pc := range ir.ops {
			operation := &ir.ops[pc]
			if operation.op != opFastCall {
				continue
			}
			_, recordFamilyRawLen := plan.records.familyRawLenPC[operation.pc]
			_, recordFamilyRemove := plan.records.familyRemovePC[operation.pc]
			_, recordArrayInsert := plan.records.arrayInsertPC[operation.pc]
			_, recordArrayRemove := plan.records.arrayRemovePC[operation.pc]
			_, recordArrayRawLen := plan.records.arrayRawLenPC[operation.pc]
			if _, structuralToString := plan.keys.tostring(operation); !structuralToString &&
				!backendGoNumericMathMin(operation) &&
				!recordFamilyRawLen && !recordFamilyRemove &&
				!recordArrayInsert && !recordArrayRemove && !recordArrayRawLen {
				if !plan.coroutines.createOperation(operation) &&
					!plan.coroutines.statusOperation(operation) {
					if _, resume := plan.coroutines.resume(operation); !resume {
						if _, _, _, ok := plan.tables.arrayOperation(ir, operation); !ok {
							if _, ok := plan.tables.metatableOperation(operation); !ok {
								continue
							}
						}
					}
				}
			}
			if operation.nativeID <= int32(nativeFuncUnknown) {
				continue
			}
			fmt.Fprintf(&source, "\tif !context.intrinsicUnchanged(%d) {\n", operation.pc)
			source.WriteString("\t\treturn machinePreparedReplayEntry()\n\t}\n")
		}
		if plan.coroutines.enabled {
			for pc := range plan.coroutines.target.ir.ops {
				operation := &plan.coroutines.target.ir.ops[pc]
				if _, ok := plan.coroutines.yields[operation.pc]; !ok {
					continue
				}
				fmt.Fprintf(
					&source,
					"\tif !context.intrinsicUnchangedAt(%d, %d) {\n",
					plan.coroutines.targetProto,
					operation.pc,
				)
				source.WriteString("\t\treturn machinePreparedReplayEntry()\n\t}\n")
			}
		}
		for protoID, target := range options.directTargets {
			if target.ir == nil {
				continue
			}
			targetKeys := analyzeBackendGoStructuralKeys(target.ir, backendGoNumericOptions{})
			for pc := range target.ir.ops {
				operation := &target.ir.ops[pc]
				_, structuralToString := targetKeys.tostring(operation)
				if !structuralToString && !backendGoNumericFixedVarargSelect(
					target.ir,
					target.fixedVarargCount,
					operation,
				) && !backendGoNumericMathMin(operation) {
					continue
				}
				fmt.Fprintf(
					&source,
					"\tif !context.intrinsicUnchangedAt(%d, %d) {\n",
					protoID,
					operation.pc,
				)
				source.WriteString("\t\treturn machinePreparedReplayEntry()\n\t}\n")
			}
		}
		source.WriteString("\treturn ")
		source.WriteString(bodyName)
		source.WriteString("(context")
		for parameter := 0; parameter < ir.params; parameter++ {
			fmt.Fprintf(&source, ", p%d", parameter)
		}
		source.WriteString(")\n}\n")
	}
	formatted, err := format.Source([]byte(source.String()))
	if err != nil {
		return nil, fmt.Errorf("emit backend Go numeric proof: format generated source: %w", err)
	}
	return formatted, nil
}

func writeBackendGoNumericResultTypes(source *strings.Builder, resultTypes []string) {
	for result, resultType := range resultTypes {
		if result != 0 {
			source.WriteString(", ")
		}
		source.WriteString(resultType)
	}
	if len(resultTypes) != 0 {
		source.WriteString(", ")
	}
	source.WriteString("bool")
}

func backendGoNumericFailureReturn(resultTypes []string) string {
	var result strings.Builder
	result.WriteString("return ")
	for index, resultType := range resultTypes {
		if index != 0 {
			result.WriteString(", ")
		}
		switch resultType {
		case "backendPreparedStringKey":
			result.WriteString("backendPreparedStringKey{}")
		case "bool":
			result.WriteString("false")
		case "float64", "uint32":
			result.WriteString("0")
		default:
			panic("unsupported backend Go result type " + resultType)
		}
	}
	if len(resultTypes) != 0 {
		result.WriteString(", ")
	}
	result.WriteString("false")
	return result.String()
}

func verifyBackendGoNumericTargets(targets []backendGoNumericTarget) error {
	for protoID, target := range targets {
		if target.ir == nil && target.functionName == "" {
			continue
		}
		if target.ir == nil ||
			!token.IsIdentifier(target.functionName) ||
			token.Lookup(target.functionName).IsKeyword() {
			return fmt.Errorf("emit backend Go numeric proof: invalid direct target Proto %d", protoID)
		}
		if err := verifyBackendProtoIR(target.ir); err != nil {
			return fmt.Errorf("emit backend Go numeric proof: direct target Proto %d: %w", protoID, err)
		}
		if _, ok := backendGoNumericArgumentCount(target.ir, target.fixedVarargCount); !ok {
			return fmt.Errorf("emit backend Go numeric proof: direct target Proto %d has invalid fixed vararg count", protoID)
		}
		if _, ok := backendGoNumericParameterTags(target.ir, target.fixedVarargCount); !ok {
			return fmt.Errorf("emit backend Go numeric proof: direct target Proto %d has no bounded scalar parameter types", protoID)
		}
		resultCount, ok := backendGoNumericFixedResultCountFor(target.ir, target.fixedVarargCount)
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: direct target Proto %d has no bounded fixed result shape", protoID)
		}
		targetOptions := backendGoNumericOptions{
			functionName:     target.functionName,
			directTargets:    targets,
			selfRecursive:    target.selfRecursive,
			fixedVarargCount: target.fixedVarargCount,
			receiverTable:    target.receiverTable,
			receiverTables:   target.receiverTables,
			coroutineTarget:  backendGoNumericHasCoroutineYield(target.ir),
		}
		if target.selfRecursive && (resultCount != 1 || !backendGoNumericSelfRecursiveTarget(target.ir)) {
			return fmt.Errorf("emit backend Go numeric proof: direct target Proto %d is not bounded self recursion", protoID)
		}
		if _, err := buildBackendGoNumericPlan(target.ir, targetOptions); err != nil {
			if _, ok := backendGoNumericClosureFactory(target.ir, targets); !ok {
				return fmt.Errorf("emit backend Go numeric proof: direct target Proto %d is not a numeric leaf or closure factory: %w", protoID, err)
			}
		}
	}
	return nil
}

func buildBackendGoNumericPlan(ir *backendProtoIR, options backendGoNumericOptions) (backendGoNumericPlan, error) {
	keys := analyzeBackendGoStructuralKeys(ir, options)
	records := analyzeBackendGoRecordTables(ir, keys)
	callSets := discoverBackendGoFiniteCallSets(ir, records, options)
	closures, err := analyzeBackendGoScalarClosures(ir, options)
	if err != nil {
		return backendGoNumericPlan{}, err
	}
	closureSets := discoverBackendGoFiniteClosureSets(ir, records, closures, options)
	if len(callSets.calls) != 0 || len(closureSets.sets) != 0 {
		allowedCalls := make(map[int32]bool, len(callSets.calls)+len(closureSets.sets))
		excludedRoots := make(map[backendValueID]bool, len(callSets.excludedRoots)+len(closureSets.excludedRoots))
		for pc := range callSets.calls {
			allowedCalls[pc] = true
		}
		for pc := range closureSets.sets {
			allowedCalls[pc] = true
		}
		for root := range callSets.excludedRoots {
			excludedRoots[root] = true
		}
		for root := range closureSets.excludedRoots {
			excludedRoots[root] = true
		}
		records = analyzeBackendGoRecordTablesWithOptions(ir, keys, backendGoRecordAnalysisOptions{
			excludedRoots: excludedRoots,
			allowedCalls:  allowedCalls,
		})
		if records.enabled {
			var ok bool
			callSets, ok = finalizeBackendGoFiniteCallSets(callSets, records)
			if !ok {
				records = backendGoRecordTablePlan{}
				callSets = backendGoFiniteCallSetPlan{}
			}
			if records.enabled {
				closureSets, ok = finalizeBackendGoFiniteClosureSets(ir, closureSets, records)
				if !ok {
					records = backendGoRecordTablePlan{}
					callSets = backendGoFiniteCallSetPlan{}
					closureSets = backendGoFiniteClosureSetPlan{}
				}
			}
		}
	}
	excludedTables := make(map[backendValueID]bool)
	if records.enabled {
		for root := range records.recordByRoot {
			excludedTables[root] = true
		}
		for root := range records.mapByRoot {
			excludedTables[root] = true
		}
		for root := range records.arrayByRoot {
			excludedTables[root] = true
		}
	} else {
		records = backendGoRecordTablePlan{}
		callSets = backendGoFiniteCallSetPlan{}
		closureSets = backendGoFiniteClosureSetPlan{}
	}
	for root := range callSets.excludedRoots {
		excludedTables[root] = true
	}
	for root := range closureSets.excludedRoots {
		excludedTables[root] = true
	}
	receiverTables := backendGoNumericReceiverTableCount(options.receiverTable, options.receiverTables)
	tables, err := analyzeBackendGoScalarTablesExcludingCount(ir, receiverTables, excludedTables)
	if err != nil {
		return backendGoNumericPlan{}, err
	}
	captured, err := analyzeBackendGoCapturedRecordCalls(ir, records, options)
	if err != nil {
		return backendGoNumericPlan{}, err
	}
	methods, err := analyzeBackendGoScalarMethods(ir, tables, options)
	if err != nil {
		return backendGoNumericPlan{}, err
	}
	coroutines, err := analyzeBackendGoScalarCoroutine(ir, options)
	if err != nil {
		return backendGoNumericPlan{}, err
	}
	plan := backendGoNumericPlan{
		tags:                 make([]backendTagMask, len(ir.values)),
		used:                 make([]bool, len(ir.values)),
		scalarReplacedValues: make([]bool, len(ir.values)),
		replayEntry:          make([]bool, len(ir.ops)),
		tables:               tables,
		keys:                 keys,
		records:              records,
		closures:             closures,
		methods:              methods,
		captured:             captured,
		callSets:             callSets,
		closureSets:          closureSets,
		coroutines:           coroutines,
	}
	parameterTags, ok := backendGoNumericParameterTags(ir, options.fixedVarargCount)
	if !ok {
		return backendGoNumericPlan{}, fmt.Errorf("emit backend Go numeric proof: function has no bounded scalar parameter types")
	}
	plan.parameterTags = parameterTags
	for register, id := range ir.initial {
		if register < receiverTables {
			plan.tags[id-1] = backendTagTable
		} else if register < ir.params {
			plan.tags[id-1] = parameterTags[register]
		} else {
			plan.tags[id-1] = backendTagNil
		}
	}
	for {
		changed := false
		for valueIndex := range ir.values {
			value := &ir.values[valueIndex]
			var tags backendTagMask
			switch value.kind {
			case backendValueUndef, backendValueParameter:
				continue
			case backendValuePhi:
				block := &ir.blocks[value.block]
				phi := block.phis[value.register]
				for inputIndex, input := range phi.inputs {
					if !ir.blocks[block.predecessors[inputIndex]].reachable {
						continue
					}
					tags |= plan.tags[input-1]
				}
			case backendValueOperation:
				operation := &ir.ops[value.pc]
				if !ir.blocks[operation.block].reachable {
					continue
				}
				switch operation.op {
				case opLoadConst:
					if plan.coroutines.dead(value.id) {
						tags = backendTagBool
					} else if operation.b >= 0 && int(operation.b) < len(ir.constants) {
						tags = backendTagForValueKind(ir.constants[operation.b].kind)
					}
				case opMove:
					source := backendOperationUse(operation, operation.b)
					if source != invalidBackendValueID {
						tags = plan.tags[source-1]
					}
				case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opNeg,
					opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
					opNumericForLoop:
					tags = backendTagNumber
				case opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
					tags = backendTagBool
				case opClosure:
					tags = backendTagFunction
				case opNewTable:
					if plan.tables.root(value.id) != invalidBackendValueID ||
						valueIndex < len(plan.callSets.scalarValues) && plan.callSets.scalarValues[valueIndex] ||
						valueIndex < len(plan.closureSets.scalarValues) && plan.closureSets.scalarValues[valueIndex] {
						tags = backendTagTable
					}
				case opGetStringField:
					if field, ok := plan.records.fieldsByPC[operation.pc]; ok {
						tags = plan.records.fieldTagsFor(plan.tags, field)
					} else if _, field, ok := plan.tables.operationField(ir, operation); ok {
						if field.child != invalidBackendValueID {
							tags = backendTagTable
						} else if field.methodProto >= 0 {
							tags = backendTagFunction
						} else {
							tags = field.tags
						}
					}
				case opGetStringFieldIndex:
					if _, ok := plan.records.familyGetByPC[operation.pc]; ok {
						tags = backendTagNumber
					} else if fused, ok := plan.records.fusedGetByPC[operation.pc]; ok {
						tags = plan.records.childRecordFieldTags(plan.tags, fused.family)
					}
				case opGetIndex:
					if _, ok := plan.closureSets.selector(operation); ok {
						tags = backendTagNumber
					} else if plan.callSets.selector(operation) {
						tags = backendTagNumber
					} else if field, ok := plan.records.fieldsByPC[operation.pc]; ok {
						tags = plan.records.fieldTagsFor(plan.tags, field)
					} else if _, ok := plan.records.dynamicChildSelectByPC[operation.pc]; ok {
						tags = backendTagNumber
					} else if dynamic, ok := plan.records.dynamicChildGetByPC[operation.pc]; ok {
						tags = plan.records.childRecordFieldTags(plan.tags, dynamic.family)
					} else if dynamic, ok := plan.records.dynamicGetByPC[operation.pc]; ok {
						tags = plan.records.dynamicFieldTags(plan.tags, dynamic)
					} else if _, ok := plan.records.mapGetByPC[operation.pc]; ok {
						tags = backendTagNumber
					} else if _, ok := plan.records.arrayGetByPC[operation.pc]; ok {
						tags = backendTagNumber
					} else if _, array, _, ok := plan.tables.arrayOperation(ir, operation); ok {
						tags = array.tags
					}
				case opPrepareIter:
					if _, ok := plan.closureSets.prepare(operation); ok {
						tags = backendTagNumber
					} else if _, ok := plan.records.arrayPreparePC[operation.pc]; ok {
						tags = 0
					} else if _, ok := plan.records.familyPrepare[operation.pc]; ok {
						tags = 0
					} else if value.register == operation.a {
						if _, _, _, ok := plan.tables.arrayOperation(ir, operation); ok {
							tags = backendTagTable
						}
					}
				case opArrayNextJump2:
					if _, ok := plan.closureSets.next(operation); ok {
						tags = backendTagNumber
					} else if _, ok := plan.records.arrayNextPC[operation.pc]; ok {
						if value.register == operation.a+1 {
							tags = backendTagNumber
						}
					} else if _, ok := plan.records.familyNext[operation.pc]; ok {
						if value.register == operation.a+1 {
							tags = backendTagNumber
						}
					} else if _, array, _, ok := plan.tables.arrayOperation(ir, operation); ok {
						switch value.register {
						case operation.a:
							tags = backendTagNumber
						case operation.a + 1:
							tags = array.tags
						}
					}
				case opFastCall:
					if _, ok := plan.records.arrayRawLenPC[operation.pc]; ok {
						tags = backendTagNumber
					} else if _, ok := plan.records.arrayInsertPC[operation.pc]; ok {
						tags = backendTagNil
					} else if _, ok := plan.records.arrayRemovePC[operation.pc]; ok {
						tags = backendTagNil
					} else if _, ok := plan.records.familyRawLenPC[operation.pc]; ok {
						tags = backendTagNumber
					} else if _, ok := plan.records.familyRemovePC[operation.pc]; ok {
						tags = backendTagNil
					} else if _, ok := plan.keys.tostring(operation); ok {
						tags = backendTagString
					} else if backendGoNumericMathMin(operation) {
						tags = backendTagNumber
					} else if plan.coroutines.createOperation(operation) {
						tags = 0
					} else if _, ok := plan.coroutines.resume(operation); ok {
						if value.register == operation.a {
							tags = backendTagBool
						} else if value.register == operation.a+1 {
							tags = backendTagNumber
						}
					} else if plan.coroutines.statusOperation(operation) {
						tags = backendTagBool
					} else if options.coroutineTarget &&
						nativeFuncID(operation.nativeID) == nativeFuncCoroutineYield {
						tags = backendTagNil
					} else if backendGoNumericFixedVarargSelect(ir, options.fixedVarargCount, operation) {
						tags = backendTagNumber
					} else if metatable, ok := plan.tables.metatableOperation(operation); ok {
						if plan.tables.root(value.id) == metatable.table {
							tags = backendTagTable
						}
					} else if _, array, _, ok := plan.tables.arrayOperation(ir, operation); ok {
						switch nativeFuncID(operation.nativeID) {
						case nativeFuncTableInsert:
							tags = backendTagNil
						case nativeFuncTableRemove:
							tags = array.tags
						case nativeFuncRawLen:
							tags = backendTagNumber
						}
					}
				case opConcatChain:
					if _, ok := plan.keys.concat(operation); ok {
						tags = backendTagString
					}
				case opVararg:
					if _, ok := backendGoNumericVarargIndex(
						ir,
						options.fixedVarargCount,
						operation,
						value.register,
					); ok {
						tags = backendTagNumber
					}
				case opCall:
					if backendGoNumericScalarReplacedCall(ir, options, operation) {
						if value.register >= operation.a &&
							value.register < operation.a+operation.callResults {
							tags = backendGoDirectTargetResultTag(
								options,
								operation,
								int(value.register-operation.a),
							)
						} else if source, ok := backendGoBorrowedCallSource(ir, operation, value.register); ok {
							tags = plan.tags[source-1]
						}
					}
				case opCallOne, opCallLocalOne:
					if _, ok := plan.closureSets.set(operation); ok {
						tags = backendTagNumber
					} else if _, ok := plan.callSets.call(operation); ok {
						tags = backendTagNumber
					} else if _, _, ok := plan.closures.factory(operation); ok {
						tags = backendTagFunction
					} else if _, ok := plan.closures.call(operation); ok {
						tags = backendTagNumber
					} else if _, ok := backendGoNumericDirectTarget(options, operation); ok {
						tags = backendGoDirectTargetResultTag(options, operation, 0)
					}
				case opCallUpvalueOne:
					if options.selfRecursive && operation.b == 0 {
						tags = backendTagNumber
					}
				case opCallMethodOne:
					if _, ok := plan.methods.call(operation); ok {
						if value.register == operation.a {
							tags = backendTagNumber
						} else if plan.tables.root(value.id) != invalidBackendValueID {
							tags = backendTagTable
						}
					}
				case opGetUpvalue:
					if plan.tables.root(value.id) != invalidBackendValueID {
						tags = backendTagTable
					} else {
						tags = backendTagNumber
					}
				default:
					return backendGoNumericPlan{}, fmt.Errorf("emit backend Go numeric proof: PC %d writes through unsupported opcode %s", value.pc, opcodeName(operation.op))
				}
			}
			if _, ok := plan.records.arrayKeyValues[value.id]; ok {
				tags |= backendTagNumber
			}
			next := plan.tags[valueIndex] | tags
			if next != plan.tags[valueIndex] {
				plan.tags[valueIndex] = next
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	if plan.records.enabled && !plan.records.finalizeFieldTags(ir, plan.tags) {
		return backendGoNumericPlan{}, fmt.Errorf(
			"emit backend Go numeric proof: %s",
			plan.records.rejectReason,
		)
	}
	var markScalarReplaced func(backendValueID)
	markScalarReplaced = func(id backendValueID) {
		if !ir.validBackendValue(id) || plan.scalarReplacedValues[id-1] {
			return
		}
		plan.scalarReplacedValues[id-1] = true
		for _, origin := range ir.values[id-1].origins {
			markScalarReplaced(origin)
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if backendGoNumericScalarReplacedCall(ir, options, operation) {
			markScalarReplaced(backendOperationUse(operation, operation.b))
			plan.replayEntry[pc] = true
		}
		if _, _, ok := plan.closures.factory(operation); ok {
			markScalarReplaced(backendOperationUse(operation, operation.b))
		}
		if _, ok := plan.closures.call(operation); ok {
			markScalarReplaced(backendOperationUse(operation, operation.b))
			plan.replayEntry[pc] = true
		}
		if _, ok := plan.methods.call(operation); ok {
			plan.replayEntry[pc] = true
		}
		if _, ok := plan.captured.call(operation); ok {
			plan.replayEntry[pc] = true
		}
		if _, ok := plan.callSets.call(operation); ok {
			markScalarReplaced(backendOperationUse(operation, operation.b))
			plan.replayEntry[pc] = true
		}
		if _, ok := plan.closureSets.set(operation); ok {
			plan.replayEntry[pc] = true
		}
		if _, ok := plan.coroutines.resume(operation); ok {
			plan.replayEntry[pc] = true
		}
	}
	for valueIndex, scalar := range plan.closures.scalarClosures {
		if scalar {
			markScalarReplaced(backendValueID(valueIndex + 1))
		}
	}
	for valueIndex, scalar := range plan.methods.methodClosures {
		if scalar {
			markScalarReplaced(backendValueID(valueIndex + 1))
		}
	}
	for valueIndex, scalar := range plan.captured.closureValues {
		if scalar {
			markScalarReplaced(backendValueID(valueIndex + 1))
		}
	}
	for valueIndex, scalar := range plan.callSets.scalarValues {
		if scalar {
			markScalarReplaced(backendValueID(valueIndex + 1))
		}
	}
	for valueIndex, scalar := range plan.closureSets.scalarValues {
		if scalar {
			markScalarReplaced(backendValueID(valueIndex + 1))
		}
	}
	for valueIndex, scalar := range plan.coroutines.closureValue {
		if scalar {
			markScalarReplaced(backendValueID(valueIndex + 1))
		}
	}
	for valueIndex, scalar := range plan.coroutines.coroutineValue {
		if scalar {
			markScalarReplaced(backendValueID(valueIndex + 1))
		}
	}
	for valueIndex, scalar := range plan.keys.scalarReplaced {
		if scalar {
			markScalarReplaced(backendValueID(valueIndex + 1))
		}
	}
	for valueIndex, scalar := range plan.records.scalarValues {
		if scalar {
			markScalarReplaced(backendValueID(valueIndex + 1))
		}
	}
	for valueIndex, root := range plan.tables.roots {
		if root != invalidBackendValueID {
			markScalarReplaced(backendValueID(valueIndex + 1))
		}
	}
	for valueIndex, token := range plan.tables.iteratorValues {
		if token {
			markScalarReplaced(backendValueID(valueIndex + 1))
		}
	}
	originUsers := make([][]backendValueID, len(ir.values))
	remainingOrigins := make([]int, len(ir.values))
	hasExternalOrigins := make([]bool, len(ir.values))
	queue := make([]backendValueID, 0, len(ir.values))
	for valueIndex, scalar := range plan.scalarReplacedValues {
		if scalar {
			queue = append(queue, backendValueID(valueIndex+1))
		}
	}
	for valueIndex := range ir.values {
		id := backendValueID(valueIndex + 1)
		for _, origin := range ir.values[valueIndex].origins {
			if !ir.validBackendValue(origin) || origin == id {
				continue
			}
			hasExternalOrigins[valueIndex] = true
			originUsers[origin-1] = append(originUsers[origin-1], id)
			if !plan.scalarReplacedValues[origin-1] {
				remainingOrigins[valueIndex]++
			}
		}
	}
	for valueIndex := range ir.values {
		if plan.scalarReplacedValues[valueIndex] ||
			!hasExternalOrigins[valueIndex] ||
			remainingOrigins[valueIndex] != 0 {
			continue
		}
		id := backendValueID(valueIndex + 1)
		markScalarReplaced(id)
		queue = append(queue, id)
	}
	for len(queue) != 0 {
		id := queue[0]
		queue = queue[1:]
		for _, user := range originUsers[id-1] {
			if plan.scalarReplacedValues[user-1] || remainingOrigins[user-1] == 0 {
				continue
			}
			remainingOrigins[user-1]--
			if remainingOrigins[user-1] != 0 {
				continue
			}
			markScalarReplaced(user)
			queue = append(queue, user)
		}
	}
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		for _, spill := range operation.spillValues {
			if plan.scalarReplacedValues[spill.value-1] ||
				plan.tags[spill.value-1]&backendTagString != 0 {
				// The numeric lowerer is effect-free, so replaying the whole
				// function is exact when a scalar-replaced object or an
				// owner-neutral image string ID is live.
				plan.replayEntry[pc] = true
				break
			}
		}
	}
	work := make([]backendValueID, 0, len(ir.values))
	markUsed := func(id backendValueID) {
		if id == invalidBackendValueID || plan.scalarReplacedValues[id-1] || plan.used[id-1] {
			return
		}
		plan.used[id-1] = true
		work = append(work, id)
	}
	for blockIndex := range ir.blocks {
		block := &ir.blocks[blockIndex]
		if !block.reachable {
			continue
		}
		for pc := block.first; pc < block.last; pc++ {
			operation := &ir.ops[pc]
			if !plan.replayEntry[pc] {
				for _, spill := range operation.spillValues {
					markUsed(spill.value)
				}
			}
			if _, ok := plan.keys.tostring(operation); ok {
				continue
			}
			if key, ok := plan.keys.concat(operation); ok {
				markUsed(key.first)
				markUsed(key.second)
				continue
			}
			if backendGoNumericPureProducer(operation.op) {
				continue
			}
			for _, use := range operation.uses {
				if (operation.op == opCall || operation.op == opCallOne || operation.op == opCallLocalOne) &&
					use.register == operation.b &&
					(backendGoNumericScalarReplacedCall(ir, options, operation) ||
						plan.closures.scalarValue(use.value) ||
						plan.callSets.hasCall(operation) ||
						plan.closureSets.hasSet(operation)) {
					continue
				}
				if operation.op == opCallMethodOne &&
					use.register == operation.b &&
					plan.tables.root(use.value) != invalidBackendValueID {
					continue
				}
				markUsed(use.value)
			}
			for _, definition := range operation.defs {
				if operation.op == opFastCall &&
					(options.coroutineTarget &&
						nativeFuncID(operation.nativeID) == nativeFuncCoroutineYield ||
						plan.coroutines.createOperation(operation) ||
						plan.coroutines.statusOperation(operation)) {
					continue
				}
				if _, ok := plan.coroutines.resume(operation); ok {
					continue
				}
				markUsed(definition.value)
			}
		}
	}
	for len(work) != 0 {
		id := work[len(work)-1]
		work = work[:len(work)-1]
		value := &ir.values[id-1]
		switch value.kind {
		case backendValuePhi:
			block := &ir.blocks[value.block]
			phi := block.phis[value.register]
			for inputIndex, input := range phi.inputs {
				if ir.blocks[block.predecessors[inputIndex]].reachable {
					markUsed(input)
				}
			}
		case backendValueOperation:
			operation := &ir.ops[value.pc]
			if key, ok := plan.keys.concat(operation); ok {
				markUsed(key.first)
				markUsed(key.second)
				continue
			}
			for _, use := range operation.uses {
				if (operation.op == opCall || operation.op == opCallOne || operation.op == opCallLocalOne) &&
					use.register == operation.b &&
					(backendGoNumericScalarReplacedCall(ir, options, operation) ||
						plan.closures.scalarValue(use.value) ||
						plan.callSets.hasCall(operation) ||
						plan.closureSets.hasSet(operation)) {
					continue
				}
				if operation.op == opCallMethodOne &&
					use.register == operation.b &&
					plan.tables.root(use.value) != invalidBackendValueID {
					continue
				}
				markUsed(use.value)
			}
		}
	}
	for blockIndex := range ir.blocks {
		block := &ir.blocks[blockIndex]
		if !block.reachable {
			continue
		}
		for pc := block.first; pc < block.last; pc++ {
			operation := &ir.ops[pc]
			if backendGoNumericOperationDead(plan, operation) {
				continue
			}
			if err := verifyBackendGoNumericOperation(ir, plan, options, operation); err != nil {
				return backendGoNumericPlan{}, err
			}
		}
	}
	for valueIndex, used := range plan.used {
		if !used {
			continue
		}
		if plan.tags[valueIndex] != backendTagNil {
			if _, ok := backendGoNumericTypeForValue(plan, backendValueID(valueIndex+1)); ok {
				continue
			}
			return backendGoNumericPlan{}, fmt.Errorf("emit backend Go numeric proof: SSA value %d has nonnumeric union %x", valueIndex+1, plan.tags[valueIndex])
		}
	}
	return plan, nil
}

func backendGoNumericDirectTarget(options backendGoNumericOptions, operation *backendOperationIR) (backendGoNumericTarget, bool) {
	if operation == nil ||
		operation.call.kind != backendCallDirectProto ||
		operation.call.targetProto < 0 ||
		int(operation.call.targetProto) >= len(options.directTargets) {
		return backendGoNumericTarget{}, false
	}
	target := options.directTargets[operation.call.targetProto]
	return target, target.ir != nil && target.functionName != ""
}

func backendGoNumericScalarReplacedCall(
	ir *backendProtoIR,
	options backendGoNumericOptions,
	operation *backendOperationIR,
) bool {
	if ir == nil || operation == nil {
		return false
	}
	switch operation.op {
	case opCall:
		if operation.callArgCount < 0 || operation.callResults < 2 {
			return false
		}
	case opCallLocalOne:
		if operation.callArgCount < 0 || operation.callResults != 1 {
			return false
		}
	default:
		return false
	}
	target, ok := backendGoNumericDirectTarget(options, operation)
	if !ok {
		return false
	}
	argumentCount, ok := backendGoNumericArgumentCount(target.ir, target.fixedVarargCount)
	if !ok || operation.callArgCount != int32(argumentCount) {
		return false
	}
	resultCount, ok := backendGoNumericFixedResultCountFor(target.ir, target.fixedVarargCount)
	if !ok || operation.callResults != int32(resultCount) {
		return false
	}
	valueID := backendOperationUse(operation, operation.b)
	if !ir.validBackendValue(valueID) {
		return false
	}
	value := &ir.values[valueID-1]
	if value.object != backendObjectClosure ||
		value.targetUnknown ||
		len(value.targetProtos) != 1 ||
		value.targetProtos[0] != operation.call.targetProto {
		return false
	}
	if target.selfRecursive {
		if value.escapes {
			return false
		}
		return backendGoNumericSelfClosure(ir, valueID, target.ir)
	}
	if len(target.ir.upvalues) == 0 {
		return !value.escapes
	}
	_, _, _, captured := backendGoCapturedRecordCallShape(ir, options, operation)
	return captured
}

func backendGoBorrowedCallSource(
	ir *backendProtoIR,
	operation *backendOperationIR,
	register int32,
) (backendValueID, bool) {
	if ir == nil || operation == nil ||
		register < operation.a+operation.callResults ||
		register < 0 || register >= int32(ir.registers) {
		return invalidBackendValueID, false
	}
	source := backendValueBeforeOperation(ir, operation, register)
	return source, ir.validBackendValue(source)
}

func backendGoNumericPureProducer(op opcode) bool {
	switch op {
	case opLoadConst, opMove,
		opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow, opNeg,
		opAddK, opSubK, opMulK, opDivK, opModK, opIDivK,
		opClosure, opNewTable, opGetStringField, opGetUpvalue, opVararg:
		return true
	default:
		return false
	}
}

func backendGoNumericOperationDead(plan backendGoNumericPlan, operation *backendOperationIR) bool {
	if operation == nil {
		return false
	}
	if _, ok := plan.keys.tostring(operation); !ok {
		if _, ok := plan.keys.concat(operation); !ok && !backendGoNumericPureProducer(operation.op) {
			return false
		}
	}
	for _, definition := range operation.defs {
		if plan.used[definition.value-1] {
			return false
		}
	}
	return true
}

func verifyBackendGoNumericOperation(
	ir *backendProtoIR,
	plan backendGoNumericPlan,
	options backendGoNumericOptions,
	operation *backendOperationIR,
) error {
	require := func(register int32, want backendTagMask) error {
		id := backendOperationUse(operation, register)
		if id == invalidBackendValueID {
			return fmt.Errorf("emit backend Go numeric proof: PC %d register %d has no SSA use", operation.pc, register)
		}
		if plan.tags[id-1] != want {
			return fmt.Errorf("emit backend Go numeric proof: PC %d %s register %d has tags %x, want %x", operation.pc, opcodeName(operation.op), register, plan.tags[id-1], want)
		}
		return nil
	}
	requireOptional := func(register int32, want backendTagMask) error {
		id := backendOperationUse(operation, register)
		if id == invalidBackendValueID {
			return fmt.Errorf("emit backend Go numeric proof: PC %d register %d has no SSA use", operation.pc, register)
		}
		if plan.tags[id-1] == want {
			return nil
		}
		payload, optional := backendGoOptionalScalarTags(plan.tags[id-1])
		if !optional || payload != want {
			return fmt.Errorf("emit backend Go numeric proof: PC %d register %d has tags %x, want optional %x", operation.pc, register, plan.tags[id-1], want)
		}
		return nil
	}
	requireStore := func(register int32, fieldTags backendTagMask) error {
		id := backendOperationUse(operation, register)
		if id == invalidBackendValueID {
			return fmt.Errorf("emit backend Go numeric proof: PC %d register %d has no SSA use", operation.pc, register)
		}
		if !backendGoRecordStoreCompatible(plan.tags[id-1], fieldTags) {
			return fmt.Errorf("emit backend Go numeric proof: PC %d register %d has tags %x, incompatible with field %x", operation.pc, register, plan.tags[id-1], fieldTags)
		}
		return nil
	}
	switch operation.op {
	case opLoadConst:
		if operation.b < 0 || int(operation.b) >= len(ir.constants) {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has invalid constant", operation.pc)
		}
		for _, definition := range operation.defs {
			if plan.coroutines.dead(definition.value) {
				return nil
			}
		}
		switch ir.constants[operation.b].kind {
		case NilKind, NumberKind, BoolKind, StringKind:
			return nil
		default:
			return fmt.Errorf("emit backend Go numeric proof: PC %d loads unsupported %s constant", operation.pc, ir.constants[operation.b].kind)
		}
	case opMove:
		return nil
	case opClosure:
		for _, definition := range operation.defs {
			if plan.used[definition.value-1] {
				return fmt.Errorf("emit backend Go numeric proof: PC %d closure escapes scalar replacement", operation.pc)
			}
		}
		return nil
	case opNewTable:
		for _, definition := range operation.defs {
			if plan.tables.root(definition.value) == invalidBackendValueID &&
				plan.records.root(definition.value) == invalidBackendValueID &&
				!plan.scalarReplacedValues[definition.value-1] {
				return fmt.Errorf("emit backend Go numeric proof: PC %d table is not scalar replaceable", operation.pc)
			}
		}
		return nil
	case opSetStringField:
		if plan.callSets.setter(operation) || plan.closureSets.setter(operation) {
			return nil
		}
		if field, ok := plan.records.fieldsByPC[operation.pc]; ok {
			if _, child := plan.records.childSetByPC[operation.pc]; child {
				return nil
			}
			if _, child := plan.records.childRecordSet[operation.pc]; child {
				return nil
			}
			return requireStore(operation.c, plan.records.fieldTags(field))
		}
		_, field, ok := plan.tables.operationField(ir, operation)
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar field", operation.pc)
		}
		source := backendOperationUse(operation, operation.c)
		if field.methodProto >= 0 {
			proto, ok := backendGoScalarMethodClosure(ir, source)
			if !ok ||
				proto != field.methodProto ||
				!plan.methods.scalarClosure(source) {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar method identity", operation.pc)
			}
			return nil
		}
		if field.child != invalidBackendValueID {
			if plan.tables.root(source) != field.child {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar table identity", operation.pc)
			}
			return nil
		}
		return require(operation.c, field.tags)
	case opSetStringFieldIndex:
		fused, ok := plan.records.fusedSetByPC[operation.pc]
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no fused child-record mutation", operation.pc)
		}
		if err := requireOptional(operation.c, backendTagString); err != nil {
			return err
		}
		return requireStore(operation.d, plan.records.childRecords[fused.family].fieldTags)
	case opSetField:
		if plan.closureSets.setter(operation) {
			return nil
		}
		if field, ok := plan.records.fieldsByPC[operation.pc]; ok {
			return requireStore(operation.c, plan.records.fieldTagsFor(plan.tags, field))
		}
		if _, ok := plan.records.arraySetByPC[operation.pc]; ok {
			return nil
		}
		_, array, _, ok := plan.tables.arrayOperation(ir, operation)
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar array element", operation.pc)
		}
		return require(operation.c, array.tags)
	case opSetIndex:
		if _, ok := plan.records.arraySetByPC[operation.pc]; ok {
			return requireOptional(operation.b, backendTagNumber)
		}
		if dynamic, ok := plan.records.dynamicSetByPC[operation.pc]; ok {
			if err := requireOptional(operation.b, backendTagString); err != nil {
				return err
			}
			return requireStore(operation.c, plan.records.dynamicFieldTags(plan.tags, dynamic))
		}
		if _, ok := plan.records.mapSetByPC[operation.pc]; !ok {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar record-map store", operation.pc)
		}
		return nil
	case opGetIndex:
		if _, ok := plan.closureSets.selector(operation); ok {
			return require(operation.c, backendTagString)
		}
		if plan.callSets.selector(operation) {
			return require(operation.c, backendTagString)
		}
		if field, ok := plan.records.fieldsByPC[operation.pc]; ok {
			if len(operation.defs) != 1 || plan.tags[operation.defs[0].value-1] != plan.records.fieldTagsFor(plan.tags, field) {
				return fmt.Errorf("emit backend Go numeric proof: PC %d positional record field changes scalar tags", operation.pc)
			}
			return nil
		}
		if _, _, _, ok := plan.tables.arrayOperation(ir, operation); ok {
			return requireOptional(operation.c, backendTagNumber)
		}
		if _, ok := plan.records.dynamicChildSelectByPC[operation.pc]; ok {
			if err := requireOptional(operation.c, backendTagString); err != nil {
				return err
			}
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != backendTagNumber {
					return fmt.Errorf("emit backend Go numeric proof: PC %d child-record selector is not scalar", operation.pc)
				}
			}
			return nil
		}
		if dynamic, ok := plan.records.dynamicChildGetByPC[operation.pc]; ok {
			if err := requireOptional(operation.c, backendTagString); err != nil {
				return err
			}
			fieldTags := plan.records.childRecordFieldTags(plan.tags, dynamic.family)
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != fieldTags {
					return fmt.Errorf("emit backend Go numeric proof: PC %d dynamic child-record lookup changes scalar tags", operation.pc)
				}
			}
			return nil
		}
		if dynamic, ok := plan.records.dynamicGetByPC[operation.pc]; ok {
			if err := requireOptional(operation.c, backendTagString); err != nil {
				return err
			}
			fieldTags := plan.records.dynamicFieldTags(plan.tags, dynamic)
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != fieldTags {
					return fmt.Errorf("emit backend Go numeric proof: PC %d dynamic record lookup changes scalar tags", operation.pc)
				}
			}
			return nil
		}
		_, mapGet := plan.records.mapGetByPC[operation.pc]
		_, arrayGet := plan.records.arrayGetByPC[operation.pc]
		if !mapGet && !arrayGet {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar record lookup", operation.pc)
		}
		if arrayGet {
			if err := requireOptional(operation.c, backendTagNumber); err != nil {
				return err
			}
		}
		for _, definition := range operation.defs {
			if plan.tags[definition.value-1] != backendTagNumber {
				return fmt.Errorf("emit backend Go numeric proof: PC %d record lookup result is not scalar", operation.pc)
			}
		}
		return nil
	case opGetStringFieldIndex:
		if _, ok := plan.records.familyGetByPC[operation.pc]; ok {
			if err := requireOptional(operation.d, backendTagNumber); err != nil {
				return err
			}
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != backendTagNumber {
					return fmt.Errorf("emit backend Go numeric proof: PC %d child-array lookup result is not scalar", operation.pc)
				}
			}
			return nil
		}
		fused, ok := plan.records.fusedGetByPC[operation.pc]
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no fused child-record lookup", operation.pc)
		}
		if err := requireOptional(operation.d, backendTagString); err != nil {
			return err
		}
		fieldTags := plan.records.childRecords[fused.family].fieldTags
		for _, definition := range operation.defs {
			if plan.tags[definition.value-1] != fieldTags {
				return fmt.Errorf(
					"emit backend Go numeric proof: PC %d fused lookup result has tags %x, want %x",
					operation.pc,
					plan.tags[definition.value-1],
					fieldTags,
				)
			}
		}
		return nil
	case opGetStringField:
		if field, ok := plan.records.fieldsByPC[operation.pc]; ok {
			fieldTags := plan.records.fieldTags(field)
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != fieldTags {
					return fmt.Errorf(
						"emit backend Go numeric proof: PC %d record field result has tags %x, want %x",
						operation.pc,
						plan.tags[definition.value-1],
						fieldTags,
					)
				}
			}
			return nil
		}
		_, field, ok := plan.tables.operationField(ir, operation)
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar field", operation.pc)
		}
		if field.child != invalidBackendValueID {
			for _, definition := range operation.defs {
				if plan.tables.root(definition.value) != field.child {
					return fmt.Errorf("emit backend Go numeric proof: PC %d loses scalar table identity", operation.pc)
				}
			}
		}
		if field.methodProto >= 0 {
			return fmt.Errorf("emit backend Go numeric proof: PC %d materializes scalar method", operation.pc)
		}
		return nil
	case opPrepareIter:
		if _, ok := plan.closureSets.prepare(operation); ok {
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != backendTagNumber {
					return fmt.Errorf("emit backend Go numeric proof: PC %d finite closure iterator state is not scalar", operation.pc)
				}
			}
			return nil
		}
		_, recordArray := plan.records.arrayPreparePC[operation.pc]
		_, recordFamily := plan.records.familyPrepare[operation.pc]
		if recordArray || recordFamily {
			for _, definition := range operation.defs {
				if !plan.records.iteratorValue(definition.value) {
					return fmt.Errorf("emit backend Go numeric proof: PC %d materializes record iterator state", operation.pc)
				}
			}
			return nil
		}
		_, _, _, ok := plan.tables.arrayOperation(ir, operation)
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar array iterator", operation.pc)
		}
		for _, definition := range operation.defs {
			if definition.register == operation.a {
				if plan.tables.root(definition.value) == invalidBackendValueID {
					return fmt.Errorf("emit backend Go numeric proof: PC %d loses scalar array identity", operation.pc)
				}
				continue
			}
			if !plan.tables.iteratorValue(definition.value) {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has materialized iterator state", operation.pc)
			}
		}
		return nil
	case opArrayNextJump2:
		if _, ok := plan.closureSets.next(operation); ok {
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != backendTagNumber {
					return fmt.Errorf("emit backend Go numeric proof: PC %d finite closure iterator result is not scalar", operation.pc)
				}
			}
			return nil
		}
		_, recordArray := plan.records.arrayNextPC[operation.pc]
		_, recordFamily := plan.records.familyNext[operation.pc]
		if recordArray || recordFamily {
			for _, definition := range operation.defs {
				switch definition.register {
				case operation.a:
					if _, observed := plan.records.arrayKeyValues[definition.value]; observed {
						if plan.tags[definition.value-1] != backendTagNumber {
							return fmt.Errorf("emit backend Go numeric proof: PC %d loses observed record iterator key", operation.pc)
						}
					} else if !plan.records.iteratorValue(definition.value) {
						return fmt.Errorf("emit backend Go numeric proof: PC %d materializes record iterator control", operation.pc)
					}
				case operation.a + 1:
					if _, ok := plan.records.ref(definition.value); !ok ||
						plan.tags[definition.value-1] != backendTagNumber {
						return fmt.Errorf("emit backend Go numeric proof: PC %d loses record iterator value", operation.pc)
					}
				}
			}
			return nil
		}
		_, array, _, ok := plan.tables.arrayOperation(ir, operation)
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar array iterator", operation.pc)
		}
		if !plan.tables.iteratorValue(backendOperationUse(operation, operation.c)) ||
			!plan.tables.iteratorValue(backendOperationUse(operation, operation.a)) {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has materialized iterator control", operation.pc)
		}
		for _, definition := range operation.defs {
			switch definition.register {
			case operation.a:
				if !plan.tables.iteratorValue(definition.value) {
					return fmt.Errorf("emit backend Go numeric proof: PC %d materializes iterator key", operation.pc)
				}
			case operation.a + 1:
				if plan.tags[definition.value-1] != array.tags {
					return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar array element tags", operation.pc)
				}
			}
		}
		return nil
	case opFastCall:
		if _, ok := plan.records.arrayRawLenPC[operation.pc]; ok {
			if operation.c != 1 || operation.d != 1 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes record-array rawlen shape", operation.pc)
			}
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != backendTagNumber {
					return fmt.Errorf("emit backend Go numeric proof: PC %d record-array rawlen result is not numeric", operation.pc)
				}
			}
			return nil
		}
		if _, ok := plan.records.arrayInsertPC[operation.pc]; ok {
			if operation.c != 2 || operation.d != 1 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes record-array insert shape", operation.pc)
			}
			for _, definition := range operation.defs {
				if plan.used[definition.value-1] || plan.tags[definition.value-1] != backendTagNil {
					return fmt.Errorf("emit backend Go numeric proof: PC %d observes record-array insert result", operation.pc)
				}
			}
			return nil
		}
		if _, ok := plan.records.arrayRemovePC[operation.pc]; ok {
			if operation.c != 2 || operation.d != 1 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes record-array remove shape", operation.pc)
			}
			if err := requireOptional(operation.a+1, backendTagNumber); err != nil {
				return err
			}
			for _, definition := range operation.defs {
				if plan.used[definition.value-1] || plan.tags[definition.value-1] != backendTagNil {
					return fmt.Errorf("emit backend Go numeric proof: PC %d observes removed record", operation.pc)
				}
			}
			return nil
		}
		if _, ok := plan.records.familyRawLenPC[operation.pc]; ok {
			if operation.c != 1 || operation.d != 1 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes child-array rawlen shape", operation.pc)
			}
			if err := require(operation.a, backendTagNumber); err != nil {
				return err
			}
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != backendTagNumber {
					return fmt.Errorf("emit backend Go numeric proof: PC %d child-array rawlen result is not numeric", operation.pc)
				}
			}
			return nil
		}
		if _, ok := plan.records.familyRemovePC[operation.pc]; ok {
			if operation.c != 2 || operation.d != 1 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes child-array remove shape", operation.pc)
			}
			if err := require(operation.a, backendTagNumber); err != nil {
				return err
			}
			if err := requireOptional(operation.a+1, backendTagNumber); err != nil {
				return err
			}
			for _, definition := range operation.defs {
				if plan.used[definition.value-1] || plan.tags[definition.value-1] != backendTagNil {
					return fmt.Errorf("emit backend Go numeric proof: PC %d observes removed child record", operation.pc)
				}
			}
			return nil
		}
		if _, ok := plan.keys.tostring(operation); ok {
			if operation.c != 1 || operation.d != 1 || len(operation.defs) != 1 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes structural tostring shape", operation.pc)
			}
			if err := require(operation.a, backendTagNumber); err != nil {
				return err
			}
			if plan.tags[operation.defs[0].value-1] != backendTagString {
				return fmt.Errorf("emit backend Go numeric proof: PC %d structural tostring result is not string", operation.pc)
			}
			return nil
		}
		if backendGoNumericMathMin(operation) {
			if operation.c <= 0 || operation.d != 1 ||
				len(operation.defs) != 1 ||
				operation.defs[0].register != operation.a ||
				plan.tags[operation.defs[0].value-1] != backendTagNumber {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes math.min shape", operation.pc)
			}
			for argument := int32(0); argument < operation.c; argument++ {
				if err := requireOptional(operation.a+argument, backendTagNumber); err != nil {
					return err
				}
			}
			return nil
		}
		if plan.coroutines.createOperation(operation) {
			if operation.c != 1 || operation.d != 1 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar coroutine.create shape", operation.pc)
			}
			return nil
		}
		if _, ok := plan.coroutines.resume(operation); ok {
			if operation.d != 2 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar coroutine.resume results", operation.pc)
			}
			for _, definition := range operation.defs {
				switch definition.register {
				case operation.a:
					if plan.tags[definition.value-1] != backendTagBool {
						return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar coroutine.resume success", operation.pc)
					}
				case operation.a + 1:
					if plan.tags[definition.value-1] != backendTagNumber {
						return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar coroutine.resume value", operation.pc)
					}
				default:
					return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar coroutine.resume window", operation.pc)
				}
			}
			if operation.c == 2 {
				return require(operation.a+1, backendTagNumber)
			}
			return nil
		}
		if plan.coroutines.statusOperation(operation) {
			if operation.c != 1 || operation.d != 1 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar coroutine.status shape", operation.pc)
			}
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != backendTagBool {
					return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar coroutine.status result", operation.pc)
				}
			}
			return nil
		}
		if options.coroutineTarget &&
			nativeFuncID(operation.nativeID) == nativeFuncCoroutineYield {
			if operation.c != 1 || operation.d != 1 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar coroutine.yield shape", operation.pc)
			}
			if err := require(operation.a, backendTagNumber); err != nil {
				return err
			}
			for _, definition := range operation.defs {
				if plan.used[definition.value-1] {
					return fmt.Errorf("emit backend Go numeric proof: PC %d consumes resumed coroutine values", operation.pc)
				}
			}
			return nil
		}
		if backendGoNumericFixedVarargSelect(ir, options.fixedVarargCount, operation) {
			if len(operation.defs) != 1 ||
				operation.defs[0].register != operation.a ||
				plan.tags[operation.defs[0].value-1] != backendTagNumber {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes fixed select result", operation.pc)
			}
			return nil
		}
		if metatable, ok := plan.tables.metatableOperation(operation); ok {
			if operation.c != 2 || operation.d != 1 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported setmetatable shape", operation.pc)
			}
			if plan.tables.root(backendOperationUse(operation, operation.a)) != metatable.table ||
				plan.tables.root(backendOperationUse(operation, operation.a+1)) != metatable.metatable {
				return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar metatable inputs", operation.pc)
			}
			for _, definition := range operation.defs {
				if definition.register != operation.a ||
					plan.tables.root(definition.value) != metatable.table ||
					plan.tags[definition.value-1] != backendTagTable {
					return fmt.Errorf("emit backend Go numeric proof: PC %d changes setmetatable result identity", operation.pc)
				}
			}
			return nil
		}
		_, array, _, ok := plan.tables.arrayOperation(ir, operation)
		if !ok {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar table intrinsic", operation.pc)
		}
		switch nativeFuncID(operation.nativeID) {
		case nativeFuncTableInsert:
			if operation.c != 2 || operation.d != 1 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported table.insert shape", operation.pc)
			}
			if err := require(operation.a+1, array.tags); err != nil {
				return err
			}
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != backendTagNil {
					return fmt.Errorf("emit backend Go numeric proof: PC %d table.insert result is not nil", operation.pc)
				}
			}
			return nil
		case nativeFuncTableRemove:
			if operation.c != 2 || operation.d != 1 ||
				!backendGoStaticNumberEquals(ir, backendOperationUse(operation, operation.a+1), 1) {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported table.remove shape", operation.pc)
			}
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != array.tags {
					return fmt.Errorf("emit backend Go numeric proof: PC %d changes table.remove result tags", operation.pc)
				}
			}
			return nil
		case nativeFuncRawLen:
			if operation.c != 1 || operation.d != 1 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported rawlen shape", operation.pc)
			}
			for _, definition := range operation.defs {
				if plan.tags[definition.value-1] != backendTagNumber {
					return fmt.Errorf("emit backend Go numeric proof: PC %d changes rawlen result tags", operation.pc)
				}
			}
			return nil
		default:
			return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported scalar array intrinsic", operation.pc)
		}
	case opConcatChain:
		key, ok := plan.keys.concat(operation)
		if !ok || operation.c != 3 || len(operation.defs) != 1 {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no structural concat key", operation.pc)
		}
		if plan.tags[key.first-1] != backendTagNumber ||
			plan.tags[key.second-1] != backendTagNumber ||
			plan.tags[operation.defs[0].value-1] != backendTagString {
			return fmt.Errorf("emit backend Go numeric proof: PC %d changes structural concat inputs", operation.pc)
		}
		return nil
	case opVararg:
		if operation.b < 0 || len(operation.defs) != int(operation.b) {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported open vararg shape", operation.pc)
		}
		for _, definition := range operation.defs {
			if _, ok := backendGoNumericVarargIndex(
				ir,
				options.fixedVarargCount,
				operation,
				definition.register,
			); !ok || plan.tags[definition.value-1] != backendTagNumber {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported fixed vararg result", operation.pc)
			}
		}
		return nil
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		if err := requireOptional(operation.b, backendTagNumber); err != nil {
			return err
		}
		return requireOptional(operation.c, backendTagNumber)
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		if err := requireOptional(operation.b, backendTagNumber); err != nil {
			return err
		}
		return verifyBackendGoNumericConstant(ir, operation, operation.c)
	case opNeg:
		return requireOptional(operation.b, backendTagNumber)
	case opEqual, opNotEqual:
		left := backendOperationUse(operation, operation.b)
		right := backendOperationUse(operation, operation.c)
		if left == invalidBackendValueID || right == invalidBackendValueID {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported equality operands", operation.pc)
		}
		leftRef, leftIsRef := plan.records.ref(left)
		rightRef, rightIsRef := plan.records.ref(right)
		if leftIsRef || rightIsRef {
			if leftIsRef && plan.tags[right-1] == backendTagNil ||
				rightIsRef && plan.tags[left-1] == backendTagNil ||
				leftIsRef && rightIsRef && leftRef == rightRef {
				return nil
			}
			return fmt.Errorf("emit backend Go numeric proof: PC %d compares incompatible record references", operation.pc)
		}
		leftTags := plan.tags[left-1]
		rightTags := plan.tags[right-1]
		leftPayload, leftOptional := backendGoOptionalScalarTags(leftTags)
		rightPayload, rightOptional := backendGoOptionalScalarTags(rightTags)
		compatible := leftTags == rightTags &&
			(leftTags == backendTagNumber || leftTags == backendTagBool || leftTags == backendTagString)
		if leftOptional && rightTags == backendTagNil ||
			rightOptional && leftTags == backendTagNil ||
			leftOptional && rightOptional && leftPayload == rightPayload ||
			leftOptional && rightTags == leftPayload ||
			rightOptional && leftTags == rightPayload {
			compatible = true
		}
		if !compatible {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported equality operands %x and %x", operation.pc, leftTags, rightTags)
		}
		if leftTags&backendTagString != 0 && rightTags&backendTagString != 0 {
			leftKey, leftOK := plan.keys.key(left)
			rightKey, rightOK := plan.keys.key(right)
			if leftOK != rightOK || leftOK && leftKey.domain != rightKey.domain {
				return fmt.Errorf("emit backend Go numeric proof: PC %d compares incompatible string domains", operation.pc)
			}
		}
		return nil
	case opLess, opLessEqual, opGreater, opGreaterEqual:
		if err := requireOptional(operation.b, backendTagNumber); err != nil {
			return err
		}
		return requireOptional(operation.c, backendTagNumber)
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		if err := requireOptional(operation.a, backendTagNumber); err != nil {
			return err
		}
		return requireOptional(operation.b, backendTagNumber)
	case opNumericForCheck:
		if err := requireOptional(operation.a, backendTagNumber); err != nil {
			return err
		}
		if err := requireOptional(operation.b, backendTagNumber); err != nil {
			return err
		}
		return requireOptional(operation.c, backendTagNumber)
	case opNumericForLoop:
		if err := requireOptional(operation.a, backendTagNumber); err != nil {
			return err
		}
		return requireOptional(operation.b, backendTagNumber)
	case opGetUpvalue:
		if operation.b < 0 || int(operation.b) >= len(ir.upvalues) {
			return fmt.Errorf("emit backend Go numeric proof: PC %d reads invalid upvalue %d", operation.pc, operation.b)
		}
		for _, definition := range operation.defs {
			if plan.tables.root(definition.value) != invalidBackendValueID {
				continue
			}
			if plan.tags[definition.value-1] != backendTagNumber {
				return fmt.Errorf("emit backend Go numeric proof: PC %d reads a nonnumeric upvalue", operation.pc)
			}
		}
		return nil
	case opSetUpvalue:
		if operation.a < 0 || int(operation.a) >= len(ir.upvalues) {
			return fmt.Errorf("emit backend Go numeric proof: PC %d writes invalid upvalue %d", operation.pc, operation.a)
		}
		return require(operation.b, backendTagNumber)
	case opJumpIfNotEqualK:
		left := backendOperationUse(operation, operation.a)
		if left == invalidBackendValueID {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no equality operand", operation.pc)
		}
		tags := plan.tags[left-1]
		if payload, optional := backendGoOptionalScalarTags(tags); optional {
			tags = payload
		}
		return verifyBackendGoComparableConstant(ir, operation, operation.b, tags)
	case opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
		if err := requireOptional(operation.a, backendTagNumber); err != nil {
			return err
		}
		return verifyBackendGoNumericConstant(ir, operation, operation.b)
	case opJumpIfFalse:
		id := backendOperationUse(operation, operation.a)
		if id == invalidBackendValueID {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported truthy operand", operation.pc)
		}
		tags := plan.tags[id-1]
		if tags != backendTagNumber && tags != backendTagBool && tags != backendTagString {
			if _, optional := backendGoOptionalScalarTags(tags); !optional {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported truthy operand", operation.pc)
			}
		}
		return nil
	case opJumpIfTableHasMetatable:
		id := backendOperationUse(operation, operation.a)
		if plan.records.root(id) == invalidBackendValueID {
			if _, ok := plan.records.ref(id); !ok {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar record metatable guard", operation.pc)
			}
		}
		return nil
	case opJump:
		return nil
	case opCall:
		target, ok := backendGoNumericDirectTarget(options, operation)
		if !ok || !backendGoNumericScalarReplacedCall(ir, options, operation) {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no fixed-result numeric target", operation.pc)
		}
		argumentCount, argsOK := backendGoNumericArgumentCount(target.ir, target.fixedVarargCount)
		resultCount, resultsOK := backendGoNumericFixedResultCountFor(target.ir, target.fixedVarargCount)
		if !argsOK || !resultsOK ||
			operation.callArgCount != int32(argumentCount) ||
			operation.callResults != int32(resultCount) {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported fixed-result call shape", operation.pc)
		}
		parameterTags, parametersOK := backendGoNumericParameterTags(target.ir, target.fixedVarargCount)
		if !parametersOK {
			return fmt.Errorf("emit backend Go numeric proof: PC %d target has no bounded scalar parameter types", operation.pc)
		}
		for argument := int32(0); argument < operation.callArgCount; argument++ {
			if err := require(operation.callArgStart+argument, parameterTags[argument]); err != nil {
				return err
			}
		}
		if len(target.ir.upvalues) != 0 {
			captured, capturedOK := plan.captured.call(operation)
			if !capturedOK || len(captured.callerFields) == 0 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has no explicit captured-record ownership", operation.pc)
			}
			for _, field := range captured.callerFields {
				if plan.records.fieldTagsFor(plan.tags, field) != backendTagNumber {
					return fmt.Errorf("emit backend Go numeric proof: PC %d captures a nonnumeric record field", operation.pc)
				}
			}
		}
		for register := operation.a; register < operation.a+operation.callResults; register++ {
			found := false
			for _, definition := range operation.defs {
				if definition.register != register {
					continue
				}
				found = true
				want := backendGoDirectTargetResultTag(options, operation, int(register-operation.a))
				if want == 0 || plan.tags[definition.value-1] != want {
					return fmt.Errorf(
						"emit backend Go numeric proof: PC %d result register %d has tags %x, want %x",
						operation.pc,
						register,
						plan.tags[definition.value-1],
						want,
					)
				}
				break
			}
			if !found {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has no result definition for register %d", operation.pc, register)
			}
		}
		for _, definition := range operation.defs {
			if definition.register >= operation.a &&
				definition.register < operation.a+operation.callResults {
				continue
			}
			if !plan.used[definition.value-1] {
				continue
			}
			source, sourceOK := backendGoBorrowedCallSource(ir, operation, definition.register)
			if !sourceOK || plan.tags[definition.value-1] != plan.tags[source-1] {
				return fmt.Errorf("emit backend Go numeric proof: PC %d uses unsupported borrowed call suffix register %d", operation.pc, definition.register)
			}
		}
		return nil
	case opCallOne:
		if call, ok := plan.callSets.call(operation); ok {
			if operation.callArgCount < 2 || len(operation.defs) != 1 ||
				operation.defs[0].register != operation.a ||
				plan.tags[operation.defs[0].value-1] != backendTagNumber {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has invalid finite call-set result", operation.pc)
			}
			key := call.key
			if !ir.validBackendValue(key) || plan.tags[key-1] != backendTagString {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has a non-string finite call-set selector", operation.pc)
			}
			for _, variant := range call.variants {
				parameterTags, tagsOK := backendGoNumericParameterTags(variant.target.ir, variant.target.fixedVarargCount)
				if !tagsOK || len(parameterTags) != int(operation.callArgCount) || len(variant.callerFields) == 0 {
					return fmt.Errorf("emit backend Go numeric proof: PC %d has an invalid finite call-set target", operation.pc)
				}
				for _, field := range variant.callerFields {
					if plan.records.fieldTagsFor(plan.tags, field) != backendTagNumber {
						return fmt.Errorf("emit backend Go numeric proof: PC %d finite call-set receiver field is not numeric", operation.pc)
					}
				}
				for argument := int32(1); argument < operation.callArgCount; argument++ {
					if err := require(operation.callArgStart+argument, parameterTags[argument]); err != nil {
						return err
					}
				}
			}
			return nil
		}
		return fmt.Errorf("emit backend Go numeric proof: PC %d has no finite call-set target", operation.pc)
	case opCallLocalOne:
		if set, ok := plan.closureSets.set(operation); ok {
			if len(operation.defs) != 1 || plan.tags[operation.defs[0].value-1] != backendTagNumber ||
				len(set.receiverFields) == 0 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has invalid finite closure result", operation.pc)
			}
			for _, field := range set.receiverFields {
				if plan.records.fieldTagsFor(plan.tags, field.field) != field.tags {
					return fmt.Errorf("emit backend Go numeric proof: PC %d finite closure receiver field changes tags", operation.pc)
				}
				if _, ok := backendGoFiniteClosureFieldPointer(field.field); !ok {
					return fmt.Errorf("emit backend Go numeric proof: PC %d finite closure receiver storage is unsupported", operation.pc)
				}
			}
			return nil
		}
		if factory, _, ok := plan.closures.factory(operation); ok {
			if len(factory.captures) == 0 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has no closure captures", operation.pc)
			}
			for _, capture := range factory.captures {
				if capture.argument < 0 {
					continue
				}
				if capture.argument >= operation.callArgCount {
					return fmt.Errorf("emit backend Go numeric proof: PC %d has invalid closure capture argument", operation.pc)
				}
				if err := require(operation.callArgStart+capture.argument, backendTagNumber); err != nil {
					return err
				}
			}
			return nil
		}
		if call, ok := plan.closures.call(operation); ok {
			if len(call.target.ir.upvalues) != call.cellCount || call.cellCount <= 0 ||
				call.target.ir.variadic ||
				operation.callArgCount != int32(call.target.ir.params) ||
				operation.callArgCount < 0 {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported scalar closure call shape", operation.pc)
			}
			for register := operation.callArgStart; register < operation.callArgStart+operation.callArgCount; register++ {
				if err := require(register, backendTagNumber); err != nil {
					return err
				}
			}
			return nil
		}
		target, ok := backendGoNumericDirectTarget(options, operation)
		if !ok || !backendGoNumericScalarReplacedCall(ir, options, operation) {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no direct numeric target", operation.pc)
		}
		argumentCount, ok := backendGoNumericArgumentCount(target.ir, target.fixedVarargCount)
		if !ok ||
			!target.selfRecursive && len(target.ir.upvalues) != 0 ||
			operation.callArgCount != int32(argumentCount) ||
			operation.callArgCount < 0 {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported direct call shape", operation.pc)
		}
		parameterTags, parametersOK := backendGoNumericParameterTags(target.ir, target.fixedVarargCount)
		if !parametersOK {
			return fmt.Errorf("emit backend Go numeric proof: PC %d target has no bounded scalar parameter types", operation.pc)
		}
		for argument := int32(0); argument < operation.callArgCount; argument++ {
			if err := require(operation.callArgStart+argument, parameterTags[argument]); err != nil {
				return err
			}
		}
		want := backendGoDirectTargetResultTag(options, operation, 0)
		if want == 0 {
			return fmt.Errorf("emit backend Go numeric proof: PC %d direct target has no scalar result", operation.pc)
		}
		for _, definition := range operation.defs {
			if definition.register == operation.a && plan.tags[definition.value-1] != want {
				return fmt.Errorf(
					"emit backend Go numeric proof: PC %d result has tags %x, want %x",
					operation.pc,
					plan.tags[definition.value-1],
					want,
				)
			}
		}
		return nil
	case opCallUpvalueOne:
		if !options.selfRecursive ||
			operation.b != 0 ||
			operation.callArgCount != int32(ir.params) ||
			operation.callArgCount != 1 {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported recursive call shape", operation.pc)
		}
		return require(operation.callArgStart, backendTagNumber)
	case opCallMethodOne:
		call, ok := plan.methods.call(operation)
		if !ok ||
			call.target.ir == nil ||
			!call.target.receiverTable ||
			operation.callArgCount != int32(call.target.ir.params) ||
			operation.callArgCount < 1 ||
			operation.callResults != 1 {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no static scalar method target", operation.pc)
		}
		if plan.tables.root(backendOperationUse(operation, operation.b)) != call.receiverRoot {
			return fmt.Errorf("emit backend Go numeric proof: PC %d changes scalar method receiver", operation.pc)
		}
		for argument := int32(1); argument < operation.callArgCount; argument++ {
			if err := require(operation.callArgStart+argument, backendTagNumber); err != nil {
				return err
			}
		}
		for _, definition := range operation.defs {
			switch definition.register {
			case operation.a:
				if plan.tags[definition.value-1] != backendTagNumber {
					return fmt.Errorf("emit backend Go numeric proof: PC %d method result is not numeric", operation.pc)
				}
			case operation.callArgStart:
				if plan.tables.root(definition.value) != call.receiverRoot {
					return fmt.Errorf("emit backend Go numeric proof: PC %d loses scalar method receiver", operation.pc)
				}
			default:
				if plan.used[definition.value-1] {
					return fmt.Errorf("emit backend Go numeric proof: PC %d uses borrowed method register %d", operation.pc, definition.register)
				}
			}
		}
		return nil
	case opReturnOne:
		id := backendOperationUse(operation, operation.a)
		if id == invalidBackendValueID {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has no return value", operation.pc)
		}
		if _, structural := plan.keys.key(id); !structural && plan.tags[id-1] != backendTagNumber {
			return fmt.Errorf("emit backend Go numeric proof: PC %d returns unsupported tags %x", operation.pc, plan.tags[id-1])
		}
		return nil
	case opReturn:
		resultCount, ok := backendGoNumericFixedResultCountFor(ir, options.fixedVarargCount)
		operationResultCount, operationOK := backendGoNumericReturnCount(ir, options.fixedVarargCount, operation)
		if !ok || !operationOK || operationResultCount != resultCount {
			return fmt.Errorf("emit backend Go numeric proof: PC %d has inconsistent fixed result count %d", operation.pc, operation.returnCount)
		}
		for result := 0; result < operationResultCount; result++ {
			id, valueOK := backendGoNumericReturnValue(ir, options.fixedVarargCount, operation, result)
			if !valueOK {
				return fmt.Errorf("emit backend Go numeric proof: PC %d has no return result %d", operation.pc, result)
			}
			if _, structural := plan.keys.key(id); !structural && plan.tags[id-1] != backendTagNumber {
				return fmt.Errorf("emit backend Go numeric proof: PC %d returns unsupported tags %x", operation.pc, plan.tags[id-1])
			}
		}
		return nil
	default:
		return fmt.Errorf("emit backend Go numeric proof: PC %d uses unsupported opcode %s", operation.pc, opcodeName(operation.op))
	}
}

func verifyBackendGoNumericConstant(ir *backendProtoIR, operation *backendOperationIR, index int32) error {
	if index < 0 || int(index) >= len(ir.constants) || ir.constants[index].kind != NumberKind {
		return fmt.Errorf("emit backend Go numeric proof: PC %d requires a numeric constant", operation.pc)
	}
	return nil
}

func verifyBackendGoComparableConstant(
	ir *backendProtoIR,
	operation *backendOperationIR,
	index int32,
	tags backendTagMask,
) error {
	if index < 0 || int(index) >= len(ir.constants) {
		return fmt.Errorf("emit backend Go numeric proof: PC %d has an invalid equality constant", operation.pc)
	}
	if backendTagForValueKind(ir.constants[index].kind) != tags ||
		(tags != backendTagNumber && tags != backendTagBool && tags != backendTagString) {
		return fmt.Errorf(
			"emit backend Go numeric proof: PC %d equality constant has %s kind for tags %x",
			operation.pc,
			ir.constants[index].kind,
			tags,
		)
	}
	return nil
}

func backendGoNumericType(tags backendTagMask) (string, bool) {
	switch tags {
	case backendTagNumber:
		return "float64", true
	case backendTagBool:
		return "bool", true
	case backendTagString:
		return "uint32", true
	default:
		return "", false
	}
}

func backendGoOptionalScalarTags(tags backendTagMask) (backendTagMask, bool) {
	if tags&backendTagNil == 0 {
		return 0, false
	}
	payload := tags &^ backendTagNil
	if _, ok := backendGoNumericType(payload); !ok {
		return 0, false
	}
	return payload, true
}

func backendGoScalarPayloadType(tags backendTagMask) (string, bool) {
	if goType, ok := backendGoNumericType(tags); ok {
		return goType, true
	}
	payload, ok := backendGoOptionalScalarTags(tags)
	if !ok {
		return "", false
	}
	return backendGoNumericType(payload)
}

func backendGoNumericTypeForValue(plan backendGoNumericPlan, id backendValueID) (string, bool) {
	if _, ok := plan.keys.key(id); ok {
		return "backendPreparedStringKey", true
	}
	if id == invalidBackendValueID || int(id) > len(plan.tags) {
		return "", false
	}
	return backendGoScalarPayloadType(plan.tags[id-1])
}

func backendGoNumericResultTypes(
	ir *backendProtoIR,
	plan backendGoNumericPlan,
	fixedVarargCount int,
) ([]string, error) {
	resultCount, ok := backendGoNumericFixedResultCountFor(ir, fixedVarargCount)
	if !ok {
		return nil, fmt.Errorf("emit backend Go numeric proof: function has no fixed result types")
	}
	resultTypes := make([]string, resultCount)
	resultDomains := make([]int32, resultCount)
	for pc := range ir.ops {
		operation := &ir.ops[pc]
		if operation.op != opReturnOne && operation.op != opReturn {
			continue
		}
		count, countOK := backendGoNumericReturnCount(ir, fixedVarargCount, operation)
		if !countOK || count != resultCount {
			return nil, fmt.Errorf("emit backend Go numeric proof: PC %d changes fixed result count", operation.pc)
		}
		for result := 0; result < resultCount; result++ {
			id, valueOK := backendGoNumericReturnValue(ir, fixedVarargCount, operation, result)
			if !valueOK {
				return nil, fmt.Errorf("emit backend Go numeric proof: PC %d has no result %d value", operation.pc, result)
			}
			resultType := "float64"
			domain := int32(0)
			if key, ok := plan.keys.key(id); ok {
				resultType = "backendPreparedStringKey"
				domain = key.domain
			}
			if resultTypes[result] == "" {
				resultTypes[result] = resultType
				resultDomains[result] = domain
				continue
			}
			if resultTypes[result] != resultType || resultDomains[result] != domain {
				return nil, fmt.Errorf("emit backend Go numeric proof: PC %d changes result %d type", operation.pc, result)
			}
		}
	}
	for result, resultType := range resultTypes {
		if resultType == "" {
			return nil, fmt.Errorf("emit backend Go numeric proof: result %d has no reachable type", result)
		}
	}
	return resultTypes, nil
}

func backendGoDirectTargetResultTag(
	options backendGoNumericOptions,
	operation *backendOperationIR,
	result int,
) backendTagMask {
	target, ok := backendGoNumericDirectTarget(options, operation)
	if !ok || target.ir == nil || result < 0 {
		return 0
	}
	if backendGoStructuralKeyTarget(target.ir) {
		if result == 0 {
			return backendTagString
		}
		return 0
	}
	for pc := range target.ir.ops {
		returnOperation := &target.ir.ops[pc]
		if returnOperation.op != opReturnOne && returnOperation.op != opReturn {
			continue
		}
		count, countOK := backendGoNumericReturnCount(target.ir, target.fixedVarargCount, returnOperation)
		if !countOK || result >= count {
			return 0
		}
		_, valueOK := backendGoNumericReturnValue(target.ir, target.fixedVarargCount, returnOperation, result)
		if !valueOK {
			return 0
		}
	}
	return backendTagNumber
}

func (emitter *backendGoNumericEmitter) emitBody() error {
	reachable := emitter.generatedReachableBlocks()
	for blockIndex := range emitter.ir.blocks {
		block := &emitter.ir.blocks[blockIndex]
		if !reachable[blockIndex] {
			continue
		}
		if blockIndex != 0 {
			fmt.Fprintf(&emitter.body, "b%d:\n", blockIndex)
		}
		terminated := false
		for pc := block.first; pc < block.last; pc++ {
			operation := &emitter.ir.ops[pc]
			var err error
			terminated, err = emitter.emitOperation(operation, block)
			if err != nil {
				return err
			}
			if terminated {
				break
			}
		}
		if terminated {
			continue
		}
		if len(block.successors) != 1 {
			return fmt.Errorf("emit backend Go numeric proof: block %d has no terminator and %d successors", blockIndex, len(block.successors))
		}
		emitter.emitGoto(int32(blockIndex), block.successors[0], 1)
	}
	return nil
}

func (emitter *backendGoNumericEmitter) generatedReachableBlocks() []bool {
	reachable := make([]bool, len(emitter.ir.blocks))
	if len(reachable) == 0 || !emitter.ir.blocks[0].reachable {
		return reachable
	}
	reachable[0] = true
	pending := []int32{0}
	for len(pending) > 0 {
		blockID := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		block := &emitter.ir.blocks[blockID]
		successors := block.successors
		for pc := block.first; pc < block.last; pc++ {
			operation := &emitter.ir.ops[pc]
			if backendGoNumericOperationDead(emitter.plan, operation) {
				continue
			}
			if operation.op == opJumpIfTableHasMetatable {
				nextBlock := int32(-1)
				if int(block.last) < len(emitter.ir.ops) {
					nextBlock = emitter.ir.pcToBlock[block.last]
				}
				successors = []int32{nextBlock}
				break
			}
		}
		for _, successor := range successors {
			if successor < 0 || int(successor) >= len(reachable) ||
				!emitter.ir.blocks[successor].reachable ||
				reachable[successor] {
				continue
			}
			reachable[successor] = true
			pending = append(pending, successor)
		}
	}
	return reachable
}

func (emitter *backendGoNumericEmitter) emitOperation(operation *backendOperationIR, block *backendBlockIR) (bool, error) {
	if backendGoNumericOperationDead(emitter.plan, operation) {
		return false, nil
	}
	definition := func(register int32) (backendValueID, error) {
		for _, candidate := range operation.defs {
			if candidate.register == register {
				return candidate.value, nil
			}
		}
		return invalidBackendValueID, fmt.Errorf("emit backend Go numeric proof: PC %d has no definition for register %d", operation.pc, register)
	}
	use := func(register int32) (backendValueID, error) {
		id := backendOperationUse(operation, register)
		if id == invalidBackendValueID {
			return id, fmt.Errorf("emit backend Go numeric proof: PC %d has no use for register %d", operation.pc, register)
		}
		return id, nil
	}
	switch operation.op {
	case opLoadConst:
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		if emitter.plan.coroutines.dead(destination) {
			fmt.Fprintf(&emitter.body, "\tv%d = false\n", destination)
			return false, nil
		}
		constant := emitter.ir.constants[operation.b]
		switch constant.kind {
		case NumberKind:
			emitter.needsMath = true
			fmt.Fprintf(&emitter.body, "\tv%d = math.Float64frombits(0x%016x)\n", destination, constant.bits)
		case BoolKind:
			fmt.Fprintf(&emitter.body, "\tv%d = %t\n", destination, constant.bits != 0)
		case StringKind:
			fmt.Fprintf(&emitter.body, "\tv%d = uint32(%d)\n", destination, constant.bits)
		}
	case opMove:
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		source, err := use(operation.b)
		if err != nil {
			return false, err
		}
		emitter.emitValueCopy(destination, source, 1)
	case opNewTable:
		return false, nil
	case opSetStringField:
		if emitter.plan.callSets.setter(operation) || emitter.plan.closureSets.setter(operation) {
			return false, nil
		}
		if handled, err := emitter.emitRecordSetStringField(operation, use); handled {
			return false, err
		}
		fieldIndex, field, ok := emitter.plan.tables.operationField(emitter.ir, operation)
		if !ok {
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar field", operation.pc)
		}
		if field.methodProto >= 0 {
			return false, nil
		}
		if field.child != invalidBackendValueID {
			return false, nil
		}
		source, err := use(operation.c)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(&emitter.body, "\t%s = v%d\n", emitter.scalarField(fieldIndex), source)
	case opSetStringFieldIndex:
		if handled, err := emitter.emitRecordFusedSet(operation, use); handled {
			return false, err
		}
		return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no fused child-record mutation", operation.pc)
	case opSetField:
		if emitter.plan.closureSets.setter(operation) {
			return false, nil
		}
		if handled, err := emitter.emitRecordSetStringField(operation, use); handled {
			return false, err
		}
		if handled, err := emitter.emitRecordArraySet(operation); handled {
			return false, err
		}
		arrayIndex, _, element, ok := emitter.plan.tables.arrayOperation(emitter.ir, operation)
		if !ok {
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar array element", operation.pc)
		}
		source, err := use(operation.c)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(&emitter.body, "\ta%d[%d] = v%d\n", arrayIndex, element-1, source)
	case opSetIndex:
		if handled, err := emitter.emitRecordArraySet(operation); handled {
			return false, err
		}
		if handled, err := emitter.emitRecordDynamicSet(operation, use); handled {
			return false, err
		}
		if handled, err := emitter.emitRecordMapSet(operation); handled {
			return false, err
		}
		return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar record-map store", operation.pc)
	case opGetIndex:
		if set, ok := emitter.plan.closureSets.selector(operation); ok {
			return false, emitter.emitFiniteClosureSelector(operation, set, definition)
		}
		if emitter.plan.callSets.selector(operation) {
			return false, nil
		}
		if handled, err := emitter.emitRecordGetStringField(operation, definition); handled {
			return false, err
		}
		if arrayIndex, array, _, ok := emitter.plan.tables.arrayOperation(emitter.ir, operation); ok {
			destination, err := definition(operation.a)
			if err != nil {
				return false, err
			}
			key, err := use(operation.c)
			if err != nil {
				return false, err
			}
			emitter.emitOptionalPresenceGuard(operation, 1, key)
			emitter.needsMath = true
			fmt.Fprintf(
				&emitter.body,
				"\tif v%d < 1 || v%d > %d || v%d != math.Trunc(v%d) {\n",
				key,
				key,
				array.length,
				key,
				key,
			)
			fmt.Fprintf(&emitter.body, "\t\t%s\n", emitter.failureReturn())
			emitter.body.WriteString("\t}\n")
			fmt.Fprintf(&emitter.body, "\tv%d = a%d[int(v%d)-1]\n", destination, arrayIndex, key)
			return false, nil
		}
		if handled, err := emitter.emitRecordDynamicChildSelector(operation, definition); handled {
			return false, err
		}
		if handled, err := emitter.emitRecordDynamicChildGet(operation, definition); handled {
			return false, err
		}
		if handled, err := emitter.emitRecordDynamicGet(operation, definition); handled {
			return false, err
		}
		if handled, err := emitter.emitRecordMapGet(operation, definition); handled {
			return false, err
		}
		if handled, err := emitter.emitRecordArrayGet(operation, definition); handled {
			return false, err
		}
		return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar record lookup", operation.pc)
	case opGetStringFieldIndex:
		if handled, err := emitter.emitRecordFusedArrayGet(operation, definition, use); handled {
			return false, err
		}
		if handled, err := emitter.emitRecordFusedGet(operation, definition); handled {
			return false, err
		}
		return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no fused child-record lookup", operation.pc)
	case opGetStringField:
		if handled, err := emitter.emitRecordGetStringField(operation, definition); handled {
			return false, err
		}
		fieldIndex, field, ok := emitter.plan.tables.operationField(emitter.ir, operation)
		if !ok {
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar field", operation.pc)
		}
		if field.child != invalidBackendValueID {
			return false, nil
		}
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(&emitter.body, "\tv%d = %s\n", destination, emitter.scalarField(fieldIndex))
	case opPrepareIter:
		if set, ok := emitter.plan.closureSets.prepare(operation); ok {
			return false, emitter.emitFiniteClosurePrepare(operation, set, use)
		}
		if handled, err := emitter.emitRecordArrayPrepare(operation, use); handled {
			return false, err
		}
		arrayIndex, _, _, ok := emitter.plan.tables.arrayOperation(emitter.ir, operation)
		if !ok {
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar array iterator", operation.pc)
		}
		fmt.Fprintf(&emitter.body, "\ti%d = 0\n", arrayIndex)
	case opArrayNextJump2:
		if set, ok := emitter.plan.closureSets.next(operation); ok {
			return true, emitter.emitFiniteClosureNext(operation, block, set, definition, use)
		}
		if handled, terminated, err := emitter.emitRecordArrayNext(operation, block, definition); handled {
			return terminated, err
		}
		arrayIndex, _, _, ok := emitter.plan.tables.arrayOperation(emitter.ir, operation)
		if !ok {
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar array iterator", operation.pc)
		}
		value, err := definition(operation.a + 1)
		if err != nil {
			return false, err
		}
		target := emitter.ir.pcToBlock[operation.targetPC]
		fmt.Fprintf(&emitter.body, "\tif i%d >= len(a%d) {\n", arrayIndex, arrayIndex)
		emitter.emitGoto(int32(block.id), target, 2)
		emitter.body.WriteString("\t}\n")
		fmt.Fprintf(&emitter.body, "\tv%d = a%d[i%d]\n", value, arrayIndex, arrayIndex)
		fmt.Fprintf(&emitter.body, "\ti%d++\n", arrayIndex)
		nextBlock := int32(-1)
		if int(block.last) < len(emitter.ir.ops) {
			nextBlock = emitter.ir.pcToBlock[block.last]
		}
		emitter.emitGoto(int32(block.id), nextBlock, 1)
		return true, nil
	case opFastCall:
		if handled, err := emitter.emitRecordRootArrayIntrinsic(operation, definition, use); handled {
			return false, err
		}
		if handled, err := emitter.emitRecordFamilyIntrinsic(operation, definition, use); handled {
			return false, err
		}
		if _, ok := emitter.plan.keys.tostring(operation); ok {
			return false, nil
		}
		if backendGoNumericMathMin(operation) {
			destination, err := definition(operation.a)
			if err != nil {
				return false, err
			}
			first, err := use(operation.a)
			if err != nil {
				return false, err
			}
			emitter.emitOptionalPresenceGuard(operation, 1, first)
			fmt.Fprintf(&emitter.body, "\tv%d = v%d\n", destination, first)
			emitter.needsMath = true
			for argument := int32(1); argument < operation.c; argument++ {
				value, err := use(operation.a + argument)
				if err != nil {
					return false, err
				}
				emitter.emitOptionalPresenceGuard(operation, 1, value)
				fmt.Fprintf(&emitter.body, "\tv%d = math.Min(v%d, v%d)\n", destination, destination, value)
			}
			return false, nil
		}
		if emitter.plan.coroutines.createOperation(operation) {
			return false, nil
		}
		if resume, ok := emitter.plan.coroutines.resume(operation); ok {
			success, err := definition(operation.a)
			if err != nil {
				return false, err
			}
			value, err := definition(operation.a + 1)
			if err != nil {
				return false, err
			}
			argument := "0"
			if resume.first {
				id, err := use(operation.a + 1)
				if err != nil {
					return false, err
				}
				argument = fmt.Sprintf("v%d", id)
			}
			fmt.Fprintf(
				&emitter.body,
				"\tv%d, _, ok%d = %s(&q0, %s, %t)\n",
				value,
				operation.pc,
				emitter.plan.coroutines.target.functionName,
				argument,
				resume.first,
			)
			fmt.Fprintf(&emitter.body, "\tif !ok%d {\n", operation.pc)
			emitter.emitReplayEntry(2)
			emitter.body.WriteString("\t}\n")
			if emitter.plan.used[success-1] {
				fmt.Fprintf(&emitter.body, "\tv%d = true\n", success)
			}
			return false, nil
		}
		if emitter.plan.coroutines.statusOperation(operation) {
			destination, err := definition(operation.a)
			if err != nil {
				return false, err
			}
			fmt.Fprintf(
				&emitter.body,
				"\tv%d = q0.state != %d\n",
				destination,
				len(emitter.plan.coroutines.yields)+1,
			)
			return false, nil
		}
		if backendGoNumericFixedVarargSelect(emitter.ir, emitter.options.fixedVarargCount, operation) {
			destination, err := definition(operation.a)
			if err != nil {
				return false, err
			}
			fmt.Fprintf(&emitter.body, "\tv%d = %d\n", destination, emitter.options.fixedVarargCount)
			return false, nil
		}
		if _, ok := emitter.plan.tables.metatableOperation(operation); ok {
			return false, nil
		}
		arrayIndex, array, _, ok := emitter.plan.tables.arrayOperation(emitter.ir, operation)
		if !ok {
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar table intrinsic", operation.pc)
		}
		switch nativeFuncID(operation.nativeID) {
		case nativeFuncTableInsert:
			source, err := use(operation.a + 1)
			if err != nil {
				return false, err
			}
			fmt.Fprintf(&emitter.body, "\tif n%d >= len(a%d) {\n", arrayIndex, arrayIndex)
			emitter.emitReplayEntry(2)
			emitter.body.WriteString("\t}\n")
			fmt.Fprintf(&emitter.body, "\tt%d = h%d + n%d\n", arrayIndex, arrayIndex, arrayIndex)
			fmt.Fprintf(&emitter.body, "\tif t%d >= len(a%d) {\n", arrayIndex, arrayIndex)
			fmt.Fprintf(&emitter.body, "\t\tt%d -= len(a%d)\n", arrayIndex, arrayIndex)
			emitter.body.WriteString("\t}\n")
			fmt.Fprintf(&emitter.body, "\tif uint(t%d) >= uint(len(a%d)) {\n", arrayIndex, arrayIndex)
			emitter.emitReplayEntry(2)
			emitter.body.WriteString("\t}\n")
			fmt.Fprintf(&emitter.body, "\ta%d[t%d] = v%d\n", arrayIndex, arrayIndex, source)
			fmt.Fprintf(&emitter.body, "\tn%d++\n", arrayIndex)
		case nativeFuncTableRemove:
			destination, err := definition(operation.a)
			if err != nil {
				return false, err
			}
			position, err := use(operation.a + 1)
			if err != nil {
				return false, err
			}
			fmt.Fprintf(&emitter.body, "\t_ = v%d\n", position)
			fmt.Fprintf(&emitter.body, "\tif n%d == 0 {\n", arrayIndex)
			emitter.emitReplayEntry(2)
			emitter.body.WriteString("\t}\n")
			fmt.Fprintf(&emitter.body, "\tif uint(h%d) >= uint(len(a%d)) {\n", arrayIndex, arrayIndex)
			emitter.emitReplayEntry(2)
			emitter.body.WriteString("\t}\n")
			fmt.Fprintf(&emitter.body, "\tv%d = a%d[h%d]\n", destination, arrayIndex, arrayIndex)
			fmt.Fprintf(&emitter.body, "\th%d++\n", arrayIndex)
			fmt.Fprintf(&emitter.body, "\tif h%d == len(a%d) {\n", arrayIndex, arrayIndex)
			fmt.Fprintf(&emitter.body, "\t\th%d = 0\n", arrayIndex)
			emitter.body.WriteString("\t}\n")
			fmt.Fprintf(&emitter.body, "\tn%d--\n", arrayIndex)
		case nativeFuncRawLen:
			destination, err := definition(operation.a)
			if err != nil {
				return false, err
			}
			if array.mutable {
				fmt.Fprintf(&emitter.body, "\tv%d = float64(n%d)\n", destination, arrayIndex)
			} else {
				fmt.Fprintf(&emitter.body, "\tv%d = %d\n", destination, array.length)
			}
		default:
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported scalar array intrinsic", operation.pc)
		}
	case opConcatChain:
		key, ok := emitter.plan.keys.concat(operation)
		if !ok {
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no structural concat key", operation.pc)
		}
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		emitter.needsMath = true
		fmt.Fprintf(
			&emitter.body,
			"\tif math.IsNaN(v%d) || math.IsInf(v%d, 0) || v%d != math.Trunc(v%d) || v%d < -2147483648 || v%d > 2147483647 || v%d == 0 && math.Signbit(v%d) {\n",
			key.first,
			key.first,
			key.first,
			key.first,
			key.first,
			key.first,
			key.first,
			key.first,
		)
		fmt.Fprintf(&emitter.body, "\t\t%s\n", emitter.failureReturn())
		emitter.body.WriteString("\t}\n")
		fmt.Fprintf(
			&emitter.body,
			"\tif math.IsNaN(v%d) || math.IsInf(v%d, 0) || v%d != math.Trunc(v%d) || v%d < -2147483648 || v%d > 2147483647 || v%d == 0 && math.Signbit(v%d) {\n",
			key.second,
			key.second,
			key.second,
			key.second,
			key.second,
			key.second,
			key.second,
			key.second,
		)
		fmt.Fprintf(&emitter.body, "\t\t%s\n", emitter.failureReturn())
		emitter.body.WriteString("\t}\n")
		fmt.Fprintf(
			&emitter.body,
			"\tv%d = backendPreparedStringKey{first: int32(v%d), second: int32(v%d)}\n",
			destination,
			key.first,
			key.second,
		)
	case opVararg:
		for _, result := range operation.defs {
			parameter, ok := backendGoNumericVarargIndex(
				emitter.ir,
				emitter.options.fixedVarargCount,
				operation,
				result.register,
			)
			if !ok {
				return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no fixed vararg parameter", operation.pc)
			}
			fmt.Fprintf(&emitter.body, "\tv%d = p%d\n", result.value, parameter)
		}
	case opAdd, opSub, opMul, opDiv, opMod, opIDiv, opPow:
		destination, _ := definition(operation.a)
		left, _ := use(operation.b)
		right, _ := use(operation.c)
		emitter.emitOptionalPresenceGuard(operation, 1, left, right)
		emitter.emitNumericBinary(destination, left, fmt.Sprintf("v%d", right), operation.op)
	case opAddK, opSubK, opMulK, opDivK, opModK, opIDivK:
		destination, _ := definition(operation.a)
		left, _ := use(operation.b)
		right := emitter.numericConstant(operation.c)
		emitter.emitOptionalPresenceGuard(operation, 1, left)
		emitter.emitNumericBinary(destination, left, right, operation.op)
	case opNeg:
		destination, _ := definition(operation.a)
		source, _ := use(operation.b)
		emitter.emitOptionalPresenceGuard(operation, 1, source)
		fmt.Fprintf(&emitter.body, "\tv%d = -v%d\n", destination, source)
	case opGetUpvalue:
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		if emitter.plan.tables.root(destination) == invalidBackendValueID {
			fmt.Fprintf(&emitter.body, "\tv%d = *u%d\n", destination, operation.b)
		}
	case opSetUpvalue:
		source, err := use(operation.b)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(&emitter.body, "\t*u%d = v%d\n", operation.a, source)
	case opEqual, opNotEqual, opLess, opLessEqual, opGreater, opGreaterEqual:
		destination, _ := definition(operation.a)
		left, _ := use(operation.b)
		right, _ := use(operation.c)
		if operation.op == opEqual || operation.op == opNotEqual {
			_, leftRef := emitter.plan.records.ref(left)
			_, rightRef := emitter.plan.records.ref(right)
			if leftRef && emitter.plan.tags[right-1] == backendTagNil {
				fmt.Fprintf(&emitter.body, "\tv%d = v%d %s 0\n", destination, left, backendGoComparisonOperator(operation.op))
				return false, nil
			}
			if rightRef && emitter.plan.tags[left-1] == backendTagNil {
				fmt.Fprintf(&emitter.body, "\tv%d = v%d %s 0\n", destination, right, backendGoComparisonOperator(operation.op))
				return false, nil
			}
			if expression, ok := emitter.optionalEqualityExpression(left, right); ok {
				if operation.op == opNotEqual {
					expression = "!(" + expression + ")"
				}
				fmt.Fprintf(&emitter.body, "\tv%d = %s\n", destination, expression)
				return false, nil
			}
		}
		if operation.op != opEqual && operation.op != opNotEqual {
			emitter.emitOptionalPresenceGuard(operation, 1, left, right)
			emitter.emitNaNGuard(operation, 1, left, right)
		}
		fmt.Fprintf(&emitter.body, "\tv%d = v%d %s v%d\n", destination, left, backendGoComparisonOperator(operation.op), right)
	case opNumericForCheck:
		loop, _ := use(operation.a)
		limit, _ := use(operation.b)
		step, _ := use(operation.c)
		emitter.emitOptionalPresenceGuard(operation, 1, loop, limit, step)
		emitter.emitNaNGuard(operation, 1, loop, limit, step)
		condition := fmt.Sprintf("(v%d > 0 && v%d > v%d) || (v%d <= 0 && v%d < v%d)", step, loop, limit, step, loop, limit)
		emitter.emitBranch(int32(block.id), operation.targetPC, condition, 1)
		return true, nil
	case opNumericForLoop:
		destination, _ := definition(operation.a)
		loop, _ := use(operation.a)
		step, _ := use(operation.b)
		emitter.emitOptionalPresenceGuard(operation, 1, loop, step)
		fmt.Fprintf(&emitter.body, "\tv%d = v%d + v%d\n", destination, loop, step)
		emitter.emitGoto(int32(block.id), emitter.ir.pcToBlock[operation.targetPC], 1)
		return true, nil
	case opJumpIfNotEqualK:
		left, _ := use(operation.a)
		expression := fmt.Sprintf("v%d != %s", left, emitter.comparableConstant(operation.b))
		if _, optional := backendGoOptionalScalarTags(emitter.plan.tags[left-1]); optional {
			expression = fmt.Sprintf("!vp%d || %s", left, expression)
		}
		emitter.emitBranch(
			int32(block.id),
			operation.targetPC,
			expression,
			1,
		)
		return true, nil
	case opJumpIfNotLessK, opJumpIfNotGreaterK, opJumpIfLessK, opJumpIfGreaterK:
		left, _ := use(operation.a)
		emitter.emitOptionalPresenceGuard(operation, 1, left)
		emitter.emitNaNGuard(operation, 1, left)
		operator := backendGoComparisonOperator(operation.op)
		condition := fmt.Sprintf("v%d %s %s", left, operator, emitter.numericConstant(operation.b))
		if operation.op == opJumpIfNotLessK || operation.op == opJumpIfNotGreaterK {
			condition = "!(" + condition + ")"
		}
		emitter.emitBranch(int32(block.id), operation.targetPC, condition, 1)
		return true, nil
	case opJumpIfNotLess, opJumpIfNotGreater, opJumpIfLess, opJumpIfGreater:
		left, _ := use(operation.a)
		right, _ := use(operation.b)
		emitter.emitOptionalPresenceGuard(operation, 1, left, right)
		emitter.emitNaNGuard(operation, 1, left, right)
		condition := fmt.Sprintf("v%d %s v%d", left, backendGoComparisonOperator(operation.op), right)
		if operation.op == opJumpIfNotLess || operation.op == opJumpIfNotGreater {
			condition = "!(" + condition + ")"
		}
		emitter.emitBranch(int32(block.id), operation.targetPC, condition, 1)
		return true, nil
	case opJumpIfFalse:
		condition, _ := use(operation.a)
		expression := "false"
		if _, ok := emitter.plan.records.ref(condition); ok {
			expression = fmt.Sprintf("v%d == 0", condition)
		} else if emitter.plan.tags[condition-1] == backendTagBool {
			expression = fmt.Sprintf("!v%d", condition)
		} else if payload, optional := backendGoOptionalScalarTags(emitter.plan.tags[condition-1]); optional {
			if payload == backendTagBool {
				expression = fmt.Sprintf("!vp%d || !v%d", condition, condition)
			} else {
				expression = fmt.Sprintf("!vp%d", condition)
			}
		}
		emitter.emitBranch(int32(block.id), operation.targetPC, expression, 1)
		return true, nil
	case opJumpIfTableHasMetatable:
		nextBlock := int32(-1)
		if int(block.last) < len(emitter.ir.ops) {
			nextBlock = emitter.ir.pcToBlock[block.last]
		}
		emitter.emitGoto(int32(block.id), nextBlock, 1)
		return true, nil
	case opJump:
		emitter.emitGoto(int32(block.id), emitter.ir.pcToBlock[operation.targetPC], 1)
		return true, nil
	case opCallUpvalueOne:
		if !emitter.options.selfRecursive || operation.b != 0 {
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no recursive self target", operation.pc)
		}
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(&emitter.body, "\tv%d = %sBody(", destination, emitter.options.functionName)
		for argument := int32(0); argument < operation.callArgCount; argument++ {
			if argument != 0 {
				emitter.body.WriteString(", ")
			}
			value, err := use(operation.callArgStart + argument)
			if err != nil {
				return false, err
			}
			fmt.Fprintf(&emitter.body, "v%d", value)
		}
		emitter.body.WriteString(")\n")
	case opCall:
		target, ok := backendGoNumericDirectTarget(emitter.options, operation)
		if !ok || !backendGoNumericScalarReplacedCall(emitter.ir, emitter.options, operation) {
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar-replaced fixed-result target", operation.pc)
		}
		captured, hasCaptured := emitter.plan.captured.call(operation)
		if hasCaptured {
			for field, callerField := range captured.callerFields {
				if callerField.storage != backendGoRecordFieldScratch {
					return false, fmt.Errorf("emit backend Go numeric proof: PC %d has unsupported captured field storage", operation.pc)
				}
				fmt.Fprintf(
					&emitter.body,
					"\tm%d_%d = r%d_%d\n",
					operation.pc,
					field,
					callerField.index,
					callerField.field,
				)
			}
		}
		for result := int32(0); result < operation.callResults; result++ {
			if result != 0 {
				emitter.body.WriteString(", ")
			}
			destination, err := definition(operation.a + result)
			if err != nil {
				return false, err
			}
			fmt.Fprintf(&emitter.body, "\tv%d", destination)
		}
		fmt.Fprintf(&emitter.body, ", ok%d = %s(", operation.pc, target.functionName)
		for field := range captured.callerFields {
			if field != 0 {
				emitter.body.WriteString(", ")
			}
			fmt.Fprintf(&emitter.body, "&m%d_%d", operation.pc, field)
		}
		for argument := int32(0); argument < operation.callArgCount; argument++ {
			if argument != 0 || len(captured.callerFields) != 0 {
				emitter.body.WriteString(", ")
			}
			value, err := use(operation.callArgStart + argument)
			if err != nil {
				return false, err
			}
			fmt.Fprintf(&emitter.body, "v%d", value)
		}
		emitter.body.WriteString(")\n")
		fmt.Fprintf(&emitter.body, "\tif !ok%d {\n", operation.pc)
		emitter.emitReplayEntry(2)
		emitter.body.WriteString("\t}\n")
		for field, callerField := range captured.callerFields {
			fmt.Fprintf(
				&emitter.body,
				"\tr%d_%d = m%d_%d\n",
				callerField.index,
				callerField.field,
				operation.pc,
				field,
			)
		}
		for _, borrowed := range operation.defs {
			if !emitter.plan.used[borrowed.value-1] {
				continue
			}
			source, sourceOK := backendGoBorrowedCallSource(emitter.ir, operation, borrowed.register)
			if !sourceOK {
				continue
			}
			emitter.emitValueCopy(borrowed.value, source, 1)
			if emitter.plan.tags[borrowed.value-1] != backendTagNil {
				fmt.Fprintf(&emitter.body, "\t_ = v%d\n", borrowed.value)
			}
		}
	case opCallOne:
		if call, ok := emitter.plan.callSets.call(operation); ok {
			destination, err := definition(operation.a)
			if err != nil {
				return false, err
			}
			fmt.Fprintf(&emitter.body, "\tswitch v%d {\n", call.key)
			for variantIndex, variant := range call.variants {
				fmt.Fprintf(&emitter.body, "\tcase uint32(%d):\n", variant.key)
				for field, callerField := range variant.callerFields {
					fmt.Fprintf(
						&emitter.body,
						"\t\tm%d_%d_%d := r%d_%d\n",
						operation.pc,
						variantIndex,
						field,
						callerField.index,
						callerField.field,
					)
				}
				fmt.Fprintf(&emitter.body, "\t\tv%d, ok%d = %s(", destination, operation.pc, variant.target.functionName)
				for field := range variant.callerFields {
					if field != 0 {
						emitter.body.WriteString(", ")
					}
					fmt.Fprintf(&emitter.body, "&m%d_%d_%d", operation.pc, variantIndex, field)
				}
				for argument := int32(1); argument < operation.callArgCount; argument++ {
					emitter.body.WriteString(", ")
					value, useErr := use(operation.callArgStart + argument)
					if useErr != nil {
						return false, useErr
					}
					fmt.Fprintf(&emitter.body, "v%d", value)
				}
				emitter.body.WriteString(")\n")
				fmt.Fprintf(&emitter.body, "\t\tif !ok%d {\n", operation.pc)
				emitter.emitReplayEntry(3)
				emitter.body.WriteString("\t\t}\n")
				for field, callerField := range variant.callerFields {
					fmt.Fprintf(
						&emitter.body,
						"\t\tr%d_%d = m%d_%d_%d\n",
						callerField.index,
						callerField.field,
						operation.pc,
						variantIndex,
						field,
					)
				}
			}
			emitter.body.WriteString("\tdefault:\n")
			emitter.emitReplayEntry(2)
			emitter.body.WriteString("\t}\n")
			return false, nil
		}
		return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no finite call-set target", operation.pc)
	case opCallLocalOne:
		if set, ok := emitter.plan.closureSets.set(operation); ok {
			return false, emitter.emitFiniteClosureCall(operation, set, definition, use)
		}
		if factory, cell, ok := emitter.plan.closures.factory(operation); ok {
			for captureIndex, capture := range factory.captures {
				if capture.argument >= 0 {
					source, err := use(operation.callArgStart + capture.argument)
					if err != nil {
						return false, err
					}
					fmt.Fprintf(&emitter.body, "\tc%d = v%d\n", cell+captureIndex, source)
					continue
				}
				fmt.Fprintf(
					&emitter.body,
					"\tc%d = %s\n",
					cell+captureIndex,
					strconv.FormatFloat(capture.constant, 'g', -1, 64),
				)
			}
			return false, nil
		}
		if call, ok := emitter.plan.closures.call(operation); ok {
			destination, err := definition(operation.a)
			if err != nil {
				return false, err
			}
			for cell := 0; cell < call.cellCount; cell++ {
				fmt.Fprintf(&emitter.body, "\ts%d = c%d\n", call.cellStart+cell, call.cellStart+cell)
			}
			fmt.Fprintf(&emitter.body, "\tv%d, ok%d = %s(", destination, operation.pc, call.target.functionName)
			for cell := 0; cell < call.cellCount; cell++ {
				if cell != 0 {
					emitter.body.WriteString(", ")
				}
				fmt.Fprintf(&emitter.body, "&s%d", call.cellStart+cell)
			}
			for argument := int32(0); argument < operation.callArgCount; argument++ {
				value, err := use(operation.callArgStart + argument)
				if err != nil {
					return false, err
				}
				fmt.Fprintf(&emitter.body, ", v%d", value)
			}
			emitter.body.WriteString(")\n")
			fmt.Fprintf(&emitter.body, "\tif !ok%d {\n", operation.pc)
			emitter.emitReplayEntry(2)
			emitter.body.WriteString("\t}\n")
			for cell := 0; cell < call.cellCount; cell++ {
				fmt.Fprintf(&emitter.body, "\tc%d = s%d\n", call.cellStart+cell, call.cellStart+cell)
			}
			return false, nil
		}
		target, ok := backendGoNumericDirectTarget(emitter.options, operation)
		if !ok || !backendGoNumericScalarReplacedCall(emitter.ir, emitter.options, operation) {
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no scalar-replaced direct target", operation.pc)
		}
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(&emitter.body, "\tv%d, ok%d = %s(", destination, operation.pc, target.functionName)
		for argument := int32(0); argument < operation.callArgCount; argument++ {
			if argument != 0 {
				emitter.body.WriteString(", ")
			}
			value, err := use(operation.callArgStart + argument)
			if err != nil {
				return false, err
			}
			fmt.Fprintf(&emitter.body, "v%d", value)
		}
		emitter.body.WriteString(")\n")
		fmt.Fprintf(&emitter.body, "\tif !ok%d {\n", operation.pc)
		emitter.emitReplayEntry(2)
		emitter.body.WriteString("\t}\n")
	case opCallMethodOne:
		call, ok := emitter.plan.methods.call(operation)
		if !ok {
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no static scalar method target", operation.pc)
		}
		destination, err := definition(operation.a)
		if err != nil {
			return false, err
		}
		for field, callerField := range call.callerFields {
			fmt.Fprintf(
				&emitter.body,
				"\tm%d_%d = %s\n",
				operation.pc,
				field,
				emitter.scalarField(callerField),
			)
		}
		fmt.Fprintf(&emitter.body, "\tv%d, ok%d = %s(", destination, operation.pc, call.target.functionName)
		for field := range call.callerFields {
			if field != 0 {
				emitter.body.WriteString(", ")
			}
			fmt.Fprintf(&emitter.body, "&m%d_%d", operation.pc, field)
		}
		for argument := int32(1); argument < operation.callArgCount; argument++ {
			if len(call.callerFields) != 0 || argument != 1 {
				emitter.body.WriteString(", ")
			}
			value, err := use(operation.callArgStart + argument)
			if err != nil {
				return false, err
			}
			fmt.Fprintf(&emitter.body, "v%d", value)
		}
		emitter.body.WriteString(")\n")
		fmt.Fprintf(&emitter.body, "\tif !ok%d {\n", operation.pc)
		emitter.emitReplayEntry(2)
		emitter.body.WriteString("\t}\n")
		for field, callerField := range call.callerFields {
			fmt.Fprintf(
				&emitter.body,
				"\t%s = m%d_%d\n",
				emitter.scalarField(callerField),
				operation.pc,
				field,
			)
		}
	case opReturnOne, opReturn:
		resultCount, ok := backendGoNumericReturnCount(
			emitter.ir,
			emitter.options.fixedVarargCount,
			operation,
		)
		if !ok {
			return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no bounded result shape", operation.pc)
		}
		results := make([]backendValueID, resultCount)
		for result := 0; result < resultCount; result++ {
			value, valueOK := backendGoNumericReturnValue(
				emitter.ir,
				emitter.options.fixedVarargCount,
				operation,
				result,
			)
			if !valueOK {
				return false, fmt.Errorf("emit backend Go numeric proof: PC %d has no result %d value", operation.pc, result)
			}
			results[result] = value
		}
		if emitter.options.selfRecursive {
			fmt.Fprintf(&emitter.body, "\treturn v%d\n", results[0])
		} else if emitter.prepared {
			fmt.Fprintf(&emitter.body, "\treturn machinePreparedReturnOneNumber(v%d)\n", results[0])
		} else {
			emitter.body.WriteString("\treturn ")
			for result, value := range results {
				if result != 0 {
					emitter.body.WriteString(", ")
				}
				fmt.Fprintf(&emitter.body, "v%d", value)
			}
			emitter.body.WriteString(", true\n")
		}
		return true, nil
	default:
		return false, fmt.Errorf("emit backend Go numeric proof: PC %d uses unsupported opcode %s", operation.pc, opcodeName(operation.op))
	}
	return false, nil
}

func (emitter *backendGoNumericEmitter) scalarField(fieldIndex int) string {
	if fieldIndex >= 0 &&
		fieldIndex < len(emitter.plan.tables.fields) &&
		emitter.plan.tables.isExternalRoot(emitter.plan.tables.fields[fieldIndex].key.table) {
		return fmt.Sprintf("*r%d", fieldIndex)
	}
	return fmt.Sprintf("f%d", fieldIndex)
}

func (emitter *backendGoNumericEmitter) emitNumericBinary(destination, left backendValueID, right string, op opcode) {
	switch op {
	case opAdd, opAddK:
		fmt.Fprintf(&emitter.body, "\tv%d = v%d + %s\n", destination, left, right)
	case opSub, opSubK:
		fmt.Fprintf(&emitter.body, "\tv%d = v%d - %s\n", destination, left, right)
	case opMul, opMulK:
		fmt.Fprintf(&emitter.body, "\tv%d = v%d * %s\n", destination, left, right)
	case opDiv, opDivK:
		fmt.Fprintf(&emitter.body, "\tv%d = v%d / %s\n", destination, left, right)
	case opMod, opModK:
		emitter.needsMath = true
		fmt.Fprintf(&emitter.body, "\tv%d = v%d - math.Floor(v%d/%s)*%s\n", destination, left, left, right, right)
	case opIDiv, opIDivK:
		emitter.needsMath = true
		fmt.Fprintf(&emitter.body, "\tv%d = math.Floor(v%d / %s)\n", destination, left, right)
	case opPow:
		emitter.needsMath = true
		fmt.Fprintf(&emitter.body, "\tv%d = math.Pow(v%d, %s)\n", destination, left, right)
	}
}

func (emitter *backendGoNumericEmitter) numericConstant(index int32) string {
	emitter.needsMath = true
	return fmt.Sprintf("math.Float64frombits(0x%016x)", emitter.ir.constants[index].bits)
}

func (emitter *backendGoNumericEmitter) comparableConstant(index int32) string {
	constant := emitter.ir.constants[index]
	switch constant.kind {
	case NumberKind:
		return emitter.numericConstant(index)
	case BoolKind:
		return fmt.Sprintf("%t", constant.bits != 0)
	case StringKind:
		return fmt.Sprintf("uint32(%d)", constant.bits)
	default:
		return "0"
	}
}

func (emitter *backendGoNumericEmitter) emitNaNGuard(operation *backendOperationIR, indent int, values ...backendValueID) {
	emitter.needsMath = true
	if emitter.options.selfRecursive {
		return
	}
	prefix := strings.Repeat("\t", indent)
	for _, value := range values {
		fmt.Fprintf(&emitter.body, "%sif math.IsNaN(v%d) {\n", prefix, value)
		if emitter.prepared {
			emitter.emitReplayBeforeOperation(operation, indent+1)
		} else {
			fmt.Fprintf(&emitter.body, "%s\t%s\n", prefix, emitter.failureReturn())
		}
		fmt.Fprintf(&emitter.body, "%s}\n", prefix)
	}
}

func (emitter *backendGoNumericEmitter) emitOptionalPresenceGuard(
	operation *backendOperationIR,
	indent int,
	values ...backendValueID,
) {
	prefix := strings.Repeat("\t", indent)
	for _, value := range values {
		if _, optional := backendGoOptionalScalarTags(emitter.plan.tags[value-1]); !optional {
			continue
		}
		fmt.Fprintf(&emitter.body, "%sif !vp%d {\n", prefix, value)
		if emitter.prepared {
			emitter.emitReplayBeforeOperation(operation, indent+1)
		} else {
			fmt.Fprintf(&emitter.body, "%s\t%s\n", prefix, emitter.failureReturn())
		}
		fmt.Fprintf(&emitter.body, "%s}\n", prefix)
	}
}

func (emitter *backendGoNumericEmitter) optionalEqualityExpression(
	left backendValueID,
	right backendValueID,
) (string, bool) {
	leftTags := emitter.plan.tags[left-1]
	rightTags := emitter.plan.tags[right-1]
	leftPayload, leftOptional := backendGoOptionalScalarTags(leftTags)
	rightPayload, rightOptional := backendGoOptionalScalarTags(rightTags)
	switch {
	case leftOptional && rightTags == backendTagNil:
		return fmt.Sprintf("!vp%d", left), true
	case rightOptional && leftTags == backendTagNil:
		return fmt.Sprintf("!vp%d", right), true
	case leftOptional && rightOptional && leftPayload == rightPayload:
		return fmt.Sprintf("vp%d == vp%d && (!vp%d || v%d == v%d)", left, right, left, left, right), true
	case leftOptional && rightTags == leftPayload:
		return fmt.Sprintf("vp%d && v%d == v%d", left, left, right), true
	case rightOptional && leftTags == rightPayload:
		return fmt.Sprintf("vp%d && v%d == v%d", right, left, right), true
	default:
		return "", false
	}
}

func (emitter *backendGoNumericEmitter) emitReplayBeforeOperation(operation *backendOperationIR, indent int) {
	prefix := strings.Repeat("\t", indent)
	if emitter.plan.replayEntry[operation.pc] {
		fmt.Fprintf(&emitter.body, "%sreturn machinePreparedReplayEntry()\n", prefix)
		return
	}
	fmt.Fprintf(&emitter.body, "%sexit := context.replayBeforeOperation(%d, %d)\n", prefix, operation.pc, len(operation.spillValues))
	for spillIndex, spill := range operation.spillValues {
		tags := emitter.plan.tags[spill.value-1]
		if payload, optional := backendGoOptionalScalarTags(tags); optional {
			fmt.Fprintf(&emitter.body, "%sif vp%d {\n", prefix, spill.value)
			switch payload {
			case backendTagBool:
				fmt.Fprintf(&emitter.body, "%s\tcontext.spillBool(%d, %d, v%d)\n", prefix, spillIndex, spill.register, spill.value)
			case backendTagNumber:
				fmt.Fprintf(&emitter.body, "%s\tcontext.spillNumber(%d, %d, v%d)\n", prefix, spillIndex, spill.register, spill.value)
			default:
				fmt.Fprintf(&emitter.body, "%s\treturn machinePreparedExit{}\n", prefix)
			}
			fmt.Fprintf(&emitter.body, "%s} else {\n", prefix)
			fmt.Fprintf(&emitter.body, "%s\tcontext.spillNil(%d, %d)\n", prefix, spillIndex, spill.register)
			fmt.Fprintf(&emitter.body, "%s}\n", prefix)
			continue
		}
		switch tags {
		case backendTagNil:
			fmt.Fprintf(&emitter.body, "%scontext.spillNil(%d, %d)\n", prefix, spillIndex, spill.register)
		case backendTagBool:
			fmt.Fprintf(&emitter.body, "%scontext.spillBool(%d, %d, v%d)\n", prefix, spillIndex, spill.register, spill.value)
		case backendTagNumber:
			fmt.Fprintf(&emitter.body, "%scontext.spillNumber(%d, %d, v%d)\n", prefix, spillIndex, spill.register, spill.value)
		default:
			fmt.Fprintf(&emitter.body, "%sreturn machinePreparedExit{}\n", prefix)
			return
		}
	}
	fmt.Fprintf(&emitter.body, "%sreturn exit\n", prefix)
}

func (emitter *backendGoNumericEmitter) emitReplayEntry(indent int) {
	prefix := strings.Repeat("\t", indent)
	if emitter.prepared {
		fmt.Fprintf(&emitter.body, "%sreturn machinePreparedReplayEntry()\n", prefix)
		return
	}
	fmt.Fprintf(&emitter.body, "%s%s\n", prefix, emitter.failureReturn())
}

func (emitter *backendGoNumericEmitter) emitBranch(from int32, targetPC int32, condition string, indent int) {
	target := emitter.ir.pcToBlock[targetPC]
	nextBlock := int32(-1)
	block := &emitter.ir.blocks[from]
	if int(block.last) < len(emitter.ir.ops) {
		nextBlock = emitter.ir.pcToBlock[block.last]
	}
	prefix := strings.Repeat("\t", indent)
	fmt.Fprintf(&emitter.body, "%sif %s {\n", prefix, condition)
	emitter.emitGoto(from, target, indent+1)
	fmt.Fprintf(&emitter.body, "%s}\n", prefix)
	emitter.emitGoto(from, nextBlock, indent)
}

func (emitter *backendGoNumericEmitter) emitGoto(from, to int32, indent int) {
	prefix := strings.Repeat("\t", indent)
	for edgeIndex := range emitter.ir.edges {
		edge := &emitter.ir.edges[edgeIndex]
		if edge.from != from || edge.to != to {
			continue
		}
		for _, copy := range edge.phiCopies {
			if !emitter.plan.used[copy.destination-1] {
				continue
			}
			emitter.emitValueCopy(copy.destination, copy.source, indent)
		}
		fmt.Fprintf(&emitter.body, "%sgoto b%d\n", prefix, to)
		return
	}
	if emitter.prepared {
		fmt.Fprintf(&emitter.body, "%sreturn machinePreparedExit{}\n", prefix)
	} else {
		fmt.Fprintf(&emitter.body, "%s%s\n", prefix, emitter.failureReturn())
	}
}

func (emitter *backendGoNumericEmitter) emitValueCopy(
	destination backendValueID,
	source backendValueID,
	indent int,
) {
	prefix := strings.Repeat("\t", indent)
	destinationTags := emitter.plan.tags[destination-1]
	if destinationTags == backendTagNil {
		return
	}
	if _, optional := backendGoOptionalScalarTags(destinationTags); !optional {
		fmt.Fprintf(&emitter.body, "%sv%d = v%d\n", prefix, destination, source)
		return
	}
	sourceTags := emitter.plan.tags[source-1]
	if sourceTags == backendTagNil {
		fmt.Fprintf(&emitter.body, "%svp%d = false\n", prefix, destination)
		return
	}
	if _, optional := backendGoOptionalScalarTags(sourceTags); optional {
		fmt.Fprintf(&emitter.body, "%sv%d = v%d\n", prefix, destination, source)
		fmt.Fprintf(&emitter.body, "%svp%d = vp%d\n", prefix, destination, source)
		return
	}
	fmt.Fprintf(&emitter.body, "%sv%d = v%d\n", prefix, destination, source)
	fmt.Fprintf(&emitter.body, "%svp%d = true\n", prefix, destination)
}

func (emitter *backendGoNumericEmitter) failureReturn() string {
	if emitter.prepared {
		return "return machinePreparedReplayEntry()"
	}
	if emitter.options.coroutineTarget {
		return "return 0, false, false"
	}
	return backendGoNumericFailureReturn(emitter.resultTypes)
}

func backendGoComparisonOperator(op opcode) string {
	switch op {
	case opEqual:
		return "=="
	case opNotEqual, opJumpIfNotEqualK:
		return "!="
	case opLess, opJumpIfLess, opJumpIfNotLess, opJumpIfLessK, opJumpIfNotLessK:
		return "<"
	case opLessEqual:
		return "<="
	case opGreater, opJumpIfGreater, opJumpIfNotGreater, opJumpIfGreaterK, opJumpIfNotGreaterK:
		return ">"
	case opGreaterEqual:
		return ">="
	default:
		return "=="
	}
}
