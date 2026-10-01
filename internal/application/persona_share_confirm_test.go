package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaShareConfirmPortFake struct {
	previewRequest chatrecipient.PersonaSharePreviewRequest
	confirmRequest chatrecipient.PersonaShareConfirmRequest
	previewCalls   int
	confirmCalls   int
}

func (f *personaShareConfirmPortFake) PreviewPersonaShare(_ context.Context, request chatrecipient.PersonaSharePreviewRequest) (chatrecipient.PersonaSharePreview, error) {
	f.previewCalls++
	f.previewRequest = request
	return chatrecipient.PersonaSharePreview{Token: "server-token"}, nil
}

func (f *personaShareConfirmPortFake) ConfirmPersonaShare(_ context.Context, request chatrecipient.PersonaShareConfirmRequest) (chatrecipient.PersonaShareConfirmResult, error) {
	f.confirmCalls++
	f.confirmRequest = request
	return chatrecipient.PersonaShareConfirmResult{Shared: true, PostID: "human-post"}, nil
}

func personaShareConfirmPrincipal(t *testing.T, tenant, subject string) *trust.Principal {
	t.Helper()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "share-session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		CredentialDigest: "share-credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPersonaShareConfirmationRequiresComposedPort(t *testing.T) {
	if _, err := NewPersonaShareConfirmation(PersonaShareConfirmConfig{}); !errors.Is(err, ErrPersonaShareConfirmUnavailable) {
		t.Fatalf("missing port error = %v", err)
	}
	var typedNil *personaShareConfirmPortFake
	if _, err := NewPersonaShareConfirmation(PersonaShareConfirmConfig{Service: typedNil}); !errors.Is(err, ErrPersonaShareConfirmUnavailable) {
		t.Fatalf("typed nil port error = %v", err)
	}
}

func TestPersonaShareConfirmationRequiresVerifiedHumanContext(t *testing.T) {
	port := &personaShareConfirmPortFake{}
	service, err := NewPersonaShareConfirmation(PersonaShareConfirmConfig{Service: port})
	if err != nil {
		t.Fatal(err)
	}
	principal := personaShareConfirmPrincipal(t, "tenant-a", "alice")
	if _, err := service.PreviewPersonaShare(context.Background(), PersonaSharePreviewRequest{Principal: principal, EphemeralResultID: "result"}); !errors.Is(err, chat.ErrUnauthenticated) {
		t.Fatalf("detached principal error = %v", err)
	}
	other := personaShareConfirmPrincipal(t, "tenant-a", "bob")
	ctx := trust.WithPrincipal(context.Background(), principal)
	if _, err := service.ConfirmPersonaShare(ctx, PersonaShareConfirmRequest{Principal: other, Token: "token"}); !errors.Is(err, chat.ErrUnauthenticated) {
		t.Fatalf("foreign principal error = %v", err)
	}
	if port.previewCalls != 0 || port.confirmCalls != 0 {
		t.Fatal("untrusted caller reached share port")
	}
}

func TestPersonaShareConfirmationBindsInvokerAndExplicitConfirmation(t *testing.T) {
	port := &personaShareConfirmPortFake{}
	service, err := NewPersonaShareConfirmation(PersonaShareConfirmConfig{Service: port})
	if err != nil {
		t.Fatal(err)
	}
	principal := personaShareConfirmPrincipal(t, "tenant-a", "alice")
	ctx := trust.WithPrincipal(context.Background(), principal)
	preview, err := service.PreviewPersonaShare(ctx, PersonaSharePreviewRequest{Principal: principal, EphemeralResultID: "result-1"})
	if err != nil || preview.Token != "server-token" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	if port.previewRequest.Principal.TenantID != "tenant-a" || port.previewRequest.Principal.SubjectID != "alice" {
		t.Fatalf("preview actor = %+v", port.previewRequest.Principal)
	}
	confirmed, err := service.ConfirmPersonaShare(ctx, PersonaShareConfirmRequest{Principal: principal, Token: preview.Token})
	if err != nil || !confirmed.Shared || confirmed.PostID != "human-post" {
		t.Fatalf("confirm=%+v err=%v", confirmed, err)
	}
	if port.confirmRequest.Principal.TenantID != "tenant-a" || port.confirmRequest.Principal.SubjectID != "alice" || port.confirmRequest.Token != "server-token" {
		t.Fatalf("confirm request = %+v", port.confirmRequest)
	}
}

func TestPersonaShareConfirmationRejectsMissingInputsBeforePort(t *testing.T) {
	port := &personaShareConfirmPortFake{}
	service, err := NewPersonaShareConfirmation(PersonaShareConfirmConfig{Service: port})
	if err != nil {
		t.Fatal(err)
	}
	principal := personaShareConfirmPrincipal(t, "tenant-a", "alice")
	ctx := trust.WithPrincipal(context.Background(), principal)
	if _, err := service.PreviewPersonaShare(ctx, PersonaSharePreviewRequest{Principal: principal}); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("missing result error = %v", err)
	}
	if _, err := service.ConfirmPersonaShare(ctx, PersonaShareConfirmRequest{Principal: principal}); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Fatalf("missing token error = %v", err)
	}
	if port.previewCalls != 0 || port.confirmCalls != 0 {
		t.Fatal("invalid request reached share port")
	}
}
