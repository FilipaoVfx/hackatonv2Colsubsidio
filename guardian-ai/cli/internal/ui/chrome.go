package ui

import (
	"fmt"
	"strings"

	"guardianai/cli/internal/theme"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	tabActive = lipgloss.NewStyle().
			Foreground(theme.Carbon).Background(theme.Guardian).
			Bold(true).Padding(0, 2)
	tabInactive = lipgloss.NewStyle().
			Foreground(theme.Pizarra).Background(theme.Cloud).
			Padding(0, 2)
	headerStyle = lipgloss.NewStyle().
			Foreground(theme.Ink).Background(theme.Carbon).Bold(true)
	statusBarStyle = lipgloss.NewStyle().
			Foreground(theme.Humo).Background(theme.Cloud).Padding(0, 1)
	liveDot      = lipgloss.NewStyle().Foreground(theme.Live)
	offlineDot   = lipgloss.NewStyle().Foreground(theme.Danger)
	readOnlyChip = lipgloss.NewStyle().Foreground(theme.Humo).Background(theme.Cloud)
)

// RenderTabs draws the 8-module tab bar, numbered 1-8.
func RenderTabs(width int, active int) string {
	var b strings.Builder
	for i, title := range ModuleTitles {
		label := fmt.Sprintf(" %d·%s ", i+1, title)
		if i == active {
			b.WriteString(tabActive.Render(label))
		} else {
			b.WriteString(tabInactive.Render(label))
		}
	}
	bar := ansi.Truncate(b.String(), width, "")
	return lipgloss.NewStyle().MaxWidth(width).Render(bar)
}

// RenderHeader draws "Secura · Guardian AI Operations Center" plus a
// connection dot fed by the real stream/health state. readOnly adds a chip so a
// blocked publish reads as policy, not as a broken build.
func RenderHeader(width int, live bool, modeLabel string, readOnly bool) string {
	dot := offlineDot.Render("●")
	if live {
		dot = liveDot.Render("●")
	}
	left := headerStyle.Render(" SECURA · Guardian AI Operations Center")
	chip := ""
	if readOnly {
		chip = readOnlyChip.Render("◇ solo lectura") + " "
	}
	right := statusBarStyle.Render(chip + dot + " " + modeLabel + " ")
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// RenderStatusBar draws the bottom help/status line.
func RenderStatusBar(width int, hints string) string {
	return statusBarStyle.Width(width).Render(hints)
}
