package toolgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type HTTPGateway struct{ client *http.Client }

func NewHTTPGateway() *HTTPGateway {
	return &HTTPGateway{client: &http.Client{Timeout: 60 * time.Second}}
}

func (g *HTTPGateway) ListTools(ctx context.Context, endpoint string) (map[string]interface{}, error) {
	endpoint = strings.TrimRight(endpoint, "/") + "/v1/tools"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tool-server list tools http %d: %s", resp.StatusCode, string(b))
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (g *HTTPGateway) Call(ctx context.Context, endpoint string, reqBody map[string]interface{}) (map[string]interface{}, error) {
	endpoint = strings.TrimRight(endpoint, "/") + "/v1/tools/call"
	b, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tool-server call http %d: %s", resp.StatusCode, string(rb))
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rb, &out); err != nil {
		return nil, err
	}
	return out, nil
}
