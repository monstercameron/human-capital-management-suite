// Package publication owns agent version review, immutable publication evidence,
// rollback validation, and the quarantine admission boundary. Persistence,
// reviewer authorization, manifest lookup, evaluation, and eligibility are
// supplied through narrow ports; this package does not issue approvals or
// manufacture evaluation results.
package publication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
)

var (
	ErrInvalid       = errors.New("agent publication: invalid request")
	ErrNotFound      = errors.New("agent publication: evidence not found")
	ErrStaleManifest = errors.New("agent publication: manifest digest is stale")
	ErrReview        = errors.New("agent publication: independent review required")
	ErrEvaluation    = errors.New("agent publication: exact fresh passing evaluation required")
	ErrIneligible    = errors.New("agent publication: version is no longer eligible")
	ErrQuarantined   = errors.New("agent publication: version is quarantined")
	ErrWriteFence    = errors.New("agent publication: write-capable work could not be fenced")
	ErrConflict      = errors.New("agent publication: publication generation conflict")
)

const AgentReviewPermission = "agents.publish"

// Evaluation is a resolver-backed, immutable result bound to one manifest.
// Freshness is computed by the resolver against current policy, not by callers.
type Evaluation struct {
	Digest         string
	ManifestDigest string
	Passed         bool
	Fresh          bool
}

// Review is immutable approval evidence for an exact manifest digest.
type Review struct {
	TenantID       string
	AgentID        string
	ManifestDigest string
	ReviewerID     string
	Permission     string
	Digest         string
}

// Publication records the evidence pinned by a publish or rollback pointer.
type Publication struct {
	TenantID         string
	AgentID          string
	ManifestVersion  uint64
	ManifestDigest   string
	EvaluationDigest string
	ReviewDigest     string
	ReviewerID       string
	Generation       uint64
	Rollback         bool
}

// ManifestResolver loads the immutable manifest stored for the requested version.
type ManifestResolver interface {
	ResolveManifest(context.Context, string, string, uint64) (agentmanifest.Manifest, error)
}

// ReviewAuthority resolves current authority; clients cannot submit an allow bit.
type ReviewAuthority interface {
	AuthorizeAgentReviewer(context.Context, string, string, string) error
}

// EvaluationResolver resolves an immutable evaluation and current freshness.
type EvaluationResolver interface {
	ResolveEvaluation(context.Context, string) (Evaluation, error)
}

// Eligibility rechecks current tool, source, model, and policy pins.
type Eligibility interface {
	ValidateAgentVersion(context.Context, agentmanifest.Manifest) error
}

// QuarantineEffects fences already-issued write-capable leases after the
// durable admission fence is recorded.
type QuarantineEffects interface {
	FenceAgentWrites(context.Context, string, string, string) error
}

// Repository persists review evidence, immutable publication history, and the
// current pointer. Publish must atomically compare generation, append history,
// and advance the pointer. Admit must atomically reject quarantine and return
// the publication matching the requested pinned manifest digest. Quarantine
// must confirm that digest has a published record and persist its admission
// fence atomically. Publish must reject a quarantined digest in the same
// transaction that advances the pointer.
type Repository interface {
	StoreReview(context.Context, Review) error
	ResolveReview(context.Context, string, string, string) (Review, error)
	ResolvePublication(context.Context, string, string, uint64) (Publication, error)
	Publish(context.Context, uint64, Publication) (Publication, error)
	Quarantine(context.Context, string, string, string, string) error
	Admit(context.Context, string, string, string) (Publication, error)
}

// Dependencies are trusted authorization and evidence boundaries.
type Dependencies struct {
	Manifests   ManifestResolver
	Reviews     ReviewAuthority
	Evaluations EvaluationResolver
	Eligibility Eligibility
	Quarantine  QuarantineEffects
	Repository  Repository
}

// Service coordinates governed publication without owning storage or policy.
type Service struct{ deps Dependencies }

// New constructs the publication service only when every fail-closed boundary exists.
func New(deps Dependencies) (*Service, error) {
	if deps.Manifests == nil || deps.Reviews == nil || deps.Evaluations == nil || deps.Eligibility == nil || deps.Quarantine == nil || deps.Repository == nil {
		return nil, fmt.Errorf("%w: all publication dependencies are required", ErrInvalid)
	}
	return &Service{deps: deps}, nil
}

// Review authorizes an independent reviewer and stores a seal over the exact
// immutable manifest. Any manifest change therefore requires a new review.
func (s *Service) Review(ctx context.Context, tenantID, agentID string, version uint64, reviewerID, expectedDigest string) (Review, error) {
	manifest, digest, err := s.resolveManifest(ctx, tenantID, agentID, version, expectedDigest)
	if err != nil {
		return Review{}, err
	}
	if strings.TrimSpace(reviewerID) == "" || reviewerID == manifest.OwnerID {
		return Review{}, ErrReview
	}
	if err := s.deps.Reviews.AuthorizeAgentReviewer(ctx, tenantID, agentID, reviewerID); err != nil {
		return Review{}, fmt.Errorf("%w: %v", ErrReview, err)
	}
	review := Review{TenantID: tenantID, AgentID: agentID, ManifestDigest: digest, ReviewerID: reviewerID, Permission: AgentReviewPermission}
	review.Digest = reviewDigest(review)
	if err := s.deps.Repository.StoreReview(ctx, review); err != nil {
		return Review{}, fmt.Errorf("store review: %w", err)
	}
	return review, nil
}

// Publish pins a manifest and its exact fresh passing evaluation after current
// reviewer authority and eligibility are rechecked. The repository performs CAS.
func (s *Service) Publish(ctx context.Context, tenantID, agentID string, version, expectedGeneration uint64, expectedManifestDigest, evaluationDigest, reviewDigestValue string) (Publication, error) {
	manifest, digest, err := s.resolveManifest(ctx, tenantID, agentID, version, expectedManifestDigest)
	if err != nil {
		return Publication{}, err
	}
	review, err := s.resolveReview(ctx, tenantID, agentID, digest, reviewDigestValue, manifest.OwnerID)
	if err != nil {
		return Publication{}, err
	}
	if err := s.validateEvidence(ctx, manifest, digest, evaluationDigest); err != nil {
		return Publication{}, err
	}
	publication := Publication{TenantID: tenantID, AgentID: agentID, ManifestVersion: version, ManifestDigest: digest, EvaluationDigest: evaluationDigest, ReviewDigest: review.Digest, ReviewerID: review.ReviewerID}
	return s.deps.Repository.Publish(ctx, expectedGeneration, publication)
}

// Rollback moves the pointer to an already published version only after its
// manifest, reviewer authority, evaluation freshness, and eligibility revalidate.
func (s *Service) Rollback(ctx context.Context, tenantID, agentID string, version, expectedGeneration uint64) (Publication, error) {
	prior, err := s.deps.Repository.ResolvePublication(ctx, tenantID, agentID, version)
	if err != nil {
		return Publication{}, fmt.Errorf("%w: publication: %v", ErrNotFound, err)
	}
	manifest, digest, err := s.resolveManifest(ctx, tenantID, agentID, version, prior.ManifestDigest)
	if err != nil {
		return Publication{}, err
	}
	review, err := s.resolveReview(ctx, tenantID, agentID, digest, prior.ReviewDigest, manifest.OwnerID)
	if err != nil {
		return Publication{}, err
	}
	if err := s.validateEvidence(ctx, manifest, digest, prior.EvaluationDigest); err != nil {
		return Publication{}, err
	}
	prior.ReviewerID = review.ReviewerID
	prior.Generation = 0
	prior.Rollback = true
	return s.deps.Repository.Publish(ctx, expectedGeneration, prior)
}

// Quarantine closes admission for the exact published manifest digest.
func (s *Service) Quarantine(ctx context.Context, tenantID, agentID, manifestDigest, reason string) error {
	if !validDigest(manifestDigest) || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(agentID) == "" || strings.TrimSpace(reason) == "" {
		return ErrInvalid
	}
	if err := s.deps.Repository.Quarantine(ctx, tenantID, agentID, manifestDigest, reason); err != nil {
		return err
	}
	if err := s.deps.Quarantine.FenceAgentWrites(ctx, tenantID, agentID, manifestDigest); err != nil {
		return fmt.Errorf("%w: %v", ErrWriteFence, err)
	}
	return nil
}

// Admit returns exact publication evidence only if the repository's atomic
// quarantine fence still permits new work for this pinned version.
func (s *Service) Admit(ctx context.Context, tenantID, agentID, manifestDigest string) (Publication, error) {
	if !validDigest(manifestDigest) || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(agentID) == "" {
		return Publication{}, ErrInvalid
	}
	return s.deps.Repository.Admit(ctx, tenantID, agentID, manifestDigest)
}

func (s *Service) resolveManifest(ctx context.Context, tenantID, agentID string, version uint64, expected string) (agentmanifest.Manifest, string, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(agentID) == "" || version == 0 || !validDigest(expected) {
		return agentmanifest.Manifest{}, "", ErrInvalid
	}
	manifest, err := s.deps.Manifests.ResolveManifest(ctx, tenantID, agentID, version)
	if err != nil {
		return agentmanifest.Manifest{}, "", fmt.Errorf("%w: manifest: %v", ErrNotFound, err)
	}
	actual, err := manifest.Digest()
	if err != nil {
		return agentmanifest.Manifest{}, "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if manifest.ID != agentID || manifest.Version != version || actual != expected {
		return agentmanifest.Manifest{}, "", ErrStaleManifest
	}
	return manifest, actual, nil
}

func (s *Service) resolveReview(ctx context.Context, tenantID, agentID, digest, expected, owner string) (Review, error) {
	if !validDigest(expected) {
		return Review{}, ErrReview
	}
	review, err := s.deps.Repository.ResolveReview(ctx, tenantID, agentID, expected)
	if err != nil || review.TenantID != tenantID || review.AgentID != agentID || review.ManifestDigest != digest || review.Digest != expected || review.Permission != AgentReviewPermission || strings.TrimSpace(review.ReviewerID) == "" || review.ReviewerID == owner || reviewDigest(review) != expected {
		return Review{}, ErrReview
	}
	if err := s.deps.Reviews.AuthorizeAgentReviewer(ctx, tenantID, agentID, review.ReviewerID); err != nil {
		return Review{}, fmt.Errorf("%w: %v", ErrReview, err)
	}
	return review, nil
}

func (s *Service) validateEvidence(ctx context.Context, manifest agentmanifest.Manifest, digest, evaluationDigest string) error {
	if !validDigest(evaluationDigest) {
		return ErrEvaluation
	}
	if err := s.deps.Eligibility.ValidateAgentVersion(ctx, manifest); err != nil {
		return fmt.Errorf("%w: %v", ErrIneligible, err)
	}
	evaluation, err := s.deps.Evaluations.ResolveEvaluation(ctx, evaluationDigest)
	if err != nil || evaluation.Digest != evaluationDigest || evaluation.ManifestDigest != digest || !evaluation.Passed || !evaluation.Fresh {
		return ErrEvaluation
	}
	return nil
}

func reviewDigest(review Review) string {
	bound := struct {
		TenantID, AgentID, ManifestDigest, ReviewerID, Permission string
	}{review.TenantID, review.AgentID, review.ManifestDigest, review.ReviewerID, review.Permission}
	data, _ := json.Marshal(bound)
	sum := sha256.Sum256(append([]byte("hcm-next-agent-review/v1\x00"), data...))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(decoded) == sha256.Size
}
