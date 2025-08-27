package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Data Models
type Notification struct {
	ID         string     `json:"id"`
	Unread     bool       `json:"unread"`
	Reason     string     `json:"reason"`
	Repository Repository `json:"repository"`
	Subject    Subject    `json:"subject"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type Repository struct {
	FullName string `json:"full_name"`
}

type Subject struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Helper methods for display
func (n *Notification) StatusIcon() string {
	if n.Unread {
		return "●" // Filled circle for unread
	}
	return "○" // Empty circle for read
}

func (n *Notification) TypeDisplay() string {
	switch n.Subject.Type {
	case "PullRequest":
		return "pr"
	case "Issue":
		return "issue"
	case "Release":
		return "release"
	case "Discussion":
		return "discuss"
	default:
		return "other"
	}
}

func (n *Notification) FormattedDate() string {
	return n.UpdatedAt.Format("01-02 15:04")
}

func (n *Notification) RepoName() string {
	return n.Repository.FullName
}

// Bubble Tea Model
type Model struct {
	notifications  []Notification
	selectedIndex  int
	loading        bool
	err            error
	showingSummary bool
	summaryContent string
	statusMessage  string
	terminalWidth  int
	terminalHeight int
}

// Messages
type notificationsLoadedMsg []Notification
type notificationMarkedMsg string
type errorMsg error
type statusMsg string

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#04B575"))

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Reverse(true)

	unreadStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5F87"))

	readStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#50FA7B"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6272A4"))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8BE9FD"))
)

// GitHub CLI functions
func checkGitHubCLI() error {
	// Check if gh command exists
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("GitHub CLI (gh) is not installed")
	}

	// Check if authenticated
	cmd := exec.Command("gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("not authenticated with GitHub. Run: gh auth login")
	}

	return nil
}

func fetchNotifications() ([]Notification, error) {
	cmd := exec.Command("gh", "api", "notifications", "--paginate")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch notifications: %v", err)
	}

	var notifications []Notification
	if err := json.Unmarshal(output, &notifications); err != nil {
		return nil, fmt.Errorf("failed to parse notifications: %v", err)
	}

	return notifications, nil
}

func markAsRead(id string) error {
	cmd := exec.Command("gh", "api",
		"--method", "PATCH",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
		fmt.Sprintf("/notifications/threads/%s", id))

	return cmd.Run()
}

func extractIssueNumber(url string) string {
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}

func openInBrowser(notification Notification) error {
	repo := notification.RepoName()
	issueNum := extractIssueNumber(notification.Subject.URL)

	var cmd *exec.Cmd

	if issueNum != "" {
		switch notification.Subject.Type {
		case "Issue":
			cmd = exec.Command("gh", "issue", "view", issueNum, "-R", repo, "--web")
		case "PullRequest":
			cmd = exec.Command("gh", "pr", "view", issueNum, "-R", repo, "--web")
		default:
			cmd = exec.Command("gh", "repo", "view", repo, "--web")
		}
	} else {
		cmd = exec.Command("gh", "repo", "view", repo, "--web")
	}

	return cmd.Run()
}

// Bubble Tea Commands
func fetchNotificationsCmd() tea.Cmd {
	return func() tea.Msg {
		notifications, err := fetchNotifications()
		if err != nil {
			return errorMsg(err)
		}
		return notificationsLoadedMsg(notifications)
	}
}

func markAsReadCmd(id string) tea.Cmd {
	return func() tea.Msg {
		err := markAsRead(id)
		if err != nil {
			return errorMsg(fmt.Errorf("failed to mark as read: %v", err))
		}
		return notificationMarkedMsg(id)
	}
}

func openInBrowserCmd(notification Notification) tea.Cmd {
	return func() tea.Msg {
		err := openInBrowser(notification)
		if err != nil {
			return errorMsg(fmt.Errorf("failed to open in browser: %v", err))
		}
		return statusMsg("Opened in browser")
	}
}

// Bubble Tea Model Implementation
func initialModel() Model {
	return Model{
		notifications:  []Notification{},
		selectedIndex:  0,
		loading:        true,
		statusMessage:  "Loading notifications...",
		terminalWidth:  80,
		terminalHeight: 24,
	}
}

func (m Model) Init() tea.Cmd {
	return fetchNotificationsCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.terminalWidth = msg.Width
		m.terminalHeight = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case notificationsLoadedMsg:
		m.notifications = []Notification(msg)
		m.loading = false
		m.err = nil
		m.statusMessage = fmt.Sprintf("Loaded %d notifications", len(m.notifications))
		if len(m.notifications) == 0 {
			m.statusMessage = "No notifications found"
		}
		return m, nil

	case notificationMarkedMsg:
		// Remove the marked notification from the list
		id := string(msg)
		for i, notification := range m.notifications {
			if notification.ID == id {
				m.notifications = append(m.notifications[:i], m.notifications[i+1:]...)
				// Adjust selected index if necessary
				if m.selectedIndex >= len(m.notifications) && len(m.notifications) > 0 {
					m.selectedIndex = len(m.notifications) - 1
				}
				if len(m.notifications) == 0 {
					m.selectedIndex = 0
				}
				break
			}
		}
		m.statusMessage = "Notification marked as read"
		return m, nil

	case errorMsg:
		m.err = error(msg)
		m.loading = false
		m.statusMessage = fmt.Sprintf("Error: %v", m.err)
		return m, nil

	case statusMsg:
		m.statusMessage = string(msg)
		return m, nil
	}

	return m, nil
}

func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {

	case "q", "ctrl+c":
		return m, tea.Quit

	case "up", "k":
		if m.selectedIndex > 0 {
			m.selectedIndex--
		}
		return m, nil

	case "down", "j":
		if m.selectedIndex < len(m.notifications)-1 {
			m.selectedIndex++
		}
		return m, nil

	case "enter":
		if len(m.notifications) > 0 {
			notification := m.notifications[m.selectedIndex]
			return m, openInBrowserCmd(notification)
		}
		return m, nil

	case "r":
		if len(m.notifications) > 0 {
			notification := m.notifications[m.selectedIndex]
			return m, markAsReadCmd(notification.ID)
		}
		return m, nil

	case "f", "F5":
		m.loading = true
		m.statusMessage = "Refreshing notifications..."
		return m, fetchNotificationsCmd()

	case "tab":
		// TODO: Implement summary view in Phase 3
		m.statusMessage = "Summary view coming soon..."
		return m, nil
	}

	return m, nil
}

func (m Model) View() string {
	if m.loading {
		return fmt.Sprintf("\n  %s\n\n  %s\n",
			titleStyle.Render("GitHub Notifications"),
			"Loading notifications...")
	}

	if m.err != nil {
		return fmt.Sprintf("\n  %s\n\n  Error: %v\n\n  Press 'q' to quit, 'f' to retry\n",
			titleStyle.Render("GitHub Notifications"),
			m.err)
	}

	var b strings.Builder

	// Title
	b.WriteString(titleStyle.Render("GitHub Notifications"))
	b.WriteString("\n\n")

	// Header
	if len(m.notifications) > 0 {
		header := fmt.Sprintf("   %-8s %-20s %-10s %s", "Status", "Repository", "Type", "Title")
		b.WriteString(headerStyle.Render(header))
		b.WriteString("\n")

		// Notifications list
		visibleHeight := m.terminalHeight - 8 // Reserve space for header, status, and help
		startIdx := 0
		endIdx := len(m.notifications)

		// Adjust visible range if list is longer than screen
		if len(m.notifications) > visibleHeight {
			startIdx = m.selectedIndex - visibleHeight/2
			if startIdx < 0 {
				startIdx = 0
			}
			endIdx = startIdx + visibleHeight
			if endIdx > len(m.notifications) {
				endIdx = len(m.notifications)
				startIdx = endIdx - visibleHeight
				if startIdx < 0 {
					startIdx = 0
				}
			}
		}

		for i := startIdx; i < endIdx; i++ {
			notification := m.notifications[i]
			line := m.formatNotificationLine(notification, i)

			if i == m.selectedIndex {
				line = selectedStyle.Render(line)
			}

			b.WriteString(line)
			b.WriteString("\n")
		}
	} else {
		b.WriteString("No notifications found\n")
	}

	// Status line
	b.WriteString("\n")
	b.WriteString(statusStyle.Render(m.statusMessage))
	b.WriteString("\n")

	// Help text
	b.WriteString("\n")
	help := "↑↓:Navigate  Enter:Open  r:Mark Read  f:Refresh  Tab:Summary  q:Quit"
	b.WriteString(dimStyle.Render(help))

	return b.String()
}

func (m Model) formatNotificationLine(notification Notification, index int) string {
	// Truncate long titles to fit terminal
	maxTitleLen := m.terminalWidth - 45 // Reserve space for other columns
	if maxTitleLen < 20 {
		maxTitleLen = 20
	}

	title := notification.Subject.Title
	if len(title) > maxTitleLen {
		title = title[:maxTitleLen-3] + "..."
	}

	// Truncate repository name if too long
	repo := notification.RepoName()
	if len(repo) > 20 {
		repo = repo[:17] + "..."
	}

	// Status icon with color
	var statusIcon string
	if notification.Unread {
		statusIcon = unreadStyle.Render(notification.StatusIcon())
	} else {
		statusIcon = readStyle.Render(notification.StatusIcon())
	}

	return fmt.Sprintf("%2d %s %-20s %-10s %s",
		index+1,
		statusIcon,
		repo,
		notification.TypeDisplay(),
		title)
}

func main() {
	// Check if gh CLI is available
	if err := checkGitHubCLI(); err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Println("Please install GitHub CLI: https://cli.github.com/")
		os.Exit(1)
	}

	// Create and run the Bubble Tea program
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
