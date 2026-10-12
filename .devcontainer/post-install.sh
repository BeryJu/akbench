#!/bin/bash
# Installs the tools the devcontainer features don't cover (kind, for
# `make test-e2e`) and pre-fetches Go modules, the Makefile's own tool
# binaries and web/ui's node_modules so the first `make test`/`make ui-dev`
# doesn't start from cold.
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: This script must be run as root"
  exit 1
fi

case "$(uname -m)" in
  x86_64) ARCH="amd64" ;;
  aarch64 | arm64) ARCH="arm64" ;;
  *)
    echo "WARNING: Unsupported architecture $(uname -m), defaulting to amd64"
    ARCH="amd64"
    ;;
esac

BASH_COMPLETIONS_DIR="/usr/share/bash-completion/completions"
if ! grep -q "source /usr/share/bash-completion/bash_completion" ~/.bashrc 2>/dev/null; then
  echo 'source /usr/share/bash-completion/bash_completion' >> ~/.bashrc
fi

if ! command -v kind &> /dev/null; then
  echo "Installing kind..."
  curl -Lo /usr/local/bin/kind "https://kind.sigs.k8s.io/dl/latest/kind-linux-${ARCH}"
  chmod +x /usr/local/bin/kind
fi

for tool in kind kubectl helm docker; do
  if command -v "$tool" &> /dev/null; then
    "$tool" completion bash > "${BASH_COMPLETIONS_DIR}/${tool}" 2>/dev/null ||
      echo "WARNING: Failed to generate ${tool} completion"
  fi
done

echo "Waiting for Docker to be ready..."
for i in {1..30}; do
  if docker info > /dev/null 2>&1; then
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "WARNING: Docker not ready after 30s"
  fi
  sleep 1
done
if ! docker network inspect kind > /dev/null 2>&1; then
  docker network create kind > /dev/null 2>&1 || echo "WARNING: Failed to create kind network"
fi

echo "Pre-fetching Go modules and build tools..."
go mod download
make controller-gen kustomize setup-envtest golangci-lint

echo "Installing web UI dependencies..."
(cd web/ui && npm ci)

kind version
kubectl version --client
helm version --short
docker --version
go version
node --version
uv --version
