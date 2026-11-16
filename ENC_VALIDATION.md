# Output encoding implementation and validation

`smol enc` and `smol dec` implement the anchored patch transport in
[SPEC_ENC.md](SPEC_ENC.md). E1 is the default; E0 is the explicit-Go fallback and
E2 is available only when selected with `-profile E2`. E2 model acceptance has
not been established.

```sh
go build -o bin/smol ./cmd/smol
bin/smol enc old-tree new-tree > change.enc
bin/smol enc -git HEAD > change.enc
bin/smol dec -n change.enc  # validate and inspect the diff
bin/smol dec change.enc
bin/smol enc -instructions # exact §7 text
bin/smol enc -check ./...
```

The decoder resolves every address against the original tree, validates every
result and import before writing, stages file replacements, and reports the
resolved spans. Partial edits preserve the surroundings of unformatted files;
explicit whole-file replacements may format the entire file. The canonical
output normalizer includes gofmt and §5.3 declaration spacing. These two
clarifications were approved during implementation.

## Current gate

The unit/CLI suite has 188 passing cases, with vet and race checks passing.
The two approved normalization clarifications have passing minimal R5 checks.
The full tool gate is **not yet passed**: the remaining §5.1 decision concerns
significant comment indentation. The minimal reproducer and current result are
in `measurements/enc-validation/spec-decisions.json` and
`spec-decision-results.jsonl`. Dropping indentation moves a switch-case comment
and breaks R1/R5. The final history/mutation replay awaits this decision; earlier
superseded runs are excluded from the final evidence directory.

## Evidence

Current results live in [measurements/enc-validation](measurements/enc-validation/).
The machine summary distinguishes the tool corpus gate from the model gate.
Unit/CLI tests cover headers, escaped marker lines, nested scopes, ambiguity,
original coordinates, block/range spans, overlap, attached comments, renames,
new/deleted files, CRLF/fences, raw strings, block comments, import resolution
and precedence, cgo, build constraints, significant formatting, E2 folds,
profile exclusions, dry runs, permissions and multi-file rejection.

The history runner checks consecutive first-parent revisions from ten external
Go modules and this module, recording both commit hashes. It loads all Go files
in directories containing changed Go files. Deliberately invalid compiler
`testdata` and vendored sources are excluded. Commits without Go changes remain
in the accounting and are reported separately. Module-owned package names come
from their source declarations; dependency names are read from installed module
sources and restricted to the revision's declared requirements. All final
resolver inputs are explicit checker options.

For each pair the checker verifies E0, E1 and E2 encoding/decoding, and verifies
that E0 output also decodes under E1 and E2. Each E1 patch line is independently
deleted, duplicated and truncated. Accepted mutations must still parse. The
report separates accepted header/body mutations; acceptance does not establish
semantic correctness and the spec sets no threshold for this experiment.

No model requests are made by the validation scripts. Replaying recorded Go
outputs through the production tool is an encoder/decoder check. It does not
measure whether a model writes encoded patches correctly. The §10.2 trial of
100 held-out tasks across ten repositories, first-attempt decoding, repairs,
fallbacks, reasoning tokens and cost per resolved task remains a separate gate.

## Reproduce

```sh
GOCACHE=/tmp/smol-validation-go-cache go test -count=1 -json ./... \
  > measurements/enc-validation/tests.jsonl
go vet ./...
go test -race ./internal/enc ./cmd/smol
python3 scripts/check_enc_history.py
python3 scripts/validate_enc_replay.py
python3 scripts/report_enc_validation.py
```

`check_enc_history.py` fetches shallow, read-only Git histories into
`/tmp/smol-enc-history`. `--reuse` reuses those snapshots; `--no-mutate` runs the
faster recovery-only pass. Commit hashes, source selection, compiler/tool
hashes, raw per-pair results and mutation totals are retained in its reports.
The original pilot snapshots, prompts, results and prototype measurements are
preserved unchanged.
