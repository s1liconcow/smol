#!/usr/bin/env python3
"""Follow-up: read Smol, author conservative syntax or ordinary Go.

Reuses the original pilot's request/evaluation machinery and frozen upstream
fixtures; changes only the selected tasks and authoring policy. Original pilot
results and prompts remain untouched.
"""
import argparse
import json
from pathlib import Path
import shutil
import statistics

import measure_agent_overhead as bench

ORIGINAL_PROMPT = bench.make_prompt
TASKS = {
    "gin-gonic__gin-1957": bench.TASKS["gin-gonic__gin-1957"],
    "gin-gonic__gin-3820": {
        "files": ["binding/form_mapping.go", "binding/multipart_form_mapping.go"],
        "new": [], "package": "./binding", "test_regex": "^TestMapping",
    },
    "gin-gonic__gin-3227": {
        "files": ["tree.go", "gin.go"], "new": [], "package": ".", "test_regex": "^TestRedirect",
    },
    "caddyserver__caddy-5404": {
        "files": ["caddyconfig/caddyfile/lexer.go"], "new": [],
        "package": "./caddyconfig/caddyfile", "test_regex": ".", "test_toolchain": "go1.20.14",
    },
}
GUIDES = {"commas": "overhead_commas.md", "go_authoring": "overhead_go_authoring.md"}


def make_prompt(a, task_id, arm):
    prompt, counts = ORIGINAL_PROMPT(a, task_id, arm)
    if arm == "go":
        return prompt, counts
    guide_name = "commas" if arm == "smol-commas" else "go_authoring"
    guide = (a.output / (guide_name + ".md")).read_text()
    marker = "Source representation: Smol v0.2. Write all edits in Smol.\n"
    representation = marker if arm == "smol-commas" else (
        "Source representation: Smol v0.2. Write newly authored code in ordinary Go syntax.\n")
    assert prompt.count(marker) == 1
    prompt = prompt.replace(marker, representation + "\n<authoring-policy>\n" + guide + "</authoring-policy>\n")
    counts.update(prompt_tokens=bench.tokens(prompt), instruction_tokens=bench.tokens(guide),
                  guide_sha256=bench.sha(guide), prompt_sha256=bench.sha(prompt))
    return prompt, counts


def configure():
    bench.TASKS = TASKS
    bench.ARMS = ["go", "smol-commas", "smol-go"]
    bench.make_prompt = make_prompt


def summarize(a):
    report = json.loads((a.output / "report.json").read_text())
    records = report["records"]
    groups = {}
    for name, selected in {
        "all": set(TASKS), "development": {"gin-gonic__gin-1957"},
        "new_tasks": set(TASKS) - {"gin-gonic__gin-1957"},
    }.items():
        arms = {}
        for arm in bench.ARMS:
            rr = [r for r in records if r["arm"] == arm and r["task"] in selected]
            attempts = [t for r in rr for t in r["attempts"]]
            arms[arm] = {
                "episodes": len(rr), "resolved": sum(r["resolved"] for r in rr),
                "first_pass_builds": sum(r["attempts"][0]["stage"] == "tests" for r in rr),
                "repairs": len(attempts) - len(rr),
                "input_tokens": sum(r["usage"]["input_tokens"] for r in rr),
                "output_tokens": sum(r["usage"]["output_tokens"] for r in rr),
                "cached_input_tokens": sum(r["usage"]["cached_input_tokens"] for r in rr),
                "visible_initial_prompt_tokens": sum(r["counts"]["prompt_tokens"] for r in rr),
                "generation_seconds_median": statistics.median(t["generation_seconds"] for t in attempts) if attempts else None,
            }
            arms[arm]["total_tokens"] = arms[arm]["input_tokens"] + arms[arm]["output_tokens"]
        for arm, values in arms.items():
            base = arms["go"]
            values["token_savings_percent"] = (100 * (1-values["total_tokens"]/base["total_tokens"])
                                                if base["total_tokens"] and base["episodes"] == values["episodes"] else None)
            control = {(r["task"], r["repeat"]): r for r in records if r["arm"] == "go" and r["task"] in selected}
            pairs = [(r, control[(r["task"], r["repeat"])]) for r in records
                     if r["arm"] == arm and r["task"] in selected and (r["task"], r["repeat"]) in control]
            values["paired_success_vs_go"] = {
                "wins": sum(r["resolved"] and not b["resolved"] for r, b in pairs),
                "losses": sum(not r["resolved"] and b["resolved"] for r, b in pairs),
                "ties": sum(r["resolved"] == b["resolved"] for r, b in pairs),
            }
        groups[name] = arms
    bench.save(a.output / "comparison.json", {"complete_planned_matrix": report["complete_planned_matrix"], "groups": groups})


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("mode", choices=("prepare", "run", "report"))
    p.add_argument("--work", type=Path, default=Path("/private/tmp/smol-overhead"))
    p.add_argument("--output", type=Path, default=bench.ROOT / "measurements/authoring-modes")
    p.add_argument("--model", default="gpt-6.1-sol")
    p.add_argument("--effort", default="medium")
    p.add_argument("--repeats", type=int, default=2)
    p.add_argument("--repairs", type=int, default=2)
    p.add_argument("--timeout", type=int, default=300)
    p.add_argument("--tasks", nargs="+", choices=TASKS)
    p.add_argument("--arms", nargs="+", choices=("go", "smol-commas", "smol-go"))
    a = p.parse_args()
    a.work, a.output = a.work.resolve(), a.output.resolve()
    a.guide_names = list(GUIDES)
    if a.repeats < 1 or a.repairs < 0:
        p.error("repeats must be positive and repairs nonnegative")
    configure()
    if a.mode == "prepare":
        a.output.mkdir(parents=True, exist_ok=True)
        for name, file in GUIDES.items():
            shutil.copyfile(bench.ROOT / "scripts" / file, a.output / (name + ".md"))
    {"prepare": bench.prepare, "run": bench.run, "report": bench.report}[a.mode](a)
    if a.mode in ("run", "report"):
        summarize(a)


if __name__ == "__main__":
    main()
