package timecardservice

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type workerRecordAuth struct{ denied bool }

func (a workerRecordAuth) Authorize(context.Context, *trust.Principal, string, string, Capability) error {
	if a.denied {
		return ErrForbidden
	}
	return nil
}

type workerRecordResolver struct {
	worker string
	err    error
}

func (r workerRecordResolver) ResolveOwnWorker(context.Context, string, string) (string, error) {
	return r.worker, r.err
}

type workerRecordReader struct {
	rows                               []WorkerEvidence
	seenTenant, seenWorker, seenCursor string
}

func (r *workerRecordReader) ListWorkerEvidence(_ context.Context, tenant, worker string, _ time.Time, _ time.Time, cursor string, _ int) ([]WorkerEvidence, string, error) {
	r.seenTenant, r.seenWorker, r.seenCursor = tenant, worker, cursor
	if cursor != "" {
		return nil, "", nil
	}
	return r.rows, "next-id", nil
}

func workerRecordPrincipal(t *testing.T, tenant, subject string, kind trust.SubjectKind) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: kind,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "record-session", IssuedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), CredentialDigest: "digest",
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

func workerRecordFixture() WorkerEvidence {
	return WorkerEvidence{ID: "obs-1", Tenant: "tenant-a", WorkerRef: "worker-a", Kind: "PUNCH", Source: "device", ReceiptRef: "receipt-1", Confidence: "HIGH", EffectiveDate: time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), EvaluatedDate: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC), RawPayload: json.RawMessage(`{"event":"CLOCK_IN"}`), PhotoEvidence: json.RawMessage(`"secret-photo"`), LocationEvidence: json.RawMessage(`{"lat":1}`)}
}

func workerRecordService(reader WorkerRecordReader) WorkerRecordService {
	s := NewWorkerRecordService(workerRecordAuth{}, workerRecordResolver{worker: "worker-a"}, reader, "record-cursor-secret-32-bytes-long")
	s.Now = func() time.Time { return time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC) }
	return s
}

func workerRecordQuery() WorkerRecordQuery {
	return WorkerRecordQuery{From: time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC), Limit: 10}
}

func TestTodo_TCLOCK_017(t *testing.T) {
	reader := &workerRecordReader{rows: []WorkerEvidence{workerRecordFixture()}}
	page, err := workerRecordService(reader).ReadOwnRecords(context.Background(), workerRecordPrincipal(t, "tenant-a", "subject-a", trust.SubjectKindHuman), workerRecordQuery())
	if err != nil {
		t.Fatalf("ReadOwnRecords: %v", err)
	}
	if len(page.Records) != 1 || page.Records[0].ReceiptRef != "receipt-1" || page.Records[0].Confidence != "HIGH" {
		t.Fatalf("typed worker projection = %+v", page.Records)
	}
	if reader.seenTenant != "tenant-a" || reader.seenWorker != "worker-a" {
		t.Fatalf("reader scope = %q/%q", reader.seenTenant, reader.seenWorker)
	}
}

func TestTodo_TCLOCK_017_Security(t *testing.T) {
	reader := &workerRecordReader{rows: []WorkerEvidence{workerRecordFixture()}}
	service := workerRecordService(reader)
	p := workerRecordPrincipal(t, "tenant-a", "subject-a", trust.SubjectKindHuman)
	q := workerRecordQuery()
	q.WorkerRef = "worker-b"
	if _, err := service.ReadOwnRecords(context.Background(), p, q); !errors.Is(err, ErrForbidden) {
		t.Fatalf("claimed other worker = %v, want forbidden", err)
	}
	foreign := workerRecordPrincipal(t, "tenant-b", "subject-a", trust.SubjectKindHuman)
	if _, err := service.ReadOwnRecords(context.Background(), foreign, workerRecordQuery()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant row = %v, want not found", err)
	}
	if reader.seenTenant != "tenant-b" {
		t.Fatalf("cross-tenant reader scope = %q", reader.seenTenant)
	}
	machine := workerRecordPrincipal(t, "tenant-a", "subject-a", trust.SubjectKindService)
	if _, err := service.ReadOwnRecords(context.Background(), machine, workerRecordQuery()); !errors.Is(err, ErrInvalidPrincipal) {
		t.Fatalf("machine principal = %v, want invalid principal", err)
	}
}

func TestTodo_TCLOCK_017_Golden(t *testing.T) {
	reader := &workerRecordReader{rows: []WorkerEvidence{workerRecordFixture()}}
	service := workerRecordService(reader)
	p := workerRecordPrincipal(t, "tenant-a", "subject-a", trust.SubjectKindHuman)
	want := `{"worker_ref":"worker-a","from":"2026-09-28T00:00:00Z","to":"2026-09-29T00:00:00Z","records":[{"id":"obs-1","kind":"PUNCH","source":"device","receipt_ref":"receipt-1","confidence":"HIGH","effective_date":"2026-09-28T00:00:00Z","evaluated_date":"2026-09-28T12:00:00Z"}]}`
	got, err := service.ExportOwnRecords(context.Background(), p, workerRecordQuery())
	if err != nil {
		t.Fatalf("ExportOwnRecords: %v", err)
	}
	if string(got) != want {
		t.Fatalf("export bytes = %s, want %s", got, want)
	}
	for _, forbidden := range []string{"secret-photo", "lat", "CLOCK_IN"} {
		if string(got) == forbidden || contains(string(got), forbidden) {
			t.Fatalf("export leaked %q: %s", forbidden, got)
		}
	}
}

func TestTodo_TCLOCK_017_Property(t *testing.T) {
	reader := &workerRecordReader{rows: []WorkerEvidence{{ID: "x", Tenant: "tenant-a", WorkerRef: "worker-a", Kind: "PUNCH", Source: "device", EffectiveDate: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC), EvaluatedDate: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC), RawPayload: json.RawMessage(`{"photo_evidence":"secret"}`)}}}
	service := workerRecordService(reader)
	_, err := service.ReadOwnRecords(context.Background(), workerRecordPrincipal(t, "tenant-a", "subject-a", trust.SubjectKindHuman), workerRecordQuery())
	if !errors.Is(err, ErrForbidden) || !IsSensitiveProjectionError(err) {
		t.Fatalf("forbidden raw field error = %v", err)
	}
}

func contains(s, sub string) bool { return len(sub) > 0 && stringsIndex(s, sub) >= 0 }

func stringsIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
