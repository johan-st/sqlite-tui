// Package config handles configuration file parsing and hot-reloading.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/johan-st/sqlite-tui/internal/access"
	"gopkg.in/yaml.v3"
)

// Config represents the application configuration.
type Config struct {
	Name   string       `yaml:"name"`
	Server ServerConfig `yaml:"server"`

	// Database sources - file paths, directories, or globs
	Databases []DatabaseSource `yaml:"databases"`

	// Anonymous access level (none, read-only, read-write)
	AnonymousAccess string `yaml:"anonymous_access"`

	// Allow keyless SSH connections
	AllowKeyless bool `yaml:"allow_keyless"`

	// Users and their access rules
	Users []User `yaml:"users"`

	// Public databases (accessible without auth)
	Public []PublicDatabase `yaml:"public"`

	// Internal: path to the config file
	path string

	// Internal: last modified time
	modTime time.Time

	mu sync.RWMutex
}

// ServerConfig contains server-related configuration.
type ServerConfig struct {
	SSH   SSHConfig   `yaml:"ssh"`
	Local LocalConfig `yaml:"local"`
}

// SSHConfig contains SSH server configuration.
type SSHConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Listen      string `yaml:"listen"`
	HostKeyPath string `yaml:"host_key_path"`
	IdleTimeout string `yaml:"idle_timeout"`
	MaxTimeout  string `yaml:"max_timeout"`
}

// LocalConfig contains local mode configuration.
type LocalConfig struct {
	Enabled bool `yaml:"enabled"`
}

// DatabaseSource defines a source of database files.
type DatabaseSource struct {
	Path        string `yaml:"path"`
	Alias       string `yaml:"alias"`
	Description string `yaml:"description"`
	Recursive   bool   `yaml:"recursive"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Name: "sqlite-tui",
		Server: ServerConfig{
			SSH: SSHConfig{
				Enabled:     true,
				Listen:      ":2222",
				HostKeyPath: ".sqlite-tui/host_key",
				IdleTimeout: "30m",
				MaxTimeout:  "24h",
			},
			Local: LocalConfig{
				Enabled: true,
			},
		},
		Databases:       []DatabaseSource{},
		AnonymousAccess: "none",
		AllowKeyless:    false,
		Users:           []User{},
		Public:          []PublicDatabase{},
	}
}

// Load reads and parses a configuration file.
func Load(path string) (*Config, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	cfg.path = absPath

	// Get file modification time
	info, err := os.Stat(absPath)
	if err == nil {
		cfg.modTime = info.ModTime()
	}

	return cfg, nil
}

// Path returns the path to the config file.
func (c *Config) Path() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.path
}

// Reload reloads the configuration from disk.
func (c *Config) Reload() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(c.path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	newCfg := DefaultConfig()
	if err := yaml.Unmarshal(data, newCfg); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Update fields
	c.Name = newCfg.Name
	c.Server = newCfg.Server
	c.Databases = newCfg.Databases
	c.AnonymousAccess = newCfg.AnonymousAccess
	c.AllowKeyless = newCfg.AllowKeyless
	c.Users = newCfg.Users
	c.Public = newCfg.Public

	// Update mod time
	info, err := os.Stat(c.path)
	if err == nil {
		c.modTime = info.ModTime()
	}

	return nil
}

// HasChanged checks if the config file has been modified.
func (c *Config) HasChanged() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info, err := os.Stat(c.path)
	if err != nil {
		return false
	}
	return info.ModTime().After(c.modTime)
}

// BuildResolver creates an access.Resolver from the configuration.
func (c *Config) BuildResolver() *access.Resolver {
	c.mu.RLock()
	defer c.mu.RUnlock()

	resolver := access.NewResolver()

	// Set anonymous access level
	resolver.SetAnonymousAccess(access.ParseLevel(c.AnonymousAccess))

	// Add public rules
	for _, pub := range c.Public {
		resolver.AddPublicRule(pub.Pattern, access.ParseLevel(pub.Level))
	}

	// Add user rules
	for _, user := range c.Users {
		if user.Admin {
			resolver.AddAdmin(user.Name)
		}
		for _, rule := range user.Access {
			resolver.AddUserRule(user.Name, rule.Pattern, access.ParseLevel(rule.Level))
		}
	}

	return resolver
}

// FindUserByPublicKey finds a user by their SSH public key.
func (c *Config) FindUserByPublicKey(keyFingerprint string) *User {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for i := range c.Users {
		for _, key := range c.Users[i].PublicKeys {
			// Simple fingerprint comparison - in practice, you'd parse the key
			if key == keyFingerprint {
				return &c.Users[i]
			}
		}
	}
	return nil
}

// GetIdleTimeout parses and returns the idle timeout duration.
func (c *Config) GetIdleTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	d, err := time.ParseDuration(c.Server.SSH.IdleTimeout)
	if err != nil {
		return 30 * time.Minute
	}
	return d
}

// GetMaxTimeout parses and returns the max timeout duration.
func (c *Config) GetMaxTimeout() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()

	d, err := time.ParseDuration(c.Server.SSH.MaxTimeout)
	if err != nil {
		return 24 * time.Hour
	}
	return d
}

// GetDataDir returns the data directory path (for history, keys, etc.).
func (c *Config) GetDataDir() string {
	return ".sqlite-tui"
}
