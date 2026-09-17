package jobarch

// Workspace is the authorized Job Architecture admin projection for
// UX-JOBARCH-001. It renders only authorized, server-resolved architecture
// state (family/level/profile graph, exact pay-band and increase rules,
// benefit impacts and publish status). The workspace owns no store and
// exposes no direct write: the single mutation entry point,
// ProposeWorkspacePublication, returns a candidate-bound proposal that only
// the governed publication workflow (PublishRevision) can apply.

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
)

var (
	ErrWorkspaceViewerUnauthorized   = errors.New("jobarch: workspace viewer is not authorized")
	ErrWorkspaceProposalUnauthorized = errors.New("jobarch: workspace viewer may not propose publication")
	ErrWorkspaceDirectPublish        = errors.New("jobarch: workspace publishes only through the governed workflow")
	ErrWorkspaceUnknownProfile       = errors.New("jobarch: workspace candidate does not contain the profile")
)

// WorkspaceViewer carries the role/page/action authorization the server
// resolved for the admin. Compensation values reach only a viewer that holds
// the compensation grant; publication proposals need the proposal grant.
type WorkspaceViewer struct {
	PrincipalID           string
	CanViewCompensation   bool
	CanProposePublication bool
}

type WorkspaceFamily struct {
	FamilyID  string
	Code      string
	Name      string
	ParentID  string
	Lifecycle Lifecycle
}

type WorkspaceLevel struct {
	LevelID   string
	FamilyID  string
	Code      string
	Title     string
	Rank      int
	Lifecycle Lifecycle
}

type WorkspacePayBand struct {
	BandID       string
	BandRevision string
	Currency     string
	Minimum      string
	Midpoint     string
	Maximum      string
}

type WorkspaceBenefitImpact struct {
	RuleRef      string
	RuleRevision string
	Authority    string
}

type WorkspaceLadderEdge struct {
	PathID              string
	FromProfileID       string
	ToProfileID         string
	Kind                PromotionPathKind
	MinimumBaseIncrease string
	MaximumBaseIncrease string
	CompensationPolicy  string
}

type WorkspaceProfile struct {
	ProfileID            string
	JobCode              string
	Title                string
	FamilyCode           string
	FamilyName           string
	LevelCode            string
	LevelTitle           string
	GradeCode            string
	GradeName            string
	Lifecycle            Lifecycle
	PublishStatus        string
	PayBand              *WorkspacePayBand
	PayBandKnown         bool
	CompensationWithheld bool
	BenefitImpacts       []WorkspaceBenefitImpact
	LadderRank           int
	LadderSpan           int
}

type WorkspaceView struct {
	ArchitectureID  string
	Revision        string
	CanonicalDigest string
	Families        []WorkspaceFamily
	Levels          []WorkspaceLevel
	Profiles        []WorkspaceProfile
	Ladder          []WorkspaceLadderEdge
}

// Canonical returns the deterministic projection bytes the admin workspace
// renders. Two resolutions of the same inputs produce identical bytes.
func (v WorkspaceView) Canonical() []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

type WorkspaceInput struct {
	Viewer       WorkspaceViewer
	Architecture ArchitectureRevision
	Bands        payband.Catalog
	Paths        []PromotionPathRevision
	PayZone      string
}

// ResolveWorkspaceView projects one authorized admin view over server-resolved
// architecture state. Unsupported job/grade combinations fail the whole
// resolution instead of being offered; salary and benefit rules stay withheld
// from viewers without the compensation grant.
func ResolveWorkspaceView(in WorkspaceInput) (WorkspaceView, error) {
	if strings.TrimSpace(in.Viewer.PrincipalID) == "" {
		return WorkspaceView{}, ErrWorkspaceViewerUnauthorized
	}
	if err := in.Architecture.Validate(); err != nil {
		return WorkspaceView{}, err
	}
	arch := in.Architecture
	families := make(map[string]JobFamilyRevision, len(arch.Families))
	for _, family := range arch.Families {
		families[family.FamilyIDOrID()] = family
	}
	levels := make(map[string]JobLevelRevision, len(arch.Levels))
	for _, level := range arch.Levels {
		levels[level.LevelIDOrID()] = level
	}
	grades := make(map[string]JobGradeRevision, len(arch.Grades))
	for _, grade := range arch.Grades {
		grades[grade.GradeIDOrID()] = grade
	}
	ladder := make([]WorkspaceLadderEdge, 0, len(in.Paths))
	impacts := make(map[string][]WorkspaceBenefitImpact)
	for i, path := range in.Paths {
		if err := path.Validate(); err != nil {
			return WorkspaceView{}, fmt.Errorf("%w: paths[%d]: %w", ErrInvalidArchitecture, i, err)
		}
		if _, ok := profileByID(arch.Profiles, path.From.ProfileID); !ok {
			return WorkspaceView{}, fmt.Errorf("%w: paths[%d].from references unknown profile %q", ErrInvalidArchitecture, i, path.From.ProfileID)
		}
		if _, ok := profileByID(arch.Profiles, path.To.ProfileID); !ok {
			return WorkspaceView{}, fmt.Errorf("%w: paths[%d].to references unknown profile %q", ErrInvalidArchitecture, i, path.To.ProfileID)
		}
		ladder = append(ladder, WorkspaceLadderEdge{
			PathID: path.PathIDOrID(), FromProfileID: path.From.ProfileID, ToProfileID: path.To.ProfileID,
			Kind:                path.Kind,
			MinimumBaseIncrease: path.MinimumBaseIncrease.String(),
			MaximumBaseIncrease: path.MaximumBaseIncrease.String(),
			CompensationPolicy:  path.CompensationPolicyRef.Ref + "@" + path.CompensationPolicyRef.Revision,
		})
		for _, rule := range path.BenefitEligibilityRuleRefs {
			impacts[path.To.ProfileID] = append(impacts[path.To.ProfileID], WorkspaceBenefitImpact{RuleRef: rule.Ref, RuleRevision: rule.Revision, Authority: rule.Authority})
		}
	}
	sort.Slice(ladder, func(i, j int) bool { return ladder[i].PathID < ladder[j].PathID })

	view := WorkspaceView{ArchitectureID: arch.ID, Revision: arch.Revision, CanonicalDigest: arch.computedDigest()}
	for _, family := range arch.Families {
		view.Families = append(view.Families, WorkspaceFamily{FamilyID: family.FamilyIDOrID(), Code: family.Code, Name: family.Name, ParentID: family.ParentID, Lifecycle: family.Lifecycle})
	}
	sort.Slice(view.Families, func(i, j int) bool { return view.Families[i].Code < view.Families[j].Code })
	for _, level := range arch.Levels {
		view.Levels = append(view.Levels, WorkspaceLevel{LevelID: level.LevelIDOrID(), FamilyID: level.FamilyIDOrFamily(), Code: level.Code, Title: level.Title, Rank: level.Rank, Lifecycle: level.Lifecycle})
	}
	sort.Slice(view.Levels, func(i, j int) bool {
		if view.Levels[i].FamilyID != view.Levels[j].FamilyID {
			return view.Levels[i].FamilyID < view.Levels[j].FamilyID
		}
		return view.Levels[i].Rank < view.Levels[j].Rank
	})
	for _, profile := range arch.Profiles {
		family := families[profile.FamilyIDOrRef()]
		level := levels[profile.LevelIDOrRef()]
		grade := grades[profile.GradeIDOrRef()]
		entry := WorkspaceProfile{
			ProfileID: profile.ProfileIDOrID(), JobCode: profile.JobCode, Title: profile.Title,
			FamilyCode: family.Code, FamilyName: family.Name,
			LevelCode: level.Code, LevelTitle: level.Title,
			GradeCode: grade.Code, GradeName: grade.Name,
			Lifecycle: profile.Lifecycle, PublishStatus: profile.Lifecycle.String(),
			BenefitImpacts: append([]WorkspaceBenefitImpact(nil), impacts[profile.ProfileIDOrID()]...),
			LadderRank:     level.Rank,
		}
		span := 0
		for _, other := range arch.Levels {
			if other.FamilyIDOrFamily() == profile.FamilyIDOrRef() {
				span++
			}
		}
		entry.LadderSpan = span
		band, ok := in.Bands.Lookup(payband.Scope{JobCode: profile.JobCode, Grade: grade.Code, PayZone: in.PayZone})
		if !ok {
			view.Profiles = append(view.Profiles, entry)
			continue
		}
		entry.PayBandKnown = true
		if !in.Viewer.CanViewCompensation {
			entry.CompensationWithheld = true
			view.Profiles = append(view.Profiles, entry)
			continue
		}
		entry.PayBand = &WorkspacePayBand{
			BandID: band.ID, BandRevision: band.Version, Currency: band.Currency(),
			Minimum: band.Minimum.String(), Midpoint: band.Midpoint.String(), Maximum: band.Maximum.String(),
		}
		view.Profiles = append(view.Profiles, entry)
	}
	sort.Slice(view.Profiles, func(i, j int) bool {
		if view.Profiles[i].FamilyCode != view.Profiles[j].FamilyCode {
			return view.Profiles[i].FamilyCode < view.Profiles[j].FamilyCode
		}
		if view.Profiles[i].LadderRank != view.Profiles[j].LadderRank {
			return view.Profiles[i].LadderRank < view.Profiles[j].LadderRank
		}
		return view.Profiles[i].JobCode < view.Profiles[j].JobCode
	})
	view.Ladder = ladder
	return view, nil
}

func profileByID(profiles []JobProfileRevision, id string) (JobProfileRevision, bool) {
	for _, profile := range profiles {
		if profile.ProfileIDOrID() == id {
			return profile, true
		}
	}
	return JobProfileRevision{}, false
}

// WorkspacePublicationProposal binds one DRAFT candidate to the governed
// publication workflow. The proposal carries no authority of its own:
// applying it still requires approval, compatibility evidence and reference
// resolution through PublishRevision.
type WorkspacePublicationProposal struct {
	ArchitectureID  string
	ProfileID       string
	CandidateDigest string
	RequestedBy     string
	RequestedAt     time.Time
}

// ProposeWorkspacePublication admits a DRAFT candidate into the publication
// workflow. Published architecture is never edited in place, so proposing a
// non-draft profile is refused with ErrWorkspaceDirectPublish.
func ProposeWorkspacePublication(viewer WorkspaceViewer, arch, candidate ArchitectureRevision, profileID string) (WorkspacePublicationProposal, error) {
	if strings.TrimSpace(viewer.PrincipalID) == "" {
		return WorkspacePublicationProposal{}, ErrWorkspaceViewerUnauthorized
	}
	if !viewer.CanProposePublication {
		return WorkspacePublicationProposal{}, ErrWorkspaceProposalUnauthorized
	}
	if err := arch.Validate(); err != nil {
		return WorkspacePublicationProposal{}, err
	}
	if err := candidate.Validate(); err != nil {
		return WorkspacePublicationProposal{}, err
	}
	if candidate.ID != arch.ID {
		return WorkspacePublicationProposal{}, fmt.Errorf("%w: candidate belongs to architecture %q", ErrWorkspaceUnknownProfile, candidate.ID)
	}
	profile, ok := profileByID(candidate.Profiles, profileID)
	if !ok {
		return WorkspacePublicationProposal{}, fmt.Errorf("%w: %q", ErrWorkspaceUnknownProfile, profileID)
	}
	if profile.Lifecycle != LifecycleDraft {
		return WorkspacePublicationProposal{}, fmt.Errorf("%w: profile %s is %s", ErrWorkspaceDirectPublish, profileID, profile.Lifecycle)
	}
	return WorkspacePublicationProposal{
		ArchitectureID: arch.ID, ProfileID: profileID,
		CandidateDigest: candidate.computedDigest(),
		RequestedBy:     viewer.PrincipalID, RequestedAt: time.Now().UTC(),
	}, nil
}
