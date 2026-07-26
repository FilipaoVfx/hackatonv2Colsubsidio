package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"guardianai/cli/internal/api"
	"guardianai/cli/internal/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const banner = `
███████╗███████╗ ██████╗██╗   ██╗██████╗  █████╗
██╔════╝██╔════╝██╔════╝██║   ██║██╔══██╗██╔══██╗
███████╗█████╗  ██║     ██║   ██║██████╔╝███████║
╚════██║██╔══╝  ██║     ██║   ██║██╔══██╗██╔══██║
███████║███████╗╚██████╗╚██████╔╝██║  ██║██║  ██║
╚══════╝╚══════╝ ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝`

type splashRow struct {
	label string
	ok    bool
	value string
}

type Splash struct {
	rows      []splashRow
	revealed  int
	err       error
	done      bool
	startedAt time.Time
}

func NewSplash() Splash {
	return Splash{startedAt: time.Now()}
}

type SplashDataMsg struct {
	Caps    api.Capabilities
	Latency time.Duration
	Calls   int
	Err     error
}

type splashRevealMsg struct{}

func LoadSplashData(src *api.LiveSource) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, lat, herr := src.Health(ctx)
		caps, cerr := src.Capabilities(ctx)
		ids, _ := src.ListCalls(ctx)
		err := herr
		if err == nil {
			err = cerr
		}
		return SplashDataMsg{caps, lat, len(ids), err}
	}
}

func revealTick() tea.Cmd {
	return tea.Tick(180*time.Millisecond, func(time.Time) tea.Msg { return splashRevealMsg{} })
}

func (s Splash) Update(msg tea.Msg) (Splash, tea.Cmd) {
	switch msg := msg.(type) {
	case SplashDataMsg:
		s.err = msg.Err
		row := func(label string, ok bool, value string) splashRow {
			return splashRow{label, ok, value}
		}
		s.rows = []splashRow{
			row("Guardian AI Core", msg.Err == nil, fmt.Sprintf("%dms", msg.Latency.Milliseconds())),
			row("WhatsApp (Kapso)", msg.Caps.WhatsApp, boolLabel(msg.Caps.WhatsApp)),
			row("LLM (OpenRouter)", msg.Caps.LLM, boolLabel(msg.Caps.LLM)),
			row("Voz (Vapi + ElevenLabs)", msg.Caps.Vapi && msg.Caps.ElevenLabs, boolLabel(msg.Caps.Vapi && msg.Caps.ElevenLabs)),
			row("Colsubsidio Protege API", msg.Caps.Colsubsidio, boolLabel(msg.Caps.Colsubsidio)),
			row("Llamadas persistidas", true, fmt.Sprintf("%d", msg.Calls)),
		}
		return s, revealTick()
	case splashRevealMsg:
		if s.revealed < len(s.rows) {
			s.revealed++
			return s, revealTick()
		}
		s.done = true
		return s, nil
	}
	return s, nil
}

func boolLabel(ok bool) string {
	if ok {
		return "conectado"
	}
	return "no configurado"
}

func (s Splash) Done() bool { return s.done && time.Since(s.startedAt) > 900*time.Millisecond }

var (
	bannerStyle = lipgloss.NewStyle().Foreground(theme.Guardian).Bold(true)
	taglineSt   = lipgloss.NewStyle().Foreground(theme.Pizarra)
	okSt        = lipgloss.NewStyle().Foreground(theme.Live).Bold(true)
	badSt       = lipgloss.NewStyle().Foreground(theme.Humo)
)

func (s Splash) View(w, h int) string {
	var b strings.Builder
	b.WriteString(bannerStyle.Render(banner))
	b.WriteString("\n\n")
	b.WriteString(taglineSt.Render("  Guardian AI Operations Center") + "\n\n")

	if s.err != nil && len(s.rows) == 0 {
		b.WriteString(badSt.Render("  conectando..."))
	}

	for i := 0; i < s.revealed && i < len(s.rows); i++ {
		r := s.rows[i]
		mark := okSt.Render("✓")
		style := okSt
		if !r.ok {
			mark = badSt.Render("◇")
			style = badSt
		}
		b.WriteString(fmt.Sprintf("  %s %-28s %s\n", mark, r.label, style.Render(r.value)))
	}

	if s.done {
		b.WriteString("\n" + taglineSt.Render("  presiona cualquier tecla para continuar"))
	}

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, b.String())
}
