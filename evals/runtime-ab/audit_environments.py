#!/usr/bin/env python3
"""Audit saved Docker identities without querying containers or their secrets."""
import argparse
import json
from pathlib import Path


def audit(directory, requirements_path):
    root = Path(directory).resolve()
    requirements = {row["task"]: row["requirements"] for row in json.loads(Path(requirements_path).read_text())}
    records = json.loads((root / "trials.json").read_text())
    images = {}
    rows = []
    for record in records:
        paths = list((root / "jobs" / record["name"]).glob("*/docker-identity.json"))
        row = {"name": record["name"], "status": record["status"], "identity_count": len(paths), "violations": []}
        if len(paths) != 1:
            row["violations"].append("expected one captured agent-container identity")
            rows.append(row)
            continue
        identity = json.loads(paths[0].read_text())
        expected = requirements[record["task"]]["environment"]
        actual = identity["resources"]
        checks = {
            "image_reference": identity["image_reference"] == expected["docker_image"],
            "cpu": actual["NanoCpus"] == int(expected["cpus"] * 1_000_000_000),
            "memory": actual["Memory"] == expected["memory_mb"] * 1024 * 1024,
            "unprivileged": actual["Privileged"] is False,
            "architecture": identity["architecture"] == "amd64" and identity["os"] == "linux",
            "repo_digest_recorded": bool(identity["repo_digests"]),
        }
        allowed = {"/logs/artifacts", "/logs/verifier", "/logs/agent"}
        mounts = identity["mounts"]
        checks["mount_destinations"] = {m["Destination"] for m in mounts} == allowed
        checks["mount_sources_owned"] = all(
            m["Type"] == "bind" and Path(m["Source"]).resolve().is_relative_to(paths[0].parent)
            for m in mounts)
        if record["runner"] == "pier":
            checks["restricted_network_name"] = all(name.endswith("_pier-egress-internal") for name in identity["networks"])
        row.update(identity_path=str(paths[0]), checks=checks, image_id=identity["image_id"])
        row["violations"].extend(name for name, passed in checks.items() if not passed)
        images.setdefault(record["task"], set()).add((identity["image_reference"], identity["image_id"], tuple(identity["repo_digests"])))
        rows.append(row)
    result = {
        "trials": rows,
        "task_images": {task: [{"reference": item[0], "id": item[1], "repo_digests": item[2]} for item in sorted(values)] for task, values in images.items()},
        "same_image_within_task": all(len(values) == 1 for values in images.values()),
        "violations": sum(len(row["violations"]) for row in rows),
        "limits": "Saved agent-container identities only. Network naming is evidence of configuration, not an independent packet-filter test; paid preflights separately tested allowed and denied domains. Native verifier-container identities are not captured here. Storage quotas are task metadata, not verified filesystem enforcement.",
    }
    (root / "environment-audit.json").write_text(json.dumps(result, indent=2) + "\n")
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("directory")
    parser.add_argument("--requirements", required=True)
    args = parser.parse_args()
    result = audit(args.directory, args.requirements)
    print(json.dumps({"trials": len(result["trials"]), "violations": result["violations"], "same_image_within_task": result["same_image_within_task"]}))
