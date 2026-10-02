package application

import (
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

// AGENTUX-076: what a run does with a search that finds nothing, and with an
// answer that is not from the documents.

// personaNoResultsInstruction is the one instruction added after a search that
// returned no passages. It carries no data of the person's.
const personaNoResultsInstruction = "The search you ran found no matching passages. That is a normal result, not an error. Write a normal answer: say you found nothing on that subject, name the one to three documents from your readable list that look closest, and ask one clarifying question or suggest how to rephrase. Do not make up what a document says."

// personaNotFromDocumentsMarker is the line a general-purpose agent ends an
// answer with when no document backs it. The server strips it before the answer
// is sealed and turns it into the typed flag the card shows.
const personaNotFromDocumentsMarker = "[[not-from-documents]]"

// personaServerInstructions is the closed set of server-authored developer
// instructions a run may add after its model work was built. The outbound
// verifier accepts exactly these texts, as the profile's own words.
var personaServerInstructions = []string{personaRegenerationInstruction, personaNoResultsInstruction}

// personaServerInstructionField reports whether an outbound field is one of the
// server's own instructions, in the developer role, under the profile's class.
func personaServerInstructionField(source agentegress.SourceClassificationRequest, profileClass trustdlp.DataClass) bool {
	if source.MessageRole != "" && source.MessageRole != agentmodel.RoleDeveloper || source.DataClass != profileClass {
		return false
	}
	for _, instruction := range personaServerInstructions {
		if source.ValueDigest == personaRunBytesDigest([]byte(instruction)) {
			return true
		}
	}
	return false
}

// appendPersonaServerInstruction adds one of the server's instructions as a
// developer message, classified as the profile's own text is. A request that
// carries no profile field is refused.
func appendPersonaServerInstruction(request *AgentModelExecutorRequest, instruction, tag string) error {
	if request == nil || strings.TrimSpace(instruction) == "" {
		return ErrPersonaRunOutputRejected
	}
	known := false
	for _, allowed := range personaServerInstructions {
		known = known || allowed == instruction
	}
	var profile agentegress.Field
	found := false
	for _, field := range request.Outbound.Fields {
		if field.Name == "model.message.1" {
			profile, found = field, true
			break
		}
	}
	if !known || !found || request.FieldSources == nil {
		return ErrPersonaRunOutputRejected
	}
	model := &request.Model
	name := fmt.Sprintf("model.message.%d", len(model.Messages))
	model.Messages = append(model.Messages, agentmodel.ModelMessage{Role: agentmodel.RoleDeveloper, Content: instruction})
	request.Outbound.Fields = append(request.Outbound.Fields, agentegress.Field{
		Name: name, Value: instruction, Class: profile.Class,
		Taint:      append([]string(nil), profile.Taint...),
		Provenance: append(append([]string(nil), profile.Provenance...), "server-instruction:"+tag),
	})
	request.Outbound.DeclaredFields = append(request.Outbound.DeclaredFields, name)
	request.FieldSources[name] = "persona-profile"
	if err := agentmodel.ValidateModelRequest(*model); err != nil {
		return ErrPersonaRunOutputRejected
	}
	return validateExecutorRequest(*request)
}

// personaStripNotFromDocuments removes the marker from a reply and says whether
// it was there.
func personaStripNotFromDocuments(text string) (string, bool) {
	if !strings.Contains(text, personaNotFromDocumentsMarker) {
		return text, false
	}
	cleaned := strings.ReplaceAll(text, personaNotFromDocumentsMarker, "")
	return strings.TrimSpace(cleaned), true
}

// personaDeliveryExtra is what the run adds to a delivery beyond the sealed
// answer: the listed documents the answer names, and what decides whether the
// answer is from no document at all.
type personaDeliveryExtra struct {
	Named    []agentdocref.ResolvedDocument
	General  bool
	SaidSo   bool
	Searched bool
}

// personaNotFromDocuments decides the typed flag. A general-purpose agent's
// answer is not from the documents when it said so itself, or when it searched
// and the sealed answer cites no document.
func personaNotFromDocuments(general, saidSo, searched bool, cited []agentdocref.ResolvedDocument) bool {
	return general && (saidSo || searched && len(cited) == 0)
}
