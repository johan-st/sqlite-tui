package config

import "github.com/johan-st/sqlite-tui/internal/access"

// AccessRule defines an access rule in the config file.
type AccessRule struct {
	Pattern string `yaml:"pattern"`
	Level   string `yaml:"level"`
}

// ToAccessRule converts a config AccessRule to an access.Rule.
func (r AccessRule) ToAccessRule() access.Rule {
	return access.Rule{
		Pattern: r.Pattern,
		Level:   access.ParseLevel(r.Level),
	}
}

// User represents a user in the config file.
type User struct {
	Name       string       `yaml:"name"`
	Admin      bool         `yaml:"admin"`
	PublicKeys []string     `yaml:"public_keys"`
	Access     []AccessRule `yaml:"access"`
}

// PublicDatabase defines a publicly accessible database pattern.
type PublicDatabase struct {
	Pattern string `yaml:"pattern"`
	Level   string `yaml:"level"`
}

// ToAccessRule converts a PublicDatabase to an access.Rule.
func (p PublicDatabase) ToAccessRule() access.Rule {
	return access.Rule{
		Pattern: p.Pattern,
		Level:   access.ParseLevel(p.Level),
	}
}
