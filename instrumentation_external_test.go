package ember_test

import (
	"runtime/debug"
	"strings"
)

func allocationInstrumentedTest() bool {
	if externalRaceInstrumentedTest {
		return true
	}
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
