#!/usr/bin/env python3
"""Paired, localized SWE-bench Go/Smol patch-writing experiment.

prepare downloads public pinned sources and tests, checks failing base/passing
oracle, and makes equivalent input bundles. run makes real model requests via
the authenticated Codex CLI. report only reads recorded evidence. Requires
tiktoken. All credentials and dependency/build caches stay outside the repo.
"""
import argparse
import copy
import hashlib
import io
import json
import math
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import time
import urllib.request

import tiktoken

ROOT = Path(__file__).resolve().parents[1]
ENC = tiktoken.get_encoding("o200k_base")
TASKS = {
    "gin-gonic__gin-1957": {
        "files": ["context.go", "binding/binding.go", "binding/query.go", "binding/form_mapping.go"],
        "new": ["binding/header.go"], "package": ".", "test_regex": "^TestContext.*Bind",
    },
    "gin-gonic__gin-4003": {
        "files": ["gin.go"], "new": [], "package": ".",
        "test_regex": "TestMethodNotAllowedNoRoute",
    },
    "caddyserver__caddy-6051": {
        "files": ["caddyconfig/caddyfile/lexer.go"], "new": [],
        "package": "./caddyconfig/caddyfile", "test_regex": ".",
    },
}
ARMS = ["go", "smol-none", "smol-brief", "smol-guide"]
SCHEMA = {
    "type": "object", "properties": {"edits": {"type": "array", "items": {
        "type": "object", "properties": {"path": {"type": "string"},
        "old": {"type": "string"}, "new": {"type": "string"}},
        "required": ["path", "old", "new"], "additionalProperties": False,
    }}}, "required": ["edits"], "additionalProperties": False,
}


def sha(data):
    if isinstance(data, str):
        data = data.encode()
    return hashlib.sha256(data).hexdigest()


def tokens(text):
    return len(ENC.encode(text, disallowed_special=()))


def save(path, obj):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(obj, indent=2) + "\n")


def fetch(url):
    return urllib.request.urlopen(urllib.request.Request(url, headers={
        "User-Agent": "smolang-overhead-experiment"}), timeout=120).read()


def env_for(a):
    return dict(os.environ, GOCACHE=str(a.work / "gocache"),
                GOMODCACHE=str(a.work / "modcache"), GOTOOLCHAIN="local")


def command(argv, cwd, env, timeout=300):
    start = time.monotonic()
    try:
        p = subprocess.run([str(x) for x in argv], cwd=cwd, env=env,
                           capture_output=True, text=True, timeout=timeout)
        return {"command": [str(x) for x in argv], "exit_code": p.returncode,
                "stdout": p.stdout, "stderr": p.stderr,
                "elapsed_seconds": round(time.monotonic()-start, 3)}
    except subprocess.TimeoutExpired as e:
        return {"command": [str(x) for x in argv], "exit_code": 124,
                "stdout": (e.stdout or b"").decode() if isinstance(e.stdout, bytes) else e.stdout or "",
                "stderr": "TIMEOUT", "elapsed_seconds": round(time.monotonic()-start, 3)}


def require_ok(r):
    if r["exit_code"]:
        raise RuntimeError(json.dumps(r)[-8000:])
    return r


def translate(a, repo, args):
    # Historical benchmark modules predate the CLI's minimum language declaration.
    # Raise only the translator's view; compile/test with the untouched original.
    mod = repo / "go.mod"
    original = mod.read_bytes()
    text = original.decode()
    match = re.search(r"(?m)^go (\d+)\.(\d+)(?:\.\d+)?$", text)
    if match and tuple(map(int, match.groups())) < (1, 22):
        text = text[:match.start()] + "go 1.22" + text[match.end():]
    elif not match:
        text += "\ngo 1.22\n"
    try:
        mod.write_text(text)
        return command([a.work / "smol", *args], repo, env_for(a))
    finally:
        mod.write_bytes(original)


def go_tests(a, repo, task, regex=None):
    go, env = "go", env_for(a)
    if task.get("test_toolchain"):
        root = a.work / "toolchains" / task["test_toolchain"] / "go"
        go = root / "bin/go"
        env.update(GOROOT=str(root), GOCACHE=str(a.work / ("gocache-" + task["test_toolchain"])))
    r = command([go, "test", "-count=1", "-json", task["package"],
                 "-run", task["test_regex"] if regex is None else regex], repo, env, 300)
    events = []
    for line in r["stdout"].splitlines():
        try:
            events.append(json.loads(line))
        except ValueError:
            pass
    r["passed"] = [e["Test"] for e in events if e.get("Action") == "pass" and "Test" in e]
    r["failed"] = [e["Test"] for e in events if e.get("Action") == "fail" and "Test" in e]
    r["skipped"] = [e["Test"] for e in events if e.get("Action") == "skip" and "Test" in e]
    return r


def apply_patch_file(repo, patch, env):
    # git apply works on archive directories too; no future git history is present.
    p = subprocess.run(["git", "apply", "--whitespace=nowarn", "-"], cwd=repo,
                       input=patch, text=True, capture_output=True, env=env)
    if p.returncode:
        raise RuntimeError(p.stderr)


def prepare(a):
    a.work.mkdir(parents=True, exist_ok=True)
    a.output.mkdir(parents=True, exist_ok=True)
    require_ok(command(["go", "build", "-o", a.work / "smol", "./cmd/smol"], ROOT, env_for(a)))
    fixture_path = a.output / "benchmark-fixtures.json"
    dataset_path = a.work / "multilingual.json"
    if fixture_path.exists():
        frozen = json.loads(fixture_path.read_text())
        rows, revision = frozen["rows"], frozen["revision_observed"]
    elif not dataset_path.exists():
        rows = []
        for offset in (0, 100, 200):
            url = ("https://datasets-server.huggingface.co/rows?dataset=SWE-bench%2F"
                   "SWE-bench_Multilingual&config=default&split=test&offset=" + str(offset) + "&length=100")
            rows += [r["row"] for r in json.loads(fetch(url))["rows"]]
        save(dataset_path, rows)
    if not fixture_path.exists():
        rows = json.loads(dataset_path.read_text())
        revision = json.loads(fetch("https://huggingface.co/api/datasets/SWE-bench/SWE-bench_Multilingual"))["sha"]
        save(fixture_path, {"revision_observed": revision,
                           "rows": [r for r in rows if r["instance_id"] in TASKS]})
    manifest = {"benchmark": "SWE-bench/SWE-bench_Multilingual", "dataset_revision_observed": revision,
                "benchmark_fixtures_sha256": sha(fixture_path.read_bytes()),
                "translator_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
                "translator_binary_sha256": sha((a.work / "smol").read_bytes()),
                "go_version": subprocess.check_output(["go", "version"], text=True).strip(),
                "codex_version": subprocess.check_output(["codex", "--version"], text=True).strip(),
                "tasks": {}}
    if getattr(a, "tasks", None) and (a.output / "manifest.json").exists():
        previous = json.loads((a.output / "manifest.json").read_text())
        for key in ("benchmark_fixtures_sha256", "translator_binary_sha256"):
            assert previous[key] == manifest[key], "Cannot mix preparation inputs"
        manifest["tasks"] = previous["tasks"]
    for task_id, config in TASKS.items():
        if getattr(a, "tasks", None) and task_id not in a.tasks:
            continue
        print("prepare " + task_id, flush=True)
        row = next(r for r in rows if r["instance_id"] == task_id)
        repo = a.work / "bases" / task_id
        out = a.output / "tasks" / task_id
        out.mkdir(parents=True, exist_ok=True)
        if not repo.exists():
            url = f"https://codeload.github.com/{row['repo']}/tar.gz/{row['base_commit']}"
            data = fetch(url)
            archive = tarfile.open(fileobj=io.BytesIO(data), mode="r:gz")
            repo.mkdir(parents=True)
            # Strip the archive root, rejecting links and traversal.
            for entry in archive.getmembers():
                parts = Path(entry.name).parts[1:]
                if not parts or entry.isdir():
                    continue
                if not entry.isfile() or ".." in parts:
                    raise RuntimeError("Unexpected archive entry: " + entry.name)
                path = repo.joinpath(*parts)
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(archive.extractfile(entry).read())
            (out / "archive.sha256").write_text(sha(data) + "\n")
        require_ok(command(["go", "mod", "download"], repo, env_for(a), 300))
        task = dict(config, repo=row["repo"], base_commit=row["base_commit"],
                    fail_to_pass=row["FAIL_TO_PASS"], pass_to_pass=row["PASS_TO_PASS"],
                    problem_statement_sha256=sha(row["problem_statement"]),
                    test_patch_sha256=sha(row["test_patch"]), gold_patch_sha256=sha(row["patch"]))
        (out / "problem.txt").write_text(row["problem_statement"])
        (out / "test.patch").write_text(row["test_patch"])
        for candidate in ("LICENSE", "LICENSE.txt"):
            if (repo / candidate).exists():
                shutil.copyfile(repo / candidate, out / "UPSTREAM-LICENSE")
                break
        # Local environment gate: official tests must fail before and pass after gold.
        for kind in ("base", "gold"):
            with tempfile.TemporaryDirectory(prefix="verify-", dir=a.work) as tmp:
                dest = Path(tmp) / "repo"
                shutil.copytree(repo, dest)
                apply_patch_file(dest, row["test_patch"], env_for(a))
                if kind == "gold":
                    apply_patch_file(dest, row["patch"], env_for(a))
                r = go_tests(a, dest, task)
                save(out / (kind + "-tests.json"), r)
                if kind == "gold":
                    require_ok(r)
                    assert set(task["fail_to_pass"]).issubset(r["passed"]), "Missing gold test"
                else:
                    assert r["exit_code"] != 0, "Base unexpectedly passes"
                    # A missing new API causes build failure on a feature task.
                    assert r["failed"] or "undefined" in r["stderr"] + r["stdout"], "Base failed for environment reasons"
        # Lift whole containing directories for sibling declaration context.
        smol_repo = a.work / "smol-bases" / task_id
        if smol_repo.exists():
            shutil.rmtree(smol_repo)
        shutil.copytree(repo, smol_repo)
        parents = sorted({str(Path(f).parent) for f in config["files"]})
        all_sources = []
        for parent in parents:
            all_sources.extend(str(p.relative_to(smol_repo)) for p in (smol_repo / parent).glob("*.go"))
        require_ok(translate(a, smol_repo, ["lift", *sorted(all_sources)]))
        # Remove notes from selected files only; tests retain comments/Example assertions.
        for f in config["files"]:
            note = (smol_repo / f).with_suffix(".smol.notes")
            note.unlink(missing_ok=True)
        selected = [str(Path(f).with_suffix(".smol")) for f in config["files"]]
        require_ok(translate(a, smol_repo, ["lower", *selected]))
        # Every representation starts from these same normalized, comment-free Go tokens.
        sources = {"go": {}, "smol": {}, "go_original": {}}
        for f, s in zip(config["files"], selected):
            sources["go"][f] = (smol_repo / f).read_text()
            sources["smol"][s] = (smol_repo / s).read_text()
            sources["go_original"][f] = (repo / f).read_text()
        sources["smol_mod"] = (smol_repo / "smol.mod").read_text() if (smol_repo / "smol.mod").exists() else ""
        save(out / "sources.json", sources)
        # Check lowering does not accidentally fix the task or break the environment.
        with tempfile.TemporaryDirectory(prefix="roundtrip-", dir=a.work) as tmp:
            dest = Path(tmp) / "repo"
            shutil.copytree(smol_repo, dest)
            r = go_tests(a, dest, task, "^$")
            save(out / "roundtrip-build.json", r)
            require_ok(r)
        manifest["tasks"][task_id] = task
        save(a.output / "manifest.json", manifest)
        print("ready " + task_id, flush=True)
    for name in ("brief", "guide"):
        shutil.copyfile(ROOT / "scripts" / ("overhead_" + name + ".md"), a.output / (name + ".md"))
    shutil.copyfile(ROOT / "SPEC.md", a.output / "spec-reference.md")
    save(a.output / "response-schema.json", SCHEMA)


def bundle(sources):
    return "".join(f"\n<file path={json.dumps(path)}>\n{text}</file>\n" for path, text in sources.items())


def make_prompt(a, task_id, arm):
    task = TASKS[task_id]
    source = json.loads((a.output / "tasks" / task_id / "sources.json").read_text())
    smol = arm != "go"
    files = source["smol" if smol else "go"]
    allowed_new = [str(Path(p).with_suffix(".smol")) if smol else p for p in task["new"]]
    common = ("Fix the repository issue below. This is a localized patch-writing evaluation. "
              "Do not use tools; all editable context is provided. Return only JSON matching the schema: "
              '{"edits":[{"path":"relative path","old":"exact unique substring","new":"replacement"}]}. '
              "Edits apply in order. Each old string must occur exactly once. "
              "For an allowed new file, old must be empty and new must contain its complete contents. "
              "Do not edit tests or unrelated behavior. Only provided files and the allowed new files may change.\n")
    representation = "Source representation: " + ("Smol v0.2. Write all edits in Smol.\n" if smol else "Go. Write all edits in Go.\n")
    guide = ""
    if arm in ("smol-brief", "smol-guide"):
        guide = (a.output / (arm.split("-")[1] + ".md")).read_text()
    extra = "\n<language-guide>\n" + guide + "</language-guide>\n" if guide else ""
    context = bundle(files)
    import_table = "\n<smol.mod>\n" + source["smol_mod"] + "</smol.mod>\n" if smol else ""
    issue = (a.output / "tasks" / task_id / "problem.txt").read_text()
    prefix = common + representation + extra + "Allowed new files: " + json.dumps(allowed_new) + "\n"
    prompt = prefix + "\n<issue>\n" + issue + "</issue>\n" + import_table + context
    return prompt, {"prompt_tokens": tokens(prompt), "instruction_tokens": tokens(guide),
                    "source_tokens": tokens(context), "import_table_tokens": tokens(import_table),
                    "original_go_source_tokens": tokens(bundle(source["go_original"])),
                    "guide_sha256": sha(guide), "prompt_sha256": sha(prompt)}


def edit_candidate(a, task_id, arm, edits, repo):
    task = TASKS[task_id]
    smol = arm != "go"
    allowed = task["files"] + task["new"]
    if smol:
        allowed = [str(Path(f).with_suffix(".smol")) for f in allowed]
    touched = []
    for edit in edits:
        path = edit["path"]
        if path not in allowed:
            raise ValueError("File not allowed: " + path)
        dest = repo / path
        old, new = edit["old"], edit["new"]
        if old == "":
            if dest.exists():
                raise ValueError("Empty old is only valid for new files: " + path)
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_text(new)
        else:
            current = dest.read_text()
            n = current.count(old)
            if n != 1:
                raise ValueError(f"old must match exactly once in {path}; found {n}")
            dest.write_text(current.replace(old, new, 1))
        touched.append(path)
    if not touched:
        raise ValueError("No edits submitted")
    return sorted(set(touched))


def evaluate(a, task_id, arm, response, out):
    task = TASKS[task_id]
    with tempfile.TemporaryDirectory(prefix="candidate-", dir=a.work) as tmp:
        repo = Path(tmp) / "repo"
        shutil.copytree(a.work / "smol-bases" / task_id, repo)
        # Reset all selected Go sources to the comment-free baseline in both arms.
        try:
            edits = json.loads(response)["edits"]
            touched = edit_candidate(a, task_id, arm, edits, repo)
        except (ValueError, KeyError, TypeError, OSError) as e:
            return {"stage": "patch", "feedback": str(e), "resolved": False}
        if arm != "go":
            r = translate(a, repo, ["lower", *touched])
            save(out / "lower.json", r)
            if r["exit_code"]:
                return {"stage": "lower", "feedback": (r["stderr"] + r["stdout"])[-6000:], "resolved": False}
        r = go_tests(a, repo, task, "^$")
        save(out / "build.json", r)
        if r["exit_code"]:
            return {"stage": "build", "feedback": (r["stderr"] + r["stdout"])[-6000:], "resolved": False}
        # Persist generated Go/Smol, and canonicality separately from correctness.
        payload = {}
        for path in touched:
            go_path = str(Path(path).with_suffix(".go"))
            payload[go_path] = (repo / go_path).read_text()
            if arm != "go":
                payload[path] = (repo / path).read_text()
        save(out / "candidate-sources.json", payload)
        if arm != "go":
            fmt = translate(a, repo, ["fmt", "-check", *touched])
            save(out / "format.json", fmt)
        apply_patch_file(repo, (a.output / "tasks" / task_id / "test.patch").read_text(), env_for(a))
        r = go_tests(a, repo, task)
        save(out / "tests.json", r)
        manifest = json.loads((a.output / "manifest.json").read_text())["tasks"][task_id]
        expected = set(manifest["fail_to_pass"] + manifest["pass_to_pass"])
        resolved = r["exit_code"] == 0 and expected.issubset(set(r["passed"]))
        return {"stage": "tests", "resolved": resolved, "passed": r["passed"], "failed": r["failed"],
                "missing_expected_tests": sorted(expected-set(r["passed"])),
                "canonical_before_format": fmt["exit_code"] == 0 if arm != "go" else None,
                "feedback": ""}  # Held-out test feedback is NEVER sent to the model.


def run(a):
    original_home = Path(os.environ.get("CODEX_HOME", str(Path.home() / ".codex")))
    # A fresh temporary home excludes user/project skills, memories and MCP tools.
    with tempfile.TemporaryDirectory(prefix="codex-overhead-", dir=a.work) as tmp:
        home, work = Path(tmp) / "home", Path(tmp) / "empty"
        home.mkdir(mode=0o700)
        work.mkdir()
        shutil.copyfile(original_home / "auth.json", home / "auth.json")
        (home / "auth.json").chmod(0o600)
        (home / "config.toml").write_text('model = ' + json.dumps(a.model) + '\nmodel_reasoning_effort = "' + a.effort + '"\n')
        env = dict(os.environ, CODEX_HOME=str(home))
        selected_tasks = a.tasks or list(TASKS)
        for repeat in range(1, a.repeats + 1):
            for i, task_id in enumerate(selected_tasks):
                order = (a.arms or ARMS).copy()
                # Counterbalance across tasks and repetitions, independent of results.
                shift = (list(TASKS).index(task_id) + repeat - 1) % len(order)
                order = order[shift:] + order[:shift]
                for arm in order:
                    out = a.output / "runs" / task_id / (f"r{repeat}-" + arm)
                    prompt, counts = make_prompt(a, task_id, arm)
                    if (out / "result.json").exists():
                        previous = json.loads((out / "result.json").read_text())
                        if previous["model"] != a.model or previous["effort"] != a.effort or previous["counts"] != counts:
                            raise RuntimeError("Existing run uses different settings/context; choose a new --output: " + str(out))
                        continue
                    out.mkdir(parents=True, exist_ok=True)
                    (out / "initial-prompt.txt").write_text(prompt)
                    attempts = []
                    for attempt in range(a.repairs + 1):
                        trial = out / f"attempt-{attempt}"
                        trial.mkdir(exist_ok=True)
                        (trial / "prompt.txt").write_text(prompt)
                        argv = ["codex", "exec", "--ephemeral", "--skip-git-repo-check", "--json",
                                "-s", "read-only", "-C", str(work), "--output-schema",
                                str(a.output / "response-schema.json"), "-o", str(trial / "response.json"), "-"]
                        print(f"request {task_id} {arm} r{repeat} attempt {attempt}", flush=True)
                        started = time.monotonic()
                        with (trial / "events.jsonl").open("w") as stdout, (trial / "stderr.txt").open("w") as stderr:
                            proc = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=stdout, stderr=stderr, env=env, text=True)
                            try:
                                proc.communicate(prompt, timeout=a.timeout)
                            except subprocess.TimeoutExpired:
                                proc.kill()
                                proc.communicate()
                        generation_seconds = round(time.monotonic()-started, 3)
                        events = []
                        for line in (trial / "events.jsonl").read_text().splitlines():
                            try:
                                events.append(json.loads(line))
                            except ValueError:
                                pass
                        turns = [e for e in events if e.get("type") == "turn.completed"]
                        tool_calls = [e for e in events if e.get("type") == "item.completed" and
                                      e.get("item", {}).get("type") not in ("agent_message", "reasoning")]
                        if proc.returncode or len(turns) != 1 or tool_calls:
                            raise RuntimeError("Invalid model run; inspect " + str(trial))
                        response = (trial / "response.json").read_text()
                        outcome = evaluate(a, task_id, arm, response, trial)
                        record = {"attempt": attempt, "usage": turns[0]["usage"],
                                  "elapsed_seconds": round(time.monotonic()-started, 3),
                                  "generation_seconds": generation_seconds,
                                  "prompt_tokens": tokens(prompt), "response_tokens": tokens(response),
                                  "prompt_sha256": sha(prompt), "response_sha256": sha(response), **outcome}
                        save(trial / "result.json", record)
                        attempts.append(record)
                        print(f"result {task_id} {arm} r{repeat}: {outcome['stage']} resolved={outcome['resolved']}", flush=True)
                        if outcome["stage"] == "tests":
                            break
                        # Fresh stateless request includes original context and all repair history.
                        # Every submitted patch must apply to the original source.
                        prompt += ("\n<previous-response>\n" + response + "\n</previous-response>\n"
                                   "<validation-error>\n" + outcome["feedback"] + "\n</validation-error>\n"
                                   "Repair the patch. Return a complete replacement JSON patch against the ORIGINAL files, "
                                   "including all still-needed edits. Do not use tools.\n")
                    save(out / "result.json", {"task": task_id, "arm": arm, "repeat": repeat,
                         "model": a.model, "effort": a.effort, "counts": counts, "attempts": attempts,
                         "resolved": attempts[-1]["resolved"], "first_pass_resolved": attempts[0]["resolved"],
                         "usage": {k: sum(x["usage"].get(k, 0) for x in attempts)
                                   for k in ("input_tokens", "cached_input_tokens", "output_tokens")}})
    report(a)


def report(a):
    records = [json.loads(p.read_text()) for p in sorted((a.output / "runs").glob("*/*/result.json"))]
    comparisons = []
    guide_tokens = {name: tokens((a.output / (name + ".md")).read_text())
                    for name in getattr(a, "guide_names", ("brief", "guide"))}
    guide_tokens["full_spec"] = tokens((a.output / "spec-reference.md").read_text())
    source_arm = next(arm for arm in ARMS if arm != "go")
    for task_id in TASKS:
        if not (a.output / "tasks" / task_id / "sources.json").exists():
            continue
        counts = {arm: make_prompt(a, task_id, arm)[1] for arm in ARMS}
        g, s = counts["go"], counts[source_arm]
        gross = g["source_tokens"]-s["source_tokens"]
        comparisons.append({"task": task_id, "counts": counts,
             "syntax_tokens_saved": gross,
             "syntax_savings_percent": 100*gross/g["source_tokens"],
             "smol_extra_context_tokens": s["import_table_tokens"],
             "net_prompt_savings": {arm: g["prompt_tokens"]-c["prompt_tokens"] for arm,c in counts.items()},
             "break_even_loads": {name: math.ceil(n/(gross-s["import_table_tokens"])) if gross>s["import_table_tokens"] else None
                                  for name,n in guide_tokens.items()}})
    arms = {}
    for arm in ARMS:
        rr = [r for r in records if r["arm"] == arm]
        usage = {k: sum(r["usage"][k] for r in rr) for k in ("input_tokens", "cached_input_tokens", "output_tokens")}
        arms[arm] = {"runs": len(rr), "resolved": sum(r["resolved"] for r in rr),
                     "first_pass_resolved": sum(r["first_pass_resolved"] for r in rr),
                     "first_pass_builds": sum(r["attempts"][0]["stage"] == "tests" for r in rr),
                     "repair_requests": sum(len(r["attempts"])-1 for r in rr), **usage,
                     "uncached_input_tokens": usage["input_tokens"]-usage["cached_input_tokens"],
                     "input_plus_output_tokens": usage["input_tokens"]+usage["output_tokens"]}
        arms[arm]["first_attempt_input_tokens"] = sum(r["attempts"][0]["usage"]["input_tokens"] for r in rr)
        arms[arm]["first_attempt_output_tokens"] = sum(r["attempts"][0]["usage"]["output_tokens"] for r in rr)
        arms[arm]["repair_input_tokens"] = usage["input_tokens"]-arms[arm]["first_attempt_input_tokens"]
        arms[arm]["repair_output_tokens"] = usage["output_tokens"]-arms[arm]["first_attempt_output_tokens"]
    baseline = arms["go"]["input_plus_output_tokens"]
    for arm, values in arms.items():
        if baseline and values["runs"] == arms["go"]["runs"]:
            values["total_token_change_vs_go_percent"] = 100*(values["input_plus_output_tokens"]-baseline)/baseline
    source_totals = {"go": sum(c["counts"]["go"]["source_tokens"] for c in comparisons),
                     "smol": sum(c["counts"][source_arm]["source_tokens"] for c in comparisons),
                     "original_go": sum(c["counts"]["go"]["original_go_source_tokens"] for c in comparisons),
                     "smol_import_table": sum(c["smol_extra_context_tokens"] for c in comparisons)}
    source_totals["syntax_savings_percent"] = 100*(source_totals["go"]-source_totals["smol"])/source_totals["go"]
    result = {"method": "Localized patch generation; no tools; upstream held-out tests; compile-only repair feedback.",
              "tokenizer": "o200k_base", "tiktoken_version": tiktoken.__version__,
              "codex_version": json.loads((a.output / "manifest.json").read_text())["codex_version"],
              "instruction_tokens": guide_tokens, "comparisons": comparisons, "arms": arms,
              "source_totals_once_per_task": source_totals,
              "api_requests": sum(len(r["attempts"]) for r in records),
              "complete_planned_matrix": {(r["task"],r["arm"],r["repeat"]) for r in records} ==
                                         {(tid,arm,repeat) for tid in TASKS for arm in ARMS for repeat in (1,2)},
              "records": records}
    save(a.output / "report.json", result)
    print(json.dumps({"instruction_tokens": guide_tokens, "arms": arms}, indent=2))



if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("mode", choices=("prepare", "run", "report"))
    p.add_argument("--work", type=Path, default=Path("/private/tmp/smol-overhead"))
    p.add_argument("--output", type=Path, default=ROOT / "measurements" / "agent-overhead")
    p.add_argument("--model", default="gpt-6.1-sol")
    p.add_argument("--effort", default="medium")
    p.add_argument("--repeats", type=int, default=2)
    p.add_argument("--repairs", type=int, default=2)
    p.add_argument("--timeout", type=int, default=300)
    p.add_argument("--tasks", nargs="+", choices=TASKS)
    p.add_argument("--arms", nargs="+", choices=ARMS)
    a = p.parse_args()
    a.work, a.output = a.work.resolve(), a.output.resolve()
    if a.repeats < 1 or a.repairs < 0:
        p.error("repeats must be positive and repairs nonnegative")
    {"prepare": prepare, "run": run, "report": report}[a.mode](a)
