// Package agentmodelpolicystore retains immutable agent policy, schema and
// evaluation contracts in the isolated agent database.
package agentmodelpolicystore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

const (
	AuthorityRole   = "hcmnext_agent_model_policy_authority"
	ModelPolicy     = "model_policy"
	OutputSchema    = "output_schema"
	EvaluationSuite = "evaluation_suite"
)

var (
	ErrInvalid     = errors.New("agentmodelpolicystore: invalid immutable contract")
	ErrUnavailable = errors.New("agentmodelpolicystore: current contract unavailable")
	ErrAuthority   = errors.New("agentmodelpolicystore: verified publication authority required")
	ErrConflict    = errors.New("agentmodelpolicystore: immutable contract or source revision conflict")
)

type Record struct {
	Kind      string
	Reference agentmanifest.Reference
	Content   []byte
}

// Authority is a signed source assertion retained verbatim. Verification is
// delegated to the configured authority, never inferred from a caller's flags.
type Authority struct {
	SourceID       string    `json:"source_id"`
	SourceRevision uint64    `json:"source_revision"`
	KeyID          string    `json:"key_id"`
	Document       []byte    `json:"document"`
	Signature      []byte    `json:"signature"`
	EffectiveFrom  time.Time `json:"effective_from"`
	EffectiveUntil time.Time `json:"effective_until"`
	Revoked        bool      `json:"revoked"`
}

type Current struct {
	Record    Record
	Authority Authority
	Revision  uint64
}

// PublicationVerifier verifies a signed, reviewed configured source in the
// publication transaction. Serving credentials cannot append authority rows.
type PublicationVerifier interface {
	VerifyPolicyPublication(context.Context, dbport.Tx, uuid.UUID, Record, Authority) error
}

type Store struct{ db dbport.Beginner }

func New(db dbport.Beginner) (*Store, error) {
	if db == nil {
		return nil, ErrUnavailable
	}
	return &Store{db: db}, nil
}

type Publisher struct {
	db       dbport.Beginner
	verifier PublicationVerifier
}

func NewPublisher(db dbport.Beginner, verifier PublicationVerifier) (*Publisher, error) {
	if db == nil || verifier == nil {
		return nil, ErrAuthority
	}
	return &Publisher{db: db, verifier: verifier}, nil
}

// ValidateRecord binds exact bytes, including the evaluation suite's existing
// domain-separated semantic digest. Records have no manifest or route digest.
func ValidateRecord(r Record) error {
	ref := r.Reference
	if strings.TrimSpace(ref.ID) == "" || strings.TrimSpace(ref.ID) != ref.ID || ref.Version == 0 || ref.SchemaVersion == 0 || len(r.Content) == 0 || len(r.Content) > 1<<20 || !json.Valid(r.Content) {
		return ErrInvalid
	}
	var digest string
	switch r.Kind {
	case ModelPolicy:
		canonical, _, err := agentstore.CanonicalPersonaModelRoutePayload(r.Content)
		if err != nil || string(canonical) != string(r.Content) {
			return ErrInvalid
		}
		var identity struct {
			ID            string `json:"id"`
			Version       uint64 `json:"version"`
			SchemaVersion uint32 `json:"schema_version"`
		}
		if err := json.Unmarshal(r.Content, &identity); err != nil || identity.ID != ref.ID || identity.Version != ref.Version || identity.SchemaVersion != ref.SchemaVersion {
			return ErrInvalid
		}
		digest = ContentDigest(r.Content)
	case OutputSchema:
		digest = ContentDigest(r.Content)
	case EvaluationSuite:
		var suite agenteval.PersonaSuite
		if err := json.Unmarshal(r.Content, &suite); err != nil || suite.ID != ref.ID {
			return ErrInvalid
		}
		canonical, err := json.Marshal(suite)
		if err != nil || string(canonical) != string(r.Content) {
			return ErrInvalid
		}
		digest = agenteval.PersonaSuiteDigest(suite)
	default:
		return ErrInvalid
	}
	if ref.Digest != digest {
		return ErrInvalid
	}
	return nil
}

func ContentDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validAuthority(a Authority) bool {
	return strings.TrimSpace(a.SourceID) != "" && a.SourceRevision > 0 && strings.TrimSpace(a.KeyID) != "" && len(a.Document) > 0 && len(a.Signature) > 0 && !a.EffectiveFrom.IsZero() && a.EffectiveUntil.After(a.EffectiveFrom)
}

// Publish retains an immutable contract and a new verified authority revision.
// Reusing an identity for different bytes or replaying a source revision fails.
func (p *Publisher) Publish(ctx context.Context, tenant uuid.UUID, r Record, a Authority) error {
	if p == nil || ctx == nil || tenant == uuid.Nil || !validAuthority(a) {
		return ErrAuthority
	}
	if err := ValidateRecord(r); err != nil {
		return err
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		return err
	}
	if role != AuthorityRole {
		return ErrAuthority
	}
	if err := p.verifier.VerifyPolicyPublication(ctx, tx, tenant, r, a); err != nil {
		return fmt.Errorf("%w: %v", ErrAuthority, err)
	}
	ref := r.Reference
	key := ContentDigest([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d", tenant, r.Kind, ref.ID, ref.Version, ref.SchemaVersion)))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agent_immutable_contract (tenant_id,kind,contract_id,version,schema_version,digest,content,content_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, tenant, r.Kind, ref.ID, ref.Version, ref.SchemaVersion, ref.Digest, r.Content, ContentDigest(r.Content))
	if err != nil {
		return err
	}
	var digest string
	var content []byte
	if err := tx.QueryRow(ctx, `SELECT digest,content FROM agent_immutable_contract WHERE tenant_id=$1 AND kind=$2 AND contract_id=$3 AND version=$4 AND schema_version=$5`, tenant, r.Kind, ref.ID, ref.Version, ref.SchemaVersion).Scan(&digest, &content); err != nil {
		return err
	}
	if digest != ref.Digest || string(content) != string(r.Content) {
		return ErrConflict
	}
	var revision, sourceRevision uint64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision),0),COALESCE(MAX(source_revision) FILTER (WHERE source_id=$6),0) FROM agent_contract_authority WHERE tenant_id=$1 AND kind=$2 AND contract_id=$3 AND version=$4 AND schema_version=$5`, tenant, r.Kind, ref.ID, ref.Version, ref.SchemaVersion, a.SourceID).Scan(&revision, &sourceRevision); err != nil {
		return err
	}
	if a.SourceRevision <= sourceRevision {
		return ErrConflict
	}
	proof, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agent_contract_authority (tenant_id,kind,contract_id,version,schema_version,digest,revision,source_id,source_revision,authority,revoked,effective_from,effective_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13)`, tenant, r.Kind, ref.ID, ref.Version, ref.SchemaVersion, ref.Digest, revision+1, a.SourceID, a.SourceRevision, string(proof), a.Revoked, a.EffectiveFrom.UTC(), a.EffectiveUntil.UTC())
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Resolve returns the exact contract and latest authority. An expired or
// revoked newest assertion never falls back to an older approval.
func (s *Store) Resolve(ctx context.Context, tenant uuid.UUID, kind string, ref agentmanifest.Reference, now time.Time) (Current, error) {
	if s == nil || ctx == nil || tenant == uuid.Nil || now.IsZero() {
		return Current{}, ErrUnavailable
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Current{}, err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return Current{}, err
	}
	var content, proof []byte
	var revision uint64
	var revoked bool
	var from, until time.Time
	err = tx.QueryRow(ctx, `SELECT c.content,a.authority,a.revision,a.revoked,a.effective_from,a.effective_until FROM agent_immutable_contract c JOIN LATERAL (SELECT * FROM agent_contract_authority a WHERE a.tenant_id=c.tenant_id AND a.kind=c.kind AND a.contract_id=c.contract_id AND a.version=c.version AND a.schema_version=c.schema_version AND a.digest=c.digest ORDER BY revision DESC LIMIT 1) a ON true WHERE c.tenant_id=$1 AND c.kind=$2 AND c.contract_id=$3 AND c.version=$4 AND c.schema_version=$5 AND c.digest=$6`, tenant, kind, ref.ID, ref.Version, ref.SchemaVersion, ref.Digest).Scan(&content, &proof, &revision, &revoked, &from, &until)
	if errors.Is(err, dbport.ErrNoRows) {
		return Current{}, ErrUnavailable
	}
	if err != nil {
		return Current{}, err
	}
	var a Authority
	if err := json.Unmarshal(proof, &a); err != nil || !validAuthority(a) || revoked || a.Revoked != revoked || !a.EffectiveFrom.UTC().Truncate(time.Microsecond).Equal(from) || !a.EffectiveUntil.UTC().Truncate(time.Microsecond).Equal(until) || now.Before(a.EffectiveFrom) || !now.Before(a.EffectiveUntil) {
		return Current{}, ErrUnavailable
	}
	r := Record{Kind: kind, Reference: ref, Content: content}
	if err := ValidateRecord(r); err != nil {
		return Current{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Current{}, err
	}
	return Current{Record: r, Authority: a, Revision: revision}, nil
}
