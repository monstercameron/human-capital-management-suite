package chatrecords

import (
	"context"
	"time"
)

// Transport is the narrow application boundary used by HTTP and gRPC
// adapters. It accepts trusted actor identity from the edge and delegates all
// policy and mutation decisions to Service.
type Transport struct{ Service *Service }

// Root is the composition-root owned capability. It keeps the durable store
// and trusted authorization adapter injectable for the runtime.
type Root struct{ Service *Service }

func NewRoot(repo Repository, auth Authorizer, clock func() time.Time) *Root {
	return &Root{Service: &Service{Repo: repo, Auth: auth, Clock: clock}}
}

func (t Transport) Append(ctx context.Context, actor string, record Record, action, reason string) (AuditEvent, error) {
	return t.Service.AppendRecord(ctx, actor, record, action, reason)
}
func (t Transport) Export(ctx context.Context, actor, tenant, id string) (Export, error) {
	return t.Service.Export(ctx, actor, tenant, id)
}
func (t Transport) Report(ctx context.Context, actor string, report Report) error {
	return t.Service.Report(ctx, actor, report)
}
func (t Transport) Moderate(ctx context.Context, actor, tenant, caseID, action, target, reason, evidence string) error {
	return t.Service.Moderate(ctx, actor, tenant, caseID, action, target, reason, evidence)
}
func (t Transport) Backup(ctx context.Context, actor, tenant string) (Snapshot, error) {
	return t.Service.Snapshot(ctx, actor, tenant)
}
func (t Transport) Restore(ctx context.Context, actor string, snapshot Snapshot) (ReconcileResult, error) {
	return t.Service.Restore(ctx, actor, snapshot)
}
