package qualification

import (
	"strings"
	"testing"
)

func mustOverallStatus(t *testing.T, evaluation Evaluation) OverallStatus {
	t.Helper()
	status, err := Summarize(evaluation)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	return status
}

// TestTodo_QUAL_004_Property: every result combination maps to exactly
// one documented overall.
func TestTodo_QUAL_004_Property(t *testing.T) {
	satisfied := RequirementResult{Kind: RequirementCredential, Ref: "c", RequiredLevel: 1, Status: StatusSatisfied, EvidenceRef: "e"}
	expiring := RequirementResult{Kind: RequirementCredential, Ref: "c", RequiredLevel: 1, Status: StatusExpiring, Gap: "renews in 9 days", EvidenceRef: "e"}
	unsatisfied := RequirementResult{Kind: RequirementCredential, Ref: "c", RequiredLevel: 1, Status: StatusUnsatisfied, Gap: "evidence expired"}
	restricted := RequirementResult{Kind: RequirementCredential, Ref: "c", RequiredLevel: 1, Status: StatusSatisfied, EvidenceRef: "e", Restricted: true}
	cases := []struct {
		name    string
		results []RequirementResult
		want    Overall
	}{
		{"all satisfied", []RequirementResult{satisfied}, OverallQualified},
		{"one expiring", []RequirementResult{satisfied, expiring}, OverallConditional},
		{"one restricted", []RequirementResult{satisfied, restricted}, OverallConditional},
		{"one missing", []RequirementResult{satisfied, unsatisfied}, OverallNotQualified},
		{"missing beats expiring", []RequirementResult{expiring, unsatisfied}, OverallNotQualified},
	}
	for _, tc := range cases {
		got := mustOverallStatus(t, Evaluation{RequirementID: "req", Revision: 1, Results: tc.results})
		if got.Overall != tc.want {
			t.Fatalf("%s: overall=%v", tc.name, got.Overall)
		}
	}
	// Restricted refs list without evidence content.
	got := mustOverallStatus(t, Evaluation{RequirementID: "req", Revision: 1, Results: []RequirementResult{restricted}})
	if len(got.Restricted) != 1 || got.Restricted[0] != "c" {
		t.Fatalf("restricted: %+v", got)
	}
	// The explanation carries counts only: no refs, no evidence content.
	if want := "status CONDITIONAL: satisfied=0 missing=0 expiring=0 restricted=1"; got.Explain() != want {
		t.Fatalf("explanation = %q, want %q", got.Explain(), want)
	}
}

// TestTodo_QUAL_004_Security: protected evidence content never leaks
// through the status buckets, the explanation or a rejection — refs,
// counts and field names only.
func TestTodo_QUAL_004_Security(t *testing.T) {
	const secret = "secret-evidence-blob-9f3"
	evaluation := Evaluation{RequirementID: "req", Revision: 1, Results: []RequirementResult{
		{Kind: RequirementCredential, Ref: "cred-1", RequiredLevel: 1, Status: StatusSatisfied, EvidenceRef: secret},
		{Kind: RequirementCredential, Ref: "cred-2", RequiredLevel: 1, Status: StatusExpiring, Gap: "renews with " + secret, EvidenceRef: secret},
		{Kind: RequirementCredential, Ref: "cred-3", RequiredLevel: 1, Status: StatusUnsatisfied, Gap: "missing " + secret},
	}}
	got := mustOverallStatus(t, evaluation)
	for _, bucket := range [][]string{got.Satisfied, got.Missing, got.Expiring, got.Restricted} {
		for _, ref := range bucket {
			if strings.Contains(ref, secret) {
				t.Fatalf("bucket leaks evidence content: %q", ref)
			}
		}
	}
	if strings.Contains(got.Explain(), secret) {
		t.Fatalf("explanation leaks evidence content: %q", got.Explain())
	}
	// Rejections name field/state/version only — never evidence content.
	_, err := Summarize(Evaluation{RequirementID: "req", Revision: 1, Results: []RequirementResult{
		{Kind: RequirementCredential, Ref: "cred-1", RequiredLevel: 1, Status: StatusSatisfied, Gap: secret},
	}})
	if err == nil {
		t.Fatal("gap-carrying satisfied result summarized")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("rejection leaks evidence content: %q", err.Error())
	}
}

// TestTodo_QUAL_004_Mutation: evaluation edges resolve on the
// documented side.
func TestTodo_QUAL_004_Mutation(t *testing.T) {
	// A satisfied result carrying a gap is malformed: reject, never judge.
	if _, err := Summarize(Evaluation{RequirementID: "req", Revision: 1, Results: []RequirementResult{
		{Kind: RequirementCredential, Ref: "c", RequiredLevel: 1, Status: StatusSatisfied, Gap: "stale gap"},
	}}); err == nil {
		t.Fatal("gap-carrying satisfied result summarized")
	}
	// An unsatisfied result without a gap is malformed.
	if _, err := Summarize(Evaluation{RequirementID: "req", Revision: 1, Results: []RequirementResult{
		{Kind: RequirementCredential, Ref: "c", RequiredLevel: 1, Status: StatusUnsatisfied},
	}}); err == nil {
		t.Fatal("gapless unsatisfied result summarized")
	}
	// Zero revision rejects.
	if _, err := Summarize(Evaluation{RequirementID: "req", Results: []RequirementResult{
		{Kind: RequirementSkill, Ref: "s", RequiredLevel: 1, Status: StatusSatisfied, EvidenceRef: "e"},
	}}); err == nil {
		t.Fatal("zero-revision evaluation summarized")
	}
}
