"""Bounded seeded mutations against an installed generated frontend. No source writes."""
from pathlib import Path
import argparse
import tempfile
import shutil
import subprocess
parser = argparse.ArgumentParser()
parser.add_argument("web", type=Path, help="Installed frontend directory")
web = parser.parse_args().web.resolve()
source=(web/'scripts/theme.mjs').read_text()
mutations=[
 ('unknown theme fields', 'throw new Error(`Unknown frontend.theme setting: ${key}`);', 'void key;'),
 ('token allowlist', '!tokens.has(key) ||', 'false ||'),
 ('stale output guard', 'if (check)\n      throw', 'if (false)\n      throw'),
 ('symlink refusal', 'if (lstatSync(current).isSymbolicLink())', 'if (false)'),
]
for name,old,new in mutations:
 if source.count(old)!=1: raise SystemExit('Target drift: '+name)
 with tempfile.TemporaryDirectory(prefix='pluto-theme-mutation-') as tmp:
  root=Path(tmp)
  (root/'node_modules').symlink_to(web/'node_modules',target_is_directory=True)
  (root/'theme.mjs').write_text(source.replace(old,new,1))
  shutil.copyfile(web/'scripts/theme.test.mjs',root/'theme.test.mjs')
  result=subprocess.run(['mise','exec','node@24.22.1','--','node','--test','theme.test.mjs'],cwd=root,capture_output=True,text=True,timeout=20)
  if result.returncode==0 or 'SyntaxError' in result.stdout+result.stderr:
   raise SystemExit('Mutation survived or invalid: '+name+'\n'+result.stdout+result.stderr)
  print(name+': killed',flush=True)
print('4 killed; zero survivors, invalid builds or timeouts')
