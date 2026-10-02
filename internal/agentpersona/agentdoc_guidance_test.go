package agentpersona

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
)

func TestTodo_AGENTDOC_008(t *testing.T) {
	profile, catalog := personaFixture(t)
	legacy := mustPersonaVersion(t, profile, personaValidator(catalog))
	encoded, err := json.Marshal(legacy.Profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"guidance"`) {
		t.Fatalf("empty guidance changed legacy profile JSON: %s", encoded)
	}

	profile.Guidance = "Follow {{doc:doc-policy}} for every travel answer."
	profile.DocumentReferences = []agentdocref.Reference{{DocumentID: "doc-policy", VersionMode: agentdocref.ModePinned, PinnedVersion: 3, Label: "Travel policy"}}
	guided := mustPersonaVersion(t, profile, personaValidator(catalog))
	if guided.Digest == legacy.Digest || guided.Profile.Guidance != profile.Guidance {
		t.Fatalf("guidance was not sealed into the profile: legacy=%s guided=%+v", legacy.Digest, guided)
	}

	changed := profile
	changed.Guidance = "Use {{doc:doc-policy}} as the primary travel source."
	changedVersion := mustPersonaVersion(t, changed, personaValidator(catalog))
	if changedVersion.Digest == guided.Digest {
		t.Fatal("changed guidance did not change the profile digest")
	}
}

func TestTodo_AGENTDOC_008_Security(t *testing.T) {
	profile, catalog := personaFixture(t)
	profile.Guidance = "Follow {{doc:doc-secret}}."
	profile.DocumentReferences = []agentdocref.Reference{{DocumentID: "doc-policy", VersionMode: agentdocref.ModePinned, PinnedVersion: 3, Label: "Travel policy"}}
	if _, err := personaValidator(catalog).Build(profile); !errors.Is(err, ErrInvalidProfile) || !errors.Is(err, agentdocref.ErrUnknownInstructionDocumentToken) || !strings.Contains(err.Error(), "These instructions mention a document that is not in the list below: doc-secret") {
		t.Fatalf("unknown guidance token error = %v", err)
	}

	profile.Guidance = "line one\u0000line two"
	if _, err := personaValidator(catalog).Build(profile); !errors.Is(err, agentdocref.ErrGuidanceControl) {
		t.Fatalf("guidance control error = %v", err)
	}
}
