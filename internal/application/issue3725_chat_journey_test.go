package application

import (
	"context"
	"errors"
	"testing"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type chat3725Intents struct {
	inst *intentsv1.IntentInstance
	err  error
	read []string
}

func (i *chat3725Intents) ReadIntent(_ context.Context, _ *trust.Principal, id string) (*intentsv1.IntentInstance, error) {
	i.read = append(i.read, id)
	return i.inst, i.err
}

func TestIssue3725_ChatJourneyDirectoryReadsAsTheSenderAndFailsClosed_Security(t *testing.T) {
	p := chat.Principal{TenantID: "t1", SubjectID: "u1"}
	inst := &intentsv1.IntentInstance{IntentId: "j1", Definition: &intentsv1.DefinitionReference{IntentTypeId: "promotion.request"}}
	intents := &chat3725Intents{inst: inst}
	d := chatJourneyDirectory{Intents: intents}

	if _, err := d.JourneyVisible(context.Background(), p, "j1"); !errors.Is(err, chat.ErrPermissionDenied) || len(intents.read) != 0 {
		t.Fatalf("no verified caller: err = %v reads = %v, want denied before any read", err, intents.read)
	}
	other := trust.WithPrincipal(context.Background(), mustChat3752Principal(t, "t1", "someone-else"))
	if _, err := d.JourneyVisible(other, p, "j1"); !errors.Is(err, chat.ErrPermissionDenied) || len(intents.read) != 0 {
		t.Fatalf("mismatched caller: err = %v reads = %v", err, intents.read)
	}
	self := trust.WithPrincipal(context.Background(), mustChat3752Principal(t, "t1", "u1"))
	name, err := d.JourneyVisible(self, p, "j1")
	if err != nil || name != "Promotion request" {
		t.Fatalf("readable journey: name = %q err = %v", name, err)
	}
	for label, source := range map[string]error{"not found": ErrChatWorkNotFound, "denied": ErrChatWorkDenied} {
		d = chatJourneyDirectory{Intents: &chat3725Intents{err: source}}
		if _, err := d.JourneyVisible(self, p, "j1"); !errors.Is(err, chat.ErrPermissionDenied) {
			t.Fatalf("%s: err = %v, want one refusal that does not confirm the journey exists", label, err)
		}
	}
	d = chatJourneyDirectory{Intents: &chat3725Intents{err: ErrChatWorkUnavailable}}
	if _, err := d.JourneyVisible(self, p, "j1"); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("owner down: err = %v, want unavailable", err)
	}
	if _, err := (chatJourneyDirectory{}).JourneyVisible(self, p, "j1"); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("intent service not composed: err = %v, want unavailable", err)
	}
}

type chat3725Linker struct{ got []chatstore.WorkLink }

func (l *chat3725Linker) LinkWork(_ context.Context, w chatstore.WorkLink) error {
	l.got = append(l.got, w)
	return nil
}

func TestIssue3725_ChatJourneyShareLinkerRecordsTheConversation(t *testing.T) {
	l := &chat3725Linker{}
	if err := (chatJourneyShareLinker{Links: l}).LinkJourneyShare(context.Background(), "t1", "c1", "j1", "u1"); err != nil {
		t.Fatal(err)
	}
	want := chatstore.WorkLink{TenantID: "t1", ConversationID: "c1", Kind: chatstore.WorkLinkJourneyShare, SourceID: "j1", ProposedByID: "u1"}
	if len(l.got) != 1 || l.got[0] != want {
		t.Fatalf("links = %+v, want %+v", l.got, want)
	}
	if err := (chatJourneyShareLinker{}).LinkJourneyShare(context.Background(), "t1", "c1", "j1", "u1"); err == nil {
		t.Fatal("a linker with no store reported success")
	}
}
