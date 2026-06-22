package model

// RuntimeServiceManifest is the generic service snapshot item resolved by Hub and
// passed into the sandbox as tool/service manifest. Sandbox Manager does not
// understand Agent/Skill/Tool business semantics; it only carries this payload
// into the Pod ConfigMap for the sandbox tool-server and sidecars.
type RuntimeServiceManifest struct {
	Kind        string                   `json:"kind"`
	Name        string                   `json:"name"`
	Alias       string                   `json:"alias,omitempty"`
	Provider    string                   `json:"provider,omitempty"`
	Object      string                   `json:"object,omitempty"`
	Version     string                   `json:"version,omitempty"`
	Label       string                   `json:"label,omitempty"`
	Revision    string                   `json:"revision,omitempty"`
	Status      string                   `json:"status,omitempty"`
	Required    bool                     `json:"required,omitempty"`
	Reload      string                   `json:"reload,omitempty"`
	MountPath   string                   `json:"mountPath,omitempty"`
	ChangeToken string                   `json:"changeToken,omitempty"`
	SnapshotID  string                   `json:"snapshotId,omitempty"`
	Runtime     map[string]interface{}   `json:"runtime,omitempty"`
	Execution   map[string]interface{}   `json:"execution,omitempty"`
	Config      map[string]interface{}   `json:"config,omitempty"`
	Payload     map[string]interface{}   `json:"payload,omitempty"`
	Metadata    map[string]interface{}   `json:"metadata,omitempty"`
	DependsOn   []RuntimeServiceManifest `json:"dependsOn,omitempty"`
}
