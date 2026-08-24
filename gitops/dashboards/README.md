# Dashboards

Five Grafana dashboards, loaded by kube-prometheus-stack's Grafana sidecar via
the `grafana_dashboard: "1"` label on the ConfigMaps `kustomization.yaml`
generates. Their UIDs are the allowlist `internal/api`'s
`/api/v1/grafana/embed-url` serves, so a UID here and a UID there are kept in
step by `TestDashboardsMatchEmbedAllowlist`.

| File | UID | What it answers |
| --- | --- | --- |
| `compare-tests.json` | `akbench-compare` | How do two or more test runs compare, split on `testid`? |
| `k6.json` | `akbench-k6` | What did the load generator see? |
| `authentik.json` | `akbench-authentik` | What did the authentik pods cost, per pod and per node? |
| `postgres.json` | `akbench-postgres` | What did the database do? |
| `nodes.json` | `akbench-nodes` | Did the underlying nodes have headroom left? |

## Label scheme

`testid`, `target` and `profile` are k6 `--tag`s and mean exactly what they
meant in the Ansible harness these replace. Two labels changed with the move to
Kubernetes:

- `host`, the VM a target ran on, has no equivalent - authentik runs in pods
  now. The `authentik` dashboard uses `pod` instead, and derives `node` by
  joining through `kube_pod_info`, which is what makes the `Dedicated` vs
  `Default` placement comparison legible.
- `cluster` is new: Grafana Alloy attaches it per workload cluster from the
  multi-cluster milestone on. Every dashboard matches it as a regex, so a
  single-cluster install - where no series carries the label yet - still
  matches everything.

## Caveats

- **k6 metric names** follow k6's `experimental-prometheus-rw` output rather
  than k6's documentation: counters get `_total`, rates `_rate`, gauges
  neither, and trends arrive as *native histograms* named after their base unit
  (`k6_http_req_duration_seconds`, in seconds - k6's own trends are in
  milliseconds). Quantiles are therefore taken over `sum(rate(...))` with no
  `le` grouping. `TestK6MetricNamesAreReal` guards the names.
- **`postgres.json` is empty until something scrapes `pg_*` series.** The
  operator does not deploy a postgres exporter for any of the three postgres
  modes yet.
- **`authentik.json` shows resource use, not authentik's own metrics.** Nothing
  scrapes the release's `/metrics` endpoint until the operator renders a
  `ServiceMonitor` for it.
- These dashboards were **authored against the label scheme, not ported from
  the Ansible harness's JSON**, which was not reachable from the machine this
  was built on. They cover the same questions; they are not the same files.
