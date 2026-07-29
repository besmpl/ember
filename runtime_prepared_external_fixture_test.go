package ember

import (
	"bytes"
	"testing"
)

func TestAssemblePreparedGoSourceAlwaysImportsBundleABI(t *testing.T) {
	generated, err := assemblePreparedGoSource(
		"preparedfixture",
		&backendProgramIR{},
		[]backendGoNumericModule{{functions: []string{""}}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(generated, []byte(`emberapi "github.com/besmpl/ember"`)) {
		t.Fatalf("generated bundle source lacks Ember ABI import:\n%s", generated)
	}
}
