package journey

import (
	"context"
	"strings"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// redactedPayStandIn is the wire stand-in for a REDACTED compensation value.
// It is deliberately not a decimal: a probe comparing it against the stored
// baseline never matches, and the product UI renders the stand-in rather
// than the raw value until per-field verdicts ride the wire (REV-066-01).
const redactedPayStandIn = "REDACTED"

// authorizeWorkers is the single serialization helper every worker-bearing
// response passes through (RBAC-RT-001, REFACTOR). Row visibility is already
// decided: all holds the complete governed listing, visible the filtered
// subset. For each visible row the helper renders the wire message with
// [toWorker], then resolves that subject's field disclosure through the
// policy decision point under the principal's server-side role set
// (RBAC-RT-002: durable assignments, never credential claims) and omits or
// masks unauthorized fields before serialization. Deny by default: a
// disclosure that cannot be resolved fails closed to omitted pay, name and
// linkage on that row, never to a refused RPC, so one unresolvable subject
// cannot hide the whole directory.
func (s *server) authorizeWorkers(ctx context.Context, principal *trust.Principal, all, visible []workspace.WorkerSummary) []*journeyv1.Worker {
	roles := s.effectiveRoles(ctx, principal)
	subject := strings.ToLower(strings.TrimSpace(principal.Subject()))
	byRef := make(map[string]workspace.WorkerSummary, len(all)*2)
	for _, worker := range all {
		for _, key := range []string{worker.WorkerRef, worker.WorkerID} {
			if k := strings.ToLower(strings.TrimSpace(key)); k != "" {
				byRef[k] = worker
			}
		}
	}
	chained := make(map[string]bool, len(all))
	for _, worker := range all {
		// An empty ref addresses no one: it must never inherit another
		// row's chain reach through the shared empty key.
		if ref := normalizedWorkerIdentity(worker.WorkerRef); ref != "" && reportsTo(worker, subject, byRef) {
			chained[ref] = true
		}
	}

	out := make([]*journeyv1.Worker, 0, len(visible))
	for _, worker := range visible {
		msg := toWorker(worker)
		disclosure, err := authz.ResolveDirectoryDisclosureWithRoles(principal, roles, "", authz.DirectorySubject{
			Self:           workerMatchesPrincipal(worker, principal.Subject()),
			InManagerChain: chained[normalizedWorkerIdentity(worker.WorkerRef)],
		})
		if err != nil {
			maskWorker(msg)
		} else {
			applyDirectoryDisclosure(msg, disclosure)
		}
		out = append(out, msg)
	}
	return out
}

// authorizeWorker renders one worker-bearing response (the CreateWorker
// answer) through the same rulings as the listing. The chain is resolved
// over the single row: a creator named as the new worker's manager matches
// directly, anything needing a wider reporting line resolves once the row
// is listed.
func (s *server) authorizeWorker(ctx context.Context, principal *trust.Principal, worker workspace.WorkerSummary) *journeyv1.Worker {
	workers := s.authorizeWorkers(ctx, principal, []workspace.WorkerSummary{worker}, []workspace.WorkerSummary{worker})
	return workers[0]
}

// applyDirectoryDisclosure omits or masks every field the ruling withholds.
// Allow copies (already rendered by [toWorker]); Redacted substitutes the
// stand-in; anything else, including the zero effect, omits.
func applyDirectoryDisclosure(msg *journeyv1.Worker, disclosure authz.DirectoryDisclosure) {
	switch disclosure.Pay.Effect {
	case authz.EffectAllow:
	case authz.EffectRedacted:
		msg.BasePay = redactedPayStandIn
		msg.BonusTarget = redactedPayStandIn
		// The basis goes with the amount even under redaction. A reader told
		// the basis is ANNUAL_SALARY beside a redacted figure has been handed
		// a range the redaction was meant to withhold.
		msg.PayBasis = ""
	default:
		msg.BasePay = ""
		msg.BonusTarget = ""
		msg.PayBasis = ""
	}
	if disclosure.LegalName.Effect != authz.EffectAllow {
		msg.LegalName = ""
	}
	if disclosure.ManagerLinkage.Effect != authz.EffectAllow {
		msg.ManagerRef = ""
		msg.ManagerRelationship = nil
	}
}

// maskWorker omits every governed field, the fail-closed rendering when a
// subject's disclosure cannot be resolved at all.
func maskWorker(msg *journeyv1.Worker) {
	msg.BasePay = ""
	msg.BonusTarget = ""
	msg.PayBasis = ""
	msg.LegalName = ""
	msg.ManagerRef = ""
	msg.ManagerRelationship = nil
}
