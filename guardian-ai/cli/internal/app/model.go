package app

import (
	"context"
	"time"

	"guardianai/cli/internal/api"
	"guardianai/cli/internal/config"
	"guardianai/cli/internal/modules/analytics"
	"guardianai/cli/internal/modules/calls"
	"guardianai/cli/internal/modules/dashboard"
	"guardianai/cli/internal/modules/knowledge"
	"guardianai/cli/internal/modules/pipeline"
	"guardianai/cli/internal/modules/playground"
	"guardianai/cli/internal/modules/prompt"
	"guardianai/cli/internal/modules/settings"
	"guardianai/cli/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

type Focus int

const (
	FocusModule Focus = iota
	FocusHelp
	FocusSplash
	FocusPalette
)

const chromeHeight = 4 // header(1) + tabs(1) + statusbar(1) + margin(1)

type Model struct {
	cfg       config.Config
	src       *api.LiveSource
	modules   []ui.Module
	active    int
	focus     Focus
	w, h      int
	live      bool
	mode      api.SourceMode
	quitting  bool
	splash    ui.Splash
	palette   ui.Palette
	prevFocus Focus
}

func New(cfg config.Config) Model {
	src := api.NewLiveSource(cfg.APIURL, cfg.Timeout)
	src.SetReadOnly(cfg.ReadOnly)
	return Model{
		cfg: cfg,
		src: src,
		modules: []ui.Module{
			dashboard.New(src),
			calls.New(src),
			pipeline.New(src),
			playground.New(src),
			prompt.New(src),
			knowledge.New(src),
			analytics.New(src),
			settings.New(src),
		},
		mode:    api.ModeLive,
		focus:   FocusSplash,
		splash:  ui.NewSplash(),
		palette: ui.NewPalette(),
	}
}

func (m Model) Init() tea.Cmd {
	m.src.StartStream()
	cmds := []tea.Cmd{waitForEvent(m.src), healthTick(), rootHealthCheck(m.src), ui.LoadSplashData(m.src)}
	for _, mod := range m.modules {
		cmds = append(cmds, mod.Init())
	}
	return tea.Batch(cmds...)
}

func healthTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return ui.TickMsg(t) })
}

// rootHealthCheck drives the header's live/offline dot independent of any
// module — the chrome must know connectivity even if the active tab never
// calls /api/health itself.
func rootHealthCheck(src *api.LiveSource) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		_, lat, err := src.Health(ctx)
		return ui.HealthMsg{OK: err == nil, Latency: lat, Err: err}
	}
}

func waitForEvent(src *api.LiveSource) tea.Cmd {
	return func() tea.Msg {
		ev := <-src.Stream().Out()
		return ui.EventMsg{Event: ev}
	}
}
