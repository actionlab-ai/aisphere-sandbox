package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	platformauth "github.com/actionlab-ai/aisphere-go/auth"
	"github.com/actionlab-ai/aisphere-sandbox/internal/config"
)

type Principal struct {
	SubjectID string   `json:"subjectId,omitempty"`
	Subject   string   `json:"subject,omitempty"`
	Username  string   `json:"username,omitempty"`
	Name      string   `json:"name,omitempty"`
	Email     string   `json:"email,omitempty"`
	App       string   `json:"app,omitempty"`
	OrgID     string   `json:"orgId,omitempty"`
	ProjectID string   `json:"projectId,omitempty"`
	Groups    []string `json:"groups,omitempty"`
	Roles     []string `json:"roles,omitempty"`
}

func (p Principal) EffectiveSubject() string {
	for _, v := range []string{p.SubjectID, p.Subject, p.Username, p.Email, p.Name} {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return "anonymous"
}

type Client struct {
	cfg        config.AuthConfig
	authClient *platformauth.Client
}

func NewClient(cfg config.AuthConfig) *Client {
	return &Client{
		cfg: cfg,
		authClient: platformauth.NewClient(platformauth.Config{
			Endpoint:               cfg.AuthEndpoint,
			ServiceToken:           cfg.ServiceToken,
			ServiceTokenHeader:     "Authorization",
			ServiceTokenPrefix:     "Bearer ",
			SessionIntrospectPath:  cfg.SessionIntrospectPath,
			ResourceGrantCheckPath: cfg.IAMCheckPath,
		}),
	}
}

func (c *Client) Authenticate(r *http.Request) (*Principal, error) {
	mode := strings.ToLower(strings.TrimSpace(c.cfg.Mode))
	if mode == "" {
		mode = "none"
	}
	if !c.cfg.Enabled || mode == "none" {
		return trustedPrincipal(r, "anonymous"), nil
	}
	if mode == "trusted" {
		return trustedPrincipal(r, "anonymous"), nil
	}
	if token := bearer(r.Header.Get("Authorization")); token != "" && c.cfg.ServiceToken != "" && token == c.cfg.ServiceToken {
		return trustedPrincipal(r, "service:sandbox-manager"), nil
	}
	if mode == "aisphere" {
		return c.introspect(r.Context(), sessionToken(r, c.cfg.CookieName))
	}
	return nil, fmt.Errorf("unsupported auth mode %q", mode)
}

func (c *Client) Check(ctx context.Context, p *Principal, object, action string) error {
	if !c.cfg.Enabled || strings.EqualFold(c.cfg.Mode, "none") || strings.EqualFold(c.cfg.Mode, "trusted") {
		return nil
	}
	if strings.TrimSpace(c.cfg.AuthEndpoint) == "" || strings.TrimSpace(c.cfg.IAMCheckPath) == "" {
		if c.cfg.FailClosed {
			return errors.New("auth endpoint or iam check path is not configured")
		}
		return nil
	}
	decision, err := c.authClient.CheckResourceGrant(ctx, platformauth.ResourceGrantCheckRequest{
		App:       c.firstNonEmpty(c.cfg.App, "aihub"),
		Object:    object,
		Action:    action,
		Subject:   p.EffectiveSubject(),
		OrgID:     p.OrgID,
		ProjectID: p.ProjectID,
		Principal: toPlatformPrincipal(p),
	})
	if err != nil {
		if c.cfg.FailClosed {
			return err
		}
		return nil
	}
	if !decision.Allow {
		return fmt.Errorf("forbidden: %s", decision.Reason)
	}
	return nil
}

func (c *Client) introspect(ctx context.Context, token string) (*Principal, error) {
	if token == "" {
		return nil, errors.New("missing session token")
	}
	if strings.TrimSpace(c.cfg.AuthEndpoint) == "" {
		return nil, errors.New("auth endpoint is not configured")
	}
	principal, err := c.authClient.Introspect(ctx, token, c.firstNonEmpty(c.cfg.App, "aihub"))
	if err != nil {
		return nil, err
	}
	return fromPlatformPrincipal(principal), nil
}

func toPlatformPrincipal(p *Principal) *platformauth.Principal {
	if p == nil {
		return nil
	}
	return &platformauth.Principal{
		SubjectID:   p.SubjectID,
		Username:    p.Username,
		DisplayName: p.Name,
		Email:       p.Email,
		App:         p.App,
		OrgID:       p.OrgID,
		ProjectID:   p.ProjectID,
		Groups:      p.Groups,
		Roles:       p.Roles,
	}
}

func fromPlatformPrincipal(p *platformauth.Principal) *Principal {
	if p == nil {
		return nil
	}
	projectID := p.ProjectID
	if projectID == "" && len(p.ProjectIDs) > 0 {
		projectID = p.ProjectIDs[0]
	}
	return &Principal{
		SubjectID: p.SubjectID,
		Subject:   p.CasdoorSubject,
		Username:  p.Username,
		Name:      p.DisplayName,
		Email:     p.Email,
		App:       p.App,
		OrgID:     p.OrgID,
		ProjectID: projectID,
		Groups:    p.Groups,
		Roles:     p.Roles,
	}
}

func trustedPrincipal(r *http.Request, fallback string) *Principal {
	p := &Principal{SubjectID: firstHeader(r, "X-Aisphere-Subject", "X-User", "X-User-Id")}
	if p.SubjectID == "" {
		p.SubjectID = fallback
	}
	p.Username = r.Header.Get("X-Aisphere-Username")
	p.OrgID = r.Header.Get("X-Aisphere-Org-Id")
	p.ProjectID = r.Header.Get("X-Aisphere-Project-Id")
	p.Groups = splitCSV(r.Header.Get("X-Aisphere-Groups"))
	p.Roles = splitCSV(r.Header.Get("X-Aisphere-Roles"))
	return p
}

func sessionToken(r *http.Request, cookieName string) string {
	if v := bearer(r.Header.Get("Authorization")); v != "" {
		return v
	}
	if cookieName == "" {
		cookieName = "aisphere_session"
	}
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

func bearer(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return strings.TrimSpace(v[7:])
	}
	return ""
}
func firstHeader(r *http.Request, names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(r.Header.Get(n)); v != "" {
			return v
		}
	}
	return ""
}
func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := []string{}
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
func (c *Client) firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
