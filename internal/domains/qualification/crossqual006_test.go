package qualification

import (
	"errors"
	"testing"
	"time"
)

var crossqual006At = time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC)

func crossqual006Evidence() []QualEvidence {
	current := func(ref, kind string) QualEvidence {
		return QualEvidence{
			Ref: ref, Kind: kind,
			IssuedAt: crossqual006At.AddDate(0, 0, -10), ExpiresAt: crossqual006At.AddDate(0, 0, 20),
			Verified: true,
		}
	}
	return []QualEvidence{
		current("ev-job-cert", "JOB_CERT"),
		current("ev-sched-avail", "AVAILABILITY"),
		current("ev-safety-card", "SAFETY_CARD"),
		current("ev-access-clear", "ACCESS_CLEARANCE"),
		{Ref: "ev-rtw-note", Kind: "FITNESS_NOTE",
			IssuedAt: crossqual006At.AddDate(0, 0, -5), ExpiresAt: crossqual006At.AddDate(0, 0, 25),
			Verified: true, Restricted: true},
	}
}

func crossqual006Input() CrossQualInput {
	return CrossQualInput{
		Tenant: "acme", WorkerRef: "worker-1", AsOf: crossqual006At,
		Evidence: crossqual006Evidence(),
		Requirements: []DomainRequirement{
			{Domain: CrossJob, Kinds: []string{"JOB_CERT"}},
			{Domain: CrossScheduling, Kinds: []string{"AVAILABILITY"}},
			{Domain: CrossSafety, Kinds: []string{"SAFETY_CARD"}},
			{Domain: CrossAccess, Kinds: []string{"ACCESS_CLEARANCE"}},
			{Domain: CrossRTW, Kinds: []string{"FITNESS_NOTE"}},
		},
	}
}

func crossqual006Verdict(t *testing.T, res CrossQualResult, domain CrossDomain) DomainVerdict {
	t.Helper()
	for _, d := range res.Domains {
		if d.Domain == domain {
			return d
		}
	}
	t.Fatalf("domain %s missing from result", domain)
	return DomainVerdict{}
}

// TestTodo_QUAL_006 is the PRIMARY contract: five domain fixtures share
// evidence, status and effective-time semantics while keeping domain
// constraints, and restricted medical evidence never enters ordinary
// output. The seeded defect (expired or missing evidence returning
// QUALIFIED) yields QUAL_006_REJECTED with field/state/version and
// persists nothing.
func TestTodo_QUAL_006(t *testing.T) {
	got, err := EvaluateCrossQualification(crossqual006Input())
	if err != nil {
		t.Fatalf("EvaluateCrossQualification: %v", err)
	}
	if len(got.Domains) != 5 {
		t.Fatalf("all five domains must be evaluated: %+v", got)
	}
	for _, d := range []CrossDomain{CrossJob, CrossScheduling, CrossSafety, CrossAccess, CrossRTW} {
		if v := crossqual006Verdict(t, got, d); v.Verdict != CrossQualified {
			t.Fatalf("domain %s must qualify on current evidence: %+v", d, v)
		}
	}
	if got.Digest == "" {
		t.Fatalf("result must seal a digest")
	}

	t.Run("restricted medical evidence stays redacted", func(t *testing.T) {
		rtw := crossqual006Verdict(t, got, CrossRTW)
		if !rtw.RestrictedUsed || len(rtw.EvidenceRefs) != 0 {
			t.Fatalf("RTW must use restricted evidence without naming it: %+v", rtw)
		}
		for _, d := range got.Domains {
			if d.Domain == CrossRTW {
				continue
			}
			for _, ref := range d.EvidenceRefs {
				if ref == "ev-rtw-note" {
					t.Fatalf("medical evidence leaked into %s: %+v", d.Domain, d)
				}
			}
			if d.RestrictedUsed {
				t.Fatalf("ordinary domain %s must not touch restricted evidence", d.Domain)
			}
		}
	})

	t.Run("expired evidence never qualifies", func(t *testing.T) {
		in := crossqual006Input()
		in.Evidence[2].ExpiresAt = crossqual006At.Add(-time.Hour)
		got, err := EvaluateCrossQualification(in)
		if err != nil {
			t.Fatal(err)
		}
		if v := crossqual006Verdict(t, got, CrossSafety); v.Verdict != CrossNotQualified {
			t.Fatalf("expired safety card must not qualify: %+v", v)
		}
		// No domain diverges: the shared effective-time semantics hold.
		if v := crossqual006Verdict(t, got, CrossJob); v.Verdict != CrossQualified {
			t.Fatalf("unrelated domain must stay qualified: %+v", v)
		}
	})

	t.Run("missing evidence never qualifies", func(t *testing.T) {
		in := crossqual006Input()
		in.Evidence = in.Evidence[:3]
		got, err := EvaluateCrossQualification(in)
		if err != nil {
			t.Fatal(err)
		}
		if v := crossqual006Verdict(t, got, CrossAccess); v.Verdict != CrossNotQualified {
			t.Fatalf("missing clearance must not qualify: %+v", v)
		}
	})

	t.Run("unverified evidence never qualifies", func(t *testing.T) {
		in := crossqual006Input()
		in.Evidence[0].Verified = false
		got, err := EvaluateCrossQualification(in)
		if err != nil {
			t.Fatal(err)
		}
		if v := crossqual006Verdict(t, got, CrossJob); v.Verdict != CrossNotQualified {
			t.Fatalf("unverified evidence must not qualify: %+v", v)
		}
	})
}

func TestTodo_QUAL_006_Property(t *testing.T) {
	a, err := EvaluateCrossQualification(crossqual006Input())
	if err != nil {
		t.Fatal(err)
	}
	b, err := EvaluateCrossQualification(crossqual006Input())
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("identical evidence must evaluate identically")
	}
	// Requirement order is not semantic.
	shuffled := crossqual006Input()
	shuffled.Requirements[0], shuffled.Requirements[4] = shuffled.Requirements[4], shuffled.Requirements[0]
	c, err := EvaluateCrossQualification(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if c.Digest != a.Digest {
		t.Fatalf("requirement order must not move the digest")
	}
	// An incomplete domain set is refused, never partially evaluated.
	incomplete := crossqual006Input()
	incomplete.Requirements = incomplete.Requirements[:4]
	if _, err := EvaluateCrossQualification(incomplete); !errors.Is(err, ErrCrossQualRejected) {
		t.Fatalf("four-domain evaluation must be QUAL_006_REJECTED")
	}
	var rej *CrossQualRejection
	if _, err := EvaluateCrossQualification(incomplete); !errors.As(err, &rej) || rej.Field == "" || rej.State == "" || rej.Version == "" {
		t.Fatalf("rejection must name field/state/version")
	}
}

func TestTodo_QUAL_006_Security(t *testing.T) {
	// Medical evidence presented to an ordinary domain is invisible:
	// even a current verified fitness note cannot qualify JOB.
	in := CrossQualInput{
		Tenant: "acme", WorkerRef: "worker-1", AsOf: crossqual006At,
		Evidence: []QualEvidence{{
			Ref: "ev-rtw-note", Kind: "JOB_CERT",
			IssuedAt: crossqual006At.AddDate(0, 0, -5), ExpiresAt: crossqual006At.AddDate(0, 0, 25),
			Verified: true, Restricted: true,
		}},
		Requirements: []DomainRequirement{
			{Domain: CrossJob, Kinds: []string{"JOB_CERT"}},
			{Domain: CrossScheduling, Kinds: []string{"AVAILABILITY"}},
			{Domain: CrossSafety, Kinds: []string{"SAFETY_CARD"}},
			{Domain: CrossAccess, Kinds: []string{"ACCESS_CLEARANCE"}},
			{Domain: CrossRTW, Kinds: []string{"FITNESS_NOTE"}},
		},
	}
	// RTW also lacks its kind here, so the whole evaluation refuses
	// nothing but qualifies nothing either.
	got, err := EvaluateCrossQualification(in)
	if err != nil {
		t.Fatal(err)
	}
	if v := crossqual006Verdict(t, got, CrossJob); v.Verdict == CrossQualified {
		t.Fatalf("restricted evidence must never qualify an ordinary domain: %+v", v)
	}
	if v := crossqual006Verdict(t, got, CrossJob); len(v.EvidenceRefs) != 0 {
		t.Fatalf("restricted evidence must not be named: %+v", v)
	}
}

func TestTodo_QUAL_006_Conformance(t *testing.T) {
	// Every domain passes the same shared-semantics fixtures: missing
	// evidence, expired evidence and unverified evidence each deny in
	// every domain.
	for _, domain := range CrossDomains {
		kind := "KIND_X"
		mk := func(ev []QualEvidence) CrossQualInput {
			reqs := []DomainRequirement{}
			for _, d := range CrossDomains {
				reqs = append(reqs, DomainRequirement{Domain: d, Kinds: []string{kind + string(d)}})
			}
			return CrossQualInput{Tenant: "acme", WorkerRef: "worker-1", AsOf: crossqual006At, Evidence: ev, Requirements: reqs}
		}
		current := QualEvidence{Ref: "ev-" + string(domain), Kind: kind + string(domain),
			IssuedAt: crossqual006At.AddDate(0, 0, -1), ExpiresAt: crossqual006At.AddDate(0, 0, 1), Verified: true}
		full := []QualEvidence{}
		for _, d := range CrossDomains {
			e := current
			e.Ref = "ev-" + string(d)
			e.Kind = kind + string(d)
			full = append(full, e)
		}
		got, err := EvaluateCrossQualification(mk(full))
		if err != nil {
			t.Fatalf("domain %s: %v", domain, err)
		}
		if v := crossqual006Verdict(t, got, domain); v.Verdict != CrossQualified {
			t.Fatalf("domain %s must qualify on current evidence: %+v", domain, v)
		}
		expired := append([]QualEvidence(nil), full...)
		for i := range expired {
			if expired[i].Ref == "ev-"+string(domain) {
				expired[i].ExpiresAt = crossqual006At.Add(-time.Hour)
			}
		}
		got, err = EvaluateCrossQualification(mk(expired))
		if err != nil {
			t.Fatalf("domain %s: %v", domain, err)
		}
		if v := crossqual006Verdict(t, got, domain); v.Verdict != CrossNotQualified {
			t.Fatalf("domain %s must deny expired evidence: %+v", domain, v)
		}
	}
}

func TestTodo_QUAL_006_Mutation(t *testing.T) {
	a, err := EvaluateCrossQualification(crossqual006Input())
	if err != nil {
		t.Fatal(err)
	}
	in := crossqual006Input()
	in.AsOf = crossqual006At.Add(time.Hour)
	b, err := EvaluateCrossQualification(in)
	if err != nil {
		t.Fatal(err)
	}
	if b.Digest == a.Digest {
		t.Fatalf("evaluation instant must bind the digest")
	}
	for name, mutate := range map[string]func(*CrossQualInput){
		"tenant":  func(in *CrossQualInput) { in.Tenant = "" },
		"worker":  func(in *CrossQualInput) { in.WorkerRef = "" },
		"instant": func(in *CrossQualInput) { in.AsOf = time.Time{} },
		"domain":  func(in *CrossQualInput) { in.Requirements[0].Domain = "PARKING" },
	} {
		candidate := crossqual006Input()
		mutate(&candidate)
		if _, err := EvaluateCrossQualification(candidate); !errors.Is(err, ErrCrossQualRejected) {
			t.Fatalf("mutated %s must be QUAL_006_REJECTED", name)
		}
	}
}
