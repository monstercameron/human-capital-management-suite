package productui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func docsChatRefsTestView(locale string, refs DocumentChatRefs) View {
	view := ApplyLocale(NewView(PageDocs, "tenant-a", "reader-a", "scope-a"), ResolveProductLocale(locale))
	view.Document = &DocumentDetail{Summary: DocumentSummary{ID: "doc-1", Title: "Guide", VersionID: "v-1"}, Chat: refs}
	return view
}

func docsChatRender(t *testing.T, view View, markdown string) string {
	t.Helper()
	out, err := ui.RenderToString(html.Div(html.Props{}, docsASTMarkdownNodes(view, markdown)...))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func docsChatTestRefs() DocumentChatRefs {
	return DocumentChatRefs{
		Channels: []DocumentChatChannelReference{
			{Key: "name:people-ops", ConversationID: "conv-1", Name: "people-ops", MemberCount: 12, Joined: true},
			{Key: "id:conv-2", ConversationID: "conv-2", Name: "leads", MemberCount: 4, Private: true, Joined: true},
			{Key: "id:conv-secret", Locked: true},
		},
		People: []DocumentChatPersonReference{{Key: "rafael.torres", SubjectID: "hc-050-rafael-torres", DisplayName: "Rafael Torres"}},
		Messages: []DocumentChatMessageReference{
			{Token: "tokOK", Readable: true, ConversationID: "conv-1", ChannelName: "people-ops", PostID: "p-1", AuthorName: "Ana Lopez", Body: "We ship Friday.", CreatedAt: time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)},
			{Token: "tokNO"},
		},
	}
}

func TestDocsChatChipsRenderResolvedReferences(t *testing.T) {
	view := docsChatRefsTestView("en-US", docsChatTestRefs())
	md := "Ask in #people-ops or [#x](channel:conv-2), not [#y](channel:conv-secret). Owner: @rafael.torres.\n\n/workspace/app/chat#share=tokOK\n\n/workspace/app/chat#share=tokNO\n"
	out := docsChatRender(t, view, md)
	for _, want := range []string{
		`href="/workspace/app/chat#channel=conv-1"`, `data-docs-action="open"`, `12 members`, `aria-label="Open channel #people-ops, 12 members"`,
		`href="/workspace/app/chat#channel=conv-2"`, `>leads<`,
		`docs-chat-locked`, `Private channel`, `You are not a member of this channel`,
		`href="/workspace/app/chat#person=hc-050-rafael-torres"`, `>Rafael Torres<`, `aria-label="View Rafael Torres&#39;s details"`,
		`class="docs-chat-quote"`, `role="figure"`, `>Ana Lopez<`, `We ship Friday.`, `>#people-ops<`, `datetime="2026-09-02T09:00:00Z"`, `Jump to message`, `href="/workspace/app/chat#share=tokOK"`,
		`docs-chat-quote-locked`, `This message isn&#39;t available to you`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render omitted %q:\n%s", want, out)
		}
	}
	// The locked channel names nothing, and the unreadable message carries
	// no link to it, no author and no text.
	if strings.Contains(out, "conv-secret") || strings.Contains(out, "tokNO") {
		t.Fatalf("locked references leaked into the page:\n%s", out)
	}
	// No inline styles (CSP).
	if strings.Contains(out, "style=") {
		t.Fatalf("inline style in chips:\n%s", out)
	}
}

// TestDocsMessageCardDeletedNamesChannel proves DOCS-08's one safe
// distinction: a message the server could resolve enough to know it was
// deleted still names the channel and offers to open it, while a message
// that is merely unreadable (no channel info returned) falls back to the
// neutral "isn't available" sentence that never confirms or denies why.
func TestDocsMessageCardDeletedNamesChannel(t *testing.T) {
	refs := DocumentChatRefs{Messages: []DocumentChatMessageReference{
		{Token: "tokDeleted", ConversationID: "conv-9", ChannelName: "release-notes"},
		{Token: "tokGone"},
	}}
	view := docsChatRefsTestView("en-US", refs)
	deleted := docsChatRender(t, view, "/workspace/app/chat#share=tokDeleted\n")
	for _, want := range []string{"This message was deleted.", `href="/workspace/app/chat#channel=conv-9"`, "release-notes"} {
		if !strings.Contains(deleted, want) {
			t.Fatalf("deleted message card omitted %q:\n%s", want, deleted)
		}
	}
	gone := docsChatRender(t, view, "/workspace/app/chat#share=tokGone\n")
	if strings.Contains(gone, "conv-9") || strings.Contains(gone, "was deleted") {
		t.Fatalf("unreadable message leaked channel info it does not have:\n%s", gone)
	}
	if !strings.Contains(gone, "This message isn&#39;t available to you") {
		t.Fatalf("unreadable message did not fall back to the neutral sentence:\n%s", gone)
	}
}

func TestDocsChatChipsLeaveUnresolvedTextAlone(t *testing.T) {
	view := docsChatRefsTestView("en-US", docsChatTestRefs())
	md := "Issue #3, email rafael@example.com, @nobody, #unknown and `#people-ops` in code.\n\n# Heading\n"
	out := docsChatRender(t, view, md)
	if strings.Contains(out, "docs-chat-chip") {
		t.Fatalf("prose became a chip:\n%s", out)
	}
	for _, want := range []string{"Issue #3", "rafael@example.com", "@nobody", "#unknown", "<code>#people-ops</code>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render lost %q:\n%s", want, out)
		}
	}
	// Without references (no chat), a channel: link keeps its label as text
	// and never becomes an unsafe link.
	bare := docsChatRender(t, docsChatRefsTestView("en-US", DocumentChatRefs{}), "[#leads](channel:conv-2) and [@Ana](person:hc-7)")
	if strings.Contains(bare, "channel:conv-2") || strings.Contains(bare, "<a ") || !strings.Contains(bare, "#leads") || !strings.Contains(bare, "@Ana") {
		t.Fatalf("unresolved links = %s", bare)
	}
}

func TestDocsChatChipsLocalized(t *testing.T) {
	for locale, want := range map[string]string{"de-DE": "12 Mitglieder", "ar": "أعضاء"} {
		out := docsChatRender(t, docsChatRefsTestView(locale, docsChatTestRefs()), "#people-ops [#y](channel:conv-secret)")
		if !strings.Contains(out, want) || !strings.Contains(out, docsChatText(locale, "locked_channel")) {
			t.Fatalf("%s chips = %s", locale, out)
		}
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for key := range docsChatCopy["en-US"] {
			if docsChatCopy[locale][key] == "" {
				t.Fatalf("%s lacks %q", locale, key)
			}
		}
	}
}

func TestDocsChatRefsEncodeRoundTrip(t *testing.T) {
	refs := docsChatTestRefs()
	encoded := encodeDocsChatRefs(refs)
	if encoded == "" || encodeDocsChatRefs(DocumentChatRefs{}) != "" {
		t.Fatalf("encoded = %q", encoded)
	}
	back := decodeDocsChatRefs(encoded)
	if len(back.Channels) != 3 || back.People[0].SubjectID != "hc-050-rafael-torres" || !back.Messages[0].CreatedAt.Equal(refs.Messages[0].CreatedAt) {
		t.Fatalf("decoded = %+v", back)
	}
	// The reader body passes them through its string props.
	out, err := ui.RenderToString(docsMarkdownBody(docsMarkdownBodyProps{Locale: "en-US", VersionID: "v-1", Markdown: "#people-ops", ChatRefs: encoded}))
	if err != nil || !strings.Contains(out, `href="/workspace/app/chat#channel=conv-1"`) {
		t.Fatalf("reader body = %s, %v", out, err)
	}
}

func TestDocsEditorKeepsChatReferenceLinks(t *testing.T) {
	for _, target := range []string{"channel:conv-2", "person:hc-050-rafael-torres"} {
		if !docsEditorSafeHref(target) {
			t.Fatalf("editor drops %q", target)
		}
	}
	for _, target := range []string{"channel:", "channel:<x>", "person:a b", "javascript:alert(1)"} {
		if docsEditorSafeHref(target) {
			t.Fatalf("editor keeps %q", target)
		}
	}
}
