#!/bin/sh
mkdir -p /logs/verifier
python3 - <<'PY'
from pathlib import Path
import subprocess
try:
    patch = Path('/logs/artifacts/model.patch')
    assert patch.stat().st_size > 0, 'no committed patch collected'
    assert Path('/logs/artifacts/service-proof.txt').read_text() == 'proof', 'service did not survive finality'
    if not Path('/app/proof.txt').exists():
        subprocess.run(['git', 'apply', str(patch)], cwd='/app', check=True)
    assert Path('/app/proof.txt').read_text() == 'proof'
    assert Path('/app/child.txt').read_text() == 'child'
    assert len(Path('/app/large.txt').read_text()) == 70000
    page = Path('/app/page.txt').read_text()
    assert 'second' in page and 'first' not in page and 'third' not in page, 'numeric paging failed'
    passed = True
except Exception as error:
    print(type(error).__name__, str(error))
    passed = False
Path('/logs/verifier/reward.txt').write_text('1' if passed else '0')
PY
