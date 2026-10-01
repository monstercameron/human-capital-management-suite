package clockservice

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Service composes every port this package's use cases need. A zero-value
// Service field is a wiring defect, not a caller error: every use case
// checks the ports it needs and returns ErrUnavailable rather than
// panicking on a nil interface.
type Service struct {
	Sessions     SessionStore
	Observations ObservationStore
	Receipts     ReceiptStore
	Devices      DeviceStore
	Enrollments  EnrollmentStore
	Credentials  CredentialStore
	Heartbeats   HeartbeatStore
	Roster       RosterSource
	Policies     PolicySource
	Profiles     ProfileResolver
	Workers      WorkerDirectory
	Auth         Authorizer
	Workflow     SessionWorkflow
	// PunchWorkflow owns synchronous workflow acceptance before any punch write.
	PunchWorkflow WorkflowPunchExecutor
	BatchWorkflow WorkflowBatchExecutor
	Outbox        EventOutbox
	Work          UnitOfWork
	PremiumInputs PremiumInputs
	CaseTasks     CaseTasks
	IDs           IDs
	Clock         func() time.Time
}

func (s Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func validPrincipal(p *trust.Principal) error {
	if p == nil || p.Subject() == "" || string(p.Tenant()) == "" {
		return ErrInvalidPrincipal
	}
	return nil
}

func tenantOf(p *trust.Principal) string { return string(p.Tenant()) }

func requireNonEmpty(fields ...string) bool {
	for _, f := range fields {
		if strings.TrimSpace(f) == "" {
			return false
		}
	}
	return true
}
