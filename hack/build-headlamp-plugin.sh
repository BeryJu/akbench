#!/usr/bin/env bash
# Builds web/headlamp-plugin and copies its dist/ output into the running
# Headlamp pod's pluginsDir (an emptyDir - see hack/dev-headlamp-values.yaml
# - so this needs re-running after every Headlamp pod restart, including the
# one hack/install-headlamp.sh's own upgrades trigger). Dev-loop convenience
# only: a real release would build this into an image used as an
# initContainer instead, the way the chart's own README documents.
set -euo pipefail

NAMESPACE="${BENCH_HEADLAMP_NAMESPACE:-headlamp}"
PLUGIN_NAME="authentik-bench"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLUGIN_DIR="$ROOT/web/headlamp-plugin"

(cd "$PLUGIN_DIR" && npm run build >&2)

POD="$(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=headlamp -o jsonpath='{.items[0].metadata.name}')"
kubectl exec -n "$NAMESPACE" "$POD" -- mkdir -p "/headlamp/plugins/$PLUGIN_NAME"
kubectl cp "$PLUGIN_DIR/dist/." "$NAMESPACE/$POD:/headlamp/plugins/$PLUGIN_NAME/"
# `headlamp-plugin build` alone only emits dist/main.js - Headlamp's own
# compatibility check (frontend/src/plugin/index.js) reads
# devDependencies['@kinvolk/headlamp-plugin'] from a package.json it expects
# to find *next to* main.js, and treats a plugin with none as incompatible
# ("This plugin is not compatible with this version of Headlamp", verified
# live). `headlamp-plugin package` copies this same file alongside main.js
# when it assembles its tarball - matching that here rather than only
# shipping the JS bundle.
kubectl cp "$PLUGIN_DIR/package.json" "$NAMESPACE/$POD:/headlamp/plugins/$PLUGIN_NAME/package.json"

echo "Copied $PLUGIN_DIR/dist into $NAMESPACE/$POD:/headlamp/plugins/$PLUGIN_NAME" >&2
echo "Reload Headlamp in the browser to pick it up." >&2
