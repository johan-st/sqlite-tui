package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/johan-st/sqlite-tui/internal/access"
	"github.com/johan-st/sqlite-tui/internal/database"
	"github.com/johan-st/sqlite-tui/internal/history"
)

// Focus represents which pane is focused
type Focus int

const (
	FocusDatabases Focus = iota
	FocusTables
	FocusData
	FocusQuery
)

// App is the main TUI application model.
type App struct {
	// Dependencies
	dbManager    *database.Manager
	historyStore *history.Store
	user         *access.UserInfo

	// Window size
	width, height int

	// State
	focus         Focus
	databases     []*database.DatabaseInfo
	selectedDB    int
	tables        []string
	selectedTable int
	data          *database.QueryResult
	selectedRow   int
	schema        *database.TableInfo

	// Query input
	queryInput  string
	queryActive bool
	queryResult *database.QueryResult
	queryError  error

	// UI state
	showHelp   bool
	showSchema bool
	err        error

	// Key bindings
	keys KeyMap
}

// NewApp creates a new TUI application.
func NewApp(dbManager *database.Manager, historyStore *history.Store, user *access.UserInfo, width, height int) *App {
	app := &App{
		dbManager:    dbManager,
		historyStore: historyStore,
		user:         user,
		width:        width,
		height:       height,
		focus:        FocusDatabases,
		keys:         DefaultKeyMap(),
	}

	return app
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd {
	return a.loadDatabases
}

// loadDatabases loads the list of databases.
func (a *App) loadDatabases() tea.Msg {
	databases := a.dbManager.ListDatabases(a.user)
	return DatabasesLoadedMsg{Databases: databases}
}

// loadTables loads tables for the selected database.
func (a *App) loadTables() tea.Msg {
	if a.selectedDB >= len(a.databases) {
		return TablesLoadedMsg{Error: fmt.Errorf("no database selected")}
	}

	db := a.databases[a.selectedDB]
	conn, err := a.dbManager.OpenConnection(db.Alias, a.user)
	if err != nil {
		return TablesLoadedMsg{Error: err}
	}

	schema := database.NewSchema(conn)
	tables, err := schema.ListTables()
	return TablesLoadedMsg{Tables: tables, Error: err}
}

// loadData loads data for the selected table.
func (a *App) loadData() tea.Msg {
	if a.selectedDB >= len(a.databases) || a.selectedTable >= len(a.tables) {
		return DataLoadedMsg{Error: fmt.Errorf("no table selected")}
	}

	db := a.databases[a.selectedDB]
	table := a.tables[a.selectedTable]

	conn, err := a.dbManager.OpenConnection(db.Alias, a.user)
	if err != nil {
		return DataLoadedMsg{Error: err}
	}

	opts := database.DefaultSelectOptions()
	opts.Limit = 100
	result, err := database.Select(conn, table, opts)
	return DataLoadedMsg{Result: result, Error: err}
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return a.handleKey(msg)

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		return a, nil

	case DatabasesLoadedMsg:
		a.databases = msg.Databases
		if len(a.databases) > 0 {
			return a, a.loadTables
		}
		return a, nil

	case TablesLoadedMsg:
		if msg.Error != nil {
			a.err = msg.Error
		} else {
			a.tables = msg.Tables
			a.selectedTable = 0
			if len(a.tables) > 0 {
				return a, a.loadData
			}
		}
		return a, nil

	case DataLoadedMsg:
		if msg.Error != nil {
			a.err = msg.Error
		} else {
			a.data = msg.Result
			a.selectedRow = 0
		}
		return a, nil

	case QueryExecutedMsg:
		a.queryActive = false
		if msg.Error != nil {
			a.queryError = msg.Error
			a.queryResult = nil
		} else {
			a.queryError = nil
			a.queryResult = msg.Result
			a.data = msg.Result
			a.selectedRow = 0
		}
		return a, nil

	case ErrorMsg:
		a.err = msg.Error
		return a, nil
	}

	return a, nil
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle query input mode
	if a.queryActive {
		return a.handleQueryInput(msg)
	}

	// Handle help overlay
	if a.showHelp {
		if key.Matches(msg, a.keys.Back) || key.Matches(msg, a.keys.Help) {
			a.showHelp = false
		}
		return a, nil
	}

	// Handle schema modal
	if a.showSchema {
		if key.Matches(msg, a.keys.Back) {
			a.showSchema = false
		}
		return a, nil
	}

	switch {
	case key.Matches(msg, a.keys.Quit):
		return a, tea.Quit

	case key.Matches(msg, a.keys.Help):
		a.showHelp = true
		return a, nil

	case key.Matches(msg, a.keys.Query):
		a.queryActive = true
		a.queryInput = ""
		return a, nil

	case key.Matches(msg, a.keys.Refresh):
		return a, a.loadDatabases

	case key.Matches(msg, a.keys.NextPane):
		a.focus = (a.focus + 1) % 3
		return a, nil

	case key.Matches(msg, a.keys.PrevPane):
		a.focus = (a.focus + 2) % 3
		return a, nil

	case key.Matches(msg, a.keys.Left):
		if a.focus > 0 {
			a.focus--
		}
		return a, nil

	case key.Matches(msg, a.keys.Right):
		if a.focus < FocusData {
			a.focus++
		}
		return a, nil

	case key.Matches(msg, a.keys.Up):
		return a.handleUp()

	case key.Matches(msg, a.keys.Down):
		return a.handleDown()

	case key.Matches(msg, a.keys.Select):
		return a.handleSelect()

	case key.Matches(msg, a.keys.Schema):
		if a.focus == FocusTables && a.selectedTable < len(a.tables) {
			a.showSchema = true
			return a, a.loadSchema
		}
		return a, nil
	}

	return a, nil
}

func (a *App) handleUp() (tea.Model, tea.Cmd) {
	switch a.focus {
	case FocusDatabases:
		if a.selectedDB > 0 {
			a.selectedDB--
			return a, a.loadTables
		}
	case FocusTables:
		if a.selectedTable > 0 {
			a.selectedTable--
			return a, a.loadData
		}
	case FocusData:
		if a.selectedRow > 0 {
			a.selectedRow--
		}
	}
	return a, nil
}

func (a *App) handleDown() (tea.Model, tea.Cmd) {
	switch a.focus {
	case FocusDatabases:
		if a.selectedDB < len(a.databases)-1 {
			a.selectedDB++
			return a, a.loadTables
		}
	case FocusTables:
		if a.selectedTable < len(a.tables)-1 {
			a.selectedTable++
			return a, a.loadData
		}
	case FocusData:
		if a.data != nil && a.selectedRow < len(a.data.Rows)-1 {
			a.selectedRow++
		}
	}
	return a, nil
}

func (a *App) handleSelect() (tea.Model, tea.Cmd) {
	switch a.focus {
	case FocusDatabases:
		a.focus = FocusTables
		return a, a.loadTables
	case FocusTables:
		a.focus = FocusData
		return a, a.loadData
	}
	return a, nil
}

func (a *App) handleQueryInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		a.queryActive = false
		return a, nil

	case tea.KeyEnter:
		if a.queryInput != "" {
			return a, a.executeQuery
		}
		a.queryActive = false
		return a, nil

	case tea.KeyBackspace:
		if len(a.queryInput) > 0 {
			a.queryInput = a.queryInput[:len(a.queryInput)-1]
		}
		return a, nil

	case tea.KeyRunes:
		a.queryInput += string(msg.Runes)
		return a, nil

	case tea.KeySpace:
		a.queryInput += " "
		return a, nil
	}

	return a, nil
}

func (a *App) executeQuery() tea.Msg {
	if a.selectedDB >= len(a.databases) {
		return QueryExecutedMsg{Error: fmt.Errorf("no database selected")}
	}

	db := a.databases[a.selectedDB]
	result, err := a.dbManager.ExecuteQuery(db.Alias, a.user, "", a.queryInput)
	return QueryExecutedMsg{Result: result, Error: err}
}

func (a *App) loadSchema() tea.Msg {
	if a.selectedDB >= len(a.databases) || a.selectedTable >= len(a.tables) {
		return SchemaLoadedMsg{Error: fmt.Errorf("no table selected")}
	}

	db := a.databases[a.selectedDB]
	table := a.tables[a.selectedTable]

	conn, err := a.dbManager.OpenConnection(db.Alias, a.user)
	if err != nil {
		return SchemaLoadedMsg{Error: err}
	}

	schema := database.NewSchema(conn)
	info, err := schema.GetTableInfo(table)
	a.schema = info
	return SchemaLoadedMsg{Info: info, Error: err}
}

// View implements tea.Model.
func (a *App) View() string {
	if a.showHelp {
		return a.renderHelp()
	}

	if a.showSchema {
		return a.renderSchema()
	}

	// Calculate pane widths
	totalWidth := a.width
	dbWidth := totalWidth / 5
	tableWidth := totalWidth / 5
	dataWidth := totalWidth - dbWidth - tableWidth - 6 // account for borders

	// Calculate heights
	headerHeight := 2
	statusHeight := 2
	queryHeight := 3
	contentHeight := a.height - headerHeight - statusHeight - queryHeight

	// Build the view
	var b strings.Builder

	// Header
	header := titleStyle.Render("sqlite-tui") + "  " + dimItemStyle.Render(a.user.DisplayName())
	b.WriteString(header)
	b.WriteString("\n\n")

	// Main content - three panes
	dbPane := a.renderDatabasePane(dbWidth, contentHeight)
	tablePane := a.renderTablePane(tableWidth, contentHeight)
	dataPane := a.renderDataPane(dataWidth, contentHeight)

	content := lipgloss.JoinHorizontal(lipgloss.Top, dbPane, tablePane, dataPane)
	b.WriteString(content)
	b.WriteString("\n")

	// Query bar
	b.WriteString(a.renderQueryBar())
	b.WriteString("\n")

	// Status bar
	b.WriteString(a.renderStatusBar())

	return b.String()
}

func (a *App) renderDatabasePane(width, height int) string {
	style := paneStyle
	if a.focus == FocusDatabases {
		style = focusedPaneStyle
	}

	var content strings.Builder
	content.WriteString(paneHeaderStyle.Render("Databases"))
	content.WriteString("\n")

	for i, db := range a.databases {
		item := db.Alias
		if i == a.selectedDB {
			item = selectedItemStyle.Render("> " + item)
		} else {
			item = normalItemStyle.Render("  " + item)
		}
		content.WriteString(item)
		content.WriteString("\n")
	}

	if len(a.databases) == 0 {
		content.WriteString(dimItemStyle.Render("  No databases"))
	}

	return style.Width(width).Height(height).Render(content.String())
}

func (a *App) renderTablePane(width, height int) string {
	style := paneStyle
	if a.focus == FocusTables {
		style = focusedPaneStyle
	}

	var content strings.Builder
	content.WriteString(paneHeaderStyle.Render("Tables"))
	content.WriteString("\n")

	for i, table := range a.tables {
		item := table
		if i == a.selectedTable {
			item = selectedItemStyle.Render("> " + item)
		} else {
			item = normalItemStyle.Render("  " + item)
		}
		content.WriteString(item)
		content.WriteString("\n")
	}

	if len(a.tables) == 0 {
		content.WriteString(dimItemStyle.Render("  No tables"))
	}

	return style.Width(width).Height(height).Render(content.String())
}

func (a *App) renderDataPane(width, height int) string {
	style := paneStyle
	if a.focus == FocusData {
		style = focusedPaneStyle
	}

	var content strings.Builder

	if a.data == nil || len(a.data.Columns) == 0 {
		content.WriteString(dimItemStyle.Render("No data"))
		return style.Width(width).Height(height).Render(content.String())
	}

	// Header
	header := strings.Join(a.data.Columns, " | ")
	content.WriteString(tableHeaderStyle.Render(header))
	content.WriteString("\n")

	// Rows
	for i, row := range a.data.Rows {
		var cells []string
		for _, v := range row {
			cells = append(cells, database.FormatValue(v))
		}
		line := strings.Join(cells, " | ")

		if i == a.selectedRow {
			line = tableSelectedRowStyle.Render(line)
		}
		content.WriteString(line)
		content.WriteString("\n")
	}

	return style.Width(width).Height(height).Render(content.String())
}

func (a *App) renderQueryBar() string {
	prompt := queryPromptStyle.Render("SQL> ")
	if a.queryActive {
		return prompt + queryInputStyle.Render(a.queryInput+"█")
	}
	if a.queryError != nil {
		return prompt + errorStyle.Render(a.queryError.Error())
	}
	return prompt + dimItemStyle.Render("Press / to query")
}

func (a *App) renderStatusBar() string {
	var parts []string

	// Current database/table
	if a.selectedDB < len(a.databases) {
		db := a.databases[a.selectedDB]
		parts = append(parts, statusKeyStyle.Render(db.Alias))
	}
	if a.selectedTable < len(a.tables) {
		parts = append(parts, statusValueStyle.Render("> "+a.tables[a.selectedTable]))
	}

	// Row count
	if a.data != nil {
		parts = append(parts, dimItemStyle.Render(fmt.Sprintf("| %d rows", len(a.data.Rows))))
	}

	// Access level
	if a.selectedDB < len(a.databases) {
		db := a.databases[a.selectedDB]
		var badge string
		switch db.AccessLevel.String() {
		case "admin":
			badge = adminBadge.Render("ADMIN")
		case "read-write":
			badge = readWriteBadge.Render("RW")
		case "read-only":
			badge = readOnlyBadge.Render("RO")
		default:
			badge = noBadge.Render("NO")
		}
		parts = append(parts, badge)
	}

	// Help hint
	parts = append(parts, dimItemStyle.Render("| ?:help q:quit"))

	return statusBarStyle.Width(a.width).Render(strings.Join(parts, " "))
}

func (a *App) renderHelp() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Help"))
	b.WriteString("\n\n")

	bindings := []struct {
		key  string
		desc string
	}{
		{"↑/k, ↓/j", "Navigate items"},
		{"←/h, →/l", "Switch panes"},
		{"Tab", "Next pane"},
		{"Enter", "Select"},
		{"/", "Query mode"},
		{"s", "Show schema"},
		{"r", "Refresh"},
		{"?", "Toggle help"},
		{"q, Ctrl+C", "Quit"},
	}

	for _, b2 := range bindings {
		line := helpKeyStyle.Render(fmt.Sprintf("%-12s", b2.key)) + helpDescStyle.Render(b2.desc)
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(dimItemStyle.Render("Press ? or Esc to close"))

	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, b.String())
}

func (a *App) renderSchema() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Schema"))
	b.WriteString("\n\n")

	if a.schema == nil {
		b.WriteString(dimItemStyle.Render("Loading..."))
	} else {
		b.WriteString(paneHeaderStyle.Render(a.schema.Name))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("Rows: %d\n\n", a.schema.RowCount))

		b.WriteString(tableHeaderStyle.Render("Column | Type | PK | NotNull"))
		b.WriteString("\n")

		for _, col := range a.schema.Columns {
			pk := ""
			if col.PrimaryKey > 0 {
				pk = "✓"
			}
			nn := ""
			if col.NotNull {
				nn = "✓"
			}
			b.WriteString(fmt.Sprintf("%s | %s | %s | %s\n", col.Name, col.Type, pk, nn))
		}
	}

	b.WriteString("\n")
	b.WriteString(dimItemStyle.Render("Press Esc to close"))

	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, b.String())
}

