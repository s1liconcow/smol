#!/usr/bin/env python3
"""Summarize saved conformance evidence; fail if any required check is missing or fails."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def report(root):
    def read(name):
        return json.loads((root / name).read_text())

    events = [json.loads(line) for line in (root / "tests.jsonl").read_text().splitlines()]
    passed = {e["Test"] for e in events if e.get("Action") == "pass" and e.get("Test")}
    forms = [{"section": "6." + str(section), "requirement": variant,
              "test": "TestRequiredForms/6." + str(section) + "/" + variant}
             for section in range(1, 14) for variant in ("fold", "near", "comment")]
    for form in forms:
        form["status"] = "PASS" if form["test"] in passed else "FAIL"
    tests = {action: sum(e.get("Action") == action and bool(e.get("Test")) for e in events)
             for action in ("pass", "fail", "skip")}
    corpus = {name: read(name + "/report.json") for name in ("stdlib", "module")}
    behavior = {name: read(name + "/report.json") for name in ("lowered", "lowered-module")}
    probes = read("probes.json")
    checks = {
        "unit_and_cli": bool(passed) and not tests["fail"] and not any(e.get("Action") == "fail" for e in events),
        "mandatory_forms": all(f["status"] == "PASS" for f in forms),
        "corpus_L1_L4": all(r["exit_code"] == 0 for r in corpus.values()),
        "lowered_behavior": all(len(r["runs"]) == 2 and all(x["exit_code"] == 0 for x in r["runs"]) for r in behavior.values()),
        "behavior_covers_corpus": len(behavior["lowered"]["sources"]) == corpus["stdlib"]["passed_files"]
            and len(behavior["lowered-module"]["sources"]) == corpus["module"]["passed_files"],
        "probes": probes["failed"] == 0,
        "vet": read("vet.json")["exit_code"] == 0,
    }
    inputs = [Path("SPEC.md"), Path("go.mod"), *Path("internal").rglob("*.go"),
              *Path("cmd").rglob("*.go"), *Path("scripts").glob("*.go"), *Path("scripts").glob("*.py")]
    result = {
        "spec": "SPEC.md v0.2, with approved lossless-expression-boundary clarification in section 6.8",
        "verdict": "PASS" if all(checks.values()) else "FAIL",
        "scope": "Section 12 corpus gate for " + corpus["stdlib"]["go_version"] + ", active files; behavioral test flags and skips are recorded below.",
        "checks": checks, "unit_and_cli_cases": tests, "required_form_cases": forms,
        "corpus": corpus, "probes": {k: probes[k] for k in ("passed", "failed")},
        "lowered_behavior": {name: r["runs"] for name, r in behavior.items()},
        "source_sha256": {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(inputs)},
        "uplift": read("uplift.json")["uplift"],
        "limits": ["This is a corpus-based gate, not a proof for every Go program or every normative clause.",
                   "Inactive build variants, historical toolchains, and tests skipped by the recorded -short profile are not validated.",
                   "Test notes remain in both behavioral modes so Example output assertions run; production notes are removed in without-notes mode.",
                   "Token measurements cover encoding/json and the spec example; no model-accuracy or latency claim."]}
    (root / "summary.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"verdict": result["verdict"], "checks": checks, "tests": tests, "form_cases": len(forms)}, indent=2))
    return int(not all(checks.values()))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path("measurements/spec-v0.2-conformance"))
    parser.add_argument("--run-vet", action="store_true", help="refresh the vet result before summarizing")
    args = parser.parse_args()
    if args.run_vet:
        run = subprocess.run(["go", "vet", "./..."], capture_output=True, text=True)
        (args.output / "vet.log").write_text(run.stdout + run.stderr)
        (args.output / "vet.json").write_text(json.dumps({"command": ["go", "vet", "./..."], "exit_code": run.returncode}) + "\n")
    raise SystemExit(report(args.output))
