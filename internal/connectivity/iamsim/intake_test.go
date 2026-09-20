package iamsim

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestIntakeAccepted(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.NewID = func() string { return "abcdef01-2345-6789" } })
	tok := issueToken(t, h.s, "")
	req := validRequest("http://127.0.0.1:1/cb")
	rec := postChange(t, h.s, tok, req.ChangeRef, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d %s", rec.Code, rec.Body)
	}
	var ack acceptResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &ack); err != nil {
		t.Fatal(err)
	}
	if ack != (acceptResponse{ChangeRef: "iam:rev-1", ProviderRef: "IAMSIM-ABCDEF012345", Status: StatusAccepted}) {
		t.Fatalf("ack = %+v", ack)
	}
}

func TestIntakeIdempotentReplayAndConflict(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Scenario.GrantDelayMS = 0 })
	tok := issueToken(t, h.s, "")
	req := validRequest("http://127.0.0.1:1/cb")
	first := postChange(t, h.s, tok, req.ChangeRef, req)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first = %d", first.Code)
	}
	// A replay must survive a scenario switch that would now refuse intake.
	h.s.mu.Lock()
	h.s.scenario.Mode = ModeRejectAtIntake
	h.s.mu.Unlock()
	replay := postChange(t, h.s, tok, req.ChangeRef, req)
	if replay.Code != http.StatusOK || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %s, want 200 %s", replay.Code, replay.Body, first.Body)
	}
	changed := req
	changed.Grade = "M5"
	conflict := postChange(t, h.s, tok, req.ChangeRef, changed)
	if conflict.Code != http.StatusConflict || errorCode(t, conflict) != "idempotency_key_reused" {
		t.Fatalf("conflict = %d %s", conflict.Code, conflict.Body)
	}
	if len(h.s.changes) != 1 || h.s.changes[req.ChangeRef].req.Grade != "M4" {
		t.Fatal("conflicting replay mutated the stored change")
	}
}

func TestIntakeValidation(t *testing.T) {
	h := newHarness(t, nil)
	tok := issueToken(t, h.s, "")
	base := validRequest("http://127.0.0.1:1/cb")
	cases := []struct {
		name   string
		mutate func(*AccessChangeRequest)
		field  string
	}{
		{"tenant", func(r *AccessChangeRequest) { r.Tenant = " " }, "tenant"},
		{"worker", func(r *AccessChangeRequest) { r.WorkerRef = "" }, "worker_ref"},
		{"job empty", func(r *AccessChangeRequest) { r.JobCode = "" }, "job_code"},
		{"job lowercase", func(r *AccessChangeRequest) { r.JobCode = "sal-dir" }, "job_code"},
		{"job too long", func(r *AccessChangeRequest) { r.JobCode = strings.Repeat("A", 33) }, "job_code"},
		{"grade space", func(r *AccessChangeRequest) { r.Grade = "M 4" }, "grade"},
		{"date format", func(r *AccessChangeRequest) { r.EffectiveDate = "2026-12-1" }, "effective_date"},
		{"date invalid", func(r *AccessChangeRequest) { r.EffectiveDate = "2026-13-01" }, "effective_date"},
		{"correlation", func(r *AccessChangeRequest) { r.CorrelationKey = "" }, "correlation_key"},
		{"callback scheme", func(r *AccessChangeRequest) { r.CallbackURL = "ftp://x/y" }, "callback_url"},
		{"callback relative", func(r *AccessChangeRequest) { r.CallbackURL = "/receipts" }, "callback_url"},
		{"ref mismatch", func(r *AccessChangeRequest) { r.ChangeRef = "iam:other" }, "change_ref"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.mutate(&req)
			rec := postChange(t, h.s, tok, base.ChangeRef, req)
			var body map[string]string
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if rec.Code != http.StatusBadRequest || body["error"] != "invalid" || body["field"] != tc.field {
				t.Fatalf("got %d %s, want field %s", rec.Code, rec.Body, tc.field)
			}
		})
	}
	edge := base
	edge.JobCode = strings.Repeat("A", 32)
	if rec := postChange(t, h.s, tok, edge.ChangeRef, edge); rec.Code != http.StatusAccepted {
		t.Fatalf("32-char job code = %d %s", rec.Code, rec.Body)
	}

	if rec := postChange(t, h.s, tok, "", base); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Idempotency-Key") {
		t.Fatalf("missing key = %d %s", rec.Code, rec.Body)
	}
	if rec := postChange(t, h.s, tok, strings.Repeat("k", MaxIdempotencyKeyLen+1), base); rec.Code != http.StatusBadRequest {
		t.Fatalf("long key = %d", rec.Code)
	}
	if rec := postChange(t, h.s, tok, base.ChangeRef, "{not json"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"body"`) {
		t.Fatalf("bad json = %d %s", rec.Code, rec.Body)
	}
	big := `{"change_ref":"` + strings.Repeat("x", MaxRequestBytes) + `"}`
	if rec := postChange(t, h.s, tok, base.ChangeRef, big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d", rec.Code)
	}
}

func TestIntakeScenarioRejectAtIntakeAndFlaky(t *testing.T) {
	h := newHarness(t, func(c *Config) {
		c.Scenario.Mode = ModeRejectAtIntake
		c.Scenario.RejectReason = "grade not provisioned"
	})
	tok := issueToken(t, h.s, "")
	req := validRequest("http://127.0.0.1:1/cb")
	rec := postChange(t, h.s, tok, req.ChangeRef, req)
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusUnprocessableEntity || body["error"] != "rejected" || body["reason"] != "grade not provisioned" {
		t.Fatalf("reject_at_intake = %d %s", rec.Code, rec.Body)
	}

	roll := 0.1
	hf := newHarness(t, func(c *Config) {
		c.Scenario.Mode = ModeFlaky
		c.Scenario.FlakyRate = 0.5
		c.Random = func() float64 { return roll }
	})
	tok = issueToken(t, hf.s, "")
	if rec := postChange(t, hf.s, tok, req.ChangeRef, req); rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "unavailable" {
		t.Fatalf("flaky failure = %d %s", rec.Code, rec.Body)
	}
	if len(hf.s.changes) != 0 {
		t.Fatal("failed flaky intake stored a change")
	}
	roll = 0.9
	if rec := postChange(t, hf.s, tok, req.ChangeRef, req); rec.Code != http.StatusAccepted {
		t.Fatalf("flaky success = %d", rec.Code)
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("abc-def"); got != "ABCDEF" {
		t.Fatalf("shortID = %q", got)
	}
	if got := shortID("0123456789abcdefXYZ"); got != "0123456789AB" {
		t.Fatalf("shortID = %q", got)
	}
}
