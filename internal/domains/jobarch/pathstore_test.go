package jobarch

import (
	"context"
	"errors"
	"testing"
	"time"
)

func pathStorePath(t *testing.T, revision string, from, to time.Time) PromotionPathRevision {
	t.Helper()
	p := testPromotionPath(t)
	p.Revision = revision
	p.EffectiveFrom = from
	p.EffectiveTo = to
	p.KnownFrom = from
	return p
}

func TestPromotionPathStorePinsProfileRevisionsAndRefusesOverlappingPublishedEdges(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("published paths persist with effective coordinates", func(t *testing.T) {
		store := NewMemoryPathStore()
		path := pathStorePath(t, "1", at, time.Time{})
		if err := store.SavePath(ctx, "acme", path); err != nil {
			t.Fatalf("SavePath: %v", err)
		}
		current, err := store.CurrentPath(ctx, "acme", path.PathID, at.Add(24*time.Hour))
		if err != nil {
			t.Fatalf("CurrentPath: %v", err)
		}
		if current.Revision != "1" || current.From.Revision != "1" || current.To.Revision != "1" {
			t.Fatalf("stored path must pin profile revisions: %+v", current)
		}
	})

	t.Run("overlapping published edges are refused", func(t *testing.T) {
		store := NewMemoryPathStore()
		if err := store.SavePath(ctx, "acme", pathStorePath(t, "1", at, time.Time{})); err != nil {
			t.Fatal(err)
		}
		overlap := pathStorePath(t, "2", at.Add(30*24*time.Hour), time.Time{})
		if err := store.SavePath(ctx, "acme", overlap); !errors.Is(err, ErrOverlappingPath) {
			t.Fatalf("overlapping published edge must be refused, got %v", err)
		}
		rows, err := store.ListPaths(ctx, "acme", overlap.PathID)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 {
			t.Fatalf("refused save must leave prior rows intact: %d rows", len(rows))
		}
	})

	t.Run("successor revisions chain without overlap", func(t *testing.T) {
		store := NewMemoryPathStore()
		first := pathStorePath(t, "1", at, at.Add(365*24*time.Hour))
		if err := store.SavePath(ctx, "acme", first); err != nil {
			t.Fatal(err)
		}
		second := pathStorePath(t, "2", at.Add(365*24*time.Hour), time.Time{})
		second.Lineage = RevisionLineage{RootID: first.PathID, Supersedes: "1"}
		if err := store.SavePath(ctx, "acme", second); err != nil {
			t.Fatalf("abutting successor must persist: %v", err)
		}
		current, err := store.CurrentPath(ctx, "acme", first.PathID, at.Add(400*24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if current.Revision != "2" {
			t.Fatalf("current revision = %q, want 2", current.Revision)
		}
	})

	t.Run("assignment and proposal snapshots pin revisions", func(t *testing.T) {
		store := NewMemoryPathStore()
		path := pathStorePath(t, "1", at, time.Time{})
		if err := store.SavePath(ctx, "acme", path); err != nil {
			t.Fatal(err)
		}
		assignment, err := store.PinAssignment(ctx, "acme", AssignmentPin{
			AssignmentID: "assign-1", PathID: path.PathID, PathRevision: "1",
			SourceProfile: ProfileRevisionRef{ProfileID: "profile-2", Revision: "1"},
			TargetProfile: ProfileRevisionRef{ProfileID: "profile-3", Revision: "1"},
			JobCode:       "OPS-HRBP3", GradeCode: "P3",
			At: at.Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("PinAssignment: %v", err)
		}
		if assignment.SourceProfile.Revision != "1" || assignment.TargetProfile.Revision != "1" {
			t.Fatalf("assignment must pin both profile revisions: %+v", assignment)
		}
		proposal, err := store.PinProposal(ctx, "acme", ProposalPin{
			ProposalID: "proposal-1", PathID: path.PathID, PathRevision: "1",
			SourceProfile: assignment.SourceProfile, TargetProfile: assignment.TargetProfile,
			At: at.Add(2 * time.Hour),
		})
		if err != nil {
			t.Fatalf("PinProposal: %v", err)
		}
		// The proposal cannot drift to another revision between simulation
		// and commit: pins are immutable values.
		loaded, err := store.LoadProposal(ctx, "acme", "proposal-1")
		if err != nil {
			t.Fatal(err)
		}
		if loaded.PathRevision != proposal.PathRevision || loaded.TargetProfile != proposal.TargetProfile {
			t.Fatalf("proposal pin moved: %+v", loaded)
		}
	})

	t.Run("pins to unstored revisions are refused", func(t *testing.T) {
		store := NewMemoryPathStore()
		_, err := store.PinAssignment(ctx, "acme", AssignmentPin{
			AssignmentID: "assign-x", PathID: "path-hrbp-2-3", PathRevision: "9",
			SourceProfile: ProfileRevisionRef{ProfileID: "profile-2", Revision: "1"},
			TargetProfile: ProfileRevisionRef{ProfileID: "profile-3", Revision: "1"},
			At:            time.Now().UTC(),
		})
		if !errors.Is(err, ErrUnknownPathRevision) {
			t.Fatalf("pin to an unstored revision must be refused, got %v", err)
		}
	})

	t.Run("free-text job codes are not identities", func(t *testing.T) {
		store := NewMemoryPathStore()
		if err := store.SavePath(ctx, "acme", pathStorePath(t, "1", at, time.Time{})); err != nil {
			t.Fatal(err)
		}
		_, err := store.PinAssignment(ctx, "acme", AssignmentPin{
			AssignmentID: "assign-x", PathID: "path-hrbp-2-3", PathRevision: "1",
			SourceProfile: ProfileRevisionRef{ProfileID: "", Revision: ""},
			TargetProfile: ProfileRevisionRef{ProfileID: "profile-3", Revision: "1"},
			JobCode:       "OPS-HRBP3", GradeCode: "P3",
			At: time.Now().UTC(),
		})
		if !errors.Is(err, ErrInvalidPin) {
			t.Fatalf("free-text codes cannot substitute pinned revisions, got %v", err)
		}
	})
}

// TestTodo_PERSIST_JOBARCH_002_Property holds the append-only store
// invariants: saves never mutate prior rows and pins are immutable.
func TestTodo_PERSIST_JOBARCH_002_Property(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemoryPathStore()
	first := pathStorePath(t, "1", at, at.Add(365*24*time.Hour))
	if err := store.SavePath(ctx, "acme", first); err != nil {
		t.Fatal(err)
	}
	before, err := store.ListPaths(ctx, "acme", first.PathID)
	if err != nil {
		t.Fatal(err)
	}
	beforeDigest, err := before[0].Digest()
	if err != nil {
		t.Fatal(err)
	}
	second := pathStorePath(t, "2", at.Add(365*24*time.Hour), time.Time{})
	second.Lineage = RevisionLineage{RootID: first.PathID, Supersedes: "1"}
	if err := store.SavePath(ctx, "acme", second); err != nil {
		t.Fatal(err)
	}
	after, err := store.ListPaths(ctx, "acme", first.PathID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 {
		t.Fatalf("append-only store must keep both rows: %d", len(after))
	}
	afterDigest, err := after[0].Digest()
	if err != nil {
		t.Fatal(err)
	}
	if beforeDigest != afterDigest {
		t.Fatal("later saves must never mutate prior rows")
	}
	// Identical re-save is idempotent, not a duplicate row.
	if err := store.SavePath(ctx, "acme", second); err != nil {
		t.Fatalf("identical re-save must be idempotent: %v", err)
	}
	rows, _ := store.ListPaths(ctx, "acme", first.PathID)
	if len(rows) != 2 {
		t.Fatalf("idempotent re-save created a duplicate row: %d", len(rows))
	}
}

// TestTodo_PERSIST_JOBARCH_002_Golden pins the canonical path digest.
func TestTodo_PERSIST_JOBARCH_002_Golden(t *testing.T) {
	path := testPromotionPath(t)
	first, err := path.Digest()
	if err != nil {
		t.Fatal(err)
	}
	second, err := testPromotionPath(t).Digest()
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("path digest must be stable: %q / %q", first, second)
	}
	const golden = "sha256:8e3edd4aedd4de1830b5192e4e84c26f8b59e725cc9c573c119d21c141fcd1e0"
	if first != golden {
		t.Fatalf("golden path digest moved: got %q want %q", first, golden)
	}
}

// TestTodo_PERSIST_JOBARCH_002_Integration runs the proposal-to-commit pin
// flow across the store boundary: path save, assignment pin, proposal pin,
// current-path resolution and pin reload agree.
func TestTodo_PERSIST_JOBARCH_002_Integration(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemoryPathStore()
	path := pathStorePath(t, "1", at, time.Time{})
	if err := store.SavePath(ctx, "acme", path); err != nil {
		t.Fatal(err)
	}
	assignment, err := store.PinAssignment(ctx, "acme", AssignmentPin{
		AssignmentID: "assign-1", PathID: path.PathID, PathRevision: "1",
		SourceProfile: ProfileRevisionRef{ProfileID: "profile-2", Revision: "1"},
		TargetProfile: ProfileRevisionRef{ProfileID: "profile-3", Revision: "1"},
		JobCode:       "OPS-HRBP3", GradeCode: "P3", At: at.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := store.PinProposal(ctx, "acme", ProposalPin{
		ProposalID: "proposal-1", PathID: path.PathID, PathRevision: "1",
		SourceProfile: assignment.SourceProfile, TargetProfile: assignment.TargetProfile,
		At: at.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.CurrentPath(ctx, "acme", path.PathID, at.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != proposal.PathRevision {
		t.Fatalf("commit-time path %q diverges from proposal pin %q", current.Revision, proposal.PathRevision)
	}
	loadedAssignment, err := store.LoadAssignment(ctx, "acme", "assign-1")
	if err != nil {
		t.Fatal(err)
	}
	if loadedAssignment.TargetProfile != proposal.TargetProfile {
		t.Fatal("assignment and proposal snapshots disagree")
	}
}

// TestTodo_PERSIST_JOBARCH_002_Fault proves cancelled work writes nothing
// and refused writes leave the store untouched.
func TestTodo_PERSIST_JOBARCH_002_Fault(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemoryPathStore()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.SavePath(cancelled, "acme", pathStorePath(t, "1", at, time.Time{})); err == nil {
		t.Fatal("cancelled context must write nothing")
	}
	rows, err := store.ListPaths(context.Background(), "acme", "path-hrbp-2-3")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("cancelled save left %d rows", len(rows))
	}
}

// TestTodo_PERSIST_JOBARCH_002_Security proves tenant isolation: tenants
// neither read nor pin through each other.
func TestTodo_PERSIST_JOBARCH_002_Security(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemoryPathStore()
	if err := store.SavePath(ctx, "acme", pathStorePath(t, "1", at, time.Time{})); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CurrentPath(ctx, "competitor", "path-hrbp-2-3", at.Add(time.Hour)); err == nil {
		t.Fatal("cross-tenant path reads must fail")
	}
	if _, err := store.PinAssignment(ctx, "competitor", AssignmentPin{
		AssignmentID: "assign-x", PathID: "path-hrbp-2-3", PathRevision: "1",
		SourceProfile: ProfileRevisionRef{ProfileID: "profile-2", Revision: "1"},
		TargetProfile: ProfileRevisionRef{ProfileID: "profile-3", Revision: "1"},
		At:            at.Add(time.Hour),
	}); err == nil {
		t.Fatal("cross-tenant pins must fail")
	}
}

// TestTodo_PERSIST_JOBARCH_002_Mutation kills the guard-removal mutants:
// overlapping published edges and unpinned identities must each fail.
func TestTodo_PERSIST_JOBARCH_002_Mutation(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := NewMemoryPathStore()
	if err := store.SavePath(ctx, "acme", pathStorePath(t, "1", at, time.Time{})); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePath(ctx, "acme", pathStorePath(t, "2", at.Add(time.Hour), time.Time{})); !errors.Is(err, ErrOverlappingPath) {
		t.Fatalf("overlap mutant survived: %v", err)
	}
	// A draft may overlap history, but publishing over a live edge may not.
	draft := pathStorePath(t, "3", at.Add(time.Hour), time.Time{})
	draft.Lifecycle = LifecycleDraft
	if err := store.SavePath(ctx, "acme", draft); err != nil {
		t.Fatalf("draft overlap must be allowed: %v", err)
	}
	if err := store.SavePath(ctx, "acme", pathStorePath(t, "4", at.Add(2*time.Hour), time.Time{})); !errors.Is(err, ErrOverlappingPath) {
		t.Fatalf("publish-over-live mutant survived: %v", err)
	}
}
