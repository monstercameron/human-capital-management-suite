package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

func TestTodo_UXBLIND_002(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "routing service", err: envelope.New(envelope.CodeUnavailable, journeyFailureDomainUnavailable, "unavailable"), want: journeyFailureDomainUnavailable},
		{name: "storage", err: envelope.New(envelope.CodeFailedPrecondition, journeyFailureStorageFailed, "storage failed"), want: journeyFailureStorageFailed},
		{name: "stage precondition", err: workspace.ErrJourneyStage, want: journeyFailureStagePrecondition},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := journeyFailureReason(tc.err); got != tc.want {
				t.Fatalf("journeyFailureReason = %q, want %q", got, tc.want)
			}
		})
	}
	if got := journeyFailureReason(nil); got != "" || journeyFailureReason(errors.New("unclassified")) != "" {
		t.Fatalf("unclassified failure reason = %q, want empty", got)
	}
}

func TestTodo_UXBLIND_002_FailureTimelineValidatesSchemaAndIntent(t *testing.T) {
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(mustFailureStruct(t, map[string]any{
		"actor": "principal:reviewer", "intent_id": "intent-1",
		"reason_ref": journeyFailureDomainUnavailable, "revision_id": "revision-1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	events := journeyFailureEvents([]TimelineEntry{{SchemaRef: JourneyFailureSchemaRef, Payload: payload, OccurredAt: time.Unix(10, 0)}}, "intent-1")
	if len(events) != 1 || events[0].Kind != JourneyEventApprovalStartFailed || events[0].Actor != "principal:reviewer" {
		t.Fatalf("valid failure projection = %+v", events)
	}
	if got := journeyFailureEvents([]TimelineEntry{{SchemaRef: JourneyFailureSchemaRef, Payload: payload}}, "other-intent"); len(got) != 0 {
		t.Fatalf("mismatched intent projected %d events", len(got))
	}
	unknownSchema := []TimelineEntry{{SchemaRef: "other.schema@1", Payload: payload}}
	if got := journeyFailureEvents(unknownSchema, "intent-1"); len(got) != 0 {
		t.Fatalf("unknown schema projected %d events", len(got))
	}
}

func TestTodo_UXBLIND_002_FailureTimelineRejectsUnknownFields(t *testing.T) {
	payload, err := (proto.MarshalOptions{Deterministic: true}).Marshal(mustFailureStruct(t, map[string]any{
		"actor": "principal:reviewer", "intent_id": "intent-1",
		"reason_ref": journeyFailureDomainUnavailable, "revision_id": "revision-1", "raw": "provider detail",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := journeyFailureEvents([]TimelineEntry{{SchemaRef: JourneyFailureSchemaRef, Payload: payload}}, "intent-1"); len(got) != 0 {
		t.Fatalf("payload with unknown field projected %d events", len(got))
	}
}

func TestTodo_UXBLIND_002_FailureRecorderErrorIsStableAndClassifiable(t *testing.T) {
	cause := errors.New("database password leaked")
	recordErr := journeyFailureRecordError{cause: cause}
	if recordErr.Error() != ErrJourneyFailureRecord.Error() {
		t.Fatalf("record error text = %q, want stable sentinel text", recordErr.Error())
	}
	if !errors.Is(recordErr, ErrJourneyFailureRecord) || !errors.Is(recordErr, cause) {
		t.Fatalf("record error does not preserve sentinel and private cause")
	}
	if strings.Contains(recordErr.Error(), "database password") {
		t.Fatal("record error exposed private recorder detail")
	}
	original := envelope.New(envelope.CodeUnavailable, journeyFailureDomainUnavailable, "service unavailable")
	joined := errors.Join(original, recordErr)
	if got := journeyFailureReason(joined); got != journeyFailureDomainUnavailable {
		t.Fatalf("joined failure reason = %q, want %q", got, journeyFailureDomainUnavailable)
	}
}

func mustFailureStruct(t *testing.T, fields map[string]any) *structpb.Struct {
	t.Helper()
	value, err := structpb.NewStruct(fields)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
