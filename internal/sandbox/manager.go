package sandbox

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/actionlab-ai/aisphere-sandbox/internal/model"
)

const (
	labelManagedBy = "aisphere.io/managed-by"
	labelSandboxID = "aisphere.io/sandbox-id"
	labelOrgID     = "aisphere.io/org-id"
	labelProjectID = "aisphere.io/project-id"
	labelSessionID = "aisphere.io/session-id"
	labelAgentID   = "aisphere.io/agent-id"
)

type Manager interface {
	Ensure(ctx context.Context, req model.SandboxEnsureRequest) (*model.SandboxStatus, error)
	Get(ctx context.Context, sandboxID string) (*model.SandboxStatus, error)
	List(ctx context.Context, q ListQuery) ([]*model.SandboxStatus, error)
	Restart(ctx context.Context, sandboxID string) (*model.SandboxStatus, error)
	Delete(ctx context.Context, sandboxID string, deleteWorkspace bool) error
	Logs(ctx context.Context, sandboxID string, q model.SandboxLogQuery) (string, error)
}

type ListQuery struct {
	OwnerSubject string
	OrgID        string
	ProjectID    string
	SessionID    string
	AgentID      string
}

type K8sManager struct {
	cfg       Config
	namespace string
	baseURL   string
	token     string
	client    *http.Client
}

func NewKubernetesManager(cfg Config) (*K8sManager, error) {
	if cfg.Namespace == "" {
		cfg.Namespace = "aisphere-sandbox"
	}
	if cfg.WorkspaceMountPath == "" {
		cfg.WorkspaceMountPath = "/workspace"
	}
	if cfg.Image == "" {
		cfg.Image = "registry.local/aisphere/agentkit-sandbox:latest"
	}
	if cfg.ImagePullPolicy == "" {
		cfg.ImagePullPolicy = "IfNotPresent"
	}
	if cfg.WorkspaceSize == "" {
		cfg.WorkspaceSize = "10Gi"
	}
	if cfg.ToolPort <= 0 {
		cfg.ToolPort = 18081
	}
	if cfg.BrowserPort <= 0 {
		cfg.BrowserPort = 9222
	}
	if cfg.VNCOrWebPort <= 0 {
		cfg.VNCOrWebPort = 6080
	}
	if cfg.DefaultCPU == "" {
		cfg.DefaultCPU = "500m"
	}
	if cfg.DefaultMemory == "" {
		cfg.DefaultMemory = "1Gi"
	}
	if cfg.DefaultNetworkMode == "" {
		cfg.DefaultNetworkMode = model.SandboxNetworkModeOffline
	}

	baseURL, token, caPEM, insecure, err := resolveKubernetesAuth(cfg)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("kubernetes api server is not configured; set sandbox.kubernetes.apiServer, kubeconfig, or run in-cluster")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecure || len(caPEM) > 0 {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecure} //nolint:gosec // operator-controlled bootstrap option
		if len(caPEM) > 0 {
			pool := x509.NewCertPool()
			if ok := pool.AppendCertsFromPEM(caPEM); ok {
				transport.TLSClientConfig.RootCAs = pool
			}
		}
	}
	m := &K8sManager{cfg: cfg, namespace: cfg.Namespace, baseURL: strings.TrimRight(baseURL, "/"), token: token, client: &http.Client{Transport: transport, Timeout: 20 * time.Second}}
	if cfg.CreateNamespace {
		_ = m.ensureNamespace(context.Background())
	}
	return m, nil
}

func (m *K8sManager) Ensure(ctx context.Context, req model.SandboxEnsureRequest) (*model.SandboxStatus, error) {
	id := normalizeSandboxID(req)
	req.SandboxID = id
	if req.Image == "" {
		req.Image = m.cfg.Image
	}
	if req.ImagePullPolicy == "" {
		req.ImagePullPolicy = m.cfg.ImagePullPolicy
	}
	if req.WorkspaceSize == "" {
		req.WorkspaceSize = firstNonEmpty(req.Limits.Storage, m.cfg.WorkspaceSize)
	}
	if req.StorageClass == "" {
		req.StorageClass = m.cfg.StorageClass
	}
	if req.Limits.CPU == "" {
		req.Limits.CPU = m.cfg.DefaultCPU
	}
	if req.Limits.Memory == "" {
		req.Limits.Memory = m.cfg.DefaultMemory
	}
	if req.Limits.IdleTTLSeconds == 0 {
		req.Limits.IdleTTLSeconds = int64(m.cfg.IdleTTLSeconds)
	}
	if req.WorkspacePVC == "" {
		req.WorkspacePVC = workspacePVCName(id)
	}
	req.Network.Mode = normalizeNetworkMode(firstNonEmpty(req.Network.Mode, m.cfg.DefaultNetworkMode))
	if len(req.Network.EgressCIDRs) == 0 && len(m.cfg.DefaultEgressCIDRs) > 0 {
		req.Network.EgressCIDRs = append([]string{}, m.cfg.DefaultEgressCIDRs...)
	}
	if req.Restart {
		_ = m.deletePod(ctx, id)
	}
	if err := m.ensurePVC(ctx, req); err != nil {
		return nil, err
	}
	if err := m.ensureConfigMap(ctx, req); err != nil {
		return nil, err
	}
	if err := m.ensureService(ctx, req); err != nil {
		return nil, err
	}
	if err := m.ensureNetworkPolicy(ctx, req); err != nil {
		return nil, err
	}
	if err := m.ensurePod(ctx, req); err != nil {
		return nil, err
	}
	return m.Get(ctx, id)
}

func (m *K8sManager) Get(ctx context.Context, sandboxID string) (*model.SandboxStatus, error) {
	id := cleanDNSName(sandboxID)
	var pod k8sObject
	if err := m.getJSON(ctx, "/api/v1/namespaces/"+url.PathEscape(m.namespace)+"/pods/"+url.PathEscape(podName(id)), &pod); err != nil {
		return nil, err
	}
	return m.statusFromPod(id, &pod), nil
}

func (m *K8sManager) List(ctx context.Context, q ListQuery) ([]*model.SandboxStatus, error) {
	selector := labelManagedBy + "=aisphere"
	if q.OrgID != "" {
		selector += "," + labelOrgID + "=" + labelValue(q.OrgID)
	}
	if q.ProjectID != "" {
		selector += "," + labelProjectID + "=" + labelValue(q.ProjectID)
	}
	if q.SessionID != "" {
		selector += "," + labelSessionID + "=" + labelValue(q.SessionID)
	}
	if q.AgentID != "" {
		selector += "," + labelAgentID + "=" + labelValue(q.AgentID)
	}
	var list k8sList
	p := "/api/v1/namespaces/" + url.PathEscape(m.namespace) + "/pods?labelSelector=" + url.QueryEscape(selector)
	if err := m.getJSON(ctx, p, &list); err != nil {
		return nil, err
	}
	out := make([]*model.SandboxStatus, 0, len(list.Items))
	for i := range list.Items {
		id := list.Items[i].Metadata.Labels[labelSandboxID]
		if id == "" {
			id = strings.TrimPrefix(list.Items[i].Metadata.Name, "aisb-")
		}
		if q.OwnerSubject != "" && list.Items[i].Metadata.Annotations["aisphere.io/owner-subject"] != q.OwnerSubject {
			continue
		}
		out = append(out, m.statusFromPod(id, &list.Items[i]))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (m *K8sManager) Restart(ctx context.Context, sandboxID string) (*model.SandboxStatus, error) {
	id := cleanDNSName(sandboxID)
	if err := m.deletePod(ctx, id); err != nil && !isNotFound(err) {
		return nil, err
	}
	var cm k8sObject
	if err := m.getJSON(ctx, "/api/v1/namespaces/"+url.PathEscape(m.namespace)+"/configmaps/"+url.PathEscape(configMapName(id)), &cm); err != nil {
		return nil, err
	}
	var req model.SandboxEnsureRequest
	if raw, ok := cm.Data["sandbox-request.json"]; ok {
		_ = json.Unmarshal([]byte(raw), &req)
	}
	req.SandboxID = id
	req.Restart = false
	return m.Ensure(ctx, req)
}

func (m *K8sManager) Delete(ctx context.Context, sandboxID string, deleteWorkspace bool) error {
	id := cleanDNSName(sandboxID)
	for _, fn := range []func(context.Context, string) error{m.deletePod, m.deleteService, m.deleteConfigMap, m.deleteNetworkPolicy} {
		if err := fn(ctx, id); err != nil && !isNotFound(err) {
			return err
		}
	}
	if deleteWorkspace {
		if err := m.deletePVC(ctx, workspacePVCName(id)); err != nil && !isNotFound(err) {
			return err
		}
	}
	return nil
}

func (m *K8sManager) Logs(ctx context.Context, sandboxID string, q model.SandboxLogQuery) (string, error) {
	id := cleanDNSName(sandboxID)
	if q.Container == "" {
		q.Container = "sandbox"
	}
	if q.TailLines <= 0 || q.TailLines > 2000 {
		q.TailLines = 200
	}
	p := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log?container=%s&tailLines=%d", url.PathEscape(m.namespace), url.PathEscape(podName(id)), url.QueryEscape(q.Container), q.TailLines)
	b, err := m.do(ctx, http.MethodGet, p, nil, "")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (m *K8sManager) ensureNamespace(ctx context.Context) error {
	obj := map[string]interface{}{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]interface{}{"name": m.namespace, "labels": map[string]string{labelManagedBy: "aisphere"}}}
	return m.createIgnoreExists(ctx, "/api/v1/namespaces", obj)
}

func (m *K8sManager) ensurePVC(ctx context.Context, req model.SandboxEnsureRequest) error {
	pvc := map[string]interface{}{
		"apiVersion": "v1", "kind": "PersistentVolumeClaim",
		"metadata": map[string]interface{}{"name": req.WorkspacePVC, "labels": sandboxLabels(req), "annotations": sandboxAnnotations(req)},
		"spec": map[string]interface{}{
			"accessModes": []string{"ReadWriteOnce"},
			"resources":   map[string]interface{}{"requests": map[string]string{"storage": req.WorkspaceSize}},
		},
	}
	if req.StorageClass != "" {
		pvc["spec"].(map[string]interface{})["storageClassName"] = req.StorageClass
	}
	return m.createIgnoreExists(ctx, "/api/v1/namespaces/"+url.PathEscape(m.namespace)+"/persistentvolumeclaims", pvc)
}

func (m *K8sManager) ensureConfigMap(ctx context.Context, req model.SandboxEnsureRequest) error {
	manifest := map[string]interface{}{"services": req.Services, "toolMounts": req.ToolMounts, "metadata": req.Metadata, "generatedAt": time.Now().UTC().Format(time.RFC3339)}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	reqBytes, _ := json.MarshalIndent(req, "", "  ")
	cm := map[string]interface{}{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]interface{}{"name": configMapName(req.SandboxID), "labels": sandboxLabels(req), "annotations": sandboxAnnotations(req)},
		"data":     map[string]string{"tool-manifest.json": string(manifestBytes), "sandbox-request.json": string(reqBytes)},
	}
	basePath := "/api/v1/namespaces/" + url.PathEscape(m.namespace) + "/configmaps"
	itemPath := basePath + "/" + url.PathEscape(configMapName(req.SandboxID))
	var existing k8sObject
	if err := m.getJSON(ctx, itemPath, &existing); err == nil {
		return m.putJSON(ctx, itemPath, cm)
	} else if !isNotFound(err) {
		return err
	}
	return m.createIgnoreExists(ctx, basePath, cm)
}

func (m *K8sManager) ensureService(ctx context.Context, req model.SandboxEnsureRequest) error {
	svc := map[string]interface{}{
		"apiVersion": "v1", "kind": "Service",
		"metadata": map[string]interface{}{"name": serviceName(req.SandboxID), "labels": sandboxLabels(req), "annotations": sandboxAnnotations(req)},
		"spec": map[string]interface{}{
			"type":     "ClusterIP",
			"selector": map[string]string{labelSandboxID: labelValue(req.SandboxID), labelManagedBy: "aisphere"},
			"ports": []map[string]interface{}{
				{"name": "tools", "port": m.cfg.ToolPort, "targetPort": m.cfg.ToolPort},
				{"name": "browser", "port": m.cfg.BrowserPort, "targetPort": m.cfg.BrowserPort},
				{"name": "web", "port": m.cfg.VNCOrWebPort, "targetPort": m.cfg.VNCOrWebPort},
			},
		},
	}
	return m.createIgnoreExists(ctx, "/api/v1/namespaces/"+url.PathEscape(m.namespace)+"/services", svc)
}

func (m *K8sManager) ensureNetworkPolicy(ctx context.Context, req model.SandboxEnsureRequest) error {
	if !m.cfg.NetworkPolicyEnabled {
		return nil
	}
	mode := normalizeNetworkMode(req.Network.Mode)
	basePath := "/apis/networking.k8s.io/v1/namespaces/" + url.PathEscape(m.namespace) + "/networkpolicies"
	itemPath := basePath + "/" + url.PathEscape(networkPolicyName(req.SandboxID))
	if mode == model.SandboxNetworkModeOnline {
		if err := m.delete(ctx, itemPath); err != nil && !isNotFound(err) {
			return err
		}
		return nil
	}
	egress := []map[string]interface{}{}
	if mode == model.SandboxNetworkModeRestricted {
		// Permit DNS to kube-dns/CoreDNS by namespace/pod label. This keeps name
		// resolution working while still denying arbitrary egress unless CIDRs are
		// explicitly allowed below.
		egress = append(egress, map[string]interface{}{
			"to": []map[string]interface{}{{
				"namespaceSelector": map[string]interface{}{"matchLabels": map[string]string{"kubernetes.io/metadata.name": "kube-system"}},
				"podSelector":       map[string]interface{}{"matchLabels": map[string]string{"k8s-app": "kube-dns"}},
			}},
			"ports": []map[string]interface{}{{"protocol": "UDP", "port": 53}, {"protocol": "TCP", "port": 53}},
		})
		for _, cidr := range req.Network.EgressCIDRs {
			cidr = strings.TrimSpace(cidr)
			if cidr == "" || strings.Contains(cidr, "..") {
				continue
			}
			egress = append(egress, map[string]interface{}{"to": []map[string]interface{}{{"ipBlock": map[string]string{"cidr": cidr}}}})
		}
	}
	np := map[string]interface{}{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata":   map[string]interface{}{"name": networkPolicyName(req.SandboxID), "labels": sandboxLabels(req), "annotations": sandboxAnnotations(req)},
		"spec": map[string]interface{}{
			"podSelector": map[string]interface{}{"matchLabels": map[string]string{labelSandboxID: labelValue(req.SandboxID), labelManagedBy: "aisphere"}},
			"policyTypes": []string{"Egress"},
			"egress":      egress,
		},
	}
	var existing k8sObject
	if err := m.getJSON(ctx, itemPath, &existing); err == nil {
		return m.putJSON(ctx, itemPath, np)
	} else if !isNotFound(err) {
		return err
	}
	return m.createIgnoreExists(ctx, basePath, np)
}

func (m *K8sManager) ensurePod(ctx context.Context, req model.SandboxEnsureRequest) error {
	pod := m.buildPod(req)
	return m.createIgnoreExists(ctx, "/api/v1/namespaces/"+url.PathEscape(m.namespace)+"/pods", pod)
}

func (m *K8sManager) buildPod(req model.SandboxEnsureRequest) map[string]interface{} {
	mountPath := firstNonEmpty(m.cfg.WorkspaceMountPath, "/workspace")
	labels := sandboxLabels(req)
	annotations := sandboxAnnotations(req)
	annotations["aisphere.io/workspace-mount-path"] = mountPath
	annotations["aisphere.io/idle-ttl-seconds"] = fmt.Sprintf("%d", req.Limits.IdleTTLSeconds)
	volumes := []map[string]interface{}{
		{"name": "workspace", "persistentVolumeClaim": map[string]interface{}{"claimName": req.WorkspacePVC}},
		{"name": "sandbox-manifest", "configMap": map[string]interface{}{"name": configMapName(req.SandboxID)}},
	}
	volumeMounts := []map[string]interface{}{
		{"name": "workspace", "mountPath": mountPath},
		{"name": "sandbox-manifest", "mountPath": "/etc/aisphere/sandbox", "readOnly": true},
	}
	for _, tm := range req.ToolMounts {
		if tm.Name == "" || tm.MountPath == "" || strings.Contains(tm.MountPath, "..") || tm.MountPath == "/" {
			continue
		}
		vname := cleanDNSName("tool-" + tm.Name)
		vol, vm := toolMountVolume(vname, tm)
		if vol != nil && vm != nil {
			volumes = append(volumes, vol)
			volumeMounts = append(volumeMounts, vm)
		}
	}
	resources := map[string]interface{}{
		"requests": map[string]string{"cpu": req.Limits.CPU, "memory": req.Limits.Memory},
		"limits":   map[string]string{"cpu": firstNonEmpty(m.cfg.MaxCPU, req.Limits.CPU), "memory": firstNonEmpty(m.cfg.MaxMemory, req.Limits.Memory)},
	}
	container := map[string]interface{}{
		"name":            "sandbox",
		"image":           req.Image,
		"imagePullPolicy": req.ImagePullPolicy,
		"workingDir":      mountPath,
		"ports": []map[string]interface{}{
			{"name": "tools", "containerPort": m.cfg.ToolPort},
			{"name": "browser", "containerPort": m.cfg.BrowserPort},
			{"name": "web", "containerPort": m.cfg.VNCOrWebPort},
		},
		"env": []map[string]string{
			{"name": "AISPHERE_SANDBOX_ID", "value": req.SandboxID},
			{"name": "AISPHERE_RUNTIME_ID", "value": req.RuntimeID},
			{"name": "AISPHERE_SESSION_ID", "value": req.SessionID},
			{"name": "AISPHERE_RUN_ID", "value": req.RunID},
			{"name": "AISPHERE_AGENT_ID", "value": req.AgentID},
			{"name": "AISPHERE_SNAPSHOT_ID", "value": req.SnapshotID},
			{"name": "AISPHERE_WORKSPACE", "value": mountPath},
			{"name": "AISPHERE_TOOL_PORT", "value": fmt.Sprintf("%d", m.cfg.ToolPort)},
			{"name": "AISPHERE_BROWSER_PORT", "value": fmt.Sprintf("%d", m.cfg.BrowserPort)},
			{"name": "AISPHERE_WEB_PORT", "value": fmt.Sprintf("%d", m.cfg.VNCOrWebPort)},
			{"name": "AISPHERE_TOOL_MANIFEST", "value": "/etc/aisphere/sandbox/tool-manifest.json"},
			{"name": "AISPHERE_NETWORK_MODE", "value": normalizeNetworkMode(req.Network.Mode)},
		},
		"resources":       resources,
		"volumeMounts":    volumeMounts,
		"securityContext": map[string]interface{}{"allowPrivilegeEscalation": false, "privileged": false, "runAsNonRoot": true, "runAsUser": 1000, "runAsGroup": 1000, "readOnlyRootFilesystem": false, "capabilities": map[string]interface{}{"drop": []string{"ALL"}}, "seccompProfile": map[string]string{"type": "RuntimeDefault"}},
	}
	podSpec := map[string]interface{}{
		"restartPolicy":      "Always",
		"serviceAccountName": m.cfg.ServiceAccount,
		"containers":         []map[string]interface{}{container},
		"volumes":            volumes,
		"securityContext":    map[string]interface{}{"runAsNonRoot": true, "runAsUser": 1000, "runAsGroup": 1000, "fsGroup": 1000, "seccompProfile": map[string]string{"type": "RuntimeDefault"}},
	}
	if m.cfg.RuntimeClassName != "" {
		podSpec["runtimeClassName"] = m.cfg.RuntimeClassName
	}
	return map[string]interface{}{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]interface{}{"name": podName(req.SandboxID), "labels": labels, "annotations": annotations},
		"spec":     podSpec,
	}
}

func toolMountVolume(name string, tm model.SandboxToolMount) (map[string]interface{}, map[string]interface{}) {
	typeName := strings.ToLower(firstNonEmpty(tm.Type, "pvc"))
	ref := strings.TrimSpace(tm.Ref)
	if ref == "" {
		ref = tm.Name
	}
	vol := map[string]interface{}{"name": name}
	switch typeName {
	case "pvc", "persistentvolumeclaim":
		vol["persistentVolumeClaim"] = map[string]interface{}{"claimName": cleanDNSName(ref)}
	case "configmap", "config-map":
		vol["configMap"] = map[string]interface{}{"name": cleanDNSName(ref)}
	case "secret":
		vol["secret"] = map[string]interface{}{"secretName": cleanDNSName(ref)}
	case "emptydir", "empty-dir":
		vol["emptyDir"] = map[string]interface{}{}
	default:
		return nil, nil
	}
	vm := map[string]interface{}{"name": name, "mountPath": path.Clean(tm.MountPath)}
	if strings.EqualFold(tm.Mode, "ro") || strings.EqualFold(tm.Mode, "readonly") {
		vm["readOnly"] = true
	}
	return vol, vm
}

func (m *K8sManager) statusFromPod(id string, pod *k8sObject) *model.SandboxStatus {
	phase := pod.Status.Phase
	if phase == "" {
		phase = model.SandboxPhaseUnknown
	}
	labels := copyMap(pod.Metadata.Labels)
	annotations := copyMap(pod.Metadata.Annotations)
	return &model.SandboxStatus{
		SandboxID:    id,
		Namespace:    m.namespace,
		Driver:       model.SandboxDriverKubernetes,
		Phase:        phase,
		Reason:       pod.Status.Reason,
		Message:      pod.Status.Message,
		PodName:      pod.Metadata.Name,
		PodIP:        pod.Status.PodIP,
		NodeName:     pod.Spec.NodeName,
		ServiceName:  serviceName(id),
		WorkspacePVC: annotations["aisphere.io/workspace-pvc"],
		Image:        annotations["aisphere.io/image"],
		NetworkMode:  annotations["aisphere.io/network-mode"],
		RuntimeID:    annotations["aisphere.io/runtime-id"],
		SessionID:    annotations["aisphere.io/session-id"],
		RunID:        annotations["aisphere.io/run-id"],
		AgentID:      annotations["aisphere.io/agent-id"],
		SnapshotID:   annotations["aisphere.io/snapshot-id"],
		Endpoints:    m.sandboxEndpoints(id),
		Labels:       labels,
		Annotations:  annotations,
		CreatedAt:    pod.Metadata.CreationTimestamp,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
}

func (m *K8sManager) sandboxEndpoints(id string) []model.SandboxEndpoint {
	dns := fmt.Sprintf("%s.%s.svc", serviceName(id), m.namespace)
	return []model.SandboxEndpoint{
		{Name: "tools", URL: fmt.Sprintf("http://%s:%d", dns, m.cfg.ToolPort), Port: m.cfg.ToolPort},
		{Name: "browser", URL: fmt.Sprintf("http://%s:%d", dns, m.cfg.BrowserPort), Port: m.cfg.BrowserPort},
		{Name: "web", URL: fmt.Sprintf("http://%s:%d", dns, m.cfg.VNCOrWebPort), Port: m.cfg.VNCOrWebPort},
	}
}

func (m *K8sManager) deletePod(ctx context.Context, id string) error {
	return m.delete(ctx, "/api/v1/namespaces/"+url.PathEscape(m.namespace)+"/pods/"+url.PathEscape(podName(id)))
}
func (m *K8sManager) deleteService(ctx context.Context, id string) error {
	return m.delete(ctx, "/api/v1/namespaces/"+url.PathEscape(m.namespace)+"/services/"+url.PathEscape(serviceName(id)))
}
func (m *K8sManager) deleteConfigMap(ctx context.Context, id string) error {
	return m.delete(ctx, "/api/v1/namespaces/"+url.PathEscape(m.namespace)+"/configmaps/"+url.PathEscape(configMapName(id)))
}
func (m *K8sManager) deleteNetworkPolicy(ctx context.Context, id string) error {
	return m.delete(ctx, "/apis/networking.k8s.io/v1/namespaces/"+url.PathEscape(m.namespace)+"/networkpolicies/"+url.PathEscape(networkPolicyName(id)))
}
func (m *K8sManager) deletePVC(ctx context.Context, name string) error {
	return m.delete(ctx, "/api/v1/namespaces/"+url.PathEscape(m.namespace)+"/persistentvolumeclaims/"+url.PathEscape(name))
}
func (m *K8sManager) delete(ctx context.Context, p string) error {
	_, err := m.do(ctx, http.MethodDelete, p, []byte(`{"propagationPolicy":"Background"}`), "application/json")
	return err
}
func (m *K8sManager) getJSON(ctx context.Context, p string, out interface{}) error {
	b, err := m.do(ctx, http.MethodGet, p, nil, "")
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
func (m *K8sManager) putJSON(ctx context.Context, p string, obj interface{}) error {
	b, _ := json.Marshal(obj)
	_, err := m.do(ctx, http.MethodPut, p, b, "application/json")
	return err
}
func (m *K8sManager) createIgnoreExists(ctx context.Context, p string, obj interface{}) error {
	b, _ := json.Marshal(obj)
	_, err := m.do(ctx, http.MethodPost, p, b, "application/json")
	if isAlreadyExists(err) {
		return nil
	}
	return err
}

func (m *K8sManager) do(ctx context.Context, method, p string, body []byte, contentType string) ([]byte, error) {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+p, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if m.token != "" {
		req.Header.Set("Authorization", "Bearer "+m.token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return b, nil
	}
	return nil, apiError{Status: resp.StatusCode, Body: string(b)}
}

func sandboxLabels(req model.SandboxEnsureRequest) map[string]string {
	return map[string]string{
		labelManagedBy: "aisphere",
		labelSandboxID: labelValue(req.SandboxID),
		labelOrgID:     labelValue(req.OrgID),
		labelProjectID: labelValue(req.ProjectID),
		labelSessionID: labelValue(req.SessionID),
		labelAgentID:   labelValue(req.AgentID),
	}
}

func sandboxAnnotations(req model.SandboxEnsureRequest) map[string]string {
	ann := map[string]string{
		"aisphere.io/sandbox-id":       req.SandboxID,
		"aisphere.io/runtime-id":       req.RuntimeID,
		"aisphere.io/session-id":       req.SessionID,
		"aisphere.io/run-id":           req.RunID,
		"aisphere.io/owner-subject":    req.OwnerSubject,
		"aisphere.io/org-id":           req.OrgID,
		"aisphere.io/project-id":       req.ProjectID,
		"aisphere.io/agent-id":         req.AgentID,
		"aisphere.io/agent-version":    req.AgentVersion,
		"aisphere.io/snapshot-id":      req.SnapshotID,
		"aisphere.io/workspace-pvc":    firstNonEmpty(req.WorkspacePVC, workspacePVCName(req.SandboxID)),
		"aisphere.io/image":            req.Image,
		"aisphere.io/delete-workspace": fmt.Sprintf("%v", req.DeleteWorkspace),
		"aisphere.io/network-mode":     normalizeNetworkMode(req.Network.Mode),
	}
	if req.Metadata != nil {
		if b, err := json.Marshal(req.Metadata); err == nil && len(b) < 65535 {
			ann["aisphere.io/metadata"] = string(b)
		}
	}
	return ann
}

func normalizeSandboxID(req model.SandboxEnsureRequest) string {
	if req.SandboxID != "" {
		return cleanDNSName(req.SandboxID)
	}
	parts := []string{"sb", req.OrgID, req.ProjectID, req.SessionID}
	if req.SessionID == "" {
		parts = append(parts, req.RuntimeID, time.Now().UTC().Format("20060102150405"))
	}
	return cleanDNSName(strings.Join(parts, "-"))
}

func podName(id string) string           { return cleanDNSName("aisb-" + id) }
func serviceName(id string) string       { return cleanDNSName("aisb-" + id) }
func configMapName(id string) string     { return cleanDNSName("aisb-" + id) }
func networkPolicyName(id string) string { return cleanDNSName("aisb-" + id) }
func workspacePVCName(id string) string  { return cleanDNSName("aisb-ws-" + id) }

var dnsCleanRE = regexp.MustCompile(`[^a-z0-9-]+`)

func normalizeNetworkMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case model.SandboxNetworkModeOnline:
		return model.SandboxNetworkModeOnline
	case model.SandboxNetworkModeRestricted:
		return model.SandboxNetworkModeRestricted
	default:
		return model.SandboxNetworkModeOffline
	}
}

func cleanDNSName(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, "_", "-")
	v = dnsCleanRE.ReplaceAllString(v, "-")
	v = strings.Trim(v, "-")
	if v == "" {
		v = "sandbox"
	}
	if len(v) <= 50 {
		return v
	}
	h := sha1.Sum([]byte(v))
	return strings.Trim(v[:40], "-") + "-" + hex.EncodeToString(h[:])[:8]
}

func labelValue(v string) string {
	if v == "" {
		return "none"
	}
	return cleanDNSName(v)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func copyMap(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

type apiError struct {
	Status int
	Body   string
}

func (e apiError) Error() string {
	return fmt.Sprintf("kubernetes api status=%d body=%s", e.Status, e.Body)
}
func isAlreadyExists(err error) bool {
	var e apiError
	return errors.As(err, &e) && e.Status == http.StatusConflict
}
func isNotFound(err error) bool {
	var e apiError
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

type k8sList struct {
	Items []k8sObject `json:"items"`
}
type k8sObject struct {
	Metadata struct {
		Name              string            `json:"name"`
		Labels            map[string]string `json:"labels"`
		Annotations       map[string]string `json:"annotations"`
		CreationTimestamp string            `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		NodeName string `json:"nodeName"`
	} `json:"spec"`
	Data   map[string]string `json:"data"`
	Status struct {
		Phase   string `json:"phase"`
		Reason  string `json:"reason"`
		Message string `json:"message"`
		PodIP   string `json:"podIP"`
	} `json:"status"`
}

func resolveKubernetesAuth(cfg Config) (server string, token string, caPEM []byte, insecure bool, err error) {
	if cfg.APIServer != "" {
		server = cfg.APIServer
	}
	if cfg.Token != "" {
		token = cfg.Token
	}
	if cfg.TokenFile != "" {
		if b, e := os.ReadFile(cfg.TokenFile); e == nil {
			token = strings.TrimSpace(string(b))
		}
	}
	if cfg.CAFile != "" {
		caPEM, _ = os.ReadFile(cfg.CAFile)
	}
	insecure = cfg.Insecure
	if cfg.Kubeconfig != "" && server == "" {
		return "", "", nil, false, errors.New("sandbox lightweight kubernetes client does not parse kubeconfig yet; set apiServer/token/caFile or run in-cluster")
	}
	if server == "" {
		host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
		if host != "" && port != "" {
			server = "https://" + host + ":" + port
			if token == "" {
				if b, e := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token"); e == nil {
					token = strings.TrimSpace(string(b))
				}
			}
			if len(caPEM) == 0 {
				caPEM, _ = os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
			}
		}
	}
	return server, token, caPEM, insecure, nil
}
