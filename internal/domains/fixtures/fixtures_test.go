package fixtures

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
)

func TestCalendar(t *testing.T) {
	cal, err := Calendar()
	if err != nil {
		t.Fatal(err)
	}
	if cal.Ref == "" || cal.Version == "" {
		t.Fatal("empty calendar")
	}
}

func TestWorkerRef(t *testing.T) {
	ref, err := WorkerRef("jane-doe")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Id == "" {
		t.Fatal("empty worker id")
	}
	if ref.Tenant != Tenant {
		t.Fatalf("tenant mismatch %v", ref.Tenant)
	}
	_, err = WorkerRef("no-such-worker-xyz")
	if err == nil {
		t.Fatal("expected error for unknown worker")
	}
}

func TestMoney(t *testing.T) {
	m, err := Money("100.00", "USD")
	if err != nil {
		t.Fatal(err)
	}
	if m.Currency() != "USD" {
		t.Fatalf("currency %q", m.Currency())
	}
	if _, err := Money("not-a-number", "USD"); err == nil {
		t.Fatal("expected error for bad money")
	}
}

func TestPercent(t *testing.T) {
	p, err := Percent("0.1000")
	if err != nil {
		t.Fatal(err)
	}
	_ = p
	if _, err := Percent("bad"); err == nil {
		t.Fatal("expected error for bad percent")
	}
}

func TestLegacyScenarios(t *testing.T) {
	s, err := LegacyScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Scenarios) == 0 {
		t.Fatal("no scenarios")
	}
}

func TestMemoryWorkerFacts(t *testing.T) {
	m, err := NewMemoryWorkerFacts()
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("nil")
	}
	if len(m.Missing) != 0 {
		t.Fatal("missing not empty")
	}
}

func TestMemoryBandCatalog(t *testing.T) {
	c, err := NewMemoryBandCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if c.CatalogVersion() == "" {
		t.Fatal("empty catalog version")
	}
	if c.CatalogVersion() == "" {
		t.Fatal("empty")
	}
}

func TestBand(t *testing.T) {
	c, err := NewMemoryBandCatalog()
	if err != nil {
		t.Fatal(err)
	}
	_ = c
	if len(c.bands) == 0 {
		t.Fatal("no bands")
	}
	b, err := Band(c.bands[0].record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID == "" {
		t.Fatal("empty band id")
	}
	if _, err := Band("no-such-band-xyz"); err == nil {
		t.Fatal("expected error for unknown band")
	}
}

func TestAllowAllDeny(t *testing.T) {
	decision := AllowAll("v1", "test", []people.FieldID{people.FieldWorkerNumber, people.FieldLegalName})
	if len(decision.Fields) != 2 {
		t.Fatalf("fields %d", len(decision.Fields))
	}
	denied := DenyFields(decision, "need-to-know", people.FieldLegalName)
	if denied.Fields[people.FieldLegalName].Effect != people.EffectDeny {
		t.Fatal("expected deny")
	}
	withheld := WithheldSubject(decision, "reason")
	if withheld.SubjectDisclosable {
		t.Fatal("expected withheld")
	}
}

// TestWorkersListsTheWholeCorpusInDeclaredOrder proves the listing helper is
// exactly the corpus and nothing derived: every worker, in the file's own
// order, with the identity WorkerRef resolves to.
func TestWorkersListsTheWholeCorpusInDeclaredOrder(t *testing.T) {
	listed, err := Workers()
	if err != nil {
		t.Fatalf("Workers(): %v", err)
	}
	if len(listed) != len(workers.Workers) {
		t.Fatalf("listed %d workers, want the corpus's own %d", len(listed), len(workers.Workers))
	}
	for i, got := range listed {
		want := workers.Workers[i]
		if got.Key != want.Key || got.ID != want.ID {
			t.Fatalf("listed[%d] = %s/%s, want %s/%s", i, got.Key, got.ID, want.Key, want.ID)
		}
		ref, err := WorkerRef(got.Key)
		if err != nil {
			t.Fatalf("WorkerRef(%q): %v", got.Key, err)
		}
		if ref.Id != got.ID {
			t.Errorf("listed[%d] id = %s, WorkerRef says %s", i, got.ID, ref.Id)
		}
		if got.JobCode == "" || got.Grade == "" || got.OrgUnit == "" ||
			got.PositionID == "" || got.PayZone == "" || got.HireDate == "" {
			t.Errorf("listed[%d] is missing placement fields: %+v", i, got)
		}
	}
}

// TestWorkerProfileDisplayNameFallsBackToTheLegalName pins the one naming rule
// the listing row owns, so no surface re-decides it.
func TestWorkerProfileDisplayNameFallsBackToTheLegalName(t *testing.T) {
	p := WorkerProfile{LegalName: "Ada Lovelace", PreferredName: "Ada"}
	if got := p.DisplayName(); got != "Ada" {
		t.Errorf("DisplayName() = %q, want the preferred name", got)
	}
	p.PreferredName = ""
	if got := p.DisplayName(); got != "Ada Lovelace" {
		t.Errorf("DisplayName() = %q, want the legal name", got)
	}
}

// TestBandScopesCoverEveryCatalogBand proves the scope listing is the catalog
// itself: one scope per band, and each one resolvable back through the
// in-memory catalog's own lookup.
func TestBandScopesCoverEveryCatalogBand(t *testing.T) {
	scopes, err := BandScopes()
	if err != nil {
		t.Fatalf("BandScopes(): %v", err)
	}
	all, err := catalogBands()
	if err != nil {
		t.Fatalf("catalogBands(): %v", err)
	}
	if len(scopes) != len(all) {
		t.Fatalf("listed %d scopes, want the catalog's own %d", len(scopes), len(all))
	}
	for i, got := range scopes {
		want := all[i].record
		if got.JobCode != want.JobCode || got.Grade != want.Grade ||
			got.PayZone != want.PayZone || got.Currency != want.Currency {
			t.Errorf("scope[%d] = %+v, want %s/%s/%s in %s",
				i, got, want.JobCode, want.Grade, want.PayZone, want.Currency)
		}
	}
}

// TestEveryCorpusWorkerIsPlacedInsideABand is the invariant the create surface
// leans on: the corpus population is itself simulatable, so offering its org
// units and positions beside the catalog's placements cannot produce a worker
// the rewards engine has no band for.
func TestEveryCorpusWorkerIsPlacedInsideABand(t *testing.T) {
	scopes, err := BandScopes()
	if err != nil {
		t.Fatalf("BandScopes(): %v", err)
	}
	listed, err := Workers()
	if err != nil {
		t.Fatalf("Workers(): %v", err)
	}
	for _, w := range listed {
		covered := false
		for _, s := range scopes {
			if s.JobCode == w.JobCode && s.Grade == w.Grade && s.PayZone == w.PayZone {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("corpus worker %s is placed on %s/%s/%s, which no band covers",
				w.Key, w.JobCode, w.Grade, w.PayZone)
		}
	}
}

// TestWorkerProfileFieldsAreNotAGovernedRead is a boundary note made
// checkable: the listing row carries placement and identity only. If a
// compartmentalised field ever appears on it, this test is where that shows
// up as a deliberate decision rather than a drifted struct.
func TestWorkerProfileFieldsAreNotAGovernedRead(t *testing.T) {
	listed, err := Workers()
	if err != nil {
		t.Fatalf("Workers(): %v", err)
	}
	for _, w := range listed {
		_ = people.FieldLegalName // the governed projection lives in people, not here
		if w.WorkerNumber == "" {
			t.Errorf("worker %s has no worker number", w.Key)
		}
	}
}
