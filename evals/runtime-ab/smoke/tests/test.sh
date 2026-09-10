#!/bin/sh
mkdir -p /logs/verifier
python3 - <<'PY'
from pathlib import Path
try: passed = Path('/app/proof.txt').read_text() == 'proof' and Path('/app/child.txt').read_text() == 'child'
except OSError: passed = False
Path('/logs/verifier/reward.txt').write_text('1' if passed else '0')
PY
