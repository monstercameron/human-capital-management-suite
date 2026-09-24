package application

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/webhookreceiver"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/platformidempotencystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/webhookreceipts"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/idempotency"
	transportwebhook "github.com/monstercameron/human-capital-management-suite/internal/transport/webhook"
)

type providerWebhookReceivers struct {
	payroll http.Handler
	iam     http.Handler
}

func composeProviderWebhookReceivers(cfg ServeConfig, pool *pgxadapter.Pool, now func() time.Time) (providerWebhookReceivers, error) {
	if cfg.PayrollWebhookEndpointID == "" && cfg.IAMWebhookEndpointID == "" {
		return providerWebhookReceivers{}, nil
	}
	if pool == nil {
		return providerWebhookReceivers{}, fmt.Errorf("application: configured provider webhook endpoints require the database pool")
	}
	if strings.TrimSpace(cfg.Tenant) == "" {
		return providerWebhookReceivers{}, fmt.Errorf("application: configured provider webhook endpoints require a fixed tenant")
	}

	registry := idempotency.NewRegistryWithStore(platformidempotencystore.New(pool))
	// The application-facing tenant is a slug. Provider receipts and the
	// durable idempotency table use the canonical tenant UUID.
	tenantID := pgstore.TenantID(cfg.Tenant).String()
	var receivers providerWebhookReceivers
	if endpointID := strings.TrimSpace(cfg.PayrollWebhookEndpointID); endpointID != "" {
		endpoint := providerreceipt.PayrollEndpoint(endpointID, tenantID, []byte(cfg.PayrollWebhookSecret))
		verifier, err := providerreceipt.NewVerifierWithRegistry(endpoint, registry)
		if err != nil {
			return providerWebhookReceivers{}, fmt.Errorf("application: compose payroll provider webhook verifier: %w", err)
		}
		sink, err := webhookreceiver.NewSink(pool, webhookreceipts.Scope{
			TenantID: pgstore.TenantID(cfg.Tenant), Provider: transportwebhook.PayrollProvider, EndpointID: endpointID,
		})
		if err != nil {
			return providerWebhookReceivers{}, fmt.Errorf("application: compose payroll provider webhook sink: %w", err)
		}
		receivers.payroll = transportwebhook.Receiver{Verifier: verifier, Sink: sink, Now: now}
	}
	if endpointID := strings.TrimSpace(cfg.IAMWebhookEndpointID); endpointID != "" {
		endpoint := providerreceipt.IAMEndpoint(endpointID, tenantID, []byte(cfg.IAMWebhookSecret))
		verifier, err := providerreceipt.NewVerifierWithRegistry(endpoint, registry)
		if err != nil {
			return providerWebhookReceivers{}, fmt.Errorf("application: compose IAM provider webhook verifier: %w", err)
		}
		sink, err := webhookreceiver.NewSink(pool, webhookreceipts.Scope{
			TenantID: pgstore.TenantID(cfg.Tenant), Provider: transportwebhook.IAMProvider, EndpointID: endpointID,
		})
		if err != nil {
			return providerWebhookReceivers{}, fmt.Errorf("application: compose IAM provider webhook sink: %w", err)
		}
		receivers.iam = transportwebhook.Receiver{Verifier: verifier, Sink: sink, Now: now}
	}
	return receivers, nil
}
