#!/usr/bin/env bash
# Builds the single-container appliance and runs it in local Docker, for
# working on the appliance itself. The operator chart and image come from
# GHCR at AKBENCH_VERSION (default: the latest published main build). The
# apiserver image is built from this checkout under that same tag and
# side-loaded through k3s's airgap image import (any tarball under
# agent/images is imported when k3s starts), so local apiserver/UI
# changes run without pushing anything.
#
# Usage: make appliance-dev [AKBENCH_VERSION=0.0.0-main.<sha>]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAME="${AKBENCH_CONTAINER:-akbench-dev}"
VERSION="${AKBENCH_VERSION:-0.0.0-main.$(git -C "$ROOT" rev-parse --short origin/main)}"
APPLIANCE_IMG="akbench-appliance:dev"
APISERVER_IMG="ghcr.io/beryju/akbench-apiserver:${VERSION}"
# Kept (not a temp dir): it stays bind-mounted into the running container.
IMAGES_DIR="$ROOT/bin/appliance-images"
mkdir -p "$IMAGES_DIR"

docker build -f "$ROOT/Dockerfile.appliance" --build-arg VERSION="$VERSION" -t "$APPLIANCE_IMG" "$ROOT"
docker build -f "$ROOT/Dockerfile.apiserver" -t "$APISERVER_IMG" "$ROOT"
docker save -o "$IMAGES_DIR/apiserver.tar" "$APISERVER_IMG"

docker rm -f "$NAME" >/dev/null 2>&1 || true
docker volume create "$NAME" >/dev/null
docker run -d --name "$NAME" --privileged \
    -v "$NAME:/var/lib/rancher/k3s" \
    -v "$IMAGES_DIR/apiserver.tar:/var/lib/rancher/k3s/agent/images/apiserver.tar:ro" \
    -p 8080:8080 -p 6443:6443 \
    "$APPLIANCE_IMG" >/dev/null

cat <<EOF

Started $NAME (akbench $VERSION). Follow the bootstrap with:
  docker logs -f $NAME 2>&1 | grep '\[akbench\]'
Then open http://localhost:8080 with the token from:
  docker exec $NAME akbench-appliance token
EOF
