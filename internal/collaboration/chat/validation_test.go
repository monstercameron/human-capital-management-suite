package chat

import (
	"context"
	"errors"
	"testing"
)

func TestValidateCreateRejectsInvalidPrincipalBeforePersistence(t *testing.T) {
	s := NewService(nil, nil)
	err := s.ValidateCreate(context.Background(), CreateConversationRequest{TenantID: "t1", Kind: PublicChannel, Name: "x", Principal: Principal{TenantID: "other", SubjectID: "actor"}, IdempotencyKey: "k"})
	if !errors.Is(err, ErrPermissionDenied) && !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateCreateSeparatesActorsUsingSameKey(t *testing.T) {
	s := NewService(nil, nil)
	s.SetAuthority(verifiedAuthority{store: &fakeStore{}})
	base := CreateConversationRequest{TenantID: "t1", Kind: PublicChannel, Name: "x", IdempotencyKey: "same", Principal: Principal{TenantID: "t1", SubjectID: "a"}}
	if err := s.ValidateCreate(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	base.Principal.SubjectID = "b"
	if err := s.ValidateCreate(context.Background(), base); err != nil {
		t.Fatal(err)
	}
}
