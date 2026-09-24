// Package intelligencestore persists typed intelligence metric publications.
package intelligencestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
)

var (
	ErrInvalid  = errors.New("intelligencestore: invalid publication")
	ErrConflict = errors.New("intelligencestore: publication conflicts with existing identity")
)

type DB interface{ dbport.Beginner }

// Publication is one immutable calculation and its observed decision link.
type Publication struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	MetricID    string
	Version     string
	DecisionID  uuid.UUID
	Result      intelligence.MetricResult
	Outcome     intelligence.OutcomeLink
	PublishedAt time.Time
}

type Store struct{ db DB }

func New(db DB) *Store { return &Store{db: db} }

// Publish atomically persists the typed metric result and outcome link under
// the tenant RLS context. Exact retries return the existing publication.
func (s *Store) Publish(ctx context.Context, in Publication) (Publication, error) {
	if s == nil || s.db == nil || ctx == nil || in.TenantID == uuid.Nil || in.ID == uuid.Nil || in.DecisionID == uuid.Nil ||
		strings.TrimSpace(in.MetricID) == "" || strings.TrimSpace(in.Version) == "" ||
		in.Result.CalculationDigest == "" || in.Result.PopulationDigest == "" ||
		in.Outcome.Digest == "" || in.Outcome.Tenant != in.TenantID.String() || in.Outcome.DecisionRef != in.DecisionID.String() || in.PublishedAt.IsZero() {
		return Publication{}, ErrInvalid
	}
	result, err := json.Marshal(in.Result)
	if err != nil {
		return Publication{}, fmt.Errorf("intelligencestore: encode result: %w", err)
	}
	outcome, err := json.Marshal(in.Outcome)
	if err != nil {
		return Publication{}, fmt.Errorf("intelligencestore: encode outcome: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Publication{}, fmt.Errorf("intelligencestore: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, in.TenantID); err != nil {
		return Publication{}, err
	}
	var storedID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO intelligence_metric_publication (
		 tenant_id, publication_id, metric_id, metric_version, decision_id,
		 calculation_digest, outcome_digest, metric_result, outcome_link, published_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10)
		ON CONFLICT (tenant_id, calculation_digest, outcome_digest) DO NOTHING
		RETURNING publication_id`, in.TenantID, in.ID, in.MetricID, in.Version, in.DecisionID,
		in.Result.CalculationDigest, in.Outcome.Digest, result, outcome, in.PublishedAt.UTC()).Scan(&storedID)
	if errors.Is(err, dbport.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT publication_id FROM intelligence_metric_publication
			WHERE tenant_id=$1 AND calculation_digest=$2 AND outcome_digest=$3`,
			in.TenantID, in.Result.CalculationDigest, in.Outcome.Digest).Scan(&storedID)
		if err == nil {
			in.ID = storedID
		}
	}
	if err != nil {
		return Publication{}, fmt.Errorf("intelligencestore: publish: %w", err)
	}
	if storedID != in.ID {
		return Publication{}, ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return Publication{}, fmt.Errorf("intelligencestore: commit: %w", err)
	}
	in.ID, in.PublishedAt = storedID, in.PublishedAt.UTC()
	return in, nil
}
