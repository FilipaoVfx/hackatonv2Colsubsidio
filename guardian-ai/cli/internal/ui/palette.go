package ui

import (
	"fmt"
	"strings"

	"guardianai/cli/internal/theme"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

type Command struct {
	Title    string
	Category string
	Target   ModuleID
}

type Palette struct {
	input  textinput.Model
	all    []Command
	hits   []fuzzy.Match
	cursor int
	Open   bool
}

func NewPalette() Palette {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = "buscar módulo o comando..."
	ti.CharLimit = 80
	var cmds []Command
	for i, t := range ModuleTitles {
		cmds = append(cmds, Command{Title: t, Category: "navegación", Target: ModuleID(i)})
	}
	return Palette{input: ti, all: cmds}
}

func (p *Palette) SetRecentCalls(ids []string) {
	// Keep the 8 navigation commands, drop any previous dynamic call entries,
	// then re-add up to 8 recent calls as deep-link commands.
	var base []Command
	for _, c := range p.all {
		if c.Category == "navegación" {
			base = append(base, c)
		}
	}
	n := len(ids)
	if n > 8 {
		n = 8
	}
	for _, id := range ids[:n] {
		base = append(base, Command{Title: "call:" + id, Category: "conversación", Target: ModCalls})
	}
	p.all = base
	p.filter()
}

func (p Palette) Toggle() Palette {
	p.Open = !p.Open
	if p.Open {
		p.input.SetValue("")
		p.input.Focus()
		p.filter()
	}
	return p
}

func (p *Palette) filter() {
	targets := make([]string, len(p.all))
	for i, c := range p.all {
		targets[i] = c.Title + " " + c.Category
	}
	q := p.input.Value()
	if q == "" {
		p.hits = nil
		for i := range p.all {
			p.hits = append(p.hits, fuzzy.Match{Index: i})
		}
		p.cursor = 0
		return
	}
	p.hits = fuzzy.Find(q, targets)
	p.cursor = 0
}

func (p Palette) Update(msg tea.Msg) (Palette, tea.Cmd, *Command) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			p.Open = false
			return p, nil, nil
		case "up", "ctrl+k":
			if p.cursor > 0 {
				p.cursor--
			}
			return p, nil, nil
		case "down", "ctrl+j":
			if p.cursor < len(p.hits)-1 {
				p.cursor++
			}
			return p, nil, nil
		case "enter":
			if p.cursor < len(p.hits) {
				idx := p.hits[p.cursor].Index
				cmd := p.all[idx]
				p.Open = false
				return p, nil, &cmd
			}
			return p, nil, nil
		}
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	p.filter()
	return p, cmd, nil
}

var (
	paletteBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).BorderForeground(theme.Guardian).
			Background(theme.Cloud).Padding(1, 2).Width(64)
	paletteRow    = lipgloss.NewStyle().Foreground(theme.Pizarra)
	paletteRowSel = lipgloss.NewStyle().Foreground(theme.Carbon).Background(theme.Guardian).Bold(true)
	paletteMatch  = lipgloss.NewStyle().Foreground(theme.Guardian).Bold(true)
	paletteCat    = lipgloss.NewStyle().Foreground(theme.Humo)
)

// renderMatched bolds the rune positions fuzzy matched, inside an otherwise
// plain string — the substring highlight that makes a palette feel VSCode.
func renderMatched(s string, idxs []int, base lipgloss.Style) string {
	if len(idxs) == 0 {
		return base.Render(s)
	}
	matched := map[int]bool{}
	for _, i := range idxs {
		matched[i] = true
	}
	var b strings.Builder
	for i, r := range []rune(s) {
		if matched[i] {
			b.WriteString(paletteMatch.Render(string(r)))
		} else {
			b.WriteString(base.Render(string(r)))
		}
	}
	return b.String()
}

func (p Palette) View(w, h int) string {
	var b strings.Builder
	b.WriteString(p.input.View() + "\n\n")
	max := 10
	for i, hit := range p.hits {
		if i >= max {
			break
		}
		c := p.all[hit.Index]
		label := fmt.Sprintf("%-40s %s", c.Title, "")
		style := paletteRow
		if i == p.cursor {
			style = paletteRowSel
		}
		line := renderMatched(label, hit.MatchedIndexes, style)
		b.WriteString(line + " " + paletteCat.Render(c.Category) + "\n")
	}
	if len(p.hits) == 0 {
		b.WriteString(paletteCat.Render("sin resultados"))
	}
	box := paletteBorder.Render(b.String())
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Top, box, lipgloss.WithWhitespaceChars(" "))
}
