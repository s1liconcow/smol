package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"smolang/internal/enc"
)

func encodingOptions(root string, p string) (enc.Options, error) {
	o := enc.Options{Profile: enc.Profile(p), Imports: map[string]string{}}
	if p != "E0" && p != "E1" && p != "E2" {
		return o, fmt.Errorf("unknown profile %q; choose E0, E1 or E2", p)
	}
	m, _, e := loadMod(root)
	if e != nil {
		return o, e
	}
	for q, im := range m {
		o.Imports[q] = im.Path
	}
	return o, nil
}
func dependencyPackages(root string) map[string][]string {
	m := map[string][]string{}
	cmd := exec.Command("go", "list", "-e", "-json", "all")
	cmd.Dir = root
	b, e := cmd.Output()
	if e != nil {
		return m
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	for {
		var p struct {
			Name, ImportPath string
			Error            any
		}
		if e := d.Decode(&p); e != nil {
			break
		}
		if p.Name != "" && p.ImportPath != "" && p.Error == nil {
			m[p.Name] = append(m[p.Name], p.ImportPath)
		}
	}
	return m
}
func runEncoding(command string, args []string) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	instructions := flags.Bool("instructions", false, "print only the exact profile instruction text from SPEC_ENC section 7")
	profile := flags.String("profile", "E1", "E0, E1 (default), or experimental E2")
	dry := flags.Bool("n", false, "check and print diff without writing")
	gitRev := flags.String("git", "", "encode the working tree against a Git revision")
	check := flags.Bool("check", false, "verify recovery laws for packages")
	corpus := flags.String("corpus", "", "check JSONL base/target tree pairs (use - for stdin)")
	jobs := flags.Int("jobs", 4, "parallel corpus checks")
	mutate := flags.Bool("mutate", false, "damage each corpus patch line and check parse-or-reject")
	if e := flags.Parse(args); e != nil {
		return e
	}
	cwd, e := os.Getwd()
	if e != nil {
		return e
	}
	root := moduleRoot(cwd)
	o, e := encodingOptions(root, *profile)
	if e != nil {
		return e
	}
	if *instructions {
		fmt.Print(enc.Instructions(o.Profile))
		return nil
	}
	if command == "dec" {
		if *check || *gitRev != "" || *corpus != "" {
			return fmt.Errorf("dec supports -profile and -n")
		}
		if flags.NArg() > 1 {
			return fmt.Errorf("usage: smol dec [-profile E1] [-n] [patch]")
		}
		var patch []byte
		if flags.NArg() == 0 || flags.Arg(0) == "-" {
			patch, e = io.ReadAll(os.Stdin)
		} else {
			patch, e = os.ReadFile(flags.Arg(0))
		}
		if e != nil {
			return e
		}
		base, e := enc.ReadTree(root)
		if e != nil {
			return e
		}
		r, e := enc.Decode(base, string(patch), o)
		if e != nil && strings.Contains(e.Error(), "unresolved import qualifier") {
			o.Packages = dependencyPackages(root)
			r, e = enc.Decode(base, string(patch), o)
		}
		if e != nil {
			return e
		}
		if *dry {
			fmt.Print(enc.Diff(base, r.Files))
		} else if e = enc.WriteTree(root, base, r.Files); e != nil {
			return e
		}
		for _, op := range r.Operations {
			fmt.Printf("%s:%d-%d %s %s\n", op.Path, op.Start, op.End, op.Kind, strings.TrimSpace(op.First))
		}
		for _, w := range r.Warnings {
			fmt.Fprintln(os.Stderr, w)
		}
		return nil
	}
	if *dry {
		return fmt.Errorf("-n is only supported by dec")
	}
	if *corpus != "" {
		var input io.Reader = os.Stdin
		if *corpus != "-" {
			f, e := os.Open(*corpus)
			if e != nil {
				return e
			}
			defer f.Close()
			input = f
		}
		if *jobs < 1 || *jobs > 16 {
			return fmt.Errorf("-jobs must be between 1 and 16")
		}
		d := json.NewDecoder(input)
		out := json.NewEncoder(os.Stdout)
		type pair struct {
			ID           string
			Base, Target enc.Tree
			Options      enc.Options
		}
		queue := make(chan pair, *jobs)
		var wg sync.WaitGroup
		var mu sync.Mutex
		fail := 0
		for i := 0; i < *jobs; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for c := range queue {
					r, e := enc.CheckWithOptions(c.Base, c.Target, *mutate, c.Options)
					msg := ""
					if e != nil {
						msg = e.Error()
					}
					mu.Lock()
					if e != nil {
						fail++
					}
					out.Encode(struct {
						ID     string
						Result enc.CheckResult
						Error  string
					}{c.ID, r, msg})
					mu.Unlock()
				}
			}()
		}
		var readErr error
		for {
			var c pair
			if e := d.Decode(&c); e == io.EOF {
				break
			} else if e != nil {
				readErr = e
				break
			}
			queue <- c
		}
		close(queue)
		wg.Wait()
		if readErr != nil {
			return readErr
		}
		if fail > 0 {
			return fmt.Errorf("%d corpus pairs failed", fail)
		}
		return nil
	}

	if *check {
		patterns := flags.Args()
		if len(patterns) == 0 {
			patterns = []string{"."}
		}
		files, e := findFiles(patterns, ".go")
		if e != nil {
			return e
		}
		for _, p := range files {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			name := filepath.Base(p)
			base := enc.Tree{name: string(b)}
			target := enc.Tree{name: string(b) + "\n// smol enc recovery check\n"}
			if _, e := enc.Check(base, target, false); e != nil {
				return fmt.Errorf("%s: %w", p, e)
			}
		}
		fmt.Printf("enc check: %d files passed R1/R5 across E0/E1/E2\n", len(files))
		return nil
	}
	var base, target enc.Tree
	if *gitRev != "" {
		if flags.NArg() != 0 {
			return fmt.Errorf("-git does not take tree paths")
		}
		target, e = enc.ReadTree(root)
		if e != nil {
			return e
		}
		base, e = gitTree(root, *gitRev)
	} else {
		if flags.NArg() != 2 {
			return fmt.Errorf("usage: smol enc [-profile E1] old new, or smol enc -git rev")
		}
		if _, err := os.Stat(filepath.Join(flags.Arg(1), "go.mod")); err == nil {
			root = flags.Arg(1)
			o, e = encodingOptions(root, *profile)
			if e != nil {
				return e
			}
		}
		base, e = enc.ReadTree(flags.Arg(0))
		if e == nil {
			target, e = enc.ReadTree(flags.Arg(1))
		}
	}
	if e != nil {
		return e
	}
	patch, e := enc.Encode(base, target, o)
	if e != nil && strings.Contains(e.Error(), "unresolved import qualifier") {
		o.Packages = dependencyPackages(root)
		patch, e = enc.Encode(base, target, o)
	}
	if e != nil {
		return e
	}
	fmt.Print(patch)
	return nil
}
func gitTree(root, rev string) (enc.Tree, error) {
	cmd := exec.Command("git", "ls-tree", "-rz", "--name-only", rev)
	cmd.Dir = root
	b, e := cmd.Output()
	if e != nil {
		return nil, e
	}
	out := enc.Tree{}
	ps := strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
	sort.Strings(ps)
	for _, p := range ps {
		if !strings.HasSuffix(p, ".go") {
			continue
		}
		cmd = exec.Command("git", "show", rev+":"+p)
		cmd.Dir = root
		b, e = cmd.Output()
		if e != nil {
			return nil, e
		}
		out[p] = string(b)
	}
	return out, nil
}
