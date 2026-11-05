// TestTodo_WF_EXT_024 proves a run carries a classification and participant
// wall applied to context reads, inspector, timeline, notes and audit.
package runtime_test

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func wfExt024Surfaces() []runtime.RunSurface {
	return []runtime.RunSurface{
		runtime.SurfaceContextReads,
		runtime.SurfaceInspector,
		runtime.SurfaceTimeline,
		runtime.SurfaceNotes,
		runtime.SurfaceAudit,
	}
}

func wfExt024Open(t *testing.T) runtime.RunAccessPolicy {
	t.Helper()
	p, err := runtime.NewOpenRunAccess("CONFIDENTIAL_HR_CASE", "HR_CASE_INVESTIGATION")
	if err != nil {
		t.Fatalf("open walled-test run: %v", err)
	}
	return p
}

func wfExt024Walled(t *testing.T) runtime.RunAccessPolicy {
	t.Helper()
	walled, err := wfExt024Open(t).Wall(
		runtime.RunParticipant{Principal: "principal:case-manager", Role: "CASE_MANAGER"},
		runtime.RunParticipant{Principal: "principal:investigator", Role: "INVESTIGATOR"},
	)
	if err != nil {
		t.Fatalf("wall test run: %v", err)
	}
	return walled
}

func TestTodo_WF_EXT_024(t *testing.T) {
	t.Run("open_run_visible_to_read_scope_on_every_surface", func(t *testing.T) {
		p := wfExt024Open(t)
		if p.Walled() {
			t.Fatal("a run with no participants must stay open")
		}
		for _, s := range wfExt024Surfaces() {
			if err := p.Authorize("principal:any-reader", "HR_CASE_INVESTIGATION", s); err != nil {
				t.Fatalf("open run denies %s: %v", s, err)
			}
			if err := p.Authorize("principal:any-reader", "SOME_OTHER_PURPOSE", s); !errors.Is(err, runtime.ErrRunNotDisclosed) {
				t.Fatalf("open run with wrong purpose must deny %s, got %v", s, err)
			}
		}
	})

	t.Run("wall_restricts_every_surface_to_named_participants", func(t *testing.T) {
		p := wfExt024Walled(t)
		if !p.Walled() {
			t.Fatal("a run with participants must be walled")
		}
		for _, s := range wfExt024Surfaces() {
			if err := p.Authorize("principal:case-manager", "HR_CASE_INVESTIGATION", s); err != nil {
				t.Fatalf("participant denied %s: %v", s, err)
			}
			if err := p.Authorize("principal:stranger", "HR_CASE_INVESTIGATION", s); !errors.Is(err, runtime.ErrRunNotDisclosed) {
				t.Fatalf("non-participant admitted to %s: %v", s, err)
			}
			if err := p.Authorize("", "HR_CASE_INVESTIGATION", s); !errors.Is(err, runtime.ErrRunNotDisclosed) {
				t.Fatalf("empty viewer admitted to %s: %v", s, err)
			}
		}
		if err := p.Authorize("principal:case-manager", "HR_CASE_INVESTIGATION", "SMUGGLED_SURFACE"); !errors.Is(err, runtime.ErrRunAccessInvalid) {
			t.Fatalf("undeclared surface must fail closed, got %v", err)
		}
	})

	t.Run("removed_and_revoked_participants_lose_every_surface", func(t *testing.T) {
		narrowed, err := wfExt024Walled(t).Narrow("principal:investigator")
		if err != nil {
			t.Fatalf("narrow: %v", err)
		}
		if narrowed.Walled() != true {
			t.Fatal("narrowing must keep the wall")
		}
		for _, s := range wfExt024Surfaces() {
			if err := narrowed.Authorize("principal:investigator", "HR_CASE_INVESTIGATION", s); !errors.Is(err, runtime.ErrRunNotDisclosed) {
				t.Fatalf("removed participant admitted to %s: %v", s, err)
			}
			if err := narrowed.Authorize("principal:case-manager", "HR_CASE_INVESTIGATION", s); err != nil {
				t.Fatalf("surviving participant denied %s: %v", s, err)
			}
		}
		revoked, err := wfExt024Walled(t).Revoke("principal:case-manager")
		if err != nil {
			t.Fatalf("revoke: %v", err)
		}
		for _, s := range wfExt024Surfaces() {
			if err := revoked.Authorize("principal:case-manager", "HR_CASE_INVESTIGATION", s); !errors.Is(err, runtime.ErrRunNotDisclosed) {
				t.Fatalf("revoked participant admitted to %s: %v", s, err)
			}
		}
	})

	t.Run("wall_only_narrows", func(t *testing.T) {
		walled := wfExt024Walled(t)
		if _, err := walled.Wall(runtime.RunParticipant{Principal: "principal:newcomer", Role: "WITNESS"}); err == nil {
			t.Fatal("a walled run must not accept a replacement wall")
		}
		if _, err := walled.Narrow("principal:stranger"); err == nil {
			t.Fatal("narrowing a non-participant must fail closed")
		}
		relocked, err := walled.Narrow("principal:investigator")
		if err != nil {
			t.Fatalf("narrow: %v", err)
		}
		if _, err := relocked.Wall(runtime.RunParticipant{Principal: "principal:investigator", Role: "INVESTIGATOR"}); err == nil {
			t.Fatal("a removed participant must never be re-added")
		}
		if relocked.Classification() != walled.Classification() {
			t.Fatal("narrowing must not move the classification")
		}
		if relocked.Digest() == walled.Digest() {
			t.Fatal("narrowing must publish a new policy version")
		}
	})

	t.Run("classification_purpose_and_version_are_carried", func(t *testing.T) {
		p := wfExt024Walled(t)
		if p.Classification() != "CONFIDENTIAL_HR_CASE" || p.Purpose() != "HR_CASE_INVESTIGATION" {
			t.Fatalf("policy lost its classification or purpose: %+v", p)
		}
		if p.Digest() == "" {
			t.Fatal("policy must carry a version digest")
		}
		again := wfExt024Walled(t)
		if again.Digest() != p.Digest() {
			t.Fatal("identical policies must digest identically")
		}
		tenant, instance := uuid.New(), uuid.New()
		bound, err := p.Bind(tenant, instance)
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		if bound.TenantID != tenant || bound.InstanceID != instance || bound.Digest != p.Digest() {
			t.Fatalf("bound policy lost its run identity: %+v", bound)
		}
		if err := bound.Authorize("principal:case-manager", "HR_CASE_INVESTIGATION", runtime.SurfaceAudit); err != nil {
			t.Fatalf("bound policy denies its participant: %v", err)
		}
		if _, err := p.Bind(uuid.Nil, instance); err == nil {
			t.Fatal("binding a nil tenant must fail closed")
		}
	})

	t.Run("malformed_policies_fail_closed", func(t *testing.T) {
		if _, err := runtime.NewOpenRunAccess("", "HR_CASE_INVESTIGATION"); !errors.Is(err, runtime.ErrRunAccessInvalid) {
			t.Fatalf("empty classification must fail closed, got %v", err)
		}
		if _, err := runtime.NewOpenRunAccess("CONFIDENTIAL_HR_CASE", ""); !errors.Is(err, runtime.ErrRunAccessInvalid) {
			t.Fatalf("empty purpose must fail closed, got %v", err)
		}
		open := wfExt024Open(t)
		if _, err := open.Wall(); !errors.Is(err, runtime.ErrRunAccessInvalid) {
			t.Fatalf("an empty wall must fail closed, got %v", err)
		}
		if _, err := open.Wall(runtime.RunParticipant{Principal: "", Role: "WITNESS"}); !errors.Is(err, runtime.ErrRunAccessInvalid) {
			t.Fatalf("an empty principal must fail closed, got %v", err)
		}
		if _, err := open.Wall(
			runtime.RunParticipant{Principal: "principal:amy", Role: "WITNESS"},
			runtime.RunParticipant{Principal: "principal:amy", Role: "WITNESS"},
		); !errors.Is(err, runtime.ErrRunAccessInvalid) {
			t.Fatalf("a duplicated participant must fail closed, got %v", err)
		}
		var zero runtime.RunAccessPolicy
		if err := zero.Authorize("principal:amy", "HR_CASE_INVESTIGATION", runtime.SurfaceAudit); !errors.Is(err, runtime.ErrRunAccessInvalid) {
			t.Fatalf("the zero policy must fail closed, got %v", err)
		}
	})
}

// TestTodo_WF_EXT_024_Security proves a non-participant with broad read scope
// sees nothing: every surface denies with one non-disclosing refusal.
func TestTodo_WF_EXT_024_Security(t *testing.T) {
	walled := wfExt024Walled(t)
	// The broad read scope is real: the same viewer is admitted to an open
	// run, so the wall — not a missing scope — is what denies them here.
	for _, s := range wfExt024Surfaces() {
		if err := wfExt024Open(t).Authorize("principal:broad-reader", "HR_CASE_INVESTIGATION", s); err != nil {
			t.Fatalf("fixture open run denies broad reader on %s: %v", s, err)
		}
	}
	var first error
	for _, s := range wfExt024Surfaces() {
		err := walled.Authorize("principal:broad-reader", "HR_CASE_INVESTIGATION", s)
		if !errors.Is(err, runtime.ErrRunNotDisclosed) {
			t.Fatalf("broad read scope admitted to %s: %v", s, err)
		}
		if first == nil {
			first = err
		} else if first.Error() != err.Error() {
			t.Fatalf("denials disclose their surface: %q vs %q", first, err)
		}
	}
	// Missing, revoked, wrong-purpose and wrong-surface-counterpart denials
	// are indistinguishable from the non-participant denial.
	revoked, err := walled.Revoke("principal:investigator")
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	peers := []error{
		revoked.Authorize("principal:investigator", "HR_CASE_INVESTIGATION", runtime.SurfaceInspector),
		walled.Authorize("principal:case-manager", "WRONG_PURPOSE", runtime.SurfaceNotes),
		walled.Authorize("principal:nobody", "HR_CASE_INVESTIGATION", runtime.SurfaceTimeline),
	}
	for _, peer := range peers {
		if !errors.Is(peer, runtime.ErrRunNotDisclosed) || peer.Error() != first.Error() {
			t.Fatalf("denial discloses its cause: %v vs %v", peer, first)
		}
	}
}

// TestTodo_WF_EXT_024_Property proves the wall only narrows as the run
// proceeds: once a viewer is denied they stay denied, the participant set
// only shrinks, and the classification never moves.
func TestTodo_WF_EXT_024_Property(t *testing.T) {
	rng := rand.New(rand.NewSource(24))
	viewers := []string{"principal:a", "principal:b", "principal:c", "principal:d", "principal:e", "principal:f"}
	surfaces := wfExt024Surfaces()
	for i := 0; i < 300; i++ {
		policy := wfExt024Open(t)
		classification := policy.Classification()
		digest := policy.Digest()
		// Random wall: a random non-empty subset in random order.
		order := rng.Perm(len(viewers))
		take := 1 + rng.Intn(len(viewers))
		var wall []runtime.RunParticipant
		for _, o := range order[:take] {
			wall = append(wall, runtime.RunParticipant{Principal: viewers[o], Role: "MEMBER"})
		}
		walled, err := policy.Wall(wall...)
		if err != nil {
			t.Fatalf("case %d: wall: %v", i, err)
		}
		if walled.Digest() == digest {
			t.Fatalf("case %d: walling kept the policy version", i)
		}
		policy = walled
		denied := map[string]bool{}
		// Random narrow chain over the remaining participants.
		remaining := append([]int(nil), order[:take]...)
		for len(remaining) > 0 {
			victim := remaining[rng.Intn(len(remaining))]
			kept := remaining[:0]
			for _, r := range remaining {
				if viewers[r] != viewers[victim] {
					kept = append(kept, r)
				}
			}
			remaining = kept
			next, err := policy.Narrow(viewers[victim])
			if err != nil {
				t.Fatalf("case %d: narrow: %v", i, err)
			}
			if next.Digest() == policy.Digest() {
				t.Fatalf("case %d: narrowing kept the policy version", i)
			}
			policy = next
			for _, v := range viewers {
				for _, s := range surfaces {
					key := v + "\x00" + string(s)
					err := policy.Authorize(v, "HR_CASE_INVESTIGATION", s)
					if denied[key] && err == nil {
						t.Fatalf("case %d: %s on %s was denied and is admitted again", i, v, s)
					}
					if err != nil {
						if !errors.Is(err, runtime.ErrRunNotDisclosed) {
							t.Fatalf("case %d: %s on %s refused with a disclosing error: %v", i, v, s, err)
						}
						denied[key] = true
					}
				}
			}
			if policy.Classification() != classification {
				t.Fatalf("case %d: classification moved while narrowing", i)
			}
			for _, v := range viewers {
				want := false
				for _, r := range remaining {
					if viewers[r] == v {
						want = true
					}
				}
				_, isParticipant := policy.ParticipantRole(v)
				if isParticipant != want {
					t.Fatalf("case %d: participant set did not shrink monotonically for %s", i, v)
				}
			}
		}
	}
}
