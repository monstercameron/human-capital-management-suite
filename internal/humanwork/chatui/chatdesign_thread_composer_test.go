//go:build !(js && wasm)

package chatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
)

// #3774: the thread reply composer has the main composer's one tool row, and
// "Also send to #channel" is part of that row, in every locale and direction.
func TestChatDesign_3774_ThreadComposerHasOneToolRow(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chat4Fixture(locale, "sent", false)
		markup := chatDesignRender(t, html.Div(html.Props{}, chatux014ThreadTools(m, handlers{}, false)...))
		if count := strings.Count(markup, `class="composer-toolbar thread-toolbar"`); count != 1 {
			t.Fatalf("%s: %d thread tool rows, want one: %s", locale, count, markup)
		}
		row := markup[strings.Index(markup, `class="composer-toolbar thread-toolbar"`):]
		tools, send, also := strings.Index(row, `class="composer-tools"`), strings.Index(row, `class="send-button"`), strings.Index(row, `class="thread-also"`)
		if tools < 0 || send < tools || also < send {
			t.Fatalf("%s: the row is not tools, Reply, Also send (%d, %d, %d)", locale, tools, send, also)
		}
		name := strings.ReplaceAll(chatux014Text(locale, "also"), "{name}", "#general")
		if name == "" || !strings.Contains(row, ">"+name+"<") {
			t.Errorf("%s: Also send does not name the channel (%q)", locale, name)
		}
		if strings.Count(row, `class="composer-tools"`) != 1 {
			t.Errorf("%s: the tools are split over more than one group", locale)
		}
	}

	for _, want := range []string{
		`.chat-workspace .thread-composer .thread-toolbar>.composer-tools{flex:0 0 auto;flex-wrap:nowrap!important}`,
		`.chat-workspace .thread-composer .thread-toolbar>.send-button{flex:none;align-self:center;margin-inline-start:auto}`,
		`.chat-workspace .thread-composer .thread-toolbar>.thread-also{order:3;flex:1 1 100%;`,
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("Chat stylesheet is missing %q", want)
		}
	}
	// The one-row rule is declared after, and is more specific than, the rule
	// that wrapped the tools, so it is the one that applies.
	wrap := strings.LastIndex(Stylesheet, `.chat-workspace .thread-composer .composer-tools{flex-wrap:wrap!important}`)
	row := strings.Index(Stylesheet, `.chat-workspace .thread-composer .thread-toolbar>.composer-tools{`)
	if wrap < 0 || row < wrap {
		t.Fatalf("the wrap rule (%d) is missing or declared after the one-row rule (%d)", wrap, row)
	}
	// The thread row takes the main composer's hairline, padding and height.
	main := regexp.MustCompile(`\.composer-toolbar\{display:flex;min-height:40px;padding:4px 2px 0;border-top:1px solid var\(--line\)\}`)
	if !main.MatchString(Stylesheet) {
		t.Fatal("the main composer's tool row changed; the thread row no longer mirrors it")
	}
	thread := regexp.MustCompile(`\.chat-workspace \.thread-composer \.composer-toolbar\.thread-toolbar\{([^}]*)\}`).FindStringSubmatch(Stylesheet)
	if thread == nil {
		t.Fatal("the thread tool row has no rule")
	}
	for _, want := range []string{"min-block-size:40px", "padding:4px 2px 0", "border-block-start:1px solid var(--line)", "align-items:center"} {
		if !strings.Contains(";"+thread[1]+";", ";"+want+";") {
			t.Errorf("the thread tool row does not declare %s", want)
		}
	}
	// RTL: nothing in the thread row is pinned to a physical side.
	if physical := regexp.MustCompile(`(margin|padding|border)-(left|right)|[;{](left|right):|text-align:(left|right)`).FindString(chatDesignThreadComposerStyles); physical != "" {
		t.Errorf("the thread tool row uses a physical side: %s", physical)
	}
}
