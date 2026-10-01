// Package workflowpages loads and validates the WFPAGE-001 decision record
// at definitions/planning/workflow-pages-decisions.yaml: the routes, defaults,
// scope guard and budgets the workflow start, history and custom page
// surfaces are built against.
package workflowpages

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrInvalid is returned when the record is malformed or breaks a decision.
var ErrInvalid = errors.New("workflowpages: invalid decision record")

// Record is the decoded decision record.
type Record struct {
	Version   int      `yaml:"version"`
	DecidedOn string   `yaml:"decided_on"`
	DecidedBy string   `yaml:"decided_by"`
	Routes    Routes   `yaml:"routes"`
	Defaults  Defaults `yaml:"defaults"`
	Guard     Guard    `yaml:"scope_guard"`
	Budgets   Budgets  `yaml:"budgets"`
}

// Routes names the three workflow page routes.
type Routes struct {
	StartCatalog string `yaml:"start_catalog"`
	Start        string `yaml:"start"`
	History      string `yaml:"history"`
}

// Defaults are the behaviours every surface starts from.
type Defaults struct {
	DefaultPage                     string        `yaml:"default_page_per_workflow_version"`
	DesignerRole                    string        `yaml:"designer_role"`
	Validations                     string        `yaml:"validations"`
	RunRecordsPageDefinitionVersion bool          `yaml:"run_records_page_definition_version"`
	Draft                           Draft         `yaml:"draft"`
	SOPLinks                        string        `yaml:"sop_links"`
	SupportChannel                  string        `yaml:"support_channel"`
	HistoryExport                   HistoryExport `yaml:"history_export"`
}

// Draft is the server-side draft policy.
type Draft struct {
	Scope      string `yaml:"scope"`
	ExpiryDays int    `yaml:"expiry_days"`
}

// HistoryExport caps and redacts the history export.
type HistoryExport struct {
	MaxRows          int  `yaml:"max_rows"`
	RedactedToViewer bool `yaml:"redacted_to_viewer"`
}

// Guard is the scope guard: the designer only overrides pages.
type Guard struct {
	AddNodes         bool   `yaml:"page_designer_may_add_nodes"`
	AddCapabilities  bool   `yaml:"page_designer_may_add_capabilities"`
	AddWorkflows     bool   `yaml:"page_designer_may_add_workflows"`
	CustomPagesServe string `yaml:"custom_pages_serve"`
}

// Budgets are the bundle and interaction budgets.
type Budgets struct {
	WasmGzipGrowthBytes  int `yaml:"wasm_gzip_growth_bytes"`
	Filter500WorkflowsMS int `yaml:"filter_500_workflows_ms"`
}

// Load reads and validates the record at path.
func Load(path string) (Record, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	var record Record
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&record); err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Validate enforces the WFPAGE-001 decisions.
func (r Record) Validate() error {
	var problems []string
	if r.Routes.StartCatalog != "/workspace/app/workflows" {
		problems = append(problems, "start_catalog route must be /workspace/app/workflows")
	}
	if r.Routes.Start != "/workspace/app/workflows/start/{workflow}" {
		problems = append(problems, "start route must be /workspace/app/workflows/start/{workflow}")
	}
	if r.Routes.History != "/workspace/app/workflows/history" {
		problems = append(problems, "history route must be /workspace/app/workflows/history")
	}
	if r.Version != 1 || r.DecidedOn == "" || r.DecidedBy == "" {
		problems = append(problems, "version, decided_on and decided_by are required")
	}
	if r.Defaults.DefaultPage == "" || r.Defaults.DesignerRole == "" || r.Defaults.Validations == "" ||
		r.Defaults.SOPLinks == "" || r.Defaults.SupportChannel == "" || r.Defaults.Draft.Scope == "" {
		problems = append(problems, "every default must be stated")
	}
	if !r.Defaults.RunRecordsPageDefinitionVersion {
		problems = append(problems, "every run must record its page-definition version")
	}
	if r.Defaults.Draft.ExpiryDays != 30 {
		problems = append(problems, "draft expiry must be 30 days")
	}
	if r.Defaults.HistoryExport.MaxRows != 10000 || !r.Defaults.HistoryExport.RedactedToViewer {
		problems = append(problems, "history export must be capped at 10000 rows and redacted to the viewer")
	}
	if r.Guard.AddNodes || r.Guard.AddCapabilities || r.Guard.AddWorkflows || r.Guard.CustomPagesServe == "" {
		problems = append(problems, "the designer must not add nodes, capabilities or workflows")
	}
	if r.Budgets.WasmGzipGrowthBytes <= 0 || r.Budgets.Filter500WorkflowsMS <= 0 {
		problems = append(problems, "bundle and filter budgets must be fixed")
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalid, strings.Join(problems, "; "))
	}
	return nil
}
