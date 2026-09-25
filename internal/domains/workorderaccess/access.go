// Package workorderaccess contains pure, decision-time authorization rules for
// work orders. Persistence and identity-directory lookups are supplied by callers.
package workorderaccess

import (
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
)

type TenantID string
type ProjectID string
type WorkOrderID string
type UserID string
type WorkerID string

type Capability string

const (
	Create     Capability = "create"
	Read       Capability = "read"
	Request    Capability = "request"
	Approve    Capability = "approve"
	AddNote    Capability = "note"
	Advance    Capability = "phase"
	RecordCost Capability = "record_cost"
	Inspect    Capability = "inspect"
	Bill       Capability = "bill"
	Configure  Capability = "configure"
	ViewCost   Capability = "view_cost"
	ViewPay    Capability = "view_pay_rate"
)

type MembershipState string

const (
	Active  MembershipState = "ACTIVE"
	Revoked MembershipState = "REVOKED"
)

var (
	ErrInvalidScope     = errors.New("invalid work order scope")
	ErrUnauthorized     = errors.New("unauthorized work order operation")
	ErrSelfApproval     = errors.New("requester cannot approve own request")
	ErrWorkerIneligible = errors.New("worker is not currently eligible")
	ErrInvalidRequest   = errors.New("invalid approval request")
)

// Grant is an explicit work-order capability. Grants are additive; Denies on
// the work order always take precedence. Revoked assignments/grants are ignored.
type Grant struct {
	Tenant     TenantID
	Project    ProjectID
	WorkOrder  WorkOrderID
	User       UserID
	Capability Capability
	State      MembershipState
}

type Denial struct {
	Tenant     TenantID
	Project    ProjectID
	WorkOrder  WorkOrderID
	User       UserID
	Capability Capability
	State      MembershipState
}

type Assignment struct {
	Tenant    TenantID
	WorkOrder WorkOrderID
	Worker    WorkerID
	State     MembershipState
}

// WorkOrder contains only authorization facts. Project contains the latest
// membership state and must be loaded at decision time by the caller.
type WorkOrder struct {
	Tenant      TenantID
	Project     ProjectID
	ID          WorkOrderID
	InitiatorID UserID
	Private     bool
	Grants      []Grant
	Denials     []Denial
	Assignments []Assignment
}

type Decision struct {
	Allowed bool
	Reason  string
}

// Authorize requires a current active project membership for every operation.
// A private order additionally requires a current scoped grant; this prevents
// project membership alone from exposing a private order or its existence.
func Authorize(order WorkOrder, project projectaccess.Project, actor UserID, tenant TenantID, capability Capability) Decision {
	if order.ID == "" || order.Tenant == "" || order.Project == "" || project.ID == "" || project.Tenant == "" {
		return Decision{Reason: "invalid_scope"}
	}
	if tenant == "" || tenant != order.Tenant || project.Tenant != projectaccess.TenantID(order.Tenant) || project.ID != projectaccess.ProjectID(order.Project) {
		return Decision{Reason: "tenant_or_project_scope"}
	}
	if actor == "" {
		return Decision{Reason: "no_active_project_membership"}
	}
	if !projectaccess.Authorize(project, projectaccess.UserID(actor), projectaccess.TenantID(tenant), projectaccess.ReadProject, nil).Allowed {
		return Decision{Reason: "no_active_project_membership"}
	}
	if scopedMatch(order.Denials, order, actor, capability) {
		return Decision{Reason: "explicit_deny"}
	}
	granted := scopedMatch(order.Grants, order, actor, capability)
	if order.Private && !granted {
		return Decision{Reason: "private_order"}
	}
	if granted {
		return Decision{Allowed: true, Reason: "scoped_grant"}
	}
	if !order.Private && capability == Advance && order.InitiatorID != "" && actor == order.InitiatorID {
		return Decision{Allowed: true, Reason: "active_initiator"}
	}
	if projectRoleAllows(project, actor, tenant, capability) {
		return Decision{Allowed: true, Reason: "active_project_role"}
	}
	return Decision{Reason: "no_scoped_grant"}
}

// projectRoleAllows is the initial role policy used before a durable,
// work-order-specific grants store exists. It is intentionally limited to
// currently active project roles. Sensitive pay-rate visibility remains an
// explicit per-order grant even for project owners and managers.
func projectRoleAllows(project projectaccess.Project, actor UserID, tenant TenantID, capability Capability) bool {
	var projectCapability projectaccess.Capability
	switch capability {
	case Create, Request:
		projectCapability = projectaccess.CreateTask
	case Read:
		projectCapability = projectaccess.ReadProject
	case AddNote:
		projectCapability = projectaccess.Comment
	case Advance, Approve, RecordCost, Inspect, Bill, ViewCost:
		projectCapability = projectaccess.ManageProject
	case Configure:
		projectCapability = projectaccess.ConfigureWorkflow
	default:
		return false
	}
	return projectaccess.Authorize(project, projectaccess.UserID(actor), projectaccess.TenantID(tenant), projectCapability, nil).Allowed
}

func scopedMatch[T interface {
	~struct {
		Tenant     TenantID
		Project    ProjectID
		WorkOrder  WorkOrderID
		User       UserID
		Capability Capability
		State      MembershipState
	}
}](items []T, order WorkOrder, actor UserID, capability Capability) bool {
	for _, item := range items {
		v := struct {
			Tenant     TenantID
			Project    ProjectID
			WorkOrder  WorkOrderID
			User       UserID
			Capability Capability
			State      MembershipState
		}(item)
		if v.Tenant == order.Tenant && v.Project == order.Project && v.WorkOrder == order.ID && v.User == actor && v.Capability == capability && v.State == Active {
			return true
		}
	}
	return false
}

// WorkerDirectory is deliberately an interface: assignment and work-log
// commands must recheck employment/contractor eligibility at decision time.
type WorkerDirectory interface {
	Eligible(tenant TenantID, worker WorkerID) bool
}

func AuthorizeWorker(order WorkOrder, tenant TenantID, worker WorkerID, directory WorkerDirectory) Decision {
	if order.ID == "" || order.Tenant == "" || order.Project == "" || tenant == "" || tenant != order.Tenant || worker == "" {
		return Decision{Reason: "tenant_or_order_scope"}
	}
	assigned := false
	for _, a := range order.Assignments {
		if a.Tenant == tenant && a.WorkOrder == order.ID && a.Worker == worker && a.State == Active {
			assigned = true
			break
		}
	}
	if !assigned {
		return Decision{Reason: "no_active_assignment"}
	}
	if directory == nil || !directory.Eligible(tenant, worker) {
		return Decision{Reason: "worker_ineligible"}
	}
	return Decision{Allowed: true, Reason: "active_assignment_and_eligibility"}
}

// CanDecide enforces separation of duties against the immutable request actor.
// Callers must pass current authorization and approval authority for the exact
// proposal revision at the moment the decision is recorded.
func CanDecide(requester, approver UserID, authorized bool) Decision {
	if requester == "" || approver == "" {
		return Decision{Reason: "invalid_requester_or_approver"}
	}
	if requester == approver {
		return Decision{Reason: "requester_approver_separation"}
	}
	if !authorized {
		return Decision{Reason: "approver_not_authorized_at_decision_time"}
	}
	return Decision{Allowed: true, Reason: "authorized_independent_approver"}
}

// FieldVisible prevents a cost-total grant from disclosing individual pay rates.
func FieldVisible(order WorkOrder, project projectaccess.Project, actor UserID, tenant TenantID, field Capability) bool {
	if field != ViewCost && field != ViewPay {
		return false
	}
	return Authorize(order, project, actor, tenant, field).Allowed
}

func ValidateRequest(requester, approver UserID) error {
	if requester == "" || approver == "" {
		return ErrInvalidRequest
	}
	if requester == approver {
		return ErrSelfApproval
	}
	return nil
}
