package agentobo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

type agentDocumentDecision struct {
	SchemaVersion  int    `yaml:"schema_version"`
	DecisionID     string `yaml:"decision_id"`
	Status         string `yaml:"status"`
	DecisionDate   string `yaml:"decision_date"`
	Owner          string `yaml:"owner"`
	ReferenceSites []struct {
		ID                 string `yaml:"id"`
		Author             string `yaml:"author"`
		DefaultVersionMode string `yaml:"default_version_mode"`
		MaximumReferences  int    `yaml:"maximum_references"`
	} `yaml:"reference_sites"`
	ReferenceShape struct {
		Fields            []string `yaml:"fields"`
		VersionModes      []string `yaml:"version_modes"`
		PinnedVersionRule string   `yaml:"pinned_version_rule"`
	} `yaml:"reference_shape"`
	Limits struct {
		PersonaReferences       int    `yaml:"persona_references"`
		RequestReferences       int    `yaml:"request_references"`
		ContentCharactersPerRun int    `yaml:"content_characters_per_run"`
		LabelRunes              int    `yaml:"label_runes"`
		OverflowRule            string `yaml:"overflow_rule"`
	} `yaml:"limits"`
	PersonaSeal struct {
		IncludedInProfileDigest bool `yaml:"included_in_profile_digest"`
		OrderSignificant        bool `yaml:"order_significant"`
		ChangeCreatesNewVersion bool `yaml:"change_creates_new_version"`
		ReviewRequired          bool `yaml:"review_required"`
		EvaluationRequired      bool `yaml:"evaluation_required"`
	} `yaml:"persona_seal"`
	RuntimeAccess struct {
		Principal             string `yaml:"principal"`
		Timing                string `yaml:"timing"`
		Authorization         string `yaml:"authorization"`
		UnreadableReference   string `yaml:"unreadable_reference"`
		VisibleDocumentNotice string `yaml:"visible_document_notice"`
		HiddenDocumentNotice  string `yaml:"hidden_document_notice"`
	} `yaml:"runtime_access"`
	Authority struct {
		Classification    string `yaml:"classification"`
		PlanningAuthority string `yaml:"planning_authority"`
		GoverningControl  string `yaml:"governing_control"`
		MayAddSkills      bool   `yaml:"may_add_skills"`
		MayAddRecipients  bool   `yaml:"may_add_recipients"`
		MayRaiseTiers     bool   `yaml:"may_raise_tiers"`
		AnswerCitation    string `yaml:"answer_citation"`
	} `yaml:"authority"`
}

func loadAgentDocumentDecision(t *testing.T) (agentDocumentDecision, []byte) {
	t.Helper()
	name := filepath.Join(repoRoot, "definitions", "planning", "agent-document-reference-decisions.yaml")
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read canonical document-reference decision: %v", err)
	}
	var record agentDocumentDecision
	if err := yaml.Unmarshal(b, &record); err != nil {
		t.Fatalf("parse canonical document-reference decision: %v", err)
	}
	return record, b
}

func TestTodo_AGENTDOC_001(t *testing.T) {
	record, _ := loadAgentDocumentDecision(t)
	if record.SchemaVersion != 1 || record.DecisionID != "AGENTDOC-001" || record.Status != "DECIDED" || record.DecisionDate != "2026-09-30" || record.Owner == "" {
		t.Fatalf("decision identity = %+v", record)
	}
	if len(record.ReferenceSites) != 2 || record.ReferenceSites[0].ID != "PERSONA_VERSION" || record.ReferenceSites[0].Author != "persona_administrator" || record.ReferenceSites[0].DefaultVersionMode != "PINNED" || record.ReferenceSites[1].ID != "USER_REQUEST" || record.ReferenceSites[1].Author != "invoking_user" || record.ReferenceSites[1].DefaultVersionMode != "LATEST_PUBLISHED" {
		t.Fatalf("reference-site defaults = %+v", record.ReferenceSites)
	}
	if !reflect.DeepEqual(record.ReferenceShape.Fields, []string{"document_id", "version_mode", "pinned_version", "section_anchor", "label"}) || !reflect.DeepEqual(record.ReferenceShape.VersionModes, []string{"PINNED", "LATEST_PUBLISHED"}) || record.ReferenceShape.PinnedVersionRule == "" {
		t.Fatalf("reference shape = %+v", record.ReferenceShape)
	}
}

func TestTodo_AGENTDOC_001_Golden(t *testing.T) {
	record, b := loadAgentDocumentDecision(t)
	var golden struct {
		CanonicalDigest string   `json:"canonical_digest"`
		Sites           []string `json:"sites"`
		Modes           []string `json:"modes"`
	}
	goldenBytes, err := os.ReadFile("testdata/agentdoc_decisions_golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if err := json.Unmarshal(goldenBytes, &golden); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if got := canonicalDigest(t, b); got != golden.CanonicalDigest {
		t.Fatalf("decision canonical digest = %s, want %s", got, golden.CanonicalDigest)
	}
	if got := []string{record.ReferenceSites[0].ID, record.ReferenceSites[1].ID}; !reflect.DeepEqual(got, golden.Sites) || !reflect.DeepEqual(record.ReferenceShape.VersionModes, golden.Modes) {
		t.Fatalf("golden decision shape changed: sites=%v modes=%v", got, record.ReferenceShape.VersionModes)
	}
}

func TestTodo_AGENTDOC_001_Security(t *testing.T) {
	record, _ := loadAgentDocumentDecision(t)
	if record.ReferenceSites[0].MaximumReferences != 8 || record.ReferenceSites[1].MaximumReferences != 5 || record.Limits.PersonaReferences != 8 || record.Limits.RequestReferences != 5 || record.Limits.ContentCharactersPerRun != 48000 || record.Limits.LabelRunes != 120 || record.Limits.OverflowRule == "" {
		t.Fatalf("missing bounded defaults: sites=%+v limits=%+v", record.ReferenceSites, record.Limits)
	}
	if !record.PersonaSeal.IncludedInProfileDigest || !record.PersonaSeal.OrderSignificant || !record.PersonaSeal.ChangeCreatesNewVersion || !record.PersonaSeal.ReviewRequired || !record.PersonaSeal.EvaluationRequired {
		t.Fatalf("missing persona seal defaults: %+v", record.PersonaSeal)
	}
	if record.RuntimeAccess.Principal != "invoking_user" || record.RuntimeAccess.Timing != "RUN_TIME" || record.RuntimeAccess.Authorization == "" || record.RuntimeAccess.UnreadableReference != "OMIT" || record.RuntimeAccess.VisibleDocumentNotice == "" || record.RuntimeAccess.HiddenDocumentNotice == "" {
		t.Fatalf("missing current-user access or omission defaults: %+v", record.RuntimeAccess)
	}
	if record.Authority.Classification != "QUARANTINED_REFERENCE_DATA" || record.Authority.PlanningAuthority != "NONE" || record.Authority.GoverningControl != "AGENT2-015" || record.Authority.MayAddSkills || record.Authority.MayAddRecipients || record.Authority.MayRaiseTiers || record.Authority.AnswerCitation == "" {
		t.Fatalf("referenced content gained authority or lost citation: %+v", record.Authority)
	}
}
