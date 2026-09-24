package crm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type campaignAudienceOwner struct {
	want     CampaignAudienceClaim
	err      error
	calls    int
	mutate   bool
	retained *CampaignAudienceClaim
}

func (v *campaignAudienceOwner) VerifyCampaignAudience(_ context.Context, got CampaignAudienceClaim) error {
	v.calls++
	if v.err != nil {
		return v.err
	}
	if !reflect.DeepEqual(got, v.want) {
		return errors.New("audience claim mismatch")
	}
	if v.mutate {
		got.Snapshot.SubjectIDs[0] = "worker:verifier-mutation"
		got.Snapshot.Watermarks[population.SubjectWorker] = campaignInstant(nil, 20)
		got.Definition.Criteria.Root.Values[0] = "verifier-mutation"
		v.retained = &got
	}
	return nil
}
func campaignInstant(_ *testing.T, day int) values.Instant {
	return values.NewInstant(time.Date(2026, 1, day, 12, 0, 0, 0, time.UTC))
}

func campaignFixture(t *testing.T) (CampaignRevision, *campaignAudienceOwner) {
	t.Helper()
	def := population.Definition{ID: "recruiting-audience", Owner: "population-owner", Subject: population.SubjectWorker, Scope: population.Scope{Tenant: "tenant-1", OrganizationScopeRef: "org:tenant-1", Purpose: "future recruiting"}, TemporalBasis: population.TemporalBasisAsOfCaller, UnknownDisclosure: population.UnknownDisclosureBlock, CountDisclosure: population.CountDisclosureExact, Criteria: population.Criteria{Root: population.Predicate{Kind: population.PredicateEquals, Field: "active", Values: []string{"true"}}}}
	known, err := values.NewKnownAt(campaignInstant(t, 10))
	if err != nil {
		t.Fatal(err)
	}
	restricted := population.RestrictedResult{Members: []population.Member{{Subject: crmRef("worker", "w-1"), Outcome: population.OutcomeIncluded}}, Completeness: population.CompletenessComplete, Count: values.Value(1), Versions: population.PolicyVersions{AuthZVersion: "authz/v1", PrivacyVersion: "privacy/v1", OrganizationVersion: "org/v1", PurposeVersion: "purpose/v1"}}
	snapshot, err := population.Freeze(def, "population/revision/7", restricted, campaignInstant(t, 10), known, map[population.SubjectKind]values.Instant{population.SubjectWorker: campaignInstant(t, 9)})
	if err != nil {
		t.Fatal(err)
	}
	money, err := values.NewMoney("25.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := values.NewInstantInterval(campaignInstant(t, 12), campaignInstant(t, 20))
	if err != nil {
		t.Fatal(err)
	}
	pool := validPool(t)
	pool.Effective, err = values.NewInstantInterval(campaignInstant(t, 1), campaignInstant(t, 31))
	if err != nil {
		t.Fatal(err)
	}
	c := CampaignRevision{CampaignID: crmRef("campaign", "campaign-1"), Revision: crmRevision(t), Pool: pool, Population: snapshot, PopulationDefinition: def, Purpose: "future recruiting", ContentRef: crmRef("content", "content-1"), Channels: []values.EntityRef{crmRef("channel", "email")}, Schedule: schedule, Frequency: FrequencyPolicy{MaxContacts: 2, WindowDays: 7}, Cost: money, Suppression: SuppressExpired}
	digest, err := c.computedDigest()
	if err != nil {
		t.Fatal(err)
	}
	poolDigest, err := campaignPoolDigest(c.Pool)
	if err != nil {
		t.Fatal(err)
	}
	c.CanonicalDigest = digest
	claim, err := c.audienceClaim(snapshot.AsOf, poolDigest)
	if err != nil {
		t.Fatal(err)
	}
	return c, &campaignAudienceOwner{want: claim}
}

func TestTodo_REV_038_02(t *testing.T) {
	c, owner := campaignFixture(t)
	members := make([]population.Member, 600)
	for i := range members {
		members[i] = population.Member{Subject: values.EntityRef{Tenant: "tenant-1", Kind: "worker", Id: fmt.Sprintf("00000000-0000-4000-8000-%012x", i+1)}, Outcome: population.OutcomeIncluded}
	}
	restricted := population.RestrictedResult{Members: members, Completeness: population.CompletenessComplete, Count: values.Value(len(members)), Versions: population.PolicyVersions{AuthZVersion: "authz/v1", PrivacyVersion: "privacy/v1", OrganizationVersion: "org/v1", PurposeVersion: "purpose/v1"}}
	snapshot, err := population.Freeze(c.PopulationDefinition, "population/revision/large", restricted, c.Population.AsOf, c.Population.KnownAt, c.Population.Watermarks)
	if err != nil {
		t.Fatal(err)
	}
	c.Population = snapshot
	c.CanonicalDigest, err = c.computedDigest()
	if err != nil {
		t.Fatal(err)
	}
	poolDigest, err := campaignPoolDigest(c.Pool)
	if err != nil {
		t.Fatal(err)
	}
	owner.want, err = c.audienceClaim(snapshot.AsOf, poolDigest)
	if err != nil {
		t.Fatalf("campaign audience claim did not page the 600-member snapshot: %v", err)
	}
	if len(owner.want.Snapshot.SubjectIDs) != 600 {
		t.Fatalf("paged audience claim has %d members, want 600", len(owner.want.Snapshot.SubjectIDs))
	}
	got, err := NewCampaign(context.Background(), c, owner)
	if err != nil {
		t.Fatalf("campaign rejected paginated 600-member snapshot: %v", err)
	}
	if owner.calls != 1 || len(got.Population.SubjectIDs) != 600 {
		t.Fatalf("campaign population members=%d verifier calls=%d, want 600 and 1", len(got.Population.SubjectIDs), owner.calls)
	}
}

func TestTodo_REV_038_02_Security(t *testing.T) {
	c, owner := campaignFixture(t)
	protected := c
	protected.Population.MembershipProtected = true
	if err := protected.Validate(); !errors.Is(err, ErrAudienceBlocked) {
		t.Fatalf("protected population err=%v, want ErrAudienceBlocked", err)
	}
	inconsistent := c
	inconsistent.Population.Count = values.Value(8)
	if _, err := NewCampaign(context.Background(), inconsistent, owner); !errors.Is(err, ErrAudienceBlocked) {
		t.Fatalf("inconsistent count err=%v, want ErrAudienceBlocked", err)
	}
	if _, err := NewCampaign(context.Background(), inconsistent, owner); err.Error() != ErrAudienceBlocked.Error() {
		t.Fatalf("population rejection disclosed details: %q", err)
	}
}

func TestTodo_CRM_003(t *testing.T) {
	c, owner := campaignFixture(t)
	owner.mutate = true
	got, err := NewCampaign(context.Background(), c, owner)
	if err != nil {
		t.Fatal(err)
	}
	if owner.calls != 1 || got.CanonicalDigest == "" || len(got.Population.SubjectIDs) != 1 {
		t.Fatalf("campaign=%+v verifier_calls=%d", got, owner.calls)
	}
	if err := got.EligibleAt(campaignInstant(t, 15)); err != nil {
		t.Fatalf("current audience blocked: %v", err)
	}
	owner.want.At = campaignInstant(t, 15)
	if err := got.AuthorizeAudienceAt(context.Background(), campaignInstant(t, 15), owner); err != nil {
		t.Fatalf("current audience authorization failed: %v", err)
	}
	c.Population.SubjectIDs[0] = "mutated"
	c.Population.Watermarks[population.SubjectWorker] = campaignInstant(t, 20)
	if got.Population.SubjectIDs[0] == "mutated" {
		t.Fatal("membership aliased caller")
	}
	if watermark, _ := got.Population.WatermarkFor(population.SubjectWorker); watermark == campaignInstant(t, 20) {
		t.Fatal("watermark aliased caller")
	}
	owner.retained.Snapshot.SubjectIDs[0] = "worker:retained-mutation"
	if got.Population.SubjectIDs[0] == "worker:verifier-mutation" || got.Population.SubjectIDs[0] == "worker:retained-mutation" || got.PopulationDefinition.Criteria.Root.Values[0] == "verifier-mutation" {
		t.Fatal("verifier mutation crossed the audience trust boundary")
	}
}

func TestTodo_CRM_003_Security(t *testing.T) {
	base, owner := campaignFixture(t)
	cases := map[string]func(*CampaignRevision){
		"purpose":                  func(c *CampaignRevision) { c.Purpose = "sales" },
		"partial":                  func(c *CampaignRevision) { c.Population.Completeness = population.CompletenessPartial },
		"hidden membership":        func(c *CampaignRevision) { c.Population.MembershipProtected = true; c.Population.SubjectIDs = nil },
		"cross tenant definition":  func(c *CampaignRevision) { c.PopulationDefinition.Scope.Tenant = "tenant-2" },
		"forged definition":        func(c *CampaignRevision) { c.PopulationDefinition.Owner = "caller" },
		"member payload tamper":    func(c *CampaignRevision) { c.Population.SubjectIDs[0] = "worker:forged" },
		"count payload tamper":     func(c *CampaignRevision) { c.Population.Count = values.Value(99) },
		"watermark payload tamper": func(c *CampaignRevision) { c.Population.Watermarks[population.SubjectWorker] = campaignInstant(t, 19) },
		"as-of payload tamper":     func(c *CampaignRevision) { c.Population.AsOf = campaignInstant(t, 19) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := base
			c.Population.SubjectIDs = append([]string(nil), base.Population.SubjectIDs...)
			c.Population.Watermarks = make(map[population.SubjectKind]values.Instant, len(base.Population.Watermarks))
			for kind, watermark := range base.Population.Watermarks {
				c.Population.Watermarks[kind] = watermark
			}
			c.PopulationDefinition.Criteria.Root = cloneCampaignPredicate(base.PopulationDefinition.Criteria.Root)
			mutate(&c)
			if _, err := NewCampaign(context.Background(), c, owner); err == nil {
				t.Fatalf("got %v", err)
			}
		})
	}
	if _, err := NewCampaign(context.Background(), base, nil); !errors.Is(err, ErrAudienceBlocked) {
		t.Fatalf("missing owner accepted: %v", err)
	}
	denying := *owner
	denying.err = errors.New("not current or consent withdrawn")
	if _, err := NewCampaign(context.Background(), base, &denying); !errors.Is(err, ErrAudienceBlocked) {
		t.Fatalf("owner denial accepted: %v", err)
	}
}

func TestTodo_CRM_003_Mutation(t *testing.T) {
	base, owner := campaignFixture(t)
	negative, err := values.NewMoney("-1.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*CampaignRevision){"negative cost": func(c *CampaignRevision) { c.Cost = negative }, "missing frequency window": func(c *CampaignRevision) { c.Frequency.WindowDays = 0 }, "duplicate channel": func(c *CampaignRevision) { c.Channels = append(c.Channels, c.Channels[0]) }, "resealed mutation": func(c *CampaignRevision) { c.Frequency.MaxContacts++; c.CanonicalDigest = "" }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := base
			mutate(&c)
			if _, err := NewCampaign(context.Background(), c, owner); err == nil {
				t.Fatal("mutation accepted")
			}
		})
	}
	valid, err := NewCampaign(context.Background(), base, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := valid.EligibleAt(campaignInstant(t, 21)); !errors.Is(err, ErrAudienceBlocked) {
		t.Fatalf("outside schedule eligible: %v", err)
	}
	valid.Suppression = SuppressExplicit
	if err := valid.EligibleAt(campaignInstant(t, 15)); err == nil {
		t.Fatalf("unsafe suppression eligible: %v", err)
	}
}
