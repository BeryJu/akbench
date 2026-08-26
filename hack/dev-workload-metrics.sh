#!/usr/bin/env bash
# Deploys the same minimal dev Prometheus (hack/dev-prometheus.yaml) onto a
# workload cluster, and exposes it via a NodePort so cmd/apiserver (running
# on the management cluster) can query it.
#
# Why this exists: k6 workloads always remote-write to
# prometheus-server.bench-system.svc.cluster.local (see
# defaultRemoteWriteURL in internal/controller/testcase_controller.go) -
# cluster-relative, so it "just works" once a Prometheus with that name
# exists on whichever cluster the k6 pod actually runs on. But a workload
# cluster's pod network has no route back into the management cluster's pod
# network (confirmed via direct ping - 100% packet loss to the minikube
# node's own IP from a workload-cluster pod), so remote-write to the
# management cluster's central Prometheus silently fails forever
# ("Failed to send the time series data", visible in `kubectl logs` on the
# k6 pod) even though the topology/testcase both report Ready. The reverse
# direction does work (a management-cluster pod CAN reach a workload
# cluster's node IP directly), which is what makes a NodePort here usable
# from cmd/apiserver.
#
# This is a single-workload-cluster dev workaround, not the planned M4
# design (a central mTLS-secured receiver plus a Grafana Alloy agent per
# workload cluster, still not built) - it does not scale past one workload
# cluster, since cmd/apiserver's BENCH_PROMETHEUS_URL only points at one
# Prometheus at a time (see hack/dev-apiserver.yaml).
set -euo pipefail

cluster="${1:?usage: hack/dev-workload-metrics.sh <workload-cluster-name>}"
secret="${cluster}-kubeconfig"

management_context="$(kubectl config current-context)"
kubeconfig_dir="$(mktemp -d)"
trap 'rm -rf "$kubeconfig_dir"' EXIT
workload_kubeconfig="$kubeconfig_dir/kubeconfig"

echo "==> Extracting kubeconfig from Secret/$secret"
kubectl --context "$management_context" get secret "$secret" -o jsonpath='{.data.value}' | base64 -d > "$workload_kubeconfig"

echo "==> Deploying hack/dev-prometheus.yaml onto $cluster"
kubectl --kubeconfig "$workload_kubeconfig" apply -f "$(dirname "$0")/dev-prometheus.yaml"
kubectl --kubeconfig "$workload_kubeconfig" rollout status deploy/prometheus-server -n bench-system --timeout=90s

echo "==> Exposing it via NodePort"
kubectl --kubeconfig "$workload_kubeconfig" expose deployment prometheus-server -n bench-system \
  --name=prometheus-server-nodeport --type=NodePort --port=9090 --target-port=9090 \
  --dry-run=client -o yaml | kubectl --kubeconfig "$workload_kubeconfig" apply -f -

node_port="$(kubectl --kubeconfig "$workload_kubeconfig" get svc -n bench-system prometheus-server-nodeport -o jsonpath='{.spec.ports[0].nodePort}')"
node_ip="$(kubectl --kubeconfig "$workload_kubeconfig" get nodes -l node-role.kubernetes.io/control-plane -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')"
if [ -z "$node_ip" ]; then
  node_ip="$(kubectl --kubeconfig "$workload_kubeconfig" get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')"
fi

echo
echo "==> Prometheus for $cluster is reachable from the management cluster at:"
echo "    http://${node_ip}:${node_port}"
echo
echo "Set this as BENCH_PROMETHEUS_URL in hack/dev-apiserver.yaml, then"
echo "'make deploy-apiserver' (or a plain rollout restart if the image is"
echo "unchanged) to pick it up."
