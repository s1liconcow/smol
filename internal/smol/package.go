package smol

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// DiscoverImports extends the module table without changing existing bindings.
func DiscoverImports(files map[string]string, imports map[string]Import) error {
	standard()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		text := files[name]
		s, e := parse(text)
		if e != nil {
			return e
		}
		for _, im := range s.file.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			if stdPaths[p] {
				continue
			}
			n := packageName(p)
			alias := false
			if im.Name != nil {
				n = im.Name.Name
				alias = true
			}
			if n == "_" || n == "." || p == "C" {
				continue
			}
			if _, ok := imports[n]; !ok {
				imports[n] = Import{Path: p, Alias: alias}
			}
		}
	}
	return nil
}

// Prepare exposes the syntax-only pass for package-wide declaration resolution.
func Prepare(text string) (string, error) {
	prefix := "__smol_internal_"
	for strings.Contains(text, prefix) {
		prefix += "x"
	}
	text, _, err := Unpack(text, "")
	if err != nil {
		return "", err
	}
	text, _, _, err = desugar(text, prefix)
	return text, err
}

// desugar runs the syntax-only phases and returns the opened packages.
func desugar(text, prefix string) (string, []string, map[string]string, error) {
	text, uses, conv, err := splitHeader(text)
	if err != nil {
		return "", nil, nil, err
	}
	for _, phase := range []func(string) (string, error){func(t string) (string, error) { return lowerImportMarker(t, prefix) },
		func(t string) (string, error) { return lowerReceivers(t, conv) }, lowerDecls, lowerCommas,
		func(t string) (string, error) { return lowerSignatures(t, conv) }, lowerTags, lowerRanges, lowerArrows,
		func(t string) (string, error) { return lowerStatements(t, prefix) }} {
		text, err = phase(text)
		if err != nil {
			return "", nil, nil, err
		}
	}
	return text, uses, conv, nil
}

// Check verifies L1–L4 and formatter idempotence without writing source files.
func Check(text string, o Options) error {
	n, e := Normalize(text)
	if e != nil {
		return e
	}
	s, e := Lift(n, o)
	if e != nil {
		return e
	}
	g, e := Lower(s, o)
	if e != nil {
		return fmt.Errorf("L1 lower: %w", e)
	}
	if n != g {
		return fmt.Errorf("L1 differs at %s", difference(n, g))
	}
	f, e := Format(s, o)
	if e != nil {
		return fmt.Errorf("L2 format: %w", e)
	}
	if f != s {
		return fmt.Errorf("L2 differs at %s", difference(s, f))
	}
	f2, e := Format(f, o)
	if e != nil {
		return e
	}
	if f2 != f {
		return fmt.Errorf("L3 differs at %s", difference(f, f2))
	}
	// The same laws for the two-channel form that tools write to disk.
	code, notes := Pack(s)
	wide, warnings, e := Unpack(code, notes)
	if e != nil || len(warnings) > 0 {
		return fmt.Errorf("unpack: %v %v", e, warnings)
	}
	g, e = Lower(wide, o)
	if e != nil {
		return fmt.Errorf("L1 lower (packed): %w", e)
	}
	if n != g {
		return fmt.Errorf("L1 (packed) differs at %s", difference(n, g))
	}
	if c2, n2 := Pack(wide); c2 != code || n2 != notes {
		return fmt.Errorf("L3 (packed): packing is not stable")
	}
	bare, e := Lower(code, o)
	if e != nil {
		return fmt.Errorf("L4 lower: %w", e)
	}
	want, e := programTokens(n)
	if e != nil {
		return e
	}
	got, e := programTokens(bare)
	if e != nil {
		return e
	}
	if want != got {
		return fmt.Errorf("L4 Go tokens/directives differ at %s", difference(want, got))
	}
	return nil
}

// programTokens uses Go's scanner, retaining toolchain directives and cgo
// preambles while ignoring only ordinary comments and their layout.
func programTokens(text string) (string, error) {
	s, err := parse(text)
	if err != nil {
		return "", err
	}
	preamble := map[int]bool{}
	for _, d := range s.file.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			for _, sp := range g.Specs {
				im := sp.(*ast.ImportSpec)
				if im.Path.Value == `"C"` {
					for _, cg := range []*ast.CommentGroup{g.Doc, im.Doc} {
						if cg != nil {
							for _, c := range cg.List {
								preamble[s.off(c.Pos())] = true
							}
						}
					}
				}
			}
		}
	}
	var b strings.Builder
	for _, t := range lex(text) {
		if t.k == token.COMMENT && !directiveAt(text, t.a, t.lit) && !preamble[t.a] {
			continue
		}
		fmt.Fprintf(&b, "%s %q\n", t.k, t.lit)
	}
	return b.String(), nil
}
func difference(a, b string) string {
	line := 1
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return fmt.Sprintf("line %d: want %q, got %q", line, excerpt(a[i:]), excerpt(b[i:]))
		}
		if a[i] == '\n' {
			line++
		}
	}
	return fmt.Sprintf("line %d (length %d vs %d)", line, len(a), len(b))
}
func excerpt(t string) string {
	if len(t) > 120 {
		return t[:120]
	}
	return t
}

// DeclaredNames is useful to consumers building package import tables.
func DeclaredNames(text string) []string {
	s, e := parse(text)
	if e != nil {
		return nil
	}
	var names []string
	for _, d := range s.file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Recv == nil {
			names = append(names, f.Name.Name)
		}
	}
	return names
}

// PackageContext caches declaration-only information shared by file translations.
type PackageContext struct {
	packages map[string]*PackageContext
	names    map[string]bool
	types    map[string]ast.Expr
	methods  map[string]map[string]bool
}

func NewPackageContext(files map[string]string) *PackageContext {
	root := &PackageContext{packages: map[string]*PackageContext{}}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s, e := parse(files[k])
		if e != nil {
			continue
		}
		c := root.packages[s.file.Name.Name]
		if c == nil {
			c = &PackageContext{names: map[string]bool{}, types: map[string]ast.Expr{}}
			root.packages[s.file.Name.Name] = c
		}
		for _, d := range s.file.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					c.names[d.Name.Name] = true
				} else if len(d.Recv.List) == 1 {
					if c.methods == nil {
						c.methods = map[string]map[string]bool{}
					}
					base := recvBase(d.Recv.List[0].Type)
					if c.methods[base] == nil {
						c.methods[base] = map[string]bool{}
					}
					c.methods[base][d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.TypeSpec:
						c.names[sp.Name.Name] = true
						c.types[sp.Name.Name] = sp.Type
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							c.names[n.Name] = true
						}
					}
				}
			}
		}
	}
	return root
}

func (c *PackageContext) forPackage(name string) *PackageContext {
	if c.packages == nil {
		return c
	}
	if p := c.packages[name]; p != nil {
		return p
	}
	return &PackageContext{names: map[string]bool{}, types: map[string]ast.Expr{}}
}
