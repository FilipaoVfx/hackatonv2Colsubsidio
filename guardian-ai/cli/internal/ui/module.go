package ui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ModuleID int

const (
	ModDashboard ModuleID = iota
	ModCalls
	ModPipeline
	ModPlayground
	ModPrompt
	ModKnowledge
	ModAnalytics
	ModSettings
)

var ModuleTitles = [...]string{
	"Dashboard", "Conversaciones", "Pipeline", "Playground",
	"Prompt", "Knowledge", "Analytics", "Settings",
}

// Module is implemented by each of the 8 tabs. Returning Module (not
// tea.Model) from Update lets the root avoid type assertions entirely.
type Module interface {
	ID() ModuleID
	Title() string
	Init() tea.Cmd
	Update(msg tea.Msg) (Module, tea.Cmd)
	View() string
	SetSize(w, h int)
	KeyMap() []key.Binding
	StatusChip() (string, lipgloss.Style)
}
