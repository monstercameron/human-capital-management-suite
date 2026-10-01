package workitem

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type smbConversionAuth struct {
	readErr   error
	createErr error
}

func (a smbConversionAuth) CanReadMessage(context.Context, MessageRef, string) error {
	return a.readErr
}
func (a smbConversionAuth) CanCreateTarget(context.Context, uuid.UUID, ConversionTargetKind, string) error {
	return a.createErr
}

type smbConversionSink struct {
	calls int
	last  MessageConversion
}

func (s *smbConversionSink) CreateTarget(_ context.Context, in MessageConversion) (string, error) {
	s.calls++
	s.last = in
	return "target-42", nil
}

func smbConversionFixture() MessageConversionRequest {
	return MessageConversionRequest{
		Message: MessageRef{TenantID: uuid.New(), ConversationID: "room-7", MessageID: "msg-9", AuthorID: "author-1", Body: "Please update the handbook."},
		ActorID: "author-1", Target: ConversionTargetDocumentCandidate, OwnerID: "owner-1", SourceContext: "room-7/msg-9", IdempotencyKey: "convert-1",
	}
}

func TestTodo_SMB_004(t *testing.T) {
	sink := &smbConversionSink{}
	coordinator := NewMessageConversionCoordinator(smbConversionAuth{}, sink)
	in := smbConversionFixture()
	result, err := coordinator.Convert(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetID != "target-42" || result.Target != ConversionTargetDocumentCandidate || result.ConversionID != sink.last.ConversionID || sink.last.SourceContext != "room-7/msg-9" {
		t.Fatalf("conversion result/source binding = %+v / %+v", result, sink.last)
	}
	if _, err := coordinator.Convert(context.Background(), in); err != nil || sink.calls != 1 {
		t.Fatalf("retry = err %v, sink calls %d; want one idempotent target", err, sink.calls)
	}
}

func TestTodo_SMB_004_Browser(t *testing.T) {
	sink := &smbConversionSink{}
	coordinator := NewMessageConversionCoordinator(smbConversionAuth{}, sink)
	result, err := coordinator.Convert(context.Background(), smbConversionFixture())
	if err != nil || result.TargetID == "" {
		t.Fatalf("component-level conversion handoff = %+v, err=%v", result, err)
	}
	if sink.last.Source.Body == "" || sink.last.Source.ConversationID == "" {
		t.Fatal("conversion omitted source context needed by the destination review")
	}
}

func TestTodo_SMB_004_Security(t *testing.T) {
	in := smbConversionFixture()
	sink := &smbConversionSink{}
	deniedRead := NewMessageConversionCoordinator(smbConversionAuth{readErr: errors.New("message not visible")}, sink)
	if _, err := deniedRead.Convert(context.Background(), in); err == nil || sink.calls != 0 {
		t.Fatalf("unauthorized source conversion err=%v, sink calls=%d", err, sink.calls)
	}
	deniedDestination := NewMessageConversionCoordinator(smbConversionAuth{createErr: errors.New("destination denied")}, sink)
	if _, err := deniedDestination.Convert(context.Background(), in); err == nil || sink.calls != 0 {
		t.Fatalf("unauthorized destination conversion err=%v, sink calls=%d", err, sink.calls)
	}
	in.Target = ConversionTargetKind("POLICY")
	if _, err := NewMessageConversionCoordinator(smbConversionAuth{}, sink).Convert(context.Background(), in); err == nil || sink.calls != 0 {
		t.Fatalf("invalid target conversion err=%v, sink calls=%d", err, sink.calls)
	}
	in = smbConversionFixture()
	coordinator := NewMessageConversionCoordinator(smbConversionAuth{}, sink)
	if _, err := coordinator.Convert(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	in.Message.MessageID = "different-message"
	if _, err := coordinator.Convert(context.Background(), in); err == nil || sink.calls != 1 {
		t.Fatalf("mismatched idempotency conversion err=%v, sink calls=%d", err, sink.calls)
	}
}
