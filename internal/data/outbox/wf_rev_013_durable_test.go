package outbox_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
)

// TestTodo_WF_REV_013_Durable proves the database-backed consumer applies a
// compensation once, leaves an early compensation retryable, and admits it
// only after the original event is committed for the same tenant and group.
func TestTodo_WF_REV_013_Durable(t *testing.T) {
	f := newNext004Fixture(t)
	ctx := context.Background()
	group := "wf-rev-013-durable"
	originalID, compensationID := uuid.New(), uuid.New()
	original := outbox.ConsumerEvent{
		Tenant: f.tenant, ConsumerGroup: group, StreamKey: "wf-rev-013-original",
		Sequence: 1, EventID: originalID, SchemaRef: next004SchemaRef,
		Digest: strings.Repeat("a", 64), SourceWatermark: 1,
	}
	compensation := outbox.ConsumerEvent{
		Tenant: f.tenant, ConsumerGroup: group, StreamKey: "wf-rev-013-compensation",
		Sequence: 1, EventID: compensationID, CompensatesEventID: originalID,
		SchemaRef: next004SchemaRef, Digest: strings.Repeat("b", 64), SourceWatermark: 1,
	}
	var originals, compensations int
	handler := func(_ context.Context, _ dbport.Tx, event outbox.ConsumerEvent) error {
		if event.CompensatesEventID == uuid.Nil {
			originals++
		} else {
			compensations++
		}
		return nil
	}

	if _, err := outbox.Process(ctx, f.db.Conn, compensation, handler); !errors.Is(err, outbox.ErrConsumerCompensationPending) {
		t.Fatalf("early compensation = %v, want ErrConsumerCompensationPending", err)
	}
	if compensations != 0 {
		t.Fatalf("early compensation reached its handler %d times, want zero", compensations)
	}

	if result, err := outbox.Process(ctx, f.db.Conn, original, handler); err != nil || result.Duplicate {
		t.Fatalf("original admission = %+v, %v", result, err)
	}
	if result, err := outbox.Process(ctx, f.db.Conn, compensation, handler); err != nil || result.Duplicate {
		t.Fatalf("compensation admission = %+v, %v", result, err)
	}
	if result, err := outbox.Process(ctx, f.db.Conn, compensation, handler); err != nil || !result.Duplicate {
		t.Fatalf("compensation redelivery = %+v, %v; want duplicate", result, err)
	}
	if originals != 1 || compensations != 1 {
		t.Fatalf("handler calls: original=%d compensation=%d, want one each", originals, compensations)
	}
}
