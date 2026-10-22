package chatrewrite

import (
	"fmt"
	"strings"
)

// RewordDecision is one recorded answer to the reword questions of
// CHATTONE-001. The record is code so a test can hold the product to it: the
// opt-in, the tone-only boundary, the hard-filter precedence, the writer's
// freedom, the reader notice, the original's readers, the record-keeping, the
// agent boundary, the direct-message choice and the fallback are all asserted
// against these entries.
type RewordDecision struct {
	Question int
	Topic    string
	Decision string
	Rejected string
	Why      string
}

// RewordDecisionsRecordedOn is when the record was written.
const RewordDecisionsRecordedOn = "2026-10-02"

// RewordDecisions returns the decisions in question order.
func RewordDecisions() []RewordDecision {
	return []RewordDecision{
		{1, "rewording is off unless a workspace turns it on",
			"Rewording is off unless a workspace turns it on, and can be turned on per channel: the workspace sets the default, a channel's administrator may choose differently for that channel, and the effective setting is shown where it is set. An unconfigured workspace never calls the model for rewording.",
			"Turning rewording on for every workspace, or offering no per-channel choice.",
			"Rewording changes what people read; imposing it rewrites voice without consent, and one setting cannot fit both a heated support channel and a channel where exact wording matters."},
		{2, "what is reworded: tone only",
			"What is reworded is tone only: insults, sarcasm, shouting and blame are cooled, while every fact, request, refusal, number, date, name, mention, link, code block and commitment survives word for word in meaning, nothing is added (no apology, promise or agreement the writer did not make), and a \"no\" stays a \"no\". A meaning check refuses any rewrite that adds, drops or softens substance.",
			"Letting the model improve clarity, fix facts or make the message nicer without bounds.",
			"An unbounded rewrite can silently change what was decided, promised or refused; employment consequences must never hinge on a model's embellishment."},
		{3, "what is never reworded into acceptability",
			"Threats, slurs, harassment and anything the workspace's hard filters block or flag still follow those outcomes: rewording never runs on them, and the hard-filter outcome (block, mask, flag) is what the writer and readers get. Rewording is not a way to launder reportable conduct, and a reworded message can still be reported with its original attached.",
			"Rewording abusive text into polite text and delivering that instead.",
			"A laundered threat reads as harmless while remaining a threat; the record would hide exactly what an investigation most needs to see."},
		{4, "the writer is never blocked or lectured",
			"The writer is never blocked or lectured by this mode: they send what they wrote, are shown what others will read, and can edit that wording or send as written where policy allows. The service returns a preview only; it has no call that sends on the writer's behalf, and no outcome refuses the send for tone.",
			"Refusing heated sends, or lecturing the writer with a mandatory coaching dialog.",
			"A block on tone stops urgent work over a stylistic judgement, and a lecture punishes the writer for the reader's benefit; showing the preview lets the writer choose."},
		{5, "every reader is told",
			"Every reader is told a message was reworded, in words, on the message: the rendering carries a notice naming it as reworded (and, where the reader may, the way to view it as written). The notice is text, not colour or icon alone, and it travels with quotes, forwards and search hits of the rendering.",
			"Showing the rewording silently, or marking it with a subtle icon only.",
			"Silent substitution lets readers mistake the model's words for the writer's; a notice only some readers perceive is the same failure for the rest."},
		{6, "who may see the original",
			"Who may see the original: the writer always; readers according to the administrator's setting (\"Members may view messages as written\": yes or no, per workspace with a channel override) and then their own preference; moderators under the removal rules, with the same recorded reason a review requires. The workforce is told plainly, where the setting lives, that originals are kept and who can read them.",
			"Showing every reader the original on demand, or hiding it from everyone but the writer.",
			"Universal on-demand originals make the rewording a speed bump a curious reader taps through; hiding them entirely leaves no way to check what was actually said."},
		{7, "the original is the record",
			"The original is the record for retention, holds, export and investigation: retention and holds apply to the stored message, an export says which readers saw which rendering, and an investigator works from the original with the rendering history beside it. Deleting a rendering never deletes the record.",
			"Treating the shown rendering as the record, or exporting one text without saying who saw what.",
			"Two readers may lawfully have seen different texts; an export that cannot say who saw which cannot support a fair investigation."},
		{8, "agents receive the allowed rendering",
			"Agents receive the rendering a reader of that conversation is allowed, so an agent cannot leak an original the channel hides: retrieval, context and answers all pass through the same reader projection as a person's view, and an agent with no read right to the original answers from the reworded text like any other reader.",
			"Giving agents the original for better answers, trusting them not to repeat it.",
			"An answer grounded in hidden text leaks that text to whoever reads the answer; the agent boundary must be the reader boundary, not a second, weaker one."},
		{9, "direct messages",
			"For direct messages rewording is offered to the writer, never imposed on the reader, and that choice is stated: a writer may preview a calmer wording of their own message, but the reader always receives what was sent, because a private conversation is the participants' own and neither asked for an editor.",
			"Rewording direct messages for the reader as channels do.",
			"Imposed rewording in a private conversation puts the product's voice between two people who chose to talk directly."},
		{10, "when the rewrite fails",
			"When the model is unavailable, over budget, or the meaning checks fail, the stated fallback applies: deliver as written with the workspace's ordinary filter applied, and the reader is not shown a failed rewrite. The writer is told plainly that the preview could not be made; the failure is logged with its reason and never retried into a send the writer did not confirm.",
			"Holding the message until the model recovers, or showing a flagged partial rewrite.",
			"A held message stops work for a nicety, and a partial rewrite shown as final puts unvetted model text in the record."},
	}
}

// RenderRewordDecisions is the stable text form of the record.
func RenderRewordDecisions() string {
	var b strings.Builder
	fmt.Fprintf(&b, "CHATTONE-001 decisions recorded %s\n", RewordDecisionsRecordedOn)
	for _, d := range RewordDecisions() {
		fmt.Fprintf(&b, "\n%d. %s\n%s\n", d.Question, d.Topic, d.Decision)
		fmt.Fprintf(&b, "  rejected: %s\n", d.Rejected)
		fmt.Fprintf(&b, "  why: %s\n", d.Why)
	}
	return b.String()
}
