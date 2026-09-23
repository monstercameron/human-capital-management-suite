package application

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatappstore"
)

// chatReferenceDirectory derives picker candidates from the chat database's
// current membership and installed-app records. It carries no profile cache
// and therefore cannot suggest a person or agent from stale directory state.
type chatReferenceDirectory struct {
	store chat.Store
	apps  *chatapps.Service
}

func (d chatReferenceDirectory) People(ctx context.Context, _ chat.Principal, tenant, conversation, query string) ([]chat.ReferenceCandidate, error) {
	v, err := d.store.ListMemberships(ctx, tenant, conversation, chat.Page{PageSize: 200})
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]chat.ReferenceCandidate, 0, len(v.Memberships))
	for _, m := range v.Memberships {
		if m.LeftAt != nil || (query != "" && !strings.Contains(strings.ToLower(m.SubjectID), query)) {
			continue
		}
		out = append(out, chat.ReferenceCandidate{Reference: chat.Reference{Kind: chat.PersonMention, TenantID: m.HomeTenantID, ID: m.SubjectID, Display: m.SubjectID}, Eligible: true})
	}
	return out, nil
}

func (d chatReferenceDirectory) Agents(ctx context.Context, _ chat.Principal, tenant, conversation, query string) ([]chat.ReferenceCandidate, error) {
	if d.apps == nil || d.apps.Repo == nil {
		return []chat.ReferenceCandidate{}, nil
	}
	installs, err := d.apps.Repo.ByConversation(ctx, tenant, conversation)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	at := time.Now().UTC()
	if d.apps.Now != nil {
		at = d.apps.Now().UTC()
	}
	out := make([]chat.ReferenceCandidate, 0, len(installs))
	for _, in := range installs {
		if !in.Current(at) || in.Manifest.Agent == nil {
			continue
		}
		name := in.Manifest.Agent.DisplayName
		if query != "" && !strings.Contains(strings.ToLower(name), query) && !strings.Contains(strings.ToLower(in.ID), query) {
			continue
		}
		out = append(out, chat.ReferenceCandidate{Reference: chat.Reference{Kind: chat.AgentMention, TenantID: tenant, ID: in.ID, Display: name}, Eligible: true})
	}
	return out, nil
}

func (d chatReferenceDirectory) Conversations(ctx context.Context, p chat.Principal, tenant, conversation, query string) ([]chat.ReferenceCandidate, error) {
	// The store's membership query is the discovery boundary. The service
	// reauthorizes each selected conversation before returning it.
	_ = conversation
	v, err := d.store.ListConversations(ctx, p, tenant, chat.Page{PageSize: 200}, chat.ConversationScope{})
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]chat.ReferenceCandidate, 0, len(v.Conversations))
	for _, c := range v.Conversations {
		if query != "" && !strings.Contains(strings.ToLower(c.Name), query) && !strings.Contains(strings.ToLower(c.ID), query) {
			continue
		}
		out = append(out, chat.ReferenceCandidate{Reference: chat.Reference{Kind: chat.ConversationMention, TenantID: c.TenantID, ID: c.ID, ConversationID: c.ID, Display: c.Name}, Eligible: true})
	}
	return out, nil
}

func (d chatReferenceDirectory) VisibleConversations(ctx context.Context, tenant, subject, query string) ([]chat.ReferenceCandidate, error) {
	v, err := d.store.ListConversations(ctx, chat.Principal{TenantID: tenant, SubjectID: subject}, tenant, chat.Page{PageSize: 200}, chat.ConversationScope{})
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]chat.ReferenceCandidate, 0, len(v.Conversations))
	for _, c := range v.Conversations {
		if query != "" && !strings.Contains(strings.ToLower(c.Name), query) && !strings.Contains(strings.ToLower(c.ID), query) {
			continue
		}
		out = append(out, chat.ReferenceCandidate{Reference: chat.Reference{Kind: chat.ConversationMention, TenantID: c.TenantID, ID: c.ID, ConversationID: c.ID, Display: c.Name}, Eligible: true})
	}
	return out, nil
}

func (d chatReferenceDirectory) AgentEligible(ctx context.Context, tenant, conversation, id string) bool {
	if d.apps == nil || d.apps.Repo == nil {
		return false
	}
	v, err := d.apps.Repo.Get(chatappstore.WithTenant(ctx, tenant), id)
	return err == nil && v.Tenant == tenant && v.Conversation == conversation && v.Status == chatapps.Active && v.Manifest.Agent != nil
}
