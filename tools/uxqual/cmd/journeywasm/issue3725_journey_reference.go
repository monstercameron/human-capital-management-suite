package main

import (
	"net/url"
	"strings"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// journeyShareReference is the typed reference "Send to chat" attaches to the
// post it writes for a journey (issue 3725): the workflow request the shared
// address names. The server authorizes the sender against the journey and keeps
// only its id, so the card, the conversation's link and a stage change follow
// the data instead of the text of the link. It is nil when the address does not
// name one journey, in which case the post goes out as it always did.
func journeyShareReference(href, tenant string) *chatv1.Reference {
	parsed, err := url.Parse(href)
	if err != nil || parsed.Path != "/workspace/app/journeys" || strings.TrimSpace(tenant) == "" {
		return nil
	}
	values := parsed.Query()
	if len(values["journey"]) != 1 || chatui.JourneyReferenceURL(values.Get("journey")) == "" {
		return nil
	}
	return &chatv1.Reference{Kind: chatv1.ReferenceKind_REFERENCE_KIND_JOURNEY, TenantId: tenant, Id: values.Get("journey")}
}

// chatJourneyPreviewLimit bounds the cards one render reads.
const chatJourneyPreviewLimit = 30

// chatJourneyPreviewIDs are the journeys whose card the visible messages need
// read, newest first and without repeats: those the typed references name and
// those whose same-origin address is in a body.
func chatJourneyPreviewIDs(model chatui.Model) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(body string, typed ...chatui.ChatReference) {
		for _, ref := range chatui.JourneyReferencesFor(body, model.EmbedOrigin, typed) {
			if !seen[ref.IntentID] && len(ids) < chatJourneyPreviewLimit {
				seen[ref.IntentID] = true
				ids = append(ids, ref.IntentID)
			}
		}
	}
	add(model.Draft)
	for i := len(model.ThreadMessages) - 1; i >= 0; i-- {
		add(model.ThreadMessages[i].Body, model.ThreadMessages[i].WorkReferences...)
	}
	for i := len(model.Messages) - 1; i >= 0; i-- {
		add(model.Messages[i].Body, model.Messages[i].WorkReferences...)
	}
	for i := range model.SearchMessages {
		add(model.SearchMessages[i].Message.Body)
	}
	return ids
}
