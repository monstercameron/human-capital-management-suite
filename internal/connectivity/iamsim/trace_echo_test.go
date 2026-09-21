package iamsim

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	inboundTrace   = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	inboundTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	revokeTrace    = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-00"
)

// jsonLogs switches the harness logger to JSON so tests can read attributes.
func jsonLogs(buf *syncBuffer) func(*Config) {
	return func(c *Config) { c.Logger = slog.New(slog.NewJSONHandler(buf, nil)) }
}

func logRecords(t *testing.T, buf *syncBuffer, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if rec["msg"] == msg {
			out = append(out, rec)
		}
	}
	return out
}

func send(t *testing.T, h http.Handler, method, path, token string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func traceParts(t *testing.T, tp string) (traceID, spanID, flags string) {
	t.Helper()
	p := strings.Split(tp, "-")
	if len(p) != 4 || p[0] != "00" || len(p[1]) != 32 || len(p[2]) != 16 || len(p[3]) != 2 {
		t.Fatalf("traceparent %q is not W3C-shaped", tp)
	}
	return p[1], p[2], p[3]
}

func TestIAMCallbackEchoesTraceAsChildAndCorrelationUnchanged(t *testing.T) {
	logs := &syncBuffer{}
	rc := newReceiver(t, http.StatusServiceUnavailable)
	h := newHarness(t, jsonLogs(logs))
	tokRec := tokenCall(t, h.s, url.Values{"grant_type": {"client_credentials"}}, basicAuth(testClientID, testClientSecret), formType)
	var tr TokenResponse
	if err := json.Unmarshal(tokRec.Body.Bytes(), &tr); err != nil {
		t.Fatal(err)
	}
	req := validRequest(rc.url())
	rec := send(t, h.s, http.MethodPost, "/v1/access-changes", tr.AccessToken, req, map[string]string{
		"Idempotency-Key": req.ChangeRef, "traceparent": inboundTrace, "X-Correlation-Id": "corr-iam-1",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d %s", rec.Code, rec.Body)
	}
	if code := send(t, h.s, http.MethodGet, "/v1/access-changes/"+url.PathEscape(req.ChangeRef), tr.AccessToken, nil,
		map[string]string{"traceparent": inboundTrace, "X-Correlation-Id": "corr-iam-1"}).Code; code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	drain(t, h.s)

	calls := rc.calls()
	if len(calls) != 2 {
		t.Fatalf("callbacks = %d, want a failed attempt and its retry", len(calls))
	}
	seen := map[string]bool{}
	for i, c := range calls {
		tid, sid, flags := traceParts(t, c.header.Get("traceparent"))
		if tid != inboundTraceID || flags != "01" || sid == "00f067aa0ba902b7" || seen[sid] {
			t.Fatalf("callback %d traceparent = %q, want a fresh child of %q", i, c.header.Get("traceparent"), inboundTrace)
		}
		seen[sid] = true
		if got := c.header.Get("X-Correlation-Id"); got != "corr-iam-1" {
			t.Fatalf("callback %d X-Correlation-Id = %q", i, got)
		}
	}
	if calls[1].err != nil {
		t.Fatalf("echo headers broke signature verification: %v", calls[1].err)
	}

	for _, msg := range []string{"intake accepted", "status read"} {
		recs := logRecords(t, logs, msg)
		if len(recs) != 1 || recs[0]["traceparent"] != inboundTrace || recs[0]["correlation_id"] != "corr-iam-1" {
			t.Fatalf("%q log = %v", msg, recs)
		}
	}
	if issued := logRecords(t, logs, "token issued"); len(issued) != 1 || issued[0]["traceparent"] != nil {
		t.Fatalf("token log = %v (no traceparent was sent)", issued)
	}
}

func TestIAMMalformedContextIgnoredAndRevocationEchoesItsOwnTrace(t *testing.T) {
	logs := &syncBuffer{}
	rc := newReceiver(t)
	h := newHarness(t, jsonLogs(logs))
	tok := issueToken(t, h.s, "")
	req := validRequest(rc.url())
	rec := send(t, h.s, http.MethodPost, "/v1/access-changes", tok, req, map[string]string{
		"Idempotency-Key":  req.ChangeRef,
		"traceparent":      "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-ff",
		"X-Correlation-Id": "bad\tvalue",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d", rec.Code)
	}
	waitStatus(t, h, tok, req.ChangeRef, StatusGranted, 1)

	rev := send(t, h.s, http.MethodPost, "/v1/access-changes/"+url.PathEscape(req.ChangeRef)+"/revocation", tok,
		map[string]string{"reason": "promotion withdrawn"},
		map[string]string{"Idempotency-Key": RevocationKeyPrefix + req.ChangeRef, "traceparent": revokeTrace, "X-Correlation-Id": "corr-revoke"})
	if rev.Code != http.StatusAccepted {
		t.Fatalf("revocation = %d %s", rev.Code, rev.Body)
	}
	drain(t, h.s)

	calls := rc.calls()
	if len(calls) != 2 {
		t.Fatalf("callbacks = %d, want grant + revocation", len(calls))
	}
	if calls[0].header.Get("traceparent") != "" || calls[0].header.Get("X-Correlation-Id") != "" {
		t.Fatalf("malformed intake context echoed: %v", calls[0].header)
	}
	tid, _, flags := traceParts(t, calls[1].header.Get("traceparent"))
	if tid != "0af7651916cd43dd8448eb211c80319c" || flags != "00" || calls[1].header.Get("X-Correlation-Id") != "corr-revoke" {
		t.Fatalf("revocation callback echo = %v", calls[1].header)
	}
	if accepted := logRecords(t, logs, "intake accepted"); len(accepted) != 1 || accepted[0]["traceparent"] != nil || accepted[0]["correlation_id"] != nil {
		t.Fatalf("malformed context logged: %v", accepted)
	}
	if revoked := logRecords(t, logs, "revocation accepted"); len(revoked) != 1 || revoked[0]["traceparent"] != revokeTrace || revoked[0]["correlation_id"] != "corr-revoke" {
		t.Fatalf("revocation log = %v", revoked)
	}
}

func TestIAMTokenIssueLogsInboundTraceContext(t *testing.T) {
	logs := &syncBuffer{}
	h := newHarness(t, jsonLogs(logs))
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(url.Values{"grant_type": {"client_credentials"}}.Encode()))
	req.Header.Set("Content-Type", formType)
	req.Header.Set("Authorization", basicAuth(testClientID, testClientSecret))
	req.Header.Set("traceparent", inboundTrace)
	req.Header.Set("X-Correlation-Id", "corr-token")
	rec := httptest.NewRecorder()
	h.s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("token = %d %s", rec.Code, rec.Body)
	}
	issued := logRecords(t, logs, "token issued")
	if len(issued) != 1 || issued[0]["traceparent"] != inboundTrace || issued[0]["correlation_id"] != "corr-token" {
		t.Fatalf("token log = %v", issued)
	}
	if strings.Contains(logs.String(), testClientSecret) {
		t.Fatal("token log leaked the client secret")
	}
}
