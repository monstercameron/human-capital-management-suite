package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/personachat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// personaOutcomeSource reads how every asker's questions of one conversation
// went (#3803). Its callers establish that the reader may read the conversation
// first; the rows it returns name no answer, reason or task.
type personaOutcomeSource interface {
	ListConversationOutcomeInvocations(context.Context, string, string) ([]agentinvocationstore.OutcomeInvocation, error)
	ListConversationRefusals(context.Context, string, string) ([]agentinvocationstore.OutcomeRefusal, error)
}

// personaPostVisibility names, among some messages, the ones a person may not
// read because they were addressed to an audience that leaves them out.
type personaPostVisibility interface {
	PostsHiddenFrom(context.Context, chat.Principal, string, []string) (map[string]bool, error)
}

// outcomeReceiptLimit bounds the receipts one read carries to the newest ones.
const outcomeReceiptLimit = 25

// personaOutcomeReceipts is the receipt of each finished agent run in a
// conversation the reader may read, for any asker (#3803).
//
// A receipt is derived from the durable run record and the stored admission
// refusal each time it is read, so the same record always yields the same
// receipt: a replayed or reordered read, or a restart, cannot add a second one,
// and there is no copy to fall out of step with the run. It is keyed by the
// run (the refusal, which has no run, by its question). Only the newest attempt
// of a question speaks for it; one still going has no receipt yet.
//
// It states how the agent's run ended and nothing else. It does not say that a
// business workflow the run began has finished, which this record cannot show,
// and it carries no failure code, answer, task name or step.
//
// The caller has authenticated the reader and the conversation: this runs after
// the surface's own membership check, on every read, so access that ended is
// not served from an earlier receipt. A failed read yields no receipts and
// leaves the rest of the activity as it was.
func (s *PersonaChatSurface) personaOutcomeReceipts(ctx context.Context, p *trust.Principal, room chat.Conversation, store runstate.Store) []personachat.OutcomeReceipt {
	source, ok := s.Invocations.(personaOutcomeSource)
	if !ok || store == nil {
		return nil
	}
	rows, err := source.ListConversationOutcomeInvocations(ctx, room.TenantID, room.ID)
	if err != nil {
		return nil
	}
	refusals, err := source.ListConversationRefusals(ctx, room.TenantID, room.ID)
	if err != nil {
		return nil
	}
	var names map[string]string
	if len(rows) > 0 {
		names = s.personaNames(ctx, p, room)
	}
	out := make([]personachat.OutcomeReceipt, 0)
	// Newest first, as read: the first attempt seen for a question is the one
	// that stands, whether or not it has ended.
	standing := make(map[string]bool, len(rows))
	for _, row := range rows {
		if len(out) >= outcomeReceiptLimit {
			break
		}
		if row.TenantID != room.TenantID || row.ConversationID != room.ID || row.ID == "" || row.PostID == "" || row.InvokerID == "" {
			continue
		}
		key := row.PostID + "\x00" + row.PersonaID
		if standing[key] {
			continue
		}
		standing[key] = true
		runID, err := personaSurfaceRunID(agentinvoke.Invocation{ID: row.ID, TenantID: row.TenantID, PostID: row.PostID})
		if err != nil {
			continue
		}
		run, err := store.Get(ctx, runID)
		if err != nil || run.TenantID != room.TenantID || run.ID != runID || run.AdmissionID != runID {
			continue
		}
		outcome := personaOutcomeWord(run.State)
		if outcome == "" {
			continue
		}
		thread := row.ThreadID
		if thread == "" {
			thread = row.PostID
		}
		out = append(out, personachat.OutcomeReceipt{ReceiptID: runID, PostID: row.PostID, ThreadID: thread, ConversationID: room.ID, InvokerID: row.InvokerID, PersonaID: row.PersonaID, AgentName: names[row.PersonaID], Outcome: outcome})
	}
	asked := make(map[string]bool, len(rows))
	for _, row := range rows {
		asked[row.PostID] = true
	}
	for _, refusal := range refusals {
		if refusal.TenantID != room.TenantID || refusal.ConversationID != room.ID || refusal.PostID == "" || refusal.InvokerID == "" || asked[refusal.PostID] {
			continue
		}
		thread := refusal.ThreadID
		if thread == "" {
			thread = refusal.PostID
		}
		out = append(out, personachat.OutcomeReceipt{ReceiptID: "refused:" + refusal.PostID, PostID: refusal.PostID, ThreadID: thread, ConversationID: room.ID, InvokerID: refusal.InvokerID, Outcome: personachat.OutcomeRefused})
	}
	if len(out) > outcomeReceiptLimit {
		out = out[:outcomeReceiptLimit]
	}
	return s.personaReadableOutcomes(ctx, p, room, out)
}

// personaReadableOutcomes keeps the receipts that sit under a message the reader
// may read. A message addressed to an audience that leaves the reader out is not
// theirs to learn of, and neither is who asked an agent about it. A reader's own
// questions always stand; where readability cannot be established the receipts
// of other people are left out.
func (s *PersonaChatSurface) personaReadableOutcomes(ctx context.Context, p *trust.Principal, room chat.Conversation, receipts []personachat.OutcomeReceipt) []personachat.OutcomeReceipt {
	others := make([]string, 0, len(receipts))
	for _, receipt := range receipts {
		if receipt.InvokerID != p.Subject() {
			others = append(others, receipt.PostID)
		}
	}
	if len(others) == 0 {
		return receipts
	}
	var hidden map[string]bool
	ok := false
	if s.Posts != nil {
		var err error
		hidden, err = s.Posts.PostsHiddenFrom(ctx, chat.Principal{TenantID: room.TenantID, SubjectID: p.Subject()}, room.TenantID, others)
		ok = err == nil
	}
	out := make([]personachat.OutcomeReceipt, 0, len(receipts))
	for _, receipt := range receipts {
		if receipt.InvokerID != p.Subject() && (!ok || hidden[receipt.PostID]) {
			continue
		}
		out = append(out, receipt)
	}
	return out
}

// personaOutcomeWord is the outcome of a run in the state it has reached, and
// empty for a run that has not ended.
func personaOutcomeWord(state runstate.State) string {
	switch state {
	case runstate.StateCompleted:
		return personachat.OutcomeSucceeded
	case runstate.StateFailed, runstate.StateNeedsRepair:
		return personachat.OutcomeFailed
	case runstate.StateCancelled:
		return personachat.OutcomeCancelled
	case runstate.StateExpired:
		return personachat.OutcomeExpired
	}
	return ""
}
