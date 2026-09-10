#!/bin/sh
mkdir -p /logs/verifier
python3 - <<'PY'
import sys
from pathlib import Path
sys.path.insert(0,'/app')
try:
 from backoff import delay
 assert [delay(i) for i in range(9)] == [0,1,2,4,8,16,32,60,60]
 assert delay(4,base=.25,cap=1.5)==1.5
 assert delay(1000000000)==60
 for args in [(-1,), (1.5,), (True,), (1,False), (1,float('nan')), (1,1,float('inf')), (1,1,0)]:
  try: delay(*args)
  except (TypeError,ValueError): pass
  else: raise AssertionError(args)
 passed=True
except Exception as e:
 print(type(e).__name__,str(e)); passed=False
Path('/logs/verifier/reward.txt').write_text('1' if passed else '0')
PY
