package smol

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

func parserFile(fs *token.FileSet, name, text string) (*ast.File, error) {
	return parser.ParseFile(fs, name, text, parser.ParseComments|parser.SkipObjectResolution)
}

type zeros struct {
	s        *source
	decls    map[string]ast.Expr
	params   map[string]bool
	imports  map[string]string
	cache    map[string]*types.Package
	locals   []localType
	position token.Pos
}
type localType struct {
	name       string
	expr       ast.Expr
	start, end token.Pos
}

func newZeros(s *source, o Options) *zeros {
	z := &zeros{s: s, decls: map[string]ast.Expr{}, params: map[string]bool{}, imports: map[string]string{}, cache: map[string]*types.Package{}}
	ctx := o.Context
	if ctx == nil {
		ctx = NewPackageContext(o.PackageFiles)
	}
	ctx = ctx.forPackage(s.file.Name.Name)
	for n, t := range ctx.types {
		z.decls[n] = t
	}
	for _, d := range s.file.Decls {
		if g, ok := d.(*ast.GenDecl); ok {
			for _, sp := range g.Specs {
				if t, ok := sp.(*ast.TypeSpec); ok {
					z.decls[t.Name.Name] = t.Type
				}
			}
		}
	}
	var scopes []*ast.BlockStmt
	var nodes []ast.Node
	ast.Inspect(s.file, func(n ast.Node) bool {
		if n == nil {
			if _, ok := nodes[len(nodes)-1].(*ast.BlockStmt); ok {
				scopes = scopes[:len(scopes)-1]
			}
			nodes = nodes[:len(nodes)-1]
			return false
		}
		nodes = append(nodes, n)
		if b, ok := n.(*ast.BlockStmt); ok {
			scopes = append(scopes, b)
		}
		if t, ok := n.(*ast.TypeSpec); ok && len(scopes) > 0 {
			z.locals = append(z.locals, localType{t.Name.Name, t.Type, t.Pos(), scopes[len(scopes)-1].End()})
		}
		return true
	})
	for n, im := range table(o) {
		z.imports[n] = im.Path
	}
	for _, im := range s.file.Imports {
		p := strings.Trim(im.Path.Value, `"`)
		n := packageName(p)
		if im.Name != nil {
			n = im.Name.Name
		}
		z.imports[n] = p
	}
	return z
}
func (z *zeros) zero(e ast.Expr, spelling string, seen map[string]bool) (string, bool) {
	switch t := e.(type) {
	case *ast.ParenExpr:
		return z.zero(t.X, spelling, seen)
	case *ast.StarExpr, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.InterfaceType:
		return "nil", true
	case *ast.ArrayType:
		if t.Len == nil {
			return "nil", true
		}
		return spelling + "{}", true
	case *ast.StructType:
		return spelling + "{}", true
	case *ast.Ident:
		if z.params[t.Name] {
			return "", false
		}
		if seen[t.Name] {
			return "", false
		}
		d := z.decls[t.Name]
		var nearest token.Pos
		for _, local := range z.locals {
			if local.name == t.Name && local.start <= z.position && z.position < local.end && local.start > nearest {
				d = local.expr
				nearest = local.start
			}
		}
		if d != nil {
			seen[t.Name] = true
			return z.zero(d, spelling, seen)
		}
		switch t.Name {
		case "any", "error":
			return "nil", true
		case "bool":
			return "false", true
		case "string":
			return `""`, true
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune", "float32", "float64", "complex64", "complex128":
			return "0", true
		}
	case *ast.SelectorExpr:
		id, ok := t.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		p := z.imports[id.Name]
		if p == "" {
			return "", false
		}
		pkg, ok := z.cache[p]
		if !ok {
			pkg, _ = exportImporter(z.s.fs).Import(p)
			z.cache[p] = pkg
		}
		if pkg == nil {
			return "", false
		}
		obj := pkg.Scope().Lookup(t.Sel.Name)
		if obj == nil {
			return "", false
		}
		return zeroUnderlying(obj.Type().Underlying(), spelling)
	case *ast.IndexExpr, *ast.IndexListExpr: // Instantiated named types retain the base's kind.
		var base ast.Expr
		if t, ok := e.(*ast.IndexExpr); ok {
			base = t.X
		} else {
			base = e.(*ast.IndexListExpr).X
		}
		return z.zero(base, spelling, seen)
	}
	return "", false
}
func zeroUnderlying(t types.Type, name string) (string, bool) {
	switch u := t.(type) {
	case *types.Basic:
		if u.Info()&types.IsBoolean != 0 {
			return "false", true
		}
		if u.Info()&types.IsString != 0 {
			return `""`, true
		}
		if u.Info()&types.IsNumeric != 0 {
			return "0", true
		}
	case *types.Array, *types.Struct:
		return name + "{}", true
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return "nil", true
	}
	return "", false
}
func results(ft *ast.FuncType) []ast.Expr {
	var out []ast.Expr
	if ft.Results != nil {
		for _, f := range ft.Results.List {
			n := len(f.Names)
			if n == 0 {
				n = 1
			}
			for i := 0; i < n; i++ {
				out = append(out, f.Type)
			}
		}
	}
	return out
}
func fallible(ft *ast.FuncType) bool {
	r := results(ft)
	if len(r) == 0 {
		return false
	}
	id, ok := r[len(r)-1].(*ast.Ident)
	return ok && id.Name == "error"
}
func (z *zeros) list(ft *ast.FuncType) ([]string, bool) {
	z.position = ft.Pos()
	z.params = map[string]bool{}
	ast.Inspect(z.s.file, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		if n.Pos() > ft.Pos() || n.End() < ft.End() {
			return false
		}
		var signature *ast.FuncType
		switch n := n.(type) {
		case *ast.FuncDecl:
			signature = n.Type
			if n.Recv != nil {
				ast.Inspect(n.Recv, func(e ast.Node) bool {
					switch e := e.(type) {
					case *ast.IndexExpr:
						if id, ok := e.Index.(*ast.Ident); ok {
							z.params[id.Name] = true
						}
					case *ast.IndexListExpr:
						for _, arg := range e.Indices {
							if id, ok := arg.(*ast.Ident); ok {
								z.params[id.Name] = true
							}
						}
					}
					return true
				})
			}
		case *ast.FuncLit:
			signature = n.Type
		}
		if signature != nil && signature.TypeParams != nil {
			for _, f := range signature.TypeParams.List {
				for _, id := range f.Names {
					z.params[id.Name] = true
				}
			}
		}
		return true
	})
	r := results(ft)
	if !fallible(ft) {
		return nil, false
	}
	var out []string
	for _, e := range r[:len(r)-1] {
		v, ok := z.zero(e, exprText(e), map[string]bool{})
		if !ok {
			return nil, false
		}
		out = append(out, v)
	}
	return out, true
}
func exprText(e ast.Expr) string { var b strings.Builder; _ = printerNode(&b, e); return b.String() }
