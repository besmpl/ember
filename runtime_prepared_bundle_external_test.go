package ember_test

import (
	"testing"

	"github.com/besmpl/ember"
)

func TestPreparedBundleGeneratedSurfaceIsOpaqueAndConstructible(t *testing.T) {
	function := ember.PreparedFunction(func(context ember.PreparedContext) ember.PreparedExit {
		if number, ok := context.NumberParameter(0); ok {
			return ember.PreparedReturnOneNumber(number)
		}
		return ember.PreparedReplayEntry()
	})
	bundle := ember.NewPreparedBundle(1, 1, [32]byte{}, [][]ember.PreparedFunction{{function}})
	if bundle == nil {
		t.Fatal("NewPreparedBundle returned nil")
	}
}
