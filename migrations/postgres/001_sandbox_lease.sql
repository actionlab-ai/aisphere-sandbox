CREATE TABLE IF NOT EXISTS sandbox_lease (
  sandbox_id TEXT PRIMARY KEY,
  org_id TEXT,
  project_id TEXT,
  owner_subject TEXT,
  session_id TEXT,
  run_id TEXT,
  agent_id TEXT,
  snapshot_id TEXT,
  profile TEXT,
  phase TEXT,
  worker_endpoint TEXT,
  tools_endpoint TEXT,
  lease_token_hash TEXT,
  expires_at TIMESTAMPTZ,
  status JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_sandbox_lease_session ON sandbox_lease(session_id);
CREATE INDEX IF NOT EXISTS idx_sandbox_lease_org_project ON sandbox_lease(org_id, project_id);
CREATE INDEX IF NOT EXISTS idx_sandbox_lease_agent ON sandbox_lease(agent_id);
CREATE INDEX IF NOT EXISTS idx_sandbox_lease_phase ON sandbox_lease(phase);
CREATE INDEX IF NOT EXISTS idx_sandbox_lease_status_gin ON sandbox_lease USING GIN(status);
