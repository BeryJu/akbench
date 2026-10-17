# authentik-bench

A Kubernetes-native replacement for the Ansible/docker-compose authentik
benchmark harness at
[BeryJu/authentik-benchmark](https://github.com/BeryJu/authentik-benchmark):
an operator plus a web UI (with an in-browser `kubectl` shell) for defining
k6 load-test cases against different authentik deployment topologies -
replica counts, node placement (dedicated vs. shared nodes), and Postgres
backend (embedded subchart, CloudNativePG HA, or external) - with
multi-cluster provisioning via Cluster API and a Prometheus/Grafana/
Pyroscope/Tempo observability stack installed per workload cluster.

See the design plan this repo implements for the full architecture and
phased delivery milestones (M1-M5).

## Status: multi-cluster provisioning, test-case templates, and per-cluster observability

Implemented so far:

- **CRDs** (`bench.goauthentik.io/v1alpha1`): `AuthentikDeployment`, `TestCase`,
  `TestCaseTemplate`, `TestRun`, `WorkloadCluster`.
- **`WorkloadClusterReconciler`**: renders a full Cluster API cluster stack
  (`Cluster`/`LXCCluster`/`KubeadmControlPlane`/`MachineDeployment` plus their
  `LXCMachineTemplate`s and a worker `KubeadmConfigTemplate`) against the
  Incus infrastructure provider already installed on the management cluster,
  and mirrors the resulting `Cluster`'s readiness (and its
  `<name>-kubeconfig` Secret) back onto its own status. Control-plane size
  and Kubernetes version are create-once; a worker pool's own `replicas`/
  flavor are a supported day-2 edit, applied in place
  (`applyMachineDeploymentPool`). A pool can also opt into
  `spec.autoscaling{minReplicas,maxReplicas}`, which installs
  [cluster-autoscaler](https://github.com/kubernetes/autoscaler/tree/master/cluster-autoscaler/cloudprovider/clusterapi)
  for it (one instance per `WorkloadCluster` that uses it, torn down again
  once no pool does) - `.spec.replicas` stops being reconciled for an
  autoscaled pool once set, so it never fights cluster-autoscaler's own
  scale-up/down.
  It also installs, once the control plane is reachable: CNI (Flannel),
  metrics-server, csi-incus (the cluster's only `StorageClass`, backed by a
  real Incus storage pool), and - in the `bench-system` namespace -
  kube-prometheus-stack (Prometheus with the remote-write receiver k6 pushes
  to, node-exporter, kube-state-metrics), Grafana (anonymous-admin, reachable
  through `cmd/apiserver`'s own reverse proxy - see below), Pyroscope,
  and Tempo.
- **`clusterRef`/`runnerClusterRef`**: `AuthentikDeploymentReconciler`,
  `TestCaseReconciler`, and `TestRunReconciler` all resolve these to a live
  client via `internal/multicluster.Tracker` (wrapping Cluster API's
  `ClusterCache`). An AuthentikDeployment must set `clusterRef`: the
  management cluster is rejected at admission.
  Cross-cluster objects (namespace, CNPG `Cluster`, k6 workloads, generated
  Secrets) skip ownerReferences - a remote cluster's garbage collector can't
  recognize a UID from a different cluster's etcd - and clean up through each
  controller's finalizer instead.
- **`AuthentikDeploymentReconciler`**: renders a Flux `HelmRelease` +
  `HelmRepository` installing the official authentik chart
  (`charts.goauthentik.io`) with the deployment's replica/resource/placement
  values. All three Postgres modes work: `Embedded` (bundled subchart, with a
  generated password pinned on both sides so they agree), `CNPG` (renders a
  `postgresql.cnpg.io/v1 Cluster`, waits for it healthy, reads its generated
  app secret), `External` (reads a user-supplied secret directly). Placement's
  `Dedicated` mode uses the chart's own `affinity.podAntiAffinity: hard`
  preset; `Default` explicitly sets `none` so pods can share a node (the
  chart's own root default is `soft`, which would already bias them apart).
  Deleting a deployment deletes its `HelmRelease` first and waits for
  helm-controller's own `helm uninstall` before sweeping the target
  namespace - required because the chart's `ClusterRole`/`ClusterRoleBinding`
  are cluster-scoped and a plain namespace delete can never reach them,
  orphaning them to block every future install under the same release name.
- **`TestCaseReconciler`**: `Burn` profile owns a self-healing k6
  `Deployment`; `Quick` profile just validates readiness (execution belongs
  to a `TestRun`). Mirrors the built-in k6 script into a `ConfigMap`.
- **`TestRunReconciler`**: executes one bounded `Quick` run as a k6 `Job`,
  tracks it to completion, and posts start/completion Grafana annotations to
  whichever Grafana `WorkloadClusterReconciler` installed for the target
  deployment's own cluster - reproducing `roles/runner/tasks/annotate.yml`'s
  behavior.
- **k6 tag/env contract**: `k6/scripts/login.ts` is ported byte-for-byte from
  the reference repo; `internal/render` reproduces its exact
  `testid`/`target`/`profile` tag scheme and env var contract
  (`TEST_ID`/`TARGET`/`BENCH_HOST`/... ), guarded by `test/contract`.
- **Fixtures**: the operator seeds synthetic test users at startup
  (`internal/fixtures`, a Go port of `fixtures/gen-blueprint.py`) unless
  they're already loaded - 50 by default (`BENCH_FIXTURE_USERS`), a random
  directory per install unless `BENCH_FIXTURE_SEED` is set.
  `make fixtures` (the original script) still replaces them on demand.
- **Test-case templates**: `TestCaseTemplate` is a real, UI-editable
  CRD (list/create/edit/delete, Monaco editor for the script) - the operator
  seeds seven built-in ones at startup (`login`; `oauth-oidc`, driving a
  real OAuth2/OIDC authorization-code round trip through
  [BeryJu/oidc-test-client](https://github.com/BeryJu/oidc-test-client);
  `saml`, an SP-initiated SAML login through
  [BeryJu/saml-test-sp](https://github.com/BeryJu/saml-test-sp);
  `oidc-source` and `saml-source`, federated logins through an authentik
  OAuth source (dex) and SAML source (saml-test-idp), where every VU is the
  IdP's one test user, enrolled once in k6's `setup()`;
  `mixed-usage`, a weighted mix of logins, logins + API browsing, failed
  logins and flow page loads, drawn from a PRNG seeded per TestCase via
  `spec.seed` - defaulting to a hash of the TestCase name - so every run
  replays the same sequence; `breakpoint`, a test-until-failure login run
  that adds `spec.vus` VUs every 2 minutes and aborts once authentik's error
  rate or p95 latency crosses its thresholds),
  sourced from the exact same embedded `k6/scripts/*.ts` files the operator
  binary carries (create-only, so editing one in the UI is never
  clobbered by a later restart). A template's `spec.dependencies` lists
  extra services the operator provisions per TestCase and target, together
  with the authentik objects they need (via a blueprint import):
  `oidc-client` ([BeryJu/oidc-test-client](https://github.com/BeryJu/oidc-test-client)
  + OAuth2 provider), `saml-client`
  ([BeryJu/saml-test-sp](https://github.com/BeryJu/saml-test-sp) + SAML
  provider), `oidc-idp` ([dex](https://dexidp.io) + OAuth source) and
  `saml-idp` ([BeryJu/saml-test-idp](https://github.com/BeryJu/saml-test-idp)
  + SAML source). Their addresses, source slugs and IdP test credentials
  reach k6 as env vars (`render.DependencyEndpoints`).
- **Web UI**: `internal/api` is a REST/JSON backend (chi router) that CRUDs
  every CRD as unstructured objects (the CRD schema stays the single source
  of truth), proxies PromQL for the compare view (resolved per-testid
  against whichever cluster's own Prometheus actually has that data - see
  `prometheusURLsForTestID`), and reverse-proxies a workload cluster's own
  Grafana (`/grafana-proxy/<cluster>/...`). The same `cmd/apiserver`
  process serves the UI itself, `web/ui` - a Vite + React SPA (no SSR) with
  guided forms for every CRD, native metrics charts and a Compare view - so
  the whole UI is one Deployment (`make deploy-apiserver`,
  `hack/dev-apiserver.yaml`; `make ui-dev` for a live-reloading dev server).
  - **`kubectl` shell**: the UI's centerpiece - real shells on a PTY
    (`internal/api/shell.go`) bridged to xterm.js over a WebSocket, with
    `KUBECONFIG` pointed at a per-session copy of the merged kubeconfig
    `ShellKubeconfigReconciler` maintains, so every context (the
    management cluster plus each Ready `WorkloadCluster`) is one tab away.
    It replaces generic resource browsing: pages link into it with the
    relevant command typed at the prompt (never run until you press Enter).
    The WebSocket authenticates with a single-use, 10-second ticket minted by
    a normal authenticated request, and closing a tab kills that shell's
    whole process session.
  - **Auth**: OIDC (authorization code + PKCE, the SPA as a public client;
    `cmd/apiserver` verifies the JWT against the issuer's JWKS, optionally
    requiring a group - `BENCH_OIDC_ISSUER`/`BENCH_OIDC_CLIENT_ID`/
    `BENCH_OIDC_REQUIRED_GROUP`) and/or a static bearer token
    (`BENCH_API_TOKEN`/`--token-secret`). Register `<origin>/auth/callback`
    and `<origin>/auth/silent` as redirect URIs on the provider.

The original plan's central, mTLS-secured metrics receiver was superseded by
the per-workload-cluster observability stack described above once it became
clear a management-cluster pod can reach a workload cluster's node IPs
directly (the reverse can't), making a shared central pipeline unnecessary.
The Grafana dashboards live in
[`internal/render/manifests/grafana-dashboards/`](internal/render/manifests/grafana-dashboards/README.md).

### MCP server

`cmd/apiserver` also serves a read-only [MCP](https://modelcontextprotocol.io)
server at `/api/v1/mcp` (streamable HTTP, stateless) so an LLM can analyze
benchmark results: deployments, test cases and runs (plus their events), the
compare table, k6 metric and CPU/memory time series, trace and CPU profile
breakdowns, firing alerts, and raw PromQL. Every tool goes through the same
GET handlers (and input validation) the UI uses; none can create, change,
delete or run anything.

By default it requires the same bearer token (static or OIDC) as the rest of
the API, e.g. for Claude Code:

```sh
claude mcp add --transport http akbench https://<host>/api/v1/mcp \
  --header "Authorization: Bearer $AKBENCH_TOKEN"
```

With `--oidc-issuer` set, MCP clients can also sign in with OAuth through
that provider, as the web UI's own public client. There's no Dynamic Client
Registration, so pass the client ID yourself:

```sh
claude mcp add --transport http akbench https://<host>/api/v1/mcp \
  --client-id <--oidc-client-id> --callback-port 33418
```

An unauthenticated MCP request gets a 401 whose challenge names RFC 9728
metadata at `/.well-known/oauth-protected-resource/api/v1/mcp`, which lists
`--oidc-issuer` as the authorization server. The client runs authorization
code + PKCE there and presents the access token, which akbench checks like
any other web UI token (audience `--oidc-client-id`,
`--oidc-required-group`). For this to work, the provider must also allow
the MCP client's loopback redirect URI, e.g. `http://localhost:33418/callback`.
In authentik, add it as a strict redirect URI, or as the regex
`^http://(localhost|127\.0\.0\.1):\d+/callback$` to allow any port. The
metadata's `resource` is the URL the request came in on (honoring
`X-Forwarded-Proto`/`-Host`) unless `--mcp-resource-url` sets it.

With `--mcp-idp-issuer` (and `--mcp-resource-url`) set, it uses MCP
Enterprise-Managed Authorization (SEP-990, ID-JAG) instead and accepts
nothing else. akbench is its own authorization server, with the origin of
`--mcp-resource-url` as its issuer (e.g. `https://bench.example.com`, no
trailing slash). The MCP client signs
the user in at the enterprise IdP and exchanges the ID token there for an
ID-JAG addressed to that issuer (RFC 8693). It then trades the ID-JAG at
akbench's `/oauth/token` for a one-hour access token (RFC 7523 JWT bearer
grant, `client_secret_post` or `client_secret_basic`).

akbench serves RFC 9728 metadata at
`/.well-known/oauth-protected-resource/api/v1/mcp` and RFC 8414 metadata at
`/.well-known/oauth-authorization-server`. It accepts an ID-JAG only if all
of these hold:

- its `typ` is `oauth-id-jag+jwt`
- it's signed by `--mcp-idp-issuer` (keys from its OIDC discovery)
- its `aud` is akbench's issuer and its `client_id` is `--mcp-client-id`,
  the client that authenticated with `BENCH_MCP_CLIENT_SECRET`
- its `resource` is the MCP URL
- it's unexpired and its `jti` hasn't been used before

The access token never carries more scopes than the ID-JAG's `scope` claim.
To use MCP it must carry `--mcp-scopes`, if set. Calls are attributed to
`<sub> via <act.sub>`, the agent named by the ID-JAG's `act` claim.

Access tokens are signed with a key generated at startup. A restart logs MCP
clients out, and the `jti` replay cache lives in memory, so both assume a
single apiserver replica. An access token opens nothing else in the API.

[`hack/mcp-ema-client`](hack/mcp-ema-client/main.go) runs that whole flow
from the client side. It prints every token, checks the ID-JAG against the
draft's required header and claims, then lists (and optionally calls) the
tools. It's meant for testing an IdP's ID-JAG implementation, such as
authentik's:

```sh
go run ./hack/mcp-ema-client -insecure -mcp-url https://localhost:8443/api/v1/mcp \
  -idp-issuer https://idp.example.com/application/o/enterprise/ -idp-client-id <id> \
  -mcp-client-id <id> -mcp-client-secret <secret> [-id-token <jwt>] [-call list_test_cases]
```

#### Example: authentik as the IdP

[`hack/mcp-ema-client/authentik-blueprint.yaml`](hack/mcp-ema-client/authentik-blueprint.yaml)
sets up authentik's `akbench-ema-idp` provider as the enterprise IdP. What
it may mint ID-JAGs for lives on an authentik system agent, which the
ID-JAG's `act` claim names.

1. Import the blueprint, and copy the `akbench-ema-idp` client secret.
2. In authentik's admin interface, under Directory > Agents (an enterprise
   feature), create an agent with no parent user. Edit it to add the
   `akbench EMA IdP` application and set these ID-JAG fields:
   - audience: akbench's issuer, the origin of `--mcp-resource-url` without
     a trailing slash (`https://localhost:8443`)
   - client ID: `akbench-mcp`
   - resources: `https://localhost:8443/api/v1/mcp`
3. Start akbench as the MCP authorization server, trusting that IdP. Pick
   any secret for the MCP client:

   ```sh
   BENCH_MCP_CLIENT_SECRET=<mcp secret> go run ./cmd/apiserver \
     --mcp-resource-url https://localhost:8443/api/v1/mcp \
     --mcp-idp-issuer https://authentik.company/application/o/akbench-ema-idp/ \
     --mcp-client-id akbench-mcp
   ```

4. Run the client. It logs you in at the IdP in the browser:

   ```sh
   IDP_CLIENT_SECRET=... MCP_CLIENT_SECRET=<mcp secret> \
   go run ./hack/mcp-ema-client -insecure -mcp-url https://localhost:8443/api/v1/mcp \
     -idp-issuer https://authentik.company/application/o/akbench-ema-idp/ \
     -idp-client-id akbench-ema-idp -mcp-client-id akbench-mcp -call list_test_cases
   ```

### Running the tests

```sh
make test          # envtest (real apiserver, no real controllers e.g. Flux/CNPG) + contract tests
go test ./test/contract/...   # just the k6 tag/env parity guard
```

## Appliance

The easiest way to run akbench is the single-container appliance: k3s as the
Cluster API management cluster, with Flux, Cluster API and akbench
preinstalled. Workload clusters run on your own infrastructure (Incus today),
whose provider and connection settings are set up on the web UI's
Infrastructure page. See
[appliance/README.md](appliance/README.md), and
[appliance/MIGRATION.md](appliance/MIGRATION.md) to move an existing
management cluster onto it.

## Development

### Prerequisites

- Go (the version in `go.mod`), Node.js 24+ and Docker
- `kubectl` and a management cluster with Cluster API, the Incus
  infrastructure provider and Flux installed - or just use the appliance above
- [uv](https://docs.astral.sh/uv/) for `make fixtures` (optional - the operator seeds test users itself)

### Running locally

```sh
make dev           # install CRDs, run the operator and cmd/apiserver against the current kubeconfig context
make ui-dev        # Vite dev server on :5173, proxying the API (run alongside make dev)
make dev-metrics   # optional: a minimal Prometheus for local metrics
make fixtures      # optional: regenerate the synthetic users + k6 credentials (the operator seeds 50 by default)
```

`make deploy-apiserver` builds `cmd/apiserver` (API, web UI and `kubectl`
shell) and deploys it in-cluster instead.

### Deploying the operator

CI publishes the operator image and its Helm chart (generated from
`config/default` by `make helm-chart`) to GHCR on every push to `main` and
every `v*` tag:

```sh
helm install akbench oci://ghcr.io/<org>/<repo>-charts/authentik-bench-operator \
  --namespace authentik-bench-system --create-namespace
```

For a one-off build, `make docker-build IMG=<registry>/akbench:tag` and
`make deploy IMG=<registry>/akbench:tag` apply the same manifests via
Kustomize (`make undeploy` removes them). `config/samples/` has an example of
every CRD.

## Contributing

Open a PR. `make manifests generate lint test` should pass; for anything
touching `internal/render`/`internal/controller`, also smoke-test live
against a real cluster where practical - this project has repeatedly found
real bugs (chart schema mismatches, webhook-defaulting races, RBAC gaps)
that `envtest` alone can't catch, since it never runs the actual
Helm/CAPI/CSI controllers being integrated with.

Run `make help` for every `make` target.

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

