#!/bin/sh
# Entrypoint of the single-container appliance (Dockerfile.appliance). Starts
# the bootstrapper (cmd/appliance) in the background, then execs k3s as PID 1
# - k3s's own container support (cgroup v2 evacuation, etc.) relies on that.
set -eu

state=/var/lib/rancher/k3s/akbench
mkdir -p "$state/node" /etc/rancher

# k3s keeps the node's password under /etc/rancher/node and checks it against
# the one it stored in its datastore on first boot. Only /var/lib/rancher/k3s
# is a volume, so without this a recreated container (e.g. on upgrade) would
# get a fresh password and k3s would reject its own node.
if [ ! -L /etc/rancher/node ]; then
    rm -rf /etc/rancher/node
    ln -s "$state/node" /etc/rancher/node
fi

/usr/local/bin/akbench-appliance bootstrap &

# --node-name: pinned, because the container's hostname changes every time
# it is recreated, and a new node name would strand every pod on the old,
# now-NotReady node.
exec /bin/k3s server \
    --node-name="${AKBENCH_NODE_NAME:-akbench}" \
    --disable=traefik \
    --write-kubeconfig-mode=0600 \
    "$@"
