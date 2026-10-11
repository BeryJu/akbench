# Dashboards

Five Grafana dashboards, embedded verbatim into
`internal/render/manifests/grafana-dashboards/` (`//go:embed` can't reach
outside its own package tree, so this is a byte-identical vendored copy, not
a symlink) and provisioned into every workload cluster's own Grafana by
`internal/render/grafana.go`. The two copies are kept in sync by
`TestDashboardsMatchRenderEmbeds` (`internal/api/dashboards_test.go`). The
Headlamp plugin's Grafana tab embeds them by UID through `cmd/apiserver`'s
`/grafana-proxy/{cluster}/...` reverse proxy (`internal/api/grafana_proxy.go`) -
not the older, since-deleted `internal/api` embed-URL allowlist this doc used
to reference, from before Grafana moved to a per-workload-cluster install.

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
- `cluster` was a reserved per-workload-cluster identifier that never got
  attached to any real series (each workload cluster runs its own
  Prometheus/Grafana - see `internal/render/metrics.go` - so there was never
  more than one cluster's worth of data behind any dashboard anyway). It was
  removed entirely from `k6.json`/`compare-tests.json` (the only two that had
  it) after it turned out to be a real bug, not a harmless no-op: it was a
  templated Grafana variable with `includeAll`, and since no series has a
  `cluster` label at all, its own discovery query returned zero options,
  leaving it unresolved - which cascaded into every panel that filtered on it
  (directly, or indirectly through `$testid`/`$target`/`$profile`, whose own
  discovery queries were themselves filtered by `$cluster`) showing no data at
  all, confirmed live.

## Caveats

- **k6 metric names** follow k6's `experimental-prometheus-rw` output rather
  than k6's documentation: counters get `_total`, rates `_rate`, gauges
  neither, and trends arrive as *native histograms* named after their base unit
  (`k6_http_req_duration_seconds`, in seconds - k6's own trends are in
  milliseconds). Quantiles are therefore taken over `sum(rate(...))` with no
  `le` grouping. `TestK6MetricNamesAreReal` guards the names.
- **`k6.json`'s "Failed requests" panel deliberately doesn't query
  `k6_dropped_iterations_total`.** That metric is only ever emitted by k6's
  arrival-rate executors; every scenario this project renders
  (`k6/scripts/login.ts`/`oauth.ts`) uses `per-vu-iterations` instead -
  confirmed live that the metric doesn't exist in Prometheus at all for this
  project, not just that it's currently zero.
- **"Failed requests" also doesn't average `k6_http_req_failed_rate`
  directly.** An earlier version did (`avg by (testid) (k6_http_req_failed_rate{...})`),
  and the same mistake was copied into `internal/render/alertrules.go`'s
  `AuthentikBenchHighRequestFailureRate` rule. k6 tags this metric with the
  full request context (status, method, url...), so it emits one series per
  distinct tag combination, and every series belonging to a non-2xx/3xx
  status is pinned at exactly 1 by definition - averaging across these series
  is not the real, request-volume-weighted failure proportion. Confirmed live
  against a real burn test: this read as "25%" (1 failing series out of 4
  distinct ones) when the true rate, computed from
  `k6_http_reqs_total{expected_response="false"}` over all `k6_http_reqs_total`,
  was 0.05% (105 failed of ~196,150 total requests). Both the panel and the
  alert rule now use that volume-weighted ratio instead.
- **`authentik.json`'s "CPU throttling" panel queries
  `container_pressure_cpu_waiting_seconds_total`, not
  `container_cpu_cfs_throttled_seconds_total`.** The latter is a cgroup v1
  metric; confirmed live that this project's nodes (cgroup v2) never expose
  it at all, only the newer PSI-based "some" CPU-pressure metric.
- A previous version of eight panels across every dashboard (`Disk I/O time`,
  `Where the time goes`, `Failures and dropped iterations`, `CPU by node`,
  `CPU throttling`, `Ready replicas and restarts`, `Connections`, `Exporter
  up`) had their explanatory text accidentally saved into Grafana's own
  `type` field instead of `description` - an invalid panel type, so none of
  them rendered as a real timeseries panel regardless of whether their
  underlying PromQL/data was fine. Fixed; every panel's `type` is now a real
  Grafana panel type again.
- These dashboards were **authored against the label scheme, not ported from
  the Ansible harness's JSON**, which was not reachable from the machine this
  was built on. They cover the same questions; they are not the same files.
