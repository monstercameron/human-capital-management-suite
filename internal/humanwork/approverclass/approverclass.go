// Package approverclass is PROMOUX-003's approver-resolution seam: it binds
// one configured approver identity to an authority class so that two
// requirements naming different classes can never be silently routed to the
// identical principal.
//
// RED's second clause names a real defect: "the work items share an
// undifferentiated principal:promotion-approver owner" because the dev
// environment mints exactly that one subject for every promotion approval.
// Real per-class directory resolution (ManagerOf(worker), a finance
// partner's own identity) is a later contract; this package closes the
// narrower, provable gap in front of it -- a composition that names only one
// approver identity must still route its distinct authority classes to
// provably distinct principals, never to the same string twice.
//
// This package is pure: no database, no clock, no network. It mirrors the
// shape internal/trust/sod already uses for the same reason: a small,
// dependency-free evaluator that a caller from any layer can call without
// pulling in workflow or storage packages.
package approverclass

import "errors"

// Class identifies a promotion-approval authority class. It names the same
// concept as internal/workflow/promotionexec's AuthorityFloor constants
// ("finance_partner", "current_manager") without importing that package: an
// approver-resolution rule is a fact about identities, not about one
// workflow's compiled requirements.
type Class string

// Declared classes. ClassUnspecified is the zero value and is never legal:
// an empty class can never be told apart from another empty class, so
// treating it as a real class would silently defeat RequireDistinct.
const (
	ClassUnspecified Class = ""
	FinancePartner   Class = "finance_partner"
	CurrentManager   Class = "current_manager"
)

// Errors this package returns. Callers classify with [errors.Is]; there is
// no code carrier because approverclass has no wire boundary of its own.
var (
	// ErrNoBase reports an empty base approver identity: a class cannot be
	// derived from an identity that names nobody.
	ErrNoBase = errors.New("approverclass: no base approver identity")
	// ErrNoClass reports an unnamed authority class.
	ErrNoClass = errors.New("approverclass: no authority class named")
	// ErrUnresolved reports a class-scoped principal that never resolved to a
	// semantic identity -- the fail-closed answer for "this class has no
	// approver" rather than treating an empty string as a wildcard match.
	ErrUnresolved = errors.New("approverclass: authority class did not resolve to a principal")
	// ErrSharedOwner is RED clause 2 made unrepresentable: two different
	// authority classes resolved to the identical principal.
	ErrSharedOwner = errors.New("approverclass: two authority classes resolved to the same principal")
)

// DeriveDistinct maps one configured base approver identity to a
// class-scoped principal id that is provably different for every distinct
// class supplied for the same base, because the class name is part of the
// derived identity: DeriveDistinct(x, A) and DeriveDistinct(x, B) can never
// collide for A != B, regardless of what x is.
//
// It fails closed on an empty base or class rather than returning "": an
// empty result would compare equal to every other unresolved class, which is
// exactly the ambiguity [RequireDistinct] exists to make impossible.
func DeriveDistinct(base string, class Class) (string, error) {
	if base == "" {
		return "", ErrNoBase
	}
	if class == ClassUnspecified {
		return "", ErrNoClass
	}
	return base + "#" + string(class), nil
}

// RequireDistinct refuses when two resolved principals for different
// authority classes are the same identity, or when either failed to
// resolve at all. It is the fail-closed check RED clause 2 requires: a
// composition may never observe two approval requirements bound to one
// undifferentiated owner, whether that happened because resolution produced
// the same principal twice or because one class simply never resolved.
func RequireDistinct(financeApprover, managerApprover string) error {
	if financeApprover == "" || managerApprover == "" {
		return ErrUnresolved
	}
	if financeApprover == managerApprover {
		return ErrSharedOwner
	}
	return nil
}

// ErrRequesterApprover reports a resolved approver who is the requester of the
// proposal they would approve (the promote-into-management reference
// workflow's "requester may not approve" separation constraint).
var ErrRequesterApprover = errors.New("approverclass: an approver resolved to the proposal's requester")

// ErrSubjectApprover reports a resolved approver who is the subject of the
// proposal they would approve.
var ErrSubjectApprover = errors.New("approverclass: an approver resolved to the proposal's subject")

// RequireSeparated is PROMOUX-015's routing-time constraint: the finance and
// manager approvers must be resolved and distinct from each other
// ([RequireDistinct]), and neither may be the requester or any of the
// proposal's subjects. subjects lists every identity the subject is known by
// (a worker's key and its entity id); empty entries are ignored. It refuses
// in a fixed order -- unresolved or shared owner first, then requester, then
// subject -- so the same inputs always name the same violation.
func RequireSeparated(requester string, subjects []string, financeApprover, managerApprover string) error {
	if err := RequireDistinct(financeApprover, managerApprover); err != nil {
		return err
	}
	if requester == "" {
		return ErrUnresolved
	}
	if financeApprover == requester || managerApprover == requester {
		return ErrRequesterApprover
	}
	for _, subject := range subjects {
		if subject != "" && (financeApprover == subject || managerApprover == subject) {
			return ErrSubjectApprover
		}
	}
	return nil
}
