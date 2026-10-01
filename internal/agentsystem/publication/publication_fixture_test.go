package publication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
)

type fixture struct {
	service        *Service
	manifestDigest string
	evalDigest     string
	manifests      *manifestResolver
	authority      *reviewAuthority
	evaluations    *evaluationResolver
	repo           *memoryRepository
	quarantine     *quarantineEffects
	eligibility    *mutableEligibility
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	manifest := testManifest()
	manifestDigest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	evalDigest := digest("eval-v1")
	manifests := &manifestResolver{manifest: manifest, versions: map[uint64]agentmanifest.Manifest{1: manifest}}
	authority := &reviewAuthority{allowed: true}
	evaluations := &evaluationResolver{records: map[string]Evaluation{evalDigest: {Digest: evalDigest, ManifestDigest: manifestDigest, Passed: true, Fresh: true}}}
	repo := &memoryRepository{reviews: make(map[string]Review), publications: make(map[uint64]Publication), quarantined: make(map[string]bool)}
	quarantine := &quarantineEffects{}
	eligibility := &mutableEligibility{}
	service, err := New(Dependencies{Manifests: manifests, Reviews: authority, Evaluations: evaluations, Eligibility: eligibility, Quarantine: quarantine, Repository: repo})
	if err != nil {
		t.Fatal(err)
	}
	return fixture{service: service, manifestDigest: manifestDigest, evalDigest: evalDigest, manifests: manifests, authority: authority, evaluations: evaluations, repo: repo, quarantine: quarantine, eligibility: eligibility}
}

func publishedFixture(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t)
	review, err := f.service.Review(context.Background(), "tenant-a", "agent-a", 1, "reviewer-a", f.manifestDigest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Publish(context.Background(), "tenant-a", "agent-a", 1, 0, f.manifestDigest, f.evalDigest, review.Digest); err != nil {
		t.Fatal(err)
	}
	return f
}

func testManifest() agentmanifest.Manifest {
	return agentmanifest.Manifest{SchemaVersion: agentmanifest.CurrentSchemaVersion, ID: "agent-a", Version: 1, OwnerID: "owner-a", Purpose: "answer employee questions", InstructionsDigest: digest("instructions"), SourceCeiling: []agentmanifest.Reference{}, ToolCeiling: []agentmanifest.Reference{}, ModelPolicy: ref("model"), AutonomyCeiling: "T0", Budget: agentmanifest.Budget{MaxCostMicros: 100, MaxInputTokens: 1000, MaxOutputTokens: 1000, MaxConcurrentRuns: 1}, OutputSchema: ref("output"), ContextGrants: []agentmanifest.Reference{}, EvaluationRefs: []agentmanifest.Reference{ref("suite")}}
}

func ref(id string) agentmanifest.Reference {
	return agentmanifest.Reference{ID: id, Version: 1, SchemaVersion: 1, Digest: digest(id)}
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

type manifestResolver struct {
	manifest agentmanifest.Manifest
	versions map[uint64]agentmanifest.Manifest
}

func (r *manifestResolver) ResolveManifest(_ context.Context, tenant, agent string, version uint64) (agentmanifest.Manifest, error) {
	if tenant != "tenant-a" || agent != "agent-a" {
		return agentmanifest.Manifest{}, ErrNotFound
	}
	manifest, ok := r.versions[version]
	if !ok {
		return agentmanifest.Manifest{}, ErrNotFound
	}
	if version == 1 {
		return r.manifest, nil
	}
	return manifest, nil
}

type reviewAuthority struct{ allowed bool }

func (a *reviewAuthority) AuthorizeAgentReviewer(_ context.Context, tenant, agent, reviewer string) error {
	if a != nil && a.allowed && tenant == "tenant-a" && agent == "agent-a" && reviewer == "reviewer-a" {
		return nil
	}
	return ErrReview
}

type evaluationResolver struct{ records map[string]Evaluation }

func (r *evaluationResolver) ResolveEvaluation(_ context.Context, digest string) (Evaluation, error) {
	record, ok := r.records[digest]
	if !ok {
		return Evaluation{}, ErrEvaluation
	}
	return record, nil
}

type mutableEligibility struct{ err error }

func (e *mutableEligibility) ValidateAgentVersion(_ context.Context, manifest agentmanifest.Manifest) error {
	if e.err != nil {
		return e.err
	}
	return manifest.Validate()
}

type quarantineEffects struct {
	calls []string
	err   error
}

func (q *quarantineEffects) FenceAgentWrites(_ context.Context, tenant, agent, manifestDigest string) error {
	if tenant != "tenant-a" || agent != "agent-a" {
		return ErrInvalid
	}
	q.calls = append(q.calls, manifestDigest)
	return q.err
}

type memoryRepository struct {
	mu           sync.Mutex
	reviews      map[string]Review
	publications map[uint64]Publication
	history      []Publication
	quarantined  map[string]bool
	generation   uint64
	publishErr   error
}

func (r *memoryRepository) StoreReview(_ context.Context, review Review) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, exists := r.reviews[review.Digest]; exists && old != review {
		return ErrReview
	}
	r.reviews[review.Digest] = review
	return nil
}

func (r *memoryRepository) ResolveReview(_ context.Context, tenant, agent, digest string) (Review, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	review, ok := r.reviews[digest]
	if !ok || review.TenantID != tenant || review.AgentID != agent {
		return Review{}, ErrNotFound
	}
	return review, nil
}

func (r *memoryRepository) ResolvePublication(_ context.Context, tenant, agent string, version uint64) (Publication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	publication, ok := r.publications[version]
	if !ok || publication.TenantID != tenant || publication.AgentID != agent {
		return Publication{}, ErrNotFound
	}
	return publication, nil
}

func (r *memoryRepository) Publish(_ context.Context, expected uint64, publication Publication) (Publication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.publishErr != nil {
		return Publication{}, r.publishErr
	}
	if expected != r.generation {
		return Publication{}, ErrConflict
	}
	if r.quarantined[publication.TenantID+"\x00"+publication.AgentID+"\x00"+publication.ManifestDigest] {
		return Publication{}, ErrQuarantined
	}
	if !publication.Rollback {
		if prior, exists := r.publications[publication.ManifestVersion]; exists && prior.ManifestDigest != publication.ManifestDigest {
			return Publication{}, ErrConflict
		}
		if _, exists := r.publications[publication.ManifestVersion]; exists {
			return Publication{}, ErrConflict
		}
		r.publications[publication.ManifestVersion] = publication
	}
	r.generation++
	publication.Generation = r.generation
	r.history = append(r.history, publication)
	return publication, nil
}

func (r *memoryRepository) Quarantine(_ context.Context, tenant, agent, digest, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tenant == "" || agent == "" || reason == "" {
		return ErrInvalid
	}
	published := false
	for _, publication := range r.history {
		if publication.TenantID == tenant && publication.AgentID == agent && publication.ManifestDigest == digest {
			published = true
			break
		}
	}
	if !published {
		return ErrNotFound
	}
	r.quarantined[tenant+"\x00"+agent+"\x00"+digest] = true
	return nil
}

func (r *memoryRepository) Admit(_ context.Context, tenant, agent, digest string) (Publication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.quarantined[tenant+"\x00"+agent+"\x00"+digest] {
		return Publication{}, ErrQuarantined
	}
	for _, publication := range r.history {
		if publication.TenantID == tenant && publication.AgentID == agent && publication.ManifestDigest == digest {
			return publication, nil
		}
	}
	return Publication{}, ErrNotFound
}
