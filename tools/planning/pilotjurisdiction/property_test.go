package pilotjurisdiction

import (
	"strings"
	"testing"
)

// TestTodo_SELECT_001_Property is SELECT-001's PROPERTY test. It proves the
// GREEN invariant - every RED element Validate checks for actually causes a
// violation naming the exact broken field when removed, and a structurally
// complete profile with none of those defects validates clean - holds
// across the whole schema, not just the one checked-in file.
func TestTodo_SELECT_001_Property(t *testing.T) {
	base := validFixture()
	if v := base.Validate(); len(v) != 0 {
		t.Fatalf("validFixture() must validate clean, got: %v", v)
	}

	cases := []struct {
		name    string
		mutate  func(*JurisdictionProfile)
		wantHit string
	}{
		{"wrong todo id", func(p *JurisdictionProfile) { p.TodoID = "SELECT-002" }, "todo_id"},
		{"invalid jurisdiction", func(p *JurisdictionProfile) { p.Jurisdiction = JurisdictionRef{Country: "us", State: "ca"} }, "jurisdiction"},
		{"invalid review status", func(p *JurisdictionProfile) { p.ReviewStatus = "MADE_UP_STATUS" }, "review_status"},

		{"no sources", func(p *JurisdictionProfile) { p.Sources = nil }, "sources"},
		{"source missing path", func(p *JurisdictionProfile) { p.Sources[0].Path = "" }, "sources[0].path"},
		{"source missing content digest", func(p *JurisdictionProfile) { p.Sources[0].ContentDigest = "" }, "sources[0].content_digest"},
		{"rule pack source missing pack id", func(p *JurisdictionProfile) { p.Sources[1].PackID = "" }, "sources[1].pack_id"},
		{"rule pack source missing vocabulary version", func(p *JurisdictionProfile) { p.Sources[1].VocabularyVersion = 0 }, "sources[1].vocabulary_version"},

		{"reviewer name missing", func(p *JurisdictionProfile) { p.Reviewer.Name = "" }, "reviewer.name"},
		{"reviewer qualification missing when named", func(p *JurisdictionProfile) { p.Reviewer.Qualification = "" }, "reviewer.qualification"},

		{"no scope items", func(p *JurisdictionProfile) { p.ScopeItems = nil }, "scope_items"},
		{"scope item missing rationale", func(p *JurisdictionProfile) { p.ScopeItems[0].Rationale = "" }, "scope_items[0].rationale"},
		{"scope item wrong disposition", func(p *JurisdictionProfile) { p.ScopeItems[0].Disposition = "MAYBE" }, "scope_items[0].disposition"},

		{"obligation mapping unknown kind", func(p *JurisdictionProfile) { p.ObligationMappings[0].Kind = "NOT_A_KIND" }, "obligation_mappings[0].kind"},
		{"obligation mapping missing intent", func(p *JurisdictionProfile) { p.ObligationMappings[0].IntentID = "" }, "obligation_mappings[0].intent_id"},
		{"obligation mapping missing evidence path", func(p *JurisdictionProfile) { p.ObligationMappings[0].EvidencePath = "" }, "obligation_mappings[0].evidence_path"},
		{"obligation mapping duplicate kind", func(p *JurisdictionProfile) {
			p.ObligationMappings = append(p.ObligationMappings, p.ObligationMappings[0])
		}, "duplicate mapping"},

		{"no exclusions at all", func(p *JurisdictionProfile) { p.Exclusions = nil }, "exclusions"},
		{"exclusion missing reason", func(p *JurisdictionProfile) { p.Exclusions[0].Reason = "" }, "exclusions[0].reason"},
		{"exclusion unknown kind", func(p *JurisdictionProfile) { p.Exclusions[0].Kind = "SOMETHING_ELSE" }, "exclusions[0].kind"},
		{"exclusion names unknown obligation kind", func(p *JurisdictionProfile) { p.Exclusions[0].Value = "NOT_A_KIND" }, "exclusions[0].value"},
		{"no jurisdiction exclusion", func(p *JurisdictionProfile) {
			var kept []Exclusion
			for _, e := range p.Exclusions {
				if e.Kind != ExclusionKindJurisdiction {
					kept = append(kept, e)
				}
			}
			p.Exclusions = kept
		}, "JURISDICTION exclusion"},
		{"obligation kind both mapped and excluded", func(p *JurisdictionProfile) {
			p.Exclusions = append(p.Exclusions, Exclusion{
				Kind: ExclusionKindObligation, Value: p.ObligationMappings[0].Kind, Reason: "double-booked",
			})
		}, "declared both mapped"},
		{"obligation kind neither mapped nor excluded", func(p *JurisdictionProfile) {
			// Drop the first OBLIGATION_KIND exclusion, leaving that kind
			// with no mapping and no exclusion.
			for i, e := range p.Exclusions {
				if e.Kind == ExclusionKindObligation {
					p.Exclusions = append(p.Exclusions[:i], p.Exclusions[i+1:]...)
					return
				}
			}
			t.Fatal("fixture has no OBLIGATION_KIND exclusion to remove")
		}, "obligation_coverage"},

		{"window missing effective start", func(p *JurisdictionProfile) { p.Window.EffectiveStart = "" }, "window.effective_start"},
		{"window missing known-at start", func(p *JurisdictionProfile) { p.Window.KnownAtStart = "" }, "window.known_at_start"},

		{"collective missing cba assumption", func(p *JurisdictionProfile) { p.Collective.CBAAssumption = "" }, "collective.cba_assumption"},
		{"collective missing multi entity handling", func(p *JurisdictionProfile) { p.Collective.MultiEntityHandling = "" }, "collective.multi_entity_handling"},

		{"uncertainty ambiguous status invalid", func(p *JurisdictionProfile) { p.Uncertainty.AmbiguousInputStatus = "GUESS" }, "uncertainty.ambiguous_input_status"},
		{"uncertainty out of scope status invalid", func(p *JurisdictionProfile) { p.Uncertainty.OutOfScopeStatus = "GUESS" }, "uncertainty.out_of_scope_status"},

		{"update sla cadence non-positive", func(p *JurisdictionProfile) { p.UpdateSLA.ReviewCadenceDays = 0 }, "update_sla.review_cadence_days"},
		{"update sla no trigger events", func(p *JurisdictionProfile) { p.UpdateSLA.TriggerEvents = nil }, "update_sla.trigger_events"},

		{"legal advice disclaimer empty", func(p *JurisdictionProfile) { p.LegalAdviceDisclaimer = "" }, "legal_advice_disclaimer"},
		{"legal advice disclaimer missing required phrase", func(p *JurisdictionProfile) {
			p.LegalAdviceDisclaimer = "This profile is thorough and complete."
		}, "legal_advice_disclaimer"},

		{"no stop/reselect thresholds", func(p *JurisdictionProfile) { p.StopReselectThresholds = nil }, "stop_reselect_thresholds"},
		{"stop/reselect threshold unknown action", func(p *JurisdictionProfile) { p.StopReselectThresholds[0].Action = "MAYBE" }, "stop_reselect_thresholds[0].action"},

		{"no signature", func(p *JurisdictionProfile) { p.Signature = nil }, "signature"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := deepCopy(base)
			tc.mutate(&p)
			violations := p.Validate()
			if len(violations) == 0 {
				t.Fatalf("expected a violation for %q, got none", tc.name)
			}
			found := false
			for _, v := range violations {
				if strings.Contains(v.String(), tc.wantHit) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected a violation containing %q, got %v", tc.wantHit, violations)
			}
		})
	}
}
