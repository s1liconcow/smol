# Short authoring policies: read Smol, write conservative syntax

The promising design is to let tools compress the code, with little syntax
instruction for the model. A 34-token rule to keep commas, braces and explicit
returns saved **5.7% of total recorded API tokens**. A 78-token rule to read Smol
but author ordinary Go saved **3.8%**. Both compiled on the first attempt in all
eight episodes, with no repair requests.

**This does not establish unchanged coding performance.** Every condition,
including Go, resolved only 2/8 episodes. Those successes were the reused
header-binding development task. All three new tasks failed their benchmark
tests in all conditions. Matching failing controls is weak evidence of
preserved accuracy. Median request duration also increased in both Smol
conditions; this experiment does not show a speed improvement.

## Measured comparison

Four SWE-bench Multilingual Go issues, two fresh episodes each, three conditions:
24 model requests. Model `gpt-6.1-sol`, medium reasoning, Codex CLI 0.164.0.
Task locations and equivalent comment-free source are provided; no tools or
reference patches are available to the model. Upstream tests are held out.
At most two syntax/build repairs are allowed; none were used. There are no
retries after semantic test failures. See the frozen [protocol](PROTOCOL.md).

| Condition | Policy tokens | First-attempt builds | Tasks resolved | Repairs | Input + output tokens | Saving vs Go |
|---|---:|---:|---:|---:|---:|---:|
| Ordinary Go | 0 | 8/8 | 2/8 | 0 | 166,489 | — |
| Smol, keep commas/returns/braces | 34 | 8/8 | 2/8 | 0 | 156,924 | 5.75% |
| Read Smol, author Go | 78 | 8/8 | 2/8 | 0 | 160,230 | 3.76% |

Counts include failed episodes, all API input tokens including cached tokens,
and output including reported reasoning tokens. They are not dollar savings.
The exact policy wrappers add 44 and 90 prompt tokens, respectively, relative
to the original pilot's no-guide Smol prompt.

| Evaluation group | Go | Short comma rule | Go authoring rule | Comma-rule token saving | Go-authoring token saving |
|---|---:|---:|---:|---:|---:|
| Reused development task, two episodes | 2/2 | 2/2 | 2/2 | 8.08% | 7.41% |
| Three new tasks, six episodes | 0/6 | 0/6 | 0/6 | 4.86% | 2.38% |

Both Smol conditions tie Go's success/failure outcome in all eight pairs.
The repetitions are not eight independent repositories, and the sample cannot
support a non-inferiority claim. These public tasks were held out from policy
design, not from possible model pretraining.

Median CLI request durations were **13.3 s Go, 18.3 s short rule, 22.5 s Go
authoring**. These include service/CLI delay and model generation, exclude local
patch validation, and were measured with some independent trials running
concurrently. They are descriptive observations, not a controlled latency study.

## Where the tokens go

Once per task, equivalent source totals fall from **20,891 Go tokens to 15,267
Smol tokens (26.9%)**. Smol's import tables add 653 tokens. All counts below use
`o200k_base` and tiktoken 0.14.0; Go comments are excluded equally from both
conditions, so comment removal does not inflate the syntax saving.

| Task | Go source | Smol source | Import table | Net prompt saving, short rule | Net prompt saving, Go authoring |
|---|---:|---:|---:|---:|---:|
| gin-1957: header binding | 7,550 | 5,343 | 330 | 1,827 | 1,781 |
| gin-3820: optional file binding | 3,000 | 2,197 | 130 | 624 | 578 |
| gin-3227: redirect behavior | 8,713 | 6,598 | 143 | 1,923 | 1,877 |
| caddy-5404: lexer panic | 1,628 | 1,129 | 50 | 400 | 354 |

Across the full matrix, visible initial prompts total 47,872 tokens for Go,
38,324 for the short rule (**19.9% less**) and 38,692 for Go authoring (**19.2%
less**). The much smaller API-total savings reflect roughly 14,450 additional
input tokens per CLI request, plus model output. This overhead is counted.

The API input count minus the locally tokenized prompt is 14,397–14,453 for
23 requests and 17,021 for gin-3820's second Go-authoring request. The reason for
that extra service/CLI input is not exposed in the saved events. Its tokens
remain in the reported cost; they are not normalized away. Cached-input totals
also differ between conditions, so these results should not be converted into
dollar savings without accounting for pricing and cache behavior.

## Why the task checks failed

All four environments passed a gate: the unchanged benchmark tests fail on the
base commit and pass with the reference patch. The comment-free lowered source
also builds with the original tests. Reference patches are used only by the
evaluator, never included in model requests.

The new-task failures are behavioral, not syntax failures:

- **gin-3820:** the candidates leave `TestMappingBaseTypes` failing for a
  `*multipart.FileHeader`; fixes to multipart-specific handling do not satisfy
  the generic mapping behavior covered by the test.
- **gin-3227:** the candidates leave `TestRedirectTrailingSlash` failing; changing
  the redirect option passed by the engine does not fully repair the tree's
  path lookup behavior.
- **caddy-5404:** candidates leave `TestLexer` failing, including the expected
  heredoc text versus an extra trailing newline. Fixing indexing alone does
  not satisfy all the benchmark's required behavior.

The exact failures and every patch are in `runs/`. A failed task is retained
even when a candidate addresses part of the reported issue.

## What to use

The best measured low-overhead rule is [commas.md](commas.md):

> In newly written code, keep every argument comma, including after nested calls.
> Use explicit braces and return statements. Leave compression to smol fmt;
> do not invent shorthand.

The architectural alternative is [go_authoring.md](go_authoring.md): match
existing Smol anchors exactly, but write new code with ordinary Go syntax,
explicit commas, types, qualifiers, braces and returns. The current lowerer
already accepts these Go constructs in Smol files. Both generated header
implementations use ordinary Go function declarations and pass the held-out
tests. Formatting is checked separately by replaying the saved patches.

For an agent integration, use compact source for reading and keep compression
in the formatter. Preserve access to comments/documentation and expanded Go
when the task needs them. Those retrieval/fallback choices are design proposals;
this localized experiment did not measure their end-to-end cost or accuracy.
Do not use this result to justify unconditional deployment or a large language
guide. A broader repository-agent evaluation with successful Go controls is
still needed to establish a useful accuracy bound.

The [original pilot](../agent-overhead/README.md) remains separate: its no-guide
condition passed 6/6 and saved 5.6% of total tokens, whereas longer guides caused
repair costs. This follow-up addresses that observed authoring failure but is
not a pooled accuracy estimate with the earlier, differently selected tasks.

## Reproduce and inspect

All upstream commits, fixture hashes, tool versions and task configurations are
in [manifest.json](manifest.json). The dataset is
[SWE-bench Multilingual](https://huggingface.co/datasets/SWE-bench/SWE-bench_Multilingual),
revision `846e647b9f33c0b51b739d005d13d85493c9af09`. This is a localized patch-writing
study, not a DeepSWE/full SWE-bench agent score. The supplied source locations
were selected using reference-patch filenames, an advantage shared by all arms.

Use Python with tiktoken installed, Go, and an authenticated Codex CLI. This
recorded run used macOS arm64, Go 1.26.3 for the translator and the Gin tests,
and **Go 1.20.14 for all Caddy-5404 tests**, because its pinned quic-go dependency
rejects newer Go versions. The latter is an environment fix made before any
Caddy model request; no dependencies or tests were patched. The compiler archive
and checksum come from the [official Go downloads](https://go.dev/dl/), and are
recorded in [toolchain.json](toolchain.json).

For the default work directory on this platform, unpack the checksum-verified
`go1.20.14.darwin-arm64.tar.gz` under
`/private/tmp/smol-overhead/toolchains/go1.20.14/`, leaving `go/bin/go` beneath
that directory. On another platform, use the corresponding official Go 1.20.14
archive. Then:

```sh
python scripts/measure_authoring_modes.py prepare
python scripts/measure_authoring_modes.py run
python scripts/measure_authoring_modes.py report
python scripts/audit_agent_overhead.py \
  --profile authoring --output measurements/authoring-modes --replay
```

Existing completed episodes are never overwritten by `run`; it checks their
settings and prompt hashes. To make fresh requests, use a new `--output`
directory and prepare it first. Preparing requires network access for pinned
source archives/modules. `run` makes authenticated model requests. Credentials
remain in temporary protected directories outside this repository.

The translator's view of historical go.mod files is temporarily raised to Go
1.22, then restored before all builds/tests. Original tests, comments, compiler
directives and module dependencies are retained. All candidates use the same
base source and task-specific compiler.

- [Full report and raw episode index](report.json)
- [Development/new-task breakdown and latency observations](comparison.json)
- [Raw evidence and formatting/test replay audit](audit-replay.json)
- [Frozen protocol](PROTOCOL.md), [34-token rule](commas.md), [78-token rule](go_authoring.md)

The final reporting pass verifies prompt/response hashes, tokenizer counts,
raw usage events, and absence of model tool calls, then replays all saved final
patches. Upstream tests and outcome checks are run again after `smol fmt`,
`smol fmt -check` and lowering. This preserves failed outcomes as well as passes.
