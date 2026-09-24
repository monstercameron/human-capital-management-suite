package attest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func executionFixture(t *testing.T) (*MemoryResponseStore, Response, ExecutionRequirement) {
	t.Helper()
	store := NewMemoryResponseStore()
	recorder, err := NewRecorder(store, TrustedClockFunc(func() (TrustedTime, error) { return testTrustedAt, nil }))
	if err != nil {
		t.Fatal(err)
	}
	request := testResponseRequest(ResponseAccepted)
	request.AffectedObligations = []string{"obligation-1"}
	response, err := recorder.RecordResponse(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	req := ExecutionRequirement{Tenant: response.Tenant, ObligationID: "obligation-1", StatementID: response.StatementID, StatementVersion: response.StatementVersion, StatementDigest: response.StatementDigest, BindingDigest: response.BindingDigest, ResponseID: response.ResponseID, ResponseRevision: response.Revision, TransactionID: response.TransactionID}
	return store, response, req
}

func TestValidateRequired_RefusesIdentityStateAndTimeBranches(t *testing.T) {
	store, response, req := executionFixture(t)
	valid, err := ValidateRequired(req, response, testTrustedAt)
	if err != nil || !valid.Allowed || valid.Reason != "ATTESTATION_ACCEPTED" || !valid.CheckedAt.At.Equal(testTrustedAt.At) {
		t.Fatalf("valid decision=%+v err=%v", valid, err)
	}
	cases := []struct {
		name           string
		mutateReq      func(*ExecutionRequirement)
		mutateResponse func(*Response)
		want           string
	}{
		{"tenant", func(r *ExecutionRequirement) { r.Tenant = "other" }, nil, "response_identity_mismatch"},
		{"response id", func(r *ExecutionRequirement) { r.ResponseID = "other" }, nil, "response_identity_mismatch"},
		{"revision", func(r *ExecutionRequirement) { r.ResponseRevision++ }, nil, "response_identity_mismatch"},
		{"statement id", func(r *ExecutionRequirement) { r.StatementID = "other" }, nil, "statement_or_binding_mismatch"},
		{"statement version", func(r *ExecutionRequirement) { r.StatementVersion++ }, nil, "statement_or_binding_mismatch"},
		{"statement digest", func(r *ExecutionRequirement) { r.StatementDigest = "other" }, nil, "statement_or_binding_mismatch"},
		{"binding digest", func(r *ExecutionRequirement) { r.BindingDigest = "other" }, nil, "statement_or_binding_mismatch"},
		{"refused", nil, func(r *Response) { r.Status = ResponseRefused }, "response_not_accepted"},
		{"correction", nil, func(r *Response) { r.Kind = AssertionCorrection }, "response_is_not_original_acceptance"},
		{"transaction", func(r *ExecutionRequirement) { r.TransactionID = "other" }, nil, "transaction_mismatch"},
		{"missing obligation coverage", nil, func(r *Response) {
			r.AffectedObligations = nil
			r.RequestDigest = requestDigest(responseRequest(*r))
			r.Digest = responseDigest(*r)
		}, "obligation_not_covered"},
		{"wrong obligation coverage", nil, func(r *Response) {
			r.AffectedObligations = []string{"obligation-other"}
			r.RequestDigest = requestDigest(responseRequest(*r))
			r.Digest = responseDigest(*r)
		}, "obligation_not_covered"},
		{"future", nil, func(r *Response) { r.RecordedAt.At = testTrustedAt.At.Add(time.Second) }, "response_recorded_in_future"},
		{"missing old evidence", nil, func(r *Response) { r.RecordedAt.At = testTrustedAt.At.Add(-time.Second); r.RecordedAt.Health = "" }, "response_time_evidence_missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rq, resp := req, response
			if tc.mutateReq != nil {
				tc.mutateReq(&rq)
			}
			if tc.mutateResponse != nil {
				tc.mutateResponse(&resp)
			}
			got, err := ValidateRequired(rq, resp, testTrustedAt)
			if err != nil || got.Allowed || got.Reason != tc.want {
				t.Fatalf("decision=%+v err=%v, want refusal %q", got, err, tc.want)
			}
		})
	}

	if _, err := ValidateRequired(req, response, TrustedTime{}); !errors.Is(err, ErrUntrustedTime) {
		t.Fatalf("invalid trusted time err=%v", err)
	}
	for _, mutate := range []func(*ExecutionRequirement){
		func(r *ExecutionRequirement) { r.Tenant = "" }, func(r *ExecutionRequirement) { r.ObligationID = "" }, func(r *ExecutionRequirement) { r.StatementID = "" },
		func(r *ExecutionRequirement) { r.StatementVersion = 0 }, func(r *ExecutionRequirement) { r.StatementDigest = "" }, func(r *ExecutionRequirement) { r.BindingDigest = "" },
		func(r *ExecutionRequirement) { r.ResponseID = "" }, func(r *ExecutionRequirement) { r.ResponseRevision = 0 },
	} {
		rq := req
		mutate(&rq)
		if _, err := ValidateRequired(rq, response, testTrustedAt); err == nil || !errors.Is(err, ErrRequiredAttestation) {
			t.Fatalf("incomplete requirement err=%v", err)
		}
	}

	_ = store
}

func TestRevalidateAndEnforceRequired_StoreClockAndAliasErrors(t *testing.T) {
	store, response, req := executionFixture(t)
	clock := TrustedClockFunc(func() (TrustedTime, error) { return testTrustedAt, nil })
	for name, fn := range map[string]func() (ExecutionDecision, error){
		"revalidate": func() (ExecutionDecision, error) {
			return RevalidateAtExecution(context.Background(), store, clock, req)
		},
		"enforce": func() (ExecutionDecision, error) { return EnforceRequired(context.Background(), store, clock, req) },
	} {
		decision, err := fn()
		if err != nil || !decision.Allowed || decision.ResponseDigest != response.Digest {
			t.Errorf("%s decision=%+v err=%v", name, decision, err)
		}
	}
	if _, err := RevalidateAtExecution(context.Background(), nil, clock, req); err == nil || !errors.Is(err, ErrRequiredAttestation) {
		t.Fatalf("nil store err=%v", err)
	}
	if _, err := RevalidateAtExecution(context.Background(), store, nil, req); err == nil || !errors.Is(err, ErrRequiredAttestation) {
		t.Fatalf("nil clock err=%v", err)
	}
	if _, err := RevalidateAtExecution(context.Background(), store, clock, ExecutionRequirement{Tenant: req.Tenant, ResponseID: "missing", ResponseRevision: 1}); err == nil || !errors.Is(err, ErrRequiredAttestation) {
		t.Fatalf("lookup err=%v", err)
	}
	failingClock := TrustedClockFunc(func() (TrustedTime, error) { return TrustedTime{}, errors.New("clock unavailable") })
	if _, err := RevalidateAtExecution(context.Background(), store, failingClock, req); err == nil || !errors.Is(err, ErrRequiredAttestation) {
		t.Fatalf("clock failure err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RevalidateAtExecution(ctx, store, clock, req); err == nil || !errors.Is(err, ErrRequiredAttestation) {
		t.Fatalf("store context failure err=%v", err)
	}
	if _, err := RevalidateAtExecution(context.Background(), store, clock, req); err != nil {
		t.Fatalf("repeat valid revalidation: %v", err)
	}
}

func TestFixedTrustedTimeAndClockAdapter(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 123, time.FixedZone("x", 3600))
	got := FixedTrustedTime(at)
	if got.Source != "conformance" || got.EvidenceID == "" || got.Health != "TRUSTED" || !got.At.Equal(at.UTC()) {
		t.Fatalf("FixedTrustedTime=%+v", got)
	}
	called := false
	clock := TrustedClockFunc(func() (TrustedTime, error) { called = true; return got, nil })
	if _, err := clock.TrustedNow(); err != nil || !called {
		t.Fatalf("TrustedClockFunc err=%v called=%v", err, called)
	}
}

func TestTodo_REV_041_02(t *testing.T) {
	store, response, req := executionFixture(t)
	called := false
	decision, err := EnforceBeforeEffect(context.Background(), store, TrustedClockFunc(func() (TrustedTime, error) {
		return testTrustedAt, nil
	}), req, func(_ context.Context, got ExecutionDecision) error {
		called = true
		if !got.Allowed || got.ResponseDigest != response.Digest || got.ObligationID != req.ObligationID {
			t.Fatalf("effect received unbound decision: %+v", got)
		}
		return nil
	})
	if err != nil || !decision.Allowed || !called {
		t.Fatalf("decision=%+v called=%v err=%v", decision, called, err)
	}

	refused := response
	refused.Status = ResponseRefused
	refused.Reason = "worker declined"
	refused.RequestDigest = requestDigest(responseRequest(refused))
	refused.Digest = responseDigest(refused)
	refusedStore := &executionResponseStore{response: refused}
	called = false
	decision, err = EnforceBeforeEffect(context.Background(), refusedStore, TrustedClockFunc(func() (TrustedTime, error) {
		return testTrustedAt, nil
	}), req, func(context.Context, ExecutionDecision) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrRequiredAttestation) || decision.Allowed || called {
		t.Fatalf("refused response decision=%+v called=%v err=%v", decision, called, err)
	}
}

func TestTodo_REV_041_02_Security(t *testing.T) {
	store, response, req := executionFixture(t)
	// A content edit with an unchanged receipt digest must fail closed before
	// the dependent callback.
	response.EvidenceReceipt = "forged-evidence"
	forged := &executionResponseStore{response: response}
	called := false
	decision, err := EnforceBeforeEffect(context.Background(), forged, TrustedClockFunc(func() (TrustedTime, error) {
		return testTrustedAt, nil
	}), req, func(context.Context, ExecutionDecision) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrRequiredAttestation) || decision.Allowed || decision.Reason != "response_integrity_mismatch" || called {
		t.Fatalf("forged receipt decision=%+v called=%v err=%v", decision, called, err)
	}

	called = false
	_, err = EnforceBeforeEffect(context.Background(), store, TrustedClockFunc(func() (TrustedTime, error) {
		return TrustedTime{}, errors.New("untrusted clock")
	}), req, func(context.Context, ExecutionDecision) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrRequiredAttestation) || called {
		t.Fatalf("clock failure called effect=%v err=%v", called, err)
	}
}

type executionResponseStore struct{ response Response }

func (s *executionResponseStore) GetResponse(_ context.Context, _ values.TenantId, _ string, _ uint64) (Response, error) {
	return s.response, nil
}

func (*executionResponseStore) AppendResponse(context.Context, Response) (Response, error) {
	return Response{}, errors.New("not implemented")
}
func (*executionResponseStore) GetResponseByIdempotency(context.Context, values.TenantId, string) (Response, error) {
	return Response{}, ErrResponseNotFound
}
func (*executionResponseStore) ListResponseHistory(context.Context, values.TenantId, string) ([]Response, error) {
	return nil, ErrResponseNotFound
}
