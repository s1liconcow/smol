package enc

import (
	"fmt"
	"go/scanner"
	"go/token"
	"sort"
	"strings"
	"unicode"
)

type source struct {
	lines                  []string
	low, end, block        []int
	inside, after, comment []bool
}

func lines(text string) []string { return strings.Split(strings.TrimSuffix(text, "\n"), "\n") }
func key(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
func scan(text string) *source {
	ss := lines(text)
	if text == "" {
		ss = nil
	}
	s := &source{lines: ss, low: make([]int, len(ss)), end: make([]int, len(ss)), block: make([]int, len(ss)), inside: make([]bool, len(ss)), after: make([]bool, len(ss)), comment: make([]bool, len(ss))}
	starts := make([]int, len(ss))
	off := 0
	for i, l := range ss {
		starts[i] = off
		off += len(l) + 1
	}
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(text))
	var sc scanner.Scanner
	sc.Init(f, []byte(text), func(token.Position, string) {}, scanner.ScanComments)
	type item struct {
		a, b int
		k    token.Token
		lit  string
	}
	var ts []item
	for {
		p, k, l := sc.Scan()
		if k == token.EOF {
			break
		}
		if k == token.SEMICOLON {
			continue
		}
		if l == "" {
			l = k.String()
		}
		a := f.Offset(p)
		b := a + len(l)
		// Scanner omits CR bytes; find the actual end of multi-line tokens.
		if k == token.STRING && strings.HasPrefix(l, "`") {
			if j := strings.Index(text[a+1:], "`"); j >= 0 {
				b = a + j + 2
			}
		}
		if k == token.COMMENT && strings.HasPrefix(l, "/*") {
			if j := strings.Index(text[a+2:], "*/"); j >= 0 {
				b = a + j + 4
			}
		}
		ts = append(ts, item{a, b, k, l})
	}
	continuationComment := make([]bool, len(ss))
	for _, t := range ts {
		first := sort.SearchInts(starts, t.a+1) - 1
		for i := first + 1; i < len(ss) && starts[i] < t.b; i++ {
			s.inside[i] = true
			continuationComment[i] = t.k == token.COMMENT
		}
		for i := max(0, first); i < len(ss) && starts[i]+len(ss[i]) < t.b; i++ {
			s.after[i] = true
		}
	}
	depth, j := 0, 0
	for i, l := range ss {
		s.low[i] = depth
		lineEnd := starts[i] + len(l)
		hasCode, hasComment := s.inside[i] && !continuationComment[i], continuationComment[i]
		for j < len(ts) && ts[j].a <= lineEnd {
			t := ts[j]
			j++
			if t.a < starts[i] {
				continue
			}
			if t.k == token.COMMENT {
				hasComment = true
				continue
			}
			hasCode = true
			switch t.k {
			case token.LPAREN, token.LBRACK, token.LBRACE:
				depth++
			case token.RPAREN, token.RBRACK, token.RBRACE:
				depth--
				if depth < s.low[i] {
					s.low[i] = depth
				}
			}
		}
		s.end[i] = depth
		s.comment[i] = hasComment && !hasCode
	}
	for i := range ss {
		s.block[i] = i
		if s.end[i] > s.low[i] {
			for j := i + 1; j < len(ss); j++ {
				if s.end[j] <= s.low[i] && !s.after[j] {
					s.block[i] = j
					break
				}
			}
		}
	}
	return s
}
func (s *source) matches(a string, lo, hi int) []int {
	k := key(a)
	var out []int
	if k == "" {
		return out
	}
	for i := lo; i < hi; i++ {
		if !s.inside[i] && key(s.lines[i]) != "" && strings.HasPrefix(key(s.lines[i]), k) {
			out = append(out, i)
		}
	}
	return out
}
func (s *source) enclosing(i int) int {
	for j := i - 1; j >= 0; j-- {
		if s.block[j] >= i && s.block[j] > j {
			return j
		}
	}
	return -1
}
func (s *source) attached(i int) int {
	for i > 0 && s.comment[i-1] && strings.TrimSpace(s.lines[i-1]) != "" {
		i--
	}
	return i
}
func (s *source) anchor(i int) string {
	if i < 0 || i >= len(s.lines) || s.inside[i] {
		return ""
	}
	a := strings.TrimSpace(s.lines[i])
	if j := strings.Index(a, " @ "); j >= 0 {
		a = a[:j]
	}
	if m := s.matches(a, 0, len(s.lines)); len(m) == 1 && m[0] == i {
		return a
	}
	for p := s.enclosing(i); p >= 0; p = s.enclosing(p) {
		scope := s.anchor(p)
		if scope != "" {
			if m := s.matches(a, p+1, s.block[p]+1); len(m) == 1 && m[0] == i {
				return scope + " @ " + a
			}
		}
	}
	return ""
}
func (s *source) resolve(a, path string, line int) (int, error) {
	lo, hi := 0, len(s.lines)
	parts := strings.Split(a, " @ ")
	for n, p := range parts {
		m := s.matches(p, lo, hi)
		if len(m) != 1 {
			var b strings.Builder
			fmt.Fprintf(&b, "anchor %q matches %d lines in %s; lengthen it or add a scope", p, len(m), path)
			for _, i := range m {
				suggest := s.anchor(i)
				if suggest == "" {
					suggest = strings.TrimSpace(s.lines[i])
					if parent := s.enclosing(i); parent >= 0 {
						suggest = strings.TrimSpace(s.lines[parent]) + " @ " + suggest + fmt.Sprintf(" (%d matches in this block)", len(s.matches(p, parent+1, s.block[parent]+1)))
					}
				}
				fmt.Fprintf(&b, "\n  %d  %s", i+1, suggest)
			}
			return 0, patchError(line, b.String())
		}
		i := m[0]
		if n == len(parts)-1 {
			return i, nil
		}
		if s.block[i] == i {
			return 0, patchError(line, fmt.Sprintf("scope %q opens no block; anchor a block opener", p))
		}
		lo, hi = i+1, s.block[i]+1
	}
	return 0, patchError(line, "empty anchor; supply a non-empty prefix")
}
func (s *source) rangeEnd(a int, b, path string, line int) (int, error) {
	hi := len(s.lines)
	if p := s.enclosing(a); p >= 0 {
		hi = s.block[p] + 1
	}
	m := s.matches(b, a+1, hi)
	if len(m) != 1 {
		return 0, patchError(line, fmt.Sprintf("range end %q matches %d lines after anchor inside its block in %s; choose a unique end in that region", b, len(m), path))
	}
	return m[0], nil
}

type change struct {
	a, b, line, order int
	body              []string
	origins           []int
	kind              string
}

func apply(s *source, cs []change) ([]string, []int, error) {
	for i, c := range cs {
		for _, d := range cs[:i] {
			if c.a < c.b && d.a < d.b && c.a < d.b && d.a < c.b || c.a == c.b && d.a < c.a && c.a < d.b || d.a == d.b && c.a < d.a && d.a < c.b {
				return nil, nil, patchError(c.line, "overlapping spans; combine the edits into one operation")
			}
		}
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].a != cs[j].a {
			return cs[i].a < cs[j].a
		}
		if (cs[i].a == cs[i].b) != (cs[j].a == cs[j].b) {
			return cs[i].a == cs[i].b
		}
		return cs[i].order < cs[j].order
	})
	var out []string
	var origins []int
	p := 0
	for _, c := range cs {
		if c.a > p {
			out = append(out, s.lines[p:c.a]...)
			origins = append(origins, make([]int, c.a-p)...)
		}
		out = append(out, c.body...)
		origins = append(origins, c.origins...)
		p = max(p, c.b)
	}
	out = append(out, s.lines[p:]...)
	origins = append(origins, make([]int, len(s.lines)-p)...)
	return out, origins, nil
}
