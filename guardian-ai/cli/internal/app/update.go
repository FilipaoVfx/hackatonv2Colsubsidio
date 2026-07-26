package app

import (
	"time"

	"guardianai/cli/internal/api"
	"guardianai/cli/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

type rootHealthCheckMsg struct{}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		cw, ch := msg.Width, msg.Height-chromeHeight
		if ch < 1 {
			ch = 1
		}
		var cmds []tea.Cmd
		for i := range m.modules {
			m.modules[i].SetSize(cw, ch)
		}
		return m, tea.Batch(cmds...)

	case ui.EventMsg:
		// Broadcast to every module, then re-arm the WS listener.
		var cmds []tea.Cmd
		for i, mod := range m.modules {
			nm, cmd := mod.Update(msg)
			m.modules[i] = nm
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		m.live = true
		cmds = append(cmds, waitForEvent(m.src))
		return m, tea.Batch(cmds...)

	case ui.HealthMsg:
		m.live = msg.OK
		if msg.OK {
			m.mode = api.ModeLive
		} else {
			m.mode = api.ModeOffline
		}
		return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg {
			return rootHealthCheckMsg{}
		})

	case rootHealthCheckMsg:
		return m, rootHealthCheck(m.src)

	case ui.TickMsg:
		var cmds []tea.Cmd
		for i, mod := range m.modules {
			nm, cmd := mod.Update(msg)
			m.modules[i] = nm
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, healthTick())
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		if m.focus == FocusSplash {
			if m.splash.Done() {
				m.focus = FocusModule
			}
			return m, nil
		}
		if msg.String() == "ctrl+p" {
			if m.focus == FocusPalette {
				m.focus = FocusModule
				m.palette.Open = false
			} else {
				m.prevFocus = m.focus
				m.focus = FocusPalette
				m.palette = m.palette.Toggle()
			}
			return m, nil
		}
		if m.focus == FocusPalette {
			var cmd tea.Cmd
			var chosen *ui.Command
			m.palette, cmd, chosen = m.palette.Update(msg)
			if !m.palette.Open {
				m.focus = FocusModule
			}
			if chosen != nil {
				m.active = int(chosen.Target)
			}
			return m, cmd
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "tab":
			m.active = (m.active + 1) % len(m.modules)
			return m, nil
		case "shift+tab":
			m.active = (m.active - 1 + len(m.modules)) % len(m.modules)
			return m, nil
		case "1", "2", "3", "4", "5", "6", "7", "8":
			idx := int(msg.String()[0] - '1')
			if idx >= 0 && idx < len(m.modules) {
				m.active = idx
			}
			return m, nil
		case "?":
			if m.focus == FocusHelp {
				m.focus = FocusModule
			} else {
				m.focus = FocusHelp
			}
			return m, nil
		}
		// Targeted: active module gets first refusal of remaining keys.
		nm, cmd := m.modules[m.active].Update(msg)
		m.modules[m.active] = nm
		return m, cmd

	default:
		// Broadcast: async replies from a module's own Init()/refresh commands
		// carry module-specific message types (promptMsg, callsListMsg, ...).
		// They must reach their owning module even if a different tab is
		// active, so every module gets a look and ignores what it doesn't
		// recognize via its own type switch. The splash gets the same
		// treatment (SplashDataMsg, and its own private reveal-tick message)
		// without app/ needing to name the unexported tick type.
		var cmds []tea.Cmd
		for i, mod := range m.modules {
			nm, cmd := mod.Update(msg)
			m.modules[i] = nm
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		var splashCmd tea.Cmd
		m.splash, splashCmd = m.splash.Update(msg)
		if splashCmd != nil {
			cmds = append(cmds, splashCmd)
		}
		return m, tea.Batch(cmds...)
	}
}
