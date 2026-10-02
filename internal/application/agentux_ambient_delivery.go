package application

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// The trusted writer and notification ports participate in the supplied chat
// transaction. Public and private ports are distinct so a private item cannot
// fall back to a channel post when its DM or notification is unavailable.
type AgentUXAmbientDeliveryAdapter struct {
	Service        *AgentUXAmbientService
	SourceLink     func(context.Context, string, string, string) (string, error)
	PublicMessage  func(context.Context, AgentUXAmbientOffer, string, string) (string, error)
	PrivateAgentDM func(context.Context, AgentUXAmbientOffer, string, string) (string, error)
	NotifyPerson   func(context.Context, string, string, string, string) error
}

func (a AgentUXAmbientDeliveryAdapter) DeliverAmbientAnnouncement(ctx context.Context, o AgentUXAmbientOffer, key string, at time.Time) (string, error) {
	var messageID string
	if a.Service == nil || !a.Service.available() || a.SourceLink == nil || key == "" || at.After(a.Service.Now()) || o.State != "SET" || o.Kind != "REMINDER" {
		return "", ErrAgentUXAmbientDenied
	}
	err := a.Service.DB.RunTenantTx(ctx, o.Tenant, func(tx dbport.Tx) error {
		var body string
		var data []byte
		var stored AgentUXAmbientOffer
		if err := tx.QueryRow(ctx, `SELECT p.body,o.data FROM chat_post p JOIN agentux_ambient_offer o ON o.tenant_id=p.tenant_id AND o.source_id=p.id AND o.conversation_id=p.conversation_id JOIN chat_app_installation i ON i.tenant_id=o.tenant_id AND i.conversation_id=o.conversation_id AND i.app_id=o.agent_id AND i.status='ACTIVE' AND i.version=(o.data->>'InstallationVersion')::bigint AND 'chat.posts.read'=ANY(i.granted_scopes) WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$3 AND p.revision=$4 AND NOT p.tombstoned AND o.id=$5 AND o.revision=$6 AND o.state='SET' FOR SHARE OF p,o,i`, o.Tenant, o.Conversation, o.Source, o.SourceRevision, o.ID, o.Revision).Scan(&body, &data); err != nil {
			return ErrAgentUXAmbientDenied
		}
		if json.Unmarshal(data, &stored) != nil || agentUXAmbientOfferDigest(stored) != agentUXAmbientOfferDigest(o) || !slices.ContainsFunc(stored.Time.Fire, func(t time.Time) bool { return t.Equal(at) }) {
			return ErrAgentUXAmbientDenied
		}
		if o.Scope == "PRIVATE" {
			if o.Person == "" || a.PrivateAgentDM == nil || a.NotifyPerson == nil {
				return ErrAgentUXAmbientDenied
			}
			if err := chatstore.AmbientChannelTaskSourceReadable(ctx, tx, o.Tenant, o.Tenant, o.Conversation, o.Person, o.Source); err != nil {
				return ErrAgentUXAmbientDenied
			}
		} else if o.Scope == "PUBLIC" {
			if o.Person != "" || a.PublicMessage == nil {
				return ErrAgentUXAmbientDenied
			}
			members, err := agentUXAmbientMembers(ctx, tx, o.Tenant, o.Conversation)
			if err != nil {
				return err
			}
			if len(members) == 0 {
				return ErrAgentUXAmbientDenied
			}
			for _, member := range members {
				if err = chatstore.AmbientChannelTaskSourceReadable(ctx, tx, o.Tenant, member.HomeTenant, o.Conversation, member.ID, o.Source); err != nil {
					return ErrAgentUXAmbientDenied
				}
			}
		} else {
			return ErrAgentUXAmbientDenied
		}
		effectCtx := dbport.ContextWithTx(ctx, tx)
		link, err := a.SourceLink(effectCtx, o.Tenant, o.Conversation, o.Source)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(link, "/") || strings.HasPrefix(link, "//") || strings.ContainsAny(link, "\r\n()\\") {
			return ErrAgentUXAmbientInvalid
		}
		// The body is read from the exact current source, not model prose, an
		// owner's private context, or the card's editable title.
		caption := "Open the original message"
		if strings.HasPrefix(strings.ToLower(o.Locale), "de") {
			caption = "Ursprüngliche Nachricht öffnen"
		} else if strings.HasPrefix(strings.ToLower(o.Locale), "ar") {
			caption = "افتح الرسالة الأصلية"
		}
		text := fmt.Sprintf("%s\n\n[%s](%s)", body, caption, link)
		if o.Scope == "PRIVATE" {
			messageID, err = a.PrivateAgentDM(effectCtx, o, text, key)
			if err == nil {
				err = a.NotifyPerson(effectCtx, o.Tenant, o.Person, messageID, key)
			}
		} else {
			messageID, err = a.PublicMessage(effectCtx, o, text, key)
		}
		return err
	})
	return messageID, err
}
