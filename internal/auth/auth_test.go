package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/actionlab-ai/aisphere-sandbox/internal/config"
)

func TestAuthenticateUsesSharedSessionContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer service-token" {
			t.Fatalf("Authorization = %q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["sessionId"] != "session-1" {
			t.Fatalf("body = %#v", body)
		}
		_, _ = w.Write([]byte(`{"active":true,"principal":{"subjectId":"user-1","orgId":"org-1","projectIds":["project-1"],"roles":["editor"],"groups":["team-a"]}}`))
	}))
	defer server.Close()

	client := NewClient(config.AuthConfig{
		Enabled:      true,
		Mode:         "aisphere",
		AuthEndpoint: server.URL,
		ServiceToken: "service-token",
		App:          "aihub",
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer session-1")
	principal, err := client.Authenticate(req)
	if err != nil || principal.SubjectID != "user-1" || principal.ProjectID != "project-1" || len(principal.Roles) != 1 {
		t.Fatalf("Authenticate() = %#v, %v", principal, err)
	}
}

func TestCheckRejectsExplicitDeny(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"allow":false,"reason":"missing grant"}`))
	}))
	defer server.Close()

	client := NewClient(config.AuthConfig{
		Enabled:      true,
		Mode:         "aisphere",
		AuthEndpoint: server.URL,
		FailClosed:   true,
		App:          "aihub",
	})
	if err := client.Check(context.Background(), &Principal{SubjectID: "user-1"}, "aihub:sandbox:1", "use"); err == nil {
		t.Fatal("Check() error = nil, want explicit deny")
	}
}
