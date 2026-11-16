package enc

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"sort"
	"strings"
)

type hunk struct{ a, b, c, d int }

// diff computes a line LCS. Large rewrites use a single middle hunk to bound
// memory; whole-file replacement remains the exact, deterministic fallback.
func diff(a, b []string) []hunk {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	ae, be := len(a), len(b)
	for ae > prefix && be > prefix && a[ae-1] == b[be-1] {
		ae--
		be--
	}
	if ae == prefix && be == prefix {
		return nil
	}
	n, m := ae-prefix, be-prefix
	if n*m > 2000000 {
		return []hunk{{prefix, ae, prefix, be}}
	}
	dp := make([]int, (n+1)*(m+1))
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			k := i*(m+1) + j
			if a[prefix+i] == b[prefix+j] {
				dp[k] = dp[(i+1)*(m+1)+j+1] + 1
			} else {
				dp[k] = max(dp[(i+1)*(m+1)+j], dp[k+1])
			}
		}
	}
	var out []hunk
	i, j := 0, 0
	var active *hunk
	flush := func() {
		if active != nil {
			out = append(out, *active)
			active = nil
		}
	}
	for i < n || j < m {
		if i < n && j < m && a[prefix+i] == b[prefix+j] {
			flush()
			i++
			j++
			continue
		}
		if active == nil {
			active = &hunk{prefix + i, prefix + i, prefix + j, prefix + j}
		}
		if j < m && (i == n || dp[i*(m+1)+j+1] >= dp[(i+1)*(m+1)+j]) {
			j++
		} else {
			i++
		}
		active.b = prefix + i
		active.d = prefix + j
	}
	flush()
	return out
}
func escape(s string) string {
	if strings.HasPrefix(s, "\\") || marker(s) != "" {
		return "\\" + s
	}
	return s
}
func bodyText(ls []string, p Profile) string {
	ls = append([]string(nil), ls...)
	if p != E0 {
		ls = dedent(ls)
	}
	for i := range ls {
		ls[i] = escape(ls[i])
	}
	return strings.Join(ls, "\n") + "\n"
}
func targetHeader(s *source, kind string, a, b int) string {
	anchor := s.anchor(a)
	if anchor == "" {
		return ""
	}
	h := kind + " " + anchor + "\n"
	if b > a+1 {
		if s.block[a] == b-1 {
			return h + "..\n"
		}
		hi := len(s.lines)
		if p := s.enclosing(a); p >= 0 {
			hi = s.block[p] + 1
		}
		if b > hi || s.inside[b-1] {
			return ""
		}
		end := strings.TrimSpace(s.lines[b-1])
		if strings.Contains(end, " @ ") {
			return ""
		}
		if m := s.matches(end, a+1, hi); len(m) != 1 || m[0] != b-1 {
			return ""
		}
		h += ".. " + end + "\n"
	}
	return h
}
func filePatch(p, old, new string, exists bool, profile Profile) string {
	head := "@@ " + p + "\n"
	if exists {
		if b, e := format.Source([]byte(old)); e == nil && string(b) != old {
			return head + "==\n" + bodyText(lines(new), profile)
		}
	}
	if !exists {
		return head + bodyText(lines(new), profile)
	}
	s := scan(old)
	t := lines(new)
	hs := diff(s.lines, t)
	var out strings.Builder
	out.WriteString(head)
	for _, h := range hs {
		body := t[h.c:h.d]
		header := ""
		if h.a == h.b {
			var choices []string
			if h.a > 0 {
				if a := s.anchor(h.a - 1); a != "" {
					choices = append(choices, "++ "+a+"\n")
				}
				for k := h.a - 2; k >= 0; k-- {
					if s.block[k] == h.a-1 {
						if a := s.anchor(k); a != "" {
							choices = append(choices, "++ "+a+"\n..\n")
						}
					}
				}
			}
			if h.a < len(s.lines) && s.attached(h.a) == h.a {
				if a := s.anchor(h.a); a != "" {
					choices = append(choices, "^^ "+a+"\n")
				}
			}
			sort.SliceStable(choices, func(i, j int) bool { return len(choices[i]) < len(choices[j]) })
			if len(choices) > 0 {
				header = choices[0]
			}
		} else {
			kind := "=="
			if len(body) == 0 {
				kind = "--"
			}
			header = targetHeader(s, kind, h.a, h.b)
		}
		if header == "" {
			return head + "==\n" + bodyText(t, profile)
		}
		out.WriteString(header)
		if len(body) > 0 {
			out.WriteString(bodyText(body, profile))
		}
	}
	return out.String()
}

// Encode returns an exact canonical patch. It verifies its own output and
// widens to whole-file replacements whenever a local hunk loses information.
func Encode(base, target Tree, o Options) (string, error) {
	if o.Profile == "" {
		o.Profile = E1
	}
	normalized := Tree{}
	for p, t := range target {
		if !validPath(p) {
			return "", fmt.Errorf("invalid Go path %q", p)
		}
		b, e := Normalize(t)
		if e != nil {
			return "", fmt.Errorf("%s: %w", p, e)
		}
		normalized[p] = b
	}
	paths := []string{}
	seen := map[string]bool{}
	for p := range base {
		seen[p] = true
	}
	for p := range target {
		seen[p] = true
	}
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var patch strings.Builder
	for _, p := range paths {
		old, exists := base[p]
		new, keep := normalized[p]
		if old == new && exists == keep {
			continue
		}
		if !keep {
			patch.WriteString("@@ " + p + "\n--\n")
			continue
		}
		patch.WriteString(filePatch(p, old, new, exists, o.Profile))
	}
	if patch.Len() == 0 {
		return "", nil
	}
	encoded := patch.String()
	if r, e := Decode(base, encoded, o); e == nil && equalTrees(r.Files, normalized) {
		return compactImports(base, normalized, encoded, o), nil
	}
	patch.Reset()
	for _, p := range paths {
		old, exists := base[p]
		new, keep := normalized[p]
		if old == new && exists == keep {
			continue
		}
		patch.WriteString("@@ " + p + "\n")
		if !keep {
			patch.WriteString("--\n")
			continue
		}
		if exists {
			patch.WriteString("==\n")
		}
		patch.WriteString(bodyText(lines(new), o.Profile))
	}
	encoded = patch.String()
	r, e := Decode(base, encoded, o)
	if e != nil {
		return "", e
	}
	if !equalTrees(r.Files, normalized) {
		return "", fmt.Errorf("target cannot be represented under %s without changing Go; %s", o.Profile, mismatch(r, normalized))
	}
	return compactImports(base, normalized, encoded, o), nil
}
func equalTrees(a, b Tree) bool {
	if len(a) != len(b) {
		return false
	}
	for p, t := range a {
		if b[p] != t {
			return false
		}
	}
	return true
}

// Import omission is attempted only when the complete body parses. Explicit
// imports remain the lossless fallback for ambiguous or unavailable tables.
func compactImports(base, target Tree, patch string, o Options) string {
	if o.Profile == E0 {
		return patch
	}
	ops, e := parsePatch(patch)
	if e != nil {
		return patch
	}
	var b strings.Builder
	for _, op := range ops {
		if op.kind == "@@" {
			b.WriteString("@@ " + op.file + "\n")
		} else {
			b.WriteString(op.kind)
			if op.anchor != "" {
				b.WriteString(" " + op.anchor)
			}
			b.WriteByte('\n')
			if op.extent != "" {
				b.WriteString("..")
				if op.extent != "\x00block" {
					b.WriteString(" " + op.extent)
				}
				b.WriteByte('\n')
			}
		}
		body := strings.Join(op.body, "\n") + "\n"
		fs, f, e := parseGo(body)
		if e == nil {
			type cut struct{ a, b int }
			var cuts []cut
			for _, d := range f.Decls {
				g, ok := d.(*ast.GenDecl)
				if !ok || g.Tok != token.IMPORT {
					continue
				}
				cgo := false
				for _, s := range g.Specs {
					if s.(*ast.ImportSpec).Path.Value == `"C"` {
						cgo = true
					}
				}
				if cgo {
					continue
				}
				a, z := fs.Position(g.Pos()).Offset, fs.Position(g.End()).Offset
				if z < len(body) && body[z] == '\n' {
					z++
				}
				cuts = append(cuts, cut{a, z})
			}
			for i := len(cuts) - 1; i >= 0; i-- {
				c := cuts[i]
				body = body[:c.a] + body[c.b:]
			}
		}
		if len(op.body) > 0 {
			b.WriteString(bodyText(lines(strings.Trim(body, "\n")), o.Profile))
		}
	}
	candidate := b.String()
	if r, e := Decode(base, candidate, o); e == nil && equalTrees(r.Files, target) {
		return candidate
	}
	return patch
}
