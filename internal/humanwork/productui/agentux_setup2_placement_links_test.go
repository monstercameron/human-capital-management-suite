package productui

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

var agentUXSetup2HrefPattern = regexp.MustCompile(`href="([^"]*)"`)

// Chat opens a conversation from the address only when the whole fragment is
// the canonical "#channel=<id>" form; anything appended makes it ignore the
// link. Every Chat link on a placement row must therefore be one Chat accepts,
// and the document links must lead to a page that exists.
func TestTodo_AGENTUX_034_PlacementLinksOpen(t *testing.T) {
	three, zero := 3, 0
	persona := agentUXSetup2Persona()
	persona.Installations = []PersonaAdminInstallation{
		{ConversationID: "general", Conversation: "general", Kind: "CHANNEL", Version: "4", OfficialDocumentCount: &three, OfficialDocumentTitles: []string{"Benefits guide", "Leave policy", "Paid time off policy"}},
		{ConversationID: "direct-walt", Conversation: "Walt Brennan", Kind: "DIRECT_MESSAGE", Version: "4", OfficialDocumentCount: &zero},
		{ConversationID: "restricted", Conversation: "restricted", Kind: "PRIVATE_CHANNEL"},
	}
	markup := personaAdminRender(t, personaAdminPlacements(ResolveProductLocale("en-US"), persona, agentUXSetup2Snapshot(persona)))
	chatLinks, documentLinks := 0, 0
	for _, match := range agentUXSetup2HrefPattern.FindAllStringSubmatch(markup, -1) {
		href := html.UnescapeString(match[1])
		switch {
		case strings.HasPrefix(href, Path(PageChat)):
			chatLinks++
			// The same acceptance rule the Chat page applies to its address.
			refs := chatui.ChannelReferences(href, "https://hcm.example")
			if len(refs) != 1 || chatui.ChannelReferenceURL(refs[0].ID) != href {
				t.Errorf("Chat would not open the conversation from %q", href)
			}
		case href == Path(PageDocs):
			documentLinks++
		default:
			t.Errorf("placement row links somewhere unexpected: %q", href)
		}
	}
	if chatLinks != 3 {
		t.Errorf("conversation links = %d, want one per placement: %s", chatLinks, markup)
	}
	// Per placement: the count link and three title links, "Place a document",
	// and "Open this conversation's Documents" when the count is not known.
	if documentLinks != 6 {
		t.Errorf("document links = %d, want 6: %s", documentLinks, markup)
	}
	for _, want := range []string{
		`<a href="/workspace/app/docs">Place a document</a>`,
		`<a href="/workspace/app/docs">3 documents in this conversation</a>`,
		`<a href="/workspace/app/docs">Open this conversation&#39;s Documents</a>`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("placement document link missing %q: %s", want, markup)
		}
	}
}
