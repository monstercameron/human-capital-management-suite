package envelope_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// allowDiagnostics is an access grant that permits reading an error's nested
// internal diagnostic. Only an operator-facing surface would implement this in
// production; the transports never do.
type allowDiagnostics struct{}

func (allowDiagnostics) AllowsInternalDiagnostics() bool { return true }

// denyDiagnostics refuses the grant.
type denyDiagnostics struct{}

func (denyDiagnostics) AllowsInternalDiagnostics() bool { return false }

// TestTodo_CAP_003 is the CAP-003 primary test. It pins the owned error model
// itself: one condition, one retry classification, one safe message, one set
// of field violations, and a nested diagnostic that no accessor without a
// grant can reach.
func TestTodo_CAP_003(t *testing.T) {
	t.Run("an owned error carries exactly the safe projection", func(t *testing.T) {
		err := envelope.New(envelope.CodeFailedPrecondition, "intent.stale_revision",
			"a precondition for the operation is not met").
			WithViolation("expected_instance_version", "the expected instance version is stale", "intent.expected_revision").
			WithCorrelation("req-1").
			WithEvidence(envelope.Evidence{ID: "ev:decision:stale", Kind: "domain_decision", Digest: "sha256:abc"}).
			WithDiagnostic(errors.New(`pq: could not serialize access due to concurrent update`))

		if err.Code() != envelope.CodeFailedPrecondition {
			t.Errorf("code = %v", err.Code())
		}
		if err.Retryable() {
			t.Error("FAILED_PRECONDITION must not be classified retryable")
		}
		detail := err.Detail()
		if detail.GetCode() != commonv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION {
			t.Errorf("detail code = %v", detail.GetCode())
		}
		if got := detail.GetCorrelationId(); got != "req-1" {
			t.Errorf("correlation = %q", got)
		}
		if got := detail.GetEvidenceRef().GetEvidenceId(); got != "ev:decision:stale" {
			t.Errorf("evidence = %q", got)
		}
		if len(detail.GetFieldViolations()) != 1 {
			t.Fatalf("field violations = %d, want 1", len(detail.GetFieldViolations()))
		}
		if got := detail.GetFieldViolations()[0].GetFieldPath(); got != "expected_instance_version" {
			t.Errorf("field path = %q", got)
		}
	})

	t.Run("the nested diagnostic is not reachable without a grant", func(t *testing.T) {
		internal := errors.New(`pq: relation "intents" does not exist`)
		err := envelope.New(envelope.CodeUnavailable, "storage.unavailable",
			"the request could not be served").WithDiagnostic(internal)

		if strings.Contains(err.Error(), "pq:") {
			t.Error("Error() leaked the nested diagnostic")
		}
		if strings.Contains(err.Message(), "pq:") {
			t.Error("Message() leaked the nested diagnostic")
		}
		if errors.Is(err, internal) {
			t.Error("errors.Is walked into the nested diagnostic")
		}
		if _, ok := err.Diagnostic(nil); ok {
			t.Error("a nil grant read the nested diagnostic")
		}
		if _, ok := err.Diagnostic(denyDiagnostics{}); ok {
			t.Error("a refusing grant read the nested diagnostic")
		}
		got, ok := err.Diagnostic(allowDiagnostics{})
		if !ok || !errors.Is(got, internal) {
			t.Error("an allowing grant should read the nested diagnostic")
		}
	})

	t.Run("an unsafe message is replaced rather than projected", func(t *testing.T) {
		for _, unsafe := range []string{
			`pq: duplicate key value violates unique constraint "intents_pkey"`,
			"panic: runtime error: index out of range\n\tgoroutine 17",
			"failed at internal/data/postgres/intents.go:214",
			"SELECT id FROM intents WHERE tenant_id = $1",
			"password=hunter2",
			strings.Repeat("x", 400),
			"",
		} {
			err := envelope.New(envelope.CodeInvalidArgument, "test.unsafe", unsafe)
			if unsafe != "" && strings.Contains(err.Message(), unsafe) {
				t.Errorf("unsafe message %q was projected verbatim", unsafe)
			}
			if err.Message() == "" {
				t.Error("an owned error must always have a message")
			}
		}
	})

	t.Run("an unclassified failure never becomes its own text", func(t *testing.T) {
		raw := errors.New(`pq: duplicate key value violates unique constraint "intents_pkey"`)
		owned := envelope.Coerce(raw)
		if owned.Code() != envelope.CodeUnavailable {
			t.Errorf("code = %v, want UNAVAILABLE", owned.Code())
		}
		if strings.Contains(owned.Message(), "pq:") {
			t.Error("Coerce projected the raw error text")
		}
		if got, ok := owned.Diagnostic(allowDiagnostics{}); !ok || !errors.Is(got, raw) {
			t.Error("Coerce should keep the raw error as an unprojected diagnostic")
		}
		alreadyOwned := envelope.New(envelope.CodeNotFound, "x", "not found")
		if envelope.Coerce(alreadyOwned) != alreadyOwned {
			t.Error("Coerce should return an already-owned error unchanged")
		}
	})

	t.Run("gRPC status projection carries the canonical detail", func(t *testing.T) {
		owned := envelope.New(envelope.CodeNotFound, "intent.not_found",
			"the resource does not exist or is not visible").
			WithViolation("intent_id", "no intent is visible at this identifier", "intent.visibility").
			WithCorrelation("req-2")

		st := status.Convert(owned)
		if st.Code() != codes.NotFound {
			t.Fatalf("status code = %v, want NotFound", st.Code())
		}
		roundTripped, ok := envelope.FromGRPC(st.Err())
		if !ok {
			t.Fatal("FromGRPC did not recognize the projected status")
		}
		assertSameMeaning(t, "grpc round trip", owned, roundTripped)
	})
}

func TestTodo_PROMOUX_007_Regression_MoneyRangeDetailRoundTrip(t *testing.T) {
	minimum, err := values.NewMoney("105.04", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := values.NewMoney("115.03", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	owned := envelope.New(envelope.CodeInvalidArgument, "journey.propose_promotion.input", "the request input is not acceptable").
		WithViolation("proposed_base", "review this field and try again", "promotion.ladder.base_increase_out_of_range").
		WithViolationMoneyRange(minimum, maximum)
	refusal := envelope.FromDetail(owned.Code(), owned.Message(), owned.Detail())
	got := refusal.Violations()
	if len(got) != 1 || got[0].MoneyRange != (envelope.MoneyRange{Minimum: "105.04", Maximum: "115.03", Currency: "USD"}) {
		t.Fatalf("typed monetary correction changed across envelope: %+v", got)
	}
	plain := envelope.New(envelope.CodeInvalidArgument, "journey.propose_promotion.input", "the request input is not acceptable").
		WithViolation("reason", "review this field and try again", "journey.input.invalid")
	if plain.Detail().GetFieldViolations()[0].GetPermittedMoneyRange() != nil {
		t.Fatal("an unrelated refusal acquired a pay range")
	}
	invalid := envelope.New(envelope.CodeInvalidArgument, "journey.propose_promotion.input", "the request input is not acceptable").
		WithViolation("proposed_base", "review this field and try again", "promotion.ladder.base_increase_out_of_range").
		WithViolationMoneyRange(maximum, minimum)
	if invalid.Detail().GetFieldViolations()[0].GetPermittedMoneyRange() != nil {
		t.Fatal("reversed bounds crossed the owned error boundary")
	}
	forged := &commonv1.ErrorDetail{FieldViolations: []*commonv1.FieldViolation{{
		FieldPath: "proposed_base", RuleRef: "promotion.ladder.base_increase_out_of_range",
		PermittedMoneyRange: &commonv1.MoneyRange{Minimum: "private SQL", Maximum: "115.03", Currency: "USD"},
	}}}
	if got := envelope.FromDetail(envelope.CodeInvalidArgument, "the request input is not acceptable", forged).Violations()[0].MoneyRange; got != (envelope.MoneyRange{}) {
		t.Fatalf("malformed wire detail entered the owned model: %+v", got)
	}
}

// TestTodo_CAP_003_Golden is the CAP-003 golden test. It pins the canonical
// error projection table from
// planning/specs/http-grpc-endpoint-contract.md#canonical-error-projection
// against the implementation, plus the one-for-one correspondence between the
// Go Code type and the wire enum. A change to either mapping has to change
// this table too, which is the point.
func TestTodo_CAP_003_Golden(t *testing.T) {
	golden := []struct {
		code      envelope.Code
		proto     commonv1.ErrorCode
		grpc      codes.Code
		http      int
		retryable bool
	}{
		{envelope.CodeInvalidArgument, commonv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, codes.InvalidArgument, http.StatusBadRequest, false},
		{envelope.CodeUnauthenticated, commonv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED, codes.Unauthenticated, http.StatusUnauthorized, false},
		{envelope.CodePermissionDenied, commonv1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, codes.PermissionDenied, http.StatusForbidden, false},
		{envelope.CodeNotFound, commonv1.ErrorCode_ERROR_CODE_NOT_FOUND, codes.NotFound, http.StatusNotFound, false},
		{envelope.CodeAlreadyExists, commonv1.ErrorCode_ERROR_CODE_ALREADY_EXISTS, codes.AlreadyExists, http.StatusConflict, false},
		{envelope.CodeAborted, commonv1.ErrorCode_ERROR_CODE_ABORTED, codes.Aborted, http.StatusConflict, true},
		{envelope.CodeFailedPrecondition, commonv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION, codes.FailedPrecondition, http.StatusPreconditionFailed, false},
		{envelope.CodeResourceExhausted, commonv1.ErrorCode_ERROR_CODE_RESOURCE_EXHAUSTED, codes.ResourceExhausted, http.StatusTooManyRequests, true},
		{envelope.CodeDeadlineExceeded, commonv1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, codes.DeadlineExceeded, http.StatusGatewayTimeout, false},
		{envelope.CodeUnavailable, commonv1.ErrorCode_ERROR_CODE_UNAVAILABLE, codes.Unavailable, http.StatusServiceUnavailable, true},
	}

	for _, row := range golden {
		if got := row.code.Proto(); got != row.proto {
			t.Errorf("%v.Proto() = %v, want %v", row.code, got, row.proto)
		}
		if got := envelope.CodeFromProto(row.proto); got != row.code {
			t.Errorf("CodeFromProto(%v) = %v, want %v", row.proto, got, row.code)
		}
		if got := row.code.GRPCCode(); got != row.grpc {
			t.Errorf("%v.GRPCCode() = %v, want %v", row.code, got, row.grpc)
		}
		if got := envelope.CodeFromGRPC(row.grpc); got != row.code {
			t.Errorf("CodeFromGRPC(%v) = %v, want %v", row.grpc, got, row.code)
		}
		if got := row.code.HTTPStatus(); got != row.http {
			t.Errorf("%v.HTTPStatus() = %d, want %d", row.code, got, row.http)
		}
		if got := envelope.DefaultRetryable(row.code); got != row.retryable {
			t.Errorf("DefaultRetryable(%v) = %v, want %v", row.code, got, row.retryable)
		}
	}

	t.Run("the golden table covers every defined code", func(t *testing.T) {
		for code := envelope.CodeInvalidArgument; code <= envelope.CodeUnavailable; code++ {
			found := false
			for _, row := range golden {
				if row.code == code {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("code %v is defined but absent from the golden projection table", code)
			}
		}
	})

	t.Run("unspecified and unknown values fail closed", func(t *testing.T) {
		if got := envelope.CodeUnspecified.HTTPStatus(); got != http.StatusInternalServerError {
			t.Errorf("unspecified HTTP status = %d, want 500", got)
		}
		if got := envelope.CodeUnspecified.GRPCCode(); got != codes.Internal {
			t.Errorf("unspecified gRPC code = %v, want Internal", got)
		}
		if got := envelope.CodeFromGRPC(codes.Unimplemented); got != envelope.CodeUnspecified {
			t.Errorf("CodeFromGRPC(Unimplemented) = %v, want unspecified", got)
		}
		if got := envelope.CodeFromProto(commonv1.ErrorCode(999)); got != envelope.CodeUnspecified {
			t.Errorf("CodeFromProto(999) = %v, want unspecified", got)
		}
	})
}

// FuzzTodo_CAP_003 is the CAP-003 fuzz target. Whatever text and field paths a
// call site supplies, the projected error must stay inside the owned model:
// a bounded printable message, a detail whose code matches, and never the
// nested diagnostic.
func FuzzTodo_CAP_003(f *testing.F) {
	f.Add(uint8(1), "reason.ref", "a safe message", "field.path", "diagnostic")
	f.Add(uint8(7), "", "", "", "")
	f.Add(uint8(10), "x", `pq: relation "intents" does not exist`, "scope.tenant_id", "internal/data/postgres/intents.go:214")
	f.Add(uint8(3), "r", strings.Repeat("m", 5000), strings.Repeat("f", 5000), "")
	f.Add(uint8(200), "r", "message\x00with\x01control", "f", "d")

	f.Fuzz(func(t *testing.T, rawCode uint8, reasonRef, message, fieldPath, diagnostic string) {
		code := envelope.Code(rawCode)
		err := envelope.New(code, reasonRef, message).
			WithViolation(fieldPath, message, reasonRef).
			WithDiagnostic(errors.New(diagnostic))

		if err.Message() == "" {
			t.Fatal("an owned error must always have a safe message")
		}
		if len(err.Message()) > 200 {
			t.Fatalf("projected message is %d bytes, beyond the owned bound", len(err.Message()))
		}
		for i := 0; i < len(err.Message()); i++ {
			if err.Message()[i] < 0x20 || err.Message()[i] > 0x7e {
				t.Fatalf("projected message contains a non-printable byte at %d", i)
			}
		}
		// A short diagnostic can appear inside an owned message by
		// coincidence ("d" is a substring of most English), so only a
		// distinctive one is evidence of a leak.
		if len(diagnostic) >= 12 && strings.Contains(err.Message(), diagnostic) {
			t.Fatal("the projected message contains the nested diagnostic")
		}

		detail := err.Detail()
		if detail == nil {
			t.Fatal("Detail() returned nil")
		}
		if detail.GetCode() != code.Proto() {
			t.Fatalf("detail code = %v, want %v", detail.GetCode(), code.Proto())
		}
		if got := err.HTTPStatus(); got < 400 || got > 599 {
			t.Fatalf("HTTP status %d is not a failure status", got)
		}

		// Round-tripping through the wire detail preserves meaning.
		// Only a defined condition round-trips; an out-of-range value has no
		// wire representation and collapses to unspecified by design.
		if code > envelope.CodeUnspecified && code <= envelope.CodeUnavailable {
			back := envelope.FromDetail(envelope.CodeFromGRPC(err.GRPCCode()), err.Message(), detail)
			if back.Code() != err.Code() {
				t.Fatalf("round trip changed the code: %v -> %v", err.Code(), back.Code())
			}
			if back.Retryable() != err.Retryable() {
				t.Fatalf("round trip changed the retry classification")
			}
		}
	})
}

// assertSameMeaning fails the test unless two owned errors agree on everything
// a caller can observe.
func assertSameMeaning(t *testing.T, label string, want, got *envelope.Error) {
	t.Helper()
	if want.Code() != got.Code() {
		t.Errorf("%s: code = %v, want %v", label, got.Code(), want.Code())
	}
	if want.Retryable() != got.Retryable() {
		t.Errorf("%s: retryable = %v, want %v", label, got.Retryable(), want.Retryable())
	}
	if want.CorrelationID() != got.CorrelationID() {
		t.Errorf("%s: correlation = %q, want %q", label, got.CorrelationID(), want.CorrelationID())
	}
	if want.EvidenceRef() != got.EvidenceRef() {
		t.Errorf("%s: evidence = %+v, want %+v", label, got.EvidenceRef(), want.EvidenceRef())
	}
	wantViolations, gotViolations := want.Violations(), got.Violations()
	if len(wantViolations) != len(gotViolations) {
		t.Fatalf("%s: %d violations, want %d", label, len(gotViolations), len(wantViolations))
	}
	for i := range wantViolations {
		if wantViolations[i] != gotViolations[i] {
			t.Errorf("%s: violation %d = %+v, want %+v", label, i, gotViolations[i], wantViolations[i])
		}
	}
}
