# SPEC v0.2 conformance repair and uplift

Validated on 2026-11-15 with Go 1.26.3, darwin/arm64. The original seven failing
probes are fixed, and §6.8 now explicitly preserves braces where an expression
body would lose its boundary, as approved by the user. The recorded §12 corpus gate passes. The machine
verdict is in [summary.json](measurements/spec-v0.2-conformance/summary.json).
Historical failures remain in `measurements/spec-v0.2-validation/`.

## Correctness

| Check | Result | Evidence |
|---|---|---|
| Unit and CLI suite | 128 passing cases, including subtests; no failures/skips | [test events](measurements/spec-v0.2-conformance/tests.jsonl) |
| `go vet ./...` | Pass | [vet result](measurements/spec-v0.2-conformance/vet.json) |
| Standard-library L1–L4 | 2,800 files pass | [stdlib report](measurements/spec-v0.2-conformance/stdlib/report.json) |
| Module L1–L4 | All 15 files pass | [module report](measurements/spec-v0.2-conformance/module/report.json) |
| Original spec-derived probes | 34/34 pass, previously 27/34 | [probes](measurements/spec-v0.2-conformance/probes.json) |
| Required §6 form cases | 39/39 pass: fold, near miss and interior comment for every §6.1–§6.13 form | [case matrix](COVERAGE.md) |
| §11 complete example | Golden Go/code/notes fixtures unchanged; tests pass | [fixtures](testdata/example/) |
| Lowered module tests | 128 cases pass in each channel mode | [module behavior](measurements/spec-v0.2-conformance/lowered-module/report.json) |
| Lowered standard-library tests | 64,225 cases pass in each mode; 521 skips under `-short`; 249 packages pass; 111 have no tests | [stdlib behavior](measurements/spec-v0.2-conformance/lowered/report.json) |

`smol check` now verifies L4's Go tokens and active directives, in addition to
exact normalized round trips and canonical formatting. The original probe runner
also uses an independent Go scanner for its directive/cgo comparisons.

The repair addresses:

- Synthetic import markers colliding with source literals, partial import blocks,
  code-only import order/declaration boundaries, and unused/shadowed imports.
- Inline comments affecting normalized layout, and comments at declaration,
  error-result or tag boundaries being swallowed by folds.
- EOF and block-form line directives, plus indented illustrative `//line` comments
  that must remain ordinary notes.
- Direct embedded pointer/generic fields missing subject elision.
- Ambiguous arrow boundaries, including intervening comments and binary operators.
- Literal bodies left in outlines and non-doc comments/blank lines in `-docs` output.

Details and regression names are in [DISCREPANCIES.md](DISCREPANCIES.md).

### Behavioral environment and scope

The corpus includes the host's active GoFiles, CgoFiles, TestGoFiles and
XTestGoFiles. Standard-package behavior is checked with `go test -count=1 -json
-short` over all 360 selected packages, using overlays of their lowered source.
Of these packages, 249 have passing tests and 111 have no test files.
Both two-channel and code-only production modes run the tests. Test notes remain
in both modes because Go's Example expectations live in `// Output:` comments;
removing them would silently drop assertions.

Homebrew's Go installation omits some binary test fixtures. The behavioral run
therefore uses the checksum-verified official Go 1.26.3 source archive with the
installed compiler tools and generated Go files. All 2,800 selected source files
were compared byte for byte with the installed distribution: **zero differences**.
[Toolchain provenance and hashes](measurements/spec-v0.2-conformance/toolchain.json).
The fixture GOROOT uses its physical path and a separate build cache, so runtime
stack paths refer to the same root. Installed GOROOT was not modified.

Initial harness runs exposed missing fixtures, module-dependent Go compatibility
defaults, and cached stack paths from another GOROOT. Correcting the harness did
not change the package tests. The focused upstream `runtime/debug` suite passed
with the isolated cache before the full behavioral run was repeated.

This is the §12 corpus-based validation profile, not a proof for all Go/Smol
inputs. Inactive platforms, other Go releases, compiler-invalid fixtures and
tests skipped by `-short` are outside the claim. Exact skips and commands are in
the raw reports. [Coverage and remaining bounds](COVERAGE.md).

## Token uplift

The benchmark is the same eight production files of Go 1.26.3 `encoding/json`,
4,264 normalized Go lines. Counts are source deltas inside the identical
measurement prompt. Raw standalone text counts are one token higher due to
merging at the prompt boundary.

| Input | Source tokens | Reduction vs Go | Recorded whole request | Request reduction vs Go |
|---|---:|---:|---:|---:|
| Go | 32,909 | — | 47,378 | — |
| Smol v0.1 | 28,898 | 12.188% | 43,367 | 8.466% |
| Fixed Smol v0.2 code | 16,377 | **50.235%** | 30,846 | **34.894%** |

The conformance fixes preserve the code-channel savings: **12,521 fewer source
tokens than v0.1 (43.328%)**, a **38.047 percentage-point** improvement in reduction
versus Go. Code-only capacity is approximately **2.01×** the Go source capacity.
The recorded whole-request reduction versus v0.1 is **28.872%**.

The Go, code and notes bundles remain byte-identical to their archived payloads.
The script verifies their hashes, prompt hashes, raw usage events and agreement
with fresh `o200k_base` counts (tiktoken 0.14.0). Each full-program API measurement
has three agreeing repetitions. **No new API requests were made.**

The repaired outlines have fresh local counts: **3,379 tokens** without docs and
**10,185 tokens** with docs. These differ from the archived outline payloads and
are not presented as new API measurements. Outlines omit bodies and are not
complete programs.

Loading both code and notes costs **34,093 tokens**, **3.598% more than Go**.
The 50.235% reduction applies to code-only loading. Against the actual Go lowered
without production notes (22,326 tokens), the syntax reduction is **26.646%**.
The §11 example remains **455 → 250 tokens (45.055%)**.

No coding-accuracy, task-success, latency, dollar-cost or cross-tokenizer uplift
was measured. Request percentages use the recorded 14,469-token harness overhead.
[Machine-readable uplift and payload verification](measurements/spec-v0.2-conformance/uplift.json).

## Reproduce

```sh
go build -o bin/smol ./cmd/smol
go test -count=1 -json ./... > measurements/spec-v0.2-conformance/tests.jsonl
go vet ./...
go run ./scripts/validate_v02.go
python3 scripts/check_corpus.py
python3 scripts/check_corpus.py --output measurements/spec-v0.2-conformance/module ./...
python3 scripts/prepare_package.py encoding/json
python3 scripts/validate_lowered.py ./... \
  --output measurements/spec-v0.2-conformance/lowered-module
python3 scripts/validate_lowered.py std --short
python3 scripts/measure_uplift.py  # requires tiktoken; no API requests
python3 scripts/report_conformance.py --run-vet
```

For a distribution with stripped testdata, supply the complete matching GOROOT
before the standard-library behavior command. Use a separate writable `GOCACHE`
for that root. `toolchain.json` records the exact source archive URL/checksum and
generated-file copies used here. The behavioral runner records GOROOT, GOCACHE,
compiler version, source hashes, translator hash, flags, passes and skips.
Tests may require local loopback access.

The report generator with `--run-vet` saves the vet command and exit code in
`vet.json`, alongside `vet.log`. It rejects missing evidence,
failed checks, missing required form cases or a behavioral corpus smaller than
the source-check corpus. Raw stdlib test events are gzip-compressed.
