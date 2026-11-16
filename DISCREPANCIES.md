# Specification decisions and validation limits

The original audit failures are fixed. Their original inputs and results remain
in [the historical audit](measurements/spec-v0.2-validation/probes.json).
Current evidence is in [VALIDATION.md](VALIDATION.md). No failing probe is
silently skipped or counted as passing.

## DISC-001 — Significant comment indentation (resolved, 2026-11-15)

Notes preserve comment indentation needed by Go's formatter. An indented
illustrative `//line` comment is not an active Go directive and now goes into
notes. Removing notes cannot activate it. Active directives and cgo preambles
remain in code. Tests: `TestInactiveLineDirective`, `TestCorpusRegressions`,
`TestDirectivesStayInCode`, `TestEOFDirectives`.

## DISC-002 — Ambiguous expression bodies (specified, 2026-11-15)

With the user's approval, §6.8 now requires braces when an arrow would absorb
surrounding syntax. This covers calls, indexing, selectors, assertions, binary
operators, and tuple returns followed by an ambiguous outer comma. Parentheses
around the literal provide an explicit boundary. The implementation checks AST
parents so intervening comments do not defeat the rule. Tests:
`TestArrowBoundaries`, `TestNestedReturnsAndOperators`, `TestCorpusRegressions`.

## DISC-003 — Import placement (normalization decision, 2026-11-15)

Normalization places merged, uncommented imports immediately after the package
clause, including its trailing comment. Commented declarations and cgo preambles
keep their separate declarations. This fixes the insertion point left implicit
by §3. Tests: `TestCorpusRegressions`, `TestCodeOnlyImportBoundaries`.

## DISC-004 — Go version and build context (validation limit, 2026-11-15)

Import tables and export data come from the installed Go toolchain. Validation
covers Go 1.26.3 on darwin/arm64 with its active build constraints. Use the target
Go toolchain and build context when translating for another version/platform;
archived tables for every historical Go release are not maintained. Inactive
platform sources and deliberately invalid compiler fixtures are outside the
recorded corpus. The report does not claim cross-version/platform conformance.

## DISC-005 — Source-position granularity (validation limit, 2026-11-15)

`lower -lines` maps generated lines to nearby stable lexical tokens in Smol.
Generated plumbing inherits a nearby position. This is file/line provenance,
not an exact map for every generated token. §4 exempts this output from L1.
Test: `TestLines` and the compiled `TestCLIWorkflow` fixture.

## DISC-006 — Import marker collision (resolved, 2026-11-15)

Lowering identifies a synthetic AST import spec with a collision-free alias.
Marker-like text in literals, comments, or real imports is left intact.
`smol check ./...` now passes. Tests: `TestImportMarkerIsolation` and
`import_marker_literal_roundtrip` in the independent probe runner.

## DISC-007 — Comment placeholders alter layout (resolved, 2026-11-15)

Single-line block comments retain their original width during printing; they
cannot change inline function layout or adjacent declaration alignment.
Comment barriers also protect function headers, error results and struct tags.
Tests: `TestCommentLayout`, `TestDeclarationCommentsAndTagKeys`,
`TestRequiredForms`, and the original `open_comment`/`convention_comment` probes.

## DISC-008 — EOF directives disappear (resolved, 2026-11-15)

Comments are classified when scanned, including after the final code lexeme.
Block-form line directives are retained too. Tests: `TestEOFDirectives`,
`TestCodeOnlyImportBoundaries`, and `trailing_directive_without_notes`.

## DISC-009 — Embedded pointer subject fields (resolved, 2026-11-15)

Member discovery unwraps embedded pointers and generic instantiations before
reading the field name. Imported embedded fields such as `*bytes.Buffer` now
use mandatory subject elision. Test: `TestEmbeddedPointerSubject` and the
`subject_embedded_pointer` probe.

## DISC-010 — Outline bodies and layout (resolved, 2026-11-15)

Outlines remove both declaration and literal bodies. `-docs` keeps AST-attached
documentation, including field docs, in dense layout without restoring blank
lines, detached comments or trailing comments. Test: `TestOutline` and both
original outline probes. Fresh outline token counts are local measurements;
old API measurements remain archived for their original payloads.

## DISC-011 — Code-only import boundaries (resolved, 2026-11-15)

Lowering preserves explicit declaration boundaries and import order even when
ordinary comments and blank lines are absent. Only inferred imports and the
reconstructed partial block are sorted. Unused or wholly shadowed imports stay
explicit because no qualifier can reconstruct them. The checker now verifies
L4's Go tokens/directives in addition to L1–L3. Tests:
`TestCodeOnlyImportBoundaries` and the standard-library corpus.

## Behavioral test profile

The standard-library behavioral run uses `go test -short` with complete official
Go source fixtures, matching compiler tools, Go's own module compatibility
defaults, a canonical GOROOT path, and a separate build cache for that GOROOT. The report records skipped tests; it does
not claim execution of tests excluded by `-short`. Test notes remain present in
both modes so `// Output:` assertions are still executed. Production notes are
removed in the code-only mode. Installed GOROOT is never modified.
