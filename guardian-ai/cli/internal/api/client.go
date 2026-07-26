package api

import (
	"context"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
)

// Client talks to the guardian-ai Fiber backend over plain HTTP. No auth
// header is sent — the backend has none (CORS wide open, verified live).
type Client struct {
	http    *resty.Client
	baseURL string
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	r := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(timeout).
		EnableTrace()
	return &Client{http: r, baseURL: baseURL}
}

func (c *Client) BaseURL() string { return c.baseURL }

func (c *Client) Health(ctx context.Context) (Health, time.Duration, error) {
	var h Health
	resp, err := c.http.R().SetContext(ctx).SetResult(&h).Get("/api/health")
	if err != nil {
		return h, 0, fmt.Errorf("health: %w", err)
	}
	if resp.IsError() {
		return h, resp.Request.TraceInfo().TotalTime, parseError(resp)
	}
	return h, resp.Request.TraceInfo().TotalTime, nil
}

func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	var caps Capabilities
	resp, err := c.http.R().SetContext(ctx).SetResult(&caps).Get("/api/capabilities")
	if err != nil {
		return caps, fmt.Errorf("capabilities: %w", err)
	}
	if resp.IsError() {
		return caps, parseError(resp)
	}
	return caps, nil
}

func (c *Client) KPIs(ctx context.Context) (KPIs, error) {
	var k KPIs
	resp, err := c.http.R().SetContext(ctx).SetResult(&k).Get("/api/analytics/kpis")
	if err != nil {
		return k, fmt.Errorf("kpis: %w", err)
	}
	if resp.IsError() {
		return k, parseError(resp)
	}
	return k, nil
}

func (c *Client) ListCalls(ctx context.Context) ([]string, error) {
	var ids []string
	resp, err := c.http.R().SetContext(ctx).SetResult(&ids).Get("/api/calls")
	if err != nil {
		return nil, fmt.Errorf("list calls: %w", err)
	}
	if resp.IsError() {
		return nil, parseError(resp)
	}
	return ids, nil
}

func (c *Client) CallDetail(ctx context.Context, id string) (CallDetail, error) {
	var d CallDetail
	resp, err := c.http.R().SetContext(ctx).SetResult(&d).Get("/api/analytics/calls/" + id)
	if err != nil {
		return nil, fmt.Errorf("call detail: %w", err)
	}
	if resp.IsError() {
		return nil, parseError(resp)
	}
	return d, nil
}

func (c *Client) CallEvents(ctx context.Context, id string) ([]Event, error) {
	var evs []Event
	resp, err := c.http.R().SetContext(ctx).SetResult(&evs).Get("/api/calls/" + id + "/events")
	if err != nil {
		return nil, fmt.Errorf("call events: %w", err)
	}
	if resp.IsError() {
		return nil, parseError(resp)
	}
	return evs, nil
}

func (c *Client) SimulateWhatsApp(ctx context.Context, from, text string) error {
	resp, err := c.http.R().SetContext(ctx).
		SetBody(SimulateInboundRequest{From: from, Text: text}).
		Post("/api/whatsapp/simulate-inbound")
	if err != nil {
		return fmt.Errorf("simulate whatsapp: %w", err)
	}
	if resp.IsError() {
		return parseError(resp)
	}
	return nil
}

func (c *Client) SimulateCall(ctx context.Context) error {
	resp, err := c.http.R().SetContext(ctx).Post("/api/calls/simulate")
	if err != nil {
		return fmt.Errorf("simulate call: %w", err)
	}
	if resp.IsError() {
		return parseError(resp)
	}
	return nil
}

func (c *Client) StudioPrompt(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	resp, err := c.http.R().SetContext(ctx).SetResult(&out).Get("/api/studio/prompt")
	if err != nil {
		return nil, fmt.Errorf("studio prompt: %w", err)
	}
	if resp.IsError() {
		return nil, parseError(resp)
	}
	return out, nil
}

func (c *Client) StudioVersions(ctx context.Context) ([]map[string]any, error) {
	var env struct {
		Versions []map[string]any `json:"versions"`
	}
	resp, err := c.http.R().SetContext(ctx).SetResult(&env).Get("/api/studio/versions")
	if err != nil {
		return nil, fmt.Errorf("studio versions: %w", err)
	}
	if resp.IsError() {
		return nil, parseError(resp)
	}
	return env.Versions, nil
}

func (c *Client) StudioConfig(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	resp, err := c.http.R().SetContext(ctx).SetResult(&out).Get("/api/studio/config")
	if err != nil {
		return nil, fmt.Errorf("studio config: %w", err)
	}
	if resp.IsError() {
		return nil, parseError(resp)
	}
	return out, nil
}

func (c *Client) StudioSaveDraft(ctx context.Context, draft map[string]any) (map[string]any, error) {
	var out map[string]any
	resp, err := c.http.R().SetContext(ctx).SetBody(draft).SetResult(&out).Put("/api/studio/config/draft")
	if err != nil {
		return nil, fmt.Errorf("studio save draft: %w", err)
	}
	if resp.IsError() {
		return nil, parseError(resp)
	}
	return out, nil
}

func (c *Client) StudioPublish(ctx context.Context, note string) (map[string]any, error) {
	var out map[string]any
	resp, err := c.http.R().SetContext(ctx).
		SetBody(map[string]string{"note": note}).SetResult(&out).
		Post("/api/studio/config/publish")
	if err != nil {
		return nil, fmt.Errorf("studio publish: %w", err)
	}
	if resp.IsError() {
		return nil, parseError(resp)
	}
	return out, nil
}

func (c *Client) StudioRollback(ctx context.Context, version int, note string) (map[string]any, error) {
	var out map[string]any
	resp, err := c.http.R().SetContext(ctx).
		SetBody(map[string]string{"note": note}).SetResult(&out).
		Post(fmt.Sprintf("/api/studio/config/rollback/%d", version))
	if err != nil {
		return nil, fmt.Errorf("studio rollback: %w", err)
	}
	if resp.IsError() {
		return nil, parseError(resp)
	}
	return out, nil
}

func (c *Client) PlaygroundInfo(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	resp, err := c.http.R().SetContext(ctx).SetResult(&out).Get("/api/studio/playground")
	if err != nil {
		return nil, fmt.Errorf("playground info: %w", err)
	}
	if resp.IsError() {
		return nil, parseError(resp)
	}
	return out, nil
}

func (c *Client) PlaygroundStart(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	resp, err := c.http.R().SetContext(ctx).SetResult(&out).Post("/api/studio/playground/start")
	if err != nil {
		return nil, fmt.Errorf("playground start: %w", err)
	}
	if resp.IsError() {
		return nil, parseError(resp)
	}
	return out, nil
}

func (c *Client) PlaygroundMessage(ctx context.Context, sessionID, text string) (map[string]any, error) {
	var out map[string]any
	resp, err := c.http.R().SetContext(ctx).
		SetBody(map[string]string{"session_id": sessionID, "text": text}).
		SetResult(&out).
		Post("/api/studio/playground/message")
	if err != nil {
		return nil, fmt.Errorf("playground message: %w", err)
	}
	if resp.IsError() {
		return nil, parseError(resp)
	}
	return out, nil
}

func (c *Client) PlaygroundReset(ctx context.Context, sessionID string) error {
	resp, err := c.http.R().SetContext(ctx).
		SetBody(map[string]string{"session_id": sessionID}).
		Post("/api/studio/playground/reset")
	if err != nil {
		return fmt.Errorf("playground reset: %w", err)
	}
	if resp.IsError() {
		return parseError(resp)
	}
	return nil
}
