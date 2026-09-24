package crm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/popscale"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidCampaign = errors.New("crm: invalid campaign")
	ErrAudienceBlocked = errors.New("crm: audience is unauthorized or incomplete")
)

type SuppressionPolicy string

const (
	SuppressExpired  SuppressionPolicy = "EXPIRED_OR_WITHDRAWN"
	SuppressExplicit SuppressionPolicy = "EXPLICIT_ONLY"
)

type CampaignRevision struct {
	CampaignID           values.EntityRef
	Revision             values.RevisionToken
	Pool                 PoolRevision
	Population           population.Snapshot
	PopulationDefinition population.Definition
	Purpose              string
	ContentRef           values.EntityRef
	Channels             []values.EntityRef
	Schedule             values.EffectiveInterval
	Frequency            FrequencyPolicy
	Cost                 values.Money
	Suppression          SuppressionPolicy
	CanonicalDigest      string
}

type FrequencyPolicy struct {
	MaxContacts uint32
	WindowDays  uint32
}

func (f FrequencyPolicy) Valid() bool { return f.MaxContacts > 0 && f.WindowDays > 0 }

type CampaignAudienceClaim struct {
	Tenant                     values.TenantId
	Purpose                    string
	PoolID                     values.EntityRef
	PoolRevision               values.RevisionToken
	PoolDigest                 string
	PopulationOwner            string
	PopulationDefinitionID     string
	PopulationDefinitionDigest string
	PopulationRevision         string
	PopulationDigest           string
	CampaignDigest             string
	At                         values.Instant
	Pool                       PoolRevision
	Definition                 population.Definition
	Snapshot                   population.Snapshot
}

type CampaignAudienceVerifier interface {
	VerifyCampaignAudience(context.Context, CampaignAudienceClaim) error
}

func (c CampaignRevision) Validate() error {
	if err := requireRef(c.CampaignID, "campaign", "campaign"); err != nil {
		return ErrInvalidCampaign
	}
	if !c.Revision.IsSpecified() || strings.TrimSpace(c.Purpose) == "" {
		return ErrInvalidCampaign
	}
	if err := c.Pool.Validate(); err != nil {
		return fmt.Errorf("%w: pool: %v", ErrInvalidCampaign, err)
	}
	if c.Population.Digest == "" || c.Population.DefinitionDigest == "" || c.Population.RevisionVersion == "" || c.Population.MembershipProtected || c.Population.Completeness != population.CompletenessComplete {
		return ErrAudienceBlocked
	}
	if err := c.PopulationDefinition.Validate(); err != nil || c.PopulationDefinition.ID != c.Population.DefinitionID || c.PopulationDefinition.Scope.Tenant != c.CampaignID.Tenant || c.PopulationDefinition.Scope.Purpose != c.Purpose || canonicalbytes.Digest(c.PopulationDefinition.Canonical()) != c.Population.DefinitionDigest {
		return ErrAudienceBlocked
	}
	if c.Purpose != c.Pool.Purpose {
		return ErrAudienceBlocked
	}
	if err := c.Population.AsOf.Validate(); err != nil || c.Population.KnownAt.Canonical() == nil {
		return ErrAudienceBlocked
	}
	if err := c.ContentRef.Validate(); err != nil {
		return ErrInvalidCampaign
	}
	if err := c.Schedule.Validate(); err != nil {
		return ErrInvalidCampaign
	}
	if err := c.Cost.Validate(); err != nil || c.Cost.Amount().Sign() < 0 || !c.Frequency.Valid() || len(c.Channels) == 0 || !c.Suppression.Valid() {
		return ErrInvalidCampaign
	}
	seenChannels := map[string]struct{}{}
	for _, ch := range c.Channels {
		if err := ch.Validate(); err != nil {
			return ErrInvalidCampaign
		}
		if ch.Tenant != c.CampaignID.Tenant {
			return ErrInvalidCampaign
		}
		if _, ok := seenChannels[ch.String()]; ok {
			return ErrInvalidCampaign
		}
		seenChannels[ch.String()] = struct{}{}
	}
	if c.CanonicalDigest != "" {
		got, err := c.computedDigest()
		if err != nil || c.CanonicalDigest != got {
			return ErrInvalidCampaign
		}
	}
	return sameTenant(c.CampaignID, c.Pool.PoolID, c.ContentRef)
}
func (s SuppressionPolicy) Valid() bool { return s == SuppressExpired || s == SuppressExplicit }

func (c CampaignRevision) computedDigest() (string, error) {
	poolDigest, err := campaignPoolDigest(c.Pool)
	if err != nil {
		return "", err
	}
	w := canonicalbytes.New("hcmnext.domains.crm.CampaignRevision", 1).Value("campaign", c.CampaignID).Value("revision", c.Revision).String("pool_digest", poolDigest).String("population_definition", c.Population.DefinitionID).String("population_definition_digest", c.Population.DefinitionDigest).String("population_revision", c.Population.RevisionVersion).String("population", c.Population.Digest).String("purpose", c.Purpose).Value("content", c.ContentRef).Count("channels", len(c.Channels))
	for _, ch := range c.Channels {
		w.Value("channel", ch)
	}
	w.Value("schedule", c.Schedule).Int("frequency.max_contacts", int64(c.Frequency.MaxContacts)).Int("frequency.window_days", int64(c.Frequency.WindowDays)).Value("cost", c.Cost).String("suppression", string(c.Suppression))
	b, err := w.Bytes()
	if err != nil {
		return "", err
	}
	return canonicalbytes.Digest(b), nil
}

func campaignPoolDigest(p PoolRevision) (string, error) {
	w := canonicalbytes.New("hcmnext.domains.crm.TalentPoolRevision", 1).Value("pool", p.PoolID).Value("revision", p.Revision).String("purpose", p.Purpose).String("criteria", p.Criteria).String("source.system", p.Source.System).String("source.reference", p.Source.Reference).Value("source.recorded_by", p.Source.RecordedBy).Value("consent.authority", p.Consent.Authority).String("consent.basis", p.Consent.Basis).Value("consent.evidence", p.Consent.Evidence).Value("scope.organization", p.Scope.Organization)
	if p.Scope.Population.Validate() == nil {
		w.Value("scope.population", p.Scope.Population)
	}
	return w.Value("effective", p.Effective).Value("owner", p.Owner).String("removal_policy", string(p.RemovalPolicy)).Digest()
}
func NewCampaign(ctx context.Context, c CampaignRevision, verifier CampaignAudienceVerifier) (CampaignRevision, error) {
	c.Channels = append([]values.EntityRef(nil), c.Channels...)
	c.PopulationDefinition.Criteria.Root = cloneCampaignPredicate(c.PopulationDefinition.Criteria.Root)
	c.Population.SubjectIDs = append([]string(nil), c.Population.SubjectIDs...)
	if c.Population.Watermarks != nil {
		watermarks := make(map[population.SubjectKind]values.Instant, len(c.Population.Watermarks))
		for k, v := range c.Population.Watermarks {
			watermarks[k] = v
		}
		c.Population.Watermarks = watermarks
	}
	if c.CanonicalDigest != "" {
		if got, err := c.computedDigest(); err != nil || got != c.CanonicalDigest {
			return CampaignRevision{}, ErrInvalidCampaign
		}
	}
	computed, err := c.computedDigest()
	if err != nil {
		return CampaignRevision{}, fmt.Errorf("%w: canonical digest: %v", ErrInvalidCampaign, err)
	}
	c.CanonicalDigest = computed
	if err := c.Validate(); err != nil {
		return CampaignRevision{}, err
	}
	if verifier == nil {
		return CampaignRevision{}, ErrAudienceBlocked
	}
	poolDigest, err := campaignPoolDigest(c.Pool)
	if err != nil {
		return CampaignRevision{}, fmt.Errorf("%w: pool digest: %v", ErrInvalidCampaign, err)
	}
	claim, err := c.audienceClaim(c.Population.AsOf, poolDigest)
	if err != nil {
		return CampaignRevision{}, ErrAudienceBlocked
	}
	if err := verifier.VerifyCampaignAudience(ctx, claim); err != nil {
		return CampaignRevision{}, fmt.Errorf("%w: audience owner verification: %v", ErrAudienceBlocked, err)
	}
	return c, nil
}

func (c CampaignRevision) AuthorizeAudienceAt(ctx context.Context, at values.Instant, verifier CampaignAudienceVerifier) error {
	if verifier == nil {
		return ErrAudienceBlocked
	}
	if err := c.EligibleAt(at); err != nil {
		return err
	}
	poolDigest, err := campaignPoolDigest(c.Pool)
	if err != nil {
		return err
	}
	claim, err := c.audienceClaim(at, poolDigest)
	if err != nil {
		return ErrAudienceBlocked
	}
	if err := verifier.VerifyCampaignAudience(ctx, claim); err != nil {
		return fmt.Errorf("%w: current audience verification: %v", ErrAudienceBlocked, err)
	}
	return nil
}

func (c CampaignRevision) audienceClaim(at values.Instant, poolDigest string) (CampaignAudienceClaim, error) {
	definition := c.PopulationDefinition
	definition.Criteria.Root = cloneCampaignPredicate(definition.Criteria.Root)
	snapshot := c.Population
	snapshot.SubjectIDs = nil
	session, err := popscale.NewSession(c.Population, popscale.Caller{MembershipDisclosed: true}, 256)
	if err != nil {
		return CampaignAudienceClaim{}, err
	}
	if err := session.Walk(func(page popscale.Page) error {
		snapshot.SubjectIDs = append(snapshot.SubjectIDs, page.Subjects...)
		return nil
	}); err != nil {
		return CampaignAudienceClaim{}, err
	}
	if snapshot.Watermarks != nil {
		watermarks := make(map[population.SubjectKind]values.Instant, len(snapshot.Watermarks))
		for kind, watermark := range snapshot.Watermarks {
			watermarks[kind] = watermark
		}
		snapshot.Watermarks = watermarks
	}
	return CampaignAudienceClaim{Tenant: c.CampaignID.Tenant, Purpose: c.Purpose, PoolID: c.Pool.PoolID, PoolRevision: c.Pool.Revision, PoolDigest: poolDigest, PopulationOwner: definition.Owner, PopulationDefinitionID: snapshot.DefinitionID, PopulationDefinitionDigest: snapshot.DefinitionDigest, PopulationRevision: snapshot.RevisionVersion, PopulationDigest: snapshot.Digest, CampaignDigest: c.CanonicalDigest, At: at, Pool: c.Pool, Definition: definition, Snapshot: snapshot}, nil
}

func cloneCampaignPredicate(p population.Predicate) population.Predicate {
	p.Values = append([]string(nil), p.Values...)
	p.Children = append([]population.Predicate(nil), p.Children...)
	for i := range p.Children {
		p.Children[i] = cloneCampaignPredicate(p.Children[i])
	}
	return p
}

func (c CampaignRevision) EligibleAt(at values.Instant) error {
	if err := c.Validate(); err != nil {
		return err
	}
	active, err := c.Schedule.ContainsInstant(at)
	if err != nil || !active {
		return ErrAudienceBlocked
	}
	if c.Pool.Effective.Kind() == values.IntervalKindInstant {
		active, err = c.Pool.Effective.ContainsInstant(at)
	} else {
		// A LocalDate pool has no zone with which an Instant can be converted.
		// Its owner must provide an explicitly zoned/instant eligibility basis.
		return ErrAudienceBlocked
	}
	if err != nil || !active || c.Suppression != SuppressExpired {
		return ErrAudienceBlocked
	}
	return nil
}
