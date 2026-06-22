package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `json:"server" yaml:"server"`
	Auth     AuthConfig     `json:"auth" yaml:"auth"`
	Database DatabaseConfig `json:"database" yaml:"database"`
	Sandbox  SandboxConfig  `json:"sandbox" yaml:"sandbox"`
}

type ServerConfig struct {
	Addr           string   `json:"addr" yaml:"addr"`
	TrustedProxies []string `json:"trustedProxies,omitempty" yaml:"trustedProxies,omitempty"`
}

type AuthConfig struct {
	Enabled               bool   `json:"enabled" yaml:"enabled"`
	Mode                  string `json:"mode" yaml:"mode"` // none / trusted / aisphere
	AuthEndpoint          string `json:"authEndpoint,omitempty" yaml:"authEndpoint,omitempty"`
	SessionIntrospectPath string `json:"sessionIntrospectPath,omitempty" yaml:"sessionIntrospectPath,omitempty"`
	IAMCheckPath          string `json:"iamCheckPath,omitempty" yaml:"iamCheckPath,omitempty"`
	ServiceToken          string `json:"serviceToken,omitempty" yaml:"serviceToken,omitempty"`
	FailClosed            bool   `json:"failClosed" yaml:"failClosed"`
	App                   string `json:"app" yaml:"app"`
	CookieName            string `json:"cookieName,omitempty" yaml:"cookieName,omitempty"`
}

type DatabaseConfig struct {
	Driver      string `json:"driver" yaml:"driver"`
	DSN         string `json:"dsn,omitempty" yaml:"dsn,omitempty"`
	AutoMigrate bool   `json:"autoMigrate" yaml:"autoMigrate"`
}

type SandboxConfig struct {
	// Driver selects the backing implementation.
	// direct-kubernetes keeps the legacy Pod/PVC/Service implementation as a fallback.
	// agent-sandbox uses kubernetes-sigs/agent-sandbox CRDs and is the recommended mode.
	Driver string `json:"driver" yaml:"driver"`

	// AgentSandbox config maps Aisphere sandbox sessions to kubernetes-sigs/agent-sandbox resources.
	AgentSandboxAPIVersion string `json:"agentSandboxApiVersion,omitempty" yaml:"agentSandboxApiVersion,omitempty"`
	UseClaim               bool   `json:"useClaim,omitempty" yaml:"useClaim,omitempty"`
	DefaultTemplate        string `json:"defaultTemplate,omitempty" yaml:"defaultTemplate,omitempty"`
	DefaultWarmPool        string `json:"defaultWarmPool,omitempty" yaml:"defaultWarmPool,omitempty"`
	DefaultProfile         string `json:"defaultProfile,omitempty" yaml:"defaultProfile,omitempty"`

	Namespace            string   `json:"namespace" yaml:"namespace"`
	CreateNamespace      bool     `json:"createNamespace" yaml:"createNamespace"`
	APIServer            string   `json:"apiServer,omitempty" yaml:"apiServer,omitempty"`
	Kubeconfig           string   `json:"kubeconfig,omitempty" yaml:"kubeconfig,omitempty"`
	Token                string   `json:"token,omitempty" yaml:"token,omitempty"`
	TokenFile            string   `json:"tokenFile,omitempty" yaml:"tokenFile,omitempty"`
	CAFile               string   `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	Insecure             bool     `json:"insecure,omitempty" yaml:"insecure,omitempty"`
	ServiceAccount       string   `json:"serviceAccount,omitempty" yaml:"serviceAccount,omitempty"`
	RuntimeClassName     string   `json:"runtimeClassName,omitempty" yaml:"runtimeClassName,omitempty"`
	NetworkPolicyEnabled bool     `json:"networkPolicyEnabled" yaml:"networkPolicyEnabled"`
	DefaultNetworkMode   string   `json:"defaultNetworkMode" yaml:"defaultNetworkMode"`
	DefaultEgressCIDRs   []string `json:"defaultEgressCidrs,omitempty" yaml:"defaultEgressCidrs,omitempty"`

	Image              string `json:"image" yaml:"image"`
	ImagePullPolicy    string `json:"imagePullPolicy" yaml:"imagePullPolicy"`
	WorkspaceMountPath string `json:"workspaceMountPath" yaml:"workspaceMountPath"`
	StorageClass       string `json:"storageClass,omitempty" yaml:"storageClass,omitempty"`
	WorkspaceSize      string `json:"workspaceSize" yaml:"workspaceSize"`
	ToolPort           int    `json:"toolPort" yaml:"toolPort"`
	BrowserPort        int    `json:"browserPort" yaml:"browserPort"`
	VNCOrWebPort       int    `json:"vncOrWebPort" yaml:"vncOrWebPort"`
	DefaultCPU         string `json:"defaultCpu" yaml:"defaultCpu"`
	DefaultMemory      string `json:"defaultMemory" yaml:"defaultMemory"`
	MaxCPU             string `json:"maxCpu,omitempty" yaml:"maxCpu,omitempty"`
	MaxMemory          string `json:"maxMemory,omitempty" yaml:"maxMemory,omitempty"`
	IdleTTLSeconds     int    `json:"idleTtlSeconds" yaml:"idleTtlSeconds"`
	LeaseTTLSeconds    int    `json:"leaseTtlSeconds" yaml:"leaseTtlSeconds"`
}

func Default() Config {
	return Config{
		Server:   ServerConfig{Addr: ":18082"},
		Auth:     AuthConfig{Enabled: false, Mode: "none", FailClosed: true, App: "aihub", CookieName: "aisphere_session", SessionIntrospectPath: "/auth/sessions/introspect", IAMCheckPath: "/iam/resource-grants/check"},
		Database: DatabaseConfig{Driver: "postgres", AutoMigrate: true},
		Sandbox: SandboxConfig{
			Driver:                 "agent-sandbox",
			AgentSandboxAPIVersion: "v1beta1",
			UseClaim:               false,
			DefaultTemplate:        "aisphere-agent-session",
			DefaultProfile:         "default-python-offline",
			Namespace:              "aisphere-sandbox",
			CreateNamespace:        true,
			ServiceAccount:         "agentkit-sandbox-controller",
			NetworkPolicyEnabled:   true,
			DefaultNetworkMode:     "offline",
			Image:                  "registry.local/aisphere/agentkit-sandbox:latest",
			ImagePullPolicy:        "IfNotPresent",
			WorkspaceMountPath:     "/workspace",
			WorkspaceSize:          "10Gi",
			ToolPort:               18081,
			BrowserPort:            9222,
			VNCOrWebPort:           6080,
			DefaultCPU:             "500m",
			DefaultMemory:          "1Gi",
			MaxCPU:                 "2",
			MaxMemory:              "4Gi",
			IdleTTLSeconds:         3600,
			LeaseTTLSeconds:        900,
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, err
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".yaml" || ext == ".yml" {
			if err := decodeYAML(b, &cfg); err != nil {
				return cfg, err
			}
		} else if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config json: %w", err)
		}
	}
	applyEnv(&cfg)
	return cfg, nil
}

func decodeYAML(b []byte, cfg *Config) error {
	decoder := yaml.NewDecoder(bytes.NewReader(b))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return fmt.Errorf("parse config yaml: %w", err)
	}
	return nil
}

func applyEnv(c *Config) {
	setStr := func(p *string, key string) {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			*p = v
		}
	}
	setBool := func(p *bool, key string) {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			if b, err := strconv.ParseBool(v); err == nil {
				*p = b
			}
		}
	}
	setInt := func(p *int, key string) {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			if i, err := strconv.Atoi(v); err == nil {
				*p = i
			}
		}
	}
	setList := func(p *[]string, key string) {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			*p = splitCSV(v)
		}
	}

	setStr(&c.Server.Addr, "SANDBOX_MANAGER_ADDR")
	setBool(&c.Auth.Enabled, "SANDBOX_AUTH_ENABLED")
	setStr(&c.Auth.Mode, "SANDBOX_AUTH_MODE")
	setStr(&c.Auth.AuthEndpoint, "SANDBOX_AUTH_ENDPOINT")
	setStr(&c.Auth.ServiceToken, "SANDBOX_AUTH_SERVICE_TOKEN")
	setBool(&c.Auth.FailClosed, "SANDBOX_AUTH_FAIL_CLOSED")
	setStr(&c.Auth.App, "SANDBOX_AUTH_APP")

	setStr(&c.Database.Driver, "SANDBOX_DATABASE_DRIVER")
	setStr(&c.Database.DSN, "SANDBOX_DATABASE_DSN")
	setBool(&c.Database.AutoMigrate, "SANDBOX_DATABASE_AUTO_MIGRATE")

	setStr(&c.Sandbox.Driver, "SANDBOX_DRIVER")
	setStr(&c.Sandbox.AgentSandboxAPIVersion, "SANDBOX_AGENT_API_VERSION")
	setBool(&c.Sandbox.UseClaim, "SANDBOX_USE_CLAIM")
	setStr(&c.Sandbox.DefaultTemplate, "SANDBOX_DEFAULT_TEMPLATE")
	setStr(&c.Sandbox.DefaultWarmPool, "SANDBOX_DEFAULT_WARM_POOL")
	setStr(&c.Sandbox.DefaultProfile, "SANDBOX_DEFAULT_PROFILE")
	setStr(&c.Sandbox.Namespace, "SANDBOX_K8S_NAMESPACE")
	setBool(&c.Sandbox.CreateNamespace, "SANDBOX_K8S_CREATE_NAMESPACE")
	setStr(&c.Sandbox.APIServer, "SANDBOX_K8S_API_SERVER")
	setStr(&c.Sandbox.Token, "SANDBOX_K8S_TOKEN")
	setStr(&c.Sandbox.TokenFile, "SANDBOX_K8S_TOKEN_FILE")
	setStr(&c.Sandbox.CAFile, "SANDBOX_K8S_CA_FILE")
	setBool(&c.Sandbox.Insecure, "SANDBOX_K8S_INSECURE")
	setStr(&c.Sandbox.ServiceAccount, "SANDBOX_K8S_SERVICE_ACCOUNT")
	setStr(&c.Sandbox.RuntimeClassName, "SANDBOX_K8S_RUNTIME_CLASS_NAME")
	setBool(&c.Sandbox.NetworkPolicyEnabled, "SANDBOX_K8S_NETWORK_POLICY_ENABLED")
	setStr(&c.Sandbox.DefaultNetworkMode, "SANDBOX_DEFAULT_NETWORK_MODE")
	setList(&c.Sandbox.DefaultEgressCIDRs, "SANDBOX_DEFAULT_EGRESS_CIDRS")
	setStr(&c.Sandbox.Image, "SANDBOX_IMAGE")
	setStr(&c.Sandbox.ImagePullPolicy, "SANDBOX_IMAGE_PULL_POLICY")
	setStr(&c.Sandbox.WorkspaceMountPath, "SANDBOX_WORKSPACE_MOUNT_PATH")
	setStr(&c.Sandbox.StorageClass, "SANDBOX_STORAGE_CLASS")
	setStr(&c.Sandbox.WorkspaceSize, "SANDBOX_WORKSPACE_SIZE")
	setInt(&c.Sandbox.ToolPort, "SANDBOX_TOOL_PORT")
	setInt(&c.Sandbox.BrowserPort, "SANDBOX_BROWSER_PORT")
	setInt(&c.Sandbox.VNCOrWebPort, "SANDBOX_WEB_PORT")
	setStr(&c.Sandbox.DefaultCPU, "SANDBOX_DEFAULT_CPU")
	setStr(&c.Sandbox.DefaultMemory, "SANDBOX_DEFAULT_MEMORY")
	setStr(&c.Sandbox.MaxCPU, "SANDBOX_MAX_CPU")
	setStr(&c.Sandbox.MaxMemory, "SANDBOX_MAX_MEMORY")
	setInt(&c.Sandbox.IdleTTLSeconds, "SANDBOX_IDLE_TTL_SECONDS")
	setInt(&c.Sandbox.LeaseTTLSeconds, "SANDBOX_LEASE_TTL_SECONDS")
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
