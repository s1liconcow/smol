#!/usr/bin/env python3
"""Run package tests against lowered sources using a Go overlay, leaving GOROOT intact."""
import argparse
import hashlib
import gzip
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import time


def objects(text):
    decoder = json.JSONDecoder()
    while text.strip():
        obj, end = decoder.raw_decode(text.lstrip())
        yield obj
        text = text.lstrip()[end:]


def validate(args):
    if os.environ.get("GOROOT"):
        # Runtime stack tests compare the environment with embedded physical
        # paths. macOS's /var -> /private/var alias must not differ between them.
        os.environ["GOROOT"] = str(Path(os.environ["GOROOT"]).resolve())
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    smol = str(args.smol.resolve())
    translator_sha256 = hashlib.sha256(Path(smol).read_bytes()).hexdigest()
    packages = list(objects(subprocess.check_output(
        ["go", "list", "-json", *args.packages], text=True)))
    for pkg in packages:
        if pkg.get("Goroot"):
            root = Path(pkg["Root"])
            license_path = next(p for p in (root / "LICENSE", root.parent / "LICENSE") if p.is_file())
            shutil.copyfile(license_path, out / "GO-LICENSE.txt")
            break
    records = []
    test_prefix = ["go"]
    if packages and all(p.get("Goroot") for p in packages):
        # Runtime compatibility defaults come from the main module. Testing
        # Go 1.26 std from this Go 1.22 module silently selects old defaults.
        test_prefix += ["-C", str(Path(packages[0]["Root"]) / "src")]
    with tempfile.TemporaryDirectory(prefix="smol-lowered-validation-") as tmp:
        work = Path(tmp)
        sources = []
        folders = []
        for index, pkg in enumerate(packages):
            folder = work / str(index)
            folder.mkdir()
            folders.append(str(folder))
            names = sorted(set(sum((pkg.get(k, []) for k in
                                   ("GoFiles", "CgoFiles", "TestGoFiles", "XTestGoFiles")), [])))
            for name in names:
                original = Path(pkg["Dir"]) / name
                local = folder / name
                local.write_bytes(original.read_bytes())
                sources.append({"original": original, "local": local,
                                "package": pkg["ImportPath"], "sha256": hashlib.sha256(local.read_bytes()).hexdigest(),
                                "production": name in pkg.get("GoFiles", []) + pkg.get("CgoFiles", [])})
        print(f"Lifting {len(sources)} files in {len(folders)} packages", flush=True)
        translate([smol, "lift", *folders])
        overlay = work / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {str(s["original"]): str(s["local"]) for s in sources}}))
        for mode in ("with-notes", "without-notes"):
            if mode == "without-notes":
                # Keep test notes: go test discovers Example expectations in
                # // Output: comments. Removing those would silently skip tests.
                for source in sources:
                    p = source["local"].with_suffix(".smol.notes")
                    if source["production"] and p.exists():
                        p.unlink()
            print(f"Lowering {mode}", flush=True)
            translate([smol, "lower", *folders])
            # Include the exact same file selection/markers as prepare_package.py.
            if mode == "without-notes":
                for pkg in packages:
                    if len(packages) > 20 and pkg["ImportPath"] != "encoding/json":
                        continue
                    bundle = "".join("// File: " + s["package"] + "/" + s["local"].name + "\n" + s["local"].read_text() + "\n"
                                     for s in sources if s["package"] == pkg["ImportPath"] and s["production"])
                    (out / (pkg["ImportPath"].replace("/", "-") + ".code-only.go.txt")).write_text(bundle)
            cmd = [*test_prefix, "test", "-count=1", "-json", *(["-short"] if args.short else []), "-overlay", str(overlay), *args.packages]
            print(f"Testing {mode}", flush=True)
            start = time.monotonic()
            run = subprocess.run(cmd, capture_output=True, text=True)
            if len(packages) > 20:
                with gzip.open(out / (mode + ".jsonl.gz"), "wt") as log:
                    log.write(run.stdout)
            else:
                (out / (mode + ".jsonl")).write_text(run.stdout)
            (out / (mode + ".stderr")).write_text(run.stderr)
            events = [json.loads(line) for line in run.stdout.splitlines() if line.startswith("{")]
            record = {"mode": mode, "command": ["<overlay>" if x == str(overlay) else x for x in cmd],
                      "exit_code": run.returncode, "elapsed_seconds": round(time.monotonic()-start, 3),
                      "passed_cases": sum(e.get("Action") == "pass" and bool(e.get("Test")) for e in events),
                      "failed_cases": [e.get("Package", "") + "/" + e.get("Test", "") for e in events if e.get("Action") == "fail"],
                      "skipped_cases": sum(e.get("Action") == "skip" and bool(e.get("Test")) for e in events),
                      "packages": [{"package": e["Package"], "status": e["Action"]} for e in events
                                   if e.get("Action") in ("pass", "fail", "skip") and not e.get("Test")]}
            records.append(record)
            print(json.dumps({k: record[k] for k in ("mode", "exit_code", "elapsed_seconds", "passed_cases", "skipped_cases")}
                             | {"failed_cases": len(record["failed_cases"])}), flush=True)
    report = {"go_version": subprocess.check_output(["go", "version"], text=True).strip(),
              "translator_sha256": translator_sha256,
              "go_environment": json.loads(subprocess.check_output(["go", "env", "-json", "GOROOT", "GOCACHE", "GOOS", "GOARCH"], text=True)),
              "method": "go test using an overlay of actively selected GoFiles, CgoFiles, TestGoFiles and XTestGoFiles after lift/lower",
              "GODEBUG": os.environ.get("GODEBUG", ""), "short": args.short, "runs": records,
              "sources": [{"path": str(s["original"]), "sha256": s["sha256"]} for s in sources],
              "limits": "Selection and -short are recorded in the commands; GOROOT unchanged. Without-notes mode removes production notes only; test notes remain so Example output assertions still run."}
    (out / "report.json").write_text(json.dumps(report, indent=2)+"\n")
    return int(any(r["exit_code"] for r in records))


def translate(command):
    run = subprocess.run(command, capture_output=True, text=True)
    if run.returncode:
        raise RuntimeError(run.stdout + run.stderr)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("packages", nargs="*", default=["fmt", "encoding/json", "go/parser"])
    parser.add_argument("--smol", type=Path, default=Path("bin/smol"))
    parser.add_argument("--short", action="store_true", help="use Go's standard short test mode")
    parser.add_argument("--output", type=Path, default=Path("measurements/spec-v0.2-conformance/lowered"))
    raise SystemExit(validate(parser.parse_args()))
