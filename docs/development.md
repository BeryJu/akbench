# Development

## Prerequisites

- Go (the version in `go.mod`), Node.js 24+ and Docker
- `kubectl` and a management cluster with Cluster API, the Incus
  infrastructure provider and Flux installed - or just use the [appliance](appliance.md)
- [uv](https://docs.astral.sh/uv/) for `make fixtures` (optional - the operator seeds test users itself)

## Running locally

```sh
make dev           # install CRDs, run the operator and cmd/apiserver against the current kubeconfig context
make ui-dev        # Vite dev server on :5173, proxying the API (run alongside make dev)
make dev-metrics   # optional: a minimal Prometheus for local metrics
make fixtures      # optional: regenerate the synthetic users + k6 credentials (the operator seeds 50 by default)
```

`make deploy-apiserver` builds `cmd/apiserver` (API, web UI and `kubectl`
shell) and deploys it in-cluster instead.

## Deploying the operator

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


## Running the tests

```sh
make test          # envtest (real apiserver, no real controllers e.g. Flux/CNPG) + contract tests
go test ./test/contract/...   # just the k6 tag/env parity guard
```

