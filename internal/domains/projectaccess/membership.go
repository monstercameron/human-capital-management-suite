// Package projectaccess contains pure project membership and authorization rules.
package projectaccess

import (
	"errors"
	"time"
)

type TenantID string
type ProjectID string
type UserID string

type Role string

const (
	RoleOwner       Role = "OWNER"
	RoleManager     Role = "MANAGER"
	RoleContributor Role = "CONTRIBUTOR"
	RoleViewer      Role = "VIEWER"
)

type MembershipState string

const (
	MembershipInvited MembershipState = "INVITED"
	MembershipActive  MembershipState = "ACTIVE"
	MembershipRevoked MembershipState = "REVOKED"
)

type Capability string

const (
	ReadProject          Capability = "read_project"
	ReadTask             Capability = "read_task"
	CreateTask           Capability = "create_task"
	EditTask             Capability = "edit_task"
	MoveTask             Capability = "move_task"
	Comment              Capability = "comment"
	ManageMembers        Capability = "manage_members"
	ManageViews          Capability = "manage_views"
	ManageProject        Capability = "manage_project"
	ConfigureWorkflow    Capability = "configure_workflow"
	PublishConfiguration Capability = "publish_configuration"
	Export               Capability = "export"
	Archive              Capability = "archive"
)

var (
	ErrInvalidProject        = errors.New("invalid project")
	ErrUnauthorized          = errors.New("unauthorized membership operation")
	ErrTenantMismatch        = errors.New("tenant mismatch")
	ErrInvalidRole           = errors.New("invalid role")
	ErrInvitationNotFound    = errors.New("active invitation not found")
	ErrMemberNotFound        = errors.New("active member not found")
	ErrOwnerRequired         = errors.New("project must retain an active owner")
	ErrRecoveryNotAuthorized = errors.New("recovery access is not authorized")
)

type Membership struct {
	Tenant TenantID
	User   UserID
	Role   Role
	State  MembershipState
}

// Project is an immutable value. Operations return a copy so callers can persist
// state only after their surrounding transaction has succeeded.
type Project struct {
	Tenant      TenantID
	ID          ProjectID
	ClassLevel  uint8
	Memberships []Membership
}

type RecoveryGrant struct {
	Tenant   TenantID
	Project  ProjectID
	Actor    UserID
	Purpose  string
	Approver UserID
	StartsAt time.Time
	EndsAt   time.Time
}

type Transfer struct {
	From UserID
	To   UserID
}

func validRole(role Role) bool {
	switch role {
	case RoleOwner, RoleManager, RoleContributor, RoleViewer:
		return true
	default:
		return false
	}
}

func copyProject(p Project) Project {
	p.Memberships = append([]Membership(nil), p.Memberships...)
	return p
}

func find(p Project, user UserID) int {
	for i := range p.Memberships {
		if p.Memberships[i].Tenant == p.Tenant && p.Memberships[i].User == user {
			return i
		}
	}
	return -1
}

func activeRole(p Project, user UserID) (Role, bool) {
	i := find(p, user)
	if i < 0 || p.Memberships[i].State != MembershipActive {
		return "", false
	}
	return p.Memberships[i].Role, true
}

func canManage(p Project, actor UserID) bool {
	role, ok := activeRole(p, actor)
	return ok && (role == RoleOwner || role == RoleManager)
}

// Invite creates a tenant-internal pending membership. Owners are assigned only
// by TransferOwnership, and managers cannot invite above the project's class.
func Invite(p Project, actor, invitee UserID, role Role, tenant TenantID, inviteClass uint8) (Project, error) {
	if p.ID == "" || p.Tenant == "" {
		return p, ErrInvalidProject
	}
	if tenant != p.Tenant {
		return p, ErrTenantMismatch
	}
	if !canManage(p, actor) {
		return p, ErrUnauthorized
	}
	if invitee == "" || invitee == actor || !validRole(role) || role == RoleOwner {
		return p, ErrInvalidRole
	}
	if inviteClass > p.ClassLevel {
		return p, ErrUnauthorized
	}
	q := copyProject(p)
	i := find(q, invitee)
	if i >= 0 {
		if q.Memberships[i].State == MembershipActive || q.Memberships[i].State == MembershipInvited {
			return p, ErrUnauthorized
		}
		q.Memberships[i] = Membership{Tenant: p.Tenant, User: invitee, Role: role, State: MembershipInvited}
	} else {
		q.Memberships = append(q.Memberships, Membership{Tenant: p.Tenant, User: invitee, Role: role, State: MembershipInvited})
	}
	return q, nil
}

func AcceptInvitation(p Project, user UserID, tenant TenantID) (Project, error) {
	if tenant != p.Tenant {
		return p, ErrTenantMismatch
	}
	q := copyProject(p)
	i := find(q, user)
	if i < 0 || q.Memberships[i].Tenant != tenant || q.Memberships[i].State != MembershipInvited {
		return p, ErrInvitationNotFound
	}
	q.Memberships[i].State = MembershipActive
	return q, nil
}

func ChangeRole(p Project, actor, member UserID, role Role, tenant TenantID) (Project, error) {
	if tenant != p.Tenant {
		return p, ErrTenantMismatch
	}
	if !canManage(p, actor) || (role == RoleOwner) {
		return p, ErrUnauthorized
	}
	if !validRole(role) {
		return p, ErrInvalidRole
	}
	q := copyProject(p)
	i := find(q, member)
	if i < 0 || q.Memberships[i].State != MembershipActive {
		return p, ErrMemberNotFound
	}
	if q.Memberships[i].Role == RoleOwner {
		return p, ErrUnauthorized
	}
	q.Memberships[i].Role = role
	return q, nil
}

func Revoke(p Project, actor, member UserID, tenant TenantID) (Project, error) {
	if tenant != p.Tenant {
		return p, ErrTenantMismatch
	}
	if !canManage(p, actor) {
		return p, ErrUnauthorized
	}
	q := copyProject(p)
	i := find(q, member)
	if i < 0 || q.Memberships[i].State != MembershipActive {
		return p, ErrMemberNotFound
	}
	if q.Memberships[i].Role == RoleOwner && activeOwnerCount(q) <= 1 {
		return p, ErrOwnerRequired
	}
	q.Memberships[i].State = MembershipRevoked
	return q, nil
}

func activeOwnerCount(p Project) int {
	n := 0
	for _, m := range p.Memberships {
		if m.Tenant == p.Tenant && m.State == MembershipActive && m.Role == RoleOwner {
			n++
		}
	}
	return n
}

func recoveryAuthorized(p Project, grant RecoveryGrant, actor UserID, at time.Time) bool {
	return grant.Tenant == p.Tenant && grant.Project == p.ID && grant.Actor == actor && grant.Purpose != "" && grant.Approver != "" && grant.Approver != actor && !at.Before(grant.StartsAt) && at.Before(grant.EndsAt) && grant.EndsAt.After(grant.StartsAt)
}

func TransferOwnership(p Project, actor, target UserID, tenant TenantID, grant *RecoveryGrant, at time.Time) (Project, Transfer, error) {
	if tenant != p.Tenant {
		return p, Transfer{}, ErrTenantMismatch
	}
	role, active := activeRole(p, actor)
	authorized := active && role == RoleOwner
	if !authorized && grant != nil {
		authorized = recoveryAuthorized(p, *grant, actor, at)
	}
	if !authorized {
		return p, Transfer{}, ErrRecoveryNotAuthorized
	}
	if actor == target {
		return p, Transfer{}, ErrInvalidRole
	}
	q := copyProject(p)
	to := find(q, target)
	if to < 0 || q.Memberships[to].State != MembershipActive {
		return p, Transfer{}, ErrMemberNotFound
	}
	from := find(q, actor)
	if active && q.Memberships[from].Role != RoleOwner {
		return p, Transfer{}, ErrUnauthorized
	}
	q.Memberships[to].Role = RoleOwner
	// The former owner retains manager authority when transferring normally.
	if active {
		q.Memberships[from].Role = RoleManager
	}
	if activeOwnerCount(q) < 1 {
		return p, Transfer{}, ErrOwnerRequired
	}
	return q, Transfer{From: actor, To: target}, nil
}

type Decision struct {
	Allowed bool
	Reason  string
}

// Authorize evaluates tenant and active membership before role grants. Explicit
// denies are checked after scope but before grants, so they always win.
func Authorize(p Project, user UserID, tenant TenantID, capability Capability, denied map[Capability]bool) Decision {
	if tenant == "" || tenant != p.Tenant {
		return Decision{Reason: "tenant_scope"}
	}
	if denied[capability] {
		return Decision{Reason: "explicit_deny"}
	}
	role, ok := activeRole(p, user)
	if !ok {
		return Decision{Reason: "no_active_membership"}
	}
	if roleAllows(role, capability) {
		return Decision{Allowed: true, Reason: "role_grant"}
	}
	return Decision{Reason: "role_denied"}
}

func roleAllows(role Role, capability Capability) bool {
	switch capability {
	case ReadProject, ReadTask:
		return validRole(role)
	case CreateTask, EditTask, MoveTask, Comment:
		return role == RoleOwner || role == RoleManager || role == RoleContributor
	case ManageMembers, ManageViews, ManageProject, ConfigureWorkflow, PublishConfiguration, Export, Archive:
		return role == RoleOwner || role == RoleManager
	default:
		return false
	}
}
