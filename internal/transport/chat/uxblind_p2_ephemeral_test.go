package chat

import (
	"context"
	"errors"
	"testing"

	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

type ephemeralTransportService struct {
	chatcore.ConversationService
	post chatcore.EphemeralPost
}

func (s ephemeralTransportService) SendEphemeralPost(context.Context, chatcore.SendEphemeralPostRequest) (chatcore.EphemeralPost, error) {
	return s.post, nil
}

func TestTodo_AGENTP_011_Transport(t *testing.T) {
	p := chatcore.Principal{TenantID: "tenant", SubjectID: "invoker"}
	service := ephemeralTransportService{post: chatcore.EphemeralPost{OnlyVisibleToYou: true, RecipientHomeTenantID: p.TenantID, RecipientSubjectID: p.SubjectID}}
	got, err := SendEphemeralPost(context.Background(), service, chatcore.SendEphemeralPostRequest{Principal: p})
	if err != nil || got.RecipientSubjectID != p.SubjectID {
		t.Fatalf("projection=%+v err=%v", got, err)
	}
}

func TestTodo_AGENTP_011_TransportSecurity(t *testing.T) {
	p := chatcore.Principal{TenantID: "tenant", SubjectID: "invoker"}
	service := ephemeralTransportService{post: chatcore.EphemeralPost{OnlyVisibleToYou: true, RecipientHomeTenantID: p.TenantID, RecipientSubjectID: "other"}}
	if _, err := SendEphemeralPost(context.Background(), service, chatcore.SendEphemeralPostRequest{Principal: p}); err == nil || errors.Is(err, chatcore.ErrUnavailable) {
		t.Fatalf("forged recipient projection accepted: %v", err)
	}
}
