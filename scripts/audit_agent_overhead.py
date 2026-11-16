#!/usr/bin/env python3
"""Verify raw overhead evidence; optionally replay patches and canonical formatting."""
import argparse
import json
from pathlib import Path
import shutil
import tempfile

import measure_agent_overhead as bench


def audit(a):
    manifest = json.loads((a.output / "manifest.json").read_text())
    fixtures = json.loads((a.output / "benchmark-fixtures.json").read_text())
    assert bench.sha((a.output / "benchmark-fixtures.json").read_bytes()) == manifest["benchmark_fixtures_sha256"]
    for row in fixtures["rows"]:
        task = manifest["tasks"][row["instance_id"]]
        assert bench.sha(row["patch"]) == task["gold_patch_sha256"]
        assert bench.sha(row["test_patch"]) == task["test_patch_sha256"]
        assert bench.sha(row["problem_statement"]) == task["problem_statement_sha256"]
    report = json.loads((a.output / "report.json").read_text())
    checks, replays = [], []
    for record in report["records"]:
        task_id, arm, repeat = record["task"], record["arm"], record["repeat"]
        out = a.output / "runs" / task_id / f"r{repeat}-{arm}"
        initial, counts = bench.make_prompt(a, task_id, arm)
        assert counts == record["counts"]
        assert initial == (out / "initial-prompt.txt").read_text()
        usage = dict(input_tokens=0, cached_input_tokens=0, output_tokens=0)
        for attempt in record["attempts"]:
            trial = out / f"attempt-{attempt['attempt']}"
            prompt, response = (trial / "prompt.txt").read_text(), (trial / "response.json").read_text()
            assert bench.sha(prompt) == attempt["prompt_sha256"]
            assert bench.sha(response) == attempt["response_sha256"]
            assert bench.tokens(prompt) == attempt["prompt_tokens"]
            assert bench.tokens(response) == attempt["response_tokens"]
            events = [json.loads(line) for line in (trial / "events.jsonl").read_text().splitlines()]
            turns = [e for e in events if e.get("type") == "turn.completed"]
            assert len(turns) == 1 and turns[0]["usage"] == attempt["usage"]
            assert all(e.get("item", {}).get("type") in ("reasoning", "agent_message")
                       for e in events if e.get("type") == "item.completed")
            for k in usage:
                usage[k] += attempt["usage"].get(k, 0)
            if attempt["stage"] == "tests":
                tests = json.loads((trial / "tests.json").read_text())
                expected = set(manifest["tasks"][task_id]["fail_to_pass"] + manifest["tasks"][task_id]["pass_to_pass"])
                resolved = tests["exit_code"] == 0 and expected.issubset(tests["passed"])
                assert resolved == attempt["resolved"]
                assert not attempt["feedback"], "Held-out feedback must not be sent to model"
            assert attempt == json.loads((trial / "result.json").read_text())
        assert usage == record["usage"]
        assert record["resolved"] == record["attempts"][-1]["resolved"]
        checks.append({"task": task_id, "arm": arm, "repeat": repeat, "verified": True})
        if not a.replay:
            continue
        print(f"replay {task_id} {arm} r{repeat}", flush=True)
        trial = out / f"attempt-{len(record['attempts'])-1}"
        replay_out = out / "replay"
        replay_out.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(prefix="audit-", dir=a.work) as tmp:
            repo = Path(tmp) / "repo"
            shutil.copytree(a.work / "smol-bases" / task_id, repo)
            edits = json.loads((trial / "response.json").read_text())["edits"]
            touched = bench.edit_candidate(a, task_id, arm, edits, repo)
            formatted = True
            if arm != "go":
                fmt = bench.translate(a, repo, ["fmt", *touched])
                bench.save(replay_out / "format.json", fmt)
                formatted = fmt["exit_code"] == 0
                if formatted:
                    check = bench.translate(a, repo, ["fmt", "-check", *touched])
                    bench.save(replay_out / "format-check.json", check)
                    formatted = check["exit_code"] == 0
                lower = bench.translate(a, repo, ["lower", *touched])
                bench.save(replay_out / "lower.json", lower)
                assert lower["exit_code"] == 0
            bench.apply_patch_file(repo, (a.output / "tasks" / task_id / "test.patch").read_text(), bench.env_for(a))
            tests = bench.go_tests(a, repo, bench.TASKS[task_id])
            bench.save(replay_out / "tests.json", tests)
            expected = set(manifest["tasks"][task_id]["fail_to_pass"] + manifest["tasks"][task_id]["pass_to_pass"])
            resolved = tests["exit_code"] == 0 and expected.issubset(tests["passed"])
            assert resolved == record["resolved"], "Replay changed outcome"
            supplement = None
            if task_id == "gin-gonic__gin-1957":
                supplement = bench.go_tests(a, repo, {"package": "./binding", "test_regex": "^TestHeaderBinding$"})
            elif task_id == "gin-gonic__gin-4003":
                supplement = bench.go_tests(a, repo, bench.TASKS[task_id], "Test(NoRoute|MiddlewareNoRoute|EngineHandleMethodNotAllowed)")
            if supplement:
                bench.save(replay_out / "supplemental-tests.json", supplement)
            replays.append({"task": task_id, "arm": arm, "repeat": repeat,
                            "canonical_format_passed": formatted, "resolved": resolved,
                            "supplemental_exit_code": supplement["exit_code"] if supplement else None,
                            "supplemental_passed": supplement["passed"] if supplement else []})
    expected_runs = {(tid, arm, repeat) for tid in bench.TASKS for arm in bench.ARMS for repeat in (1, 2)}
    actual = {(r["task"], r["arm"], r["repeat"]) for r in checks}
    assert expected_runs == actual, "Incomplete pilot matrix"
    result = {"all_raw_evidence_verified": True, "episodes": len(checks),
              "api_requests": sum(len(r["attempts"]) for r in report["records"]),
              "checks": checks, "replays": replays}
    bench.save(a.output / ("audit-replay.json" if a.replay else "audit.json"), result)
    print(json.dumps({k: v for k, v in result.items() if k not in ("checks", "replays")}, indent=2))


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--output", type=Path, default=bench.ROOT / "measurements/agent-overhead")
    p.add_argument("--work", type=Path, default=Path("/private/tmp/smol-overhead"))
    p.add_argument("--replay", action="store_true")
    p.add_argument("--profile", choices=("original", "authoring"), default="original")
    a = p.parse_args()
    if a.profile == "authoring":
        from measure_authoring_modes import configure
        configure()
    audit(a)
