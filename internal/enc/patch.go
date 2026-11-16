// Package enc implements SPEC_ENC.md's exact anchored Go patches.
package enc

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

type Tree map[string]string
type Profile string

const (
	E0 Profile = "E0"
	E1 Profile = "E1"
	E2 Profile = "E2"
)

type operation struct {
	file, kind, anchor, extent string
	line                       int
	body                       []string
	origins                    []int
}

func patchError(line int, reason string) error { return fmt.Errorf("patch:%d: %s", line, reason) }
func validPath(p string) bool {
	return p != "" && p != "." && !strings.ContainsAny(p, "\x00\\\r\n") && !strings.HasPrefix(p, "/") && path.Clean(p) == p && !strings.HasPrefix(p, "../") && strings.HasSuffix(p, ".go")
}
func marker(s string) string {
	if len(s) >= 2 && (len(s) == 2 || s[2] == ' ') {
		switch s[:2] {
		case "@@", "==", "--", "++", "^^", "..", ">>":
			return s[:2]
		}
	}
	return ""
}
func parsePatch(text string) ([]operation, error) {
	if text == "" {
		return nil, nil
	}
	if !utf8.ValidString(text) {
		return nil, patchError(1, "patch is not UTF-8; use UTF-8 text")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	ls := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	offset := 0
	if len(ls) > 0 && strings.HasPrefix(ls[0], "```") {
		ls = ls[1:]
		offset = 1
	}
	if len(ls) > 0 && strings.HasPrefix(ls[len(ls)-1], "```") {
		ls = ls[:len(ls)-1]
	}
	var ops []operation
	file := ""
	lastHeader := -1
	for i, l := range ls {
		n := i + offset + 1
		m := marker(l)
		arg := ""
		if m != "" {
			arg = strings.TrimSpace(l[2:])
		}
		switch m {
		case "@@":
			if !validPath(arg) {
				return nil, patchError(n, "invalid module-relative Go path; use a .go path within the module")
			}
			file = arg
			ops = append(ops, operation{file: file, kind: "@@", line: n})
			lastHeader = i
		case "..":
			if len(ops) == 0 || lastHeader != i-1 {
				return nil, patchError(n, "range must immediately follow ==, -- or ++")
			}
			o := &ops[len(ops)-1]
			if o.kind != "==" && o.kind != "--" && o.kind != "++" || o.anchor == "" {
				return nil, patchError(n, "range needs an anchored ==, -- or ++")
			}
			o.extent = arg
			if arg == "" {
				o.extent = "\x00block"
			}
			lastHeader = -1
		case "==", "--", "++", "^^", ">>":
			if file == "" {
				return nil, patchError(n, "operation before @@; select a file first")
			}
			if (m == "++" || m == "^^") && arg == "" {
				return nil, patchError(n, "insertion needs an anchor")
			}
			if m == ">>" && !validPath(arg) {
				return nil, patchError(n, "rename needs a module-relative .go path")
			}
			ops = append(ops, operation{file: file, kind: m, anchor: arg, line: n})
			lastHeader = i
		default:
			if len(ops) == 0 {
				return nil, patchError(n, "text before first @@ header; reply with a patch only")
			}
			if strings.HasPrefix(l, "\\") {
				l = l[1:]
			}
			o := &ops[len(ops)-1]
			o.body = append(o.body, l)
			o.origins = append(o.origins, n)
			lastHeader = -1
		}
	}
	for i := range ops {
		o := &ops[i]
		for len(o.body) > 0 && strings.TrimSpace(o.body[0]) == "" {
			o.body = o.body[1:]
			o.origins = o.origins[1:]
		}
		for len(o.body) > 0 && strings.TrimSpace(o.body[len(o.body)-1]) == "" {
			o.body = o.body[:len(o.body)-1]
			o.origins = o.origins[:len(o.origins)-1]
		}
		if (o.kind == "==" || o.kind == "++" || o.kind == "^^") && len(o.body) == 0 {
			return nil, patchError(o.line, "operation requires a body; supply at least one line")
		}
		if (o.kind == "--" || o.kind == ">>") && len(o.body) > 0 {
			return nil, patchError(o.line, "operation takes no body; start another header")
		}
	}
	if len(ops) == 0 {
		return nil, patchError(1, "empty patch; select a file with @@")
	}
	return ops, nil
}

type Options struct {
	Profile Profile
	// Imports is smol.mod. Packages is the final, lowest-priority package table,
	// populated from go.mod requirements by the CLI (or supplied by a caller).
	Imports  map[string]string
	Packages map[string][]string
	analysis *analysisCache
}
type Applied struct {
	Path, Kind, First     string
	Start, End, PatchLine int
}
type Result struct {
	Files      Tree
	Operations []Applied
	Warnings   []string
}

func Decode(base Tree, patch string, o Options) (*Result, error) {
	if o.Profile == "" {
		o.Profile = E1
	}
	if o.Profile != E0 && o.Profile != E1 && o.Profile != E2 {
		return nil, patchError(1, "unknown profile; choose E0, E1 or E2")
	}
	if o.analysis == nil {
		o.analysis = newAnalysis()
	}
	ops, err := parsePatch(patch)
	if err != nil {
		return nil, err
	}
	groups := map[string][]operation{}
	var order []string
	for _, op := range ops {
		if _, ok := groups[op.file]; !ok {
			order = append(order, op.file)
		}
		groups[op.file] = append(groups[op.file], op)
	}
	result := &Result{Files: Tree{}}
	for p, t := range base {
		result.Files[p] = t
	}
	renames := map[string]string{}
	touched := map[string]int{}
	originMap := map[string][]int{}
	wholeFiles := map[string]bool{}
	for _, p := range order {
		text, exists := base[p]
		s := scan(text)
		var cs []change
		create := false
		deleted := false
		for n, op := range groups[p] {
			body := append([]string(nil), op.body...)
			if o.Profile != E0 {
				body = dedent(body)
			}
			if op.kind == "@@" {
				if len(body) > 0 {
					if exists || create {
						return nil, patchError(op.line, "body under @@ is only valid for a new file; use ==")
					}
					create = true
					cs = append(cs, change{0, 0, op.line, n, body, op.origins, "new"})
					result.Operations = append(result.Operations, Applied{p, "new", "", 0, 0, op.line})
				}
				continue
			}
			if !exists {
				return nil, patchError(op.line, "anchored/whole-file operations require an existing file; write a new file under @@")
			}
			if op.kind == ">>" {
				if _, ok := renames[p]; ok {
					return nil, patchError(op.line, "file already renamed; use one rename per file")
				}
				renames[p] = op.anchor
				result.Operations = append(result.Operations, Applied{p, ">>", op.anchor, 1, len(s.lines), op.line})
				continue
			}
			a, b := 0, len(s.lines)
			first := ""
			if op.anchor != "" {
				a, err = s.resolve(op.anchor, p, op.line)
				if err != nil {
					return nil, err
				}
				b = a + 1
				first = s.lines[a]
				if op.extent == "\x00block" {
					b = s.block[a] + 1
				} else if op.extent != "" {
					end, e := s.rangeEnd(a, op.extent, p, op.line)
					if e != nil {
						return nil, e
					}
					b = end + 1
				}
			}
			start, end := a+1, b
			switch op.kind {
			case "++":
				a = b
			case "^^":
				a = s.attached(a)
				b = a
			case "--":
				if op.anchor == "" {
					deleted = true
				} else {
					a = s.attached(a)
				}
			case "==":
				if op.anchor == "" {
					wholeFiles[p] = true
				}
			}
			cs = append(cs, change{a, b, op.line, n, body, op.origins, op.kind})
			result.Operations = append(result.Operations, Applied{p, op.kind, first, start, end, op.line})
		}
		if !exists && !create {
			return nil, patchError(groups[p][0].line, "new file needs a body under @@")
		}
		if deleted && (len(cs) > 1 || renames[p] != "") {
			return nil, patchError(groups[p][0].line, "file deletion conflicts with another operation")
		}
		if deleted {
			delete(result.Files, p)
			continue
		}
		out, origins, e := apply(s, cs)
		if e != nil {
			return nil, e
		}
		if len(cs) == 0 && renames[p] == "" {
			continue
		}
		result.Files[p] = strings.Join(out, "\n") + "\n"
		touched[p] = groups[p][0].line
		originMap[p] = origins
	}
	// Resolve names from the final package tree, independent of patch order.
	for _, p := range order {
		line, ok := touched[p]
		if !ok {
			continue
		}
		out, w, e := finish(p, result.Files[p], base[p], result.Files, originMap[p], o, wholeFiles[p])
		if e != nil {
			return nil, patchError(errorLine(e, originMap[p], line), fmt.Sprintf("%s: %v", p, e))
		}
		result.Files[p] = out
		if w != "" {
			result.Warnings = append(result.Warnings, p+": "+w)
		}
	}
	destinations := map[string]bool{}
	for _, p := range order {
		dest := renames[p]
		if dest == "" {
			continue
		}
		if destinations[dest] {
			return nil, patchError(groups[p][0].line, "multiple renames target the same file")
		}
		destinations[dest] = true
		if _, ok := base[dest]; ok {
			return nil, patchError(groups[p][0].line, "rename destination exists; choose a new path")
		}
		if _, ok := touched[dest]; ok {
			return nil, patchError(groups[p][0].line, "rename destination is also edited")
		}
	}
	for p, dest := range renames {
		result.Files[dest] = result.Files[p]
		delete(result.Files, p)
	}
	return result, nil
}
func dedent(body []string) []string {
	s := scan(strings.Join(body, "\n") + "\n")
	for i := range body {
		if !s.inside[i] {
			body[i] = strings.TrimLeftFunc(body[i], func(r rune) bool { return r == ' ' || r == '\t' })
		}
	}
	return body
}
