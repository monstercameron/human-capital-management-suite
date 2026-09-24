// Package contentregistrystore persists the industry-pack, content-registry
// and knowledge rows from migrations/00082_contentregistry.sql. The adapter
// owns transaction boundaries for tenant operations and establishes the RLS
// tenant setting before reading or writing a row.
package contentregistrystore

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
	"github.com/monstercameron/human-capital-management-suite/internal/domains/industrypack"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/knowledge"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the database capability owned by this adapter. It deliberately uses
// dbport rather than a PostgreSQL driver type.
type DB interface {
	dbport.Beginner
	dbport.Execer
	dbport.Querier
}

// Store implements the content registry persistence operations.
type Store struct{ db DB }

var _ industrypack.BindingStore = (*Store)(nil)

// ErrorCode is a stable, driver-independent refusal classification.
type ErrorCode string

const (
	CodeInvalid            ErrorCode = "CONTENTREGISTRY_INVALID"
	CodeDuplicate          ErrorCode = "CONTENTREGISTRY_DUPLICATE"
	CodeNotFound           ErrorCode = "CONTENTREGISTRY_NOT_FOUND"
	CodeStaleCAS           ErrorCode = "CONTENTREGISTRY_STALE_CAS"
	CodeReferenceNotFound  ErrorCode = "CONTENTREGISTRY_REFERENCE_NOT_FOUND"
	CodeIntegrityViolation ErrorCode = "CONTENTREGISTRY_INTEGRITY_VIOLATION"
)

var (
	ErrInvalid           = errors.New("contentregistrystore: invalid row")
	ErrDuplicate         = errors.New("contentregistrystore: duplicate row")
	ErrNotFound          = errors.New("contentregistrystore: row not found")
	ErrStaleCAS          = errors.New("contentregistrystore: stale compare-and-swap")
	ErrReferenceNotFound = errors.New("contentregistrystore: referenced registry row not found")
	ErrIntegrity         = errors.New("contentregistrystore: stored row failed integrity validation")
)

// Error is a typed refusal. Callers can use errors.Is for the broad category
// and Code for stable telemetry.
type Error struct {
	Code   ErrorCode
	Cause  error
	Detail string
}

func (e *Error) Error() string {
	if e == nil {
		return "contentregistrystore: <nil error>"
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Detail)
}

func (e *Error) Unwrap() error { return e.Cause }

func refusal(code ErrorCode, cause error, detail string) error {
	return &Error{Code: code, Cause: cause, Detail: detail}
}

// New returns a content-registry store over db.
func New(db DB) *Store { return &Store{db: db} }

// ManifestRecord is the durable identity/lineage envelope of an industry
// pack. The migration intentionally stores catalogue metadata, not executable
// or body content.
type ManifestRecord struct {
	RowID           uuid.UUID
	PackID          string
	Version         int
	ParentVersion   *int
	ParentDigest    string
	CanonicalDigest string
}

// ContentRecord is the durable identity and dependency envelope of content.
type ContentRecord struct {
	RowID         uuid.UUID
	Ref           string
	EffectiveFrom *time.Time
	EffectiveTo   *time.Time
	Digest        string
	Dependencies  []industrypack.ContentRef
}

// LocalizedRecord reflects exactly the columns available in the migration.
// Classification, provenance and citation detail are intentionally not
// invented here: they belong to the source revision or its governed body.
type LocalizedRecord struct {
	RowID                uuid.UUID
	TenantID             uuid.UUID
	ArticleID            string
	Revision             uint64
	Locale               string
	Reviewer             string
	SourceRevisionDigest string
	BodyDigest           string
	Title                string
	Summary              string
	CreatedAt            time.Time
	Digest               string
}

// SaveManifest appends one platform manifest catalogue row. The caller must
// use an elevated migration/admin connection because hcmnext_app is read-only.
func (s *Store) SaveManifest(ctx context.Context, pack industrypack.IndustryPack) error {
	if s == nil || s.db == nil {
		return refusal(CodeInvalid, ErrInvalid, "database capability is required")
	}
	if err := pack.Validate(); err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	packID := strings.TrimSpace(pack.PackID)
	if packID == "" {
		packID = strings.TrimSpace(pack.ID)
	}
	parentVersion := nullableInt(pack.ParentVersion)
	parentDigest := nullableDigest(pack.ParentDigest)
	affected, err := s.db.Exec(ctx, `
		INSERT INTO industry_pack_manifest
			(row_id, pack_id, version, parent_version, parent_digest, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (pack_id, version) DO NOTHING`,
		uuid.New(), packID, pack.Version, parentVersion, parentDigest, storageDigest(pack.CanonicalDigest))
	if err != nil {
		return fmt.Errorf("contentregistrystore: save manifest: %w", err)
	}
	if affected == 0 {
		return refusal(CodeDuplicate, ErrDuplicate, "manifest revision already exists")
	}
	return nil
}

// PutManifest is an explicit-name alias for SaveManifest.
func (s *Store) PutManifest(ctx context.Context, pack industrypack.IndustryPack) error {
	return s.SaveManifest(ctx, pack)
}

// LoadManifest reads platform catalogue metadata.
func (s *Store) LoadManifest(ctx context.Context, packID string, version int) (ManifestRecord, error) {
	if s == nil || s.db == nil || strings.TrimSpace(packID) == "" || version <= 0 {
		return ManifestRecord{}, refusal(CodeInvalid, ErrInvalid, "pack id, version and database are required")
	}
	var row ManifestRecord
	var parentVersion *int
	var parentDigest *string
	if err := s.db.QueryRow(ctx, `
		SELECT row_id, pack_id, version, parent_version, parent_digest, canonical_digest
		FROM industry_pack_manifest WHERE pack_id=$1 AND version=$2`, packID, version).
		Scan(&row.RowID, &row.PackID, &row.Version, &parentVersion, &parentDigest, &row.CanonicalDigest); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ManifestRecord{}, refusal(CodeNotFound, ErrNotFound, "manifest revision is absent")
		}
		return ManifestRecord{}, fmt.Errorf("contentregistrystore: load manifest: %w", err)
	}
	row.ParentVersion = parentVersion
	row.CanonicalDigest = domainDigest(row.CanonicalDigest)
	row.ParentDigest = domainDigestOptional(stringValue(parentDigest))
	return row, nil
}

// SaveContent appends one platform content-registry entry.
func (s *Store) SaveContent(ctx context.Context, content industrypack.Content) error {
	if s == nil || s.db == nil {
		return refusal(CodeInvalid, ErrInvalid, "database capability is required")
	}
	if err := content.Validate(); err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	deps, err := json.Marshal(content.Dependencies)
	if err != nil {
		return fmt.Errorf("contentregistrystore: marshal content dependencies: %w", err)
	}
	if content.Dependencies == nil {
		deps = []byte("[]")
	}
	from, to := contentWindow(content.Effective)
	ref := content.Ref.Key()
	affected, err := s.db.Exec(ctx, `
		INSERT INTO content_registry_entry
			(row_id, ref, effective_from, effective_to, digest, dependencies)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb)
		ON CONFLICT (ref) DO NOTHING`,
		uuid.New(), ref, from, to, storageDigest(content.Digest), string(deps))
	if err != nil {
		return fmt.Errorf("contentregistrystore: save content: %w", err)
	}
	if affected == 0 {
		return refusal(CodeDuplicate, ErrDuplicate, "content entry already exists")
	}
	return nil
}

// PutContent is an explicit-name alias for SaveContent.
func (s *Store) PutContent(ctx context.Context, content industrypack.Content) error {
	return s.SaveContent(ctx, content)
}

// LoadContent reads a platform content-registry entry.
func (s *Store) LoadContent(ctx context.Context, ref industrypack.ContentRef) (ContentRecord, error) {
	if s == nil || s.db == nil {
		return ContentRecord{}, refusal(CodeInvalid, ErrInvalid, "database capability is required")
	}
	if err := ref.Validate(); err != nil {
		return ContentRecord{}, refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	var row ContentRecord
	var dependencies []byte
	if err := s.db.QueryRow(ctx, `
		SELECT row_id, ref, effective_from, effective_to, digest, dependencies
		FROM content_registry_entry WHERE ref=$1`, ref.Key()).
		Scan(&row.RowID, &row.Ref, &row.EffectiveFrom, &row.EffectiveTo, &row.Digest, &dependencies); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return ContentRecord{}, refusal(CodeNotFound, ErrNotFound, "content entry is absent")
		}
		return ContentRecord{}, fmt.Errorf("contentregistrystore: load content: %w", err)
	}
	if len(dependencies) != 0 {
		if err := json.Unmarshal(dependencies, &row.Dependencies); err != nil {
			return ContentRecord{}, refusal(CodeIntegrityViolation, ErrIntegrity, "content dependencies are invalid JSON")
		}
	}
	row.Digest = domainDigest(row.Digest)
	return row, nil
}

// PutBinding stores a tenant composition after checking every manifest and
// content reference against the platform catalogs in the same transaction.
func (s *Store) SaveBinding(ctx context.Context, tenantID string, binding industrypack.Binding) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if binding.CanonicalDigest == "" || len(binding.Packs) == 0 {
		return refusal(CodeInvalid, ErrInvalid, "binding digest and at least one pack are required")
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		if err := s.checkBindingRefs(ctx, tx, binding); err != nil {
			return err
		}
		payload, err := marshalBinding(binding)
		if err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO industry_pack_binding (row_id, tenant_id, packs, contents, canonical_digest)
			VALUES ($1,$2,$3::jsonb,$4::jsonb,$5)
			ON CONFLICT DO NOTHING`, uuid.New(), tid, payload.packs, payload.contents, storageDigest(binding.CanonicalDigest))
		if err != nil {
			return fmt.Errorf("contentregistrystore: save binding: %w", err)
		}
		if affected == 0 {
			return refusal(CodeDuplicate, ErrDuplicate, "binding row already exists")
		}
		return nil
	})
}

// PutBinding is an explicit-name alias for SaveBinding.
func (s *Store) PutBinding(ctx context.Context, tenantID string, binding industrypack.Binding) error {
	return s.SaveBinding(ctx, tenantID, binding)
}

func (s *Store) LoadBinding(ctx context.Context, tenantID, digest string) (industrypack.Binding, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return industrypack.Binding{}, err
	}
	var out industrypack.Binding
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var packs, contents []byte
		var storedDigest string
		err := tx.QueryRow(ctx, `
			SELECT packs, contents, canonical_digest
			FROM industry_pack_binding WHERE tenant_id=$1 AND canonical_digest=$2
			ORDER BY row_id LIMIT 1`, tid, storageDigest(digest)).Scan(&packs, &contents, &storedDigest)
		if errors.Is(err, dbport.ErrNoRows) {
			return refusal(CodeNotFound, ErrNotFound, "binding is absent")
		}
		if err != nil {
			return fmt.Errorf("contentregistrystore: load binding: %w", err)
		}
		var decodeErr error
		out, decodeErr = unmarshalBinding(packs, contents, domainDigest(storedDigest))
		return decodeErr
	})
	return out, err
}

// SaveArticle appends a tenant-scoped immutable knowledge article revision.
func (s *Store) SaveArticle(ctx context.Context, tenantID string, article knowledge.ArticleRevision) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if err := article.Validate(); err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	if len(article.AuthorizedRoles) == 0 || strings.TrimSpace(article.RetentionScheduleRef) == "" {
		return refusal(CodeInvalid, ErrInvalid, "authorized roles and a governed retention schedule are required")
	}
	roleSeen := make(map[string]struct{}, len(article.AuthorizedRoles))
	for _, role := range article.AuthorizedRoles {
		role = strings.TrimSpace(role)
		if role == "" {
			return refusal(CodeInvalid, ErrInvalid, "authorized roles cannot be blank")
		}
		key := strings.ToLower(role)
		if _, duplicate := roleSeen[key]; duplicate {
			return refusal(CodeInvalid, ErrInvalid, "authorized roles cannot be duplicated")
		}
		roleSeen[key] = struct{}{}
	}
	payload, err := marshalArticle(article)
	if err != nil {
		return err
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		affected, execErr := tx.Exec(ctx, `
			INSERT INTO knowledge_article_revision (
				row_id, tenant_id, article_id, revision, locale, audience_scope,
				authorized_roles, retention_schedule_ref, retain_until,
				classification, owner, source_authority, source_refs, jurisdiction,
				effective_from, effective_to, known_from, known_to, body_digest,
				title, summary, review, supersession)
			VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22::jsonb,$23::jsonb)
			ON CONFLICT (tenant_id, article_id, revision) DO NOTHING`,
			uuid.New(), tid, article.ArticleID, int64(article.Revision), article.Locale,
			payload.audience, payload.authorizedRoles, article.RetentionScheduleRef, payload.retainUntil,
			article.Classification, article.Owner, article.SourceAuthority,
			payload.sourceRefs, article.Jurisdiction, payload.effectiveFrom, payload.effectiveTo,
			payload.knownFrom, payload.knownTo, storageDigest(article.BodyDigest), article.Title,
			article.Summary, payload.review, nullableJSONString(payload.supersession))
		if execErr != nil {
			return fmt.Errorf("contentregistrystore: save article: %w", execErr)
		}
		if affected == 0 {
			return refusal(CodeDuplicate, ErrDuplicate, "article revision already exists")
		}
		return nil
	})
}

// PutArticle is an explicit-name alias for SaveArticle.
func (s *Store) PutArticle(ctx context.Context, tenantID string, article knowledge.ArticleRevision) error {
	return s.SaveArticle(ctx, tenantID, article)
}

// LoadArticle reloads an immutable article revision and verifies its domain
// validation on the way out.
func (s *Store) LoadArticle(ctx context.Context, tenantID, articleID string, revision uint64) (knowledge.ArticleRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return knowledge.ArticleRevision{}, err
	}
	var out knowledge.ArticleRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var row articleRow
		if scanErr := tx.QueryRow(ctx, `
			SELECT article_id, revision, locale, audience_scope, classification, owner,
				authorized_roles, retention_schedule_ref, retain_until,
				source_authority, source_refs, jurisdiction, effective_from, effective_to,
				known_from, known_to, body_digest, title, summary, review, supersession
			FROM knowledge_article_revision
				WHERE tenant_id=$1 AND article_id=$2 AND revision=$3`, tid, articleID, int64(revision)).Scan(
			&row.articleID, &row.revision, &row.locale, &row.audience, &row.classification,
			&row.owner, &row.roles, &row.retentionScheduleRef, &row.retainUntil,
			&row.sourceAuthority, &row.sourceRefs, &row.jurisdiction,
			&row.effectiveFrom, &row.effectiveTo, &row.knownFrom, &row.knownTo,
			&row.bodyDigest, &row.title, &row.summary, &row.review, &row.supersession); scanErr != nil {
			if errors.Is(scanErr, dbport.ErrNoRows) {
				return refusal(CodeNotFound, ErrNotFound, "article revision is absent")
			}
			return fmt.Errorf("contentregistrystore: load article: %w", scanErr)
		}
		var decodeErr error
		out, decodeErr = unmarshalArticle(row)
		if decodeErr != nil {
			return decodeErr
		}
		if validateErr := out.Validate(); validateErr != nil {
			return refusal(CodeIntegrityViolation, ErrIntegrity, validateErr.Error())
		}
		return nil
	})
	return out, err
}

// SearchCandidates returns activated article revisions that pass tenant,
// locale, role, audience, retention, review, supersession, and effective-time
// checks before the bounded candidate limit. The caller repeats those policy
// checks in knowledge.SearchService before projecting a result.
func (s *Store) SearchCandidates(ctx context.Context, tenantID, locale, query, audience string, roles []string, at time.Time) ([]knowledge.SearchableArticle, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(locale) == "" || strings.TrimSpace(query) == "" || strings.TrimSpace(audience) == "" || len(roles) == 0 {
		return nil, refusal(CodeInvalid, ErrInvalid, "locale, query, audience and roles are required")
	}
	var out []knowledge.SearchableArticle
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, queryErr := tx.Query(ctx, `
			SELECT a.article_id, a.revision, a.locale, a.audience_scope, a.classification, a.owner,
				a.authorized_roles, a.retention_schedule_ref, a.retain_until,
				a.source_authority, a.source_refs, a.jurisdiction, a.effective_from, a.effective_to,
				a.known_from, a.known_to, a.body_digest, a.title, a.summary, a.review, a.supersession
			FROM knowledge_article_revision a
		JOIN knowledge_activation active
		  ON active.tenant_id=a.tenant_id AND active.article_id=a.article_id
		 AND active.revision=a.revision AND active.locale=a.locale
		WHERE a.tenant_id=$1 AND a.locale=$2
		  AND a.retention_schedule_ref <> '' AND jsonb_array_length(a.authorized_roles) > 0
		  AND (a.retain_until IS NULL OR a.retain_until > $3)
		  AND EXISTS (SELECT 1 FROM jsonb_array_elements_text(a.authorized_roles) article_role
			              WHERE lower(btrim(article_role)) = ANY($5::text[]))
		  AND CASE upper(btrim(a.audience_scope #>> '{}'))
		        WHEN 'PUBLIC' THEN 0 WHEN 'EMPLOYEES' THEN 1 WHEN 'MANAGERS' THEN 2 ELSE 99 END
		      <= CASE upper(btrim($4::text))
		        WHEN 'PUBLIC' THEN 0 WHEN 'EMPLOYEES' THEN 1 WHEN 'MANAGERS' THEN 2 ELSE -1 END
		  AND a.effective_from <= $3
		  AND (a.effective_to IS NULL OR a.effective_to > $3)
		  AND (a.review->>'expires_at')::timestamptz > $3
		  AND a.supersession IS NULL
		  AND to_tsvector('simple', coalesce(a.title,'') || ' ' || coalesce(a.summary,''))
		      @@ plainto_tsquery('simple',$6)
		ORDER BY a.title, a.article_id, a.revision DESC LIMIT 200`, tid, locale, at.UTC(), audience, normalizedRoles(roles), query)
		if queryErr != nil {
			return fmt.Errorf("contentregistrystore: search articles: %w", queryErr)
		}
		defer rows.Close()
		for rows.Next() {
			var row articleRow
			if scanErr := rows.Scan(&row.articleID, &row.revision, &row.locale, &row.audience,
				&row.classification, &row.owner, &row.roles, &row.retentionScheduleRef, &row.retainUntil,
				&row.sourceAuthority, &row.sourceRefs, &row.jurisdiction, &row.effectiveFrom, &row.effectiveTo,
				&row.knownFrom, &row.knownTo, &row.bodyDigest, &row.title, &row.summary, &row.review, &row.supersession); scanErr != nil {
				return fmt.Errorf("contentregistrystore: scan search candidate: %w", scanErr)
			}
			article, decodeErr := unmarshalArticle(row)
			if decodeErr != nil {
				return decodeErr
			}
			if validateErr := article.Validate(); validateErr != nil {
				return refusal(CodeIntegrityViolation, ErrIntegrity, validateErr.Error())
			}
			out = append(out, knowledge.SearchableArticle{Article: article})
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return fmt.Errorf("contentregistrystore: read search candidates: %w", rowsErr)
		}
		return nil
	})
	return out, err
}

func normalizedRoles(roles []string) []string {
	out := make([]string, 0, len(roles))
	for _, role := range roles {
		if value := strings.ToLower(strings.TrimSpace(role)); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// SaveLocalized appends the sparse localized revision row defined by the
// migration. The source article and source digest are checked before insert.
func (s *Store) SaveLocalized(ctx context.Context, tenantID string, localized knowledge.LocalizedRevision) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if err := localized.Validate(); err != nil {
		return refusal(CodeInvalid, ErrInvalid, err.Error())
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var sourceRow articleRow
		if err := tx.QueryRow(ctx, `
			SELECT article_id, revision, locale, audience_scope, classification, owner,
				authorized_roles, retention_schedule_ref, retain_until,
				source_authority, source_refs, jurisdiction, effective_from, effective_to,
				known_from, known_to, body_digest, title, summary, review, supersession
			FROM knowledge_article_revision
			WHERE tenant_id=$1 AND article_id=$2 AND revision=$3`, tid, localized.ArticleID, int64(localized.Revision)).Scan(
			&sourceRow.articleID, &sourceRow.revision, &sourceRow.locale, &sourceRow.audience, &sourceRow.classification,
			&sourceRow.owner, &sourceRow.roles, &sourceRow.retentionScheduleRef, &sourceRow.retainUntil,
			&sourceRow.sourceAuthority, &sourceRow.sourceRefs, &sourceRow.jurisdiction,
			&sourceRow.effectiveFrom, &sourceRow.effectiveTo, &sourceRow.knownFrom, &sourceRow.knownTo,
			&sourceRow.bodyDigest, &sourceRow.title, &sourceRow.summary, &sourceRow.review, &sourceRow.supersession); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return refusal(CodeReferenceNotFound, ErrReferenceNotFound, "localized revision has no source article")
			}
			return fmt.Errorf("contentregistrystore: check localized source: %w", err)
		}
		sourceArticle, decodeErr := unmarshalArticle(sourceRow)
		if decodeErr != nil {
			return decodeErr
		}
		if localized.SourceRevisionDigest != sourceArticle.Digest() {
			return refusal(CodeStaleCAS, ErrStaleCAS, "localized revision cites a different source digest")
		}
		affected, execErr := tx.Exec(ctx, `
			INSERT INTO knowledge_localized_revision (
				row_id, tenant_id, article_id, revision, locale, reviewer,
				source_revision_digest, body_digest, title, summary, created_at, digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT (tenant_id, article_id, locale, revision) DO NOTHING`,
			uuid.New(), tid, localized.ArticleID, int64(localized.Revision), localized.Locale,
			nullableText(localized.Reviewer), nullableDigest(localized.SourceRevisionDigest),
			storageDigest(localized.BodyDigest), localized.Title, nullableText(localized.Summary),
			localized.CreatedAt.UTC(), storageDigest(localized.DigestValue()))
		if execErr != nil {
			return fmt.Errorf("contentregistrystore: save localized revision: %w", execErr)
		}
		if affected == 0 {
			return refusal(CodeDuplicate, ErrDuplicate, "localized revision already exists")
		}
		return nil
	})
}

// LoadLocalized reads exactly the sparse localized row persisted by 00082.
func (s *Store) LoadLocalized(ctx context.Context, tenantID, articleID, locale string, revision uint64) (LocalizedRecord, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return LocalizedRecord{}, err
	}
	var out LocalizedRecord
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var rev int64
		err := tx.QueryRow(ctx, `
			SELECT row_id, tenant_id, article_id, revision, locale, reviewer,
				source_revision_digest, body_digest, title, summary, created_at, digest
			FROM knowledge_localized_revision
			WHERE tenant_id=$1 AND article_id=$2 AND locale=$3 AND revision=$4`, tid, articleID, locale, int64(revision)).Scan(
			&out.RowID, &out.TenantID, &out.ArticleID, &rev, &out.Locale, &out.Reviewer,
			&out.SourceRevisionDigest, &out.BodyDigest, &out.Title, &out.Summary,
			&out.CreatedAt, &out.Digest)
		if errors.Is(err, dbport.ErrNoRows) {
			return refusal(CodeNotFound, ErrNotFound, "localized revision is absent")
		}
		if err != nil {
			return fmt.Errorf("contentregistrystore: load localized revision: %w", err)
		}
		out.Revision = uint64(rev)
		out.SourceRevisionDigest = domainDigestOptional(out.SourceRevisionDigest)
		out.BodyDigest = domainDigest(out.BodyDigest)
		out.Digest = domainDigest(out.Digest)
		return nil
	})
	return out, err
}

// AppendLifecycleEvent records one immutable knowledge lifecycle event.
func (s *Store) AppendLifecycleEvent(ctx context.Context, tenantID string, event knowledge.LifecycleEvent, sequence uint64) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if sequence == 0 || event.EventID == "" || event.ArticleID == "" || event.At.IsZero() {
		return refusal(CodeInvalid, ErrInvalid, "event id, article id, time and positive sequence are required")
	}
	if event.Digest == "" {
		event.Digest = event.DigestValue()
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		affected, execErr := tx.Exec(ctx, `
			INSERT INTO knowledge_lifecycle_event (
				row_id, tenant_id, event_id, kind, previous_state, state, article_id,
				revision, locale, reviewer, bundle_id, bundle_digest, receipt_digest,
				activation_epoch, at, digest, event_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
			ON CONFLICT DO NOTHING`, uuid.New(), tid, event.EventID, event.Kind,
			nullableText(string(event.PreviousState)), event.State, event.ArticleID,
			nullableUint(event.Revision), nullableText(event.Locale), nullableText(event.Reviewer),
			nullableText(event.BundleID), nullableDigest(event.BundleDigest), nullableDigest(event.ReceiptDigest),
			nullableUint(event.ActivationEpoch), event.At.UTC(), storageDigest(event.Digest), sequence)
		if execErr != nil {
			return fmt.Errorf("contentregistrystore: append lifecycle event: %w", execErr)
		}
		if affected == 0 {
			return refusal(CodeDuplicate, ErrDuplicate, "lifecycle event identity already exists")
		}
		return nil
	})
}

// ListLifecycleEvents returns immutable event history oldest first.
func (s *Store) ListLifecycleEvents(ctx context.Context, tenantID, articleID string) ([]knowledge.LifecycleEvent, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	var out []knowledge.LifecycleEvent
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, queryErr := tx.Query(ctx, `
			SELECT event_id, kind, previous_state, state, article_id, revision, locale,
				reviewer, bundle_id, bundle_digest, receipt_digest, activation_epoch,
				at, digest, event_sequence
			FROM knowledge_lifecycle_event
			WHERE tenant_id=$1 AND article_id=$2 ORDER BY event_sequence`, tid, articleID)
		if queryErr != nil {
			return fmt.Errorf("contentregistrystore: list lifecycle events: %w", queryErr)
		}
		defer rows.Close()
		for rows.Next() {
			var event knowledge.LifecycleEvent
			var revision, epoch *int64
			var eventSequence int64
			var previous, locale, reviewer, bundleID, bundleDigest, receiptDigest *string
			var digest string
			if scanErr := rows.Scan(&event.EventID, &event.Kind, &previous, &event.State, &event.ArticleID,
				&revision, &locale, &reviewer, &bundleID, &bundleDigest, &receiptDigest, &epoch,
				&event.At, &digest, &eventSequence); scanErr != nil {
				return fmt.Errorf("contentregistrystore: scan lifecycle event: %w", scanErr)
			}
			event.PreviousState = knowledge.LifecycleState(stringValue(previous))
			event.Revision = uint64(int64Value(revision))
			event.Locale, event.Reviewer = stringValue(locale), stringValue(reviewer)
			event.BundleID, event.BundleDigest = stringValue(bundleID), domainDigestOptional(stringValue(bundleDigest))
			event.ReceiptDigest, event.ActivationEpoch = domainDigestOptional(stringValue(receiptDigest)), uint64(int64Value(epoch))
			event.Digest = domainDigest(digest)
			_ = eventSequence
			out = append(out, event)
		}
		if rowErr := rows.Err(); rowErr != nil {
			return fmt.Errorf("contentregistrystore: list lifecycle events: %w", rowErr)
		}
		return nil
	})
	return out, err
}

// SaveActivation writes the current activation for an article/locale. A
// lower or equal epoch is refused, making retries and stale writers explicit.
func (s *Store) SaveKnowledgeActivation(ctx context.Context, tenantID string, activation knowledge.ActivationBinding) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if activation.ArticleID == "" || activation.Revision == 0 || activation.Locale == "" || activation.ActivationEpoch == 0 || activation.ActivatedAt.IsZero() {
		return refusal(CodeInvalid, ErrInvalid, "activation identity, positive epoch and time are required")
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		affected, execErr := tx.Exec(ctx, `
			INSERT INTO knowledge_activation (
				row_id, tenant_id, article_id, revision, locale, bundle_id,
				bundle_digest, activation_epoch, activated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (tenant_id, article_id, locale) DO UPDATE SET
				revision=EXCLUDED.revision, bundle_id=EXCLUDED.bundle_id,
				bundle_digest=EXCLUDED.bundle_digest, activation_epoch=EXCLUDED.activation_epoch,
				activated_at=EXCLUDED.activated_at
			WHERE EXCLUDED.activation_epoch > knowledge_activation.activation_epoch`,
			uuid.New(), tid, activation.ArticleID, int64(activation.Revision), activation.Locale,
			nullableText(activation.BundleID), nullableDigest(activation.BundleDigest),
			int64(activation.ActivationEpoch), activation.ActivatedAt.UTC())
		if execErr != nil {
			return fmt.Errorf("contentregistrystore: save activation: %w", execErr)
		}
		if affected == 0 {
			return refusal(CodeStaleCAS, ErrStaleCAS, "activation epoch is not newer than the current epoch")
		}
		return nil
	})
}

// LoadActivation reads the current activation for an article/locale.
func (s *Store) LoadActivation(ctx context.Context, tenantID, articleID, locale string) (knowledge.ActivationBinding, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return knowledge.ActivationBinding{}, err
	}
	var out knowledge.ActivationBinding
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var revision, epoch int64
		var bundleID, bundleDigest *string
		if scanErr := tx.QueryRow(ctx, `
			SELECT article_id, revision, locale, bundle_id, bundle_digest,
				activation_epoch, activated_at
			FROM knowledge_activation WHERE tenant_id=$1 AND article_id=$2 AND locale=$3`, tid, articleID, locale).
			Scan(&out.ArticleID, &revision, &out.Locale, &bundleID, &bundleDigest, &epoch, &out.ActivatedAt); scanErr != nil {
			if errors.Is(scanErr, dbport.ErrNoRows) {
				return refusal(CodeNotFound, ErrNotFound, "activation is absent")
			}
			return fmt.Errorf("contentregistrystore: load activation: %w", scanErr)
		}
		out.Revision, out.BundleID, out.BundleDigest, out.ActivationEpoch = uint64(revision), stringValue(bundleID), domainDigestOptional(stringValue(bundleDigest)), uint64(epoch)
		return nil
	})
	return out, err
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return refusal(CodeInvalid, ErrInvalid, "database capability is required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("contentregistrystore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("contentregistrystore: commit transaction: %w", err)
	}
	return nil
}

func parseTenant(raw string) (uuid.UUID, error) {
	tid, err := uuid.Parse(raw)
	if err != nil || tid == uuid.Nil {
		return uuid.Nil, refusal(CodeInvalid, ErrInvalid, "tenant id must be a non-nil UUID")
	}
	return tid, nil
}

func (s *Store) checkBindingRefs(ctx context.Context, tx dbport.Tx, binding industrypack.Binding) error {
	packIDs := make([]string, 0, len(binding.Packs))
	packVersions := make([]int32, 0, len(binding.Packs))
	for _, pack := range binding.Packs {
		packID := strings.TrimSpace(pack.PackID)
		if packID == "" {
			packID = strings.TrimSpace(pack.ID)
		}
		packIDs = append(packIDs, packID)
		packVersions = append(packVersions, int32(pack.Version))
	}
	if len(packIDs) > 0 {
		rows, err := tx.Query(ctx, `
			SELECT pack_id, version
			FROM industry_pack_manifest
			WHERE (pack_id, version) IN (SELECT * FROM unnest($1::text[], $2::integer[]))`, packIDs, packVersions)
		if err != nil {
			return fmt.Errorf("contentregistrystore: check manifest references: %w", err)
		}
		found := make(map[string]struct{}, len(packIDs))
		for rows.Next() {
			var id string
			var version int32
			if err := rows.Scan(&id, &version); err != nil {
				rows.Close()
				return fmt.Errorf("contentregistrystore: scan manifest reference: %w", err)
			}
			found[fmt.Sprintf("%s/%d", id, version)] = struct{}{}
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return fmt.Errorf("contentregistrystore: read manifest references: %w", rowsErr)
		}
		for i, id := range packIDs {
			key := fmt.Sprintf("%s/%d", id, packVersions[i])
			if _, ok := found[key]; !ok {
				return refusal(CodeReferenceNotFound, ErrReferenceNotFound, fmt.Sprintf("pack %s is absent", key))
			}
		}
	}
	contentRefs := make([]string, 0, len(binding.Contents))
	for _, content := range binding.Contents {
		contentRefs = append(contentRefs, content.Ref.Key())
	}
	if len(contentRefs) > 0 {
		rows, err := tx.Query(ctx, `SELECT ref FROM content_registry_entry WHERE ref = ANY($1::text[])`, contentRefs)
		if err != nil {
			return fmt.Errorf("contentregistrystore: check content references: %w", err)
		}
		found := make(map[string]struct{}, len(contentRefs))
		for rows.Next() {
			var ref string
			if err := rows.Scan(&ref); err != nil {
				rows.Close()
				return fmt.Errorf("contentregistrystore: scan content reference: %w", err)
			}
			found[ref] = struct{}{}
		}
		rowsErr := rows.Err()
		rows.Close()
		if rowsErr != nil {
			return fmt.Errorf("contentregistrystore: read content references: %w", rowsErr)
		}
		for _, ref := range contentRefs {
			if _, ok := found[ref]; !ok {
				return refusal(CodeReferenceNotFound, ErrReferenceNotFound, fmt.Sprintf("content %s is absent", ref))
			}
		}
	}
	return nil
}

type bindingPayload struct {
	packs, contents string
}

type storedBoundContent struct {
	Ref           industrypack.ContentRef   `json:"ref"`
	EffectiveFrom string                    `json:"effective_from"`
	EffectiveTo   string                    `json:"effective_to,omitempty"`
	HasEnd        bool                      `json:"has_end"`
	Digest        string                    `json:"digest"`
	Dependencies  []industrypack.ContentRef `json:"dependencies,omitempty"`
	Overridable   bool                      `json:"overridable"`
	OverrideOf    *industrypack.ContentRef  `json:"override_of,omitempty"`
	PackID        string                    `json:"pack_id"`
	PackVersion   int                       `json:"pack_version"`
}

func marshalBinding(binding industrypack.Binding) (bindingPayload, error) {
	packs, err := json.Marshal(binding.Packs)
	if err != nil {
		return bindingPayload{}, fmt.Errorf("contentregistrystore: marshal binding packs: %w", err)
	}
	contents := make([]storedBoundContent, 0, len(binding.Contents))
	for _, item := range binding.Contents {
		from := item.Effective.Start
		if err := from.Validate(); err != nil {
			return bindingPayload{}, refusal(CodeInvalid, ErrInvalid, "binding content effective window must be date-based")
		}
		entry := storedBoundContent{Ref: item.Ref, EffectiveFrom: from.String(), Digest: item.Digest, Dependencies: item.Dependencies, Overridable: item.Overridable, OverrideOf: item.OverrideOf, PackID: item.PackID, PackVersion: item.PackVersion}
		if item.Effective.HasEnd {
			entry.EffectiveTo, entry.HasEnd = item.Effective.End.String(), true
		}
		contents = append(contents, entry)
	}
	contentJSON, err := json.Marshal(contents)
	if err != nil {
		return bindingPayload{}, fmt.Errorf("contentregistrystore: marshal binding contents: %w", err)
	}
	return bindingPayload{packs: string(packs), contents: string(contentJSON)}, nil
}

func unmarshalBinding(packsJSON, contentsJSON []byte, digest string) (industrypack.Binding, error) {
	var packs []industrypack.IndustryPack
	if err := json.Unmarshal(packsJSON, &packs); err != nil {
		return industrypack.Binding{}, refusal(CodeIntegrityViolation, ErrIntegrity, "binding packs are invalid JSON")
	}
	var stored []storedBoundContent
	if err := json.Unmarshal(contentsJSON, &stored); err != nil {
		return industrypack.Binding{}, refusal(CodeIntegrityViolation, ErrIntegrity, "binding contents are invalid JSON")
	}
	contents := make([]industrypack.BoundContent, 0, len(stored))
	for _, item := range stored {
		start, err := values.ParseLocalDate(item.EffectiveFrom)
		if err != nil {
			return industrypack.Binding{}, refusal(CodeIntegrityViolation, ErrIntegrity, "binding effective start is invalid")
		}
		window := industrypack.OpenWindow(start)
		if item.HasEnd {
			end, parseErr := values.ParseLocalDate(item.EffectiveTo)
			if parseErr != nil {
				return industrypack.Binding{}, refusal(CodeIntegrityViolation, ErrIntegrity, "binding effective end is invalid")
			}
			window = industrypack.ClosedWindow(start, end)
		}
		contents = append(contents, industrypack.BoundContent{Content: industrypack.Content{Ref: item.Ref, Effective: window, Digest: domainDigest(item.Digest), Dependencies: item.Dependencies, Overridable: item.Overridable, OverrideOf: item.OverrideOf}, PackID: item.PackID, PackVersion: item.PackVersion})
	}
	return industrypack.Binding{Packs: packs, Contents: contents, CanonicalDigest: digest}, nil
}

type articlePayload struct {
	audience, authorizedRoles, sourceRefs, review, supersession string
	retentionScheduleRef                                        string
	effectiveFrom, effectiveTo, knownFrom, knownTo, retainUntil *time.Time
}

type supersessionJSON struct {
	ArticleID string `json:"superseding_article_id"`
	Revision  uint64 `json:"superseding_revision"`
	At        string `json:"superseded_at"`
}
type reviewJSON struct {
	ReviewedBy  string `json:"reviewed_by"`
	ReviewedAt  string `json:"reviewed_at"`
	ExpiresAt   string `json:"expires_at"`
	ApprovalRef string `json:"approval_ref"`
}

func marshalArticle(article knowledge.ArticleRevision) (articlePayload, error) {
	audience, err := json.Marshal(article.AudienceScope)
	if err != nil {
		return articlePayload{}, fmt.Errorf("contentregistrystore: marshal audience: %w", err)
	}
	roles, err := json.Marshal(article.AuthorizedRoles)
	if err != nil {
		return articlePayload{}, fmt.Errorf("contentregistrystore: marshal authorized roles: %w", err)
	}
	sourceRefs, err := json.Marshal(article.SourceRefs)
	if err != nil {
		return articlePayload{}, fmt.Errorf("contentregistrystore: marshal source refs: %w", err)
	}
	review, err := json.Marshal(reviewJSON{ReviewedBy: article.Review.ReviewedBy, ReviewedAt: article.Review.ReviewedAt.String(), ExpiresAt: article.Review.ExpiresAt.String(), ApprovalRef: article.Review.ApprovalRef})
	if err != nil {
		return articlePayload{}, fmt.Errorf("contentregistrystore: marshal review: %w", err)
	}
	var supersession string
	if article.Supersession != nil {
		raw, marshalErr := json.Marshal(supersessionJSON{ArticleID: article.Supersession.SupersedingArticleID, Revision: article.Supersession.SupersedingRevision, At: article.Supersession.SupersededAt.String()})
		if marshalErr != nil {
			return articlePayload{}, fmt.Errorf("contentregistrystore: marshal supersession: %w", marshalErr)
		}
		supersession = string(raw)
	}
	effectiveFrom, effectiveTo := article.EffectiveInterval.EffectiveFrom.Time(), nullableInstant(article.EffectiveInterval.EffectiveTo)
	knownFrom, knownTo := article.KnownInterval.KnownFrom.Time(), nullableInstant(article.KnownInterval.KnownTo)
	return articlePayload{audience: string(audience), authorizedRoles: string(roles), retentionScheduleRef: article.RetentionScheduleRef, retainUntil: nullableInstant(article.RetainUntil), sourceRefs: string(sourceRefs), review: string(review), supersession: supersession, effectiveFrom: &effectiveFrom, effectiveTo: effectiveTo, knownFrom: &knownFrom, knownTo: knownTo}, nil
}

type articleRow struct {
	articleID                                                    string
	revision                                                     int64
	locale, classification, owner, sourceAuthority, jurisdiction string
	audience, roles, sourceRefs                                  []byte
	retentionScheduleRef                                         string
	effectiveFrom, effectiveTo, knownFrom, knownTo, retainUntil  *time.Time
	bodyDigest, title, summary                                   string
	review, supersession                                         []byte
}

func unmarshalArticle(row articleRow) (knowledge.ArticleRevision, error) {
	var audience string
	if err := json.Unmarshal(row.audience, &audience); err != nil {
		return knowledge.ArticleRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "article audience is invalid JSON")
	}
	var sourceRefs []knowledge.SourceRef
	if err := json.Unmarshal(row.sourceRefs, &sourceRefs); err != nil {
		return knowledge.ArticleRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "article source references are invalid JSON")
	}
	var roles []string
	if len(row.roles) != 0 {
		if err := json.Unmarshal(row.roles, &roles); err != nil {
			return knowledge.ArticleRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "article authorized roles are invalid JSON")
		}
	}
	review := reviewJSON{}
	if err := json.Unmarshal(row.review, &review); err != nil {
		return knowledge.ArticleRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "article review is invalid JSON")
	}
	reviewedAt, err := parseInstant(review.ReviewedAt)
	if err != nil {
		return knowledge.ArticleRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "article reviewed_at is invalid")
	}
	expiresAt, err := parseInstant(review.ExpiresAt)
	if err != nil {
		return knowledge.ArticleRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "article expires_at is invalid")
	}
	article := knowledge.ArticleRevision{ArticleID: row.articleID, Revision: uint64(row.revision), Locale: row.locale, AudienceScope: audience, AuthorizedRoles: roles, RetentionScheduleRef: row.retentionScheduleRef, Classification: row.classification, Owner: row.owner, SourceAuthority: row.sourceAuthority, SourceRefs: sourceRefs, Jurisdiction: row.jurisdiction, BodyDigest: domainDigest(row.bodyDigest), Title: row.title, Summary: row.summary, Review: knowledge.ReviewMetadata{ReviewedBy: review.ReviewedBy, ReviewedAt: values.NewInstant(reviewedAt), ExpiresAt: values.NewInstant(expiresAt), ApprovalRef: review.ApprovalRef}}
	if row.retainUntil != nil {
		article.RetainUntil = values.NewInstant(row.retainUntil.UTC())
	}
	if row.effectiveFrom == nil || row.knownFrom == nil {
		return knowledge.ArticleRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "article interval boundary is null")
	}
	from := values.NewInstant((*row.effectiveFrom).UTC())
	if row.effectiveTo != nil {
		article.EffectiveInterval = knowledge.EffectiveInterval{EffectiveFrom: from, EffectiveTo: values.NewInstant((*row.effectiveTo).UTC())}
	} else {
		article.EffectiveInterval = knowledge.EffectiveInterval{EffectiveFrom: from}
	}
	knownFrom := values.NewInstant((*row.knownFrom).UTC())
	if row.knownTo != nil {
		article.KnownInterval = knowledge.KnownInterval{KnownFrom: knownFrom, KnownTo: values.NewInstant((*row.knownTo).UTC())}
	} else {
		article.KnownInterval = knowledge.KnownInterval{KnownFrom: knownFrom}
	}
	if len(row.supersession) != 0 {
		var sup supersessionJSON
		if err := json.Unmarshal(row.supersession, &sup); err != nil {
			return knowledge.ArticleRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "article supersession is invalid JSON")
		}
		at, err := parseInstant(sup.At)
		if err != nil {
			return knowledge.ArticleRevision{}, refusal(CodeIntegrityViolation, ErrIntegrity, "article superseded_at is invalid")
		}
		article.Supersession = &knowledge.Supersession{SupersedingArticleID: sup.ArticleID, SupersedingRevision: sup.Revision, SupersededAt: values.NewInstant(at)}
	}
	return article, nil
}

func parseInstant(raw string) (time.Time, error) { return time.Parse(time.RFC3339Nano, raw) }

func nullableInstant(value values.Instant) *time.Time {
	if !value.IsSet() {
		return nil
	}
	t := value.Time()
	return &t
}
func contentWindow(window industrypack.EffectiveWindow) (*time.Time, *time.Time) {
	start := window.Start
	if start.Validate() != nil {
		return nil, nil
	}
	from := time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC)
	if window.HasEnd {
		end := window.End
		to := time.Date(int(end.Year()), end.Month(), int(end.Day()), 0, 0, 0, 0, time.UTC)
		return &from, &to
	}
	return &from, nil
}
func nullableText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func nullableDigest(value string) *string {
	if value == "" {
		return nil
	}
	normalized := storageDigest(value)
	return &normalized
}
func nullableJSONString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func nullableInt(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}
func nullableUint(value uint64) *int64 {
	if value == 0 {
		return nil
	}
	v := int64(value)
	return &v
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }
func domainDigest(value string) string {
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}
func domainDigestOptional(value string) string {
	if value == "" {
		return ""
	}
	return domainDigest(value)
}
