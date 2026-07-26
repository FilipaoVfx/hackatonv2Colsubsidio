package api

import (
	"context"
	"errors"
	"time"
)

// Source is the single interface every module and subcommand depends on.
// Live wraps Client+EventStream directly; Fixture/Fallback (added in a later
// phase) satisfy the same shape so --demo and --replay work identically.
type Source interface {
	Mode() SourceMode
	Health(ctx context.Context) (Health, time.Duration, error)
	Capabilities(ctx context.Context) (Capabilities, error)
	KPIs(ctx context.Context) (KPIs, error)
	ListCalls(ctx context.Context) ([]string, error)
	CallDetail(ctx context.Context, id string) (CallDetail, error)
	CallEvents(ctx context.Context, id string) ([]Event, error)
	SimulateWhatsApp(ctx context.Context, from, text string) error
	SimulateCall(ctx context.Context) error
	StudioPrompt(ctx context.Context) (map[string]any, error)
	StudioVersions(ctx context.Context) ([]map[string]any, error)
	StudioConfig(ctx context.Context) (map[string]any, error)
	StudioSaveDraft(ctx context.Context, draft map[string]any) (map[string]any, error)
	StudioPublish(ctx context.Context, note string) (map[string]any, error)
	StudioRollback(ctx context.Context, version int, note string) (map[string]any, error)
	PlaygroundInfo(ctx context.Context) (map[string]any, error)
	PlaygroundStart(ctx context.Context) (map[string]any, error)
	PlaygroundMessage(ctx context.Context, sessionID, text string) (map[string]any, error)
	PlaygroundReset(ctx context.Context, sessionID string) error
	Stream() *EventStream
}

// ErrReadOnly is returned by every mutating method when the source runs in
// read-only mode. The guard lives here, at the single point every write funnels
// through, rather than in the modules: the public web-terminal session shares
// one binary with the operator's, and a UI-level check would be bypassable via
// the command palette or a future non-interactive subcommand.
var ErrReadOnly = errors.New("sesión de solo lectura: escritura bloqueada")

// LiveSource is the straightforward Source backed by the real HTTP+WS client.
type LiveSource struct {
	client   *Client
	stream   *EventStream
	readOnly bool
}

func NewLiveSource(baseURL string, timeout time.Duration) *LiveSource {
	return &LiveSource{
		client: NewClient(baseURL, timeout),
		stream: NewEventStream(baseURL),
	}
}

// SetReadOnly blocks every mutating call on this source.
func (l *LiveSource) SetReadOnly(v bool) { l.readOnly = v }

func (l *LiveSource) ReadOnly() bool { return l.readOnly }

func (l *LiveSource) Mode() SourceMode { return ModeLive }

func (l *LiveSource) Health(ctx context.Context) (Health, time.Duration, error) {
	return l.client.Health(ctx)
}
func (l *LiveSource) Capabilities(ctx context.Context) (Capabilities, error) {
	return l.client.Capabilities(ctx)
}
func (l *LiveSource) KPIs(ctx context.Context) (KPIs, error) { return l.client.KPIs(ctx) }
func (l *LiveSource) ListCalls(ctx context.Context) ([]string, error) {
	return l.client.ListCalls(ctx)
}
func (l *LiveSource) CallDetail(ctx context.Context, id string) (CallDetail, error) {
	return l.client.CallDetail(ctx, id)
}
func (l *LiveSource) CallEvents(ctx context.Context, id string) ([]Event, error) {
	return l.client.CallEvents(ctx, id)
}
func (l *LiveSource) SimulateWhatsApp(ctx context.Context, from, text string) error {
	return l.client.SimulateWhatsApp(ctx, from, text)
}
func (l *LiveSource) SimulateCall(ctx context.Context) error {
	return l.client.SimulateCall(ctx)
}
func (l *LiveSource) StudioPrompt(ctx context.Context) (map[string]any, error) {
	return l.client.StudioPrompt(ctx)
}
func (l *LiveSource) StudioVersions(ctx context.Context) ([]map[string]any, error) {
	return l.client.StudioVersions(ctx)
}
func (l *LiveSource) StudioConfig(ctx context.Context) (map[string]any, error) {
	return l.client.StudioConfig(ctx)
}
func (l *LiveSource) StudioSaveDraft(ctx context.Context, draft map[string]any) (map[string]any, error) {
	if l.readOnly {
		return nil, ErrReadOnly
	}
	return l.client.StudioSaveDraft(ctx, draft)
}
func (l *LiveSource) StudioPublish(ctx context.Context, note string) (map[string]any, error) {
	if l.readOnly {
		return nil, ErrReadOnly
	}
	return l.client.StudioPublish(ctx, note)
}
func (l *LiveSource) StudioRollback(ctx context.Context, version int, note string) (map[string]any, error) {
	if l.readOnly {
		return nil, ErrReadOnly
	}
	return l.client.StudioRollback(ctx, version, note)
}
func (l *LiveSource) PlaygroundInfo(ctx context.Context) (map[string]any, error) {
	return l.client.PlaygroundInfo(ctx)
}
func (l *LiveSource) PlaygroundStart(ctx context.Context) (map[string]any, error) {
	return l.client.PlaygroundStart(ctx)
}
func (l *LiveSource) PlaygroundMessage(ctx context.Context, sessionID, text string) (map[string]any, error) {
	return l.client.PlaygroundMessage(ctx, sessionID, text)
}
func (l *LiveSource) PlaygroundReset(ctx context.Context, sessionID string) error {
	return l.client.PlaygroundReset(ctx, sessionID)
}

func (l *LiveSource) Stream() *EventStream { return l.stream }

func (l *LiveSource) StartStream() { l.stream.Start() }
