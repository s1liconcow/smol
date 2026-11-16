# Smol Output Encoding Specification

Version 0.1 (draft)

[SPEC.md](SPEC.md) shrinks the Go a model reads. This document shrinks the Go a model writes. The model reads ordinary Go and replies with an **encoded patch**. A deterministic tool, the **decoder**, turns the patch into ordinary, gofmt-formatted Go in the working tree. People and the Go toolchain only ever see Go.

The encoding has no semantics of its own. A patch means exactly the Go files that decoding it produces.

**Status.** The encoder and decoder are implemented in `smol`; tool validation is recorded in [ENC_VALIDATION.md](ENC_VALIDATION.md). The design measurements in §2 come from re-encoding patches a model already wrote in Go; they are not encoded-model results. Whether a model writes the format as accurately as it writes Go is the separate acceptance gate in §10.2, which has not been run.

## 1. Goals and non-goals

Goals:

1. **Fewer output tokens.** Minimise the visible tokens a model emits to make a change.
2. **Ordinary Go in, ordinary Go out.** The model is shown unmodified Go. The decoded result is the Go a careful programmer would have typed.
3. **Exact recovery.** Decoding is a pure function of the base files and the patch. It never guesses.
4. **Plain Go is always valid.** Every piece of new code can be written as ordinary Go. No shorthand is ever required.
5. **Small instructions.** The model is taught the format in about 150 tokens (§7).

Non-goals:

- Reducing reasoning tokens. They are 38% of the output measured in §2 and no output format reaches them.
- Reducing input tokens. That is SPEC.md. The two are independent: the input may be Go, the Smol code channel, or an outline.
- Fuzzy or model-assisted patch application. A patch that is not exact is rejected (§3).
- Languages other than Go. §4 is language-neutral apart from bracket matching; §5 and §6 are Go-specific.

## 2. Design basis

All counts are `o200k_base` tokens (tiktoken 0.14.0). The sample is every Go control episode of the two recorded pilots: 14 episodes over 6 SWE-bench Multilingual tasks, `gpt-6.1-sol`. Each response was a JSON list of `{path, old, new}` edits. `scripts/measure_enc_design.py` re-encodes each recorded patch with a prototype of §4 and §5, decodes it, and checks that the result is the same Go as the model's own patch produced. All 56 round trips (14 episodes, 4 variants) pass. [Report](measurements/enc-design/report.json).

### 2.1 Where output tokens go

| Output of the 14 episodes | Tokens | Share |
|---|---:|---:|
| All output, as billed | 4,937 | |
| Reasoning | 1,879 | 38.1% |
| Visible response (JSON edits) | 2,890 | 58.5% |
| — of which the `new` strings | 1,574 | |
| — of which the `old` strings | 519 | |
| — of which JSON structure and escaping | 797 | |

Only 1,574 of the 2,890 visible tokens are new text, and part of that restates the old string so the new code has somewhere to sit.

### 2.2 Measured formats

The same 14 changes in each format:

| Format | Tokens | Reduction |
|---|---:|---:|
| Recorded JSON `old`/`new` edits | 2,890 | |
| Unified diff, 3 lines of context | 3,441 | −19.1% |
| The same edits as plain-text search/replace blocks | 2,427 | 16.0% |
| Anchored operations (§4), new lines as the model wrote them | 1,669 | 42.2% |
| + no indentation or alignment (§5.1) | 1,627 | 43.7% |
| + no import declarations (§5.2) | 1,591 | **44.9%** |
| + every anchor cut to its shortest unique prefix | 1,431 | 50.5% |

Profile E1 (§6) is the 1,591 row. Anchor lines are 466 of its tokens. The last row is a lower bound: it needs the writer to know exactly how little of a line is unique.

An estimate, not a decoded result: folding `if err != nil` returns into `?` (profile E2) takes 1,591 to 1,547 (46.5%).

### 2.3 What costs tokens

| Finding | Consequence |
|---|---|
| The model restates code it is not changing: 519 tokens of `old` strings, and again inside `new`. | Address existing code by the start of one line (§4.2). |
| JSON escaping costs 15.7% on top of an encoded patch (1,591 → 1,841). | The patch is plain text, not a JSON value (§8.3). |
| `==`, `++`, `--`, `^^`, `..`, `@@` are one token each at the start of a line. `@=`, `@+` and every other `@x` pair are two. | Doubled-character markers (§4.1). |
| `}\n\n` is one token, the same as `}\n`. | Blank lines are free. The format does not remove them. |
| One level of indentation merges with the next word (`\treturn`). Deeper levels cost a token per line. | Indentation is dropped, but it is worth only 1.5 points on this sample. |
| Imports were 36 tokens across the sample. | Dropped because the tool can always recover them, not because they are large. |

### 2.4 Measured and rejected

| Idea | Measurement | Why not |
|---|---|---|
| Unified diff | +19.1% tokens | Context lines and hunk headers restate more than `old`/`new` does. |
| Line numbers as addresses | Numbering `encoding/json` adds 10,384 input tokens (31.6%) | The model would no longer read ordinary code, and every read pays for it. |
| Removing blank lines | 0 tokens | They merge with the preceding line break. |
| Lazy elision (`// ... existing code ...`) | Not measured | Applying it needs a model or a heuristic. That breaks R2 and R3. |
| SPEC.md scope elision and implied commas in output (§6.10–§6.13) | All four parse failures in the first pilot were omitted commas | The writer must prove a non-local fact for each use. Excluded from every profile (§6). |

### 2.5 Limits of these numbers

1. They come from a canonical encoder, which writes the smallest edit. A model may replace more than it needs to.
2. The patches are small and localized: 55 to 455 tokens each, 1 to 4 edits. Four of the six tasks are in one repository.
3. In this harness output is 1.7% of all tokens (4,937 of 285,004). At the list prices in [prices.json](measurements/finetuning-roi/prices.json), $2 per million input and $10 per million output, E1 removes 26.3% of output tokens and 2.1% of request cost, before the instruction text is paid for. The share grows with how much a task writes.
4. The instruction text costs 149 input tokens per request. At uncached input prices that is repaid once a patch saves 30 output tokens. Two of the 14 episodes saved fewer.

## 3. Recovery contract

`dec(base, patch)` takes a tree of Go files and a patch and returns a new tree or an error. Conforming tools satisfy:

- **R1. Plain Go is a fixed point.** If every body line of a patch is ordinary Go, the decoded files contain exactly those lines at the addressed positions, after formatting (§5.4). No profile changes the meaning of valid Go.
- **R2. Determinism.** `dec` depends only on its two arguments, the module's `go.mod` and `smol.mod`, and the Go version. It uses no model, no similarity score and no heuristic.
- **R3. Exact or rejected.** If any anchor does not resolve to exactly one line, any spans overlap, any import cannot be resolved, or any resulting file does not parse, `dec` returns an error and writes nothing. It never chooses between candidates.
- **R4. Locality.** A base line outside every span of the patch is unchanged, except for gofmt realignment next to inserted lines and the import declaration (§5.2).
- **R5. Round trip.** For Go trees `a` and `b`, `dec(a, enc(a, b)) == N(b)`, where `enc` is the canonical encoder (§8.1) and `N` is gofmt plus the declaration spacing of §5.3. This output normalizer is separate from the input-channel normalizer in SPEC.md.

What follows from the contract, and what does not:

| Claim | Basis |
|---|---|
| The decoded program is the one the model wrote. The compiler sees ordinary Go, so there is no run-time cost. | R1, R2. Testable on a corpus with no model (§10.1). |
| A wrong address cannot silently edit the wrong place. | R3. |
| Decoding adds no meaningful latency. | One scan of each addressed file, plus gofmt. |
| The model solves tasks as often as it does when writing Go. | **Not a property of the format.** It holds for a model, profile and instruction text only after §10.2 passes. |

## 4. Patch format

A patch is UTF-8 text. Lines end in `\n`; `\r\n` is accepted. A first line and a last line that start with three backquotes are ignored. Anything else before the first header is an error.

### 4.1 Headers and bodies

A **header** is a line whose first two characters are one of the markers below, followed by a space or the end of the line. Every other line is a **body** line and belongs to the header above it.

| Header | Effect |
|---|---|
| `@@ path` | Select a file, relative to the module root. If it does not exist, the body that follows is its whole contents. |
| `== A` | Replace the span of `A` with the body. |
| `-- A` | Delete the span of `A`. |
| `++ A` | Insert the body after the span of `A`. |
| `^^ A` | Insert the body before line `A`. |
| `..` | Directly after `==`, `--` or `++`: extend the span to the whole block that line `A` opens (§4.3). |
| `.. B` | Directly after `==`, `--` or `++`: extend the span through line `B`. |
| `==` | With no anchor: replace the whole file with the body. |
| `--` | With no anchor: delete the file. |
| `>> path` | Rename the selected file. Later operations under the same `@@` still address its contents. |

Without a `..` line, the span of `A` is the one line `A`.

Rules:

1. `==`, `++` and `^^` need at least one body line. `--` and `>>` take none. A violation is an error, so a truncated reply cannot delete code.
2. A body directly under `@@` is allowed only when the file does not exist.
3. Blank lines at the start and end of a body are dropped. Blank lines inside it are kept.
4. A body line that begins with a marker and a space, or with `\`, is written with one extra leading `\`. Decoding removes one leading `\` from any body line. In gofmt-formatted Go this arises only inside raw strings and block comments.
5. Body lines are otherwise taken verbatim. Nothing in a body is interpreted except by the profile in force (§6).

### 4.2 Anchors

An anchor names one existing line by how it starts.

- **Matching.** Delete all white space from the anchor and from the line. The anchor matches the line if what remains of the anchor is a non-empty prefix of what remains of the line. Indentation, alignment and spacing therefore never matter.
- **Anchorable lines.** Any base line that is not blank and does not begin inside a raw string literal or a block comment.
- **Uniqueness.** An anchor must match exactly one anchorable line in its search region. Zero matches and two matches are both errors.

| Anchor | Search region |
|---|---|
| `A` | The whole file. |
| `S @ A` | `S` is resolved first and must open a block (§4.3). `A` is searched in the lines after `S`, inside that block. Scopes nest: `S @ T @ A`. |
| `B` in `.. B` | The lines after `A`, up to the end of the innermost block that contains `A`. |

` @ ` (space, at sign, space) is the separator. Go has no `@` token, so it can only collide with text inside a string or comment; an anchor is a prefix, so it can always stop before such text.

All anchors resolve against the files as they were before the patch. Operations do not see each other's effects, and their order within a file does not matter, except that bodies inserted at the same position are placed in patch order. Two spans that share a line, or an insertion strictly inside a span, are errors. A file may be selected by `@@` more than once.

### 4.3 Blocks and spans

Bracket depth counts `(`, `[`, `{` and their closers as Go lexemes, ignoring string, rune and comment text.

For a line `L`, let `m` be the lowest depth reached on `L` and `e` the depth at its end. If `e == m`, `L` opens no block and its block is `L` alone. Otherwise the block runs through the first later line that ends at depth `m` or less, and does not end inside a raw string or block comment.

| Line `A` | Block that `..` selects |
|---|---|
| `func (s *Server) Get(…) {` | The whole function. |
| `if x {` | The whole `if`, including every `else` branch. |
| `} else {` | That `else` branch through the closing brace. |
| `v := T{` or `f(a,` | Through the line that closes the literal or call. |
| `return x` | The line itself. |

`++ A` followed by `..` inserts after the block, so `++ func Foo(` then `..` adds a declaration after `Foo`. `++ A` alone inserts directly below line `A`, which for a line that opens a block is the first position inside it.

### 4.4 Attached comments

Comment lines directly above an anchored line, with no blank line between, are attached to it. `^^` inserts above them and `--` deletes them. `==` leaves them in place. To change an attached comment, anchor the comment line itself.

This keeps a doc comment with its declaration when another declaration is inserted before it.

### 4.5 Errors

An error names the patch line, the reason, and what would fix it:

```
patch:7: anchor "return nil, err" matches 3 lines in store/user.go
  41  func (s *Store) Find(ctx context.Context, id int64) (*User, error) {  @  return nil, err
  58  func (s *Store) List(ctx context.Context) ([]*User, error) {  @  return nil, err
  77  func (s *Store) Save(ctx context.Context, u *User) error {  @  return nil, err   (2 matches in this block)
```

For each candidate the tool prints the nearest enclosing scope that would make the anchor unique. The decoder never applies one of them itself.

## 5. Bodies

Body lines are Go. Three things a tool can always recover are left out.

### 5.1 Layout

Body lines carry no indentation and no alignment padding. Leading white space on a body line is ignored outside raw strings, so indenting is harmless, only wasteful. Spacing inside a line is free; the decoder formats (§5.4).

A line that continues a raw string literal or a block comment is kept byte for byte.

### 5.2 Imports

A patch does not edit import declarations, and a new file is written without one. After applying the operations, for each file the patch touched, the decoder:

1. Finds every package reference as SPEC.md §6.1 defines it: an identifier `q` in a selector `q.X` that no enclosing scope, the package scope or the universe scope declares.
2. Resolves each `q` that the file does not import, taking the first of these that gives exactly one path:
   1. an `import` line written in a body of the patch;
   2. the other files of the same package;
   3. the `smol.mod` table;
   4. the standard-library table of SPEC.md §6.1;
   5. the packages of the module and of its `go.mod` requirements that are named `q`.
3. Adds the resolved imports to the file's import declaration in gofmt order, standard library first.
4. Removes each import that is not blank, not a dot import and no longer referenced.

A qualifier that no step resolves is an error listing the candidate paths. The model can always settle it by writing the `import "path"` line in a body; such lines are moved into the file's import declaration. `import "C"` and its preamble are never inferred or removed.

### 5.3 Declaration spacing

Where a line ending a multi-line top-level declaration is directly followed by the start of another top-level declaration or its attached comment, and the patch wrote at least one of the two lines, the decoder inserts one blank line. The model does not have to manage blank lines around the functions it adds.

### 5.4 Formatting

After §5.2 and §5.3:

- If the base file is unchanged by gofmt, the file is new, or an explicit bare `==` replaces the whole file, the decoder formats the whole file with gofmt.
- Otherwise it indents each body line with one tab per level of bracket depth at that position, touches nothing else, and reports that the file was not formatted.

The result must parse. A syntax error is reported against the patch line that supplied the offending text, and the patch is rejected (R3).

### 5.5 What is never left out

Identifiers, literals, types, receivers, package qualifiers, commas, braces, `func`, `return`, and comments the model chooses to write. A body is complete Go apart from §5.1–§5.3.

## 6. Profiles

A profile fixes which body forms beyond Go the decoder accepts. The harness chooses it; the patch does not declare it.

| Profile | Body language | Instruction text | Status |
|---|---|---|---|
| E0 | Go, with indentation and imports written out. §4 only. | — | Fallback when §5 tooling is unavailable. |
| **E1** | Go, with §5. | §7.1, 149 tokens | Default. Measured in §2. |
| E2 | E1 plus the folds below. | §7.1 and §7.2, 195 tokens | Off until it passes §10.2 on its own. |

**E2 folds.** A body may use these SPEC.md forms. The decoder lowers them by SPEC.md's rules, with zero values from SPEC.md §7.

| Form | SPEC.md | Lowers to |
|---|---|---|
| `x := f()?`, `f()?`, `? "context"`, `? { … }` | §6.5 | The assignment with `err`, then `if err != nil { … }` |
| `^x`, `!e` | §6.6 | `return x, nil`, `return Z, e` |
| `for v in xs {` | §6.7 | `for _, v := range xs {` |
| `f() T!` | §6.3 | `(T, error)` results |
| `=> expr` | §6.8 | A body of one `return` |

**Admission rule.** A form may enter a profile only if both hold:

1. *Writer-local.* The writer can tell that the form is valid from the function it is writing. It need not know what else is in scope, in the file or in the package.
2. *Fail-closed.* The form is not valid Go, so it cannot be mistaken for Go, and a misuse is a decoding error rather than a different program.

This excludes SPEC.md §6.10–§6.13 (opened packages, bare subject members, `let` conventions, implied commas), receiver inheritance in §6.2, and tag directives in §6.9. Each needs a fact about the surrounding scope, or changes what a valid Go spelling means. They remain available for the code channel, where the lifter proves those facts mechanically.

The `?` forms are the reason for E2: the decoder writes the zero values, which the model otherwise has to get right for each result type.

## 7. Instructions for the model

The instruction text is part of this specification. An implementation gives the model this text and nothing else about the format. It belongs in the stable prefix of the prompt so that it is cached.

### 7.1 E1

149 tokens:

```
Reply with a patch only: no prose, no code fence.
@@ path — the file; for a new file its whole contents follow.
== A — replace line A with the lines that follow.
-- A — delete line A.
++ A — insert the lines that follow after line A.
^^ A — insert them before line A.
A line ".." after ==, -- or ++ extends A to the whole block that line A opens; ".. B" extends it through line B.
A and B are the start of an existing line, long enough to match no other line. "S @ A" looks for A only inside the block opened by line S.
Write the new lines as ordinary Go, without indentation and without import declarations.
```

When the patch is the argument of a tool call rather than the whole reply, the first line becomes the tool's description.

### 7.2 E2 addition

46 tokens, appended to §7.1:

```
In new lines you may also write: "x := f()?" for "x, err := f()" followed by returning zero values and err when err != nil; "f()?" likewise when f returns only an error.
```

Only the two commonest folds are taught. The decoder accepts the rest of the E2 table from a model that already knows them.

### 7.3 Rules for the text

1. **No longer than it is.** In the first pilot a 314-token syntax guide and a 976-token one each needed repair requests that cost more than they saved, while 34- and 78-token rules needed none. Text is added only after §10.2 shows a failure that it fixes.
2. **No examples by default.** The model sees no encoded patch before it writes one. If §10.2 shows that an example is needed, it is one patch of at most 60 tokens and it is counted.
3. **Every sentence permits; none requires a shorthand.** "Ordinary Go" is always a correct way to follow the instruction.
4. **Rename and whole-file operations are not taught.** A model that does not know `>>` deletes and recreates, which is correct and only costs tokens.

## 8. Tooling

### 8.1 Commands

| Command | Effect |
|---|---|
| `smol dec [-profile E1] [-n] [patch]` | Decode a patch from a file or standard input into the working tree. `-n` checks and prints the resulting diff without writing. |
| `smol enc [-profile E1] old new` | Print the canonical patch that turns tree `old` into tree `new`. `smol enc -git rev` encodes the working tree against a revision. |
| `smol enc -check [packages]` | Verify R1–R5 on a corpus (§10.1). |

`smol dec` resolves every operation in every file before writing any file, and replaces files atomically. On error it writes nothing and exits non-zero with the report of §4.5. Decoding is not idempotent: an insertion applied twice inserts twice, so a harness applies each patch once.

On success it prints one line per operation, giving the file, the resolved line range and the first line of the span:

```
context.go:471-473 ++ after func (c *Context) BindQuery(obj interface{}) error {
```

The harness returns these lines to the model as the result of the edit. They let the model, or a reviewer, see where each anchor landed without re-reading the file.

### 8.2 The canonical encoder

`enc(a, b)` exists so that the format can be tested and measured without a model, and to produce examples or training data from real commits. For each changed file it:

1. Diffs `a` and `N(b)` by line, comparing lines with white space removed.
2. Moves each pure insertion to the equivalent position where its text is bracket-balanced.
3. Chooses, for each hunk, the cheapest of `++ A`, `++ A` with `..`, `^^ A`, `== A` and `-- A` with the range forms, using whole-line anchors and the innermost scope that makes each unique.
4. When no anchor is unique, widens the hunk by one line and restates that line in the body.

The canonical patch is one valid patch among many. R5 is the only requirement on it; minimality is not.

### 8.3 Transport

The patch is plain text. In order of preference:

1. The whole reply, or a free-form tool input if the API has one.
2. One string argument of a tool call. JSON escaping adds 15.7% on the §2 sample.
3. Never a structured list of edits. That is the format §2 starts from.

### 8.4 Repairs

A rejected patch costs a second request, which in the first pilot erased the savings of the first. So:

1. The error report (§4.5) is the entire repair prompt, apart from the conversation already in context.
2. The harness may keep the operations that resolved and ask only for the rejected ones, provided nothing has been written yet. The final application is still atomic.
3. After two rejections the harness falls back to its ordinary edit format for that turn. Rejections, repairs and fallbacks are counted in every measurement.

### 8.5 Fallbacks the model always has

- Any anchor can be made unique by lengthening it or by adding a scope.
- Any change can be written as `==` with a block or a range and the full new text.
- Any file can be rewritten with a bare `==`.

Each costs tokens and none costs correctness.

## 9. Example

The recorded response of one Go control episode (gin-gonic/gin #1957, first pilot, repetition 1) was 451 tokens of JSON. The same change under E1 is 263 tokens:

```
@@ context.go
++ func (c *Context) BindQuery(obj interface{}) error {
..
func (c *Context) BindHeader(obj interface{}) error {
return c.MustBindWith(obj, binding.Header)
}
++ func (c *Context) ShouldBindQuery(obj interface{}) error {
..
func (c *Context) ShouldBindHeader(obj interface{}) error {
return c.ShouldBindWith(obj, binding.Header)
}
@@ binding/binding.go
++ Query = queryBinding{}
Header = headerBinding{}
@@ binding/header.go
package binding
type headerBinding struct{}
func (headerBinding) Name() string {
return "header"
}
func (headerBinding) Bind(req *http.Request, obj interface{}) error {
if err := mapHeader(obj, req.Header); err != nil {
return err
}
return validate(obj)
}
func mapHeader(ptr interface{}, headers map[string][]string) error {
return mappingByPtr(ptr, headerSource(headers), "header")
}
type headerSource map[string][]string
var _ setter = headerSource(nil)
func (headers headerSource) TrySet(value reflect.Value, field reflect.StructField, tagValue string, opt setOptions) (bool, error) {
return setByForm(value, field, headers, http.CanonicalHeaderKey(tagValue), opt)
}
```

Decoding it:

- `context.go`: each new method goes after the whole function its anchor starts, with a blank line on each side (§5.3).
- `binding/binding.go`: `Header` goes below `Query` in the `var` block, and gofmt realigns the block.
- `binding/header.go`: the file is new. The decoder adds `import ("net/http"; "reflect")` from the standard-library table, separates the declarations and formats the file.

Under E2 the three-line `if err := mapHeader(…)` becomes `mapHeader(obj, req.Header)?`, an estimated 252 tokens.

## 10. Conformance and acceptance

### 10.1 The tool

An implementation conforms if:

1. **Corpus round trip (R5).** For every pair of consecutive revisions in a corpus of at least 1,000 commits from at least 10 Go modules, including this one, `dec(a, enc(a, b)) == N(b)`.
2. **Plain Go (R1).** The same holds when `enc` is run with profile E0 and the patch is decoded with E1 and with E2.
3. **Rejection (R3).** For each rule of §4 and §5 there is a test with a patch that breaks it, checking that nothing is written: zero and double anchor matches, a scope that opens no block, an out-of-region range end, overlapping spans, a body where none is allowed and none where one is required, an unresolved import, a result that does not parse.
4. **Edge cases.** Tests cover a body line that starts with each marker, anchors next to raw strings and block comments, attached comments under `^^`, `--` and `==`, a base file that is not gofmt-clean, `import "C"`, and a build-constrained file.
5. **Mutation.** Each corpus patch is damaged one line at a time: a line deleted, duplicated or cut short. Every damaged patch must be rejected or decode to files that parse. The share that is accepted is reported, split by header and body lines. It measures how much a slip by the writer can pass unnoticed; this version sets no threshold.

### 10.2 The model

"No accuracy penalty" may be claimed for a model, a profile and an instruction text only when all of the following hold on tasks that were not used to write the instruction text:

1. **A control that works.** The same tasks are run with the harness's ordinary edit format, and that control resolves at least half of them. The follow-up pilot's control resolved 2 of 8, which made its tie uninformative.
2. **Paired non-inferiority.** At least 100 tasks from at least 10 repositories. The encoded arm resolves no fewer tasks than the control, within a margin fixed before the run.
3. **First-attempt decoding.** At least 99% of patches decode without a repair.
4. **Everything is counted.** Reasoning tokens, repairs, fallbacks, the instruction text and failed tasks are in the totals. The reported figure is cost per resolved task, with output tokens alongside.
5. **Reasoning is watched.** Mean reasoning tokens per task are reported for both arms. A format that saves visible tokens and spends them on reasoning has saved nothing.

Until then the supported claims are those of §3: the decoded program is exact, and a bad patch is rejected rather than misapplied.

## 11. Open questions

1. **Does a model write it?** Nothing here has been produced by a model. Anchors in particular ask for a judgement about uniqueness that `old` strings also ask for, but in fewer tokens and with less redundancy to catch a slip.
2. **How short should anchors be?** Whole-line anchors cost 466 of 1,591 tokens. Shortest-prefix anchors would save a further 160, but a model cannot see uniqueness as a tool can. A middle rule, such as "through the first parenthesis", has not been measured.
3. **Dense spacing.** Writing `x:=f(a,b)` saved 6% of code tokens on input (SPEC.md §2.2). It is valid under §5.1 today but is not taught, because whether a model writes it as reliably as spaced Go is unknown.
4. **Copy and move.** A body directive that pastes an existing span would make moves and wraps nearly free. It is left out until a task set with refactors exists to measure it on.
5. **Larger and agentic edits.** The sample is 14 small single-reply patches. Multi-file changes, edits to code the model wrote earlier in the session, and anchors against files that changed since they were read are untested.
6. **Other tokenizers.** The one-token markers of §4.1 were chosen for `o200k_base` and must be re-checked for each target model.
7. **Other languages.** §4 needs only a lexer that knows brackets, strings and comments. §5.1 and §5.2 need a formatter and an import resolver as dependable as gofmt and the Go package tables.
