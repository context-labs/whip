"""Compatibility entry point; canonical implementation lives in evals/whip_evals."""
import sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from whip_evals import prepare_ripgrep as implementation
if __name__ == "__main__":
    implementation.prepare(sys.argv[1])
else:
    sys.modules[__name__] = implementation
