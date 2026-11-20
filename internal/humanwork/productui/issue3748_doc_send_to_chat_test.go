package productui

import (
	"strings"
	"testing"
)

func issue3748View(locale string) View {
	view := NewView(PageDocs, "tenant-a", "reader-a", "scope-a")
	view.Locale = LocaleContext{Resolved: locale}
	view.Document = &DocumentDetail{Summary: DocumentSummary{ID: "doc-42", Title: "Handbook"}, Markdown: "Body"}
	view.DocumentOrigin = "http://127.0.0.1:8899"
	return view
}

// A reader of a document is offered Send to chat when chat is available; the
// button carries the canonical in-app address, the title and the dialog words.
func TestIssue3748_DocumentOffersSendToChat(t *testing.T) {
	view := issue3748View("en-US")
	view.CanSendDocumentToChat = true
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-projectui-action="share-chat"`,
		`data-href="/workspace/app/docs?document=doc-42"`,
		`data-title="Handbook"`,
		`data-text-send-title="Send to chat"`,
		`data-text-sent="Sent to {name}"`,
		`data-text-open-conversation="Open conversation"`,
		"Send to chat…",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("document page omitted %q", want)
		}
	}
}

// Without chat (or when the host did not enable it) there is no dead button.
func TestIssue3748_NoSendToChatWithoutChat(t *testing.T) {
	doc, err := Render(issue3748View("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `data-projectui-action="share-chat"`) {
		t.Fatal("Send to chat was offered with chat unavailable")
	}
}

// A document the viewer cannot read renders the unavailable state, never the
// action (which would carry its title).
func TestIssue3748_UnreadableDocumentHasNoSendToChat(t *testing.T) {
	view := issue3748View("en-US")
	view.CanSendDocumentToChat = true
	view.Document = nil
	view.DocumentUnavailable = true
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `data-projectui-action="share-chat"`) {
		t.Fatal("Send to chat offered for an unavailable document")
	}
}

func TestIssue3748_SendToChatTranslated(t *testing.T) {
	for locale, want := range map[string][]string{
		"de-DE": {"An Chat senden …", `data-text-send-title="An Chat senden"`},
		"ar":    {"إرسال إلى الدردشة…", `data-text-send-title="إرسال إلى الدردشة"`},
	} {
		view := issue3748View(locale)
		view.CanSendDocumentToChat = true
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range want {
			if !strings.Contains(doc, w) {
				t.Fatalf("%s: missing %q", locale, w)
			}
		}
	}
}
