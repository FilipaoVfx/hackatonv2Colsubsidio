package prompt

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

type confirmState int

const (
	confirmNone confirmState = iota
	confirmAsked
)

type Module struct {
	ui.Base
	src *api.LiveSource

	promptText string
	promptVer  int
	promptErr  error

	versions []map[string]any
	cursor   int
	versErr  error

	confirm     confirmState
	rollbackOK  string
	rollbackErr error
}

func New(src *api.LiveSource) *Module {
	return &Module{Base: ui.NewBase(ui.ModPrompt, "Prompt"), src: src}
}

type promptMsg struct {
	out map[string]any
	err error
}
type versionsMsg struct {
	out []map[string]any
	err error
}
type rollbackMsg struct {
	out map[string]any
	err error
}

func (m *Module) Init() tea.Cmd {
	src := m.src
	return tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			out, err := src.StudioPrompt(ctx)
			return promptMsg{out, err}
		},
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			vs, err := src.StudioVersions(ctx)
			return versionsMsg{vs, err}
		},
	)
}

func (m *Module) rollbackCmd(version int) tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		out, err := src.StudioRollback(ctx, version, "rollback desde secura cli")
		return rollbackMsg{out, err}
	}
}

func (m *Module) Update(msg tea.Msg) (ui.Module, tea.Cmd) {
	switch msg := msg.(type) {
	case promptMsg:
		m.promptErr = msg.err
		if msg.err == nil {
			if p, ok := msg.out["prompt"].(string); ok {
				m.promptText = p
			}
			if v, ok := msg.out["version"].(float64); ok {
				m.promptVer = int(v)
			}
		}
		return m, nil
	case versionsMsg:
		m.versions, m.versErr = msg.out, msg.err
		return m, nil
	case rollbackMsg:
		m.confirm = confirmNone
		m.rollbackErr = msg.err
		if msg.err == nil {
			m.rollbackOK = fmt.Sprintf("restaurado a versión %v", msg.out["restored_from"])
			return m, m.Init()
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.versions)-1 {
				m.cursor++
			}
		case "r":
			if len(m.versions) > 0 {
				m.confirm = confirmAsked
			}
		case "y":
			if m.confirm == confirmAsked && m.cursor < len(m.versions) {
				if v, ok := m.versions[m.cursor]["version"].(float64); ok {
					return m, m.rollbackCmd(int(v))
				}
			}
		case "n", "esc":
			m.confirm = confirmNone
		}
	}
	return m, nil
}

var (
	hint      = lipgloss.NewStyle().Foreground(theme.Humo)
	rowSel    = lipgloss.NewStyle().Foreground(theme.Carbon).Background(theme.Guardian).Bold(true)
	row       = lipgloss.NewStyle().Foreground(theme.Pizarra)
	dangerBox = lipgloss.NewStyle().Foreground(theme.Danger).Bold(true)
	okStyle   = lipgloss.NewStyle().Foreground(theme.Live).Bold(true)
)

func (m *Module) View() string {
	var b strings.Builder
	if m.promptErr != nil {
		b.WriteString(dangerBox.Render("error prompt: "+m.promptErr.Error()) + "\n\n")
	} else {
		b.WriteString(hint.Render(fmt.Sprintf("Prompt vivo · versión %d · %d bytes\n\n", m.promptVer, len(m.promptText))))
		preview := m.promptText
		if len(preview) > 400 {
			preview = preview[:400] + "..."
		}
		b.WriteString(preview + "\n\n")
	}

	b.WriteString(hint.Render("Historial de versiones — ↑/↓ · r rollback\n"))
	if m.versErr != nil {
		b.WriteString(dangerBox.Render(m.versErr.Error()) + "\n")
	}
	for i, v := range m.versions {
		line := fmt.Sprintf("  v%v  %v  %v", v["version"], v["status"], v["note"])
		if i == m.cursor {
			b.WriteString(rowSel.Render(line))
		} else {
			b.WriteString(row.Render(line))
		}
		b.WriteString("\n")
	}

	if m.confirm == confirmAsked {
		v := m.versions[m.cursor]
		b.WriteString("\n" + dangerBox.Render(fmt.Sprintf("¿Restaurar versión %v en PRODUCCIÓN? [y/n]", v["version"])) + "\n")
	}
	if m.rollbackOK != "" {
		b.WriteString("\n" + okStyle.Render(m.rollbackOK) + "\n")
	}
	if m.rollbackErr != nil {
		b.WriteString("\n" + dangerBox.Render("error: "+m.rollbackErr.Error()) + "\n")
	}
	return b.String()
}
