#!/usr/bin/env bash
# Deploy the API on the Docker host. Run from the checkout on the host:
#   cd ~/helderberg-social && git pull --ff-only && bash api/deploy.sh
# Builds the image from the current commit, restarts the container and
# checks the health route. Refuses to run without api/.env.
set -euo pipefail
cd "$(dirname "$0")"

[ -f .env ] || { echo "api/.env missing: copy .env.example and fill it in (chmod 600)"; exit 1; }
perm=$(stat -c %a .env); [ "$perm" = "600" ] || { echo ".env must be chmod 600 (is $perm)"; exit 1; }

VERSION=$(git describe --always --dirty 2>/dev/null || echo dev)
export HS_VERSION="$VERSION"
BIND=$(grep -E '^HS_BIND_IP=' .env | cut -d= -f2)
export HS_BIND_IP="${BIND:-127.0.0.1}"

echo "building helderberg-social-api:$VERSION"
docker compose build --pull --quiet
docker compose up -d --remove-orphans

for i in $(seq 1 20); do
  sleep 2
  if curl -fsS --max-time 3 "http://${HS_BIND_IP}:8102/api/health" >/dev/null 2>&1; then
    echo "healthy: $(curl -s http://${HS_BIND_IP}:8102/api/health)"
    docker image prune -f --filter "label=none" >/dev/null 2>&1 || true
    exit 0
  fi
done
echo "container did not become healthy; last log lines:"
docker logs --tail 40 helderberg-social-api
exit 1
