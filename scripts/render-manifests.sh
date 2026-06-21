#!/usr/bin/env bash
set -euo pipefail
OUT_DIR="${OUT_DIR:-dist/rendered}"
NAMESPACE="${NAMESPACE:-aisphere-sandbox-system}"
SANDBOX_NAMESPACE="${SANDBOX_NAMESPACE:-aisphere-sandbox}"
REGISTRY="${REGISTRY:-registry.local/aisphere}"
VERSION="${VERSION:-latest}"
MANAGER_IMAGE="${MANAGER_IMAGE:-${REGISTRY}/aisphere-sandbox-manager:${VERSION}}"
SANDBOX_IMAGE="${SANDBOX_IMAGE:-${REGISTRY}/agentkit-sandbox:${VERSION}}"
STORAGE_CLASS="${STORAGE_CLASS:-nfs-client}"
mkdir -p "$OUT_DIR"
cp deployments/kubernetes/manager-rbac.yaml "$OUT_DIR/manager-rbac.yaml"
cp deployments/kubernetes/sandbox-rbac.yaml "$OUT_DIR/sandbox-rbac.yaml"
python3 - <<PY
from pathlib import Path
s=Path('deployments/kubernetes/deployment.yaml').read_text()
s=s.replace('namespace: aisphere-sandbox-system','namespace: ${NAMESPACE}')
s=s.replace('registry.local/aisphere/aisphere-sandbox-manager:latest','${MANAGER_IMAGE}')
s=s.replace('registry.local/aisphere/agentkit-sandbox:latest','${SANDBOX_IMAGE}')
s=s.replace('"storageClass":"nfs-client"','"storageClass":"${STORAGE_CLASS}"')
Path('${OUT_DIR}/deployment.yaml').write_text(s)
PY
cat > "$OUT_DIR/namespace.yaml" <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: ${NAMESPACE}
---
apiVersion: v1
kind: Namespace
metadata:
  name: ${SANDBOX_NAMESPACE}
YAML
echo "rendered to $OUT_DIR"
