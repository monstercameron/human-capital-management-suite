package chatstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func publicChatBodyDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PutPublicChatPostClassification records server-owned source classification
// for exact current bytes. Only a trusted classifier may call this authority
// API; request payloads must never provide its class or digest assertion.
// Classification is independent of public disclosure: private and DM sources
// require the same exact-byte assertion before model egress.
func (s *Store) PutPublicChatPostClassification(ctx context.Context, tenantID, conversationID, postID, expectedDigest string, class dlp.DataClass) error {
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(postID) == "" || !class.Valid() {
		return ErrAudienceEligibilityUnavailable
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var revision uint64
		var kind, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT audience_revision,kind,lifecycle FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, conversationID).Scan(&revision, &kind, &lifecycle); err != nil {
			return err
		}
		if !personaClassifiableConversation(kind) || lifecycle != "ACTIVE" {
			return ErrAudienceEligibilityUnavailable
		}
		var body string
		if err := tx.QueryRow(ctx, `SELECT body FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 AND NOT tombstoned`, tenantID, conversationID, postID).Scan(&body); err != nil {
			return err
		}
		if publicChatBodyDigest(body) != expectedDigest {
			return ErrAudienceChanged
		}
		var currentDigest, currentClass string
		err := tx.QueryRow(ctx, `SELECT body_digest,data_class FROM chat_persona_source_classification WHERE tenant_id=$1 AND conversation_id=$2 AND post_id=$3`, tenantID, conversationID, postID).Scan(&currentDigest, &currentClass)
		if err != nil && !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if err == nil && currentDigest == expectedDigest && currentClass == string(class) {
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO chat_persona_source_classification(tenant_id,conversation_id,post_id,body_digest,data_class) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,conversation_id,post_id) DO UPDATE SET body_digest=EXCLUDED.body_digest,data_class=EXCLUDED.data_class`, tenantID, conversationID, postID, expectedDigest, string(class))
		return err
	})
}

// PublicChatDisclosureClass requires both the current source bytes and its
// independently persisted classifier assertion to match the citation digest.
func (s *Store) PublicChatDisclosureClass(ctx context.Context, tenantID, conversationID, postID, expectedDigest string) (dlp.DataClass, error) {
	return s.chatDisclosureClass(ctx, tenantID, "", "", conversationID, postID, expectedDigest)
}

// ReadableChatDisclosureClass additionally checks current source-reader
// membership and history. It supplies classification, never a public grant.
func (s *Store) ReadableChatDisclosureClass(ctx context.Context, tenantID, homeTenantID, subjectID, conversationID, postID, expectedDigest string) (dlp.DataClass, error) {
	if strings.TrimSpace(homeTenantID) == "" || strings.TrimSpace(subjectID) == "" {
		return "", ErrNotMember
	}
	return s.chatDisclosureClass(ctx, tenantID, homeTenantID, subjectID, conversationID, postID, expectedDigest)
}

func personaClassifiableConversation(kind string) bool {
	return kind == "PUBLIC_CHANNEL" || kind == "PRIVATE_CHANNEL" || kind == "DIRECT" || kind == "GROUP"
}

func (s *Store) chatDisclosureClass(ctx context.Context, tenantID, homeTenantID, subjectID, conversationID, postID, expectedDigest string) (dlp.DataClass, error) {
	var class dlp.DataClass
	if s == nil || s.pool == nil || ctx == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(postID) == "" || strings.TrimSpace(expectedDigest) == "" {
		return class, ErrAudienceEligibilityUnavailable
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var revision uint64
		var kind, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT audience_revision,kind,lifecycle FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, conversationID).Scan(&revision, &kind, &lifecycle); err != nil {
			return err
		}
		if !personaClassifiableConversation(kind) || lifecycle != "ACTIVE" {
			return ErrAudienceEligibilityUnavailable
		}
		var body, digest string
		var located bool
		if err := tx.QueryRow(ctx, `SELECT p.body,c.body_digest,c.data_class,EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(p.references_json)='array' THEN p.references_json ELSE '[]'::jsonb END) r WHERE r->>'Kind'='LOCATION') FROM chat_post p JOIN chat_persona_source_classification c ON c.tenant_id=p.tenant_id AND c.conversation_id=p.conversation_id AND c.post_id=p.id WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$3 AND NOT p.tombstoned`, tenantID, conversationID, postID).Scan(&body, &digest, &class, &located); err != nil {
			return err
		}
		if !class.Valid() || digest != expectedDigest || publicChatBodyDigest(body) != digest {
			return ErrAudienceChanged
		}
		// A shared location is special-category data whatever the words say, and
		// it can be attached after the text was classified, so it is applied here
		// where every disclosure reads the class.
		if located {
			class = dlp.DataClass(chat.LocationDLPClass)
		}
		if subjectID != "" {
			var readable bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_membership m JOIN chat_post p ON p.tenant_id=m.tenant_id AND p.conversation_id=m.conversation_id AND p.id=$5 AND NOT p.tombstoned WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.home_tenant_id=$3 AND m.member_id=$4 AND m.state='active' AND (m.history_visibility='FULL_HISTORY' OR (m.history_visibility='FROM_JOIN' AND m.joined_at<=p.created_at)))`, tenantID, conversationID, homeTenantID, subjectID, postID).Scan(&readable); err != nil {
				return err
			}
			if !readable {
				return ErrNotMember
			}
		}
		return nil
	})
	return class, err
}
