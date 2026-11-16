package smol

import (
	"go/build"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// packRound lifts Go, splits it into the two channels, and lowers the pair.
func packRound(t *testing.T, name, src string, o Options) (code, notes string) {
	t.Helper()
	n, err := Normalize(src)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	wide, err := Lift(n, o)
	if err != nil {
		t.Fatalf("%s: lift: %v", name, err)
	}
	code, notes = Pack(wide)
	back, warnings, err := Unpack(code, notes)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("%s: unpack: %v %v", name, err, warnings)
	}
	g, err := Lower(back, o)
	if err != nil {
		t.Fatalf("%s: lower: %v", name, err)
	}
	if g != n {
		t.Fatalf("%s: round trip differs at %s", name, difference(n, g))
	}
	c2, n2 := Pack(back)
	if c2 != code || n2 != notes {
		t.Fatalf("%s: packing is not stable", name)
	}
	return code, notes
}

func TestPackStandardPackage(t *testing.T) {
	dir := filepath.Join(runtime.GOROOT(), "src", "encoding", "json")
	pkg, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		t.Skip(err)
	}
	texts := map[string]string{}
	for _, f := range pkg.GoFiles {
		b, _ := os.ReadFile(filepath.Join(dir, f))
		texts[filepath.Join(dir, f)] = string(b)
	}
	ctx := NewPackageContext(texts)
	names := make([]string, 0, len(texts))
	for f := range texts {
		names = append(names, f)
	}
	sort.Strings(names)
	var bundle strings.Builder
	for _, f := range names {
		code, _ := packRound(t, filepath.Base(f), texts[f], Options{Context: ctx, PackageFiles: texts})
		bundle.WriteString("// File: encoding/json/" + filepath.Base(f) + "\n" + code + "\n")
	}
	if out := os.Getenv("SMOL_BUNDLE"); out != "" {
		os.WriteFile(out, []byte(bundle.String()), 0o644)
	}
}

func lowerOK(t *testing.T, smol, want string) {
	t.Helper()
	got, err := Lower(smol, Options{})
	if err != nil {
		t.Fatalf("lower: %v\n%s", err, smol)
	}
	w, err := Normalize(want)
	if err != nil {
		t.Fatal(err)
	}
	if got != w {
		t.Fatalf("lower:\n%s\nwant:\n%s", got, w)
	}
}

// liftPacked returns the code channel for Go source and checks the round trip.
func liftPacked(t *testing.T, src string) string {
	t.Helper()
	code, _ := packRound(t, "case", src, Options{})
	return code
}

func TestScopeForms(t *testing.T) {
	// §6.10 opened packages: two uses open the package; a local name blocks it.
	code := liftPacked(t, `package p

import "strings"

func F(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func G(s string) string {
	ToLower := 1
	_ = ToLower
	return strings.ToLower(s)
}
`)
	for _, want := range []string{"use strings\n", "{return ToUpper(TrimSpace(s))}", "return strings.ToLower(s)"} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in:\n%s", want, code)
		}
	}
	// A single use does not pay for the header.
	if code := liftPacked(t, "package p\n\nimport \"strings\"\n\nfunc F(s string) string { return strings.ToUpper(s) }\n"); strings.Contains(code, "use ") {
		t.Errorf("opened for one use:\n%s", code)
	}
	// A name exported by two opened packages stays qualified.
	code = liftPacked(t, `package p

import (
	"bytes"
	"strings"
)

func F(a []byte, s string) bool {
	return bytes.Contains(a, nil) && strings.Contains(s, "") && bytes.Equal(a, nil) && strings.ContainsRune(s, 0) && strings.ContainsAny(s, "") && bytes.Equal(nil, a)
}
`)
	for _, want := range []string{"use bytes\n", "bytes.Contains(", "strings.Contains(", "&&Equal(a nil)", "&&strings.ContainsRune(", "&&Equal(nil a)"} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in:\n%s", want, code)
		}
	}
	// §6.11 subject members: receiver, then first parameter of a package type.
	code = liftPacked(t, `package p

type T struct {
	n    int
	len  int
	next *T
}

func (t *T) Inc() { t.n++ }

func (t *T) Shadow(n int) int { return t.n + n }

func (t *T) Len() int { return t.len }

func (t *T) Lit() T { return T{n: t.n} }

func (t *T) Closure() func() int {
	return func() int { t := &T{}; return t.n }
}

func reset(t *T, other *T) {
	t.n = other.n
	t.next.Inc()
}
`)
	for _, want := range []string{"Inc(){n++}", "{return t.n+n}", "{return t.len}", "T{n:n}", ";return t.n}", "n=other.n", "next.Inc()"} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in:\n%s", want, code)
		}
	}
	// §6.12 file conventions and §6.13 argument commas.
	code = liftPacked(t, `package p

type buf struct{ data []byte }

func put(dst *buf, n int) { dst.data = append(dst.data, byte(n), 1, 'x') }

func puts(dst *buf, s string) { put(dst, len(s)) }

func both(dst *buf, dst2 *buf) { puts(dst, "x"); puts(dst2, "y") }

func pick(dst, n int) int { return max(dst, n, -1) }

func types(func(dst, buf)) {}
`)
	for _, want := range []string{"let dst*buf\n", "put(dst,n int){data=append(data byte(n),1 'x')}", "puts(dst,s string){put(dst len(s))}",
		"both(dst,dst2*buf)", `puts(dst "x")`, `puts(dst2 "y")`, `pick(\dst,n int)`, "max(dst n,-1)", `types(func(\dst,buf))`} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in:\n%s", want, code)
		}
	}
	// Hand-written forms lower to the same Go.
	lowerOK(t, "package p\nuse strings\nlet s string\nUp(s)string=>ToUpper(TrimSpace(s))\n",
		"package p\n\nimport \"strings\"\n\nfunc Up(s string) string {\n\treturn strings.ToUpper(strings.TrimSpace(s))\n}\n")
	lowerOK(t, "package p\ntype T struct{n int}\n(t*T)Add(d int)int{\nn+=d return n\n}\nsum(t*T,xs[]int)int{\nfor x in xs{n+=x}\nreturn max(n 0)\n}\n",
		"package p\n\ntype T struct{ n int }\n\nfunc (t *T) Add(d int) int {\n\tt.n += d\n\treturn t.n\n}\nfunc sum(t *T, xs []int) int {\n\tfor _, x := range xs {\n\t\tt.n += x\n\t}\n\treturn max(t.n, 0)\n}\n")
	if _, err := Lower("package p\nuse bytes strings\nF(s string)bool=>Contains(s \"\")\n", Options{}); err == nil || !strings.Contains(err.Error(), "more than one opened package") {
		t.Errorf("ambiguous opened name: %v", err)
	}
}

func TestPartialImportBlock(t *testing.T) {
	code := liftPacked(t, `package p

import (
	"fmt"
	"strings"
	_ "unsafe" // for linkname
)

func F() string { return fmt.Sprint(strings.ToUpper("x")) }
`)
	if !strings.Contains(code, "import(\n...\n_ \"unsafe\"\n)") {
		t.Errorf("partial import block:\n%s", code)
	}
}

func TestNotes(t *testing.T) {
	src := `// Package p is documented.
package p // trailing

// A is a constant.
const A = 1 /* inline */ + 2

// F does things.
//
// Second paragraph.
func F(x int) int {
	// before
	if x > 0 { // trailing if
		return x
	}

	return -x // negate
}

// end of file
`
	code, notes := packRound(t, "notes", src, Options{})
	if strings.Contains(code, "//") || strings.Contains(code, "/*") || strings.Contains(code, "\n\n") {
		t.Errorf("code channel keeps comments or blank lines:\n%s", code)
	}
	for _, want := range []string{"documented", "trailing", "inline", "Second paragraph", "before", "negate", "end of file"} {
		if !strings.Contains(notes, want) {
			t.Errorf("notes lack %q:\n%s", want, notes)
		}
	}
	// Editing one declaration keeps every comment: its doc comment stays, its
	// interior comments move above it, and other declarations are untouched.
	edited := strings.Replace(code, "if x>0{", "if x>1{\nx--", 1)
	if edited == code {
		t.Fatalf("edit did not apply:\n%s", code)
	}
	wide, warnings, err := Unpack(edited, notes)
	if err != nil || len(warnings) != 1 {
		t.Fatalf("unpack edited: %v %v", err, warnings)
	}
	g, err := Lower(wide, Options{})
	if err != nil {
		t.Fatalf("lower edited: %v\n%s", err, wide)
	}
	for _, want := range []string{"// A is a constant.\nconst A = 1 /* inline */ + 2", "// before\n", "// trailing if\n", "// negate\n", "// F does things.\n//\n// Second paragraph.\nfunc F(x int) int {", "// end of file"} {
		if !strings.Contains(g, want) {
			t.Errorf("edited lowering lacks %q:\n%s", want, g)
		}
	}
	// Notes for a declaration that no longer exists are reported.
	_, warnings, err = Unpack(strings.Replace(code, "const A=1+2\n", "", 1), notes)
	if err != nil || len(warnings) == 0 {
		t.Errorf("orphan notes: %v %v", err, warnings)
	}
}

func TestImpliedBreaks(t *testing.T) {
	code := liftPacked(t, `package p

var a = 1
var b = 2

func F(x int) int {
	y := x
	if y > 0 {
		y = 1
	} else if y < 0 {
		y = 2
	}
	for y < 10 {
		y++
	}
	z := y
	return z
}
`)
	for _, want := range []string{"var a=1\nvar b=2\n", "y:=x if y>0{", "}else if y<0{", "}\nfor y<10{", "z:=y return z\n"} {
		if !strings.Contains(code, want) {
			t.Errorf("missing %q in:\n%s", want, code)
		}
	}
}

func TestDirectivesStayInCode(t *testing.T) {
	src := `// Copyright notice.

//go:build linux || darwin

// Package p is documented.
package p

/*
#include <stdio.h>
*/
import "C"

// F is documented.
//
//go:noinline
func F() int {
	// plain comment
	return int(C.EOF)
}

func G(xs []int) int {
	switch len(xs) {
	case 0, 1:
		return 1
	}
	return 2
}
`
	code, notes := packRound(t, "directives", src, Options{})
	for _, want := range []string{"//go:build linux || darwin\npackage p\n", "/*\n#include <stdio.h>\n*/\nimport \"C\"\n", "//go:noinline\nF()int{", "case 0 1:"} {
		if !strings.Contains(code, want) {
			t.Errorf("code lacks %q:\n%s", want, code)
		}
	}
	for _, gone := range []string{"Copyright", "documented", "plain comment"} {
		if strings.Contains(code, gone) || !strings.Contains(notes, gone) {
			t.Errorf("%q should be in the notes only:\n%s\n%s", gone, code, notes)
		}
	}
	// The code channel alone is the same program, directives included.
	bare, err := Lower(code, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"//go:build linux || darwin\n\npackage p", "#include <stdio.h>\n*/\nimport \"C\"", "//go:noinline\nfunc F() int {", "case 0, 1:"} {
		if !strings.Contains(bare, want) {
			t.Errorf("lowering without notes lacks %q:\n%s", want, bare)
		}
	}
}
