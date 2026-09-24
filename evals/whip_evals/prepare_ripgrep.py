#!/usr/bin/env python3
"""Fetch a pinned static search binary beside the Whip evaluation binary."""
import argparse
import hashlib
import io
from pathlib import Path
import tarfile
import urllib.request

URL = "https://github.com/BurntSushi/ripgrep/releases/download/14.1.1/ripgrep-14.1.1-x86_64-unknown-linux-musl.tar.gz"
SHA256 = "4cf9f2741e6c465ffdb7c26f38056a59e2a2544b51f7cc128ef28337eeae4d8e"


def prepare(directory):
    target = Path(directory)
    target.mkdir(parents=True, exist_ok=True)
    with urllib.request.urlopen(URL, timeout=60) as response:
        data = response.read()
    if hashlib.sha256(data).hexdigest() != SHA256:
        raise ValueError("ripgrep archive checksum mismatch")
    prefix = "ripgrep-14.1.1-x86_64-unknown-linux-musl/"
    with tarfile.open(fileobj=io.BytesIO(data)) as archive:
        for source, name in (("rg", "rg-linux-amd64"), ("LICENSE-MIT", "ripgrep-LICENSE-MIT"),
                             ("COPYING", "ripgrep-COPYING"), ("UNLICENSE", "ripgrep-UNLICENSE")):
            (target / name).write_bytes(archive.extractfile(prefix + source).read())
    (target / "rg-linux-amd64").chmod(0o755)
    print(hashlib.sha256((target / "rg-linux-amd64").read_bytes()).hexdigest())


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("directory", help="directory containing the Whip Linux binary")
    prepare(parser.parse_args().directory)
