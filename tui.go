package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styles for TUI
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			MarginBottom(1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("99"))

	activeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42"))

	reconnectingStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("220"))

	failedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			MarginTop(1)
)

// updateMsg is sent when a port-forward status changes
type updateMsg struct {
	forward *PortForward
}

// tickMsg is sent on each tick for refresh
type tickMsg time.Time

// model represents the TUI state
type model struct {
	manager  *PortForwardManager
	forwards []*PortForward
	width    int
	height   int
	quitting bool
}

// NewTUIModel creates a new TUI model
func NewTUIModel(manager *PortForwardManager) model {
	return model{
		manager:  manager,
		forwards: manager.GetForwards(),
	}
}

// Init initializes the TUI
func (m model) Init() tea.Cmd {
	return tea.Batch(
		waitForUpdate(m.manager),
		tickCmd(),
	)
}

// Update handles messages
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.quitting = true
			m.manager.Stop()
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case updateMsg:
		// Refresh forwards list
		m.forwards = m.manager.GetForwards()
		return m, waitForUpdate(m.manager)

	case tickMsg:
		// Periodic refresh
		m.forwards = m.manager.GetForwards()
		return m, tickCmd()
	}

	return m, nil
}

// View renders the TUI
func (m model) View() string {
	if m.quitting {
		return "Shutting down port-forwards...\n"
	}

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("nanoporter - Kubernetes Port-Forward Manager"))
	b.WriteString("\n\n")

	// Table header - wider columns to accommodate full names
	header := fmt.Sprintf("%-25s %-20s %-40s %-15s %-15s %s",
		"Cluster", "Namespace", "Service", "Ports", "Status", "Info")
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", 150))
	b.WriteString("\n")

	// Port-forward rows
	if len(m.forwards) == 0 {
		b.WriteString("No port-forwards configured.\n")
	}

	for _, pf := range m.forwards {
		pf.mu.RLock()
		cluster := pf.ClusterName
		namespace := pf.Config.Namespace
		service := pf.Config.Service
		ports := fmt.Sprintf("%d:%d", pf.Config.LocalPort, pf.Config.RemotePort)
		state := pf.State
		errorMsg := pf.Error
		retryCount := pf.RetryCount
		reconnectAt := pf.ReconnectAt
		lastCheck := pf.LastCheck
		pf.mu.RUnlock()

		// Format status with color
		var statusText, info string
		var statusStyle lipgloss.Style

		switch state {
		case StateActive:
			statusText = "🟢 Active"
			statusStyle = activeStyle
			if !lastCheck.IsZero() {
				info = fmt.Sprintf("checked %s ago", formatDuration(time.Since(lastCheck)))
			}
		case StateReconnecting:
			statusText = "🟡 Reconnecting"
			statusStyle = reconnectingStyle
			if !reconnectAt.IsZero() {
				until := time.Until(reconnectAt)
				if until > 0 {
					info = fmt.Sprintf("retry in %s (attempt %d)", formatDuration(until), retryCount)
				} else {
					info = fmt.Sprintf("retrying... (attempt %d)", retryCount)
				}
			}
		case StateFailed:
			statusText = "🔴 Failed"
			statusStyle = failedStyle
			if errorMsg != "" {
				info = truncate(errorMsg, 40)
			}
		case StateStarting:
			statusText = "⚪ Starting"
			statusStyle = lipgloss.NewStyle()
			info = "initializing..."
		case StateStopped:
			statusText = "⚫ Stopped"
			statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		}

		row := fmt.Sprintf("%-25s %-20s %-40s %-15s %-15s %s",
			cluster, namespace, service, ports, statusText, info)

		b.WriteString(statusStyle.Render(row))
		b.WriteString("\n")

		// Show error details on separate line if present and state is failed
		if state == StateFailed && errorMsg != "" && len(errorMsg) > 40 {
			b.WriteString(failedStyle.Render(fmt.Sprintf("  Error: %s", errorMsg)))
			b.WriteString("\n")
		}
	}

	// Help text
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Press 'q' or Ctrl+C to quit"))

	return b.String()
}

// waitForUpdate waits for port-forward updates
func waitForUpdate(manager *PortForwardManager) tea.Cmd {
	return func() tea.Msg {
		forward := <-manager.GetUpdateChannel()
		return updateMsg{forward: forward}
	}
}

// tickCmd returns a command that sends a tick message
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// truncate truncates a string to the specified length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
