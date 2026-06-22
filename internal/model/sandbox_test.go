package model

import (
	"encoding/json"
	"testing"
)

func TestSandboxEnsureRequestDecodesReuse(t *testing.T) {
	var req SandboxEnsureRequest
	if err := json.Unmarshal([]byte(`{"sessionId":"sess-1","reuse":true}`), &req); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !req.Reuse {
		t.Fatal("Reuse = false, want true")
	}
}
