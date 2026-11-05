// WF-CAP-017 RED: the requisition lifecycle has no posting, screening
// or verification capability.
package recruiting

import (
	"errors"
	"testing"
	"time"
)

var cap017Now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func cap017Pack() RulePack {
	return RulePack{
		Version:                "recruiting.adverse-action/2026.1",
		VendorTurnaroundDays:   2,
		PostingDays:            30,
		PreAdverseResponseDays: 5,
		FinalAdverseWaitDays:   5,
		TNCContestDays:         8,
	}
}

// fakeConnector017 is the vendor on the far side of the connector binding:
// it records every delegated call and answers from a script.
type fakeConnector017 struct {
	calls []DelegatedCall
	next  map[string]VendorResponse
	err   error
}

func (f *fakeConnector017) Invoke(call DelegatedCall) (VendorResponse, error) {
	if f.err != nil {
		return VendorResponse{}, f.err
	}
	f.calls = append(f.calls, call)
	if r, ok := f.next[call.Operation]; ok {
		return r, nil
	}
	return VendorResponse{ReceiptID: "rcpt-" + call.Operation, Result: "ACK"}, nil
}

// TestTodo_WF_CAP_017 is the PRIMARY test: posting, screening with the
// pre-adverse and final adverse action sequence, and eligibility
// verification with its tentative-nonconfirmation branch are DELEGATED
// contracts over connector bindings with rule-pack deadlines.
func TestTodo_WF_CAP_017(t *testing.T) {
	bind := func(t *testing.T, connector, capability string) VendorBinding {
		t.Helper()
		b, err := BindVendor(connector, capability, cap017Pack())
		if err != nil {
			t.Fatalf("BindVendor(%s/%s): %v", connector, capability, err)
		}
		return b
	}

	t.Run("bindings pin connector capability and rule pack", func(t *testing.T) {
		b := bind(t, "jobboard-acme", CapabilityPosting)
		if b.Connector != "jobboard-acme" || b.RulePack.Version == "" {
			t.Fatalf("binding: %+v", b)
		}
		if _, err := BindVendor(" padded ", CapabilityPosting, cap017Pack()); !errors.Is(err, ErrVendorInvalid) {
			t.Fatalf("padded connector err = %v, want ErrVendorInvalid", err)
		}
		if _, err := BindVendor("", CapabilityPosting, cap017Pack()); !errors.Is(err, ErrVendorInvalid) {
			t.Fatalf("blank connector err = %v, want ErrVendorInvalid", err)
		}
		if _, err := BindVendor("x", "mind-reading", cap017Pack()); !errors.Is(err, ErrVendorInvalid) {
			t.Fatalf("unknown capability err = %v, want ErrVendorInvalid", err)
		}
		pack := cap017Pack()
		pack.PreAdverseResponseDays = 0
		if _, err := BindVendor("x", CapabilityScreening, pack); !errors.Is(err, ErrVendorInvalid) {
			t.Fatalf("zero deadline err = %v, want ErrVendorInvalid", err)
		}
	})

	t.Run("posting delegates with a rule-pack deadline", func(t *testing.T) {
		port := &fakeConnector017{}
		posting, err := PostJob(bind(t, "jobboard-acme", CapabilityPosting),
			PostingRequest{RequisitionID: "req-7", Title: "Senior HRBP"}, port, cap017Now)
		if err != nil {
			t.Fatalf("PostJob: %v", err)
		}
		if posting.ExpiresAt != cap017Now.AddDate(0, 0, 30) {
			t.Fatalf("expires = %v, want +30d", posting.ExpiresAt)
		}
		if len(port.calls) != 1 || port.calls[0].Connector != "jobboard-acme" || port.calls[0].Operation != "post" {
			t.Fatalf("calls = %+v", port.calls)
		}
		if port.calls[0].Deadline != posting.ExpiresAt {
			t.Fatal("delegated call carries no rule-pack deadline")
		}
	})

	t.Run("screening runs the adverse-action sequence in order", func(t *testing.T) {
		port := &fakeConnector017{}
		b := bind(t, "screening-vendor", CapabilityScreening)
		s, err := StartScreening(b, "cand-1", port, cap017Now)
		if err != nil {
			t.Fatalf("StartScreening: %v", err)
		}
		if _, err := BeginPreAdverse(s, cap017Now); !errors.Is(err, ErrVendorOrder) {
			t.Fatalf("pre-adverse before result err = %v, want ErrVendorOrder", err)
		}
		s, err = RecordScreeningResult(s, ScreeningConsider, "rep-1")
		if err != nil {
			t.Fatalf("RecordScreeningResult: %v", err)
		}
		s, err = BeginPreAdverse(s, cap017Now)
		if err != nil {
			t.Fatalf("BeginPreAdverse: %v", err)
		}
		if s.RespondBy != cap017Now.AddDate(0, 0, 5) {
			t.Fatalf("respond-by = %v, want +5d", s.RespondBy)
		}
		if _, err := BeginFinalAdverse(s, cap017Now.AddDate(0, 0, 6)); !errors.Is(err, ErrVendorOrder) {
			t.Fatalf("final before response err = %v, want ErrVendorOrder", err)
		}
		if _, err := RecordPreAdverseResponse(s, "  ", cap017Now.AddDate(0, 0, 2)); !errors.Is(err, ErrVendorInvalid) {
			t.Fatalf("empty response err = %v, want ErrVendorInvalid", err)
		}
		s, err = RecordPreAdverseResponse(s, "dispute-filed", cap017Now.AddDate(0, 0, 2))
		if err != nil {
			t.Fatalf("RecordPreAdverseResponse: %v", err)
		}
		s, err = BeginFinalAdverse(s, cap017Now.AddDate(0, 0, 6))
		if err != nil {
			t.Fatalf("BeginFinalAdverse: %v", err)
		}
		if s.State != ScreeningFinalAdverse {
			t.Fatalf("state = %s, want FINAL_ADVERSE", s.State)
		}
		s, err = RecordFinalAdverse(s, cap017Now.AddDate(0, 0, 7))
		if err != nil {
			t.Fatalf("RecordFinalAdverse: %v", err)
		}
		if s.State != ScreeningClosedAdverse {
			t.Fatalf("state = %s, want CLOSED_ADVERSE", s.State)
		}
	})

	t.Run("eligibility runs the tentative-nonconfirmation branch", func(t *testing.T) {
		port := &fakeConnector017{}
		b := bind(t, "e-verify", CapabilityEligibility)
		v, err := StartVerification(b, "cand-1", port, cap017Now)
		if err != nil {
			t.Fatalf("StartVerification: %v", err)
		}
		v, err = RecordVerificationResult(v, VerifyTNC, cap017Now)
		if err != nil {
			t.Fatalf("RecordVerificationResult: %v", err)
		}
		if v.ContestBy != cap017Now.AddDate(0, 0, 8) {
			t.Fatalf("contest-by = %v, want +8d", v.ContestBy)
		}
		if _, err := ContestTNC(v, cap017Now.AddDate(0, 0, 9)); !errors.Is(err, ErrVendorDeadline) {
			t.Fatalf("late contest err = %v, want ErrVendorDeadline", err)
		}
		v, err = ContestTNC(v, cap017Now.AddDate(0, 0, 3))
		if err != nil {
			t.Fatalf("ContestTNC: %v", err)
		}
		v, err = ResolveTNC(v, VerifyFinalTNC, cap017Now.AddDate(0, 0, 4))
		if err != nil {
			t.Fatalf("ResolveTNC: %v", err)
		}
		if v.State != VerifyFinalTNC || !v.HireBlocked {
			t.Fatalf("tnc resolution = %+v, want blocked hire", v)
		}
	})
}

// TestTodo_WF_CAP_017_Integration runs a requisition across all three
// vendors: post, screen to final adverse, verify through a contested TNC
// to authorized.
func TestTodo_WF_CAP_017_Integration(t *testing.T) {
	pack := cap017Pack()
	port := &fakeConnector017{}
	post, err := BindVendor("jobboard-acme", CapabilityPosting, pack)
	if err != nil {
		t.Fatal(err)
	}
	screen, err := BindVendor("screening-vendor", CapabilityScreening, pack)
	if err != nil {
		t.Fatal(err)
	}
	elig, err := BindVendor("e-verify", CapabilityEligibility, pack)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PostJob(post, PostingRequest{RequisitionID: "req-9", Title: "HRBP"}, port, cap017Now); err != nil {
		t.Fatalf("PostJob: %v", err)
	}
	s, _ := StartScreening(screen, "cand-9", port, cap017Now)
	s, _ = RecordScreeningResult(s, ScreeningConsider, "rep-9")
	s, _ = BeginPreAdverse(s, cap017Now)
	if _, err := RecordPreAdverseResponse(s, "no-response", cap017Now.AddDate(0, 0, 6)); !errors.Is(err, ErrVendorDeadline) {
		t.Fatalf("late response err = %v, want ErrVendorDeadline", err)
	}
	s, _ = ExpirePreAdverse(s, cap017Now.AddDate(0, 0, 6))
	s, err = BeginFinalAdverse(s, cap017Now.AddDate(0, 0, 11))
	if err != nil {
		t.Fatalf("BeginFinalAdverse after expiry: %v", err)
	}
	_ = s
	v, _ := StartVerification(elig, "cand-9", port, cap017Now)
	v, _ = RecordVerificationResult(v, VerifyTNC, cap017Now)
	v, err = ContestTNC(v, cap017Now.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("ContestTNC: %v", err)
	}
	v, err = ResolveTNC(v, VerifyAuthorized, cap017Now.AddDate(0, 0, 2))
	if err != nil {
		t.Fatalf("ResolveTNC: %v", err)
	}
	if v.State != VerifyAuthorized || v.HireBlocked {
		t.Fatalf("verification = %+v, want authorized hire", v)
	}
	// Every step delegated to its bound connector, in order.
	wantConnectors := []string{"jobboard-acme", "screening-vendor", "e-verify"}
	if len(port.calls) != 3 {
		t.Fatalf("calls = %+v, want 3 delegated calls", port.calls)
	}
	for i, want := range wantConnectors {
		if port.calls[i].Connector != want {
			t.Fatalf("call %d connector = %s, want %s", i, port.calls[i].Connector, want)
		}
		if port.calls[i].Deadline.IsZero() {
			t.Fatalf("call %d carries no deadline", i)
		}
	}
}

// TestTodo_WF_CAP_017_Security proves screening reports stay in a
// restricted compartment and delegated payloads carry refs, not PII.
func TestTodo_WF_CAP_017_Security(t *testing.T) {
	report := ScreeningReport{
		ID: "rep-1", CandidateRef: "cand-1", Status: ScreeningConsider,
		Findings: "county record 2019-CR-88", Sources: []string{"county-court-feed"},
	}

	t.Run("only the adjudicator sees findings", func(t *testing.T) {
		full, err := ViewScreeningReport(report, ReportViewerAdjudicator)
		if err != nil {
			t.Fatalf("adjudicator view: %v", err)
		}
		if full.Findings == "" || len(full.Sources) != 1 {
			t.Fatal("adjudicator view redacts the record")
		}
		for _, role := range []ReportViewerRole{ReportViewerRecruiter, ReportViewerCandidate} {
			view, err := ViewScreeningReport(report, role)
			if err != nil {
				t.Fatalf("%s view: %v", role, err)
			}
			if view.Findings != "" || len(view.Sources) != 0 {
				t.Fatalf("%s view leaks the compartment", role)
			}
			if view.Status != ScreeningConsider {
				t.Fatalf("%s view loses the status", role)
			}
		}
		if _, err := ViewScreeningReport(report, "stranger"); !errors.Is(err, ErrVendorInvalid) {
			t.Fatalf("unknown role err = %v, want ErrVendorInvalid", err)
		}
	})

	t.Run("delegated payloads carry refs under an allow-list", func(t *testing.T) {
		port := &fakeConnector017{}
		post, _ := BindVendor("jobboard-acme", CapabilityPosting, cap017Pack())
		if _, err := PostJob(post, PostingRequest{RequisitionID: "req-1", Title: "HRBP"}, port, cap017Now); err != nil {
			t.Fatal(err)
		}
		screen, _ := BindVendor("screening-vendor", CapabilityScreening, cap017Pack())
		if _, err := StartScreening(screen, "cand-1", port, cap017Now); err != nil {
			t.Fatal(err)
		}
		elig, _ := BindVendor("e-verify", CapabilityEligibility, cap017Pack())
		if _, err := StartVerification(elig, "cand-1", port, cap017Now); err != nil {
			t.Fatal(err)
		}
		allowed := map[string]map[string]bool{
			"post":   {"requisition_id": true, "title": true},
			"screen": {"candidate_ref": true, "package": true},
			"verify": {"candidate_ref": true},
		}
		for _, call := range port.calls {
			for k := range call.Payload {
				if !allowed[call.Operation][k] {
					t.Fatalf("%s payload carries %q outside the allow-list", call.Operation, k)
				}
			}
		}
	})
}

// TestTodo_WF_CAP_017_TNCExpiry proves the contest window anchors to TNC
// issuance rather than the order, and that an uncontested TNC expires to
// a hire-blocking final nonconfirmation.
func TestTodo_WF_CAP_017_TNCExpiry(t *testing.T) {
	b, err := BindVendor("e-verify", CapabilityEligibility, cap017Pack())
	if err != nil {
		t.Fatal(err)
	}
	port := &fakeConnector017{}
	v, err := StartVerification(b, "cand-x", port, cap017Now)
	if err != nil {
		t.Fatal(err)
	}
	// The vendor issues the TNC five days after the order: vendor
	// processing time must not eat the candidate window.
	v, err = RecordVerificationResult(v, VerifyTNC, cap017Now.AddDate(0, 0, 5))
	if err != nil {
		t.Fatal(err)
	}
	if want := cap017Now.AddDate(0, 0, 13); v.ContestBy != want {
		t.Fatalf("contest-by = %v, want issuance-anchored %v", v.ContestBy, want)
	}
	// Day 9 would be late under the old order-anchored window; it is
	// timely under the issuance-anchored one.
	if _, err := ContestTNC(v, cap017Now.AddDate(0, 0, 9)); err != nil {
		t.Fatalf("timely contest: %v", err)
	}

	w, err := StartVerification(b, "cand-y", port, cap017Now)
	if err != nil {
		t.Fatal(err)
	}
	w, err = RecordVerificationResult(w, VerifyTNC, cap017Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExpireTNC(w, cap017Now.AddDate(0, 0, 3)); !errors.Is(err, ErrVendorOrder) {
		t.Fatalf("early expiry err = %v, want ErrVendorOrder", err)
	}
	w, err = ExpireTNC(w, cap017Now.AddDate(0, 0, 9))
	if err != nil {
		t.Fatalf("ExpireTNC: %v", err)
	}
	if w.State != VerifyFinalTNC || !w.HireBlocked {
		t.Fatalf("expired TNC = %+v, want blocked hire", w)
	}
	if _, err := ContestTNC(w, cap017Now.AddDate(0, 0, 9)); !errors.Is(err, ErrVendorOrder) {
		t.Fatalf("contest after expiry err = %v, want ErrVendorOrder", err)
	}
}
