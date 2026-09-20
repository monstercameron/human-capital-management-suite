package journeyinvalidation

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var hubNow = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

const (
	journeyA = "00000000-0000-4000-8000-00000000000a"
	journeyB = "00000000-0000-4000-8000-00000000000b"
	workerA  = "00000000-0000-4000-8000-0000000000a1"
	workerB  = "00000000-0000-4000-8000-0000000000b1"
)

func hubPrincipal(t *testing.T, tenant values.TenantId, subject, org string, roles ...string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenant, Subject: subject, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: org, Roles: roles, Purposes: []string{"compensation_review"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-" + subject, IssuedAt: hubNow.Add(-time.Hour), ExpiresAt: hubNow.Add(time.Hour),
		CredentialDigest: "digest-" + subject,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeInspector answers Inspect from a visibility table: a journey absent
// from visible is refused exactly as the engine refuses a journey the viewer
// may not see.
type fakeInspector struct {
	mu      sync.Mutex
	tenant  values.TenantId
	visible map[string]string // intent id -> worker id
	calls   int
}

func (f *fakeInspector) Inspect(_ context.Context, intentID string) (workspace.JourneyDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	worker, ok := f.visible[intentID]
	if !ok {
		return workspace.JourneyDetail{}, workspace.ErrDenied
	}
	return workspace.JourneyDetail{Summary: workspace.JourneySummary{
		IntentID:  intentID,
		Worker:    values.EntityRef{Tenant: f.tenant, Kind: values.Kind("worker"), Id: worker},
		UpdatedAt: hubNow.Add(-time.Minute),
	}}, nil
}

func decodeMessage(t *testing.T, raw []byte) productquery.InvalidationMessage {
	t.Helper()
	var message productquery.InvalidationMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if err := message.Validate(); err != nil {
		t.Fatalf("delivered message invalid: %v", err)
	}
	return message
}

func nextWithin(t *testing.T, sub *Subscription) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	return sub.Next(ctx)
}

func newHub() *Hub { return NewHub(Options{Now: func() time.Time { return hubNow }}) }

func TestHubDeliversShellCountHintForVisibleJourney(t *testing.T) {
	tenant := values.TenantId("acme")
	hub := newHub()
	inspector := &fakeInspector{tenant: tenant, visible: map[string]string{journeyA: workerA}}
	sub, err := hub.Subscribe(SubscribeRequest{
		Principal: hubPrincipal(t, tenant, "admin", "org:acme:people-ops", "comp_admin"),
		Region:    promotion.RegionShellCount, Inspector: inspector,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if !hub.Publish(Committed{Tenant: tenant, IntentID: journeyA, Revision: 41}) {
		t.Fatal("valid record refused")
	}
	raw, err := nextWithin(t, sub)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	message := decodeMessage(t, raw)
	projection, _ := promotion.RegionShellCount.Projection()
	if message.Projection != projection || message.Tenant != tenant {
		t.Fatalf("message scope = %q/%q, want %q/%q", message.Projection, message.Tenant, projection, tenant)
	}
	if message.SourceSequence != 1 || message.Watermark != 0 {
		t.Fatalf("sequence = %d/%d, want the subscriber's own 1/0, never the global 41", message.SourceSequence, message.Watermark)
	}
	if len(message.Items) != 1 || message.Items[0].Subject != promotion.ShellCountSubject(tenant) || message.Items[0].Revision != 41 {
		t.Fatalf("items = %+v, want the shell subject at revision 41", message.Items)
	}
	if sub.Deliveries() != 1 || hub.Published() != 1 {
		t.Fatalf("deliveries=%d published=%d", sub.Deliveries(), hub.Published())
	}
}

func TestHubRegionSubjects(t *testing.T) {
	tenant := values.TenantId("acme")
	cases := map[promotion.Region]values.EntityRef{
		promotion.RegionJourneys: JourneyRef(tenant, journeyA),
		promotion.RegionMyWork:   JourneyRef(tenant, journeyA),
		promotion.RegionDetail:   JourneyRef(tenant, journeyA),
		promotion.RegionPerson:   {Tenant: tenant, Kind: values.Kind("worker"), Id: workerA},
	}
	for region, want := range cases {
		hub := newHub()
		sub, err := hub.Subscribe(SubscribeRequest{
			Principal: hubPrincipal(t, tenant, "admin", "", "comp_admin"), Region: region,
			Inspector: &fakeInspector{tenant: tenant, visible: map[string]string{journeyA: workerA}},
		})
		if err != nil {
			t.Fatal(err)
		}
		hub.Publish(Committed{Tenant: tenant, IntentID: journeyA, Revision: 7})
		raw, err := nextWithin(t, sub)
		sub.Close()
		if err != nil {
			t.Fatalf("%s: %v", region, err)
		}
		if got := decodeMessage(t, raw).Items[0].Subject; got != want {
			t.Fatalf("%s subject = %v, want %v", region, got, want)
		}
	}
}

func TestHubResumesNumberingAfterClientSequence(t *testing.T) {
	tenant := values.TenantId("acme")
	hub := newHub()
	sub, err := hub.Subscribe(SubscribeRequest{
		Principal: hubPrincipal(t, tenant, "admin", "", "comp_admin"), Region: promotion.RegionShellCount,
		AfterSequence: 9, Inspector: &fakeInspector{tenant: tenant, visible: map[string]string{journeyA: workerA, journeyB: workerB}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	hub.Publish(Committed{Tenant: tenant, IntentID: journeyA, Revision: 3})
	hub.Publish(Committed{Tenant: tenant, IntentID: journeyB, Revision: 4})
	for want := uint64(10); want <= 11; want++ {
		raw, err := nextWithin(t, sub)
		if err != nil {
			t.Fatal(err)
		}
		if message := decodeMessage(t, raw); message.SourceSequence != want || message.Watermark != want-1 {
			t.Fatalf("sequence = %d/%d, want %d/%d", message.SourceSequence, message.Watermark, want, want-1)
		}
	}
}

func TestHubLagEndsSubscription(t *testing.T) {
	tenant := values.TenantId("acme")
	hub := NewHub(Options{Buffer: 1, Now: func() time.Time { return hubNow }})
	sub, err := hub.Subscribe(SubscribeRequest{
		Principal: hubPrincipal(t, tenant, "admin", "", "comp_admin"), Region: promotion.RegionShellCount,
		Inspector: &fakeInspector{tenant: tenant, visible: map[string]string{journeyA: workerA}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	hub.Publish(Committed{Tenant: tenant, IntentID: journeyA, Revision: 1})
	hub.Publish(Committed{Tenant: tenant, IntentID: journeyA, Revision: 2})
	if _, err := nextWithin(t, sub); !errors.Is(err, ErrLagged) {
		t.Fatalf("Next after overflow = %v, want ErrLagged", err)
	}
}

func TestHubRevokesWhenAuthorityEnds(t *testing.T) {
	tenant := values.TenantId("acme")
	hub := newHub()
	allowed := true
	sub, err := hub.Subscribe(SubscribeRequest{
		Principal: hubPrincipal(t, tenant, "admin", "", "comp_admin"), Region: promotion.RegionShellCount,
		Inspector: &fakeInspector{tenant: tenant, visible: map[string]string{journeyA: workerA}},
		Authorize: func(context.Context) error {
			if allowed {
				return nil
			}
			return errors.New("page access removed")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	allowed = false
	hub.Publish(Committed{Tenant: tenant, IntentID: journeyA, Revision: 1})
	if raw, err := nextWithin(t, sub); !errors.Is(err, ErrRevoked) || raw != nil {
		t.Fatalf("Next after revocation = %q, %v; want no bytes and ErrRevoked", raw, err)
	}
}

func TestHubRefusesExpiredCredential(t *testing.T) {
	tenant := values.TenantId("acme")
	hub := NewHub(Options{Now: func() time.Time { return hubNow.Add(2 * time.Hour) }})
	sub, err := hub.Subscribe(SubscribeRequest{
		Principal: hubPrincipal(t, tenant, "admin", "", "comp_admin"), Region: promotion.RegionShellCount,
		Inspector: &fakeInspector{tenant: tenant, visible: map[string]string{journeyA: workerA}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	hub.Publish(Committed{Tenant: tenant, IntentID: journeyA, Revision: 1})
	if _, err := nextWithin(t, sub); !errors.Is(err, ErrRevoked) {
		t.Fatalf("Next with an expired credential = %v, want ErrRevoked", err)
	}
}

func TestHubInputValidationAndLifecycle(t *testing.T) {
	tenant := values.TenantId("acme")
	hub := NewHub(Options{MaxSubscribersPerTenant: 1, Now: func() time.Time { return hubNow }})
	principal := hubPrincipal(t, tenant, "admin", "", "comp_admin")
	inspector := &fakeInspector{tenant: tenant}
	bad := []SubscribeRequest{
		{Region: promotion.RegionShellCount, Inspector: inspector},
		{Principal: principal, Region: promotion.Region("NOPE"), Inspector: inspector},
		{Principal: principal, Region: promotion.RegionShellCount},
		{Principal: principal, Region: promotion.RegionShellCount, Inspector: inspector, AfterSequence: MaxAfterSequence + 1},
	}
	for i, req := range bad {
		if _, err := hub.Subscribe(req); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad request %d: err = %v, want ErrInvalid", i, err)
		}
	}
	var nilHub *Hub
	if _, err := nilHub.Subscribe(bad[0]); !errors.Is(err, ErrInvalid) || nilHub.Publish(Committed{}) || nilHub.Published() != 0 || nilHub.Subscribers(tenant) != 0 {
		t.Fatal("nil hub must refuse and report nothing")
	}
	if (Committed{}).Validate() == nil || hub.Publish(Committed{Tenant: tenant, IntentID: journeyA}) {
		t.Fatal("zero-revision record must be refused")
	}
	sub, err := hub.Subscribe(SubscribeRequest{Principal: principal, Region: promotion.RegionShellCount, Inspector: inspector})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Subscribe(SubscribeRequest{Principal: principal, Region: promotion.RegionShellCount, Inspector: inspector}); !errors.Is(err, ErrTooManySubscribers) {
		t.Fatalf("second subscription = %v, want ErrTooManySubscribers", err)
	}
	if hub.Subscribers(tenant) != 1 {
		t.Fatalf("subscribers = %d, want 1", hub.Subscribers(tenant))
	}
	sub.Close()
	sub.Close()
	if hub.Subscribers(tenant) != 0 {
		t.Fatal("Close did not remove the subscription")
	}
	if _, err := sub.Next(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Next after Close = %v, want ErrClosed", err)
	}
	var nilSub *Subscription
	nilSub.Close()
	if _, err := nilSub.Next(context.Background()); !errors.Is(err, ErrClosed) || nilSub.Deliveries() != 0 {
		t.Fatal("nil subscription must report closed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	live, _ := hub.Subscribe(SubscribeRequest{Principal: principal, Region: promotion.RegionShellCount, Inspector: inspector})
	defer live.Close()
	if _, err := live.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Next on a cancelled context = %v", err)
	}
}
