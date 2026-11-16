# Draft specification decisions and limits

SPEC.md remains unchanged. Historical implementation choices are followed by
observed v0.2 defects from the 2026-11-15 audit. This implementation does not
conform to §12: the failing cases below are not authorized spec exceptions.
See [VALIDATION.md](VALIDATION.md) for the current evidence.

## DISC-001 — Significant comment indentation (accepted, 2026-11-15)

§5 removes all indentation, but §3 requires byte-exact gofmt output. gofmt uses
comment columns before `case` and closing delimiters to distinguish comments in
a body from comments on the following clause. For example, a comment in an
otherwise empty switch case must retain its indentation to survive another
gofmt pass. Also, removing indentation from an illustrative `//line` comment
activates it as a Go compiler directive and may make valid Go fail to parse.

In v0.2, notes record significant comment indentation; directives in code may
still retain a tab to avoid activating an illustrative directive. Code lines
remain unindented. Comment and literal contents remain verbatim. Covered by
`TestCorpusRegressions` and the host standard-library corpus.

## DISC-002 — Ambiguous expression bodies (accepted, 2026-11-15)

§6.8 does not delimit a literal's return expressions from surrounding syntax.
`func() int { return 1 }()` cannot become `func() int => 1()`: that moves the
invocation into the function body. Similarly, a literal returning a tuple from
one call cannot always distinguish an outer argument from another return value.

Lifting retains braces for immediately invoked/indexed/selected literals and
for tuple-returning literals followed by an outer comma when expression counts
would be ambiguous. Other expression bodies fold normally. Nested function
result types retain result-list parentheses where needed to bind the final `!`.
Covered by `TestCorpusRegressions`, `TestNestedReturnsAndOperators`, and
`TestForms/6.3_func_type`.

## DISC-003 — Import placement (accepted, 2026-11-15)

Omitting imports loses their original placement relative to detached file
comments. Normalization places merged, uncommented imports directly after the
package line (including its trailing comment), separated by a blank line.
Commented import declarations and cgo preambles remain separate. This makes the
normalization rule in §3 precise enough for implicit imports to round-trip.
Covered by `TestCorpusRegressions`.

## DISC-004 — Go version and build context (limitation, 2026-11-15)

The standard import table and export data come from the installed Go toolchain,
with ambiguous package names excluded, including versioned packages whose
package name differs from their final path component. Translation uses the host
build context for sibling declarations. Cross-platform and historical Go-version
conformance require running the corresponding toolchain/build environment.
The corpus command includes active package files and internal/external tests;
it excludes inactive platform files and deliberately invalid compiler testdata.

## DISC-005 — Source-position granularity (limitation, 2026-11-15)

`lower -lines` maps generated lines to nearby stable lexical tokens in the Smol
file; generated plumbing inherits a nearby source position. It provides file
and line provenance, not a token-exact source map for every generated token.
Covered by `TestLines` for direct statement locations.

## DISC-006 — Import marker collides with literals (INVESTIGATING, 2026-11-15)

Violates §3 L1 and literal preservation. `inferImports` searches all source text
for `_ "smol.invalid/inferred-imports"`, including raw string literals. It replaces
the literal content with inferred imports and fails to emit the import block.
This breaks `smol check ./...` on `internal/smol/imports.go` itself.
Audit case: `import_marker_literal_roundtrip`. Restricting replacement to the
actual synthetic import spec would avoid the collision; no fix was made in this audit.

## DISC-007 — Comment placeholders alter layout (INVESTIGATING, 2026-11-15)

Violates §3 L1. Fixed-width placeholders in `normalizePreservingComments` affect
gofmt's line-breaking and declaration alignment. Restoring the shorter original
comments does not restore the original normalized layout. A commented selector
in a one-line function and a comment in a parameter list both reproduce this.
Audit cases: `open_comment`, `convention_comment`.

## DISC-008 — EOF directives moved to notes (INVESTIGATING, 2026-11-15)

Violates §3 L4 and §4.1. `codeLexemes` classifies a pending comment as a directive
only when another code lexeme follows it. A trailing `//go:generate` is therefore
absent when the code channel is lowered without notes.
Audit case: `trailing_directive_without_notes`.

## DISC-009 — Imported embedded pointer field omitted (INVESTIGATING, 2026-11-15)

Violates §6.11's mandatory fold. `fieldNames` misses the embedded field name of
`*bytes.Buffer`; `t.Buffer` consequently stays qualified even when `Buffer` is
free. This preserves the Go source but fails canonical lifting requirements.
Audit case: `subject_embedded_pointer`.

## DISC-010 — Outline body removal and layout (INVESTIGATING, 2026-11-15)

Violates §9's description of code-channel output with every function body removed.
`Outline` removes only top-level FuncDecl bodies, leaving package-level FuncLit
bodies. Its `-docs` path calls `Unpack`, which restores blank lines around comments.
Audit cases: `outline_removes_literal_bodies`, `outline_docs_dense_layout`.

All audit cases, including the failing inputs and observed errors, are in
[probes.json](measurements/spec-v0.2-validation/probes.json) and can be rerun with
`go run ./scripts/validate_v02.go` (expected nonzero until the defects are fixed).
