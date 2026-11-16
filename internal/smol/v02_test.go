package smol

import (
	"strings"
	"testing"
)

// §12 requires a fold, a nearest non-match and an interior-comment case for
// each §6 form. Expectations are spec-derived, not regenerated golden output.
func TestRequiredForms(t *testing.T) {
	type variant struct{ source, want, absent string }
	cases := []struct {
		section             string
		fold, near, comment variant
	}{
		{"6.1", variant{"import \"fmt\"\nfunc F(){fmt.Println(1)}", "fmt.Println(1)", "import"},
			variant{"import \"math/rand\"\nfunc F(){rand.Int()}", "import \"math/rand\"", ""},
			variant{"import (\n// keep\n\"fmt\"\n)\nfunc F(){fmt.Println(1)}", "import(", ""}},
		{"6.2", variant{"type T int\nfunc(x *T) F(){}\nfunc(x *T) G(){}", ".G()", ""},
			variant{"type T int\nfunc(x *T) F(){}\nfunc(y *T) G(){}", "(y*T)G()", ".G()"},
			variant{"type T int\nfunc(x /*keep*/ *T) F(){}\nfunc(x /*keep*/ *T) G(){}", "(x*T)G()", ".G()"}},
		{"6.3", variant{"func F()error{return nil}", "F()!", ""},
			variant{"func F()(err error){return}", "F()(err error)", "F()!"},
			variant{"func F()(/*keep*/error){return nil}", "F()error", "F()!"}},
		{"6.4", variant{"import \"context\"\nfunc F(ctx context.Context){}", "F(ctx)", ""},
			variant{"func F(ctx string){}", "F(ctx string)", ""},
			variant{"import \"context\"\nfunc F(ctx /*keep*/ context.Context){}", "F(ctx context.Context)", "F(ctx)"}},
		{"6.5", variant{"func F()error{\nerr:=read()\nif err!=nil{return err}\nreturn nil\n}", "err:=read()?", ""},
			variant{"func F()error{\nerr:=read()\nif err!=nil{return err}else{println(1)}\nreturn nil\n}", "else", "read()?"},
			variant{"func F()error{\nerr:=read()\n// keep\nif err!=nil{return err}\nreturn nil\n}", "if err!=nil", "read()?"}},
		{"6.6", variant{"func F()(int,error){return 0,Oops}", "!Oops", ""},
			variant{"func F()(float64,error){return 0.0,Oops}", "return 0.0,Oops", "!Oops"},
			variant{"func F()(int,error){return /*keep*/0,Oops}", "return 0,Oops", "!Oops"}},
		{"6.7", variant{"func F(){for _,v:=range xs {println(v)}}", "for v in xs", ""},
			variant{"func F(){for i:=range xs {println(i)}}", "for i:=range xs", "for i in"},
			variant{"func F(){for _,v:=/*keep*/range xs {println(v)}}", "range xs", " in xs"}},
		{"6.8", variant{"func F()int{\nreturn 1\n}", "F()int=>1", ""},
			variant{"func F()int{return 1}", "F()int{return 1}", "=>"},
			variant{"func F()int{\n// keep\nreturn 1\n}", "return 1", "=>"}},
		{"6.9", variant{"type T struct{\nA int `json:\"a\"`\nB int `json:\"b\"`\n}", "struct@json", ""},
			variant{"type T struct{\nA int `json:\"a\"`\nB int\n}", "`json:\"a\"`", "@json"},
			variant{"type T struct{\nA int /*keep*/ `json:\"a\"`\nB int `json:\"b\"`\n}", "`json:\"a\"`", "@json"}},
		{"6.10", variant{"import \"strings\"\nfunc F(s string)string{return strings.ToUpper(strings.TrimSpace(s))}", "use strings\n", ""},
			variant{"import \"strings\"\nfunc F(s string)string{return strings.ToUpper(s)}", "strings.ToUpper(s)", "use strings"},
			variant{"import \"strings\"\nfunc F(s string)string{return strings./*keep*/ToUpper(s)+strings.TrimSpace(s)}", "strings.ToUpper(s)", "use strings"}},
		{"6.11", variant{"type T struct{n int}\nfunc(t*T)F()int{return t.n}", "{return n}", ""},
			variant{"type T struct{n int}\nfunc(t*T)F(n int)int{return t.n+n}", "t.n+n", ""},
			variant{"type T struct{n int}\nfunc(t*T)F()int{return t./*keep*/n}", "t.n", "{return n}"}},
		{"6.12", variant{"func A(x int){}\nfunc B(x int){}\nfunc C(x int){}", "let x int\n", ""},
			variant{"func A(x int){}\nfunc B(x int){}", "A(x int)", "let "},
			variant{"func A(x /*keep*/ int){}\nfunc B(x int){}\nfunc C(x int){}", "A(x int)", "let "}},
		{"6.13", variant{"func F(a,b int){println(a,b)}", "println(a b)", ""},
			variant{"func F(a int){println(a,-1)}", "println(a,-1)", ""},
			variant{"func F(a,b int){println(a,/*keep*/b)}", "println(a,b)", "println(a b)"}},
	}
	for _, c := range cases {
		for _, v := range []struct {
			name string
			v    variant
		}{{"fold", c.fold}, {"near", c.near}, {"comment", c.comment}} {
			t.Run(c.section+"/"+v.name, func(t *testing.T) {
				src := "package p\n" + v.v.source + "\n"
				if err := Check(src, Options{}); err != nil {
					t.Fatal(err)
				}
				code, notes := packRound(t, c.section, src, Options{})
				if !strings.Contains(code, v.v.want) || (v.v.absent != "" && strings.Contains(code, v.v.absent)) {
					t.Fatalf("want %q; forbid %q:\n%s", v.v.want, v.v.absent, code)
				}
				if v.name == "comment" && !strings.Contains(notes, "keep") {
					t.Fatalf("comment lost:\n%s", notes)
				}
			})
		}
	}
}

func TestImportMarkerIsolation(t *testing.T) {
	for _, src := range []string{
		"package p\nimport \"fmt\"\nconst marker = `_ \"smol.invalid/inferred-imports\"`\nfunc F(){fmt.Println(marker)}\n",
		"package p\nimport \"fmt\"\n// _ \"smol.invalid/inferred-imports\"\nfunc F(){fmt.Println(1)}\n",
		"package p\nimport _ \"smol.invalid/inferred-imports\"\nvar X = 1\n",
		"package p\nimport (\n\"fmt\"\n_ \"unsafe\" //keep\n)\nvar __smol_internal_imports = 1\nfunc F(){fmt.Println(__smol_internal_imports)}\n",
	} {
		if err := Check(src, Options{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestArrowBoundaries(t *testing.T) {
	for _, c := range []struct{ body, want string }{
		{"var X any = func() any { return g }()", "func()any{return g}()"},
		{"var X any = func() any { return g } /*keep*/ ()", "func()any{return g}()"},
		{"var X any = func() any { return g() }", "func()any=>g()"},
		{"var X = func() int { return 1 } == nil", "func()int{return 1}==nil"},
		{"var X = func() int { return 1 } != nil", "func()int{return 1}!=nil"},
		{"var X = (func() int { return 1 })()", "(func()int=>1)()"},
		{"var X = call(func()(int,int){return pair()}, 2)", "func()(int,int){return pair()}"},
		{"var X = call(func()(int,int){return pair()} /*keep*/, 2)", "func()(int,int){return pair()}"},
		{"var X = call(func()(int,int){return 1,2}, 3)", "func()(int,int)=>1,2"},
	} {
		t.Run(c.body, func(t *testing.T) {
			src := "package p\nvar g func() any\n" + c.body + "\n"
			if err := Check(src, Options{}); err != nil {
				t.Fatal(err)
			}
			code := liftPacked(t, src)
			if !strings.Contains(code, c.want) {
				t.Fatalf("want %q:\n%s", c.want, code)
			}
		})
	}
}

func TestCodeOnlyImportBoundaries(t *testing.T) {
	for _, src := range []string{
		"package p\nimport \"fmt\"\n",
		"package p\nimport \"fmt\"\nfunc F(fmt struct{Println func()}){fmt.Println()}\n",
		"package p\nimport \"fmt\"\n// strings docs\nimport \"strings\"\nfunc F(s string)string{return fmt.Sprint(strings.TrimSpace(s))}\n",
		"package p\nimport (\n\"fmt\"\n_ \"unsafe\"\n)\nfunc F(){fmt.Println(1)}\n",
		"package p\nimport \"fmt\"\n/*line input.go:10*/\nfunc F(){fmt.Println(1)}\n",
	} {
		if err := Check(src, Options{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCommentLayout(t *testing.T) {
	for _, comment := range []string{"/**/", "/*x*/", "/* keep */", "/* 世界 */", "/*" + strings.Repeat("x", 100) + "*/"} {
		for _, src := range []string{
			"package p\nimport \"strings\"\nfunc F(s string)string{return strings." + comment + "ToUpper(s)+strings.TrimSpace(s)}\n",
			"package p\nfunc A(x " + comment + " int){}\nfunc B(x int){}\nfunc C(x int){}\n",
		} {
			if err := Check(src, Options{}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestDeclarationCommentsAndTagKeys(t *testing.T) {
	for _, src := range []string{
		"package p\nfunc /*keep*/ F() {}\n",
		"package p\ntype T int\nfunc /*keep*/ (x*T) F(){}\n",
		"package p\ntype T struct /*keep*/ {\nA int `json:\"a\"`\nB int `json:\"b\"`\n}\n",
		"package p\ntype T struct{\nA int `foo-bar:\"a\"`\nB int `foo-bar:\"b\"`\n}\n",
	} {
		if err := Check(src, Options{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEOFDirectives(t *testing.T) {
	for _, tail := range []string{"//go:generate echo end", "// regular\n//go:generate echo one\n//go:generate echo two", "//line other.go:12", "//extern symbol"} {
		src := "package p\nconst A = 1\n" + tail + "\n"
		code, _ := packRound(t, "EOF", src, Options{})
		low, err := Lower(code, Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(tail, "\n") {
			if directive(line) && (!strings.Contains(code, line) || !strings.Contains(low, line)) {
				t.Fatalf("directive lost: %q\n%s\n%s", line, code, low)
			}
		}
	}
}

func TestInactiveLineDirective(t *testing.T) {
	src := "package p\nfunc F(){\n\t//line filename:line:col\nprintln(1)\n}\n"
	if err := Check(src, Options{}); err != nil {
		t.Fatal(err)
	}
	code, notes := packRound(t, "inactive line directive", src, Options{})
	if strings.Contains(code, "//line") || !strings.Contains(notes, "//line filename:line:col") {
		t.Fatalf("illustrative directive must be in notes:\n%s\n%s", code, notes)
	}
}

func TestEmbeddedPointerSubject(t *testing.T) {
	src := "package p\nimport \"bytes\"\ntype T struct{*bytes.Buffer}\nfunc(t*T)F()*bytes.Buffer{return t.Buffer}\n"
	code := liftPacked(t, src)
	if !strings.Contains(code, "{return Buffer}") {
		t.Fatal(code)
	}
}

func TestOutline(t *testing.T) {
	src := `// Package p docs.
package p

// detached, not documentation

// T docs.
type T struct {
// Field docs.
Field int // trailing
}
// F docs.
var F = func() int { return 12345 }
var G = func() func() int { return func() int { return 23456 } }()
var S = "__smol_outline_body"
// H docs.
func H() int { return 34567 }
`
	for _, docs := range []bool{false, true} {
		out, err := Outline(src, Options{}, docs)
		if err != nil {
			t.Fatal(err)
		}
		for _, absent := range []string{"12345", "23456", "34567", "=>", "return", "detached", "trailing", "\n\n"} {
			if strings.Contains(out, absent) {
				t.Fatalf("unexpected %q:\n%s", absent, out)
			}
		}
		for _, doc := range []string{"Package p docs.", "T docs.", "Field docs.", "F docs.", "H docs."} {
			if strings.Contains(out, doc) != docs {
				t.Fatalf("docs=%v, %q:\n%s", docs, doc, out)
			}
		}
		if !strings.Contains(out, `"__smol_outline_body"`) {
			t.Fatal(out)
		}
	}
}
