package webhook

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt"
	corewebhook "github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

type receiptSink struct {
	byID     map[string]providerreceipt.Parsed
	accepts  int
	approval corewebhook.ReplayApproval
	received time.Time
}

func (s *receiptSink) Accept(_ context.Context, parsed providerreceipt.Parsed, received time.Time) (Disposition, error) {
	if s.byID == nil {
		s.byID = make(map[string]providerreceipt.Parsed)
	}
	s.accepts++
	s.received = received
	if old, ok := s.byID[parsed.EventID]; ok {
		if old.PayloadDigest != parsed.PayloadDigest {
			return "", providerreceipt.ErrDuplicateDifferent
		}
		return DispositionDuplicate, nil
	}
	// The sink owns the exact bytes in quarantine and retains a copy.
	parsed.Payload = append([]byte(nil), parsed.Payload...)
	s.byID[parsed.EventID] = parsed
	return DispositionAccepted, nil
}

func TestTodo_INTG_018_FixedProviderRoutes(t *testing.T) {
	receiver, sink, headers, body, now := receiverFixture(t)
	routes := OverlayProviderReceivers(http.NotFoundHandler(), receiver, nil)
	path := PathForProvider(PayrollProvider)
	if path != Path+"/payroll" || PathForProvider("other") != "" {
		t.Fatalf("provider paths: payroll=%q unsupported=%q", path, PathForProvider("other"))
	}
	serve := func(route string, h http.Header) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, route, bytesReader(body))
		req.Header = h.Clone()
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, req)
		return w
	}
	if response := serve(path, headers); response.Code != http.StatusAccepted || !sink.received.Equal(now) {
		t.Fatalf("configured provider route: status=%d sink time=%v, want 202 at %v", response.Code, sink.received, now)
	}
	if response := serve(PathForProvider(IAMProvider), headers); response.Code != http.StatusNotFound {
		t.Fatalf("unconfigured IAM route status=%d, want 404", response.Code)
	}
	wrongTenant := headers.Clone()
	wrongTenant.Set(providerreceipt.HeaderTenant, "tenant-b")
	if response := serve(path, wrongTenant); response.Code != http.StatusUnauthorized || sink.accepts != 1 {
		t.Fatalf("foreign tenant callback: status=%d sink accepts=%d, want 401 without persistence", response.Code, sink.accepts)
	}

	unconfigured := OverlayProviderReceivers(http.NotFoundHandler(), nil, nil)
	for _, route := range []string{PathForProvider(PayrollProvider), PathForProvider(IAMProvider)} {
		w := httptest.NewRecorder()
		unconfigured.ServeHTTP(w, httptest.NewRequest(http.MethodPost, route, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("unconfigured route %s status=%d, want 404", route, w.Code)
		}
	}
}

func (s *receiptSink) Replay(_ context.Context, receiptID string, approval corewebhook.ReplayApproval) (providerreceipt.Parsed, error) {
	s.approval = approval
	parsed, ok := s.byID[receiptID]
	if !ok {
		return providerreceipt.Parsed{}, errors.New("missing receipt")
	}
	parsed.Payload = append([]byte(nil), parsed.Payload...)
	return parsed, nil
}

func receiverFixture(t *testing.T) (Receiver, *receiptSink, http.Header, []byte, time.Time) {
	t.Helper()
	secret := []byte("unit-secret-0123456789abcdef-current")
	endpoint := providerreceipt.PayrollEndpoint("endpoint-a", "tenant-a", secret)
	verifier, err := providerreceipt.NewVerifierWithRegistry(endpoint, idempotency.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"event_id":"event-a","change_ref":"payroll:change-a","correlation_key":"corr-a","provider_ref":"provider-a","outcome":"APPLIED","effective_date":"2026-10-01","base_pay":{"amount":"10.00","currency":"USD"}}`)
	req := corewebhook.Request{EventID: "event-a", EventType: providerreceipt.PayrollEventApplied, Schema: providerreceipt.PayrollSchema, Timestamp: now, Payload: body}
	header := make(http.Header)
	header.Set(providerreceipt.HeaderID, req.EventID)
	header.Set(providerreceipt.HeaderEvent, req.EventType)
	header.Set(providerreceipt.HeaderSchema, req.Schema)
	header.Set(providerreceipt.HeaderTenant, "tenant-a")
	header.Set(providerreceipt.HeaderTimestamp, strconv.FormatInt(now.UnixNano(), 10))
	header.Set(providerreceipt.HeaderSignature, corewebhook.Sign(secret, req))
	sink := &receiptSink{}
	return Receiver{Verifier: verifier, Sink: sink, Now: func() time.Time { return now }}, sink, header, body, now
}

func TestTodo_INTG_018_Transport(t *testing.T) {
	receiver, sink, headers, body, _ := receiverFixture(t)
	serve := func(h http.Header, b []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, Path, bytesReader(b))
		req.Header = h.Clone()
		w := httptest.NewRecorder()
		receiver.ServeHTTP(w, req)
		return w
	}
	if got := serve(headers, body); got.Code != http.StatusAccepted || sink.accepts != 1 {
		t.Fatalf("valid callback: status=%d accepts=%d body=%s", got.Code, sink.accepts, got.Body.String())
	}
	if got := serve(headers, body); got.Code != http.StatusAccepted || sink.accepts != 2 {
		t.Fatalf("identical replay: status=%d accepts=%d", got.Code, sink.accepts)
	}
	if got := sink.byID["event-a"]; string(got.Payload) != string(body) || got.TenantID != "tenant-a" {
		t.Fatalf("sink did not receive verified exact receipt: %+v", got)
	}

	bad := headers.Clone()
	bad.Set(providerreceipt.HeaderSignature, "bad")
	if got := serve(bad, body); got.Code != http.StatusUnauthorized || sink.accepts != 2 {
		t.Fatalf("invalid signature reached sink: status=%d accepts=%d", got.Code, sink.accepts)
	}
	if _, err := receiver.Replay(context.Background(), "event-a", corewebhook.ReplayApproval{}); !errors.Is(err, corewebhook.ErrReplayNotAuthorized) {
		t.Fatalf("unapproved replay = %v", err)
	}
	replayed, err := receiver.Replay(context.Background(), "event-a", corewebhook.ReplayApproval{Actor: "operator-1", Purpose: "incident recovery", Approved: true})
	if err != nil || string(replayed.Payload) != string(body) || !sink.approval.Approved {
		t.Fatalf("approved replay = %+v, %v", replayed, err)
	}
}

func TestTodo_INTG_018_TransportRejectsMethodAndOversize(t *testing.T) {
	receiver, sink, headers, body, _ := receiverFixture(t)
	get := httptest.NewRecorder()
	receiver.ServeHTTP(get, httptest.NewRequest(http.MethodGet, Path, nil))
	if get.Code != http.StatusMethodNotAllowed || get.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("GET response = %d Allow=%q", get.Code, get.Header().Get("Allow"))
	}
	receiver.MaxBytes = int64(len(body) - 1)
	req := httptest.NewRequest(http.MethodPost, Path, bytesReader(body))
	req.Header = headers
	tooLarge := httptest.NewRecorder()
	receiver.ServeHTTP(tooLarge, req)
	if tooLarge.Code != http.StatusRequestEntityTooLarge || sink.accepts != 0 {
		t.Fatalf("oversize response = %d, sink accepts=%d", tooLarge.Code, sink.accepts)
	}
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
