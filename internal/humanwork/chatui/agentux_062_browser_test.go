package chatui_test

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// agentux062Model is a channel with a thread open on its first message, built
// the way the page builds it, with the product's own catalog.
func agentux062Model(locale string) chatui.Model {
	ctx := productui.ResolveProductLocale(locale)
	at := time.Now().Add(-time.Hour)
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: "general", CurrentUser: "me", CurrentTenantID: "t",
		Text:          func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{{ID: "general", Name: "general", Kind: chatui.PublicChannel, Joined: true, MemberCount: 12}},
		Messages: []chatui.Message{
			{ID: "root", AuthorID: "walt", Author: "Walt Brennan", Body: "Who has the numbers?", Replies: 1, SentAt: at, Revision: 1},
			{ID: "mine", AuthorID: "me", Author: "Me", Body: "I do", SentAt: at.Add(time.Minute), Revision: 1},
		},
		ThreadMessages: []chatui.Message{{ID: "r1", AuthorID: "me", Author: "Me", Body: "Reply", SentAt: at.Add(2 * time.Minute), Revision: 1}},
	}
	m.Callbacks.OpenPicker = func(string) {}
	m.Callbacks.OpenMenu = func(string) {}
	m.Callbacks.CopyLink = func(string) {}
	m.Callbacks.Pin = func(string) {}
	m.Callbacks.BeginEdit = func(string) {}
	m.Callbacks.DeleteMessage = func(string, uint64) {}
	m.Callbacks.OpenThread = func(string) {}
	m.Callbacks.CloseThread = func() {}
	m.Callbacks.ToggleDetails = func(bool) {}
	return m
}

func agentux062Page(t *testing.T, m chatui.Model) string {
	t.Helper()
	markup, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	page := html.UnescapeString(markup)
	if strings.Contains(page, "⟦") {
		t.Fatalf("a copy key is printed: %s", regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
	}
	return page
}

// TestTodo_AGENTUX_062_Browser opens, in three languages and with the product's
// catalog, the layers a person opens from a message: the reaction picker on a
// message that is also the root of the open thread, a message's menu, and the
// conversation details. One picker opens per action, a menu holds its actions,
// the thread pane's close control is named, and nothing prints a key.
func TestTodo_AGENTUX_062_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := agentux062Model(locale)
			m.ShowThread, m.ThreadParentID = true, "root"
			m.PickerID = "root"
			page := agentux062Page(t, m)
			if n := strings.Count(page, `data-chat-layer="reaction"`); n != 1 {
				t.Errorf("reacting to the thread's root opened %d pickers", n)
			}
			if !strings.Contains(page, `type="search"`) || !strings.Contains(page, `role="grid"`) {
				t.Error("the picker has no search or no grid of emoji")
			}
			if !regexp.MustCompile(`<button[^>]*(?:data-action="close-thread"[^>]*aria-label="[^"]+"|aria-label="[^"]+"[^>]*data-action="close-thread")`).MatchString(page) {
				t.Error("the thread pane's close control has no name")
			}

			menu := agentux062Model(locale)
			menu.MenuID = "mine"
			page = agentux062Page(t, menu)
			for _, action := range []string{"copy-link", "edit", "delete", "pin"} {
				if !strings.Contains(page, `data-action="`+action+`"`) {
					t.Errorf("the menu of one's own message has no %s", action)
				}
			}
			if n := strings.Count(page, `data-message-menu=`); n != 1 {
				t.Errorf("%d message menus are open for one press", n)
			}
			// Everything in the menu is a menu item: no stray link.
			root, err := xhtml.Parse(strings.NewReader(page))
			if err != nil {
				t.Fatal(err)
			}
			var walk func(*xhtml.Node, bool)
			walk = func(n *xhtml.Node, inMenu bool) {
				if n.Type == xhtml.ElementNode {
					role := ""
					for _, a := range n.Attr {
						if a.Key == "role" {
							role = a.Val
						}
					}
					if role == "menu" {
						for _, a := range n.Attr {
							if a.Key == "data-message-menu" {
								inMenu = true
							}
						}
					}
					if inMenu && (n.Data == "button" || n.Data == "a") && role != "menuitem" {
						t.Errorf("<%s> in the menu is not a menu item", n.Data)
					}
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c, inMenu)
				}
			}
			walk(root, false)
		})
	}
}

// TestTodo_AGENTUX_063_Browser renders a 111-message channel with the product's
// catalog: it has fewer than 400 interactive elements, no message action bar
// until a row is active, and every control that shows no words has a name and a
// tooltip, in three languages.
func TestTodo_AGENTUX_063_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			m := agentux062Model(locale)
			at := time.Now().Add(-3 * time.Hour)
			m.Messages = nil
			for i := 0; i < 111; i++ {
				m.Messages = append(m.Messages, chatui.Message{ID: fmt.Sprint("m", i), AuthorID: "walt", Author: "Walt Brennan", Body: fmt.Sprint("Message ", i), SentAt: at.Add(time.Duration(i) * time.Minute), Revision: 1})
			}
			page := agentux062Page(t, m)
			controls := len(regexp.MustCompile(`<(?:button|a [^>]*href|input|select|textarea|summary)\b`).FindAllString(page, -1))
			if controls >= 400 {
				t.Errorf("111 messages render %d interactive elements", controls)
			}
			if strings.Contains(page, `class="message-actions"`) {
				t.Error("an idle channel renders message action bars")
			}
			root, err := xhtml.Parse(strings.NewReader(page))
			if err != nil {
				t.Fatal(err)
			}
			var walk func(*xhtml.Node)
			walk = func(n *xhtml.Node) {
				if n.Type == xhtml.ElementNode && n.Data == "button" {
					attrs := map[string]string{}
					for _, a := range n.Attr {
						attrs[a.Key] = a.Val
					}
					var text func(*xhtml.Node) string
					text = func(x *xhtml.Node) string {
						if x.Type == xhtml.TextNode {
							return x.Data
						}
						s := ""
						for c := x.FirstChild; c != nil; c = c.NextSibling {
							s += text(c)
						}
						return s
					}
					visible := strings.TrimSpace(text(n))
					if visible == "" && attrs["aria-label"] == "" && attrs["aria-labelledby"] == "" {
						t.Errorf("<button class=%q data-action=%q> has no name", attrs["class"], attrs["data-action"])
					}
					if visible == "" && attrs["title"] == "" && attrs["aria-describedby"] == "" {
						t.Errorf("<button class=%q data-action=%q> is icon-only without a tooltip", attrs["class"], attrs["data-action"])
					}
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
			}
			walk(root)
		})
	}
}
