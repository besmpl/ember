package architectureproof

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProofCasesMatchLuau(t *testing.T) {
	if os.Getenv("EMBER_ARCHITECTURE_PROOF_LIVE") != "1" {
		t.Skip("set EMBER_ARCHITECTURE_PROOF_LIVE=1 to compare the proof lowerings with pinned Luau")
	}
	luau := os.Getenv("LUAU_BIN")
	if luau == "" {
		t.Fatal("LUAU_BIN is required")
	}
	if err := validateLuau(luau); err != nil {
		t.Fatal(err)
	}
	if err := validateFrozenFunctions(); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range proofCases {
		candidate := candidate
		t.Run(candidate.id, func(t *testing.T) {
			script := filepath.Join(t.TempDir(), "case.luau")
			if err := os.WriteFile(script, []byte(luauScript(candidate.luauBody)), 0o600); err != nil {
				t.Fatal(err)
			}
			for _, seed := range []int64{0, 1, 7, 29} {
				goMeasurement := measureGo(candidate.run, 3, seed)
				luauMeasurement, err := measureLuau(luau, script, 3, seed)
				if err != nil {
					t.Fatal(err)
				}
				if goMeasurement.checksum != luauMeasurement.checksum {
					t.Fatalf("seed=%d Go checksum=%d, Luau checksum=%d", seed, goMeasurement.checksum, luauMeasurement.checksum)
				}
			}
		})
	}
}

func TestManualLoweringsMatchFrozenResults(t *testing.T) {
	if err := validateFrozenFunctions(); err != nil {
		t.Fatal(err)
	}
}
