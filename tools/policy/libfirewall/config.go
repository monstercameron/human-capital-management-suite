package libfirewall

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ProtobufGRPC is the LIB-003 qualification detail.
type ProtobufGRPC struct {
	Modules            []string `yaml:"modules"`
	AllowedImportRoots []string `yaml:"allowed_import_roots"`
}

// Config is the parsed form of library-firewall.yaml.
type Config struct {
	Version int    `yaml:"version"`
	Module  string `yaml:"module"`

	ProtobufGRPC           ProtobufGRPC `yaml:"protobuf_grpc"`
	PGXLeakTypes           []string     `yaml:"pgx_leak_types"`
	APDLeakTypes           []string     `yaml:"apd_leak_types"`
	MongoLeakTypes         []string     `yaml:"mongo_leak_types"`
	OTelModulePrefix       string       `yaml:"otel_module_prefix"`
	GooseGoMigrationMarker string       `yaml:"goose_go_migration_marker"`
}

// LoadConfig reads and parses the manifest at path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("libfirewall: reading config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("libfirewall: parsing config: %w", err)
	}
	if c.Module == "" {
		return nil, fmt.Errorf("libfirewall: config has no module")
	}
	return &c, nil
}

// LeakTypesByImport groups a "import/path.TypeName" list (as
// pgx_leak_types/apd_leak_types are written in library-firewall.yaml) by
// import path, splitting on each entry's last "." (safe even for a
// dotted-hostname import path such as go.opentelemetry.io/otel, since a Go
// type name itself never contains a dot).
func LeakTypesByImport(qualifiedTypes []string) map[string][]string {
	out := map[string][]string{}
	for _, qt := range qualifiedTypes {
		idx := lastDot(qt)
		if idx < 0 {
			continue
		}
		path, name := qt[:idx], qt[idx+1:]
		out[path] = append(out[path], name)
	}
	return out
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}
