package wcag

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Evidence struct {
	Todo      string     `yaml:"todo"`
	Standard  string     `yaml:"standard"`
	Artifact  string     `yaml:"artifact"`
	Scenarios []Scenario `yaml:"scenarios"`
	Waivers   []Waiver   `yaml:"waivers"`
}
type Scenario struct {
	ID     string `yaml:"id"`
	Kind   string `yaml:"kind"`
	Status string `yaml:"status"`
	Method string `yaml:"method"`
}
type Waiver struct {
	ID         string `yaml:"id"`
	Criterion  string `yaml:"criterion"`
	Owner      string `yaml:"owner"`
	Severity   string `yaml:"severity"`
	Workaround string `yaml:"workaround"`
	Expires    string `yaml:"expires"`
	ApprovedBy string `yaml:"approved_by"`
}

func LoadEvidence() (Evidence, error) {
	_, file, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(file), "..", "..", "..", "definitions", "ux", "wcag", "ux-003-evidence.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		return Evidence{}, err
	}
	var e Evidence
	if err := yaml.Unmarshal(b, &e); err != nil {
		return Evidence{}, err
	}
	return e, nil
}

func (e Evidence) Validate() error {
	if e.Todo != "UX-003" || e.Standard != "WCAG 2.2 AA" || e.Artifact == "" {
		return fmt.Errorf("invalid UX-003 evidence identity")
	}
	if filepath.IsAbs(e.Artifact) || strings.HasPrefix(filepath.Clean(e.Artifact), ".."+string(filepath.Separator)) || filepath.Clean(e.Artifact) == ".." {
		return fmt.Errorf("evidence artifact must be a repository-relative path")
	}
	want := map[string]bool{"zoom-200": false, "reflow-400": false, "reduced-motion": false, "accessible-auth": false}
	seenScenarios := map[string]bool{}
	for _, s := range e.Scenarios {
		if _, ok := want[s.ID]; !ok {
			return fmt.Errorf("unknown scenario %q", s.ID)
		}
		if seenScenarios[s.ID] {
			return fmt.Errorf("duplicate scenario %q", s.ID)
		}
		seenScenarios[s.ID] = true
		if s.Kind == "" || (s.Status != "PASS" && s.Status != "PENDING") || strings.TrimSpace(s.Method) == "" {
			return fmt.Errorf("scenario %q lacks kind/status/method", s.ID)
		}
		want[s.ID] = true
	}
	for id, ok := range want {
		if !ok {
			return fmt.Errorf("missing named scenario %q", id)
		}
	}
	seenWaivers := map[string]bool{}
	for _, w := range e.Waivers {
		if w.ID == "" || w.Criterion == "" || w.Owner == "" || w.Severity == "" || w.Workaround == "" || w.Expires == "" || w.ApprovedBy == "" {
			return fmt.Errorf("waiver %q is incomplete", w.ID)
		}
		if seenWaivers[w.ID] {
			return fmt.Errorf("duplicate waiver %q", w.ID)
		}
		seenWaivers[w.ID] = true
		if _, err := time.Parse("2006-01-02", w.Expires); err != nil {
			return fmt.Errorf("waiver %q has invalid expiry %q", w.ID, w.Expires)
		}
	}
	return nil
}

// ReleaseReady rejects structurally valid evidence until every named manual
// scenario has a recorded pass. PENDING is intentionally not a waiver.
func (e Evidence) ReleaseReady() error {
	if err := e.Validate(); err != nil {
		return err
	}
	for _, s := range e.Scenarios {
		if s.Status != "PASS" {
			return fmt.Errorf("scenario %q has not passed", s.ID)
		}
	}
	today := time.Now().UTC().Format("2006-01-02")
	for _, w := range e.Waivers {
		if w.Expires < today {
			return fmt.Errorf("waiver %q expired on %s", w.ID, w.Expires)
		}
	}
	return nil
}
