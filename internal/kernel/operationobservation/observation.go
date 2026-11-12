// Package operationobservation holds the typed provider read-back that moves an
// accepted or ambiguous connector operation to completion: its verdict and the
// observation record.
//
// The values live apart from internal/connectivity/operation, whose journal,
// privacy gate and dispatch machinery pull in the governance and trust
// packages. Payroll and payable consume only these values, so the browser
// client can link them without linking the operation journal. The operation
// package re-exports both types and every verdict constant as aliases.
package operationobservation

import (
	"time"

	"github.com/google/uuid"
)

// ObservationVerdict is the provider read-back result for an operation.
type ObservationVerdict string

const (
	ObservationApplied    ObservationVerdict = "APPLIED"
	ObservationNotApplied ObservationVerdict = "NOT_APPLIED"
	ObservationConflict   ObservationVerdict = "CONFLICT"
	ObservationUnknown    ObservationVerdict = "UNKNOWN"
)

// Observation is a typed provider read-back. It is the only input that can
// move an accepted or ambiguous operation to business completion.
type Observation struct {
	TenantID            string
	ObservationID       uuid.UUID
	OperationID         uuid.UUID
	ExternalResourceKey string
	Verdict             ObservationVerdict
	ExternalObjectRef   string
	ExternalVersion     string
	ObservedDigest      string
	ObservedAt          time.Time
	Authority           string
}
