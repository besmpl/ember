package sprigproof_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/besmpl/ember/internal/sprigproof"
	"github.com/besmpl/ember/preparedsource"
)

func TestExternalStaticApplicationHasNoDeliveryDependency(t *testing.T) {
	program := proofProgram(t)
	set, err := program.PreparedSource("generated")
	if err != nil {
		t.Fatal(err)
	}
	root, env := materializeApplication(t, set, `package main
import (
 "context"
 "fmt"
 "example.com/sprig-static/generated"
)
func main(){ result,err:=generated.Reduce(context.Background(),[]generated.Reading{{Value:6,Divisor:2},{Value:1,Divisor:0}},3);if err!=nil{panic(err)};fmt.Printf("%d|%d",result.Total,result.Rejected[0]) }
`)
	assertExactApplicationFiles(t, root, []string{"cmd/app/main.go", "generated/reduce_generated.go", "go.mod"})
	imports := parseImports(t, filepath.Join(root, "generated", "reduce_generated.go"))
	wantImports := []string{"context", "errors", "fmt", "math"}
	if strings.Join(imports, "|") != strings.Join(wantImports, "|") {
		t.Fatalf("generated imports=%v", imports)
	}
	modules := runCommand(t, root, env, "go", "list", "-m", "all")
	if strings.TrimSpace(modules) != "example.com/sprig-static" {
		t.Fatalf("module graph=%s", modules)
	}
	nonstandard := commandLines(runCommand(t, root, env, "go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."))
	want := []string{"example.com/sprig-static/cmd/app", "example.com/sprig-static/generated"}
	sort.Strings(nonstandard)
	sort.Strings(want)
	if strings.Join(nonstandard, "|") != strings.Join(want, "|") {
		t.Fatalf("nonstandard deps=%v want %v", nonstandard, want)
	}
	binary := filepath.Join(root, "app")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	runCommand(t, root, env, "go", "build", "-o", binary, "./cmd/app")
	buildInfo := runCommand(t, root, env, "go", "version", "-m", binary)
	if !strings.Contains(buildInfo, "path\texample.com/sprig-static/cmd/app") || strings.Contains(buildInfo, "dep\t") || !strings.Contains(buildInfo, "build\tCGO_ENABLED=0") {
		t.Fatalf("unexpected build info:\n%s", buildInfo)
	}
	assertNoToolingNames(t, "build info", buildInfo)
	symbols := runCommand(t, root, env, "go", "tool", "nm", binary)
	assertNoToolingNames(t, "symbols", symbols)
	if output := strings.TrimSpace(runCommand(t, root, env, binary)); output != "3|7" {
		t.Fatalf("native output=%q", output)
	}
}

func TestProjectPackagesBuildOfflineAndReuseIndependentCacheLeaf(t *testing.T) {
	project := proofProject(t)
	prepared, err := project.PrepareStandalone([]sprigproof.PackageID{"counter", "math"})
	if err != nil {
		t.Fatal(err)
	}
	baseLayout, err := preparedsource.NewLayout([]preparedsource.Mount{{Path: "generated/math", Set: prepared[1].Set()}, {Path: "generated/counter", Set: prepared[0].Set()}})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/sprig-project\n\ngo 1.26\n")
	writeLayout(t, root, baseLayout)
	mustWrite(t, filepath.Join(root, "cmd/app/main.go"), `package main
import (
 "context"
 "fmt"
 counter "example.com/sprig-project/generated/counter"
 mathpkg "example.com/sprig-project/generated/math"
)
func main(){ r,err:=counter.Reduce(context.Background(),[]counter.Reading{{Value:12,Divisor:3},{Value:1,Divisor:0}},3);if err!=nil{panic(err)};q,d:=mathpkg.Divide(20,5);fmt.Printf("%d|%d|%d|%d",r.Total,r.Rejected[0],q,d) }
`)
	assertExactApplicationFiles(t, root, []string{"cmd/app/main.go", "generated/counter/reduce_generated.go", "generated/math/package_generated.go", "go.mod"})
	env := replaceEnvironment(os.Environ(), []string{"CGO_ENABLED=0", "GOENV=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOCACHE=" + filepath.Join(root, "go-cache"), "GOMODCACHE=" + filepath.Join(root, "mod-cache")})
	if modules := strings.TrimSpace(runCommand(t, root, env, "go", "list", "-m", "all")); modules != "example.com/sprig-project" {
		t.Fatalf("module graph=%s", modules)
	}
	wantDeps := []string{"example.com/sprig-project/cmd/app", "example.com/sprig-project/generated/counter", "example.com/sprig-project/generated/math"}
	gotDeps := commandLines(runCommand(t, root, env, "go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."))
	sort.Strings(gotDeps)
	sort.Strings(wantDeps)
	if fmt.Sprint(gotDeps) != fmt.Sprint(wantDeps) {
		t.Fatalf("nonstandard deps=%v", gotDeps)
	}
	flags := []string{"build", "-x", "-mod=readonly", "-buildvcs=false", "-trimpath", "-pgo=off", "-buildmode=exe"}
	freshBinary := nativeTestExecutable(root, "fresh-app")
	fresh := runCommand(t, root, env, "go", append(flags, "-o", freshBinary, "./cmd/app")...)
	assertCompileCounts(t, "fresh", fresh, map[string]int{"example.com/sprig-project/generated/counter": 1, "example.com/sprig-project/generated/math": 1, "main": 1})
	baseIDs := packageBuildIDs(t, root, env)
	const appImportPath = "example.com/sprig-project/cmd/app"
	if baseIDs[appImportPath] == "" {
		t.Fatalf("missing application BuildID: %v", baseIDs)
	}
	freshDigest := fileDigest(t, freshBinary)
	warmBinary := nativeTestExecutable(root, "warm-app")
	warm := runCommand(t, root, env, "go", append(flags, "-o", warmBinary, "./cmd/app")...)
	assertCompileCounts(t, "warm", warm, map[string]int{"example.com/sprig-project/generated/counter": 0, "example.com/sprig-project/generated/math": 0, "main": 0})
	if got := packageBuildIDs(t, root, env); !reflect.DeepEqual(got, baseIDs) {
		t.Fatalf("warm BuildIDs changed: %v/%v", baseIDs, got)
	}
	if output := strings.TrimSpace(runCommand(t, root, env, freshBinary)); output != "4|7|4|0" {
		t.Fatalf("fresh output=%q", output)
	}
	buildInfo := runCommand(t, root, env, "go", "version", "-m", freshBinary)
	if !strings.Contains(buildInfo, "build\tCGO_ENABLED=0") || strings.Contains(buildInfo, "dep\t") {
		t.Fatalf("build info:\n%s", buildInfo)
	}
	assertNoToolingNames(t, "build info", buildInfo)
	assertNoToolingNames(t, "symbols", runCommand(t, root, env, "go", "tool", "nm", freshBinary))
	changedSources := sprigproof.ProofProjectSources()
	changedSources[0].Source = strings.Replace(changedSources[0].Source, "let total = 0", "let total = 10", 1)
	changed, diagnostics := sprigproof.CompileProject(changedSources)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	changedPrepared, _ := changed.PrepareStandalone([]sprigproof.PackageID{"counter", "math"})
	if changedPrepared[1].Set().Digest() != prepared[1].Set().Digest() || changedPrepared[1].Set().Files()[0].Content != prepared[1].Set().Files()[0].Content {
		t.Fatal("math Set changed")
	}
	changedLayout, err := preparedsource.NewLayout([]preparedsource.Mount{{Path: "generated/counter", Set: changedPrepared[0].Set()}, {Path: "generated/math", Set: changedPrepared[1].Set()}})
	if err != nil {
		t.Fatal(err)
	}
	if changedLayout.Digest() == baseLayout.Digest() {
		t.Fatal("Layout identity did not change")
	}
	writeLayout(t, root, changedLayout)
	changedBinary := nativeTestExecutable(root, "changed-app")
	changedTrace := runCommand(t, root, env, "go", append(flags, "-o", changedBinary, "./cmd/app")...)
	assertCompileCounts(t, "root change", changedTrace, map[string]int{"example.com/sprig-project/generated/counter": 1, "example.com/sprig-project/generated/math": 0, "main": 1})
	changedIDs := packageBuildIDs(t, root, env)
	if changedIDs["example.com/sprig-project/generated/counter"] == baseIDs["example.com/sprig-project/generated/counter"] || changedIDs["example.com/sprig-project/generated/math"] != baseIDs["example.com/sprig-project/generated/math"] || changedIDs[appImportPath] == baseIDs[appImportPath] {
		t.Fatalf("leaf BuildIDs base=%v changed=%v", baseIDs, changedIDs)
	}
	if fileDigest(t, changedBinary) == freshDigest {
		t.Fatal("root change preserved executable digest")
	}
	if output := strings.TrimSpace(runCommand(t, root, env, changedBinary)); output != "14|7|4|0" {
		t.Fatalf("changed output=%q", output)
	}
	dependencySources := sprigproof.ProofProjectSources()
	dependencySources[1].Source = strings.Replace(dependencySources[1].Source, "return left + right", "return left + right + 0", 1)
	dependencyChanged, diagnostics := sprigproof.CompileProject(dependencySources)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	dependencyPrepared, err := dependencyChanged.PrepareStandalone([]sprigproof.PackageID{"counter", "math"})
	if err != nil {
		t.Fatal(err)
	}
	dependencyLayout, err := preparedsource.NewLayout([]preparedsource.Mount{{Path: "generated/counter", Set: dependencyPrepared[0].Set()}, {Path: "generated/math", Set: dependencyPrepared[1].Set()}})
	if err != nil {
		t.Fatal(err)
	}
	writeLayout(t, root, dependencyLayout)
	dependencyBinary := nativeTestExecutable(root, "dependency-app")
	dependencyTrace := runCommand(t, root, env, "go", append(flags, "-o", dependencyBinary, "./cmd/app")...)
	assertCompileCounts(t, "dependency change", dependencyTrace, map[string]int{"example.com/sprig-project/generated/counter": 1, "example.com/sprig-project/generated/math": 1, "main": 1})
	dependencyIDs := packageBuildIDs(t, root, env)
	for _, path := range []string{"example.com/sprig-project/generated/counter", "example.com/sprig-project/generated/math", appImportPath} {
		if dependencyIDs[path] == baseIDs[path] {
			t.Fatalf("dependency change preserved %s BuildID: %v/%v", path, baseIDs, dependencyIDs)
		}
	}
	if fileDigest(t, dependencyBinary) == freshDigest {
		t.Fatal("dependency change preserved executable digest")
	}
	if output := strings.TrimSpace(runCommand(t, root, env, dependencyBinary)); output != "4|7|4|0" {
		t.Fatalf("dependency output=%q", output)
	}
}

func TestTransitiveImportedCallsAgreeAndExecuteNatively(t *testing.T) {
	project, diagnostics := sprigproof.CompileProject(transitiveProjectSources())
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	program, ok := project.Program("counter")
	if !ok {
		t.Fatal("counter Reduce missing")
	}
	left, err := program.Reduce(context.Background(), []sprigproof.Reading{{Value: math.MinInt64, Divisor: -1}}, 2)
	if err != nil || left.Tag != sprigproof.ResultError || left.Code != sprigproof.DomainOverflow {
		t.Fatalf("left-first imported error=%#v,%v", left, err)
	}
	right, err := program.Reduce(context.Background(), []sprigproof.Reading{{Value: 4, Divisor: 2}}, 2)
	if err != nil || right.Tag != sprigproof.ResultError || right.Code != sprigproof.DomainDivideByZero {
		t.Fatalf("right imported error=%#v,%v", right, err)
	}
	prepared, err := project.PrepareStandalone(packageEntries(project))
	if err != nil {
		t.Fatal(err)
	}
	mounts := make([]preparedsource.Mount, len(prepared))
	for i, item := range prepared {
		mounts[i] = preparedsource.Mount{Path: "generated/" + string(item.Package().ID()), Set: item.Set()}
	}
	layout, err := preparedsource.NewLayout(mounts)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/sprig-transitive\n\ngo 1.26\n")
	writeLayout(t, root, layout)
	mustWrite(t, filepath.Join(root, "cmd/app/main.go"), `package main
import (
 "context"
 "fmt"
 "math"
 counter "example.com/sprig-transitive/generated/counter"
 mathpkg "example.com/sprig-transitive/generated/math"
 spare "example.com/sprig-transitive/generated/spare"
 util "example.com/sprig-transitive/generated/util"
)
func main(){ left,_:=counter.Reduce(context.Background(),[]counter.Reading{{Value:math.MinInt64,Divisor:-1}},2);right,_:=counter.Reduce(context.Background(),[]counter.Reading{{Value:4,Divisor:2}},2);q,qd:=mathpkg.Divide(20,5);u,ud:=util.Divide(9,3);s,sd:=spare.Identity(9);fmt.Printf("%d|%d|%d|%d|%d|%d|%d|%d",left.Code,right.Code,q,qd,u,ud,s,sd) }
`)
	env := replaceEnvironment(os.Environ(), []string{"CGO_ENABLED=0", "GOENV=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOCACHE=" + filepath.Join(root, "go-cache"), "GOMODCACHE=" + filepath.Join(root, "mod-cache")})
	if output := strings.TrimSpace(runCommand(t, root, env, "go", "run", "-mod=readonly", "./cmd/app")); output != "1|2|4|0|3|0|9|0" {
		t.Fatalf("transitive native output=%q", output)
	}
}

func TestLinkedProjectBuildsOfflineWithExactSemanticsAndCacheAttribution(t *testing.T) {
	const module = "example.com/sprig-linked"
	bindings := []sprigproof.PackageBinding{
		{ID: "counter", ImportPath: module + "/generated/counter"},
		{ID: "math", ImportPath: module + "/generated/math"},
		{ID: "util", ImportPath: module + "/generated/util"},
	}
	project, diagnostics := sprigproof.CompileProject(transitiveProjectSources())
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	program, ok := project.Program("counter")
	if !ok {
		t.Fatal("counter Reduce missing")
	}
	semanticCases := []struct {
		input []sprigproof.Reading
		limit uint64
		want  sprigproof.Result
	}{
		{[]sprigproof.Reading{{Value: 12, Divisor: 3}, {Value: 1, Divisor: 0}}, 3, sprigproof.Result{Tag: sprigproof.ResultOK, Total: 4, Rejected: []int64{7}}},
		{[]sprigproof.Reading{{Value: math.MinInt64, Divisor: -1}}, 2, sprigproof.Result{Tag: sprigproof.ResultError, Code: sprigproof.DomainOverflow}},
		{[]sprigproof.Reading{{Value: 4, Divisor: 2}}, 2, sprigproof.Result{Tag: sprigproof.ResultError, Code: sprigproof.DomainDivideByZero}},
	}
	for _, test := range semanticCases {
		got, err := program.Reduce(context.Background(), test.input, test.limit)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("linked evaluator=%#v,%v want %#v", got, err, test.want)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := program.Reduce(canceled, nil, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("linked evaluator cancellation=%v", err)
	}
	if _, err := program.Reduce(context.Background(), []sprigproof.Reading{{Value: 8, Divisor: 2}}, 1); !errors.Is(err, sprigproof.ErrStepLimit) {
		t.Fatalf("linked evaluator limit=%v", err)
	}
	prepared, err := project.PrepareLinked([]sprigproof.PackageID{"counter"}, bindings)
	if err != nil {
		t.Fatal(err)
	}
	mounts := make([]preparedsource.Mount, len(prepared))
	for i, item := range prepared {
		mounts[i] = preparedsource.Mount{Path: "generated/" + string(item.Package().ID()), Set: item.Set()}
	}
	layout, err := preparedsource.NewLayout(mounts)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module "+module+"\n\ngo 1.26\n")
	writeLayout(t, root, layout)
	mustWrite(t, filepath.Join(root, "cmd/app/main.go"), `package main
import (
 "context"
 "errors"
 "fmt"
 "math"
 counter "example.com/sprig-linked/generated/counter"
)
func main(){
 good,_:=counter.Reduce(context.Background(),[]counter.Reading{{Value:12,Divisor:3},{Value:1,Divisor:0}},3)
 left,_:=counter.Reduce(context.Background(),[]counter.Reading{{Value:math.MinInt64,Divisor:-1}},2)
 right,_:=counter.Reduce(context.Background(),[]counter.Reading{{Value:4,Divisor:2}},2)
 canceled,cancel:=context.WithCancel(context.Background());cancel();_,cancelErr:=counter.Reduce(canceled,nil,0)
 _,limitErr:=counter.Reduce(context.Background(),[]counter.Reading{{Value:8,Divisor:2}},1)
 fmt.Printf("%d|%d|%d|%d|%t|%t",good.Total,good.Rejected[0],left.Code,right.Code,errors.Is(cancelErr,context.Canceled),errors.Is(limitErr,counter.ErrStepLimit))
}
`)
	assertExactApplicationFiles(t, root, []string{
		"cmd/app/main.go",
		"generated/counter/reduce_generated.go",
		"generated/math/package_generated.go",
		"generated/util/package_generated.go",
		"go.mod",
	})
	env := replaceEnvironment(os.Environ(), []string{"CGO_ENABLED=0", "GOENV=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOCACHE=" + filepath.Join(root, "go-cache"), "GOMODCACHE=" + filepath.Join(root, "mod-cache")})
	if modules := strings.TrimSpace(runCommand(t, root, env, "go", "list", "-m", "all")); modules != module {
		t.Fatalf("module graph=%s", modules)
	}
	wantDeps := []string{module + "/cmd/app", module + "/generated/counter", module + "/generated/math", module + "/generated/util"}
	gotDeps := commandLines(runCommand(t, root, env, "go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./..."))
	sort.Strings(gotDeps)
	sort.Strings(wantDeps)
	if !slices.Equal(gotDeps, wantDeps) {
		t.Fatalf("nonstandard deps=%v want %v", gotDeps, wantDeps)
	}
	flags := []string{"build", "-x", "-mod=readonly", "-buildvcs=false", "-trimpath", "-pgo=off", "-buildmode=exe"}
	baseBinary := nativeTestExecutable(root, "base-app")
	fresh := runCommand(t, root, env, "go", append(flags, "-o", baseBinary, "./cmd/app")...)
	assertCompileCounts(t, "linked fresh", fresh, map[string]int{module + "/generated/counter": 1, module + "/generated/math": 1, module + "/generated/util": 1, "main": 1})
	baseIDs := packageBuildIDs(t, root, env)
	baseDigest := fileDigest(t, baseBinary)
	warm := runCommand(t, root, env, "go", append(flags, "-o", nativeTestExecutable(root, "warm-app"), "./cmd/app")...)
	assertCompileCounts(t, "linked warm", warm, map[string]int{module + "/generated/counter": 0, module + "/generated/math": 0, module + "/generated/util": 0, "main": 0})
	if output := strings.TrimSpace(runCommand(t, root, env, baseBinary)); output != "4|7|1|2|true|true" {
		t.Fatalf("linked native output=%q", output)
	}
	buildInfo := runCommand(t, root, env, "go", "version", "-m", baseBinary)
	if !strings.Contains(buildInfo, "build\tCGO_ENABLED=0") || strings.Contains(buildInfo, "dep\t") {
		t.Fatalf("build info:\n%s", buildInfo)
	}
	assertNoToolingNames(t, "build info", buildInfo)
	assertNoToolingNames(t, "symbols", runCommand(t, root, env, "go", "tool", "nm", baseBinary))

	rootChangedSources := transitiveProjectSources()
	for i := range rootChangedSources {
		if rootChangedSources[i].ID == "counter" {
			rootChangedSources[i].Source = strings.Replace(rootChangedSources[i].Source, "let total = 0", "let total = 10", 1)
		}
	}
	rootChanged, diagnostics := sprigproof.CompileProject(rootChangedSources)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	rootPrepared, err := rootChanged.PrepareLinked([]sprigproof.PackageID{"counter"}, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if rootPrepared[1].Set().Digest() != prepared[1].Set().Digest() || rootPrepared[2].Set().Digest() != prepared[2].Set().Digest() {
		t.Fatal("root change reached linked dependencies")
	}
	writePreparedPackages(t, root, rootPrepared)
	rootBinary := nativeTestExecutable(root, "root-app")
	rootTrace := runCommand(t, root, env, "go", append(flags, "-o", rootBinary, "./cmd/app")...)
	assertCompileCounts(t, "linked root change", rootTrace, map[string]int{module + "/generated/counter": 1, module + "/generated/math": 0, module + "/generated/util": 0, "main": 1})
	rootIDs := packageBuildIDs(t, root, env)
	if rootIDs[module+"/generated/counter"] == baseIDs[module+"/generated/counter"] || rootIDs[module+"/generated/math"] != baseIDs[module+"/generated/math"] || rootIDs[module+"/generated/util"] != baseIDs[module+"/generated/util"] || rootIDs[module+"/cmd/app"] == baseIDs[module+"/cmd/app"] {
		t.Fatalf("root BuildIDs base=%v changed=%v", baseIDs, rootIDs)
	}
	if fileDigest(t, rootBinary) == baseDigest {
		t.Fatal("root change preserved linked executable digest")
	}
	if output := strings.TrimSpace(runCommand(t, root, env, rootBinary)); output != "14|7|1|2|true|true" {
		t.Fatalf("linked root output=%q", output)
	}

	dependencySources := transitiveProjectSources()
	for i := range dependencySources {
		if dependencySources[i].ID == "util" {
			dependencySources[i].Source = strings.Replace(dependencySources[i].Source, "return value / divisor", "return value / divisor + 0", 1)
		}
	}
	dependencyChanged, diagnostics := sprigproof.CompileProject(dependencySources)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	dependencyPrepared, err := dependencyChanged.PrepareLinked([]sprigproof.PackageID{"counter"}, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if dependencyPrepared[0].Set().Digest() != prepared[0].Set().Digest() || dependencyPrepared[1].Set().Digest() != prepared[1].Set().Digest() || dependencyPrepared[2].Set().Digest() == prepared[2].Set().Digest() {
		t.Fatal("dependency source change was not localized to the linked leaf source")
	}
	writePreparedPackages(t, root, dependencyPrepared)
	dependencyBinary := nativeTestExecutable(root, "dependency-app")
	dependencyTrace := runCommand(t, root, env, "go", append(flags, "-o", dependencyBinary, "./cmd/app")...)
	assertCompileCounts(t, "linked dependency change", dependencyTrace, map[string]int{module + "/generated/counter": 1, module + "/generated/math": 1, module + "/generated/util": 1, "main": 1})
	dependencyIDs := packageBuildIDs(t, root, env)
	for _, path := range []string{module + "/generated/counter", module + "/generated/math", module + "/generated/util", module + "/cmd/app"} {
		if dependencyIDs[path] == baseIDs[path] {
			t.Fatalf("dependency change preserved %s BuildID: %v/%v", path, baseIDs, dependencyIDs)
		}
	}
	if output := strings.TrimSpace(runCommand(t, root, env, dependencyBinary)); output != "4|7|1|2|true|true" {
		t.Fatalf("linked dependency output=%q", output)
	}

	unrelatedSources := transitiveProjectSources()
	for i := range unrelatedSources {
		if unrelatedSources[i].ID == "spare" {
			unrelatedSources[i].Source = strings.Replace(unrelatedSources[i].Source, "return value", "return value + 0", 1)
		}
	}
	unrelatedChanged, diagnostics := sprigproof.CompileProject(unrelatedSources)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	unrelatedPrepared, err := unrelatedChanged.PrepareLinked([]sprigproof.PackageID{"counter"}, bindings)
	if err != nil || !reflect.DeepEqual(unrelatedPrepared, prepared) {
		t.Fatalf("unrelated package changed linked closure: %v", err)
	}
}

func TestLinkedLowercaseTargetBuildsThroughNominalWrapper(t *testing.T) {
	const module = "example.com/sprig-linked-lowercase"
	sources := sprigproof.ProofProjectSources()
	sources[0].Source = strings.Replace(sources[0].Source, "m.Divide", "m.divide", 1)
	sources[1].Source = strings.Replace(sources[1].Source, "func Divide", "func divide", 1)
	project, diagnostics := sprigproof.CompileProject(sources)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	prepared, err := project.PrepareLinked(
		[]sprigproof.PackageID{"counter"},
		[]sprigproof.PackageBinding{
			{ID: "counter", ImportPath: module + "/generated/counter"},
			{ID: "math", ImportPath: module + "/generated/math"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module "+module+"\n\ngo 1.26\n")
	writePreparedPackages(t, root, prepared)
	mustWrite(t, filepath.Join(root, "cmd/app/main.go"), `package main
import (
 "context"
 "fmt"
 counter "example.com/sprig-linked-lowercase/generated/counter"
)
func main(){ result,err:=counter.Reduce(context.Background(),[]counter.Reading{{Value:12,Divisor:3}},2);fmt.Printf("%d|%v",result.Total,err) }
`)
	env := replaceEnvironment(os.Environ(), []string{"CGO_ENABLED=0", "GOENV=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOCACHE=" + filepath.Join(root, "go-cache"), "GOMODCACHE=" + filepath.Join(root, "mod-cache")})
	if output := strings.TrimSpace(runCommand(t, root, env, "go", "run", "-mod=readonly", "./cmd/app")); output != "4|<nil>" {
		t.Fatalf("lowercase linked output=%q", output)
	}
}

func writePreparedPackages(t *testing.T, root string, packages []sprigproof.PreparedPackage) {
	t.Helper()
	mounts := make([]preparedsource.Mount, len(packages))
	for i, item := range packages {
		mounts[i] = preparedsource.Mount{Path: "generated/" + string(item.Package().ID()), Set: item.Set()}
	}
	layout, err := preparedsource.NewLayout(mounts)
	if err != nil {
		t.Fatal(err)
	}
	writeLayout(t, root, layout)
}

func writeLayout(t *testing.T, root string, layout preparedsource.Layout) {
	t.Helper()
	for _, mount := range layout.Mounts() {
		for _, file := range mount.Set.Files() {
			mustWrite(t, filepath.Join(root, filepath.FromSlash(mount.Path), file.Name), file.Content)
		}
	}
}

func nativeTestExecutable(root, name string) string {
	path := filepath.Join(root, name)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	return path
}

func assertCompileCounts(t *testing.T, phase, trace string, want map[string]int) {
	t.Helper()
	for pkg, n := range want {
		got := 0
		for _, line := range strings.Split(trace, "\n") {
			if isGoCompileTrace(line) && strings.Contains(line, " -p "+pkg+" ") {
				got++
			}
		}
		if got != n {
			t.Fatalf("%s compile count %s=%d want %d\n%s", phase, pkg, got, n, trace)
		}
	}
}

func isGoCompileTrace(line string) bool {
	output := strings.Index(line, " -o ")
	if output < 0 {
		return false
	}
	tool := strings.Trim(strings.TrimSpace(line[:output]), `"`)
	tool = strings.ReplaceAll(tool, `\`, "/")
	if slash := strings.LastIndexByte(tool, '/'); slash >= 0 {
		tool = tool[slash+1:]
	}
	return tool == "compile" || tool == "compile.exe"
}

func TestGoCompileTraceRecognizesNativeToolPaths(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"Unix", `/opt/go/pkg/tool/darwin_arm64/compile -o $WORK/b001/_pkg_.a -p main -pack main.go`, true},
		{"Windows", `"C:\hostedtoolcache\windows\go\1.26.5\x64\pkg\tool\windows_amd64\compile.exe" -o "$WORK\b001\_pkg_.a" -p main -pack main.go`, true},
		{"linker", `/opt/go/pkg/tool/darwin_arm64/link -o app -buildmode=exe`, false},
		{"compiler argument", `/usr/bin/env -o output -p compile`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isGoCompileTrace(test.line); got != test.want {
				t.Fatalf("isGoCompileTrace(%q) = %v, want %v", test.line, got, test.want)
			}
		})
	}
}

func packageBuildIDs(t *testing.T, root string, env []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	decoder := json.NewDecoder(strings.NewReader(runCommand(t, root, env, "go", "list", "-export", "-json", "./...")))
	for {
		var item struct {
			ImportPath string
			BuildID    string
		}
		if err := decoder.Decode(&item); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatal(err)
		}
		if item.ImportPath == "" || item.BuildID == "" {
			t.Fatalf("invalid package BuildID record: %#v", item)
		}
		out[item.ImportPath] = item.BuildID
	}
	return out
}

func fileDigest(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(content)
}

func TestAlternateGeneratedSourceExecutesNatively(t *testing.T) {
	source := strings.ReplaceAll(sprigproof.ProofSource, "let total = 0", "let total = 10")
	source = strings.ReplaceAll(source, "code: 7", "code: 9")
	source = strings.ReplaceAll(source, "readings", "items")
	source = strings.ReplaceAll(source, "reading", "item")
	source = strings.ReplaceAll(source, "total", "sum")
	source = strings.ReplaceAll(source, "rejected", "rejects")
	// Restore the fixed host-schema field names while retaining structural local
	// and parameter renaming in the accepted function body.
	source = strings.ReplaceAll(source, "Ok { sum i64 rejects []i64 }", "Ok { total i64 rejected []i64 }")
	source = strings.ReplaceAll(source, "Ok { sum:", "Ok { total:")
	source = strings.ReplaceAll(source, "rejects: rejects", "rejected: rejects")
	program, diagnostics := sprigproof.Compile(source)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	set, err := program.PreparedSource("generated")
	if err != nil {
		t.Fatal(err)
	}
	root, env := materializeApplication(t, set, `package main
import (
 "context"
 "fmt"
 "example.com/sprig-static/generated"
)
func main(){ result,err:=generated.Reduce(context.Background(),[]generated.Reading{{Value:6,Divisor:2},{Value:1,Divisor:0}},3);if err!=nil{panic(err)};fmt.Printf("%d|%d",result.Total,result.Rejected[0]) }
`)
	if output := strings.TrimSpace(runCommand(t, root, env, "go", "run", "./cmd/app")); output != "13|9" {
		t.Fatalf("alternate native output=%q", output)
	}
}

func TestDirectDivisionByZeroGeneratedAgreement(t *testing.T) {
	source := strings.Replace(sprigproof.ProofSource, "reading.divisor == 0", "reading.divisor == 99", 1)
	program, diagnostics := sprigproof.Compile(source)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	set, err := program.PreparedSource("generated")
	if err != nil {
		t.Fatal(err)
	}
	root, env := materializeApplication(t, set, `package main
import (
 "context"
 "fmt"
 "example.com/sprig-static/generated"
)
func main(){ result,err:=generated.Reduce(context.Background(),[]generated.Reading{{Value:1,Divisor:0}},2);if err!=nil{panic(err)};fmt.Printf("%d|%d",result.Tag,result.Code) }
`)
	if output := strings.TrimSpace(runCommand(t, root, env, "go", "run", "./cmd/app")); output != "2|2" {
		t.Fatalf("division-zero native output=%q", output)
	}
}

func TestExplicitErrVariantGeneratedAgreement(t *testing.T) {
	source := strings.Replace(sprigproof.ProofSource, "return Ok { total: total, rejected: rejected }", "return Err { code: 42 }", 1)
	program, diagnostics := sprigproof.Compile(source)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	evaluated, evalErr := program.Reduce(context.Background(), nil, 1)
	if evalErr != nil || evaluated.Tag != sprigproof.ResultError || evaluated.Code != 42 {
		t.Fatalf("evaluator Err=%#v,%v", evaluated, evalErr)
	}
	set, err := program.PreparedSource("generated")
	if err != nil {
		t.Fatal(err)
	}
	root, env := materializeApplication(t, set, `package main
import (
 "context"
 "fmt"
 "example.com/sprig-static/generated"
)
func main(){ result,err:=generated.Reduce(context.Background(),nil,1);if err!=nil{panic(err)};fmt.Printf("%d|%d",result.Tag,result.Code) }
`)
	if output := strings.TrimSpace(runCommand(t, root, env, "go", "run", "./cmd/app")); output != "2|42" {
		t.Fatalf("Err native output=%q", output)
	}
}

func TestGeneratedListLimitAgreement(t *testing.T) {
	line := "rejected = append(rejected, code)"
	source := strings.Replace(sprigproof.ProofSource, line, line+"\n        "+line, 1)
	program, diagnostics := sprigproof.Compile(source)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	input := make([]sprigproof.Reading, 2049)
	evaluated, evalErr := program.Reduce(context.Background(), input, 2050)
	if !errors.Is(evalErr, sprigproof.ErrListLimit) || evaluated.Tag != 0 || evaluated.Total != 0 || len(evaluated.Rejected) != 0 || evaluated.Code != 0 {
		t.Fatalf("evaluator list limit=%#v,%v", evaluated, evalErr)
	}
	set, err := program.PreparedSource("generated")
	if err != nil {
		t.Fatal(err)
	}
	root, env := materializeApplication(t, set, `package main
import (
 "context"
 "errors"
 "fmt"
 "example.com/sprig-static/generated"
)
func main(){ _,err:=generated.Reduce(context.Background(),make([]generated.Reading,2049),2050);if !errors.Is(err,generated.ErrListLimit){panic(err)};fmt.Print("list-limit") }
`)
	if output := strings.TrimSpace(runCommand(t, root, env, "go", "run", "./cmd/app")); output != "list-limit" {
		t.Fatalf("list-limit native output=%q", output)
	}
}

func TestGeneratedListsHaveValueSemantics(t *testing.T) {
	source := strings.Replace(sprigproof.ProofSource, "        rejected = append(rejected, code)", `        rejected = append(rejected, code)
        rejected = append(rejected, code)
        rejected = append(rejected, code)
        let alias = rejected
        alias = append(alias, code + 3)
        rejected = append(rejected, code + 4)
        rejected = alias`, 1)
	program, diagnostics := sprigproof.Compile(source)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	evaluated, evalErr := program.Reduce(context.Background(), []sprigproof.Reading{{Divisor: 0}}, 2)
	want := []int64{7, 7, 7, 10}
	if evalErr != nil || !slices.Equal(evaluated.Rejected, want) {
		t.Fatalf("evaluator alias result=%#v,%v\n%s", evaluated, evalErr, source)
	}
	set, err := program.PreparedSource("generated")
	if err != nil {
		t.Fatal(err)
	}
	root, env := materializeApplication(t, set, `package main
import (
 "context"
 "fmt"
 "example.com/sprig-static/generated"
)
func main(){ result,err:=generated.Reduce(context.Background(),[]generated.Reading{{Divisor:0}},2);if err!=nil{panic(err)};fmt.Printf("%v",result.Rejected) }
`)
	if output := strings.TrimSpace(runCommand(t, root, env, "go", "run", "./cmd/app")); output != "[7 7 7 10]" {
		t.Fatalf("native alias result=%q", output)
	}
}

func TestAppendValueErrorPrecedesListLimit(t *testing.T) {
	line := "rejected = append(rejected, code)"
	replacement := "rejected = append(rejected, reading.value + 9223372036854775807)\n        " + line
	source := strings.Replace(sprigproof.ProofSource, line, replacement, 1)
	program, diagnostics := sprigproof.Compile(source)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	input := make([]sprigproof.Reading, 2049)
	input[len(input)-1].Value = 1
	evaluated, evalErr := program.Reduce(context.Background(), input, 2050)
	if evalErr != nil || evaluated.Tag != sprigproof.ResultError || evaluated.Code != sprigproof.DomainOverflow {
		t.Fatalf("evaluator competing error=%#v,%v", evaluated, evalErr)
	}
	set, err := program.PreparedSource("generated")
	if err != nil {
		t.Fatal(err)
	}
	root, env := materializeApplication(t, set, `package main
import (
 "context"
 "fmt"
 "example.com/sprig-static/generated"
)
func main(){ input:=make([]generated.Reading,2049);input[len(input)-1].Value=1;result,err:=generated.Reduce(context.Background(),input,2050);if err!=nil{panic(err)};fmt.Printf("%d|%d",result.Tag,result.Code) }
`)
	if output := strings.TrimSpace(runCommand(t, root, env, "go", "run", "./cmd/app")); output != "2|1" {
		t.Fatalf("competing-error native output=%q", output)
	}
}

func materializeApplication(t *testing.T, set preparedsource.Set, mainSource string) (string, []string) {
	t.Helper()
	root := t.TempDir()
	if set.PackageName() != "generated" {
		t.Fatalf("package=%q", set.PackageName())
	}
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/sprig-static\n\ngo 1.26\n")
	for _, file := range set.Files() {
		mustWrite(t, filepath.Join(root, "generated", file.Name), file.Content)
	}
	mustWrite(t, filepath.Join(root, "cmd", "app", "main.go"), mainSource)
	env := replaceEnvironment(os.Environ(), []string{"CGO_ENABLED=0", "GOENV=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOCACHE=" + filepath.Join(root, "go-cache"), "GOMODCACHE=" + filepath.Join(root, "mod-cache")})
	return root, env
}

func replaceEnvironment(base, overrides []string) []string {
	keys := make(map[string]struct{}, len(overrides))
	for _, item := range overrides {
		key, _, _ := strings.Cut(item, "=")
		keys[key] = struct{}{}
	}
	out := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if _, replaced := keys[key]; !replaced {
			out = append(out, item)
		}
	}
	return append(out, overrides...)
}
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
func runCommand(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	command.Env = env
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s timed out", command)
	}
	if err != nil {
		t.Fatalf("%s: %v\n%s", command, err, output)
	}
	return string(output)
}
func commandLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
func assertNoToolingNames(t *testing.T, observer, output string) {
	t.Helper()
	lower := strings.ToLower(output)
	for _, forbidden := range []string{"github.com/besmpl/ember", "sprigproof", "preparedsource", "preparedworker", ".registry", ".codec"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("%s retained %q", observer, forbidden)
		}
	}
}
func parseImports(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	imports := make([]string, len(file.Imports))
	for i, item := range file.Imports {
		imports[i] = strings.Trim(item.Path.Value, "\"")
	}
	sort.Strings(imports)
	return imports
}
func assertExactApplicationFiles(t *testing.T, root string, want []string) {
	t.Helper()
	var got []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == "go-cache" || entry.Name() == "mod-cache") {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		got = append(got, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("application files=%v want %v", got, want)
	}
}
