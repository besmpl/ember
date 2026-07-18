package preparedplugin_test

import (
	"strings"
	"testing"

	"github.com/besmpl/ember/preparedplugin"
)

func TestOpenRequiresAnAbsoluteArtifactPath(t *testing.T) {
	for _, path := range []string{"", "relative.so"} {
		t.Run(path, func(t *testing.T) {
			bundle, err := preparedplugin.Open(path)
			if bundle != nil || err == nil {
				t.Fatalf("Open(%q) = (%v, %v), want nil error bundle", path, bundle, err)
			}
			if path != "" && !strings.Contains(err.Error(), "absolute") {
				t.Fatalf("Open(%q) error = %v, want absolute-path error", path, err)
			}
		})
	}
}
