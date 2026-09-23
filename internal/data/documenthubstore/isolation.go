// Isolation registry for HUB-003: the machine-readable link between the
// document disposition YAML and the conformance tests. Every document-owned
// table must be registered with its tenant-isolation contract; the tests
// prove the live database matches the registry in both directions, so a
// future table todo cannot land storage without isolation review.
package documenthubstore

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

// DispositionTable is one document-owned table's isolation contract.
type DispositionTable struct {
	Table        string `yaml:"table"`
	Migration    string `yaml:"migration"`
	OwnerPackage string `yaml:"owner_package"`
	RLSPolicy    string `yaml:"rls_policy"`
	RLSRequired  bool   `yaml:"rls_required"`
	TenantColumn string `yaml:"tenant_scoping_column"`
	AppendOnly   bool   `yaml:"append_only"`
}

// Disposition is the parsed document storage disposition.
type Disposition struct {
	Tables []DispositionTable `yaml:"tables"`
}

// LoadDisposition parses the document storage disposition registry.
func LoadDisposition(path string) (Disposition, error) {
	var reg Disposition
	raw, err := os.ReadFile(path)
	if err != nil {
		return reg, err
	}
	if err := yaml.Unmarshal(raw, &reg); err != nil {
		return reg, err
	}
	if len(reg.Tables) == 0 {
		return reg, errors.New("document disposition registers no tables")
	}
	return reg, nil
}

func tableNames(reg Disposition) []string {
	names := make([]string, 0, len(reg.Tables))
	for _, tb := range reg.Tables {
		names = append(names, tb.Table)
	}
	return names
}
