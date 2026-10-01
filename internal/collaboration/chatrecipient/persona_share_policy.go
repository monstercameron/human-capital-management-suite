package chatrecipient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdeliver"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func evaluatePersonaShareItems(ctx context.Context, result PrivatePersonaResult, authority AudienceFloorAuthority) ([]PersonaShareItemPreview, uint64) {
	items := make([]PersonaShareItemPreview, 0, len(result.Items))
	rendered := make(map[string]string, len(result.Items))
	outputSafe := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		body, err := renderPersonaShareItem(result, item)
		if err == nil {
			rendered[item.Output.ID] = body
			outputSafe[item.Output.ID] = true
		}
	}
	defaultReason := AudienceFloorInvalid
	if ctx == nil || isNilAuthority(authority) || invalidConversation(result.Conversation) || result.AlwaysPrivate || result.OutputPolicy.Policy.AlwaysPrivate {
		if result.AlwaysPrivate || result.OutputPolicy.Policy.AlwaysPrivate {
			defaultReason = AudienceFloorAlwaysPrivate
		}
		return droppedPersonaShareItems(result.Items, defaultReason), 0
	}
	if result.Conversation.Kind != chat.PublicChannel {
		return droppedPersonaShareItems(result.Items, AudienceFloorPrivateChannel), 0
	}
	snapshot, err := authority.CurrentAudience(ctx, result.Conversation)
	if err != nil || !snapshotMatches(snapshot, result.Conversation, true) {
		return droppedPersonaShareItems(result.Items, AudienceFloorIncomplete), 0
	}
	audience, ok := audienceSet(snapshot.CurrentMembers, snapshot.EligibilityPopulation)
	if !ok || len(audience) == 0 || len(snapshot.CurrentMembers) == 0 || len(snapshot.EligibilityPopulation) == 0 {
		return droppedPersonaShareItems(result.Items, AudienceFloorIncomplete), 0
	}
	classAllowed := make(map[string]bool)
	for _, item := range result.Items {
		for _, disclosure := range item.Disclosures {
			key := string(disclosure.DataClass)
			if _, seen := classAllowed[key]; seen {
				continue
			}
			classAllowed[key] = authority.AllowDataClass(ctx, result.Conversation, disclosure.DataClass) == nil
		}
	}
	for _, item := range result.Items {
		reason := AudienceFloorAllowed
		if !outputSafe[item.Output.ID] {
			reason = AudienceFloorInvalid
		}
		if len(item.Disclosures) == 0 {
			reason = AudienceFloorInvalid
		}
		for _, disclosure := range item.Disclosures {
			if reason != AudienceFloorAllowed {
				break
			}
			if !validDisclosure(disclosure) {
				reason = AudienceFloorInvalid
				break
			}
			if !classAllowed[string(disclosure.DataClass)] {
				reason = AudienceFloorUnauthorized
				break
			}
			for _, recipient := range audience {
				if authority.AuthorizeDisclosure(ctx, recipient, disclosure) != nil {
					reason = AudienceFloorUnauthorized
					break
				}
			}
			if reason != AudienceFloorAllowed {
				break
			}
		}
		items = append(items, PersonaShareItemPreview{ID: item.Output.ID, Text: rendered[item.Output.ID], Kept: reason == AudienceFloorAllowed, DropReason: reasonIfDropped(reason)})
	}
	return items, snapshot.Revision
}

func droppedPersonaShareItems(source []PrivatePersonaShareItem, reason AudienceFloorReason) []PersonaShareItemPreview {
	items := make([]PersonaShareItemPreview, 0, len(source))
	for _, item := range source {
		items = append(items, PersonaShareItemPreview{ID: item.Output.ID, Kept: false, DropReason: reason})
	}
	return items
}

func reasonIfDropped(reason AudienceFloorReason) AudienceFloorReason {
	if reason == AudienceFloorAllowed {
		return ""
	}
	return reason
}

func validShareSource(result PrivatePersonaResult) bool {
	if result.EphemeralID == "" || result.TenantID == "" || result.OwnerHomeTenantID == "" || result.OwnerSubjectID == "" || result.ThreadID == "" || strings.TrimSpace(result.PersonaDisplayName) == "" || invalidConversation(result.Conversation) || !validOutputConversation(result) || len(result.Items) == 0 || len(result.Items) > 100 {
		return false
	}
	seen := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		if strings.TrimSpace(item.Output.ID) == "" || seen[item.Output.ID] || strings.TrimSpace(item.Output.Text) == "" || len(item.Output.Text) > 64*1024 || len(item.Disclosures) > 50 {
			return false
		}
		seen[item.Output.ID] = true
	}
	return true
}

func validOutputConversation(result PrivatePersonaResult) bool {
	policy := result.OutputPolicy
	if policy.TenantID != result.Conversation.TenantID || policy.ConversationID != result.Conversation.ID {
		return false
	}
	switch result.Conversation.Kind {
	case chat.PublicChannel:
		return policy.Kind == agentdeliver.PublicChannel
	case chat.PrivateChannel:
		return policy.Kind == agentdeliver.PrivateChannel
	case chat.Direct:
		return policy.Kind == agentdeliver.Direct
	case chat.Group:
		return policy.Kind == agentdeliver.GroupDM
	default:
		return false
	}
}

func shareOutputDisclosureComplete(item PrivatePersonaShareItem) bool {
	for _, material := range item.Output.Materials {
		covered := false
		for _, disclosure := range item.Disclosures {
			if disclosure.DataClass != dlp.DataClass(material.DataClass) {
				continue
			}
			switch material.Kind {
			case agentdeliver.MaterialSource:
				covered = disclosure.SourceID == material.ID
			case agentdeliver.MaterialRecord:
				covered = disclosure.RecordID == material.ID
			case agentdeliver.MaterialField:
				covered = disclosure.Field == material.ID
			case agentdeliver.MaterialCitationTitle:
				covered = (disclosure.SourceID == material.ID || disclosure.CitationTitle == material.ID) && (material.Value == "" || disclosure.CitationTitle == material.Value)
			default:
				return false
			}
			if covered {
				break
			}
		}
		if !covered {
			return false
		}
	}
	for _, citation := range item.Output.Citations {
		covered := false
		for _, disclosure := range item.Disclosures {
			if disclosure.SourceID == citation.SourceID && disclosure.DataClass == dlp.DataClass(citation.DataClass) && (citation.Title == "" || disclosure.CitationTitle == citation.Title) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func renderPersonaShareItem(result PrivatePersonaResult, item PrivatePersonaShareItem) (string, error) {
	if !validOutputConversation(result) || item.Output.ID == "" || !shareOutputDisclosureComplete(item) {
		return "", agentdeliver.ErrUnsafeOutput
	}
	for _, disclosure := range item.Disclosures {
		if !validDisclosure(disclosure) {
			return "", agentdeliver.ErrUnsafeOutput
		}
	}
	rendered, err := agentdeliver.Render(result.OutputPolicy, agentdeliver.Result{PersonaLabel: result.PersonaDisplayName, Items: []agentdeliver.ResultItem{item.Output}})
	if err != nil || len(rendered.Items) != 1 || rendered.Items[0].ID != item.Output.ID || strings.TrimSpace(rendered.Items[0].Body) == "" {
		return "", agentdeliver.ErrUnsafeOutput
	}
	for _, material := range item.Output.Materials {
		if material.Value != "" && !strings.Contains(rendered.Items[0].Body, material.Value) {
			return "", agentdeliver.ErrUnsafeOutput
		}
	}
	return rendered.Items[0].Body, nil
}

func personaShareSourceDigest(result PrivatePersonaResult) string {
	payload := struct {
		ID, Tenant, OwnerTenant, Owner, Thread, Persona string
		Conversation                                    chat.Conversation
		OutputPolicy                                    agentdeliver.Conversation
		AlwaysPrivate                                   bool
		Items                                           []PrivatePersonaShareItem
	}{result.EphemeralID, result.TenantID, result.OwnerHomeTenantID, result.OwnerSubjectID, result.ThreadID, result.PersonaDisplayName, result.Conversation, result.OutputPolicy, result.AlwaysPrivate, result.Items}
	b, _ := json.Marshal(payload)
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}

func composePersonaShareBody(persona string, items []PersonaShareItemPreview) string {
	parts := make([]string, 0, len(items)+1)
	for _, item := range items {
		if item.Kept && strings.TrimSpace(item.Text) != "" {
			parts = append(parts, strings.TrimSpace(item.Text))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	label := strings.Join(strings.Fields(strings.TrimSpace(persona)), " ")
	if label == "" {
		return ""
	}
	body := "Shared from " + label + "\n\n" + strings.Join(parts, "\n\n")
	if len(body) > 4000 {
		return ""
	}
	return body
}

func fitPersonaShareItems(persona string, items []PersonaShareItemPreview) []PersonaShareItemPreview {
	fitted := append([]PersonaShareItemPreview(nil), items...)
	selected := make([]PersonaShareItemPreview, 0, len(items))
	for i := range fitted {
		if !fitted[i].Kept {
			continue
		}
		candidate := append(append([]PersonaShareItemPreview(nil), selected...), fitted[i])
		if composePersonaShareBody(persona, candidate) == "" {
			fitted[i].Kept = false
			fitted[i].DropReason = AudienceFloorInvalid
			continue
		}
		selected = candidate
	}
	return fitted
}

func sameShareItems(a, b []PersonaShareItemPreview) bool { return reflect.DeepEqual(a, b) }

func clonePersonaSharePreview(preview PersonaSharePreview) PersonaSharePreview {
	preview.Items = append([]PersonaShareItemPreview(nil), preview.Items...)
	return preview
}

func ptrPersonaSharePreview(preview PersonaSharePreview) *PersonaSharePreview { return &preview }

func newPersonaShareToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}

func validSharePrincipal(principal chat.Principal) bool {
	return strings.TrimSpace(principal.TenantID) != "" && strings.TrimSpace(principal.SubjectID) != ""
}

func isNilSharePort(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
