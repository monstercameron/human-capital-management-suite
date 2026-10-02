package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// VoiceDecision is one fixed decision of CHATVOICE-001. The record is code, not
// prose in a document, so a test can pin it and a change to it has to be seen.
type VoiceDecision struct {
	Number    int
	Statement string
	// Revised names the owner decision that replaced part of the original
	// wording, empty when the original stands.
	Revised string
}

const voiceOwnerDecision = "owner decision 2026-10-02: language, speech to text and writing style may use OpenAI through SchemaFlux"

// VoiceDecisions returns the record. Decisions 2 and 8 carry the owner's
// decision of 2026-10-02 in place of the original in-deployment wording.
func VoiceDecisions() []VoiceDecision {
	return []VoiceDecision{
		{1, "A voice message is a self-recording of at most two minutes, never a recording of a call or of other people.", ""},
		{2, "Transcription calls OpenAI through the agent model gateway: the same key source, base-URL rules, pinned model, price and budget as the typed model calls. Audio is sent only after the workspace and the channel both allow an outside service; a channel set to \"Never use an outside service\" never has its audio or transcript sent. The model is configurable and never replaced by another one silently.", voiceOwnerDecision},
		{3, "The audio is the record. The transcript is marked \"Transcribed automatically\" and may be corrected by its author, with the correction recorded.", ""},
		{4, "Audio and transcript carry the message's audience, retention, legal hold, export and data-loss rules, and deleting the message deletes both.", ""},
		{5, "No speaker identification, voiceprint or emotion inference is derived from audio.", ""},
		{6, "Agents and every automated consumer read the transcript only, as untrusted data, and never the audio.", ""},
		{7, "Voice is on in direct and group conversations and off in channels until a channel administrator enables it, with a workspace switch above both and a personal switch.", ""},
		{8, "Listen on a typed message or an agent answer sends that message's text, read as the reader, to OpenAI text to speech through the same gateway and the same outside-service rules; the speech is played and stored nowhere.", voiceOwnerDecision},
	}
}

// VoiceDecisionRecord renders the record as the exact text that is pinned.
func VoiceDecisionRecord() string {
	var b strings.Builder
	for _, d := range VoiceDecisions() {
		fmt.Fprintf(&b, "%d. %s", d.Number, d.Statement)
		if d.Revised != "" {
			fmt.Fprintf(&b, " (%s)", d.Revised)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// VoiceDecisionDigest is the digest of the rendered record.
func VoiceDecisionDigest() string {
	sum := sha256.Sum256([]byte(VoiceDecisionRecord()))
	return hex.EncodeToString(sum[:])
}
