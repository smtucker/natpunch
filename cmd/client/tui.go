
package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	tuiHistorySize = 200
)

var (
	docStyle      = lipgloss.NewStyle().Margin(1, 1)
	prompt        = "> "
	outputStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).BorderForeground(lipgloss.Color("63"))
	lobbyStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).BorderForeground(lipgloss.Color("63"))
	inputStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).BorderForeground(lipgloss.Color("63"))
)

type model struct {
	client         *Client
	lobbyViewport  viewport.Model
	outputViewport viewport.Model
	textinput      textinput.Model
	history        []string
	err            error
	ready          bool
}

type lobbyUpdateMsg string
type outputUpdateMsg string

func initialModel(client *Client) model {
	ti := textinput.New()
	ti.Placeholder = "Enter command..."
	ti.Focus()
	ti.Prompt = prompt
	ti.CharLimit = 280

	outputVp := viewport.New(0, 0)
	outputVp.Style = outputStyle
	outputVp.SetContent("Welcome to the NAT Punching Client!")

	lobbyVp := viewport.New(0, 0)
	lobbyVp.Style = lobbyStyle
	lobbyVp.SetContent("Lobby status will appear here.")

	return model{
		textinput:      ti,
		outputViewport: outputVp,
		lobbyViewport:  lobbyVp,
		history:        []string{"Welcome to the NAT Punching Client!"},
		err:            nil,
		client:         client,
		ready:          false,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.updateStatusCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		iCmd   tea.Cmd
		ovpCmd tea.Cmd
		lvpCmd tea.Cmd
	)

	m.textinput, iCmd = m.textinput.Update(msg)
	m.outputViewport, ovpCmd = m.outputViewport.Update(msg)
	m.lobbyViewport, lvpCmd = m.lobbyViewport.Update(msg)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		verticalMargin, horizontalMargin := docStyle.GetFrameSize()
		const inputHeight = 3 // Account for border and padding
		viewportsHeight := msg.Height - inputHeight - verticalMargin

		// Panes are horizontal, so they split the width
		lobbyWidth := msg.Width / 3
		outputWidth := msg.Width - lobbyWidth - horizontalMargin

		m.lobbyViewport.Width = lobbyWidth
		m.lobbyViewport.Height = viewportsHeight
		m.outputViewport.Width = outputWidth
		m.outputViewport.Height = viewportsHeight

		// The text input takes the full width of the window, minus the margins, borders, and padding of its container.
		m.textinput.Width = msg.Width - docStyle.GetHorizontalFrameSize() - inputStyle.GetHorizontalFrameSize() - lipgloss.Width(m.textinput.Prompt)
		if !m.ready {
			m.ready = true
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			cmdStr := m.textinput.Value()
			if cmdStr == "exit" {
				m.client.stopClient()
				return m, tea.Quit
			}
			m.history = append(m.history, "> "+cmdStr)
			go m.client.handleCommand(cmdStr)
			m.textinput.Reset()
			if m.ready {
				m.outputViewport.GotoBottom()
			}
		}
	case lobbyUpdateMsg:
		m.lobbyViewport.SetContent(string(msg))
		return m, m.updateStatusCmd()
	case outputUpdateMsg:
		m.history = append(m.history, string(msg))
		if len(m.history) > tuiHistorySize {
			m.history = m.history[len(m.history)-tuiHistorySize:]
		}
		m.outputViewport.SetContent(strings.Join(m.history, "\n"))
		if m.ready {
			m.outputViewport.GotoBottom()
		}
		return m, nil
	case error:
		m.err = msg
		return m, nil
	}

	return m, tea.Batch(iCmd, ovpCmd, lvpCmd)
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}
	mainView := lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.outputViewport.View(),
			m.lobbyViewport.View(),
		),
		inputStyle.Render(m.textinput.View()),
	)
	return docStyle.Render(mainView)
}

type tuiWriter struct {
	tui *tea.Program
}

func (w *tuiWriter) Write(p []byte) (n int, err error) {
	if w.tui != nil {
		w.tui.Send(outputUpdateMsg(strings.TrimSpace(string(p))))
	}
	return len(p), nil
}

func (c *Client) runTUI() {
	defer c.wg.Done()
	p := tea.NewProgram(initialModel(c))

	c.tui = p

	log.SetOutput(&tuiWriter{tui: p})

	go c.statusUpdater()

	if err := p.Start(); err != nil {
		log.Fatal(err)
	}
}

func (c *Client) statusUpdater() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			if c.tui == nil {
				return
			}

			c.lobbyMutex.RLock()
			var sb strings.Builder
			if c.CurrentLobby != nil {
				fmt.Fprintf(&sb, "Lobby: %s\n", c.CurrentLobby.Name)
				fmt.Fprintf(&sb, "ID: %s\n", c.CurrentLobby.ID)
				fmt.Fprintf(&sb, "Host: %s\n", c.CurrentLobby.HostClientID)
				fmt.Fprintf(&sb, "Players: %d/%d\n", c.CurrentLobby.CurrentPlayers, c.CurrentLobby.MaxPlayers)
				fmt.Fprintln(&sb, "Members:")
				for _, member := range c.CurrentLobby.Members {
					fmt.Fprintf(&sb, "- %s\n", member.ClientId)
				}
				fmt.Fprintln(&sb, "Peers:")
				for _, peer := range c.CurrentLobby.Peers {
					fmt.Fprintf(&sb, "- %s: %s\n", peer.id, peer.addr)
				}
			} else {
				fmt.Fprintln(&sb, "Available Lobbies:")
				for _, lobby := range c.AvailableLobbies {
					fmt.Fprintf(&sb, "- %s (%s) %d/%d\n", lobby.Name, lobby.ID, lobby.CurrentPlayers, lobby.MaxPlayers)
				}
			}
			c.lobbyMutex.RUnlock()

			c.tui.Send(lobbyUpdateMsg(sb.String()))
		}
	}
}

func (m model) updateStatusCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		m.client.lobbyMutex.RLock()
		defer m.client.lobbyMutex.RUnlock()
		var sb strings.Builder
		if m.client.CurrentLobby != nil {
			fmt.Fprintf(&sb, "Lobby: %s\n", m.client.CurrentLobby.Name)
			fmt.Fprintf(&sb, "ID: %s\n", m.client.CurrentLobby.ID)
			fmt.Fprintf(&sb, "Host: %s\n", m.client.CurrentLobby.HostClientID)
			fmt.Fprintf(&sb, "Players: %d/%d\n", m.client.CurrentLobby.CurrentPlayers, m.client.CurrentLobby.MaxPlayers)
			fmt.Fprintln(&sb, "Members:")
			for _, member := range m.client.CurrentLobby.Members {
				fmt.Fprintf(&sb, "- %s\n", member.ClientId)
			}
			fmt.Fprintln(&sb, "Peers:")
			for _, peer := range m.client.CurrentLobby.Peers {
				fmt.Fprintf(&sb, "- %s: %s\n", peer.id, peer.addr)
			}
		} else {
			fmt.Fprintln(&sb, "Available Lobbies:")
			for _, lobby := range m.client.AvailableLobbies {
				fmt.Fprintf(&sb, "- %s (%s) %d/%d\n", lobby.Name, lobby.ID, lobby.CurrentPlayers, lobby.MaxPlayers)
			}
		}
		return lobbyUpdateMsg(sb.String())
	})
}
