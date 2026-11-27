#!/usr/bin/env bash
# Deploy Homelab Guard to a remote Docker host over SSH.
#
#   ./remote-deploy.sh agent user@host      # remote agent managed by this instance
#   ./remote-deploy.sh full  user@host      # complete second instance
#
# The image is built locally and transferred with "podman save | ssh docker load",
# so no registry access or GHCR login is required on the remote host.
#
# Environment:
#   GUARD_SERVER_URL    server URL the agent must reach (default: http://192.168.0.120:9015)
#   GUARD_ENROLL_TOKEN  enrollment token (required for "agent"; create it from
#                       Settings -> Docker & Swarm -> "Déployer un nouvel agent")
#   MASTER_KEY          master key (required for "full")
#   SSH_KEY             path to the SSH private key (optional)
#   SSH_PORT            SSH port (default 22)
set -euo pipefail

MODE="${1:-}"
TARGET="${2:-}"
SERVER_URL="${GUARD_SERVER_URL:-http://192.168.0.120:9015}"
TOKEN="${GUARD_ENROLL_TOKEN:-}"
MASTER_KEY="${MASTER_KEY:-}"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

usage() {
  cat <<EOF
Usage: $0 <agent|full> <user@host>

  agent : deploy the lightweight agent on the remote host (Docker socket mounted)
          env: GUARD_SERVER_URL, GUARD_ENROLL_TOKEN
  full  : deploy a complete Homelab Guard instance (port 9015)
          env: MASTER_KEY

Examples:
  GUARD_ENROLL_TOKEN=hlg_xxx $0 agent root@192.168.0.26
  MASTER_KEY=\$(grep MASTER_KEY deploy/.env | cut -d= -f2) $0 full user@192.168.0.26
EOF
}

[[ -n "$MODE" && -n "$TARGET" ]] || { usage; exit 1; }

SSH_ARGS=(-o BatchMode=yes -o StrictHostKeyChecking=accept-new)
[[ -n "${SSH_KEY:-}" ]] && SSH_ARGS+=(-i "$SSH_KEY")
[[ -n "${SSH_PORT:-}" ]] && SSH_ARGS+=(-p "$SSH_PORT")

remote() { ssh "${SSH_ARGS[@]}" "$TARGET" "$@"; }

echo "==> Checking Docker on $TARGET"
remote 'command -v docker >/dev/null && docker version --format "{{.Server.Version}}" >/dev/null' \
  || { echo "Docker is not available on $TARGET"; exit 1; }

case "$MODE" in
  agent)
    [[ -n "$TOKEN" ]] || { echo "GUARD_ENROLL_TOKEN is required for mode=agent"; exit 1; }
    echo "==> Building agent image locally"
    podman build -q -t homelab-guard-agent:remote -f "$REPO_DIR/agent/Dockerfile" "$REPO_DIR"
    echo "==> Transferring image to $TARGET"
    podman save homelab-guard-agent:remote | remote 'docker load'
    remote 'docker tag localhost/homelab-guard-agent:remote homelab-guard-agent:remote 2>/dev/null || true'
    echo "==> Starting agent container"
    remote "docker rm -f homelab-guard-agent >/dev/null 2>&1 || true
      docker run -d --name homelab-guard-agent --restart unless-stopped \
        --user 0:0 \
        -v /var/run/docker.sock:/var/run/docker.sock \
        -e GUARD_SERVER_URL='$SERVER_URL' \
        -e GUARD_ENROLL_TOKEN='$TOKEN' \
        homelab-guard-agent:remote >/dev/null"
    sleep 2
    remote 'docker ps --filter name=homelab-guard-agent --format "{{.Names}} | {{.Status}}"'
    echo "==> Agent logs"
    remote 'docker logs --tail 20 homelab-guard-agent 2>&1'
    ;;

  full)
    [[ -n "$MASTER_KEY" ]] || { echo "MASTER_KEY is required for mode=full"; exit 1; }
    echo "==> Building server image locally"
    podman build -q -t homelab-guard:remote -f "$REPO_DIR/Dockerfile" "$REPO_DIR"
    echo "==> Transferring image to $TARGET"
    podman save homelab-guard:remote | remote 'docker load'
    remote 'docker tag localhost/homelab-guard:remote homelab-guard:remote 2>/dev/null || true'
    echo "==> Starting Homelab Guard"
    remote "docker rm -f homelab-guard >/dev/null 2>&1 || true
      docker run -d --name homelab-guard --restart unless-stopped \
        --user 0:0 \
        -p 9015:9015 \
        -v /var/run/docker.sock:/var/run/docker.sock \
        -v homelab-guard-data:/data \
        -e DOCKER_HOST=unix:///var/run/docker.sock \
        -e MASTER_KEY='$MASTER_KEY' \
        -e TZ=Europe/Paris \
        homelab-guard:remote >/dev/null"
    sleep 5
    remote 'docker ps --filter name=homelab-guard --format "{{.Names}} | {{.Status}} | {{.Ports}}"'
    echo "==> Web UI should be reachable at http://<remote-ip>:9015"
    ;;

  *)
    usage
    exit 1
    ;;
esac
