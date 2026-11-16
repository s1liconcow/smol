#!/usr/bin/env python3
"""Replay the recorded Go-control edits through the production enc/dec commands.

No model requests. Corpus snapshots and original JSON patches stay untouched.
"""
import argparse,ast,glob,hashlib,json,subprocess,tempfile
from pathlib import Path

def run(cmd,**kwargs):
 r=subprocess.run(cmd,capture_output=True,text=True,**kwargs)
 if r.returncode:raise RuntimeError(r.stderr)
 return r
def gofmt(text):return run(['gofmt'],input=text).stdout

def comparable(text):
 # Independent prototype lexer: ignore optional Go blank lines while retaining
 # blank lines inside literals/comments. Exact R5 bytes are checked by history.
 module=ast.parse(Path('scripts/measure_enc_design.py').read_text())
 node=next(n for n in module.body if isinstance(n,ast.FunctionDef) and n.name=='scan')
 scope={};exec(compile(ast.Module(body=[node],type_ignores=[]),'<prototype lexer>','exec'),scope)
 ls=gofmt(text).split('\n');info=scope['scan'](ls)
 return '\n'.join(line for line,state in zip(ls,info) if line.strip() or state[2])

def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--smol',type=Path,default=Path('bin/smol'));p.add_argument('--output',type=Path,default=Path('measurements/enc-validation/replay'));a=p.parse_args();a.output.mkdir(parents=True,exist_ok=True);binary=str(a.smol.resolve())
 cases=[]
 for study in ('agent-overhead','authoring-modes'):
  for result_path in sorted(Path('measurements',study,'runs').glob('*/r*-go/result.json')):
   record=json.loads(result_path.read_text());attempt=record['attempts'][-1];folder=result_path.parent/('attempt-'+str(attempt['attempt']))
   response=(folder/'response.json').read_text();edits=json.loads(response)['edits']
   base=json.loads(Path('measurements',study,'tasks',record['task'],'sources.json').read_text())['go'];target=dict(base)
   for edit in edits:
    path,old,new=(edit[k] for k in ('path','old','new'))
    if old=='':target[path]=new
    else:
     if target[path].count(old)!=1:raise ValueError('nonunique original edit')
     target[path]=target[path].replace(old,new)
   with tempfile.TemporaryDirectory(prefix='smol-enc-replay-') as tmp:
    root=Path(tmp);old,new=root/'old',root/'new';old.mkdir();new.mkdir()
    for dest,tree in ((old,base),(new,target)):
     (dest/'go.mod').write_text('module replay.invalid/case\n\ngo 1.22\n')
     for path,text in tree.items():(dest/path).parent.mkdir(parents=True,exist_ok=True);(dest/path).write_text(gofmt(text))
    patch=run([binary,'enc',str(old),str(new)]).stdout
    patchfile=root/'patch.enc';patchfile.write_text(patch)
    run([binary,'dec',str(patchfile)],cwd=old)
    got={str(f.relative_to(old)):f.read_text() for f in old.rglob('*.go')}
    want={path:gofmt(text) for path,text in target.items()}
    if {p:comparable(t) for p,t in got.items()}!={p:comparable(t) for p,t in want.items()}:raise ValueError('production replay mismatch '+str(result_path))
   ident=study+'-'+record['task']+'-r'+str(record['repeat']);(a.output/(ident+'.enc')).write_text(patch)
   cases.append({'id':ident,'response':str(folder/'response.json'),'source_sha256':hashlib.sha256(json.dumps(base,sort_keys=True).encode()).hexdigest(),
                 'json_bytes':len(response.encode()),'patch_bytes':len(patch.encode()),'status':'PASS'})
 report={'cases':cases,'passed':len(cases),'failed':0,'translator_sha256':hashlib.sha256(Path(binary).read_bytes()).hexdigest(),'model_requests':0,
         'meaning':'Production replay versus original Go, ignoring optional blank lines outside literals/comments to allow section 5.3 spacing. Exact R5 is checked by the history harness. This is not an encoded-model acceptance trial.'}
 (a.output/'report.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps({'passed':len(cases),'failed':0}))
if __name__=='__main__':main()
