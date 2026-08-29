#!/usr/bin/env python3
"""Build and deploy CPA Manager Plus from local my-feature.

The script keeps all output in deploy.log and refreshes upstream refs before
building. A clean my-feature branch is merged with upstream/main; an uncommitted
my-feature working tree is preserved and packaged without merging. The script
increments a local build number based on the latest upstream tag, injects it as
the Docker build VERSION, uploads the local working tree, and recreates the
Docker service on Oracle 01 bound to 0.0.0.0 for public access on
wcpap.edmundvps.site:18317. After the service is healthy, Docker Build Cache is
pruned so a successful deploy does not leave BuildKit cache on the server.
"""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path
from typing import Iterable, Mapping


if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


PROJECT_ROOT = Path(__file__).resolve().parent
LOG_PATH = PROJECT_ROOT / "deploy.log"
LOCAL_VERSION_PATH = PROJECT_ROOT / "LOCAL_VERSION"

BRANCH = "my-feature"
UPSTREAM_REMOTE = "upstream"
UPSTREAM_URL = "https://github.com/seakee/CPA-Manager-Plus.git"
UPSTREAM_REF = "upstream/main"

SSH_KEY = Path("E:/Files/SSH Key/oracle-ssh-key-2026-05-16.key")
SSH_PORT = "27312"
REMOTE = "ubuntu@163.192.9.157"
REMOTE_DEPLOY_DIR = "/opt/cpa-manager-plus"
REMOTE_TARBALL = "/tmp/cpa-manager-plus-my-feature.tar.gz"
PUBLIC_HOST = "wcpap.edmundvps.site"
SERVICE_PORT = "18317"


class Logger:
    def __init__(self, path: Path) -> None:
        self.path = path
        if path.exists():
            path.unlink()
        self.file = path.open("w", encoding="utf-8", newline="\n")

    def close(self) -> None:
        self.file.close()

    def write(self, message: str = "") -> None:
        print(message, flush=True)
        self.file.write(message + "\n")
        self.file.flush()

    def section(self, title: str) -> None:
        self.write()
        self.write("=" * 80)
        self.write(title)
        self.write("=" * 80)


def resolve_command(name: str) -> str:
    resolved = shutil.which(name)
    if not resolved:
        raise RuntimeError(f"Command not found in PATH: {name}")
    return resolved


def run_checked(
    logger: Logger,
    command: Iterable[str],
    description: str,
    cwd: Path | None = None,
    env: Mapping[str, str] | None = None,
) -> str:
    command_list = [str(part) for part in command]
    logger.section(description)
    logger.write(f"cwd: {cwd or PROJECT_ROOT}")
    logger.write("cmd: " + " ".join(command_list))

    process_env = os.environ.copy()
    if env:
        process_env.update(env)

    process = subprocess.Popen(
        command_list,
        cwd=str(cwd or PROJECT_ROOT),
        env=process_env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        encoding="utf-8",
        errors="replace",
    )

    output_lines: list[str] = []
    assert process.stdout is not None
    for line in process.stdout:
        line = line.rstrip("\r\n")
        output_lines.append(line)
        print(line, flush=True)
        logger.file.write(line + "\n")
        logger.file.flush()

    exit_code = process.wait()
    logger.write(f"exit_code: {exit_code}")
    if exit_code != 0:
        raise RuntimeError(f"{description} failed with exit code {exit_code}")
    return "\n".join(output_lines).strip()


def run_output(command: Iterable[str], cwd: Path | None = None) -> str:
    return subprocess.check_output(
        [str(part) for part in command],
        cwd=str(cwd or PROJECT_ROOT),
        text=True,
        encoding="utf-8",
        errors="replace",
    ).strip()


def ssh_command(remote_command: str) -> list[str]:
    return [
        resolve_command("ssh"),
        "-i",
        str(SSH_KEY),
        "-p",
        SSH_PORT,
        "-o",
        "StrictHostKeyChecking=accept-new",
        "-o",
        "ServerAliveInterval=30",
        REMOTE,
        remote_command,
    ]


def ensure_git_remote(logger: Logger) -> None:
    remotes = run_output([resolve_command("git"), "remote"], PROJECT_ROOT).splitlines()
    if UPSTREAM_REMOTE in remotes:
        run_checked(
            logger,
            [resolve_command("git"), "remote", "set-url", UPSTREAM_REMOTE, UPSTREAM_URL],
            "Ensuring upstream remote URL",
        )
    else:
        run_checked(
            logger,
            [resolve_command("git"), "remote", "add", UPSTREAM_REMOTE, UPSTREAM_URL],
            "Adding upstream remote",
        )


def current_branch() -> str:
    return run_output(
        [resolve_command("git"), "branch", "--show-current"],
        PROJECT_ROOT,
    )


def working_tree_has_changes() -> bool:
    return bool(
        run_output(
            [resolve_command("git"), "status", "--porcelain"],
            PROJECT_ROOT,
        )
    )


def prepare_branch(logger: Logger) -> None:
    ensure_git_remote(logger)
    run_checked(
        logger,
        [resolve_command("git"), "fetch", UPSTREAM_REMOTE, "--tags", "--prune"],
        "Fetching upstream with tags",
    )
    run_checked(
        logger,
        [resolve_command("git"), "fetch", "origin", "--prune"],
        "Fetching origin",
    )

    active_branch = current_branch()
    has_local_changes = working_tree_has_changes()
    if has_local_changes and active_branch != BRANCH:
        raise RuntimeError(
            f"Working tree has local changes on branch {active_branch or '<detached>'}; "
            f"switch to {BRANCH} before deploying."
        )

    if active_branch != BRANCH:
        run_checked(logger, [resolve_command("git"), "switch", BRANCH], f"Switching to {BRANCH}")

    if has_local_changes:
        logger.section(f"Skipping merge of {UPSTREAM_REF}")
        logger.write("Local working tree has uncommitted changes.")
        logger.write("Fetched upstream refs and tags, but preserved the local working tree for packaging.")
        return

    run_checked(
        logger,
        [resolve_command("git"), "merge", "--no-edit", UPSTREAM_REF],
        f"Merging {UPSTREAM_REF} into {BRANCH}",
    )


def latest_upstream_version() -> str:
    tags = run_output(
        [resolve_command("git"), "tag", "--merged", UPSTREAM_REF, "--sort=-v:refname"],
        PROJECT_ROOT,
    ).splitlines()
    if not tags:
        raise RuntimeError(f"No version tag found on {UPSTREAM_REF}")
    return tags[0]


def latest_local_version_tag() -> str:
    tags = run_output(
        [resolve_command("git"), "tag", "--sort=-v:refname"],
        PROJECT_ROOT,
    ).splitlines()
    if not tags:
        raise RuntimeError("No local version tag found")
    return tags[0]


def infer_base_version_without_upstream(logger: Logger) -> str:
    current = LOCAL_VERSION_PATH.read_text(encoding="utf-8").strip() if LOCAL_VERSION_PATH.exists() else ""
    if current:
        base, dot, local_part = current.rpartition(".")
        if dot and base and local_part.isdigit():
            logger.write(f"Base version inferred from LOCAL_VERSION: {base}")
            return base
        logger.write(f"LOCAL_VERSION cannot be used as base version: {current}")

    version = latest_local_version_tag()
    logger.write(f"Base version inferred from local tag: {version}")
    return version


def next_build_version(upstream_version: str, logger: Logger) -> str:
    current = LOCAL_VERSION_PATH.read_text(encoding="utf-8").strip() if LOCAL_VERSION_PATH.exists() else ""
    prefix = f"{upstream_version}."

    if current.startswith(prefix):
        local_part = current[len(prefix) :]
        if not local_part.isdigit():
            raise RuntimeError(f"LOCAL_VERSION has invalid local part: {current}")
        next_number = int(local_part) + 1
    else:
        next_number = 1

    version = f"{prefix}{next_number}"
    LOCAL_VERSION_PATH.write_text(version + "\n", encoding="utf-8")
    logger.write(f"Previous local version: {current or '<none>'}")
    logger.write(f"Next local version: {version}")
    return version


def should_skip(path: Path) -> bool:
    parts = set(path.parts)
    name = path.name
    return (
        ".git" in parts
        or "node_modules" in parts
        or "dist" in parts
        or name in {"build_and_deploy.log", "deploy.log"}
        or name.endswith(".tar.gz")
    )


def create_source_tarball(logger: Logger) -> Path:
    temp_dir = Path(tempfile.mkdtemp(prefix="cpa-manager-plus-"))
    tarball = temp_dir / "source.tar.gz"
    logger.section("Packaging local working tree")
    logger.write(f"tarball: {tarball}")
    with tarfile.open(tarball, "w:gz") as archive:
        for path in PROJECT_ROOT.rglob("*"):
            relative = path.relative_to(PROJECT_ROOT)
            if should_skip(relative):
                continue
            archive.add(path, arcname=str(relative))
    logger.write("Local source tarball created")
    return tarball


def upload_tarball(logger: Logger, tarball: Path) -> None:
    run_checked(
        logger,
        [
            resolve_command("scp"),
            "-i",
            str(SSH_KEY),
            "-P",
            SSH_PORT,
            "-o",
            "StrictHostKeyChecking=accept-new",
            str(tarball),
            f"{REMOTE}:{REMOTE_TARBALL}",
        ],
        "Uploading source tarball to server",
    )


def remote_deploy_script(version: str) -> str:
    return f"""set -e
VERSION='{version}'
DEPLOY_DIR='{REMOTE_DEPLOY_DIR}'
NEW_DIR='{REMOTE_DEPLOY_DIR}.new'
PREV_DIR='{REMOTE_DEPLOY_DIR}.prev'
TARBALL='{REMOTE_TARBALL}'

if ! command -v docker >/dev/null 2>&1; then
  sudo apt-get update
  sudo apt-get install -y docker.io docker-compose-v2
  sudo systemctl enable --now docker
fi

if command -v ufw >/dev/null 2>&1; then
  sudo ufw allow {SERVICE_PORT}/tcp
fi

sudo rm -rf "$NEW_DIR"
sudo mkdir -p "$NEW_DIR"
sudo tar -xzf "$TARBALL" -C "$NEW_DIR"
sudo chown -R ubuntu:ubuntu "$NEW_DIR"

if [ -f "$DEPLOY_DIR/.env" ]; then
  cp "$DEPLOY_DIR/.env" "$NEW_DIR/.env"
fi

if [ ! -f "$NEW_DIR/.env" ]; then
  ADMIN_KEY="cmp_admin_$(openssl rand -hex 24)"
  printf 'CPA_MANAGER_ADMIN_KEY=%s\n' "$ADMIN_KEY" > "$NEW_DIR/.env"
fi

if ! grep -q '^CPA_MANAGER_ADMIN_KEY=' "$NEW_DIR/.env"; then
  ADMIN_KEY="cmp_admin_$(openssl rand -hex 24)"
  printf 'CPA_MANAGER_ADMIN_KEY=%s\n' "$ADMIN_KEY" >> "$NEW_DIR/.env"
fi

cat > "$NEW_DIR/docker-compose.deploy.yml" <<'EOF'
services:
  cpa-manager-plus:
    build:
      context: .
      dockerfile: Dockerfile.manager-server
      args:
        VERSION: "${{BUILD_VERSION}}"
    image: cpa-manager-plus:my-feature
    restart: unless-stopped
    network_mode: "host"
    environment:
      HTTP_ADDR: "0.0.0.0:{SERVICE_PORT}"
      USAGE_DB_PATH: "/data/usage.sqlite"
      CPA_MANAGER_DATA_KEY_PATH: "/data/data.key"
      CPA_MANAGER_ADMIN_KEY: "${{CPA_MANAGER_ADMIN_KEY}}"
      USAGE_COLLECTOR_MODE: "auto"
      USAGE_RESP_QUEUE: "usage"
      USAGE_RESP_POP_SIDE: "right"
      USAGE_BATCH_SIZE: "100"
      USAGE_POLL_INTERVAL_MS: "500"
      USAGE_QUERY_LIMIT: "50000"
      USAGE_CORS_ORIGINS: "*"
      CPA_SERVER_LOG_DIR: "/opt/cli-proxy-api/logs"
    volumes:
      - cpa-manager-plus-data:/data
      - /opt/cli-proxy-api/logs:/opt/cli-proxy-api/logs:ro
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:{SERVICE_PORT}/health"]
      interval: 10s
      timeout: 3s
      retries: 3

volumes:
  cpa-manager-plus-data:
EOF

printf 'BUILD_VERSION=%s\n' "$VERSION" > "$NEW_DIR/.build-version"
export BUILD_VERSION="$VERSION"

sudo rm -rf "$PREV_DIR"
if [ -d "$DEPLOY_DIR" ]; then
  sudo mv "$DEPLOY_DIR" "$PREV_DIR"
fi
sudo mv "$NEW_DIR" "$DEPLOY_DIR"
sudo chown -R ubuntu:ubuntu "$DEPLOY_DIR"

cd "$DEPLOY_DIR"
docker compose -f docker-compose.deploy.yml up -d --build

for i in $(seq 1 60); do
  HEALTH=$(docker inspect --format '{{{{.State.Health.Status}}}}' cpa-manager-plus-cpa-manager-plus-1 2>/dev/null || true)
  if wget -qO- http://127.0.0.1:{SERVICE_PORT}/health >/tmp/cpa-manager-plus-health.json 2>/dev/null && [ "$HEALTH" = "healthy" ]; then
    cat /tmp/cpa-manager-plus-health.json
    break
  fi
  sleep 2
done

HEALTH=$(docker inspect --format '{{{{.State.Health.Status}}}}' cpa-manager-plus-cpa-manager-plus-1 2>/dev/null || true)
if [ "$HEALTH" != "healthy" ]; then
  docker compose -f docker-compose.deploy.yml ps
  docker compose -f docker-compose.deploy.yml logs --tail=120 cpa-manager-plus
  exit 1
fi

printf '\n--- compose ps ---\n'
docker compose -f docker-compose.deploy.yml ps
printf '\n--- listeners ---\n'
(ss -ltnp 2>/dev/null || true) | awk 'NR==1 || /:{SERVICE_PORT}|:8317/'
printf '\n--- firewall ---\n'
(sudo ufw status 2>/dev/null || true) | sed -n '1,20p'
printf '\n--- deployed ---\n'
printf 'version: %s\n' "$VERSION"
printf 'public url: http://{PUBLIC_HOST}:{SERVICE_PORT}\n'
printf 'admin key: %s\n' "$(sed -n 's/^CPA_MANAGER_ADMIN_KEY=//p' .env | tail -1)"
"""


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Build and deploy CPA Manager Plus from the local working tree."
    )
    parser.add_argument(
        "--merge",
        action="store_true",
        help="Fetch and merge upstream/main before building. Default skips upstream.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    logger = Logger(LOG_PATH)
    tarball: Path | None = None
    try:
        logger.write(f"Log file: {LOG_PATH}")
        logger.write(f"Project root: {PROJECT_ROOT}")

        if args.merge:
            prepare_branch(logger)
            upstream_version = latest_upstream_version()
        else:
            logger.section("Skipping upstream merge (default)")
            logger.write("Deploying current local working tree without fetching or merging upstream.")
            upstream_version = infer_base_version_without_upstream(logger)

        version = next_build_version(upstream_version, logger)
        logger.section("Resolved build version")
        logger.write(f"base version: {upstream_version}")
        logger.write(f"build version: {version}")

        tarball = create_source_tarball(logger)
        upload_tarball(logger, tarball)

        run_checked(
            logger,
            ssh_command(remote_deploy_script(version)),
            "Building and deploying on server",
        )

        run_checked(
            logger,
            ssh_command("docker builder prune -af"),
            "Pruning Docker Build Cache after successful deploy",
        )

        logger.section("Done")
        logger.write(f"Deployed version: {version}")
        logger.write(f"Service listens on server 0.0.0.0:{SERVICE_PORT}")
        logger.write(f"Public URL: http://{PUBLIC_HOST}:{SERVICE_PORT}")
        logger.write(f"Health check: http://{PUBLIC_HOST}:{SERVICE_PORT}/health")
        logger.write(f"Log file: {LOG_PATH}")
        return 0
    except Exception as exc:
        logger.section("FAILED")
        logger.write(str(exc))
        logger.write(f"Log file: {LOG_PATH}")
        return 1
    finally:
        if tarball:
            shutil.rmtree(tarball.parent, ignore_errors=True)
        logger.close()


if __name__ == "__main__":
    sys.exit(main())
