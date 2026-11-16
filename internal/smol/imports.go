package smol

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type Import struct {
	Path  string
	Alias bool
}
type Options struct {
	Context      *PackageContext
	Imports      map[string]Import
	PackageFiles map[string]string
	Lines        bool
	NoScope      bool   // lift without scope-based elision
	LineSource   string // code channel text that -lines positions refer to
	Filename     string
}

var stdOnce sync.Once
var stdTable map[string]Import
var stdPaths map[string]bool
var stdNames map[string]string

func standard() map[string]Import {
	stdOnce.Do(func() {
		stdTable = map[string]Import{}
		stdPaths = map[string]bool{}
		stdNames = map[string]string{}
		counts := map[string]int{}
		b, err := exec.Command("go", "list", "-f", "{{.ImportPath}} {{.Name}}", "std").Output()
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) != 2 {
				continue
			}
			p, n := f[0], f[1]
			stdPaths[p] = true
			stdNames[p] = n
			if !strings.HasPrefix(p, "internal/") && !strings.Contains(p, "/internal/") && !strings.HasPrefix(p, "vendor/") {
				counts[n]++
			}
		}
		for p, n := range stdNames {
			if counts[n] == 1 && !strings.HasPrefix(p, "internal/") && !strings.Contains(p, "/internal/") && !strings.HasPrefix(p, "vendor/") {
				stdTable[n] = Import{Path: p}
			}
		}
	})
	return stdTable
}
func table(o Options) map[string]Import {
	m := map[string]Import{}
	for k, v := range standard() {
		m[k] = v
	}
	for k, v := range o.Imports {
		m[k] = v
	}
	return m
}
func packageName(p string) string {
	if stdPaths[p] {
		return stdNames[p]
	}
	b, e := exec.Command("go", "list", "-f", "{{.Name}}", p).Output()
	if e == nil {
		return strings.TrimSpace(string(b))
	}
	n := path.Base(p)
	if i := strings.IndexByte(n, '.'); i >= 0 {
		n = n[:i]
	}
	return n
}
func ReadMod(text string) (map[string]Import, error) {
	m := map[string]Import{}
	for i, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		f := strings.Fields(l)
		if len(f) == 3 && f[0] == "use" {
			m[f[1]] = Import{Path: f[2]}
		} else if len(f) == 4 && f[0] == "use" && f[2] == "=" {
			m[f[1]] = Import{Path: f[3], Alias: true}
		} else {
			return nil, fmt.Errorf("smol.mod:%d: expected use name [=] path", i+1)
		}
	}
	return m, nil
}
func WriteMod(m map[string]Import) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := m[k]
		b.WriteString("use " + k + " ")
		if v.Alias {
			b.WriteString("= ")
		}
		b.WriteString(v.Path + "\n")
	}
	return b.String()
}

// Normalize implements §3. Commented imports and cgo preambles stay in place.
func Normalize(text string) (string, error) {
	text, err := norm(text)
	if err != nil {
		return "", err
	}
	s, err := parse(text)
	if err != nil {
		return "", err
	}
	standard()
	var specs []*ast.ImportSpec
	var decls []*ast.GenDecl
	for _, d := range s.file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.IMPORT {
			continue
		}
		safe := g.Doc == nil
		for _, sp := range g.Specs {
			im := sp.(*ast.ImportSpec)
			if im.Path.Value == `"C"` || im.Doc != nil || im.Comment != nil {
				safe = false
			}
		}
		if !s.clean(s.off(g.Pos()), s.off(g.End())) {
			safe = false
		}
		if safe {
			decls = append(decls, g)
			for _, sp := range g.Specs {
				specs = append(specs, sp.(*ast.ImportSpec))
			}
		}
	}
	if len(decls) == 0 {
		return text, nil
	}
	sort.SliceStable(specs, func(i, j int) bool {
		a, _ := strconv.Unquote(specs[i].Path.Value)
		b, _ := strconv.Unquote(specs[j].Path.Value)
		if stdPaths[a] != stdPaths[b] {
			return stdPaths[a]
		}
		return a < b
	})
	var lines []string
	lastStd := true
	seen := map[string]bool{}
	for _, sp := range specs {
		p, _ := strconv.Unquote(sp.Path.Value)
		if seen[s.raw(sp)] {
			continue
		}
		seen[s.raw(sp)] = true
		if lastStd && !stdPaths[p] && len(lines) > 0 {
			lines = append(lines, "")
		}
		lastStd = stdPaths[p]
		lines = append(lines, s.raw(sp))
	}
	im := "import "
	if len(lines) == 1 {
		im += lines[0]
	} else {
		im += "(\n" + strings.Join(lines, "\n") + "\n)"
	}
	remove := decls[1:]
	between := s.text[s.packageEnd():s.off(decls[0].Pos())]
	if strings.TrimSpace(between) != "" {
		s.put(s.packageEnd(), s.packageEnd(), "\n\n"+im)
		remove = decls
	} else {
		s.replace(decls[0], im)
		s.put(s.packageEnd(), s.off(decls[0].Pos()), "\n\n")
	}
	for _, d := range remove {
		a := s.off(d.Pos())
		b := s.off(d.End())
		if b < len(text) && text[b] == '\n' {
			b++
		}
		if b < len(text) && text[b] == '\n' {
			b++
		}
		s.put(a, b, "")
	}
	return norm(s.render(0, len(text)))
}
func (s *source) liftImports(o Options) {
	m := table(o)
	for _, d := range s.file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.IMPORT {
			continue
		}
		if g.Doc != nil {
			continue
		}
		// A block whose only comments belong to its specs keeps those specs
		// after a "..." line that stands for the inferred imports.
		partial := false
		if !s.clean(s.off(g.Pos()), s.off(g.End())) {
			if !g.Lparen.IsValid() || !s.specComments(g) {
				continue
			}
			partial = true
		}
		var keep []string
		cgo := false
		for _, sp := range g.Specs {
			im := sp.(*ast.ImportSpec)
			p, _ := strconv.Unquote(im.Path.Value)
			name := packageName(p)
			if im.Name != nil {
				name = im.Name.Name
			}
			cgo = cgo || p == "C"
			binding, ok := m[name]
			if name == "_" || name == "." || p == "C" || im.Doc != nil || im.Comment != nil || !ok || binding.Path != p || binding.Alias != (im.Name != nil) {
				a, b := im.Pos(), im.End()
				if im.Doc != nil {
					a = im.Doc.Pos()
				}
				if im.Comment != nil {
					b = im.Comment.End()
				}
				keep = append(keep, s.text[s.off(a):s.off(b)])
			}
		}
		if len(keep) == len(g.Specs) || (partial && cgo) {
			continue
		}
		if partial {
			s.replace(g, "import (\n...\n"+strings.Join(keep, "\n")+"\n)")
			continue
		}
		if len(keep) == 0 {
			a, b := s.off(g.Pos()), s.off(g.End())
			if b < len(s.text) && s.text[b] == '\n' {
				b++
			}
			if b < len(s.text) && s.text[b] == '\n' {
				b++
			}
			s.put(a, b, "")
		} else {
			s.replace(g, "import (\n"+strings.Join(keep, "\n")+"\n)")
		}
	}
}

var importerOnce sync.Once
var sharedImporter types.Importer

func exportImporter(fs *token.FileSet) types.Importer {
	importerOnce.Do(func() { sharedImporter = makeExportImporter(token.NewFileSet()) })
	return sharedImporter
}
func makeExportImporter(fs *token.FileSet) types.Importer {
	return importer.ForCompiler(fs, "gc", func(p string) (io.ReadCloser, error) {
		b, e := exec.Command("go", "list", "-export", "-f", "{{.Export}}", p).Output()
		if e != nil {
			return nil, e
		}
		return os.Open(strings.TrimSpace(string(b)))
	})
}
func (s *source) inferImports(o Options) (string, error) {
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Scopes: map[ast.Node]*types.Scope{}}
	files := []*ast.File{s.file}
	ctx := o.Context
	if ctx == nil {
		ctx = NewPackageContext(o.PackageFiles)
	}
	rootContext := ctx
	ctx = ctx.forPackage(s.file.Name.Name)
	conf := types.Config{Importer: exportImporter(s.fs), Error: func(error) {}, DisableUnusedImportCheck: true}
	pkg, _ := conf.Check(s.file.Name.Name, s.fs, files, info)
	explicit := map[string]bool{}
	dotNames := map[string]bool{}
	for _, im := range s.file.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		n := packageName(p)
		if im.Name != nil {
			if im.Name.Name == "." {
				for name := range rootContext.forPackage(n).names {
					if ast.IsExported(name) {
						dotNames[name] = true
					}
				}
			}
			n = im.Name.Name
		}
		explicit[n] = true
	}
	refs := map[string]bool{}
	ast.Inspect(s.file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || explicit[id.Name] || dotNames[id.Name] || info.Uses[id] != nil || types.Universe.Lookup(id.Name) != nil {
			return true
		}
		if (pkg != nil && pkg.Scope().Lookup(id.Name) != nil) || ctx.names[id.Name] {
			return true
		}
		for _, scope := range info.Scopes {
			if scope.Contains(id.Pos()) {
				_, obj := scope.LookupParent(id.Name, id.Pos())
				if obj != nil {
					return true
				}
			}
		}
		refs[id.Name] = true
		return true
	})
	var imports []string
	m := table(o)
	for q := range refs {
		im, ok := m[q]
		if !ok {
			return "", fmt.Errorf("unresolved package qualifier %q (add an import or smol.mod entry)", q)
		}
		l := strconv.Quote(im.Path)
		if im.Alias {
			l = q + " " + l
		}
		imports = append(imports, l)
	}
	sort.Strings(imports)
	if i := strings.Index(s.text, importsMarker); i >= 0 {
		lines := strings.Join(imports, "\n")
		j := i + len(importsMarker)
		if lines == "" && j < len(s.text) && s.text[j] == '\n' {
			j++
		}
		return s.text[:i] + lines + s.text[j:], nil
	}
	if len(imports) == 0 {
		return s.text, nil
	}
	p := s.packageEnd()
	return s.text[:p] + "\n\nimport (\n" + strings.Join(imports, "\n") + "\n)" + s.text[p:], nil
}

const importsMarker = `_ "smol.invalid/inferred-imports"`

// specComments reports whether an import block has no blank lines and every
// comment in it is the doc or line comment of one of its specs.
func (s *source) specComments(g *ast.GenDecl) bool {
	own := map[*ast.CommentGroup]bool{}
	for _, sp := range g.Specs {
		im := sp.(*ast.ImportSpec)
		own[im.Doc], own[im.Comment] = true, true
	}
	a, b := s.off(g.Pos()), s.off(g.End())
	for _, c := range s.file.Comments {
		if s.off(c.Pos()) < b && s.off(c.End()) > a && !own[c] {
			return false
		}
	}
	return !strings.Contains(strings.ReplaceAll(s.text[a:b], "\t", ""), "\n\n")
}

// lowerImportMarker turns the "..." line of an import block into a
// placeholder spec that inferImports replaces.
func lowerImportMarker(text string) (string, error) {
	ts := significant(lex(text))
	var es []edit
	for i := 0; i+2 < len(ts); i++ {
		if ts[i].lit == "import" && ts[i+1].lit == "(" && ts[i+2].lit == "..." {
			es = append(es, edit{ts[i+2].a, ts[i+2].b, importsMarker})
		}
	}
	return apply(text, es)
}
