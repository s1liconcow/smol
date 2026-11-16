# Complete encoding/json package measurement

All eight active production files of the installed Go 1.26.3 `encoding/json`
package were included: 4,264 lines. Tests, dependencies, and inactive build
variants were excluded. Comments, identifiers, literals, and blank lines were
retained. File selection was fixed before measuring token usage.

Actual `gpt-6.1-sol` input usage through Codex, identical in three repetitions:

| Input | Total request tokens | Tokens above empty-source baseline |
|---|---:|---:|
| Empty-source baseline | 14,469 | — |
| Go package | 47,378 | 32,909 |
| Smol package | 43,367 | 28,898 |

**4,011 fewer input tokens per request: 8.466% of the complete request.**
The source bundle itself used 12.188% fewer tokens.

The model only acknowledged each input with `OK`; it used no tools. This
isolates the cost in input tokens of supplying package context. It does not
measure coding quality or total usage across a multi-turn development task.
The 12,288 cached input tokens in every request are included in these totals.

The package was copied into a temporary module, checked for L1–L3, lifted,
checked for canonical formatting, and lowered. Inputs bundle the normalized Go
and canonical Smol files in filename order, with identical `// File:` markers.
The installed Go sources were unchanged.

- [Source manifest, per-file hashes, and selection](manifest.json)
- [Round-trip and formatting validation](validation.json)
- [Go source bundle](package.go.txt) and [Smol source bundle](package.smol.txt)
- [Usage report, payload hashes, and all repetitions](usage/report.json)
- [Raw Go event](usage/1-go.jsonl) and [raw Smol event](usage/1-smol.jsonl)
- [Go source license](GO-LICENSE.txt)

Reproduce from the repository root using an authenticated Codex CLI:

```sh
python3 scripts/prepare_package.py encoding/json
python3 scripts/measure_usage.py \
  --go measurements/encoding-json/package.go.txt \
  --smol measurements/encoding-json/package.smol.txt \
  --model gpt-6.1-sol \
  --output measurements/encoding-json/usage
```
