package selectionbind

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotblueprint"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling"
)

// TestTodo_NEXT_002_Mutation starts from the fully consistent COMPLETE tree
// and applies every single-fact regression, one at a time, proving each is
// detected and attributed to the right binding or slot rather than silently
// absorbed. It then edits every bound artifact after signing (validly
// re-signed under the trusted key, so only the binding can catch it) and
// proves every one is reported as a stale binding by name.
func TestTodo_NEXT_002_Mutation(t *testing.T) {
	t.Run("every missing fact regresses COMPLETE with an attributed reason", func(t *testing.T) {
		wantReason := map[fix]string{
			fixProvider:     "SELECT-002: real provider-selection gate",
			fixJurisdiction: `SELECT-001: review_status "UNREVIEWED" is not releasable`,
			fixTopology:     "TOPOLOGY-001: no deployable topology decision artifact",
			fixCustomer:     "CUSTOMER-001: Instantiate reports BLOCKED",
			fixSLO:          "slot slo: COMMERCIAL-001 promises no SLO",
			fixThreat:       "THREAT-001: release blocked",
		}
		for _, missing := range allFixes {
			var fixes []fix
			for _, f := range allFixes {
				if f != missing {
					fixes = append(fixes, f)
				}
			}
			r := evaluate(t, buildFixture(t, fixes...))
			if r.Status != StatusIncomplete {
				t.Errorf("without %s: Status = %s - the regression was silently absorbed", missing, r.Status)
				continue
			}
			if !strings.Contains(joinedReasons(r), wantReason[missing]) {
				t.Errorf("without %s: reasons do not name %q:\n%s", missing, wantReason[missing], joinedReasons(r))
			}
		}
	})

	t.Run("topology decision disagreeing with the selected provider", func(t *testing.T) {
		f := buildFixture(t, allFixes...)
		d := fixtureDecision()
		d.Topology.Selection.ProviderRef = "some-other-vendor"
		rebindTopology(t, &f, d)
		r := evaluate(t, f)
		if r.Status != StatusIncomplete || !strings.Contains(joinedReasons(r), "disagrees with SELECT-002 vendor_id") {
			t.Fatalf("provider disagreement absorbed: %s\n%s", r.Status, joinedReasons(r))
		}
	})

	t.Run("placeholder topology decision", func(t *testing.T) {
		f := buildFixture(t, allFixes...)
		d := fixtureDecision()
		d.Topology.Selection.Region = "placeholder:region-selection"
		rebindTopology(t, &f, d)
		r := evaluate(t, f)
		if !strings.Contains(joinedReasons(r), "HUMAN_SELECTION_REQUIRED") {
			t.Fatalf("placeholder topology absorbed: %s\n%s", r.Status, joinedReasons(r))
		}
	})

	t.Run("an SLO-premise exemption never waives any other commercial rule", func(t *testing.T) {
		f := buildFixture(t, allFixes...)
		freezePath := filepath.Join(f.root, filepath.FromSlash(commercialPath))
		freeze := mustLoad(t, freezePath, loadFreeze)
		freeze.Billing.ReplayBillable = true
		resignAndRebind(t, &f, "COMMERCIAL-001", commercialPath, signFreezeWith(t, freeze, f))
		r := evaluate(t, f)
		if bindingByTodo(r, "COMMERCIAL-001").Ready || !strings.Contains(joinedReasons(r), "billing.replay_billable") {
			t.Fatalf("replay-billable freeze accepted while an SLO is selected:\n%s", joinedReasons(r))
		}
	})

	t.Run("every bound artifact edited after signing is a stale binding", func(t *testing.T) {
		edits := map[string]func(t *testing.T, f fixture){
			"PHASE-001": func(t *testing.T, f fixture) {
				c := mustLoad(t, filepath.Join(f.root, filepath.FromSlash(ceilingPath)), scopeceiling.LoadManifest)
				c.FreshnessWindowDays++
				signed, err := scopeceiling.SignManifest(*c, f.priv, f.pub, keyFixture)
				if err != nil {
					t.Fatal(err)
				}
				writeYAML(t, f.root, ceilingPath, signed)
			},
			"SELECT-002": func(t *testing.T, f fixture) {
				p := mustLoad(t, filepath.Join(f.root, filepath.FromSlash(providerPath)), pilotprovider.LoadTopology)
				p.Provider.Region = "fixture-region-2"
				signed, err := pilotprovider.SignTopology(*p, f.priv, f.pub, keyFixture)
				if err != nil {
					t.Fatal(err)
				}
				writeYAML(t, f.root, providerPath, signed)
			},
			"CUSTOMER-001": func(t *testing.T, f fixture) {
				b := mustLoad(t, filepath.Join(f.root, filepath.FromSlash(blueprintPath)), pilotblueprint.LoadBlueprint)
				b.TemplateVersion = "1.0.1"
				signed, err := pilotblueprint.SignBlueprint(*b, f.priv, f.pub, keyFixture)
				if err != nil {
					t.Fatal(err)
				}
				writeYAML(t, f.root, blueprintPath, signed)
			},
			"TOPOLOGY-001": func(t *testing.T, f fixture) {
				writeFile(t, f.root, fixtureTopologyPath, []byte(`{"topology":{},"deploy":{}}`))
			},
			"SELECT-001":     func(t *testing.T, f fixture) { editSignedDate(t, f, jurisdictionPath) },
			"COMMERCIAL-001": func(t *testing.T, f fixture) { editSignedDate(t, f, commercialPath) },
			"THREAT-001":     func(t *testing.T, f fixture) { editSignedDate(t, f, threatPath) },
		}
		for _, todo := range gateevidence.RequiredSelectionBindingTodoIDs {
			t.Run(todo, func(t *testing.T) {
				f := buildFixture(t, allFixes...)
				edits[todo](t, f)
				r := evaluate(t, f)
				b := bindingByTodo(r, todo)
				if r.Status != StatusIncomplete || b.Ready || !strings.Contains(strings.Join(b.Reasons, "|"), "stale binding") {
					t.Fatalf("editing %s after signing was not reported stale: status %s, reasons %v", todo, r.Status, b.Reasons)
				}
			})
		}
	})
}
