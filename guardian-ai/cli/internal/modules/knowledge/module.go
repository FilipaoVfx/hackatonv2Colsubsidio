package knowledge

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"guardianai/cli/internal/api"
	"guardianai/cli/internal/theme"
	"guardianai/cli/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Module struct {
	ui.Base
	src *api.LiveSource

	ragMode  string
	ragChunk int
	ragTopK  int
	loadErr  error

	queries map[string]int // aggregated from KNOWLEDGE_RETRIEVED payloads seen this session
	total   int
}

func New(src *api.LiveSource) *Module {
	return &Module{Base: ui.NewBase(ui.ModKnowledge, "Knowledge"), src: src, queries: map[string]int{}}
}

type runtimeMsg struct {
	out map[string]any
	err error
}

func (m *Module) Init() tea.Cmd {
	src := m.src
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := src.StudioConfig(ctx)
		return runtimeMsg{out, err}
	}
}

func (m *Module) Update(msg tea.Msg) (ui.Module, tea.Cmd) {
	switch msg := msg.(type) {
	case runtimeMsg:
		m.loadErr = msg.err
		if msg.err == nil {
			if rt, ok := msg.out["runtime"].(map[string]any); ok {
				if v, ok := rt["rag_mode"].(string); ok {
					m.ragMode = v
				}
				if v, ok := rt["rag_chunks"].(float64); ok {
					m.ragChunk = int(v)
				}
				if v, ok := rt["rag_top_k"].(float64); ok {
					m.ragTopK = int(v)
				}
			}
		}
		return m, nil
	case ui.EventMsg:
		if msg.Event.Type == api.KnowledgeRetrieved {
			m.total++
			q := "consulta"
			if v, ok := msg.Event.Payload["query"].(string); ok && v != "" {
				q = v
			} else if v, ok := msg.Event.Payload["intent"].(string); ok && v != "" {
				q = v
			}
			m.queries[q]++
		}
	}
	return m, nil
}

var (
	hint  = lipgloss.NewStyle().Foreground(theme.Humo)
	value = lipgloss.NewStyle().Foreground(theme.Ink).Bold(true)
)

func (m *Module) View() string {
	if m.loadErr != nil {
		return ui.PendingView("Knowledge", "Error: "+m.loadErr.Error(), m.W, m.H)
	}
	var b strings.Builder
	b.WriteString(hint.Render("Corpus RAG (runtime real del motor)\n\n"))
	b.WriteString(fmt.Sprintf("  Modo:        %s\n", value.Render(m.ragMode)))
	b.WriteString(fmt.Sprintf("  Chunks:      %s ◆\n", value.Render(fmt.Sprintf("%d", m.ragChunk))))
	b.WriteString(fmt.Sprintf("  Top-K:       %s ◆\n\n", value.Render(fmt.Sprintf("%d", m.ragTopK))))

	b.WriteString(hint.Render(fmt.Sprintf("Consultas RAG vistas en esta sesión: %d\n", m.total)))
	if m.total == 0 {
		b.WriteString(hint.Render("(sin consultas todavía — dispara una llamada en Pipeline con 'w')\n"))
		return b.String()
	}

	type kv struct {
		k string
		v int
	}
	var top []kv
	for k, v := range m.queries {
		top = append(top, kv{k, v})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].v > top[j].v })
	if len(top) > 10 {
		top = top[:10]
	}
	for _, e := range top {
		b.WriteString(fmt.Sprintf("  %-30s %d\n", e.k, e.v))
	}
	return b.String()
}
