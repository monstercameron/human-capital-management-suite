package main

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/sendingdomainstore"
	delivery "github.com/monstercameron/human-capital-management-suite/internal/operations/messagingdelivery"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

const defaultDomainDNSInterval = 6 * time.Hour

func runDomainDNSVerifier(ctx context.Context, store *sendingdomainstore.Store, interval time.Duration, clock bootstrap.Clock, logger bootstrap.Logger) error {
	if store == nil || interval <= 0 || logger == nil {
		return delivery.ErrInvalidEmail
	}
	if clock == nil {
		clock = time.Now
	}
	verifier := delivery.DomainReverifier{Profiles: store, DNS: delivery.NetDNSResolver{}, Alerts: store, Now: clock}
	run := func() {
		if err := verifier.RunOnce(ctx); err != nil {
			logger.Error("scheduler.sending_domain_dns.failed", "error", err.Error())
			return
		}
		logger.Info("scheduler.sending_domain_dns.completed", "checked_at", clock().UTC().Format(time.RFC3339))
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			run()
		}
	}
}
