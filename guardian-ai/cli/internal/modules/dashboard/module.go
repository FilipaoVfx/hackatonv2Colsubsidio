package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"guardianai/cli/internal/api"
	"guardianai/cli/internal/theme"
	"guardianai/cli/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Module struct {
	ui.Base
	src *api.LiveSource

	caps       api.Capabilities
	capsErr    error
	kpis       api.KPIs
	kpisErr    error
	health     api.Health
	latency    time.Duration
	healthErr  error
	eventsSeen int
}

func New(src *api.LiveSource) *Module {
	return &Module{Base: ui.NewBase(ui.ModDashboard, "Dashboard"), src: src}
}

func (m *Module) Init() tea.Cmd {
	return m.refresh()
}

func (m *Module) refresh() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h, lat, herr := src.Health(ctx)
		caps, cerr := src.Capabilities(ctx)
		kpis, kerr := src.KPIs(ctx)
		return dashRefreshMsg{h, lat, herr, caps, cerr, kpis, kerr}
	}
}

type dashRefreshMsg struct {
	health    api.Health
	latency   time.Duration
	healthErr error
	caps      api.Capabilities
	capsErr   error
	kpis      api.KPIs
	kpisErr   error
}

func (m *Module) Update(msg tea.Msg) (ui.Module, tea.Cmd) {
	switch msg := msg.(type) {
	case dashRefreshMsg:
		m.health, m.latency, m.healthErr = msg.health, msg.latency, msg.healthErr
		m.caps, m.capsErr = msg.caps, msg.capsErr
		m.kpis, m.kpisErr = msg.kpis, msg.kpisErr
		return m, nil
	case ui.TickMsg:
		return m, m.refresh()
	case ui.EventMsg:
		m.eventsSeen++
		return m, nil
	}
	return m, nil
}

var (
	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(theme.Line).
			Background(theme.Cloud).Padding(0, 2).Margin(0, 1, 1, 0)
	kpiValue = lipgloss.NewStyle().Foreground(theme.Ink).Bold(true)
	kpiLabel = lipgloss.NewStyle().Foreground(theme.Humo)
	okMark   = lipgloss.NewStyle().Foreground(theme.Live).Bold(true)
	badMark  = lipgloss.NewStyle().Foreground(theme.Danger).Bold(true)
	badge    = lipgloss.NewStyle().Foreground(theme.Live)
)

func kpi(label, value string) string {
	return cardStyle.Render(kpiLabel.Render(label) + "\n" + kpiValue.Render(value) + " " + badge.Render("◆"))
}

func (m *Module) View() string {
	var b strings.Builder

	if m.healthErr != nil {
		b.WriteString(badMark.Render("✗ API health: "+m.healthErr.Error()) + "\n\n")
	} else {
		b.WriteString(okMark.Render(fmt.Sprintf("✓ %s (%s) — %dms", m.health.Status, m.health.Service, m.latency.Milliseconds())) + "\n\n")
	}

	if m.capsErr == nil {
		row := func(name string, ok bool) string {
			if ok {
				return okMark.Render("✓") + " " + name
			}
			return badMark.Render("✗") + " " + name
		}
		caps := []string{
			row("LLM", m.caps.LLM), row("WhatsApp", m.caps.WhatsApp), row("Vapi", m.caps.Vapi),
			row("ElevenLabs", m.caps.ElevenLabs), row("Colsubsidio", m.caps.Colsubsidio), row("Guardian", m.caps.Guardian),
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(20).Render(caps[0]),
			lipgloss.NewStyle().Width(20).Render(caps[1]),
			lipgloss.NewStyle().Width(20).Render(caps[2]),
		) + "\n")
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(20).Render(caps[3]),
			lipgloss.NewStyle().Width(20).Render(caps[4]),
			lipgloss.NewStyle().Width(20).Render(caps[5]),
		) + "\n\n")
	}

	if m.kpisErr != nil {
		b.WriteString(badMark.Render("KPIs: "+m.kpisErr.Error()) + "\n")
	} else {
		row1 := lipgloss.JoinHorizontal(lipgloss.Top,
			kpi("Leads Ready", fmt.Sprintf("%d", m.kpis.LeadsReady)),
			kpi("Leads WhatsApp", fmt.Sprintf("%d", m.kpis.LeadsWhatsApp)),
			kpi("Tool Calls", fmt.Sprintf("%d", m.kpis.ToolCalls)),
		)
		row2 := lipgloss.JoinHorizontal(lipgloss.Top,
			kpi("Latencia LLM", fmt.Sprintf("%.0f ms", m.kpis.AvgLLMLatencyMS)),
			kpi("Tokens in/out", fmt.Sprintf("%d / %d", m.kpis.TokensIn, m.kpis.TokensOut)),
			kpi("Costo USD", fmt.Sprintf("$%.2f", m.kpis.CostUSD)),
		)
		b.WriteString(row1 + "\n" + row2 + "\n\n")
	}

	b.WriteString(kpiLabel.Render(fmt.Sprintf("Eventos vistos en esta sesión: %d", m.eventsSeen)))

	return b.String()
}
