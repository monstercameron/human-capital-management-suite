package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chattoneSettingsMemo reads each workspace's reword settings once for one
// projection, so a page of posts costs one read, not one per post.
type chattoneSettingsMemo struct {
	ChattoneRewordStore
	rows map[string][]chatstore.RewordSetting
}

func (m *chattoneSettingsMemo) LoadRewordSettings(ctx context.Context, tenant string) ([]chatstore.RewordSetting, error) {
	if rows, ok := m.rows[tenant]; ok {
		return rows, nil
	}
	rows, err := m.ChattoneRewordStore.LoadRewordSettings(ctx, tenant)
	if err != nil {
		return nil, err
	}
	if m.rows == nil {
		m.rows = map[string][]chatstore.RewordSetting{}
	}
	m.rows[tenant] = rows
	return rows, nil
}

func (m *chattoneSettingsMemo) RewordExempt(ctx context.Context, tenant, post string, revision uint64) (bool, error) {
	if inner, ok := m.ChattoneRewordStore.(chattoneExemptions); ok {
		return inner.RewordExempt(ctx, tenant, post, revision)
	}
	return false, nil
}

func (m *chattoneSettingsMemo) SaveRewordExemption(ctx context.Context, tenant, post string, revision uint64, person string) error {
	if inner, ok := m.ChattoneRewordStore.(chattoneExemptions); ok {
		return inner.SaveRewordExemption(ctx, tenant, post, revision, person)
	}
	return errChattoneRewordNotComposed
}

// chattoneReaderProjector is the one place that keeps the original of a heated
// message from a reader the channel does not let read it as written (Reword
// "Members may view messages as written" = No). Chat's reader views (history,
// threads, search hits, pins, saved messages, links, the live stream) ask it for
// every post they hand out, so no surface needs a rule of its own.
//
// The decision is the reword policy's: the same Apply that builds a message's
// rendering policy says whether this message is required reworded for this
// reader. The writer is never asked, and neither is anyone the policy exempts
// (the policy exempts nobody but the writer; there is no moderator exemption to
// copy here).
type chattoneReaderProjector struct {
	Policy     *ChattoneRewordPolicy
	Renderings ChattoneRenderingReader
	// Locale is the language of the stand-in line shown while the reworded text
	// is not ready; the page draws its own, localized, line from the selection.
	Locale string
}

func (p chattoneReaderProjector) ProjectReaderBodies(ctx context.Context, reader chat.Principal, posts []chat.Post) []chat.ReaderBody {
	out := make([]chat.ReaderBody, len(posts))
	if p.Policy == nil || p.Policy.Store == nil {
		return out
	}
	memo := &chattoneSettingsMemo{ChattoneRewordStore: p.Policy.Store}
	policy := *p.Policy
	policy.Store = memo
	for i, post := range posts {
		if post.Deleted || strings.TrimSpace(post.Body) == "" || chatlangOwn(reader, post) {
			continue
		}
		if _, system := chat.ParseMembershipAdded(post.Body); system {
			continue
		}
		rows, err := memo.LoadRewordSettings(ctx, post.TenantID)
		if err != nil {
			// Whether this reader may read the post is not known: they are not shown it.
			out[i] = p.standIn(chatrender.Mark{State: "unavailable"})
			continue
		}
		setting := chattoneRewordEffective(rows, post.ConversationID)
		if setting.Mode == chatstore.RewordModeOff || setting.MembersMayViewOriginal {
			continue
		}
		gate := chatrender.Policy{
			Original:      chatrender.Rendering{Tenant: post.TenantID, Message: post.ID, Revision: post.Revision},
			AllowOriginal: true,
			AllowedKinds:  []chatrender.Kind{chatrender.Mask},
		}
		if err := policy.Apply(ctx, post.TenantID, post.ConversationID, post.AuthorID, false, post.Body, &gate); err != nil {
			out[i] = p.standIn(chatrender.Mark{State: "unavailable"})
			continue
		}
		if !gate.RequireReworded {
			continue
		}
		out[i] = p.reworded(ctx, reader, post)
	}
	return out
}

// reworded is what this reader is shown of a message they may not read as
// written: the reworded text when it is ready, otherwise a neutral line. It
// asks for the rendering (the same read the page makes), so a list that is
// opened starts the rewording nobody has asked for yet.
func (p chattoneReaderProjector) reworded(ctx context.Context, reader chat.Principal, post chat.Post) chat.ReaderBody {
	if p.Renderings == nil {
		return p.standIn(chatrender.Mark{State: "unavailable"})
	}
	scope := chatstore.RenderingScope{Principal: reader, Tenant: post.TenantID, Conversation: post.ConversationID}
	selected, mark, err := p.Renderings.ReadRenderingSelection(ctx, scope, post.ID)
	if err != nil {
		return p.standIn(chatrender.Mark{State: "unavailable"})
	}
	switch mark.State {
	case "ready":
		if strings.TrimSpace(selected.Text) != "" {
			return chat.ReaderBody{Text: selected.Text, Replaced: true}
		}
	case "original", "fallback":
		// The message's own policy lets this reader read it as written.
		return chat.ReaderBody{}
	}
	return p.standIn(mark)
}

// standIn is the neutral line for a message whose text this reader may not be
// shown yet: "pending" while the rewording is on its way, "unavailable" when it
// will not come.
func (p chattoneReaderProjector) standIn(mark chatrender.Mark) chat.ReaderBody {
	key := "unavailable"
	if mark.State == "pending" {
		key = "pending"
	}
	return chat.ReaderBody{Text: chatui.RenderingText(p.Locale, key), Replaced: true}
}

var _ chat.ReaderBodyProjector = chattoneReaderProjector{}
