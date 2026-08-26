#!/usr/bin/env bash
# Installs/upgrades Headlamp (the official headlamp-k8s/headlamp chart) on
# the current kubeconfig context's cluster - this project's management
# cluster, replacing the old web/frontend Next.js UI. See
# hack/dev-headlamp-values.yaml for the values this passes.
set -euo pipefail

NAMESPACE="${BENCH_HEADLAMP_NAMESPACE:-headlamp}"
RELEASE="${BENCH_HEADLAMP_RELEASE:-headlamp}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

helm repo add headlamp https://kubernetes-sigs.github.io/headlamp/ >/dev/null 2>&1 || true
helm repo update headlamp >/dev/null

helm upgrade --install "$RELEASE" headlamp/headlamp \
    --namespace "$NAMESPACE" --create-namespace \
    --values "$ROOT/hack/dev-headlamp-values.yaml"

echo "Headlamp installed. Port-forward with:" >&2
echo "  kubectl port-forward -n $NAMESPACE svc/$RELEASE 8087:80" >&2
