package publication

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestTodo_AGENT_009(t *testing.T) {
	f := newFixture(t)
	review, err := f.service.Review(context.Background(), "tenant-a", "agent-a", 1, "reviewer-a", f.manifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	publication, err := f.service.Publish(context.Background(), "tenant-a", "agent-a", 1, 0, f.manifestDigest, f.evalDigest, review.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if publication.ManifestDigest != f.manifestDigest || publication.EvaluationDigest != f.evalDigest || publication.ReviewerID != "reviewer-a" || publication.Generation != 1 {
		t.Fatalf("publication did not pin exact evidence: %+v", publication)
	}
}

func TestTodo_AGENT_009_Race(t *testing.T) {
	f := newFixture(t)
	review, err := f.service.Review(context.Background(), "tenant-a", "agent-a", 1, "reviewer-a", f.manifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.service.Publish(context.Background(), "tenant-a", "agent-a", 1, 0, f.manifestDigest, f.evalDigest, review.Digest)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				accepted++
			} else if errors.Is(err, ErrConflict) {
				conflicts++
			} else {
				t.Errorf("unexpected publish result: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if accepted != 1 || conflicts != 1 {
		t.Fatalf("accepted=%d conflicts=%d, want one CAS winner", accepted, conflicts)
	}
}

func TestTodo_AGENT_009_Security(t *testing.T) {
	t.Run("author cannot review", func(t *testing.T) {
		f := newFixture(t)
		if _, err := f.service.Review(context.Background(), "tenant-a", "agent-a", 1, "owner-a", f.manifestDigest); !errors.Is(err, ErrReview) {
			t.Fatalf("author review error = %v", err)
		}
	})
	t.Run("unauthorized reviewer refused", func(t *testing.T) {
		f := newFixture(t)
		f.authority.allowed = false
		if _, err := f.service.Review(context.Background(), "tenant-a", "agent-a", 1, "reviewer-a", f.manifestDigest); !errors.Is(err, ErrReview) {
			t.Fatalf("unauthorized review error = %v", err)
		}
	})
	t.Run("review authority is rechecked at publish", func(t *testing.T) {
		f := newFixture(t)
		review, err := f.service.Review(context.Background(), "tenant-a", "agent-a", 1, "reviewer-a", f.manifestDigest)
		if err != nil {
			t.Fatal(err)
		}
		f.authority.allowed = false
		if _, err := f.service.Publish(context.Background(), "tenant-a", "agent-a", 1, 0, f.manifestDigest, f.evalDigest, review.Digest); !errors.Is(err, ErrReview) {
			t.Fatalf("revoked reviewer permission error = %v", err)
		}
	})
	t.Run("quarantine fences pinned admission", func(t *testing.T) {
		f := publishedFixture(t)
		if err := f.service.Quarantine(context.Background(), "tenant-a", "agent-a", f.manifestDigest, "incident-7"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.Admit(context.Background(), "tenant-a", "agent-a", f.manifestDigest); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("admit after quarantine error = %v", err)
		}
		if len(f.quarantine.calls) != 1 || f.quarantine.calls[0] != f.manifestDigest {
			t.Fatalf("write leases were not fenced for exact digest: %+v", f.quarantine.calls)
		}
		if _, err := f.service.Publish(context.Background(), "tenant-a", "agent-a", 1, 1, f.manifestDigest, f.evalDigest, f.repo.history[0].ReviewDigest); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("republish of quarantined version error = %v", err)
		}
	})
	t.Run("write fence failure remains reported after admission closes", func(t *testing.T) {
		f := publishedFixture(t)
		f.quarantine.err = errors.New("kill-switch unavailable")
		if err := f.service.Quarantine(context.Background(), "tenant-a", "agent-a", f.manifestDigest, "incident-7"); !errors.Is(err, ErrWriteFence) {
			t.Fatalf("write-fence failure error = %v", err)
		}
		if _, err := f.service.Admit(context.Background(), "tenant-a", "agent-a", f.manifestDigest); !errors.Is(err, ErrQuarantined) {
			t.Fatalf("admission was not durably fenced first: %v", err)
		}
	})
}

func TestTodo_AGENT_009_Mutation(t *testing.T) {
	t.Run("stale manifest refuses", func(t *testing.T) {
		f := newFixture(t)
		changed := testManifest()
		changed.Purpose = "changed after review"
		f.manifests.manifest = changed
		if _, err := f.service.Review(context.Background(), "tenant-a", "agent-a", 1, "reviewer-a", f.manifestDigest); !errors.Is(err, ErrStaleManifest) {
			t.Fatalf("stale digest error = %v", err)
		}
	})
	t.Run("evaluation digest cannot be substituted", func(t *testing.T) {
		f := newFixture(t)
		review, err := f.service.Review(context.Background(), "tenant-a", "agent-a", 1, "reviewer-a", f.manifestDigest)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.Publish(context.Background(), "tenant-a", "agent-a", 1, 0, f.manifestDigest, digest("other-eval"), review.Digest); !errors.Is(err, ErrEvaluation) {
			t.Fatalf("substituted evaluation error = %v", err)
		}
	})
}

func TestTodo_AGENT_009_Fault(t *testing.T) {
	f := newFixture(t)
	review, err := f.service.Review(context.Background(), "tenant-a", "agent-a", 1, "reviewer-a", f.manifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	f.repo.publishErr = errors.New("storage unavailable")
	if _, err := f.service.Publish(context.Background(), "tenant-a", "agent-a", 1, 0, f.manifestDigest, f.evalDigest, review.Digest); err == nil {
		t.Fatal("storage fault was hidden")
	}
	if f.repo.generation != 0 || len(f.repo.history) != 0 {
		t.Fatalf("failed publish changed state: generation=%d history=%d", f.repo.generation, len(f.repo.history))
	}
}

func TestTodo_AGENT_009_Golden(t *testing.T) {
	f := newFixture(t)
	review, err := f.service.Review(context.Background(), "tenant-a", "agent-a", 1, "reviewer-a", f.manifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	if review.Digest != "sha256:a8ae1ba9f14eda85f3d139cafc5b7546df2839e51cdc8c6865a0ba8b92816f3e" {
		t.Fatalf("review seal changed: %s", review.Digest)
	}
}

func TestTodo_AGENT_009_Recovery(t *testing.T) {
	f := newFixture(t)
	review, err := f.service.Review(context.Background(), "tenant-a", "agent-a", 1, "reviewer-a", f.manifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.service.Publish(context.Background(), "tenant-a", "agent-a", 1, 0, f.manifestDigest, f.evalDigest, review.Digest)
	if err != nil {
		t.Fatal(err)
	}
	secondManifest := testManifest()
	secondManifest.Version = 2
	f.manifests.versions[2] = secondManifest
	secondDigest, err := secondManifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	f.evaluations.records[digest("eval-v2")] = Evaluation{Digest: digest("eval-v2"), ManifestDigest: secondDigest, Passed: true, Fresh: true}
	secondReview, err := f.service.Review(context.Background(), "tenant-a", "agent-a", 2, "reviewer-a", secondDigest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Publish(context.Background(), "tenant-a", "agent-a", 2, first.Generation, secondDigest, digest("eval-v2"), secondReview.Digest); err != nil {
		t.Fatal(err)
	}
	recovered, err := f.service.Rollback(context.Background(), "tenant-a", "agent-a", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.Rollback || recovered.ManifestDigest != f.manifestDigest || recovered.Generation != 3 {
		t.Fatalf("rollback did not recover exact reviewed version: %+v", recovered)
	}
	f.evaluations.records[f.evalDigest] = Evaluation{Digest: f.evalDigest, ManifestDigest: f.manifestDigest, Passed: true, Fresh: false}
	if _, err := f.service.Rollback(context.Background(), "tenant-a", "agent-a", 1, 3); !errors.Is(err, ErrEvaluation) {
		t.Fatalf("rollback with stale evaluation error = %v", err)
	}
	f.evaluations.records[f.evalDigest] = Evaluation{Digest: f.evalDigest, ManifestDigest: f.manifestDigest, Passed: true, Fresh: true}
	f.eligibility.err = errors.New("pinned tool retired")
	if _, err := f.service.Rollback(context.Background(), "tenant-a", "agent-a", 1, 3); !errors.Is(err, ErrIneligible) {
		t.Fatalf("rollback with retired tool error = %v", err)
	}
}
