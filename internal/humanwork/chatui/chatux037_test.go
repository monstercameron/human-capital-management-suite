package chatui

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var chatux037Locales = []string{"en-US", "de-DE", "ar"}

// chatux037Bare is any sentence that says something is loading, in the three
// languages the product ships.
var chatux037Bare = regexp.MustCompile(`(?i)loading|please wait|wird geladen|werden geladen|geladen \.\.\.|جار[ٍ]? تحميل|يرجى الانتظار`)

// chatux037Visible is the text a person can see in markup: the accessible
// status elements, scripts and styles are taken out, then the tags.
func chatux037Visible(markup string) string {
	markup = html.UnescapeString(markup)
	markup = regexp.MustCompile(`(?s)<(span|p|div)[^>]*class="[^"]*sr-only[^"]*"[^>]*>.*?</(span|p|div)>`).ReplaceAllString(markup, " ")
	markup = regexp.MustCompile(`(?s)<(style|script)[^>]*>.*?</(style|script)>`).ReplaceAllString(markup, " ")
	markup = regexp.MustCompile(`(?s)<[^>]*>`).ReplaceAllString(markup, " ")
	return strings.Join(strings.Fields(markup), " ")
}

func chatux037Render(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// TestTodo_CHATUX_037 renders the shared loading placeholder, the failed state
// and the empty state of the surfaces that call it, in English, German and
// Arabic, and fails on any loading sentence that is not inside the accessible
// status element.
func TestTodo_CHATUX_037(t *testing.T) {
	shapes := []LoadingShape{LoadingShapeQueue, LoadingShapeMessage, LoadingShapeList, LoadingShapePeople, LoadingShapeChannels, LoadingShapeMenu, LoadingShapeSection, LoadingShapeCard}
	slow := map[string]string{"en-US": "This is taking longer than usual.", "de-DE": "Das dauert länger als gewöhnlich.", "ar": "يستغرق هذا وقتاً أطول من المعتاد."}
	retry := map[string]string{"en-US": "Try again", "de-DE": "Erneut versuchen", "ar": "حاول مجدداً"}
	for _, locale := range chatux037Locales {
		for _, shape := range shapes {
			markup := chatux037Render(t, ChatLoadingFrame(LoadingFrame{Locale: locale, Shape: shape, Status: "Loading things. Please wait.", RetryData: map[string]string{"action": "retry"}}))
			if visible := chatux037Visible(markup); chatux037Bare.MatchString(visible) {
				t.Errorf("%s %s: a loading sentence is printed: %q", locale, shape, visible)
			}
			for _, want := range []string{`class="sr-only" role="status"`, `Loading things. Please wait.`, `aria-busy="true"`, `aria-hidden="true"`, `chat-skeleton`, `data-chat-loading="` + string(shape) + `"`, slow[locale], retry[locale], `data-action="retry"`} {
				if !strings.Contains(markup, want) {
					t.Errorf("%s %s: loading placeholder is missing %q in %s", locale, shape, want, markup)
				}
			}
			if strings.Count(markup, `role="status"`) != 2 {
				t.Errorf("%s %s: want the status once and the slow line once, got %d status elements", locale, shape, strings.Count(markup, `role="status"`))
			}
		}
	}

	// Rows are shaped like the rows that arrive: a queue row has an avatar, two
	// text bars and an action; four of them by default.
	queue := chatux037Render(t, ChatLoadingFrame(LoadingFrame{Locale: "en-US", Shape: LoadingShapeQueue, Status: "Loading moderation items"}))
	if got := strings.Count(queue, `chatux037-row chatux037-queue`); got != 4 {
		t.Errorf("queue placeholder has %d rows, want 4", got)
	}
	for _, want := range []string{"chatux037-avatar", "chatux037-action"} {
		if !strings.Contains(queue, want) {
			t.Errorf("queue placeholder is missing %q", want)
		}
	}
	if strings.Contains(queue, `<button`) {
		t.Error("a placeholder with no retry control draws a button")
	}

	// The eight-second state: the line is in the page, hidden, and revealed by a
	// style rule eight seconds in that survives reduced motion.
	for _, want := range []string{`animation:chatux037-reveal 1ms linear 8s forwards`, `@keyframes chatux037-reveal`, `!important`, `.mention-agent-state .chatux037-loading`} {
		if !strings.Contains(ChatUX037Styles, want) {
			t.Errorf("styles are missing %q", want)
		}
	}
	if !strings.Contains(Stylesheet, "chatux037-reveal") {
		t.Error("the page stylesheet does not carry the loading styles")
	}
	if ChatLoadingSlowAfterSeconds != 8 {
		t.Errorf("slow after %d seconds, want 8", ChatLoadingSlowAfterSeconds)
	}

	// A failed load says what failed with Try again, in the same place.
	for _, locale := range chatux037Locales {
		failed := chatux037Render(t, ChatLoadFailed(locale, "Moderation items could not be loaded.", map[string]string{"chatremove-open": "/x"}, nil))
		for _, want := range []string{`role="alert"`, "Moderation items could not be loaded.", retry[locale], `data-chatremove-open="/x"`} {
			if !strings.Contains(failed, want) {
				t.Errorf("%s: failed state is missing %q in %s", locale, want, failed)
			}
		}
	}

	// Moderation: the frame is there at once, tabs disabled with no counts, four
	// queue rows, nothing printed.
	for _, locale := range chatux037Locales {
		page := chatux037Render(t, ModerationPage(ModerationPageModel{Locale: locale, State: StateLoading}))
		if visible := chatux037Visible(page); chatux037Bare.MatchString(visible) {
			t.Errorf("%s: the Moderation page prints a loading sentence: %q", locale, visible)
		}
		if strings.Count(page, `disabled role="tab"`) != 2 || strings.Contains(page, "chatsave-seg-count") {
			t.Errorf("%s: Open and Resolved must be disabled and carry no counts: %s", locale, page)
		}
		if !strings.Contains(page, "<h2") || !strings.Contains(page, `data-chatremove-close`) || strings.Count(page, `chatux037-row chatux037-queue`) != 4 {
			t.Errorf("%s: the Moderation frame is not complete: %s", locale, page)
		}
		if markup := ModerationLoadingMarkup(locale, "resolved"); !strings.Contains(markup, `aria-selected="true"`) || !strings.Contains(markup, "chatux037-loading") {
			t.Errorf("%s: the client markup lacks the frame", locale)
		}
		failed := ModerationFailedMarkup(locale, "open", "error")
		if !strings.Contains(failed, `role="alert"`) || !strings.Contains(failed, "chatux037-retry") || !strings.Contains(failed, "data-chatremove-open") || !strings.Contains(failed, "chatux037-page-failed") {
			t.Errorf("%s: the failed Moderation page lacks Try again: %s", locale, failed)
		}
	}

	// Empty: a centred icon, the title and one line, as Saved draws it.
	empty := chatux037Render(t, ModerationPage(ModerationPageModel{Locale: "en-US", State: StateReady, Tab: "open"}))
	for _, want := range []string{"chatsave-empty", "<svg", "Nothing to review"} {
		if !strings.Contains(empty, want) {
			t.Errorf("Moderation empty state is missing %q in %s", want, empty)
		}
	}

	// The mention and command menus' rows.
	menu := chatux037Render(t, chatux037MenuLoading(Model{Locale: "en-US"}, "loading", "Loading agents…", "persona-mention-retry"))
	if chatux037Bare.MatchString(chatux037Visible(menu)) || !strings.Contains(menu, `class="mention-agent-state loading"`) || !strings.Contains(menu, "chatux037-menu") || !strings.Contains(menu, `data-action="persona-mention-retry"`) {
		t.Errorf("menu loading row is wrong: %s", menu)
	}

	// Thread, person details and the model-driven surfaces.
	thread := render(t, Model{State: StateReady, SelectedID: "room", ShowThread: true, ThreadParentID: "root", ThreadLoading: true,
		Conversations: []Conversation{{ID: "room", Name: "General", Kind: PublicChannel}},
		Messages:      []Message{{ID: "root", Author: "Ari", Body: "Question"}}})
	if !strings.Contains(thread, `data-chat-loading="message"`) || strings.Contains(thread, "No replies yet") || !strings.Contains(thread, `data-action="reply" data-id="root"`) {
		t.Errorf("thread loading is not the shared placeholder with Try again: %s", thread)
	}
	person := render(t, Model{ShowPerson: true, PersonDetails: &PersonDetails{ID: "worker"}})
	if !strings.Contains(person, `data-chat-loading="section"`) || !strings.Contains(person, `data-action="open-person" data-id="worker"`) {
		t.Errorf("person details loading is not the shared placeholder: %s", person)
	}
}
