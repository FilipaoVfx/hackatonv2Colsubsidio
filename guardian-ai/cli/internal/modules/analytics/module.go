package analytics

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

var blocks = []rune("█▉▊▋▌▍▎▏")

type Module struct {
	ui.Base
	src *api.LiveSource

	kpis   api.KPIs
	callsN int
	err    error
	hourly [24]int // events seen this session, by local hour — session-scoped, labeled as such
}

func New(src *api.LiveSource) *Module {
	return &Module{Base: ui.NewBase(ui.ModAnalytics, "Analytics"), src: src}
}

type analyticsMsg struct {
	kpis   api.KPIs
	callsN int
	err    error
}

func (m *Module) Init() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		k, kerr := src.KPIs(ctx)
		ids, ierr := src.ListCalls(ctx)
		err := kerr
		if err == nil {
			err = ierr
		}
		return analyticsMsg{k, len(ids), err}
	}
}

func (m *Module) Update(msg tea.Msg) (ui.Module, tea.Cmd) {
	switch msg := msg.(type) {
	case analyticsMsg:
		m.kpis, m.callsN, m.err = msg.kpis, msg.callsN, msg.err
		return m, nil
	case ui.TickMsg:
		return m, m.Init()
	case ui.EventMsg:
		h := time.Now().Hour()
		m.hourly[h]++
	}
	return m, nil
}

var (
	hint  = lipgloss.NewStyle().Foreground(theme.Humo)
	value = lipgloss.NewStyle().Foreground(theme.Ink).Bold(true)
	badge = lipgloss.NewStyle().Foreground(theme.Live)
	deriv = lipgloss.NewStyle().Foreground(theme.AI)
)

func bar(n, max int) string {
	if max == 0 {
		return strings.Repeat(" ", 10)
	}
	filled := n * 10 / max
	return strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
}

func (m *Module) View() string {
	if m.err != nil {
		return ui.PendingView("Analytics", "Error: "+m.err.Error(), m.W, m.H)
	}
	var b strings.Builder

	conversion := 0.0
	if m.callsN > 0 {
		conversion = float64(m.kpis.LeadsReady) / float64(m.callsN) * 100
	}

	b.WriteString(hint.Render("Métricas medidas (◆ /api/analytics/kpis)\n\n"))
	b.WriteString(fmt.Sprintf("  Tool calls:        %s ◆\n", value.Render(fmt.Sprintf("%d", m.kpis.ToolCalls))))
	b.WriteString(fmt.Sprintf("  Tokens in/out:     %s ◆\n", value.Render(fmt.Sprintf("%d / %d", m.kpis.TokensIn, m.kpis.TokensOut))))
	b.WriteString(fmt.Sprintf("  Costo acumulado:   %s ◆\n", value.Render(fmt.Sprintf("$%.2f", m.kpis.CostUSD))))
	b.WriteString(fmt.Sprintf("  Latencia LLM prom: %s ◆\n", value.Render(fmt.Sprintf("%.0fms", m.kpis.AvgLLMLatencyMS))))
	b.WriteString(fmt.Sprintf("  Variables capt.:   %s ◆\n\n", value.Render(fmt.Sprintf("%d", m.kpis.VariablesCaptured))))

	b.WriteString(hint.Render("Derivado\n"))
	b.WriteString(fmt.Sprintf("  Conversión (leads_ready/llamadas): %s ◈\n\n", deriv.Render(fmt.Sprintf("%.1f%%", conversion))))

	b.WriteString(hint.Render(fmt.Sprintf("Leads: ready %d · whatsapp %d · nurturing %d\n\n",
		m.kpis.LeadsReady, m.kpis.LeadsWhatsApp, m.kpis.LeadsNurturing)))

	b.WriteString(hint.Render("Actividad por hora — eventos vistos en ESTA sesión (no histórico)\n"))
	maxH := 1
	for _, v := range m.hourly {
		if v > maxH {
			maxH = v
		}
	}
	for h := 0; h < 24; h++ {
		b.WriteString(fmt.Sprintf("  %02d %s %d\n", h, bar(m.hourly[h], maxH), m.hourly[h]))
	}

	_ = badge
	return b.String()
}
