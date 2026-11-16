# Current v0.2 audit evidence

See [VALIDATION.md](../../VALIDATION.md) for the verdict, findings, uplift and
reproduction commands. Implementation and spec hashes are pinned in
[summary.json](summary.json).

- `tests.jsonl`: original 70-case suite before adding the audit program.
- `tests-final.jsonl`: suite after adding the audit program; 70 cases pass.
- `vet.log`: empty output from successful `go vet ./...`.
- `stdlib/`: current L1–L3 corpus run, 2,800 files pass.
- `module/`: original module, 12 pass / 1 fail.
- `module-final/`: module including audit program, 13 pass / 1 fail.
- `probes.json`, `probes.log`: 34 spec-derived cases, 27 pass / 7 fail;
  failing inputs and outputs are retained. Exit status 1 is intentional evidence
  of current defects, not a successful conformance check.
- `encoding-json/`: freshly regenerated benchmark bundles, manifest,
  round-trip/format validation and upstream Go license.
- `lowered/`: package tests run through Go overlays with both channels and
  without production notes. Both modes run 857 passing cases; test notes remain
  so Go Example output assertions execute. Go installation unchanged.
- `uplift.json`: fresh local token counts and verification of preserved real API
  usage. No API requests were made in this audit.

The preserved API evidence under `../encoding-json/` and
`../encoding-json-v0.1/` was not regenerated or overwritten. Fresh bundle hashes
and token deltas match those historical payloads and recorded input counts.

Scope: Go 1.26.3, darwin/arm64, active production/cgo/internal-test/external-test
files. The report does not claim all-platform conformance, complete
standard-library behavioral coverage, or a model-accuracy improvement.
