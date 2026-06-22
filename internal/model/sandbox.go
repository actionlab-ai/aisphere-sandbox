package model

const (
	SandboxDriverKubernetes   = "direct-kubernetes"
	SandboxDriverAgentSandbox = "agent-sandbox"

	SandboxNetworkModeOffline    = "offline"
	SandboxNetworkModeRestricted = "restricted"
	SandboxNetworkModeOnline     = "online"

	SandboxPhasePending   = "Pending"
	SandboxPhaseRunning   = "Running"
	SandboxPhaseSucceeded = "Succeeded"
	SandboxPhaseFailed    = "Failed"
	SandboxPhaseUnknown   = "Unknown"
)

type SandboxLimits struct {
	CPU               string `json:"cpu,omitempty"`
	Memory            string `json:"memory,omitempty"`
	Storage           string `json:"storage,omitempty"`
	IdleTTLSeconds    int64  `json:"idleTtlSeconds,omitempty"`
	MaxSessionSeconds int64  `json:"maxSessionSeconds,omitempty"`
}

type SandboxToolMount struct {
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"` // pvc / configmap / secret / emptyDir
	Ref       string `json:"ref,omitempty"`  // existing PVC/ConfigMap/Secret name
	MountPath string `json:"mountPath"`
	Mode      string `json:"mode,omitempty"` // ro / rw
}

type SandboxNetworkPolicy struct {
	Mode        string   `json:"mode,omitempty"`        // offline / restricted / online
	EgressCIDRs []string `json:"egressCidrs,omitempty"` // used when mode=restricted
}

type SandboxEnsureRequest struct {
	SandboxID       string                   `json:"sandboxId,omitempty"`
	RuntimeID       string                   `json:"runtimeId,omitempty"`
	SessionID       string                   `json:"sessionId,omitempty"`
	RunID           string                   `json:"runId,omitempty"`
	OwnerSubject    string                   `json:"ownerSubject,omitempty"`
	OrgID           string                   `json:"orgId,omitempty"`
	ProjectID       string                   `json:"projectId,omitempty"`
	AgentID         string                   `json:"agentId,omitempty"`
	AgentVersion    string                   `json:"agentVersion,omitempty"`
	SnapshotID      string                   `json:"snapshotId,omitempty"`
	Profile         string                   `json:"profile,omitempty"`
	TemplateRef     string                   `json:"templateRef,omitempty"`
	WarmPoolRef     string                   `json:"warmPoolRef,omitempty"`
	Reuse           bool                     `json:"reuse,omitempty"`
	Image           string                   `json:"image,omitempty"`
	ImagePullPolicy string                   `json:"imagePullPolicy,omitempty"`
	WorkspacePVC    string                   `json:"workspacePvc,omitempty"`
	WorkspaceSize   string                   `json:"workspaceSize,omitempty"`
	StorageClass    string                   `json:"storageClass,omitempty"`
	Network         SandboxNetworkPolicy     `json:"network,omitempty"`
	Restart         bool                     `json:"restart,omitempty"`
	DeleteWorkspace bool                     `json:"deleteWorkspace,omitempty"`
	Limits          SandboxLimits            `json:"limits,omitempty"`
	Services        []RuntimeServiceManifest `json:"services,omitempty"`
	ToolMounts      []SandboxToolMount       `json:"toolMounts,omitempty"`
	Metadata        map[string]interface{}   `json:"metadata,omitempty"`
}

type SandboxDeleteRequest struct {
	DeleteWorkspace bool `json:"deleteWorkspace,omitempty"`
}

type SandboxLogQuery struct {
	Container string `json:"container,omitempty"`
	TailLines int64  `json:"tailLines,omitempty"`
}

type SandboxEndpoint struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Port int    `json:"port,omitempty"`
}

type SandboxLease struct {
	Token     string `json:"token,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

type SandboxStatus struct {
	SandboxID    string            `json:"sandboxId"`
	Namespace    string            `json:"namespace,omitempty"`
	Driver       string            `json:"driver"`
	Phase        string            `json:"phase"`
	Reason       string            `json:"reason,omitempty"`
	Message      string            `json:"message,omitempty"`
	PodName      string            `json:"podName,omitempty"`
	PodIP        string            `json:"podIp,omitempty"`
	NodeName     string            `json:"nodeName,omitempty"`
	ServiceName  string            `json:"serviceName,omitempty"`
	WorkspacePVC string            `json:"workspacePvc,omitempty"`
	Profile      string            `json:"profile,omitempty"`
	TemplateRef  string            `json:"templateRef,omitempty"`
	WarmPoolRef  string            `json:"warmPoolRef,omitempty"`
	Image        string            `json:"image,omitempty"`
	NetworkMode  string            `json:"networkMode,omitempty"`
	RuntimeID    string            `json:"runtimeId,omitempty"`
	OwnerSubject string            `json:"ownerSubject,omitempty"`
	OrgID        string            `json:"orgId,omitempty"`
	ProjectID    string            `json:"projectId,omitempty"`
	SessionID    string            `json:"sessionId,omitempty"`
	RunID        string            `json:"runId,omitempty"`
	AgentID      string            `json:"agentId,omitempty"`
	SnapshotID   string            `json:"snapshotId,omitempty"`
	Endpoints    []SandboxEndpoint `json:"endpoints,omitempty"`
	Lease        *SandboxLease     `json:"lease,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	CreatedAt    string            `json:"createdAt,omitempty"`
	UpdatedAt    string            `json:"updatedAt,omitempty"`
}
