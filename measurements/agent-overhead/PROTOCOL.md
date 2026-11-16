# Smol authoring overhead pilot

Protocol fixed before the first model request, 2026-11-15.

Question: how much language instruction does an agent need to produce working
Smol edits, and does that cost consume the source-token savings?

Three public SWE-bench Multilingual tasks: gin-gonic__gin-1957 (new HTTP header
binding), gin-gonic__gin-4003 (empty-router panic), caddyserver__caddy-6051 (blank
heredoc lines). Purposive small Go sample, two repositories, not random. DeepSWE
was considered, but its long-horizon tasks confound learning the language with
large repository exploration. This pilot uses actual SWE-bench issues, pinned
base commits, official test patches and official test selection expressions.
It is not a DeepSWE run or an official SWE-bench score.

Four arms: normalized comment-free Go, Smol with no language guide, Smol with a
brief guide, Smol with a longer guide. Two independent runs per task/arm: 24
episodes. Same gpt-6.1-sol model, medium reasoning, JSON patch schema, source
selection, task statement, and maximum of two compile/patch repair requests.
Arm order rotates across tasks/repetitions. Each request has a fresh temporary
Codex home and empty working directory. No tool calls are permitted or accepted.
Language guides are frozen before runs; their hashes and exact prompts are saved.

This is a **localized patch-writing** experiment, not autonomous repository
exploration. The evaluator supplies relevant files and allowed new filenames.
Benchmark patch paths were inspected during task selection, so localization is
an oracle advantage shared by every arm. Original issue statements are unchanged.
Reference solutions and held-out tests are not shown to the model. The model
receives no feedback on held-out test failures. It may receive exact-patch,
Smol-lowering, and original-test-compilation errors. Each repair must produce a
complete patch against the original source; all repair prompts and usage count.

The Go control omits the same comments/blank lines that Smol omits. This avoids
claiming comment removal as a language-syntax advantage. Original commented Go
is counted separately, but not evaluated as another arm. Smol import-table
context is charged to Smol. Normal Go constructs accepted in Smol are allowed;
canonical formatting is reported separately and never substitutes for passing
tests. No model is required to maximize shorthand use in its patch.

Before requests, official tests must fail on the base and pass on the gold
patch, and the selected files must lower and compile. Host: macOS arm64, Go
1.26.3, native execution rather than the benchmark's Linux container. Original
tests and directives retain their comments. Targeted test results are checked
for all expected FAIL_TO_PASS and PASS_TO_PASS names, not just an exit code.
The historical modules declare Go versions below Smol's 1.22 minimum. The
harness temporarily sets `go 1.22` only while invoking the translator, restoring
the original go.mod bytes before every build/test. No production dependency or
benchmark test is changed for this adaptation.

Measures:

- Standalone guide tokens and complete prompt tokens with o200k_base.
- Exact complete-prompt difference versus Go, including the guide/import table.
- Gross source syntax savings and hypothetical repeated-load break-even when
  one guide is retained in context. Cumulative API usage repeats context and
  does not have this same amortization model.
- First-attempt lowering/build/task success, final task success, repair count,
  raw API input/cached-input/output tokens, and input plus output totals.
- Full SPEC.md counted as a reference overhead, not used as an evaluated arm.

Limitations fixed in advance: six episodes per arm cannot establish equal
accuracy, the minimum sufficient guide, a universal break-even, or results for
long-horizon agents. Public tasks may be familiar to the model. Repeated runs
are not independent tasks. No dollar-cost or speed conclusion will be drawn.
Failures and unfavorable comparisons stay in the report.
