package projectaccess

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type ExternalRole string

const (
	ExternalViewer      ExternalRole = "VIEWER"
	ExternalContributor ExternalRole = "CONTRIBUTOR"
)

type CrossCompanySurface string

const (
	SurfaceProject            CrossCompanySurface = "PROJECT"
	SurfaceTask               CrossCompanySurface = "TASK"
	SurfaceLinkedDocument     CrossCompanySurface = "LINKED_DOCUMENT"
	CrossCompanySurfaceSearch CrossCompanySurface = "SEARCH"
	SurfaceRetainedStream     CrossCompanySurface = "RETAINED_STREAM"
)

var (
	ErrInvalidCrossCompany = errors.New("projectaccess: invalid cross-company admission")
	ErrCrossCompanyDenied  = errors.New("projectaccess: cross-company access denied")
	ErrCrossCompanyExpired = errors.New("projectaccess: cross-company admission expired")
)

// CrossCompanyRequest is the bilateral admission record. Neither a chat
// share nor a URL can construct one: both tenants must explicitly accept it.
type CrossCompanyRequest struct {
	ProjectID      ProjectID
	HostTenant     TenantID
	HostOwner      UserID
	GuestTenant    TenantID
	HostAccepted   bool
	GuestAccepted  bool
	Classification uint8
	EgressClasses  []string
	AdmittedAt     time.Time
	ExpiresAt      time.Time
}

type Delegation struct {
	Tenant    TenantID
	User      UserID
	Role      ExternalRole
	GrantedBy UserID
	GrantedAt time.Time
	ExpiresAt time.Time
}

type CrossCompanyProject struct {
	ProjectID      ProjectID
	HostTenant     TenantID
	HostOwner      UserID
	GuestTenant    TenantID
	Classification uint8
	EgressClasses  []string
	AdmittedAt     time.Time
	ExpiresAt      time.Time
	Delegations    []Delegation
	RevokedAt      time.Time
	RevokedBy      UserID
	RevocationNote string
}

func AdmitCrossCompany(req CrossCompanyRequest) (CrossCompanyProject, error) {
	if strings.TrimSpace(string(req.ProjectID)) == "" || strings.TrimSpace(string(req.HostTenant)) == "" || strings.TrimSpace(string(req.GuestTenant)) == "" || strings.TrimSpace(string(req.HostOwner)) == "" || req.HostTenant == req.GuestTenant || !req.HostAccepted || !req.GuestAccepted || req.ExpiresAt.IsZero() || req.AdmittedAt.IsZero() || !req.ExpiresAt.After(req.AdmittedAt) || len(req.EgressClasses) == 0 {
		return CrossCompanyProject{}, ErrInvalidCrossCompany
	}
	classes := make([]string, 0, len(req.EgressClasses))
	seen := make(map[string]struct{}, len(req.EgressClasses))
	for _, class := range req.EgressClasses {
		if strings.TrimSpace(class) == "" || strings.TrimSpace(class) != class {
			return CrossCompanyProject{}, ErrInvalidCrossCompany
		}
		if _, exists := seen[class]; exists {
			return CrossCompanyProject{}, ErrInvalidCrossCompany
		}
		seen[class] = struct{}{}
		classes = append(classes, class)
	}
	return CrossCompanyProject{ProjectID: req.ProjectID, HostTenant: req.HostTenant, HostOwner: req.HostOwner, GuestTenant: req.GuestTenant, Classification: req.Classification, EgressClasses: classes, AdmittedAt: req.AdmittedAt, ExpiresAt: req.ExpiresAt}, nil
}

func (p CrossCompanyProject) Grant(actor UserID, tenant TenantID, user UserID, role ExternalRole, at, expiresAt time.Time) (CrossCompanyProject, error) {
	if !p.activeAt(at) || tenant != p.GuestTenant || actor != p.HostOwner || strings.TrimSpace(string(user)) == "" || user == p.HostOwner || !validExternalRole(role) || at.IsZero() || expiresAt.IsZero() || !expiresAt.After(at) || expiresAt.After(p.ExpiresAt) {
		return CrossCompanyProject{}, ErrCrossCompanyDenied
	}
	for _, delegation := range p.Delegations {
		if delegation.Tenant == tenant && delegation.User == user && delegation.ExpiresAt.After(at) {
			return CrossCompanyProject{}, ErrCrossCompanyDenied
		}
	}
	p.Delegations = append(append([]Delegation(nil), p.Delegations...), Delegation{Tenant: tenant, User: user, Role: role, GrantedBy: actor, GrantedAt: at, ExpiresAt: expiresAt})
	return p, nil
}

func (p CrossCompanyProject) Revoke(actor UserID, at time.Time, note string) (CrossCompanyProject, error) {
	if actor != p.HostOwner || strings.TrimSpace(note) == "" || at.IsZero() || !p.activeAt(at) {
		return CrossCompanyProject{}, ErrCrossCompanyDenied
	}
	p.RevokedAt, p.RevokedBy, p.RevocationNote = at, actor, note
	return p, nil
}

type AccessDecision struct {
	Allowed bool
	Reason  string
}

func (p CrossCompanyProject) Authorize(tenant TenantID, user UserID, surface CrossCompanySurface, at time.Time) AccessDecision {
	if strings.TrimSpace(string(user)) == "" || !validCrossCompanySurface(surface) {
		return AccessDecision{Reason: "invalid_request"}
	}
	if p.RevokedAt.IsZero() == false {
		return AccessDecision{Reason: "revoked"}
	}
	if at.IsZero() || at.Before(p.AdmittedAt) || !at.Before(p.ExpiresAt) {
		return AccessDecision{Reason: "expired"}
	}
	if tenant == p.HostTenant && user == p.HostOwner {
		return AccessDecision{Allowed: true, Reason: "host_owner"}
	}
	if tenant != p.GuestTenant {
		return AccessDecision{Reason: "tenant_scope"}
	}
	for _, delegation := range p.Delegations {
		if delegation.Tenant == tenant && delegation.User == user && at.Before(delegation.ExpiresAt) && roleAllowsExternal(delegation.Role, surface) {
			return AccessDecision{Allowed: true, Reason: "delegated_role"}
		}
	}
	return AccessDecision{Reason: "no_active_delegation"}
}

type EgressDecision struct {
	Allowed bool
	Reason  string
}

func (p CrossCompanyProject) CheckEgress(tenant TenantID, class string, at time.Time) EgressDecision {
	if !p.activeAt(at) {
		return EgressDecision{Reason: "revoked_or_expired"}
	}
	if tenant != p.HostTenant && tenant != p.GuestTenant {
		return EgressDecision{Reason: "tenant_scope"}
	}
	for _, allowed := range p.EgressClasses {
		if allowed == class {
			return EgressDecision{Allowed: true, Reason: "bilateral_egress_agreement"}
		}
	}
	return EgressDecision{Reason: "classification_not_agreed"}
}

func (p CrossCompanyProject) activeAt(at time.Time) bool {
	return !at.IsZero() && p.RevokedAt.IsZero() && !at.Before(p.AdmittedAt) && at.Before(p.ExpiresAt)
}

func validExternalRole(role ExternalRole) bool {
	return role == ExternalViewer || role == ExternalContributor
}

func validCrossCompanySurface(surface CrossCompanySurface) bool {
	switch surface {
	case SurfaceProject, SurfaceTask, SurfaceLinkedDocument, CrossCompanySurfaceSearch, SurfaceRetainedStream:
		return true
	default:
		return false
	}
}

func roleAllowsExternal(role ExternalRole, surface CrossCompanySurface) bool {
	if role == ExternalContributor {
		return validCrossCompanySurface(surface)
	}
	return role == ExternalViewer && (surface == SurfaceProject || surface == SurfaceTask || surface == SurfaceLinkedDocument || surface == CrossCompanySurfaceSearch || surface == SurfaceRetainedStream)
}

func (p CrossCompanyProject) String() string {
	return fmt.Sprintf("%s/%s->%s", p.ProjectID, p.HostTenant, p.GuestTenant)
}
