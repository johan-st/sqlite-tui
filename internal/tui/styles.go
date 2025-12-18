package tui

import "github.com/charmbracelet/lipgloss"

// Colors - using a professional dark theme
var (
	primaryColor   = lipgloss.Color("#7C3AED") // Purple
	secondaryColor = lipgloss.Color("#10B981") // Green
	accentColor    = lipgloss.Color("#F59E0B") // Amber
	errorColor     = lipgloss.Color("#EF4444") // Red
	mutedColor     = lipgloss.Color("#6B7280") // Gray
	textColor      = lipgloss.Color("#F3F4F6") // Light gray
	bgColor        = lipgloss.Color("#1F2937") // Dark gray
)

// Pane styles
var (
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(mutedColor).
			Padding(0, 1)

	focusedPaneStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primaryColor).
				Padding(0, 1)

	paneHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(textColor).
			MarginBottom(1)
)

// List item styles
var (
	selectedItemStyle = lipgloss.NewStyle().
				Foreground(primaryColor).
				Bold(true)

	normalItemStyle = lipgloss.NewStyle().
			Foreground(textColor)

	dimItemStyle = lipgloss.NewStyle().
			Foreground(mutedColor)
)

// Table styles
var (
	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(textColor).
				BorderBottom(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(mutedColor)

	tableCellStyle = lipgloss.NewStyle().
			Foreground(textColor).
			PaddingRight(2)

	tableSelectedRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#374151")).
				Foreground(textColor)
)

// Status bar styles
var (
	statusBarStyle = lipgloss.NewStyle().
			Background(bgColor).
			Foreground(textColor).
			Padding(0, 1)

	statusKeyStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

	statusValueStyle = lipgloss.NewStyle().
				Foreground(textColor)
)

// Access level badges
var (
	adminBadge = lipgloss.NewStyle().
			Background(primaryColor).
			Foreground(lipgloss.Color("#FFF")).
			Padding(0, 1).
			Bold(true)

	readWriteBadge = lipgloss.NewStyle().
			Background(secondaryColor).
			Foreground(lipgloss.Color("#FFF")).
			Padding(0, 1)

	readOnlyBadge = lipgloss.NewStyle().
			Background(accentColor).
			Foreground(lipgloss.Color("#000")).
			Padding(0, 1)

	noBadge = lipgloss.NewStyle().
		Background(errorColor).
		Foreground(lipgloss.Color("#FFF")).
		Padding(0, 1)
)

// Query editor styles
var (
	queryPromptStyle = lipgloss.NewStyle().
				Foreground(primaryColor).
				Bold(true)

	queryInputStyle = lipgloss.NewStyle().
			Foreground(textColor)
)

// Help styles
var (
	helpKeyStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(mutedColor)
)

// Error styles
var (
	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(secondaryColor)
)

// Title style
var titleStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(primaryColor).
	MarginBottom(1)

