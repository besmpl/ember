package preparedworkerbuild

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	workerartifact "github.com/besmpl/ember/internal/preparedworkerartifact"
)

func TestHashToolchainBindsExecutableToolsStandardLibraryAndHeaders(t *testing.T) {
	root := t.TempDir()
	goCommand := writeIdentityFixture(t, root, "go", "go executable one")
	toolDir := filepath.Join(root, "pkg", "tool", "host")
	includeDir := filepath.Join(root, "pkg", "include")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(includeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tool := writeIdentityFixture(t, toolDir, "compile", "compile tool one")
	header := writeIdentityFixture(t, includeDir, "textflag.h", "header one")
	policy := buildPolicy{
		goCommand: goCommand,
		goRoot:    root,
		goToolDir: toolDir,
		goVersion: "go-test-version",
	}
	standard := workerartifact.Digest{1}
	original, err := hashToolchain(policy, standard)
	if err != nil {
		t.Fatal(err)
	}

	assertChanges := func(file, contents string, library workerartifact.Digest) {
		t.Helper()
		if file != "" {
			if err := os.WriteFile(file, []byte(contents), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		changed, err := hashToolchain(policy, library)
		if err != nil {
			t.Fatal(err)
		}
		if changed == original {
			t.Fatal("toolchain identity did not bind changed bytes")
		}
		originalID, err := identityTestDescriptor(policy.goVersion + "+sha256:" + original.String()).BuildID()
		if err != nil {
			t.Fatal(err)
		}
		changedID, err := identityTestDescriptor(policy.goVersion + "+sha256:" + changed.String()).BuildID()
		if err != nil {
			t.Fatal(err)
		}
		if changedID == originalID {
			t.Fatal("same-version changed toolchain bytes retained the same build ID")
		}
	}
	assertChanges(goCommand, "go executable two", standard)
	if err := os.WriteFile(goCommand, []byte("go executable one"), 0o755); err != nil {
		t.Fatal(err)
	}
	assertChanges(tool, "compile tool two", standard)
	if err := os.WriteFile(tool, []byte("compile tool one"), 0o755); err != nil {
		t.Fatal(err)
	}
	assertChanges(header, "header two", standard)
	if err := os.WriteFile(header, []byte("header one"), 0o755); err != nil {
		t.Fatal(err)
	}
	standard[0]++
	assertChanges("", "", standard)
}

func identityTestDescriptor(toolchain string) workerartifact.Descriptor {
	nonzero := workerartifact.Digest{1}
	return workerartifact.Descriptor{
		FormatVersion: workerartifact.DescriptorVersion, ProtocolVersion: workerartifact.ProtocolVersion,
		Contract: nonzero, ProgramRecipeDigest: nonzero, PreparedABIVersion: 1, SemanticVersion: 1,
		ProgramHash: nonzero, ProtoCounts: []uint32{1}, GeneratedDigest: nonzero, WrapperDigest: nonzero,
		EmberVersion: "test", Toolchain: toolchain, TargetOS: "test", TargetArch: "test",
	}
}

func TestBuildPolicyPinsTargetAndToolchainEnvironment(t *testing.T) {
	t.Setenv("GOROOT", "/inherited/root")
	t.Setenv("godebug", "lower-case-inherited")
	t.Setenv("GODEBUG", "toolchaintrace=1")
	t.Setenv("GOTOOLCHAIN", "auto")
	t.Setenv("GOAMD64", "v4")
	t.Setenv("GOFIPS140", "latest")
	environment := seedBuildEnvironment(Target{GOOS: "linux", GOARCH: "amd64"})
	values := make(map[string]string)
	for _, variable := range environment {
		name, value, found := strings.Cut(variable, "=")
		if found {
			values[name] = value
		}
	}
	want := map[string]string{
		"CGO_ENABLED": "0", "GO111MODULE": "on", "GOARCH": "amd64",
		"GOENV": "off", "GOEXPERIMENT": "", "GOFLAGS": "", "GOOS": "linux",
		"GOWORK": "off", "GOAMD64": "v1", "GOFIPS140": "off",
		"GO_EXTLINK_ENABLED": "0", "GODEBUG": "", "GOTOOLCHAIN": "local",
		"GOCACHEPROG": "", "GOTOOLCHAIN_INTERNAL_SWITCH_VERSION": "",
		"GOTOOLCHAIN_INTERNAL_SWITCH_COUNT": "", "GOROOT_FINAL": "",
	}
	for name, value := range want {
		if values[name] != value {
			t.Errorf("%s = %q, want %q", name, values[name], value)
		}
	}
	if _, inherited := values["GOROOT"]; inherited {
		t.Fatal("seed environment retained inherited GOROOT")
	}
}

func TestHashDirectoryTreeRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := writeIdentityFixture(t, root, "target", "bytes")
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	if _, err := hashDirectoryTree(root, 16, 1024); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("hash symlink error = %v", err)
	}
}

func TestBuildPolicyCanonicalFlags(t *testing.T) {
	command, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	moduleDir, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := newBuildPolicy(context.Background(), moduleDir, command, Target{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{"-mod=readonly", "-trimpath", "-buildvcs=false", "-pgo=off", "-buildmode=exe"}
	if !slices.Equal(policy.buildFlags, wantPrefix) || policy.identity[0] != "ember-go-build-policy-v1" ||
		!slices.Equal(policy.identity[1:1+len(wantPrefix)], wantPrefix) {
		t.Fatalf("canonical build flags = %v; identity = %v", policy.buildFlags, policy.identity)
	}
	requiredIdentity := []string{
		"env:CGO_ENABLED=0", "env:GO111MODULE=on", "env:GOTOOLCHAIN=local",
		"env:GOFIPS140=off", "env:GO_EXTLINK_ENABLED=0",
	}
	if runtime.GOARCH == "amd64" {
		requiredIdentity = append(requiredIdentity, "env:GOAMD64=v1")
	} else {
		requiredIdentity = append(requiredIdentity, "env:GOARM64=v8.0")
	}
	for _, required := range requiredIdentity {
		if !slices.Contains(policy.identity, required) {
			t.Errorf("canonical policy identity omits %q: %v", required, policy.identity)
		}
	}
	if !slices.Contains(policy.listFlags, "-pgo=off") || !slices.Contains(policy.buildFlags, "-pgo=off") {
		t.Fatalf("default PGO policy not applied to list/build: list=%v build=%v", policy.listFlags, policy.buildFlags)
	}
}

func TestCanonicalPolicyEnvironmentChangesBuildID(t *testing.T) {
	flags := []string{"-pgo=off", "-buildmode=exe"}
	environment := fixedBuildEnvironment(Target{GOOS: "linux", GOARCH: "amd64"})
	firstIdentity := canonicalPolicyIdentity(flags, environment)
	changedEnvironment := append([]string(nil), environment...)
	for index := range changedEnvironment {
		if changedEnvironment[index] == "GOFIPS140=off" {
			changedEnvironment[index] = "GOFIPS140=v1.0"
		}
	}
	secondIdentity := canonicalPolicyIdentity(flags, changedEnvironment)
	if slices.Equal(firstIdentity, secondIdentity) {
		t.Fatal("changed fixed environment retained the same policy identity")
	}
	first := identityTestDescriptor("go-test+sha256:" + strings.Repeat("1", 64))
	first.BuildFlags = firstIdentity
	second := first.Clone()
	second.BuildFlags = secondIdentity
	firstID, err := first.BuildID()
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := second.BuildID()
	if err != nil {
		t.Fatal(err)
	}
	if firstID == secondID {
		t.Fatal("changed fixed environment retained the same build ID")
	}
}

func TestListedSourceGroupsIncludesHeaders(t *testing.T) {
	groups := listedSourceGroups(listedPackage{HFiles: []string{"textflag.h"}})
	for _, group := range groups {
		if group.kind == "header" && slices.Equal(group.files, []string{"textflag.h"}) {
			return
		}
	}
	t.Fatalf("listed source groups omit HFiles: %#v", groups)
}

func writeIdentityFixture(t *testing.T, directory, name, contents string) string {
	t.Helper()
	file := filepath.Join(directory, name)
	if err := os.WriteFile(file, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	return file
}
