package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadYAMLAppliesEnvironmentOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "server:\n" +
		"  addr: :19000\n" +
		"auth:\n" +
		"  enabled: true\n" +
		"  mode: aisphere\n" +
		"database:\n" +
		"  driver: postgres\n" +
		"  dsn: postgres://sandbox\n" +
		"  autoMigrate: false\n" +
		"sandbox:\n" +
		"  namespace: from-yaml\n" +
		"  defaultEgressCidrs:\n" +
		"    - 10.0.0.0/8\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SANDBOX_MANAGER_ADDR", ":19001")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Addr != ":19001" {
		t.Fatalf("Server.Addr = %q, want environment override", cfg.Server.Addr)
	}
	if cfg.Sandbox.Namespace != "from-yaml" {
		t.Fatalf("Sandbox.Namespace = %q, want YAML value", cfg.Sandbox.Namespace)
	}
	if got, want := cfg.Sandbox.DefaultEgressCIDRs, []string{"10.0.0.0/8"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("Sandbox.DefaultEgressCIDRs = %#v, want %#v", got, want)
	}
}

func TestLoadYAMLRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("server:\n  addrr: :18082\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "field addrr not found") {
		t.Fatalf("Load() error = %v, want unknown field failure", err)
	}
}
