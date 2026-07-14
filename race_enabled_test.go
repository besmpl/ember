//go:build race

package ember

import "testing"

const raceInstrumentedTest = true

func assertRaceBuildTagMode(t *testing.T) {
	t.Helper()
	if !raceInstrumentedTest {
		t.Fatal("race build tag selected but race instrumentation is disabled")
	}
	t.Log("race instrumentation enabled=true")
}
