package enc

import (
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func normalized(t *testing.T, s string) string {
	t.Helper()
	b, e := format.Source([]byte(s))
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestDecodeForms(t *testing.T) {
	base := normalized(t, `package p
// A docs.
func A(x int) int {
 if x > 0 {
 return 1
 } else {
 return 2
 }
}
// B docs.
func B() int {
 return 1
}
`)
	for _, c := range []struct{ name, patch, want, absent string }{
		{"replace scoped", "@@ a.go\n== func A @ if x @ return 1\nreturn 3\n", "return 3", ""},
		{"block all branches", "@@ a.go\n== if x\n..\nreturn x\n", "return x", "else"},
		{"range", "@@ a.go\n== func A @ if x\n.. } else {\nif x == 0 {\n", "if x == 0", "else"},
		{"after block", "@@ a.go\n++ func A\n..\nfunc C() {}\n", "}\n\nfunc C()", ""},
		{"before docs", "@@ a.go\n^^ func B\nfunc C() {}\n", "func C() {}\n\n// B docs.", ""},
		{"delete docs", "@@ a.go\n-- func B\n..\n", "func A", "B docs"},
		{"replace keeps docs", "@@ a.go\n== func B\n..\nfunc B() int { return 9 }\n", "// B docs.\nfunc B()", ""},
		{"repeat file", "@@ a.go\n== func A @ if x @ return 1\nreturn 3\n@@ a.go\n== func B @ return\nreturn 4\n", "return 4", ""},
		{"rename", "@@ a.go\n>> b.go\n== func B @ return\nreturn 4\n", "return 4", ""},
		{"whole", "@@ a.go\n==\npackage p\nvar Z = 1\n", "var Z = 1", "func A"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, e := Decode(Tree{"a.go": base}, c.patch, Options{})
			if e != nil {
				t.Fatal(e)
			}
			p := "a.go"
			if c.name == "rename" {
				p = "b.go"
				if _, ok := r.Files["a.go"]; ok {
					t.Fatal("old path survives")
				}
			}
			got := r.Files[p]
			if !strings.Contains(got, c.want) || c.absent != "" && strings.Contains(got, c.absent) {
				t.Fatalf("want %q absent %q:\n%s", c.want, c.absent, got)
			}
		})
	}
}
func TestRejectAtomic(t *testing.T) {
	base := Tree{"a.go": normalized(t, "package p\nfunc A(){println(1)}\nfunc B(){println(1)}\n")}
	cases := map[string]string{
		"zero": "@@ a.go\n-- missing\n", "double": "@@ a.go\n== func\nfunc C(){}\n", "no scope": "@@ a.go\n== package @ func\nfunc C(){}\n",
		"end region": "@@ a.go\n== func B\n.. func A\nfunc C(){}\n", "overlap": "@@ a.go\n-- func A\n== func A\nfunc C(){}\n",
		"empty replacement": "@@ a.go\n== func A\n", "empty insert": "@@ a.go\n++ func A\n", "delete body": "@@ a.go\n-- func A\nx\n",
		"rename body": "@@ a.go\n>> b.go\nx\n", "existing body": "@@ a.go\npackage p\n", "before header": "prose\n@@ a.go\n--\n",
		"non immediate range": "@@ a.go\n== func A\n\n..\nfunc C(){}\n", "before range": "@@ a.go\n^^ func A\n..\nfunc C(){}\n",
		"no anchor": "@@ a.go\n++\nx\n", "path": "@@ ../bad.go\npackage p\n", "absolute": "@@ /bad.go\npackage p\n",
		"unresolved": "@@ a.go\n++ func A\nfunc C(){unknown.Do()}\n", "syntax": "@@ a.go\n++ func A\nfunc C( {\n",
		"rename existing": "@@ a.go\n>> a.go\n", "partial multi": "@@ b.go\npackage p\nvar X=1\n@@ a.go\n-- missing\n",
	}
	for name, patch := range cases {
		t.Run(name, func(t *testing.T) {
			r, e := Decode(base, patch, Options{})
			if e == nil || r != nil {
				t.Fatalf("accepted bad patch: %v", r)
			}
			if !strings.Contains(e.Error(), "patch:") {
				t.Fatal(e)
			}
			if base["a.go"] != normalized(t, "package p\nfunc A(){println(1)}\nfunc B(){println(1)}\n") {
				t.Fatal("base mutated")
			}
		})
	}
}
func TestLiteralAndCommentLines(t *testing.T) {
	content := "package p\nvar S = `start\n"
	for _, m := range []string{"@@", "==", "--", "++", "^^", "..", ">>"} {
		content += m + " example\n"
	}
	content += "\\keep\n  raw indent\n`\n/* block\n  keep this\n*/\n"
	patch := "@@ new.go\n" + bodyText(lines(content), E1)
	r, e := Decode(Tree{}, patch, Options{})
	if e != nil {
		t.Fatal(e)
	}
	if r.Files["new.go"] != normalized(t, content) {
		t.Fatalf("literal changed:\n%s", r.Files["new.go"])
	}
	for _, a := range []string{"raw indent", "keep this", "== example"} {
		if _, e := Decode(r.Files, "@@ new.go\n-- "+a+"\n", Options{}); e == nil {
			t.Fatalf("anchored literal interior %q", a)
		}
	}
	fence := "```patch\r\n" + strings.ReplaceAll(patch, "\n", "\r\n") + "```\r\n"
	if _, e := Decode(Tree{}, fence, Options{}); e != nil {
		t.Fatal(e)
	}
}
func TestOriginalCoordinatesAndInsertionOrder(t *testing.T) {
	base := Tree{"a.go": "package p\nvar A=1\nvar B=2\n"}
	r, e := Decode(base, "@@ a.go\n++ var A\nvar C=3\n++ var A\nvar D=4\n== var B\nvar B=9\n", Options{})
	if e != nil {
		t.Fatal(e)
	}
	s := r.Files["a.go"]
	if !(strings.Index(s, "var C") < strings.Index(s, "var D") && strings.Contains(s, "var B=9")) {
		t.Fatal(s)
	}
}
func TestImportsAndPackages(t *testing.T) {
	for _, c := range []struct {
		name, source string
		tree         Tree
		opts         Options
		want         string
	}{
		{name: "standard", source: "package p\nfunc F(){fmt.Println(1)}\n", want: `"fmt"`},
		{name: "unused", source: "package p\nimport \"fmt\"\nvar A=1\n", want: "var A"},
		{name: "blank dot", source: "package p\nimport _ \"unsafe\"\nimport . \"math\"\nvar A=Pi\n", want: `. "math"`},
		{name: "sibling", source: "package p\nvar A=foo.Value\n", tree: Tree{"s.go": "package p\nimport foo \"example.org/foo\"\nvar B=foo.Other\n"}, want: `"example.org/foo"`},
		{name: "table", source: "package p\nvar A=foo.Value\n", opts: Options{Imports: map[string]string{"foo": "example.org/foo"}}, want: `"example.org/foo"`},
		{name: "deps", source: "package p\nvar A=foo.Value\n", opts: Options{Packages: map[string][]string{"foo": {"example.org/foo"}}}, want: `"example.org/foo"`},
		{name: "local", source: "package p\nfunc F(){fmt:=struct{X int}{};println(fmt.X)}\n", want: "fmt.X"},
		{name: "package scope", source: "package p\nvar A=fmt.X\n", tree: Tree{"s.go": "package p\nvar fmt struct{X int}\n"}, want: "fmt.X"},
		{name: "hint in body", source: "package p\nfunc F(){\nimport foo \"example.org/foo\"\nfoo.Call()\n}\n", want: `foo "example.org/foo"`},
		{name: "cgo", source: "package p\n/*\n#include <stdio.h>\n*/\nimport \"C\"\nfunc F(){C.puts(nil)}\n", want: "#include <stdio.h>"},
		{name: "build", source: "//go:build linux\n\npackage p\nvar A=1\n", want: "//go:build linux\n\npackage p"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tree := c.tree
			if tree == nil {
				tree = Tree{}
			}
			r, e := Decode(tree, "@@ a.go\n"+bodyText(lines(c.source), E1), c.opts)
			if e != nil {
				t.Fatal(e)
			}
			s := r.Files["a.go"]
			if !strings.Contains(s, c.want) {
				t.Fatal(s)
			}
			if c.name == "unused" && strings.Contains(s, `"fmt"`) {
				t.Fatal(s)
			}
			if (c.name == "local" || c.name == "package scope") && strings.Contains(s, "import") {
				t.Fatal(s)
			}
		})
	}
}
func TestE2AndPlainFixedPoint(t *testing.T) {
	plain := []string{"package p\nfunc F(x int)int{return ^x}\n", "package p\nfunc F(x bool)bool{return !x}\n", "package p\nfunc F(in []int){for _,v:=range in{println(v)}}\n"}
	for _, src := range plain {
		for _, p := range []Profile{E0, E1, E2} {
			r, e := Decode(Tree{}, "@@ a.go\n"+bodyText(lines(src), E0), Options{Profile: p})
			if e != nil {
				t.Fatal(e)
			}
			if r.Files["a.go"] != normalized(t, src) {
				t.Fatal(r.Files["a.go"])
			}
		}
	}
	for _, src := range []string{"package p\nfunc F() error {\nf()?\n^\n}\n", "package p\nfunc F() int! {\nx:=f()?\n^x\n}\n", "package p\nfunc F(xs []int){for v in xs {println(v)}}\n", "package p\nfunc F()int=>1\n", "package p\nfunc F()(int,error){\n!oops\n}\n"} {
		r, e := Decode(Tree{}, "@@ a.go\n"+bodyText(lines(src), E0), Options{Profile: E2})
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(r.Files["a.go"], "?") {
			t.Fatal(r.Files["a.go"])
		}
	}
	for _, src := range []string{"package p\nF(){}\n", "package p\nuse fmt\nfunc F(){}\n", "package p\nfunc F(){g(a b)}\n", "package p\ntype T struct @json {A int}\n", "package p\nfunc F(){f()?}\n"} {
		profile := E2
		if strings.Contains(src, "f()?") {
			profile = E1
		}
		if _, e := Decode(Tree{}, "@@ a.go\n"+bodyText(lines(src), E0), Options{Profile: profile}); e == nil {
			t.Fatalf("accepted excluded syntax %s", src)
		}
	}
}
func TestUnformattedLocality(t *testing.T) {
	src := "package p\nfunc F() {\n  x:=1\n  println(x)\n}\n"
	r, e := Decode(Tree{"a.go": src}, "@@ a.go\n++ x:=1\nprintln(2)\n", Options{})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Files["a.go"], "  x:=1\n\tprintln(2)\n  println(x)") {
		t.Fatal(r.Files["a.go"], r.Warnings)
	}
}
func TestWriteTreeAndDryDiff(t *testing.T) {
	dir := t.TempDir()
	base := Tree{"a.go": "package p\n"}
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(base["a.go"]), 0600)
	next := Tree{"new/b.go": "package p\n"}
	if e := WriteTree(dir, base, next); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(dir, "a.go")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	if !strings.Contains(Diff(base, next), "+++ b/new/b.go") {
		t.Fatal(Diff(base, next))
	}
	os.Symlink(t.TempDir(), filepath.Join(dir, "outside"))
	if e := WriteTree(dir, next, Tree{"outside/x.go": "package p\n"}); e == nil {
		t.Fatal("followed symlink")
	}
}
func TestEncoderRecovery(t *testing.T) {
	base := Tree{"a.go": normalized(t, "package p\nfunc A(){println(1)}\nfunc B(){println(2)}\n")}
	target := Tree{"a.go": normalized(t, "package p\nfunc A(){println(3)}\nfunc C(){println(4)}\nfunc B(){println(2)}\n"), "new.go": "package p\nvar N=1\n"}
	if _, e := Check(base, target, true); e != nil {
		t.Fatal(e)
	}
}

func TestFailureDiagnosticsAndLocality(t *testing.T) {
	src := normalized(t, "package p\nfunc A(){\nprintln(1)\nprintln(2)\n}\nfunc B(){\nprintln(1)\n}\n")
	_, e := Decode(Tree{"a.go": src}, "@@ a.go\n-- println(1)\n", Options{})
	if e == nil || !strings.Contains(e.Error(), "func A()") || !strings.Contains(e.Error(), "func B()") {
		t.Fatalf("missing scope candidates: %v", e)
	}
	_, e = Decode(Tree{"a.go": src}, "@@ a.go\n++ println(2)\nvar =\n", Options{})
	if e == nil || !strings.Contains(e.Error(), "patch:3:") {
		t.Fatalf("wrong patch line: %v", e)
	}
	_, e = Decode(Tree{"a.go": src}, "@@ a.go\n-- func A\n..\n++ println(2)\nprintln(9)\n", Options{})
	if e == nil {
		t.Fatal("accepted interior insertion")
	}
	// Existing adjacent declarations stay adjacent when the patch edits elsewhere.
	src = "package p\nfunc A(){\nprintln(1)\n}\nfunc B(){\nprintln(2)\n}\n"
	r, e := Decode(Tree{"a.go": src}, "@@ a.go\n== println(1)\nprintln(3)\n", Options{})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(r.Files["a.go"], "}\n\nfunc B") {
		t.Fatal("spaced untouched declaration boundary")
	}
}
func TestE2ReturnValues(t *testing.T) {
	for _, c := range []struct{ code, want string }{
		{"func F()(int,error){\n^1\n}", "return 1, nil"},
		{"func F()(int,error){\n!oops\n}", "return 0, oops"},
		{"func F()error{\nf()? \"reading\"\n^\n}", `fmt.Errorf("reading: %w", err)`},
		{"func F()error{\nf()? { println(err); return err }\n^\n}", "println(err)"},
	} {
		r, e := Decode(Tree{}, "@@ a.go\npackage p\n"+c.code+"\n", Options{Profile: E2})
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(r.Files["a.go"], c.want) {
			t.Fatal(r.Files["a.go"])
		}
	}
	for _, code := range []string{"func F(){\n^1\n}", "func F[T any]()(T,error){\nf()?\n^v\n}"} {
		if _, e := Decode(Tree{}, "@@ a.go\npackage p\n"+code+"\n", Options{Profile: E2}); e == nil {
			t.Fatal("accepted invalid E2 return")
		}
	}
}

func TestDeclarationSpacingAndImportOrder(t *testing.T) {
	r, e := Decode(Tree{}, "@@ a.go\npackage p\nimport \"strings\"\nfunc A(){\nfmt.Println(strings.TrimSpace(\"x\"))\n}\n// B docs\nfunc B(){}\n", Options{})
	if e != nil {
		t.Fatal(e)
	}
	got := r.Files["a.go"]
	if strings.Count(got, "import") != 1 || !strings.Contains(got, "\"fmt\"\n\t\"strings\"") || !strings.Contains(got, "}\n\n// B docs") {
		t.Fatal(got)
	}
	r, e = Decode(Tree{}, "@@ a.go\npackage p\nimport . \"time\"\nvar X = Local.String()\n", Options{})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(r.Files["a.go"], `"Local"`) {
		t.Fatal(r.Files["a.go"])
	}
}

func TestNestedScopesUnicodeAndDeterminism(t *testing.T) {
	src := normalized(t, "package p\nfunc A(){\nif ok {\nprintln(1)\n}\n}\nfunc B(){\nif ok {\nprintln(1)\n}\n}\nvar 世界 = 1\n")
	patch := "@@ a.go\n== func A @ if ok @ println\nprintln(2)\n== var 世 界\nvar 世界=3\n"
	a, e := Decode(Tree{"a.go": src}, patch, Options{})
	if e != nil {
		t.Fatal(e)
	}
	reverse := "@@ a.go\n== var 世界\nvar 世界=3\n== func A @ if ok @ println\nprintln(2)\n"
	b, e := Decode(Tree{"a.go": src}, reverse, Options{})
	if e != nil {
		t.Fatal(e)
	}
	if !equalTrees(a.Files, b.Files) {
		t.Fatal("operation order changes result")
	}
	if _, e := Decode(Tree{"a.go": src}, "@@ a.go\n== func A @ println\n.. var 世界\nprintln(3)\n", Options{}); e == nil {
		t.Fatal("range escaped inner block")
	}
}
func TestImportAmbiguityAndPrecedence(t *testing.T) {
	source := "@@ a.go\npackage p\nvar X = foo.X\n"
	options := Options{Packages: map[string][]string{"foo": {"a/foo", "b/foo"}}}
	_, e := Decode(Tree{}, source, options)
	if e == nil || !strings.Contains(e.Error(), "a/foo") || !strings.Contains(e.Error(), "b/foo") {
		t.Fatalf("candidates missing: %v", e)
	}
	options.Imports = map[string]string{"foo": "c/foo"}
	r, e := Decode(Tree{}, source, options)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(r.Files["a.go"], `"c/foo"`) {
		t.Fatal(r.Files["a.go"])
	}
	_, e = Decode(Tree{}, "@@ a.go\npackage p\nimport foo \"a/foo\"\nimport foo \"b/foo\"\nvar X=foo.X\n", Options{})
	if e == nil {
		t.Fatal("chose conflicting explicit import")
	}
	options = Options{Packages: map[string][]string{"yaml": {"gopkg.in/yaml.v3"}}}
	r, e = Decode(Tree{}, "@@ a.go\npackage p\nimport \"gopkg.in/yaml.v3\"\nvar X=yaml.Marshal\n", options)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Count(r.Files["a.go"], "gopkg.in/yaml.v3") != 1 {
		t.Fatal(r.Files["a.go"])
	}
}

func TestApprovedNormalizationClarifications(t *testing.T) {
	base := Tree{"a.go": "package p\nfunc F(x int)int{\nswitch x{\ncase 1:\nreturn 1\n}\nreturn 0\n}\n"}
	target := Tree{"a.go": base["a.go"] + "func G(){}\n"}
	if _, e := Check(base, target, false); e != nil {
		t.Fatal(e)
	}
	patch, e := Encode(base, target, Options{})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(patch, "\n==\n") {
		t.Fatal("unformatted base should use explicit whole-file replacement", patch)
	}
	if _, e := Check(Tree{}, Tree{"a.go": "package p\nfunc A(){\nprintln(1)\n}\nfunc B(){}\n"}, false); e != nil {
		t.Fatal(e)
	}
}

func TestE2FormattedHandlerBindings(t *testing.T) {
	_, e := Decode(Tree{}, "@@ a.go\npackage p\nfunc F(fmt struct{ Errorf func(string,...any)error })error{\nf()? \"context\"\n^\n}\n", Options{Profile: E2})
	if e == nil {
		t.Fatal("formatted handler captured a local fmt")
	}
	r, e := Decode(Tree{}, "@@ a.go\npackage p\nimport f \"fmt\"\nfunc F()error{\nf2()? \"context\"\n^\n}\n", Options{Profile: E2})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(r.Files["a.go"], "f.Errorf(") {
		t.Fatal(r.Files["a.go"])
	}
}

func TestConcurrentIndependentDecoders(t *testing.T) {
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			_, e := Decode(Tree{}, "@@ a.go\npackage p\nfunc F()error{\nf()? \"context\"\n^\n}\n", Options{Profile: E2})
			done <- e
		}()
	}
	for i := 0; i < 8; i++ {
		if e := <-done; e != nil {
			t.Fatal(e)
		}
	}
}

func TestImportHintsAfterDeclarations(t *testing.T) {
	patch := "@@ a.go\n// package is mentioned in docs.\npackage p\nfunc F(){fmt.Println(1)}\nimport \"fmt\"\n"
	r, e := Decode(Tree{}, patch, Options{})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(r.Files["a.go"], "package p\n\nimport") {
		t.Fatal(r.Files["a.go"])
	}
}
