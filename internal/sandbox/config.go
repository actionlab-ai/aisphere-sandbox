package sandbox

// Config is the runtime-only sandbox manager configuration.
type Config struct {
	Enabled bool
	Driver  string

	AgentSandboxAPIVersion string
	UseClaim               bool
	DefaultTemplate        string
	DefaultWarmPool        string
	DefaultProfile         string

	Namespace            string
	CreateNamespace      bool
	APIServer            string
	Kubeconfig           string
	Token                string
	TokenFile            string
	CAFile               string
	Insecure             bool
	ServiceAccount       string
	RuntimeClassName     string
	NetworkPolicyEnabled bool
	DefaultNetworkMode   string
	DefaultEgressCIDRs   []string

	Image              string
	ImagePullPolicy    string
	WorkspaceMountPath string
	StorageClass       string
	WorkspaceSize      string
	ToolPort           int
	BrowserPort        int
	VNCOrWebPort       int
	DefaultCPU         string
	DefaultMemory      string
	MaxCPU             string
	MaxMemory          string
	IdleTTLSeconds     int
	LeaseTTLSeconds    int
}
