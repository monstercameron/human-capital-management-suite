package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_059 follows the member-list notice from the failure the
// review saw: the line is in the reader's language, and it goes when the same
// read works on the next try, without taking down a notice another action wrote.
func TestTodo_CHATBUG_059(t *testing.T) {
	const action, clause = "load the member list", "The service did not answer. Try again."
	for locale, want := range map[string]string{
		"en-US": "We couldn't load the member list. The service did not answer. Try again.",
		"de-DE": "Die Mitgliederliste konnte nicht geladen werden. Der Dienst hat nicht geantwortet. Versuchen Sie es erneut.",
		"ar":    "تعذّر تحميل قائمة الأعضاء. لم تستجب الخدمة. حاول مرة أخرى.",
	} {
		if got := chatui.ActionFailureNotice(locale, action, clause); got != want {
			t.Errorf("%s: %q, want %q", locale, got, want)
		}
	}
	// An action with no sentence of its own never falls back to English.
	if got := chatui.ActionFailureNotice("de-DE", "reticulate splines", clause); strings.Contains(got, "We couldn't") || !strings.HasPrefix(got, "Das hat nicht geklappt.") {
		t.Errorf("an unknown action reads %q in German", got)
	}

	// The failure line goes when the same action works again, and only then.
	resetChatNoticeActions()
	state := &chatState{}
	token := state.setNotice(chatui.ActionFailureNotice("de-DE", action, clause), false)
	rememberChatNoticeAction(action, token)
	if state.noticeTokenNow() != token {
		t.Fatal("the notice's number is not the one on screen")
	}
	other := takeChatNoticeAction("load this thread")
	if other != 0 {
		t.Fatal("another action owns the notice")
	}
	if got := takeChatNoticeAction(action); got != token || !state.clearNotice(got) || state.currentNotice() != "" {
		t.Fatal("the member list working again leaves its failure line up")
	}
	// A newer notice written by something else is not cleared by an older recovery.
	first := state.setNotice("first", false)
	rememberChatNoticeAction(action, first)
	state.setNotice("second", false)
	if state.clearNotice(takeChatNoticeAction(action)) || state.currentNotice() != "second" {
		t.Fatal("a recovery took down another action's line")
	}
}

// TestTodo_CHATBUG_059_Notices walks the client's own source: every English
// sentence it hands to the notice funnel (chatActionSucceeded, noteChatAction,
// noteChatActionWithRetry) has a German and an Arabic form, and a load failure
// is worded for the reader's language, whatever the subject.
func TestTodo_CHATBUG_059_Notices(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			name, ok := call.Fun.(*ast.Ident)
			if !ok || name.Name != "chatActionSucceeded" && name.Name != "noteChatAction" && name.Name != "noteChatActionWithRetry" {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(literal.Value)
			if err != nil || text == "" {
				return true
			}
			seen++
			for _, locale := range []string{"de-DE", "ar"} {
				if got := chatui.LocalizeChatNotice(locale, text); got == text {
					t.Errorf("%s: %q reaches a %s page in English", path, text, locale)
				}
			}
			return true
		})
	}
	if seen < 10 {
		t.Fatalf("only %d notice sentences found; the walk is not reading the client", seen)
	}
	// A line that is already localized, or a failure line, is left as written.
	if got := chatui.LocalizeChatNotice("de-DE", "Link kopiert"); got != "Link kopiert" {
		t.Errorf("a localized line was changed to %q", got)
	}
	if got := chatui.LocalizeChatNotice("en-US", "Link copied"); got != "Link copied" {
		t.Errorf("an English reader's line was changed to %q", got)
	}
	// The line that replaces the timeline, and the add-people failure, follow the locale.
	raw := status.Error(codes.Unavailable, "down")
	for _, tc := range []struct{ subject, locale, want string }{
		{"this conversation", "en-US", "We couldn't load this conversation. The service did not answer. Try again."},
		{"this conversation", "de-DE", "Die Unterhaltung konnte nicht geladen werden. Der Dienst hat nicht geantwortet. Versuchen Sie es erneut."},
		{"your conversations", "ar", "تعذّر تحميل محادثاتك. لم تستجب الخدمة. حاول مرة أخرى."},
		{"add everyone you picked", "en-US", "We couldn't add everyone you picked. The service did not answer. Try again."},
		{"add everyone you picked", "de-DE", "Nicht alle ausgewählten Personen konnten hinzugefügt werden. Der Dienst hat nicht geantwortet. Versuchen Sie es erneut."},
	} {
		chatBrowser.mutate(func(m *chatui.Model) { m.Locale = tc.locale })
		if got := chatLoadFailureMessage(tc.subject, raw); got != tc.want {
			t.Errorf("%s %s: %q, want %q", tc.locale, tc.subject, got, tc.want)
		}
	}
	chatBrowser.mutate(func(m *chatui.Model) { m.Locale = "" })
}
