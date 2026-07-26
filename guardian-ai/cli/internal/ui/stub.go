package ui

import (
	"guardianai/cli/internal/theme"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Base gives a module the boilerplate (size tracking, empty keymap/status)
// so concrete modules only implement what makes them different. Embed it and
// override View()/Update() as needed.
type Base struct {
	id    ModuleID
	title string
	W, H  int
}

func NewBase(id ModuleID, title string) Base {
	return Base{id: id, title: title}
}

func (b Base) ID() ModuleID          { return b.id }
func (b Base) Title() string         { return b.title }
func (b *Base) SetSize(w, h int)     { b.W, b.H = w, h }
func (b Base) KeyMap() []key.Binding { return nil }
func (b Base) StatusChip() (string, lipgloss.Style) {
	return "", lipgloss.NewStyle()
}
func (b Base) Init() tea.Cmd { return nil }

// PendingView renders a simple "coming in phase N" placeholder card so the
// module is navigable and honest about its own status during development.
func PendingView(title, note string, w, h int) string {
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(theme.Line).
		Foreground(theme.Pizarra).Background(theme.Cloud).
		Padding(1, 3).
		Render(title + "\n\n" + note)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, card)
}
