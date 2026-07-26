package app

import (
	"guardianai/cli/internal/ui"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.w == 0 {
		return "cargando..."
	}
	if m.focus == FocusSplash {
		return m.splash.View(m.w, m.h)
	}

	modeLabel := m.mode.String()
	header := ui.RenderHeader(m.w, m.live, modeLabel)
	tabs := ui.RenderTabs(m.w, m.active)

	var body string
	switch {
	case m.focus == FocusPalette:
		bodyHeight := m.h - chromeHeight
		if bodyHeight < 1 {
			bodyHeight = 1
		}
		body = m.palette.View(m.w, bodyHeight)
	case m.focus == FocusHelp:
		body = helpText()
	default:
		body = m.modules[m.active].View()
	}

	hints := "q salir · tab cambiar · 1-8 saltar · ctrl+p paleta · ? ayuda · w/s simular (en Pipeline)"
	status := ui.RenderStatusBar(m.w, hints)

	return lipgloss.JoinVertical(lipgloss.Left, header, tabs, body, status)
}

func helpText() string {
	return `Atajos:
  q / ctrl+c    salir
  tab / shift+tab   cambiar módulo
  1-8           saltar a módulo
  ?             esta ayuda
  w             (Pipeline) simular WhatsApp entrante
  s             (Pipeline) simular llamada
  ↑/↓ · j/k     (Conversaciones) navegar lista
`
}
