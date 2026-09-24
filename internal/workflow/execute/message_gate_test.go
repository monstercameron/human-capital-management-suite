package execute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/audience"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type messageGateTx struct{ calls int }

func (tx *messageGateTx) Exec(context.Context, string, ...any) (int64, error) {
	tx.calls++
	return 0, nil
}
func (tx *messageGateTx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	tx.calls++
	return nil, errors.New("unexpected query")
}
func (tx *messageGateTx) QueryRow(context.Context, string, ...any) dbport.Row {
	tx.calls++
	return messageGateRow{}
}
func (*messageGateTx) Commit(context.Context) error   { return nil }
func (*messageGateTx) Rollback(context.Context) error { return nil }

type messageGateRow struct{}

func (messageGateRow) Scan(...any) error { return errors.New("unexpected scan") }

func TestPublishWorkItemMessageRejectsUnboundAudienceBeforePersistence(t *testing.T) {
	owner := "approval-owner"
	tenantID, itemID, instanceID := uuid.New(), uuid.New(), uuid.New()
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	item := workitem.WorkItem{
		TenantID: tenantID, WorkItemID: itemID, WorkflowInstanceID: instanceID, CorrelationID: "corr",
		Kind: workitem.KindApproval, OrganizationScopeID: "org-1", RecordedAt: at, DeadlineAt: at.Add(time.Hour),
		Assignment: workitem.Assignment{ChosenOwner: owner, Resolution: humanwork.Resolution{Candidates: []humanwork.Candidate{{PrincipalID: owner}}}},
	}
	wrong := values.EntityRef{Tenant: values.TenantId(tenantID.String()), Kind: "principal", Id: uuid.NewSHA1(tenantID, []byte("another-principal")).String()}
	wrongSpec := audience.AudienceSpec{ExplicitSubjects: []values.EntityRef{wrong}}
	ownerRef := workItemAudiencePrincipal(tenantID, owner)
	ownerSpec := audience.AudienceSpec{ExplicitSubjects: []values.EntityRef{ownerRef}}
	cases := map[string]audience.Resolution{
		"unrelated principal": {Principals: []values.EntityRef{wrong}, Expression: wrongSpec.Expression(), ResolvedAt: values.NewInstant(at), ResultDigest: "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"},
		"empty digest":        {Principals: []values.EntityRef{ownerRef}, Expression: ownerSpec.Expression(), ResolvedAt: values.NewInstant(at)},
	}
	for name, resolution := range cases {
		t.Run(name, func(t *testing.T) {
			tx := &messageGateTx{}
			if err := PublishWorkItemMessage(context.Background(), tx, item, resolution); err == nil {
				t.Fatal("accepted audience resolution that was not bound to the routed recipient")
			}
			if tx.calls != 0 {
				t.Fatalf("rejected audience reached persistence: %d transaction calls", tx.calls)
			}
		})
	}
}
