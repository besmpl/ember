package ember_test

import (
	"fmt"
	"os"
	"runtime"
	"testing"
)

func runtimeAcceptanceProcessError(environment string, actual int) error {
	if environment != "1" {
		return fmt.Errorf("acceptance process: GOMAXPROCS environment must be 1, got %q", environment)
	}
	if actual != 1 {
		return fmt.Errorf("acceptance process: Go-runtime GOMAXPROCS must be 1, got %d", actual)
	}
	return nil
}

func TestRuntimeAcceptanceProcessContract(t *testing.T) {
	if os.Getenv("EMBER_RUNTIME_ACCEPTANCE_PROCESS") != "1" {
		t.Skip("run through scripts/runtime-acceptance-env")
	}
	if err := runtimeAcceptanceProcessError(os.Getenv("GOMAXPROCS"), runtime.GOMAXPROCS(0)); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeAcceptanceProcessRejectsRuntimeMismatch(t *testing.T) {
	if err := runtimeAcceptanceProcessError("1", 2); err == nil {
		t.Fatal("accepted Go-runtime GOMAXPROCS mismatch")
	}
}
