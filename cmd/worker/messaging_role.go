package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/delivery"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/platformidempotencystore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
)

// MessagingDeliverySchemaRef identifies the only outbox payload the hosted
// messaging role consumes. Other outbox schemas belong to their own worker
// roles and retain the existing provider-neutral acknowledgement path.
const MessagingDeliverySchemaRef = "hcmnext.messaging.delivery.v1.Envelope@1"

var ErrMessagingPayload = errors.New("worker: invalid messaging delivery payload")

// messagingDeliverer is the provider-neutral port used by the worker role.
// A provider adapter implements connectivity/delivery.Transport behind the
// runner; cmd/worker never receives an address, body or credential.
type messagingDeliverer interface {
	Deliver(context.Context, delivery.Envelope) (delivery.Observation, error)
}

type messagingDeliveryRole struct {
	logger  bootstrap.Logger
	deliver messagingDeliverer
}

func (r messagingDeliveryRole) dispatch(ctx context.Context, msg outbox.Record) error {
	if !isMessagingSchema(msg.SchemaRef) {
		return dispatch(r.logger, msg)
	}
	envelope, err := decodeMessagingEnvelope(msg)
	if err != nil {
		return err
	}
	if r.deliver == nil {
		return errors.New("worker: messaging delivery runner is not configured")
	}
	observation, err := r.deliver.Deliver(ctx, envelope)
	r.logger.Info("worker.messaging_delivery_observed",
		"outbox_id", msg.OutboxID.String(), "intent_id", envelope.IntentID,
		"attempt_id", observation.AttemptID, "attempt", observation.Attempt,
		"state", string(observation.State), "provider_ref", observation.ProviderRef)
	return err
}

func decodeMessagingEnvelope(msg outbox.Record) (delivery.Envelope, error) {
	if msg.Tenant == [16]byte{} || len(bytes.TrimSpace(msg.Payload)) == 0 {
		return delivery.Envelope{}, fmt.Errorf("%w: tenant and payload are required", ErrMessagingPayload)
	}
	decoder := json.NewDecoder(bytes.NewReader(msg.Payload))
	decoder.DisallowUnknownFields()
	var envelope delivery.Envelope
	if err := decoder.Decode(&envelope); err != nil {
		return delivery.Envelope{}, fmt.Errorf("%w: decode envelope: %v", ErrMessagingPayload, err)
	}
	if envelope.TenantID != msg.Tenant.String() {
		return delivery.Envelope{}, fmt.Errorf("%w: tenant does not match outbox row", ErrMessagingPayload)
	}
	if err := envelope.Validate(msg.UpdatedAt); err != nil {
		return delivery.Envelope{}, fmt.Errorf("%w: %v", ErrMessagingPayload, err)
	}
	return envelope, nil
}

// unavailableMessagingTransport fails closed until a configured integration
// adapter is supplied. It is intentionally not a fake success: accepting an
// outbox row without a provider observation would turn provider outage into a
// false delivery claim.
type unavailableMessagingTransport struct{}

func (unavailableMessagingTransport) Deliver(context.Context, delivery.Envelope) (delivery.ProviderResult, error) {
	return delivery.ProviderResult{}, &delivery.ProviderError{
		Err: errors.New("messaging provider transport is not configured"), Retryable: true,
	}
}

func messagingRoleFor(deps bootstrap.Deps, pool workerPool, maxAttempts int) messagingDeliveryRole {
	store := delivery.PostgresAttemptStore{DB: pool, Provider: "hcmnext.messaging"}
	runner := delivery.Runner{Store: store, Transport: unavailableMessagingTransport{}, Idempotency: idempotency.NewRegistryWithStore(platformidempotencystore.New(pool)), MaxAttempts: maxAttempts}
	return messagingDeliveryRole{logger: deps.Logger, deliver: runner}
}

func isMessagingSchema(schemaRef string) bool {
	return strings.TrimSpace(schemaRef) == MessagingDeliverySchemaRef
}
