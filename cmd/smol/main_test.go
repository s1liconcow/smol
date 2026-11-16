package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIWorkflow(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "example.go")
	s := filepath.Join(dir, "example.smol")
	source := `package example
import "fmt"
func F(x int) (int,error) {
if x<0{return 0,fmt.Errorf("negative")}
return x+1,nil
}
`
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.org/test\n\ngo 1.22\n"), 0644)
	os.WriteFile(g, []byte(source), 0644)
	os.WriteFile(filepath.Join(dir, "example_test.go"), []byte(`package example
import "testing"
func TestBehavior(t *testing.T){if v,e:=F(2);v!=3||e!=nil{t.Fatal(v,e)};if v,e:=F(-1);v!=0||e==nil{t.Fatal(v,e)}}
`), 0644)
	for _, args := range [][]string{{"lift", g}, {"fmt", "-check", s}, {"lower", s}, {"check", g}} {
		if e := run(args); e != nil {
			t.Fatalf("%v: %v", args, e)
		}
	}
	cmd := exec.Command("go", "test", ".")
	cmd.Dir = dir
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("lowered Go behavior: %v\n%s", e, out)
	}
	if e := run([]string{"lower", "-lines", s}); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(g)
	if !strings.Contains(string(b), "//line "+s+":") {
		t.Fatal(string(b))
	}
	cmd = exec.Command("go", "test", ".")
	cmd.Dir = dir
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("line-mapped Go: %v\n%s", e, out)
	}
}
func TestUnsupportedGoVersion(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "go.mod"), []byte("module old\n\ngo 1.21\n"), 0644)
	if _, _, e := loadMod(d); e == nil {
		t.Fatal("accepted pre-1.22 module")
	}
}

func TestModuleImportTable(t *testing.T) {
	d := t.TempDir()
	os.Mkdir(filepath.Join(d, "lib"), 0755)
	os.WriteFile(filepath.Join(d, "go.mod"), []byte("module example.org/fixture\n\ngo 1.22\n"), 0644)
	os.WriteFile(filepath.Join(d, "lib", "lib.go"), []byte("package library\nfunc Value()int{return 3}\n"), 0644)
	g := filepath.Join(d, "demo.go")
	os.WriteFile(g, []byte("package demo\nimport pb \"example.org/fixture/lib\"\nfunc Value()int{return pb.Value()}\n"), 0644)
	if e := run([]string{"lift", g}); e != nil {
		t.Fatal(e)
	}
	mod, _ := os.ReadFile(filepath.Join(d, "smol.mod"))
	if string(mod) != "use pb = example.org/fixture/lib\n" {
		t.Fatal(string(mod))
	}
	s := strings.TrimSuffix(g, ".go") + ".smol"
	if e := run([]string{"lower", s}); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("go", "test", ".")
	cmd.Dir = d
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("%v: %s", e, b)
	}
}
