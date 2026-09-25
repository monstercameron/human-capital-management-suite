package transport

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

type diagnosticSQLState string

func (s diagnosticSQLState) Error() string    { return "secret database payload" }
func (s diagnosticSQLState) SQLState() string { return string(s) }

func TestRequestDiagnosticClassificationDoesNotExposePayload(t *testing.T) {
	for err, want := range map[error]string{context.Canceled: "CANCELED", context.DeadlineExceeded: "DEADLINE_EXCEEDED"} {
		if got := diagnosticType(envelope.New(envelope.CodeUnavailable, "test", "unavailable").WithDiagnostic(fmt.Errorf("wrapped: %w", err))); got != want {
			t.Fatalf("got %s want %s", got, want)
		}
	}
	for state, want := range map[string]string{"42703": "SCHEMA_MISMATCH", "42P01": "SCHEMA_MISMATCH", "40001": "TRANSACTION_CONFLICT", "40P01": "TRANSACTION_CONFLICT", "23505": "UNIQUENESS_CONFLICT", "XX000": "DATABASE_FAILURE"} {
		err := envelope.New(envelope.CodeUnavailable, "test", "unavailable").WithDiagnostic(fmt.Errorf("wrapped: %w", diagnosticSQLState(state)))
		record := NewLogRecord("/test", KindGRPC, nil, "req-test", time.Millisecond, err)
		if record.ErrorType != want || strings.Contains(fmt.Sprintf("%+v", record), "secret") {
			t.Fatalf("unsafe or incorrect record: %+v", record)
		}
	}
	if got := diagnosticType(envelope.New(envelope.CodeUnavailable, "test", "unavailable")); got != "UNCLASSIFIED" {
		t.Fatal(got)
	}
	if got := diagnosticType(envelope.New(envelope.CodeUnavailable, "test", "unavailable").WithDiagnostic(fmt.Errorf("secret"))); got != "INTERNAL_FAILURE" {
		t.Fatal(got)
	}
}

func TestOwnedErrorNil(t *testing.T) {
	if got := OwnedError(nil, nil); got != nil {
		t.Fatalf("expected nil got %v", got)
	}
}

func TestOwnedErrorClassify(t *testing.T) {
	owned := envelope.New(envelope.CodeNotFound, "test.not_found", "not found")
	got := OwnedError(owned, nil)
	if got.Code() != envelope.CodeNotFound {
		t.Fatalf("code %v", got.Code())
	}
	deadline := OwnedError(context.DeadlineExceeded, nil)
	if deadline.Code() != envelope.CodeDeadlineExceeded {
		t.Fatalf("code %v", deadline.Code())
	}
	canceled := OwnedError(context.Canceled, nil)
	if canceled.Code() != envelope.CodeUnavailable {
		t.Fatalf("code %v", canceled.Code())
	}
	plain := OwnedError(assertErr("boom"), nil)
	if plain.Code() == envelope.CodeUnspecified {
		t.Fatal("plain should be coerced to error")
	}
}

func assertErr(s string) error { return &testErr{s} }

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }

func TestMapMetadata(t *testing.T) {
	m := MapMetadata{"Authorization": {"Bearer token"}, "X-Request-ID": {"req-1"}}
	if got := m.Get("authorization"); len(got) == 0 || got[0] != "Bearer token" {
		t.Fatalf("get %v", got)
	}
	if got := m.Get("x-request-id"); len(got) == 0 {
		t.Fatalf("get %v", got)
	}
	if keys := m.Keys(); len(keys) != 2 {
		t.Fatalf("keys %v", keys)
	}
	if got := m.Get("missing"); got != nil {
		t.Fatalf("expected nil got %v", got)
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("/hcmnext.intents.v1.IntentService/ListIntents", nil); err != nil {
		t.Fatalf("nil msg should not error %v", err)
	}
	v := DefaultValidator{}
	if err := v.Validate("/hcmnext.intents.v1.IntentService/ListIntents", nil); err != nil {
		t.Fatal(err)
	}
}

func TestNewLogRecord(t *testing.T) {
	rec := NewLogRecord("/m", KindGRPC, nil, "req-1", time.Millisecond, nil)
	if rec.RequestID != "req-1" {
		t.Fatalf("id %q", rec.RequestID)
	}
	if !rec.Succeeded() {
		t.Fatal("should succeed")
	}
	err := envelope.New(envelope.CodeNotFound, "x", "y")
	rec2 := NewLogRecord("/m", KindHTTPEdge, nil, "", time.Millisecond, err)
	if rec2.Code != envelope.CodeNotFound {
		t.Fatalf("code %v", rec2.Code)
	}
	if rec2.Succeeded() {
		t.Fatal("should not succeed")
	}
}

// TestNewLogRecordUnclassifiedErrorIsNeverLoggedAsOK pins the fix for a request
// log that recorded a real failure as "outcome":"OK". CodeUnspecified is both
// the zero value of [envelope.Code] (no error at all) and the code an
// unclassified internal error projects to (see callErr's fallback in
// internal/transport/chat), so Succeeded must not infer success from Code
// alone: it has to come from whether NewLogRecord was handed an error.
func TestNewLogRecordUnclassifiedErrorIsNeverLoggedAsOK(t *testing.T) {
	err := envelope.New(envelope.CodeUnspecified, "chat.internal_error", "the chat operation could not be completed")
	rec := NewLogRecord("/m", KindGRPC, nil, "req-unclassified", time.Millisecond, err)
	if rec.Code != envelope.CodeUnspecified {
		t.Fatalf("code = %v, want CodeUnspecified", rec.Code)
	}
	if rec.Succeeded() {
		t.Fatal("an unclassified internal error must not be logged as a success")
	}
	if !rec.Failed {
		t.Fatal("Failed must be set for a non-nil error, independent of Code")
	}
}

func TestLoggerFunc(t *testing.T) {
	called := false
	var f LoggerFunc = func(r LogRecord) { called = true }
	f.LogRequest(LogRecord{})
	if !called {
		t.Fatal("not called")
	}
}

func TestCapDeadline(t *testing.T) {
	cfg := Config{MaxDeadline: time.Second}
	ctx, cancel := CapDeadline(context.Background(), cfg)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("cap should set deadline")
	}
	ctx2, cancel2 := CapDeadline(context.Background(), Config{MaxDeadline: 10 * time.Millisecond})
	defer cancel2()
	_ = ctx2
	short, cancel3 := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel3()
	ctx4, cancel4 := CapDeadline(short, cfg)
	defer cancel4()
	_ = ctx4
}

func TestTrustedFingerprint(t *testing.T) {
	inv := &Invocation{method: "/m", tenantID: "t", organizationScopeID: "org", purpose: "p"}
	fp1 := inv.TrustedFingerprint()
	fp2 := inv.TrustedFingerprint()
	if fp1 != fp2 {
		t.Fatal("not deterministic")
	}
	if len(fp1) != 64 {
		t.Fatalf("len %d", len(fp1))
	}
	inv2 := &Invocation{method: "/m2", tenantID: "t", organizationScopeID: "org", purpose: "p"}
	if inv.TrustedFingerprint() == inv2.TrustedFingerprint() {
		t.Fatal("different method same fingerprint")
	}
}
