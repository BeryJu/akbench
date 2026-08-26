#!/usr/bin/env bash
# Generates the synthetic-user blueprint + k6 credentials fixture and loads
# the credentials into the Secret every k6 workload mounts (see
# fixturesSecretName in internal/controller/fixtures.go). The blueprint
# itself still needs importing into the target authentik instance for the
# users to actually exist there - this only wires up the k6 side.
set -euo pipefail

NAMESPACE="${BENCH_NAMESPACE:-default}"
USERS="${BENCH_FIXTURE_USERS:-50}"
SEED="${BENCH_FIXTURE_SEED:-1337}"
SECRET_NAME="bench-fixtures-users"
BLUEPRINT_CONFIGMAP_NAME="bench-fixtures-blueprint"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BLUEPRINT_OUT="${BENCH_FIXTURE_BLUEPRINT_OUT:-/tmp/authentik-bench-fixtures-blueprint.yaml}"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

uv run --with faker --with pyyaml python3 "$ROOT/fixtures/gen-blueprint.py" \
    -n "$USERS" -s "$SEED" -o "$BLUEPRINT_OUT" -c "$WORKDIR/users.json"

kubectl create secret generic "$SECRET_NAME" -n "$NAMESPACE" \
    --from-file=users.json="$WORKDIR/users.json" \
    --dry-run=client -o yaml | kubectl apply -f -

# AuthentikTopologyReconciler reads this and mirrors+applies it onto each
# topology's own runner cluster - see fixturesBlueprintConfigMapName in
# internal/controller/fixtures.go.
kubectl create configmap "$BLUEPRINT_CONFIGMAP_NAME" -n "$NAMESPACE" \
    --from-file=blueprint.yaml="$BLUEPRINT_OUT" \
    --dry-run=client -o yaml | kubectl apply -f -

echo "blueprint written to $BLUEPRINT_OUT and loaded into configmap/$BLUEPRINT_CONFIGMAP_NAME" >&2
