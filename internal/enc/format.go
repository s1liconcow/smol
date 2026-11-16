package enc

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"smolang/internal/smol"
)

func parseGo(text string) (*token.FileSet, *ast.File, error) {
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, "result.go", text, parser.ParseComments|parser.AllErrors)
	// Diagnostics refer to patch input lines even when Go //line directives
	// change the logical source filename or line number.
	if errors, ok := e.(scanner.ErrorList); ok {
		for _, item := range errors {
			off := min(max(item.Pos.Offset, 0), len(text))
			item.Pos.Line = strings.Count(text[:off], "\n") + 1
		}
	}
	return fs, f, e
}
func errorLine(err error, origins []int, fallback int) int {
	if located, ok := err.(sourceError); ok && located.patchLine > 0 {
		return located.patchLine
	}
	if errs, ok := err.(scanner.ErrorList); ok && len(errs) > 0 {
		i := errs[0].Pos.Line - 1
		if i >= 0 && i < len(origins) && origins[i] > 0 {
			return origins[i]
		}
	}
	return fallback
}
func finish(name, text, base string, tree Tree, origins []int, o Options, whole bool) (string, string, error) {
	if o.Profile == E0 {
		b, e := Normalize(text)
		return b, "", e
	}
	// Move explicit body import hints to the legal package import position.
	var err error
	before := text
	text, err = hoistImports(text)
	if err != nil {
		return "", "", err
	}
	siblings := map[string]string{}
	for p, t := range tree {
		if path.Dir(p) == path.Dir(name) {
			siblings[p] = t
		}
	}
	if o.Profile == E2 {
		expanded, e := smol.LowerOutput(text, siblings, o.Imports)
		if e != nil {
			mapped := remapOrigins(text, expanded, origins)
			return "", "", locateError(e, mapped)
		}
		text = expanded
	}
	text, err = repairImports(name, text, siblings, o)
	if err != nil {
		return "", "", err
	}
	origins = remapOrigins(before, text, origins)
	before = text
	text = declarationSpacing(text, origins)
	origins = remapOrigins(before, text, origins)
	clean := base == "" || whole
	if !clean {
		if b, e := format.Source([]byte(base)); e == nil {
			clean = string(b) == base
		}
	}
	if clean {
		b, e := format.Source([]byte(text))
		return string(b), "", e
	}
	// For an unformatted base retain original lines; indent only supplied lines.
	// Import changes are allowed to change their own declaration by §5.2.
	s := scan(text)
	ls := s.lines

	for i, l := range ls {
		if s.inside[i] || i >= len(origins) || origins[i] == 0 || strings.TrimSpace(l) == "" {
			continue
		}
		depth := s.low[i]
		if depth < 0 {
			depth = 0
		}
		ls[i] = strings.Repeat("\t", depth) + strings.TrimLeft(l, " \t")
	}
	out := strings.Join(ls, "\n") + "\n"
	_, _, err = parseGo(out)
	return out, "base is not gofmt-clean; file was not formatted", err
}
func hoistImports(text string) (string, error) {
	// Lexical tokens distinguish import hints from strings and comments, even
	// when the hint appears in a body that is not yet parseable Go.
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(text))
	var sc scanner.Scanner
	sc.Init(f, []byte(text), func(token.Position, string) {}, scanner.ScanComments)
	type tok struct {
		k    token.Token
		a, b int
	}
	var ts []tok
	for {
		p, k, l := sc.Scan()
		if k == token.EOF {
			break
		}
		if l == "" {
			l = k.String()
		}
		a := f.Offset(p)
		ts = append(ts, tok{k, a, a + len(l)})
	}
	depth := 0
	seenDecl := false
	type cut struct{ a, b int }
	var cuts []cut
	var hints []string
	for i, t := range ts {
		if depth == 0 && (t.k == token.FUNC || t.k == token.TYPE || t.k == token.VAR || t.k == token.CONST) {
			seenDecl = true
		}
		if t.k == token.IMPORT && (depth > 0 || seenDecl) {
			end := t.b
			j := i + 1
			for j < len(ts) && ts[j].k == token.COMMENT {
				j++
			}
			if j < len(ts) && ts[j].k == token.LPAREN {
				d := 1
				j++
				for j < len(ts) && d > 0 {
					if ts[j].k == token.LPAREN {
						d++
					}
					if ts[j].k == token.RPAREN {
						d--
					}
					end = ts[j].b
					j++
				}
			} else {
				for j < len(ts) && ts[j].k != token.SEMICOLON {
					end = ts[j].b
					j++
				}
			}
			snippet := text[t.a:end]
			_, im, e := parseGo("package p\n" + snippet + "\n")
			if e != nil {
				return "", e
			}
			for _, s := range im.Imports {
				if s.Path.Value == `"C"` {
					return "", fmt.Errorf("import C must stay at file scope with its preamble")
				}
			}
			cuts = append(cuts, cut{t.a, end})
			hints = append(hints, snippet)
		}
		switch t.k {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth--
		}
	}
	if len(cuts) == 0 {
		return text, nil
	}
	for i := len(cuts) - 1; i >= 0; i-- {
		c := cuts[i]
		text = text[:c.a] + text[c.b:]
	}
	pkg := -1
	for _, t := range ts {
		if t.k == token.PACKAGE {
			pkg = t.a
			break
		}
	}
	if pkg < 0 {
		return "", fmt.Errorf("missing package clause")
	}
	pos := strings.Index(text[pkg:], "\n")
	if pos < 0 {
		return "", fmt.Errorf("missing package clause newline")
	}
	pos += pkg
	return text[:pos+1] + strings.Join(hints, "\n") + "\n" + text[pos+1:], nil
}
func packageFacts(name, text string, siblings Tree, o Options) (map[string]bool, map[string][]string) {
	_, file, e := cachedGo(text, o)
	if e != nil {
		return nil, nil
	}
	declared := map[string]bool{}
	candidates := map[string][]string{}
	for p, t := range siblings {
		_, f, e := cachedGo(t, o)
		if e != nil || f.Name.Name != file.Name.Name {
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					declared[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.TypeSpec:
						declared[sp.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							declared[n.Name] = true
						}
					}
				}
			}
		}
		if p == name {
			continue
		}
		for _, im := range f.Imports {
			q, ip := importName(im, o)
			if q != "_" && q != "." && ip != "C" {
				candidates[q] = append(candidates[q], ip)
			}
		}
	}
	return declared, candidates
}
func importName(im *ast.ImportSpec, o Options) (string, string) {
	p, _ := strconv.Unquote(im.Path.Value)
	n := path.Base(p)
	if im.Name != nil {
		n = im.Name.Name
	} else {
		var known []string
		for q, ps := range o.Packages {
			for _, ip := range ps {
				if ip == p {
					known = append(known, q)
				}
			}
		}
		for q, ip := range o.Imports {
			if ip == p {
				known = append(known, q)
			}
		}
		known = unique(known)
		if len(known) == 1 {
			n = known[0]
		}
		if q := o.analysis.pathNames[p]; q != "" {
			n = q
		}
		names := o.analysis.names
		for q, ip := range names {
			if ip == p {
				n = q
				break
			}
		}
	}
	return n, p
}
func unique(ps []string) []string {
	m := map[string]bool{}
	for _, p := range ps {
		m[p] = true
	}
	out := []string{}
	for p := range m {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
func repairImports(name, text string, siblings Tree, o Options) (string, error) {
	fs, file, err := cachedGo(text, o)
	if err != nil {
		return "", err
	}
	declared, siblingImports := packageFacts(name, text, siblings, o)
	refs := map[string]bool{}
	explicit := map[string]string{}
	for _, im := range file.Imports {
		q, p := importName(im, o)
		if prior := explicit[q]; q != "_" && q != "." && prior != "" && prior != p {
			return "", fmt.Errorf("import qualifier %q has candidate paths %s and %s; choose one explicit import", q, prior, p)
		}
		explicit[q] = p
		if q == "." {
			for _, name := range smol.OutputExports(p) {
				declared[name] = true
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Obj == nil && (explicit[id.Name] != "" || !declared[id.Name] && types.Universe.Lookup(id.Name) == nil) {
				refs[id.Name] = true
			}
		}
		return true
	})
	names, std := o.analysis.names, o.analysis.std
	var added []string
	qs := []string{}
	for q := range refs {
		qs = append(qs, q)
	}
	sort.Strings(qs)
	for _, q := range qs {
		if q == "C" && explicit[q] == "" {
			return "", fmt.Errorf("import C and its preamble must be written explicitly")
		}
		if explicit[q] != "" {
			continue
		}
		tiers := [][]string{siblingImports[q]}
		if p := o.Imports[q]; p != "" {
			tiers = append(tiers, []string{p})
		}
		if p := names[q]; p != "" {
			tiers = append(tiers, []string{p})
		}
		tiers = append(tiers, o.Packages[q])
		var selected string
		var all []string
		for _, tier := range tiers {
			ps := unique(tier)
			all = append(all, ps...)
			if len(ps) == 1 {
				selected = ps[0]
				break
			}
		}
		if selected == "" {
			return "", fmt.Errorf("unresolved import qualifier %q (candidates: %s); write import %q explicitly", q, strings.Join(unique(all), ", "), "path/to/"+q)
		}
		spec := strconv.Quote(selected)
		if path.Base(selected) != q {
			spec = q + " " + spec
		}
		added = append(added, spec)
	}
	type edit struct {
		a, b int
		s    string
	}
	var es []edit
	if len(added) > 0 {
		// Rebuild the ordinary import declaration only when adding imports. Keep
		// C and its preamble in their original declaration.
		type spec struct{ path, text string }
		var specs []spec
		for _, sp := range added {
			ip := sp[strings.Index(sp, "\""):]
			p, _ := strconv.Unquote(ip)
			specs = append(specs, spec{p, sp})
		}
		for _, d := range file.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.IMPORT {
				continue
			}
			hasC := false
			for _, sp := range g.Specs {
				if sp.(*ast.ImportSpec).Path.Value == `"C"` {
					hasC = true
				}
			}
			if hasC {
				continue
			}
			for _, sp := range g.Specs {
				im := sp.(*ast.ImportSpec)
				q, p := importName(im, o)
				if q != "_" && q != "." && !refs[q] {
					continue
				}
				raw := ""
				if im.Doc != nil {
					for _, c := range im.Doc.List {
						raw += c.Text + "\n"
					}
				}
				raw += text[fs.Position(im.Pos()).Offset:fs.Position(im.End()).Offset]
				if im.Comment != nil {
					for _, c := range im.Comment.List {
						raw += " " + c.Text
					}
				}
				specs = append(specs, spec{p, raw})
			}
			a, b := fs.Position(g.Pos()).Offset, fs.Position(g.End()).Offset
			if b < len(text) && text[b] == '\n' {
				b++
			}
			es = append(es, edit{a, b, ""})
		}
		sort.SliceStable(specs, func(i, j int) bool {
			if std[specs[i].path] != std[specs[j].path] {
				return std[specs[i].path]
			}
			return specs[i].path < specs[j].path
		})
		decl := "\nimport (\n"
		last := true
		for _, sp := range specs {
			if last && !std[sp.path] {
				decl += "\n"
			}
			last = std[sp.path]
			decl += sp.text + "\n"
		}
		decl += ")\n"
		decl = formattedImport(decl)
		pos := fs.Position(file.Name.End()).Offset
		for pos < len(text) && text[pos] != '\n' {
			pos++
		}
		if pos < len(text) {
			pos++
		}
		es = append(es, edit{pos, pos, decl})
		sort.SliceStable(es, func(i, j int) bool { return es[i].a > es[j].a })
		for _, e := range es {
			text = text[:e.a] + e.s + text[e.b:]
		}
		return text, nil
	}
	for _, d := range file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.IMPORT {
			continue
		}
		kept := []ast.Spec{}
		changed := false
		for _, sp := range g.Specs {
			im := sp.(*ast.ImportSpec)
			q, p := importName(im, o)
			if p != "C" && q != "_" && q != "." && !refs[q] {
				changed = true
			} else {
				kept = append(kept, sp)
			}
		}
		if !changed {
			continue
		}
		a, b := fs.Position(g.Pos()).Offset, fs.Position(g.End()).Offset
		if len(kept) == 0 {
			if b < len(text) && text[b] == '\n' {
				b++
			}
			es = append(es, edit{a, b, ""})
		} else {
			var bld strings.Builder
			bld.WriteString("import (\n")
			for _, sp := range kept {
				im := sp.(*ast.ImportSpec)
				if im.Doc != nil {
					for _, c := range im.Doc.List {
						bld.WriteString(c.Text + "\n")
					}
				}
				bld.WriteString(text[fs.Position(im.Pos()).Offset:fs.Position(im.End()).Offset])
				if im.Comment != nil {
					for _, c := range im.Comment.List {
						bld.WriteString(" " + c.Text)
					}
				}
				bld.WriteByte('\n')
			}
			bld.WriteString(")")
			es = append(es, edit{a, b, strings.TrimSpace(formattedImport(bld.String()))})
		}
	}
	sort.Slice(es, func(i, j int) bool { return es[i].a > es[j].a })
	for _, e := range es {
		text = text[:e.a] + e.s + text[e.b:]
	}
	return text, nil
}
func declarationSpacing(text string, origins []int) string {
	fs, f, e := parseGo(text)
	if e != nil {
		return text
	}
	ls := lines(text)
	var at []int
	for i := 1; i < len(f.Decls); i++ {
		prev, cur := f.Decls[i-1], f.Decls[i]
		a, b := fs.Position(prev.Pos()).Line, fs.Position(prev.End()).Line
		c := fs.Position(cur.Pos()).Line
		switch d := cur.(type) {
		case *ast.FuncDecl:
			if d.Doc != nil {
				c = fs.Position(d.Doc.Pos()).Line
			}
		case *ast.GenDecl:
			if d.Doc != nil {
				c = fs.Position(d.Doc.Pos()).Line
			}
		}
		written := func(i int) bool { return i >= 0 && i < len(origins) && origins[i] > 0 }
		if b > a && c == b+1 && (written(b-1) || written(c-1)) {
			at = append(at, c-1)
		}
	}
	for i := len(at) - 1; i >= 0; i-- {
		p := at[i]
		ls = append(ls[:p], append([]string{""}, ls[p:]...)...)
	}
	return strings.Join(ls, "\n") + "\n"
}

// Preserve line provenance through syntax/import insertions and removals.
func remapOrigins(before, after string, origins []int) []int {
	a, b := lines(before), lines(after)
	out := make([]int, len(b))
	ai, bi := 0, 0
	for _, h := range diff(a, b) {
		for ai < h.a && bi < h.c {
			if ai < len(origins) {
				out[bi] = origins[ai]
			}
			ai++
			bi++
		}
		origin := 0
		for k := h.a; k < h.b && k < len(origins); k++ {
			if origins[k] > 0 {
				origin = origins[k]
				break
			}
		}
		for k := h.c; k < h.d; k++ {
			out[k] = origin
		}
		ai, bi = h.b, h.d
	}
	for ai < len(a) && bi < len(b) {
		if ai < len(origins) {
			out[bi] = origins[ai]
		}
		ai++
		bi++
	}
	return out
}

type parsedGo struct {
	fs   *token.FileSet
	file *ast.File
	err  error
}
type analysisCache struct {
	parsed    map[string]parsedGo
	names     map[string]string
	std       map[string]bool
	pathNames map[string]string
}

func newAnalysis() *analysisCache {
	names, std := smol.OutputImports()
	return &analysisCache{parsed: map[string]parsedGo{}, names: names, std: std, pathNames: smol.OutputPackageNames()}
}
func cachedGo(text string, o Options) (*token.FileSet, *ast.File, error) {
	if o.analysis == nil {
		return parseGo(text)
	}
	if p, ok := o.analysis.parsed[text]; ok {
		return p.fs, p.file, p.err
	}
	fs, f, e := parseGo(text)
	if len(o.analysis.parsed) > 512 {
		o.analysis.parsed = map[string]parsedGo{}
	}
	o.analysis.parsed[text] = parsedGo{fs, f, e}
	return fs, f, e
}

type sourceError struct {
	error
	patchLine int
}

var reportedLine = regexp.MustCompile(`line (\d+)`)

func locateError(e error, origins []int) error {
	line := errorLine(e, origins, 0)
	if line == 0 {
		if match := reportedLine.FindStringSubmatch(e.Error()); len(match) > 1 {
			n, _ := strconv.Atoi(match[1])
			if n > 0 && n <= len(origins) {
				line = origins[n-1]
			}
		}
	}
	return sourceError{e, line}
}

// Normalize is the output-encoding canonical Go form: gofmt plus the
// declaration spacing in SPEC_ENC 5.3. Input-channel normalization is separate.
func Normalize(text string) (string, error) {
	b, e := format.Source([]byte(text))
	if e != nil {
		return "", e
	}
	text = string(b)
	origins := make([]int, len(lines(text)))
	for i := range origins {
		origins[i] = 1
	}
	text = declarationSpacing(text, origins)
	b, e = format.Source([]byte(text))
	return string(b), e
}

func formattedImport(decl string) string {
	b, e := format.Source([]byte("package p\n" + decl + "\n"))
	if e != nil {
		return decl
	}
	text := string(b)
	return strings.TrimPrefix(text, "package p\n\n")
}
