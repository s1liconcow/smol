# Validation coverage

The current specification is **SPEC.md v0.2 (draft)**. The validation verdict is
**not conformant**: byte-preserving round trips fail on the module itself and on
targeted comment fixtures; code-only directive retention and mandatory forms also
have gaps. [VALIDATION.md](VALIDATION.md) gives the findings and token uplift.

Tests compare observable source and compiled behavior with Go's parser, scanner,
formatter and package tests. No implementation output was used to regenerate
the spec's expected example. The original 70 unit/CLI cases all pass, including
subtests; passing these tests alone does not establish conformance.

## Coverage matrix

| Specification | Existing coverage | New audit / remaining limits |
|---|---|---|
| §3 L1–L3 | `roundtrip`, `TestSpecExample`, `packRound`, `smol check` | 2,800 active stdlib files pass; original module 12 pass / 1 fail; two new inline-comment fixtures fail L1 |
| §3 L4 | `TestDirectivesStayInCode`, code-only example | Independent Go-scanner token/directive comparison passes ordinary directive/cgo fixture; trailing directive fails; three package test suites pass after production notes are removed |
| §4 CLI/files | `TestCLIWorkflow`, `TestModuleImportTable`, `TestUnsupportedGoVersion`, `TestLines` | Existing CLI behavior passes; Go-version tables still use installed toolchain (DISC-004) |
| §4.1 notes | `TestNotes`, `TestPackStandardPackage` | Exact restoration and changed-count relocation pass; full invalid-notes grammar and all edit permutations untested |
| §5 layout | `TestImpliedBreaks`, `TestUnicodeAndLiterals` | Dense inline/implied-break/contextual-identifier probes pass; `outline -docs` introduces blank lines |
| §6.1 imports | `TestForms/6.1_*`, `TestPartialImportBlock`, package/dot-import regressions | Partial-import positive and blank-line near miss pass; raw placeholder collision corrupts source |
| §6.2 declarations | Receiver inheritance, changed/commented receivers, generic funcs, literals/types | Covered in `TestForms`, `TestCorpusRegressions`, `TestRoundTrips` |
| §6.3 error results | Unnamed/named results, parenthesized error, nested types | Existing cases pass |
| §6.4 parameters | Conventional, typed, escaped, unnamed, commented, interface/literal forms | Existing cases pass; new inline-comment normalization failure also affects signatures |
| §6.5 handlers | Bare/formatted/custom, assignments, initializer, comment/blank/else near misses | Existing cases pass |
| §6.6 returns | Success/failure, nested literals, noncanonical zeros, comments, invalid usage | Existing cases pass |
| §6.7 range | Value/pair folds, index/assignment near misses, `in` identifier, comments | Existing cases pass |
| §6.8 expression bodies | Declaration/literal layout, tuples, comments | Existing cases pass; ambiguity exceptions remain in DISC-002 |
| §6.9 tags | Multiple keys, acronyms, explicit/suppressed tags, unexported/grouped fields, near miss | `TestStructTags` and round-trip cases pass |
| §6.10 opened packages | `TestScopeForms`: thresholds, shadowing, ambiguous exports | Fold, near miss, local scope, key exclusion pass; comment fixture fails L1 |
| §6.11 subjects | `TestScopeForms`: receiver/first parameter, closure shadow, keys | Direct member, shadow, comment, closure, promoted near miss pass; imported embedded pointer field misses fold |
| §6.12 conventions | `TestScopeForms`: let, escapes, call integration | Threshold, tie-breaking, inherited-receiver count pass; comment fixture fails L1 |
| §6.13 implied commas | Calls, cases, directives fixture | Fold, operator near miss, comment, case pass |
| §7 zeros | Builtin, package/imported named, composite/local, unavailable type parameters | Existing cases pass |
| §8 lifting order | Golden example, corpus, scope fallback in `Lift` | No exhaustive stage-order proof; fallback does not fix failures outside scope elision |
| §9 outlines | Regenerated benchmark outlines | Literal body retention and docs layout fail new probes |
| §10 measurements | Preserved raw Codex input usage | Fresh bundle/token/hash verification passes; no new API requests; no model-accuracy measurement |
| §11 example | `TestSpecExample` | Go, code, notes fixtures match spec verbatim |
| §12 conformance | Host stdlib and original-module corpus; selected lowered-package tests | Full standard-library test suite and exhaustive fold/near-miss/comment matrix not completed; known violations prevent conformance |

## Spec-derived probe accounting

These are audit-case counts, **not** counts of every MUST/SHOULD clause in the
spec. They must not be reported as an overall spec coverage percentage. Each
failed probe is a demonstrated unmet requirement; none is marked skipped or
accepted. [Machine-readable cases and reproducers](measurements/spec-v0.2-validation/probes.json).

| Section | Tested | Passing | Failing |
|---|---:|---:|---:|
| §3 L1 | 1 | 0 | 1 |
| §3 L4 | 3 | 2 | 1 |
| §4.1 | 2 | 2 | 0 |
| §5 | 4 | 4 | 0 |
| §6.1 | 2 | 2 | 0 |
| §6.10 | 5 | 4 | 1 |
| §6.11 | 6 | 5 | 1 |
| §6.12 | 5 | 4 | 1 |
| §6.13 | 4 | 4 | 0 |
| §9 | 2 | 0 | 2 |
| Total | 34 | 27 | 7 |

The stdlib corpus uses active GoFiles, CgoFiles, TestGoFiles and XTestGoFiles for
Go 1.26.3 on darwin/arm64. It excludes deliberately invalid compiler testdata,
inactive platforms, other experiments and historical Go releases. Behavioral
validation uses overlays of transformed `fmt`, `encoding/json`, and `go/parser`,
with `GODEBUG=asynctimerchan=0` and loopback access. Installed sources are untouched.
Test comments remain in both modes so Go's Example assertions execute.

Original evidence in `measurements/conformance/` and `measurements/tests/` is
historical. Current evidence is in `measurements/spec-v0.2-validation/`.
Fixture provenance is in [testdata/PROVENANCE.md](testdata/PROVENANCE.md).

Reproduction commands are in [VALIDATION.md](VALIDATION.md#reproduce).
