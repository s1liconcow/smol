#!/usr/bin/env python3
"""Design-basis measurement for SPEC_ENC.md. No model requests.

Re-encodes the patches that the Go control arms of the two recorded pilots
actually produced, using a prototype of the SPEC_ENC patch format, checks that
each one decodes to the same Go, and counts o200k_base tokens per format.

    uv run --with tiktoken python scripts/measure_enc_design.py

This is a prototype of the format, not the conforming tool: imports are
stripped from both sides rather than inferred, blank lines are ignored, and
bracket matching uses a small scanner instead of go/scanner.
"""
import difflib
import glob
import json
import os
import re
import subprocess
import sys

import tiktoken

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ENC = tiktoken.get_encoding("o200k_base")
MARKS = ("@@", "==", "++", "^^", "--", "..")


def ntok(s):
    return len(ENC.encode(s))


def gofmt(src):
    p = subprocess.run(["gofmt"], input=src, capture_output=True, text=True)
    if p.returncode:
        raise ValueError("gofmt: " + p.stderr[:300])
    return p.stdout


IMPORT_RE = re.compile(r'^import\s*(\((?:[^)]*)\)|[^\n(]*)\n?', re.M)


def strip_imports(src):
    return IMPORT_RE.sub("", src)


def key(line):
    return re.sub(r"\s+", "", line)


def scan(lines):
    """Per line: (lowest depth reached, depth at end, starts inside a literal)."""
    out, depth, state = [], 0, None  # state: None | 'raw' | 'block'
    for line in lines:
        inside, low, i, n = state is not None, depth, 0, len(line)
        while i < n:
            c = line[i]
            if state == "raw":
                j = line.find("`", i)
                if j < 0:
                    break
                state, i = None, j + 1
            elif state == "block":
                j = line.find("*/", i)
                if j < 0:
                    break
                state, i = None, j + 2
            elif c == "`":
                state, i = "raw", i + 1
            elif line.startswith("/*", i):
                state, i = "block", i + 2
            elif line.startswith("//", i):
                break
            elif c in "\"'":
                i += 1
                while i < n and line[i] != c:
                    i += 2 if line[i] == "\\" else 1
                i += 1
            else:
                if c in "([{":
                    depth += 1
                elif c in ")]}":
                    depth -= 1
                    low = min(low, depth)
                i += 1
        out.append((low, depth, inside))
    return out


class File:
    def __init__(self, text):
        self.lines = [l for l in text.split("\n") if l.strip()]
        self.keys = [key(l) for l in self.lines]
        self.info = scan(self.lines)
        self.n = len(self.lines)

    def span_end(self, i):
        low, end, _ = self.info[i]
        if end == low:
            return i
        for j in range(i + 1, self.n):
            if self.info[j][1] <= low and not (j + 1 < self.n and self.info[j + 1][2]):
                return j
        return self.n - 1

    def encloser(self, i):
        for k in range(i - 1, -1, -1):
            if self.span_end(k) >= i and self.span_end(k) > k:
                return k
        return None

    def match(self, text, lo, hi):
        k = key(text)
        return [i for i in range(lo, hi) if not self.info[i][2] and self.keys[i].startswith(k)]

    def resolve(self, anchor):
        parts = [p.strip() for p in anchor.split(" @ ")]
        lo, hi = 0, self.n
        for depth, part in enumerate(parts):
            m = self.match(part, lo, hi)
            if len(m) != 1:
                raise ValueError("anchor %r matches %d lines" % (part, len(m)))
            i = m[0]
            if depth < len(parts) - 1:
                if self.span_end(i) == i:
                    raise ValueError("scope %r opens no block" % part)
                lo, hi = i + 1, self.span_end(i) + 1
        return i

    def resolve_end(self, a, anchor):
        k = self.encloser(a)
        hi = self.span_end(k) + 1 if k is not None else self.n
        m = self.match(anchor, a + 1, hi)
        if len(m) != 1:
            raise ValueError("range end %r matches %d lines" % (anchor, len(m)))
        return m[0]


def tidy(line):
    """Trim and collapse alignment padding outside literals and comments."""
    line, out, i, n = line.strip(), [], 0, len(line.strip())
    while i < n:
        c = line[i]
        if c in "\"'`":
            j = i + 1
            while j < n and line[j] != c:
                j += 2 if (line[j] == "\\" and c != "`") else 1
            out.append(line[i:j + 1])
            i = j + 1
        elif line.startswith("//", i):
            out.append(line[i:])
            break
        elif c in " \t":
            j = i
            while j < n and line[j] in " \t":
                j += 1
            out.append(" ")
            i = j
        else:
            out.append(c)
            i += 1
    return "".join(out)


def prefixes(text):
    acc = ""
    for piece in re.findall(r"\w+|\s+|[^\w\s]", text):
        acc += piece
        if piece.strip() and " @" not in acc:
            yield acc


class Encoder:
    def __init__(self, f, short):
        self.f, self.short = f, short

    def _pick(self, i, lo, hi):
        if self.f.info[i][2]:
            return None
        full = tidy(self.f.lines[i])
        if " @ " in full:
            full = full.split(" @ ")[0]
        cands = prefixes(full) if self.short else [full]
        for c in cands:
            if self.f.match(c, lo, hi) == [i]:
                return c
        return None

    def anchor(self, i):
        f = self.f
        a = self._pick(i, 0, f.n)
        if a:
            return a
        k = f.encloser(i)
        while k is not None:
            s = self._pick(k, 0, f.n)
            if s:
                a = self._pick(i, k + 1, f.span_end(k) + 1)
                if a:
                    return s + " @ " + a
            k = f.encloser(k)
        return None

    def end(self, a, b):
        f = self.f
        if f.span_end(a) == b and b > a:
            return ".."
        k = f.encloser(a)
        hi = f.span_end(k) + 1 if k is not None else f.n
        if f.info[b][2]:
            return None
        full = tidy(f.lines[b])
        for c in (prefixes(full) if self.short else [full]):
            if f.match(c, a + 1, hi) == [b]:
                return ".. " + c
        return None

    def target(self, mark, i1, i2):
        """Header lines addressing base lines i1..i2-1, or None."""
        a = self.anchor(i1)
        if a is None:
            return None
        head = [mark + " " + a]
        if i2 - i1 > 1:
            e = self.end(i1, i2 - 1)
            if e is None:
                return None
            head.append(e)
        return head

    def insert(self, i):
        f, opts = self.f, []
        if i > 0:
            a = self.anchor(i - 1)
            if a:
                opts.append(["++ " + a])
            for k in range(i - 2, -1, -1):
                if f.span_end(k) == i - 1:
                    a = self.anchor(k)
                    if a:
                        opts.append(["++ " + a, ".."])
        if i < f.n:
            a = self.anchor(i)
            if a:
                opts.append(["^^ " + a])
        return min(opts, key=lambda h: ntok("\n".join(h))) if opts else None


def esc(line):
    if line.startswith("\\") or line[:2] in MARKS and (len(line) == 2 or line[2] == " "):
        return "\\" + line
    return line


def balanced(lines):
    info = scan(lines)
    return info[-1][1] == 0 and min(low for low, _, _ in info) == 0


def slide(f, t, i, j1, j2):
    """Move an insertion to an equivalent position whose text is bracket-balanced,
    as an author would write it, instead of the diff's arbitrary alignment."""
    if balanced(t.lines[j1:j2]):
        return j1, j2, i
    k = 1
    while i - k >= 0 and j1 - k >= 0 and f.keys[i - k] == t.keys[j2 - k]:
        if balanced(t.lines[j1 - k:j2 - k]):
            return j1 - k, j2 - k, i - k
        k += 1
    k = 1
    while i + k <= f.n and j2 + k <= t.n and f.keys[i + k - 1] == t.keys[j1 + k - 1]:
        if balanced(t.lines[j1 + k:j2 + k]):
            return j1 + k, j2 + k, i + k
        k += 1
    return j1, j2, i


def encode_file(path, base, tgt, dedent, short):
    """base/tgt: gofmt'd text or None. Returns patch text."""
    body = (lambda ls: [esc(tidy(l)) for l in ls]) if dedent else (lambda ls: [esc(l) for l in ls])
    out = ["@@ " + path]
    if base is None:
        return "\n".join(out + body([l for l in tgt.split("\n") if l.strip()])) + "\n"
    f, t = File(base), File(tgt)
    enc = Encoder(f, short)
    sm = difflib.SequenceMatcher(None, f.keys, t.keys, autojunk=False)
    for tag, i1, i2, j1, j2 in sm.get_opcodes():
        if tag == "equal":
            continue
        if i1 == i2:
            j1, j2, i1 = slide(f, t, i1, j1, j2)
            i2 = i1
        new = t.lines[j1:j2]
        while True:
            if i1 == i2:
                head = enc.insert(i1)
            else:
                head = enc.target("==" if new else "--", i1, i2)
            if head:
                break
            # Fall back to restating a neighbouring base line.
            if i1 > 0:
                i1 -= 1
                new = [f.lines[i1]] + new
            elif i2 < f.n:
                new = new + [f.lines[i2]]
                i2 += 1
            else:
                raise ValueError("no anchor in " + path)
        out += head + body(new)
    return "\n".join(out) + "\n" if len(out) > 1 else ""


def decode(patch, bases):
    """bases: path -> gofmt'd text. Returns path -> text (not yet gofmt'd)."""
    files, cur, ops = {}, None, {}
    for raw in patch.split("\n")[:-1]:
        m = raw[:2] if raw[:2] in MARKS and (len(raw) == 2 or raw[2] == " ") else None
        arg = raw[3:].strip() if m else None
        if m == "@@":
            cur = arg
            ops.setdefault(cur, [])
            if cur not in bases:
                ops[cur].append(["new", None, None, []])
        elif m == "..":
            ops[cur][-1][2] = arg or True
        elif m:
            ops[cur].append([m, arg, None, []])
        else:
            ops[cur][-1][3].append(raw[1:] if raw.startswith("\\") else raw)
    for path, lst in ops.items():
        if path not in bases:
            files[path] = "\n".join(lst[0][3]) + "\n"
            continue
        f, edits = File(bases[path]), []
        for m, anchor, end, body in lst:
            a = f.resolve(anchor)
            b = a if end is None else f.span_end(a) if end is True else f.resolve_end(a, end)
            if m == "==":
                assert body, "empty replacement"
                edits.append((a, b + 1, body))
            elif m == "--":
                assert not body, "delete with body"
                edits.append((a, b + 1, []))
            elif m == "++":
                edits.append((b + 1, b + 1, body))
            elif m == "^^":
                edits.append((a, a, body))
        edits.sort(key=lambda e: (e[0], e[1]))
        for (s1, e1, _), (s2, e2, _) in zip(edits, edits[1:]):
            assert e1 <= s2 and not (s1 == s2 and e1 != s1), "overlapping edits"
        lines = list(f.lines)
        for s, e, body in reversed(edits):
            lines[s:e] = body
        files[path] = "\n".join(lines) + "\n"
    return files


def noblank(s):
    return "\n".join(l for l in s.split("\n") if l.strip())


ERR_FOLD = re.compile(
    r"^(?:(?P<lhs>[\w., ]+), )?err (?P<op>:?=) (?P<e>.+)\nif err != nil \{\nreturn (?:(?:nil|0|\"\"|false), )*err\n\}$", re.M)
ERR_FOLD2 = re.compile(r"^if err := (?P<e>.+); err != nil \{\nreturn (?:(?:nil|0|\"\"|false), )*err\n\}$", re.M)


def fold_estimate(patch):
    patch = ERR_FOLD.sub(lambda m: (m["lhs"] + " " + m["op"] + " " if m["lhs"] else "") + m["e"] + "?", patch)
    return ERR_FOLD2.sub(lambda m: m["e"] + "?", patch)


def episode(result_path):
    run = os.path.dirname(result_path)
    study = run.split(os.sep)[-4]
    result = json.load(open(result_path))
    attempt = result["attempts"][-1]
    adir = os.path.join(run, "attempt-%d" % attempt["attempt"])
    response = open(os.path.join(adir, "response.json")).read()
    edits = json.loads(response)["edits"]
    srcs = json.load(open(os.path.join(ROOT, "measurements", study, "tasks", result["task"], "sources.json")))["go"]
    cand = dict(srcs)
    for e in edits:
        if e["old"] == "":
            cand[e["path"]] = e["new"]
        else:
            assert cand[e["path"]].count(e["old"]) == 1
            cand[e["path"]] = cand[e["path"]].replace(e["old"], e["new"])
    changed = [p for p in cand if cand[p] != srcs.get(p)]

    sr = "".join("%s\n<<<<<<< SEARCH\n%s\n=======\n%s\n>>>>>>> REPLACE\n" % (e["path"], e["old"], e["new"]) for e in edits)
    ud = "".join("".join(difflib.unified_diff(
        srcs.get(p, "").splitlines(True), cand[p].splitlines(True), "a/" + p, "b/" + p)) for p in changed)

    row = {
        "study": study, "task": result["task"], "repeat": result["repeat"], "resolved": result["resolved"],
        "output_tokens": attempt["usage"]["output_tokens"],
        "reasoning_output_tokens": attempt["usage"].get("reasoning_output_tokens", 0),
        "edits": len(edits), "files_changed": len(changed),
        "tokens": {
            "json_recorded": ntok(response),
            "search_replace_text": ntok(sr),
            "unified_diff": ntok(ud),
            "old_strings": sum(ntok(e["old"]) for e in edits),
            "new_strings": sum(ntok(e["new"]) for e in edits),
        },
    }
    assert row["tokens"]["json_recorded"] == attempt["response_tokens"]

    variants = {
        "enc_indented": (False, False, False),
        "enc_layout": (True, False, False),
        "enc_layout_imports": (True, True, False),
        "enc_layout_imports_short": (True, True, True),
    }
    for name, (dedent, noimp, short) in variants.items():
        prep = (lambda s: gofmt(strip_imports(s))) if noimp else gofmt
        bases = {p: prep(srcs[p]) for p in changed if p in srcs}
        tgts = {p: prep(cand[p]) for p in changed}
        patch = "".join(encode_file(p, bases.get(p), tgts[p], dedent, short) for p in changed)
        got = decode(patch, bases)
        for p in changed:
            if noblank(gofmt(got[p])) != noblank(tgts[p]):
                raise AssertionError("round trip failed: %s %s %s" % (result_path, name, p))
        row["tokens"][name] = ntok(patch)
        if name == "enc_layout_imports":
            row["patch"] = patch
            row["tokens"]["enc_layout_imports_errfold_estimate"] = ntok(fold_estimate(patch))
            heads = [l for l in patch.split("\n") if l[:2] in MARKS and l[2:3] in ("", " ")]
            row["tokens"]["enc_headers_part"] = ntok("\n".join(heads) + "\n")
    return row


def main():
    rows = []
    for study in ("agent-overhead", "authoring-modes"):
        for path in sorted(glob.glob(os.path.join(ROOT, "measurements", study, "runs", "*", "r?-go", "result.json"))):
            rows.append(episode(path))
    keys = list(rows[0]["tokens"])
    total = {k: sum(r["tokens"][k] for r in rows) for k in keys}
    base = total["json_recorded"]
    report = {
        "tokenizer": "o200k_base", "tiktoken": tiktoken.__version__,
        "episodes": len(rows), "round_trips_verified": len(rows) * 4,
        "total_output_tokens": sum(r["output_tokens"] for r in rows),
        "total_reasoning_output_tokens": sum(r["reasoning_output_tokens"] for r in rows),
        "totals": total,
        "reduction_vs_json_recorded": {k: round(1 - total[k] / base, 4) for k in keys},
        "rows": rows,
    }
    out = os.path.join(ROOT, "measurements", "enc-design")
    os.makedirs(out, exist_ok=True)
    with open(os.path.join(out, "report.json"), "w") as fh:
        json.dump(report, fh, indent=2)
        fh.write("\n")
    for k in keys:
        print("%-40s %6d  %6.1f%%" % (k, total[k], 100 * (1 - total[k] / base)))
    print("episodes", len(rows), "output", report["total_output_tokens"], "reasoning", report["total_reasoning_output_tokens"])
    for r in rows:
        t = r["tokens"]
        print(r["task"], r["repeat"], t["json_recorded"], t["search_replace_text"], t["unified_diff"], t["enc_indented"], t["enc_layout"], t["enc_layout_imports"], t["enc_layout_imports_short"])


if __name__ == "__main__":
    sys.exit(main())
