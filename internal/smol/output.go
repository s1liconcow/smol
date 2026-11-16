package smol

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// OutputImports is the unambiguous standard-package table used by output E1.
func OutputImports() (map[string]string, map[string]bool) {
	names := map[string]string{}
	paths := map[string]bool{}
	for n, im := range standard() {
		names[n] = im.Path
	}
	for p := range stdPaths {
		paths[p] = true
	}
	return names, paths
}

// LowerOutput accepts only the writer-local E2 extensions. Valid Go is a fixed
// point: it bypasses every Smol layout, scope and naming transformation.
func LowerOutput(text string, files map[string]string, imports map[string]string) (string, error) {
	if parsed, err := parse(text); err == nil {
		extension := false
		ast.Inspect(parsed.file, func(n ast.Node) bool {
			if stmt, ok := n.(*ast.ExprStmt); ok {
				if u, ok := stmt.X.(*ast.UnaryExpr); ok && (u.Op.String() == "^" || u.Op.String() == "!") {
					extension = true
				}
			}
			return true
		})
		if !extension {
			return text, nil
		}
	}
	prefix := "__smol_output_"
	for strings.Contains(text, prefix) {
		prefix += "x"
	}
	var err error
	for _, phase := range []func(string) (string, error){func(t string) (string, error) { return lowerSignatures(t, nil) }, lowerRanges, lowerArrows, func(t string) (string, error) { return lowerStatements(t, prefix) }} {
		text, err = phase(text)
		if err != nil {
			return text, err
		}
	}
	s, err := parse(text)
	if err != nil {
		return text, err
	}
	o := Options{PackageFiles: files, Imports: map[string]Import{}}
	for q, p := range imports {
		o.Imports[q] = Import{Path: p}
	}
	qualifier := "fmt"
	explicitFmt := false
	for _, im := range s.file.Imports {
		if path, _ := strconv.Unquote(im.Path.Value); path == "fmt" {
			explicitFmt = true
			if im.Name != nil {
				qualifier = im.Name.Name
			}
		}
	}
	var trial []edit
	ast.Inspect(s.file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == prefix+"wrap" {
				trial = append(trial, edit{s.off(id.Pos()), s.off(id.End()), qualifier})
			}
		}
		return true
	})
	if len(trial) > 0 {
		if qualifier == "." || qualifier == "_" {
			return text, fmt.Errorf("E2 formatted handler requires a named fmt import; write the handler in Go")
		}
		facts, _ := newScopeFacts(text, o)
		if !explicitFmt && facts != nil && facts.ctx.names[qualifier] {
			return text, fmt.Errorf("E2 formatted handler qualifier %s is shadowed; write the handler in Go", qualifier)
		}
		trialText, e := apply(text, trial)
		if e != nil {
			return text, e
		}
		fs := token.NewFileSet()
		f, e := parser.ParseFile(fs, "output.go", trialText, 0)
		if e != nil {
			return text, e
		}
		shadowed := false
		ast.Inspect(f, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok && id.Name == qualifier && id.Obj != nil {
					shadowed = true
				}
			}
			return true
		})
		if shadowed {
			return text, fmt.Errorf("E2 formatted handler qualifier %s is shadowed; write the handler in Go", qualifier)
		}
		s.errorQualifier = qualifier
	}
	z := newZeros(s, o)
	var bodyErr error
	ast.Inspect(s.file, func(n ast.Node) bool {
		switch f := n.(type) {
		case *ast.FuncDecl:
			lowerBody(s, f.Body, f.Type, z, prefix, &bodyErr)
			return false
		case *ast.FuncLit:
			lowerBody(s, f.Body, f.Type, z, prefix, &bodyErr)
			return false
		}
		return true
	})
	if bodyErr != nil {
		return text, fmt.Errorf("E2: %w", bodyErr)
	}
	return s.render(0, len(text)), nil
}

// OutputExports resolves a dot import with an independent importer so parallel
// patch checks do not share mutable go/types importer state.
func OutputExports(path string) []string {
	p, e := makeExportImporter(token.NewFileSet()).Import(path)
	if e != nil {
		return nil
	}
	var out []string
	for _, name := range p.Scope().Names() {
		if ast.IsExported(name) {
			out = append(out, name)
		}
	}
	return out
}

// OutputPackageNames includes ambiguous standard names for explicitly written
// imports. Such names remain excluded from the implicit-import table.
func OutputPackageNames() map[string]string {
	standard()
	out := map[string]string{}
	for p, n := range stdNames {
		out[p] = n
	}
	return out
}
