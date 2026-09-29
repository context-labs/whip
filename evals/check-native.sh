#!/bin/sh
# Offline checks; the optional real CLI runs only in a fresh Linux fixture home.
set -eu
uv sync --project evals --frozen
uv run --project evals --frozen python -m unittest discover -s evals/tests -v
PYTHONPATH=evals/runtime-ab uv run --project evals --frozen python -m unittest discover -s evals/runtime-ab -p 'test_*.py' -v
if [ "$(uname -s)" = Linux ]; then
  native_eval_check_dir=$(mktemp -d)
  trap 'rm -rf "$native_eval_check_dir"' EXIT HUP INT TERM
  CGO_ENABLED=0 go build -o "$native_eval_check_dir/whip" ./cmd/whip
  WHIP_EVAL_TEST_BINARY="$native_eval_check_dir/whip" uv run --project evals --frozen python -m unittest discover -s evals/tests -p test_native_observer.py -v
fi
