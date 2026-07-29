// Command appmaterialize is temporary application tooling. It knows the
// application module and entry point, but no language registry or generated
// semantics. Generated delivery is copied exclusively through Layout.Mounts
// and Set.Files, whose order and collision policy are already canonical.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	producer "example.com/ember-external-producer"
	"github.com/besmpl/ember/preparedsource"
)

const applicationMain = `package main

import (
	"fmt"

	"example.com/generated-application/generated/ruby"
	"example.com/generated-application/generated/sprig"
)

func result() string {
	owner := rubyshape.NewOwner()
	detached := owner.Allocate("Widget").Detach()
	composed := sprigshape.Compose(sprigshape.Increment, sprigshape.Double)
	value, err := composed(20)
	if err != nil { panic(err) }
	return fmt.Sprintf("shape-only|%d:%s|%d", detached.ID, detached.Class, value)
}

func main() { fmt.Println(result()) }
`

const applicationTest = `package main

import (
	"strings"
	"testing"

	"example.com/generated-application/generated/ruby"
	"example.com/generated-application/generated/sprig"
)

func TestGeneratedShapes(t *testing.T) {
	if got := result(); got != "shape-only|1:Widget|42" { t.Fatalf("result = %q", got) }
	if !strings.Contains(rubyshape.Scope, "no Ruby syntax or semantic compatibility") { t.Fatal(rubyshape.Scope) }
	if !strings.Contains(sprigshape.Scope, "no Sprig syntax or semantic compatibility") { t.Fatal(sprigshape.Scope) }
}
`

func main() {
	out := flag.String("out", "", "application directory to materialize")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "-out is required")
		os.Exit(2)
	}
	rubySet, sprigSet, err := producer.Sets(false)
	if err != nil {
		fatal(err)
	}
	layout, err := preparedsource.NewLayout([]preparedsource.Mount{
		{Path: "generated/ruby", Set: rubySet},
		{Path: "generated/sprig", Set: sprigSet},
	})
	if err != nil {
		fatal(err)
	}
	if err := os.Mkdir(*out, 0o755); err != nil {
		fatal(err)
	}
	for _, mount := range layout.Mounts() {
		directory := filepath.Join(*out, filepath.FromSlash(mount.Path))
		if err := os.MkdirAll(directory, 0o755); err != nil {
			fatal(err)
		}
		for _, file := range mount.Set.Files() {
			write(filepath.Join(directory, file.Name), file.Content)
		}
	}
	if err := os.MkdirAll(filepath.Join(*out, "cmd", "app"), 0o755); err != nil {
		fatal(err)
	}
	write(filepath.Join(*out, "go.mod"), "module example.com/generated-application\n\ngo 1.26\n")
	write(filepath.Join(*out, "cmd", "app", "main.go"), applicationMain)
	write(filepath.Join(*out, "cmd", "app", "main_test.go"), applicationTest)
	fmt.Printf("ruby=%x\nsprig=%x\nlayout=%x\n", layout.Mounts()[0].Set.Digest(), layout.Mounts()[1].Set.Digest(), layout.Digest())
}

func write(path, content string) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		fatal(err)
	}
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		fatal(err)
	}
	if err := file.Close(); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
