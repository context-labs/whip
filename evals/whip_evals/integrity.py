#!/usr/bin/env python3
"""Inventory artifacts and audit a declared set of credentials without changing evidence.

Exit 0: no findings within the declared scope; 1: exact-key/proxy findings need
review; 2: incomplete scan or output error. No available keys means the exact
key scan was not performed. Proxy heuristics do not prove a credential exists.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import tarfile
import tempfile


TOOL = "whip-artifact-integrity"
KEY_NAMES = ("INFERENCE_API_KEY", "OPENCODE_INFERENCE_API_KEY")
CHUNK_BYTES = 1024 * 1024
TAR_SUFFIXES = (".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz", ".txz", ".tar.zst")
CONFIG_SUFFIXES = (".json", ".yaml", ".yml", ".toml", ".env", ".conf", ".ini", ".cfg")
PROXY_FIELD = re.compile(
    rb'''(?i)["']?(?:https?_proxy|all_proxy|(?:[a-z0-9_-]{0,40}[_-])?proxy(?:[_-]?(?:url|user(?:name)?|pass(?:word)?|auth(?:orization)?|token|credentials?))?)["']?\s{0,32}[:=]''')


def available_keys(environ):
    return {name: environ[name].encode("utf-8", "surrogateescape")
            for name in KEY_NAMES if environ.get(name)}


def safe_path(value, keys):
    # A generated filename can itself contain a credential. Never echo it.
    for secret in sorted(set(keys.values()), key=len, reverse=True):
        value = value.replace(secret.decode("utf-8", "surrogateescape"), "[REDACTED]")
    return value


def scan_stream(stream, keys, config=False):
    digest = hashlib.sha256()
    size, tail, matches, proxy = 0, b"", set(), False
    overlap = max([256, *(len(value) - 1 for value in keys.values())])
    while chunk := stream.read(CHUNK_BYTES):
        digest.update(chunk)
        size += len(chunk)
        window = tail + chunk
        matches.update(name for name, value in keys.items() if value in window)
        proxy = proxy or bool(config and PROXY_FIELD.search(window))
        tail = window[-overlap:]
    return {"bytes": size, "sha256": digest.hexdigest()}, sorted(matches), proxy


def inventory(directory, output, *, environ=None, max_tar_bytes=2 * 1024**3):
    root = Path(directory).resolve(strict=True)
    output = Path(output).absolute()
    output = output.parent.resolve() / output.name
    keys = available_keys(os.environ if environ is None else environ)
    clean = lambda value: safe_path(str(value), keys)
    report = {"tool": TOOL, "schema_version": 1,
              "created_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "root": clean(root), "excluded_manifest": clean(output),
              "scope": {
                  "exact_key_variables_available": sorted(keys),
                  "exact_key_variables_unavailable_or_empty": sorted(set(KEY_NAMES) - keys.keys()),
                  "exact_key_scan": "exact byte values only" if keys else "not performed: no nonempty keys available",
                  "content": "regular files and regular entries of top-level tar archives; no extraction",
                  "tar_detection": "stdlib tarfile sniffing, including gzip/bzip2/xz; named unreadable tar archives are errors",
                  "nested_archives": "payload bytes scanned, not recursively decompressed",
                  "other_compressed_formats": "raw bytes only, not decompressed",
                  "symlinks_and_special_files": "not followed or read",
                  "proxy_detection": "potential config by proxy/egress filename or proxy field-name pattern; values never emitted",
                  "consistency": "per-file read, not an atomic directory snapshot; changes during reads are errors",
                  "max_regular_entry_bytes_per_tar": max_tar_bytes,
                  "limitation": "This does not establish that all possible secrets are absent."},
              "files": [], "tar_entries": [], "exact_key_matches": [],
              "potential_proxy_configs": [], "skipped_nonregular": [],
              "nested_archives_not_expanded": [], "errors": []}

    def error(path, reason):
        report["errors"].append({"path": clean(path), "reason": reason})

    def scan(stream, path, name):
        lower = name.lower()
        config = lower.endswith(CONFIG_SUFFIXES) or Path(lower).name.startswith(".env")
        data, matches, proxy = scan_stream(stream, keys, config)
        data["path"] = clean(path)
        if matches:
            report["exact_key_matches"].append({"path": clean(path), "key_variables": matches})
        indicators = []
        if config and re.search(r"proxy|egress", Path(lower).name):
            indicators.append("filename")
        if proxy:
            indicators.append("field_name")
        if indicators:
            report["potential_proxy_configs"].append({"path": clean(path), "indicators": indicators})
        return data

    def scan_tar(stream, path):
        stream.seek(0)
        try:
            archive = tarfile.open(fileobj=stream, mode="r:*")
        except (tarfile.TarError, OSError, EOFError) as exc:
            if path.lower().endswith(TAR_SUFFIXES):
                error(path, "tar_open_" + type(exc).__name__)
            return
        expanded = 0
        try:
            with archive:
                for member in archive:
                    label = path + "!" + member.name
                    if not member.isfile():
                        report["skipped_nonregular"].append(clean(label))
                        continue
                    expanded += member.size
                    if expanded > max_tar_bytes:
                        error(path, "tar_expanded_byte_limit")
                        break
                    with archive.extractfile(member) as payload:
                        data = scan(payload, label, member.name)
                    report["tar_entries"].append(data)
                    if data["bytes"] != member.size:
                        error(label, "tar_entry_size_mismatch")
                    if member.name.lower().endswith(TAR_SUFFIXES):
                        report["nested_archives_not_expanded"].append(clean(label))
        except (tarfile.TarError, OSError, EOFError, ValueError) as exc:
            error(path, "tar_read_" + type(exc).__name__)

    def walk_error(exc):
        error(exc.filename or ".", "walk_" + type(exc).__name__)

    for current, directories, files, directory_fd in os.fwalk(root, follow_symlinks=False, onerror=walk_error):
        directories.sort()
        for name in [*directories, *sorted(files)]:
            path = Path(current) / name
            relative = str(path.relative_to(root))
            if path == output:
                continue
            try:
                before = os.stat(name, dir_fd=directory_fd, follow_symlinks=False)
                if stat.S_ISDIR(before.st_mode):
                    continue
                if not stat.S_ISREG(before.st_mode):
                    report["skipped_nonregular"].append(clean(relative))
                    continue
                # Descriptor-relative open refuses a symlink swap; NONBLOCK
                # prevents a concurrent FIFO replacement from hanging open().
                fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory_fd)
                with os.fdopen(fd, "rb") as stream:
                    opened = os.fstat(stream.fileno())
                    if not stat.S_ISREG(opened.st_mode):
                        error(relative, "changed_to_nonregular")
                        continue
                    data = scan(stream, relative, name)
                    report["files"].append(data)
                    scan_tar(stream, relative)
                    after = os.fstat(stream.fileno())
                    identity = lambda value: (value.st_dev, value.st_ino, value.st_size, value.st_mtime_ns, value.st_ctime_ns)
                    if identity(before) != identity(after) or data["bytes"] != opened.st_size:
                        error(relative, "file_changed_during_read")
            except (OSError, ValueError) as exc:
                error(relative, "file_read_" + type(exc).__name__)
    report["scan_complete_within_declared_scope"] = not report["errors"]
    report["counts"] = {key: len(report[key]) for key in (
        "files", "tar_entries", "exact_key_matches", "potential_proxy_configs",
        "skipped_nonregular", "nested_archives_not_expanded", "errors")}
    return report


def write_manifest(output, report):
    output = Path(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    # Only this tool's own prior manifest may be replaced; never raw evidence.
    if output.is_symlink():
        raise ValueError("manifest output is a symlink")
    if output.exists():
        fd = os.open(output, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd) as existing:
            if not stat.S_ISREG(os.fstat(existing.fileno()).st_mode) or json.load(existing).get("tool") != TOOL:
                raise ValueError("manifest output is not this tool's prior manifest")
    with tempfile.NamedTemporaryFile(mode="w", dir=output.parent, prefix=".artifact-audit-", delete=False) as stream:
        temporary = Path(stream.name)
        try:
            json.dump(report, stream, indent=2, ensure_ascii=True)
            stream.write("\n")
            stream.close()
            os.replace(temporary, output)
        finally:
            temporary.unlink(missing_ok=True)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory")
    parser.add_argument("--output", help="new manifest path; only this tool's own manifest may be replaced")
    parser.add_argument("--max-tar-bytes", type=int, default=2 * 1024**3)
    args = parser.parse_args(argv)
    keys = available_keys(os.environ)
    try:
        if args.max_tar_bytes <= 0:
            raise ValueError("max tar bytes must be positive")
        root = Path(args.directory).resolve(strict=True)
        output = Path(args.output).absolute() if args.output else root / "final-audit/artifact-integrity.json"
        report = inventory(root, output, max_tar_bytes=args.max_tar_bytes)
        write_manifest(output, report)
        print(json.dumps({"manifest": safe_path(str(output), keys), "counts": report["counts"],
                          "exact_key_variables_available": sorted(keys),
                          "scan_complete_within_declared_scope": report["scan_complete_within_declared_scope"]}))
        return 2 if report["errors"] else 1 if report["exact_key_matches"] or report["potential_proxy_configs"] else 0
    except Exception as exc:
        # Exception messages and subprocess/config values must never be echoed.
        print(json.dumps({"audit_error": type(exc).__name__}))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
