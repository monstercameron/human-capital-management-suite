package clockservice

import (
	"context"
	"errors"
	"testing"
	"time"

	clockdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
)

type tclockCaptureWorkers struct{ active bool }

func (w tclockCaptureWorkers) ResolveWorker(_ context.Context, _, claimed string) (string, bool, error) {
	return claimed, w.active, nil
}

func (tclockCaptureWorkers) ResolveAssignment(context.Context, string, string, string) (string, string, bool, error) {
	return "project", "site", true, nil
}

type tclockCapturePolicy struct{ location clockdomain.LocationEvidence }

func (p tclockCapturePolicy) ResolveCaptureLocation(context.Context, string, string, time.Time) (clockdomain.LocationEvidence, error) {
	return p.location, nil
}

func (tclockCapturePolicy) PunchPolicy(context.Context, string, string) (punchpolicy.Policy, error) {
	return punchpolicy.Policy{}, nil
}

func (tclockCapturePolicy) AttestationQuestions(context.Context, string, string) (punchpolicy.QuestionSet, error) {
	return punchpolicy.QuestionSet{}, nil
}

func TestTodo_CLOCK_002(t *testing.T) {
	now := time.Unix(20, 0).UTC()
	p := batchPrincipal(t)
	service := batchService(&batchCommitFake{})
	service.Workers = tclockCaptureWorkers{active: true}

	got, err := service.AuthenticateCapture(context.Background(), p, CaptureAuthenticationRequest{DeviceID: "dev-1", ClaimedWorkerRef: "worker-1", CapturedAt: now})
	if err != nil {
		t.Fatalf("capture = %v", err)
	}
	if !got.Result.Accepted || got.Result.Evidence.Device.DeviceRef != "dev-1" || got.Result.Evidence.Principal.Subject != p.Subject() {
		t.Fatalf("capture evidence = %+v", got.Result.Evidence)
	}
	if got.Result.Evidence.Worker.ClaimedWorkerRef != "worker-1" || got.Result.Evidence.Worker.ResolvedWorkerRef != "worker-1" {
		t.Fatalf("worker evidence collapsed or lost: %+v", got.Result.Evidence.Worker)
	}
	if got.Result.Evidence.Location.LocationRef != "site" {
		t.Fatalf("location evidence = %+v", got.Result.Evidence.Location)
	}

	service.Workers = tclockCaptureWorkers{active: false}
	if _, err := service.AuthenticateCapture(context.Background(), p, CaptureAuthenticationRequest{DeviceID: "dev-1", ClaimedWorkerRef: "worker-1", CapturedAt: now}); !errors.Is(err, clockdomain.ErrCaptureRejected) {
		t.Fatalf("inactive worker error = %v, want capture rejection", err)
	}

	service.Workers = tclockCaptureWorkers{active: true}
	service.Policies = tclockCapturePolicy{location: clockdomain.LocationEvidence{LocationRef: "site", PolicyVersion: "location/v1", EvidenceRef: "denied", Allowed: false}}
	if _, err := service.AuthenticateCapture(context.Background(), p, CaptureAuthenticationRequest{DeviceID: "dev-1", ClaimedWorkerRef: "worker-1", CapturedAt: now}); !errors.Is(err, clockdomain.ErrCaptureRejected) {
		t.Fatalf("prohibited location error = %v, want capture rejection", err)
	}
}

func TestTodo_CLOCK_002_Property(t *testing.T) {
	now := time.Unix(20, 0).UTC()
	service := batchService(&batchCommitFake{})
	service.Workers = tclockCaptureWorkers{active: true}
	p := batchPrincipal(t)
	request := CaptureAuthenticationRequest{DeviceID: "dev-1", ClaimedWorkerRef: "worker-1", CapturedAt: now}
	first, err := service.AuthenticateCapture(context.Background(), p, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.AuthenticateCapture(context.Background(), p, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Result.Evidence.Digest != second.Result.Evidence.Digest {
		t.Fatalf("identical capture changed digest: %q != %q", first.Result.Evidence.Digest, second.Result.Evidence.Digest)
	}
	request.ClaimedWorkerRef = "worker-2"
	changed, err := service.AuthenticateCapture(context.Background(), p, request)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Result.Evidence.Digest == first.Result.Evidence.Digest {
		t.Fatal("capture digest did not bind claimed worker")
	}
}

func offlineEntry(sequence uint64, occurred time.Time) OfflineForwardEntry {
	return OfflineForwardEntry{Sequence: sequence, EventType: clockdomain.EventClockIn, WorkerRef: "w1", OccurredAt: occurred, PayloadDigest: "sha256:payload-" + string(rune('0'+sequence))}
}

func TestTodo_CLOCK_004(t *testing.T) {
	now := time.Unix(20, 0).UTC()
	service := batchService(&batchCommitFake{})
	service.Clock = func() time.Time { return now }
	got, err := service.ForwardOffline(context.Background(), batchPrincipal(t), OfflineForwardRequest{DeviceID: "dev-1", Entries: []OfflineForwardEntry{
		offlineEntry(1, now.Add(-3*time.Second)), offlineEntry(2, now.Add(-2*time.Second)), offlineEntry(3, now.Add(-time.Second)),
	}})
	if err != nil {
		t.Fatalf("forward offline = %v", err)
	}
	if got.DeviceRef != "dev-1" || got.ReceiptDigest == "" || len(got.Entries) != 3 {
		t.Fatalf("result = %+v", got)
	}
	for i, entry := range got.Entries {
		if entry.Sequence != uint64(i+1) || entry.ReceiptOrder != uint64(i+1) || entry.Confidence != clockdomain.SyncConfidenceHigh {
			t.Fatalf("entry %d = %+v", i, entry)
		}
		if !entry.OccurredAt.Equal(now.Add(-time.Duration(3-i) * time.Second)) {
			t.Fatalf("entry %d occurred_at rewritten: %v", i, entry.OccurredAt)
		}
	}
}

func TestTodo_CLOCK_004_Property(t *testing.T) {
	now := time.Unix(20, 0).UTC()
	service := batchService(&batchCommitFake{})
	service.Clock = func() time.Time { return now }
	request := OfflineForwardRequest{DeviceID: "dev-1", Entries: []OfflineForwardEntry{offlineEntry(1, now.Add(-time.Minute)), offlineEntry(2, now.Add(-30*time.Second))}}
	first, err := service.ForwardOffline(context.Background(), batchPrincipal(t), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ForwardOffline(context.Background(), batchPrincipal(t), request)
	if err != nil || first.ReceiptDigest != second.ReceiptDigest {
		t.Fatalf("replay digest=%q/%q err=%v", first.ReceiptDigest, second.ReceiptDigest, err)
	}
	drifted := OfflineForwardRequest{DeviceID: "dev-1", Entries: []OfflineForwardEntry{offlineEntry(1, now.Add(6*time.Minute))}}
	degraded, err := service.ForwardOffline(context.Background(), batchPrincipal(t), drifted)
	if err != nil || len(degraded.Entries) != 1 || degraded.Entries[0].Confidence != clockdomain.SyncConfidenceDegraded {
		t.Fatalf("drifted result=%+v err=%v", degraded, err)
	}
}

func TestTodo_CLOCK_004_Recovery(t *testing.T) {
	now := time.Unix(20, 0).UTC()
	service := batchService(&batchCommitFake{})
	service.Clock = func() time.Time { return now }
	_, err := service.ForwardOffline(context.Background(), batchPrincipal(t), OfflineForwardRequest{DeviceID: "dev-1", Entries: []OfflineForwardEntry{
		offlineEntry(1, now.Add(-time.Minute)), offlineEntry(3, now.Add(-30*time.Second)),
	}})
	if !errors.Is(err, clockdomain.ErrOfflineRejected) {
		t.Fatalf("reordered offline log = %v, want ErrOfflineRejected", err)
	}
	_, err = service.ForwardOffline(context.Background(), batchPrincipal(t), OfflineForwardRequest{DeviceID: "dev-1", Entries: []OfflineForwardEntry{
		offlineEntry(1, now.Add(-time.Minute)), offlineEntry(2, now.Add(-30*time.Second)),
	}})
	if err != nil {
		t.Fatalf("recovery forward = %v", err)
	}
}

var _ CaptureLocationResolver = tclockCapturePolicy{}
var _ WorkerDirectory = tclockCaptureWorkers{}
var _ PolicySource = tclockCapturePolicy{}
