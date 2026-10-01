package application

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var ErrPersonaEvaluationEvidenceUnavailable = errors.New("application: persona evaluation evidence is unavailable")

// PersonaPublicationEvidenceConfig contains trusted production dependencies
// for review, signed evaluation persistence, and transactional publication.
type PersonaPublicationEvidenceConfig struct {
	DB                   agentpersonastore.DB
	TenantUUID           func(values.TenantId) uuid.UUID
	EvaluationPublicKeys map[string]ed25519.PublicKey
	Now                  func() time.Time
}

// PersonaPublicationEvidenceServices share one durable tenant-scoped store
// between evidence recording and publication resolution.
type PersonaPublicationEvidenceServices struct {
	Publication *PersonaPublicationService
	Evaluations *PersonaEvaluationEvidenceService
}

// PersonaEvaluationEvidenceService persists evaluator-signed results. It
// accepts no pass/fresh booleans; the store verifies the signature and claim.
type PersonaEvaluationEvidenceService struct{ store *agentpersonastore.Store }

// Record retains one trusted evaluation claim under the tenant named by its
// caller and signed payload.
func (s *PersonaEvaluationEvidenceService) Record(ctx context.Context, tenant values.TenantId, evidence agentpersonastore.SignedPersonaEvaluation) error {
	if s == nil || s.store == nil || ctx == nil || tenant.Validate() != nil || evidence.Claim.TenantID != string(tenant) {
		return ErrPersonaEvaluationEvidenceUnavailable
	}
	if err := s.store.RecordPersonaEvaluation(ctx, tenant, evidence); err != nil {
		return fmt.Errorf("application: record persona evaluation evidence: %w", err)
	}
	return nil
}

// NewPersonaPublicationEvidenceServices builds production publication and
// evidence-writing services over durable review and signed evaluation
// authorities. Missing key material or storage dependencies fail closed.
func NewPersonaPublicationEvidenceServices(cfg PersonaPublicationEvidenceConfig) (*PersonaPublicationEvidenceServices, error) {
	if cfg.DB == nil || cfg.TenantUUID == nil || cfg.Now == nil || len(cfg.EvaluationPublicKeys) == 0 {
		return nil, ErrPersonaEvaluationEvidenceUnavailable
	}
	reviews, err := agentpersonastore.NewDurableReviewAuthority(cfg.TenantUUID)
	if err != nil {
		return nil, fmt.Errorf("application: configure persona review authority: %w", err)
	}
	evaluations, err := agentpersonastore.NewEvaluationSealAuthority(cfg.EvaluationPublicKeys, cfg.TenantUUID, cfg.Now)
	if err != nil {
		return nil, fmt.Errorf("application: configure persona evaluation authority: %w", err)
	}
	store, err := agentpersonastore.NewWithPublicationAuthorities(cfg.DB, cfg.TenantUUID, reviews, evaluations)
	if err != nil {
		return nil, fmt.Errorf("application: configure durable persona publication store: %w", err)
	}
	publication, err := NewPersonaPublicationService(store)
	if err != nil {
		return nil, err
	}
	return &PersonaPublicationEvidenceServices{
		Publication: publication,
		Evaluations: &PersonaEvaluationEvidenceService{store: store},
	}, nil
}
