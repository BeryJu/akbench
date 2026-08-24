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

## Status: M1 (single cluster, kubectl only)

Implemented so far:

- **CRDs** (`bench.goauthentik.io/v1alpha1`): `AuthentikTopology`, `TestCase`,
  `TestRun`. Full spec shape from the design plan (placement modes, all three
  Postgres modes, multi-cluster `clusterRef`s) is defined now so the schema
  doesn't churn across milestones, but the M1 controllers only implement:
  Postgres mode `Embedded`, placement mode `Default`, and topologies/test
  cases on the operator's own cluster (`clusterRef`/`runnerClusterRef` report
  a `NotImplemented` condition until M4's multicluster tracker lands).
- **`AuthentikTopologyReconciler`**: renders a Flux `HelmRelease` +
  `HelmRepository` installing the official authentik chart
  (`charts.goauthentik.io`) with the topology's replica/resource/placement
  values.
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

Not yet implemented (see the plan's milestones): CNPG/External postgres,
`Dedicated` placement, the web UI, multi-cluster (`AdoptedCluster` CAPI
provider), the mTLS central metrics receiver, and Flux-managed GitOps for the
metrics stack itself.

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

