#!/usr/bin/env python3
"""Audit L1–L3 over the host standard library and save a reproducible report."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import time

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--smol',default='bin/smol')
p.add_argument('--output',type=Path,default=Path('measurements/conformance'))
p.add_argument('packages',nargs='*',default=['std'])
a=p.parse_args()
a.output.mkdir(parents=True,exist_ok=True)
start=time.time()
r=subprocess.run([a.smol,'check',*a.packages],text=True,capture_output=True)
log=r.stdout+r.stderr
(a.output/'check.log').write_text(log)
passed=re.search(r'(?:check: |; )(\d+)(?: files?)? passed',log)
failed=re.search(r'(\d+) files failed',log)
report={'go_version':subprocess.check_output(['go','version'],text=True).strip(),
        'command':[a.smol,'check',*a.packages],'exit_code':r.returncode,
        'passed_files':int(passed.group(1)) if passed else None,
        'failed_files':int(failed.group(1)) if failed else (0 if r.returncode==0 else None),
        'elapsed_seconds':round(time.time()-start,3),
        'scope':'Host GOOS/GOARCH and build constraints; GoFiles, CgoFiles, TestGoFiles, XTestGoFiles.',
        'meaning':'Byte equality, deterministic lifting, and formatter idempotence. Not mandatory-fold completeness or all package test suites.'}
(a.output/'report.json').write_text(json.dumps(report,indent=2)+'\n')
print(log,end='')
raise SystemExit(r.returncode)
