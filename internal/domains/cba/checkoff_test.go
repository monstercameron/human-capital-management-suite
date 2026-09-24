package cba

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payinput"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func rev05202At() time.Time { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) }
func rev05202Authorization(action CheckoffAction) CheckoffAuthorizationRevision {
	return CheckoffAuthorizationRevision{ID: "authrev-1", AuthorizationID: "auth-1", Revision: "1", WorkerID: "worker-1", MembershipID: "member-1", UnitID: "unit-1", Action: action, InstrumentRef: "signed-form-1", EvidenceRef: "evidence:form-1", EffectiveFrom: rev05202At().Add(-24 * time.Hour), KnownFrom: rev05202At().Add(-24 * time.Hour)}
}
func rev05202History(action CheckoffAction) CheckoffAuthorizationHistory {
	r := rev05202Authorization(action)
	digest, _ := r.Digest()
	return CheckoffAuthorizationHistory{TenantID: "tenant-1", AuthorizationID: r.AuthorizationID, CurrentRevision: r.Revision, HeadDigest: digest, HistoryRef: "cba-history:auth-1", EvidenceRef: "history-evidence:auth-1", Revisions: []CheckoffAuthorizationRevision{r}}
}
func rev05202RevokedHistory() CheckoffAuthorizationHistory {
	first := rev05202Authorization(CheckoffAuthorize)
	second := first
	second.ID, second.Revision, second.Action = "authrev-2", "2", CheckoffRevoke
	second.InstrumentRef, second.EvidenceRef = "signed-revocation-2", "evidence:revocation-2"
	second.EffectiveFrom, second.KnownFrom = rev05202At(), rev05202At()
	second.SupersedesDigest, _ = first.Digest()
	secondDigest, _ := second.Digest()
	return CheckoffAuthorizationHistory{TenantID: "tenant-1", AuthorizationID: first.AuthorizationID, CurrentRevision: "2", HeadDigest: secondDigest, HistoryRef: "cba-history:auth-1:r2", EvidenceRef: "history-evidence:auth-1:r2", Revisions: []CheckoffAuthorizationRevision{first, second}}
}
func rev05202Membership() MembershipRevision {
	return MembershipRevision{ID: "membership-rev-1", MembershipID: "member-1", Revision: "1", WorkerID: "worker-1", UnitID: "unit-1", Source: "cba-record:1", EffectiveFrom: rev05202At().Add(-time.Hour), KnownFrom: rev05202At().Add(-time.Hour)}
}
func rev05202Policy(state CheckoffState) CheckoffPolicyEvidence {
	return CheckoffPolicyEvidence{State: state, Jurisdiction: "jurisdiction:example", PolicyRef: "policy:rev-4", EvidenceRef: "policy-evidence:rev-4", WorkerID: "worker-1", MembershipID: "member-1", JurisdictionBindingRef: "worker-jurisdiction:evidence-1", TenantID: "tenant-1"}
}

func rev05202PayInputDefinition(t *testing.T) payinput.Definition {
	t.Helper()
	start, err := values.ParseLocalDate("2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.ParseLocalDate("2027-01-01")
	if err != nil {
		t.Fatal(err)
	}
	effective, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "payroll", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	minimum, err := values.NewDecimal("1.00", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	maximum, err := values.NewDecimal("75.00", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	d, err := payinput.NewDefinition(payinput.Definition{DefinitionID: "dues", Code: "DUES", Kind: payinput.KindDeduction, Currency: "USD", Taxability: map[payinput.JurisdictionClass]bool{payinput.JurisdictionFederal: false}, CalculationBasis: payinput.BasisFlatAmount, Limits: payinput.DecimalLimits{Minimum: minimum, Maximum: maximum}, AccountingCode: "DUES", OwnerRef: "cba", Effective: effective, Version: "v1", State: payinput.StatePublished})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTodo_REV_052_02(t *testing.T) {
	member := rev05202Membership()
	for _, tc := range []struct {
		name   string
		action CheckoffAction
		policy CheckoffPolicyEvidence
		want   CheckoffState
	}{
		{"authorized", CheckoffAuthorize, rev05202Policy(CheckoffAuthorized), CheckoffAuthorized},
		{"revoked", CheckoffRevoke, CheckoffPolicyEvidence{}, CheckoffRevoked},
		{"agency fee only", CheckoffAuthorize, rev05202Policy(CheckoffAgencyFeeOnly), CheckoffAgencyFeeOnly},
		{"prohibited", CheckoffAuthorize, rev05202Policy(CheckoffProhibited), CheckoffProhibited},
		{"unknown policy", CheckoffAuthorize, CheckoffPolicyEvidence{}, CheckoffUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			history := rev05202History(tc.action)
			if tc.action == CheckoffRevoke {
				history = rev05202RevokedHistory()
			}
			got, err := ResolveCheckoff(history, member, "tenant-1", tc.policy, rev05202At(), rev05202At())
			if err != nil {
				t.Fatal(err)
			}
			if got.State != tc.want {
				t.Fatalf("state=%s want=%s", got.State, tc.want)
			}
			if got.AuthorizationDigest == "" {
				t.Fatal("missing authorization digest")
			}
			if (tc.want == CheckoffUnknown || tc.want == CheckoffRevoked) && got.PolicyRef != "" {
				t.Fatalf("unexpected policy binding: %+v", got)
			}
		})
	}
	badMember := member
	badMember.WorkerID = "another-worker"
	got, err := ResolveCheckoff(rev05202History(CheckoffAuthorize), badMember, "tenant-1", rev05202Policy(CheckoffAuthorized), rev05202At(), rev05202At())
	if err != nil || got.State != CheckoffUnknown {
		t.Fatalf("mismatched membership resolved as %+v, err=%v", got, err)
	}
	staleHistory := rev05202RevokedHistory()
	resolved, err := ResolveCheckoff(staleHistory, member, "tenant-1", rev05202Policy(CheckoffAuthorized), rev05202At(), rev05202At())
	if err != nil || resolved.State != CheckoffRevoked {
		t.Fatalf("stale authorization survived later revocation: %+v err=%v", resolved, err)
	}
	crossTenant, err := ResolveCheckoff(rev05202History(CheckoffAuthorize), member, "tenant-2", rev05202Policy(CheckoffAuthorized), rev05202At(), rev05202At())
	if err != nil || crossTenant.State != CheckoffUnknown {
		t.Fatalf("cross-tenant membership resolved as %+v err=%v", crossTenant, err)
	}
	beforeRevocation, err := ResolveCheckoff(staleHistory, member, "tenant-1", rev05202Policy(CheckoffAuthorized), rev05202At().Add(-time.Hour), rev05202At().Add(-time.Hour))
	if err != nil || beforeRevocation.State != CheckoffAuthorized {
		t.Fatalf("authorization failed before revocation effective time: %+v err=%v", beforeRevocation, err)
	}
	active, err := ResolveCheckoff(rev05202History(CheckoffAuthorize), member, "tenant-1", rev05202Policy(CheckoffAuthorized), rev05202At(), rev05202At())
	if err != nil {
		t.Fatal(err)
	}
	start, err := values.ParseLocalDate("2026-09-01")
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.ParseLocalDate("2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	effective, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "payroll", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	amount, err := values.NewDecimal("25.00", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	deduction, err := BuildCheckoffDeduction(active, rev05202PayInputDefinition(t), "union-1", "2026-09", amount, effective)
	if err != nil {
		t.Fatal(err)
	}
	if deduction.Assignment.WorkerRef != "worker-1" || deduction.MembershipID != "member-1" {
		t.Fatalf("deduction subject differs from authorization: %+v", deduction)
	}
	if _, err := BuildCheckoffDeduction(got, rev05202PayInputDefinition(t), "union-1", "2026-09", amount, effective); !errors.Is(err, ErrInvalidCheckoff) {
		t.Fatalf("unknown authorization projection error=%v", err)
	}
	if _, err := BuildCheckoffDeduction(CheckoffResolution{State: CheckoffAuthorized, AuthorizationDigest: "forged"}, rev05202PayInputDefinition(t), "union-1", "2026-09", amount, effective); !errors.Is(err, ErrInvalidCheckoff) {
		t.Fatalf("unbound authorization projection error=%v", err)
	}
	if _, err := ResolveCheckoff(rev05202History("INVENTED"), member, "tenant-1", rev05202Policy(CheckoffAuthorized), rev05202At(), rev05202At()); !errors.Is(err, ErrInvalidCheckoff) {
		t.Fatalf("invalid action error=%v", err)
	}
}

func TestTodo_REV_052_02_Property(t *testing.T) {
	history := rev05202History(CheckoffAuthorize)
	for i := 2; i <= 20; i++ {
		prev := history.Revisions[len(history.Revisions)-1]
		digest, _ := prev.Digest()
		next := prev
		next.ID, next.Revision, next.InstrumentRef, next.EvidenceRef = "authrev-"+strconv.Itoa(i), strconv.Itoa(i), "instrument:"+strconv.Itoa(i), "evidence:"+strconv.Itoa(i)
		next.SupersedesDigest = digest
		history.Revisions = append(history.Revisions, next)
		history.CurrentRevision = next.Revision
		history.HeadDigest, _ = next.Digest()
		if _, err := ResolveCheckoff(history, rev05202Membership(), "tenant-1", rev05202Policy(CheckoffAuthorized), rev05202At(), rev05202At()); err != nil {
			t.Fatalf("valid history through revision %d: %v", i, err)
		}
	}
	for _, mutate := range []func(*CheckoffAuthorizationRevision){func(r *CheckoffAuthorizationRevision) { r.WorkerID = "" }, func(r *CheckoffAuthorizationRevision) { r.MembershipID = "" }, func(r *CheckoffAuthorizationRevision) { r.InstrumentRef = "" }, func(r *CheckoffAuthorizationRevision) { r.EffectiveTo = r.EffectiveFrom }} {
		bad := rev05202History(CheckoffAuthorize)
		mutate(&bad.Revisions[0])
		if _, err := ResolveCheckoff(bad, rev05202Membership(), "tenant-1", rev05202Policy(CheckoffAuthorized), rev05202At(), rev05202At()); !errors.Is(err, ErrInvalidCheckoff) {
			t.Fatalf("invalid history accepted: %+v err=%v", bad, err)
		}
	}
	stale := rev05202RevokedHistory()
	stale.Revisions = stale.Revisions[:1]
	if _, err := ResolveCheckoff(stale, rev05202Membership(), "tenant-1", rev05202Policy(CheckoffAuthorized), rev05202At(), rev05202At()); !errors.Is(err, ErrInvalidCheckoff) {
		t.Fatalf("truncated history accepted: %v", err)
	}
}

func TestTodo_REV_052_02_Golden(t *testing.T) {
	digest, err := rev05202Authorization(CheckoffAuthorize).Digest()
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:06b45b4a448b5568124968a29a57e5891c271a14d1b2a0690fd00b3c5f6bb612"
	if digest != want {
		t.Fatalf("authorization digest=%s want=%s", digest, want)
	}
}

func TestTodo_REV_052_02_Conformance(t *testing.T) {
	// Policy outcomes can only be supplied with a jurisdiction, policy version,
	// and evidence reference; no jurisdiction's rule is embedded here.
	for _, state := range []CheckoffState{CheckoffAuthorized, CheckoffAgencyFeeOnly, CheckoffProhibited} {
		p := rev05202Policy(state)
		if !p.validFor("tenant-1", "worker-1", "member-1") {
			t.Fatalf("policy evidence rejected: %+v", p)
		}
	}
	if (CheckoffPolicyEvidence{State: CheckoffProhibited, Jurisdiction: "x", PolicyRef: "law"}).validFor("tenant-1", "worker-1", "member-1") {
		t.Fatal("policy without evidence was trusted")
	}
}

func TestTodo_REV_052_02_Security(t *testing.T) {
	member := rev05202Membership()
	for _, policy := range []CheckoffPolicyEvidence{{State: CheckoffAuthorized}, {State: "PERMITTED", Jurisdiction: "x", PolicyRef: "p", EvidenceRef: "e", WorkerID: "worker-1", MembershipID: "member-1", JurisdictionBindingRef: "evidence"}, func() CheckoffPolicyEvidence {
		p := rev05202Policy(CheckoffAuthorized)
		p.WorkerID = "another-worker"
		return p
	}(), func() CheckoffPolicyEvidence {
		p := rev05202Policy(CheckoffAuthorized)
		p.TenantID = "tenant-2"
		return p
	}()} {
		got, err := ResolveCheckoff(rev05202History(CheckoffAuthorize), member, "tenant-1", policy, rev05202At(), rev05202At())
		if err != nil {
			t.Fatal(err)
		}
		if got.State != CheckoffUnknown {
			t.Fatalf("unsupported policy produced %s", got.State)
		}
	}
	if _, err := ResolveCheckoff(rev05202History(CheckoffAuthorize), member, "tenant-1", rev05202Policy(CheckoffAuthorized), time.Time{}, rev05202At()); !errors.Is(err, ErrInvalidCheckoff) {
		t.Fatalf("zero time error=%v", err)
	}
	if got, err := ResolveCheckoff(rev05202History(CheckoffAuthorize), member, "tenant-1", rev05202Policy(CheckoffAuthorized), rev05202At(), rev05202At().Add(-2*time.Hour)); err != nil || got.State != CheckoffUnknown {
		t.Fatalf("membership not yet known resolved as %+v, err=%v", got, err)
	}
	ending := rev05202History(CheckoffAuthorize)
	ending.Revisions[0].EffectiveTo = rev05202At()
	ending.HeadDigest, _ = ending.Revisions[0].Digest()
	if got, err := ResolveCheckoff(ending, member, "tenant-1", rev05202Policy(CheckoffAuthorized), rev05202At(), rev05202At()); err != nil || got.State != CheckoffUnknown {
		t.Fatalf("exclusive authorization end resolved as %+v, err=%v", got, err)
	}
}
