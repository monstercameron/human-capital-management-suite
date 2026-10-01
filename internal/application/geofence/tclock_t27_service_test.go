package geofence

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	domaingeofence "github.com/monstercameron/human-capital-management-suite/internal/domains/geofence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var tclockNow = time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)

func tclockBoundary(t *testing.T, at time.Time) domaingeofence.Boundary {
	t.Helper()
	effective, err := values.NewOpenInstantInterval(values.NewInstant(at.Add(-time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	return domaingeofence.Boundary{SiteRef: "site-riverside", Revision: 7,
		Center: domaingeofence.Coordinate{Latitude: 37.7749, Longitude: -122.4194}, RadiusMeters: 100,
		AccuracyThresholdMeters: 40, MaxEvidenceAge: 5 * time.Minute, ExitGrace: 10 * time.Minute,
		HysteresisMeters: 20, Effective: effective, RetentionPeriod: 24 * time.Hour,
		AuthorizedBy: "site-authority", Version: "site-policy/v7"}
}

type boundaryFake struct {
	boundary domaingeofence.Boundary
	calls    int
}

func (f *boundaryFake) ResolveBoundary(context.Context, string, string, time.Time) (domaingeofence.Boundary, error) {
	f.calls++
	return f.boundary, nil
}

type pinFake struct{ pins map[string]SessionPin }

func (f *pinFake) Pin(_ context.Context, tenant, sessionID string, pin SessionPin) error {
	if f.pins == nil {
		f.pins = map[string]SessionPin{}
	}
	key := tenant + ":" + sessionID
	if old, ok := f.pins[key]; ok && old.BoundaryDigest != pin.BoundaryDigest {
		return errors.New("pin conflict")
	}
	f.pins[key] = pin
	return nil
}
func (f *pinFake) Load(_ context.Context, tenant, sessionID string) (SessionPin, error) {
	p, ok := f.pins[tenant+":"+sessionID]
	if !ok {
		return SessionPin{}, errors.New("pin missing")
	}
	return p, nil
}

type sessionFake struct {
	mu       sync.Mutex
	sessions map[string]timesession.Session
}

func (f *sessionFake) Load(_ context.Context, tenant, id string) (timesession.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[tenant+":"+id]
	if !ok {
		return timesession.Session{}, errors.New("session missing")
	}
	return s, nil
}

type writerFake struct {
	mu          sync.Mutex
	session     *sessionFake
	applied     []Transition
	auto        int
	fail        bool
	autoReceipt AutoOutReceipt
}

func (f *writerFake) Apply(_ context.Context, tr Transition) (timesession.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return timesession.Session{}, errors.New("writer fault")
	}
	f.session.mu.Lock()
	defer f.session.mu.Unlock()
	current := f.session.sessions[tr.Tenant+":"+tr.SessionID]
	if current.Revision != tr.ExpectedRevision {
		return timesession.Session{}, errors.New("revision conflict")
	}
	f.session.sessions[tr.Tenant+":"+tr.SessionID] = tr.Session
	f.applied = append(f.applied, tr)
	return tr.Session, nil
}
func (f *writerFake) CommitAutoOut(_ context.Context, cmd AutoOutCommand) (AutoOutReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return AutoOutReceipt{}, errors.New("writer fault")
	}
	f.auto++
	f.session.mu.Lock()
	defer f.session.mu.Unlock()
	current := f.session.sessions[cmd.Tenant+":"+cmd.SessionID]
	if current.State == timesession.StateAutoClosed {
		return AutoOutReceipt{Session: current, ObservationID: cmd.Observation.ID, Duplicate: true}, nil
	}
	if current.Revision != cmd.ExpectedRevision {
		return AutoOutReceipt{}, errors.New("revision conflict")
	}
	f.session.sessions[cmd.Tenant+":"+cmd.SessionID] = cmd.Session
	return AutoOutReceipt{Session: cmd.Session, ObservationID: cmd.Observation.ID}, nil
}

type followFake struct {
	notices, reviews int
	fail             bool
}

func (f *followFake) NotifyWorker(context.Context, WorkerNotification) error {
	f.notices++
	if f.fail {
		return errors.New("notify fault")
	}
	return nil
}
func (f *followFake) MarkReview(context.Context, ReviewMarker) error {
	f.reviews++
	if f.fail {
		return errors.New("review fault")
	}
	return nil
}

func tclockSession() timesession.Session {
	return timesession.Session{Tenant: "tenant-a", Worker: "worker-a", Assignment: "assignment-a", SessionID: "session-a", State: timesession.StateOpen, Revision: 1, Segments: []timesession.Segment{{Kind: timesession.SegmentWork, Start: tclockNow.Add(-time.Hour)}}}
}
func tclockService(t *testing.T, now time.Time) (*Service, *boundaryFake, *pinFake, *sessionFake, *writerFake, *followFake) {
	t.Helper()
	b := &boundaryFake{boundary: tclockBoundary(t, now)}
	p := &pinFake{}
	sessions := &sessionFake{sessions: map[string]timesession.Session{"tenant-a:session-a": tclockSession()}}
	w := &writerFake{session: sessions}
	f := &followFake{}
	return &Service{Boundaries: b, Pins: p, Sessions: sessions, Writer: w, FollowUp: f, Policy: Policy{SustainedMinimum: 2 * time.Minute}, Clock: func() time.Time { return now }}, b, p, sessions, w, f
}
func tclockEvidence(at time.Time, lat float64) EvidenceInput {
	return EvidenceInput{Observation: domaingeofence.DeviceObservation{ObservedAt: at, Position: domaingeofence.Coordinate{Latitude: lat, Longitude: -122.4194}, AccuracyMeters: 10, Source: domaingeofence.SourceDeviceGPS, PermissionGranted: true, ConsentRef: "consent-1"}, EvidenceRef: "evidence-1", Sustained: 3 * time.Minute}
}

// TestTodo_FTIME_010 proves a session pins the server-resolved boundary and
// retains a receipt-separated, coordinate-free proof for each decision.
func TestTodo_FTIME_010(t *testing.T) {
	svc, resolver, _, sessions, writer, _ := tclockService(t, tclockNow)
	pin, err := svc.PinSession(context.Background(), PinRequest{Tenant: "tenant-a", SessionID: "session-a", WorkerRef: "worker-a", AssignmentRef: "assignment-a", SiteRef: "site-riverside"})
	if err != nil {
		t.Fatal(err)
	}
	if pin.Boundary.Revision != 7 || pin.BoundaryDigest == "" {
		t.Fatalf("pin did not retain the authorized boundary revision: %+v", pin)
	}
	inside := tclockEvidence(tclockNow, 37.7749)
	got, err := svc.ObserveExit(context.Background(), "tenant-a", "session-a", inside)
	if err != nil {
		t.Fatal(err)
	}
	if got.Evaluation.Status != domaingeofence.StatusInside || got.Evidence.ReceivedAt != tclockNow || got.Evidence.Confidence != "SUFFICIENT" {
		t.Fatalf("inside result = %+v", got)
	}
	if resolver.calls != 1 || len(writer.applied) != 1 {
		t.Fatalf("boundary resolution/writes = %d/%d", resolver.calls, len(writer.applied))
	}
	if sessions.sessions["tenant-a:session-a"].PendingAutoOut != nil {
		t.Fatal("inside evidence started an auto-out grace")
	}
}

// TestTodo_FTIME_010_Golden pins the stable audit event projection: raw
// coordinates cannot enter the retained event payload.
func TestTodo_FTIME_010_Golden(t *testing.T) {
	svc, _, _, _, writer, _ := tclockService(t, tclockNow)
	if _, err := svc.PinSession(context.Background(), PinRequest{Tenant: "tenant-a", SessionID: "session-a", WorkerRef: "worker-a", AssignmentRef: "assignment-a", SiteRef: "site-riverside"}); err != nil {
		t.Fatal(err)
	}
	in := tclockEvidence(tclockNow, 37.8)
	in.EvidenceRef = "golden-evidence"
	if _, err := svc.ObserveExit(context.Background(), "tenant-a", "session-a", in); err != nil {
		t.Fatal(err)
	}
	payload := string(writer.applied[0].Event.Payload)
	if payload == "" || containsAny(payload, "37.8", "-122.4194", "latitude", "longitude") {
		t.Fatalf("event payload retained raw location: %s", payload)
	}
}

// TestTodo_FTIME_010_Security proves unknown permission, stale and weak fixes
// remain reviewable but never become confirmed exit evidence.
func TestTodo_FTIME_010_Security(t *testing.T) {
	cases := []struct {
		name string
		edit func(*EvidenceInput)
	}{{"permission", func(e *EvidenceInput) { e.Observation.PermissionGranted = false }}, {"stale", func(e *EvidenceInput) { e.Observation.ObservedAt = tclockNow.Add(-10 * time.Minute) }}, {"accuracy", func(e *EvidenceInput) { e.Observation.AccuracyMeters = 100 }}, {"source", func(e *EvidenceInput) { e.Observation.Source = domaingeofence.Source("spoofed") }}, {"ambiguous indoor", func(e *EvidenceInput) { e.AmbiguousIndoor = true }}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, _, sessions, _, _ := tclockService(t, tclockNow)
			if _, err := svc.PinSession(context.Background(), PinRequest{Tenant: "tenant-a", SessionID: "session-a", WorkerRef: "worker-a", AssignmentRef: "assignment-a", SiteRef: "site-riverside"}); err != nil {
				t.Fatal(err)
			}
			input := tclockEvidence(tclockNow, 37.7749)
			tc.edit(&input)
			got, err := svc.ObserveExit(context.Background(), "tenant-a", "session-a", input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Evaluation.Status != domaingeofence.StatusUnknown || got.Decision.Kind != timesession.ExitUncertain || sessions.sessions["tenant-a:session-a"].State != timesession.StateOpen {
				t.Fatalf("uncertain evidence = %+v", got)
			}
			if got.Session.OpenExceptions[len(got.Session.OpenExceptions)-1].Kind != timesession.ExceptionUncertainLocation {
				t.Fatalf("missing visible uncertain exception: %+v", got.Session.OpenExceptions)
			}
		})
	}
}

// TestTodo_FTIME_010_Property proves the application never treats a point
// outside the pin's radius+hysteresis as inside.
func TestTodo_FTIME_010_Property(t *testing.T) {
	svc, _, _, _, _, _ := tclockService(t, tclockNow)
	if _, err := svc.PinSession(context.Background(), PinRequest{Tenant: "tenant-a", SessionID: "session-a", WorkerRef: "worker-a", AssignmentRef: "assignment-a", SiteRef: "site-riverside"}); err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(27, 10))
	for i := 0; i < 100; i++ {
		lat := 37.7749 + (rng.Float64()+0.5)*0.02
		in := tclockEvidence(tclockNow, lat)
		in.EvidenceRef = "property-" + string(rune(i+33))
		got, err := svc.ObserveExit(context.Background(), "tenant-a", "session-a", in)
		if err != nil {
			t.Fatal(err)
		}
		if got.Evaluation.Status == domaingeofence.StatusInside && got.Evaluation.DistanceMeters > 120 {
			t.Fatalf("case %d: outside hysteresis classified inside: %+v", i, got.Evaluation)
		}
	}
}

// TestTodo_FTIME_011 proves sustained outside evidence starts a server-owned
// grace, re-entry cancels it, and expiry commits one provisional AUTO_OUT.
func TestTodo_FTIME_011(t *testing.T) {
	svc, _, _, sessions, writer, follow := tclockService(t, tclockNow)
	if _, err := svc.PinSession(context.Background(), PinRequest{Tenant: "tenant-a", SessionID: "session-a", WorkerRef: "worker-a", AssignmentRef: "assignment-a", SiteRef: "site-riverside"}); err != nil {
		t.Fatal(err)
	}
	outside := tclockEvidence(tclockNow, 37.80)
	outside.EvidenceRef = "exit-1"
	got, err := svc.ObserveExit(context.Background(), "tenant-a", "session-a", outside)
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision.Kind != timesession.ExitGraceStarted || got.Session.PendingAutoOut == nil {
		t.Fatalf("outside result = %+v", got)
	}
	svc.Clock = func() time.Time { return tclockNow.Add(time.Minute) }
	reentry := tclockEvidence(tclockNow.Add(time.Minute), 37.7749)
	reentry.EvidenceRef = "reentry-1"
	got, err = svc.ObserveExit(context.Background(), "tenant-a", "session-a", reentry)
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision.Kind != timesession.ExitGraceCancelled || got.Session.PendingAutoOut != nil {
		t.Fatalf("reentry result = %+v", got)
	}
	outside.EvidenceRef = "exit-2"
	got, err = svc.ObserveExit(context.Background(), "tenant-a", "session-a", outside)
	if err != nil {
		t.Fatal(err)
	}
	expiry := got.Session.PendingAutoOut.ExpiresAt
	svc.Clock = func() time.Time { return expiry.Add(time.Second) }
	receipt, err := svc.ExpireAutoOut(context.Background(), "tenant-a", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.ReviewRequired || receipt.Session.State != timesession.StateAutoClosed || writer.auto != 1 || follow.notices != 1 || follow.reviews != 1 || sessions.sessions["tenant-a:session-a"].State != timesession.StateAutoClosed {
		t.Fatalf("auto-out = %+v writes=%d follow=%d/%d", receipt, writer.auto, follow.notices, follow.reviews)
	}
}

// TestTodo_FTIME_011_Integration proves a fresh service instance recovers the
// pinned boundary and pending grace from the durable fakes.
func TestTodo_FTIME_011_Integration(t *testing.T) {
	svc, resolver, pins, sessions, writer, follow := tclockService(t, tclockNow)
	if _, err := svc.PinSession(context.Background(), PinRequest{Tenant: "tenant-a", SessionID: "session-a", WorkerRef: "worker-a", AssignmentRef: "assignment-a", SiteRef: "site-riverside"}); err != nil {
		t.Fatal(err)
	}
	in := tclockEvidence(tclockNow, 37.80)
	in.EvidenceRef = "restart-exit"
	got, err := svc.ObserveExit(context.Background(), "tenant-a", "session-a", in)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Service{Boundaries: resolver, Pins: pins, Sessions: sessions, Writer: writer, FollowUp: follow, Policy: svc.Policy, Clock: func() time.Time { return got.Session.PendingAutoOut.ExpiresAt.Add(time.Second) }}
	receipt, err := restarted.ExpireAutoOut(context.Background(), "tenant-a", "session-a")
	if err != nil || receipt.Session.State != timesession.StateAutoClosed {
		t.Fatalf("restart expiry = %+v, %v", receipt, err)
	}
}

// TestTodo_FTIME_011_Security proves a duplicate expiry cannot create a
// second AUTO_OUT observation or second worker notification.
func TestTodo_FTIME_011_Security(t *testing.T) {
	svc, _, _, _, writer, follow := tclockService(t, tclockNow)
	if _, err := svc.PinSession(context.Background(), PinRequest{Tenant: "tenant-a", SessionID: "session-a", WorkerRef: "worker-a", AssignmentRef: "assignment-a", SiteRef: "site-riverside"}); err != nil {
		t.Fatal(err)
	}
	in := tclockEvidence(tclockNow, 37.80)
	in.EvidenceRef = "security-exit"
	got, err := svc.ObserveExit(context.Background(), "tenant-a", "session-a", in)
	if err != nil {
		t.Fatal(err)
	}
	svc.Clock = func() time.Time { return got.Session.PendingAutoOut.ExpiresAt.Add(time.Second) }
	if _, err := svc.ExpireAutoOut(context.Background(), "tenant-a", "session-a"); err != nil {
		t.Fatal(err)
	}
	before := follow.notices
	if _, err := svc.ExpireAutoOut(context.Background(), "tenant-a", "session-a"); err != nil {
		t.Fatal(err)
	}
	if writer.auto != 2 || follow.notices != before {
		t.Fatalf("duplicate changed effects: writer=%d notices=%d before=%d", writer.auto, follow.notices, before)
	}
}

// TestTodo_FTIME_011_Race drives concurrent expiry calls through a CAS writer;
// one call commits and all later calls replay the same observation.
func TestTodo_FTIME_011_Race(t *testing.T) {
	svc, _, _, _, writer, follow := tclockService(t, tclockNow)
	if _, err := svc.PinSession(context.Background(), PinRequest{Tenant: "tenant-a", SessionID: "session-a", WorkerRef: "worker-a", AssignmentRef: "assignment-a", SiteRef: "site-riverside"}); err != nil {
		t.Fatal(err)
	}
	in := tclockEvidence(tclockNow, 37.80)
	in.EvidenceRef = "race-exit"
	got, err := svc.ObserveExit(context.Background(), "tenant-a", "session-a", in)
	if err != nil {
		t.Fatal(err)
	}
	svc.Clock = func() time.Time { return got.Session.PendingAutoOut.ExpiresAt.Add(time.Second) }
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.ExpireAutoOut(context.Background(), "tenant-a", "session-a"); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 8 || writer.auto != 8 || follow.notices != 1 {
		t.Fatalf("race results successes=%d commits=%d notices=%d", successes, writer.auto, follow.notices)
	}
}

// TestTodo_FTIME_011_Fault proves a commit fault leaves the open session
// untouched and no follow-up effect is emitted.
func TestTodo_FTIME_011_Fault(t *testing.T) {
	svc, _, _, sessions, writer, follow := tclockService(t, tclockNow)
	if _, err := svc.PinSession(context.Background(), PinRequest{Tenant: "tenant-a", SessionID: "session-a", WorkerRef: "worker-a", AssignmentRef: "assignment-a", SiteRef: "site-riverside"}); err != nil {
		t.Fatal(err)
	}
	in := tclockEvidence(tclockNow, 37.80)
	in.EvidenceRef = "fault-exit"
	got, err := svc.ObserveExit(context.Background(), "tenant-a", "session-a", in)
	if err != nil {
		t.Fatal(err)
	}
	writer.fail = true
	svc.Clock = func() time.Time { return got.Session.PendingAutoOut.ExpiresAt.Add(time.Second) }
	if _, err := svc.ExpireAutoOut(context.Background(), "tenant-a", "session-a"); err == nil {
		t.Fatal("expected writer fault")
	}
	if sessions.sessions["tenant-a:session-a"].State != timesession.StateOpen || follow.notices != 0 {
		t.Fatalf("fault changed state/follow-up: %+v notices=%d", sessions.sessions["tenant-a:session-a"], follow.notices)
	}
}

// TestTodo_FTIME_011_Browser is the component-level projection proof used
// when a live browser is not available to this package lane.
func TestTodo_FTIME_011_Browser(t *testing.T) {
	svc, _, _, _, _, _ := tclockService(t, tclockNow)
	if _, err := svc.PinSession(context.Background(), PinRequest{Tenant: "tenant-a", SessionID: "session-a", WorkerRef: "worker-a", AssignmentRef: "assignment-a", SiteRef: "site-riverside"}); err != nil {
		t.Fatal(err)
	}
	in := tclockEvidence(tclockNow, 37.7749)
	in.EvidenceRef = "browser-uncertain"
	in.Observation.PermissionGranted = false
	got, err := svc.ObserveExit(context.Background(), "tenant-a", "session-a", in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision.Kind != timesession.ExitUncertain || got.Session.State != timesession.StateOpen || len(got.Session.OpenExceptions) != 1 {
		t.Fatalf("worker-visible exception projection = %+v", got)
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if len(needle) > 0 && index(value, needle) >= 0 {
			return true
		}
	}
	return false
}
func index(value, needle string) int {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
