package diagnostic

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func testRequest() Request {
	start := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	return Request{Revision: 1, Actor: "operator-a", Approver: "operator-b", Purpose: "incident-123", Scope: "tenant:opaque-a", Level: LevelDebug, VolumeBudget: 2, StartsAt: start, ExpiresAt: start.Add(30 * time.Minute), Signature: strings.Repeat("s", 64)}
}

func TestDiagnosticElevationRequiresScopedExpiringGovernedConfiguration(t *testing.T) {
	request := testRequest()
	if err := Validate(request); err != nil {
		t.Fatal(err)
	}
	controller := NewController()
	snapshot, err := controller.Apply(request)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Digest != Digest(request) || snapshot.Scope != request.Scope {
		t.Fatalf("snapshot=%+v does not bind request", snapshot)
	}
	if decision := controller.Decide(request.Scope, LevelDebug, request.StartsAt.Add(time.Minute)); !decision.Allowed {
		t.Fatalf("active scoped decision=%+v", decision)
	}
	if decision := controller.Decide(request.Scope, LevelDebug, request.ExpiresAt); decision.Allowed || decision.Reason != "EXPIRED_OR_NOT_ACTIVE" {
		t.Fatalf("expired decision=%+v", decision)
	}
}

func TestTodo_OBS_018_Property(t *testing.T) {
	controller := NewController()
	request := testRequest()
	request.VolumeBudget = 1
	if _, err := controller.Apply(request); err != nil {
		t.Fatal(err)
	}
	when := request.StartsAt.Add(time.Minute)
	if !controller.Decide(request.Scope, LevelInfo, when).Allowed {
		t.Fatal("first diagnostic record was denied")
	}
	if got := controller.Decide(request.Scope, LevelInfo, when); got.Allowed || got.Reason != "VOLUME_BUDGET_EXHAUSTED" {
		t.Fatalf("second decision=%+v", got)
	}
}

func TestTodo_OBS_018_Golden(t *testing.T) {
	request := testRequest()
	snapshot, err := NewController().Apply(request)
	if err != nil {
		t.Fatal(err)
	}
	want := "diagnostic revision=1 scope=tenant:opaque-a level=3 expires=2026-09-02T12:30:00Z"
	if got := snapshot.Explain(); got != want {
		t.Fatalf("Explain=%q, want %q", got, want)
	}
}

func TestTodo_OBS_018_Race(t *testing.T) {
	controller := NewController()
	request := testRequest()
	request.VolumeBudget = 3
	if _, err := controller.Apply(request); err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wg sync.WaitGroup
	allowed := make(chan bool, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decision := controller.Decide(request.Scope, LevelInfo, request.StartsAt.Add(time.Minute))
			allowed <- decision.Allowed
		}()
	}
	wg.Wait()
	close(allowed)
	count := 0
	for ok := range allowed {
		if ok {
			count++
		}
	}
	if count != request.VolumeBudget {
		t.Fatalf("concurrent diagnostic grants=%d, want exact budget %d", count, request.VolumeBudget)
	}
}

func TestTodo_OBS_018_Security(t *testing.T) {
	request := testRequest()
	request.Scope = "tenant:opaque-a *"
	if !errors.Is(Validate(request), ErrInvalidRequest) {
		t.Fatal("wildcard diagnostic scope was accepted")
	}
	request = testRequest()
	request.Actor = request.Approver
	if !errors.Is(Validate(request), ErrInvalidRequest) {
		t.Fatal("self-approved elevation was accepted")
	}
}

func TestTodo_OBS_018_Conformance(t *testing.T) {
	request := testRequest()
	controller := NewController()
	if _, err := controller.Apply(request); err != nil {
		t.Fatal(err)
	}
	if decision := controller.Decide("correlation:other", LevelDebug, request.StartsAt.Add(time.Minute)); decision.Allowed || decision.Reason != "SCOPE_MISMATCH" {
		t.Fatalf("cross-scope decision=%+v", decision)
	}
}

func TestTodo_OBS_018_Mutation(t *testing.T) {
	request := testRequest()
	controller := NewController()
	snapshot, err := controller.Apply(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Scope = "tenant:changed"
	if snapshot.Scope == request.Scope {
		t.Fatal("snapshot shares mutable request state")
	}
}

func TestTodo_OBS_018_SignatureBinding(t *testing.T) {
	base := testRequest()
	cases := []struct {
		name   string
		mutate func(*Request)
	}{
		{"signature", func(r *Request) { r.Signature = strings.Repeat("t", 64) }},
		{"approver", func(r *Request) { r.Approver = "operator-c" }},
		{"volume_budget", func(r *Request) { r.VolumeBudget = 7 }},
		{"scope", func(r *Request) { r.Scope = "tenant:opaque-b" }},
		{"level", func(r *Request) { r.Level = LevelWarn }},
		{"purpose", func(r *Request) { r.Purpose = "incident-999" }},
		{"actor", func(r *Request) { r.Actor = "operator-z" }},
		{"window", func(r *Request) { r.StartsAt = r.StartsAt.Add(time.Minute); r.ExpiresAt = r.ExpiresAt.Add(time.Minute) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := testRequest()
			tc.mutate(&mutated)
			if err := Validate(mutated); err != nil {
				t.Fatalf("mutated request is invalid: %v", err)
			}
			if Digest(mutated) == Digest(base) {
				t.Fatalf("changing %s leaves Digest unchanged", tc.name)
			}
		})
	}
	snapshot, err := NewController().Apply(base)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Approver != base.Approver {
		t.Fatalf("snapshot approver=%q, want %q", snapshot.Approver, base.Approver)
	}
	if snapshot.SignatureDigest == "" {
		t.Fatal("snapshot carries no signature fingerprint")
	}
	if snapshot.SignatureDigest != SignatureFingerprint(base.Signature) {
		t.Fatal("snapshot signature fingerprint does not bind the request token")
	}
	if dumped := fmt.Sprintf("%+v", snapshot); strings.Contains(dumped, base.Signature) {
		t.Fatal("snapshot leaks the raw approval token")
	}
	if SignatureFingerprint(base.Signature) != SignatureFingerprint(base.Signature) {
		t.Fatal("signature fingerprint is not deterministic")
	}
	other := testRequest()
	other.Signature = strings.Repeat("t", 64)
	if SignatureFingerprint(other.Signature) == snapshot.SignatureDigest {
		t.Fatal("distinct approval tokens share a fingerprint")
	}
}

func TestTodo_OBS_018_SnapshotImmutability(t *testing.T) {
	base := testRequest()
	controller := NewController()
	snapshot, err := controller.Apply(base)
	if err != nil {
		t.Fatal(err)
	}
	want := snapshot
	cases := []struct {
		name   string
		mutate func(*Request)
	}{
		{"scope", func(r *Request) { r.Scope = "tenant:changed" }},
		{"signature", func(r *Request) { r.Signature = strings.Repeat("t", 64) }},
		{"approver", func(r *Request) { r.Approver = "operator-c" }},
		{"budget", func(r *Request) { r.VolumeBudget = 99 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.mutate(&req)
			if Digest(req) == snapshot.Digest {
				t.Fatalf("mutated %s still matches the stored digest", tc.name)
			}
			if report := controller.CheckDrift(req); report.Converged {
				t.Fatalf("mutated %s still converges with stored state", tc.name)
			}
		})
	}
	if !reflect.DeepEqual(snapshot, want) {
		t.Fatalf("stored snapshot moved: got %+v, want %+v", snapshot, want)
	}
	if snapshot.Digest != Digest(base) {
		t.Fatal("stored digest no longer binds the applied request")
	}
	when := base.StartsAt.Add(time.Minute)
	if got := controller.Decide(base.Scope, LevelDebug, when); !got.Allowed || got.Digest != snapshot.Digest {
		t.Fatalf("stored configuration was disturbed: decision=%+v", got)
	}
}

func TestTodo_OBS_018_ExpiryEvidence(t *testing.T) {
	base := testRequest()
	cases := []struct {
		name        string
		applyLevel  Level
		budget      int
		consume     bool
		scope       string
		level       Level
		at          func(Request) time.Time
		wantReason  string
		wantAllowed bool
	}{
		{"not_yet_active", LevelDebug, 2, false, base.Scope, LevelInfo, func(r Request) time.Time { return r.StartsAt.Add(-time.Minute) }, "EXPIRED_OR_NOT_ACTIVE", false},
		{"active", LevelDebug, 2, false, base.Scope, LevelInfo, func(r Request) time.Time { return r.StartsAt.Add(time.Minute) }, "ALLOWED", true},
		{"at_exact_expiry", LevelDebug, 2, false, base.Scope, LevelInfo, func(r Request) time.Time { return r.ExpiresAt }, "EXPIRED_OR_NOT_ACTIVE", false},
		{"after_expiry", LevelDebug, 2, false, base.Scope, LevelInfo, func(r Request) time.Time { return r.ExpiresAt.Add(time.Minute) }, "EXPIRED_OR_NOT_ACTIVE", false},
		{"scope_mismatch", LevelDebug, 2, false, "correlation:other", LevelInfo, func(r Request) time.Time { return r.StartsAt.Add(time.Minute) }, "SCOPE_MISMATCH", false},
		{"level_not_granted", LevelWarn, 2, false, base.Scope, LevelDebug, func(r Request) time.Time { return r.StartsAt.Add(time.Minute) }, "LEVEL_NOT_GRANTED", false},
		{"budget_exhausted", LevelDebug, 1, true, base.Scope, LevelInfo, func(r Request) time.Time { return r.StartsAt.Add(time.Minute) }, "VOLUME_BUDGET_EXHAUSTED", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := testRequest()
			request.Level = tc.applyLevel
			request.VolumeBudget = tc.budget
			controller := NewController()
			snapshot, err := controller.Apply(request)
			if err != nil {
				t.Fatal(err)
			}
			if tc.consume {
				if got := controller.Decide(request.Scope, LevelInfo, request.StartsAt.Add(time.Minute)); !got.Allowed {
					t.Fatalf("budget setup decision=%+v", got)
				}
			}
			at := tc.at(request)
			first := controller.Decide(tc.scope, tc.level, at)
			second := controller.Decide(tc.scope, tc.level, at)
			if first.Reason != tc.wantReason || !reflect.DeepEqual(first, second) {
				t.Fatalf("decisions=%+v %+v, want stable reason %q", first, second, tc.wantReason)
			}
			if first.Allowed != tc.wantAllowed {
				t.Fatalf("allowed=%v, want %v", first.Allowed, tc.wantAllowed)
			}
			if first.Revision != snapshot.Revision || first.Digest != snapshot.Digest {
				t.Fatalf("decision does not bind the active snapshot: %+v", first)
			}
			evidence := first.Evidence
			if evidence.Reason != tc.wantReason || evidence.Revision != snapshot.Revision || evidence.Digest != snapshot.Digest {
				t.Fatalf("evidence does not bind the refusal: %+v", evidence)
			}
			if !evidence.At.Equal(at.UTC()) || !evidence.StartsAt.Equal(snapshot.StartsAt) || !evidence.ExpiresAt.Equal(snapshot.ExpiresAt) {
				t.Fatalf("evidence window is not deterministic: %+v", evidence)
			}
			if evidence.LevelRequested != tc.level || evidence.LevelGranted != snapshot.Level {
				t.Fatalf("evidence levels are wrong: %+v", evidence)
			}
			if evidence.SignatureDigest != snapshot.SignatureDigest {
				t.Fatalf("evidence does not bind the approval fingerprint: %+v", evidence)
			}
			rendered := evidence.String()
			if !strings.Contains(rendered, tc.wantReason) || !strings.Contains(rendered, snapshot.Digest) {
				t.Fatalf("evidence rendering omits the decision: %q", rendered)
			}
			if strings.Contains(rendered, request.Signature) {
				t.Fatal("evidence leaks the raw approval token")
			}
		})
	}
}

func TestTodo_OBS_018_StaleRefusal(t *testing.T) {
	base := testRequest()
	base.Revision = 2
	controller := NewController()
	snapshot, err := controller.Apply(base)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		revision uint64
	}{
		{"equal_revision", 2},
		{"older_revision", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := testRequest()
			request.Revision = tc.revision
			_, err := controller.Apply(request)
			if !errors.Is(err, ErrStaleRevision) {
				t.Fatalf("Apply revision %d err=%v, want %v", tc.revision, err, ErrStaleRevision)
			}
			var stale *StaleRevisionError
			if !errors.As(err, &stale) {
				t.Fatalf("refusal carries no evidence: %v", err)
			}
			if stale.RequestRevision != tc.revision || stale.ActiveRevision != snapshot.Revision || stale.ActiveDigest != snapshot.Digest {
				t.Fatalf("stale evidence is wrong: %+v", stale)
			}
		})
	}
	when := base.StartsAt.Add(time.Minute)
	if got := controller.Decide(base.Scope, LevelDebug, when); !got.Allowed || got.Digest != snapshot.Digest {
		t.Fatalf("refused revisions disturbed active state: %+v", got)
	}
}

func TestTodo_OBS_018_ConvergenceDrift(t *testing.T) {
	base := testRequest()
	controller := NewController()
	snapshot, err := controller.Apply(base)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		mutate     func(*Request)
		converged  bool
		reason     string
		driftFiles []string
	}{
		{"identical", func(r *Request) {}, true, "CONVERGED", nil},
		{"signature", func(r *Request) { r.Signature = strings.Repeat("t", 64) }, false, "DRIFT", []string{"signature"}},
		{"approver", func(r *Request) { r.Approver = "operator-c" }, false, "DRIFT", []string{"approver"}},
		{"scope", func(r *Request) { r.Scope = "tenant:opaque-b" }, false, "DRIFT", []string{"scope"}},
		{"level", func(r *Request) { r.Level = LevelWarn }, false, "DRIFT", []string{"level"}},
		{"volume_budget", func(r *Request) { r.VolumeBudget = 9 }, false, "DRIFT", []string{"volume_budget"}},
		{"purpose", func(r *Request) { r.Purpose = "incident-999" }, false, "DRIFT", []string{"purpose"}},
		{"actor", func(r *Request) { r.Actor = "operator-z" }, false, "DRIFT", []string{"actor"}},
		{"window", func(r *Request) { r.StartsAt = r.StartsAt.Add(time.Minute); r.ExpiresAt = r.ExpiresAt.Add(time.Minute) }, false, "DRIFT", []string{"expires_at", "starts_at"}},
		{"revision_bump_same_material", func(r *Request) { r.Revision = 2 }, false, "DRIFT", []string{"revision"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := testRequest()
			tc.mutate(&candidate)
			pure := CheckDrift(snapshot, candidate)
			held := controller.CheckDrift(candidate)
			if !reflect.DeepEqual(pure, held) || !reflect.DeepEqual(pure, CheckDrift(snapshot, candidate)) {
				t.Fatal("convergence result is not deterministic")
			}
			if pure.Converged != tc.converged || pure.Reason != tc.reason {
				t.Fatalf("report=%+v, want converged=%v reason=%q", pure, tc.converged, tc.reason)
			}
			if pure.ActiveRevision != snapshot.Revision || pure.CandidateRevision != candidate.Revision || pure.Digest != snapshot.Digest {
				t.Fatalf("report does not bind the compared inputs: %+v", pure)
			}
			if !reflect.DeepEqual(pure.DriftFields, tc.driftFiles) {
				t.Fatalf("drift fields=%q, want %q", pure.DriftFields, tc.driftFiles)
			}
			if !sort.StringsAreSorted(pure.DriftFields) {
				t.Fatalf("drift fields are not stable: %q", pure.DriftFields)
			}
		})
	}
	if report := NewController().CheckDrift(base); report.Converged || report.Reason != "NO_ACTIVE_CONFIGURATION" {
		t.Fatalf("empty controller report=%+v, want fail-closed drift", report)
	}
}

type stubSignatureAuthority struct{ err error }

func (s stubSignatureAuthority) VerifySignature(Request) error { return s.err }

func TestTodo_OBS_018_AuthorityFailClosed(t *testing.T) {
	request := testRequest()
	t.Run("nil_authority", func(t *testing.T) {
		controller := NewController()
		if _, err := controller.ApplyWithAuthority(request, nil); !errors.Is(err, ErrUnverifiedSignature) {
			t.Fatalf("nil authority err=%v, want %v", err, ErrUnverifiedSignature)
		}
		if report := controller.CheckDrift(request); report.Converged || report.Reason != "NO_ACTIVE_CONFIGURATION" {
			t.Fatalf("refused elevation left state behind: %+v", report)
		}
	})
	t.Run("rejecting_authority", func(t *testing.T) {
		controller := NewController()
		authority := stubSignatureAuthority{err: errors.New("unknown approval key")}
		if _, err := controller.ApplyWithAuthority(request, authority); !errors.Is(err, ErrUnverifiedSignature) {
			t.Fatalf("rejected authority err=%v, want %v", err, ErrUnverifiedSignature)
		} else if !strings.Contains(err.Error(), "unknown approval key") {
			t.Fatalf("authority reason was dropped: %v", err)
		}
		if report := controller.CheckDrift(request); report.Converged {
			t.Fatal("rejected elevation mutated controller state")
		}
	})
	t.Run("accepting_stub_binds_material", func(t *testing.T) {
		controller := NewController()
		snapshot, err := controller.ApplyWithAuthority(request, stubSignatureAuthority{})
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Digest != Digest(request) || snapshot.SignatureDigest != SignatureFingerprint(request.Signature) {
			t.Fatalf("verified snapshot does not bind approval material: %+v", snapshot)
		}
	})
}
