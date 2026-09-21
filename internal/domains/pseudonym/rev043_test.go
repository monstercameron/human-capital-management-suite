// REV-043-02 RED: ScopeLimiter denials must publish an ABUSE-001 activity
// signal (pseudonymous scope and code only, no token) covered by ABUSE-004
// risk scoring and ABUSE-007 drift evaluation.
package pseudonym

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/abuse"
)

func rev043Burst(t *testing.T, lim *ScopeLimiter, now time.Time) {
	t.Helper()
	if err := lim.Check(now, "tok-a", "digest-a", anonProof(now, "n-a")); err != nil {
		t.Fatalf("first submission must pass: %v", err)
	}
	dupErr := lim.Check(now.Add(time.Second), "tok-a", "digest-a", anonProof(now.Add(time.Second), "n-b"))
	var rej *LimitRejection
	if !errors.As(dupErr, &rej) || rej.Code != LimitDuplicate {
		t.Fatalf("duplicate error = %v, want DUPLICATE", dupErr)
	}
	for i := 0; i < 5; i++ {
		at := now.Add(time.Duration(10+i) * time.Second)
		if err := lim.Check(at, "tok-bot", fmt.Sprintf("digest-bot-%d", i), anonProof(at, fmt.Sprintf("nonce-bot-%d", i))); err != nil {
			t.Fatalf("budget submission %d must pass: %v", i, err)
		}
	}
	at := now.Add(20 * time.Second)
	last := lim.Check(at, "tok-bot", "digest-bot-final", anonProof(at, "nonce-bot-final"))
	if !isLimitCode(last, LimitRateLimited) {
		t.Fatalf("burst error = %v, want RATE_LIMITED", last)
	}
}

// TestTodo_REV_043_02 is the PRIMARY contract: every ScopeLimiter denial
// publishes a governed ABUSE-001 activity signal carrying only the
// pseudonymous scope and denial code, and that signal is covered by ABUSE-004
// risk scoring and ABUSE-007 drift evaluation like any other detector input.
func TestTodo_REV_043_02(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	lim, err := NewScopeLimiter(anonScopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	rev043Burst(t, lim, now)

	signals, err := lim.DenialSignals("tenant-a", "")
	if err != nil {
		t.Fatalf("DenialSignals: %v", err)
	}
	if len(signals) != len(lim.Reviews()) || len(signals) < 2 {
		t.Fatalf("signals = %d, reviews = %d, want one signal per denial", len(signals), len(lim.Reviews()))
	}

	seenCodes := map[string]bool{}
	seenIDs := map[string]bool{}
	for _, sig := range signals {
		if err := sig.Validate(); err != nil {
			t.Fatalf("denial signal invalid: %v (%+v)", err, sig)
		}
		if sig.Kind != abuse.SignalKindAuthAnomaly {
			t.Fatalf("signal kind = %q, want AUTH_ANOMALY", sig.Kind)
		}
		if _, ok := sig.Kind.Classification(); !ok {
			t.Fatalf("signal kind %q has no classification", sig.Kind)
		}
		if sig.Subject.Type != DenialScopeRefType || sig.Subject.ID != "intake:case" {
			t.Fatalf("subject = %+v, want pseudonymous scope intake:case", sig.Subject)
		}
		if sig.Subject.ID == "" || sig.Actor.ID == "" {
			t.Fatalf("signal refs must name scope and code: %+v", sig)
		}
		if sig.Tenant != "tenant-a" {
			t.Fatalf("signal tenant = %q, want tenant-a", sig.Tenant)
		}
		if sig.RawContent != "" {
			t.Fatalf("signal carries raw content: %q", sig.RawContent)
		}
		first, err := sig.Digest()
		if err != nil || first == "" {
			t.Fatalf("signal digest error = %v", err)
		}
		second, err := sig.Digest()
		if err != nil || second != first {
			t.Fatal("signal digest is not deterministic")
		}
		if seenIDs[sig.ID] {
			t.Fatalf("duplicate signal id %q", sig.ID)
		}
		seenIDs[sig.ID] = true
		seenCodes[sig.Actor.ID] = true
	}
	if !seenCodes[string(LimitDuplicate)] || !seenCodes[string(LimitRateLimited)] {
		t.Fatalf("denial codes covered = %v, want DUPLICATE and RATE_LIMITED", seenCodes)
	}

	// ABUSE-004: every denial signal scores as a contributing finding ref.
	table, err := abuse.NewWeightingTable("rev043", "1", []abuse.FindingWeight{
		{Category: abuse.FindingPrivilegeBurst, Weight: 40, Confidence: 80},
	}, 100, 50)
	if err != nil {
		t.Fatal(err)
	}
	scorer, err := abuse.NewRiskScorer(table)
	if err != nil {
		t.Fatal(err)
	}
	window := abuse.RiskWindow{Start: now, End: now.Add(time.Hour)}
	findings := make([]abuse.RiskFinding, 0, len(signals))
	for _, sig := range signals {
		findings = append(findings, abuse.RiskFinding{
			DetectorID: "pseudonym-denial-detector", DetectorSemver: "1.0.0", DetectorDigest: "sha256:detector",
			SignalID: sig.ID, Kind: sig.Kind, Category: abuse.FindingPrivilegeBurst, Severity: abuse.SeverityHigh,
			Evidence: abuse.RiskEvidence{Scope: abuse.FindingScopePrincipal, Principal: "confidential-intake"},
		})
	}
	assessment, err := abuse.ScoreRisk(scorer, "confidential-intake", window, findings)
	if err != nil {
		t.Fatalf("ScoreRisk: %v", err)
	}
	covered := map[string]bool{}
	for _, ref := range assessment.ContributingFindingRefs {
		covered[ref.SignalID] = true
	}
	for _, sig := range signals {
		if !covered[sig.ID] {
			t.Fatalf("denial signal %s is not covered by risk scoring", sig.ID)
		}
	}

	// ABUSE-007: every denial signal evaluates like any other detector input.
	outcomes := make([]abuse.ReviewedOutcome, 0, len(signals))
	for _, sig := range signals {
		outcomes = append(outcomes, abuse.ReviewedOutcome{
			SignalID: sig.ID, Verdict: abuse.ReviewedFalsePositive,
			Reviewer: "reviewer-1", At: now.Add(30 * time.Minute),
		})
	}
	eval, err := abuse.EvaluateDetector(abuse.EvaluationInput{
		DetectorID: "pseudonym-denial-detector", DetectorVersion: "1.0.0", DetectorDigest: "sha256:detector",
		Outcomes: outcomes, SampledTotal: len(outcomes), EvaluatedAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("EvaluateDetector: %v", err)
	}
	if !eval.Action.Valid() {
		t.Fatalf("evaluation action = %q, want a governed action", eval.Action)
	}
	if eval.Reviewed != len(signals) {
		t.Fatalf("evaluated = %d, want %d", eval.Reviewed, len(signals))
	}
}

// TestTodo_REV_043_02_Security proves no token or raw identity crosses into
// the abuse signal: only the pseudonymous scope and the closed-vocabulary
// denial code travel.
func TestTodo_REV_043_02_Security(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	lim, err := NewScopeLimiter(anonScopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	token := "blinded-token-SECRET-9f27"
	if err := lim.Check(now, token, "payload-digest-1", anonProof(now, "nonce-s-1")); err != nil {
		t.Fatal(err)
	}
	dupErr := lim.Check(now.Add(time.Second), token, "payload-digest-1", anonProof(now.Add(time.Second), "nonce-s-2"))
	var rej *LimitRejection
	if !errors.As(dupErr, &rej) || rej.Code != LimitDuplicate {
		t.Fatalf("duplicate error = %v, want DUPLICATE", dupErr)
	}

	signals, err := lim.DenialSignals("tenant-a", "intake-monitor")
	if err != nil {
		t.Fatalf("DenialSignals: %v", err)
	}
	if len(signals) == 0 {
		t.Fatal("denial published no signal")
	}
	for _, sig := range signals {
		blob := fmt.Sprintf("%+v", sig)
		for _, secret := range []string{token, "SECRET-9f27", "payload-digest-1", "nonce-s-1", "nonce-s-2"} {
			if strings.Contains(blob, secret) {
				t.Fatalf("token/raw identity %q crossed into abuse signal %+v", secret, sig)
			}
		}
		if sig.RawContent != "" {
			t.Fatalf("signal carries raw content: %q", sig.RawContent)
		}
		if err := sig.Validate(); err != nil {
			t.Fatalf("denial signal invalid: %v", err)
		}
		if _, err := sig.Digest(); err != nil {
			t.Fatalf("signal digest refused: %v", err)
		}
		// Positive shape: exactly scope plus closed-vocabulary code.
		if sig.Subject.ID != "intake:case" {
			t.Fatalf("subject = %+v, want pseudonymous scope only", sig.Subject)
		}
		if code := LimitCode(sig.Actor.ID); !code.Valid() {
			t.Fatalf("actor = %+v, want a closed-vocabulary denial code", sig.Actor)
		}
		if sig.SourceSystem != "intake-monitor" {
			t.Fatalf("source system = %q, want intake-monitor", sig.SourceSystem)
		}
	}

	if _, err := lim.DenialSignals("", ""); err == nil {
		t.Fatal("tenantless signal publication accepted")
	}
}
