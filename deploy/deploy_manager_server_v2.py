#!/usr/bin/env python3
"""Build and deploy CPA Manager Plus from the isolated my-feature-v2 branch.

The rollout uses a separate source directory, Docker image, container, bridge
network, data directory, environment file, backup root, and public port. It
never stops, recreates, mounts, or joins the existing cpa-manager-plus service.
"""

from __future__ import annotations

import argparse
import io
import os
import re
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile
from datetime import datetime, timezone
from pathlib import Path, PurePosixPath
from typing import Iterable, Mapping


if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(encoding="utf-8", errors="replace")


PROJECT_ROOT = Path(__file__).resolve().parents[1]
BRANCH = "my-feature-v2"
UPSTREAM_TAG_PATTERN = re.compile(r"^v\d+\.\d+\.\d+$")
BUILD_VERSION_PATTERN = re.compile(r"^(v\d+\.\d+\.\d+)\.(\d+)$")

SSH_KEY = Path("E:/Files/SSH Key/oracle-ssh-key-2026-05-16.key")
SSH_PORT = "27312"
REMOTE = "ubuntu@163.192.9.157"

SERVICE_PORT = 18318
SERVICE_NAME = "cpa-manager-plus-v2"
COMPOSE_PROJECT = "cpa-manager-plus-v2"
NETWORK_NAME = "cpa-manager-plus-v2-network"
IMAGE_NAME = "cpa-manager-plus-v2"
REMOTE_DEPLOY_DIR = "/opt/cpa-manager-plus-v2"
REMOTE_DATA_ROOT = "/var/lib/cpa-manager-plus-v2"
REMOTE_DATA_DIR = f"{REMOTE_DATA_ROOT}/data"
REMOTE_ENV_FILE = "/etc/cpa-manager-plus-v2.env"
REMOTE_ENV_MARKER = "/etc/cpa-manager-plus-v2.install-id"
REMOTE_BACKUP_ROOT = "/var/backups/cpa-manager-plus-v2"
REMOTE_LOG_ROOT = "/var/log/cpa-manager-plus-v2"
REMOTE_TARBALL_PREFIX = "/tmp/cpa-manager-plus-v2-source"
REMOTE_SCRIPT_PREFIX = "/tmp/cpa-manager-plus-v2-deploy"
ORIGINAL_SERVICE_NAME = "cpa-manager-plus-cpa-manager-plus-1"
ORIGINAL_SERVICE_PORT = 18317

TEXT_SUFFIXES = {
    ".c",
    ".cc",
    ".css",
    ".go",
    ".h",
    ".html",
    ".ini",
    ".java",
    ".js",
    ".json",
    ".jsx",
    ".md",
    ".mjs",
    ".mts",
    ".py",
    ".sh",
    ".sql",
    ".sum",
    ".toml",
    ".ts",
    ".tsx",
    ".txt",
    ".vue",
    ".xml",
    ".yaml",
    ".yml",
}
TEXT_NAMES = {
    ".dockerignore",
    ".gitignore",
    "Dockerfile",
    "Dockerfile.manager-server",
    "Makefile",
}


class Logger:
    def __init__(self, path: Path) -> None:
        self.path = path
        path.parent.mkdir(parents=True, exist_ok=True)
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
    command = shutil.which(name)
    if not command:
        raise RuntimeError(f"Command not found in PATH: {name}")
    return command


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


def run_capture(command: Iterable[str], cwd: Path | None = None) -> str:
    result = subprocess.run(
        [str(part) for part in command],
        cwd=str(cwd or PROJECT_ROOT),
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    return result.stdout.strip()


def git_command(*args: str) -> list[str]:
    return [resolve_command("git"), *args]


def ssh_command(remote_command: str) -> list[str]:
    return [
        resolve_command("ssh"),
        "-i",
        str(SSH_KEY),
        "-p",
        SSH_PORT,
        "-o",
        "BatchMode=yes",
        "-o",
        "StrictHostKeyChecking=accept-new",
        "-o",
        "ServerAliveInterval=30",
        REMOTE,
        remote_command,
    ]


def scp_command(local_path: Path, remote_path: str) -> list[str]:
    return [
        resolve_command("scp"),
        "-i",
        str(SSH_KEY),
        "-P",
        SSH_PORT,
        "-o",
        "BatchMode=yes",
        "-o",
        "StrictHostKeyChecking=accept-new",
        str(local_path),
        f"{REMOTE}:{remote_path}",
    ]


def git_status(logger: Logger) -> None:
    branch = run_capture(git_command("branch", "--show-current"), PROJECT_ROOT)
    if branch != BRANCH:
        raise RuntimeError(f"Expected branch {BRANCH}, got {branch or '<detached>'}")

    head = run_capture(git_command("rev-parse", "HEAD"), PROJECT_ROOT)
    status = run_capture(git_command("status", "--porcelain", "--untracked-files=all"), PROJECT_ROOT)
    tracked_changes = [
        line for line in status.splitlines() if line and not line.startswith("?? ")
    ]
    if tracked_changes:
        raise RuntimeError(
            "Tracked worktree changes are not deployable:\n" + "\n".join(tracked_changes)
        )

    logger.section("Source lock")
    logger.write(f"branch: {branch}")
    logger.write(f"commit: {head}")
    if status:
        logger.write("untracked files are excluded by git archive:")
        for line in status.splitlines():
            logger.write(f"  {line}")
    else:
        logger.write("worktree: clean")


def stable_tag_for_head() -> str:
    candidates = run_capture(
        git_command("tag", "--merged", "HEAD", "--sort=-v:refname"),
        PROJECT_ROOT,
    ).splitlines()
    for candidate in candidates:
        if UPSTREAM_TAG_PATTERN.fullmatch(candidate):
            return candidate
    raise RuntimeError("No stable release tag vX.Y.Z is an ancestor of HEAD")


def remote_marker(logger: Logger) -> str:
    command = (
        f"if [ -f {shlex.quote(REMOTE_DEPLOY_DIR + '/.build-version')} ]; "
        f"then sed -n 's/^BUILD_VERSION=//p' {shlex.quote(REMOTE_DEPLOY_DIR + '/.build-version')} "
        "| head -n 1 | tr -d '\\r\\n'; fi"
    )
    logger.section("Reading isolated deployment marker")
    logger.write("remote marker path: " + REMOTE_DEPLOY_DIR + "/.build-version")
    output = run_checked(logger, ssh_command(command), "Checking Oracle 01")
    return output.strip()


def next_build_version(base_tag: str, previous: str, logger: Logger) -> str:
    prefix = f"{base_tag}."
    next_number = 1
    match = BUILD_VERSION_PATTERN.fullmatch(previous)
    if match and match.group(1) == base_tag:
        next_number = int(match.group(2)) + 1
    version = f"{prefix}{next_number:04d}"
    logger.section("Build identity")
    logger.write(f"stable base: {base_tag}")
    logger.write(f"previous isolated version: {previous or '<none>'}")
    logger.write(f"new version: {version}")
    return version


def is_text_member(name: str) -> bool:
    path = PurePosixPath(name)
    return path.name in TEXT_NAMES or path.suffix.lower() in TEXT_SUFFIXES


def create_lf_archive(logger: Logger, head: str, stamp: str) -> tuple[Path, Path]:
    temp_dir = Path(tempfile.mkdtemp(prefix="cpa-manager-plus-v2-"))
    raw_tar = temp_dir / "source.raw.tar"
    archive_path = temp_dir / f"cpa-manager-plus-v2-{stamp}.tar.gz"

    with raw_tar.open("wb") as output:
        process = subprocess.run(
            [
                resolve_command("git"),
                "-c",
                "core.autocrlf=false",
                "-c",
                "core.eol=lf",
                "archive",
                "--format=tar",
                head,
            ],
            cwd=str(PROJECT_ROOT),
            check=True,
            stdout=output,
            stderr=subprocess.PIPE,
            text=False,
        )
    with tarfile.open(raw_tar, "r:") as source, tarfile.open(
        archive_path, "w:gz", format=tarfile.GNU_FORMAT
    ) as target:
        member_count = 0
        text_count = 0
        for member in source.getmembers():
            member_count += 1
            if member.isfile():
                data = source.extractfile(member)
                if data is None:
                    raise RuntimeError(f"Unable to read archive member: {member.name}")
                payload = data.read()
                if is_text_member(member.name):
                    text_count += 1
                    payload = payload.replace(b"\r\n", b"\n").replace(b"\r", b"\n")
                member.size = len(payload)
                target.addfile(member, io.BytesIO(payload))
            else:
                target.addfile(member)

    crlf_members: list[str] = []
    required_members = {
        "Dockerfile.manager-server",
        "package-lock.json",
        "apps/manager-server/cmd/cpa-manager-plus/main.go",
    }
    seen_members: set[str] = set()
    with tarfile.open(archive_path, "r:gz") as archive:
        for member in archive.getmembers():
            seen_members.add(member.name)
            if member.isfile() and is_text_member(member.name):
                data = archive.extractfile(member)
                if data is not None and b"\r" in data.read():
                    crlf_members.append(member.name)
    missing = sorted(required_members - seen_members)
    if missing:
        raise RuntimeError("Archive is missing required members: " + ", ".join(missing))
    if crlf_members:
        raise RuntimeError("Archive still contains CR bytes: " + ", ".join(crlf_members))

    logger.section("Source archive")
    logger.write(f"archive: {archive_path}")
    logger.write(f"members: {member_count}")
    logger.write(f"normalized text members: {text_count}")
    logger.write("CRLF check: clear")
    return archive_path, temp_dir


def render_remote_script(
    *,
    version: str,
    source_commit: str,
    stamp: str,
    remote_tarball: str,
    remote_script: str,
) -> str:
    values = {
        "version": shlex.quote(version),
        "source_commit": shlex.quote(source_commit),
        "stamp": shlex.quote(stamp),
        "remote_tarball": shlex.quote(remote_tarball),
        "remote_script": shlex.quote(remote_script),
        "deploy_dir": shlex.quote(REMOTE_DEPLOY_DIR),
        "data_root": shlex.quote(REMOTE_DATA_ROOT),
        "data_dir": shlex.quote(REMOTE_DATA_DIR),
        "env_file": shlex.quote(REMOTE_ENV_FILE),
        "env_marker": shlex.quote(REMOTE_ENV_MARKER),
        "backup_root": shlex.quote(REMOTE_BACKUP_ROOT),
        "log_root": shlex.quote(REMOTE_LOG_ROOT),
        "project": shlex.quote(COMPOSE_PROJECT),
        "service": shlex.quote(SERVICE_NAME),
        "network": shlex.quote(NETWORK_NAME),
        "image": shlex.quote(IMAGE_NAME),
        "original_service": shlex.quote(ORIGINAL_SERVICE_NAME),
    }
    template = '''#!/usr/bin/env bash
set -Eeuo pipefail

VERSION=@VERSION@
SOURCE_COMMIT=@SOURCE_COMMIT@
STAMP=@STAMP@
TARBALL=@REMOTE_TARBALL@
REMOTE_SCRIPT=@REMOTE_SCRIPT@
DEPLOY_DIR=@DEPLOY_DIR@
DATA_ROOT=@DATA_ROOT@
DATA_DIR=@DATA_DIR@
ENV_FILE=@ENV_FILE@
ENV_MARKER=@ENV_MARKER@
BACKUP_ROOT=@BACKUP_ROOT@
LOG_ROOT=@LOG_ROOT@
PROJECT=@PROJECT@
SERVICE=@SERVICE@
NETWORK=@NETWORK@
IMAGE=@IMAGE@
ORIGINAL_SERVICE=@ORIGINAL_SERVICE@
PORT=@PORT@
ORIGINAL_PORT=@ORIGINAL_PORT@
BACKUP_DIR="$BACKUP_ROOT/$VERSION-$STAMP"
STAGE_DIR="$DEPLOY_DIR.stage.$STAMP"
COMPOSE_FILE="$DEPLOY_DIR/docker-compose.deploy.yml"
CONTAINER="$SERVICE"

mkdir -p "$LOG_ROOT"
exec > >(tee -a "$LOG_ROOT/deploy-$VERSION-$STAMP.log") 2>&1

rollback_base=""
old_original_running=0

fail_with_status() {{
  local message="$1"
  printf 'ERROR: %s\\n' "$message"
  if docker inspect "$CONTAINER" >/dev/null 2>&1; then
    docker inspect --format='container status={{{{.State.Status}}}} health={{{{if .State.Health}}}}{{{{.State.Health.Status}}}}{{{{else}}}}none{{{{end}}}}}' "$CONTAINER" || true
  fi
  if [ -f "$DEPLOY_DIR/docker-compose.deploy.yml" ]; then
    docker compose -p "$PROJECT" -f "$DEPLOY_DIR/docker-compose.deploy.yml" ps || true
    docker compose -p "$PROJECT" -f "$DEPLOY_DIR/docker-compose.deploy.yml" logs --tail=120 > "$BACKUP_DIR/compose-failure.log" 2>&1 || true
    chmod 600 "$BACKUP_DIR/compose-failure.log" || true
  fi
  exit 1
}}

rollback_previous() {{
  set +e
  printf 'Rolling back isolated service only.\\n'
  if [ -f "$DEPLOY_DIR/docker-compose.deploy.yml" ]; then
    docker compose -p "$PROJECT" -f "$DEPLOY_DIR/docker-compose.deploy.yml" down --remove-orphans
  fi
  rm -rf "$DEPLOY_DIR"
  if [ -d "$BACKUP_DIR/source" ]; then
    mv "$BACKUP_DIR/source" "$DEPLOY_DIR"
    if [ -f "$DEPLOY_DIR/docker-compose.deploy.yml" ]; then
      docker compose -p "$PROJECT" -f "$DEPLOY_DIR/docker-compose.deploy.yml" up -d --no-build
    fi
  fi
  printf 'Rollback attempted; original cpa-manager-plus was not touched.\\n'
}}

printf 'version=%s\\n' "$VERSION"
printf 'source_commit=%s\\n' "$SOURCE_COMMIT"
printf 'deploy_dir=%s\\n' "$DEPLOY_DIR"
printf 'data_dir=%s\\n' "$DATA_DIR"
printf 'port=%s\\n' "$PORT"
printf 'network=%s\\n' "$NETWORK"

command -v docker >/dev/null 2>&1 || fail_with_status 'docker is not installed'
docker compose version >/dev/null 2>&1 || fail_with_status 'docker compose is not available'
command -v ss >/dev/null 2>&1 || fail_with_status 'ss is not installed'
command -v curl >/dev/null 2>&1 || fail_with_status 'curl is not installed'
command -v openssl >/dev/null 2>&1 || fail_with_status 'openssl is not installed'

if docker inspect "$CONTAINER" >/dev/null 2>&1; then
  existing_project="$(docker inspect --format='{{{{index .Config.Labels "com.docker.compose.project"}}}}' "$CONTAINER")"
  if [ -n "$existing_project" ] && [ "$existing_project" != "$PROJECT" ]; then
    fail_with_status "container $CONTAINER belongs to unexpected compose project $existing_project"
  fi
fi

if [ -e "$DEPLOY_DIR" ] && [ ! -f "$DEPLOY_DIR/.isolation-id" ]; then
  fail_with_status "$DEPLOY_DIR exists without the v2 isolation marker"
fi
if [ -e "$DATA_ROOT" ] && [ ! -f "$DATA_ROOT/.isolation-id" ]; then
  fail_with_status "$DATA_ROOT exists without the v2 isolation marker"
fi
if [ -e "$ENV_FILE" ] && [ ! -f "$ENV_MARKER" ]; then
  fail_with_status "$ENV_FILE exists without the v2 isolation marker"
fi

listeners="$(ss -ltnH | awk '$4 ~ /(^|:)18318$/ {{print}}' || true)"
if [ -n "$listeners" ] && ! docker inspect "$CONTAINER" >/dev/null 2>&1; then
  fail_with_status 'port 18318 is already occupied by an unknown process'
fi

if docker inspect "$ORIGINAL_SERVICE" >/dev/null 2>&1; then
  if [ "$(docker inspect --format='{{{{.State.Running}}}}' "$ORIGINAL_SERVICE")" = "true" ]; then
    old_original_running=1
  fi
fi

mkdir -p "$BACKUP_DIR" "$DATA_DIR" "$STAGE_DIR"
chmod 700 "$DATA_ROOT" "$DATA_DIR"
printf 'cpa-manager-plus-v2\\n' > "$DATA_ROOT/.isolation-id"
chmod 644 "$DATA_ROOT/.isolation-id"
if [ -d "$DEPLOY_DIR" ]; then
  tar --exclude='node_modules' --exclude='dist' --exclude='dist-demo' --exclude='dist-ssr' \\
    -czf "$BACKUP_DIR/source.tar.gz" -C "$DEPLOY_DIR" .
  mv "$DEPLOY_DIR" "$BACKUP_DIR/source"
  rollback_base="$BACKUP_DIR/source"
fi
if [ -f "$ENV_FILE" ]; then
  cp -p "$ENV_FILE" "$BACKUP_DIR/environment"
  chmod 600 "$BACKUP_DIR/environment"
fi

if ! tar -xzf "$TARBALL" --no-same-owner -C "$STAGE_DIR"; then
  fail_with_status 'source extraction failed'
fi
printf 'cpa-manager-plus-v2\\n' > "$STAGE_DIR/.isolation-id"
chmod 644 "$STAGE_DIR/.isolation-id"

if [ ! -f "$ENV_FILE" ]; then
  umask 077
  printf 'CPA_MANAGER_ADMIN_KEY=cpamp_v2_%s\\n' "$(openssl rand -hex 32)" > "$ENV_FILE"
fi
if ! grep -q '^CPA_MANAGER_ADMIN_KEY=' "$ENV_FILE"; then
  printf 'CPA_MANAGER_ADMIN_KEY=cpamp_v2_%s\\n' "$(openssl rand -hex 32)" >> "$ENV_FILE"
fi
printf 'cpa-manager-plus-v2\\n' > "$ENV_MARKER"
chmod 600 "$ENV_FILE"
chmod 644 "$ENV_MARKER"

cat > "$STAGE_DIR/docker-compose.deploy.yml" <<EOF
name: $PROJECT
services:
  manager-v2:
    container_name: $SERVICE
    image: $IMAGE:$VERSION
    restart: unless-stopped
    env_file:
      - $ENV_FILE
    environment:
      HTTP_ADDR: "0.0.0.0:$PORT"
      USAGE_DATA_DIR: "/data"
      USAGE_DB_PATH: "/data/usage.sqlite"
      CPA_MANAGER_DATA_KEY_PATH: "/data/data.key"
      USAGE_COLLECTOR_MODE: "auto"
      USAGE_RESP_QUEUE: "usage"
      USAGE_RESP_POP_SIDE: "right"
      USAGE_BATCH_SIZE: "100"
      USAGE_POLL_INTERVAL_MS: "500"
      USAGE_QUERY_LIMIT: "50000"
      USAGE_CORS_ORIGINS: "*"
    ports:
      - "$PORT:$PORT"
    volumes:
      - $DATA_DIR:/data
    networks:
      - $NETWORK
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:$PORT/health"]
      interval: 10s
      timeout: 3s
      retries: 12
      start_period: 10s

networks:
  $NETWORK:
    name: $NETWORK
EOF

printf 'BUILD_VERSION=%s\\n' "$VERSION" > "$STAGE_DIR/.build-version"
printf 'SOURCE_COMMIT=%s\\n' "$SOURCE_COMMIT" >> "$STAGE_DIR/.build-version"

printf 'Building isolated image.\\n'
docker build --pull --file "$STAGE_DIR/Dockerfile.manager-server" --label "org.opencontainers.image.version=$VERSION" \\
  --label "org.opencontainers.image.revision=$SOURCE_COMMIT" \\
  --build-arg "VERSION=$VERSION" \\
  --build-arg "SOURCE_COMMIT=$SOURCE_COMMIT" \\
  -t "$IMAGE:$VERSION" "$STAGE_DIR"

mv "$STAGE_DIR" "$DEPLOY_DIR"

docker compose -p "$PROJECT" -f "$COMPOSE_FILE" up -d --no-build --force-recreate --remove-orphans

if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
  ufw allow "$PORT/tcp" comment 'CPA Manager Plus v2'
fi

healthy=0
for attempt in $(seq 1 120); do
  running="$(docker inspect --format='{{{{.State.Running}}}}' "$CONTAINER" 2>/dev/null || true)"
  health="$(docker inspect --format='{{{{.State.Health.Status}}}}' "$CONTAINER" 2>/dev/null || true)"
  if [ "$running" = "true" ] && [ "$health" = "healthy" ] && curl --noproxy '*' -fsS --max-time 3 -o /dev/null "http://127.0.0.1:$PORT/health" 2>/dev/null; then
    healthy=1
    break
  fi
  sleep 1
done
if [ "$healthy" != "1" ]; then
  docker compose -p "$PROJECT" -f "$COMPOSE_FILE" ps || true
  docker compose -p "$PROJECT" -f "$COMPOSE_FILE" logs --tail=120 > "$BACKUP_DIR/compose-failure.log" 2>&1 || true
  chmod 600 "$BACKUP_DIR/compose-failure.log" || true
  rollback_previous
  fail_with_status 'isolated service did not become healthy within 120 seconds'
fi

running_version="$(docker run --rm --entrypoint /usr/local/bin/cpa-manager-plus "$IMAGE:$VERSION" --version | tr -d '\\r\\n')"
[ "$running_version" = "$VERSION" ] || fail_with_status "binary version mismatch: $running_version"
image_revision="$(docker image inspect --format='{{{{index .Config.Labels "org.opencontainers.image.revision"}}}}' "$IMAGE:$VERSION")"
[ "$image_revision" = "$SOURCE_COMMIT" ] || fail_with_status "image revision mismatch: $image_revision"

network_mode="$(docker inspect --format='{{{{.HostConfig.NetworkMode}}}}' "$CONTAINER")"
[ "$network_mode" != "host" ] || fail_with_status 'isolated service unexpectedly uses host networking'
mounts="$(docker inspect --format='{{{{range .Mounts}}}}{{{{.Source}}}}->{{{{.Destination}}}};{{{{end}}}}' "$CONTAINER")"
case "$mounts" in
  *'/opt/cli-proxy-api'*|*'cpa-manager-plus_cpa-manager-plus-data'*)
    fail_with_status 'isolated service shares a mount with the original service'
    ;;
esac
network_list="$(docker inspect --format='{{{{range $name, $network := .NetworkSettings.Networks}}}}{{{{printf "%s " $name}}}}{{{{end}}}}' "$CONTAINER")"
case " $network_list " in
  *" $NETWORK "*) ;;
  *) fail_with_status 'isolated service is not attached to the dedicated network' ;;
esac
if [ "$(printf '%s' "$network_list" | wc -w)" -ne 1 ]; then
  fail_with_status "isolated service has unexpected network attachments: $network_list"
fi
port_mapping="$(docker port "$CONTAINER" "$PORT/tcp")"
printf '%s\\n' "$port_mapping" | grep -qE "(^|:)${{PORT}}($|/)" || fail_with_status "unexpected port mapping: $port_mapping"

if [ "$old_original_running" = "1" ]; then
  [ "$(docker inspect --format='{{{{.State.Running}}}}' "$ORIGINAL_SERVICE")" = "true" ] || fail_with_status 'original service stopped during isolated deployment'
  curl -fsS --max-time 5 -o /dev/null "http://127.0.0.1:$ORIGINAL_PORT/health" || fail_with_status 'original service health changed during isolated deployment'
fi

printf 'container=%s\\n' "$CONTAINER"
printf 'container_health=%s\\n' "$(docker inspect --format='{{{{.State.Health.Status}}}}' "$CONTAINER")"
printf 'binary_version=%s\\n' "$running_version"
printf 'image_revision=%s\\n' "$image_revision"
printf 'network_mode=%s\\n' "$network_mode"
printf 'network=%s\\n' "$network_list"
printf 'port_mapping=%s\\n' "$port_mapping"
printf 'data_dir=%s\\n' "$DATA_DIR"
printf 'backup_dir=%s\\n' "$BACKUP_DIR"
printf 'remote_log=%s/deploy-%s-%s.log\\n' "$LOG_ROOT" "$VERSION" "$STAMP"
printf 'original_service_preserved=true\\n'
printf 'DEPLOY_RESULT=success\\n'

rm -f "$TARBALL" "$REMOTE_SCRIPT"
'''
    template = template.replace("{{", "{").replace("}}", "}")
    for placeholder, value in {
        "@VERSION@": values["version"],
        "@SOURCE_COMMIT@": values["source_commit"],
        "@STAMP@": values["stamp"],
        "@REMOTE_TARBALL@": values["remote_tarball"],
        "@REMOTE_SCRIPT@": values["remote_script"],
        "@DEPLOY_DIR@": values["deploy_dir"],
        "@DATA_ROOT@": values["data_root"],
        "@DATA_DIR@": values["data_dir"],
        "@ENV_FILE@": values["env_file"],
        "@ENV_MARKER@": values["env_marker"],
        "@BACKUP_ROOT@": values["backup_root"],
        "@LOG_ROOT@": values["log_root"],
        "@PROJECT@": values["project"],
        "@SERVICE@": values["service"],
        "@NETWORK@": values["network"],
        "@IMAGE@": values["image"],
        "@ORIGINAL_SERVICE@": values["original_service"],
        "@PORT@": str(SERVICE_PORT),
        "@ORIGINAL_PORT@": str(ORIGINAL_SERVICE_PORT),
    }.items():
        template = template.replace(placeholder, value)
    return template


def deploy(
    logger: Logger,
    archive_path: Path,
    version: str,
    source_commit: str,
    stamp: str,
) -> None:
    remote_tarball = f"{REMOTE_TARBALL_PREFIX}-{stamp}.tar.gz"
    remote_script = f"{REMOTE_SCRIPT_PREFIX}-{stamp}.sh"
    script_text = render_remote_script(
        version=version,
        source_commit=source_commit,
        stamp=stamp,
        remote_tarball=remote_tarball,
        remote_script=remote_script,
    )
    with tempfile.TemporaryDirectory(prefix="cpa-manager-plus-v2-script-") as temp_dir:
        local_script = Path(temp_dir) / "deploy.sh"
        local_script.write_text(script_text.replace("\r\n", "\n"), encoding="utf-8", newline="\n")
        run_checked(logger, scp_command(archive_path, remote_tarball), "Uploading LF-normalized source archive")
        run_checked(logger, scp_command(local_script, remote_script), "Uploading standalone remote deploy script")
        try:
            run_checked(
                logger,
                ssh_command(f"sudo bash {shlex.quote(remote_script)}"),
                "Building and deploying isolated service on Oracle 01",
            )
        finally:
            cleanup_command = f"sudo rm -f {shlex.quote(remote_script)}"
            try:
                run_checked(logger, ssh_command(cleanup_command), "Cleaning remote deploy script")
            except Exception as cleanup_error:
                logger.write(f"Remote deploy script cleanup skipped: {cleanup_error}")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Deploy CPA Manager Plus my-feature-v2 as a physically isolated service on Oracle 01."
    )
    parser.add_argument(
        "--yes",
        action="store_true",
        help="Confirm the isolated deployment and allow remote changes.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if not args.yes:
        print("Refusing remote changes without --yes.", file=sys.stderr)
        return 2

    stamp = datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")
    log_path = PROJECT_ROOT / "deploy" / f"deploy_manager_server_v2-{stamp}.log"
    logger = Logger(log_path)
    archive_path: Path | None = None
    archive_temp_dir: Path | None = None
    try:
        logger.write(f"log: {log_path}")
        logger.write(f"project_root: {PROJECT_ROOT}")
        git_status(logger)
        source_commit = run_capture(git_command("rev-parse", "HEAD"), PROJECT_ROOT)
        stable_tag = stable_tag_for_head()
        previous_version = remote_marker(logger)
        version = next_build_version(stable_tag, previous_version, logger)
        archive_path, archive_temp_dir = create_lf_archive(logger, source_commit, stamp)
        deploy(logger, archive_path, version, source_commit, stamp)
        logger.section("Deployment complete")
        logger.write(f"branch: {BRANCH}")
        logger.write(f"commit: {source_commit}")
        logger.write(f"version: {version}")
        logger.write(f"service: {SERVICE_NAME}")
        logger.write(f"port: {SERVICE_PORT}")
        logger.write(f"log: {log_path}")
        return 0
    except Exception as error:
        logger.section("Deployment failed")
        logger.write(str(error))
        logger.write(f"log: {log_path}")
        return 1
    finally:
        if archive_temp_dir is not None:
            shutil.rmtree(archive_temp_dir, ignore_errors=True)
        logger.close()


if __name__ == "__main__":
    sys.exit(main())
