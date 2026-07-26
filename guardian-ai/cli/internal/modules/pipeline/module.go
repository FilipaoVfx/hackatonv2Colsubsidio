package pipeline

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
	"github.com/charmbracelet/x/ansi"
)

// eventLine is one rendered row in the live feed for the tracked call.
type eventLine struct {
	t   time.Time
	typ string
	txt string
}

type Module struct {
	ui.Base
	src *api.LiveSource

	callID    string
	state     string // current STATE_CHANGED value
	lines     []eventLine
	simError  error
	simBusy   bool
	tokensIn  int
	tokensOut int
	costUSD   float64
}

func New(src *api.LiveSource) *Module {
	return &Module{Base: ui.NewBase(ui.ModPipeline, "Pipeline"), src: src, state: api.StateNew}
}

func (m *Module) Init() tea.Cmd { return nil }

type simDoneMsg struct{ err error }

func (m *Module) triggerWhatsApp() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// A fresh number each trigger so the journey always starts at NEW —
		// reusing one test number means later runs pick up mid-flow (or in a
		// terminal branch) since the backend persists conversation state.
		phone := fmt.Sprintf("57300%07d", time.Now().UnixNano()%10000000)
		err := src.SimulateWhatsApp(ctx, phone, "Hola, quiero asegurar mi vehículo")
		return simDoneMsg{err}
	}
}

func (m *Module) triggerSimCall() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := src.SimulateCall(ctx)
		return simDoneMsg{err}
	}
}

func (m *Module) Update(msg tea.Msg) (ui.Module, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "w":
			if !m.simBusy {
				m.simBusy = true
				m.simError = nil
				return m, m.triggerWhatsApp()
			}
		case "s":
			if !m.simBusy {
				m.simBusy = true
				m.simError = nil
				return m, m.triggerSimCall()
			}
		}
	case simDoneMsg:
		m.simBusy = false
		m.simError = msg.err
	case ui.EventMsg:
		ev := msg.Event
		if m.callID == "" && ev.CallID != "" {
			m.callID = ev.CallID
		}
		if ev.CallID != m.callID {
			return m, nil // only track the most recent call for now
		}
		if ev.Type == api.StateChanged {
			if to, ok := ev.Payload["to"].(string); ok {
				m.state = to
			}
		}
		// A returning WhatsApp user can start a turn already past NEW without
		// emitting STATE_CHANGED this turn (no transition happened). strategy
		// on LLM_REQUESTED reflects the conversation's actual current state,
		// so use it as a fallback signal — otherwise the stepper stays stuck
		// on NEW for every non-fresh conversation, which is most real traffic.
		if ev.Type == api.LLMRequested {
			if s, ok := ev.Payload["strategy"].(string); ok && s != "" {
				m.state = s
			}
		}
		if ev.Type == api.LLMResponse {
			if v, ok := ev.Payload["tokens_in"].(float64); ok {
				m.tokensIn += int(v)
			}
			if v, ok := ev.Payload["tokens_out"].(float64); ok {
				m.tokensOut += int(v)
			}
			if v, ok := ev.Payload["cost_usd"].(float64); ok {
				m.costUSD += v
			}
		}
		m.lines = append(m.lines, eventLine{t: time.Now(), typ: ev.Type, txt: summarize(ev)})
		if len(m.lines) > 200 {
			m.lines = m.lines[len(m.lines)-200:]
		}
	}
	return m, nil
}

// truncOneLine caps a summary at maxRunes and strips newlines, so one event
// never wraps into more than ~2 terminal rows — otherwise long LLM/WhatsApp
// text can push the fixed header/tabs off-screen in a real terminal (the
// alt-screen buffer scrolls when rendered content exceeds its row count).
func truncOneLine(s string, maxRunes int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) > maxRunes {
		return string(r[:maxRunes]) + "…"
	}
	return s
}

func summarize(ev api.Event) string {
	const maxLen = 100
	switch ev.Type {
	case api.StateChanged:
		return fmt.Sprintf("%v → %v", ev.Payload["from"], ev.Payload["to"])
	case api.LLMResponse:
		return truncOneLine(fmt.Sprintf("%v", ev.Payload["text"]), maxLen)
	case api.ToolCalled:
		return truncOneLine(fmt.Sprintf("%v(%v)", ev.Payload["tool"], ev.Payload["args"]), maxLen)
	case api.ToolExecuted:
		return fmt.Sprintf("%v — %vms", ev.Payload["tool"], ev.Payload["latency_ms"])
	case api.RecommendationGenerated:
		return truncOneLine(fmt.Sprintf("%v", ev.Payload["products"]), maxLen)
	default:
		if len(ev.Payload) == 0 {
			return ""
		}
		return truncOneLine(fmt.Sprintf("%v", ev.Payload), maxLen)
	}
}

var (
	stepPending = lipgloss.NewStyle().Foreground(theme.Humo)
	stepDone    = lipgloss.NewStyle().Foreground(theme.Live).Bold(true)
	stepCurrent = lipgloss.NewStyle().Foreground(theme.Guardian).Bold(true)
	eventStyle  = lipgloss.NewStyle().Foreground(theme.Pizarra)
	hintStyle   = lipgloss.NewStyle().Foreground(theme.Humo)
)

func (m *Module) stepIndex() int {
	for i, s := range api.PipelineStates {
		if s == m.state {
			return i
		}
	}
	return -1
}

func (m *Module) View() string {
	cur := m.stepIndex()
	var left strings.Builder
	for i, s := range api.PipelineStates {
		var icon, style = "○", stepPending
		switch {
		case i < cur:
			icon, style = "✓", stepDone
		case i == cur:
			icon, style = "●", stepCurrent
		}
		left.WriteString(style.Render(fmt.Sprintf("%s %s", icon, s)) + "\n")
	}
	if m.state == api.StateNurturing || m.state == api.StateCompleted {
		branchStyle := stepDone
		if m.state == api.StateNurturing {
			branchStyle = lipgloss.NewStyle().Foreground(theme.AI).Bold(true)
		}
		left.WriteString(branchStyle.Render(fmt.Sprintf("↳ %s (rama terminal)", m.state)) + "\n")
	}

	var right strings.Builder
	if m.callID == "" {
		right.WriteString(hintStyle.Render("Sin llamada activa.\n\n[w] simular WhatsApp   [s] simular llamada\n"))
	} else {
		right.WriteString(hintStyle.Render(fmt.Sprintf("call=%s\n\n", m.callID)))
		// Window sized to the actual content height, not a fixed constant —
		// otherwise events overflow the terminal's alt-screen row count and
		// scroll the fixed header/tabs off-screen.
		maxEvents := m.H - 6
		if maxEvents < 5 {
			maxEvents = 5
		}
		if maxEvents > 30 {
			maxEvents = 30
		}
		start := 0
		if len(m.lines) > maxEvents {
			start = len(m.lines) - maxEvents
		}
		rightWidth := m.W - m.W*38/100
		if rightWidth < 10 {
			rightWidth = 10
		}
		for _, l := range m.lines[start:] {
			line := fmt.Sprintf("%s %-26s %s", l.t.Format("15:04:05"), l.typ, l.txt)
			// Hard-truncate to the column width — one event must never wrap
			// to more than one terminal row, or enough of them overflow the
			// alt-screen's row count and scroll the fixed header off-screen.
			line = ansi.Truncate(line, rightWidth, "")
			right.WriteString(eventStyle.Render(line) + "\n")
		}
		right.WriteString(fmt.Sprintf("\n%s\n", hintStyle.Render(
			fmt.Sprintf("tokens in/out %d/%d · costo $%.4f", m.tokensIn, m.tokensOut, m.costUSD))))
	}
	if m.simBusy {
		right.WriteString(hintStyle.Render("\ndisparando simulación...\n"))
	}
	if m.simError != nil {
		right.WriteString(lipgloss.NewStyle().Foreground(theme.Danger).Render("\nerror: "+m.simError.Error()) + "\n")
	}

	leftCol := lipgloss.NewStyle().Width(m.W * 38 / 100).Render(left.String())
	rightCol := lipgloss.NewStyle().Width(m.W - m.W*38/100).Render(right.String())
	return lipgloss.JoinHorizontal(lipgloss.Top, leftCol, rightCol)
}
