package agentstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrPersonaRouteQualificationRequired marks missing or mismatched trusted qualification.
	ErrPersonaRouteQualificationRequired = errors.New("agentstore: trusted persona route qualification required")
	// ErrPersonaRoutePayload marks a noncanonical or invalid route payload.
	ErrPersonaRoutePayload = errors.New("agentstore: invalid persona model route payload")
)

// PersonaRouteQualification is returned only by the configured trusted
// qualification authority after it resolves and verifies durable evidence.
type PersonaRouteQualification struct {
	RunID, TenantID, PersonaID string
	PersonaVersion             int64
	ProfileDigest              string
	ModelDigest                string
	SuiteDigest, RunDigest     string
	Passed, Fresh              bool
	ExpiresAt                  time.Time
}

// PersonaRouteQualificationAuthority resolves durable evidence in the exact
// tenant transaction used to publish the route policy.
type PersonaRouteQualificationAuthority interface {
	ResolvePersonaRouteQualification(context.Context, dbport.Tx, values.TenantId, string, string, int64, string) (PersonaRouteQualification, error)
}

// PersonaModelRoutePublisher writes route policies through a separately
// credentialed database role. Its DB must authenticate as
// hcmnext_persona_model_route_authority, never as the request-serving app role.
type PersonaModelRoutePublisher struct {
	db         dbport.Beginner
	tenantUUID func(values.TenantId) uuid.UUID
	qualified  PersonaRouteQualificationAuthority
}

// NewPersonaModelRoutePublisher requires a dedicated database authority,
// tenant mapper and trusted qualification resolver. A missing dependency fails closed.
func NewPersonaModelRoutePublisher(db dbport.Beginner, tenantUUID func(values.TenantId) uuid.UUID, qualified PersonaRouteQualificationAuthority) (*PersonaModelRoutePublisher, error) {
	if db == nil || tenantUUID == nil || qualified == nil {
		return nil, fmt.Errorf("%w: restricted database, tenant mapper, and qualification authority are required", ErrInvalidConfig)
	}
	return &PersonaModelRoutePublisher{db: db, tenantUUID: tenantUUID, qualified: qualified}, nil
}

// PersonaModelRoutePublication identifies one immutable route revision and
// references evaluator evidence; it never accepts caller-asserted pass flags.
type PersonaModelRoutePublication struct {
	Tenant          values.TenantId
	LegalEntityID   string
	PolicyID        string
	PolicyVersion   int64
	SchemaVersion   int64
	PolicyDigest    string
	Revision        int64
	EffectiveFrom   time.Time
	EffectiveUntil  *time.Time
	PersonaID       string
	PersonaVersion  int64
	ProfileDigest   string
	EvaluationRunID string
	PolicyPayload   []byte
	RoutePayload    []byte
}

// PublishPersonaModelRoutePolicy verifies tenant-bound evaluator evidence,
// seals the canonical payload digest and appends one immutable route policy.
func (p *PersonaModelRoutePublisher) PublishPersonaModelRoutePolicy(ctx context.Context, in PersonaModelRoutePublication) (string, error) {
	tenantUUID := uuid.Nil
	if p != nil && p.tenantUUID != nil {
		tenantUUID = p.tenantUUID(in.Tenant)
	}
	if p == nil || p.db == nil || p.tenantUUID == nil || p.qualified == nil || ctx == nil || tenantUUID == uuid.Nil ||
		strings.TrimSpace(string(in.Tenant)) == "" || !exactNonblank(in.LegalEntityID) || !exactNonblank(in.PolicyID) ||
		in.PolicyVersion <= 0 || in.SchemaVersion <= 0 || !validStoreDigest(in.PolicyDigest) || in.Revision <= 0 ||
		in.EffectiveFrom.IsZero() || in.PersonaVersion <= 0 || !exactNonblank(in.PersonaID) ||
		!validStoreDigest(in.ProfileDigest) || !exactNonblank(in.EvaluationRunID) {
		return "", fmt.Errorf("%w: incomplete route publication", ErrInvalidConfig)
	}
	if in.EffectiveUntil != nil && !in.EffectiveUntil.After(in.EffectiveFrom) {
		return "", fmt.Errorf("%w: invalid effective interval", ErrInvalidConfig)
	}
	canonical, payloadDigest, err := CanonicalPersonaModelRoutePayload(in.RoutePayload)
	if err != nil {
		return "", err
	}
	manifestDigest, err := personaRouteManifestDigest(canonical)
	if err != nil {
		return "", err
	}
	policyCanonical, policyDigest, err := CanonicalPersonaModelRoutePayload(in.PolicyPayload)
	if err != nil {
		return "", err
	}
	if in.PolicyDigest != policyDigest || !personaPolicyIdentity(policyCanonical, in.PolicyID, in.PolicyVersion, in.SchemaVersion) {
		return "", fmt.Errorf("%w: policy digest and identity must identify the canonical semantic policy", ErrPersonaRoutePayload)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("agentstore: begin route publication: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantUUID); err != nil {
		return "", err
	}
	qualification, err := p.qualified.ResolvePersonaRouteQualification(ctx, tx, in.Tenant, in.EvaluationRunID, in.PersonaID, in.PersonaVersion, in.ProfileDigest)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPersonaRouteQualificationRequired, err)
	}
	if qualification.RunID != in.EvaluationRunID || qualification.TenantID != string(in.Tenant) ||
		qualification.PersonaID != in.PersonaID || qualification.PersonaVersion != in.PersonaVersion ||
		qualification.ProfileDigest != in.ProfileDigest || !validStoreDigest(qualification.ModelDigest) ||
		!validStoreDigest(qualification.SuiteDigest) || !validStoreDigest(qualification.RunDigest) ||
		!qualification.Passed || !qualification.Fresh || !qualification.ExpiresAt.After(in.EffectiveFrom) {
		return "", ErrPersonaRouteQualificationRequired
	}
	if in.EffectiveUntil == nil {
		until := qualification.ExpiresAt.UTC()
		in.EffectiveUntil = &until
	} else if in.EffectiveUntil.After(qualification.ExpiresAt) {
		return "", fmt.Errorf("%w: route interval outlives its qualification", ErrPersonaRouteQualificationRequired)
	}
	var payloadModelDigest string
	if err := json.Unmarshal(canonical, &mapStringDigest{ModelDigest: &payloadModelDigest}); err != nil || payloadModelDigest != qualification.ModelDigest {
		return "", fmt.Errorf("%w: route model digest does not match trusted qualification", ErrPersonaRouteQualificationRequired)
	}
	// Serialize revisions for this exact policy key. The tenant-scoped advisory
	// lock prevents two trusted publishers from issuing the same revision.
	lockDomain := strings.Join([]string{tenantUUID.String(), in.LegalEntityID, in.PolicyID, fmt.Sprint(in.PolicyVersion), fmt.Sprint(in.SchemaVersion), manifestDigest}, "\x00")
	lockDigest := sha256.Sum256([]byte(lockDomain))
	lockKey := hex.EncodeToString(lockDigest[:])
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
		return "", fmt.Errorf("agentstore: lock route policy revision: %w", err)
	}
	var maxRevision int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision),0) FROM persona_model_route_deployment
		WHERE tenant_id=$1 AND legal_entity_id=$2 AND policy_id=$3 AND policy_version=$4 AND policy_schema_version=$5 AND agent_version_digest=$6`,
		tenantUUID, in.LegalEntityID, in.PolicyID, in.PolicyVersion, in.SchemaVersion, manifestDigest).Scan(&maxRevision); err != nil {
		return "", fmt.Errorf("agentstore: read route policy revision: %w", err)
	}
	if in.Revision != maxRevision+1 {
		return "", ErrConflict
	}
	var overlap bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM persona_model_route_deployment
		WHERE tenant_id=$1 AND legal_entity_id=$2 AND policy_id=$3 AND policy_version=$4 AND policy_schema_version=$5
		AND agent_version_digest=$8
		AND effective_from < COALESCE($7,'infinity'::timestamptz)
		AND (effective_until IS NULL OR effective_until > $6))`, tenantUUID, in.LegalEntityID,
		in.PolicyID, in.PolicyVersion, in.SchemaVersion, in.EffectiveFrom.UTC(), in.EffectiveUntil, manifestDigest).Scan(&overlap); err != nil {
		return "", fmt.Errorf("agentstore: check route policy interval: %w", err)
	}
	if overlap {
		return "", ErrPersonaModelRouteAmbiguous
	}
	_, err = tx.Exec(ctx, `INSERT INTO persona_model_route_deployment
		(tenant_id,legal_entity_id,policy_id,policy_version,policy_schema_version,policy_digest,revision,effective_from,effective_until,route_payload,route_payload_digest,evaluation_run_id,evaluation_model_digest,policy_payload,agent_version_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13,$14::jsonb,$15)`, tenantUUID, in.LegalEntityID,
		in.PolicyID, in.PolicyVersion, in.SchemaVersion, in.PolicyDigest, in.Revision, in.EffectiveFrom.UTC(), in.EffectiveUntil,
		string(canonical), payloadDigest, in.EvaluationRunID, qualification.ModelDigest, string(policyCanonical), manifestDigest)
	if err != nil {
		return "", fmt.Errorf("agentstore: append route policy: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("agentstore: commit route policy: %w", err)
	}
	return payloadDigest, nil
}

func personaRouteManifestDigest(raw []byte) (string, error) {
	var payload struct {
		Route struct {
			Pin  struct{ AgentVersionDigest string }
			Task struct{ AgentVersionDigest string }
		} `json:"route"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || !validStoreDigest(payload.Route.Pin.AgentVersionDigest) || payload.Route.Pin.AgentVersionDigest != payload.Route.Task.AgentVersionDigest {
		return "", fmt.Errorf("%w: exact matching route and task manifest pins required", ErrPersonaRoutePayload)
	}
	return payload.Route.Pin.AgentVersionDigest, nil
}

func personaPolicyIdentity(raw []byte, id string, version, schema int64) bool {
	var identity struct {
		ID            string `json:"id"`
		Version       int64  `json:"version"`
		SchemaVersion int64  `json:"schema_version"`
	}
	return json.Unmarshal(raw, &identity) == nil && identity.ID == id && identity.Version == version && identity.SchemaVersion == schema
}

type mapStringDigest struct {
	ModelDigest *string `json:"model_digest"`
}

func exactNonblank(v string) bool { return strings.TrimSpace(v) != "" && strings.TrimSpace(v) == v }

// CanonicalPersonaModelRoutePayload implements the publisher's canonical JSON
// contract: object keys sort lexicographically, arrays retain order, strings
// use encoding/json escaping, and numbers are canonical base-10 integers.
// Duplicate keys, fractional/exponent numbers and non-object top levels fail.
func CanonicalPersonaModelRoutePayload(raw []byte) ([]byte, string, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	value, err := readCanonicalJSONValue(dec)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrPersonaRoutePayload, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, "", fmt.Errorf("%w: trailing JSON value", ErrPersonaRoutePayload)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, "", fmt.Errorf("%w: top-level value must be an object", ErrPersonaRoutePayload)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, "", fmt.Errorf("%w: encode canonical JSON: %v", ErrPersonaRoutePayload, err)
	}
	sum := sha256.Sum256(canonical)
	return canonical, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func readCanonicalJSONValue(dec *json.Decoder) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch v := token.(type) {
	case json.Delim:
		switch v {
		case '{':
			object := make(map[string]any)
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				if _, exists := object[key]; exists {
					return nil, fmt.Errorf("duplicate object key %q", key)
				}
				child, err := readCanonicalJSONValue(dec)
				if err != nil {
					return nil, err
				}
				object[key] = child
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return object, nil
		case '[':
			array := make([]any, 0)
			for dec.More() {
				child, err := readCanonicalJSONValue(dec)
				if err != nil {
					return nil, err
				}
				array = append(array, child)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return array, nil
		default:
			return nil, errors.New("unexpected closing delimiter")
		}
	case json.Number:
		integer := new(big.Int)
		if _, ok := integer.SetString(string(v), 10); !ok {
			return nil, errors.New("numbers must be base-10 integers")
		}
		return json.Number(integer.String()), nil
	default:
		return token, nil
	}
}
