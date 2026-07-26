package ui

import (
	"time"

	"guardianai/cli/internal/api"
)

// Shared message types, imported by app/ and every module. Imports nothing
// but stdlib+api to avoid import cycles.

type HealthMsg struct {
	OK      bool
	Latency time.Duration
	Err     error
}

type CapabilitiesMsg struct {
	Caps api.Capabilities
	Err  error
}

type KPIsMsg struct {
	KPIs api.KPIs
	Err  error
}

type ModeChangedMsg struct {
	Mode api.SourceMode
}

type StreamStateMsg struct {
	State api.StreamState
}

// EventMsg carries one backend event, broadcast to every module.
type EventMsg struct {
	Event api.Event
}

type ErrMsg struct {
	Module ModuleID
	Err    error
}

type ToastLevel int

const (
	ToastInfo ToastLevel = iota
	ToastWarn
	ToastError
)

type ToastMsg struct {
	Text  string
	Level ToastLevel
	TTL   time.Duration
}

type TickMsg time.Time

// NavigateMsg lets the command palette deep-link into a module (e.g. jump
// straight to a specific call's detail view).
type NavigateMsg struct {
	To      ModuleID
	Payload any
}
