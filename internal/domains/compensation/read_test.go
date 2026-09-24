package compensation_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/compensation"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type absentCompensationReader struct{ query compensation.Query }

func (r *absentCompensationReader) CompensationAt(_ context.Context, q compensation.Query) (compensation.FactSet, error) {
	r.query = q
	return compensation.FactSet{Worker: q.Worker}, nil
}

func TestCompensationReadReportsNoRecordWithoutInventingFacts(t *testing.T) {
	tenant := values.TenantId("tenant-a")
	worker := values.EntityRef{Tenant: tenant, Kind: people.KindWorker, Id: uuid.NewString()}
	instant := values.NewInstant(time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
	knownAt, err := values.NewKnownAt(instant)
	if err != nil {
		t.Fatal(err)
	}
	effectiveOn, err := values.NewLocalDate(2026, time.September, 23)
	if err != nil {
		t.Fatal(err)
	}
	reader := &absentCompensationReader{}
	result, err := compensation.Read(context.Background(), reader, compensation.Request{
		Tenant: tenant, Worker: worker,
		AsOf:   people.AsOf{EffectiveOn: effectiveOn, KnownAt: knownAt},
		Fields: []compensation.FieldID{compensation.FieldAmount},
		Authorization: compensation.AuthorizationDecision{
			PolicyVersion: "policy/1", Purpose: "self-service", SubjectDisclosable: true,
			Fields: map[compensation.FieldID]compensation.FieldRuling{
				compensation.FieldAmount: {Effect: compensation.EffectAllow},
			},
		},
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if result.Presence != "ABSENT" || len(result.Fields) != 0 || result.Watermark.IsSpecified() {
		t.Fatalf("absent result = presence %q, fields %d, watermark %v; want ABSENT with no facts or watermark", result.Presence, len(result.Fields), result.Watermark.IsSpecified())
	}
	if reader.query.Worker != worker || len(reader.query.Fields) != 1 || reader.query.Fields[0] != compensation.FieldAmount {
		t.Fatalf("reader query = %+v; want authenticated subject and requested field", reader.query)
	}
}
