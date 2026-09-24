package roleaccessstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ErrInvalidRevision marks a revision entry the ledger must not record.
var ErrInvalidRevision = errors.New("roleaccessstore: invalid revision entry")

// ChangeKind identifies which saved shape a revision records. The set
// matches the access_role_revision change_kind check in migration 00327.
type ChangeKind string

const (
	// RevisionRole records an access role create, rename, deactivation or
	// reactivation.
	RevisionRole ChangeKind = "ROLE"
	// RevisionAssignment records a worker assignment save.
	RevisionAssignment ChangeKind = "ASSIGNMENT"
	// RevisionVisibility records an organization visibility save.
	RevisionVisibility ChangeKind = "VISIBILITY"
	// RevisionPagePermission records a page permission save.
	RevisionPagePermission ChangeKind = "PAGE_PERMISSION"
	// RevisionFeaturePermission records a feature permission save.
	RevisionFeaturePermission ChangeKind = "FEATURE_PERMISSION"
)

// RevisionEntry is one append-only permission revision: the before and
// after image of a single role's row for a single change. A change that
// writes rows for more than one role saves one entry per role. Before is
// nil for creates, After is nil for removals; PriorRevision is nil for the
// first revision of its row.
type RevisionEntry struct {
	RevisionID          uuid.UUID
	ActorRef            string
	Reason              string
	Kind                ChangeKind
	RoleID              string
	WorkerRef           string
	OrganizationScopeID string
	PageID              string
	FeatureID           string
	Before              []byte
	After               []byte
	PriorRevision       *uuid.UUID
}

type membershipImage struct {
	Version int64 `json:"version"`
	Present bool  `json:"present"`
}

func containsRole(roleIDs []string, roleID string) bool {
	for _, candidate := range roleIDs {
		if candidate == roleID {
			return true
		}
	}
	return false
}

// NewRevision builds the ledger entry for one role's row change, encoding
// the before and after images as JSON. The store inserts the entry in the
// same transaction as the permission change; this constructor only shapes
// and validates the row.
func NewRevision(revisionID uuid.UUID, actorRef string, kind ChangeKind, roleID, workerRef, pageID, featureID string, before, after any, prior *uuid.UUID, reasons ...string) (RevisionEntry, error) {
	return newScopedRevision(revisionID, actorRef, kind, roleID, workerRef, "", pageID, featureID, before, after, prior, reasons...)
}

func newScopedRevision(revisionID uuid.UUID, actorRef string, kind ChangeKind, roleID, workerRef, organizationScopeID, pageID, featureID string, before, after any, prior *uuid.UUID, reasons ...string) (RevisionEntry, error) {
	reason := ""
	if len(reasons) == 1 {
		reason = reasons[0]
	} else if len(reasons) > 1 {
		return RevisionEntry{}, fmt.Errorf("%w: exactly one reason may be supplied", ErrInvalidRevision)
	}
	switch kind {
	case RevisionRole, RevisionAssignment, RevisionVisibility, RevisionPagePermission, RevisionFeaturePermission:
	default:
		return RevisionEntry{}, fmt.Errorf("%w: unknown change kind %q", ErrInvalidRevision, kind)
	}
	if revisionID == uuid.Nil {
		return RevisionEntry{}, fmt.Errorf("%w: revision id is required", ErrInvalidRevision)
	}
	if strings.TrimSpace(actorRef) == "" {
		return RevisionEntry{}, fmt.Errorf("%w: actor ref is required", ErrInvalidRevision)
	}
	if strings.TrimSpace(reason) == "" || len(strings.TrimSpace(reason)) > 500 || strings.ContainsAny(reason, "\r\n\x00") {
		return RevisionEntry{}, fmt.Errorf("%w: a single-line reason of at most 500 characters is required", ErrInvalidRevision)
	}
	if strings.TrimSpace(roleID) == "" {
		return RevisionEntry{}, fmt.Errorf("%w: role id is required", ErrInvalidRevision)
	}
	beforeJSON, err := marshalRevisionImage(before)
	if err != nil {
		return RevisionEntry{}, fmt.Errorf("%w: before image: %v", ErrInvalidRevision, err)
	}
	afterJSON, err := marshalRevisionImage(after)
	if err != nil {
		return RevisionEntry{}, fmt.Errorf("%w: after image: %v", ErrInvalidRevision, err)
	}
	return RevisionEntry{
		RevisionID: revisionID, ActorRef: actorRef, Reason: strings.TrimSpace(reason),
		Kind: kind, RoleID: roleID, WorkerRef: workerRef, OrganizationScopeID: organizationScopeID,
		PageID: pageID, FeatureID: featureID, Before: beforeJSON, After: afterJSON, PriorRevision: prior,
	}, nil
}

func marshalRevisionImage(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
