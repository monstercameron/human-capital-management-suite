package application

import (
	"context"
	"log/slog"
)

// personaAudienceOmissions counts what one audience read left out, so the read
// writes one line with the counts instead of one line per member of every
// conversation on every page load (AGENTUX-033). A member the trusted directory
// cannot describe, and a conversation whose members cannot be listed, are the
// only things ever left out; an agent identity is never one of them because it
// is classified from the agent identity registry and never looked up as a person.
type personaAudienceOmissions struct {
	conversations, members int
	reasons                map[string]int
}

func (o *personaAudienceOmissions) conversation(reason string) { o.add(reason, true) }

func (o *personaAudienceOmissions) member(reason string) { o.add(reason, false) }

func (o *personaAudienceOmissions) add(reason string, conversation bool) {
	if o == nil {
		return
	}
	if o.reasons == nil {
		o.reasons = map[string]int{}
	}
	o.reasons[reason]++
	if conversation {
		o.conversations++
		return
	}
	o.members++
}

// report writes the single summary line for the read; nothing is written when
// nothing was left out.
func (o *personaAudienceOmissions) report(ctx context.Context) {
	if o == nil || o.conversations == 0 && o.members == 0 {
		return
	}
	slog.WarnContext(ctx, "hcmnext.persona_projection_items_omitted", "projection", "audience",
		"conversations_omitted", o.conversations, "members_omitted", o.members, "reasons", o.reasons)
}
