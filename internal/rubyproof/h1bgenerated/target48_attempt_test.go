package h1bgenerated

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

const target48AttemptSchema = "target48-attempt-plan-v2"

var target48RequiredIdentityRoles = []string{
	"adr", "attempt", "benchmark", "checks", "compiler-receipt", "comparator", "disassembly-receipt",
	"gate", "generated", "generated-tests", "host-controller", "observer", "parser", "runner",
	"schedule", "statistics",
}

type target48AttemptPlan struct {
	campaignSHA256, capture string
	attemptOrdinal          int
	scheduleSHA256          string
	analysisSeed            uint64
	binarySHA256            string
	binaryBytes             uint64
	buildID                 string
	goVersion               string
	goos, goarch            string
	bootSession             string
	hostProfileSHA256       string
	controllerReceiptSHA256 string
	argv500msSHA256         string
	argv2sSHA256            string
	environmentSHA256       string
	previousLedgerSHA256    string
	identities              map[string]string
}

func target48ValidateAttemptPlan(plan target48AttemptPlan) error {
	if plan.capture != "A" && plan.capture != "B" {
		return fmt.Errorf("target48: invalid capture %q", plan.capture)
	}
	if plan.attemptOrdinal < 1 || plan.attemptOrdinal > 4 {
		return fmt.Errorf("target48: attempt ordinal %d outside 1..4", plan.attemptOrdinal)
	}
	selected := target48Schedules[0]
	if plan.capture == "B" {
		selected = target48Schedules[1]
	}
	if plan.scheduleSHA256 != selected.wantSHA256 {
		return fmt.Errorf("target48: capture %s schedule digest mismatch", plan.capture)
	}
	if plan.analysisSeed != target48BootstrapSeed {
		return fmt.Errorf("target48: analysis seed mismatch")
	}
	if plan.binaryBytes == 0 || plan.binaryBytes > 64<<20 {
		return fmt.Errorf("target48: binary size %d outside 1..64MiB", plan.binaryBytes)
	}
	if plan.goVersion != "go1.26.4" {
		return fmt.Errorf("target48: toolchain = %q, want go1.26.4", plan.goVersion)
	}
	for name, value := range map[string]string{
		"campaign": plan.campaignSHA256, "binary": plan.binarySHA256,
		"host-profile": plan.hostProfileSHA256, "controller-receipt": plan.controllerReceiptSHA256,
		"argv-500ms": plan.argv500msSHA256, "argv-2s": plan.argv2sSHA256,
		"environment": plan.environmentSHA256, "previous-ledger": plan.previousLedgerSHA256,
	} {
		if !target48IsSHA256(value) {
			return fmt.Errorf("target48: %s is not lowercase SHA-256", name)
		}
	}
	for name, value := range map[string]string{
		"build-id": plan.buildID, "goos": plan.goos, "goarch": plan.goarch, "boot-session": plan.bootSession,
	} {
		if !target48IsToken(value) {
			return fmt.Errorf("target48: %s is not a canonical token", name)
		}
	}
	if len(plan.identities) != len(target48RequiredIdentityRoles) {
		return fmt.Errorf("target48: identity role count = %d, want %d", len(plan.identities), len(target48RequiredIdentityRoles))
	}
	for _, role := range target48RequiredIdentityRoles {
		if !target48IsSHA256(plan.identities[role]) {
			return fmt.Errorf("target48: missing or invalid %s identity", role)
		}
	}
	if plan.identities["schedule"] != plan.scheduleSHA256 {
		return errors.New("target48: schedule identity does not bind selected schedule")
	}
	return nil
}

func (plan target48AttemptPlan) canonical() ([]byte, error) {
	if err := target48ValidateAttemptPlan(plan); err != nil {
		return nil, err
	}
	var output strings.Builder
	write := func(key, value string) { fmt.Fprintf(&output, "%s\t%s\n", key, value) }
	write("schema", target48AttemptSchema)
	write("campaign.sha256", plan.campaignSHA256)
	write("capture", plan.capture)
	write("attempt", strconv.Itoa(plan.attemptOrdinal))
	write("schedule.sha256", plan.scheduleSHA256)
	write("analysis.seed", fmt.Sprintf("%016x", plan.analysisSeed))
	write("binary.sha256", plan.binarySHA256)
	write("binary.bytes", strconv.FormatUint(plan.binaryBytes, 10))
	write("binary.build-id", plan.buildID)
	write("toolchain", plan.goVersion)
	// Keep the target components framed independently. target48IsToken permits
	// '/', so joining them into one field would let distinct GOOS/GOARCH pairs
	// produce the same attempt identity.
	write("target.goos", plan.goos)
	write("target.goarch", plan.goarch)
	write("runtime", "cgo=0,pgo=off,gomaxprocs=1,gogc=off")
	write("boot-session", plan.bootSession)
	write("host-profile.sha256", plan.hostProfileSHA256)
	write("controller-receipt.sha256", plan.controllerReceiptSHA256)
	write("argv-500ms.sha256", plan.argv500msSHA256)
	write("argv-2s.sha256", plan.argv2sSHA256)
	write("environment.sha256", plan.environmentSHA256)
	write("previous-ledger.sha256", plan.previousLedgerSHA256)
	for _, role := range target48RequiredIdentityRoles {
		write("identity."+role+".sha256", plan.identities[role])
	}
	return []byte(output.String()), nil
}

func target48AttemptID(plan target48AttemptPlan) (string, error) {
	canonical, err := plan.canonical()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(canonical)), nil
}

func target48IsSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func target48IsToken(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') &&
			!(char >= '0' && char <= '9') && !strings.ContainsRune("._:/+-", char) {
			return false
		}
	}
	return true
}

type target48AttemptDisposition uint8

const (
	target48ExternallyInvalid target48AttemptDisposition = iota + 1
	target48ValidStrong
	target48ValidPass
	target48ValidInconclusive
	target48ValidFail
	target48ValidNoiseBreach
)

type target48AttemptLedgerRow struct {
	attemptID, capture, bootSession string
	disposition                     target48AttemptDisposition
}

type target48CampaignClass uint8

const (
	target48CampaignOpen target48CampaignClass = iota + 1
	target48CampaignStrong
	target48CampaignPass
	target48CampaignUnfavorable
	target48CampaignExhausted
)

// target48ValidateAttemptLedger freezes only the pure stopping policy. The
// effectful owner must publish each immutable plan before warmup and append its
// terminal row without overwriting earlier attempts.
func target48ValidateAttemptLedger(rows []target48AttemptLedgerRow) (target48CampaignClass, error) {
	if len(rows) > 4 {
		return 0, errors.New("target48: more than four started attempts")
	}
	wantCapture := "A"
	validA, strongA := false, false
	seenAttempts := make(map[string]bool, len(rows))
	bootA := ""
	for index, row := range rows {
		if !target48IsSHA256(row.attemptID) || seenAttempts[row.attemptID] {
			return 0, fmt.Errorf("target48: invalid or duplicate attempt ID at row %d", index+1)
		}
		seenAttempts[row.attemptID] = true
		if row.capture != wantCapture {
			return 0, fmt.Errorf("target48: row %d capture = %s, want %s", index+1, row.capture, wantCapture)
		}
		if !target48IsToken(row.bootSession) {
			return 0, fmt.Errorf("target48: row %d invalid boot session", index+1)
		}
		if row.capture == "B" {
			if !validA {
				return 0, errors.New("target48: capture B has no valid capture A predecessor")
			}
			if row.bootSession == bootA {
				return 0, errors.New("target48: capture B is not reboot-separated from valid A")
			}
		}
		switch row.disposition {
		case target48ExternallyInvalid:
			// Only independently attested exogenous invalidity may reach this
			// state. Noise is outcome-derived and is terminal unfavorable.
			continue
		case target48ValidStrong, target48ValidPass:
			if row.capture == "A" {
				validA, strongA, bootA, wantCapture = true, row.disposition == target48ValidStrong, row.bootSession, "B"
				continue
			}
			if index != len(rows)-1 {
				return 0, errors.New("target48: successful capture B did not stop acquisition")
			}
			if strongA && row.disposition == target48ValidStrong {
				return target48CampaignStrong, nil
			}
			return target48CampaignPass, nil
		case target48ValidInconclusive, target48ValidFail, target48ValidNoiseBreach:
			if index != len(rows)-1 {
				return 0, errors.New("target48: valid non-pass did not stop acquisition")
			}
			return target48CampaignUnfavorable, nil
		default:
			return 0, fmt.Errorf("target48: invalid disposition %d", row.disposition)
		}
	}
	if len(rows) == 4 {
		return target48CampaignExhausted, nil
	}
	return target48CampaignOpen, nil
}

func target48SyntheticAttemptPlan() target48AttemptPlan {
	digest := strings.Repeat("a", 64)
	identities := make(map[string]string, len(target48RequiredIdentityRoles))
	for _, role := range target48RequiredIdentityRoles {
		identities[role] = digest
	}
	identities["schedule"] = target48Schedules[0].wantSHA256
	return target48AttemptPlan{
		campaignSHA256: digest, capture: "A", attemptOrdinal: 1,
		scheduleSHA256: target48Schedules[0].wantSHA256, analysisSeed: target48BootstrapSeed,
		binarySHA256: digest, binaryBytes: 1 << 20, buildID: "test-build-id", goVersion: "go1.26.4",
		goos: "linux", goarch: "amd64", bootSession: "boot-a",
		hostProfileSHA256: digest, controllerReceiptSHA256: digest,
		argv500msSHA256: digest, argv2sSHA256: digest, environmentSHA256: digest,
		previousLedgerSHA256: digest, identities: identities,
	}
}

func TestTarget48AttemptPlanCanonicalIdentity(t *testing.T) {
	plan := target48SyntheticAttemptPlan()
	canonical, err := plan.canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(canonical), "schema\t"+target48AttemptSchema+"\n") || canonical[len(canonical)-1] != '\n' {
		t.Fatalf("noncanonical plan:\n%s", canonical)
	}
	firstID, err := target48AttemptID(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.binaryBytes++
	secondID, err := target48AttemptID(plan)
	if err != nil {
		t.Fatal(err)
	}
	if firstID == secondID {
		t.Fatal("binary size did not change attempt identity")
	}

	left := target48SyntheticAttemptPlan()
	left.goos, left.goarch = "linux/amd64", "v1"
	right := target48SyntheticAttemptPlan()
	right.goos, right.goarch = "linux", "amd64/v1"
	leftID, err := target48AttemptID(left)
	if err != nil {
		t.Fatal(err)
	}
	rightID, err := target48AttemptID(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftID == rightID {
		t.Fatal("distinct GOOS/GOARCH components produced the same attempt identity")
	}
}

func TestTarget48AttemptPlanFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*target48AttemptPlan)
	}{
		{"wrong schedule", func(plan *target48AttemptPlan) { plan.scheduleSHA256 = strings.Repeat("b", 64) }},
		{"wrong analysis seed", func(plan *target48AttemptPlan) { plan.analysisSeed++ }},
		{"wrong toolchain", func(plan *target48AttemptPlan) { plan.goVersion = "go1.26.5" }},
		{"missing identity", func(plan *target48AttemptPlan) { delete(plan.identities, "runner") }},
		{"noncanonical digest", func(plan *target48AttemptPlan) { plan.binarySHA256 = strings.Repeat("A", 64) }},
		{"unsafe token", func(plan *target48AttemptPlan) { plan.bootSession = "boot\nforged" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := target48SyntheticAttemptPlan()
			test.mutate(&plan)
			if _, err := plan.canonical(); err == nil {
				t.Fatal("invalid attempt plan accepted")
			}
		})
	}
}

func TestTarget48AttemptLedgerStoppingPolicy(t *testing.T) {
	digest := func(symbol byte) string { return strings.Repeat(string(symbol), 64) }
	strong, err := target48ValidateAttemptLedger([]target48AttemptLedgerRow{
		{digest('a'), "A", "boot-1", target48ExternallyInvalid},
		{digest('b'), "A", "boot-2", target48ValidStrong},
		{digest('c'), "B", "boot-3", target48ValidStrong},
	})
	if err != nil || strong != target48CampaignStrong {
		t.Fatalf("strong campaign = %d, %v", strong, err)
	}
	pass, err := target48ValidateAttemptLedger([]target48AttemptLedgerRow{
		{digest('a'), "A", "boot-1", target48ValidPass},
		{digest('b'), "B", "boot-2", target48ValidStrong},
	})
	if err != nil || pass != target48CampaignPass {
		t.Fatalf("pass campaign = %d, %v", pass, err)
	}
	unfavorable, err := target48ValidateAttemptLedger([]target48AttemptLedgerRow{
		{digest('a'), "A", "boot-1", target48ValidNoiseBreach},
	})
	if err != nil || unfavorable != target48CampaignUnfavorable {
		t.Fatalf("unfavorable campaign = %d, %v", unfavorable, err)
	}
	invalid := [][]target48AttemptLedgerRow{
		{{digest('a'), "B", "boot-1", target48ValidPass}},
		{{digest('a'), "A", "boot-1", target48ValidPass}, {digest('b'), "B", "boot-1", target48ValidPass}},
		{{digest('a'), "A", "boot-1", target48ValidPass}, {digest('b'), "B", "boot-1", target48ValidInconclusive}},
		{{digest('a'), "A", "boot-1", target48ValidPass}, {digest('b'), "B", "boot-2", target48ValidPass}, {digest('c'), "B", "boot-3", target48ExternallyInvalid}},
		{{digest('a'), "A", "boot-1", target48ValidFail}, {digest('b'), "A", "boot-2", target48ExternallyInvalid}},
		{{digest('a'), "A", "boot-1", target48ExternallyInvalid}, {digest('b'), "A", "boot-2", target48ExternallyInvalid}, {digest('c'), "A", "boot-3", target48ExternallyInvalid}, {digest('d'), "A", "boot-4", target48ExternallyInvalid}, {digest('e'), "A", "boot-5", target48ExternallyInvalid}},
	}
	for index, rows := range invalid {
		if _, err := target48ValidateAttemptLedger(rows); err == nil {
			t.Errorf("invalid ledger %d accepted", index)
		}
	}
	exhausted, err := target48ValidateAttemptLedger([]target48AttemptLedgerRow{
		{digest('a'), "A", "boot-1", target48ExternallyInvalid},
		{digest('b'), "A", "boot-2", target48ExternallyInvalid},
		{digest('c'), "A", "boot-3", target48ExternallyInvalid},
		{digest('d'), "A", "boot-4", target48ExternallyInvalid},
	})
	if err != nil || exhausted != target48CampaignExhausted {
		t.Fatalf("exhausted campaign = %d, %v", exhausted, err)
	}
}
