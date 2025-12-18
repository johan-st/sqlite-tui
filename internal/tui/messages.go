package tui

import (
	"github.com/johan-st/sqlite-tui/internal/database"
)

// Messages for async operations

// DatabasesLoadedMsg is sent when databases are loaded.
type DatabasesLoadedMsg struct {
	Databases []*database.DatabaseInfo
}

// TablesLoadedMsg is sent when tables are loaded.
type TablesLoadedMsg struct {
	Tables []string
	Error  error
}

// DataLoadedMsg is sent when table data is loaded.
type DataLoadedMsg struct {
	Result *database.QueryResult
	Error  error
}

// SchemaLoadedMsg is sent when table schema is loaded.
type SchemaLoadedMsg struct {
	Info  *database.TableInfo
	Error error
}

// QueryExecutedMsg is sent when a query is executed.
type QueryExecutedMsg struct {
	Result *database.QueryResult
	Error  error
}

// ErrorMsg is sent when an error occurs.
type ErrorMsg struct {
	Error error
}

// RefreshMsg triggers a refresh of the current view.
type RefreshMsg struct{}

