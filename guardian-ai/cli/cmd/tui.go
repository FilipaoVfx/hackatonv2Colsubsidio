package cmd

import (
	"guardianai/cli/internal/app"
	"guardianai/cli/internal/config"

	tea "github.com/charmbracelet/bubbletea"
)

func runTUI(cfg config.Config) error {
	m := app.New(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
