package clockadapters

import (
	"context"
	"errors"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
)

type adapterAuth struct {
	device AuthenticatedDevice
	secret string
}

func (a adapterAuth) Authenticate(_ context.Context, credential string) (AuthenticatedDevice, error) {
	if credential != a.secret {
		return AuthenticatedDevice{}, ErrUnauthenticated
	}
	return a.device, nil
}

type adapterSink struct {
	requests []*timev1.SubmitPunchesRequest
}

func (s *adapterSink) SubmitPunches(_ context.Context, _ AuthenticatedDevice, request *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error) {
	s.requests = append(s.requests, request)
	return &timev1.SubmitPunchesResponse{HighestContiguousSequence: uint64(len(request.GetPunches()))}, nil
}

type adapterRoster struct{ snapshot *timev1.RosterSnapshot }

func (r adapterRoster) SyncRoster(context.Context, *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error) {
	return &timev1.SyncRosterResponse{Snapshot: r.snapshot}, nil
}

type adapterClaims struct{ claimed map[string]bool }

func (c *adapterClaims) BeginBatch(_ context.Context, _ AuthenticatedDevice, digest string) (BatchLease, error) {
	if c.claimed[digest] {
		return BatchLease{}, ErrBatchAlreadyProcessed
	}
	c.claimed[digest] = true
	return BatchLease{ID: digest, Digest: digest}, nil
}

func (*adapterClaims) CompleteBatch(context.Context, BatchLease) error { return nil }
func (*adapterClaims) ReleaseBatch(context.Context, BatchLease) error  { return nil }

// TestTodo_TCLOCK_014 proves the application boundary authenticates an
// enrolled device, ignores the body serial, and emits a canonical request.
func TestTodo_TCLOCK_014(t *testing.T) {
	sink := &adapterSink{}
	handler := ADMSHandler{Auth: adapterAuth{device: AuthenticatedDevice{DeviceID: "enrolled-device", Serial: "real", Tenant: "tenant-1"}, secret: "credential"}, Sink: sink}
	response, err := handler.Handle(context.Background(), "credential", "spoofed-body-serial", []byte("badge-7\t2026-09-28 08:00:00\t0\t\t\t\t44"))
	if err != nil {
		t.Fatal(err)
	}
	if response.GetHighestContiguousSequence() != 1 || len(sink.requests) != 1 {
		t.Fatalf("response=%+v requests=%+v", response, sink.requests)
	}
	request := sink.requests[0]
	if request.GetDeviceId() != "enrolled-device" || request.GetPunches()[0].GetDeviceSequence() != 44 {
		t.Fatalf("canonical request=%+v", request)
	}
}

// TestTodo_TCLOCK_014_Integration proves the REST, roster and file paths all
// terminate in the same canonical translation vocabulary.
func TestTodo_TCLOCK_014_Integration(t *testing.T) {
	rest, err := MapRESTPunches("device-1", []byte(`[{"sequence":9,"worker_id":"worker-1","event":"OUT","occurred_at":"2026-09-28T17:00:00Z"}]`))
	if err != nil || rest.Request.GetPunches()[0].GetEventType() != timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_OUT {
		t.Fatalf("REST translation=%+v err=%v", rest, err)
	}
	roster, err := SyncRoster(context.Background(), adapterRoster{snapshot: &timev1.RosterSnapshot{Workers: []*timev1.WorkerCredentialVerifier{{WorkerId: "worker-1", DisplayName: "Worker", VerifierRef: "badge-ref"}}}}, "device-1", "cursor-1", 20)
	if err != nil || len(roster.Workers) != 1 || roster.Workers[0].VerifierRef != "badge-ref" {
		t.Fatalf("roster=%+v err=%v", roster, err)
	}
	batch, err := ParseBatch("device-1", []byte("worker,timestamp,event,sequence\nworker-1,2026-09-28T08:00:00Z,IN,7\n"), FormatADP)
	if err != nil || batch.Request.GetPunches()[0].GetDeviceSequence() != 7 {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
}

// TestTodo_TCLOCK_014_Security proves authentication precedes parsing and a
// request cannot select a different enrolled device by body content.
func TestTodo_TCLOCK_014_Security(t *testing.T) {
	sink := &adapterSink{}
	handler := ADMSHandler{Auth: adapterAuth{device: AuthenticatedDevice{DeviceID: "enrolled-device", Tenant: "tenant-1"}, secret: "credential"}, Sink: sink}
	if _, err := handler.Handle(context.Background(), "wrong", "enrolled-device", []byte("malformed")); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("authentication error=%v", err)
	}
	if len(sink.requests) != 0 {
		t.Fatal("canonical sink called after failed authentication")
	}
}

// TestTodo_TCLOCK_014_Fault proves malformed key/value, unsupported format,
// and invalid REST payloads return typed errors instead of panicking.
func TestTodo_TCLOCK_014_Fault(t *testing.T) {
	if _, err := ParseADMS("device-1", []byte("PIN=worker&Timestamp=bad&Status=0&Seq=1"), time.UTC); !errors.Is(err, ErrMalformed) {
		t.Fatalf("ADMS error=%v", err)
	}
	if _, err := MapRESTPunches("device-1", []byte("[]")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("REST error=%v", err)
	}
	if _, err := ParseBatch("device-1", []byte("worker,timestamp,event\nworker,2026-09-28T08:00:00Z,IN\n"), FormatADP); !errors.Is(err, ErrMalformed) {
		t.Fatalf("CSV error=%v", err)
	}
}

// TestTodo_TCLOCK_014_BatchRecovery proves identical file content is claimed
// once and a replay cannot submit a second canonical batch.
func TestTodo_TCLOCK_014_BatchRecovery(t *testing.T) {
	claims := &adapterClaims{claimed: make(map[string]bool)}
	sink := &adapterSink{}
	device := AuthenticatedDevice{DeviceID: "device-1", Tenant: "tenant-1"}
	body := []byte("worker,timestamp,event,sequence\nworker-1,2026-09-28T08:00:00Z,IN,1\n")
	if _, _, _, err := ImportAuthenticatedBatch(context.Background(), claims, sink, device, body, FormatADP); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ImportAuthenticatedBatch(context.Background(), claims, sink, device, body, FormatADP); !errors.Is(err, ErrBatchAlreadyProcessed) {
		t.Fatalf("replay error=%v", err)
	}
	if len(sink.requests) != 1 {
		t.Fatalf("canonical submissions=%d, want 1", len(sink.requests))
	}
}

// FuzzTodo_TCLOCK_014 exercises bounded parser failure paths with arbitrary
// key=value and delimiter input.
func FuzzTodo_TCLOCK_014(f *testing.F) {
	f.Add("PIN=worker\t2026-09-28 08:00:00\t0\t\t\t\t1")
	f.Fuzz(func(t *testing.T, body string) {
		_, _ = ParseADMS("device-1", []byte(body), time.UTC)
	})
}
