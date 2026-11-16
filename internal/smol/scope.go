package smol

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// Scope-based elision (§6.10–§6.12): opened packages, receiver members and
// file parameter conventions. Lifting removes a qualifier only where the bare
// name cannot mean anything else; lowering restores it from the same facts.

// reservedBare are names never written bare because Smol reads them as syntax.
var reservedBare = map[string]bool{"in": true, "use": true, "let": true, "_": true}

func recvBase(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

func fieldNames(t ast.Expr, into map[string]bool) {
	st, ok := t.(*ast.StructType)
	if !ok || st.Fields == nil {
		return
	}
	for _, f := range st.Fields.List {
		for _, n := range f.Names {
			into[n.Name] = true
		}
		if len(f.Names) == 0 {
			e := f.Type
			if p, ok := e.(*ast.StarExpr); ok {
				e = p.X
			}
			switch t := e.(type) {
			case *ast.IndexExpr:
				e = t.X
			case *ast.IndexListExpr:
				e = t.X
			}
			if sel, ok := e.(*ast.SelectorExpr); ok {
				into[sel.Sel.Name] = true
			} else if n := recvBase(e); n != "" {
				into[n] = true
			}
		}
	}
}

// members returns the direct fields and declared methods of a package type.
func (c *PackageContext) members(file *ast.File, typ string) map[string]bool {
	m := map[string]bool{}
	if typ == "" {
		return m
	}
	if c != nil {
		if t := c.types[typ]; t != nil {
			fieldNames(t, m)
		}
		for n := range c.methods[typ] {
			m[n] = true
		}
	}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil && len(d.Recv.List) == 1 && recvBase(d.Recv.List[0].Type) == typ {
				m[d.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, sp := range d.Specs {
				if t, ok := sp.(*ast.TypeSpec); ok && t.Name.Name == typ {
					fieldNames(t.Type, m)
				}
			}
		}
	}
	return m
}

type scopeFacts struct {
	file     *ast.File
	fs       *token.FileSet
	imports  map[string]string // local name -> path
	dot      bool
	ctx      *PackageContext
	pkgNames map[string]bool
}

func (f *scopeFacts) off(p token.Pos) int { return f.fs.PositionFor(p, false).Offset }

func newScopeFacts(text string, o Options) (*scopeFacts, error) {
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "source.go", text, 0)
	if err != nil {
		return nil, err
	}
	ctx := o.Context
	if ctx == nil {
		ctx = NewPackageContext(o.PackageFiles)
	}
	ctx = ctx.forPackage(file.Name.Name)
	f := &scopeFacts{file: file, fs: fs, imports: map[string]string{}, ctx: ctx, pkgNames: map[string]bool{}}
	for n := range table(o) {
		f.pkgNames[n] = true
	}
	for _, im := range file.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		n := packageName(p)
		if im.Name != nil {
			n = im.Name.Name
		}
		if n == "." {
			f.dot = true
		}
		f.imports[n] = p
		f.pkgNames[n] = true
	}
	return f, nil
}

// taken reports whether a bare name already has another meaning in the file,
// the package, the universe or the import table.
func (f *scopeFacts) taken(name string) bool {
	return reservedBare[name] || f.pkgNames[name] || f.ctx.names[name] || types.Universe.Lookup(name) != nil || token.IsKeyword(name)
}

type method struct {
	a, b    int
	recv    string
	members map[string]bool
}

// methods lists the bodies that have a subject: the receiver of a method, or
// the first parameter of a function when it is a package type or a pointer
// to one.
func (f *scopeFacts) methods() []method {
	var out []method
	for _, d := range f.file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		list := fd.Recv
		if list == nil {
			list = fd.Type.Params
		}
		if list == nil || len(list.List) == 0 || len(list.List[0].Names) == 0 {
			continue
		}
		r := list.List[0].Names[0].Name
		base := recvBase(list.List[0].Type)
		if _, ok := list.List[0].Type.(*ast.Ellipsis); ok || r == "_" || base == "" {
			continue
		}
		mem := f.ctx.members(f.file, base)
		if len(mem) == 0 {
			continue
		}
		out = append(out, method{f.off(fd.Body.Pos()), f.off(fd.Body.End()), r, mem})
	}
	return out
}

func enclosing(ms []method, off int) *method {
	for i := range ms {
		if off >= ms[i].a && off < ms[i].b {
			return &ms[i]
		}
	}
	return nil
}

// declared collects every name a declaration introduces anywhere in the file,
// except struct fields and method names.
func (f *scopeFacts) declared() map[string]bool {
	d := map[string]bool{}
	add := func(ids ...*ast.Ident) {
		for _, id := range ids {
			if id != nil {
				d[id.Name] = true
			}
		}
	}
	fields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, x := range fl.List {
			add(x.Names...)
		}
	}
	ast.Inspect(f.file, func(n ast.Node) bool {
		switch t := n.(type) {
		case *ast.FuncDecl:
			if t.Recv == nil {
				add(t.Name)
			} else {
				fields(t.Recv)
				for _, x := range t.Recv.List { // receiver type parameters
					ast.Inspect(x.Type, func(n ast.Node) bool {
						if id, ok := n.(*ast.Ident); ok {
							add(id)
						}
						return true
					})
				}
			}
		case *ast.FuncType:
			fields(t.TypeParams)
			fields(t.Params)
			fields(t.Results)
		case *ast.TypeSpec:
			add(t.Name)
			fields(t.TypeParams)
		case *ast.ValueSpec:
			add(t.Names...)
		case *ast.AssignStmt:
			if t.Tok == token.DEFINE {
				for _, e := range t.Lhs {
					if id, ok := e.(*ast.Ident); ok {
						add(id)
					}
				}
			}
		case *ast.RangeStmt:
			if t.Tok == token.DEFINE {
				for _, e := range []ast.Expr{t.Key, t.Value} {
					if id, ok := e.(*ast.Ident); ok {
						add(id)
					}
				}
			}
		case *ast.LabeledStmt:
			add(t.Label)
		}
		return true
	})
	return d
}

func exportsOf(path string) *types.Package {
	defer func() { _ = recover() }()
	p, err := exportImporter(nil).Import(path)
	if err != nil {
		return nil
	}
	return p
}

func exported2(p *types.Package, name string) bool {
	return p != nil && ast.IsExported(name) && p.Scope().Lookup(name) != nil
}

// liftScopes registers the elision edits and returns the header lines.
func (s *source) liftScopes(o Options) string {
	f, err := newScopeFacts(s.text, o)
	if err != nil {
		return ""
	}
	header := ""
	if !f.dot {
		keys := map[ast.Expr]bool{}
		ast.Inspect(f.file, func(n ast.Node) bool {
			if c, ok := n.(*ast.CompositeLit); ok {
				for _, e := range c.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						keys[kv.Key] = true
					}
				}
			}
			return true
		})
		ms := f.methods()
		type site struct {
			sel *ast.SelectorExpr
			pkg string
		}
		var all []site
		ast.Inspect(f.file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || keys[sel] {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || f.taken(sel.Sel.Name) || !s.clean(f.off(sel.Pos()), f.off(sel.End())) {
				return true
			}
			m := enclosing(ms, f.off(sel.Pos()))
			if id.Obj != nil {
				// A subject member: the subject must be the one in scope.
				if m != nil && id.Name == m.recv && m.members[sel.Sel.Name] {
					if fld, ok := id.Obj.Decl.(*ast.Field); ok && f.off(fld.Pos()) < m.a {
						all = append(all, site{sel, ""})
					}
				}
				return true
			}
			if path := f.imports[id.Name]; path != "" && path != "C" && !f.ctx.names[id.Name] && (m == nil || !m.members[sel.Sel.Name]) {
				all = append(all, site{sel, id.Name})
			}
			return true
		})
		// Keep the sites whose bare name resolves to nothing where it stands.
		var trial []edit
		for _, st := range all {
			trial = append(trial, edit{f.off(st.sel.Pos()), f.off(st.sel.Sel.Pos()), ""})
		}
		unresolved := map[int]bool{}
		if t, err := apply(s.text, trial); err == nil {
			tf := token.NewFileSet()
			if file, err := parser.ParseFile(tf, "trial.go", t, 0); err == nil {
				for _, id := range file.Unresolved {
					unresolved[tf.PositionFor(id.Pos(), false).Offset] = true
				}
			}
		}
		var sites []site
		counts := map[string]int{}
		shift := 0
		for _, st := range all { // ast.Inspect visits selectors in source order of their end
			_ = st
		}
		sort.SliceStable(all, func(i, j int) bool { return f.off(all[i].sel.Pos()) < f.off(all[j].sel.Pos()) })
		for _, st := range all {
			a, b := f.off(st.sel.Pos()), f.off(st.sel.Sel.Pos())
			shift += b - a
			if !unresolved[b-shift] {
				continue
			}
			if st.pkg == "" {
				s.put(a, b, "")
				continue
			}
			sites = append(sites, st)
			counts[st.pkg]++
		}
		var opened []string
		exports := map[string]*types.Package{}
		for name, c := range counts {
			if c < 2 {
				continue
			}
			if p := exportsOf(f.imports[name]); p != nil {
				exports[name] = p
				opened = append(opened, name)
			}
		}
		sort.Strings(opened)
		used := map[string]bool{}
		for _, st := range sites {
			if exports[st.pkg] == nil {
				continue
			}
			n := 0
			for _, name := range opened {
				if exported2(exports[name], st.sel.Sel.Name) {
					n++
				}
			}
			if n == 1 {
				s.put(f.off(st.sel.Pos()), f.off(st.sel.Sel.Pos()), "")
				used[st.pkg] = true
			}
		}
		var names []string
		for _, name := range opened {
			if used[name] {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			header += "\nuse " + strings.Join(names, " ")
		}
	}
	// File parameter conventions.
	count := map[string]map[string]int{}
	prevRecv := ""
	ast.Inspect(s.file, func(n ast.Node) bool {
		var lists []*ast.FieldList
		switch t := n.(type) {
		case *ast.FuncDecl:
			// An inherited receiver is not written, so it does not count.
			if t.Recv != nil && s.raw(t.Recv) != prevRecv {
				lists = append(lists, t.Recv)
			}
			if t.Recv != nil {
				prevRecv = s.raw(t.Recv)
			}
		case *ast.FuncType:
			lists = append(lists, t.Params)
		}
		for _, l := range lists {
			if l == nil {
				continue
			}
			for _, fl := range l.List {
				if len(fl.Names) != 1 || fl.Names[0].Name == "_" || !s.clean(s.off(fl.Pos()), s.off(fl.End())) || strings.Contains(s.raw(fl.Type), "\n") {
					continue
				}
				if _, ok := fl.Type.(*ast.Ellipsis); ok {
					continue
				}
				if count[fl.Names[0].Name] == nil {
					count[fl.Names[0].Name] = map[string]int{}
				}
				count[fl.Names[0].Name][s.raw(fl.Type)]++
			}
		}
		return true
	})
	var lets []string
	for name, types := range count {
		best, n := "", 0
		for t, c := range types {
			if c > n || (c == n && t < best) {
				best, n = t, c
			}
		}
		if n >= 3 && conventions[name] != best {
			s.conv[name] = best
			lets = append(lets, name+" "+best)
		}
	}
	sort.Strings(lets)
	if len(lets) > 0 {
		header += "\nlet " + strings.Join(lets, ", ")
	}
	return header
}

// splitHeader removes the use and let lines that follow the package clause.
func splitHeader(text string) (rest string, uses []string, conv map[string]string, err error) {
	conv = map[string]string{}
	for k, v := range conventions {
		conv[k] = v
	}
	ts := lex(text)
	i := 0
	for i < len(ts) && ts[i].lit != "package" {
		i++
	}
	i += 2
	if i > len(ts) {
		return text, nil, conv, nil
	}
	prevEnd := ts[i-1].b
	var cuts []edit
	for i < len(ts) {
		t := ts[i]
		if t.k == token.COMMENT {
			prevEnd = t.b
			i++
			continue
		}
		if t.k == token.SEMICOLON {
			i++
			continue
		}
		if t.k != token.IDENT || (t.lit != "use" && t.lit != "let") || i+1 >= len(ts) || ts[i+1].k != token.IDENT || ts[i+1].line != t.line {
			break
		}
		j := i + 1
		if t.lit == "use" {
			for j < len(ts) && ts[j].k == token.IDENT && ts[j].line == t.line {
				uses = append(uses, ts[j].lit)
				j++
			}
		} else {
			for j < len(ts) && ts[j].k == token.IDENT {
				name := ts[j]
				k := j + 1
				for k < len(ts) && ts[k].lit != "," && ts[k].k != token.SEMICOLON {
					if ts[k].lit == "(" || ts[k].lit == "[" || ts[k].lit == "{" {
						m := matching(ts, k)
						if m < 0 {
							return "", nil, nil, fmt.Errorf("line %d: unclosed type in let", t.line)
						}
						k = m
					}
					k++
				}
				if k == j+1 {
					return "", nil, nil, fmt.Errorf("line %d: let %s has no type", t.line, name.lit)
				}
				conv[name.lit] = strings.TrimSpace(text[ts[j+1].a:ts[k-1].b])
				j = k
				if j < len(ts) && ts[j].lit == "," {
					j++
				}
			}
		}
		if j >= len(ts) || j == i+1 {
			break
		}
		cuts = append(cuts, edit{prevEnd, ts[j-1].b, ""})
		prevEnd = ts[j-1].b
		i = j
	}
	rest, err = apply(text, cuts)
	return rest, uses, conv, err
}

// lowerReceivers expands conventional receivers before declarations are read.
func lowerReceivers(text string, conv map[string]string) (string, error) {
	ts := significant(lex(text))
	var es []edit
	depth := 0
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		if depth == 0 && t.lit == "(" && (i == 0 || ts[i-1].line < t.line || ts[i-1].lit == "func") {
			j := matching(ts, i)
			if j > 0 && j+2 < len(ts) && ts[j+1].k == token.IDENT && (ts[j+2].lit == "(" || ts[j+2].lit == "[") {
				if j == i+2 && ts[i+1].k == token.IDENT && conv[ts[i+1].lit] != "" {
					es = append(es, edit{ts[i+1].b, ts[i+1].b, " " + conv[ts[i+1].lit]})
				} else if j == i+3 && ts[i+1].lit == "\\" {
					es = append(es, edit{ts[i+1].a, ts[i+1].b, ""})
				}
			}
		}
		switch t.lit {
		case "{", "(", "[":
			depth++
		case "}", ")", "]":
			depth--
		}
	}
	return apply(text, es)
}

// completeNames restores the qualifiers that lifting removed: a bare name
// that resolves to nothing is a receiver member inside a method of a type
// that has it, and otherwise an export of the one opened package that has it.
func completeNames(text string, o Options, uses []string) (string, error) {
	f, err := newScopeFacts(text, o)
	if err != nil {
		return "", fmt.Errorf("lower: %w", err)
	}
	if f.dot {
		return text, nil
	}
	ms := f.methods()
	tab := table(o)
	exports := map[string]*types.Package{}
	for _, u := range uses {
		path := f.imports[u]
		if path == "" {
			path = tab[u].Path
		}
		if path == "" {
			return "", fmt.Errorf("use %s: unknown package (add an import or smol.mod entry)", u)
		}
		p := exportsOf(path)
		if p == nil {
			return "", fmt.Errorf("use %s: cannot load %s", u, path)
		}
		exports[u] = p
	}
	var es []edit
	for _, id := range f.file.Unresolved {
		if f.taken(id.Name) {
			continue
		}
		at := f.off(id.Pos())
		if m := enclosing(ms, at); m != nil && m.members[id.Name] {
			es = append(es, edit{at, at, m.recv + "."})
			continue
		}
		var found []string
		for _, u := range uses {
			if exported2(exports[u], id.Name) {
				found = append(found, u)
			}
		}
		if len(found) > 1 {
			return "", fmt.Errorf("%s is exported by more than one opened package: %s", id.Name, strings.Join(found, ", "))
		}
		if len(found) == 1 {
			es = append(es, edit{at, at, found[0] + "."})
		}
	}
	if len(es) == 0 {
		return text, nil
	}
	return apply(text, es)
}

// Argument commas (§6.13). The comma between two call arguments is implied
// when the first ends in, and the second begins with, an identifier or a
// basic literal on the same line. The same holds for the list of a case.

func operandEdge(t tok) bool {
	switch t.k {
	case token.IDENT, token.INT, token.FLOAT, token.IMAG, token.CHAR, token.STRING:
		return true
	}
	return false
}

func (s *source) liftCommas() {
	ast.Inspect(s.file, func(n ast.Node) bool {
		var list []ast.Expr
		switch c := n.(type) {
		case *ast.CallExpr:
			list = c.Args
		case *ast.CaseClause:
			list = c.List
		}
		c := struct{ Args []ast.Expr }{list}
		for i := 0; i+1 < len(c.Args); i++ {
			a, b := s.off(c.Args[i].End()), s.off(c.Args[i+1].Pos())
			if s.text[a:b] != ", " {
				continue
			}
			left, right := lex(s.raw(c.Args[i])), lex(s.raw(c.Args[i+1]))
			left = significant(left)
			right = significant(right)
			if len(left) == 0 || len(right) == 0 || !operandEdge(left[len(left)-1]) || !operandEdge(right[0]) {
				continue
			}
			s.put(a, b, " ")
		}
		return true
	})
}

// lowerCommas restores implied argument commas. It runs after declarations
// have their func keyword, so signatures can be told apart from calls.
func lowerCommas(text string) (string, error) {
	ts := significant(lex(text))
	skip := map[int]bool{}
	for _, sig := range signatures(ts) {
		skip[sig.open] = true
		if sig.close+1 < len(ts) && ts[sig.close+1].lit == "(" {
			skip[sig.close+1] = true
		}
	}
	var es []edit
	var stack []bool // whether each open bracket is a call's argument list
	caseAt := -1     // bracket depth of the case list being read
	for i, t := range ts {
		switch t.lit {
		case "case":
			caseAt = len(stack)
			continue
		case ":":
			if caseAt == len(stack) {
				caseAt = -1
			}
			continue
		}
		if caseAt == len(stack) && i > 0 && operandEdge(t) && operandEdge(ts[i-1]) {
			es = append(es, edit{ts[i-1].b, ts[i-1].b, ","})
			continue
		}
		switch t.lit {
		case "(", "[", "{":
			call := false
			if t.lit == "(" && i > 0 && !skip[i] {
				p := ts[i-1]
				call = p.k == token.IDENT || p.lit == ")" || p.lit == "]" || p.lit == "}"
			}
			stack = append(stack, call)
			continue
		case ")", "]", "}":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if len(stack) > 0 && stack[len(stack)-1] && i > 0 && operandEdge(t) && operandEdge(ts[i-1]) {
			es = append(es, edit{ts[i-1].b, ts[i-1].b, ","})
		}
	}
	return apply(text, es)
}
