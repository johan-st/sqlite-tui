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

	// Column scrolling
	colOffset   int // first visible column index
	visibleCols int // number of columns that fit in viewport

	// Table viewport
	tableDataRows int // number of data rows visible in table (excludes header)

	// Cell editing
	editingCell   bool
	editCellCol   int
	editCellRow   int
	editCellValue string
	editError     error

	// Lists
	dbList    list.Model
	tableList list.Model

	// Schema
	schema *database.TableInfo

	// Query input
	queryInput  string
	queryActive bool
	queryError  error

	// Query history
	queryHistory      []string // cached query strings (most recent first)
	queryHistoryIdx   int      // -1 = current input, 0+ = history index
	queryHistoryDraft string   // saves current input when navigating history

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
			a.updateTableHeight()
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
			a.updateTableHeight()
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
			a.updateTableHeight()
		}
		return a, nil

	case ErrorMsg:
		a.err = msg.Error
		return a, nil

	case QueryHistoryLoadedMsg:
		if msg.Queries != nil {
			a.queryHistory = msg.Queries
		}
		return a, nil

	case CellUpdatedMsg:
		a.editingCell = false
		if msg.Error != nil {
			a.editError = msg.Error
		} else {
			a.editError = nil
			a.updateDataTable()
		}
		a.updateTableHeight()
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

// updateTableHeight recalculates and updates the table height based on current indicators
func (a *App) updateTableHeight() {
	contentHeight := a.height - 2 // query (1) + status (1)

	// Pane inner height = contentHeight - 2 (top and bottom borders)
	paneInnerHeight := contentHeight - 2
	if paneInnerHeight < 1 {
		paneInnerHeight = 1
	}

	// Calculate indicators that come BEFORE the table (affect table height)
	indicatorsBeforeTable := 0

	// Column scroll indicator (rendered before table)
	totalCols := len(a.dataColumns)
	endCol := a.colOffset + a.visibleCols
	if endCol > totalCols {
		endCol = totalCols
	}
	if a.colOffset > 0 || endCol < totalCols {
		indicatorsBeforeTable++
	}

	// Edit mode indicator (rendered before table)
	if a.editingCell || a.editError != nil {
		indicatorsBeforeTable++
	}

	// Calculate maximum available height for table within the pane
	maxTableHeight := paneInnerHeight - indicatorsBeforeTable
	if maxTableHeight < 1 {
		maxTableHeight = 1
	}

	// First, check if we need "rows below" indicator using maximum table height
	dataRowsAvailable := maxTableHeight - 1 // subtract header row
	if dataRowsAvailable < 1 {
		dataRowsAvailable = 1
	}
	scrollOffset := a.selectedRow - dataRowsAvailable + 1
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	lastVisible := scrollOffset + dataRowsAvailable - 1
	if lastVisible >= len(a.dataRows) {
		lastVisible = len(a.dataRows) - 1
	}
	// When selectedRow is the last loaded row, lastVisible should be selectedRow
	if a.selectedRow == len(a.dataRows)-1 && len(a.dataRows) > 0 {
		lastVisible = a.selectedRow
	}

	// Calculate if we need to show "rows below" indicator
	showRowsBelowIndicator := false
	if len(a.dataRows) > 0 {
		if int64(len(a.dataRows)) < a.totalRows {
			// Not all rows loaded - check against totalRows
			rowsBelow := a.totalRows - int64(lastVisible) - 1
			if rowsBelow > 0 {
				showRowsBelowIndicator = true
			}
		} else {
			// All rows loaded - only show indicator if we can't see the last row
			maxLoadedRowIndex := len(a.dataRows) - 1
			if lastVisible < maxLoadedRowIndex {
				showRowsBelowIndicator = true
			}
		}
	}

	// Set table height: reduce by 1 if we need to show the indicator
	tableHeight := maxTableHeight
	if showRowsBelowIndicator {
		tableHeight--
		if tableHeight < 1 {
			tableHeight = 1
		}
	}

	a.dataTable.SetHeight(tableHeight)
	a.tableDataRows = tableHeight - 1
	if a.tableDataRows < 1 {
		a.tableDataRows = 1
	}
}

// calculateTableHeight calculates the available height for the table based on indicators
// This is used for initial sizing in updateSizes
func (a *App) calculateTableHeight(contentHeight int) int {
	// Pane inner height = contentHeight - 2 (borders)
	// Conservative estimate: subtract 2 more for potential indicators
	return contentHeight - 2 - 2
}

func (a *App) updateSizes() {
	contentHeight := a.height - 2 // query (1) + status (1)

	// Calculate panel widths based on content
	dbWidth := a.calculateDBPaneWidth()
	tableWidth := a.calculateTablePaneWidth()

	// Cap panel widths to reasonable maximum (1/3 of screen each)
	maxPanelWidth := a.width / 3
	if dbWidth > maxPanelWidth {
		dbWidth = maxPanelWidth
	}
	if tableWidth > maxPanelWidth {
		tableWidth = maxPanelWidth
	}

	// Minimum widths
	if dbWidth < 15 {
		dbWidth = 15
	}
	if tableWidth < 12 {
		tableWidth = 12
	}

	dataWidth := a.width - dbWidth - tableWidth - 2 // -2 for gaps between panes

	a.dbList.SetSize(dbWidth, contentHeight)
	a.tableList.SetSize(tableWidth, contentHeight)

	// Calculate table height accounting for indicators
	tableHeight := a.calculateTableHeight(contentHeight)
	a.dataTable.SetHeight(tableHeight)
	a.tableDataRows = tableHeight - 1   // subtract header row
	a.dataTable.SetWidth(dataWidth - 4) // account for pane padding

	// Update to accurate height based on current state
	a.updateTableHeight()

	// Calculate how many columns fit in viewport
	// Each column uses: colWidth + 1 (gap between columns)
	const minColWidth = 8
	availableWidth := dataWidth - 4 // borders + padding
	a.visibleCols = availableWidth / (minColWidth + 1)
	if a.visibleCols < 1 {
		a.visibleCols = 1
	}
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

	totalCols := len(a.dataColumns)

	// Clamp colOffset to valid range
	if a.colOffset < 0 {
		a.colOffset = 0
	}
	if a.colOffset >= totalCols {
		a.colOffset = totalCols - 1
	}

	// Determine which columns to show
	endCol := a.colOffset + a.visibleCols
	if endCol > totalCols {
		endCol = totalCols
	}
	visibleColCount := endCol - a.colOffset
	if visibleColCount < 1 {
		visibleColCount = 1
		endCol = a.colOffset + 1
		if endCol > totalCols {
			endCol = totalCols
			a.colOffset = totalCols - 1
		}
	}

	// Calculate available width for the dataview
	dataWidth := a.width - (a.width/5)*2 - 10
	maxColWidth := dataWidth // max width per column is the full dataview width

	// Calculate content width for each visible column
	columnWidths := make([]int, visibleColCount)
	for i := 0; i < visibleColCount; i++ {
		srcIdx := a.colOffset + i

		// Start with column header width
		maxWidth := len(a.dataColumns[srcIdx])

		// Check all cell values in this column
		for _, row := range a.dataRows {
			if srcIdx < len(row) {
				cellValue := database.FormatValue(row[srcIdx])
				if len(cellValue) > maxWidth {
					maxWidth = len(cellValue)
				}
			}
		}

		// Cap at maxColWidth
		if maxWidth > maxColWidth {
			maxWidth = maxColWidth
		}

		// Minimum width of 8
		if maxWidth < 8 {
			maxWidth = 8
		}

		columnWidths[i] = maxWidth
	}

	columns := make([]table.Column, visibleColCount)
	for i := 0; i < visibleColCount; i++ {
		srcIdx := a.colOffset + i
		colWidth := columnWidths[i]
		columns[i] = table.Column{
			Title: truncateString(a.dataColumns[srcIdx], colWidth-2),
			Width: colWidth,
		}
	}

	rows := make([]table.Row, len(a.dataRows))
	for i, row := range a.dataRows {
		cells := make([]string, visibleColCount)
		for j := 0; j < visibleColCount; j++ {
			srcIdx := a.colOffset + j
			if srcIdx < len(row) {
				colWidth := columnWidths[j]
				cells[j] = truncateString(database.FormatValue(row[srcIdx]), colWidth-2)
			} else {
				cells[j] = ""
			}
		}
		rows[i] = cells
	}

	// Must set rows before columns to avoid index panic in bubbles/table
	a.dataTable.SetRows([]table.Row{}) // clear first
	a.dataTable.SetColumns(columns)
	a.dataTable.SetRows(rows)
	if a.selectedRow < len(rows) {
		a.dataTable.SetCursor(a.selectedRow)
	} else if len(rows) > 0 {
		a.dataTable.SetCursor(0)
		a.selectedRow = 0
	}
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle cell editing mode
	if a.editingCell {
		return a.handleEditInput(msg)
	}

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
		a.queryHistoryIdx = -1
		a.queryHistoryDraft = ""
		return a, a.loadQueryHistory

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
		if a.focus == FocusData {
			// Scroll columns left, or move to Tables panel if at leftmost
			if a.colOffset > 0 {
				a.colOffset--
				a.updateDataTable()
				a.updateTableHeight()
			} else {
				// At leftmost column - move to Tables panel
				a.focus = FocusTables
				a.updateFocus()
			}
		} else if a.focus > 0 {
			a.focus--
			a.updateFocus()
		}
		return a, nil

	case key.Matches(msg, a.keys.Right):
		if a.focus == FocusData {
			// Scroll columns right
			if a.colOffset < len(a.dataColumns)-1 {
				a.colOffset++
				a.updateDataTable()
				a.updateTableHeight()
			}
		} else if a.focus < FocusData {
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

	case key.Matches(msg, a.keys.Edit):
		return a.handleEditCell()

	case key.Matches(msg, a.keys.Schema):
		if (a.focus == FocusTables || a.focus == FocusData) && a.selectedTable < len(a.tables) {
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
			a.updateTableHeight()
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
			a.updateTableHeight()
			// Load more if near end
			if a.selectedRow >= len(a.dataRows)-5 && int64(len(a.dataRows)) < a.totalRows {
				return a, a.loadMoreData(len(a.dataRows))
			}
		} else if int64(len(a.dataRows)) < a.totalRows {
			// At end but more rows exist - load them
			return a, a.loadMoreData(len(a.dataRows))
		} else {
			a.updateTableHeight()
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
		a.updateTableHeight()
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
		a.updateTableHeight()
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
		a.updateTableHeight()
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
		a.updateTableHeight()
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
		a.queryHistoryIdx = -1
		return a, nil

	case tea.KeyEnter:
		if a.queryInput != "" {
			query := a.queryInput
			// Add to history cache (prepend, avoid duplicates)
			if len(a.queryHistory) == 0 || a.queryHistory[0] != query {
				a.queryHistory = append([]string{query}, a.queryHistory...)
				if len(a.queryHistory) > 100 {
					a.queryHistory = a.queryHistory[:100]
				}
			}
			a.queryHistoryIdx = -1
			return a, a.executeQuery
		}
		a.queryActive = false
		return a, nil

	case tea.KeyUp:
		// Navigate to older query in history
		if len(a.queryHistory) > 0 && a.queryHistoryIdx < len(a.queryHistory)-1 {
			if a.queryHistoryIdx == -1 {
				// Save current input as draft
				a.queryHistoryDraft = a.queryInput
			}
			a.queryHistoryIdx++
			a.queryInput = a.queryHistory[a.queryHistoryIdx]
		}
		return a, nil

	case tea.KeyDown:
		// Navigate to newer query in history
		if a.queryHistoryIdx > -1 {
			a.queryHistoryIdx--
			if a.queryHistoryIdx == -1 {
				// Restore draft
				a.queryInput = a.queryHistoryDraft
			} else {
				a.queryInput = a.queryHistory[a.queryHistoryIdx]
			}
		}
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

func (a *App) loadQueryHistory() tea.Msg {
	if a.historyStore == nil || a.user == nil {
		return QueryHistoryLoadedMsg{Queries: nil}
	}

	// Load recent queries for this user
	records, err := a.historyStore.GetQueryHistoryForUser(a.user.Name, 100)
	if err != nil {
		return QueryHistoryLoadedMsg{Queries: nil}
	}

	queries := make([]string, 0, len(records))
	seen := make(map[string]bool)
	for _, r := range records {
		if r.Query != "" && !seen[r.Query] {
			queries = append(queries, r.Query)
			seen[r.Query] = true
		}
	}
	return QueryHistoryLoadedMsg{Queries: queries}
}

func (a *App) handleEditCell() (tea.Model, tea.Cmd) {
	if a.focus != FocusData {
		return a, nil
	}

	// Check access level
	if a.selectedDB >= len(a.databases) {
		return a, nil
	}
	db := a.databases[a.selectedDB]
	if !db.AccessLevel.CanWrite() {
		a.editError = fmt.Errorf("read-only access")
		return a, nil
	}

	// Check we have data and a valid row
	if len(a.dataRows) == 0 || a.selectedRow >= len(a.dataRows) {
		return a, nil
	}

	// Enter edit mode for first visible column
	a.editingCell = true
	a.editCellRow = a.selectedRow
	a.editCellCol = a.colOffset // start at first visible column
	a.editError = nil
	a.updateTableHeight()

	// Get current value
	if a.editCellCol < len(a.dataRows[a.selectedRow]) {
		a.editCellValue = database.FormatValue(a.dataRows[a.selectedRow][a.editCellCol])
	} else {
		a.editCellValue = ""
	}

	return a, nil
}

func (a *App) handleEditInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		a.editingCell = false
		a.editError = nil
		return a, nil

	case tea.KeyEnter:
		// Save the cell value
		return a, a.executeCellUpdate

	case tea.KeyLeft:
		// Move to previous column
		if a.editCellCol > 0 {
			a.editCellCol--
			if a.editCellCol < a.colOffset {
				a.colOffset = a.editCellCol
				a.updateDataTable()
			}
			if a.editCellCol < len(a.dataRows[a.editCellRow]) {
				a.editCellValue = database.FormatValue(a.dataRows[a.editCellRow][a.editCellCol])
			} else {
				a.editCellValue = ""
			}
		}
		return a, nil

	case tea.KeyRight, tea.KeyTab:
		// Move to next column
		if a.editCellCol < len(a.dataColumns)-1 {
			a.editCellCol++
			if a.editCellCol >= a.colOffset+a.visibleCols {
				a.colOffset = a.editCellCol - a.visibleCols + 1
				a.updateDataTable()
			}
			if a.editCellCol < len(a.dataRows[a.editCellRow]) {
				a.editCellValue = database.FormatValue(a.dataRows[a.editCellRow][a.editCellCol])
			} else {
				a.editCellValue = ""
			}
		}
		return a, nil

	case tea.KeyBackspace:
		if len(a.editCellValue) > 0 {
			a.editCellValue = a.editCellValue[:len(a.editCellValue)-1]
		}
		return a, nil

	case tea.KeyRunes:
		a.editCellValue += string(msg.Runes)
		return a, nil

	case tea.KeySpace:
		a.editCellValue += " "
		return a, nil
	}

	return a, nil
}

func (a *App) executeCellUpdate() tea.Msg {
	if a.selectedDB >= len(a.databases) || a.selectedTable >= len(a.tables) {
		return CellUpdatedMsg{Error: fmt.Errorf("no table selected")}
	}

	db := a.databases[a.selectedDB]
	tableName := a.tables[a.selectedTable]

	conn, err := a.dbManager.OpenConnection(db.Alias, a.user)
	if err != nil {
		return CellUpdatedMsg{Error: err}
	}

	// Get schema to find primary key
	schema := database.NewSchema(conn)
	tableInfo, err := schema.GetTableInfo(tableName)
	if err != nil {
		return CellUpdatedMsg{Error: err}
	}

	// Find primary key column(s)
	var pkCols []string
	for _, col := range tableInfo.Columns {
		if col.PrimaryKey > 0 {
			pkCols = append(pkCols, col.Name)
		}
	}

	if len(pkCols) == 0 {
		return CellUpdatedMsg{Error: fmt.Errorf("table has no primary key")}
	}

	// Build UPDATE query
	colName := a.dataColumns[a.editCellCol]
	row := a.dataRows[a.editCellRow]

	// Build WHERE clause from primary key values
	whereParts := make([]string, len(pkCols))
	whereArgs := make([]any, len(pkCols))
	for i, pkCol := range pkCols {
		// Find pk column index
		pkIdx := -1
		for j, c := range a.dataColumns {
			if c == pkCol {
				pkIdx = j
				break
			}
		}
		if pkIdx == -1 || pkIdx >= len(row) {
			return CellUpdatedMsg{Error: fmt.Errorf("primary key column %s not found in data", pkCol)}
		}
		whereParts[i] = fmt.Sprintf("%s = ?", pkCol)
		whereArgs[i] = row[pkIdx]
	}

	query := fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s",
		tableName, colName, strings.Join(whereParts, " AND "))
	args := append([]any{a.editCellValue}, whereArgs...)

	_, err = conn.Execute(query, args...)
	if err != nil {
		return CellUpdatedMsg{Error: err}
	}

	// Update local data
	a.dataRows[a.editCellRow][a.editCellCol] = a.editCellValue

	return CellUpdatedMsg{Error: nil}
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

	// Calculate pane widths based on content
	dbWidth := a.calculateDBPaneWidth()
	tableWidth := a.calculateTablePaneWidth()

	// Cap panel widths to reasonable maximum (1/3 of screen each)
	maxPanelWidth := a.width / 3
	if dbWidth > maxPanelWidth {
		dbWidth = maxPanelWidth
	}
	if tableWidth > maxPanelWidth {
		tableWidth = maxPanelWidth
	}

	// Minimum widths
	if dbWidth < 15 {
		dbWidth = 15
	}
	if tableWidth < 12 {
		tableWidth = 12
	}

	dataWidth := a.width - dbWidth - tableWidth - 2 // -2 for gaps between panes
	contentHeight := a.height - 2                   // query (1) + status (1)

	var b strings.Builder

	// Main content - three panes (no header - title moved to status bar)
	dbPane := a.renderDBPane(dbWidth, contentHeight)
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

func (a *App) renderDBPane(width, height int) string {
	focused := a.focus == FocusDatabases

	// Inner height = height - 2 (borders)
	visibleHeight := height - 2
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	var content strings.Builder

	if len(a.databases) == 0 {
		content.WriteString(dimItemStyle.Render(" No databases"))
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
			content.WriteString(dimItemStyle.Render(" ↑ more\n"))
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
			if i < end-1 || end < len(a.databases) {
				content.WriteString("\n")
			}
		}

		if end < len(a.databases) {
			content.WriteString(dimItemStyle.Render(" ↓ more"))
		}
	}

	return a.renderPaneWithTitle(content.String(), width, height, "Databases", focused)
}

func (a *App) renderTablePane(width, height int) string {
	focused := a.focus == FocusTables

	// Inner height = height - 2 (borders)
	visibleHeight := height - 2
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	var content strings.Builder

	if len(a.tables) == 0 {
		content.WriteString(dimItemStyle.Render(" No tables"))
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
			content.WriteString(dimItemStyle.Render(" ↑ more\n"))
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
			if i < end-1 || end < len(a.tables) {
				content.WriteString("\n")
			}
		}

		if end < len(a.tables) {
			content.WriteString(dimItemStyle.Render(" ↓ more"))
		}
	}

	return a.renderPaneWithTitle(content.String(), width, height, "Tables", focused)
}

func (a *App) renderDataPane(width, height int) string {
	focused := a.focus == FocusData

	if len(a.dataColumns) == 0 {
		return a.renderPaneWithTitle(dimItemStyle.Render("No data"), width, height, "Data", focused)
	}

	var content strings.Builder

	// Column scroll indicator (header)
	totalCols := len(a.dataColumns)
	endCol := a.colOffset + a.visibleCols
	if endCol > totalCols {
		endCol = totalCols
	}
	if a.colOffset > 0 || endCol < totalCols {
		leftArrow := ""
		rightArrow := ""
		if a.colOffset > 0 {
			leftArrow = fmt.Sprintf("← %d ", a.colOffset)
		}
		if endCol < totalCols {
			rightArrow = fmt.Sprintf(" %d →", totalCols-endCol)
		}
		colIndicator := dimItemStyle.Render(fmt.Sprintf("%scols %d-%d/%d%s", leftArrow, a.colOffset+1, endCol, totalCols, rightArrow))
		content.WriteString(colIndicator)
		content.WriteString("\n")
	}

	// Edit mode indicator
	if a.editingCell {
		editInfo := fmt.Sprintf("Editing [%s]: %s█", a.dataColumns[a.editCellCol], a.editCellValue)
		content.WriteString(queryInputStyle.Render(editInfo))
		content.WriteString("\n")
	} else if a.editError != nil {
		content.WriteString(errorStyle.Render(a.editError.Error()))
		content.WriteString("\n")
	}

	// Get table view - the table component handles scrolling internally
	tableView := a.dataTable.View()
	content.WriteString(tableView)

	// Add indicator for rows below viewport
	scrollOffset := a.selectedRow - a.tableDataRows + 1
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	lastVisible := scrollOffset + a.tableDataRows - 1
	if lastVisible >= len(a.dataRows) {
		lastVisible = len(a.dataRows) - 1
	}
	if a.selectedRow == len(a.dataRows)-1 && len(a.dataRows) > 0 {
		lastVisible = a.selectedRow
	}
	rowsBelow := a.totalRows - int64(lastVisible) - 1
	if rowsBelow > 0 {
		indicator := fmt.Sprintf("\n↓ %d more rows", rowsBelow)
		if int64(len(a.dataRows)) < a.totalRows {
			indicator += " (scroll to load)"
		}
		content.WriteString(dimItemStyle.Render(indicator))
	}

	return a.renderPaneWithTitle(content.String(), width, height, "Data", focused)
}

// buildBorderTitle builds a top border line with an embedded title
// width is the total width including border characters
// title is the plain text title (no styling applied yet)
// focused determines the border color
func (a *App) buildBorderTitle(width int, title string, focused bool) string {
	border := lipgloss.RoundedBorder()
	var borderColor lipgloss.Color
	var titleStyle lipgloss.Style
	if focused {
		borderColor = lipgloss.Color("#7C3AED") // primaryColor
		titleStyle = focusedBorderTitleStyle
	} else {
		borderColor = lipgloss.Color("#6B7280") // mutedColor
		titleStyle = borderTitleStyle
	}

	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// Build: ╭─ Title ─────────────────╮
	// Corner + horizontal line + space + title + space + horizontal lines + corner
	titleText := title
	titleRendered := titleStyle.Render(titleText)
	titleWidth := lipgloss.Width(titleRendered)

	// Calculate how many horizontal border chars we need after the title
	// Format: ╭─ Title ───────╮
	// width - 1 (left corner) - 1 (left bar) - 1 (space) - titleWidth - 1 (space) - 1 (right corner) = remaining bars
	// = width - 5 - titleWidth
	remainingWidth := width - 5 - titleWidth
	if remainingWidth < 0 {
		remainingWidth = 0
	}

	// Build the line
	var b strings.Builder
	b.WriteString(borderStyle.Render(border.TopLeft))
	b.WriteString(borderStyle.Render(border.Top))
	b.WriteString(" ")
	b.WriteString(titleRendered)
	b.WriteString(" ")
	for i := 0; i < remainingWidth; i++ {
		b.WriteString(borderStyle.Render(border.Top))
	}
	b.WriteString(borderStyle.Render(border.TopRight))

	return b.String()
}

// renderPaneWithTitle renders content in a pane with a title in the top border
func (a *App) renderPaneWithTitle(content string, width, height int, title string, focused bool) string {
	border := lipgloss.RoundedBorder()
	var borderColor lipgloss.Color
	if focused {
		borderColor = lipgloss.Color("#7C3AED") // primaryColor
	} else {
		borderColor = lipgloss.Color("#6B7280") // mutedColor
	}
	borderStyle := lipgloss.NewStyle().Foreground(borderColor)

	// Inner dimensions (excluding borders)
	innerWidth := width - 2   // left and right borders
	innerHeight := height - 2 // top and bottom borders
	if innerWidth < 1 {
		innerWidth = 1
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Split content into lines and pad/truncate to fit
	contentLines := strings.Split(content, "\n")

	// Pad or truncate to innerHeight lines
	for len(contentLines) < innerHeight {
		contentLines = append(contentLines, "")
	}
	if len(contentLines) > innerHeight {
		contentLines = contentLines[:innerHeight]
	}

	var result strings.Builder

	// Top border with title
	result.WriteString(a.buildBorderTitle(width, title, focused))
	result.WriteString("\n")

	// Content lines with side borders
	for _, line := range contentLines {
		result.WriteString(borderStyle.Render(border.Left))
		// Pad line to innerWidth (accounting for padding)
		paddedLine := " " + line // left padding
		lineWidth := lipgloss.Width(paddedLine)
		if lineWidth < innerWidth {
			paddedLine += strings.Repeat(" ", innerWidth-lineWidth)
		}
		result.WriteString(paddedLine)
		result.WriteString(borderStyle.Render(border.Right))
		result.WriteString("\n")
	}

	// Bottom border
	result.WriteString(borderStyle.Render(border.BottomLeft))
	for i := 0; i < innerWidth; i++ {
		result.WriteString(borderStyle.Render(border.Bottom))
	}
	result.WriteString(borderStyle.Render(border.BottomRight))

	return result.String()
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
	var leftParts []string
	var rightParts []string

	// Left side: title and user
	leftParts = append(leftParts, titleStyle.Render("sqlite-tui"))
	leftParts = append(leftParts, dimItemStyle.Render(a.user.DisplayName()))

	// Right side: db/table info, row count, badge, help
	if a.selectedDB < len(a.databases) {
		db := a.databases[a.selectedDB]
		rightParts = append(rightParts, statusKeyStyle.Render(db.Alias))
	}
	if a.selectedTable < len(a.tables) {
		rightParts = append(rightParts, statusValueStyle.Render("> "+a.tables[a.selectedTable]))
	}

	// Row count
	if len(a.dataRows) > 0 {
		rightParts = append(rightParts, dimItemStyle.Render(fmt.Sprintf("| row %d/%d", a.selectedRow+1, a.totalRows)))
	} else if a.totalRows > 0 {
		rightParts = append(rightParts, dimItemStyle.Render(fmt.Sprintf("| %d rows", a.totalRows)))
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
		rightParts = append(rightParts, badge)
	}

	rightParts = append(rightParts, dimItemStyle.Render("| ?:help q:quit"))

	// Combine left and right with space in between
	leftContent := strings.Join(leftParts, " ")
	rightContent := strings.Join(rightParts, " ")

	// Calculate padding between left and right
	leftWidth := lipgloss.Width(leftContent)
	rightWidth := lipgloss.Width(rightContent)
	padding := a.width - leftWidth - rightWidth - 2 // -2 for statusBar padding
	if padding < 1 {
		padding = 1
	}

	content := leftContent + strings.Repeat(" ", padding) + rightContent
	return statusBarStyle.Width(a.width).Render(content)
}

func (a *App) renderHelp() string {
	var b strings.Builder

	bindings := []struct {
		key  string
		desc string
	}{
		{"↑/k, ↓/j", "Navigate rows"},
		{"←/h, →/l", "Scroll columns (in data pane)"},
		{"PgUp/^U", "Page up"},
		{"PgDn/^D", "Page down"},
		{"Home/g", "Go to top"},
		{"End/G", "Go to bottom"},
		{"Tab", "Next pane"},
		{"Enter", "Select"},
		{"/", "Query mode (↑/↓ for history)"},
		{"e", "Edit cell (write access)"},
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

// calculateDBPaneWidth returns the width needed for the database panel
// based on the longest database name, plus space for "> " prefix and borders
func (a *App) calculateDBPaneWidth() int {
	maxLen := 9 // "Databases" header length
	for _, db := range a.databases {
		if len(db.Alias) > maxLen {
			maxLen = len(db.Alias)
		}
	}
	// +2 for "> " prefix, +2 for horizontal padding, +2 for borders, +1 extra
	return maxLen + 7
}

// calculateTablePaneWidth returns the width needed for the tables panel
// based on the longest table name, plus space for "> " prefix and borders
func (a *App) calculateTablePaneWidth() int {
	maxLen := 6 // "Tables" header length
	for _, t := range a.tables {
		if len(t) > maxLen {
			maxLen = len(t)
		}
	}
	// +2 for "> " prefix, +2 for horizontal padding, +2 for borders, +1 extra
	return maxLen + 7
}
