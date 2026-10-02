package chatrecipient

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATSIDE-001: a person's sidebar sections are their own. The layout document
// already carries the sections, the order of their conversations and the
// starred conversations (Favorites); these are the rules a saved layout must
// keep, checked before it is written so a client cannot store a shape the
// sidebar could not show.

const (
	// SectionNameMax is the longest name a section the person made may have.
	SectionNameMax = 40
	// SectionCustomMax is the most sections of their own a person may keep.
	SectionCustomMax = 20
	// FavoritesID is the built-in Favorites section. It is drawn from the starred
	// list and is never a stored section, so no stored section may use the id.
	FavoritesID = "favorites"
)

func builtinSection(id string) bool { return id == "channels" || id == "direct" }

// sectionNameOK is the name rule: one to forty characters once trimmed, and not
// the word Favorites, which the sidebar already uses for its own section.
func sectionNameOK(name string) bool {
	trimmed := strings.TrimSpace(name)
	n := utf8.RuneCountInString(trimmed)
	return n >= 1 && n <= SectionNameMax && !strings.EqualFold(trimmed, "favorites")
}

// validateSectionRules checks the section names, the number of sections, that a
// conversation sits in one section only and that every star names a
// conversation the layout holds. A section already saved under its present id
// and name is kept as it is, so a layout written before these rules existed can
// still be saved by a person who has not touched that section.
func (s *Service) validateSectionRules(ctx context.Context, p chat.Principal, layout sidebarLayout) error {
	// The stored layout is read only when a name or the count would otherwise be
	// refused, so an ordinary save costs no extra read.
	var previous map[string]string
	earlier := func() map[string]string {
		if previous != nil {
			return previous
		}
		previous = map[string]string{}
		if stored, err := s.Repo.Sidebar(ctx, p.TenantID, p.SubjectID); err == nil && len(stored.Layout) > 0 {
			var old sidebarLayout
			if json.Unmarshal(stored.Layout, &old) == nil {
				for _, section := range old.Sections {
					if !builtinSection(section.ID) {
						previous[section.ID] = section.Name
					}
				}
			}
		}
		return previous
	}
	custom := 0
	names := map[string]int{}
	for _, section := range layout.Sections {
		if section.ID == FavoritesID {
			return chat.ErrInvalidArgument
		}
		if builtinSection(section.ID) {
			continue
		}
		custom++
		names[strings.ToLower(strings.TrimSpace(section.Name))]++
	}
	if custom > SectionCustomMax && custom > len(earlier()) {
		return chat.ErrInvalidArgument
	}
	placed := map[string]bool{}
	for _, section := range layout.Sections {
		// An unnamed section is an older shape the client labels by its id; a
		// name that is written must meet the rule.
		if !builtinSection(section.ID) && section.Name != "" {
			if !sectionNameOK(section.Name) || names[strings.ToLower(strings.TrimSpace(section.Name))] > 1 {
				if old, kept := earlier()[section.ID]; !kept || old != section.Name {
					return chat.ErrInvalidArgument
				}
			}
		}
		for _, room := range section.Chats {
			if placed[room.ConversationID] {
				return chat.ErrInvalidArgument
			}
			placed[room.ConversationID] = true
		}
	}
	starred := map[string]bool{}
	for _, id := range layout.Starred {
		if starred[id] || !placed[id] {
			return chat.ErrInvalidArgument
		}
		starred[id] = true
	}
	return nil
}
