#!/usr/bin/env bash
set -euo pipefail
REGISTRY="${REGISTRY:-registry.local/aisphere}"
MANAGER_IMAGE="${MANAGER_IMAGE:-${REGISTRY}/aisphere-sandbox:latest}"
SANDBOX_IMAGE="${SANDBOX_IMAGE:-${REGISTRY}/agentkit-sandbox:latest}"

docker build -t "$MANAGER_IMAGE" .
docker build -t "$SANDBOX_IMAGE" deployments/image

echo "built: $MANAGER_IMAGE"
echo "built: $SANDBOX_IMAGE"
