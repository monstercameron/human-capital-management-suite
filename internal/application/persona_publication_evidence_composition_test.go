package application

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaEvidenceCompositionDB struct{}

func (personaEvidenceCompositionDB) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("unexpected database access")
}

func TestNewPersonaPublicationEvidenceServices_RequiresVerifiedProductionDependencies(t *testing.T) {
	seed := sha256.Sum256([]byte("persona publication composition"))
	private := ed25519.NewKeyFromSeed(seed[:])
	cfg := PersonaPublicationEvidenceConfig{
		DB:                   personaEvidenceCompositionDB{},
		TenantUUID:           func(values.TenantId) uuid.UUID { return uuid.New() },
		EvaluationPublicKeys: map[string]ed25519.PublicKey{"eval-v1": private.Public().(ed25519.PublicKey)},
		Now:                  func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) },
	}
	services, err := NewPersonaPublicationEvidenceServices(cfg)
	if err != nil || services == nil || services.Publication == nil || services.Evaluations == nil {
		t.Fatalf("compose durable persona authorities = %#v, %v", services, err)
	}

	cases := []struct {
		name string
		edit func(*PersonaPublicationEvidenceConfig)
	}{
		{name: "missing database", edit: func(c *PersonaPublicationEvidenceConfig) { c.DB = nil }},
		{name: "missing tenant mapping", edit: func(c *PersonaPublicationEvidenceConfig) { c.TenantUUID = nil }},
		{name: "missing evaluator keys", edit: func(c *PersonaPublicationEvidenceConfig) { c.EvaluationPublicKeys = nil }},
		{name: "missing clock", edit: func(c *PersonaPublicationEvidenceConfig) { c.Now = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := cfg
			tc.edit(&bad)
			if got, err := NewPersonaPublicationEvidenceServices(bad); got != nil || !errors.Is(err, ErrPersonaEvaluationEvidenceUnavailable) {
				t.Fatalf("invalid composition = %#v, %v", got, err)
			}
		})
	}
}

func TestPersonaEvaluationEvidenceService_RejectsTenantSubstitution(t *testing.T) {
	service := &PersonaEvaluationEvidenceService{store: &agentpersonastore.Store{}}
	evidence := agentpersonastore.SignedPersonaEvaluation{Claim: agentpersonastore.PersonaEvaluationClaim{TenantID: "tenant-a"}}
	if err := service.Record(context.Background(), values.TenantId("tenant-b"), evidence); !errors.Is(err, ErrPersonaEvaluationEvidenceUnavailable) {
		t.Fatalf("cross-tenant evidence error = %v", err)
	}
	if err := (*PersonaEvaluationEvidenceService)(nil).Record(context.Background(), values.TenantId("tenant-a"), evidence); !errors.Is(err, ErrPersonaEvaluationEvidenceUnavailable) {
		t.Fatalf("nil service error = %v", err)
	}
}
