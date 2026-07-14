//go:build !race

package ember

import "testing"

const raceInstrumentedTest = false

func assertRaceBuildTagMode(t *testing.T) {
	t.Helper()
	if raceInstrumentedTest {
		t.Fatal("race build tag is disabled but race instrumentation is enabled")
	}
	t.Log("race instrumentation enabled=false")
}
