package releaseevidence_test

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/releaseevidence"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

var t0 = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

// shippedRelease is the promotion slice as this tree would ship it: the real
// migration artifact digest and target version and the real compiled plan
// digest.
func shippedRelease(t *testing.T) releaseevidence.Release {
	t.Helper()
	digest, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatal(err)
	}
	target, err := migrations.TargetVersion()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return releaseevidence.Release{
		Slice: "promotion", Version: "2026.09.14", BinaryRevision: "rev-abc123",
		SchemaDigest: digest, SchemaVersion: target,
		Definitions:  map[string]string{plan.WorkflowID: plan.Digest(), "hcmnext.people.promote_worker/v1": "sha256:intent-def"},
		EvidenceRefs: []string{"test:TestTodo_ALIGN_029", "test:TestTodo_WF_RUN_022"},
		RecordedBy:   "release:pipeline", RecordedAt: t0,
	}
}

// TestTodo_ALIGN_054 proves a release record binds the slice version to the
// binary, schema artifact, definitions and evidence it shipped with, is
// sealed by its digest, and cannot be re-recorded with different content.
func TestTodo_ALIGN_054(t *testing.T) {
	j := releaseevidence.NewJournal()
	rec, err := j.Record(shippedRelease(t))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Digest == "" || rec.Verify() != nil {
		t.Fatalf("sealed release = %+v", rec)
	}
	again, err := j.Record(shippedRelease(t))
	if err != nil || again.Digest != rec.Digest {
		t.Fatalf("identical re-record = %+v, %v", again, err)
	}
	altered := shippedRelease(t)
	altered.BinaryRevision = "rev-hotfix"
	if _, err := j.Record(altered); !errors.Is(err, releaseevidence.ErrImmutable) {
		t.Fatalf("rewriting a recorded release = %v", err)
	}
	latest, err := j.Latest("promotion")
	if err != nil || latest.Digest != rec.Digest || latest.BinaryRevision != "rev-abc123" {
		t.Fatalf("latest = %+v, %v", latest, err)
	}
}

// TestTodo_ALIGN_054_Property proves the digest is a function of content only:
// evidence order and time zone do not change it, and every bound field does.
func TestTodo_ALIGN_054_Property(t *testing.T) {
	base, err := shippedRelease(t).Seal()
	if err != nil {
		t.Fatal(err)
	}
	reordered := shippedRelease(t)
	reordered.EvidenceRefs = []string{reordered.EvidenceRefs[1], reordered.EvidenceRefs[0]}
	reordered.RecordedAt = t0.In(time.FixedZone("x", 3600))
	if r, _ := reordered.Seal(); r.Digest != base.Digest {
		t.Fatal("evidence order or zone changed the digest")
	}
	for name, mutate := range map[string]func(*releaseevidence.Release){
		"binary":     func(r *releaseevidence.Release) { r.BinaryRevision = "other" },
		"schema":     func(r *releaseevidence.Release) { r.SchemaDigest = "other" },
		"version":    func(r *releaseevidence.Release) { r.SchemaVersion++ },
		"definition": func(r *releaseevidence.Release) { r.Definitions["extra"] = "sha256:x" },
		"evidence":   func(r *releaseevidence.Release) { r.EvidenceRefs = append(r.EvidenceRefs, "test:X") },
		"slice":      func(r *releaseevidence.Release) { r.Version = "2026.09.15" },
	} {
		r := shippedRelease(t)
		mutate(&r)
		if s, _ := r.Seal(); s.Digest == base.Digest {
			t.Errorf("%s change left the digest unchanged", name)
		}
	}
}

// TestTodo_ALIGN_054_Golden pins the record encoding.
func TestTodo_ALIGN_054_Golden(t *testing.T) {
	r, err := releaseevidence.Release{Slice: "s", Version: "1", BinaryRevision: "b", SchemaDigest: "d", SchemaVersion: 7,
		Definitions: map[string]string{"wf": "sha256:p"}, EvidenceRefs: []string{"test:B", "test:A"}, RecordedBy: "pipeline", RecordedAt: t0}.Seal()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r)
	prefix := `{"slice":"s","version":"1","binary_revision":"b","schema_digest":"d","schema_version":7,"definitions":{"wf":"sha256:p"},"evidence_refs":["test:A","test:B"],"recorded_by":"pipeline","recorded_at":"2026-09-14T12:00:00Z","digest":"sha256:`
	if !strings.HasPrefix(string(b), prefix) {
		t.Fatalf("record = %s", b)
	}
}

// TestTodo_ALIGN_054_Security proves tampering is detected and incomplete
// evidence is refused.
func TestTodo_ALIGN_054_Security(t *testing.T) {
	rec, err := releaseevidence.NewJournal().Record(shippedRelease(t))
	if err != nil {
		t.Fatal(err)
	}
	tampered := rec
	tampered.Definitions = map[string]string{"hcmnext.workflows.promotion.execute": "sha256:swapped", "hcmnext.people.promote_worker/v1": "sha256:intent-def"}
	if tampered.Verify() == nil {
		t.Fatal("a tampered release verified")
	}
	for name, mutate := range map[string]func(*releaseevidence.Release){
		"no binary":      func(r *releaseevidence.Release) { r.BinaryRevision = "" },
		"no evidence":    func(r *releaseevidence.Release) { r.EvidenceRefs = nil },
		"no definitions": func(r *releaseevidence.Release) { r.Definitions = nil },
		"blank digest":   func(r *releaseevidence.Release) { r.Definitions = map[string]string{"wf": " "} },
		"zero schema":    func(r *releaseevidence.Release) { r.SchemaVersion = 0 },
		"no instant":     func(r *releaseevidence.Release) { r.RecordedAt = time.Time{} },
	} {
		r := shippedRelease(t)
		mutate(&r)
		if _, err := releaseevidence.NewJournal().Record(r); !errors.Is(err, releaseevidence.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestTodo_ALIGN_054_Integration binds the record to the real embedded
// migration artifact and compiled promotion plan, so a change to either
// changes what the release must say.
func TestTodo_ALIGN_054_Integration(t *testing.T) {
	r := shippedRelease(t)
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	if r.SchemaVersion != files[len(files)-1].Version {
		t.Fatalf("release schema version %d != embedded target %d", r.SchemaVersion, files[len(files)-1].Version)
	}
	plan, _ := promotionexec.Compile()
	if err := plan.Verify(); err != nil || r.Definitions[plan.WorkflowID] != plan.Digest() {
		t.Fatalf("release does not pin the verified compiled plan: %v", err)
	}
}

// TestTodo_ALIGN_054_Fault covers an empty journal and concurrent recorders.
func TestTodo_ALIGN_054_Fault(t *testing.T) {
	j := releaseevidence.NewJournal()
	if _, err := j.Latest("promotion"); !errors.Is(err, releaseevidence.ErrNotFound) {
		t.Fatalf("empty journal = %v", err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	digests := map[string]int{}
	for i := range 16 {
		wg.Go(func() {
			r := shippedRelease(t)
			if i%2 == 1 {
				r.BinaryRevision = "rev-racer"
			}
			if rec, err := j.Record(r); err == nil {
				mu.Lock()
				digests[rec.Digest]++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(digests) != 1 {
		t.Fatalf("concurrent recorders stored %d different releases for one version", len(digests))
	}
}

// TestTodo_ALIGN_054_Conformance proves Latest follows recording order across
// versions of one slice and ignores other slices.
func TestTodo_ALIGN_054_Conformance(t *testing.T) {
	j := releaseevidence.NewJournal()
	first := shippedRelease(t)
	second := shippedRelease(t)
	second.Version, second.BinaryRevision = "2026.09.15", "rev-def456"
	other := shippedRelease(t)
	other.Slice = "payroll"
	for _, r := range []releaseevidence.Release{first, second, other} {
		if _, err := j.Record(r); err != nil {
			t.Fatal(err)
		}
	}
	if latest, _ := j.Latest("promotion"); latest.Version != "2026.09.15" {
		t.Fatalf("latest promotion release = %s", latest.Version)
	}
}
