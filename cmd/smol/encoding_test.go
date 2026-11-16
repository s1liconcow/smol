package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodingCLI(t *testing.T) {
	root := t.TempDir()
	old, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chdir(root); e != nil {
		t.Fatal(e)
	}
	defer os.Chdir(old)
	os.WriteFile("go.mod", []byte("module example.org/enc\n\ngo 1.22\n"), 0644)
	source := "package p\n\nfunc A() int { return 1 }\n"
	os.WriteFile("a.go", []byte(source), 0600)
	patch := filepath.Join(root, "change.enc")
	os.WriteFile(patch, []byte("@@ a.go\n== func A\nfunc A() int { return 2 }\n@@ b.go\npackage p\nfunc B(){fmt.Println(A())}\n"), 0644)
	if e := run([]string{"dec", "-n", patch}); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile("a.go")
	if string(b) != source {
		t.Fatal("dry run wrote source")
	}
	if _, e := os.Stat("b.go"); !os.IsNotExist(e) {
		t.Fatal("dry run created file")
	}
	if e := run([]string{"dec", patch}); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile("b.go")
	if !strings.Contains(string(b), `"fmt"`) {
		t.Fatal(string(b))
	}
	st, _ := os.Stat("a.go")
	if st.Mode().Perm() != 0600 {
		t.Fatal("mode changed")
	}
	os.WriteFile(patch, []byte("@@ a.go\n== func A\nfunc A()int{return 9}\n@@ b.go\n-- absent\n"), 0644)
	before, _ := os.ReadFile("a.go")
	if e := run([]string{"dec", patch}); e == nil {
		t.Fatal("accepted bad second operation")
	}
	after, _ := os.ReadFile("a.go")
	if string(before) != string(after) {
		t.Fatal("partial write")
	}
	if e := run([]string{"enc", "-check", "."}); e != nil {
		t.Fatal(e)
	}
}
