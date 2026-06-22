package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/actionlab-ai/aisphere-sandbox/internal/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgres(dsn string) (*PostgresStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("postgres dsn is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if pcfg.MaxConns == 0 {
		pcfg.MaxConns = 30
	}
	pcfg.MaxConnLifetime = 30 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	s := &PostgresStore{pool: pool}
	if err := s.AutoMigrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}
func (s *PostgresStore) Close() error { s.pool.Close(); return nil }
func (s *PostgresStore) AutoMigrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sandbox_lease (
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
        )`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_lease_session ON sandbox_lease(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_lease_org_project ON sandbox_lease(org_id, project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_lease_agent ON sandbox_lease(agent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_lease_phase ON sandbox_lease(phase)`,
		`CREATE INDEX IF NOT EXISTS idx_sandbox_lease_status_gin ON sandbox_lease USING GIN(status)`,
	}
	for _, stmt := range stmts {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
func (s *PostgresStore) SaveSandboxStatus(ctx context.Context, st *model.SandboxStatus) error {
	if st == nil || strings.TrimSpace(st.SandboxID) == "" {
		return nil
	}
	b, _ := json.Marshal(st)
	var worker, tools string
	for _, ep := range st.Endpoints {
		if ep.Name == "worker" {
			worker = ep.URL
		}
		if ep.Name == "tools" {
			tools = ep.URL
		}
	}
	var exp any
	if st.Lease != nil && st.Lease.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, st.Lease.ExpiresAt); err == nil {
			exp = t
		}
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO sandbox_lease(sandbox_id,org_id,project_id,owner_subject,session_id,run_id,agent_id,snapshot_id,profile,phase,worker_endpoint,tools_endpoint,expires_at,status,created_at,updated_at)
      VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,now(),now())
      ON CONFLICT(sandbox_id) DO UPDATE SET org_id=EXCLUDED.org_id,project_id=EXCLUDED.project_id,owner_subject=EXCLUDED.owner_subject,session_id=EXCLUDED.session_id,run_id=EXCLUDED.run_id,agent_id=EXCLUDED.agent_id,snapshot_id=EXCLUDED.snapshot_id,profile=EXCLUDED.profile,phase=EXCLUDED.phase,worker_endpoint=EXCLUDED.worker_endpoint,tools_endpoint=EXCLUDED.tools_endpoint,expires_at=EXCLUDED.expires_at,status=EXCLUDED.status,updated_at=now()`, st.SandboxID, st.OrgID, st.ProjectID, st.OwnerSubject, st.SessionID, st.RunID, st.AgentID, st.SnapshotID, st.Profile, st.Phase, worker, tools, exp, string(b))
	return err
}
func (s *PostgresStore) DeleteSandbox(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sandbox_lease WHERE sandbox_id=$1`, id)
	return err
}

func (s *PostgresStore) GetSandbox(ctx context.Context, id string) (*model.SandboxStatus, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT status FROM sandbox_lease WHERE sandbox_id=$1`, strings.TrimSpace(id)).Scan(&raw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return decodeSandboxStatus(raw)
}

func (s *PostgresStore) GetBySession(ctx context.Context, sessionID string) (*model.SandboxStatus, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT status FROM sandbox_lease WHERE session_id=$1 ORDER BY updated_at DESC LIMIT 1`, strings.TrimSpace(sessionID)).Scan(&raw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return decodeSandboxStatus(raw)
}

func (s *PostgresStore) ListSandboxes(ctx context.Context, q ListQuery) ([]*model.SandboxStatus, error) {
	clauses := []string{"1=1"}
	args := []any{}
	add := func(col, v string) {
		if strings.TrimSpace(v) == "" {
			return
		}
		args = append(args, strings.TrimSpace(v))
		clauses = append(clauses, fmt.Sprintf("%s=$%d", col, len(args)))
	}
	add("org_id", q.OrgID)
	add("project_id", q.ProjectID)
	add("owner_subject", q.OwnerSubject)
	add("session_id", q.SessionID)
	add("agent_id", q.AgentID)
	rows, err := s.pool.Query(ctx, `SELECT status FROM sandbox_lease WHERE `+strings.Join(clauses, " AND ")+` ORDER BY updated_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.SandboxStatus{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		st, err := decodeSandboxStatus(raw)
		if err != nil {
			return nil, err
		}
		if st != nil {
			out = append(out, st)
		}
	}
	return out, rows.Err()
}

type ListQuery struct {
	OwnerSubject string
	OrgID        string
	ProjectID    string
	SessionID    string
	AgentID      string
}

func decodeSandboxStatus(raw []byte) (*model.SandboxStatus, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var st model.SandboxStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	return &st, nil
}
