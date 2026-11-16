package smol

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"sort"
	"strings"
)

type edit struct {
	a, b int
	text string
}
type source struct {
	text  string
	fs    *token.FileSet
	file  *ast.File
	edits []edit
	conv  map[string]string
}

func parse(text string) (*source, error) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "source.go", text, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	return &source{text: text, fs: fs, file: f}, nil
}
func (s *source) off(p token.Pos) int   { return s.fs.PositionFor(p, false).Offset }
func (s *source) raw(n ast.Node) string { return s.text[s.off(n.Pos()):s.off(n.End())] }
func (s *source) put(a, b int, t string) { // Outer rewrites consume already-rendered inner edits.
	es := s.edits[:0]
	for _, e := range s.edits {
		if e.a >= a && e.b <= b {
			continue
		}
		es = append(es, e)
	}
	s.edits = append(es, edit{a, b, t})
}
func (s *source) replace(n ast.Node, t string) { s.put(s.off(n.Pos()), s.off(n.End()), t) }
func (s *source) render(a, b int) string {
	es := append([]edit(nil), s.edits...)
	sort.SliceStable(es, func(i, j int) bool { return es[i].a < es[j].a })
	var out strings.Builder
	p := a
	for _, e := range es {
		if e.a < a || e.b > b {
			continue
		}
		if e.a < p {
			continue
		}
		out.WriteString(s.text[p:e.a])
		out.WriteString(e.text)
		p = e.b
	}
	out.WriteString(s.text[p:b])
	return out.String()
}
func (s *source) node(n ast.Node) string { return s.render(s.off(n.Pos()), s.off(n.End())) }
func (s *source) clean(a, b int) bool {
	for _, c := range s.file.Comments {
		if s.off(c.Pos()) < b && s.off(c.End()) > a {
			return false
		}
	}
	// Only whitespace between tokens matters; raw string contents are untouched.
	ts := lex(s.text[a:b])
	end := 0
	for _, t := range ts {
		if strings.Contains(strings.ReplaceAll(s.text[a+end:a+t.a], "\t", ""), "\n\n") {
			return false
		}
		end = t.b
	}
	return true
}
func norm(text string) (string, error) { b, e := format.Source([]byte(text)); return string(b), e }

type tok struct {
	k    token.Token
	lit  string
	a, b int
	line int
}

func lex(text string) []tok {
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(text))
	var sc scanner.Scanner
	sc.Init(f, []byte(text), func(token.Position, string) {}, scanner.ScanComments)
	var out []tok
	for {
		p, k, l := sc.Scan()
		if k == token.EOF {
			break
		}
		if l == "" {
			l = k.String()
		}
		a := f.Offset(p)
		b := a + len(l)
		if k == token.SEMICOLON && l == "\n" {
			b = a
		}
		out = append(out, tok{k, l, a, b, f.Line(p)})
	}
	var fixed []tok
	for _, t := range out {
		if len(fixed) > 0 {
			p := fixed[len(fixed)-1]
			if (p.lit == "?" || p.lit == "~") && t.line > p.line && t.k != token.SEMICOLON {
				fixed = append(fixed, tok{k: token.SEMICOLON, lit: "\n", a: p.b, b: p.b, line: p.line})
			}
		}
		fixed = append(fixed, t)
	}
	return fixed
}
func matching(ts []tok, i int) int {
	pairs := map[string]string{"(": ")", "[": "]", "{": "}"}
	end := pairs[ts[i].lit]
	d := 0
	for j := i; j < len(ts); j++ {
		if ts[j].lit == ts[i].lit {
			d++
		}
		if ts[j].lit == end {
			d--
			if d == 0 {
				return j
			}
		}
	}
	return -1
}
func significant(ts []tok) []tok {
	var out []tok
	for _, t := range ts {
		if t.k != token.COMMENT && t.k != token.SEMICOLON {
			out = append(out, t)
		}
	}
	return out
}
func apply(text string, es []edit) (string, error) {
	sort.SliceStable(es, func(i, j int) bool { return es[i].a < es[j].a })
	var b strings.Builder
	p := 0
	for _, e := range es {
		if e.a < p || e.b < e.a || e.b > len(text) {
			return "", fmt.Errorf("overlapping rewrite at %d near %q", e.a, excerpt(text[max(0, e.a-50):min(len(text), max(e.a, e.b)+50)]))
		}
		b.WriteString(text[p:e.a])
		b.WriteString(e.text)
		p = e.b
	}
	b.WriteString(text[p:])
	return b.String(), nil
}

// compact changes whitespace only, preserving literals and comments byte for byte.
func compact(text string) string {
	ts := lex(text)
	bodyComments := map[int]bool{}
	column := func(pos int) int { return pos - strings.LastIndex(text[:pos], "\n") - 1 }
	for i, t := range ts {
		if t.k != token.COMMENT {
			continue
		}
		if strings.HasPrefix(t.lit, "//line ") && column(t.a) > 0 {
			bodyComments[t.a] = true
		}
		j := i + 1
		for j < len(ts) && (ts[j].k == token.COMMENT || ts[j].k == token.SEMICOLON) {
			j++
		}
		if j < len(ts) && (ts[j].lit == "case" || ts[j].lit == "default" || ts[j].lit == ")" || ts[j].lit == "}" || ts[j].lit == "]") && column(t.a) > column(ts[j].a) {
			bodyComments[t.a] = true
		}
	}
	var b strings.Builder
	p := 0
	for _, t := range ts {
		if t.b == t.a {
			continue
		}
		ws := text[p:t.a]
		if strings.Contains(ws, "\n") {
			n := strings.Count(ws, "\n")
			b.WriteString(strings.Repeat("\n", n))
			if bodyComments[t.a] {
				b.WriteByte('\t')
			}
		} else if len(ws) > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(text[t.a:t.b])
		p = t.b
	}
	b.WriteString(strings.TrimRight(text[p:], " \t"))
	return b.String()
}

// Smol comments already have normalized Go contents. Hide them from gofmt's
// documentation reflow, whose interpretation depends on surrounding indentation.
func normalizePreservingComments(text string) (string, error) {
	prefix := "SMOL_PRESERVED_COMMENT_"
	for strings.Contains(text, prefix) {
		prefix += "X"
	}
	originals := map[string]string{}
	var es []edit
	for _, t := range lex(text) {
		if t.k != token.COMMENT {
			continue
		}
		key := fmt.Sprintf("%s%08d_END", prefix, len(originals))
		replacement := "//" + key
		if strings.HasPrefix(t.lit, "/*") {
			replacement = "/*" + key + strings.Repeat("\n", strings.Count(t.lit, "\n")) + "*/"
		}
		originals[key] = t.lit
		es = append(es, edit{t.a, t.b, replacement})
	}
	protected, e := apply(text, es)
	if e != nil {
		return "", e
	}
	formatted, e := Normalize(protected)
	if e != nil {
		return "", e
	}
	var restore []edit
	for _, t := range lex(formatted) {
		if t.k != token.COMMENT {
			continue
		}
		for key, original := range originals {
			if strings.Contains(t.lit, key) {
				restore = append(restore, edit{t.a, t.b, original})
				break
			}
		}
	}
	return apply(formatted, restore)
}

func (s *source) packageEnd() int {
	p := s.off(s.file.Name.End())
	line := s.fs.PositionFor(s.file.Name.End(), false).Line
	for _, c := range s.file.Comments {
		if s.fs.PositionFor(c.Pos(), false).Line == line && s.off(c.Pos()) >= p {
			p = s.off(c.End())
		}
	}
	return p
}
