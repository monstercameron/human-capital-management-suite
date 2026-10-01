package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

type personaAudienceFloorSnapshotFake struct {
	snapshot PersonaAudienceFloorSnapshot
	err      error
}

func (f *personaAudienceFloorSnapshotFake) ReadPersonaAudienceFloorSnapshot(context.Context, chat.Conversation) (PersonaAudienceFloorSnapshot, error) {
	return f.snapshot, f.err
}

type personaAudienceFloorDisclosureFake struct{ err error }

func (f *personaAudienceFloorDisclosureFake) AuthorizePersonaAudienceDisclosure(context.Context, chatrecipient.AudiencePrincipal, chatrecipient.Disclosure) error {
	return f.err
}

type personaAudienceFloorPolicyFake struct{ err error }

func (f *personaAudienceFloorPolicyFake) AllowPersonaAudienceDataClass(context.Context, chat.Conversation, dlp.DataClass) error {
	return f.err
}

func personaAudienceFloorConversation() chat.Conversation {
	return chat.Conversation{ID: "room", TenantID: "tenant", Kind: chat.PublicChannel, Revision: 9}
}

func personaAudienceFloorSnapshot() PersonaAudienceFloorSnapshot {
	return PersonaAudienceFloorSnapshot{
		Revision: 41, CurrentComplete: true, EligibilityComplete: true, GuestExternalComplete: true, FenceComplete: true,
		CurrentMembers: []chatrecipient.AudiencePrincipal{{TenantID: "tenant", SubjectID: "member"}},
		EligibleFutureMembers: []chatrecipient.AudiencePrincipal{
			{TenantID: "tenant", SubjectID: "member"},
			{TenantID: "guest-tenant", SubjectID: "guest", Guest: true, External: true},
		},
	}
}

func personaAudienceFloorDisclosure() chatrecipient.Disclosure {
	return chatrecipient.Disclosure{SourceID: "source", RecordID: "record", Field: "field", DataClass: dlp.ClassPublic}
}

func TestTodo_AGENTP_012(t *testing.T) {
	source := &personaAudienceFloorSnapshotFake{snapshot: personaAudienceFloorSnapshot()}
	disclosure := &personaAudienceFloorDisclosureFake{}
	policy := &personaAudienceFloorPolicyFake{}
	authority := NewPersonaAudienceFloorAdapter(source, disclosure, policy)

	got, err := authority.CurrentAudience(context.Background(), personaAudienceFloorConversation())
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 41 || !got.Complete || !got.EligibilityComplete || !got.GuestAndExternalComplete || len(got.CurrentMembers) != 1 || len(got.EligibilityPopulation) != 2 {
		t.Fatalf("audience snapshot = %+v", got)
	}
	if !got.EligibilityPopulation[1].Guest || !got.EligibilityPopulation[1].External {
		t.Fatalf("guest/external eligibility was lost: %+v", got.EligibilityPopulation)
	}
	got.CurrentMembers[0].SubjectID = "mutated"
	if source.snapshot.CurrentMembers[0].SubjectID != "member" {
		t.Fatal("adapter exposed source audience backing storage")
	}
	if err := authority.AuthorizeDisclosure(context.Background(), got.CurrentMembers[0], personaAudienceFloorDisclosure()); err != nil {
		t.Fatal(err)
	}
	if err := authority.AllowDataClass(context.Background(), personaAudienceFloorConversation(), dlp.ClassPublic); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_AGENTP_012_Security(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PersonaAudienceFloorSnapshot)
	}{
		{name: "membership-only fence", mutate: func(s *PersonaAudienceFloorSnapshot) { s.FenceComplete = false }},
		{name: "future eligibility incomplete", mutate: func(s *PersonaAudienceFloorSnapshot) { s.EligibilityComplete = false }},
		{name: "guest external coverage incomplete", mutate: func(s *PersonaAudienceFloorSnapshot) { s.GuestExternalComplete = false }},
		{name: "future population omitted", mutate: func(s *PersonaAudienceFloorSnapshot) { s.EligibleFutureMembers = nil }},
		{name: "current population omitted", mutate: func(s *PersonaAudienceFloorSnapshot) { s.CurrentMembers = nil }},
		{name: "invalid principal", mutate: func(s *PersonaAudienceFloorSnapshot) { s.CurrentMembers[0].SubjectID = " " }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := personaAudienceFloorSnapshot()
			tc.mutate(&snapshot)
			authority := NewPersonaAudienceFloorAdapter(&personaAudienceFloorSnapshotFake{snapshot: snapshot}, &personaAudienceFloorDisclosureFake{}, &personaAudienceFloorPolicyFake{})
			if _, err := authority.CurrentAudience(context.Background(), personaAudienceFloorConversation()); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
				t.Fatalf("CurrentAudience error = %v, want unavailable", err)
			}
		})
	}

	authority := NewPersonaAudienceFloorAdapter(&personaAudienceFloorSnapshotFake{snapshot: personaAudienceFloorSnapshot()}, &personaAudienceFloorDisclosureFake{}, &personaAudienceFloorPolicyFake{})
	if err := authority.AuthorizeDisclosure(context.Background(), chatrecipient.AudiencePrincipal{}, personaAudienceFloorDisclosure()); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("invalid principal error = %v", err)
	}
	if err := authority.AuthorizeDisclosure(context.Background(), chatrecipient.AudiencePrincipal{TenantID: "tenant", SubjectID: "member"}, chatrecipient.Disclosure{SourceID: "s", RecordID: "r", Field: "f", DataClass: "UNKNOWN"}); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("unknown class error = %v", err)
	}
	if err := authority.AllowDataClass(context.Background(), personaAudienceFloorConversation(), dlp.DataClass("UNKNOWN")); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
		t.Fatalf("unknown policy class error = %v", err)
	}
}

func TestTodo_AGENTP_012_Fault(t *testing.T) {
	conversation := personaAudienceFloorConversation()
	for _, tc := range []struct {
		name string
		make func() *PersonaAudienceFloorAdapter
	}{
		{name: "nil adapter", make: func() *PersonaAudienceFloorAdapter { return nil }},
		{name: "nil snapshot source", make: func() *PersonaAudienceFloorAdapter {
			return NewPersonaAudienceFloorAdapter(nil, &personaAudienceFloorDisclosureFake{}, &personaAudienceFloorPolicyFake{})
		}},
		{name: "snapshot source error", make: func() *PersonaAudienceFloorAdapter {
			return NewPersonaAudienceFloorAdapter(&personaAudienceFloorSnapshotFake{err: errors.New("down")}, &personaAudienceFloorDisclosureFake{}, &personaAudienceFloorPolicyFake{})
		}},
		{name: "disclosure port error", make: func() *PersonaAudienceFloorAdapter {
			return NewPersonaAudienceFloorAdapter(&personaAudienceFloorSnapshotFake{snapshot: personaAudienceFloorSnapshot()}, &personaAudienceFloorDisclosureFake{err: errors.New("denied")}, &personaAudienceFloorPolicyFake{})
		}},
		{name: "policy port error", make: func() *PersonaAudienceFloorAdapter {
			return NewPersonaAudienceFloorAdapter(&personaAudienceFloorSnapshotFake{snapshot: personaAudienceFloorSnapshot()}, &personaAudienceFloorDisclosureFake{}, &personaAudienceFloorPolicyFake{err: errors.New("blocked")})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authority := tc.make()
			if _, err := authority.CurrentAudience(context.Background(), conversation); tc.name == "nil adapter" {
				if !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
					t.Fatalf("nil adapter error = %v", err)
				}
			} else if tc.name == "disclosure port error" || tc.name == "policy port error" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
				t.Fatalf("CurrentAudience error = %v", err)
			}
			if tc.name == "disclosure port error" {
				if err := authority.AuthorizeDisclosure(context.Background(), chatrecipient.AudiencePrincipal{TenantID: "tenant", SubjectID: "member"}, personaAudienceFloorDisclosure()); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
					t.Fatalf("disclosure error = %v", err)
				}
			}
			if tc.name == "policy port error" {
				if err := authority.AllowDataClass(context.Background(), conversation, dlp.ClassPublic); !errors.Is(err, ErrPersonaAudienceFloorUnavailable) {
					t.Fatalf("policy error = %v", err)
				}
			}
		})
	}
}
