package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/table"
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
)

const (
	pageSize = 50 // rows per page
)

// listItem implements list.Item for bubbles/list
type listItem struct {
	title string
	desc  string
}

func (i listItem) Title() string       { return i.title }
func (i listItem) Description() string { return i.desc }
func (i listItem) FilterValue() string { return i.title }

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

	// Data state
	dataTable    table.Model
	dataColumns  []string
	dataRows     [][]any
	totalRows    int64
	loadedOffset int
	selectedRow  int

	// Lists
	dbList    list.Model
	tableList list.Model

	// Schema
	schema *database.TableInfo

	// Query input
	queryInput  string
	queryActive bool
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
	// Create database list
	dbDelegate := list.NewDefaultDelegate()
	dbDelegate.ShowDescription = false
	dbDelegate.SetHeight(1)
	dbList := list.New([]list.Item{}, dbDelegate, width/5, height-10)
	dbList.Title = "Databases"
	dbList.SetShowStatusBar(false)
	dbList.SetFilteringEnabled(false)
	dbList.SetShowHelp(false)
	dbList.Styles.Title = paneHeaderStyle

	// Create table list
	tableDelegate := list.NewDefaultDelegate()
	tableDelegate.ShowDescription = false
	tableDelegate.SetHeight(1)
	tableList := list.New([]list.Item{}, tableDelegate, width/5, height-10)
	tableList.Title = "Tables"
	tableList.SetShowStatusBar(false)
	tableList.SetFilteringEnabled(false)
	tableList.SetShowHelp(false)
	tableList.Styles.Title = paneHeaderStyle

	// Create data table
	dataTable := table.New(
		table.WithColumns([]table.Column{}),
		table.WithRows([]table.Row{}),
		table.WithFocused(false),
		table.WithHeight(height-10),
	)
	dataTable.SetStyles(table.Styles{
		Header:   tableHeaderStyle,
		Cell:     tableCellStyle,
		Selected: tableSelectedRowStyle,
	})

	app := &App{
		dbManager:    dbManager,
		historyStore: historyStore,
		user:         user,
		width:        width,
		height:       height,
		focus:        FocusDatabases,
		keys:         DefaultKeyMap(),
		dbList:       dbList,
		tableList:    tableList,
		dataTable:    dataTable,
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
	tableName := a.tables[a.selectedTable]

	conn, err := a.dbManager.OpenConnection(db.Alias, a.user)
	if err != nil {
		return DataLoadedMsg{Error: err}
	}

	// Get total row count
	schema := database.NewSchema(conn)
	totalRows, err := schema.GetRowCount(tableName)
	if err != nil {
		return DataLoadedMsg{Error: err}
	}

	// Load first page
	opts := database.DefaultSelectOptions()
	opts.Limit = pageSize
	opts.Offset = 0
	result, err := database.Select(conn, tableName, opts)

	return DataLoadedMsg{
		Result:    result,
		TotalRows: totalRows,
		Offset:    0,
		Error:     err,
	}
}

// loadMoreData loads additional rows.
func (a *App) loadMoreData(offset int) tea.Cmd {
	return func() tea.Msg {
		if a.selectedDB >= len(a.databases) || a.selectedTable >= len(a.tables) {
			return MoreDataLoadedMsg{Error: fmt.Errorf("no table selected")}
		}

		db := a.databases[a.selectedDB]
		tableName := a.tables[a.selectedTable]

		conn, err := a.dbManager.OpenConnection(db.Alias, a.user)
		if err != nil {
			return MoreDataLoadedMsg{Error: err}
		}

		opts := database.DefaultSelectOptions()
		opts.Limit = pageSize
		opts.Offset = offset
		result, err := database.Select(conn, tableName, opts)

		return MoreDataLoadedMsg{
			Result: result,
			Offset: offset,
			Error:  err,
		}
	}
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return a.handleKey(msg)

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.updateSizes()
		return a, nil

	case DatabasesLoadedMsg:
		a.databases = msg.Databases
		a.selectedDB = 0
		a.updateDBList()
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
			a.updateTableList()
			if len(a.tables) > 0 {
				return a, a.loadData
			}
		}
		return a, nil

	case DataLoadedMsg:
		if msg.Error != nil {
			a.err = msg.Error
		} else {
			a.dataColumns = msg.Result.Columns
			a.dataRows = msg.Result.Rows
			a.totalRows = msg.TotalRows
			a.loadedOffset = 0
			a.selectedRow = 0
			a.updateDataTable()
		}
		return a, nil

	case MoreDataLoadedMsg:
		if msg.Error != nil {
			a.err = msg.Error
		} else if msg.Result != nil && len(msg.Result.Rows) > 0 {
			// Append new rows
			a.dataRows = append(a.dataRows, msg.Result.Rows...)
			a.loadedOffset = msg.Offset
			a.updateDataTable()
		}
		return a, nil

	case QueryExecutedMsg:
		a.queryActive = false
		if msg.Error != nil {
			a.queryError = msg.Error
		} else {
			a.queryError = nil
			a.dataColumns = msg.Result.Columns
			a.dataRows = msg.Result.Rows
			a.totalRows = int64(len(msg.Result.Rows))
			a.selectedRow = 0
			a.updateDataTable()
		}
		return a, nil

	case ErrorMsg:
		a.err = msg.Error
		return a, nil
	}

	// Update focused component
	switch a.focus {
	case FocusDatabases:
		var cmd tea.Cmd
		a.dbList, cmd = a.dbList.Update(msg)
		cmds = append(cmds, cmd)
	case FocusTables:
		var cmd tea.Cmd
		a.tableList, cmd = a.tableList.Update(msg)
		cmds = append(cmds, cmd)
	case FocusData:
		var cmd tea.Cmd
		a.dataTable, cmd = a.dataTable.Update(msg)
		cmds = append(cmds, cmd)
	}

	return a, tea.Batch(cmds...)
}

func (a *App) updateSizes() {
	contentHeight := a.height - 7 // header + status + query
	listWidth := a.width / 5
	if listWidth < 15 {
		listWidth = 15
	}
	dataWidth := a.width - listWidth*2 - 6

	a.dbList.SetSize(listWidth, contentHeight)
	a.tableList.SetSize(listWidth, contentHeight)
	a.dataTable.SetHeight(contentHeight - 2)
	a.dataTable.SetWidth(dataWidth)
}

func (a *App) updateDBList() {
	items := make([]list.Item, len(a.databases))
	for i, db := range a.databases {
		items[i] = listItem{title: db.Alias}
	}
	a.dbList.SetItems(items)
}

func (a *App) updateTableList() {
	items := make([]list.Item, len(a.tables))
	for i, t := range a.tables {
		items[i] = listItem{title: t}
	}
	a.tableList.SetItems(items)
}

func (a *App) updateDataTable() {
	if len(a.dataColumns) == 0 {
		a.dataTable.SetColumns([]table.Column{})
		a.dataTable.SetRows([]table.Row{})
		return
	}

	// Calculate column widths
	dataWidth := a.width - (a.width/5)*2 - 10
	colWidth := dataWidth / len(a.dataColumns)
	if colWidth < 8 {
		colWidth = 8
	}
	if colWidth > 30 {
		colWidth = 30
	}

	columns := make([]table.Column, len(a.dataColumns))
	for i, col := range a.dataColumns {
		columns[i] = table.Column{
			Title: col,
			Width: colWidth,
		}
	}

	rows := make([]table.Row, len(a.dataRows))
	for i, row := range a.dataRows {
		cells := make([]string, len(row))
		for j, v := range row {
			cells[j] = truncateString(database.FormatValue(v), colWidth)
		}
		rows[i] = cells
	}

	a.dataTable.SetColumns(columns)
	a.dataTable.SetRows(rows)
	a.dataTable.SetCursor(a.selectedRow)
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
		a.updateFocus()
		return a, nil

	case key.Matches(msg, a.keys.PrevPane):
		a.focus = (a.focus + 2) % 3
		a.updateFocus()
		return a, nil

	case key.Matches(msg, a.keys.Left):
		if a.focus > 0 {
			a.focus--
			a.updateFocus()
		}
		return a, nil

	case key.Matches(msg, a.keys.Right):
		if a.focus < FocusData {
			a.focus++
			a.updateFocus()
		}
		return a, nil

	case key.Matches(msg, a.keys.Up):
		return a.handleUp()

	case key.Matches(msg, a.keys.Down):
		return a.handleDown()

	case key.Matches(msg, a.keys.PageUp):
		return a.handlePageUp()

	case key.Matches(msg, a.keys.PageDown):
		return a.handlePageDown()

	case key.Matches(msg, a.keys.Home):
		return a.handleHome()

	case key.Matches(msg, a.keys.End):
		return a.handleEnd()

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

func (a *App) updateFocus() {
	a.dataTable.Blur()
	switch a.focus {
	case FocusData:
		a.dataTable.Focus()
	}
}

func (a *App) handleUp() (tea.Model, tea.Cmd) {
	switch a.focus {
	case FocusDatabases:
		if a.dbList.Index() > 0 {
			a.dbList.CursorUp()
			a.selectedDB = a.dbList.Index()
			return a, a.loadTables
		}
	case FocusTables:
		if a.tableList.Index() > 0 {
			a.tableList.CursorUp()
			a.selectedTable = a.tableList.Index()
			return a, a.loadData
		}
	case FocusData:
		if a.selectedRow > 0 {
			a.selectedRow--
			a.dataTable.SetCursor(a.selectedRow)
		}
	}
	return a, nil
}

func (a *App) handleDown() (tea.Model, tea.Cmd) {
	switch a.focus {
	case FocusDatabases:
		if a.dbList.Index() < len(a.databases)-1 {
			a.dbList.CursorDown()
			a.selectedDB = a.dbList.Index()
			return a, a.loadTables
		}
	case FocusTables:
		if a.tableList.Index() < len(a.tables)-1 {
			a.tableList.CursorDown()
			a.selectedTable = a.tableList.Index()
			return a, a.loadData
		}
	case FocusData:
		if a.selectedRow < len(a.dataRows)-1 {
			a.selectedRow++
			a.dataTable.SetCursor(a.selectedRow)
			// Load more if near end
			if a.selectedRow >= len(a.dataRows)-5 && int64(len(a.dataRows)) < a.totalRows {
				return a, a.loadMoreData(len(a.dataRows))
			}
		} else if int64(len(a.dataRows)) < a.totalRows {
			// At end but more rows exist - load them
			return a, a.loadMoreData(len(a.dataRows))
		}
	}
	return a, nil
}

func (a *App) handlePageUp() (tea.Model, tea.Cmd) {
	pageSize := 10
	switch a.focus {
	case FocusDatabases:
		for i := 0; i < pageSize && a.dbList.Index() > 0; i++ {
			a.dbList.CursorUp()
		}
		a.selectedDB = a.dbList.Index()
		return a, a.loadTables
	case FocusTables:
		for i := 0; i < pageSize && a.tableList.Index() > 0; i++ {
			a.tableList.CursorUp()
		}
		a.selectedTable = a.tableList.Index()
		return a, a.loadData
	case FocusData:
		a.selectedRow -= pageSize
		if a.selectedRow < 0 {
			a.selectedRow = 0
		}
		a.dataTable.SetCursor(a.selectedRow)
	}
	return a, nil
}

func (a *App) handlePageDown() (tea.Model, tea.Cmd) {
	pageSize := 10
	switch a.focus {
	case FocusDatabases:
		for i := 0; i < pageSize && a.dbList.Index() < len(a.databases)-1; i++ {
			a.dbList.CursorDown()
		}
		a.selectedDB = a.dbList.Index()
		return a, a.loadTables
	case FocusTables:
		for i := 0; i < pageSize && a.tableList.Index() < len(a.tables)-1; i++ {
			a.tableList.CursorDown()
		}
		a.selectedTable = a.tableList.Index()
		return a, a.loadData
	case FocusData:
		a.selectedRow += pageSize
		if a.selectedRow >= len(a.dataRows) {
			a.selectedRow = len(a.dataRows) - 1
		}
		if a.selectedRow < 0 {
			a.selectedRow = 0
		}
		a.dataTable.SetCursor(a.selectedRow)
		// Load more if needed
		if int64(len(a.dataRows)) < a.totalRows && a.selectedRow >= len(a.dataRows)-5 {
			return a, a.loadMoreData(len(a.dataRows))
		}
	}
	return a, nil
}

func (a *App) handleHome() (tea.Model, tea.Cmd) {
	switch a.focus {
	case FocusDatabases:
		a.dbList.Select(0)
		a.selectedDB = 0
		return a, a.loadTables
	case FocusTables:
		a.tableList.Select(0)
		a.selectedTable = 0
		return a, a.loadData
	case FocusData:
		a.selectedRow = 0
		a.dataTable.SetCursor(0)
	}
	return a, nil
}

func (a *App) handleEnd() (tea.Model, tea.Cmd) {
	switch a.focus {
	case FocusDatabases:
		if len(a.databases) > 0 {
			a.dbList.Select(len(a.databases) - 1)
			a.selectedDB = len(a.databases) - 1
			return a, a.loadTables
		}
	case FocusTables:
		if len(a.tables) > 0 {
			a.tableList.Select(len(a.tables) - 1)
			a.selectedTable = len(a.tables) - 1
			return a, a.loadData
		}
	case FocusData:
		// Jump to end - may need to load more
		if int64(len(a.dataRows)) < a.totalRows {
			// Need to load all remaining - for now just load next batch
			return a, a.loadMoreData(len(a.dataRows))
		}
		a.selectedRow = len(a.dataRows) - 1
		if a.selectedRow < 0 {
			a.selectedRow = 0
		}
		a.dataTable.SetCursor(a.selectedRow)
	}
	return a, nil
}

func (a *App) handleSelect() (tea.Model, tea.Cmd) {
	switch a.focus {
	case FocusDatabases:
		a.focus = FocusTables
		a.updateFocus()
		return a, a.loadTables
	case FocusTables:
		a.focus = FocusData
		a.updateFocus()
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
	tableName := a.tables[a.selectedTable]

	conn, err := a.dbManager.OpenConnection(db.Alias, a.user)
	if err != nil {
		return SchemaLoadedMsg{Error: err}
	}

	schema := database.NewSchema(conn)
	info, err := schema.GetTableInfo(tableName)
	a.schema = info
	return SchemaLoadedMsg{Info: info, Error: err}
}

// View implements tea.Model.
func (a *App) View() string {
	if a.width < 40 || a.height < 10 {
		return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center,
			errorStyle.Render("Terminal too small\nMin: 40x10"))
	}

	if a.showHelp {
		return a.renderHelp()
	}

	if a.showSchema {
		return a.renderSchema()
	}

	// Calculate pane widths
	listWidth := a.width / 5
	if listWidth < 15 {
		listWidth = 15
	}
	dataWidth := a.width - listWidth*2 - 6
	contentHeight := a.height - 7

	var b strings.Builder

	// Header
	header := titleStyle.Render("sqlite-tui") + "  " + dimItemStyle.Render(a.user.DisplayName())
	b.WriteString(header)
	b.WriteString("\n\n")

	// Main content - three panes
	dbPane := a.renderDBPane(listWidth, contentHeight)
	tablePane := a.renderTablePane(listWidth, contentHeight)
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

func (a *App) renderDBPane(width, height int) string {
	style := paneStyle
	if a.focus == FocusDatabases {
		style = focusedPaneStyle
	}

	// Render list content manually for consistent styling
	var content strings.Builder
	content.WriteString(paneHeaderStyle.Render("Databases"))
	content.WriteString("\n")

	visibleHeight := height - 4
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	if len(a.databases) == 0 {
		content.WriteString(dimItemStyle.Render("  No databases"))
	} else {
		// Calculate scroll offset
		offset := 0
		if a.selectedDB >= visibleHeight {
			offset = a.selectedDB - visibleHeight + 1
		}
		end := offset + visibleHeight
		if end > len(a.databases) {
			end = len(a.databases)
		}

		if offset > 0 {
			content.WriteString(dimItemStyle.Render("  ↑ more\n"))
			visibleHeight--
			end = offset + visibleHeight
			if end > len(a.databases) {
				end = len(a.databases)
			}
		}

		for i := offset; i < end; i++ {
			db := a.databases[i]
			item := truncateString(db.Alias, width-6)
			if i == a.selectedDB {
				item = selectedItemStyle.Render("> " + item)
			} else {
				item = normalItemStyle.Render("  " + item)
			}
			content.WriteString(item)
			content.WriteString("\n")
		}

		if end < len(a.databases) {
			content.WriteString(dimItemStyle.Render("  ↓ more"))
		}
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

	visibleHeight := height - 4
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	if len(a.tables) == 0 {
		content.WriteString(dimItemStyle.Render("  No tables"))
	} else {
		offset := 0
		if a.selectedTable >= visibleHeight {
			offset = a.selectedTable - visibleHeight + 1
		}
		end := offset + visibleHeight
		if end > len(a.tables) {
			end = len(a.tables)
		}

		if offset > 0 {
			content.WriteString(dimItemStyle.Render("  ↑ more\n"))
			visibleHeight--
			end = offset + visibleHeight
			if end > len(a.tables) {
				end = len(a.tables)
			}
		}

		for i := offset; i < end; i++ {
			item := truncateString(a.tables[i], width-6)
			if i == a.selectedTable {
				item = selectedItemStyle.Render("> " + item)
			} else {
				item = normalItemStyle.Render("  " + item)
			}
			content.WriteString(item)
			content.WriteString("\n")
		}

		if end < len(a.tables) {
			content.WriteString(dimItemStyle.Render("  ↓ more"))
		}
	}

	return style.Width(width).Height(height).Render(content.String())
}

func (a *App) renderDataPane(width, height int) string {
	style := paneStyle
	if a.focus == FocusData {
		style = focusedPaneStyle
	}

	if len(a.dataColumns) == 0 {
		return style.Width(width).Height(height).Render(dimItemStyle.Render("No data"))
	}

	// Use bubbles table
	tableView := a.dataTable.View()

	// Add loading indicator if more rows available
	var footer string
	if int64(len(a.dataRows)) < a.totalRows {
		footer = dimItemStyle.Render(fmt.Sprintf("\n↓ %d more rows (scroll to load)", a.totalRows-int64(len(a.dataRows))))
	}

	return style.Width(width).Height(height).Render(tableView + footer)
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

	// Row count - show actual total
	if len(a.dataRows) > 0 {
		parts = append(parts, dimItemStyle.Render(fmt.Sprintf("| row %d/%d", a.selectedRow+1, a.totalRows)))
		if int64(len(a.dataRows)) < a.totalRows {
			parts = append(parts, dimItemStyle.Render(fmt.Sprintf("(loaded %d)", len(a.dataRows))))
		}
	} else if a.totalRows > 0 {
		parts = append(parts, dimItemStyle.Render(fmt.Sprintf("| %d rows", a.totalRows)))
	}

	// Access level badge
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

	parts = append(parts, dimItemStyle.Render("| ?:help q:quit"))

	return statusBarStyle.Width(a.width).Render(strings.Join(parts, " "))
}

func (a *App) renderHelp() string {
	var b strings.Builder

	bindings := []struct {
		key  string
		desc string
	}{
		{"↑/k, ↓/j", "Navigate items"},
		{"←/h, →/l", "Switch panes"},
		{"PgUp/^U", "Page up"},
		{"PgDn/^D", "Page down"},
		{"Home/g", "Go to top"},
		{"End/G", "Go to bottom"},
		{"Tab", "Next pane"},
		{"Enter", "Select"},
		{"/", "Query mode"},
		{"s", "Show schema"},
		{"r", "Refresh"},
		{"?", "Toggle help"},
		{"q, Ctrl+C", "Quit"},
	}

	for _, binding := range bindings {
		b.WriteString(helpKeyStyle.Render(fmt.Sprintf("%-12s", binding.key)))
		b.WriteString(helpDescStyle.Render(binding.desc))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(dimItemStyle.Render("Press ? or Esc to close"))

	modal := modalStyle.Render(titleStyle.Render("Help") + "\n\n" + b.String())
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, modal)
}

func (a *App) renderSchema() string {
	var b strings.Builder

	if a.schema == nil {
		b.WriteString(dimItemStyle.Render("Loading..."))
	} else {
		b.WriteString(paneHeaderStyle.Render(a.schema.Name))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("Rows: %d\n\n", a.schema.RowCount))

		nameW, typeW := 6, 4
		for _, col := range a.schema.Columns {
			if len(col.Name) > nameW {
				nameW = len(col.Name)
			}
			if len(col.Type) > typeW {
				typeW = len(col.Type)
			}
		}

		b.WriteString(tableHeaderStyle.Render(fmt.Sprintf("%-*s  %-*s  PK  NotNull", nameW, "Column", typeW, "Type")))
		b.WriteString("\n")

		for _, col := range a.schema.Columns {
			pk := "  "
			if col.PrimaryKey > 0 {
				pk = "✓ "
			}
			nn := "  "
			if col.NotNull {
				nn = "✓"
			}
			b.WriteString(fmt.Sprintf("%-*s  %-*s  %s  %s\n", nameW, col.Name, typeW, col.Type, pk, nn))
		}
	}

	b.WriteString("\n")
	b.WriteString(dimItemStyle.Render("Press Esc to close"))

	modal := modalStyle.Render(titleStyle.Render("Schema") + "\n\n" + b.String())
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, modal)
}

// truncateString truncates a string to maxLen, adding ellipsis if needed
func truncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}
