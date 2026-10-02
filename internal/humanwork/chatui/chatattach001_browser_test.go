package chatui_test

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func chatattach001Model(locale string) chatui.Model {
	ctx := productui.ResolveProductLocale(locale)
	return chatui.Model{State: chatui.StateReady, SelectedID: "room", CurrentTenantID: "tenant", CurrentUser: "alice", Locale: ctx.Resolved, Direction: string(ctx.Direction), Text: func(key string) string { return ctx.Text(key) }, Draft: "Here is the file",
		Conversations: []chatui.Conversation{{ID: "room", Name: "Team", Kind: chatui.PrivateChannel, Joined: true}},
		Messages:      []chatui.Message{{ID: "post", Author: "Alice", AuthorID: "alice", Body: "Read this", Attachments: []chatui.Attachment{{ID: "pdf", Name: "policy.pdf", ContentType: "application/pdf", Bytes: 8192}}}},
		Callbacks:     chatui.Callbacks{SendMessage: func(string, string) {}, DownloadAttachment: func(string, string) {}},
		Chatattach001: &chatui.Chatattach001Composer{Choose: func() {}, Send: func(string, []chatui.ChatReference) {}, Files: []chatui.Chatattach001Draft{{Key: "draft", Attachment: chatui.Attachment{Name: "photo.png", ContentType: "image/png", Bytes: 2048, URL: "blob:photo"}, Uploading: true, Progress: 42}}},
	}
}

func chatattach001Markup(t *testing.T, m chatui.Model) (string, string) {
	t.Helper()
	page, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(page, `class="chat-composer"`)
	if start < 0 {
		t.Fatal("composer missing")
	}
	start = strings.LastIndex(page[:start], "<form")
	end := strings.Index(page[start:], "</form>")
	if end < 0 {
		t.Fatal("composer close missing")
	}
	return html.UnescapeString(page[start : start+end]), html.UnescapeString(page)
}

func TestTodo_CHATATTACH_001_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := chatattach001Model(locale)
			composer, page := chatattach001Markup(t, m)
			if strings.Contains(composer, "⟦") || strings.Contains(composer, "⟧") {
				t.Fatalf("real catalog prints copy keys: %s", composer)
			}
			for _, want := range []string{chatui.Chatattach001Text(locale, "attach"), chatui.Chatattach001Text(locale, "note"), chatui.Chatattach001Text(locale, "remove"), chatui.Chatattach001Text(locale, "uploading") + " 42%", "photo.png", "2 KB", `src="blob:photo"`, `type="file"`, `multiple`, `accept=`} {
				if !strings.Contains(composer, want) {
					t.Fatalf("composer misses %q", want)
				}
			}
			first := strings.Index(composer, `data-extra="attachment"`)
			next := strings.Index(composer, `data-extra="location"`)
			if first < 0 || next >= 0 && first > next {
				t.Fatal("attachment is not first menu item")
			}
			if !strings.Contains(page, "chatattach001-file") || !strings.Contains(page, "policy.pdf") || !strings.Contains(page, "8 KB") || !strings.Contains(page, chatui.Chatattach001Text(locale, "download")) {
				t.Fatal("non-image file row missing")
			}
			fileStart := strings.Index(page, `data-chatattach001-file=`)
			fileEnd := strings.Index(page[fileStart:], "</div>")
			if fileEnd < 0 || strings.ContainsAny(page[fileStart:fileStart+fileEnd], "⟦⟧") {
				t.Fatal("file row prints a catalog key")
			}
			m.Messages[0].Attachments[0].PreviewUnavailable = true
			_, unavailable := chatattach001Markup(t, m)
			if !strings.Contains(unavailable, chatui.Chatattach001Text(locale, "unavailable")) {
				t.Fatal("unavailable file state missing")
			}
			for _, reason := range []string{"size", "empty", "type", "limit", "quota", "failed"} {
				m.Chatattach001.Error = reason
				composer, _ = chatattach001Markup(t, m)
				if !strings.Contains(composer, chatui.Chatattach001Text(locale, reason)) || strings.Contains(composer, "⟦") {
					t.Fatalf("%s refusal missing or raw", reason)
				}
			}
		})
	}
}

func TestTodo_CHATATTACH_001_Accessibility(t *testing.T) {
	m := chatattach001Model("ar")
	composer, _ := chatattach001Markup(t, m)
	for _, want := range []string{`aria-label="` + chatui.Chatattach001Text("ar", "remove") + `: photo.png"`, `aria-live="polite"`, `role="status"`, `dir="rtl"`, `dir="auto"`, `data-send-capable="false"`} {
		if !strings.Contains(composer, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if m.Chatattach001.Ready() {
		t.Fatal("upload enables send")
	}
	m.Chatattach001.Files[0].Uploading = false
	if !m.Chatattach001.Ready() {
		t.Fatal("finished upload blocks send")
	}
	m.Chatattach001.Sending = true
	if m.Chatattach001.Ready() {
		t.Fatal("send in flight allows duplicate")
	}
	var empty *chatui.Chatattach001Composer
	if !empty.Ready() {
		t.Fatal("ordinary composer blocked")
	}
	if !strings.Contains(chatui.ScopedStylesheet(), ".chatattach001-chip") || !strings.Contains(chatui.Chatattach001Styles, "max-width:800px") || strings.Contains(chatui.Chatattach001Styles, "#") {
		t.Fatal("responsive token styles missing")
	}
}
