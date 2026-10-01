package projectaccess

import (
	"errors"
	"strings"
	"time"
)

// ExternalGuestRole is deliberately separate from the tenant-internal Role
// set. A cross-company guest can never be delegated OWNER, MANAGER, or any
// other project-administration role by spelling that role in a request.
type ExternalGuestRole string

const (
	ExternalGuestViewer      ExternalGuestRole = "GUEST_VIEWER"
	ExternalGuestContributor ExternalGuestRole = "GUEST_CONTRIBUTOR"
)

type ExternalGuestState string

const (
	ExternalGuestInvited ExternalGuestState = "INVITED"
	ExternalGuestActive  ExternalGuestState = "ACTIVE"
	ExternalGuestRevoked ExternalGuestState = "REVOKED"
	ExternalGuestExpired ExternalGuestState = "EXPIRED"
)

var (
	ErrInvalidExternalPolicy = errors.New("invalid cross-company project policy")
	ErrBilateralConsent      = errors.New("cross-company project requires bilateral consent")
	ErrExternalProject       = errors.New("invalid cross-company project")
	ErrExternalGuest         = errors.New("invalid external project guest")
	ErrExternalGuestAccess   = errors.New("external project guest is not authorized")
	ErrExternalRevision      = errors.New("cross-company project revision conflict")
	ErrExternalOwner         = errors.New("cross-company project owner is not authorized")
)

// ExternalProjectPolicy is the minimum policy that both companies must
// approve. The fields are copied into the admission record so later reads do
// not silently widen when a caller supplies a different policy.
type ExternalProjectPolicy struct {
	TenantID              TenantID
	PolicyAdministrator   UserID
	Consent               bool
	ClassificationCeiling uint8
	EgressProfile         string
	RetentionClass        string
	ExportAllowed         bool
	GuestRoleCeiling      ExternalGuestRole
	RevocationEpoch       uint64
}

func (p ExternalProjectPolicy) validate(expectedTenant TenantID) error {
	if p.TenantID == "" || p.TenantID != expectedTenant || p.PolicyAdministrator == "" ||
		!p.Consent || p.EgressProfile == "" || p.RetentionClass == "" ||
		!validExternalGuestRole(p.GuestRoleCeiling) || p.RevocationEpoch == 0 {
		return ErrInvalidExternalPolicy
	}
	return nil
}

type CrossCompanyAdmission struct {
	ProjectID       ProjectID
	HostTenant      TenantID
	HomeTenant      TenantID
	Classification  uint8
	EgressProfile   string
	RetentionClass  string
	HostPolicy      ExternalProjectPolicy
	HomePolicy      ExternalProjectPolicy
	RevocationEpoch uint64
	Revision        uint64
	Active          bool
}

type ExternalProjectGuest struct {
	ID              string
	User            UserID
	HomeTenant      TenantID
	Role            ExternalGuestRole
	State           ExternalGuestState
	InviteExpiresAt time.Time
	SessionEpoch    uint64
}

type ExternalProjectEvidence struct {
	Event       string
	GuestID     string
	Actor       UserID
	ActorTenant TenantID
	Revision    uint64
	RecordedAt  time.Time
}

// BilateralProjectAdmission is the project-owned admission and guest lifecycle
// state. Chat memberships are intentionally absent: a chat relationship has
// no operation that can create this record or grant a project read.
type BilateralProjectAdmission struct {
	ID         ProjectID
	HostTenant TenantID
	Owner      UserID
	Admission  CrossCompanyAdmission
	Guests     []ExternalProjectGuest
	Evidence   []ExternalProjectEvidence
}

// AdmitCrossCompanyProject creates an active admission only when host and
// home policies agree on scope, classification, egress, retention, and a
// non-zero revocation epoch. The returned project is the sole source of guest
// project authorization.
func AdmitBilateralProject(id ProjectID, hostTenant TenantID, owner UserID, classification uint8, homeTenant TenantID, hostPolicy, homePolicy ExternalProjectPolicy) (BilateralProjectAdmission, error) {
	if id == "" || hostTenant == "" || owner == "" || homeTenant == "" || hostTenant == homeTenant {
		return BilateralProjectAdmission{}, ErrExternalProject
	}
	if err := hostPolicy.validate(hostTenant); err != nil {
		return BilateralProjectAdmission{}, err
	}
	if err := homePolicy.validate(homeTenant); err != nil {
		return BilateralProjectAdmission{}, err
	}
	if !hostPolicy.Consent || !homePolicy.Consent || classification > hostPolicy.ClassificationCeiling || classification > homePolicy.ClassificationCeiling || hostPolicy.EgressProfile != homePolicy.EgressProfile || hostPolicy.RetentionClass != homePolicy.RetentionClass {
		return BilateralProjectAdmission{}, ErrBilateralConsent
	}
	return BilateralProjectAdmission{
		ID: id, HostTenant: hostTenant, Owner: owner,
		Admission: CrossCompanyAdmission{
			ProjectID: id, HostTenant: hostTenant, HomeTenant: homeTenant, Classification: classification,
			EgressProfile: hostPolicy.EgressProfile, RetentionClass: hostPolicy.RetentionClass,
			HostPolicy: hostPolicy, HomePolicy: homePolicy, RevocationEpoch: 1, Revision: 1, Active: true,
		},
	}, nil
}

// InviteExternalGuest creates a bounded invitation. It does not activate the
// guest and records no access until AcceptExternalGuest succeeds.
func (p BilateralProjectAdmission) InviteExternalGuest(actorTenant TenantID, actor UserID, guestID string, user UserID, homeTenant TenantID, role ExternalGuestRole, expiresAt, now time.Time) (BilateralProjectAdmission, error) {
	if err := p.authorizeHostOwner(actorTenant, actor); err != nil {
		return BilateralProjectAdmission{}, err
	}
	if !p.Admission.Active || homeTenant != p.Admission.HomeTenant || user == "" || guestID == "" || !validExternalGuestRole(role) || !roleAtMost(role, p.Admission.HostPolicy.GuestRoleCeiling) || !roleAtMost(role, p.Admission.HomePolicy.GuestRoleCeiling) || expiresAt.IsZero() || !expiresAt.After(now) {
		return BilateralProjectAdmission{}, ErrExternalGuest
	}
	for _, guest := range p.Guests {
		if guest.ID == guestID && guest.State != ExternalGuestRevoked && guest.State != ExternalGuestExpired {
			return BilateralProjectAdmission{}, ErrExternalGuest
		}
	}
	q := p.copy()
	q.Guests = append(q.Guests, ExternalProjectGuest{ID: guestID, User: user, HomeTenant: homeTenant, Role: role, State: ExternalGuestInvited, InviteExpiresAt: expiresAt.UTC(), SessionEpoch: 1})
	q.record("GUEST_INVITED", guestID, actor, actorTenant, now)
	return q, nil
}

func (p BilateralProjectAdmission) AcceptExternalGuest(guestID string, user UserID, homeTenant TenantID, now time.Time) (BilateralProjectAdmission, error) {
	q := p.copy()
	index := q.guestIndex(guestID)
	if index < 0 {
		return BilateralProjectAdmission{}, ErrExternalGuest
	}
	guest := q.Guests[index]
	if guest.User != user || guest.HomeTenant != homeTenant || guest.State != ExternalGuestInvited {
		return BilateralProjectAdmission{}, ErrExternalGuestAccess
	}
	if !now.Before(guest.InviteExpiresAt) {
		q.Guests[index].State = ExternalGuestExpired
		q.record("GUEST_INVITE_EXPIRED", guestID, user, homeTenant, now)
		return BilateralProjectAdmission{}, ErrExternalGuestAccess
	}
	q.Guests[index].State = ExternalGuestActive
	q.record("GUEST_ACCEPTED", guestID, user, homeTenant, now)
	return q, nil
}

// ChangeExternalGuestRole is host-owned and applies both companies' role
// ceilings. It cannot produce an administrator role.
func (p BilateralProjectAdmission) ChangeExternalGuestRole(actorTenant TenantID, actor UserID, guestID string, role ExternalGuestRole, now time.Time) (BilateralProjectAdmission, error) {
	if err := p.authorizeHostOwner(actorTenant, actor); err != nil {
		return BilateralProjectAdmission{}, err
	}
	if !p.Admission.Active || !validExternalGuestRole(role) || !roleAtMost(role, p.Admission.HostPolicy.GuestRoleCeiling) || !roleAtMost(role, p.Admission.HomePolicy.GuestRoleCeiling) {
		return BilateralProjectAdmission{}, ErrExternalGuest
	}
	q := p.copy()
	i := q.guestIndex(guestID)
	if i < 0 || q.Guests[i].State != ExternalGuestActive {
		return BilateralProjectAdmission{}, ErrExternalGuestAccess
	}
	q.Guests[i].Role = role
	q.Guests[i].SessionEpoch++
	q.record("GUEST_ROLE_CHANGED", guestID, actor, actorTenant, now)
	return q, nil
}

// RevokeExternalGuest is a host-side revocation and invalidates active
// cursors by advancing both the admission and guest session epochs.
func (p BilateralProjectAdmission) RevokeExternalGuest(actorTenant TenantID, actor UserID, guestID string, now time.Time) (BilateralProjectAdmission, error) {
	if err := p.authorizeHostOwner(actorTenant, actor); err != nil {
		return BilateralProjectAdmission{}, err
	}
	q := p.copy()
	i := q.guestIndex(guestID)
	if i < 0 || q.Guests[i].State != ExternalGuestActive {
		return BilateralProjectAdmission{}, ErrExternalGuestAccess
	}
	q.Guests[i].State = ExternalGuestRevoked
	q.Guests[i].SessionEpoch++
	q.Admission.RevocationEpoch++
	q.Admission.Revision++
	q.record("GUEST_REVOKED", guestID, actor, actorTenant, now)
	return q, nil
}

// OffboardExternalGuest is the home-company revocation path. It does not
// require host membership and has the same cursor invalidation guarantee.
func (p BilateralProjectAdmission) OffboardExternalGuest(homeTenant TenantID, guestID string, now time.Time) (BilateralProjectAdmission, error) {
	if homeTenant != p.Admission.HomeTenant {
		return BilateralProjectAdmission{}, ErrExternalGuestAccess
	}
	q := p.copy()
	i := q.guestIndex(guestID)
	if i < 0 || q.Guests[i].HomeTenant != homeTenant || q.Guests[i].State != ExternalGuestActive {
		return BilateralProjectAdmission{}, ErrExternalGuestAccess
	}
	q.Guests[i].State = ExternalGuestRevoked
	q.Guests[i].SessionEpoch++
	q.Admission.RevocationEpoch++
	q.Admission.Revision++
	q.record("HOME_OFFBOARDED", guestID, q.Guests[i].User, homeTenant, now)
	return q, nil
}

// RevokeCrossCompanyAdmission is a bilateral-scope kill switch. Once false,
// it cannot be revived by a guest or a chat membership.
func (p BilateralProjectAdmission) RevokeCrossCompanyAdmission(actorTenant TenantID, actor UserID, reason string, now time.Time) (BilateralProjectAdmission, error) {
	if strings.TrimSpace(reason) == "" || p.Admission.Revision == 0 || (actorTenant != p.HostTenant && actorTenant != p.Admission.HomeTenant) {
		return BilateralProjectAdmission{}, ErrExternalOwner
	}
	if (actorTenant == p.HostTenant && actor != p.Owner) || (actorTenant == p.Admission.HomeTenant && actor != p.Admission.HomePolicy.PolicyAdministrator) {
		return BilateralProjectAdmission{}, ErrExternalOwner
	}
	q := p.copy()
	q.Admission.Active = false
	q.Admission.RevocationEpoch++
	q.Admission.Revision++
	q.record("ADMISSION_REVOKED:"+reason, "", actor, actorTenant, now)
	return q, nil
}

type ExternalReadGrant struct {
	ProjectID       ProjectID
	HostTenant      TenantID
	HomeTenant      TenantID
	GuestID         string
	Role            ExternalGuestRole
	Classification  uint8
	EgressProfile   string
	RetentionClass  string
	CursorEpoch     uint64
	ExportPermitted bool
}

// AuthorizeExternalGuestRead is the read-time check. Callers must pass the
// current admission epoch; this invalidates cached cards, cursors, notices,
// and streams after either guest or admission revocation.
func (p BilateralProjectAdmission) AuthorizeExternalGuestRead(guestID string, user UserID, homeTenant TenantID, now time.Time, currentRevocationEpoch uint64, requestedClassification uint8) (ExternalReadGrant, error) {
	if !p.Admission.Active || currentRevocationEpoch != p.Admission.RevocationEpoch || requestedClassification > p.Admission.Classification {
		return ExternalReadGrant{}, ErrExternalGuestAccess
	}
	i := p.guestIndex(guestID)
	if i < 0 {
		return ExternalReadGrant{}, ErrExternalGuestAccess
	}
	guest := p.Guests[i]
	if guest.User != user || guest.HomeTenant != homeTenant || guest.State != ExternalGuestActive || !now.Before(guest.InviteExpiresAt) {
		return ExternalReadGrant{}, ErrExternalGuestAccess
	}
	return ExternalReadGrant{ProjectID: p.ID, HostTenant: p.HostTenant, HomeTenant: p.Admission.HomeTenant, GuestID: guest.ID, Role: guest.Role, Classification: p.Admission.Classification, EgressProfile: p.Admission.EgressProfile, RetentionClass: p.Admission.RetentionClass, CursorEpoch: p.Admission.RevocationEpoch<<32 | guest.SessionEpoch, ExportPermitted: p.Admission.HostPolicy.ExportAllowed && p.Admission.HomePolicy.ExportAllowed && guest.Role == ExternalGuestContributor}, nil
}

func (p BilateralProjectAdmission) authorizeHostOwner(tenant TenantID, actor UserID) error {
	if !p.Admission.Active || tenant != p.HostTenant || actor != p.Owner {
		return ErrExternalOwner
	}
	return nil
}

func (p BilateralProjectAdmission) copy() BilateralProjectAdmission {
	p.Guests = append([]ExternalProjectGuest(nil), p.Guests...)
	p.Evidence = append([]ExternalProjectEvidence(nil), p.Evidence...)
	return p
}

func (p *BilateralProjectAdmission) record(event, guestID string, actor UserID, actorTenant TenantID, at time.Time) {
	p.Evidence = append(p.Evidence, ExternalProjectEvidence{Event: event, GuestID: guestID, Actor: actor, ActorTenant: actorTenant, Revision: p.Admission.Revision, RecordedAt: at.UTC()})
}

func (p BilateralProjectAdmission) guestIndex(id string) int {
	for i := range p.Guests {
		if p.Guests[i].ID == id {
			return i
		}
	}
	return -1
}

func validExternalGuestRole(role ExternalGuestRole) bool {
	return role == ExternalGuestViewer || role == ExternalGuestContributor
}

func roleAtMost(role, ceiling ExternalGuestRole) bool {
	if !validExternalGuestRole(role) || !validExternalGuestRole(ceiling) {
		return false
	}
	return role == ExternalGuestViewer || ceiling == ExternalGuestContributor
}
