package enc

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func ReadTree(root string) (Tree, error) {
	out := Tree{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if p != root && (d.Name() == ".git" || d.Name() == "vendor" || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink: %s", p)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	return out, err
}
func safeFile(root, p string) error {
	if !validPath(p) {
		return fmt.Errorf("invalid path %q", p)
	}
	cur := root
	for _, part := range strings.Split(p, "/") {
		cur = filepath.Join(cur, part)
		st, e := os.Lstat(cur)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink: %s", cur)
		}
	}
	return nil
}

// WriteTree stages every changed file before committing any rename. Failed
// commits restore the original files; validation is always completed first.
func WriteTree(root string, base, next Tree) error {
	type staged struct {
		path, tmp string
		mode      fs.FileMode
	}
	var stages []staged
	paths := []string{}
	seen := map[string]bool{}
	for p := range base {
		seen[p] = true
	}
	for p := range next {
		seen[p] = true
	}
	for p := range seen {
		if base[p] != next[p] {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	defer func() {
		for _, s := range stages {
			if s.tmp != "" {
				os.Remove(s.tmp)
			}
		}
	}()
	for _, p := range paths {
		if e := safeFile(root, p); e != nil {
			return e
		}
		full := filepath.Join(root, filepath.FromSlash(p))
		mode := fs.FileMode(0644)
		if st, e := os.Stat(full); e == nil {
			mode = st.Mode().Perm()
		}
		if _, ok := next[p]; !ok {
			stages = append(stages, staged{p, "", mode})
			continue
		}
		if e := os.MkdirAll(filepath.Dir(full), 0755); e != nil {
			return e
		}
		f, e := os.CreateTemp(filepath.Dir(full), ".smol-dec-*")
		if e != nil {
			return e
		}
		s := staged{p, f.Name(), mode}
		stages = append(stages, s)
		if e = f.Chmod(mode); e == nil {
			_, e = f.WriteString(next[p])
		}
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	var done []staged
	for _, s := range stages {
		full := filepath.Join(root, filepath.FromSlash(s.path))
		var e error
		if s.tmp == "" {
			e = os.Remove(full)
		} else {
			e = os.Rename(s.tmp, full)
		}
		if e != nil {
			for i := len(done) - 1; i >= 0; i-- {
				d := done[i]
				if old, ok := base[d.path]; ok {
					os.WriteFile(filepath.Join(root, d.path), []byte(old), d.mode)
				} else {
					os.Remove(filepath.Join(root, d.path))
				}
			}
			return e
		}
		done = append(done, s)
	}
	return nil
}
func Diff(base, next Tree) string {
	ps := []string{}
	seen := map[string]bool{}
	for p := range base {
		seen[p] = true
	}
	for p := range next {
		seen[p] = true
	}
	for p := range seen {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	var b strings.Builder
	for _, p := range ps {
		a, aok := base[p]
		n, nok := next[p]
		if a == n && aok == nok {
			continue
		}
		old, new := "a/"+p, "b/"+p
		if !aok {
			old = "/dev/null"
		}
		if !nok {
			new = "/dev/null"
		}
		al, nl := lines(a), lines(n)
		if !aok {
			al = nil
		}
		if !nok {
			nl = nil
		}
		fmt.Fprintf(&b, "--- %s\n+++ %s\n@@ -1,%d +1,%d @@\n", old, new, len(al), len(nl))
		for _, l := range al {
			b.WriteString("-" + l + "\n")
		}
		for _, l := range nl {
			b.WriteString("+" + l + "\n")
		}
	}
	return b.String()
}
