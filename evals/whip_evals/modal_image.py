"""Explicitly build the qualified Docker-in-VM base, never an agent task image."""
import argparse
from pathlib import Path

from .common import EVALS, file_hash, write_json
from .modal_cloud import APP_NAME, ENVIRONMENT, sdk

TOOLS = {
    "docker": ("https://download.docker.com/linux/static/stable/x86_64/docker-28.3.3.tgz",
               "40c16bcf324f354b382d07e845e6a79e3493fc0c09b252dff9e1a46125589bff"),
    "compose": ("https://github.com/docker/compose/releases/download/v2.39.2/docker-compose-linux-x86_64",
                "a55a8cd4ef103aac282812554e531aac8df7e914a287ee81e14d695556a22902"),
    "buildx": ("https://github.com/docker/buildx/releases/download/v0.27.0/buildx-v0.27.0.linux-amd64",
               "4f5e5a1b6dd0d6ff8476c8def7602d1eeedcb6f602e8dcd45079d352247eba06"),
}


def image(*, evals=EVALS):
    commands = []
    for name, (url, digest) in TOOLS.items():
        commands += [f"curl -fsSL --retry 2 {url} -o /tmp/{name}.download",
                     f"echo '{digest}  /tmp/{name}.download' | sha256sum -c -"]
    commands += ["tar -xzf /tmp/docker.download -C /usr/local/bin --strip-components=1",
                 "mkdir -p /usr/local/libexec/docker/cli-plugins",
                 "install -m755 /tmp/compose.download /usr/local/libexec/docker/cli-plugins/docker-compose",
                 "install -m755 /tmp/buildx.download /usr/local/libexec/docker/cli-plugins/docker-buildx",
                 "rm /tmp/docker.download /tmp/compose.download /tmp/buildx.download",
                 "docker --version && docker compose version && docker buildx version"]
    return (sdk().Image.debian_slim(python_version="3.12")
            .apt_install("ca-certificates", "curl", "git", "iptables", "iproute2", "procps", "xz-utils", "tini", "uidmap")
            .run_commands(*commands)
            .uv_sync(str(evals), frozen=True, uv_version="0.12.13")
            .entrypoint([]))


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True, type=Path, help="new local image identity receipt; no secrets")
    args = parser.parse_args(argv)
    if args.output.exists():
        raise ValueError("image receipt already exists")
    modal = sdk()
    app = modal.App.lookup(APP_NAME, environment_name=ENVIRONMENT)
    worker = image()
    with modal.enable_output():
        worker.build(app)
    write_json(args.output, {"image_id": worker.object_id, "environment": ENVIRONMENT,
               "python": "3.12", "uv": "0.12.13", "tools": TOOLS,
               "uv_lock_sha256": file_hash(EVALS / "uv.lock"),
               "pyproject_sha256": file_hash(EVALS / "pyproject.toml"),
               "qualification_status": "required_for_new_image"}, exclusive=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
