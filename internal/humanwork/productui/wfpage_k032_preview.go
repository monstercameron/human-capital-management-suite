package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/testprofile"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/widgetreg"
)

type WorkflowPagePreviewPersona string

const (
	WorkflowPagePreviewRecruiter     WorkflowPagePreviewPersona = "recruiter"
	WorkflowPagePreviewHiringManager WorkflowPagePreviewPersona = "hiring_manager"
	WorkflowPagePreviewHRPartner     WorkflowPagePreviewPersona = "hr_partner"
)

type WorkflowPagePreviewCheck struct {
	Name   string
	Passed bool
	Detail string
}

type WorkflowPagePreviewRequest struct {
	Tenant       string
	Persona      WorkflowPagePreviewPersona
	Profile      testprofile.ProfileRef
	Profiles     *testprofile.Registry
	Plan         *workflow.CompiledWorkflow
	Page         pagedef.WorkflowPageDefinition
	Copy         map[string]WorkflowPageLocalizedMarkdown
	ValueDomains map[string][]string
}

type WorkflowPagePreviewResult struct {
	Tenant        string
	Persona       WorkflowPagePreviewPersona
	Profile       testprofile.ProfileRef
	Values        map[string]string
	Checks        []WorkflowPagePreviewCheck
	Passed        bool
	RunStarted    bool
	Writes        int
	FixtureBytes  []byte
	FixtureDigest string
}

// PreviewWorkflowPage is a read-only draft projection. It resolves a named
// tenant-scoped fake-data profile, validates the page against its compiled
// workflow, and records deterministic release-fixture evidence in memory.
func PreviewWorkflowPage(request WorkflowPagePreviewRequest) (WorkflowPagePreviewResult, error) {
	if !validWorkflowPagePreviewPersona(request.Persona) {
		return WorkflowPagePreviewResult{}, fmt.Errorf("productui: unsupported workflow page preview persona %q", request.Persona)
	}
	if request.Profiles == nil {
		return WorkflowPagePreviewResult{}, fmt.Errorf("productui: preview fake-data registry is required")
	}
	profile, err := request.Profiles.ResolveForTenant(request.Profile, request.Tenant)
	if err != nil {
		return WorkflowPagePreviewResult{}, fmt.Errorf("productui: resolve preview profile: %w", err)
	}
	violations := request.Page.ValidateAgainst(request.Plan)
	if len(violations) > 0 {
		return WorkflowPagePreviewResult{}, fmt.Errorf("productui: preview page is invalid at %s: %s", violations[0].Path, violations[0].Reason)
	}
	values := previewProfileValues(profile)
	checks := make([]WorkflowPagePreviewCheck, 0, len(request.Page.Widgets)+len(request.Page.ContentBlocks)+3)
	add := func(name string, passed bool, detail string) {
		checks = append(checks, WorkflowPagePreviewCheck{Name: name, Passed: passed, Detail: detail})
	}
	add("translations", previewTranslations(request.Page, request.Copy), "en-US, de-DE and ar copy is present")
	add("landmarks", len(request.Page.Accessibility.Landmarks) > 0, "the page declares a landmark")
	add("contrast", previewContrastContract(request.Page), "the page declares a governed brand color token")
	registry := widgetreg.NewWorkflowInputRegistry()
	for _, pageWidget := range request.Page.Widgets {
		registered, ok := registry.Lookup(pageWidget.Kind)
		add("label:"+pageWidget.ID, ok && strings.TrimSpace(pageWidget.Label) != "", "every control has a closed semantic role and label")
		if ok {
			add("keyboard:"+pageWidget.ID, registered.Role != "", "the control is reachable through the semantic input registry")
		}
	}
	for _, rule := range request.Page.Rules {
		never := workflowPageRuleNeverPasses(rule.When, request.ValueDomains)
		add("rule:"+rule.ID, !never, "the validation rule has at least one possible sample value")
	}
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].Name < checks[j].Name })
	result := WorkflowPagePreviewResult{Tenant: request.Tenant, Persona: request.Persona, Profile: request.Profile, Values: values, Checks: checks}
	result.Passed = true
	for _, check := range checks {
		if !check.Passed {
			result.Passed = false
		}
	}
	result.FixtureBytes = encodeWorkflowPagePreviewFixture(result)
	sum := sha256.Sum256(result.FixtureBytes)
	result.FixtureDigest = "sha256:" + hex.EncodeToString(sum[:])
	return result, nil
}

func (result WorkflowPagePreviewResult) ReleaseFixture() []byte {
	return append([]byte(nil), result.FixtureBytes...)
}

func validWorkflowPagePreviewPersona(persona WorkflowPagePreviewPersona) bool {
	switch persona {
	case WorkflowPagePreviewRecruiter, WorkflowPagePreviewHiringManager, WorkflowPagePreviewHRPartner:
		return true
	default:
		return false
	}
}

func previewTranslations(page pagedef.WorkflowPageDefinition, copy map[string]WorkflowPageLocalizedMarkdown) bool {
	for _, block := range page.ContentBlocks {
		localized, ok := copy[block.TextKey]
		if !ok || ValidateWorkflowPageLocalizedMarkdown(localized) != nil {
			return false
		}
	}
	return true
}

func previewContrastContract(page pagedef.WorkflowPageDefinition) bool {
	for _, token := range page.BrandTokens {
		if strings.HasPrefix(token, "brand.color.") {
			return true
		}
	}
	return false
}

func previewProfileValues(profile testprofile.Profile) map[string]string {
	values := make(map[string]string)
	for _, response := range profile.Responses {
		for _, field := range response.Fields {
			if _, exists := values[field.Path]; !exists {
				values[field.Path] = field.Value
			}
		}
	}
	return values
}

func workflowPageRuleNeverPasses(condition pagedef.WorkflowCondition, domains map[string][]string) bool {
	if condition.Op != pagedef.RuleEquals || len(domains) == 0 {
		return false
	}
	allowed, ok := domains[condition.Path]
	if !ok || len(allowed) == 0 {
		return false
	}
	for _, value := range allowed {
		if value == condition.Value {
			return false
		}
	}
	return true
}

func encodeWorkflowPagePreviewFixture(result WorkflowPagePreviewResult) []byte {
	fixture := struct {
		Tenant  string                     `json:"tenant"`
		Persona WorkflowPagePreviewPersona `json:"persona"`
		Profile testprofile.ProfileRef     `json:"profile"`
		Values  map[string]string          `json:"values"`
		Checks  []WorkflowPagePreviewCheck `json:"checks"`
		Passed  bool                       `json:"passed"`
	}{result.Tenant, result.Persona, result.Profile, result.Values, result.Checks, result.Passed}
	encoded, _ := json.Marshal(fixture)
	return encoded
}
