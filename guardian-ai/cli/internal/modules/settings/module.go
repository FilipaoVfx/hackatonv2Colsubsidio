package settings

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

// Settings edits the REAL agent config knobs (Persona/Safety from
// AgentConfig) via /api/studio/config/draft + /publish. There is no
// "modelo/temperatura/provider" field in the backend — those are env vars,
// not runtime config — so this module edits what's actually editable.

var fieldNames = []string{"empathy", "formality", "closeness", "persuasion", "proactivity", "emojis", "humor", "safety_level"}

type Module struct {
	ui.Base
	src *api.LiveSource

	draft   map[string]any
	loadErr error
	cursor  int
	dirty   bool

	confirmPublish bool
	saveMsg        string
	saveErr        error
}

func New(src *api.LiveSource) *Module {
	return &Module{Base: ui.NewBase(ui.ModSettings, "Settings"), src: src}
}

type configMsg struct {
	out map[string]any
	err error
}
type draftSavedMsg struct{ err error }
type publishedMsg struct {
	out map[string]any
	err error
}

func (m *Module) Init() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := src.StudioConfig(ctx)
		return configMsg{out, err}
	}
}

func (m *Module) persona() map[string]any {
	if p, ok := m.draft["persona"].(map[string]any); ok {
		return p
	}
	return map[string]any{}
}
func (m *Module) safety() map[string]any {
	if s, ok := m.draft["safety"].(map[string]any); ok {
		return s
	}
	return map[string]any{}
}

func (m *Module) adjust(delta int) {
	p := m.persona()
	s := m.safety()
	switch fieldNames[m.cursor] {
	case "empathy", "formality", "closeness", "persuasion", "proactivity":
		key := fieldNames[m.cursor]
		v, _ := p[key].(float64)
		nv := clamp(int(v)+delta, 1, 10)
		p[key] = float64(nv)
		m.draft["persona"] = p
	case "emojis", "humor":
		key := fieldNames[m.cursor]
		v, _ := p[key].(bool)
		p[key] = !v
		m.draft["persona"] = p
	case "safety_level":
		levels := []string{"bajo", "medio", "alto"}
		cur, _ := s["level"].(string)
		idx := 0
		for i, l := range levels {
			if l == cur {
				idx = i
			}
		}
		idx = clamp(idx+delta, 0, len(levels)-1)
		s["level"] = levels[idx]
		m.draft["safety"] = s
	}
	m.dirty = true
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m *Module) saveDraftCmd() tea.Cmd {
	src := m.src
	draft := m.draft
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, err := src.StudioSaveDraft(ctx, draft)
		return draftSavedMsg{err}
	}
}

func (m *Module) publishCmd() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		out, err := src.StudioPublish(ctx, "editado desde secura cli")
		return publishedMsg{out, err}
	}
}

func (m *Module) Update(msg tea.Msg) (ui.Module, tea.Cmd) {
	switch msg := msg.(type) {
	case configMsg:
		m.loadErr = msg.err
		if msg.err == nil {
			if d, ok := msg.out["draft"].(map[string]any); ok {
				m.draft = d
			}
		}
		return m, nil
	case draftSavedMsg:
		if msg.err != nil {
			m.saveErr = msg.err
		} else {
			m.saveMsg = "borrador guardado"
			m.dirty = false
		}
		return m, nil
	case publishedMsg:
		m.confirmPublish = false
		if msg.err != nil {
			m.saveErr = msg.err
		} else {
			m.saveMsg = fmt.Sprintf("publicado — versión %v en producción", msg.out["version"])
			m.dirty = false
		}
		return m, nil
	case tea.KeyMsg:
		if m.confirmPublish {
			switch msg.String() {
			case "y":
				return m, m.publishCmd()
			case "n", "esc":
				m.confirmPublish = false
			}
			return m, nil
		}
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(fieldNames)-1 {
				m.cursor++
			}
		case "left", "h":
			m.adjust(-1)
		case "right", "l":
			m.adjust(1)
		case "d":
			if m.dirty {
				return m, m.saveDraftCmd()
			}
		case "p":
			m.confirmPublish = true
		}
	}
	return m, nil
}

var (
	rowSel  = lipgloss.NewStyle().Foreground(theme.Carbon).Background(theme.Guardian).Bold(true)
	row     = lipgloss.NewStyle().Foreground(theme.Pizarra)
	hint    = lipgloss.NewStyle().Foreground(theme.Humo)
	danger  = lipgloss.NewStyle().Foreground(theme.Danger).Bold(true)
	okStyle = lipgloss.NewStyle().Foreground(theme.Live).Bold(true)
)

func (m *Module) View() string {
	if m.loadErr != nil {
		return ui.PendingView("Settings", "Error: "+m.loadErr.Error(), m.W, m.H)
	}
	var b strings.Builder
	b.WriteString(hint.Render("Perillas reales del agente (Studio) — h/l ajustar · d guardar borrador · p publicar\n\n"))

	p := m.persona()
	s := m.safety()
	values := map[string]string{
		"empathy":      fmt.Sprintf("%v/10", p["empathy"]),
		"formality":    fmt.Sprintf("%v/10", p["formality"]),
		"closeness":    fmt.Sprintf("%v/10", p["closeness"]),
		"persuasion":   fmt.Sprintf("%v/10", p["persuasion"]),
		"proactivity":  fmt.Sprintf("%v/10", p["proactivity"]),
		"emojis":       fmt.Sprintf("%v", p["emojis"]),
		"humor":        fmt.Sprintf("%v", p["humor"]),
		"safety_level": fmt.Sprintf("%v", s["level"]),
	}
	for i, f := range fieldNames {
		line := fmt.Sprintf("  %-14s %s", f, values[f])
		if i == m.cursor {
			b.WriteString(rowSel.Render(line))
		} else {
			b.WriteString(row.Render(line))
		}
		b.WriteString("\n")
	}

	if m.dirty {
		b.WriteString("\n" + hint.Render("cambios sin guardar (d para guardar borrador)") + "\n")
	}
	if m.confirmPublish {
		b.WriteString("\n" + danger.Render("¿Publicar este borrador a PRODUCCIÓN? [y/n]") + "\n")
	}
	if m.saveMsg != "" {
		b.WriteString("\n" + okStyle.Render(m.saveMsg) + "\n")
	}
	if m.saveErr != nil {
		b.WriteString("\n" + danger.Render("error: "+m.saveErr.Error()) + "\n")
	}
	return b.String()
}
