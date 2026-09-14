package scopeceiling

import (
	"strings"
	"testing"
)

// bannedProviderNames catches a hidden provider dependency naming a real
// vendor product directly instead of leaving the field/effect bound to the
// UnboundProviderSentinel until SELECT-002 selects one.
var bannedProviderNames = []string{
	"workday", "sap successfactors", "ukg", "adp", "oracle hcm",
	"bamboohr", "ceridian", "dayforce", "paycom", "namely", "gusto",
}

// TestPhaseOneManifestContainsOnlyAuthorizedSourceBoundExecutableScope is
// PHASE-001's named PRIMARY TEST. It enforces every RED clause as its own
// subtest against the checked-in, signed
// definitions/planning/gates/phase1-scope-ceiling.yaml: source-unbound
// intent, future native payroll/WFM/talent ownership, endpoint without
// disposition, capability without owner, workflow without vertical slice,
// hidden provider dependency, and missing include/defer/reject rationale.
func TestPhaseOneManifestContainsOnlyAuthorizedSourceBoundExecutableScope(t *testing.T) {
	m := mustLoadCeiling(t)

	t.Run("no source-unbound intent: ceiling intents are exactly the live capability-coverage.yaml intent set", func(t *testing.T) {
		// definitions/planning/capability-coverage.yaml's `kind: intent`
		// entries key by the unversioned intent_type_id
		// (e.g. "hcmnext.people.promote_worker"), while the ceiling and
		// business-intent-catalog.md use the versioned definition_ref
		// (".../v1"); strip the version suffix before comparing identity.
		live := liveIntentIDs(t)
		if len(m.Intents) != len(live) {
			t.Fatalf("ceiling names %d intents, live capability-coverage.yaml names %d", len(m.Intents), len(live))
		}
		for _, it := range m.Intents {
			base := baseIntentID(it.ID)
			src, ok := live[base]
			if !ok {
				t.Errorf("intent %s is source-unbound: %s not present in definitions/planning/capability-coverage.yaml", it.ID, base)
				continue
			}
			if !strings.EqualFold(src.OwnerDomain, it.OwnerDomain) {
				t.Errorf("intent %s: ceiling owner_domain %q disagrees with live source %q", it.ID, it.OwnerDomain, src.OwnerDomain)
			}
		}
		seen := map[string]bool{}
		for _, it := range m.Intents {
			seen[baseIntentID(it.ID)] = true
		}
		for id := range live {
			if !seen[id] {
				t.Errorf("live source-bound intent %s is missing from the ceiling", id)
			}
		}
	})

	t.Run("no source-unbound capability: ceiling capabilities are exactly the live capability-coverage.yaml capability set", func(t *testing.T) {
		live := liveCapabilityIDs(t)
		if len(m.Capabilities) != len(live) {
			t.Fatalf("ceiling names %d capabilities, live capability-coverage.yaml names %d", len(m.Capabilities), len(live))
		}
		for _, c := range m.Capabilities {
			if _, ok := live[c.ID]; !ok {
				t.Errorf("capability %s is source-unbound: not present in definitions/planning/capability-coverage.yaml", c.ID)
			}
		}
	})

	t.Run("no future native payroll/WFM/talent ownership is INCLUDE", func(t *testing.T) {
		for _, it := range m.Intents {
			if it.Disposition == Include && domainTokenHit(it.ID, it.OwnerDomain) {
				t.Errorf("intent %s (owner %s) is INCLUDE and matches a deferred native-ownership token", it.ID, it.OwnerDomain)
			}
		}
		for _, c := range m.Capabilities {
			if c.Disposition == Include && domainTokenHit(c.ID, c.OwnerDomain) {
				t.Errorf("capability %s (owner %s) is INCLUDE and matches a deferred native-ownership token", c.ID, c.OwnerDomain)
			}
		}
		for _, mo := range m.Models {
			if mo.Disposition == Include && domainTokenHit(mo.ID, mo.SpecRef) {
				t.Errorf("model %s is INCLUDE and matches a deferred native-ownership token", mo.ID)
			}
		}
	})

	t.Run("no endpoint without disposition", func(t *testing.T) {
		if len(m.Endpoints) == 0 {
			t.Fatal("ceiling names zero endpoints")
		}
		for _, e := range m.Endpoints {
			if !e.EndpointDisposition.valid() {
				t.Errorf("endpoint %s has no valid endpoint_disposition (got %q)", e.EndpointID, e.EndpointDisposition)
			}
		}
	})

	t.Run("live generated endpoints are present with matching phase and INCLUDE disposition", func(t *testing.T) {
		live := liveEndpoints(t)
		if len(live) == 0 {
			t.Fatal("definitions/api/endpoint-manifest.json names zero endpoints")
		}
		for id, entry := range live {
			ce, ok := findEndpoint(m, id)
			if !ok {
				t.Errorf("live generated endpoint %s is missing from the ceiling", id)
				continue
			}
			if !ce.Generated {
				t.Errorf("endpoint %s exists in the generated endpoint manifest but the ceiling marks it generated: false", id)
			}
			if ce.Disposition != Include {
				t.Errorf("endpoint %s exists in the generated endpoint manifest but the ceiling disposition is %q, want INCLUDE", id, ce.Disposition)
			}
			wantGate := Gate("P1A")
			if entry.Phase == "GATE_B" {
				wantGate = "P1B"
			}
			if ce.Gate != wantGate {
				t.Errorf("endpoint %s: ceiling gate %q disagrees with live phase %q", id, ce.Gate, entry.Phase)
			}
		}
	})

	t.Run("no capability without owner", func(t *testing.T) {
		for _, c := range m.Capabilities {
			if strings.TrimSpace(c.OwnerDomain) == "" {
				t.Errorf("capability %s has no owner_domain", c.ID)
			}
		}
	})

	t.Run("no workflow without vertical slice", func(t *testing.T) {
		slices := liveSliceIDs(t)
		if len(m.Workflows) == 0 {
			t.Fatal("ceiling names zero workflows")
		}
		for _, w := range m.Workflows {
			if strings.TrimSpace(w.VerticalSlice) == "" {
				t.Errorf("workflow %s has no vertical_slice", w.ID)
				continue
			}
			if !slices[w.VerticalSlice] {
				t.Errorf("workflow %s names vertical_slice %q, which does not exist in definitions/planning/product-slices.yaml", w.ID, w.VerticalSlice)
			}
		}
	})

	t.Run("no hidden provider dependency", func(t *testing.T) {
		for _, e := range m.Effects {
			if e.ProviderDependency == "" {
				continue
			}
			if e.ProviderDependency != UnboundProviderSentinel {
				t.Errorf("effect %s names a concrete provider dependency %q instead of the unbound sentinel", e.Class, e.ProviderDependency)
			}
		}
		lower := func(s string) string { return strings.ToLower(s) }
		checkText := func(context, text string) {
			l := lower(text)
			for _, banned := range bannedProviderNames {
				if strings.Contains(l, banned) {
					t.Errorf("%s rationale names a concrete provider %q before SELECT-002 has selected one", context, banned)
				}
			}
		}
		for _, e := range m.Effects {
			checkText("effect "+string(e.Class), e.Rationale)
		}
		for _, c := range m.Capabilities {
			checkText("capability "+c.ID, c.Rationale)
		}
		for _, s := range m.SelectionSlots {
			if s.Name == "provider" {
				if s.Filled || s.Value != "" {
					t.Errorf("provider selection slot must remain empty; got filled=%v value=%q", s.Filled, s.Value)
				}
			}
		}
	})

	t.Run("every scope item carries an explicit include/defer/reject rationale", func(t *testing.T) {
		for _, it := range m.Intents {
			if !it.Disposition.valid() || strings.TrimSpace(it.Rationale) == "" {
				t.Errorf("intent %s lacks a valid disposition+rationale", it.ID)
			}
		}
		for _, c := range m.Capabilities {
			if !c.Disposition.valid() || strings.TrimSpace(c.Rationale) == "" {
				t.Errorf("capability %s lacks a valid disposition+rationale", c.ID)
			}
		}
		for _, w := range m.Workflows {
			if !w.Disposition.valid() || strings.TrimSpace(w.Rationale) == "" {
				t.Errorf("workflow %s lacks a valid disposition+rationale", w.ID)
			}
		}
		for _, uf := range m.UserFlows {
			if !uf.Disposition.valid() || strings.TrimSpace(uf.Rationale) == "" {
				t.Errorf("user flow %s lacks a valid disposition+rationale", uf.ID)
			}
		}
		for _, e := range m.Endpoints {
			if !e.Disposition.valid() || strings.TrimSpace(e.Rationale) == "" {
				t.Errorf("endpoint %s lacks a valid disposition+rationale", e.EndpointID)
			}
		}
		for _, mo := range m.Models {
			if !mo.Disposition.valid() || strings.TrimSpace(mo.Rationale) == "" {
				t.Errorf("model %s lacks a valid disposition+rationale", mo.ID)
			}
		}
		for _, e := range m.Effects {
			if !e.Disposition.valid() || strings.TrimSpace(e.Rationale) == "" {
				t.Errorf("effect %s lacks a valid disposition+rationale", e.Class)
			}
		}
		for _, d := range m.DeferredDomains {
			if strings.TrimSpace(d.Rationale) == "" {
				t.Errorf("deferred domain %s lacks a rationale", d.Name)
			}
		}
	})

	t.Run("selection slots are exactly the four required names, all empty", func(t *testing.T) {
		want := map[string]bool{"provider": true, "jurisdiction": true, "topology": true, "slo": true}
		if len(m.SelectionSlots) != len(want) {
			t.Fatalf("got %d selection slots, want %d", len(m.SelectionSlots), len(want))
		}
		for _, s := range m.SelectionSlots {
			if !want[s.Name] {
				t.Errorf("unexpected selection slot %q", s.Name)
			}
			delete(want, s.Name)
			if s.Filled {
				t.Errorf("selection slot %q must not be filled", s.Name)
			}
			if s.Value != "" {
				t.Errorf("selection slot %q must not carry a value, got %q", s.Name, s.Value)
			}
			if strings.TrimSpace(s.FillingTodoID) == "" {
				t.Errorf("selection slot %q must name the todo that fills it", s.Name)
			}
		}
		if len(want) != 0 {
			t.Errorf("missing selection slots: %v", want)
		}
	})

	t.Run("the manifest is signed and verifies", func(t *testing.T) {
		ok, err := VerifyManifestSignature(m)
		if err != nil {
			t.Fatalf("VerifyManifestSignature: %v", err)
		}
		if !ok {
			t.Fatal("the checked-in phase1-scope-ceiling.yaml must verify against its own signature")
		}
	})
}
