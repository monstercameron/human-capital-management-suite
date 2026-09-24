package attest

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

var testTrustedAt = FixedTrustedTime(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))

func testResponseRequest(status ResponseStatus) ResponseRequest {
	return ResponseRequest{
		Tenant: "tenant-a", ResponseID: "response-1", StatementID: "statement-1", StatementVersion: 3,
		StatementDigest: "sha256:statement", BindingDigest: "sha256:binding", Status: status,
		Reason: func() string {
			if status == ResponseAccepted {
				return ""
			}
			return "respondent declined"
		}(),
		EvidenceReceipt: "sha256:evidence", IdempotencyKey: "idem-1", TransactionID: "tx-1",
	}
}

func testRecorder(t *testing.T) (*Recorder, *MemoryResponseStore, *int) {
	t.Helper()
	store := NewMemoryResponseStore()
	calls := 0
	var callsMu sync.Mutex
	clock := TrustedClockFunc(func() (TrustedTime, error) {
		callsMu.Lock()
		defer callsMu.Unlock()
		calls++
		return testTrustedAt, nil
	})
	recorder, err := NewRecorder(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	return recorder, store, &calls
}

func TestTodo_ATTEST_004(t *testing.T) {
	recorder, store, calls := testRecorder(t)
	first, err := recorder.RecordResponse(context.Background(), testResponseRequest(ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || first.RecordedAt != testTrustedAt || first.Digest == "" {
		t.Fatalf("first receipt = %+v", first)
	}
	replay, err := recorder.RecordResponse(context.Background(), testResponseRequest(ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Replayed || replay.Digest != first.Digest || *calls != 1 {
		t.Fatalf("replay=%+v clock calls=%d", replay, *calls)
	}
	if _, err := store.GetResponse(context.Background(), "tenant-a", "response-1", 2); !errors.Is(err, ErrResponseNotFound) {
		t.Fatalf("unexpected second revision: %v", err)
	}
}

func TestTodo_ATTEST_004_Race(t *testing.T) {
	recorder, _, _ := testRecorder(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var accepted, refused int
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := testResponseRequest(ResponseAccepted)
			req.ResponseID = "response-" + string(rune('a'+i))
			req.IdempotencyKey = "idem-" + string(rune('a'+i))
			if _, err := recorder.RecordResponse(context.Background(), req); err == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			} else {
				mu.Lock()
				refused++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if accepted != 20 || refused != 0 {
		t.Fatalf("accepted=%d refused=%d", accepted, refused)
	}
}

func TestTodo_ATTEST_004_Recovery(t *testing.T) {
	recorder, store, _ := testRecorder(t)
	want, err := recorder.RecordResponse(context.Background(), testResponseRequest(ResponseUnknown))
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetResponse(context.Background(), "tenant-a", want.ResponseID, want.Revision)
	if err != nil || got.Digest != want.Digest || got.Status != ResponseUnknown {
		t.Fatalf("recovered=%+v err=%v", got, err)
	}
}

func TestTodo_ATTEST_004_Mutation(t *testing.T) {
	recorder, _, _ := testRecorder(t)
	req := testResponseRequest(ResponseAccepted)
	if _, err := recorder.RecordResponse(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	req.StatementDigest = "sha256:tampered"
	if _, err := recorder.RecordResponse(context.Background(), req); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("mutation error=%v", err)
	}
	badClock := TrustedClockFunc(func() (TrustedTime, error) { return TrustedTime{}, nil })
	bad, err := NewRecorder(NewMemoryResponseStore(), badClock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.RecordResponse(context.Background(), testResponseRequest(ResponseAccepted)); !errors.Is(err, ErrUntrustedTime) {
		t.Fatalf("untrusted clock error=%v", err)
	}
}

func FuzzTodo_ATTEST_004(f *testing.F) {
	f.Add("ACCEPTED")
	f.Add("REFUSED")
	f.Add("UNKNOWN")
	f.Fuzz(func(t *testing.T, status string) {
		if status != string(ResponseAccepted) && status != string(ResponseRefused) && status != string(ResponseUnknown) {
			return
		}
		recorder, _, _ := testRecorder(t)
		req := testResponseRequest(ResponseStatus(status))
		_, _ = recorder.RecordResponse(context.Background(), req)
	})
}

func TestTodo_ATTEST_005(t *testing.T) {
	recorder, store, _ := testRecorder(t)
	refused, err := recorder.RecordResponse(context.Background(), testResponseRequest(ResponseRefused))
	if err != nil {
		t.Fatal(err)
	}
	unknownReq := testResponseRequest(ResponseUnknown)
	unknownReq.ResponseID, unknownReq.IdempotencyKey = "response-unknown", "idem-unknown"
	unknown, err := recorder.RecordResponse(context.Background(), unknownReq)
	if err != nil {
		t.Fatal(err)
	}
	correctionReq := testResponseRequest(ResponseAccepted)
	correctionReq.ResponseID, correctionReq.IdempotencyKey = "response-correction", "idem-correction"
	correctionReq.Kind, correctionReq.CorrectsResponseID, correctionReq.Authority = AssertionCorrection, refused.ResponseID, "authority:hr"
	correctionReq.Reason = "corrected respondent record"
	correctionReq.AffectedObligations = []string{"obligation-1"}
	correction, err := recorder.RecordResponse(context.Background(), correctionReq)
	if err != nil {
		t.Fatal(err)
	}
	revocationReq := testResponseRequest(ResponseRefused)
	revocationReq.ResponseID, revocationReq.IdempotencyKey = "response-revocation", "idem-revocation"
	revocationReq.Kind, revocationReq.CorrectsResponseID, revocationReq.Authority = AssertionRevocation, correction.ResponseID, "authority:legal"
	revocation, err := recorder.RecordResponse(context.Background(), revocationReq)
	if err != nil {
		t.Fatal(err)
	}
	if refused.Status == ResponseAccepted || unknown.Status == ResponseAccepted || correction.CorrectsResponseID != refused.ResponseID || revocation.Kind != AssertionRevocation {
		t.Fatal("response history was coerced or lineage was lost")
	}
	if _, err := store.ListResponseHistory(context.Background(), "tenant-a", "response-correction"); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_ATTEST_005_Mutation(t *testing.T) {
	recorder, _, _ := testRecorder(t)
	req := testResponseRequest(ResponseRefused)
	if _, err := recorder.RecordResponse(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	req.IdempotencyKey, req.Status = "idem-new", ResponseAccepted
	if _, err := recorder.RecordResponse(context.Background(), req); err == nil || !strings.Contains(err.Error(), "accepted response") {
		t.Fatalf("coercion error=%v", err)
	}
	correction := testResponseRequest(ResponseAccepted)
	correction.ResponseID, correction.IdempotencyKey, correction.Kind = "response-c", "idem-c", AssertionCorrection
	if _, err := recorder.RecordResponse(context.Background(), correction); err == nil {
		t.Fatal("correction without authority was accepted")
	}
}

func TestTodo_ATTEST_006(t *testing.T) {
	recorder, _, _ := testRecorder(t)
	accepted := testResponseRequest(ResponseAccepted)
	accepted.AffectedObligations = []string{"obligation-1"}
	response, err := recorder.RecordResponse(context.Background(), accepted)
	if err != nil {
		t.Fatal(err)
	}
	req := ExecutionRequirement{Tenant: "tenant-a", ObligationID: "obligation-1", StatementID: response.StatementID, StatementVersion: response.StatementVersion, StatementDigest: response.StatementDigest, BindingDigest: response.BindingDigest, ResponseID: response.ResponseID, ResponseRevision: response.Revision, TransactionID: "tx-1"}
	decision, err := ValidateRequired(req, response, testTrustedAt)
	if err != nil || !decision.Allowed {
		t.Fatalf("valid execution decision=%+v err=%v", decision, err)
	}
	response.Status = ResponseRefused
	decision, err = ValidateRequired(req, response, testTrustedAt)
	if err != nil || decision.Allowed || decision.Reason != "response_not_accepted" {
		t.Fatalf("refused execution decision=%+v err=%v", decision, err)
	}
}

func TestTodo_ATTEST_006_Mutation(t *testing.T) {
	recorder, _, _ := testRecorder(t)
	response, err := recorder.RecordResponse(context.Background(), testResponseRequest(ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	req := ExecutionRequirement{Tenant: "tenant-a", ObligationID: "obligation-1", StatementID: response.StatementID, StatementVersion: response.StatementVersion, StatementDigest: "sha256:wrong", BindingDigest: response.BindingDigest, ResponseID: response.ResponseID, ResponseRevision: 1}
	decision, err := ValidateRequired(req, response, testTrustedAt)
	if err != nil || decision.Allowed || decision.Reason != "statement_or_binding_mismatch" {
		t.Fatalf("stale execution decision=%+v err=%v", decision, err)
	}
}

func TestTodo_ATTEST_007(t *testing.T) {
	recorder, store, _ := testRecorder(t)
	response, err := recorder.RecordResponse(context.Background(), testResponseRequest(ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Export(ExportRequest{Authorization: EvidenceAuthorization{Tenant: "tenant-a", Purpose: "audit", Recipient: "auditor", RedactionProfile: "minimum-necessary/v1", DecisionDigest: "sha256:authz", Allowed: true}, StatementID: response.StatementID, StatementVersion: response.StatementVersion, StatementDigest: response.StatementDigest, BindingDigest: response.BindingDigest, Response: response})
	if err != nil {
		t.Fatal(err)
	}
	verification := pkg.Verify()
	if !verification.Valid || verification.Status != "COMPLETE" {
		t.Fatalf("verification=%+v", verification)
	}
	pkg.ResponseDigest = "sha256:tampered"
	verification = VerifyEvidencePackage(pkg)
	if verification.Valid || verification.Status != "ATTEST_007_REJECTED" || verification.Invalid != "digest" {
		t.Fatalf("tampered verification=%+v", verification)
	}
	if _, err := store.ListResponseHistory(context.Background(), "tenant-a", response.ResponseID); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_ATTEST_007_Security(t *testing.T) {
	recorder, _, _ := testRecorder(t)
	response, err := recorder.RecordResponse(context.Background(), testResponseRequest(ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	_, err = Export(ExportRequest{Authorization: EvidenceAuthorization{Tenant: "tenant-a", Purpose: "audit", Recipient: "auditor", RedactionProfile: "minimum-necessary/v1", DecisionDigest: "sha256:authz"}, StatementID: response.StatementID, StatementVersion: response.StatementVersion, StatementDigest: response.StatementDigest, BindingDigest: response.BindingDigest, Response: response})
	if !errors.Is(err, ErrEvidencePackage) {
		t.Fatalf("unauthorized export=%v", err)
	}
}

func TestTodo_ATTEST_007_Mutation(t *testing.T) {
	recorder, _, _ := testRecorder(t)
	response, err := recorder.RecordResponse(context.Background(), testResponseRequest(ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := Export(ExportRequest{Authorization: EvidenceAuthorization{Tenant: "tenant-a", Purpose: "audit", Recipient: "auditor", RedactionProfile: "minimum-necessary/v1", DecisionDigest: "sha256:authz", Allowed: true}, StatementID: response.StatementID, StatementVersion: response.StatementVersion, StatementDigest: response.StatementDigest, BindingDigest: response.BindingDigest, Response: response})
	if err != nil {
		t.Fatal(err)
	}
	pkg.TimeEvidenceID = ""
	if got := pkg.Verify(); got.Valid || got.Invalid != "time" {
		t.Fatalf("missing time evidence=%+v", got)
	}
}

func TestTodo_ATTEST_008(t *testing.T) {
	recorder, _, _ := testRecorder(t)
	domains := []struct {
		domain AcknowledgementDomain
		claim  AcknowledgementClaim
	}{{DomainTime, ClaimTimecardAccuracy}, {DomainPayroll, ClaimPayrollInputCompleteness}, {DomainLegal, ClaimLegalFact}}
	var cases []ConformanceCase
	for i, item := range domains {
		req := testResponseRequest(ResponseAccepted)
		req.ResponseID, req.IdempotencyKey = "response-"+string(rune('a'+i)), "idem-"+string(rune('a'+i))
		response, err := recorder.RecordResponse(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, ConformanceCase{Domain: item.domain, Claim: item.claim, Response: response})
	}
	report, err := ProveConformance(cases)
	if err != nil || !report.Valid || !report.SharedEvidence || !report.DistinctFromApproval || !report.DistinctFromSignature || !report.DistinctFromForm {
		t.Fatalf("conformance=%+v err=%v", report, err)
	}
}

func TestTodo_ATTEST_008_Security(t *testing.T) {
	recorder, _, _ := testRecorder(t)
	response, err := recorder.RecordResponse(context.Background(), testResponseRequest(ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ProveConformance([]ConformanceCase{{Domain: DomainTime, Claim: ClaimTimecardAccuracy, Response: response}})
	if !errors.Is(err, ErrConformance) {
		t.Fatalf("incomplete conformance=%v", err)
	}
}

func TestTodo_ATTEST_008_Conformance(t *testing.T) {
	recorder, _, _ := testRecorder(t)
	response, err := recorder.RecordResponse(context.Background(), testResponseRequest(ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	cases := []ConformanceCase{
		{Domain: DomainTime, Claim: ClaimTimecardAccuracy, Response: response},
		{Domain: DomainPayroll, Claim: ClaimPayrollInputCompleteness, Response: response},
		{Domain: DomainLegal, Claim: ClaimLegalFact, Response: response},
	}
	report, err := ProveConformance(cases)
	if err != nil || !report.Valid || report.Cases != len(cases) || report.Domains[0] != DomainTime || report.Domains[1] != DomainPayroll || report.Domains[2] != DomainLegal {
		t.Fatalf("shared acknowledgement did not retain three domain claims: report=%+v err=%v", report, err)
	}
	if !report.SharedEvidence || !report.DistinctFromApproval || !report.DistinctFromSignature || !report.DistinctFromForm {
		t.Fatalf("conformance blurred acknowledgement with another control: %+v", report)
	}
}
func TestTodo_ATTEST_008_Mutation(t *testing.T) {
	recorder, _, _ := testRecorder(t)
	response, err := recorder.RecordResponse(context.Background(), testResponseRequest(ResponseAccepted))
	if err != nil {
		t.Fatal(err)
	}
	c := ConformanceCase{Domain: DomainTime, Claim: ClaimTimecardAccuracy, Response: response}
	if _, err := ProveConformance([]ConformanceCase{c, c}); !errors.Is(err, ErrConformance) {
		t.Fatalf("duplicate domain=%v", err)
	}
}
