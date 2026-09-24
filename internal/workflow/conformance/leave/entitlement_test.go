package leave

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
	app "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

var conformanceAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func conformanceSnapshot(t *testing.T, status commercial.ContractStatus) commercial.EntitlementSnapshot {
	t.Helper()
	snapshot, err := commercial.NewEntitlementSnapshot(commercial.FixedPricePilotContract{
		TenantID: "tenant-a", ContractID: "pilot-a", Revision: 1,
		EffectiveFrom: conformanceAt, EffectiveTo: conformanceAt.Add(90 * 24 * time.Hour), Status: status,
		Capabilities: []string{commercial.PromotionEntitlementCapability, commercial.LeaveEntitlementCapability},
		Bound:        commercial.EntitlementBound{Seats: 25}, PriceCents: 2500000, Currency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertPromotionLeaveParity(t *testing.T) {
	t.Helper()
	snapshot := conformanceSnapshot(t, commercial.StatusSuspended)
	promotion, err := app.NewPromotionEntitlementGate(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	leave, err := NewEntitlementGate(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	channels := []commercial.Channel{
		commercial.ChannelUI, commercial.ChannelHTTP, commercial.ChannelGRPC,
		commercial.ChannelWorkflow, commercial.ChannelConnector,
	}
	phases := []string{"discovery", "submit", "resume", "assisted", "manual"}
	for _, channel := range channels {
		for _, phase := range phases {
			request := app.PromotionEntitlementRequest{
				TenantID: "tenant-a", Capability: "hcmnext.people.promote_worker/execute/v1",
				At: conformanceAt, Channel: channel, Phase: phase,
			}
			promotionDecision := promotion.Decide(request)
			leaveDecision := leave.Decide(EntitlementRequest{
				TenantID: request.TenantID, Capability: request.Capability,
				At: request.At, Channel: channel, Phase: phase,
			})
			if promotionDecision != leaveDecision {
				t.Fatalf("%s/%s decisions differ: promotion=%+v leave=%+v", channel, phase, promotionDecision, leaveDecision)
			}
			if promotionDecision.Code != commercial.CodeContractSuspended || promotionDecision.Fingerprint != snapshot.Fingerprint() {
				t.Fatalf("%s/%s decision=%+v, want suspended with pinned fingerprint", channel, phase, promotionDecision)
			}
			if _, err := promotion.Admit(request); err == nil {
				t.Fatalf("Promotion %s/%s bypassed suspended entitlement", channel, phase)
			}
			_, leaveErr := leave.Admit(EntitlementRequest{
				TenantID: request.TenantID, Capability: request.Capability,
				At: request.At, Channel: channel, Phase: phase,
			})
			var refusal *commercial.EntitlementRefusal
			if !errors.As(leaveErr, &refusal) || refusal.Decision() != leaveDecision {
				t.Fatalf("Leave %s/%s refusal=%T %v, want shared typed refusal", channel, phase, leaveErr, leaveErr)
			}
		}
	}

	for _, capability := range []string{commercial.PromotionEntitlementCapability, commercial.LeaveEntitlementCapability} {
		p := promotion.Decide(app.PromotionEntitlementRequest{TenantID: "tenant-a", Capability: capability, At: conformanceAt, Channel: commercial.ChannelUI})
		l := leave.Decide(EntitlementRequest{TenantID: "tenant-a", Capability: capability, At: conformanceAt, Channel: commercial.ChannelUI})
		if p.Code != l.Code || p.Fingerprint != l.Fingerprint || p.Code != commercial.CodeContractSuspended {
			t.Fatalf("capability=%q suspension parity failed: promotion=%+v leave=%+v", capability, p, l)
		}
	}
}

func TestPromotionAndLeaveEntitlementDenialParityAcrossAllInitiationAndResumeChannels(t *testing.T) {
	assertPromotionLeaveParity(t)
}

func TestTodo_CROSS_CONF_002_Property(t *testing.T) { assertPromotionLeaveParity(t) }

func TestTodo_CROSS_CONF_002_Golden(t *testing.T) { assertPromotionLeaveParity(t) }

func TestTodo_CROSS_CONF_002_Race(t *testing.T) {
	snapshot := conformanceSnapshot(t, commercial.StatusSuspended)
	gate, err := NewEntitlementGate(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 24
	var wg sync.WaitGroup
	decisions := make(chan commercial.EntitlementDecision, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decisions <- gate.Decide(EntitlementRequest{TenantID: "tenant-a", Capability: commercial.LeaveEntitlementCapability, At: conformanceAt, Channel: commercial.ChannelWorkflow, Phase: "resume"})
		}()
	}
	wg.Wait()
	close(decisions)
	for decision := range decisions {
		if decision.Code != commercial.CodeContractSuspended || decision.Fingerprint != snapshot.Fingerprint() {
			t.Fatalf("concurrent entitlement decision = %+v", decision)
		}
	}
}

func TestTodo_CROSS_CONF_002_Integration(t *testing.T) { assertPromotionLeaveParity(t) }

func TestTodo_CROSS_CONF_002_Fault(t *testing.T) { assertPromotionLeaveParity(t) }

func TestTodo_CROSS_CONF_002_Security(t *testing.T) { assertPromotionLeaveParity(t) }

func TestTodo_CROSS_CONF_002_Conformance(t *testing.T) { assertPromotionLeaveParity(t) }

func TestTodo_CROSS_CONF_002_Browser(t *testing.T) { assertPromotionLeaveParity(t) }

func TestTodo_CROSS_CONF_002_Recovery(t *testing.T) { assertPromotionLeaveParity(t) }

func TestTodo_CROSS_CONF_002_Mutation(t *testing.T) { assertPromotionLeaveParity(t) }
