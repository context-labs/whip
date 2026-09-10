#!/usr/bin/env python3
"""Read-only Docker identity/mount capture before runners delete trial images."""
import argparse
import datetime
import json
from pathlib import Path
import subprocess
import time


def docker_json(*args):
    result = subprocess.run(["docker", *args], capture_output=True, text=True)
    return json.loads(result.stdout) if result.returncode == 0 else None


def capture(root):
    captured = []
    for trial in (root / "jobs").glob("*/*"):
        if not (trial / "config.json").exists():
            continue
        target = trial / "docker-identity.json"
        if target.exists():
            continue
        prefix = trial.name.lower()
        for suffix in ("-main-1", "__env-main-1"):
            containers = docker_json("container", "inspect", prefix + suffix)
            if not containers:
                continue
            container = containers[0]
            images = docker_json("image", "inspect", container["Image"])
            image = images[0] if images else {}
            config = container["HostConfig"]
            record = {
                "captured_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                "container_id": container["Id"], "name": container["Name"],
                "image_id": container["Image"], "image_reference": container["Config"]["Image"],
                "repo_digests": image.get("RepoDigests"), "architecture": image.get("Architecture"),
                "os": image.get("Os"), "mounts": container["Mounts"],
                "resources": {key: config.get(key) for key in ("Memory", "MemorySwap", "NanoCpus", "CpuQuota", "CpuPeriod", "PidsLimit", "Privileged")},
                "network_mode": config["NetworkMode"],
                "networks": list(container["NetworkSettings"]["Networks"]),
                "method": "Read-only Docker inspection; process environment and proxy credentials excluded."
            }
            target.write_text(json.dumps(record, indent=2) + "\n")
            captured.append(trial.name)
            break
    return captured


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("directory")
    parser.add_argument("--watch", action="store_true")
    args = parser.parse_args()
    root = Path(args.directory)
    while True:
        found = capture(root)
        if found:
            print(json.dumps({"captured": found}), flush=True)
        trials = json.loads((root / "trials.json").read_text())
        recipe = json.loads((root / "recipe.json").read_text())
        if not args.watch or (len(trials) == len(recipe["schedule"]) and all(t["status"] != "running" for t in trials)):
            break
        time.sleep(10)
