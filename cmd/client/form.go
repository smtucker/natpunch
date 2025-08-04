package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type (
	errMsg error
)

type formModel struct {
	inputs  []textinput.Model
	focused int
	err     error
}

func newFormModel() *formModel {
	m := &formModel{
		inputs:  make([]textinput.Model, 2),
		focused: 0,
		err:     nil,
	}

	var t textinput.Model
	for i := range m.inputs {
		t = textinput.New()
		t.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		t.CharLimit = 32
		t.Placeholder = ""
		t.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("63"))
		t.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

		switch i {
		case 0:
			t.Placeholder = "server address"
			t.Focus()
			t.Prompt = "┃ "
		case 1:
			t.Placeholder = "port"
			t.CharLimit = 5
			t.Prompt = "  "
		}

		m.inputs[i] = t
	}

	return m
}

func (m *formModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *formModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab", "shift+tab", "enter", "up", "down":
			s := msg.String()

			if s == "enter" && m.focused == len(m.inputs) {
				return m, tea.Quit
			}

			if s == "up" || s == "shift+tab" {
				m.focused--
			} else {
				m.focused++
			}

			if m.focused > len(m.inputs) {
				m.focused = 0
			} else if m.focused < 0 {
				m.focused = len(m.inputs)
			}

			cmds := make([]tea.Cmd, len(m.inputs))
			if m.focused < len(m.inputs) {
				for i := 0; i <= len(m.inputs)-1; i++ {
					if i == m.focused {
						cmds[i] = m.inputs[i].Focus()
						m.inputs[i].Prompt = "┃ "
					} else {
						m.inputs[i].Blur()
						m.inputs[i].Prompt = "  "
					}
				}
			}

			return m, tea.Batch(cmds...)
		}
	case tea.WindowSizeMsg:
		// We subtract the padding of the view (1*2), the border (1*2), the margin (2*2), the prompt (2), and the cursor (1)
		for i := range m.inputs {
			m.inputs[i].Width = msg.Width - 13
		}
		return m, nil
	case errMsg:
		m.err = msg
		return m, nil
	}

	cmd := m.updateInputs(msg)

	return m, cmd
}

func (m *formModel) updateInputs(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))

	for i := range m.inputs {
		m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
	}

	return tea.Batch(cmds...)
}

func (m *formModel) View() string {
	var b strings.Builder

	b.WriteString("Enter Server Details\n\n")

	for i := range m.inputs {
		b.WriteString(m.inputs[i].View())
		if i < len(m.inputs)-1 {
			b.WriteRune('\n')
		}
	}

	buttonStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("63")).
		Padding(0, 3).
		MarginTop(1)

	if m.focused == len(m.inputs) {
		buttonStyle = buttonStyle.
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("202"))
	}

	button := buttonStyle.Render("Submit")

	b.WriteString("\n\n" + button)

	form := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 2).
		Render(b.String())

	return lipgloss.NewStyle().Margin(0, 2).Render(form)
}
