package crm

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func conversionAt(t *testing.T, s string) values.Instant {
	t.Helper()
	x, e := time.Parse(time.RFC3339, s)
	if e != nil {
		t.Fatal(e)
	}
	return values.NewInstant(x)
}
func TestTodo_CRM_005(t *testing.T) {
	p := validProspect(t)
	got, rej, err := PrepareProspectConversion(p, crmRef("candidate", "c-1"), crmRef("application", "a-1"), crmRef("identity_link", "i-1"), CRM005IntentType, CRM005IntentVersion, conversionAt(t, "2026-01-03T00:00:00Z"))
	if err != nil || rej != nil {
		t.Fatalf("conversion=%+v rejection=%+v err=%v", got, rej, err)
	}
	if got.Source.Campaign != p.Attribution.Campaign || got.Consent.Authority != p.Consent.Authority {
		t.Fatal("provenance was not preserved")
	}

	// Seeded defect: a prospect converts without source lineage and without
	// consent. Both refuse with CRM_005_REJECTED naming field/state/version,
	// and neither writes anything: the proposal stays zero.
	unattributed := validProspect(t)
	unattributed.Attribution.Campaign = ""
	empty, rej, err := PrepareProspectConversion(unattributed, crmRef("candidate", "c-1"), crmRef("application", "a-1"), crmRef("identity_link", "i-1"), CRM005IntentType, CRM005IntentVersion, conversionAt(t, "2026-01-03T00:00:00Z"))
	if !errors.Is(err, ErrCRM005Rejected) || rej == nil || rej.Field != "source.campaign" || rej.State != "UNBOUND" || rej.Version != unattributed.Revision {
		t.Fatalf("lineage rejection=%+v err=%v", rej, err)
	}
	if !reflect.DeepEqual(empty, CandidateConversionProposal{}) {
		t.Fatalf("refused conversion wrote a proposal: %+v", empty)
	}
	nonconsensual := validProspect(t)
	withdrawn, err := nonconsensual.Consent.Withdraw(conversionAt(t, "2026-01-02T12:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	nonconsensual.Consent = withdrawn
	empty, rej, err = PrepareProspectConversion(nonconsensual, crmRef("candidate", "c-1"), crmRef("application", "a-1"), crmRef("identity_link", "i-1"), CRM005IntentType, CRM005IntentVersion, conversionAt(t, "2026-01-03T00:00:00Z"))
	if !errors.Is(err, ErrCRM005Rejected) || rej == nil || rej.Field != "consent" {
		t.Fatalf("consent rejection=%+v err=%v", rej, err)
	}
	if !reflect.DeepEqual(empty, CandidateConversionProposal{}) {
		t.Fatalf("refused conversion wrote a proposal: %+v", empty)
	}
}
func TestTodo_CRM_005_Security(t *testing.T) {
	p := validProspect(t)
	p.Consent.ExpiresAt = conversionAt(t, "2026-01-03T00:00:00Z")
	_, rej, err := PrepareProspectConversion(p, crmRef("candidate", "c-1"), crmRef("application", "a-1"), crmRef("identity_link", "i-1"), "intent", "v1", conversionAt(t, "2026-01-03T00:00:00Z"))
	if !errors.Is(err, ErrCRM005Rejected) || rej == nil || rej.Field != "consent" {
		t.Fatalf("rejection=%+v err=%v", rej, err)
	}
}
func TestTodo_CRM_005_Mutation(t *testing.T) {
	p := validProspect(t)
	got, _, err := PrepareProspectConversion(p, crmRef("candidate", "c-1"), crmRef("application", "a-1"), crmRef("identity_link", "i-1"), CRM005IntentType, CRM005IntentVersion, conversionAt(t, "2026-01-03T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	got.Source.Campaign = "changed"
	if p.Attribution.Campaign == "changed" {
		t.Fatal("conversion aliased prospect provenance")
	}
	p.Consent.Authority = values.EntityRef{}
	if got.Consent.Authority.Kind != "processing_authority" {
		t.Fatal("conversion aliased consent")
	}
}
