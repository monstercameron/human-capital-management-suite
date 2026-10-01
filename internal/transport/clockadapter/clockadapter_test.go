package clockadapter

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
)

type authFake struct {
	device     AuthenticatedDevice
	credential string
	calls      int
}

func (a *authFake) Authenticate(_ context.Context, credential string) (AuthenticatedDevice, error) {
	a.calls++
	if credential != a.credential {
		return AuthenticatedDevice{}, errors.New("bad credential")
	}
	return a.device, nil
}

type sinkFake struct {
	req   *timev1.SubmitPunchesRequest
	calls int
	err   error
}

func (s *sinkFake) SubmitPunches(_ context.Context, _ AuthenticatedDevice, req *timev1.SubmitPunchesRequest) (*timev1.SubmitPunchesResponse, error) {
	s.calls++
	s.req = req
	if s.err != nil {
		return nil, s.err
	}
	return &timev1.SubmitPunchesResponse{HighestContiguousSequence: uint64(len(req.GetPunches()))}, nil
}

type rosterFake struct{ snapshot *timev1.RosterSnapshot }

func (r rosterFake) SyncRoster(context.Context, *timev1.SyncRosterRequest) (*timev1.SyncRosterResponse, error) {
	return &timev1.SyncRosterResponse{Snapshot: r.snapshot}, nil
}

type claimFake struct{ claimed bool }

func (c *claimFake) BeginBatch(context.Context, AuthenticatedDevice, string) (BatchLease, error) {
	if !c.claimed {
		return BatchLease{}, ErrBatchAlreadyProcessed
	}
	c.claimed = false
	return BatchLease{ID: "x"}, nil
}
func (c *claimFake) CompleteBatch(context.Context, BatchLease) error { return nil }
func (c *claimFake) ReleaseBatch(context.Context, BatchLease) error  { return nil }

func TestTodo_TCLOCK_014(t *testing.T) {
	a := &authFake{device: AuthenticatedDevice{DeviceID: "enrolled-1", Serial: "real", Tenant: "t1"}, credential: "Bearer good"}
	s := &sinkFake{}
	h := ADMSHandler{Auth: a, Sink: s}
	got, err := h.Handle(context.Background(), "Bearer good", "spoof", []byte("123\t2026-09-28 08:00:00\t0\t1\tJOB\t\t44"))
	if err != nil {
		t.Fatal(err)
	}
	if got.GetHighestContiguousSequence() != 1 || s.req.GetDeviceId() != "enrolled-1" || s.req.GetPunches()[0].GetDeviceSequence() != 44 {
		t.Fatalf("unexpected canonical request: %v", s.req)
	}
}

func TestTodo_TCLOCK_014_Integration(t *testing.T) {
	tr, err := ParseADMS("dev", []byte("p1\t2026-09-28T08:00:00Z\tIN\t\t\t\t1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Request.GetPunches()[0].GetEventType() != timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_IN {
		t.Fatal("event not mapped")
	}
	rr, err := TranslateRoster("dev", &timev1.RosterSnapshot{Workers: []*timev1.WorkerCredentialVerifier{{WorkerId: "w", DisplayName: "W", VerifierRef: "v"}}})
	if err != nil || len(rr.Workers) != 1 {
		t.Fatalf("roster: %+v %v", rr, err)
	}
}

func TestTodo_TCLOCK_014_Security(t *testing.T) {
	a := &authFake{device: AuthenticatedDevice{DeviceID: "enrolled", Tenant: "t1"}, credential: "good"}
	s := &sinkFake{}
	h := ADMSHandler{Auth: a, Sink: s}
	_, err := h.Handle(context.Background(), "bad", "enrolled", []byte("attacker\t2026-09-28 08:00:00\t0"))
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("want auth error: %v", err)
	}
	if s.calls != 0 {
		t.Fatal("sink called after failed auth")
	}
	r := httptest.NewRequest("POST", "/iclock/cdata?SN=body", strings.NewReader("p\t2026-09-28 08:00:00\t0\t\t\t\t1"))
	r.Header.Set("Authorization", "good")
	w := httptest.NewRecorder()
	h.HTTPHandler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestAuthenticatedIdentityRequiresTenant(t *testing.T) {
	a := &authFake{device: AuthenticatedDevice{DeviceID: "d"}, credential: "ok"}
	s := &sinkFake{}
	_, err := (ADMSHandler{Auth: a, Sink: s}).Handle(context.Background(), "ok", "spoof", []byte("p\t2026-09-28 08:00:00\t0\t\t\t\t1"))
	if !errors.Is(err, ErrUnauthenticated) || s.calls != 0 {
		t.Fatalf("identity was not rejected: %v calls=%d", err, s.calls)
	}
}

func TestBatchRejectsUnstableOrUnboundedData(t *testing.T) {
	if _, err := ParseBatch("d", []byte("worker,timestamp,event\nw,2026-09-28T08:00:00Z,0\n"), FormatADP); !errors.Is(err, ErrMalformed) {
		t.Fatalf("missing sequence: %v", err)
	}
	if _, err := ParseBatch("d", []byte("worker,worker,timestamp,event,sequence\nw,w,2026-09-28T08:00:00Z,0,1\n"), FormatADP); !errors.Is(err, ErrMalformed) {
		t.Fatalf("duplicate header: %v", err)
	}
	if _, err := ParseBatch("d", []byte("worker,timestamp,event,sequence\n,2026-09-28T08:00:00Z,0,1\n"), FormatADP); err == nil {
		t.Fatal("blank worker accepted")
	}
	if _, err := ParseBatch("d", []byte("worker,timestamp,event,sequence\nw,2026-09-28T08:00:00Z,0,1\n"), BatchFormat("unknown")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("unknown format: %v", err)
	}
}

func TestTodo_TCLOCK_014_Fault(t *testing.T) {
	if _, err := ParseADMS("d", []byte("broken"), nil); !errors.Is(err, ErrMalformed) {
		t.Fatal(err)
	}
	if _, err := MapRESTPunches("d", []byte("[]")); !errors.Is(err, ErrMalformed) {
		t.Fatal(err)
	}
	if _, err := ParseBatch("d", []byte("worker,timestamp\n"), FormatADP); !errors.Is(err, ErrMalformed) {
		t.Fatal(err)
	}
}

func FuzzTodo_TCLOCK_014(f *testing.F) {
	f.Add("x\t2026-09-28 08:00:00\t0")
	f.Fuzz(func(t *testing.T, body string) { _, _ = ParseADMS("d", []byte(body), nil) })
}

func TestParseBatchFormats(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format BatchFormat
		body   string
	}{{"adp", FormatADP, "worker,timestamp,event,sequence\nw,2026-09-28T08:00:00Z,0,1\n"}, {"paychex", FormatPaychex, "Employee ID,Date,Type,Sequence\nw,2026-09-28T08:00:00Z,1,1\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			tr, e := ParseBatch("d", []byte(tc.body), tc.format)
			if e != nil || len(tr.Request.GetPunches()) != 1 {
				t.Fatalf("%v %v", tr, e)
			}
		})
	}
	if _, err := ParseBatch("d", []byte("!TIMERENTRY\tw\t09/28/2026\t08:00\n"), FormatQuickBooksIIF); !errors.Is(err, ErrMalformed) {
		t.Fatalf("IIF should be unsupported: %v", err)
	}
}

func TestADMSHTTPBoundedAndMethod(t *testing.T) {
	a := &authFake{device: AuthenticatedDevice{DeviceID: "d", Tenant: "t1"}, credential: "ok"}
	h := ADMSHandler{Auth: a, Sink: &sinkFake{}, MaxBodyBytes: 3}
	too := httptest.NewRequest("POST", "/iclock/cdata", strings.NewReader("1234"))
	too.Header.Set("Authorization", "ok")
	w := httptest.NewRecorder()
	h.HTTPHandler().ServeHTTP(w, too)
	if w.Code != 413 {
		t.Fatalf("body status %d", w.Code)
	}
	get := httptest.NewRequest("GET", "/iclock/cdata", nil)
	w = httptest.NewRecorder()
	h.HTTPHandler().ServeHTTP(w, get)
	if w.Code != 405 {
		t.Fatalf("method status %d", w.Code)
	}
}

func TestImportBatchClaimsAndREST(t *testing.T) {
	c := &claimFake{claimed: true}
	device := AuthenticatedDevice{DeviceID: "d", Tenant: "t"}
	_, digest, _, err := ImportAuthenticatedBatch(context.Background(), c, &sinkFake{}, device, []byte("x"), FormatADP)
	if err == nil || digest == "" {
		t.Fatalf("claim result %q %v", digest, err)
	}
	c.claimed = true
	c.claimed = true
	_, _, _, err = ImportAuthenticatedBatch(context.Background(), c, &sinkFake{}, device, []byte("worker,timestamp,event,sequence\nw,2026-09-28T08:00:00Z,0,1\n"), FormatADP)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = ImportAuthenticatedBatch(context.Background(), c, &sinkFake{}, device, []byte("worker,timestamp,event,sequence\nw,2026-09-28T08:00:00Z,0,1\n"), FormatADP)
	if !errors.Is(err, ErrBatchAlreadyProcessed) {
		t.Fatalf("want duplicate: %v", err)
	}
	tr, err := MapRESTPunches("d", []byte(`[{"sequence":1,"worker_id":"w","event":"OUT","occurred_at":"2026-09-28T08:00:00Z"}]`))
	if err != nil || tr.Request.GetPunches()[0].GetEventType() != timev1.PunchEventType_PUNCH_EVENT_TYPE_CLOCK_OUT {
		t.Fatalf("REST: %+v %v", tr, err)
	}
}

func TestSyncRosterDelegates(t *testing.T) {
	tr, err := SyncRoster(context.Background(), rosterFake{snapshot: &timev1.RosterSnapshot{Workers: []*timev1.WorkerCredentialVerifier{{WorkerId: "w"}}}}, "d", "cursor", 10)
	if err != nil || len(tr.Workers) != 1 {
		t.Fatalf("sync: %+v %v", tr, err)
	}
	if _, err := SyncRoster(context.Background(), nil, "d", "", 0); !errors.Is(err, ErrMalformed) {
		t.Fatal(err)
	}
}
