#!/usr/bin/env bash
# Builds web/headlamp-plugin's image (web/headlamp-plugin/Dockerfile) and
# re-installs Headlamp with it wired in as an initContainer
# (hack/dev-headlamp-values.yaml), so the plugin survives a Headlamp pod
# restart. For fast dev-loop iteration without a full image
# build/reload/helm-upgrade round trip, use `make headlamp-plugin`
# (hack/build-headlamp-plugin.sh) instead - it kubectl cp's straight into
# the running pod, but is wiped the next time that pod restarts.
#
# Tags with the current commit (plus a dirty-tree suffix) rather than a
# fixed "dev" tag - same reasoning as hack/deploy-apiserver.sh: verified
# live that `minikube image load`'s own --overwrite=true does not actually
# replace an already-loaded tag's content in this environment.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NAMESPACE="${BENCH_HEADLAMP_NAMESPACE:-headlamp}"
RELEASE="${BENCH_HEADLAMP_RELEASE:-headlamp}"

TAG="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo dev)"
if ! git -C "$ROOT" diff --quiet 2>/dev/null || ! git -C "$ROOT" diff --cached --quiet 2>/dev/null; then
    TAG="$TAG-$(date +%s)"
fi
IMAGE="${BENCH_HEADLAMP_PLUGIN_IMAGE_REPO:-authentik-bench-headlamp-plugin}:$TAG"

docker build -t "$IMAGE" "$ROOT/web/headlamp-plugin"
minikube image load "$IMAGE"

helm repo add headlamp https://kubernetes-sigs.github.io/headlamp/ >/dev/null 2>&1 || true
helm repo update headlamp >/dev/null

sed "s|authentik-bench-headlamp-plugin:dev|$IMAGE|" "$ROOT/hack/dev-headlamp-values.yaml" >/tmp/dev-headlamp-values.rendered.yaml
helm upgrade --install "$RELEASE" headlamp/headlamp \
    --namespace "$NAMESPACE" --create-namespace \
    --values /tmp/dev-headlamp-values.rendered.yaml
rm -f /tmp/dev-headlamp-values.rendered.yaml

kubectl -n "$NAMESPACE" rollout status deployment/"$RELEASE" --timeout=90s

echo "Deployed $IMAGE" >&2
