// Package preparedsource defines immutable, canonical generated-source values.
//
// The package validates and canonicalizes source entirely in memory. It does
// not materialize files, invoke a toolchain, or otherwise perform effects.
package preparedsource

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/scanner"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

const (
	maximumFileCount       = 256
	maximumMountCount      = 64
	maximumFileBytes       = 32 << 20
	maximumSetBytes        = 128 << 20
	maximumPackageBytes    = 64
	maximumBasenameBytes   = 64
	maximumMountBytes      = 240
	maximumMountComponents = 32
	maximumMountedLeaves   = 4096
	maximumMountedBytes    = 1 << 30
	maximumGoTokens        = 4_000_000
	maximumDelimiterDepth  = 1024
)

const (
	setDigestDomain    = "github.com/besmpl/ember/preparedsource:Set:v1"
	layoutDigestDomain = "github.com/besmpl/ember/preparedsource:Layout:v1"
)

// FileKind identifies how a generated package consumes a file.
type FileKind uint8

const (
	// GoFile is a generated Go source file.
	GoFile FileKind = iota + 1
	// AssetFile is inert data owned by exactly one package-level //go:embed
	// declaration in the same Set.
	AssetFile
)

// File is raw input to NewSet. Name is a canonical lowercase-ASCII basename,
// not a path, and Content is an immutable byte string. GoFile names match
// `[a-z][a-z0-9_]*.go`. AssetFile names begin and end with a lowercase ASCII
// letter or digit and additionally allow '.', '_', and '-' internally.
type File struct {
	Name    string
	Kind    FileKind
	Content string
}

// Set is one immutable generated contribution to exactly one Go package. A
// nonzero Set can only be constructed by NewSet and is safe to copy and read
// concurrently.
type Set struct {
	data *setData
}

type setData struct {
	packageName string
	files       []File
	totalBytes  uint64
	digest      [sha256.Size]byte
}

// NewSet validates, copies, and canonically orders one generated package
// contribution. Package names match `[a-z][a-z0-9_]*`, are not Go keywords, and
// contain at most 64 bytes. File basenames contain at most 64 bytes. Limits are
// fixed at 256 files, 32 MiB per file, and 128 MiB in aggregate.
func NewSet(packageName string, files []File) (Set, error) {
	if !validPackageName(packageName) {
		return Set{}, fmt.Errorf("prepared source: invalid package name %q", packageName)
	}
	if len(files) == 0 {
		return Set{}, fmt.Errorf("prepared source: set is empty")
	}
	if len(files) > maximumFileCount {
		return Set{}, fmt.Errorf("prepared source: file count %d exceeds limit %d", len(files), maximumFileCount)
	}

	canonical := append([]File(nil), files...)
	for index, file := range canonical {
		if len(file.Name) > maximumBasenameBytes {
			return Set{}, fmt.Errorf("prepared source: file %d basename exceeds limit %d", index, maximumBasenameBytes)
		}
	}
	sort.Slice(canonical, func(left, right int) bool {
		return canonical[left].Name < canonical[right].Name
	})
	seenNames := make(map[string]struct{}, len(canonical))
	totalBytes := uint64(0)
	goFiles := 0
	assets := make(map[string]struct{}, len(canonical))
	for index := range canonical {
		file := canonical[index]
		if _, exists := seenNames[file.Name]; exists {
			return Set{}, fmt.Errorf("prepared source: duplicate file name %q", file.Name)
		}
		seenNames[file.Name] = struct{}{}
		var err error
		totalBytes, err = addContentBytes(totalBytes, file.Name, len(file.Content))
		if err != nil {
			return Set{}, err
		}

		switch file.Kind {
		case GoFile:
			if err := validateGoBasename(file.Name); err != nil {
				return Set{}, fmt.Errorf("prepared source: file %d: %w", index, err)
			}
			goFiles++
		case AssetFile:
			if err := validateAssetBasename(file.Name); err != nil {
				return Set{}, fmt.Errorf("prepared source: file %d: %w", index, err)
			}
			assets[file.Name] = struct{}{}
		default:
			return Set{}, fmt.Errorf("prepared source: file %q has invalid kind %d", file.Name, file.Kind)
		}
	}
	if goFiles == 0 {
		return Set{}, fmt.Errorf("prepared source: set has no Go file")
	}

	validation := sourceValidation{assetReferences: make(map[string]uint64, len(assets))}
	for _, file := range canonical {
		if file.Kind != GoFile {
			continue
		}
		if err := validateGoSource(packageName, file, assets, &validation); err != nil {
			return Set{}, err
		}
	}
	if len(assets) == 0 {
		if validation.embedImports != 0 {
			return Set{}, fmt.Errorf("prepared source: blank embed import requires an asset")
		}
	} else if validation.embedImports != 1 {
		return Set{}, fmt.Errorf("prepared source: assets require exactly one blank embed import, found %d", validation.embedImports)
	}
	for _, file := range canonical {
		if file.Kind != AssetFile {
			continue
		}
		switch validation.assetReferences[file.Name] {
		case 0:
			return Set{}, fmt.Errorf("prepared source: asset %q is not referenced", file.Name)
		case 1:
		default:
			return Set{}, fmt.Errorf("prepared source: asset %q is referenced more than once", file.Name)
		}
	}

	data := &setData{packageName: packageName, files: canonical, totalBytes: totalBytes}
	data.digest = digestSet(packageName, canonical)
	return Set{data: data}, nil
}

// PackageName returns the generated Go package name, or "" for a zero Set.
func (set Set) PackageName() string {
	if set.data == nil {
		return ""
	}
	return set.data.packageName
}

// Digest returns the domain-separated, length-framed SHA-256 content identity,
// or the zero digest for a zero Set.
func (set Set) Digest() [sha256.Size]byte {
	if set.data == nil {
		return [sha256.Size]byte{}
	}
	return set.data.digest
}

// Files returns a detached copy in canonical basename order. It returns nil
// for a zero Set.
func (set Set) Files() []File {
	if set.data == nil {
		return nil
	}
	return append([]File(nil), set.data.files...)
}

// IsZero reports whether set is the invalid zero value.
func (set Set) IsZero() bool {
	return set.data == nil
}

// Mount is raw application placement input. Path is a nonempty canonical
// lowercase-ASCII module-relative package directory. Its slash-separated
// components begin and end with a lowercase ASCII letter or digit, allow '_'
// and '-' internally, and contain at most 64 bytes. Set must be nonzero.
type Mount struct {
	Path string
	Set  Set
}

// Layout is an immutable canonical application placement of generated Sets. A
// nonzero Layout can only be constructed by NewLayout and is safe to copy and
// read concurrently.
type Layout struct {
	data *layoutData
}

type layoutData struct {
	mounts []Mount
	digest [sha256.Size]byte
}

// NewLayout validates, copies, and canonically orders application mounts. A
// layout contains at most 64 mounts, 4,096 repeated mounted leaves, and 1 GiB
// of repeated mounted content.
func NewLayout(mounts []Mount) (Layout, error) {
	if len(mounts) == 0 {
		return Layout{}, fmt.Errorf("prepared source: layout is empty")
	}
	if len(mounts) > maximumMountCount {
		return Layout{}, fmt.Errorf("prepared source: mount count %d exceeds limit %d", len(mounts), maximumMountCount)
	}
	canonical := append([]Mount(nil), mounts...)
	for index, mount := range canonical {
		if len(mount.Path) > maximumMountBytes {
			return Layout{}, fmt.Errorf("prepared source: mount %d path exceeds limit %d", index, maximumMountBytes)
		}
	}
	sort.Slice(canonical, func(left, right int) bool {
		return canonical[left].Path < canonical[right].Path
	})
	seenPaths := make(map[string]struct{}, len(canonical))
	var mountedLeaves, mountedBytes uint64
	for index, mount := range canonical {
		if mount.Set.IsZero() {
			return Layout{}, fmt.Errorf("prepared source: mount %d has a zero Set", index)
		}
		if err := validateMountPath(mount.Path); err != nil {
			return Layout{}, fmt.Errorf("prepared source: mount %d: %w", index, err)
		}
		if _, exists := seenPaths[mount.Path]; exists {
			return Layout{}, fmt.Errorf("prepared source: duplicate mount path %q", mount.Path)
		}
		seenPaths[mount.Path] = struct{}{}
		var err error
		mountedLeaves, mountedBytes, err = addMountedTotals(
			mountedLeaves,
			mountedBytes,
			uint64(len(mount.Set.data.files)),
			mount.Set.data.totalBytes,
		)
		if err != nil {
			return Layout{}, err
		}
	}
	data := &layoutData{mounts: canonical}
	data.digest = digestLayout(canonical)
	return Layout{data: data}, nil
}

// Digest returns the domain-separated, length-framed SHA-256 placement
// identity, or the zero digest for a zero Layout.
func (layout Layout) Digest() [sha256.Size]byte {
	if layout.data == nil {
		return [sha256.Size]byte{}
	}
	return layout.data.digest
}

// Mounts returns a detached copy in canonical path order. It returns nil for a
// zero Layout. Each returned Set remains immutable.
func (layout Layout) Mounts() []Mount {
	if layout.data == nil {
		return nil
	}
	return append([]Mount(nil), layout.data.mounts...)
}

// IsZero reports whether layout is the invalid zero value.
func (layout Layout) IsZero() bool {
	return layout.data == nil
}

func validPackageName(name string) bool {
	if len(name) == 0 || len(name) > maximumPackageBytes || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for index := 1; index < len(name); index++ {
		character := name[index]
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return !token.Lookup(name).IsKeyword()
}

func validateGoBasename(name string) error {
	if len(name) == 0 || len(name) > maximumBasenameBytes || !strings.HasSuffix(name, ".go") {
		return fmt.Errorf("invalid Go basename %q", name)
	}
	stem := strings.TrimSuffix(name, ".go")
	if !validLowerIdentifier(stem) {
		return fmt.Errorf("invalid Go basename %q", name)
	}
	if strings.HasSuffix(name, "_test.go") {
		return fmt.Errorf("test Go basename %q is not allowed", name)
	}
	if targetSelectingGoName(name) {
		return fmt.Errorf("target-selecting Go basename %q is not allowed", name)
	}
	if windowsReservedName(stem) {
		return fmt.Errorf("reserved Go basename %q", name)
	}
	return nil
}

func validLowerIdentifier(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func validateAssetBasename(name string) error {
	if len(name) == 0 || len(name) > maximumBasenameBytes || !asciiLowerDigit(name[0]) || !asciiLowerDigit(name[len(name)-1]) {
		return fmt.Errorf("invalid asset basename %q", name)
	}
	for index := 1; index < len(name)-1; index++ {
		character := name[index]
		if !asciiLowerDigit(character) && character != '.' && character != '_' && character != '-' {
			return fmt.Errorf("invalid asset basename %q", name)
		}
	}
	stem := name
	if dot := strings.IndexByte(stem, '.'); dot >= 0 {
		stem = stem[:dot]
	}
	if name == "go.mod" || name == "go.sum" || foreignOrBuildSourceName(name) || windowsReservedName(stem) {
		return fmt.Errorf("asset basename %q names source, module, or prebuilt input", name)
	}
	return nil
}

func asciiLowerDigit(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
}

func validateMountPath(mountPath string) error {
	if len(mountPath) == 0 || len(mountPath) > maximumMountBytes {
		return fmt.Errorf("invalid mount path %q", mountPath)
	}
	components := strings.Split(mountPath, "/")
	if len(components) > maximumMountComponents {
		return fmt.Errorf("mount path %q has %d components, exceeds limit %d", mountPath, len(components), maximumMountComponents)
	}
	for _, component := range components {
		if err := validateMountComponent(component); err != nil {
			return fmt.Errorf("invalid mount path %q: %w", mountPath, err)
		}
	}
	return nil
}

func validateMountComponent(component string) error {
	if len(component) == 0 || len(component) > maximumBasenameBytes || !asciiLowerDigit(component[0]) || !asciiLowerDigit(component[len(component)-1]) {
		return fmt.Errorf("invalid component %q", component)
	}
	for index := 1; index < len(component)-1; index++ {
		character := component[index]
		if !asciiLowerDigit(character) && character != '_' && character != '-' {
			return fmt.Errorf("invalid component %q", component)
		}
	}
	if windowsReservedName(component) {
		return fmt.Errorf("reserved component %q", component)
	}
	return nil
}

func windowsReservedName(name string) bool {
	switch name {
	case "con", "prn", "aux", "nul", "conin$", "conout$":
		return true
	}
	if len(name) == 4 && name[3] >= '1' && name[3] <= '9' {
		return name[:3] == "com" || name[:3] == "lpt"
	}
	return false
}

func addContentBytes(total uint64, name string, size int) (uint64, error) {
	if size < 0 || uint64(size) > maximumFileBytes {
		return total, fmt.Errorf("prepared source: file %q is %d bytes, exceeds limit %d", name, size, maximumFileBytes)
	}
	addition := uint64(size)
	if total > maximumSetBytes || addition > maximumSetBytes-total {
		return total, fmt.Errorf("prepared source: aggregate content exceeds limit %d", maximumSetBytes)
	}
	return total + addition, nil
}

func addMountedTotals(leaves, bytes, addedLeaves, addedBytes uint64) (uint64, uint64, error) {
	if leaves > maximumMountedLeaves || addedLeaves > maximumMountedLeaves-leaves {
		return leaves, bytes, fmt.Errorf("prepared source: mounted leaf count exceeds limit %d", maximumMountedLeaves)
	}
	if bytes > maximumMountedBytes || addedBytes > maximumMountedBytes-bytes {
		return leaves, bytes, fmt.Errorf("prepared source: mounted content exceeds limit %d", maximumMountedBytes)
	}
	return leaves + addedLeaves, bytes + addedBytes, nil
}

func targetSelectingGoName(name string) bool {
	stem := strings.TrimSuffix(name, ".go")
	parts := strings.Split(stem, "_")
	if len(parts) < 2 {
		return false
	}
	last := parts[len(parts)-1]
	return knownGOOS(last) || knownGOARCH(last)
}

func knownGOOS(value string) bool {
	switch value {
	case "aix", "android", "darwin", "dragonfly", "freebsd", "hurd",
		"illumos", "ios", "js", "linux", "netbsd", "openbsd", "plan9",
		"solaris", "wasip1", "windows", "zos":
		return true
	default:
		return false
	}
}

func knownGOARCH(value string) bool {
	switch value {
	case "386", "amd64", "amd64p32", "arm", "arm64", "loong64", "mips",
		"mips64", "mips64le", "mipsle", "ppc64", "ppc64le", "riscv64",
		"s390x", "sparc64", "wasm":
		return true
	default:
		return false
	}
}

func foreignOrBuildSourceName(name string) bool {
	for _, suffix := range []string{
		".a", ".c", ".cc", ".cpp", ".cxx", ".dll", ".dylib", ".exe",
		".f", ".f90", ".for", ".go", ".h", ".hh", ".hpp", ".hxx",
		".lib", ".m", ".mm", ".o", ".obj", ".pgo", ".s", ".so", ".swig",
		".swigcxx", ".syso",
	} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

type sourceValidation struct {
	embedImports    int
	assetReferences map[string]uint64
}

func validateGoSource(packageName string, file File, assets map[string]struct{}, validation *sourceValidation) error {
	if err := scanGoSource(file.Name, file.Content, maximumGoTokens, maximumDelimiterDepth); err != nil {
		return err
	}
	parsed, err := parser.ParseFile(
		token.NewFileSet(),
		file.Name,
		file.Content,
		parser.AllErrors|parser.ParseComments|parser.SkipObjectResolution,
	)
	if err != nil {
		return fmt.Errorf("prepared source: parse Go file %q: %w", file.Name, err)
	}
	if parsed.Name == nil || parsed.Name.Name != packageName {
		actual := ""
		if parsed.Name != nil {
			actual = parsed.Name.Name
		}
		return fmt.Errorf("prepared source: Go file %q declares package %q, want %q", file.Name, actual, packageName)
	}

	hasBlankEmbedImport := false
	for _, imported := range parsed.Imports {
		importPath, err := strconv.Unquote(imported.Path.Value)
		if err != nil || !validImportPath(importPath) {
			return fmt.Errorf("prepared source: Go file %q has invalid import path", file.Name)
		}
		if importPath == "C" {
			return fmt.Errorf("prepared source: Go file %q imports C", file.Name)
		}
		if importPath == "embed" {
			if imported.Path.Value != `"embed"` || imported.Name == nil || imported.Name.Name != "_" {
				return fmt.Errorf("prepared source: Go file %q must import embed with blank identifier", file.Name)
			}
			validation.embedImports++
			hasBlankEmbedImport = true
		}
	}

	attached := make(map[*ast.Comment]struct{})
	for _, declaration := range parsed.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if declaration.Recv == nil && declaration.Name != nil && declaration.Name.Name == "init" {
				return fmt.Errorf("prepared source: Go file %q declares init", file.Name)
			}
		case *ast.GenDecl:
			if declaration.Tok != token.VAR || declaration.Doc == nil {
				continue
			}
			directives := embedComments(declaration.Doc)
			if len(directives) == 0 {
				continue
			}
			if len(directives) != 1 || len(declaration.Specs) != 1 {
				return fmt.Errorf("prepared source: Go file %q embed declaration must own one directive and one variable", file.Name)
			}
			value, ok := declaration.Specs[0].(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name == "_" || len(value.Values) != 0 || !explicitStringType(value.Type) {
				return fmt.Errorf("prepared source: Go file %q embed declaration must name one explicit uninitialized string", file.Name)
			}
			asset, ok := exactEmbedAsset(directives[0].Text)
			if !ok {
				return fmt.Errorf("prepared source: Go file %q contains malformed embed directive %q", file.Name, directives[0].Text)
			}
			if _, exists := assets[asset]; !exists {
				return fmt.Errorf("prepared source: Go file %q embeds undeclared asset %q", file.Name, asset)
			}
			if validation.assetReferences[asset] == ^uint64(0) {
				return fmt.Errorf("prepared source: asset %q reference count overflows", asset)
			}
			validation.assetReferences[asset]++
			attached[directives[0]] = struct{}{}
		}
	}

	for _, group := range parsed.Comments {
		for _, comment := range group.List {
			text := comment.Text
			if constraint.IsGoBuild(text) || constraint.IsPlusBuild(text) {
				return fmt.Errorf("prepared source: Go file %q contains a build constraint", file.Name)
			}
			if lineDirective(text) {
				return fmt.Errorf("prepared source: Go file %q contains a line directive", file.Name)
			}
			if strings.HasPrefix(text, "//go:") {
				if _, ok := exactEmbedAsset(text); !ok {
					return fmt.Errorf("prepared source: Go file %q contains unsupported directive %q", file.Name, text)
				}
				if _, ok := attached[comment]; !ok {
					return fmt.Errorf("prepared source: Go file %q contains unattached embed directive", file.Name)
				}
			}
		}
	}
	if len(attached) != 0 && !hasBlankEmbedImport {
		return fmt.Errorf("prepared source: Go file %q with embed directives requires its blank embed import", file.Name)
	}
	return nil
}

func scanGoSource(name, source string, tokenLimit, depthLimit int) error {
	fileSet := token.NewFileSet()
	file := fileSet.AddFile(name, -1, len(source))
	firstScanError := ""
	var lexer scanner.Scanner
	lexer.Init(file, []byte(source), func(_ token.Position, message string) {
		if firstScanError == "" {
			firstScanError = message
		}
	}, scanner.ScanComments)
	initialDelimiterCapacity := depthLimit
	if initialDelimiterCapacity < 0 {
		initialDelimiterCapacity = 0
	}
	if initialDelimiterCapacity > 32 {
		initialDelimiterCapacity = 32
	}
	delimiters := make([]token.Token, 0, initialDelimiterCapacity)
	for count := 1; ; count++ {
		_, scanned, _ := lexer.Scan()
		if firstScanError != "" {
			return fmt.Errorf("prepared source: scan Go file %q: %s", name, firstScanError)
		}
		if count > tokenLimit {
			return fmt.Errorf("prepared source: Go file %q exceeds token limit %d", name, tokenLimit)
		}
		switch scanned {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			delimiters = append(delimiters, scanned)
			if len(delimiters) > depthLimit {
				return fmt.Errorf("prepared source: Go file %q exceeds delimiter depth %d", name, depthLimit)
			}
		case token.RPAREN, token.RBRACK, token.RBRACE:
			if len(delimiters) == 0 || !matchingDelimiter(delimiters[len(delimiters)-1], scanned) {
				return fmt.Errorf("prepared source: Go file %q has mismatched delimiter", name)
			}
			delimiters = delimiters[:len(delimiters)-1]
		}
		if scanned == token.EOF {
			if len(delimiters) != 0 {
				return fmt.Errorf("prepared source: Go file %q has unclosed delimiter", name)
			}
			return nil
		}
	}
}

func matchingDelimiter(open, close token.Token) bool {
	return open == token.LPAREN && close == token.RPAREN ||
		open == token.LBRACK && close == token.RBRACK ||
		open == token.LBRACE && close == token.RBRACE
}

func validImportPath(importPath string) bool {
	if importPath == "" || strings.HasPrefix(importPath, "/") || strings.HasSuffix(importPath, "/") {
		return false
	}
	for index := 0; index < len(importPath); index++ {
		character := importPath[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '/' || character == '.' ||
			character == '_' || character == '-' {
			continue
		}
		return false
	}
	for _, component := range strings.Split(importPath, "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

func embedComments(group *ast.CommentGroup) []*ast.Comment {
	if group == nil {
		return nil
	}
	var comments []*ast.Comment
	for _, comment := range group.List {
		if strings.HasPrefix(comment.Text, "//go:embed") {
			comments = append(comments, comment)
		}
	}
	return comments
}

func explicitStringType(expression ast.Expr) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == "string"
}

func exactEmbedAsset(directive string) (string, bool) {
	const prefix = "//go:embed "
	if !strings.HasPrefix(directive, prefix) {
		return "", false
	}
	asset := strings.TrimPrefix(directive, prefix)
	if err := validateAssetBasename(asset); err != nil {
		return "", false
	}
	return asset, true
}

func lineDirective(comment string) bool {
	return strings.HasPrefix(comment, "//line ") || strings.HasPrefix(comment, "//line\t") ||
		strings.HasPrefix(comment, "/*line ") || strings.HasPrefix(comment, "/*line\t")
}

func digestSet(packageName string, files []File) [sha256.Size]byte {
	hasher := sha256.New()
	writeDigestFrame(hasher, setDigestDomain)
	writeDigestFrame(hasher, packageName)
	writeDigestUint64(hasher, uint64(len(files)))
	for _, file := range files {
		_, _ = hasher.Write([]byte{byte(file.Kind)})
		writeDigestFrame(hasher, file.Name)
		writeDigestFrame(hasher, file.Content)
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

func digestLayout(mounts []Mount) [sha256.Size]byte {
	hasher := sha256.New()
	writeDigestFrame(hasher, layoutDigestDomain)
	writeDigestUint64(hasher, uint64(len(mounts)))
	for _, mount := range mounts {
		writeDigestFrame(hasher, mount.Path)
		digest := mount.Set.Digest()
		_, _ = hasher.Write(digest[:])
	}
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest
}

type digestWriter interface {
	Write([]byte) (int, error)
}

func writeDigestFrame(writer digestWriter, value string) {
	writeDigestUint64(writer, uint64(len(value)))
	_, _ = writer.Write([]byte(value))
}

func writeDigestUint64(writer digestWriter, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = writer.Write(encoded[:])
}
