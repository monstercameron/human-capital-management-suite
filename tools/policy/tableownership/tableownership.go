// Package tableownership implements ALIGN-010/ALIGN-011. It checks that
// every default-schema table has an explicit repository owner and an actual
// Go consumer, reporting every deficient table in one deterministic result.
package tableownership

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/tableinventory"
)

const schemaVersion = 1

// ModulePath is the repository module path used in consumer evidence.
const ModulePath = "github.com/monstercameron/human-capital-management-suite"

// Version identifies this policy contract.
func Version() int { return schemaVersion }

// Finding is one owner or consumer violation.
type Finding struct {
	Table  string `json:"table"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

func (f Finding) Error() string {
	return fmt.Sprintf("tableownership: %s: %s: %s", f.Table, f.Code, f.Detail)
}

// Report contains all findings and the consumer evidence discovered by the
// source scan.
type Report struct {
	Findings  []Finding           `json:"findings"`
	Consumers map[string][]string `json:"consumers"`
}

// OK reports whether the ownership report is clean.
func (r Report) OK() bool { return len(r.Findings) == 0 }

// Explain renders bounded facts without exposing source contents.
func (r Report) Explain() string {
	return fmt.Sprintf("table ownership report v%d with %d finding(s) and %d consumer mapping(s)", schemaVersion, len(r.Findings), len(r.Consumers))
}

// Validate applies owner and consumer rules to a caller-supplied inventory.
// The map value is the list of packages that consume each table. This pure
// form is used by tests and by callers with a precomputed source index.
func Validate(inventory tableinventory.Inventory, consumers map[string][]string) []Finding {
	var findings []Finding
	for _, table := range inventory.Tables {
		name := strings.TrimSpace(table.Table)
		if name == "" {
			continue
		}
		if strings.TrimSpace(table.OwnerPackage) == "" {
			findings = append(findings, Finding{Table: name, Code: "OWNERLESS", Detail: "table has no owner_package"})
		}
		if len(consumers[name]) == 0 && len(consumers[strings.ToLower(name)]) == 0 {
			findings = append(findings, Finding{Table: name, Code: "CONSUMERLESS", Detail: "no Go package references the default table"})
		}
	}
	return sortFindings(findings)
}

// Evaluate loads the source registry, confirms owner package directories
// exist, and scans Go source for table consumers. It does not edit any
// registry.
func Evaluate(root string) (Report, error) {
	inventory, err := tableinventory.Scan(root)
	if err != nil {
		return Report{}, err
	}
	consumers, err := DiscoverConsumers(root, inventory.Tables)
	if err != nil {
		return Report{}, fmt.Errorf("tableownership: discover consumers: %w", err)
	}
	findings := Validate(inventory, consumers)
	for _, table := range inventory.Tables {
		if strings.TrimSpace(table.OwnerPackage) == "" {
			continue
		}
		ownerPath := filepath.Join(root, filepath.FromSlash(table.OwnerPackage))
		if _, err := os.Stat(ownerPath); err != nil {
			if os.IsNotExist(err) {
				findings = append(findings, Finding{Table: table.Table, Code: "OWNER_PACKAGE_MISSING", Detail: fmt.Sprintf("owner package %q is not present", table.OwnerPackage)})
			} else {
				findings = append(findings, Finding{Table: table.Table, Code: "OWNER_PACKAGE_UNREADABLE", Detail: fmt.Sprintf("owner package %q: %v", table.OwnerPackage, err)})
			}
		}
	}
	return Report{Findings: sortFindings(findings), Consumers: consumers}, nil
}

// Check is the policy-tool entry point. Its error contains every finding so a
// caller can repair all deficient tables in one pass.
func Check(root string) error {
	report, err := Evaluate(root)
	if err != nil {
		return err
	}
	if !report.OK() {
		parts := make([]string, len(report.Findings))
		for i, finding := range report.Findings {
			parts[i] = finding.Error()
		}
		return fmt.Errorf("%s", strings.Join(parts, "; "))
	}
	return nil
}

// DiscoverConsumers indexes literal table-name references in Go files. A
// source reference is evidence of a package consumer; comments and SQL in
// tests are intentionally retained because they document and exercise the
// same physical contract.
func DiscoverConsumers(root string, tables []tableinventory.Table) (map[string][]string, error) {
	out := make(map[string][]string)
	for _, table := range tables {
		out[table.Table] = nil
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			// .artifacts holds local build caches and a pristine checkout copy;
			// scanning it would double-count consumers under non-module paths.
			if entry.Name() == ".git" || entry.Name() == "vendor" || entry.Name() == ".artifacts" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := ModulePath + "/" + filepath.ToSlash(rel)
		if rel == "." {
			pkg = ModulePath
		}
		text := string(body)
		for _, table := range tables {
			if table.Table == "" || !containsWord(text, table.Table) {
				continue
			}
			out[table.Table] = appendUnique(out[table.Table], pkg)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for table := range out {
		sort.Strings(out[table])
	}
	return out, nil
}

func containsWord(text, word string) bool {
	for start := 0; ; {
		idx := strings.Index(text[start:], word)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !isIdentifier(text[idx-1])
		after := idx + len(word)
		afterOK := after == len(text) || !isIdentifier(text[after])
		if beforeOK && afterOK {
			return true
		}
		start = after
		if start >= len(text) {
			return false
		}
	}
}

func isIdentifier(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func sortFindings(findings []Finding) []Finding {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Table != findings[j].Table {
			return findings[i].Table < findings[j].Table
		}
		return findings[i].Code < findings[j].Code
	})
	return findings
}
