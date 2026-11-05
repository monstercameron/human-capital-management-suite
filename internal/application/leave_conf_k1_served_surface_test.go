package application_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
	app "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	leave "github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/leave"
)

var servedLeaveAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

// TestTodo_CROSS_CONF_002_Served proves CROSS-CONF-002's GREEN clause through
// the served application boundary: the Leave entitlement gate built only from
// the served surface shares the pinned suspended decision and non-disclosing
// typed denial with the Promotion gate across every initiation/resume channel
// and phase.
func TestTodo_CROSS_CONF_002_Served(t *testing.T) {
	surface := application.NewServedLeaveConformanceSurface()
	if surface.ServingContractID() == "" || surface.ServingContractID() != leave.ServingContractID {
		t.Fatalf("served contract id = %q, want %q", surface.ServingContractID(), leave.ServingContractID)
	}
	if err := surface.ValidateServingContract(); err != nil {
		t.Fatalf("served ValidateServingContract: %v", err)
	}

	snapshot, err := commercial.NewEntitlementSnapshot(commercial.FixedPricePilotContract{
		TenantID: "tenant-a", ContractID: "pilot-a", Revision: 1,
		EffectiveFrom: servedLeaveAt, EffectiveTo: servedLeaveAt.Add(90 * 24 * time.Hour), Status: commercial.StatusSuspended,
		Capabilities: []string{commercial.PromotionEntitlementCapability, commercial.LeaveEntitlementCapability},
		Bound:        commercial.EntitlementBound{Seats: 25}, PriceCents: 2500000, Currency: "USD",
	})
	if err != nil {
		t.Fatalf("served snapshot: %v", err)
	}
	promotion, err := app.NewPromotionEntitlementGate(snapshot)
	if err != nil {
		t.Fatalf("served promotion gate: %v", err)
	}
	leaveGate, err := surface.NewEntitlementGate(snapshot)
	if err != nil {
		t.Fatalf("served leave gate: %v", err)
	}

	channels := []commercial.Channel{
		commercial.ChannelUI, commercial.ChannelHTTP, commercial.ChannelGRPC,
		commercial.ChannelWorkflow, commercial.ChannelConnector,
	}
	phases := []string{"discovery", "submit", "resume", "assisted", "manual"}
	for _, channel := range channels {
		for _, phase := range phases {
			promotionDecision := promotion.Decide(app.PromotionEntitlementRequest{
				TenantID: "tenant-a", Capability: "hcmnext.people.promote_worker/execute/v1",
				At: servedLeaveAt, Channel: channel, Phase: phase,
			})
			leaveDecision := leaveGate.Decide(leave.EntitlementRequest{
				TenantID: "tenant-a", Capability: "hcmnext.people.promote_worker/execute/v1",
				At: servedLeaveAt, Channel: channel, Phase: phase,
			})
			if promotionDecision != leaveDecision {
				t.Fatalf("served %s/%s decisions differ: promotion=%+v leave=%+v", channel, phase, promotionDecision, leaveDecision)
			}
			if promotionDecision.Code != commercial.CodeContractSuspended || promotionDecision.Fingerprint != snapshot.Fingerprint() {
				t.Fatalf("served %s/%s decision=%+v, want suspended with pinned fingerprint", channel, phase, promotionDecision)
			}
			if _, err := leaveGate.Admit(leave.EntitlementRequest{
				TenantID: "tenant-a", Capability: "hcmnext.people.promote_worker/execute/v1",
				At: servedLeaveAt, Channel: channel, Phase: phase,
			}); err == nil {
				t.Fatalf("served Leave %s/%s bypassed suspended entitlement", channel, phase)
			} else {
				var refusal *commercial.EntitlementRefusal
				if !errors.As(err, &refusal) || refusal.Decision() != leaveDecision {
					t.Fatalf("served Leave %s/%s refusal=%T %v, want shared typed refusal", channel, phase, err, err)
				}
			}
		}
	}
}

func TestServedLeaveConformanceAppSurface(t *testing.T) {
	var app application.App
	if got := app.LeaveConformance(); got.NewEntitlementGate == nil || got.ValidateServingContract == nil {
		t.Fatal("composed application omitted Leave conformance surface")
	}
	var nilApp *application.App
	if got := nilApp.LeaveConformance(); got.NewEntitlementGate != nil {
		t.Fatal("nil application exposed Leave conformance capabilities")
	}
}
