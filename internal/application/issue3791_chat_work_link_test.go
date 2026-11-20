package application

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

type issue3791Linker struct {
	got []chatstore.WorkLink
	err error
}

func (l *issue3791Linker) LinkWork(_ context.Context, w chatstore.WorkLink) error {
	l.got = append(l.got, w)
	return l.err
}

func TestIssue3791_ProposalIsLinkedToItsConversation(t *testing.T) {
	linker := &issue3791Linker{}
	p := chatapps.Proposal{Tenant: "tenant-a", Principal: "dana", Conversation: "general", AgentInstallation: "tenant-a:general:helper", IntentType: "promotion.request"}
	if err := linkProposal(context.Background(), linker, p, chatapps.ProposalReceipt{IntentID: "intent-1", Status: "REQUEST_STATE_DRAFT"}); err != nil {
		t.Fatal(err)
	}
	want := chatstore.WorkLink{TenantID: "tenant-a", ConversationID: "general", Kind: chatstore.WorkLinkIntentProposal, SourceID: "intent-1", ProposedByID: "dana", AgentInstallationID: "tenant-a:general:helper", IntentType: "promotion.request"}
	if len(linker.got) != 1 || linker.got[0] != want {
		t.Fatalf("link %+v", linker.got)
	}
	// The link holds no status: it cannot be stale and cannot show anyone what they may not open.
	if err := linkProposal(context.Background(), linker, p, chatapps.ProposalReceipt{}); err != nil || len(linker.got) != 1 {
		t.Fatalf("a receipt without an intent was linked: %v %+v", err, linker.got)
	}
}

func TestIssue3791_ProposalThatCannotBeLinkedIsNotReportedAsSettled(t *testing.T) {
	linker := &issue3791Linker{err: errors.New("db down")}
	err := linkProposal(context.Background(), linker, chatapps.Proposal{Tenant: "t", Principal: "dana", Conversation: "c"}, chatapps.ProposalReceipt{IntentID: "intent-1"})
	if err == nil {
		t.Fatal("a failed link was swallowed, so the panel would never show the proposal")
	}
}
