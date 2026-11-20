package application

import (
	"strings"
	"unicode/utf8"
)

// AgentUXAmbientWorkflow is one governed workflow an administrator has tagged
// as offerable from Chat, with the phrases that name it. Name is the
// workflow's own catalog name: it is what a card shows, never message text.
type AgentUXAmbientWorkflow struct {
	ID, Name string
	Cues     []string
}

const (
	// AgentUXAmbientWorkflowTag is the catalog keyword an administrator adds to
	// a published workflow to let Chat offer it.
	AgentUXAmbientWorkflowTag = "chat-offerable"
	// AgentUXAmbientWorkflowCuePrefix marks a catalog keyword as a phrase that
	// names the workflow in a message: "chat-cue:bank details".
	AgentUXAmbientWorkflowCuePrefix = "chat-cue:"

	agentUXAmbientWorkflowMinCue  = 3
	agentUXAmbientWorkflowMaxCue  = 80
	agentUXAmbientWorkflowMaxCues = 8
)

// Phrases that show the writer is talking about their own need. A message
// about somebody else, a past event or a question to someone else carries none.
var agentUXAmbientWorkflowNeedMarkers = []string{
	"i need to ", "i have to ", "i want to ", "i must ", "i'd like to ", "i would like to ", "i'm going to ", "i am going to ", "how do i ", "how can i ", "where do i ",
	"ich muss ", "ich möchte ", "ich will ", "ich brauche ", "wie kann ich ", "wie ändere ich ", "wo kann ich ",
	"أحتاج", "أريد", "أود ", "عليّ ", "علي أن", "كيف يمكنني", "كيف أغير",
}

// Phrases that turn a mention into something other than a request: it is over,
// denied, quoted, asked of someone else or about a third person.
var agentUXAmbientWorkflowRefusalMarkers = []string{
	"already ", "i updated ", "i changed ", "i submitted ", "i sent ", "i did ", "i finished ", "was updated", "were updated", "has been ", "have been ",
	"don't need", "do not need", "no need to", "don't want", "do not want", "not going to", "never need", "didn't ", "did not ",
	"did you ", "have you ", "can you ", "could you ", "will you ", "she needs", "he needs", "they need", "she has to", "he has to", "they have to", "she wants", "he wants",
	"sarcasm", "yeah right", "as if", "said \"", "sagte \"", "قال \"",
	"bereits ", "ich habe ", "habe ich ", "brauche nicht", "muss nicht", "will nicht", "hast du ", "kannst du ", "sie muss ", "er muss ", "sie braucht ", "er braucht ",
	"لقد ", "تم ", "لا أحتاج", "لا أريد", "هل ",
}

// AgentUXAmbientClassifyWorkflow picks the one catalog entry a message asks to
// start, or none. It is deliberately conservative: it needs a first-person
// statement of need and a phrase the administrator named for exactly one
// workflow, and it refuses quotation, negation, past tense, questions asked of
// someone else, anything resembling an instruction to the agent, and a message
// that names two different workflows (the person starts from the catalog
// instead). The message is read, never obeyed.
func AgentUXAmbientClassifyWorkflow(body string, entries []AgentUXAmbientWorkflow) (AgentUXAmbientWorkflow, bool) {
	if len(entries) == 0 || len(body) < 8 || len(body) > 1000 || !utf8.ValidString(body) || agentUXAmbientUnsafe(body) {
		return AgentUXAmbientWorkflow{}, false
	}
	text := strings.ToLower(strings.TrimSpace(body)) + " "
	if strings.HasPrefix(text, ">") || strings.HasPrefix(text, "\"") || strings.HasPrefix(text, "“") || !agentUXAmbientContains(text, agentUXAmbientWorkflowNeedMarkers...) || agentUXAmbientContains(text, agentUXAmbientWorkflowRefusalMarkers...) {
		return AgentUXAmbientWorkflow{}, false
	}
	var found AgentUXAmbientWorkflow
	matches := 0
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.Name) == "" {
			continue
		}
		for _, cue := range entry.Cues {
			cue = strings.ToLower(strings.TrimSpace(cue))
			if len(cue) < agentUXAmbientWorkflowMinCue || len(cue) > agentUXAmbientWorkflowMaxCue || !strings.Contains(text, cue) {
				continue
			}
			found = entry
			matches++
			break
		}
	}
	return found, matches == 1
}

// AgentUXAmbientWorkflowFixture is one labelled message of the evaluation set.
// Want is the workflow id the message should be offered, or empty for none.
type AgentUXAmbientWorkflowFixture struct{ Body, Want string }

// AgentUXAmbientWorkflowScore is the result of running the classifier over a
// fixture set.
type AgentUXAmbientWorkflowScore struct {
	Offered, Correct, Wanted int
	Precision, Recall        float64
}

// AgentUXAmbientEvaluateWorkflows scores the classifier against labelled
// messages. An offer counts as correct only when it names the labelled
// workflow. Precision is the share of offers that were wanted; with no offers
// it is 1, so the gate is read together with recall.
func AgentUXAmbientEvaluateWorkflows(entries []AgentUXAmbientWorkflow, fixtures []AgentUXAmbientWorkflowFixture) AgentUXAmbientWorkflowScore {
	var score AgentUXAmbientWorkflowScore
	for _, fixture := range fixtures {
		if fixture.Want != "" {
			score.Wanted++
		}
		entry, ok := AgentUXAmbientClassifyWorkflow(fixture.Body, entries)
		if !ok {
			continue
		}
		score.Offered++
		if entry.ID == fixture.Want {
			score.Correct++
		}
	}
	score.Precision, score.Recall = 1, 1
	if score.Offered > 0 {
		score.Precision = float64(score.Correct) / float64(score.Offered)
	}
	if score.Wanted > 0 {
		score.Recall = float64(score.Correct) / float64(score.Wanted)
	}
	return score
}

// AgentUXAmbientWorkflowFromCatalog turns a catalog entry's keywords into an
// offerable workflow. It returns false unless an administrator tagged the entry
// and named at least one cue; cues that are too short, too long or beyond the
// bound are dropped so a careless tag cannot match everything.
func AgentUXAmbientWorkflowFromCatalog(id, name string, keywords []string) (AgentUXAmbientWorkflow, bool) {
	entry := AgentUXAmbientWorkflow{ID: strings.TrimSpace(id), Name: strings.TrimSpace(name)}
	tagged := false
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		switch {
		case strings.EqualFold(keyword, AgentUXAmbientWorkflowTag):
			tagged = true
		case len(keyword) > len(AgentUXAmbientWorkflowCuePrefix) && strings.EqualFold(keyword[:len(AgentUXAmbientWorkflowCuePrefix)], AgentUXAmbientWorkflowCuePrefix):
			cue := strings.TrimSpace(keyword[len(AgentUXAmbientWorkflowCuePrefix):])
			if len(cue) >= agentUXAmbientWorkflowMinCue && len(cue) <= agentUXAmbientWorkflowMaxCue && len(entry.Cues) < agentUXAmbientWorkflowMaxCues {
				entry.Cues = append(entry.Cues, cue)
			}
		}
	}
	return entry, tagged && entry.ID != "" && entry.Name != "" && len(entry.Cues) > 0
}
