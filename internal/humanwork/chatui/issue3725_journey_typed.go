package chatui

import (
	"net/url"
	"sort"
	"strings"
)

// ChatReferenceJourney is the kind of a typed reference that "Send to chat"
// attaches to the post that shares a workflow request (issue 3725). Its ID is
// the journey's intent id. The server authorized the sender when the post was
// written and authorizes each reader when it is read, so a reference the reader
// may not open arrives with no ID at all. It carries no stage, requester or pay
// figure: the card reads those live as the viewer.
const ChatReferenceJourney = "JOURNEY"

// JourneyChatReference is the journey id a typed reference names.
func JourneyChatReference(ref ChatReference) (intentID string, ok bool) {
	if ref.Kind != ChatReferenceJourney || !journeyIDPattern.MatchString(ref.ID) {
		return "", false
	}
	return ref.ID, true
}

// JourneyReferencesFor is JourneyReferences for a message that may carry typed
// references. The typed reference, not the text of the body, says which journey
// the message shares:
//   - an address in the body that names the same journey is recognized on any
//     origin, since the typed reference vouches for it;
//   - a typed reference whose address is not in the body (the author edited it
//     away) is returned with Start and End of -1: a card, no inline text.
//
// A hand-pasted address with no typed reference still renders as before; it is
// only a convenience, and the journey service still decides what the viewer
// sees.
func JourneyReferencesFor(body, origin string, typed []ChatReference) []JourneyReference {
	refs := JourneyReferences(body, origin)
	want := map[string]bool{}
	var order []string
	for _, ref := range typed {
		if id, ok := JourneyChatReference(ref); ok && !want[id] {
			want[id] = true
			order = append(order, id)
		}
	}
	if len(want) == 0 {
		return refs
	}
	if len(body) > 32*1024 {
		body = body[:32*1024]
	}
	overlaps := func(start, end int) bool {
		for _, ref := range refs {
			if start < ref.End && ref.Start < end {
				return true
			}
		}
		return false
	}
	for _, span := range journeyURLPattern.FindAllStringIndex(body, 16) {
		candidate := strings.TrimRight(body[span[0]:span[1]], ".,!?;:)]}>")
		end := span[0] + len(candidate)
		if overlaps(span[0], end) {
			continue
		}
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.User != nil || parsed.Fragment != "" || parsed.Path != "/workspace/app/journeys" {
			continue
		}
		values, err := url.ParseQuery(parsed.RawQuery)
		if err != nil || len(values["journey"]) != 1 || values.Encode() != parsed.RawQuery {
			continue
		}
		known := true
		for key, entries := range values {
			if !journeyReferenceKeys[key] || len(entries) != 1 {
				known = false
			}
		}
		if id := values.Get("journey"); known && want[id] {
			refs = append(refs, JourneyReference{IntentID: id, Start: span[0], End: end})
		}
	}
	sortJourneyReferences(refs)
	placed := map[string]bool{}
	for _, ref := range refs {
		placed[ref.IntentID] = true
	}
	for _, id := range order {
		if !placed[id] {
			refs = append(refs, JourneyReference{IntentID: id, Start: -1, End: -1})
		}
	}
	return refs
}

func sortJourneyReferences(refs []JourneyReference) {
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].Start < refs[j].Start })
}
