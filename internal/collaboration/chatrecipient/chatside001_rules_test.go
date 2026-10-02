package chatrecipient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// sideLayout builds a saved sidebar: custom sections named by the pairs, each
// holding the conversations after its name, plus Channels holding "general".
func sideLayout(starred string, sections ...[]string) string {
	var parts []string
	for i, section := range sections {
		var rooms []string
		for _, id := range section[1:] {
			rooms = append(rooms, fmt.Sprintf(`{"hostTenantId":"home","conversationId":%q}`, id))
		}
		parts = append(parts, fmt.Sprintf(`{"id":"custom-%d","name":%q,"chats":[%s]}`, i, section[0], strings.Join(rooms, ",")))
	}
	parts = append(parts, `{"id":"channels","name":"Channels","chats":[{"hostTenantId":"home","conversationId":"general"}]}`)
	return `{"sections":[` + strings.Join(parts, ",") + `],"starred":[` + starred + `]}`
}

// TestTodo_CHATSIDE_001: the rules a saved sidebar layout keeps: names of one to
// forty characters that are unique for the person, at most twenty sections of
// their own, a conversation in one section only, stars that name a conversation
// the layout holds, and a layout saved before the rules existing still saves.
func TestTodo_CHATSIDE_001(t *testing.T) {
	p := chat.Principal{TenantID: "home", SubjectID: "alice"}
	put := func(stored Sidebar, layout string) error {
		s := &Service{Conversations: conversations{allowed: true}, Repo: &repo{sidebar: stored}}
		_, err := s.PutSidebar(context.Background(), p, Sidebar{Layout: []byte(layout)}, 1)
		return err
	}
	if err := put(Sidebar{}, sideLayout(`"general"`, []string{"Projects", "sales"}, []string{"Reading"})); err != nil {
		t.Fatalf("a layout with two sections and a favorite was refused: %v", err)
	}
	many := make([][]string, 0, 21)
	for i := 0; i < 21; i++ {
		many = append(many, []string{fmt.Sprintf("Section %d", i)})
	}
	for name, layout := range map[string]string{
		"empty name":                       sideLayout("", []string{"  "}),
		"name over forty characters":       sideLayout("", []string{strings.Repeat("x", 41)}),
		"duplicate name, other case":       sideLayout("", []string{"Projects"}, []string{"projects "}),
		"the word Favorites":               sideLayout("", []string{"favorites"}),
		"twenty-one sections":              sideLayout("", many...),
		"a conversation in two":            sideLayout("", []string{"One", "sales"}, []string{"Two", "sales"}),
		"a star for an unknown room":       sideLayout(`"nowhere"`, []string{"One", "sales"}),
		"a star listed twice":              sideLayout(`"sales","sales"`, []string{"One", "sales"}),
		"a stored section named favorites": `{"sections":[{"id":"favorites","name":"x"}]}`,
	} {
		if err := put(Sidebar{}, layout); !errors.Is(err, chat.ErrInvalidArgument) {
			t.Errorf("%s: err = %v, want ErrInvalidArgument", name, err)
		}
	}
	forty := strings.Repeat("é", 40)
	if err := put(Sidebar{}, sideLayout("", []string{forty})); err != nil {
		t.Errorf("a name of exactly forty characters (counted as characters, not bytes) was refused: %v", err)
	}
	// A section saved before the rules existed keeps saving while it is untouched.
	legacy := Sidebar{Layout: []byte(sideLayout("", []string{strings.Repeat("y", 60)}))}
	if err := put(legacy, sideLayout("", []string{strings.Repeat("y", 60)}, []string{"New one"})); err != nil {
		t.Errorf("an untouched older section blocked a save: %v", err)
	}
	if err := put(legacy, sideLayout("", []string{strings.Repeat("y", 61)})); !errors.Is(err, chat.ErrInvalidArgument) {
		t.Errorf("a changed long name was accepted: %v", err)
	}
	// Deleting a section is a save without it; its conversations are named in
	// Channels again and nothing is left out.
	if err := put(Sidebar{Layout: []byte(sideLayout("", []string{"Projects", "sales"}))}, `{"sections":[{"id":"channels","name":"Channels","chats":[{"hostTenantId":"home","conversationId":"general"},{"hostTenantId":"home","conversationId":"sales"}]}]}`); err != nil {
		t.Errorf("deleting a section was refused: %v", err)
	}
}
