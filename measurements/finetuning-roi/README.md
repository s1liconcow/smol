# Smol fine-tuning: cost and savings estimate

Estimate date: **November 15, 2026**. USD, before tax. No model was trained and
no inference requests were made for this estimate. Observed trial usage is
repriced at current published rates; projected training and deployment outcomes
are explicitly conditional.

**The token-only case for fine-tuning is weak at modest volume.** Smol already
saves tokens without training. A useful fine-tune must preserve accuracy where
untuned Smol fails, enable more compression, or reduce costly extra attempts.
Simply learning the 34-token guide has very little economic value.

## Observed usage, current API-equivalent cost

GPT-6.1 Sol Standard lists $2/M input, $0.10/M cached input, and $10/M output.
These are API-equivalent estimates, not the user's Codex subscription invoice.
All saved requests have zero reported cache-write tokens and are below the
long-context threshold. [Official model pricing](https://developers.openai.com/api/docs/models/gpt-6.1-sol).

| Follow-up condition | Episodes passed | Mean input | Mean output | Cost per 1,000 episodes, all input uncached | Saving per 1,000 |
|---|---:|---:|---:|---:|---:|
| Go | 2/8 | 20,428.1 | 383.0 | $44.69 | — |
| Smol, 34-token rule | 2/8 | 19,234.6 | 380.9 | $42.28 | **$2.41** |
| Read Smol, author Go | 2/8 | 19,608.6 | 420.1 | $43.42 | $1.27 |

The original pilot independently prices at $42.00/1,000 Go episodes and
$39.60/1,000 no-guide Smol episodes: **$2.41/1,000 saved**, with both passing 6/6.
The two studies remain separate; all failures and repairs are counted. See
[original evidence](../agent-overhead/README.md) and
[follow-up evidence](../authoring-modes/README.md).

Actual recorded cache hits produce larger apparent follow-up savings:
$35.93/1,000 Go versus $27.69/1,000 short-rule Smol. Cache hits differed between
conditions, so the $8.25 difference cannot all be attributed to compression.
For an equal-cache comparison, if 90% of the *removed input tokens* would have
been cache hits, savings fall from $2.41 to **$0.37/1,000**. Shared prefix caching
does not imply that newly loaded source gets that same hit rate.

## A concrete open-model price and tokenizer

Together lists **Qwen3.5-9B LoRA training at $0.34/M processed tokens**, minimum
$4/job, with validation reads billed too. Its base serverless inference is
$0.17/M input and $0.25/M output.
[Training prices](https://www.together.ai/pricing) ·
[Base inference catalog](https://docs.together.ai/docs/serverless/models).

I downloaded only the official tokenizer and counted the actual saved prompts.
No Qwen inference or accuracy result is implied. The tokenizer is pinned by
revision and SHA-256 in [tokenizer.json](tokenizer.json).

| Task | Go input | Short-rule Smol input | Tokens removed |
|---|---:|---:|---:|
| gin-1957 | 8,716 | 6,690 | 2,026 |
| gin-3820 | 4,317 | 3,609 | 708 |
| gin-3227 | 10,086 | 8,047 | 2,039 |
| caddy-5404 | 3,398 | 2,952 | 446 |
| **Mean** | **6,629.25** | **5,324.50** | **1,304.75** |

These are text-only prompt counts, including the issue, source wrappers, import
table and policy. Unknown provider instructions and chat overhead are excluded.
The paired source difference is measured; total Qwen request cost and reasoning
length are not. No Go-to-Qwen quality equivalence is assumed.

At the base input rate, all this compression is worth **$0.2218/1,000 requests**,
assuming identical output behavior. Removing the short policy block alone saves
**49 Qwen tokens**, including its wrapper, worth **$0.00833/1,000**. The same
policy text is 34 tokens in the earlier o200k tokenizer; these counts are not
interchangeable.

## Proposed pilot budget

This is a planning scenario for 5,000–10,000 *new* verified examples, not a
generated dataset. Existing benchmark cases are used only as length proxies.
Their failed model outputs must not become training targets, and the cases
should stay out of a fresh held-out evaluation.

Assumptions: 50% Smol-to-Go editing examples, 25% compiler-derived translation,
25% existing verified Go examples. On our four task bundles, the weighted
prompt-plus-answer length is about 7,063 Qwen tokens/example, including an
assumed 32-token chat wrapper. Three candidate training jobs each run two epochs;
a validation set of 10% additional examples is read twice per job.

| Cash item | 5,000 examples | 10,000 examples |
|---|---:|---:|
| Three LoRA jobs, including validation reads | $79 | $158 |
| New teacher examples, lean API prompts | $158 | $316 |
| Test compute/storage allowance | $100 | $100 |
| **Total with lean API generation** | **$337** | **$574** |
| **Total using the existing CLI scaffold for generation** | **$626** | **$1,152** |

Teacher estimates use our observed Go prompt length (5,984 visible tokens) and
mean output usage (383 tokens) at GPT-6.1 Sol's rates. Half the dataset needs new
teacher generation. A **25% acceptance rate is a stress assumption**, informed
by the follow-up's low pass rate; it is not a forecast. That implies 10,000–20,000
teacher attempts. A 50% acceptance rate halves this line, while 10% increases it
2.5-fold. Generated prose alone is not accepted as correctness evidence.

The remaining translation/Go data is assumed compiler-derived or already
available and verified. Producing additional teacher examples for that portion
adds cost. The $100 test/storage line is an allowance, not a measured invoice.
Engineering labor is excluded: an illustrative 40 hours at $100/hour adds
**$4,000**. That rate and duration are user-adjustable assumptions.

## Break-even: distinguish three different benefits

For **$1,000 upfront**, with no added recurring hosting cost:

| Benefit credited | Saving / 1,000 requests | Requests to recover $1,000 |
|---|---:|---:|
| Existing untuned Smol on GPT-6.1 Sol | $2.408 | 415,000 |
| All measured Qwen input compression, priced at base rate | $0.222 | 4.51 million |
| Fine-tuning only removes the short Qwen policy block | $0.00833 | 120 million |

The first row is implementation ROI for an optimization that does not require
fine-tuning. GPT-6.1 Sol is not fine-tunable. The second row can be credited to
training **only if training is what makes Smol usable at the required accuracy**;
that has not been shown. If untuned Qwen already handles Smol, training cannot
claim those savings. The third row isolates the extra value of eliminating the
guide, assuming unchanged accuracy/output/serving price.

At one million requests/month, these save $2,408, $222 and $8.33/month
respectively. The conditional Qwen compression case recovers $1,000 in 4.5
months, versus 120 months for guide removal alone. Adding the illustrative
$4,000 labor multiplies those break-even volumes by five. At 100,000 requests
per month, payback takes ten times longer.

Formula:

```text
monthly net saving = requests/month × saving/request − extra recurring cost
payback months = upfront cost / monthly net saving
ROI over N requests = (N × saving/request − upfront cost) / upfront cost
```

A nonpositive monthly net saving means no payback. At the conditional Qwen
compression rate, $1,000 upfront yields **−77.8% ROI after one million requests**,
and **+121.8% after ten million**, before extra hosting and assuming equal quality.

## Hosting can dominate

**The base serverless price is not a custom fine-tune serving quote.** Together
documents dedicated endpoints or downloading the checkpoint for local serving.
Dedicated replicas bill while idle.
[Fine-tuned deployment documentation](https://docs.together.ai/docs/fine-tuning/deployment).

Its listed dedicated H100 price is $5.49/GPU-hour: one continuously running GPU
costs **$4,008 per 730-hour month**, before storage. This is an example deployment,
not a claim that this model requires an H100 or that one GPU meets the desired
throughput. [Dedicated GPU price](https://www.together.ai/pricing).

Under hourly billing, shorter prompts do not automatically lower the bill.
Savings require fewer billed hours, fewer GPUs, or avoiding additional capacity.
Measure serving throughput and utilization before substituting a dollar-per-token
projection. An existing GPU, scheduled batch serving, or a verified shared-adapter
service changes the economics substantially. This estimate does not assume any
of those is free or already available.

## Decision

For token savings alone, use the minimal-policy approach first. A **roughly
$350–$1,200 cash pilot**, plus engineering time, is a reasonable research budget
to test whether fine-tuning preserves task accuracy. It is not an investment
with demonstrated positive ROI yet. A cheaper model replacing an expensive one
could save much more, but that is a separate model-substitution benefit requiring
its own accuracy comparison.

Use cost per successfully completed task as the production metric. Our small
pilot cannot establish quality parity or the cost of regressions and human
review. Neither these projections nor the original measurements establish
faster generation.

## Reproduce

Install `tokenizers` in an isolated Python environment. Download the URL in
`tokenizer.json` to `/private/tmp/smol-overhead/qwen35-tokenizer.json`; the script
verifies its SHA-256. Then run:

```sh
python scripts/estimate_smol_roi.py
```

[report.json](report.json) contains counts, budgets, sensitivities and input
hashes. [prices.json](prices.json) records dated rates and source URLs. The
calculator makes no network calls and does not train or run any model.
