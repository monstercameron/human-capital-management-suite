package webhook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"
)

func testEndpoint(t *testing.T) (Endpoint, Request, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	ep := Endpoint{ID: "ep-1", TenantID: "tenant-1", ConnectionID: "conn-1", Secret: []byte("secret"), AllowedSchemas: []string{"promotion.v1"}, MaxPayloadBytes: 1024, ReplayWindow: 5 * time.Minute}
	req := Request{EndpointID: ep.ID, TenantID: ep.TenantID, EventID: "evt-1", EventType: "promotion.updated", Schema: "promotion.v1", Timestamp: now, Payload: []byte(`{"worker":"w-1","version":2}`), Correlation: "corr-1"}
	req.Signature = Sign(ep.Secret, req)
	return ep, req, now
}

func TestTodo_INTG_018(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	r, err := s.Receive(req, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.SignatureState != "VALID" || r.PayloadRef == "" || r.PayloadHash == "" {
		t.Fatalf("invalid receipt: %#v", r)
	}
	if _, err := s.Replay(r.ID, ReplayApproval{Actor: "ops", Purpose: "recovery", Approved: true}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_INTG_018_Golden(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	r1, err := s.Receive(req, now)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.Receive(req, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	const want = "receipt=sha256:c0aa889c46685ea3b0176dadcb5671858048d6ce60a0edff6afbf537315421c4 event=evt-1 signature=VALID disposition=RECEIVED replay_key=ep-1:evt-1"
	if got := Explain(r1); got != want {
		t.Fatalf("receipt golden bytes = %q, want %q", got, want)
	}
	if r1.ID != "sha256:c0aa889c46685ea3b0176dadcb5671858048d6ce60a0edff6afbf537315421c4" || r1.ID != r2.ID || Explain(r2) != want {
		t.Fatalf("duplicate changed deterministic identity: first=%#v duplicate=%#v", r1, r2)
	}
}

func TestStoreAcceptsDistinctEventsWithoutColliding(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for i := 0; i < 3; i++ {
		req.EventID = "evt-" + string(rune('1'+i))
		req.Signature = Sign(ep.Secret, req)
		receipt, err := s.Receive(req, now)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.EventID != req.EventID || seen[receipt.ID] {
			t.Fatalf("distinct event received unexpected identity: event=%q receipt=%+v", req.EventID, receipt)
		}
		seen[receipt.ID] = true
	}
	if len(seen) != 3 {
		t.Fatalf("distinct events produced %d receipt identities, want 3", len(seen))
	}
}

func TestTodo_INTG_018_Fault(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	bad := req
	bad.Timestamp = now.Add(-time.Hour)
	bad.Signature = Sign(ep.Secret, bad)
	if _, err := s.Receive(bad, now); !errors.Is(err, ErrOutsideReplayWindow) {
		t.Fatalf("got %v", err)
	}
	bad = req
	bad.Payload = make([]byte, ep.MaxPayloadBytes+1)
	bad.Signature = Sign(ep.Secret, bad)
	if _, err := s.Receive(bad, now); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestTodo_INTG_018_Security(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	bad := req
	bad.TenantID = "tenant-other"
	bad.Signature = Sign(ep.Secret, bad)
	if _, err := s.Receive(bad, now); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("got %v", err)
	}
	if _, err := s.Receive(req, now); err != nil {
		t.Fatal(err)
	}
	r, err := s.Get(digestText(req.EndpointID + ":" + req.EventID + "|" + hexPayloadHash(req.Payload)))
	if err != nil || r.PayloadRef == "" {
		t.Fatalf("metadata receipt was not retained: %#v err=%v", r, err)
	}
}

func hexPayloadHash(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func TestTodo_INTG_018_Recovery(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	r, err := s.Receive(req, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Replay(r.ID, ReplayApproval{Actor: "recovery", Purpose: "redelivery", Approved: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.EventID != req.EventID || got.ReplayOf != r.ID || !bytes.Equal(got.Payload, req.Payload) {
		t.Fatalf("recovery changed event: %#v", got)
	}
}

func TestTodo_INTG_018_Mutation(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Receive(req, now); err != nil {
		t.Fatal(err)
	}
	changed := req
	changed.Payload = []byte("different")
	changed.Signature = Sign(ep.Secret, changed)
	if _, err := s.Receive(changed, now); !errors.Is(err, ErrDuplicateDifferent) {
		t.Fatalf("got %v", err)
	}
}

func TestTodo_INTG_018_Race(t *testing.T) {
	ep, req, now := testEndpoint(t)
	s := NewStore()
	if err := s.RegisterEndpoint(ep); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan Receipt, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r, err := s.Receive(req, now); results <- r; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	var id string
	for r := range results {
		if id == "" {
			id = r.ID
		}
		if r.ID != id {
			t.Fatal("concurrent receipt identity changed")
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func FuzzTodo_INTG_018(f *testing.F) {
	f.Add([]byte("payload"), "promotion.v1")
	f.Add([]byte{}, "not-allowed.v1")
	f.Add([]byte("payload"), "")
	f.Add(bytes.Repeat([]byte("x"), 1025), "promotion.v1")
	f.Fuzz(func(t *testing.T, payload []byte, schema string) {
		ep, req, now := testEndpoint(t)
		req.Payload = payload
		req.Schema = schema
		req.Signature = Sign(ep.Secret, req)
		s := NewStore()
		if err := s.RegisterEndpoint(ep); err != nil {
			t.Fatal(err)
		}
		receipt, err := s.Receive(req, now)
		switch {
		case schema == "":
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("empty schema error=%v, want ErrInvalidRequest", err)
			}
		case len(payload) > ep.MaxPayloadBytes:
			if !errors.Is(err, ErrPayloadTooLarge) {
				t.Fatalf("oversized payload (%d bytes) error=%v, want ErrPayloadTooLarge", len(payload), err)
			}
		case schema != "promotion.v1":
			if !errors.Is(err, ErrUnknownSchema) {
				t.Fatalf("disallowed schema %q error=%v, want ErrUnknownSchema", schema, err)
			}
		case err != nil:
			t.Fatalf("allowed payload rejected: %v", err)
		default:
			wantPayloadHash := sha256.Sum256(payload)
			if receipt.EventID != req.EventID || receipt.PayloadHash != hex.EncodeToString(wantPayloadHash[:]) || receipt.PayloadRef == "" || receipt.SignatureState != "VALID" {
				t.Fatalf("accepted receipt violates identity/hash/quarantine invariant: %+v", receipt)
			}
			duplicate, err := s.Receive(req, now.Add(time.Second))
			if err != nil || duplicate.ID != receipt.ID || duplicate.PayloadHash != receipt.PayloadHash {
				t.Fatalf("identical replay = %+v, %v; want same receipt identity and hash", duplicate, err)
			}
		}
	})
}
