package config

import (
	"log"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches the config file for changes and reloads it.
type Watcher struct {
	config    *Config
	watcher   *fsnotify.Watcher
	callbacks []func(*Config)
	stop      chan struct{}
	mu        sync.RWMutex
}

// NewWatcher creates a new config file watcher.
func NewWatcher(config *Config) (*Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		config:    config,
		watcher:   watcher,
		callbacks: make([]func(*Config), 0),
		stop:      make(chan struct{}),
	}

	return w, nil
}

// OnReload registers a callback to be called when the config is reloaded.
func (w *Watcher) OnReload(callback func(*Config)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.callbacks = append(w.callbacks, callback)
}

// Start begins watching the config file.
func (w *Watcher) Start() error {
	path := w.config.Path()
	if path == "" {
		return nil // No config file to watch
	}

	if err := w.watcher.Add(path); err != nil {
		return err
	}

	go w.watch()
	return nil
}

// Stop stops watching the config file.
func (w *Watcher) Stop() {
	close(w.stop)
	w.watcher.Close()
}

// watch is the main watch loop.
func (w *Watcher) watch() {
	// Debounce timer to avoid multiple reloads for rapid changes
	var debounceTimer *time.Timer
	const debounceDelay = 100 * time.Millisecond

	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Only care about writes and creates
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				// Reset or start debounce timer
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDelay, func() {
					w.reload()
				})
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("config watcher error: %v", err)

		case <-w.stop:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return
		}
	}
}

// reload reloads the config and notifies callbacks.
func (w *Watcher) reload() {
	if err := w.config.Reload(); err != nil {
		log.Printf("failed to reload config: %v", err)
		return
	}

	log.Printf("config reloaded from %s", w.config.Path())

	w.mu.RLock()
	callbacks := make([]func(*Config), len(w.callbacks))
	copy(callbacks, w.callbacks)
	w.mu.RUnlock()

	for _, cb := range callbacks {
		cb(w.config)
	}
}

// GetConfig returns the current config.
func (w *Watcher) GetConfig() *Config {
	return w.config
}
