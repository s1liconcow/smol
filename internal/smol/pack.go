package smol

import (
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
)

// A lexeme is one token of Smol text plus the layout of the gap before it.
type lexeme struct {
	lit     string
	comment bool
	nl      int  // newlines in the gap before this lexeme
	tab     bool // the gap ends in a tab (significant comment indentation)
	items   []note
	total   int  // newlines between the previous code lexeme and this one
	inner   bool // inside a bracket pair
}

// A note is a comment removed from the code channel.
type note struct {
	nl   int
	tab  bool
	text string
	keep bool // a compiler directive: stays in the code channel
}

// directive reports whether a comment changes what the Go toolchain builds.
func directive(text string) bool {
	for _, p := range []string{"//go:", "//line ", "//export ", "//extern ", "// +build"} {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

const notesHeader = "smol-notes 1"

func scanLexemes(text string) []lexeme {
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(text))
	var sc scanner.Scanner
	sc.Init(f, []byte(text), func(token.Position, string) {}, scanner.ScanComments)
	var out []lexeme
	p := 0
	for {
		pos, k, l := sc.Scan()
		if k == token.EOF {
			break
		}
		if k == token.SEMICOLON && l == "\n" {
			continue
		}
		if l == "" {
			l = k.String()
		}
		a := f.Offset(pos)
		if a < p {
			continue
		}
		gap := text[p:a]
		if k == token.COMMENT || k == token.STRING {
			// The scanner drops carriage returns; keep the source bytes.
			if end := a + len(l); end <= len(text) && text[a:end] != l {
				l = sourceLiteral(text, a, l)
			}
		}
		p = a + len(l)
		if gap == "" && l == ">" && len(out) > 0 && out[len(out)-1].lit == "=" && !out[len(out)-1].comment {
			out[len(out)-1].lit = "=>"
			continue
		}
		out = append(out, lexeme{lit: l, comment: k == token.COMMENT, nl: strings.Count(gap, "\n"), tab: strings.HasSuffix(gap, "\t") && strings.Contains(gap, "\n")})
	}
	return out
}

// sourceLiteral recovers the exact source bytes of a literal whose scanned
// form had carriage returns removed.
func sourceLiteral(text string, a int, lit string) string {
	i, j := a, 0
	for j < len(lit) && i < len(text) {
		if text[i] == lit[j] {
			i++
			j++
		} else if text[i] == '\r' {
			i++
		} else {
			return lit
		}
	}
	return text[a:i]
}

var spaceCache = map[string]bool{}

// needSpace reports whether two adjacent lexemes need a separating space to
// scan back as the same two lexemes.
func needSpace(a, b string) bool {
	key := a + "\x00" + b
	if v, ok := spaceCache[key]; ok {
		return v
	}
	ls := scanLexemes(a + b)
	v := len(ls) != 2 || ls[0].lit != a || ls[1].lit != b
	if len(key) < 64 {
		spaceCache[key] = v
	}
	return v
}

// codeLexemes folds comments into the gap of the code lexeme that follows.
// A final entry with an empty literal carries comments at the end of the file.
func codeLexemes(text string) []lexeme {
	var out []lexeme
	var items []note
	total, depth := 0, 0
	all := scanLexemes(text)
	for n, x := range all {
		cgoNext := false
		for m := n + 1; m < len(all) && m <= n+2; m++ {
			if !all[m].comment {
				cgoNext = all[m].lit == `"C"`
				break
			}
		}
		total += x.nl
		if x.comment {
			total += strings.Count(x.lit, "\n")
			items = append(items, note{nl: x.nl, tab: x.tab, text: x.lit})
			continue
		}
		for j := range items {
			// The cgo preamble is part of the program, like a directive.
			items[j].keep = directive(items[j].text) || (x.lit == "import" && cgoNext)
		}
		x.items, x.total = items, total
		x.inner = depth > 0
		switch x.lit {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		out = append(out, x)
		items, total = nil, 0
	}
	if len(items) > 0 {
		tail := strings.Count(text[strings.LastIndex(text, items[len(items)-1].text):], "\n") - strings.Count(items[len(items)-1].text, "\n")
		out = append(out, lexeme{items: items, nl: tail, total: total + tail})
	}
	return out
}

type segment struct {
	head       string
	start, end int
}

// segments splits code lexemes at top-level line starts. The split depends
// only on the code channel, so both directions compute the same segments.
func segments(ls []lexeme) []segment {
	var out []segment
	depth := 0
	for i, x := range ls {
		if x.lit == "" {
			break
		}
		if i == 0 || (depth == 0 && (x.total > 0 || impliedBreak(ls, i))) {
			if len(out) > 0 {
				out[len(out)-1].end = i
			}
			out = append(out, segment{start: i})
		}
		switch x.lit {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
	}
	n := len(ls)
	if n > 0 && ls[n-1].lit == "" {
		n--
	}
	if len(out) > 0 {
		out[len(out)-1].end = n
	}
	seen := map[string]int{}
	for i := range out {
		var b strings.Builder
		for j := out[i].start; j < out[i].end && j < out[i].start+10; j++ {
			if j > out[i].start && (ls[j].total > 0 || impliedBreak(ls, j) || ls[j].lit == "{") {
				break
			}
			if j > out[i].start && needSpace(ls[j-1].lit, ls[j].lit) {
				b.WriteByte(' ')
			}
			b.WriteString(ls[j].lit)
		}
		h := strings.ReplaceAll(b.String(), "\n", " ")
		seen[h]++
		if seen[h] > 1 {
			h += " #" + strconv.Itoa(seen[h])
		}
		out[i].head = h
	}
	return out
}

// joinable lists the statement keywords before which a line break is implied.
var joinable = map[string]bool{"if": true, "for": true, "switch": true, "select": true, "return": true, "var": true,
	"go": true, "defer": true, "break": true, "continue": true, "goto": true, "fallthrough": true}

func wordy(l string) bool {
	if l == "" {
		return false
	}
	c := l[len(l)-1]
	return c == '_' || c >= 0x80 || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '"' || c == '\'' || c == '`'
}

// impliedBreak reports whether the line break before ls[i] is implied (§5):
// inside a body, a statement keyword that follows a word or literal on the
// same line starts a new line.
func impliedBreak(ls []lexeme, i int) bool {
	if i == 0 || !joinable[ls[i].lit] || !ls[i].inner {
		return false
	}
	p := ls[i-1].lit
	return wordy(p) && p != "else" && !joinable[p] && p != "case"
}

// operandGap reports whether two lexemes are operands with nothing between
// them, as around an implied argument comma; they are spaced for legibility.
func operandGap(a, b string) bool {
	if !wordy(a) || b == "" {
		return false
	}
	quote := func(c byte) bool { return c == '"' || c == '\'' || c == '`' }
	return quote(b[0]) || (quote(a[len(a)-1]) && wordy(b[:1]))
}

// Pack splits Smol text into the dense code channel and the notes channel.
// Comments and blank lines move to the notes; optional spaces are dropped.
func Pack(text string) (code, notes string) {
	ls := codeLexemes(text)
	segs := segments(ls)
	var cb, nb strings.Builder
	si := 0
	for i, x := range ls {
		for si+1 < len(segs) && i >= segs[si+1].start {
			si++
		}
		kept := false
		for _, it := range x.items {
			if !it.keep {
				continue
			}
			if cb.Len() > 0 {
				cb.WriteByte('\n')
			}
			if it.tab {
				cb.WriteByte('\t')
			}
			cb.WriteString(it.text)
			kept = true
		}
		if x.lit != "" {
			switch {
			case kept:
				cb.WriteByte('\n')
			case i == 0:
			case x.total > 0 && impliedBreak(ls, i):
				cb.WriteByte(' ')
			case x.total > 0:
				cb.WriteByte('\n')
			case needSpace(ls[i-1].lit, x.lit) || operandGap(ls[i-1].lit, x.lit):
				cb.WriteByte(' ')
			}
			cb.WriteString(x.lit)
		}
	}
	if cb.Len() > 0 {
		cb.WriteByte('\n')
	}
	for n, sg := range segs {
		end := sg.end
		if n == len(segs)-1 {
			end = len(ls)
		}
		var b strings.Builder
		for i := sg.start; i < end; i++ {
			x := ls[i]
			k := i - sg.start
			if len(x.items) == 0 {
				if x.total > 1 && i > 0 {
					fmt.Fprintf(&b, "%d b\n", k)
				}
				continue
			}
			for _, it := range x.items {
				t := 0
				if it.tab {
					t = 1
				}
				if it.keep {
					fmt.Fprintf(&b, "%d d%d%d\n", k, min(it.nl, 2), t)
					continue
				}
				fmt.Fprintf(&b, "%d c%d%d %s\n", k, min(it.nl, 2), t, strconv.Quote(it.text))
			}
			fmt.Fprintf(&b, "%d e%d\n", k, min(x.nl, 2))
		}
		if b.Len() > 0 {
			fmt.Fprintf(&nb, "@%d %s\n", sg.end-sg.start, sg.head)
			nb.WriteString(b.String())
		}
	}
	if nb.Len() > 0 {
		notes = notesHeader + "\n" + nb.String()
	}
	return cb.String(), notes
}

type segNotes struct {
	count   int
	entries map[int][]string
	order   []int
}

func parseNotes(notes string) (map[string]*segNotes, error) {
	out := map[string]*segNotes{}
	if strings.TrimSpace(notes) == "" {
		return out, nil
	}
	lines := strings.Split(strings.TrimRight(notes, "\n"), "\n")
	if lines[0] != notesHeader {
		return nil, fmt.Errorf("notes: expected %q header", notesHeader)
	}
	var cur *segNotes
	for i, l := range lines[1:] {
		if strings.HasPrefix(l, "@") {
			sp := strings.IndexByte(l, ' ')
			if sp < 0 {
				return nil, fmt.Errorf("notes:%d: malformed segment", i+2)
			}
			n, e := strconv.Atoi(l[1:sp])
			if e != nil {
				return nil, fmt.Errorf("notes:%d: malformed segment", i+2)
			}
			cur = &segNotes{count: n, entries: map[int][]string{}}
			out[l[sp+1:]] = cur
			continue
		}
		sp := strings.IndexByte(l, ' ')
		if cur == nil || sp < 0 {
			return nil, fmt.Errorf("notes:%d: malformed entry", i+2)
		}
		k, e := strconv.Atoi(l[:sp])
		if e != nil {
			return nil, fmt.Errorf("notes:%d: malformed entry", i+2)
		}
		if _, ok := cur.entries[k]; !ok {
			cur.order = append(cur.order, k)
		}
		cur.entries[k] = append(cur.entries[k], l[sp+1:])
	}
	return out, nil
}

// Unpack merges the notes back into the code channel. Notes of a declaration
// whose code changed keep their doc comment; its interior comments move above
// the declaration, and notes of declarations that no longer exist are
// reported.
func Unpack(code, notes string) (string, []string, error) {
	table, err := parseNotes(notes)
	if err != nil {
		return "", nil, err
	}
	ls := codeLexemes(code)
	if n := len(ls); n > 0 && ls[n-1].lit != "" {
		ls = append(ls, lexeme{}) // anchor for comments at the end of the file
	}
	segs := segments(ls)
	var warnings []string
	var b strings.Builder
	used := map[string]bool{}
	writeEntries := func(es []string, first bool, inCode []note) error {
		for _, e := range es {
			switch {
			case e == "b":
				b.WriteString("\n\n")
			case strings.HasPrefix(e, "d") && len(e) == 3:
				if len(inCode) == 0 {
					continue // the directive was deleted from the code channel
				}
				b.WriteString(strings.Repeat("\n", int(e[1]-'0')))
				if e[2] == '1' {
					b.WriteByte('\t')
				}
				b.WriteString(inCode[0].text)
				inCode = inCode[1:]
				first = false
			case strings.HasPrefix(e, "c") && len(e) > 4:
				text, err := strconv.Unquote(e[4:])
				if err != nil {
					return fmt.Errorf("notes: malformed comment %q", e)
				}
				n := int(e[1] - '0')
				if n == 0 && !first {
					b.WriteByte(' ')
				}
				b.WriteString(strings.Repeat("\n", n))
				if e[2] == '1' {
					b.WriteByte('\t')
				}
				b.WriteString(text)
				first = false
			case strings.HasPrefix(e, "e") && len(e) == 2:
				n := int(e[1] - '0')
				for _, it := range inCode { // comments added to the code channel
					b.WriteString("\n" + it.text)
					n = max(n, 1)
				}
				if n == 0 {
					b.WriteByte(' ')
				}
				b.WriteString(strings.Repeat("\n", n))
			default:
				return fmt.Errorf("notes: malformed entry %q", e)
			}
		}
		return nil
	}
	plain := func(i int) {
		x := ls[i]
		for j, it := range x.items { // comments written in the code channel stay in place
			if it.nl == 0 && (i > 0 || j > 0) {
				b.WriteByte(' ')
			}
			b.WriteString(strings.Repeat("\n", it.nl))
			if it.tab {
				b.WriteByte('\t')
			}
			b.WriteString(it.text)
		}
		switch {
		case (i == 0 || x.lit == "") && len(x.items) == 0:
		case len(x.items) > 0:
			if x.nl == 0 {
				b.WriteByte(' ')
			}
			nl := x.nl
			if last := x.items[len(x.items)-1].text; x.lit == "package" && nl == 1 && (strings.HasPrefix(last, "//go:build") || strings.HasPrefix(last, "// +build")) {
				nl = 2 // a build constraint needs a blank line before the clause
			}
			b.WriteString(strings.Repeat("\n", nl))
		case x.total > 0:
			b.WriteString(strings.Repeat("\n", min(x.total, 2)))
		case impliedBreak(ls, i):
			b.WriteByte('\n')
		case needSpace(ls[i-1].lit, x.lit) || operandGap(ls[i-1].lit, x.lit):
			b.WriteByte(' ')
		}
	}
	for n, sg := range segs {
		sn := table[sg.head]
		used[sg.head] = true
		end := sg.end
		if n == len(segs)-1 {
			end = len(ls)
		}
		intact := sn != nil && sn.count == sg.end-sg.start
		if sn != nil && !intact {
			warnings = append(warnings, "notes: "+sg.head+" changed; its interior comments were moved above it")
			// Doc comment first, then every interior comment on its own line.
			var doc, rest []string
			for _, k := range sn.order {
				for _, e := range sn.entries[k] {
					if k == 0 {
						doc = append(doc, e)
					} else if strings.HasPrefix(e, "c") && len(e) > 4 {
						rest = append(rest, "c10"+e[3:])
					}
				}
			}
			if len(rest) > 0 {
				lead := []string{}
				if n > 0 {
					lead = append(lead, "c1"+rest[0][2:])
				} else {
					lead = append(lead, "c0"+rest[0][2:])
				}
				lead = append(lead, rest[1:]...)
				if len(doc) == 0 {
					doc = append(lead, "e1")
				} else {
					doc = append(lead, doc...)
				}
			}
			sn = &segNotes{count: sg.end - sg.start, entries: map[int][]string{0: doc}}
			if len(doc) == 0 {
				sn = nil
			}
		}
		for i := sg.start; i < end; i++ {
			var es []string
			if sn != nil {
				es = sn.entries[i-sg.start]
			}
			if len(es) == 0 {
				plain(i)
			} else if err := writeEntries(es, i == 0, ls[i].items); err != nil {
				return "", nil, err
			}
			b.WriteString(ls[i].lit)
		}
	}
	for h := range table {
		if !used[h] {
			warnings = append(warnings, "notes: no declaration matches "+strconv.Quote(h)+"; its comments were dropped")
		}
	}
	out := b.String()
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out, warnings, nil
}

// Outline returns a read-only view of Go source: the code channel with every
// function body removed, optionally with doc comments. It is not a program
// and cannot be lowered back to the original.
func Outline(text string, o Options, docs bool) (string, error) {
	text, err := Normalize(text)
	if err != nil {
		return "", err
	}
	s, err := parse(text)
	if err != nil {
		return "", err
	}
	for _, d := range s.file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Body != nil {
			s.put(s.off(f.Type.End()), s.off(f.Body.End()), "")
		}
	}
	wide, err := Lift(s.render(0, len(text)), o)
	if err != nil {
		return "", err
	}
	code, notes := Pack(wide)
	if !docs || notes == "" {
		return code, nil
	}
	var keep []string
	for _, l := range strings.Split(strings.TrimRight(notes, "\n"), "\n") {
		if l == notesHeader || strings.HasPrefix(l, "@") || strings.HasPrefix(l, "0 c") || strings.HasPrefix(l, "0 e") {
			keep = append(keep, l)
		}
	}
	out, _, err := Unpack(code, strings.Join(keep, "\n")+"\n")
	return out, err
}
