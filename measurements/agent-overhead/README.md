**Smol agent instruction overhead — measured 2026-11-15**

The smallest successful instruction condition in this pilot was **zero syntax-guide tokens**. Existing Smol source provided examples: the no-guide arm passed all six episodes on its first attempt and used **5.6% fewer total API tokens** than Go. This establishes an observed condition for these tasks, not a universal minimum for writing Smol.

Teaching more syntax did not improve this small sample. The brief and longer guides needed two extra requests each to repair missing commas; those requests erased the initial input savings. The longer-guide arm also made one behavioral error. These observations do not establish that guides cause worse performance.

Three real [SWE-bench Multilingual](https://huggingface.co/datasets/SWE-bench/SWE-bench_Multilingual) Go tasks, two runs per condition, four conditions: **24 episodes / 28 actual model requests**, using `gpt-6.1-sol`, medium reasoning, Codex CLI 0.164.0. This is a localized patch-writing pilot with no tools, not a full repository-agent or DeepSWE score. Task locations were supplied equally to all arms. The [frozen protocol](PROTOCOL.md) records selection and limits.

| Condition | Guide tokens | First-attempt passes | Final passes | Repairs | Input + output tokens | Change vs Go |
|---|---:|---:|---:|---:|---:|---:|
| Go control | 0 | 6/6 | 6/6 | 0 | 118,515 | — |
| Smol, no syntax guide | 0 | 6/6 | 6/6 | 0 | 111,891 | -5.6% |
| Smol, brief guide | 314 | 4/6 | 6/6 | 2 | 156,755 | +32.3% |
| Smol, longer guide | 976 | 3/6 | 5/6 | 2 | 162,207 | +36.9% |

Input includes cached input; output is the provider-reported output-token count. These are token totals, **not dollar costs**. Cache hits differed across runs. No hidden-test feedback was returned to the model; repairs were allowed only for patch application, lowering or original-test compilation errors. Each repair was a fresh request containing the original context and previous responses/errors, so the totals describe this particular harness.

| Condition | Initial input | Repair input | All output | Cached input (included above) |
|---|---:|---:|---:|---:|
| Go control | 116,642 | 0 | 1,873 | 0 |
| Smol, no syntax guide | 110,166 | 0 | 1,725 | 12,288 |
| Smol, brief guide | 112,098 | 42,303 | 2,354 | 36,864 |
| Smol, longer guide | 116,070 | 43,633 | 2,504 | 0 |

The Go control is the **same comment-free program** as Smol. The original commented Go is counted below only for context; its comments are not credited as syntax savings. Counts use `o200k_base` with identical file wrappers; Smol's import table is charged separately. Positive net values mean fewer complete prompt tokens than Go, including language instructions, import-table context and format/file-name differences.

| Task | Original Go | Comment-free Go | Smol code | Smol import context | Net saving: 314-token guide | Net saving: 976-token guide |
|---|---:|---:|---:|---:|---:|---:|
| gin-gonic__gin-1957 | 11,422 | 7,550 | 5,343 | 330 | +1,549 | +887 |
| gin-gonic__gin-4003 | 6,550 | 4,583 | 3,539 | 157 | +560 | -102 |
| caddyserver__caddy-6051 | 2,948 | 1,784 | 1,244 | 50 | +163 | -499 |

Across the three contexts, syntax alone saves **3,791 / 13,917 tokens (27.2%)**. The 314-token guide adds 322 prompt tokens including its wrapper; the 976-token guide adds 984. The no-guide condition still has a format-selection sentence and 50–330 import-context tokens. The roughly 14.45k-token common Codex scaffolding also reduces the percentage saved on actual requests.

The brief guide pays for its initial prompt cost on all three contexts. The longer guide pays on the largest, but adds 102 and 499 tokens on the smaller two. Full `SPEC.md` costs **8,918 tokens**; it was counted, not evaluated as an arm.

For a guide taught once and retained across new equally sized source bundles, an approximate break-even is `ceil(guide_tokens / (Go_source - Smol_source - import_context))`. The full spec needs **5, 11, or 19 such bundles**, respectively; the longer guide needs **1, 2, or 2**. This is a context-capacity calculation, not a claim that retained instructions stop counting toward cumulative API input. Exact one-request net counts are in the table above.

All four initial Smol parser failures occurred on the header-binding feature: the model omitted a required comma after a nested call's closing `)`. Smol only infers commas between adjacent identifiers/literals in the permitted positions. The one behavioral failure treated whitespace-only heredoc lines as empty and removed a meaningful tab. Neither failure was discarded. An explicit rule to keep commas in newly authored code and let the formatter compact them is a reasonable next experiment; it was not tested here.

The official base fails and reference patch passes for each task. Original Go modules are restored before testing; only the translator temporarily sees a Go 1.22 declaration. Execution used macOS arm64 / Go 1.26.3 rather than the official Linux container. Passing requires the official test command to succeed and every expected FAIL_TO_PASS/PASS_TO_PASS name to be present. The header feature, empty-router fix and heredoc task have 15, 1 and 20 selected top-level tests in their passing reference runs.

[Raw-data audit](audit.json) verifies all prompts, responses, token counts and usage events. [Independent replay](audit-replay.json) reapplies every final patch, formats all 18 Smol candidates to a stable canonical form, and reproduces all 24 outcomes. Supplemental header binding and existing router regression checks pass (40 test executions across 16 episodes). These post-run checks were not exposed to the model.

The sample is three purposively selected public issues from two repositories. Repetitions do not create six independent tasks. Models may already know these public problems; the existing Smol files also act as examples. This does not establish whole-language competence, equal accuracy, the minimum sufficient primer, long-horizon savings, or a dollar/latency benefit.

Reproduce from the repository root (Python with `tiktoken`, Go, authenticated Codex CLI):

```sh
python3 scripts/measure_agent_overhead.py prepare
python3 scripts/measure_agent_overhead.py run
python3 scripts/audit_agent_overhead.py --replay
```

`run` skips identical completed episodes; use a new `--output` directory for new paid model requests. To reuse the exact dataset when preparing another output directory, copy `benchmark-fixtures.json` there first. Preparation downloads pinned repository archives and Go dependencies into `--work`; no reference patches are included in model prompts. The native test harness retains original test comments and applies the official test patch only after the candidate builds.

To audit and regenerate the machine-readable report without network or new model requests:

```sh
python3 scripts/measure_agent_overhead.py report
python3 scripts/audit_agent_overhead.py
```

[Machine-readable report](report.json) · [Pinned sources and task provenance](manifest.json) · [314-token guide](brief.md) · [976-token guide](guide.md) · [Runner](../../scripts/measure_agent_overhead.py) · [Audit/replay](../../scripts/audit_agent_overhead.py)
