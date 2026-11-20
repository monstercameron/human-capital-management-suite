package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// Issue 3817: a request that waits for review must reach the people who review
// it, and the "tell administrators after N hours" settings must do something.
// chatgate.Service.SweepQueue raises the deadline notices; this is the served
// caller that runs it for every gate of every served workspace.

const (
	// chatgateReviewSweepInterval is how often waiting requests are checked
	// against their deadlines. The shortest deadline an administrator may set is
	// an hour, so a quarter of an hour is soon enough.
	chatgateReviewSweepInterval = 15 * time.Minute

	// chatgateSweepActor is the identity the sweep runs as. It is not a person:
	// it holds exactly the "retention" permission, and only on a context that
	// the in-process sweep itself marked (chatgateSystemContext).
	chatgateSweepActor = "system:chat-gate-review-sweep"
)

type chatgateSystemKey struct{}

// chatgateSystemContext marks a context as the served sweep's. The key type is
// unexported, so no request, header or stored value can produce it.
func chatgateSystemContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, chatgateSystemKey{}, true)
}

// chatgateIsSystem reports whether the actor is the sweep acting on a marked
// context.
func chatgateIsSystem(ctx context.Context, a chatgate.Actor) bool {
	marked, _ := ctx.Value(chatgateSystemKey{}).(bool)
	return marked && a.Person == chatgateSweepActor && a.Tenant != ""
}

// chatgateScopeLister lists the gates of one workspace.
type chatgateScopeLister interface {
	ListGateScopes(ctx context.Context, tenant string) ([]chatgate.Scope, error)
}

// chatgateSweeper runs the deadline sweep of one gate.
type chatgateSweeper interface {
	SweepQueue(ctx context.Context, a chatgate.Actor, scope chatgate.Scope) (overdue, escalated int, err error)
}

// sweepChatgateReviewQueues sweeps every gate of the given workspaces, each as
// that workspace's system actor. A gate or workspace that fails is logged and
// the rest are still swept. It returns how many gates were swept without error.
func sweepChatgateReviewQueues(ctx context.Context, lister chatgateScopeLister, sweeper chatgateSweeper, tenants []string, logger personaInvocationLogger) int {
	swept := 0
	fail := func(tenant, conversation string, err error) {
		if ctx.Err() == nil && !isNilPersonaOutputPort(logger) {
			logger.Error("hcmnext.chat_gate_review_sweep_failed", "tenant_id", tenant, "conversation_id", conversation, "error_type", fmt.Sprintf("%T", err))
		}
	}
	for _, tenant := range tenants {
		if ctx.Err() != nil {
			return swept
		}
		scopes, err := lister.ListGateScopes(ctx, tenant)
		if err != nil {
			fail(tenant, "", err)
			continue
		}
		actor := chatgate.Actor{Tenant: tenant, Person: chatgateSweepActor}
		for _, scope := range scopes {
			if ctx.Err() != nil {
				return swept
			}
			if scope.Tenant != tenant {
				continue
			}
			if _, _, err = sweeper.SweepQueue(chatgateSystemContext(ctx), actor, scope); err != nil {
				fail(tenant, scope.Conversation, err)
				continue
			}
			swept++
		}
	}
	return swept
}

// chatgateReviewSweepWorkload is the served sweep of waiting join requests.
// It runs beside the other chat background work for the workspaces this process
// serves, and is nil when there is nothing to sweep with.
func chatgateReviewSweepWorkload(lister chatgateScopeLister, sweeper chatgateSweeper, tenants []string, logger personaInvocationLogger, interval time.Duration) *bootstrap.Workload {
	if isNilPersonaOutputPort(lister) || isNilPersonaOutputPort(sweeper) || interval <= 0 {
		return nil
	}
	scoped := make([]string, 0, len(tenants))
	seen := make(map[string]bool, len(tenants))
	for _, tenant := range tenants {
		if strings.TrimSpace(tenant) != tenant || values.TenantId(tenant).Validate() != nil {
			return nil
		}
		if !seen[tenant] {
			seen[tenant] = true
			scoped = append(scoped, tenant)
		}
	}
	if len(scoped) == 0 {
		return nil
	}
	return &bootstrap.Workload{Name: "chat-gate-review-sweep", Run: func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			sweepChatgateReviewQueues(ctx, lister, sweeper, scoped, logger)
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}}
}

// chatgateSweepPorts is what the served composition hands the workload.
type chatgateSweepPorts struct {
	Lister  chatgateScopeLister
	Sweeper chatgateSweeper
}
