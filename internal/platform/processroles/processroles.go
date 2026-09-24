// Package processroles loads the SVC-001 process-role manifest used by
// application and topology composition.
package processroles

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Process is one process-role manifest row.
type Process struct {
	Command string `yaml:"command"`
	Status  string `yaml:"status"` // "initial" or "later"
	Role    string `yaml:"role"`
}

// Manifest is the parsed form of process-roles.yaml.
type Manifest struct {
	Version   int       `yaml:"version"`
	Module    string    `yaml:"module"`
	Processes []Process `yaml:"processes"`
}

// Load reads and parses the manifest at path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("processroles: reading manifest: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("processroles: parsing manifest: %w", err)
	}
	return &m, nil
}

// InitialCommands returns the command names with status "initial".
func (m *Manifest) InitialCommands() []string {
	var names []string
	for _, p := range m.Processes {
		if p.Status == "initial" {
			names = append(names, p.Command)
		}
	}
	return names
}

// LaterCommands returns the command names with status "later".
func (m *Manifest) LaterCommands() []string {
	var names []string
	for _, p := range m.Processes {
		if p.Status == "later" {
			names = append(names, p.Command)
		}
	}
	return names
}

// DuplicateCommands returns any command name that appears more than once in
// the manifest (SVC-001 requires exactly one row per cmd/* directory).
func (m *Manifest) DuplicateCommands() []string {
	seen := map[string]int{}
	for _, p := range m.Processes {
		seen[p.Command]++
	}
	var dupes []string
	for name, count := range seen {
		if count > 1 {
			dupes = append(dupes, name)
		}
	}
	return dupes
}

// ListCmdDirectories lists the second-level directory names under root/cmd.
func ListCmdDirectories(root string) ([]string, error) {
	entries, err := os.ReadDir(root + "/cmd")
	if err != nil {
		return nil, fmt.Errorf("processroles: reading cmd directory: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
