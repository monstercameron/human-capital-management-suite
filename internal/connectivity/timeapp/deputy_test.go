package timeapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/syncjob"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
)

type testLinker struct {
	link IdentityLink
	err  error
}

func (l testLinker) Resolve(context.Context, string, string) (IdentityLink, error) {
	return l.link, l.err
}

type testWriter struct {
	observations []SourcedObservation
	corrections  []SourcedCorrection
	err          error
	failAfter    int
}

type testState struct {
	records map[string]StateRecord
	err     error
}

func newTestState() *testState { return &testState{records: make(map[string]StateRecord)} }
func (s *testState) Load(_ context.Context, tenant, provider, id string) (StateRecord, bool, error) {
	if s.err != nil {
		return StateRecord{}, false, s.err
	}
	r, ok := s.records[tenant+"/"+provider+"/"+id]
	return r, ok, nil
}
func (s *testState) Save(_ context.Context, r StateRecord) error {
	if s.err != nil {
		return s.err
	}
	s.records[r.Tenant+"/Deputy/"+r.ProviderRecord] = r
	return nil
}

func (w *testWriter) Observe(_ context.Context, in SourcedObservation) (string, error) {
	if w.err != nil {
		return "", w.err
	}
	if w.failAfter > 0 && len(w.observations) >= w.failAfter {
		return "", errors.New("temporary observation failure")
	}
	w.observations = append(w.observations, in)
	return "obs-1", nil
}

func TestTodo_TCLOCK_013_Recovery_RetriesPartialRecordWithStableIdentity(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	firstWriter := &testWriter{failAfter: 1}
	c, secret, receipts, state := newConnector(t, testLinker{link: IdentityLink{WorkerRef: "worker-7", ResolutionRef: "r", EvidenceRef: "e", Resolved: true}}, firstWriter)
	req := request(secret, "partial-1", body("2026-09-28T09:00:00Z", "2026-09-28T17:00:00Z"), at)
	if _, err := c.VerifyAndImport(context.Background(), req, at); err == nil {
		t.Fatal("partial writer failure must be returned")
	}
	secondWriter := &testWriter{}
	c, err := New(Config{EndpointID: "ep-1", TenantID: "tenant-1", Secret: secret, Registry: profileRegistry(t), ReceiptStore: receipts, StateStore: state}, testLinker{link: IdentityLink{WorkerRef: "worker-7", ResolutionRef: "r", EvidenceRef: "e", Resolved: true}}, secondWriter)
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.VerifyAndImport(context.Background(), req, at.Add(time.Second))
	if err != nil || result.Observed != 2 || len(secondWriter.observations) != 2 {
		t.Fatalf("retry result=%+v err=%v writes=%+v", result, err, secondWriter.observations)
	}
	if secondWriter.observations[0].ProviderRecordID != "42" || secondWriter.observations[1].ProviderRecordID != "42" {
		t.Fatalf("provider identity changed: %+v", secondWriter.observations)
	}
}

func TestTodo_TCLOCK_013_ConfigAndPayloadGuards(t *testing.T) {
	w := &testWriter{}
	reg := profileRegistry(t)
	if _, err := New(Config{EndpointID: "ep", TenantID: "tenant", Secret: []byte("short"), Registry: reg}, testLinker{}, w); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("short secret err=%v", err)
	}
	if _, err := New(Config{EndpointID: "ep", TenantID: "tenant", Secret: []byte("01234567890123456789012345678901"), Registry: reg}, testLinker{}, w); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("missing durable ports err=%v", err)
	}
	c, secret, _, state := newConnector(t, testLinker{link: IdentityLink{Resolved: true, WorkerRef: "w", ResolutionRef: "r", EvidenceRef: "e"}}, w)
	r := request(secret, "guard", body("2026-09-28T09:00:00Z", "2026-09-28T17:00:00Z"), time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	r.Payload = []byte(`{"topic":"Timesheet.Insert","data":[]}`)
	r.DeputySignature = deputyMAC(secret, r.Payload)
	if _, err := c.VerifyAndImport(context.Background(), r, time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("topic/empty guard err=%v", err)
	}
	state.err = errors.New("state unavailable")
	r = request(secret, "state-error", body("2026-09-28T09:00:00Z", "2026-09-28T17:00:00Z"), time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	if _, err := c.VerifyAndImport(context.Background(), r, time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("state failure must be returned")
	}
}
func (w *testWriter) Correct(_ context.Context, in SourcedCorrection) error {
	if w.err != nil {
		return w.err
	}
	w.corrections = append(w.corrections, in)
	return nil
}

func profileRegistry(t *testing.T) clock.ProfileRegistry {
	t.Helper()
	profiles := []clock.IntegrationProfile{{Class: clock.SourceThirdPartyApp, Transport: "HTTPS_WEBHOOK", Authentication: "SIGNED_WEBHOOK", TrustCeiling: clock.TrustCeilingMedium, PermittedMethods: []clock.IdentificationMethod{clock.MethodPIN}, FirstPartner: "Deputy webhooks", Version: "v1"}}
	r, err := clock.NewProfileRegistry(profiles)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func newConnector(t *testing.T, linker IdentityLinker, writer ClockObservationPort) (*Connector, []byte, *webhook.Store, *testState) {
	t.Helper()
	secret := []byte("01234567890123456789012345678901")
	receipts := webhook.NewStore()
	if err := receipts.RegisterEndpoint(webhook.Endpoint{ID: "ep-1", TenantID: "tenant-1", ConnectionID: "deputy:ep-1", Secret: secret, AllowedSchemas: []string{deputySchema}, MaxPayloadBytes: 1 << 20, ReplayWindow: 5 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	state := newTestState()
	c, err := New(Config{EndpointID: "ep-1", TenantID: "tenant-1", Secret: secret, Registry: profileRegistry(t), ReceiptStore: receipts, StateStore: state}, linker, writer)
	if err != nil {
		t.Fatal(err)
	}
	return c, secret, receipts, state
}
func deputyMAC(secret, body []byte) string {
	h := hmac.New(sha256.New, secret)
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
func request(secret []byte, event, body string, at time.Time) WebhookRequest {
	b := []byte(body)
	return WebhookRequest{EventID: event, TenantID: "tenant-1", Topic: "Timesheet.Update", GenerationTime: at, DeputySignature: deputyMAC(secret, b), Payload: b}
}
func body(start, end string) string {
	return `{"topic":"Timesheet.Update","data":{"Id":42,"EmployeeId":7,"Start":"` + start + `","End":"` + end + `"}}`
}

func TestTodo_TCLOCK_013(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	w := &testWriter{}
	c, secret, _, _ := newConnector(t, testLinker{link: IdentityLink{WorkerRef: "worker-7", ResolutionRef: "resolution-1", EvidenceRef: "link-evidence-1", Resolved: true}}, w)
	result, err := c.VerifyAndImport(context.Background(), request(secret, "event-1", body("2026-09-28T09:00:00Z", "2026-09-28T17:00:00Z"), at), at)
	if err != nil {
		t.Fatal(err)
	}
	if result.Observed != 2 || len(w.observations) != 2 {
		t.Fatalf("observed=%d writes=%d", result.Observed, len(w.observations))
	}
	if w.observations[0].TrustCeiling != clock.TrustCeilingMedium || w.observations[0].Provider != "Deputy" || w.observations[0].WorkerRef != "worker-7" {
		t.Fatalf("untrusted mapping: %+v", w.observations[0])
	}
	if result.ReceiptID == "" {
		t.Fatal("receipt id is required")
	}
}

func TestTodo_TCLOCK_013_Integration(t *testing.T) {
	b, err := PlanSync(syncjob.Cursor{}, []syncjob.SourceItem{{ID: "42", Fingerprint: "a", Origin: "deputy"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Writes) != 1 || b.Writes[0].Reason != syncjob.ReasonNew {
		t.Fatalf("sync batch=%+v", b)
	}
	b, err = PlanSync(b.Cursor, []syncjob.SourceItem{{ID: "42", Fingerprint: "b", Origin: "deputy"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Writes) != 1 || b.Writes[0].Reason != syncjob.ReasonChanged {
		t.Fatalf("changed batch=%+v", b)
	}
}

func TestTodo_TCLOCK_013_Security(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	w := &testWriter{}
	c, secret, _, _ := newConnector(t, testLinker{link: IdentityLink{Resolved: false}}, w)
	req := request(secret, "event-sec", body("2026-09-28T09:00:00Z", "2026-09-28T17:00:00Z"), at)
	req.DeputySignature = "00"
	if _, err := c.VerifyAndImport(context.Background(), req, at); !errors.Is(err, webhook.ErrInvalidSignature) {
		t.Fatalf("bad signature err=%v", err)
	}
	req = request(secret, "event-unmatched", body("2026-09-28T09:00:00Z", "2026-09-28T17:00:00Z"), at)
	result, err := c.VerifyAndImport(context.Background(), req, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Exceptions) != 1 || result.Exceptions[0].Kind != "UNMATCHED_EXTERNAL_USER" || len(w.observations) != 0 {
		t.Fatalf("unmatched=%+v observations=%d", result.Exceptions, len(w.observations))
	}
}

func TestTodo_TCLOCK_013_Fault(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	w := &testWriter{}
	c, secret, _, _ := newConnector(t, testLinker{err: ErrProviderUnavailable}, w)
	result, err := c.VerifyAndImport(context.Background(), request(secret, "event-fault", `{"topic":"Timesheet.Update","data":{"Id":42,"EmployeeId":7,"Start":"bad","End":"bad"}}`, at), at)
	if err != nil || len(result.Exceptions) != 1 || result.Exceptions[0].Kind != "MALFORMED_RECORD" {
		t.Fatalf("malformed payload result=%+v err=%v", result, err)
	}
	_, err = c.VerifyAndImport(context.Background(), request(secret, "event-provider", body("2026-09-28T09:00:00Z", "2026-09-28T17:00:00Z"), at), at)
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("provider error=%v", err)
	}
}

func TestTodo_TCLOCK_013_Recovery(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	w := &testWriter{}
	c, secret, receipts, state := newConnector(t, testLinker{link: IdentityLink{WorkerRef: "worker-7", ResolutionRef: "resolution-1", EvidenceRef: "evidence-1", Resolved: true}}, w)
	first := request(secret, "event-recover-1", body("2026-09-28T09:00:00Z", "2026-09-28T17:00:00Z"), at)
	if _, err := c.VerifyAndImport(context.Background(), first, at); err != nil {
		t.Fatal(err)
	}
	// A fresh connector sharing durable receipts and provider state retains the
	// original observation references across a process restart.
	var err error
	c, err = New(Config{EndpointID: "ep-1", TenantID: "tenant-1", Secret: secret, Registry: profileRegistry(t), ReceiptStore: receipts, StateStore: state}, testLinker{link: IdentityLink{WorkerRef: "worker-7", ResolutionRef: "resolution-1", EvidenceRef: "evidence-1", Resolved: true}}, w)
	if err != nil {
		t.Fatal(err)
	}
	second := request(secret, "event-recover-2", body("2026-09-28T10:00:00Z", "2026-09-28T18:00:00Z"), at.Add(time.Second))
	result, err := c.VerifyAndImport(context.Background(), second, at.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if result.Corrected != 1 || len(w.corrections) != 2 || w.corrections[0].OriginalDigest == "" {
		t.Fatalf("corrections=%+v result=%+v", w.corrections, result)
	}
}
