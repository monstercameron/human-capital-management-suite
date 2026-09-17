// CRM-004 RED: campaign delivery and engagement tracking must observe only
// approved signals (never covert surveillance) and must reconcile
// preference, consent, bounce and fallback states.
package crm

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func crm004Ref(kind, suffix string) values.EntityRef {
	return values.EntityRef{Tenant: "tenant-1", Kind: values.Kind(kind), Id: "00000000-0000-4000-8000-0000000000" + suffix}
}

func crm004Campaign(t *testing.T) CampaignRevision {
	t.Helper()
	c, owner := campaignFixture(t)
	sealed, err := NewCampaign(context.Background(), c, owner)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func crm004Plan(t *testing.T, campaign CampaignRevision) DeliveryPlan {
	t.Helper()
	return DeliveryPlan{
		CampaignDigest:  campaign.CanonicalDigest,
		Channel:         crm004Ref("channel", "21"),
		FallbackChannel: crm004Ref("channel", "22"),
		ApprovedSignals: []SignalKind{SignalDelivery, SignalReply, SignalClick, SignalBounce, SignalOptOut, SignalConsentWithdrawn},
		Recipients: []RecipientContact{
			{Subject: crm004Ref("worker", "31"), HasConsent: true},
			{Subject: crm004Ref("worker", "32"), HasConsent: true, PreferenceOptOut: true},
			{Subject: crm004Ref("worker", "33")},
			{Subject: crm004Ref("worker", "34"), HasConsent: true, PreviouslyBounced: true},
		},
		SendAt: campaignInstant(t, 15),
	}
}

func crm004Deliver(t *testing.T) (CampaignRevision, DeliveryBatch) {
	t.Helper()
	campaign := crm004Campaign(t)
	batch, err := Deliver(crm004Plan(t, campaign), campaign, campaignInstant(t, 15))
	if err != nil {
		t.Fatal(err)
	}
	return campaign, batch
}

func crm004DeliveryID(t *testing.T, batch DeliveryBatch, subject values.EntityRef) string {
	t.Helper()
	for _, r := range batch.Records {
		if r.Subject == subject {
			return r.DeliveryID
		}
	}
	t.Fatalf("no delivery for %v", subject)
	return ""
}

// TestTodo_CRM_004 is the PRIMARY contract: only approved signals are
// tracked, and preference/consent/bounce/fallback states reconcile.
func TestTodo_CRM_004(t *testing.T) {
	_, batch := crm004Deliver(t)
	if len(batch.Records) != 4 || batch.CanonicalDigest == "" {
		t.Fatalf("batch=%+v", batch)
	}
	state := map[string]ContactState{}
	for _, r := range batch.Records {
		state[r.Subject.Id] = r.State
		if r.DeliveryID == "" {
			t.Fatalf("delivery without id: %+v", r)
		}
	}
	if state[crm004Ref("worker", "31").Id] != ContactSent {
		t.Fatalf("consented contact not sent: %+v", state)
	}
	if state[crm004Ref("worker", "32").Id] != ContactSuppressed || state[crm004Ref("worker", "33").Id] != ContactSuppressed {
		t.Fatalf("opt-out/unconsented contacts not suppressed: %+v", state)
	}
	if state[crm004Ref("worker", "34").Id] != ContactFallbackQueued {
		t.Fatalf("bounced contact without fallback: %+v", state)
	}

	t.Run("approved-tracking-reconciles", func(t *testing.T) {
		id := crm004DeliveryID(t, batch, crm004Ref("worker", "31"))
		delivered, err := batch.RecordEngagement(EngagementEvent{DeliveryID: id, Signal: SignalDelivery, At: campaignInstant(t, 15)})
		if err != nil {
			t.Fatal(err)
		}
		engaged, err := delivered.RecordEngagement(EngagementEvent{DeliveryID: id, Signal: SignalClick, At: campaignInstant(t, 16)})
		if err != nil {
			t.Fatal(err)
		}
		summary := engaged.Summary()
		if summary.Engaged != 1 || summary.Suppressed != 2 || summary.FallbackQueued != 1 || summary.Total != 4 {
			t.Fatalf("summary=%+v", summary)
		}
		if summary.CanonicalDigest == "" || summary.CanonicalDigest == batch.CanonicalDigest {
			t.Fatalf("summary digest not rebound: %+v", summary)
		}
	})

	t.Run("unapproved-signal-refused-without-trace", func(t *testing.T) {
		narrow := batch
		narrow.ApprovedSignals = []SignalKind{SignalDelivery, SignalReply}
		id := crm004DeliveryID(t, batch, crm004Ref("worker", "31"))
		event := EngagementEvent{DeliveryID: id, Signal: SignalClick, At: campaignInstant(t, 16)}
		if _, err := narrow.RecordEngagement(event); !errors.Is(err, ErrSurveillanceRefused) {
			t.Fatalf("unapproved click err=%v", err)
		}
		narrowDelivered, err := narrow.RecordEngagement(EngagementEvent{DeliveryID: id, Signal: SignalDelivery, At: campaignInstant(t, 15)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := narrowDelivered.RecordEngagement(event); !errors.Is(err, ErrSurveillanceRefused) {
			t.Fatalf("post-delivery unapproved click err=%v", err)
		}
	})
}

// TestTodo_CRM_004_Race proves concurrent engagement recording converges on
// one sealed digest.
func TestTodo_CRM_004_Race(t *testing.T) {
	_, batch := crm004Deliver(t)
	id := crm004DeliveryID(t, batch, crm004Ref("worker", "31"))
	const racers = 16
	var wg sync.WaitGroup
	digests := make([]string, racers)
	errs := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := batch.RecordEngagement(EngagementEvent{DeliveryID: id, Signal: SignalDelivery, At: campaignInstant(t, 15)})
			if err == nil {
				digests[i] = got.CanonicalDigest
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i := 0; i < racers; i++ {
		if errs[i] != nil || digests[i] != digests[0] || digests[0] == "" {
			t.Fatalf("racer %d diverged: digest=%q err=%v", i, digests[i], errs[i])
		}
	}
}

// TestTodo_CRM_004_Integration proves the governed chain campaign seal ->
// delivery -> engagement -> summary carries one digest lineage.
func TestTodo_CRM_004_Integration(t *testing.T) {
	campaign := crm004Campaign(t)
	batch, err := Deliver(crm004Plan(t, campaign), campaign, campaignInstant(t, 15))
	if err != nil {
		t.Fatal(err)
	}
	if batch.CampaignDigest != campaign.CanonicalDigest {
		t.Fatalf("delivery not bound to campaign: %+v", batch)
	}
	id := crm004DeliveryID(t, batch, crm004Ref("worker", "31"))
	delivered, err := batch.RecordEngagement(EngagementEvent{DeliveryID: id, Signal: SignalDelivery, At: campaignInstant(t, 15)})
	if err != nil {
		t.Fatal(err)
	}
	replied, err := delivered.RecordEngagement(EngagementEvent{DeliveryID: id, Signal: SignalReply, At: campaignInstant(t, 16)})
	if err != nil {
		t.Fatal(err)
	}
	summary := replied.Summary()
	if summary.Engaged != 1 || summary.Total != 4 || summary.CanonicalDigest == "" {
		t.Fatalf("integrated summary=%+v", summary)
	}
	if batch.Records[0].State != ContactSent {
		t.Fatal("engagement mutated the sealed batch")
	}
}

// TestTodo_CRM_004_Fault proves bounce/fallback, consent withdrawal and
// schedule violations reach allowed states with no lost effects.
func TestTodo_CRM_004_Fault(t *testing.T) {
	campaign := crm004Campaign(t)

	t.Run("bounce-without-fallback-parks", func(t *testing.T) {
		plan := crm004Plan(t, campaign)
		plan.FallbackChannel = values.EntityRef{}
		plan.Recipients = []RecipientContact{{Subject: crm004Ref("worker", "31"), HasConsent: true}}
		batch, err := Deliver(plan, campaign, campaignInstant(t, 15))
		if err != nil {
			t.Fatal(err)
		}
		bounced, err := batch.RecordEngagement(EngagementEvent{DeliveryID: batch.Records[0].DeliveryID, Signal: SignalBounce, At: campaignInstant(t, 16)})
		if err != nil {
			t.Fatal(err)
		}
		if bounced.Records[0].State != ContactBounced || !bounced.SuppressedOrParkedFor(batch.Records[0].DeliveryID) {
			t.Fatalf("bounce not parked: %+v", bounced.Records)
		}
	})

	t.Run("consent-withdrawal-blocks-tracking", func(t *testing.T) {
		_, batch := crm004Deliver(t)
		id := crm004DeliveryID(t, batch, crm004Ref("worker", "31"))
		withdrawn, err := batch.RecordEngagement(EngagementEvent{DeliveryID: id, Signal: SignalConsentWithdrawn, At: campaignInstant(t, 16)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := withdrawn.RecordEngagement(EngagementEvent{DeliveryID: id, Signal: SignalClick, At: campaignInstant(t, 17)}); !errors.Is(err, ErrContactSuppressed) {
			t.Fatalf("post-withdrawal tracking err=%v", err)
		}
		if summary := withdrawn.Summary(); summary.Unsubscribed != 1 {
			t.Fatalf("withdrawal not reconciled: %+v", summary)
		}
	})

	t.Run("send-outside-schedule-refused", func(t *testing.T) {
		if _, err := Deliver(crm004Plan(t, campaign), campaign, campaignInstant(t, 21)); !errors.Is(err, ErrInvalidDelivery) {
			t.Fatalf("out-of-schedule send err=%v", err)
		}
	})
}

// TestTodo_CRM_004_Security proves unapproved tracking is denied without
// existence or data leakage, and foreign tenants cannot be contacted.
func TestTodo_CRM_004_Security(t *testing.T) {
	_, batch := crm004Deliver(t)
	subject := crm004Ref("worker", "31")
	id := crm004DeliveryID(t, batch, subject)
	for name, event := range map[string]EngagementEvent{
		"unknown kind":     {DeliveryID: id, Signal: SignalKind("LOCATION_PING"), At: campaignInstant(t, 16)},
		"empty kind":       {DeliveryID: id, Signal: "", At: campaignInstant(t, 16)},
		"unknown delivery": {DeliveryID: "delivery-9999", Signal: SignalClick, At: campaignInstant(t, 16)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := batch.RecordEngagement(event)
			if !errors.Is(err, ErrSurveillanceRefused) && !errors.Is(err, ErrInvalidDelivery) {
				t.Fatalf("err=%v", err)
			}
			if err != nil && strings.Contains(err.Error(), subject.Id) {
				t.Fatalf("refusal leaks subject: %v", err)
			}
		})
	}
	campaign := crm004Campaign(t)
	plan := crm004Plan(t, campaign)
	plan.Recipients = []RecipientContact{{Subject: values.EntityRef{Tenant: "tenant-2", Kind: "worker", Id: "00000000-0000-4000-8000-000000000099"}, HasConsent: true}}
	if _, err := Deliver(plan, campaign, campaignInstant(t, 15)); !errors.Is(err, ErrInvalidDelivery) {
		t.Fatalf("cross-tenant delivery err=%v", err)
	}
}

// TestTodo_CRM_004_Mutation proves forged digests, duplicate recipients and
// tampered batches are rejected.
func TestTodo_CRM_004_Mutation(t *testing.T) {
	campaign := crm004Campaign(t)
	plan := crm004Plan(t, campaign)
	plan.CampaignDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := Deliver(plan, campaign, campaignInstant(t, 15)); !errors.Is(err, ErrInvalidDelivery) {
		t.Fatalf("forged campaign digest err=%v", err)
	}
	duplicated := crm004Plan(t, campaign)
	duplicated.Recipients = append(duplicated.Recipients, duplicated.Recipients[0])
	if _, err := Deliver(duplicated, campaign, campaignInstant(t, 15)); !errors.Is(err, ErrInvalidDelivery) {
		t.Fatalf("duplicate recipient err=%v", err)
	}
	_, batch := crm004Deliver(t)
	tampered := batch
	tampered.Records = append([]DeliveryRecord(nil), batch.Records...)
	tampered.Records[0].State = ContactEngaged
	if err := tampered.Verify(); !errors.Is(err, ErrInvalidDelivery) {
		t.Fatalf("tampered batch Verify=%v", err)
	}
	if err := batch.Verify(); err != nil {
		t.Fatalf("sealed batch Verify=%v", err)
	}
}
