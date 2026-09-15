package runtime

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// ExecutionDelegation is the authority a workflow instance's later steps act
// under (WF-RUN-034). Steps run after the call that started the instance has
// returned -- on an approval resume, a timer the scheduler fires, a retry --
// so no authenticated caller exists when they invoke capabilities. The
// verified principal that executed the proposal is pinned here at start, and
// each step re-authorizes this delegation against the current policy before
// every capability invocation: the delegation is the upper bound of what a
// step may do, never a standing grant.
//
// It is deliberately not part of [ExecutionContext] or its digest: resume
// paths rebuild the same [StartRequest] without a delegation, and the pinned
// context must keep deriving identically for them.
type ExecutionDelegation struct {
	Subject             string
	SubjectKind         string
	TenantKey           string
	OrganizationScopeID string
	Roles               []string
	// Purposes are the purposes of processing the execution was authorized
	// under.
	Purposes []string
	// AuthenticationMethod, Assurance and SessionRef describe the
	// authentication event the delegating principal presented, in their
	// canonical wire spellings.
	AuthenticationMethod string
	Assurance            string
	SessionRef           string
	// EvidenceRef is the authentication evidence identifier of that
	// credential.
	EvidenceRef string
	// RecordedAt is when the delegation was pinned. Set by
	// [LoadExecutionDelegation]; ignored on write, which uses the start's own
	// creation instant.
	RecordedAt time.Time
}

// Validate refuses a delegation missing any field a step needs to
// re-authorize it.
func (d ExecutionDelegation) Validate() error {
	for _, field := range []struct{ name, value string }{
		{"subject", d.Subject}, {"subject kind", d.SubjectKind}, {"tenant", d.TenantKey},
		{"authentication method", d.AuthenticationMethod}, {"assurance", d.Assurance},
		{"session", d.SessionRef}, {"authentication evidence", d.EvidenceRef},
	} {
		if strings.TrimSpace(field.value) == "" {
			return refuse(CodeInvalidRecord, "", "", "execution delegation names no %s", field.name)
		}
	}
	if len(d.Purposes) == 0 || slices.ContainsFunc(d.Purposes, func(p string) bool { return strings.TrimSpace(p) == "" }) {
		return refuse(CodeInvalidRecord, "", "", "execution delegation names no purpose")
	}
	return nil
}

func sortedSet(in []string) []string {
	out := append([]string{}, in...)
	slices.Sort(out)
	return slices.Compact(out)
}

// recordExecutionDelegation inserts the instance's delegation once, in the
// start transaction. A replayed start inserts nothing.
func recordExecutionDelegation(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, d ExecutionDelegation, recordedAt time.Time) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if _, err := ex.Exec(ctx, `INSERT INTO workflow_execution_delegation
		(tenant_id, instance_id, subject, subject_kind, tenant_key, organization_scope_id, roles, purposes,
		 authentication_method, assurance, session_ref, evidence_ref, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) ON CONFLICT (tenant_id, instance_id) DO NOTHING`,
		tenantID, instanceID, d.Subject, d.SubjectKind, d.TenantKey, d.OrganizationScopeID,
		sortedSet(d.Roles), sortedSet(d.Purposes), d.AuthenticationMethod, d.Assurance, d.SessionRef, d.EvidenceRef,
		recordedAt.UTC()); err != nil {
		return wrap(CodeStorageFailed, instanceID.String(), "", err, "record execution delegation")
	}
	return nil
}

// LoadExecutionDelegation returns the delegation an instance pinned at start.
// found is false for an instance started without one (a prototype run, or a
// start that predates WF-RUN-034); callers that need authority fail closed.
func LoadExecutionDelegation(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID) (ret0 ExecutionDelegation, found bool, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.runtime.load_execution_delegation", tenantID, instanceID)
	defer func() { observe.DoneWith(obsOp, retErr, found) }()
	var d ExecutionDelegation
	err := ex.QueryRow(ctx, `SELECT subject, subject_kind, tenant_key, organization_scope_id, roles, purposes,
		authentication_method, assurance, session_ref, evidence_ref, recorded_at
		FROM workflow_execution_delegation WHERE tenant_id = $1 AND instance_id = $2`, tenantID, instanceID).
		Scan(&d.Subject, &d.SubjectKind, &d.TenantKey, &d.OrganizationScopeID, &d.Roles, &d.Purposes,
			&d.AuthenticationMethod, &d.Assurance, &d.SessionRef, &d.EvidenceRef, &d.RecordedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return ExecutionDelegation{}, false, nil
	}
	if err != nil {
		return ExecutionDelegation{}, false, wrap(CodeStorageFailed, instanceID.String(), "", err, "load execution delegation")
	}
	d.RecordedAt = d.RecordedAt.UTC()
	return d, true, nil
}
