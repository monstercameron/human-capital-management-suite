package chatstore

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// AuthorizePublicChatDisclosure checks exact current source and recipient
// authority, including citation titles and current-member history restrictions.
// Worker records and arbitrary fields are deliberately outside this chat port.
func (s *Store) AuthorizePublicChatDisclosure(ctx context.Context, hostTenant, expectedConversation, homeTenant, subject, postID, expectedDigest, field, title, class string) error {
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(hostTenant) == "" || strings.TrimSpace(expectedConversation) == "" || strings.TrimSpace(homeTenant) == "" || strings.TrimSpace(subject) == "" || strings.TrimSpace(postID) == "" || field != "body" || title != "" || (class != "PUBLIC" && class != "INTERNAL") {
		return ErrNotMember
	}
	return s.RunTenantTx(ctx, hostTenant, func(tx dbport.Tx) error {
		var conversationID string
		if err := tx.QueryRow(ctx, `SELECT conversation_id FROM chat_post WHERE tenant_id=$1 AND id=$2 AND NOT tombstoned`, hostTenant, postID).Scan(&conversationID); err != nil {
			return err
		}
		if conversationID != expectedConversation {
			return ErrNotMember
		}
		var revision uint64
		var name, kind, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT audience_revision,name,kind,lifecycle FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, hostTenant, conversationID).Scan(&revision, &name, &kind, &lifecycle); err != nil {
			return err
		}
		if kind != "PUBLIC_CHANNEL" || lifecycle != "ACTIVE" || (title != "" && title != name) {
			return ErrNotMember
		}
		var sourceBody, sourceDigest, sourceClass string
		if err := tx.QueryRow(ctx, `SELECT p.body,c.body_digest,c.data_class FROM chat_post p JOIN chat_persona_source_classification c ON c.tenant_id=p.tenant_id AND c.conversation_id=p.conversation_id AND c.post_id=p.id WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$3 AND NOT p.tombstoned`, hostTenant, conversationID, postID).Scan(&sourceBody, &sourceDigest, &sourceClass); err != nil {
			return err
		}
		if publicChatBodyDigest(sourceBody) != sourceDigest || sourceDigest != expectedDigest || sourceClass != class {
			return ErrNotMember
		}
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_public_audience_principal e JOIN chat_public_audience_policy p ON p.tenant_id=e.tenant_id AND p.conversation_id=e.conversation_id JOIN chat_persona_channel_policy cp ON cp.tenant_id=e.tenant_id AND cp.conversation_id=e.conversation_id JOIN chat_post post ON post.tenant_id=e.tenant_id AND post.conversation_id=e.conversation_id AND post.id=$5 AND NOT post.tombstoned WHERE e.tenant_id=$1 AND e.conversation_id=$2 AND e.home_tenant_id=$3 AND e.subject_id=$4 AND (p.classification=$6 OR ($6='PUBLIC' AND p.classification='INTERNAL')) AND cp.policy_json->'allowed_data_classes' ? $6 AND NOT COALESCE((cp.policy_json->>'always_private')::boolean,true) AND ($6='PUBLIC' OR (e.home_tenant_id=e.tenant_id AND NOT e.guest)) AND NOT EXISTS(SELECT 1 FROM chat_membership m WHERE m.tenant_id=e.tenant_id AND m.conversation_id=e.conversation_id AND m.home_tenant_id=e.home_tenant_id AND m.member_id=e.subject_id AND m.state='active' AND m.history_visibility='FROM_JOIN' AND m.joined_at>post.created_at))`, hostTenant, conversationID, homeTenant, subject, postID, class).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return ErrNotMember
		}
		return nil
	})
}
