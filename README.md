# authentik-bench

A Kubernetes-native replacement for the Ansible/docker-compose authentik
benchmark harness at
[BeryJu/authentik-benchmark](https://github.com/BeryJu/authentik-benchmark):
an operator plus (eventually) a web UI for defining k6 load-test cases
against different authentik deployment topologies - replica counts, node
placement (dedicated vs. shared nodes), and Postgres backend (embedded
subchart, CloudNativePG HA, or external) - with multi-cluster support via
Cluster API and a central, mTLS-secured metrics pipeline.

See the design plan this repo implements for the full architecture and
phased delivery milestones (M1-M4).

## Status: M4 (multi-cluster provisioning + clusterRef wiring)

Implemented so far:

- **CRDs** (`bench.goauthentik.io/v1alpha1`): `AuthentikDeployment`, `TestCase`,
  `TestRun`, `WorkloadCluster`.
- **`WorkloadClusterReconciler`**: renders a full Cluster API cluster stack
  (`Cluster`/`LXCCluster`/`KubeadmControlPlane`/`MachineDeployment` plus their
  `LXCMachineTemplate`s and a worker `KubeadmConfigTemplate`) against the
  Incus infrastructure provider already installed on the management cluster,
  and mirrors the resulting `Cluster`'s readiness (and its
  `<name>-kubeconfig` Secret) back onto its own status. Provisioning is
  create-once for now - no day-2 resize/upgrade of an existing
  `WorkloadCluster` yet.
- **`clusterRef`/`runnerClusterRef`**: `AuthentikDeploymentReconciler`,
  `TestCaseReconciler`, and `TestRunReconciler` all resolve these to a live
  client via `internal/multicluster.Tracker` (wrapping Cluster API's
  `ClusterCache`), falling back to the management cluster when unset.
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
  tracks it to completion, and (optionally, if `BENCH_GRAFANA_URL` is set)
  posts start/completion Grafana annotations - reproducing
  `roles/runner/tasks/annotate.yml`'s behavior with a service-account token
  instead of the anonymous-admin approach.
- **k6 tag/env contract**: `k6/scripts/login.ts` is ported byte-for-byte from
  the reference repo; `internal/render` reproduces its exact
  `testid`/`target`/`profile` tag scheme and env var contract
  (`TEST_ID`/`TARGET`/`BENCH_HOST`/... ), guarded by `test/contract`.
- **Fixtures**: `fixtures/gen-blueprint.py` ported unchanged.
- **Web UI**: `internal/api` is a REST/JSON backend (chi router, bearer-token
  auth) that CRUDs all four CRDs as unstructured objects (the CRD schema
  stays the single source of truth), proxies PromQL for the compare view
  (resolved per-testid against whichever cluster's own Prometheus actually
  has that data - see `prometheusURLForTestID`), and builds allowlisted
  Grafana embed URLs - deployed in-cluster (`make deploy-apiserver`,
  `hack/dev-apiserver.yaml`). The UI itself is [Headlamp](https://headlamp.dev)
  (`make headlamp`, `hack/dev-headlamp-values.yaml`) plus
  `web/headlamp-plugin`, a custom plugin (guided create/edit forms for all
  four CRDs, a Compare page, a TestRuns history page, and metrics charting)
  for what Headlamp has no native concept of, reached through Headlamp's own
  `ApiProxy` (a Kubernetes API Service-proxy call, not a browser-direct one).
  `make headlamp-plugin` copies a fresh build straight into the running pod
  for fast dev-loop iteration (wiped on a Headlamp pod restart);
  `make deploy-headlamp-plugin` bakes it into an image installed as a
  Headlamp initContainer instead, so it survives one. Everything else
  (listing/creating/editing every CRD as raw YAML, pod logs, events,
  multi-cluster switching across every CAPI-provisioned `WorkloadCluster`) is
  Headlamp's own generic Kubernetes UI - see `HeadlampKubeconfigReconciler`
  for how every `WorkloadCluster` becomes a switchable cluster automatically.

Not yet implemented (see the plan's milestones): a central,
mTLS-secured metrics receiver. Each workload cluster instead runs its own
Prometheus/Pyroscope/Tempo (`WorkloadClusterReconciler`'s `ensurePrometheus`/
`ensurePyroscope`/`ensureTempo`), reachable from the management cluster
directly (a management-cluster pod can reach a workload cluster's node IP;
the reverse can't) rather than through a shared central pipeline. Also not
yet wired up: Flux-managed GitOps for this stack (dashboard JSON already
lives in `gitops/dashboards/`, not yet wired to a Flux Kustomization).

### Running the tests

```sh
make test          # envtest (real apiserver, no real controllers e.g. Flux/CNPG) + contract tests
go test ./test/contract/...   # just the k6 tag/env parity guard
```

## Getting Started

### Prerequisites
- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/authentik-bench:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/authentik-bench:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/authentik-bench:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/authentik-bench/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v2-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

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

