package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// CapSend is the governed notice-send operation (WF-EXT-015). The notify
// fragment binds to it; the served handler adapts runner calls onto [Send].
const CapSend = "hcmnext.messaging.send"

// Definition returns the send capability manifest: an external mutation —
// the notice leaves the platform — over the notice intent contract.
func Definition() capability.Definition {
	return capability.Definition{
		ID:                   CapSend,
		Version:              1,
		OwnerDomain:          "messaging",
		RequestSchema:        capability.SchemaRef{SchemaID: CapSend + ".request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		ResponseSchema:       capability.SchemaRef{SchemaID: CapSend + ".response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		ErrorSchema:          capability.SchemaRef{SchemaID: CapSend + ".error/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		EffectClass:          capability.EffectExternalMutation,
		ReadData:             capability.DataDomainFieldSet{DataDomains: []string{"notice"}},
		WriteData:            capability.DataDomainFieldSet{DataDomains: []string{"notice"}},
		RiskClass:            "HIGH",
		IdempotencyPolicyRef: "idempotency.effect-key.v1",
		AgentEligible:        false,
		AuthZScopeRef:        "scope:messaging.send",
		LegalBasisRef:        "legal.messaging.notice.v1",
		EntitlementRef:       "entitlement.messaging.send.v1",
		SLOClassRef:          "slo.interactive.p95-2s.v1",
		TestRef:              "conformance:" + CapSend + "/v1",
	}
}

// Definitions returns the messaging capability manifests.
func Definitions() []capability.Definition {
	return []capability.Definition{Definition()}
}

// Sender is the vendor edge a notice is handed to. The capability proves
// the governed intent; the sender proves delivery. Test compositions record.
type Sender interface {
	Send(ctx context.Context, intent MessageIntent) (Receipt, error)
}

// Receipt is the sender's acceptance of one notice.
type Receipt struct {
	MessageID string
}

// SendOutcome is what the send operation returns: the sender's message
// identity plus the digest of the exact intent that was sent.
type SendOutcome struct {
	MessageID string
	Digest    string
	Intent    MessageIntent
}

// Send validates the notice intent and hands it to the sender. Inputs are
// the well-known notice paths; notice_key is the stable effect identity,
// so resending the same key addresses the same logical notice.
func Send(ctx context.Context, s Sender, inputs map[string]string) (SendOutcome, error) {
	if s == nil {
		return SendOutcome{}, fmt.Errorf("%w: send requires a sender", ErrInvalidIntent)
	}
	expires, err := strconv.ParseInt(strings.TrimSpace(inputs["expires_at"]), 10, 64)
	if err != nil {
		return SendOutcome{}, fmt.Errorf("%w: expires_at is unix seconds: %v", ErrInvalidIntent, err)
	}
	intent, err := Accept(MessageIntent{
		IntentID:                 "notice-" + strings.TrimSpace(inputs["notice_key"]),
		TenantID:                 inputs["tenant_id"],
		OrganizationScope:        inputs["organization_scope"],
		Purpose:                  Purpose(inputs["purpose"]),
		AudienceExpression:       inputs["audience_expression"],
		AudienceResolutionPolicy: inputs["audience_resolution_policy"],
		ContentRef:               inputs["content_ref"],
		TemplateRef:              inputs["template_ref"],
		ParametersRef:            inputs["parameters_ref"],
		Classification:           inputs["classification"],
		Urgency:                  inputs["urgency"],
		DeliveryRequirement:      DeliveryRequirement(inputs["delivery_requirement"]),
		ReplyMode:                ReplyMode(inputs["reply_mode"]),
		CorrelationID:            inputs["correlation_id"],
		ExpiresAt:                time.Unix(expires, 0).UTC(),
	})
	if err != nil {
		return SendOutcome{}, err
	}
	if strings.TrimSpace(inputs["notice_key"]) == "" {
		return SendOutcome{}, fmt.Errorf("%w: notice_key is required", ErrInvalidIntent)
	}
	receipt, err := s.Send(ctx, intent)
	if err != nil {
		return SendOutcome{}, err
	}
	if strings.TrimSpace(receipt.MessageID) == "" {
		return SendOutcome{}, fmt.Errorf("%w: sender returned no message identity", ErrInvalidIntent)
	}
	return SendOutcome{MessageID: receipt.MessageID, Digest: DigestIntent(intent), Intent: intent}, nil
}

// DigestIntent is the stable identity of the exact notice that was sent.
func DigestIntent(m MessageIntent) string {
	parts := []string{"messaging-notice", m.IntentID, m.TenantID, m.OrganizationScope,
		string(m.Purpose), m.AudienceExpression, m.AudienceResolutionPolicy,
		m.ContentRef, m.TemplateRef, m.ParametersRef, m.Classification,
		string(m.DeliveryRequirement), string(m.ReplyMode), m.CorrelationID}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
