#!/usr/bin/env python3
"""Generate SPEC_ENC coverage from saved Go test events and corpus reports."""
import argparse,hashlib,json
from pathlib import Path
REQUIREMENTS = {
 '4.1 headers/bodies':['TestDecodeForms','TestRejectAtomic','TestLiteralAndCommentLines','TestEncodingCLI'],
 '4.2 anchors/regions':['TestNestedScopesUnicodeAndDeterminism','TestFailureDiagnosticsAndLocality','TestRejectAtomic'],
 '4.3 blocks/spans':['TestDecodeForms','TestOriginalCoordinatesAndInsertionOrder','TestRejectAtomic'],
 '4.4 attached comments':['TestDecodeForms'],
 '4.5 errors':['TestFailureDiagnosticsAndLocality','TestImportAmbiguityAndPrecedence'],
 '5.1 layout/literal preservation':['TestLiteralAndCommentLines','TestUnformattedLocality'],
 '5.2 import recovery':['TestImportsAndPackages','TestImportAmbiguityAndPrecedence','TestDeclarationSpacingAndImportOrder'],
 '5.3 declaration spacing':['TestDeclarationSpacingAndImportOrder'],
 '5.4 formatting/locality':['TestUnformattedLocality','TestFailureDiagnosticsAndLocality'],
 '6 profiles and plain Go':['TestE2AndPlainFixedPoint','TestE2ReturnValues'],
 '8.1 commands/atomic application':['TestEncodingCLI','TestWriteTreeAndDryDiff'],
 '8.2 canonical encoding':['TestEncoderRecovery'],
}
def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--output',type=Path,default=Path('measurements/enc-validation'));a=p.parse_args()
 events=[json.loads(s) for s in (a.output/'tests.jsonl').read_text().splitlines()]
 passed={e['Test'] for e in events if e.get('Action')=='pass' and e.get('Test')}
 matrix=[{'section':section,'tests':tests,'tested':len(tests),'passing':sum(t in passed for t in tests),'status':'PASS' if all(t in passed for t in tests) else 'FAIL'} for section,tests in REQUIREMENTS.items()]
 history_path=a.output/'history-report.json';history=json.loads(history_path.read_text()) if history_path.exists() else None
 report={'spec_sha256':hashlib.sha256(Path('SPEC_ENC.md').read_bytes()).hexdigest(),
         'tests':{action:sum(e.get('Action')==action and bool(e.get('Test')) for e in events) for action in ('pass','fail','skip')},
         'coverage':matrix,'coverage_unit':'Named behavioral cases; not a claim that each normative clause is exhaustively enumerated.',
         'tool_corpus':None if history is None else {k:history[k] for k in ('pairs','modules','round_trips','mutation','exit_code')},
         'tool_gate':'PASS' if history and history['pairs']>=1000 and history['modules']>=10 and history['exit_code']==0 and history['mutation']['Total']>0 and all(r['status']=='PASS' for r in matrix) and not any(e.get('Action')=='fail' for e in events) else 'NOT PASSED',
         'corpus_scope':'Changed-directory Go trees, including unchanged siblings; vendor and invalid testdata excluded. See history-report.json for complete selection.',
         'model_gate':'NOT RUN: 100 held-out tasks/10 repositories and the paired acceptance measurements are required before any model-accuracy claim.',
         'source_sha256':{str(p):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted([*Path('internal/enc').glob('*.go'),Path('internal/smol/output.go'),*Path('cmd/smol').glob('*.go')])}}
 (a.output/'summary.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps({k:report[k] for k in ('tests','tool_gate','model_gate')},indent=2))
if __name__=='__main__':main()
