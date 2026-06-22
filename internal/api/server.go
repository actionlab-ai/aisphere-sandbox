package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/actionlab-ai/aisphere-sandbox/internal/auth"
	"github.com/actionlab-ai/aisphere-sandbox/internal/config"
	"github.com/actionlab-ai/aisphere-sandbox/internal/model"
	"github.com/actionlab-ai/aisphere-sandbox/internal/sandbox"
	"github.com/actionlab-ai/aisphere-sandbox/internal/store"
	"github.com/actionlab-ai/aisphere-sandbox/internal/toolgateway"
)

type Server struct {
	cfg     config.Config
	auth    *auth.Client
	mgr     sandbox.Manager
	gateway *toolgateway.HTTPGateway
	store   *store.PostgresStore
}

func New(cfg config.Config, mgr sandbox.Manager) *Server {
	return NewWithStore(cfg, mgr, nil)
}

func NewWithStore(cfg config.Config, mgr sandbox.Manager, st *store.PostgresStore) *Server {
	return &Server{cfg: cfg, auth: auth.NewClient(cfg.Auth), mgr: mgr, gateway: toolgateway.NewHTTPGateway(), store: st}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.health)
	mux.HandleFunc("/readyz", s.health)
	mux.HandleFunc("/v1/sandboxes/ensure", s.withAuth("aihub:sandbox:*", "run", s.ensureSandbox))
	mux.HandleFunc("/v1/sandboxes", s.withAuth("aihub:sandbox:*", "read", s.sandboxesRoot))
	mux.HandleFunc("/v1/sandboxes/", s.sandboxByID)
	return requestID(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "service": "aisphere-sandbox"})
}

func (s *Server) sandboxesRoot(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	q := sandbox.ListQuery{OwnerSubject: r.URL.Query().Get("ownerSubject"), OrgID: r.URL.Query().Get("orgId"), ProjectID: r.URL.Query().Get("projectId"), SessionID: r.URL.Query().Get("sessionId"), AgentID: r.URL.Query().Get("agentId")}
	items, err := s.mgr.List(r.Context(), q)
	if err != nil && s.store != nil {
		items, err = s.store.ListSandboxes(r.Context(), store.ListQuery(q))
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sandbox_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items, "total": len(items)})
}

func (s *Server) ensureSandbox(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req model.SandboxEnsureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.OwnerSubject == "" {
		req.OwnerSubject = p.EffectiveSubject()
	}
	if req.Reuse && s.store != nil && strings.TrimSpace(req.SessionID) != "" {
		if cached, err := s.store.GetBySession(r.Context(), req.SessionID); err == nil && cached != nil && endpointURL(cached, "worker") != "" {
			attachLease(cached, s.cfg.Sandbox.LeaseTTLSeconds)
			writeJSON(w, http.StatusOK, cached)
			return
		}
	}
	st, err := s.mgr.Ensure(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "sandbox_ensure_failed", err.Error())
		return
	}
	fillRequestContext(st, req)
	attachLease(st, s.cfg.Sandbox.LeaseTTLSeconds)
	if s.store != nil {
		_ = s.store.SaveSandboxStatus(r.Context(), st)
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) sandboxByID(w http.ResponseWriter, r *http.Request) {
	p, err := s.auth.Authenticate(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/sandboxes/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not_found", "sandbox id is required")
		return
	}
	id := parts[0]
	object := "aihub:sandbox:" + id
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			if err := s.auth.Check(r.Context(), p, object, "read"); err != nil {
				writeError(w, http.StatusForbidden, "forbidden", err.Error())
				return
			}
			st, err := s.mgr.Get(r.Context(), id)
			if err != nil && s.store != nil {
				st, err = s.store.GetSandbox(r.Context(), id)
			}
			if err != nil || st == nil {
				msg := "not found"
				if err != nil {
					msg = err.Error()
				}
				writeError(w, http.StatusNotFound, "sandbox_not_found", msg)
				return
			}
			writeJSON(w, http.StatusOK, st)
		case http.MethodDelete:
			if err := s.auth.Check(r.Context(), p, object, "delete"); err != nil {
				writeError(w, http.StatusForbidden, "forbidden", err.Error())
				return
			}
			deleteWorkspace := parseBool(r.URL.Query().Get("deleteWorkspace"))
			if err := s.mgr.Delete(r.Context(), id, deleteWorkspace); err != nil {
				writeError(w, http.StatusInternalServerError, "sandbox_delete_failed", err.Error())
				return
			}
			if s.store != nil {
				_ = s.store.DeleteSandbox(r.Context(), id)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"deleted": true, "sandboxId": id, "deleteWorkspace": deleteWorkspace})
		default:
			methodNotAllowed(w)
		}
		return
	}
	switch parts[1] {
	case "restart":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if err := s.auth.Check(r.Context(), p, object, "run"); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		st, err := s.mgr.Restart(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "sandbox_restart_failed", err.Error())
			return
		}
		attachLease(st, s.cfg.Sandbox.LeaseTTLSeconds)
		if s.store != nil {
			_ = s.store.SaveSandboxStatus(r.Context(), st)
		}
		writeJSON(w, http.StatusOK, st)
	case "logs":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if err := s.auth.Check(r.Context(), p, object, "read"); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		tail, _ := strconv.ParseInt(firstNonEmpty(r.URL.Query().Get("tail"), "200"), 10, 64)
		logs, err := s.mgr.Logs(r.Context(), id, model.SandboxLogQuery{Container: r.URL.Query().Get("container"), TailLines: tail})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "sandbox_logs_failed", err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(logs))
	case "tools":
		if err := s.auth.Check(r.Context(), p, object, "run"); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		if len(parts) == 2 && r.Method == http.MethodGet {
			s.listTools(w, r, id)
			return
		}
		if len(parts) == 3 && parts[2] == "call" && r.Method == http.MethodPost {
			s.callTool(w, r, id)
			return
		}
		methodNotAllowed(w)
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown sandbox subresource")
	}
}

func (s *Server) listTools(w http.ResponseWriter, r *http.Request, id string) {
	st, err := s.mgr.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "sandbox_not_found", err.Error())
		return
	}
	endpoint := endpointURL(st, "tools")
	out, err := s.gateway.ListTools(r.Context(), endpoint)
	if err != nil {
		writeError(w, http.StatusBadGateway, "toolserver_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) callTool(w http.ResponseWriter, r *http.Request, id string) {
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	st, err := s.mgr.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "sandbox_not_found", err.Error())
		return
	}
	endpoint := endpointURL(st, "tools")
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	out, err := s.gateway.Call(ctx, endpoint, body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "tool_call_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) withAuth(object, action string, next func(http.ResponseWriter, *http.Request, *auth.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := s.auth.Authenticate(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
			return
		}
		if err := s.auth.Check(r.Context(), p, object, action); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		next(w, r, p)
	}
}

func fillRequestContext(st *model.SandboxStatus, req model.SandboxEnsureRequest) {
	if st == nil {
		return
	}
	if st.OwnerSubject == "" {
		st.OwnerSubject = req.OwnerSubject
	}
	if st.OrgID == "" {
		st.OrgID = req.OrgID
	}
	if st.ProjectID == "" {
		st.ProjectID = req.ProjectID
	}
	if st.SessionID == "" {
		st.SessionID = req.SessionID
	}
	if st.RunID == "" {
		st.RunID = req.RunID
	}
	if st.AgentID == "" {
		st.AgentID = req.AgentID
	}
	if st.SnapshotID == "" {
		st.SnapshotID = req.SnapshotID
	}
}

func endpointURL(st *model.SandboxStatus, name string) string {
	for _, ep := range st.Endpoints {
		if ep.Name == name {
			return ep.URL
		}
	}
	return ""
}

func attachLease(st *model.SandboxStatus, ttl int) {
	if ttl <= 0 {
		ttl = 900
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	st.Lease = &model.SandboxLease{Token: hex.EncodeToString(b), ExpiresAt: time.Now().UTC().Add(time.Duration(ttl) * time.Second).Format(time.RFC3339)}
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-Id")
		if rid == "" {
			rid = fmt.Sprintf("req_%d", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-Id", rid)
		slog.Debug("request", "id", rid, "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]interface{}{"error": map[string]string{"code": code, "message": msg}})
}
func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}
func parseBool(v string) bool { b, _ := strconv.ParseBool(strings.TrimSpace(v)); return b }
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
