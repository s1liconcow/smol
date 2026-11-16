#!/usr/bin/env python3
"""Bundle all active production files of a real Go package for usage measurement."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile


def sha(data):
    return hashlib.sha256(data).hexdigest()


def prepare(a):
    out = a.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    smol = str(a.smol.resolve())
    package = json.loads(subprocess.check_output(["go", "list", "-json", a.package], text=True))
    if package.get("DepsErrors") or package.get("Error"):
        raise RuntimeError("go list reported errors: " + json.dumps(package.get("DepsErrors", package.get("Error"))))
    names = sorted(package.get("GoFiles", []) + package.get("CgoFiles", []))
    if not names:
        raise RuntimeError("No active production Go files")
    source_dir = Path(package["Dir"])
    version = subprocess.check_output(["go", "version"], text=True).strip()
    go_version = version.split()[2][2:]
    with tempfile.TemporaryDirectory(prefix="smol-package-") as tmp:
        work = Path(tmp)
        (work / "go.mod").write_text("module benchmark.invalid/package\n\ngo " + go_version + "\n")
        originals = {}
        for name in names:
            originals[name] = (source_dir / name).read_bytes()
            (work / name).write_bytes(originals[name])
        checks = []
        for cmd in ([smol, "check", str(work)], [smol, "lift", str(work)],
                    [smol, "fmt", "-check", str(work)], [smol, "lower", str(work)]):
            r = subprocess.run(cmd, text=True, capture_output=True, check=True)
            checks.append({"command": [Path(cmd[0]).name, *["<package>" if arg == str(work) else arg for arg in cmd[1:]]],
                           "stdout": r.stdout, "stderr": r.stderr, "exit_code": r.returncode})
        records = []
        bundles = {"go": [], "smol": [], "notes": []}
        for name in names:
            go = (work / name).read_bytes()
            smol_path = work / Path(name).with_suffix(".smol")
            sm = smol_path.read_bytes()
            notes_path = Path(str(smol_path) + ".notes")
            notes = notes_path.read_bytes() if notes_path.exists() else b""
            records.append({"name": name, "original_bytes": len(originals[name]),
                            "original_sha256": sha(originals[name]), "go_bytes": len(go),
                            "go_sha256": sha(go), "go_lines": len(go.splitlines()),
                            "smol_bytes": len(sm), "smol_sha256": sha(sm), "smol_lines": len(sm.splitlines()),
                            "notes_bytes": len(notes), "notes_sha256": sha(notes)})
            # Same file marker in every input. The Smol input is the code channel
            # only; comments and blank lines are in the notes channel.
            marker = "// File: " + package["ImportPath"] + "/" + name + "\n"
            bundles["go"].append(marker + go.decode() + "\n")
            bundles["smol"].append(marker + sm.decode() + "\n")
            bundles["notes"].append(marker + notes.decode() + "\n")
        for kind, parts in bundles.items():
            (out / ("package." + kind + ".txt")).write_text("".join(parts))
        for flag, kind in ((None, "outline"), ("-docs", "outline-docs")):
            cmd = [smol, "outline", *([flag] if flag else []), *[str(work / n) for n in names]]
            r = subprocess.run(cmd, text=True, capture_output=True, check=True)
            (out / ("package." + kind + ".txt")).write_text(
                r.stdout.replace("// File: ", "// File: " + package["ImportPath"] + "/"))
        (out / "validation.json").write_text(json.dumps(checks, indent=2) + "\n")
    manifest = {"package": package["ImportPath"], "go_version": version,
                "selection": "All active GoFiles and CgoFiles; excludes tests, dependencies, inactive build variants.",
                "go_source": "Normalized Go produced by lower(lift(original)); L1-L3 checked before bundling.",
                "smol_source": "Canonical code channel (name.smol): identifiers and literals preserved; comments and blank lines are in the notes channel (name.smol.notes), bundled separately as package.notes.txt and not part of the measured input.",
                "outline": "package.outline.txt and package.outline-docs.txt: read-only views without function bodies; not round-trippable.",
                "bundle_format": "Concatenated files in filename order, identical // File: markers in both inputs.",
                "goos": subprocess.check_output(["go", "env", "GOOS"], text=True).strip(),
                "goarch": subprocess.check_output(["go", "env", "GOARCH"], text=True).strip(),
                "files": records, "file_count": len(records),
                "go_lines": sum(r["go_lines"] for r in records),
                "go_bytes": sum(r["go_bytes"] for r in records),
                "smol_bytes": sum(r["smol_bytes"] for r in records),
                "notes_bytes": sum(r["notes_bytes"] for r in records)}
    (out / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    if package.get("Goroot"):
        root = Path(package["Root"])
        license_path = next((p for p in (root / "LICENSE", root.parent / "LICENSE") if p.is_file()), None)
        if license_path is None:
            raise RuntimeError("Cannot find the installed Go distribution's LICENSE")
        shutil.copyfile(license_path, out / "GO-LICENSE.txt")
    print(json.dumps({k: manifest[k] for k in ("package", "file_count", "go_lines", "go_bytes", "smol_bytes", "notes_bytes")}, indent=2))


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("package", nargs="?", default="encoding/json")
    p.add_argument("--smol", type=Path, default=Path("bin/smol"))
    p.add_argument("--output", type=Path, default=Path("measurements/encoding-json"))
    prepare(p.parse_args())
