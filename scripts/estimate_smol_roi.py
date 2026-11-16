#!/usr/bin/env python3
"""Reprice saved trials and estimate conditional fine-tuning economics.

No inference, training, or network requests. Qwen counts describe saved text,
not Qwen model behavior. Training data size, teacher acceptance, labor and
equal-price serving are explicit assumptions rather than observed outcomes.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import statistics

import tokenizers
from tokenizers import Tokenizer

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "measurements/finetuning-roi"


def read(path):
    return json.loads(path.read_text())


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tokenizer", type=Path, default=Path("/private/tmp/smol-overhead/qwen35-tokenizer.json"))
    a = parser.parse_args()
    prices, meta = read(OUT / "prices.json"), read(OUT / "tokenizer.json")
    assert sha(a.tokenizer) == meta["sha256"]
    tokenizer = Tokenizer.from_file(str(a.tokenizer))
    count = lambda text: len(tokenizer.encode(text, add_special_tokens=False).ids)
    gp = prices["gpt_6_1_sol_standard_per_million"]
    qp = prices["qwen_3_5_9b_base_serverless_per_million"]
    repriced, hashes = {}, {}
    for name in ("agent-overhead", "authoring-modes"):
        path = ROOT / "measurements" / name / "report.json"
        hashes[str(path.relative_to(ROOT))] = sha(path)
        report = read(path)
        arms = {}
        for arm, v in report["arms"].items():
            n = v["runs"]
            uncached_cost = (v["input_tokens"] * gp["input"] + v["output_tokens"] * gp["output"]) / 1e6
            recorded_cost = ((v["input_tokens"] - v["cached_input_tokens"]) * gp["input"]
                             + v["cached_input_tokens"] * gp["cached_input"]
                             + v["output_tokens"] * gp["output"]) / 1e6
            # Check raw cache-write counts, omitted from old aggregate records.
            for r in report["records"]:
                if r["arm"] == arm:
                    assert all(t["usage"].get("cache_write_input_tokens", 0) == 0 for t in r["attempts"])
            arms[arm] = {"episodes": n, "resolved": v["resolved"],
                         "uncached_usd_per_1000": uncached_cost / n * 1000,
                         "recorded_cache_usd_per_1000": recorded_cost / n * 1000,
                         "mean_input_tokens": v["input_tokens"] / n,
                         "mean_output_tokens": v["output_tokens"] / n}
        for arm, v in arms.items():
            v["uncached_saving_usd_per_1000"] = arms["go"]["uncached_usd_per_1000"] - v["uncached_usd_per_1000"]
        repriced[name] = arms

    base = ROOT / "measurements/authoring-modes"
    report = read(base / "report.json")
    cases = []
    for c in report["comparisons"]:
        tid = c["task"]
        prompts, counts, removed = {}, {}, {}
        for arm in ("go", "smol-commas", "smol-go"):
            path = base / "runs" / tid / ("r1-" + arm) / "initial-prompt.txt"
            hashes[str(path.relative_to(ROOT))] = sha(path)
            prompt = path.read_text()
            # Strip just the authoring policy; keep the representation marker.
            stripped = re.sub(r"\n<authoring-policy>\n.*?</authoring-policy>\n", "", prompt, flags=re.S)
            prompts[arm] = prompt
            counts[arm] = count(prompt)
            removed[arm] = counts[arm] - count(stripped)
        source_path = base / "tasks" / tid / "sources.json"
        hashes[str(source_path.relative_to(ROOT))] = sha(source_path)
        source = read(source_path)
        go = "".join(f"\n<file path={json.dumps(p)}>\n{s}</file>\n" for p, s in source["go"].items())
        smol = "".join(f"\n<file path={json.dumps(p)}>\n{s}</file>\n" for p, s in source["smol"].items())
        translation = "Translate this Smol v0.2 package context into Go.\n" + source["smol_mod"] + smol + "\n" + go
        lengths = {}
        for arm in ("go", "smol-go"):
            rr = []
            for repeat in (1, 2):
                p = base / "runs" / tid / (f"r{repeat}-" + arm) / "attempt-0/response.json"
                hashes[str(p.relative_to(ROOT))] = sha(p)
                rr.append(count(prompts[arm] + "\n" + p.read_text()))
            lengths[arm] = statistics.mean(rr)
        # Use lengths only. Failed benchmark patches are NOT training targets.
        cases.append({"task": tid, "prompt_tokens": counts, "removable_policy_tokens": removed,
                      "length_proxies": {"translation": count(translation), **lengths}})
    means = {arm: statistics.mean(c["prompt_tokens"][arm] for c in cases)
             for arm in ("go", "smol-commas", "smol-go")}
    removable = statistics.mean(c["removable_policy_tokens"]["smol-commas"] for c in cases)
    gross_saved = means["go"] - means["smol-commas"]
    mixed_length = statistics.mean(.5*c["length_proxies"]["smol-go"]
                                   + .25*c["length_proxies"]["translation"]
                                   + .25*c["length_proxies"]["go"] for c in cases)
    # Assume 32 chat-format tokens/example. Source examples are full task bundles,
    # only four unique issues: useful length anchors, not a representative corpus.
    example_tokens = mixed_length + 32
    teacher_mean = (statistics.mean(c["counts"]["go"]["prompt_tokens"] for c in report["comparisons"])
                    * gp["input"] + repriced["authoring-modes"]["go"]["mean_output_tokens"] * gp["output"]) / 1e6
    teacher_cli = repriced["authoring-modes"]["go"]["uncached_usd_per_1000"] / 1000
    pilot = []
    for examples in (5000, 10000):
        # Three jobs, two epochs/job, 10% validation read twice/job.
        billed_per_job = examples * example_tokens * (2 + .1*2)
        training = 3 * max(prices["qwen_3_5_9b_lora_minimum_per_job"],
                           billed_per_job / 1e6 * prices["qwen_3_5_9b_lora_sft_per_million"])
        # Half the examples need new teacher generation; other half assumed
        # compiler-derived or existing verified material. 25% acceptance is a
        # stress scenario informed by the new pilot, NOT a prediction.
        teacher = examples*.5/.25*teacher_mean
        teacher_with_cli = examples*.5/.25*teacher_cli
        pilot.append({"examples": examples, "assumed_mean_tokens_per_example": example_tokens,
                      "billed_training_tokens_per_job": billed_per_job, "jobs": 3,
                      "training_usd": training, "teacher_lean_api_usd": teacher,
                      "teacher_cli_usd": teacher_with_cli,
                      "validation_and_storage_allowance_usd": 100,
                      "cash_budget_lean_api_usd": training+teacher+100,
                      "cash_budget_cli_usd": training+teacher_with_cli+100})
    savings = {
        "observed_gpt_compression_without_finetuning": repriced["authoring-modes"]["smol-commas"]["uncached_saving_usd_per_1000"] / 1000,
        "qwen_all_compression_input_only_at_base_rate": gross_saved * qp["input"] / 1e6,
        "qwen_remove_policy_only_at_base_rate": removable * qp["input"] / 1e6,
    }
    roi = {name: {"saving_usd_per_1000_requests": delta*1000,
                  "breakeven_requests": {str(cost): cost/delta for cost in (100, 500, 1000, 5000)},
                  "gross_saving_usd": {str(n): n*delta for n in (10000, 100000, 1000000, 10000000)},
                  "roi_percent_1000_upfront": {str(n): 100*(n*delta-1000)/1000 for n in (100000, 1000000, 10000000)}}
           for name, delta in savings.items()}
    cache_sensitivity = []
    g, s = repriced["authoring-modes"]["go"], repriced["authoring-modes"]["smol-commas"]
    for cached_fraction in (0, .5, .9, 1):
        marginal_rate = (1-cached_fraction)*gp["input"] + cached_fraction*gp["cached_input"]
        delta = ((g["mean_input_tokens"]-s["mean_input_tokens"])*marginal_rate
                 +(g["mean_output_tokens"]-s["mean_output_tokens"])*gp["output"]) / 1e6
        cache_sensitivity.append({"fraction_of_saved_input_tokens_cached": cached_fraction,
                                  "saving_usd_per_1000": delta*1000})
    result = {"prices": prices, "tokenizer": dict(meta, library_version=tokenizers.__version__),
              "source_hashes": hashes, "repriced_observed_trials": repriced,
              "qwen_measured_text_counts": cases, "qwen_mean_prompt_tokens": means,
              "qwen_mean_prompt_tokens_saved": gross_saved, "qwen_removable_policy_tokens": removable,
              "pilot_budgets_assumed": pilot, "roi_conditional_equal_marginal_serving_price": roi,
              "cache_sensitivity": cache_sensitivity,
              "dedicated_one_h100_month_730_hours_usd": 730*prices["together_dedicated_h100_per_gpu_hour"],
              "assumed_labor_example": {"hours": 40, "usd_per_hour": 100, "total_usd": 4000},
              "limitations": ["No fine-tuned model was trained or evaluated.",
                  "Qwen tokenization is measured, Qwen task accuracy/output/reasoning length is unmeasured.",
                  "Qwen ROI rows assume equal marginal serving rates and zero extra recurring hosting cost; Together does not quote custom fine-tunes at the base serverless rate.",
                  "All training budgets are scale estimates; examples were not generated. Four known benchmark issues are length proxies only and should remain excluded from a fresh held-out evaluation.",
                  "The observed savings already work without fine-tuning; training cannot automatically be credited with them.",
                  "All-request economics are not cost per successful task; accuracy parity remains unproven."]}
    (OUT / "report.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({k:result[k] for k in ("qwen_mean_prompt_tokens", "qwen_mean_prompt_tokens_saved", "qwen_removable_policy_tokens", "pilot_budgets_assumed", "roi_conditional_equal_marginal_serving_price", "cache_sensitivity")}, indent=2))


if __name__ == "__main__":
    main()
