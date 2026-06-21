#!/usr/bin/env bash
set -euo pipefail

REGISTRY="${REGISTRY:-ghcr.io/actionlab-ai}"
VERSION="${VERSION:-$(git rev-parse --short HEAD 2>/dev/null || echo dev)}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"
PUSH="${PUSH:-false}"
LOAD="${LOAD:-false}"
MANAGER_IMAGE="${MANAGER_IMAGE:-${REGISTRY}/aisphere-sandbox-manager:${VERSION}}"
SANDBOX_IMAGE="${SANDBOX_IMAGE:-${REGISTRY}/agentkit-sandbox:${VERSION}}"

args=(--platform "$PLATFORMS")
if [[ "$PUSH" == "true" ]]; then
  args+=(--push)
elif [[ "$LOAD" == "true" ]]; then
  args+=(--load)
else
  args+=(--load)
fi

echo "building manager image: $MANAGER_IMAGE platforms=$PLATFORMS"
docker buildx build "${args[@]}" -t "$MANAGER_IMAGE" .

echo "building sandbox image: $SANDBOX_IMAGE platforms=$PLATFORMS"
docker buildx build "${args[@]}" -t "$SANDBOX_IMAGE" deployments/image
