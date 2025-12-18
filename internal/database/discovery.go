package database

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/fsnotify/fsnotify"
	"github.com/johan-st/sqlite-tui/internal/config"
)

// DiscoveredDatabase represents a discovered database file.
type DiscoveredDatabase struct {
	Path        string
	Alias       string
	Description string
	Size        int64
	ModTime     int64
	Source      *config.DatabaseSource
}

// Discovery handles database file discovery and watching.
type Discovery struct {
	sources   []config.DatabaseSource
	databases map[string]*DiscoveredDatabase
	watcher   *fsnotify.Watcher
	callbacks []func(added, removed []*DiscoveredDatabase)
	stop      chan struct{}
	mu        sync.RWMutex
}

// NewDiscovery creates a new database discovery service.
func NewDiscovery(sources []config.DatabaseSource) (*Discovery, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	d := &Discovery{
		sources:   sources,
		databases: make(map[string]*DiscoveredDatabase),
		watcher:   watcher,
		callbacks: make([]func(added, removed []*DiscoveredDatabase), 0),
		stop:      make(chan struct{}),
	}

	return d, nil
}

// OnChange registers a callback for when databases are added or removed.
func (d *Discovery) OnChange(callback func(added, removed []*DiscoveredDatabase)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.callbacks = append(d.callbacks, callback)
}

// Start begins discovering and watching for database files.
func (d *Discovery) Start() error {
	// Initial scan
	if err := d.scan(); err != nil {
		return err
	}

	// Start watching
	go d.watch()

	return nil
}

// Stop stops the discovery service.
func (d *Discovery) Stop() {
	close(d.stop)
	d.watcher.Close()
}

// GetDatabases returns all discovered databases.
func (d *Discovery) GetDatabases() []*DiscoveredDatabase {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]*DiscoveredDatabase, 0, len(d.databases))
	for _, db := range d.databases {
		result = append(result, db)
	}
	return result
}

// GetDatabase returns a specific database by path or alias.
func (d *Discovery) GetDatabase(pathOrAlias string) *DiscoveredDatabase {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Try exact path match
	if db, ok := d.databases[pathOrAlias]; ok {
		return db
	}

	// Try alias match
	for _, db := range d.databases {
		if db.Alias == pathOrAlias {
			return db
		}
	}

	return nil
}

// scan discovers all database files from configured sources.
func (d *Discovery) scan() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	newDatabases := make(map[string]*DiscoveredDatabase)
	watchPaths := make(map[string]bool)

	for i := range d.sources {
		source := &d.sources[i]
		found, watchDirs, err := d.discoverSource(source)
		if err != nil {
			log.Printf("warning: failed to discover databases from %s: %v", source.Path, err)
			continue
		}

		for _, db := range found {
			newDatabases[db.Path] = db
		}

		for _, dir := range watchDirs {
			watchPaths[dir] = true
		}
	}

	// Determine added and removed databases
	var added, removed []*DiscoveredDatabase

	for path, db := range newDatabases {
		if _, exists := d.databases[path]; !exists {
			added = append(added, db)
		}
	}

	for path, db := range d.databases {
		if _, exists := newDatabases[path]; !exists {
			removed = append(removed, db)
		}
	}

	d.databases = newDatabases

	// Update watched paths
	for path := range watchPaths {
		d.watcher.Add(path)
	}

	// Notify callbacks (outside lock)
	if len(added) > 0 || len(removed) > 0 {
		go d.notifyCallbacks(added, removed)
	}

	return nil
}

// discoverSource discovers databases from a single source.
func (d *Discovery) discoverSource(source *config.DatabaseSource) ([]*DiscoveredDatabase, []string, error) {
	var databases []*DiscoveredDatabase
	var watchDirs []string

	path := source.Path

	// Check if it's a glob pattern
	if strings.ContainsAny(path, "*?[") {
		matches, err := doublestar.FilepathGlob(path)
		if err != nil {
			return nil, nil, err
		}

		for _, match := range matches {
			if isSQLiteFile(match) {
				db, err := d.createDiscoveredDB(match, source)
				if err != nil {
					log.Printf("warning: failed to stat %s: %v", match, err)
					continue
				}
				databases = append(databases, db)
			}
		}

		// Watch the parent directory of the glob pattern
		dir := filepath.Dir(strings.Split(path, "*")[0])
		if dir != "" && dir != "." {
			watchDirs = append(watchDirs, dir)
		}

		return databases, watchDirs, nil
	}

	// Check if path is a directory
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}

	if info.IsDir() {
		// Discover all .db files in directory
		walkFn := func(filePath string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // Skip errors
			}
			if d.IsDir() && filePath != path && !source.Recursive {
				return filepath.SkipDir
			}
			if !d.IsDir() && isSQLiteFile(filePath) {
				db, err := createDiscoveredDBFromPath(filePath, source)
				if err == nil {
					databases = append(databases, db)
				}
			}
			return nil
		}

		filepath.WalkDir(path, walkFn)
		watchDirs = append(watchDirs, path)

		return databases, watchDirs, nil
	}

	// Single file
	if isSQLiteFile(path) {
		db, err := d.createDiscoveredDB(path, source)
		if err != nil {
			return nil, nil, err
		}
		databases = append(databases, db)
		watchDirs = append(watchDirs, filepath.Dir(path))
	}

	return databases, watchDirs, nil
}

// createDiscoveredDB creates a DiscoveredDatabase from a path.
func (d *Discovery) createDiscoveredDB(path string, source *config.DatabaseSource) (*DiscoveredDatabase, error) {
	return createDiscoveredDBFromPath(path, source)
}

func createDiscoveredDBFromPath(path string, source *config.DatabaseSource) (*DiscoveredDatabase, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}

	// Generate alias
	alias := source.Alias
	if alias == "" {
		// Use filename without extension as alias
		alias = strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))
	} else if strings.Contains(alias, "*") {
		// Replace wildcard with actual filename
		name := strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))
		alias = strings.ReplaceAll(alias, "*", name)
	}

	return &DiscoveredDatabase{
		Path:        absPath,
		Alias:       alias,
		Description: source.Description,
		Size:        info.Size(),
		ModTime:     info.ModTime().Unix(),
		Source:      source,
	}, nil
}

// isSQLiteFile checks if a file looks like a SQLite database.
func isSQLiteFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".db" || ext == ".sqlite" || ext == ".sqlite3" || ext == ".db3"
}

// watch watches for file system changes.
func (d *Discovery) watch() {
	for {
		select {
		case event, ok := <-d.watcher.Events:
			if !ok {
				return
			}

			// Handle file changes
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				if isSQLiteFile(event.Name) {
					// Rescan to pick up changes
					d.scan()
				}
			}

		case err, ok := <-d.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("discovery watcher error: %v", err)

		case <-d.stop:
			return
		}
	}
}

// notifyCallbacks notifies all registered callbacks.
func (d *Discovery) notifyCallbacks(added, removed []*DiscoveredDatabase) {
	d.mu.RLock()
	callbacks := make([]func(added, removed []*DiscoveredDatabase), len(d.callbacks))
	copy(callbacks, d.callbacks)
	d.mu.RUnlock()

	for _, cb := range callbacks {
		cb(added, removed)
	}
}

// Refresh forces a rescan of all sources.
func (d *Discovery) Refresh() error {
	return d.scan()
}

// UpdateSources updates the database sources and rescans.
func (d *Discovery) UpdateSources(sources []config.DatabaseSource) error {
	d.mu.Lock()
	d.sources = sources
	d.mu.Unlock()

	return d.scan()
}
