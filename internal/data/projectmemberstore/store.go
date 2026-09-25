// Package projectmemberstore persists tenant-scoped project membership and
// evaluates current project access from the stored membership state.
package projectmemberstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	projectactivity "github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
)

var (
	ErrInvalidRequest   = errors.New("invalid project membership request")
	ErrRevisionConflict = errors.New("project membership revision conflict")
	ErrNotFound         = errors.New("project membership project not found")
)

type Store struct{ projects *projectstore.Store }

func New(projects *projectstore.Store) (*Store, error) {
	if projects == nil {
		return nil, errors.New("project membership store requires project store")
	}
	return &Store{projects: projects}, nil
}

type Membership struct {
	TenantID, ProjectID, UserID string
	Role                        projectaccess.Role
	State                       projectaccess.MembershipState
	Revision                    int64
	CreatedAt, UpdatedAt        time.Time
}

type Snapshot struct {
	TenantID, ProjectID string
	Revision            int64
	Members             []Membership
}

// GetSnapshot reads membership policy on every call. Callers should use it
// before constructing any derived project read, cursor, stream, or search page.
func (s *Store) GetSnapshot(ctx context.Context, tenantID, projectID string) (Snapshot, error) {
	if tenantID == "" || projectID == "" {
		return Snapshot{}, ErrInvalidRequest
	}
	var out Snapshot
	err := s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var owner string
		if err := tx.QueryRow(ctx, `SELECT owner_id FROM project WHERE tenant_id=$1 AND id=$2 FOR SHARE`, tenantID, projectID).Scan(&owner); err != nil {
			return ErrNotFound
		}
		out.TenantID, out.ProjectID = tenantID, projectID
		if err := tx.QueryRow(ctx, `SELECT revision FROM project_membership_clock WHERE tenant_id=$1 AND project_id=$2`, tenantID, projectID).Scan(&out.Revision); err != nil {
			if !errors.Is(err, dbport.ErrNoRows) {
				return err
			}
			out.Revision = 1
		}
		rows, err := tx.Query(ctx, `SELECT tenant_id,project_id,user_id,role,state,revision,created_at,updated_at FROM project_membership WHERE tenant_id=$1 AND project_id=$2 ORDER BY user_id`, tenantID, projectID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Membership
			if err := rows.Scan(&m.TenantID, &m.ProjectID, &m.UserID, &m.Role, &m.State, &m.Revision, &m.CreatedAt, &m.UpdatedAt); err != nil {
				return err
			}
			out.Members = append(out.Members, m)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		out.Members = normalizeOwnerRows(out.Members, tenantID, projectID, owner)
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	return out, nil
}

func (s *Store) Authorize(ctx context.Context, tenantID, projectID, userID string, capability projectaccess.Capability) error {
	if userID == "" {
		return projectaccess.ErrUnauthorized
	}
	snapshot, err := s.GetSnapshot(ctx, tenantID, projectID)
	if err != nil {
		return err
	}
	p := projectaccess.Project{Tenant: projectaccess.TenantID(tenantID), ID: projectaccess.ProjectID(projectID), Memberships: make([]projectaccess.Membership, 0, len(snapshot.Members))}
	for _, m := range snapshot.Members {
		p.Memberships = append(p.Memberships, projectaccess.Membership{Tenant: projectaccess.TenantID(m.TenantID), User: projectaccess.UserID(m.UserID), Role: m.Role, State: m.State})
	}
	decision := projectaccess.Authorize(p, projectaccess.UserID(userID), projectaccess.TenantID(tenantID), capability, nil)
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", projectaccess.ErrUnauthorized, decision.Reason)
	}
	return nil
}

// ResolveVisibleMentions resolves only active members after rechecking the
// actor's current task-read capability. Handles are project member subject IDs;
// an unknown, inactive, or unauthorized target produces the same empty result.
func (s *Store) ResolveVisibleMentions(ctx context.Context, access projectactivity.AccessRequest, handles []string) ([]projectactivity.Mention, error) {
	if access.Action != projectactivity.ActionRead || access.Principal.TenantID == "" || access.Principal.SubjectID == "" || access.ProjectID == "" || access.TaskID == "" || len(handles) == 0 || len(handles) > 32 {
		return nil, ErrInvalidRequest
	}
	if err := s.Authorize(ctx, access.Principal.TenantID, access.ProjectID, access.Principal.SubjectID, projectaccess.ReadTask); err != nil {
		return nil, err
	}
	snapshot, err := s.GetSnapshot(ctx, access.Principal.TenantID, access.ProjectID)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(handles))
	for _, handle := range handles {
		if len(handle) == 0 || len(handle) > 200 {
			return nil, ErrInvalidRequest
		}
		wanted[strings.ToLower(handle)] = struct{}{}
	}
	resolved := make([]projectactivity.Mention, 0, len(wanted))
	for _, member := range snapshot.Members {
		if member.State != projectaccess.MembershipActive {
			continue
		}
		key := strings.ToLower(member.UserID)
		if _, ok := wanted[key]; !ok {
			continue
		}
		delete(wanted, key)
		resolved = append(resolved, projectactivity.Mention{SubjectID: member.UserID, DisplayName: member.UserID})
	}
	return resolved, nil
}

// AuthorizeList validates tenant-scoped listing inputs. Project visibility is
// enforced by the list query itself, which must filter active memberships
// before its cursor and page limit.
func (s *Store) AuthorizeList(_ context.Context, tenantID, userID string) error {
	if tenantID == "" || userID == "" {
		return projectaccess.ErrUnauthorized
	}
	return nil
}

// ListAuthorizedProjects filters membership before applying the cursor and
// limit, so invisible project IDs cannot affect page boundaries.
func (s *Store) ListAuthorizedProjects(ctx context.Context, tenantID, userID, afterID string, limit int32) ([]projectstore.ProjectRecord, error) {
	if tenantID == "" || userID == "" || limit < 1 || limit > 101 {
		return nil, ErrInvalidRequest
	}
	out := make([]projectstore.ProjectRecord, 0, limit)
	err := s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT p.id,p.tenant_id,p.owner_id,p.name,p.project_timezone,p.lifecycle,p.revision,p.created_at,p.updated_at FROM project p WHERE p.tenant_id=$1 AND p.id>$2 AND (p.owner_id=$3 OR EXISTS (SELECT 1 FROM project_membership m WHERE m.tenant_id=p.tenant_id AND m.project_id=p.id AND m.user_id=$3 AND m.state='ACTIVE' AND m.role IN ('OWNER','MANAGER','CONTRIBUTOR','VIEWER'))) ORDER BY p.id LIMIT $4`, tenantID, afterID, userID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p projectstore.ProjectRecord
			if err := rows.Scan(&p.ID, &p.TenantID, &p.OwnerID, &p.Name, &p.Timezone, &p.Lifecycle, &p.Revision, &p.CreatedAt, &p.UpdatedAt); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) Invite(ctx context.Context, tenantID, projectID, actorID, inviteeID string, role projectaccess.Role, inviteClass uint8, expectedRevision int64, key string) error {
	return s.mutate(ctx, tenantID, projectID, actorID, "invite", expectedRevision, key, map[string]any{"invitee": inviteeID, "role": role, "class": inviteClass}, func(tx dbport.Tx, p projectaccess.Project) (projectaccess.Project, string, map[string]any, error) {
		if inviteeID == "" {
			return p, "", nil, ErrInvalidRequest
		}
		q, err := projectaccess.Invite(p, projectaccess.UserID(actorID), projectaccess.UserID(inviteeID), role, projectaccess.TenantID(tenantID), inviteClass)
		return q, "membership.invited", map[string]any{"user_id": inviteeID, "role": role}, err
	})
}

func (s *Store) AcceptInvitation(ctx context.Context, tenantID, projectID, userID string, expectedRevision int64, key string) error {
	return s.mutate(ctx, tenantID, projectID, userID, "accept", expectedRevision, key, map[string]any{"user_id": userID}, func(tx dbport.Tx, p projectaccess.Project) (projectaccess.Project, string, map[string]any, error) {
		q, err := projectaccess.AcceptInvitation(p, projectaccess.UserID(userID), projectaccess.TenantID(tenantID))
		return q, "membership.accepted", map[string]any{"user_id": userID}, err
	})
}

func (s *Store) ChangeRole(ctx context.Context, tenantID, projectID, actorID, memberID string, role projectaccess.Role, expectedRevision int64, key string) error {
	return s.mutate(ctx, tenantID, projectID, actorID, "role", expectedRevision, key, map[string]any{"member": memberID, "role": role}, func(tx dbport.Tx, p projectaccess.Project) (projectaccess.Project, string, map[string]any, error) {
		q, err := projectaccess.ChangeRole(p, projectaccess.UserID(actorID), projectaccess.UserID(memberID), role, projectaccess.TenantID(tenantID))
		return q, "membership.role_changed", map[string]any{"user_id": memberID, "role": role}, err
	})
}

func (s *Store) Revoke(ctx context.Context, tenantID, projectID, actorID, memberID string, expectedRevision int64, key string) error {
	return s.mutate(ctx, tenantID, projectID, actorID, "revoke", expectedRevision, key, map[string]any{"member": memberID}, func(tx dbport.Tx, p projectaccess.Project) (projectaccess.Project, string, map[string]any, error) {
		q, err := projectaccess.Revoke(p, projectaccess.UserID(actorID), projectaccess.UserID(memberID), projectaccess.TenantID(tenantID))
		return q, "membership.revoked", map[string]any{"user_id": memberID}, err
	})
}

func (s *Store) TransferOwnership(ctx context.Context, tenantID, projectID, actorID, targetID string, grant *projectaccess.RecoveryGrant, at time.Time, expectedRevision int64, key string) error {
	input := map[string]any{"target": targetID, "at": at.UTC()}
	if grant != nil {
		input["grant"] = grant
	}
	return s.mutate(ctx, tenantID, projectID, actorID, "transfer", expectedRevision, key, input, func(tx dbport.Tx, p projectaccess.Project) (projectaccess.Project, string, map[string]any, error) {
		q, tr, err := projectaccess.TransferOwnership(p, projectaccess.UserID(actorID), projectaccess.UserID(targetID), projectaccess.TenantID(tenantID), grant, at)
		payload := map[string]any{"from": tr.From, "to": tr.To, "recovery": grant != nil}
		if grant != nil {
			payload["purpose"] = grant.Purpose
			payload["approver"] = grant.Approver
			payload["starts_at"] = grant.StartsAt.UTC()
			payload["ends_at"] = grant.EndsAt.UTC()
		}
		return q, "membership.ownership_transferred", payload, err
	})
}

type transition func(dbport.Tx, projectaccess.Project) (projectaccess.Project, string, map[string]any, error)

func fingerprint(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Store) mutate(ctx context.Context, tenantID, projectID, actorID, operation string, expected int64, key string, input any, apply transition) error {
	if tenantID == "" || projectID == "" || actorID == "" || expected <= 0 || strings.TrimSpace(key) == "" {
		return ErrInvalidRequest
	}
	fingerprint, err := fingerprint(struct {
		TenantID, ProjectID, ActorID string
		ExpectedRevision             int64
		Input                        any
	}{tenantID, projectID, actorID, expected, input})
	if err != nil {
		return err
	}
	return s.projects.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var owner string
		var class int
		if err := tx.QueryRow(ctx, `SELECT owner_id,classification_level FROM project WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, projectID).Scan(&owner, &class); err != nil {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `INSERT INTO project_membership_clock(tenant_id,project_id,revision) VALUES($1,$2,1) ON CONFLICT DO NOTHING`, tenantID, projectID); err != nil {
			return err
		}
		var rev int64
		if err := tx.QueryRow(ctx, `SELECT revision FROM project_membership_clock WHERE tenant_id=$1 AND project_id=$2 FOR UPDATE`, tenantID, projectID).Scan(&rev); err != nil {
			return err
		}
		var prior string
		err = tx.QueryRow(ctx, `SELECT fingerprint FROM project_membership_idempotency WHERE tenant_id=$1 AND actor_id=$2 AND operation=$3 AND client_key=$4`, tenantID, actorID, operation, key).Scan(&prior)
		if err == nil {
			if prior != fingerprint {
				return projectstore.ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if rev != expected {
			return ErrRevisionConflict
		}
		rows, err := tx.Query(ctx, `SELECT user_id,role,state FROM project_membership WHERE tenant_id=$1 AND project_id=$2 FOR UPDATE`, tenantID, projectID)
		if err != nil {
			return err
		}
		p := projectaccess.Project{Tenant: projectaccess.TenantID(tenantID), ID: projectaccess.ProjectID(projectID), ClassLevel: uint8(class)}
		for rows.Next() {
			var user string
			var role projectaccess.Role
			var state projectaccess.MembershipState
			if err := rows.Scan(&user, &role, &state); err != nil {
				rows.Close()
				return err
			}
			p.Memberships = append(p.Memberships, projectaccess.Membership{Tenant: p.Tenant, User: projectaccess.UserID(user), Role: role, State: state})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		p.Memberships = normalizeDomainOwner(p.Memberships, p.Tenant, owner)
		if operation == "revoke" && input.(map[string]any)["member"] == owner {
			return projectaccess.ErrOwnerRequired
		}
		if operation == "invite" && input.(map[string]any)["invitee"] == owner {
			return projectaccess.ErrInvalidRole
		}
		q, event, payload, err := apply(tx, p)
		if err != nil {
			return err
		}
		if err := persistMembers(ctx, tx, tenantID, projectID, p, q, rev+1); err != nil {
			return err
		}
		if event == "membership.ownership_transferred" {
			if _, err := tx.Exec(ctx, `UPDATE project SET owner_id=$1,revision=revision+1,updated_at=now() WHERE tenant_id=$2 AND id=$3`, fmt.Sprint(payload["to"]), tenantID, projectID); err != nil {
				return err
			}
		}
		body, _ := json.Marshal(payload)
		if _, err := tx.Exec(ctx, `INSERT INTO project_membership_event(tenant_id,project_id,revision,actor_id,event_type,payload) VALUES($1,$2,$3,$4,$5,$6::jsonb)`, tenantID, projectID, rev+1, actorID, event, body); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE project_membership_clock SET revision=$1,updated_at=now() WHERE tenant_id=$2 AND project_id=$3`, rev+1, tenantID, projectID); err != nil {
			return err
		}
		if _, err := projectstore.AppendOutboxEventTx(ctx, tx, projectstore.AppendOutboxEvent{
			TenantID: tenantID, ProjectID: projectID, AggregateID: projectID, EventType: event,
			SourceRevision: rev + 1, Classification: "INTERNAL", Value: payload,
		}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO project_membership_idempotency(tenant_id,actor_id,operation,client_key,fingerprint) VALUES($1,$2,$3,$4,$5)`, tenantID, actorID, operation, key, fingerprint)
		return err
	})
}

func normalizeOwnerRows(rows []Membership, tenantID, projectID, owner string) []Membership {
	found := false
	for i := range rows {
		if rows[i].UserID == owner {
			rows[i].Role = projectaccess.RoleOwner
			rows[i].State = projectaccess.MembershipActive
			found = true
		}
	}
	if !found {
		rows = append(rows, Membership{TenantID: tenantID, ProjectID: projectID, UserID: owner, Role: projectaccess.RoleOwner, State: projectaccess.MembershipActive})
	}
	return rows
}

func normalizeDomainOwner(rows []projectaccess.Membership, tenant projectaccess.TenantID, owner string) []projectaccess.Membership {
	found := false
	for i := range rows {
		if string(rows[i].User) == owner {
			rows[i].Role = projectaccess.RoleOwner
			rows[i].State = projectaccess.MembershipActive
			found = true
		}
	}
	if !found {
		rows = append(rows, projectaccess.Membership{Tenant: tenant, User: projectaccess.UserID(owner), Role: projectaccess.RoleOwner, State: projectaccess.MembershipActive})
	}
	return rows
}

func persistMembers(ctx context.Context, tx dbport.Tx, tenant, project string, _ projectaccess.Project, next projectaccess.Project, revision int64) error {
	byUser := map[string]projectaccess.Membership{}
	for _, m := range next.Memberships {
		byUser[string(m.User)] = m
	}
	for user, m := range byUser {
		if user == "" {
			continue
		}
		_, err := tx.Exec(ctx, `INSERT INTO project_membership(tenant_id,project_id,user_id,role,state,revision) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(tenant_id,project_id,user_id) DO UPDATE SET role=EXCLUDED.role,state=EXCLUDED.state,revision=EXCLUDED.revision,updated_at=now()`, tenant, project, user, m.Role, m.State, revision)
		if err != nil {
			return err
		}
	}
	return nil
}
