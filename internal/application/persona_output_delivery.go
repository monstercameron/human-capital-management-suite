package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdeliver"
)

// ErrPersonaOutputDeliveryUnavailable identifies an unwired output source,
// current policy source, or commit gate. Delivery cannot invent any of them.
var ErrPersonaOutputDeliveryUnavailable = errors.New("application: persona output delivery unavailable")

// PersonaOutputSource loads one complete, server-validated persona result for
// the requesting invoker. Implementations must enforce ownership and tenant
// scope; the application adapter does not trust an output ID by itself.
type PersonaOutputSource interface {
	LoadPersonaOutput(context.Context, PersonaOutputRequest) (agentdeliver.Result, error)
}

// PersonaOutputPolicySource resolves the current destination policy at the
// delivery boundary. A policy captured during inference is not sufficient.
type PersonaOutputPolicySource interface {
	CurrentPersonaOutputPolicy(context.Context, string, string) (agentdeliver.Conversation, error)
}

// PersonaOutputRequest binds an output to its original invocation and thread.
// OutputID is an opaque source key and never grants access on its own.
type PersonaOutputRequest struct {
	TenantID, ConversationID, ParentPostID, OutputID string
	Invoker                                          agentdeliver.AudienceMember
}

// PersonaOutputDeliveryConfig composes the source and current policy resolver
// with the existing agentdeliver audience-floor and commit-time ports.
type PersonaOutputDeliveryConfig struct {
	Source  PersonaOutputSource
	Policy  PersonaOutputPolicySource
	Service *agentdeliver.Service
}

// PersonaOutputDelivery is the application composition seam for persona
// result delivery. It has no authorization of its own.
type PersonaOutputDelivery struct {
	source  PersonaOutputSource
	policy  PersonaOutputPolicySource
	service *agentdeliver.Service
}

// NewPersonaOutputDelivery requires every authority and effect port needed by
// the delivery boundary. Missing composition fails before an output is read.
func NewPersonaOutputDelivery(config PersonaOutputDeliveryConfig) (*PersonaOutputDelivery, error) {
	if isNilPersonaOutputPort(config.Source) || isNilPersonaOutputPort(config.Policy) || config.Service == nil ||
		isNilPersonaOutputPort(config.Service.Audience) || isNilPersonaOutputPort(config.Service.Authorize) ||
		isNilPersonaOutputPort(config.Service.Public) || isNilPersonaOutputPort(config.Service.Private) {
		return nil, ErrPersonaOutputDeliveryUnavailable
	}
	return &PersonaOutputDelivery{source: config.Source, policy: config.Policy, service: config.Service}, nil
}

// Deliver loads and validates the complete result, resolves current policy,
// then delegates to the shared audience-floor gate. The gate publishes a full
// result only when every current and eligible reader is authorized; otherwise
// it emits a neutral receipt and the invoker-only ephemeral/private copy.
func (d *PersonaOutputDelivery) Deliver(ctx context.Context, request PersonaOutputRequest) (agentdeliver.DeliveryReceipt, error) {
	if d == nil || isNilPersonaOutputPort(d.source) || isNilPersonaOutputPort(d.policy) || d.service == nil || ctx == nil {
		return agentdeliver.DeliveryReceipt{}, ErrPersonaOutputDeliveryUnavailable
	}
	if !validPersonaOutputRequest(request) {
		return agentdeliver.DeliveryReceipt{}, agentdeliver.ErrInvalidRequest
	}
	result, err := d.source.LoadPersonaOutput(ctx, request)
	if err != nil {
		return agentdeliver.DeliveryReceipt{}, fmt.Errorf("load persona output: %w", err)
	}
	if strings.TrimSpace(result.PersonaLabel) == "" || len(result.Items) == 0 {
		return agentdeliver.DeliveryReceipt{}, agentdeliver.ErrInvalidRequest
	}
	conversation, err := d.policy.CurrentPersonaOutputPolicy(ctx, request.TenantID, request.ConversationID)
	if err != nil {
		return agentdeliver.DeliveryReceipt{}, fmt.Errorf("resolve current persona output policy: %w", err)
	}
	if !currentPersonaOutputPolicy(conversation, request) {
		return agentdeliver.DeliveryReceipt{}, agentdeliver.ErrInvalidRequest
	}
	return d.service.Deliver(ctx, agentdeliver.DeliveryRequest{
		Conversation: conversation,
		ParentPostID: request.ParentPostID,
		Invoker:      request.Invoker,
		Result:       result,
	})
}

func validPersonaOutputRequest(request PersonaOutputRequest) bool {
	return strings.TrimSpace(request.TenantID) != "" &&
		strings.TrimSpace(request.ConversationID) != "" &&
		strings.TrimSpace(request.ParentPostID) != "" &&
		strings.TrimSpace(request.OutputID) != "" &&
		strings.TrimSpace(request.Invoker.TenantID) != "" &&
		strings.TrimSpace(request.Invoker.SubjectID) != "" &&
		request.Invoker.TenantID == request.TenantID
}

func currentPersonaOutputPolicy(conversation agentdeliver.Conversation, request PersonaOutputRequest) bool {
	if strings.TrimSpace(conversation.TenantID) == "" || strings.TrimSpace(conversation.ConversationID) == "" || conversation.Kind == "" {
		return false
	}
	return conversation.TenantID == request.TenantID && conversation.ConversationID == request.ConversationID
}

// isNilPersonaOutputPort catches typed nil ports at the composition boundary.
func isNilPersonaOutputPort(value any) bool {
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
