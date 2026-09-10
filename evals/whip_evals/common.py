"""Small filesystem and identity helpers shared by execution and offline analysis."""
import hashlib
import json
import math
import os
from pathlib import Path
import re
import tempfile
from datetime import datetime, timezone
from uuid import uuid4

EVALS = Path(__file__).resolve().parents[1]
REPO = EVALS.parent
SCHEMA_VERSION = 1


def utc_now():
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def new_id():
    return datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ-") + uuid4().hex[:10]


def identifier(value):
    if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}", value):
        raise ValueError("invalid identifier")
    return value


def _invalid_constant(value):
    raise ValueError("non-finite JSON number: " + value)


def read_json(path):
    return json.loads(Path(path).read_text(), parse_constant=_invalid_constant)


def json_bytes(value):
    return (json.dumps(value, indent=2, sort_keys=True, allow_nan=False) + "\n").encode()


def value_hash(value):
    return hashlib.sha256(json_bytes(value)).hexdigest()


def file_hash(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def tree_files(directory):
    root = Path(directory)
    result = {}
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError("symlinks are not permitted in an evidence/task bundle")
        if path.is_file():
            result[path.relative_to(root).as_posix()] = file_hash(path)
    return result


def atomic_write(path, data, *, exclusive=False):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix="." + path.name + "-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        if exclusive:
            os.link(temporary, path)  # Atomic publication without clobbering.
            os.unlink(temporary)
        else:
            os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        Path(temporary).unlink(missing_ok=True)


def write_json(path, value, *, exclusive=False):
    atomic_write(path, json_bytes(value), exclusive=exclusive)


def inside(root, relative):
    root = Path(root).resolve()
    if not isinstance(relative, str) or Path(relative).is_absolute():
        raise ValueError("expected a relative artifact path")
    path = (root / relative).resolve()
    if not path.is_relative_to(root) or path == root:
        raise ValueError("artifact path escapes its root")
    return path


def number(value, *, positive=False):
    return (isinstance(value, (int, float)) and not isinstance(value, bool)
            and math.isfinite(value) and (value > 0 if positive else value >= 0))

