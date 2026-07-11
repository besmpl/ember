package ember

import (
	"runtime/debug"
	"strings"
)

func checkptrInstrumentedTest() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, setting := range info.Settings {
		if setting.Key == "-gcflags" && strings.Contains(setting.Value, "checkptr") {
			return true
		}
	}
	return false
}

func allocationInstrumentedTest() bool {
	return raceInstrumentedTest || checkptrInstrumentedTest()
}
