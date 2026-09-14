package gateb

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestTodo_GATEB_EVID_001_Property proves monotonicity and determinism:
// voiding any binding of a CONTINUE manifest can only block, and
// identical manifests digest identically.
func TestTodo_GATEB_EVID_001_Property(t *testing.T) {
	healthy := healthyManifest()
	base, err := Compile(healthy)
	if err != nil {
		t.Fatalf("healthy manifest failed to compile: %v", err)
	}
	if base.Verdict != VerdictContinue {
		t.Fatalf("healthy verdict = %s", base.Verdict)
	}

	voids := []func(*Manifest){
		func(m *Manifest) { m.Criteria[0].TodoID = "" },
		func(m *Manifest) { m.Criteria[1].Fixture = "" },
		func(m *Manifest) { m.Criteria[2].Command = "" },
		func(m *Manifest) { m.Criteria[3].EvidenceDigest = "   " },
		func(m *Manifest) { m.Criteria[4].Owner = "" },
		func(m *Manifest) { m.Criteria[5].RetentionDays = -1 },
		func(m *Manifest) { m.Criteria[6].SignOff = "" },
		func(m *Manifest) { m.Criteria[7].Result = ResultFail },
		func(m *Manifest) { m.Criteria[8].ObservedUnixMilli = 0 },
		func(m *Manifest) { m.Criteria[9].MaxAgeMillis = 0 },
	}
	for i, void := range voids {
		manifest := cloneManifest(healthy)
		void(&manifest)
		report, err := Compile(manifest)
		if err != nil {
			t.Fatalf("void %d returned error instead of a verdict: %v", i, err)
		}
		if report.Verdict == VerdictContinue {
			t.Fatalf("void %d still continues", i)
		}
		if report.Grant.Permitted {
			t.Fatalf("void %d granted authority", i)
		}
	}

	again, err := Compile(healthy)
	if err != nil {
		t.Fatalf("second compile failed: %v", err)
	}
	if !reflect.DeepEqual(base, again) {
		t.Fatal("identical manifests compiled to different reports")
	}
}

// TestTodo_GATEB_EVID_001_Golden pins the healthy-report digest: any
// silent change to the manifest math moves the digest and fails here.
func TestTodo_GATEB_EVID_001_Golden(t *testing.T) {
	report, err := Compile(healthyManifest())
	if err != nil {
		t.Fatalf("healthy manifest failed to compile: %v", err)
	}
	const wantDigest = "sha256:bda76566e9580d89587b1ce01acefa99270b3d94492436d6636fd831159be097"
	if report.Digest != wantDigest {
		t.Fatalf("golden digest = %s, want %s", report.Digest, wantDigest)
	}
}

// TestTodo_GATEB_EVID_001_Fault proves malformed manifests fail closed
// with errors — never a verdict — and rejected compiles change nothing.
func TestTodo_GATEB_EVID_001_Fault(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{"gate_ref", func(m *Manifest) { m.GateRef = " " }, "gate ref"},
		{"no_criteria", func(m *Manifest) { m.Criteria = nil }, "criteria"},
		{"duplicate_bullet", func(m *Manifest) { m.Criteria = append(m.Criteria, m.Criteria[0]) }, "duplicate"},
		{"unknown_bullet", func(m *Manifest) {
			m.Criteria[0].Bullet = "launch-the-rockets"
		}, "not required"},
		{"bad_result", func(m *Manifest) { m.Criteria[1].Result = "MAYBE" }, "result"},
		{"negative_observed", func(m *Manifest) { m.Criteria[2].ObservedUnixMilli = -1 }, "observed"},
		{"prior_missing_digest", func(m *Manifest) { m.PriorDecisions[0].Digest = "" }, "prior decision"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := cloneManifest(healthyManifest())
			tc.mutate(&manifest)
			before := cloneManifest(manifest)
			_, err := Compile(manifest)
			if err == nil {
				t.Fatalf("%s compiled without error", tc.name)
			}
			var compileErr *CompileError
			if !errors.As(err, &compileErr) {
				t.Fatalf("%s error is not a typed compile error: %v", tc.name, err)
			}
			if !strings.Contains(compileErr.Reason, tc.want) {
				t.Fatalf("%s reason = %q, want %q", tc.name, compileErr.Reason, tc.want)
			}
			if !reflect.DeepEqual(before, manifest) {
				t.Fatalf("%s mutated its input manifest", tc.name)
			}
		})
	}
}

// TestTodo_GATEB_EVID_001_Security proves tamper-evidence and waiver
// discipline: any digest swap moves the report digest, and an expired
// waiver blocks instead of excusing.
func TestTodo_GATEB_EVID_001_Security(t *testing.T) {
	healthy := healthyManifest()
	base, err := Compile(healthy)
	if err != nil {
		t.Fatalf("healthy manifest failed to compile: %v", err)
	}
	swapped := cloneManifest(healthy)
	swapped.Criteria[0].EvidenceDigest = "sha256:tampered"
	moved, err := Compile(swapped)
	if err != nil {
		t.Fatalf("swapped manifest failed to compile: %v", err)
	}
	if moved.Digest == base.Digest {
		t.Fatal("swapped evidence digest left the report digest unchanged")
	}

	expired := cloneManifest(healthy)
	expired.Criteria[3].Result = ResultFail
	expired.Criteria[3].Waiver = &Waiver{
		By: "support-owner@example.com", Reason: "runbook drill late",
		Scope: "support runbooks", Control: "drill scheduled",
		ExpiresUnixMilli: expired.NowUnixMilli - 1,
	}
	blocked, err := Compile(expired)
	if err != nil {
		t.Fatalf("expired-waiver manifest returned error: %v", err)
	}
	if blocked.Verdict != VerdictGateBlocked {
		t.Fatalf("expired waiver verdict = %s, want GATE_BLOCKED", blocked.Verdict)
	}
	if blocked.Grant.Permitted {
		t.Fatalf("expired waiver granted authority: %+v", blocked.Grant)
	}
}

// TestTodo_GATEB_EVID_001_Conformance proves the closed vocabulary: the
// exact required bullet set, verdicts from the closed set, and zero
// authority on anything but a clean continue.
func TestTodo_GATEB_EVID_001_Conformance(t *testing.T) {
	if len(RequiredBullets) == 0 {
		t.Fatal("no required bullets declared")
	}
	seen := map[string]bool{}
	for _, bullet := range RequiredBullets {
		if bullet == "" || seen[bullet] {
			t.Fatalf("required bullets contain an empty or duplicate entry: %q", bullet)
		}
		seen[bullet] = true
	}
	report, err := Compile(healthyManifest())
	if err != nil {
		t.Fatalf("healthy manifest failed to compile: %v", err)
	}
	if len(report.Criteria) != len(RequiredBullets) {
		t.Fatalf("report covers %d bullets, want %d", len(report.Criteria), len(RequiredBullets))
	}
	for _, criterion := range report.Criteria {
		if !seen[criterion.Bullet] {
			t.Fatalf("report covers undeclared bullet %q", criterion.Bullet)
		}
		switch criterion.Status {
		case StatusPass, StatusFail, StatusUnbound, StatusStale, StatusExpired, StatusUnsigned, StatusWaived, StatusVoidWaiver:
		default:
			t.Fatalf("bullet %s has undeclared status %q", criterion.Bullet, criterion.Status)
		}
	}
	switch report.Verdict {
	case VerdictContinue, VerdictConditionalContinue, VerdictGateBlocked:
	default:
		t.Fatalf("undeclared verdict %q", report.Verdict)
	}
	if report.Verdict != VerdictContinue && report.Grant.Permitted {
		t.Fatal("non-continue verdict granted authority")
	}
}

// TestTodo_GATEB_EVID_001_Recovery proves decisions accumulate
// append-only across compiles — including blocked verdicts, so a block
// can never erase the trail that led to it.
func TestTodo_GATEB_EVID_001_Recovery(t *testing.T) {
	first, err := Compile(healthyManifest())
	if err != nil {
		t.Fatalf("healthy manifest failed to compile: %v", err)
	}
	next := healthyManifest()
	next.PriorDecisions = append([]PriorDecision(nil), first.Decisions...)
	next.Criteria[0].SignOff = ""
	second, err := Compile(next)
	if err != nil {
		t.Fatalf("blocked manifest returned error: %v", err)
	}
	if second.Verdict != VerdictGateBlocked {
		t.Fatalf("unsigned verdict = %s, want GATE_BLOCKED", second.Verdict)
	}
	if len(second.Decisions) != len(first.Decisions)+1 {
		t.Fatalf("blocked compile retains %d decisions, want %d", len(second.Decisions), len(first.Decisions)+1)
	}
	for i, prior := range first.Decisions {
		if second.Decisions[i] != prior {
			t.Fatalf("blocked compile rewrote prior decision %d", i)
		}
	}
	last := second.Decisions[len(second.Decisions)-1]
	if last.Verdict != string(VerdictGateBlocked) || last.Digest != second.Digest {
		t.Fatalf("appended decision = %+v, want the blocked verdict bound to the report digest", last)
	}
}

// TestTodo_GATEB_EVID_001_Mutation flips exactly one binding at a time
// off a known-healthy manifest and proves the verdict blocks with that
// bullet named.
func TestTodo_GATEB_EVID_001_Mutation(t *testing.T) {
	baseline := healthyManifest()
	if report, err := Compile(baseline); err != nil || report.Verdict != VerdictContinue {
		t.Fatalf("baseline manifest should continue: %v %+v", err, report)
	}
	cases := []struct {
		name   string
		mutate func(*Manifest)
		bullet string
	}{
		{"todo", func(m *Manifest) { m.Criteria[0].TodoID = "" }, BulletTransactionCorrectness},
		{"test", func(m *Manifest) { m.Criteria[1].Test = "" }, BulletAccessIsolation},
		{"fixture", func(m *Manifest) { m.Criteria[2].Fixture = "" }, BulletOperabilityRestore},
		{"command", func(m *Manifest) { m.Criteria[3].Command = "" }, BulletOperabilitySupport},
		{"result", func(m *Manifest) { m.Criteria[4].Result = ResultFail }, BulletOperabilityRelease},
		{"digest", func(m *Manifest) { m.Criteria[5].EvidenceDigest = "" }, BulletCustomerEvidence},
		{"owner", func(m *Manifest) { m.Criteria[6].Owner = "" }, BulletPerformanceSoak},
		{"retention", func(m *Manifest) { m.Criteria[7].RetentionDays = -5 }, BulletAssurance},
		{"signoff", func(m *Manifest) { m.Criteria[8].SignOff = "" }, BulletPilotDecision},
		{"stale", func(m *Manifest) { m.Criteria[9].MaxAgeMillis = 1 }, BulletRetentionDisposition},
		{"waiver_scope", func(m *Manifest) {
			m.Criteria[0].Result = ResultFail
			m.Criteria[0].Waiver = &Waiver{By: "a", Reason: "b", Control: "c", ExpiresUnixMilli: m.NowUnixMilli + 1}
		}, BulletTransactionCorrectness},
		{"waiver_control", func(m *Manifest) {
			m.Criteria[1].Result = ResultFail
			m.Criteria[1].Waiver = &Waiver{By: "a", Reason: "b", Scope: "s", ExpiresUnixMilli: m.NowUnixMilli + 1}
		}, BulletAccessIsolation},
		{"waiver_expiry", func(m *Manifest) {
			m.Criteria[2].Result = ResultFail
			m.Criteria[2].Waiver = &Waiver{By: "a", Reason: "b", Scope: "s", Control: "c"}
		}, BulletOperabilityRestore},
		{"waiver_on_pass", func(m *Manifest) {
			m.Criteria[3].Waiver = &Waiver{By: "a", Reason: "b", Scope: "s", Control: "c", ExpiresUnixMilli: m.NowUnixMilli + 1}
		}, BulletOperabilitySupport},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := cloneManifest(baseline)
			tc.mutate(&manifest)
			report, err := Compile(manifest)
			if err != nil {
				t.Fatalf("%s returned error instead of GATE_BLOCKED: %v", tc.name, err)
			}
			if report.Verdict != VerdictGateBlocked {
				t.Fatalf("%s verdict = %s, want GATE_BLOCKED", tc.name, report.Verdict)
			}
			found := false
			for _, blocker := range report.Blockers {
				if blocker.Bullet == tc.bullet {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s names no blocker for %s: %+v", tc.name, tc.bullet, report.Blockers)
			}
			if report.Grant.Permitted {
				t.Fatalf("%s granted authority while blocked", tc.name)
			}
		})
	}
}
