#!/usr/bin/env bash
# Builds cmd/apiserver's image and deploys it in-cluster (hack/dev-apiserver.yaml)
# so the Headlamp plugin can reach it through Headlamp's own ApiProxy. Local
# dev only: builds with plain docker and loads straight into minikube,
# rather than pushing to a registry.
#
# Tags with the current commit (plus a dirty-tree suffix) rather than a
# fixed "dev" tag: verified live that `minikube image load`'s own
# --overwrite=true (its documented default) does not actually replace an
# already-loaded tag's content in this environment - every reload kept
# serving the very first build no matter how many times the tag was
# rebuilt and reloaded, with no error either at load time or at pod
# start time to say so. A unique tag per build sidesteps that entirely
# rather than depending on a mechanism that's silently unreliable here.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

TAG="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo dev)"
if ! git -C "$ROOT" diff --quiet 2>/dev/null || ! git -C "$ROOT" diff --cached --quiet 2>/dev/null; then
    TAG="$TAG-$(date +%s)"
fi
IMAGE="${BENCH_APISERVER_IMAGE_REPO:-authentik-bench-apiserver}:$TAG"

docker build -f "$ROOT/Dockerfile.apiserver" -t "$IMAGE" "$ROOT"
minikube image load "$IMAGE"

sed "s|authentik-bench-apiserver:dev|$IMAGE|" "$ROOT/hack/dev-apiserver.yaml" | kubectl apply -f -
kubectl -n authentik-bench rollout status deployment/apiserver --timeout=60s

echo "Deployed $IMAGE" >&2
