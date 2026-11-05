package workorderservice

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Bill002PolicyBook resolves pinned published pricing for billing drafts.
// Each entry binds one order's contract version to its server-authoritative
// pricing policy; RequestBillingDraft verifies the pin against the published
// template before use. In production the rate-card owner (SMB-SALE-002)
// populates the book; the book itself never invents rates.
type Bill002PolicyBook struct {
	mu       sync.Mutex
	policies map[string]workorderbilling.PricingPolicy
}

// NewBill002PolicyBook returns an empty pinned-pricing resolver.
func NewBill002PolicyBook() *Bill002PolicyBook {
	return &Bill002PolicyBook{policies: map[string]workorderbilling.PricingPolicy{}}
}

// Add pins one policy for an order contract version.
func (b *Bill002PolicyBook) Add(tenant, orderID, contractVersion string, policy workorderbilling.PricingPolicy) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.policies[strings.TrimSpace(tenant)+"\x00"+strings.TrimSpace(orderID)+"\x00"+strings.TrimSpace(contractVersion)] = policy
}

// ResolvePricing implements PricingPolicies against the pinned book.
func (b *Bill002PolicyBook) ResolvePricing(_ context.Context, tenant, _, orderID, contractVersion string) (workorderbilling.PricingPolicy, error) {
	tenant = strings.TrimSpace(tenant)
	orderID = strings.TrimSpace(orderID)
	contractVersion = strings.TrimSpace(contractVersion)
	if tenant == "" || orderID == "" || contractVersion == "" {
		return workorderbilling.PricingPolicy{}, fmt.Errorf("bill002: pricing needs tenant, order and contract version")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	policy, ok := b.policies[tenant+"\x00"+orderID+"\x00"+contractVersion]
	if !ok {
		return workorderbilling.PricingPolicy{}, fmt.Errorf("bill002: no pinned pricing for order %s contract %s", orderID, contractVersion)
	}
	return policy, nil
}

// Bill002SourceAdapter serves approved immutable sources from a
// SourceClaimRegistry through BillingSourceProvider. It emits only eligible
// views for the mode it was constructed with, in billable units with exact
// math, and only customer-safe descriptions: pay details never enter a view.
type Bill002SourceAdapter struct {
	registry *workorderbilling.SourceClaimRegistry
	entity   string
	actor    string
	mode     workorderbilling.PricingMode
}

// NewBill002SourceAdapter binds a registry to one billing mode.
func NewBill002SourceAdapter(registry *workorderbilling.SourceClaimRegistry, entity, actor string, mode workorderbilling.PricingMode) *Bill002SourceAdapter {
	return &Bill002SourceAdapter{registry: registry, entity: entity, actor: actor, mode: mode}
}

// Sources implements BillingSourceProvider.
func (a *Bill002SourceAdapter) Sources(_ context.Context, order workorder.Snapshot, start, end time.Time) ([]workorderbilling.Source, map[string]string, error) {
	if a.registry == nil {
		return nil, nil, fmt.Errorf("bill002: source registry is not configured")
	}
	op := workorderbilling.BillOp{TenantID: order.TenantID, LegalEntityID: a.entity, ActorID: a.actor, BillingAllowed: true}
	views, err := a.registry.QuerySources(op, order.ID, start, end)
	if err != nil {
		return nil, nil, err
	}
	sources := []workorderbilling.Source{}
	descriptions := map[string]string{}
	for _, view := range views {
		if !view.Eligible || view.PricingMode != a.mode {
			continue
		}
		source, err := bill002ViewSource(order.ID, view)
		if err != nil {
			return nil, nil, err
		}
		sources = append(sources, source)
		descriptions[source.ID] = view.Description
	}
	return sources, descriptions, nil
}

func bill002ViewSource(orderID string, view workorderbilling.BillSourceView) (workorderbilling.Source, error) {
	id := view.FamilyID + ":" + strconv.FormatUint(view.Revision, 10)
	revision := strconv.FormatUint(view.Revision, 10)
	switch view.PricingMode {
	case workorderbilling.PricingUnitPrice:
		qty, err := bill002Decimal(view.BillableRemaining)
		if err != nil {
			return workorderbilling.Source{}, fmt.Errorf("bill002: source %s billable quantity: %v", id, err)
		}
		return workorderbilling.Source{
			ID: id, Revision: revision, WorkOrderID: orderID,
			Kind: workorderbilling.SourceAcceptedQuantity, Accepted: true,
			Quantity: qty, Unit: view.BillableUnit,
		}, nil
	case workorderbilling.PricingTimeMaterial:
		kind, err := bill002TMKind(view.Kind)
		if err != nil {
			return workorderbilling.Source{}, fmt.Errorf("bill002: source %s: %v", id, err)
		}
		basis := view.RemainingAmount
		if view.CostAmount != "" {
			basis = view.CostAmount
		}
		amount, err := bill002Money(basis)
		if err != nil {
			return workorderbilling.Source{}, fmt.Errorf("bill002: source %s cost basis: %v", id, err)
		}
		return workorderbilling.Source{
			ID: id, Revision: revision, WorkOrderID: orderID,
			Kind: kind, Approved: true, Amount: amount, Category: view.Category,
		}, nil
	case workorderbilling.PricingFixedMilestone:
		qty, err := bill002Decimal(view.RemainingQty)
		if err != nil {
			return workorderbilling.Source{}, fmt.Errorf("bill002: source %s quantity: %v", id, err)
		}
		return workorderbilling.Source{
			ID: id, Revision: revision, WorkOrderID: orderID,
			Kind: workorderbilling.SourceAcceptedMilestone, Accepted: true,
			Quantity: qty, Unit: view.Unit, MilestoneID: view.MilestoneRef,
		}, nil
	default:
		return workorderbilling.Source{}, fmt.Errorf("bill002: source %s has unknown pricing mode", id)
	}
}

func bill002TMKind(kind workorderbilling.BillSourceKind) (workorderbilling.SourceKind, error) {
	switch kind {
	case workorderbilling.BillKindMaterial, workorderbilling.BillKindExpense:
		return workorderbilling.SourceApprovedMaterial, nil
	case workorderbilling.BillKindSubcontract:
		return workorderbilling.SourceApprovedSubcontract, nil
	case workorderbilling.BillKindTime:
		return workorderbilling.SourceApprovedTime, nil
	default:
		return "", fmt.Errorf("kind %q is not a time/material source", kind)
	}
}

func bill002Decimal(text string) (values.Decimal, error) {
	trimmed := strings.TrimSpace(text)
	scale := int32(0)
	if dot := strings.IndexByte(trimmed, '.'); dot >= 0 {
		scale = int32(len(trimmed) - dot - 1)
	}
	return values.NewDecimal(trimmed, scale, values.RoundingExactRequired)
}

func bill002Money(text string) (values.Money, error) {
	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	if len(parts) != 2 {
		return values.Money{}, fmt.Errorf("money %q must read like 50.00 USD", text)
	}
	scale := int32(0)
	if dot := strings.IndexByte(parts[0], '.'); dot >= 0 {
		scale = int32(len(parts[0]) - dot - 1)
	}
	return values.NewMoney(parts[0], parts[1], scale, values.RoundingExactRequired)
}
