"""Pinned task downloads and explicit metadata overlays; never executes a task."""
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import re
import shutil
import tarfile
import tempfile
import tomllib
import urllib.error
import urllib.parse
import urllib.request

from .common import EVALS, file_hash, number, read_json, value_hash


def download(url):
    with urllib.request.urlopen(url, timeout=60) as response:
        return response.read()


def image_digest(image):
    """Resolve public OCI/Docker manifests to an immutable linux/amd64 image."""
    if "@sha256:" in image:
        return image
    name, sep, tag = image.rpartition(":")
    if not sep or "/" in tag:
        name, tag = image, "latest"
    first, _, rest = name.partition("/")
    registry, repository = (first, rest) if "." in first or ":" in first else ("registry-1.docker.io", name)
    if registry == "registry-1.docker.io" and "/" not in repository:
        repository = "library/" + repository
    headers = {"Accept": ", ".join([
        "application/vnd.oci.image.index.v1+json", "application/vnd.oci.image.manifest.v1+json",
        "application/vnd.docker.distribution.manifest.list.v2+json", "application/vnd.docker.distribution.manifest.v2+json"])}

    def get(reference):
        url = f"https://{registry}/v2/{repository}/manifests/{reference}"
        try:
            return urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=60)
        except urllib.error.HTTPError as error:
            if error.code != 401:
                raise
            challenge = dict(re.findall(r'(\w+)="([^"]*)"', error.headers.get("WWW-Authenticate", "")))
            realm = challenge.pop("realm", "")
            if not realm.startswith("https://"):
                raise ValueError("registry did not provide an HTTPS authentication realm") from error
            token = json.loads(download(realm + "?" + urllib.parse.urlencode(challenge)))
            headers["Authorization"] = "Bearer " + (token.get("token") or token["access_token"])
            return urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=60)

    with get(tag) as response:
        raw = response.read()
    manifest = json.loads(raw)
    if "manifests" in manifest:
        matches = [entry for entry in manifest["manifests"]
                   if entry.get("platform", {}).get("os") == "linux"
                   and entry.get("platform", {}).get("architecture") == "amd64"]
        if len(matches) != 1:
            raise ValueError("image must provide exactly one linux/amd64 manifest")
        with get(matches[0]["digest"]) as response:
            raw = response.read()
        if "sha256:" + hashlib.sha256(raw).hexdigest() != matches[0]["digest"]:
            raise ValueError("registry manifest digest mismatch")
    return name + "@sha256:" + hashlib.sha256(raw).hexdigest()


def task_archive_files(data, task_path):
    """Read only one selected task; solutions never enter the prepared bundle."""
    files = {}
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as archive:
        for member in archive:
            parts = PurePosixPath(member.name).parts
            relative = PurePosixPath(*parts[1:])
            if not relative.is_relative_to(task_path):
                continue
            relative = relative.relative_to(task_path)
            if any(part.startswith("solution") or part == "__pycache__" for part in relative.parts):
                continue
            if member.isdir():
                continue
            if not member.isfile() or relative.is_absolute() or ".." in relative.parts:
                raise ValueError("unsafe member in selected task archive")
            name = relative.as_posix()
            if name in files:
                raise ValueError("duplicate task archive member")
            files[name] = (archive.extractfile(member).read(), member.mode & 0o777)
    if not {"task.toml", "instruction.md"}.issubset(files):
        raise ValueError("task archive is missing metadata/instruction")
    return files


def file_inventory(files):
    return {name: {"sha256": hashlib.sha256(data).hexdigest(), "mode": mode}
            for name, (data, mode) in sorted(files.items())}


def set_field(text, section, key, value):
    """Change one known TOML field without rewriting native grader configuration."""
    lines = text.splitlines()
    start = lines.index("[" + section + "]") + 1
    end = next((i for i in range(start, len(lines)) if lines[i].startswith("[")), len(lines))
    matches = [i for i in range(start, end) if re.match(r"^" + re.escape(key) + r"\s*=", lines[i])]
    if len(matches) != 1:
        raise ValueError(f"expected one {section}.{key}")
    lines[matches[0]] = key + " = " + json.dumps(value)
    return "\n".join(lines) + "\n"


def load_spec(evals=EVALS):
    directory = Path(evals) / "frontier"
    lock = read_json(directory / "tasks.lock.json")
    profiles = read_json(directory / "profiles.json")
    protocol = read_json(directory / "protocol.json")
    index = {task["id"]: task for task in lock["tasks"]}
    sets = {name: set(profile["task_ids"]) for name, profile in profiles.items()}
    if (len(index) != 30 or len(lock["tasks"]) != 30
            or [len(sets.get(k, [])) for k in ("smoke", "medium", "full")] != [8, 15, 30]
            or not sets["smoke"] < sets["medium"] < sets["full"]
            or sets["full"] != set(index)):
        raise ValueError("expected exact nested 8/15/30 task profiles")
    for profile in profiles.values():
        if len(profile["task_ids"]) != len(set(profile["task_ids"])):
            raise ValueError("duplicate profile task")
    for task in index.values():
        if not re.fullmatch(r"(terminal-bench|datacurve)/[a-z0-9-]+", task["id"]):
            raise ValueError("invalid task identity")
        if not re.fullmatch(r".+@sha256:[0-9a-f]{64}", task["image"]):
            raise ValueError("task image is not pinned")
        if task["reward"] != {"field": "reward", "pass": 1, "fail": 0}:
            raise ValueError("unsupported native reward mapping")
        if not all(number(task["resources"].get(k), positive=True) for k in ("cpus", "memory_mb", "storage_mb")):
            raise ValueError("positive whole-trial resource reservations required")
        if not number(task["agent_seconds"], positive=True) or not number(task["verifier_seconds"], positive=True):
            raise ValueError("positive native deadlines required")
    return lock, profiles, protocol


def prepare_tasks(lock, tasks, cache):
    """Verify source bytes and prepare immutable task paths before any agent clock."""
    cache = Path(cache)
    cache.mkdir(parents=True, exist_ok=True)
    archives = {}
    prepared = {}
    for task in tasks:
        source = lock["sources"][task["source"]]
        archive_path = cache / (source["sha256"] + ".tar.gz")
        if not archive_path.exists():
            from .common import atomic_write
            data = download(source["url"])
            if hashlib.sha256(data).hexdigest() != source["sha256"]:
                raise ValueError("task source archive digest mismatch")
            atomic_write(archive_path, data, exclusive=True)
        if file_hash(archive_path) != source["sha256"]:
            raise ValueError("cached task source archive digest mismatch")
        if task["source"] not in archives:
            archives[task["source"]] = archive_path.read_bytes()
        files = task_archive_files(archives[task["source"]], task["path"])
        if file_inventory(files) != task["files"]:
            raise ValueError("task file inventory differs from lock")
        text = files["task.toml"][0].decode()
        text = set_field(text, "agent", "timeout_sec", task["agent_seconds"])
        text = set_field(text, "environment", "docker_image", task["image"])
        files["task.toml"] = (text.encode(), files["task.toml"][1])
        from importlib import import_module
        native = import_module(task["runner"] + ".models.task.config").TaskConfig.model_validate(tomllib.loads(text))
        environment = native.environment.model_dump(mode="json")
        verifier = native.verifier.model_dump(mode="json")
        separate = verifier["environment_mode"] == "separate" or verifier["environment"] is not None
        reserved = {key: environment[key] + ((verifier["environment"] or environment)[key] if separate else 0)
                    for key in ("cpus", "memory_mb", "storage_mb")}
        if reserved != task["resources"] or native.verifier.timeout_sec != task["verifier_seconds"]:
            raise ValueError("resolved native resources/deadline differ from lock")
        # Separate-verifier Dockerfiles may refer to a different prebuilt image.
        for path, replacements in task.get("dockerfile_images", {}).items():
            data, mode = files[path]
            for tag, pinned in replacements.items():
                data = data.replace(tag.encode(), pinned.encode())
            files[path] = (data, mode)
        expected = file_inventory(files)
        destination = cache / value_hash(expected)
        if destination.exists():
            actual = {p.relative_to(destination).as_posix(): (p.read_bytes(), p.stat().st_mode & 0o777)
                      for p in destination.rglob("*") if p.is_file() and not p.is_symlink()}
            if file_inventory(actual) != expected or any(p.is_symlink() for p in destination.rglob("*")):
                raise ValueError("prepared task cache was modified")
        else:
            temporary = Path(tempfile.mkdtemp(prefix="task-", dir=cache))
            try:
                for name, (data, mode) in files.items():
                    path = temporary / name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_bytes(data)
                    path.chmod(mode)
                temporary.rename(destination)
            finally:
                if temporary.exists():
                    shutil.rmtree(temporary)
        prepared[task["id"]] = {"path": str(destination), "sha256": value_hash(expected),
                               "resolved_native": native.model_dump(mode="json")}
    return prepared
