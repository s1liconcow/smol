# Smol

A compact Go surface syntax targeting [SPEC.md](SPEC.md) v0.2. Go source is
lifted into a dense code channel (`.smol`) and a notes channel
(`.smol.notes`) holding comments and blank lines. The Go standard library is
the translator's only dependency. Requires Go 1.22 or newer.

The recorded v0.2 conformance profile passes: 2,815 files satisfy L1–L4, and
the 360-package standard-library run succeeds under `-short` in both
channel modes. The fixes cover import inference, exact comment layout,
code-only directives, embedded subjects, expression boundaries, and outlines.
The original 34 spec probes pass, with 39 required fold/near-miss/comment cases
and a strengthened L1–L4 corpus checker. See the recorded validation profile in
[VALIDATION.md](VALIDATION.md), [COVERAGE.md](COVERAGE.md), and
[DISCREPANCIES.md](DISCREPANCIES.md).

```sh
go build -o bin/smol ./cmd/smol
bin/smol lift ./...       # Go -> code and notes; update smol.mod
bin/smol fmt ./...        # canonicalize both channels
bin/smol fmt -check ./... # verify formatting without writing
bin/smol lower ./...      # code + optional notes -> normalized Go
bin/smol lower -lines .   # include Smol source positions
bin/smol check ./...      # verify L1–L4 without writing
bin/smol check std        # audit the installed standard library
bin/smol outline .        # signatures and declarations
bin/smol outline -docs .  # include doc comments
```

Arguments may be files, directories, or Go package patterns. Translation keeps
file names, including `_test` and platform suffixes. Import and zero-value
resolution use package declarations, `smol.mod`, and the installed Go toolchain.
Run commands from the module whose dependencies are being translated.

The translator implements implicit imports, declarations and inherited receivers,
error results, conventional parameters, error handlers, abbreviated returns,
range loops, expression bodies, struct tags, named zero values, opened packages,
subject members, file parameter conventions, and implied commas. Outputs use
atomic file replacement; each package is translated before its files are written.

## Measured savings

The complete Go 1.26.3 `encoding/json` production package contains eight files
and 4,264 normalized Go lines. Current output reproduces the recorded
`gpt-6.1-sol` request measurements:

| Input | Source tokens | Reduction vs Go | Whole request | Request reduction vs Go |
|---|---:|---:|---:|---:|
| Go | 32,909 | — | 47,378 | — |
| Smol v0.1 | 28,898 | 12.188% | 43,367 | 8.466% |
| Smol v0.2 code | 16,377 | **50.235%** | 30,846 | **34.894%** |

v0.2 saves another **12,521 source tokens (43.328%) versus v0.1**.
The §11 example goes from **455 to 250 tokens (45.055%)**.

The source reduction applies to loading the code channel alone. Loading code
and notes together costs 34,093 tokens, **3.598% more than Go**. Both channels
are needed for byte-exact reconstruction. These measurements do not establish
coding accuracy, speed, dollar cost, or savings on other packages/tokenizers.

The audit regenerated every benchmark bundle and verified hashes, raw recorded
usage and fresh `o200k_base` counts. No new API requests were made. The preserved
Go/v0.1/v0.2 API runs each have three identical repetitions and a 14,469-token
empty-input baseline; cached input tokens are included. Identical filename
markers appear in each bundle.

[Current audit and uplift](VALIDATION.md) ·
[Machine-readable uplift](measurements/spec-v0.2-conformance/uplift.json) ·
[Preserved v0.2 API usage](measurements/encoding-json/usage/report.json) ·
[Preserved v0.1 baseline](measurements/encoding-json-v0.1/usage/report.json)

## Validation and reproduction

```sh
go test ./...
go vet ./...
python3 scripts/check_corpus.py
go run ./scripts/validate_v02.go  # independent spec-derived probes
python3 scripts/prepare_package.py encoding/json \
  --output measurements/spec-v0.2-conformance/encoding-json
python3 scripts/check_corpus.py --output measurements/spec-v0.2-conformance/module ./...
python3 scripts/validate_lowered.py ./... --output measurements/spec-v0.2-conformance/lowered-module
# Full host stdlib profile needs complete Go distribution test fixtures:
python3 scripts/validate_lowered.py std --short
python3 scripts/measure_uplift.py # requires tiktoken; no API requests
```

To collect new real-request usage with an authenticated Codex CLI:

```sh
python3 scripts/measure_usage.py \
  --go measurements/spec-v0.2-conformance/encoding-json/package.go.txt \
  --smol measurements/spec-v0.2-conformance/encoding-json/package.smol.txt \
  --model gpt-6.1-sol --repeats 3 \
  --output measurements/new-usage
```

This sends the selected source to OpenAI. Each probe uses a fresh, read-only
conversation with the same prompt and no tools. The script records
`turn.completed.usage`, subtracts an empty-input baseline, alternates request
order, and rejects varying counts. It uses an isolated temporary Codex home and
removes its temporary credential copy when finished.
