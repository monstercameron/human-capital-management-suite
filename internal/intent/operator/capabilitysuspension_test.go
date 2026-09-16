package operator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestCapabilitySuspensionsAnswersOnlyForGovernedFamilies proves the bridge
// from bypass obligations to capability suspensions: a capability outside the
// governed families is never suspended, a governed one is suspended exactly
// while its family owes an overdue review, and the refusal names the
// obligation that caused it.
func TestCapabilitySuspensionsAnswersOnlyForGovernedFamilies(t *testing.T) {
	ctx := context.Background()
	journal := NewMemoryJournal()
	o := obligationAt("key-sus", FamilyRepair, "operator:ana", "approver:lead", testNow, "instance-1")
	if err := journal.RecordObligation(ctx, o); err != nil {
		t.Fatal(err)
	}
	clock := &movingClock{at: testNow}
	src, err := NewCapabilitySuspensions(journal, FixedTenant(testTenant), clock.now)
	if err != nil {
		t.Fatalf("NewCapabilitySuspensions: %v", err)
	}

	// Inside the review window nothing is suspended.
	for _, id := range []string{"workflow.repair.retry_node", "workflow.override.decision", "people.employee.read"} {
		if reason, suspended := src.SuspendedCapability(ctx, id); suspended {
			t.Errorf("%s suspended inside the review window: %s", id, reason)
		}
	}

	// Past the due review the repair family is suspended, and only it.
	clock.advance(ReviewWindow)
	reason, suspended := src.SuspendedCapability(ctx, "workflow.repair.retry_node")
	if !suspended || !strings.Contains(reason, o.ID) {
		t.Fatalf("workflow.repair.retry_node = %q, %v; want a suspension naming %s", reason, suspended, o.ID)
	}
	for _, id := range []string{"workflow.override.decision", "workflow.instances.pause", "people.employee.read", ""} {
		if _, suspended := src.SuspendedCapability(ctx, id); suspended {
			t.Errorf("%q was suspended by a repair obligation", id)
		}
	}

	// A review lifts it.
	if _, err := journal.DischargeObligation(ctx, testTenant, o.ID, ObligationReview{
		Reviewer: "reviewer:sam", Outcome: ObligationJustified, Note: "checked", At: clock.at}); err != nil {
		t.Fatalf("discharge: %v", err)
	}
	if reason, suspended := src.SuspendedCapability(ctx, "workflow.repair.retry_node"); suspended {
		t.Errorf("still suspended after the review: %s", reason)
	}
}

// TestCapabilitySuspensionsFailClosed proves a suspension source that cannot
// answer suspends the capability rather than admitting it: a governance
// control that fails open is not a control.
func TestCapabilitySuspensionsFailClosed(t *testing.T) {
	ctx := context.Background()
	if _, err := NewCapabilitySuspensions(nil, FixedTenant(testTenant), nil); CodeOf(err) != CodeInvalidRequest {
		t.Errorf("constructed a suspension source without a store: %v", err)
	}
	if _, err := NewCapabilitySuspensions(NewMemoryJournal(), nil, nil); CodeOf(err) != CodeInvalidRequest {
		t.Errorf("constructed a suspension source without a tenant resolver: %v", err)
	}
	if src, err := NewCapabilitySuspensions(NewMemoryJournal(), FixedTenant(testTenant), nil); err != nil || src.clock == nil {
		t.Errorf("a nil clock was not defaulted: %+v, %v", src, err)
	}

	for name, src := range map[string]*CapabilitySuspensions{
		"zero value":       {},
		"unreadable store": mustSuspensions(t, brokenObligations{}, FixedTenant(testTenant)),
		"no tenant":        mustSuspensions(t, NewMemoryJournal(), func(context.Context) (values.TenantId, bool) { return "", false }),
	} {
		reason, suspended := src.SuspendedCapability(ctx, "workflow.repair.retry_node")
		if !suspended || reason == "" {
			t.Errorf("%s: SuspendedCapability = %q, %v; want a closed failure with a reason", name, reason, suspended)
		}
		// An ungoverned capability is still none of this source's business.
		if _, suspended := src.SuspendedCapability(ctx, "people.employee.read"); suspended {
			t.Errorf("%s: suspended an ungoverned capability", name)
		}
	}

	// FixedTenant refuses an invalid tenant rather than resolving to one.
	if _, ok := FixedTenant("")(ctx); ok {
		t.Error("FixedTenant resolved a blank tenant")
	}
	if got, ok := FixedTenant(testTenant)(ctx); !ok || got != testTenant {
		t.Errorf("FixedTenant = %q, %v", got, ok)
	}
}

func mustSuspensions(t *testing.T, store ObligationStore, tenant TenantResolver) *CapabilitySuspensions {
	t.Helper()
	src, err := NewCapabilitySuspensions(store, tenant, func() time.Time { return testNow })
	if err != nil {
		t.Fatalf("NewCapabilitySuspensions: %v", err)
	}
	return src
}
