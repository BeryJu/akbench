#!/usr/bin/env bash
# Starts the operator manager and the web API backend against the current
# kubeconfig context, in one foreground command. Ctrl+C stops both. The web
# UI itself is Headlamp (see `make headlamp`/`make headlamp-plugin`), not
# started here - it's a separate, longer-lived deployment, not a per-session
# dev process.
#
# Usage: make dev
set -euo pipefail
# Job control: puts each `&` job in its own process group, so cleanup can
# signal "-$pid" (the group) rather than just $pid. Needed because `go run`
# forks further (a temp binary) - killing only the tracked PID would leave
# that actual process running as an orphan still bound to its port. See
# hack/webui-e2e.sh, which hit exactly this.
set -m

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

BENCH_API_TOKEN="${BENCH_API_TOKEN:-dev-token}"
export BENCH_API_TOKEN

pids=()
cleanup() {
  echo "==> Stopping dev environment"
  for pid in "${pids[@]:-}"; do
    kill -TERM -- "-${pid}" 2>/dev/null || kill "${pid}" 2>/dev/null || true
  done
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

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

echo "==> Installing CRDs into the current kubeconfig context ($(kubectl config current-context 2>/dev/null || echo unknown))"
(cd "${root}" && make install)

# Best-effort: the operator/API both work without it, but every metrics
# feature (compare, the dashboard's live charts, a TestCase's own metrics)
# just reports "no Prometheus configured" - see hack/dev-prometheus.yaml for
# how to stand one up locally if this doesn't find one.
if [[ -z "${BENCH_PROMETHEUS_URL:-}" ]] && kubectl get svc prometheus-server -n bench-system >/dev/null 2>&1; then
  echo "==> Port-forwarding bench-system/prometheus-server to :9091"
  kubectl port-forward -n bench-system svc/prometheus-server 9091:80 >/dev/null 2>&1 &
  pids+=($!)
  BENCH_PROMETHEUS_URL="http://127.0.0.1:9091"
fi
export BENCH_PROMETHEUS_URL="${BENCH_PROMETHEUS_URL:-}"

echo "==> Starting the operator manager"
(cd "${root}" && go run ./cmd/main.go) &
pids+=($!)
wait_for "http://127.0.0.1:8081/healthz" "operator manager"

echo "==> Starting the web API backend"
(cd "${root}" && go run ./cmd/apiserver) &
pids+=($!)
wait_for "http://127.0.0.1:8080/healthz" "web API"

cat <<EOF

authentik-bench dev environment is up:
  API backend       http://localhost:8080  (token: ${BENCH_API_TOKEN:-dev-token})
  operator manager  health probe on :8081

Web UI: run \`make headlamp\` once (deploys Headlamp in-cluster) and
\`make headlamp-plugin\` after any plugin change, then port-forward it:
  kubectl port-forward -n headlamp svc/headlamp 8087:80

Press Ctrl+C to stop everything.
EOF

wait
