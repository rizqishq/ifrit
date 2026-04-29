package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	colorGreen  = lipgloss.Color("#22c55e")
	colorYellow = lipgloss.Color("#eab308")
	colorRed    = lipgloss.Color("#ef4444")
	colorBlue   = lipgloss.Color("#3b82f6")
	colorDim    = lipgloss.Color("#6b7280")
	colorWhite  = lipgloss.Color("#e5e7eb")

	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(colorWhite)

	styleSelected = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("#374151"))

	styleStatusBar = lipgloss.NewStyle().Foreground(colorDim)

	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
)

func statusColor(status string) lipgloss.Color {
	switch status {
	case "LISTEN":
		return colorGreen
	case "ESTABLISHED":
		return colorYellow
	case "CLOSE_WAIT", "TIME_WAIT", "FIN_WAIT1", "FIN_WAIT2":
		return colorRed
	case "SYN_SENT", "SYN_RECV":
		return colorBlue
	default:
		return colorDim
	}
}

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Kill    key.Binding
	Refresh key.Binding
	Filter  key.Binding
	Escape  key.Binding
	Quit    key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Kill: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "kill"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}

type tickMsg time.Time

type connectionsMsg struct {
	entries []PortEntry
	err     error
}

type model struct {
	entries    []PortEntry
	filtered   []PortEntry
	cursor     int
	width      int
	height     int
	interval   time.Duration
	err        error
	filter     textinput.Model
	filtering  bool
	message    string
	confirming bool
	killTarget *PortEntry
}

func initialModel(interval time.Duration) model {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.CharLimit = 64

	return model{
		interval: interval,
		filter:   ti,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchConnections, tickCmd(m.interval))
}

func fetchConnections() tea.Msg {
	entries, err := getConnections("all", "", 0)
	return connectionsMsg{entries: entries, err: err}
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		return m, tea.Batch(fetchConnections, tickCmd(m.interval))

	case connectionsMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.entries = msg.entries
		m.applyFilter()
		if m.cursor >= len(m.filtered) && len(m.filtered) > 0 {
			m.cursor = len(m.filtered) - 1
		}
		return m, nil

	case tea.KeyMsg:
		if m.confirming {
			switch msg.String() {
			case "y":
				target := m.killTarget
				m.confirming = false
				m.killTarget = nil
				err := killProcess(target.PID, false)
				if err != nil {
					m.message = fmt.Sprintf("error: %v", err)
				} else {
					m.message = fmt.Sprintf("killed %d (%s)", target.PID, target.ProcessName)
				}
				return m, fetchConnections
			default:
				m.confirming = false
				m.killTarget = nil
				m.message = "cancelled"
			}
			return m, nil
		}

		if m.filtering {
			switch {
			case key.Matches(msg, keys.Escape):
				m.filtering = false
				m.filter.Blur()
				m.filter.SetValue("")
				m.applyFilter()
				m.cursor = 0
				return m, nil
			case msg.String() == "enter":
				m.filtering = false
				m.filter.Blur()
				return m, nil
			default:
				var cmd tea.Cmd
				m.filter, cmd = m.filter.Update(msg)
				m.applyFilter()
				m.cursor = 0
				return m, cmd
			}
		}

		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, keys.Down):
			if len(m.filtered) > 0 && m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		case key.Matches(msg, keys.Kill):
			if len(m.filtered) > 0 {
				entry := m.filtered[m.cursor]
				if entry.PID > 0 {
					m.confirming = true
					m.killTarget = &entry
					m.message = fmt.Sprintf("kill %d (%s)? y/n", entry.PID, entry.ProcessName)
				} else {
					m.message = "cannot kill: no PID"
				}
			}
		case key.Matches(msg, keys.Refresh):
			m.message = "refreshing..."
			return m, fetchConnections
		case key.Matches(msg, keys.Filter):
			m.filtering = true
			m.filter.Focus()
			return m, m.filter.Cursor.BlinkCmd()
		}
	}

	return m, nil
}

func (m *model) applyFilter() {
	query := strings.ToLower(m.filter.Value())
	if query == "" {
		m.filtered = m.entries
		return
	}

	var result []PortEntry
	for _, e := range m.entries {
		line := fmt.Sprintf("%d %d %s %s %s %s",
			e.PID, e.LocalPort, e.Proto, e.Status, e.ProcessName, e.User)
		if strings.Contains(strings.ToLower(line), query) {
			result = append(result, e)
		}
	}
	m.filtered = result
}

func (m model) View() string {
	if m.width == 0 {
		return "loading..."
	}

	var b strings.Builder

	title := styleTitle.Render("ifrit")
	info := styleStatusBar.Render(fmt.Sprintf(
		"  %d connections  refresh %s", len(m.filtered), m.interval))
	b.WriteString(title + info + "\n")

	if m.filtering || m.filter.Value() != "" {
		b.WriteString("/ " + m.filter.View() + "\n")
	} else {
		b.WriteString("\n")
	}

	header := fmt.Sprintf("  %-8s %-7s %-7s %-14s %-20s %-12s",
		"PID", "PORT", "PROTO", "STATUS", "PROCESS", "USER")
	b.WriteString(styleHeader.Render(header) + "\n")

	// Calculate how many rows we can show.
	// 4 lines reserved: title, filter/blank, header, status bar
	maxRows := m.height - 4
	if maxRows < 1 {
		maxRows = 1
	}

	if m.cursor >= len(m.filtered) && len(m.filtered) > 0 {
		m.cursor = len(m.filtered) - 1
	}

	if m.err != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(colorRed).Render(
			fmt.Sprintf("  error: %v", m.err)) + "\n")
	} else if len(m.filtered) == 0 {
		b.WriteString(styleStatusBar.Render("  no connections found") + "\n")
	} else {
		start := 0
		if m.cursor >= maxRows {
			start = m.cursor - maxRows + 1
		}
		end := start + maxRows
		if end > len(m.filtered) {
			end = len(m.filtered)
		}

		for i := start; i < end; i++ {
			e := m.filtered[i]
			sColor := statusColor(e.Status)
			status := lipgloss.NewStyle().Foreground(sColor).Render(
				fmt.Sprintf("%-14s", e.Status))

			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}

			line := fmt.Sprintf("%s%-8d %-7d %-7s %s %-20s %-12s",
				cursor, e.PID, e.LocalPort, e.Proto, status, e.ProcessName, e.User)

			if i == m.cursor {
				line = styleSelected.Render(line)
			}

			b.WriteString(line + "\n")
		}
	}

	if m.confirming && m.killTarget != nil {
		rendered := strings.Count(b.String(), "\n")
		for rendered < m.height-3 {
			b.WriteString("\n")
			rendered++
		}

		prompt := fmt.Sprintf(" kill %d (%s)? ", m.killTarget.PID, m.killTarget.ProcessName)
		hint := "y/n"

		dialogStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorYellow).
			Padding(0, 1).
			Width(m.width - 4)

		content := lipgloss.NewStyle().Bold(true).Foreground(colorYellow).Render(prompt) +
			lipgloss.NewStyle().Foreground(colorDim).Render(hint)

		b.WriteString(lipgloss.Place(m.width, 3, lipgloss.Center, lipgloss.Bottom, dialogStyle.Render(content)))

		return b.String()
	}

	// Pad remaining space so the status bar stays at the bottom
	rendered := strings.Count(b.String(), "\n")
	for rendered < m.height-1 {
		b.WriteString("\n")
		rendered++
	}

	helpKeys := []struct{ key, desc string }{
		{"↑/k", "up"},
		{"↓/j", "down"},
		{"x", "kill"},
		{"r", "refresh"},
		{"/", "filter"},
		{"q", "quit"},
	}
	var helpParts []string
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(colorWhite)
	descStyle := lipgloss.NewStyle().Foreground(colorDim)
	for _, h := range helpKeys {
		helpParts = append(helpParts, keyStyle.Render(h.key)+" "+descStyle.Render(h.desc))
	}
	helpLine := strings.Join(helpParts, descStyle.Render("  ·  "))

	var styledMessage string
	switch {
	case m.message == "" || m.message == "ready":
		styledMessage = styleStatusBar.Render("ready")
	case strings.HasPrefix(m.message, "error"):
		styledMessage = lipgloss.NewStyle().Bold(true).Foreground(colorRed).Render(m.message)
	case strings.HasPrefix(m.message, "killed"):
		styledMessage = lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render(m.message)
	case m.message == "cancelled":
		styledMessage = lipgloss.NewStyle().Bold(true).Foreground(colorYellow).Render(m.message)
	default:
		styledMessage = lipgloss.NewStyle().Foreground(colorWhite).Render(m.message)
	}

	gap := m.width - lipgloss.Width(styledMessage) - lipgloss.Width(helpLine)
	if gap < 1 {
		gap = 1
	}
	bar := styledMessage + strings.Repeat(" ", gap) + helpLine
	b.WriteString(bar)

	return b.String()
}

var watchCmd = &cobra.Command{
	Use:     "watch",
	Short:   "Interactive TUI to monitor ports in real time",
	Aliases: []string{"tui", "ui"},
	RunE: func(cmd *cobra.Command, args []string) error {
		interval, _ := cmd.Flags().GetInt("interval")
		d := time.Duration(interval) * time.Second

		p := tea.NewProgram(
			initialModel(d),
			tea.WithAltScreen(),
		)
		_, err := p.Run()
		return err
	},
}

func init() {
	watchCmd.Flags().IntP("interval", "i", 2, "refresh interval in seconds")
	rootCmd.AddCommand(watchCmd)
}
