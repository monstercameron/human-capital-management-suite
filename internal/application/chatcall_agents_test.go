package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHATCALL_007 pins what agents may do with a call: the permitted
// features with their consent, the ruled-out list, and the speech-model
// fitness finding. Permissibility is decided against CHATCALL-005,
// CHATVOICE-003 and AGENTUX-066, all three cited from the code that
// implements them.
func TestTodo_CHATCALL_007(t *testing.T) {
	features := CallAgentFeatures()
	if len(features) != 7 {
		t.Fatalf("features = %d, want 7", len(features))
	}
	byID := map[string]CallAgentFeature{}
	for _, f := range features {
		byID[f.ID] = f
		if strings.TrimSpace(f.Name) == "" || strings.TrimSpace(f.Consent) == "" ||
			strings.TrimSpace(f.OnDecline) == "" || strings.TrimSpace(f.Mechanism) == "" {
			t.Fatalf("feature %s leaves its answer open: %+v", f.ID, f)
		}
	}
	for id, permitted := range map[string]bool{
		"LIVE_CAPTIONS": false, "AFTER_CALL_SUMMARY": true, "PRIVATE_NOTES": true,
		"SILENT_ATTENDANCE": false, "SCORING_ASSESSMENT": false,
		"SENTIMENT_EMOTION": false, "EMPLOYMENT_DECISIONS": false,
	} {
		f, ok := byID[id]
		if !ok {
			t.Fatalf("feature %s is missing", id)
		}
		if f.Permitted != permitted {
			t.Fatalf("feature %s permitted = %v, want %v", id, f.Permitted, permitted)
		}
	}
	// The record cites the shared audience rule: prove the rule it cites.
	uncertain := AgentUXAmbientAudienceFor(
		AgentUXAmbientMessage{Tenant: "tenant-a", Conversation: "general", ID: "m1", Author: "ana", Body: "hmm, maybe later"},
		AgentUXAmbientProposal{Kind: "TASK", Title: "maybe later"},
		[]AgentUXAmbientMember{{ID: "ana", Name: "Ana"}})
	if uncertain.Scope != "PRIVATE" {
		t.Fatalf("uncertain audience = %q, want PRIVATE", uncertain.Scope)
	}
	// The record cites the gateway speech path: prove it is the product's.
	var _ chat.Transcriber = OpenAIVoiceTranscriber{}
	// The record cites the no-inference voice rule: prove it still stands.
	if !strings.Contains(chat.VoiceDecisions()[4].Statement, "emotion") {
		t.Fatal("CHATVOICE-001 decision 5 no longer forbids emotion inference")
	}
	fitness := CallSpeechFitness()
	if fitness.Available {
		t.Fatal("the record claims an in-deployment speech model that does not exist")
	}
	if fitness.Status != "NOT_RUN" {
		t.Fatalf("fitness status = %q, want NOT_RUN", fitness.Status)
	}
	rendered := RenderCallAgents()
	for _, phrase := range []string{
		"Task Catcher", "Reminder",
		"Silent attendance", "emotion", "employment decision",
		"CHATCALL-005", "CHATVOICE-003", "AGENTUX-066",
		"NOT_RUN",
	} {
		if !strings.Contains(rendered, phrase) {
			t.Fatalf("record does not pin %q", phrase)
		}
	}
	features[0].Name = "changed"
	if CallAgentFeatures()[0].Name == "changed" {
		t.Fatal("the record is mutable by a caller")
	}
}

// TestTodo_CHATCALL_007_Security holds every permitted feature to its
// consent: each names the consent it needs and what happens when one
// participant declines.
func TestTodo_CHATCALL_007_Security(t *testing.T) {
	for _, f := range CallAgentFeatures() {
		if !f.Permitted {
			if f.ID == "LIVE_CAPTIONS" {
				if !strings.Contains(f.Consent, "blocked") {
					t.Fatalf("feature %s is blocked but does not say so: %q", f.ID, f.Consent)
				}
				continue
			}
			if !strings.Contains(f.Consent, "ruled out") {
				t.Fatalf("feature %s is refused but does not say it is ruled out: %q", f.ID, f.Consent)
			}
			continue
		}
		if !strings.Contains(f.Consent, "every participant") {
			t.Fatalf("permitted feature %s does not require every participant's consent: %q", f.ID, f.Consent)
		}
		if !strings.Contains(f.OnDecline, "decline") {
			t.Fatalf("permitted feature %s does not say what a decline does: %q", f.ID, f.OnDecline)
		}
	}
	ruledOut := 0
	for _, f := range CallAgentFeatures() {
		if !f.Permitted && strings.Contains(f.Consent, "ruled out") {
			ruledOut++
		}
	}
	if ruledOut != 4 {
		t.Fatalf("ruled-out features = %d, want 4", ruledOut)
	}
}

func TestTodo_CHATCALL_007_Golden(t *testing.T) {
	const digest = "3083e219c26299053b97afe46da8669e146fad2858d6e078ae9f1c8208da5c4e"
	if got := strings.Count(RenderCallAgents(), "\n"); got < 10 {
		t.Fatalf("record has %d lines", got)
	}
	if CallAgentsDigest() != digest {
		t.Fatalf("the record changed; digest is now %s", CallAgentsDigest())
	}
}
