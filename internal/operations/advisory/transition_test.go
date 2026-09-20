package advisory

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/incidentstate"
)

// assertTransitionGolden pins the exact customer evidence bytes: the
// advisory digest already binds tenant, incident, version and facts, so any
// drift in the published surface is a byte-visible diff here.
func assertTransitionGolden(t *testing.T, path, got string) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file %s: %v", path, err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch for %s:\n got:\n%s\nwant:\n%s", path, got, string(want))
	}
}

// REV-017-03, advisory half: a real DETECTED-to-REVIEWED incident never
// called advisory publication. PublishForTransition closes that gap: an
// incident transition into a newly entered customer-facing state publishes
// with the incident's scoped affected set.

func rev01703Incident(state incidentstate.State, version uint64) incidentstate.Incident {
	return incidentstate.Incident{
		ID: "inc-1", TenantID: "tenant-a", State: state, Version: version,
		Affected: incidentstate.AffectedSet{Known: true, Verified: true, Facts: []incidentstate.AffectedFact{
			{TenantID: "tenant-a", Kind: "capability", Value: "promotion"},
			{TenantID: "tenant-b", Kind: "capability", Value: "other-tenant"},
			{TenantID: "tenant-a", Kind: "worker", Value: "worker-secret"},
		}},
	}
}

func rev01703Audience() Audience {
	return Audience{TenantID: "tenant-a", Role: "tenant-customer", Authorized: true}
}

func rev01703Delivery(at time.Time) DeliveryEvidence {
	return DeliveryEvidence{ReceiptID: "rcpt-1", Channel: "status-page", Status: "delivered", At: at}
}

// TestTodo_REV_017_03 is the REV-017-03 primary test for the advisory half:
// entering MITIGATING publishes a MITIGATING advisory carrying only the
// incident's own-tenant, safe-kind facts, and the publisher history holds
// exactly that evidence.
func TestTodo_REV_017_03(t *testing.T) {
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	pub := NewPublisher()
	before := rev01703Incident(incidentstate.Declared, 3)
	after := rev01703Incident(incidentstate.Mitigating, 4)

	got, published, err := PublishForTransition(pub, before, after, rev01703Audience(), rev01703Delivery(at), at)
	if err != nil {
		t.Fatalf("PublishForTransition: %v", err)
	}
	if !published {
		t.Fatal("entering MITIGATING published nothing")
	}
	if got.Status != StatusMitigating || got.TenantID != "tenant-a" || got.IncidentID != "inc-1" || got.Version != 4 {
		t.Fatalf("advisory = %+v, want MITIGATING tenant-a/inc-1 v4", got)
	}
	if len(got.Facts) != 1 || got.Facts[0] != (PublicFact{Kind: "capability", Value: "promotion"}) {
		t.Fatalf("advisory facts = %+v, want only the own-tenant safe-kind fact", got.Facts)
	}
	history := pub.History("tenant-a", "inc-1")
	if len(history) != 1 || history[0].Digest != got.Digest {
		t.Fatalf("publisher history = %+v, want the published advisory", history)
	}
	if explain := Explain(got); !strings.Contains(explain, "status=MITIGATING") {
		t.Fatalf("Explain = %q, want the MITIGATING status", explain)
	}
}

// TestTodo_REV_017_03_Fault proves the bridge stays silent or fails closed:
// non-customer-facing states and same-status transitions publish nothing,
// and a cross-tenant audience is refused rather than published.
func TestTodo_REV_017_03_Fault(t *testing.T) {
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		before  incidentstate.State
		after   incidentstate.State
		version uint64
		aud     Audience
		wantPub bool
		wantErr string
	}{
		{"false positive is not customer facing", incidentstate.Detected, incidentstate.FalsePositive, 2, rev01703Audience(), false, ""},
		{"same status republishes nothing", incidentstate.Detected, incidentstate.Triaged, 2, rev01703Audience(), false, ""},
		{"monitoring to resolved stays monitoring", incidentstate.Monitoring, incidentstate.Resolved, 6, rev01703Audience(), false, ""},
		{"cross tenant audience refused", incidentstate.Resolved, incidentstate.Reviewed, 7, Audience{TenantID: "tenant-b", Role: "tenant-customer", Authorized: true}, false, "unauthorized"},
		{"unauthorized audience refused", incidentstate.Resolved, incidentstate.Reviewed, 7, Audience{TenantID: "tenant-a", Role: "tenant-customer"}, false, "invalid request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub := NewPublisher()
			before := rev01703Incident(tc.before, tc.version-1)
			after := rev01703Incident(tc.after, tc.version)
			got, published, err := PublishForTransition(pub, before, after, tc.aud, rev01703Delivery(at), at)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.wantErr) {
					t.Fatalf("err = %v, want fragment %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("PublishForTransition: %v", err)
			}
			if published != tc.wantPub {
				t.Fatalf("published = %v, want %v", published, tc.wantPub)
			}
			if !tc.wantPub && len(pub.History("tenant-a", "inc-1")) != 0 {
				t.Fatalf("skipped transition published: %+v", got)
			}
		})
	}
}

// TestTodo_REV_017_03_Race proves concurrent transitions publish exactly one
// advisory lineage: the publisher stays mutex-guarded end to end.
func TestTodo_REV_017_03_Race(t *testing.T) {
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	pub := NewPublisher()
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			before := rev01703Incident(incidentstate.Declared, 3)
			after := rev01703Incident(incidentstate.Mitigating, 4)
			if _, _, err := PublishForTransition(pub, before, after, rev01703Audience(), rev01703Delivery(at), at); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent PublishForTransition: %v", err)
	}
	history := pub.History("tenant-a", "inc-1")
	if len(history) != 1 {
		t.Fatalf("history holds %d advisories, want exactly 1", len(history))
	}
}

// TestTodo_REV_017_03_Golden pins the byte-exact advisory a resolved review
// publishes, so a change to the customer evidence surface is a visible diff.
func TestTodo_REV_017_03_Golden(t *testing.T) {
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	pub := NewPublisher()
	before := rev01703Incident(incidentstate.Resolved, 6)
	after := rev01703Incident(incidentstate.Reviewed, 7)
	got, published, err := PublishForTransition(pub, before, after, rev01703Audience(), rev01703Delivery(at), at)
	if err != nil || !published {
		t.Fatalf("PublishForTransition = %+v, %v, %v", got, published, err)
	}
	assertTransitionGolden(t, "testdata/rev01703_advisory.golden.txt", Explain(got)+"\n")
}
