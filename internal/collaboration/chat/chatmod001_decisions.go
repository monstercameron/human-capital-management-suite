package chat

import (
	"fmt"
	"strings"
)

// ModerationDecision is one recorded answer to the moderation questions of
// CHATMOD-001. The record is code so a test can hold the product to it: the
// filter outcomes, the masked-form storage rules, the places filters apply,
// the removal flow, the permissions, the appeal path and the privacy limit are
// all asserted against these entries.
type ModerationDecision struct {
	Question int
	Topic    string
	Decision string
	Rejected string
	Why      string
}

// ModerationDecisionsRecordedOn is when the record was written (owner request
// 2026-10-01: "a way to censor language on the chat ... custom filters ...
// admins to delete messages ... change the status of channels and rbac gate
// that").
const ModerationDecisionsRecordedOn = "2026-10-02"

// ModerationDecisions returns the decisions in question order.
func ModerationDecisions() []ModerationDecision {
	return []ModerationDecision{
		{1, "a filter's three outcomes",
			"A filter ends in exactly one of three outcomes. Block: the message is not sent, the author is told why in one sentence naming the term and the channel, and the message stays in the composer so the author can edit and send again; this is the default for a blocked term. Mask: the message is sent with the term masked for readers. Flag: the message is sent and flagged for review in the moderation queue. Nothing is ever silently dropped and nothing is ever silently altered: every outcome is visible to the author and explainable to a reader.",
			"Silently dropping or silently rewriting offending text, and a single block-only outcome.",
			"Silent moderation cannot be appealed, cannot be audited, and reads as tampering with employment records; a single block outcome forces workspaces to choose between over-blocking ordinary conversation and under-blocking real abuse."},
		{2, "what is stored when a term is masked",
			"When a term is masked the original is kept under the same retention and hold rules as any message: retention deletes both forms together and a legal hold preserves both. Readers and search see the masked form only. The original may be seen by the author, who wrote it, and by a reviewer holding the review permission, only with a recorded reason; every such view is logged against the review.",
			"Storing only the masked form, or letting any administrator read the original at will.",
			"Keeping only the mask destroys evidence the author may need for an appeal and the company may need under a hold; unrestricted reads turn a language filter into a surveillance tap over private conversation."},
		{3, "where filters apply",
			"Filters apply to new messages, edits (an edit is filtered exactly like a new message), thread replies, channel names and topics, display names, voice transcripts (CHATVOICE-003), gate answers, and agent output, which passes the same filters as a person's message. Direct messages are filtered only for the tenant's hard rules (the lists the tenant marks non-negotiable), because a private conversation is otherwise the participants' own; that choice is stated here and surfaced where direct-message filtering is configured.",
			"Filtering only new channel messages, or filtering direct messages with every workspace list.",
			"Edits, names and agent output are the same attack surface as new messages and would become the bypass; full filtering of direct messages hands workspace administrators editorial control over conversations they cannot see."},
		{4, "removal by an administrator",
			"Removal by an administrator is a recorded action with a reason, never an erasure. The row stays in the store, a hold still preserves it, readers see \"Removed by an administrator\" where the message was, and the author is told what was removed, why, and how to appeal. The record names who removed it, when, and the reason given.",
			"Deleting the row, or removing without telling the author.",
			"Erasure breaks retention, holds and appeals at once, and in a workplace product a vanished message is indistinguishable from tampering; an author who is not told cannot fix their behaviour or appeal an error."},
		{5, "who may do what",
			"Moderation (CHATMOD-005) has four named permissions: \"Report\", \"Remove messages\", \"Review removed messages\" and \"Manage filters\". By default a workspace administrator holds all four, a channel's manager holds removal and that channel's filters, and every member may report. Channel status (CHATSTATE-001) has two named permissions: \"Change channel status\" and \"Post announcements\". By default a workspace administrator may make any status change including the restricted ones (lock, archive); a channel administrator may make the open changes; \"Change channel status\" granted to a role opens both, \"Post announcements\" lets its holder post where only announcements may be posted. Stored rows may narrow or widen these defaults per workspace or per channel.",
			"A single administrator switch covering removal, review, filters and status.",
			"One switch cannot express the common case (channel managers handle their own channel, only workspace administrators archive) and forces workspaces to hand out more power than the job needs."},
		{6, "appeals",
			"The author of a blocked, masked, flagged or removed message can ask for a review once. The appeal goes to a reviewer who did not take the original action, the reviewer sees the original with the recorded reason being the appeal, and the outcome (upheld or overturned, with a note) is told to the author and kept with the moderation record. One appeal keeps the queue bounded; a second look at the same text by the same team adds delay without adding fairness.",
			"Unlimited appeals, or no appeal path at all.",
			"No appeal leaves errors uncorrectable and authors aggrieved; unlimited appeals let one message consume the queue forever."},
		{7, "what an administrator can never do",
			"An administrator can never read a private conversation they are not a member of by way of a filter hit. A hit in such a conversation shows that a rule matched and which rule matched, never the text; review of the text requires membership or a separate, governed disclosure path outside moderation. This limit is enforced in the queue's reads, not merely stated in policy.",
			"Showing the matching text to any administrator who can see the queue.",
			"Queue visibility must not become a back door into private conversations; employment trust and privacy law both forbid it."},
	}
}

// RenderModerationDecisions is the stable text form used by the golden test.
func RenderModerationDecisions() string {
	var b strings.Builder
	fmt.Fprintf(&b, "CHATMOD-001 decisions recorded %s\n", ModerationDecisionsRecordedOn)
	for _, d := range ModerationDecisions() {
		fmt.Fprintf(&b, "\n%d. %s\n%s\n", d.Question, d.Topic, d.Decision)
		fmt.Fprintf(&b, "  rejected: %s\n", d.Rejected)
		fmt.Fprintf(&b, "  why: %s\n", d.Why)
	}
	return b.String()
}
