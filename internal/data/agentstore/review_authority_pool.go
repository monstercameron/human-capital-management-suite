package agentstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// PersonaReviewAuthorityRole is the non-login role selected by the dedicated
// persona-review authority service pool.
const PersonaReviewAuthorityRole = "hcmnext_persona_review_authority"

// PersonaReviewAuthorityConfig isolates the review writer credential from
// every other data-plane credential while keeping its pool on the agent DB.
type PersonaReviewAuthorityConfig struct {
	DSN, AgentDSN, CoreDSN, ChatDSN, DocumentDSN string
	MaxConns, MinConns                           int32
}

// PersonaReviewAuthorityStore is the restricted database pool used only by
// the trusted persona-review evidence issuer.
type PersonaReviewAuthorityStore struct{ pool *pgxadapter.Pool }

// NewPersonaReviewAuthorityStore opens a pool that selects the dedicated
// evidence-issuer role after authenticating with its separately managed login.
func NewPersonaReviewAuthorityStore(ctx context.Context, cfg PersonaReviewAuthorityConfig) (*PersonaReviewAuthorityStore, error) {
	if ctx == nil || strings.TrimSpace(cfg.DSN) == "" || strings.TrimSpace(cfg.AgentDSN) == "" || strings.TrimSpace(cfg.CoreDSN) == "" {
		return nil, fmt.Errorf("%w: review authority, agent, and core DSNs are required", ErrInvalidConfig)
	}
	if !sameDatabase(cfg.DSN, cfg.AgentDSN) {
		return nil, fmt.Errorf("%w: review authority must use the isolated agent database", ErrInvalidConfig)
	}
	if sameCredential(cfg.DSN, cfg.AgentDSN) {
		return nil, ErrSharedCredential
	}
	for _, other := range []string{cfg.CoreDSN, cfg.ChatDSN, cfg.DocumentDSN} {
		if strings.TrimSpace(other) == "" {
			continue
		}
		if sameDatabase(cfg.DSN, other) {
			return nil, ErrSharedDatabase
		}
		if sameCredential(cfg.DSN, other) {
			return nil, ErrSharedCredential
		}
	}
	dsn, err := withPoolSize(cfg.DSN, cfg.MaxConns, cfg.MinConns)
	if err != nil {
		return nil, err
	}
	pool, err := pgxadapter.NewPool(ctx, dsn, map[string]string{"role": PersonaReviewAuthorityRole})
	if err != nil {
		return nil, fmt.Errorf("agentstore: open persona review authority pool: %w", err)
	}
	return &PersonaReviewAuthorityStore{pool: pool}, nil
}

// Begin opens a transaction for one review evidence operation.
func (s *PersonaReviewAuthorityStore) Begin(ctx context.Context) (dbport.Tx, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("agentstore: persona review authority pool unavailable")
	}
	return s.pool.Begin(ctx)
}

// Close closes the dedicated review authority pool.
func (s *PersonaReviewAuthorityStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

var _ dbport.Beginner = (*PersonaReviewAuthorityStore)(nil)
