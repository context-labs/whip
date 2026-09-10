#!/bin/sh
mkdir -p /logs/verifier
python3 - <<'PY'
import json
from pathlib import Path
try: passed = json.loads(Path('/app/summary.json').read_text()) == {'a':1300,'b':1100,'c':700}
except Exception: passed = False
Path('/logs/verifier/reward.txt').write_text('1' if passed else '0')
PY
