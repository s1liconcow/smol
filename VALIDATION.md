# SPEC v0.2 validation and measured uplift

Audited on 2026-11-15 against the workspace's `SPEC.md` v0.2 draft with
Go 1.26.3, darwin/arm64. **The token target is reproduced; the implementation
is not yet conformant.** Implementation files and SPEC.md were not changed by
this audit. Source hashes and machine-readable results are in
[measurements/spec-v0.2-validation](measurements/spec-v0.2-validation/).

## Correctness

| Check | Result | Evidence |
|---|---|---|
| Existing unit/CLI suite | 70 passing cases, including subtests; no failures/skips | [tests.jsonl](measurements/spec-v0.2-validation/tests.jsonl) |
| `go vet ./...` | Pass | [summary.json](measurements/spec-v0.2-validation/summary.json) |
| Standard-library L1–L3 check | 2,800 files pass | [stdlib report](measurements/spec-v0.2-validation/stdlib/report.json) |
| Original module L1–L3 check | 12 files pass, `internal/smol/imports.go` fails | [module report](measurements/spec-v0.2-validation/module/report.json) |
| New spec-derived probes | 27 pass, 7 fail | [probes.json](measurements/spec-v0.2-validation/probes.json) |
| Complete §11 example | All three fixtures match the spec verbatim; existing golden test passes | [fixtures](testdata/example/) |
| Fresh encoding/json bundle | Go, code, notes, and both outlines match preserved v0.2 artifacts byte for byte | [uplift.json](measurements/spec-v0.2-validation/uplift.json) |
| Lowered package behavior | `fmt`, `encoding/json`, `go/parser`: 857 passing cases in each mode, with both channels and with production notes removed | [lowered report](measurements/spec-v0.2-validation/lowered/report.json) |

The corpus includes active production, cgo, internal-test and external-test files
for this host. It does not cover inactive platforms, all Go releases or all
experiments. The complete standard-library test suite was not run. Behavioral
checks use a Go overlay; installed Go sources are unchanged. Test notes are
retained in the code-only production run because Go's example expectations live
in `// Output:` comments. Both modes therefore execute the same assertions.

### Failures

The full inputs and observed outputs are recorded in `probes.json`. These are
observed defects, not accepted spec exceptions.

1. **Import inference corrupts raw string literals and loses imports (L1).**
   `inferImports` searches the whole source for its marker with `strings.Index`.
   A literal containing `_ "smol.invalid/inferred-imports"` is mistaken for the
   placeholder import. This breaks the translator's own `imports.go`.
   Probe: `import_marker_literal_roundtrip`. [Implementation](internal/smol/imports.go).

   ```go
   package p
   import "fmt"
   const marker = `_ "smol.invalid/inferred-imports"`
   func F() { fmt.Println(marker) }
   ```

   Lowering changes the literal to `"fmt"` and omits the import declaration.
   The scope-elision fallback does not repair it.

2. **Inline comments can change normalized layout (L1).** A comment between
   a qualifier and its member expands a one-line function into multiple lines;
   a comment inside a parameter list changes alignment of adjacent declarations.
   The fixed-width placeholders in `normalizePreservingComments` influence
   gofmt before the original comment text is restored.
   Probes: `open_comment`, `convention_comment`.
   [Implementation](internal/smol/source.go).

3. **Trailing directives disappear from the code channel (L4, §4.1).**
   `//go:generate echo end` after the last declaration is moved entirely to notes.
   Pending comments are classified as directives only when another code lexeme
   follows them. Probe: `trailing_directive_without_notes`.
   Ordinary directives and a cgo preamble before declarations pass the independent
   Go-scanner comparison. [Implementation](internal/smol/pack.go).

4. **Embedded imported pointer fields miss mandatory subject elision (§6.11).**
   With `type T struct { *bytes.Buffer }`, `return t.Buffer` is kept qualified,
   although `Buffer` is a directly embedded field name. `fieldNames` recognizes
   an unwrapped selector, but not a selector under `*`.
   Probe: `subject_embedded_pointer`. [Implementation](internal/smol/scope.go).

5. **Outlines do not fully implement §9.** A package-level function literal
   keeps its body (`var F=func()int=>1`); only function declarations are stripped.
   With `-docs`, unpacking reintroduces blank lines rather than emitting dense
   code plus doc comments. Probes: `outline_removes_literal_bodies`,
   `outline_docs_dense_layout`. [Implementation](internal/smol/pack.go).

See [DISCREPANCIES.md](DISCREPANCIES.md) for tracked IDs and
[COVERAGE.md](COVERAGE.md) for the coverage matrix. The 27/34 probe result is not
a percentage of all spec requirements. Passing round trips cannot establish
mandatory-fold completeness, and §12's full conformance conditions remain unmet.

## Token uplift

The benchmark is the same eight production files of Go 1.26.3 `encoding/json`,
4,264 normalized Go lines. All counts below use the source delta of the identical
measurement prompt with and without its payload. The plain-text tokenizer count
is one higher for these bundles because of token merging at the prompt boundary.

| Input | Source tokens | Reduction vs Go | Whole request tokens | Request reduction vs Go |
|---|---:|---:|---:|---:|
| Go | 32,909 | — | 47,378 | — |
| Smol v0.1 | 28,898 | 12.188% | 43,367 | 8.466% |
| Smol v0.2 code | 16,377 | **50.235%** | 30,846 | **34.894%** |
| Outline with docs | 10,616 | 67.741% | 25,085 | 47.053% |
| Outline | 3,571 | 89.149% | 18,046 | 61.923% |

Relative to v0.1, v0.2 saves another **12,521 source tokens**, or **43.328%**.
The reduction versus Go improves by **38.047 percentage points**. It fits about
**2.01×** as much source in the same source-token budget. For the recorded harness,
whole-request input shrinks **28.872% versus v0.1**.

The fresh local measurements use `o200k_base` (tiktoken 0.14.0). The script verifies
bundle hashes, prompt hashes, every preserved raw usage event, and agreement with
the recorded API counts. Go/v0.1/v0.2 each have three matching request repetitions;
each outline has one. **No new API requests were made during this audit.** The
historical real-request evidence is valid for the freshly regenerated identical
payloads. [Reproducible measurement script](scripts/measure_uplift.py).

### What the gain does and does not mean

- **Code plus notes costs 34,093 tokens**, 3.598% more than Go. The 50.235%
  reduction applies when the model loads code alone; both channels are needed
  for byte-exact reconstruction.
- Actual Go lowered without production notes costs 22,326 tokens in the same
  bundle, giving a **26.646%** code-syntax reduction to 16,377. The spec's
  separately quoted 22,162-token comment-free intermediate was not reproduced by
  this method; it is not a saved fixture. These two denominators should not be
  treated as identical.
- The §11 example reproduces **455 → 250 tokens**, a **45.055%** reduction.
- The 14,469-token harness overhead is fixed in the recorded runs. Different
  harnesses change request-level percentages. Outlines omit program bodies and
  have the correctness gaps above, so their savings are not complete-program
  compression results.
- There is no measurement here of coding accuracy, task completion, latency,
  cost, or other tokenizers. One package does not establish corpus-wide savings.

## Reproduce

```sh
go build -o bin/smol ./cmd/smol
go test -count=1 -json ./...
go vet ./...
python3 scripts/check_corpus.py --output measurements/spec-v0.2-validation/stdlib
bin/smol check ./...  # expected to fail on the import-marker defect
go run ./scripts/validate_v02.go  # expected nonzero: records the seven failures
python3 scripts/prepare_package.py encoding/json \
  --output measurements/spec-v0.2-validation/encoding-json
python3 scripts/validate_lowered.py
python3 scripts/measure_uplift.py  # needs tiktoken; makes no API requests
```

In a filesystem sandbox, set `GOCACHE` to a writable directory. The lowered
`encoding/json` tests need permission to listen on local loopback for `httptest`.
The original-module corpus report predates the added audit program; the final
module report includes it and is stored as `module-final/`.
