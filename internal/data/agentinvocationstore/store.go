package agentinvocationstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

var (
	ErrInvalid  = errors.New("agentinvocationstore: invalid invocation")
	ErrConflict = errors.New("agentinvocationstore: invocation conflict")
	ErrNotFound = errors.New("agentinvocationstore: invocation not found")
)

// Store is a repository backed by the isolated agent database.
type Store struct {
	db         *agentstore.Store
	tenantUUID func(string) uuid.UUID
}

// New constructs an invocation repository over the isolated agent store.
func New(db *agentstore.Store) (*Store, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Store{db: db, tenantUUID: func(value string) uuid.UUID { id, _ := uuid.Parse(value); return id }}, nil
}

// NewWithTenantUUID preserves logical tenant identities while mapping database scope.
func NewWithTenantUUID(db *agentstore.Store, mapper func(string) uuid.UUID) (*Store, error) {
	if db == nil || mapper == nil {
		return nil, ErrInvalid
	}
	return &Store{db: db, tenantUUID: mapper}, nil
}

func (s *Store) resolveTenant(tenant string) (uuid.UUID, error) {
	if s == nil || s.db == nil || s.tenantUUID == nil || tenant == "" || tenant != strings.TrimSpace(tenant) {
		return uuid.Nil, ErrInvalid
	}
	id := s.tenantUUID(tenant)
	if id == uuid.Nil {
		return uuid.Nil, ErrInvalid
	}
	return id, nil
}

var _ agentinvoke.InvocationRepository = (*Store)(nil)

// ListPersonaInvocations returns the newest 100 invocations belonging to the
// exact tenant, invoker and conversation. It never exposes another user's runs.
func (s *Store) ListPersonaInvocations(ctx context.Context, tenant, invoker, conversation string) ([]agentinvoke.Invocation, error) {
	tid, err := s.resolveTenant(tenant)
	if s == nil || s.db == nil || ctx == nil || err != nil || tid == uuid.Nil || strings.TrimSpace(invoker) == "" || invoker != strings.TrimSpace(invoker) || strings.TrimSpace(conversation) == "" || conversation != strings.TrimSpace(conversation) {
		return nil, ErrInvalid
	}
	out := make([]agentinvoke.Invocation, 0)
	err = s.db.RunTenantTx(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT invocation_id,tenant_id::text,conversation_id,thread_id,post_id,invoker_id,
			persona_id,persona_version,installation_id,mode,skills::text,actor::text,state,COALESCE(grant_payload::text,'')
			FROM persona_invocations WHERE tenant_id=$1 AND owner_id=$2 AND invoker_id=$2 AND conversation_id=$3
			ORDER BY created_at DESC,invocation_id COLLATE "C" LIMIT 100`, tid, invoker, conversation)
		if err != nil {
			return fmt.Errorf("agentinvocationstore: list invocations: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var item agentinvoke.Invocation
			var skills, actor, grant string
			if err := rows.Scan(&item.ID, &item.TenantID, &item.ConversationID, &item.ThreadID, &item.PostID, &item.InvokerID,
				&item.PersonaID, &item.PersonaVersion, &item.InstallationID, &item.Mode, &skills, &actor, &item.State, &grant); err != nil {
				return fmt.Errorf("agentinvocationstore: scan invocation: %w", err)
			}
			if err := json.Unmarshal([]byte(skills), &item.Skills); err != nil {
				return err
			}
			item.TenantID = tenant
			if err := json.Unmarshal([]byte(actor), &item.Actor); err != nil {
				return err
			}
			if grant != "" && grant != "null" {
				if err := json.Unmarshal([]byte(grant), &item.Grant); err != nil {
					return err
				}
				item.Skills = agentinvoke.IntersectSkillScopes(item.Skills, item.Grant.Skills)
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}

// Claim durably claims one post/persona pair. A replay returns the original
// row; a replay with changed authority or identity fails closed.
func (s *Store) Claim(ctx context.Context, candidate agentinvoke.Invocation) (agentinvoke.Invocation, bool, error) {
	if err := validate(candidate); err != nil {
		return agentinvoke.Invocation{}, false, err
	}
	tenantID, err := s.resolveTenant(candidate.TenantID)
	if err != nil {
		return agentinvoke.Invocation{}, false, fmt.Errorf("%w: tenant id: %v", ErrInvalid, err)
	}
	skills, err := json.Marshal(candidate.Skills.Clone())
	if err != nil {
		return agentinvoke.Invocation{}, false, fmt.Errorf("%w: skills: %v", ErrInvalid, err)
	}
	actor, err := json.Marshal(candidate.Actor)
	if err != nil {
		return agentinvoke.Invocation{}, false, fmt.Errorf("%w: actor: %v", ErrInvalid, err)
	}
	var out agentinvoke.Invocation
	created := false
	err = s.db.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var rawSkills, rawActor, rawGrant string
		err := tx.QueryRow(ctx, `SELECT invocation_id,tenant_id::text,conversation_id,thread_id,post_id,invoker_id,persona_id,persona_version,installation_id,mode,skills::text,actor::text,state,COALESCE(grant_payload::text,'') FROM persona_invocations WHERE tenant_id=$1 AND post_id=$2 AND persona_id=$3`, tenantID, candidate.PostID, candidate.PersonaID).Scan(&out.ID, &out.TenantID, &out.ConversationID, &out.ThreadID, &out.PostID, &out.InvokerID, &out.PersonaID, &out.PersonaVersion, &out.InstallationID, &out.Mode, &rawSkills, &rawActor, &out.State, &rawGrant)
		if errors.Is(err, dbport.ErrNoRows) {
			_, err = tx.Exec(ctx, `INSERT INTO persona_invocations (tenant_id,invocation_id,conversation_id,thread_id,post_id,invoker_id,persona_id,persona_version,installation_id,mode,skills,actor,owner_id,state) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12::jsonb,$6,'CLAIMED')`, tenantID, candidate.ID, candidate.ConversationID, candidate.ThreadID, candidate.PostID, candidate.InvokerID, candidate.PersonaID, candidate.PersonaVersion, candidate.InstallationID, candidate.Mode, string(skills), string(actor))
			if err != nil {
				return fmt.Errorf("insert invocation: %w", err)
			}
			out = clone(candidate)
			out.State = agentinvoke.InvocationClaimed
			created = true
			return nil
		}
		if err != nil {
			return fmt.Errorf("read invocation: %w", err)
		}
		if err := json.Unmarshal([]byte(rawSkills), &out.Skills); err != nil {
			return fmt.Errorf("decode skills: %w", err)
		}
		if err := json.Unmarshal([]byte(rawActor), &out.Actor); err != nil {
			return fmt.Errorf("decode actor: %w", err)
		}
		out.TenantID = candidate.TenantID
		if out.ID != candidate.ID || out.Actor != candidate.Actor || out.ConversationID != candidate.ConversationID || out.ThreadID != candidate.ThreadID || out.InvokerID != candidate.InvokerID || out.PersonaVersion != candidate.PersonaVersion || out.InstallationID != candidate.InstallationID || out.Mode != candidate.Mode || !agentinvoke.SkillScopesSubset(out.Skills, candidate.Skills) || !agentinvoke.SkillScopesSubset(candidate.Skills, out.Skills) {
			return ErrConflict
		}
		if rawGrant != "" && rawGrant != "null" {
			if err := json.Unmarshal([]byte(rawGrant), &out.Grant); err != nil {
				return err
			}
			out.Skills = agentinvoke.IntersectSkillScopes(out.Skills, out.Grant.Skills)
		}
		return nil
	})
	return out, created, err
}

// SetGrant attaches the single immutable delegation grant to a claimed
// invocation. Replays of the same grant are idempotent.
func (s *Store) SetGrant(ctx context.Context, tenant, id string, grant agentinvoke.DelegationGrant) (agentinvoke.Invocation, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(tenant) == "" || tenant != grant.TenantID || strings.TrimSpace(grant.ID) == "" {
		return agentinvoke.Invocation{}, ErrInvalid
	}
	tenantID, err := s.resolveTenant(tenant)
	if err != nil {
		return agentinvoke.Invocation{}, fmt.Errorf("%w: tenant id: %v", ErrInvalid, err)
	}
	var out agentinvoke.Invocation
	err = s.db.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var rawSkills, rawActor, rawGrant string
		var state string
		err := tx.QueryRow(ctx, `SELECT invocation_id,tenant_id::text,conversation_id,thread_id,post_id,invoker_id,persona_id,persona_version,installation_id,mode,skills::text,actor::text,state,COALESCE(grant_payload::text,'') FROM persona_invocations WHERE tenant_id=$1 AND invocation_id=$2 FOR UPDATE`, tenantID, id).Scan(&out.ID, &out.TenantID, &out.ConversationID, &out.ThreadID, &out.PostID, &out.InvokerID, &out.PersonaID, &out.PersonaVersion, &out.InstallationID, &out.Mode, &rawSkills, &rawActor, &state, &rawGrant)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(rawSkills), &out.Skills); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(rawActor), &out.Actor); err != nil {
			return err
		}
		out.State = agentinvoke.InvocationState(state)
		out.TenantID = tenant
		if rawGrant != "" && rawGrant != "null" {
			if err := json.Unmarshal([]byte(rawGrant), &out.Grant); err != nil {
				return err
			}
			if !sameGrant(out.Grant, grant) {
				return ErrConflict
			}
			out.Skills = agentinvoke.IntersectSkillScopes(out.Skills, out.Grant.Skills)
			return nil
		}
		if grant.UserID != out.InvokerID || grant.TenantID != out.TenantID || (grant.TaskID != "" && grant.TaskID != out.ID) || !agentinvoke.SkillScopesSubset(grant.Skills, out.Skills) || effectiveSkillCount(grant.Skills) == 0 {
			return ErrConflict
		}
		if out.State != agentinvoke.InvocationClaimed {
			return ErrConflict
		}
		effective := agentinvoke.IntersectSkillScopes(out.Skills, grant.Skills)
		if _, err := tx.Exec(ctx, `UPDATE persona_invocations SET grant_payload=$1::jsonb WHERE tenant_id=$2 AND invocation_id=$3`, string(mustJSON(grant)), tenantID, id); err != nil {
			return err
		}
		out.Grant = grant
		out.Skills = effective
		return nil
	})
	return out, err
}

// Lookup returns an invocation only when both its tenant and owner match.
// Owner matching is an application-level defense in depth check alongside RLS.
func (s *Store) Lookup(ctx context.Context, tenantID, ownerID, id string) (agentinvoke.Invocation, error) {
	tid, err := s.resolveTenant(tenantID)
	if err != nil || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(id) == "" {
		return agentinvoke.Invocation{}, ErrInvalid
	}
	var out agentinvoke.Invocation
	err = s.db.RunTenantTx(ctx, tid, func(tx dbport.Tx) error {
		var skills, actor, grant string
		var owner string
		err := tx.QueryRow(ctx, `SELECT invocation_id,tenant_id::text,conversation_id,thread_id,post_id,invoker_id,persona_id,persona_version,installation_id,mode,skills::text,actor::text,state,COALESCE(grant_payload::text,''),owner_id FROM persona_invocations WHERE tenant_id=$1 AND invocation_id=$2 AND owner_id=$3`, tid, id, ownerID).Scan(&out.ID, &out.TenantID, &out.ConversationID, &out.ThreadID, &out.PostID, &out.InvokerID, &out.PersonaID, &out.PersonaVersion, &out.InstallationID, &out.Mode, &skills, &actor, &out.State, &grant, &owner)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(skills), &out.Skills); err != nil {
			return err
		}
		out.TenantID = tenantID
		if err := json.Unmarshal([]byte(actor), &out.Actor); err != nil {
			return err
		}
		if grant != "" {
			if err := json.Unmarshal([]byte(grant), &out.Grant); err != nil {
				return err
			}
			out.Skills = agentinvoke.IntersectSkillScopes(out.Skills, out.Grant.Skills)
		}
		return nil
	})
	return out, err
}

// MarkStarted transitions a granted invocation exactly once within its tenant.
func (s *Store) MarkStarted(ctx context.Context, tenant, id string) (bool, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(id) == "" {
		return false, ErrInvalid
	}
	tid, err := s.resolveTenant(tenant)
	if err != nil {
		return false, fmt.Errorf("%w: tenant id: %v", ErrInvalid, err)
	}
	var started bool
	err = s.db.RunTenantTx(ctx, tid, func(tx dbport.Tx) error {
		n, err := tx.Exec(ctx, `UPDATE persona_invocations SET state='STARTED', started_at=now() WHERE tenant_id=$1 AND invocation_id=$2 AND state='CLAIMED' AND grant_payload IS NOT NULL`, tid, id)
		if err != nil {
			return err
		}
		if n == 1 {
			started = true
			return nil
		}
		var state string
		err = tx.QueryRow(ctx, `SELECT state FROM persona_invocations WHERE tenant_id=$1 AND invocation_id=$2`, tid, id).Scan(&state)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if state != string(agentinvoke.InvocationStarted) {
			return ErrConflict
		}
		return nil
	})
	return started, err
}

func validate(v agentinvoke.Invocation) error {
	for n, x := range map[string]string{"id": v.ID, "tenant": v.TenantID, "conversation": v.ConversationID, "thread": v.ThreadID, "post": v.PostID, "invoker": v.InvokerID, "persona": v.PersonaID, "version": v.PersonaVersion, "installation": v.InstallationID} {
		if strings.TrimSpace(x) == "" {
			return fmt.Errorf("%w: %s required", ErrInvalid, n)
		}
	}
	if v.Mode != agentinvoke.OnBehalfOf {
		return fmt.Errorf("%w: mode", ErrInvalid)
	}
	if v.Actor.Validate() != nil || v.Actor.UserID != v.InvokerID || v.Actor.PersonaID != v.PersonaID || v.Actor.PersonaVersion != v.PersonaVersion || v.Actor.InstallationID != v.InstallationID || v.Actor.ConversationID != v.ConversationID || v.Actor.InvokingPostID != v.PostID || v.Actor.InvocationID != v.ID {
		return ErrInvalid
	}
	return nil
}
func clone(v agentinvoke.Invocation) agentinvoke.Invocation { v.Skills = v.Skills.Clone(); return v }
func sameGrant(a, b agentinvoke.DelegationGrant) bool {
	return a.ID == b.ID && a.UserID == b.UserID && a.TenantID == b.TenantID && a.TaskID == b.TaskID && a.TargetAgentID == b.TargetAgentID && a.ExpiresAt.Equal(b.ExpiresAt) && agentinvoke.SkillScopesSubset(a.Skills, b.Skills) && agentinvoke.SkillScopesSubset(b.Skills, a.Skills)
}

func effectiveSkillCount(skills agentinvoke.SkillScopes) int {
	n := 0
	for _, scopes := range skills {
		n += len(scopes)
	}
	return n
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
