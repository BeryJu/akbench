#!/usr/bin/env bash
# Drives the web UI end to end without a Kubernetes cluster.
#
# Starts the envtest-backed harness (control plane + bench CRDs + stub
# Prometheus + the real web API), starts the built Next.js server against it,
# then runs the Playwright suite. Everything it starts is torn down on exit,
# including on failure.
#
# Usage: KUBEBUILDER_ASSETS=$(setup-envtest use -p path) hack/webui-e2e.sh
# or just: make test-webui
set -euo pipefail
# Job control: puts each `&` job in its own process group, so cleanup can
# signal "-$pid" (the group) rather than just $pid. Needed because both
# background jobs below fork further (go run execs a temp binary; npm run
# start execs next-server as a child) - killing only the tracked PID left
# those actual servers running as orphans, still bound to :8080/:3000, which
# broke the next invocation with EADDRINUSE.
set -m

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
frontend="${root}/web/frontend"

if [[ -z "${KUBEBUILDER_ASSETS:-}" ]]; then
  echo "KUBEBUILDER_ASSETS is not set; run this through 'make test-webui'" >&2
  exit 1
fi

pids=()
cleanup() {
  for pid in "${pids[@]:-}"; do
    kill -TERM -- "-${pid}" 2>/dev/null || kill "${pid}" 2>/dev/null || true
  done
  wait 2>/dev/null || true
}
trap cleanup EXIT

wait_for() {
  local url="$1" name="$2" attempt
  for attempt in $(seq 1 60); do
    if curl -fsS -o /dev/null "${url}"; then
      return 0
    fi
    sleep 1
  done
  echo "${name} did not come up at ${url}" >&2
  return 1
}

echo "==> Starting the envtest-backed API harness"
(cd "${root}" && go run ./hack/webui-harness) &
pids+=($!)
wait_for "http://127.0.0.1:8080/healthz" "web API"

echo "==> Building the frontend"
(cd "${frontend}" && npm run build >/dev/null)

echo "==> Starting the frontend"
(cd "${frontend}" && BENCH_API_URL="http://127.0.0.1:8080" BENCH_API_TOKEN="e2e-token" npm run start >/dev/null) &
pids+=($!)
wait_for "http://127.0.0.1:3000/clusters" "frontend"

echo "==> Running Playwright"
(cd "${frontend}" && npx playwright test "$@")
