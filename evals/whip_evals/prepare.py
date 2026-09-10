"""Capture source/build/provider/environment identity before scored execution."""
import hashlib
import io
from importlib.metadata import version
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tarfile
import tempfile
import urllib.request

from .common import EVALS, REPO, atomic_write, file_hash, read_json, utc_now, value_hash, write_json
from .observe import write_config
from .prepare_ripgrep import prepare as prepare_ripgrep


def command(args, *, cwd=None, env=None, timeout=120):
    return subprocess.run(args, cwd=cwd, env=env, check=True, capture_output=True, timeout=timeout).stdout


def environment(evals=EVALS):
    template = '{"cpus":{{.NCPU}},"memory_bytes":{{.MemTotal}},"architecture":{{json .Architecture}},"docker_version":{{json .ServerVersion}},"os":{{json .OSType}}}'
    docker = json.loads(command(["docker", "info", "--format", template]))
    cpu = platform.processor()
    if Path("/proc/cpuinfo").exists():
        cpu = next((line.split(":", 1)[1].strip() for line in Path("/proc/cpuinfo").read_text().splitlines()
                    if line.startswith("model name")), cpu)
    return {**docker, "host_os": platform.system(), "host_architecture": platform.machine(),
            "cpu_model": cpu, "disk_free_mb": shutil.disk_usage(evals).free // (1024 * 1024),
            "python_version": platform.python_version(),
            "emulated": docker["architecture"] not in ("amd64", "x86_64"),
            "runner_versions": {name: version(name) for name in ("harbor", "datacurve-pier")}}


def available_capacity(host, protocol):
    headroom = protocol["headroom"]
    return {"cpus": max(0, host["cpus"] - headroom["cpus"]),
            "memory_mb": max(0, host["memory_bytes"] // (1024 * 1024) - headroom["memory_mb"]),
            "storage_mb": max(0, host["disk_free_mb"] - headroom["storage_mb"])}


def source_snapshot(repo, ref=None):
    repo = Path(repo).resolve()
    commit = command(["git", "rev-parse", "--verify", "--end-of-options", (ref or "HEAD") + "^{commit}"], cwd=repo).decode().strip()
    # Our own launch/report/baseline files must not make every new run dirty or
    # inject a random run ID into its build cache key. Other source stays visible.
    source_paths = [".", ":(exclude)evals/reports", ":(exclude)evals/baselines"]
    dirty = ref is None and bool(command(["git", "status", "--porcelain", "--", *source_paths], cwd=repo).strip())
    if ref is not None or not dirty:
        return command(["git", "archive", "--format=tar", commit], cwd=repo), commit, False
    names = command(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", *source_paths], cwd=repo).decode().split("\0")
    # Only repository source files, never personal credentials or ignored caches.
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w") as archive:
        for name in sorted(set(names) - {""}):
            path = repo / name
            if not path.exists():
                continue
            if path.name == ".env" or path.name.startswith(".env.") or path.name in ("credentials.json", "secrets.json"):
                continue
            if not path.is_file() or path.is_symlink():
                raise ValueError("unsupported source entry: " + name)
            data = path.read_bytes()
            info = tarfile.TarInfo(name)
            info.size = len(data)
            info.mode = path.stat().st_mode & 0o777
            archive.addfile(info, io.BytesIO(data))
    return buffer.getvalue(), commit, dirty


def build_candidate(candidate_id, engine, *, repo=REPO, evals=EVALS, ref=None):
    source, commit, dirty = source_snapshot(repo, ref)
    source_hash = hashlib.sha256(source).hexdigest()
    toolchain = command(["go", "version"]).decode().strip()
    recipe = {"source_sha256": source_hash, "commit": commit, "go_version": toolchain,
              "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0",
              "flags": ["-trimpath", "-buildvcs=false", "-ldflags", "-X main.version=" + commit]}
    directory = Path(evals) / "cache" / "builds" / value_hash(recipe)
    binary = directory / "whip-linux-amd64"
    if directory.exists():
        metadata = read_json(directory / "build.json")
        if metadata["recipe"] != recipe or file_hash(binary) != metadata["binary_sha256"]:
            raise ValueError("cached build differs from frozen identity")
        if file_hash(directory / "rg-linux-amd64") != metadata["ripgrep_sha256"] or file_hash(directory / "source.tar") != source_hash:
            raise ValueError("cached supporting build inputs changed")
    else:
        directory.parent.mkdir(parents=True, exist_ok=True)
        temporary = Path(tempfile.mkdtemp(prefix="build-", dir=directory.parent))
        try:
            source_dir = temporary / "source"
            source_dir.mkdir()
            with tarfile.open(fileobj=io.BytesIO(source)) as archive:
                archive.extractall(source_dir, filter="data")
            env = {**os.environ, "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0"}
            command(["go", "build", *recipe["flags"], "-o", str(temporary / binary.name), "./cmd/whip"],
                    cwd=source_dir, env=env, timeout=600)
            prepare_ripgrep(temporary)
            atomic_write(temporary / "source.tar", source, exclusive=True)
            metadata = {"recipe": recipe, "binary_sha256": file_hash(temporary / binary.name),
                        "ripgrep_sha256": file_hash(temporary / "rg-linux-amd64")}
            write_json(temporary / "build.json", metadata, exclusive=True)
            shutil.rmtree(source_dir)
            temporary.rename(directory)
        finally:
            if temporary.exists():
                shutil.rmtree(temporary)
    return {"id": candidate_id, "engine": engine, "commit": commit, "dirty": dirty,
            "source_sha256": source_hash, "binary_sha256": metadata["binary_sha256"],
            "binary_path": binary.relative_to(evals).as_posix(), "build": metadata,
            "configuration": configuration(engine)}


def configuration(engine):
    # Share the existing observer's production-default configuration constructor.
    with tempfile.TemporaryDirectory() as temporary:
        return write_config(Path(temporary) / "home", engine, 0, native_defaults=True)


def catalog(protocol):
    key = os.environ.get("INFERENCE_API_KEY")
    if not key:
        raise ValueError("INFERENCE_API_KEY is required to run; doctor and dry-run need no key")
    request = urllib.request.Request(protocol["endpoint"] + "/models", headers={"Authorization": "Bearer " + key})
    with urllib.request.urlopen(request, timeout=30) as response:
        models = json.load(response)["data"]
    selected = [m for m in models if m["id"] == protocol["model"]]
    if len(selected) != 1 or protocol["effort"] not in selected[0].get("reasoning_efforts", []):
        raise ValueError("pinned model/effort is not available")
    model = selected[0]
    # Whitelist catalog fields; provider extensions cannot enter public reports.
    selected = {key: model[key] for key in ("id", "context_length", "max_completion_tokens", "reasoning_efforts", "pricing")}
    if "input_modalities" in model:
        selected["input_modalities"] = model["input_modalities"]
    return selected


def contract(candidate, protocol, model):
    return {"engine": candidate["engine"], "configuration": candidate["configuration"],
            "catalog_model": model, "commit_instruction": protocol["commit_instruction"],
            "catalog_cache": {protocol["provider"]: {
                "discoveryVersion": 1, "baseUrl": protocol["endpoint"], "fetchedAt": utc_now(),
                "models": [{"id": model["id"], "contextLength": model["context_length"],
                            "maxCompletionTokens": model["max_completion_tokens"],
                            "reasoningEfforts": model["reasoning_efforts"], "pricing": model["pricing"],
                            **({"inputModalities": model["input_modalities"]} if "input_modalities" in model else {})}]}}}


def pull_images(tasks):
    images = {task["image"] for task in tasks}
    images.update(pin for task in tasks for refs in task.get("dockerfile_images", {}).values() for pin in refs.values())
    for image in sorted(images):
        command(["docker", "pull", "--platform", "linux/amd64", image], timeout=600)
