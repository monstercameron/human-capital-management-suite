package productui

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/compensation"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func managerCompRead(tenant, worker, disclosure string, facts ...compensation.DisclosedFact) compensation.Result {
	return compensation.Result{
		Worker:     values.EntityRef{Tenant: values.TenantId(tenant), Kind: values.Kind("worker"), Id: worker},
		Disclosure: disclosure, Presence: "PRESENT", Fields: facts,
	}
}

func TestTodo_REV_074_02(t *testing.T) {
	people := []Person{{ID: "worker-a", Name: "Ada", WorkerID: "worker-a"}}
	projection := ManagerCompensationProjection{CycleID: "cycle-q3", Reads: []compensation.Result{
		managerCompRead("tenant-a", "worker-a", "FULL",
			compensation.DisclosedFact{Field: compensation.FieldComponentType, Access: compensation.EffectAllow, Value: values.Value("BASE")},
			compensation.DisclosedFact{Field: compensation.FieldAmount, Access: compensation.EffectAllow, Value: values.Value("125000.00")},
			compensation.DisclosedFact{Field: compensation.FieldCurrency, Access: compensation.EffectAllow, Value: values.Value("USD")},
		),
		managerCompRead("tenant-a", "worker-b", "FULL"),
		managerCompRead("tenant-a", "worker-a", "WITHHELD"),
		managerCompRead("tenant-b", "worker-a", "FULL"),
	}}
	rows := scopedManagerCompensationRows("tenant-a", people, projection)
	if len(rows) != 1 || rows[0].Subject != "worker-a" || rows[0].Name != "Ada" {
		t.Fatalf("scoped rows = %+v, want only admitted worker-a", rows)
	}
	if len(rows[0].Components) != 1 || rows[0].Components[0].Kind != "BASE" || rows[0].Components[0].Amount != "125000.00" || rows[0].Components[0].Currency != "USD" {
		t.Fatalf("component projection = %+v", rows[0].Components)
	}
	view := testView(PageCompProposals)
	view.Tenant = "tenant-a"
	view.People = people
	view.ManagerCompensation = &projection
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "Ada") || !strings.Contains(doc, "USD") || !strings.Contains(doc, "125,000.00") || strings.Contains(doc, "worker-b") {
		t.Fatalf("manager compensation page did not render only its real scoped projection: %s", doc)
	}
	for _, code := range SupportedProductLocales() {
		localized := ApplyLocale(view, ResolveProductLocale(code))
		doc, err := Render(localized)
		if err != nil {
			t.Fatalf("render %s: %v", code, err)
		}
		if strings.Contains(doc, "⟦") || !strings.Contains(doc, "Ada") {
			t.Fatalf("manager compensation projection failed locale %s: %s", code, doc)
		}
	}
}

func TestTodo_REV_074_02_Golden(t *testing.T) {
	people := []Person{{ID: "worker-a", WorkerID: "worker-a", Name: "Ada"}}
	projection := ManagerCompensationProjection{CycleID: "cycle-q3", Reads: []compensation.Result{
		managerCompRead("tenant-a", "worker-a", "FULL",
			compensation.DisclosedFact{Field: compensation.FieldComponentType, Access: compensation.EffectAllow, Value: values.Value("BASE")},
			compensation.DisclosedFact{Field: compensation.FieldAmount, Access: compensation.EffectAllow, Value: values.Value("125000.00")},
		),
	}}
	rows := scopedManagerCompensationRows("tenant-a", people, projection)
	if len(rows) != 1 || rows[0].Components[0].Amount != "125000.00" || rows[0].Range != nil {
		t.Fatalf("projection should carry actual pay and no invented range: %+v", rows)
	}
}

func TestTodo_REV_074_02_Security(t *testing.T) {
	rows := []ManagerCompensationRow{{Subject: "worker-a"}}
	called := false
	var received ManagerCompensationIntent
	projection := ManagerCompensationProjection{CycleID: "cycle-q3", Submit: func(intent ManagerCompensationIntent, _ func(error)) { called = true; received = intent }}
	for _, intent := range []ManagerCompensationIntent{
		{Kind: "worksheet", CycleID: "cycle-q3", Subjects: []string{"worker-b"}},
		{Kind: "worksheet", CycleID: "cycle-old", Subjects: []string{"worker-a"}},
		{Kind: "worksheet", CycleID: "cycle-q3", Subjects: []string{"worker-a"}, Changes: []ManagerCompensationChange{{Subject: "worker-b", Amount: "1.00"}}},
		{Kind: "worksheet", CycleID: "cycle-q3", Subjects: []string{"worker-a", "worker-a"}},
	} {
		if err := submitManagerCompensation(projection, intent, rows, nil); !errors.Is(err, ErrManagerCompensationSubjectOutOfScope) {
			t.Fatalf("submission error = %v, want out-of-scope refusal", err)
		}
	}
	if called {
		t.Fatal("out-of-scope or stale-cycle submission reached intent adapter")
	}
	if err := submitManagerCompensation(projection, ManagerCompensationIntent{Kind: "calibration", CycleID: "cycle-q3", Subjects: []string{"worker-a"}, Changes: []ManagerCompensationChange{{Subject: "worker-a", Amount: "5.00"}}}, rows, nil); err != nil {
		t.Fatalf("authorized submission refused: %v", err)
	}
	if !called {
		t.Fatal("authorized submission did not reach intent adapter")
	}
	if received.Kind != "calibration" || received.CycleID != "cycle-q3" || len(received.Subjects) != 1 || received.Subjects[0] != "worker-a" || len(received.Changes) != 1 || received.Changes[0].Amount != "5.00" {
		t.Fatalf("submitted intent lost scoped input: %+v", received)
	}
}
