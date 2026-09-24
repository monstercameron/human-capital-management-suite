// Package i18ncatalogstore persists reviewed product catalog revisions and
// append-only activation events under tenant row-level security.
package i18ncatalogstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/i18n"
)

var errNotConfigured = errors.New("i18ncatalogstore: database is not configured")

// catalogRevisionPayload keeps the durable JSON contract independent of the
// kernel type's Go field names and matches the database's snake_case checks.
type catalogRevisionPayload struct {
	ID               string              `json:"id"`
	Locale           string              `json:"locale"`
	PreviousRevision string              `json:"previous_revision"`
	Version          string              `json:"version"`
	Fallbacks        map[string][]string `json:"fallbacks"`
	Translations     []i18n.Translation  `json:"translations"`
	CreatedAt        time.Time           `json:"created_at"`
	EffectiveFrom    time.Time           `json:"effective_from"`
	EffectiveUntil   time.Time           `json:"effective_until"`
	CanonicalDigest  string              `json:"canonical_digest"`
	Digest           string              `json:"digest"`
}

func encodeRevision(revision i18n.CatalogRevision) ([]byte, error) {
	return json.Marshal(catalogRevisionPayload{
		ID: revision.ID, Locale: revision.Locale, PreviousRevision: revision.PreviousRevision,
		Version: revision.Version, Fallbacks: revision.Fallbacks, Translations: revision.Translations,
		CreatedAt: revision.CreatedAt, EffectiveFrom: revision.EffectiveFrom,
		EffectiveUntil: revision.EffectiveUntil, CanonicalDigest: revision.CanonicalDigest, Digest: revision.Digest,
	})
}

func decodeRevision(payload []byte) (i18n.CatalogRevision, error) {
	var stored catalogRevisionPayload
	if err := json.Unmarshal(payload, &stored); err != nil {
		return i18n.CatalogRevision{}, err
	}
	return i18n.CatalogRevision{
		ID: stored.ID, Locale: stored.Locale, PreviousRevision: stored.PreviousRevision,
		Version: stored.Version, Fallbacks: stored.Fallbacks, Translations: stored.Translations,
		CreatedAt: stored.CreatedAt, EffectiveFrom: stored.EffectiveFrom,
		EffectiveUntil: stored.EffectiveUntil, CanonicalDigest: stored.CanonicalDigest, Digest: stored.Digest,
	}, nil
}

// Store is the durable publication boundary for reviewed product catalogs.
type Store struct {
	DB       dbport.Beginner
	TenantID func(string) uuid.UUID
}

var _ i18n.ActivatedCatalogStore = (*Store)(nil)

func New(db dbport.Beginner, tenantID func(string) uuid.UUID) *Store {
	return &Store{DB: db, TenantID: tenantID}
}

func (s *Store) begin(ctx context.Context, scope i18n.Scope) (dbport.Tx, uuid.UUID, error) {
	if err := scope.Validate(); err != nil {
		return nil, uuid.Nil, err
	}
	if s == nil || s.DB == nil || s.TenantID == nil {
		return nil, uuid.Nil, errNotConfigured
	}
	tenantID := s.TenantID(scope.Tenant)
	if tenantID == uuid.Nil {
		return nil, uuid.Nil, fmt.Errorf("i18ncatalogstore: tenant %q does not resolve", scope.Tenant)
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
		_ = tx.Rollback(context.Background())
		return nil, uuid.Nil, fmt.Errorf("i18ncatalogstore: set tenant: %w", err)
	}
	return tx, tenantID, nil
}

func (s *Store) Publish(ctx context.Context, scope i18n.Scope, revision i18n.CatalogRevision) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if err := revision.Validate(); err != nil {
		return err
	}
	if !revision.VerifyDigest() {
		return errors.New("i18ncatalogstore: revision digest is invalid")
	}
	payload, err := encodeRevision(revision)
	if err != nil {
		return fmt.Errorf("i18ncatalogstore: encode revision: %w", err)
	}
	tx, tenantID, err := s.begin(ctx, scope)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	_, err = tx.Exec(ctx, `
		INSERT INTO activated_translation_catalog_revision
			(tenant_id, product_id, locale, revision_id, digest, payload)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		ON CONFLICT (tenant_id, product_id, locale, revision_id) DO NOTHING`,
		tenantID, scope.Product, revision.Locale, revision.ID, revision.CanonicalDigest, string(payload))
	if err != nil {
		return fmt.Errorf("i18ncatalogstore: insert revision: %w", err)
	}
	var same bool
	if err := tx.QueryRow(ctx, `
		SELECT digest = $5 AND payload = $6::jsonb
		FROM activated_translation_catalog_revision
		WHERE tenant_id = $1 AND product_id = $2 AND locale = $3 AND revision_id = $4`,
		tenantID, scope.Product, revision.Locale, revision.ID, revision.CanonicalDigest, string(payload)).Scan(&same); err != nil {
		return fmt.Errorf("i18ncatalogstore: verify revision: %w", err)
	}
	if !same {
		return fmt.Errorf("i18ncatalogstore: immutable revision conflict for %q", revision.ID)
	}
	return tx.Commit(ctx)
}

func (s *Store) Activate(ctx context.Context, scope i18n.Scope, revisionID, actor string) error {
	if strings.TrimSpace(revisionID) == "" {
		return errors.New("i18ncatalogstore: revision id is required")
	}
	if strings.TrimSpace(actor) == "" {
		return errors.New("i18ncatalogstore: activation actor is required")
	}
	tx, tenantID, err := s.begin(ctx, scope)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	// The interface identifies a revision by ID alone. Detect IDs that collide
	// across locales instead of activating whichever row PostgreSQL finds first.
	var locale, digest, previous string
	err = tx.QueryRow(ctx, `
		SELECT locale, digest, COALESCE(payload->>'previous_revision', '')
		FROM activated_translation_catalog_revision
		WHERE tenant_id = $1 AND product_id = $2 AND revision_id = $3`,
		tenantID, scope.Product, revisionID).Scan(&locale, &digest, &previous)
	if errors.Is(err, dbport.ErrNoRows) {
		return fmt.Errorf("i18ncatalogstore: revision %q does not exist", revisionID)
	}
	if err != nil {
		return fmt.Errorf("i18ncatalogstore: load revision: %w", err)
	}
	var matches int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM activated_translation_catalog_revision
		WHERE tenant_id = $1 AND product_id = $2 AND revision_id = $3`,
		tenantID, scope.Product, revisionID).Scan(&matches); err != nil {
		return fmt.Errorf("i18ncatalogstore: count revision identities: %w", err)
	}
	if matches != 1 {
		return fmt.Errorf("i18ncatalogstore: revision id %q is ambiguous across locales", revisionID)
	}

	// Serialize compare-and-append for this tenant/product/locale so two
	// activations cannot both observe the same prior revision.
	// PostgreSQL text cannot contain NUL bytes. Length-prefix each component
	// so distinct tenant/product/locale tuples cannot share an advisory key.
	lockKey := fmt.Sprintf("%d:%s%d:%s%d:%s", len(tenantID.String()), tenantID.String(), len(scope.Product), scope.Product, len(locale), locale)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return fmt.Errorf("i18ncatalogstore: lock catalog activation: %w", err)
	}
	var activeID string
	err = tx.QueryRow(ctx, `
		SELECT revision_id FROM activated_translation_catalog_event
		WHERE tenant_id = $1 AND product_id = $2 AND locale = $3
		ORDER BY sequence DESC LIMIT 1`, tenantID, scope.Product, locale).Scan(&activeID)
	if errors.Is(err, dbport.ErrNoRows) {
		activeID = ""
	} else if err != nil {
		return fmt.Errorf("i18ncatalogstore: load active revision: %w", err)
	}
	if previous != activeID {
		return fmt.Errorf("i18ncatalogstore: previous revision mismatch: active is %q", activeID)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO activated_translation_catalog_event
			(tenant_id, product_id, locale, revision_id, digest, actor_id, activated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		tenantID, scope.Product, locale, revisionID, digest, actor, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("i18ncatalogstore: append activation: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Store) Active(ctx context.Context, scope i18n.Scope, locale string, at time.Time) (i18n.CatalogRevision, error) {
	if strings.TrimSpace(locale) == "" {
		return i18n.CatalogRevision{}, i18n.ErrInvalidLocale
	}
	tx, tenantID, err := s.begin(ctx, scope)
	if err != nil {
		return i18n.CatalogRevision{}, err
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(ctx, `
		SELECT r.payload::text
		FROM activated_translation_catalog_event e
		JOIN activated_translation_catalog_revision r
		  USING (tenant_id, product_id, locale, revision_id, digest)
		WHERE e.tenant_id = $1 AND e.product_id = $2 AND e.locale = $3
		  AND e.activated_at <= $4
		ORDER BY e.sequence DESC`, tenantID, scope.Product, locale, at)
	if err != nil {
		return i18n.CatalogRevision{}, fmt.Errorf("i18ncatalogstore: load active revision: %w", err)
	}
	defer rows.Close()
	var revision i18n.CatalogRevision
	found := false
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return i18n.CatalogRevision{}, err
		}
		candidate, err := decodeRevision([]byte(payload))
		if err != nil {
			return i18n.CatalogRevision{}, fmt.Errorf("i18ncatalogstore: decode active revision: %w", err)
		}
		if !candidate.VerifyDigest() {
			return i18n.CatalogRevision{}, errors.New("i18ncatalogstore: active revision failed integrity check")
		}
		if candidate.Effective(at) {
			revision, found = candidate, true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return i18n.CatalogRevision{}, err
	}
	rows.Close()
	if !found {
		return i18n.CatalogRevision{}, i18n.ErrNoActiveRevision
	}
	if err := tx.Commit(ctx); err != nil {
		return i18n.CatalogRevision{}, err
	}
	return revision, nil
}
