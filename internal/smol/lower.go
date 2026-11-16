package smol

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// Lower first desugars syntax to parseable Go, then uses declaration information
// for zero-value returns. No expression type information is required.
func Lower(text string, o Options) (string, error) {
	original := text
	if o.LineSource != "" {
		original = o.LineSource
	}
	text, _, err := Unpack(text, "") // make implied line breaks explicit
	if err != nil {
		return "", err
	}
	prefix := "__smol_internal_"
	for strings.Contains(text, prefix) {
		prefix += "x"
	}
	var uses []string
	text, uses, _, err = desugar(text, prefix)
	if err != nil {
		return "", err
	}
	if text, err = completeNames(text, o, uses); err != nil {
		return "", err
	}
	s, err := parse(text)
	if err != nil {
		return "", fmt.Errorf("lower: %w", err)
	}
	z := newZeros(s, o)
	var contexts []*ast.FuncType
	var walkErr error
	ast.Inspect(s.file, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		switch f := n.(type) {
		case *ast.FuncDecl:
			contexts = append(contexts, f.Type)
			lowerBody(s, f.Body, f.Type, z, prefix, &walkErr)
			return false
		case *ast.FuncLit:
			contexts = append(contexts, f.Type)
			lowerBody(s, f.Body, f.Type, z, prefix, &walkErr)
			return false
		}
		return true
	})
	_ = contexts
	if walkErr != nil {
		return "", walkErr
	}
	text = s.render(0, len(text))
	s, err = parse(text)
	if err != nil {
		return "", err
	}
	text, err = s.inferImports(o, prefix)
	if err != nil {
		return "", err
	}
	text, err = normalizePreservingComments(text)
	if err != nil {
		return "", err
	}
	if o.Lines {
		name := o.Filename
		if name == "" {
			name = "source.smol"
		}
		text = lineDirectives(text, original, name)
	}
	return text, nil
}
func lowerDecls(text string) (string, error) {
	ts := lex(text)
	var es []edit
	depth := 0
	start := true
	prev := ""
	bangLine := 0
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		if depth == 0 && bangLine > 0 && t.line > bangLine {
			start = true
			bangLine = 0
		}
		if depth == 0 && bangLine > 0 && t.lit != "!" && t.k != token.COMMENT && t.k != token.SEMICOLON {
			bangLine = 0
		}
		if depth == 0 && t.lit == "!" {
			bangLine = t.line
		}
		if t.k == token.COMMENT {
			continue
		}
		if depth == 0 && start {
			if t.lit == "." {
				if prev == "" {
					return "", fmt.Errorf("line %d: receiver inheritance has no preceding receiver", t.line)
				}
				es = append(es, edit{t.a, t.b, "func " + prev + " "})
				start = false
			} else if t.lit == "(" {
				j := matching(ts, i)
				if j < 0 {
					return "", fmt.Errorf("unclosed receiver")
				}
				prev = text[t.a:ts[j].b]
				es = append(es, edit{t.a, t.a, "func "})
				start = false
			} else if t.k == token.IDENT && i+1 < len(ts) && (ts[i+1].lit == "(" || ts[i+1].lit == "[") {
				es = append(es, edit{t.a, t.a, "func "})
				start = false
			} else if t.lit == "func" {
				if i+1 < len(ts) && ts[i+1].lit == "(" {
					j := matching(ts, i+1)
					if j >= 0 {
						prev = text[ts[i+1].a:ts[j].b]
					}
				}
				start = false
			} else if t.k != token.SEMICOLON {
				start = false
			}
		}
		switch t.lit {
		case "{", "(", "[":
			depth++
		case "}", ")", "]":
			depth--

		}
		if depth == 0 && t.k == token.SEMICOLON {
			start = true
		}
	}
	return apply(text, es)
}

// consumeType returns the first token after a Go type (including nested signatures).
func consumeType(ts []tok, i int) int {
	if i >= len(ts) {
		return i
	}
	switch ts[i].lit {
	case "*", "...", "~":
		return consumeType(ts, i+1)
	case "<-":
		return consumeType(ts, i+2)
	case "chan":
		if i+1 < len(ts) && ts[i+1].lit == "<-" {
			return consumeType(ts, i+2)
		}
		return consumeType(ts, i+1)
	case "[":
		j := matching(ts, i)
		if j < 0 {
			return i
		}
		return consumeType(ts, j+1)
	case "map":
		if i+1 < len(ts) && ts[i+1].lit == "[" {
			j := matching(ts, i+1)
			if j >= 0 {
				return consumeType(ts, j+1)
			}
		}
	case "struct", "interface":
		j := i + 1
		for j < len(ts) && ts[j].lit != "{" {
			j++
		}
		if j < len(ts) {
			k := matching(ts, j)
			if k >= 0 {
				return k + 1
			}
		}
	case "func":
		if i+1 < len(ts) && ts[i+1].lit == "(" {
			j := matching(ts, i+1)
			if j >= 0 {
				return consumeResult(ts, j+1)
			}
		}
	case "(":
		j := matching(ts, i)
		if j >= 0 {
			return j + 1
		}
	default:
		if ts[i].k == token.IDENT {
			i++
			if i+1 < len(ts) && ts[i].lit == "." {
				i += 2
			}
			if i < len(ts) && ts[i].lit == "[" {
				j := matching(ts, i)
				if j >= 0 {
					i = j + 1
				}
			}
			return i
		}
	}
	return i
}
func consumeResult(ts []tok, i int) int {
	if i >= len(ts) {
		return i
	}
	if ts[i].lit == "(" {
		j := matching(ts, i)
		if j >= 0 {
			return j + 1
		}
	}
	return consumeType(ts, i)
}

type signature struct{ open, close, end int }

func signatures(ts []tok) []signature {
	var out []signature
	seen := map[int]bool{}
	add := func(i int) {
		if i < 0 || i >= len(ts) || ts[i].lit != "(" || seen[i] {
			return
		}
		j := matching(ts, i)
		if j < 0 {
			return
		}
		seen[i] = true
		out = append(out, signature{i, j, consumeResult(ts, j+1)})
	}
	for i, t := range ts {
		if t.lit == "func" {
			j := i + 1
			if j >= len(ts) {
				continue
			}
			if ts[j].lit == "(" {
				k := matching(ts, j)
				if k >= 0 && k+2 < len(ts) && ts[k+1].k == token.IDENT && (ts[k+2].lit == "(" || ts[k+2].lit == "[") {
					j = k + 2
				} else {
					add(j)
					continue
				}
			} else {
				j++
			}
			if j < len(ts) && ts[j].lit == "[" {
				k := matching(ts, j)
				if k < 0 {
					continue
				}
				j = k + 1
			}
			add(j)
		}
		if t.lit == "interface" && i+1 < len(ts) && ts[i+1].lit == "{" {
			end := matching(ts, i+1)
			for j := i + 2; j < end; j++ {
				if ts[j].k == token.IDENT && j+1 < end && ts[j+1].lit == "(" {
					add(j + 1)
					k := matching(ts, j+1)
					if k >= 0 {
						j = k
					}
				} else if ts[j].lit == "{" || ts[j].lit == "[" || ts[j].lit == "(" {
					k := matching(ts, j)
					if k >= 0 {
						j = k
					}
				}
			}
		}
	}
	return out
}
func lowerSignatures(text string, conventions map[string]string) (string, error) {
	ts := lex(text)
	var es []edit
	for _, sig := range signatures(ts) {
		op, cl := sig.open, sig.close
		// Split the parameter list at commas outside nested types.
		var groups [][]tok
		start := op + 1
		for i := start; i < cl; i++ {
			if ts[i].lit == "(" || ts[i].lit == "[" || ts[i].lit == "{" {
				j := matching(ts, i)
				if j >= 0 {
					i = j
				}
				continue
			}
			if ts[i].lit == "," {
				groups = append(groups, ts[start:i])
				start = i + 1
			}
		}
		if start < cl {
			groups = append(groups, ts[start:cl])
		}
		allBare := true
		allConventional := true
		for _, g := range groups {
			g = significant(g)
			if len(g) != 1 || g[0].k != token.IDENT {
				allBare = false
			}
			if len(g) != 1 || conventions[g[0].lit] == "" {
				allConventional = false
			}
		}
		for _, g := range groups {
			g = significant(g)
			if len(g) == 0 {
				continue
			}
			if g[0].lit == "\\" {
				es = append(es, edit{g[0].a, g[0].b, ""})
				continue
			}
			if g[0].k != token.IDENT || conventions[g[0].lit] == "" {
				continue
			}
			if len(g) == 1 && (!allBare || allConventional) {
				es = append(es, edit{g[0].b, g[0].b, " " + conventions[g[0].lit]})
			}
		}
	}
	text, err := apply(text, es)
	if err != nil {
		return "", err
	}
	for {
		ts = lex(text)
		sigs := signatures(ts)
		found := false
		sort.Slice(sigs, func(i, j int) bool { return sigs[i].open > sigs[j].open })
		for _, sig := range sigs {
			end := sig.end
			cl := sig.close
			if end >= len(ts) || ts[end].lit != "!" {
				continue
			}
			a, b := ts[cl].b, ts[end].b
			result := strings.TrimSpace(text[a:ts[end].a])
			v := "error"
			if result != "" {
				if strings.HasPrefix(result, "(") && strings.HasSuffix(result, ")") {
					result = strings.TrimSpace(result[1 : len(result)-1])
					result = strings.TrimSuffix(result, ",")
				}
				v = "(" + result + ", error)"
			}
			text = text[:a] + " " + v + text[b:]
			found = true
			break
		}
		if !found {
			return text, nil
		}
	}

}
func lowerTags(text string) (string, error) {
	ts := lex(text)
	var es []edit
	for i, t := range ts {
		if t.lit != "struct" {
			continue
		}
		j := i + 1
		var keys []string
		for j+1 < len(ts) && ts[j].lit == "@" {
			keys = append(keys, ts[j+1].lit)
			j += 2
		}
		if len(keys) == 0 {
			continue
		}
		if j >= len(ts) || ts[j].lit != "{" {
			return "", fmt.Errorf("line %d: expected struct body", t.line)
		}
		end := matching(ts, j)
		if end < 0 {
			return "", fmt.Errorf("unclosed struct")
		}
		es = append(es, edit{t.b, ts[j].a, " "})
		start := j + 1
		for k := start; k < end; k++ {
			if ts[k].lit == "(" || ts[k].lit == "[" || ts[k].lit == "{" {
				m := matching(ts, k)
				if m >= 0 {
					k = m
				}
				continue
			}
			if ts[k].k == token.SEMICOLON || k == end-1 {
				stop := k
				if k == end-1 && ts[k].k != token.SEMICOLON {
					stop = k + 1
				}
				g := significant(ts[start:stop])
				start = k + 1
				if len(g) < 2 || g[0].k != token.IDENT || !ast.IsExported(g[0].lit) || g[1].lit == "," || g[1].lit == "." {
					continue
				}
				last := g[len(g)-1]
				if last.lit == "~" {
					es = append(es, edit{last.a, last.b, ""})
					continue
				}
				if last.k == token.STRING {
					continue
				}
				if consumeType(g, 1) != len(g) {
					continue
				}
				es = append(es, edit{last.b, last.b, " " + implied(keys, g[0].lit)})
			}
		}
	}
	return apply(text, es)
}
func lowerRanges(text string) (string, error) {
	ts := lex(text)
	var es []edit
	for i, t := range ts {
		if t.lit != "for" || i+2 >= len(ts) {
			continue
		}
		j := i + 1
		if ts[j].k != token.IDENT {
			continue
		}
		first := ts[j]
		j++
		count := 1
		if j < len(ts) && ts[j].lit == "," {
			j++
			if j >= len(ts) || ts[j].k != token.IDENT {
				continue
			}
			j++
			count = 2
		}
		if j >= len(ts) || ts[j].lit != "in" {
			continue
		}
		if count == 1 {
			es = append(es, edit{first.a, first.a, "_, "})
		}
		es = append(es, edit{ts[j].a, ts[j].b, ":= range"})
	}
	return apply(text, es)
}

// expressionEnd honors nested literals, calls and indexing; delimiters belong to
// the enclosing expression, so arrow literals work inside calls and composites.
func expressionEnd(ts []tok, i int) int {
	for j := i; j < len(ts); j++ {
		switch ts[j].lit {
		case "(", "[", "{":
			k := matching(ts, j)
			if k < 0 {
				return j
			}
			j = k
		case ")", "]", "}", ";", "\n":
			return j
		case ",":
			return j
		}
	}
	return len(ts)
}
func lowerArrows(text string) (string, error) {
	// Rewrite the rightmost arrow first so nested function literals are handled.
	for {
		ts := lex(text)
		idx := -1
		for i := 0; i+1 < len(ts); i++ {
			if ts[i].lit == "=" && ts[i+1].lit == ">" {
				idx = i
			}
		}
		if idx < 0 {
			return text, nil
		}
		end := expressionEnd(ts, idx+2) // Declarations can return multiple expressions.
		literal := false
		resultCount := 1
		for _, sig := range signatures(ts) {
			if sig.end == idx {
				if sig.close+1 < sig.end && ts[sig.close+1].lit == "(" {
					for j := sig.close + 2; j < sig.end-1; j++ {
						if ts[j].lit == "(" || ts[j].lit == "[" || ts[j].lit == "{" {
							k := matching(ts, j)
							if k >= 0 {
								j = k
							}
							continue
						}
						if ts[j].lit == "," {
							resultCount++
						}
					}
				}
				for j := sig.open - 1; j >= 0; j-- {
					if ts[j].lit == "func" {
						literal = j+1 == sig.open
						break
					}
					if ts[j].k == token.SEMICOLON || ts[j].lit == "{" {
						break
					}
				}
				break
			}
		}
		if !literal {
			resultCount = len(ts)
		}
		for consumed := 1; consumed < resultCount && end < len(ts) && ts[end].lit == ","; consumed++ {
			end = expressionEnd(ts, end+1)
		}
		b := len(text)
		if end < len(ts) {
			b = ts[end].a
		}
		expr := strings.TrimSpace(text[ts[idx+1].b:b])
		if expr == "" {
			return "", fmt.Errorf("line %d: empty expression body", ts[idx].line)
		}
		body := "{ return " + expr + " }"
		if !literal {
			body = "{\nreturn " + expr + "\n}"
		}
		text = text[:ts[idx].a] + body + text[b:]
	}
}
func statementEnd(ts []tok, i int) int {
	for j := i; j < len(ts); j++ {
		switch ts[j].lit {
		case "(", "[", "{":
			k := matching(ts, j)
			if k >= 0 {
				j = k
			}
		case "}", ";", "\n":
			return j
		default:
			if ts[j].k == token.COMMENT && strings.HasPrefix(ts[j].lit, "//") {
				return j
			}
		}
	}
	return len(ts)
}
func statementStart(ts []tok, i int) int {
	for j := i - 1; j >= 0; j-- {
		switch ts[j].lit {
		case ")", "]", "}":
			close := ts[j].lit
			open := map[string]string{")": "(", "]": "[", "}": "{"}[close]
			d := 1
			for j--; j >= 0; j-- {
				if ts[j].lit == close {
					d++
				}
				if ts[j].lit == open {
					d--
					if d == 0 {
						break
					}
				}
			}
		case "{", ";", "\n", ":":
			return j + 1
		}
	}
	return 0
}
func lowerStatements(text, prefix string) (string, error) {
	for {
		ts := lex(text)
		idx := -1
		for i, t := range ts {
			if t.lit == "?" {
				idx = i
			}
		}
		if idx < 0 {
			break
		}
		start := statementStart(ts, idx)
		for start < idx && (ts[start].k == token.COMMENT || ts[start].k == token.SEMICOLON) {
			start++
		}
		if start >= idx {
			return "", fmt.Errorf("line %d: ? requires a statement", ts[idx].line)
		}
		end := idx + 1
		body := "return " + prefix + "try()"
		if end < len(ts) && ts[end].lit == "{" {
			k := matching(ts, end)
			if k < 0 {
				return "", fmt.Errorf("unclosed ? handler")
			}
			body = strings.TrimSpace(text[ts[end].b:ts[k].a])
			end = k + 1
		} else if end < len(ts) && ts[end].k == token.STRING && ts[end].line == ts[idx].line {
			if !strings.HasPrefix(ts[end].lit, `"`) {
				return "", fmt.Errorf("line %d: ? format must be interpreted string", ts[end].line)
			}
			stop := statementEnd(ts, end)
			b := len(text)
			if stop < len(ts) {
				b = ts[stop].a
			}
			format := ts[end].lit
			args := text[ts[end].b:b]
			body = "return " + prefix + "wrap(" + strings.TrimSuffix(format, `"`) + `: %w"` + args + ", err)"
			end = stop
		}
		a := ts[start].a
		b := ts[idx].a
		stmt := strings.TrimSpace(text[a:b])
		g := significant(ts[start:idx])
		assign := -1
		for j := 0; j < len(g); j++ {
			if g[j].lit == "(" || g[j].lit == "[" || g[j].lit == "{" {
				k := matching(g, j)
				if k >= 0 {
					j = k
				}
				continue
			}
			if g[j].lit == ":=" || g[j].lit == "=" {
				assign = j
				break
			}
		}
		block := "{\n" + body + "\n}"
		if body == "" {
			block = "{\n}"
		}
		expansion := ""
		if assign >= 0 {
			lhs := strings.TrimSpace(text[a:g[assign].a])
			if g[assign-1].lit != "err" {
				lhs += ", err"
			}
			rhs := strings.TrimSpace(text[g[assign].b:b])
			expansion = lhs + " " + g[assign].lit + " " + rhs + "\nif err != nil " + block
		} else {
			expansion = "if err := " + stmt + "; err != nil " + block
		}
		stop := ts[idx].b
		if end > idx+1 {
			stop = ts[end-1].b
		}
		text = text[:a] + expansion + text[stop:]
	}
	ts := lex(text)
	blockBraces := map[int]bool{}
	for _, sig := range signatures(ts) {
		if sig.end < len(ts) && ts[sig.end].lit == "{" {
			blockBraces[ts[sig.end].a] = true
		}
	}
	for i, t := range ts {
		if t.lit != "if" && t.lit != "for" && t.lit != "switch" && t.lit != "select" && t.lit != "else" {
			continue
		}
		for j := i + 1; j < len(ts); j++ {
			if ts[j].lit == "(" || ts[j].lit == "[" {
				k := matching(ts, j)
				if k >= 0 {
					j = k
				}
				continue
			}
			if ts[j].lit == "{" {
				if j >= 2 && ts[j-1].k == token.IDENT && ts[j-2].lit == "]" {
					k := matching(ts, j)
					if k >= 0 {
						j = k
						continue
					}
				}
				blockBraces[ts[j].a] = true
				break
			}
			if ts[j].lit == "}" {
				break
			}
		}
	}
	literalBraces := map[int]bool{}
	fs := token.NewFileSet()
	tree, _ := parser.ParseFile(fs, "", text, parser.AllErrors|parser.SkipObjectResolution)
	if tree != nil {
		ast.Inspect(tree, func(n ast.Node) bool {
			if c, ok := n.(*ast.CompositeLit); ok && !blockBraces[fs.PositionFor(c.Lbrace, false).Offset] {
				a, b := fs.PositionFor(c.Pos(), false).Offset, fs.PositionFor(c.End(), false).Offset
				if a >= 0 && b >= a && b <= len(text) {
					expr := text[a:b]
					if c.Type == nil {
						expr = "T" + expr
					}
					if _, err := parser.ParseExpr(expr); err == nil {
						literalBraces[fs.PositionFor(c.Lbrace, false).Offset] = true
					}
				}
			}
			return true
		})
	}
	enclosing := make([]int, len(ts))
	var braces []int
	for i, t := range ts {
		enclosing[i] = -1
		if len(braces) > 0 {
			enclosing[i] = braces[len(braces)-1]
		}
		if t.lit == "{" {
			braces = append(braces, t.a)
		}
		if t.lit == "}" && len(braces) > 0 {
			braces = braces[:len(braces)-1]
		}
	}
	var es []edit
	for i, t := range ts {
		if literalBraces[enclosing[i]] {
			continue
		}
		if t.lit != "^" && t.lit != "!" {
			continue
		}
		prev := i - 1
		for prev >= 0 && ts[prev].k == token.COMMENT {
			prev--
		}
		start := prev < 0 || (ts[prev].k == token.SEMICOLON && ts[prev].lit == "\n") || ts[prev].lit == "{" || ts[prev].lit == ":" || ts[prev].lit == "}"
		if !start {
			continue
		}
		end := statementEnd(ts, i+1)
		if t.lit == "^" && i+1 < len(ts) && (ts[i+1].line > t.line || ts[i+1].k == token.COMMENT) {
			end = i + 1
		}
		b := len(text)
		if end < len(ts) {
			b = ts[end].a
		}
		if t.lit == "^" && end == i+1 {
			b = t.b
		}
		args := strings.TrimSpace(text[t.b:b])
		kind := "ok"
		if t.lit == "!" {
			kind = "fail"
			if args == "" {
				return "", fmt.Errorf("line %d: ! requires an error expression", t.line)
			}
		}
		es = append(es, edit{t.a, b, "return " + prefix + kind + "(" + args + ")"})
	}
	rewrites := &source{text: text}
	for i := len(es) - 1; i >= 0; i-- {
		e := es[i]
		kind := "ok"
		if text[e.a] == '!' {
			kind = "fail"
		}
		args := strings.TrimSpace(rewrites.render(e.a+1, e.b))
		rewrites.put(e.a, e.b, "return "+prefix+kind+"("+args+")")
	}
	return rewrites.render(0, len(text)), nil
}
func lowerBody(s *source, body *ast.BlockStmt, ft *ast.FuncType, z *zeros, prefix string, errp *error) {
	if body == nil {
		return
	}
	rewrite := func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			lowerBody(s, lit.Body, lit.Type, z, prefix, errp)
			return false
		}
		r, ok := n.(*ast.ReturnStmt)
		if !ok || len(r.Results) != 1 {
			return true
		}
		call, ok := r.Results[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || !strings.HasPrefix(id.Name, prefix) {
			return true
		}
		kind := strings.TrimPrefix(id.Name, prefix)
		if !fallible(ft) {
			*errp = fmt.Errorf("%s is only valid in a fallible function at line %d (%s)", kind, s.fs.PositionFor(n.Pos(), false).Line, s.raw(ft))
			return true
		}
		var values []string
		switch kind {
		case "ok":
			for _, a := range call.Args {
				values = append(values, s.node(a))
			}
			if len(values) != len(results(ft))-1 {
				*errp = fmt.Errorf("^ has %d values, expected %d", len(values), len(results(ft))-1)
				return true
			}
			values = append(values, "nil")
		case "fail", "try", "wrap":
			zs, ok := z.list(ft)
			if !ok {
				*errp = fmt.Errorf("no canonical zero value for function results")
				return true
			}
			if kind == "fail" && (len(zs) == 0 || len(call.Args) != 1) {
				*errp = fmt.Errorf("! requires at least two function results and one error expression")
				return true
			}
			values = append(values, zs...)
			switch kind {
			case "try":
				values = append(values, "err")
			case "fail":
				values = append(values, s.node(call.Args[0]))
			case "wrap":
				var args []string
				for _, a := range call.Args {
					args = append(args, s.node(a))
				}
				qualifier := s.errorQualifier
				if qualifier == "" {
					qualifier = "fmt"
				}
				values = append(values, qualifier+".Errorf("+strings.Join(args, ", ")+")")
			}
		default:
			return true
		}
		s.replace(r, "return "+strings.Join(values, ", "))
		return true
	}
	var nodes []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			lowerBody(s, lit.Body, lit.Type, z, prefix, errp)
			return false
		}
		if _, ok := n.(*ast.ReturnStmt); ok {
			nodes = append(nodes, n)
		}
		return true
	})
	for i := len(nodes) - 1; i >= 0; i-- {
		rewrite(nodes[i])
	}
}
func lineDirectives(goText, smolText, name string) string {
	// Match stable lexical tokens in order; generated plumbing inherits the nearest
	// preceding Smol position. Directives are emitted only at physical line starts.
	st := significant(lex(smolText))
	gt := lex(goText)
	lineMap := map[int]int{}
	cursor := 0
	last := 1
	for _, t := range gt {
		for j := cursor; j < len(st) && j < cursor+32; j++ {
			if st[j].lit == t.lit {
				last = st[j].line
				cursor = j + 1
				break
			}
		}
		if _, ok := lineMap[t.line]; !ok {
			lineMap[t.line] = last
		}
	}
	var out strings.Builder
	for i, l := range strings.SplitAfter(goText, "\n") {
		if l == "" {
			continue
		}
		if n, ok := lineMap[i+1]; ok && strings.TrimSpace(l) != "" {
			out.WriteString("//line " + name + ":" + strconv.Itoa(n) + "\n")
		}
		out.WriteString(l)
	}
	return out.String()
}
func Format(text string, o Options) (string, error) {
	g, e := Lower(text, o)
	if e != nil {
		return "", e
	}
	return Lift(g, o)
}
