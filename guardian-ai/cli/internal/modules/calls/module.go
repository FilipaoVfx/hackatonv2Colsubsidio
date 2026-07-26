package calls

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"guardianai/cli/internal/api"
	"guardianai/cli/internal/theme"
	"guardianai/cli/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type pane int

const (
	paneList pane = iota
	paneDetail
)

type section int

const (
	secTranscript section = iota
	secFases
	secTools
	secInsights
	secCosto
)

var sectionTitles = []string{"Transcript", "Fases", "Tool Calls", "Insights", "Costo"}

type Module struct {
	ui.Base
	src *api.LiveSource

	ids    []string
	cursor int
	err    error
	flash  map[string]time.Time

	pane    pane
	section section
	detail  api.CallDetail
	events  []api.Event
	detErr  error // CallDetail (analytics) — only blocks the Insights section
	evErr   error // CallEvents — blocks everything else
	loading bool
}

func New(src *api.LiveSource) *Module {
	return &Module{Base: ui.NewBase(ui.ModCalls, "Conversaciones"), src: src, flash: map[string]time.Time{}}
}

type callsListMsg struct {
	ids []string
	err error
}

type callDetailMsg struct {
	detail api.CallDetail
	events []api.Event
	detErr error
	evErr  error
}

func (m *Module) Init() tea.Cmd {
	return m.refresh()
}

func (m *Module) refresh() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ids, err := src.ListCalls(ctx)
		return callsListMsg{ids, err}
	}
}

func (m *Module) loadDetail(id string) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		det, derr := src.CallDetail(ctx, id)
		evs, eerr := src.CallEvents(ctx, id)
		return callDetailMsg{det, evs, derr, eerr}
	}
}

// mergeByRecency keeps calls seen live during this session pinned to the top
// (most recent first), then appends whatever the API returns in its own
// order, skipping duplicates. Without this, a plain API refresh reshuffles
// the list on every 2s tick and can bury the call the user just triggered.
func mergeByRecency(prev, fresh []string, flash map[string]time.Time) []string {
	type seen struct {
		id string
		at time.Time
	}
	var recent []seen
	for _, id := range prev {
		if t, ok := flash[id]; ok {
			recent = append(recent, seen{id, t})
		}
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].at.After(recent[j].at) })

	out := make([]string, 0, len(fresh)+len(recent))
	added := map[string]bool{}
	for _, r := range recent {
		out = append(out, r.id)
		added[r.id] = true
	}
	for _, id := range fresh {
		if !added[id] {
			out = append(out, id)
			added[id] = true
		}
	}
	return out
}

func (m *Module) currentID() string {
	if m.cursor < 0 || m.cursor >= len(m.ids) {
		return ""
	}
	return m.ids[m.cursor]
}

func (m *Module) Update(msg tea.Msg) (ui.Module, tea.Cmd) {
	switch msg := msg.(type) {
	case callsListMsg:
		m.err = msg.err
		if msg.err == nil {
			m.ids = mergeByRecency(m.ids, msg.ids, m.flash)
		}
		return m, nil
	case callDetailMsg:
		m.detail, m.events = msg.detail, msg.events
		m.detErr, m.evErr = msg.detErr, msg.evErr
		m.loading = false
		return m, nil
	case ui.TickMsg:
		if m.pane == paneList {
			return m, m.refresh()
		}
		return m, nil
	case ui.EventMsg:
		if msg.Event.CallID != "" {
			m.flash[msg.Event.CallID] = time.Now()
			found := false
			for _, id := range m.ids {
				if id == msg.Event.CallID {
					found = true
					break
				}
			}
			if !found {
				m.ids = append([]string{msg.Event.CallID}, m.ids...)
			}
			if m.pane == paneDetail && m.currentID() == msg.Event.CallID {
				m.events = append(m.events, msg.Event)
			}
		}
		return m, nil
	case tea.KeyMsg:
		switch m.pane {
		case paneList:
			switch msg.String() {
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(m.ids)-1 {
					m.cursor++
				}
			case "enter":
				if id := m.currentID(); id != "" {
					m.pane = paneDetail
					m.loading = true
					m.detErr = nil
					return m, m.loadDetail(id)
				}
			}
		case paneDetail:
			switch msg.String() {
			case "esc":
				m.pane = paneList
			case "h", "left":
				if m.section > 0 {
					m.section--
				}
			case "l", "right":
				if int(m.section) < len(sectionTitles)-1 {
					m.section++
				}
			}
		}
	}
	return m, nil
}

var (
	rowStyle    = lipgloss.NewStyle().Foreground(theme.Pizarra)
	rowSelected = lipgloss.NewStyle().Foreground(theme.Carbon).Background(theme.Guardian).Bold(true)
	rowFlash    = lipgloss.NewStyle().Foreground(theme.Live).Bold(true)
	secActive   = lipgloss.NewStyle().Foreground(theme.Carbon).Background(theme.AI).Bold(true).Padding(0, 1)
	secInactive = lipgloss.NewStyle().Foreground(theme.Humo).Padding(0, 1)
	hint        = lipgloss.NewStyle().Foreground(theme.Humo)
	roleUser    = lipgloss.NewStyle().Foreground(theme.Ink).Bold(true)
	roleAgent   = lipgloss.NewStyle().Foreground(theme.AI).Bold(true)
)

func (m *Module) View() string {
	if m.pane == paneDetail {
		return m.viewDetail()
	}
	return m.viewList()
}

func (m *Module) viewList() string {
	if m.err != nil {
		return ui.PendingView("Conversaciones", "Error: "+m.err.Error(), m.W, m.H)
	}
	if len(m.ids) == 0 {
		return ui.PendingView("Conversaciones", "Sin llamadas todavía. Presiona w en Pipeline para disparar una.", m.W, m.H)
	}
	var b strings.Builder
	b.WriteString(rowStyle.Render(fmt.Sprintf("%d conversaciones — ↑/↓ navegar · enter abrir\n\n", len(m.ids))))
	for i, id := range m.ids {
		line := fmt.Sprintf("  %s", id)
		recentFlash := time.Since(m.flash[id]) < 400*time.Millisecond
		switch {
		case i == m.cursor:
			b.WriteString(rowSelected.Render(line))
		case recentFlash:
			b.WriteString(rowFlash.Render(line))
		default:
			b.WriteString(rowStyle.Render(line))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m *Module) viewDetail() string {
	var b strings.Builder
	b.WriteString(hint.Render(fmt.Sprintf("call=%s · esc volver · h/l cambiar sección\n\n", m.currentID())))

	var tabs []string
	for i, t := range sectionTitles {
		if section(i) == m.section {
			tabs = append(tabs, secActive.Render(t))
		} else {
			tabs = append(tabs, secInactive.Render(t))
		}
	}
	b.WriteString(strings.Join(tabs, " ") + "\n\n")

	if m.loading {
		b.WriteString(hint.Render("cargando..."))
		return b.String()
	}
	if m.evErr != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(theme.Danger).Render("error: " + m.evErr.Error()))
		return b.String()
	}

	switch m.section {
	case secTranscript:
		b.WriteString(m.renderTranscript())
	case secFases:
		b.WriteString(m.renderFases())
	case secTools:
		b.WriteString(m.renderTools())
	case secInsights:
		b.WriteString(m.renderInsights())
	case secCosto:
		b.WriteString(m.renderCosto())
	}
	return b.String()
}

func (m *Module) renderTranscript() string {
	var b strings.Builder
	for _, ev := range m.events {
		if ev.Type != api.TranscriptUpdated {
			continue
		}
		role, _ := ev.Payload["role"].(string)
		text, _ := ev.Payload["text"].(string)
		style := roleUser
		label := "Usuario"
		if role == "agent" {
			style = roleAgent
			label = "Agente"
		}
		b.WriteString(style.Render(label+": ") + text + "\n\n")
	}
	if b.Len() == 0 {
		return hint.Render("sin transcript")
	}
	return b.String()
}

func (m *Module) renderFases() string {
	var b strings.Builder
	for _, ev := range m.events {
		if ev.Type != api.StateChanged {
			continue
		}
		b.WriteString(fmt.Sprintf("%v → %v  (%v)\n", ev.Payload["from"], ev.Payload["to"], ev.Payload["reason"]))
	}
	if b.Len() == 0 {
		return hint.Render("sin cambios de estado")
	}
	return b.String()
}

func (m *Module) renderTools() string {
	var b strings.Builder
	for _, ev := range m.events {
		switch ev.Type {
		case api.ToolCalled:
			b.WriteString(fmt.Sprintf("→ %v(%v)\n", ev.Payload["tool"], ev.Payload["args"]))
		case api.ToolExecuted:
			b.WriteString(fmt.Sprintf("  %v — %vms  error=%v\n\n", ev.Payload["tool"], ev.Payload["latency_ms"], ev.Payload["error"]))
		}
	}
	if b.Len() == 0 {
		return hint.Render("sin tool calls")
	}
	return b.String()
}

func (m *Module) renderInsights() string {
	if m.detErr != nil {
		return hint.Render("sin insights todavía: " + m.detErr.Error())
	}
	if m.detail == nil {
		return hint.Render("sin insights (requiere Supabase)")
	}
	pretty, _ := json.MarshalIndent(m.detail, "", "  ")
	return string(pretty)
}

func (m *Module) renderCosto() string {
	var tokensIn, tokensOut int
	var cost float64
	for _, ev := range m.events {
		if ev.Type != api.LLMResponse {
			continue
		}
		if v, ok := ev.Payload["tokens_in"].(float64); ok {
			tokensIn += int(v)
		}
		if v, ok := ev.Payload["tokens_out"].(float64); ok {
			tokensOut += int(v)
		}
		if v, ok := ev.Payload["cost_usd"].(float64); ok {
			cost += v
		}
	}
	return fmt.Sprintf("Tokens in:  %d ◆\nTokens out: %d ◆\nCosto:      $%.4f ◆\n\n(medido de LLM_RESPONSE de esta llamada)", tokensIn, tokensOut, cost)
}
