# akbench appliance

A single container that is a complete akbench management cluster:

- **k3s**, the management cluster itself (PID 1 of the container)
- **Flux**, the root GitOps controllers: helm-controller and source-controller install
  every chart akbench renders, here and on workload clusters
- **Cluster API**, with the kubeadm bootstrap/control-plane providers and the
  **Incus provider** (CAPN), plus cert-manager, installed with `clusterctl`
- **akbench**: the operator (via a Flux `HelmRelease` of its published chart)
  and the apiserver (web UI + kubectl shell)

Incus is **not** part of the appliance. Workload clusters run on your own
Incus server, which the appliance reaches over the network.

## Running it

```sh
docker run -d --name akbench --privileged --restart unless-stopped \
  -v akbench:/var/lib/rancher/k3s \
  -p 8080:8080 -p 6443:6443 \
  -e INCUS_SERVER=https://incus.example.com:8443 \
  -e INCUS_SERVER_CERT=/run/incus/server.crt -v /path/to/incus-server.crt:/run/incus/server.crt:ro \
  ghcr.io/beryju/akbench-appliance:latest
```

Follow the first boot (2-5 minutes, mostly image pulls):

```sh
docker logs -f akbench 2>&1 | grep '\[akbench\]'
```

On first boot the appliance generates an **Incus client certificate**. Trust it
on the Incus server:

```sh
docker exec akbench akbench-appliance incus-cert > akbench.crt
incus config trust add-certificate akbench.crt --name akbench
```

Then open `http://<host>:8080/` and log in with the API token:

```sh
docker exec akbench akbench-appliance token
```

`kubectl` is available inside the container (`docker exec -it akbench kubectl get workloadclusters -A`).
To use it from outside, copy the admin kubeconfig. Its server is
`https://127.0.0.1:6443`, which works through `-p 6443:6443`:

```sh
docker exec akbench cat /etc/rancher/k3s/k3s.yaml > akbench.kubeconfig
```

### Requirements

- Docker (or Podman) with **cgroup v2** and `--privileged`, the same as any
  k3s-in-Docker setup (k3d, Rancher's single-node install).
- About 2 vCPU, 4 GiB RAM and 20 GiB disk for the management plane itself.
  Workload clusters run on Incus, not here.
- **Outbound** network access from the Docker host to:
  - the Incus API (`INCUS_SERVER`, usually `:8443`);
  - each workload cluster's API load balancer (`:6443`) and node IPs
    (Grafana/Prometheus NodePorts) on the Incus instance network. If the
    Incus network is NATed (the default `incusbr0`), route it to the Docker host or
    bridge it onto your LAN.
  - container registries (ghcr.io, docker.io, registry.k8s.io, quay.io) and
    the chart repositories akbench installs from.

  Nothing ever connects *into* the appliance except your browser, so the
  Docker NAT in front of it is fine.
- No address overlap between k3s's own pod/service networks (`10.42.0.0/16` and
  `10.43.0.0/16`) and your Incus instance network. If they do overlap, pass
  `--cluster-cidr`/`--service-cidr` as extra arguments (see below). They take
  effect on the first boot only.

### Configuration

| Variable | Default | |
|---|---|---|
| `INCUS_SERVER` | | Incus API URL. If unset, the appliance does not manage the Incus Secret, for example when it was migrated over. |
| `INCUS_PROJECT` | `default` | |
| `INCUS_SERVER_CERT` | | Incus server certificate (PEM, or a path to one). |
| `INCUS_INSECURE_SKIP_VERIFY` | `false` | Instead of `INCUS_SERVER_CERT`. |
| `INCUS_CLIENT_CERT` / `INCUS_CLIENT_KEY` | generated | Bring your own client certificate instead of the generated one. |
| `INCUS_SECRET_NAME` | `lxc-secret` | The Secret a `WorkloadCluster`'s `spec.incus.lxcSecretRef` names. |
| `AKBENCH_NAMESPACE` | `default` | Namespace for your akbench objects. |
| `AKBENCH_UI_PORT` | `8080` | Port the web UI listens on (publish it with `-p`). |
| `AKBENCH_API_TOKEN` | generated | Pin the web UI's bearer token. |
| `AKBENCH_OIDC_ISSUER`, `AKBENCH_OIDC_CLIENT_ID`, `AKBENCH_OIDC_REQUIRED_GROUP`, `AKBENCH_OIDC_SCOPES` | | OIDC login for the web UI (see the main README). |
| `AKBENCH_VERSION` | the image's own | Operator chart/image and apiserver image version to install. |

Extra arguments to `docker run … <image> <args>` are passed to `k3s server`.

The appliance writes its generated state (API token, Incus client
certificate, k3s node identity) under `/var/lib/rancher/k3s/akbench` on the
volume.

## Upgrading

Every boot re-runs the whole bootstrap, and every step is idempotent. To upgrade,
replace the container and keep the volume:

```sh
docker pull ghcr.io/beryju/akbench-appliance:<new>
docker rm -f akbench
docker run -d --name akbench … ghcr.io/beryju/akbench-appliance:<new>   # same flags as before
```

This rolls out:
- the new operator chart and apiserver image;
- `clusterctl upgrade apply` for any Cluster API or Incus provider the new image ships a newer
  version of (providers are never downgraded);
- the new Flux manifests;
- the new k3s.

k3s only supports upgrading its datastore **one minor version at a time**,
so don't skip appliance releases that bump k3s by more than one minor.

## Backups

The appliance is a single node. If you lose the volume, your workload clusters
keep running on Incus, but nothing manages them any more. Back the volume up,
for example with the container stopped:

```sh
docker run --rm -v akbench:/data -v "$PWD:/backup" alpine tar czf /backup/akbench.tgz -C /data .
```

Or move everything to another management cluster with `clusterctl move`, the
same mechanism the migration uses.

## Migrating from an existing management cluster

See [MIGRATION.md](MIGRATION.md).

## Development

```sh
make appliance-dev                    # build + run locally (apiserver from this checkout)
make docker-build-appliance           # just build the image
make appliance-assets                 # re-pin Flux/clusterctl/CAPI/CAPN/cert-manager/k3s
```

Every upstream file the image ships is pinned with its checksum in
`assets.lock`. To bump a version, edit it in `hack/update-appliance-assets.sh`
and run `make appliance-assets`.
