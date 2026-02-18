package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dormstern/segspec/internal/model"
)

type item struct {
	dep      model.NetworkDependency
	selected bool
}

// Picker is a bubbletea model for selecting dependencies.
type Picker struct {
	items     []item
	cursor    int
	confirmed bool
}

// NewPicker creates a picker with all deps selected by default.
func NewPicker(deps []model.NetworkDependency) *Picker {
	items := make([]item, len(deps))
	for i, d := range deps {
		items[i] = item{dep: d, selected: true}
	}
	return &Picker{items: items}
}

func (p *Picker) toggle(i int) {
	if i >= 0 && i < len(p.items) {
		p.items[i].selected = !p.items[i].selected
	}
}

func (p *Picker) selectAll() {
	for i := range p.items {
		p.items[i].selected = true
	}
}

func (p *Picker) selectNone() {
	for i := range p.items {
		p.items[i].selected = false
	}
}

// Selected returns the dependencies the user accepted.
func (p *Picker) Selected() []model.NetworkDependency {
	var result []model.NetworkDependency
	for _, it := range p.items {
		if it.selected {
			result = append(result, it.dep)
		}
	}
	return result
}

// Confirmed returns true if user pressed Enter (not q).
func (p *Picker) Confirmed() bool {
	return p.confirmed
}

func (p *Picker) Init() tea.Cmd {
	return nil
}

func (p *Picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return p, tea.Quit
		case "enter":
			p.confirmed = true
			return p, tea.Quit
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.items)-1 {
				p.cursor++
			}
		case " ":
			p.toggle(p.cursor)
		case "a":
			p.selectAll()
		case "n":
			p.selectNone()
		}
	}
	return p, nil
}

var (
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	unselStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	cursorStyle   = lipgloss.NewStyle().Bold(true)
	headerStyle   = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

func (p *Picker) View() string {
	var b strings.Builder

	selected := 0
	for _, it := range p.items {
		if it.selected {
			selected++
		}
	}

	fmt.Fprintf(&b, "%s\n\n", headerStyle.Render(
		fmt.Sprintf("segspec: %d dependencies found (%d selected)", len(p.items), selected)))

	for i, it := range p.items {
		cursor := "  "
		if i == p.cursor {
			cursor = "> "
		}

		checkbox := "[ ]"
		style := unselStyle
		if it.selected {
			checkbox = "[x]"
			style = selectedStyle
		}

		line := fmt.Sprintf("%s %s -> %s:%d/%s  [%s]  %s",
			checkbox,
			it.dep.Source, it.dep.Target, it.dep.Port, it.dep.Protocol,
			it.dep.Confidence, it.dep.Description)

		if i == p.cursor {
			fmt.Fprintf(&b, "%s%s\n", cursorStyle.Render(cursor), style.Render(line))
		} else {
			fmt.Fprintf(&b, "%s%s\n", cursor, style.Render(line))
		}
	}

	fmt.Fprintf(&b, "\n%s\n", helpStyle.Render(
		"up/down navigate  SPACE toggle  a all  n none  ENTER generate  q quit"))

	return b.String()
}

// Run launches the interactive picker and returns the selected dependencies.
// Returns nil, false if user quit without confirming.
func Run(deps []model.NetworkDependency) ([]model.NetworkDependency, bool) {
	p := NewPicker(deps)
	finalModel, err := tea.NewProgram(p).Run()
	if err != nil {
		return nil, false
	}
	picker := finalModel.(*Picker)
	if !picker.Confirmed() {
		return nil, false
	}
	return picker.Selected(), true
}
