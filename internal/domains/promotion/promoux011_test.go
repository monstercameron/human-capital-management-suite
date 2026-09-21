package promotion_test

// TestTodo_PROMOUX_011 is the PRIMARY: it proves, with no database, no
// transport and no browser involved, that:
//
//  1. Region and Transition's mappings are exhaustive over the five GREEN
//     names them and refuse anything else (ErrUnknownRegion), rather than a
//     default branch silently reusing another region's wiring.
//  2. A zero-value Transition and a zero-value RegionCursor both fail
//     closed: neither can be mistaken for "nothing to do" or "already
//     current".
//  3. The decisive proof this todo calls out: a viewer denied access to some
//     transitions observes no gap in the sequence numbers it is delivered,
//     using SubscriberSequencer -- contrasted directly against the naive
//     shared-position scheme, which does leak a gap, so the proof is that
//     the fix actually closes a real hole rather than a hole that never
//     existed.
//  4. A targeted invalidation does not cause unrelated regions to
//     re-render: driving the exact wire bytes SubscriberSequencer produces
//     through three independent tools/uxqual/invalidation.Client
//     subscriptions (same worker, different region; same region, different
//     worker) shows exactly one of the three ever calls its Refetch.
import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/journeyinvalidation"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/productquery"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/invalidation"
)

// promoux011OneShotStream delivers one already-canonical invalidation frame
// and then ends cleanly, the same minimal Stream shape
// tools/uxqual/productclient's own WEB-035 integration test uses to drive a
// real invalidation.Client without a network.
type promoux011OneShotStream struct {
	frame []byte
	used  bool
}

func (s *promoux011OneShotStream) Recv() ([]byte, error) {
	if s.used {
		return nil, io.EOF
	}
	s.used = true
	return append([]byte(nil), s.frame...), nil
}

func (*promoux011OneShotStream) Close() error { return nil }

var promoux011Now = time.Date(2026, 9, 12, 15, 4, 0, 0, time.UTC)

func promoux011Principal(t *testing.T, tenant values.TenantId) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               tenant,
		Subject:              "promoux011-viewer",
		SubjectKind:          trust.SubjectKindHuman,
		Roles:                []string{string(authz.RoleCompAdmin)},
		Purposes:             []string{authz.PurposeCompensationReview},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceHigh,
		SessionRef:           "promoux011-session",
		IssuedAt:             promoux011Now.Add(-time.Hour),
		ExpiresAt:            promoux011Now.Add(time.Hour),
		CredentialDigest:     "promoux011-credential-digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func promoux011Subject(tenant values.TenantId, id string) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: values.Kind("worker"), Id: id}
}

func promoux011Decision(t *testing.T, p *trust.Principal, s values.EntityRef) authz.Decision {
	t.Helper()
	d, err := authz.Enforce(authz.Request{
		Principal:   p,
		Purpose:     authz.PurposeCompensationReview,
		EffectiveAt: values.NewInstant(promoux011Now),
		Subject:     s,
		Fields:      []authz.FieldID{authz.FieldWorkerNumber},
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func promoux011Request(p *trust.Principal, globalSequence uint64, targets ...productquery.InvalidationTarget) productquery.InvalidationRequest {
	return productquery.InvalidationRequest{
		Principal: p,
		Purpose:   authz.PurposeCompensationReview,
		// Projection is set here to the same name the naive-scheme
		// assertions in this file exercise directly against
		// productquery.EmitInvalidation; SubscriberSequencer.EmitForSubscriber
		// overwrites it with the region's own projection name before it ever
		// reaches EmitInvalidation, so subscriber-routed calls in this file
		// do not depend on this value.
		Projection:     "promotion_detail",
		EffectiveAt:    values.NewInstant(promoux011Now),
		Fields:         []authz.FieldID{authz.FieldWorkerNumber},
		SourceSequence: globalSequence,
		Watermark:      globalSequence,
		Targets:        targets,
	}
}

func TestTodo_PROMOUX_011(t *testing.T) {
	t.Run("region and subject mapping is exhaustive", func(t *testing.T) {
		tenant := values.TenantId("acme")
		transition := promotion.Transition{
			Tenant:     tenant,
			JourneyRef: promoux011Subject(tenant, "00000000-0000-4000-8000-00000000a001"),
			WorkerRef:  promoux011Subject(tenant, "00000000-0000-4000-8000-00000000a002"),
			OccurredAt: promoux011Now,
			Revision:   1,
		}
		if err := transition.Validate(); err != nil {
			t.Fatalf("well-formed transition rejected: %v", err)
		}
		regions := promotion.Regions()
		if len(regions) != 5 {
			t.Fatalf("Regions() = %v, want exactly the 5 GREEN names", regions)
		}
		seenProjections := make(map[string]bool, len(regions))
		for _, region := range regions {
			projection, err := region.Projection()
			if err != nil || projection == "" {
				t.Fatalf("region %q has no projection: %v", region, err)
			}
			if seenProjections[projection] {
				t.Fatalf("projection %q reused by more than one region", projection)
			}
			seenProjections[projection] = true
			if _, err := transition.RegionSubject(region); err != nil {
				t.Fatalf("region %q has no subject mapping: %v", region, err)
			}
		}
		if _, err := promotion.Region("BOGUS").Projection(); err == nil {
			t.Fatal("unknown region silently resolved a projection")
		}
		if _, err := transition.RegionSubject(promotion.Region("BOGUS")); err == nil {
			t.Fatal("unknown region silently resolved a subject")
		}
	})

	t.Run("zero-value transition fails closed", func(t *testing.T) {
		if err := (promotion.Transition{}).Validate(); err == nil {
			t.Fatal("zero-value transition validated as legitimate")
		}
		tenant := values.TenantId("acme")
		valid := promotion.Transition{
			Tenant:     tenant,
			JourneyRef: promoux011Subject(tenant, "00000000-0000-4000-8000-00000000a001"),
			WorkerRef:  promoux011Subject(tenant, "00000000-0000-4000-8000-00000000a002"),
			OccurredAt: promoux011Now,
			Revision:   1,
		}
		zeroRevision := valid
		zeroRevision.Revision = 0
		if err := zeroRevision.Validate(); err == nil {
			t.Fatal("transition with zero revision validated as legitimate")
		}
		zeroTime := valid
		zeroTime.OccurredAt = time.Time{}
		if err := zeroTime.Validate(); err == nil {
			t.Fatal("transition with zero OccurredAt validated as legitimate")
		}
	})

	t.Run("zero-value region cursor never means current", func(t *testing.T) {
		var cursor promotion.RegionCursor
		for _, incoming := range []uint64{0, 1, 1000} {
			if !cursor.NeedsRefresh(incoming) {
				t.Fatalf("zero-value cursor treated incoming=%d as already current", incoming)
			}
		}
		if _, err := cursor.Advance(0); err == nil {
			t.Fatal("advancing to sequence 0 was accepted as meaningful progress")
		}
		advanced, err := cursor.Advance(5)
		if err != nil {
			t.Fatalf("Advance(5): %v", err)
		}
		if advanced.NeedsRefresh(5) {
			t.Fatal("cursor still needs refresh after observing the same sequence it just advanced to")
		}
		if !advanced.NeedsRefresh(6) {
			t.Fatal("cursor did not need refresh for a genuinely newer sequence")
		}
		if _, err := advanced.Advance(5); err == nil {
			t.Fatal("advancing to a non-newer sequence was accepted")
		}
	})

	t.Run("cache keys agree across independent call sites", func(t *testing.T) {
		tenant := values.TenantId("acme")
		subject := promoux011Subject(tenant, "00000000-0000-4000-8000-00000000a001")
		one := promotion.RegionCacheKey(promotion.RegionDetail, subject)
		two := promotion.RegionCacheKey(promotion.RegionDetail, values.EntityRef{Tenant: subject.Tenant, Kind: subject.Kind, Id: subject.Id})
		if one != two || one == "" {
			t.Fatalf("cache keys diverged for identical (region, subject): %q vs %q", one, two)
		}
		other := promotion.RegionCacheKey(promotion.RegionPerson, subject)
		if other == one {
			t.Fatal("different regions over the same subject collided on one cache key")
		}
	})

	// The decisive proof: a viewer denied access to some transitions
	// observes no gap in the sequence it is delivered.
	t.Run("subscriber sequencer closes the disclosure gap the naive scheme leaks", func(t *testing.T) {
		tenant := values.TenantId("acme")
		otherTenant := values.TenantId("other-tenant")
		viewer := promoux011Principal(t, tenant)

		// Three committed transitions arrive in order. The middle one's
		// subject belongs to a different tenant than the viewer, so
		// productquery.EmitInvalidation drops it for this viewer exactly the
		// way it would drop a subject the viewer's authorization denies --
		// this is the existing, already-proven authority filter
		// (TestTodo_ALIGN_023_Security exercises the same drop rule); this
		// test's subject is what a scheme built on top of that filter does
		// with the resulting hole.
		authorizedA := promoux011Subject(tenant, "00000000-0000-4000-8000-00000000b001")
		deniedB := promoux011Subject(otherTenant, "00000000-0000-4000-8000-00000000b002")
		authorizedC := promoux011Subject(tenant, "00000000-0000-4000-8000-00000000b003")

		reqA := promoux011Request(viewer, 1, productquery.InvalidationTarget{
			Subject: authorizedA, Revision: 1, Decision: promoux011Decision(t, viewer, authorizedA),
		})
		reqB := promoux011Request(viewer, 2, productquery.InvalidationTarget{Subject: deniedB, Revision: 1})
		reqC := promoux011Request(viewer, 3, productquery.InvalidationTarget{
			Subject: authorizedC, Revision: 1, Decision: promoux011Decision(t, viewer, authorizedC),
		})

		// First, show the bug is real: the naive scheme puts the shared
		// commit position directly on the wire. Transition B is filtered out
		// entirely (ok == false, no message at all) but transition C's
		// message still carries the global position 3, so this viewer's
		// delivered sequence is [1, 3] -- a hole exactly where the withheld
		// transition sits.
		msgA, okA, err := productquery.EmitInvalidation(reqA)
		if err != nil || !okA {
			t.Fatalf("transition A: emit=%v err=%v", okA, err)
		}
		_, okB, err := productquery.EmitInvalidation(reqB)
		if err != nil {
			t.Fatal(err)
		}
		if okB {
			t.Fatal("transition B (cross-tenant) was not filtered out by the authority check this test depends on")
		}
		msgC, okC, err := productquery.EmitInvalidation(reqC)
		if err != nil || !okC {
			t.Fatalf("transition C: emit=%v err=%v", okC, err)
		}
		if msgA.SourceSequence != 1 || msgC.SourceSequence != 3 {
			t.Fatalf("naive scheme sequences = %d, %d; want 1, 3 (the demonstration requires the gap to be real)", msgA.SourceSequence, msgC.SourceSequence)
		}
		if gap := msgC.SourceSequence - msgA.SourceSequence; gap != 2 {
			t.Fatalf("naive scheme did not reproduce a visible gap: delta=%d, want 2 (the withheld transition's existence would be inferable)", gap)
		}

		// Now the fix: the same three requests, routed through
		// SubscriberSequencer for one named subscriber and one region
		// (Detail). The viewer's delivered sequence must be exactly [1, 2]
		// with no gap -- nothing about the numbering can tell them a second
		// transition ever existed between the two they received.
		sequencer := promotion.NewSubscriberSequencer()
		const subscriberKey = "session:promoux011-viewer"

		fixedA, deliveredA, err := journeyinvalidation.EmitForSubscriber(sequencer, subscriberKey, promotion.RegionDetail, reqA)
		if err != nil || !deliveredA {
			t.Fatalf("subscriber emit A: delivered=%v err=%v", deliveredA, err)
		}
		_, deliveredB, err := journeyinvalidation.EmitForSubscriber(sequencer, subscriberKey, promotion.RegionDetail, reqB)
		if err != nil {
			t.Fatal(err)
		}
		if deliveredB {
			t.Fatal("denied transition B was delivered to the subscriber")
		}
		fixedC, deliveredC, err := journeyinvalidation.EmitForSubscriber(sequencer, subscriberKey, promotion.RegionDetail, reqC)
		if err != nil || !deliveredC {
			t.Fatalf("subscriber emit C: delivered=%v err=%v", deliveredC, err)
		}

		if fixedA.SourceSequence != 1 {
			t.Fatalf("first delivered message sequence = %d, want 1", fixedA.SourceSequence)
		}
		if fixedC.SourceSequence != 2 {
			t.Fatalf("second delivered message sequence = %d, want 2 (gap-free: the withheld transition must not consume a number)", fixedC.SourceSequence)
		}
		if delta := fixedC.SourceSequence - fixedA.SourceSequence; delta != 1 {
			t.Fatalf("subscriber-visible delta = %d, want exactly 1: a viewer denied access to some transitions must observe no gap", delta)
		}
		// The watermark chain must also be contiguous, since a client
		// (tools/uxqual/invalidation's handle()) checks Watermark too.
		if fixedC.Watermark != fixedA.SourceSequence {
			t.Fatalf("second message watermark = %d, want %d (must chain from the first delivered sequence, not the global position)", fixedC.Watermark, fixedA.SourceSequence)
		}
	})

	t.Run("a subscriber's counter never advances on a transition it receives nothing from", func(t *testing.T) {
		tenant := values.TenantId("acme")
		viewer := promoux011Principal(t, tenant)
		sequencer := promotion.NewSubscriberSequencer()
		const subscriberKey = "session:isolated"

		denied := promoux011Subject(values.TenantId("other-tenant"), "00000000-0000-4000-8000-00000000c001")
		for i := 0; i < 5; i++ {
			req := promoux011Request(viewer, uint64(i+1), productquery.InvalidationTarget{Subject: denied, Revision: 1})
			_, delivered, err := journeyinvalidation.EmitForSubscriber(sequencer, subscriberKey, promotion.RegionDetail, req)
			if err != nil {
				t.Fatal(err)
			}
			if delivered {
				t.Fatal("a cross-tenant subject was delivered")
			}
		}
		projection, err := promotion.RegionDetail.Projection()
		if err != nil {
			t.Fatal(err)
		}
		if got := sequencer.Snapshot()[subscriberKey+"\x00"+projection]; got != 0 {
			t.Fatalf("subscriber counter advanced to %d despite delivering nothing", got)
		}
	})

	// The other half of "shell count, Journeys, My Work, Person and detail
	// refresh only their affected regions": a message this todo's own
	// scheme produced for one region and one worker must not be admitted by
	// a client subscribed to a different region, or by a client subscribed
	// to the same region for a different worker. This is not new filtering
	// logic -- it is tools/uxqual/invalidation's existing
	// Projection/Subjects check in handle(), driven end to end with the
	// exact bytes SubscriberSequencer emits.
	t.Run("a targeted invalidation does not cause unrelated regions to re-render", func(t *testing.T) {
		tenant := values.TenantId("acme")
		viewer := promoux011Principal(t, tenant)
		workerX := promoux011Subject(tenant, "00000000-0000-4000-8000-00000000e001")
		workerY := promoux011Subject(tenant, "00000000-0000-4000-8000-00000000e002")

		sequencer := promotion.NewSubscriberSequencer()
		req := promoux011Request(viewer, 1, productquery.InvalidationTarget{
			Subject: workerX, Revision: 1, Decision: promoux011Decision(t, viewer, workerX),
		})
		message, delivered, err := journeyinvalidation.EmitForSubscriber(sequencer, "session:regions", promotion.RegionDetail, req)
		if err != nil || !delivered {
			t.Fatalf("emit for worker X detail: delivered=%v err=%v", delivered, err)
		}
		frame, err := message.CanonicalBytes()
		if err != nil {
			t.Fatal(err)
		}

		newCounter := func() *int {
			count := 0
			return &count
		}
		newClient := func(projection string, subject values.EntityRef) (*invalidation.Client, *int) {
			count := newCounter()
			client, err := invalidation.New(invalidation.Scope{
				Tenant: tenant, Projection: projection, Subjects: []values.EntityRef{subject},
			}, func(context.Context, invalidation.Refresh) error {
				*count++
				return nil
			}, invalidation.Options{})
			if err != nil {
				t.Fatal(err)
			}
			return client, count
		}

		detailProjection, err := promotion.RegionDetail.Projection()
		if err != nil {
			t.Fatal(err)
		}
		myWorkProjection, err := promotion.RegionMyWork.Projection()
		if err != nil {
			t.Fatal(err)
		}

		matchingClient, matchingCount := newClient(detailProjection, workerX)
		otherRegionClient, otherRegionCount := newClient(myWorkProjection, workerX)
		otherWorkerClient, otherWorkerCount := newClient(detailProjection, workerY)

		for _, subscription := range []*invalidation.Client{matchingClient, otherRegionClient, otherWorkerClient} {
			if err := subscription.Run(context.Background(), &promoux011OneShotStream{frame: frame}); err != nil {
				t.Fatalf("client.Run: %v", err)
			}
		}

		if *matchingCount != 1 {
			t.Fatalf("matching (region=detail, worker=X) refetch count = %d, want 1", *matchingCount)
		}
		if *otherRegionCount != 0 {
			t.Fatalf("unrelated region (my_work) refetch count = %d, want 0 -- a Detail-region message must not refresh My Work", *otherRegionCount)
		}
		if *otherWorkerCount != 0 {
			t.Fatalf("unrelated worker (Y) refetch count = %d, want 0 -- a message about worker X must not refresh worker Y's detail", *otherWorkerCount)
		}
	})
}
