// Package loader reads codegen inputs: JSON schemas and profile YAML.
package loader

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ProfileMessage maps one action to its schema files and direction.
type ProfileMessage struct {
	Name     string `yaml:"name"`
	Request  string `yaml:"request"`
	Response string `yaml:"response"`
	Dir      string `yaml:"dir"`
}

// Profile is one OCPP feature profile (Core, FirmwareManagement, ...).
type Profile struct {
	Messages []ProfileMessage `yaml:"messages"`
}

// ProfileSet is the full v*.yaml document.
type ProfileSet struct {
	Version  string             `yaml:"version"`
	Profiles map[string]Profile `yaml:"profiles"`
}

// LoadProfile reads and parses a profile YAML file.
func LoadProfile(path string) (ProfileSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ProfileSet{}, fmt.Errorf("read profile: %w", err)
	}
	var ps ProfileSet
	if err := yaml.Unmarshal(b, &ps); err != nil {
		return ProfileSet{}, fmt.Errorf("parse profile: %w", err)
	}
	return ps, nil
}
