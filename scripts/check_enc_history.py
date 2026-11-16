#!/usr/bin/env python3
"""Check SPEC_ENC recovery/mutation laws on pinned consecutive Git revisions.

Clones are read-only corpus inputs in /tmp; the report records every commit pair.
No model requests are made. Re-run --reuse to reuse the recorded shallow clones.
"""
import argparse, concurrent.futures, hashlib, json, subprocess, time
from pathlib import Path
REPOS = ['go-chi/chi', 'stretchr/testify', 'spf13/pflag', 'google/uuid',
         'gorilla/mux', 'julienschmidt/httprouter', 'pkg/errors', 'rs/zerolog',
         'tidwall/gjson', 'spf13/cast']

def git(repo,*args):
    return subprocess.check_output(['git','-C',str(repo),*args])

def prepare(args, slug):
    dest=args.work/slug.replace('/','--')
    if not dest.exists():
        subprocess.run(['git','clone','--quiet','--depth',str(args.commits+1),
                        'https://github.com/'+slug+'.git',str(dest)],check=True)
    return slug,dest

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--work',type=Path,default=Path('/tmp/smol-enc-history'))
    p.add_argument('--output',type=Path,default=Path('measurements/enc-validation'))
    p.add_argument('--commits',type=int,default=125)
    p.add_argument('--smol',type=Path,default=Path('bin/smol'))
    p.add_argument('--prepare-only',action='store_true')
    p.add_argument('--reuse',action='store_true')
    p.add_argument('--no-mutate',action='store_true')
    p.add_argument('--only',default='')
    a=p.parse_args();a.work.mkdir(parents=True,exist_ok=True);a.output.mkdir(parents=True,exist_ok=True)
    start=time.monotonic()
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        repos=list(pool.map(lambda slug:prepare(a,slug),REPOS))
    repos.append(('s1liconcow/smol',Path.cwd()))
    if a.prepare_only:
        print(json.dumps([(slug,str(repo)) for slug,repo in repos]));return
    records=[]
    dependency_names={}
    import re
    cache_roots=[Path('/tmp/smol-overhead/modcache'),Path.home()/'go/pkg/mod']
    for cache_root in cache_roots:
        if not cache_root.exists():continue
        for modfile in cache_root.glob('**/go.mod'):
            if '@' not in str(modfile):continue
            match=re.search(r'^module\s+(\S+)',modfile.read_text(errors='replace'),re.M)
            if not match:continue
            for source_file in modfile.parent.glob('*.go'):
                source=source_file.read_text(errors='replace');pkg=re.search(r'^package\s+(\w+)',source,re.M)
                if pkg and not pkg[1].endswith('_test'):
                    dependency_names.setdefault(pkg[1],set()).add(match[1]);break
    blobs={}
    indexes={}
    # Stream JSON trees directly to the Go checker; no per-patch subprocess.
    errfile=open(a.output/'history.stderr','w')
    outfile=open(a.output/'history.jsonl','w')
    proc=subprocess.Popen([str(a.smol.resolve()),'enc','-corpus','-',*([] if a.no_mutate else ['-mutate'])],stdin=subprocess.PIPE,stdout=outfile,stderr=errfile,text=True)
    for slug,repo in repos:
        commits=git(repo,'rev-list','--first-parent','--max-count='+str(a.commits+1),'HEAD').decode().splitlines()
        for newer,older in zip(commits,commits[1:]):
            if a.only and a.only not in newer: continue
            names=git(repo,'diff','--name-only',older,newer,'--','*.go').decode().splitlines()
            # Invalid parser/compiler fixtures are not Go packages; record the filter.
            names=[s for s in names if '/testdata/' not in '/'+s and '/vendor/' not in '/'+s]
            dirs={str(Path(s).parent) for s in names}
            trees=[]
            packages={}
            for rev in (older,newer):
                cache_key=(slug,rev)
                if cache_key not in indexes:
                    index={}
                    for entry in git(repo,'ls-tree','-rz',rev).split(b'\0'):
                        if not entry:continue
                        meta,name=entry.split(b'\t',1)
                        if meta.split()[1]==b'blob':index[name.decode()]=meta.split()[2].decode()
                    indexes[cache_key]=index
                index=indexes[cache_key];files=list(index)
                selected=[s for s in files if s.endswith('.go') and str(Path(s).parent) in dirs]
                wanted={index[s] for s in selected if index[s] not in blobs}
                if wanted:
                    payload=subprocess.check_output(
                        ['git','-C',str(repo),'cat-file','--batch'],input=('\n'.join(sorted(wanted))+'\n').encode())
                    offset=0
                    for oid in sorted(wanted):
                        end=payload.index(b'\n',offset);header=payload[offset:end].split();size=int(header[2]);offset=end+1
                        blobs[oid]=payload[offset:offset+size].decode();offset+=size+1
                tree={s:blobs[index[s]] for s in selected}
                trees.append(tree)
                try:
                    modtext=git(repo,'show',rev+':go.mod').decode()
                    mod=modtext.split('module ',1)[1].split()[0]
                    requirements=re.findall(r'(?m)^\s*(?:require\s+)?([\w./!~-]+)\s+v[\w.+~-]+',modtext)
                    for q,ps in dependency_names.items():
                        for package_path in ps:
                            if any(package_path==req or package_path.startswith(req+'/') for req in requirements):packages.setdefault(q,set()).add(package_path)
                except (subprocess.CalledProcessError,IndexError):
                    mod='github.com/'+slug
                # Package clauses are authoritative for module-owned imports.
                for file,source in tree.items():
                    import re
                    match=re.search(r'^package\s+(\w+)',source,re.M)
                    if match and not match[1].endswith('_test'):
                        package_path=mod+('/'+str(Path(file).parent) if str(Path(file).parent)!='.' else '')
                        packages.setdefault(match[1],set()).add(package_path)
                # References to the root package occur in package tests in subdirs.
                for file in files:
                    if file.endswith('.go') and '/' not in file:
                        oid=index[file]
                        if oid not in blobs:blobs[oid]=git(repo,'show',rev+':'+file).decode()
                        source=blobs[oid]
                        match=re.search(r'^package\s+(\w+)',source,re.M)
                        if match and not match[1].endswith('_test'): packages.setdefault(match[1],set()).add(mod);break
            id=slug+':'+older+'..'+newer
            record={'id':id,'module':slug,'base':older,'target':newer,'changed_go_files':len(names),
                    'files':[len(t) for t in trees]}
            if names and not any(trees): raise RuntimeError('empty corpus selection for '+id)
            records.append(record)
            proc.stdin.write(json.dumps({'ID':id,'Base':trees[0],'Target':trees[1],'Options':{'Packages':{q:sorted(ps) for q,ps in packages.items()}}})+'\n');proc.stdin.flush()
            if len(records)%50==0:print(len(records),'pairs queued',flush=True)
        print(slug,len(commits)-1,'pairs queued',flush=True)
    proc.stdin.close();code=proc.wait();outfile.close();errfile.close()
    results=[json.loads(s) for s in (a.output/'history.jsonl').read_text().splitlines()]
    report={'spec_sha256':hashlib.sha256(Path('SPEC_ENC.md').read_bytes()).hexdigest(),
            'translator_sha256':hashlib.sha256(a.smol.read_bytes()).hexdigest(),
            'command':['smol','enc','-corpus','-',*([] if a.no_mutate else ['-mutate'])], 'go_version':subprocess.check_output(['go','version'],text=True).strip(),
            'pairs':len(records),'modules':len({r['module'] for r in records}),'exit_code':code,'elapsed_seconds':round(time.monotonic()-start,3),
            'failed':[r for r in results if r['Error']], 'round_trips':sum(r['Result']['RoundTrips'] for r in results),
            'mutation':{k:sum(r['Result']['Mutations'][k] for r in results) for k in results[0]['Result']['Mutations']},
            'selection':'Consecutive first-parent commits; all Go files in changed directories, excluding vendor and invalid testdata; non-Go commits included and counted separately.',
            'pairs_with_go_changes':sum(r['changed_go_files']>0 for r in records),'records':records,
            'dependency_package_names':{q:sorted(ps) for q,ps in dependency_names.items()},
            'model_acceptance':'Not run; no model-accuracy claim.'}
    (a.output/'history-report.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps({k:v for k,v in report.items() if k not in ('records','failed')},indent=2))
    print('failed pairs',len(report['failed']))
    raise SystemExit(code)
if __name__=='__main__':main()
