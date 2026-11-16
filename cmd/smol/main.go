package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/build"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"smolang/internal/smol"
)

func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "smol:", e)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Println(`Usage: smol <lift|lower|fmt|check|outline|enc|dec> [flags] [files or packages]

Defaults to the current package. Package patterns (./..., std) are supported.

lift     Go -> name.smol (code) and name.smol.notes (comments, blank lines)
lower    name.smol [+ name.smol.notes] -> Go
fmt      rewrite .smol files in canonical form
check    verify the round-trip laws without writing
outline  print declarations without function bodies (read-only view)
enc      encode Go changes as an exact anchored patch
dec      decode an anchored patch into ordinary Go (use -n for a dry run)

-lines  lower only; emit Smol source positions.
-check  fmt only; fail if formatting would change files.
-docs   outline only; include doc comments.`)
		return nil
	}
	command := args[0]
	if command == "enc" || command == "dec" {
		return runEncoding(command, args[1:])
	}
	if command != "lift" && command != "lower" && command != "fmt" && command != "check" && command != "outline" {
		return fmt.Errorf("unknown command %q", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	lines := flags.Bool("lines", false, "emit source position directives (lower)")
	dry := flags.Bool("check", false, "check formatting without writing (fmt)")
	docs := flags.Bool("docs", false, "include doc comments (outline)")
	if e := flags.Parse(args[1:]); e != nil {
		return e
	}
	if *lines && command != "lower" {
		return fmt.Errorf("-lines is only supported by lower")
	}
	if *dry && command != "fmt" {
		return fmt.Errorf("-check is only supported by fmt")
	}
	ext := ".go"
	if command == "lower" || command == "fmt" {
		ext = ".smol"
	}
	paths := flags.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	files, e := findFiles(paths, ext)
	if e != nil {
		return e
	}
	if len(files) == 0 {
		return fmt.Errorf("no %s files found", ext)
	}
	groups := map[string][]string{}
	for _, f := range files {
		dir := filepath.Dir(f)
		groups[dir] = append(groups[dir], f)
	}
	dirs := make([]string, 0, len(groups))
	for d := range groups {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	total := 0
	failed := 0
	for _, dir := range dirs {
		root := moduleRoot(dir)
		imports, old, e := loadMod(root)
		if e != nil {
			return e
		}
		pkgFiles := map[string]string{}
		codeFiles := map[string]string{}
		for _, f := range groups[dir] {
			b, e := os.ReadFile(f)
			if e != nil {
				return e
			}
			pkgFiles[f] = string(b)
			if ext == ".smol" {
				// Merge the notes channel so comments and blank lines return.
				notes, _ := os.ReadFile(f + ".notes")
				wide, warnings, e := smol.Unpack(string(b), string(notes))
				if e != nil {
					return fmt.Errorf("%s: %w", f, e)
				}
				for _, w := range warnings {
					fmt.Fprintf(os.Stderr, "%s: %s\n", f, w)
				}
				codeFiles[f] = string(b)
				pkgFiles[f] = wide
			}
		}
		// Include declarations from siblings even when the user selected one file.
		siblings, _ := filepath.Glob(filepath.Join(dir, "*"+ext))
		for _, f := range siblings {
			ctx := build.Default
			base := filepath.Base(f)
			if ext == ".smol" {
				ctx.OpenFile = func(name string) (io.ReadCloser, error) { return os.Open(strings.TrimSuffix(name, ".go") + ".smol") }
				base = strings.TrimSuffix(base, ".smol") + ".go"
			}
			if active, err := ctx.MatchFile(dir, base); err != nil || !active {
				continue
			}
			if _, ok := pkgFiles[f]; !ok {
				b, e := os.ReadFile(f)
				if e == nil {
					pkgFiles[f] = string(b)
				}
			}
		}
		if command == "lift" {
			if e := smol.DiscoverImports(pkgFiles, imports); e != nil {
				return e
			}
		}
		declarations := pkgFiles
		if ext == ".smol" {
			declarations = map[string]string{}
			for f, t := range pkgFiles {
				p, e := smol.Prepare(t)
				if e != nil {
					return fmt.Errorf("%s: %w", f, e)
				}
				declarations[f] = p
			}
		}
		ctx := smol.NewPackageContext(declarations)
		pending := map[string]string{}
		for _, f := range groups[dir] {
			o := smol.Options{Context: ctx, Imports: imports, PackageFiles: declarations, Filename: f, Lines: *lines, LineSource: codeFiles[f]}
			text := pkgFiles[f]
			var out string
			var err error
			switch command {
			case "lift":
				out, err = smol.Lift(text, o)
			case "lower":
				out, err = smol.Lower(text, o)
			case "fmt":
				out, err = smol.Format(text, o)
			case "check":
				err = smol.Check(text, o)
			case "outline":
				out, err = smol.Outline(text, o, *docs)
			}
			if err != nil {
				if command == "check" {
					fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
					failed++
					continue
				}
				return fmt.Errorf("%s: %w", f, err)
			}
			total++
			if command == "check" {
				continue
			}
			if command == "outline" {
				fmt.Printf("// File: %s\n%s\n", filepath.Base(f), out)
				continue
			}
			target := strings.TrimSuffix(f, ext) + ".smol"
			if command == "lower" {
				pending[strings.TrimSuffix(f, ext)+".go"] = out
				continue
			}
			// lift and fmt write the two channels: dense code and notes.
			code, notes := smol.Pack(out)
			if command == "fmt" && *dry {
				old, _ := os.ReadFile(f + ".notes")
				if code != codeFiles[f] || notes != string(old) {
					return fmt.Errorf("%s needs formatting", f)
				}
				continue
			}
			pending[target] = code
			pending[target+".notes"] = notes
		}
		// Finish transforming a package before writing any of its files.
		for f, text := range pending {
			if text == "" && strings.HasSuffix(f, ".notes") {
				if e := os.Remove(f); e != nil && !os.IsNotExist(e) {
					return e
				}
				continue
			}
			if e := atomicWrite(f, text); e != nil {
				return e
			}
		}
		if command == "lift" && smol.WriteMod(imports) != old {
			if e := atomicWrite(filepath.Join(root, "smol.mod"), smol.WriteMod(imports)); e != nil {
				return e
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d files failed; %d passed", failed, total)
	}
	fmt.Fprintf(os.Stderr, "%s: %d files passed\n", command, total)
	return nil
}
func moduleRoot(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		if _, e := os.Stat(filepath.Join(d, "go.mod")); e == nil {
			return d
		}
		if filepath.Dir(d) == d {
			return dir
		}
	}
}
func loadMod(root string) (map[string]smol.Import, string, error) {
	if mod, e := os.ReadFile(filepath.Join(root, "go.mod")); e == nil {
		version := ""
		for _, line := range strings.Split(string(mod), "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 && f[0] == "go" {
				version = f[1]
				break
			}
		}
		var major, minor int
		if _, e := fmt.Sscanf(version, "%d.%d", &major, &minor); e != nil || major < 1 || (major == 1 && minor < 22) {
			return nil, "", fmt.Errorf("%s: Smol requires a Go version of at least 1.22", filepath.Join(root, "go.mod"))
		}
	}
	b, e := os.ReadFile(filepath.Join(root, "smol.mod"))
	if os.IsNotExist(e) {
		return map[string]smol.Import{}, "", nil
	}
	if e != nil {
		return nil, "", e
	}
	m, e := smol.ReadMod(string(b))
	return m, string(b), e
}
func atomicWrite(name, text string) error {
	f, e := os.CreateTemp(filepath.Dir(name), ".smol-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	mode := os.FileMode(0644)
	if st, e := os.Stat(name); e == nil {
		mode = st.Mode().Perm()
	}
	if e = f.Chmod(mode); e != nil {
		f.Close()
		return e
	}
	if _, e = f.WriteString(text); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(tmp, name)
}
func findFiles(paths []string, ext string) ([]string, error) {
	seen := map[string]bool{}
	add := func(p string) {
		a, e := filepath.Abs(p)
		if e == nil {
			seen[a] = true
		}
	}
	for _, p := range paths {
		if st, e := os.Stat(p); e == nil {
			if !st.IsDir() {
				if filepath.Ext(p) != ext {
					return nil, fmt.Errorf("expected %s file: %s", ext, p)
				}
				add(p)
				continue
			}
			fs, e := filepath.Glob(filepath.Join(p, "*"+ext))
			if e != nil {
				return nil, e
			}
			for _, f := range fs {
				add(f)
			}
			continue
		}
		if ext == ".smol" && strings.HasSuffix(p, "/...") {
			base := strings.TrimSuffix(p, "/...")
			e := filepath.WalkDir(base, func(p string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if d.IsDir() && (d.Name() == "vendor" || d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) && p != base {
					return filepath.SkipDir
				}
				if !d.IsDir() && filepath.Ext(p) == ext {
					add(p)
				}
				return nil
			})
			if e != nil {
				return nil, e
			}
			continue
		}
		cmd := exec.Command("go", "list", "-e", "-json", p)
		b, e := cmd.Output()
		if e != nil {
			return nil, fmt.Errorf("go list %s: %w", p, e)
		}
		dec := json.NewDecoder(strings.NewReader(string(b)))
		for {
			var pkg struct {
				Dir                                          string
				GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
			}
			e := dec.Decode(&pkg)
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
			if ext == ".go" {
				for _, list := range [][]string{pkg.GoFiles, pkg.CgoFiles, pkg.TestGoFiles, pkg.XTestGoFiles} {
					for _, f := range list {
						add(filepath.Join(pkg.Dir, f))
					}
				}
			} else {
				fs, _ := filepath.Glob(filepath.Join(pkg.Dir, "*"+ext))
				for _, f := range fs {
					add(f)
				}
			}
		}
	}
	files := make([]string, 0, len(seen))
	for f := range seen {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, nil
}
