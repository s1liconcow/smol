package smol

import (
	"os"
	"strings"
	"testing"
)

func TestSpecExample(t *testing.T) {
	g, _ := os.ReadFile("../../testdata/example/users.go")
	want, _ := os.ReadFile("../../testdata/example/users.smol")
	wantNotes, _ := os.ReadFile("../../testdata/example/users.smol.notes")
	wide, e := Lift(string(g), Options{})
	if e != nil {
		t.Fatal(e)
	}
	code, notes := Pack(wide)
	if code != string(want) {
		t.Errorf("code:\n%s\nwant:\n%s", code, want)
	}
	if notes != string(wantNotes) {
		t.Errorf("notes:\n%s\nwant:\n%s", notes, wantNotes)
	}
	back, warnings, e := Unpack(string(want), string(wantNotes))
	if e != nil || len(warnings) > 0 {
		t.Fatal(e, warnings)
	}
	low, e := Lower(back, Options{})
	if e != nil {
		t.Fatal(e)
	}
	n, _ := Normalize(string(g))
	if low != n {
		t.Errorf("lower:\n%s\nwant:\n%s", low, n)
	}
	// Without the notes the program is the same; only comments and blank lines go.
	bare, e := Lower(string(want), Options{})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(bare, "//") || !strings.Contains(bare, "func (s *Server) Emails(ctx context.Context) ([]string, error) {") {
		t.Errorf("lower without notes:\n%s", bare)
	}
}
func roundtrip(t *testing.T, g string) {
	t.Helper()
	n, e := Normalize(g)
	if e != nil {
		t.Fatal(e)
	}
	s, e := Lift(g, Options{})
	if e != nil {
		t.Fatal(e)
	}
	l, e := Lower(s, Options{})
	if e != nil {
		t.Fatalf("lower: %v\nSmol:\n%s", e, s)
	}
	if l != n {
		t.Fatalf("L1:\nSmol:\n%s\nGo:\n%s\nwant:\n%s", s, l, n)
	}
	f, e := Format(s, Options{})
	if e != nil {
		t.Fatal(e)
	}
	if f != s {
		t.Fatalf("L2/L3:\n%s\nwant:\n%s", f, s)
	}
}
func TestRoundTrips(t *testing.T) {
	cases := []string{
		`package p
func Add(a,b int) int { return a+b }
func Multi(a int) (int,string) {
return a,"hello"
}
`,
		`package p
import "context"
func F(ctx context.Context) error { return nil }
func G(ctx, x string) {}
type ctx int
var f func(ctx)
`,
		`package p
func F() (string,error) {
x,err:=read()
if err!=nil {return "",err}
return x,nil
}
func read()(string,error){return "",nil}
`,
		`package p
func F() (string,error) {
x,err:=read()
// stay unfolded
if err!=nil {return "",err}
return x,nil
}
func read()(string,error){return "",nil}
`,
		`package p
func F() (string,error) {
x,err:=read()

if err!=nil {return "",err}
return x,nil
}
func read()(string,error){return "",nil}
`,
		`package p
func F() (float64,error) {return 0.0,Oops}
func G[T any]() (T,error) {return *new(T),Oops}
`,
		`package p
func F() {
for _,v:=range xs {
println(v)
}
for i:=range xs {println(i)}
for k,v = range xs {println(k,v)}
}
`,
		"package p\ntype X struct {\nHTTPServer string `json:\"http_server\" db:\"http_server\"`\nUserID int `json:\"user_id\" db:\"user_id\"`\nCache int\nsecret string\nA,B int\n}\n",
		`package p
func F() {
f:=func(x int) int { return x+1 }
_ = f
}
`,
		"package p\nvar S = `hello\n\t   raw\n\n string`\n// a    comment\nfunc F() {\n// stay\nif true {\n// inside\nprintln(S)\n}\n}\n",
		`package p
type B bool
type S struct{X int}
type A [2]int
type N int
func F()(B,S,A,N,error) {
return false,S{},A{},0,Oops
}
`,
		`package p
func F()(int,error){
f:=func()(string,error){
x,err:=read()
if err!=nil{return "",err}
return x,nil
}
_,err:=f()
if err!=nil{return 0,err}
return 2,nil
}
func read()(string,error){return "",nil}
`,
	}
	for i, g := range cases {
		t.Run(string(rune('A'+i)), func(t *testing.T) { roundtrip(t, g) })
	}
}
func TestInvalid(t *testing.T) {
	for _, s := range []string{"package p\n.F() {}\n", "package p\nF(){\n^1\n}\n", "package p\nF[T any]() T! {\nx := f()?\n^x\n}\n", "package p\nF()! {\n!Oops\n}\n"} {
		if _, e := Lower(s, Options{}); e == nil {
			t.Errorf("accepted %s", s)
		}
	}
}
func TestMod(t *testing.T) {
	m, e := ReadMod("use pb = example.org/gen\nuse yaml gopkg.in/yaml.v3\n")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(WriteMod(m), "use pb = example.org/gen") {
		t.Fatal(m)
	}
}

// Spec-derived cases assert observable canonical folds and their nearest misses.
func TestForms(t *testing.T) {
	cases := []struct{ name, goText, contains, absent string }{
		{"6.1_std_import", `package p; import "fmt"; func F(){fmt.Println("hi")}`, `fmt.Println`, `import`},
		{"6.1_ambiguous_import", `package p; import "math/rand"; func F(){rand.Int()}`, `import "math/rand"`, ``},
		{"6.1_dot_import", `package p; import . "fmt"; func F(){Println("hi")}`, `import . "fmt"`, ``},
		{"6.1_blank_import", `package p; import _ "net/http/pprof"`, `import _ "net/http/pprof"`, ``},
		{"6.1_comment_import", "package p\nimport (\n// keep\n\"fmt\"\n)\nfunc F(){fmt.Println()}\n", `import`, ``},
		{"6.2_receiver", "package p\ntype S int\nfunc(s *S) A() {}\nfunc(s *S) B() {}\n", `.B()`, ``},
		{"6.2_receiver_miss", "package p\ntype S int\nfunc(s *S) A() {}\nfunc(q *S) B() {}\n", `(q *S) B()`, `.B()`},
		{"6.2_func_literal", "package p\nvar f=func(){}\n", `func()`, ``},
		{"6.3_error", "package p\nfunc F() error {return nil}\n", `F()! { ^ }`, ``},
		{"6.3_named", "package p\nfunc F()(e error) {return}\n", `F() (e error)`, `F()!`},
		{"6.3_func_type", "package p\ntype F func() (func() error,error)\nfunc G(){}\n", `func() (func()!)!`, ``},
		{"6.4_conventional", "package p\nimport \"context\"\nfunc F(ctx context.Context,id int){}\n", `F(ctx, id int)`, ``},
		{"6.4_typed", "package p\nfunc F(ctx int){}\n", `F(ctx int)`, `\ctx`},
		{"6.4_grouped", "package p\nfunc F(ctx,id int){}\n", `F(\ctx, id int)`, ``},
		{"6.4_comment", "package p\nimport \"context\"\nfunc F(ctx /*keep*/ context.Context){}\n", `ctx /*keep*/ context.Context`, ``},
		{"6.5_bare", "package p\nfunc F() (int,error){\nx,err:=read()\nif err!=nil{return 0,err}\nreturn x,nil\n}\n", `x := read()?`, ``},
		{"6.5_assignment", "package p\nfunc F() (int,error){\nvar x int\nx,err=read()\nif err!=nil{return 0,err}\nreturn x,nil\n}\n", `x = read()?`, ``},
		{"6.5_err_only", "package p\nfunc F() error {\nerr:=read()\nif err!=nil{return err}\nreturn nil\n}\n", `err := read()?`, ``},
		{"6.5_custom", "package p\nfunc F(){\nx,err:=read()\nif err!=nil{println(err);return}\nprintln(x)\n}\n", `x := read() ? {`, ``},
		{"6.5_comment", "package p\nfunc F() error {\nerr:=read()\n// keep\nif err!=nil{return err}\nreturn nil\n}\n", `// keep`, `read()?`},
		{"6.5_blank", "package p\nfunc F() error {\nerr:=read()\n\nif err!=nil{return err}\nreturn nil\n}\n", `if err != nil`, `read()?`},
		{"6.5_else", "package p\nfunc F() error {\nerr:=read()\nif err!=nil{return err}else{println(1)}\nreturn nil\n}\n", `else`, `read()?`},
		{"6.6_ok", "package p\nfunc F()(int,error){return 1,nil}\n", `^1`, ``},
		{"6.6_fail", "package p\nfunc F()(int,error){return 0,Oops}\n", `!Oops`, ``},
		{"6.6_noncanonical", "package p\nfunc F()(float64,error){return 0.0,Oops}\n", `return 0.0, Oops`, ``},
		{"6.6_comment", "package p\nfunc F()(int,error){return /*keep*/ 0,Oops}\n", `return /*keep*/`, ``},
		{"6.7_value", "package p\nfunc F(){for _,v:=range xs {println(v)}}\n", `for v in xs`, ``},
		{"6.7_index", "package p\nfunc F(){for i:=range xs {println(i)}}\n", `for i := range xs`, `for i in`},
		{"6.7_in_identifier", "package p\nfunc F(){for _,in:=range xs {println(in)}}\n", `for in in xs`, ``},
		{"6.7_comment", "package p\nfunc F(){for _,v:= /*keep*/ range xs {println(v)}}\n", `/*keep*/`, ` in xs`},
		{"6.8_decl", "package p\nfunc F() int {\nreturn 1\n}\n", `F() int => 1`, ``},
		{"6.8_oneline_decl", "package p\nfunc F() int {return 1}\n", `{ return 1 }`, `=>`},
		{"6.8_literal", "package p\nvar f=func() int {return 1}\n", `func() int => 1`, ``},
		{"6.8_comment", "package p\nfunc F() int {\n//keep\nreturn 1\n}\n", `// keep`, `=>`},
		{"7_type_parameter", "package p\nfunc F[T any]()(T,error){return *new(T),Oops}\n", `return *new(T), Oops`, `!Oops`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, e := Lift(c.goText, Options{})
			if e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(s, c.contains) || (c.absent != "" && strings.Contains(s, c.absent)) {
				t.Fatalf("unexpected canonical form:\n%s\nneed %q; forbid %q", s, c.contains, c.absent)
			}
			roundtrip(t, c.goText)
		})
	}
}

func TestCorpusRegressions(t *testing.T) {
	for _, g := range []string{
		"package p\nvar x=func()int{return 1}()\nfunc F(){}\n",
		"package p\ntype Fn func()error\nfunc F(){}\n",
		"package p\nfunc F()error{\nif err:=f();err!=nil{\n}\nreturn nil\n}\n",
		"package p\nfunc F()error{\nswitch x{\ncase 1:\nreturn nil\ncase 2:\nreturn Oops\n}\nreturn nil\n}\n",
		"package p\nfunc F()(int,error){\nreturn 1,nil //keep\n}\n",
		"package p\nfunc F()(int,error){\nif true{ //keep\nreturn 0,Oops\n}\nreturn 1,nil\n}\n",
		"package p\nfunc F(){if _,ok:=m[k];!ok {println(1)}}\n",
		"package p\nfunc F(){\nswitch x{\ncase 1:\n// body comment\ncase 2:\nprintln(2)\n}\n}\n",
		"package p // import \"example.org/p\"\nimport \"fmt\"\nfunc F(){fmt.Println()}\n",
		"package p\n// file notes\n\nimport \"fmt\"\nfunc F(){fmt.Println()}\n",
		"package p\nvar f=func() (int,int){return g()}\nfunc F(){use(func()(int,int){return g()},1)}\n",
		"package p\nfunc F(){\n\t//line filename:line:col\nprintln(1)\n}\n",
	} {
		roundtrip(t, g)
	}
}

func TestPackageScopeAndZeros(t *testing.T) {
	o := Options{PackageFiles: map[string]string{"types.go": "package p\ntype Flag bool\ntype Record struct {X int}\n"}}
	g, e := Lower("package p\nF() (Flag,Record)! {\n!Oops\n}\n", o)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(g, "return false, Record{}, Oops") {
		t.Fatal(g)
	}
	for _, s := range []string{"package p\nF() time.Duration! {\n!Oops\n}\n", "package p\nF() time.Time! {\n!Oops\n}\n", "package p\nF() io.Reader! {\n!Oops\n}\n"} {
		g, e := Lower(s, Options{})
		if e != nil {
			t.Fatal(e)
		}
		roundtrip(t, g)
	}
	s := "package p\nF(){\nfmt:=struct{Value int}{Value:1}\nprintln(fmt.Value)\n}\n"
	g, e = Lower(s, Options{})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(g, "import") {
		t.Fatal(g)
	}
	o = Options{PackageFiles: map[string]string{"sibling.go": "package p\nvar local struct{Value int}\n"}}
	g, e = Lower("package p\nF(){println(local.Value)}\n", o)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(g, "import") {
		t.Fatal(g)
	}
}

func TestStructTags(t *testing.T) {
	g := "package p\ntype X struct {\nUserID int `json:\"user_id\" db:\"user_id\"`\nHTTPServer string `json:\"http_server\" db:\"http_server\"`\nSkip int\nName string `json:\"name,omitempty\"`\nsecret string\n}\n"
	s, e := Lift(g, Options{})
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range []string{"struct @json @db", "Skip int ~", "`json:\"name,omitempty\"`"} {
		if !strings.Contains(s, part) {
			t.Fatal(s)
		}
	}
	roundtrip(t, g)
	for _, g := range []string{"package p\ntype X struct {\nA int `json:\"a\"`\nB int\n}\n", "package p\ntype X struct {\nA int `json:\"a\"` //keep\nB int `json:\"b\"`\n}\n"} {
		roundtrip(t, g)
	}
}

func TestUnicodeAndLiterals(t *testing.T) {
	roundtrip(t, "package p\nvar Text=`one\n\t  two\n\n three`\nfunc F()string{return \"a  b\\n\"}\n")
}

func TestNestedReturnsAndOperators(t *testing.T) {
	for _, g := range []string{
		"package p\nfunc F()(any,error){\nreturn &struct{F func()(int,error)}{F:func()(int,error){return 0,Oops}},nil\n}\n",
		"package p\nvar x=[]struct{A bool}{{A:!flag}}\nvar y=[]uint64{^uint64(0), ^uint64(1)}\n",
		"package p\nfunc F()(func()int,error){return nil,Oops}\n",
		"package p\nfunc F() (error) {return nil}\n",
		"package p\ntype S int\nfunc(s /*keep*/ *S) F(){}\nfunc(s /*keep*/ *S) G(){}\n",
	} {
		roundtrip(t, g)
	}
}

func TestLines(t *testing.T) {
	g, e := Lower("package p\n\nF() {\nUnknown()\n}\n", Options{Lines: true, Filename: "input.smol"})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(g, "//line input.smol:4\n\tUnknown()") {
		t.Fatal(g)
	}
}

func TestExternalTestDotImport(t *testing.T) {
	o := Options{PackageFiles: map[string]string{"export_test.go": "package time\nvar AsynctimerChan struct{Value func()string}\n"}}
	g, e := Lower("package time_test\nimport . \"time\"\nF(){println(AsynctimerChan.Value())}\n", o)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(g, `import . "time"`) {
		t.Fatal(g)
	}
}

func TestLocalResultTypes(t *testing.T) {
	g := "package p\ntype T int\nfunc F(){\ntype Flag bool\nf:=func()(Flag,error){return false,Oops}\n_=f\n}\nfunc G[U any]()(U,error){return *new(U),Oops}\nfunc H()(T,error){return 0,Oops}\n"
	s, e := Lift(g, Options{})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(s, "!Oops") {
		t.Fatal(s)
	}
	roundtrip(t, g)
}

func TestDotImportDoesNotExportPrivateNames(t *testing.T) {
	o := Options{PackageFiles: map[string]string{"export_test.go": "package time\nvar debug int\n"}}
	g, e := Lower("package time_test\nimport . \"time\"\nF(){debug.PrintStack()}\n", o)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(g, `"runtime/debug"`) {
		t.Fatal(g)
	}
}
