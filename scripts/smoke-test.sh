#!/usr/bin/env bash
set -euo pipefail
BASE="${BASE:-http://127.0.0.1:18082}"
SESSION_ID="${SESSION_ID:-sess-smoke-001}"

echo "[health]"
curl -fsS "$BASE/healthz" | jq .

echo "[ensure sandbox]"
curl -fsS "$BASE/v1/sandboxes/ensure" \
  -H 'Content-Type: application/json' \
  -d '{"runtimeId":"agentkit-smoke","sessionId":"'"$SESSION_ID"'","orgId":"org-smoke","projectId":"project-smoke","agentId":"demo","network":{"mode":"offline"},"limits":{"cpu":"500m","memory":"1Gi"}}' | tee /tmp/sandbox.json | jq .

SANDBOX_ID=$(jq -r .sandboxId /tmp/sandbox.json)
echo "sandboxId=$SANDBOX_ID"

echo "[get]"
curl -fsS "$BASE/v1/sandboxes/$SANDBOX_ID" | jq .

echo "[tools, may fail until pod is ready]"
curl -fsS "$BASE/v1/sandboxes/$SANDBOX_ID/tools" | jq . || true
