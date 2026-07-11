package ember

import "reflect"

type CompilerBenchmarkMetrics struct {
	Instructions    int
	Constants       int
	RegisterSlots   int
	ChildProtos     int
	PackedBytes     int64
	ProtoOwnedBytes int64
}

func CompilerBenchmarkMetricsForTest(proto *Proto) CompilerBenchmarkMetrics {
	if proto == nil {
		return CompilerBenchmarkMetrics{}
	}
	return compilerBenchmarkMetrics([]*Proto{proto})
}

func CompilerProgramBenchmarkMetricsForTest(program *Program) CompilerBenchmarkMetrics {
	if program == nil {
		return CompilerBenchmarkMetrics{}
	}
	roots := make([]*Proto, 0, len(program.protos))
	for _, proto := range program.protos {
		if proto != nil {
			roots = append(roots, proto)
		}
	}
	return compilerBenchmarkMetrics(roots)
}

func compilerBenchmarkMetrics(roots []*Proto) CompilerBenchmarkMetrics {
	rootSet := make(map[*Proto]bool, len(roots))
	for _, root := range roots {
		if root != nil {
			rootSet[root] = true
		}
	}
	seen := make(map[*Proto]bool)
	metrics := CompilerBenchmarkMetrics{}
	var visit func(*Proto)
	visit = func(proto *Proto) {
		if proto == nil || seen[proto] {
			return
		}
		seen[proto] = true
		metrics.Instructions += len(proto.code)
		metrics.Constants += len(proto.constants)
		metrics.RegisterSlots += proto.registers
		metrics.PackedBytes += int64(len(proto.packedCode)) * int64(reflect.TypeOf(packedInstruction{}).Size())
		metrics.ProtoOwnedBytes += protoOwnedBenchmarkBytes(proto)
		if !rootSet[proto] {
			metrics.ChildProtos++
		}
		for _, child := range proto.prototypes {
			visit(child)
		}
	}
	for _, root := range roots {
		visit(root)
	}
	return metrics
}

func protoOwnedBenchmarkBytes(proto *Proto) int64 {
	if proto == nil {
		return 0
	}
	value := reflect.ValueOf(proto).Elem()
	owned := int64(value.Type().Size())
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		switch field.Kind() {
		case reflect.String:
			owned += int64(field.Len())
		case reflect.Slice:
			owned += int64(field.Cap()) * int64(field.Type().Elem().Size())
			if field.Type().Elem().Kind() == reflect.String {
				for item := 0; item < field.Len(); item++ {
					owned += int64(field.Index(item).Len())
				}
			}
		}
	}
	return owned
}
