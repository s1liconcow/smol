package smol

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

var conventions = map[string]string{"ctx": "context.Context", "w": "http.ResponseWriter", "r": "*http.Request", "t": "*testing.T"}

// Lift converts normalized Go to wide Smol: canonical syntax with comments
// and blank lines in place. Pack splits the result into its two channels.
// Scope-based elision is verified by lowering the result; a file whose
// elided names do not round-trip is lifted without it.
func Lift(text string, o Options) (string, error) {
	text, err := Normalize(text)
	if err != nil {
		return "", err
	}
	if !o.NoScope {
		out, err := lift(text, o)
		var back string
		if err == nil {
			if back, err = Lower(out, o); err == nil && back == text {
				return out, nil
			}
		}
		if os.Getenv("SMOL_TRACE") != "" {
			reason := "differs at " + difference(text, back)
			if err != nil {
				reason = err.Error()
			}
			fmt.Fprintf(os.Stderr, "smol: %s: scope elision disabled: %s\n", o.Filename, reason)
		}
		o.NoScope = true
	}
	return lift(text, o)
}

func lift(text string, o Options) (string, error) {
	s, err := parse(text)
	if err != nil {
		return "", err
	}
	z := newZeros(s, o)
	s.conv = map[string]string{}
	for k, v := range conventions {
		s.conv[k] = v
	}
	if !o.NoScope {
		if header := s.liftScopes(o); header != "" {
			s.put(s.packageEnd(), s.packageEnd(), header)
		}
		s.liftCommas()
	}
	s.liftImports(o)
	var prev string
	for _, d := range s.file.Decls {
		f, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if s.clean(s.off(f.Pos()), s.off(f.Name.Pos())) {
			s.put(s.off(f.Pos()), s.off(f.Pos())+5, "")
		}
		if f.Recv != nil {
			s.liftParams(f.Recv)
			recv := s.raw(f.Recv)
			if recv == prev && s.clean(s.off(f.Recv.Pos()), s.off(f.Recv.End())) {
				s.replace(f.Recv, ".")
				s.put(s.off(f.Recv.End()), s.off(f.Name.Pos()), "")
			}
			prev = recv
		}
	}
	// Postorder allows enclosing statement folds to include all inner rewrites.
	var nodes []ast.Node
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(s.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		nodes = append(nodes, n)
		return true
	})
	enclosing := func(n ast.Node) *ast.FuncType {
		for p := parents[n]; p != nil; p = parents[p] {
			switch f := p.(type) {
			case *ast.FuncDecl:
				return f.Type
			case *ast.FuncLit:
				return f.Type
			}
		}
		return nil
	}
	for i := len(nodes) - 1; i >= 0; i-- {
		n := nodes[i]
		switch t := n.(type) {
		case *ast.FuncType:
			s.liftSignature(t)
		case *ast.StructType:
			s.liftTags(t)
		case *ast.ReturnStmt:
			ft := enclosing(t)
			if ft == nil || !fallible(ft) || len(t.Results) != len(results(ft)) || !s.clean(s.off(t.Pos()), s.off(t.End())) {
				continue
			}
			var vals []string
			for _, e := range t.Results {
				vals = append(vals, s.node(e))
			}
			if vals[len(vals)-1] == "nil" {
				s.replace(t, "^"+strings.Join(vals[:len(vals)-1], ", "))
			} else if zs, ok := z.list(ft); ok && len(zs) > 0 && reflect.DeepEqual(vals[:len(vals)-1], zs) {
				s.replace(t, "!"+vals[len(vals)-1])
			}
		case *ast.RangeStmt:
			if t.Tok != token.DEFINE || t.Value == nil || t.Key == nil {
				continue
			}
			a, b := s.off(t.Key.Pos()), s.off(t.X.Pos())
			if !s.clean(a, b) {
				continue
			}
			key := s.node(t.Key)
			v := s.node(t.Value)
			if key == "_" {
				key = v
			} else {
				key += ", " + v
			}
			s.put(a, b, key+" in ")
		case *ast.BlockStmt:
			ft := enclosing(t)
			s.liftTries(t, ft, z)
			if len(t.List) == 1 && s.clean(s.off(t.Pos()), s.off(t.End())) {
				if r, ok := t.List[0].(*ast.ReturnStmt); ok && len(r.Results) > 0 {
					multi := strings.Contains(s.raw(t), "\n")
					decl := false
					switch parents[t].(type) {
					case *ast.FuncDecl:
						decl = true
					case *ast.FuncLit:
					default:
						goto inline
					}
					ambiguous := false
					if !decl && len(r.Results) < len(results(ft)) {
						for _, next := range lex(s.text[s.off(t.End()):]) {
							if next.k != token.COMMENT {
								ambiguous = next.lit == ","
								break
							}
						}
					}
					if !decl {
						if literal, ok := parents[t].(*ast.FuncLit); ok {
							switch outer := parents[literal].(type) {
							case *ast.CallExpr:
								ambiguous = ambiguous || outer.Fun == literal
							case *ast.IndexExpr:
								ambiguous = ambiguous || outer.X == literal
							case *ast.IndexListExpr:
								ambiguous = ambiguous || outer.X == literal
							case *ast.SelectorExpr:
								ambiguous = ambiguous || outer.X == literal
							case *ast.TypeAssertExpr:
								ambiguous = ambiguous || outer.X == literal
							case *ast.BinaryExpr:
								ambiguous = ambiguous || outer.X == literal
							}
						}
					}
					if multi == decl && !ambiguous {
						var vals []string
						for _, e := range r.Results {
							vals = append(vals, s.node(e))
						}
						s.replace(t, "=> "+strings.Join(vals, ", "))
						continue
					}
				}
			}
		inline:
			switch parents[t].(type) {
			case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
				if len(t.List) == 1 && s.clean(s.off(t.Pos()), s.off(t.End())) && !hasBlock(t.List[0]) {
					s.replace(t, "{ "+s.node(t.List[0])+" }")
				}
			}
		}
	}
	return compact(s.render(0, len(text))), nil
}
func hasBlock(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if _, ok := n.(*ast.BlockStmt); ok {
			found = true
			return false
		}
		return true
	})
	return found
}
func (s *source) liftParams(fl *ast.FieldList) {
	if fl == nil {
		return
	}
	conventions := s.conv
	for _, f := range fl.List {
		if len(f.Names) == 1 && conventions[f.Names[0].Name] == s.raw(f.Type) && s.clean(s.off(f.Pos()), s.off(f.End())) {
			s.replace(f, f.Names[0].Name)
			continue
		}
		// A conventional name in a grouped declaration or unnamed type needs escaping.
		for index, n := range f.Names {
			if index < len(f.Names)-1 && conventions[n.Name] != "" {
				s.replace(n, "\\"+n.Name)
			}
		}
		if len(f.Names) == 0 {
			if id, ok := f.Type.(*ast.Ident); ok && conventions[id.Name] != "" {
				s.replace(id, "\\"+id.Name)
			}
		}
	}
}
func (s *source) liftSignature(ft *ast.FuncType) {
	s.liftParams(ft.Params)
	r := ft.Results
	if r == nil || len(r.List) == 0 {
		return
	}
	if len(r.List) == 1 && r.Opening.IsValid() {
		return
	}
	last := r.List[len(r.List)-1]
	id, ok := last.Type.(*ast.Ident)
	if !ok || id.Name != "error" || len(last.Names) > 0 || !s.clean(s.off(ft.Params.End()), s.off(r.End())) {
		return
	}
	for _, f := range r.List {
		if len(f.Names) > 0 {
			return
		}
	}
	var vals []string
	for _, f := range r.List[:len(r.List)-1] {
		vals = append(vals, s.node(f))
	}
	v := ""
	if len(vals) == 1 {
		v = vals[0]
		if strings.HasSuffix(v, "!") || endsInFunction(r.List[0].Type) {
			v = "(" + v + ")"
		}
	} else if len(vals) > 1 {
		v = "(" + strings.Join(vals, ", ") + ")"
	}
	s.replace(r, v+"!")
	// Remove the preceding result space for an error-only signature.
	if len(vals) == 0 {
		s.put(s.off(ft.Params.End()), s.off(r.Pos()), "")
	}
}
func snake(name string) string {
	rs := []rune(name)
	var b strings.Builder
	for i, r := range rs {
		if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1]) || (unicode.IsUpper(rs[i-1]) && i+1 < len(rs) && unicode.IsLower(rs[i+1]))) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
func implied(keys []string, name string) string {
	var vals []string
	for _, k := range keys {
		vals = append(vals, k+":"+strconv.Quote(snake(name)))
	}
	return "`" + strings.Join(vals, " ") + "`"
}
func tagKeys(tag, name string) []string {
	if !strings.HasPrefix(tag, "`") {
		return nil
	}
	body := strings.Trim(tag, "`")
	var keys []string
	for _, part := range strings.Split(body, " ") {
		i := strings.IndexByte(part, ':')
		if i <= 0 || !token.IsIdentifier(part[:i]) || part[i+1:] != strconv.Quote(snake(name)) {
			return nil
		}
		keys = append(keys, part[:i])
	}
	if implied(keys, name) != tag {
		return nil
	}
	return keys
}
func exported(f *ast.Field) bool { return len(f.Names) == 1 && ast.IsExported(f.Names[0].Name) }
func (s *source) liftTags(t *ast.StructType) {
	if !s.clean(s.off(t.Struct), s.off(t.Fields.Opening)) {
		return
	}
	var keys []string
	for _, f := range t.Fields.List {
		if !exported(f) || f.Tag == nil {
			continue
		}
		keys = tagKeys(s.raw(f.Tag), f.Names[0].Name)
		if len(keys) > 0 {
			break
		}
	}
	if len(keys) == 0 {
		return
	}
	count := 0
	for _, f := range t.Fields.List {
		if exported(f) && f.Tag != nil && s.raw(f.Tag) == implied(keys, f.Names[0].Name) && s.clean(s.off(f.Pos()), s.off(f.End())) {
			count++
		}
	}
	if count < 2 {
		return
	}
	s.put(s.off(t.Struct)+6, s.off(t.Fields.Opening), " @"+strings.Join(keys, " @")+" ")
	for _, f := range t.Fields.List {
		if !exported(f) {
			continue
		}
		if f.Tag == nil {
			s.put(s.off(f.End()), s.off(f.End()), " ~")
		} else if s.raw(f.Tag) == implied(keys, f.Names[0].Name) && s.clean(s.off(f.Pos()), s.off(f.End())) {
			s.put(s.off(f.Type.End()), s.off(f.Tag.End()), "")
		}
	}
}
func errorIf(i *ast.IfStmt) bool { return i.Else == nil && isErrCond(i.Cond) }
func isErrCond(e ast.Expr) bool {
	b, ok := e.(*ast.BinaryExpr)
	return ok && b.Op == token.NEQ && exprText(b.X) == "err" && exprText(b.Y) == "nil"
}
func (s *source) handler(b *ast.BlockStmt, ft *ast.FuncType, z *zeros) string {
	if ft != nil && len(b.List) == 1 {
		if r, ok := b.List[0].(*ast.ReturnStmt); ok && len(r.Results) == len(results(ft)) {
			zs, ok := z.list(ft)
			if ok {
				vals := []string{}
				for _, e := range r.Results[:len(r.Results)-1] {
					vals = append(vals, s.raw(e))
				}
				if reflect.DeepEqual(vals, zs) || (len(vals) == 0 && len(zs) == 0) {
					e := r.Results[len(r.Results)-1]
					if s.raw(e) == "err" {
						return "?"
					}
					if c, ok := e.(*ast.CallExpr); ok && s.raw(c.Fun) == "fmt.Errorf" && len(c.Args) >= 2 && s.raw(c.Args[len(c.Args)-1]) == "err" && c.Ellipsis == token.NoPos {
						lit, ok := c.Args[0].(*ast.BasicLit)
						if ok && lit.Kind == token.STRING && strings.HasPrefix(lit.Value, `"`) && strings.HasSuffix(lit.Value, `: %w"`) {
							format := strings.TrimSuffix(lit.Value, `: %w"`) + `"`
							args := []string{format}
							for _, e := range c.Args[1 : len(c.Args)-1] {
								args = append(args, s.node(e))
							}
							return "? " + strings.Join(args, ", ")
						}
					}
				}
			}
		}
	}
	body := s.node(b) // includes folds already applied to the block itself
	body = body[1 : len(body)-1]
	if len(b.List) == 1 && !hasBlock(b.List[0]) {
		return "? { " + strings.TrimSpace(body) + " }"
	}
	return "? {" + body + "}"
}
func (s *source) liftTries(b *ast.BlockStmt, ft *ast.FuncType, z *zeros) {
	for i := len(b.List) - 1; i >= 0; i-- {
		cur, ok := b.List[i].(*ast.IfStmt)
		if !ok || !errorIf(cur) || (len(cur.Body.List) == 0 && !strings.Contains(s.raw(cur.Body), "\n")) {
			continue
		}
		if cur.Init != nil {
			a, ok := cur.Init.(*ast.AssignStmt)
			if !ok || a.Tok != token.DEFINE || len(a.Lhs) != 1 || s.raw(a.Lhs[0]) != "err" || len(a.Rhs) != 1 || !s.clean(s.off(cur.Pos()), s.off(cur.End())) {
				continue
			}
			s.replace(cur, s.node(a.Rhs[0])+" "+s.handler(cur.Body, ft, z))
			continue
		}
		if i == 0 {
			continue
		}
		a, ok := b.List[i-1].(*ast.AssignStmt)
		if !ok || (a.Tok != token.DEFINE && a.Tok != token.ASSIGN) || len(a.Lhs) == 0 || len(a.Rhs) != 1 || s.raw(a.Lhs[len(a.Lhs)-1]) != "err" || !s.clean(s.off(a.Pos()), s.off(cur.End())) {
			continue
		}
		var lhs []string
		for _, e := range a.Lhs[:len(a.Lhs)-1] {
			lhs = append(lhs, s.node(e))
		}
		if len(lhs) == 0 {
			lhs = []string{"err"}
		}
		handler := s.handler(cur.Body, ft, z)
		space := " "
		if handler == "?" {
			space = ""
		}
		s.put(s.off(a.Pos()), s.off(cur.End()), strings.Join(lhs, ", ")+" "+a.Tok.String()+" "+s.node(a.Rhs[0])+space+handler)
	}
}

func endsInFunction(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.FuncType:
		return true
	case *ast.StarExpr:
		return endsInFunction(t.X)
	case *ast.ArrayType:
		return endsInFunction(t.Elt)
	case *ast.MapType:
		return endsInFunction(t.Value)
	case *ast.ChanType:
		return endsInFunction(t.Value)
	}
	return false
}
