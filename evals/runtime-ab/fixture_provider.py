"""Compatibility entry point; canonical implementation lives in evals/whip_evals."""
import sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from whip_evals import fixture_provider as implementation
sys.modules[__name__] = implementation
