package timeimport

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

type importLinker struct{ link IdentityLink }

func (l importLinker) Resolve(context.Context, string, string) (IdentityLink, error) {
	return l.link, nil
}

type importWriter struct {
	observations []SourcedObservation
	corrections  []SourcedCorrection
	err          error
}

func (w *importWriter) Observe(_ context.Context, observation SourcedObservation) (string, error) {
	if w.err != nil {
		return "", w.err
	}
	w.observations = append(w.observations, observation)
	return "observation:" + observation.ProviderRecordID + ":" + string(observation.Event), nil
}

func (w *importWriter) Correct(_ context.Context, correction SourcedCorrection) error {
	if w.err != nil {
		return w.err
	}
	w.corrections = append(w.corrections, correction)
	return nil
}

type importState struct{ records map[string]StateRecord }

func (s *importState) Load(_ context.Context, tenant, provider, id string) (StateRecord, bool, error) {
	r, ok := s.records[tenant+"\x00"+provider+"\x00"+id]
	return r, ok, nil
}

func (s *importState) Save(_ context.Context, record StateRecord) error {
	s.records[record.Tenant+"\x00Deputy\x00"+record.ProviderRecord] = record
	return nil
}

func importFixture(t *testing.T, linker IdentityLinker, writer ClockObservationPort) (*Connector, []byte, *importState) {
	t.Helper()
	secret := []byte("01234567890123456789012345678901")
	receipts := webhook.NewStore()
	if err := receipts.RegisterEndpoint(webhook.Endpoint{
		ID: "endpoint-1", TenantID: "tenant-1", ConnectionID: "deputy:endpoint-1", Secret: secret,
		AllowedSchemas: []string{"deputy.timesheet/v1"}, MaxPayloadBytes: 1 << 20, ReplayWindow: 5 * time.Minute,
	}); err != nil {
		t.Fatal(err)
	}
	registry, err := clock.NewProfileRegistry([]clock.IntegrationProfile{{
		Class: clock.SourceThirdPartyApp, Transport: "HTTPS_WEBHOOK", Authentication: "SIGNED_WEBHOOK",
		TrustCeiling: clock.TrustCeilingMedium, PermittedMethods: []clock.IdentificationMethod{clock.MethodPIN},
		FirstPartner: "Deputy webhooks", Version: "v1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	state := &importState{records: make(map[string]StateRecord)}
	connector, err := NewDeputy(Config{EndpointID: "endpoint-1", TenantID: "tenant-1", Secret: secret, Registry: registry, ReceiptStore: receipts, StateStore: state}, linker, writer)
	if err != nil {
		t.Fatal(err)
	}
	return connector, secret, state
}

func signedImportRequest(secret []byte, eventID, body string, at time.Time) WebhookRequest {
	payload := []byte(body)
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write(payload)
	return WebhookRequest{EventID: eventID, TenantID: "tenant-1", Topic: "Timesheet.Update", GenerationTime: at, DeputySignature: hex.EncodeToString(h.Sum(nil)), Payload: payload}
}

const importTimesheet = `{"topic":"Timesheet.Update","data":{"Id":42,"EmployeeId":7,"Start":"2026-09-28T09:00:00Z","End":"2026-09-28T17:00:00Z"}}`

// TestTodo_TCLOCK_013 proves that an imported interval is an external sourced
// observation with an exact identity link, provider record, and trust ceiling.
func TestTodo_TCLOCK_013(t *testing.T) {
	writer := &importWriter{}
	connector, secret, _ := importFixture(t, importLinker{link: IdentityLink{WorkerRef: "worker-7", ResolutionRef: "resolution-1", EvidenceRef: "evidence-1", Resolved: true}}, writer)
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	result, err := VerifyAndImport(context.Background(), connector, signedImportRequest(secret, "event-1", importTimesheet, at), at)
	if err != nil {
		t.Fatal(err)
	}
	if result.Observed != 2 || len(writer.observations) != 2 {
		t.Fatalf("result=%+v observations=%d", result, len(writer.observations))
	}
	for _, observation := range writer.observations {
		if observation.SourceClass != clock.SourceThirdPartyApp || observation.TrustCeiling != clock.TrustCeilingMedium || observation.ProviderRecordID != "42" || observation.WorkerRef != "worker-7" {
			t.Fatalf("observation lost provenance: %+v", observation)
		}
	}
}

// TestTodo_TCLOCK_013_Integration proves the application facade uses the
// shared resumable SyncJob contract for provider pulls.
func TestTodo_TCLOCK_013_Integration(t *testing.T) {
	first, err := PlanSync(syncjob.Cursor{JobID: "deputy-timesheets"}, []syncjob.SourceItem{{ID: "record-1", Fingerprint: "v1", Origin: "deputy"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := PlanSync(first.Cursor, []syncjob.SourceItem{{ID: "record-1", Fingerprint: "v1", Origin: "deputy"}, {ID: "record-2", Fingerprint: "v1", Origin: "deputy"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Writes) != 1 || second.Writes[0].ID != "record-2" {
		t.Fatalf("sync writes=%+v, want only new record", second.Writes)
	}
}

// TestTodo_TCLOCK_013_Security proves an unresolved or conflicting external
// user never becomes a guessed worker observation.
func TestTodo_TCLOCK_013_Security(t *testing.T) {
	writer := &importWriter{}
	connector, secret, _ := importFixture(t, importLinker{link: IdentityLink{Resolved: false}}, writer)
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	result, err := connector.VerifyAndImport(context.Background(), signedImportRequest(secret, "unmatched", importTimesheet, at), at)
	if err != nil {
		t.Fatal(err)
	}
	if len(writer.observations) != 0 || len(result.Exceptions) != 1 || result.Exceptions[0].Kind != "UNMATCHED_EXTERNAL_USER" {
		t.Fatalf("unmatched import=%+v observations=%+v", result, writer.observations)
	}
}

// TestTodo_TCLOCK_013_Fault proves malformed provider bytes and bad signatures
// fail before identity resolution or canonical writes.
func TestTodo_TCLOCK_013_Fault(t *testing.T) {
	writer := &importWriter{}
	connector, secret, _ := importFixture(t, importLinker{link: IdentityLink{WorkerRef: "worker-7", ResolutionRef: "r", EvidenceRef: "e", Resolved: true}}, writer)
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	badSignature := signedImportRequest(secret, "bad-signature", importTimesheet, at)
	badSignature.DeputySignature = "00"
	if _, err := connector.VerifyAndImport(context.Background(), badSignature, at); !errors.Is(err, webhook.ErrInvalidSignature) {
		t.Fatalf("signature error=%v", err)
	}
	malformed := signedImportRequest(secret, "malformed", `{`, at)
	if _, err := connector.VerifyAndImport(context.Background(), malformed, at); !errors.Is(err, ErrInvalidPayload) {
		t.Fatalf("payload error=%v", err)
	}
	if len(writer.observations) != 0 {
		t.Fatalf("fault path wrote observations=%+v", writer.observations)
	}
}

// TestTodo_TCLOCK_013_Recovery proves a provider edit appends corrections to
// the original record instead of replacing its approved evidence.
func TestTodo_TCLOCK_013_Recovery(t *testing.T) {
	writer := &importWriter{}
	connector, secret, _ := importFixture(t, importLinker{link: IdentityLink{WorkerRef: "worker-7", ResolutionRef: "r", EvidenceRef: "e", Resolved: true}}, writer)
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	first, err := connector.VerifyAndImport(context.Background(), signedImportRequest(secret, "first", importTimesheet, at), at)
	if err != nil || first.Observed != 2 {
		t.Fatalf("first import=%+v err=%v", first, err)
	}
	updated := `{"topic":"Timesheet.Update","data":{"Id":42,"EmployeeId":7,"Start":"2026-09-28T09:30:00Z","End":"2026-09-28T17:00:00Z"}}`
	second, err := connector.VerifyAndImport(context.Background(), signedImportRequest(secret, "second", updated, at.Add(time.Minute)), at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.Corrected != 1 || len(writer.observations) != 2 || len(writer.corrections) != 2 {
		t.Fatalf("recovery result=%+v observations=%d corrections=%d", second, len(writer.observations), len(writer.corrections))
	}
}
