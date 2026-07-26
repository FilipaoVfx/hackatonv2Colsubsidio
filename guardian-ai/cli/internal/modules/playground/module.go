package playground

import (
	"context"
	"strings"
	"time"

	"guardianai/cli/internal/api"
	"guardianai/cli/internal/theme"
	"guardianai/cli/internal/ui"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type turn struct {
	role string
	text string
}

type Module struct {
	ui.Base
	src *api.LiveSource

	sessionID string
	enabled   bool
	startErr  error
	turns     []turn
	input     textinput.Model
	spin      spinner.Model
	busy      bool
	sendErr   error
}

func New(src *api.LiveSource) *Module {
	ti := textinput.New()
	ti.Placeholder = "Escribe como si fueras el cliente..."
	ti.Prompt = "> "
	ti.Focus()
	ti.CharLimit = 300

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(theme.Guardian)

	return &Module{Base: ui.NewBase(ui.ModPlayground, "Playground"), src: src, input: ti, spin: sp}
}

type infoMsg struct {
	out map[string]any
	err error
}
type startMsg struct {
	out map[string]any
	err error
}
type msgSentMsg struct {
	out map[string]any
	err error
}

func (m *Module) Init() tea.Cmd {
	src := m.src
	return tea.Batch(m.spin.Tick, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		out, err := src.PlaygroundInfo(ctx)
		return infoMsg{out, err}
	})
}

func (m *Module) startCmd() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		out, err := src.PlaygroundStart(ctx)
		return startMsg{out, err}
	}
}

func (m *Module) sendCmd(text string) tea.Cmd {
	src := m.src
	sessionID := m.sessionID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := src.PlaygroundMessage(ctx, sessionID, text)
		return msgSentMsg{out, err}
	}
}

func (m *Module) Update(msg tea.Msg) (ui.Module, tea.Cmd) {
	switch msg := msg.(type) {
	case infoMsg:
		if msg.err != nil {
			m.startErr = msg.err
			return m, nil
		}
		if enabled, ok := msg.out["enabled"].(bool); ok {
			m.enabled = enabled
		}
		if m.enabled {
			return m, m.startCmd()
		}
		return m, nil
	case startMsg:
		if msg.err != nil {
			m.startErr = msg.err
			return m, nil
		}
		if enabled, ok := msg.out["enabled"].(bool); ok {
			m.enabled = enabled
		}
		if sid, ok := msg.out["session_id"].(string); ok {
			m.sessionID = sid
		}
		return m, nil
	case msgSentMsg:
		m.busy = false
		if msg.err != nil {
			m.sendErr = msg.err
			return m, nil
		}
		m.sendErr = nil
		if reply, ok := msg.out["reply"].(string); ok {
			m.turns = append(m.turns, turn{"agent", reply})
		} else if text, ok := msg.out["text"].(string); ok {
			m.turns = append(m.turns, turn{"agent", text})
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if msg.String() == "enter" && m.input.Value() != "" && !m.busy && m.enabled {
			text := m.input.Value()
			m.turns = append(m.turns, turn{"user", text})
			m.input.SetValue("")
			m.busy = true
			return m, tea.Batch(m.sendCmd(text), m.spin.Tick)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

var (
	sandboxBadge = lipgloss.NewStyle().Foreground(theme.Carbon).Background(theme.GuardianDeep).Bold(true).Padding(0, 1)
	roleUserSt   = lipgloss.NewStyle().Foreground(theme.Ink).Bold(true)
	roleAgentSt  = lipgloss.NewStyle().Foreground(theme.AI).Bold(true)
	hintSt       = lipgloss.NewStyle().Foreground(theme.Humo)
)

func (m *Module) View() string {
	var b strings.Builder
	b.WriteString(sandboxBadge.Render("SANDBOX · agente real, sesión aislada · no consume producción") + "\n\n")

	if m.startErr != nil {
		return b.String() + lipgloss.NewStyle().Foreground(theme.Danger).Render("error al iniciar: "+m.startErr.Error())
	}
	if !m.enabled {
		return b.String() + hintSt.Render("Playground deshabilitado en el backend (LLM no configurado).")
	}

	for _, t := range m.turns {
		if t.role == "user" {
			b.WriteString(roleUserSt.Render("Tú: ") + t.text + "\n\n")
		} else {
			b.WriteString(roleAgentSt.Render("Agente: ") + t.text + "\n\n")
		}
	}
	if m.busy {
		b.WriteString(m.spin.View() + " pensando...\n\n")
	}
	if m.sendErr != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(theme.Danger).Render("error: "+m.sendErr.Error()) + "\n\n")
	}
	b.WriteString(m.input.View())
	return b.String()
}
