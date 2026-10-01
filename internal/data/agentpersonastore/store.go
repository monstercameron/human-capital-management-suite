// Package agentpersonastore persists the immutable persona profile and its
// tenant-scoped lifecycle, ownership, and installation state.
//
// Persona versions and lifecycle events are append-only.  The current
// lifecycle is therefore a projection of the event log, never a mutable state
// column that can erase a previous transition.  Installations deliberately
// retain their version reference without a foreign key: restore reconciliation
// must be able to load an installation whose version image was not restored.
package agentpersonastore

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
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalid                     = errors.New("agentpersonastore: invalid request")
	ErrNotFound                    = errors.New("agentpersonastore: not found")
	ErrConflict                    = errors.New("agentpersonastore: conflict")
	ErrPublicationEvidenceRequired = errors.New("agentpersonastore: verified publication evidence required")
)

// MissingVersionAfterRestore is the typed suspension reason used when an
// installation survives a restore but its immutable persona version does not.
const MissingVersionAfterRestore = "PERSONA_VERSION_MISSING_AFTER_RESTORE"

type LifecycleState string

const (
	StateDraft     LifecycleState = "DRAFT"
	StateInReview  LifecycleState = "IN_REVIEW"
	StatePublished LifecycleState = "PUBLISHED"
	StateSuspended LifecycleState = "SUSPENDED"
	StateRetired   LifecycleState = "RETIRED"
)

type OwnerRole string

const (
	BusinessOwner    OwnerRole = "BUSINESS_OWNER"
	TechnicalSteward OwnerRole = "TECHNICAL_STEWARD"
)

type InstallationState string

const (
	InstallationActive    InstallationState = "ACTIVE"
	InstallationSuspended InstallationState = "SUSPENDED"
	InstallationRetired   InstallationState = "RETIRED"
	InstallationKilled    InstallationState = "KILLED"
)

// ConversationClass is the chat-owned classification captured at placement.
type ConversationClass string

const (
	ConversationPublic       ConversationClass = "PUBLIC"
	ConversationPrivate      ConversationClass = "PRIVATE"
	ConversationGroupDM      ConversationClass = "GROUP_DM"
	ConversationOneToOne     ConversationClass = "ONE_TO_ONE"
	ConversationExternal     ConversationClass = "EXTERNAL"
	ConversationCrossCompany ConversationClass = "CROSS_COMPANY"
)

// ChannelPolicy is the normalized durable projection of an installation's
// channel-owned outer bound.
type ChannelPolicy struct {
	PlacementClass            string              `json:"placement_class,omitempty"`
	MaxTier                   string              `json:"max_tier"`
	AllowedDataClasses        []string            `json:"allowed_data_classes"`
	AlwaysPrivate             bool                `json:"always_private"`
	ConversationSearchAllowed bool                `json:"conversation_search_allowed"`
	AllowedChannelClasses     []ConversationClass `json:"allowed_channel_classes"`
	AllowExternalMembers      bool                `json:"allow_external_members"`
	AllowCrossCompanyMembers  bool                `json:"allow_cross_company_members"`
}

// PersonaVersion is the immutable profile image. Profile is the P3 persona
// payload and is kept as JSON so this store does not import the still-evolving
// internal/agentpersona package.
type PersonaVersion struct {
	TenantID      values.TenantId
	PersonaID     string
	Version       int64
	AgentVersion  string
	Handle        string
	DisplayName   string
	Profile       json.RawMessage
	ContentDigest string
	CreatedAt     time.Time
}

type PersonaOwner struct {
	TenantID    values.TenantId
	PersonaID   string
	Role        OwnerRole
	PrincipalID string
	AssignedBy  string
	AssignedAt  time.Time
}

type PersonaInstallation struct {
	TenantID          values.TenantId
	InstallationID    string
	PersonaID         string
	PersonaVersion    int64
	ConversationID    string
	ConversationClass ConversationClass
	InstallerID       string
	ChannelPolicy     ChannelPolicy
	State             InstallationState
	SuspensionReason  string
	Revision          int64
	RevocationEpoch   int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type LifecycleEvent struct {
	Sequence                int64
	EventID                 string
	TenantID                values.TenantId
	PersonaID               string
	PersonaVersion          int64
	From                    LifecycleState
	To                      LifecycleState
	Reason                  string
	ActorID                 string
	OccurredAt              time.Time
	ProfileDigest           string
	ReviewDigest            string
	ReviewerID              string
	EvaluationDigest        string
	EvaluationProfileDigest string
	EvaluationSuiteDigest   string
}

// PublicationEvidence is resolved from trusted review and evaluation
// authorities. Callers provide references only; the store never accepts
// caller-asserted pass/fresh booleans.
type PublicationEvidence struct {
	ReviewID        string
	EvaluationRunID string
}

// ReviewEvidenceSource resolves an immutable approval and revalidates the
// reviewer's current persona-review grant for this exact profile digest. It
// receives the publication transaction so grant revocation can be serialized
// with publication by locking the authoritative grant row until commit.
type ReviewEvidenceSource interface {
	ResolvePersonaReview(context.Context, dbport.Tx, values.TenantId, string, int64, string, string) (VerifiedReview, error)
}

// EvaluationEvidenceSource resolves a recorded evaluation by tenant-scoped ID.
type EvaluationEvidenceSource interface {
	ResolvePersonaEvaluation(context.Context, dbport.Tx, values.TenantId, string, string, int64, string) (VerifiedEvaluation, error)
}

// VerifiedReview is returned by the trusted review authority, not constructed
// from publication request fields.
type VerifiedReview struct {
	ReviewID, TenantID, PersonaID, ReviewerID, Permission, Decision string
	PersonaVersion                                                  int64
	ProfileDigest, ReviewDigest                                     string
	GrantCurrent                                                    bool
}

// VerifiedEvaluation is returned by the trusted evaluation authority.
type VerifiedEvaluation struct {
	RunID, TenantID, PersonaID            string
	PersonaVersion                        int64
	ProfileDigest, SuiteDigest, RunDigest string
	Passed, Fresh                         bool
}

type Reconciliation struct {
	InstallationID string
	PersonaID      string
	PersonaVersion int64
	State          InstallationState
	Reason         string
}

// DB is the transaction opener required by Store.
type DB interface{ dbport.Beginner }

// Store is a tenant-agnostic factory for scoped persona stores.
type Store struct {
	db          DB
	tenantUUID  func(values.TenantId) uuid.UUID
	reviews     ReviewEvidenceSource
	evaluations EvaluationEvidenceSource
}

// TenantStore is the tenant-bound persistence adapter.
type TenantStore struct {
	db          DB
	tenant      values.TenantId
	tenantID    uuid.UUID
	reviews     ReviewEvidenceSource
	evaluations EvaluationEvidenceSource
}

func New(db DB, tenantUUID func(values.TenantId) uuid.UUID) (*Store, error) {
	if db == nil || tenantUUID == nil {
		return nil, fmt.Errorf("%w: database and tenant mapper are required", ErrInvalid)
	}
	return &Store{db: db, tenantUUID: tenantUUID}, nil
}

// NewWithReviewAuthority enables verified review reads without permitting
// publication when no evaluation verification authority is configured.
func NewWithReviewAuthority(db DB, tenantUUID func(values.TenantId) uuid.UUID, reviews ReviewEvidenceSource) (*Store, error) {
	if reviews == nil {
		return nil, fmt.Errorf("%w: trusted review authority is required", ErrInvalid)
	}
	store, err := New(db, tenantUUID)
	if err != nil {
		return nil, err
	}
	store.reviews = reviews
	return store, nil
}

// NewWithPublicationAuthorities constructs a store whose publication methods
// use the review and evaluation authorities supplied by the trusted
// composition root. These dependencies are not accepted per Publish call.
func NewWithPublicationAuthorities(db DB, tenantUUID func(values.TenantId) uuid.UUID, reviews ReviewEvidenceSource, evaluations EvaluationEvidenceSource) (*Store, error) {
	if reviews == nil || evaluations == nil {
		return nil, fmt.Errorf("%w: trusted publication authorities are required", ErrInvalid)
	}
	store, err := New(db, tenantUUID)
	if err != nil {
		return nil, err
	}
	store.reviews, store.evaluations = reviews, evaluations
	return store, nil
}

func (s *Store) ForTenant(ctx context.Context, tenant values.TenantId) (*TenantStore, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is required", ErrInvalid)
	}
	return s.Scoped(tenant)
}

func (s *Store) Scoped(tenant values.TenantId) (*TenantStore, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalid)
	}
	if strings.TrimSpace(string(tenant)) == "" || strings.TrimSpace(string(tenant)) != string(tenant) {
		return nil, fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	id := s.tenantUUID(tenant)
	if id == uuid.Nil {
		return nil, fmt.Errorf("%w: unknown tenant", ErrInvalid)
	}
	return &TenantStore{db: s.db, tenant: tenant, tenantID: id, reviews: s.reviews, evaluations: s.evaluations}, nil
}

func (s *TenantStore) begin(ctx context.Context) (dbport.Tx, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: context is required", ErrInvalid)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: begin: %v", ErrInvalid, err)
	}
	if err := tenancy.WithTenant(ctx, tx, s.tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

// CreateDraft atomically creates an immutable persona version, its required
// business owner and technical steward, and the initial DRAFT lifecycle event.
// The actor and timestamp are supplied by the caller so creation never invents
// audit identity or time.
func (s *TenantStore) CreateDraft(ctx context.Context, version PersonaVersion, businessOwner, technicalSteward PersonaOwner, actorID string, occurredAt time.Time) error {
	if err := validateVersion(version, s.tenant); err != nil {
		return err
	}
	if strings.TrimSpace(actorID) == "" || occurredAt.IsZero() {
		return fmt.Errorf("%w: actor and creation timestamp are required", ErrInvalid)
	}
	at := occurredAt.UTC()
	for _, input := range []struct {
		owner PersonaOwner
		role  OwnerRole
	}{{businessOwner, BusinessOwner}, {technicalSteward, TechnicalSteward}} {
		if input.owner.Role != input.role {
			return fmt.Errorf("%w: required owner role %q", ErrInvalid, input.role)
		}
		if input.owner.PersonaID != version.PersonaID {
			return fmt.Errorf("%w: owner persona does not match version", ErrInvalid)
		}
		input.owner.AssignedBy = actorID
		input.owner.AssignedAt = at
		if err := validateOwner(input.owner, s.tenant); err != nil {
			return err
		}
	}
	profile, err := json.Marshal(version.Profile)
	if err != nil {
		return fmt.Errorf("%w: profile: %v", ErrInvalid, err)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	n, err := tx.Exec(ctx, `INSERT INTO persona_versions
		(tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9) ON CONFLICT DO NOTHING`,
		s.tenantID, version.PersonaID, version.Version, version.AgentVersion, version.Handle,
		version.DisplayName, string(profile), version.ContentDigest, at)
	if err != nil {
		return fmt.Errorf("agentpersonastore: create draft version: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: persona version already exists", ErrConflict)
	}
	for _, owner := range []PersonaOwner{businessOwner, technicalSteward} {
		n, err = tx.Exec(ctx, `INSERT INTO persona_owners
			(tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at)
			VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
			s.tenantID, version.PersonaID, owner.Role, owner.PrincipalID, actorID, at)
		if err != nil {
			return fmt.Errorf("agentpersonastore: create draft owner: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("%w: persona owner already exists", ErrConflict)
		}
	}
	n, err = tx.Exec(ctx, `INSERT INTO persona_lifecycle_events
		(tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at)
		VALUES ($1,$2,$3,$4,'',$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
		s.tenantID, uuid.NewString(), version.PersonaID, version.Version, StateDraft, "Initial draft created", actorID, at)
	if err != nil {
		return fmt.Errorf("agentpersonastore: create draft lifecycle: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: initial lifecycle event already exists", ErrConflict)
	}
	return commit(ctx, tx)
}

func (s *TenantStore) PutVersion(ctx context.Context, version PersonaVersion) error {
	if err := validateVersion(version, s.tenant); err != nil {
		return err
	}
	profile, err := json.Marshal(version.Profile)
	if err != nil {
		return fmt.Errorf("%w: profile: %v", ErrInvalid, err)
	}
	created := version.CreatedAt.UTC()
	if created.IsZero() {
		created = time.Now().UTC()
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	n, err := tx.Exec(ctx, `INSERT INTO persona_versions
		(tenant_id,persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9) ON CONFLICT DO NOTHING`,
		s.tenantID, version.PersonaID, version.Version, version.AgentVersion, version.Handle,
		version.DisplayName, string(profile), version.ContentDigest, created)
	if err != nil {
		return fmt.Errorf("agentpersonastore: put version: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: persona version already exists", ErrConflict)
	}
	return commit(ctx, tx)
}

func (s *TenantStore) GetVersion(ctx context.Context, personaID string, version int64) (PersonaVersion, error) {
	if strings.TrimSpace(personaID) == "" || version <= 0 {
		return PersonaVersion{}, fmt.Errorf("%w: persona id and positive version are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PersonaVersion{}, err
	}
	defer tx.Rollback(ctx)
	got, err := scanVersion(tx.QueryRow(ctx, `SELECT persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at
		FROM persona_versions WHERE tenant_id=$1 AND persona_id=$2 AND version=$3`, s.tenantID, personaID, version), s.tenant)
	if errors.Is(err, dbport.ErrNoRows) {
		return PersonaVersion{}, fmt.Errorf("%w: persona version %s/%d", ErrNotFound, personaID, version)
	}
	if err != nil {
		return PersonaVersion{}, err
	}
	return got, commit(ctx, tx)
}

func (s *TenantStore) ListVersions(ctx context.Context, personaID string) ([]PersonaVersion, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at
		FROM persona_versions WHERE tenant_id=$1 AND ($2='' OR persona_id=$2) ORDER BY persona_id COLLATE "C", version`, s.tenantID, personaID)
	if err != nil {
		return nil, fmt.Errorf("agentpersonastore: list versions: %w", err)
	}
	defer rows.Close()
	var out []PersonaVersion
	for rows.Next() {
		item, err := scanVersion(rows, s.tenant)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentpersonastore: list versions rows: %w", err)
	}
	if err := commit(ctx, tx); err != nil {
		return nil, err
	}
	return out, nil
}

// ListPublished returns persona versions whose latest lifecycle event is
// PUBLISHED. The version image and lifecycle projection are read by one
// tenant-scoped statement inside one transaction, so a caller cannot observe a
// version separately from the lifecycle state that made it invocable.
func (s *TenantStore) ListPublished(ctx context.Context) ([]PersonaVersion, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `WITH current_lifecycle AS (
		SELECT DISTINCT ON (persona_id, persona_version)
			persona_id, persona_version, to_state, profile_digest, review_digest, reviewer_id, evaluation_digest, evaluation_profile_digest, evaluation_suite_digest
		FROM persona_lifecycle_events
		WHERE tenant_id=$1
		ORDER BY persona_id, persona_version, event_sequence DESC
	)
	SELECT v.persona_id,v.version,v.agent_version,v.handle,v.display_name,v.profile,v.content_digest,v.created_at
	FROM persona_versions AS v
	JOIN current_lifecycle AS lifecycle
		ON lifecycle.persona_id=v.persona_id AND lifecycle.persona_version=v.version
	WHERE v.tenant_id=$1 AND lifecycle.to_state='PUBLISHED' AND lifecycle.profile_digest=v.content_digest AND lifecycle.profile_digest<>'' AND lifecycle.review_digest<>'' AND lifecycle.reviewer_id<>'' AND lifecycle.evaluation_digest<>'' AND lifecycle.evaluation_profile_digest=lifecycle.profile_digest AND lifecycle.evaluation_suite_digest<>''
	ORDER BY v.persona_id COLLATE "C", v.version`, s.tenantID)
	if err != nil {
		return nil, fmt.Errorf("agentpersonastore: list published personas: %w", err)
	}
	defer rows.Close()
	var out []PersonaVersion
	for rows.Next() {
		item, err := scanVersion(rows, s.tenant)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentpersonastore: list published personas rows: %w", err)
	}
	if err := commit(ctx, tx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *TenantStore) PutOwner(ctx context.Context, owner PersonaOwner) error {
	if err := validateOwner(owner, s.tenant); err != nil {
		return err
	}
	at := owner.AssignedAt.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM persona_versions WHERE tenant_id=$1 AND persona_id=$2)`, s.tenantID, owner.PersonaID).Scan(&exists); err != nil {
		return fmt.Errorf("agentpersonastore: check owner persona: %w", err)
	}
	if !exists {
		return fmt.Errorf("%w: owner persona does not exist", ErrNotFound)
	}
	_, err = tx.Exec(ctx, `INSERT INTO persona_owners (tenant_id,persona_id,owner_role,principal_id,assigned_by,assigned_at)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (tenant_id,persona_id,owner_role)
		DO UPDATE SET principal_id=EXCLUDED.principal_id,assigned_by=EXCLUDED.assigned_by,assigned_at=EXCLUDED.assigned_at`,
		s.tenantID, owner.PersonaID, owner.Role, owner.PrincipalID, owner.AssignedBy, at)
	if err != nil {
		return fmt.Errorf("agentpersonastore: put owner: %w", err)
	}
	return commit(ctx, tx)
}

func (s *TenantStore) ListOwners(ctx context.Context, personaID string) ([]PersonaOwner, error) {
	if strings.TrimSpace(personaID) == "" {
		return nil, fmt.Errorf("%w: persona id is required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT persona_id,owner_role,principal_id,assigned_by,assigned_at
		FROM persona_owners WHERE tenant_id=$1 AND persona_id=$2 ORDER BY owner_role`, s.tenantID, personaID)
	if err != nil {
		return nil, fmt.Errorf("agentpersonastore: list owners: %w", err)
	}
	defer rows.Close()
	var out []PersonaOwner
	for rows.Next() {
		var item PersonaOwner
		if err := rows.Scan(&item.PersonaID, &item.Role, &item.PrincipalID, &item.AssignedBy, &item.AssignedAt); err != nil {
			return nil, fmt.Errorf("agentpersonastore: scan owner: %w", err)
		}
		item.TenantID, item.AssignedAt = s.tenant, item.AssignedAt.UTC()
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, commit(ctx, tx)
}

func (s *TenantStore) AppendLifecycle(ctx context.Context, event LifecycleEvent) error {
	if err := validateEvent(event, s.tenant); err != nil {
		return err
	}
	if event.To == StatePublished {
		return fmt.Errorf("%w: use Publish with trusted review and evaluation authorities", ErrPublicationEvidenceRequired)
	}
	return s.appendLifecycle(ctx, event, nil)
}

// Publish atomically appends a PUBLISHED lifecycle event after resolving
// tenant-scoped, immutable review and evaluation evidence in the same
// transaction as the lifecycle change. The reviewer must
// be distinct from the recorded business owner, and both evidence records
// must pin the stored version digest. Authorities are mandatory and fail
// closed when unavailable.
func (s *TenantStore) Publish(ctx context.Context, event LifecycleEvent, evidence PublicationEvidence) error {
	if err := validateEvent(event, s.tenant); err != nil {
		return err
	}
	if event.To != StatePublished || (event.From != StateInReview && event.From != StateSuspended) || s.reviews == nil || s.evaluations == nil || strings.TrimSpace(evidence.ReviewID) == "" || strings.TrimSpace(evidence.EvaluationRunID) == "" {
		return fmt.Errorf("%w: publication request and authorities are required", ErrPublicationEvidenceRequired)
	}
	return s.appendLifecycle(ctx, event, func(tx dbport.Tx, persisted *LifecycleEvent) error {
		version, err := scanVersion(tx.QueryRow(ctx, `SELECT persona_id,version,agent_version,handle,display_name,profile,content_digest,created_at FROM persona_versions WHERE tenant_id=$1 AND persona_id=$2 AND version=$3`, s.tenantID, event.PersonaID, event.PersonaVersion), s.tenant)
		if errors.Is(err, dbport.ErrNoRows) {
			return fmt.Errorf("%w: lifecycle version does not exist", ErrNotFound)
		}
		if err != nil {
			return err
		}
		review, err := s.reviews.ResolvePersonaReview(ctx, tx, s.tenant, event.PersonaID, event.PersonaVersion, version.ContentDigest, evidence.ReviewID)
		if err != nil {
			return fmt.Errorf("%w: resolve independent review: %v", ErrPublicationEvidenceRequired, err)
		}
		evaluation, err := s.evaluations.ResolvePersonaEvaluation(ctx, tx, s.tenant, evidence.EvaluationRunID, event.PersonaID, event.PersonaVersion, version.ContentDigest)
		if err != nil {
			return fmt.Errorf("%w: resolve evaluation: %v", ErrPublicationEvidenceRequired, err)
		}
		var businessOwner string
		ownerErr := tx.QueryRow(ctx, `SELECT principal_id FROM persona_owners WHERE tenant_id=$1 AND persona_id=$2 AND owner_role=$3`, s.tenantID, event.PersonaID, BusinessOwner).Scan(&businessOwner)
		if ownerErr != nil || strings.TrimSpace(businessOwner) == "" {
			if errors.Is(ownerErr, dbport.ErrNoRows) || (ownerErr == nil && strings.TrimSpace(businessOwner) == "") {
				return fmt.Errorf("%w: durable business owner is required", ErrPublicationEvidenceRequired)
			}
			return fmt.Errorf("agentpersonastore: read business owner for publication: %w", ownerErr)
		}
		var profileOwner struct {
			Owner string `json:"owner"`
		}
		if err := json.Unmarshal(version.Profile, &profileOwner); err != nil {
			return fmt.Errorf("%w: persona profile owner is invalid", ErrInvalid)
		}
		if strings.TrimSpace(profileOwner.Owner) == "" || profileOwner.Owner != businessOwner {
			return fmt.Errorf("%w: profile owner and durable business owner must match", ErrPublicationEvidenceRequired)
		}
		if review.ReviewID != evidence.ReviewID || review.TenantID != string(s.tenant) || review.PersonaID != event.PersonaID || review.PersonaVersion != event.PersonaVersion || review.ProfileDigest != version.ContentDigest || strings.TrimSpace(review.ReviewDigest) == "" || review.Permission != "persona:review" || review.Decision != "APPROVE" || !review.GrantCurrent || strings.TrimSpace(review.ReviewerID) == "" || review.ReviewerID == businessOwner || review.ReviewerID == profileOwner.Owner {
			return fmt.Errorf("%w: review does not prove current independent approval of this version", ErrPublicationEvidenceRequired)
		}
		if evaluation.RunID != evidence.EvaluationRunID || evaluation.TenantID != string(s.tenant) || evaluation.PersonaID != event.PersonaID || evaluation.PersonaVersion != event.PersonaVersion || evaluation.ProfileDigest != version.ContentDigest || strings.TrimSpace(evaluation.SuiteDigest) == "" || strings.TrimSpace(evaluation.RunDigest) == "" || !evaluation.Passed || !evaluation.Fresh {
			return fmt.Errorf("%w: evaluation does not prove a fresh passing run for this version", ErrPublicationEvidenceRequired)
		}
		persisted.ActorID = review.ReviewerID
		persisted.ProfileDigest = version.ContentDigest
		persisted.ReviewDigest = review.ReviewDigest
		persisted.ReviewerID = review.ReviewerID
		persisted.EvaluationDigest = evaluation.RunDigest
		persisted.EvaluationProfileDigest = evaluation.ProfileDigest
		persisted.EvaluationSuiteDigest = evaluation.SuiteDigest
		return nil
	})
}

func (s *TenantStore) appendLifecycle(ctx context.Context, event LifecycleEvent, verify func(dbport.Tx, *LifecycleEvent) error) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var lockTaken bool
	if err := tx.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2 || ':' || $3::text, 0)) IS NULL`,
		string(s.tenant), event.PersonaID, fmt.Sprintf("%d", event.PersonaVersion)).Scan(&lockTaken); err != nil {
		return fmt.Errorf("agentpersonastore: lock lifecycle: %w", err)
	}
	var lockedPersona string
	if err := tx.QueryRow(ctx, `SELECT persona_id FROM persona_versions WHERE tenant_id=$1 AND persona_id=$2 AND version=$3`, s.tenantID, event.PersonaID, event.PersonaVersion).Scan(&lockedPersona); errors.Is(err, dbport.ErrNoRows) {
		return fmt.Errorf("%w: lifecycle version does not exist", ErrNotFound)
	} else if err != nil {
		return fmt.Errorf("agentpersonastore: check lifecycle version: %w", err)
	}
	var current LifecycleState
	err = tx.QueryRow(ctx, `SELECT to_state FROM persona_lifecycle_events WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 ORDER BY event_sequence DESC LIMIT 1`, s.tenantID, event.PersonaID, event.PersonaVersion).Scan(&current)
	if errors.Is(err, dbport.ErrNoRows) {
		current = ""
	} else if err != nil {
		return fmt.Errorf("agentpersonastore: read lifecycle: %w", err)
	}
	if event.From != current || !validTransition(current, event.To) {
		return fmt.Errorf("%w: lifecycle transition %q -> %q is not valid from %q", ErrConflict, event.From, event.To, current)
	}
	if verify != nil {
		if err := verify(tx, &event); err != nil {
			return err
		}
	}
	at := event.OccurredAt.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	n, err := tx.Exec(ctx, `INSERT INTO persona_lifecycle_events
		(tenant_id,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT DO NOTHING`,
		s.tenantID, event.EventID, event.PersonaID, event.PersonaVersion, event.From, event.To, event.Reason, event.ActorID, at,
		event.ProfileDigest, event.ReviewDigest, event.ReviewerID, event.EvaluationDigest, event.EvaluationProfileDigest, event.EvaluationSuiteDigest)
	if err != nil {
		return fmt.Errorf("agentpersonastore: append lifecycle: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: lifecycle event already exists", ErrConflict)
	}
	return commit(ctx, tx)
}

func (s *TenantStore) Lifecycle(ctx context.Context, personaID string, version int64) (LifecycleState, error) {
	if strings.TrimSpace(personaID) == "" || version <= 0 {
		return "", fmt.Errorf("%w: persona id and positive version are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var state LifecycleState
	err = tx.QueryRow(ctx, `SELECT to_state FROM persona_lifecycle_events WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 ORDER BY event_sequence DESC LIMIT 1`, s.tenantID, personaID, version).Scan(&state)
	if errors.Is(err, dbport.ErrNoRows) {
		return "", fmt.Errorf("%w: lifecycle for %s/%d", ErrNotFound, personaID, version)
	}
	if err != nil {
		return "", err
	}
	return state, commit(ctx, tx)
}

func (s *TenantStore) ListLifecycle(ctx context.Context, personaID string, version int64) ([]LifecycleEvent, error) {
	if strings.TrimSpace(personaID) == "" || version <= 0 {
		return nil, fmt.Errorf("%w: persona id and positive version are required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT event_sequence,event_id,persona_id,persona_version,from_state,to_state,reason,actor_id,occurred_at,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest
		FROM persona_lifecycle_events WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 ORDER BY event_sequence`, s.tenantID, personaID, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LifecycleEvent
	for rows.Next() {
		var item LifecycleEvent
		if err := rows.Scan(&item.Sequence, &item.EventID, &item.PersonaID, &item.PersonaVersion, &item.From, &item.To, &item.Reason, &item.ActorID, &item.OccurredAt, &item.ProfileDigest, &item.ReviewDigest, &item.ReviewerID, &item.EvaluationDigest, &item.EvaluationProfileDigest, &item.EvaluationSuiteDigest); err != nil {
			return nil, fmt.Errorf("agentpersonastore: scan lifecycle: %w", err)
		}
		item.TenantID, item.OccurredAt = s.tenant, item.OccurredAt.UTC()
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, commit(ctx, tx)
}

func (s *TenantStore) Install(ctx context.Context, installation PersonaInstallation) error {
	if err := validateInstallation(installation, s.tenant); err != nil {
		return err
	}
	created, updated := installation.CreatedAt.UTC(), installation.UpdatedAt.UTC()
	if created.IsZero() {
		created = time.Now().UTC()
	}
	if updated.IsZero() {
		updated = created
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var lockTaken bool
	if err := tx.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2 || ':' || $3::text, 0)) IS NULL`,
		string(s.tenant), installation.PersonaID, fmt.Sprintf("%d", installation.PersonaVersion)).Scan(&lockTaken); err != nil {
		return fmt.Errorf("agentpersonastore: lock installation lifecycle: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('persona-installation:' || $1 || ':' || $2, 0))`, s.tenantID.String(), installation.ConversationID); err != nil {
		return fmt.Errorf("agentpersonastore: lock installation conversation: %w", err)
	}
	if installation.State == InstallationActive {
		var count int64
		var duplicate bool
		if err := tx.QueryRow(ctx, `SELECT count(*),COALESCE(bool_or(persona_id=$3),false)
			FROM persona_installations WHERE tenant_id=$1 AND conversation_id=$2 AND state='ACTIVE'`,
			s.tenantID, installation.ConversationID, installation.PersonaID).Scan(&count, &duplicate); err != nil {
			return fmt.Errorf("agentpersonastore: count conversation installations: %w", err)
		}
		if duplicate || count >= 5 {
			return fmt.Errorf("%w: conversation already has this persona or five active personas", ErrConflict)
		}
	}
	var versionDigest string
	err = tx.QueryRow(ctx, `SELECT content_digest FROM persona_versions WHERE tenant_id=$1 AND persona_id=$2 AND version=$3`, s.tenantID, installation.PersonaID, installation.PersonaVersion).Scan(&versionDigest)
	if errors.Is(err, dbport.ErrNoRows) {
		return fmt.Errorf("%w: installation version does not exist", ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("agentpersonastore: check installation version: %w", err)
	}
	var current LifecycleState
	var profileDigest, reviewDigest, reviewerID, evaluationDigest, evaluationProfileDigest, evaluationSuiteDigest string
	err = tx.QueryRow(ctx, `SELECT to_state,profile_digest,review_digest,reviewer_id,evaluation_digest,evaluation_profile_digest,evaluation_suite_digest FROM persona_lifecycle_events WHERE tenant_id=$1 AND persona_id=$2 AND persona_version=$3 ORDER BY event_sequence DESC LIMIT 1`,
		s.tenantID, installation.PersonaID, installation.PersonaVersion).Scan(&current, &profileDigest, &reviewDigest, &reviewerID, &evaluationDigest, &evaluationProfileDigest, &evaluationSuiteDigest)
	if errors.Is(err, dbport.ErrNoRows) || (err == nil && (current != StatePublished || profileDigest != versionDigest || reviewDigest == "" || reviewerID == "" || evaluationDigest == "" || evaluationProfileDigest != profileDigest || evaluationSuiteDigest == "")) {
		return fmt.Errorf("%w: installation version is not currently published", ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("agentpersonastore: read installation lifecycle: %w", err)
	}
	n, err := tx.Exec(ctx, `INSERT INTO persona_installations
		(tenant_id,installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,suspension_reason,revision,revocation_epoch,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING`,
		s.tenantID, installation.InstallationID, installation.PersonaID, installation.PersonaVersion,
		installation.ConversationID, installation.ConversationClass, installation.InstallerID, marshalChannelPolicy(installation.ChannelPolicy),
		installation.State, installation.SuspensionReason, installation.Revision, installation.RevocationEpoch, created, updated)
	if err != nil {
		return fmt.Errorf("agentpersonastore: install: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: installation already exists", ErrConflict)
	}
	return commit(ctx, tx)
}

func (s *TenantStore) GetInstallation(ctx context.Context, id string) (PersonaInstallation, error) {
	if strings.TrimSpace(id) == "" {
		return PersonaInstallation{}, fmt.Errorf("%w: installation id is required", ErrInvalid)
	}
	tx, err := s.begin(ctx)
	if err != nil {
		return PersonaInstallation{}, err
	}
	defer tx.Rollback(ctx)
	var item PersonaInstallation
	var policyJSON []byte
	err = tx.QueryRow(ctx, `SELECT installation_id,persona_id,persona_version,conversation_id,conversation_class,installer_id,channel_policy,state,suspension_reason,revision,revocation_epoch,created_at,updated_at
		FROM persona_installations WHERE tenant_id=$1 AND installation_id=$2`, s.tenantID, id).Scan(
		&item.InstallationID, &item.PersonaID, &item.PersonaVersion, &item.ConversationID, &item.ConversationClass, &item.InstallerID,
		&policyJSON, &item.State, &item.SuspensionReason, &item.Revision, &item.RevocationEpoch, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return PersonaInstallation{}, fmt.Errorf("%w: installation %s", ErrNotFound, id)
	}
	if err != nil {
		return PersonaInstallation{}, err
	}
	if err := json.Unmarshal(policyJSON, &item.ChannelPolicy); err != nil {
		return PersonaInstallation{}, fmt.Errorf("agentpersonastore: decode installation policy: %w", err)
	}
	item.TenantID, item.CreatedAt, item.UpdatedAt = s.tenant, item.CreatedAt.UTC(), item.UpdatedAt.UTC()
	return item, commit(ctx, tx)
}

func (s *TenantStore) ReconcileInstallations(ctx context.Context) ([]Reconciliation, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT i.installation_id,i.persona_id,i.persona_version,i.state
		FROM persona_installations i LEFT JOIN persona_versions v ON v.tenant_id=i.tenant_id
		AND v.persona_id=i.persona_id AND v.version=i.persona_version
		WHERE i.tenant_id=$1 AND i.state NOT IN ('RETIRED','KILLED') AND v.persona_id IS NULL
		AND (i.state<>'SUSPENDED' OR i.suspension_reason<>$2) FOR UPDATE OF i`, s.tenantID, MissingVersionAfterRestore)
	if err != nil {
		return nil, fmt.Errorf("agentpersonastore: find orphan installations: %w", err)
	}
	defer rows.Close()
	var out []Reconciliation
	for rows.Next() {
		var item Reconciliation
		var state InstallationState
		if err := rows.Scan(&item.InstallationID, &item.PersonaID, &item.PersonaVersion, &state); err != nil {
			return nil, err
		}
		item.State = state
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		item := &out[i]
		if _, err := tx.Exec(ctx, `UPDATE persona_installations SET state='SUSPENDED',suspension_reason=$3,
			revision=revision+1,revocation_epoch=revocation_epoch+1,updated_at=now()
			WHERE tenant_id=$1 AND installation_id=$2`, s.tenantID, item.InstallationID, MissingVersionAfterRestore); err != nil {
			return nil, fmt.Errorf("agentpersonastore: suspend orphan installation: %w", err)
		}
		item.State, item.Reason = InstallationSuspended, MissingVersionAfterRestore
	}
	if err := commit(ctx, tx); err != nil {
		return nil, err
	}
	return out, nil
}

func validateVersion(v PersonaVersion, tenant values.TenantId) error {
	if v.TenantID != tenant || strings.TrimSpace(v.PersonaID) == "" || v.Version <= 0 || strings.TrimSpace(v.AgentVersion) == "" || strings.TrimSpace(v.Handle) == "" || strings.TrimSpace(v.DisplayName) == "" || len(v.Profile) == 0 || strings.TrimSpace(v.ContentDigest) == "" {
		return fmt.Errorf("%w: incomplete persona version", ErrInvalid)
	}
	var object struct {
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(v.Profile, &object); err != nil {
		return fmt.Errorf("%w: profile must be a JSON object", ErrInvalid)
	}
	if strings.TrimSpace(object.Owner) == "" || strings.TrimSpace(object.Owner) != object.Owner {
		return fmt.Errorf("%w: persona profile owner is required", ErrInvalid)
	}
	return nil
}

func validateOwner(o PersonaOwner, tenant values.TenantId) error {
	if o.TenantID != tenant || strings.TrimSpace(o.PersonaID) == "" || (o.Role != BusinessOwner && o.Role != TechnicalSteward) || strings.TrimSpace(o.PrincipalID) == "" || strings.TrimSpace(o.AssignedBy) == "" {
		return fmt.Errorf("%w: incomplete persona owner", ErrInvalid)
	}
	return nil
}

func validateInstallation(i PersonaInstallation, tenant values.TenantId) error {
	if i.TenantID != tenant || strings.TrimSpace(i.InstallationID) == "" || strings.TrimSpace(i.PersonaID) == "" || i.PersonaVersion <= 0 || strings.TrimSpace(i.ConversationID) == "" || !validConversationClass(i.ConversationClass) || strings.TrimSpace(i.InstallerID) == "" || (i.State != InstallationActive && i.State != InstallationSuspended && i.State != InstallationRetired && i.State != InstallationKilled) || i.Revision <= 0 || i.RevocationEpoch <= 0 || !validChannelPolicy(i.ChannelPolicy) {
		return fmt.Errorf("%w: incomplete persona installation", ErrInvalid)
	}
	if i.State == InstallationSuspended && strings.TrimSpace(i.SuspensionReason) == "" {
		return fmt.Errorf("%w: suspended installation needs a reason", ErrInvalid)
	}
	return nil
}

func validConversationClass(class ConversationClass) bool {
	switch class {
	case ConversationPublic, ConversationPrivate, ConversationGroupDM, ConversationOneToOne, ConversationExternal, ConversationCrossCompany:
		return true
	default:
		return false
	}
}

func validChannelPolicy(policy ChannelPolicy) bool {
	if policy.AllowedDataClasses == nil || policy.AllowedChannelClasses == nil {
		return false
	}
	switch policy.MaxTier {
	case "T0", "T1", "T2", "T3", "T4":
	default:
		return false
	}
	for _, class := range policy.AllowedChannelClasses {
		if !validConversationClass(class) {
			return false
		}
	}
	for _, dataClass := range policy.AllowedDataClasses {
		if strings.TrimSpace(dataClass) == "" || strings.TrimSpace(dataClass) != dataClass {
			return false
		}
	}
	return true
}

func marshalChannelPolicy(policy ChannelPolicy) []byte {
	encoded, _ := json.Marshal(policy)
	return encoded
}

func validateEvent(e LifecycleEvent, tenant values.TenantId) error {
	if e.TenantID != tenant || strings.TrimSpace(e.EventID) == "" || strings.TrimSpace(e.PersonaID) == "" || e.PersonaVersion <= 0 || strings.TrimSpace(e.Reason) == "" || strings.TrimSpace(e.ActorID) == "" || !validState(e.To) {
		return fmt.Errorf("%w: incomplete lifecycle event", ErrInvalid)
	}
	return nil
}

func validState(s LifecycleState) bool {
	return s == StateDraft || s == StateInReview || s == StatePublished || s == StateSuspended || s == StateRetired
}

func validTransition(from, to LifecycleState) bool {
	if from == "" {
		return to == StateDraft
	}
	switch from {
	case StateDraft:
		return to == StateInReview || to == StateRetired
	case StateInReview:
		return to == StatePublished || to == StateDraft || to == StateRetired
	case StatePublished:
		return to == StateSuspended || to == StateRetired
	case StateSuspended:
		return to == StatePublished || to == StateRetired
	case StateRetired:
		return false
	default:
		return false
	}
}

type scanner interface{ Scan(...any) error }

func scanVersion(row scanner, tenant values.TenantId) (PersonaVersion, error) {
	var v PersonaVersion
	if err := row.Scan(&v.PersonaID, &v.Version, &v.AgentVersion, &v.Handle, &v.DisplayName, &v.Profile, &v.ContentDigest, &v.CreatedAt); err != nil {
		return PersonaVersion{}, fmt.Errorf("agentpersonastore: scan version: %w", err)
	}
	v.TenantID, v.CreatedAt = tenant, v.CreatedAt.UTC()
	return v, nil
}

func commit(ctx context.Context, tx dbport.Tx) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agentpersonastore: commit: %w", err)
	}
	return nil
}
