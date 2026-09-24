// Package pageledger defines the storage port for tenant-scoped published
// page revisions and rollouts. Payload verification belongs to productui.
package pageledger

import "context"

type RevisionRow struct {
	Page    string
	Version int64
	Digest  string
	Payload []byte
}

type RolloutRow struct {
	Page          string
	RecordVersion int64
	TargetVersion int64
	Digest        string
	Payload       []byte
}

type RetirementRow struct {
	Page    string
	Digest  string
	Payload []byte
}

// Store is the tenant-isolated append-only page publication ledger boundary.
type Store interface {
	PutRevision(context.Context, string, string, int64, string, []byte) error
	LoadRevisions(context.Context, string) ([]RevisionRow, error)
	PutRollout(context.Context, string, string, int64, int64, string, []byte) error
	LoadRollouts(context.Context, string) ([]RolloutRow, error)
	PutRetirement(context.Context, string, string, string, []byte) error
	LoadRetirements(context.Context, string) ([]RetirementRow, error)
}
