# Validation coverage

The current target is **SPEC.md v0.2**, including the approved §6.8 clarification
that ambiguous expression boundaries keep braces. The reproducible, scoped
verdict is in [summary.json](measurements/spec-v0.2-conformance/summary.json).
[VALIDATION.md](VALIDATION.md) records results, environment and token uplift.

The original seven failing probes now pass. No expected outputs were regenerated
from the translator to hide failures; the §11 example fixtures remain unchanged.
Tests use Go's parser, scanner, formatter and compiled package behavior as oracles.

## Required form matrix (§12.3)

Each row has three independently named cases in `TestRequiredForms`: a mandatory
fold, the nearest non-match, and an interior comment. Each case checks round-trip
laws, expected/forbidden code fragments, and comment retention. The machine
report is generated from the actual Go test events by `scripts/report_conformance.py`.

| Form | Required cases | Tested | Passing |
|---|---:|---:|---:|
| §6.1 imports | 3 | 3 | 3 |
| §6.2 declarations/receivers | 3 | 3 | 3 |
| §6.3 error results | 3 | 3 | 3 |
| §6.4 conventional parameters | 3 | 3 | 3 |
| §6.5 handlers | 3 | 3 | 3 |
| §6.6 returns | 3 | 3 | 3 |
| §6.7 range | 3 | 3 | 3 |
| §6.8 expression bodies | 3 | 3 | 3 |
| §6.9 tags | 3 | 3 | 3 |
| §6.10 opened packages | 3 | 3 | 3 |
| §6.11 subjects | 3 | 3 | 3 |
| §6.12 file conventions | 3 | 3 | 3 |
| §6.13 commas | 3 | 3 | 3 |
| **Total** | **39** | **39** | **39** |

These are the cases required by §12.3, not an enumeration of every normative
clause or a claim of 100% input-space coverage. §6.14 summarizes the grammar of
the preceding forms and introduces no additional fold.

## Contract and regression coverage

| Requirement | Evidence |
|---|---|
| §3 L1–L4, §8 lifting order | `smol check` across active stdlib and module files; independent scanner probes; golden §11 example |
| §4 tools and package context | `TestCLIWorkflow`, `TestModuleImportTable`, `TestUnsupportedGoVersion`, `TestPackageScopeAndZeros`, `TestExternalTestDotImport`, `TestLines` |
| §4.1 notes | `TestNotes`, `TestPackStandardPackage`; exact restoration, changed-count relocation, missing/stale heads |
| §4.1 active directives and cgo | `TestDirectivesStayInCode`, `TestEOFDirectives`, `TestInactiveLineDirective`; independent code-only scanner probes and compiled standard packages |
| §5 dense layout and literals | `TestImpliedBreaks`, `TestUnicodeAndLiterals`, `TestCommentLayout`, `TestCorpusRegressions` |
| §6.1 import boundaries/collisions | `TestImportMarkerIsolation`, `TestCodeOnlyImportBoundaries`, `TestPartialImportBlock`; unused/shadowed imports and real sentinel-path imports |
| §6.2–§6.4 declaration comments | `TestDeclarationCommentsAndTagKeys`, `TestForms`, required-form matrix |
| §6.5–§6.7 handlers/returns/range | `TestForms`, `TestRoundTrips`, `TestNestedReturnsAndOperators`, required-form matrix |
| §6.8 boundary clarification | `TestArrowBoundaries`: invoked literal versus returned call, binary operators, outer parentheses, tuples and intervening comments |
| §6.9 tags | `TestStructTags`, `TestDeclarationCommentsAndTagKeys`, required-form matrix |
| §6.10–§6.13 scopes | `TestScopeForms`, independent threshold/tie/shadow/key probes, `TestEmbeddedPointerSubject`, required-form matrix |
| §7 zeros | `TestPackageScopeAndZeros`, `TestLocalResultTypes`, imported types, unavailable type parameters and invalid-form tests |
| §9 outlines | `TestOutline`: declaration/literal bodies, nested literals, doc/field comments, detached/trailing comments, dense layout |
| §10 measurements | Fresh bundles and local tokenizer counts, archived payload/prompt hashes and raw API usage verification |
| §11 complete example | `TestSpecExample`; Go/code/notes fixtures unchanged |
| §12 compiled behavior | Every selected standard package under `go test -short`, plus this module; overlays in both channel modes |

## Bounds of the claim

The report covers Go 1.26.3, darwin/arm64 and active GoFiles, CgoFiles, TestGoFiles
and XTestGoFiles. Inactive platforms, other Go releases and deliberately invalid
compiler fixtures are excluded. Behavioral test skips and flags are recorded;
`-short` does not exercise all long-running or external-network tests. Test notes
remain present so Go's Example assertions execute in both modes.

No complete enumeration of all malformed notes, edit permutations, valid Go
programs or valid handwritten Smol is claimed. See [DISCREPANCIES.md](DISCREPANCIES.md)
for toolchain/version and source-position limits. Historical audit results remain
in `measurements/spec-v0.2-validation/`; current results are in
`measurements/spec-v0.2-conformance/`. Fixture provenance is recorded in
[testdata/PROVENANCE.md](testdata/PROVENANCE.md) and the measurement manifests.
