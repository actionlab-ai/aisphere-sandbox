package store

import (
	"context"
	"os"
	"testing"

	"github.com/actionlab-ai/aisphere-sandbox/internal/model"
)

func TestPostgresLeaseRecoverBySession(t *testing.T) {
	dsn := os.Getenv("SANDBOX_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("SANDBOX_POSTGRES_DSN is not set")
	}
	st, err := NewPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	status := &model.SandboxStatus{SandboxID: "sbx-it-001", SessionID: "sess-it-001", OrgID: "org-it", ProjectID: "project-it", OwnerSubject: "user-it", AgentID: "agent-it", Profile: "default-python-offline", Phase: "Running", Endpoints: []model.SandboxEndpoint{{Name: "worker", URL: "http://worker:8088"}}}
	if err := st.SaveSandboxStatus(ctx, status); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetBySession(ctx, status.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.SandboxID != status.SandboxID {
		t.Fatalf("unexpected status: %#v", got)
	}
}
