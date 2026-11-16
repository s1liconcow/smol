// Run with: go run ./scripts/validate_v02.go -output measurements/spec-v0.2-validation/probes.json
// This is an audit, not a golden-output generator: expectations come from SPEC.md.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"runtime"
	"strings"
	"time"

	"smolang/internal/smol"
)

type result struct {
	ID      string `json:"id"`
	Section string `json:"section"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	Source  string `json:"source,omitempty"`
}

type probe struct {
	id, section, source string
	want, absent        []string
}

// programTokens ignores ordinary comments and layout, but retains directives
// and the cgo preamble. No implementation lexer is used for this comparison.
func programTokens(src string) []string {
	fs := token.NewFileSet()
	file, _ := parser.ParseFile(fs, "source.go", src, parser.ParseComments)
	cgo := map[string]bool{}
	if file != nil {
		for _, decl := range file.Decls {
			if d, ok := decl.(*ast.GenDecl); ok && d.Tok == token.IMPORT {
				for _, spec := range d.Specs {
					im := spec.(*ast.ImportSpec)
					if im.Path.Value == `"C"` {
						for _, group := range []*ast.CommentGroup{d.Doc, im.Doc} {
							if group != nil {
								for _, c := range group.List {
									cgo[c.Text] = true
								}
							}
						}
					}
				}
			}
		}
	}
	f := fs.AddFile("tokens.go", -1, len(src))
	var sc scanner.Scanner
	sc.Init(f, []byte(src), nil, scanner.ScanComments)
	var out []string
	for {
		_, k, lit := sc.Scan()
		if k == token.EOF {
			break
		}
		if k == token.COMMENT {
			keep := cgo[lit]
			for _, prefix := range []string{"//go:", "//line ", "/*line ", "//export ", "//extern ", "// +build"} {
				keep = keep || strings.HasPrefix(lit, prefix)
			}
			if !keep {
				continue
			}
		}
		out = append(out, k.String()+":"+lit)
	}
	return out
}

func codeFor(src string) (string, string, error) {
	w, err := smol.Lift(src, smol.Options{})
	if err != nil {
		return "", "", err
	}
	c, n := smol.Pack(w)
	return c, n, nil
}

func main() {
	output := flag.String("output", "measurements/spec-v0.2-validation/probes.json", "JSON audit report")
	flag.Parse()
	start := time.Now()
	var results []result
	record := func(id, section, source string, check func() error) {
		r := result{ID: id, Section: section, Status: "PASS"}
		if err := check(); err != nil {
			r.Status = "FAIL"
			r.Error = err.Error()
			r.Source = source
		}
		results = append(results, r)
		fmt.Printf("%s %s\n", r.Status, r.ID)
	}
	probes := []probe{
		{"open_fold", "6.10", "package p\nimport \"strings\"\nfunc F(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }\n", []string{"use strings\n", "ToUpper(TrimSpace(s))"}, nil},
		{"open_one_use", "6.10", "package p\nimport \"strings\"\nfunc F(s string) string { return strings.ToUpper(s) }\n", []string{"strings.ToUpper(s)"}, []string{"use strings"}},
		{"open_local_shadow", "6.10", "package p\nimport \"strings\"\nfunc F(s string) string { ToUpper := s; return ToUpper+strings.ToUpper(strings.TrimSpace(s)) }\n", []string{"strings.ToUpper("}, nil},
		{"open_comment", "6.10", "package p\nimport \"strings\"\nfunc F(s string) string { return strings./*keep*/ToUpper(s)+strings.TrimSpace(s) }\n", []string{"strings.ToUpper("}, []string{"use strings"}},
		{"open_composite_key", "6.10", "package p\nimport \"reflect\"\nvar X = map[reflect.Kind]int{reflect.Int: 1}\nfunc F() reflect.Kind { return reflect.Int }\n", []string{"reflect.Int:"}, nil},
		{"subject_fold", "6.11", "package p\ntype T struct { n int }\nfunc (t *T) F() int { return t.n }\n", []string{"{return n}"}, nil},
		{"subject_shadow", "6.11", "package p\ntype T struct { n int }\nfunc (t *T) F(n int) int { return t.n+n }\n", []string{"t.n+n"}, nil},
		{"subject_comment", "6.11", "package p\ntype T struct { n int }\nfunc (t *T) F() int { return t./*keep*/n }\n", []string{"t.n"}, nil},
		{"subject_nested_closure", "6.11", "package p\ntype T struct { n int }\nfunc (t *T) F() func() int { return func() int { return t.n } }\n", []string{"=>n"}, []string{"t.n"}},
		{"subject_promoted_near_miss", "6.11", "package p\ntype B struct { n int }\ntype T struct { B }\nfunc (t *T) F() int { return t.n }\n", []string{"t.n"}, nil},
		{"subject_embedded_pointer", "6.11", "package p\nimport \"bytes\"\ntype T struct { *bytes.Buffer }\nfunc (t *T) F() *bytes.Buffer { return t.Buffer }\n", []string{"{return Buffer}"}, []string{"t.Buffer"}},
		{"convention_three", "6.12", "package p\nfunc A(x int) {}\nfunc B(x int) {}\nfunc C(x int) {}\n", []string{"let x int\n", "A(x)"}, nil},
		{"convention_two", "6.12", "package p\nfunc A(x int) {}\nfunc B(x int) {}\n", []string{"A(x int)"}, []string{"let "}},
		{"convention_comment", "6.12", "package p\nfunc A(x /*keep*/ int) {}\nfunc B(x int) {}\nfunc C(x int) {}\n", []string{"A(x int)"}, []string{"let "}},
		{"convention_tie", "6.12", "package p\nfunc A(x string) {}\nfunc B(x string) {}\nfunc C(x string) {}\nfunc D(x int) {}\nfunc E(x int) {}\nfunc F(x int) {}\n", []string{"let x int\n"}, nil},
		{"convention_inherited_receiver_not_counted", "6.12", "package p\ntype T struct{}\nfunc (x *T) A() {}\nfunc (x *T) B() {}\nfunc (x *T) C() {}\n", []string{".B()", ".C()"}, []string{"let x"}},
		{"comma_fold", "6.13", "package p\nfunc F(a,b int) { println(a,b) }\n", []string{"println(a b)"}, nil},
		{"comma_operator_near_miss", "6.13", "package p\nfunc F(a int) { println(a,-1) }\n", []string{"println(a,-1)"}, nil},
		{"comma_comment", "6.13", "package p\nfunc F(a,b int) { println(a,/*keep*/b) }\n", []string{"println(a,b)"}, nil},
		{"comma_case", "6.13", "package p\nfunc F(n int) { switch n { case 0,1: println(n) } }\n", []string{"case 0 1:"}, nil},
		{"contextual_identifiers", "5", "package p\nfunc use() {}\nfunc let() {}\nfunc F(in int) int { return in }\n", []string{"use()", "let()", "F(in int)"}, nil},
		{"partial_import", "6.1", "package p\nimport (\n\"fmt\"\n_ \"unsafe\" //keep\n)\nfunc F() { fmt.Println(1) }\n", []string{"import(\n...\n_ \"unsafe\""}, nil},
		{"partial_import_blank_near_miss", "6.1", "package p\nimport (\n\"fmt\"\n\n_ \"unsafe\" //keep\n)\nfunc F() { fmt.Println(1) }\n", []string{"\"fmt\""}, []string{"..."}},
		{"inline_fold", "5", "package p\nfunc F(x int) {\nif x>0 {\nprintln(x)\n}\n}\n", []string{"if x>0{println(x)}"}, nil},
		{"inline_comment", "5", "package p\nfunc F(x int) {\nif x>0 {\n//keep\nprintln(x)\n}\n}\n", []string{"if x>0{\n"}, nil},
		{"implied_break", "5", "package p\nfunc F(x int) int {\ny:=x\nreturn y\n}\n", []string{"y:=x return y"}, nil},
	}
	for _, p := range probes {
		record(p.id, p.section, p.source, func() error {
			if err := smol.Check(p.source, smol.Options{}); err != nil {
				return err
			}
			c, _, err := codeFor(p.source)
			if err != nil {
				return err
			}
			for _, want := range p.want {
				if !strings.Contains(c, want) {
					return fmt.Errorf("missing %q in:\n%s", want, c)
				}
			}
			for _, absent := range p.absent {
				if strings.Contains(c, absent) {
					return fmt.Errorf("unexpected %q in:\n%s", absent, c)
				}
			}
			return nil
		})
	}
	marker := "package p\nimport \"fmt\"\nconst marker = `_ \"smol.invalid/inferred-imports\"`\nfunc F() { fmt.Println(marker) }\n"
	record("import_marker_literal_roundtrip", "3/L1", marker, func() error { return smol.Check(marker, smol.Options{}) })
	for _, p := range []probe{
		{id: "directives_and_cgo_without_notes", section: "3/L4", source: "//go:build darwin || linux\n\npackage p\n/*\n#include <stdio.h>\n*/\nimport \"C\"\n// F doc.\n//go:noinline\nfunc F() int { return int(C.EOF) }\n"},
		{id: "trailing_directive_without_notes", section: "3/L4", source: "package p\nconst A = 1\n//go:generate echo end\n"},
		{id: "code_only_program_tokens", section: "3/L4", source: "package p\nimport \"strings\"\n// F doc.\nfunc F(x string) string { return strings.ToUpper(strings.TrimSpace(x)) }\n"},
	} {
		record(p.id, p.section, p.source, func() error {
			c, _, err := codeFor(p.source)
			if err != nil {
				return err
			}
			low, err := smol.Lower(c, smol.Options{})
			if err != nil {
				return err
			}
			n, err := smol.Normalize(p.source)
			if err != nil {
				return err
			}
			if strings.Join(programTokens(n), "\n") != strings.Join(programTokens(low), "\n") {
				return fmt.Errorf("Go tokens/directives differ without notes:\n%s", low)
			}
			return nil
		})
	}
	outline := "package p\nvar F = func() int { return 1 }\nfunc G() int { return 2 }\n"
	record("outline_removes_literal_bodies", "9", outline, func() error {
		out, err := smol.Outline(outline, smol.Options{}, false)
		if err != nil {
			return err
		}
		if strings.Contains(out, "=>") || strings.Contains(out, "return") {
			return fmt.Errorf("function literal body remains:\n%s", out)
		}
		return nil
	})
	docs := "package p\n// F doc.\nfunc F(x int) int { return x }\n"
	record("outline_docs_dense_layout", "9", docs, func() error {
		out, err := smol.Outline(docs, smol.Options{}, true)
		if err != nil {
			return err
		}
		if !strings.Contains(out, "// F doc.\nF(x int)int") || strings.Contains(out, "\n\n") {
			return fmt.Errorf("outline is not dense code plus docs:\n%s", out)
		}
		return nil
	})
	for _, changed := range []bool{false, true} {
		id := "notes_unchanged"
		if changed {
			id = "notes_after_edit"
		}
		src := "package p\n// F doc.\nfunc F(x int) int {\n// inside\nreturn x\n}\n"
		record(id, "4.1", src, func() error {
			c, n, err := codeFor(src)
			if err != nil {
				return err
			}
			if changed {
				c = strings.Replace(c, "return x", "x++\nreturn x", 1)
			}
			wide, warnings, err := smol.Unpack(c, n)
			if err != nil {
				return err
			}
			if (len(warnings) > 0) != changed {
				return fmt.Errorf("unexpected warnings: %v", warnings)
			}
			if !strings.Contains(wide, "// inside") || !strings.Contains(wide, "// F doc.") {
				return fmt.Errorf("comments lost: %s", wide)
			}
			_, err = smol.Lower(wide, smol.Options{})
			return err
		})
	}
	passed := 0
	for _, r := range results {
		if r.Status == "PASS" {
			passed++
		}
	}
	report := map[string]any{"spec": "SPEC.md v0.2 draft", "go_version": runtime.Version(), "passed": passed, "failed": len(results) - passed, "total": len(results), "elapsed_seconds": time.Since(start).Seconds(), "results": results, "scope": "Targeted spec-derived probes, not an exhaustive requirement count. FAIL means observed divergence, not an accepted exception."}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(*output, append(b, '\n'), 0644); err != nil {
		panic(err)
	}
	if passed != len(results) {
		os.Exit(1)
	}
}
